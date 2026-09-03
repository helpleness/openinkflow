<script setup>
defineProps({ logs: { type: Array, default: () => [] } })
defineEmits(['refresh'])
function formatTime(value) { if (!value) return '—'; const date = new Date(value); return Number.isNaN(date.getTime()) ? '—' : date.toLocaleString('zh-CN', { hour12: false }) }
</script>

<template>
  <section class="panel"><div class="table-wrap"><table><thead><tr><th>时间</th><th>操作</th><th>资源</th><th>结果</th><th>状态</th><th>来源</th><th class="audit-actions"><button class="text-button" type="button" @click="$emit('refresh')">刷新</button></th></tr></thead><tbody><tr v-for="entry in logs" :key="entry.ID"><td>{{ formatTime(entry.CreatedAt) }}</td><td>{{ entry.action }}</td><td><code>{{ entry.path || entry.resource || '—' }}</code></td><td><span :class="['result', entry.result]">{{ entry.result }}</span></td><td>{{ entry.status_code || '—' }}</td><td>{{ entry.client_ip || '—' }}</td><td class="audit-actions"></td></tr><tr v-if="!logs.length"><td colspan="7" class="muted">暂无审计记录。</td></tr></tbody></table></div></section>
</template>

<style scoped>
.panel{padding:24px;border:1px solid #dce5df;border-radius:14px;background:#fff;box-shadow:0 8px 20px rgba(30,58,44,.04)}.text-button{border:0;color:#1b6854;background:none;font-weight:700}.table-wrap{overflow-x:auto}table{width:100%;border-collapse:collapse;font-size:13px}th,td{padding:12px 10px;border-bottom:1px solid #e8eeea;text-align:left;white-space:nowrap}th{color:#61736a;font-weight:700}td code{color:#476c5d}.audit-actions{width:76px;text-align:right}.result{padding:3px 7px;border-radius:999px;font-size:12px}.result.success{color:#246241;background:#e3f4e8}.result.failure{color:#973b31;background:#fff0ed}.muted{color:#7a8981}
</style>

<style scoped>
.panel{min-height:520px;padding:26px}.table-wrap{border:1px solid #e1eae3;border-radius:9px}.table-wrap table{background:#fff}.table-wrap th{background:#f7faf8}.table-wrap td,.table-wrap th{height:58px;padding:13px 14px}
</style>
