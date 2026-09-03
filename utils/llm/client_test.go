package llm

import (
	"InkFlow/config"
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

func TestResolveThinkingDisablesDeepSeekV4ToolCalls(t *testing.T) {
	cfg := config.LLM{BaseUrl: "https://api.deepseek.com", ModelDefault: "deepseek-v4-pro"}
	thinking := resolveThinking(cfg, cfg.ModelDefault, GenerateOptions{Tools: []Tool{{Type: "function"}}})
	if thinking == nil || thinking.Type != "disabled" {
		t.Fatalf("tool call thinking = %#v, want disabled", thinking)
	}
}

func TestResolveThinkingDoesNotSendExtensionToOtherProviders(t *testing.T) {
	thinking := resolveThinking(config.LLM{BaseUrl: "https://api.openai.com"}, "gpt-5", GenerateOptions{
		Thinking: &Thinking{Type: "disabled"},
	})
	if thinking != nil {
		t.Fatalf("non-DeepSeek thinking = %#v, want nil", thinking)
	}
}
