<script setup>
import { computed, onBeforeUnmount, onMounted, ref } from 'vue'

import AuditLogPage from './components/system/AuditLogPage.vue'
import DocumentTemplatePage from './components/officialdoc/DocumentTemplatePage.vue'
import KnowledgeDocumentPage from './components/officialdoc/KnowledgeDocumentPage.vue'
import KnowledgeSearchPage from './components/officialdoc/KnowledgeSearchPage.vue'
import ApplicationReviewPage from './components/system/ApplicationReviewPage.vue'
import AccountSecurityPage from './components/system/AccountSecurityPage.vue'
import MembershipPage from './components/system/MembershipPage.vue'
import MenuConfigPage from './components/system/MenuConfigPage.vue'
import ModelConfigPage from './components/system/ModelConfigPage.vue'
import OrganizationPage from './components/system/OrganizationPage.vue'
import OrganizationApplicationPage from './components/system/OrganizationApplicationPage.vue'
import RolePermissionPage from './components/system/RolePermissionPage.vue'
import WorkspacePage from './components/system/WorkspacePage.vue'
import WritingTaskPage from './components/officialdoc/WritingTaskPage.vue'
import {
  addMembership,
  applyToOrganization,
  authenticate,
	completePendingMFA,
  createOrganization,
  createRole,
	fetchCaptcha,
  fetchAPIResources,
  fetchAuditLogs,
  fetchCurrentUser,
  fetchOAuthProviders,
  oauthLoginURL,
  fetchMenus,
  fetchMemberships,
  fetchMenuConfigs,
  fetchMembershipApplications,
  fetchGlobalUsers,
  fetchOrganizations,
  fetchPublicOrganizations,
  fetchRoles,
  fetchTenants,
  logout,
  reviewMembershipApplication,
  setOrganizationVisibility,
	setupPendingMFA,
  syncMenuConfigs,
  createMenuConfig,
  updateMenuConfig,
  updateRolePermissions,
} from './systemApi'
import { claimAuthSessionForTab, getAuthToken, setAuthToken, usesCookieSession, usesFrontendInference } from './api'
import { useLocalAI } from './ai/useLocalAI'
import {
  Bell,
  BookOpen,
  Building2,
  ChevronDown,
  ChevronRight,
  ClipboardList,
  FileText,
  Fingerprint,
  LayoutDashboard,
  LockKeyhole,
  LogOut,
  PenLine,
  Search,
  Settings2,
  Shield,
  Sparkles,
  UserRound,
  Users,
} from 'lucide-vue-next'

const activeView = ref('workspace')
const authMode = ref('login')
const user = ref(null)
const tenants = ref([])
const organizations = ref([])
const publicOrganizations = ref([])
const roles = ref([])
const memberships = ref([])
const globalUsers = ref([])
const membershipApplications = ref([])
const auditLogs = ref([])
const apiResources = ref([])
const systemMenus = ref([])
const menus = ref(['workspace'])
const selectedTenantID = ref(0)
const selectedOrganizationID = ref(0)
const assignedOrganizationID = ref(0)
const loading = ref(false)
const systemLoading = ref(false)
const notice = ref('')
const error = ref('')
const toast = ref('')
const credentials = ref({ username: '', password: '', captcha_id: '', captcha_answer: '' })
const captchaImage = ref('')
const captchaLoading = ref(false)
const oauthProviders = ref([])
const oauthLoading = ref('')
const authStep = ref('credentials')
const pendingAuthToken = ref('')
const pendingMFASetup = ref(null)
const pendingMFACode = ref('')
const expandedMenuGroups = ref({})
const { initLocalAI } = useLocalAI()
let toastTimer

// 菜单定义属于前端。所有者首次登录或菜单升级后，由前端将这些键同步
// 到 owner 角色；后端只保存键值并通过 Casbin 自动维护 owner 的 API 权限。
const OWNER_MENU_KEYS = ['workspace', 'personal_center', 'model_config', 'permission_management', 'applications', 'application_reviews', 'organizations', 'roles', 'members', 'audit', 'menu_configs', 'knowledge_base', 'knowledge_documents', 'knowledge_search', 'writing_management', 'document_templates', 'writing_tasks']
const FRONTEND_MENU_DEFINITIONS = [
  { name: '工作台', menu_key: 'workspace', parent_key: '', view_key: 'workspace', description: '进入公文写作与个人工作区', sort: 10, is_enabled: true },
  { name: '个人中心', menu_key: 'personal_center', parent_key: '', view_key: 'personal_center', description: '查看个人信息、绑定多重验证并管理自己的设备会话', sort: 15, is_enabled: true },
  { name: '模型配置', menu_key: 'model_config', parent_key: '', view_key: 'model_config', description: '配置主模型、OCR 语义模型与本地 AI 引擎', sort: 20, is_enabled: true },
  { menu_key: 'permission_management', name: '权限管理', parent_key: '', view_key: '', description: '系统权限与组织管理的菜单目录', sort: 100, is_enabled: true },
  { name: '组织申请', menu_key: 'applications', parent_key: 'permission_management', view_key: 'applications', description: '浏览公开组织并提交加入申请', sort: 110, is_enabled: true },
  { name: '申请审核', menu_key: 'application_reviews', parent_key: 'permission_management', view_key: 'application_reviews', description: '审核组织加入申请', sort: 120, is_enabled: true },
  { name: '组织管理', menu_key: 'organizations', parent_key: 'permission_management', view_key: 'organizations', description: '查看和维护组织结构', sort: 130, is_enabled: true },
  { name: '角色与权限', menu_key: 'roles', parent_key: 'permission_management', view_key: 'roles', description: '配置角色、菜单和 API 权限', sort: 140, is_enabled: true },
  { name: '成员授权', menu_key: 'members', parent_key: 'permission_management', view_key: 'members', description: '分配组织与成员角色', sort: 150, is_enabled: true },
  { name: '审计日志', menu_key: 'audit', parent_key: 'permission_management', view_key: 'audit', description: '查看组织操作记录', sort: 160, is_enabled: true },
  { name: '菜单配置', menu_key: 'menu_configs', parent_key: 'permission_management', view_key: 'menu_configs', description: '维护前端导航菜单', sort: 170, is_enabled: true },
  { menu_key: 'knowledge_base', name: '知识库', parent_key: '', view_key: '', description: '组织知识的导入、索引与检索目录', sort: 200, is_enabled: true },
  { name: '文档导入与索引', menu_key: 'knowledge_documents', parent_key: 'knowledge_base', view_key: 'knowledge_documents', description: '导入文档、查看切片并重建知识索引', sort: 210, is_enabled: true },
  { name: '混合检索与证据', menu_key: 'knowledge_search', parent_key: 'knowledge_base', view_key: 'knowledge_search', description: '使用向量与词法混合检索回溯知识证据', sort: 220, is_enabled: true },
  { menu_key: 'writing_management', name: '受控写作', parent_key: '', view_key: '', description: '模板、版本和受控生成工作流目录', sort: 300, is_enabled: true },
  { name: '写作模板', menu_key: 'document_templates', parent_key: 'writing_management', view_key: 'document_templates', description: '维护组织级 Markdown 写作模板和约束', sort: 310, is_enabled: true },
  { name: '写作任务', menu_key: 'writing_tasks', parent_key: 'writing_management', view_key: 'writing_tasks', description: '以模板和知识证据创建可回溯写作任务', sort: 320, is_enabled: true },
]

const selectedOrganization = computed(() => organizations.value.find((item) => item.ID === selectedOrganizationID.value) || null)
const effectiveOrganizationID = computed(() => selectedOrganizationID.value || assignedOrganizationID.value || 0)
const visibleMemberships = computed(() => memberships.value.filter((item) => item.organization_id === 0 || item.organization_id === selectedOrganizationID.value))
const canManage = computed(() => menus.value.includes('roles'))
const configuredMenuItems = computed(() => systemMenus.value.length ? systemMenus.value.filter((item) => item.is_enabled) : FRONTEND_MENU_DEFINITIONS)
const navigationGroups = computed(() => configuredMenuItems.value
  .filter((item) => !item.parent_key && !['workspace', 'personal_center', 'model_config'].includes(item.menu_key))
  .map((parent) => ({ ...parent, children: configuredMenuItems.value.filter((item) => item.parent_key === parent.menu_key && menus.value.includes(item.menu_key)) }))
  .filter((group) => menus.value.includes(group.menu_key) || group.children.length > 0))
