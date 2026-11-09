package microvm

import (
	"context"
	"errors"
	"os"
	"path/filepath"
	"testing"

	"github.com/YanChao1999/Cell/internal/worker/runtime"
)

func TestCreateFailClosedWithoutBackend(t *testing.T) {
	r := NewWith(Config{}, nil)
	err := r.Create(context.Background(), runtime.CreateOpts{CellID: "c1"})
	if err == nil {
		t.Fatal("expected error")
	}
	if !errors.Is(err, runtime.ErrNotImplemented) {
		t.Fatalf("want ErrNotImplemented, got %v", err)
	}
}

func TestValidateRequiresArtifacts(t *testing.T) {
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
