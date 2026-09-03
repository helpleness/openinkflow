package toolchain

import (
	"context"
	"encoding/json"
	"fmt"
	"strings"
	"time"

	llmutil "InkFlow/utils/llm"
)

const (
	// 连续返回非 JSON 参数时，继续重试通常无意义；限制次数以避免无效循环。
	maxInvalidToolArgumentCalls = 5
	// 非致命工具错误交给模型纠正，但同一工具连续失败过多时必须终止。
	maxRecoverableToolErrors = 5
)

// RunOptions 控制一轮工具编排的调用额度、完成条件、模型选项和事件回调。
type RunOptions struct {
	// UserName 是本次编排所属的认证用户。配置 Executor 后，它用于公平队列与请求缓存隔离。
	UserName string
	// Executor 可选的应用层工具执行器。空值时保持直接调用 Registry.Call 的原有行为。
	Executor Executor
	// MaxToolCalls 是“模型决策并执行工具”的最大编排轮数；一轮中可包含多个 tool call。
	MaxToolCalls int
	// MaxLLMToolCalls 限制 KindLLM 工具成功调用的总次数，防止嵌套模型调用失控。
	MaxLLMToolCalls int
	// MaxLLMRetries 控制模型请求遇到临时错误时的重试策略。
	MaxLLMRetries int
	// MaxMutationToolCalls 限制 KindMutation 工具的调用次数；失败调用同样消耗额度。
	MaxMutationToolCalls int
	// MaxIsolatedToolRounds 仅在 ReturnAfterToolCalls 为 true 时生效，限制隔离执行轮数。
	MaxIsolatedToolRounds int
	// RequiredTool 要求指定工具至少成功一次，否则模型的纯文本回复不会结束本轮流程。
	RequiredTool string
	// RequiredTools 中每个工具都必须至少成功一次。
	RequiredTools []string
	// RequiredAnyTools 中任意一个工具成功即可满足这部分完成条件。
	RequiredAnyTools []string
	// RequiredToolCallCounts 要求每个指定工具达到对应的成功调用次数。
	// 适用于服务端已批准的批量计划：一次成功写入不足以完成任务的场景。
	RequiredToolCallCounts map[string]int
	// IncompleteMessage 是完成条件未满足时追加给模型的纠正指令；空值使用默认指令。
	IncompleteMessage string
	// ReturnAfterToolCalls 为 true 时，每轮工具执行后会重建消息上下文，而非直接续接模型对话。
	// 这可以避免模型重放已成功的写操作。
	ReturnAfterToolCalls bool
	// SynthesizeAfterTools 为 true 时，工具完成后可额外请求模型基于轨迹生成最终结论。
	SynthesizeAfterTools bool
	// AlwaysSynthesizeAfterTools 为 true 时，即使编排模型已有文本回复，也强制使用专门的
	// 流式总结阶段生成最终答案。该选项依赖 SynthesizeAfterTools 和 LLM 同时启用。
	AlwaysSynthesizeAfterTools bool
	// OnEvent 接收编排过程事件，常用于 SSE 推送；回调不应阻塞或修改编排状态。
	OnEvent func(event string, payload any)
	// LLM 覆盖默认的模型生成参数；Tools 和 ToolChoice 由编排器根据注册表统一设置。
	LLM *llmutil.GenerateOptions
}

// RunResult 是编排结束后返回给 API 层的最终文本、推理文本和完整调用轨迹。
type RunResult struct {
	Message   string  `json:"message"`
	Reasoning string  `json:"reasoning,omitempty"`
	Traces    []Trace `json:"traces"`
}

