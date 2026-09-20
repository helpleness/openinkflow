package orchestrator_test

import (
	"context"
	"encoding/json"
	"strings"
	"testing"

	"InkFlow/utils/toolchain/executor"
	"InkFlow/utils/toolchain/orchestrator"
)

func TestRegistryUsesProtocolSafeToolNames(t *testing.T) {
	registry := orchestrator.NewRegistry()
	if err := registry.Register(orchestrator.Tool{
		Name: "document.review",
		Handler: func(context.Context, json.RawMessage) (any, error) {
			return nil, nil
		},
	}); err != nil {
		t.Fatal(err)
	}

	tools := registry.LLMTools()
	if len(tools) != 1 || tools[0].Name != "document_review" {
		t.Fatalf("unexpected protocol tool name: %#v", tools)
	}

	name, _, ok := registry.ResolveLLMTool("document_review")
	if !ok || name != "document.review" {
		t.Fatalf("failed to resolve protocol tool name: name=%q ok=%v", name, ok)
	}
}

func TestRegistryRejectsProtocolNameCollision(t *testing.T) {
	registry := orchestrator.NewRegistry()
	handler := func(context.Context, json.RawMessage) (any, error) { return nil, nil }
	if err := registry.Register(orchestrator.Tool{Name: "document.search", Handler: handler}); err != nil {
		t.Fatal(err)
	}
	if err := registry.Register(orchestrator.Tool{Name: "document_search", Handler: handler}); err == nil {
		t.Fatal("expected protocol tool name collision")
	}
}

func TestRegistryUsesToolSpecificSummaryBudget(t *testing.T) {
	registry := orchestrator.NewRegistry()
	longResult := strings.Repeat("x", 1500) + "TAIL"
	if err := registry.Register(orchestrator.Tool{
		Name:            "document.list",
		SummaryMaxRunes: 2000,
		Handler: func(context.Context, json.RawMessage) (any, error) {
			return longResult, nil
		},
	}); err != nil {
		t.Fatal(err)
	}

	_, trace, err := registry.Call(context.Background(), "document.list", json.RawMessage(`{}`))
	if err != nil {
		t.Fatal(err)
	}
	if !strings.Contains(trace.OutputSummary, "TAIL") {
		t.Fatalf("custom summary budget still truncated the result: %s", trace.OutputSummary[len(trace.OutputSummary)-40:])
	}
}

func TestRegistryConvertsToolPanicToTraceError(t *testing.T) {
	registry := orchestrator.NewRegistry()
	if err := registry.Register(orchestrator.Tool{
		Name: "document.search",
		Handler: func(context.Context, json.RawMessage) (any, error) {
			panic("vector index is unavailable")
		},
	}); err != nil {
		t.Fatal(err)
	}

	result, trace, err := registry.Call(context.Background(), "document.search", json.RawMessage(`{}`))
	if err == nil || result != nil {
		t.Fatalf("panic must become an error result=%#v err=%v", result, err)
	}
	if trace.Status != "error" || !strings.Contains(trace.Error, "vector index is unavailable") {
		t.Fatalf("panic trace = %#v", trace)
	}
}

var _ orchestrator.Executor = (*executor.Dispatcher)(nil)

func TestRegistryGetListAndResultTraceRemainCompatible(t *testing.T) {
	registry := orchestrator.NewRegistry()
	for _, name := range []string{"z.read", "a.read"} {
		if err := registry.Register(orchestrator.Tool{Name: name, Handler: func(context.Context, json.RawMessage) (any, error) { return "result", nil }}); err != nil {
			t.Fatal(err)
		}
	}
	tool, ok := registry.Get("a.read")
	if !ok || tool.Name != "a.read" || tool.Kind != orchestrator.KindQuery {
		t.Fatalf("get=%+v ok=%v", tool, ok)
	}
	listed := registry.List()
	if len(listed) != 2 || listed[0].Name != "a.read" || listed[1].Name != "z.read" {
		t.Fatalf("list=%+v", listed)
	}
	trace := registry.ResultTrace("a.read", json.RawMessage(`{"id":7}`), "cached result")
	if trace.ToolName != "a.read" || trace.Kind != orchestrator.KindQuery || trace.Status != "ok" || !strings.Contains(trace.OutputSummary, "cached result") {
		t.Fatalf("cached trace=%+v", trace)
	}
}
