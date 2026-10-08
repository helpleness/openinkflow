<script setup>
import { onBeforeUnmount, ref, watch } from 'vue'
import { deleteKnowledgeDocument, getKnowledgeDocument, getKnowledgeDocumentDownload, importKnowledgeDocument, listKnowledgeDocuments, reindexKnowledgeDocument, reprocessKnowledgeDocument, streamKnowledgeDocument } from '../../officialdocApi'

const props = defineProps({ tenantId: { type:Number, required:true }, organizationId: { type:Number, required:true } })
const emit = defineEmits(['notice'])
const documents = ref([])
const selectedFile = ref(null)
const selectedDetail = ref(null)
const loading = ref(false)
const importing = ref(false)
const busyDocumentId = ref(0)
const importDialogOpen = ref(false)
const documentStreams = new Map()

function showError(error) { emit('notice',{ type:'error', text:error?.message || '操作未完成，请稍后重试。' }) }
function isProcessing(document) { return document?.status === 'processing' || document?.status === 'indexing' }
function canReprocess(document) { return ['processing_failed','ready','index_failed'].includes(document?.status) }
function stageText(stage) { return ({ queued:'已上传，等待处理', parsing:'正在解析正文', analyzing_images:'正在分析图片', chunking:'正在生成切片', indexing:'正在建立索引', completed:'处理完成', failed:'处理失败' }[stage] || '处理中') }
function statusText(document) {
  if (isProcessing(document)) return `${stageText(document.processing_stage)} ${Math.max(0, Math.min(100, Number(document.processing_progress) || 0))}%`
  return ({ processing_failed:'处理失败', ready:'可检索', index_failed:'索引失败', delete_failed:'删除待重试', imported:'已导入' }[document?.status] || document?.status || '待处理')
}
function onFileChange(event) { selectedFile.value=event.target.files?.[0] || null }
function replaceDocument(document) {
  if (!document?.id) return
  const index = documents.value.findIndex(item => item.id === document.id)
  if (index < 0) documents.value.unshift(document)
  else documents.value[index] = { ...documents.value[index], ...document }
}
function stopDocumentStream(documentID) {
  const controller = documentStreams.get(documentID)
  if (controller) controller.abort()
  documentStreams.delete(documentID)
}
function syncDocumentStreams() {
  const activeIDs = new Set(documents.value.filter(isProcessing).map(document => document.id))
  for (const [documentID] of documentStreams) if (!activeIDs.has(documentID)) stopDocumentStream(documentID)
  for (const document of documents.value) if (isProcessing(document)) watchDocument(document)
}
function watchDocument(document) {
  if (!document?.id || documentStreams.has(document.id) || !props.tenantId) return
  const controller = new AbortController()
  documentStreams.set(document.id, controller)
  streamKnowledgeDocument(props.tenantId, document.id, {
    document: update => replaceDocument(update),
    done: update => replaceDocument(update),
  }, { signal: controller.signal }).catch(error => {
    if (error?.name !== 'AbortError') console.warn('knowledge document SSE disconnected', error)
  }).finally(() => documentStreams.delete(document.id))
}
async function loadDocuments() {
  if (!props.tenantId || !props.organizationId) { documents.value=[]; syncDocumentStreams(); return }
  loading.value=true
  try {
    documents.value=await listKnowledgeDocuments(props.tenantId, props.organizationId) || []
    syncDocumentStreams()
  } catch (error) { showError(error) } finally { loading.value=false }
}
function openImport() { selectedFile.value=null; importDialogOpen.value=true }
async function upload() {
  if (!selectedFile.value) { showError(new Error('请选择要导入的文件。')); return }
  importing.value=true
  try {
    const document=await importKnowledgeDocument(props.tenantId,props.organizationId,selectedFile.value)
    selectedFile.value=null
    importDialogOpen.value=false
    replaceDocument(document)
    watchDocument(document)
    emit('notice',{ text:'文件已上传，正在后台解析并建立索引。' })
  } catch (error) { showError(error) } finally { importing.value=false }
}
async function inspect(document) { busyDocumentId.value=document.id; try { selectedDetail.value=await getKnowledgeDocument(props.tenantId,document.id) } catch (error) { showError(error) } finally { busyDocumentId.value=0 } }
async function download(document) { busyDocumentId.value=document.id; try { const result=await getKnowledgeDocumentDownload(props.tenantId,document.id); window.open(result.url, '_blank', 'noopener,noreferrer') } catch (error) { showError(error) } finally { busyDocumentId.value=0 } }
async function reindex(document) { busyDocumentId.value=document.id; try { const result=await reindexKnowledgeDocument(props.tenantId,document.id); replaceDocument(result); emit('notice',{ text:result?.failure_reason ? '已记录索引失败：' + result.failure_reason : '索引任务已完成。' }) } catch (error) { showError(error) } finally { busyDocumentId.value=0 } }
async function reprocess(document) { busyDocumentId.value=document.id; try { const result=await reprocessKnowledgeDocument(props.tenantId,document.id); replaceDocument(result); watchDocument(result); emit('notice',{ text:'已重新排队解析原文件。' }) } catch (error) { showError(error) } finally { busyDocumentId.value=0 } }
async function remove(document) { if (!window.confirm('确定删除“' + (document.original_name || document.name) + '”及其切片吗？')) return; busyDocumentId.value=document.id; try { await deleteKnowledgeDocument(props.tenantId,document.id); stopDocumentStream(document.id); documents.value=documents.value.filter(item => item.id !== document.id); if (selectedDetail.value?.document?.id === document.id) selectedDetail.value=null; emit('notice',{ text:'文档、切片和检索索引已删除。' }) } catch (error) { showError(error) } finally { busyDocumentId.value=0 } }
watch(() => [props.tenantId,props.organizationId], () => { for (const documentID of documentStreams.keys()) stopDocumentStream(documentID); loadDocuments() }, { immediate:true })
onBeforeUnmount(() => { for (const documentID of documentStreams.keys()) stopDocumentStream(documentID) })
</script>

