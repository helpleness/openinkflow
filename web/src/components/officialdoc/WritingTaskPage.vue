<script setup>
import { computed, onBeforeUnmount, ref, watch } from 'vue'

import { createDocumentReviewComment, createWritingTask, exportWritingTaskVersion, getDocumentVersionDiff, getWritingRun, getWritingTask, listDocumentReviewComments, listDocumentTemplates, listWritingRuns, listWritingTasks, pauseWritingRun, resolveDocumentReviewComment, resumeWritingRun, saveWritingTaskVersion, startWritingRun, streamWritingRun, validateDocumentVersion } from '../../officialdocApi'

const props = defineProps({ tenantId: { type: Number, required: true }, organizationId: { type: Number, required: true } })
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
const comparison = ref(null)
const reviewComments = ref([])
const reviewDraft = ref('')
const reviewAnchor = ref({ start: 0, end: 0, quote: '' })
const governanceLoading = ref(false)
const exportLoading = ref('')
let runStreamController = null

const activeVersion = computed(() => selectedTask.value?.versions?.find((item) => item.id === selectedVersionID.value) || selectedTask.value?.versions?.[0] || null)
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
  manualContent.value = activeVersion.value?.content || ''
  validation.value = null
  comparison.value = null
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
  try { validation.value = await validateDocumentVersion(props.tenantId, selectedTask.value.id, activeVersion.value.id) } catch (error) { showError(error) } finally { governanceLoading.value = false }
}

async function compareActiveVersion() {
  if (!selectedTask.value || !activeVersion.value) return
  governanceLoading.value = true
  try { comparison.value = await getDocumentVersionDiff(props.tenantId, selectedTask.value.id, activeVersion.value.id) } catch (error) { showError(error) } finally { governanceLoading.value = false }
}

function captureReviewAnchor(event) {
  const start = Number(event?.target?.selectionStart || 0)
  const end = Number(event?.target?.selectionEnd || 0)
  reviewAnchor.value = { start, end, quote: start === end ? '' : manualContent.value.slice(start, end) }
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
  manualContent.value = activeVersion.value?.content || ''
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
    manualContent.value = activeVersion.value?.content || ''
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
  selectedTask.value = null
  activeRun.value = null
  runHistory.value = []
  manualContent.value = ''
  selectedVersionID.value = 0
  validation.value = null
  comparison.value = null
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
  savingVersion.value = true
  try {
    selectedTask.value = await saveWritingTaskVersion(props.tenantId, selectedTask.value.id, { stage: 'manual', content: manualContent.value })
    selectedVersionID.value = selectedTask.value.versions?.[0]?.id || 0
    emit('notice', { text: '人工修订已保存为新的不可变版本。' })
    await load()
    await loadRuns(selectedTask.value.id)
  } catch (error) {
    showError(error)
  } finally {
    savingVersion.value = false
  }
}

