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
- Phase 5 foundation: in-memory worker **registry** + register/heartbeat/list API; local worker self-registers and heartbeats. Placement still uses in-process scheduler workers; **cross-node cache pull** not done.

## Still open

### MicroVM backend (real VMM)
- Firecracker / Cloud Hypervisor / QEMU behind `Runtime`. See `microvm/DESIGN.md`.

### Hardening polish
- Seed size/latency tuning; optional content hashing of seeded files.
- Stronger userns than daemon remap + `--user` (rootless/podman paths).
- Harder disk caps (project quotas) where operators can enable them.

### Phase 5 (remaining)
- Remote worker transport (RPC over registered endpoints).
- Scheduler place across registry peers (not only local process workers).
- Optional cross-node cache pull.
- Cross-platform mirror lock if Windows workers appear.
