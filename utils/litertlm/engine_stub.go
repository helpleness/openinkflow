//go:build litertlm && cgo && !windows && !linux && !darwin

package litertlm

import "errors"

type LocalEngine struct{}

func newLocalEngine(modelPath string, opt Options) (Engine, error) {
	return nil, errors.New("litertlm local backend: not implemented on this platform yet")
}

func (e *LocalEngine) Close() error { return nil }

func (e *LocalEngine) Reset() error { return nil }

func (e *LocalEngine) Chat(messages []Message, opt Options) (string, error) {
	return "", errors.New("litertlm local backend: not implemented on this platform yet")
}

func (e *LocalEngine) Complete(prompt string, opt Options) (string, error) {
	return "", errors.New("litertlm local backend: not implemented on this platform yet")
}

func (e *LocalEngine) Embedding(text string) ([]float32, error) {
	return nil, errors.New("litertlm local backend: embedding is not implemented")
}

func (e *LocalEngine) Rerank(query string, documents []string) ([]float32, error) {
	return nil, errors.New("litertlm local backend: rerank is not implemented")
}
