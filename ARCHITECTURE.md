# Cell architecture

Locked design for the Cell runtime. Names match the code (`celld`, `cell`, Python `cell`). The older plan file still says Spark; this document is the current picture.

A **Cell** is a disposable workspace. Caches and git mirrors survive destroy. Mutable workspace state and credentials are not shared.

## What ships now, and what is later

Local MVP is one `celld` process: control plane plus one in-process `LocalWorker` (`celld serve --mode=all`). The agent API is HTTP JSON on a Unix socket (`~/.cell/celld.sock`). The SDK never talks to a worker directly.

`--mode=control` and `--mode=worker` are reserved. Remote `Register` / `Heartbeat`, cross-node cache pull, auth, snapshot/restore, interactive TTY, and a microVM `Runtime` are later. The same `Worker` and `Runtime` interfaces are the plug-in points.

## Network / egress policy (security-first)

Environment templates declare an egress policy. This is not “always offline”.

| Policy | Behavior | Used for |
| --- | --- | --- |
| `default` / `open` | Normal Docker outbound | Agent Cells (`go-1.23`, `python-3.12`) that need modules, docs, APIs |
| `none` | `docker run --network none` | Air-gapped embedded builds (`embedded-zephyr`, `embedded-linux`) |
| `allowlist` | Internal Docker network + `cell-egress` sidecar | No direct egress; HTTP(S) via CONNECT/forward proxy; host allowlist + JSON audit log |

Schema already accepted in templates:

```yaml
network: allowlist
egress:
  allow:
    - proxy.golang.org
    - github.com
```

Until the proxy exists, create with `network: allowlist` fails closed (never silently opens the network). Host fallback rejects `none` and `allowlist` because it cannot enforce them. When Docker is enabled, missing images and missing runtime entries never fall back to the host.

## Security in design (not bolt-on)

Cell is an isolation product. Controls belong in the daemon and `Runtime` implementation:

- Fail closed on unfinished or unenforceable policy (allowlist, host+`none`, Docker+empty image).
- No host escape when Docker is the selected backend; no docker.sock; no privileged / host-namespace shortcuts.
- Docker create path hardens by construction: non-root (`--user` daemon uid/gid), `no-new-privileges`, `--cap-drop=ALL` + minimal add-backs, read-only rootfs + tmpfs, per-Cell cache binds, RO same-path git repos mount. Remaining: allowlist egress proxy, warm trusted cache seed, microVM backend — see [TECH_DEBT.md](TECH_DEBT.md).
- Stronger isolation later is another `Runtime` (microVM stub under `internal/worker/runtime/microvm`), not an SDK change. Egress allowlist is `internal/worker/egress` (stub; create still fail-closed).

Placement order, already in `scheduler.Place`:

1. `prefer_local` (request `client_worker_id`, else the configured local worker)
2. warm `CacheHints`
3. free capacity
4. otherwise reject

Exec and destroy stay on the worker recorded in meta. They are not placed again.

## Component view

Clients may also be workers and share capacity and caches. The control plane places a Cell once. Dedicated workers and peer client-workers are the same role on other machines; only the local worker exists in the MVP.

```mermaid
flowchart TB
  subgraph clientNode [Client node]
    Agent[Agent]
    SDK[Python SDK or cell CLI]
    Agent --> SDK
  end

  SDK -->|"HTTP JSON unix socket"| API

  subgraph control [Control plane in celld]
    API[API server]
    Sched[Scheduler]
    Meta[Meta store cells.json]
    Reaper[TTL reaper]
    API --> Sched
    API --> Meta
    Reaper --> Meta
    Reaper --> Sched
  end

  Sched -->|"Place once"| LW

  subgraph worker [Worker]
    LW[LocalWorker]
    Src[Git source]
    Cache[Cache layout]
    RT[Runtime]
    LW --> Src
    LW --> Cache
    LW --> RT
    RT --> Docker[Docker runtime]
    RT --> Host[Host fallback]
    Src --> Mirrors["~/.cell/repos"]
    Cache --> CacheDir["~/.cell/cache"]
    LW --> Workspaces["~/.cell/workspaces"]
  end

  Sched -.->|"later: Register and Heartbeat"| Remote[Remote workers]
  Cache -.->|"later: cache hint or pull"| Remote
```

