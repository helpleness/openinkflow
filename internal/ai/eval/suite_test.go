package eval

import "testing"

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
