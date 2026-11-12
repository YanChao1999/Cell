# Changelog

All notable changes to Cell are documented here. Versioning follows semver-ish `0.y.z` while the project is pre-1.0.

## [0.7.0] — pending tag

Internal-production (P1) cut. Binaries report `0.7.0` once tagged (tree may still show `0.7.0-dev` on main between tags).

### Added
- Ops docs: support matrix, upgrade notes, restart/rolling playbook (`docs/ops/`)
- Security docs: threat model + Docker escape checklist (`docs/security/`)
- CI: Compose cluster smoke, `govulncheck`, Dependabot (Go / Actions / Docker)
- Digest-pinned `golang:1.26-bookworm` base images (ECR public)
- MicroVM M1: Firecracker Backend, `cell-guest-agent` (vsock), `scripts/fetch-microvm-artifacts.sh`
- Docker `--pids-limit` (default 256, override `CELL_DOCKER_PIDS_LIMIT`)
- Unified `internal/version`; GitHub Pages isolation animation

### From 0.6 / P0 (already on main before this cut)
- Short-lived HMAC worker tokens + rotation grace
- Prometheus `/metrics` + built-in `/metrics/ui`
- Durable control meta + worker registry
- Optional Grafana/Prometheus via GHCR-mirrored images

### Still not in 0.7.0
- MicroVM M2 (egress/jailer) / M3 (rootfs CI)
- mTLS control↔worker
- Formal SLA

## [Unreleased]

- Bump Go toolchain to `go1.26.11` (govulncheck GO-2026-6859 / net/http MaxBytesReader)
