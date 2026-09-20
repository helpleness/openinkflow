package llm

import (
	"InkFlow/config"
	domain "InkFlow/internal/ai/llm"
	"testing"
	"time"
)

func TestRequestContextAppliesPerRequestTimeout(t *testing.T) {
	startedAt := time.Now()
	ctx, cancel := requestContext(GenerateOptions{Timeout: 120 * time.Second})
	defer cancel()
	deadline, ok := ctx.Deadline()
	if !ok {
		t.Fatal("request context has no deadline")
	}
	remaining := deadline.Sub(startedAt)
	if remaining < 119*time.Second || remaining > 121*time.Second {
		t.Fatalf("request timeout = %v, want about 120s", remaining)
	}
}

func TestPrepareRequestUsesExplicitProviderTypeForReasoning(t *testing.T) {
	deepSeek := config.LLM{ProviderType: "deepseek", BaseUrl: "https://example.invalid/v1", ModelDefault: "deepseek-chat"}
	_, request, _, cancel, err := prepareRequest(
		[]Message{{Role: "user", Content: "hello"}},
		GenerateOptions{LLM: &deepSeek, Tools: []domain.ToolDefinition{{Name: "lookup"}}},
	)
	if err != nil {
		t.Fatalf("prepareRequest() error = %v", err)
	}
	defer cancel()
	if request.Reasoning == nil || request.Reasoning.Enabled {
		t.Fatalf("deepseek tool request reasoning = %#v, want disabled", request.Reasoning)
	}

	openAI := config.LLM{ProviderType: "openai", BaseUrl: "https://example.invalid/v1", ModelDefault: "gpt-5"}
	explicit := &domain.Reasoning{Enabled: false}
	_, request, _, cancel, err = prepareRequest([]Message{{Role: "user", Content: "hello"}}, GenerateOptions{LLM: &openAI, Reasoning: explicit})
	if err != nil {
		t.Fatalf("prepareRequest() error = %v", err)
	}
	defer cancel()
	if request.Reasoning != explicit {
		t.Fatalf("explicit reasoning intent was not preserved")
	}
}