watch(() => [props.tenantId, props.organizationId], load, { immediate: true })
onBeforeUnmount(closeRunStream)
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
      <p class="muted">任务必须关联模板。每次运行或人工保存都会创建新版本，不会覆盖旧稿。</p>
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
        <header><strong>{{ activeVersion ? 'v' + activeVersion.version + ' · ' + stageLabel(activeVersion.stage) : '尚未生成版本' }}</strong><small v-if="activeVersion?.model">模型：{{ activeVersion.model }}</small></header>
        <textarea v-model="manualContent" rows="18" placeholder="生成的内容或人工修订会在这里显示。" @select="captureReviewAnchor" @keyup="captureReviewAnchor" />
        <div class="editor-actions"><button class="secondary" type="button" :disabled="savingVersion" @click="saveManual">{{ savingVersion ? '保存中…' : '保存人工修订为新版本' }}</button><button class="secondary" type="button" :disabled="!activeVersion || !!exportLoading" @click="exportActiveVersion('md')">{{ exportLoading === 'md' ? '正在导出…' : '导出 Markdown' }}</button><button class="secondary" type="button" :disabled="!activeVersion || !!exportLoading" @click="exportActiveVersion('docx')">{{ exportLoading === 'docx' ? '正在导出…' : '导出 DOCX' }}</button><button class="secondary" type="button" :disabled="!activeVersion || !!exportLoading" @click="exportActiveVersion('pdf')">{{ exportLoading === 'pdf' ? '正在导出…' : '导出 PDF' }}</button></div>

        <section v-if="activeVersion" class="governance-panel">
          <header><div><strong>审校与合规</strong><small>检查基于当前不可变版本；在正文中选中文字后再填写批注，可保留引用定位。</small></div><div class="governance-actions"><button class="secondary" type="button" :disabled="governanceLoading" @click="validateActiveVersion">格式与敏感词检查</button><button class="secondary" type="button" :disabled="governanceLoading" @click="compareActiveVersion">与上一版对比</button></div></header>
          <div v-if="validation" :class="['validation-result', { failed: !validation.passed }]"><strong>{{ validation.passed ? '未发现阻断项' : '发现需处理的问题' }}</strong><p v-if="!validation.findings?.length">当前版本未命中内置格式、结构和敏感信息规则。</p><ul v-else><li v-for="(finding,index) in validation.findings" :key="finding.rule + '-' + index" :class="finding.severity"><b>{{ finding.category }}</b> · {{ finding.message }}<span v-if="finding.line">（第 {{ finding.line }} 行）</span><em v-if="finding.excerpt">{{ finding.excerpt }}</em></li></ul></div>
          <details v-if="comparison" class="version-diff" open><summary>与 {{ comparison.base_version_id ? '选定历史版本' : '空白版本' }} 的差异</summary><pre><template v-for="(segment,index) in comparison.segments" :key="index"><span :class="'diff-' + segment.kind">{{ segment.kind === 'added' ? '+ ' : segment.kind === 'removed' ? '- ' : '  ' }}{{ segment.text }}\n</span></template></pre></details>
          <div class="review-compose"><textarea v-model="reviewDraft" rows="3" placeholder="填写审阅意见；正文中选中文字后会作为引用附带。" /><div><small v-if="reviewAnchor.quote">已定位：{{ reviewAnchor.quote.length > 60 ? reviewAnchor.quote.slice(0, 60) + '…' : reviewAnchor.quote }}</small><small v-else>未选择正文时，将创建通用批注。</small><button class="primary" type="button" :disabled="governanceLoading || !reviewDraft.trim()" @click="submitReviewComment">提交批注</button></div></div>
          <div class="review-list"><p v-if="!reviewComments.length" class="muted">当前版本还没有批注。</p><article v-for="comment in reviewComments" :key="comment.id" :class="{ resolved: comment.status === 'resolved' }"><header><span><b>成员 #{{ comment.created_by }}</b><small>{{ new Date(comment.created_at).toLocaleString() }}</small></span><button class="text-button" type="button" :disabled="governanceLoading" @click="toggleReviewComment(comment)">{{ comment.status === 'resolved' ? '重新打开' : '标记已处理' }}</button></header><blockquote v-if="comment.quote">{{ comment.quote }}</blockquote><p>{{ comment.content }}</p></article></div>
        </section>

        <details v-if="activeVersion?.evidence?.length" class="evidence"><summary><span><strong>本版本证据快照</strong><small>{{ activeVersion.evidence.length }} 条证据</small></span><span>查看</span></summary><div class="evidence-list"><article v-for="(item,index) in activeVersion.evidence" :key="item.chunk_id + '-' + index"><strong>[E{{ index + 1 }}] 《{{ item.document_name }}》 · {{ item.title || '正文切片' }}</strong><p>{{ item.content }}</p></article></div></details>
      </section>
    </article>
  </section>
</template>

