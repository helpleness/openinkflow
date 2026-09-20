package orchestrator

import (
	"context"
	"encoding/json"
	"errors"
	"fmt"
	"strings"
	"testing"

	domainllm "InkFlow/internal/ai/llm"
)

type recordingExecutor struct {
	calls      int
	user, name string
	args       json.RawMessage
}

func (e *recordingExecutor) Execute(_ context.Context, user, name string, args json.RawMessage) (any, Trace, error) {
	e.calls++
	e.user = user
	e.name = name
	e.args = args
	return "executed", Trace{ToolName: name, Kind: KindQuery, Status: "ok", OutputSummary: "executed"}, nil
}

func TestToolLifecycleUsesExecutorAndRecordsLedger(t *testing.T) {
	registry := NewRegistry()
	registerTestTool(t, registry, Tool{Name: "document.search", Handler: func(context.Context, json.RawMessage) (any, error) {
		t.Fatal("handler bypassed executor")
		return nil, nil
	}})
	executor := &recordingExecutor{}
	state := testRunState(t, registry, RunOptions{Executor: executor, UserName: "alice"}, nil)
	_, err := state.executeToolCallBatch([]domainllm.ToolCall{{ID: "c1", Name: "document_search", Arguments: json.RawMessage(`{"id":7}`)}})
	if err != nil || executor.calls != 1 || executor.user != "alice" || executor.name != "document.search" || string(executor.args) != `{"id":7}` || len(state.ledger.Traces) != 1 {
		t.Fatalf("executor=%+v ledger=%+v err=%v", executor, state.ledger, err)
	}
	last := state.messages[len(state.messages)-1]
	if last.ToolCallID != "c1" || !strings.Contains(last.Content, "executed") {
		t.Fatalf("tool result=%+v", last)
	}
}

func TestInvalidArgumentsAndRecoverableErrorsStopAfterFiveAttempts(t *testing.T) {
	for _, invalid := range []bool{false, true} {
		t.Run(fmt.Sprint(invalid), func(t *testing.T) {
			registry := NewRegistry()
			calls := 0
			registerTestTool(t, registry, Tool{Name: "document.search", Handler: func(context.Context, json.RawMessage) (any, error) { calls++; return nil, errors.New("record missing") }})
			state := testRunState(t, registry, RunOptions{}, nil)
			args := json.RawMessage(`{}`)
			if invalid {
				args = json.RawMessage(`{"id":`)
			}
			for attempt := 1; attempt <= 5; attempt++ {
				_, err := state.executeToolCallBatch([]domainllm.ToolCall{{ID: fmt.Sprint(attempt), Name: "document_search", Arguments: args}})
				if (err != nil) != (attempt == 5) {
					t.Fatalf("attempt=%d err=%v", attempt, err)
				}
			}
			if invalid {
				if calls != 0 || len(state.ledger.Traces) != 0 {
					t.Fatal("invalid arguments reached handler")
				}
			} else if calls != 5 || len(state.ledger.Traces) != 5 {
				t.Fatalf("calls=%d traces=%d", calls, len(state.ledger.Traces))
			}
		})
	}
}

func TestRecoverableFailureSkipsRemainingBatchAndDoesNotReplaySuccessfulTool(t *testing.T) {
	registry := NewRegistry()
	firstCalls := 0
	laterCalls := 0
	registerTestTool(t, registry, Tool{Name: "first", RunOncePerRun: true, Handler: func(context.Context, json.RawMessage) (any, error) { firstCalls++; return "first done", nil }})
	registerTestTool(t, registry, Tool{Name: "fail", Handler: func(context.Context, json.RawMessage) (any, error) { return nil, errors.New("fix arguments") }})
	registerTestTool(t, registry, Tool{Name: "later", Handler: func(context.Context, json.RawMessage) (any, error) { laterCalls++; return nil, nil }})
	state := testRunState(t, registry, RunOptions{}, nil)
	calls := []domainllm.ToolCall{{ID: "1", Name: "first"}, {ID: "2", Name: "fail"}, {ID: "3", Name: "later"}}
	_, err := state.executeToolCallBatch(calls)
	if err != nil || firstCalls != 1 || laterCalls != 0 || !strings.Contains(state.messages[len(state.messages)-1].Content, `"skipped":true`) {
		t.Fatalf("batch err=%v first=%d later=%d messages=%+v", err, firstCalls, laterCalls, state.messages)
	}
	_, err = state.executeToolCallBatch(calls[:1])
	if err != nil || firstCalls != 1 || !strings.Contains(state.messages[len(state.messages)-1].Content, `"reused":true`) {
		t.Fatalf("successful run-once replayed: %v", err)
	}
}

func TestRepeatedQueryWithoutCompletionRequirementKeepsRunOpen(t *testing.T) {
	registry := NewRegistry()
	calls := 0
	registerTestTool(t, registry, Tool{Name: "knowledge.search", Handler: func(context.Context, json.RawMessage) (any, error) {
		calls++
		return "evidence", nil
	}})
	state := testRunState(t, registry, RunOptions{}, nil)
	call := domainllm.ToolCall{ID: "search", Name: "knowledge.search", Arguments: json.RawMessage(`{"query":"same"}`)}
	if result, err := state.executeToolCallBatch([]domainllm.ToolCall{call}); err != nil || result.result != nil {
		t.Fatalf("first query result=%+v err=%v", result, err)
	}
	call.ID = "search-again"
	if result, err := state.executeToolCallBatch([]domainllm.ToolCall{call}); err != nil || result.result != nil {
		t.Fatalf("reused query unexpectedly ended run: result=%+v err=%v", result, err)
	}
	if calls != 1 || !strings.Contains(state.messages[len(state.messages)-1].Content, `"reused":true`) {
		t.Fatalf("calls=%d messages=%+v", calls, state.messages)
	}
}

