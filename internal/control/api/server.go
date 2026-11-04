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

	sparkapi "github.com/YanChao1999/spark/api"
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
	s.mux.HandleFunc("POST /v1/sparks", s.handleCreate)
	s.mux.HandleFunc("GET /v1/sparks", s.handleList)
	s.mux.HandleFunc("GET /v1/sparks/{id}", s.handleGet)
	s.mux.HandleFunc("DELETE /v1/sparks/{id}", s.handleDelete)
	s.mux.HandleFunc("POST /v1/sparks/{id}/exec", s.handleExec)
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
	defer ln.Close()
	_ = os.Chmod(socketPath, 0o600)
	log.Printf("sparkd listening on unix://%s", socketPath)
	return http.Serve(ln, s.mux)
}

func (s *Server) handleHealth(w http.ResponseWriter, r *http.Request) {
	writeJSON(w, http.StatusOK, map[string]string{"status": "ok"})
}

func (s *Server) handleCreate(w http.ResponseWriter, r *http.Request) {
	var req sparkapi.CreateSparkRequest
	if err := json.NewDecoder(r.Body).Decode(&req); err != nil && err != io.EOF {
		writeErr(w, http.StatusBadRequest, err.Error())
		return
	}
	info, err := s.createSpark(r.Context(), req)
	if err != nil {
		writeErr(w, http.StatusInternalServerError, err.Error())
		return
	}
	writeJSON(w, http.StatusCreated, info)
}

func (s *Server) createSpark(ctx context.Context, req sparkapi.CreateSparkRequest) (*sparkapi.SparkInfo, error) {
	worker, err := s.scheduler.Place(req)
	if err != nil {
		return nil, err
	}
	spark, err := worker.CreateSpark(ctx, req)
	if err != nil {
		return nil, err
	}
	ttl := parseTTL(req.TTL)
	now := time.Now().UTC()
	rec := &meta.Record{
		ID:          spark.ID,
		WorkerID:    worker.ID(),
		Environment: spark.Environment,
		Status:      spark.Status,
		CreatedAt:   now,
		ExpiresAt:   now.Add(ttl),
		Repo:        req.Repo,
		Ref:         req.Ref,
	}
	if err := s.meta.Put(rec); err != nil {
		_ = worker.DestroySpark(ctx, spark.ID)
		return nil, err
	}
	return recordToInfo(rec), nil
}

func (s *Server) handleList(w http.ResponseWriter, r *http.Request) {
	recs := s.meta.List()
	out := make([]*sparkapi.SparkInfo, 0, len(recs))
	for _, rec := range recs {
		out = append(out, recordToInfo(rec))
	}
	writeJSON(w, http.StatusOK, out)
}

func (s *Server) handleGet(w http.ResponseWriter, r *http.Request) {
	id := r.PathValue("id")
	rec, ok := s.meta.Get(id)
	if !ok {
		writeErr(w, http.StatusNotFound, "spark not found")
		return
	}
	writeJSON(w, http.StatusOK, recordToInfo(rec))
}

func (s *Server) handleDelete(w http.ResponseWriter, r *http.Request) {
	id := r.PathValue("id")
	if err := s.destroySpark(r.Context(), id); err != nil {
		if strings.Contains(err.Error(), "not found") {
			writeErr(w, http.StatusNotFound, err.Error())
			return
		}
		writeErr(w, http.StatusInternalServerError, err.Error())
		return
	}
	w.WriteHeader(http.StatusNoContent)
}

// DestroySpark is used by the reaper and DELETE handler.
func (s *Server) DestroySpark(ctx context.Context, sparkID, workerID string) error {
	return s.destroySparkOn(ctx, sparkID, workerID)
}

func (s *Server) destroySpark(ctx context.Context, id string) error {
	rec, ok := s.meta.Get(id)
	if !ok {
		return fmt.Errorf("spark %s not found", id)
	}
	return s.destroySparkOn(ctx, id, rec.WorkerID)
}

func (s *Server) destroySparkOn(ctx context.Context, sparkID, workerID string) error {
	if w, ok := s.scheduler.Get(workerID); ok {
		_ = w.DestroySpark(ctx, sparkID)
	}
	return s.meta.Delete(sparkID)
}

func (s *Server) handleExec(w http.ResponseWriter, r *http.Request) {
	id := r.PathValue("id")
	var req sparkapi.ExecRequest
	if err := json.NewDecoder(r.Body).Decode(&req); err != nil {
		writeErr(w, http.StatusBadRequest, err.Error())
		return
	}
	result, err := s.execSpark(r.Context(), id, req)
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

func (s *Server) execSpark(ctx context.Context, id string, req sparkapi.ExecRequest) (*sparkapi.ExecResult, error) {
	rec, ok := s.meta.Get(id)
	if !ok {
		return nil, fmt.Errorf("spark %s not found", id)
	}
	w, ok := s.scheduler.Get(rec.WorkerID)
	if !ok {
		return nil, fmt.Errorf("worker %s not found for spark %s", rec.WorkerID, id)
	}
	return w.Exec(ctx, id, req)
}

func (s *Server) handleRun(w http.ResponseWriter, r *http.Request) {
	var req sparkapi.RunRequest
	if err := json.NewDecoder(r.Body).Decode(&req); err != nil {
		writeErr(w, http.StatusBadRequest, err.Error())
		return
	}
	info, err := s.createSpark(r.Context(), req.CreateSparkRequest)
	if err != nil {
		writeErr(w, http.StatusInternalServerError, err.Error())
		return
	}
	results := make([]sparkapi.ExecResult, 0, len(req.Commands))
	for _, cmd := range req.Commands {
		res, err := s.execSpark(r.Context(), info.ID, sparkapi.ExecRequest{Command: cmd})
		if err != nil {
			results = append(results, sparkapi.ExecResult{ExitCode: 126, Stderr: err.Error()})
			break
		}
		results = append(results, *res)
		if res.ExitCode != 0 {
			break
		}
	}
	destroyed := true
	if err := s.destroySpark(r.Context(), info.ID); err != nil {
		destroyed = false
	}
	writeJSON(w, http.StatusOK, sparkapi.RunResult{
		ID:        info.ID,
		Results:   results,
		Destroyed: destroyed,
	})
}

func recordToInfo(r *meta.Record) *sparkapi.SparkInfo {
	return &sparkapi.SparkInfo{
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
	writeJSON(w, status, sparkapi.ErrorResponse{Error: msg})
}
