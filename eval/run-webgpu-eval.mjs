import { readFile, mkdir, writeFile } from 'node:fs/promises'
import { createRequire } from 'node:module'
import { join, resolve } from 'node:path'
import { fileURLToPath } from 'node:url'

const root = resolve(fileURLToPath(new URL('..', import.meta.url)))
const require = createRequire(new URL('../web/package.json', import.meta.url))
const { chromium } = require('playwright-core')
const url = process.env.WEBGPU_EVAL_URL || 'http://127.0.0.1:5174/webgpu-eval.html'
const outputDir = resolve(process.env.WEBGPU_EVAL_OUTPUT || join(root, 'eval/.local/webgpu'))
const profileDir = resolve(process.env.WEBGPU_EVAL_PROFILE || join(root, 'eval/.local/webgpu-chrome'))
const chrome = process.env.CHROME_PATH || 'C:/Program Files/Google/Chrome/Application/chrome.exe'

function parseJSONL(source) {
  return source.trim().split(/\r?\n/).map((line) => JSON.parse(line))
}

function percentile(samples, p) {
  if (!samples.length) return null
  const sorted = [...samples].sort((a, b) => a - b)
  return sorted[Math.ceil(sorted.length * p) - 1]
}

function dot(a, b) {
  let sum = 0
  for (let i = 0; i < a.length; i++) sum += a[i] * b[i]
  return sum
}

function score(tasks, results) {
  const taskById = new Map(tasks.map((task) => [task.id, task]))
  const ks = [1, 3, 5, 10]
  const retrieval = Object.fromEntries(ks.map((k) => [k, { numerator: 0, denominator: 0 }]))
  const rerank = Object.fromEntries(ks.map((k) => [k, { numerator: 0, denominator: 0 }]))
  for (const result of results) {
    const task = taskById.get(result.task_id)
    if (!task) throw new Error(`Unknown task ${result.task_id}`)
    const gold = new Set(task.relevant_chunk_ids)
    for (const k of ks) {
      retrieval[k].numerator += new Set(result.retrieved_chunk_ids.slice(0, k).filter((id) => gold.has(id))).size
      retrieval[k].denominator += gold.size
      rerank[k].numerator += Number(result.reranked_chunk_ids.slice(0, k).some((id) => gold.has(id)))
      rerank[k].denominator++
    }
  }
  for (const rates of [retrieval, rerank]) {
    for (const rate of Object.values(rates)) rate.value = rate.numerator / rate.denominator
  }
  return {
    suite_version: 'public-office-v2',
    task_count: tasks.length,
    submitted_result_count: results.length,
    missing_result_count: tasks.length - results.length,
    retrieval_recall_at_k: retrieval,
    rerank_hit_rate_at_k: rerank,
    tool_call_success_rate: { numerator: 0, denominator: 0 },
    citation_accuracy: { numerator: 0, denominator: 0 },
    task_success_rate: { numerator: 0, denominator: 0 },
    warnings: ['some tasks have no result and are excluded from metric denominators', 'Task Success Rate requires task_passed from a configured judge'],
  }
}

async function writeScore() {
  const tasks = parseJSONL(await readFile(join(root, 'eval/public_office_tasks_v2.jsonl'), 'utf8'))
  const results = parseJSONL(await readFile(join(outputDir, 'retrieval-results.jsonl'), 'utf8'))
  const report = score(tasks, results)
  await writeFile(join(outputDir, 'score-report.json'), JSON.stringify(report, null, 2) + '\n')
  console.log(JSON.stringify(report, null, 2))
}

