package orchestrator

import (
	"context"
	"fmt"
	"strings"
	"time"

	domainllm "InkFlow/internal/ai/llm"
	"InkFlow/utils"
	llmutil "InkFlow/utils/llm"
)

// RunOptions controls model/tool budgets, completion conditions and event delivery.
type RunOptions struct {
	UserName string
	Executor Executor

	MaxToolCalls          int
	MaxLLMToolCalls       int
	MaxLLMRetries         int
	MaxMutationToolCalls  int
	MaxIsolatedToolRounds int

	RequiredTool           string
	RequiredTools          []string
	RequiredAnyTools       []string
	RequiredToolCallCounts map[string]int
	IncompleteMessage      string

	ReturnAfterToolCalls       bool
	SynthesizeAfterTools       bool
	AlwaysSynthesizeAfterTools bool
	// ContextCompactor is an internal, service-supplied hook. It is never
	// registered as an LLM tool: the runtime invokes it only once the model
	// context crosses ContextCompactionMaxRunes.
	ContextCompactionMaxRunes int
	ContextCompactor          ContextCompactor
	ContextCompactionTool     string
	OnEvent                   func(event string, payload any)
	LLM                       *llmutil.GenerateOptions
}

// ContextItem is one complete, still-visible tool result passed to an internal
// context compactor. Output deliberately contains the full context payload,
// not the short audit summary returned by the API.
type ContextItem struct {
	ToolName string
	Kind     Kind
	Input    string
	Output   string
}

// ContextCompactor returns a compact replacement for the currently visible
// tool results. It is not a Tool Handler and can never appear in LLMTools.
type ContextCompactor func(ctx context.Context, items []ContextItem) (string, error)

func (options RunOptions) withDefaults() RunOptions {
	if options.MaxToolCalls <= 0 {
		options.MaxToolCalls = 6
	}
	if options.MaxLLMToolCalls <= 0 {
		options.MaxLLMToolCalls = 2
	}
	if options.MaxLLMRetries <= 0 {
		options.MaxLLMRetries = 2
	}
	if options.MaxMutationToolCalls <= 0 {
		options.MaxMutationToolCalls = 1
	}
	if options.MaxIsolatedToolRounds <= 0 {
		options.MaxIsolatedToolRounds = 2
	}
	return options
}

// RunResult contains the final model text and the complete execution trace.
type RunResult struct {
	Message   string  `json:"message"`
	Reasoning string  `json:"reasoning,omitempty"`
	Traces    []Trace `json:"traces"`
}

const (
	maxInvalidToolArgumentCalls = 5
	maxRecoverableToolErrors    = 5
)

// RunLedger owns execution evidence and counters for one run, never across users.
type RunLedger struct {
	Traces            []Trace
	MutationVersion   int
	LLMCalls          int // Successful LLM tools, not model decision requests.
	MutationCalls     int // Attempted mutation calls, including failures.
	InvalidArguments  map[string]int
	RecoverableErrors map[string]int
	SuccessfulQueries map[string]Trace
	SuccessfulRunOnce map[string]Trace
	SuccessfulCalls   map[string]int
	AttemptedCalls    map[string]int
}

type Budget struct {
	MaxRounds         int
	MaxModelRetries   int
	MaxLLMTools       int
	MaxMutations      int
	MaxIsolatedRounds int
}

type CompletionPolicy struct {
	RequiredAll       []string
	RequiredAny       []string
	RequiredCounts    map[string]int
	IncompleteMessage string
}

type SynthesisPolicy struct{ Enabled, Always bool }

// RunConfig is the normalized per-run configuration; callers keep using RunOptions.
type RunConfig struct {
	UserName                  string
	Budget                    Budget
	Completion                CompletionPolicy
	Synthesis                 SynthesisPolicy
	ReturnAfterToolCalls      bool
	ContextCompactionMaxRunes int
	ContextCompactor          ContextCompactor
	ContextCompactionTool     string
	OnEvent                   func(event string, payload any)
	ModelContext              context.Context
	ModelTimeout              time.Duration
}

