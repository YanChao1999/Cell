# Changelog

All notable changes to Cell are documented here. Versioning follows semver-ish `0.y.z` while the project is pre-1.0.

## [Unreleased]

## [0.8.0] — 2026-11-15

Productization cut: discoverability, MicroVM one-shot, MCP + DSH plugin.

### Added
- Quickstart + SUPPORT refresh; MicroVM one-shot `./scripts/setup-microvm.sh` + `microvm-smoke.sh`
- MCP server (`integrations/mcp`, `cell-mcp`) for Cursor and other agents (`mcp>=1.0,<2` for FastMCP)
- DeepSeek Harness bundle (`integrations/dsh-cell` / `dsh-plugin-cell`) with `cell_create` / `cell_exec` / `cell_snapshot` / `cell_kill`
- MicroVM M2: jailer; `network: none` / `allowlist` (TAP + host egress + DNS)
- MicroVM M3: ultra-tiny RO rootfs (~6–11MiB, busybox + optional `rg` + `base64`); CI rootfs artifact
- Python SDK: E2B-style `Sandbox` / files / snapshot
- Workspace snapshot API with hardened restore (`snapshot_id` validation, wipe-before-unpack, skip fetch/setup)
- `fetch-microvm-artifacts.sh`: Firecracker quickstart S3 `vmlinux` fallback when release asset is missing

### Notes
- Host smoke without Docker uses env setup binaries on `PATH` (e.g. `go` for `go-1.23`; `python` → `python3` for `python-3.12`)
- Full guest boot still needs `/dev/kvm` + rootfs; `CELL_MICROVM_SKIP_RG=1` when GitHub rate-limits `rg` downloads

## [0.7.0] — 2026-11-12

Internal-production (P1) cut.

### Added
- Ops docs: support matrix, upgrade notes, restart/rolling playbook (`docs/ops/`)
- Security docs: threat model + Docker escape checklist (`docs/security/`)
- CI: Compose cluster smoke, `govulncheck`, Dependabot (Go / Actions / Docker)
- Digest-pinned `golang:1.26-bookworm` base images (ECR public)
- MicroVM M1: Firecracker Backend, `cell-guest-agent` (vsock), `scripts/fetch-microvm-artifacts.sh`
- Docker `--pids-limit` (default 256, override `CELL_DOCKER_PIDS_LIMIT`)
- Unified `internal/version`; GitHub Pages isolation animation
- Local host-runtime cluster smoke script (`scripts/cluster-local-smoke.sh`)

### Fixed
- Bump Go toolchain to `go1.26.11` (govulncheck GO-2026-6859 / net/http MaxBytesReader)

### From 0.6 / P0 (already on main before this cut)
- Short-lived HMAC worker tokens + rotation grace
- Prometheus `/metrics` + built-in `/metrics/ui`
- Durable control meta + worker registry
- Optional Grafana/Prometheus via GHCR-mirrored images

### Still not in 0.7.0
- MicroVM M2 (egress/jailer) / M3 (rootfs CI)
- mTLS control↔worker
- Formal SLA
