// Command celld is the Cell control plane / local worker daemon.
package main

import (
	"bytes"
	"context"
	"encoding/json"
	"flag"
	"fmt"
	"log"
	"net/http"
	"os"
	"os/signal"
	"path/filepath"
	"strings"
	"sync"
	"syscall"
	"time"

	ctrlapi "github.com/YanChao1999/Cell/internal/control/api"
	"github.com/YanChao1999/Cell/internal/control/meta"
	"github.com/YanChao1999/Cell/internal/control/reaper"
	"github.com/YanChao1999/Cell/internal/control/registry"
	"github.com/YanChao1999/Cell/internal/control/scheduler"
	"github.com/YanChao1999/Cell/internal/worker/auth"
	"github.com/YanChao1999/Cell/internal/worker/cache"
	"github.com/YanChao1999/Cell/internal/worker/envtemplate"
	"github.com/YanChao1999/Cell/internal/worker/httpserver"
	"github.com/YanChao1999/Cell/internal/worker/manager"
	"github.com/YanChao1999/Cell/internal/worker/peers"
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
		fmt.Println("celld 0.6.0-p0")
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
  celld serve --mode=control --listen-http :8080
  celld serve --mode=worker --worker-id=w1 --listen-http :8080 --control-url http://control:8080

Flags (serve):
  --mode            all | control | worker
  --socket          unix socket (default <cell-home>/celld.sock); used in mode=all
  --listen-http     TCP listen addr for control and/or worker RPC (e.g. :8080)
  --control-url     control plane base URL for mode=worker registration
  --cell-home       base dir (default ~/.cell); sets defaults for other paths
  --state-dir       meta state dir (default <cell-home>/state)
  --workspaces      workspace root (default <cell-home>/workspaces)
  --repos           bare git mirrors (default <cell-home>/repos)
  --cache-dir       package caches (default <cell-home>/cache)
  --environments    env template YAML dir (default ./environments)
  --worker-id       worker id (default local)
  --worker-token    long-lived HMAC/bootstrap secret (or CELL_WORKER_TOKEN)
                    short-lived tokens issued via POST /v1/auth/token; rotation via CELL_WORKER_TOKEN_PREV

Commands:
  serve     Start the daemon
  version   Print version
`)
}

func runServe(args []string) error {
	fs := flag.NewFlagSet("serve", flag.ExitOnError)
	mode := fs.String("mode", "all", "mode: all | control | worker")
	socket := fs.String("socket", "", "unix socket path (default <cell-home>/celld.sock)")
	listenHTTP := fs.String("listen-http", "", "TCP listen address (required for control/worker cluster)")
	controlURL := fs.String("control-url", "", "control plane URL for worker registration")
	cellHomeFlag := fs.String("cell-home", "", "cell home directory (default ~/.cell)")
	stateDir := fs.String("state-dir", "", "meta state dir (default <cell-home>/state)")
	workspaces := fs.String("workspaces", "", "workspace root (default <cell-home>/workspaces)")
	repos := fs.String("repos", "", "bare git mirror root (default <cell-home>/repos)")
	cacheDir := fs.String("cache-dir", "", "cache root (default <cell-home>/cache)")
	environments := fs.String("environments", "", "environment templates dir (default ./environments)")
	workerID := fs.String("worker-id", "local", "worker id")
	workerToken := fs.String("worker-token", "", "shared Bearer token (default CELL_WORKER_TOKEN)")
	if err := fs.Parse(args); err != nil {
		return err
	}
	token := strings.TrimSpace(*workerToken)
	if token == "" {
		token = auth.FromEnv()
	}
	tokenPrev := auth.PrevFromEnv()
	issuer := auth.NewIssuer(token, tokenPrev)

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
	switch *mode {
	case "all", "control", "worker":
	default:
		return fmt.Errorf("unsupported mode %q (want all|control|worker)", *mode)
	}
	if *mode != "all" && strings.TrimSpace(*listenHTTP) == "" {
		return fmt.Errorf("mode %q requires --listen-http", *mode)
	}
	if *mode == "worker" && strings.TrimSpace(*controlURL) == "" {
		return fmt.Errorf("mode=worker requires --control-url")
	}

	for _, p := range []*string{socket, stateDir, workspaces, repos, cacheDir} {
		abs, err := filepath.Abs(*p)
		if err != nil {
			return err
		}
		*p = abs
	}

	ctx, cancel := signal.NotifyContext(context.Background(), syscall.SIGINT, syscall.SIGTERM)
	defer cancel()

	switch *mode {
	case "control":
		return runControl(ctx, *listenHTTP, *stateDir, *workerID, issuer)
	case "worker":
		return runWorker(ctx, workerOpts{
			id:           *workerID,
			listenHTTP:   *listenHTTP,
			controlURL:   strings.TrimRight(*controlURL, "/"),
			workspaces:   *workspaces,
			repos:        *repos,
			cacheDir:     *cacheDir,
			environments: *environments,
			issuer:       issuer,
		})
	default:
		return runAll(ctx, *socket, *listenHTTP, *stateDir, *workspaces, *repos, *cacheDir, *environments, *workerID, issuer)
	}
}

func runControl(ctx context.Context, listenHTTP, stateDir, localID string, issuer *auth.Issuer) error {
	store, err := meta.New(stateDir)
	if err != nil {
		return fmt.Errorf("meta: %w", err)
	}
	// Control mode keeps durable meta across restarts; workers re-attach via register.
	sched := scheduler.New(nil, localID)
	reg := registry.NewPersistent(stateDir, 45*time.Second)
	srv := ctrlapi.NewWithOptions(store, sched, reg, ctrlapi.Options{Issuer: issuer})
	// Re-hydrate scheduler remotes from persisted registry.
	for _, info := range reg.List() {
		cp := info
		if err := srv.SyncRemoteWorker(&cp); err != nil {
			log.Printf("celld: restore worker %s: %v", info.ID, err)
		}
	}
	rp := reaper.New(store, sched, srv.DestroyCell, 15*time.Second)
	go rp.Run(ctx)

	errCh := make(chan error, 1)
	go func() { errCh <- srv.ListenAndServeHTTP(listenHTTP) }()
	authState := "off"
	if issuer != nil && issuer.Enabled() {
		authState = "on"
	}
	log.Printf("celld: mode=control listen-http=%s worker-auth=%s meta=%s", listenHTTP, authState, stateDir)
	return waitServe(ctx, errCh, "")
}

type workerOpts struct {
	id, listenHTTP, controlURL                string
	workspaces, repos, cacheDir, environments string
	issuer                                    *auth.Issuer
}

func runWorker(ctx context.Context, o workerOpts) error {
	secret := ""
	if o.issuer != nil {
		secret = o.issuer.Secret()
	}
	lw, err := newLocalWorker(o.id, o.workspaces, o.repos, o.cacheDir, o.environments, o.controlURL, secret)
	if err != nil {
		return err
	}
	if err := docker.PruneLabeledContainers(ctx); err != nil {
		log.Printf("celld: prune orphan containers: %v", err)
	}

	wSrv := httpserver.New(lw, httpserver.Options{Issuer: o.issuer})
	publicEP := advertiseHTTP(o.listenHTTP)
	cred := newCredential(o.issuer)
	go refreshCredential(ctx, o.controlURL, cred, 5*time.Minute)
	go heartbeatToControl(ctx, o.controlURL, o.id, publicEP, lw, cred, 10*time.Second)
	if err := registerWithControl(ctx, o.controlURL, o.id, publicEP, lw, cred); err != nil {
		log.Printf("celld: initial register: %v (will retry via heartbeat path after re-register)", err)
		go reRegisterLoop(ctx, o.controlURL, o.id, publicEP, lw, cred)
	}

	errCh := make(chan error, 1)
	go func() { errCh <- wSrv.ListenAndServe(o.listenHTTP) }()
	authState := "off"
	if o.issuer != nil && o.issuer.Enabled() {
		authState = "on"
	}
	log.Printf("celld: mode=worker id=%s listen-http=%s control=%s worker-auth=%s", o.id, o.listenHTTP, o.controlURL, authState)
	return waitServe(ctx, errCh, "")
}

func runAll(ctx context.Context, socket, listenHTTP, stateDir, workspaces, repos, cacheDir, environments, workerID string, issuer *auth.Issuer) error {
	store, err := meta.New(stateDir)
	if err != nil {
		return fmt.Errorf("meta: %w", err)
	}
	if err := docker.PruneLabeledContainers(ctx); err != nil {
		log.Printf("celld: prune orphan containers: %v", err)
	}
	for _, rec := range store.List() {
		_ = os.RemoveAll(filepath.Join(workspaces, rec.ID))
	}
	if err := store.Reset(); err != nil {
		return fmt.Errorf("meta reset on startup: %w", err)
	}

	secret := ""
	if issuer != nil {
		secret = issuer.Secret()
	}
	lw, err := newLocalWorker(workerID, workspaces, repos, cacheDir, environments, "", secret)
	if err != nil {
		return err
	}
	sched := scheduler.New([]workerapi.Worker{lw}, workerID)
	reg := registry.NewPersistent(stateDir, 45*time.Second)
	srv := ctrlapi.NewWithOptions(store, sched, reg, ctrlapi.Options{Issuer: issuer})

	if reg := srv.Registry(); reg != nil {
		ep := "unix://" + socket
		if listenHTTP != "" {
			ep = advertiseHTTP(listenHTTP)
		}
		cap := lw.Capacity()
		_, _ = reg.Register(registry.RegisterRequest{
			ID: lw.ID(), Endpoint: ep, MaxCells: cap.MaxCells, UsedCells: cap.UsedCells, CacheKeys: lw.CacheIndex().Keys(),
		})
		go heartbeatLocalWorker(ctx, reg, lw, 15*time.Second)
	}

	rp := reaper.New(store, sched, srv.DestroyCell, 15*time.Second)
	go rp.Run(ctx)

	errCh := make(chan error, 2)
	go func() { errCh <- srv.ListenAndServe(socket) }()
	if listenHTTP != "" {
		go func() { errCh <- srv.ListenAndServeHTTP(listenHTTP) }()
	}
	log.Printf("celld: mode=all socket=%s listen-http=%q", socket, listenHTTP)
	return waitServe(ctx, errCh, socket)
}

func newLocalWorker(id, workspaces, repos, cacheDir, environments, controlURL, token string) (*manager.LocalWorker, error) {
	src, err := git.New(repos)
	if err != nil {
		return nil, fmt.Errorf("git source: %w", err)
	}
	envAbs, err := filepath.Abs(environments)
	if err != nil {
		return nil, fmt.Errorf("environments: %w", err)
	}
	cacheLayout := cache.NewLayout(cacheDir)
	if err := cacheLayout.Ensure(); err != nil {
		return nil, fmt.Errorf("cache: %w", err)
	}
	log.Printf("celld: environments=%s repos=%s cache=%s", envAbs, repos, cacheDir)
	var peerFinder manager.PeerFinder
	if strings.TrimSpace(controlURL) != "" {
		peerFinder = &peers.Finder{ControlURL: controlURL, Token: token}
	}
	return manager.New(manager.Options{
		ID:             id,
		WorkspacesRoot: workspaces,
		ReposRoot:      repos,
		CacheDir:       cacheDir,
		CacheLayout:    cacheLayout,
		Runtime:        docker.New(),
		Source:         src,
		EnvLoader:      envtemplate.New(envAbs),
		Peers:          peerFinder,
		WorkerToken:    token,
	})
}

func waitServe(ctx context.Context, errCh <-chan error, socket string) error {
	select {
	case <-ctx.Done():
		if socket != "" {
			_ = os.Remove(socket)
		}
		log.Println("celld shutting down")
		return nil
	case err := <-errCh:
		return err
	}
}

func advertiseHTTP(listen string) string {
	// Compose services use the service hostname; listen may be ":8080".
	host := os.Getenv("CELL_ADVERTISE_HOST")
	port := listen
	if i := strings.LastIndex(listen, ":"); i >= 0 {
		port = listen[i+1:]
	}
	if host == "" {
		host = "127.0.0.1"
	}
	return "http://" + host + ":" + port
}

type credential struct {
	mu    sync.Mutex
	token string
	iss   *auth.Issuer
}

func newCredential(iss *auth.Issuer) *credential {
	c := &credential{iss: iss}
	if iss != nil {
		c.token = iss.Secret()
		if tok, _, err := iss.Issue(auth.ScopeWorker, auth.DefaultTTL); err == nil {
			c.token = tok
		}
	}
	return c
}

func (c *credential) get() string {
	c.mu.Lock()
	defer c.mu.Unlock()
	return c.token
}

func (c *credential) set(tok string) {
	c.mu.Lock()
	defer c.mu.Unlock()
	c.token = tok
}

func refreshCredential(ctx context.Context, controlURL string, c *credential, every time.Duration) {
	if c == nil || c.iss == nil || !c.iss.Enabled() {
		return
	}
	t := time.NewTicker(every)
	defer t.Stop()
	for {
		select {
		case <-ctx.Done():
			return
		case <-t.C:
			tok, err := fetchShortToken(ctx, controlURL, c.get())
			if err != nil {
				// Fall back to locally minted token from the shared secret.
				if local, _, ierr := c.iss.Issue(auth.ScopeWorker, auth.DefaultTTL); ierr == nil {
					c.set(local)
				}
				continue
			}
			c.set(tok)
		}
	}
}

func fetchShortToken(ctx context.Context, controlURL, bearer string) (string, error) {
	body := map[string]string{"scope": auth.ScopeWorker, "ttl": "15m"}
	b, _ := json.Marshal(body)
	req, err := http.NewRequestWithContext(ctx, http.MethodPost, controlURL+"/v1/auth/token", bytes.NewReader(b))
	if err != nil {
		return "", err
	}
	req.Header.Set("Content-Type", "application/json")
	auth.SetBearer(req.Header, bearer)
	resp, err := http.DefaultClient.Do(req)
	if err != nil {
		return "", err
	}
	defer func() { _ = resp.Body.Close() }()
	if resp.StatusCode >= 300 {
		return "", fmt.Errorf("token: %s", resp.Status)
	}
	var out struct {
		Token string `json:"token"`
	}
	if err := json.NewDecoder(resp.Body).Decode(&out); err != nil {
		return "", err
	}
	if out.Token == "" {
		return "", fmt.Errorf("token: empty")
	}
	return out.Token, nil
}

func registerWithControl(ctx context.Context, controlURL, id, endpoint string, w workerapi.Worker, cred *credential) error {
	cap := w.Capacity()
	body := registry.RegisterRequest{
		ID: id, Endpoint: endpoint, MaxCells: cap.MaxCells, UsedCells: cap.UsedCells, CacheKeys: cacheKeys(w),
	}
	return postJSON(ctx, controlURL+"/v1/workers/register", body, cred.get())
}

func heartbeatToControl(ctx context.Context, controlURL, id, endpoint string, w workerapi.Worker, cred *credential, every time.Duration) {
	t := time.NewTicker(every)
	defer t.Stop()
	for {
		select {
		case <-ctx.Done():
			return
		case <-t.C:
			cap := w.Capacity()
			body := registry.HeartbeatRequest{UsedCells: cap.UsedCells, MaxCells: cap.MaxCells, CacheKeys: cacheKeys(w)}
			if err := postJSON(ctx, controlURL+"/v1/workers/"+id+"/heartbeat", body, cred.get()); err != nil {
				log.Printf("celld: heartbeat: %v — re-registering", err)
				_ = registerWithControl(ctx, controlURL, id, endpoint, w, cred)
			}
		}
	}
}

func reRegisterLoop(ctx context.Context, controlURL, id, endpoint string, w workerapi.Worker, cred *credential) {
	t := time.NewTicker(3 * time.Second)
	defer t.Stop()
	for {
		select {
		case <-ctx.Done():
			return
		case <-t.C:
			if err := registerWithControl(ctx, controlURL, id, endpoint, w, cred); err == nil {
				log.Printf("celld: registered with control as %s → %s", id, endpoint)
				return
			}
		}
	}
}

func postJSON(ctx context.Context, url string, body any, token string) error {
	b, err := json.Marshal(body)
	if err != nil {
		return err
	}
	req, err := http.NewRequestWithContext(ctx, http.MethodPost, url, bytes.NewReader(b))
	if err != nil {
		return err
	}
	req.Header.Set("Content-Type", "application/json")
	auth.SetBearer(req.Header, token)
	resp, err := http.DefaultClient.Do(req)
	if err != nil {
		return err
	}
	defer func() { _ = resp.Body.Close() }()
	if resp.StatusCode >= 300 {
		return fmt.Errorf("%s: %s", url, resp.Status)
	}
	return nil
}

func cacheKeys(w workerapi.Worker) []string {
	if lw, ok := w.(*manager.LocalWorker); ok {
		return lw.WarmCacheKeys()
	}
	if idx := w.CacheIndex(); idx != nil {
		return idx.Keys()
	}
	return nil
}

func heartbeatLocalWorker(ctx context.Context, reg *registry.Registry, w workerapi.Worker, every time.Duration) {
	t := time.NewTicker(every)
	defer t.Stop()
	for {
		select {
		case <-ctx.Done():
			return
		case <-t.C:
			cap := w.Capacity()
			if _, err := reg.Heartbeat(w.ID(), registry.HeartbeatRequest{
				UsedCells: cap.UsedCells, MaxCells: cap.MaxCells, CacheKeys: cacheKeys(w),
			}); err != nil {
				log.Printf("celld: worker heartbeat: %v", err)
			}
		}
	}
}
