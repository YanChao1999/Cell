import http from 'node:http'
import https from 'node:https'
import os from 'node:os'
import path from 'node:path'

function parseEndpoint(endpoint) {
  const raw = (endpoint || process.env.CELL_ENDPOINT || '').trim()
  if (!raw || raw.startsWith('unix://')) {
    let sock = raw ? raw.slice('unix://'.length) : path.join(os.homedir(), '.cell', 'celld.sock')
    if (sock.startsWith('//')) sock = sock.slice(1)
    if (!sock.startsWith('/')) sock = '/' + sock
    return { socketPath: sock, host: 'cell' }
  }
  const u = new URL(raw)
  return {
    hostname: u.hostname,
    port: u.port || (u.protocol === 'https:' ? 443 : 80),
    protocol: u.protocol,
  }
}

function authHeaders() {
  const token = (process.env.CELL_AGENT_TOKEN || process.env.CELL_WORKER_TOKEN || '').trim()
  if (!token) return {}
  return { Authorization: `Bearer ${token}` }
}

/**
 * Minimal HTTP(S)/unix client for the Cell Agent API.
 * @param {string} [endpoint]
 */
export function createCellClient(endpoint) {
  const target = parseEndpoint(endpoint)

  function request(method, apiPath, body) {
    const payload = body === undefined ? null : JSON.stringify(body)
    const headers = { Accept: 'application/json', ...authHeaders() }
    if (payload !== null) {
      headers['Content-Type'] = 'application/json'
      headers['Content-Length'] = Buffer.byteLength(payload)
    }
    const opts = {
      method,
      path: apiPath,
      headers,
    }
    if (target.socketPath) {
      opts.socketPath = target.socketPath
      opts.host = 'localhost'
    } else {
      opts.hostname = target.hostname
      opts.port = target.port
    }
    const transport = target.protocol === 'https:' ? https : http
    return new Promise((resolve, reject) => {
      const req = transport.request(opts, (res) => {
        const chunks = []
        res.on('data', (c) => chunks.push(c))
        res.on('end', () => {
          const raw = Buffer.concat(chunks).toString('utf8')
          if (res.statusCode && res.statusCode >= 400) {
            reject(new Error(`cell ${method} ${apiPath}: ${res.statusCode} ${raw}`))
            return
          }
          if (!raw || res.statusCode === 204) {
            resolve(null)
            return
          }
          try {
            resolve(JSON.parse(raw))
          } catch {
            resolve(raw)
          }
        })
      })
      req.on('error', reject)
      if (payload !== null) req.write(payload)
      req.end()
    })
  }

  return {
    create: (body) => request('POST', '/v1/cells', body),
    exec: (id, body) => request('POST', `/v1/cells/${id}/exec`, body),
    snapshot: (id) => request('POST', `/v1/cells/${id}/snapshot`),
    kill: (id) => request('DELETE', `/v1/cells/${id}`),
    list: () => request('GET', '/v1/cells'),
  }
}
