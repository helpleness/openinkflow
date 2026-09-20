<script setup>
import { computed, ref, watch } from 'vue'
const props = defineProps({ workspace: { type: Object, default: null }, roles: { type: Array, default: () => [] }, saving: Boolean })
const emit = defineEmits(['save', 'refresh'])
const draft = ref({ is_visible: false, default_role_id: 0 })
const safeRoles = computed(() => Array.isArray(props.roles) ? props.roles : [])
watch(() => props.workspace, (workspace) => { draft.value = { is_visible: Boolean(workspace?.is_visible), default_role_id: Number(workspace?.default_role_id || 0) } }, { immediate: true })
function submit() { emit('save', { ...draft.value, default_role_id: Number(draft.value.default_role_id || 0) }) }
</script>

<template>
  <section class="settings-page"><article class="panel"><header><div><p>WORKSPACE ACCESS</p><h2>工作空间开放设置</h2><span>控制新账号能否申请加入，以及批准后自动获得的角色。</span></div><button class="text-button" type="button" @click="emit('refresh')">刷新</button></header><form @submit.prevent="submit"><label class="visibility"><input v-model="draft.is_visible" type="checkbox" /><span><strong>允许申请加入此工作空间</strong><small>关闭后不会出现在未加入用户的“申请加入工作空间”列表。</small></span></label><label><span>审批默认角色</span><select v-model.number="draft.default_role_id" :disabled="saving"><option :value="0">请选择角色</option><option v-for="role in safeRoles" :key="role.ID" :value="role.ID">{{ role.name }}（{{ role.code }}）</option></select><small>工作空间申请获批后，申请人会在根组织获得此角色。</small></label><button class="primary" :disabled="saving" type="submit">{{ saving ? '保存中…' : '保存开放设置' }}</button></form></article></section>
</template>

<style scoped>
.settings-page{display:grid}.panel{max-width:760px;padding:28px;border:1px solid #dce5df;border-radius:12px;background:#fff;box-shadow:0 10px 28px rgba(30,58,44,.05)}header{display:flex;align-items:flex-start;justify-content:space-between;gap:16px;margin-bottom:26px}header p{margin:0;color:#6f9683;font-size:10px;font-weight:800;letter-spacing:.14em}h2{margin:8px 0;color:#173f31;font-size:23px}header span,label small{display:block;color:#73857b;font-size:12px;line-height:1.65}.text-button{border:0;color:#1b6854;background:none;font:inherit;font-weight:700;cursor:pointer}form{display:grid;gap:20px}label{display:grid;gap:8px;color:#385c4d;font-size:13px;font-weight:750}.visibility{grid-template-columns:auto 1fr;align-items:flex-start;gap:12px;padding:16px;border:1px solid #dce9e0;border-radius:10px;background:#f8fbf8}.visibility input{width:18px;height:18px;margin-top:2px;accent-color:#1e6956}.visibility strong{display:block;color:#244d3d}.visibility small{margin-top:4px}select{min-height:42px;padding:0 12px;border:1px solid #cfdcd3;border-radius:8px;color:#192820;background:#fff;font:inherit}.primary{width:max-content;min-height:42px;padding:0 16px;border:0;border-radius:8px;color:#fff;background:#1e6956;font:inherit;font-weight:700;cursor:pointer}.primary:disabled{opacity:.65;cursor:wait}@media(max-width:600px){.panel{padding:20px}.primary{width:100%}}
</style>
