import * as ort from 'onnxruntime-web/webgpu'
import { PreTrainedTokenizer, env } from '@huggingface/transformers'

const EMBEDDING_MODEL_ID = 'onnx-community/Qwen3-Embedding-0.6B-ONNX'
const EMBEDDING_MODEL_NAME = 'Qwen3-Embedding-0.6B Q4F16'
const EMBEDDING_MODEL_FILE = 'onnx/model_q4f16.onnx'
const EMBEDDING_FALLBACK_FILE = 'onnx/model_int8.onnx'
const RERANK_MODEL_ID = 'onnx-community/bge-reranker-v2-m3-ONNX'
const RERANK_MODEL_FILE = 'onnx/model_q4f16.onnx'
const RERANK_FALLBACK_FILE = 'onnx/model_int8.onnx'
const DB_EMBEDDING_DIM = 1024
const MAX_EMBED_TOKENS = 8192
const MAX_RERANK_TOKENS = 160
// A 4060 can score the four RAG candidates together, avoiding a second WebGPU
// dispatch for every rerank request.
const RERANK_BATCH_SIZE = 4
const RERANK_MAX_CANDIDATES = 4
const EMBED_RUN_TIMEOUT_MS = 45000
const RERANK_RUN_TIMEOUT_MS = 20000
const EMBEDDING_CACHE_DB = 'inkflow-local-ai'
const EMBEDDING_CACHE_STORE = 'embedding_cache'
const EMBEDDING_CACHE_VERSION = 'qwen3-embedding-0.6b-q4f16-ort:1024:v3'
const EMBEDDING_CACHE_MAX_ENTRIES = 1500
// Keep both Q4F16 sessions resident. The user can still explicitly release the
// reranker, but automatic eviction forces expensive WebGPU pipeline rebuilds.
const RERANK_IDLE_UNLOAD_MS = 0
const MODEL_CACHE_DIR = 'inkflow-onnx-models'
const MODEL_IDB_NAME = 'inkflow-local-models'
const MODEL_IDB_STORE = 'files'
const MODEL_CACHE_SCHEMA_VERSION = 2
const MODEL_CACHE_PACKAGE_VERSION = 'q4f16-int8-config-20260728'
const MODEL_PACKAGE_FILES = {
  embedding: [
    'tokenizer.json',
    'tokenizer_config.json',
    'config.json',
    'special_tokens_map.json',
    'added_tokens.json',
    'generation_config.json',
  ],
  rerank: [
    'tokenizer.json',
    'tokenizer_config.json',
    'config.json',
    'special_tokens_map.json',
    'quantize_config.json',
  ],
}
const WEBGPU_EP = { name: 'webgpu', validationMode: 'wgpuOnly' }
const QWEN3_KV_HEADS = 8
const QWEN3_HEAD_DIM = 128

env.allowLocalModels = false
env.allowRemoteModels = false
env.useBrowserCache = false

ort.env.wasm.proxy = false
ort.env.wasm.numThreads = 1
ort.env.logLevel = 'error'
ort.env.debug = false
ort.env.webgpu.powerPreference = 'high-performance'

let embedder = null
let reranker = null
let rerankerPromise = null
let rerankUnloadTimer = 0
let runtimeDevice = 'webgpu'
let rerankRuntimeDevice = 'webgpu'
let apiBase = self.location.origin
let currentModels = {
  embedding: EMBEDDING_MODEL_ID,
  rerank: RERANK_MODEL_ID,
}
let embeddingCacheDbPromise = null
let embeddingCacheWrites = 0
let inferenceQueue = Promise.resolve()
const modelFilePromises = new Map()
const modelPackagePromises = new Map()

self.addEventListener('message', async (event) => {
  const { type, id, payload } = event.data

  try {
    if (type === 'init') {
      apiBase = payload?.apiBase || self.location.origin
      currentModels = {
        embedding: payload?.embeddingModel || EMBEDDING_MODEL_ID,
        rerank: payload?.rerankModel || RERANK_MODEL_ID,
      }
      self.postMessage({ type: 'status', message: `Loading embedding model ${EMBEDDING_MODEL_NAME} with ONNX Runtime...` })
      embedder = await loadEmbeddingEngine(currentModels.embedding)
      runtimeDevice = embedder.device
      self.postMessage({
        type: 'ready',
        models: {
          embedding: `${embedder.modelId} (${embedder.file}, ${runtimeDevice}, ${embedder.quantization})`,
          rerank: `${currentModels.rerank} (正在从浏览器缓存预热)`,
        },
        cache: { embedding: embedder.cache, rerank: null },
      })
      if (id) self.postMessage({ type: 'result', id, result: { ready: true } })
      // Compile the reranker before the first RAG request. It is queued behind
      // any real inference request and stays resident in WebGPU memory.
      void enqueueInference(() => prewarmReranker())
    } else if (type === 'embed') {
      await enqueueInference(() => handleEmbedRequest(id, payload))
    } else if (type === 'rerank') {
      await enqueueInference(() => handleRerankRequest(id, payload))
    } else if (type === 'release-unused') {
      await unloadReranker()
      self.postMessage({ type: 'result', id, result: { released: ['rerank'] } })
    } else if (type === 'clear-cache') {
      await disposeEngine(embedder)
      await unloadReranker()
      embedder = null
      rerankerPromise = null
      await clearOPFSModels()
      await embeddingCacheClear()
      self.postMessage({ type: 'result', id, result: { deleted: [MODEL_CACHE_DIR, MODEL_IDB_NAME] } })
    }
  } catch (error) {
    self.postMessage({ type: 'error', id, error: error.message })
  }
})

function enqueueInference(task) {
  const run = inferenceQueue.catch(() => {}).then(task)
  inferenceQueue = run.finally(() => {})
  return run
}

async function handleEmbedRequest(id, payload) {
  if (!embedder) throw new Error('Embedding model is not loaded')
  const text = payload.text || ''
  const startedAt = performance.now()
  const cacheStartedAt = performance.now()
  const cacheKey = await embeddingCacheKey(text)
  const cached = await embeddingCacheGet(cacheKey)
  const cacheLookupMs = elapsedMs(cacheStartedAt)
  if (cached) {
    self.postMessage({
      type: 'result',
      id,
      result: {
        vector: cached,
        timings: embeddingTimings({
          cache_hit: true,
          cache_lookup_ms: cacheLookupMs,
          total_ms: elapsedMs(startedAt),
        }),
      },
    })
    return
  }
  const embedded = await withTimeout(embedTextWithTimings(text), EMBED_RUN_TIMEOUT_MS, 'embedding run')
  const cacheStoreStartedAt = performance.now()
  await embeddingCachePut(cacheKey, embedded.vector)
  const timings = embeddingTimings({
    cache_hit: false,
    cache_lookup_ms: cacheLookupMs,
    cache_store_ms: elapsedMs(cacheStoreStartedAt),
    ...embedded.timings,
    total_ms: elapsedMs(startedAt),
  })
  self.postMessage({ type: 'status', message: formatEmbeddingTimings(timings) })
  self.postMessage({ type: 'result', id, result: { vector: embedded.vector, timings } })
}

