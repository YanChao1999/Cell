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
}

// CellInfo is returned by create/get/list.
type CellInfo struct {
	ID          string    `json:"id"`
	WorkerID    string    `json:"worker_id"`
	Environment string    `json:"environment"`
	Status      string    `json:"status"`
	ExpiresAt   time.Time `json:"expires_at"`
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
