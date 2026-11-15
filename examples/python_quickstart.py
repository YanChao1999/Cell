#!/usr/bin/env python3
"""Minimal Cell Python SDK example (E2B-style Sandbox).

Requires a running celld daemon (default: ~/.cell/celld.sock).
"""

from __future__ import annotations

import asyncio
import sys

from cell import Sandbox


async def main() -> int:
    env = sys.argv[1] if len(sys.argv) > 1 else "python-3.12"
    async with await Sandbox.create(environment=env, ttl="15m") as sbx:
        print(f"sandbox {sbx.id}")
        await sbx.files.write("hello.txt", "cell\n")
        result = await sbx.commands.run("cat hello.txt && grep -n cell hello.txt")
        print(f"exit={result.exit_code}")
        if result.stdout:
            print(result.stdout, end="")
        if result.stderr:
            print(result.stderr, end="", file=sys.stderr)
        snap = await sbx.snapshot()
        print(f"snapshot {snap.id} ({snap.bytes} bytes)")
        return result.exit_code


if __name__ == "__main__":
    raise SystemExit(asyncio.run(main()))
