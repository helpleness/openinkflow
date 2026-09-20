package llm

import (
	"errors"
	"strings"
	"testing"
	"time"
)

func TestIsRetryableError(t *testing.T) {
	tests := []struct {
		message string
		want    bool
	}{
		{`LLM API Error: {"error":{"message":"Upstream request failed","type":"upstream_error"}}`, true},
		{"504 Gateway Time-out", true},
		{"context deadline exceeded", true},
		{"LLM API Error: 429 quota exceeded RESOURCE_EXHAUSTED", false},
		{"INVALID_ARGUMENT: tool name is invalid", false},
		{"User location is not supported for the API use", false},
	}
	for _, tt := range tests {
		if got := IsRetryableError(errors.New(tt.message)); got != tt.want {
			t.Errorf("IsRetryableError(%q) = %v, want %v", tt.message, got, tt.want)
		}
	}
}

func TestRetryDelay(t *testing.T) {
	if got := RetryDelay(1); got != time.Second {
		t.Fatalf("RetryDelay(1) = %v", got)
	}
	if got := RetryDelay(2); got != 2*time.Second {
		t.Fatalf("RetryDelay(2) = %v", got)
	}
}

func TestMaxAttemptsCountsRetriesAfterInitialRequest(t *testing.T) {
	if got := MaxAttempts(5); got != 6 {
		t.Fatalf("MaxAttempts(5) = %d, want 6", got)
	}
}

func TestMaxAttemptsNeverDropsInitialRequest(t *testing.T) {
	if got := MaxAttempts(-1); got != 1 {
		t.Fatalf("MaxAttempts(-1) = %d, want 1", got)
	}
}

func TestOutputLimitErrorIsTyped(t *testing.T) {
	err := &OutputLimitError{ToolCall: true, Partial: "partial markdown"}
	if !IsOutputLimitError(err) {
		t.Fatal("typed output-limit error was not recognized")
	}
	if IsOutputLimitError(errors.New(err.Error())) {
		t.Fatal("plain text error was incorrectly recognized as a typed output-limit error")
	}
	if strings.Contains(err.Error(), "正文") || strings.Contains(err.Error(), "工具参数达到") {
		t.Fatalf("output-limit error still gives the misleading old diagnosis: %s", err)
	}
	if got := OutputLimitPartial(err); got != "partial markdown" {
		t.Fatalf("OutputLimitPartial() = %q, want preserved partial output", got)
	}
}