func TestTerminalToolStopsRemainingBatch(t *testing.T) {
	for _, flag := range []bool{false, true} {
		t.Run(fmt.Sprint(flag), func(t *testing.T) {
			registry := NewRegistry()
			calls := 0
			registerTestTool(t, registry, Tool{Name: "finish", TerminalOnSuccess: flag, Handler: func(context.Context, json.RawMessage) (any, error) {
				calls++
				if flag {
					return "done", nil
				}
				return TerminalResult{Result: "done", Message: "已完成"}, nil
			}})
			state := testRunState(t, registry, RunOptions{}, nil)
			batch, err := state.executeToolCallBatch([]domainllm.ToolCall{{ID: "1", Name: "finish"}, {ID: "2", Name: "finish"}})
			if err != nil || batch.result == nil || calls != 1 || len(batch.result.Traces) != 1 {
				t.Fatalf("batch=%+v calls=%d err=%v", batch, calls, err)
			}
		})
	}
}

func TestToolBusinessRetryHonorsCancellation(t *testing.T) {
	registry := NewRegistry()
	calls := 0
	registerTestTool(t, registry, Tool{Name: "retry", MaxRetries: 2, Handler: func(context.Context, json.RawMessage) (any, error) {
		calls++
		return nil, errors.New("gateway timeout")
	}})
	ctx, cancel := context.WithCancel(context.Background())
	defer cancel()
	state := testRunState(t, registry, RunOptions{OnEvent: func(event string, _ any) {
		if event == "tool_retry" {
			cancel()
		}
	}}, nil)
	state.ctx = ctx
	tool, _ := registry.Get("retry")
	_, _, err := state.runToolWithRetry(preparedToolCall{name: "retry", tool: tool, arguments: json.RawMessage(`{}`)})
	if !errors.Is(err, context.Canceled) || calls != 1 || len(state.ledger.Traces) != 1 {
		t.Fatalf("calls=%d err=%v ledger=%+v", calls, err, state.ledger)
	}
}

func TestInvalidToolArgumentsAllowFiveCorrections(t *testing.T) {
	if maxInvalidToolArgumentCalls != 5 {
		t.Fatalf("maxInvalidToolArgumentCalls = %d, want 5", maxInvalidToolArgumentCalls)
	}
	if maxRecoverableToolErrors != 5 {
		t.Fatalf("maxRecoverableToolErrors = %d, want 5", maxRecoverableToolErrors)
	}
}

func TestToolCallPayloadCompactsLargeResults(t *testing.T) {
	result := map[string]any{"content": strings.Repeat("设", 100)}
	trace := Trace{outputContext: `{"content":"设设设...(truncated)`, outputTrimmed: true}
	payload := toolCallPayload(result, trace, nil)
	if _, exists := payload["result"]; exists {
		t.Fatal("large result was replayed into the next model round")
	}
	if payload["result_truncated"] != true || payload["result_summary"] != trace.outputContext {
		t.Fatalf("unexpected compact payload: %#v", payload)
	}
	if _, err := json.Marshal(payload); err != nil {
		t.Fatalf("compact payload is not valid JSON: %v", err)
	}
}

func TestRecoverableToolFailureCompletesRemainingToolCallResponses(t *testing.T) {
	messages := []domainllm.Message{{Role: "assistant", ToolCalls: []domainllm.ToolCall{
		{ID: "call_failed", Name: "document_get"},
		{ID: "call_pending_1", Name: "document_search"},
		{ID: "call_pending_2", Name: "document_generate"},
	}}}
	messages = append(messages, toolResultMessage("call_failed", map[string]any{"ok": false, "error": "record not found"}))
	messages = appendSkippedToolResults(
		messages,
		messages[0].ToolCalls[1:],
		"document.get",
		errors.New("record not found"),
	)

	if len(messages) != 4 {
		t.Fatalf("message count = %d, want assistant + 3 tool responses", len(messages))
	}
	for index, wantID := range []string{"call_failed", "call_pending_1", "call_pending_2"} {
		message := messages[index+1]
		if message.Role != "tool" || message.ToolCallID != wantID {
			t.Fatalf("tool response %d = %#v, want id %s", index, message, wantID)
		}
	}
	if !strings.Contains(messages[2].Content, `"skipped":true`) || !strings.Contains(messages[2].Content, "document.get") {
		t.Fatalf("skipped response lacks recovery context: %s", messages[2].Content)
	}
}

func TestDecodeToolArgumentsRejectsInvalidJSON(t *testing.T) {
	if _, _, err := decodeToolArguments(`{"draft":"unclosed}`); err == nil {
		t.Fatal("expected invalid tool arguments to be rejected")
	}
}

func TestDecodeToolArgumentsReturnsSerializableEventInput(t *testing.T) {
	raw, input, err := decodeToolArguments(`{"document":"text","section_id":11}`)
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
