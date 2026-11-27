# Custom images & environment limits

Cell runs each sandbox from an **environment YAML** under `environments/`.
The `image` field is a normal container image; you do **not** need a custom
image for every language — only when the stock tags are missing toolchains.

## When to use a custom image

| Workload | Recommendation |
|----------|----------------|
| Python / Go / Node | Stock tags (`python:3.12-bookworm`, `golang:…`) |
| C/C++ cmake/ninja | Stock `gcc:*-bookworm` via `environments/cpp.yaml` |
| Yocto / OE / Buildroot | **Custom** (or org-pinned) image + `embedded-linux` template |
| Cross-compile + SDK sysroot | Custom image with SDK pre-installed |
| Air-gapped / licensed tools | Custom image + `network: none` + cache mounts |

**Do not mix** unrelated toolchains (Yocto + Zephyr + desktop C++) into one
image. Prefer **multiple environments**; agents pick the one that matches the
task. Shared durable caches (`ccache`, `yocto-sstate`, …) can still be reused
across environments via `cache_mounts`.

## Limits are required (cpu / memory / disk)

Every environment **must** set `default_limits` with all three fields:

```yaml
default_limits:
  cpu: "4"      # Docker --cpus
  memory: 8Gi   # Docker --memory
  disk: 20Gi    # host workspace du cap (portable); optional Docker storage-opt
```

Create fails closed if any field is missing after merging request overrides.
Per-create overrides:

```json
{ "environment": "cpp", "limits": { "cpu": "8", "memory": "16Gi", "disk": "40Gi" } }
```

Guidance:

- Light language sandboxes: `cpu: "2"`, `memory: 4Gi`, `disk: 10Gi`
- C++ / multi-core builds: `cpu: "4"`, `memory: 8Gi`, `disk: 20Gi+`
- Yocto/OE: `cpu: "4"+`, `memory: 16Gi+`, `disk: 50Gi+` (see `embedded-linux.yaml`)

Disk is always enforced via host workspace measurement. Docker `--storage-opt`
is opt-in (`CELL_DOCKER_DISK_STORAGE_OPT=1`) because many overlay2 installs
reject it.

## Example: pin a private C++ image

```yaml
name: cpp-org
image: ghcr.io/example/cell-cpp@sha256:…
cache_mounts:
  - host: ccache
    container: /root/.ccache
default_limits:
  cpu: "8"
  memory: 16Gi
  disk: 40Gi
network: allowlist
egress:
  allow:
    - github.com
    - proxy.example.com
```

## Local / dirty tree (no GitHub clone)

1. `POST /v1/cells` with `"async": true` and **empty** `repo` (or omit it)
2. Poll `GET /v1/cells/{id}` until `status` is `ready` (or `error`)
3. `PUT /v1/cells/{id}/workspace` with a gzip tar of the local tree

MCP: `cell_create(..., async=True)` → `cell_wait` → `cell_sync(local_path)`.

Response `timings` breaks down `fetch_ms` (git) vs `runtime_ms` (Docker/microVM)
vs `setup_ms` for tuning.
