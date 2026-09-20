package llm

import "fmt"

type UnsupportedCapabilityError struct {
	Provider   string
	Capability string
}

func (e *UnsupportedCapabilityError) Error() string {
	return fmt.Sprintf("LLM provider %q does not support %s", e.Provider, e.Capability)
}

type InvalidRequestError struct {
	Reason string
}

func (e *InvalidRequestError) Error() string { return "invalid LLM request: " + e.Reason }

// OutputLimitError signals an incomplete model decision or answer.
type OutputLimitError struct {
	ToolCall bool
	Partial  string
}

func (e *OutputLimitError) Error() string {
	if e != nil && e.ToolCall {
		return "LLM 编排输出达到 max_tokens 上限，工具调用可能未完整生成"
	}
	return "LLM 输出达到 max_tokens 上限并被截断"
}
