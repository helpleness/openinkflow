package toolchain

import (
	"context"
	"encoding/json"
	"errors"
	"strings"
	"testing"

	llmutil "InkFlow/utils/llm"
)

func TestInvalidToolArgumentsAllowFiveCorrections(t *testing.T) {
	if maxInvalidToolArgumentCalls != 5 {
		t.Fatalf("maxInvalidToolArgumentCalls = %d, want 5", maxInvalidToolArgumentCalls)
	}
	if maxRecoverableToolErrors != 5 {
		t.Fatalf("maxRecoverableToolErrors = %d, want 5", maxRecoverableToolErrors)
	}
}

func TestIsolatedToolRoundDoesNotReplaySuccessfulMutations(t *testing.T) {
	messages := isolatedToolRoundMessages(
		[]llmutil.Message{{Role: "user", Content: "继续创建节点"}},
		[]Trace{
			{ToolName: "outline.create", Kind: KindMutation, Status: "ok", OutputSummary: `{"id":44,"title":"渗透与预警"}`},
			{ToolName: "outline.create", Kind: KindMutation, Status: "error", Error: "title and core_goal are required"},
		},
		RunOptions{RequiredAnyTools: []string{"outline.create", "outline.update"}},
	)
	content := messages[len(messages)-1].Content
	for _, required := range []string{"任务目标没有改变", "outline.create 或 outline.update", "不得重放已经成功的 mutation", "只修正该失败调用", "渗透与预警", "title and core_goal are required"} {
		if !strings.Contains(content, required) {
			t.Fatalf("isolated correction prompt does not contain %q: %s", required, content)
		}
	}
}

