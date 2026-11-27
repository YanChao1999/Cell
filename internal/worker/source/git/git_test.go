package git

import (
	"context"
	"os"
	"os/exec"
	"path/filepath"
	"testing"
	"time"
)

func TestMirrorFreshTTL(t *testing.T) {
	dir := t.TempDir()
	mirror := filepath.Join(dir, "m")
	if err := os.MkdirAll(mirror, 0o755); err != nil {
		t.Fatal(err)
	}
	if err := os.WriteFile(filepath.Join(mirror, "HEAD"), []byte("ref: refs/heads/main\n"), 0o644); err != nil {
		t.Fatal(err)
	}
	p := &Provider{ReposRoot: dir, MirrorMaxAge: time.Minute}
	// HEAD was just written → fresh
	if !p.mirrorFresh(mirror) {
		t.Fatal("expected fresh mirror right after HEAD write")
	}
	p.MirrorMaxAge = time.Nanosecond
	time.Sleep(2 * time.Millisecond)
	if p.mirrorFresh(mirror) {
		t.Fatal("expected stale when max age elapsed")
	}
	p.MirrorMaxAge = -1
	if p.mirrorFresh(mirror) {
		t.Fatal("negative max age must always fetch")
	}
}

func TestFetchUsesCacheThenWorktree(t *testing.T) {
	if _, err := exec.LookPath("git"); err != nil {
		t.Skip("git not installed")
	}
	origin := t.TempDir()
	mustRun(t, origin, "git", "init", "-b", "main")
	mustRun(t, origin, "git", "config", "user.email", "cell@test")
	mustRun(t, origin, "git", "config", "user.name", "cell")
	if err := os.WriteFile(filepath.Join(origin, "README"), []byte("hi\n"), 0o644); err != nil {
		t.Fatal(err)
	}
	mustRun(t, origin, "git", "add", "README")
	mustRun(t, origin, "git", "commit", "-m", "init")

	repos := t.TempDir()
	p := &Provider{ReposRoot: repos, MirrorMaxAge: time.Hour}
	ctx := context.Background()

	dest1 := filepath.Join(t.TempDir(), "wt1")
	st1, err := p.Fetch(ctx, origin, "main", dest1)
	if err != nil {
		t.Fatalf("first fetch: %v", err)
	}
	if st1.Mirror != "clone" {
		t.Fatalf("first mirror action=%q want clone", st1.Mirror)
	}
	if _, err := os.Stat(filepath.Join(dest1, "README")); err != nil {
		t.Fatalf("worktree missing README: %v", err)
	}
	// .git file marks a linked worktree
	raw, err := os.ReadFile(filepath.Join(dest1, ".git"))
	if err != nil {
		t.Fatal(err)
	}
	if len(raw) == 0 || string(raw[:8]) != "gitdir: " {
		t.Fatalf("expected gitdir worktree pointer, got %q", raw)
	}

	dest2 := filepath.Join(t.TempDir(), "wt2")
	t0 := time.Now()
	st2, err := p.Fetch(ctx, origin, "main", dest2)
	if err != nil {
		t.Fatalf("second fetch (cache): %v", err)
	}
	if st2.Mirror != "cache" {
		t.Fatalf("second mirror action=%q want cache", st2.Mirror)
	}
	if time.Since(t0) > 5*time.Second {
		t.Fatalf("cached fetch too slow: %s", time.Since(t0))
	}
	body, err := os.ReadFile(filepath.Join(dest2, "README"))
	if err != nil || string(body) != "hi\n" {
		t.Fatalf("second worktree README: %v %q", err, body)
	}
}

func mustRun(t *testing.T, dir string, name string, args ...string) {
	t.Helper()
	cmd := exec.Command(name, args...)
	cmd.Dir = dir
	out, err := cmd.CombinedOutput()
	if err != nil {
		t.Fatalf("%s %v: %v\n%s", name, args, err, out)
	}
}
