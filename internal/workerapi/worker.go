// Package workerapi defines the Worker contract used by the control plane.
package workerapi

import (
	"context"

	"github.com/YanChao1999/spark/api"
)

// Capacity describes free/total slots on a worker.
type Capacity struct {
	MaxSparks  int `json:"max_sparks"`
	UsedSparks int `json:"used_sparks"`
}

// Free returns remaining spark slots.
func (c Capacity) Free() int {
	free := c.MaxSparks - c.UsedSparks
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

// Spark is a running disposable environment on a worker.
type Spark struct {
	ID          string
	Workspace   string
	Environment string
	Status      string
}

// Worker is the interface implemented by LocalWorker and remote workers.
type Worker interface {
	ID() string
	Capacity() Capacity
	CacheIndex() CacheIndex
	CreateSpark(ctx context.Context, req api.CreateSparkRequest) (*Spark, error)
	Exec(ctx context.Context, sparkID string, req api.ExecRequest) (*api.ExecResult, error)
	DestroySpark(ctx context.Context, sparkID string) error
}
