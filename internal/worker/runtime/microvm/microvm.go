// Package microvm is a placeholder Runtime backend for stronger isolation than
// containers (Firecracker / Cloud Hypervisor / QEMU microVM).
//
// Security design: the agent API and Worker contracts stay unchanged; raising
// the isolation ceiling is a Runtime swap. This stub exists so the gap stays
// visible in-tree and cannot be “forgotten” as folklore.
package microvm

import (
	"context"
	"fmt"

	"github.com/YanChao1999/Cell/api"
	"github.com/YanChao1999/Cell/internal/worker/runtime"
)

// Runtime is not implemented yet.
type Runtime struct{}

// New returns a microVM Runtime stub.
func New() *Runtime { return &Runtime{} }

// Create returns ErrNotImplemented until a microVM backend ships.
func (r *Runtime) Create(ctx context.Context, opts runtime.CreateOpts) error {
	_ = ctx
	_ = opts
	return fmt.Errorf("%w: microVM backend", runtime.ErrNotImplemented)
}

// Exec returns ErrNotImplemented.
func (r *Runtime) Exec(ctx context.Context, opts runtime.ExecOpts) (*api.ExecResult, error) {
	_ = ctx
	_ = opts
	return nil, fmt.Errorf("%w: microVM backend", runtime.ErrNotImplemented)
}

// Destroy returns ErrNotImplemented.
func (r *Runtime) Destroy(ctx context.Context, cellID string) error {
	_ = ctx
	_ = cellID
	return fmt.Errorf("%w: microVM backend", runtime.ErrNotImplemented)
}

var _ runtime.Runtime = (*Runtime)(nil)
