// Package llm is a compatibility facade for existing application services.
// New application code should depend on internal/ai/llm directly.
package llm

import (
	"InkFlow/config"
	"InkFlow/global"
	domain "InkFlow/internal/ai/llm"
	"InkFlow/internal/ai/llm/providers"
	"InkFlow/utils/inference"
	"context"
	"encoding/json"
	"errors"
	"fmt"
	"strings"
	"time"
)

// Message is retained as an import-compatible alias. Its definition belongs to
// the provider-neutral LLM domain rather than an OpenAI wire schema.
type Message = domain.Message

type RerankResult struct {
	Index int
	Score float32
	Text  string
}

type RerankResponse struct{ Results []RerankResult }

type GenerateOptions struct {
	Temperature float64
	MaxTokens   int
	Context     context.Context
	Timeout     time.Duration
	Seed        int64
	Model       string
	OnDelta     func(delta string)
	TopP        float64
	TopK        int
	LLM         *config.LLM
	Tools       []domain.ToolDefinition
	ToolChoice  any
	// Reasoning is an optional provider-neutral intent. Provider adapters decide
	// how to encode it; callers never construct vendor-specific request fields.
	Reasoning *domain.Reasoning
}

// OutputLimitError remains import-compatible with the provider-neutral error.
type OutputLimitError = domain.OutputLimitError

func IsOutputLimitError(err error) bool {
	var target *OutputLimitError
	return errors.As(err, &target)
}

func OutputLimitPartial(err error) string {
	var target *OutputLimitError
	if !errors.As(err, &target) || target == nil {
		return ""
	}
	return target.Partial
}

func requestContext(opt GenerateOptions) (context.Context, context.CancelFunc) {
	ctx := opt.Context
	if ctx == nil {
		ctx = context.Background()
	}
	if opt.Timeout > 0 {
		return context.WithTimeout(ctx, opt.Timeout)
	}
	return ctx, func() {}
}

func Generate(systemPrompt string, userPrompt string, temp float64) (string, error) {
	return GenerateWithOptions(systemPrompt, userPrompt, GenerateOptions{Temperature: temp, MaxTokens: 8192}, false)
}

func GenerateWithOptions(systemPrompt string, userPrompt string, opt GenerateOptions, stream bool) (string, error) {
	messages := []Message{{Role: "system", Content: systemPrompt}, {Role: "user", Content: userPrompt}}
	if !stream {
		return GenerateMessages(messages, opt)
	}

	var content strings.Builder
	if err := GenerateMessagesStream(messages, opt, func(delta StreamDelta) error {
		content.WriteString(delta.Content)
		emitDelta(opt, delta.ReasoningContent+delta.Content)
		return nil
	}); err != nil {
		return CleanJSON(content.String()), err
	}
	return CleanJSON(content.String()), nil
}

func emitDelta(opt GenerateOptions, delta string) {
	if opt.OnDelta != nil && delta != "" {
		opt.OnDelta(delta)
	}
}

func GenerateMessages(messages []Message, opt GenerateOptions) (string, error) {
	provider, request, ctx, cancel, err := prepareRequest(messages, opt)
	if err != nil {
		return "", err
	}
	defer cancel()

	response, err := provider.Chat(ctx, request)
	if err != nil {
		return "", err
	}
	content := CleanJSON(response.Message.Content)
	if response.FinishReason == "length" {
		return content, &OutputLimitError{Partial: content}
	}
	return content, nil
}

func GenerateMessagesWithToolCalls(messages []Message, opt GenerateOptions) (Message, error) {
	provider, request, ctx, cancel, err := prepareRequest(messages, opt)
	if err != nil {
		return Message{}, err
	}
	defer cancel()

	response, err := provider.Chat(ctx, request)
	if err != nil {
		return Message{}, err
	}
	if response.FinishReason == "length" {
		return Message{}, &OutputLimitError{ToolCall: true}
	}
	message := fromDomainMessage(response.Message)
	message.Content = CleanJSON(message.Content)
	return message, nil
}

func prepareRequest(messages []Message, opt GenerateOptions) (domain.Provider, domain.ChatRequest, context.Context, context.CancelFunc, error) {
	provider, request, err := PrepareChatRequest(messages, opt)
	if err != nil {
		return nil, domain.ChatRequest{}, nil, nil, err
	}
	ctx, cancel := requestContext(opt)
	return provider, request, ctx, cancel, nil
}

