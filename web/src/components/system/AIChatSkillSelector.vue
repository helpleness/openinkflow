<script setup>
import { computed, nextTick, onBeforeUnmount, ref, useId, watch } from 'vue'
import { Check, ChevronDown, Search, Sparkles, Wrench } from 'lucide-vue-next'

const props = defineProps({
  skills: { type: Array, default: () => [] },
  mode: { type: String, default: 'auto' },
  selectedIds: { type: Array, default: () => [] },
  disabled: { type: Boolean, default: false },
})
const emit = defineEmits(['update:mode', 'update:selectedIds'])

const maxSkills = 3
const panelId = useId()
const modeTrigger = ref(null)
const panel = ref(null)
const searchInput = ref(null)
const activePanel = ref(null)
const query = ref('')
const position = ref({})

const modeOptions = [
  { value: 'auto', label: '自动匹配' },
  { value: 'manual', label: '手动选择' },
  { value: 'none', label: '已关闭' },
]

const filteredSkills = computed(() => {
  const term = query.value.trim().toLocaleLowerCase()
  return props.skills.filter((skill) => skill.enabled && `${skill.name || ''}\n${skill.description || ''}`.toLocaleLowerCase().includes(term))
})
const selectedSkills = computed(() => props.selectedIds.map((id) => props.skills.find((skill) => String(skill.id) === String(id))).filter(Boolean))
const selectedCount = computed(() => props.selectedIds.length)
const skillSummary = computed(() => {
  if (props.mode === 'none') return 'Skill · 已关闭'
  if (props.mode !== 'manual') return 'Skill · 自动匹配'
  if (selectedSkills.value.length === 1) return `Skill · ${selectedSkills.value[0].name || '未命名 Skill'}`
  if (selectedCount.value > 1) return `Skill · 已选 ${selectedCount.value} 项`
  return 'Skill · 手动选择'
})

function currentTrigger() {
  return modeTrigger.value
}

function updatePosition() {
  const trigger = currentTrigger()
  if (!trigger || !panel.value) return
  const rect = trigger.getBoundingClientRect()
  const width = activePanel.value === 'skills' ? 360 : 280
  const actualWidth = Math.min(width, window.innerWidth - 16)
  const above = rect.top - 14
  const below = window.innerHeight - rect.bottom - 14
  const placeAbove = above >= Math.min(panel.value.scrollHeight, 420) || above >= below
  position.value = {
    width: `${actualWidth}px`,
    left: `${Math.max(8, Math.min(rect.left, window.innerWidth - actualWidth - 8))}px`,
    maxHeight: `${Math.min(420, Math.max(0, placeAbove ? above : below))}px`,
    ...(placeAbove ? { bottom: `${window.innerHeight - rect.top + 8}px` } : { top: `${rect.bottom + 8}px` }),
  }
}

function close(restoreFocus = false) {
  const trigger = currentTrigger()
  activePanel.value = null
  query.value = ''
  if (restoreFocus) trigger?.focus?.()
}

async function openPanel(type) {
  if (activePanel.value === type) return close(true)
  if (props.disabled || (type === 'skills' && props.mode !== 'manual')) return
  activePanel.value = type
  query.value = ''
  await nextTick()
  updatePosition()
  if (type === 'skills') searchInput.value?.focus()
  else panel.value?.querySelector('[aria-pressed="true"]')?.focus()
}

function setMode(mode) {
  emit('update:mode', mode)
  if (mode !== 'manual') return close(true)
  activePanel.value = 'skills'
  query.value = ''
  nextTick(() => {
    updatePosition()
    searchInput.value?.focus()
  })
}

function toggleSkill(skill, event) {
  if (props.disabled) return
  if (!event.target.checked) {
    emit('update:selectedIds', props.selectedIds.filter((id) => String(id) !== String(skill.id)))
  } else if (selectedCount.value < maxSkills) {
    emit('update:selectedIds', [...props.selectedIds, skill.id])
  } else {
    event.target.checked = false
  }
}

