<script setup>
import { computed, nextTick, onBeforeUnmount, onMounted, ref, watch } from 'vue'
import MarkdownIt from 'markdown-it'
import { Bot, ChevronRight, Clock3, Expand, FileText, ImagePlus, MessageSquarePlus, Minimize, Paperclip, Send, Sparkles, Trash2, Wrench, X } from 'lucide-vue-next'
import AIChatSkillSelector from './AIChatSkillSelector.vue'

import {
  createAIChatConversation,
  deleteAIChatConversation,
  getAIChatConversation,
  listAISkills,
  listAIChatConversations,
  streamAIChatMessage,
} from '../../systemApi'

const props = defineProps({ tenantId: { type: Number, required: true }, organizationId: { type: Number, required: true } })
const emit = defineEmits(['notice'])

const markdown = new MarkdownIt({ breaks: true, linkify: true, html: false })
const conversations = ref([])
const selectedConversationId = ref(0)
const messages = ref([])
const messageText = ref('')
const attachments = ref([])
const loading = ref(false)
const creating = ref(false)
const sending = ref(false)
const availableSkills = ref([])
const skillMode = ref('auto')
const manualSkillIDs = ref([])
const aiChatShell = ref(null)
const isFullscreen = ref(false)
const chatBody = ref(null)
const attachmentInput = ref(null)
const streamingAssistantID = ref('')

const currentConversation = computed(() => conversations.value.find((item) => item.id === selectedConversationId.value) || null)
const emptyState = computed(() => !messages.value.length && !sending.value)
const enabledSkills = computed(() => availableSkills.value.filter((skill) => skill.enabled))
const conversationGroups = computed(() => {
  const groups = new Map()
  for (const conversation of conversations.value) {
    const label = conversationGroupLabel(conversation.updated_at)
    if (!groups.has(label)) groups.set(label, [])
    groups.get(label).push(conversation)
  }
  return Array.from(groups, ([label, items]) => ({ label, items }))
})

function notifyError(error) { emit('notice', { type: 'error', text: error?.message || 'AI 对话操作未完成。' }) }
function syncFullscreenState() { isFullscreen.value = document.fullscreenElement === aiChatShell.value }
async function toggleFullscreen() {
  try {
    if (document.fullscreenElement === aiChatShell.value) await document.exitFullscreen()
    else await aiChatShell.value?.requestFullscreen()
  } catch (error) {
    emit('notice', { type: 'error', text: `无法切换全屏显示：${error?.message || '浏览器拒绝了此操作。'}` })
  }
}
function renderMarkdown(content) { return markdown.render(String(content || '')) }
function formatTime(value) {
  if (!value) return ''
  const date = new Date(value)
  if (Number.isNaN(date.getTime())) return ''
  return new Intl.DateTimeFormat('zh-CN', { month: 'numeric', day: 'numeric', hour: '2-digit', minute: '2-digit' }).format(date)
}

function conversationGroupLabel(value) {
  const date = new Date(value)
  if (Number.isNaN(date.getTime())) return '更早'
  const now = new Date()
  const today = new Date(now.getFullYear(), now.getMonth(), now.getDate())
  const yesterday = new Date(today)
  yesterday.setDate(today.getDate() - 1)
  if (date >= today) return '今天'
  if (date >= yesterday) return '昨天'
  return new Intl.DateTimeFormat('zh-CN', { month: 'long', day: 'numeric' }).format(date)
}

function formatConversationTime(value) {
  const date = new Date(value)
  if (Number.isNaN(date.getTime())) return ''
  const now = new Date()
  const sameDay = date.getFullYear() === now.getFullYear() && date.getMonth() === now.getMonth() && date.getDate() === now.getDate()
  return new Intl.DateTimeFormat('zh-CN', sameDay ? { hour: '2-digit', minute: '2-digit' } : { month: 'numeric', day: 'numeric' }).format(date)
}

async function scrollToBottom() {
  await nextTick()
  if (chatBody.value) chatBody.value.scrollTop = chatBody.value.scrollHeight
}

let scrollQueued = false
function queueScrollToBottom() {
  if (scrollQueued) return
  scrollQueued = true
  void nextTick().then(() => {
    if (chatBody.value) chatBody.value.scrollTop = chatBody.value.scrollHeight
    scrollQueued = false
  })
}

async function loadConversation(conversationId) {
  if (!conversationId) {
    messages.value = []
    return
  }
  loading.value = true
  try {
    const detail = await getAIChatConversation(props.tenantId, conversationId)
    selectedConversationId.value = detail.conversation.id
    messages.value = detail.messages || []
    await scrollToBottom()
  } catch (error) {
    notifyError(error)
  } finally {
    loading.value = false
  }
}

