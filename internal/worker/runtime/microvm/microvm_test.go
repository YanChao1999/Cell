package microvm

import (
	"context"
	"errors"
	"os"
	"path/filepath"
	"strings"
	"testing"

	"github.com/YanChao1999/Cell/internal/worker/runtime"
)

func TestCreateFailClosedWithoutBackend(t *testing.T) {
	t.Setenv("CELL_MICROVM_ALLOW_UNJAILED", "1")
	r := NewWith(Config{}, nil)
	err := r.Create(context.Background(), runtime.CreateOpts{CellID: "c1"})
	if err == nil {
		t.Fatal("expected error")
	}
	if !errors.Is(err, runtime.ErrNotImplemented) {
		t.Fatalf("want ErrNotImplemented, got %v", err)
	}
}

func TestCreateFailClosedMissingArtifacts(t *testing.T) {
	t.Setenv("CELL_MICROVM_ALLOW_UNJAILED", "1")
	lookPath = func(file string) (string, error) { return "/usr/bin/" + file, nil }
	t.Cleanup(func() { lookPath = execLookPathStd })

	r := NewWith(Config{VMM: "firecracker", ArtifactDir: t.TempDir()}, NewFirecracker())
	err := r.Create(context.Background(), runtime.CreateOpts{CellID: "c1"})
	if err == nil {
		t.Fatal("expected missing kernel/rootfs error")
	}
}

func TestValidateRequiresArtifacts(t *testing.T) {
	t.Setenv("CELL_MICROVM_ALLOW_UNJAILED", "1")
	lookPath = func(file string) (string, error) { return "/bin/" + file, nil }
	t.Cleanup(func() { lookPath = execLookPathStd })

	cfg := Config{VMM: "firecracker"}
	if err := cfg.Validate(); err == nil {
		t.Fatal("expected empty kernel/rootfs error")
	}

	dir := t.TempDir()
	kernel := filepath.Join(dir, "vmlinux")
	rootfs := filepath.Join(dir, "rootfs.ext4")
	for _, p := range []string{kernel, rootfs} {
		if err := os.WriteFile(p, []byte("x"), 0o644); err != nil {
			t.Fatal(err)
		}
	}
	cfg = Config{VMM: "firecracker", Kernel: kernel, Rootfs: rootfs}
	if err := cfg.Validate(); err != nil {
		t.Fatal(err)
	}
}

func TestValidateMissingVMM(t *testing.T) {
	t.Setenv("CELL_MICROVM_ALLOW_UNJAILED", "1")
	lookPath = func(string) (string, error) { return "", os.ErrNotExist }
	t.Cleanup(func() { lookPath = execLookPathStd })
	dir := t.TempDir()
	kernel := filepath.Join(dir, "vmlinux")
	rootfs := filepath.Join(dir, "rootfs.ext4")
	_ = os.WriteFile(kernel, []byte("x"), 0o644)
	_ = os.WriteFile(rootfs, []byte("x"), 0o644)
	err := Config{VMM: "nope", Kernel: kernel, Rootfs: rootfs}.Validate()
	if err == nil {
		t.Fatal("expected missing VMM error")
	}
}

func TestValidateRequiresJailer(t *testing.T) {
	t.Setenv("CELL_MICROVM_ALLOW_UNJAILED", "0")
	lookPath = func(file string) (string, error) {
		if file == "jailer" || strings.Contains(file, "jailer") {
			return "", os.ErrNotExist
		}
		return "/bin/" + file, nil
	}
	t.Cleanup(func() { lookPath = execLookPathStd })
	dir := t.TempDir()
	kernel := filepath.Join(dir, "vmlinux")
	rootfs := filepath.Join(dir, "rootfs.ext4")
	_ = os.WriteFile(kernel, []byte("x"), 0o644)
	_ = os.WriteFile(rootfs, []byte("x"), 0o644)
	err := Config{VMM: "firecracker", Kernel: kernel, Rootfs: rootfs}.Validate()
	if err == nil || !strings.Contains(err.Error(), "jailer") {
		t.Fatalf("want jailer error, got %v", err)
	}
}

func TestResolveDefaults(t *testing.T) {
	dir := t.TempDir()
	cfg := Config{ArtifactDir: dir}
	cfg.ResolveDefaults()
	if cfg.Kernel != filepath.Join(dir, "vmlinux") || cfg.Rootfs != filepath.Join(dir, "rootfs.ext4") {
		t.Fatalf("got %+v", cfg)
	}
}

func TestNewWiresFirecracker(t *testing.T) {
	t.Setenv("CELL_MICROVM_VMM", "firecracker")
	r := New()
	if r.backend == nil || r.backend.Name() != "firecracker" {
		t.Fatalf("expected firecracker backend, got %#v", r.backend)
	}
}
