<script setup>
import { computed, ref, watch } from 'vue'
import { CheckCircle2, Pencil, Puzzle, ServerCog, Trash2, X } from 'lucide-vue-next'

import {
  createAISkill,
  createMCPServer,
  deleteAISkill,
  deleteMCPServer,
  listAISkills,
  listMCPServers,
  updateAISkill,
  updateMCPServer,
} from '../../systemApi'

const props = defineProps({ tenantId: { type: Number, required: true } })
const emit = defineEmits(['notice'])

const activeTab = ref('mcp')
const loading = ref(false)
const saving = ref(false)
const servers = ref([])
const skills = ref([])
const editingServerId = ref(0)
const editingSkillId = ref(0)
const serverForm = ref(emptyServerForm())
const skillForm = ref(emptySkillForm())

const enabledServerCount = computed(() => servers.value.filter((item) => item.enabled).length)
const enabledSkillCount = computed(() => skills.value.filter((item) => item.enabled).length)

function emptyServerForm() { return { name: '', endpoint_url: '', bearer_token: '', clear_bearer_token: false, timeout_seconds: 60, enabled: true } }
function emptySkillForm() { return { name: '', description: '', instructions: '', enabled: true } }
function notifyError(error) { emit('notice', { type: 'error', text: error?.message || '工具与 Skill 操作未完成。' }) }

async function load() {
  if (!props.tenantId) return
  loading.value = true
  try {
    const [serverItems, skillItems] = await Promise.all([listMCPServers(props.tenantId), listAISkills(props.tenantId)])
    servers.value = serverItems || []
    skills.value = skillItems || []
  } catch (error) {
    notifyError(error)
  } finally {
    loading.value = false
  }
}

function resetServerForm() { editingServerId.value = 0; serverForm.value = emptyServerForm() }
function resetSkillForm() { editingSkillId.value = 0; skillForm.value = emptySkillForm() }

function editServer(server) {
  editingServerId.value = server.id
  serverForm.value = { name: server.name || '', endpoint_url: server.endpoint_url || '', bearer_token: '', clear_bearer_token: false, timeout_seconds: Number(server.timeout_seconds || 60), enabled: Boolean(server.enabled) }
  activeTab.value = 'mcp'
}

function editSkill(skill) {
  editingSkillId.value = skill.id
  skillForm.value = { name: skill.name || '', description: skill.description || '', instructions: skill.instructions || '', enabled: Boolean(skill.enabled) }
  activeTab.value = 'skills'
}

async function saveServer() {
  saving.value = true
  try {
    const form = serverForm.value
    const payload = { name: form.name.trim(), endpoint_url: form.endpoint_url.trim(), timeout_seconds: Number(form.timeout_seconds || 60), enabled: Boolean(form.enabled) }
    if (form.bearer_token.trim()) payload.bearer_token = form.bearer_token.trim()
    if (form.clear_bearer_token) payload.bearer_token = ''
    if (editingServerId.value) await updateMCPServer(props.tenantId, editingServerId.value, payload)
    else await createMCPServer(props.tenantId, payload)
    emit('notice', { type: 'success', text: editingServerId.value ? 'MCP 服务已更新。' : '远程 MCP 服务已添加。' })
    resetServerForm()
    await load()
  } catch (error) {
    notifyError(error)
  } finally {
    saving.value = false
  }
}

async function saveSkill() {
  saving.value = true
  try {
    const form = skillForm.value
    const payload = { name: form.name.trim(), description: form.description.trim(), instructions: form.instructions.trim(), enabled: Boolean(form.enabled) }
    if (editingSkillId.value) await updateAISkill(props.tenantId, editingSkillId.value, payload)
    else await createAISkill(props.tenantId, payload)
    emit('notice', { type: 'success', text: editingSkillId.value ? 'Skill 已更新。' : 'Skill 已添加。' })
    resetSkillForm()
    await load()
  } catch (error) {
    notifyError(error)
  } finally {
    saving.value = false
  }
}

async function removeServer(server) {
  if (!window.confirm(`删除远程 MCP 服务“${server.name}”？`)) return
  try { await deleteMCPServer(props.tenantId, server.id); emit('notice', { type: 'success', text: 'MCP 服务已删除。' }); await load() } catch (error) { notifyError(error) }
}

async function removeSkill(skill) {
  if (!window.confirm(`删除 Skill“${skill.name}”？`)) return
  try { await deleteAISkill(props.tenantId, skill.id); emit('notice', { type: 'success', text: 'Skill 已删除。' }); await load() } catch (error) { notifyError(error) }
}

