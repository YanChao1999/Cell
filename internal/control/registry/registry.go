// Package registry tracks live workers for Phase 5 multi-node placement.
//
// Workers register and heartbeat; stale workers are dropped after TTL so the
// scheduler only places onto healthy peers.
package registry

import (
	"fmt"
	"strings"
	"sync"
	"time"
)

// Info is the control-plane view of a registered worker.
type Info struct {
	ID            string    `json:"id"`
	Endpoint      string    `json:"endpoint,omitempty"` // e.g. unix:///… or http://host:port
	MaxCells      int       `json:"max_cells"`
	UsedCells     int       `json:"used_cells"`
	CacheKeys     []string  `json:"cache_keys,omitempty"`
	LastHeartbeat time.Time `json:"last_heartbeat"`
}

// RegisterRequest is POST /v1/workers/register.
type RegisterRequest struct {
	ID        string   `json:"id"`
	Endpoint  string   `json:"endpoint,omitempty"`
	MaxCells  int      `json:"max_cells"`
	UsedCells int      `json:"used_cells"`
	CacheKeys []string `json:"cache_keys,omitempty"`
}

// HeartbeatRequest is POST /v1/workers/{id}/heartbeat.
type HeartbeatRequest struct {
	UsedCells int      `json:"used_cells"`
	MaxCells  int      `json:"max_cells,omitempty"`
	CacheKeys []string `json:"cache_keys,omitempty"`
}

// Registry is an in-memory worker directory with heartbeat expiry.
type Registry struct {
	mu      sync.Mutex
	ttl     time.Duration
	workers map[string]*Info
}

// New returns a registry that expires workers after ttl without a heartbeat.
func New(ttl time.Duration) *Registry {
	if ttl <= 0 {
		ttl = 45 * time.Second
	}
	return &Registry{ttl: ttl, workers: make(map[string]*Info)}
}

// Register inserts or refreshes a worker.
func (r *Registry) Register(req RegisterRequest) (*Info, error) {
	id := strings.TrimSpace(req.ID)
	if id == "" {
		return nil, fmt.Errorf("registry: worker id required")
	}
	if req.MaxCells <= 0 {
		req.MaxCells = 1
	}
	r.mu.Lock()
	defer r.mu.Unlock()
	info := &Info{
		ID:            id,
		Endpoint:      req.Endpoint,
		MaxCells:      req.MaxCells,
		UsedCells:     req.UsedCells,
		CacheKeys:     append([]string(nil), req.CacheKeys...),
		LastHeartbeat: time.Now().UTC(),
	}
	r.workers[id] = info
	return cloneInfo(info), nil
}

// Heartbeat refreshes liveness and capacity for an existing worker.
func (r *Registry) Heartbeat(id string, req HeartbeatRequest) (*Info, error) {
	id = strings.TrimSpace(id)
	r.mu.Lock()
	defer r.mu.Unlock()
	r.expireLocked(time.Now())
	info, ok := r.workers[id]
	if !ok {
		return nil, fmt.Errorf("registry: worker %q not registered", id)
	}
	info.UsedCells = req.UsedCells
	if req.MaxCells > 0 {
		info.MaxCells = req.MaxCells
	}
	if req.CacheKeys != nil {
		info.CacheKeys = append([]string(nil), req.CacheKeys...)
	}
	info.LastHeartbeat = time.Now().UTC()
	return cloneInfo(info), nil
}

// List returns non-expired workers.
func (r *Registry) List() []Info {
	r.mu.Lock()
	defer r.mu.Unlock()
	r.expireLocked(time.Now())
	out := make([]Info, 0, len(r.workers))
	for _, w := range r.workers {
		out = append(out, *cloneInfo(w))
	}
	return out
}

// Get returns one live worker.
func (r *Registry) Get(id string) (*Info, bool) {
	r.mu.Lock()
	defer r.mu.Unlock()
	r.expireLocked(time.Now())
	w, ok := r.workers[id]
	if !ok {
		return nil, false
	}
	return cloneInfo(w), true
}

func (r *Registry) expireLocked(now time.Time) {
	for id, w := range r.workers {
		if now.Sub(w.LastHeartbeat) > r.ttl {
			delete(r.workers, id)
		}
	}
}

func cloneInfo(in *Info) *Info {
	if in == nil {
		return nil
	}
	out := *in
	out.CacheKeys = append([]string(nil), in.CacheKeys...)
	return &out
}
