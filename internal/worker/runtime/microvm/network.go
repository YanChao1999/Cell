package microvm

import (
	"fmt"
	"strings"

	"github.com/YanChao1999/Cell/internal/worker/runtime"
)

// Network modes supported by the microVM Runtime (M2).
// Open NAT (default/open) is intentionally rejected until a later milestone.
const (
	networkNone      = "none"
	networkAllowlist = "allowlist"
)

// normalizeNetwork maps CreateOpts.Network to an enforced microVM mode.
func normalizeNetwork(n string) (string, error) {
	switch strings.ToLower(strings.TrimSpace(n)) {
	case "", "none":
		return networkNone, nil
	case "allowlist":
		return networkAllowlist, nil
	case "default", "open", "bridge":
		return "", fmt.Errorf("microvm: network %q not supported (want none|allowlist; open NAT is not in M2)", n)
	default:
		return "", fmt.Errorf("microvm: unsupported network mode %q (want none|allowlist)", n)
	}
}

func validateNetworkOpts(opts runtime.CreateOpts) (string, error) {
	mode, err := normalizeNetwork(opts.Network)
	if err != nil {
		return "", err
	}
	if mode == networkAllowlist && len(opts.EgressAllow) == 0 {
		return "", fmt.Errorf("microvm: network allowlist requires egress.allow hosts")
	}
	if mode == networkAllowlist && strings.TrimSpace(opts.Workspace) == "" {
		return "", fmt.Errorf("microvm: network allowlist requires workspace for audit log")
	}
	return mode, nil
}
