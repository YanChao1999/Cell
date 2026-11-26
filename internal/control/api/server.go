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

	cellapi "github.com/YanChao1999/Cell/api"
	"github.com/YanChao1999/Cell/internal/control/meta"
	"github.com/YanChao1999/Cell/internal/control/metrics"
	"github.com/YanChao1999/Cell/internal/control/registry"
	"github.com/YanChao1999/Cell/internal/control/scheduler"
	"github.com/YanChao1999/Cell/internal/worker/auth"
	"github.com/YanChao1999/Cell/internal/worker/remote"
	"github.com/YanChao1999/Cell/internal/workerapi"
)

const defaultTTL = 30 * time.Minute

// Server is the control-plane HTTP API.
type Server struct {
	meta      *meta.Store
	scheduler *scheduler.Scheduler
	registry  *registry.Registry
	issuer    *auth.Issuer
	metrics   *metrics.Registry
	mux       *http.ServeMux
}

// Options configure the control API.
type Options struct {
	WorkerToken     string // long-lived HMAC/bootstrap secret
	WorkerTokenPrev string // optional previous secret for rotation
	Issuer          *auth.Issuer
	Metrics         *metrics.Registry
}

// New constructs the API server (Phase 5 worker registry enabled).
func New(m *meta.Store, sched *scheduler.Scheduler) *Server {
	return NewWithOptions(m, sched, registry.New(45*time.Second), Options{})
}

// NewWithRegistry constructs the API server with an explicit worker registry.
func NewWithRegistry(m *meta.Store, sched *scheduler.Scheduler, reg *registry.Registry) *Server {
	return NewWithOptions(m, sched, reg, Options{})
}

// NewWithOptions constructs the API server with registry and auth options.
func NewWithOptions(m *meta.Store, sched *scheduler.Scheduler, reg *registry.Registry, opts Options) *Server {
	iss := opts.Issuer
	if iss == nil {
		iss = auth.NewIssuer(opts.WorkerToken, opts.WorkerTokenPrev)
	}
	met := opts.Metrics
	if met == nil {
		met = metrics.New()
	}
	s := &Server{meta: m, scheduler: sched, registry: reg, issuer: iss, metrics: met, mux: http.NewServeMux()}
	s.routes()
	return s
}

// Registry returns the worker registry (may be nil).
func (s *Server) Registry() *registry.Registry { return s.registry }

func (s *Server) routes() {
	s.mux.HandleFunc("GET /health", s.handleHealth)
	s.mux.HandleFunc("GET /metrics", s.handleMetrics)
	s.mux.Handle("GET /metrics/ui", metrics.UIHandler())
	s.mux.HandleFunc("POST /v1/auth/token", s.handleIssueToken)
	s.mux.HandleFunc("POST /v1/cells", s.handleCreate)
	s.mux.HandleFunc("GET /v1/cells", s.handleList)
	s.mux.HandleFunc("GET /v1/cells/{id}", s.handleGet)
	s.mux.HandleFunc("DELETE /v1/cells/{id}", s.handleDelete)
	s.mux.HandleFunc("POST /v1/cells/{id}/exec", s.handleExec)
	s.mux.HandleFunc("PUT /v1/cells/{id}/workspace", s.handleWorkspacePut)
	s.mux.HandleFunc("POST /v1/cells/{id}/snapshot", s.handleSnapshot)
	s.mux.HandleFunc("POST /v1/run", s.handleRun)
	s.mux.HandleFunc("POST /v1/workers/register", s.handleWorkerRegister)
	s.mux.HandleFunc("POST /v1/workers/{id}/heartbeat", s.handleWorkerHeartbeat)
	s.mux.HandleFunc("GET /v1/workers", s.handleWorkerList)
}

// Handler returns the HTTP handler.
func (s *Server) Handler() http.Handler { return s.mux }

// Issuer returns the auth issuer (may be disabled).
func (s *Server) Issuer() *auth.Issuer { return s.issuer }

// ListenAndServe serves on a Unix domain socket path.
func (s *Server) ListenAndServe(socketPath string) error {
	if err := os.MkdirAll(filepath.Dir(socketPath), 0o755); err != nil {
		return err
	}
	if err := removeStaleSocket(socketPath); err != nil {
		return err
	}
	ln, err := net.Listen("unix", socketPath)
	if err != nil {
		return err
	}
	defer func() { _ = ln.Close() }()
	_ = os.Chmod(socketPath, 0o600)
	log.Printf("celld listening on unix://%s", socketPath)
	return http.Serve(ln, s.Handler())
}

