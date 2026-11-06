// Package manager implements LocalWorker: cell lifecycle on a single host.
package manager

import (
	"context"
	"crypto/rand"
	"encoding/hex"
	"fmt"
	"os"
	"path/filepath"
	"strings"
	"sync"
	"time"

	"github.com/YanChao1999/Cell/api"
	"github.com/YanChao1999/Cell/internal/worker/cache"
	"github.com/YanChao1999/Cell/internal/worker/envtemplate"
	"github.com/YanChao1999/Cell/internal/worker/runtime"
	"github.com/YanChao1999/Cell/internal/worker/source/git"
	"github.com/YanChao1999/Cell/internal/workerapi"
)

// LocalWorker manages Cells on the local machine.
type LocalWorker struct {
	id         string
	maxCells   int
	workspaces string
	reposRoot  string
	rt         runtime.Runtime
	source     git.Source
	envs       *envtemplate.Loader
	cache      *cache.Layout
	cacheIndex *cache.Index
	mu         sync.Mutex
	cells      map[string]*workerapi.Cell
}

// Options configure a LocalWorker.
type Options struct {
	ID             string
	MaxCells       int
	WorkspacesRoot string // default ~/.cell/workspaces
	ReposRoot      string // bare git mirrors (for Docker same-path RO bind)
	CacheDir       string // durable package caches (default ~/.cell/cache)
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
	if opts.MaxCells <= 0 {
		opts.MaxCells = 32
	}
	if opts.WorkspacesRoot == "" {
		home, err := os.UserHomeDir()
		if err != nil {
			return nil, err
		}
		opts.WorkspacesRoot = filepath.Join(home, ".cell", "workspaces")
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
			cacheRoot = filepath.Join(home, ".cell", "cache")
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
	reposRoot := opts.ReposRoot
	if reposRoot == "" {
		if p, ok := opts.Source.(*git.Provider); ok {
			reposRoot = p.ReposRoot
		}
	}
	return &LocalWorker{
		id:         opts.ID,
		maxCells:   opts.MaxCells,
		workspaces: opts.WorkspacesRoot,
		reposRoot:  reposRoot,
		rt:         opts.Runtime,
		source:     opts.Source,
		envs:       opts.EnvLoader,
		cache:      layout,
		cacheIndex: opts.CacheIndex,
		cells:      make(map[string]*workerapi.Cell),
	}, nil
}

func (w *LocalWorker) ID() string { return w.id }

func (w *LocalWorker) Capacity() workerapi.Capacity {
	w.mu.Lock()
	defer w.mu.Unlock()
	return workerapi.Capacity{MaxCells: w.maxCells, UsedCells: len(w.cells)}
}

func (w *LocalWorker) CacheIndex() workerapi.CacheIndex { return w.cacheIndex }

// CreateCell allocates an id, prepares workspace from source, resolves the
// environment template, then calls Runtime.Create.
func (w *LocalWorker) CreateCell(ctx context.Context, req api.CreateCellRequest) (*workerapi.Cell, error) {
	w.mu.Lock()
	if len(w.cells) >= w.maxCells {
		w.mu.Unlock()
		return nil, fmt.Errorf("worker %s: at capacity", w.id)
	}
	id, err := newCellID()
	if err != nil {
		w.mu.Unlock()
		return nil, err
	}
	ws := filepath.Join(w.workspaces, id)
	cell := &workerapi.Cell{
		ID:          id,
		Workspace:   ws,
		Environment: req.Environment,
		Status:      "creating",
	}
	w.cells[id] = cell
	w.mu.Unlock()

	if err := os.MkdirAll(ws, 0o755); err != nil {
		w.removeCell(id)
		return nil, err
	}

	if err := w.source.Fetch(ctx, req.Repo, req.Ref, ws); err != nil {
		_ = os.RemoveAll(ws)
		w.removeCell(id)
		return nil, err
	}

	image, limits, envVars, mounts, network, egressAllow, setup, err := w.resolveEnv(req)
	if err != nil {
		_ = w.source.Release(ctx, ws)
		_ = os.RemoveAll(ws)
		w.removeCell(id)
		return nil, err
	}

	for _, m := range mounts {
		w.cacheIndex.Add(m.HostKey)
	}

	createOpts := runtime.CreateOpts{
		CellID:       id,
		Workspace:    ws,
		Environment:  req.Environment,
		Image:        image,
		Limits:       limits,
		Env:          envVars,
		Network:      network,
		EgressAllow:  egressAllow,
		CacheMounts:  mounts,
		GitReposRoot: w.reposRoot,
	}
	// Docker: use per-Cell writable cache dirs so one Cell cannot poison the
	// durable shared host cache. Host fallback keeps shared dirs via env.
	if dockerIsolating(w.rt) {
		cellMounts, err := w.perCellCacheMounts(ws, mounts)
		if err != nil {
			_ = w.source.Release(ctx, ws)
			_ = os.RemoveAll(ws)
			w.removeCell(id)
			return nil, err
		}
		createOpts.CacheMounts = cellMounts
	}

	if err := w.rt.Create(ctx, createOpts); err != nil {
		_ = w.source.Release(ctx, ws)
		_ = os.RemoveAll(ws)
		w.removeCell(id)
		return nil, err
	}

	w.mu.Lock()
	cell.DiskLimit = limits.Disk
	w.mu.Unlock()

	if len(setup) > 0 {
		w.mu.Lock()
		cell.Status = "setup"
		w.mu.Unlock()
		setupTimeout := w.setupTimeoutSeconds(req.Environment)
		if err := w.runSetup(ctx, id, ws, setup, setupTimeout); err != nil {
			_ = w.rt.Destroy(ctx, id)
			_ = w.source.Release(ctx, ws)
			_ = os.RemoveAll(ws)
			w.removeCell(id)
			return nil, err
		}
		if err := checkWorkspaceDisk(ws, limits.Disk); err != nil {
			_ = w.rt.Destroy(ctx, id)
			_ = w.source.Release(ctx, ws)
			_ = os.RemoveAll(ws)
			w.removeCell(id)
			return nil, err
		}
	}

	w.mu.Lock()
	cell.Status = "ready"
	w.mu.Unlock()
	return cell, nil
}

func (w *LocalWorker) runSetup(ctx context.Context, cellID, workspace string, steps []envtemplate.SetupStep, timeoutSeconds int) error {
	for i, step := range steps {
		run := strings.TrimSpace(step.Run)
		if run == "" {
			continue
		}
		skip, err := w.setupShouldSkip(ctx, cellID, workspace, i, step, timeoutSeconds)
		if err != nil {
			return err
		}
		if skip {
			continue
		}
		res, err := w.rt.Exec(ctx, runtime.ExecOpts{
			CellID:         cellID,
			Workspace:      workspace,
			Command:        run,
			TimeoutSeconds: timeoutSeconds,
		})
		if err != nil {
			return fmt.Errorf("manager: setup[%d] %q: %w", i, run, err)
		}
		if res != nil && res.ExitCode != 0 {
			return fmt.Errorf("manager: setup[%d] %q: exit %d\nstdout:\n%s\nstderr:\n%s",
				i, run, res.ExitCode, res.Stdout, res.Stderr)
		}
	}
	return nil
}

// setupShouldSkip evaluates if/unless predicates. skip=true means do not run the step.
func (w *LocalWorker) setupShouldSkip(ctx context.Context, cellID, workspace string, i int, step envtemplate.SetupStep, timeoutSeconds int) (bool, error) {
	if cond := strings.TrimSpace(step.If); cond != "" {
		ok, err := w.setupCondOK(ctx, cellID, workspace, timeoutSeconds, cond)
		if err != nil {
			return false, fmt.Errorf("manager: setup[%d] if %q: %w", i, cond, err)
		}
		if !ok {
			return true, nil
		}
	}
	if cond := strings.TrimSpace(step.Unless); cond != "" {
		ok, err := w.setupCondOK(ctx, cellID, workspace, timeoutSeconds, cond)
		if err != nil {
			return false, fmt.Errorf("manager: setup[%d] unless %q: %w", i, cond, err)
		}
		if ok {
			return true, nil
		}
	}
	return false, nil
}

func (w *LocalWorker) setupCondOK(ctx context.Context, cellID, workspace string, timeoutSeconds int, cond string) (bool, error) {
	res, err := w.rt.Exec(ctx, runtime.ExecOpts{
		CellID: cellID, Workspace: workspace, Command: cond, TimeoutSeconds: timeoutSeconds,
	})
	if err != nil {
		return false, err
	}
	return res == nil || res.ExitCode == 0, nil
}

func (w *LocalWorker) resolveEnv(req api.CreateCellRequest) (
	image string,
	limits api.Limits,
	env map[string]string,
	mounts []cache.Mount,
	network string,
	egressAllow []string,
	setup []envtemplate.SetupStep,
	err error,
) {
	limits = req.Limits
	if req.Environment == "" {
		return "", limits, nil, nil, "", nil, nil, nil
	}
	if w.envs == nil {
		return "", limits, nil, nil, "", nil, nil, fmt.Errorf("manager: environment %q requested but no environments dir configured", req.Environment)
	}
	tmpl, err := w.envs.Load(req.Environment)
	if err != nil {
		return "", limits, nil, nil, "", nil, nil, err
	}
	image = tmpl.Image
	env = cloneStringMap(tmpl.Env)
	network = tmpl.Network
	egressAllow = append([]string(nil), tmpl.EgressAllow...)
	setup = append([]envtemplate.SetupStep(nil), tmpl.Setup...)
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
			return "", limits, nil, nil, "", nil, nil, err
		}
	}
	return image, limits, env, mounts, network, egressAllow, setup, nil
}

