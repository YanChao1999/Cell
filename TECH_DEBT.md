# Tech debt

Follow-ups from security hardening. Do not drop remaining items when starting Phase 5 cluster work.

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
- Per-Cell caches; **trusted seed** via `cache.SeedCopy` (no hardlinks; refuses symlinks).
- Git repos RO same-path bind.
- **`network: allowlist`**: internal Docker network + `cell-egress` sidecar with CONNECT/forward proxy, host allowlist, JSON audit log. Optional **upstream chain** via `CELL_EGRESS_UPSTREAM` / host `HTTPS_PROXY` on the sidecar only.

### Single-node UX
- Template **`setup`** runs on create (fail → destroy + rollback).
- CLI/SDK client timeouts no longer clip template budgets (`CELL_CLIENT_TIMEOUT` override).
- Smoke uses ephemeral `--cell-home` by default; builds `cell-egress`.
- Per-repo git **mirror flock** during clone/fetch.
- Disk → Docker `--storage-opt size=` **opt-in** (`CELL_DOCKER_DISK_STORAGE_OPT=1`; most hosts lack overlay2+xfs pquota).
- CI builds `cell-egress` and runs worker package tests (cache/env/egress/manager/git/docker).

## Still open

### MicroVM backend (real VMM)
- Firecracker / Cloud Hypervisor / QEMU behind `Runtime` — needs VMM binary, guest kernel/rootfs, vsock/tap, jailer. See `microvm/DESIGN.md`. Do not mark done until Create boots a VM.

### Hardening polish
- User-namespace remapping beyond uid/gid match.
- Seed size/latency tuning; optional content hashing of seeded files.
- Proxy sidecar image choice when Cell image cannot run the static/host `cell-egress` binary.
- Portable disk enforcement without storage-opt / pquota (or document operator requirements).

### Usability / ops (remaining)
- Richer setup (conditional steps, setup-only timeout field separate from exec).
- Cross-platform mirror lock if Windows workers appear.

### Phase 5 (cluster) — after single-node UX is solid
- Worker register and heartbeat, `prefer_local` across peers, optional cross-node cache pull.
