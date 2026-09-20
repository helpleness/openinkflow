// Package openaicompat adapts OpenAI-compatible Chat Completions APIs through
// the official OpenAI Go SDK. Provider-specific extensions stay in this package.
package openaicompat

import (
	"InkFlow/internal/ai/llm"
	"context"
	"encoding/base64"
	"encoding/json"
	"fmt"
	"io"
	"strings"
	"time"

	"github.com/openai/openai-go/v3"
	"github.com/openai/openai-go/v3/option"
)

type Config struct {
	Name         string
	BaseURL      string
	APIKey       string
	DefaultModel string
	Timeout      time.Duration
	Capabilities llm.Capabilities
	// MutateRequest is confined to explicitly selected provider adapters.
	MutateRequest func(*openai.ChatCompletionNewParams, llm.ChatRequest) error
}

type Provider struct {
	client openai.Client
	config Config
}

type OpenAIProvider struct{ *Provider }

func NewOpenAIProvider(config Config) (*OpenAIProvider, error) {
	config.Capabilities = llm.Capabilities{Streaming: true, ToolCalling: true, StructuredOutputs: true, Vision: true}
	if strings.TrimSpace(config.Name) == "" {
		return nil, fmt.Errorf("openai-compatible provider name is required")
	}
	if strings.TrimSpace(config.DefaultModel) == "" {
		return nil, fmt.Errorf("default model is required for provider %q", config.Name)
	}

	options := []option.RequestOption{option.WithAPIKey(config.APIKey)}
	if baseURL := strings.TrimSpace(config.BaseURL); baseURL != "" {
		options = append(options, option.WithBaseURL(strings.TrimRight(baseURL, "/")))
	}
	return &OpenAIProvider{Provider: &Provider{client: openai.NewClient(options...), config: config}}, nil
}

func (p *Provider) Name() string { return p.config.Name }

func (p *Provider) Capabilities() llm.Capabilities { return p.config.Capabilities }

func (p *Provider) Chat(ctx context.Context, request llm.ChatRequest) (*llm.ChatResponse, error) {
	params, err := p.toParams(request)
	if err != nil {
		return nil, err
	}
	ctx, cancel := p.withTimeout(ctx)
	defer cancel()

	response, err := p.client.Chat.Completions.New(ctx, params)
	if err != nil {
		return nil, fmt.Errorf("%s chat completion: %w", p.Name(), err)
	}
	if len(response.Choices) == 0 {
		return nil, fmt.Errorf("%s chat completion returned no choices", p.Name())
	}

	choice := response.Choices[0]
	message := llm.Message{Role: llm.RoleAssistant, Content: choice.Message.Content, ReasoningContent: responseReasoning(choice.Message.RawJSON())}
	for _, call := range choice.Message.ToolCalls {
		if call.Type != "function" {
			continue
		}
		message.ToolCalls = append(message.ToolCalls, llm.ToolCall{
			ID:        call.ID,
			Name:      call.Function.Name,
			Arguments: []byte(call.Function.Arguments),
		})
	}

	return &llm.ChatResponse{
		Message:      message,
		FinishReason: choice.FinishReason,
		Usage: llm.Usage{
			InputTokens:  response.Usage.PromptTokens,
			OutputTokens: response.Usage.CompletionTokens,
			TotalTokens:  response.Usage.TotalTokens,
		},
	}, nil
}

func (p *Provider) Stream(ctx context.Context, request llm.ChatRequest) (llm.ChatStream, error) {
	if !p.Capabilities().Streaming {
		return nil, &llm.UnsupportedCapabilityError{Provider: p.Name(), Capability: "streaming"}
	}
	params, err := p.toParams(request)
	if err != nil {
		return nil, err
	}
	ctx, cancel := p.withTimeout(ctx)
	return &stream{stream: p.client.Chat.Completions.NewStreaming(ctx, params), cancel: cancel}, nil
}

func (p *Provider) withTimeout(ctx context.Context) (context.Context, context.CancelFunc) {
	if ctx == nil {
		ctx = context.Background()
	}
	if p.config.Timeout <= 0 {
		return ctx, func() {}
	}
	if _, hasDeadline := ctx.Deadline(); hasDeadline {
		return ctx, func() {}
	}
	return context.WithTimeout(ctx, p.config.Timeout)
}

