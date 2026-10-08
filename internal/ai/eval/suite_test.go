package eval

import (
	"strings"
	"testing"
)

func TestBuildSuiteHasBalancedPublicOfficeCoverage(t *testing.T) {
	tasks := BuildSuite()
	if len(tasks) != 120 {
		t.Fatalf("task count = %d, want 120", len(tasks))
	}
	counts := map[string]int{}
	seen := map[string]bool{}
	for _, task := range tasks {
		if seen[task.ID] {
			t.Fatalf("duplicate task ID %q", task.ID)
		}
		seen[task.ID] = true
		counts[task.Kind]++
		if len(task.RelevantChunkIDs) != 2 {
			t.Fatalf("%s relevant chunks = %d, want 2", task.ID, len(task.RelevantChunkIDs))
		}
	}
	for _, kind := range []string{KindRetrieval, KindSummary, KindDraft, KindRewrite, KindToolCall, KindCitation} {
		if counts[kind] != 20 {
			t.Fatalf("%s tasks = %d, want 20", kind, counts[kind])
		}
	}
	if corpus := Corpus(); len(corpus) != 40 {
		t.Fatalf("corpus chunks = %d, want 40", len(corpus))
	}
}

func TestBuildSuiteKeepsToolPolicyOutOfUserQuestion(t *testing.T) {
	var retrieval, toolCall Task
	for _, task := range BuildSuite() {
		switch task.ID {
		case "retrieval-training":
			retrieval = task
		case "tool_call-training":
			toolCall = task
		}
	}
	if retrieval.UserPrompt != "业务培训的办理要求？" || toolCall.UserPrompt != retrieval.UserPrompt {
		t.Fatalf("retrieval and tool-call user questions differ: %q, %q", retrieval.UserPrompt, toolCall.UserPrompt)
	}
	if strings.Contains(toolCall.UserPrompt, "knowledge.search") || strings.Contains(toolCall.UserPrompt, "先查询") {
		t.Fatalf("tool policy leaked into user question: %q", toolCall.UserPrompt)
	}
	if !strings.Contains(toolCall.SystemInstruction, "knowledge.search") {
		t.Fatalf("tool requirement missing from system instruction: %q", toolCall.SystemInstruction)
	}
}
