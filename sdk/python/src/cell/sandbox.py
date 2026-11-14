"""E2B-style Sandbox façade over the Cell Client API."""

from __future__ import annotations

from typing import Any, Sequence

from cell.client import Cell, Client
from cell.files import Files
from cell.types import CellInfo, ExecResult, Limits, RunResult, SnapshotInfo


class Commands:
    """Command runner bound to a live sandbox (``sandbox.commands.run``)."""

    def __init__(self, cell: Cell) -> None:
        self._cell = cell

    async def run(
        self,
        command: str,
        *,
        timeout_seconds: int | None = None,
    ) -> ExecResult:
        return await self._cell.exec(command, timeout_seconds=timeout_seconds)


class Sandbox:
    """Agent-facing sandbox handle aligned with E2B's ``Sandbox.create()`` mindshare.

    Example::

        async with await Sandbox.create(environment="python-3.12") as sbx:
            await sbx.files.write("hello.txt", "hi")
            result = await sbx.commands.run("pytest -q")
            snap = await sbx.snapshot()
    """

    def __init__(self, cell: Cell, client: Client) -> None:
        self._cell = cell
        self._client = client
        self.commands = Commands(cell)
        self.files = Files(cell)

    @property
    def id(self) -> str:
        return self._cell.id

    @property
    def info(self) -> CellInfo:
        return self._cell.info

    @property
    def is_running(self) -> bool:
        status = (self._cell.status or "").lower()
        return status in ("", "running", "ready", "active")

    @classmethod
    async def create(
        cls,
        *,
        client: Client | None = None,
        repo: str = "",
        ref: str = "main",
        environment: str = "python-3.12",
        ttl: str = "30m",
        prefer_local: bool = True,
        limits: Limits | dict[str, str] | None = None,
        snapshot_id: str | None = None,
        **kwargs: Any,
    ) -> Sandbox:
        owns = client is None
        c = client or Client()
        if owns:
            await c.__aenter__()
        try:
            cell = await c.create(
                repo=repo,
                ref=ref,
                environment=environment,
                ttl=ttl,
                prefer_local=prefer_local,
                limits=limits,
                snapshot_id=snapshot_id,
                **kwargs,
            )
        except Exception:
            if owns:
                await c.aclose()
            raise
        sbx = cls(cell, c)
        sbx._owns_client = owns  # type: ignore[attr-defined]
        return sbx

    async def snapshot(self) -> SnapshotInfo:
        return await self._cell.snapshot()

    async def kill(self) -> None:
        await self._cell.destroy()

    async def __aenter__(self) -> Sandbox:
        return self

    async def __aexit__(self, *exc: object) -> None:
        try:
            await self.kill()
        finally:
            if getattr(self, "_owns_client", False):
                await self._client.aclose()

    @classmethod
    async def run(
        cls,
        commands: Sequence[str],
        **kwargs: Any,
    ) -> RunResult:
        """One-shot: create → run commands → destroy (delegates to Client.run)."""
        async with Client() as client:
            return await client.run(commands=commands, **kwargs)
