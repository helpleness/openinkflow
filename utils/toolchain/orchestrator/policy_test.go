package orchestrator

import (
	"context"
	"encoding/json"
	"errors"
	"reflect"
	"testing"

	domainllm "InkFlow/internal/ai/llm"
)

func TestNormalizedCompletionMergesLegacyNamesAndCopiesCollections(t *testing.T) {
	options := RunOptions{RequiredTool: " document.search ", RequiredTools: []string{"document.search", "document.create"}, RequiredAnyTools: []string{"verify"}, RequiredToolCallCounts: map[string]int{"document.create": 2, " document.create ": 3}}
	config := normalizeRunOptions(options)
	if !reflect.DeepEqual(config.Completion.RequiredAll, []string{"document.search", "document.create"}) || config.Completion.RequiredCounts["document.create"] != 3 {
		t.Fatalf("config=%+v", config)
	}
	options.RequiredTools[1] = "changed"
	options.RequiredAnyTools[0] = "changed"
	options.RequiredToolCallCounts["document.create"] = 9
	if config.Completion.RequiredAll[1] != "document.create" || config.Completion.RequiredAny[0] != "verify" || config.Completion.RequiredCounts["document.create"] != 3 {
		t.Fatal("run config aliases caller collections")
	}
}

func TestToolBudgetsKeepMutationAttemptsAndSuccessfulLLMCalls(t *testing.T) {
	registry := NewRegistry()
	mutationCalls := 0
	llmCalls := 0
	registerTestTool(t, registry, Tool{Name: "write", Kind: KindMutation, Handler: func(context.Context, json.RawMessage) (any, error) { mutationCalls++; return "saved", nil }})
	registerTestTool(t, registry, Tool{Name: "review", Kind: KindLLM, Handler: func(context.Context, json.RawMessage) (any, error) { llmCalls++; return "reviewed", nil }})
	state := testRunState(t, registry, RunOptions{MaxMutationToolCalls: 1, MaxLLMToolCalls: 1}, nil)
	for _, name := range []string{"write", "review"} {
		batch, err := state.executeToolCallBatch([]domainllm.ToolCall{{ID: "first", Name: name}, {ID: "second", Name: name}})
		if err != nil || !batch.limitReached {
			t.Fatalf("name=%s batch=%+v err=%v", name, batch, err)
		}
	}
	if mutationCalls != 1 || llmCalls != 1 || state.ledger.MutationVersion != 1 || state.ledger.MutationCalls != 1 || state.ledger.LLMCalls != 1 {
		t.Fatalf("ledger=%+v", state.ledger)
	}
}

func TestQueryReuseInvalidatesOnlyAfterSuccessfulMutation(t *testing.T) {
	registry := NewRegistry()
	queries := 0
	writes := 0
	registerTestTool(t, registry, Tool{Name: "query", Handler: func(context.Context, json.RawMessage) (any, error) { queries++; return "evidence", nil }})
	registerTestTool(t, registry, Tool{Name: "write", Kind: KindMutation, Handler: func(context.Context, json.RawMessage) (any, error) {
		writes++
		if writes == 1 {
			return nil, errors.New("write failed")
		}
		return "saved", nil
	}})
	state := testRunState(t, registry, RunOptions{RequiredTool: "unfinished", MaxMutationToolCalls: 2}, nil)
	for _, name := range []string{"query", "query", "write", "query", "write", "query"} {
		if _, err := state.executeToolCallBatch([]domainllm.ToolCall{{ID: name, Name: name, Arguments: json.RawMessage(`{}`)}}); err != nil {
			t.Fatal(err)
		}
	}
	if queries != 2 || state.ledger.MutationVersion != 1 || state.ledger.MutationCalls != 2 || len(state.ledger.Traces) != 4 {
		t.Fatalf("queries=%d ledger=%+v", queries, state.ledger)
	}
}

func TestPerRunLimitAppliesWithinOneBatch(t *testing.T) {
	registry := NewRegistry()
	calls := 0
	registerTestTool(t, registry, Tool{Name: "search", MaxCallsPerRun: 1, Handler: func(context.Context, json.RawMessage) (any, error) { calls++; return "found", nil }})
	state := testRunState(t, registry, RunOptions{}, nil)
	_, err := state.executeToolCallBatch([]domainllm.ToolCall{{ID: "1", Name: "search"}, {ID: "2", Name: "search"}})
	if err != nil || calls != 1 || state.ledger.SuccessfulCalls["search"] != 1 {
		t.Fatalf("calls=%d err=%v", calls, err)
	}
}

func TestRunOptionsDefaults(t *testing.T) {
	options := (RunOptions{}).withDefaults()
	if options.MaxToolCalls != 6 || options.MaxLLMToolCalls != 2 || options.MaxLLMRetries != 2 || options.MaxMutationToolCalls != 1 || options.MaxIsolatedToolRounds != 2 {
		t.Fatalf("unexpected run defaults: %#v", options)
	}
}

func TestAvailableLLMToolsRemovesCompletedRunOnceQuery(t *testing.T) {
	registry := NewRegistry()
	if err := registry.Register(Tool{
		Name:          "catalog.snapshot",
		Kind:          KindQuery,
		RunOncePerRun: true,
		Parameters:    map[string]any{"type": "object"},
		Handler:       func(context.Context, json.RawMessage) (any, error) { return nil, nil },
	}); err != nil {
		t.Fatal(err)
	}
	if err := registry.Register(Tool{
		Name:       "catalog.update",
		Kind:       KindMutation,
		Parameters: map[string]any{"type": "object"},
		Handler:    func(context.Context, json.RawMessage) (any, error) { return nil, nil },
	}); err != nil {
		t.Fatal(err)
	}

	tools := availableLLMTools(registry, map[string]Trace{"catalog.snapshot": {ToolName: "catalog.snapshot", Status: "ok"}}, nil, nil)
	if len(tools) != 1 || tools[0].Name != "catalog_update" {
		t.Fatalf("available tools = %#v, want only catalog_update", tools)
	}
}

