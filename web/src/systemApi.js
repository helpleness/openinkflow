import {
  apiGet,
  apiJson,
  claimAuthSessionForTab,
  getAPIBase,
  setAuthToken,
} from './api'

// Browser deployments use Nginx's /api prefix; the desktop runtime points
// directly at its loopback backend and therefore must use the root routes.
const API_PREFIX = getAPIBase() ? '' : '/api'

function tenantOptions(tenantID) {
  return {
    headers: {
      'X-InkFlow-Tenant-ID': String(tenantID),
    },
  }
}

export async function authenticate(mode, payload) {
  const endpoint = mode === 'register'
    ? `${API_PREFIX}/auth/local/register`
    : `${API_PREFIX}/auth/local/login`
  const result = await apiJson(endpoint, payload)
  if (result.pending_token) {
    setAuthToken('')
    return result
  }
  setAuthToken(result.session_token)
  claimAuthSessionForTab()
  return result
}

export function fetchCaptcha() { return apiGet(`${API_PREFIX}/auth/captcha`) }

export function fetchOAuthProviders() { return apiGet(`${API_PREFIX}/auth/oauth/providers`) }
export function oauthLoginURL(provider) {
  return `${API_PREFIX}/auth/oauth/${encodeURIComponent(provider)}/login`
}

export function setupMFA(tenantID) { return apiJson(`${API_PREFIX}/auth/mfa/setup`, {}, 'POST', tenantOptions(tenantID)) }
export function enableMFA(tenantID, code) { return apiJson(`${API_PREFIX}/auth/mfa/enable`, { code }, 'POST', tenantOptions(tenantID)) }
export function disableMFA(tenantID, password, code) { return apiJson(`${API_PREFIX}/auth/mfa/disable`, { password, code }, 'POST', tenantOptions(tenantID)) }
export function setupPendingMFA(pendingToken) { return apiJson(`${API_PREFIX}/auth/mfa/pending/setup`, { pending_token: pendingToken }) }
export async function completePendingMFA(pendingToken, code) {
  const result = await apiJson(`${API_PREFIX}/auth/mfa/pending/complete`, { pending_token: pendingToken, code })
  setAuthToken(result.session_token)
  claimAuthSessionForTab()
  return result
}
export function fetchSessions(tenantID) { return apiGet(`${API_PREFIX}/auth/sessions`, {}, tenantOptions(tenantID)) }
export function revokeSession(tenantID, sessionID) { return apiJson(`${API_PREFIX}/auth/sessions/${sessionID}`, {}, 'DELETE', tenantOptions(tenantID)) }
export function revokeOtherSessions(tenantID) { return apiJson(`${API_PREFIX}/auth/sessions/revoke-others`, {}, 'POST', tenantOptions(tenantID)) }

export function fetchCurrentUser() {
  return apiGet(`${API_PREFIX}/auth/me`)
}

export function fetchTenants() {
  return apiGet(`${API_PREFIX}/system/tenants`)
}

export function fetchOrganizations(tenantID) {
  return apiGet(`${API_PREFIX}/system/organizations`, {}, tenantOptions(tenantID))
}

export function fetchPublicOrganizations(tenantID) {
  return apiGet(`${API_PREFIX}/system/public-organizations`, {}, tenantOptions(tenantID))
}

export function createOrganization(tenantID, payload) {
  return apiJson(`${API_PREFIX}/system/organizations`, payload, 'POST', tenantOptions(tenantID))
}

export function setOrganizationVisibility(tenantID, organizationID, isVisible) {
  return apiJson(`${API_PREFIX}/system/organizations/${organizationID}/visibility`, { is_visible: isVisible }, 'PUT', tenantOptions(tenantID))
}

export function fetchRoles(tenantID) {
  return apiGet(`${API_PREFIX}/system/roles`, {}, tenantOptions(tenantID))
}

export function fetchAPIResources(tenantID) {
  return apiGet(`${API_PREFIX}/system/apis`, {}, tenantOptions(tenantID))
}

export function createRole(tenantID, payload) {
  return apiJson(`${API_PREFIX}/system/roles`, payload, 'POST', tenantOptions(tenantID))
}

export function updateRolePermissions(tenantID, roleID, payload) {
  return apiJson(`${API_PREFIX}/system/roles/${roleID}/permissions`, payload, 'PUT', tenantOptions(tenantID))
}

export function fetchModelSettings(tenantID) {
  return apiGet(`${API_PREFIX}/system/model-settings`, {}, tenantOptions(tenantID))
}

export function saveModelSettings(tenantID, payload) {
  return apiJson(`${API_PREFIX}/system/model-settings`, payload, 'PUT', tenantOptions(tenantID))
}

export function fetchMenus(tenantID) {
  return apiGet(`${API_PREFIX}/system/menus`, {}, tenantOptions(tenantID))
}

export function fetchMenuConfigs(tenantID) {
  return apiGet(`${API_PREFIX}/system/menu-configs`, {}, tenantOptions(tenantID))
}

export function syncMenuConfigs(tenantID, menus) {
  return apiJson(`${API_PREFIX}/system/menu-configs/sync`, { menus }, 'POST', tenantOptions(tenantID))
}

export function createMenuConfig(tenantID, payload) {
  return apiJson(`${API_PREFIX}/system/menu-configs`, payload, 'POST', tenantOptions(tenantID))
}

export function updateMenuConfig(tenantID, menuID, payload) {
  return apiJson(`${API_PREFIX}/system/menu-configs/${menuID}`, payload, 'PUT', tenantOptions(tenantID))
}

export function fetchMemberships(tenantID) {
  return apiGet(`${API_PREFIX}/system/memberships`, {}, tenantOptions(tenantID))
}

export function addMembership(tenantID, payload) {
  return apiJson(`${API_PREFIX}/system/memberships`, payload, 'POST', tenantOptions(tenantID))
}

export function fetchGlobalUsers(tenantID) { return apiGet(`${API_PREFIX}/system/users`, {}, tenantOptions(tenantID)) }
export function fetchMembershipApplications(tenantID) { return apiGet(`${API_PREFIX}/system/membership-applications`, {}, tenantOptions(tenantID)) }
export function applyToOrganization(tenantID, organizationID) { return apiJson(`${API_PREFIX}/system/membership-applications`, { organization_id: organizationID }, 'POST', tenantOptions(tenantID)) }
export function reviewMembershipApplication(tenantID, applicationID, payload) { return apiJson(`${API_PREFIX}/system/membership-applications/${applicationID}`, payload, 'PUT', tenantOptions(tenantID)) }

export function fetchAuditLogs(tenantID, limit = 100) {
  return apiGet(`${API_PREFIX}/system/audit-logs`, { limit }, tenantOptions(tenantID))
}

export async function logout() {
  try {
    await apiJson(`${API_PREFIX}/auth/logout`, {})
  } finally {
    setAuthToken('')
  }
}
