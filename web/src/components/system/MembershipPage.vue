<script setup>
import { computed, ref } from 'vue'
import { ChevronRight, RefreshCcw, Search, Users } from 'lucide-vue-next'

const props = defineProps({
  organizations: { type: Array, default: () => [] },
  roles: { type: Array, default: () => [] },
  members: { type: Array, default: () => [] },
  users: { type: Array, default: () => [] },
  selectedOrganization: { type: Object, default: null },
  selectedOrganizationID: { type: Number, default: 0 },
})
const emit = defineEmits(['save', 'refresh'])

const dialogMode = ref('')
const organizationFilter = ref('all')
const roleFilter = ref('all')
const searchQuery = ref('')
const form = ref(emptyForm())
const safeUsers = computed(() => Array.isArray(props.users) ? props.users : [])
const safeMembers = computed(() => Array.isArray(props.members) ? props.members : [])
const filteredMembers = computed(() => {
  const query = searchQuery.value.trim().toLowerCase()
  return safeMembers.value.filter((member) => {
    const matchesOrganization = organizationFilter.value === 'all'
      || Number(member.organization_id) === Number(organizationFilter.value)
    const matchesRole = roleFilter.value === 'all'
      || Number(member.role_id) === Number(roleFilter.value)
    const searchableText = [member.username, member.organization_name, member.role_name]
      .filter(Boolean)
      .join(' ')
      .toLowerCase()
    return matchesOrganization && matchesRole && (!query || searchableText.includes(query))
  })
})

function emptyForm() {
  return { user_id: 0, username: '', role_id: 0, organization_id: Number(props.selectedOrganizationID) || 0, mfa_enrollment_required: false }
}
function resetForm() { form.value = emptyForm() }
function openCreate() { resetForm(); dialogMode.value = 'create' }
function openEdit(member) {
  form.value = {
    user_id: Number(member.user_id) || 0,
    username: member.username || '',
    role_id: Number(member.role_id) || 0,
    organization_id: Number(member.organization_id) || 0,
    mfa_enrollment_required: Boolean(member.mfa_enrollment_required),
  }
  dialogMode.value = 'edit'
}
function closeDialog() { dialogMode.value = ''; resetForm() }
function selectedUserName() {
  return safeUsers.value.find((item) => Number(item.id) === Number(form.value.user_id))?.username || form.value.username
}
function submit() {
  const payload = {
    username: dialogMode.value === 'create' ? selectedUserName().trim() : form.value.username.trim(),
    user_id: Number(form.value.user_id) || 0,
    organization_id: Number(form.value.organization_id) || 0,
  }
  if (dialogMode.value === 'edit') {
    payload.role_id = Number(form.value.role_id) || 0
    payload.mfa_enrollment_required = Boolean(form.value.mfa_enrollment_required)
  }
  emit('save', payload)
  closeDialog()
}
</script>

