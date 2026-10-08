package eval

import (
	"fmt"
	"math"
	"sort"
)

// ToolCall records one attempted call made by an evaluated agent.
type ToolCall struct {
	Name    string `json:"name"`
	Success bool   `json:"success"`
}

// Result is emitted by an adapter around a real agent. TaskPassed is optional:
// it must be set only after a deterministic or human/LLM judge has evaluated
// the full answer. The scorer never pretends an unjudged answer has passed.
type Result struct {
	TaskID            string     `json:"task_id"`
	RetrievedChunkIDs []string   `json:"retrieved_chunk_ids,omitempty"`
	RerankedChunkIDs  []string   `json:"reranked_chunk_ids,omitempty"`
	ToolCalls         []ToolCall `json:"tool_calls,omitempty"`
	CitationIDs       []string   `json:"citation_ids,omitempty"`
	TaskPassed        *bool      `json:"task_passed,omitempty"`
}

type Rate struct {
	Value       *float64 `json:"value,omitempty"`
	Numerator   int      `json:"numerator"`
	Denominator int      `json:"denominator"`
}

// Report is a transparent metric report. A nil Value means that no completed
// result was supplied for that metric; it is intentionally not rendered as 0.
type Report struct {
	SuiteVersion         string       `json:"suite_version"`
	TaskCount            int          `json:"task_count"`
	SubmittedResultCount int          `json:"submitted_result_count"`
	MissingResultCount   int          `json:"missing_result_count"`
	RetrievalRecallAtK   map[int]Rate `json:"retrieval_recall_at_k"`
	RerankHitRateAtK     map[int]Rate `json:"rerank_hit_rate_at_k"`
	ToolCallSuccessRate  Rate         `json:"tool_call_success_rate"`
	CitationAccuracy     Rate         `json:"citation_accuracy"`
	TaskSuccessRate      Rate         `json:"task_success_rate"`
	Warnings             []string     `json:"warnings,omitempty"`
}

// Score computes standard metrics from agent traces. Retrieval Recall@K is a
// micro-average over gold chunks. Rerank hit rate is per task (at least one
// gold chunk in the top K). Citation accuracy is correct citations / submitted
// citations. Tool-call success requires every expected tool at least once.
func Score(tasks []Task, results []Result, ks ...int) (Report, error) {
	if len(tasks) == 0 {
		return Report{}, fmt.Errorf("evaluation suite is empty")
	}
	if len(ks) == 0 {
		ks = []int{1, 3, 5, 10}
	}
	ks = sortedPositiveUnique(ks)
	taskByID := make(map[string]Task, len(tasks))
	for _, task := range tasks {
		if task.ID == "" {
			return Report{}, fmt.Errorf("task with empty ID")
		}
		if _, exists := taskByID[task.ID]; exists {
			return Report{}, fmt.Errorf("duplicate task ID %q", task.ID)
		}
		taskByID[task.ID] = task
	}
	resultByTask := make(map[string]Result, len(results))
	for _, result := range results {
		if _, exists := taskByID[result.TaskID]; !exists {
			return Report{}, fmt.Errorf("result references unknown task %q", result.TaskID)
		}
		if _, exists := resultByTask[result.TaskID]; exists {
			return Report{}, fmt.Errorf("duplicate result for task %q", result.TaskID)
		}
		resultByTask[result.TaskID] = result
	}

	report := Report{
		SuiteVersion:         SuiteVersion,
		TaskCount:            len(tasks),
		SubmittedResultCount: len(results),
		MissingResultCount:   len(tasks) - len(results),
		RetrievalRecallAtK:   make(map[int]Rate, len(ks)),
		RerankHitRateAtK:     make(map[int]Rate, len(ks)),
	}
	type counter struct{ correct, total int }
	retrieval := make(map[int]counter, len(ks))
	rerank := make(map[int]counter, len(ks))
	var tools, citations, taskSuccess counter

	for _, task := range tasks {
		result, ok := resultByTask[task.ID]
		if !ok {
			continue
		}
		gold := asSet(task.RelevantChunkIDs)
		if len(gold) > 0 && result.RetrievedChunkIDs != nil {
			for _, k := range ks {
				hits := intersectionCount(gold, result.RetrievedChunkIDs[:min(k, len(result.RetrievedChunkIDs))])
				retrieval[k] = counter{retrieval[k].correct + hits, retrieval[k].total + len(gold)}
			}
		}
		if len(gold) > 0 && result.RerankedChunkIDs != nil {
			for _, k := range ks {
				hit := intersectionCount(gold, result.RerankedChunkIDs[:min(k, len(result.RerankedChunkIDs))]) > 0
				item := rerank[k]
				if hit {
					item.correct++
				}
				item.total++
				rerank[k] = item
			}
		}
		if len(task.ExpectedTools) > 0 {
			successful := successfulTools(result.ToolCalls)
			for _, expected := range task.ExpectedTools {
				tools.total++
				if successful[expected] {
					tools.correct++
				}
			}
		}
		if len(gold) > 0 && len(result.CitationIDs) > 0 {
			for _, citation := range result.CitationIDs {
				citations.total++
				if gold[citation] {
					citations.correct++
				}
			}
		}
		if result.TaskPassed != nil {
			taskSuccess.total++
			if *result.TaskPassed {
				taskSuccess.correct++
			}
		}
	}
	for _, k := range ks {
		report.RetrievalRecallAtK[k] = toRate(retrieval[k])
		report.RerankHitRateAtK[k] = toRate(rerank[k])
	}
	report.ToolCallSuccessRate = toRate(tools)
	report.CitationAccuracy = toRate(citations)
	report.TaskSuccessRate = toRate(taskSuccess)
	if report.MissingResultCount > 0 {
		report.Warnings = append(report.Warnings, "some tasks have no result and are excluded from metric denominators")
	}
	if taskSuccess.total == 0 {
		report.Warnings = append(report.Warnings, "Task Success Rate requires task_passed from a configured judge")
	}
	return report, nil
}

func asSet(values []string) map[string]bool {
	set := make(map[string]bool, len(values))
	for _, value := range values {
		set[value] = true
	}
	return set
}
func intersectionCount(gold map[string]bool, values []string) int {
	seen := map[string]bool{}
	hits := 0
	for _, value := range values {
		if gold[value] && !seen[value] {
			hits++
			seen[value] = true
		}
	}
	return hits
}
func successfulTools(calls []ToolCall) map[string]bool {
	found := map[string]bool{}
	for _, call := range calls {
		if call.Success {
			found[call.Name] = true
		}
	}
	return found
}
func min(a, b int) int {
	if a < b {
		return a
	}
	return b
}
func toRate(item struct{ correct, total int }) Rate {
	rate := Rate{Numerator: item.correct, Denominator: item.total}
	if item.total > 0 {
		value := float64(item.correct) / float64(item.total)
		rate.Value = &value
	}
	return rate
}
func sortedPositiveUnique(values []int) []int {
	set := map[int]bool{}
	for _, value := range values {
		if value > 0 {
			set[value] = true
		}
	}
	out := make([]int, 0, len(set))
	for value := range set {
		out = append(out, value)
	}
	sort.Ints(out)
	return out
}

// Percentage formats an optional rate for Markdown reports and CLIs.
func Percentage(rate Rate) string {
	if rate.Value == nil {
		return "N/A"
	}
	return fmt.Sprintf("%.2f%%", math.Round(*rate.Value*10000)/100)
}
