// Package docker provides a Docker-backed Runtime with host fallback.
package docker

import (
	"bytes"
	"context"
	"fmt"
	"log"
	"os"
	"os/exec"
	"strings"
	"sync"
	"syscall"
	"time"

	"github.com/YanChao1999/Cell/api"
	cachepkg "github.com/YanChao1999/Cell/internal/worker/cache"
	"github.com/YanChao1999/Cell/internal/worker/runtime"
)

type mode int

const (
	modeHost mode = iota
	modeDocker
)

type cellRuntime struct {
	mode      mode
	container string // docker container name/id when modeDocker
	workspace string
	env       map[string]string // host-fallback / extra exec env (cache paths)
}

type activeExec struct {
	cancel    context.CancelFunc
	cmd       *exec.Cmd
	mode      mode
	container string
	marker    string
	done      chan struct{}
}

// Runtime uses Docker when the CLI and daemon are available; otherwise it
// falls back to host `sh -c` in the workspace when isolation policy allows.
type Runtime struct {
	useDocker bool
	mu        sync.Mutex
	cells     map[string]*cellRuntime
	execs     map[string]map[uint64]*activeExec // cellID → execID → active
	nextExec  uint64
}

// New detects Docker availability and returns a Runtime.
func New() *Runtime {
	ok := dockerAvailable()
	if !ok {
		log.Println("runtime/docker: docker unavailable; create/exec/destroy will use host fallback")
	} else {
		log.Println("runtime/docker: docker available")
	}
	return &Runtime{
		useDocker: ok,
		cells:     make(map[string]*cellRuntime),
		execs:     make(map[string]map[uint64]*activeExec),
	}
}

// Available reports whether Docker was detected at construction time.
func (r *Runtime) Available() bool { return r.useDocker }

func dockerAvailable() bool {
	// Explicit host mode for CI smoke and machines that should not pull images.
	if os.Getenv("CELL_RUNTIME") == "host" {
		return false
	}
	if _, err := exec.LookPath("docker"); err != nil {
		return false
	}
	ctx, cancel := context.WithTimeout(context.Background(), 3*time.Second)
	defer cancel()
	cmd := exec.CommandContext(ctx, "docker", "info")
	cmd.Env = os.Environ()
	if err := cmd.Run(); err != nil {
		return false
	}
	return true
}

// PruneLabeledContainers force-removes any leftover containers labeled cell.id.
// Called at daemon startup so prior crashes cannot leave reachable workloads.
func PruneLabeledContainers(ctx context.Context) error {
	if os.Getenv("CELL_RUNTIME") == "host" {
		return nil
	}
	if _, err := exec.LookPath("docker"); err != nil {
		return nil
	}
	list := exec.CommandContext(ctx, "docker", "ps", "-aq", "--filter", "label=cell.id")
	list.Env = os.Environ()
	out, err := list.Output()
	if err != nil {
		return fmt.Errorf("runtime/docker: list orphan containers: %w", err)
	}
	ids := strings.Fields(string(out))
	for _, id := range ids {
		rm := exec.CommandContext(ctx, "docker", "rm", "-f", id)
		rm.Env = os.Environ()
		if out, err := rm.CombinedOutput(); err != nil {
			log.Printf("runtime/docker: prune %s: %v (%s)", id, err, bytes.TrimSpace(out))
		}
	}
	if len(ids) > 0 {
		log.Printf("runtime/docker: pruned %d orphan cell container(s) on startup", len(ids))
	}
	return nil
}

