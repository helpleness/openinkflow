<script setup>
import { computed, onBeforeUnmount, ref, watch } from 'vue'

import { createDocumentReviewComment, createWritingTask, exportWritingTaskVersion, getDocumentVersionDiff, getWritingRun, getWritingTask, listDocumentReviewComments, listDocumentTemplates, listWritingRuns, listWritingTasks, pauseWritingRun, resolveDocumentReviewComment, resumeWritingRun, saveWritingTaskVersion, startWritingRun, streamWritingRun, updateWritingTaskManualVersion, validateDocumentVersion } from '../../officialdocApi'
import MarkdownRichEditor from './MarkdownRichEditor.vue'

const props = defineProps({ tenantId: { type: Number, required: true }, organizationId: { type: Number, required: true }, userId: { type: Number, default: 0 } })
const emit = defineEmits(['notice'])
const templates = ref([])
const tasks = ref([])
const selectedTask = ref(null)
const activeRun = ref(null)
const runHistory = ref([])
const loading = ref(false)
const creating = ref(false)
const runAction = ref(false)
const savingVersion = ref(false)
const editorMode = ref('list')
const form = ref({ template_id: 0, title: '', requirement: '', constraintsText: '' })
const manualContent = ref('')
const selectedVersionID = ref(0)
const validation = ref(null)
const validatedContent = ref('')
const comparison = ref(null)
const diffExpanded = ref(false)
const reviewComments = ref([])
const reviewDraft = ref('')
const reviewAnchor = ref({ start: 0, end: 0, quote: '' })
const governanceLoading = ref(false)
const exportLoading = ref('')
const autosaveStatus = ref('saved')
const lastSavedContent = ref('')
const AUTOSAVE_IDLE_MS = 15000
const AUTOSAVE_MIN_REMOTE_INTERVAL_MS = 30000
const AUTOSAVE_RETRY_MS = 30000
let runStreamController = null
let autosaveTimer = null
let autosaveRetryTimer = null
let lastRemoteSaveAt = 0

const activeVersion = computed(() => selectedTask.value?.versions?.find((item) => item.id === selectedVersionID.value) || selectedTask.value?.versions?.[0] || null)
const hasInlineValidation = computed(() => Boolean(validation.value?.findings?.length) && validatedContent.value === manualContent.value)
const autosaveLabel = computed(() => ({ saved: '已保存', local: '已本地暂存，稍后同步', saving: '正在保存…', failed: '本地已暂存，等待重试' }[autosaveStatus.value] || '已本地暂存'))