func normalizeRunOptions(options RunOptions) RunConfig {
	options = options.withDefaults()
	required := make([]string, 0, len(options.RequiredTools)+1)
	seen := map[string]bool{}
	for _, name := range append([]string{options.RequiredTool}, options.RequiredTools...) {
		name = strings.TrimSpace(name)
		if name != "" && !seen[name] {
			required = append(required, name)
			seen[name] = true
		}
	}
	counts := make(map[string]int, len(options.RequiredToolCallCounts))
	for name, count := range options.RequiredToolCallCounts {
		name = strings.TrimSpace(name)
		if name != "" && count > counts[name] {
			counts[name] = count
		}
	}
	return RunConfig{
		UserName:                  options.UserName,
		Budget:                    Budget{MaxRounds: options.MaxToolCalls, MaxModelRetries: options.MaxLLMRetries, MaxLLMTools: options.MaxLLMToolCalls, MaxMutations: options.MaxMutationToolCalls, MaxIsolatedRounds: options.MaxIsolatedToolRounds},
		Completion:                CompletionPolicy{RequiredAll: required, RequiredAny: append([]string(nil), options.RequiredAnyTools...), RequiredCounts: counts, IncompleteMessage: options.IncompleteMessage},
		Synthesis:                 SynthesisPolicy{Enabled: options.SynthesizeAfterTools, Always: options.AlwaysSynthesizeAfterTools},
		ReturnAfterToolCalls:      options.ReturnAfterToolCalls,
		ContextCompactionMaxRunes: options.ContextCompactionMaxRunes,
		ContextCompactor:          options.ContextCompactor,
		ContextCompactionTool:     strings.TrimSpace(options.ContextCompactionTool),
		OnEvent:                   options.OnEvent,
	}
}

// runState coordinates one invocation using normalized config and its own ledger.
type runState struct {
	ctx              context.Context
	registry         *Registry
	executor         Executor
	originalMessages []domainllm.Message
	messages         []domainllm.Message
	llm              domainllm.Provider
	modelRequest     domainllm.ChatRequest
	synthesisRequest domainllm.ChatRequest
	config           RunConfig
	ledger           RunLedger
}

func newRunState(ctx context.Context, messages []domainllm.Message, registry *Registry, options RunOptions) (*runState, error) {
	if registry == nil {
		return nil, fmt.Errorf("tool registry is nil")
	}
	if ctx == nil {
		ctx = context.Background()
	}
	config := normalizeRunOptions(options)
	llmOptions := llmutil.GenerateOptions{
		Context:     ctx,
		Temperature: 0.4,
		MaxTokens:   4096,
		Tools:       registry.LLMTools(),
		ToolChoice:  "auto",
	}
	if options.LLM != nil {
		llmOptions = *options.LLM
		llmOptions.Tools = registry.LLMTools()
		if llmOptions.Context == nil {
			llmOptions.Context = ctx
		}
		if llmOptions.ToolChoice == nil {
			llmOptions.ToolChoice = "auto"
		}
	}
	// This is the sole legacy options boundary. All runtime model calls use Provider.
	provider, request, err := llmutil.PrepareChatRequest(nil, llmOptions)
	if err != nil {
		return nil, err
	}
	config.ModelContext, config.ModelTimeout = llmOptions.Context, llmOptions.Timeout
	config.Synthesis.Enabled = config.Synthesis.Enabled && options.LLM != nil
	synthesis := request
	synthesis.Tools, synthesis.ToolChoice = nil, nil
	synthesis.Reasoning = &domainllm.Reasoning{Enabled: false}
	maxTokens, temperature := llmOptions.MaxTokens, llmOptions.Temperature
	if maxTokens <= 0 || maxTokens > 2048 {
		maxTokens = 2048
	}
	if temperature <= 0 || temperature > 0.4 {
		temperature = 0.25
	}
	synthesis.MaxTokens, synthesis.Temperature = &maxTokens, &temperature
	return &runState{
		ctx: ctx, registry: registry, executor: options.Executor, config: config,
		originalMessages: append([]domainllm.Message(nil), messages...),
		messages:         append([]domainllm.Message(nil), messages...), llm: provider, modelRequest: request, synthesisRequest: synthesis,
		ledger: RunLedger{
			InvalidArguments: map[string]int{}, RecoverableErrors: map[string]int{},
			SuccessfulQueries: map[string]Trace{}, SuccessfulRunOnce: map[string]Trace{}, SuccessfulCalls: map[string]int{}, AttemptedCalls: map[string]int{},
		},
	}, nil
}