async function loadConversations({ preserveSelection = true } = {}) {
  if (!props.tenantId || !props.organizationId) {
    conversations.value = []
    selectedConversationId.value = 0
    messages.value = []
    return
  }
  loading.value = true
  try {
    const items = await listAIChatConversations(props.tenantId, props.organizationId)
    conversations.value = items || []
    const selectedExists = preserveSelection && conversations.value.some((item) => item.id === selectedConversationId.value)
    if (selectedExists) {
      await loadConversation(selectedConversationId.value)
    } else if (conversations.value[0]) {
      await loadConversation(conversations.value[0].id)
    } else {
      selectedConversationId.value = 0
      messages.value = []
    }
  } catch (error) {
    notifyError(error)
  } finally {
    loading.value = false
  }
}

async function loadSkills() {
  if (!props.tenantId) {
    availableSkills.value = []
    manualSkillIDs.value = []
    return
  }
  try {
    const skills = await listAISkills(props.tenantId)
    availableSkills.value = skills || []
    const enabledIDs = new Set(availableSkills.value.filter((skill) => skill.enabled).map((skill) => skill.id))
    manualSkillIDs.value = manualSkillIDs.value.filter((id) => enabledIDs.has(id))
  } catch (error) {
    notifyError(error)
  }
}

async function startConversation({ preserveAttachments = false } = {}) {
  if (!props.organizationId) {
    emit('notice', { type: 'error', text: '请先在右上角选择组织。' })
    return
  }
  creating.value = true
  try {
    const conversation = await createAIChatConversation(props.tenantId, { organization_id: props.organizationId })
    conversations.value = [conversation, ...conversations.value]
    selectedConversationId.value = conversation.id
    messages.value = []
    messageText.value = ''
    if (!preserveAttachments) clearAttachments()
    await nextTick()
  } catch (error) {
    notifyError(error)
  } finally {
    creating.value = false
  }
}

async function selectConversation(conversationId) {
  if (conversationId === selectedConversationId.value && messages.value.length) return
  await loadConversation(conversationId)
}

async function removeConversation(conversation, event) {
  event?.stopPropagation()
  if (!window.confirm(`删除会话“${conversation.title}”？此操作会同时删除其消息记录。`)) return
  try {
    await deleteAIChatConversation(props.tenantId, conversation.id)
    conversations.value = conversations.value.filter((item) => item.id !== conversation.id)
    if (selectedConversationId.value === conversation.id) {
      const next = conversations.value[0]
      selectedConversationId.value = 0
      messages.value = []
      if (next) await loadConversation(next.id)
    }
  } catch (error) {
    notifyError(error)
  }
}

function conversationTitleFromMessage(content) {
  const compact = String(content || '').replace(/\s+/g, ' ').trim()
  return compact.length > 36 ? `${compact.slice(0, 33)}...` : compact || '新对话'
}

