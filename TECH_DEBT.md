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

### Docker sandbox + warm seed + allowlist proxy (this branch)
- Non-root, `no-new-privileges`, cap-drop ALL + minimal, read-only rootfs + tmpfs.
- Per-Cell caches; **trusted seed** via `cache.SeedCopy` (no hardlinks; refuses symlinks).
- Git repos RO same-path bind.
- **`network: allowlist`**: internal Docker network + `cell-egress` sidecar (`cmd/cell-egress`) with CONNECT/forward proxy, host allowlist, JSON audit log. Cell has no direct egress.
- MicroVM: fail-closed stub + `internal/worker/runtime/microvm/DESIGN.md` (not theater).

## Still open

### MicroVM backend (real VMM)
- Firecracker / Cloud Hypervisor / QEMU behind `Runtime` — needs VMM binary, guest kernel/rootfs, vsock/tap, jailer. See `microvm/DESIGN.md`. Do not mark done until Create boots a VM.

### Hardening polish
- User-namespace remapping beyond uid/gid match.
- Seed size/latency tuning; optional content hashing of seeded files.
- Wire `go test ./internal/worker/runtime/docker/` and egress tests into CI; build `bin/cell-egress` in CI/smoke.
- Proxy sidecar image choice when Cell image cannot run the static/host `cell-egress` binary.

### Usability / ops
- Template `setup` on create.
- CLI/SDK timeouts vs long template budgets.
- Smoke temp `--cell-home`.
- Per-repo git mirror lock.
- Disk limits on `docker run`.
