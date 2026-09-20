package litertlm

// Options keeps the calling style close to utils/llamacpp while exposing the
// LiteRT-LM knobs that map cleanly to the official C API.
type Options struct {
	// Session generation options.
	MaxTokens   int
	SamplerType string
	TopK        int
	TopP        float32
	Temperature float32
	Seed        int32

	// Engine options.
	Backend                    string
	VisionBackend              string
	AudioBackend               string
	MaxNumTokens               int
	ParallelFileSectionLoading *bool
	CacheDir                   string
	ActivationDataType         int
	PrefillChunkSize           int
	EnableBenchmark            bool
	BenchmarkPrefillTokens     int
	BenchmarkDecodeTokens      int
	LogLevel                   int

	// Conversation options.
	SystemPrompt              string
	ToolsJSON                 string
	EnableConstrainedDecoding bool
	ExtraContextJSON          string
	VisualTokenBudget         int
}

type Message struct {
	Role    string `json:"role"`
	Content any    `json:"content"`
}

type Part struct {
	Type string `json:"type"`
	Text string `json:"text,omitempty"`
	Path string `json:"path,omitempty"`
	Blob string `json:"blob,omitempty"`
}

func (o *Options) applyDefaults() {
	if o.MaxTokens <= 0 {
		o.MaxTokens = 1024
	}
	if o.TopK <= 0 {
		o.TopK = 40
	}
	if o.TopP <= 0 {
		o.TopP = 0.9
	}
	if o.Temperature <= 0 {
		o.Temperature = 0.7
	}
	if o.SamplerType == "" {
		o.SamplerType = "top_p"
	}
	if o.Backend == "" {
		o.Backend = "cpu"
	}
	if o.LogLevel < 0 {
		o.LogLevel = 0
	}
}
