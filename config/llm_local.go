package config

// LLMLocal holds the local llama.cpp engines and the backend used to build
// the cgo binding. Backend is a build-time choice: cpu, cuda, vulkan, or auto.
type LLMLocal struct {
	Backend   string      `mapstructure:"backend" json:"backend" yaml:"backend"`
	Chat      LocalEngine `mapstructure:"chat" json:"chat" yaml:"chat"`
	Embedding LocalEngine `mapstructure:"embedding" json:"embedding" yaml:"embedding"`
	Rerank    LocalEngine `mapstructure:"rerank" json:"rerank" yaml:"rerank"`
}

// LocalEngine contains settings for one local llama.cpp model.
type LocalEngine struct {
	ModelPath     string `mapstructure:"model-path" json:"model-path" yaml:"model-path"`
	ContextSize   int    `mapstructure:"context-size" json:"context-size" yaml:"context-size"`
	Threads       int    `mapstructure:"threads" json:"threads" yaml:"threads"`
	ThreadsBatch  int    `mapstructure:"threads-batch" json:"threads-batch" yaml:"threads-batch"`
	FlashAttnAuto bool   `mapstructure:"flash-attn-auto" json:"flash-attn-auto" yaml:"flash-attn-auto"`
	// RerankMaxSequences controls the number of independent rerank sequences
	// evaluated by one encoder graph. It does not truncate or skip inputs.
	RerankMaxSequences int `mapstructure:"max-sequences" json:"max-sequences" yaml:"max-sequences"`

	// Hybrid CPU/GPU offload. gpu-layers=0 means CPU only; a negative value
	// means all layers when the selected llama.cpp backend supports offload.
	GPULayers   int       `mapstructure:"gpu-layers" json:"gpu-layers" yaml:"gpu-layers"`
	MainGPU     int       `mapstructure:"main-gpu" json:"main-gpu" yaml:"main-gpu"`
	SplitMode   string    `mapstructure:"split-mode" json:"split-mode" yaml:"split-mode"`
	TensorSplit []float64 `mapstructure:"tensor-split" json:"tensor-split" yaml:"tensor-split"`
}
