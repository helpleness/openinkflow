import AIWorker from './src/ai/ai-worker.js?worker'

const worker = new AIWorker()
const pending = new Map()
let nextId = 0

const state = { ready: false, rerankReady: false, models: null, cache: null, status: '', downloads: {} }
worker.addEventListener('message', ({ data }) => {
  if (data.type === 'ready') {
    state.ready = true
    state.models = data.models
    state.cache = data.cache
  } else if (data.type === 'rerank-ready') {
    state.rerankReady = true
    state.models = data.models
    state.cache = data.cache
  } else if (data.type === 'status') {
    state.status = data.message
    if (/unavailable|error|failed|fallback|falling back|ONNX ready/i.test(data.message)) console.log(`[worker] ${data.message}`)
  } else if (data.type === 'model-download') {
    state.downloads[data.info.kind] = data.info
  } else if (data.type === 'error') {
    console.error(`[worker] ${data.error}`)
  }
  if (data.id && pending.has(data.id) && (data.type === 'result' || data.type === 'error')) {
    const { resolve, reject } = pending.get(data.id)
    pending.delete(data.id)
    if (data.type === 'error') reject(new Error(data.error))
    else resolve(data.result)
  }
})

function request(type, payload = {}) {
  const id = ++nextId
  return new Promise((resolve, reject) => {
    pending.set(id, { resolve, reject })
    worker.postMessage({ type, id, payload })
  })
}

window.webgpuEval = {
  state,
  init: () => request('init', { apiBase: location.origin }),
  embed: (text) => request('embed', { text, bypassCache: true }),
  rerank: (query, docs) => request('rerank', { query, docs }),
}
