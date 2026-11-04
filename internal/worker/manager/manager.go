// Package manager implements LocalWorker: spark lifecycle on a single host.
package manager

import (
	"context"
	"crypto/rand"
	"encoding/hex"
	"fmt"
	"os"
	"path/filepath"
	"sync"
	"time"

	"github.com/YanChao1999/spark/api"
	"github.com/YanChao1999/spark/internal/worker/cache"
	"github.com/YanChao1999/spark/internal/worker/envtemplate"
	"github.com/YanChao1999/spark/internal/worker/runtime"
	"github.com/YanChao1999/spark/internal/worker/source/git"
	"github.com/YanChao1999/spark/internal/workerapi"
)

// LocalWorker manages Sparks on the local machine.
type LocalWorker struct {
	id         string
	maxSparks  int
	workspaces string
	rt         runtime.Runtime
	source     git.Source
	envs       *envtemplate.Loader
	cache      *cache.Layout
	cacheIndex *cache.Index
	mu         sync.Mutex
	sparks     map[string]*workerapi.Spark
}

// Options configure a LocalWorker.
type Options struct {
	ID             string
	MaxSparks      int
	WorkspacesRoot string // default ~/.spark/workspaces
	CacheDir       string // durable package caches (default ~/.spark/cache)
	Runtime        runtime.Runtime
	Source         git.Source
	EnvLoader      *envtemplate.Loader
	CacheIndex     *cache.Index
	CacheLayout    *cache.Layout
}

// New creates a LocalWorker.
func New(opts Options) (*LocalWorker, error) {
	if opts.ID == "" {
		opts.ID = "local"
	}
	if opts.MaxSparks <= 0 {
		opts.MaxSparks = 32
	}
	if opts.WorkspacesRoot == "" {
		home, err := os.UserHomeDir()
		if err != nil {
			return nil, err
		}
		opts.WorkspacesRoot = filepath.Join(home, ".spark", "workspaces")
	}
	if opts.Runtime == nil {
		return nil, fmt.Errorf("manager: Runtime is required")
	}
	if opts.Source == nil {
		opts.Source = git.NewStub()
	}
	if opts.CacheIndex == nil {
		opts.CacheIndex = cache.NewIndex()
	}
	layout := opts.CacheLayout
	if layout == nil {
		cacheRoot := opts.CacheDir
		if cacheRoot == "" {
			home, err := os.UserHomeDir()
			if err != nil {
				return nil, err
			}
			cacheRoot = filepath.Join(home, ".spark", "cache")
		}
		layout = cache.NewLayout(cacheRoot)
	}
	if err := layout.Ensure(); err != nil {
		return nil, fmt.Errorf("manager: cache layout: %w", err)
	}
	opts.CacheIndex.SeedFromLayout(layout)

	if err := os.MkdirAll(opts.WorkspacesRoot, 0o755); err != nil {
		return nil, err
	}
	return &LocalWorker{
		id:         opts.ID,
		maxSparks:  opts.MaxSparks,
		workspaces: opts.WorkspacesRoot,
		rt:         opts.Runtime,
		source:     opts.Source,
		envs:       opts.EnvLoader,
		cache:      layout,
		cacheIndex: opts.CacheIndex,
		sparks:     make(map[string]*workerapi.Spark),
	}, nil
}

func (w *LocalWorker) ID() string { return w.id }

func (w *LocalWorker) Capacity() workerapi.Capacity {
	w.mu.Lock()
	defer w.mu.Unlock()
	return workerapi.Capacity{MaxSparks: w.maxSparks, UsedSparks: len(w.sparks)}
}

func (w *LocalWorker) CacheIndex() workerapi.CacheIndex { return w.cacheIndex }

// CreateSpark allocates an id, prepares workspace from source, resolves the
// environment template, then calls Runtime.Create.
func (w *LocalWorker) CreateSpark(ctx context.Context, req api.CreateSparkRequest) (*workerapi.Spark, error) {
	w.mu.Lock()
	if len(w.sparks) >= w.maxSparks {
		w.mu.Unlock()
		return nil, fmt.Errorf("worker %s: at capacity", w.id)
	}
	id, err := newSparkID()
	if err != nil {
		w.mu.Unlock()
		return nil, err
	}
	ws := filepath.Join(w.workspaces, id)
	spark := &workerapi.Spark{
		ID:          id,
		Workspace:   ws,
		Environment: req.Environment,
		Status:      "creating",
	}
	w.sparks[id] = spark
	w.mu.Unlock()

	if err := os.MkdirAll(ws, 0o755); err != nil {
		w.removeSpark(id)
		return nil, err
	}

	if err := w.source.Fetch(ctx, req.Repo, req.Ref, ws); err != nil {
		_ = os.RemoveAll(ws)
		w.removeSpark(id)
		return nil, err
	}

	image, limits, envVars, mounts, err := w.resolveEnv(req)
	if err != nil {
		_ = w.source.Release(ctx, ws)
		_ = os.RemoveAll(ws)
		w.removeSpark(id)
		return nil, err
	}

	for _, m := range mounts {
		w.cacheIndex.Add(m.HostKey)
	}

	if err := w.rt.Create(ctx, runtime.CreateOpts{
		SparkID:     id,
		Workspace:   ws,
		Environment: req.Environment,
		Image:       image,
		Limits:      limits,
		Env:         envVars,
		CacheMounts: mounts,
	}); err != nil {
		_ = w.source.Release(ctx, ws)
		_ = os.RemoveAll(ws)
		w.removeSpark(id)
		return nil, err
	}

	w.mu.Lock()
	spark.Status = "ready"
	w.mu.Unlock()
	return spark, nil
}