<template>
  <section class="page-stack">
    <article class="panel">
      <header class="member-toolbar">
        <div class="member-filters">
          <label class="organization-filter"><select v-model="organizationFilter" aria-label="显示组织"><option value="all">全部组织</option><option value="0">未分配组织</option><option v-for="organization in props.organizations" :key="organization.ID" :value="String(organization.ID)">{{ organization.name }}</option></select></label>
          <label class="search-field"><Search :size="16" aria-hidden="true" /><input v-model="searchQuery" type="search" placeholder="搜索成员、组织或角色" aria-label="搜索成员、组织或角色" /></label>
          <label class="role-filter"><select v-model="roleFilter" aria-label="角色筛选"><option value="all">全部角色</option><option v-for="role in props.roles" :key="role.ID" :value="String(role.ID)">{{ role.name }}</option></select></label>
          <span class="directory-count">{{ filteredMembers.length }} 人</span>
          <div class="member-actions"><button class="text-button" type="button" @click="emit('refresh')"><RefreshCcw :size="15" />刷新</button></div>
        </div>
      </header>
      <ul class="member-list">
        <li v-for="member in filteredMembers" :key="member.id" class="member-item" tabindex="0" @click="openEdit(member)" @keydown.enter.prevent="openEdit(member)" @keydown.space.prevent="openEdit(member)">
          <span class="member-avatar">{{ (member.username || '?').slice(0, 1).toUpperCase() }}</span>
          <div class="member-details"><strong>{{ member.username || '未知用户' }}</strong><small>{{ member.organization_name || '未分配组织' }} · {{ member.status === 'active' ? '正常' : member.status }}<template v-if="member.mfa_enabled"> · MFA 已启用</template><template v-else-if="member.mfa_enrollment_required"> · MFA 待绑定</template></small></div>
          <span class="role-badge">{{ member.role_name || '待授权' }}</span>
          <span class="edit-affordance">编辑权限 <ChevronRight :size="15" /></span>
        </li>
        <li v-if="!filteredMembers.length" class="member-empty"><span class="empty-icon"><Users :size="18" /></span><div><strong>没有找到匹配成员</strong><small>{{ searchQuery || roleFilter !== 'all' || organizationFilter !== 'all' ? '可以调整筛选条件后再试。' : '当前租户暂无成员。' }}</small></div></li>
      </ul>
    </article>

    <div v-if="dialogMode" class="modal-backdrop" @click.self="closeDialog">
      <section class="modal-card" role="dialog" aria-modal="true" :aria-labelledby="`${dialogMode}-member-dialog-title`">
        <header class="modal-heading"><div><p>{{ dialogMode === 'create' ? 'MEMBERSHIP' : 'MEMBER ACCESS' }}</p><h2 :id="`${dialogMode}-member-dialog-title`">{{ dialogMode === 'create' ? '添加成员' : '编辑成员权限' }}</h2></div><button class="icon-button" type="button" aria-label="关闭" @click="closeDialog">×</button></header>
        <form class="form-stack" @submit.prevent="submit">
          <template v-if="dialogMode === 'create'">
            <label>用户<select v-if="safeUsers.length" v-model.number="form.user_id" required><option :value="0" disabled>请选择用户</option><option v-for="item in safeUsers" :key="item.id" :value="Number(item.id)">{{ item.username }}{{ item.organization_name ? ` · 当前：${item.organization_name}` : ' · 未分配组织' }}</option></select><input v-else v-model="form.username" required maxlength="64" placeholder="输入成员用户名" /></label>
            <p class="form-hint">添加成员只建立组织归属，不会自动授予角色。添加后请点击该成员设置权限。</p>
          </template>
          <template v-else>
            <div class="member-name"><span class="member-avatar">{{ (form.username || '?').slice(0, 1).toUpperCase() }}</span><div><strong>{{ form.username || '未知用户' }}</strong><small>调整此成员的组织与角色</small></div></div>
            <label>角色<select v-model.number="form.role_id" required><option :value="0" disabled>请选择角色</option><option v-for="role in props.roles" :key="role.ID" :value="Number(role.ID)">{{ role.name }} · {{ role.code }}</option></select></label>
            <label class="mfa-requirement"><input v-model="form.mfa_enrollment_required" type="checkbox" /><span><strong>要求下次登录绑定 MFA</strong><small>用户完成账号密码和图片验证码后，会进入独立的验证器绑定页。</small></span></label>
          </template>
          <label>所属组织<select v-model.number="form.organization_id"><option :value="0">未分配组织</option><option v-for="organization in props.organizations" :key="organization.ID" :value="Number(organization.ID)">{{ organization.name }}</option></select></label>
          <footer class="modal-actions"><button class="ghost" type="button" @click="closeDialog">取消</button><button class="primary" type="submit">{{ dialogMode === 'create' ? '添加成员' : '保存权限' }}</button></footer>
        </form>
      </section>
    </div>
  </section>
</template>

