package llamacpp

import (
	"context"
	"errors"
	"fmt"
	"strings"

	"github.com/openai/openai-go/v3"
	"github.com/openai/openai-go/v3/option"
)

// ServerEngine is the llama-server protocol adapter. It uses the official
// OpenAI SDK because llama-server exposes the OpenAI-compatible API.
type ServerEngine struct {
	client openai.Client
	o      Options
}

func newServerEngine(opt Options) *ServerEngine {
	baseURL := strings.TrimRight(opt.BaseURL, "/")
	if !strings.HasSuffix(baseURL, "/v1") {
		baseURL += "/v1"
	}
	return &ServerEngine{
		client: openai.NewClient(option.WithAPIKey(""), option.WithBaseURL(baseURL)),
		o:      opt,
	}
}

func (e *ServerEngine) Close() error { return nil }

func (e *ServerEngine) Reset() error { return nil }

// Chat preserves the legacy Engine contract. New request-facing code should
// use the context-aware internal AI provider instead.
func (e *ServerEngine) Chat(messages []Message, opt Options) (string, error) {
	opt.applyDefaults()
	if e == nil {
		return "", errors.New("llamacpp: nil ServerEngine")
	}
	if len(messages) == 0 || strings.ToLower(strings.TrimSpace(messages[0].Role)) != "system" {
		messages = append([]Message{{Role: "system", Content: opt.SystemPrompt}}, messages...)
	}

	request := openai.ChatCompletionNewParams{Model: openai.ChatModel(opt.Model)}
	for _, message := range messages {
		switch strings.ToLower(strings.TrimSpace(message.Role)) {
		case "system":
			request.Messages = append(request.Messages, openai.SystemMessage(message.Content))
		case "assistant":
			request.Messages = append(request.Messages, openai.AssistantMessage(message.Content))
		case "user", "":
			request.Messages = append(request.Messages, openai.UserMessage(message.Content))
		default:
			return "", fmt.Errorf("llamacpp: unsupported message role %q", message.Role)
		}
	}
	temperature := float64(opt.Temperature)
	request.Temperature = openai.Float(temperature)
	request.MaxTokens = openai.Int(int64(opt.MaxTokens))
	if opt.Seed != 0 {
		request.Seed = openai.Int(opt.Seed)
	}

	response, err := e.client.Chat.Completions.New(context.Background(), request)
	if err != nil {
		return "", fmt.Errorf("llamacpp chat completion: %w", err)
	}
	if len(response.Choices) == 0 {
		return "", errors.New("llamacpp: empty response")
	}
	return strings.TrimSpace(response.Choices[0].Message.Content), nil
}

func (e *ServerEngine) Complete(prompt string, opt Options) (string, error) {
	return e.Chat([]Message{{Role: "user", Content: prompt}}, opt)
}

func (e *ServerEngine) Embedding(text string, opt Options) ([]float32, error) {
	if e == nil {
		return nil, errors.New("llamacpp: nil ServerEngine")
	}
	response, err := e.client.Embeddings.New(context.Background(), openai.EmbeddingNewParams{
		Model: opt.Model,
		Input: openai.EmbeddingNewParamsInputUnion{OfString: openai.String(text)},
	})
	if err != nil {
		return nil, fmt.Errorf("llamacpp embedding: %w", err)
	}
	if len(response.Data) == 0 {
		return nil, errors.New("llamacpp: empty embedding response")
	}
	vector := make([]float32, len(response.Data[0].Embedding))
	for index, value := range response.Data[0].Embedding {
		vector[index] = float32(value)
	}
	return vector, nil
}

func (e *ServerEngine) Rerank(query string, documents []string) ([]float32, error) {
	return nil, errors.New("llamacpp server rerank is not supported by the OpenAI-compatible API")
}
