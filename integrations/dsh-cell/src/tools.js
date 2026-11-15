import { createRequire } from 'node:module'
import { createCellClient } from './client.js'

export const name = 'dsh-plugin-cell-tools'
export const inject = ['tools']

const require = createRequire(import.meta.url)

/**
 * Register Cell sandbox tools on the DSH tool registry.
 * Cordis config.endpoint (optional) overrides CELL_ENDPOINT.
 */
export function apply(ctx, config = {}) {
  const client = createCellClient(config.endpoint || '')
  const tools = ctx.tools
  if (!tools?.register) {
    ctx?.logger?.warn?.('[dsh-plugin-cell] tools service missing; skip registration')
    return
  }

  let defineTool = null
  try {
    defineTool = require('@deepseek-ai/dsh-tools').defineTool
  } catch {
    // Peer not installed outside a DSH profile — fall back to plain objects.
  }

  const specs = [
    {
      name: 'cell_create',
      description: 'Create an ephemeral Cell sandbox for untrusted agent work.',
      parameters: {
        type: 'object',
        properties: {
          environment: { type: 'string', default: 'python-3.12' },
          repo: { type: 'string' },
          ref: { type: 'string', default: 'main' },
          ttl: { type: 'string', default: '30m' },
          snapshot_id: { type: 'string' },
        },
      },
      execute: async (args = {}) =>
        client.create({
          environment: args.environment || 'python-3.12',
          repo: args.repo || '',
          ref: args.ref || 'main',
          ttl: args.ttl || '30m',
          prefer_local: true,
          ...(args.snapshot_id ? { snapshot_id: args.snapshot_id } : {}),
        }),
    },
    {
      name: 'cell_exec',
      description: 'Run a command inside a Cell.',
      parameters: {
        type: 'object',
        required: ['cell_id', 'command'],
        properties: {
          cell_id: { type: 'string' },
          command: { type: 'string' },
          timeout_seconds: { type: 'number' },
        },
      },
      execute: async (args = {}) => {
        const body = { command: args.command }
        if (args.timeout_seconds) body.timeout_seconds = args.timeout_seconds
        return client.exec(args.cell_id, body)
      },
    },
    {
      name: 'cell_snapshot',
      description: 'Snapshot a Cell workspace for later restore.',
      parameters: {
        type: 'object',
        required: ['cell_id'],
        properties: { cell_id: { type: 'string' } },
      },
      execute: async (args = {}) => client.snapshot(args.cell_id),
    },
    {
      name: 'cell_kill',
      description: 'Destroy a Cell.',
      parameters: {
        type: 'object',
        required: ['cell_id'],
        properties: { cell_id: { type: 'string' } },
      },
      execute: async (args = {}) => {
        await client.kill(args.cell_id)
        return { destroyed: args.cell_id }
      },
    },
  ]

  for (const spec of specs) {
    if (typeof defineTool === 'function') {
      tools.register(
        defineTool({
          name: spec.name,
          description: spec.description,
          parameters: spec.parameters,
          async execute(args) {
            return spec.execute(args)
          },
        }),
      )
    } else {
      tools.register({
        name: spec.name,
        description: spec.description,
        parameters: spec.parameters,
        execute: spec.execute,
      })
    }
  }
}
