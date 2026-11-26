# Cell

**Self-hosted sandboxes for AI coding agents** — MIT, E2B-style Python API, Docker or Firecracker isolation.

A **Cell** is a disposable execution workspace: checkout code, install deps, build/test, snapshot, destroy — while reusable caches survive. You run the control plane; data stays on your hosts.

**Start here:** [docs/quickstart.md](docs/quickstart.md) · Support: [docs/ops/SUPPORT.md](docs/ops/SUPPORT.md) · Custom images / limits: [docs/custom-images.md](docs/custom-images.md)

| Path | Command |
|------|---------|
| Docker (default) | `go build -o bin/celld ./cmd/celld && ./bin/celld serve --mode=all --environments ./environments` |
| MicroVM | `make celld microvm` then `CELL_RUNTIME=microvm ./bin/celld serve …` |
| Python SDK | `pip install -e sdk/python` → `Sandbox.create` / `commands.run` / `files.*` / `snapshot` |
| MCP (Cursor) | `pip install -e integrations/mcp` → `cell-mcp` |
| DeepSeek Harness | `dsh plugin add ./integrations/dsh-cell` |

## Why Cell (vs cloud sandboxes)

| | Cell | Typical hosted sandbox |
|--|------|-------------------------|
| License / ops | MIT, you operate | Usage SaaS (+ optional OSS runtime) |
| Isolation | Hardened Docker **or** Firecracker microVM | Usually Firecracker fleet |
| Egress | `none` / `allowlist` + audit log | Vendor policy |
| Snapshots | Workspace tar restore (`snapshot_id`) | Often memory pause/fork |
| Fit | Self-host, air-gap-ish, DSH/Cursor plugins | Zero-ops cloud |

Isolation story and diagrams: [ARCHITECTURE.md](ARCHITECTURE.md) · MicroVM design: `internal/worker/runtime/microvm/DESIGN.md`.

## Architecture (short)

- **Control plane** — HTTP Agent API, scheduler, meta store, TTL reaper
- **Workers** — create / exec / destroy / snapshot; local or remote
- **Source** — `git clone --mirror` + worktree into the workspace
- **Runtime** — Docker (default), host (dev), or `CELL_RUNTIME=microvm`
- **Caches** — durable host dirs; env templates declare mounts
- **Network** — `default` \| `none` \| `allowlist` (`cell-egress`)

Module: `github.com/YanChao1999/Cell` · version `0.8.1-dev` (post-0.8.0).

## Build

```bash
make build   # bin/celld cell cell-egress cell-guest-agent
make test    # ./internal/worker/... control/... version/
```

## Run (`mode=all`)

```bash
./bin/celld serve --mode=all \
  --cell-home ~/.cell \
  --environments ./environments \
  --cache-dir ~/.cell/cache \
  --socket ~/.cell/celld.sock
```

| Flag | Default |
|------|---------|
| `--cell-home` | `~/.cell` |
| `--repos` | `<cell-home>/repos` |
| `--workspaces` | `<cell-home>/workspaces` |
| `--cache-dir` | `<cell-home>/cache` |
| `--state-dir` | `<cell-home>/state` |
| `--environments` | `./environments` |
| `--socket` | `<cell-home>/celld.sock` |

Without Docker, celld logs a host-fallback line (dev/CI only).

## MicroVM (one shot)

```bash
make celld microvm   # build + Firecracker/jailer/kernel/rootfs + smoke
# prints exports: CELL_RUNTIME / CELL_MICROVM_DIR / PATH / jailer
# Dev only: export CELL_MICROVM_ALLOW_UNJAILED=1
```

Smoke skips guest boot without `/dev/kvm`. CI uploads a `cell-microvm-rootfs` artifact from the Ultra-tiny MicroVM rootfs job (14-day retention).

## Python SDK

```bash
pip install -e sdk/python
```

```python
import asyncio
from cell import Sandbox

async def main():
    async with await Sandbox.create(environment="python-3.12") as sbx:
        await sbx.files.write("hi.py", "print(42)\n")
        print((await sbx.commands.run("python hi.py")).stdout)
        snap = await sbx.snapshot()
        print(snap.id)

asyncio.run(main())
```

## Integrations

- **MCP** — [integrations/mcp](integrations/mcp/README.md) (`cell_create` / `cell_exec` / `cell_snapshot` / `cell_kill` / `cell_list`)
- **DeepSeek Harness** — [integrations/dsh-cell](integrations/dsh-cell/README.md)

## Caches

On start, celld ensures well-known dirs under `--cache-dir`:

`go-mod`, `go-build`, `pip-cache`, `python-venv`, `zephyr-modules`, `west-cache`, `yocto-downloads`, `yocto-sstate`, `ccache`

Env templates declare mounts and **required** `default_limits` (cpu/memory/disk); see `environments/` and [docs/custom-images.md](docs/custom-images.md).

## TTL reaper

Default Cell TTL is **30m** (override with create `ttl`). The reaper ticks on start and every 15s.

## Cluster / observability

```bash
# One-liners (see Makefile):
make celld up              # host-build binaries + Docker-runtime compose cluster
make celld local-cluster   # alias for up
make celld obs             # Prometheus + Grafana
make celld status          # health + workers
make celld mcp-install     # cell-mcp for Cursor (.cursor/mcp.json)
./scripts/cluster-smoke.sh
# Grafana http://127.0.0.1:13000 (admin/cell) · metrics UI http://127.0.0.1:18080/metrics/ui
```

## Contributing & community

- Contribute: [CONTRIBUTING.md](CONTRIBUTING.md)
- Code of Conduct: [CODE_OF_CONDUCT.md](CODE_OF_CONDUCT.md)
- Security reports (private): [SECURITY.md](SECURITY.md)

## Ops & security

- Support matrix: [docs/ops/SUPPORT.md](docs/ops/SUPPORT.md)
- Restart / rolling: [docs/ops/PLAYBOOK.md](docs/ops/PLAYBOOK.md)
- Upgrades: [docs/ops/UPGRADE.md](docs/ops/UPGRADE.md)
- Release cut: [docs/ops/RELEASE.md](docs/ops/RELEASE.md) (`./scripts/release.sh x.y.z`)
- npm / PyPI adapters: [docs/ops/PUBLISH.md](docs/ops/PUBLISH.md) (`dsh-plugin-cell`, `cell-mcp`)
- Changelog: [CHANGELOG.md](CHANGELOG.md)
- Threat model: [docs/security/THREAT_MODEL.md](docs/security/THREAT_MODEL.md)
- Escape checklist: [docs/security/ESCAPE_CHECKLIST.md](docs/security/ESCAPE_CHECKLIST.md)
- Tech debt: [TECH_DEBT.md](TECH_DEBT.md)
