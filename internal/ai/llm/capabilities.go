package llm

type Capabilities struct {
	Streaming         bool `json:"streaming"`
	ToolCalling       bool `json:"tool_calling"`
	Reasoning         bool `json:"reasoning"`
	TopK              bool `json:"top_k"`
	StructuredOutputs bool `json:"structured_outputs"`
	Vision            bool `json:"vision"`
}
