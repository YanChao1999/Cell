// Package git provides a Source fetcher that mirrors repos and checks out worktrees.
package git

import (
	"context"
	"crypto/sha256"
	"encoding/hex"
	"fmt"
	"log"
	"os"
	"os/exec"
	"path/filepath"
	"strings"
	"time"
)

// FetchStats describes how a Source.Fetch obtained repo content.
type FetchStats struct {
	// Mirror is cache (TTL hit), fetch (network update), clone (new mirror), or skip (empty repo).
	Mirror string
}

// Source fetches repository content into a workspace.
type Source interface {
	Fetch(ctx context.Context, repo, ref, dest string) (FetchStats, error)
	// Release unregisters a worktree (best-effort). Safe to call for empty repos.
	Release(ctx context.Context, dest string) error
}

// Provider mirrors remotes under ReposRoot and checks out a worktree into dest.
type Provider struct {
	ReposRoot string
	// MirrorMaxAge skips git fetch when the mirror was updated more recently.
	// Zero means DefaultMirrorMaxAge. Negative disables the TTL (always fetch).
	MirrorMaxAge time.Duration
}

// DefaultMirrorMaxAge is used when MirrorMaxAge is 0.
// Override with CELL_GIT_MIRROR_MAX_AGE (Go duration, e.g. "5m", "0s" = always fetch).
const DefaultMirrorMaxAge = 5 * time.Minute

// New returns a git Source using reposRoot for bare mirrors
// (default ~/.cell/repos).
func New(reposRoot string) (*Provider, error) {
	if reposRoot == "" {
		home, err := os.UserHomeDir()
		if err != nil {
			return nil, err
		}
		reposRoot = filepath.Join(home, ".cell", "repos")
	}
	if err := os.MkdirAll(reposRoot, 0o755); err != nil {
		return nil, err
	}
	return &Provider{ReposRoot: reposRoot, MirrorMaxAge: mirrorMaxAgeFromEnv()}, nil
}

func mirrorMaxAgeFromEnv() time.Duration {
	raw := strings.TrimSpace(os.Getenv("CELL_GIT_MIRROR_MAX_AGE"))
	if raw == "" {
		return DefaultMirrorMaxAge
	}
	d, err := time.ParseDuration(raw)
	if err != nil {
		log.Printf("source/git: invalid CELL_GIT_MIRROR_MAX_AGE %q; using %s", raw, DefaultMirrorMaxAge)
		return DefaultMirrorMaxAge
	}
	return d
}

// Fetch clones/fetches a bare mirror then adds a detached worktree at dest.
// Empty repo is a no-op (skip). Existing mirrors skip network fetch when fresh
// (MirrorMaxAge); a failed worktree add retries once after fetch.
func (p *Provider) Fetch(ctx context.Context, repo, ref, dest string) (FetchStats, error) {
	repo = strings.TrimSpace(repo)
	if repo == "" {
		return FetchStats{Mirror: "skip"}, nil
	}
	if _, err := exec.LookPath("git"); err != nil {
		return FetchStats{}, fmt.Errorf("source/git: git not found in PATH: %w", err)
	}
	if ref == "" {
		ref = "HEAD"
	}

	start := time.Now()
	var mirror string
	var mirrorAction string
	var mirrorDur time.Duration
	lockEnsure := func(force bool) error {
		return withMirrorLock(filepath.Join(p.ReposRoot, repoHash(repo)), func() error {
			t0 := time.Now()
			var e error
			mirror, mirrorAction, e = p.ensureMirror(ctx, repo, force)
			mirrorDur += time.Since(t0)
			return e
		})
	}
	if err := lockEnsure(false); err != nil {
		return FetchStats{}, err
	}

	if err := prepareDest(dest); err != nil {
		return FetchStats{}, err
	}

	tWT := time.Now()
	err := worktreeAdd(ctx, mirror, dest, ref)
	wtDur := time.Since(tWT)
	if err != nil {
		// Ref may be missing from a stale mirror — force fetch once and retry.
		log.Printf("source/git: worktree miss for %s@%s; forcing mirror fetch", repo, ref)
		if err := lockEnsure(true); err != nil {
			return FetchStats{}, err
		}
		tWT = time.Now()
		err = worktreeAdd(ctx, mirror, dest, ref)
		wtDur += time.Since(tWT)
		if err != nil {
			return FetchStats{}, err
		}
	}

	log.Printf("source/git: fetch repo=%s ref=%s mirror=%s mirror_ms=%d worktree_ms=%d total_ms=%d",
		repo, ref, mirrorAction, mirrorDur.Milliseconds(), wtDur.Milliseconds(), time.Since(start).Milliseconds())
	return FetchStats{Mirror: mirrorAction}, nil
}

func worktreeAdd(ctx context.Context, mirror, dest, ref string) error {
	cmd := exec.CommandContext(ctx, "git", "-C", mirror, "worktree", "add", "--detach", dest, ref)
	out, err := cmd.CombinedOutput()
	if err != nil {
		return fmt.Errorf("source/git: worktree add @%q → %s: %w\n%s", ref, dest, err, out)
	}
	return nil
}