// RunWithTools 按“模型决策、工具执行、结果回传、必要时二次总结”的循环完成任务。
//
// 每轮先让模型从当前可用工具中选择调用，再把每个调用的成功结果或错误作为 tool 消息写回。
// 成功查询会按“数据版本 + 工具名 + 参数”复用；成功写操作会提升数据版本，使旧查询结果失效。
// 写操作、LLM 工具、非法参数和可恢复错误都有独立上限，确保模型无法无限重试或重放副作用。
func RunWithTools(ctx context.Context, messages []llmutil.Message, registry *Registry, opt RunOptions) (*RunResult, error) {
	if registry == nil {
		return nil, fmt.Errorf("tool registry is nil")
	}
	if opt.MaxToolCalls <= 0 {
		opt.MaxToolCalls = 6
	}
	if opt.MaxLLMToolCalls <= 0 {
		opt.MaxLLMToolCalls = 2
	}
	if opt.MaxLLMRetries <= 0 {
		opt.MaxLLMRetries = 2
	}
	if opt.MaxMutationToolCalls <= 0 {
		opt.MaxMutationToolCalls = 1
	}
	if opt.MaxIsolatedToolRounds <= 0 {
		opt.MaxIsolatedToolRounds = 2
	}
	originalMessages := append([]llmutil.Message(nil), messages...)

	llmOpt := llmutil.GenerateOptions{Temperature: 0.4, MaxTokens: 4096, Tools: registry.LLMTools(), ToolChoice: "auto"}
	if opt.LLM != nil {
		llmOpt = *opt.LLM
		llmOpt.Tools = registry.LLMTools()
		if llmOpt.ToolChoice == nil {
			llmOpt.ToolChoice = "auto"
		}
	}

	// 下列状态只服务于当前一次 RunWithTools 调用，不会写入全局缓存。
	var traces []Trace
	llmCalls := 0
	mutationCalls := 0
	// invalidArgumentCalls 与 recoverableToolErrors 按工具计数，用于识别模型无法自行修正的调用。
	invalidArgumentCalls := map[string]int{}
	recoverableToolErrors := map[string]int{}
	// successfulQueries 缓存同一数据版本中的成功查询；写操作成功后 mutationVersion 递增。
	successfulQueries := map[string]Trace{}
	// successfulRunOnceTools 记录本轮已成功的快照类查询，避免再次暴露给模型调用。
	successfulRunOnceTools := map[string]Trace{}
	successfulToolCalls := map[string]int{}
	mutationVersion := 0
	maxLLMAttempts := llmutil.MaxAttempts(opt.MaxLLMRetries)
	// 外层每次循环代表一次“请求模型决定下一步”的编排轮次。
	for step := 0; step <= opt.MaxToolCalls; step++ {
		// 已完成 run-once 或达到单轮上限的工具从模型可见列表中移除。
		llmOpt.Tools = availableLLMTools(registry, successfulRunOnceTools, successfulToolCalls)
		if len(llmOpt.Tools) == 0 {
			llmOpt.ToolChoice = nil
		}
		emitRunEvent(opt, "status", map[string]any{"message": "正在让模型选择要调用的工具", "step": step + 1})
		var msg llmutil.Message
		var err error
		attemptMessages := messages
		// 模型请求只重试可恢复错误；输出额度耗尽则追加更严格的纠正指令后立即重试。
		for attempt := 1; attempt <= maxLLMAttempts; attempt++ {
			llmStartedAt := time.Now()
			emitRunEvent(opt, "llm_start", map[string]any{"phase": "orchestrator", "round": step + 1, "attempt": attempt})
			msg, err = llmutil.GenerateMessagesWithToolCalls(attemptMessages, llmOpt)
			if err == nil {
				emitRunEvent(opt, "llm_done", map[string]any{
					"phase":          "orchestrator",
					"round":          step + 1,
					"attempt":        attempt,
					"elapsed_ms":     time.Since(llmStartedAt).Milliseconds(),
					"has_tool_calls": len(msg.ToolCalls) > 0,
				})
				break
			}
			emitRunEvent(opt, "llm_error", map[string]any{
				"phase":      "orchestrator",
				"round":      step + 1,
				"attempt":    attempt,
				"elapsed_ms": time.Since(llmStartedAt).Milliseconds(),
				"error":      err.Error(),
			})
			if llmutil.IsOutputLimitError(err) {
				if len(traces) > 0 && completionRequirementSatisfied(traces, opt) {
					emitRunEvent(opt, "status", map[string]any{
						"message": "模型编排输出超出上限，正在根据已取得的工具证据直接生成结论",
						"step":    step + 1,
					})
					return finishToolRun(ctx, originalMessages, traces, opt)
				}
				if attempt < maxLLMAttempts {
					attemptMessages = outputLimitRecoveryMessages(messages, opt)
					emitRunEvent(opt, "llm_retry", map[string]any{
						"phase": "orchestrator", "round": step + 1, "attempt": attempt,
						"delay_ms": 0, "error": err.Error(),
						"message": "上一轮编排耗尽输出额度，已要求模型省略推理并立即执行一个最小工具调用或给出短答",
					})
					continue
				}
				return nil, err
			}
			if attempt == maxLLMAttempts || !llmutil.IsRetryableError(err) {
				return nil, err
			}
			delay := llmutil.RetryDelay(attempt)
			emitRunEvent(opt, "llm_retry", map[string]any{
				"phase": "orchestrator", "round": step + 1, "attempt": attempt,
				"delay_ms": delay.Milliseconds(), "error": err.Error(),
			})
			select {
			case <-ctx.Done():
				return nil, ctx.Err()
			case <-time.After(delay):
			}
		}
		if len(msg.ToolCalls) == 0 {
			// 有完成条件但模型提前给出文本时，要求它继续调用必要工具。
			if hasCompletionRequirement(opt) && !completionRequirementSatisfied(traces, opt) {
				if step >= opt.MaxToolCalls {
					return nil, fmt.Errorf("工具链流程未完成：必须成功调用 %s", completionRequirementLabel(opt))
				}
				messages = append(messages, msg)
				instruction := strings.TrimSpace(opt.IncompleteMessage)
				if instruction == "" {
					instruction = fmt.Sprintf("任务尚未完成。你刚才输出的内容只是待处理草稿，不能作为最终回答；请继续调用必要工具，并以成功调用 %s 结束流程。", completionRequirementLabel(opt))
				}
				messages = append(messages, llmutil.Message{Role: "user", Content: instruction})
				emitRunEvent(opt, "status", map[string]any{"message": instruction, "step": step + 1})
				continue
			}
			message := strings.TrimSpace(msg.Content)
			reasoning := strings.TrimSpace(msg.ReasoningContent)
			if len(traces) > 0 && opt.AlwaysSynthesizeAfterTools && opt.SynthesizeAfterTools && opt.LLM != nil {
				message, reasoning = synthesizeOrSummarize(ctx, originalMessages, traces, opt)
			} else if message == "" && len(traces) > 0 && opt.SynthesizeAfterTools && opt.LLM != nil {
				message, reasoning = synthesizeOrSummarize(ctx, originalMessages, traces, opt)
			}
			if message == "" && len(traces) > 0 {
				message = summarizeToolTraces(traces)
			}
			result := &RunResult{Message: message, Reasoning: reasoning, Traces: traces}
			emitRunEvent(opt, "done", result)
			return result, nil
		}
		// 先保存带 tool_calls 的模型消息，随后必须为每个调用补一条 tool 响应。
		messages = append(messages, msg)
		limitReached := false
		limitEventSent := false
		for callIndex, call := range msg.ToolCalls {
			// 模型返回的是协议名称；在此还原为领域名称并取得完整工具定义。
			toolName, tool, ok := registry.ResolveLLMTool(call.Function.Name)
			if !ok {
				payload := map[string]any{"error": "tool not found", "tool_name": call.Function.Name}
				emitRunEvent(opt, "tool_error", payload)
				messages = append(messages, toolResultMessage(call.ID, payload))
				continue
			}
			// 同时保留原始 JSON（给 Handler）和解码后的输入（给事件与查询去重键）。
			args, eventInput, err := decodeToolArguments(call.Function.Arguments)
			if err != nil {
				invalidArgumentCalls[toolName]++
				payload := map[string]any{"error": err.Error(), "tool_name": toolName}
				emitRunEvent(opt, "tool_error", payload)
				messages = append(messages, toolResultMessage(call.ID, payload))
				if invalidArgumentCalls[toolName] >= maxInvalidToolArgumentCalls {
					return nil, fmt.Errorf(
						"工具 %s 连续 %d 次返回了不完整或非法的 JSON 参数；请一次只处理一个对象、缩短参数并严格按工具 schema 重试: %w",
						toolName,
						maxInvalidToolArgumentCalls,
						err,
					)
				}
				continue
			}
			invalidArgumentCalls[toolName] = 0
			// run-once 查询的已有结果直接反馈给模型，不重复执行实际 Handler。
			if previous, exists := successfulRunOnceTools[toolName]; tool.RunOncePerRun && exists {
				payload := map[string]any{
					"ok":      true,
					"reused":  true,
					"summary": previous.OutputSummary,
					"message": fmt.Sprintf("%s 已在本轮成功执行；请使用已有结果和后续 mutation 返回值继续任务，不要再次读取", toolName),
				}
				messages = append(messages, toolResultMessage(call.ID, payload))
				emitRunEvent(opt, "status", map[string]any{
					"message":   fmt.Sprintf("%s 已在本轮读取，跳过重复调用", toolName),
					"tool_name": toolName,
				})
				continue
			}
			if tool.MaxCallsPerRun > 0 && successfulToolCalls[toolName] >= tool.MaxCallsPerRun {
				payload := map[string]any{
					"ok":      true,
					"skipped": true,
					"message": fmt.Sprintf("%s 已达到本轮 %d 次调用上限；请使用已收集证据继续后续工具，不要继续扩展检索", toolName, tool.MaxCallsPerRun),
				}
				messages = append(messages, toolResultMessage(call.ID, payload))
				emitRunEvent(opt, "status", map[string]any{
					"message":   payload["message"],
					"tool_name": toolName,
				})
				continue
			}
			queryKey := ""
			if tool.Kind == KindQuery {
				// 只有数据未被 mutation 改变且参数相同，查询结果才可安全复用。
				queryKey = toolQueryCacheKey(mutationVersion, toolName, eventInput)
				if previous, exists := successfulQueries[queryKey]; exists {
					payload := map[string]any{
						"ok":      true,
						"reused":  true,
						"summary": previous.OutputSummary,
					}
					messages = append(messages, toolResultMessage(call.ID, payload))
					emitRunEvent(opt, "status", map[string]any{
						"message":   fmt.Sprintf("%s 查询参数及数据版本未变化，复用上一结果", toolName),
						"tool_name": toolName,
					})
					if completionRequirementSatisfied(traces, opt) {
						return finishToolRun(ctx, originalMessages, traces, opt)
					}
					continue
				}
			}
			// LLM 与 mutation 均有独立额度；超额调用仍写回 tool 响应，让模型能调整计划。
			if tool.Kind == KindLLM {
				if llmCalls >= opt.MaxLLMToolCalls {
					payload := map[string]any{"error": "llm tool call limit exceeded", "tool_name": toolName}
					if !limitEventSent {
						emitRunEvent(opt, "tool_error", payload)
						limitEventSent = true
					}
					messages = append(messages, toolResultMessage(call.ID, payload))
					limitReached = true
					continue
				}
			}
			if tool.Kind == KindMutation {
				if mutationCalls >= opt.MaxMutationToolCalls {
					payload := map[string]any{"error": "mutation tool call limit exceeded", "tool_name": toolName}
					if !limitEventSent {
						emitRunEvent(opt, "tool_error", payload)
						limitEventSent = true
					}
					messages = append(messages, toolResultMessage(call.ID, payload))
					limitReached = true
					continue
				}
				mutationCalls++
			}
			var result any
			var trace Trace
			toolAttempts := llmutil.MaxAttempts(tool.MaxRetries)
			// Handler 的临时错误在这里重试；每次尝试都会保留一条 Trace，便于审计。
			for attempt := 1; attempt <= toolAttempts; attempt++ {
				emitRunEvent(opt, "tool_start", map[string]any{
					"tool_name": toolName,
					"kind":      tool.Kind,
					"input":     eventInput,
					"attempt":   attempt,
				})
				result, trace, err = executeTool(ctx, registry, opt.Executor, opt.UserName, toolName, args)
				traces = append(traces, trace)
				emitRunEvent(opt, "tool_done", trace)
				if err == nil || attempt == toolAttempts || !llmutil.IsRetryableError(err) {
					break
				}
				delay := llmutil.RetryDelay(attempt)
				emitRunEvent(opt, "tool_retry", map[string]any{
					"tool_name": toolName,
					"kind":      tool.Kind,
					"attempt":   attempt,
					"delay_ms":  delay.Milliseconds(),
					"error":     err.Error(),
					"message":   fmt.Sprintf("%s 第 %d 次调用失败，%s 后重试", toolName, attempt, delay),
				})
				select {
				case <-ctx.Done():
					return nil, ctx.Err()
				case <-time.After(delay):
				}
			}
			if err == nil {
				// 仅成功调用会写入查询复用表、run-once 表和成功调用计数。
				recoverableToolErrors[toolName] = 0
				successfulToolCalls[toolName]++
				if tool.RunOncePerRun {
					successfulRunOnceTools[toolName] = trace
				}
				if tool.Kind == KindQuery && queryKey != "" {
					successfulQueries[queryKey] = trace
				}
				if tool.Kind == KindMutation {
					mutationVersion++
				}
				if tool.Kind == KindLLM {
					llmCalls++
				}
			}
			payload := toolCallPayload(result, trace, err)
			if err != nil {
				payload["error"] = err.Error()
			}
			terminal, shouldStop := result.(TerminalResult)
			if err == nil && tool.TerminalOnSuccess {
				terminal = TerminalResult{Result: result, Message: fmt.Sprintf("%s 已完成当前受控步骤", toolName)}
				shouldStop = true
			}
			if shouldStop {
				payload["result"] = terminal.Result
			}
			messages = append(messages, toolResultMessage(call.ID, payload))
			if err != nil && !tool.StopOnError {
				// 不致命错误交给模型修正。后续同批 tool call 必须标记为跳过，
				// 否则协议会缺少与 tool_call 对应的响应。
				recoverableToolErrors[toolName]++
				if recoverableToolErrors[toolName] >= maxRecoverableToolErrors {
					return nil, fmt.Errorf(
						"工具 %s 连续 %d 次调用失败，模型未能修正参数或调用条件: %w",
						toolName,
						maxRecoverableToolErrors,
						err,
					)
				}
				emitRunEvent(opt, "status", map[string]any{
					"message":   fmt.Sprintf("%s 调用失败，正在让模型根据错误修正参数；已成功的工具调用不会重放", toolName),
					"tool_name": toolName,
					"attempt":   recoverableToolErrors[toolName],
					"error":     err.Error(),
				})
				messages = appendSkippedToolResults(messages, msg.ToolCalls[callIndex+1:], toolName, err)
				break
			}
			if err != nil && (tool.StopOnError || toolName == opt.RequiredTool && tool.MaxRetries > 0) {
				return nil, fmt.Errorf("工具 %s 调用失败: %w", toolName, err)
			}
			if err == nil && shouldStop {
				message := strings.TrimSpace(terminal.Message)
				if message == "" {
					message = summarizeToolTraces(traces)
				}
				runResult := &RunResult{Message: message, Traces: traces}
				emitRunEvent(opt, "done", runResult)
				return runResult, nil
			}
		}
		if limitReached {
			if hasCompletionRequirement(opt) && !completionRequirementSatisfied(traces, opt) {
				return nil, fmt.Errorf("工具链流程未完成：调用额度已用尽，但 %s 尚未成功", completionRequirementLabel(opt))
			}
			return finishToolRun(ctx, originalMessages, traces, opt)
		}
		if opt.ReturnAfterToolCalls {
			// 隔离轮次只携带原始任务与压缩进度，不直接复用长对话，避免重放已成功的 mutation。
			if step+1 < opt.MaxIsolatedToolRounds {
				emitRunEvent(opt, "status", map[string]any{"message": "正在根据已有工具结果判断是否需要继续调用工具", "step": step + 1})
				messages = isolatedToolRoundMessages(originalMessages, traces, opt)
				continue
			}
			if hasCompletionRequirement(opt) && !completionRequirementSatisfied(traces, opt) {
				return nil, fmt.Errorf("工具链流程未完成：隔离工具轮次已达上限，但 %s 尚未成功", completionRequirementLabel(opt))
			}
			message, reasoning := synthesizeOrSummarize(ctx, originalMessages, traces, opt)
			result := &RunResult{Message: message, Reasoning: reasoning, Traces: traces}
			emitRunEvent(opt, "done", result)
			return result, nil
		}
	}
	if hasCompletionRequirement(opt) && !completionRequirementSatisfied(traces, opt) {
		return nil, fmt.Errorf("工具链流程未完成：工具调用次数已达上限，但 %s 尚未成功", completionRequirementLabel(opt))
	}
	message := "工具调用次数已达上限，请根据已有工具结果给出当前最可靠的回答。"
	reasoning := ""
	if opt.SynthesizeAfterTools && opt.LLM != nil && len(traces) > 0 {
		message, reasoning = synthesizeOrSummarize(ctx, originalMessages, traces, opt)
	}
	result := &RunResult{Message: message, Reasoning: reasoning, Traces: traces}
	emitRunEvent(opt, "done", result)
	return result, nil
}

