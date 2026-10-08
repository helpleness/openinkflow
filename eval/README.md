# 公文 Agent Eval（public-office-v1）

本目录只包含合成公文材料，不包含用户文件、密钥或业务数据。`public_office_tasks_v1.jsonl` 有 120 条任务：20 个主题 × 检索、摘要、起草、改写、Tool Call、引用六类；`public_office_corpus_v1.jsonl` 有 40 个可引用片段。任务及语料由 `internal/ai/eval` 确定性生成。

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

`cmd/agent-retrieval-eval` 对 20 条检索任务调用真实本地 GGUF Embedding、SQLite、USearch 和 Rerank。它将合成片段写入独立的 SQLite `knowledge_chunks` 表，采用桌面端 HNSW 默认参数保存 `.usearch`，关闭并重载两者后才开始查询。默认将每次运行的数据库与索引保留在被 Git 忽略的 `eval/.local/retrieval-*` 中，可用 `-fixture-root` 指定根目录；`-validation` 输出行数、索引重载、自查询和精确余弦 Top10 对照。它尚未调用正式知识库的 FTS5 混合召回、权限过滤或 Chat LLM，因此 Tool Call、引用和 Task Success Rate 仍需完整 Agent 适配器。

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