const hasModelConfigMenu = computed(() => configuredMenuItems.value.some((item) => item.menu_key === 'model_config') && menus.value.includes('model_config'))
const hasPersonalCenterMenu = computed(() => configuredMenuItems.value.some((item) => item.menu_key === 'personal_center') && menus.value.includes('personal_center'))
const activeViewTitle = computed(() => ({ workspace: '公文写作工作台', personal_center: '个人中心', model_config: '模型配置', applications: '组织申请', application_reviews: '申请审核', organizations: '组织管理', roles: '角色与权限', members: '成员授权', audit: '审计日志', menu_configs: '菜单配置', knowledge_documents: '文档导入与索引', knowledge_search: '混合检索与证据', document_templates: '写作模板', writing_tasks: '写作任务' }[activeView.value] || '工作台'))
const activeViewSubtitle = computed(() => ({ workspace: '把组织知识、模板与安全协作放在一个工作台里。', personal_center: '查看个人信息，绑定多重验证并管理已登录设备。', model_config: '管理当前账号在当前租户中使用的模型连接。', knowledge_documents: '导入资料并建立可回溯的知识索引。', knowledge_search: '从组织知识中找到可以引用的证据。', document_templates: '维护可复用、可约束的公文写作模板。', writing_tasks: '创建、运行并审阅版本化的写作任务。' }[activeView.value] || '在当前组织上下文中完成安全协作。'))
const navigationIconMap = { workspace: LayoutDashboard, model_config: Settings2, permission_management: Shield, applications: ClipboardList, application_reviews: ClipboardList, organizations: Building2, roles: Shield, members: Users, audit: ClipboardList, menu_configs: Settings2, knowledge_base: BookOpen, knowledge_documents: FileText, knowledge_search: Search, writing_management: PenLine, document_templates: FileText, writing_tasks: PenLine }

function clearMessage() { notice.value = ''; error.value = '' }
function showError(message) { notice.value = ''; error.value = message || '操作未完成，请稍后重试。' }
function showToast(message, duration = 2800) {
  if (toastTimer) clearTimeout(toastTimer)
  toast.value = message
  toastTimer = setTimeout(() => {
    toast.value = ''
    toastTimer = undefined
  }, duration)
}
function handleModelNotice(payload) {
  if (payload?.type === 'error') {
    showError(payload.text)
    return
  }
  error.value = ''
  showToast(payload?.text || '操作已完成。')
}
function selectOrganization(event) { selectedOrganizationID.value = Number(event.target.value) }
function menuIcon(menuKey) { return navigationIconMap[menuKey] || Sparkles }
function selectView(view, parentKey = '') {
  activeView.value = view
  if (parentKey) expandedMenuGroups.value = { ...expandedMenuGroups.value, [parentKey]: true }
}
function isMenuGroupExpanded(menuKey) { return Boolean(expandedMenuGroups.value[menuKey]) }
function toggleMenuGroup(menuKey) { expandedMenuGroups.value = { ...expandedMenuGroups.value, [menuKey]: !expandedMenuGroups.value[menuKey] } }

function consumeOAuthRedirect() {
  const query = new URLSearchParams(window.location.search)
  const provider = query.get('provider')
  const registered = query.get('oauth_registered') === '1'
  const oauthError = query.get('error')
  if (!provider && !registered && !oauthError) return

  if (oauthError) {
    showError(`第三方登录失败：${oauthError}`)
  } else if (registered) {
    notice.value = `已通过 ${provider || '第三方账号'} 自动注册并登录。`
  } else if (provider) {
    notice.value = `已通过 ${provider} 登录。`
  }
  window.history.replaceState({}, document.title, window.location.pathname)
}

async function loadOAuthProviders() {
  try {
    const result = await fetchOAuthProviders()
    oauthProviders.value = Array.isArray(result?.providers) ? result.providers : []
  } catch {
    oauthProviders.value = []
  }
}

function startOAuth(provider) {
  oauthLoading.value = provider
  window.location.assign(oauthLoginURL(provider))
}

async function refreshCaptcha() {
	captchaLoading.value = true
	try {
		const result = await fetchCaptcha()
		credentials.value.captcha_id = result.captcha_id || ''
		credentials.value.captcha_answer = ''
		captchaImage.value = result.image_data || ''
	} catch (requestError) {
		credentials.value.captcha_id = ''
		captchaImage.value = ''
		showError('图片验证码暂时无法加载，请刷新页面后重试。')
	} finally { captchaLoading.value = false }
}

async function submitAuthentication() {
  clearMessage()
  loading.value = true
  try {
		const registering = authMode.value === 'register'
    const result = await authenticate(authMode.value, {
      username: credentials.value.username.trim(),
      password: credentials.value.password,
      captcha_id: credentials.value.captcha_id,
      captcha_answer: credentials.value.captcha_answer,
    })
		credentials.value.password = ''
		if (result.pending_token) {
			pendingAuthToken.value = result.pending_token
			if (result.auth_stage === 'mfa_enrollment') {
				pendingMFASetup.value = await setupPendingMFA(pendingAuthToken.value)
				authStep.value = 'mfa_enrollment'
			} else {
				authStep.value = 'mfa_verify'
			}
			return
		}
		await finishAuthentication(result, registering ? '账号已创建，等待管理员分配组织和角色。' : '登录成功。')
  } catch (requestError) {
    if (!user.value) setAuthToken('')
    showError(requestError.message)
    await refreshCaptcha()
  } finally { loading.value = false }
}

async function finishAuthentication(result, message) {
	user.value = result.user
	pendingAuthToken.value = ''
	pendingMFASetup.value = null
	pendingMFACode.value = ''
	authStep.value = 'credentials'
	showToast(message)
	await loadTenants()
}

async function submitPendingMFA() {
	clearMessage()
	loading.value = true
	try {
		const result = await completePendingMFA(pendingAuthToken.value, pendingMFACode.value)
		await finishAuthentication(result, authStep.value === 'mfa_enrollment' ? '多重验证已绑定，登录成功。' : '验证成功，登录成功。')
	} catch (requestError) {
		showError(requestError.message)
	} finally { loading.value = false }
}

async function cancelPendingMFA() {
	pendingAuthToken.value = ''
	pendingMFASetup.value = null
	pendingMFACode.value = ''
	authStep.value = 'credentials'
	clearMessage()
	await refreshCaptcha()
}

async function loadTenants(silent = false) {
  try {
    const [currentUser, items] = await Promise.all([fetchCurrentUser(), fetchTenants()])
    user.value = currentUser
    tenants.value = items || []
    if (!tenants.value.some((item) => item.ID === selectedTenantID.value)) selectedTenantID.value = tenants.value[0]?.ID || 0
    if (selectedTenantID.value) {
      let result = await fetchMenus(selectedTenantID.value)
      const configuredMenus = Array.isArray(result.menus) ? result.menus : []
      const ownerMenusNeedSync = result.role_code === 'owner' && result.role_id && OWNER_MENU_KEYS.some((key) => !configuredMenus.includes(key))
      if (ownerMenusNeedSync) {
        await updateRolePermissions(selectedTenantID.value, result.role_id, { menu_keys: OWNER_MENU_KEYS, api_ids: [] })
        result = await fetchMenus(selectedTenantID.value)
      }
      menus.value = Array.isArray(result.menus) && result.menus.length ? result.menus : ['workspace']
      if (result.role_code === 'owner') await syncMenuConfigs(selectedTenantID.value, FRONTEND_MENU_DEFINITIONS)
      systemMenus.value = await fetchMenuConfigs(selectedTenantID.value) || []
      assignedOrganizationID.value = Number(result.organization_id) || 0
      if (usesFrontendInference() && menus.value.includes('model_config')) initLocalAI({ tenantID: selectedTenantID.value })
    }
    await loadTenantResources()
  } catch (requestError) {
    user.value = null; tenants.value = []; selectedTenantID.value = 0; assignedOrganizationID.value = 0; setAuthToken(''); if (!silent) showError(requestError.message)
  }
}

