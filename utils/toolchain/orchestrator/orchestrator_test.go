package orchestrator

import (
	"context"
	"encoding/json"
	"errors"
	"io"
	"net/http"
	"net/http/httptest"
	"strings"
	"testing"
	"time"

	"InkFlow/config"
	domainllm "InkFlow/internal/ai/llm"
	llmutil "InkFlow/utils/llm"
)

func TestRunWithToolsThroughConfiguredProvider(t *testing.T) {
	requests := 0
	server := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		requests++
		var request struct {
			Messages []domainllm.Message `json:"messages"`
		}
		if err := json.NewDecoder(r.Body).Decode(&request); err != nil {
			t.Error(err)
			w.WriteHeader(400)
			return
		}
		w.Header().Set("Content-Type", "application/json")
		if requests == 1 {
			_, _ = w.Write([]byte(`{"choices":[{"finish_reason":"tool_calls","message":{"role":"assistant","tool_calls":[{"id":"search-1","type":"function","function":{"name":"document_search","arguments":"{}"}}]}}]}`))
			return
		}
		last := request.Messages[len(request.Messages)-1]
		if last.ToolCallID != "search-1" || !strings.Contains(last.Content, "executed") {
			t.Errorf("provider received wrong tool result: %+v", last)
		}
		_, _ = w.Write([]byte(`{"choices":[{"finish_reason":"stop","message":{"role":"assistant","content":"已检索"}}]}`))
	}))
	defer server.Close()
	registry := NewRegistry()
	registerTestTool(t, registry, Tool{Name: "document.search", Handler: func(context.Context, json.RawMessage) (any, error) { t.Error("executor was bypassed"); return nil, nil }})
	executor := &recordingExecutor{}
	result, err := RunWithTools(context.Background(), []llmutil.Message{{Role: "user", Content: "检索"}}, registry, RunOptions{
		Executor: executor, UserName: "alice", RequiredTool: "document.search",
		LLM: &llmutil.GenerateOptions{Timeout: 5 * time.Second, LLM: &config.LLM{ProviderType: "openai", ModelDefault: "test", BaseUrl: server.URL}},
	})
	if err != nil || result.Message != "已检索" || len(result.Traces) != 1 || requests != 2 || executor.calls != 1 {
		t.Fatalf("result=%+v err=%v requests=%d executor=%+v", result, err, requests, executor)
	}
}

type scriptedProvider struct {
	requests []domainllm.ChatRequest
	chat     func(context.Context, domainllm.ChatRequest) (*domainllm.ChatResponse, error)
	stream   func(context.Context, domainllm.ChatRequest) (domainllm.ChatStream, error)
}

func (*scriptedProvider) Name() string { return "test" }
func (*scriptedProvider) Capabilities() domainllm.Capabilities {
	return domainllm.Capabilities{ToolCalling: true, Streaming: true}
}
func (p *scriptedProvider) Chat(ctx context.Context, request domainllm.ChatRequest) (*domainllm.ChatResponse, error) {
	p.requests = append(p.requests, request)
	return p.chat(ctx, request)
}
func (p *scriptedProvider) Stream(ctx context.Context, request domainllm.ChatRequest) (domainllm.ChatStream, error) {
	return p.stream(ctx, request)
}

func testRunState(t *testing.T, registry *Registry, options RunOptions, provider *scriptedProvider) *runState {
	t.Helper()
	if options.LLM == nil {
		options.LLM = &llmutil.GenerateOptions{LLM: &config.LLM{ProviderType: "openai", ModelDefault: "test"}}
	}
	state, err := newRunState(context.Background(), []domainllm.Message{{Role: "user", Content: "原始任务"}}, registry, options)
	if err != nil {
		t.Fatal(err)
	}
	state.llm = provider
	return state
}

func registerTestTool(t *testing.T, registry *Registry, tool Tool) {
	t.Helper()
	if err := registry.Register(tool); err != nil {
		t.Fatal(err)
	}
}

func toolDecision(calls ...domainllm.ToolCall) *domainllm.ChatResponse {
	return &domainllm.ChatResponse{Message: domainllm.Message{Role: "assistant", ToolCalls: calls}}
}

