# InkFlow

面向组织协作的公文写作与知识库系统。项目保留 Go + Vue 的共享推理、分块、向量检索和工具编排基础设施，并以 `officialdoc` 作为独立业务领域；系统管理、模型配置和部署能力由 `system` 模块统一提供。

## 当前实现

### 组织与系统管理

- [x] 多租户、组织树、公开组织申请、申请审核、成员授权、角色、Casbin 域权限和审计日志。
- [x] 所有者可维护全局用户并分配组织；成员关系按“租户 + 组织 + 用户”保存，并可在成员授权中变更组织与角色。
- [ ] 当前“管理员/所有者”对成员和申请的管理范围仍是租户级；若要让组织所有者只能审核本组织申请，需要补充组织级管理范围校验。
- [x] `sys_menus` 前端菜单配置、父子菜单折叠和角色菜单授权；角色 API 权限与菜单权限分开维护。
- [x] 系统 API 资源可同步到 `sys_apis`，每个接口维护分组、用途说明、菜单关联与公开标识；仅 `owner` 角色由系统维护全部 API 权限，其他角色由管理员或所有者配置。
- [x] 当前用户、当前租户维度的 OpenAI Chat Completions 兼容模型配置；主模型和可选的 OCR 图片语义总结模型均不会向前端返回 API Key 明文。

### 本地与浏览器推理

- [x] `utils/inference` 中的 Embedding、Rerank 和 LLM 调用能力，以及 `utils/vectorstore` 中的 SQLite FTS5、USearch HNSW 和向量 CRUD 基础设施。
- [x] 客户端可选择 Go 后端 llama.cpp 本地推理，或浏览器 WebGPU Embedding/Rerank 推理；前端展示两个模型的独立下载、缓存与进度状态。
- [x] 前端 WebGPU worker 与后端推理 WebSocket 通道已实现。Web 服务器部署时需按 [deploy-manual.md](docs/deploy-manual.md) 为 `/api/system/inference/ws` 配置 WebSocket Upgrade 转发。
- [x] 基于 ONNX Runtime 的 `PP-DocLayout-S` 文档版面检测器：仅识别文字、表格、标题、图表等区域，不执行 OCR 正文转写；支持 Windows 安装包和 Linux 服务器构建。

### 知识库文档导入、索引与证据检索

- [x] 独立领域边界：`model/officialdoc`、`service/officialdoc`、`api/v1/officialdoc` 与 `router/officialdoc`，并已接入数据库迁移和路由装配。
- [x] `KnowledgeDocument`、`KnowledgeChunk`、`KnowledgeImage` 三张模型表，分别保存原始文档、切片和嵌入图片元数据。
- [x] `POST /officialdoc/knowledge-documents/import`：在当前租户、组织成员关系和角色 API 权限校验后导入文件；单文件上限为 200 MB。
- [x] 支持 Markdown、DOCX、XLSX（含多工作表）、PPTX 与带嵌入文本的 PDF。解析结果统一规范化为适合 `utils/chunker` 的 Markdown，表格、标题层级和 UTF-8 边界均有测试覆盖。
- [x] 原始文件和内嵌图片写入私有阿里云 OSS；数据库仅保存按组织隔离的 `object_key`，同一租户和组织不重复导入相同内容。
- [x] 导入时复用 `utils/chunker` 生成带标题路径、章节类型和 token 估算的知识库切片。
- [x] 图片先经本地 `PP-DocLayout-S` 判断是否含有可解析文字或表格；仅命中后才调用用户配置的 OpenAI 兼容图片语义模型补充文字和结构摘要。
- [x] 命中图片语义模型的表格、图表和版面摘要会额外写入 `KnowledgeChunk`，因此可与正文一起被检索和引用；图片原件、摘要和 SHA-256 均可回溯。
- [x] `KnowledgeChunk` 已接入 PostgreSQL `pgvector` 或桌面 SQLite + USearch HNSW 向量索引；桌面 SQLite 同时建立 FTS5 触发器和历史回填，关系库中的切片始终是事实源。
- [x] `POST /officialdoc/knowledge-search` 实现向量召回与词法召回的融合排序，并可选调用 Rerank；任一索引、浏览器推理 worker 或重排模型不可用时会保留另一种召回结果并返回降级提示。
- [x] 已提供知识文档目录、切片预览、删除、重建索引、私有 OSS 短时下载和混合检索前端页面。所有请求均以租户、组织和成员关系过滤，角色还必须显式拥有相应菜单和 API 权限。
- [x] 对没有文字层、且页面以 JPEG 嵌入的扫描 PDF，复用“OCR 图片语义总结模型”逐页转写并建立索引；不引入独立 OCR 二进制或第二套模型配置。未包含可提取页面图片的 PDF 会保留原文件并标记处理失败，等待后续补齐统一的 PDF 页面渲染能力。

