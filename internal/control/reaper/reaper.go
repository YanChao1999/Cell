// Package reaper destroys Cells whose TTL has expired.
package reaper

import (
	"context"
	"log"
	"time"

	"github.com/YanChao1999/Cell/internal/control/meta"
	"github.com/YanChao1999/Cell/internal/control/scheduler"
)

// Reaper periodically checks meta for expired Cells and destroys them.
type Reaper struct {
	meta      *meta.Store
	scheduler *scheduler.Scheduler
	interval  time.Duration
	destroy   DestroyFunc
}

// DestroyFunc destroys a cell on its worker and clears meta.
type DestroyFunc func(ctx context.Context, cellID, workerID string) error

// New creates a Reaper. interval defaults to 30s.
func New(m *meta.Store, sched *scheduler.Scheduler, destroy DestroyFunc, interval time.Duration) *Reaper {
	if interval <= 0 {
		interval = 30 * time.Second
	}
	return &Reaper{
		meta:      m,
		scheduler: sched,
		interval:  interval,
		destroy:   destroy,
	}
}

// Run blocks until ctx is cancelled, reaping expired Cells.
// It performs an immediate pass on start so short TTLs are honored without
// waiting a full interval.
func (r *Reaper) Run(ctx context.Context) {
	r.tick(ctx)
	t := time.NewTicker(r.interval)
	defer t.Stop()
	for {
		select {
		case <-ctx.Done():
			return
		case <-t.C:
			r.tick(ctx)
		}
	}
}

func (r *Reaper) tick(ctx context.Context) {
	expired := r.meta.Expired(time.Now())
	for _, rec := range expired {
		if err := r.destroy(ctx, rec.ID, rec.WorkerID); err != nil {
			log.Printf("reaper: destroy %s: %v", rec.ID, err)
			continue
		}
		log.Printf("reaper: destroyed expired cell %s", rec.ID)
	}
}
