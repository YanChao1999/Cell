import assert from 'node:assert/strict'
import { createCellClient } from './client.js'

// Smoke: client factory does not throw with default unix endpoint.
const c = createCellClient('')
assert.equal(typeof c.create, 'function')
assert.equal(typeof c.exec, 'function')
assert.equal(typeof c.snapshot, 'function')
assert.equal(typeof c.kill, 'function')
console.log('dsh-cell client ok')