// PrepareChatRequest converts legacy options to the existing Provider contract.
// It does not send requests or start a deadline; callers own per-call contexts.
func PrepareChatRequest(messages []Message, opt GenerateOptions) (domain.Provider, domain.ChatRequest, error) {
	cfg := global.GVA_CONFIG.LLM
	if opt.LLM != nil {
		cfg = *opt.LLM
	}
	provider, err := providers.New(cfg)
	if err != nil {
		return nil, domain.ChatRequest{}, err
	}

	if opt.MaxTokens <= 0 {
		opt.MaxTokens = 1024
	}
	if opt.Temperature < 0 {
		opt.Temperature = 0.7
	}
	if opt.TopP == 0 {
		opt.TopP = cfg.TopP
		if opt.TopP == 0 {
			opt.TopP = 0.9
		}
	}
	if provider.Capabilities().TopK && opt.TopK == 0 {
		opt.TopK = cfg.TopK
		if opt.TopK == 0 {
			opt.TopK = 40
		}
	}

	modelName := strings.TrimSpace(opt.Model)
	if modelName == "" {
		modelName = strings.TrimSpace(cfg.ModelDefault)
	}
	if modelName == "" {
		return nil, domain.ChatRequest{}, fmt.Errorf("calling LLM API requires a model name")
	}
	toolChoice, err := toDomainToolChoice(opt.ToolChoice)
	if err != nil {
		return nil, domain.ChatRequest{}, err
	}

	temperature, maxTokens, topP := opt.Temperature, opt.MaxTokens, opt.TopP
	request := domain.ChatRequest{
		Model:       modelName,
		Messages:    messages,
		Temperature: &temperature,
		MaxTokens:   &maxTokens,
		TopP:        &topP,
		Tools:       opt.Tools,
		ToolChoice:  toolChoice,
		Reasoning:   opt.Reasoning,
	}
	if opt.Seed != 0 {
		seed := opt.Seed
		request.Seed = &seed
	}
	if provider.Capabilities().TopK && opt.TopK > 0 {
		topK := opt.TopK
		request.TopK = &topK
	}
	// Tool selection does not benefit from visible reasoning and can delay calls.
	if request.Reasoning == nil && len(request.Tools) > 0 && provider.Capabilities().Reasoning {
		request.Reasoning = &domain.Reasoning{Enabled: false}
	}
	return provider, request, nil
}

func fromDomainMessage(message domain.Message) Message {
	return message
}

func toDomainToolChoice(choice any) (*domain.ToolChoice, error) {
	if choice == nil {
		return nil, nil
	}
	if typed, ok := choice.(*domain.ToolChoice); ok {
		return typed, nil
	}
	if typed, ok := choice.(domain.ToolChoice); ok {
		return &typed, nil
	}
	if mode, ok := choice.(string); ok {
		return parseToolChoice(mode, "")
	}
	if object, ok := choice.(map[string]any); ok && object["type"] == "function" {
		if function, ok := object["function"].(map[string]any); ok {
			name, _ := function["name"].(string)
			return parseToolChoice("function", name)
		}
	}
	return nil, &domain.InvalidRequestError{Reason: "unsupported tool choice"}
}

func parseToolChoice(mode, name string) (*domain.ToolChoice, error) {
	choice := &domain.ToolChoice{Mode: domain.ToolChoiceMode(strings.ToLower(strings.TrimSpace(mode))), Name: name}
	switch choice.Mode {
	case domain.ToolChoiceAuto, domain.ToolChoiceNone, domain.ToolChoiceRequired, domain.ToolChoiceFunction:
		return choice, nil
	default:
		return nil, &domain.InvalidRequestError{Reason: fmt.Sprintf("unsupported tool choice %q", mode)}
	}
}

// GetEmbedding delegates to the dedicated inference domain. It remains here
// only to avoid breaking existing callers during migration.
func GetEmbedding(ctx context.Context, text string) ([]float32, error) {
	key := embeddingCacheKey(text)
	if vec, ok := embeddingCacheGet(key); ok {
		return vec, nil
	}
	if vec, ok := embeddingSQLiteGet(key); ok {
		embeddingCachePut(key, vec)
		return vec, nil
	}

	value, err, _ := global.GVA_Concurrency_Control.Do("embedding:"+key, func() (any, error) {
		if vec, ok := embeddingCacheGet(key); ok {
			return vec, nil
		}
		if vec, ok := embeddingSQLiteGet(key); ok {
			embeddingCachePut(key, vec)
			return vec, nil
		}
		if ctx == nil {
			ctx = context.Background()
		}
		vec, err := inference.ActiveProvider().Embedding(ctx, text)
		if err != nil {
			return nil, err
		}
		embeddingCachePut(key, vec)
		embeddingSQLitePut(key, vec)
		return vec, nil
	})
	if err != nil {
		return nil, err
	}
	return cloneVec(value.([]float32)), nil
}

func Rerank(ctx context.Context, query string, docs []string, topN int) (*RerankResponse, error) {
	if ctx == nil {
		ctx = context.Background()
	}
	results, err := inference.ActiveProvider().Rerank(ctx, query, docs, topN)
	if err != nil {
		return nil, err
	}
	response := &RerankResponse{Results: make([]RerankResult, 0, len(results))}
	for _, result := range results {
		text := result.Text
		if text == "" && result.Index >= 0 && result.Index < len(docs) {
			text = docs[result.Index]
		}
		response.Results = append(response.Results, RerankResult{Index: result.Index, Score: result.Score, Text: text})
	}
	return response, nil
}

func CleanJSON(input string) string {
	input = strings.TrimSpace(input)
	input = strings.TrimPrefix(input, "```json")
	input = strings.TrimPrefix(input, "```")
	input = strings.TrimSuffix(input, "```")
	return strings.TrimSpace(input)
}

// Retained for focused diagnostic tests. Production chat decoding is handled by
// the official SDK in the provider adapter.
func decodeLLMJSONResponse(body []byte, statusCode int, contentType, requestURL string, target any) error {
	if err := json.Unmarshal(body, target); err != nil {
		preview := strings.TrimSpace(string(body))
		if runes := []rune(preview); len(runes) > 500 {
			preview = string(runes[:500]) + "...(truncated)"
		}
		return fmt.Errorf("LLM API 返回了非 JSON 响应: status=%d, content_type=%q, url=%q, body=%q: %w", statusCode, contentType, requestURL, preview, err)
	}
	return nil
}
