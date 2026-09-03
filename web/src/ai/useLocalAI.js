import { reactive } from 'vue'
import { AUTH_SESSION_REPLACED_EVENT, getAPIBase, getAuthToken, revokeAuthTokenForTab } from '../api'
import { getInferenceClientId } from './inferenceClient'
import AIWorker from './ai-worker.js?worker'

function createModelDownloadState() {
  return { active: false, state: 'idle', file: '', received: 0, total: 0, progress: 0, error: '' }
}

export const localAI = reactive({
  ready: false,
  status: '未初始化；首次启动会下载并缓存到浏览器本地',
  socketStatus: 'disconnected',
  progress: 0,
  memory: {
    supported: false,
    used: 0,
    limit: 0,
    percent: 0,
    warning: false,
  },
  models: {
    embedding: 'onnx-community/Qwen3-Embedding-0.6B-ONNX',
    rerank: 'onnx-community/bge-reranker-v2-m3-ONNX',
  },
  cache: {
    embedding: null,
    rerank: null,
  },
  downloads: {
    embedding: createModelDownloadState(),
    rerank: createModelDownloadState(),
  },
})

let aiWorker = null
let inferenceSocket = null
let inferenceSocketTenantID = 0
let reconnectTimer = 0
let heartbeatTimer = 0
let heartbeatTimeoutTimer = 0
let memoryTimer = 0
let reconnectAttempt = 0
let initializing = false
let messageIdCounter = 0
let lastMemoryReleaseAt = 0
let lastInitOptions = {}
let frontendInferenceQueue = Promise.resolve()
let persistentStoragePromise = null
const workerCallbacks = new Map()
const REQUEST_TIMEOUTS = {
  init: 900000,
  embed: 60000,
  rerank: 25000,
  'release-unused': 30000,
  'clear-cache': 30000,
}
const HEARTBEAT_INTERVAL_MS = 15000
const HEARTBEAT_TIMEOUT_MS = 45000
const RECONNECT_MAX_DELAY_MS = 30000
const SESSION_REPLACED_CLOSE_CODE = 4001

function resetModelDownloadStates() {
  for (const kind of ['embedding', 'rerank']) {
    Object.assign(localAI.downloads[kind], createModelDownloadState())
  }
}

function updateModelDownloadState(info = {}) {
  const kind = String(info.kind || '')
  const target = localAI.downloads[kind]
  if (!target) return

  const state = String(info.state || 'downloading')
  const total = Math.max(0, Number(info.total || 0))
  const received = Math.max(0, Number(info.received || 0))
  const reportedProgress = Number(info.progress)
  const progress = Number.isFinite(reportedProgress)
    ? Math.max(0, Math.min(100, Math.round(reportedProgress)))
    : total > 0 ? Math.max(0, Math.min(100, Math.round((received / total) * 100))) : 0

  Object.assign(target, {
    active: state === 'checking' || state === 'downloading',
    state,
    file: String(info.file || target.file || ''),
    received,
    total,
    progress: state === 'complete' || state === 'cached' ? 100 : progress,
    error: String(info.error || ''),
  })
  if (state === 'downloading' && progress > 0) localAI.progress = progress
}

function completeModelDownload(kind) {
  const target = localAI.downloads[kind]
  if (!target || (!target.active && target.state !== 'checking')) return
  Object.assign(target, { active: false, state: 'complete', progress: 100, error: '' })
}

function ensurePersistentLocalModelStorage() {
  if (persistentStoragePromise) return persistentStoragePromise
  persistentStoragePromise = (async () => {
    const storage = window.navigator?.storage
    if (!storage?.persist) return false
    try {
      if (await storage.persisted?.()) return true
      return Boolean(await storage.persist())
    } catch {
      return false
    }
  })()
  return persistentStoragePromise
}

