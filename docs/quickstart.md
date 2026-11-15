# Cell quickstart

Self-hosted ephemeral sandboxes for AI coding agents (E2B-style API, MIT).

## 5-minute path (Docker)

```bash
go build -o bin/celld ./cmd/celld
./bin/celld serve --mode=all --environments ./environments
```

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

asyncio.run(main())
```

Default runtime is Docker when a daemon is available; otherwise host fallback (dev/CI only).

## MicroVM path (Firecracker)

Stronger isolation for untrusted agent code:

```bash
./scripts/setup-microvm.sh
export PATH="$HOME/.cell/runtime/microvm:$PATH"
export CELL_RUNTIME=microvm
export CELL_MICROVM_DIR=$HOME/.cell/runtime/microvm
export CELL_MICROVM_JAILER=$HOME/.cell/runtime/microvm/jailer
# Dev only if jailer cannot run:
# export CELL_MICROVM_ALLOW_UNJAILED=1

./bin/celld serve --mode=all --environments ./environments
./scripts/microvm-smoke.sh
```

Needs Linux + `/dev/kvm`. Rootfs is ~11MiB (busybox applets + `rg` + `cell-guest-agent`).

## MCP (Cursor / other agents)

```bash
pip install -e integrations/mcp
# Cursor mcp.json — see integrations/mcp/README.md
cell-mcp
```

## DeepSeek Harness

```bash
dsh plugin add ./integrations/dsh-cell
# tools: cell_create / cell_exec / cell_snapshot / cell_kill
```

## vs cloud sandboxes

| | Cell (this repo) | Typical cloud (E2B, …) |
|--|------------------|-------------------------|
| License | MIT, self-host | Open runtime + metered cloud |
| Isolation | Docker harden or Firecracker | Firecracker fleet |
| Egress | `none` / `allowlist` + audit | Vendor policy |
| Data residency | Your host | Vendor regions |

Cell is **not** a hosted SaaS clone — it is the self-hosted / local-first control plane + worker you operate.