### 受控写作工作流

- [x] `DocumentTemplate` 保存组织级 Markdown 模板、变量、用途说明和约束；模板的启用状态由组织内有对应 API 权限的成员维护。
- [x] `WritingTask` 以模板、写作要求与任务约束启动；只允许显式生成 `outline`（大纲）和 `draft`（草稿）两个阶段，避免模型静默覆盖用户内容。
- [x] 每次生成或人工保存都会创建不可变 `DocumentVersion`；生成过程同时固化 `WritingEvidence`，保存文档名、切片标题、正文快照、排序和分数，支持版本级证据回溯。
- [x] `WritingRun` 将一次生成保存为可恢复的 MCP 运行：每一阶段均通过 `Registry → Dispatcher → RunWithTools` 调用受限工具；只注册“检索证据、生成文稿、固化版本”三项工具，不向 MCP 暴露删除能力。外部 stdio MCP 工具可继续通过既有 `RegisterMCPStdioServer` 接入同一 Registry。
- [x] 运行会持久化用户消息、助手步骤、工具轨迹、冻结证据和当前检查点。暂停、失败或服务重启后可从未完成步骤恢复，不会重放已经成功的工具。
- [x] 写作工作台通过 `GET /officialdoc/writing-runs/:id/events` 建立 SSE 连接，实时显示运行状态、多轮消息和工具轨迹；断开后可重新打开任务并从数据库快照继续订阅。
- [x] 已提供模板管理与 MCP 写作工作台：可创建任务、检索组织知识、生成大纲/草稿、暂停/恢复运行、查看版本、查看证据，并将人工修改保存为新版本。
- [ ] 尚未实现版本差异比对、多人审核批注、规则校验、Word/PDF 导出和通用分布式任务队列。

### 已验证的构建与解析能力

- [x] 文档解析器覆盖 Markdown、DOCX、XLSX、PPTX、PDF 的 UTF-8 与 Markdown 兼容性测试；拒绝旧版 Office 二进制格式，避免将不可控格式误作 OOXML 解析。
- [x] Markdown 切片器覆盖标题路径、超长段落、表格分页、代码围栏、UTF-8 边界和语义分割回退。
- [x] Linux `build_priv.sh` 可下载并校验 ONNX Runtime 与 `PP-DocLayout-S`、构建 C++ 桥接库、构建服务端与前端并重启服务；Windows 安装包构建使用 MSVC 生成对应桥接库。

## 下一阶段建议

现在的链路已经是“**导入 → Markdown 规范化 → 切片 → 索引 → 混合检索 → MCP 受控生成 → SSE 运行观测 → 版本回溯**”。下一步建议把现有闭环做成更可靠的生产工作流：

1. **把其余长耗时操作纳入可恢复运行**
   - 文档索引、扫描 PDF 页面渲染和图片语义总结应复用 `WritingRun` 的持久化检查点与 SSE 观测模式，或接入后续通用任务队列。
   - 对模型不可用、向量维度变化和 USearch 索引损坏提供批量重建工具。

2. **补齐公文规则与审核**
   - 为模板补充结构化字段、必填项、字数、落款和敏感词规则；生成后先规则校验，再进入人工审核和修改意见流程。
   - 按版本提供 Markdown 差异、审核状态、审批记录与一键回退，不修改已有版本。

3. **实现可交付文档**
   - 将已审核版本渲染为 DOCX/PDF；保留模板、证据编号和导出快照，确保导出的内容与审计记录一致。

4. **提升资料解析覆盖面**
   - 给扫描 PDF 加页面渲染/OCR 任务；为复杂图表、公式和嵌入对象提供可选专用解析器，避免在普通导入请求中阻塞。

