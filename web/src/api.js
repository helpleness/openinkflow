import { getInferenceClientId } from './ai/inferenceClient'

// 桌面版由内嵌页面注入随机 loopback API 地址；Web 开发/部署版仍使用同源请求。
const API_BASE = globalThis.__INKFLOW_RUNTIME__?.apiBase || ''
const INFERENCE_PROVIDER = globalThis.__INKFLOW_RUNTIME__?.inferenceProvider || 'frontend'
const BACKEND_EMBEDDING_READY = Boolean(globalThis.__INKFLOW_RUNTIME__?.backendEmbeddingReady)
const BACKEND_EMBEDDING_MODEL_PATH = globalThis.__INKFLOW_RUNTIME__?.backendEmbeddingModelPath || ''
const BACKEND_RERANK_READY = Boolean(globalThis.__INKFLOW_RUNTIME__?.backendRerankReady)
const BACKEND_RERANK_MODEL_PATH = globalThis.__INKFLOW_RUNTIME__?.backendRerankModelPath || ''
const TAB_AUTH_REVOKED_KEY = 'inkflow.auth.tab-revoked'
const ACTIVE_AUTH_TAB_KEY = 'inkflow.auth.active-tab'
export const AUTH_SESSION_REPLACED_EVENT = 'inkflow:auth-session-replaced'

export function getAPIBase() {
  return API_BASE
}

// 桌面端后端会把该值注入 inkflow-runtime.js。它属于编译期/运行时能力，而不是
// 用户可以编辑的模型设置。
export function getInferenceProvider() {
  return String(INFERENCE_PROVIDER).trim().toLowerCase() === 'frontend' ? 'frontend' : 'local'
}

export function usesFrontendInference() {
  return getInferenceProvider() === 'frontend'
}

export function isBackendEmbeddingReady() {
  return BACKEND_EMBEDDING_READY
}

export function getBackendEmbeddingModelPath() {
  return BACKEND_EMBEDDING_MODEL_PATH
}

export function isBackendRerankReady() {
  return BACKEND_RERANK_READY
}

export function getBackendRerankModelPath() {
  return BACKEND_RERANK_MODEL_PATH
}
// Desktop uses an in-memory loopback credential. Browser deployments use an
// HttpOnly cookie exclusively; neither deployment keeps credentials in Web
// Storage. Remove the temporary legacy key once during upgrade.
try {
  localStorage.removeItem('inkflow_token')
  sessionStorage.removeItem('inkflow.web.session-token')
} catch {}

function createAuthTabId() {
  if (globalThis.crypto?.randomUUID) return globalThis.crypto.randomUUID()
  return `tab-${Date.now()}-${Math.random().toString(36).slice(2)}`
}

const authTabId = createAuthTabId()

export function isWebDeployment() {
  // 服务端会提供一个空的 inkflow-runtime.js，只有桌面壳会注入 loopback apiBase。
  // 不能只判断对象是否存在，否则 Web 版会错误跳过 sessionStorage。
  return !String(globalThis.__INKFLOW_RUNTIME__?.apiBase || '').trim()
}

export function usesCookieSession() { return isWebDeployment() }

function isTabAuthRevoked() {
  try {
    return sessionStorage.getItem(TAB_AUTH_REVOKED_KEY) === '1'
  } catch {
    return false
  }
}

let authToken = isTabAuthRevoked() || isWebDeployment() ? '' : (globalThis.__INKFLOW_RUNTIME__?.sessionToken || '')

export function setAuthToken(token) {
	authToken = isWebDeployment() ? '' : (token || '')
  try {
    if (authToken) {
      sessionStorage.removeItem(TAB_AUTH_REVOKED_KEY)
    } else if (localStorage.getItem(ACTIVE_AUTH_TAB_KEY) === authTabId) {
      localStorage.removeItem(ACTIVE_AUTH_TAB_KEY)
    }
		sessionStorage.removeItem('inkflow.web.session-token')
  } catch {}
}

// 桌面端只保存无敏感信息的活动窗口标记；浏览器会话始终由 HttpOnly Cookie 持有。
export function claimAuthSessionForTab() {
	if (!authToken || isWebDeployment()) return
  try {
    localStorage.setItem(ACTIVE_AUTH_TAB_KEY, authTabId)
  } catch {}
}