async function loadTenantResources() {
  if (!selectedTenantID.value) {
    organizations.value = []; publicOrganizations.value = []; roles.value = []; memberships.value = []; globalUsers.value = []; membershipApplications.value = []; selectedOrganizationID.value = 0; auditLogs.value = []; apiResources.value = []; systemMenus.value = []
    return
  }
  systemLoading.value = true
  try {
    if (canManage.value) organizations.value = await fetchOrganizations(selectedTenantID.value) || []
    else organizations.value = []
    publicOrganizations.value = await fetchPublicOrganizations(selectedTenantID.value) || []
    // Readers applying to an organization do not need the management-only
    // role directory. Loading it here caused a harmless 403 to blank the
    // whole public organization list.
    if (menus.value.includes('roles') || menus.value.includes('members')) {
      roles.value = await fetchRoles(selectedTenantID.value) || []
    } else roles.value = []
    const requests = []
    // Optional newer capabilities must not blank the whole administration UI
    // while a rolling deployment still has an older backend process.
    requests.push(fetchMembershipApplications(selectedTenantID.value)
      .then((items) => { membershipApplications.value = Array.isArray(items) ? items : [] })
      .catch(() => { membershipApplications.value = [] }))
    if (menus.value.includes('members')) requests.push(fetchMemberships(selectedTenantID.value).then((items) => { memberships.value = Array.isArray(items) ? items : [] }))
    else memberships.value = []
    if (menus.value.includes('audit')) requests.push(fetchAuditLogs(selectedTenantID.value).then((items) => { auditLogs.value = items || [] }))
    else auditLogs.value = []
    if (menus.value.includes('roles')) requests.push(fetchAPIResources(selectedTenantID.value).then((items) => { apiResources.value = items || [] }))
    else apiResources.value = []
    await Promise.all(requests)
    if (menus.value.includes('members')) {
      try {
        const items = await fetchGlobalUsers(selectedTenantID.value)
        globalUsers.value = Array.isArray(items) ? items : []
      } catch { globalUsers.value = [] }
    } else globalUsers.value = []
    if (assignedOrganizationID.value) selectedOrganizationID.value = assignedOrganizationID.value
    else if (canManage.value && !organizations.value.some((item) => item.ID === selectedOrganizationID.value)) selectedOrganizationID.value = organizations.value.find((item) => !item.parent_id)?.ID || organizations.value[0]?.ID || 0
    else if (!canManage.value) selectedOrganizationID.value = 0
  } catch (requestError) { showError(requestError.message) } finally { systemLoading.value = false }
}

async function changeOrganizationVisibility(payload) {
  clearMessage()
  try { await setOrganizationVisibility(selectedTenantID.value, payload.id, payload.visible); notice.value = payload.visible ? '组织已设为公开可申请。' : '组织已隐藏。'; await loadTenantResources() } catch (requestError) { showError(requestError.message) }
}

async function applyFromWorkspace(organizationID) {
  clearMessage()
  try { await applyToOrganization(selectedTenantID.value, organizationID); notice.value = '申请已提交，等待管理员或所有者审核。'; await loadTenantResources() } catch (requestError) { showError(requestError.message) }
}

async function reviewApplication(payload) {
  clearMessage()
  try { await reviewMembershipApplication(selectedTenantID.value, payload.id, { approve: payload.approve }); notice.value = payload.approve ? '申请已批准，成员已加入组织，角色未变。' : '申请已拒绝。'; await loadTenantResources() } catch (requestError) { showError(requestError.message) }
}

async function createOrganizationFromPage(payload) {
  clearMessage()
  try {
    await createOrganization(selectedTenantID.value, { name: payload.name.trim(), code: payload.code.trim(), parent_id: payload.parent_id })
    notice.value = '组织已创建。'
    await loadTenantResources()
  } catch (requestError) { showError(requestError.message) }
}

async function saveMembershipFromPage(payload) {
  clearMessage()
  try {
    await addMembership(selectedTenantID.value, { username: payload.username.trim(), user_id: payload.user_id, role_id: payload.role_id, organization_id: payload.organization_id, ...(payload.mfa_enrollment_required === undefined ? {} : { mfa_enrollment_required: payload.mfa_enrollment_required }) })
    notice.value = payload.role_id ? '成员权限已保存。' : '成员已添加，请点击成员设置权限。'
    await loadTenantResources()
  } catch (requestError) { showError(requestError.message) }
}

async function createRoleFromPage(payload) {
  clearMessage()
  try {
    await createRole(selectedTenantID.value, { name: payload.name.trim(), code: payload.code.trim(), description: payload.description.trim(), menu_keys: payload.menu_keys, api_ids: payload.api_ids })
    notice.value = '自定义角色已创建。请继续配置菜单与 API 权限。'
    await loadTenantResources()
  } catch (requestError) { showError(requestError.message) }
}

async function updateRoleFromPage(payload) {
  clearMessage()
  try {
    await updateRolePermissions(selectedTenantID.value, payload.roleID, { menu_keys: payload.menu_keys, api_ids: payload.api_ids })
    notice.value = '角色权限已更新。'
    await loadTenantResources()
  } catch (requestError) { showError(requestError.message) }
}

async function createMenuFromPage(payload) {
  clearMessage()
  try { await createMenuConfig(selectedTenantID.value, payload); notice.value = '前端菜单已创建。'; systemMenus.value = await fetchMenuConfigs(selectedTenantID.value) || [] } catch (requestError) { showError(requestError.message) }
}

async function updateMenuFromPage(payload) {
  clearMessage()
  try { const { id, ...menu } = payload; await updateMenuConfig(selectedTenantID.value, id, menu); notice.value = '前端菜单已更新。'; systemMenus.value = await fetchMenuConfigs(selectedTenantID.value) || [] } catch (requestError) { showError(requestError.message) }
}

async function signOut() {
	await logout()
  user.value = null; tenants.value = []; organizations.value = []; publicOrganizations.value = []; roles.value = []; memberships.value = []; auditLogs.value = []; apiResources.value = []; systemMenus.value = []; menus.value = ['workspace']; selectedTenantID.value = 0; selectedOrganizationID.value = 0; assignedOrganizationID.value = 0; activeView.value = 'workspace'; notice.value = '已退出当前会话。'
}

onMounted(async () => {
  consumeOAuthRedirect()
  await Promise.all([refreshCaptcha(), loadOAuthProviders()])
  if (getAuthToken()) {
    claimAuthSessionForTab()
    await loadTenants()
  } else if (usesCookieSession()) {
    await loadTenants(true)
  }
})
onBeforeUnmount(() => { if (toastTimer) clearTimeout(toastTimer) })
</script>

