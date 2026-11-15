# cell-mcp

Thin [Model Context Protocol](https://modelcontextprotocol.io/) server over a running `celld`.

Tools: `cell_create`, `cell_exec`, `cell_snapshot`, `cell_kill`, `cell_list`.

## Install

```bash
pip install -e integrations/mcp
# or from repo root with PYTHONPATH
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