// 只撤销被替换的旧页面，不能删除新页面正在使用的进程内会话令牌。
export function revokeAuthTokenForTab(message = '当前账号已在新的窗口登录，本窗口已退出。') {
  const alreadyRevoked = !authToken && isTabAuthRevoked()
  authToken = ''
  try {
    sessionStorage.setItem(TAB_AUTH_REVOKED_KEY, '1')
  } catch {}
  if (!alreadyRevoked) {
    window.dispatchEvent(new CustomEvent(AUTH_SESSION_REPLACED_EVENT, { detail: { message } }))
  }
}

window.addEventListener('storage', (event) => {
  if (event.key !== ACTIVE_AUTH_TAB_KEY || !event.newValue || event.newValue === authTabId || !authToken) return
  revokeAuthTokenForTab()
})

export function getAuthToken() {
  return authToken
}

function authHeaders(headers = {}) {
	const inferenceHeaders = { ...headers, 'X-InkFlow-Inference-Client': getInferenceClientId() }
	if (!isWebDeployment()) inferenceHeaders['X-InkFlow-Desktop-Client'] = '1'
	return authToken ? { ...inferenceHeaders, Authorization: `Bearer ${authToken}` } : inferenceHeaders
}

async function parseJsonResponse(response) {
  const data = await response.json().catch(() => ({}))
  if (!response.ok || (typeof data.code === 'number' && data.code !== 0)) {
    throw new Error(data.msg || response.statusText || '请求失败')
  }
  return data.data ?? data
}

async function parseJsonEnvelope(response) {
  const data = await response.json().catch(() => ({}))
  if (!response.ok || (typeof data.code === 'number' && data.code !== 0)) {
    throw new Error(data.msg || response.statusText || '璇锋眰澶辫触')
  }
  return data
}

export async function apiGet(path, query = {}, options = {}) {
  const params = new URLSearchParams()
  Object.entries(query).forEach(([key, value]) => {
    if (value !== undefined && value !== null && value !== '') params.set(key, value)
  })
  const suffix = params.toString() ? `?${params.toString()}` : ''
  const response = await fetch(`${API_BASE}${path}${suffix}`, {
    ...options,
    credentials: 'include',
    headers: authHeaders(options.headers || {}),
  })
  return parseJsonResponse(response)
}

export async function apiGetFull(path, query = {}, options = {}) {
  const params = new URLSearchParams()
  Object.entries(query).forEach(([key, value]) => {
    if (Array.isArray(value)) {
      value.forEach((item) => {
        if (item !== undefined && item !== null && item !== '') params.append(key, item)
      })
      return
    }
    if (value !== undefined && value !== null && value !== '') params.set(key, value)
  })
  const suffix = params.toString() ? `?${params.toString()}` : ''
  const response = await fetch(`${API_BASE}${path}${suffix}`, {
    ...options,
    credentials: 'include',
    headers: authHeaders(options.headers || {}),
  })
  return parseJsonEnvelope(response)
}

export async function apiJson(path, body, method = 'POST', options = {}) {
  const response = await fetch(`${API_BASE}${path}`, {
    ...options,
    method,
    credentials: 'include',
    headers: authHeaders({ 'Content-Type': 'application/json', ...(options.headers || {}) }),
    body: JSON.stringify(body),
  })
  return parseJsonResponse(response)
}

export async function apiUpload(path, formData, options = {}) {
  const response = await fetch(`${API_BASE}${path}`, {
    method: 'POST',
    ...options,
    credentials: 'include',
    headers: authHeaders(options.headers || {}),
    body: formData,
  })
  return parseJsonResponse(response)
}

export async function apiDownload(path, fallbackFilename = 'InkFlow-data.xlsx', options = {}) {
	const response = await fetch(`${API_BASE}${path}`, {
		credentials: 'include',
		...options,
		headers: authHeaders(options.headers || {}),
	})
  if (!response.ok) {
    const data = await response.json().catch(() => ({}))
    throw new Error(data.msg || response.statusText || '下载失败')
  }
  const blob = await response.blob()
  const disposition = response.headers.get('Content-Disposition') || ''
  const matched = disposition.match(/filename="?([^";]+)"?/i)
  const filename = matched?.[1] || fallbackFilename
  const url = URL.createObjectURL(blob)
  const anchor = document.createElement('a')
  anchor.href = url
  anchor.download = filename
  document.body.appendChild(anchor)
  anchor.click()
  anchor.remove()
  URL.revokeObjectURL(url)
}

