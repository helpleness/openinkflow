# utils/llamacpp

一个最小可用的 `llama.cpp` 推理 Go 封装，提供两种后端：

- **ServerEngine（推荐，跨平台）**：通过 `llama-server` 的 HTTP(OpenAI-compatible)接口调用推理
- **LocalEngine（高性能）**：`windows+cgo` 直连 `llama.cpp` 动态库（当前仓库已配好 Windows 的链接路径）

## 依赖

本仓库已包含 `llama.cpp` 源码（`llama/llama.cpp`）以及一个 Windows CMake 构建产物示例（`llama/cmake-build-release`）。

Go 侧通过 cgo 链接这些库：

- `llama/cmake-build-release/llama.cpp/src/llama.lib`
- `llama/cmake-build-release/llama.cpp/ggml/src/ggml*.lib`

运行时需要 DLL 可被找到（通常把以下目录加到 `PATH`，或把 DLL 复制到你的 Go 可执行文件同目录）：

- `llama/cmake-build-release/bin`（`llama.dll`, `ggml*.dll`）

Linux / macOS 的本地直连（LocalEngine）同理：你需要先在对应平台把 `llama/cmake-build-release` 构建出来（生成 `libllama.so/.dylib` 或静态库）。

## 用法

### 1) 跨平台（推荐）：ServerEngine

只要能运行 `llama-server`（Windows/Linux/macOS 都可），Go 侧完全不需要 cgo。

```go
eng, err := llamacpp.NewServer(llamacpp.Options{
    BaseURL: "http://127.0.0.1:8080",
    Model:   "your-model", // 可选
})
if err != nil { panic(err) }
defer eng.Close()

sess := llamacpp.NewChatSession(eng, llamacpp.Options{
    MaxTokens:     256,
    SystemPrompt:  "请用中文回答",
})
out, err := sess.Send("你是谁？")
```

### 2) Windows + cgo：LocalEngine

```go
eng, err := llamacpp.NewLocal("D:/models/your-model.gguf", llamacpp.Options{
    ContextSize:   4096,
    Threads:       8,
    ThreadsBatch:  8,
    MaxTokens:     512,
    Temperature:   0.7,
    TopP:          0.9,
    FlashAttnAuto: true,
})
if err != nil { panic(err) }
defer eng.Close()

sess := llamacpp.NewChatSession(eng, llamacpp.Options{
    MaxTokens:    256,
    SystemPrompt: "请用中文回答",
})
out, err := sess.Send("你是谁？")
```