// Create starts a long-lived container (docker) or records a host fallback.
func (r *Runtime) Create(ctx context.Context, opts runtime.CreateOpts) error {
	network, err := normalizeNetwork(opts.Network)
	if err != nil {
		return err
	}

	if r.useDocker && opts.Image == "" {
		return fmt.Errorf("runtime/docker: image required when Docker is enabled (refusing host escape)")
	}

	if r.useDocker {
		name := containerName(opts.CellID)
		args := []string{
			"run", "-d",
			"--name", name,
			"--label", "cell.id=" + opts.CellID,
			"-v", opts.Workspace + ":/workspace",
			"-w", "/workspace",
		}
		if network != "" {
			args = append(args, "--network", network)
		}
		for _, m := range opts.CacheMounts {
			if m.HostAbs == "" || m.Container == "" {
				continue
			}
			args = append(args, "-v", m.HostAbs+":"+m.Container)
		}
		args = append(args, limitArgs(opts.Limits)...)
		for k, v := range opts.Env {
			args = append(args, "-e", k+"="+v)
		}
		args = append(args, opts.Image, "sleep", "infinity")

		cmd := exec.CommandContext(ctx, "docker", args...)
		cmd.Env = os.Environ()
		var stdout, stderr bytes.Buffer
		cmd.Stdout = &stdout
		cmd.Stderr = &stderr
		if err := cmd.Run(); err != nil {
			return fmt.Errorf("runtime/docker: docker run: %w\n%s", err, stderr.String())
		}
		id := lastLine(stdout.String())
		r.mu.Lock()
		r.cells[opts.CellID] = &cellRuntime{
			mode:      modeDocker,
			container: firstNonEmpty(id, name),
			workspace: opts.Workspace,
			env:       copyEnv(opts.Env),
		}
		r.mu.Unlock()
		return nil
	}

	if network != "" {
		return fmt.Errorf("runtime/docker: network %q cannot be enforced on host fallback", network)
	}

	log.Printf("runtime/docker: cell %s using host fallback", opts.CellID)
	hostEnv := mergeEnv(opts.Env, cacheHostEnv(opts))
	r.mu.Lock()
	r.cells[opts.CellID] = &cellRuntime{
		mode:      modeHost,
		workspace: opts.Workspace,
		env:       hostEnv,
	}
	r.mu.Unlock()
	return nil
}

// Exec runs a command in the container or on the host in the workspace.
func (r *Runtime) Exec(ctx context.Context, opts runtime.ExecOpts) (*api.ExecResult, error) {
	r.mu.Lock()
	sr, ok := r.cells[opts.CellID]
	if !ok {
		r.mu.Unlock()
		return nil, fmt.Errorf("runtime/docker: cell %s not found", opts.CellID)
	}
	// Copy fields under lock; do not retain pointer across unlock for mutation safety.
	mode := sr.mode
	container := sr.container
	workspace := sr.workspace
	env := copyEnv(sr.env)
	r.mu.Unlock()

	if opts.Command == "" {
		return &api.ExecResult{ExitCode: 126, Stderr: "runtime/docker: empty command"}, nil
	}

	timeout := time.Duration(opts.TimeoutSeconds) * time.Second
	if timeout <= 0 {
		timeout = 60 * time.Second
	}
	execCtx, cancel := context.WithTimeout(ctx, timeout)
	defer cancel()

	secs := int(timeout.Seconds())
	if secs < 1 {
		secs = 1
	}
	marker := fmt.Sprintf("cell-exec-%d", time.Now().UnixNano())

	var cmd *exec.Cmd
	if mode == modeDocker {
		// Enforce the deadline inside the container so killing the local docker
		// client cannot leave the workload running past timeout_seconds.
		wrapped := fmt.Sprintf(
			"if command -v timeout >/dev/null 2>&1; then exec timeout -k 2s %ds sh -c %s; else exec sh -c %s; fi # %s",
			secs, shellQuote(opts.Command), shellQuote(opts.Command), marker,
		)
		cmd = exec.CommandContext(execCtx, "docker", "exec", container, "sh", "-c", wrapped)
		cmd.Env = os.Environ()
	} else {
		cmd = exec.CommandContext(execCtx, "sh", "-c", opts.Command)
		ws := opts.Workspace
		if ws == "" {
			ws = workspace
		}
		if ws != "" {
			cmd.Dir = ws
		}
		cmd.Env = mergeEnviron(os.Environ(), env, opts.Env)
		cmd.SysProcAttr = &syscall.SysProcAttr{Setpgid: true}
	}

	ae := &activeExec{
		cancel:    cancel,
		cmd:       cmd,
		mode:      mode,
		container: container,
		marker:    marker,
		done:      make(chan struct{}),
	}
	execID := r.trackExec(opts.CellID, ae)
	defer func() {
		r.untrackExec(opts.CellID, execID)
		close(ae.done)
	}()

	var stdout, stderr bytes.Buffer
	cmd.Stdout = &stdout
	cmd.Stderr = &stderr
	err := cmd.Run()

	if execCtx.Err() != nil {
		killActiveExec(ae)
	}

	result := &api.ExecResult{
		Stdout: stdout.String(),
		Stderr: stderr.String(),
	}
	if execCtx.Err() == context.DeadlineExceeded {
		result.ExitCode = 124
		if result.Stderr == "" {
			result.Stderr = "runtime/docker: command timed out"
		}
		return result, nil
	}
	if err != nil {
		if ee, ok := err.(*exec.ExitError); ok {
			result.ExitCode = ee.ExitCode()
			return result, nil
		}
		result.ExitCode = 126
		result.Stderr = fmt.Sprintf("runtime/docker: %v", err)
		return result, nil
	}
	return result, nil
}

