//go:build !cgo

package llamacpp

import "errors"

type LocalEngine struct{}

func newLocalEngine(modelPath string, opt Options) (Engine, error) {
	return nil, errors.New("llamacpp local backend: requires cgo (use ServerEngine for cross-platform)")
}

func (e *LocalEngine) Close() error { return nil }

func (e *LocalEngine) Reset() error { return nil }

func (e *LocalEngine) Chat(messages []Message, opt Options) (string, error) {
	return "", errors.New("llamacpp local backend: requires cgo")
}

func (e *LocalEngine) Complete(prompt string, opt Options) (string, error) {
	return "", errors.New("llamacpp local backend: requires cgo")
}