function setupWorker() {
  if (aiWorker) return

  aiWorker = new AIWorker()

  aiWorker.addEventListener('message', (event) => {
    const { type, message, info, id, result, error, models, cache } = event.data

    if (type === 'status') {
      localAI.status = message
    } else if (type === 'progress') {
      if (info.status === 'progress') {
        localAI.progress = Math.round(info.progress)
      }
    } else if (type === 'model-download') {
      updateModelDownloadState(info)
    } else if (type === 'ready') {
      localAI.ready = true
      initializing = false
      localAI.progress = 100
      completeModelDownload('embedding')
      if (cache) localAI.cache = { ...localAI.cache, ...cache }
      localAI.status = models
        ? `Local models ready: ${models.embedding} / ${models.rerank}`
        : 'Local models ready (WebGPU)'
      connectInferenceSocket()
    } else if (type === 'rerank-ready' || type === 'rerank-unloaded') {
      if (cache) localAI.cache = { ...localAI.cache, ...cache }
      if (type === 'rerank-ready') completeModelDownload('rerank')
      if (models) {
        localAI.status = `Local models ready: ${models.embedding} / ${models.rerank}`
      }
    } else if (type === 'result') {
      if (workerCallbacks.has(id)) {
        const callback = workerCallbacks.get(id)
        window.clearTimeout(callback.timer)
        callback.resolve(result)
        workerCallbacks.delete(id)
      }
    } else if (type === 'error') {
      if (id && workerCallbacks.has(id)) {
        const callback = workerCallbacks.get(id)
        window.clearTimeout(callback.timer)
        callback.reject(new Error(error))
        workerCallbacks.delete(id)
        if (/timeout|WebGPU|GPU|session\.run/i.test(String(error || ''))) {
          restartLocalWorker(`Local AI worker reset after error: ${error}`)
        }
      } else {
        initializing = false
        localAI.status = `Local AI error: ${error}`
        console.error(`Local AI error: ${error}`)
      }
    }
  })
}

function requestWorker(type, payload) {
  return new Promise((resolve, reject) => {
    if (!localAI.ready && type !== 'init' && type !== 'clear-cache') return reject(new Error('Local models are not ready'))
    const id = ++messageIdCounter
    const timeoutMs = REQUEST_TIMEOUTS[type] || 60000
    const timer = window.setTimeout(() => {
      if (!workerCallbacks.has(id)) return
      workerCallbacks.delete(id)
      reject(new Error(`${type} timeout after ${timeoutMs}ms`))
      restartLocalWorker(`Local AI worker reset after ${type} timeout`)
    }, timeoutMs)
    workerCallbacks.set(id, { resolve, reject, timer })
    aiWorker.postMessage({ type, id, payload })
  })
}

function requestInferenceWorker(type, payload) {
  const run = frontendInferenceQueue.catch(() => {}).then(() => requestWorker(type, payload))
  frontendInferenceQueue = run.finally(() => {})
  return run
}

function rejectPendingWorkerRequests(error) {
  for (const callback of workerCallbacks.values()) {
    window.clearTimeout(callback.timer)
    callback.reject(error)
  }
  workerCallbacks.clear()
}

function restartLocalWorker(reason) {
  localAI.ready = false
  initializing = false
  localAI.status = reason
  window.clearTimeout(reconnectTimer)
  stopSocketHeartbeat()
  inferenceSocket?.close()
  inferenceSocket = null
  inferenceSocketTenantID = 0
  rejectPendingWorkerRequests(new Error(reason))
  try {
    aiWorker?.terminate()
  } catch {
    // Worker may already be gone.
  }
  aiWorker = null
  window.setTimeout(() => startLocalAI(lastInitOptions), 500)
}

function websocketURL() {
  const apiBase = getAPIBase() || window.location.origin
  const endpoint = new URL(getAPIBase() ? '/system/inference/ws' : '/api/system/inference/ws', apiBase)
  endpoint.protocol = endpoint.protocol === 'https:' ? 'wss:' : 'ws:'
  endpoint.searchParams.set('token', getAuthToken())
  endpoint.searchParams.set('client_id', getInferenceClientId())
  endpoint.searchParams.set('tenant_id', String(Number(lastInitOptions.tenantID || 0)))
  return endpoint.toString()
}

