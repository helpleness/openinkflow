import { apiDelete, apiDownload, apiGet, apiJson, apiUpload, getAPIBase, streamGet } from './api'

const API_PREFIX = getAPIBase() ? '' : '/api'

function tenantOptions(tenantID) {
  return { headers: { 'X-InkFlow-Tenant-ID': String(tenantID) } }
}

export function listKnowledgeDocuments(tenantID, organizationID) {
  return apiGet(`${API_PREFIX}/officialdoc/knowledge-documents`, { organization_id: organizationID }, tenantOptions(tenantID))
}

export function getKnowledgeDocument(tenantID, documentID) {
  return apiGet(`${API_PREFIX}/officialdoc/knowledge-documents/${documentID}`, {}, tenantOptions(tenantID))
}

export function getKnowledgeDocumentDownload(tenantID, documentID) {
  return apiGet(`${API_PREFIX}/officialdoc/knowledge-documents/${documentID}/download`, {}, tenantOptions(tenantID))
}

export function importKnowledgeDocument(tenantID, organizationID, file) {
  const form = new FormData()
  form.set('organization_id', String(organizationID))
  form.set('file', file)
  return apiUpload(`${API_PREFIX}/officialdoc/knowledge-documents/import`, form, tenantOptions(tenantID))
}

export function reindexKnowledgeDocument(tenantID, documentID) {
  return apiJson(`${API_PREFIX}/officialdoc/knowledge-documents/${documentID}/reindex`, {}, 'POST', tenantOptions(tenantID))
}

export function deleteKnowledgeDocument(tenantID, documentID) {
  return apiDelete(`${API_PREFIX}/officialdoc/knowledge-documents/${documentID}`, {}, tenantOptions(tenantID))
}

export function searchKnowledge(tenantID, payload) {
  return apiJson(`${API_PREFIX}/officialdoc/knowledge-search`, payload, 'POST', tenantOptions(tenantID))
}

export function listDocumentTemplates(tenantID, organizationID) {
  return apiGet(`${API_PREFIX}/officialdoc/document-templates`, { organization_id: organizationID }, tenantOptions(tenantID))
}

export function createDocumentTemplate(tenantID, payload) {
  return apiJson(`${API_PREFIX}/officialdoc/document-templates`, payload, 'POST', tenantOptions(tenantID))
}

export function updateDocumentTemplate(tenantID, templateID, payload) {
  return apiJson(`${API_PREFIX}/officialdoc/document-templates/${templateID}`, payload, 'PUT', tenantOptions(tenantID))
}

export function listWritingTasks(tenantID, organizationID) {
  return apiGet(`${API_PREFIX}/officialdoc/writing-tasks`, { organization_id: organizationID }, tenantOptions(tenantID))
}

export function getWritingTask(tenantID, taskID) {
  return apiGet(`${API_PREFIX}/officialdoc/writing-tasks/${taskID}`, {}, tenantOptions(tenantID))
}

export function createWritingTask(tenantID, payload) {
  return apiJson(`${API_PREFIX}/officialdoc/writing-tasks`, payload, 'POST', tenantOptions(tenantID))
}

export function listWritingRuns(tenantID, taskID) {
	return apiGet(`${API_PREFIX}/officialdoc/writing-tasks/${taskID}/runs`, {}, tenantOptions(tenantID))
}

export function startWritingRun(tenantID, taskID, payload) {
	return apiJson(`${API_PREFIX}/officialdoc/writing-tasks/${taskID}/runs`, payload, 'POST', tenantOptions(tenantID))
}

export function getWritingRun(tenantID, runID) {
	return apiGet(`${API_PREFIX}/officialdoc/writing-runs/${runID}`, {}, tenantOptions(tenantID))
}

export function streamWritingRun(tenantID, runID, handlers, options = {}) {
	return streamGet(`${API_PREFIX}/officialdoc/writing-runs/${runID}/events`, handlers, {
		...options,
		headers: { ...(options.headers || {}), ...tenantOptions(tenantID).headers },
	})
}

export function pauseWritingRun(tenantID, runID) {
	return apiJson(`${API_PREFIX}/officialdoc/writing-runs/${runID}/pause`, {}, 'POST', tenantOptions(tenantID))
}

export function resumeWritingRun(tenantID, runID) {
	return apiJson(`${API_PREFIX}/officialdoc/writing-runs/${runID}/resume`, {}, 'POST', tenantOptions(tenantID))
}

export function saveWritingTaskVersion(tenantID, taskID, payload) {
  return apiJson(`${API_PREFIX}/officialdoc/writing-tasks/${taskID}/versions`, payload, 'POST', tenantOptions(tenantID))
}

export function exportWritingTaskVersion(tenantID, taskID, versionID, format, fallbackFilename) {
  return apiDownload(`${API_PREFIX}/officialdoc/writing-tasks/${taskID}/versions/${versionID}/export?format=${encodeURIComponent(format)}`, fallbackFilename, tenantOptions(tenantID))
}

export function getDocumentVersionDiff(tenantID, taskID, versionID, baseVersionID = 0) {
  return apiGet(`${API_PREFIX}/officialdoc/writing-tasks/${taskID}/versions/${versionID}/diff`, { base_version_id: baseVersionID }, tenantOptions(tenantID))
}

export function validateDocumentVersion(tenantID, taskID, versionID) {
  return apiJson(`${API_PREFIX}/officialdoc/writing-tasks/${taskID}/versions/${versionID}/validate`, {}, 'POST', tenantOptions(tenantID))
}

export function listDocumentReviewComments(tenantID, taskID, versionID) {
  return apiGet(`${API_PREFIX}/officialdoc/writing-tasks/${taskID}/versions/${versionID}/comments`, {}, tenantOptions(tenantID))
}

export function createDocumentReviewComment(tenantID, taskID, versionID, payload) {
  return apiJson(`${API_PREFIX}/officialdoc/writing-tasks/${taskID}/versions/${versionID}/comments`, payload, 'POST', tenantOptions(tenantID))
}

export function resolveDocumentReviewComment(tenantID, taskID, versionID, commentID, resolved) {
  return apiJson(`${API_PREFIX}/officialdoc/writing-tasks/${taskID}/versions/${versionID}/comments/${commentID}`, { resolved }, 'PUT', tenantOptions(tenantID))
}