// Destroy stops active executions, removes the container (if any), then forgets the cell.
func (r *Runtime) Destroy(ctx context.Context, cellID string) error {
	r.mu.Lock()
	sr, ok := r.cells[cellID]
	execs := r.execs[cellID]
	r.mu.Unlock()
	if !ok {
		return nil
	}

	for _, ae := range copyExecMap(execs) {
		ae.cancel()
		killActiveExec(ae)
		select {
		case <-ae.done:
		case <-time.After(5 * time.Second):
		case <-ctx.Done():
		}
	}

	if sr.mode == modeDocker && sr.container != "" {
		cmd := exec.CommandContext(ctx, "docker", "rm", "-f", sr.container)
		cmd.Env = os.Environ()
		out, err := cmd.CombinedOutput()
		if err != nil {
			return fmt.Errorf("runtime/docker: docker rm -f: %w\n%s", err, out)
		}
	}

	r.mu.Lock()
	delete(r.cells, cellID)
	delete(r.execs, cellID)
	r.mu.Unlock()
	return nil
}

func (r *Runtime) trackExec(cellID string, ae *activeExec) uint64 {
	r.mu.Lock()
	defer r.mu.Unlock()
	r.nextExec++
	id := r.nextExec
	if r.execs[cellID] == nil {
		r.execs[cellID] = make(map[uint64]*activeExec)
	}
	r.execs[cellID][id] = ae
	return id
}

func (r *Runtime) untrackExec(cellID string, id uint64) {
	r.mu.Lock()
	defer r.mu.Unlock()
	if m := r.execs[cellID]; m != nil {
		delete(m, id)
		if len(m) == 0 {
			delete(r.execs, cellID)
		}
	}
}

func copyExecMap(in map[uint64]*activeExec) []*activeExec {
	out := make([]*activeExec, 0, len(in))
	for _, ae := range in {
		out = append(out, ae)
	}
	return out
}

func killActiveExec(ae *activeExec) {
	if ae == nil {
		return
	}
	if ae.mode == modeHost && ae.cmd != nil && ae.cmd.Process != nil {
		_ = syscall.Kill(-ae.cmd.Process.Pid, syscall.SIGKILL)
		return
	}
	if ae.mode == modeDocker && ae.container != "" && ae.marker != "" {
		ctx, cancel := context.WithTimeout(context.Background(), 5*time.Second)
		defer cancel()
		// Best-effort: kill the in-container shell tagged with our marker.
		kill := exec.CommandContext(ctx, "docker", "exec", ae.container, "sh", "-c",
			"pkill -9 -f "+shellQuote(ae.marker)+" 2>/dev/null || true")
		kill.Env = os.Environ()
		_ = kill.Run()
	}
}

// normalizeNetwork maps template policy to a Docker --network value.
//
// Policies:
//   - "" / default / open / bridge → normal Docker egress (agent Cells)
//   - none → --network none (air gap; embedded offline builds)
//   - allowlist → refused until an egress proxy exists (never silently open)
func normalizeNetwork(n string) (string, error) {
	n = strings.TrimSpace(strings.ToLower(n))
	switch n {
	case "", "default", "open", "bridge":
		return "", nil
	case "none":
		return "none", nil
	case "allowlist":
		return "", fmt.Errorf("runtime/docker: network allowlist is not implemented yet (refusing open egress; use default or none)")
	default:
		return "", fmt.Errorf("runtime/docker: unsupported network mode %q (want default|none|allowlist)", n)
	}
}

func shellQuote(s string) string {
	return "'" + strings.ReplaceAll(s, "'", `'"'"'`) + "'"
}

func containerName(cellID string) string {
	safe := strings.ReplaceAll(cellID, "/", "-")
	return "cell-" + safe
}

func limitArgs(limits api.Limits) []string {
	var args []string
	if limits.CPU != "" {
		args = append(args, "--cpus", limits.CPU)
	}
	if limits.Memory != "" {
		args = append(args, "--memory", dockerMemory(limits.Memory))
	}
	return args
}

func dockerMemory(m string) string {
	m = strings.TrimSpace(m)
	switch {
	case strings.HasSuffix(m, "Gi"):
		return strings.TrimSuffix(m, "Gi") + "g"
	case strings.HasSuffix(m, "Mi"):
		return strings.TrimSuffix(m, "Mi") + "m"
	case strings.HasSuffix(m, "Ki"):
		return strings.TrimSuffix(m, "Ki") + "k"
	case strings.HasSuffix(m, "G"):
		return strings.TrimSuffix(m, "G") + "g"
	case strings.HasSuffix(m, "M"):
		return strings.TrimSuffix(m, "M") + "m"
	default:
		return m
	}
}

