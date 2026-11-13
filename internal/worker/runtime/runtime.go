// Package runtime defines the container/runtime abstraction for Cells.
package runtime

import (
	"context"
	"errors"

	"github.com/YanChao1999/Cell/api"
	"github.com/YanChao1999/Cell/internal/worker/cache"
)

// ErrNotImplemented is returned by stub runtimes that are not wired yet.
var ErrNotImplemented = errors.New("runtime: not implemented")

// CreateOpts are options when creating a runtime instance for a Cell.
type CreateOpts struct {
	CellID      string
	Workspace   string
	Environment string
	Image       string
	Limits      api.Limits
	Env         map[string]string
	// Network is the egress policy from the environment template:
	//   "" / "default" / "open" — normal Docker egress (rejected on microVM)
	//   "none" — no network (Docker --network none; microVM: no NIC)
	//   "allowlist" — proxy allowlisting (Docker sidecar; microVM TAP + host cell-egress)
	// Host fallback rejects any mode other than default/open.
	Network string
	// EgressAllow is the hostname allowlist when Network is allowlist.
	EgressAllow []string
	// CacheMounts are durable host dirs bind-mounted into the container
	// (Docker). In host-fallback mode they drive cache-related env vars.
	// In Docker mode the worker should pass per-Cell writable dirs here so
	// Cells cannot poison a shared host cache (see TECH_DEBT / cache trust).
	CacheMounts []cache.Mount
	// GitReposRoot, when set, is bind-mounted read-only at the same absolute
	// path inside the container so git worktrees (.git gitdir files) resolve.
	GitReposRoot string
}

// ExecOpts are options for running a command inside a Cell runtime.
type ExecOpts struct {
	CellID         string
	Workspace      string
	Command        string
	TimeoutSeconds int
	Env            map[string]string // extra env (host-fallback cache paths, etc.)
}

// Runtime manages isolated execution environments (e.g. Docker).
type Runtime interface {
	Create(ctx context.Context, opts CreateOpts) error
	Exec(ctx context.Context, opts ExecOpts) (*api.ExecResult, error)
	Destroy(ctx context.Context, cellID string) error
}
