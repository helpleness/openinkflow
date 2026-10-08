package eval

import "testing"

func TestScoreUsesExplicitEvidenceAndJudge(t *testing.T) {
	passed := true
	tasks := []Task{{ID: "a", SuiteVersion: SuiteVersion, RelevantChunkIDs: []string{"c1", "c2"}, ExpectedTools: []string{"knowledge.search"}}}
	report, err := Score(tasks, []Result{{TaskID: "a", RetrievedChunkIDs: []string{"c1", "miss", "c2"}, RerankedChunkIDs: []string{"miss", "c2"}, ToolCalls: []ToolCall{{Name: "knowledge.search", Success: true}}, CitationIDs: []string{"c1", "wrong"}, TaskPassed: &passed}}, 1, 2, 3)
	if err != nil {
		t.Fatal(err)
	}
	if got := *report.RetrievalRecallAtK[1].Value; got != .5 {
		t.Fatalf("Recall@1 = %v, want .5", got)
	}
	if got := *report.RetrievalRecallAtK[3].Value; got != 1 {
		t.Fatalf("Recall@3 = %v, want 1", got)
	}
	if got := *report.RerankHitRateAtK[1].Value; got != 0 {
		t.Fatalf("rerank hit@1 = %v, want 0", got)
	}
	if got := *report.RerankHitRateAtK[2].Value; got != 1 {
		t.Fatalf("rerank hit@2 = %v, want 1", got)
	}
	if got := *report.ToolCallSuccessRate.Value; got != 1 {
		t.Fatalf("tool rate = %v, want 1", got)
	}
	if got := *report.CitationAccuracy.Value; got != .5 {
		t.Fatalf("citation accuracy = %v, want .5", got)
	}
	if got := *report.TaskSuccessRate.Value; got != 1 {
		t.Fatalf("task rate = %v, want 1", got)
	}
}

func TestScoreDoesNotInventMetricsForMissingEvidence(t *testing.T) {
	report, err := Score([]Task{{ID: "a", SuiteVersion: SuiteVersion, RelevantChunkIDs: []string{"c1"}}}, []Result{{TaskID: "a"}})
	if err != nil {
		t.Fatal(err)
	}
	if report.RetrievalRecallAtK[5].Value != nil {
		t.Fatal("empty retrieval must be N/A")
	}
	if report.TaskSuccessRate.Value != nil {
		t.Fatal("unjudged result must be N/A")
	}
}

func TestScoreCountsExecutedEmptyRetrievalAsMiss(t *testing.T) {
	tasks := []Task{{ID: "a", SuiteVersion: SuiteVersion, RelevantChunkIDs: []string{"c1"}}}
	report, err := Score(tasks, []Result{{TaskID: "a", RetrievedChunkIDs: []string{}, RerankedChunkIDs: []string{}}}, 5)
	if err != nil {
		t.Fatal(err)
	}
	if rate := report.RetrievalRecallAtK[5]; rate.Value == nil || *rate.Value != 0 || rate.Denominator != 1 {
		t.Fatalf("empty retrieval = %+v, want 0/1", rate)
	}
	if rate := report.RerankHitRateAtK[5]; rate.Value == nil || *rate.Value != 0 || rate.Denominator != 1 {
		t.Fatalf("empty rerank = %+v, want 0/1", rate)
	}
}
