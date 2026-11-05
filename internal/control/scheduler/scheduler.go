// Package scheduler places Cells onto workers.
package scheduler

import (
	"fmt"

	"github.com/YanChao1999/Cell/api"
	"github.com/YanChao1999/Cell/internal/workerapi"
)

// Scheduler selects a worker for CreateCell placement.
// Order: prefer_local (client_worker_id) → warm CacheHints → free capacity.
type Scheduler struct {
	workers []workerapi.Worker
	localID string // preferred local worker id when prefer_local
}

// New creates a Scheduler over the given workers.
func New(workers []workerapi.Worker, localID string) *Scheduler {
	return &Scheduler{workers: workers, localID: localID}
}

// Register adds a worker.
func (s *Scheduler) Register(w workerapi.Worker) {
	s.workers = append(s.workers, w)
}

// Workers returns registered workers.
func (s *Scheduler) Workers() []workerapi.Worker {
	return s.workers
}

// Get returns a worker by id.
func (s *Scheduler) Get(id string) (workerapi.Worker, bool) {
	for _, w := range s.workers {
		if w.ID() == id {
			return w, true
		}
	}
	return nil, false
}

// Place chooses a worker for the create request.
func (s *Scheduler) Place(req api.CreateCellRequest) (workerapi.Worker, error) {
	if len(s.workers) == 0 {
		return nil, fmt.Errorf("scheduler: no workers registered")
	}

	// 1) prefer_local → client_worker_id or configured localID
	if req.PreferLocal {
		prefer := req.ClientWorkerID
		if prefer == "" {
			prefer = s.localID
		}
		if prefer != "" {
			if w, ok := s.Get(prefer); ok && w.Capacity().Free() > 0 {
				return w, nil
			}
		}
	}

	// 2) warm CacheHints — worker that has the most matching cache keys
	if len(req.CacheHints) > 0 {
		var best workerapi.Worker
		bestScore := -1
		for _, w := range s.workers {
			if w.Capacity().Free() <= 0 {
				continue
			}
			score := 0
			idx := w.CacheIndex()
			for _, h := range req.CacheHints {
				if idx != nil && idx.Has(h) {
					score++
				}
			}
			if score > bestScore {
				bestScore = score
				best = w
			}
		}
		if best != nil && bestScore > 0 {
			return best, nil
		}
	}

	// 3) free capacity — pick worker with most free slots
	var best workerapi.Worker
	bestFree := -1
	for _, w := range s.workers {
		free := w.Capacity().Free()
		if free > bestFree {
			bestFree = free
			best = w
		}
	}
	if best == nil || bestFree <= 0 {
		return nil, fmt.Errorf("scheduler: no free capacity")
	}
	return best, nil
}
