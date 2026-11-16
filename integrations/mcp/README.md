# cell-mcp

Thin [Model Context Protocol](https://modelcontextprotocol.io/) server over a running `celld`.

Tools: `cell_create`, `cell_exec`, `cell_snapshot`, `cell_kill`, `cell_list`.

## Install

```bash
# Editable (dev)
pip install -e integrations/mcp

# From the v0.8.0 GitHub Release wheel
pip install https://github.com/YanChao1999/Cell/releases/download/v0.8.0/cell_mcp-0.1.0-py3-none-any.whl

# Pin MCP v1 (FastMCP); mcp 2.x renames FastMCP → MCPServer
# dependencies: httpx, mcp>=1.0,<2
```

Requires a reachable Cell daemon (`CELL_ENDPOINT`, default `unix://~/.cell/celld.sock`).

## Cursor `mcp.json`

```json
{
  "mcpServers": {
    "cell": {
      "command": "cell-mcp",
      "env": {
        "CELL_ENDPOINT": "unix:///home/YOU/.cell/celld.sock"
      }
    }
  }
}
```

## Run

```bash
cell-mcp
```

stdio transport (default for Cursor / Claude Desktop).
