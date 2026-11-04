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
	"time"

	"github.com/YanChao1999/spark/api"
	cachepkg "github.com/YanChao1999/spark/internal/worker/cache"
	"github.com/YanChao1999/spark/internal/worker/runtime"
)

type mode int

const (
	modeHost mode = iota
	modeDocker
)

type sparkRuntime struct {
	mode      mode
	container string // docker container name/id when modeDocker
	workspace string
	env       map[string]string // host-fallback / extra exec env (cache paths)
}

// Runtime uses Docker when the CLI and daemon are available; otherwise it
// falls back to host `sh -c` in the workspace (Phase 1 behavior).
type Runtime struct {
	useDocker bool
	mu        sync.Mutex
	sparks    map[string]*sparkRuntime
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
		sparks:    make(map[string]*sparkRuntime),
	}
}

// Available reports whether Docker was detected at construction time.
func (r *Runtime) Available() bool { return r.useDocker }

func dockerAvailable() bool {
	if _, err := exec.LookPath("docker"); err != nil {
		return false
	}
	// Daemon / DOCKER_HOST must respond.
	ctx, cancel := context.WithTimeout(context.Background(), 3*time.Second)
	defer cancel()
	cmd := exec.CommandContext(ctx, "docker", "info")
	cmd.Env = os.Environ()
	if err := cmd.Run(); err != nil {
		return false
	}
	return true
}

// Create starts a long-lived container (docker) or records a host fallback.
func (r *Runtime) Create(ctx context.Context, opts runtime.CreateOpts) error {
	r.mu.Lock()
	defer r.mu.Unlock()

	if r.useDocker && opts.Image != "" {
		name := containerName(opts.SparkID)
		args := []string{
			"run", "-d",
			"--name", name,
			"--label", "spark.id=" + opts.SparkID,
			"-v", opts.Workspace + ":/workspace",
			"-w", "/workspace",
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
		// Keep container alive for subsequent docker exec.
		args = append(args, opts.Image, "sleep", "infinity")

		cmd := exec.CommandContext(ctx, "docker", args...)
		cmd.Env = os.Environ()
		out, err := cmd.CombinedOutput()
		if err != nil {
			return fmt.Errorf("runtime/docker: docker run: %w\n%s", err, out)
		}
		id := strings.TrimSpace(string(out))
		r.sparks[opts.SparkID] = &sparkRuntime{
			mode:      modeDocker,
			container: firstNonEmpty(id, name),
			workspace: opts.Workspace,
			env:       copyEnv(opts.Env),
		}
		return nil
	}

	if r.useDocker && opts.Image == "" {
		log.Printf("runtime/docker: spark %s has no image; using host fallback", opts.SparkID)
	} else if !r.useDocker {
		log.Printf("runtime/docker: spark %s using host fallback", opts.SparkID)
	}

	// Host fallback: point tool caches at durable host dirs via env.
	hostEnv := mergeEnv(opts.Env, cacheHostEnv(opts))
	r.sparks[opts.SparkID] = &sparkRuntime{
		mode:      modeHost,
		workspace: opts.Workspace,
		env:       hostEnv,
	}
	return nil
}

// Exec runs a command in the container or on the host in the workspace.
func (r *Runtime) Exec(ctx context.Context, opts runtime.ExecOpts) (*api.ExecResult, error) {
	r.mu.Lock()
	sr, ok := r.sparks[opts.SparkID]
	r.mu.Unlock()
	if !ok {
		// Allow exec if Create was skipped somehow: host fallback.
		sr = &sparkRuntime{mode: modeHost, workspace: opts.Workspace}
	}

	if opts.Command == "" {
		return &api.ExecResult{ExitCode: 126, Stderr: "runtime/docker: empty command"}, nil
	}

	timeout := time.Duration(opts.TimeoutSeconds) * time.Second
	if timeout <= 0 {
		timeout = 60 * time.Second
	}
	execCtx, cancel := context.WithTimeout(ctx, timeout)
	defer cancel()

	var cmd *exec.Cmd
	if sr.mode == modeDocker {
		cmd = exec.CommandContext(execCtx, "docker", "exec", sr.container, "sh", "-c", opts.Command)
		cmd.Env = os.Environ()
	} else {
		cmd = exec.CommandContext(execCtx, "sh", "-c", opts.Command)
		ws := opts.Workspace
		if ws == "" {
			ws = sr.workspace
		}
		if ws != "" {
			cmd.Dir = ws
		}
		cmd.Env = mergeEnviron(os.Environ(), sr.env, opts.Env)
	}

	var stdout, stderr bytes.Buffer
	cmd.Stdout = &stdout
	cmd.Stderr = &stderr
	err := cmd.Run()
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

// Destroy removes the container (if any) and forgets the spark.
func (r *Runtime) Destroy(ctx context.Context, sparkID string) error {
	r.mu.Lock()
	sr, ok := r.sparks[sparkID]
	if ok {
		delete(r.sparks, sparkID)
	}
	r.mu.Unlock()

	if ok && sr.mode == modeDocker && sr.container != "" {
		cmd := exec.CommandContext(ctx, "docker", "rm", "-f", sr.container)
		cmd.Env = os.Environ()
		out, err := cmd.CombinedOutput()
		if err != nil {
			return fmt.Errorf("runtime/docker: docker rm -f: %w\n%s", err, out)
		}
	}
	return nil
}

func containerName(sparkID string) string {
	// Docker names: [a-zA-Z0-9][a-zA-Z0-9_.-]
	safe := strings.ReplaceAll(sparkID, "/", "-")
	return "spark-" + safe
}

func limitArgs(limits api.Limits) []string {
	var args []string
	if limits.CPU != "" {
		args = append(args, "--cpus", limits.CPU)
	}
	if limits.Memory != "" {
		args = append(args, "--memory", dockerMemory(limits.Memory))
	}
	// Disk quotas are backend-specific; skip for MVP docker run.
	return args
}

// dockerMemory maps K8s-style sizes (4Gi, 512Mi) to docker --memory values.
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

// Create is a no-op.
func (s *Stub) Create(ctx context.Context, opts runtime.CreateOpts) error {
	_ = ctx
	_ = opts
	return nil
}

// Exec runs `sh -c` in the workspace on the host.
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
	var stdout, stderr bytes.Buffer
	cmd.Stdout = &stdout
	cmd.Stderr = &stderr
	err := cmd.Run()
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
func (s *Stub) Destroy(ctx context.Context, sparkID string) error {
	_ = ctx
	_ = sparkID
	return nil
}

var _ runtime.Runtime = (*Runtime)(nil)
var _ runtime.Runtime = (*Stub)(nil)