func (p *Provider) toParams(request llm.ChatRequest) (openai.ChatCompletionNewParams, error) {
	model := request.Model
	if strings.TrimSpace(model) == "" {
		model = p.config.DefaultModel
	}
	if len(request.Messages) == 0 {
		return openai.ChatCompletionNewParams{}, &llm.InvalidRequestError{Reason: "at least one message is required"}
	}
	for _, message := range request.Messages {
		if len(message.Images) > 0 && !p.Capabilities().Vision {
			return openai.ChatCompletionNewParams{}, &llm.UnsupportedCapabilityError{Provider: p.Name(), Capability: "vision"}
		}
	}
	if len(request.Tools) > 0 && !p.Capabilities().ToolCalling {
		return openai.ChatCompletionNewParams{}, &llm.UnsupportedCapabilityError{Provider: p.Name(), Capability: "tool calling"}
	}
	if request.TopK != nil && !p.Capabilities().TopK {
		return openai.ChatCompletionNewParams{}, &llm.UnsupportedCapabilityError{Provider: p.Name(), Capability: "top_k"}
	}

	params := openai.ChatCompletionNewParams{Model: openai.ChatModel(model)}
	params.Messages = make([]openai.ChatCompletionMessageParamUnion, 0, len(request.Messages))
	for _, message := range request.Messages {
		converted, err := toMessage(message)
		if err != nil {
			return openai.ChatCompletionNewParams{}, err
		}
		params.Messages = append(params.Messages, converted)
	}
	if request.Temperature != nil {
		params.Temperature = openai.Float(*request.Temperature)
	}
	if request.MaxTokens != nil && *request.MaxTokens > 0 {
		params.MaxTokens = openai.Int(int64(*request.MaxTokens))
	}
	if request.TopP != nil {
		params.TopP = openai.Float(*request.TopP)
	}
	if request.Seed != nil {
		params.Seed = openai.Int(*request.Seed)
	}
	if request.TopK != nil {
		params.SetExtraFields(map[string]any{"top_k": *request.TopK})
	}
	if request.Reasoning != nil && !p.Capabilities().Reasoning {
		return openai.ChatCompletionNewParams{}, &llm.UnsupportedCapabilityError{Provider: p.Name(), Capability: "reasoning"}
	}
	for _, tool := range request.Tools {
		if strings.TrimSpace(tool.Name) == "" {
			return openai.ChatCompletionNewParams{}, &llm.InvalidRequestError{Reason: "tool name is required"}
		}
		params.Tools = append(params.Tools, openai.ChatCompletionFunctionTool(openai.FunctionDefinitionParam{
			Name:        tool.Name,
			Description: openai.String(tool.Description),
			Parameters:  openai.FunctionParameters(tool.InputSchema),
		}))
	}
	if request.ToolChoice != nil {
		switch request.ToolChoice.Mode {
		case llm.ToolChoiceAuto, llm.ToolChoiceNone, llm.ToolChoiceRequired:
			params.ToolChoice.OfAuto = openai.String(string(request.ToolChoice.Mode))
		case llm.ToolChoiceFunction:
			if strings.TrimSpace(request.ToolChoice.Name) == "" {
				return openai.ChatCompletionNewParams{}, &llm.InvalidRequestError{Reason: "tool choice function name is required"}
			}
			params.ToolChoice = openai.ToolChoiceOptionFunctionToolChoice(openai.ChatCompletionNamedToolChoiceFunctionParam{
				Name: request.ToolChoice.Name,
			})
		default:
			return openai.ChatCompletionNewParams{}, &llm.InvalidRequestError{Reason: "unknown tool choice"}
		}
	}
	if p.config.MutateRequest != nil {
		if err := p.config.MutateRequest(&params, request); err != nil {
			return openai.ChatCompletionNewParams{}, err
		}
	}
	return params, nil
}