function connectInferenceSocket() {
  if (!localAI.ready || !getAuthToken()) return
  const tenantID = Number(lastInitOptions.tenantID)
  if (!Number.isInteger(tenantID) || tenantID <= 0) {
    localAI.socketStatus = 'waiting-tenant'
    return
  }
  if (inferenceSocket && inferenceSocketTenantID === tenantID && inferenceSocket.readyState <= WebSocket.OPEN) return

  // 切换租户后必须创建带新 tenant_id 的连接；否则服务端仍按旧租户执行权限校验。
  if (inferenceSocket) {
    const previousSocket = inferenceSocket
    inferenceSocket = null
    inferenceSocketTenantID = 0
    previousSocket.close()
  }

  window.clearTimeout(reconnectTimer)
  stopSocketHeartbeat()
  localAI.socketStatus = 'connecting'
  const socket = new WebSocket(websocketURL())
  inferenceSocket = socket
  inferenceSocketTenantID = tenantID

  socket.onopen = () => {
    reconnectAttempt = 0
    localAI.socketStatus = 'connected'
    startSocketHeartbeat(socket)
  }
  socket.onclose = (event) => {
    if (inferenceSocket !== socket) return
    stopSocketHeartbeat()
    inferenceSocket = null
    inferenceSocketTenantID = 0
    if (event.code === SESSION_REPLACED_CLOSE_CODE) {
      revokeAuthTokenForTab()
      return
    }
    localAI.socketStatus = 'disconnected'
    if (!localAI.ready || !getAuthToken()) return
    const delay = Math.min(RECONNECT_MAX_DELAY_MS, 1000 * 2 ** reconnectAttempt)
    reconnectAttempt += 1
    reconnectTimer = window.setTimeout(connectInferenceSocket, delay)
  }
  socket.onerror = () => {
    if (inferenceSocket !== socket) return
    localAI.socketStatus = 'error'
    if (socket.readyState === WebSocket.OPEN) socket.close()
  }
  socket.onmessage = async (event) => {
    const message = JSON.parse(event.data)
    if (message?.type === 'pong' || message?.type === 'heartbeat') {
      markSocketAlive()
      return
    }
    if (!message?.id || !message.type) return
    try {
      let result
      if (message.type === 'embed') {
        localAI.status = 'Running local embedding...'
        const workerStartedAt = performance.now()
        result = await requestInferenceWorker('embed', { text: message.payload?.text || '' })
        const timings = result?.timings || {}
        timings.browser_worker_ms = Math.round((performance.now() - workerStartedAt) * 100) / 100
        result = { ...(result || {}), timings }
        localAI.status = formatEmbeddingTimingStatus(timings)
        console.info('[InkFlow local embedding timings]', timings)
      } else if (message.type === 'rerank') {
        const docs = Array.isArray(message.payload?.docs) ? message.payload.docs : []
        localAI.status = `Running local rerank for ${docs.length} docs...`
        const workerStartedAt = performance.now()
        result = await requestInferenceWorker('rerank', {
          query: message.payload?.query || '',
          docs,
          top_n: Number(message.payload?.top_n || 0),
        })
        const timings = result?.timings || {}
        timings.browser_worker_ms = Math.round((performance.now() - workerStartedAt) * 100) / 100
        result = { ...(result || {}), timings }
        localAI.status = formatRerankTimingStatus(timings)
        console.info('[InkFlow local rerank timings]', timings)
      } else {
        return
      }
      sendSocketResponse({ type: 'result', id: message.id, result })
    } catch (error) {
      sendSocketResponse({ type: 'error', id: message.id, error: error.message })
    }
  }
}

function stopLocalAIForSessionReplacement() {
  localAI.ready = false
  initializing = false
  reconnectAttempt = 0
  window.clearTimeout(reconnectTimer)
  reconnectTimer = 0
  stopSocketHeartbeat()
  const socket = inferenceSocket
  inferenceSocket = null
  inferenceSocketTenantID = 0
  try {
    socket?.close()
  } catch {}
  rejectPendingWorkerRequests(new Error('会话已被新的窗口替换'))
  try {
    aiWorker?.terminate()
  } catch {}
  aiWorker = null
  stopMemoryMonitor()
  localAI.socketStatus = 'superseded'
  localAI.status = '当前账号已在新的窗口登录，本窗口已退出'
}

window.addEventListener(AUTH_SESSION_REPLACED_EVENT, stopLocalAIForSessionReplacement)

function formatEmbeddingTimingStatus(timings) {
  if (timings.cache_hit) return `Embedding cache hit ${timings.total_ms || 0}ms`
  return `Embedding: cache ${timings.cache_lookup_ms || 0}ms, tokenize ${timings.tokenizer_ms || 0}ms, GPU ${timings.session_run_ms || 0}ms, pool ${timings.pooling_ms || 0}ms, Worker ${timings.browser_worker_ms || 0}ms`
}

function formatRerankTimingStatus(timings) {
  if (timings.fallback) return `Rerank fallback after ${timings.total_ms || 0}ms`
  return `Rerank: load ${timings.model_load_ms || 0}ms, tokenize ${timings.tokenizer_ms || 0}ms, GPU ${timings.session_run_ms || 0}ms, Worker ${timings.browser_worker_ms || 0}ms`
}

function startSocketHeartbeat(socket) {
  sendSocketHeartbeat(socket)
  heartbeatTimer = window.setInterval(() => sendSocketHeartbeat(socket), HEARTBEAT_INTERVAL_MS)
}

function stopSocketHeartbeat() {
  window.clearInterval(heartbeatTimer)
  window.clearTimeout(heartbeatTimeoutTimer)
  heartbeatTimer = 0
  heartbeatTimeoutTimer = 0
}

