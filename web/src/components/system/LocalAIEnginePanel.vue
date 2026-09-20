<script setup>
import { computed } from 'vue'
import { BrainCircuit, Database, Loader2, Trash2 } from 'lucide-vue-next'

import {
  getBackendEmbeddingModelPath,
  getBackendRerankModelPath,
  isBackendEmbeddingReady,
  isBackendRerankReady,
  usesFrontendInference,
} from '../../api'
import { useLocalAI } from '../../ai/useLocalAI'

const props = defineProps({
  tenantId: { type: Number, default: 0 },
})

const { localAI, initLocalAI, clearLocalModelCache, releaseUnusedLocalWeights } = useLocalAI()
const frontendInference = usesFrontendInference()
const backendEmbeddingReady = isBackendEmbeddingReady()
const backendRerankReady = isBackendRerankReady()

const socketText = computed(() => ({
  connected: '已连接',
  connecting: '正在连接',
  disconnected: '未连接',
  'waiting-tenant': '等待租户上下文',
  timeout: '连接超时',
  superseded: '会话已替换',
  error: '连接错误',
}[localAI.socketStatus] || localAI.socketStatus))

const modelDownloads = computed(() => [
  { key: 'embedding', name: 'Embedding', ...localAI.downloads.embedding },
  { key: 'rerank', name: 'Reranker', ...localAI.downloads.rerank },
].filter((item) => item.active || item.state === 'error'))
const hasActiveDownload = computed(() => modelDownloads.value.some((item) => item.active))

function formatBytes(value) {
  const bytes = Number(value || 0)
  if (bytes < 1024) return `${bytes} B`
  if (bytes < 1024 ** 2) return `${(bytes / 1024).toFixed(1)} KB`
  if (bytes < 1024 ** 3) return `${(bytes / 1024 ** 2).toFixed(1)} MB`
  return `${(bytes / 1024 ** 3).toFixed(2)} GB`
}

function formatCache(cache) {
  if (!cache) return '准备中'
  return `${cache.hit ? '本地命中' : '首次写入完成'} · ${cache.files || 0} 个文件 · ${formatBytes(cache.totalBytes)}`
}

function downloadProgressText(download) {
  if (download.state === 'error') return `下载失败：${download.error || '请重试'}`
  if (download.total > 0) return `${download.progress}% · ${formatBytes(download.received)} / ${formatBytes(download.total)}`
  if (download.received > 0) return `已下载 ${formatBytes(download.received)} · 等待服务器提供总大小`
  return download.state === 'checking' ? '正在检查浏览器缓存…' : '正在下载…'
}

function start() {
  initLocalAI({ tenantID: props.tenantId })
}
</script>

<template>
  <section class="engine-card">
    <header>
      <div><p>LOCAL AI ENGINE</p><h2>本地 AI 引擎</h2></div>
      <BrainCircuit :size="21" />
    </header>

    <template v-if="frontendInference">
      <p class="description">用于本地知识库计算（ONNX Runtime WebGPU）。模型只写入浏览器的持久化缓存。</p>
      <div class="engine-status" :class="{ running: hasActiveDownload }">
        <strong>状态：{{ localAI.status }}</strong>
        <small>WebSocket：{{ socketText }}</small>
        <small>Embedding：{{ localAI.models.embedding }}</small>
        <small>Embedding 缓存：{{ formatCache(localAI.cache.embedding) }}</small>
        <small>Reranker 缓存：{{ formatCache(localAI.cache.rerank) }}</small>
        <div v-if="modelDownloads.length" class="download-list" aria-live="polite">
          <div v-for="download in modelDownloads" :key="download.key" class="download-item" :class="{ failed: download.state === 'error' }">
            <div class="download-meta"><span>{{ download.name }}：{{ download.file || '模型包' }}</span><strong>{{ downloadProgressText(download) }}</strong></div>
            <div class="download-track" :class="{ indeterminate: download.state === 'checking' || download.total <= 0, failed: download.state === 'error' }"><i v-if="download.state !== 'error'" :style="download.total > 0 ? { width: `${download.progress}%` } : undefined" /></div>
          </div>
        </div>
        <small v-if="localAI.memory.supported" :class="{ warning: localAI.memory.warning }">内存：{{ formatBytes(localAI.memory.used) }} / {{ formatBytes(localAI.memory.limit) }}（{{ localAI.memory.percent }}%）</small>
      </div>
      <div class="engine-actions">
        <button class="primary" type="button" :disabled="localAI.ready || !tenantId" @click="start">
          <Loader2 v-if="!localAI.ready && localAI.progress > 0" class="spin" :size="17" />
          <Database v-else :size="17" />{{ localAI.ready ? '已启动' : '初始化并缓存模型' }}
        </button>
        <button class="secondary" type="button" :disabled="!localAI.ready" @click="releaseUnusedLocalWeights"><BrainCircuit :size="17" />释放权重</button>
        <button class="secondary danger" type="button" @click="clearLocalModelCache"><Trash2 :size="17" />删除缓存</button>
      </div>
    </template>

    <template v-else>
      <p class="description">Embedding 与 Rerank 由安装包内的 llama.cpp 后端执行，不会加载浏览器 WebGPU worker。</p>
      <div class="engine-status">
        <strong>状态：{{ backendEmbeddingReady && backendRerankReady ? '后端本地推理已就绪' : '后端模型尚未完全加载' }}</strong>
        <small>Embedding：{{ backendEmbeddingModelPath || '未配置' }}</small>
        <small>Rerank：{{ backendRerankModelPath || '未配置' }}</small>
        <small>模型路径在客户端配置文件中维护，修改后重启 InkFlow 生效。</small>
      </div>
    </template>
  </section>