export async function apiDelete(path, query = {}, options = {}) {
  const params = new URLSearchParams(query)
  const response = await fetch(`${API_BASE}${path}?${params.toString()}`, {
    ...options,
    method: 'DELETE',
    credentials: 'include',
    headers: authHeaders(options.headers || {}),
  })
  return parseJsonResponse(response)
}

async function readEventStream(response, handlers = {}) {
	if (!response.ok || !response.body) {
		const text = await response.text().catch(() => '')
		throw new Error(text || response.statusText || '流式请求失败')
	}
	const contentType = String(response.headers.get('Content-Type') || '').toLowerCase()
	if (!contentType.includes('text/event-stream')) {
		const text = await response.text().catch(() => '')
		try {
			const payload = JSON.parse(text)
			throw new Error(payload.msg || payload.message || '本地服务没有建立流式连接')
		} catch (error) {
			if (error instanceof SyntaxError) {
				throw new Error(text || '本地服务没有建立流式连接')
			}
			throw error
		}
	}

	const reader = response.body.getReader()
	const decoder = new TextDecoder()
	let buffer = ''
	let receivedEvent = false

  const dispatch = async (rawEvent) => {
    const lines = rawEvent.split('\n')
    let event = 'message'
    const dataLines = []
    for (const line of lines) {
      if (line.startsWith('event:')) event = line.slice(6).trim()
      if (line.startsWith('data:')) dataLines.push(line.slice(5).trimStart())
    }
		if (!dataLines.length) return
		receivedEvent = true
    const rawData = dataLines.join('\n')
    const payload = rawData === '[DONE]' ? rawData : JSON.parse(rawData)
    await handlers[event]?.(payload)
    await handlers.message?.({ event, payload })
    return event === 'done' || event === 'error' || rawData === '[DONE]'
  }

	while (true) {
    const { done, value } = await reader.read()
    if (done) break
    buffer += decoder.decode(value, { stream: true })
    const events = buffer.split('\n\n')
    buffer = events.pop() || ''
    for (const event of events) {
      if (!event.trim()) continue
      if (await dispatch(event)) {
        await reader.cancel().catch(() => {})
        return
      }
    }
  }
	if (buffer.trim() && await dispatch(buffer)) {
		await reader.cancel().catch(() => {})
		return
	}
	if (!receivedEvent) throw new Error('本地服务没有返回流式事件，请重启 InkFlow 后重试')
	throw new Error('流式连接意外断开，请重试')
}

export async function streamJson(path, body, handlers = {}, options = {}) {
	const { connectTimeoutMs = 0, signal: externalSignal, ...fetchOptions } = options
	const controller = new AbortController()
	let timedOut = false
	let timer = 0
	const cancelFromCaller = () => controller.abort()
	if (externalSignal) externalSignal.addEventListener('abort', cancelFromCaller, { once: true })
	if (connectTimeoutMs > 0) {
		timer = window.setTimeout(() => {
			timedOut = true
			controller.abort()
		}, connectTimeoutMs)
	}
	try {
		const response = await fetch(`${API_BASE}${path}`, {
			...fetchOptions,
			method: 'POST',
			credentials: 'include',
			headers: {
				Accept: 'text/event-stream',
				'Content-Type': 'application/json',
				...(authToken ? { Authorization: `Bearer ${authToken}` } : {}),
				'X-InkFlow-Inference-Client': getInferenceClientId(),
				...(fetchOptions.headers || {}),
			},
			body: JSON.stringify(body),
			signal: controller.signal,
		})
		if (timer) window.clearTimeout(timer)
		return await readEventStream(response, handlers)
	} catch (error) {
		if (timedOut) throw new Error('连接本地服务超时，请重启 InkFlow 后重试')
		if (error?.name === 'AbortError') throw new Error('流式请求已取消')
		throw error
	} finally {
		if (timer) window.clearTimeout(timer)
		if (externalSignal) externalSignal.removeEventListener('abort', cancelFromCaller)
	}
}

// streamGet opens an authenticated SSE request. It uses fetch rather than
// EventSource because both desktop and Web deployments authenticate with the
// normal Authorization header.
export async function streamGet(path, handlers = {}, options = {}) {
	const { signal, ...fetchOptions } = options
	const response = await fetch(`${API_BASE}${path}`, {
		...fetchOptions,
		method: 'GET',
		credentials: 'include',
		headers: authHeaders({ Accept: 'text/event-stream', ...(fetchOptions.headers || {}) }),
		signal,
	})
	return readEventStream(response, handlers)
}
