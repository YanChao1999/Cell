#!/usr/bin/env python3
"""Minimal Cell Python SDK example.

Requires a running celld daemon (default: ~/.cell/celld.sock).
"""

from __future__ import annotations

import asyncio
import sys

from cell import Client


async def main() -> int:
    repo = sys.argv[1] if len(sys.argv) > 1 else "https://github.com/example/app"
    async with Client() as client:
        cell = await client.create(
            repo=repo,
            ref="main",
            environment="go-1.23",
            ttl="30m",
        )
        print(f"created cell {cell.id} on worker {cell.worker_id}")
        try:
            result = await cell.exec("go test ./...")
            print(f"exit={result.exit_code}")
            if result.stdout:
                print(result.stdout, end="")
            if result.stderr:
                print(result.stderr, end="", file=sys.stderr)
            return result.exit_code
        finally:
            await cell.destroy()
            print(f"destroyed cell {cell.id}")


if __name__ == "__main__":
    raise SystemExit(asyncio.run(main()))
