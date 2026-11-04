# Spark

Ephemeral isolated environments for AI agents.

**Phase 3:** durable package caches (gomod / pip / zephyr / ccache), TTL reaper with default 30m Spark TTL, and one-shot `POST /v1/run`.

## Architecture

- **Control plane** — HTTP Agent API, scheduler, meta store, TTL reaper
- **Workers** — create / exec / destroy Sparks; clients may also be workers
- **Source** — `git clone --mirror` under `~/.spark/repos/<hash>`, then `git worktree add` into the workspace
- **Runtime** — Docker (`docker run -d` + `docker exec` + `docker rm -f`) or host fallback
- **Caches** — host dirs under `--cache-dir` (default `~/.spark/cache`); Docker bind-mounts them via env template `cache_mounts`; host fallback sets `GOMODCACHE` / `GOCACHE` / `PIP_CACHE_DIR` / `CCACHE_DIR` / …
- **Environments** — YAML under `environments/` (image, limits, `cache_mounts`, optional `default_exec_timeout`)
- **Placement** — `prefer_local` → warm `cache_hints` → free capacity
- **Sticky Exec** — meta maps `spark_id → worker_id`
- **Disposable Sparks**, durable caches / mirrors

## Build

```bash
export PATH=$HOME/.local/bin:$HOME/.local/go/bin:/usr/local/go/bin:$PATH
go build -o bin/sparkd ./cmd/sparkd
go build -o bin/spark ./cmd/spark
go test ./internal/worker/cache/ ./internal/worker/envtemplate/
```

## Run (`mode=all`)

```bash
./bin/sparkd serve --mode=all \
  --spark-home ~/.spark \
  --environments ./environments \
  --cache-dir ~/.spark/cache \
  --socket ~/.spark/sparkd.sock
```

| Flag | Default |
|------|---------|
| `--spark-home` | `~/.spark` |
| `--repos` | `<spark-home>/repos` |
| `--workspaces` | `<spark-home>/workspaces` |
| `--cache-dir` | `<spark-home>/cache` |
| `--state-dir` | `<spark-home>/state` |
| `--environments` | `./environments` |
| `--socket` | `<spark-home>/sparkd.sock` |

Without a Docker daemon, sparkd logs a host-fallback line and create/exec/destroy still work; cache env vars point tools at `--cache-dir` subdirs.

## Caches (Phase 3)

On start, sparkd ensures well-known dirs under `--cache-dir`:

`go-mod`, `go-build`, `pip-cache`, `python-venv`, `zephyr-modules`, `west-cache`, `yocto-downloads`, `yocto-sstate`, `ccache`

Env templates declare mounts, e.g. `go-1.23.yaml`:

```yaml
cache_mounts:
  - host: go-mod
    container: /go/pkg/mod
  - host: go-build
    container: /root/.cache/go-build
```

- **Docker:** `-v <cache-dir>/<host>:<container>` on `docker run`
- **Host fallback:** `GOMODCACHE` / `GOCACHE` (and peers) point at those host dirs during `exec`

Warm path: first Spark populates e.g. `~/.spark/cache/go-mod`; a later Spark with the same environment reuses the same bind/env, so `go build` / `pip install` skip re-downloads.

## TTL reaper

Default Spark TTL is **30m** (override with create `ttl`, e.g. `"1h"`). The reaper ticks on start and every 15s, destroying expired Sparks on their sticky worker and clearing meta.

Embedded templates set a longer optional `default_exec_timeout` (Zephyr `2h`, embedded-linux `4h`) used when `exec` omits `timeout_seconds`.

## Agent HTTP API (Unix socket)

| Method | Path | Notes |
|--------|------|--------|
| GET | `/health` | liveness |
| POST | `/v1/sparks` | create (`repo`, `ref`, `environment`, `ttl`, `prefer_local`, `limits`, `client_worker_id`) |
| GET | `/v1/sparks` | list |
| GET | `/v1/sparks/{id}` | get |
| POST | `/v1/sparks/{id}/exec` | `{ "command", "timeout_seconds" }` → `exit_code`, `stdout`, `stderr` |
| DELETE | `/v1/sparks/{id}` | destroy |
| POST | `/v1/run` | create → exec commands sequentially → destroy |

```bash
curl --unix-socket ~/.spark/sparkd.sock http://localhost/health
curl --unix-socket ~/.spark/sparkd.sock -X POST http://localhost/v1/sparks \
  -H 'Content-Type: application/json' \
  -d '{"environment":"go-1.23","ttl":"30m"}'
curl --unix-socket ~/.spark/sparkd.sock -X POST http://localhost/v1/run \
  -H 'Content-Type: application/json' \
  -d '{"environment":"go-1.23","commands":["go version"]}'
```

## Python SDK (primary agent client)

```bash
uv venv .venv && uv pip install --python .venv/bin/python -e sdk/python
# or: pip install -e sdk/python
```

```python
async with Client() as client:
    spark = await client.create(environment="go-1.23", ttl="30m")
    result = await spark.exec("go test ./...")
    await spark.destroy()
    # or one-shot:
    await client.run(environment="go-1.23", commands=["go version"])
```

`repo` is optional (empty = blank workspace). See `sdk/python/README.md`.

## Ops CLI

```bash
./bin/spark health
./bin/spark list
./bin/spark run --env go-1.23 'echo hello'
./bin/spark destroy <id>
```

## Smoke

```bash
./scripts/smoke.sh
```

## Module

`github.com/YanChao1999/spark`
