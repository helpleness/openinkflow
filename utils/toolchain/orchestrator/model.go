package orchestrator

import (
	"context"
	"encoding/json"
	"errors"
	"fmt"
	"io"
	"strings"
	"time"

	domainllm "InkFlow/internal/ai/llm"
	"InkFlow/utils"
)

func (state *runState) requestModelDecision(step int) (domainllm.Message, *RunResult, error) {
	state.modelRequest.Tools = availableLLMTools(state.registry, state.ledger.SuccessfulRunOnce, state.ledger.SuccessfulCalls, state.ledger.AttemptedCalls)
	if len(state.modelRequest.Tools) == 0 {
		state.modelRequest.ToolChoice = nil
	}
	emitRunEvent(state.config, "status", map[string]any{"message": "正在让模型选择要调用的工具", "step": step + 1})
	attemptMessages := state.messages
	for attempt := 1; attempt <= (state.config.Budget.MaxModelRetries + 1); attempt++ {
		message, err := state.requestModelAttempt(step, attempt, attemptMessages)
		if err == nil {
			return message, nil, nil
		}
		var outputLimit *domainllm.OutputLimitError
		if errors.As(err, &outputLimit) {
			if len(state.ledger.Traces) > 0 && completionRequirementSatisfied(state.ledger.Traces, state.config.Completion) {
				emitRunEvent(state.config, "status", map[string]any{
					"message": "模型编排输出超出上限，正在根据已取得的工具证据直接生成结论",
					"step":    step + 1,
				})
				result, finishErr := state.finishToolRun()
				return domainllm.Message{}, result, finishErr
			}
			if attempt < (state.config.Budget.MaxModelRetries + 1) {
				attemptMessages = outputLimitRecoveryMessages(state.messages, state.config)
				emitRunEvent(state.config, "llm_retry", map[string]any{
					"phase": "orchestrator", "round": step + 1, "attempt": attempt,
					"delay_ms": 0, "error": err.Error(),
					"message": "上一轮编排耗尽输出额度，已要求模型省略推理并立即执行一个最小工具调用或给出短答",
				})
				continue
			}
			return domainllm.Message{}, nil, err
		}
		// Transport retries belong to Provider; only truncated decisions are re-prompted here.
		return domainllm.Message{}, nil, err
	}
	return domainllm.Message{}, nil, fmt.Errorf("模型编排未返回结果")
}

func (state *runState) requestModelAttempt(step, attempt int, messages []domainllm.Message) (domainllm.Message, error) {
	startedAt := time.Now()
	emitRunEvent(state.config, "llm_start", map[string]any{"phase": "orchestrator", "round": step + 1, "attempt": attempt})
	request := state.modelRequest
	request.Messages = messages
	ctx, cancel := state.modelContext()
	defer cancel()
	response, err := state.llm.Chat(ctx, request)
	message := domainllm.Message{}
	if err == nil {
		if response.FinishReason == "length" {
			err = &domainllm.OutputLimitError{ToolCall: true}
		} else {
			message = response.Message
			message.Content = utils.CleanModelText(message.Content)
		}
	}
	payload := map[string]any{
		"phase":      "orchestrator",
		"round":      step + 1,
		"attempt":    attempt,
		"elapsed_ms": time.Since(startedAt).Milliseconds(),
	}
	if err != nil {
		payload["error"] = err.Error()
		emitRunEvent(state.config, "llm_error", payload)
		return domainllm.Message{}, err
	}
	payload["has_tool_calls"] = len(message.ToolCalls) > 0
	emitRunEvent(state.config, "llm_done", payload)
	return message, nil
}

func (state *runState) handleTextResponse(step int, message domainllm.Message) (*RunResult, bool, error) {
	if hasCompletionRequirement(state.config.Completion) && !completionRequirementSatisfied(state.ledger.Traces, state.config.Completion) {
		if step >= state.config.Budget.MaxRounds {
			return nil, false, fmt.Errorf("工具链流程未完成：必须成功调用 %s", completionRequirementLabel(state.config.Completion))
		}
		state.messages = append(state.messages, message)
		instruction := strings.TrimSpace(state.config.Completion.IncompleteMessage)
		if instruction == "" {
			instruction = fmt.Sprintf("任务尚未完成。你刚才输出的内容只是待处理草稿，不能作为最终回答；请继续调用必要工具，并以成功调用 %s 结束流程。", completionRequirementLabel(state.config.Completion))
		}
		state.messages = append(state.messages, domainllm.Message{Role: "user", Content: instruction})
		emitRunEvent(state.config, "status", map[string]any{"message": instruction, "step": step + 1})
		return nil, true, nil
	}

	text := strings.TrimSpace(message.Content)
	reasoning := strings.TrimSpace(message.ReasoningContent)
	shouldSynthesize := len(state.ledger.Traces) > 0 && state.config.Synthesis.Enabled && (state.config.Synthesis.Always || text == "")
	if shouldSynthesize {
		text, reasoning = state.synthesizeOrSummarize()
	}
	if text == "" && len(state.ledger.Traces) > 0 {
		text = summarizeToolTraces(state.ledger.Traces)
	}
	result := &RunResult{Message: text, Reasoning: reasoning, Traces: state.ledger.Traces}
	emitRunEvent(state.config, "done", result)
	return result, false, nil
}