async function sendMessage() {
  const content = messageText.value.trim()
  if ((!content && !attachments.value.length) || sending.value) return
  if (!selectedConversationId.value) await startConversation({ preserveAttachments: true })
  if (!selectedConversationId.value) return
  const pendingId = `pending-${Date.now()}`
  const attachmentLabels = attachments.value.map((attachment) => `[已附${attachment.kind}：${attachment.name}]`)
  const displayContent = [content, ...attachmentLabels].filter(Boolean).join('\n')
  const pending = { id: pendingId, role: 'user', content: displayContent, created_at: new Date().toISOString() }
  messages.value.push(pending)
  messageText.value = ''
  sending.value = true
  await scrollToBottom()
  let acceptedUser = false
  let completed = false
  const temporaryTranscriptIDs = new Set()
  const addTemporaryTranscript = (message, role) => {
    const id = `stream-${role}-${Date.now()}-${Math.random().toString(36).slice(2)}`
    temporaryTranscriptIDs.add(id)
    messages.value.push({ ...message, id, role: message.role || role, _streaming: true })
    queueScrollToBottom()
  }
  const removeStreamingAssistant = () => {
    if (!streamingAssistantID.value) return
    messages.value = messages.value.filter((item) => item.id !== streamingAssistantID.value)
    streamingAssistantID.value = ''
  }
  try {
    await streamAIChatMessage(props.tenantId, selectedConversationId.value, {
      content,
      attachments: attachments.value.map(({ name, media_type, data_base64 }) => ({ name, media_type, data_base64 })),
      use_skills: skillMode.value !== 'none',
      skill_ids: skillMode.value === 'manual' ? manualSkillIDs.value : null,
    }, {
      user: (message) => {
        acceptedUser = true
        const index = messages.value.findIndex((item) => item.id === pendingId)
        if (index >= 0) messages.value.splice(index, 1, message)
        else messages.value.push(message)
        queueScrollToBottom()
      },
      skill: (message) => addTemporaryTranscript(message, 'skill'),
      tool: (message) => addTemporaryTranscript(message, 'tool'),
      delta: ({ content: delta }) => {
        if (!delta) return
        if (!streamingAssistantID.value) {
          const message = { id: `stream-assistant-${Date.now()}-${Math.random().toString(36).slice(2)}`, role: 'assistant', content: '', created_at: new Date().toISOString(), _streaming: true }
          streamingAssistantID.value = message.id
          messages.value.push(message)
        }
        const message = messages.value.find((item) => item.id === streamingAssistantID.value)
        if (message) message.content += delta
        queueScrollToBottom()
      },
      assistant_reset: () => removeStreamingAssistant(),
      error: (payload) => { throw new Error(payload?.message || 'AI 对话流式请求失败') },
      done: (result) => {
        completed = true
        const userIndex = messages.value.findIndex((item) => item.id === pendingId)
        if (userIndex >= 0) messages.value.splice(userIndex, 1, result.user_message)
        messages.value = messages.value.filter((item) => !temporaryTranscriptIDs.has(item.id))
        const turnMessages = [...(result.skill_message ? [result.skill_message] : []), ...(result.tool_messages || []), result.assistant_message]
        const assistantIndex = messages.value.findIndex((item) => item.id === streamingAssistantID.value)
        if (assistantIndex >= 0) messages.value.splice(assistantIndex, 1, ...turnMessages)
        else messages.value.push(...turnMessages)
        streamingAssistantID.value = ''
        const index = conversations.value.findIndex((item) => item.id === selectedConversationId.value)
        if (index >= 0 && conversations.value[index].title === '新对话') {
          conversations.value[index] = { ...conversations.value[index], title: conversationTitleFromMessage(content || attachmentLabels[0]), updated_at: new Date().toISOString() }
        }
        clearAttachments()
        for (const warning of result.warnings || []) emit('notice', { type: 'error', text: warning })
        queueScrollToBottom()
      },
    })
    if (!completed) throw new Error('流式连接已结束，但未收到完成结果')
  } catch (error) {
    messages.value = messages.value.filter((item) => !temporaryTranscriptIDs.has(item.id))
    removeStreamingAssistant()
    if (acceptedUser) await loadConversation(selectedConversationId.value)
    else {
      messages.value = messages.value.filter((item) => item.id !== pendingId)
      messageText.value = content
    }
    notifyError(error)
  } finally {
    streamingAssistantID.value = ''
    sending.value = false
  }
}

function onComposerKeydown(event) {
  if (event.key === 'Enter' && (event.ctrlKey || event.metaKey)) {
    event.preventDefault()
    void sendMessage()
  }
}

function usePrompt(prompt) {
  messageText.value = prompt
}

function attachmentAccepts(file) {
  return ['image/png', 'image/jpeg', 'image/webp', 'image/gif', 'text/plain', 'text/markdown', 'text/csv'].includes(file.type)
}

function attachmentLabel(file) {
  return file.type.startsWith('image/') ? '图片' : '文本'
}

function chooseAttachments() {
  attachmentInput.value?.click()
}

function readAttachment(file) {
  return new Promise((resolve, reject) => {
    const reader = new FileReader()
    reader.onerror = () => reject(new Error(`无法读取附件“${file.name}”`))
    reader.onload = () => {
      const value = String(reader.result || '')
      const comma = value.indexOf(',')
      if (comma < 0) return reject(new Error(`附件“${file.name}”内容无效`))
      resolve(value.slice(comma + 1))
    }
    reader.readAsDataURL(file)
  })
}