async function main() {
  await mkdir(outputDir, { recursive: true })
  const corpus = parseJSONL(await readFile(join(root, 'eval/public_office_corpus_v1.jsonl'), 'utf8'))
  const tasks = parseJSONL(await readFile(join(root, 'eval/public_office_tasks_v2.jsonl'), 'utf8'))
    .filter((task) => task.kind === 'retrieval')

  const browser = await chromium.launchPersistentContext(profileDir, {
    executablePath: chrome,
    headless: true,
    args: ['--enable-unsafe-webgpu'],
  })
  let progress
  try {
    const page = browser.pages()[0] || await browser.newPage()
    page.on('console', (message) => {
      if (message.type() === 'error' || message.text().startsWith('[worker]')) {
        console.log(`[browser] ${message.text()}`)
      }
    })
    page.on('pageerror', (error) => console.error(`[page] ${error.message}`))
    await page.goto(url)
    await page.waitForFunction(() => Boolean(window.webgpuEval))
    const adapter = await page.evaluate(async () => {
      const gpu = navigator.gpu
      const device = await gpu?.requestAdapter({ powerPreference: 'high-performance' })
      const info = device?.info
      return {
        supported: Boolean(gpu), available: Boolean(device),
        info: info ? {
          vendor: info.vendor, architecture: info.architecture,
          device: info.device, description: info.description,
        } : null,
      }
    })
    if (!adapter.available) throw new Error('No WebGPU adapter in Chrome')
    console.log(`WebGPU adapter available; loading ONNX models from ${url}`)

    progress = setInterval(async () => {
      try {
        const state = await page.evaluate(() => window.webgpuEval.state)
        const downloads = Object.values(state.downloads).map((item) => `${item.kind}: ${item.state} ${item.progress ?? '?'}%`).join(', ')
        console.log(`${state.status} ${downloads}`)
      } catch {}
    }, 15000)
    let initMs
    const initAt = performance.now()
    await page.evaluate(() => window.webgpuEval.init())
    initMs = Math.round(performance.now() - initAt)
    console.log(`Embedding model ready in ${initMs} ms`)

    const vectors = []
    const embeddingTimes = []
    let embeddingCacheHits = 0
    for (const [index, chunk] of corpus.entries()) {
      const result = await page.evaluate((text) => window.webgpuEval.embed(text), `${chunk.title}\n${chunk.content}`)
      if (result.timings?.cache_hit) embeddingCacheHits++
      else embeddingTimes.push(result.timings?.total_ms || 0)
      if (result.timings?.device !== 'webgpu') throw new Error(`Embedding device: ${result.timings?.device}`)
      if (result.vector.length !== 1024 || result.vector.some((value) => !Number.isFinite(value))) {
        throw new Error(`Invalid embedding for ${chunk.id}`)
      }
      vectors.push(result.vector)
      console.log(`corpus ${index + 1}/${corpus.length}: ${chunk.id}, ${result.timings.total_ms} ms`)
    }

    const results = []
    const rerankTimes = []
    const queryTimes = []
    for (const [index, task] of tasks.entries()) {
      const embedded = await page.evaluate((text) => window.webgpuEval.embed(text), task.user_prompt)
      if (embedded.timings?.device !== 'webgpu') throw new Error(`Query device: ${embedded.timings?.device}`)
      if (embedded.timings?.cache_hit) embeddingCacheHits++
      else queryTimes.push(embedded.timings.total_ms)
      const ranked = corpus.map((chunk, position) => ({ chunk, position, similarity: dot(embedded.vector, vectors[position]) }))
        .sort((a, b) => b.similarity - a.similarity)
        .slice(0, 10)
      const docs = ranked.map(({ chunk }) => `${chunk.title}\n${chunk.content}`)
      const reranked = await page.evaluate(({ query, docs }) => window.webgpuEval.rerank(query, docs), {
        query: task.user_prompt,
        docs,
      })
      if (reranked.timings?.device !== 'webgpu' || reranked.timings?.fallback) {
        throw new Error(`Rerank did not use WebGPU: ${JSON.stringify(reranked.timings)}`)
      }
      if (reranked.results.length !== 10) throw new Error(`Rerank returned ${reranked.results.length} items`)
      rerankTimes.push(reranked.timings.total_ms)
      results.push({
        task_id: task.id,
        retrieved_chunk_ids: ranked.map(({ chunk }) => chunk.id),
        reranked_chunk_ids: reranked.results.map(({ index: position }) => ranked[position].chunk.id),
      })
      console.log(`task ${index + 1}/${tasks.length}: ${task.id}; embed ${embedded.timings.total_ms} ms, rerank ${reranked.timings.total_ms} ms`)
    }
    const state = await page.evaluate(() => window.webgpuEval.state)
    const embeddingInferenceMs = [...embeddingTimes, ...queryTimes]
    const embeddingTotalMs = embeddingInferenceMs.reduce((sum, value) => sum + value, 0)
    const rerankTotalMs = rerankTimes.reduce((sum, value) => sum + value, 0)
    const summary = {
      date: new Date().toISOString(),
      url,
      chrome_version: browser.browser()?.version() || 'unknown',
      adapter,
      models: state.models,
      model_cache: state.cache,
      corpus_count: corpus.length,
      retrieval_task_count: tasks.length,
      retrieval: 'exact cosine top 10 over WebGPU vectors; no SQLite, USearch, FTS5, or permissions',
      embedding_cache_hits: embeddingCacheHits,
      embedding_model_init_ms: initMs,
      embedding_corpus_p50_ms: percentile(embeddingTimes, 0.5),
      embedding_corpus_p95_ms: percentile(embeddingTimes, 0.95),
      embedding_query_p50_ms: percentile(queryTimes, 0.5),
      embedding_query_p95_ms: percentile(queryTimes, 0.95),
      embedding_inference_count: embeddingInferenceMs.length,
      embedding_total_ms: Math.round(embeddingTotalMs * 100) / 100,
      embedding_items_per_second: Math.round((embeddingInferenceMs.length * 1000 / embeddingTotalMs) * 1000) / 1000,
      rerank_10_candidates_p50_ms: percentile(rerankTimes, 0.5),
      rerank_10_candidates_p95_ms: percentile(rerankTimes, 0.95),
      rerank_total_ms: Math.round(rerankTotalMs * 100) / 100,
      rerank_documents_per_second: Math.round((tasks.length * 10 * 1000 / rerankTotalMs) * 1000) / 1000,
      timings_ms: { embedding_corpus: embeddingTimes, embedding_query: queryTimes, rerank_10_candidates: rerankTimes },
    }
    await writeFile(join(outputDir, 'retrieval-results.jsonl'), results.map((row) => JSON.stringify(row)).join('\n') + '\n')
    await writeFile(join(outputDir, 'run-summary.json'), JSON.stringify(summary, null, 2) + '\n')
    await writeScore()
    console.log(JSON.stringify(summary, null, 2))
  } finally {
    clearInterval(progress)
    await browser.close()
  }
}

(process.argv.includes('--score-only') ? writeScore() : main())
  .catch((error) => { console.error(error); process.exitCode = 1 })