func toMessage(message llm.Message) (openai.ChatCompletionMessageParamUnion, error) {
	switch message.Role {
	case llm.RoleSystem:
		return openai.SystemMessage(message.Content), nil
	case llm.RoleUser:
		if len(message.Images) == 0 {
			return openai.UserMessage(message.Content), nil
		}
		parts := make([]openai.ChatCompletionContentPartUnionParam, 0, 1+len(message.Images))
		if message.Content != "" {
			parts = append(parts, openai.TextContentPart(message.Content))
		}
		for _, image := range message.Images {
			if len(image.Data) == 0 {
				return openai.ChatCompletionMessageParamUnion{}, &llm.InvalidRequestError{Reason: "image input has no data"}
			}
			mimeType := strings.TrimSpace(image.MIMEType)
			if mimeType == "" {
				mimeType = "image/png"
			}
			parts = append(parts, openai.ImageContentPart(openai.ChatCompletionContentPartImageImageURLParam{
				URL: "data:" + mimeType + ";base64," + base64.StdEncoding.EncodeToString(image.Data),
			}))
		}
		return openai.UserMessage(parts), nil
	case llm.RoleTool:
		if strings.TrimSpace(message.ToolCallID) == "" {
			return openai.ChatCompletionMessageParamUnion{}, &llm.InvalidRequestError{Reason: "tool message requires tool_call_id"}
		}
		return openai.ToolMessage(message.Content, message.ToolCallID), nil
	case llm.RoleAssistant:
		assistant := openai.AssistantMessage(message.Content)
		if len(message.ToolCalls) == 0 {
			return assistant, nil
		}
		for _, call := range message.ToolCalls {
			if strings.TrimSpace(call.ID) == "" || strings.TrimSpace(call.Name) == "" {
				return openai.ChatCompletionMessageParamUnion{}, &llm.InvalidRequestError{Reason: "assistant tool call requires id and name"}
			}
			assistant.OfAssistant.ToolCalls = append(assistant.OfAssistant.ToolCalls, openai.ChatCompletionMessageToolCallUnionParam{
				OfFunction: &openai.ChatCompletionMessageFunctionToolCallParam{
					ID: call.ID,
					Function: openai.ChatCompletionMessageFunctionToolCallFunctionParam{
						Name:      call.Name,
						Arguments: string(call.Arguments),
					},
				},
			})
		}
		return assistant, nil
	default:
		return openai.ChatCompletionMessageParamUnion{}, &llm.InvalidRequestError{Reason: fmt.Sprintf("unsupported message role %q", message.Role)}
	}
}

type stream struct {
	stream interface {
		Next() bool
		Current() openai.ChatCompletionChunk
		Err() error
		Close() error
	}
	cancel context.CancelFunc
}

func (s *stream) Recv() (llm.StreamEvent, error) {
	if !s.stream.Next() {
		if err := s.stream.Err(); err != nil {
			return llm.StreamEvent{}, err
		}
		return llm.StreamEvent{}, io.EOF
	}
	chunk := s.stream.Current()
	event := llm.StreamEvent{}
	if chunk.Usage.TotalTokens > 0 || chunk.Usage.PromptTokens > 0 || chunk.Usage.CompletionTokens > 0 {
		event.Usage = &llm.Usage{InputTokens: chunk.Usage.PromptTokens, OutputTokens: chunk.Usage.CompletionTokens, TotalTokens: chunk.Usage.TotalTokens}
	}
	for _, choice := range chunk.Choices {
		event.ContentDelta += choice.Delta.Content
		event.ReasoningDelta += responseReasoning(choice.Delta.RawJSON())
		event.FinishReason = choice.FinishReason
		for _, call := range choice.Delta.ToolCalls {
			event.ToolCalls = append(event.ToolCalls, llm.ToolCallDelta{
				Index: int(call.Index), ID: call.ID, Name: call.Function.Name, Arguments: call.Function.Arguments,
			})
		}
	}
	return event, nil
}

func responseReasoning(raw string) string {
	if raw == "" {
		return ""
	}
	var payload struct {
		ReasoningContent string `json:"reasoning_content"`
	}
	_ = json.Unmarshal([]byte(raw), &payload)
	return payload.ReasoningContent
}

func (s *stream) Close() error {
	s.cancel()
	return s.stream.Close()
}
