package metrics

import (
	"net/http"
	"net/http/httptest"
	"strings"
	"testing"
	"time"
)

func TestHandlerExposesCounters(t *testing.T) {
	r := New()
	r.ObserveCreate(10*time.Millisecond, nil)
	r.ObserveCreate(5*time.Millisecond, assertErr{})
	r.ObserveExec(2*time.Millisecond, nil)

	rr := httptest.NewRecorder()
	r.Handler().ServeHTTP(rr, httptest.NewRequest(http.MethodGet, "/metrics", nil))
	body := rr.Body.String()
	for _, want := range []string{
		`cell_create_total{result="ok"} 1`,
		`cell_create_total{result="error"} 1`,
		`cell_exec_total{result="ok"} 1`,
		"cell_create_duration_seconds_sum",
		"cell_exec_duration_seconds_count",
	} {
		if !strings.Contains(body, want) {
			t.Fatalf("missing %q in:\n%s", want, body)
		}
	}
}

type assertErr struct{}

func (assertErr) Error() string { return "x" }
