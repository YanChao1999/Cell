// Package httpserver exposes LocalWorker over HTTP for remote control planes.
package httpserver

import (
	"encoding/json"
	"io"
	"net"
	"net/http"

	"github.com/YanChao1999/Cell/api"
	"github.com/YanChao1999/Cell/internal/worker/manager"
)

// Server serves worker RPC routes.
type Server struct {
	worker *manager.LocalWorker
	mux    *http.ServeMux
}

// New wraps a LocalWorker.
func New(w *manager.LocalWorker) *Server {
	s := &Server{worker: w, mux: http.NewServeMux()}
	s.routes()
	return s
}

func (s *Server) routes() {
	s.mux.HandleFunc("GET /worker/v1/health", s.handleHealth)
	s.mux.HandleFunc("GET /worker/v1/capacity", s.handleCapacity)
	s.mux.HandleFunc("POST /worker/v1/cells", s.handleCreate)
	s.mux.HandleFunc("POST /worker/v1/cells/{id}/exec", s.handleExec)
	s.mux.HandleFunc("DELETE /worker/v1/cells/{id}", s.handleDestroy)
}

// Handler returns the HTTP handler.
func (s *Server) Handler() http.Handler { return s.mux }

// ListenAndServe serves on TCP addr (e.g. ":8080").
func (s *Server) ListenAndServe(addr string) error {
	ln, err := net.Listen("tcp", addr)
	if err != nil {
		return err
	}
	return http.Serve(ln, s.mux)
}

func (s *Server) handleHealth(w http.ResponseWriter, r *http.Request) {
	writeJSON(w, http.StatusOK, map[string]string{"status": "ok", "worker_id": s.worker.ID()})
}

func (s *Server) handleCapacity(w http.ResponseWriter, r *http.Request) {
	writeJSON(w, http.StatusOK, s.worker.Capacity())
}

func (s *Server) handleCreate(w http.ResponseWriter, r *http.Request) {
	var req api.CreateCellRequest
	if err := json.NewDecoder(r.Body).Decode(&req); err != nil && err != io.EOF {
		writeErr(w, http.StatusBadRequest, err.Error())
		return
	}
	cell, err := s.worker.CreateCell(r.Context(), req)
	if err != nil {
		writeErr(w, http.StatusInternalServerError, err.Error())
		return
	}
	writeJSON(w, http.StatusCreated, cell)
}

func (s *Server) handleExec(w http.ResponseWriter, r *http.Request) {
	id := r.PathValue("id")
	var req api.ExecRequest
	if err := json.NewDecoder(r.Body).Decode(&req); err != nil {
		writeErr(w, http.StatusBadRequest, err.Error())
		return
	}
	res, err := s.worker.Exec(r.Context(), id, req)
	if err != nil {
		writeErr(w, http.StatusInternalServerError, err.Error())
		return
	}
	writeJSON(w, http.StatusOK, res)
}

func (s *Server) handleDestroy(w http.ResponseWriter, r *http.Request) {
	id := r.PathValue("id")
	if err := s.worker.DestroyCell(r.Context(), id); err != nil {
		writeErr(w, http.StatusInternalServerError, err.Error())
		return
	}
	w.WriteHeader(http.StatusNoContent)
}

func writeJSON(w http.ResponseWriter, status int, v any) {
	w.Header().Set("Content-Type", "application/json")
	w.WriteHeader(status)
	_ = json.NewEncoder(w).Encode(v)
}

func writeErr(w http.ResponseWriter, status int, msg string) {
	writeJSON(w, status, api.ErrorResponse{Error: msg})
}
