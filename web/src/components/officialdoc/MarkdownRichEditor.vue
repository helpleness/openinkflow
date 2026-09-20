<script setup>
import { computed, nextTick, onBeforeUnmount, ref, watch } from 'vue'
import { Extension } from '@tiptap/core'
import { Markdown } from '@tiptap/markdown'
import { Plugin, PluginKey } from '@tiptap/pm/state'
import { Decoration, DecorationSet } from '@tiptap/pm/view'
import StarterKit from '@tiptap/starter-kit'
import TaskItem from '@tiptap/extension-task-item'
import TaskList from '@tiptap/extension-task-list'
import Underline from '@tiptap/extension-underline'
import { EditorContent, useEditor } from '@tiptap/vue-3'
import { BubbleMenu, FloatingMenu } from '@tiptap/vue-3/menus'

const props = defineProps({
  modelValue: { type: String, default: '' },
  highlightFindings: { type: Array, default: () => [] },
  placeholder: { type: String, default: '输入 / 打开块菜单，或直接开始撰写…' },
})
const emit = defineEmits(['update:modelValue', 'selection-change'])
const fullscreen = ref(false)
const slashMenu = ref({ open: false, query: '', from: 0, x: 0, y: 0 })
const blockStyle = ref('paragraph')
const issueHighlightKey = new PluginKey('inkflowIssueHighlight')

function normalizedFindings() {
  const seen = new Set()
  return (props.highlightFindings || []).map((finding) => ({
    excerpt: String(finding?.excerpt || '').trim(),
    category: String(finding?.category || 'issue'),
  })).filter(({ excerpt }) => excerpt.length > 0 && !seen.has(excerpt) && seen.add(excerpt))
}

function createIssueDecorations(doc) {
  const findings = normalizedFindings()
  if (!findings.length) return DecorationSet.empty
  const decorations = []
  doc.descendants((node, pos) => {
    if (!node.isText || !node.text) return
    for (const finding of findings) {
      let start = 0
      while (start < node.text.length) {
        const index = node.text.indexOf(finding.excerpt, start)
        if (index < 0) break
        decorations.push(Decoration.inline(pos + index, pos + index + finding.excerpt.length, {
          class: `document-issue issue-${finding.category}`,
          title: '检测到需要处理的问题',
        }))
        start = index + finding.excerpt.length
      }
    }
  })
  return decorations.length ? DecorationSet.create(doc, decorations) : DecorationSet.empty
}

const IssueHighlight = Extension.create({
  name: 'issueHighlight',
  addProseMirrorPlugins() {
    return [new Plugin({
      key: issueHighlightKey,
      props: { decorations: (state) => createIssueDecorations(state.doc) },
    })]
  },
})

function selectedText(editorInstance) {
  const { from, to } = editorInstance.state.selection
  return from === to ? '' : editorInstance.state.doc.textBetween(from, to, ' ')
}

function selectedBlockStyle(editorInstance) {
  if (editorInstance.isActive('heading', { level: 1 })) return 'heading-1'
  if (editorInstance.isActive('heading', { level: 2 })) return 'heading-2'
  if (editorInstance.isActive('heading', { level: 3 })) return 'heading-3'
  if (editorInstance.isActive('heading', { level: 4 })) return 'heading-4'
  if (editorInstance.isActive('bulletList')) return 'bullet'
  if (editorInstance.isActive('orderedList')) return 'ordered'
  if (editorInstance.isActive('taskList')) return 'task'
  if (editorInstance.isActive('blockquote')) return 'quote'
  if (editorInstance.isActive('codeBlock')) return 'code-block'
  return 'paragraph'
}

