package peers

import (
	"context"
	"encoding/json"
	"net/http"
	"net/http/httptest"
	"testing"

	"github.com/YanChao1999/Cell/internal/control/registry"
	"github.com/YanChao1999/Cell/internal/worker/auth"
)

func TestEndpointForCache(t *testing.T) {
	mux := http.NewServeMux()
	mux.HandleFunc("GET /v1/workers", func(w http.ResponseWriter, r *http.Request) {
		if !auth.Authorize(r, "tok") {
			http.Error(w, "no", http.StatusUnauthorized)
			return
		}
		_ = json.NewEncoder(w).Encode([]registry.Info{
			{ID: "self", Endpoint: "http://127.0.0.1:1", CacheKeys: []string{"go-mod"}},
			{ID: "peer", Endpoint: "http://127.0.0.1:2", CacheKeys: []string{"go-mod", "pip-cache"}},
		})
	})
	srv := httptest.NewServer(mux)
	defer srv.Close()

	f := &Finder{ControlURL: srv.URL, Token: "tok"}
	ep, err := f.EndpointForCache(context.Background(), "go-mod", "self")
	if err != nil || ep != "http://127.0.0.1:2" {
		t.Fatalf("ep=%q err=%v", ep, err)
	}
	ep, err = f.EndpointForCache(context.Background(), "missing", "self")
	if err != nil || ep != "" {
		t.Fatalf("want empty, got %q %v", ep, err)
	}
}
