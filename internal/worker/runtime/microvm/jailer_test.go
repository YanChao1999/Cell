package microvm

import (
	"os"
	"path/filepath"
	"strings"
	"testing"
)

func TestPrepareVMMUnjailed(t *testing.T) {
	t.Setenv("CELL_MICROVM_ALLOW_UNJAILED", "1")
	lookPath = func(file string) (string, error) {
		if file == "jailer" {
			return "", os.ErrNotExist
		}
		return "/usr/bin/" + filepath.Base(file), nil
	}
	t.Cleanup(func() { lookPath = execLookPathStd })

	dir := t.TempDir()
	kernel := filepath.Join(dir, "vmlinux")
	rootfs := filepath.Join(dir, "rootfs.ext4")
	_ = os.WriteFile(kernel, []byte("k"), 0o644)
	_ = os.WriteFile(rootfs, []byte("r"), 0o644)
	work := filepath.Join(dir, "work")
	_ = os.MkdirAll(work, 0o755)

	launch, err := prepareVMM(Config{VMM: "firecracker", Kernel: kernel, Rootfs: rootfs}, "cell-1", work, filepath.Join(work, "vsock.sock"))
	if err != nil {
		t.Fatal(err)
	}
	if launch.JailRoot != "" {
		t.Fatalf("expected unjailed, got jail root %q", launch.JailRoot)
	}
	if launch.KernelAPI != kernel || launch.RootfsAPI != rootfs {
		t.Fatalf("unexpected API paths %+v", launch)
	}
	args := launch.Cmd.Args
	if len(args) < 2 || !strings.Contains(args[0], "firecracker") {
		t.Fatalf("want bare firecracker, got %v", args)
	}
}

func TestPrepareVMMJailed(t *testing.T) {
	t.Setenv("CELL_MICROVM_ALLOW_UNJAILED", "0")
	lookPath = func(file string) (string, error) {
		base := filepath.Base(file)
		return "/usr/bin/" + base, nil
	}
	t.Cleanup(func() { lookPath = execLookPathStd })

	dir := t.TempDir()
	kernel := filepath.Join(dir, "vmlinux")
	rootfs := filepath.Join(dir, "rootfs.ext4")
	_ = os.WriteFile(kernel, []byte("k"), 0o644)
	_ = os.WriteFile(rootfs, []byte("r"), 0o644)
	work := filepath.Join(dir, "work")
	_ = os.MkdirAll(work, 0o755)

	launch, err := prepareVMM(Config{VMM: "firecracker", Jailer: "jailer", Kernel: kernel, Rootfs: rootfs}, "cell-1", work, filepath.Join(work, "vsock.sock"))
	if err != nil {
		t.Fatal(err)
	}
	if launch.JailRoot == "" {
		t.Fatal("expected jail root")
	}
	if launch.KernelAPI != "/vmlinux" || launch.RootfsAPI != "/rootfs.ext4" {
		t.Fatalf("want chroot-relative paths, got %+v", launch)
	}
	joined := strings.Join(launch.Cmd.Args, " ")
	if !strings.Contains(joined, "jailer") || !strings.Contains(joined, "--chroot-base-dir") {
		t.Fatalf("want jailer argv, got %v", launch.Cmd.Args)
	}
	if _, err := os.Stat(filepath.Join(launch.JailRoot, "vmlinux")); err != nil {
		t.Fatalf("staged kernel: %v", err)
	}
}

func TestTapNameLength(t *testing.T) {
	name := tapNameFor("this-is-a-very-long-cell-identifier-xyz")
	if len(name) > 15 {
		t.Fatalf("tap name too long: %q (%d)", name, len(name))
	}
}
