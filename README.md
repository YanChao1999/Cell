# Cell

Ephemeral isolated environments for AI coding agents.

> Formerly sketched as “Spark”; renamed to **Cell** to avoid collision with Apache Spark.

A **Cell** is a disposable execution workspace: checkout code, install deps, build/test, return outputs, then destroy — while reusable caches survive.

## Architecture

- **Control plane** — HTTP Agent API, scheduler, meta store, TTL reaper
- **Workers** — create / exec / destroy Cells; clients may also be workers
- **Source** — `git clone --mirror` under `~/.cell/repos/<hash>`, then `git worktree add` into the workspace
- **Runtime** — Docker (`docker run -d` + `docker exec` + `docker rm -f`) or host fallback
- **Caches** — host dirs under `--cache-dir` (default `~/.cell/cache`); Docker bind-mounts via env template `cache_mounts`; host fallback sets `GOMODCACHE` / `GOCACHE` / `PIP_CACHE_DIR` / `CCACHE_DIR` / …
- **Environments** — YAML under `environments/` (image, limits, `cache_mounts`, optional `default_exec_timeout`, egress `network`: `default` \| `none` \| `allowlist`). Allowlist Cells use `cell-egress`; set `CELL_EGRESS_UPSTREAM` (or host `HTTPS_PROXY`) so the sidecar can reach the internet through a corporate proxy.
- **Placement** — `prefer_local` → warm `cache_hints` → free capacity
- **Sticky Exec** — meta maps `cell_id → worker_id`
- **Disposable Cells**, durable caches / mirrors

Module and repository: `github.com/YanChao1999/Cell`.

Component, class, and sequence diagrams are in [ARCHITECTURE.md](ARCHITECTURE.md).

## Build

```bash
export PATH=$HOME/.local/bin:$HOME/.local/go/bin:/usr/local/go/bin:$PATH
go build -o bin/celld ./cmd/celld
go build -o bin/cell ./cmd/cell
# Required for network: allowlist (Docker sidecar proxy)
CGO_ENABLED=0 go build -o bin/cell-egress ./cmd/cell-egress
# Optional dedicated sidecar image (recommended for allowlist on arbitrary Cell images)
# ./scripts/build-egress-image.sh   # tags CELL_EGRESS_IMAGE or cell-egress:local
go test ./internal/worker/cache/ ./internal/worker/auth/ ./internal/worker/envtemplate/ ./internal/worker/egress/ ./internal/worker/runtime/docker/ ./internal/worker/remote/ ./internal/control/registry/
```

## Run (`mode=all`)

```bash
./bin/celld serve --mode=all \
  --cell-home ~/.cell \
  --environments ./environments \
  --cache-dir ~/.cell/cache \
  --socket ~/.cell/celld.sock
```

| Flag | Default |
|------|---------|
| `--cell-home` | `~/.cell` |
| `--repos` | `<cell-home>/repos` |
| `--workspaces` | `<cell-home>/workspaces` |
| `--cache-dir` | `<cell-home>/cache` |
| `--state-dir` | `<cell-home>/state` |
| `--environments` | `./environments` |
| `--socket` | `<cell-home>/celld.sock` |

Without a Docker daemon, celld logs a host-fallback line and create/exec/destroy still work; cache env vars point tools at `--cache-dir` subdirs.

## Caches

On start, celld ensures well-known dirs under `--cache-dir`:

`go-mod`, `go-build`, `pip-cache`, `python-venv`, `zephyr-modules`, `west-cache`, `yocto-downloads`, `yocto-sstate`, `ccache`

Env templates declare mounts, e.g. `go-1.23.yaml`:

```yaml
cache_mounts:
  - host: go-mod
    container: /go/pkg/mod
  - host: go-build
    container: /root/.cache/go-build
```

Warm path: first Cell populates e.g. `~/.cell/cache/go-mod`; a later Cell with the same environment reuses the same bind/env.

## TTL reaper

Default Cell TTL is **30m** (override with create `ttl`, e.g. `"1h"`). The reaper ticks on start and every 15s, destroying expired Cells on their sticky worker and clearing meta.

Embedded templates set a longer optional `default_exec_timeout` (Zephyr `2h`, embedded-linux `4h`) used when `exec` omits `timeout_seconds`.

## Agent HTTP API (Unix socket)