watch(() => props.tenantId, load, { immediate: true })
</script>

<template>
  <section class="tool-skill-page">
    <header class="page-intro"><div><p>MCP & SKILLS</p><h2>工具与 Skill 技能</h2><span>AI 对话仅加载当前账号已启用的配置。MCP 仅支持远程 HTTPS Streamable HTTP，不会启动本机进程或执行本地命令。</span></div><div class="intro-counts"><span><ServerCog :size="15" />{{ enabledServerCount }} 个 MCP</span><span><Puzzle :size="15" />{{ enabledSkillCount }} 个 Skill</span></div></header>

    <div class="tabbar"><button :class="{ active: activeTab === 'mcp' }" type="button" @click="activeTab = 'mcp'"><ServerCog :size="16" />远程 MCP 工具</button><button :class="{ active: activeTab === 'skills' }" type="button" @click="activeTab = 'skills'"><Puzzle :size="16" />Skill 技能</button></div>

    <section v-if="activeTab === 'mcp'" class="tool-layout">
      <form class="editor-card compact-editor" @submit.prevent="saveServer">
        <label>服务名称<input v-model="serverForm.name" required maxlength="128" placeholder="例如：企业知识库" /></label>
        <label>Streamable HTTP 地址<input v-model="serverForm.endpoint_url" required inputmode="url" placeholder="https://mcp.example.com/mcp" /></label>
        <label>Bearer Token（可选）<input v-model="serverForm.bearer_token" type="password" autocomplete="new-password" placeholder="留空不会修改已有 Token" /></label>
        <label>超时秒数<input v-model.number="serverForm.timeout_seconds" required type="number" min="1" max="600" /></label>
        <label v-if="editingServerId" class="toggle-row toggle-row-danger"><span><strong>清除 Bearer Token</strong><small>保存后将移除已存密钥</small></span><input v-model="serverForm.clear_bearer_token" type="checkbox" /><i aria-hidden="true"></i></label>
        <label class="toggle-row"><span><strong>在 AI 对话中启用</strong><small>模型可按需调用此服务</small></span><input v-model="serverForm.enabled" type="checkbox" /><i aria-hidden="true"></i></label>
        <footer class="editor-actions"><button v-if="editingServerId" class="secondary" type="button" @click="resetServerForm"><X :size="15" />取消</button><button class="primary" type="submit" :disabled="saving"><CheckCircle2 :size="15" />{{ editingServerId ? '保存 MCP 服务' : '添加 MCP 服务' }}</button></footer>
      </form>
      <div class="card-list"><div v-if="loading" class="empty">正在读取 MCP 配置…</div><div v-else-if="!servers.length" class="empty"><ServerCog :size="26" /><strong>还没有远程 MCP 服务</strong><span>添加后，模型会在对话中按需发现和调用该服务提供的工具。</span></div><article v-for="server in servers" :key="server.id" class="config-card"><header><div><h3>{{ server.name }}</h3><p>{{ server.transport }}</p></div><span :class="['enabled', { off: !server.enabled }]">{{ server.enabled ? '已启用' : '已停用' }}</span></header><code>{{ server.endpoint_url }}</code><div class="meta"><span>超时 {{ server.timeout_seconds }} 秒</span><span>{{ server.has_bearer_token ? '已保存 Token' : '无 Token' }}</span></div><footer><button type="button" @click="editServer(server)"><Pencil :size="14" />编辑</button><button class="danger" type="button" @click="removeServer(server)"><Trash2 :size="14" />删除</button></footer></article></div>
    </section>

    <section v-else class="tool-layout">
      <form class="editor-card compact-editor" @submit.prevent="saveSkill">
        <label>Skill 名称<input v-model="skillForm.name" required maxlength="128" placeholder="例如：公文校对" /></label>
        <label>用途说明<input v-model="skillForm.description" maxlength="512" placeholder="例如：检查文稿的事实约束与正式表达" /></label>
        <label>指令内容<textarea v-model="skillForm.instructions" required rows="7" maxlength="12000" placeholder="写给 AI 的可复用工作指令。请明确范围、输出要求和不能做的事。"></textarea></label>
        <label class="toggle-row"><span><strong>在 AI 对话中启用</strong><small>Skill 只作为提示词，不具备执行权限</small></span><input v-model="skillForm.enabled" type="checkbox" /><i aria-hidden="true"></i></label>
        <footer class="editor-actions"><button v-if="editingSkillId" class="secondary" type="button" @click="resetSkillForm"><X :size="15" />取消</button><button class="primary" type="submit" :disabled="saving"><CheckCircle2 :size="15" />{{ editingSkillId ? '保存 Skill' : '添加 Skill' }}</button></footer>
      </form>
      <div class="card-list"><div v-if="loading" class="empty">正在读取 Skill 配置…</div><div v-else-if="!skills.length" class="empty"><Puzzle :size="26" /><strong>还没有 Skill 技能</strong><span>例如创建“公文校对”“预算分析”或“会议纪要整理”等可复用指令。</span></div><article v-for="skill in skills" :key="skill.id" class="config-card"><header><div><h3>{{ skill.name }}</h3><p>{{ skill.description || '未填写用途说明' }}</p></div><span :class="['enabled', { off: !skill.enabled }]">{{ skill.enabled ? '已启用' : '已停用' }}</span></header><p class="skill-preview">{{ skill.instructions }}</p><footer><button type="button" @click="editSkill(skill)"><Pencil :size="14" />编辑</button><button class="danger" type="button" @click="removeSkill(skill)"><Trash2 :size="14" />删除</button></footer></article></div>
    </section>
  </section>