<template>
  <section class="page-stack">
    <article class="panel">
      <header class="panel-heading panel-actions-only"><div class="heading-actions"><button class="primary" type="button" :disabled="!props.organizationId" @click="openImport">＋ 导入文档</button><button class="text-button" type="button" :disabled="loading" @click="loadDocuments">刷新</button></div></header>
      <div v-if="!props.organizationId" class="hint warning">请先选择或加入一个组织，再导入组织知识。</div>
      <div v-if="loading" class="empty-state">正在读取文档目录…</div>
      <div v-else-if="!documents.length" class="empty-state">当前组织还没有导入知识文档。</div>
      <div v-else class="document-list">
        <article v-for="document in documents" :key="document.id" class="document-row">
          <div class="file-mark">{{ (document.original_name || document.name || '?').slice(0,1).toUpperCase() }}</div>
          <div class="document-main">
            <strong>{{ document.original_name || document.name }}</strong>
            <span>{{ document.content_type || '未知格式' }} · {{ document.chunk_count || 0 }} 个切片 · {{ new Date(document.created_at).toLocaleString() }}</span>
            <div v-if="isProcessing(document)" class="progress-detail"><span>{{ stageText(document.processing_stage) }}</span><strong>{{ document.processing_progress || 0 }}%</strong><i><b :style="{ width: `${document.processing_progress || 0}%` }"></b></i></div>
            <small v-if="document.failure_reason" class="failure">{{ document.failure_reason }}</small>
          </div>
          <span :class="['status', 'status-' + document.status]">{{ statusText(document) }}</span>
          <div class="row-actions">
            <button type="button" :disabled="busyDocumentId === document.id || isProcessing(document)" @click="inspect(document)">切片</button>
            <button type="button" :disabled="busyDocumentId === document.id" @click="download(document)">下载原文件</button>
            <button v-if="canReprocess(document)" type="button" :disabled="busyDocumentId === document.id" @click="reprocess(document)">重新解析</button>
            <button v-if="!isProcessing(document) && document.status !== 'delete_failed'" type="button" :disabled="busyDocumentId === document.id" @click="reindex(document)">重建索引</button>
            <button class="danger" type="button" :disabled="busyDocumentId === document.id || isProcessing(document)" @click="remove(document)">删除</button>
          </div>
        </article>
      </div>
    </article>
    <article v-if="selectedDetail" class="panel"><header class="panel-heading"><div><p>CHUNK PREVIEW</p><h2>{{ selectedDetail.document.original_name || selectedDetail.document.name }} · 切片</h2></div><button class="text-button" type="button" @click="selectedDetail=null">收起</button></header><div class="chunk-list"><article v-for="chunk in selectedDetail.chunks" :key="chunk.id" class="chunk"><span>#{{ chunk.chunk_index + 1 }}</span><div><strong>{{ chunk.title || chunk.parent_title || '正文切片' }}</strong><pre>{{ chunk.content }}</pre></div></article></div></article>
    <div v-if="importDialogOpen" class="modal-backdrop" @click.self="importDialogOpen=false"><section class="modal-card" role="dialog" aria-modal="true" aria-labelledby="import-dialog-title"><header class="modal-heading"><div><p>KNOWLEDGE INGESTION</p><h2 id="import-dialog-title">导入知识文档</h2></div><button class="icon-button" type="button" aria-label="关闭" @click="importDialogOpen=false">×</button></header><p class="muted">文件上传完成后会立即显示在目录中，解析、图片分析、切片和索引将在后台进行。</p><label class="drop-zone" :class="{ selected:selectedFile }"><input type="file" accept=".md,.markdown,.txt,.csv,.pdf,.docx,.xlsx,.pptx" @change="onFileChange" /><strong>{{ selectedFile ? selectedFile.name : '选择知识文档' }}</strong><span>{{ selectedFile ? Math.ceil(selectedFile.size / 1024) + ' KB' : '支持 .md / .txt / .csv / .pdf / .docx / .xlsx / .pptx' }}</span></label><footer class="modal-actions"><button class="ghost" type="button" @click="importDialogOpen=false">取消</button><button class="primary" type="button" :disabled="!selectedFile || importing" @click="upload">{{ importing ? '正在上传…' : '上传并后台处理' }}</button></footer></section></div>
  </section>