// ListenAndServeHTTP serves the control API on a TCP address (e.g. ":8080").
// Agent routes (/v1/cells*, /v1/run) require Bearer auth; refuses to listen
// when CELL_WORKER_TOKEN / issuer is unset so the Agent API is not open.
func (s *Server) ListenAndServeHTTP(addr string) error {
	if s.issuer == nil || !s.issuer.Enabled() {
		return fmt.Errorf("listen-http requires CELL_WORKER_TOKEN (or --worker-token): Agent API must not be unauthenticated on TCP")
	}
	ln, err := net.Listen("tcp", addr)
	if err != nil {
		return err
	}
	log.Printf("celld listening on http://%s (agent API auth required)", ln.Addr().String())
	return http.Serve(ln, s.HandlerHTTP())
}

// removeStaleSocket deletes only a dead socket. A live listener is left alone
// so a second celld cannot hijack the first daemon's endpoint.
func removeStaleSocket(socketPath string) error {
	fi, err := os.Lstat(socketPath)
	if err != nil {
		if os.IsNotExist(err) {
			return nil
		}
		return err
	}
	if fi.Mode()&os.ModeSocket == 0 {
		return fmt.Errorf("socket path %s exists and is not a unix socket", socketPath)
	}
	conn, err := net.DialTimeout("unix", socketPath, 500*time.Millisecond)
	if err == nil {
		_ = conn.Close()
		return fmt.Errorf("celld already listening on %s", socketPath)
	}
	if err := os.Remove(socketPath); err != nil && !os.IsNotExist(err) {
		return err
	}
	return nil
}

func (s *Server) handleHealth(w http.ResponseWriter, r *http.Request) {
	writeJSON(w, http.StatusOK, map[string]string{"status": "ok"})
}

type issueTokenRequest struct {
	Scope string `json:"scope"`
	TTL   string `json:"ttl,omitempty"` // e.g. "15m"
}

type issueTokenResponse struct {
	Token     string    `json:"token"`
	Scope     string    `json:"scope"`
	ExpiresAt time.Time `json:"expires_at"`
}

func (s *Server) handleIssueToken(w http.ResponseWriter, r *http.Request) {
	if s.issuer == nil || !s.issuer.Enabled() {
		writeErr(w, http.StatusNotImplemented, "auth disabled")
		return
	}
	// Bootstrap with static secret or any valid worker-scoped credential.
	if !auth.Authorize(r, s.issuer, auth.ScopeWorker) {
		writeErr(w, http.StatusUnauthorized, "unauthorized")
		return
	}
	var req issueTokenRequest
	if err := json.NewDecoder(r.Body).Decode(&req); err != nil && err != io.EOF {
		writeErr(w, http.StatusBadRequest, err.Error())
		return
	}
	scope := strings.TrimSpace(req.Scope)
	if scope == "" {
		scope = auth.ScopeWorker
	}
	ttl := auth.DefaultTTL
	if req.TTL != "" {
		d, err := time.ParseDuration(req.TTL)
		if err != nil {
			writeErr(w, http.StatusBadRequest, "invalid ttl")
			return
		}
		ttl = d
	}
	tok, exp, err := s.issuer.Issue(scope, ttl)
	if err != nil {
		writeErr(w, http.StatusBadRequest, err.Error())
		return
	}
	writeJSON(w, http.StatusOK, issueTokenResponse{Token: tok, Scope: scope, ExpiresAt: exp})
}

func (s *Server) handleMetrics(w http.ResponseWriter, r *http.Request) {
	s.syncWorkerMetrics()
	s.metrics.Handler().ServeHTTP(w, r)
}

func (s *Server) syncWorkerMetrics() {
	if s.registry == nil || s.metrics == nil {
		return
	}
	list := s.registry.List()
	samples := make([]metrics.WorkerHost, 0, len(list))
	for _, info := range list {
		samples = append(samples, metrics.WorkerHost{
			ID:             info.ID,
			CPURatio:       info.Host.CPURatio,
			MemUsedBytes:   info.Host.MemUsedBytes,
			MemTotalBytes:  info.Host.MemTotalBytes,
			DiskUsedBytes:  info.Host.DiskUsedBytes,
			DiskTotalBytes: info.Host.DiskTotalBytes,
			CellsUsed:      info.UsedCells,
			CellsMax:       info.MaxCells,
		})
	}
	s.metrics.SyncWorkers(samples)
}

