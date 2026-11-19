"""Stdio MCP server: thin adapter over the Cell Agent HTTP API."""

from __future__ import annotations

import os
from pathlib import Path
from typing import Any
from urllib.parse import urlparse

import httpx
from mcp.server.fastmcp import FastMCP

mcp = FastMCP("cell")


def _default_endpoint() -> str:
    return f"unix://{Path.home() / '.cell' / 'celld.sock'}"


def _parse_endpoint(endpoint: str) -> tuple[str | None, str]:
    if endpoint.startswith("unix://"):
        path = endpoint[len("unix://") :]
        if path.startswith("//"):
            path = path[1:]
        if not path.startswith("/"):
            path = "/" + path
        return path, "http://cell"
    parsed = urlparse(endpoint)
    if parsed.scheme in ("http", "https"):
        return None, endpoint.rstrip("/")
    raise ValueError(f"Unsupported CELL_ENDPOINT: {endpoint!r}")


def _auth_headers() -> dict[str, str]:
    token = (os.environ.get("CELL_AGENT_TOKEN") or os.environ.get("CELL_WORKER_TOKEN") or "").strip()
    if not token:
        return {}
    return {"Authorization": f"Bearer {token}"}


def _client() -> httpx.Client:
    endpoint = os.environ.get("CELL_ENDPOINT") or _default_endpoint()
    uds, base = _parse_endpoint(endpoint)
    # Bound hangs; operators can still raise via httpx defaults if needed.
    timeout = httpx.Timeout(600.0, connect=10.0)
    headers = _auth_headers()
    if uds:
        transport = httpx.HTTPTransport(uds=uds)
        return httpx.Client(
            transport=transport, base_url=base, timeout=timeout, headers=headers
        )
    return httpx.Client(base_url=base, timeout=timeout, headers=headers)


def _request(method: str, path: str, **kwargs: Any) -> Any:
    with _client() as client:
        resp = client.request(method, path, **kwargs)
        resp.raise_for_status()
        if resp.status_code == 204 or not resp.content:
            return None
        return resp.json()


@mcp.tool()
def cell_create(
    environment: str = "python-3.12",
    repo: str = "",
    ref: str = "main",
    ttl: str = "30m",
    snapshot_id: str = "",
) -> dict[str, Any]:
    """Create an ephemeral Cell sandbox. Returns cell id and metadata."""
    body: dict[str, Any] = {
        "environment": environment,
        "repo": repo,
        "ref": ref,
        "ttl": ttl,
        "prefer_local": True,
    }
    if snapshot_id:
        body["snapshot_id"] = snapshot_id
    data = _request("POST", "/v1/cells", json=body)
    return data if isinstance(data, dict) else {"raw": data}


@mcp.tool()
def cell_exec(cell_id: str, command: str, timeout_seconds: int = 0) -> dict[str, Any]:
    """Run a shell command inside a Cell. Returns exit_code, stdout, stderr."""
    body: dict[str, Any] = {"command": command}
    if timeout_seconds > 0:
        body["timeout_seconds"] = timeout_seconds
    data = _request("POST", f"/v1/cells/{cell_id}/exec", json=body)
    return data if isinstance(data, dict) else {"raw": data}


@mcp.tool()
def cell_snapshot(cell_id: str) -> dict[str, Any]:
    """Snapshot a Cell workspace; returns snapshot id for later restore."""
    data = _request("POST", f"/v1/cells/{cell_id}/snapshot")
    return data if isinstance(data, dict) else {"raw": data}


@mcp.tool()
def cell_kill(cell_id: str) -> str:
    """Destroy a Cell."""
    _request("DELETE", f"/v1/cells/{cell_id}")
    return f"destroyed {cell_id}"


@mcp.tool()
def cell_list() -> list[Any]:
    """List live Cells."""
    data = _request("GET", "/v1/cells")
    if isinstance(data, list):
        return data
    if isinstance(data, dict):
        return list(data.get("cells") or data.get("items") or [])
    return []


def main() -> None:
    mcp.run(transport="stdio")


if __name__ == "__main__":
    main()