<template>
  <Teleport to="body"><Transition name="toast"><div v-if="toast" class="app-toast" role="status" aria-live="polite"><span aria-hidden="true">✓</span>{{ toast }}</div></Transition></Teleport>
  <main v-if="!user" class="auth-shell">
    <section class="auth-showcase">
      <div class="showcase-brand"><span class="brand-mark"><img src="/inkflow-icon.png" alt="" /></span><div><strong>InkFlow</strong><small>公文写作</small></div></div>
      <div class="showcase-copy"><p class="showcase-eyebrow">LOCAL-FIRST DOCUMENT STUDIO</p><h2>以组织为工作上下文，按角色安全协作。</h2><p>把知识、模板与写作任务放在同一个清晰的工作空间里，让每一次生成都有来源、可追溯。</p></div>
      <div class="showcase-art" aria-hidden="true"><span class="art-orbit orbit-one"></span><span class="art-orbit orbit-two"></span><div class="art-sheet"><FileText :size="34" /><span></span><span></span><span></span></div><div class="art-shield"><Shield :size="25" /></div></div>
      <div class="showcase-footer"><span><Sparkles :size="15" />本地优先</span><span><Shield :size="15" />权限可控</span><span><FileText :size="15" />证据回溯</span></div>
    </section>
    <section class="auth-card" aria-labelledby="auth-title">
      <div class="auth-card-top"><span class="auth-kicker">WELCOME BACK</span><span class="auth-status"><i></i>安全连接</span></div>
      <template v-if="authStep === 'credentials'">
        <h1 id="auth-title">进入你的工作台</h1><p class="intro">登录后，所有内容都会围绕当前组织与角色展开。</p>
        <div class="auth-tabs" role="tablist"><button :class="{ active: authMode === 'login' }" type="button" @click="authMode = 'login'">登录</button><button :class="{ active: authMode === 'register' }" type="button" @click="authMode = 'register'">注册新账号</button></div>
        <form class="form-stack" @submit.prevent="submitAuthentication"><label><span>用户名</span><div class="input-shell"><UserRound :size="17" /><input v-model="credentials.username" autocomplete="username" minlength="3" maxlength="64" required placeholder="输入用户名" /></div></label><label><span>密码</span><div class="input-shell"><LockKeyhole :size="17" /><input v-model="credentials.password" type="password" :autocomplete="authMode === 'login' ? 'current-password' : 'new-password'" minlength="8" required placeholder="至少 8 个字符" /></div></label><label><span>图片验证码</span><div class="captcha-field"><div class="input-shell"><LockKeyhole :size="17" /><input v-model="credentials.captcha_answer" autocomplete="off" autocapitalize="characters" maxlength="6" required placeholder="输入图中字符" /></div><button class="captcha-image" type="button" :disabled="captchaLoading" title="换一张验证码" @click="refreshCaptcha"><img v-if="captchaImage" :src="captchaImage" alt="图片验证码" /><span v-else>{{ captchaLoading ? '加载中…' : '重新加载' }}</span></button></div></label><button class="primary auth-submit" type="submit" :disabled="loading || captchaLoading">{{ loading ? '处理中…' : authMode === 'login' ? '登录工作台' : '注册只读账号' }}<ChevronRight :size="17" /></button></form>
        <div v-if="usesCookieSession() && oauthProviders.length" class="oauth-section">
          <div class="oauth-divider"><span>或使用第三方登录</span></div>
          <div class="oauth-buttons">
            <button v-for="provider in oauthProviders" :key="provider.name" class="oauth-button" type="button" :disabled="!!oauthLoading" @click="startOAuth(provider.name)">
              <Fingerprint :size="17" /><span>{{ oauthLoading === provider.name ? '正在跳转…' : (provider.display_name || provider.name) }}</span>
            </button>
          </div>
          <p class="oauth-note">首次第三方登录会自动创建系统账号；组织和角色仍由管理员分配。</p>
        </div>
      </template>
      <template v-else-if="authStep === 'mfa_verify'">
        <h1 id="auth-title">验证你的身份</h1><p class="intro">账号密码已验证。请打开身份验证器，输入当前 6 位动态验证码后继续。</p>
        <form class="form-stack" @submit.prevent="submitPendingMFA"><label><span>动态验证码</span><div class="input-shell"><Shield :size="17" /><input v-model="pendingMFACode" inputmode="numeric" autocomplete="one-time-code" maxlength="6" required autofocus placeholder="输入 6 位动态验证码" /></div></label><button class="primary auth-submit" type="submit" :disabled="loading">{{ loading ? '验证中…' : '验证并进入工作台' }}<ChevronRight :size="17" /></button><button class="auth-cancel" type="button" :disabled="loading" @click="cancelPendingMFA">返回登录</button></form>
      </template>
      <template v-else>
        <h1 id="auth-title">绑定多重验证</h1><p class="intro">账号密码已验证。请先将以下密钥添加到身份验证器，再输入应用生成的 6 位验证码。</p>
        <form class="form-stack" @submit.prevent="submitPendingMFA"><label><span>设置密钥</span><div class="mfa-secret">{{ pendingMFASetup?.secret || '正在准备密钥…' }}</div></label><details class="mfa-uri"><summary>身份验证器支持链接导入时，查看配置链接</summary><code>{{ pendingMFASetup?.otpauth_url }}</code></details><label><span>动态验证码</span><div class="input-shell"><Shield :size="17" /><input v-model="pendingMFACode" inputmode="numeric" autocomplete="one-time-code" maxlength="6" required autofocus placeholder="输入 6 位动态验证码" /></div></label><button class="primary auth-submit" type="submit" :disabled="loading || !pendingMFASetup">{{ loading ? '绑定中…' : '确认绑定并进入工作台' }}<ChevronRight :size="17" /></button><button class="auth-cancel" type="button" :disabled="loading" @click="cancelPendingMFA">返回登录</button></form>
      </template>
      <p v-if="error" class="message error" role="alert">{{ error }}</p><p v-else-if="notice" class="message success">{{ notice }}</p>
      <p class="auth-note">账号注册后默认为只读成员，组织和角色由管理员分配。登录受图片验证码、失败锁定和安全 Cookie 保护。</p>
    </section>
  </main>

  <main v-else class="app-shell">
    <aside class="sidebar">
      <div class="sidebar-glow glow-one"></div><div class="sidebar-glow glow-two"></div>
      <div class="sidebar-head"><div class="brand-line"><span class="brand-mark"><img src="/inkflow-icon.png" alt="InkFlow" /></span><div><strong>InkFlow</strong><small>公文写作</small></div></div><span class="sidebar-mode">WORKSPACE</span></div>
      <nav class="side-nav">
        <span class="nav-caption">工作区</span>
        <button :class="['nav-item', { active: activeView === 'workspace' }]" type="button" @click="selectView('workspace')"><span class="nav-icon"><LayoutDashboard :size="17" /></span><span>工作台</span><ChevronRight class="nav-item-arrow" :size="15" /></button>
        <button v-if="hasPersonalCenterMenu" :class="['nav-item', { active: activeView === 'personal_center' }]" type="button" @click="selectView('personal_center')"><span class="nav-icon"><UserRound :size="17" /></span><span>个人中心</span><ChevronRight class="nav-item-arrow" :size="15" /></button>
        <button v-if="hasModelConfigMenu" :class="['nav-item', { active: activeView === 'model_config' }]" type="button" @click="selectView('model_config')"><span class="nav-icon"><Settings2 :size="17" /></span><span>模型配置</span><ChevronRight class="nav-item-arrow" :size="15" /></button>
        <span class="nav-caption nav-caption-spaced">管理与协作</span>
        <section v-for="group in navigationGroups" :key="group.ID || group.menu_key" class="nav-group">
          <button :class="['nav-item', 'nav-group-toggle', { active: group.children.some((item) => item.view_key === activeView), open: isMenuGroupExpanded(group.menu_key) }]" type="button" :aria-expanded="isMenuGroupExpanded(group.menu_key)" @click="toggleMenuGroup(group.menu_key)"><span class="nav-icon"><component :is="menuIcon(group.menu_key)" :size="17" /></span><span>{{ group.name }}</span><ChevronDown class="nav-item-arrow nav-group-arrow" :size="15" /></button>
          <div v-show="isMenuGroupExpanded(group.menu_key)" class="nav-children"><button v-for="item in group.children" :key="item.ID || item.menu_key" :class="['nav-child', { active: activeView === item.view_key }]" type="button" @click="selectView(item.view_key, group.menu_key)"><span class="child-dot"></span>{{ item.name }}</button></div>
        </section>
      </nav>
      <div class="sidebar-bottom"><div class="org-mini"><span class="org-mini-icon"><Building2 :size="16" /></span><div><small>当前组织</small><strong>{{ selectedOrganization?.name || '未分配组织' }}</strong></div></div><div class="user-card"><span class="avatar">{{ user.username.slice(0, 1).toUpperCase() }}</span><div><strong>{{ user.username }}</strong><button type="button" @click="signOut"><LogOut :size="13" />退出登录</button></div></div></div>
    </aside>
    <section class="content">
      <header class="topbar">
        <div class="topbar-copy"><div class="breadcrumb"><span>INKFLOW</span><ChevronRight :size="13" /><span>{{ activeView === 'workspace' ? 'WORKSPACE' : activeViewTitle.toUpperCase() }}</span></div><h1>{{ activeViewTitle }}</h1><p>{{ activeViewSubtitle }}</p></div>
        <div class="topbar-actions"><label class="tenant-select"><span>当前组织</span><div class="select-shell"><select :value="effectiveOrganizationID" @change="selectOrganization" :disabled="!canManage || (!organizations.length && !effectiveOrganizationID) || systemLoading">
            <option :value="0">未分配组织</option>
            <option v-if="effectiveOrganizationID && !organizations.some((item) => item.ID === effectiveOrganizationID)" :value="effectiveOrganizationID">当前已分配组织</option>
            <option v-for="organization in organizations" :key="organization.ID" :value="organization.ID">{{ organization.parent_id ? '↳ ' : '' }}{{ organization.name }}</option>
          </select><ChevronDown :size="15" /></div></label><button class="notification-button" type="button" aria-label="通知"><Bell :size="18" /><i></i></button></div>
      </header>
      <div v-if="error" class="message error">{{ error }}</div><div v-else-if="notice" class="message success">{{ notice }}</div>
      <WorkspacePage v-if="activeView === 'workspace'" :organization="selectedOrganization" :organizations="publicOrganizations" :applications="membershipApplications" :organization-count="publicOrganizations.length" :member-count="visibleMemberships.length" :role-count="roles.length" :can-manage="canManage" @navigate="selectView($event)" @apply="applyFromWorkspace" />
      <ModelConfigPage v-else-if="activeView === 'model_config'" :tenant-id="selectedTenantID" @notice="handleModelNotice" />
      <AccountSecurityPage v-else-if="activeView === 'personal_center'" :user="user" :tenant-id="selectedTenantID" @notice="handleModelNotice" @updated="user = $event" @logout="signOut" />
      <OrganizationApplicationPage v-else-if="activeView === 'applications'" :organizations="publicOrganizations" :applications="membershipApplications" :loading="systemLoading" @apply="applyFromWorkspace" @refresh="loadTenantResources" />
      <ApplicationReviewPage v-else-if="activeView === 'application_reviews'" :applications="membershipApplications" :loading="systemLoading" @review="reviewApplication" @refresh="loadTenantResources" />
      <KnowledgeDocumentPage v-else-if="activeView === 'knowledge_documents'" :tenant-id="selectedTenantID" :organization-id="effectiveOrganizationID" @notice="handleModelNotice" />
      <KnowledgeSearchPage v-else-if="activeView === 'knowledge_search'" :tenant-id="selectedTenantID" :organization-id="effectiveOrganizationID" @notice="handleModelNotice" />
      <DocumentTemplatePage v-else-if="activeView === 'document_templates'" :tenant-id="selectedTenantID" :organization-id="effectiveOrganizationID" @notice="handleModelNotice" />
      <WritingTaskPage v-else-if="activeView === 'writing_tasks'" :tenant-id="selectedTenantID" :organization-id="effectiveOrganizationID" @notice="handleModelNotice" />
      <section v-else-if="!selectedOrganization" class="empty"><h2>尚未选择组织</h2><p>请先在当前工作空间创建一个组织。</p></section>
      <OrganizationPage v-else-if="activeView === 'organizations'" :organizations="organizations" :selected-organization-i-d="selectedOrganizationID" @create="createOrganizationFromPage" @select="selectedOrganizationID = $event" @visibility="changeOrganizationVisibility" @refresh="loadTenantResources" />
      <RolePermissionPage v-else-if="activeView === 'roles'" :roles="roles" :menus="systemMenus.length ? systemMenus : FRONTEND_MENU_DEFINITIONS" :api-resources="apiResources" @create="createRoleFromPage" @update-permissions="updateRoleFromPage" @refresh="loadTenantResources" />
      <MenuConfigPage v-else-if="activeView === 'menu_configs'" :menus="systemMenus" @create="createMenuFromPage" @update="updateMenuFromPage" @refresh="loadTenants" />
      <MembershipPage v-else-if="activeView === 'members'" :organizations="organizations" :roles="roles" :members="memberships" :users="globalUsers" :selected-organization="selectedOrganization" :selected-organization-i-d="selectedOrganizationID" @save="saveMembershipFromPage" @refresh="loadTenantResources" />
      <AuditLogPage v-else-if="activeView === 'audit'" :logs="auditLogs" @refresh="loadTenantResources" />
    </section>
  </main>