function refreshSlashMenu(editorInstance) {
  const { from, empty } = editorInstance.state.selection
  if (!empty) {
    slashMenu.value.open = false
    return
  }
  const $from = editorInstance.state.doc.resolve(from)
  const beforeCaret = $from.parent.textBetween(0, $from.parentOffset, '\u0000', '\u0000')
  const match = beforeCaret.match(/(?:^|\s)\/([^\s/]*)$/)
  if (!match) {
    slashMenu.value.open = false
    return
  }
  const coords = editorInstance.view.coordsAtPos(from)
  slashMenu.value = {
    open: true,
    query: match[1].toLowerCase(),
    from: from - match[0].length + (match[0].startsWith(' ') ? 1 : 0),
    x: coords.left,
    y: coords.bottom + 8,
  }
}

const editor = useEditor({
  extensions: [
    StarterKit.configure({ heading: { levels: [1, 2, 3, 4] } }),
    Underline,
    TaskList,
    TaskItem.configure({ nested: true }),
    Markdown.configure({ markedOptions: { gfm: true, breaks: false } }),
    IssueHighlight,
  ],
  content: props.modelValue || '',
  contentType: 'markdown',
  editorProps: { attributes: { class: 'notion-editor-content', 'data-placeholder': props.placeholder } },
  onUpdate: ({ editor: editorInstance }) => {
    blockStyle.value = selectedBlockStyle(editorInstance)
    emit('update:modelValue', editorInstance.getMarkdown())
    refreshSlashMenu(editorInstance)
  },
  onSelectionUpdate: ({ editor: editorInstance }) => {
    blockStyle.value = selectedBlockStyle(editorInstance)
    emit('selection-change', { quote: selectedText(editorInstance) })
    refreshSlashMenu(editorInstance)
  },
  onFocus: ({ editor: editorInstance }) => {
    blockStyle.value = selectedBlockStyle(editorInstance)
    refreshSlashMenu(editorInstance)
  },
  onBlur: () => window.setTimeout(() => { slashMenu.value.open = false }, 160),
})

const slashActions = [
  { id: 'paragraph', icon: 'T', title: '普通文本', hint: '开始一个普通段落', run: (chain) => chain.setParagraph() },
  { id: 'heading-1', icon: 'H1', title: '标题 1', hint: '大标题', run: (chain) => chain.toggleHeading({ level: 1 }) },
  { id: 'heading-2', icon: 'H2', title: '标题 2', hint: '章节标题', run: (chain) => chain.toggleHeading({ level: 2 }) },
  { id: 'heading-3', icon: 'H3', title: '标题 3', hint: '小节标题', run: (chain) => chain.toggleHeading({ level: 3 }) },
  { id: 'bullet', icon: '•', title: '项目符号列表', hint: '无序列表', run: (chain) => chain.toggleBulletList() },
  { id: 'ordered', icon: '1.', title: '有序列表', hint: '编号列表', run: (chain) => chain.toggleOrderedList() },
  { id: 'task', icon: '☐', title: '待办事项', hint: '可勾选任务', run: (chain) => chain.toggleTaskList() },
  { id: 'quote', icon: '❝', title: '引用', hint: '突出说明或引文', run: (chain) => chain.toggleBlockquote() },
  { id: 'code', icon: '</>', title: '代码块', hint: '保留代码格式', run: (chain) => chain.toggleCodeBlock() },
]

const visibleSlashActions = computed(() => {
  const query = slashMenu.value.query
  return query ? slashActions.filter((item) => `${item.title}${item.hint}${item.id}`.toLowerCase().includes(query)) : slashActions
})

function runSlashAction(action) {
  if (!editor.value) return
  const { from } = slashMenu.value
  const chain = editor.value.chain().focus().deleteRange({ from, to: editor.value.state.selection.from })
  action.run(chain).run()
  slashMenu.value.open = false
}

function runToolbar(action) {
  if (editor.value) action(editor.value.chain().focus()).run()
}

function runInlineFormat(format) {
  if (!editor.value) return
  const chain = editor.value.chain().focus()
  if (format === 'bold') chain.toggleBold()
  if (format === 'italic') chain.toggleItalic()
  if (format === 'underline') chain.toggleUnderline()
  if (format === 'strike') chain.toggleStrike()
  if (format === 'code') chain.toggleCode()
  chain.run()
}