async function addAttachments(event) {
  const files = Array.from(event.target?.files || [])
  if (event.target) event.target.value = ''
  if (!files.length) return
  if (attachments.value.length + files.length > 4) {
    emit('notice', { type: 'error', text: '一次最多添加 4 个附件。' })
    return
  }
  const totalBytes = attachments.value.reduce((total, item) => total + item.size, 0) + files.reduce((total, file) => total + file.size, 0)
  if (files.some((file) => !attachmentAccepts(file))) {
    emit('notice', { type: 'error', text: '目前支持 PNG、JPEG、WebP、GIF、TXT、Markdown 和 CSV 附件。' })
    return
  }
  if (files.some((file) => file.size > 4 * 1024 * 1024) || totalBytes > 6 * 1024 * 1024) {
    emit('notice', { type: 'error', text: '单个附件不能超过 4 MB，全部附件不能超过 6 MB。' })
    return
  }
  try {
    const items = await Promise.all(files.map(async (file) => ({
      id: `${file.name}-${file.lastModified}-${Math.random().toString(36).slice(2)}`,
      name: file.name,
      media_type: file.type,
      data_base64: await readAttachment(file),
      preview_url: file.type.startsWith('image/') ? URL.createObjectURL(file) : '',
      size: file.size,
      kind: attachmentLabel(file),
    })))
    attachments.value.push(...items)
  } catch (error) {
    notifyError(error)
  }
}

function removeAttachment(id) {
  const attachment = attachments.value.find((item) => item.id === id)
  if (attachment?.preview_url) URL.revokeObjectURL(attachment.preview_url)
  attachments.value = attachments.value.filter((item) => item.id !== id)
}

function clearAttachments() {
  for (const attachment of attachments.value) {
    if (attachment.preview_url) URL.revokeObjectURL(attachment.preview_url)
  }
  attachments.value = []
}

watch(() => [props.tenantId, props.organizationId], () => {
  void loadConversations({ preserveSelection: false })
  void loadSkills()
}, { immediate: true })
onMounted(() => {
  syncFullscreenState()
  document.addEventListener('fullscreenchange', syncFullscreenState)
})
onBeforeUnmount(() => {
  document.removeEventListener('fullscreenchange', syncFullscreenState)
  if (document.fullscreenElement === aiChatShell.value) void document.exitFullscreen()
  clearAttachments()
})
</script>

