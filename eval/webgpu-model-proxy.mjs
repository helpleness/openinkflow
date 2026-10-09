import { createReadStream } from 'node:fs'
import { stat } from 'node:fs/promises'
import { createServer } from 'node:http'
import { Readable } from 'node:stream'

const port = Number(process.env.WEBGPU_EVAL_MODEL_PROXY_PORT || 5175)
const localRerankFile = process.env.WEBGPU_EVAL_RERANK_MODEL_PATH || ''
const rerankPath = '/onnx-community/bge-reranker-v2-m3-ONNX/resolve/main/onnx/model_q4f16.onnx'

createServer(async (request, response) => {
  if (request.method !== 'GET' && request.method !== 'HEAD') {
    response.writeHead(405).end()
    return
  }
  const path = new URL(request.url, `http://127.0.0.1:${port}`).pathname
  try {
    if (localRerankFile && path === rerankPath) {
      const file = await stat(localRerankFile)
      response.writeHead(200, {
        'content-type': 'application/octet-stream',
        'content-length': file.size,
      })
      if (request.method === 'HEAD') response.end()
      else createReadStream(localRerankFile).pipe(response)
      return
    }
    const upstream = await fetch(`https://huggingface.co${path}`, { method: request.method })
    response.writeHead(upstream.status, {
      'content-type': upstream.headers.get('content-type') || 'application/octet-stream',
      ...(upstream.headers.get('content-length') ? { 'content-length': upstream.headers.get('content-length') } : {}),
    })
    if (request.method === 'HEAD' || !upstream.body) response.end()
    else Readable.fromWeb(upstream.body).pipe(response)
  } catch (error) {
    response.writeHead(502, { 'content-type': 'text/plain' }).end(String(error))
  }
}).listen(port, '127.0.0.1', () => {
  console.log(`WebGPU model proxy listening at http://127.0.0.1:${port}`)
})
