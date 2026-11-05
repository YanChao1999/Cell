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
)

// Source fetches repository content into a workspace.
type Source interface {
	Fetch(ctx context.Context, repo, ref, dest string) error
	// Release unregisters a worktree (best-effort). Safe to call for empty repos.
	Release(ctx context.Context, dest string) error
}

// Provider mirrors remotes under ReposRoot and checks out a worktree into dest.
type Provider struct {
	ReposRoot string
}

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
	return &Provider{ReposRoot: reposRoot}, nil
}

// Fetch clones/fetches a bare mirror then adds a detached worktree at dest.
// Empty repo is a no-op (skip).
func (p *Provider) Fetch(ctx context.Context, repo, ref, dest string) error {
	repo = strings.TrimSpace(repo)
	if repo == "" {
		return nil
	}
	if _, err := exec.LookPath("git"); err != nil {
		return fmt.Errorf("source/git: git not found in PATH: %w", err)
	}

	mirror, err := p.ensureMirror(ctx, repo)
	if err != nil {
		return err
	}

	if ref == "" {
		ref = "HEAD"
	}

	if err := prepareDest(dest); err != nil {
		return err
	}

	cmd := exec.CommandContext(ctx, "git", "-C", mirror, "worktree", "add", "--detach", dest, ref)
	out, err := cmd.CombinedOutput()
	if err != nil {
		return fmt.Errorf("source/git: worktree add %q@%q → %s: %w\n%s", repo, ref, dest, err, out)
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

func (p *Provider) ensureMirror(ctx context.Context, repo string) (string, error) {
	mirror := filepath.Join(p.ReposRoot, repoHash(repo))
	gitDir := filepath.Join(mirror, "HEAD")
	if _, err := os.Stat(gitDir); err == nil {
		cmd := exec.CommandContext(ctx, "git", "-C", mirror, "fetch", "--all", "--prune")
		out, err := cmd.CombinedOutput()
		if err != nil {
			return "", fmt.Errorf("source/git: fetch mirror %s: %w\n%s", mirror, err, out)
		}
		return mirror, nil
	}

	if err := os.MkdirAll(filepath.Dir(mirror), 0o755); err != nil {
		return "", err
	}
	// Remove partial mirror dirs from failed clones.
	_ = os.RemoveAll(mirror)

	log.Printf("source/git: cloning mirror %s → %s", repo, mirror)
	cmd := exec.CommandContext(ctx, "git", "clone", "--mirror", repo, mirror)
	out, err := cmd.CombinedOutput()
	if err != nil {
		_ = os.RemoveAll(mirror)
		return "", fmt.Errorf("source/git: clone --mirror %q: %w\n%s", repo, err, out)
	}
	return mirror, nil
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
func (s *Stub) Fetch(ctx context.Context, repo, ref, dest string) error {
	_ = ctx
	_ = ref
	_ = dest
	_ = s.AllowSkipEmpty
	if strings.TrimSpace(repo) == "" {
		return nil
	}
	return fmt.Errorf("%w: clone of %q", ErrNotImplemented, repo)
}

// Release is a no-op for Stub.
func (s *Stub) Release(ctx context.Context, dest string) error {
	_ = ctx
	_ = dest
	return nil
}

var _ Source = (*Provider)(nil)
var _ Source = (*Stub)(nil)
