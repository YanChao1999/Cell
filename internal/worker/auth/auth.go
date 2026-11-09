// Package auth provides Bearer-token helpers for worker RPC and
// control-plane worker registration (static secret + short-lived HMAC tokens).
package auth

import (
	"net/http"
	"os"
	"strings"
)

const header = "Authorization"

// EnvVar is the environment variable for the long-lived HMAC / bootstrap secret.
const EnvVar = "CELL_WORKER_TOKEN"

// EnvVarPrev is an optional previous secret for rotation grace.
const EnvVarPrev = "CELL_WORKER_TOKEN_PREV"

// FromEnv returns CELL_WORKER_TOKEN (trimmed). Empty means auth disabled.
func FromEnv() string {
	return strings.TrimSpace(os.Getenv(EnvVar))
}

// PrevFromEnv returns CELL_WORKER_TOKEN_PREV.
func PrevFromEnv() string {
	return strings.TrimSpace(os.Getenv(EnvVarPrev))
}

// SetBearer sets Authorization: Bearer <token> when token is non-empty.
func SetBearer(h http.Header, token string) {
	token = strings.TrimSpace(token)
	if token == "" {
		return
	}
	h.Set(header, "Bearer "+token)
}

// Middleware requires a credential that validates to one of wantScopes.
// When issuer is nil or disabled, all requests pass (local/dev trusted mode).
func Middleware(issuer *Issuer, wantScopes ...string) func(http.Handler) http.Handler {
	allowed := scopeSet(wantScopes...)
	return func(next http.Handler) http.Handler {
		if issuer == nil || !issuer.Enabled() {
			return next
		}
		return http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
			scope, ok := issuer.Validate(bearerToken(r.Header.Get(header)))
			if !ok || !allowed[scope] {
				http.Error(w, `{"error":"unauthorized"}`, http.StatusUnauthorized)
				return
			}
			next.ServeHTTP(w, r)
		})
	}
}

// Authorize returns true when auth is disabled or the request Bearer is valid
// for one of wantScopes (default: worker).
func Authorize(r *http.Request, issuer *Issuer, wantScopes ...string) bool {
	if issuer == nil || !issuer.Enabled() {
		return true
	}
	if len(wantScopes) == 0 {
		wantScopes = []string{ScopeWorker}
	}
	allowed := scopeSet(wantScopes...)
	scope, ok := issuer.Validate(bearerToken(r.Header.Get(header)))
	return ok && allowed[scope]
}

func scopeSet(scopes ...string) map[string]bool {
	m := make(map[string]bool, len(scopes))
	for _, s := range scopes {
		m[s] = true
	}
	if len(m) == 0 {
		m[ScopeWorker] = true
	}
	return m
}

func bearerToken(v string) string {
	v = strings.TrimSpace(v)
	if len(v) < 8 || !strings.EqualFold(v[:7], "bearer ") {
		return ""
	}
	return strings.TrimSpace(v[7:])
}
