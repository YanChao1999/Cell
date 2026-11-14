# cell-sdk

Python SDK for [Cell](https://github.com/YanChao1999/Cell) — ephemeral isolated environments for AI agents.

Talks to the local Cell daemon over a Unix socket (`~/.cell/celld.sock` by default).

## Install

```bash
pip install -e .
```

Requires Python ≥ 3.10 and a running `celld` daemon.

## Quick start (E2B-style)

```python
import asyncio
from cell import Sandbox

async def main() -> None:
    async with await Sandbox.create(environment="python-3.12") as sbx:
        result = await sbx.commands.run("python -c 'print(40+2)'")
        print(result.stdout)  # 42
        snap = await sbx.snapshot()
        print(snap.id)

asyncio.run(main())
```

Restore later:

```python
async with await Sandbox.create(
    environment="python-3.12",
    snapshot_id=snap.id,
) as sbx:
    ...
```

## Classic Client API

```python
import asyncio
from cell import Client

async def main() -> None:
    async with Client() as client:
        cell = await client.create(
            repo="https://github.com/example/app",
            ref="main",
            environment="go-1.23",
            ttl="30m",
        )
        result = await cell.exec("go test ./...")
        print(result.stdout)
        await cell.destroy()

asyncio.run(main())
```

## One-shot run

```python
result = await client.run(
    repo="https://github.com/example/app",
    environment="python-3.12",
    commands=["pip install -e .", "pytest"],
)
```

## Connection

| Source | Value |
|--------|-------|
| Default | `unix:///home/$USER/.cell/celld.sock` |
| Override | `CELL_ENDPOINT` env var (`unix:///path` or `http://host:port`) |

## Environments

Named environments are YAML templates under `environments/` in the Cell repo (e.g. `go-1.23`, `python-3.12`). Pass the template name as `environment=` when creating a cell.
