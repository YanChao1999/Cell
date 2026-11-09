package microvm

import (
	"bytes"
	"context"
	"encoding/base64"
	"encoding/json"
	"fmt"
	"io"
	"net"
	"net/http"
	"os"
	"os/exec"
	"path/filepath"
	"sync"
	"time"

	"github.com/YanChao1999/Cell/api"
	"github.com/YanChao1999/Cell/internal/worker/runtime"
	"github.com/YanChao1999/Cell/internal/worker/runtime/microvm/guestproto"
)

// Firecracker implements Backend via the Firecracker HTTP API over a UNIX socket.
type Firecracker struct {
	mu    sync.Mutex
	cells map[string]*fcCell
}

type fcCell struct {
	cfg      Config
	opts     runtime.CreateOpts
	cmd      *exec.Cmd
	apiSock  string
	vsockUDS string
	workDir  string
	cid      uint32
	client   *http.Client
}

// NewFirecracker returns a Firecracker Backend.
func NewFirecracker() *Firecracker {
	return &Firecracker{cells: make(map[string]*fcCell)}
}

func (f *Firecracker) Name() string { return "firecracker" }

func (f *Firecracker) Boot(ctx context.Context, cellID string, opts runtime.CreateOpts, cfg Config) error {
	cfg.ResolveDefaults()
	if err := cfg.Validate(); err != nil {
		return err
	}
	f.mu.Lock()
	defer f.mu.Unlock()
	if _, ok := f.cells[cellID]; ok {
		return fmt.Errorf("microvm: cell %q already booted", cellID)
	}

	workDir, err := os.MkdirTemp(cfg.ArtifactDir, "cell-"+sanitizeID(cellID)+"-")
	if err != nil {
		workDir, err = os.MkdirTemp("", "cell-fc-")
		if err != nil {
			return err
		}
	}
	apiSock := filepath.Join(workDir, "firecracker.sock")
	vsockUDS := filepath.Join(workDir, "vsock.sock")
	_ = os.Remove(apiSock)
	_ = os.Remove(vsockUDS)

	vmmPath, err := lookPath(cfg.VMM)
	if err != nil {
		_ = os.RemoveAll(workDir)
		return err
	}
	cmd := exec.CommandContext(ctx, vmmPath, "--api-sock", apiSock)
	cmd.Stdout = os.Stderr
	cmd.Stderr = os.Stderr
	if err := cmd.Start(); err != nil {
		_ = os.RemoveAll(workDir)
		return fmt.Errorf("microvm: start firecracker: %w", err)
	}

	client := &http.Client{
		Transport: &http.Transport{
			DialContext: func(ctx context.Context, _, _ string) (net.Conn, error) {
				d := net.Dialer{Timeout: 2 * time.Second}
				return d.DialContext(ctx, "unix", apiSock)
			},
		},
		Timeout: 30 * time.Second,
	}

	cell := &fcCell{
		cfg: cfg, opts: opts, cmd: cmd, apiSock: apiSock, vsockUDS: vsockUDS,
		workDir: workDir, cid: 3, client: client,
	}
	if err := cell.waitAPISock(ctx); err != nil {
		_ = cmd.Process.Kill()
		_, _ = cmd.Process.Wait()
		_ = os.RemoveAll(workDir)
		return err
	}
	if err := cell.configure(ctx); err != nil {
		_ = cmd.Process.Kill()
		_, _ = cmd.Process.Wait()
		_ = os.RemoveAll(workDir)
		return err
	}
	if err := cell.putJSON(ctx, http.MethodPut, "http://localhost/actions", map[string]string{
		"action_type": "InstanceStart",
	}); err != nil {
		_ = cmd.Process.Kill()
		_, _ = cmd.Process.Wait()
		_ = os.RemoveAll(workDir)
		return fmt.Errorf("microvm: InstanceStart: %w", err)
	}
	if err := cell.waitAgent(ctx); err != nil {
		_ = cmd.Process.Kill()
		_, _ = cmd.Process.Wait()
		_ = os.RemoveAll(workDir)
		return err
	}
	if opts.Workspace != "" {
		if err := cell.syncWorkspace(ctx, opts.Workspace); err != nil {
			_ = cmd.Process.Kill()
			_, _ = cmd.Process.Wait()
			_ = os.RemoveAll(workDir)
			return err
		}
	}
	f.cells[cellID] = cell
	return nil
}