<style scoped>
.page-stack{display:grid;gap:24px;max-width:1240px}.panel{min-width:0;padding:clamp(24px,3vw,36px);border:1px solid #e2e8e4;border-radius:16px;background:#fff;box-shadow:0 1px 2px rgba(15,45,34,.03),0 14px 34px rgba(15,45,34,.045)}.panel-heading,.page-heading,.workbench-head{display:flex;align-items:flex-start;justify-content:space-between;gap:20px}.panel-heading{align-items:center;margin-bottom:14px}.panel-heading p,.eyebrow{margin:0;color:#5e8975;font-size:11px;font-weight:800;letter-spacing:.14em}.panel h2{margin:8px 0 0;color:#143b2f;font-size:clamp(22px,2vw,27px);letter-spacing:-.025em}.heading-actions,.generation-actions,.form-actions{display:flex;align-items:center;gap:10px;flex-wrap:wrap}.muted,.workbench-head>div>p:last-child{color:#66776f;line-height:1.65}.muted{max-width:720px;margin:0 0 18px}.directory-summary{display:flex;flex-wrap:wrap;gap:8px;margin:0 0 22px}.directory-summary span{padding:6px 10px;border:1px solid #e1e9e4;border-radius:999px;color:#60766b;background:#f8fbf9;font-size:12px}.directory-summary b{color:#1a654e}.empty{min-height:260px;display:grid;place-content:center;gap:8px;padding:45px 12px;border:1px dashed #cfddd5;border-radius:12px;color:#738278;background:#fbfdfc;text-align:center}.empty strong{color:#315d4c;font-size:18px}.task-row{position:relative;display:grid;width:100%;gap:5px;margin-top:10px;padding:18px 132px 18px 18px;border:1px solid #e4ebe6;border-radius:12px;color:inherit;background:#fff;text-align:left;box-shadow:0 1px 1px rgba(19,54,40,.02);cursor:pointer;transition:border-color .16s ease,box-shadow .16s ease,transform .16s ease}.task-row:hover,.task-row.active{border-color:#97c5ad;background:#fbfefc;box-shadow:0 10px 22px rgba(20,81,59,.08);transform:translateY(-1px)}.task-row strong{color:#1c4033;font-size:15px}.task-row small{color:#718078}.task-status{justify-self:start;padding:3px 8px;border-radius:999px;color:#267054;background:#e7f5eb;font-size:11px;font-weight:800}.row-action{position:absolute;top:50%;right:18px;transform:translateY(-50%);color:#176851;font-size:13px;font-weight:750}.primary,.secondary,.danger,.ghost,.text-button,.back-button,.generation-actions button,.mcp-run header button{min-height:40px;padding:0 14px;border-radius:9px;font:inherit;font-weight:750;cursor:pointer}.primary{border:0;color:#fff;background:#17694f;box-shadow:0 1px 2px rgba(13,60,44,.18)}.primary:hover{background:#105a43}.secondary,.ghost,.back-button,.generation-actions button,.mcp-run header button{border:1px solid #cfded5;color:#1c684f;background:#fff}.secondary:hover,.ghost:hover,.back-button:hover,.generation-actions button:hover,.mcp-run header button:hover{border-color:#91bda7;background:#f4faf6}.text-button{border:0;color:#176851;background:transparent}.back-button{margin-bottom:16px}.back-button:hover{transform:translateX(-2px)}.task-editor-page{max-width:970px}.form-guidance{display:flex;gap:13px;align-items:flex-start;margin:22px 0 0;padding:15px 16px;border:1px solid #dbece1;border-radius:12px;background:#f3faf6}.form-guidance>span{display:grid;width:27px;height:27px;flex:0 0 auto;place-items:center;border-radius:50%;color:#fff;background:#247457;font-size:11px;font-weight:800}.form-guidance strong{color:#1b543f;font-size:13px}.form-guidance p{max-width:680px;margin:4px 0 0;color:#61776b;font-size:13px;line-height:1.55}.task-form{display:grid;gap:18px;margin-top:26px}.task-form label{display:grid;gap:8px;color:#385b4d;font-size:13px;font-weight:750}.task-form input,.task-form textarea,.task-form select,.editor textarea{width:100%;padding:11px 13px;border:1px solid #cfdcd4;border-radius:9px;resize:vertical;color:#20382e;background:#fff;font:inherit;line-height:1.6;transition:border-color .16s ease,box-shadow .16s ease}.task-form textarea{min-height:104px}.task-form input:focus,.task-form textarea:focus,.task-form select:focus,.editor textarea:focus{border-color:#4f9d7a;outline:0;box-shadow:0 0 0 3px rgba(79,157,122,.13)}.form-actions{justify-content:flex-end;padding-top:7px}.workbench{margin-top:0}.workbench-head{margin-bottom:24px;padding-bottom:22px;border-bottom:1px solid #e6ece8}.workbench-head h2{margin-bottom:7px}.workbench-head>div>p:last-child{max-width:760px;margin:0}.generation-actions{justify-content:flex-end}.mcp-run{margin:0 0 22px;padding:18px;border:1px solid #cde3d5;border-radius:14px;background:linear-gradient(135deg,#f4fbf7,#eef8f2)}.mcp-run header{display:flex;justify-content:space-between;gap:12px;align-items:center}.mcp-run strong{color:#185c46}.run-error{margin:13px 0 0;padding:11px 12px;border:1px solid #f4d2cb;border-radius:9px;color:#9c3732;background:#fff4f2}.run-ledger{display:grid;grid-template-columns:repeat(2,minmax(0,1fr));gap:12px;margin-top:16px}.run-ledger>div{min-width:0;padding:14px;border:1px solid rgba(202,223,210,.8);border-radius:10px;background:rgba(255,255,255,.88)}.run-ledger h3{margin:0 0 8px;color:#315d4c;font-size:13px}.run-ledger p{margin:7px 0;color:#50665b;font-size:12px;line-height:1.55}.run-ledger b{color:#1f5d48}.run-ledger small{color:#708178}.message-list,.trace-list{display:grid;gap:8px}.run-message{padding-bottom:8px;border-bottom:1px solid #edf3ee}.tool-result{margin:0;border:1px solid #dce9e0;border-radius:8px;overflow:hidden;background:#fbfefc}.tool-result summary{display:flex;align-items:center;justify-content:space-between;gap:10px;padding:10px 11px;cursor:pointer;list-style:none}.tool-result summary::-webkit-details-marker{display:none}.tool-result summary::before{content:'›';margin-right:2px;color:#2d795d;font-size:17px;line-height:1;transition:transform .16s ease}.tool-result[open] summary::before{transform:rotate(90deg)}.tool-result-title{display:flex;min-width:0;flex:1;align-items:baseline;gap:7px}.tool-result-title b{overflow:hidden;text-overflow:ellipsis;white-space:nowrap}.tool-result-title small{white-space:nowrap}.tool-result-hint{color:#4f7765;font-size:11px;font-weight:700}.tool-result pre{max-height:320px;overflow:auto;margin:0;padding:11px;border-top:1px solid #e0ebe3;color:#566b60;background:#f5faf7;font:11px/1.6 ui-monospace,SFMono-Regular,Consolas,monospace;white-space:pre-wrap;overflow-wrap:anywhere}.versions{padding:20px 0;border-top:1px solid #e5ede7;border-bottom:1px solid #e5ede7}.versions>header{display:flex;align-items:center;justify-content:space-between;gap:12px;margin-bottom:12px;color:#365c4b}.versions>header>div{display:grid;gap:3px}.versions>header small{color:#74837a}.version-count{padding:5px 8px;border-radius:999px;color:#557368;background:#f0f6f2;font-size:12px}.version-list{display:flex;flex-wrap:wrap;gap:8px}.versions button{display:grid;gap:4px;min-width:136px;padding:11px 12px;border:1px solid #dce7df;border-radius:9px;color:#436155;background:#fbfdfb;text-align:left;cursor:pointer;transition:border-color .16s ease,background .16s ease}.versions button:hover{border-color:#a7c9b5}.versions button.active{border-color:#5b9e7b;color:#174f3d;background:#eef8f1;box-shadow:0 1px 5px rgba(30,69,49,.08)}.versions small{font-size:11px}.editor{min-width:0;padding-top:22px}.editor header{display:flex;justify-content:space-between;gap:12px;margin-bottom:10px;color:#365c4b}.editor header small{color:#74837a}.editor textarea{min-height:330px;background:#fcfefd}.editor-actions{display:flex;justify-content:flex-end;margin-top:12px}.evidence{margin-top:24px;padding-top:18px;border-top:1px solid #e5ede7}.evidence h3{margin:0 0 11px;color:#224d3e;font-size:16px}.evidence article{margin-top:9px;padding:13px 14px;border:1px solid #e0ebe3;border-radius:10px;background:#f8fbf9}.evidence strong{color:#2b6853;font-size:13px}.evidence p{margin:7px 0 0;color:#53675d;font-size:13px;line-height:1.65;white-space:pre-wrap}@media(max-width:760px){.panel{padding:22px}.panel-heading,.workbench-head{flex-direction:column}.heading-actions,.generation-actions{justify-content:flex-start}.task-row{padding-right:18px}.row-action{position:static;transform:none;margin-top:5px}.mcp-run header,.versions>header,.editor header{align-items:flex-start;flex-direction:column}.run-ledger{grid-template-columns:1fr}.form-actions{justify-content:flex-start}}@media(max-width:480px){.panel{padding:18px}.directory-summary{display:grid}.task-row{padding:15px}.generation-actions{width:100%}.generation-actions button{flex:1}.form-guidance{padding:13px}}
</style>

<style scoped>
.directory-toolbar{display:flex;align-items:center;justify-content:space-between;gap:18px;margin-bottom:22px}.directory-toolbar .directory-summary{justify-content:flex-end;margin:0}@media(max-width:760px){.directory-toolbar{align-items:flex-start;flex-direction:column}.directory-toolbar .directory-summary{justify-content:flex-start}}
</style>

<style scoped>
.versions{margin:0 0 22px;padding:0 0 20px;border-top:0;border-bottom:1px solid #e5ede7}.versions>header{margin-bottom:0}.versions>header>div{gap:7px}.version-picker{display:grid;gap:6px;color:#365c4b;font-size:13px;font-weight:800}.version-picker select{min-width:min(100%,430px);padding:10px 34px 10px 12px;border:1px solid #cfded5;border-radius:9px;color:#1d503e;background:#fff;font:inherit;font-weight:700;cursor:pointer}.version-picker select:focus{border-color:#4f9d7a;outline:0;box-shadow:0 0 0 3px rgba(79,157,122,.13)}.versions .version-picker+small{font-size:12px}.version-count{white-space:nowrap}.run-empty{display:grid;gap:5px;margin:0 0 22px;padding:18px;border:1px dashed #cfe0d6;border-radius:12px;color:#5e766a;background:#fbfdfc}.run-empty strong{color:#345f4d}.run-empty span{font-size:13px}.evidence{padding:0;border:1px solid #dfeae3;border-radius:11px;background:#fbfdfc;overflow:hidden}.evidence summary{display:flex;align-items:center;justify-content:space-between;gap:12px;padding:14px 15px;color:#224d3e;cursor:pointer;list-style:none}.evidence summary::-webkit-details-marker{display:none}.evidence summary::after{content:'›';color:#357458;font-size:18px;line-height:1;transition:transform .16s ease}.evidence[open] summary::after{transform:rotate(90deg)}.evidence summary>span:first-child{display:flex;align-items:baseline;gap:9px}.evidence summary small{color:#718278;font-size:12px}.evidence summary>span:last-child{color:#4f7765;font-size:12px;font-weight:750}.evidence-list{padding:0 14px 14px;border-top:1px solid #e5ede7}.governance-panel{display:grid;gap:15px;margin:22px 0;padding:18px;border:1px solid #d8e8dd;border-radius:12px;background:#f9fcfa}.governance-panel>header,.review-list article>header{display:flex;align-items:flex-start;justify-content:space-between;gap:12px}.governance-panel header strong{display:block;color:#245842}.governance-panel header small{display:block;margin-top:4px;color:#708178;font-size:12px;line-height:1.5}.governance-actions{display:flex;flex-wrap:wrap;gap:8px}.governance-actions button{min-height:36px}.validation-result{padding:13px;border:1px solid #cfe4d5;border-radius:9px;background:#f4fbf6}.validation-result.failed{border-color:#eccfca;background:#fff8f6}.validation-result>strong{color:#245842}.validation-result ul{display:grid;gap:8px;margin:10px 0 0;padding-left:20px}.validation-result li{color:#586d61;font-size:13px;line-height:1.55}.validation-result li.error{color:#9d3f36}.validation-result li.warning{color:#8a6324}.validation-result em{display:block;margin-top:3px;color:#7a8981;font-style:normal}.version-diff{border:1px solid #dce8e0;border-radius:9px;background:#fff;overflow:hidden}.version-diff summary{padding:11px 13px;color:#285d46;font-size:13px;font-weight:800;cursor:pointer}.version-diff pre{max-height:300px;overflow:auto;margin:0;padding:12px;border-top:1px solid #e6eee8;color:#576a60;background:#fbfdfb;white-space:pre-wrap}.diff-added{display:block;color:#176a4e;background:#e8f7ec}.diff-removed{display:block;color:#a84138;background:#fff0ee}.diff-unchanged{display:block;color:#809087}.review-compose{display:grid;gap:9px}.review-compose textarea{min-height:76px}.review-compose>div{display:flex;align-items:center;justify-content:space-between;gap:12px}.review-compose small{overflow:hidden;color:#718278;font-size:12px;text-overflow:ellipsis;white-space:nowrap}.review-list{display:grid;gap:9px}.review-list article{padding:12px 13px;border:1px solid #dfe9e2;border-radius:9px;background:#fff}.review-list article.resolved{opacity:.72;background:#f8fbf9}.review-list article header span{display:grid;gap:2px}.review-list article header small{color:#7b8b82;font-size:11px}.review-list article blockquote{margin:10px 0 0;padding:7px 10px;border-left:3px solid #7ab18f;color:#5c7066;background:#f3f8f4;font-size:13px;white-space:pre-wrap}.review-list article p{margin:10px 0 0;color:#40584d;line-height:1.6;white-space:pre-wrap}@media(max-width:760px){.version-picker select{width:100%;min-width:0}.governance-panel>header,.review-compose>div{align-items:stretch;flex-direction:column}.governance-actions button,.review-compose button{width:100%}}
</style>
