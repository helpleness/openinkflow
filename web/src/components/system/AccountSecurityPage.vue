<script setup>
import { onMounted, ref } from 'vue'
import { toDataURL } from 'qrcode'

import { disableMFA, enableMFA, fetchSessions, revokeOtherSessions, revokeSession, setupMFA } from '../../systemApi'

const props = defineProps({ user: { type: Object, required: true }, tenantId: { type: Number, required: true } })
const emit = defineEmits(['notice', 'updated', 'logout'])
const sessions = ref([])
const loading = ref(false)
const action = ref('')
const setup = ref(null)
const mfaQRCode = ref('')
const enableCode = ref('')
const disableForm = ref({ password: '', code: '' })

function showError(error) { emit('notice', { type: 'error', text: error?.message || '个人中心设置未完成。' }) }
async function refresh() {
  loading.value = true
  try { sessions.value = await fetchSessions(props.tenantId) || [] } catch (error) { showError(error) } finally { loading.value = false }
}
async function beginMFA() {
  action.value = 'setup'
  try {
    const nextSetup = await setupMFA(props.tenantId)
    mfaQRCode.value = await toDataURL(nextSetup.otpauth_url, {
      errorCorrectionLevel: 'M',
      margin: 2,
      width: 220,
      color: { dark: '#173f31', light: '#ffffff' },
    })
    setup.value = nextSetup
    enableCode.value = ''
  } catch (error) { showError(error) } finally { action.value = '' }
}
async function confirmMFA() {
  action.value = 'enable'
  try {
    await enableMFA(props.tenantId, enableCode.value)
    setup.value = null; mfaQRCode.value = ''; enableCode.value = ''
    emit('updated', { ...props.user, mfa_enabled: true })
    emit('notice', { text: '多重验证已启用。请妥善保存验证器配置。' })
  } catch (error) { showError(error) } finally { action.value = '' }
}
async function turnOffMFA() {
  action.value = 'disable'
  try {
    await disableMFA(props.tenantId, disableForm.value.password, disableForm.value.code)
    emit('logout')
  } catch (error) { showError(error) } finally { action.value = '' }
}
async function removeSession(session) {
  action.value = `session-${session.id}`
  try {
    const result = await revokeSession(props.tenantId, session.id)
    if (result?.current) { emit('logout'); return }
    await refresh()
    emit('notice', { text: '设备会话已撤销。' })
  } catch (error) { showError(error) } finally { action.value = '' }
}
async function removeOthers() {
  action.value = 'others'
  try { await revokeOtherSessions(props.tenantId); await refresh(); emit('notice', { text: '其他设备已退出。' }) } catch (error) { showError(error) } finally { action.value = '' }
}

onMounted(refresh)
</script>

<template>
  <section class="personal-center-page">
    <article class="panel personal-profile">
      <header class="personal-header"><div><p class="eyebrow">PERSONAL CENTER</p><h2>个人信息</h2><p>管理你的登录身份和多重验证设置。</p></div><span class="profile-avatar">{{ (user.username || '?').slice(0, 1).toUpperCase() }}</span></header>
      <dl class="profile-meta"><div><dt>用户名</dt><dd>{{ user.username }}</dd></div><div><dt>账号状态</dt><dd>{{ user.status === 'active' ? '正常' : (user.status || '未知') }}</dd></div><div><dt>认证方式</dt><dd>{{ user.auth_domain ? '远程认证' : '本地账号' }}</dd></div></dl>
      <section class="mfa-section"><header><div><h3>多重验证（MFA）</h3><p>使用兼容 TOTP 的验证器应用生成 6 位动态验证码。</p></div><span :class="['status', { enabled: user.mfa_enabled }]">{{ user.mfa_enabled ? '已启用' : (user.mfa_enrollment_required ? '要求绑定' : '未启用') }}</span></header>
        <div v-if="!user.mfa_enabled && !setup" class="actions"><button class="primary" type="button" :disabled="!!action" @click="beginMFA">启用多重验证</button></div>
        <div v-else-if="setup" class="mfa-setup"><p>使用验证器扫描二维码完成添加；也可手动输入下方密钥。此密钥只在当前页面显示一次。</p><div class="mfa-qr"><img :src="mfaQRCode" alt="用于绑定 MFA 的验证器二维码" /><span>请使用 Google Authenticator、Microsoft Authenticator 或其他兼容 TOTP 的应用扫描。</span></div><code>{{ setup.secret }}</code><textarea readonly :value="setup.otpauth_url" aria-label="验证器配置链接" /><label>验证器动态验证码<input v-model="enableCode" inputmode="numeric" maxlength="6" autocomplete="one-time-code" placeholder="6 位验证码" /></label><div class="actions"><button class="secondary" type="button" :disabled="!!action" @click="setup = null; mfaQRCode = ''">取消</button><button class="primary" type="button" :disabled="action === 'enable' || !enableCode" @click="confirmMFA">{{ action === 'enable' ? '验证中…' : '确认启用' }}</button></div></div>
        <form v-else class="disable-form" @submit.prevent="turnOffMFA"><p>关闭 MFA 会使全部设备退出登录；请同时输入当前密码和验证器验证码。</p><label>当前密码<input v-model="disableForm.password" type="password" autocomplete="current-password" required /></label><label>动态验证码<input v-model="disableForm.code" inputmode="numeric" maxlength="6" autocomplete="one-time-code" required /></label><button class="danger" type="submit" :disabled="action === 'disable'">{{ action === 'disable' ? '处理中…' : '关闭 MFA 并退出全部设备' }}</button></form>
      </section>
    </article>
    <article class="panel"><header><div><h3>已登录设备</h3><p>会话令牌仅保存为服务端哈希；可单独撤销可疑设备。</p></div><div class="actions"><button class="secondary" type="button" :disabled="loading || !!action" @click="refresh">刷新</button><button class="secondary" type="button" :disabled="!sessions.length || action === 'others'" @click="removeOthers">{{ action === 'others' ? '撤销中…' : '退出其他设备' }}</button></div></header>
      <div v-if="loading" class="empty">正在读取设备会话…</div><div v-else-if="!sessions.length" class="empty">暂无有效设备会话。</div><div v-else class="session-list"><article v-for="session in sessions" :key="session.id"><div><strong>{{ session.current ? '当前设备' : (session.device_name || '未知设备') }}</strong><small>{{ session.client_ip || 'IP 未记录' }} · 最近活动 {{ new Date(session.last_seen_at).toLocaleString() }} · {{ session.kind }}</small></div><button class="text-button" type="button" :disabled="action === 'session-' + session.id" @click="removeSession(session)">{{ action === 'session-' + session.id ? '撤销中…' : '撤销' }}</button></article></div>
    </article>
  </section>
