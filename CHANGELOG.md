# Changelog

All notable changes to Cell are documented here. Versioning follows semver-ish `0.y.z` while the project is pre-1.0.

## [Unreleased]

- MicroVM M2: jailer boot path; `network: none` / `allowlist` (TAP + host cell-egress + DNS forwarder); reject open NAT
- Dependabot: bump GitHub Actions majors (checkout/setup-go/setup-python/deploy-pages/login-action)

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