func (w *LocalWorker) resolveEnv(req api.CreateSparkRequest) (
	image string,
	limits api.Limits,
	env map[string]string,
	mounts []cache.Mount,
	err error,
) {
	limits = req.Limits
	if req.Environment == "" {
		return "", limits, nil, nil, nil
	}
	if w.envs == nil {
		return "", limits, nil, nil, fmt.Errorf("manager: environment %q requested but no environments dir configured", req.Environment)
	}
	tmpl, err := w.envs.Load(req.Environment)
	if err != nil {
		return "", limits, nil, nil, err
	}
	image = tmpl.Image
	env = cloneStringMap(tmpl.Env)
	if limits.CPU == "" {
		limits.CPU = tmpl.DefaultLimits.CPU
	}
	if limits.Memory == "" {
		limits.Memory = tmpl.DefaultLimits.Memory
	}
	if limits.Disk == "" {
		limits.Disk = tmpl.DefaultLimits.Disk
	}
	if len(tmpl.CacheMounts) > 0 && w.cache != nil {
		keys := make([]string, len(tmpl.CacheMounts))
		ctrs := make([]string, len(tmpl.CacheMounts))
		for i, m := range tmpl.CacheMounts {
			keys[i] = m.Host
			ctrs[i] = m.Container
		}
		mounts, err = w.cache.ResolveMounts(keys, ctrs)
		if err != nil {
			return "", limits, nil, nil, err
		}
	}
	return image, limits, env, mounts, nil
}

func (w *LocalWorker) Exec(ctx context.Context, sparkID string, req api.ExecRequest) (*api.ExecResult, error) {
	w.mu.Lock()
	spark, ok := w.sparks[sparkID]
	w.mu.Unlock()
	if !ok {
		return nil, fmt.Errorf("spark %s not found on worker %s", sparkID, w.id)
	}
	timeout := req.TimeoutSeconds
	if timeout <= 0 {
		timeout = w.defaultExecTimeout(spark.Environment)
	}
	return w.rt.Exec(ctx, runtime.ExecOpts{
		SparkID:        sparkID,
		Workspace:      spark.Workspace,
		Command:        req.Command,
		TimeoutSeconds: timeout,
	})
}

func (w *LocalWorker) defaultExecTimeout(environment string) int {
	if environment == "" || w.envs == nil {
		return 0
	}
	tmpl, err := w.envs.Load(environment)
	if err != nil || tmpl.DefaultExecTimeout == "" {
		return 0
	}
	d, err := time.ParseDuration(tmpl.DefaultExecTimeout)
	if err != nil || d <= 0 {
		return 0
	}
	secs := int(d.Seconds())
	if secs < 1 {
		secs = 1
	}
	return secs
}

func (w *LocalWorker) DestroySpark(ctx context.Context, sparkID string) error {
	w.mu.Lock()
	spark, ok := w.sparks[sparkID]
	if !ok {
		w.mu.Unlock()
		return fmt.Errorf("spark %s not found on worker %s", sparkID, w.id)
	}
	delete(w.sparks, sparkID)
	ws := spark.Workspace
	w.mu.Unlock()

	_ = w.rt.Destroy(ctx, sparkID)
	_ = w.source.Release(ctx, ws)
	if ws != "" {
		_ = os.RemoveAll(ws)
	}
	return nil
}

func (w *LocalWorker) removeSpark(id string) {
	w.mu.Lock()
	defer w.mu.Unlock()
	delete(w.sparks, id)
}

// Get returns a local spark record if present.
func (w *LocalWorker) Get(sparkID string) (*workerapi.Spark, bool) {
	w.mu.Lock()
	defer w.mu.Unlock()
	s, ok := w.sparks[sparkID]
	return s, ok
}

func cloneStringMap(in map[string]string) map[string]string {
	if len(in) == 0 {
		return nil
	}
	out := make(map[string]string, len(in))
	for k, v := range in {
		out[k] = v
	}
	return out
}

func newSparkID() (string, error) {
	var b [16]byte
	if _, err := rand.Read(b[:]); err != nil {
		return "", err
	}
	// UUID-like layout without external deps.
	b[6] = (b[6] & 0x0f) | 0x40
	b[8] = (b[8] & 0x3f) | 0x80
	dst := make([]byte, 36)
	hex.Encode(dst[0:8], b[0:4])
	dst[8] = '-'
	hex.Encode(dst[9:13], b[4:6])
	dst[13] = '-'
	hex.Encode(dst[14:18], b[6:8])
	dst[18] = '-'
	hex.Encode(dst[19:23], b[8:10])
	dst[23] = '-'
	hex.Encode(dst[24:36], b[10:16])
	return string(dst), nil
}

var _ workerapi.Worker = (*LocalWorker)(nil)