// Release removes a worktree registration if dest is a git worktree.
func (p *Provider) Release(ctx context.Context, dest string) error {
	if dest == "" {
		return nil
	}
	gitMeta := filepath.Join(dest, ".git")
	if _, err := os.Stat(gitMeta); err != nil {
		return nil
	}
	cmd := exec.CommandContext(ctx, "git", "-C", dest, "worktree", "remove", "--force", dest)
	_ = cmd.Run() // best-effort; Destroy still RemoveAll's the dir
	return nil
}

// ensureMirror returns the bare mirror path and an action label: clone|fetch|cache.
func (p *Provider) ensureMirror(ctx context.Context, repo string, forceFetch bool) (string, string, error) {
	mirror := filepath.Join(p.ReposRoot, repoHash(repo))
	gitDir := filepath.Join(mirror, "HEAD")
	if _, err := os.Stat(gitDir); err == nil {
		if !forceFetch && p.mirrorFresh(mirror) {
			return mirror, "cache", nil
		}
		cmd := exec.CommandContext(ctx, "git", "-C", mirror, "fetch", "--all", "--prune")
		out, err := cmd.CombinedOutput()
		if err != nil {
			return "", "", fmt.Errorf("source/git: fetch mirror %s: %w\n%s", mirror, err, out)
		}
		_ = touchMirrorStamp(mirror)
		return mirror, "fetch", nil
	}

	if err := os.MkdirAll(filepath.Dir(mirror), 0o755); err != nil {
		return "", "", err
	}
	// Remove partial mirror dirs from failed clones.
	_ = os.RemoveAll(mirror)

	log.Printf("source/git: cloning mirror %s → %s", repo, mirror)
	cmd := exec.CommandContext(ctx, "git", "clone", "--mirror", repo, mirror)
	out, err := cmd.CombinedOutput()
	if err != nil {
		_ = os.RemoveAll(mirror)
		return "", "", fmt.Errorf("source/git: clone --mirror %q: %w\n%s", repo, err, out)
	}
	_ = touchMirrorStamp(mirror)
	return mirror, "clone", nil
}

func (p *Provider) mirrorFresh(mirror string) bool {
	maxAge := p.MirrorMaxAge
	if maxAge == 0 {
		maxAge = DefaultMirrorMaxAge
	}
	if maxAge < 0 {
		return false // always fetch
	}
	stamp := mirrorStampPath(mirror)
	fi, err := os.Stat(stamp)
	if err != nil {
		// Fall back to HEAD mtime for mirrors created before stamp files.
		fi, err = os.Stat(filepath.Join(mirror, "HEAD"))
		if err != nil {
			return false
		}
	}
	return time.Since(fi.ModTime()) <= maxAge
}

func mirrorStampPath(mirror string) string {
	return filepath.Join(mirror, "cell-mirror.stamp")
}

func touchMirrorStamp(mirror string) error {
	path := mirrorStampPath(mirror)
	f, err := os.OpenFile(path, os.O_CREATE|os.O_WRONLY|os.O_TRUNC, 0o644)
	if err != nil {
		return err
	}
	_, _ = f.WriteString(time.Now().UTC().Format(time.RFC3339Nano) + "\n")
	return f.Close()
}

func prepareDest(dest string) error {
	info, err := os.Stat(dest)
	if err != nil {
		if os.IsNotExist(err) {
			return nil
		}
		return err
	}
	if !info.IsDir() {
		return fmt.Errorf("source/git: destination exists and is not a directory: %s", dest)
	}
	entries, err := os.ReadDir(dest)
	if err != nil {
		return err
	}
	if len(entries) > 0 {
		return fmt.Errorf("source/git: destination not empty: %s", dest)
	}
	return os.Remove(dest)
}

func repoHash(repo string) string {
	sum := sha256.Sum256([]byte(repo))
	return hex.EncodeToString(sum[:16])
}

// ErrNotImplemented is returned by Stub when a non-empty repo is requested.
var ErrNotImplemented = fmt.Errorf("source/git: not implemented")

// Stub skips cloning. Empty repo is a no-op; non-empty returns ErrNotImplemented.
type Stub struct {
	AllowSkipEmpty bool
}

// NewStub returns a git source stub. AllowSkipEmpty defaults to true.
func NewStub() *Stub {
	return &Stub{AllowSkipEmpty: true}
}

// Fetch: empty repo is skipped; otherwise not implemented (use Provider).
func (s *Stub) Fetch(ctx context.Context, repo, ref, dest string) (FetchStats, error) {
	_ = ctx
	_ = ref
	_ = dest
	_ = s.AllowSkipEmpty
	if strings.TrimSpace(repo) == "" {
		return FetchStats{Mirror: "skip"}, nil
	}
	return FetchStats{}, fmt.Errorf("%w: clone of %q", ErrNotImplemented, repo)
}

// Release is a no-op for Stub.
func (s *Stub) Release(ctx context.Context, dest string) error {
	_ = ctx
	_ = dest
	return nil
}

var _ Source = (*Provider)(nil)
var _ Source = (*Stub)(nil)
