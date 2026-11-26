// Package metrics exposes a tiny Prometheus text registry for Cell control plane.
package metrics

import (
	"fmt"
	"net/http"
	"sort"
	"strings"
	"sync"
	"sync/atomic"
	"time"

	"github.com/YanChao1999/Cell/api"
)

// WorkerHost is the latest heartbeat resource sample for one worker.
type WorkerHost struct {
	ID             string
	CPURatio       float64
	MemUsedBytes   uint64
	MemTotalBytes  uint64
	DiskUsedBytes  uint64
	DiskTotalBytes uint64
	CellsUsed      int
	CellsMax       int
}

// Registry holds process counters and latency sums (no external deps).
type Registry struct {
	createOK, createErr   atomic.Uint64
	execOK, execErr       atomic.Uint64
	destroyOK, destroyErr atomic.Uint64
	createSumUs, createN  atomic.Uint64
	execSumUs, execN      atomic.Uint64

	// Create latency split by whether CreateCellRequest.Repo was set.
	createRepoSumUs, createRepoN     atomic.Uint64
	createNoRepoSumUs, createNoRepoN atomic.Uint64

	// Create phase latency: git fetch vs Docker/runtime start vs setup.
	fetchSumUs, fetchN     atomic.Uint64
	runtimeSumUs, runtimeN atomic.Uint64
	setupSumUs, setupN     atomic.Uint64

	// Shared-cache reuse: git mirror TTL hits and durable mount warm hits.
	gitMirrorCache, gitMirrorFetch, gitMirrorClone atomic.Uint64
	cacheMountHit, cacheMountMiss, cacheMountPull  atomic.Uint64

	workerMu sync.Mutex
	workers  map[string]WorkerHost
}

// New returns an empty registry.
func New() *Registry { return &Registry{workers: make(map[string]WorkerHost)} }

// SyncWorkers replaces the per-worker host gauge set (from registry heartbeats).
func (r *Registry) SyncWorkers(samples []WorkerHost) {
	if r == nil {
		return
	}
	r.workerMu.Lock()
	defer r.workerMu.Unlock()
	r.workers = make(map[string]WorkerHost, len(samples))
	for _, s := range samples {
		if s.ID == "" {
			continue
		}
		r.workers[s.ID] = s
	}
}

// ObserveWorker upserts one worker's latest host sample.
func (r *Registry) ObserveWorker(s WorkerHost) {
	if r == nil || s.ID == "" {
		return
	}
	r.workerMu.Lock()
	defer r.workerMu.Unlock()
	if r.workers == nil {
		r.workers = make(map[string]WorkerHost)
	}
	r.workers[s.ID] = s
}

// ObserveCreate records a create attempt.
// withRepo should be true when the request included a non-empty repo URL
// (git mirror/worktree path); false for empty-workspace creates.
func (r *Registry) ObserveCreate(d time.Duration, err error, withRepo bool) {
	if r == nil {
		return
	}
	r.observe(d, err, &r.createSumUs, &r.createN, &r.createOK, &r.createErr)
	if withRepo {
		r.createRepoSumUs.Add(uint64(d / time.Microsecond))
		r.createRepoN.Add(1)
	} else {
		r.createNoRepoSumUs.Add(uint64(d / time.Microsecond))
		r.createNoRepoN.Add(1)
	}
}

// ObserveCreatePhases records fetch vs runtime vs setup breakdown (ms → µs)
// plus git-mirror and durable-cache reuse counters.
func (r *Registry) ObserveCreatePhases(t *api.CreateTimings) {
	if r == nil || t == nil {
		return
	}
	if t.FetchMS > 0 {
		r.fetchSumUs.Add(uint64(t.FetchMS) * 1000)
		r.fetchN.Add(1)
	}
	if t.RuntimeMS > 0 {
		r.runtimeSumUs.Add(uint64(t.RuntimeMS) * 1000)
		r.runtimeN.Add(1)
	}
	if t.SetupMS > 0 {
		r.setupSumUs.Add(uint64(t.SetupMS) * 1000)
		r.setupN.Add(1)
	}
	switch t.Mirror {
	case "cache":
		r.gitMirrorCache.Add(1)
	case "fetch":
		r.gitMirrorFetch.Add(1)
	case "clone":
		r.gitMirrorClone.Add(1)
	}
	if t.CacheHit > 0 {
		r.cacheMountHit.Add(uint64(t.CacheHit))
	}
	if t.CacheMiss > 0 {
		r.cacheMountMiss.Add(uint64(t.CacheMiss))
	}
	if t.CachePull > 0 {
		r.cacheMountPull.Add(uint64(t.CachePull))
	}
}

