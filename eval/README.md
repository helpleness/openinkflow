# 公文 Agent Eval（public-office-v2）

本目录只包含合成公文材料，不包含用户文件、密钥或业务数据。`public_office_tasks_v2.jsonl` 有 120 条任务：20 个主题 × 检索、摘要、起草、改写、Tool Call、引用六类；`public_office_corpus_v1.jsonl` 有 40 个可引用片段。任务及语料由 `internal/ai/eval` 确定性生成。每条任务的 `user_prompt` 是用户实际提问，`system_instruction` 是工具使用、引用或格式约束。Agent 适配器应把两者作为不同角色传入；向量检索只嵌入 `user_prompt`，不得把系统约束拼进去。

```powershell
go run ./cmd/agent-eval suite
```

## 结果格式与计分

实际 Agent 的适配器应为每个任务写一行 JSONL。只提交真实执行得到的证据；未执行的字段应省略。检索或重排已经执行但没有结果时，显式写 `[]`，这样会计为零命中。

```json
{"task_id":"retrieval-archive","retrieved_chunk_ids":["PO-archive-01"],"reranked_chunk_ids":["PO-archive-01"],"citation_ids":["PO-archive-01"],"tool_calls":[{"name":"knowledge.search","success":true}],"task_passed":true}
```

`task_passed` 只能由配置好的人工或模型评审依据完整回答判定；计分器不会从关键词自动推断任务成功。运行：

```powershell
go run ./cmd/agent-eval score -results path/to/agent-results.jsonl -output path/to/report.json
```

指标口径：

| 指标 | 口径 |
| --- | --- |
| Retrieval Recall@K | 前 K 个召回片段中命中的 gold 片段数 ÷ gold 片段总数，跨已执行任务微平均 |
| Rerank 命中率@K | 前 K 个重排片段至少包含一个 gold 片段的任务数 ÷ 已执行重排任务数；包含上游召回失败的影响 |
| Tool Call 成功率 | 成功执行的预期工具数 ÷ 预期工具数，工具名和任务标签匹配 |
| Citation Accuracy | 引用中属于该任务 gold 片段的数量 ÷ 所有已输出引用数量；没有输出引用时为 N/A |
| Task Success Rate | 评审通过任务数 ÷ 已评审任务数；没有评审时为 N/A |

报告中的 `numerator` 和 `denominator` 保留原始计数，`value` 缺失表示 N/A。不要把 N/A 写成 0%，也不要把检索层测试称为端到端 Agent 成功率。

## 本机可复现的分层测试

`cmd/agent-retrieval-eval` 对 20 条检索任务调用真实本地 GGUF Embedding、SQLite、USearch 和 Rerank。它将合成片段写入独立的 SQLite `knowledge_chunks` 表，以 1024 维、余弦距离、F32、HNSW M=32、efConstruction=256、efSearch=64 保存 `.usearch`，关闭并重载两者后才开始查询。默认将每次运行的 `inkflow-eval.db` 与 `officialdoc_knowledge_chunks.usearch` 保留在被 Git 忽略的 `eval/.local/retrieval-*` 中，可用 `-fixture-root` 指定根目录；`-validation` 输出行数、数据库与索引向量逐维读回校验、自查询和精确余弦 Top10 对照。这些校验验证落盘与近似搜索，不能验证 Embedding 的语义质量。它尚未调用正式知识库的 FTS5 混合召回、权限过滤或 Chat LLM，因此 Tool Call、引用和 Task Success Rate 仍需完整 Agent 适配器。

`-query-mode user|qwen-instruct|focused|topic` 控制检索查询。默认的 `user` 直接使用 `user_prompt`；`qwen-instruct` 按 Qwen3 模型示例在用户问题前加检索任务指令，仅作诊断；`focused` 使用主题和办理事项、时限、责任目标；`topic` 仅用于诊断主题词的向量匹配能力。四种模式不改变语料、gold 标注或索引参数，应写入不同的结果文件。Agent 的 `system_instruction`、gold 片段正文和片段 ID 均不会拼入查询向量。Qwen3 指令模式与 Agent 系统提示词无关；本次合成样本默认查询已达到 Recall@10 满分，无法用于判断指令对真实语料的增益。

例如在已配置 CGO、USearch 和 CUDA DLL 路径的 Windows 环境运行：

```powershell
$models = Join-Path $env:LOCALAPPDATA 'InkFlow\models'
go run -tags inkflow_cuda ./cmd/agent-retrieval-eval -backend cuda `
  -embedding-model (Join-Path $models 'qwen3-embedding-0.6b-q4_k_m.gguf') `
  -rerank-model (Join-Path $models 'bge-reranker-v2-m3-Q4_K_M.gguf') `
  -results eval/results/retrieval-eval-cuda.jsonl `
  -report eval/results/retrieval-eval-cuda-report.json `
  -validation eval/results/retrieval-eval-cuda-validation.json
