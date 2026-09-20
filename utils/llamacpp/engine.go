package llamacpp

import "errors"

// Engine 是对“推理能力”的统一抽象：
// - LocalEngine: cgo 直接调用 llama.cpp（性能最好，但需要本地编译/链接）
// - ServerEngine: 通过 llama-server HTTP 调用（跨平台最稳，推荐默认）
type Engine interface {
	// Chat 以“完整对话 messages”作为输入，返回本轮 assistant 文本。
	// 建议配合 ChatSession 使用，由 session 维护 messages。
	Chat(messages []Message, opt Options) (string, error)

	// Complete 是 Chat 的快捷方式：用 system + 单轮 user prompt。
	Complete(prompt string, opt Options) (string, error)
	// Embedding 输入文本，返回向量浮点数组
	Embedding(text string) ([]float32, error)
	Rerank(query string, documents []string) ([]float32, error)
	// Reset 清空对话态（本地后端会清空 KV cache；HTTP 后端为 no-op）。
	Reset() error

	Close() error
}

// NewServer 创建 HTTP 后端（跨平台）。
func NewServer(opt Options) (*ServerEngine, error) {
	opt.applyDefaults()
	if opt.BaseURL == "" {
		return nil, errors.New("llamacpp: BaseURL is required for ServerEngine")
	}
	return newServerEngine(opt), nil
}

// NewLocal 创建本地 cgo 后端（Windows 支持；其他平台可按需扩展）。
// modelPath: gguf 模型文件路径。
func NewLocal(modelPath string, opt Options) (Engine, error) {
	return newLocalEngine(modelPath, opt)
}
