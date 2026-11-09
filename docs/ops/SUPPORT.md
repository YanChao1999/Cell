# Support matrix (internal production — P1)

Cell is an execution boundary for untrusted agent work. This document states what operators can rely on today. It is **not** an SLA.

## Supported

| Area | What works |
|------|------------|
| Topology | Single-node `mode=all`; 1-control + N-worker compose (`docker-compose.yml`) |
| Runtime | Docker sandbox (default when Docker is available); explicit `CELL_RUNTIME=host` for CI/dev only |
| Auth | Shared `CELL_WORKER_TOKEN` bootstrap; short-lived HMAC tokens via `POST /v1/auth/token`; optional `CELL_WORKER_TOKEN_PREV` rotation |
| Network policy | `default` / `none` / `allowlist` (allowlist via `cell-egress` sidecar) |
| Observability | `GET /metrics`, `GET /metrics/ui`; optional Prometheus + Grafana (`docker-compose.obs.yml`, GHCR mirrors) |
| Persistence | Control meta + worker registry on disk under `--cell-home` / `--state-dir` |
| Platforms | Linux hosts (compose uses `network_mode: host`) |

## Not supported (yet)

| Area | Status |
|------|--------|
| MicroVM isolation | Fail-closed stub; Firecracker/CH/QEMU mainline under construction |
| mTLS control↔worker | Planned; HMAC tokens are the current trust boundary |
| Multi-region / HA control | Single control process; restart recovers registry from disk |
| Windows / macOS workers | Not validated |
| Guaranteed disk quotas | Host `du` enforcement; Docker `storage-opt` opt-in only |
| Formal SLA / uptime targets | Out of scope for P1 |

## Operator expectations

- Treat Cells as **hostile**: do not mount host secrets into workspaces.
- Keep `CELL_WORKER_TOKEN` long and rotate with `CELL_WORKER_TOKEN_PREV` grace.
- Prefer Docker over host runtime outside smoke tests.
- Pin image tags for workers/control; rebuild after upgrades (see [UPGRADE.md](UPGRADE.md)).
