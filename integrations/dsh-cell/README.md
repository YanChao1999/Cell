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

dsh plugin add /absolute/path/to/Cell/integrations/dsh-cell
# or from a published package name once published to npm
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
