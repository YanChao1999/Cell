"""E2B-style filesystem helpers over Cell exec (workspace-relative paths)."""

from __future__ import annotations

import base64
import shlex
from typing import TYPE_CHECKING

if TYPE_CHECKING:
    from cell.client import Cell


class Files:
    """Read/write/list files inside a live Cell workspace (``/workspace``)."""

    def __init__(self, cell: Cell) -> None:
        self._cell = cell

    async def write(self, path: str, data: str | bytes) -> None:
        rel = _workspace_rel(path)
        if isinstance(data, str):
            payload = base64.b64encode(data.encode("utf-8")).decode("ascii")
        else:
            payload = base64.b64encode(data).decode("ascii")
        # mkdir -p + base64 decode keeps binary-safe writes without a dedicated RPC.
        cmd = (
            f"mkdir -p $(dirname -- {shlex.quote(rel)}) && "
            f"printf %s {shlex.quote(payload)} | base64 -d > {shlex.quote(rel)}"
        )
        result = await self._cell.exec(cmd)
        if result.exit_code != 0:
            raise RuntimeError(
                f"files.write({path!r}) failed: {result.stderr or result.stdout}"
            )

    async def read(self, path: str, *, binary: bool = False) -> str | bytes:
        rel = _workspace_rel(path)
        result = await self._cell.exec(f"base64 {shlex.quote(rel)}")
        if result.exit_code != 0:
            raise FileNotFoundError(
                f"files.read({path!r}): {result.stderr or result.stdout}"
            )
        raw = base64.b64decode("".join(result.stdout.split()))
        return raw if binary else raw.decode("utf-8")

    async def list(self, path: str = ".") -> list[str]:
        rel = _workspace_rel(path)
        result = await self._cell.exec(
            f"find {shlex.quote(rel)} -maxdepth 1 -mindepth 1 -printf '%f\\n' | sort"
        )
        if result.exit_code != 0:
            raise FileNotFoundError(
                f"files.list({path!r}): {result.stderr or result.stdout}"
            )
        return [line for line in result.stdout.splitlines() if line]


def _workspace_rel(path: str) -> str:
    p = path.replace("\\", "/").lstrip("/")
    if p.startswith("workspace/"):
        p = p[len("workspace/") :]
    if ".." in p.split("/"):
        raise ValueError(f"path escapes workspace: {path!r}")
    return p or "."
