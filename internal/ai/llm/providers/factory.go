// Package providers owns the explicit mapping from persisted configuration to
// provider adapters. It never guesses a provider from a model name or URL.
package providers

import (
	"InkFlow/config"
	"InkFlow/internal/ai/llm"
	"InkFlow/internal/ai/llm/provider/openaicompat"
	"fmt"
	"strings"
	"time"
)

type Type string

const (
	TypeOpenAI   Type = "openai"
	TypeDeepSeek Type = "deepseek"
	TypeQwen     Type = "qwen"
)

// NormalizeType provides a stable migration default for existing records. It
// does not inspect BaseURL or model names; users can choose any explicit type.
func NormalizeType(raw string) (Type, error) {
	normalized := Type(strings.ToLower(strings.TrimSpace(raw)))
	switch normalized {
	case TypeOpenAI, TypeDeepSeek, TypeQwen:
		return normalized, nil
	default:
		return "", fmt.Errorf("unsupported LLM provider type %q", raw)
	}
}

// New creates the adapter selected by ProviderType. The compatibility default
// exists solely for migration of earlier URL-only configuration.
func New(cfg config.LLM) (llm.Provider, error) {
	typeName, err := NormalizeType(cfg.ProviderType)
	if err != nil {
		return nil, err
	}
	base := openaicompat.Config{
		Name:         string(typeName),
		BaseURL:      cfg.BaseUrl,
		APIKey:       cfg.ApiKey,
		DefaultModel: cfg.ModelDefault,
		Timeout:      time.Duration(cfg.Timeout) * time.Second,
	}

	switch typeName {
	case TypeOpenAI:
		return openaicompat.NewOpenAIProvider(base)
	//case TypeNewAPI:
	//	return NewNewAPIProvider(base)
	//case TypeDeepSeek:
	//	return NewDeepSeekProvider(base)
	//case TypeQwen:
	//	return NewQwenProvider(base)
	//case TypeOpenAICompatible:
	//	return NewOpenAICompatibleProvider(base)
	default:
		return nil, fmt.Errorf("unsupported LLM provider type %q", typeName)
	}
}
