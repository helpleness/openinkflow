package inference

import (
	"context"
	"strings"

	"InkFlow/global"

	"go.uber.org/zap"
)

type ScoredDocument struct {
	Index int     `json:"index"`
	Score float32 `json:"score"`
	Text  string  `json:"text,omitempty"`
}

type Provider interface {
	Embedding(ctx context.Context, text string) ([]float32, error)
	Rerank(ctx context.Context, query string, docs []string, topN int) ([]ScoredDocument, error)
}

func ActiveProvider() Provider {
	provider := strings.ToLower(strings.TrimSpace(global.GVA_CONFIG.LLM.InferenceProvider))
	switch provider {
	case "frontend":
		return FrontendProvider{}
	case "", "local":
		return LocalProvider{}
	default:
		if global.GVA_LOG != nil {
			global.GVA_LOG.Warn("unknown inference provider, falling back to local", zap.String("provider", provider))
		}
		return LocalProvider{}
	}
}
