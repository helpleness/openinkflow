import assert from 'node:assert/strict'
import { readFile } from 'node:fs/promises'
import test from 'node:test'
import { createContext, SourceTextModule, SyntheticModule } from 'node:vm'

// Load the real API/session and connection code, replacing only browser surfaces
// and the GPU worker so these regressions do not need model downloads.
async function setup(runtime, origin = 'https://inkflow.example') {
  const sockets = []
  const timers = new Map()
  const listeners = new Map()
  let timerID = 0
  const storage = () => {
    const values = new Map()
    return {
      getItem: (key) => values.get(key) ?? null,
      setItem: (key, value) => values.set(key, String(value)),
      removeItem: (key) => values.delete(key),
    }
  }
  class Socket {
    static OPEN = 1
    readyState = 0
    sent = []
    constructor(url) { this.url = new URL(url); sockets.push(this) }
    send(data) { this.sent.push(JSON.parse(data)) }
    open() { this.readyState = 1; this.onopen?.() }
    close(code = 1006) { this.readyState = 3; this.onclose?.({ code }) }
  }
  const context = createContext({
    __INKFLOW_RUNTIME__: runtime,
    URL, console, WebSocket: Socket,
    localStorage: storage(), sessionStorage: storage(),
    performance: {},
    CustomEvent: class { constructor(type, options) { this.type = type; this.detail = options.detail } },
    location: { origin },
    addEventListener: (type, callback) => listeners.set(type, callback),
    dispatchEvent: (event) => listeners.get(event.type)?.(event),
    setTimeout: (callback, delay) => { timers.set(++timerID, { callback, delay }); return timerID },
    clearTimeout: (id) => timers.delete(id),
    setInterval: () => ++timerID,
    clearInterval: () => {},
  })
  context.window = context
  const modules = new Map()
  async function load(url) {
    if (!modules.has(url.href)) {
      modules.set(url.href, new SourceTextModule(await readFile(url, 'utf8'), { context, identifier: url.href }))
    }
    return modules.get(url.href)
  }
  const entry = await load(new URL('./useLocalAI.js', import.meta.url))
  await entry.link(async (specifier, parent) => {
    if (specifier === 'vue') {
      return new SyntheticModule(['reactive'], function () { this.setExport('reactive', (value) => value) }, { context })
    }
    if (specifier.endsWith('?worker')) {
      return new SyntheticModule(['default'], function () { this.setExport('default', class {}) }, { context })
    }
    return load(new URL(`${specifier}.js`, parent.identifier))
  })
  await entry.evaluate()
  const ai = entry.namespace.useLocalAI()
  ai.localAI.ready = true
  return {
    ai, sockets,
    api: modules.get(new URL('../api.js', import.meta.url).href).namespace,
    runReconnect() {
      const pending = [...timers].find(([, timer]) => timer.delay === 1000)
      assert.ok(pending, 'a reconnect must be scheduled')
      timers.delete(pending[0])
      pending[1].callback()
    },
    timers,
  }
}

for (const origin of ['https://inkflow.example', 'http://127.0.0.1:5173']) {
  test(`cookie session connects without a readable token: ${origin}`, async () => {
    const { ai, api, sockets } = await setup(undefined, origin)
    assert.equal(api.getAuthToken(), '')
    ai.initLocalAI({ tenantID: 7 })
    assert.equal(sockets.length, 1)
    const socket = sockets[0]
    assert.equal(socket.url.origin, origin.replace(/^http/, 'ws'))
    assert.equal(socket.url.pathname, '/api/system/inference/ws')
    assert.equal(socket.url.searchParams.has('token'), false)
    assert.equal(socket.url.searchParams.get('tenant_id'), '7')
    assert.ok(socket.url.searchParams.get('client_id'))
    socket.open()
    assert.equal(ai.localAI.socketStatus, 'connected')
    assert.equal(socket.sent[0].type, 'heartbeat')
  })
}

test('cookie session reconnects and switches tenant without reloading models', async () => {
  const { ai, sockets, runReconnect } = await setup()
  ai.initLocalAI({ tenantID: 7 })
  assert.equal(sockets.length, 1)
  sockets[0].open()
  sockets[0].close()
  runReconnect()
  assert.equal(sockets.length, 2)
  sockets[1].open()
  ai.initLocalAI({ tenantID: 9 })
  assert.equal(sockets[1].readyState, 3)
  assert.equal(sockets[2].url.searchParams.get('tenant_id'), '9')
  assert.equal(ai.localAI.ready, true)
})

test('desktop retains loopback token authentication and reconnects', async () => {
  const { ai, sockets, runReconnect } = await setup({ apiBase: 'http://127.0.0.1:54321', sessionToken: 'desktop-test-token' })
  ai.initLocalAI({ tenantID: 7 })
  assert.equal(sockets.length, 1)
  assert.equal(sockets[0].url.origin, 'ws://127.0.0.1:54321')
  assert.equal(sockets[0].url.pathname, '/system/inference/ws')
  assert.equal(sockets[0].url.searchParams.get('token'), 'desktop-test-token')
  sockets[0].open()
  sockets[0].close()
  runReconnect()
  assert.equal(sockets.length, 2)
})

test('desktop without credentials does not connect', async () => {
  const { ai, sockets } = await setup({ apiBase: 'http://127.0.0.1:54321' })
  ai.initLocalAI({ tenantID: 7 })
  assert.equal(sockets.length, 0)
})

test('cookie session still requires a tenant and stops after session replacement', async () => {
  const { ai, sockets, timers } = await setup()
  ai.initLocalAI({ tenantID: 0 })
  assert.equal(sockets.length, 0)
  assert.equal(ai.localAI.socketStatus, 'waiting-tenant')
  ai.initLocalAI({ tenantID: 7 })
  assert.equal(sockets.length, 1)
  sockets[0].open()
  sockets[0].close(4001)
  assert.equal(ai.localAI.ready, false)
  assert.equal(ai.localAI.socketStatus, 'superseded')
  assert.equal(timers.size, 0)
})
