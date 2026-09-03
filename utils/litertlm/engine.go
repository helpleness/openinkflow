package litertlm

import "errors"

// Engine mirrors the shape used by utils/llamacpp so callers can switch
// backends with minimal changes.
type Engine interface {
	Chat(messages []Message, opt Options) (string, error)
	Complete(prompt string, opt Options) (string, error)
	Embedding(text string) ([]float32, error)
	Rerank(query string, documents []string) ([]float32, error)
	Reset() error
	Close() error
}

func NewLocal(modelPath string, opt Options) (Engine, error) {
	if modelPath == "" {
		return nil, errors.New("litertlm: modelPath is required")
	}
	return newLocalEngine(modelPath, opt)
}
