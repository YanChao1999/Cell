package auth

import (
	"net/http"
	"net/http/httptest"
	"testing"
	"time"
)

func TestMiddlewareDisabled(t *testing.T) {
	h := Middleware(nil)(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		w.WriteHeader(http.StatusNoContent)
	}))
	rr := httptest.NewRecorder()
	h.ServeHTTP(rr, httptest.NewRequest(http.MethodGet, "/", nil))
	if rr.Code != http.StatusNoContent {
		t.Fatalf("code=%d", rr.Code)
	}
}

func TestMiddlewareRequiresBearer(t *testing.T) {
	iss := NewIssuer("s3cret", "")
	h := Middleware(iss, ScopeWorker)(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
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

	tok, _, err := iss.Issue(ScopeWorker, time.Minute)
	if err != nil {
		t.Fatal(err)
	}
	req = httptest.NewRequest(http.MethodGet, "/", nil)
	SetBearer(req.Header, tok)
	rr = httptest.NewRecorder()
	h.ServeHTTP(rr, req)
	if rr.Code != http.StatusNoContent {
		t.Fatalf("short token: want 204, got %d", rr.Code)
	}
}

func TestAuthorize(t *testing.T) {
	iss := NewIssuer("tok", "")
	req := httptest.NewRequest(http.MethodGet, "/", nil)
	if Authorize(req, nil) {
		// nil issuer = disabled = allow
	} else {
		t.Fatal("nil issuer should allow")
	}
	if Authorize(req, iss) {
		t.Fatal("missing bearer should deny")
	}
	SetBearer(req.Header, "tok")
	if !Authorize(req, iss, ScopeWorker) {
		t.Fatal("matching bearer should allow")
	}
}
