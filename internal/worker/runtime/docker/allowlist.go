package docker

import (
	"context"
	"fmt"
	"log"
	"os"
	"os/exec"
	"path/filepath"
	"strings"
	"time"

	"github.com/YanChao1999/Cell/internal/worker/runtime"
)

// startAllowlistEgress creates an internal Docker network and a proxy sidecar so
// the Cell has no direct internet — only HTTP(S) via cell-egress.
//
// Layout:
//
//	internal network ← Cell + proxy (alias cell-egress)
//	bridge         ← proxy only (dial-out)
func startAllowlistEgress(ctx context.Context, opts runtime.CreateOpts) (netName, proxyName, auditHost string, proxyEnv map[string]string, err error) {
	if len(opts.EgressAllow) == 0 {
		return "", "", "", nil, fmt.Errorf("runtime/docker: network allowlist requires egress.allow hosts")
	}
	bin, err := lookPathEgress()
	if err != nil {
		return "", "", "", nil, err
	}
	safe := strings.ReplaceAll(opts.CellID, "/", "-")
	netName = "cell-eg-" + safe
	proxyName = "cell-proxy-" + safe
	if opts.Workspace == "" {
		return "", "", "", nil, fmt.Errorf("runtime/docker: workspace required for allowlist audit log")
	}
	auditDir := filepath.Join(opts.Workspace, ".cell")
	if err := os.MkdirAll(auditDir, 0o755); err != nil {
		return "", "", "", nil, err
	}
	auditHost = filepath.Join(auditDir, "egress-audit.jsonl")
	_ = os.WriteFile(auditHost, nil, 0o600)

	if out, err := dockerOutput(ctx, "network", "create", "--internal", "--label", "cell.id="+opts.CellID, netName); err != nil {
		return "", "", "", nil, fmt.Errorf("runtime/docker: network create: %w\n%s", err, out)
	}

	args := []string{
		"run", "-d",
		"--name", proxyName,
		"--label", "cell.id=" + opts.CellID,
		"--label", "cell.role=egress-proxy",
		"--network", netName,
		"--network-alias", "cell-egress",
		"--user", fmt.Sprintf("%d:%d", os.Getuid(), os.Getgid()),
		"--security-opt", "no-new-privileges:true",
		"--cap-drop", "ALL",
		"--cap-add", "NET_BIND_SERVICE",
		"--read-only",
		"--tmpfs", "/tmp:rw,nosuid,nodev,size=64m",
		"-v", bin + ":/cell-egress:ro",
		"-v", auditHost + ":/audit.jsonl",
		"--entrypoint", "/cell-egress",
		opts.Image,
		"--listen", ":3128",
		"--audit", "/audit.jsonl",
	}
	for _, h := range opts.EgressAllow {
		args = append(args, "--allow", h)
	}
	if out, err := dockerOutput(ctx, args...); err != nil {
		cleanupAllowlistEgress(context.Background(), "", netName)
		return "", "", "", nil, fmt.Errorf("runtime/docker: egress proxy: %w\n%s", err, out)
	}
	// Give the proxy a path to the internet (Cell stays internal-only).
	if out, err := dockerOutput(ctx, "network", "connect", "bridge", proxyName); err != nil {
		cleanupAllowlistEgress(context.Background(), proxyName, netName)
		return "", "", "", nil, fmt.Errorf("runtime/docker: connect proxy to bridge: %w\n%s", err, out)
	}

	proxyEnv = map[string]string{
		"HTTP_PROXY":  "http://cell-egress:3128",
		"HTTPS_PROXY": "http://cell-egress:3128",
		"http_proxy":  "http://cell-egress:3128",
		"https_proxy": "http://cell-egress:3128",
		"NO_PROXY":    "localhost,127.0.0.1",
		"no_proxy":    "localhost,127.0.0.1",
	}
	log.Printf("runtime/docker: allowlist egress proxy=%s network=%s audit=%s allow=%v",
		proxyName, netName, auditHost, opts.EgressAllow)
	return netName, proxyName, auditHost, proxyEnv, nil
}

func cleanupAllowlistEgress(ctx context.Context, proxyName, netName string) {
	if proxyName != "" {
		_ = dockerRun(ctx, "rm", "-f", proxyName)
	}
	if netName != "" {
		// Retry briefly: docker may need the endpoint to settle after rm.
		for i := 0; i < 5; i++ {
			if err := dockerRun(ctx, "network", "rm", netName); err == nil {
				return
			}
			time.Sleep(100 * time.Millisecond)
		}
	}
}

func lookPathEgress() (string, error) {
	if p := os.Getenv("CELL_EGRESS_BIN"); p != "" {
		if st, err := os.Stat(p); err == nil && !st.IsDir() {
			abs, _ := filepath.Abs(p)
			return abs, nil
		}
		return "", fmt.Errorf("runtime/docker: CELL_EGRESS_BIN=%q not found", p)
	}
	candidates := []string{}
	if exe, err := os.Executable(); err == nil {
		candidates = append(candidates, filepath.Join(filepath.Dir(exe), "cell-egress"))
	}
	if cwd, err := os.Getwd(); err == nil {
		candidates = append(candidates, filepath.Join(cwd, "bin", "cell-egress"))
	}
	candidates = append(candidates, "bin/cell-egress", "cell-egress")
	for _, c := range candidates {
		if st, err := os.Stat(c); err == nil && !st.IsDir() {
			abs, err := filepath.Abs(c)
			if err != nil {
				return c, nil
			}
			return abs, nil
		}
	}
	if p, err := exec.LookPath("cell-egress"); err == nil {
		return p, nil
	}
	return "", fmt.Errorf("runtime/docker: cell-egress binary not found (build with: go build -o bin/cell-egress ./cmd/cell-egress)")
}

func dockerOutput(ctx context.Context, args ...string) ([]byte, error) {
	cmd := exec.CommandContext(ctx, "docker", args...)
	cmd.Env = os.Environ()
	return cmd.CombinedOutput()
}

func dockerRun(ctx context.Context, args ...string) error {
	_, err := dockerOutput(ctx, args...)
	return err
}
