package docker

import (
	"context"
	"os"
	"path/filepath"
	"strings"
	"testing"
	"time"

	"github.com/YanChao1999/Cell/internal/worker/runtime"
)

// These tests lock the security invariants: when Docker is enabled, Cells must
// never silently execute on the host.

func TestHostEscape_EmptyImageRejectedWhenDockerOn(t *testing.T) {
	if os.Getenv("CELL_RUNTIME") == "host" {
		t.Skip("CELL_RUNTIME=host")
	}
	rt := New()
	if !rt.Available() {
		t.Skip("docker not available")
	}
	err := rt.Create(context.Background(), runtime.CreateOpts{
		CellID:    "escape-empty-image",
		Workspace: t.TempDir(),
		Image:     "",
	})
	if err == nil {
		t.Fatal("expected reject empty image when Docker enabled")
	}
	if !strings.Contains(err.Error(), "host escape") && !strings.Contains(err.Error(), "image required") {
		t.Fatalf("unexpected error: %v", err)
	}
}

func TestHostEscape_MissingRuntimeNoHostExec(t *testing.T) {
	rt := New()
	ws := t.TempDir()
	marker := filepath.Join(ws, "HOST_ESCAPED")
	_, err := rt.Exec(context.Background(), runtime.ExecOpts{
		CellID:         "does-not-exist",
		Workspace:      ws,
		Command:        "echo pwned > HOST_ESCAPED",
		TimeoutSeconds: 5,
	})
	if err == nil {
		t.Fatal("expected not-found error")
	}
	if !strings.Contains(err.Error(), "not found") {
		t.Fatalf("expected not found, got: %v", err)
	}
	if _, statErr := os.Stat(marker); statErr == nil {
		t.Fatal("HOST ESCAPE: command ran on host for missing cell")
	}
}

func TestHostEscape_NetworkNoneRejectedOnHostMode(t *testing.T) {
	t.Setenv("CELL_RUNTIME", "host")
	rt := New()
	if rt.Available() {
		t.Fatal("CELL_RUNTIME=host should disable docker")
	}
	err := rt.Create(context.Background(), runtime.CreateOpts{
		CellID:    "escape-none-host",
		Workspace: t.TempDir(),
		Network:   "none",
	})
	if err == nil {
		t.Fatal("host fallback must reject network none")
	}
}

func TestHostEscape_AllowlistFailClosed(t *testing.T) {
	t.Setenv("CELL_RUNTIME", "")
	_ = os.Unsetenv("CELL_RUNTIME")
	rt := New()
	if !rt.Available() {
		t.Skip("docker not available")
	}
	err := rt.Create(context.Background(), runtime.CreateOpts{
		CellID:      "escape-allowlist",
		Workspace:   t.TempDir(),
		Image:       "golang:1.23-bookworm",
		Network:     "allowlist",
		EgressAllow: []string{"example.com"},
	})
	if err == nil {
		_ = rt.Destroy(context.Background(), "escape-allowlist")
		t.Fatal("allowlist must fail closed")
	}
	if !strings.Contains(err.Error(), "allowlist") {
		t.Fatalf("unexpected: %v", err)
	}
}

func TestHostEscape_DestroyThenExecNoHost(t *testing.T) {
	_ = os.Unsetenv("CELL_RUNTIME")
	rt := New()
	if !rt.Available() {
		t.Skip("docker not available")
	}
	ws := t.TempDir()
	id := "escape-after-destroy"
	ctx, cancel := context.WithTimeout(context.Background(), 2*time.Minute)
	defer cancel()
	if err := rt.Create(ctx, runtime.CreateOpts{
		CellID:    id,
		Workspace: ws,
		Image:     "golang:1.23-bookworm",
	}); err != nil {
		t.Fatal(err)
	}
	if err := rt.Destroy(ctx, id); err != nil {
		t.Fatal(err)
	}
	marker := filepath.Join(ws, "AFTER_DESTROY")
	_, err := rt.Exec(ctx, runtime.ExecOpts{
		CellID:         id,
		Workspace:      ws,
		Command:        "echo pwned > AFTER_DESTROY",
		TimeoutSeconds: 5,
	})
	if err == nil || !strings.Contains(err.Error(), "not found") {
		t.Fatalf("post-destroy exec must not-found, got %v", err)
	}
	if _, statErr := os.Stat(marker); statErr == nil {
		t.Fatal("HOST ESCAPE: exec after destroy wrote on host workspace")
	}
}
