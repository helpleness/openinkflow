<script setup>
import { onMounted, reactive, ref, watch } from 'vue'
import { KeyRound, Loader2, RefreshCcw, Save } from 'lucide-vue-next'

import { fetchModelSettings, saveModelSettings } from '../../systemApi'
import LocalAIEnginePanel from './LocalAIEnginePanel.vue'

const props = defineProps({ tenantId: { type: Number, default: 0 } })
const emit = defineEmits(['notice'])

const loading = ref(false)
const saving = ref(false)
const settings = reactive({
  base_url: '', api_key: '', has_api_key: false, model_default: '',
  ocr_base_url: '', ocr_api_key: '', has_ocr_api_key: false, ocr_model: '',
})

function assign(data = {}) {
  Object.assign(settings, {
    base_url: data.base_url || '', api_key: '', has_api_key: Boolean(data.has_api_key), model_default: data.model_default || '',
    ocr_base_url: data.ocr_base_url || '', ocr_api_key: '', has_ocr_api_key: Boolean(data.has_ocr_api_key), ocr_model: data.ocr_model || '',
  })
}

async function load() {
  if (!props.tenantId) return
  loading.value = true
  try { assign(await fetchModelSettings(props.tenantId)) } catch (error) { emit('notice', { type: 'error', text: error.message }) } finally { loading.value = false }
}

async function save() {
  saving.value = true
  try {
    const data = await saveModelSettings(props.tenantId, { ...settings })
    assign(data)
    emit('notice', { type: 'success', text: '模型配置已保存。密钥已安全加密存储，不会返回到页面。' })
  } catch (error) { emit('notice', { type: 'error', text: error.message }) } finally { saving.value = false }
}

watch(() => props.tenantId, load)
onMounted(load)
</script>

<template>
  <section class="model-config-page">
    <header class="page-head">
      <div>
        <p class="eyebrow">MODEL CONNECTIONS</p>
        <span>配置当前租户可用的模型连接。密钥会安全加密存储，且不会返回到页面。</span>
      </div>
      <button class="text-button" type="button" :disabled="loading" @click="load">
        <RefreshCcw :size="16" />重新读取
      </button>
    </header>

    <form class="model-grid" @submit.prevent="save">
      <section class="model-card primary-model">
        <header>
          <div><p>PRIMARY LLM</p><h3>主模型连接</h3></div>
          <KeyRound :size="20" />
        </header>
        <p>用于写作、检索总结等主要工作流的 OpenAI Chat Completions 兼容服务。</p>
        <label>Base URL<input v-model.trim="settings.base_url" inputmode="url" placeholder="https://example.com/v1" /></label>
        <label>API Key<input v-model="settings.api_key" type="password" autocomplete="new-password" :placeholder="settings.has_api_key ? '已保存；留空则保持不变' : '可留空，使用无密钥服务'" /></label>
        <label>默认模型<input v-model.trim="settings.model_default" placeholder="例如：gpt-4.1-mini" /></label>
        <div class="save-row"><button class="primary" type="submit" :disabled="saving"><Loader2 v-if="saving" class="spin" :size="17" /><Save v-else :size="17" />保存配置</button></div>
      </section>

      <section class="model-card ocr-model">
        <header>
          <div><p>OPTIONAL OCR SEMANTICS</p><h3>OCR 图片语义总结模型</h3></div>
          <span class="optional">可选</span>
        </header>
        <p>为识别出的文字、表格和标题补充摘要与结构说明；全部留空时自动复用主模型。</p>
        <label>语义模型 Base URL<input v-model.trim="settings.ocr_base_url" inputmode="url" placeholder="留空：复用主模型 Base URL" /></label>
        <label>语义模型 API Key<input v-model="settings.ocr_api_key" type="password" autocomplete="new-password" :placeholder="settings.has_ocr_api_key ? '已保存；留空则保持不变' : '留空：复用主模型 API Key'" /></label>
        <label>语义模型名称<input v-model.trim="settings.ocr_model" placeholder="留空：复用主模型默认模型" /></label>
        <div class="ocr-save-row"><button class="primary" type="button" :disabled="saving" @click="save"><Loader2 v-if="saving" class="spin" :size="17" /><Save v-else :size="17" />保存 OCR 配置</button></div>
      </section>

      <section class="local-model"><LocalAIEnginePanel :tenant-id="tenantId" /></section>
    </form>
  </section>
