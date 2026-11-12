// Package version is the single source of truth for Cell binary versions.
package version

// Version is the release identifier reported by `celld version` / `cell version`.
// Bump on each tagged release; keep the -dev suffix on main between tags.
const Version = "0.7.1-dev"

// String returns a human-readable version line for a binary name.
func String(bin string) string {
	return bin + " " + Version
}
