// Package inference contains provider-neutral embedding and reranking contracts.
// They intentionally have no dependency on the chat-completions domain.
package inference

import "context"

type ScoredDocument struct {
	Index int     `json:"index"`
	Score float32 `json:"score"`
	Text  string  `json:"text,omitempty"`
}

type EmbeddingProvider interface {
	Embedding(ctx context.Context, text string) ([]float32, error)
}

type RerankProvider interface {
	Rerank(ctx context.Context, query string, docs []string, topN int) ([]ScoredDocument, error)
}

// Provider is convenient when one backend implements both capabilities; callers
// may still depend on either smaller interface above.
type Provider interface {
	EmbeddingProvider
	RerankProvider
}
