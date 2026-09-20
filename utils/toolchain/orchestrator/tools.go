package orchestrator

import (
	"context"
	"encoding/json"
	"fmt"
	"strings"
	"time"

	domainllm "InkFlow/internal/ai/llm"
	llmutil "InkFlow/utils/llm"
)

type preparedToolCall struct {
	call       domainllm.ToolCall
	name       string
	tool       Tool
	arguments  json.RawMessage
	eventInput any
	queryKey   string
}

type toolCallBatchResult struct {
	result       *RunResult
	limitReached bool
}

type callPreparation struct {
	call         *preparedToolCall
	result       *RunResult
	limitReached bool
}

type callExecution struct {
	result           *RunResult
	recoverableError error
}

func (state *runState) executeToolCallBatch(calls []domainllm.ToolCall) (toolCallBatchResult, error) {
	batch := toolCallBatchResult{}
	limitEventSent := false
	for index, call := range calls {
		prepared, err := state.prepareToolCall(call, &limitEventSent)
		if err != nil {
			return batch, err
		}
		batch.limitReached = batch.limitReached || prepared.limitReached
		if prepared.result != nil {
			batch.result = prepared.result
			return batch, nil
		}
		if prepared.call == nil {
			continue
		}

		execution, err := state.executePreparedToolCall(*prepared.call)
		if err != nil {
			return batch, err
		}
		if execution.result != nil {
			batch.result = execution.result
			return batch, nil
		}
		if execution.recoverableError != nil {
			state.messages = appendSkippedToolResults(state.messages, calls[index+1:], prepared.call.name, execution.recoverableError)
			break
		}
	}
	return batch, nil
}

func (state *runState) prepareToolCall(call domainllm.ToolCall, limitEventSent *bool) (callPreparation, error) {
	toolName, tool, ok := state.registry.ResolveLLMTool(call.Name)
	if !ok {
		payload := map[string]any{"error": "tool not found", "tool_name": call.Name}
		emitRunEvent(state.config, "tool_error", payload)
		state.messages = append(state.messages, toolResultMessage(call.ID, payload))
		return callPreparation{}, nil
	}
	if state.skipCallAtPerRunAttemptLimit(call, toolName, tool) {
		return callPreparation{}, nil
	}
	state.ledger.AttemptedCalls[toolName]++

	arguments, eventInput, err := decodeToolArguments(string(call.Arguments))
	if err != nil {
		return state.handleInvalidArguments(call, toolName, err)
	}
	state.ledger.InvalidArguments[toolName] = 0
	if state.reuseRunOnceResult(call, toolName, tool) || state.skipCallAtPerRunLimit(call, toolName, tool) {
		return callPreparation{}, nil
	}

	queryKey := ""
	if tool.Kind == KindQuery {
		queryKey = toolQueryCacheKey(state.ledger.MutationVersion, toolName, eventInput)
		if result, reused, err := state.reuseQueryResult(call, toolName, queryKey); reused || err != nil {
			return callPreparation{result: result}, err
		}
	}
	if payload := state.toolLimitPayload(toolName, tool); payload != nil {
		if !*limitEventSent {
			emitRunEvent(state.config, "tool_error", payload)
			*limitEventSent = true
		}
		state.messages = append(state.messages, toolResultMessage(call.ID, payload))
		return callPreparation{limitReached: true}, nil
	}
	if tool.Kind == KindMutation {
		state.ledger.MutationCalls++
	}
	return callPreparation{call: &preparedToolCall{
		call: call, name: toolName, tool: tool, arguments: arguments, eventInput: eventInput, queryKey: queryKey,
	}}, nil
}

func (state *runState) handleInvalidArguments(call domainllm.ToolCall, toolName string, cause error) (callPreparation, error) {
	state.ledger.InvalidArguments[toolName]++
	payload := map[string]any{"error": cause.Error(), "tool_name": toolName}
	emitRunEvent(state.config, "tool_error", payload)
	state.messages = append(state.messages, toolResultMessage(call.ID, payload))
	if state.ledger.InvalidArguments[toolName] < maxInvalidToolArgumentCalls {
		return callPreparation{}, nil
	}
	return callPreparation{}, fmt.Errorf(
		"工具 %s 连续 %d 次返回了不完整或非法的 JSON 参数；请一次只处理一个对象、缩短参数并严格按工具 schema 重试: %w",
		toolName,
		maxInvalidToolArgumentCalls,
		cause,
	)
}

