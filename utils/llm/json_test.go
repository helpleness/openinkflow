package llm

import (
	"strings"
	"testing"
)

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
