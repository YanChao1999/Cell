package api

import (
	"crypto/sha256"
	"encoding/hex"
	"net/http"
	"strings"

	"github.com/YanChao1999/Cell/internal/worker/auth"
)

// agentPath reports whether path is an Agent API route that must not be
// exposed unauthenticated on TCP.
func agentPath(path string) bool {
	if path == "/v1/run" {
		return true
	}
	if path == "/v1/cells" || strings.HasPrefix(path, "/v1/cells/") {
		return true
	}
	return false
}

// HandlerHTTP returns the control handler for TCP listeners.
// Agent routes require a valid worker or agent-scoped Bearer when the issuer
// is enabled; ListenAndServeHTTP refuses to start without an issuer secret.
func (s *Server) HandlerHTTP() http.Handler {
	return http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		if agentPath(r.URL.Path) {
			if s.issuer == nil || !s.issuer.Enabled() {
				writeErr(w, http.StatusServiceUnavailable, "agent auth not configured")
				return
			}
			if !auth.Authorize(r, s.issuer, auth.ScopeAgent, auth.ScopeWorker) {
				writeErr(w, http.StatusUnauthorized, "unauthorized")
				return
			}
		}
		s.mux.ServeHTTP(w, r)
	})
}

// agentOwnerFingerprint is a stable, non-reversible id for snapshot ACL.
// Empty Bearer (unix/dev without token) maps to "anonymous".
func agentOwnerFingerprint(r *http.Request) string {
	tok := auth.BearerToken(r)
	if tok == "" {
		return "anonymous"
	}
	sum := sha256.Sum256([]byte(tok))
	return hex.EncodeToString(sum[:])
}
