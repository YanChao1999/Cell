// Package api implements the Agent HTTP API on a Unix socket.
package api

import (
	"context"
	"encoding/json"
	"fmt"
	"io"
	"log"
	"net"
	"net/http"
	"os"
	"path/filepath"
	"strings"
	"time"

	cellapi "github.com/YanChao1999/spark/api"
	"github.com/YanChao1999/spark/internal/control/meta"
	"github.com/YanChao1999/spark/internal/control/scheduler"
)

const defaultTTL = 30 * time.Minute

// Server is the control-plane HTTP API.
type Server struct {
	meta      *meta.Store
	scheduler *scheduler.Scheduler
	mux       *http.ServeMux
}

// New constructs the API server.
func New(m *meta.Store, sched *scheduler.Scheduler) *Server {
	s := &Server{meta: m, scheduler: sched, mux: http.NewServeMux()}
	s.routes()
	return s
}

func (s *Server) routes() {
	s.mux.HandleFunc("GET /health", s.handleHealth)
	s.mux.HandleFunc("POST /v1/cells", s.handleCreate)
	s.mux.HandleFunc("GET /v1/cells", s.handleList)
	s.mux.HandleFunc("GET /v1/cells/{id}", s.handleGet)
	s.mux.HandleFunc("DELETE /v1/cells/{id}", s.handleDelete)
	s.mux.HandleFunc("POST /v1/cells/{id}/exec", s.handleExec)
	s.mux.HandleFunc("POST /v1/run", s.handleRun)
}

// Handler returns the HTTP handler.
func (s *Server) Handler() http.Handler { return s.mux }

// ListenAndServe serves on a Unix domain socket path.
func (s *Server) ListenAndServe(socketPath string) error {
	if err := os.MkdirAll(filepath.Dir(socketPath), 0o755); err != nil {
		return err
	}
	_ = os.Remove(socketPath)
	ln, err := net.Listen("unix", socketPath)
	if err != nil {
		return err
	}
	defer func() { _ = ln.Close() }()
	_ = os.Chmod(socketPath, 0o600)
	log.Printf("celld listening on unix://%s", socketPath)
	return http.Serve(ln, s.mux)
}

func (s *Server) handleHealth(w http.ResponseWriter, r *http.Request) {
	writeJSON(w, http.StatusOK, map[string]string{"status": "ok"})
}

func (s *Server) handleCreate(w http.ResponseWriter, r *http.Request) {
	var req cellapi.CreateCellRequest
	if err := json.NewDecoder(r.Body).Decode(&req); err != nil && err != io.EOF {
		writeErr(w, http.StatusBadRequest, err.Error())
		return
	}
	info, err := s.createCell(r.Context(), req)
	if err != nil {
		writeErr(w, http.StatusInternalServerError, err.Error())
		return
	}
	writeJSON(w, http.StatusCreated, info)
}

func (s *Server) createCell(ctx context.Context, req cellapi.CreateCellRequest) (*cellapi.CellInfo, error) {
	worker, err := s.scheduler.Place(req)
	if err != nil {
		return nil, err
	}
	cell, err := worker.CreateCell(ctx, req)
	if err != nil {
		return nil, err
	}
	ttl := parseTTL(req.TTL)
	now := time.Now().UTC()
	rec := &meta.Record{
		ID:          cell.ID,
		WorkerID:    worker.ID(),
		Environment: cell.Environment,
		Status:      cell.Status,
		CreatedAt:   now,
		ExpiresAt:   now.Add(ttl),
		Repo:        req.Repo,
		Ref:         req.Ref,
	}
	if err := s.meta.Put(rec); err != nil {
		_ = worker.DestroyCell(ctx, cell.ID)
		return nil, err
	}
	return recordToInfo(rec), nil
}

func (s *Server) handleList(w http.ResponseWriter, r *http.Request) {
	recs := s.meta.List()
	out := make([]*cellapi.CellInfo, 0, len(recs))
	for _, rec := range recs {
		out = append(out, recordToInfo(rec))
	}
	writeJSON(w, http.StatusOK, out)
}

func (s *Server) handleGet(w http.ResponseWriter, r *http.Request) {
	id := r.PathValue("id")
	rec, ok := s.meta.Get(id)
	if !ok {
		writeErr(w, http.StatusNotFound, "cell not found")
		return
	}
	writeJSON(w, http.StatusOK, recordToInfo(rec))
}

