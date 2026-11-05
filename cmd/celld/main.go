// Command celld is the Cell control plane / local worker daemon.
package main

import (
	"context"
	"flag"
	"fmt"
	"log"
	"os"
	"os/signal"
	"path/filepath"
	"syscall"
	"time"

	ctrlapi "github.com/YanChao1999/Cell/internal/control/api"
	"github.com/YanChao1999/Cell/internal/control/meta"
	"github.com/YanChao1999/Cell/internal/control/reaper"
	"github.com/YanChao1999/Cell/internal/control/scheduler"
	"github.com/YanChao1999/Cell/internal/worker/cache"
	"github.com/YanChao1999/Cell/internal/worker/envtemplate"
	"github.com/YanChao1999/Cell/internal/worker/manager"
	"github.com/YanChao1999/Cell/internal/worker/runtime/docker"
	"github.com/YanChao1999/Cell/internal/worker/source/git"
	"github.com/YanChao1999/Cell/internal/workerapi"
)

func main() {
	if len(os.Args) < 2 {
		usage()
		os.Exit(2)
	}
	switch os.Args[1] {
	case "serve":
		if err := runServe(os.Args[2:]); err != nil {
			log.Fatal(err)
		}
	case "version", "-version", "--version":
		fmt.Println("celld 0.3.0-phase3")
	case "help", "-h", "--help":
		usage()
	default:
		fmt.Fprintf(os.Stderr, "unknown command: %s\n", os.Args[1])
		usage()
		os.Exit(2)
	}
}

func usage() {
	fmt.Fprintf(os.Stderr, `celld — Cell control plane / worker daemon

Usage:
  celld serve --mode=all --socket ~/.cell/celld.sock

Flags (serve):
  --mode            all | control | worker (MVP: all)
  --socket          unix socket (default ~/.cell/celld.sock)
  --cell-home       base dir (default ~/.cell); sets defaults for other paths
  --state-dir       meta state dir (default <cell-home>/state)
  --workspaces      workspace root (default <cell-home>/workspaces)
  --repos           bare git mirrors (default <cell-home>/repos)
  --cache-dir       package caches (default <cell-home>/cache)
  --environments    env template YAML dir (default ./environments)
  --worker-id       local worker id (default local)

Commands:
  serve     Start the daemon
  version   Print version
`)
}

func runServe(args []string) error {
	fs := flag.NewFlagSet("serve", flag.ExitOnError)
	mode := fs.String("mode", "all", "mode: all | control | worker")
	socket := fs.String("socket", "", "unix socket path (default <cell-home>/celld.sock)")
	cellHomeFlag := fs.String("cell-home", "", "cell home directory (default ~/.cell)")
	stateDir := fs.String("state-dir", "", "meta state dir (default <cell-home>/state)")
	workspaces := fs.String("workspaces", "", "workspace root (default <cell-home>/workspaces)")
	repos := fs.String("repos", "", "bare git mirror root (default <cell-home>/repos)")
	cacheDir := fs.String("cache-dir", "", "cache root (default <cell-home>/cache)")
	environments := fs.String("environments", "", "environment templates dir (default ./environments)")
	workerID := fs.String("worker-id", "local", "local worker id")
	if err := fs.Parse(args); err != nil {
		return err
	}

	home, err := os.UserHomeDir()
	if err != nil {
		return err
	}
	cellHome := *cellHomeFlag
	if cellHome == "" {
		cellHome = filepath.Join(home, ".cell")
	}
	cellHome, err = filepath.Abs(cellHome)
	if err != nil {
		return fmt.Errorf("cell-home: %w", err)
	}
	if *socket == "" {
		*socket = filepath.Join(cellHome, "celld.sock")
	}
	if *stateDir == "" {
		*stateDir = filepath.Join(cellHome, "state")
	}
	if *workspaces == "" {
		*workspaces = filepath.Join(cellHome, "workspaces")
	}
	if *repos == "" {
		*repos = filepath.Join(cellHome, "repos")
	}
	if *cacheDir == "" {
		*cacheDir = filepath.Join(cellHome, "cache")
	}
	if *environments == "" {
		*environments = "environments"
	}
	if *mode != "all" && *mode != "control" && *mode != "worker" {
		return fmt.Errorf("unsupported mode %q (phase3 supports all)", *mode)
	}
	_ = *mode

	// Docker bind mounts and git worktrees require absolute roots.
	for _, p := range []*string{socket, stateDir, workspaces, repos, cacheDir} {
		abs, err := filepath.Abs(*p)
		if err != nil {
			return err
		}
		*p = abs
	}

	store, err := meta.New(*stateDir)
	if err != nil {
		return fmt.Errorf("meta: %w", err)
	}

	src, err := git.New(*repos)
	if err != nil {
		return fmt.Errorf("git source: %w", err)
	}

	envAbs, err := filepath.Abs(*environments)
	if err != nil {
		return fmt.Errorf("environments: %w", err)
	}
	envLoader := envtemplate.New(envAbs)
	cacheLayout := cache.NewLayout(*cacheDir)
	if err := cacheLayout.Ensure(); err != nil {
		return fmt.Errorf("cache: %w", err)
	}
	log.Printf("celld: environments=%s repos=%s cache=%s", envAbs, *repos, *cacheDir)

	ctx, cancel := signal.NotifyContext(context.Background(), syscall.SIGINT, syscall.SIGTERM)
	defer cancel()

	// Security: after restart, worker/runtime maps are empty. Tear down any
	// leftover containers and drop unrecovered meta rather than advertising
	// ghost Cells that exec would miss (or worse, host-escape).
	if err := docker.PruneLabeledContainers(ctx); err != nil {
		log.Printf("celld: prune orphan containers: %v", err)
	}
	for _, rec := range store.List() {
		_ = os.RemoveAll(filepath.Join(*workspaces, rec.ID))
	}
	if err := store.Reset(); err != nil {
		return fmt.Errorf("meta reset on startup: %w", err)
	}

	lw, err := manager.New(manager.Options{
		ID:             *workerID,
		WorkspacesRoot: *workspaces,
		CacheDir:       *cacheDir,
		CacheLayout:    cacheLayout,
		Runtime:        docker.New(),
		Source:         src,
		EnvLoader:      envLoader,
	})
	if err != nil {
		return fmt.Errorf("worker: %w", err)
	}

	sched := scheduler.New([]workerapi.Worker{lw}, *workerID)
	srv := ctrlapi.New(store, sched)

	rp := reaper.New(store, sched, srv.DestroyCell, 15*time.Second)
	go rp.Run(ctx)

	errCh := make(chan error, 1)
	go func() {
		errCh <- srv.ListenAndServe(*socket)
	}()

	select {
	case <-ctx.Done():
		_ = os.Remove(*socket)
		log.Println("celld shutting down")
		return nil
	case err := <-errCh:
		return err
	}
}