</template>

<style scoped>
.engine-card{padding:24px;border:1px solid #dce5df;border-radius:14px;background:#fff;box-shadow:0 8px 20px rgba(30,58,44,.04)}header{display:flex;align-items:center;justify-content:space-between;gap:16px}header p{margin:0;color:#5c796c;font-size:11px;font-weight:800;letter-spacing:.12em}h2{margin:5px 0 0;font-size:21px}.description{margin:16px 0;color:#60736a;font-size:13px;line-height:1.65}.engine-status{display:grid;gap:7px;padding:14px;border:1px solid #d8e2d9;border-radius:8px;color:#5d6f66;background:#f7faf8}.engine-status.running{border-color:#e7c186;background:#fff9ef}.engine-status strong{color:#254b40;font-size:13px}.engine-status small{font-size:12px;line-height:1.5}.download-list{display:grid;gap:10px;margin-top:4px;padding-top:10px;border-top:1px solid #e3ebe5}.download-item{display:grid;gap:6px}.download-meta{display:flex;gap:12px;align-items:baseline;justify-content:space-between;color:#446356;font-size:12px}.download-meta span{overflow:hidden;text-overflow:ellipsis;white-space:nowrap}.download-meta strong{flex:none;color:#1d6854;font-size:11px;font-weight:700}.download-item.failed .download-meta strong{color:#a1493d}.download-track{position:relative;overflow:hidden;height:7px;border-radius:999px;background:#dce9e0}.download-track i{display:block;width:0;height:100%;border-radius:inherit;background:linear-gradient(90deg,#2a8064,#5db88d);transition:width .16s ease}.download-track.indeterminate i{width:34%;animation:download-sweep 1.1s ease-in-out infinite}.download-track.failed{background:#f4d9d5}@keyframes download-sweep{0%{transform:translateX(-110%)}100%{transform:translateX(320%)}}.warning{color:#a95a29}.engine-actions{display:flex;flex-wrap:wrap;gap:10px;margin-top:14px}.primary,.secondary{display:flex;min-height:40px;align-items:center;justify-content:center;gap:7px;padding:0 13px;border-radius:8px;font-weight:700}.primary{border:1px solid #1e6956;color:#fff;background:#1e6956}.secondary{border:1px solid #b9d2c5;color:#1b6854;background:#f5faf6}.danger{border-color:#e3c6c2;color:#994137;background:#fff7f6}.primary:disabled,.secondary:disabled{cursor:not-allowed;opacity:.55}.spin{animation:spin .9s linear infinite}@keyframes spin{to{transform:rotate(360deg)}}
</style>

<style scoped>
.engine-card{padding:26px;border-top:3px solid #5b876f}.engine-status{min-height:190px;padding:18px;background:#f7faf8}.engine-actions{padding-top:4px}.engine-actions button{min-height:42px}
</style>