// executeTool 优先委托应用层执行器；未配置时退化为 Registry 的同步调用。
// 保留该回退路径可让命令行工具、单元测试及无需全局调度的场景继续直接使用 RunWithTools。
func executeTool(ctx context.Context, registry *Registry, executor Executor, userName, name string, args json.RawMessage) (any, Trace, error) {
	if executor != nil {
		return executor.Execute(ctx, userName, name, args)
	}
	return registry.Call(ctx, name, args)
}

// hasSuccessfulTool 判断指定工具是否至少存在一条成功轨迹。
func hasSuccessfulTool(traces []Trace, name string) bool {
	for _, trace := range traces {
		if trace.ToolName == name && trace.Status == "ok" {
			return true
		}
	}
	return false
}

// successfulToolCallCount 统计指定工具成功执行的次数，不计重试失败或复用结果。
func successfulToolCallCount(traces []Trace, name string) int {
	count := 0
	for _, trace := range traces {
		if trace.ToolName == name && trace.Status == "ok" {
			count++
		}
	}
	return count
}

// hasCompletionRequirement 判断本轮是否配置了“必须成功调用工具”的结束条件。
func hasCompletionRequirement(opt RunOptions) bool {
	return strings.TrimSpace(opt.RequiredTool) != "" || len(opt.RequiredTools) > 0 || len(opt.RequiredAnyTools) > 0 || len(opt.RequiredToolCallCounts) > 0
}

