# Tech debt

Accepted follow-up from the Copilot review on [PR #1](https://github.com/YanChao1999/Cell/pull/1) (2026-11-05). These are confirmed against the current code and are **not fixed**. Do not drop them when starting Docker-as-default, restart hardening, or Phase 5 cluster work.

## Isolation (before Docker is the default path)

- Missing runtime entry runs the command on the host. `Exec` in `internal/worker/runtime/docker/docker.go` builds a host `sh -c` when the cell is absent from the runtime map, including while a concurrent delete is removing a container. Return not-found.
- An empty image takes the same host path while Docker is available. Reject a missing image, or resolve a default container environment before create.
- Template `network` is parsed and then dropped in `LocalWorker.resolveEnv`. `network: none` never becomes `docker run --network none`. Carry it on `CreateOpts`. Reject network policies the host fallback cannot enforce.
- An exec timeout kills only the local `docker exec` client. The process inside the container keeps running. Enforce the deadline inside the container and terminate that process group when the request context is canceled.
- On the host path, cancellation kills `sh` and leaves its children, which can hold stdout/stderr open past `timeout_seconds`. Terminate the process group (same behavior as the host stub in `docker.go`).
- Host destroy forgets the cell and does not stop commands already running in `Exec`. Track executions per cell, cancel their process groups, and wait before reporting success.
- A git worktree’s `.git` file points at the host mirror, which is not mounted in the container. `git status` and similar commands fail inside Docker cells. Mount an isolated git directory or use a self-contained checkout.

## Lifecycle (success reported while resources remain)

- After restart, `meta.New` reloads `cells.json`, but the worker and runtime maps start empty. List still shows those cells; exec and the reaper cannot find their containers or workspaces. Reconcile from live resources, or tear them down during startup, before serving or starting the reaper (`cmd/celld/main.go`).
- Delete and the reaper ignore `DestroyCell` errors and remove metadata anyway (`destroyCellOn` in `internal/control/api/server.go`). Keep the record until worker teardown succeeds.
- The worker deletes its record before `docker rm` and workspace removal, and ignores both errors (`LocalWorker.DestroyCell`). Propagate failures and retain the record until cleanup succeeds.
- The runtime map drops the container id before `docker rm -f` returns. A failed remove cannot be retried. Delete the entry only after teardown succeeds.
- `/v1/run` destroys with the request context. A client disconnect or HTTP timeout cancels `docker rm` as well. Use a separate bounded cleanup context.
- `meta.Store.Put` and `Delete` update the in-memory map before persistence. A failed write leaves list/get disagreeing with `cells.json`, and a later write can persist a ghost record. Restore the previous map entry when persistence fails.

## Operational follow-ups

- A second `celld` unlinks a live socket (`os.Remove` in `ListenAndServe`) and both processes then share `cells.json`. Remove only a confirmed stale socket.
- `scripts/smoke.sh` removes that socket and never passes `--cell-home`, so a local smoke run can hit the normal daemon state. Default to a temporary home and socket, and pass `--cell-home`.
- The CLI (`cmd/cell`, 120s) and Python client (`sdk/python/src/cell/client.py`, default 120s) time out before template exec limits (Go 30m, Zephyr 2h, embedded-linux 4h). Give run/exec a longer read deadline, or derive it from `timeout_seconds`.
- Relative `--cell-home` values are not made absolute before Docker bind mounts and git worktrees. Normalize roots in `cmd/celld` before constructing providers; `cache.Layout.Resolve` and `manager.New` need the same absolute-path invariant.
- Template `setup` commands are parsed and tested, and the worker never runs them. The Python template’s `pip install` does not happen. Run setup inside the selected runtime and destroy the cell if setup fails.
- `Runtime.Create` holds the runtime mutex across `docker run`, so an image pull blocks every other exec and destroy. Lock only the runtime map.
- Concurrent creates of the same repo race on the shared mirror (`internal/worker/source/git/git.go`): double clone, cleanup deleting a finished mirror, and concurrent fetches on git ref locks. Serialize mirror init and update per repository.