// ObserveExec records an exec attempt.
func (r *Registry) ObserveExec(d time.Duration, err error) {
	r.observe(d, err, &r.execSumUs, &r.execN, &r.execOK, &r.execErr)
}

func (r *Registry) observe(d time.Duration, err error, sumUs, n, ok, fail *atomic.Uint64) {
	if r == nil {
		return
	}
	sumUs.Add(uint64(d / time.Microsecond))
	n.Add(1)
	if err != nil {
		fail.Add(1)
		return
	}
	ok.Add(1)
}

// ObserveDestroy records a destroy attempt.
func (r *Registry) ObserveDestroy(d time.Duration, err error) {
	if r == nil {
		return
	}
	if err != nil {
		r.destroyErr.Add(1)
		return
	}
	r.destroyOK.Add(1)
}

// Handler serves Prometheus text exposition at GET /metrics.
func (r *Registry) Handler() http.Handler {
	return http.HandlerFunc(func(w http.ResponseWriter, _ *http.Request) {
		w.Header().Set("Content-Type", "text/plain; version=0.0.4; charset=utf-8")
		var b strings.Builder
		writeCounter(&b, "cell_create_total", "Cell create requests", map[string]uint64{
			"ok": r.createOK.Load(), "error": r.createErr.Load(),
		})
		writeCounter(&b, "cell_exec_total", "Cell exec requests", map[string]uint64{
			"ok": r.execOK.Load(), "error": r.execErr.Load(),
		})
		writeCounter(&b, "cell_destroy_total", "Cell destroy requests", map[string]uint64{
			"ok": r.destroyOK.Load(), "error": r.destroyErr.Load(),
		})
		writeLatency(&b, "cell_create", r.createSumUs.Load(), r.createN.Load())
		writeLatencyLabeled(&b, "cell_create", "repo", "yes", r.createRepoSumUs.Load(), r.createRepoN.Load())
		writeLatencyLabeled(&b, "cell_create", "repo", "no", r.createNoRepoSumUs.Load(), r.createNoRepoN.Load())
		writePhaseLatency(&b, r.fetchSumUs.Load(), r.fetchN.Load(), r.runtimeSumUs.Load(), r.runtimeN.Load(), r.setupSumUs.Load(), r.setupN.Load())
		writeCounter(&b, "cell_git_mirror_total", "Git bare-mirror outcome (cache=TTL hit; longer uptime → more cache)", map[string]uint64{
			"cache": r.gitMirrorCache.Load(),
			"fetch": r.gitMirrorFetch.Load(),
			"clone": r.gitMirrorClone.Load(),
		})
		writeCounter(&b, "cell_cache_mount_total", "Durable env cache mounts (hit=already warm shared dir)", map[string]uint64{
			"hit":  r.cacheMountHit.Load(),
			"miss": r.cacheMountMiss.Load(),
			"pull": r.cacheMountPull.Load(),
		})
		writeLatency(&b, "cell_exec", r.execSumUs.Load(), r.execN.Load())
		r.writeWorkerGauges(&b)
		_, _ = w.Write([]byte(b.String()))
	})
}

