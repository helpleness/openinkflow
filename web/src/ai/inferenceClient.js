const INFERENCE_CLIENT_KEY = 'inkflow.inference.client-id'

function createInferenceClientId() {
  if (globalThis.crypto?.randomUUID) return globalThis.crypto.randomUUID()
  return `client-${Date.now()}-${Math.random().toString(36).slice(2)}`
}

export function getInferenceClientId() {
  let clientId = localStorage.getItem(INFERENCE_CLIENT_KEY)
  if (!clientId) {
    clientId = createInferenceClientId()
    localStorage.setItem(INFERENCE_CLIENT_KEY, clientId)
  }
  return clientId
}