func (f *Firecracker) Exec(ctx context.Context, opts runtime.ExecOpts) (*api.ExecResult, error) {
	f.mu.Lock()
	cell := f.cells[opts.CellID]
	f.mu.Unlock()
	if cell == nil {
		return nil, fmt.Errorf("microvm: unknown cell %q", opts.CellID)
	}
	timeout := opts.TimeoutSeconds
	if timeout <= 0 {
		timeout = int(guestproto.DefaultExecTimeout / time.Second)
	}
	req := guestproto.Request{
		Op:      guestproto.OpExec,
		Command: opts.Command,
		Timeout: timeout,
		Env:     opts.Env,
		Cwd:     "/workspace",
	}
	resp, err := cell.roundTrip(ctx, req, time.Duration(timeout)*time.Second+10*time.Second)
	if err != nil {
		return nil, err
	}
	if !resp.OK && resp.ExitCode == 0 && resp.Error != "" {
		return nil, fmt.Errorf("microvm: guest exec: %s", resp.Error)
	}
	return &api.ExecResult{
		ExitCode: resp.ExitCode,
		Stdout:   resp.Stdout,
		Stderr:   resp.Stderr,
	}, nil
}

func (f *Firecracker) Halt(ctx context.Context, cellID string) error {
	f.mu.Lock()
	cell := f.cells[cellID]
	delete(f.cells, cellID)
	f.mu.Unlock()
	if cell == nil {
		return nil
	}
	// Best-effort graceful shutdown via agent, then Kill.
	_, _ = cell.roundTrip(ctx, guestproto.Request{Op: guestproto.OpShutdown}, 3*time.Second)
	_ = cell.putJSON(ctx, http.MethodPut, "http://localhost/actions", map[string]string{
		"action_type": "SendCtrlAltDel",
	})
	done := make(chan error, 1)
	go func() { done <- cell.cmd.Wait() }()
	select {
	case <-done:
	case <-time.After(3 * time.Second):
		_ = cell.cmd.Process.Kill()
		<-done
	case <-ctx.Done():
		_ = cell.cmd.Process.Kill()
		<-done
	}
	_ = os.RemoveAll(cell.workDir)
	return nil
}

func (c *fcCell) waitAPISock(ctx context.Context) error {
	deadline := time.Now().Add(10 * time.Second)
	for time.Now().Before(deadline) {
		if err := ctx.Err(); err != nil {
			return err
		}
		if st, err := os.Stat(c.apiSock); err == nil && !st.IsDir() {
			// Give firecracker a beat to accept.
			time.Sleep(50 * time.Millisecond)
			return nil
		}
		time.Sleep(25 * time.Millisecond)
	}
	return fmt.Errorf("microvm: api sock %q not ready", c.apiSock)
}

func (c *fcCell) configure(ctx context.Context) error {
	mem := c.cfg.MemSizeMiB
	if mem <= 0 {
		mem = 256
	}
	vcpu := c.cfg.VCPUCount
	if vcpu <= 0 {
		vcpu = 1
	}
	if err := c.putJSON(ctx, http.MethodPut, "http://localhost/machine-config", map[string]any{
		"vcpu_count":   vcpu,
		"mem_size_mib": mem,
	}); err != nil {
		return fmt.Errorf("machine-config: %w", err)
	}
	bootArgs := c.cfg.KernelArgs
	if bootArgs == "" {
		bootArgs = "console=ttyS0 reboot=k panic=1 pci=off init=/usr/local/bin/cell-guest-agent"
	}
	if err := c.putJSON(ctx, http.MethodPut, "http://localhost/boot-source", map[string]any{
		"kernel_image_path": c.cfg.Kernel,
		"boot_args":         bootArgs,
	}); err != nil {
		return fmt.Errorf("boot-source: %w", err)
	}
	if err := c.putJSON(ctx, http.MethodPut, "http://localhost/drives/rootfs", map[string]any{
		"drive_id":       "rootfs",
		"path_on_host":   c.cfg.Rootfs,
		"is_root_device": true,
		"is_read_only":   false,
	}); err != nil {
		return fmt.Errorf("drives/rootfs: %w", err)
	}
	if err := c.putJSON(ctx, http.MethodPut, "http://localhost/vsock", map[string]any{
		"guest_cid": c.cid,
		"uds_path":  c.vsockUDS,
	}); err != nil {
		return fmt.Errorf("vsock: %w", err)
	}
	return nil
}

