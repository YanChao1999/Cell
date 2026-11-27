# cell-mcp

Thin [Model Context Protocol](https://modelcontextprotocol.io/) server over a running `celld`.

Tools: `cell_create`, `cell_exec`, `cell_read`, `cell_write`, `cell_ls`, `cell_snapshot`, `cell_kill`, `cell_list`.

## Install

```bash
# Editable (dev)
pip install -e integrations/mcp

# From the v0.8.0 GitHub Release wheel (verify checksum first)
curl -fsSL -O https://github.com/YanChao1999/Cell/releases/download/v0.8.0/cell_mcp-0.1.0-py3-none-any.whl
echo '6592deb93c720fd3b702bf0d9cecbdbf08516cb5301c6729f1e92c354c0c57f2  cell_mcp-0.1.0-py3-none-any.whl' | sha256sum -c
pip install ./cell_mcp-0.1.0-py3-none-any.whl
# Full list: docs/ops/release-assets-v0.8.0.sha256

# Pin MCP v1 (FastMCP); mcp 2.x renames FastMCP → MCPServer
# dependencies: httpx, mcp>=1.0,<2
```

Requires a reachable Cell daemon (`CELL_ENDPOINT`, default `unix://~/.cell/celld.sock`).
TCP `--listen-http` requires `Authorization: Bearer` (`CELL_AGENT_TOKEN` or `CELL_WORKER_TOKEN`).

## Cursor `mcp.json`

Project file [`.cursor/mcp.json`](../../.cursor/mcp.json) points at the local Docker compose control plane:

```json
{
  "mcpServers": {
    "cell": {
      "command": "/home/YOU/.cell/mcp-venv/bin/cell-mcp",
      "env": {
        "CELL_ENDPOINT": "http://127.0.0.1:18080",
        "CELL_WORKER_TOKEN": "cell-dev-token"
      }
    }
  }
}
```

Install once: `uv venv ~/.cell/mcp-venv && uv pip install --python ~/.cell/mcp-venv/bin/python -e integrations/mcp 'mcp>=1.0,<2'`.  
Then **Cursor → MCP → refresh/reload** so tools `cell_create` / `cell_exec` / … appear.

## Run

```bash
cell-mcp
```

stdio transport (default for Cursor / Claude Desktop).
