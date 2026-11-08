package auth

import (
	"net/http"
	"net/http/httptest"
	"testing"
)

func TestMiddlewareDisabled(t *testing.T) {
	h := Middleware("")(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		w.WriteHeader(http.StatusNoContent)
	}))
	rr := httptest.NewRecorder()
	h.ServeHTTP(rr, httptest.NewRequest(http.MethodGet, "/", nil))
	if rr.Code != http.StatusNoContent {
		t.Fatalf("code=%d", rr.Code)
	}
}

func TestMiddlewareRequiresBearer(t *testing.T) {
	h := Middleware("s3cret")(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		w.WriteHeader(http.StatusNoContent)
	}))
	rr := httptest.NewRecorder()
	h.ServeHTTP(rr, httptest.NewRequest(http.MethodGet, "/", nil))
	if rr.Code != http.StatusUnauthorized {
		t.Fatalf("want 401, got %d", rr.Code)
	}

	req := httptest.NewRequest(http.MethodGet, "/", nil)
	SetBearer(req.Header, "s3cret")
	rr = httptest.NewRecorder()
	h.ServeHTTP(rr, req)
	if rr.Code != http.StatusNoContent {
		t.Fatalf("want 204, got %d", rr.Code)
	}
}

func TestAuthorize(t *testing.T) {
	req := httptest.NewRequest(http.MethodGet, "/", nil)
	if !Authorize(req, "") {
		t.Fatal("empty expected should allow")
	}
	if Authorize(req, "tok") {
		t.Fatal("missing bearer should deny")
	}
	SetBearer(req.Header, "tok")
	if !Authorize(req, "tok") {
		t.Fatal("matching bearer should allow")
	}
}
