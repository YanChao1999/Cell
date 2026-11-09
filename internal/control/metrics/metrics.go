// Package metrics exposes a tiny Prometheus text registry for Cell control plane.
package metrics

import (
	"fmt"
	"net/http"
	"sort"
	"strings"
	"sync/atomic"
	"time"
)

// Registry holds process counters and latency sums (no external deps).
type Registry struct {
	createOK, createErr   atomic.Uint64
	execOK, execErr       atomic.Uint64
	destroyOK, destroyErr atomic.Uint64
	createSumUs, createN  atomic.Uint64
	execSumUs, execN      atomic.Uint64
}

// New returns an empty registry.
func New() *Registry { return &Registry{} }

// ObserveCreate records a create attempt.
func (r *Registry) ObserveCreate(d time.Duration, err error) {
	r.observe(d, err, &r.createSumUs, &r.createN, &r.createOK, &r.createErr)
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
		writeLatency(&b, "cell_exec", r.execSumUs.Load(), r.execN.Load())
		_, _ = w.Write([]byte(b.String()))
	})
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
