package orchestrator

import (
	"context"
	"fmt"
	"strings"
	"time"

	domainllm "InkFlow/internal/ai/llm"
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
	OnEvent                    func(event string, payload any)
	LLM                        *llmutil.GenerateOptions
}

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
	UserName             string
	Budget               Budget
	Completion           CompletionPolicy
	Synthesis            SynthesisPolicy
	ReturnAfterToolCalls bool
	OnEvent              func(event string, payload any)
	ModelContext         context.Context
	ModelTimeout         time.Duration
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
		UserName:             options.UserName,
		Budget:               Budget{MaxRounds: options.MaxToolCalls, MaxModelRetries: options.MaxLLMRetries, MaxLLMTools: options.MaxLLMToolCalls, MaxMutations: options.MaxMutationToolCalls, MaxIsolatedRounds: options.MaxIsolatedToolRounds},
		Completion:           CompletionPolicy{RequiredAll: required, RequiredAny: append([]string(nil), options.RequiredAnyTools...), RequiredCounts: counts, IncompleteMessage: options.IncompleteMessage},
		Synthesis:            SynthesisPolicy{Enabled: options.SynthesizeAfterTools, Always: options.AlwaysSynthesizeAfterTools},
		ReturnAfterToolCalls: options.ReturnAfterToolCalls, OnEvent: options.OnEvent,
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
