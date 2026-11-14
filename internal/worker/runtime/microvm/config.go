package microvm

import (
	"fmt"
	"os"
	"path/filepath"
	"strconv"
	"strings"

	"github.com/YanChao1999/Cell/internal/worker/runtime/microvm/guestproto"
)

// Config locates VMM binaries and guest artifacts.
// Empty paths are allowed at construction; Validate fails closed before boot.
type Config struct {
	// VMM is the hypervisor binary: firecracker | cloud-hypervisor | qemu-system-x86_64.
	VMM string
	// Kernel is the guest kernel image path (e.g. vmlinux).
	Kernel string
	// Rootfs is the guest root filesystem image path.
	Rootfs string
	// Jailer is an optional Firecracker jailer binary.
	Jailer string
	// ArtifactDir is where versioned kernels/rootfs live (default ~/.cell/runtime/microvm).
	ArtifactDir string
	// KernelArgs passed to the guest (default runs cell-guest-agent as init).
	KernelArgs string
	// MemSizeMiB guest memory (default 256).
	MemSizeMiB int
	// VCPUCount guest vCPUs (default 1).
	VCPUCount int
	// VsockPort guest agent port (default guestproto.DefaultPort).
	VsockPort uint32
}

// ConfigFromEnv reads CELL_MICROVM_* overrides.
func ConfigFromEnv() Config {
	dir := strings.TrimSpace(os.Getenv("CELL_MICROVM_DIR"))
	if dir == "" {
		home, _ := os.UserHomeDir()
		if home != "" {
			dir = home + "/.cell/runtime/microvm"
		}
	}
	cfg := Config{
		VMM:         firstEnv("CELL_MICROVM_VMM", "firecracker"),
		Kernel:      os.Getenv("CELL_MICROVM_KERNEL"),
		Rootfs:      os.Getenv("CELL_MICROVM_ROOTFS"),
		Jailer:      os.Getenv("CELL_MICROVM_JAILER"),
		ArtifactDir: dir,
		KernelArgs:  os.Getenv("CELL_MICROVM_KERNEL_ARGS"),
		MemSizeMiB:  envInt("CELL_MICROVM_MEM_MIB", 256),
		VCPUCount:   envInt("CELL_MICROVM_VCPUS", 1),
		VsockPort:   uint32(envInt("CELL_MICROVM_VSOCK_PORT", guestproto.DefaultPort)),
	}
	cfg.ResolveDefaults()
	return cfg
}

// ResolveDefaults fills Kernel/Rootfs from ArtifactDir when unset.
func (c *Config) ResolveDefaults() {
	if c == nil {
		return
	}
	if strings.TrimSpace(c.Kernel) == "" && c.ArtifactDir != "" {
		c.Kernel = filepath.Join(c.ArtifactDir, "vmlinux")
	}
	if strings.TrimSpace(c.Rootfs) == "" && c.ArtifactDir != "" {
		c.Rootfs = filepath.Join(c.ArtifactDir, "rootfs.ext4")
	}
	if c.VsockPort == 0 {
		c.VsockPort = guestproto.DefaultPort
	}
	if c.MemSizeMiB <= 0 {
		c.MemSizeMiB = 256
	}
	if c.VCPUCount <= 0 {
		c.VCPUCount = 1
	}
}

func envInt(key string, fallback int) int {
	v := strings.TrimSpace(os.Getenv(key))
	if v == "" {
		return fallback
	}
	n, err := strconv.Atoi(v)
	if err != nil {
		return fallback
	}
	return n
}

func firstEnv(key, fallback string) string {
	if v := strings.TrimSpace(os.Getenv(key)); v != "" {
		return v
	}
	return fallback
}

// Validate checks that required artifacts exist. Fail closed — never boot without them.
func (c Config) Validate() error {
	c.ResolveDefaults()
	if strings.TrimSpace(c.VMM) == "" {
		return fmt.Errorf("microvm: VMM binary path empty")
	}
	if _, err := lookPath(c.VMM); err != nil {
		return fmt.Errorf("microvm: VMM %q not found on PATH: %w", c.VMM, err)
	}
	if _, err := resolveJailer(c); err != nil {
		return err
	}
	for _, p := range []struct{ name, path string }{
		{"kernel", c.Kernel},
		{"rootfs", c.Rootfs},
	} {
		if strings.TrimSpace(p.path) == "" {
			return fmt.Errorf("microvm: %s path empty (set CELL_MICROVM_%s)", p.name, strings.ToUpper(p.name))
		}
		if st, err := os.Stat(p.path); err != nil || st.IsDir() {
			return fmt.Errorf("microvm: %s %q missing or not a file", p.name, p.path)
		}
	}
	return nil
}

// lookPath is overridden in tests.
var lookPath = execLookPath

func execLookPath(file string) (string, error) {
	return execLookPathStd(file)
}
