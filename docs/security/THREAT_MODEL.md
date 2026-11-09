# Threat model (简版 — P1)

## Asset

- **Cell workspace** — untrusted agent code and tool output
- **Host** — worker machine running `celld` / Docker
- **Control plane** — scheduler, meta, registry, token issuance
- **Caches / git mirrors** — durable shared state across Cells

## Actors

| Actor | Intent |
|-------|--------|
| Agent / Cell guest | Escape isolation, poison caches, reach internal network, steal tokens |
| Compromised worker | Impersonate peers, pull caches, schedule hostile work |
| Network attacker | Replay/forge worker RPC without Bearer/HMAC material |
| Operator error | `CELL_RUNTIME=host`, weak token, privileged Docker mounts |

## Trust boundaries

1. **Guest → Host**: Docker (or future microVM) is the isolation boundary. Host fallback is **dev/CI only**.
2. **Worker ↔ Control**: Shared secret (`CELL_WORKER_TOKEN`) + optional short-lived HMAC tokens. Not mTLS yet.
3. **Worker ↔ Worker**: Cache pull over worker RPC with the same Bearer auth.
4. **Egress**: Template `network` (`none` / `default` / `allowlist` via `cell-egress`).

## What we prevent today (design intent)

- Silent downgrade to host when Docker is configured but missing (fail closed for non-host modes)
- Privileged container shortcuts (`no-new-privileges`, cap-drop, read-only rootfs + tmpfs)
- Unauthenticated worker RPC when a token is configured
- Cross-Cell writable shared package caches (per-Cell mounts + seed copy)

## What we do not yet prevent

- Kernel / container breakout (mitigation path: microVM)
- Stolen long-lived `CELL_WORKER_TOKEN` without rotation detection beyond `PREV`
- Compromised control plane forging placements
- Supply-chain compromise of base images (mitigation: pin digests, CVE scan in CI)

## Residual risk acceptance (internal P1)

Internal production may run Docker isolation with egress allowlists and token auth, accepting container-escape residual risk until microVM is default for high-trust boundaries. See [ESCAPE_CHECKLIST.md](ESCAPE_CHECKLIST.md).
