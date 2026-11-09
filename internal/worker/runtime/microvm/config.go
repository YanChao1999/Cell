package microvm

import (
	"fmt"
	"os"
	"strings"
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
	return Config{
		VMM:         firstEnv("CELL_MICROVM_VMM", "firecracker"),
		Kernel:      os.Getenv("CELL_MICROVM_KERNEL"),
		Rootfs:      os.Getenv("CELL_MICROVM_ROOTFS"),
		Jailer:      os.Getenv("CELL_MICROVM_JAILER"),
		ArtifactDir: dir,
	}
}

func firstEnv(key, fallback string) string {
	if v := strings.TrimSpace(os.Getenv(key)); v != "" {
		return v
	}
	return fallback
}

// Validate checks that required artifacts exist. Fail closed — never boot without them.
func (c Config) Validate() error {
	if strings.TrimSpace(c.VMM) == "" {
		return fmt.Errorf("microvm: VMM binary path empty")
	}
	if _, err := lookPath(c.VMM); err != nil {
		return fmt.Errorf("microvm: VMM %q not found on PATH: %w", c.VMM, err)
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