<template>
  <section ref="aiChatShell" class="ai-chat-shell" aria-label="InkFlow AI 对话">
    <aside class="conversation-pane">
      <header class="conversation-pane-header">
        <button class="new-chat" type="button" :disabled="creating || !organizationId" @click="startConversation"><MessageSquarePlus :size="16" />新对话</button>
      </header>
      <div class="conversation-content">
        <div v-if="loading && !conversations.length" class="conversation-empty">正在读取会话…</div>
        <div v-else-if="!conversations.length" class="conversation-empty"><Bot :size="21" /><strong>开始一个新对话</strong><span>对话会保存在当前组织下，仅自己可见。</span></div>
        <div v-else class="conversation-list">
          <section v-for="group in conversationGroups" :key="group.label" class="conversation-group">
            <h3>{{ group.label }}</h3>
            <article v-for="conversation in group.items" :key="conversation.id" :class="['conversation-item', { active: conversation.id === selectedConversationId }]" @click="selectConversation(conversation.id)">
              <button class="conversation-main" type="button"><span>{{ conversation.title }}</span><small><Clock3 :size="11" />{{ formatConversationTime(conversation.updated_at) }}</small></button>
              <button class="conversation-delete" type="button" :aria-label="`删除会话 ${conversation.title}`" title="删除会话" @click="removeConversation(conversation, $event)"><Trash2 :size="13" /></button>
            </article>
          </section>
        </div>
      </div>
      <footer><Wrench :size="13" /><span>远程 MCP 与 Skill 在“工具与技能”中管理。</span></footer>
    </aside>

    <section class="chat-pane">
      <header class="chat-header">
        <div class="chat-identity"><span><Bot :size="17" /></span><div><strong>InkFlow</strong><small>{{ currentConversation?.title || '新对话' }}</small></div></div>
        <div class="chat-header-actions">
          <span class="chat-status"><Sparkles :size="13" />按需匹配能力</span>
          <button class="fullscreen-button" type="button" :aria-label="isFullscreen ? '退出全屏显示' : '全屏显示 AI 对话'" :title="isFullscreen ? '退出全屏' : '全屏显示'" @click="toggleFullscreen"><Minimize v-if="isFullscreen" :size="16" /><Expand v-else :size="16" /></button>
        </div>
      </header>
      <div ref="chatBody" class="chat-body">
        <section v-if="emptyState" class="chat-welcome">
          <span class="welcome-avatar"><Bot :size="20" /></span>
          <p class="welcome-eyebrow">INKFLOW AI</p>
          <h3>今天想处理什么工作？</h3>
          <p>可以分析文件、查询知识、调用工具，或直接开始起草。</p>
          <div class="prompt-grid">
            <button type="button" @click="usePrompt('请分析我附上的文件，并提取重点。')">分析文件<ChevronRight :size="14" /></button>
            <button type="button" @click="usePrompt('请查询当前组织知识库中与数字政府有关的资料。')">查询知识库<ChevronRight :size="14" /></button>
            <button type="button" @click="usePrompt('请说明当前已启用的 MCP 工具适合解决哪些问题。')">调用 MCP<ChevronRight :size="14" /></button>
            <button type="button" @click="usePrompt('请根据现有资料起草一份工作报告。')">写报告<ChevronRight :size="14" /></button>
          </div>
        </section>
        <article v-for="message in messages" :key="message.id" :class="['chat-message', message.role]">
          <template v-if="message.role === 'skill'">
            <div class="skill-message"><Sparkles :size="13" /><span>Skill · {{ message.content }}</span></div>
          </template>
          <template v-else-if="message.role === 'tool'">
            <details class="tool-message"><summary><span class="tool-success">✓</span><strong>{{ message.tool_name || '远程 MCP 工具' }}</strong><span>已完成</span></summary><pre>{{ message.content }}</pre></details>
          </template>
          <template v-else>
            <span class="message-avatar"><Bot v-if="message.role === 'assistant'" :size="17" /><span v-else>你</span></span>
            <div class="message-card"><div v-if="message.role === 'assistant'" class="markdown-body" v-html="renderMarkdown(message.content)"></div><p v-else>{{ message.content }}</p><time>{{ formatTime(message.created_at) }}</time></div>
          </template>
        </article>
        <article v-if="sending && !streamingAssistantID" class="chat-message assistant thinking"><span class="message-avatar"><Bot :size="17" /></span><div class="message-card"><i></i><i></i><i></i><span>正在思考…</span></div></article>
      </div>
      <footer class="composer-wrap">
        <div class="composer">
          <input ref="attachmentInput" class="attachment-input" type="file" multiple accept="image/png,image/jpeg,image/webp,image/gif,text/plain,text/markdown,text/csv,.txt,.md,.markdown,.csv" @change="addAttachments" />
          <div v-if="attachments.length" class="attachment-list"><article v-for="attachment in attachments" :key="attachment.id" class="attachment-chip"><img v-if="attachment.preview_url" :src="attachment.preview_url" :alt="attachment.name" /><ImagePlus v-else-if="attachment.kind === '图片'" :size="15" /><FileText v-else :size="15" /><span>{{ attachment.name }}</span><button type="button" :disabled="sending" :aria-label="`移除附件 ${attachment.name}`" @click="removeAttachment(attachment.id)"><X :size="13" /></button></article></div>
          <textarea v-model="messageText" :disabled="sending" rows="3" placeholder="输入问题，或添加图片 / 文本附件…" @keydown="onComposerKeydown"></textarea>
          <div class="composer-toolbar">
            <button class="attach-button" type="button" :disabled="sending" title="添加图片或文本附件" @click="chooseAttachments"><Paperclip :size="17" /></button>
            <AIChatSkillSelector v-model:mode="skillMode" v-model:selected-ids="manualSkillIDs" :skills="enabledSkills" :disabled="sending" />
            <span class="composer-shortcut">Ctrl/⌘ + Enter</span>
            <button class="send-button" type="button" :disabled="(!messageText.trim() && !attachments.length) || sending" :aria-label="sending ? '正在发送' : '发送消息'" @click="sendMessage"><Send :size="17" /><span>{{ sending ? '处理中' : '发送' }}</span></button>
          </div>
        </div>
      </footer>
    </section>
  </section>
</template>

