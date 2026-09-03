import katex from 'katex'
import MarkdownIt from 'markdown-it'

const markdown = new MarkdownIt({
  html: false,
  breaks: true,
  linkify: false,
  typographer: false,
})
markdown.renderer.rules.table_open = () => '<div class="markdown-table-wrap"><table>'
markdown.renderer.rules.table_close = () => '</table></div>'

export function idOf(item) { return item?.ID || item?.id }

export function escapeHtml(text) {
  return String(text || '').replace(/[&<>"']/g, (char) => ({ '&': '&amp;', '<': '&lt;', '>': '&gt;', '"': '&quot;', "'": '&#39;' })[char])
}

function renderFormula(formula, displayMode) {
  try {
    return katex.renderToString(String(formula || '').trim(), {
      displayMode,
      throwOnError: false,
      strict: 'ignore',
      trust: false,
      output: 'htmlAndMathml',
    })
  } catch {
    return `<code class="markdown-math-error">${escapeHtml(formula)}</code>`
  }
}

export function renderMarkdown(text) {
  const codeBlocks = []
  const inlineCode = []
  const mathBlocks = []
  const inlineMath = []
  let source = String(text || '').replace(/\r\n?/g, '\n').replace(/```([\w-]*)\n([\s\S]*?)```/g, (_, language, code) => {
    const index = codeBlocks.length
    codeBlocks.push(`<pre><code class="language-${language || 'text'}">${escapeHtml(code.replace(/\n$/, ''))}</code></pre>`)
    return `\nINKFLOWCODEBLOCK${index}TOKEN\n`
  })
  source = source.replace(/`([^`\n]+)`/g, (_, code) => {
    const index = inlineCode.length
    inlineCode.push(`<code>${escapeHtml(code)}</code>`)
    return `INKFLOWINLINECODE${index}TOKEN`
  })
  const addMathBlock = (_, formula) => {
    const index = mathBlocks.length
    mathBlocks.push(`<div class="markdown-math-block">${renderFormula(formula, true)}</div>`)
    return `\nINKFLOWMATHBLOCK${index}TOKEN\n`
  }
  source = source
    .replace(/\$\$([\s\S]*?)\$\$/g, addMathBlock)
    .replace(/\\\[([\s\S]*?)\\\]/g, addMathBlock)
    .replace(/\\\(([^\n]+?)\\\)/g, (_, formula) => {
      const index = inlineMath.length
      inlineMath.push(`<span class="markdown-math-inline">${renderFormula(formula, false)}</span>`)
      return `INKFLOWINLINEMATH${index}TOKEN`
    })
    .replace(/(^|[^\\$])\$([^$\n]+?)\$/gm, (match, prefix, formula) => {
      if (!formula.trim() || formula !== formula.trim()) return match
      const index = inlineMath.length
      inlineMath.push(`<span class="markdown-math-inline">${renderFormula(formula, false)}</span>`)
      return `${prefix}INKFLOWINLINEMATH${index}TOKEN`
    })
  return markdown.render(source)
    .replace(/<p>INKFLOWCODEBLOCK(\d+)TOKEN<\/p>/g, (_, index) => codeBlocks[Number(index)] || '')
    .replace(/<p>INKFLOWMATHBLOCK(\d+)TOKEN<\/p>/g, (_, index) => mathBlocks[Number(index)] || '')
    .replace(/INKFLOWINLINECODE(\d+)TOKEN/g, (_, index) => inlineCode[Number(index)] || '')
    .replace(/INKFLOWINLINEMATH(\d+)TOKEN/g, (_, index) => inlineMath[Number(index)] || '')
}

export function renderInlineMarkdown(text) {
  return escapeHtml(text)
    .replace(/`([^`\n]+)`/g, '<code>$1</code>')
    .replace(/\*\*([^*]+)\*\*/g, '<strong>$1</strong>')
    .replace(/\*([^*\n]+)\*/g, '<em>$1</em>')
    .replace(/\n/g, '<br>')
}

export function formatBytes(bytes) {
  const value = Number(bytes || 0)
  if (value <= 0) return '0 MB'
  const units = ['B', 'KB', 'MB', 'GB']
  let size = value
  let index = 0
  while (size >= 1024 && index < units.length - 1) {
    size /= 1024
    index += 1
  }
  return `${size.toFixed(index < 2 ? 0 : 1)} ${units[index]}`
}

export function createTypewriter(append, delay = 18) {
  let queue = ''
  let timer = 0
  let drainWaiters = []
  const resolveDrain = () => {
    if (queue || timer) return
    const waiters = drainWaiters
    drainWaiters = []
    waiters.forEach((resolve) => resolve())
  }
  const tick = () => {
    if (!queue) {
      timer = 0
      resolveDrain()
      return
    }
    const size = queue.charCodeAt(0) > 255 ? 1 : 2
    append(queue.slice(0, size))
    queue = queue.slice(size)
    timer = window.setTimeout(tick, delay)
  }
  return {
    push(text) {
      queue += text || ''
      if (!timer) tick()
    },
    drain() {
      if (!queue && !timer) return Promise.resolve()
      return new Promise((resolve) => drainWaiters.push(resolve))
    },
    flush() {
      if (timer) window.clearTimeout(timer)
      timer = 0
      if (queue) append(queue)
      queue = ''
      resolveDrain()
    },
  }
}

export function listFrom(data) {
  if (Array.isArray(data)) return data
  if (Array.isArray(data?.data)) return data.data
  return data?.list || []
}

export function deepClone(value) {
  return value == null ? value : JSON.parse(JSON.stringify(value))
}

export function listText(items) {
  return Array.isArray(items) ? items.filter(Boolean).join('\n') : ''
}

export function jsonText(value, fallback) {
  return JSON.stringify(value ?? fallback, null, 2)
}

export function parseListText(value) {
  return String(value || '')
    .split(/[\n,，]/)
    .map((item) => item.trim())
    .filter(Boolean)
}

export function parseLineText(value) {
  return String(value || '')
    .split(/\r?\n/)
    .map((item) => item.trim())
    .filter(Boolean)
}

export function parseJsonText(value, fallback, label) {
  if (!String(value || '').trim()) return deepClone(fallback)
  try {
    return JSON.parse(value)
  } catch {
    throw new Error(`${label} 需要是合法 JSON`)
  }
}

export function optionalNumber(value) {
  if (value === '' || value === null || value === undefined) return null
  const num = Number(value)
  return Number.isFinite(num) ? num : null
}

export function requiredNumber(value, fallback = 0) {
  const num = Number(value)
  return Number.isFinite(num) ? num : fallback
}

export function splitCitationUnits(text) {
  return String(text || '')
    .split(/(?<=[。！？!?；;])\s*|\n+/)
    .map((item) => item.trim())
    .filter((item) => item.length >= 12)
    .slice(0, 80)
}

export function citationTokens(text) {
  const matches = String(text || '').match(/[\u4e00-\u9fa5]{2,}|[A-Za-z0-9_-]{3,}/g) || []
  const stop = new Set(['这个', '一种', '进行', '当前', '他们', '我们', '需要', '可以', '没有', '已经', '因为', '所以'])
  return [...new Set(matches.filter((item) => !stop.has(item)).slice(0, 28))]
}

export function buildWritingCitationHints(draft, sections) {
  const refs = sections.flatMap((section) => splitCitationUnits(section.content).map((text) => ({ title: section.title, text, tokens: citationTokens(text) })))
  if (!refs.length) return []
  return splitCitationUnits(draft).map((sentence) => {
    const tokens = citationTokens(sentence)
    const matches = refs.map((ref) => {
      let score = 0
      tokens.forEach((token) => { if (ref.tokens.includes(token) || ref.text.includes(token)) score += 1 })
      return { ...ref, score }
    }).filter((ref) => ref.score >= 2).sort((a, b) => b.score - a.score).slice(0, 3)
    return matches.length ? { sentence, matches } : null
  }).filter(Boolean).slice(0, 12)
}
