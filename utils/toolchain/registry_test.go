package toolchain

import (
	"context"
	"encoding/json"
	"strings"
	"testing"
)

func TestRegistryUsesProtocolSafeToolNames(t *testing.T) {
	registry := NewRegistry()
	if err := registry.Register(Tool{
		Name: "draft.review_logic",
		Handler: func(context.Context, json.RawMessage) (any, error) {
			return nil, nil
		},
	}); err != nil {
		t.Fatal(err)
	}

	tools := registry.LLMTools()
	if len(tools) != 1 || tools[0].Function.Name != "draft_review_logic" {
		t.Fatalf("unexpected protocol tool name: %#v", tools)
	}

	name, _, ok := registry.ResolveLLMTool("draft_review_logic")
	if !ok || name != "draft.review_logic" {
		t.Fatalf("failed to resolve protocol tool name: name=%q ok=%v", name, ok)
	}
}

func TestRegistryRejectsProtocolNameCollision(t *testing.T) {
	registry := NewRegistry()
	handler := func(context.Context, json.RawMessage) (any, error) { return nil, nil }
	if err := registry.Register(Tool{Name: "fact.search", Handler: handler}); err != nil {
		t.Fatal(err)
	}
	if err := registry.Register(Tool{Name: "fact_search", Handler: handler}); err == nil {
		t.Fatal("expected protocol tool name collision")
	}
}

func TestDecodeToolArgumentsRejectsInvalidJSON(t *testing.T) {
	if _, _, err := decodeToolArguments(`{"draft":"unclosed}`); err == nil {
		t.Fatal("expected invalid tool arguments to be rejected")
	}
}

func TestDecodeToolArgumentsReturnsSerializableEventInput(t *testing.T) {
	raw, input, err := decodeToolArguments(`{"draft":"text","node_id":11}`)
	if err != nil {
		t.Fatal(err)
	}
	if !json.Valid(raw) {
		t.Fatalf("arguments are not valid JSON: %s", raw)
	}
	if _, err := json.Marshal(input); err != nil {
		t.Fatalf("event input is not serializable: %v", err)
	}
}

func TestRegistryUsesToolSpecificSummaryBudget(t *testing.T) {
	registry := NewRegistry()
	longResult := strings.Repeat("x", 1500) + "TAIL"
	if err := registry.Register(Tool{
		Name:            "outline.list",
		SummaryMaxRunes: 2000,
		Handler: func(context.Context, json.RawMessage) (any, error) {
			return longResult, nil
		},
	}); err != nil {
		t.Fatal(err)
	}

	_, trace, err := registry.Call(context.Background(), "outline.list", json.RawMessage(`{}`))
	if err != nil {
		t.Fatal(err)
	}
	if !strings.Contains(trace.OutputSummary, "TAIL") {
		t.Fatalf("custom summary budget still truncated the result: %s", trace.OutputSummary[len(trace.OutputSummary)-40:])
	}
}

func TestRegistryConvertsToolPanicToTraceError(t *testing.T) {
	registry := NewRegistry()
	if err := registry.Register(Tool{
		Name: "knowledge.search",
		Handler: func(context.Context, json.RawMessage) (any, error) {
			panic("vector index is unavailable")
		},
	}); err != nil {
		t.Fatal(err)
	}

	result, trace, err := registry.Call(context.Background(), "knowledge.search", json.RawMessage(`{}`))
	if err == nil || result != nil {
		t.Fatalf("panic must become an error result=%#v err=%v", result, err)
	}
	if trace.Status != "error" || !strings.Contains(trace.Error, "vector index is unavailable") {
		t.Fatalf("panic trace = %#v", trace)
	}
}