5. **补齐权限与端到端验证**
   - 用不同组织、不同角色验证“只检索/只生成本组织资料”的边界，并给 API 授权、索引降级、证据快照和版本不可变性补充集成测试。

## 运行形态与数据边界

- Web 服务端部署时，组织、权限、审计、模型连接配置及知识库元数据由服务端数据库维护；导入的原始文件和图片存入私有 OSS，下载必须通过后端完成成员鉴权后获得短时签名地址。
- 桌面客户端继续支持本地数据目录（如 Windows `%LOCALAPPDATA%\InkFlow`）和 loopback Gin 服务；桌面安装包可内置本地模型与 ONNX 版面检测器。
- 用户主动配置第三方云端 LLM 或图片语义模型时，相应内容会发送到该用户配置的 OpenAI 兼容服务；InkFlow 不会将其隐式转发到账号服务。
- 跨设备同步与加密备份尚未实现；OSS 仅保存组织知识库对象，不会作为登录后的隐式同步行为。

客户端化的已完成能力与待办事项见 [docs/client-task-list.md](docs/client-task-list.md)。

## Linux 服务器一键部署与 ONNX 文档版面检测

`PP-DocLayout-S` 的 C++ 桥接分为两个平台构建物：Windows 客户端使用 MSVC 构建
`InkFlowLayout.dll`；Linux 服务器使用服务器本机的 `g++` 构建
`libInkFlowLayout.so`。因此不能、也不需要在 Ubuntu 服务器上安装或运行 MSVC。

在服务器的项目目录中，以 root 运行：

```bash
bash ./build_priv.sh document-writing
```

脚本会在拉取最新代码后自动完成以下事项：下载并校验 Linux x86_64 ONNX Runtime 和
PP-DocLayout-S 模型、用 `g++` 编译 Linux 桥接库、把 `libonnxruntime.so*` 与
`libInkFlowLayout.so` 放入 `lib/`、编译后端、发布前端并重启服务。首次执行需要访问
GitHub 和 Hugging Face；缺少 `g++`、`tar` 或 `sha256sum` 时，Ubuntu/Debian 服务器会
自动安装 `build-essential`、`tar`、`coreutils` 和 `libgomp1`。

模型保存在 `ocr/pp_doclayout_s.onnx`，运行库缓存在
`third_party/onnxruntime/linux_amd64`；二者均不提交 Git。后续部署会复用校验通过的
文件，不会重复下载。若需强制重下模型和 Runtime，可在服务器执行：

```bash
bash ./scripts/prepare_onnx_layout.sh --force
bash ./build_priv.sh document-writing
```

## Windows 桌面客户端构建

桌面版使用 Wails 承载 Vue 页面，业务 API、SSE 与前端推理 WebSocket 仍由随机
loopback 端口上的 Gin 服务直接处理。运行数据默认写入
`%LOCALAPPDATA%\InkFlow`，可通过 `INKFLOW_DATA_DIR` 覆盖。

### 构建依赖

构建机需要 Windows x64、PowerShell、Go、Node.js/npm、CMake、Ninja、Visual
Studio 2022 Build Tools（Desktop C++ workload）、Inno Setup 6，以及可运行的
WebView2 Runtime。构建脚本首次运行会下载 ONNX Runtime C++ SDK 和一个约 5 MB 的
量化文档版面模型；不需要 Python、PaddleX 或 PaddleOCR。先确认仓库中的 USearch
静态库存在：

```powershell
.\scripts\setup_usearch.ps1
```

CUDA 包还需要 CUDA Toolkit 13.3（包含 `nvcc.exe`）；Vulkan 包需要 Vulkan SDK
（或 MSYS2 ucrt64 的 `vulkan-devel` 和 `shaderc`）。这些是构建依赖，不会被
`setup_usearch.ps1` 自动安装。

### 三种本地推理安装包

