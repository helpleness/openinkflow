package llm

import (
	"strings"
	"testing"
)

func TestExtractJSONKeepsOnlyFirstPayload(t *testing.T) {
	got := ExtractJSON("结果如下：\n{\"facts\":[{\"summary\":\"括号 } 在字符串里\"}]}\n以上是结果。")
	want := "{\"facts\":[{\"summary\":\"括号 } 在字符串里\"}]}"
	if got != want {
		t.Fatalf("ExtractJSON() = %q, want %q", got, want)
	}
}

func TestExtractJSONReadsFencedArray(t *testing.T) {
	got := ExtractJSON("```json\n[{\"label\":\"A\"}]\n```\n额外说明")
	want := "[{\"label\":\"A\"}]"
	if got != want {
		t.Fatalf("ExtractJSON() = %q, want %q", got, want)
	}
}

func TestRepairJSONClosesIncompletePayload(t *testing.T) {
	got := RepairJSON("{\"facts\":[{\"summary\":\"ok\"}")
	want := "{\"facts\":[{\"summary\":\"ok\"}]}"
	if got != want {
		t.Fatalf("RepairJSON() = %q, want %q", got, want)
	}
}

func TestDecodeLLMJSONResponseExplainsHTMLResponse(t *testing.T) {
	var target map[string]any
	err := decodeLLMJSONResponse(
		[]byte("<!doctype html><title>Bad gateway</title>"),
		200,
		"text/html",
		"https://relay.example.com/chat/completions",
		&target,
	)
	if err == nil {
		t.Fatal("decodeLLMJSONResponse() error = nil, want non-JSON response error")
	}
	for _, want := range []string{"status=200", "text/html", "relay.example.com/chat/completions", "Bad gateway"} {
		if !strings.Contains(err.Error(), want) {
			t.Fatalf("decodeLLMJSONResponse() error = %q, want it to contain %q", err, want)
		}
	}
}