// RunWithTools drives model decisions and tool execution until a final answer
// or a configured completion condition is reached.
func RunWithTools(ctx context.Context, messages []domainllm.Message, registry *Registry, options RunOptions) (*RunResult, error) {
	state, err := newRunState(ctx, messages, registry, options)
	if err != nil {
		return nil, err
	}
	return state.run()
}

func (state *runState) run() (*RunResult, error) {
	for step := 0; step <= state.config.Budget.MaxRounds; step++ {
		message, earlyResult, err := state.requestModelDecision(step)
		if err != nil || earlyResult != nil {
			return earlyResult, err
		}
		if len(message.ToolCalls) == 0 {
			result, continueRun, err := state.handleTextResponse(step, message)
			if err != nil || !continueRun {
				return result, err
			}
			continue
		}

		state.messages = append(state.messages, message)
		traceStart := len(state.ledger.Traces)
		batch, err := state.executeToolCallBatch(message.ToolCalls)
		if err != nil || batch.result != nil {
			return batch.result, err
		}
		if batch.limitReached {
			if hasCompletionRequirement(state.config.Completion) && !completionRequirementSatisfied(state.ledger.Traces, state.config.Completion) {
				return nil, fmt.Errorf("工具链流程未完成：调用额度已用尽，但 %s 尚未成功", completionRequirementLabel(state.config.Completion))
			}
			return state.finishToolRun()
		}
		if err := state.compactToolContextIfNeeded(traceStart); err != nil {
			return nil, err
		}
		if result, continueRun, err := state.finishIsolatedRound(step); err != nil || !continueRun {
			return result, err
		}
	}
	return state.finishAfterCallLimit()
}

func (state *runState) finishIsolatedRound(step int) (*RunResult, bool, error) {
	if !state.config.ReturnAfterToolCalls {
		return nil, true, nil
	}
	if step+1 < state.config.Budget.MaxIsolatedRounds {
		emitRunEvent(state.config, "status", map[string]any{"message": "正在根据已有工具结果判断是否需要继续调用工具", "step": step + 1})
		state.messages = isolatedToolRoundMessages(state.originalMessages, state.ledger.Traces, state.config)
		return nil, true, nil
	}
	if hasCompletionRequirement(state.config.Completion) && !completionRequirementSatisfied(state.ledger.Traces, state.config.Completion) {
		return nil, false, fmt.Errorf("工具链流程未完成：隔离工具轮次已达上限，但 %s 尚未成功", completionRequirementLabel(state.config.Completion))
	}
	result, err := state.finishToolRun()
	return result, false, err
}