function applyBlockStyle(event) {
  if (!editor.value) return
  const style = event.target.value
  const chain = editor.value.chain().focus()
  if (style === 'paragraph') chain.setParagraph()
  if (style.startsWith('heading-')) chain.setHeading({ level: Number(style.at(-1)) })
  if (style === 'bullet') chain.toggleBulletList()
  if (style === 'ordered') chain.toggleOrderedList()
  if (style === 'task') chain.toggleTaskList()
  if (style === 'quote') chain.toggleBlockquote()
  if (style === 'code-block') chain.toggleCodeBlock()
  chain.run()
  blockStyle.value = style
}

function focusEditor() {
  editor.value?.commands.focus('end')
}

function shouldShowBubble({ state }) {
  return !state.selection.empty && state.doc.textBetween(state.selection.from, state.selection.to, ' ').trim().length > 0
}

function shouldShowFloating({ state }) {
  const { $from, empty } = state.selection
  return empty && $from.parent.type.name === 'paragraph' && $from.parent.textContent.length === 0
}

watch(() => props.modelValue, (value) => {
  if (!editor.value) return
  const next = String(value || '')
  if (next !== editor.value.getMarkdown()) editor.value.commands.setContent(next, { contentType: 'markdown', emitUpdate: false })
})

watch(() => props.highlightFindings, () => {
  if (editor.value) editor.value.view.dispatch(editor.value.state.tr.setMeta(issueHighlightKey, 'refresh'))
}, { deep: true })

watch(fullscreen, async (expanded) => {
  if (expanded) {
    await nextTick()
    focusEditor()
  }
})

onBeforeUnmount(() => editor.value?.destroy())
</script>

<template>
  <section :class="['notion-editor', { fullscreen }]">
    <div class="notion-editor-toolbar" aria-label="文档格式工具">
      <span>块式编辑 · 输入 <kbd>/</kbd> 插入内容块</span>
      <div>
        <button type="button" class="expand-editor" @click="fullscreen = !fullscreen">{{ fullscreen ? '退出全屏' : '展开全屏编辑' }}</button>
      </div>
    </div>

    <div class="notion-editor-shell" @mousedown.self="focusEditor">
      <EditorContent v-if="editor" :editor="editor" />
      <BubbleMenu v-if="editor" :editor="editor" :should-show="shouldShowBubble" :update-delay="80" :options="{ placement: 'top', offset: 10 }" class="selection-menu">
        <select aria-label="选中文字的块类型" :value="blockStyle" @mousedown.stop @change="applyBlockStyle">
          <option value="paragraph">文本</option><option value="heading-1">标题 1</option><option value="heading-2">标题 2</option><option value="heading-3">标题 3</option><option value="heading-4">标题 4</option><option value="bullet">项目符号列表</option><option value="ordered">有序列表</option><option value="task">待办事项</option><option value="quote">引用</option><option value="code-block">代码块</option>
        </select>
        <span class="menu-divider"></span>
        <button type="button" title="加粗" :class="{ active: editor.isActive('bold') }" @mousedown.prevent.stop="runInlineFormat('bold')"><b>B</b></button>
        <button type="button" title="斜体" :class="{ active: editor.isActive('italic') }" @mousedown.prevent.stop="runInlineFormat('italic')"><i>I</i></button>
        <button type="button" title="下划线" :class="{ active: editor.isActive('underline') }" @mousedown.prevent.stop="runInlineFormat('underline')"><u>U</u></button>
        <button type="button" title="删除线" :class="{ active: editor.isActive('strike') }" @mousedown.prevent.stop="runInlineFormat('strike')"><s>S</s></button>
        <button type="button" title="行内代码" :class="{ active: editor.isActive('code') }" @mousedown.prevent.stop="runInlineFormat('code')">&lt;/&gt;</button>
        <span class="menu-divider"></span>
        <button type="button" title="项目符号列表" @mousedown.prevent.stop="runToolbar((chain) => chain.toggleBulletList())">列表</button>
        <button type="button" title="有序列表" @mousedown.prevent.stop="runToolbar((chain) => chain.toggleOrderedList())">编号</button>
        <button type="button" title="待办事项" @mousedown.prevent.stop="runToolbar((chain) => chain.toggleTaskList())">待办</button>
      </BubbleMenu>
      <FloatingMenu v-if="editor" :editor="editor" :should-show="shouldShowFloating" class="empty-block-menu">
        <button type="button" @mousedown.prevent="runToolbar((chain) => chain.toggleHeading({ level: 1 }))">标题</button>
        <button type="button" @mousedown.prevent="runToolbar((chain) => chain.toggleBulletList())">列表</button>
        <button type="button" @mousedown.prevent="runToolbar((chain) => chain.toggleTaskList())">待办</button>
      </FloatingMenu>
    </div>

    <Teleport to="body">
      <div v-if="slashMenu.open && visibleSlashActions.length" class="slash-command-menu" :style="{ left: `${slashMenu.x}px`, top: `${slashMenu.y}px` }" @mousedown.prevent>
        <p>基础块</p>
        <button v-for="action in visibleSlashActions" :key="action.id" type="button" @click="runSlashAction(action)">
          <b>{{ action.icon }}</b><span>{{ action.title }}<small>{{ action.hint }}</small></span>
        </button>
      </div>
    </Teleport>
  </section>