func (s *Server) handleDelete(w http.ResponseWriter, r *http.Request) {
	id := r.PathValue("id")
	if err := s.destroyCell(r.Context(), id); err != nil {
		if strings.Contains(err.Error(), "not found") {
			writeErr(w, http.StatusNotFound, err.Error())
			return
		}
		writeErr(w, http.StatusInternalServerError, err.Error())
		return
	}
	w.WriteHeader(http.StatusNoContent)
}

// DestroyCell is used by the reaper and DELETE handler.
func (s *Server) DestroyCell(ctx context.Context, cellID, workerID string) error {
	return s.destroyCellOn(ctx, cellID, workerID)
}

func (s *Server) destroyCell(ctx context.Context, id string) error {
	rec, ok := s.meta.Get(id)
	if !ok {
		return fmt.Errorf("cell %s not found", id)
	}
	return s.destroyCellOn(ctx, id, rec.WorkerID)
}

func (s *Server) destroyCellOn(ctx context.Context, cellID, workerID string) error {
	if w, ok := s.scheduler.Get(workerID); ok {
		_ = w.DestroyCell(ctx, cellID)
	}
	return s.meta.Delete(cellID)
}

func (s *Server) handleExec(w http.ResponseWriter, r *http.Request) {
	id := r.PathValue("id")
	var req cellapi.ExecRequest
	if err := json.NewDecoder(r.Body).Decode(&req); err != nil {
		writeErr(w, http.StatusBadRequest, err.Error())
		return
	}
	result, err := s.execCell(r.Context(), id, req)
	if err != nil {
		if strings.Contains(err.Error(), "not found") {
			writeErr(w, http.StatusNotFound, err.Error())
			return
		}
		writeErr(w, http.StatusInternalServerError, err.Error())
		return
	}
	writeJSON(w, http.StatusOK, result)
}

func (s *Server) execCell(ctx context.Context, id string, req cellapi.ExecRequest) (*cellapi.ExecResult, error) {
	rec, ok := s.meta.Get(id)
	if !ok {
		return nil, fmt.Errorf("cell %s not found", id)
	}
	w, ok := s.scheduler.Get(rec.WorkerID)
	if !ok {
		return nil, fmt.Errorf("worker %s not found for cell %s", rec.WorkerID, id)
	}
	return w.Exec(ctx, id, req)
}

func (s *Server) handleRun(w http.ResponseWriter, r *http.Request) {
	var req cellapi.RunRequest
	if err := json.NewDecoder(r.Body).Decode(&req); err != nil {
		writeErr(w, http.StatusBadRequest, err.Error())
		return
	}
	info, err := s.createCell(r.Context(), req.CreateCellRequest)
	if err != nil {
		writeErr(w, http.StatusInternalServerError, err.Error())
		return
	}
	results := make([]cellapi.ExecResult, 0, len(req.Commands))
	for _, cmd := range req.Commands {
		res, err := s.execCell(r.Context(), info.ID, cellapi.ExecRequest{Command: cmd})
		if err != nil {
			results = append(results, cellapi.ExecResult{ExitCode: 126, Stderr: err.Error()})
			break
		}
		results = append(results, *res)
		if res.ExitCode != 0 {
			break
		}
	}
	destroyed := true
	if err := s.destroyCell(r.Context(), info.ID); err != nil {
		destroyed = false
	}
	writeJSON(w, http.StatusOK, cellapi.RunResult{
		ID:        info.ID,
		Results:   results,
		Destroyed: destroyed,
	})
}

func recordToInfo(r *meta.Record) *cellapi.CellInfo {
	return &cellapi.CellInfo{
		ID:          r.ID,
		WorkerID:    r.WorkerID,
		Environment: r.Environment,
		Status:      r.Status,
		ExpiresAt:   r.ExpiresAt,
	}
}

func parseTTL(s string) time.Duration {
	if s == "" {
		return defaultTTL
	}
	d, err := time.ParseDuration(s)
	if err != nil || d <= 0 {
		return defaultTTL
	}
	return d
}

func writeJSON(w http.ResponseWriter, status int, v any) {
	w.Header().Set("Content-Type", "application/json")
	w.WriteHeader(status)
	_ = json.NewEncoder(w).Encode(v)
}

func writeErr(w http.ResponseWriter, status int, msg string) {
	writeJSON(w, status, cellapi.ErrorResponse{Error: msg})
}