func (c *fcCell) putJSON(ctx context.Context, method, url string, body any) error {
	var rdr io.Reader
	if body != nil {
		b, err := json.Marshal(body)
		if err != nil {
			return err
		}
		rdr = bytes.NewReader(b)
	}
	req, err := http.NewRequestWithContext(ctx, method, url, rdr)
	if err != nil {
		return err
	}
	req.Header.Set("Content-Type", "application/json")
	res, err := c.client.Do(req)
	if err != nil {
		return err
	}
	defer func() { _ = res.Body.Close() }()
	if res.StatusCode >= 300 {
		msg, _ := io.ReadAll(io.LimitReader(res.Body, 4096))
		return fmt.Errorf("firecracker %s %s: %s (%s)", method, url, res.Status, bytes.TrimSpace(msg))
	}
	return nil
}

func (c *fcCell) waitAgent(ctx context.Context) error {
	deadline := time.Now().Add(30 * time.Second)
	var last error
	for time.Now().Before(deadline) {
		if err := ctx.Err(); err != nil {
			return err
		}
		resp, err := c.roundTrip(ctx, guestproto.Request{Op: guestproto.OpPing}, 2*time.Second)
		if err == nil && resp.OK {
			return nil
		}
		last = err
		if err == nil {
			last = fmt.Errorf("ping not ok: %s", resp.Error)
		}
		time.Sleep(200 * time.Millisecond)
	}
	if last == nil {
		last = fmt.Errorf("timeout")
	}
	return fmt.Errorf("microvm: guest agent not ready: %w", last)
}

func (c *fcCell) syncWorkspace(ctx context.Context, workspace string) error {
	var buf bytes.Buffer
	if err := tarGzDir(workspace, &buf); err != nil {
		return fmt.Errorf("microvm: pack workspace: %w", err)
	}
	req := guestproto.Request{
		Op:  guestproto.OpSync,
		Tar: base64.StdEncoding.EncodeToString(buf.Bytes()),
	}
	resp, err := c.roundTrip(ctx, req, 60*time.Second)
	if err != nil {
		return err
	}
	if !resp.OK {
		return fmt.Errorf("microvm: sync workspace: %s", resp.Error)
	}
	return nil
}

func (c *fcCell) roundTrip(ctx context.Context, req guestproto.Request, timeout time.Duration) (*guestproto.Response, error) {
	port := c.cfg.VsockPort
	if port == 0 {
		port = guestproto.DefaultPort
	}
	conn, err := dialHostVsock(c.vsockUDS, port, timeout)
	if err != nil {
		return nil, err
	}
	defer func() { _ = conn.Close() }()
	_ = conn.SetDeadline(time.Now().Add(timeout))
	if err := writeFrame(conn, req); err != nil {
		return nil, err
	}
	var resp guestproto.Response
	if err := readFrame(conn, &resp); err != nil {
		return nil, err
	}
	select {
	case <-ctx.Done():
		return nil, ctx.Err()
	default:
		return &resp, nil
	}
}

func sanitizeID(id string) string {
	b := make([]byte, 0, len(id))
	for i := 0; i < len(id); i++ {
		c := id[i]
		if (c >= 'a' && c <= 'z') || (c >= 'A' && c <= 'Z') || (c >= '0' && c <= '9') || c == '-' {
			b = append(b, c)
		} else {
			b = append(b, '-')
		}
	}
	if len(b) > 32 {
		b = b[:32]
	}
	return string(b)
}
