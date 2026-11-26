"""Stdio MCP server: thin adapter over the Cell Agent HTTP API."""

from __future__ import annotations

import base64
import io
import os
import shlex
import tarfile
import time
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
    background: bool = False,
    wait: bool = False,
    cpu: str = "",
    memory: str = "",
    disk: str = "",
) -> dict[str, Any]:
    """Create an ephemeral Cell sandbox.

    Set background=True to return immediately (status=creating); poll with
    cell_wait / cell_get. Set wait=True to block until ready (uses async create).
    Omit repo (empty) then cell_sync to push a local dirty tree.
    Optional cpu/memory/disk override environment default_limits.
    """
    body: dict[str, Any] = {
        "environment": environment,
        "repo": repo,
        "ref": ref,
        "ttl": ttl,
        "prefer_local": True,
        "async": bool(background or wait),
    }
    if snapshot_id:
        body["snapshot_id"] = snapshot_id
    limits: dict[str, str] = {}
    if cpu:
        limits["cpu"] = cpu
    if memory:
        limits["memory"] = memory
    if disk:
        limits["disk"] = disk
    if limits:
        body["limits"] = limits
    data = _request("POST", "/v1/cells", json=body)
    if not isinstance(data, dict):
        return {"raw": data}
    if wait and data.get("id"):
        return cell_wait(str(data["id"]))
    return data


@mcp.tool()
def cell_get(cell_id: str) -> dict[str, Any]:
    """Get Cell status / timings (poll while status is creating/fetching/starting/setup)."""
    data = _request("GET", f"/v1/cells/{cell_id}")
    return data if isinstance(data, dict) else {"raw": data}


@mcp.tool()
def cell_wait(cell_id: str, timeout_seconds: int = 600) -> dict[str, Any]:
    """Poll until Cell status is ready or error. Returns final metadata."""
    deadline = time.time() + max(1, timeout_seconds)
    last: dict[str, Any] = {}
    while time.time() < deadline:
        last = cell_get(cell_id)
        status = str(last.get("status") or "")
        if status in ("ready", "error"):
            if status == "error":
                raise RuntimeError(
                    f"cell {cell_id} create failed: {last.get('error') or 'unknown'}"
                )
            return last
        time.sleep(0.4)
    raise TimeoutError(
        f"cell {cell_id} not ready after {timeout_seconds}s (last={last.get('status')})"
    )


@mcp.tool()
def cell_sync(cell_id: str, local_path: str = ".") -> dict[str, Any]:
    """Upload a local directory into the Cell workspace (gzip tar).

    Use after cell_create with empty repo to work on unpushed / dirty trees.
    Replaces workspace contents (same semantics as snapshot restore unpack).
    """
    root = Path(local_path).expanduser().resolve()
    if not root.is_dir():
        raise ValueError(f"local_path is not a directory: {root}")
    buf = io.BytesIO()
    with tarfile.open(fileobj=buf, mode="w:gz") as tar:
        for path in root.rglob("*"):
            if not path.is_file() and not path.is_symlink():
                if path.is_dir():
                    continue
                continue
            rel = path.relative_to(root).as_posix()
            # Skip heavy / irrelevant VCS objects; keep working tree + .git metadata light.
            if rel == ".git" or rel.startswith(".git/objects/"):
                continue
            tar.add(path, arcname=rel, recursive=False)
        # Ensure empty dirs that matter are not required; files cover agent use.
    payload = buf.getvalue()
    with _client() as client:
        resp = client.put(
            f"/v1/cells/{cell_id}/workspace",
            content=payload,
            headers={"Content-Type": "application/gzip"},
        )
        resp.raise_for_status()
    return {"cell_id": cell_id, "bytes": len(payload), "local_path": str(root)}


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


def _workspace_rel(path: str) -> str:
    """Normalize to a workspace-relative path; reject ``..`` escapes."""
    p = path.replace("\\", "/").lstrip("/")
    if p.startswith("workspace/"):
        p = p[len("workspace/") :]
    if ".." in p.split("/"):
        raise ValueError(f"path escapes workspace: {path!r}")
    return p or "."


def _exec(cell_id: str, command: str) -> dict[str, Any]:
    data = _request(
        "POST",
        f"/v1/cells/{cell_id}/exec",
        json={"command": command},
    )
    return data if isinstance(data, dict) else {"raw": data}


@mcp.tool()
def cell_write(cell_id: str, path: str, content: str) -> str:
    """Write a UTF-8 text file inside the Cell workspace (creates parent dirs)."""
    rel = _workspace_rel(path)
    payload = base64.b64encode(content.encode("utf-8")).decode("ascii")
    cmd = (
        f"mkdir -p $(dirname -- {shlex.quote(rel)}) && "
        f"printf %s {shlex.quote(payload)} | base64 -d > {shlex.quote(rel)}"
    )
    result = _exec(cell_id, cmd)
    if int(result.get("exit_code") or 0) != 0:
        raise RuntimeError(
            f"cell_write({path!r}) failed: {result.get('stderr') or result.get('stdout')}"
        )
    return f"wrote {rel} ({len(content)} bytes)"


@mcp.tool()
def cell_read(cell_id: str, path: str) -> str:
    """Read a UTF-8 text file from the Cell workspace."""
    rel = _workspace_rel(path)
    result = _exec(cell_id, f"base64 {shlex.quote(rel)}")
    if int(result.get("exit_code") or 0) != 0:
        raise FileNotFoundError(
            f"cell_read({path!r}): {result.get('stderr') or result.get('stdout')}"
        )
    raw = base64.b64decode("".join(str(result.get("stdout") or "").split()))
    return raw.decode("utf-8")


@mcp.tool()
def cell_ls(cell_id: str, path: str = ".") -> list[str]:
    """List entries in a Cell workspace directory (non-recursive)."""
    rel = _workspace_rel(path)
    result = _exec(cell_id, f"ls -1A -- {shlex.quote(rel)} | sort")
    if int(result.get("exit_code") or 0) != 0:
        raise FileNotFoundError(
            f"cell_ls({path!r}): {result.get('stderr') or result.get('stdout')}"
        )
    return [line for line in str(result.get("stdout") or "").splitlines() if line]


def main() -> None:
    mcp.run(transport="stdio")


if __name__ == "__main__":
    main()