func TestRunReturnsDirectTextAndReasoning(t *testing.T) {
	done := 0
	p := &scriptedProvider{chat: func(context.Context, domainllm.ChatRequest) (*domainllm.ChatResponse, error) {
		return &domainllm.ChatResponse{Message: domainllm.Message{Role: "assistant", Content: " 答案 ", ReasoningContent: " 理由 "}}, nil
	}}
	state := testRunState(t, NewRegistry(), RunOptions{OnEvent: func(event string, payload any) {
		if event == "done" {
			done++
		}
	}}, p)
	result, err := state.run()
	if err != nil || result.Message != "答案" || result.Reasoning != "理由" || done != 1 || len(p.requests) != 1 {
		t.Fatalf("result=%+v err=%v done=%d requests=%d", result, err, done, len(p.requests))
	}
}

func TestRunFeedsToolResultToNextModelRound(t *testing.T) {
	registry := NewRegistry()
	registerTestTool(t, registry, Tool{Name: "document.search", Handler: func(context.Context, json.RawMessage) (any, error) { return map[string]any{"id": 7}, nil }})
	p := &scriptedProvider{}
	p.chat = func(_ context.Context, request domainllm.ChatRequest) (*domainllm.ChatResponse, error) {
		if len(p.requests) == 1 {
			return toolDecision(domainllm.ToolCall{ID: "call1", Name: "document_search", Arguments: json.RawMessage(`{}`)}), nil
		}
		last := request.Messages[len(request.Messages)-1]
		if last.Role != "tool" || last.ToolCallID != "call1" || !strings.Contains(last.Content, `"id":7`) {
			t.Fatalf("tool response=%+v", last)
		}
		return &domainllm.ChatResponse{Message: domainllm.Message{Content: "完成"}}, nil
	}
	state := testRunState(t, registry, RunOptions{RequiredTool: "document.search"}, p)
	result, err := state.run()
	if err != nil || result.Message != "完成" || len(result.Traces) != 1 || state.ledger.SuccessfulCalls["document.search"] != 1 {
		t.Fatalf("result=%+v err=%v ledger=%+v", result, err, state.ledger)
	}
}

func TestRunIsolatedRoundKeepsGoalWithoutReplayingProtocolHistory(t *testing.T) {
	registry := NewRegistry()
	registerTestTool(t, registry, Tool{Name: "document.create", Kind: KindMutation, Handler: func(context.Context, json.RawMessage) (any, error) { return "created", nil }})
	p := &scriptedProvider{}
	p.chat = func(_ context.Context, request domainllm.ChatRequest) (*domainllm.ChatResponse, error) {
		if len(p.requests) == 1 {
			return toolDecision(domainllm.ToolCall{ID: "create", Name: "document_create", Arguments: json.RawMessage(`{}`)}), nil
		}
		if len(request.Messages) != 2 || request.Messages[0].Content != "原始任务" || !strings.Contains(request.Messages[1].Content, "document.create [成功]") {
			t.Fatalf("isolated messages=%+v", request.Messages)
		}
		return &domainllm.ChatResponse{Message: domainllm.Message{Content: "已创建"}}, nil
	}
	result, err := testRunState(t, registry, RunOptions{ReturnAfterToolCalls: true, RequiredTool: "document.create"}, p).run()
	if err != nil || result.Message != "已创建" || len(result.Traces) != 1 {
		t.Fatalf("result=%+v err=%v", result, err)
	}
}