func (s *Server) handleWorkerRegister(w http.ResponseWriter, r *http.Request) {
	if s.registry == nil {
		writeErr(w, http.StatusNotImplemented, "worker registry disabled")
		return
	}
	if !auth.Authorize(r, s.issuer, auth.ScopeWorker) {
		writeErr(w, http.StatusUnauthorized, "unauthorized")
		return
	}
	var req registry.RegisterRequest
	if err := json.NewDecoder(r.Body).Decode(&req); err != nil && err != io.EOF {
		writeErr(w, http.StatusBadRequest, err.Error())
		return
	}
	info, err := s.registry.Register(req)
	if err != nil {
		writeErr(w, http.StatusBadRequest, err.Error())
		return
	}
	if err := s.syncRemoteWorker(info); err != nil {
		writeErr(w, http.StatusBadRequest, err.Error())
		return
	}
	s.metrics.ObserveWorker(metrics.WorkerHost{
		ID: info.ID, CPURatio: info.Host.CPURatio,
		MemUsedBytes: info.Host.MemUsedBytes, MemTotalBytes: info.Host.MemTotalBytes,
		DiskUsedBytes: info.Host.DiskUsedBytes, DiskTotalBytes: info.Host.DiskTotalBytes,
		CellsUsed: info.UsedCells, CellsMax: info.MaxCells,
	})
	writeJSON(w, http.StatusCreated, info)
}

func (s *Server) handleWorkerHeartbeat(w http.ResponseWriter, r *http.Request) {
	if s.registry == nil {
		writeErr(w, http.StatusNotImplemented, "worker registry disabled")
		return
	}
	if !auth.Authorize(r, s.issuer, auth.ScopeWorker) {
		writeErr(w, http.StatusUnauthorized, "unauthorized")
		return
	}
	id := r.PathValue("id")
	var req registry.HeartbeatRequest
	if err := json.NewDecoder(r.Body).Decode(&req); err != nil && err != io.EOF {
		writeErr(w, http.StatusBadRequest, err.Error())
		return
	}
	info, err := s.registry.Heartbeat(id, req)
	if err != nil {
		writeErr(w, http.StatusNotFound, err.Error())
		return
	}
	_ = s.syncRemoteWorker(info)
	s.metrics.ObserveWorker(metrics.WorkerHost{
		ID: info.ID, CPURatio: info.Host.CPURatio,
		MemUsedBytes: info.Host.MemUsedBytes, MemTotalBytes: info.Host.MemTotalBytes,
		DiskUsedBytes: info.Host.DiskUsedBytes, DiskTotalBytes: info.Host.DiskTotalBytes,
		CellsUsed: info.UsedCells, CellsMax: info.MaxCells,
	})
	writeJSON(w, http.StatusOK, info)
}

// SyncRemoteWorker ensures HTTP-endpoint workers are schedulable for Place/Exec.
func (s *Server) SyncRemoteWorker(info *registry.Info) error {
	return s.syncRemoteWorker(info)
}

// syncRemoteWorker ensures HTTP-endpoint workers are schedulable for Place/Exec.
func (s *Server) syncRemoteWorker(info *registry.Info) error {
	if info == nil || s.scheduler == nil {
		return nil
	}
	ep := strings.TrimSpace(info.Endpoint)
	if !strings.HasPrefix(ep, "http://") && !strings.HasPrefix(ep, "https://") {
		return nil // unix/local workers are already in the scheduler process
	}
	tok := s.workerRPCToken()
	if existing, ok := s.scheduler.Get(info.ID); ok {
		if rw, ok := existing.(*remote.Worker); ok {
			rw.UpdateCapacity(info.MaxCells, info.UsedCells, info.CacheKeys)
			if tok != "" {
				rw.SetToken(tok)
			}
		}
		return nil
	}
	rw, err := remote.New(info.ID, ep, remote.Options{Token: tok})
	if err != nil {
		return err
	}
	rw.UpdateCapacity(info.MaxCells, info.UsedCells, info.CacheKeys)
	s.scheduler.Register(rw)
	log.Printf("api: scheduled remote worker id=%s endpoint=%s", info.ID, ep)
	return nil
}

func (s *Server) workerRPCToken() string {
	if s.issuer == nil || !s.issuer.Enabled() {
		return ""
	}
	tok, _, err := s.issuer.Issue(auth.ScopeWorker, auth.DefaultTTL)
	if err != nil {
		return s.issuer.Secret()
	}
	return tok
}

func (s *Server) handleWorkerList(w http.ResponseWriter, r *http.Request) {
	if s.registry == nil {
		writeJSON(w, http.StatusOK, []registry.Info{})
		return
	}
	writeJSON(w, http.StatusOK, s.registry.List())
}