// completionRequirementSatisfied 校验所有必需条件：RequiredTool、RequiredTools 和调用次数
// 条件必须全部满足；RequiredAnyTools 只要求其中任意一个成功。
func completionRequirementSatisfied(traces []Trace, opt RunOptions) bool {
	if required := strings.TrimSpace(opt.RequiredTool); required != "" && !hasSuccessfulTool(traces, required) {
		return false
	}
	for _, name := range opt.RequiredTools {
		if name = strings.TrimSpace(name); name != "" && !hasSuccessfulTool(traces, name) {
			return false
		}
	}
	for name, requiredCount := range opt.RequiredToolCallCounts {
		name = strings.TrimSpace(name)
		if name != "" && requiredCount > 0 && successfulToolCallCount(traces, name) < requiredCount {
			return false
		}
	}
	if len(opt.RequiredAnyTools) > 0 {
		for _, name := range opt.RequiredAnyTools {
			if hasSuccessfulTool(traces, strings.TrimSpace(name)) {
				return true
			}
		}
		return false
	}
	return true
}

// completionRequirementLabel 将完成条件转换为面向模型和用户的可读描述。
func completionRequirementLabel(opt RunOptions) string {
	requiredNames := make([]string, 0, 1+len(opt.RequiredTools)+len(opt.RequiredToolCallCounts))
	seen := map[string]bool{}
	if required := strings.TrimSpace(opt.RequiredTool); required != "" {
		requiredNames = append(requiredNames, required)
		seen[required] = true
	}
	for _, name := range opt.RequiredTools {
		if name = strings.TrimSpace(name); name != "" && !seen[name] {
			requiredNames = append(requiredNames, name)
			seen[name] = true
		}
	}
	for name, requiredCount := range opt.RequiredToolCallCounts {
		name = strings.TrimSpace(name)
		if name == "" || requiredCount <= 0 || seen[name] {
			continue
		}
		requiredNames = append(requiredNames, fmt.Sprintf("%s 成功调用 %d 次", name, requiredCount))
		seen[name] = true
	}
	names := make([]string, 0, len(opt.RequiredAnyTools))
	for _, name := range opt.RequiredAnyTools {
		if name = strings.TrimSpace(name); name != "" {
			names = append(names, name)
		}
	}
	if len(names) > 0 {
		anyLabel := strings.Join(names, " 或 ")
		if len(requiredNames) == 0 {
			return anyLabel
		}
		requiredNames = append(requiredNames, "("+anyLabel+")")
	}
	return strings.Join(requiredNames, " 且 ")
}