async function handleRerankRequest(id, payload) {
  const reranked = await withTimeout(
    rerankDocuments(payload.query || '', payload.docs || [], Number(payload.top_n || 0)),
    RERANK_RUN_TIMEOUT_MS,
    'rerank run',
  )
  self.postMessage({ type: 'status', message: formatRerankTimings(reranked.timings) })
  self.postMessage({ type: 'result', id, result: reranked })
}

async function loadEmbeddingEngine(modelId) {
  return createTextEngineWithFallback({
    modelId,
    file: EMBEDDING_MODEL_FILE,
    fallbackFile: EMBEDDING_FALLBACK_FILE,
    maxLength: MAX_EMBED_TOKENS,
    kind: 'embedding',
  })
}

async function createTextEngineWithFallback({ fallbackFile, ...options }) {
  try {
    return await createTextEngine(options)
  } catch (error) {
    if (!fallbackFile || fallbackFile === options.file) throw error
    self.postMessage({
      type: 'status',
      message: `${options.kind} Q4F16 is unavailable on this WebGPU adapter; falling back to INT8: ${error.message}`,
    })
    return createTextEngine({ ...options, file: fallbackFile })
  }
}

async function createTextEngine({ modelId, file, maxLength, kind }) {
  const cache = await ensureModelPackageCached(modelId, kind, file)
  const tokenizer = await loadTokenizerFromLocal(modelId)
  const config = await readCachedJSON(modelId, 'config.json')
  const { source: modelSource, dispose: disposeModelSource } = await getCachedModelSource(modelId, file)
  const canUseWebGPU = await hasWebGPUAdapter()
  if (!canUseWebGPU) {
    disposeModelSource?.()
    throw new Error(`${kind} requires WebGPU; WASM/CPU fallback is disabled`)
  }
  let session
  try {
    session = await ort.InferenceSession.create(modelSource, {
      executionProviders: [WEBGPU_EP],
      graphOptimizationLevel: 'all',
      logSeverityLevel: 3,
      // Reuse stable-shape buffers instead of allocating them on every run.
      // This intentionally trades some retained GPU/CPU memory for latency.
      enableMemPattern: true,
      enableCpuMemArena: true,
    })
    validateSessionInputs(session, kind, modelId, file)
  } finally {
    disposeModelSource?.()
  }
  return {
    modelId,
    file,
    tokenizer,
    session,
    device: 'webgpu',
    maxLength,
    kind,
    config,
    cache,
    kvDtype: 'float32',
    quantization: file.includes('q4f16') ? 'q4f16' : 'int8',
  }
}

function validateSessionInputs(session, kind, modelId, file) {
  const inputNames = Array.isArray(session.inputNames) ? session.inputNames : []
  const outputNames = Array.isArray(session.outputNames) ? session.outputNames : []
  self.postMessage({
    type: 'status',
    message: `${kind} ONNX ready: ${modelId}/${file}; inputs=${inputNames.join(',')}; outputs=${outputNames.join(',')}`,
  })
}

async function getCachedModelSource(modelId, file) {
  const key = `${modelId}/${file}`
  if (!modelFilePromises.has(key)) {
    modelFilePromises.set(key, loadCachedModelSource(modelId, file).finally(() => {
      modelFilePromises.delete(key)
    }))
  }
  return modelFilePromises.get(key)
}

async function loadCachedModelSource(modelId, file) {
  let cached = await openLocalModelSource(modelId, file)
  if (cached) return cached

  await downloadModelToLocalCache(modelId, file, modelKindForID(modelId))
  cached = await openLocalModelSource(modelId, file)
  if (!cached) throw new Error(`模型已下载但无法打开本地缓存：${modelId}/${file}`)
  return cached
}

function modelFileURL(modelId, file) {
  return `${apiBase}/hf-mirror/${modelId}/resolve/main/${file}`
}