function markSocketAlive() {
  window.clearTimeout(heartbeatTimeoutTimer)
  heartbeatTimeoutTimer = 0
}

function sendSocketHeartbeat(socket) {
  if (inferenceSocket !== socket || socket.readyState !== WebSocket.OPEN) return
  try {
    socket.send(JSON.stringify({ type: 'heartbeat', ts: Date.now() }))
    if (!heartbeatTimeoutTimer) {
      heartbeatTimeoutTimer = window.setTimeout(() => {
        if (inferenceSocket === socket && socket.readyState === WebSocket.OPEN) {
          localAI.socketStatus = 'timeout'
          socket.close()
        }
      }, HEARTBEAT_TIMEOUT_MS)
    }
  } catch {
    socket.close()
  }
}

function updateMemoryUsage() {
  const memory = performance?.memory
  if (!memory) {
    localAI.memory = { supported: false, used: 0, limit: 0, percent: 0, warning: false }
    return
  }
  const used = Number(memory.usedJSHeapSize || 0)
  const limit = Number(memory.jsHeapSizeLimit || 0)
  const percent = limit > 0 ? Math.round((used / limit) * 100) : 0
  localAI.memory = {
    supported: true,
    used,
    limit,
    percent,
    warning: percent >= 80,
  }
  if (localAI.ready && aiWorker && percent >= 80 && Date.now() - lastMemoryReleaseAt > 30000) {
    lastMemoryReleaseAt = Date.now()
    requestWorker('release-unused', {}).catch(() => {})
  }
}

function startMemoryMonitor() {
  updateMemoryUsage()
  if (memoryTimer) return
  memoryTimer = window.setInterval(updateMemoryUsage, 5000)
}

function stopMemoryMonitor() {
  window.clearInterval(memoryTimer)
  memoryTimer = 0
  updateMemoryUsage()
}

function sendSocketResponse(payload) {
  if (inferenceSocket?.readyState !== WebSocket.OPEN) {
    connectInferenceSocket()
    return
  }
  try {
    inferenceSocket.send(JSON.stringify(payload))
  } catch {
    inferenceSocket.close()
    connectInferenceSocket()
  }
}

async function startLocalAI(options = {}) {
  // 同一个页面切换组织时只更新 WebSocket 的租户上下文，不重新下载或加载模型。
  lastInitOptions = { ...lastInitOptions, ...options }
  if (localAI.ready) {
    connectInferenceSocket()
    return
  }
  if (initializing) return
  initializing = true
  localAI.progress = 0
  resetModelDownloadStates()
  localAI.status = '正在申请浏览器本地持久化存储...'
  const storagePersisted = await ensurePersistentLocalModelStorage()
  if (!initializing) return
  localAI.status = storagePersisted
    ? '正在从浏览器本地持久化缓存初始化模型...'
    : '正在从浏览器缓存初始化模型（浏览器可能在空间不足时清理缓存）...'
  setupWorker()
  startMemoryMonitor()
  requestWorker('init', { ...localAI.models, ...options, apiBase: getAPIBase() }).catch((error) => {
    initializing = false
    localAI.status = `Local AI init failed: ${error.message}`
  })
}

export function useLocalAI() {
  function initLocalAI(options = {}) {
    startLocalAI(options)
  }

  async function clearLocalModelCache() {
    setupWorker()
    localAI.ready = false
    localAI.progress = 0
    resetModelDownloadStates()
    window.clearTimeout(reconnectTimer)
    stopSocketHeartbeat()
    inferenceSocket?.close()
    inferenceSocket = null
    inferenceSocketTenantID = 0
    await requestWorker('clear-cache', {})
    localAI.cache = { embedding: null, rerank: null }
    localAI.status = 'Local model cache cleared'
    localAI.socketStatus = 'disconnected'
    stopMemoryMonitor()
  }

  async function refreshLocalModels() {
    await clearLocalModelCache()
    initLocalAI()
  }

  function getLocalEmbedding(text) {
    return requestWorker('embed', { text }).then((result) => result?.vector || result)
  }

  function getLocalRerank(query, docs) {
    return requestWorker('rerank', { query, docs }).then((result) => result?.results || result)
  }

  async function releaseUnusedLocalWeights() {
    setupWorker()
    await requestWorker('release-unused', {})
    updateMemoryUsage()
  }

  return {
    localAI,
    initLocalAI,
    clearLocalModelCache,
    refreshLocalModels,
    releaseUnusedLocalWeights,
    getLocalEmbedding,
    getLocalRerank,
  }
}
