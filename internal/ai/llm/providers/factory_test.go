package providers

import (
	"InkFlow/config"
	domain "InkFlow/internal/ai/llm"
	"context"
	"encoding/json"
	"net/http"
	"net/http/httptest"
	"testing"
)

func TestDeepSeekProviderOwnsThinkingWireExtension(t *testing.T) {
	server := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		if r.URL.Path != "/v1/chat/completions" {
			t.Fatalf("request path = %q", r.URL.Path)
		}
		var body map[string]any
		if err := json.NewDecoder(r.Body).Decode(&body); err != nil {
			t.Fatalf("decode request: %v", err)
		}
		thinking, ok := body["thinking"].(map[string]any)
		if !ok || thinking["type"] != "disabled" {
			t.Fatalf("thinking extension = %#v, want disabled", body["thinking"])
		}
		tools, ok := body["tools"].([]any)
		if !ok || len(tools) != 1 {
			t.Fatalf("tools = %#v", body["tools"])
		}
		w.Header().Set("Content-Type", "application/json")
		_, _ = w.Write([]byte(`{"id":"chatcmpl_1","object":"chat.completion","created":1,"model":"deepseek-chat","choices":[{"index":0,"message":{"role":"assistant","content":"","tool_calls":[{"id":"call_1","type":"function","function":{"name":"lookup","arguments":"{\"query\":\"InkFlow\"}"}}]},"finish_reason":"tool_calls"}],"usage":{"prompt_tokens":3,"completion_tokens":2,"total_tokens":5}}`))
	}))
	defer server.Close()

	provider, err := New(config.LLM{ProviderType: string(TypeDeepSeek), BaseUrl: server.URL + "/v1", ModelDefault: "deepseek-chat"})
	if err != nil {
		t.Fatalf("New() error = %v", err)
	}
	temperature, maxTokens := 0.0, 64
	response, err := provider.Chat(context.Background(), domain.ChatRequest{
		Model:       "deepseek-chat",
		Temperature: &temperature,
		MaxTokens:   &maxTokens,
		Reasoning:   &domain.Reasoning{Enabled: false},
		Messages:    []domain.Message{{Role: domain.RoleUser, Content: "search"}},
		Tools:       []domain.ToolDefinition{{Name: "lookup", InputSchema: map[string]any{"type": "object"}}},
	})
	if err != nil {
		t.Fatalf("Chat() error = %v", err)
	}
	if len(response.Message.ToolCalls) != 1 || response.Message.ToolCalls[0].Name != "lookup" {
		t.Fatalf("tool calls = %#v", response.Message.ToolCalls)
	}
	if response.Usage.TotalTokens != 5 {
		t.Fatalf("usage = %#v", response.Usage)
	}
}

func TestProviderTypeDoesNotInferFromURL(t *testing.T) {
	got, err := NormalizeType("")
	if err != nil {
		t.Fatalf("NormalizeType(empty) = %q, %v", got, err)
	}
	if _, err := NormalizeType("https://api.deepseek.com"); err == nil {
		t.Fatal("NormalizeType accepted a URL as a provider type")
	}
}
