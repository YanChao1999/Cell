# Tech debt

Follow-ups from the Copilot review on [PR #1](https://github.com/YanChao1999/Cell/pull/1) and subsequent security hardening. Do not drop remaining items when starting Docker-as-default polish or Phase 5 cluster work.

## Principle: security in design, in software

Cell is an execution boundary for untrusted agent work. Security is not a later “hardening pass” bolted onto a permissive default.

- **Fail closed.** Unknown network modes, unfinished allowlists, missing images under Docker, and missing runtime entries must reject — never silently weaken to host or open egress.
- **Enforce in the runtime path.** Policy lives in templates and API, but the daemon must implement it (`docker run` flags, process kill, startup prune). Docs alone are not control.
- **Least privilege by construction.** New Runtime backends (Docker hardening, then microVM) should default to non-root, dropped caps, no docker.sock, no host namespaces. Do not add privileged shortcuts for convenience.
- **Disposable Cells; shared only what is immutable-by-policy.** Workspace is per-Cell. Caches/mirrors may be shared only with an explicit trust model (today: writable shared caches are known debt).
- **Prefer stronger isolation over more features** when they conflict. Phase 5 cluster and new templates must not re-introduce host escape or open-by-default allowlist.

## Fixed (security-first pass, 2026-11-05)

- Missing runtime entry no longer falls back to host `sh -c`; Exec returns not-found.
- Empty image is rejected when Docker is enabled (no host escape).
- Network policy is carried through `CreateOpts`. `network: none` becomes `docker run --network none`. Host fallback rejects policies it cannot enforce.
- Policy model: `default`/`open` (normal egress for agents), `none` (air gap), `allowlist` (reserved — create is refused until an egress proxy exists; never silently opens).
- Exec timeout wraps the in-container command with `timeout` when available; cancel/destroy best-effort kills the tagged process. Host commands use a process group and are SIGKILL’d on cancel/destroy.
- Destroy retains worker/runtime records until teardown succeeds; `/v1/run` uses a separate cleanup context.
- Meta `Put`/`Delete` roll back in-memory state if persistence fails.
- Startup prunes containers labeled `cell.id`, removes unrecovered workspaces, and resets meta before serving.
- Live Unix sockets are not unlinked; only confirmed-stale sockets are removed.
- `--cell-home` and derived roots are absolutized before Docker binds / git worktrees.
- `docker run` no longer holds the runtime mutex for the whole pull/start (map update only).

## Still open

### Docker isolation hardening (checked 2026-11-05 — baseline OK, not a hardened sandbox)

Live inspect showed: not privileged, no docker.sock, no host pid/ipc/uts, no extra CapAdd/devices, binds confined to cell home workspace/cache. Residual debt — design these into `Runtime.Create`, do not leave as optional ops folklore:

- **Non-root Cell user.** Images/containers still run as uid 0. Default to a non-root user (and fix workspace/cache ownership), or user-namespace remapping. Root-in-container widens any future breakout.
- **`--security-opt=no-new-privileges`.** Prevent privilege escalation via setuid helpers inside the Cell.
- **Explicit capability policy.** Today we inherit Docker’s default cap set. Prefer `--cap-drop=ALL` plus a minimal documented add-back list required by templates; never `--privileged` / host devices.
- **Read-only rootfs + tmpfs** where builds allow (`--read-only`, writable `/tmp` / build dirs only). Shrinks persistent tampering inside the image layer.
- **Shared cache trust model.** `go-mod` / `go-build` (and peers) are writable bind mounts shared across Cells — not host escape, but cross-Cell supply-chain poison. Options: per-Cell cache overlay, read-only caches populated by a trusted fetcher, or content hashing / verification before use.
- **Kernel/runtime threat.** Docker/runc/kernel CVEs remain in scope until a microVM `Runtime` backend. Keep the `Runtime` interface the place that raises the isolation ceiling; do not bake Docker-only assumptions into the API.

Regression tests for host-escape invariants live in `internal/worker/runtime/docker/host_escape_test.go`. Add similar create-path assertions when the hardening flags above land.

### Isolation / egress

- A git worktree’s `.git` file points at the host mirror, which is not mounted in the container. `git status` and similar commands fail inside Docker cells. Mount an isolated git directory or use a self-contained checkout.
- **Egress allowlist proxy** (next network security feature): when `network: allowlist`, run Cells with no direct internet and force HTTP(S) through a Cell-managed proxy that enforces `egress.allow` hostnames, with DNS constrained and an audit log. Schema (`egress.allow`) is already parsed; runtime must refuse open egress until this exists (current behavior). Implement as software in the daemon, not as a documented manual proxy beside Cell.

### Lifecycle / ops

- `scripts/smoke.sh` can still remove a user socket and omits `--cell-home` unless CI sets `HOME`. Default to a temporary home/socket.
- CLI and Python client HTTP timeout is 120s; templates allow 30m–4h. Derive read deadlines from `timeout_seconds`.
- Template `setup` commands are parsed and never run. Execute setup inside the runtime; destroy the Cell if setup fails.
- Concurrent creates of the same repo race on the shared mirror. Serialize mirror init/update per repository.