function onOutside(event) {
  if (!modeTrigger.value?.contains(event.target) && !panel.value?.contains(event.target)) close()
}
function onEscape(event) {
  if (event.key !== 'Escape') return
  event.preventDefault()
  close(true)
}
function removeListeners() {
  document.removeEventListener('pointerdown', onOutside)
  document.removeEventListener('focusin', onOutside)
  document.removeEventListener('keydown', onEscape)
  window.removeEventListener('resize', updatePosition)
  window.removeEventListener('scroll', updatePosition, true)
}
watch(activePanel, (visible) => {
  if (!visible) return removeListeners()
  document.addEventListener('pointerdown', onOutside)
  document.addEventListener('focusin', onOutside)
  document.addEventListener('keydown', onEscape)
  window.addEventListener('resize', updatePosition)
  window.addEventListener('scroll', updatePosition, true)
})
watch(() => props.disabled, (disabled) => { if (disabled) close() })
onBeforeUnmount(removeListeners)
</script>

<template>
  <button ref="modeTrigger" class="skill-trigger" type="button" :disabled="disabled" :title="skillSummary" aria-haspopup="menu" :aria-expanded="Boolean(activePanel)" :aria-controls="activePanel ? panelId : undefined" @click="openPanel('mode')">
    <Sparkles :size="14" /><span>{{ skillSummary }}</span><ChevronDown :size="12" />
  </button>

  <span class="mcp-indicator" title="远程 MCP 工具会在模型判断需要时自动调用。">
    <Wrench :size="14" />MCP · 按需
  </span>

  <Teleport to="body">
    <div v-if="activePanel" :id="panelId" ref="panel" class="skill-popover" :class="{ 'is-skill-panel': activePanel === 'skills' }" :style="position" :role="activePanel === 'skills' ? 'dialog' : 'menu'" :aria-label="activePanel === 'skills' ? '选择具体 Skill' : '选择 Skill 使用方式'">
      <div v-if="activePanel === 'mode'" class="mode-menu">
        <button v-for="item in modeOptions" :key="item.value" type="button" class="mode-item" :class="{ active: mode === item.value }" :aria-pressed="mode === item.value" :disabled="disabled" @click="setMode(item.value)">
          <span class="mode-item-main"><Check v-if="mode === item.value" :size="14" /><strong>{{ item.label }}</strong></span>
        </button>
      </div>

      <template v-else>
        <header class="skill-panel-header"><strong>选择 Skill</strong></header>
        <div class="skill-search">
          <Search :size="14" />
          <input ref="searchInput" v-model="query" type="search" placeholder="搜索 Skill..." aria-label="搜索 Skill" />
        </div>
        <div class="skill-options" aria-label="可选 Skill">
          <label v-for="skill in filteredSkills" :key="skill.id" class="skill-option" :class="{ unavailable: !selectedIds.some((id) => String(id) === String(skill.id)) && selectedCount >= maxSkills }">
            <input type="checkbox" :checked="selectedIds.some((id) => String(id) === String(skill.id))" :disabled="disabled || (!selectedIds.some((id) => String(id) === String(skill.id)) && selectedCount >= maxSkills)" @change="toggleSkill(skill, $event)" />
            <span>
              <strong :title="skill.name">{{ skill.name }}</strong>
              <small v-if="skill.description" :title="skill.description">{{ skill.description }}</small>
            </span>
          </label>
          <p v-if="!filteredSkills.length" class="skill-empty">{{ query.trim() ? '没有匹配的 Skill' : '暂无已启用的 Skill' }}</p>
        </div>
        <footer class="skill-footer">
          <span aria-live="polite">已选择 {{ selectedCount }} / {{ maxSkills }}</span>
          <button type="button" @click="close(true)">完成</button>
        </footer>
      </template>
    </div>
  </Teleport>
</template>

