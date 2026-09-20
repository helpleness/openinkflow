package llamacpp

import "time"

// RerankBatchStats describes one llama_encode call made by the local reranker.
type RerankBatchStats struct {
	BatchIndex        int
	DocumentCount     int
	TokenCount        int
	MaxSequenceLength int
	EncodeDuration    time.Duration
}

type RerankBatchObserver func(RerankBatchStats)

// Options 同时服务于两类后端：
// - Local（cgo 直连 llama.cpp）
// - Server（HTTP 调用 llama-server）
//
// 未使用的字段会被忽略。
type Options struct {
	// -------- 通用生成参数 --------
	ContextSize  int
	Threads      int
	ThreadsBatch int
	MaxTokens    int
	Temperature  float32
	TopP         float32
	MinKeep      int
	Seed         int64

	// -------- 本地推理参数（Local）--------
	FlashAttnAuto bool
	// DisableFlashAttn disables the fused FlashAttention path. This is useful
	// for shape-specific benchmarking; the default remains backend-selected.
	DisableFlashAttn bool
	// GPULayers controls CPU/GPU hybrid offload in llama.cpp. Zero forces
	// CPU-only execution; a negative value means all layers.
	GPULayers int
	// MainGPU selects the device used when SplitMode is "none".
	MainGPU int
	// SplitMode is one of "none", "layer", or "row".
	SplitMode string
	// TensorSplit contains per-device split proportions for multi-GPU setups.
	TensorSplit []float32

	// -------- HTTP 推理参数（Server）--------
	BaseURL      string // e.g. http://127.0.0.1:8080
	Model        string // llama-server 可选 model 字段
	SystemPrompt string
	BatchSize    int
	// PhysicalBatchSize is the maximum number of tokens evaluated in one
	// backend batch. Encoder/reranker graphs do not support micro-batching, so
	// it should normally match BatchSize for the local rerank engine.
	PhysicalBatchSize int
	// RerankMaxSequences controls the number of independent query-document
	// sequences that may share one rerank encode. It is separate from
	// ContextSize: encoder context is per sequence, while BatchSize is the
	// aggregate token capacity across sequences.
	RerankMaxSequences int
	// RerankMaxTokensPerSequence optionally truncates long query-document
	// sequences before encoding. Zero preserves the full tokenized input.
	RerankMaxTokensPerSequence int
	IsEmbedding                bool
	// IsRerank enables rank pooling and multi-sequence batching for BERT rerank models.
	IsRerank bool
	// OnRerankBatch is called after each local rerank llama_encode call.
	// It is primarily useful for profiling and test diagnostics.
	OnRerankBatch RerankBatchObserver
}

const (
	// RerankBatchTokens is the default logical and physical token capacity for
	// the local encoder. It lets a typical multi-document rerank request fit in
	// one graph instead of being split into many small graphs.
	RerankBatchTokens = 8192

	// maxRerankBatchSequences bounds the number of independent query-document
	// sequences in one graph. The token budget remains the final limiter, so
	// long documents are still split safely.
	maxRerankBatchSequences = 16
)

type Message struct {
	Role    string `json:"role"`
	Content string `json:"content"`
}

func (o *Options) applyDefaults() {
	if o.ContextSize <= 0 {
		o.ContextSize = 2048
	}
	if o.IsRerank && o.ContextSize < RerankBatchTokens {
		// llama.cpp reserves the encoder graph using n_ctx. Keep the sequence
		// context at least as large as the aggregate rerank batch so increasing
		// n_batch/n_ubatch cannot outgrow the reserved graph.
		o.ContextSize = RerankBatchTokens
	}
	if o.IsRerank && o.RerankMaxSequences <= 0 {
		o.RerankMaxSequences = maxRerankBatchSequences
	}
	if o.Threads <= 0 {
		o.Threads = 6
	}
	if o.ThreadsBatch <= 0 {
		o.ThreadsBatch = o.Threads
	}
	if o.MaxTokens <= 0 {
		o.MaxTokens = 1024
	}
	if o.TopP <= 0 {
		o.TopP = 0.9
	}
	if o.MinKeep <= 0 {
		o.MinKeep = 1
	}
	if o.Temperature <= 0 {
		o.Temperature = 0.7
	}
	if o.SystemPrompt == "" {
		o.SystemPrompt = "You are a helpful assistant."
	}
	if o.SplitMode == "" {
		o.SplitMode = "none"
	}
}

func (o Options) resolvedBatchSizes() (batchSize int, ubatchSize int) {
	if o.ContextSize <= 0 {
		o.ContextSize = 2048
	}
	if o.IsEmbedding {
		return o.ContextSize, o.ContextSize
	}
	if o.IsRerank {
		batchSize = o.BatchSize
		if batchSize <= 0 {
			batchSize = RerankBatchTokens
		}

		ubatchSize = o.PhysicalBatchSize
		if ubatchSize <= 0 {
			ubatchSize = batchSize
		}
		if ubatchSize > batchSize {
			ubatchSize = batchSize
		}
		return batchSize, ubatchSize
	}

	batchSize = o.BatchSize
	if batchSize <= 0 {
		batchSize = 2048
	}
	if batchSize > o.ContextSize {
		batchSize = o.ContextSize
	}
	if batchSize <= 0 {
		batchSize = 1
	}
	return batchSize, batchSize
}
