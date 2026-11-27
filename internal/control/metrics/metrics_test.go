package metrics

import (
	"net/http"
	"net/http/httptest"
	"strings"
	"testing"
	"time"

	"github.com/YanChao1999/Cell/api"
)

func TestHandlerExposesCounters(t *testing.T) {
	r := New()
	r.ObserveCreate(10*time.Millisecond, nil, true)
	r.ObserveCreate(5*time.Millisecond, assertErr{}, false)
	r.ObserveCreatePhases(&api.CreateTimings{
		FetchMS: 40, RuntimeMS: 5, SetupMS: 2,
		Mirror: "cache", CacheHit: 2, CacheMiss: 1,
	})
	r.ObserveExec(2*time.Millisecond, nil)

	rr := httptest.NewRecorder()
	r.Handler().ServeHTTP(rr, httptest.NewRequest(http.MethodGet, "/metrics", nil))
	body := rr.Body.String()
	for _, want := range []string{
		`cell_create_total{result="ok"} 1`,
		`cell_create_total{result="error"} 1`,
		`cell_exec_total{result="ok"} 1`,
		"cell_create_duration_seconds_sum",
		`cell_create_duration_seconds_sum{repo="yes"}`,
		`cell_create_duration_seconds_sum{repo="no"}`,
		`cell_create_duration_seconds_count{repo="yes"} 1`,
		`cell_create_duration_seconds_count{repo="no"} 1`,
		`cell_create_phase_duration_seconds_sum{phase="fetch"}`,
		`cell_create_phase_duration_seconds_sum{phase="runtime"}`,
		`cell_create_phase_duration_seconds_count{phase="fetch"} 1`,
		`cell_git_mirror_total{result="cache"} 1`,
		`cell_cache_mount_total{result="hit"} 2`,
		`cell_cache_mount_total{result="miss"} 1`,
		"cell_exec_duration_seconds_count",
	} {
		if !strings.Contains(body, want) {
			t.Fatalf("missing %q in:\n%s", want, body)
		}
	}
}

type assertErr struct{}

func (assertErr) Error() string { return "x" }
