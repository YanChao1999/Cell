package httpserver

import (
	"context"
	"net/http"
	"net/http/httptest"
	"os"
	"path/filepath"
	"testing"

	"github.com/YanChao1999/Cell/api"
	"github.com/YanChao1999/Cell/internal/worker/auth"
	"github.com/YanChao1999/Cell/internal/worker/manager"
	"github.com/YanChao1999/Cell/internal/worker/runtime"
	"github.com/YanChao1999/Cell/internal/worker/source/git"
)

type stubRT struct{}

func (stubRT) Available() bool                                  { return false }
func (stubRT) Create(context.Context, runtime.CreateOpts) error { return nil }
func (stubRT) Exec(context.Context, runtime.ExecOpts) (*api.ExecResult, error) {
	return &api.ExecResult{ExitCode: 0}, nil
}
func (stubRT) Destroy(context.Context, string) error { return nil }

func TestAuthAndCacheExport(t *testing.T) {
	root := t.TempDir()
	cacheDir := filepath.Join(root, "cache")
	ws := filepath.Join(root, "ws")
	_ = os.MkdirAll(filepath.Join(cacheDir, "go-mod"), 0o755)
	_ = os.WriteFile(filepath.Join(cacheDir, "go-mod", "x"), []byte("warm"), 0o644)

	lw, err := manager.New(manager.Options{
		ID:             "w1",
		WorkspacesRoot: ws,
		CacheDir:       cacheDir,
		Runtime:        stubRT{},
		Source:         git.NewStub(),
	})
	if err != nil {
		t.Fatal(err)
	}
	srv := New(lw, Options{Token: "s3cret"})
	h := srv.Handler()

	rr := httptest.NewRecorder()
	h.ServeHTTP(rr, httptest.NewRequest(http.MethodGet, "/worker/v1/health", nil))
	if rr.Code != http.StatusUnauthorized {
		t.Fatalf("want 401, got %d", rr.Code)
	}

	req := httptest.NewRequest(http.MethodGet, "/worker/v1/caches/go-mod", nil)
	auth.SetBearer(req.Header, "s3cret")
	rr = httptest.NewRecorder()
	h.ServeHTTP(rr, req)
	if rr.Code != http.StatusOK {
		t.Fatalf("cache get: %d %s", rr.Code, rr.Body.String())
	}
	if rr.Body.Len() == 0 {
		t.Fatal("expected gzip body")
	}
}