</template>

<style scoped>
.tool-skill-page{display:grid;gap:18px}.page-intro{display:flex;align-items:flex-start;justify-content:space-between;gap:20px;padding:22px 24px;border:1px solid #dce8df;border-radius:17px;background:linear-gradient(120deg,#f7fcf8,#fff);box-shadow:0 11px 28px rgba(31,61,47,.045)}.page-intro p,.editor-card header p{margin:0 0 5px;color:#5a856e;font-size:10px;font-weight:850;letter-spacing:.14em}.page-intro h2{margin:0;color:#173d2e;font-size:25px;letter-spacing:-.04em}.page-intro span{display:block;max-width:760px;margin-top:8px;color:#647b6e;font-size:13px;line-height:1.7}.intro-counts{display:flex;flex:none;gap:8px}.intro-counts span{display:inline-flex;align-items:center;gap:5px;margin:0;padding:7px 10px;border:1px solid #cae2d1;border-radius:999px;color:#276249;background:#eff9f1;font-size:11px;font-weight:800;white-space:nowrap}.tabbar{display:flex;gap:6px;padding:5px;border:1px solid #dce6df;border-radius:10px;background:#f3f8f4}.tabbar button{display:flex;align-items:center;justify-content:center;gap:7px;min-height:37px;padding:0 15px;border:0;border-radius:7px;color:#62786b;background:transparent;font:inherit;font-size:13px;font-weight:750;cursor:pointer}.tabbar button.active{color:#1e6047;background:#fff;box-shadow:0 3px 10px rgba(29,70,47,.09)}.tool-layout{display:grid;grid-template-columns:minmax(300px,.75fr) minmax(0,1.25fr);gap:18px;align-items:start}.editor-card,.config-card{padding:21px;border:1px solid #dce6df;border-radius:15px;background:#fff;box-shadow:0 10px 24px rgba(31,61,47,.045)}.editor-card{display:grid;gap:13px}.editor-card header,.config-card header{display:flex;align-items:flex-start;justify-content:space-between;gap:12px}.editor-card header{color:#1d7257}.editor-card h3,.config-card h3{margin:0;color:#234b3a;font-size:17px}.editor-card>p{margin:0;color:#75867c;font-size:12px;line-height:1.65}.editor-card label{display:grid;gap:6px;color:#3d5d4c;font-size:12px;font-weight:760}.editor-card input:not([type=checkbox]),.editor-card textarea{width:100%;box-sizing:border-box;padding:10px 11px;border:1px solid #cbdcd1;border-radius:8px;outline:0;color:#1f3329;background:#fbfefb;font:inherit;font-size:13px;line-height:1.55}.editor-card textarea{resize:vertical}.editor-card input:focus,.editor-card textarea:focus{border-color:#619d7b;box-shadow:0 0 0 3px rgba(62,131,92,.1)}.check-row{display:flex!important;grid-template-columns:none!important;align-items:center;gap:7px!important}.editor-card footer,.config-card footer{display:flex;justify-content:flex-end;gap:8px;margin-top:3px}.primary,.secondary,.config-card footer button{display:inline-flex;align-items:center;justify-content:center;gap:6px;min-height:35px;padding:0 11px;border:1px solid #c9ddd0;border-radius:8px;color:#32604a;background:#fff;font:inherit;font-size:12px;font-weight:780;cursor:pointer}.primary{border-color:#176149;color:#fff;background:#176149}.primary:disabled{cursor:not-allowed;opacity:.6}.secondary{background:#f7faf7}.config-card footer button:hover,.secondary:hover{background:#edf7ef}.config-card footer .danger{border-color:#f0cfca;color:#a43e34;background:#fff7f6}.card-list{display:grid;gap:12px}.config-card{display:grid;gap:12px}.config-card header p{margin:4px 0 0;color:#788980;font-size:11px}.config-card code{overflow:hidden;padding:8px 9px;border-radius:7px;color:#4b6357;background:#f2f7f3;font-size:11px;text-overflow:ellipsis;white-space:nowrap}.meta{display:flex;flex-wrap:wrap;gap:7px}.meta span,.enabled{display:inline-flex;width:max-content;align-items:center;padding:4px 7px;border-radius:999px;color:#276247;background:#eaf7ed;font-size:10px;font-weight:800}.enabled.off{color:#85623c;background:#fff2dc}.skill-preview{display:-webkit-box;overflow:hidden;margin:0;color:#526a5d;font-size:12px;line-height:1.7;-webkit-box-orient:vertical;-webkit-line-clamp:4}.empty{display:grid;min-height:210px;place-items:center;align-content:center;gap:8px;padding:16px;border:1px dashed #cfe0d4;border-radius:15px;color:#779087;text-align:center}.empty strong{color:#335a45;font-size:14px}.empty span{max-width:310px;font-size:12px;line-height:1.65}.security-note{display:flex;gap:8px;padding:11px 13px;border:1px solid #cfdfd3;border-radius:10px;color:#51715e;background:#f4faf5;font-size:12px;line-height:1.65}.security-note svg{flex:none;margin-top:2px;color:#34765a}@media(max-width:980px){.tool-layout{grid-template-columns:1fr}.page-intro{align-items:stretch;flex-direction:column}.intro-counts{flex-wrap:wrap}}@media(max-width:560px){.page-intro{padding:18px}.intro-counts{display:grid;grid-template-columns:1fr 1fr}.intro-counts span{justify-content:center}.tabbar button{flex:1;padding:0 7px;font-size:12px}.editor-card,.config-card{padding:17px}.security-note{font-size:11px}}
</style>

<style scoped>
.tool-skill-page { gap: 16px; }
.tool-layout { grid-template-columns: minmax(380px, .9fr) minmax(0, 1.1fr); align-items: start; min-height: 0; }
.compact-editor { min-height: 0; padding: 18px; gap: 11px; overflow: auto; align-content: start; }
.compact-editor textarea { min-height: 108px; }
.compact-editor label { gap: 5px; }
.toggle-row { display: grid !important; grid-template-columns: minmax(0, 1fr) auto; align-items: center; gap: 10px !important; min-height: 52px; padding: 8px 10px; border: 1px solid #d9e6dd; border-radius: 9px; color: #315a45; background: #f8fcf9; cursor: pointer; }
.toggle-row > span { display: grid; gap: 2px; min-width: 0; }
.toggle-row strong { font-size: 12px; }
.toggle-row small { color: #74867b; font-size: 10px; font-weight: 600; line-height: 1.4; }
.toggle-row > input { position: absolute; width: 1px; height: 1px; opacity: 0; pointer-events: none; }
.toggle-row > i { position: relative; width: 35px; height: 20px; border-radius: 999px; background: #bccdc2; transition: background .18s ease; }
.toggle-row > i::after { position: absolute; top: 3px; left: 3px; width: 14px; height: 14px; border-radius: 50%; background: #fff; box-shadow: 0 1px 2px rgba(24, 57, 39, .24); content: ''; transition: transform .18s ease; }
.toggle-row > input:checked + i { background: #1c7658; }
.toggle-row > input:checked + i::after { transform: translateX(15px); }
.toggle-row:focus-within { border-color: #69a483; box-shadow: 0 0 0 3px rgba(62, 131, 92, .1); }
.toggle-row-danger { border-color: #ead7d1; background: #fffbfa; }
.toggle-row-danger > input:checked + i { background: #b45143; }
.compact-editor > .editor-actions { display: flex !important; width: 100%; justify-self: stretch; justify-content: center !important; gap: 8px; margin-top: auto !important; padding-top: 4px; }
.card-list { min-width: 0; min-height: 0; padding-right: 3px; align-content: start; }
.card-list > .empty { min-height: 210px; box-sizing: border-box; }
.config-card { align-self: start; box-shadow: none; }

@media (max-width: 980px) {
  .tool-layout { grid-template-columns: 1fr; height: auto; min-height: 0; }
  .card-list { max-height: none; }
  .card-list > .empty { min-height: 210px; }
}
</style>
