package runtime

import (
	"fmt"
	"os"
	"strings"
)

// Name is a runtime backend identifier from CELL_RUNTIME.
type Name string

const (
	NameDocker  Name = "docker"
	NameHost    Name = "host"
	NameMicroVM Name = "microvm"
	NameAuto    Name = "" // docker with host fallback policy inside docker package
)

// ParseName reads CELL_RUNTIME (docker|host|microvm). Empty means docker/auto.
func ParseName(s string) (Name, error) {
	switch strings.ToLower(strings.TrimSpace(s)) {
	case "", "docker", "auto":
		return NameDocker, nil
	case "host":
		return NameHost, nil
	case "microvm", "firecracker", "cloud-hypervisor", "qemu":
		return NameMicroVM, nil
	default:
		return "", fmt.Errorf("runtime: unknown CELL_RUNTIME %q (want docker|host|microvm)", s)
	}
}

// FromEnv returns the configured runtime name.
func FromEnv() (Name, error) {
	return ParseName(os.Getenv("CELL_RUNTIME"))
}
