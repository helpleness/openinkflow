package inference

import (
	domain "InkFlow/internal/ai/inference"
	"strings"

	"InkFlow/global"

	"go.uber.org/zap"
)

type ScoredDocument = domain.ScoredDocument
type EmbeddingProvider = domain.EmbeddingProvider
type RerankProvider = domain.RerankProvider
type Provider = domain.Provider

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