<style scoped>
.ai-chat-shell { display: grid; height: calc(100dvh - 188px); min-height: 500px; grid-template-columns: 276px minmax(0, 1fr); overflow: hidden; border: 1px solid #e3e8e5; border-radius: 12px; background: #fff; }
.ai-chat-shell:fullscreen { width: 100vw; height: 100dvh; min-height: 0; border: 0; border-radius: 0; }
.conversation-pane, .chat-pane { min-width: 0; min-height: 0; overflow: hidden; }
.conversation-pane { display: flex; flex-direction: column; background: #fafcfb; border-right: 1px solid #e7ece9; }
.conversation-pane-header { padding: 14px 14px 11px; }
.new-chat { display: inline-flex; align-items: center; justify-content: center; gap: 7px; min-height: 34px; padding: 0 11px; border: 1px solid #d8e3dc; border-radius: 7px; color: #185b46; background: #fff; font: inherit; font-size: 12px; font-weight: 750; cursor: pointer; }
.new-chat:hover { border-color: #82ad94; background: #f2f8f4; }.new-chat:disabled, .composer button:disabled { cursor: not-allowed; opacity: .55; }
.conversation-content { min-height: 0; flex: 1; overflow: auto; padding: 2px 8px 10px; scrollbar-gutter: stable; }
.conversation-list, .conversation-group { display: grid; gap: 2px; }.conversation-group { margin: 10px 0 16px; }.conversation-group:first-child { margin-top: 0; }
.conversation-group h3 { margin: 0 6px 5px; color: #8a9890; font-size: 10px; font-weight: 750; letter-spacing: .06em; }
.conversation-item { display: flex; align-items: center; gap: 2px; border-radius: 7px; }.conversation-item:hover, .conversation-item.active { background: #eaf6ef; }
.conversation-main { display: grid; flex: 1; min-width: 0; gap: 3px; padding: 8px 9px; border: 0; border-radius: 7px; color: #304b3e; background: transparent; text-align: left; cursor: pointer; }
.conversation-main > span { overflow: hidden; font-size: 12px; font-weight: 660; line-height: 1.35; text-overflow: ellipsis; white-space: nowrap; }.conversation-main small { display: flex; align-items: center; gap: 4px; color: #9aa69f; font-size: 10px; }
.conversation-delete { display: grid; width: 28px; height: 28px; place-items: center; margin-right: 3px; border: 0; border-radius: 6px; color: #829087; background: transparent; opacity: 0; cursor: pointer; }.conversation-item:hover .conversation-delete, .conversation-item:focus-within .conversation-delete { opacity: 1; }.conversation-delete:hover { color: #a8473c; background: #fff5f3; }
.conversation-empty { display: grid; place-items: center; gap: 7px; margin-top: 40px; padding: 18px; color: #8a9990; text-align: center; font-size: 12px; line-height: 1.55; }.conversation-empty strong { color: #4d6457; font-size: 12px; }.conversation-pane footer { display: flex; gap: 6px; padding: 12px 15px; border-top: 1px solid #edf1ee; color: #8a9790; font-size: 10px; line-height: 1.45; }
.chat-pane { display: grid; grid-template-rows: auto minmax(0, 1fr) auto; background: #fff; }.chat-header { display: flex; align-items: center; justify-content: space-between; gap: 12px; min-height: 54px; padding: 0 24px; border-bottom: 1px solid #edf0ee; }.chat-identity { display: flex; min-width: 0; align-items: center; gap: 8px; }.chat-identity > span { display: grid; width: 27px; height: 27px; place-items: center; border-radius: 8px; color: #23674f; background: #eff8f2; }.chat-identity div { display: grid; min-width: 0; gap: 1px; }.chat-identity strong { color: #214235; font-size: 13px; }.chat-identity small { overflow: hidden; color: #87958d; font-size: 10px; text-overflow: ellipsis; white-space: nowrap; }.chat-header-actions { display: inline-flex; align-items: center; gap: 8px; }.chat-status { display: inline-flex; align-items: center; gap: 5px; color: #7b9284; font-size: 10px; }.fullscreen-button { display: grid; width: 29px; height: 29px; flex: none; place-items: center; border: 1px solid #e1e9e4; border-radius: 6px; color: #587466; background: #fff; cursor: pointer; }.fullscreen-button:hover { border-color: #b7d0be; color: #176149; background: #f1f8f3; }.fullscreen-button:focus-visible { outline: 2px solid #74a68a; outline-offset: 2px; }
.chat-body { min-height: 0; overflow-y: auto; padding: 30px clamp(24px, 4vw, 68px); background: #fff; overscroll-behavior: contain; scrollbar-gutter: stable; }
.chat-welcome { display: grid; max-width: 680px; place-items: center; gap: 8px; margin: clamp(24px, 10vh, 96px) auto; color: #7b8b83; text-align: center; }.welcome-avatar { display: grid; width: 38px; height: 38px; place-items: center; border-radius: 10px; color: #17634c; background: #edf7f0; }.welcome-eyebrow { margin: 9px 0 0; color: #6d947f; font-size: 10px; font-weight: 800; letter-spacing: .13em; }.chat-welcome h3 { margin: 1px 0 0; color: #1f3c30; font-size: 23px; letter-spacing: -.03em; }.chat-welcome > p:not(.welcome-eyebrow) { margin: 0; font-size: 13px; line-height: 1.7; }.prompt-grid { display: grid; width: 100%; grid-template-columns: repeat(4, minmax(0, 1fr)); gap: 8px; margin-top: 17px; }.prompt-grid button { display: flex; align-items: center; justify-content: space-between; gap: 8px; min-height: 42px; padding: 0 11px; border: 1px solid #e4ebe6; border-radius: 7px; color: #486556; background: #fff; text-align: left; font: inherit; font-size: 11px; font-weight: 680; cursor: pointer; }.prompt-grid button:hover { border-color: #b9d4c2; background: #f5faf6; color: #1e684f; }
.chat-message { display: flex; max-width: 920px; gap: 10px; margin: 0 auto 25px; }.chat-message.user { justify-content: flex-end; }.message-avatar { display: grid; width: 29px; height: 29px; flex: none; place-items: center; border: 1px solid #e0e9e3; border-radius: 8px; color: #33725a; background: #f5faf6; font-size: 10px; font-weight: 800; }.user .message-avatar { order: 2; border-color: #d9e7dc; color: #2a664e; background: #eaf6ed; }.message-card { min-width: 0; color: #263d32; font-size: 15px; line-height: 1.74; }.assistant .message-card { max-width: min(100%, 860px); padding: 2px 0; border: 0; overflow-wrap: anywhere; background: transparent; }.user .message-card { max-width: min(65%, 560px); padding: 10px 13px; border: 1px solid #dce9e0; border-radius: 11px 4px 11px 11px; background: #edf8f0; font-size: 13px; line-height: 1.65; }.message-card p { margin: 0; white-space: pre-wrap; }.message-card time { display: block; margin-top: 7px; color: #9aa79f; font-size: 10px; line-height: 1; }.markdown-body :deep(p) { margin: 0 0 13px; }.markdown-body :deep(p:last-child) { margin-bottom: 0; }.markdown-body :deep(h1), .markdown-body :deep(h2), .markdown-body :deep(h3) { color: #183c2e; letter-spacing: -.02em; }.markdown-body :deep(h1) { margin: 25px 0 13px; font-size: 24px; }.markdown-body :deep(h2) { margin: 22px 0 11px; font-size: 20px; }.markdown-body :deep(h3) { margin: 18px 0 9px; font-size: 17px; }.markdown-body :deep(h1:first-child), .markdown-body :deep(h2:first-child), .markdown-body :deep(h3:first-child) { margin-top: 0; }.markdown-body :deep(ul), .markdown-body :deep(ol) { margin: 10px 0 14px; padding-left: 23px; }.markdown-body :deep(li + li) { margin-top: 4px; }.markdown-body :deep(code) { padding: 2px 5px; border-radius: 4px; color: #315343; background: #f0f4f1; font-family: Consolas, "Courier New", monospace; font-size: .88em; }.markdown-body :deep(pre) { overflow: auto; margin: 15px 0; padding: 13px 15px; border: 1px solid #283d34; border-radius: 8px; color: #e3f0e8; background: #18342a; font-size: 12px; line-height: 1.6; }.markdown-body :deep(pre code) { padding: 0; color: inherit !important; background: transparent; }.markdown-body :deep(table) { display: block; width: 100%; overflow-x: auto; margin: 14px 0; border-collapse: collapse; font-size: 13px; }.markdown-body :deep(th), .markdown-body :deep(td) { padding: 8px 10px; border-bottom: 1px solid #e3eae5; text-align: left; }.markdown-body :deep(th) { color: #426353; background: #f7faf8; font-size: 12px; }
.chat-message.skill { display: block; margin: -10px auto 15px; }.skill-message { display: inline-flex; max-width: 860px; align-items: center; gap: 5px; color: #5c856e; font-size: 10px; line-height: 1.5; }.skill-message span { overflow: hidden; text-overflow: ellipsis; white-space: nowrap; }.tool-message { width: min(100%, 860px); margin: -4px auto 18px; border: 1px solid #e3e9e5; border-radius: 8px; color: #4d6356; background: #fbfcfb; font-size: 12px; }.tool-message summary { display: flex; align-items: center; gap: 7px; padding: 9px 11px; cursor: pointer; list-style: none; }.tool-message summary::-webkit-details-marker { display: none; }.tool-message summary > span:last-child { margin-left: auto; color: #93a097; font-size: 10px; }.tool-success { display: grid; width: 16px; height: 16px; place-items: center; border-radius: 50%; color: #fff; background: #4d9a71; font-size: 10px; }.tool-message pre { overflow: auto; max-height: 220px; margin: 0; padding: 11px; border-top: 1px solid #edf0ee; color: #365243; background: #f6f9f7; white-space: pre-wrap; font-size: 11px; line-height: 1.6; }.thinking .message-card { display: flex; align-items: center; gap: 5px; color: #7b8c83; font-size: 12px; }.thinking i { display: block; width: 5px; height: 5px; border-radius: 50%; background: #5e9a78; animation: blink 1.05s infinite; }.thinking i:nth-child(2) { animation-delay: .15s; }.thinking i:nth-child(3) { animation-delay: .3s; }.thinking span { margin-left: 3px; } @keyframes blink { 50% { opacity: .2; transform: translateY(-2px); } }
.composer-wrap { padding: 14px clamp(16px, 3vw, 28px) 18px; border-top: 1px solid #edf0ee; background: #fff; }.composer { max-width: 980px; margin: auto; padding: 11px 12px 9px; border: 1px solid #d9e4dd; border-radius: 10px; background: #fff; transition: border-color .18s ease, box-shadow .18s ease; }.composer:focus-within { border-color: #74a68a; box-shadow: 0 0 0 3px rgba(48, 122, 83, .09); }.attachment-input { display: none; }.attachment-list { display: flex; flex-wrap: wrap; gap: 6px; padding: 0 0 8px; }.attachment-chip { display: flex; max-width: 240px; align-items: center; gap: 6px; padding: 4px 5px 4px 7px; border: 1px solid #dbe8df; border-radius: 6px; color: #4b6958; background: #f4faf6; font-size: 10px; line-height: 1; }.attachment-chip img { width: 22px; height: 22px; border-radius: 4px; object-fit: cover; }.attachment-chip span { overflow: hidden; text-overflow: ellipsis; white-space: nowrap; }.attachment-chip button { display: grid; width: 20px; height: 20px; place-items: center; padding: 0; border: 0; border-radius: 4px; color: #71867a; background: transparent; cursor: pointer; }.attachment-chip button:hover { color: #ae493e; background: #fff; }.composer textarea { display: block; width: 100%; min-height: 66px; max-height: 210px; box-sizing: border-box; resize: vertical; padding: 1px 3px 9px; border: 0; outline: 0; color: #243a2f; background: transparent; font: inherit; font-size: 14px; line-height: 1.65; }.composer textarea::placeholder { color: #9aa69f; }.composer-toolbar { display: flex; align-items: center; gap: 6px; min-height: 30px; border-top: 1px solid #edf1ee; padding-top: 8px; }.attach-button, .send-button { display: inline-flex; height: 30px; align-items: center; justify-content: center; gap: 5px; border: 0; border-radius: 6px; font: inherit; font-size: 11px; font-weight: 750; cursor: pointer; }.attach-button { width: 30px; color: #597366; background: transparent; }.attach-button:hover { color: #1c7457; background: #eff7f1; }.composer-shortcut { margin-left: auto; color: #a0aaa4; font-size: 10px; }.send-button { min-width: 31px; padding: 0 9px; color: #fff; background: #176149; }.send-button:hover:not(:disabled) { background: #10523e; }
@media (max-width: 1200px) { .ai-chat-shell { grid-template-columns: 232px minmax(0, 1fr); }.chat-body { padding-inline: 30px; } }
@media (max-width: 900px) { .ai-chat-shell { height: calc(100dvh - 178px); grid-template-columns: 210px minmax(0, 1fr); }.prompt-grid { grid-template-columns: repeat(2, minmax(0, 1fr)); }.chat-header { padding-inline: 16px; }.chat-status, .composer-shortcut { display: none; } }
@media (max-width: 700px) { .ai-chat-shell { height: auto; min-height: calc(100dvh - 180px); grid-template-columns: 1fr; }.conversation-pane { max-height: 170px; border-right: 0; border-bottom: 1px solid #e7ece9; }.conversation-pane-header { padding-bottom: 5px; }.conversation-content { padding-inline: 8px; }.conversation-list { display: flex; gap: 4px; overflow-x: auto; }.conversation-group { display: flex; min-width: max-content; gap: 2px; margin: 0 4px 0 0; }.conversation-group h3 { align-self: center; margin: 0 4px; }.conversation-item { width: 170px; }.conversation-pane footer { display: none; }.chat-pane { min-height: 600px; }.chat-body { padding: 22px 16px; }.chat-welcome { margin-block: 48px; }.user .message-card { max-width: 76%; }.composer-wrap { padding: 10px; }.prompt-grid { grid-template-columns: 1fr; }.composer textarea { min-height: 58px; }.send-button span { display: none; }.send-button { width: 31px; padding: 0; } }
</style>
