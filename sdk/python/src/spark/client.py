"""Async HTTP client for the Spark daemon Agent API."""

from __future__ import annotations

import os
from pathlib import Path
from typing import Any, Sequence
from urllib.parse import urlparse

import httpx

from spark.types import ExecResult, Limits, RunResult, SparkInfo


def _default_endpoint() -> str:
    return f"unix://{Path.home() / '.spark' / 'sparkd.sock'}"


def _parse_endpoint(endpoint: str) -> tuple[str | None, str]:
    """Return (unix_socket_path | None, http_base_url).

    Unix sockets are addressed as ``unix:///path/to.sock`` (or ``unix://path``).
    HTTP endpoints use a normal URL (e.g. ``http://127.0.0.1:8080``).
    """
    if endpoint.startswith("unix://"):
        path = endpoint[len("unix://") :]
        # Accept both unix:///abs/path and unix://abs/path
        if path.startswith("//"):
            path = path[1:]
        if not path.startswith("/"):
            path = "/" + path
        return path, "http://spark"
    parsed = urlparse(endpoint)
    if parsed.scheme in ("http", "https"):
        return None, endpoint.rstrip("/")
    raise ValueError(
        f"Unsupported SPARK_ENDPOINT scheme: {endpoint!r}. "
        "Use unix:///path/to.sock or http(s)://host:port"
    )


class Spark:
    """Handle for a live Spark environment."""

    def __init__(self, client: Client, info: SparkInfo) -> None:
        self._client = client
        self._info = info

    @property
    def id(self) -> str:
        return self._info.id

    @property
    def worker_id(self) -> str:
        return self._info.worker_id

    @property
    def environment(self) -> str:
        return self._info.environment

    @property
    def status(self) -> str:
        return self._info.status

    @property
    def expires_at(self) -> str:
        return self._info.expires_at

    @property
    def info(self) -> SparkInfo:
        return self._info

    async def exec(
        self,
        command: str,
        *,
        timeout_seconds: int | None = None,
    ) -> ExecResult:
        return await self._client.exec(
            self.id, command, timeout_seconds=timeout_seconds
        )

    async def destroy(self) -> None:
        await self._client.destroy(self.id)

    async def refresh(self) -> SparkInfo:
        self._info = await self._client.get(self.id)
        return self._info

    def __repr__(self) -> str:
        return (
            f"Spark(id={self.id!r}, environment={self.environment!r}, "
            f"status={self.status!r})"
        )


class Client:
    """Async client for the Spark daemon Agent API.

    Connects via ``SPARK_ENDPOINT`` or the default Unix socket
    ``unix://~/.spark/sparkd.sock``.
    """

    def __init__(
        self,
        endpoint: str | None = None,
        *,
        timeout: float = 120.0,
        transport: httpx.AsyncBaseTransport | None = None,
    ) -> None:
        endpoint = endpoint or os.environ.get("SPARK_ENDPOINT") or _default_endpoint()
        uds, base_url = _parse_endpoint(endpoint)
        self._endpoint = endpoint
        self._base_url = base_url
        self._timeout = timeout

        if transport is not None:
            self._transport = transport
            self._owns_transport = False
        elif uds is not None:
            self._transport = httpx.AsyncHTTPTransport(uds=uds)
            self._owns_transport = True
        else:
            self._transport = None
            self._owns_transport = False

        self._http: httpx.AsyncClient | None = None

    @property
    def endpoint(self) -> str:
        return self._endpoint

    async def __aenter__(self) -> Client:
        await self._ensure_http()
        return self

    async def __aexit__(self, *exc: object) -> None:
        await self.aclose()

    async def aclose(self) -> None:
        if self._http is not None:
            await self._http.aclose()
            self._http = None

    async def _ensure_http(self) -> httpx.AsyncClient:
        if self._http is None:
            kwargs: dict[str, Any] = {
                "base_url": self._base_url,
                "timeout": self._timeout,
            }
            if self._transport is not None:
                kwargs["transport"] = self._transport
            self._http = httpx.AsyncClient(**kwargs)
        return self._http

    async def _request(
        self,
        method: str,
        path: str,
        *,
        json: dict[str, Any] | None = None,
    ) -> Any:
        http = await self._ensure_http()
        response = await http.request(method, path, json=json)
        response.raise_for_status()
        if response.status_code == 204 or not response.content:
            return None
        return response.json()

    async def health(self) -> dict[str, Any]:
        data = await self._request("GET", "/health")
        return data if isinstance(data, dict) else {"status": "ok"}

    async def create(
        self,
        *,
        repo: str = "",
        ref: str = "main",
        environment: str = "go-1.23",
        ttl: str = "30m",
        prefer_local: bool = True,
        limits: Limits | dict[str, str] | None = None,
        client_worker_id: str | None = None,
    ) -> Spark:
        body: dict[str, Any] = {
            "repo": repo,
            "ref": ref,
            "environment": environment,
            "ttl": ttl,
            "prefer_local": prefer_local,
        }
        if limits is not None:
            body["limits"] = (
                limits.to_dict() if isinstance(limits, Limits) else dict(limits)
            )
        if client_worker_id is not None:
            body["client_worker_id"] = client_worker_id

        data = await self._request("POST", "/v1/sparks", json=body)
        info = SparkInfo.from_dict(data)
        return Spark(self, info)

    async def exec(
        self,
        spark_id: str,
        command: str,
        *,
        timeout_seconds: int | None = None,
    ) -> ExecResult:
        body: dict[str, Any] = {"command": command}
        if timeout_seconds is not None:
            body["timeout_seconds"] = timeout_seconds
        data = await self._request("POST", f"/v1/sparks/{spark_id}/exec", json=body)
        return ExecResult.from_dict(data)

    async def destroy(self, spark_id: str) -> None:
        await self._request("DELETE", f"/v1/sparks/{spark_id}")

    async def get(self, spark_id: str) -> SparkInfo:
        data = await self._request("GET", f"/v1/sparks/{spark_id}")
        return SparkInfo.from_dict(data)

    async def list(self) -> list[SparkInfo]:
        data = await self._request("GET", "/v1/sparks")
        if isinstance(data, list):
            items = data
        elif isinstance(data, dict):
            items = data.get("sparks") or data.get("items") or []
        else:
            items = []
        return [SparkInfo.from_dict(item) for item in items]

    async def run(
        self,
        *,
        commands: Sequence[str],
        repo: str = "",
        ref: str = "main",
        environment: str = "go-1.23",
        ttl: str = "30m",
        prefer_local: bool = True,
        limits: Limits | dict[str, str] | None = None,
        client_worker_id: str | None = None,
    ) -> RunResult:
        body: dict[str, Any] = {
            "repo": repo,
            "ref": ref,
            "environment": environment,
            "ttl": ttl,
            "prefer_local": prefer_local,
            "commands": list(commands),
        }
        if limits is not None:
            body["limits"] = (
                limits.to_dict() if isinstance(limits, Limits) else dict(limits)
            )
        if client_worker_id is not None:
            body["client_worker_id"] = client_worker_id

        data = await self._request("POST", "/v1/run", json=body)
        return RunResult.from_dict(data)