<style scoped>
.page-stack{display:grid;gap:20px}.panel{padding:26px;border:1px solid #dce5df;border-radius:12px;background:#fff;box-shadow:0 10px 28px rgba(30,58,44,.05)}.panel-heading,.modal-heading{display:flex;align-items:flex-start;justify-content:space-between;gap:16px}.panel-heading{align-items:center;margin-bottom:10px}.panel-heading p,.modal-heading p{margin:0;color:#5c796c;font-size:11px;font-weight:800;letter-spacing:.12em}.panel-heading h2,.modal-heading h2{margin:6px 0 0;color:#173f31;font-size:22px}.heading-actions,.modal-actions{display:flex;align-items:center;gap:10px}.organization-filter{display:flex;align-items:center;gap:7px;color:#5b7065;font-size:12px;font-weight:700;white-space:nowrap}.organization-filter select{min-height:36px;max-width:160px;padding:0 28px 0 9px;border:1px solid #c9dcd0;border-radius:7px;color:#294d3e;background:#fff;font:inherit}.primary,.ghost,.text-button,.icon-button{font:inherit;cursor:pointer}.primary{min-height:40px;padding:0 14px;border:0;border-radius:8px;color:#fff;background:#1e6956;font-weight:700}.primary:hover{background:#155642}.ghost{min-height:40px;padding:0 14px;border:1px solid #bfd4c7;border-radius:8px;color:#176851;background:#fff;font-weight:700}.text-button{border:0;color:#1b6854;background:none;font-weight:700}.directory-count,.role-badge{padding:5px 8px;border-radius:999px;color:#2d7055;background:#e7f4ea;font-size:12px;font-weight:750}.helper{margin:0 0 18px;color:#65766d;font-size:13px;line-height:1.65}.member-list{display:grid;gap:8px;margin:0;padding:0;list-style:none}.member-list li{display:flex;min-height:66px;align-items:center;gap:11px;padding:13px 14px;border:1px solid #e6eee8;border-radius:9px;background:#fbfdfb}.member-item{cursor:pointer;transition:border-color .15s,box-shadow .15s,background .15s}.member-item:hover,.member-item:focus-visible{border-color:#8fbaa2;background:#f4faf6;box-shadow:0 0 0 3px rgba(38,112,91,.12);outline:0}.member-details{min-width:0}.member-list strong,.member-list small{display:block}.member-list small{margin-top:3px;color:#76877e;font-size:12px}.member-avatar{display:grid;flex:0 0 auto;width:36px;height:36px;place-items:center;border-radius:50%;color:#fff;background:#347664;font-weight:800}.role-badge{margin-left:auto;white-space:nowrap}.edit-affordance{color:#1b6854;font-size:12px;font-weight:750;white-space:nowrap}.muted{justify-content:center;color:#7a8981}.modal-backdrop{position:fixed;z-index:60;inset:0;display:grid;place-items:center;padding:20px;background:rgba(14,28,22,.46)}.modal-card{width:min(520px,100%);padding:26px;border-radius:12px;background:#fff;box-shadow:0 24px 72px rgba(0,0,0,.22)}.modal-heading{align-items:flex-start}.icon-button{display:grid;width:32px;height:32px;place-items:center;border:0;border-radius:7px;color:#52645a;background:#eff3f0;font-size:23px;line-height:1}.form-stack{display:grid;gap:16px;margin-top:22px}.form-stack label{display:grid;gap:7px;color:#4d6258;font-size:13px;font-weight:650}.form-stack input,.form-stack select{width:100%;min-height:44px;padding:0 12px;border:1px solid #cfdcd3;border-radius:8px;color:#192820;background:#fff}.form-stack input:focus,.form-stack select:focus{border-color:#26705b;box-shadow:0 0 0 3px rgba(38,112,91,.12);outline:none}.form-hint{margin:0;padding:10px 12px;border:1px solid #e0ebe4;border-radius:8px;color:#668074;background:#f7faf8;font-size:12px;line-height:1.5}.member-name{display:flex;align-items:center;gap:10px;padding:11px 12px;border:1px solid #e0ebe4;border-radius:8px;background:#f8fbf9}.member-name strong,.member-name small{display:block}.member-name small{margin-top:3px;color:#76877e;font-size:12px}.modal-actions{justify-content:flex-end}@media(max-width:760px){.panel{padding:20px}.heading-actions{flex-wrap:wrap;justify-content:flex-end}.organization-filter{order:3;width:100%;justify-content:flex-end}.member-list li{align-items:flex-start;flex-wrap:wrap}.role-badge{margin-left:47px}.edit-affordance{margin-left:auto}}
</style>

<style scoped>
.mfa-requirement { display: flex !important; align-items: flex-start; gap: 10px; padding: 11px 12px; border: 1px solid #dce9e0; border-radius: 8px; color: #315b4c !important; background: #f7fbf8; }
.form-stack .mfa-requirement input { width: 18px; min-height: 18px; margin: 1px 0 0; padding: 0; accent-color: #1e6956; }
.mfa-requirement strong, .mfa-requirement small { display: block; }
.mfa-requirement strong { font-size: 13px; }
.mfa-requirement small { margin-top: 3px; color: #71857a; font-size: 12px; font-weight: 500; line-height: 1.5; }
</style>

<style scoped>
.member-toolbar{display:flex;align-items:center;justify-content:space-between;gap:16px;margin-bottom:18px}.member-filters,.member-actions{display:flex;align-items:center;gap:10px}.member-actions{margin-left:auto}@media(max-width:760px){.member-toolbar{align-items:flex-start;flex-direction:column}.member-filters,.member-actions{flex-wrap:wrap}.member-actions{align-self:stretch;justify-content:flex-end}.organization-filter{order:initial;width:auto;justify-content:flex-start}}
</style>

<style scoped>
.modal-card{width:min(680px,calc(100vw - 40px));padding:30px 34px}@media(max-width:600px){.modal-card{width:calc(100vw - 32px);padding:24px 20px}}
</style>

<style scoped>
.panel { padding: 28px; }
.member-toolbar { display: flex; align-items: center; justify-content: flex-start; margin-bottom: 22px; padding-bottom: 18px; border-bottom: 1px solid #e7eee9; }
.member-filters { display: flex; min-width: 0; flex: 1 1 auto; align-items: center; gap: 10px; flex-wrap: wrap; }
.organization-filter, .role-filter { min-height: 40px; color: #5b7065; font-size: 12px; font-weight: 700; }
.organization-filter span, .role-filter span { color: #6a7c72; font-size: 12px; font-weight: 750; }
.organization-filter select, .role-filter select { min-height: 40px; padding: 0 30px 0 11px; border-color: #cbdcd1; border-radius: 9px; color: inherit; background: #fbfdfb; font: inherit; }
.organization-filter select { min-width: 122px; }
.role-filter { display: inline-flex; align-items: center; gap: 7px; }
.role-filter select { min-width: 116px; }
.search-field { display: flex; width: 33%; min-width: 0; flex: 0 1 33%; min-height: 40px; align-items: center; gap: 8px; padding: 0 11px; border: 1px solid #cbdcd1; border-radius: 9px; color: #6e8a7b; background: #fbfdfb; transition: border-color .18s ease, box-shadow .18s ease, background .18s ease; }
.search-field:focus-within { border-color: #5c9d7d; background: #fff; box-shadow: 0 0 0 4px rgba(56, 130, 96, .1); }
.search-field input { width: 100%; min-width: 0; min-height: 36px; padding: 0; border: 0; outline: 0; color: #20352b; background: transparent; font: inherit; font-size: 13px; }
.search-field input::placeholder { color: #90a098; }
.member-actions { display: flex; flex: none; align-items: center; justify-content: flex-end; margin-left: 0; white-space: nowrap; }
.member-actions .text-button { display: inline-flex; min-height: 40px; align-items: center; gap: 6px; padding: 0 10px; border-radius: 8px; }
.member-list { gap: 10px; }
.member-list li.member-item { min-height: 74px; padding: 15px 17px; border-color: #e0ebe3; border-radius: 12px; background: #fff; }
.member-list li.member-item:hover, .member-list li.member-item:focus-visible { border-color: #82b69b; background: #f7fbf8; box-shadow: 0 8px 20px rgba(30, 88, 64, .08); }
.member-avatar { width: 40px; height: 40px; background: linear-gradient(145deg, #3b856f, #286652); box-shadow: 0 0 0 4px #edf6f0; }
.member-details strong { color: #173d31; font-size: 14px; }
.member-details small { color: #7a8e84; }
.role-badge { padding: 6px 10px; color: #286b53; background: #e6f3e9; }
.edit-affordance { display: inline-flex; align-items: center; gap: 3px; color: #176851; }
.member-empty { min-height: 150px !important; justify-content: center; gap: 12px; border: 1px dashed #cbdcd1 !important; background: #fbfdfb !important; }
.empty-icon { display: grid; width: 38px; height: 38px; place-items: center; border-radius: 11px; color: #3e856c; background: #e8f4eb; }
.member-empty strong, .member-empty small { display: block; }
.member-empty strong { color: #315b4c; font-size: 14px; }
.member-empty small { margin-top: 4px; color: #819188; font-size: 12px; }
@media (max-width: 900px) {
  .member-toolbar { display: flex; align-items: stretch; flex-direction: column; }
  .member-actions { align-self: flex-end; }
  .search-field { width: auto; flex-basis: 260px; }
}
@media (max-width: 620px) {
  .panel { padding: 20px; }
  .member-filters { align-items: stretch; }
  .organization-filter, .role-filter { flex: 1 1 170px; }
  .organization-filter select, .role-filter select { flex: 1; max-width: none; }
  .search-field { width: 100%; flex-basis: 100%; }
  .member-actions { width: 100%; align-self: stretch; }
  .member-actions .text-button, .member-actions .primary { flex: 1; }
  .member-list li.member-item { display: grid; grid-template-columns: 40px minmax(0, 1fr) auto; align-items: center; gap: 10px; }
  .member-item .role-badge { grid-column: 2; margin-left: 0; width: fit-content; }
  .member-item .edit-affordance { grid-column: 3; grid-row: 2; }
}
</style>
