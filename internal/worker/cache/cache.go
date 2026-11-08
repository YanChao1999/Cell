// Package cache implements durable package caches and a warm-placement index.
package cache

import (
	"fmt"
	"os"
	"path/filepath"
	"strings"
	"sync"
)

// Well-known host cache directory names under --cache-dir.
// Templates reference these via cache_mounts[].host.
var WellKnownDirs = []string{
	"go-mod",
	"go-build",
	"pip-cache",
	"python-venv",
	"zephyr-modules",
	"west-cache",
	"yocto-downloads",
	"yocto-sstate",
	"ccache",
}

// hostKeyEnv maps cache_mounts host keys to host-fallback environment variables.
// When Docker is unavailable, exec uses these so tools still hit durable caches.
var hostKeyEnv = map[string]string{
	"go-mod":          "GOMODCACHE",
	"go-build":        "GOCACHE",
	"gomod":           "GOMODCACHE",
	"pip-cache":       "PIP_CACHE_DIR",
	"pip":             "PIP_CACHE_DIR",
	"ccache":          "CCACHE_DIR",
	"west-cache":      "WEST_CACHE",
	"zephyr-modules":  "ZEPHYR_MODULES",
	"yocto-downloads": "DL_DIR",
	"yocto-sstate":    "SSTATE_DIR",
}

// Mount is a resolved host↔container cache bind.
type Mount struct {
	HostAbs   string // absolute path under cache-dir
	Container string // path inside the container
	HostKey   string // relative key from the env template
}

// Layout manages durable cache directories under a root (--cache-dir).
type Layout struct {
	Root string
}

// NewLayout returns a Layout for root. Empty root is invalid for Ensure/Resolve.
func NewLayout(root string) *Layout {
	return &Layout{Root: root}
}

// Ensure creates the cache root and well-known subdirectories.
func (l *Layout) Ensure() error {
	if l == nil || l.Root == "" {
		return fmt.Errorf("cache: empty cache root")
	}
	if err := os.MkdirAll(l.Root, 0o755); err != nil {
		return err
	}
	for _, name := range WellKnownDirs {
		if err := os.MkdirAll(filepath.Join(l.Root, name), 0o755); err != nil {
			return err
		}
	}
	return nil
}

// Resolve returns the absolute host path for a template host key and creates it.
// Host keys are single path segments (no ".." or separators).
func (l *Layout) Resolve(hostKey string) (string, error) {
	key, err := sanitizeHostKey(hostKey)
	if err != nil {
		return "", err
	}
	if l == nil || l.Root == "" {
		return "", fmt.Errorf("cache: empty cache root")
	}
	abs := filepath.Join(l.Root, key)
	if err := os.MkdirAll(abs, 0o755); err != nil {
		return "", err
	}
	return abs, nil
}

// ResolveMounts resolves template host keys to absolute mounts under the layout.
func (l *Layout) ResolveMounts(hostKeys, containers []string) ([]Mount, error) {
	if len(hostKeys) != len(containers) {
		return nil, fmt.Errorf("cache: host/container mount length mismatch")
	}
	out := make([]Mount, 0, len(hostKeys))
	for i, key := range hostKeys {
		abs, err := l.Resolve(key)
		if err != nil {
			return nil, err
		}
		ctr := strings.TrimSpace(containers[i])
		if ctr == "" {
			return nil, fmt.Errorf("cache: empty container path for host key %q", key)
		}
		out = append(out, Mount{HostAbs: abs, Container: ctr, HostKey: key})
	}
	return out, nil
}

// HostEnv returns environment variables for host-fallback exec so tools use
// the durable cache directories (GOMODCACHE, PIP_CACHE_DIR, …).
func HostEnv(mounts []Mount) map[string]string {
	env := make(map[string]string)
	for _, m := range mounts {
		if varName, ok := hostKeyEnv[m.HostKey]; ok && m.HostAbs != "" {
			env[varName] = m.HostAbs
		}
	}
	return env
}

// EnvVarForHostKey returns the host-fallback env var name for a cache host key.
func EnvVarForHostKey(hostKey string) (string, bool) {
	key, err := sanitizeHostKey(hostKey)
	if err != nil {
		return "", false
	}
	v, ok := hostKeyEnv[key]
	return v, ok
}

func sanitizeHostKey(hostKey string) (string, error) {
	key := strings.TrimSpace(hostKey)
	if key == "" {
		return "", fmt.Errorf("cache: empty host key")
	}
	if strings.Contains(key, "..") || strings.ContainsAny(key, `/\`) || filepath.IsAbs(key) {
		return "", fmt.Errorf("cache: invalid host key %q", hostKey)
	}
	return key, nil
}

// Index is an in-memory CacheIndex of durable cache keys on a worker.
type Index struct {
	mu   sync.RWMutex
	keys map[string]struct{}
}

// NewIndex returns an empty cache index.
func NewIndex() *Index {
	return &Index{keys: make(map[string]struct{})}
}

// Has reports whether key is present.
func (i *Index) Has(key string) bool {
	i.mu.RLock()
	defer i.mu.RUnlock()
	_, ok := i.keys[key]
	return ok
}

// Keys returns all cache keys.
func (i *Index) Keys() []string {
	i.mu.RLock()
	defer i.mu.RUnlock()
	out := make([]string, 0, len(i.keys))
	for k := range i.keys {
		out = append(out, k)
	}
	return out
}

// Add registers a durable cache key.
func (i *Index) Add(key string) {
	i.mu.Lock()
	defer i.mu.Unlock()
	i.keys[key] = struct{}{}
}

// Remove drops a cache key.
func (i *Index) Remove(key string) {
	i.mu.Lock()
	defer i.mu.Unlock()
	delete(i.keys, key)
}

// SeedFromLayout registers warm (non-empty) cache directories into the index.
// Empty well-known dirs are not advertised so placement/pull see real warmth.
func (i *Index) SeedFromLayout(l *Layout) {
	if i == nil || l == nil {
		return
	}
	for _, key := range l.WarmKeys() {
		i.Add(key)
	}
}
