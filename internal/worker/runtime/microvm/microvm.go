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
	"strings"

	"github.com/YanChao1999/Cell/api"
	"github.com/YanChao1999/Cell/internal/worker/runtime"
)

// ErrNoBackend means no VMM Backend is registered yet.
var ErrNoBackend = errors.New("microvm: no VMM backend registered")

// Runtime implements runtime.Runtime via an optional Backend.
type Runtime struct {
	cfg     Config
	backend Backend
}

// New returns a microVM Runtime. When CELL_MICROVM_VMM is firecracker (default),
// a Firecracker Backend is wired; Boot still fail-closes until artifacts validate.
func New() *Runtime {
	cfg := ConfigFromEnv()
	cfg.ResolveDefaults()
	var b Backend
	switch baseName(cfg.VMM) {
	case "firecracker", "":
		b = NewFirecracker()
	default:
		// Unknown VMM: leave backend nil → ErrNotImplemented (fail closed).
	}
	return NewWith(cfg, b)
}

func baseName(vmm string) string {
	vmm = strings.TrimSpace(vmm)
	if vmm == "" {
		return ""
	}
	if i := strings.LastIndex(vmm, "/"); i >= 0 {
		vmm = vmm[i+1:]
	}
	return strings.ToLower(vmm)
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
		return fmt.Errorf("%w: microVM backend (unsupported CELL_MICROVM_VMM; use firecracker)", runtime.ErrNotImplemented)
	}
	if _, err := validateNetworkOpts(opts); err != nil {
		return err
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

// ResyncWorkspace re-pushes the host workspace into a running guest (snapshot restore).
func (r *Runtime) ResyncWorkspace(ctx context.Context, cellID, workspace string) error {
	if r == nil || r.backend == nil {
		return fmt.Errorf("%w: microVM backend", runtime.ErrNotImplemented)
	}
	fc, ok := r.backend.(*Firecracker)
	if !ok {
		return fmt.Errorf("microvm: resync unsupported for backend %T", r.backend)
	}
	return fc.Resync(ctx, cellID, workspace)
}

// PullWorkspace copies guest /workspace onto the host path (snapshot capture).
func (r *Runtime) PullWorkspace(ctx context.Context, cellID, workspace string) error {
	if r == nil || r.backend == nil {
		return fmt.Errorf("%w: microVM backend", runtime.ErrNotImplemented)
	}
	fc, ok := r.backend.(*Firecracker)
	if !ok {
		return fmt.Errorf("microvm: pull unsupported for backend %T", r.backend)
	}
	return fc.Pull(ctx, cellID, workspace)
}

var _ runtime.Runtime = (*Runtime)(nil)