func (w *LocalWorker) Exec(ctx context.Context, cellID string, req api.ExecRequest) (*api.ExecResult, error) {
	w.mu.Lock()
	cell, ok := w.cells[cellID]
	w.mu.Unlock()
	if !ok {
		return nil, fmt.Errorf("cell %s not found on worker %s", cellID, w.id)
	}
	if err := checkWorkspaceDisk(cell.Workspace, cell.DiskLimit); err != nil {
		return nil, err
	}
	timeout := req.TimeoutSeconds
	if timeout <= 0 {
		timeout = w.defaultExecTimeout(cell.Environment)
	}
	res, err := w.rt.Exec(ctx, runtime.ExecOpts{
		CellID:         cellID,
		Workspace:      cell.Workspace,
		Command:        req.Command,
		TimeoutSeconds: timeout,
	})
	if err != nil {
		return nil, err
	}
	if diskErr := checkWorkspaceDisk(cell.Workspace, cell.DiskLimit); diskErr != nil {
		return nil, diskErr
	}
	return res, nil
}

func (w *LocalWorker) parseTimeoutSeconds(raw string) int {
	raw = strings.TrimSpace(raw)
	if raw == "" {
		return 0
	}
	d, err := time.ParseDuration(raw)
	if err != nil || d <= 0 {
		return 0
	}
	secs := int(d.Seconds())
	if secs < 1 {
		secs = 1
	}
	return secs
}