func TestRunRepromptsOnlyTruncatedModelDecisions(t *testing.T) {
	for _, transportFailure := range []bool{false, true} {
		t.Run(map[bool]string{false: "output_limit", true: "transport_error"}[transportFailure], func(t *testing.T) {
			p := &scriptedProvider{}
			p.chat = func(_ context.Context, request domainllm.ChatRequest) (*domainllm.ChatResponse, error) {
				if transportFailure {
					return nil, errors.New("gateway timeout")
				}
				if len(p.requests) == 1 {
					return &domainllm.ChatResponse{FinishReason: "length"}, nil
				}
				if !strings.Contains(request.Messages[len(request.Messages)-1].Content, "上一轮输出额度已耗尽") {
					t.Fatal("missing recovery prompt")
				}
				return &domainllm.ChatResponse{Message: domainllm.Message{Content: "完整答案"}}, nil
			}
			result, err := testRunState(t, NewRegistry(), RunOptions{}, p).run()
			if transportFailure {
				if err == nil || len(p.requests) != 1 {
					t.Fatalf("transport error was retried: %v %d", err, len(p.requests))
				}
			} else if err != nil || result.Message != "完整答案" || len(p.requests) != 2 {
				t.Fatalf("result=%+v err=%v calls=%d", result, err, len(p.requests))
			}
		})
	}
}

type scriptedStream struct {
	events  []domainllm.StreamEvent
	failure error
	closed  bool
}

func (s *scriptedStream) Recv() (domainllm.StreamEvent, error) {
	if len(s.events) > 0 {
		event := s.events[0]
		s.events = s.events[1:]
		return event, nil
	}
	if s.failure != nil {
		return domainllm.StreamEvent{}, s.failure
	}
	return domainllm.StreamEvent{}, io.EOF
}
func (s *scriptedStream) Close() error { s.closed = true; return nil }

func TestSynthesisUsesProviderStreamAndFreshDeadlineForFallback(t *testing.T) {
	for _, failStream := range []bool{false, true} {
		t.Run(map[bool]string{false: "stream", true: "fallback"}[failStream], func(t *testing.T) {
			stream := &scriptedStream{events: []domainllm.StreamEvent{{ContentDelta: "结论", ReasoningDelta: "依据"}}}
			if failStream {
				stream.failure = errors.New("stream failed")
			}
			var streamContext context.Context
			p := &scriptedProvider{}
			p.stream = func(ctx context.Context, request domainllm.ChatRequest) (domainllm.ChatStream, error) {
				streamContext = ctx
				if len(request.Tools) != 0 || request.ToolChoice != nil || *request.MaxTokens != 2048 || *request.Temperature != 0.25 || request.Reasoning.Enabled {
					t.Fatalf("synthesis request=%+v", request)
				}
				return stream, nil
			}
			p.chat = func(ctx context.Context, request domainllm.ChatRequest) (*domainllm.ChatResponse, error) {
				if ctx == streamContext || ctx.Err() != nil || len(request.Tools) != 0 {
					t.Fatal("fallback did not get a fresh tool-free request")
				}
				return &domainllm.ChatResponse{Message: domainllm.Message{Content: "回退结论"}}, nil
			}
			state := testRunState(t, NewRegistry(), RunOptions{SynthesizeAfterTools: true, OnEvent: func(string, any) {}}, p)
			state.config.ModelTimeout = time.Second
			state.ledger.Traces = []Trace{{ToolName: "evidence", Status: "ok", OutputSummary: "证据"}}
			text, reasoning, err := state.synthesizeToolAnswer()
			if err != nil || !stream.closed {
				t.Fatalf("stream not closed or failed: %v", err)
			}
			if failStream {
				if text != "回退结论" || reasoning != "" || len(p.requests) != 1 {
					t.Fatalf("fallback=%s/%s", text, reasoning)
				}
			} else if text != "结论" || reasoning != "依据" || len(p.requests) != 0 {
				t.Fatalf("stream=%s/%s", text, reasoning)
			}
		})
	}
}

func TestRunFailsWhenCompletionBudgetIsExhausted(t *testing.T) {
	p := &scriptedProvider{chat: func(context.Context, domainllm.ChatRequest) (*domainllm.ChatResponse, error) {
		return &domainllm.ChatResponse{Message: domainllm.Message{Content: "草稿"}}, nil
	}}
	_, err := testRunState(t, NewRegistry(), RunOptions{MaxToolCalls: 1, RequiredTool: "document.create"}, p).run()
	if err == nil || !strings.Contains(err.Error(), "必须成功调用 document.create") || len(p.requests) != 2 {
		t.Fatalf("err=%v requests=%d", err, len(p.requests))
	}
}

