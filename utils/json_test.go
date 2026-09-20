package utils

import (
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

func TestSummarizeUsesJSONAndRuneLimit(t *testing.T) {
	got := Summarize(map[string]string{"title": "你好世界"}, 12)
	want := `{"title":"你好...(truncated)`
	if got != want {
		t.Fatalf("Summarize() = %q, want %q", got, want)
	}
}
