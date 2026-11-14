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
	cfg       Config
	opts      runtime.CreateOpts
	cmd       *exec.Cmd
	apiSock   string
	vsockUDS  string
	workDir   string
	cid       uint32
	client    *http.Client
	kernelAPI string
	rootfsAPI string
	netMode   string
	allowlist *allowlistNet
	jailRoot  string
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
	netMode, err := validateNetworkOpts(opts)
	if err != nil {
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
	vsockUDS := filepath.Join(workDir, "vsock.sock")
	_ = os.Remove(vsockUDS)

	var allow *allowlistNet
	if netMode == networkAllowlist {
		allow, err = setupAllowlist(ctx, opts)
		if err != nil {
			_ = os.RemoveAll(workDir)
			return err
		}
	}

	launch, err := prepareVMM(cfg, cellID, workDir, vsockUDS)
	if err != nil {
		_ = allow.cleanup(context.Background())
		_ = os.RemoveAll(workDir)
		return err
	}
	cmd := launch.Cmd
	cmd.Stdout = os.Stderr
	cmd.Stderr = os.Stderr
	if err := cmd.Start(); err != nil {
		_ = allow.cleanup(context.Background())
		_ = os.RemoveAll(workDir)
		return fmt.Errorf("microvm: start firecracker: %w", err)
	}

	apiSock := launch.APISock
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
		kernelAPI: launch.KernelAPI, rootfsAPI: launch.RootfsAPI,
		netMode: netMode, allowlist: allow, jailRoot: launch.JailRoot,
	}
	fail := func(err error) error {
		_ = cmd.Process.Kill()
		_, _ = cmd.Process.Wait()
		_ = allow.cleanup(context.Background())
		_ = os.RemoveAll(workDir)
		if launch.JailRoot != "" {
			_ = os.RemoveAll(launch.JailRoot)
		}
		return err
	}
	if err := cell.waitAPISock(ctx); err != nil {
		return fail(err)
	}
	if err := cell.configure(ctx); err != nil {
		return fail(err)
	}
	if err := cell.putJSON(ctx, http.MethodPut, "http://localhost/actions", map[string]string{
		"action_type": "InstanceStart",
	}); err != nil {
		return fail(fmt.Errorf("microvm: InstanceStart: %w", err))
	}
	if err := cell.waitAgent(ctx); err != nil {
		return fail(err)
	}
	if allow != nil {
		// Belt-and-suspenders: agent also applies cell.nameserver= from cmdline.
		if err := cell.applyGuestNameserver(ctx, allow.hostIP); err != nil {
			return fail(err)
		}
	}
	if opts.Workspace != "" {
		if err := cell.syncWorkspace(ctx, opts.Workspace); err != nil {
			return fail(err)
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
	env := map[string]string{}
	if cell.allowlist != nil {
		for k, v := range cell.allowlist.proxyEnv {
			env[k] = v
		}
	}
	for k, v := range opts.Env {
		env[k] = v
	}
	req := guestproto.Request{
		Op:      guestproto.OpExec,
		Command: opts.Command,
		Timeout: timeout,
		Env:     env,
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

// Resync re-uploads the host workspace into a booted guest.
func (f *Firecracker) Resync(ctx context.Context, cellID, workspace string) error {
	f.mu.Lock()
	cell := f.cells[cellID]
	f.mu.Unlock()
	if cell == nil {
		return fmt.Errorf("microvm: unknown cell %q", cellID)
	}
	if workspace == "" {
		workspace = cell.opts.Workspace
	}
	return cell.syncWorkspace(ctx, workspace)
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
	_ = cell.allowlist.cleanup(ctx)
	_ = os.RemoveAll(cell.workDir)
	if cell.jailRoot != "" {
		_ = os.RemoveAll(cell.jailRoot)
	}
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
	if c.allowlist != nil {
		if ip := c.allowlist.bootIPArg(); ip != "" {
			bootArgs = bootArgs + " " + ip
		}
	}
	kernel := c.kernelAPI
	if kernel == "" {
		kernel = c.cfg.Kernel
	}
	rootfs := c.rootfsAPI
	if rootfs == "" {
		rootfs = c.cfg.Rootfs
	}
	if err := c.putJSON(ctx, http.MethodPut, "http://localhost/boot-source", map[string]any{
		"kernel_image_path": kernel,
		"boot_args":         bootArgs,
	}); err != nil {
		return fmt.Errorf("boot-source: %w", err)
	}
	// M3: rootfs is read-only; guest agent mounts tmpfs on /workspace and /tmp.
	if err := c.putJSON(ctx, http.MethodPut, "http://localhost/drives/rootfs", map[string]any{
		"drive_id":       "rootfs",
		"path_on_host":   rootfs,
		"is_root_device": true,
		"is_read_only":   true,
	}); err != nil {
		return fmt.Errorf("drives/rootfs: %w", err)
	}
	if c.allowlist != nil {
		if err := c.putJSON(ctx, http.MethodPut, "http://localhost/network-interfaces/eth0", map[string]any{
			"iface_id":      "eth0",
			"guest_mac":     c.allowlist.guestMAC,
			"host_dev_name": c.allowlist.tapName,
		}); err != nil {
			return fmt.Errorf("network-interfaces/eth0: %w", err)
		}
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
