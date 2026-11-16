# dsh-plugin-cell

[DeepSeek Harness](https://github.com/deepseek-ai/deepseek-harness) bundle that exposes Cell sandboxes as tools.

| Tool | Action |
|------|--------|
| `cell_create` | `POST /v1/cells` |
| `cell_exec` | `POST /v1/cells/{id}/exec` |
| `cell_snapshot` | `POST /v1/cells/{id}/snapshot` |
| `cell_kill` | `DELETE /v1/cells/{id}` |

## Install

```bash
# celld must already be running (unix socket or HTTP)
export CELL_ENDPOINT=unix://$HOME/.cell/celld.sock

# From a local clone (preferred while developing)
dsh plugin add /absolute/path/to/Cell/integrations/dsh-cell

# Or from the v0.8.0 GitHub Release tarball
# https://github.com/YanChao1999/Cell/releases/tag/v0.8.0
curl -fsSL -o dsh-plugin-cell.tgz \
  https://github.com/YanChao1999/Cell/releases/download/v0.8.0/dsh-plugin-cell-0.1.0.tgz
mkdir -p /tmp/dsh-plugin-cell && tar -xzf dsh-plugin-cell.tgz -C /tmp/dsh-plugin-cell --strip-components=1
dsh plugin add /tmp/dsh-plugin-cell

# npm registry publish (`npm publish` as dsh-plugin-cell) needs `npm login` — not done in CI yet
```

Optional Cordis config (profile patch):

```yaml
- id: cell-tools
  config:
    endpoint: "unix:///home/you/.cell/celld.sock"
```

## Layout

```
integrations/dsh-cell/
  package.json          # dsh.bundle.patch
  cordis.patch.yml
  src/index.js          # lifecycle
  src/tools.js          # tool registration
  src/client.js         # celld HTTP/unix client
```

Peer packages `@deepseek-ai/cordis` and `@deepseek-ai/dsh-tools` come from the DSH profile; they are not vendored here.

## Notes

- Thin adapter only — isolation stays in `celld` (Docker or Firecracker).
- For Cursor without DSH, use `integrations/mcp` instead.
