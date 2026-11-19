package api

import (
	"io"
	"net/http"
	"net/http/httptest"
	"testing"

	"github.com/YanChao1999/Cell/internal/control/meta"
	"github.com/YanChao1999/Cell/internal/control/registry"
	"github.com/YanChao1999/Cell/internal/control/scheduler"
	"github.com/YanChao1999/Cell/internal/worker/auth"
)

func TestListenAndServeHTTPRequiresIssuer(t *testing.T) {
	dir := t.TempDir()
	store, err := meta.New(dir)
	if err != nil {
		t.Fatal(err)
	}
	srv := NewWithOptions(store, scheduler.New(nil, "local"), registry.New(0), Options{})
	if err := srv.ListenAndServeHTTP("127.0.0.1:0"); err == nil {
		t.Fatal("expected error when issuer disabled")
	}
}

func TestHandlerHTTPRequiresBearerOnAgentRoutes(t *testing.T) {
	dir := t.TempDir()
	store, err := meta.New(dir)
	if err != nil {
		t.Fatal(err)
	}
	iss := auth.NewIssuer("test-secret", "")
	srv := NewWithOptions(store, scheduler.New(nil, "local"), registry.New(0), Options{Issuer: iss})
	h := srv.HandlerHTTP()

	req := httptest.NewRequest(http.MethodGet, "/v1/cells", nil)
	rr := httptest.NewRecorder()
	h.ServeHTTP(rr, req)
	if rr.Code != http.StatusUnauthorized {
		t.Fatalf("unauth cells: got %d", rr.Code)
	}

	req2 := httptest.NewRequest(http.MethodGet, "/health", nil)
	rr2 := httptest.NewRecorder()
	h.ServeHTTP(rr2, req2)
	if rr2.Code != http.StatusOK {
		t.Fatalf("health should stay open: %d %s", rr2.Code, rr2.Body.String())
	}

	req3 := httptest.NewRequest(http.MethodGet, "/v1/cells", nil)
	auth.SetBearer(req3.Header, "test-secret")
	rr3 := httptest.NewRecorder()
	h.ServeHTTP(rr3, req3)
	if rr3.Code != http.StatusOK {
		body, _ := io.ReadAll(rr3.Body)
		t.Fatalf("authed cells: got %d %s", rr3.Code, body)
	}
}

func TestSnapshotOwnerACL(t *testing.T) {
	dir := t.TempDir()
	id, err := newSnapshotID()
	if err != nil {
		t.Fatal(err)
	}
	if err := writeSnapshotOwner(dir, id, "owner-a"); err != nil {
		t.Fatal(err)
	}
	if err := checkSnapshotOwner(dir, id, "owner-a"); err != nil {
		t.Fatal(err)
	}
	if err := checkSnapshotOwner(dir, id, "owner-b"); err == nil {
		t.Fatal("expected access denied")
	}
	id2, err := newSnapshotID()
	if err != nil {
		t.Fatal(err)
	}
	if err := checkSnapshotOwner(dir, id2, "owner-a"); err == nil {
		t.Fatal("expected missing owner reject")
	}
}