func (state *runState) executePreparedToolCall(prepared preparedToolCall) (callExecution, error) {
	result, trace, err := state.runToolWithRetry(prepared)
	if err == nil {
		state.recordSuccessfulTool(prepared, trace)
	}
	payload := toolCallPayload(result, trace, err)
	if err != nil {
		payload["error"] = err.Error()
	}
	terminal, shouldStop := result.(TerminalResult)
	if err == nil && prepared.tool.TerminalOnSuccess {
		terminal = TerminalResult{Result: result, Message: fmt.Sprintf("%s 已完成当前受控步骤", prepared.name)}
		shouldStop = true
	}
	if shouldStop {
		payload["result"] = terminal.Result
	}
	state.messages = append(state.messages, toolResultMessage(prepared.call.ID, payload))

	if err != nil && !prepared.tool.StopOnError {
		state.ledger.RecoverableErrors[prepared.name]++
		if state.ledger.RecoverableErrors[prepared.name] >= maxRecoverableToolErrors {
			return callExecution{}, fmt.Errorf("工具 %s 连续 %d 次调用失败，模型未能修正参数或调用条件: %w", prepared.name, maxRecoverableToolErrors, err)
		}
		emitRunEvent(state.config, "status", map[string]any{
			"message":   fmt.Sprintf("%s 调用失败，正在让模型根据错误修正参数；已成功的工具调用不会重放", prepared.name),
			"tool_name": prepared.name,
			"attempt":   state.ledger.RecoverableErrors[prepared.name],
			"error":     err.Error(),
		})
		return callExecution{recoverableError: err}, nil
	}
	if err != nil {
		return callExecution{}, fmt.Errorf("工具 %s 调用失败: %w", prepared.name, err)
	}
	if shouldStop {
		message := strings.TrimSpace(terminal.Message)
		if message == "" {
			message = summarizeToolTraces(state.ledger.Traces)
		}
		result := &RunResult{Message: message, Traces: state.ledger.Traces}
		emitRunEvent(state.config, "done", result)
		return callExecution{result: result}, nil
	}
	return callExecution{}, nil
}

// runToolWithRetry preserves business retries for one Agent tool call. It does
// not implement Dispatcher queueing, rate limits, caching, or singleflight.
// The legacy retry helpers retain the existing error classification and delays.
func (state *runState) runToolWithRetry(prepared preparedToolCall) (result any, trace Trace, err error) {
	attempts := llmutil.MaxAttempts(prepared.tool.MaxRetries)
	for attempt := 1; attempt <= attempts; attempt++ {
		emitRunEvent(state.config, "tool_start", map[string]any{
			"tool_name": prepared.name,
			"kind":      prepared.tool.Kind,
			"input":     prepared.eventInput,
			"attempt":   attempt,
		})
		result, trace, err = executeTool(state.ctx, state.registry, state.executor, state.config.UserName, prepared.name, prepared.arguments)
		state.ledger.Traces = append(state.ledger.Traces, trace)
		emitRunEvent(state.config, "tool_done", trace)
		if err == nil || attempt == attempts || !llmutil.IsRetryableError(err) {
			return result, trace, err
		}
		delay := llmutil.RetryDelay(attempt)
		emitRunEvent(state.config, "tool_retry", map[string]any{
			"tool_name": prepared.name,
			"kind":      prepared.tool.Kind,
			"attempt":   attempt,
			"delay_ms":  delay.Milliseconds(),
			"error":     err.Error(),
			"message":   fmt.Sprintf("%s 第 %d 次调用失败，%s 后重试", prepared.name, attempt, delay),
		})
		select {
		case <-state.ctx.Done():
			return nil, trace, state.ctx.Err()
		case <-time.After(delay):
		}
	}
	return result, trace, err
}

func (state *runState) recordSuccessfulTool(prepared preparedToolCall, trace Trace) {
	state.ledger.RecoverableErrors[prepared.name] = 0
	state.ledger.SuccessfulCalls[prepared.name]++
	if prepared.tool.RunOncePerRun {
		state.ledger.SuccessfulRunOnce[prepared.name] = trace
	}
	if prepared.tool.Kind == KindQuery && prepared.queryKey != "" {
		state.ledger.SuccessfulQueries[prepared.queryKey] = trace
	}
	if prepared.tool.Kind == KindMutation {
		state.ledger.MutationVersion++
	}
	if prepared.tool.Kind == KindLLM {
		state.ledger.LLMCalls++
	}
}

func executeTool(ctx context.Context, registry *Registry, executor Executor, userName, name string, arguments json.RawMessage) (any, Trace, error) {
	if executor != nil {
		return executor.Execute(ctx, userName, name, arguments)
	}
	return registry.Call(ctx, name, arguments)
}