</template>

<style scoped>
.model-config-page { display: grid; gap: 20px; }
.page-head { display: flex; align-items: center; justify-content: space-between; gap: 20px; padding: 18px 20px; border: 1px solid #dbe6de; border-radius: 16px; background: rgba(255, 255, 255, .78); box-shadow: 0 10px 24px rgba(31, 61, 47, .04); }
.eyebrow, .model-card header p { margin: 0 0 5px; color: #57806d; font-size: 10px; font-weight: 800; letter-spacing: .14em; }
.page-head span { display: block; color: #697c72; font-size: 13px; line-height: 1.65; }
.text-button { display: inline-flex; min-height: 38px; flex: none; align-items: center; gap: 6px; padding: 0 10px; border: 0; border-radius: 8px; color: #1a6953; background: transparent; font: inherit; font-size: 13px; font-weight: 750; cursor: pointer; }
.text-button:hover { background: #edf6f0; }
.model-grid { display: grid; grid-template-columns: repeat(3, minmax(0, 1fr)); gap: 20px; align-items: stretch; }
.model-card { display: grid; align-content: start; gap: 16px; min-width: 0; min-height: 100%; box-sizing: border-box; padding: 28px; border: 1px solid #dbe5de; border-radius: 18px; background: #fff; box-shadow: 0 13px 30px rgba(32, 61, 47, .055); }
.model-card header { display: flex; align-items: flex-start; justify-content: space-between; gap: 14px; }
.model-card header p { color: #628c77; }
.model-card h3 { margin: 0; color: #173f31; font-size: 23px; letter-spacing: -.035em; }
.model-card > p { margin: 0; color: #667a70; font-size: 13px; line-height: 1.7; }
.model-card label { display: grid; gap: 7px; color: #385647; font-size: 12px; font-weight: 750; }
.model-card input { width: 100%; min-height: 44px; padding: 0 12px; border: 1px solid #cfddd4; border-radius: 9px; outline: 0; color: #192820; background: #fbfdfb; font: inherit; transition: border-color .18s ease, box-shadow .18s ease, background .18s ease; }
.model-card input:focus { border-color: #619a80; background: #fff; box-shadow: 0 0 0 4px rgba(56, 130, 96, .1); }
.primary-model { border-top: 3px solid #2c7a5e; }
.ocr-model { border-top: 3px solid #d39a50; }
.optional { padding: 5px 9px; border-radius: 999px; color: #7d5722; background: #fff2dc; font-size: 11px; font-weight: 800; }
.save-row, .ocr-save-row { display: flex; align-items: center; justify-content: center; margin-top: auto; padding-top: 10px; }
.primary { display: inline-flex; min-height: 42px; align-items: center; justify-content: center; gap: 7px; padding: 0 15px; border: 0; border-radius: 9px; color: #fff; background: #176149; box-shadow: 0 9px 18px rgba(23, 97, 73, .16); font: inherit; font-size: 13px; font-weight: 750; cursor: pointer; }
.primary:hover { background: #104e3b; }
.primary:disabled, .text-button:disabled { cursor: not-allowed; opacity: .6; }
.save-row .primary, .ocr-save-row .primary { min-width: 132px; }
.local-model { display: flex; min-width: 0; }
.local-model :deep(.engine-card) { display: flex; width: 100%; min-height: 0; height: 100%; box-sizing: border-box; flex-direction: column; padding: 26px 28px; border-radius: 18px; }
.local-model :deep(.engine-actions) { justify-content: center; margin-top: auto; }
.local-model :deep(.engine-actions button) { min-width: 116px; }
.spin { animation: spin .9s linear infinite; }
@keyframes spin { to { transform: rotate(360deg); } }
@media (max-width: 1180px) { .model-grid { grid-template-columns: repeat(2, minmax(0, 1fr)); } .local-model { grid-column: 1 / -1; } .model-card { min-height: auto; } }
@media (max-width: 900px) { .model-grid { grid-template-columns: 1fr; } .local-model { grid-column: auto; } }
@media (max-width: 620px) { .page-head, .save-row, .ocr-save-row { align-items: stretch; flex-direction: column; } .model-card { padding: 22px; } .primary { width: 100%; } }
</style>