function readableDiffLine(value) {
  return String(value || '')
    .replace(/\\r/g, '')
    .replace(/\\n/g, '\n')
    .replace(/\\([\[\]_*`#])/g, '$1')
    .replace(/^(\s*)#{1,6}\s+/, '$1')
    .replace(/\*\*([^*]+)\*\*/g, '$1')
    .replace(/~~([^~]+)~~/g, '$1')
    .replace(/`([^`]+)`/g, '$1')
}

const diffLines = computed(() => {
  if (!comparison.value?.segments?.length) return []
  const lines = []
  let oldLine = 1
  let newLine = 1
  for (const segment of comparison.value.segments) {
    const kind = ['added', 'removed'].includes(segment.kind) ? segment.kind : 'unchanged'
    const chunks = readableDiffLine(segment.text).split('\n')
    if (chunks.length > 1 && chunks.at(-1) === '') chunks.pop()
    for (const text of chunks) {
      lines.push({ kind, oldNumber: kind === 'added' ? null : oldLine++, newNumber: kind === 'removed' ? null : newLine++, text })
    }
  }
  return lines
})

const diffStatistics = computed(() => ({
  added: diffLines.value.filter((line) => line.kind === 'added').length,
  removed: diffLines.value.filter((line) => line.kind === 'removed').length,
}))

function draftStorageKey() {
  if (!props.tenantId || !selectedTask.value?.id) return ''
  return `inkflow:writing-draft:${props.userId || 'session'}:${props.tenantId}:${selectedTask.value.id}`
}

function cacheDraft(content = manualContent.value) {
  const key = draftStorageKey()
  if (!key) return
  try {
    localStorage.setItem(key, JSON.stringify({ content, baseVersionID: Number(selectedVersionID.value), updatedAt: Date.now() }))
  } catch { /* A full or unavailable browser store must never block editing. */ }
}

function clearCachedDraft() {
  const key = draftStorageKey()
  if (!key) return
  try { localStorage.removeItem(key) } catch { /* Ignore unavailable browser storage. */ }
}

function persistLocalDraftIfNeeded() {
  if (!selectedTask.value) return
  if (manualContent.value.trim() === lastSavedContent.value && manualContent.value.trim()) { clearCachedDraft(); return }
  cacheDraft()
}

function clearAutosaveTimers() {
  if (autosaveTimer) { window.clearTimeout(autosaveTimer); autosaveTimer = null }
  if (autosaveRetryTimer) { window.clearTimeout(autosaveRetryTimer); autosaveRetryTimer = null }
}

function setEditorContent(content) {
  clearAutosaveTimers()
  manualContent.value = String(content || '')
  lastSavedContent.value = manualContent.value.trim()
  lastRemoteSaveAt = 0
  autosaveStatus.value = 'saved'
}

function restoreCachedDraft() {
  const key = draftStorageKey()
  if (!key) return
  try {
    const draft = JSON.parse(localStorage.getItem(key) || 'null')
    if (!draft || typeof draft.content !== 'string') return
    if (Number(draft.baseVersionID) !== Number(selectedVersionID.value)) { localStorage.removeItem(key); return }
    if (draft.content === manualContent.value) return
    manualContent.value = draft.content
    autosaveStatus.value = 'local'
    emit('notice', { text: '已恢复此浏览器中暂存的未保存草稿。' })
    queueAutosave()
  } catch { /* Treat invalid local data as absent. */ }
}

function queueAutosave() {
  if (!selectedTask.value) return
  cacheDraft()
  if (!manualContent.value.trim()) { autosaveStatus.value = 'local'; return }
  if (manualContent.value.trim() === lastSavedContent.value) { clearCachedDraft(); autosaveStatus.value = 'saved'; return }
  autosaveStatus.value = 'local'
  if (autosaveTimer) window.clearTimeout(autosaveTimer)
  const sinceLastRemoteSave = Date.now() - lastRemoteSaveAt
  const waitForRateLimit = Math.max(0, AUTOSAVE_MIN_REMOTE_INTERVAL_MS - sinceLastRemoteSave)
  const delay = Math.max(AUTOSAVE_IDLE_MS, waitForRateLimit)
  autosaveTimer = window.setTimeout(() => { autosaveTimer = null; void autosaveDraft() }, delay)
}

async function persistManualSnapshot(snapshot) {
  const target = activeVersion.value
  if (target?.stage === 'manual') {
    return {
      task: await updateWritingTaskManualVersion(props.tenantId, selectedTask.value.id, target.id, { content: snapshot }),
      versionID: target.id,
      created: false,
    }
  }
  const task = await saveWritingTaskVersion(props.tenantId, selectedTask.value.id, { stage: 'manual', content: snapshot })
  return { task, versionID: task.versions?.find((item) => item.stage === 'manual')?.id || 0, created: true }
}

async function autosaveDraft() {
  if (!selectedTask.value || savingVersion.value || autosaveStatus.value === 'saving') return
  const snapshot = manualContent.value.trim()
  if (!snapshot || snapshot === lastSavedContent.value) return
  autosaveStatus.value = 'saving'
  try {
    const saved = await persistManualSnapshot(snapshot)
    selectedTask.value = saved.task
    selectedVersionID.value = saved.versionID || selectedVersionID.value
    lastSavedContent.value = snapshot
    lastRemoteSaveAt = Date.now()
    if (manualContent.value.trim() === snapshot) {
      manualContent.value = snapshot
      clearCachedDraft()
      autosaveStatus.value = 'saved'
    } else {
      cacheDraft()
      autosaveStatus.value = 'local'
      queueAutosave()
    }
  } catch {
    cacheDraft()
    autosaveStatus.value = 'failed'
    if (autosaveRetryTimer) window.clearTimeout(autosaveRetryTimer)
    autosaveRetryTimer = window.setTimeout(() => { autosaveRetryTimer = null; void autosaveDraft() }, AUTOSAVE_RETRY_MS)
  }
}

function handleMarkdownChange(content) {
  manualContent.value = String(content || '')
  queueAutosave()
}

function captureReviewAnchor(selection) {
  const quote = String(selection?.quote || '').trim()
  const start = quote ? manualContent.value.indexOf(quote) : 0
  reviewAnchor.value = { start: start < 0 ? 0 : start, end: start < 0 ? 0 : start + quote.length, quote }
}

function clearValidation() {
  validation.value = null
  validatedContent.value = ''
}
function splitLines(value) { return String(value || '').split(/[\n,，]/).map((item) => item.trim()).filter(Boolean) }
function showError(error) { emit('notice', { type: 'error', text: error?.message || '写作任务操作未完成。' }) }
function stageLabel(stage) { return ({ outline: '大纲', draft: '草稿', manual: '人工修订' }[stage] || stage) }
function runStatusLabel(status) { return ({ queued: '排队中', running: '执行中', pause_requested: '正在暂停', paused: '已暂停', failed: '执行失败', completed: '已完成', canceled: '已取消' }[status] || status) }
function stepLabel(step) { return ({ retrieve_evidence: '检索并冻结证据', compose_document: '生成受控文档', commit_version: '固化版本', completed: '已完成' }[step] || step) }
function runIsActive(run = activeRun.value) { return ['queued', 'running', 'pause_requested'].includes(run?.status) }
function closeRunStream() { if (runStreamController) { runStreamController.abort(); runStreamController = null } }
function updateRunHistory(run) { runHistory.value = [run, ...runHistory.value.filter((item) => item.id !== run.id)] }
function findRunForVersion(versionID = selectedVersionID.value) { return runHistory.value.find((item) => Number(item.version_id) === Number(versionID)) || null }

async function loadRunForSelectedVersion() {
  closeRunStream()
  const run = findRunForVersion()
  if (!run) {
    activeRun.value = null
    return
  }
  activeRun.value = await getWritingRun(props.tenantId, run.id)
  updateRunHistory(activeRun.value)
  if (runIsActive(activeRun.value)) followRun(activeRun.value.id)
}

async function selectVersion() {
  setEditorContent(activeVersion.value?.content || '')
  restoreCachedDraft()
  clearValidation()
  comparison.value = null
  diffExpanded.value = false
  reviewDraft.value = ''
  reviewAnchor.value = { start: 0, end: 0, quote: '' }
  try {
    await Promise.all([loadRunForSelectedVersion(), loadComments()])
  } catch (error) {
    showError(error)
  }
}

async function loadComments() {
  if (!selectedTask.value || !activeVersion.value) { reviewComments.value = []; return }
  reviewComments.value = await listDocumentReviewComments(props.tenantId, selectedTask.value.id, activeVersion.value.id)
}

async function validateActiveVersion() {
  if (!selectedTask.value || !activeVersion.value) return
  governanceLoading.value = true
  try {
    validation.value = await validateDocumentVersion(props.tenantId, selectedTask.value.id, activeVersion.value.id)
    validatedContent.value = activeVersion.value.content || ''
  } catch (error) { showError(error) } finally { governanceLoading.value = false }
}

async function compareActiveVersion() {
  if (!selectedTask.value || !activeVersion.value) return
  governanceLoading.value = true
  try {
    comparison.value = await getDocumentVersionDiff(props.tenantId, selectedTask.value.id, activeVersion.value.id)
    diffExpanded.value = true
  } catch (error) { showError(error) } finally { governanceLoading.value = false }
}

async function submitReviewComment() {
  if (!selectedTask.value || !activeVersion.value || !reviewDraft.value.trim()) return
  governanceLoading.value = true
  try {
    const comment = await createDocumentReviewComment(props.tenantId, selectedTask.value.id, activeVersion.value.id, { content: reviewDraft.value.trim(), anchor_start: reviewAnchor.value.start, anchor_end: reviewAnchor.value.end, quote: reviewAnchor.value.quote })
    reviewComments.value = [...reviewComments.value, comment]
    reviewDraft.value = ''
    reviewAnchor.value = { start: 0, end: 0, quote: '' }
  } catch (error) { showError(error) } finally { governanceLoading.value = false }
}

async function toggleReviewComment(comment) {
  if (!selectedTask.value || !activeVersion.value) return
  governanceLoading.value = true
  try {
    const updated = await resolveDocumentReviewComment(props.tenantId, selectedTask.value.id, activeVersion.value.id, comment.id, comment.status !== 'resolved')
    reviewComments.value = reviewComments.value.map((item) => item.id === updated.id ? updated : item)
  } catch (error) { showError(error) } finally { governanceLoading.value = false }
}

async function exportActiveVersion(format) {
  if (!selectedTask.value || !activeVersion.value) return
  exportLoading.value = format
  try {
    const fallback = `${selectedTask.value.title || 'InkFlow_公文'}_v${activeVersion.value.version}.${format}`
    await exportWritingTaskVersion(props.tenantId, selectedTask.value.id, activeVersion.value.id, format, fallback)
  } catch (error) { showError(error) } finally { exportLoading.value = '' }
}

async function finishRun(run) {
  activeRun.value = run
  updateRunHistory(run)
  closeRunStream()
  if (run.status !== 'completed' || !selectedTask.value) return
  selectedTask.value = await getWritingTask(props.tenantId, selectedTask.value.id)
  selectedVersionID.value = selectedTask.value.versions?.[0]?.id || 0
  setEditorContent(activeVersion.value?.content || '')
  emit('notice', { text: '受控写作已完成，版本和证据快照已固化。' })
  await load()
  await loadRuns(selectedTask.value.id)
}

function followRun(runID) {
  if (!runID || !props.tenantId) return
  closeRunStream()
  const controller = new AbortController()
  runStreamController = controller
  void streamWritingRun(props.tenantId, runID, {
    run: (run) => { if (!controller.signal.aborted) { activeRun.value = run; updateRunHistory(run) } },
    done: async (run) => { if (!controller.signal.aborted) await finishRun(run) },
    error: (payload) => { if (!controller.signal.aborted) showError(new Error(payload?.message || '写作事件流已中断。')) },
  }, { signal: controller.signal }).catch((error) => {
    if (!controller.signal.aborted) showError(error)
  }).finally(() => {
    if (runStreamController === controller) runStreamController = null
  })
}

async function load() {
  if (!props.tenantId || !props.organizationId) {
    templates.value = []
    tasks.value = []
    selectedTask.value = null
    activeRun.value = null
    runHistory.value = []
    editorMode.value = 'list'
    closeRunStream()
    return
  }
  loading.value = true
  try {
    const [templateItems, taskItems] = await Promise.all([listDocumentTemplates(props.tenantId, props.organizationId), listWritingTasks(props.tenantId, props.organizationId)])
    templates.value = (templateItems || []).filter((item) => item.is_enabled)
    tasks.value = taskItems || []
    if (!form.value.template_id && templates.value[0]) form.value.template_id = templates.value[0].id
  } catch (error) {
    showError(error)
  } finally {
    loading.value = false
  }
}

async function loadRuns(taskID) {
  runHistory.value = await listWritingRuns(props.tenantId, taskID)
  await loadRunForSelectedVersion()
}

async function openTask(task) {
  try {
    closeRunStream()
    selectedTask.value = await getWritingTask(props.tenantId, task.id)
    selectedVersionID.value = selectedTask.value.versions?.[0]?.id || 0
    setEditorContent(activeVersion.value?.content || '')
    restoreCachedDraft()
    await loadRuns(task.id)
    await loadComments()
    editorMode.value = 'edit'
  } catch (error) {
    showError(error)
  }
}

function openCreate() {
  closeRunStream()
  selectedTask.value = null
  activeRun.value = null
  runHistory.value = []
  form.value = { template_id: templates.value[0]?.id || 0, title: '', requirement: '', constraintsText: '' }
  editorMode.value = 'create'
}

function backToList() {
  closeRunStream()
  clearAutosaveTimers()
  selectedTask.value = null
  activeRun.value = null
  runHistory.value = []
  manualContent.value = ''
  lastSavedContent.value = ''
  autosaveStatus.value = 'saved'
  selectedVersionID.value = 0
  validation.value = null
  validatedContent.value = ''
  comparison.value = null
  diffExpanded.value = false
  reviewComments.value = []
  editorMode.value = 'list'
}

async function create() {
  if (!props.organizationId) { showError(new Error('请先选择或加入一个组织。')); return }
  creating.value = true
  try {
    const task = await createWritingTask(props.tenantId, { organization_id: props.organizationId, template_id: form.value.template_id, title: form.value.title.trim(), requirement: form.value.requirement.trim(), constraints: splitLines(form.value.constraintsText) })
    emit('notice', { text: '写作任务已创建，下一步可检索证据后生成大纲或草稿。' })
    form.value = { template_id: templates.value[0]?.id || 0, title: '', requirement: '', constraintsText: '' }
    await load()
    await openTask(task)
  } catch (error) {
    showError(error)
  } finally {
    creating.value = false
  }
}

async function refreshRun() {
  if (!activeRun.value || !props.tenantId) return
  try {
    activeRun.value = await getWritingRun(props.tenantId, activeRun.value.id)
    updateRunHistory(activeRun.value)
    if (runIsActive()) { followRun(activeRun.value.id); return }
    if (activeRun.value.status === 'completed') { await finishRun(activeRun.value); return }
    await loadRuns(selectedTask.value.id)
  } catch (error) {
    showError(error)
  }
}

async function startRun(stage) {
  if (!selectedTask.value || runIsActive()) return
  runAction.value = true
  try {
    activeRun.value = await startWritingRun(props.tenantId, selectedTask.value.id, { stage, evidence_query: selectedTask.value.requirement, evidence_limit: 6 })
    updateRunHistory(activeRun.value)
    emit('notice', { text: stageLabel(stage) + '已提交，MCP 将依次检索证据、生成文稿并固化版本。' })
    followRun(activeRun.value.id)
  } catch (error) {
    showError(error)
  } finally {
    runAction.value = false
  }
}

async function pauseRun() {
  if (!activeRun.value) return
  runAction.value = true
  try {
    activeRun.value = await pauseWritingRun(props.tenantId, activeRun.value.id)
    updateRunHistory(activeRun.value)
    emit('notice', { text: '已请求暂停，当前工具完成或取消后会保存检查点。' })
  } catch (error) {
    showError(error)
  } finally {
    runAction.value = false
  }
}

async function resumeRun() {
  if (!activeRun.value) return
  runAction.value = true
  try {
    activeRun.value = await resumeWritingRun(props.tenantId, activeRun.value.id)
    updateRunHistory(activeRun.value)
    emit('notice', { text: 'MCP 工作流已恢复，将从未完成步骤继续。' })
    followRun(activeRun.value.id)
  } catch (error) {
    showError(error)
  } finally {
    runAction.value = false
  }
}

async function saveManual() {
  if (!selectedTask.value || !manualContent.value.trim()) { showError(new Error('请先输入要保存的版本正文。')); return }
  clearAutosaveTimers()
  savingVersion.value = true
  try {
    const snapshot = manualContent.value.trim()
    const saved = await persistManualSnapshot(snapshot)
    selectedTask.value = saved.task
    selectedVersionID.value = saved.versionID || selectedVersionID.value
    manualContent.value = snapshot
    lastSavedContent.value = snapshot
    lastRemoteSaveAt = Date.now()
    clearCachedDraft()
    autosaveStatus.value = 'saved'
    emit('notice', { text: saved.created ? '已创建人工修订工作版本，后续修改会直接更新此版本。' : '人工修订工作版本已更新。' })
    await load()
    await loadRuns(selectedTask.value.id)
  } catch (error) {
    showError(error)
  } finally {
    savingVersion.value = false
  }
}

watch(() => [props.tenantId, props.organizationId], load, { immediate: true })
onBeforeUnmount(() => { persistLocalDraftIfNeeded(); clearAutosaveTimers(); closeRunStream() })
</script>

<template>
  <section class="page-stack">
    <article v-if="editorMode === 'list'" class="panel task-directory">
      <div class="directory-toolbar"><div class="heading-actions"><button class="primary" type="button" :disabled="!props.organizationId || !templates.length" @click="openCreate">＋ 新建任务</button><button class="text-button" type="button" @click="load">刷新</button></div><div class="directory-summary" aria-label="任务概览"><span><b>{{ tasks.length }}</b> 个任务</span><span><b>{{ templates.length }}</b> 个可用模板</span><span>所有生成均保留版本和证据快照</span></div></div>
      <div v-if="loading" class="empty">正在读取写作任务…</div>
      <div v-else-if="!tasks.length" class="empty"><strong>还没有写作任务</strong><span>点击“新建任务”开始创建一个受控写作流程。</span></div>
      <button v-for="task in tasks" :key="task.id" type="button" :class="['task-row', { active: task.id === selectedTask?.id }]" @click="openTask(task)"><span class="task-status">{{ task.status }}</span><strong>{{ task.title }}</strong><small>{{ new Date(task.updated_at).toLocaleString() }} · 当前 v{{ task.current_version_id || '—' }}</small><span class="row-action">打开编辑</span></button>
    </article>

    <article v-else-if="editorMode === 'create'" class="panel task-editor-page">
      <header class="page-heading"><div><button class="back-button" type="button" @click="backToList">← 返回任务列表</button><p class="eyebrow">CONTROLLED WRITING</p><h2>新建受控写作任务</h2></div></header>
      <p class="muted">任务必须关联模板。模型运行会固化新版本；人工修订使用独立工作版本，后续保存会直接更新该版本。</p>
      <aside class="form-guidance"><span>01</span><div><strong>先定义写作边界</strong><p>写清受众、目标、已知事实与输出长度；后续步骤会依据此要求检索组织知识。</p></div></aside>
      <form class="task-form" @submit.prevent="create">
        <label>使用模板<select v-model.number="form.template_id" required><option :value="0" disabled>请选择已启用模板</option><option v-for="template in templates" :key="template.id" :value="template.id">{{ template.name }} · {{ template.category || '未分类' }}</option></select></label>
        <label>任务标题<input v-model="form.title" required placeholder="例如：2026 年度重点工作会议纪要" /></label>
        <label>写作要求<textarea v-model="form.requirement" required rows="7" placeholder="说明受众、目标、关键信息和期望长度。生成时会据此检索组织知识库。" /></label>
        <label>本任务约束（每行一个）<textarea v-model="form.constraintsText" rows="4" placeholder="例如：保留正式公文语气&#10;没有证据的数值标记待补充" /></label>
        <footer class="form-actions"><button class="secondary" type="button" @click="backToList">取消</button><button class="primary" type="submit" :disabled="creating || !templates.length">{{ creating ? '创建中…' : '创建并打开工作台' }}</button></footer>
      </form>
    </article>

    <article v-else-if="selectedTask" class="panel workbench">
      <header class="workbench-head">
        <div><button class="back-button" type="button" @click="backToList">← 返回任务列表</button><p class="eyebrow">MCP VERSIONED WORKBENCH</p><h2>{{ selectedTask.title }}</h2><p>{{ selectedTask.requirement }}</p></div>
        <div class="generation-actions"><button type="button" :disabled="runAction || runIsActive()" @click="startRun('outline')">生成大纲</button><button class="primary" type="button" :disabled="runAction || runIsActive()" @click="startRun('draft')">生成草稿</button><button v-if="runIsActive()" class="danger" type="button" :disabled="runAction" @click="pauseRun">暂停</button><button v-else-if="activeRun && ['paused','failed'].includes(activeRun.status)" class="secondary" type="button" :disabled="runAction" @click="resumeRun">恢复运行</button></div>
      </header>
      <section class="versions"><header><div><label class="version-picker"><span>版本记录</span><select v-model.number="selectedVersionID" aria-label="版本记录" @change="selectVersion"><option v-for="version in selectedTask.versions" :key="version.id" :value="version.id">v{{ version.version }} · {{ stageLabel(version.stage) }} · {{ new Date(version.created_at).toLocaleString() }}</option></select></label><small>选择版本后，会同步切换对应的多轮消息与工具轨迹。</small></div><span class="version-count">{{ selectedTask.versions?.length || 0 }} 个版本</span></header></section>
      <section v-if="activeRun" class="mcp-run"><header><div><p class="eyebrow">MCP RUN #{{ activeRun.id }}</p><strong>{{ runStatusLabel(activeRun.status) }} · {{ stepLabel(activeRun.current_step) }}</strong></div><button type="button" @click="refreshRun">刷新轨迹</button></header><p v-if="activeRun.failure_reason" class="run-error">{{ activeRun.failure_reason }}</p><div class="run-ledger"><div><h3>多轮消息</h3><div class="message-list"><template v-for="message in activeRun.messages" :key="message.id"><p v-if="message.role !== 'tool'" class="run-message"><b>{{ message.role }}</b> · {{ message.content }}</p><details v-else class="tool-result"><summary><span class="tool-result-title"><b>工具 {{ message.tool_name }}</b><small>返回结果</small></span><span class="tool-result-hint">查看</span></summary><pre>{{ message.content || '工具未返回内容。' }}</pre></details></template></div><p v-if="!activeRun.messages?.length" class="muted">运行已建立，等待第一轮工具调用。</p></div><div><h3>工具轨迹</h3><div class="trace-list"><details v-for="trace in activeRun.traces" :key="trace.id" class="tool-result"><summary><span class="tool-result-title"><b>{{ trace.tool_name }}</b><small>{{ trace.status }} · {{ trace.elapsed_ms }}ms</small></span><span class="tool-result-hint">查看</span></summary><pre>{{ trace.output_summary || trace.error || '工具未返回内容。' }}</pre></details></div><p v-if="!activeRun.traces?.length" class="muted">尚未执行工具。</p></div></div></section>
      <section v-else-if="activeVersion" class="run-empty"><strong>本版本暂无 MCP 运行轨迹</strong><span>{{ activeVersion.stage === 'manual' ? '人工修订版本不会生成 MCP 运行轨迹。' : '关联运行记录暂不可用。' }}</span></section>
      <section class="editor">
        <header><div><strong>{{ activeVersion ? 'v' + activeVersion.version + ' · ' + stageLabel(activeVersion.stage) : '尚未生成版本' }}</strong><small v-if="activeVersion?.model">模型：{{ activeVersion.model }}</small></div><span :class="['autosave-status', autosaveStatus]">{{ autosaveLabel }}</span></header>
        <MarkdownRichEditor :model-value="manualContent" :highlight-findings="hasInlineValidation ? validation?.findings || [] : []" @update:model-value="handleMarkdownChange" @selection-change="captureReviewAnchor" />
        <p v-if="validation && !hasInlineValidation" class="validation-stale">正文已修改；原位标注仅对应刚刚检查的不可变版本。保存后的新版本可重新检查。</p>
        <div class="editor-actions"><button class="secondary" type="button" :disabled="savingVersion || autosaveStatus === 'saving'" @click="saveManual">{{ savingVersion ? '保存中…' : '保存人工修订' }}</button><button class="secondary" type="button" :disabled="!activeVersion || !!exportLoading" @click="exportActiveVersion('md')">{{ exportLoading === 'md' ? '正在导出…' : '导出 Markdown' }}</button><button class="secondary" type="button" :disabled="!activeVersion || !!exportLoading" @click="exportActiveVersion('docx')">{{ exportLoading === 'docx' ? '正在导出…' : '导出 DOCX' }}</button><button class="secondary" type="button" :disabled="!activeVersion || !!exportLoading" @click="exportActiveVersion('pdf')">{{ exportLoading === 'pdf' ? '正在导出…' : '导出 PDF' }}</button></div>

        <section v-if="activeVersion" class="governance-panel">
          <header><div><strong>审校与合规</strong><small>检查基于当前不可变版本；在正文中选中文字后再填写批注，可保留引用定位。</small></div><div class="governance-actions"><button class="secondary" type="button" :disabled="governanceLoading" @click="validateActiveVersion">格式与敏感词检查</button><button class="secondary" type="button" :disabled="governanceLoading" @click="compareActiveVersion">与上一版对比</button></div></header>
          <div v-if="validation" :class="['validation-result', { failed: !validation.passed }]"><strong>{{ validation.passed ? '未发现阻断项' : '发现需处理的问题' }}</strong><p v-if="validation.findings?.length">已在正文中标红可定位的问题；以下清单保留规则说明，以及无法在正文中直接标注的问题。</p><p v-else>当前版本未命中内置格式、结构和敏感信息规则。</p><ul v-if="validation.findings?.length"><li v-for="(finding,index) in validation.findings" :key="finding.rule + '-' + index" :class="finding.severity"><b>{{ finding.category }}</b> · {{ finding.message }}<span v-if="finding.line">（第 {{ finding.line }} 行）</span><em v-if="finding.excerpt">{{ finding.excerpt }}</em></li></ul></div>
          <section v-if="comparison" class="version-diff" :class="{ expanded: diffExpanded }">
            <header><div><strong>版本差异</strong><small>当前版本与 {{ comparison.base_version_id ? '上一历史版本' : '空白版本' }} 对比</small></div><span><em class="diff-added-count">+{{ diffStatistics.added }}</em><em class="diff-removed-count">−{{ diffStatistics.removed }}</em><button class="secondary" type="button" @click="diffExpanded = !diffExpanded">{{ diffExpanded ? '收起差异' : '展开差异' }}</button></span></header>
            <div v-if="diffExpanded" class="repository-diff" role="region" aria-label="版本文本差异">
              <div v-if="!diffLines.length" class="diff-empty">两个版本内容一致。</div>
              <div v-for="(line,index) in diffLines" v-else :key="index" :class="['diff-line', line.kind]"><span class="diff-symbol">{{ line.kind === 'added' ? '+' : line.kind === 'removed' ? '−' : ' ' }}</span><span class="diff-line-number">{{ line.oldNumber || '' }}</span><span class="diff-line-number">{{ line.newNumber || '' }}</span><code>{{ line.text || ' ' }}</code></div>
            </div>
          </section>
          <div class="review-compose"><textarea v-model="reviewDraft" rows="3" placeholder="填写审阅意见；正文中选中文字后会作为引用附带。" /><div><small v-if="reviewAnchor.quote">已定位：{{ reviewAnchor.quote.length > 60 ? reviewAnchor.quote.slice(0, 60) + '…' : reviewAnchor.quote }}</small><small v-else>未选择正文时，将创建通用批注。</small><button class="primary" type="button" :disabled="governanceLoading || !reviewDraft.trim()" @click="submitReviewComment">提交批注</button></div></div>
          <div class="review-list"><p v-if="!reviewComments.length" class="muted">当前版本还没有批注。</p><article v-for="comment in reviewComments" :key="comment.id" :class="{ resolved: comment.status === 'resolved' }"><header><span><b>成员 #{{ comment.created_by }}</b><small>{{ new Date(comment.created_at).toLocaleString() }}</small></span><button class="text-button" type="button" :disabled="governanceLoading" @click="toggleReviewComment(comment)">{{ comment.status === 'resolved' ? '重新打开' : '标记已处理' }}</button></header><blockquote v-if="comment.quote">{{ comment.quote }}</blockquote><p>{{ comment.content }}</p></article></div>
        </section>

        <details v-if="activeVersion?.evidence?.length" class="evidence"><summary><span><strong>本版本证据快照</strong><small>{{ activeVersion.evidence.length }} 条证据</small></span><span>查看</span></summary><div class="evidence-list"><article v-for="(item,index) in activeVersion.evidence" :key="item.chunk_id + '-' + index"><strong>[E{{ index + 1 }}] 《{{ item.document_name }}》 · {{ item.title || '正文切片' }}</strong><p>{{ item.content }}</p></article></div></details>
      </section>
    </article>
  </section>
</template>

<style scoped>
.version-diff{overflow:hidden;border:1px solid #dce8e0;border-radius:10px;background:#fff}.version-diff>header{display:flex;align-items:center;justify-content:space-between;gap:14px;padding:12px 14px;background:#fbfdfc}.version-diff>header>div{display:grid;gap:2px}.version-diff>header strong{color:#285d46;font-size:13px}.version-diff>header small{color:#75877d;font-size:12px}.version-diff>header>span{display:flex;align-items:center;gap:8px}.version-diff em{padding:3px 7px;border-radius:999px;font-size:11px;font-style:normal;font-weight:800}.diff-added-count{color:#176a4e;background:#e8f7ec}.diff-removed-count{color:#a84138;background:#fff0ee}.version-diff button{min-height:32px;padding:0 10px;font-size:12px}.repository-diff{max-height:440px;overflow:auto;border-top:1px solid #e6eee8;background:#fff;font:12px/1.62 ui-monospace,SFMono-Regular,Consolas,monospace}.diff-empty{padding:20px;color:#72837a;text-align:center}.diff-line{display:grid;grid-template-columns:25px 48px 48px minmax(0,1fr);min-width:640px;border-bottom:1px solid rgba(224,235,227,.7)}.diff-line:last-child{border-bottom:0}.diff-line span{padding:3px 8px;color:#91a097;background:rgba(245,249,246,.8);text-align:right;user-select:none}.diff-line .diff-symbol{padding-left:9px;color:#8a9a91;text-align:center}.diff-line code{min-width:0;padding:3px 10px;color:#40584d;background:transparent;font:inherit;white-space:pre-wrap;overflow-wrap:anywhere}.diff-line.added,.diff-line.added span{background:#e8f7ec}.diff-line.removed,.diff-line.removed span{background:#fff0ee}.diff-line.added .diff-symbol{color:#176a4e;font-weight:900}.diff-line.removed .diff-symbol{color:#a84138;font-weight:900}.diff-line.added code{color:#176a4e}.diff-line.removed code{color:#a84138}@media(max-width:760px){.version-diff>header{align-items:flex-start;flex-direction:column}.version-diff>header>span{width:100%;justify-content:flex-start}.repository-diff{margin-right:-1px;margin-left:-1px}.diff-line{min-width:520px;grid-template-columns:23px 40px 40px minmax(0,1fr)}}
</style>

<style scoped>
.page-stack{display:grid;gap:24px;max-width:1240px}.panel{min-width:0;padding:clamp(24px,3vw,36px);border:1px solid #e2e8e4;border-radius:16px;background:#fff;box-shadow:0 1px 2px rgba(15,45,34,.03),0 14px 34px rgba(15,45,34,.045)}.panel-heading,.page-heading,.workbench-head{display:flex;align-items:flex-start;justify-content:space-between;gap:20px}.panel-heading{align-items:center;margin-bottom:14px}.panel-heading p,.eyebrow{margin:0;color:#5e8975;font-size:11px;font-weight:800;letter-spacing:.14em}.panel h2{margin:8px 0 0;color:#143b2f;font-size:clamp(22px,2vw,27px);letter-spacing:-.025em}.heading-actions,.generation-actions,.form-actions{display:flex;align-items:center;gap:10px;flex-wrap:wrap}.muted,.workbench-head>div>p:last-child{color:#66776f;line-height:1.65}.muted{max-width:720px;margin:0 0 18px}.directory-summary{display:flex;flex-wrap:wrap;gap:8px;margin:0 0 22px}.directory-summary span{padding:6px 10px;border:1px solid #e1e9e4;border-radius:999px;color:#60766b;background:#f8fbf9;font-size:12px}.directory-summary b{color:#1a654e}.empty{min-height:260px;display:grid;place-content:center;gap:8px;padding:45px 12px;border:1px dashed #cfddd5;border-radius:12px;color:#738278;background:#fbfdfc;text-align:center}.empty strong{color:#315d4c;font-size:18px}.task-row{position:relative;display:grid;width:100%;gap:5px;margin-top:10px;padding:18px 132px 18px 18px;border:1px solid #e4ebe6;border-radius:12px;color:inherit;background:#fff;text-align:left;box-shadow:0 1px 1px rgba(19,54,40,.02);cursor:pointer;transition:border-color .16s ease,box-shadow .16s ease,transform .16s ease}.task-row:hover,.task-row.active{border-color:#97c5ad;background:#fbfefc;box-shadow:0 10px 22px rgba(20,81,59,.08);transform:translateY(-1px)}.task-row strong{color:#1c4033;font-size:15px}.task-row small{color:#718078}.task-status{justify-self:start;padding:3px 8px;border-radius:999px;color:#267054;background:#e7f5eb;font-size:11px;font-weight:800}.row-action{position:absolute;top:50%;right:18px;transform:translateY(-50%);color:#176851;font-size:13px;font-weight:750}.primary,.secondary,.danger,.ghost,.text-button,.back-button,.generation-actions button,.mcp-run header button{min-height:40px;padding:0 14px;border-radius:9px;font:inherit;font-weight:750;cursor:pointer}.primary{border:0;color:#fff;background:#17694f;box-shadow:0 1px 2px rgba(13,60,44,.18)}.primary:hover{background:#105a43}.secondary,.ghost,.back-button,.generation-actions button,.mcp-run header button{border:1px solid #cfded5;color:#1c684f;background:#fff}.secondary:hover,.ghost:hover,.back-button:hover,.generation-actions button:hover,.mcp-run header button:hover{border-color:#91bda7;background:#f4faf6}.text-button{border:0;color:#176851;background:transparent}.back-button{margin-bottom:16px}.back-button:hover{transform:translateX(-2px)}.task-editor-page{max-width:970px}.form-guidance{display:flex;gap:13px;align-items:flex-start;margin:22px 0 0;padding:15px 16px;border:1px solid #dbece1;border-radius:12px;background:#f3faf6}.form-guidance>span{display:grid;width:27px;height:27px;flex:0 0 auto;place-items:center;border-radius:50%;color:#fff;background:#247457;font-size:11px;font-weight:800}.form-guidance strong{color:#1b543f;font-size:13px}.form-guidance p{max-width:680px;margin:4px 0 0;color:#61776b;font-size:13px;line-height:1.55}.task-form{display:grid;gap:18px;margin-top:26px}.task-form label{display:grid;gap:8px;color:#385b4d;font-size:13px;font-weight:750}.task-form input,.task-form textarea,.task-form select,.editor textarea{width:100%;padding:11px 13px;border:1px solid #cfdcd4;border-radius:9px;resize:vertical;color:#20382e;background:#fff;font:inherit;line-height:1.6;transition:border-color .16s ease,box-shadow .16s ease}.task-form textarea{min-height:104px}.task-form input:focus,.task-form textarea:focus,.task-form select:focus,.editor textarea:focus{border-color:#4f9d7a;outline:0;box-shadow:0 0 0 3px rgba(79,157,122,.13)}.form-actions{justify-content:flex-end;padding-top:7px}.workbench{margin-top:0}.workbench-head{margin-bottom:24px;padding-bottom:22px;border-bottom:1px solid #e6ece8}.workbench-head h2{margin-bottom:7px}.workbench-head>div>p:last-child{max-width:760px;margin:0}.generation-actions{justify-content:flex-end}.mcp-run{margin:0 0 22px;padding:18px;border:1px solid #cde3d5;border-radius:14px;background:linear-gradient(135deg,#f4fbf7,#eef8f2)}.mcp-run header{display:flex;justify-content:space-between;gap:12px;align-items:center}.mcp-run strong{color:#185c46}.run-error{margin:13px 0 0;padding:11px 12px;border:1px solid #f4d2cb;border-radius:9px;color:#9c3732;background:#fff4f2}.run-ledger{display:grid;grid-template-columns:repeat(2,minmax(0,1fr));gap:12px;margin-top:16px}.run-ledger>div{min-width:0;padding:14px;border:1px solid rgba(202,223,210,.8);border-radius:10px;background:rgba(255,255,255,.88)}.run-ledger h3{margin:0 0 8px;color:#315d4c;font-size:13px}.run-ledger p{margin:7px 0;color:#50665b;font-size:12px;line-height:1.55}.run-ledger b{color:#1f5d48}.run-ledger small{color:#708178}.message-list,.trace-list{display:grid;gap:8px}.run-message{padding-bottom:8px;border-bottom:1px solid #edf3ee}.tool-result{margin:0;border:1px solid #dce9e0;border-radius:8px;overflow:hidden;background:#fbfefc}.tool-result summary{display:flex;align-items:center;justify-content:space-between;gap:10px;padding:10px 11px;cursor:pointer;list-style:none}.tool-result summary::-webkit-details-marker{display:none}.tool-result summary::before{content:'›';margin-right:2px;color:#2d795d;font-size:17px;line-height:1;transition:transform .16s ease}.tool-result[open] summary::before{transform:rotate(90deg)}.tool-result-title{display:flex;min-width:0;flex:1;align-items:baseline;gap:7px}.tool-result-title b{overflow:hidden;text-overflow:ellipsis;white-space:nowrap}.tool-result-title small{white-space:nowrap}.tool-result-hint{color:#4f7765;font-size:11px;font-weight:700}.tool-result pre{max-height:320px;overflow:auto;margin:0;padding:11px;border-top:1px solid #e0ebe3;color:#566b60;background:#f5faf7;font:11px/1.6 ui-monospace,SFMono-Regular,Consolas,monospace;white-space:pre-wrap;overflow-wrap:anywhere}.versions{padding:20px 0;border-top:1px solid #e5ede7;border-bottom:1px solid #e5ede7}.versions>header{display:flex;align-items:center;justify-content:space-between;gap:12px;margin-bottom:12px;color:#365c4b}.versions>header>div{display:grid;gap:3px}.versions>header small{color:#74837a}.version-count{padding:5px 8px;border-radius:999px;color:#557368;background:#f0f6f2;font-size:12px}.version-list{display:flex;flex-wrap:wrap;gap:8px}.versions button{display:grid;gap:4px;min-width:136px;padding:11px 12px;border:1px solid #dce7df;border-radius:9px;color:#436155;background:#fbfdfb;text-align:left;cursor:pointer;transition:border-color .16s ease,background .16s ease}.versions button:hover{border-color:#a7c9b5}.versions button.active{border-color:#5b9e7b;color:#174f3d;background:#eef8f1;box-shadow:0 1px 5px rgba(30,69,49,.08)}.versions small{font-size:11px}.editor{min-width:0;padding-top:22px}.editor header{display:flex;justify-content:space-between;gap:12px;margin-bottom:10px;color:#365c4b}.editor header small{color:#74837a}.editor textarea{min-height:330px;background:#fcfefd}.editor-actions{display:flex;justify-content:flex-end;margin-top:12px}.evidence{margin-top:24px;padding-top:18px;border-top:1px solid #e5ede7}.evidence h3{margin:0 0 11px;color:#224d3e;font-size:16px}.evidence article{margin-top:9px;padding:13px 14px;border:1px solid #e0ebe3;border-radius:10px;background:#f8fbf9}.evidence strong{color:#2b6853;font-size:13px}.evidence p{margin:7px 0 0;color:#53675d;font-size:13px;line-height:1.65;white-space:pre-wrap}@media(max-width:760px){.panel{padding:22px}.panel-heading,.workbench-head{flex-direction:column}.heading-actions,.generation-actions{justify-content:flex-start}.task-row{padding-right:18px}.row-action{position:static;transform:none;margin-top:5px}.mcp-run header,.versions>header,.editor header{align-items:flex-start;flex-direction:column}.run-ledger{grid-template-columns:1fr}.form-actions{justify-content:flex-start}}@media(max-width:480px){.panel{padding:18px}.directory-summary{display:grid}.task-row{padding:15px}.generation-actions{width:100%}.generation-actions button{flex:1}.form-guidance{padding:13px}}
</style>

<style scoped>
.markdown-toolbar{display:flex;align-items:center;justify-content:space-between;gap:12px;margin-bottom:10px;padding:8px 10px;border:1px solid #dfe9e2;border-radius:10px;background:#f8fbf9}.markdown-toolbar>span{color:#698077;font-size:12px}.markdown-toolbar>div{display:flex;align-items:center;gap:6px;flex-wrap:wrap}.markdown-toolbar button{min-height:30px;padding:0 9px;border:1px solid #d2e0d7;border-radius:6px;color:#285f4a;background:#fff;font:inherit;font-size:12px;cursor:pointer}.markdown-toolbar button:hover{border-color:#7aac91;background:#eff8f2}.markdown-toolbar .expand-editor{margin-left:5px;color:#17694f;font-weight:800}.autosave-status{padding:4px 8px;border-radius:999px;color:#557368;background:#eef5f0;font-size:11px;font-weight:800}.autosave-status.local{color:#8a6324;background:#fff6df}.autosave-status.saving{color:#286c96;background:#e9f5fc}.autosave-status.failed{color:#a24238;background:#fff0ed}.markdown-rich-editor{min-height:420px;padding:clamp(18px,2.5vw,30px);border:1px solid #cfded5;border-radius:12px;color:#213d32;background:#fcfefd;line-height:1.8;outline:0;overflow-wrap:anywhere;cursor:text}.markdown-rich-editor:focus{border-color:#4f9d7a;box-shadow:0 0 0 3px rgba(79,157,122,.13)}.markdown-rich-editor:empty::before{content:attr(data-placeholder);color:#91a099;pointer-events:none}.markdown-rich-editor :deep(h1),.markdown-rich-editor :deep(h2),.markdown-rich-editor :deep(h3),.markdown-rich-editor :deep(h4),.markdown-rich-editor :deep(h5),.markdown-rich-editor :deep(h6){margin:1.1em 0 .48em;color:#173f31;line-height:1.32}.markdown-rich-editor :deep(h1){font-size:2em}.markdown-rich-editor :deep(h2){font-size:1.55em}.markdown-rich-editor :deep(h3){font-size:1.25em}.markdown-rich-editor :deep(p){margin:.55em 0}.markdown-rich-editor :deep(ul),.markdown-rich-editor :deep(ol){margin:.6em 0;padding-left:1.55em}.markdown-rich-editor :deep(li+li){margin-top:.22em}.markdown-rich-editor :deep(blockquote){margin:.8em 0;padding:.2em 1em;border-left:3px solid #78a98c;color:#547064;background:#f4faf6}.markdown-rich-editor :deep(code){padding:.12em .34em;border-radius:4px;color:#8a4431;background:#fff1ed;font:12px ui-monospace,SFMono-Regular,Consolas,monospace}.markdown-rich-editor :deep(pre){padding:12px;border-radius:8px;color:#d8ede0;background:#173e30;overflow:auto}.markdown-rich-editor :deep(pre code){padding:0;color:inherit;background:transparent}.markdown-rich-editor :deep(.document-issue){padding:0 2px;border-radius:3px;color:#a52f2a;background:rgba(222,70,61,.16);box-shadow:inset 0 -2px 0 #dd463d}.markdown-rich-editor :deep(.document-issue.issue-sensitive){color:#962822;background:rgba(210,54,46,.19);box-shadow:inset 0 -2px 0 #c9342d}.editor-fullscreen{position:fixed;inset:0;z-index:1200;display:block;overflow:auto;padding:clamp(18px,3vw,46px);background:#f6faf7}.editor-fullscreen>header,.editor-fullscreen>.markdown-toolbar,.editor-fullscreen>.markdown-rich-editor,.editor-fullscreen>.editor-actions,.editor-fullscreen>.governance-panel,.editor-fullscreen>.evidence,.editor-fullscreen>.validation-stale{max-width:1080px;margin-right:auto;margin-left:auto}.editor-fullscreen .markdown-rich-editor{min-height:calc(100vh - 270px);background:#fff}@media(max-width:760px){.markdown-toolbar{align-items:stretch;flex-direction:column}.markdown-toolbar>div{justify-content:flex-start}.editor-fullscreen{padding:16px}.editor-fullscreen .markdown-rich-editor{min-height:calc(100vh - 310px)}}
</style>

<style scoped>
.document-editor-shell{position:relative;min-width:0}.document-editor-highlight{display:none;position:absolute;inset:0 16px 0 0;z-index:1;box-sizing:border-box;min-height:330px;margin:0;padding:11px 13px;overflow:hidden;color:#20382e;background:#fcfefd;font:inherit;line-height:1.6;white-space:pre-wrap;overflow-wrap:break-word;word-break:break-word;pointer-events:none}.document-editor-highlight mark{padding:0;border-radius:2px;color:#aa302a;background:rgba(222,70,61,.15);box-shadow:inset 0 -2px 0 #de463d}.document-editor-highlight mark.issue-sensitive{color:#9e2622;background:rgba(214,52,45,.18);box-shadow:inset 0 -2px 0 #c9342d}.document-editor-shell.has-inline-validation .document-editor-highlight{display:block}.document-editor-textarea{position:relative;z-index:2;scrollbar-gutter:stable}.document-editor-shell.has-inline-validation .document-editor-textarea{color:transparent;background:transparent;caret-color:#20382e}.document-editor-shell.has-inline-validation .document-editor-textarea::selection{color:transparent;background:rgba(83,143,231,.34)}.validation-stale{margin:9px 0 0;color:#9b6022;font-size:12px;line-height:1.55}
</style>

<style scoped>
.document-editor-shell{overflow:hidden;border-radius:9px}
.directory-toolbar{display:flex;align-items:center;justify-content:space-between;gap:18px;margin-bottom:22px}.directory-toolbar .directory-summary{justify-content:flex-end;margin:0}@media(max-width:760px){.directory-toolbar{align-items:flex-start;flex-direction:column}.directory-toolbar .directory-summary{justify-content:flex-start}}
</style>

<style scoped>
.versions{margin:0 0 22px;padding:0 0 20px;border-top:0;border-bottom:1px solid #e5ede7}.versions>header{margin-bottom:0}.versions>header>div{gap:7px}.version-picker{display:grid;gap:6px;color:#365c4b;font-size:13px;font-weight:800}.version-picker select{min-width:min(100%,430px);padding:10px 34px 10px 12px;border:1px solid #cfded5;border-radius:9px;color:#1d503e;background:#fff;font:inherit;font-weight:700;cursor:pointer}.version-picker select:focus{border-color:#4f9d7a;outline:0;box-shadow:0 0 0 3px rgba(79,157,122,.13)}.versions .version-picker+small{font-size:12px}.version-count{white-space:nowrap}.run-empty{display:grid;gap:5px;margin:0 0 22px;padding:18px;border:1px dashed #cfe0d6;border-radius:12px;color:#5e766a;background:#fbfdfc}.run-empty strong{color:#345f4d}.run-empty span{font-size:13px}.evidence{padding:0;border:1px solid #dfeae3;border-radius:11px;background:#fbfdfc;overflow:hidden}.evidence summary{display:flex;align-items:center;justify-content:space-between;gap:12px;padding:14px 15px;color:#224d3e;cursor:pointer;list-style:none}.evidence summary::-webkit-details-marker{display:none}.evidence summary::after{content:'›';color:#357458;font-size:18px;line-height:1;transition:transform .16s ease}.evidence[open] summary::after{transform:rotate(90deg)}.evidence summary>span:first-child{display:flex;align-items:baseline;gap:9px}.evidence summary small{color:#718278;font-size:12px}.evidence summary>span:last-child{color:#4f7765;font-size:12px;font-weight:750}.evidence-list{padding:0 14px 14px;border-top:1px solid #e5ede7}.governance-panel{display:grid;gap:15px;margin:22px 0;padding:18px;border:1px solid #d8e8dd;border-radius:12px;background:#f9fcfa}.governance-panel>header,.review-list article>header{display:flex;align-items:flex-start;justify-content:space-between;gap:12px}.governance-panel header strong{display:block;color:#245842}.governance-panel header small{display:block;margin-top:4px;color:#708178;font-size:12px;line-height:1.5}.governance-actions{display:flex;flex-wrap:wrap;gap:8px}.governance-actions button{min-height:36px}.validation-result{padding:13px;border:1px solid #cfe4d5;border-radius:9px;background:#f4fbf6}.validation-result.failed{border-color:#eccfca;background:#fff8f6}.validation-result>strong{color:#245842}.validation-result ul{display:grid;gap:8px;margin:10px 0 0;padding-left:20px}.validation-result li{color:#586d61;font-size:13px;line-height:1.55}.validation-result li.error{color:#9d3f36}.validation-result li.warning{color:#8a6324}.validation-result em{display:block;margin-top:3px;color:#7a8981;font-style:normal}.version-diff{border:1px solid #dce8e0;border-radius:9px;background:#fff;overflow:hidden}.version-diff summary{padding:11px 13px;color:#285d46;font-size:13px;font-weight:800;cursor:pointer}.version-diff pre{max-height:300px;overflow:auto;margin:0;padding:12px;border-top:1px solid #e6eee8;color:#576a60;background:#fbfdfb;white-space:pre-wrap}.diff-added{display:block;color:#176a4e;background:#e8f7ec}.diff-removed{display:block;color:#a84138;background:#fff0ee}.diff-unchanged{display:block;color:#809087}.review-compose{display:grid;gap:9px}.review-compose textarea{min-height:76px}.review-compose>div{display:flex;align-items:center;justify-content:space-between;gap:12px}.review-compose small{overflow:hidden;color:#718278;font-size:12px;text-overflow:ellipsis;white-space:nowrap}.review-list{display:grid;gap:9px}.review-list article{padding:12px 13px;border:1px solid #dfe9e2;border-radius:9px;background:#fff}.review-list article.resolved{opacity:.72;background:#f8fbf9}.review-list article header span{display:grid;gap:2px}.review-list article header small{color:#7b8b82;font-size:11px}.review-list article blockquote{margin:10px 0 0;padding:7px 10px;border-left:3px solid #7ab18f;color:#5c7066;background:#f3f8f4;font-size:13px;white-space:pre-wrap}.review-list article p{margin:10px 0 0;color:#40584d;line-height:1.6;white-space:pre-wrap}@media(max-width:760px){.version-picker select{width:100%;min-width:0}.governance-panel>header,.review-compose>div{align-items:stretch;flex-direction:column}.governance-actions button,.review-compose button{width:100%}}
</style>