func (r *Registry) writeWorkerGauges(b *strings.Builder) {
	r.workerMu.Lock()
	ids := make([]string, 0, len(r.workers))
	for id := range r.workers {
		ids = append(ids, id)
	}
	sort.Strings(ids)
	samples := make([]WorkerHost, 0, len(ids))
	for _, id := range ids {
		samples = append(samples, r.workers[id])
	}
	r.workerMu.Unlock()
	if len(samples) == 0 {
		return
	}
	fmt.Fprintf(b, "# HELP cell_worker_cpu_ratio Worker host CPU busy ratio (0..1)\n# TYPE cell_worker_cpu_ratio gauge\n")
	fmt.Fprintf(b, "# HELP cell_worker_memory_used_bytes Worker host memory used\n# TYPE cell_worker_memory_used_bytes gauge\n")
	fmt.Fprintf(b, "# HELP cell_worker_memory_total_bytes Worker host memory total\n# TYPE cell_worker_memory_total_bytes gauge\n")
	fmt.Fprintf(b, "# HELP cell_worker_disk_used_bytes Worker workspaces filesystem used\n# TYPE cell_worker_disk_used_bytes gauge\n")
	fmt.Fprintf(b, "# HELP cell_worker_disk_total_bytes Worker workspaces filesystem total\n# TYPE cell_worker_disk_total_bytes gauge\n")
	fmt.Fprintf(b, "# HELP cell_worker_cells_used Live Cells on worker\n# TYPE cell_worker_cells_used gauge\n")
	fmt.Fprintf(b, "# HELP cell_worker_cells_max Worker cell capacity\n# TYPE cell_worker_cells_max gauge\n")
	for _, s := range samples {
		w := s.ID
		fmt.Fprintf(b, "cell_worker_cpu_ratio{worker=%q} %g\n", w, s.CPURatio)
		fmt.Fprintf(b, "cell_worker_memory_used_bytes{worker=%q} %d\n", w, s.MemUsedBytes)
		fmt.Fprintf(b, "cell_worker_memory_total_bytes{worker=%q} %d\n", w, s.MemTotalBytes)
		fmt.Fprintf(b, "cell_worker_disk_used_bytes{worker=%q} %d\n", w, s.DiskUsedBytes)
		fmt.Fprintf(b, "cell_worker_disk_total_bytes{worker=%q} %d\n", w, s.DiskTotalBytes)
		fmt.Fprintf(b, "cell_worker_cells_used{worker=%q} %d\n", w, s.CellsUsed)
		fmt.Fprintf(b, "cell_worker_cells_max{worker=%q} %d\n", w, s.CellsMax)
	}
}

func writeCounter(b *strings.Builder, name, help string, byResult map[string]uint64) {
	fmt.Fprintf(b, "# HELP %s %s\n# TYPE %s counter\n", name, help, name)
	keys := make([]string, 0, len(byResult))
	for k := range byResult {
		keys = append(keys, k)
	}
	sort.Strings(keys)
	for _, k := range keys {
		fmt.Fprintf(b, "%s{result=%q} %d\n", name, k, byResult[k])
	}
}

func writeLatency(b *strings.Builder, prefix string, sumUs, n uint64) {
	sumName := prefix + "_duration_seconds_sum"
	countName := prefix + "_duration_seconds_count"
	fmt.Fprintf(b, "# HELP %s Cumulative create/exec duration\n# TYPE %s summary\n", sumName, sumName)
	fmt.Fprintf(b, "%s %g\n", sumName, float64(sumUs)/1e6)
	fmt.Fprintf(b, "# HELP %s Count of timed operations\n# TYPE %s summary\n", countName, countName)
	fmt.Fprintf(b, "%s %d\n", countName, n)
}

func writeLatencyLabeled(b *strings.Builder, prefix, label, value string, sumUs, n uint64) {
	sumName := prefix + "_duration_seconds_sum"
	countName := prefix + "_duration_seconds_count"
	fmt.Fprintf(b, "%s{%s=%q} %g\n", sumName, label, value, float64(sumUs)/1e6)
	fmt.Fprintf(b, "%s{%s=%q} %d\n", countName, label, value, n)
}

func writePhaseLatency(b *strings.Builder, fetchSum, fetchN, runtimeSum, runtimeN, setupSum, setupN uint64) {
	sumName := "cell_create_phase_duration_seconds_sum"
	countName := "cell_create_phase_duration_seconds_count"
	fmt.Fprintf(b, "# HELP %s Create phase duration (fetch=git, runtime=Docker/microvm, setup=template)\n# TYPE %s summary\n", sumName, sumName)
	fmt.Fprintf(b, "%s{phase=%q} %g\n", sumName, "fetch", float64(fetchSum)/1e6)
	fmt.Fprintf(b, "%s{phase=%q} %g\n", sumName, "runtime", float64(runtimeSum)/1e6)
	fmt.Fprintf(b, "%s{phase=%q} %g\n", sumName, "setup", float64(setupSum)/1e6)
	fmt.Fprintf(b, "# HELP %s Count of create phases observed\n# TYPE %s summary\n", countName, countName)
	fmt.Fprintf(b, "%s{phase=%q} %d\n", countName, "fetch", fetchN)
	fmt.Fprintf(b, "%s{phase=%q} %d\n", countName, "runtime", runtimeN)
	fmt.Fprintf(b, "%s{phase=%q} %d\n", countName, "setup", setupN)
}