function modelCacheFileName(modelId, file) {
  return `${modelId}__${file}`.replace(/[\\/:"*?<>|]+/g, '_')
}

function modelKindForID(modelId) {
  if (modelId === currentModels.embedding || modelId === EMBEDDING_MODEL_ID) return 'embedding'
  if (modelId === currentModels.rerank || modelId === RERANK_MODEL_ID) return 'rerank'
  return ''
}

function reportModelDownload(kind, file, { state = 'downloading', received = 0, total = 0, error = '' } = {}) {
  if (kind !== 'embedding' && kind !== 'rerank') return
  const knownTotal = Math.max(0, Number(total || 0))
  const downloaded = Math.max(0, Number(received || 0))
  self.postMessage({
    type: 'model-download',
    info: {
      kind,
      file,
      state,
      received: downloaded,
      total: knownTotal,
      progress: knownTotal > 0 ? Math.round((downloaded / knownTotal) * 100) : null,
      error,
    },
  })
}

async function modelCacheRoot() {
  if (!self.navigator?.storage?.getDirectory) {
    throw new Error('当前浏览器不支持 OPFS')
  }
  const root = await self.navigator.storage.getDirectory()
  return root.getDirectoryHandle(MODEL_CACHE_DIR, { create: true })
}

async function openLocalModelSource(modelId, file) {
  const opfs = await openOPFSModelSource(modelId, file)
  if (opfs) return opfs
  return openIDBModelSource(modelId, file)
}

function modelManifestFile(kind) {
  return `__inkflow_${kind}_package_manifest_v${MODEL_CACHE_SCHEMA_VERSION}.json`
}

function modelPackageFiles(modelId, kind, modelFile) {
  const knownModel = (kind === 'embedding' && modelId === EMBEDDING_MODEL_ID)
    || (kind === 'rerank' && modelId === RERANK_MODEL_ID)
  const configs = knownModel
    ? MODEL_PACKAGE_FILES[kind]
    : ['tokenizer.json', 'tokenizer_config.json', 'config.json']
  return [...new Set([...configs, modelFile])]
}

async function ensureModelPackageCached(modelId, kind, modelFile) {
  const key = `${modelId}:${kind}:${modelFile}:${MODEL_CACHE_PACKAGE_VERSION}`
  if (!modelPackagePromises.has(key)) {
    modelPackagePromises.set(key, ensureModelPackageCachedOnce(modelId, kind, modelFile).finally(() => {
      modelPackagePromises.delete(key)
    }))
  }
  return modelPackagePromises.get(key)
}

async function ensureModelPackageCachedOnce(modelId, kind, modelFile) {
  reportModelDownload(kind, modelFile, { state: 'checking' })
  await cleanupPartialOPFSFiles()
  const files = modelPackageFiles(modelId, kind, modelFile)
  const manifest = await readCachedJSONIfPresent(modelId, modelManifestFile(kind))
  const validManifest = manifest
    && manifest.schema === MODEL_CACHE_SCHEMA_VERSION
    && manifest.packageVersion === MODEL_CACHE_PACKAGE_VERSION
    && manifest.modelId === modelId
    && manifest.kind === kind
    && Array.isArray(manifest.files)
    ? manifest
    : null
  const expectedSizes = new Map((validManifest?.files || []).map((entry) => [entry.name, Number(entry.size || 0)]))
  const localFiles = []
  const missing = []

  for (const file of files) {
    const info = await getLocalModelFileInfo(modelId, file)
    const expectedSize = expectedSizes.get(file) || 0
    if (!info || info.size <= 0 || (expectedSize > 0 && info.size !== expectedSize)) {
      missing.push(file)
    } else {
      localFiles.push({ name: file, size: info.size, storage: info.storage })
    }
  }

  for (const file of missing) {
    await downloadModelToLocalCache(modelId, file, kind)
    const info = await getLocalModelFileInfo(modelId, file)
    if (!info || info.size <= 0) {
      throw new Error(`downloaded file is missing from browser cache: ${modelId}/${file}`)
    }
    localFiles.push({ name: file, size: info.size, storage: info.storage })
  }

  localFiles.sort((a, b) => files.indexOf(a.name) - files.indexOf(b.name))
  await writeCachedJSON(modelId, modelManifestFile(kind), {
    schema: MODEL_CACHE_SCHEMA_VERSION,
    packageVersion: MODEL_CACHE_PACKAGE_VERSION,
    modelId,
    kind,
    modelFile,
    files: localFiles,
    completedAt: Date.now(),
  })
  const totalBytes = localFiles.reduce((sum, entry) => sum + entry.size, 0)
  const storage = [...new Set(localFiles.map((entry) => entry.storage))].join('+')
  reportModelDownload(kind, modelFile, {
    state: missing.length ? 'complete' : 'cached',
    received: totalBytes,
    total: totalBytes,
  })
  self.postMessage({
    type: 'status',
    message: missing.length
      ? `${kind} model package cached (${formatByteSize(totalBytes)}, downloaded ${missing.length} missing files)`
      : `${kind} model package loaded from browser cache (${formatByteSize(totalBytes)}, ${storage || 'local'})`,
  })
  return {
    hit: missing.length === 0,
    downloaded: missing,
    files: localFiles.length,
    totalBytes,
    storage,
    packageVersion: MODEL_CACHE_PACKAGE_VERSION,
  }
}

async function getLocalModelFileInfo(modelId, file) {
  const opfs = await getOPFSModelFileInfo(modelId, file)
  if (opfs) return opfs
  return getIDBModelFileInfo(modelId, file)
}

async function getOPFSModelFileInfo(modelId, file) {
  try {
    const dir = await modelCacheRoot()
    const handle = await dir.getFileHandle(modelCacheFileName(modelId, file))
    const cached = await handle.getFile()
    if (cached.size <= 0) return null
    return { size: cached.size, storage: 'opfs' }
  } catch {
    return null
  }
}

async function getIDBModelFileInfo(modelId, file) {
  try {
    const cached = await modelIDBOperation('readonly', (store) => store.get(modelCacheFileName(modelId, file)))
    const size = Number(cached?.size || cached?.blob?.size || 0)
    if (size <= 0) return null
    return { size, storage: 'indexeddb' }
  } catch {
    return null
  }
}

async function openOPFSModelSource(modelId, file) {
  try {
    const dir = await modelCacheRoot()
    const handle = await dir.getFileHandle(modelCacheFileName(modelId, file))
    const cached = await handle.getFile()
    if (cached.size <= 0) return null
    self.postMessage({ type: 'status', message: `Using cached ${modelId}/${file}` })
    const url = URL.createObjectURL(cached)
    return { source: url, dispose: () => URL.revokeObjectURL(url) }
  } catch {
    return null
  }
}

async function clearOPFSModels() {
  if (self.navigator?.storage?.getDirectory) {
    try {
      const root = await self.navigator.storage.getDirectory()
      await root.removeEntry(MODEL_CACHE_DIR, { recursive: true })
    } catch {
      // Missing OPFS cache is fine.
    }
  }
  await clearIDBModels()
}

async function cleanupPartialOPFSFiles() {
  if (!self.navigator?.storage?.getDirectory) return
  try {
    const dir = await modelCacheRoot()
    for await (const [name] of dir.entries()) {
      if (name.includes('.part-')) await dir.removeEntry(name).catch(() => {})
    }
  } catch {
    // OPFS may be unavailable; IndexedDB remains the fallback.
  }
}

function openModelIDB() {
  return new Promise((resolve, reject) => {
    if (!self.indexedDB) return reject(new Error('当前浏览器不支持本地模型数据库'))
    const request = self.indexedDB.open(MODEL_IDB_NAME, 1)
    request.onupgradeneeded = () => {
      const db = request.result
      if (!db.objectStoreNames.contains(MODEL_IDB_STORE)) db.createObjectStore(MODEL_IDB_STORE)
    }
    request.onsuccess = () => resolve(request.result)
    request.onerror = () => reject(request.error || new Error('打开本地模型数据库失败'))
  })
}

function modelIDBOperation(mode, action) {
  return openModelIDB().then((db) => new Promise((resolve, reject) => {
    const tx = db.transaction(MODEL_IDB_STORE, mode)
    const store = tx.objectStore(MODEL_IDB_STORE)
    const request = action(store)
    request.onsuccess = () => resolve(request.result)
    request.onerror = () => reject(request.error || new Error('本地模型数据库操作失败'))
    tx.oncomplete = () => db.close()
    tx.onerror = () => {
      db.close()
      reject(tx.error || new Error('本地模型数据库事务失败'))
    }
  }))
}

async function openIDBModelSource(modelId, file) {
  try {
    const cached = await modelIDBOperation('readonly', (store) => store.get(modelCacheFileName(modelId, file)))
    const blob = cached?.blob
    if (!blob || blob.size <= 0) return null
    self.postMessage({ type: 'status', message: `Using local IndexedDB ${modelId}/${file}` })
    const url = URL.createObjectURL(blob)
    return { source: url, dispose: () => URL.revokeObjectURL(url) }
  } catch {
    return null
  }
}

async function readLocalModelBytes(modelId, file) {
  const opfsBytes = await readOPFSModelBytes(modelId, file)
  if (opfsBytes) return opfsBytes
  const idbBytes = await readIDBModelBytes(modelId, file)
  if (idbBytes) return idbBytes
  return null
}

async function readOPFSModelBytes(modelId, file) {
  try {
    const dir = await modelCacheRoot()
    const handle = await dir.getFileHandle(modelCacheFileName(modelId, file))
    const cached = await handle.getFile()
    if (cached.size <= 0) return null
    return new Uint8Array(await cached.arrayBuffer())
  } catch {
    return null
  }
}

async function readIDBModelBytes(modelId, file) {
  try {
    const cached = await modelIDBOperation('readonly', (store) => store.get(modelCacheFileName(modelId, file)))
    const blob = cached?.blob
    if (!blob || blob.size <= 0) return null
    return new Uint8Array(await blob.arrayBuffer())
  } catch {
    return null
  }
}

async function downloadModelToLocalCache(modelId, fileName, kind = '') {
  try {
    await downloadModelToOPFS(modelId, fileName, kind)
    return
  } catch (error) {
    self.postMessage({ type: 'status', message: `OPFS 缓存不可用，改用 IndexedDB: ${error.message}` })
  }
  await downloadModelToIDB(modelId, fileName, kind)
}

async function downloadModelToOPFS(modelId, fileName, kind) {
  const url = modelFileURL(modelId, fileName)
  const cacheName = modelCacheFileName(modelId, fileName)
  const dir = await modelCacheRoot()
  const partialName = `${cacheName}.part-${Date.now()}-${Math.random().toString(16).slice(2)}`
  const handle = await dir.getFileHandle(partialName, { create: true })
  const writable = await handle.createWritable({ keepExistingData: false })
  let reader = null
  try {
    self.postMessage({ type: 'status', message: `Downloading ${modelId}/${fileName} to browser cache...` })
    reportModelDownload(kind, fileName, { state: 'downloading' })
    const response = await fetch(url)
    if (!response.ok || !response.body) throw new Error(`download ${fileName}: HTTP ${response.status}`)
    const total = Number(response.headers.get('content-length') || 0)
    reader = response.body.getReader()
    let received = 0
    let lastReportedAt = 0
    while (true) {
      const { value, done } = await reader.read()
      if (done) break
      received += value.byteLength
      await writable.write(value)
      const now = Date.now()
      if (now - lastReportedAt >= 120) {
        reportModelDownload(kind, fileName, { state: 'downloading', received, total })
        if (total > 0) {
          self.postMessage({ type: 'progress', info: { status: 'progress', progress: Math.round((received / total) * 100) } })
        }
        lastReportedAt = now
      }
    }
    await writable.close()
    await commitOPFSDownload(dir, handle, partialName, cacheName)
    reportModelDownload(kind, fileName, { state: 'complete', received, total })
  } catch (error) {
    await reader?.cancel?.().catch(() => {})
    await writable.abort().catch(() => {})
    await dir.removeEntry(partialName).catch(() => {})
    throw error
  }
}

async function commitOPFSDownload(dir, partialHandle, partialName, cacheName) {
  if (typeof partialHandle.move === 'function') {
    await dir.removeEntry(cacheName).catch(() => {})
    await partialHandle.move(cacheName)
    return
  }

  const source = await partialHandle.getFile()
  const finalHandle = await dir.getFileHandle(cacheName, { create: true })
  const finalWritable = await finalHandle.createWritable({ keepExistingData: false })
  try {
    await source.stream().pipeTo(finalWritable)
    await dir.removeEntry(partialName)
  } catch (error) {
    await finalWritable.abort().catch(() => {})
    await dir.removeEntry(cacheName).catch(() => {})
    throw error
  }
}

async function downloadModelToIDB(modelId, fileName, kind) {
  const url = modelFileURL(modelId, fileName)
  self.postMessage({ type: 'status', message: `Downloading ${modelId}/${fileName} to IndexedDB...` })
  reportModelDownload(kind, fileName, { state: 'downloading' })
  const response = await fetch(url)
  if (!response.ok) throw new Error(`download ${fileName}: HTTP ${response.status}`)
  const total = Number(response.headers.get('content-length') || 0)
  let blob
  if (response.body) {
    const chunks = []
    const reader = response.body.getReader()
    let received = 0
    let lastReportedAt = 0
    while (true) {
      const { value, done } = await reader.read()
      if (done) break
      chunks.push(value)
      received += value.byteLength
      const now = Date.now()
      if (now - lastReportedAt >= 120) {
        reportModelDownload(kind, fileName, { state: 'downloading', received, total })
        lastReportedAt = now
      }
    }
    blob = new Blob(chunks)
    reportModelDownload(kind, fileName, { state: 'complete', received, total })
  } else {
    blob = await response.blob()
    reportModelDownload(kind, fileName, { state: 'complete', received: blob.size, total })
  }
  if (!blob || blob.size <= 0) throw new Error(`downloaded empty model file: ${fileName}`)
  await writeIDBBlob(modelId, fileName, blob)
}

async function writeIDBBlob(modelId, fileName, blob) {
  await modelIDBOperation('readwrite', (store) => store.put({
    blob,
    modelId,
    fileName,
    size: blob.size,
    updatedAt: Date.now(),
  }, modelCacheFileName(modelId, fileName)))
}

async function clearIDBModels() {
  try {
    await modelIDBOperation('readwrite', (store) => store.clear())
  } catch {
    // Missing IndexedDB cache is fine.
  }
}

async function loadTokenizerFromLocal(modelId) {
  const [tokenizerJSON, tokenizerConfig] = await Promise.all([
    readCachedJSON(modelId, 'tokenizer.json'),
    readCachedJSON(modelId, 'tokenizer_config.json'),
  ])
  return new PreTrainedTokenizer(tokenizerJSON, tokenizerConfig || {})
}

async function readCachedJSON(modelId, file) {
  const bytes = await readLocalModelBytes(modelId, file)
  if (!bytes) throw new Error(`configuration file is missing from browser cache: ${modelId}/${file}`)
  return JSON.parse(new TextDecoder().decode(bytes))
}

async function readCachedJSONIfPresent(modelId, file) {
  try {
    const bytes = await readLocalModelBytes(modelId, file)
    if (!bytes) return null
    return JSON.parse(new TextDecoder().decode(bytes))
  } catch {
    return null
  }
}

async function writeCachedJSON(modelId, file, value) {
  const blob = new Blob([JSON.stringify(value)], { type: 'application/json' })
  try {
    const dir = await modelCacheRoot()
    const handle = await dir.getFileHandle(modelCacheFileName(modelId, file), { create: true })
    const writable = await handle.createWritable({ keepExistingData: false })
    await writable.write(blob)
    await writable.close()
    return
  } catch {
    await writeIDBBlob(modelId, file, blob)
  }
}

function formatByteSize(bytes) {
  if (!Number.isFinite(bytes) || bytes <= 0) return '0 B'
  const units = ['B', 'KB', 'MB', 'GB']
  const unit = Math.min(Math.floor(Math.log(bytes) / Math.log(1024)), units.length - 1)
  return `${(bytes / (1024 ** unit)).toFixed(unit > 1 ? 1 : 0)} ${units[unit]}`
}

async function hasWebGPUAdapter() {
  if (!self.navigator?.gpu?.requestAdapter) return false
  try {
    return Boolean(await self.navigator.gpu.requestAdapter({ powerPreference: undefined }))
  } catch {
    return false
  }
}

async function embedTextWithTimings(text) {
  const tokenizeStartedAt = performance.now()
  const feeds = tokenizeForSession(embedder.tokenizer, [text], embedder.maxLength)
  const tokenizeMs = elapsedMs(tokenizeStartedAt)
  let outputs = null
  try {
    const sessionStartedAt = performance.now()
    outputs = await runSession(embedder, feeds)
    const sessionRunMs = elapsedMs(sessionStartedAt)
    const poolingStartedAt = performance.now()
    const tensor = pickEmbeddingTensor(outputs)
    const values = poolEmbedding(tensor, feeds.attention_mask)
    const vector = normalizeEmbedding(values, DB_EMBEDDING_DIM)
    return {
      vector,
      timings: {
        tokenizer_ms: tokenizeMs,
        session_run_ms: sessionRunMs,
        pooling_ms: elapsedMs(poolingStartedAt),
        tokens: Number(feeds.input_ids?.dims?.[1] || 0),
      },
    }
  } finally {
    disposeTensors(outputs)
    disposeTensors(feeds)
  }
}

function elapsedMs(startedAt) {
  return Math.round((performance.now() - startedAt) * 100) / 100
}

function embeddingTimings(timings) {
  return { device: embedder?.device || runtimeDevice, ...timings }
}

function formatEmbeddingTimings(timings) {
  if (timings.cache_hit) return `Embedding cache hit ${timings.total_ms}ms`
  return `Embedding ${timings.tokens || 0} tokens: cache ${timings.cache_lookup_ms || 0}ms, tokenize ${timings.tokenizer_ms || 0}ms, GPU ${timings.session_run_ms || 0}ms, pool ${timings.pooling_ms || 0}ms, store ${timings.cache_store_ms || 0}ms, total ${timings.total_ms || 0}ms`
}

function rerankTimings(timings) {
  return { device: reranker?.device || rerankRuntimeDevice, ...timings }
}

function formatRerankTimings(timings) {
  if (timings.fallback) return `Rerank fallback after ${timings.total_ms || 0}ms`
  return `Rerank ${timings.candidates || 0} candidates: load ${timings.model_load_ms || 0}ms, tokenize ${timings.tokenizer_ms || 0}ms, GPU ${timings.session_run_ms || 0}ms, output ${timings.output_ms || 0}ms, total ${timings.total_ms || 0}ms`
}

function tokenizeForSession(tokenizer, texts, maxLength) {
  const encoded = tokenizer(texts, {
    padding: true,
    truncation: true,
    max_length: maxLength,
  })
  const inputIds = tensorData(encoded.input_ids)
  const attentionMask = tensorData(encoded.attention_mask)
  const dims = encoded.input_ids?.dims || [texts.length, inputIds.length / texts.length]
  const feeds = {
    input_ids: new ort.Tensor('int64', toBigInt64Array(inputIds), dims),
    attention_mask: new ort.Tensor('int64', toBigInt64Array(attentionMask), dims),
    position_ids: new ort.Tensor('int64', buildPositionIdsFromMask(attentionMask, dims), dims),
  }
  if (encoded.token_type_ids) {
    feeds.token_type_ids = new ort.Tensor('int64', toBigInt64Array(tensorData(encoded.token_type_ids)), dims)
  }
  disposeTokenizerOutput(encoded)
  return feeds
}

function buildPositionIds(dims) {
  const batch = Number(dims[0] || 1)
  const seqLen = Number(dims[1] || 0)
  const out = new BigInt64Array(batch * seqLen)
  for (let row = 0; row < batch; row += 1) {
    const offset = row * seqLen
    for (let col = 0; col < seqLen; col += 1) {
      out[offset + col] = BigInt(col)
    }
  }
  return out
}

function buildPositionIdsFromMask(attentionMask, dims) {
  const batch = Number(dims[0] || 1)
  const seqLen = Number(dims[1] || 0)
  const mask = attentionMask || []
  const out = new BigInt64Array(batch * seqLen)
  for (let row = 0; row < batch; row += 1) {
    let sum = 0n
    const offset = row * seqLen
    for (let col = 0; col < seqLen; col += 1) {
      const index = offset + col
      if (BigInt(mask[index] || 0) === 0n) {
        out[index] = 1n
      } else {
        out[index] = sum
        sum += BigInt(mask[index])
      }
    }
  }
  return out
}

function disposeTokenizerOutput(encoded) {
  for (const value of Object.values(encoded || {})) {
    try {
      value?.dispose?.()
      value?.ort_tensor?.dispose?.()
    } catch {
      // Tokenizer output may be plain arrays; disposal is best-effort.
    }
  }
}

function tensorData(value) {
  if (!value) return []
  if (value.data) return value.data
  if (value.ort_tensor?.data) return value.ort_tensor.data
  return value
}

function toBigInt64Array(values) {
  if (values instanceof BigInt64Array) return values
  const out = new BigInt64Array(values.length)
  for (let i = 0; i < values.length; i += 1) out[i] = BigInt(values[i])
  return out
}

function pickEmbeddingTensor(outputs) {
  const names = ['sentence_embedding', 'embeddings', 'last_hidden_state', 'token_embeddings']
  for (const name of names) {
    if (outputs[name]) return outputs[name]
  }
  return Object.values(outputs)[0]
}

function poolEmbedding(tensor, attentionMask) {
  const data = tensor.data || []
  const dims = tensor.dims || []
  if (dims.length === 2) return data.subarray ? data.subarray(0, dims[1] || DB_EMBEDDING_DIM) : data
  if (dims.length !== 3) return data.subarray ? data.subarray(0, DB_EMBEDDING_DIM) : Array.prototype.slice.call(data, 0, DB_EMBEDDING_DIM)
  const seqLen = dims[1]
  const hidden = dims[2]
  const mask = attentionMask?.data || attentionMask || []
  let last = 0
  for (let i = 0; i < seqLen; i += 1) {
    if (Number(mask[i]) > 0) last = i
  }
  const start = last * hidden
  return data.subarray ? data.subarray(start, start + hidden) : Array.prototype.slice.call(data, start, start + hidden)
}

function normalizeEmbedding(values, dim) {
  const out = new Array(dim)
  let normSq = 0
  for (let i = 0; i < dim; i += 1) {
    const value = Number(values[i] || 0)
    out[i] = value
    normSq += value * value
  }
  const norm = Math.sqrt(normSq) || 1
  for (let i = 0; i < dim; i += 1) out[i] /= norm
  return out
}

function loadReranker() {
  clearTimeout(rerankUnloadTimer)
  if (reranker) return Promise.resolve(reranker)
  if (rerankerPromise) return rerankerPromise
  self.postMessage({ type: 'status', message: `Loading rerank model ${currentModels.rerank} with ONNX Runtime...` })
  rerankerPromise = createTextEngineWithFallback({
    modelId: currentModels.rerank,
    file: RERANK_MODEL_FILE,
    fallbackFile: RERANK_FALLBACK_FILE,
    maxLength: MAX_RERANK_TOKENS,
    kind: 'rerank',
  }).then((engine) => {
    reranker = engine
    rerankRuntimeDevice = engine.device
    self.postMessage({
      type: 'rerank-ready',
      models: {
        embedding: `${embedder?.modelId || currentModels.embedding} (${runtimeDevice}, ${embedder?.quantization || 'q4f16'})`,
        rerank: `${currentModels.rerank} (${rerankRuntimeDevice}, ${engine.quantization})`,
      },
      cache: { embedding: embedder?.cache || null, rerank: engine.cache },
    })
    return engine
  }).catch((error) => {
    rerankerPromise = null
    throw error
  })
  return rerankerPromise
}

async function rerankDocuments(query, docs, topN = 0) {
  const startedAt = performance.now()
  const lexical = lexicalRerank(query, docs)
  if (!docs.length) {
    return {
      results: lexicalSlice(lexical, topN),
      timings: rerankTimings({ candidates: 0, total_ms: elapsedMs(startedAt) }),
    }
  }
  const candidateLimit = rerankCandidateLimit(docs.length, topN)
  const candidateDocs = docs.slice(0, candidateLimit)
  const candidateLexical = lexical
    .filter((item) => item.index < candidateLimit)
    .map((item) => ({ ...item }))
  try {
    const alreadyLoaded = Boolean(reranker)
    const modelStartedAt = performance.now()
    const engine = await withTimeout(loadReranker(), 20000, 'rerank model load')
    const modelLoadMs = elapsedMs(modelStartedAt)
    self.postMessage({ type: 'status', message: `Reranking ${candidateDocs.length}/${docs.length} candidates with WebGPU...` })
    const ranked = []
    const aggregate = {
      tokenizer_ms: 0,
      session_run_ms: 0,
      output_ms: 0,
      tokens: 0,
      batches: 0,
    }
    for (let offset = 0; offset < candidateDocs.length; offset += RERANK_BATCH_SIZE) {
      const batchDocs = candidateDocs.slice(offset, offset + RERANK_BATCH_SIZE)
      const batchLexical = candidateLexical
        .filter((item) => item.index >= offset && item.index < offset + batchDocs.length)
        .map((item) => ({ ...item, index: item.index - offset }))
      const batch = await rerankBatch(engine, query, batchDocs, batchLexical)
      aggregate.tokenizer_ms += batch.timings.tokenizer_ms
      aggregate.session_run_ms += batch.timings.session_run_ms
      aggregate.output_ms += batch.timings.output_ms
      aggregate.tokens = Math.max(aggregate.tokens, batch.timings.tokens)
      aggregate.batches += 1
      for (const item of batch.results) ranked.push({ ...item, index: item.index + offset })
    }
    return {
      results: lexicalSlice(ranked.sort((a, b) => b.score - a.score), topN),
      timings: rerankTimings({
        candidates: candidateDocs.length,
        model_cache_hit: alreadyLoaded,
        model_load_ms: modelLoadMs,
        ...aggregate,
        total_ms: elapsedMs(startedAt),
      }),
    }
  } catch (error) {
    self.postMessage({ type: 'status', message: `Rerank fallback: ${error.message}` })
    return {
      results: lexicalSlice(lexical, topN),
      timings: rerankTimings({
        candidates: candidateDocs.length,
        fallback: true,
        total_ms: elapsedMs(startedAt),
      }),
    }
  } finally {
    scheduleRerankUnload()
  }
}

function rerankCandidateLimit(total, topN) {
  if (total <= 0) return 0
  return Math.min(total, RERANK_MAX_CANDIDATES)
}

function lexicalSlice(items, topN) {
  return topN > 0 && topN < items.length ? items.slice(0, topN) : items
}

async function rerankBatch(engine, query, docs, lexical) {
  let feeds = null
  let outputs = null
  try {
    const tokenizerStartedAt = performance.now()
    const inputs = docs.map((doc) => [query, doc])
    feeds = tokenizePairsForSession(engine.tokenizer, inputs, engine.maxLength)
    const tokenizerMs = elapsedMs(tokenizerStartedAt)
    const sessionStartedAt = performance.now()
    outputs = await runSession(engine, feeds)
    const sessionRunMs = elapsedMs(sessionStartedAt)
    const outputStartedAt = performance.now()
    return {
      results: normalizeRerankOutput(outputs, docs, lexical),
      timings: {
        tokenizer_ms: tokenizerMs,
        session_run_ms: sessionRunMs,
        output_ms: elapsedMs(outputStartedAt),
        tokens: Number(feeds.input_ids?.dims?.[1] || 0),
      },
    }
  } finally {
    disposeTensors(outputs)
    disposeTensors(feeds)
  }
}

function disposeTensors(values) {
  if (!values) return
  for (const value of Object.values(values)) {
    try {
      value?.dispose?.()
    } catch {
      // ORT tensor disposal is best-effort.
    }
  }
}

async function runSession(engine, feeds) {
  try {
    return await engine.session.run(prepareSessionFeeds(engine, feeds))
  } catch (error) {
    if (shouldRetryWithFloat16KV(error, engine)) {
      engine.kvDtype = 'float16'
      self.postMessage({ type: 'status', message: `${engine.kind} retrying ONNX run with float16 empty KV cache...` })
      return engine.session.run(prepareSessionFeeds(engine, feeds))
    }
    throw error
  }
}

function shouldRetryWithFloat16KV(error, engine) {
  if (engine.kvDtype === 'float16') return false
  return /past_key_values|tensor\(float16\)|float16|Unexpected input data type/i.test(String(error?.message || error || ''))
}

function prepareSessionFeeds(engine, feeds) {
  const prepared = filterFeeds(engine.session, feeds)
  addMissingDecoderFeeds(engine, prepared, feeds)
  return prepared
}

function filterFeeds(session, feeds) {
  const names = Array.isArray(session.inputNames) ? session.inputNames : Object.keys(feeds)
  const filtered = {}
  for (const name of names) {
    if (feeds[name]) filtered[name] = feeds[name]
  }
  return filtered
}

function addMissingDecoderFeeds(engine, feeds, baseFeeds) {
  const names = Array.isArray(engine.session.inputNames) ? engine.session.inputNames : []
  if (!names.length) return
  const dims = baseFeeds.input_ids?.dims || [1, 0]
  const batch = Number(dims[0] || 1)
  const seqLen = Number(dims[1] || 0)
  for (const name of names) {
    if (feeds[name]) continue
    if (/^past_key_values\.\d+\.(key|value)$/.test(name)) {
      feeds[name] = emptyPastKeyValueTensor(engine, name, batch, seqLen)
    } else if (name === 'cache_position') {
      feeds[name] = tensorForMetadata(engine, name, buildCachePosition(seqLen), [seqLen], 'int64', { sequence_length: seqLen })
    } else if (name === 'use_cache_branch') {
      feeds[name] = tensorForMetadata(engine, name, [false], [1], 'bool')
    } else if (name === 'num_logits_to_keep') {
      feeds[name] = tensorForMetadata(engine, name, new BigInt64Array([0n]), [], 'int64')
    }
  }
}

function emptyPastKeyValueTensor(engine, name, batch, seqLen) {
  const metadata = inputMetadata(engine, name)
  const dtype = engine.kvDtype === 'float16' ? 'float16' : normalizeTensorType(metadata?.type || 'float32')
  const dims = resolveInputShape(metadata?.shape, {
    batch_size: batch,
    sequence_length: seqLen,
    past_sequence_length: 0,
    total_sequence_length: seqLen,
    num_heads: QWEN3_KV_HEADS,
    num_key_value_heads: QWEN3_KV_HEADS,
    head_dim: QWEN3_HEAD_DIM,
    'batch_size x num_heads': batch * QWEN3_KV_HEADS,
    'batch_size x num_key_value_heads': batch * QWEN3_KV_HEADS,
  }, [batch, QWEN3_KV_HEADS, 0, QWEN3_HEAD_DIM])
  return new ort.Tensor(dtype, zeroDataForType(dtype, elementCount(dims)), dims)
}

function inputMetadata(engine, name) {
  const metadata = engine.session.inputMetadata
  if (Array.isArray(metadata)) return metadata.find((item) => item.name === name)
  return metadata?.[name] ? { name, ...metadata[name] } : null
}

function tensorForMetadata(engine, name, data, fallbackDims, fallbackType, symbols = {}) {
  const metadata = inputMetadata(engine, name)
  const dtype = normalizeTensorType(metadata?.type || fallbackType)
  const dims = resolveInputShape(metadata?.shape, symbols, fallbackDims)
  return new ort.Tensor(dtype, coerceDataForType(dtype, data, elementCount(dims)), dims)
}

function resolveInputShape(shape, symbols, fallback) {
  if (!Array.isArray(shape) || !shape.length) return fallback
  return shape.map((dim) => {
    if (typeof dim === 'number') return dim
    if (typeof dim === 'bigint') return Number(dim)
    if (typeof dim === 'string') return Number(symbols[dim] ?? 0)
    return 0
  })
}

function normalizeTensorType(type) {
  if (type === 'tensor(float16)') return 'float16'
  if (type === 'tensor(float)') return 'float32'
  if (type === 'tensor(float32)') return 'float32'
  if (type === 'tensor(int64)') return 'int64'
  if (type === 'tensor(bool)') return 'bool'
  if (type === 'bfloat16') return 'float16'
  return type || 'float32'
}

function elementCount(dims) {
  return dims.reduce((total, dim) => total * Math.max(0, Number(dim || 0)), 1)
}

function zeroDataForType(dtype, size) {
  if (dtype === 'float16') return new Uint16Array(size)
  if (dtype === 'int64') return new BigInt64Array(size)
  if (dtype === 'bool') return new Array(size).fill(false)
  return new Float32Array(size)
}

function coerceDataForType(dtype, data, size) {
  if (dtype === 'bool') {
    if (Array.isArray(data)) return data
    return Array.from(data || []).map(Boolean)
  }
  if (dtype === 'int64') {
    if (data instanceof BigInt64Array) return data
    const out = new BigInt64Array(size)
    Array.from(data || []).forEach((value, index) => { out[index] = BigInt(value) })
    return out
  }
  if (dtype === 'float16') {
    if (data instanceof Uint16Array) return data
    return new Uint16Array(size)
  }
  if (data instanceof Float32Array) return data
  const out = new Float32Array(size)
  Array.from(data || []).forEach((value, index) => { out[index] = Number(value) })
  return out
}

function buildCachePosition(seqLen) {
  const out = new BigInt64Array(seqLen)
  for (let i = 0; i < seqLen; i += 1) out[i] = BigInt(i)
  return out
}

function tokenizePairsForSession(tokenizer, pairs, maxLength) {
  try {
    return tokenizeForSession(tokenizer, pairs, maxLength)
  } catch {
    const sep = tokenizer.sep_token || '[SEP]'
    return tokenizeForSession(tokenizer, pairs.map(([query, doc]) => `${query} ${sep} ${doc}`), maxLength)
  }
}

function normalizeRerankOutput(outputs, docs, fallback) {
  const tensor = outputs.logits || outputs.output || Object.values(outputs)[0]
  const values = Array.from(tensor.data)
  const dims = tensor.dims || [docs.length, values.length / Math.max(1, docs.length)]
  const fallbackScores = new Map(fallback.map((item) => [item.index, item.score]))
  const ranked = docs.map((doc, index) => {
    const width = dims.length > 1 ? dims[1] : 1
    const base = index * width
    const score = Number(values[base + width - 1] ?? values[base] ?? fallbackScores.get(index) ?? 0)
    return { index, text: doc, content: doc, score }
  })
  return ranked.sort((a, b) => b.score - a.score)
}

function scheduleRerankUnload() {
  clearTimeout(rerankUnloadTimer)
  if (RERANK_IDLE_UNLOAD_MS <= 0) return
  rerankUnloadTimer = setTimeout(() => {
    unloadReranker().catch(() => {})
  }, RERANK_IDLE_UNLOAD_MS)
}

async function prewarmReranker() {
  try {
    const startedAt = performance.now()
    const engine = await loadReranker()
    await rerankBatch(engine, 'semantic relevance warmup', ['warmup'], [{ index: 0, score: 0 }])
    self.postMessage({
      type: 'status',
      message: `Rerank WebGPU prewarmed in ${elapsedMs(startedAt)}ms`,
    })
  } catch (error) {
    reportModelDownload('rerank', RERANK_MODEL_FILE, { state: 'error', error: error.message })
    self.postMessage({ type: 'status', message: `Rerank prewarm skipped: ${error.message}` })
  }
}

async function unloadReranker() {
  clearTimeout(rerankUnloadTimer)
  rerankUnloadTimer = 0
  if (rerankerPromise && !reranker) {
    try {
      await rerankerPromise
    } catch {
      // Ignore failed in-flight load; it has already been reported to callers.
    }
  }
  await disposeEngine(reranker)
  reranker = null
  rerankerPromise = null
  self.postMessage({
    type: 'rerank-unloaded',
    models: {
      embedding: `${embedder?.modelId || currentModels.embedding} (${runtimeDevice}, ${embedder?.quantization || 'q4f16'})`,
      rerank: `${currentModels.rerank} (released)`,
    },
  })
}

async function disposeEngine(engine) {
  try {
    await engine?.session?.release?.()
  } catch {
    // ORT/WebGPU cleanup is best-effort in the browser.
  }
}

function withTimeout(promise, ms, label = 'operation') {
  return new Promise((resolve, reject) => {
    const timer = setTimeout(() => reject(new Error(`${label} timeout after ${ms}ms`)), ms)
    promise.then(
      (value) => {
        clearTimeout(timer)
        resolve(value)
      },
      (error) => {
        clearTimeout(timer)
        reject(error)
      },
    )
  })
}

function lexicalRerank(query, docs) {
  const terms = extractTerms(query)
  return docs
    .map((doc, index) => ({
      index,
      text: doc,
      content: doc,
      score: lexicalScore(terms, doc),
    }))
    .sort((a, b) => b.score - a.score)
}

function extractTerms(text) {
  const seen = new Set()
  const terms = []
  for (const raw of String(text).split(/[\s,，。！？；;:：、()[\]{}"'“”‘’<>《》/\\]+/)) {
    const value = raw.trim().toLowerCase()
    if (!value) continue
    addTerm(value)
    if (/[\u4e00-\u9fff]/.test(value)) {
      const chars = Array.from(value)
      for (let n = 2; n <= Math.min(4, chars.length); n += 1) {
        for (let i = 0; i + n <= chars.length; i += 1) addTerm(chars.slice(i, i + n).join(''))
      }
    }
  }
  function addTerm(term) {
    if (term.length < 2 || seen.has(term)) return
    seen.add(term)
    terms.push(term)
  }
  return terms
}

function lexicalScore(terms, doc) {
  const text = String(doc || '').toLowerCase()
  if (!terms.length || !text) return 0
  let score = 0
  for (const term of terms) {
    let count = 0
    let from = 0
    while (count < 8) {
      const at = text.indexOf(term, from)
      if (at < 0) break
      count += 1
      from = at + term.length
    }
    score += count * Math.min(4, term.length)
  }
  return score / Math.max(1, Math.sqrt(text.length))
}

function openEmbeddingCache() {
  if (!self.indexedDB) return Promise.resolve(null)
  if (embeddingCacheDbPromise) return embeddingCacheDbPromise
  embeddingCacheDbPromise = new Promise((resolve) => {
    const request = self.indexedDB.open(EMBEDDING_CACHE_DB, 2)
    request.onupgradeneeded = () => {
      const db = request.result
      let store
      if (!db.objectStoreNames.contains(EMBEDDING_CACHE_STORE)) {
        store = db.createObjectStore(EMBEDDING_CACHE_STORE, { keyPath: 'key' })
      } else {
        store = request.transaction.objectStore(EMBEDDING_CACHE_STORE)
      }
      if (store && !store.indexNames.contains('updatedAt')) {
        store.createIndex('updatedAt', 'updatedAt')
      }
    }
    request.onsuccess = () => resolve(request.result)
    request.onerror = () => resolve(null)
  })
  return embeddingCacheDbPromise
}

async function embeddingCacheKey(text) {
  const normalized = `${EMBEDDING_CACHE_VERSION}\0${text}`
  if (!self.crypto?.subtle) return normalized
  const bytes = new TextEncoder().encode(normalized)
  const hash = await self.crypto.subtle.digest('SHA-256', bytes)
  return Array.from(new Uint8Array(hash)).map((value) => value.toString(16).padStart(2, '0')).join('')
}

async function embeddingCacheGet(key) {
  const db = await openEmbeddingCache()
  if (!db) return null
  return new Promise((resolve) => {
    const tx = db.transaction(EMBEDDING_CACHE_STORE, 'readonly')
    const req = tx.objectStore(EMBEDDING_CACHE_STORE).get(key)
    req.onsuccess = () => resolve(req.result?.vector || null)
    req.onerror = () => resolve(null)
  })
}

async function embeddingCachePut(key, vector) {
  const db = await openEmbeddingCache()
  if (!db) return
  await new Promise((resolve) => {
    const tx = db.transaction(EMBEDDING_CACHE_STORE, 'readwrite')
    tx.objectStore(EMBEDDING_CACHE_STORE).put({ key, vector, updatedAt: Date.now() })
    tx.oncomplete = () => resolve()
    tx.onerror = () => resolve()
  })
  embeddingCacheWrites += 1
  if (embeddingCacheWrites % 50 === 0) {
    pruneEmbeddingCache(db).catch(() => {})
  }
}

async function embeddingCacheClear() {
  const db = await openEmbeddingCache()
  if (!db) return
  return new Promise((resolve) => {
    const tx = db.transaction(EMBEDDING_CACHE_STORE, 'readwrite')
    tx.objectStore(EMBEDDING_CACHE_STORE).clear()
    tx.oncomplete = () => resolve()
    tx.onerror = () => resolve()
  })
}

async function pruneEmbeddingCache(db) {
  const count = await new Promise((resolve) => {
    const tx = db.transaction(EMBEDDING_CACHE_STORE, 'readonly')
    const req = tx.objectStore(EMBEDDING_CACHE_STORE).count()
    req.onsuccess = () => resolve(Number(req.result || 0))
    req.onerror = () => resolve(0)
  })
  const removeCount = count - EMBEDDING_CACHE_MAX_ENTRIES
  if (removeCount <= 0) return
  await new Promise((resolve) => {
    const tx = db.transaction(EMBEDDING_CACHE_STORE, 'readwrite')
    const index = tx.objectStore(EMBEDDING_CACHE_STORE).index('updatedAt')
    const cursorReq = index.openCursor()
    let removed = 0
    cursorReq.onsuccess = () => {
      const cursor = cursorReq.result
      if (!cursor || removed >= removeCount) return
      cursor.delete()
      removed += 1
      cursor.continue()
    }
    tx.oncomplete = () => resolve()
    tx.onerror = () => resolve()
  })
}
