package initialize

import (
	"InkFlow/config"
	"InkFlow/global"
	"InkFlow/utils/llamacpp"
	"InkFlow/utils/llmclient"
	"fmt"
	"os"
	"runtime"
	"strings"

	"go.uber.org/zap"
)

func maxInt(a, b int) int {
	if a > b {
		return a
	}
	return b
}

func tensorSplit32(values []float64) []float32 {
	if len(values) == 0 {
		return nil
	}
	out := make([]float32, 0, len(values))
	for _, value := range values {
		out = append(out, float32(value))
	}
	return out
}

func localThreads(configured int) int {
	if configured > 0 {
		return configured
	}
	if n := runtime.NumCPU(); n > 0 {
		return n
	}
	return 8
}

func localOptions(cfg config.LocalEngine) llamacpp.Options {
	threads := localThreads(cfg.Threads)
	threadsBatch := maxInt(cfg.ThreadsBatch, threads)
	return llamacpp.Options{
		ContextSize:   cfg.ContextSize,
		Threads:       threads,
		ThreadsBatch:  threadsBatch,
		FlashAttnAuto: cfg.FlashAttnAuto,
		GPULayers:     cfg.GPULayers,
		MainGPU:       cfg.MainGPU,
		SplitMode:     cfg.SplitMode,
		TensorSplit:   tensorSplit32(cfg.TensorSplit),
	}
}

func SetupLLMClient() {
	llmclient.Apply(global.GVA_CONFIG.LLM)
}

// InitializeLLM initializes the chat engine. The local backend is selected at
// compile time by llama/build_windows_backend.ps1; gpu-layers selects CPU or
// GPU execution at model-load time.
func InitializeLLM() {
	cfg := global.GVA_CONFIG.LLMLocal.Chat
	if strings.TrimSpace(cfg.ModelPath) == "" {
		fmt.Println("未配置本地 Chat 模型路径 (llm-local.chat.model-path)，跳过本地引擎初始化")
		return
	}

	opts := localOptions(cfg)
	opts.BatchSize = 2048
	global.GVA_LLM_LOCAL = newLocalOrPanic(cfg.ModelPath, opts, "本地大模型")
	logLocalEngineReady("chat", cfg)
	fmt.Printf("本地大模型加载成功: %s (backend=%s, gpu-layers=%d)\n", cfg.ModelPath, configuredBackend(), cfg.GPULayers)
}

// InitRerankEngine initializes the independent local reranker.
func InitRerankEngine() {
	provider := strings.ToLower(strings.TrimSpace(global.GVA_CONFIG.LLM.InferenceProvider))
	if provider == "" || provider == "frontend" {
		fmt.Println("Rerank 使用前端 WebGPU worker，跳过后端本地 Rerank 模型初始化")
		return
	}

	cfg := global.GVA_CONFIG.LLMLocal.Rerank
	if strings.TrimSpace(cfg.ModelPath) == "" {
		fmt.Println("未配置本地 Rerank 模型路径，跳过初始化")
		return
	}
	if _, err := os.Stat(cfg.ModelPath); err != nil {
		fmt.Printf("本地 Rerank 模型不可用 (%s)，跳过初始化: %v\n", cfg.ModelPath, err)
		return
	}

	opts := localOptions(cfg)
	opts.IsRerank = true
	// Rerank uses the encoder graph in one large batch. Keep both logical and
	// physical capacities at 8192; engine_* sets n_ctx_seq to this value.
	opts.BatchSize = llamacpp.RerankBatchTokens
	opts.PhysicalBatchSize = llamacpp.RerankBatchTokens
	opts.RerankMaxSequences = cfg.RerankMaxSequences
	global.GVA_LLM_RERANK = newLocalOrPanic(cfg.ModelPath, opts, "本地 Rerank 模型")
	logLocalEngineReady("rerank", cfg)
	fmt.Printf("本地 Rerank 模型加载成功: %s (backend=%s, gpu-layers=%d, n_ctx_seq=8192)\n", cfg.ModelPath, configuredBackend(), cfg.GPULayers)
}

// InitLocalEmbeddingEngine initializes the local embedding model when the
// inference provider is not delegated to the browser worker.
func InitLocalEmbeddingEngine() {
	provider := strings.ToLower(strings.TrimSpace(global.GVA_CONFIG.LLM.InferenceProvider))
	if provider == "" || provider == "frontend" {
		fmt.Println("Embedding 使用前端 WebGPU worker，跳过后端本地 Embedding 模型初始化")
		return
	}

	cfg := global.GVA_CONFIG.LLMLocal.Embedding
	if strings.TrimSpace(cfg.ModelPath) == "" {
		fmt.Println("未配置本地 Embedding 模型路径，跳过初始化")
		return
	}
	if _, err := os.Stat(cfg.ModelPath); err != nil {
		fmt.Printf("本地 Embedding 模型不可用 (%s)，跳过初始化: %v\n", cfg.ModelPath, err)
		return
	}

	opts := localOptions(cfg)
	opts.IsEmbedding = true
	opts.BatchSize = cfg.ContextSize
	global.GVA_LLM_EMBEDDING = newLocalOrPanic(cfg.ModelPath, opts, "本地 Embedding 模型")
	logLocalEngineReady("embedding", cfg)
	fmt.Printf("本地 Embedding 模型加载成功: %s (backend=%s, gpu-layers=%d)\n", cfg.ModelPath, configuredBackend(), cfg.GPULayers)
}

func logLocalEngineReady(name string, cfg config.LocalEngine) {
	if global.GVA_LOG == nil {
		return
	}
	global.GVA_LOG.Info("本地推理引擎已加载",
		zap.String("engine", name),
		zap.String("backend", configuredBackend()),
		zap.Int("gpu_layers", cfg.GPULayers),
		zap.Int("main_gpu", cfg.MainGPU),
		zap.String("split_mode", cfg.SplitMode),
		zap.Int("context_size", cfg.ContextSize),
		zap.Int("threads", cfg.Threads),
		zap.Int("threads_batch", cfg.ThreadsBatch),
		zap.Bool("gpu_offload_requested", cfg.GPULayers != 0),
	)
}

func newLocalOrPanic(modelPath string, opts llamacpp.Options, label string) llamacpp.Engine {
	engine, err := llamacpp.NewLocal(modelPath, opts)
	if err != nil {
		panic(fmt.Sprintf("初始化%s失败: %v", label, err))
	}
	return engine
}

func configuredBackend() string {
	backend := strings.ToLower(strings.TrimSpace(global.GVA_CONFIG.LLMLocal.Backend))
	if backend == "" {
		return "cpu"
	}
	return backend
}
