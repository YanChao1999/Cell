package auth

import (
	"crypto/hmac"
	"crypto/rand"
	"crypto/sha256"
	"crypto/subtle"
	"encoding/base64"
	"encoding/hex"
	"fmt"
	"strconv"
	"strings"
	"time"
)

// Scopes for short-lived tokens.
const (
	ScopeWorker = "worker" // register, heartbeat, worker RPC, cache pull
	ScopeAgent  = "agent"  // future: HTTP agent API on TCP (optional)
)

const tokenPrefix = "cellv1"

// DefaultTTL is the default short-lived token lifetime.
const DefaultTTL = 15 * time.Minute

// MaxTTL caps issued token lifetime.
const MaxTTL = 24 * time.Hour

// Issuer mints and validates HMAC-signed short-lived tokens.
// The long-lived CELL_WORKER_TOKEN is the HMAC secret (and still accepted
// as a bootstrap/static credential for ScopeWorker).
type Issuer struct {
	secret     []byte
	prevSecret []byte // optional previous secret for rotation grace
}

// NewIssuer builds an issuer from the current (and optional previous) secret.
func NewIssuer(secret, prev string) *Issuer {
	secret = strings.TrimSpace(secret)
	prev = strings.TrimSpace(prev)
	if secret == "" {
		return &Issuer{}
	}
	iss := &Issuer{secret: []byte(secret)}
	if prev != "" && prev != secret {
		iss.prevSecret = []byte(prev)
	}
	return iss
}

// Enabled reports whether a signing secret is configured.
func (i *Issuer) Enabled() bool {
	return i != nil && len(i.secret) > 0
}

// Secret returns the raw long-lived secret (for bootstrap / static compare).
func (i *Issuer) Secret() string {
	if i == nil || len(i.secret) == 0 {
		return ""
	}
	return string(i.secret)
}

// Issue mints a short-lived token for scope.
func (i *Issuer) Issue(scope string, ttl time.Duration) (string, time.Time, error) {
	if !i.Enabled() {
		return "", time.Time{}, fmt.Errorf("auth: issuer not configured")
	}
	scope = strings.TrimSpace(scope)
	if scope != ScopeWorker && scope != ScopeAgent {
		return "", time.Time{}, fmt.Errorf("auth: invalid scope %q", scope)
	}
	if ttl <= 0 {
		ttl = DefaultTTL
	}
	if ttl > MaxTTL {
		ttl = MaxTTL
	}
	exp := time.Now().UTC().Add(ttl).Truncate(time.Second)
	nonce := make([]byte, 8)
	if _, err := rand.Read(nonce); err != nil {
		return "", time.Time{}, err
	}
	payload := fmt.Sprintf("%s.%s.%d.%s", tokenPrefix, scope, exp.Unix(), hex.EncodeToString(nonce))
	mac := i.sign(i.secret, payload)
	tok := payload + "." + base64.RawURLEncoding.EncodeToString(mac)
	return tok, exp, nil
}

// Validate checks a bearer credential. Returns granted scope.
// Accepts: short-lived signed tokens, or the static secret (ScopeWorker).
func (i *Issuer) Validate(token string) (scope string, ok bool) {
	token = strings.TrimSpace(token)
	if token == "" {
		return "", false
	}
	if !i.Enabled() {
		return ScopeWorker, true // auth disabled
	}
	// Static long-lived secret → worker bootstrap scope.
	if subtle.ConstantTimeCompare([]byte(token), i.secret) == 1 {
		return ScopeWorker, true
	}
	if len(i.prevSecret) > 0 && subtle.ConstantTimeCompare([]byte(token), i.prevSecret) == 1 {
		return ScopeWorker, true
	}
	return i.validateSigned(token)
}

func (i *Issuer) validateSigned(token string) (string, bool) {
	parts := strings.Split(token, ".")
	if len(parts) != 5 || parts[0] != tokenPrefix {
		return "", false
	}
	scope := parts[1]
	if scope != ScopeWorker && scope != ScopeAgent {
		return "", false
	}
	expUnix, err := strconv.ParseInt(parts[2], 10, 64)
	if err != nil {
		return "", false
	}
	if time.Now().UTC().Unix() > expUnix {
		return "", false
	}
	payload := strings.Join(parts[:4], ".")
	macBytes, err := base64.RawURLEncoding.DecodeString(parts[4])
	if err != nil {
		return "", false
	}
	if hmac.Equal(macBytes, i.sign(i.secret, payload)) {
		return scope, true
	}
	if len(i.prevSecret) > 0 && hmac.Equal(macBytes, i.sign(i.prevSecret, payload)) {
		return scope, true
	}
	return "", false
}

func (i *Issuer) sign(secret []byte, payload string) []byte {
	m := hmac.New(sha256.New, secret)
	_, _ = m.Write([]byte(payload))
	return m.Sum(nil)
}
