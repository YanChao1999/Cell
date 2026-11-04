# spark-sdk

Python SDK for [Spark](https://github.com/) — ephemeral isolated environments for AI agents.

Talks to the local Spark daemon over a Unix socket (`~/.spark/sparkd.sock` by default).

## Install

```bash
pip install -e .
```

Requires Python ≥ 3.10 and a running `sparkd` daemon.

## Quick start

```python
import asyncio
from spark import Client

async def main() -> None:
    async with Client() as client:
        spark = await client.create(
            repo="https://github.com/example/app",
            ref="main",
            environment="go-1.23",
            ttl="30m",
        )
        result = await spark.exec("go test ./...")
        print(result.stdout)
        await spark.destroy()

asyncio.run(main())
```

## One-shot run

Create, exec a list of commands, and destroy in a single call:

```python
result = await client.run(
    repo="https://github.com/example/app",
    environment="python-3.12",
    commands=["pip install -e .", "pytest"],
)
for r in result.results:
    print(r.exit_code, r.stdout)
```

## Connection

| Source | Value |
|--------|-------|
| Default | `unix:///home/$USER/.spark/sparkd.sock` |
| Override | `SPARK_ENDPOINT` env var (`unix:///path` or `http://host:port`) |

## Environments

Named environments are YAML templates under `environments/` in the Spark repo (e.g. `go-1.23`, `python-3.12`). Pass the template name as `environment=` when creating a spark.