```

`cmd/agent-bench` 使用固定种子生成 10 万条合成向量，测量 HNSW 建索引时间、检索 P50/P95、原向量 Recall@K 和索引内存。该 Recall@K 是向量自查询命中率，不能代替公文语义检索 Recall@K。

`cmd/agent-model-bench` 使用上面的 40 条合成片段测试本地 Embedding 与 Rerank 的 CPU、CUDA、Vulkan 推理时间。编译标签及 PATH 必须与后端对应；模型文件放在本机，不提交到仓库。

Windows CGO 环境示例：

```powershell
$repo = (Get-Location).Path
$env:CGO_ENABLED = '1'
$env:CGO_CFLAGS = "-I$repo\third_party\usearch\windows_amd64"
$env:CGO_LDFLAGS = "-L$repo\third_party\usearch\windows_amd64 -lusearch_c -lstdc++ -static-libgcc -static-libstdc++ -lwinpthread"
go run ./cmd/agent-bench -chunks 100000 -dimensions 384 -queries 1000 -top-k 10 -output eval/results/retrieval-100k-windows.json
```

本机完整运行记录及适用范围见仓库根目录 `README.md` 的“Agent Eval 与本机性能”章节。

## 浏览器 WebGPU 分层 Eval

`eval/run-webgpu-eval.mjs` 在真实 Chrome WebGPU worker 中运行前端的 ONNX Q4F16 Embedding 与 Rerank。它把同一批 40 条合成证据嵌入为 1024 维向量，只将 20 条检索任务的 `user_prompt` 嵌入为查询，用**精确余弦相似度**取 Top10，再将这 10 条交给 WebGPU Rerank。评测请求明确绕过文本向量缓存，模型文件仍可复用浏览器 OPFS 缓存。此评测不经过 SQLite、USearch、FTS5、权限过滤或 Chat Agent；不能当作正式混合检索或端到端任务成功率。

先在 `web/` 安装依赖（包含 `playwright-core`），再开两个 PowerShell 终端：

```powershell
# 终端 1：从 Hugging Face 官方源获取缺失的公开模型文件
cd web
$env:VITE_HF_PROXY_TARGET = 'https://huggingface.co'
npm run dev -- --port 5174
```

```powershell
# 终端 2：在仓库根目录运行；需要本机已安装 Chrome，首次约下载 1.3 GB 模型
node eval/run-webgpu-eval.mjs
```

模型下载很慢时，可把已有的 **bge-reranker-v2-m3-ONNX Q4F16** 文件路径设为 `WEBGPU_EVAL_RERANK_MODEL_PATH`，运行 `node eval/webgpu-model-proxy.mjs`，并将终端 1 的 `VITE_HF_PROXY_TARGET` 改成 `http://127.0.0.1:5175`。代理仅从该路径读取指定模型文件，其余公开模型文件仍从 Hugging Face 获取。`CHROME_PATH`、`WEBGPU_EVAL_PROFILE` 和 `WEBGPU_EVAL_OUTPUT` 可分别指定 Chrome、浏览器缓存目录和结果目录。默认逐题结果与报告保存在被 Git 忽略的 `eval/.local/webgpu/`；只重新计分可运行 `node eval/run-webgpu-eval.mjs --score-only`。

2026-10-09 在 Chrome 154、NVIDIA Lovelace 适配器上实测两个 Q4F16 模型，逐题结果、计分报告与计时记录分别见 [`results/retrieval-eval-webgpu.jsonl`](results/retrieval-eval-webgpu.jsonl)、[`results/retrieval-eval-webgpu-report.json`](results/retrieval-eval-webgpu-report.json)、[`results/retrieval-eval-webgpu-run.json`](results/retrieval-eval-webgpu-run.json)：

| 指标 | 本次结果 |
| --- | --- |
| Retrieval Recall@1 / @3 / @5 / @10 | 20/40（50%） / 40/40（100%） / 40/40（100%） / 40/40（100%） |
| Rerank 命中率@1 / @3 / @5 / @10 | 20/20（100%） / 20/20（100%） / 20/20（100%） / 20/20（100%） |
| Embedding 用户问题 P50 / P95 | 196.9 / 207.1 ms（20 条，文本向量缓存绕过） |
| Embedding 吞吐 | 4.179 条/s（40 条证据＋20 条问题，模型加载不计入） |
| Rerank 10 候选 P50 / P95 | 120.1 / 164.1 ms（20 次） |
| Rerank 吞吐 | 75.472 文档/s（200 个候选，模型加载不计入） |

模型文件在计时前已缓存在 OPFS；本次 Embedding 模型初始化为 2052 ms，不包含首次下载。Rerank 每次处理 10 个候选，而根目录的 CPU/CUDA/Vulkan GGUF 基准每次处理 8 个候选，模型格式与执行路径也不同，因此延迟数字不应直接当作同条件速度比。Tool Call、Citation Accuracy 与 Task Success Rate 本次未执行，计为 N/A。
