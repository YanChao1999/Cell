# Tech debt

Follow-ups from security hardening. Do not drop remaining items when starting deeper Phase 5 work.

## Principle: security in design, in software

Cell is an execution boundary for untrusted agent work. Security is not a later “hardening pass” bolted onto a permissive default.

- **Fail closed.** Unknown network modes, unfinished features, missing images under Docker, and missing runtime entries must reject — never silently weaken to host or open egress.
- **Enforce in the runtime path.** Policy lives in templates and API; the daemon implements it.
- **Least privilege by construction.** No privileged shortcuts for convenience.
- **Prefer stronger isolation over more features** when they conflict.

## Fixed / landed in tree

### PR #2 — host escape & lifecycle
- No host fallback when Docker is on; `none`/`default`; allowlist fail-closed until proxy existed; destroy/startup safety.

### PR #3 — Docker sandbox + warm seed + allowlist proxy
- Non-root, `no-new-privileges`, cap-drop ALL + minimal, read-only rootfs + tmpfs.
- Per-Cell caches; **trusted seed** via `cache.SeedCopy`.
- Git repos RO same-path bind.
- **`network: allowlist`**: `cell-egress` sidecar, audit, optional upstream chain.

### PR #4 — Single-node UX
- Template **`setup`** on create; CLI/SDK long-op timeouts; smoke temp `--cell-home`; git mirror flock; CI egress/worker tests.

### Setup / egress / hardening / Phase 5 foundation (this branch)
- Richer setup: `setup_timeout`, conditional steps (`if` / `unless`).
- Dedicated egress image (`docker/cell-egress`, `CELL_EGRESS_IMAGE`, `./scripts/build-egress-image.sh`); bind-mount fallback if image missing.
- Portable disk: host workspace `du` enforcement; storage-opt still opt-in via `CELL_DOCKER_DISK_STORAGE_OPT=1`.
- Userns: `--cgroupns=private`; do not force `--userns=host` (escape hatch `CELL_DOCKER_USERNS=host`).
- Phase 5 foundation: in-memory worker **registry** + register/heartbeat/list API; local worker self-registers and heartbeats.

## Still open

### MicroVM backend (real VMM)
- Firecracker / Cloud Hypervisor / QEMU behind `Runtime`. See `microvm/DESIGN.md`.

### Hardening polish
- Seed size/latency tuning; optional content hashing of seeded files.
- Stronger userns than daemon remap + `--user` (rootless/podman paths).
- Harder disk caps (project quotas) where operators can enable them.

### Phase 5 (remaining)
- Cross-platform mirror lock if Windows workers appear.
- Compose CI job (optional; local `docker compose` + `scripts/cluster-smoke.sh` for now).
- mTLS between control and workers (beyond HMAC short-lived tokens).

### P0 landed (auth / metrics / persist)
- Short-lived HMAC worker tokens (`POST /v1/auth/token`) signed by `CELL_WORKER_TOKEN`; `CELL_WORKER_TOKEN_PREV` rotation grace; static secret still valid as bootstrap.
- Prometheus text metrics at `GET /metrics` (create/exec/destroy totals + duration sums).
- Durable control meta kept across `--mode=control` restarts; registry persisted to `state/workers.json`.

### Phase 5 landed (cluster compose + cache/auth)
- `celld --mode=control|worker|all`, `--listen-http`, `--control-url`.
- Worker HTTP RPC (`/worker/v1/...`) + remote client; registry sync into scheduler.
- `docker-compose.yml` (control + worker-a + worker-b) and `scripts/cluster-smoke.sh`.
- Shared Bearer auth: `CELL_WORKER_TOKEN` / `--worker-token` on worker RPC + register/heartbeat.
- Cross-node cache pull: `GET /worker/v1/caches/{key}` + create-time pull of cold keys from warm peers.