func TestSummarizeTraceInputKeepsGenericJSONFields(t *testing.T) {
	summary := summarizeTraceInput(json.RawMessage(`{"document":"text","section_id":11}`))
	if !strings.Contains(summary, `"document":"text"`) || !strings.Contains(summary, `"section_id":11`) {
		t.Fatalf("generic trace input summary lost fields: %s", summary)
	}
}

func TestIsolatedToolRoundDoesNotReplaySuccessfulMutations(t *testing.T) {
	messages := isolatedToolRoundMessages(
		[]domainllm.Message{{Role: "user", Content: "继续创建节点"}},
		[]Trace{
			{ToolName: "outline.create", Kind: KindMutation, Status: "ok", OutputSummary: `{"id":44,"title":"渗透与预警"}`},
			{ToolName: "outline.create", Kind: KindMutation, Status: "error", Error: "title and core_goal are required"},
		},
		normalizeRunOptions(RunOptions{RequiredAnyTools: []string{"outline.create", "outline.update"}}),
	)
	content := messages[len(messages)-1].Content
	for _, required := range []string{"任务目标没有改变", "outline.create 或 outline.update", "不得重放已经成功的 mutation", "只修正该失败调用", "渗透与预警", "title and core_goal are required"} {
		if !strings.Contains(content, required) {
			t.Fatalf("isolated correction prompt does not contain %q: %s", required, content)
		}
	}
}

func TestIsolatedToolProgressContextStaysBoundedAndKeepsGoalState(t *testing.T) {
	traces := make([]Trace, 0, 32)
	for index := 0; index < 32; index++ {
		traces = append(traces, Trace{
			ToolName:      "catalog.update",
			Kind:          KindMutation,
			Input:         json.RawMessage(`{"record_id":51,"content":{"state":"` + strings.Repeat("长", 500) + `"}}`),
			OutputSummary: `{"id":51,"updated":true,"detail":"` + strings.Repeat("更", 500) + `"}`,
			Status:        "ok",
		})
	}
	context := isolatedToolProgressContext(traces, 18000)
	if len([]rune(context)) > 18000 {
		t.Fatalf("progress context has %d runes, want <= 18000", len([]rune(context)))
	}
	if !strings.Contains(context, "32. catalog.update [成功]") || !strings.Contains(context, `"record_id":51`) {
		t.Fatalf("progress ledger lost completion state: %s", context)
	}
}

func TestIsolatedToolProgressContextKeepsExpandedQueryEvidence(t *testing.T) {
	fullSnapshot := `{"records":"` + strings.Repeat("条", 1000) + `快照末尾"}`
	context := isolatedToolProgressContext([]Trace{
		{
			ToolName:      "document.search",
			Kind:          KindQuery,
			Input:         json.RawMessage(`{"query":"项目状态"}`),
			OutputSummary: "检索证据",
			outputContext: "项目处于已审核状态",
			Status:        "ok",
		},
		{
			ToolName:      "catalog.snapshot",
			Kind:          KindQuery,
			Input:         json.RawMessage(`{"scope":"current"}`),
			OutputSummary: "目录短摘要",
			outputContext: fullSnapshot,
			Status:        "ok",
		},
	}, 18000)
	if !strings.Contains(context, "快照末尾") {
		t.Fatalf("character snapshot was compacted too aggressively: %s", context)
	}
	if !strings.Contains(context, "项目处于已审核状态") {
		t.Fatalf("distinct knowledge evidence was lost: %s", context)
	}
}

func TestOutputLimitRecoveryPromptRequiresImmediateConvergence(t *testing.T) {
	messages := outputLimitRecoveryMessages(
		[]domainllm.Message{{Role: "user", Content: "创建文档"}},
		normalizeRunOptions(RunOptions{RequiredTools: []string{"knowledge.create"}}),
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
	messages := outputLimitRecoveryMessages([]domainllm.Message{{Role: "user", Content: "生成 40 条记录"}}, normalizeRunOptions(RunOptions{}))
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