// synthesizeOrSummarize prefers a configured model synthesis and falls back to
// a protocol-neutral trace summary when no synthesis is available.
func (state *runState) synthesizeOrSummarize() (string, string) {
	message := summarizeToolTraces(state.ledger.Traces)
	if state.config.Synthesis.Enabled {
		emitRunEvent(state.config, "status", map[string]any{"message": "正在根据工具结果生成结论"})
		if synthesized, reasoning, err := state.synthesizeToolAnswer(); err == nil && strings.TrimSpace(synthesized) != "" {
			return synthesized, reasoning
		} else if err != nil {
			message += "\n\n注意：工具结果二次总结失败，已退回工具摘要：" + err.Error()
		}
	}
	return message, ""
}

// synthesizeToolAnswer consumes recorded evidence without exposing tools.
func (state *runState) synthesizeToolAnswer() (string, string, error) {
	userTask := ""
	for index := len(state.originalMessages) - 1; index >= 0; index-- {
		if state.originalMessages[index].Role == "user" {
			userTask = state.originalMessages[index].Content
			break
		}
	}
	request := state.synthesisRequest
	request.Messages = []domainllm.Message{
		{Role: "system", Content: "你是工具结果总结器。不得再调用工具，只能依据用户问题和已记录的工具结果回答。结论应明确区分已证实的事实、工具失败或缺失的证据，以及仍需用户或后续工具确认的部分；不得编造工具没有返回的信息。"},
		{Role: "user", Content: fmt.Sprintf("用户原始问题：\n%s\n\n工具结果：\n%s\n\n请直接回答用户问题。", userTask, toolTraceContext(state.ledger.Traces, 3000))},
	}
	if state.config.OnEvent != nil {
		startedAt := time.Now()
		emitRunEvent(state.config, "llm_start", map[string]any{"phase": "synthesis", "mode": "stream"})
		text, reasoning, err := state.streamSynthesis(request)
		if err == nil {
			emitRunEvent(state.config, "llm_done", map[string]any{"phase": "synthesis", "mode": "stream", "elapsed_ms": time.Since(startedAt).Milliseconds()})
			return text, reasoning, nil
		}
		emitRunEvent(state.config, "llm_error", map[string]any{"phase": "synthesis", "mode": "stream", "elapsed_ms": time.Since(startedAt).Milliseconds(), "error": err.Error()})
	}
	startedAt := time.Now()
	emitRunEvent(state.config, "llm_start", map[string]any{"phase": "synthesis", "mode": "non_stream"})
	ctx, cancel := state.modelContext()
	defer cancel()
	response, err := state.llm.Chat(ctx, request)
	text := ""
	if err == nil {
		text = utils.CleanModelText(response.Message.Content)
		if response.FinishReason == "length" {
			err = &domainllm.OutputLimitError{Partial: text}
		}
	}
	payload := map[string]any{"phase": "synthesis", "mode": "non_stream", "elapsed_ms": time.Since(startedAt).Milliseconds()}
	if err != nil {
		payload["error"] = err.Error()
		emitRunEvent(state.config, "llm_error", payload)
	} else {
		emitRunEvent(state.config, "llm_done", payload)
	}
	return text, "", err
}

func (state *runState) streamSynthesis(request domainllm.ChatRequest) (string, string, error) {
	ctx, cancel := state.modelContext()
	defer cancel()
	stream, err := state.llm.Stream(ctx, request)
	if err != nil {
		return "", "", err
	}
	defer stream.Close()
	var answer, reasoning strings.Builder
	for {
		event, err := stream.Recv()
		if errors.Is(err, io.EOF) {
			return answer.String(), reasoning.String(), nil
		}
		if err != nil {
			return "", "", err
		}
		if event.ReasoningDelta != "" {
			reasoning.WriteString(event.ReasoningDelta)
			emitRunEvent(state.config, "reasoning", map[string]any{"delta": event.ReasoningDelta, "stage": "synthesis"})
		}
		if event.ContentDelta != "" {
			answer.WriteString(event.ContentDelta)
			emitRunEvent(state.config, "synthesis_delta", map[string]any{"delta": event.ContentDelta})
		}
		if event.FinishReason == "length" {
			return "", "", &domainllm.OutputLimitError{}
		}
	}
}

func (state *runState) modelContext() (context.Context, context.CancelFunc) {
	if state.config.ModelTimeout > 0 {
		return context.WithTimeout(state.config.ModelContext, state.config.ModelTimeout)
	}
	return state.config.ModelContext, func() {}
}

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
		toolLines = append(toolLines, fmt.Sprintf("工具 %s 返回：%s", trace.ToolName, utils.TruncateRunes(output, perTraceMax)))
	}
	return strings.Join(toolLines, "\n\n")
}

