// Package runtime defines the container/runtime abstraction for Cells.
package runtime

import (
	"context"
	"errors"

	"github.com/YanChao1999/spark/api"
	"github.com/YanChao1999/spark/internal/worker/cache"
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
	// CacheMounts are durable host dirs bind-mounted into the container
	// (Docker). In host-fallback mode they drive cache-related env vars.
	CacheMounts []cache.Mount
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