| Piece | Package | Role |
| --- | --- | --- |
| API server | `internal/control/api` | `GET /health`, `/v1/cells`, exec, `POST /v1/run` |
| Scheduler | `internal/control/scheduler` | Place on create; look up the sticky worker after that |
| Meta store | `internal/control/meta` | `cell_id` to worker, status, TTL; JSON under `~/.cell/state` |
| TTL reaper | `internal/control/reaper` | Tick on start and every 15s; destroy expired Cells |
| LocalWorker | `internal/worker/manager` | Checkout, template, runtime, workspace lifecycle |
| Runtime | `internal/worker/runtime` | `Create` / `Exec` / `Destroy`. Docker first; host fallback when Docker is absent or `CELL_RUNTIME=host` |
| Git source | `internal/worker/source/git` | `clone --mirror`, then worktree. Empty repo is a blank workspace |
| Cache | `internal/worker/cache` | Durable dirs and placement hints |
| Env templates | `internal/worker/envtemplate` | `go-1.23`, `python-3.12`, `embedded-zephyr`, `embedded-linux` |
| Python SDK | `sdk/python` | Primary agent client |
| CLI | `cmd/cell` | Ops: health, list, run, destroy |

## Class view

```mermaid
classDiagram
  class Server {
    +ListenAndServe(socket)
    +createCell()
    +execCell()
    +destroyCell()
  }
  class Scheduler {
    +Register(worker)
    +Place(request) Worker
    +Get(workerID) Worker
  }
  class Store {
    +Put(record)
    +Get(id) Record
    +Delete(id)
    +Expired(now) Record[]
  }
  class Reaper {
    +Run(ctx)
  }
  class Worker {
    <<interface>>
    +ID() string
    +Capacity() Capacity
    +CacheIndex() CacheIndex
    +CreateCell(request) Cell
    +Exec(cellID, request) ExecResult
    +DestroyCell(cellID)
  }
  class LocalWorker {
    +workspaces
    +cells
  }
  class Runtime {
    <<interface>>
    +Create(CreateOpts)
    +Exec(ExecOpts) ExecResult
    +Destroy(cellID)
  }
  class DockerRuntime {
    +useDocker bool
    +cells
  }
  class Source {
    <<interface>>
    +Fetch(repo, ref, dest)
    +Release(dest)
  }
  class GitProvider {
    +ReposRoot string
  }
  class EnvLoader {
    +Load(name) Template
  }
  class CacheLayout {
    +ResolveMounts()
    +HostEnv()
  }

  Server --> Scheduler
  Server --> Store
  Reaper --> Store
  Reaper --> Scheduler
  Scheduler --> Worker
  LocalWorker ..|> Worker
  LocalWorker --> Runtime
  LocalWorker --> Source
  LocalWorker --> EnvLoader
  LocalWorker --> CacheLayout
  DockerRuntime ..|> Runtime
  GitProvider ..|> Source
```

`DockerRuntime` is the only `Runtime` implementation. It selects Docker when `docker info` works and the template has an image, and host `sh -c` otherwise. A future microVM backend implements `Runtime` and does not change the SDK or the scheduler.

## Create, exec, destroy

```mermaid
sequenceDiagram
  actor Agent
  participant API as API server
  participant Sched as Scheduler
  participant Meta as Meta store
  participant W as LocalWorker
  participant Git as Git source
  participant RT as Runtime

  Agent->>API: POST /v1/cells
  API->>Sched: Place
  Sched-->>API: worker
  API->>W: CreateCell
  W->>Git: Fetch mirror and worktree
  W->>W: resolve environment template
  W->>RT: Create container or host cell
  W-->>API: Cell ready
  API->>Meta: Put id, worker, expiry
  API-->>Agent: CellInfo

  Agent->>API: POST /v1/cells/id/exec
  API->>Meta: Get worker id
  API->>W: Exec
  W->>RT: Exec
  RT-->>Agent: exit, stdout, stderr

  Agent->>API: DELETE /v1/cells/id
  API->>Meta: Get worker id
  API->>W: DestroyCell
  W->>RT: Destroy
  W->>Git: Release worktree
  API->>Meta: Delete
```

`POST /v1/run` is the same three steps in one request: create, exec each command, destroy. The TTL reaper calls the same destroy path for expired records.

## Disk layout

```text
~/.cell/
  celld.sock
  state/cells.json
  repos/<hash>/          bare mirrors, durable
  workspaces/<id>/       disposable
  cache/                 durable: go-mod, go-build, pip-cache, zephyr, yocto, ccache
```

## Open follow-ups

Accepted debt (including Docker sandbox hardening and egress allowlist proxy) is in [TECH_DEBT.md](TECH_DEBT.md). Security stays in design and in the runtime software path. Phase 5 is still the cluster: worker register and heartbeat, `prefer_local` across peers, and optional cross-node cache pull.
