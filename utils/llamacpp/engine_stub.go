//go:build !windows && !linux && !darwin && cgo

package llamacpp

import "errors"

type LocalEngine struct{}

func newLocalEngine(modelPath string, opt Options) (Engine, error) {
	return nil, errors.New("llamacpp local backend: not implemented on this platform yet (use ServerEngine or add unix cgo build)")
}

func (e *LocalEngine) Close() error { return nil }

func (e *LocalEngine) Reset() error { return nil }

func (e *LocalEngine) Chat(messages []Message, opt Options) (string, error) {
	return "", errors.New("llamacpp local backend: not implemented on this platform yet")
}

func (e *LocalEngine) Complete(prompt string, opt Options) (string, error) {
	return "", errors.New("llamacpp local backend: not implemented on this platform yet")
}
