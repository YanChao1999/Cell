package cache

import (
	"os"
	"path/filepath"
	"testing"
)

func TestResolveAndHostEnv(t *testing.T) {
	root := t.TempDir()
	layout := NewLayout(root)
	if err := layout.Ensure(); err != nil {
		t.Fatal(err)
	}
	for _, name := range []string{"go-mod", "pip-cache", "ccache"} {
		p := filepath.Join(root, name)
		fi, err := os.Stat(p)
		if err != nil || !fi.IsDir() {
			t.Fatalf("expected well-known dir %s: %v", name, err)
		}
	}

	abs, err := layout.Resolve("go-mod")
	if err != nil {
		t.Fatal(err)
	}
	if abs != filepath.Join(root, "go-mod") {
		t.Fatalf("Resolve: got %q", abs)
	}

	mounts, err := layout.ResolveMounts(
		[]string{"go-mod", "go-build", "pip-cache"},
		[]string{"/go/pkg/mod", "/root/.cache/go-build", "/root/.cache/pip"},
	)
	if err != nil {
		t.Fatal(err)
	}
	env := HostEnv(mounts)
	if env["GOMODCACHE"] != filepath.Join(root, "go-mod") {
		t.Fatalf("GOMODCACHE: %+v", env)
	}
	if env["GOCACHE"] != filepath.Join(root, "go-build") {
		t.Fatalf("GOCACHE: %+v", env)
	}
	if env["PIP_CACHE_DIR"] != filepath.Join(root, "pip-cache") {
		t.Fatalf("PIP_CACHE_DIR: %+v", env)
	}
}

func TestSanitizeHostKey(t *testing.T) {
	layout := NewLayout(t.TempDir())
	for _, bad := range []string{"", "../etc", "a/b", "/abs", `a\b`} {
		if _, err := layout.Resolve(bad); err == nil {
			t.Fatalf("expected error for %q", bad)
		}
	}
}

func TestEnvVarForHostKey(t *testing.T) {
	v, ok := EnvVarForHostKey("go-mod")
	if !ok || v != "GOMODCACHE" {
		t.Fatalf("got %q %v", v, ok)
	}
	if _, ok := EnvVarForHostKey("python-venv"); ok {
		t.Fatal("python-venv should not map to a tool env var")
	}
}
