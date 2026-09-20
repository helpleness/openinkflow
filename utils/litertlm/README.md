# utils/litertlm

按 `utils/llamacpp` 的调用风格，对 [LiteRT-LM](https://github.com/google-ai-edge/LiteRT-LM) 官方 C API 做的一层 Go cgo 封装。

## 设计目标

- 保留 `Engine` / `Options` / `Message` / `ChatSession` 这套调用习惯
- 直接复用官方 `c/engine.h`
- 优先走 LiteRT-LM 的 `Conversation` API，方便多轮、system prompt、tools、约束解码
- 默认不参与仓库构建，避免在还没放入 LiteRT-LM 源码和产物时影响现有项目

## 构建标签

只有在显式启用 `litertlm` 标签时才会编译真实 cgo 实现：

```bash
go build -tags litertlm ./...
```

未启用 `litertlm` 或未启用 `cgo` 时，会自动走 stub 实现。

## 目录约定

当前封装默认从下面这个位置找官方头文件：

```text
third_party/LiteRT-LM
```

也就是让下面这个文件存在：

```text
third_party/LiteRT-LM/c/engine.h
```

## Windows 链接方式

当前 Windows cgo 实现默认链接下面这个 Bazel DLL import library：

```text
third_party/LiteRT-LM/bazel-bin/c/engine_cpu_dll.if.lib
```

`engine_cpu.lib` 是 MSVC 静态库，Go cgo 默认使用 MinGW `gcc` 时不能直接链接。建议在 LiteRT-LM 的 `c/BUILD` 中给 `:engine_cpu` 增加一个 `linkshared` DLL target，然后构建：

```powershell
cd D:\MYgopro\InkFlow\third_party\LiteRT-LM
bazelisk --output_base=C:\bzl build //c:engine_cpu_dll --config=windows
```

然后在仓库根目录用下面命令验证 Go cgo 包：

```powershell
.\test-litertlm.cmd
```

Linux / macOS 的库名和产物路径需要按实际 Bazel/CMake 输出补 `#cgo LDFLAGS` 或设置 `CGO_LDFLAGS`。

如果运行主程序或同时跑 `llama.cpp` 本地后端，也需要把 MinGW、`llama.cpp` DLL 目录和 LiteRT-LM Windows 预构建 DLL 目录放进运行配置的 `PATH`。仓库提供了两个辅助脚本：

- `dev-env.cmd`：适合在 `cmd` 或批处理里 `call dev-env.cmd` 后继续执行命令。
- `dev-env.ps1`：适合 PowerShell；如果系统执行策略拦截，可使用 `powershell -ExecutionPolicy Bypass -Command ". .\dev-env.ps1; go test -tags litertlm ./utils/litertlm"`。

## 最小用法

```go
eng, err := litertlm.NewLocal("D:/models/gemma-3n.litertlm", litertlm.Options{
    Backend:      "cpu",
    MaxTokens:    512,
    Temperature:  0.7,
    TopP:         0.9,
    SystemPrompt: "请使用中文回答",
})
if err != nil {
    panic(err)
}
defer eng.Close()

sess := litertlm.NewChatSession(eng, litertlm.Options{
    Backend:      "cpu",
    MaxTokens:    512,
    Temperature:  0.7,
    TopP:         0.9,
    SystemPrompt: "请使用中文回答",
})

out, err := sess.Send("你是谁？")
```

## 当前范围

- 已封装：本地引擎创建、多轮对话、system prompt、tools JSON、extra context、约束解码、基础采样参数
- 未封装：Embedding、Rerank、流式回调、benchmark 结果读取、多模态字节输入的 Go 辅助层

说明：`Options.ParallelFileSectionLoading` 字段保留给新版本 LiteRT-LM 使用；当前本地头文件没有暴露对应 C API，所以 Windows cgo 实现暂不调用它。
说明：当前 Windows DLL 没有导出 `litert_lm_set_min_log_level`，因此 `Options.LogLevel` 暂不在 cgo 初始化时调用。