// toolQueryCacheKey 使用 mutationVersion 隔离数据变更前后的相同查询。
// input 来自已验证的 JSON 参数，因此相同对象会生成稳定键。
func toolQueryCacheKey(mutationVersion int, toolName string, input any) string {
	encoded, err := json.Marshal(input)
	if err != nil {
		encoded = []byte(fmt.Sprint(input))
	}
	return fmt.Sprintf("%d:%s:%s", mutationVersion, toolName, encoded)
}

// decodeToolArguments 校验模型返回的函数参数必须是合法 JSON。
// 空参数按空对象处理；同时返回原始 JSON 与反序列化值，分别供 Handler 和事件系统使用。
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

// finishToolRun 对已完成的工具轨迹生成最终答案，并发送 done 事件。
func finishToolRun(ctx context.Context, originalMessages []llmutil.Message, traces []Trace, opt RunOptions) (*RunResult, error) {
	message, reasoning := synthesizeOrSummarize(ctx, originalMessages, traces, opt)
	result := &RunResult{Message: message, Reasoning: reasoning, Traces: traces}
	emitRunEvent(opt, "done", result)
	return result, nil
}

// emitRunEvent 统一处理可选事件回调，避免编排逻辑散落 nil 判断。
func emitRunEvent(opt RunOptions, event string, payload any) {
	if opt.OnEvent != nil {
		opt.OnEvent(event, payload)
	}
}

// toolResultMessage 将执行结果编码为 OpenAI 工具协议要求的 tool 消息。
// 即使编码失败也返回空 JSON 内容，保证调用 ID 与响应消息一一对应。
func toolResultMessage(toolCallID string, payload any) llmutil.Message {
	content, _ := json.Marshal(payload)
	return llmutil.Message{Role: "tool", ToolCallID: toolCallID, Content: string(content)}
}

// appendSkippedToolResults 为同一批次中未执行的 tool call 补充“已跳过”响应。
// 当较早调用失败时这样做能保持工具协议完整，并让模型从失败位置继续。
func appendSkippedToolResults(messages []llmutil.Message, calls []llmutil.ToolCall, failedTool string, cause error) []llmutil.Message {
	for _, call := range calls {
		payload := map[string]any{
			"ok":        false,
			"skipped":   true,
			"tool_name": call.Function.Name,
			"error":     fmt.Sprintf("skipped because the earlier tool %s failed: %v", failedTool, cause),
		}
		messages = append(messages, toolResultMessage(call.ID, payload))
	}
	return messages
}

// toolCallPayload 构造返回给模型的结果。大结果不回传完整内容，只回传可用上下文摘要，
// 以控制下一轮消息长度；完整内容仅由具体业务层按需保存。
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

// outputLimitRecoveryMessages 在模型输出额度耗尽时追加一条收敛指令。
// 若尚有完成条件，则明确要求模型先完成工具调用，避免它把参数 JSON 当作普通文本输出。
func outputLimitRecoveryMessages(messages []llmutil.Message, opt RunOptions) []llmutil.Message {
	retry := append([]llmutil.Message(nil), messages...)
	content := `上一轮输出额度已耗尽，且没有形成完整结果。不要复述材料，也不要输出思考过程。
保持用户原始任务要求的格式、数量和篇幅，直接重新生成完整最终内容；不要擅自缩短为摘要。如果任务确实需要可用工具，只调用当前最必要的工具。`
	if requirement := completionRequirementLabel(opt); requirement != "" {
		content = fmt.Sprintf(`上一轮输出额度已耗尽，且尚未满足工具完成条件。不要复述材料，不要输出长正文或思考过程。
当前必须成功完成：%s。
不得把函数参数 JSON 当成普通正文返回。请通过 tool_calls 协议调用一个当前最必要的可用工具；若写操作存在前置读取条件，先完成前置工具。完成条件未满足前不得直接回答。`, requirement)
	}
	retry = append(retry, llmutil.Message{
		Role:    "user",
		Content: content,
	})
	return retry
}

