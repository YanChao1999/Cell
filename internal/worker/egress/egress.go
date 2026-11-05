// Package egress implements allowlist HTTP(S) egress for network: allowlist.
//
// Security design: Cells must not have open internet. Traffic goes through a
// Cell-managed CONNECT/forward proxy that enforces host allowlists and writes
// an audit log. Docker wiring places the Cell on an internal network with a
// proxy sidecar that also has a bridge path for dial-out.
//
// Optional upstream (CELL_EGRESS_UPSTREAM / corporate HTTP proxy) is used only
// by the sidecar after allowlist checks: Cell → cell-egress → upstream → internet.
package egress

import "fmt"

// Policy is the declarative egress policy from an environment template.
type Policy struct {
	Mode  string   // "allowlist"
	Allow []string // hostnames, e.g. "proxy.golang.org", "github.com"
	// Upstream is an optional HTTP proxy URL for dial-out after allowlist
	// (e.g. corporate proxy). Empty means dial destinations directly.
	Upstream string
}

// ErrEmptyAllowlist is returned when allowlist mode has no hosts.
var ErrEmptyAllowlist = fmt.Errorf("egress: empty allowlist")