</template>

<style scoped>
.mfa-qr{display:flex;align-items:center;gap:16px;padding:14px;border:1px solid #d8e7de;border-radius:10px;background:#f8fcf9}.mfa-qr img{display:block;width:176px;height:176px;flex:0 0 176px;border-radius:6px;image-rendering:pixelated}.mfa-qr span{color:#537064;font-size:13px;line-height:1.65}@media(max-width:620px){.mfa-qr{align-items:stretch;flex-direction:column}.mfa-qr img{align-self:center}}
.personal-center-page{display:grid;gap:20px;max-width:980px}.panel{padding:26px;border:1px solid #dfe8e2;border-radius:15px;background:#fff;box-shadow:0 12px 30px rgba(24,61,42,.045)}.eyebrow{margin:0;color:#54846e;font-size:11px;font-weight:800;letter-spacing:.14em}.personal-header,.panel header{display:flex;align-items:flex-start;justify-content:space-between;gap:16px}.personal-header h2{margin:7px 0;color:#173f31;font-size:27px}.personal-header>div>p:last-child,header p,.mfa-setup p,.disable-form p{margin:7px 0 0;color:#687b71;font-size:13px;line-height:1.65}.profile-avatar{display:grid;width:48px;height:48px;place-items:center;border-radius:50%;color:#fff;background:linear-gradient(145deg,#3b856f,#286652);box-shadow:0 0 0 5px #edf6f0;font-size:18px;font-weight:800}.profile-meta{display:grid;grid-template-columns:repeat(3,minmax(0,1fr));gap:12px;margin:24px 0 0}.profile-meta div{padding:12px;border:1px solid #e0ebe3;border-radius:9px;background:#fbfdfc}.profile-meta dt{color:#788b81;font-size:12px}.profile-meta dd{margin:5px 0 0;color:#244c3d;font-size:14px;font-weight:750}.mfa-section{margin-top:20px;padding-top:20px;border-top:1px solid #e5eee8}.panel h3{margin:0;color:#244c3d;font-size:17px}.status{padding:5px 9px;border-radius:999px;color:#7a5c2d;background:#fff4dc;font-size:12px;font-weight:750}.status.enabled{color:#17694f;background:#e7f5ea}.actions{display:flex;gap:9px;align-items:center;flex-wrap:wrap;margin-top:18px}.primary,.secondary,.danger,.text-button{min-height:39px;padding:0 13px;border-radius:8px;font:inherit;font-size:13px;font-weight:750;cursor:pointer}.primary{border:0;color:#fff;background:#17694f}.secondary{border:1px solid #cadbd1;color:#26634d;background:#fff}.danger{border:1px solid #efc9c4;color:#a13f37;background:#fff6f4}.text-button{border:0;color:#17694f;background:transparent}.mfa-setup,.disable-form{display:grid;gap:12px;margin-top:18px}.mfa-setup code{display:block;overflow:auto;padding:12px;border:1px dashed #a8cdb7;border-radius:8px;color:#174f3d;background:#f3faf5;font-size:16px;letter-spacing:.08em}.mfa-setup textarea{min-height:62px;resize:vertical}.mfa-setup textarea,.mfa-setup input,.disable-form input{width:100%;padding:10px 11px;border:1px solid #ceddd4;border-radius:8px;color:#244438;font:inherit}.mfa-setup label,.disable-form label{display:grid;gap:7px;color:#466457;font-size:13px;font-weight:700}.session-list{display:grid;gap:9px;margin-top:18px}.session-list article{display:flex;align-items:center;justify-content:space-between;gap:16px;padding:13px;border:1px solid #e0e9e3;border-radius:9px;background:#fbfdfc}.session-list strong,.session-list small{display:block}.session-list strong{color:#244c3d;font-size:13px}.session-list small{margin-top:5px;color:#718279;font-size:12px;line-height:1.5}.empty{margin-top:18px;padding:25px;border:1px dashed #d4e1d8;border-radius:9px;color:#75857c;text-align:center}@media(max-width:620px){.panel{padding:20px}.profile-meta{grid-template-columns:1fr}.personal-header,.panel header,.session-list article{align-items:stretch;flex-direction:column}.actions>*{flex:1}.text-button{align-self:flex-start}}
</style>
