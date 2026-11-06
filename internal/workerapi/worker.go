// Package workerapi defines the Worker contract used by the control plane.
package workerapi

import (
	"context"

	"github.com/YanChao1999/Cell/api"
)

// Capacity describes free/total slots on a worker.
type Capacity struct {
	MaxCells  int `json:"max_cells"`
	UsedCells int `json:"used_cells"`
}

// Free returns remaining cell slots.
func (c Capacity) Free() int {
	free := c.MaxCells - c.UsedCells
	if free < 0 {
		return 0
	}
	return free
}

// CacheIndex reports durable caches present on a worker (warm placement hints).
type CacheIndex interface {
	Has(key string) bool
	Keys() []string
}

// Cell is a running disposable environment on a worker.
type Cell struct {
	ID          string
	Workspace   string
	Environment string
	Status      string
	DiskLimit   string // template/request disk cap (portable host-side enforcement)
}

// Worker is the interface implemented by LocalWorker and remote workers.
type Worker interface {
	ID() string
	Capacity() Capacity
	CacheIndex() CacheIndex
	CreateCell(ctx context.Context, req api.CreateCellRequest) (*Cell, error)
	Exec(ctx context.Context, cellID string, req api.ExecRequest) (*api.ExecResult, error)
	DestroyCell(ctx context.Context, cellID string) error
}