func (s *Server) handleCreate(w http.ResponseWriter, r *http.Request) {
	var req cellapi.CreateCellRequest
	if err := json.NewDecoder(r.Body).Decode(&req); err != nil && err != io.EOF {
		writeErr(w, http.StatusBadRequest, err.Error())
		return
	}
	info, err := s.createCell(r.Context(), req, agentOwnerFingerprint(r))
	if err != nil {
		writeErr(w, http.StatusInternalServerError, err.Error())
		return
	}
	writeJSON(w, http.StatusCreated, info)
}

func (s *Server) createCell(ctx context.Context, req cellapi.CreateCellRequest, owner string) (*cellapi.CellInfo, error) {
	start := time.Now()
	info, err := s.createCellInner(ctx, req, owner)
	withRepo := strings.TrimSpace(req.Repo) != ""
	// Async creates finish later — record wall time only for sync path here.
	if !req.Async {
		s.metrics.ObserveCreate(time.Since(start), err, withRepo)
		if err == nil && info != nil && info.Timings != nil {
			s.metrics.ObserveCreatePhases(info.Timings)
		}
	} else if err != nil {
		s.metrics.ObserveCreate(time.Since(start), err, withRepo)
	}
	return info, err
}

func (s *Server) createCellInner(ctx context.Context, req cellapi.CreateCellRequest, owner string) (*cellapi.CellInfo, error) {
	if req.SnapshotID != "" && !validSnapshotID(req.SnapshotID) {
		return nil, fmt.Errorf("invalid snapshot_id")
	}
	worker, err := s.scheduler.Place(req)
	if err != nil {
		return nil, err
	}
	cell, err := worker.CreateCell(ctx, req)
	if err != nil {
		return nil, err
	}
	// Snapshot restore needs a ready runtime; wait if create was async.
	if req.SnapshotID != "" {
		if cell.Status != "ready" {
			if err := s.waitWorkerReady(ctx, worker, cell); err != nil {
				_ = worker.DestroyCell(ctx, cell.ID)
				return nil, err
			}
		}
		if err := s.restoreSnapshot(ctx, worker, cell.ID, req.SnapshotID, owner); err != nil {
			_ = worker.DestroyCell(ctx, cell.ID)
			return nil, err
		}
		cell.Status = "ready"
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
		Error:       cell.Error,
	}
	applyTimings(rec, cell.Timings)
	if err := s.meta.Put(rec); err != nil {
		_ = worker.DestroyCell(ctx, cell.ID)
		return nil, err
	}
	return recordToInfo(rec), nil
}

func (s *Server) waitWorkerReady(ctx context.Context, worker workerapi.Worker, cell *workerapi.Cell) error {
	for {
		if cell.Status == "ready" {
			return nil
		}
		if cell.Status == "error" {
			if cell.Error != "" {
				return fmt.Errorf("%s", cell.Error)
			}
			return fmt.Errorf("cell %s create failed", cell.ID)
		}
		select {
		case <-ctx.Done():
			return ctx.Err()
		case <-time.After(200 * time.Millisecond):
		}
		fresh, err := worker.GetCell(ctx, cell.ID)
		if err != nil {
			return err
		}
		*cell = *fresh
	}
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
	info, err := s.refreshCell(r.Context(), rec)
	if err != nil {
		// Meta is still authoritative if the worker is briefly unreachable.
		writeJSON(w, http.StatusOK, recordToInfo(rec))
		return
	}
	writeJSON(w, http.StatusOK, info)
}

// refreshCell pulls live status/timings from the worker for in-flight creates.
func (s *Server) refreshCell(ctx context.Context, rec *meta.Record) (*cellapi.CellInfo, error) {
	if rec.Status == "ready" || rec.Status == "error" {
		return recordToInfo(rec), nil
	}
	w, ok := s.scheduler.Get(rec.WorkerID)
	if !ok {
		return recordToInfo(rec), nil
	}
	cell, err := w.GetCell(ctx, rec.ID)
	if err != nil {
		return nil, err
	}
	prev := rec.Status
	rec.Status = cell.Status
	rec.Error = cell.Error
	applyTimings(rec, cell.Timings)
	_ = s.meta.Put(rec)
	// First transition to terminal state → record create metrics.
	if prev != "ready" && prev != "error" && (cell.Status == "ready" || cell.Status == "error") {
		var cerr error
		if cell.Status == "error" {
			cerr = fmt.Errorf("%s", cell.Error)
		}
		total := time.Duration(rec.TotalMS) * time.Millisecond
		if total <= 0 {
			total = time.Since(rec.CreatedAt)
		}
		s.metrics.ObserveCreate(total, cerr, strings.TrimSpace(rec.Repo) != "")
		if cell.Timings != nil {
			s.metrics.ObserveCreatePhases(cell.Timings)
		}
	}
	return recordToInfo(rec), nil
}

