package llm

import (
	"strings"
	"time"
)

func MaxAttempts(maxRetries int) int {
	if maxRetries < 0 {
		maxRetries = 0
	}
	return maxRetries + 1
}

func IsRetryableError(err error) bool {
	if err == nil {
		return false
	}
	message := strings.ToLower(err.Error())
	for _, marker := range []string{
		"invalid_argument", "invalid_request_error", "bad request", "status=400",
		"unauthorized", "forbidden", "status=401", "status=403",
		"quota", "resource_exhausted", "rate limit", "status=429",
		"location is not supported", "requires a model name", "requires a specific model name",
	} {
		if strings.Contains(message, marker) {
			return false
		}
	}
	for _, marker := range []string{
		"upstream_error", "upstream request failed",
		"bad gateway", "gateway time-out", "gateway timeout",
		"service unavailable", "temporarily unavailable",
		"status=502", "status=503", "status=504", " 502 ", " 503 ", " 504 ",
		"connection reset", "connection refused", "connection aborted",
		"unexpected eof", " eof", "timeout", "deadline exceeded",
		"tls handshake timeout", "temporary failure", "empty response from llm",
	} {
		if strings.Contains(message, marker) {
			return true
		}
	}
	return false
}

func RetryDelay(attempt int) time.Duration {
	if attempt < 1 {
		attempt = 1
	}
	if attempt > 3 {
		attempt = 3
	}
	return time.Duration(1<<(attempt-1)) * time.Second
}
