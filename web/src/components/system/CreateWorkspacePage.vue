<script setup>
import { ref } from 'vue'

defineProps({ saving: Boolean })
const emit = defineEmits(['create'])
const form = ref({ name: '', code: '' })
function submit() { emit('create', { name: form.value.name.trim(), code: form.value.code.trim() }) }
</script>

<template>
  <section class="create-page"><article class="panel"><header><p>NEW WORKSPACE</p><h2>新建工作空间</h2><span>创建后，你会自动成为该工作空间的所有者。</span></header><form @submit.prevent="submit"><label>工作空间名称<input v-model="form.name" required maxlength="128" placeholder="例如：办公室公文协作空间" /></label><label>工作空间代码 <small>可选；仅小写字母、数字和连字符。未填写时系统自动生成。</small><input v-model="form.code" maxlength="64" pattern="[a-z][a-z0-9-]{2,63}" placeholder="例如：office-docs" /></label><div class="bootstrap-note"><strong>系统会自动初始化：</strong><span><code>root</code> 根组织、<code>owner</code> 所有者角色，以及你的 owner 成员关系。</span></div><button class="primary" :disabled="saving" type="submit">{{ saving ? '创建中…' : '创建并进入工作空间' }}</button></form></article></section>
</template>

<style scoped>
.create-page{display:grid}.panel{max-width:760px;padding:30px;border:1px solid #dce5df;border-radius:12px;background:#fff;box-shadow:0 10px 28px rgba(30,58,44,.05)}header{margin-bottom:26px}header p{margin:0;color:#6f9683;font-size:10px;font-weight:800;letter-spacing:.14em}h2{margin:8px 0;color:#173f31;font-size:24px}header span,label small{display:block;color:#73857b;font-size:12px;line-height:1.65}form{display:grid;gap:19px}label{display:grid;gap:8px;color:#385c4d;font-size:13px;font-weight:750}input{min-height:44px;padding:0 12px;border:1px solid #cfdcd3;border-radius:8px;color:#192820;background:#fff;font:inherit}input:focus{border-color:#26705b;box-shadow:0 0 0 3px rgba(38,112,91,.12);outline:0}.bootstrap-note{display:grid;gap:5px;padding:14px;border:1px solid #dce9e0;border-radius:9px;color:#547166;background:#f7fbf8;font-size:12px;line-height:1.65}.bootstrap-note strong{color:#315b4c}.bootstrap-note code{padding:1px 4px;border-radius:4px;color:#286b53;background:#e5f2e9}.primary{width:max-content;min-height:42px;padding:0 16px;border:0;border-radius:8px;color:#fff;background:#1e6956;font:inherit;font-weight:700;cursor:pointer}.primary:disabled{opacity:.65;cursor:wait}@media(max-width:600px){.panel{padding:20px}.primary{width:100%}}
</style>