func (s *Server) handleWorkspacePut(w http.ResponseWriter, r *http.Request) {
	id := r.PathValue("id")
	rec, ok := s.meta.Get(id)
	if !ok {
		writeErr(w, http.StatusNotFound, "cell not found")
		return
	}
	// Ensure create finished before importing a dirty tree.
	if _, err := s.refreshCell(r.Context(), rec); err == nil {
		rec, _ = s.meta.Get(id)
	}
	if rec != nil && rec.Status != "ready" {
		if rec.Status == "error" {
			writeErr(w, http.StatusConflict, "cell create failed: "+rec.Error)
			return
		}
		writeErr(w, http.StatusConflict, "cell not ready (status="+rec.Status+")")
		return
	}
	worker, ok := s.scheduler.Get(rec.WorkerID)
	if !ok {
		writeErr(w, http.StatusInternalServerError, "worker not available")
		return
	}
	arch, ok := worker.(workerapi.WorkspaceArchive)
	if !ok {
		writeErr(w, http.StatusNotImplemented, "worker does not support workspace import")
		return
	}
	if err := arch.UnpackWorkspace(r.Context(), id, r.Body); err != nil {
		writeErr(w, http.StatusInternalServerError, err.Error())
		return
	}
	w.WriteHeader(http.StatusNoContent)
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
	start := time.Now()
	err := s.destroyCellOnInner(ctx, cellID, workerID)
	s.metrics.ObserveDestroy(time.Since(start), err)
	return err
}

func (s *Server) destroyCellOnInner(ctx context.Context, cellID, workerID string) error {
	w, ok := s.scheduler.Get(workerID)
	if !ok {
		return fmt.Errorf("worker %s not found for cell %s", workerID, cellID)
	}
	if err := w.DestroyCell(ctx, cellID); err != nil {
		return err
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
	start := time.Now()
	res, err := s.execCellInner(ctx, id, req)
	s.metrics.ObserveExec(time.Since(start), err)
	return res, err
}

func (s *Server) execCellInner(ctx context.Context, id string, req cellapi.ExecRequest) (*cellapi.ExecResult, error) {
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
	info, err := s.createCell(r.Context(), req.CreateCellRequest, agentOwnerFingerprint(r))
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
	// Cleanup must outlive a canceled client request so containers are not left behind.
	cleanupCtx, cancel := context.WithTimeout(context.Background(), 2*time.Minute)
	defer cancel()
	destroyed := true
	if err := s.destroyCell(cleanupCtx, info.ID); err != nil {
		destroyed = false
		log.Printf("api: /v1/run cleanup for %s: %v", info.ID, err)
	}
	writeJSON(w, http.StatusOK, cellapi.RunResult{
		ID:        info.ID,
		Results:   results,
		Destroyed: destroyed,
	})
}

func recordToInfo(r *meta.Record) *cellapi.CellInfo {
	info := &cellapi.CellInfo{
		ID:          r.ID,
		WorkerID:    r.WorkerID,
		Environment: r.Environment,
		Status:      r.Status,
		ExpiresAt:   r.ExpiresAt,
		Error:       r.Error,
	}
	if r.FetchMS > 0 || r.RuntimeMS > 0 || r.SetupMS > 0 || r.TotalMS > 0 ||
		r.Mirror != "" || r.CacheHit > 0 || r.CacheMiss > 0 || r.CachePull > 0 {
		info.Timings = &cellapi.CreateTimings{
			FetchMS:   r.FetchMS,
			RuntimeMS: r.RuntimeMS,
			SetupMS:   r.SetupMS,
			TotalMS:   r.TotalMS,
			Mirror:    r.Mirror,
			CacheHit:  r.CacheHit,
			CacheMiss: r.CacheMiss,
			CachePull: r.CachePull,
		}
	}
	return info
}

func applyTimings(rec *meta.Record, t *cellapi.CreateTimings) {
	if rec == nil || t == nil {
		return
	}
	rec.FetchMS = t.FetchMS
	rec.RuntimeMS = t.RuntimeMS
	rec.SetupMS = t.SetupMS
	rec.TotalMS = t.TotalMS
	rec.Mirror = t.Mirror
	rec.CacheHit = t.CacheHit
	rec.CacheMiss = t.CacheMiss
	rec.CachePull = t.CachePull
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
