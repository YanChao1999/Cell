package envtemplate

import (
	"os"
	"path/filepath"
	"testing"
)

func TestParseGoTemplate(t *testing.T) {
	data := []byte(`name: go-1.23
description: Go 1.23 on Debian Bookworm with module/build caches

image: golang:1.23-bookworm

cache_mounts:
  - host: go-mod
    container: /go/pkg/mod
  - host: go-build
    container: /root/.cache/go-build

setup:
  - go version

env:
  GO111MODULE: "on"
  CGO_ENABLED: "0"
  GOMODCACHE: /go/pkg/mod
  GOCACHE: /root/.cache/go-build

default_limits:
  cpu: "2"
  memory: 4Gi
  disk: 10Gi

default_exec_timeout: 30m

network: default
`)
	tmpl, err := Parse(data)
	if err != nil {
		t.Fatal(err)
	}
	if tmpl.Name != "go-1.23" || tmpl.Image != "golang:1.23-bookworm" {
		t.Fatalf("unexpected name/image: %+v", tmpl)
	}
	if tmpl.DefaultLimits.CPU != "2" || tmpl.DefaultLimits.Memory != "4Gi" {
		t.Fatalf("limits: %+v", tmpl.DefaultLimits)
	}
	if tmpl.Env["GO111MODULE"] != "on" || tmpl.Env["CGO_ENABLED"] != "0" {
		t.Fatalf("env: %+v", tmpl.Env)
	}
	if tmpl.Env["GOMODCACHE"] != "/go/pkg/mod" || tmpl.Env["GOCACHE"] != "/root/.cache/go-build" {
		t.Fatalf("cache env: %+v", tmpl.Env)
	}
	if len(tmpl.CacheMounts) != 2 || tmpl.CacheMounts[0].Host != "go-mod" {
		t.Fatalf("cache_mounts: %+v", tmpl.CacheMounts)
	}
	if len(tmpl.Setup) != 1 || tmpl.Setup[0].Run != "go version" {
		t.Fatalf("setup: %+v", tmpl.Setup)
	}
	if tmpl.DefaultExecTimeout != "30m" {
		t.Fatalf("default_exec_timeout: %q", tmpl.DefaultExecTimeout)
	}
	if tmpl.Network != "default" {
		t.Fatalf("network: %q", tmpl.Network)
	}
}

func TestParseSetupConditionalsAndTimeout(t *testing.T) {
	data := []byte(`name: rich
setup_timeout: 15m
setup:
  - go version
  - run: pip install -r requirements.txt
    if: test -f requirements.txt
  - run: npm ci
    unless: test -d node_modules
`)
	tmpl, err := Parse(data)
	if err != nil {
		t.Fatal(err)
	}
	if tmpl.SetupTimeout != "15m" {
		t.Fatalf("setup_timeout: %q", tmpl.SetupTimeout)
	}
	if len(tmpl.Setup) != 3 {
		t.Fatalf("setup len: %+v", tmpl.Setup)
	}
	if tmpl.Setup[0].Run != "go version" || tmpl.Setup[0].If != "" {
		t.Fatalf("step0: %+v", tmpl.Setup[0])
	}
	if tmpl.Setup[1].Run != "pip install -r requirements.txt" || tmpl.Setup[1].If != "test -f requirements.txt" {
		t.Fatalf("step1: %+v", tmpl.Setup[1])
	}
	if tmpl.Setup[2].Run != "npm ci" || tmpl.Setup[2].Unless != "test -d node_modules" {
		t.Fatalf("step2: %+v", tmpl.Setup[2])
	}
}

func TestParseEgressAllowlist(t *testing.T) {
	data := []byte(`name: agent-net
image: golang:1.23-bookworm
network: allowlist
egress:
  allow:
    - proxy.golang.org
    - github.com
`)
	tmpl, err := Parse(data)
	if err != nil {
		t.Fatal(err)
	}
	if tmpl.Network != "allowlist" {
		t.Fatalf("network: %q", tmpl.Network)
	}
	if len(tmpl.EgressAllow) != 2 || tmpl.EgressAllow[0] != "proxy.golang.org" || tmpl.EgressAllow[1] != "github.com" {
		t.Fatalf("egress.allow: %+v", tmpl.EgressAllow)
	}
}

func TestLoadFromDir(t *testing.T) {
	root := filepath.Join("..", "..", "..", "environments")
	if _, err := os.Stat(filepath.Join(root, "go-1.23.yaml")); err != nil {
		t.Skip("environments/go-1.23.yaml not found:", err)
	}
	l := New(root)
	tmpl, err := l.Load("go-1.23")
	if err != nil {
		t.Fatal(err)
	}
	if tmpl.Image == "" {
		t.Fatal("expected image")
	}
}
