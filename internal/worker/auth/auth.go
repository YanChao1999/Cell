// Package auth provides shared Bearer-token helpers for worker RPC and
// control-plane worker registration.
package auth

import (
	"crypto/subtle"
	"net/http"
	"os"
	"strings"
)

const header = "Authorization"

// EnvVar is the environment variable for the shared worker token.
const EnvVar = "CELL_WORKER_TOKEN"

// FromEnv returns CELL_WORKER_TOKEN (trimmed). Empty means auth disabled.
func FromEnv() string {
	return strings.TrimSpace(os.Getenv(EnvVar))
}

// SetBearer sets Authorization: Bearer <token> when token is non-empty.
func SetBearer(h http.Header, token string) {
	token = strings.TrimSpace(token)
	if token == "" {
		return
	}
	h.Set(header, "Bearer "+token)
}

// Middleware requires a matching Bearer token when expected is non-empty.
// When expected is empty, all requests pass (local/dev trusted mode).
func Middleware(expected string) func(http.Handler) http.Handler {
	expected = strings.TrimSpace(expected)
	return func(next http.Handler) http.Handler {
		if expected == "" {
			return next
		}
		want := []byte(expected)
		return http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
			got := bearerToken(r.Header.Get(header))
			if subtle.ConstantTimeCompare([]byte(got), want) != 1 {
				http.Error(w, `{"error":"unauthorized"}`, http.StatusUnauthorized)
				return
			}
			next.ServeHTTP(w, r)
		})
	}
}

// Authorize returns true when expected is empty or the request Bearer matches.
func Authorize(r *http.Request, expected string) bool {
	expected = strings.TrimSpace(expected)
	if expected == "" {
		return true
	}
	got := bearerToken(r.Header.Get(header))
	return subtle.ConstantTimeCompare([]byte(got), []byte(expected)) == 1
}

func bearerToken(v string) string {
	v = strings.TrimSpace(v)
	if len(v) < 8 || !strings.EqualFold(v[:7], "bearer ") {
		return ""
	}
	return strings.TrimSpace(v[7:])
}
