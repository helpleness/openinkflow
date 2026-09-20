package orchestrator

import (
	"encoding/json"
	"fmt"
	"strings"

	domainllm "InkFlow/internal/ai/llm"
)

func hasSuccessfulTool(traces []Trace, name string) bool {
	for _, trace := range traces {
		if trace.ToolName == name && trace.Status == "ok" {
			return true
		}
	}
	return false
}

func successfulToolCallCount(traces []Trace, name string) int {
	count := 0
	for _, trace := range traces {
		if trace.ToolName == name && trace.Status == "ok" {
			count++
		}
	}
	return count
}

func hasCompletionRequirement(options CompletionPolicy) bool {
	return len(options.RequiredAll) > 0 || len(options.RequiredAny) > 0 || len(options.RequiredCounts) > 0
}

func completionRequirementSatisfied(traces []Trace, options CompletionPolicy) bool {
	for _, name := range options.RequiredAll {
		if name = strings.TrimSpace(name); name != "" && !hasSuccessfulTool(traces, name) {
			return false
		}
	}
	for name, requiredCount := range options.RequiredCounts {
		name = strings.TrimSpace(name)
		if name != "" && requiredCount > 0 && successfulToolCallCount(traces, name) < requiredCount {
			return false
		}
	}
	if len(options.RequiredAny) == 0 {
		return true
	}
	for _, name := range options.RequiredAny {
		if hasSuccessfulTool(traces, strings.TrimSpace(name)) {
			return true
		}
	}
	return false
}

func completionRequirementLabel(options CompletionPolicy) string {
	requiredNames := make([]string, 0, 1+len(options.RequiredAll)+len(options.RequiredCounts))
	seen := map[string]bool{}
	for _, name := range options.RequiredAll {
		if name = strings.TrimSpace(name); name != "" && !seen[name] {
			requiredNames = append(requiredNames, name)
			seen[name] = true
		}
	}
	for name, requiredCount := range options.RequiredCounts {
		name = strings.TrimSpace(name)
		if name == "" || requiredCount <= 0 || seen[name] {
			continue
		}
		requiredNames = append(requiredNames, fmt.Sprintf("%s 成功调用 %d 次", name, requiredCount))
		seen[name] = true
	}
	anyNames := make([]string, 0, len(options.RequiredAny))
	for _, name := range options.RequiredAny {
		if name = strings.TrimSpace(name); name != "" {
			anyNames = append(anyNames, name)
		}
	}
	if len(anyNames) > 0 {
		anyLabel := strings.Join(anyNames, " 或 ")
		if len(requiredNames) == 0 {
			return anyLabel
		}
		requiredNames = append(requiredNames, "("+anyLabel+")")
	}
	return strings.Join(requiredNames, " 且 ")
}

func (state *runState) toolLimitPayload(toolName string, tool Tool) map[string]any {
	if tool.Kind == KindLLM && state.ledger.LLMCalls >= state.config.Budget.MaxLLMTools {
		return map[string]any{"error": "llm tool call limit exceeded", "tool_name": toolName}
	}
	if tool.Kind == KindMutation && state.ledger.MutationCalls >= state.config.Budget.MaxMutations {
		return map[string]any{"error": "mutation tool call limit exceeded", "tool_name": toolName}
	}
	return nil
}

func availableLLMTools(registry *Registry, completed map[string]Trace, successfulCalls, attemptedCalls map[string]int) []domainllm.ToolDefinition {
	tools := registry.LLMTools()
	if len(completed) == 0 && len(successfulCalls) == 0 && len(attemptedCalls) == 0 {
		return tools
	}
	excluded := make(map[string]bool, len(completed)+len(successfulCalls)+len(attemptedCalls))
	for name := range completed {
		if llmName := registry.llmToolNames[name]; llmName != "" {
			excluded[llmName] = true
		}
	}
	for name, count := range successfulCalls {
		tool, ok := registry.tools[name]
		if !ok || !perRunLimitReached(tool, count) {
			continue
		}
		if llmName := registry.llmToolNames[name]; llmName != "" {
			excluded[llmName] = true
		}
	}
	for name, count := range attemptedCalls {
		tool, ok := registry.tools[name]
		if !ok || !perRunAttemptLimitReached(tool, count) {
			continue
		}
		if llmName := registry.llmToolNames[name]; llmName != "" {
			excluded[llmName] = true
		}
	}
	filtered := tools[:0]
	for _, tool := range tools {
		if !excluded[tool.Name] {
			filtered = append(filtered, tool)
		}
	}
	return filtered
}

func toolQueryCacheKey(mutationVersion int, toolName string, input any) string {
	encoded, err := json.Marshal(input)
	if err != nil {
		encoded = []byte(fmt.Sprint(input))
	}
	return fmt.Sprintf("%d:%s:%s", mutationVersion, toolName, encoded)
}

func completedRunOnce(name string, tool Tool, ledger RunLedger) (Trace, bool) {
	trace, exists := ledger.SuccessfulRunOnce[name]
	return trace, tool.RunOncePerRun && exists
}

func perRunLimitReached(tool Tool, successfulCalls int) bool {
	return tool.MaxCallsPerRun > 0 && successfulCalls >= tool.MaxCallsPerRun
}

func perRunAttemptLimitReached(tool Tool, attemptedCalls int) bool {
	return tool.MaxAttemptsPerRun > 0 && attemptedCalls >= tool.MaxAttemptsPerRun
}