// summarizeToolTraces is the generic no-model fallback. It deliberately does
// not infer product semantics from tool names or result shapes.
func summarizeToolTraces(traces []Trace) string {
	if len(traces) == 0 {
		return "工具链未调用任何工具。"
	}
	var output strings.Builder
	fmt.Fprintf(&output, "工具执行摘要：已执行 %d 个工具。", len(traces))
	for _, trace := range traces {
		if trace.Status == "error" {
			fmt.Fprintf(&output, "\n- %s：失败，%s", trace.ToolName, trace.Error)
			continue
		}
		value := trace.outputContext
		if strings.TrimSpace(value) == "" {
			value = trace.OutputSummary
		}
		fmt.Fprintf(&output, "\n- %s：%s", trace.ToolName, utils.TruncateRunes(value, 360))
	}
	return output.String()
}

func outputLimitRecoveryMessages(messages []domainllm.Message, options RunConfig) []domainllm.Message {
	retry := append([]domainllm.Message(nil), messages...)
	content := `上一轮输出额度已耗尽，且没有形成完整结果。不要复述材料，也不要输出思考过程。
保持用户原始任务要求的格式、数量和篇幅，直接重新生成完整最终内容；不要擅自缩短为摘要。如果任务确实需要可用工具，只调用当前最必要的工具。`
	if requirement := completionRequirementLabel(options.Completion); requirement != "" {
		content = fmt.Sprintf(`上一轮输出额度已耗尽，且尚未满足工具完成条件。不要复述材料，不要输出长正文或思考过程。
当前必须成功完成：%s。
不得把函数参数 JSON 当成普通正文返回。请通过 tool_calls 协议调用一个当前最必要的可用工具；若写操作存在前置读取条件，先完成前置工具。完成条件未满足前不得直接回答。`, requirement)
	}
	return append(retry, domainllm.Message{Role: "user", Content: content})
}

func isolatedToolRoundMessages(originalMessages []domainllm.Message, traces []Trace, options RunConfig) []domainllm.Message {
	next := append([]domainllm.Message(nil), originalMessages...)
	requirement := completionRequirementLabel(options.Completion)
	if requirement == "" {
		requirement = "以用户原始目标为准，不强制调用特定工具"
	}
	content := fmt.Sprintf(`任务目标没有改变。始终以此前的用户任务为目标，不得把最近一次工具返回误当成新目标。
当前完成条件：%s

本轮累计执行进度：
%s

如果还缺必要信息，请继续调用最少量的工具；如果信息已经足够，请不要再调用工具，直接回答用户原问题。
不要重复调用已经返回足够结果的同一工具和相同参数，尤其不得重放已经成功的 mutation。
已经成功执行且从工具列表中消失的 run-once 工具仍然有效，请使用进度记录中的结果。
如果上一轮某个工具失败，只修正该失败调用的参数或前置条件，然后从失败位置继续。`, requirement, isolatedToolProgressContext(traces, 18000))
	return append(next, domainllm.Message{Role: "user", Content: content})
}

func isolatedToolProgressContext(traces []Trace, maxRunes int) string {
	if len(traces) == 0 {
		return "尚未执行任何工具。"
	}
	var progress strings.Builder
	progress.WriteString("执行账本（成功项表示已完成，不得重放）：\n")
	for index, trace := range traces {
		status := "成功"
		detail := utils.TruncateRunes(trace.OutputSummary, 180)
		if trace.Status == "error" {
			status = "失败"
			detail = utils.TruncateRunes(trace.Error, 180)
		}
		fmt.Fprintf(&progress, "%d. %s [%s]", index+1, trace.ToolName, status)
		if input := summarizeTraceInput(trace.Input); input != "" {
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
		evidence = append(evidence, fmt.Sprintf("工具 %s 的有效结果（越靠前越新）：%s", trace.ToolName, utils.TruncateRunes(context, 1200)))
	}
	if len(evidence) > 0 {
		progress.WriteString("\n最近有效查询结果：\n")
		progress.WriteString(strings.Join(evidence, "\n\n"))
	}
	return utils.TruncateRunes(progress.String(), maxRunes)
}

func summarizeTraceInput(raw json.RawMessage) string {
	if len(raw) == 0 {
		return ""
	}
	var input any
	if err := json.Unmarshal(raw, &input); err != nil {
		return utils.TruncateRunes(string(raw), 120)
	}
	// Bound each top-level value before the whole input. Otherwise a long nested
	// value sorted before an ID consumes the entire preview and hides that ID.
	if fields, ok := input.(map[string]any); ok {
		for key, value := range fields {
			encoded, err := json.Marshal(value)
			if err == nil && len([]rune(string(encoded))) > 48 {
				fields[key] = utils.TruncateRunes(string(encoded), 48)
			}
		}
	}
	encoded, err := json.Marshal(input)
	if err != nil {
		return utils.TruncateRunes(string(raw), 120)
	}
	return utils.TruncateRunes(string(encoded), 120)
}