func lastLine(s string) string {
	s = strings.TrimSpace(s)
	if s == "" {
		return ""
	}
	lines := strings.Split(s, "\n")
	return strings.TrimSpace(lines[len(lines)-1])
}

func firstNonEmpty(a, b string) string {
	if a != "" {
		return a
	}
	return b
}

func cacheHostEnv(opts runtime.CreateOpts) map[string]string {
	return cachepkg.HostEnv(opts.CacheMounts)
}

func copyEnv(in map[string]string) map[string]string {
	if len(in) == 0 {
		return nil
	}
	out := make(map[string]string, len(in))
	for k, v := range in {
		out[k] = v
	}
	return out
}

func mergeEnv(maps ...map[string]string) map[string]string {
	out := make(map[string]string)
	for _, m := range maps {
		for k, v := range m {
			if k == "" {
				continue
			}
			out[k] = v
		}
	}
	if len(out) == 0 {
		return nil
	}
	return out
}

func mergeEnviron(base []string, overlays ...map[string]string) []string {
	envMap := make(map[string]string, len(base))
	order := make([]string, 0, len(base))
	for _, kv := range base {
		i := strings.IndexByte(kv, '=')
		if i <= 0 {
			continue
		}
		k, v := kv[:i], kv[i+1:]
		if _, ok := envMap[k]; !ok {
			order = append(order, k)
		}
		envMap[k] = v
	}
	for _, m := range overlays {
		for k, v := range m {
			if k == "" {
				continue
			}
			if _, ok := envMap[k]; !ok {
				order = append(order, k)
			}
			envMap[k] = v
		}
	}
	out := make([]string, 0, len(order))
	for _, k := range order {
		out = append(out, k+"="+envMap[k])
	}
	return out
}

// Stub never calls Docker. Create/Destroy are no-ops; Exec runs on the host.
type Stub struct{}

// NewStub returns a host-only runtime stub.
func NewStub() *Stub { return &Stub{} }

// Create rejects network policies the host cannot enforce.
func (s *Stub) Create(ctx context.Context, opts runtime.CreateOpts) error {
	_ = ctx
	if _, err := normalizeNetwork(opts.Network); err != nil {
		return err
	}
	network, _ := normalizeNetwork(opts.Network)
	if network != "" {
		return fmt.Errorf("runtime stub: network %q cannot be enforced on host", network)
	}
	return nil
}

// Exec runs `sh -c` in the workspace on the host with process-group kill.
func (s *Stub) Exec(ctx context.Context, opts runtime.ExecOpts) (*api.ExecResult, error) {
	if opts.Command == "" {
		return &api.ExecResult{ExitCode: 126, Stderr: "runtime stub: empty command"}, nil
	}
	timeout := time.Duration(opts.TimeoutSeconds) * time.Second
	if timeout <= 0 {
		timeout = 60 * time.Second
	}
	execCtx, cancel := context.WithTimeout(ctx, timeout)
	defer cancel()

	cmd := exec.CommandContext(execCtx, "sh", "-c", opts.Command)
	if opts.Workspace != "" {
		cmd.Dir = opts.Workspace
	}
	if len(opts.Env) > 0 {
		cmd.Env = mergeEnviron(os.Environ(), opts.Env)
	}
	cmd.SysProcAttr = &syscall.SysProcAttr{Setpgid: true}
	var stdout, stderr bytes.Buffer
	cmd.Stdout = &stdout
	cmd.Stderr = &stderr
	err := cmd.Run()
	if execCtx.Err() != nil && cmd.Process != nil {
		_ = syscall.Kill(-cmd.Process.Pid, syscall.SIGKILL)
	}
	result := &api.ExecResult{Stdout: stdout.String(), Stderr: stderr.String()}
	if execCtx.Err() == context.DeadlineExceeded {
		result.ExitCode = 124
		if result.Stderr == "" {
			result.Stderr = "runtime stub: command timed out"
		}
		return result, nil
	}
	if err != nil {
		if ee, ok := err.(*exec.ExitError); ok {
			result.ExitCode = ee.ExitCode()
			return result, nil
		}
		result.ExitCode = 126
		result.Stderr = fmt.Sprintf("runtime stub: %v", err)
		return result, nil
	}
	return result, nil
}

// Destroy is a no-op.
func (s *Stub) Destroy(ctx context.Context, cellID string) error {
	_ = ctx
	_ = cellID
	return nil
}

var _ runtime.Runtime = (*Runtime)(nil)
var _ runtime.Runtime = (*Stub)(nil)
