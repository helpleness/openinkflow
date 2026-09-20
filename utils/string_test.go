package utils

import "testing"

func TestTruncateRunesPreservesUnicodeBoundaries(t *testing.T) {
	if got, want := TruncateRunes("  你好🙂世界  ", 3), "你好🙂...(truncated)"; got != want {
		t.Fatalf("TruncateRunes() = %q, want %q", got, want)
	}
}

func TestTruncateRunesKeepsTextForNonPositiveLimit(t *testing.T) {
	if got, want := TruncateRunes("  你好  ", 0), "你好"; got != want {
		t.Fatalf("TruncateRunes() = %q, want %q", got, want)
	}
}
