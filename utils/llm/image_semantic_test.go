package llm

import (
	"strings"
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

func TestNormalizeImageKnowledgePreservesModelOutput(t *testing.T) {
	tests := []struct {
		name    string
		content string
		want    string
	}{
		{
			name:    "transcription",
			content: "2025 年重点项目进度表\n完成率：82%",
			want:    "2025 年重点项目进度表\n完成率：82%",
		},
		{
			name:    "chart summary",
			content: "图表展示 2023—2025 年项目完成率持续上升；2025 年为 82%。",
			want:    "图表展示 2023—2025 年项目完成率持续上升；2025 年为 82%。",
		},
		{
			name:    "json is ordinary text",
			content: `{"text":"模型未遵守提示词时也保留原始内容"}`,
			want:    `{"text":"模型未遵守提示词时也保留原始内容"}`,
		},
	}
	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			got, err := normalizeImageKnowledge(tt.content)
			if err != nil {
				t.Fatalf("normalizeImageKnowledge() error = %v", err)
			}
			if got != tt.want {
				t.Fatalf("normalizeImageKnowledge() = %q, want %q", got, tt.want)
			}
		})
	}
}

func TestNormalizeImageKnowledgeRejectsEmptyContent(t *testing.T) {
	_, err := normalizeImageKnowledge(" \n \t ")
	if err == nil || !strings.Contains(err.Error(), "empty content") {
		t.Fatalf("normalizeImageKnowledge() error = %v, want empty-content error", err)
	}
}

func TestNewImageSemanticAnalyzerRequiresEndpointAndModel(t *testing.T) {
	if analyzer := NewImageSemanticAnalyzer(config.LLM{}); analyzer != nil {
		t.Fatal("analyzer should be nil without an OpenAI-compatible semantic model")
	}
}
