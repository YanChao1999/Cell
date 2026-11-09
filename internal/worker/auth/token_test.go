package auth

import (
	"encoding/base64"
	"testing"
	"time"
)

func TestIssueAndValidate(t *testing.T) {
	iss := NewIssuer("super-secret", "")
	tok, exp, err := iss.Issue(ScopeWorker, time.Minute)
	if err != nil || tok == "" || exp.Before(time.Now()) {
		t.Fatalf("issue: tok=%q exp=%v err=%v", tok, exp, err)
	}
	scope, ok := iss.Validate(tok)
	if !ok || scope != ScopeWorker {
		t.Fatalf("validate signed: scope=%q ok=%v", scope, ok)
	}
	scope, ok = iss.Validate("super-secret")
	if !ok || scope != ScopeWorker {
		t.Fatalf("validate static: scope=%q ok=%v", scope, ok)
	}
	if _, ok := iss.Validate("wrong"); ok {
		t.Fatal("expected reject")
	}
}

func TestPrevSecretRotation(t *testing.T) {
	old := NewIssuer("old-secret", "")
	tok, _, err := old.Issue(ScopeWorker, time.Minute)
	if err != nil {
		t.Fatal(err)
	}
	neu := NewIssuer("new-secret", "old-secret")
	if scope, ok := neu.Validate(tok); !ok || scope != ScopeWorker {
		t.Fatalf("prev should accept old signed token, got %q %v", scope, ok)
	}
	if _, ok := neu.Validate("old-secret"); !ok {
		t.Fatal("prev static secret should work")
	}
}

func TestExpiredRejected(t *testing.T) {
	iss := NewIssuer("secret", "")
	payload := "cellv1.worker.1.deadbeef"
	mac := iss.sign(iss.secret, payload)
	bad := payload + "." + base64.RawURLEncoding.EncodeToString(mac)
	if _, ok := iss.Validate(bad); ok {
		t.Fatal("expired should fail")
	}
}

func TestDisabledIssuerAllows(t *testing.T) {
	iss := NewIssuer("", "")
	if scope, ok := iss.Validate("anything"); !ok || scope != ScopeWorker {
		t.Fatalf("disabled issuer should allow, got %q %v", scope, ok)
	}
}
