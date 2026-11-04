// Command sparkd is the Spark control plane / local worker daemon.
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

	ctrlapi "github.com/YanChao1999/spark/internal/control/api"
	"github.com/YanChao1999/spark/internal/control/meta"
	"github.com/YanChao1999/spark/internal/control/reaper"
	"github.com/YanChao1999/spark/internal/control/scheduler"
	"github.com/YanChao1999/spark/internal/worker/cache"
	"github.com/YanChao1999/spark/internal/worker/envtemplate"
	"github.com/YanChao1999/spark/internal/worker/manager"
	"github.com/YanChao1999/spark/internal/worker/runtime/docker"
	"github.com/YanChao1999/spark/internal/worker/source/git"
	"github.com/YanChao1999/spark/internal/workerapi"
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
		fmt.Println("sparkd 0.3.0-phase3")
	case "help", "-h", "--help":
		usage()
	default:
		fmt.Fprintf(os.Stderr, "unknown command: %s\n", os.Args[1])
		usage()
		os.Exit(2)
	}
}

func usage() {
	fmt.Fprintf(os.Stderr, `sparkd — Spark control plane / worker daemon

Usage:
  sparkd serve --mode=all --socket ~/.spark/sparkd.sock

Flags (serve):
  --mode            all | control | worker (MVP: all)
  --socket          unix socket (default ~/.spark/sparkd.sock)
  --spark-home      base dir (default ~/.spark); sets defaults for other paths
  --state-dir       meta state dir (default <spark-home>/state)
  --workspaces      workspace root (default <spark-home>/workspaces)
  --repos           bare git mirrors (default <spark-home>/repos)
  --cache-dir       package caches (default <spark-home>/cache)
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
	socket := fs.String("socket", "", "unix socket path (default <spark-home>/sparkd.sock)")
	sparkHomeFlag := fs.String("spark-home", "", "spark home directory (default ~/.spark)")
	stateDir := fs.String("state-dir", "", "meta state dir (default <spark-home>/state)")
	workspaces := fs.String("workspaces", "", "workspace root (default <spark-home>/workspaces)")
	repos := fs.String("repos", "", "bare git mirror root (default <spark-home>/repos)")
	cacheDir := fs.String("cache-dir", "", "cache root (default <spark-home>/cache)")
	environments := fs.String("environments", "", "environment templates dir (default ./environments)")
	workerID := fs.String("worker-id", "local", "local worker id")
	if err := fs.Parse(args); err != nil {
		return err
	}

	home, err := os.UserHomeDir()
	if err != nil {
		return err
	}
	sparkHome := *sparkHomeFlag
	if sparkHome == "" {
		sparkHome = filepath.Join(home, ".spark")
	}
	if *socket == "" {
		*socket = filepath.Join(sparkHome, "sparkd.sock")
	}
	if *stateDir == "" {
		*stateDir = filepath.Join(sparkHome, "state")
	}
	if *workspaces == "" {
		*workspaces = filepath.Join(sparkHome, "workspaces")
	}
	if *repos == "" {
		*repos = filepath.Join(sparkHome, "repos")
	}
	if *cacheDir == "" {
		*cacheDir = filepath.Join(sparkHome, "cache")
	}
	if *environments == "" {
		*environments = "environments"
	}
	if *mode != "all" && *mode != "control" && *mode != "worker" {
		return fmt.Errorf("unsupported mode %q (phase3 supports all)", *mode)
	}
	_ = *mode

	store, err := meta.New(*stateDir)
	if err != nil {
		return fmt.Errorf("meta: %w", err)
	}

	src, err := git.New(*repos)
	if err != nil {
		return fmt.Errorf("git source: %w", err)
	}

	envAbs := *environments
	if !filepath.IsAbs(envAbs) {
		if wd, err := os.Getwd(); err == nil {
			envAbs = filepath.Join(wd, envAbs)
		}
	}
	envLoader := envtemplate.New(envAbs)
	cacheLayout := cache.NewLayout(*cacheDir)
	if err := cacheLayout.Ensure(); err != nil {
		return fmt.Errorf("cache: %w", err)
	}
	log.Printf("sparkd: environments=%s repos=%s cache=%s", envAbs, *repos, *cacheDir)

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

	ctx, cancel := signal.NotifyContext(context.Background(), syscall.SIGINT, syscall.SIGTERM)
	defer cancel()

	rp := reaper.New(store, sched, srv.DestroySpark, 15*time.Second)
	go rp.Run(ctx)

	errCh := make(chan error, 1)
	go func() {
		errCh <- srv.ListenAndServe(*socket)
	}()

	select {
	case <-ctx.Done():
		_ = os.Remove(*socket)
		log.Println("sparkd shutting down")
		return nil
	case err := <-errCh:
		return err
	}
}