// isolatedToolRoundMessages 为下一次隔离编排构建最小上下文：原始用户任务、完成条件和执行账本。
// 它不携带先前模型的长文本，以降低模型把旧 tool call 当成待重放动作的风险。
func isolatedToolRoundMessages(originalMessages []llmutil.Message, traces []Trace, opt RunOptions) []llmutil.Message {
	next := append([]llmutil.Message(nil), originalMessages...)
	requirement := completionRequirementLabel(opt)
	if requirement == "" {
		requirement = "以用户原始目标为准，不强制调用特定工具"
	}
	next = append(next, llmutil.Message{
		Role: "user",
		Content: fmt.Sprintf(`任务目标没有改变。始终以此前的“用户任务”和最近会话方案为目标，不得把最近一次工具返回误当成新目标。
当前完成条件：%s

本轮累计执行进度：
%s

如果还缺必要证据，请继续调用最少量的工具；如果证据已经足够，请不要再调用工具，直接回答用户原问题。
不要重复调用已经返回足够结果的同一工具和相同参数；尤其不得重放已经成功的 mutation。
已经成功执行过且从工具列表中消失的 run-once 查询仍然有效；请使用进度记录中的结果和 ID，不要尝试重新调用。
如果 create 提示对象已存在，改用对应的 update，并沿用已读取的 ID；不要再次 list 或重试 create。
如果上一轮某个工具失败，只修正该失败调用的参数或前置条件，然后从失败位置继续。`, requirement, isolatedToolProgressContext(traces, 18000)),
	})
	return next
}

// availableLLMTools 从注册表中筛选下一轮可见工具。
// 已成功的 RunOncePerRun 工具及达到 MaxCallsPerRun 的工具会被移除。
func availableLLMTools(registry *Registry, completed map[string]Trace, successfulCalls map[string]int) []llmutil.Tool {
	tools := registry.LLMTools()
	if len(completed) == 0 && len(successfulCalls) == 0 {
		return tools
	}
	excluded := make(map[string]bool, len(completed)+len(successfulCalls))
	for name := range completed {
		if llmName := registry.llmToolNames[name]; llmName != "" {
			excluded[llmName] = true
		}
	}
	for name, count := range successfulCalls {
		tool, ok := registry.tools[name]
		if !ok || tool.MaxCallsPerRun <= 0 || count < tool.MaxCallsPerRun {
			continue
		}
		if llmName := registry.llmToolNames[name]; llmName != "" {
			excluded[llmName] = true
		}
	}
	filtered := tools[:0]
	for _, tool := range tools {
		if !excluded[tool.Function.Name] {
			filtered = append(filtered, tool)
		}
	}
	return filtered
}

// isolatedToolProgressContext 将执行轨迹压缩成下一轮模型可理解的“已完成事项 + 最近证据”。
// 仅保留少量最新成功查询，防止进度消息反过来挤占模型上下文。
func isolatedToolProgressContext(traces []Trace, maxRunes int) string {
	if len(traces) == 0 {
		return "尚未执行任何工具。"
	}
	var progress strings.Builder
	progress.WriteString("执行账本（成功项表示已完成，不得重放）：\n")
	for index, trace := range traces {
		status := "成功"
		detail := trimRunes(trace.OutputSummary, 180)
		if trace.Status == "error" {
			status = "失败"
			detail = trimRunes(trace.Error, 180)
		}
		input := summarizeTraceInput(trace.Input)
		fmt.Fprintf(&progress, "%d. %s [%s]", index+1, trace.ToolName, status)
		if input != "" {
			fmt.Fprintf(&progress, " 输入=%s", input)
		}
		if detail != "" {
			fmt.Fprintf(&progress, " 结果=%s", detail)
		}
		progress.WriteByte('\n')
	}

	evidence := make([]string, 0, 4)
	seenQueries := map[string]bool{}
	for index := len(traces) - 1; index >= 0 && len(evidence) < 4; index-- {
		trace := traces[index]
		queryKey := trace.ToolName + ":" + string(trace.Input)
		if trace.Kind != KindQuery || trace.Status != "ok" || seenQueries[queryKey] {
			continue
		}
		seenQueries[queryKey] = true
		context := trace.outputContext
		if strings.TrimSpace(context) == "" {
			context = trace.OutputSummary
		}
		contextMaxRunes := 1200
		if trace.ToolName == "character.list" {
			contextMaxRunes = 8000
		}
		evidence = append(evidence, fmt.Sprintf("工具 %s 的有效结果（越靠前越新）：%s", trace.ToolName, trimRunes(context, contextMaxRunes)))
	}
	if len(evidence) > 0 {
		progress.WriteString("\n最近有效查询证据：\n")
		progress.WriteString(strings.Join(evidence, "\n\n"))
	}
	return trimRunes(progress.String(), maxRunes)
}

// summarizeTraceInput 从输入 JSON 中摘取对后续操作有用的标识字段。
// 无法解析或不存在已知字段时退化为截断后的原始 JSON，保证诊断信息不丢失。
func summarizeTraceInput(raw json.RawMessage) string {
	if len(raw) == 0 {
		return ""
	}
	var input map[string]any
	if err := json.Unmarshal(raw, &input); err != nil {
		return trimRunes(string(raw), 120)
	}
	keys := []string{
		"query", "document_id", "title", "character_id", "name", "names",
		"node_id", "after_node_id", "confirm_name", "confirm_title", "mode",
	}
	selected := make(map[string]any)
	for _, key := range keys {
		if value, exists := input[key]; exists {
			selected[key] = value
		}
	}
	if len(selected) == 0 {
		return trimRunes(string(raw), 120)
	}
	encoded, err := json.Marshal(selected)
	if err != nil {
		return trimRunes(string(raw), 120)
	}
	return trimRunes(string(encoded), 120)
}

// synthesizeOrSummarize 优先调用专门的总结模型；总结失败或未启用时退化为本地轨迹摘要。
func synthesizeOrSummarize(ctx context.Context, originalMessages []llmutil.Message, traces []Trace, opt RunOptions) (string, string) {
	message := summarizeToolTraces(traces)
	if opt.SynthesizeAfterTools && opt.LLM != nil {
		emitRunEvent(opt, "status", map[string]any{"message": "正在根据工具结果生成结论"})
		if synthesized, reasoning, err := synthesizeToolAnswer(ctx, originalMessages, traces, *opt.LLM, opt.OnEvent); err == nil && strings.TrimSpace(synthesized) != "" {
			return synthesized, reasoning
		} else if err != nil {
			message += "\n\n注意：工具结果二次总结失败，已退回工具摘要：" + err.Error()
		}
	}
	return message, ""
}

