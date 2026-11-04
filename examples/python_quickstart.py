#!/usr/bin/env python3
"""Minimal Spark Python SDK example.

Requires a running sparkd daemon (default: ~/.spark/sparkd.sock).
"""

from __future__ import annotations

import asyncio
import sys

from spark import Client


async def main() -> int:
    repo = sys.argv[1] if len(sys.argv) > 1 else "https://github.com/example/app"
    async with Client() as client:
        spark = await client.create(
            repo=repo,
            ref="main",
            environment="go-1.23",
            ttl="30m",
        )
        print(f"created spark {spark.id} on worker {spark.worker_id}")
        try:
            result = await spark.exec("go test ./...")
            print(f"exit={result.exit_code}")
            if result.stdout:
                print(result.stdout, end="")
            if result.stderr:
                print(result.stderr, end="", file=sys.stderr)
            return result.exit_code
        finally:
            await spark.destroy()
            print(f"destroyed spark {spark.id}")


if __name__ == "__main__":
    raise SystemExit(asyncio.run(main()))
