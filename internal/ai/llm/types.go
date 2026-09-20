// Package llm defines the provider-neutral language-model contract used by
// application services. It deliberately contains no vendor SDK types.
package llm

import (
	"context"
	"encoding/json"
	"io"
)

type Role string

const (
	RoleSystem    Role = "system"
	RoleUser      Role = "user"
	RoleAssistant Role = "assistant"
	RoleTool      Role = "tool"
)

type Message struct {
	Role             Role         `json:"role"`
	Content          string       `json:"content"`
	Images           []ImageInput `json:"images,omitempty"`
	ReasoningContent string       `json:"reasoning_content,omitempty"`
	ToolCallID       string       `json:"tool_call_id,omitempty"`
	ToolCalls        []ToolCall   `json:"tool_calls,omitempty"`
}

// ImageInput is a provider-neutral image attachment. The provider adapter owns
// the target protocol's data-URL or multipart representation.
type ImageInput struct {
	MIMEType string `json:"mime_type"`
	Data     []byte `json:"-"`
}

type ToolDefinition struct {
	Name        string         `json:"name"`
	Description string         `json:"description,omitempty"`
	InputSchema map[string]any `json:"input_schema,omitempty"`
}

type ToolCall struct {
	ID        string          `json:"id"`
	Name      string          `json:"name"`
	Arguments json.RawMessage `json:"arguments"`
}

type ToolCallDelta struct {
	Index     int    `json:"index"`
	ID        string `json:"id,omitempty"`
	Name      string `json:"name,omitempty"`
	Arguments string `json:"arguments,omitempty"`
}

type ToolChoiceMode string

const (
	ToolChoiceAuto     ToolChoiceMode = "auto"
	ToolChoiceNone     ToolChoiceMode = "none"
	ToolChoiceRequired ToolChoiceMode = "required"
	ToolChoiceFunction ToolChoiceMode = "function"
)

type ToolChoice struct {
	Mode ToolChoiceMode `json:"mode"`
	Name string         `json:"name,omitempty"`
}

// Reasoning is a provider-neutral intent. The selected provider owns its wire
// representation and can reject it when unsupported.
type Reasoning struct {
	Enabled bool `json:"enabled"`
}

type ChatRequest struct {
	Model       string           `json:"model"`
	Messages    []Message        `json:"messages"`
	Temperature *float64         `json:"temperature,omitempty"`
	MaxTokens   *int             `json:"max_tokens,omitempty"`
	TopP        *float64         `json:"top_p,omitempty"`
	TopK        *int             `json:"top_k,omitempty"`
	Seed        *int64           `json:"seed,omitempty"`
	Tools       []ToolDefinition `json:"tools,omitempty"`
	ToolChoice  *ToolChoice      `json:"tool_choice,omitempty"`
	Reasoning   *Reasoning       `json:"reasoning,omitempty"`
}

type Usage struct {
	InputTokens  int64 `json:"input_tokens,omitempty"`
	OutputTokens int64 `json:"output_tokens,omitempty"`
	TotalTokens  int64 `json:"total_tokens,omitempty"`
}

type ChatResponse struct {
	Message      Message `json:"message"`
	FinishReason string  `json:"finish_reason,omitempty"`
	Usage        Usage   `json:"usage,omitempty"`
}

type StreamEvent struct {
	ContentDelta   string          `json:"content_delta,omitempty"`
	ReasoningDelta string          `json:"reasoning_delta,omitempty"`
	ToolCalls      []ToolCallDelta `json:"tool_calls,omitempty"`
	FinishReason   string          `json:"finish_reason,omitempty"`
	Usage          *Usage          `json:"usage,omitempty"`
}

// ChatStream yields provider-neutral chunks. It returns io.EOF when the stream
// has completed and must always be closed by its caller.
type ChatStream interface {
	Recv() (StreamEvent, error)
	Close() error
}

var _ io.Closer = (ChatStream)(nil)

// Provider is the only LLM interface application code is allowed to depend on.
// Context controls cancellation and deadlines for every network operation.
type Provider interface {
	Name() string
	Capabilities() Capabilities
	Chat(ctx context.Context, request ChatRequest) (*ChatResponse, error)
	Stream(ctx context.Context, request ChatRequest) (ChatStream, error)
}
