package microvm

import (
	"context"

	"github.com/YanChao1999/Cell/api"
	"github.com/YanChao1999/Cell/internal/worker/runtime"
)

// Backend is a real VMM integration (Firecracker / Cloud Hypervisor / QEMU).
// The Runtime wrapper stays fail-closed until a Backend is registered and Config validates.
type Backend interface {
	Name() string
	Boot(ctx context.Context, cellID string, opts runtime.CreateOpts, cfg Config) error
	Exec(ctx context.Context, opts runtime.ExecOpts) (*api.ExecResult, error)
	Halt(ctx context.Context, cellID string) error
}

// nopBackend documents the contract; it is never used for successful boots.
type nopBackend struct{}

func (nopBackend) Name() string { return "none" }

func (nopBackend) Boot(context.Context, string, runtime.CreateOpts, Config) error {
	return errNoBackend
}

func (nopBackend) Exec(context.Context, runtime.ExecOpts) (*api.ExecResult, error) {
	return nil, errNoBackend
}

func (nopBackend) Halt(context.Context, string) error { return errNoBackend }
