# Support matrix (internal production)

Cell is an execution boundary for untrusted agent work. This document states what operators can rely on today. It is **not** an SLA.

## Supported

| Area | What works |
|------|------------|
| Topology | Single-node `mode=all`; 1-control + N-worker compose (`docker-compose.yml`) |
| Runtime | Docker sandbox (default when Docker is available); `CELL_RUNTIME=host` for CI/dev only; `CELL_RUNTIME=microvm` after `./scripts/setup-microvm.sh` |
| MicroVM | Firecracker + jailer + ultra-tiny **read-only** rootfs (~11MiB); guest tmpfs for `/workspace` `/tmp` `/etc`; network `none` or `allowlist` (TAP + host egress + DNS) |
| Auth | Shared `CELL_WORKER_TOKEN` bootstrap; short-lived HMAC tokens via `POST /v1/auth/token`; optional `CELL_WORKER_TOKEN_PREV` rotation |
| Network policy | `default` / `none` / `allowlist` (Docker sidecar or microVM host proxy) |
| Snapshots | `POST /v1/cells/{id}/snapshot`; create with `snapshot_id` (hex id; wipe-before-unpack) |
| Observability | `GET /metrics`, `GET /metrics/ui`; optional Prometheus + Grafana (`docker-compose.obs.yml`) |
| Persistence | Control meta + worker registry on disk under `--cell-home` / `--state-dir` |
| Platforms | Linux hosts (compose uses `network_mode: host`) |
| Integrations | Python SDK (`sdk/python`); MCP (`integrations/mcp`); DSH plugin (`integrations/dsh-cell`) |

## Not supported (yet)

| Area | Status |
|------|--------|
| MicroVM workspaces > ~64MiB sync frame | Single vsock JSON frame (`MaxFrameBytes`); chunked export planned |
| Memory pause / fork (E2B-style) | Workspace tar snapshots only |
| mTLS control↔worker | Planned; HMAC tokens are the current trust boundary |
| Multi-region / HA control | Single control process; restart recovers registry from disk |
| Windows / macOS workers | Not validated |
| Guaranteed disk quotas | Host `du` enforcement; Docker `storage-opt` opt-in only |
| Formal SLA / uptime targets | Out of scope |
| Nested-virt guest-boot CI | Artifact + rootfs CI landed; full InstanceStart on GHA optional |

## Operator expectations

- Treat Cells as **hostile**: do not mount host secrets into workspaces.
- Prefer MicroVM for multi-tenant / untrusted agents; Docker for single-trust-domain speed.
- Keep `CELL_WORKER_TOKEN` long and rotate with `CELL_WORKER_TOKEN_PREV` grace.
- Production MicroVM needs jailer on PATH (avoid `CELL_MICROVM_ALLOW_UNJAILED` outside dev).
- Pin image tags for workers/control; rebuild after upgrades (see [UPGRADE.md](UPGRADE.md)).
