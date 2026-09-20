<script setup>
import { ref, watch } from 'vue'

import { searchKnowledge } from '../../officialdocApi'

const props = defineProps({ tenantId: { type: Number, required: true }, organizationId: { type: Number, required: true } })
const emit = defineEmits(['notice'])
const query = ref('')
const limit = ref(6)
const result = ref(null)
const searching = ref(false)

function showError(error) { emit('notice', { type: 'error', text: error?.message || '检索未完成，请稍后重试。' }) }
async function search() {
  if (!props.organizationId) { showError(new Error('请先选择或加入一个组织。')); return }
  if (!query.value.trim()) { showError(new Error('请输入检索问题或关键词。')); return }
  searching.value = true
  try { result.value = await searchKnowledge(props.tenantId, { organization_id: props.organizationId, query: query.value.trim(), limit: limit.value }) } catch (error) { showError(error) } finally { searching.value = false }
}
watch(() => [props.tenantId, props.organizationId], () => { result.value = null })
</script>

<template>
  <section class="search-page">
    <article class="search-hero"><form @submit.prevent="search"><input v-model="query" placeholder="例如：项目验收的时间、责任分工和交付材料是什么？" /><label>证据数量<select v-model.number="limit"><option :value="4">4 条</option><option :value="6">6 条</option><option :value="8">8 条</option></select></label><button type="submit" :disabled="searching">{{ searching ? '检索中…' : '检索证据' }}</button></form></article>
    <article v-if="result" class="results"><div v-if="result.warnings?.length" class="warnings"><strong>检索降级提示</strong><span v-for="warning in result.warnings" :key="warning">{{ warning }}</span></div><div v-if="!result.items?.length" class="empty">未找到能作为证据的切片。可先检查文档是否已完成索引，或换用更具体的关键词。</div><article v-for="(item,index) in result.items" :key="item.chunk_id" class="evidence"><header><span>[E{{ index + 1 }}]</span><div><strong>{{ item.title || item.parent_title || '正文切片' }}</strong><small>《{{ item.document_name }}》 · 切片 {{ item.chunk_index + 1 }} · 综合分 {{ item.score.toFixed(3) }}</small></div><div class="rank"><small v-if="item.vector_rank">向量 #{{ item.vector_rank }}</small><small v-if="item.lexical_rank">词法 #{{ item.lexical_rank }}</small></div></header><pre>{{ item.content }}</pre></article></article>
    <article v-else class="empty initial">检索结果会以 [E1]、[E2]… 证据编号展示，并可直接供受控生成任务引用。</article>
  </section>
</template>

<style scoped>
.search-page{display:grid;gap:22px;align-content:start;max-width:1240px}.search-hero,.results,.empty{padding:clamp(24px,3vw,34px);border:1px solid #e1e9e4;border-radius:16px;background:#fff;box-shadow:0 1px 2px rgba(15,45,34,.03),0 14px 34px rgba(15,45,34,.04)}.search-hero{background:linear-gradient(135deg,#fff 0,#f3faf6 100%)}.search-hero form{display:grid;grid-template-columns:minmax(0,1fr) 150px 132px;align-items:end;gap:10px;margin-top:0}.search-hero input,.search-hero select{width:100%;min-height:46px;padding:0 12px;border:1px solid #cfdcd4;border-radius:9px;color:#20382e;background:#fff}.search-hero input:focus,.search-hero select:focus{border-color:#4f9d7a;outline:0;box-shadow:0 0 0 3px rgba(79,157,122,.13)}.search-hero label{display:grid;gap:6px;color:#466055;font-size:12px;font-weight:750}.search-hero button{align-self:end;min-height:46px;padding:0 18px;border:0;border-radius:9px;color:#fff;background:#17694f;box-shadow:0 1px 2px rgba(13,60,44,.18);font-weight:750}.search-hero button:hover{background:#105a43}.warnings{display:grid;gap:5px;margin-bottom:16px;padding:13px 14px;border:1px solid #f0d49a;border-radius:10px;color:#7f5b1a;background:#fff8e8;font-size:13px}.evidence{padding:22px 0;border-top:1px solid #e8eeea}.evidence:first-of-type{border-top:0}.evidence header{display:flex;gap:13px;align-items:flex-start}.evidence header>span{padding:5px 8px;border-radius:7px;color:#24654f;background:#e8f5ec;font-family:ui-monospace,monospace;font-weight:800}.evidence header div:nth-child(2){display:grid;gap:4px;min-width:0;flex:1}.evidence strong{color:#234b3c}.evidence small{color:#718278}.rank{display:grid;text-align:right}.evidence pre{margin:14px 0 0;padding:16px;border:1px solid #e6eee9;border-radius:10px;color:#45594f;background:#f8fbf9;white-space:pre-wrap;word-break:break-word;font:13px/1.7 ui-monospace,SFMono-Regular,Consolas,monospace}.empty{display:grid;min-height:220px;place-items:center;border-style:dashed;color:#697b71;background:#fbfdfc;text-align:center}.initial{padding:46px}.results{min-height:0!important}@media(max-width:720px){.search-hero form{grid-template-columns:1fr}.rank{display:none}.search-hero,.results,.empty{padding:20px}}
</style>
