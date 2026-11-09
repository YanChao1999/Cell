// Package microvm is the stronger-isolation Runtime mainline (Firecracker /
// Cloud Hypervisor / QEMU). Until a real Backend ships, all operations fail closed.
//
// Security design: the agent API and Worker contracts stay unchanged; raising
// the isolation ceiling is a Runtime swap. Selecting CELL_RUNTIME=microvm must
// never silently fall back to Docker or host.
package microvm

import (
	"context"
	"errors"
	"fmt"

	"github.com/YanChao1999/Cell/api"
	"github.com/YanChao1999/Cell/internal/worker/runtime"
)

// ErrNoBackend means no VMM Backend is registered yet.
var ErrNoBackend = errors.New("microvm: no VMM backend registered")

var errNoBackend = ErrNoBackend

// Runtime implements runtime.Runtime via an optional Backend.
type Runtime struct {
	cfg     Config
	backend Backend
}

// New returns a microVM Runtime using ConfigFromEnv and no Backend (fail-closed).
func New() *Runtime {
	return NewWith(ConfigFromEnv(), nil)
}

// NewWith wires an explicit config and backend (backend may be nil).
func NewWith(cfg Config, backend Backend) *Runtime {
	return &Runtime{cfg: cfg, backend: backend}
}

// Create boots a guest or returns a fail-closed error.
func (r *Runtime) Create(ctx context.Context, opts runtime.CreateOpts) error {
	if r == nil {
		return fmt.Errorf("%w: nil microvm runtime", runtime.ErrNotImplemented)
	}
	if r.backend == nil {
		return fmt.Errorf("%w: microVM backend (set CELL_RUNTIME=docker until Firecracker/CH ships)", runtime.ErrNotImplemented)
	}
	if err := r.cfg.Validate(); err != nil {
		return err
	}
	return r.backend.Boot(ctx, opts.CellID, opts, r.cfg)
}

// Exec runs a command inside the guest or fails closed.
func (r *Runtime) Exec(ctx context.Context, opts runtime.ExecOpts) (*api.ExecResult, error) {
	if r == nil || r.backend == nil {
		return nil, fmt.Errorf("%w: microVM backend", runtime.ErrNotImplemented)
	}
	return r.backend.Exec(ctx, opts)
}

// Destroy halts the guest or fails closed.
func (r *Runtime) Destroy(ctx context.Context, cellID string) error {
	if r == nil || r.backend == nil {
		return fmt.Errorf("%w: microVM backend", runtime.ErrNotImplemented)
	}
	return r.backend.Halt(ctx, cellID)
}

var _ runtime.Runtime = (*Runtime)(nil)