func (w *LocalWorker) defaultExecTimeout(environment string) int {
	if environment == "" || w.envs == nil {
		return 0
	}
	tmpl, err := w.envs.Load(environment)
	if err != nil {
		return 0
	}
	return w.parseTimeoutSeconds(tmpl.DefaultExecTimeout)
}

func (w *LocalWorker) setupTimeoutSeconds(environment string) int {
	if environment == "" || w.envs == nil {
		return 0
	}
	tmpl, err := w.envs.Load(environment)
	if err != nil {
		return 0
	}
	if secs := w.parseTimeoutSeconds(tmpl.SetupTimeout); secs > 0 {
		return secs
	}
	return w.parseTimeoutSeconds(tmpl.DefaultExecTimeout)
}

func (w *LocalWorker) DestroyCell(ctx context.Context, cellID string) error {
	w.mu.Lock()
	cell, ok := w.cells[cellID]
	if !ok {
		w.mu.Unlock()
		return fmt.Errorf("cell %s not found on worker %s", cellID, w.id)
	}
	ws := cell.Workspace
	w.mu.Unlock()

	if err := w.rt.Destroy(ctx, cellID); err != nil {
		return err
	}
	_ = w.source.Release(ctx, ws)
	if ws != "" {
		if err := os.RemoveAll(ws); err != nil {
			return fmt.Errorf("manager: remove workspace %s: %w", ws, err)
		}
	}
	w.removeCell(cellID)
	return nil
}

func (w *LocalWorker) removeCell(id string) {
	w.mu.Lock()
	defer w.mu.Unlock()
	delete(w.cells, id)
}

// Get returns a local cell record if present.
func (w *LocalWorker) Get(cellID string) (*workerapi.Cell, bool) {
	w.mu.Lock()
	defer w.mu.Unlock()
	s, ok := w.cells[cellID]
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

// dockerIsolating reports whether the runtime will create a Docker Cell (not host fallback).
func dockerIsolating(rt runtime.Runtime) bool {
	type dockerAvail interface{ Available() bool }
	if d, ok := rt.(dockerAvail); ok {
		return d.Available()
	}
	return false
}

// perCellCacheMounts places tool caches under the Cell workspace so Docker
// binds are unique per Cell. Shared ~/.cell/cache remains for host fallback.
// When a shared durable dir exists, it is copied into the per-Cell dir with
// cache.SeedCopy (no hardlinks — Cell writes cannot poison shared).
func (w *LocalWorker) perCellCacheMounts(ws string, shared []cache.Mount) ([]cache.Mount, error) {
	if len(shared) == 0 {
		return nil, nil
	}
	out := make([]cache.Mount, 0, len(shared))
	for _, m := range shared {
		key := m.HostKey
		if key == "" {
			key = filepath.Base(m.HostAbs)
		}
		host := filepath.Join(ws, ".cell-cache", key)
		if err := os.MkdirAll(host, 0o755); err != nil {
			return nil, fmt.Errorf("manager: per-cell cache %s: %w", key, err)
		}
		if m.HostAbs != "" && m.HostAbs != host {
			if err := cache.SeedCopy(m.HostAbs, host, cache.SeedOptions{}); err != nil {
				return nil, fmt.Errorf("manager: trusted cache seed %s: %w", key, err)
			}
		}
		out = append(out, cache.Mount{
			HostKey:   key,
			HostAbs:   host,
			Container: m.Container,
		})
	}
	return out, nil
}

func newCellID() (string, error) {
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