`build_installer.ps1` 会先构建客户端和 llama.cpp 后端，再调用 Inno Setup，输出到
`build\installer\`。安装包名称会明确写出推理来源和运行时依赖：

| 包类型 | 构建命令 | 输出文件名 | 运行依赖 |
| --- | --- | --- | --- |
| CPU 本地推理 | `.\scripts\build_installer.ps1 -Version 0.1.0 -InferenceProvider local -Backend cpu` | `InkFlow-Setup-0.1.0-local-cpu-msvc-bundled-x64.exe` | Windows x64；MSVC 运行库已随包复制，不需要 CUDA/Vulkan |
| CUDA 本地推理 | `.\scripts\build_installer.ps1 -Version 0.1.0 -InferenceProvider local -Backend cuda -CudaVersion 13.3` | `InkFlow-Setup-0.1.0-local-cuda13.3-nvidia-driver-x64.exe` | 支持 CUDA 13.3 的 NVIDIA GPU、显卡驱动，以及可用的 CUDA 13.3 runtime DLL |
| Vulkan 本地推理 | `.\scripts\build_installer.ps1 -Version 0.1.0 -InferenceProvider local -Backend vulkan` | `InkFlow-Setup-0.1.0-local-vulkan-driver-x64.exe` | 支持 Vulkan 的 GPU 和系统 Vulkan 驱动；不需要 CUDA |

CUDA 包名中的 `cuda13.3` 必须与构建时使用的 Toolkit 版本一致。当前脚本会优先
选择 `C:\Program Files\NVIDIA GPU Computing Toolkit\CUDA\v13.3`，并把版本写入
文件名；如果没有传 `-CudaVersion 13.3`，CUDA 安装包会被拒绝生成。CUDA Toolkit
和驱动不是同一个依赖：Toolkit 用于构建，运行机至少要有兼容的 NVIDIA 驱动，且
CUDA runtime DLL 必须能被程序找到。

### 前端 WebGPU 变体（可选）

如果模型推理由浏览器 WebGPU worker 提供，可另外构建：

```powershell
.\scripts\build_installer.ps1 -Version 0.1.0 -InferenceProvider frontend -Backend cpu
```

输出为 `InkFlow-Setup-0.1.0-frontend-webgpu-webview2-x64.exe`。运行机需要
WebView2 Runtime、支持 WebGPU 的显卡驱动和浏览器图形能力；此包不使用本地
llama.cpp 进行 embedding/rerank/chat 推理。

### 安装和模型

安装器默认安装程序到 `%LOCALAPPDATA%\Programs\InkFlow`，卸载时保留
`%LOCALAPPDATA%\InkFlow` 中的 SQLite 数据库、向量索引、日志、备份和模型。安装
过程中可以选择从 Hugging Face、`hf-mirror.com` 或自定义兼容端点下载默认 GGUF
embedding/rerank 模型，也可以稍后手动放入 `%LOCALAPPDATA%\InkFlow\models`。

每个桌面安装包还会携带 ONNX Runtime DLL 和量化的 `PP-DocLayout-S` 模型。它只做
图片版面检测，直接给出文字、表格、标题、图片、公式和图表区域；不转写文字，也不
创建 Python 环境。模型放在程序目录 `ocr\pp_doclayout_s.onnx`，会跟随应用升级。
网络受限时可先执行下面的脚本准备构建资源：

```powershell
.\scripts\prepare_onnx_layout.ps1 -Destination build\package\ocr
```

`utils/ocr/layout` 通过 Windows DLL 调用 MSVC 编译的 C++ ONNX Runtime 桥接，并返回
`HasText`、`HasTable` 和完整区域坐标。模型配置页中的“OCR 图片语义总结模型”只负责对
版面检测结果做可选的 LLM 总结，不替代本地检测器。

如果只想重新编译已有后端，可使用 `-SkipBackendBuild`；如果已有
`build\package\InkFlow.exe` 和 `InkFlow.backend`，可使用 `-SkipClientBuild`。跳过
构建时必须保证 marker 与命令中的 `-Backend` 一致，否则脚本会停止。

### 本地 Rerank 性能测试

测试文件为 [`utils/inference/local_rerank_test.go`](utils/inference/local_rerank_test.go)，
性能 fixture 为 [`docs/rerank性能测试.log`](docs/rerank性能测试.log)。测试会自动从当前
Windows 用户目录查找 `AppData\Local\InkFlow\models\bge-reranker-v2-m3-Q4_K_M.gguf`；
也可以通过 `INKFLOW_RERANK_MODEL` 覆盖模型路径。以下命令均从仓库根目录执行，且需要
先构建对应的 llama.cpp 后端。

先设置三个测试共用的 CGO 参数：

```powershell
$env:CGO_ENABLED = "1"
$repo = (Get-Location).Path
$env:CGO_CFLAGS = "-I$repo\third_party\usearch\windows_amd64"
$env:CGO_LDFLAGS = "-L$repo\third_party\usearch\windows_amd64 -lusearch_c -lstdc++ -static-libgcc -static-libstdc++ -lwinpthread"
$env:INKFLOW_RERANK_INFO_LOG = "$repo\docs\reranktestcuda.log"
```

CPU 版本：

```powershell
$env:PATH = "$repo\llama\cmake-build-cpu\bin;$env:PATH"
go test -v -count=1 -run '^TestLocalProviderRerankFromInfoLog$' ./utils/inference
```

CUDA 版本：

```powershell
$env:PATH = "$repo\llama\cmake-build-cuda\bin;C:\Program Files\NVIDIA GPU Computing Toolkit\CUDA\v13.3\bin;$env:PATH"
go test -v -count=1 -tags inkflow_cuda -run '^TestLocalProviderRerankFromInfoLog$' ./utils/inference
```

Vulkan 版本：

```powershell
$env:PATH = "$repo\llama\cmake-build-vulkan\bin;$env:PATH"
go test -v -count=1 -tags inkflow_vulkan -run '^TestLocalProviderRerankFromInfoLog$' ./utils/inference
```

CUDA 和 Vulkan 命令必须带对应的 `-tags`，否则可能加载到错误的后端 DLL。测试输出会
记录模型加载、每个 rerank batch、推理总耗时、GPU offload 层数和最终 Top-N 结果。

## 部署方式

推荐优先使用 Docker，部署和更新都会轻很多。

### Docker 快捷部署

- 配置文件：`config.docker.yaml`
- 编排文件：`docker-compose.yml`
- 文档：[`docs/deploy-docker.md`](docs/deploy-docker.md)

启动命令：

```bash
docker compose up -d --build
```

更新命令：

```bash
git pull
docker compose up -d --build
```

### 手动部署

手动部署文档已经单独整理，包含：

- 环境准备
- PostgreSQL / pgvector
- 模型下载
- 后端构建
- 前端构建
- 直接启动
- Nginx 反代
- 手动更新

文档入口：[`docs/deploy-manual.md`](docs/deploy-manual.md)

## 项目结构

```text
.
├─ main.go
├─ config.yaml
├─ config.docker.yaml
├─ docker-compose.yml
├─ Dockerfile
├─ web/                  # Vue + Vite 前端
├─ llama/                # llama.cpp 及本地模型相关构建
├─ docs/
│  ├─ deploy-docker.md
│  └─ deploy-manual.md
└─ start.sh / build.sh
```

## 运行说明

- 后端默认端口：`8888`
- Docker 对外默认端口：`8080`
- 健康检查：`/health`

只要 `web/dist/index.html` 存在，后端会自动托管前端静态资源。

## 备注

- Docker 方案默认使用 `pgvector/pgvector` 数据库容器。
- 本地模型默认从 `llama/llama.cpp/models/` 目录挂载。
- 生产环境的数据库密码和 JWT 密钥仅通过未跟踪的环境变量注入。


## OAuth 2.0 登录

后端已内置 Google / GitHub 的 OAuth2 协议默认值，登录后会创建或复用系统用户，并使用同一套 HttpOnly 会话 Cookie。

将 `config.example.yaml` 复制为本机的 `config.yaml` 后，通过环境变量注入 OAuth 配置；不要把任何 OAuth 值写入 Git：

- `INKFLOW_AUTH_OAUTH_GOOGLE_CLIENT_ID` / `INKFLOW_AUTH_OAUTH_GOOGLE_CLIENT_SECRET`
- `INKFLOW_AUTH_OAUTH_GITHUB_CLIENT_ID` / `INKFLOW_AUTH_OAUTH_GITHUB_CLIENT_SECRET`

如需自定义域名，可再覆盖：

```yaml
auth:
  public-base-url: "https://api.example.com"
  frontend-base-url: "https://app.example.com"
```

在 OAuth Provider 控制台配置的回调地址为：

```text
{auth.public-base-url}/auth/oauth/{provider}/callback
```

浏览器跳转 `GET /auth/oauth/{provider}/login` 后，首次登录会自动创建系统账号；组织和角色仍由管理员分配。