| Method | Path | Notes |
|--------|------|--------|
| GET | `/health` | liveness |
| POST | `/v1/cells` | create (`repo`, `ref`, `environment`, `ttl`, `prefer_local`, `limits`, `client_worker_id`) |
| GET | `/v1/cells` | list |
| GET | `/v1/cells/{id}` | get |
| POST | `/v1/cells/{id}/exec` | `{ "command", "timeout_seconds" }` → `exit_code`, `stdout`, `stderr` |
| DELETE | `/v1/cells/{id}` | destroy |
| POST | `/v1/run` | create → exec commands sequentially → destroy |

```bash
curl --unix-socket ~/.cell/celld.sock http://localhost/health
curl --unix-socket ~/.cell/celld.sock -X POST http://localhost/v1/cells \
  -H 'Content-Type: application/json' \
  -d '{"environment":"go-1.23","ttl":"30m"}'
curl --unix-socket ~/.cell/celld.sock -X POST http://localhost/v1/run \
  -H 'Content-Type: application/json' \
  -d '{"environment":"go-1.23","commands":["go version"]}'
```

## Python SDK (primary agent client)

```bash
uv venv .venv && uv pip install --python .venv/bin/python -e sdk/python
```

```python
from cell import Client

async with Client() as client:
    c = await client.create(environment="go-1.23", ttl="30m")
    result = await c.exec("go test ./...")
    await c.destroy()
    await client.run(environment="go-1.23", commands=["go version"])
```

`repo` is optional (empty = blank workspace). Endpoint: `CELL_ENDPOINT` or default `unix://~/.cell/celld.sock`. See `sdk/python/README.md`.

## Ops CLI

```bash
./bin/cell health
./bin/cell list
./bin/cell run --env go-1.23 'echo hello'
./bin/cell destroy <id>
```

Socket override: `CELL_SOCKET`.

## Smoke

```bash
./scripts/smoke.sh
```

## CI

GitHub Actions (`.github/workflows/ci.yml`) on push/PR to `main`:

| Job | Checks |
| --- | --- |
| Go | `go build` + `golangci-lint` (`gofmt`, `govet`, `staticcheck`, `dupl`, …) |
| API smoke | `scripts/smoke.sh` (host fallback; no Docker required) |
| Python | `compileall` on `sdk/python` |

Config: `.golangci.yml`.

## Module

`github.com/YanChao1999/Cell`

## Tech debt

### Mini cluster (Compose)

```bash
docker compose up --build -d
./scripts/cluster-smoke.sh
docker compose down -v
```

Control listens on `localhost:18080`; workers register over HTTP and receive Create/Exec via worker RPC (`CELL_RUNTIME=host` in compose). Set `CELL_WORKER_TOKEN` (compose uses `cell-dev-token`) so register/heartbeat and `/worker/v1/*` require Bearer auth. Cold cache keys are pulled from warm peers on create (`GET /worker/v1/caches/{key}`).

`GET /metrics` exposes Prometheus counters for create/exec/destroy. Workers may mint short-lived tokens via `POST /v1/auth/token` (HMAC over `CELL_WORKER_TOKEN`; optional `CELL_WORKER_TOKEN_PREV` for rotation). Control `--mode=control` keeps meta + worker registry on disk across restarts.

### Metrics UI

Built-in (no extra images): open **http://127.0.0.1:18080/metrics/ui** while control is running.

Optional Prometheus + Grafana (images mirrored to **ghcr.io** via `Mirror obs images` workflow; override with `CELL_GRAFANA_IMAGE` / `CELL_PROM_IMAGE` if needed):

```bash
docker compose up --build -d
docker compose -f docker-compose.yml -f docker-compose.obs.yml up -d
# Grafana http://127.0.0.1:13000  (admin / cell)
# Prometheus http://127.0.0.1:19090
```

### Ops & security (P1)

- Support matrix: [docs/ops/SUPPORT.md](docs/ops/SUPPORT.md)
- Restart / rolling: [docs/ops/PLAYBOOK.md](docs/ops/PLAYBOOK.md)
- Upgrades: [docs/ops/UPGRADE.md](docs/ops/UPGRADE.md)
- Threat model: [docs/security/THREAT_MODEL.md](docs/security/THREAT_MODEL.md)
- Docker escape checklist: [docs/security/ESCAPE_CHECKLIST.md](docs/security/ESCAPE_CHECKLIST.md)

`CELL_RUNTIME=microvm` selects the fail-closed microVM mainline (M0); real VMM Backend is tracked in [TECH_DEBT.md](TECH_DEBT.md) and `internal/worker/runtime/microvm/DESIGN.md`. Remaining gaps (mTLS, Firecracker M1+) are in [TECH_DEBT.md](TECH_DEBT.md).
