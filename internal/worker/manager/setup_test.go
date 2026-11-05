package manager

import (
	"context"
	"os"
	"path/filepath"
	"strings"
	"sync"
	"testing"

	"github.com/YanChao1999/Cell/api"
	"github.com/YanChao1999/Cell/internal/worker/envtemplate"
	"github.com/YanChao1999/Cell/internal/worker/runtime"
	"github.com/YanChao1999/Cell/internal/worker/source/git"
)

type fakeRT struct {
	mu       sync.Mutex
	created  []string
	cmds     []string
	failCmd  string
	failExit int
}

func (f *fakeRT) Create(ctx context.Context, opts runtime.CreateOpts) error {
	_ = ctx
	f.mu.Lock()
	defer f.mu.Unlock()
	f.created = append(f.created, opts.CellID)
	return nil
}

func (f *fakeRT) Exec(ctx context.Context, opts runtime.ExecOpts) (*api.ExecResult, error) {
	_ = ctx
	f.mu.Lock()
	defer f.mu.Unlock()
	f.cmds = append(f.cmds, opts.Command)
	if f.failCmd != "" && opts.Command == f.failCmd {
		return &api.ExecResult{ExitCode: f.failExit, Stderr: "setup boom"}, nil
	}
	return &api.ExecResult{ExitCode: 0, Stdout: "ok\n"}, nil
}

func (f *fakeRT) Destroy(ctx context.Context, cellID string) error {
	_ = ctx
	f.mu.Lock()
	defer f.mu.Unlock()
	for i, id := range f.created {
		if id == cellID {
			f.created = append(f.created[:i], f.created[i+1:]...)
			break
		}
	}
	return nil
}

func writeEnv(t *testing.T, dir, name, body string) {
	t.Helper()
	if err := os.WriteFile(filepath.Join(dir, name+".yaml"), []byte(body), 0o644); err != nil {
		t.Fatal(err)
	}
}

func TestCreateRunsSetup(t *testing.T) {
	root := t.TempDir()
	envDir := filepath.Join(root, "envs")
	if err := os.MkdirAll(envDir, 0o755); err != nil {
		t.Fatal(err)
	}
	writeEnv(t, envDir, "t1", `
name: t1
setup:
  - echo one
  - echo two
default_exec_timeout: 1m
`)
	rt := &fakeRT{}
	w, err := New(Options{
		ID:             "w",
		WorkspacesRoot: filepath.Join(root, "ws"),
		CacheDir:       filepath.Join(root, "cache"),
		Runtime:        rt,
		Source:         git.NewStub(),
		EnvLoader:      envtemplate.New(envDir),
	})
	if err != nil {
		t.Fatal(err)
	}
	cell, err := w.CreateCell(context.Background(), api.CreateCellRequest{Environment: "t1", TTL: "5m"})
	if err != nil {
		t.Fatal(err)
	}
	if cell.Status != "ready" {
		t.Fatalf("status %q", cell.Status)
	}
	if got := strings.Join(rt.cmds, ","); got != "echo one,echo two" {
		t.Fatalf("setup cmds: %q", got)
	}
}

func TestCreateSetupFailureRollsBack(t *testing.T) {
	root := t.TempDir()
	envDir := filepath.Join(root, "envs")
	if err := os.MkdirAll(envDir, 0o755); err != nil {
		t.Fatal(err)
	}
	writeEnv(t, envDir, "t1", `
name: t1
setup:
  - echo ok
  - echo fail
`)
	rt := &fakeRT{failCmd: "echo fail", failExit: 7}
	w, err := New(Options{
		ID:             "w",
		WorkspacesRoot: filepath.Join(root, "ws"),
		CacheDir:       filepath.Join(root, "cache"),
		Runtime:        rt,
		Source:         git.NewStub(),
		EnvLoader:      envtemplate.New(envDir),
	})
	if err != nil {
		t.Fatal(err)
	}
	_, err = w.CreateCell(context.Background(), api.CreateCellRequest{Environment: "t1"})
	if err == nil {
		t.Fatal("expected setup failure")
	}
	if !strings.Contains(err.Error(), "setup[1]") {
		t.Fatalf("err: %v", err)
	}
	if len(rt.created) != 0 {
		t.Fatalf("runtime not destroyed: %v", rt.created)
	}
	if cap := w.Capacity(); cap.UsedCells != 0 {
		t.Fatalf("worker still tracking cell: %+v", cap)
	}
	entries, _ := os.ReadDir(filepath.Join(root, "ws"))
	if len(entries) != 0 {
		t.Fatalf("workspace leftover: %v", entries)
	}
}

func TestCreateNoSetup(t *testing.T) {
	root := t.TempDir()
	envDir := filepath.Join(root, "envs")
	_ = os.MkdirAll(envDir, 0o755)
	writeEnv(t, envDir, "bare", "name: bare\n")
	rt := &fakeRT{}
	w, err := New(Options{
		WorkspacesRoot: filepath.Join(root, "ws"),
		CacheDir:       filepath.Join(root, "cache"),
		Runtime:        rt,
		Source:         git.NewStub(),
		EnvLoader:      envtemplate.New(envDir),
	})
	if err != nil {
		t.Fatal(err)
	}
	if _, err := w.CreateCell(context.Background(), api.CreateCellRequest{Environment: "bare"}); err != nil {
		t.Fatal(err)
	}
	if len(rt.cmds) != 0 {
		t.Fatalf("unexpected setup: %v", rt.cmds)
	}
}

var _ runtime.Runtime = (*fakeRT)(nil)