func (state *runState) compactToolContextIfNeeded(retainTraceStart int) error {
	if state.config.ContextCompactor == nil || state.config.ContextCompactionMaxRunes <= 0 || retainTraceStart <= 0 || messageRunes(state.messages) <= state.config.ContextCompactionMaxRunes {
		return nil
	}
	if retainTraceStart > len(state.ledger.Traces) {
		retainTraceStart = len(state.ledger.Traces)
	}
	items := make([]ContextItem, 0, retainTraceStart)
	for index := 0; index < retainTraceStart; index++ {
		trace := state.ledger.Traces[index]
		if trace.contextArchived || trace.Status != "ok" || (trace.Kind != KindQuery && trace.Kind != KindLLM) {
			continue
		}
		output := strings.TrimSpace(trace.outputContext)
		if output == "" {
			output = strings.TrimSpace(trace.OutputSummary)
		}
		if output == "" {
			continue
		}
		items = append(items, ContextItem{ToolName: trace.ToolName, Kind: trace.Kind, Input: string(trace.Input), Output: output})
	}
	if len(items) == 0 {
		return nil
	}
	toolName := state.config.ContextCompactionTool
	if toolName == "" {
		toolName = "context.compress"
	}
	emitRunEvent(state.config, "tool_start", map[string]any{"tool_name": toolName, "kind": KindLLM, "input": map[string]any{"source_count": len(items), "automatic": true}})
	summary, err := state.config.ContextCompactor(state.ctx, items)
	if err != nil {
		trace := Trace{ToolName: toolName, Kind: KindLLM, Status: "error", Error: err.Error(), CreatedAt: time.Now()}
		state.ledger.Traces = append(state.ledger.Traces, trace)
		emitRunEvent(state.config, "tool_done", trace)
		return fmt.Errorf("自动压缩工具上下文失败: %w", err)
	}
	summary = strings.TrimSpace(summary)
	if summary == "" {
		return fmt.Errorf("自动压缩工具上下文失败: 未返回摘要")
	}
	for index := 0; index < retainTraceStart; index++ {
		trace := &state.ledger.Traces[index]
		if trace.Status == "ok" && !trace.contextArchived && (trace.Kind == KindQuery || trace.Kind == KindLLM) {
			trace.contextArchived = true
		}
	}
	trace := Trace{
		ToolName: toolName, Kind: KindLLM, Status: "ok", OutputSummary: utils.TruncateRunes(summary, 2000),
		CreatedAt: time.Now(), outputContext: summary,
	}
	state.ledger.Traces = append(state.ledger.Traces, trace)
	emitRunEvent(state.config, "tool_done", trace)
	state.messages = isolatedToolRoundMessages(state.originalMessages, state.ledger.Traces, state.config)
	state.messages = append(state.messages, currentToolResultsMessage(state.ledger.Traces, retainTraceStart))
	return nil
}

func currentToolResultsMessage(traces []Trace, start int) domainllm.Message {
	if start < 0 {
		start = 0
	}
	if start >= len(traces) {
		return domainllm.Message{Role: "user", Content: "服务端已自动压缩较早的工具上下文，请继续处理原始任务。"}
	}
	var content strings.Builder
	content.WriteString("服务端已自动压缩较早的工具上下文。以下是本次刚返回、尚未压缩的完整工具结果；应与压缩摘要一起使用：\n")
	for index := start; index < len(traces); index++ {
		trace := traces[index]
		if trace.contextArchived || trace.Status != "ok" || (trace.Kind != KindQuery && trace.Kind != KindLLM) {
			continue
		}
		output := strings.TrimSpace(trace.outputContext)
		if output == "" {
			output = strings.TrimSpace(trace.OutputSummary)
		}
		if output == "" {
			continue
		}
		fmt.Fprintf(&content, "\n## %s\n%s\n", trace.ToolName, output)
	}
	return domainllm.Message{Role: "user", Content: content.String()}
}

func messageRunes(messages []domainllm.Message) int {
	total := 0
	for _, message := range messages {
		total += len([]rune(message.Content))
	}
	return total
}

func (state *runState) finishAfterCallLimit() (*RunResult, error) {
	if hasCompletionRequirement(state.config.Completion) && !completionRequirementSatisfied(state.ledger.Traces, state.config.Completion) {
		return nil, fmt.Errorf("工具链流程未完成：工具调用次数已达上限，但 %s 尚未成功", completionRequirementLabel(state.config.Completion))
	}
	message := "工具调用次数已达上限，请根据已有工具结果给出当前最可靠的回答。"
	reasoning := ""
	if state.config.Synthesis.Enabled && len(state.ledger.Traces) > 0 {
		message, reasoning = state.synthesizeOrSummarize()
	}
	result := &RunResult{Message: message, Reasoning: reasoning, Traces: state.ledger.Traces}
	emitRunEvent(state.config, "done", result)
	return result, nil
}

func (state *runState) finishToolRun() (*RunResult, error) {
	message, reasoning := state.synthesizeOrSummarize()
	result := &RunResult{Message: message, Reasoning: reasoning, Traces: state.ledger.Traces}
	emitRunEvent(state.config, "done", result)
	return result, nil
}

func emitRunEvent(options RunConfig, event string, payload any) {
	if options.OnEvent != nil {
		options.OnEvent(event, payload)
	}
}
