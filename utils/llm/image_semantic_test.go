package llm

import (
	"testing"

	"InkFlow/config"
)

func TestNewImageSemanticAnalyzer(t *testing.T) {
	analyzer := NewImageSemanticAnalyzer(config.LLM{
		BaseUrl:      "https://llm.example/v1",
		ApiKey:       "customer-key",
		ModelDefault: "customer-multimodal",
	})
	if analyzer == nil {
		t.Fatal("analyzer is nil")
	}
	if analyzer.model != "customer-multimodal" || analyzer.config.ApiKey != "customer-key" {
		t.Fatalf("unexpected analyzer configuration: %#v", analyzer)
	}
}

func TestNewImageSemanticAnalyzerRequiresEndpointAndModel(t *testing.T) {
	if analyzer := NewImageSemanticAnalyzer(config.LLM{}); analyzer != nil {
		t.Fatal("analyzer should be nil without an OpenAI-compatible semantic model")
	}
}