</template>

<style scoped>
.page-stack{display:grid;gap:24px;max-width:1240px}.panel{min-width:0;padding:clamp(24px,3vw,36px);border:1px solid #e2e9e4;border-radius:16px;background:#fff;box-shadow:0 1px 2px rgba(15,45,34,.03),0 14px 34px rgba(15,45,34,.045)}.panel-heading,.modal-heading{display:flex;align-items:flex-start;justify-content:space-between;gap:16px}.panel-heading{align-items:center;margin-bottom:14px}.panel-heading p,.modal-heading p{margin:0;color:#5e8975;font-size:11px;font-weight:800;letter-spacing:.14em}.panel-heading h2,.modal-heading h2{margin:8px 0 0;color:#143b2f;font-size:clamp(22px,2vw,27px);letter-spacing:-.025em}.heading-actions,.modal-actions{display:flex;align-items:center;gap:10px}.helper,.muted{color:#66776f;font-size:13px;line-height:1.65}.helper{max-width:740px;margin:0 0 20px}.hint{padding:12px 14px;border:1px solid #f0d49a;border-radius:10px;font-size:14px}.warning{color:#87551d;background:#fff8e8}.empty-state{min-height:300px;display:grid;place-items:center;padding:34px 12px;border:1px dashed #cfddd5;border-radius:12px;color:#718177;background:#fbfdfc;text-align:center}.document-list{display:grid;gap:10px}.document-row{display:grid;grid-template-columns:44px minmax(0,1fr) auto;gap:14px;align-items:center;padding:18px;border:1px solid #e4ebe6;border-radius:13px;background:#fff;box-shadow:0 1px 1px rgba(20,54,41,.02);transition:border-color .16s ease,box-shadow .16s ease,transform .16s ease}.document-row:hover{border-color:#a7c9b5;box-shadow:0 11px 24px rgba(25,79,59,.07);transform:translateY(-1px)}.file-mark{display:grid;width:42px;height:42px;place-items:center;border-radius:11px;color:#fff;background:linear-gradient(145deg,#3b9371,#17694f);font-weight:800}.document-main{display:grid;gap:5px;min-width:0}.document-main strong{color:#1c4033;font-size:15px}.document-main span,.failure{overflow:hidden;color:#60756a;font-size:12px;text-overflow:ellipsis;white-space:nowrap}.failure{color:#a33e35}.status{padding:5px 9px;border-radius:999px;font-size:12px;font-weight:750;background:#edf1ed;color:#597066}.status-ready{background:#e5f4e9;color:#287046}.status-indexing{background:#fff4d8;color:#956313}.status-index_failed{background:#ffecea;color:#a54239}.row-actions{grid-column:2 / -1;display:flex;flex-wrap:wrap;gap:12px}.row-actions button,.text-button,.primary,.ghost,.icon-button{font:inherit;cursor:pointer}.row-actions button,.text-button{border:0;color:#176851;background:transparent;font-weight:700}.row-actions .danger{color:#af4b42}.primary{min-height:40px;padding:0 14px;border:0;border-radius:9px;color:#fff;background:#17694f;box-shadow:0 1px 2px rgba(13,60,44,.18);font-weight:750}.primary:hover{background:#105a43}.ghost{min-height:40px;padding:0 14px;border:1px solid #cfded5;border-radius:9px;color:#176851;background:#fff;font-weight:750}.chunk-list{display:grid;gap:10px}.chunk{display:grid;grid-template-columns:36px minmax(0,1fr);gap:12px;padding:14px;border:1px solid #e2ebe4;border-radius:11px;background:#fbfdfc}.chunk>span{display:grid;width:30px;height:30px;place-items:center;border-radius:50%;color:#2e705b;background:#e5f2e9;font-size:12px;font-weight:800}.chunk strong{display:block;margin-bottom:6px;color:#254c3e}.chunk pre{margin:0;white-space:pre-wrap;word-break:break-word;color:#475b51;font:13px/1.7 ui-monospace,SFMono-Regular,Consolas,monospace}.modal-backdrop{position:fixed;z-index:60;inset:0;display:grid;place-items:center;padding:20px;background:rgba(14,28,22,.46)}.modal-card{width:min(560px,100%);max-height:calc(100vh - 40px);overflow:auto;padding:30px;border:1px solid #e1e9e3;border-radius:16px;background:#fff;box-shadow:0 24px 72px rgba(0,0,0,.22)}.modal-heading{align-items:flex-start}.icon-button{display:grid;width:32px;height:32px;place-items:center;border:0;border-radius:8px;color:#52645a;background:#eff3f0;font-size:23px;line-height:1}.drop-zone{display:grid;gap:5px;min-height:160px;place-content:center;margin:22px 0 14px;padding:20px;border:1px dashed #80af97;border-radius:13px;color:#3d6959;background:#f7fbf8;text-align:center;cursor:pointer}.drop-zone:hover,.drop-zone.selected{color:#1d6c54;border-style:solid;background:#eef8f1}.drop-zone input{position:absolute;width:1px;height:1px;opacity:0}.modal-actions{justify-content:flex-end;padding-top:7px}@media(max-width:700px){.panel{padding:20px}.heading-actions{flex-wrap:wrap;justify-content:flex-end}.document-row{grid-template-columns:42px minmax(0,1fr);padding:15px}.document-row>.status{grid-column:2}.row-actions{grid-column:2}.modal-card{padding:22px}}
.progress-detail{display:grid;grid-template-columns:minmax(0,1fr) auto;gap:5px 10px;align-items:center;color:#467563;font-size:12px}.progress-detail strong{color:#1d6c54;font-size:12px}.progress-detail i{grid-column:1 / -1;height:5px;overflow:hidden;border-radius:999px;background:#dcebe2}.progress-detail b{display:block;height:100%;border-radius:inherit;background:linear-gradient(90deg,#54a77f,#17694f);transition:width .35s ease}
</style>