</template>

<style scoped>
.notion-editor{min-width:0}.notion-editor-toolbar{display:flex;align-items:center;justify-content:space-between;gap:12px;margin-bottom:10px;padding:8px 10px;border:1px solid #dfe9e2;border-radius:10px;background:#f8fbf9}.notion-editor-toolbar>span{color:#698077;font-size:12px}.notion-editor-toolbar kbd{padding:1px 4px;border:1px solid #d5e1da;border-radius:4px;background:#fff;font:inherit}.notion-editor-toolbar button,.selection-menu button,.empty-block-menu button{min-height:30px;padding:0 9px;border:1px solid #d2e0d7;border-radius:6px;color:#285f4a;background:#fff;font:inherit;font-size:12px;cursor:pointer}.notion-editor-toolbar button:hover,.selection-menu button:hover,.empty-block-menu button:hover,.selection-menu button.active{border-color:#7aac91;background:#eff8f2}.notion-editor-toolbar .expand-editor{color:#17694f;font-weight:800}.notion-editor-shell{min-height:420px;border:1px solid #cfded5;border-radius:12px;background:#fcfefd;cursor:text}.notion-editor-shell:focus-within{border-color:#4f9d7a;box-shadow:0 0 0 3px rgba(79,157,122,.13)}.notion-editor :deep(.notion-editor-content){min-height:420px;padding:clamp(18px,2.5vw,30px);color:#213d32;line-height:1.8;outline:0;overflow-wrap:anywhere}.notion-editor :deep(.notion-editor-content.is-editor-empty:first-child::before){content:attr(data-placeholder);float:left;height:0;color:#91a099;pointer-events:none}.notion-editor :deep(h1),.notion-editor :deep(h2),.notion-editor :deep(h3),.notion-editor :deep(h4){margin:1.1em 0 .48em;color:#173f31;line-height:1.32}.notion-editor :deep(h1){font-size:2em}.notion-editor :deep(h2){font-size:1.55em}.notion-editor :deep(h3){font-size:1.25em}.notion-editor :deep(h4){font-size:1.08em}.notion-editor :deep(p){margin:.55em 0}.notion-editor :deep(ul),.notion-editor :deep(ol){margin:.6em 0;padding-left:1.55em}.notion-editor :deep(li+li){margin-top:.22em}.notion-editor :deep(ul[data-type="taskList"]){padding-left:.35em;list-style:none}.notion-editor :deep(ul[data-type="taskList"] li){display:flex;gap:.55em}.notion-editor :deep(ul[data-type="taskList"] label){margin-top:.34em}.notion-editor :deep(blockquote){margin:.8em 0;padding:.2em 1em;border-left:3px solid #78a98c;color:#547064;background:#f4faf6}.notion-editor :deep(code){padding:.12em .34em;border-radius:4px;color:#8a4431;background:#fff1ed;font:12px ui-monospace,SFMono-Regular,Consolas,monospace}.notion-editor :deep(em){display:inline-block;font-style:italic;transform:skewX(-8deg)}.notion-editor :deep(pre){padding:12px;border-radius:8px;color:#d8ede0;background:#173e30;overflow:auto}.notion-editor :deep(pre code){padding:0;color:inherit;background:transparent}.notion-editor :deep(.document-issue){padding:0 2px;border-radius:3px;color:#a52f2a;background:rgba(222,70,61,.16);box-shadow:inset 0 -2px 0 #dd463d}.notion-editor :deep(.document-issue.issue-sensitive){color:#962822;background:rgba(210,54,46,.19);box-shadow:inset 0 -2px 0 #c9342d}.selection-menu,.empty-block-menu{display:flex;align-items:center;gap:4px;padding:6px;border:1px solid #d5e2db;border-radius:9px;background:#fff;box-shadow:0 10px 28px rgba(24,60,45,.16)}.selection-menu select{height:30px;max-width:106px;padding:0 23px 0 8px;border:0;border-radius:6px;color:#285f4a;background:#f4f8f5;font:700 12px inherit;cursor:pointer}.selection-menu select:focus{outline:2px solid rgba(79,157,122,.28)}.selection-menu .menu-divider{width:1px;height:21px;margin:0 2px;background:#dce7df}.empty-block-menu{gap:6px}.slash-command-menu{position:fixed;z-index:1300;width:260px;max-height:min(420px,calc(100vh - 32px));overflow:auto;padding:7px;border:1px solid #d7e4dd;border-radius:10px;background:#fff;box-shadow:0 16px 40px rgba(19,57,42,.2)}.slash-command-menu p{margin:5px 8px 7px;color:#83958b;font-size:11px;font-weight:800}.slash-command-menu button{display:flex;align-items:center;gap:9px;width:100%;padding:8px;border:0;border-radius:7px;color:#28463a;background:transparent;text-align:left;cursor:pointer}.slash-command-menu button:hover{background:#edf7f0}.slash-command-menu b{display:grid;flex:0 0 30px;place-items:center;height:30px;border-radius:6px;color:#17694f;background:#e7f3eb;font-size:11px}.slash-command-menu span{font-size:13px;font-weight:750}.slash-command-menu small{display:block;margin-top:1px;color:#83958b;font-size:11px;font-weight:500}.notion-editor.fullscreen{position:fixed;inset:0;z-index:1200;display:block;overflow:auto;padding:clamp(18px,3vw,46px);background:#f6faf7}.notion-editor.fullscreen>.notion-editor-toolbar,.notion-editor.fullscreen>.notion-editor-shell{max-width:1080px;margin-right:auto;margin-left:auto}.notion-editor.fullscreen .notion-editor-shell,.notion-editor.fullscreen :deep(.notion-editor-content){min-height:calc(100vh - 140px);background:#fff}@media(max-width:760px){.notion-editor-toolbar{align-items:stretch;flex-direction:column}.notion-editor.fullscreen{padding:16px}.notion-editor.fullscreen .notion-editor-shell,.notion-editor.fullscreen :deep(.notion-editor-content){min-height:calc(100vh - 180px)}.selection-menu{max-width:calc(100vw - 20px);overflow:auto}}
</style>