func TestAvailableLLMToolsRemovesToolAtPerRunLimit(t *testing.T) {
	registry := NewRegistry()
	if err := registry.Register(Tool{
		Name:           "knowledge.search",
		Kind:           KindQuery,
		MaxCallsPerRun: 3,
		Parameters:     map[string]any{"type": "object"},
		Handler:        func(context.Context, json.RawMessage) (any, error) { return nil, nil },
	}); err != nil {
		t.Fatal(err)
	}

	tools := availableLLMTools(registry, nil, map[string]int{"knowledge.search": 3}, nil)
	if len(tools) != 0 {
		t.Fatalf("available tools = %#v, want knowledge.search removed at its call limit", tools)
	}
}

func TestAttemptLimitIncludesFailedCalls(t *testing.T) {
	registry := NewRegistry()
	calls := 0
	registerTestTool(t, registry, Tool{
		Name:              "knowledge.search",
		Kind:              KindQuery,
		MaxAttemptsPerRun: 1,
		Handler: func(context.Context, json.RawMessage) (any, error) {
			calls++
			return nil, errors.New("search unavailable")
		},
	})
	state := testRunState(t, registry, RunOptions{}, nil)
	for index := 0; index < 2; index++ {
		if _, err := state.executeToolCallBatch([]domainllm.ToolCall{{ID: "search", Name: "knowledge.search"}}); err != nil {
			t.Fatal(err)
		}
	}
	if calls != 1 || state.ledger.AttemptedCalls["knowledge.search"] != 1 {
		t.Fatalf("calls=%d ledger=%+v", calls, state.ledger)
	}
	tools := availableLLMTools(registry, nil, nil, state.ledger.AttemptedCalls)
	if len(tools) != 0 {
		t.Fatalf("available tools = %#v, want knowledge.search removed at its attempt limit", tools)
	}
}

func TestCompletionRequirementAcceptsAnySuccessfulTool(t *testing.T) {
	opt := RunOptions{RequiredAnyTools: []string{"outline.create", "outline.update"}}
	if completionRequirementSatisfied(nil, normalizeRunOptions(opt).Completion) {
		t.Fatal("empty traces unexpectedly satisfied an alternative completion requirement")
	}
	traces := []Trace{{ToolName: "outline.update", Status: "ok"}}
	if !completionRequirementSatisfied(traces, normalizeRunOptions(opt).Completion) {
		t.Fatal("successful outline.update did not satisfy create-or-update requirement")
	}
	if got := completionRequirementLabel(normalizeRunOptions(opt).Completion); got != "outline.create 或 outline.update" {
		t.Fatalf("completion requirement label = %q", got)
	}
}

func TestCompletionRequirementRequiresAllSuccessfulTools(t *testing.T) {
	opt := RunOptions{RequiredTools: []string{"knowledge.search", "knowledge.create"}}
	if completionRequirementSatisfied([]Trace{{ToolName: "knowledge.search", Status: "ok"}}, normalizeRunOptions(opt).Completion) {
		t.Fatal("one successful tool unexpectedly satisfied an all-tools requirement")
	}
	traces := []Trace{{ToolName: "knowledge.search", Status: "ok"}, {ToolName: "knowledge.create", Status: "ok"}}
	if !completionRequirementSatisfied(traces, normalizeRunOptions(opt).Completion) {
		t.Fatal("all successful tools did not satisfy completion requirement")
	}
	if got := completionRequirementLabel(normalizeRunOptions(opt).Completion); got != "knowledge.search 且 knowledge.create" {
		t.Fatalf("completion requirement label = %q", got)
	}
}

func TestCompletionRequirementRequiresConfiguredSuccessfulCallCount(t *testing.T) {
	opt := RunOptions{RequiredToolCallCounts: map[string]int{"knowledge.create": 3}}
	traces := []Trace{
		{ToolName: "knowledge.create", Status: "ok"},
		{ToolName: "knowledge.create", Status: "error"},
		{ToolName: "knowledge.create", Status: "ok"},
	}
	if completionRequirementSatisfied(traces, normalizeRunOptions(opt).Completion) {
		t.Fatal("two successful creates unexpectedly satisfied a three-call requirement")
	}
	traces = append(traces, Trace{ToolName: "knowledge.create", Status: "ok"})
	if !completionRequirementSatisfied(traces, normalizeRunOptions(opt).Completion) {
		t.Fatal("three successful creates did not satisfy the call-count requirement")
	}
}

func TestToolQueryCacheKeyChangesAfterMutation(t *testing.T) {
	input := map[string]any{"project_id": 2}
	first := toolQueryCacheKey(0, "document.list", input)
	same := toolQueryCacheKey(0, "document.list", map[string]any{"project_id": 2})
	afterMutation := toolQueryCacheKey(1, "document.list", input)
	if first != same {
		t.Fatalf("equivalent query inputs produced different keys: %q != %q", first, same)
	}
	if first == afterMutation {
		t.Fatal("query cache key did not change after a mutation")
	}
}