func decodeToolArguments(arguments string) (json.RawMessage, any, error) {
	arguments = strings.TrimSpace(arguments)
	if arguments == "" {
		arguments = "{}"
	}
	raw := json.RawMessage(arguments)
	var input any
	if err := json.Unmarshal(raw, &input); err != nil {
		return nil, nil, fmt.Errorf("invalid tool arguments JSON: %w", err)
	}
	return raw, input, nil
}

// toolResultMessage creates a provider-neutral tool result message and keeps
// the response paired with the originating call ID.
func toolResultMessage(toolCallID string, payload any) domainllm.Message {
	content, _ := json.Marshal(payload)
	return domainllm.Message{Role: "tool", ToolCallID: toolCallID, Content: string(content)}
}

func appendSkippedToolResults(messages []domainllm.Message, calls []domainllm.ToolCall, failedTool string, cause error) []domainllm.Message {
	for _, call := range calls {
		payload := map[string]any{
			"ok":        false,
			"skipped":   true,
			"tool_name": call.Name,
			"error":     fmt.Sprintf("skipped because the earlier tool %s failed: %v", failedTool, cause),
		}
		messages = append(messages, toolResultMessage(call.ID, payload))
	}
	return messages
}

func toolCallPayload(result any, trace Trace, err error) map[string]any {
	payload := map[string]any{"ok": err == nil}
	if err != nil || !trace.outputTrimmed {
		payload["result"] = result
		return payload
	}
	payload["result_summary"] = trace.outputContext
	payload["result_truncated"] = true
	return payload
}

func (state *runState) reuseRunOnceResult(call domainllm.ToolCall, toolName string, tool Tool) bool {
	previous, exists := completedRunOnce(toolName, tool, state.ledger)
	if !exists {
		return false
	}
	payload := map[string]any{
		"ok":      true,
		"reused":  true,
		"summary": previous.OutputSummary,
		"message": fmt.Sprintf("%s 已在本轮成功执行；请使用已有结果继续任务，不要再次调用", toolName),
	}
	state.messages = append(state.messages, toolResultMessage(call.ID, payload))
	emitRunEvent(state.config, "status", map[string]any{
		"message":   fmt.Sprintf("%s 已在本轮执行，跳过重复调用", toolName),
		"tool_name": toolName,
	})
	return true
}

func (state *runState) skipCallAtPerRunLimit(call domainllm.ToolCall, toolName string, tool Tool) bool {
	if !perRunLimitReached(tool, state.ledger.SuccessfulCalls[toolName]) {
		return false
	}
	payload := map[string]any{
		"ok":      true,
		"skipped": true,
		"message": fmt.Sprintf("%s 已达到本轮 %d 次调用上限；请使用已收集结果继续任务", toolName, tool.MaxCallsPerRun),
	}
	state.messages = append(state.messages, toolResultMessage(call.ID, payload))
	emitRunEvent(state.config, "status", map[string]any{"message": payload["message"], "tool_name": toolName})
	return true
}

func (state *runState) skipCallAtPerRunAttemptLimit(call domainllm.ToolCall, toolName string, tool Tool) bool {
	if !perRunAttemptLimitReached(tool, state.ledger.AttemptedCalls[toolName]) {
		return false
	}
	payload := map[string]any{
		"ok":      true,
		"skipped": true,
		"message": fmt.Sprintf("%s 已达到本轮 %d 次调用尝试上限；请使用已收集结果继续任务", toolName, tool.MaxAttemptsPerRun),
	}
	state.messages = append(state.messages, toolResultMessage(call.ID, payload))
	emitRunEvent(state.config, "status", map[string]any{"message": payload["message"], "tool_name": toolName})
	return true
}

func (state *runState) reuseQueryResult(call domainllm.ToolCall, toolName, queryKey string) (*RunResult, bool, error) {
	previous, exists := state.ledger.SuccessfulQueries[queryKey]
	if !exists {
		return nil, false, nil
	}
	payload := map[string]any{"ok": true, "reused": true, "summary": previous.OutputSummary}
	state.messages = append(state.messages, toolResultMessage(call.ID, payload))
	emitRunEvent(state.config, "status", map[string]any{
		"message":   fmt.Sprintf("%s 查询参数及数据版本未变化，复用上一结果", toolName),
		"tool_name": toolName,
	})
	if !hasCompletionRequirement(state.config.Completion) || !completionRequirementSatisfied(state.ledger.Traces, state.config.Completion) {
		return nil, true, nil
	}
	result, err := state.finishToolRun()
	return result, true, err
}