</template>

<style scoped>
.auth-shell,.app-shell{min-height:100vh;color:#1a2924;background:#f4f7f3}.auth-shell{display:grid;place-items:center;padding:24px;background:radial-gradient(circle at top left,#e4eee5,transparent 46%),#f5f7f4}.auth-card{width:min(480px,100%);padding:36px;border:1px solid #d9e2db;border-radius:18px;background:#fff;box-shadow:0 24px 70px rgba(29,53,42,.12)}.brand-line,.user-card{display:flex;align-items:center}.brand-line{gap:14px}.seal{display:grid;width:44px;height:44px;place-items:center;border-radius:12px;color:#fff;background:#1d6654;font-size:22px;font-weight:700}.brand-line p,.topbar p{margin:0;color:#5c796c;font-size:11px;font-weight:800;letter-spacing:.12em}.brand-line h1{margin:4px 0 0;font-size:24px}.intro{margin:24px 0;color:#5f7068;line-height:1.7}.auth-tabs{display:grid;grid-template-columns:1fr 1fr;margin-bottom:20px;padding:4px;border-radius:10px;background:#eef3ef}.auth-tabs button{padding:10px;border:0;border-radius:7px;color:#617168;background:transparent}.auth-tabs button.active{color:#174d3f;background:#fff;box-shadow:0 1px 4px rgba(25,50,39,.12);font-weight:700}.form-stack{display:grid;gap:16px}.form-stack label,.tenant-select{display:grid;gap:7px;color:#4d6258;font-size:13px;font-weight:650}.form-stack input,.tenant-select select{width:100%;min-height:42px;padding:0 12px;border:1px solid #cfdcd3;border-radius:8px;color:#192820;background:#fff}.primary{min-height:42px;padding:0 16px;border:0;border-radius:8px;color:#fff;background:#1e6956;font-weight:700}.message{margin:16px 0;padding:11px 13px;border-radius:8px;font-size:14px}.error{color:#8c3028;background:#fff0ee}.success{color:#205b38;background:#e6f4e9}.app-toast{position:fixed;z-index:1000;top:24px;left:50%;display:flex;align-items:center;gap:9px;max-width:min(420px,calc(100vw - 32px));padding:12px 16px;border:1px solid #bbdfc5;border-radius:12px;color:#195138;background:#f0fbf3;box-shadow:0 14px 34px rgba(24,61,42,.18);font-size:14px;font-weight:700;transform:translateX(-50%)}.app-toast>span{display:grid;width:20px;height:20px;place-items:center;border-radius:50%;color:#fff;background:#2d865d;font-size:13px}.toast-enter-active,.toast-leave-active{transition:opacity .2s ease,transform .2s ease}.toast-enter-from,.toast-leave-to{opacity:0;transform:translate(-50%,-12px)}.app-shell{display:grid;grid-template-columns:244px minmax(0,1fr)}.sidebar{display:flex;position:sticky;top:0;height:100vh;flex-direction:column;gap:32px;padding:24px 16px;color:#e6efe9;background:#14241f}.sidebar .seal{width:38px;height:38px;border-radius:9px;font-size:18px}.sidebar strong,.sidebar small{display:block}.sidebar small{margin-top:2px;color:#91aa9f;font-size:12px}nav{display:grid;gap:5px}nav button{padding:11px 12px;border:1px solid transparent;border-radius:8px;color:#b8c9c0;background:transparent;text-align:left}nav button:hover,nav button.active{border-color:rgba(255,255,255,.08);color:#fff;background:rgba(102,153,133,.22)}.nav-group{display:grid;gap:4px}.nav-group-toggle{display:flex;align-items:center;justify-content:space-between;font-weight:700}.nav-chevron{font-size:18px;line-height:1}.nav-children{display:grid;gap:3px;margin-left:12px;padding-left:9px;border-left:1px solid rgba(160,198,181,.25)}.nav-children button{padding:9px 10px;font-size:14px}.user-card{gap:10px;margin-top:auto;padding:12px 8px 0;border-top:1px solid rgba(255,255,255,.1)}.user-card>span{display:grid;width:32px;height:32px;place-items:center;border-radius:50%;background:#2b6f5c;font-weight:700}.user-card button{display:block;margin-top:3px;padding:0;border:0;color:#9eb6ab;background:none;font-size:12px}.content{min-width:0;padding:34px clamp(20px,4vw,64px)}.topbar{display:flex;gap:24px;align-items:end;justify-content:space-between;margin-bottom:26px}.topbar h1{margin:6px 0 0;font-size:clamp(26px,3vw,36px)}.tenant-select{min-width:min(280px,100%)}.empty{padding:48px;border:1px solid #dce5df;border-radius:14px;background:#fff;text-align:center}.empty p{color:#7a8981}@media(max-width:900px){.app-shell{grid-template-columns:1fr}.sidebar{position:static;height:auto;gap:16px}nav{display:block;overflow-x:auto}nav>button,.nav-group{margin-bottom:5px}nav button{text-align:left;white-space:nowrap}.user-card{display:none}}@media(max-width:600px){.auth-card{padding:26px 20px}.content{padding:24px 16px}.topbar{align-items:stretch;flex-direction:column}.tenant-select{min-width:0}.app-toast{top:16px}}
</style>

<style scoped>
:global(body) {
  background: #f4f6f2;
}

.auth-shell {
  position: relative;
  grid-template-columns: minmax(0, 1.15fr) minmax(390px, 0.78fr);
  place-items: stretch;
  gap: clamp(24px, 5vw, 76px);
  padding: clamp(18px, 4vw, 64px);
  overflow: hidden;
  background:
    radial-gradient(circle at 10% 12%, rgba(214, 233, 222, 0.92), transparent 32%),
    #f4f6f2;
}

.auth-showcase {
  position: relative;
  display: flex;
  min-height: min(760px, calc(100vh - 36px));
  flex-direction: column;
  overflow: hidden;
  padding: clamp(28px, 5vw, 68px);
  border-radius: 28px;
  color: #f2f8f4;
  background: linear-gradient(145deg, #0b3d31 0%, #125844 56%, #0d4638 100%);
  box-shadow: 0 24px 70px rgba(25, 64, 48, 0.18);
}

.auth-showcase::before,
.auth-showcase::after {
  position: absolute;
  border: 1px solid rgba(201, 237, 218, 0.14);
  border-radius: 50%;
  content: '';
}

.auth-showcase::before {
  right: -12%;
  bottom: -28%;
  width: 72%;
  aspect-ratio: 1;
}

.auth-showcase::after {
  right: 10%;
  bottom: -13%;
  width: 42%;
  aspect-ratio: 1;
}

.showcase-brand,
.showcase-footer,
.auth-card-top,
.auth-status,
.input-shell,
.auth-submit,
.topbar-actions,
.select-shell,
.notification-button,
.org-mini,
.org-mini-icon,
.user-card button {
  display: flex;
  align-items: center;
}

.showcase-brand {
  z-index: 1;
  gap: 12px;
}

.showcase-brand strong,
.showcase-brand small {
  display: block;
}

.showcase-brand strong {
  font-size: 23px;
  letter-spacing: -0.02em;
}

.showcase-brand small {
  margin-top: 3px;
  color: #a6cbb9;
  font-size: 12px;
}

.brand-mark {
  display: grid;
  width: 44px;
  height: 44px;
  flex: 0 0 auto;
  place-items: center;
  overflow: hidden;
  border: 1px solid rgba(255, 255, 255, 0.2);
  border-radius: 13px;
  background: #0a3026;
  box-shadow: 0 7px 16px rgba(0, 0, 0, 0.16);
}

.brand-mark img {
  display: block;
  width: 100%;
  height: 100%;
  object-fit: cover;
}

.showcase-copy {
  z-index: 1;
  max-width: 640px;
  margin-top: clamp(82px, 14vh, 170px);
}

.showcase-eyebrow,
.auth-kicker,
.sidebar-mode,
.nav-caption,
.breadcrumb {
  font-size: 10px;
  font-weight: 800;
  letter-spacing: 0.16em;
}

.showcase-eyebrow {
  margin: 0 0 18px;
  color: #a9d8bf;
}

.showcase-copy h2 {
  max-width: 13ch;
  margin: 0;
  font-size: clamp(32px, 4.6vw, 62px);
  line-height: 1.08;
  letter-spacing: -0.045em;
}

.showcase-copy p:last-child {
  max-width: 48ch;
  margin: 24px 0 0;
  color: #c3ded0;
  font-size: 15px;
  line-height: 1.85;
}

.showcase-art {
  position: absolute;
  right: 7%;
  bottom: 19%;
  width: min(260px, 35vw);
  aspect-ratio: 1.16;
}

.art-orbit {
  position: absolute;
  display: block;
  border: 1px solid rgba(192, 234, 211, 0.2);
  border-radius: 50%;
  transform: rotate(-18deg);
}

.orbit-one {
  inset: 7% -4% -7% 4%;
}

.orbit-two {
  inset: 20% 9% -20% 14%;
  border-color: rgba(192, 234, 211, 0.12);
  transform: rotate(32deg);
}

.art-sheet {
  position: absolute;
  top: 20%;
  left: 23%;
  display: grid;
  width: 40%;
  height: 54%;
  align-content: start;
  gap: 12px;
  padding: 22px 18px;
  border: 1px solid rgba(225, 249, 235, 0.45);
  border-radius: 12px;
  color: #d4f1df;
  background: linear-gradient(145deg, rgba(180, 226, 202, 0.28), rgba(65, 142, 111, 0.16));
  box-shadow: 18px 24px 28px rgba(2, 29, 22, 0.18);
  transform: rotate(-8deg);
}

.art-sheet span {
  display: block;
  width: 82%;
  height: 5px;
  border-radius: 10px;
  background: rgba(225, 249, 235, 0.32);
}

.art-sheet span:nth-last-child(1) { width: 56%; }
.art-sheet span:nth-last-child(2) { width: 90%; }

.art-shield {
  position: absolute;
  right: 12%;
  bottom: 19%;
  display: grid;
  width: 56px;
  height: 56px;
  place-items: center;
  border: 1px solid rgba(225, 249, 235, 0.4);
  border-radius: 16px;
  color: #0c503d;
  background: #c8ead5;
  box-shadow: 0 14px 28px rgba(4, 36, 27, 0.22);
  transform: rotate(8deg);
}

.showcase-footer {
  z-index: 1;
  gap: 20px;
  margin-top: auto;
  color: #abd1bc;
  font-size: 12px;
}

.showcase-footer span {
  display: inline-flex;
  align-items: center;
  gap: 7px;
}

.auth-card {
  align-self: center;
  width: min(480px, 100%);
  padding: clamp(28px, 4vw, 46px);
  border: 1px solid #e0e7e1;
  border-radius: 24px;
  box-shadow: 0 22px 70px rgba(37, 60, 48, 0.1);
}

.auth-card-top {
  justify-content: space-between;
  gap: 14px;
}

.auth-kicker {
  color: #4e7867;
}

.auth-status {
  gap: 6px;
  color: #5f8170;
  font-size: 11px;
}

.auth-status i,
.org-status-dot {
  display: block;
  width: 7px;
  height: 7px;
  border-radius: 50%;
  background: #49a476;
  box-shadow: 0 0 0 4px #e8f5eb;
}

.auth-card h1 {
  margin: 36px 0 9px;
  color: #17392e;
  font-size: clamp(28px, 4vw, 38px);
  letter-spacing: -0.04em;
}

.auth-card .intro {
  margin: 0 0 26px;
  color: #6a7d73;
  font-size: 14px;
  line-height: 1.7;
}

.auth-cancel {
  min-height: 42px;
  border: 1px solid #d5e1da;
  border-radius: 10px;
  color: #356354;
  background: #fff;
  font-weight: 700;
}

.mfa-secret {
  overflow-wrap: anywhere;
  padding: 14px;
  border: 1px dashed #9fc4ae;
  border-radius: 10px;
  color: #174d3f;
  background: #f2faf4;
  font-family: ui-monospace, SFMono-Regular, Menlo, monospace;
  font-size: 13px;
  letter-spacing: .06em;
}

.mfa-uri {
  color: #527366;
  font-size: 12px;
  line-height: 1.6;
}

.mfa-uri summary { cursor: pointer; }
.mfa-uri code { display: block; overflow-wrap: anywhere; margin-top: 8px; color: #335b4d; }

.auth-tabs {
  margin-bottom: 24px;
  border: 1px solid #e5ebe6;
  background: #f4f7f4;
}

.auth-tabs button {
  color: #72837a;
  font-size: 13px;
}

.auth-tabs button.active {
  color: #1b624f;
  box-shadow: 0 2px 8px rgba(27, 73, 56, 0.1);
}

.form-stack {
  gap: 17px;
}

.form-stack label > span {
  font-size: 12px;
  font-weight: 750;
}

.input-shell {
  gap: 9px;
  min-height: 46px;
  padding: 0 13px;
  border: 1px solid #d6e1da;
  border-radius: 10px;
  color: #7b968a;
  background: #fbfdfb;
  transition: border-color 0.18s ease, box-shadow 0.18s ease, background 0.18s ease;
}

.input-shell:focus-within {
  border-color: #5c9a7f;
  background: #fff;
  box-shadow: 0 0 0 4px rgba(58, 133, 100, 0.11);
}

.input-shell input {
	min-height: 44px;
  padding: 0;
  border: 0;
  outline: 0;
	background: transparent;
}

.form-stack label > span em {
	margin-left: 6px;
	color: #89978f;
	font-size: 10px;
	font-style: normal;
	font-weight: 500;
}

.captcha-field {
	display: grid;
	grid-template-columns: minmax(0, 1fr) 132px;
	gap: 10px;
}

.captcha-image {
	display: grid;
	min-height: 46px;
	place-items: center;
	overflow: hidden;
	border: 1px solid #d6e1da;
	border-radius: 10px;
	color: #527866;
	background: #f7fbf8;
	font-size: 12px;
	cursor: pointer;
}

.captcha-image:hover:not(:disabled) {
	border-color: #8cb8a0;
	background: #eef7f0;
}

.captcha-image:disabled { cursor: wait; opacity: .7; }
.captcha-image img {
	display: block;
	width: 100%;
	height: 54px;
	object-fit: contain;
}

.auth-submit {
  justify-content: center;
  gap: 8px;
  min-height: 48px;
  margin-top: 5px;
  border-radius: 10px;
  background: #0f5b46;
  box-shadow: 0 10px 20px rgba(15, 91, 70, 0.17);
}

.auth-submit:hover,
.primary:hover {
  background: #0b4d3b;
}

.oauth-section {
  margin-top: 22px;
  padding-top: 20px;
  border-top: 1px solid #e5ebe6;
}

.oauth-divider {
  display: grid;
  grid-template-columns: 1fr auto 1fr;
  align-items: center;
  gap: 12px;
  color: #87958e;
  font-size: 11px;
  font-weight: 700;
  letter-spacing: .08em;
}

.oauth-divider::before,
.oauth-divider::after {
  content: "";
  height: 1px;
  background: #e5ebe6;
}

.oauth-buttons {
  display: grid;
  gap: 10px;
  margin-top: 16px;
}

.oauth-button {
  display: flex;
  align-items: center;
  justify-content: center;
  gap: 9px;
  min-height: 46px;
  padding: 0 14px;
  border: 1px solid #d6e1da;
  border-radius: 10px;
  color: #224b3d;
  background: #fbfdfb;
  font: inherit;
  font-weight: 700;
  cursor: pointer;
  transition: border-color .18s ease, background .18s ease, transform .12s ease;
}

.oauth-button:hover:not(:disabled) {
  border-color: #8cb8a0;
  background: #eef7f0;
  transform: translateY(-1px);
}

.oauth-button:disabled {
  cursor: wait;
  opacity: .65;
}

.oauth-note {
  margin: 12px 0 0;
  color: #87958e;
  font-size: 11px;
  line-height: 1.6;
  text-align: center;
}

.auth-note {
  margin: 23px 0 0;
  color: #87958e;
  font-size: 11px;
  line-height: 1.6;
  text-align: center;
}

.app-shell {
  grid-template-columns: 248px minmax(0, 1fr);
  background: #f4f6f2;
}

.sidebar {
  position: sticky;
  isolation: isolate;
  gap: 0;
  padding: 25px 14px 17px;
  border-right: 1px solid rgba(124, 175, 150, 0.12);
  background: linear-gradient(180deg, #0e3d32 0%, #0a3028 100%);
  box-shadow: 12px 0 34px rgba(18, 57, 43, 0.06);
}

.sidebar-glow {
  position: absolute;
  z-index: -1;
  width: 180px;
  height: 180px;
  border-radius: 50%;
  background: rgba(77, 157, 119, 0.15);
  filter: blur(4px);
}

.glow-one { top: -100px; right: -80px; }
.glow-two { bottom: 80px; left: -135px; opacity: 0.65; }

.sidebar-head {
  display: grid;
  gap: 22px;
  padding: 0 8px 25px;
  border-bottom: 1px solid rgba(186, 227, 204, 0.12);
}

.sidebar .brand-line {
  gap: 11px;
}

.sidebar .brand-mark {
  width: 40px;
  height: 40px;
  border-radius: 11px;
}

.sidebar strong,
.sidebar small {
  display: block;
}

.sidebar .brand-line strong {
  color: #f3faf5;
  font-size: 17px;
  letter-spacing: -0.02em;
}

.sidebar small {
  margin-top: 3px;
  color: #93b7a6;
  font-size: 11px;
}

.sidebar-mode {
  color: #71ad91;
}

.side-nav {
  display: grid;
  align-content: start;
  gap: 4px;
  padding-top: 24px;
}

.nav-caption {
  padding: 0 12px 7px;
  color: #5d9279;
}

.nav-caption-spaced {
  margin-top: 21px;
}

.nav-item {
  position: relative;
  display: flex;
  width: 100%;
  min-height: 42px;
  align-items: center;
  gap: 10px;
  padding: 0 12px;
  border: 1px solid transparent;
  border-radius: 10px;
  color: #b9d3c4;
  background: transparent;
  font-size: 13px;
  font-weight: 650;
  text-align: left;
  transition: color 0.18s ease, background 0.18s ease, border-color 0.18s ease, transform 0.18s ease;
}

.nav-item:hover {
  color: #eff8f1;
  background: rgba(118, 187, 150, 0.12);
  transform: translateX(2px);
}

.nav-item.active {
  border-color: rgba(187, 234, 207, 0.14);
  color: #f5fff7;
  background: linear-gradient(90deg, rgba(98, 177, 135, 0.27), rgba(98, 177, 135, 0.1));
  box-shadow: inset 3px 0 0 #78c29a;
}

.nav-icon {
  display: grid;
  width: 20px;
  height: 20px;
  flex: 0 0 auto;
  place-items: center;
  color: #8fbea6;
}

.nav-item.active .nav-icon,
.nav-item:hover .nav-icon {
  color: #b8e5c8;
}

.nav-item-arrow {
  margin-left: auto;
  color: #6f9f88;
  transition: transform 0.18s ease;
}

.nav-group-arrow {
  transform: rotate(-90deg);
}

.nav-group-toggle.open .nav-group-arrow {
  transform: rotate(0deg);
}

.nav-children {
  display: grid;
  gap: 3px;
  margin: 3px 0 3px 21px;
  padding: 4px 0 4px 17px;
  border-left: 1px solid rgba(143, 190, 166, 0.24);
}

.nav-child {
  display: flex;
  min-height: 34px;
  align-items: center;
  gap: 9px;
  padding: 0 9px;
  border: 0;
  border-radius: 7px;
  color: #99bdaa;
  background: transparent;
  font-size: 12px;
  text-align: left;
}

.nav-child:hover,
.nav-child.active {
  color: #f3fff6;
  background: rgba(118, 187, 150, 0.16);
}

.child-dot {
  width: 5px;
  height: 5px;
  border: 1px solid #77aa90;
  border-radius: 50%;
}

.nav-child.active .child-dot {
  border-color: #aee1be;
  background: #aee1be;
  box-shadow: 0 0 0 3px rgba(174, 225, 190, 0.12);
}

.sidebar-bottom {
  display: grid;
  gap: 13px;
  margin-top: auto;
}

.org-mini {
  gap: 9px;
  padding: 11px 10px;
  border: 1px solid rgba(185, 226, 203, 0.13);
  border-radius: 10px;
  background: rgba(119, 185, 148, 0.09);
}

.org-mini-icon {
  display: grid;
  width: 28px;
  height: 28px;
  flex: 0 0 auto;
  place-items: center;
  border-radius: 8px;
  color: #a9d7b8;
  background: rgba(169, 215, 184, 0.12);
}

.org-mini small {
  margin: 0 0 2px;
  color: #73a48c;
  font-size: 10px;
}

.org-mini strong {
  overflow: hidden;
  max-width: 158px;
  color: #e4f3e8;
  font-size: 12px;
  text-overflow: ellipsis;
  white-space: nowrap;
}

.user-card {
  gap: 10px;
  padding: 14px 8px 0;
  border-top: 1px solid rgba(186, 227, 204, 0.13);
}

.avatar {
  display: grid;
  width: 32px;
  height: 32px;
  flex: 0 0 auto;
  place-items: center;
  border: 1px solid rgba(213, 243, 222, 0.22);
  border-radius: 50%;
  color: #0b3b2e;
  background: #b7dfc3;
  font-size: 13px;
  font-weight: 800;
}

.user-card > div {
  min-width: 0;
}

.user-card strong {
  overflow: hidden;
  color: #edf8ef;
  font-size: 12px;
  text-overflow: ellipsis;
  white-space: nowrap;
}

.user-card button {
  gap: 5px;
  margin-top: 5px;
  padding: 0;
  color: #8fb5a3;
  font-size: 11px;
}

.user-card button:hover {
  color: #d4efdd;
}

.content {
  min-height: 100vh;
  padding: clamp(22px, 3vw, 42px) clamp(20px, 4vw, 62px);
  background:
    radial-gradient(circle at 92% 4%, rgba(213, 229, 219, 0.55), transparent 24%),
    #f4f6f2;
}

.topbar {
  align-items: flex-start;
  margin-bottom: 30px;
}

.topbar-copy {
  min-width: 0;
}

.breadcrumb {
  display: flex;
  align-items: center;
  gap: 4px;
  color: #739181;
}

.breadcrumb span:last-child {
  color: #376b56;
}

.topbar h1 {
  margin: 10px 0 5px;
  color: #183d30;
  font-size: clamp(25px, 3vw, 34px);
  letter-spacing: -0.04em;
}

.topbar-copy > p {
  margin: 0;
  color: #788980;
  font-size: 13px;
  line-height: 1.6;
}

.topbar-actions {
  gap: 12px;
  padding-top: 2px;
}

.tenant-select {
  min-width: 220px;
  gap: 6px;
}

.tenant-select > span {
  color: #789084;
  font-size: 10px;
  font-weight: 800;
  letter-spacing: 0.08em;
}

.select-shell {
  position: relative;
  min-height: 42px;
  border: 1px solid #d6e1d9;
  border-radius: 10px;
  background: #fff;
}

.select-shell select {
  min-height: 40px;
  padding: 0 36px 0 12px;
  border: 0;
  outline: 0;
  appearance: none;
  background: transparent;
  cursor: pointer;
}

.select-shell > svg {
  position: absolute;
  top: 50%;
  right: 11px;
  color: #6f8b7d;
  pointer-events: none;
  transform: translateY(-50%);
}

.notification-button {
  position: relative;
  display: grid;
  width: 42px;
  height: 42px;
  flex: 0 0 auto;
  place-items: center;
  border: 1px solid #d6e1d9;
  border-radius: 10px;
  color: #527866;
  background: #fff;
}

.notification-button:hover {
  border-color: #a9c8b7;
  color: #1c6b52;
  background: #f7fbf8;
}

.notification-button i {
  position: absolute;
  top: 9px;
  right: 10px;
  width: 5px;
  height: 5px;
  border: 2px solid #fff;
  border-radius: 50%;
  background: #d4824b;
}

.content > .message {
  margin: 0 0 20px;
  border: 1px solid transparent;
  border-radius: 10px;
  box-shadow: 0 7px 18px rgba(44, 72, 55, 0.04);
}

.content > .message.success {
  border-color: #c6e2ce;
  background: #f0f9f1;
}

.content > .message.error {
  border-color: #f0cbc4;
  background: #fff5f3;
}

@media (max-width: 980px) {
  .auth-shell {
    grid-template-columns: 1fr;
  }

  .auth-showcase {
    min-height: 500px;
  }

  .showcase-copy {
    margin-top: 70px;
  }

  .showcase-art {
    right: 12%;
    bottom: 12%;
  }
}

@media (max-width: 760px) {
  .app-shell {
    grid-template-columns: 1fr;
  }

  .sidebar {
    position: static;
    height: auto;
    max-height: none;
    overflow: visible;
    padding: 17px 15px;
  }

  .sidebar-head {
    grid-template-columns: 1fr auto;
    align-items: center;
    padding-bottom: 17px;
  }

  .sidebar-mode {
    align-self: center;
  }

  .side-nav {
    grid-template-columns: repeat(2, minmax(0, 1fr));
    gap: 5px;
    padding-top: 15px;
  }

  .nav-caption,
  .nav-caption-spaced {
    display: none;
  }

  .nav-group {
    min-width: 0;
  }

  .nav-children {
    margin-left: 9px;
  }

  .sidebar-bottom {
    display: none;
  }

  .content {
    min-height: auto;
  }

  .topbar {
    align-items: stretch;
    flex-direction: column;
  }

  .topbar-actions {
    width: 100%;
  }

  .tenant-select {
    flex: 1;
  }
}

@media (max-width: 560px) {
  .auth-shell {
    padding: 12px;
  }

  .auth-showcase {
    min-height: 450px;
    padding: 25px;
    border-radius: 20px;
  }

  .showcase-copy h2 {
    font-size: 35px;
  }

  .showcase-art {
    right: 5%;
    bottom: 12%;
    width: 190px;
  }

  .showcase-footer {
    flex-wrap: wrap;
    gap: 10px 16px;
  }

  .auth-card {
    padding: 27px 20px;
    border-radius: 18px;
  }

	.auth-card h1 {
    margin-top: 28px;
	}

	.captcha-field { grid-template-columns: 1fr 118px; }

  .content {
    padding: 22px 15px;
  }

  .topbar-actions {
    gap: 8px;
  }

  .tenant-select {
    min-width: 0;
  }

  .notification-button {
    width: 40px;
    height: 40px;
  }
}
</style>

<style scoped>
.app-shell{grid-template-columns:272px minmax(0,1fr);background:#f8faf9}.content{width:100%;min-width:0;padding:32px clamp(24px,4vw,64px) 56px;background:linear-gradient(180deg,#f8faf9 0,#f5f8f6 100%)}.content>*{width:100%;max-width:none}.topbar{min-height:76px;align-items:flex-start;margin-bottom:26px;padding-bottom:22px;border-bottom:1px solid #e2e9e4}.topbar h1{font-size:30px;letter-spacing:-.03em}.topbar-copy>p{max-width:720px}.topbar-actions{align-items:flex-end}.tenant-select{min-width:240px}.notification-button{width:42px;height:42px}.message{max-width:none;margin:0 0 20px!important;border-radius:10px!important}
.sidebar{padding:26px 16px 18px;border-right:1px solid rgba(185,226,203,.08);background:linear-gradient(180deg,#123c31 0%,#0e3027 100%);box-shadow:none}.sidebar-glow{z-index:0;pointer-events:none}.sidebar-head,.side-nav,.sidebar-bottom{position:relative;z-index:1}.sidebar-head{padding:0 5px 23px}.side-nav{padding-top:20px}.nav-caption{padding:0 7px}.nav-caption-spaced{margin-top:17px}.nav-item{min-height:42px;border-radius:9px}.nav-child{border-radius:8px}.org-mini{border-radius:11px}.topbar-actions{gap:12px}.select-shell,.notification-button{border-color:#d9e4dc;border-radius:10px;box-shadow:0 1px 2px rgba(25,55,43,.025)}
@media(max-width:760px){.app-shell{grid-template-columns:1fr}.content{padding:24px 16px 36px}.sidebar{padding:17px 15px}}

@media(min-width:761px){
  .sidebar{height:100dvh;overflow:visible;z-index:1}
  .sidebar-head,.sidebar-bottom{flex:0 0 auto}
  .side-nav{min-height:0;flex:1 1 auto;overflow-x:hidden;overflow-y:auto;overscroll-behavior:contain}
}
</style>