// synthesizeToolAnswer 关闭工具和思考模式，以流式优先、非流式兜底的方式生成最终结论。
// 此阶段只消费已获得的轨迹证据，不允许再发起工具调用。
func synthesizeToolAnswer(ctx context.Context, originalMessages []llmutil.Message, traces []Trace, opt llmutil.GenerateOptions, onEvent func(event string, payload any)) (string, string, error) {
	opt.Tools = nil
	opt.ToolChoice = nil
	// 工具执行完后的结论只需依据已取得的证据生成。关闭 DeepSeek V4 思考模式，
	// 让桌面端可以尽快收到第一段 SSE 输出，避免界面看起来停在规划阶段。
	opt.Thinking = &llmutil.Thinking{Type: "disabled"}
	if opt.MaxTokens <= 0 || opt.MaxTokens > 2048 {
		opt.MaxTokens = 2048
	}
	if opt.Temperature <= 0 || opt.Temperature > 0.4 {
		opt.Temperature = 0.25
	}
	userTask := ""
	for i := len(originalMessages) - 1; i >= 0; i-- {
		if originalMessages[i].Role == "user" {
			userTask = originalMessages[i].Content
			break
		}
	}
	messages := []llmutil.Message{
		{
			Role: "system",
			Content: `你是 InkFlow 的工具结果总结器。你不会再调用工具，只根据用户问题和工具结果给出结论。
回答必须结论先行，不要只罗列检索结果。
若用户问“有没有吃设定/是否冲突”，必须明确给出：有/没有/无法确定；然后列证据、风险点和下一步建议。
如果证据不足，要说明缺哪类证据，而不是假装确定。`,
		},
		{
			Role:    "user",
			Content: fmt.Sprintf("用户原始问题：\n%s\n\n工具结果：\n%s\n\n请直接回答用户问题。", userTask, toolTraceContext(traces, 3000)),
		},
	}
	var answer strings.Builder
	var reasoning strings.Builder
	if onEvent != nil {
		startedAt := time.Now()
		onEvent("llm_start", map[string]any{"phase": "synthesis", "mode": "stream"})
		err := llmutil.GenerateMessagesStream(messages, opt, func(delta llmutil.StreamDelta) error {
			if delta.ReasoningContent != "" {
				reasoning.WriteString(delta.ReasoningContent)
				onEvent("reasoning", map[string]any{"delta": delta.ReasoningContent, "stage": "synthesis"})
			}
			if delta.Content != "" {
				answer.WriteString(delta.Content)
				onEvent("synthesis_delta", map[string]any{"delta": delta.Content})
			}
			return nil
		})
		if err == nil {
			onEvent("llm_done", map[string]any{"phase": "synthesis", "mode": "stream", "elapsed_ms": time.Since(startedAt).Milliseconds()})
			return answer.String(), reasoning.String(), nil
		}
		onEvent("llm_error", map[string]any{"phase": "synthesis", "mode": "stream", "elapsed_ms": time.Since(startedAt).Milliseconds(), "error": err.Error()})
	}
	startedAt := time.Now()
	if onEvent != nil {
		onEvent("llm_start", map[string]any{"phase": "synthesis", "mode": "non_stream"})
	}
	text, err := llmutil.GenerateMessages(messages, opt)
	if onEvent != nil {
		payload := map[string]any{"phase": "synthesis", "mode": "non_stream", "elapsed_ms": time.Since(startedAt).Milliseconds()}
		if err != nil {
			payload["error"] = err.Error()
			onEvent("llm_error", payload)
		} else {
			onEvent("llm_done", payload)
		}
	}
	return text, "", err
}

// toolTraceContext 生成供总结模型读取的工具证据文本；优先使用较长上下文，失败轨迹只提供错误信息。
func toolTraceContext(traces []Trace, perTraceMax int) string {
	toolLines := make([]string, 0, len(traces))
	for _, trace := range traces {
		if trace.Status == "error" {
			toolLines = append(toolLines, fmt.Sprintf("工具 %s 失败：%s", trace.ToolName, trace.Error))
			continue
		}
		output := trace.outputContext
		if strings.TrimSpace(output) == "" {
			output = trace.OutputSummary
		}
		toolLines = append(toolLines, fmt.Sprintf("工具 %s 返回：%s", trace.ToolName, trimRunes(output, perTraceMax)))
	}
	return strings.Join(toolLines, "\n\n")
}

// summarizeToolTraces 在没有二次模型总结时生成保底答案，包含结论摘要和每次调用明细。
func summarizeToolTraces(traces []Trace) string {
	if len(traces) == 0 {
		return "工具链未调用任何工具。"
	}
	conclusions := summarizeTraceConclusions(traces)
	out := strings.Join(conclusions, "\n")
	out += fmt.Sprintf("\n\n工具明细：已执行 %d 个工具。", len(traces))
	for _, trace := range traces {
		if trace.Status == "error" {
			out += fmt.Sprintf("\n- %s: 失败，%s", trace.ToolName, trace.Error)
			continue
		}
		out += fmt.Sprintf("\n- %s: %s", trace.ToolName, conciseTraceOutput(trace))
	}
	return out
}

