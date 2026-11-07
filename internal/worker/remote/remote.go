// Package remote implements workerapi.Worker over HTTP for Phase 5 clusters.
package remote

import (
	"bytes"
	"context"
	"encoding/json"
	"fmt"
	"io"
	"net/http"
	"strings"
	"sync"
	"time"

	"github.com/YanChao1999/Cell/api"
	"github.com/YanChao1999/Cell/internal/workerapi"
)

// Worker is a client for a remote celld --mode=worker endpoint.
type Worker struct {
	id       string
	endpoint string // http://host:port (no trailing slash)
	client   *http.Client

	mu        sync.Mutex
	maxCells  int
	usedCells int
	cacheKeys []string
}

// New returns a remote worker client. endpoint must be http(s)://...
func New(id, endpoint string) (*Worker, error) {
	endpoint = strings.TrimRight(strings.TrimSpace(endpoint), "/")
	if id == "" {
		return nil, fmt.Errorf("remote: id required")
	}
	if !strings.HasPrefix(endpoint, "http://") && !strings.HasPrefix(endpoint, "https://") {
		return nil, fmt.Errorf("remote: endpoint must be http(s), got %q", endpoint)
	}
	return &Worker{
		id:       id,
		endpoint: endpoint,
		client:   &http.Client{Timeout: 0}, // long create/setup/exec; server enforces budgets
		maxCells: 1,
	}, nil
}

func (w *Worker) ID() string { return w.id }

// UpdateCapacity refreshes placement stats from registry heartbeats.
func (w *Worker) UpdateCapacity(max, used int, cacheKeys []string) {
	w.mu.Lock()
	defer w.mu.Unlock()
	if max > 0 {
		w.maxCells = max
	}
	w.usedCells = used
	if cacheKeys != nil {
		w.cacheKeys = append([]string(nil), cacheKeys...)
	}
}

func (w *Worker) Capacity() workerapi.Capacity {
	w.mu.Lock()
	defer w.mu.Unlock()
	return workerapi.Capacity{MaxCells: w.maxCells, UsedCells: w.usedCells}
}

func (w *Worker) CacheIndex() workerapi.CacheIndex {
	w.mu.Lock()
	defer w.mu.Unlock()
	return mapIndex(w.cacheKeys)
}

func (w *Worker) CreateCell(ctx context.Context, req api.CreateCellRequest) (*workerapi.Cell, error) {
	var out workerapi.Cell
	if err := w.doJSON(ctx, http.MethodPost, "/worker/v1/cells", req, &out); err != nil {
		return nil, err
	}
	w.mu.Lock()
	w.usedCells++
	w.mu.Unlock()
	return &out, nil
}

func (w *Worker) Exec(ctx context.Context, cellID string, req api.ExecRequest) (*api.ExecResult, error) {
	var out api.ExecResult
	if err := w.doJSON(ctx, http.MethodPost, "/worker/v1/cells/"+cellID+"/exec", req, &out); err != nil {
		return nil, err
	}
	return &out, nil
}

func (w *Worker) DestroyCell(ctx context.Context, cellID string) error {
	err := w.doJSON(ctx, http.MethodDelete, "/worker/v1/cells/"+cellID, nil, nil)
	if err == nil {
		w.mu.Lock()
		if w.usedCells > 0 {
			w.usedCells--
		}
		w.mu.Unlock()
	}
	return err
}

func (w *Worker) doJSON(ctx context.Context, method, path string, body any, out any) error {
	var rdr io.Reader
	if body != nil {
		b, err := json.Marshal(body)
		if err != nil {
			return err
		}
		rdr = bytes.NewReader(b)
	}
	req, err := http.NewRequestWithContext(ctx, method, w.endpoint+path, rdr)
	if err != nil {
		return err
	}
	if body != nil {
		req.Header.Set("Content-Type", "application/json")
	}
	// Dial stays bounded even when overall Timeout is 0.
	if w.client.Transport == nil {
		w.client.Transport = &http.Transport{
			ResponseHeaderTimeout: 0,
			IdleConnTimeout:       90 * time.Second,
		}
	}
	resp, err := w.client.Do(req)
	if err != nil {
		return err
	}
	defer func() { _ = resp.Body.Close() }()
	data, _ := io.ReadAll(resp.Body)
	if resp.StatusCode >= 300 {
		var er api.ErrorResponse
		_ = json.Unmarshal(data, &er)
		if er.Error != "" {
			return fmt.Errorf("remote %s: %s", w.id, er.Error)
		}
		return fmt.Errorf("remote %s: %s: %s", w.id, resp.Status, data)
	}
	if out == nil || resp.StatusCode == http.StatusNoContent || len(data) == 0 {
		return nil
	}
	return json.Unmarshal(data, out)
}

type mapIndex []string

func (m mapIndex) Has(key string) bool {
	for _, k := range m {
		if k == key {
			return true
		}
	}
	return false
}

func (m mapIndex) Keys() []string { return append([]string(nil), m...) }

var _ workerapi.Worker = (*Worker)(nil)