<style scoped>
.skill-trigger, .mcp-indicator { display: inline-flex; min-width: 0; max-width: 220px; height: 29px; flex: 0 1 auto; align-items: center; gap: 4px; padding: 0 7px; border: 0; border-radius: 6px; color: #587466; background: transparent; font: inherit; font-size: 10px; font-weight: 700; }
.skill-trigger { cursor: pointer; }
.skill-trigger span, .mcp-indicator span { overflow: hidden; text-overflow: ellipsis; white-space: nowrap; }
.skill-trigger svg, .mcp-indicator svg { flex: none; }
.skill-trigger:hover, .skill-trigger[aria-expanded="true"] { background: #f0f7f2; }
.skill-trigger:disabled { opacity: .55; cursor: not-allowed; }
.mcp-indicator { color: #6f7f78; }

.skill-popover { position: fixed; z-index: 1000; display: flex; flex-direction: column; box-sizing: border-box; overflow: hidden; border: 1px solid #e3e8e5; border-radius: 8px; background: #fff; color: #304b3e; box-shadow: 0 4px 16px rgb(25 55 38 / 9%); font-family: inherit; }
.skill-popover.is-skill-panel { width: min(360px, calc(100vw - 24px)); }

.mode-menu { display: grid; gap: 2px; padding: 6px; }
.mode-item { display: flex; min-height: 34px; align-items: center; padding: 7px 9px; border: 0; border-radius: 7px; color: #5c6f66; background: transparent; font: inherit; text-align: left; cursor: pointer; }
.mode-item:hover { background: #f4f8f5; }
.mode-item.active { background: #eef7f0; color: #1c7658; }
.mode-item-main { display: flex; align-items: center; gap: 5px; }
.mode-item-main strong { font-size: 12px; font-weight: 700; }

.skill-panel-header { display: flex; flex: none; align-items: center; min-height: 34px; padding: 0 12px; color: #304b3e; font-size: 13px; }
.skill-panel-header strong { font-weight: 750; }

.skill-search { display: flex; flex: none; align-items: center; gap: 6px; margin: 8px; padding: 0 8px; border: 1px solid #e3e8e5; border-radius: 6px; color: #87948c; }
.skill-search input { box-sizing: border-box; width: 100%; min-width: 0; height: 30px; padding: 0; border: 0; outline: 0; color: #304b3e; background: transparent; font: inherit; font-size: 12px; }
.skill-search:focus-within { border-color: #74a68a; }

.skill-options { min-height: 0; overflow-y: auto; padding: 0 4px 4px; overscroll-behavior: contain; }
.skill-option { display: flex; box-sizing: border-box; min-height: 48px; align-items: center; gap: 9px; padding: 7px 8px; border-radius: 5px; cursor: pointer; }
.skill-option:hover { background: #f4f8f5; }
.skill-option.unavailable { opacity: .5; cursor: not-allowed; }
.skill-option input { flex: none; width: 14px; height: 14px; margin: 0; accent-color: #176149; cursor: inherit; }
.skill-option > span { display: grid; min-width: 0; gap: 3px; }
.skill-option strong, .skill-option small { overflow: hidden; text-overflow: ellipsis; white-space: nowrap; line-height: 16px; }
.skill-option strong { font-size: 12px; font-weight: 650; }
.skill-option small { color: #87948c; font-size: 11px; }
.skill-empty { margin: 0; padding: 20px 8px; color: #87948c; text-align: center; font-size: 12px; }

.skill-footer { display: flex; flex: none; align-items: center; justify-content: space-between; padding: 7px 12px; border-top: 1px solid #edf1ee; color: #87948c; font-size: 12px; }
.skill-footer button { min-height: 26px; padding: 0 8px; border: 0; border-radius: 5px; color: #176149; background: transparent; font: inherit; cursor: pointer; }
.skill-footer button:hover { background: #f0f6f2; }

.skill-trigger:focus-visible, .mcp-trigger:focus-visible, .mode-item:focus-visible, .skill-footer button:focus-visible, .skill-option input:focus-visible { outline: 2px solid #74a68a; outline-offset: 2px; }
@media (max-width: 700px) {
  .skill-trigger, .mcp-indicator { max-width: clamp(68px, calc((100vw - 230px) / 2), 150px); }
}
</style>