func TestAvailableLLMToolsRemovesCompletedRunOnceQuery(t *testing.T) {
	registry := NewRegistry()
	if err := registry.Register(Tool{
		Name:          "character.list",
		Kind:          KindQuery,
		RunOncePerRun: true,
		Parameters:    map[string]any{"type": "object"},
		Handler:       func(context.Context, json.RawMessage) (any, error) { return nil, nil },
	}); err != nil {
		t.Fatal(err)
	}
	if err := registry.Register(Tool{
		Name:       "character.update",
		Kind:       KindMutation,
		Parameters: map[string]any{"type": "object"},
		Handler:    func(context.Context, json.RawMessage) (any, error) { return nil, nil },
	}); err != nil {
		t.Fatal(err)
	}

	tools := availableLLMTools(registry, map[string]Trace{"character.list": {ToolName: "character.list", Status: "ok"}}, nil)
	if len(tools) != 1 || tools[0].Function.Name != "character_update" {
		t.Fatalf("available tools = %#v, want only character_update", tools)
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

	tools := availableLLMTools(registry, nil, map[string]int{"knowledge.search": 3})
	if len(tools) != 0 {
		t.Fatalf("available tools = %#v, want knowledge.search removed at its call limit", tools)
	}
}

func TestIsolatedToolProgressContextStaysBoundedAndKeepsGoalState(t *testing.T) {
	traces := make([]Trace, 0, 32)
	for index := 0; index < 32; index++ {
		traces = append(traces, Trace{
			ToolName:      "character.update",
			Kind:          KindMutation,
			Input:         json.RawMessage(`{"character_id":51,"current_status":{"state":"` + strings.Repeat("长", 500) + `"}}`),
			OutputSummary: `{"ID":51,"name":"沈伯远","updated":true,"detail":"` + strings.Repeat("更", 500) + `"}`,
			Status:        "ok",
		})
	}
	context := isolatedToolProgressContext(traces, 18000)
	if len([]rune(context)) > 18000 {
		t.Fatalf("progress context has %d runes, want <= 18000", len([]rune(context)))
	}
	if !strings.Contains(context, "32. character.update [成功]") || !strings.Contains(context, `"character_id":51`) {
		t.Fatalf("progress ledger lost completion state: %s", context)
	}
}

func TestIsolatedToolProgressContextKeepsRunOnceCharacterSnapshot(t *testing.T) {
	fullSnapshot := `{"characters":"` + strings.Repeat("角", 6000) + `快照末尾"}`
	context := isolatedToolProgressContext([]Trace{
		{
			ToolName:      "knowledge.search",
			Kind:          KindQuery,
			Input:         json.RawMessage(`{"query":"江时雨等级"}`),
			OutputSummary: "等级证据",
			outputContext: "江时雨从S逐步晋升SS",
			Status:        "ok",
		},
		{
			ToolName:      "character.list",
			Kind:          KindQuery,
			Input:         json.RawMessage(`{"names":["江时雨","沈酌之"]}`),
			OutputSummary: "角色短摘要",
			outputContext: fullSnapshot,
			Status:        "ok",
		},
	}, 18000)
	if !strings.Contains(context, "快照末尾") {
		t.Fatalf("character snapshot was compacted too aggressively: %s", context)
	}
	if !strings.Contains(context, "江时雨从S逐步晋升SS") {
		t.Fatalf("distinct knowledge evidence was lost: %s", context)
	}
}

func TestCompletionRequirementAcceptsAnySuccessfulTool(t *testing.T) {
	opt := RunOptions{RequiredAnyTools: []string{"outline.create", "outline.update"}}
	if completionRequirementSatisfied(nil, opt) {
		t.Fatal("empty traces unexpectedly satisfied an alternative completion requirement")
	}
	traces := []Trace{{ToolName: "outline.update", Status: "ok"}}
	if !completionRequirementSatisfied(traces, opt) {
		t.Fatal("successful outline.update did not satisfy create-or-update requirement")
	}
	if got := completionRequirementLabel(opt); got != "outline.create 或 outline.update" {
		t.Fatalf("completion requirement label = %q", got)
	}
}

func TestCompletionRequirementRequiresAllSuccessfulTools(t *testing.T) {
	opt := RunOptions{RequiredTools: []string{"knowledge.search", "knowledge.create"}}
	if completionRequirementSatisfied([]Trace{{ToolName: "knowledge.search", Status: "ok"}}, opt) {
		t.Fatal("one successful tool unexpectedly satisfied an all-tools requirement")
	}
	traces := []Trace{{ToolName: "knowledge.search", Status: "ok"}, {ToolName: "knowledge.create", Status: "ok"}}
	if !completionRequirementSatisfied(traces, opt) {
		t.Fatal("all successful tools did not satisfy completion requirement")
	}
	if got := completionRequirementLabel(opt); got != "knowledge.search 且 knowledge.create" {
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
	if completionRequirementSatisfied(traces, opt) {
		t.Fatal("two successful creates unexpectedly satisfied a three-call requirement")
	}
	traces = append(traces, Trace{ToolName: "knowledge.create", Status: "ok"})
	if !completionRequirementSatisfied(traces, opt) {
		t.Fatal("three successful creates did not satisfy the call-count requirement")
	}
}

func TestToolQueryCacheKeyChangesAfterMutation(t *testing.T) {
	input := map[string]any{"project_id": 2}
	first := toolQueryCacheKey(0, "outline.list", input)
	same := toolQueryCacheKey(0, "outline.list", map[string]any{"project_id": 2})
	afterMutation := toolQueryCacheKey(1, "outline.list", input)
	if first != same {
		t.Fatalf("equivalent query inputs produced different keys: %q != %q", first, same)
	}
	if first == afterMutation {
		t.Fatal("query cache key did not change after a mutation")
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
	messages := []llmutil.Message{{Role: "assistant", ToolCalls: []llmutil.ToolCall{
		{ID: "call_failed", Function: llmutil.ToolCallFunction{Name: "draft_get_current"}},
		{ID: "call_pending_1", Function: llmutil.ToolCallFunction{Name: "rag_search"}},
		{ID: "call_pending_2", Function: llmutil.ToolCallFunction{Name: "writing_generate_scenes"}},
	}}}
	messages = append(messages, toolResultMessage("call_failed", map[string]any{"ok": false, "error": "record not found"}))
	messages = appendSkippedToolResults(
		messages,
		messages[0].ToolCalls[1:],
		"draft.get_current",
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
	if !strings.Contains(messages[2].Content, `"skipped":true`) || !strings.Contains(messages[2].Content, "draft.get_current") {
		t.Fatalf("skipped response lacks recovery context: %s", messages[2].Content)
	}
}

func TestOutputLimitRecoveryPromptRequiresImmediateConvergence(t *testing.T) {
	messages := outputLimitRecoveryMessages(
		[]llmutil.Message{{Role: "user", Content: "创建设定文档"}},
		RunOptions{RequiredTools: []string{"knowledge.create"}},
	)
	if len(messages) != 2 {
		t.Fatalf("recovery message count = %d, want 2", len(messages))
	}
	content := messages[1].Content
	for _, required := range []string{"不要输出长正文或思考过程", "knowledge.create", "tool_calls", "不得把函数参数 JSON 当成普通正文"} {
		if !strings.Contains(content, required) {
			t.Fatalf("recovery prompt does not contain %q: %s", required, content)
		}
	}
	if strings.Contains(content, "1200 个汉字") {
		t.Fatalf("recovery prompt still imposes the old short-answer limit: %s", content)
	}
}

func TestOutputLimitRecoveryPromptPreservesPureCreationRequirements(t *testing.T) {
	messages := outputLimitRecoveryMessages([]llmutil.Message{{Role: "user", Content: "生成 40 条评论"}}, RunOptions{})
	content := messages[len(messages)-1].Content
	for _, required := range []string{"保持用户原始任务要求的格式、数量和篇幅", "完整最终内容", "不要擅自缩短为摘要"} {
		if !strings.Contains(content, required) {
			t.Fatalf("pure creation recovery prompt does not contain %q: %s", required, content)
		}
	}
}

func TestToolTraceContextPrefersExpandedContext(t *testing.T) {
	context := toolTraceContext([]Trace{
		{ToolName: "knowledge.search", Status: "ok", OutputSummary: "短摘要", outputContext: "包含时间减速计算公式的完整证据"},
	}, 3000)
	if !strings.Contains(context, "时间减速计算公式") || strings.Contains(context, "短摘要") {
		t.Fatalf("synthesis did not use expanded tool context: %s", context)
	}
}
