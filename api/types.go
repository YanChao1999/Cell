// Package api defines shared HTTP/JSON types for the Cell Agent API.
package api

import "time"

// Limits are optional resource caps for a Cell.
type Limits struct {
	CPU    string `json:"cpu,omitempty"`
	Memory string `json:"memory,omitempty"`
	Disk   string `json:"disk,omitempty"`
}

// CreateCellRequest is the body for POST /v1/cells.
type CreateCellRequest struct {
	Repo           string   `json:"repo,omitempty"`
	Ref            string   `json:"ref,omitempty"`
	Environment    string   `json:"environment,omitempty"`
	TTL            string   `json:"ttl,omitempty"` // e.g. "30m", "1h"
	PreferLocal    bool     `json:"prefer_local,omitempty"`
	Limits         Limits   `json:"limits,omitempty"`
	ClientWorkerID string   `json:"client_worker_id,omitempty"`
	CacheHints     []string `json:"cache_hints,omitempty"`
	// SnapshotID restores a previously saved workspace tarball after create.
	SnapshotID string `json:"snapshot_id,omitempty"`
	// Async returns as soon as the Cell id is allocated (status=creating).
	// Poll GET /v1/cells/{id} until status is ready or error.
	Async bool `json:"async,omitempty"`
}

// SnapshotInfo is returned by POST /v1/cells/{id}/snapshot.
type SnapshotInfo struct {
	ID        string `json:"id"`
	CellID    string `json:"cell_id"`
	Bytes     int64  `json:"bytes"`
	CreatedAt string `json:"created_at"`
}

// CreateTimings breaks down create latency for agents/ops.
type CreateTimings struct {
	FetchMS   int64 `json:"fetch_ms"`
	RuntimeMS int64 `json:"runtime_ms"`
	SetupMS   int64 `json:"setup_ms"`
	TotalMS   int64 `json:"total_ms"`
	// Mirror is how the git bare mirror was obtained: cache | fetch | clone | skip.
	// cache means shared mirror was fresh (CELL_GIT_MIRROR_MAX_AGE) — expect lower fetch_ms over time.
	Mirror string `json:"mirror,omitempty"`
	// Durable env cache mounts (pip/go-mod/ccache/…): already warm / cold / pulled from peer.
	CacheHit  int `json:"cache_hit,omitempty"`
	CacheMiss int `json:"cache_miss,omitempty"`
	CachePull int `json:"cache_pull,omitempty"`
}

// CellInfo is returned by create/get/list.
type CellInfo struct {
	ID          string         `json:"id"`
	WorkerID    string         `json:"worker_id"`
	Environment string         `json:"environment"`
	Status      string         `json:"status"`
	ExpiresAt   time.Time      `json:"expires_at"`
	Error       string         `json:"error,omitempty"`
	Timings     *CreateTimings `json:"timings,omitempty"`
}

// ExecRequest is the body for POST /v1/cells/{id}/exec.
type ExecRequest struct {
	Command        string `json:"command"`
	TimeoutSeconds int    `json:"timeout_seconds,omitempty"`
}

// ExecResult is the response for exec.
type ExecResult struct {
	ExitCode int    `json:"exit_code"`
	Stdout   string `json:"stdout"`
	Stderr   string `json:"stderr"`
}

// RunRequest is POST /v1/run — create, exec commands, destroy.
type RunRequest struct {
	CreateCellRequest
	Commands []string `json:"commands,omitempty"`
}

// RunResult is the response for POST /v1/run.
type RunResult struct {
	ID        string       `json:"id"`
	Results   []ExecResult `json:"results"`
	Destroyed bool         `json:"destroyed"`
}

// ErrorResponse is a standard API error body.
type ErrorResponse struct {
	Error string `json:"error"`
}