// summarizeTraceConclusions 对当前内置的文档、事实、伏笔和逻辑审查工具提炼业务结论。
// 未识别的领域工具不会被臆测解释，仍会在调用明细中保留原始摘要。
func summarizeTraceConclusions(traces []Trace) []string {
	lines := []string{"结论摘要："}
	var factCount, clueCount int
	var factSamples, clueSamples []string
	hasDraft := false
	hasReview := false

	for _, trace := range traces {
		if trace.Status == "error" {
			lines = append(lines, fmt.Sprintf("- %s 调用失败：%s", trace.ToolName, trace.Error))
			continue
		}
		switch trace.ToolName {
		case "draft.get_current":
			hasDraft = true
		case "fact.search":
			factSamples = extractJSONFieldStrings(trace.OutputSummary, "summary", 4)
			factCount = countJSONObjects(trace.OutputSummary)
		case "clue.search":
			clueSamples = extractJSONFieldStrings(trace.OutputSummary, "content", 4)
			if len(clueSamples) == 0 {
				clueSamples = extractJSONFieldStrings(trace.OutputSummary, "label", 4)
			}
			clueCount = countJSONObjects(trace.OutputSummary)
		case "draft.review_logic":
			hasReview = true
			lines = append(lines, summarizeReviewTrace(trace.OutputSummary)...)
		}
	}

	if hasDraft {
		lines = append(lines, "- 已读取当前正文，后续判断基于当前正文内容。")
	}
	if factCount > 0 || len(factSamples) > 0 {
		if factCount == 0 {
			factCount = len(factSamples)
		}
		lines = append(lines, fmt.Sprintf("- 查到 %d 条相关事实；现有事实可作为一致性依据。", factCount))
		for _, sample := range factSamples {
			lines = append(lines, "  · "+sample)
		}
	} else if usedTool(traces, "fact.search") {
		lines = append(lines, "- 未查到相关事实，不能据此确认是否吃设定。")
	}
	if clueCount > 0 || len(clueSamples) > 0 {
		if clueCount == 0 {
			clueCount = len(clueSamples)
		}
		lines = append(lines, fmt.Sprintf("- 查到 %d 条相关伏笔，需要核对当前正文是否回收或冲突。", clueCount))
		for _, sample := range clueSamples {
			lines = append(lines, "  · "+sample)
		}
	} else if usedTool(traces, "clue.search") {
		lines = append(lines, "- 未查到相关伏笔，本次主要按事实库判断。")
	}
	if !hasReview && (usedTool(traces, "fact.search") || usedTool(traces, "clue.search")) {
		lines = append(lines, "- 初步判断：未从工具结果中看到直接冲突；但这不是完整逻辑审查，若要强判定应再调用 draft.review_logic。")
	}
	if len(lines) == 1 {
		lines = append(lines, "- 工具已执行，但没有可提炼的业务结论；请查看下方工具明细。")
	}
	return lines
}

// summarizeReviewTrace 读取 draft.review_logic 的约定 JSON，并提取最重要的审查结论。
// 摘要已经被截断或不符合约定时保守提示调用方查看完整工具明细。
func summarizeReviewTrace(summary string) []string {
	var obj map[string]any
	if err := json.Unmarshal([]byte(summary), &obj); err != nil {
		return []string{"- 逻辑法官已返回结果，但摘要被截断，建议查看工具明细。"}
	}
	if valid, ok := obj["is_valid"].(bool); ok && valid {
		return []string{"- 逻辑法官结论：未发现硬性逻辑冲突。"}
	}
	lines := []string{"- 逻辑法官结论：发现需要处理的问题。"}
	if suggestion, ok := obj["suggestion"].(string); ok && strings.TrimSpace(suggestion) != "" {
		lines = append(lines, "  · 建议："+strings.TrimSpace(suggestion))
	}
	if errorsValue, ok := obj["errors"].([]any); ok {
		for i, item := range errorsValue {
			if i >= 4 {
				break
			}
			if entry, ok := item.(map[string]any); ok {
				content, _ := entry["content"].(string)
				if strings.TrimSpace(content) != "" {
					lines = append(lines, "  · "+strings.TrimSpace(content))
				}
			}
		}
	}
	return lines
}

// conciseTraceOutput 为常用领域工具生成短而可读的调用结果；其他工具使用通用截断摘要。
func conciseTraceOutput(trace Trace) string {
	switch trace.ToolName {
	case "draft.get_current":
		return "已读取当前正文。"
	case "fact.search":
		samples := extractJSONFieldStrings(trace.OutputSummary, "summary", 3)
		if len(samples) == 0 {
			return trimRunes(trace.OutputSummary, 260)
		}
		return strings.Join(samples, "；")
	case "clue.search":
		if strings.TrimSpace(trace.OutputSummary) == "[]" {
			return "未找到相关伏笔。"
		}
	case "draft.index":
		return trimRunes(trace.OutputSummary, 360)
	}
	return trimRunes(trace.OutputSummary, 360)
}

// usedTool 判断工具是否曾被调用，不要求调用成功。
func usedTool(traces []Trace, name string) bool {
	for _, trace := range traces {
		if trace.ToolName == name {
			return true
		}
	}
	return false
}

// countJSONObjects 是轻量级的数组对象计数，只用于展示性结论，不承担严格 JSON 校验职责。
func countJSONObjects(text string) int {
	if strings.TrimSpace(text) == "[]" {
		return 0
	}
	count := strings.Count(text, "{")
	if count > 0 {
		return count
	}
	return 0
}

// extractJSONFieldStrings 从工具 JSON 摘要中提取指定字符串字段。
// 它刻意避免再次完整反序列化未知结构，仅服务于保底摘要展示。
func extractJSONFieldStrings(text string, field string, max int) []string {
	if max <= 0 {
		return nil
	}
	needle := `"` + field + `":"`
	out := make([]string, 0, max)
	rest := text
	for len(out) < max {
		start := strings.Index(rest, needle)
		if start < 0 {
			break
		}
		rest = rest[start+len(needle):]
		var builder strings.Builder
		escaped := false
		for i, r := range rest {
			if escaped {
				builder.WriteRune(r)
				escaped = false
				continue
			}
			if r == '\\' {
				escaped = true
				continue
			}
			if r == '"' {
				rest = rest[i+1:]
				break
			}
			builder.WriteRune(r)
		}
		value := strings.TrimSpace(builder.String())
		if value != "" {
			out = append(out, trimRunes(value, 180))
		}
	}
	return out
}

// trimRunes 按 Unicode 字符数裁剪文本，避免截断中文或 emoji 的 UTF-8 字节序列。
func trimRunes(text string, max int) string {
	text = strings.TrimSpace(text)
	if max <= 0 {
		return text
	}
	runes := []rune(text)
	if len(runes) <= max {
		return text
	}
	return string(runes[:max]) + "...(truncated)"
}
