package llm

import (
	"bufio"
	"encoding/json"
	"errors"
	"fmt"
	"io"
	"strings"
)

type StreamDelta struct {
	Content          string
	ReasoningContent string
	FinishReason     string
}

func GenerateMessagesStream(messages []Message, opt GenerateOptions, onDelta func(StreamDelta) error) error {
	if onDelta == nil {
		return errors.New("stream callback is required")
	}
	cfg, client := resolveClient(opt)
	if client == nil {
		return errors.New("LLM client is not initialized")
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
	if supportsTopK(cfg.BaseUrl) && opt.TopK == 0 {
		opt.TopK = cfg.TopK
		if opt.TopK == 0 {
			opt.TopK = 40
		}
	}

	modelName := opt.Model
	if modelName == "" {
		modelName = cfg.ModelDefault
	}
	if modelName == "" {
		return fmt.Errorf("calling LLM API requires a specific model name")
	}

	req := ChatRequest{
		Model:       modelName,
		Messages:    messages,
		Temperature: opt.Temperature,
		MaxTokens:   opt.MaxTokens,
		Stream:      true,
		Seed:        opt.Seed,
		TopP:        opt.TopP,
		TopK:        opt.TopK,
		Thinking:    resolveThinking(cfg, modelName, opt),
	}

	ctx, cancel := requestContext(opt)
	defer cancel()
	resp, err := client.R().
		SetContext(ctx).
		SetHeader("Accept", "text/event-stream").
		SetDoNotParseResponse(true).
		SetBody(req).
		Post("/chat/completions")
	if err != nil {
		return err
	}
	if resp.IsError() {
		return fmt.Errorf("LLM API Error: %s", streamErrorBody(resp.RawBody(), resp.String()))
	}

	raw := resp.RawBody()
	if raw == nil {
		return errors.New("empty stream body from LLM")
	}
	defer raw.Close()

	scanner := bufio.NewScanner(raw)
	scanner.Buffer(make([]byte, 0, 64*1024), 1024*1024)
	for scanner.Scan() {
		line := strings.TrimSpace(scanner.Text())
		if line == "" || strings.HasPrefix(line, ":") {
			continue
		}
		if !strings.HasPrefix(line, "data:") {
			continue
		}

		payload := strings.TrimSpace(strings.TrimPrefix(line, "data:"))
		if payload == "[DONE]" {
			return nil
		}

		var chunk struct {
			Choices []struct {
				Delta struct {
					Content          string `json:"content"`
					ReasoningContent string `json:"reasoning_content"`
				} `json:"delta"`
				FinishReason any `json:"finish_reason"`
			} `json:"choices"`
		}
		if err := json.Unmarshal([]byte(payload), &chunk); err != nil {
			return err
		}
		for _, choice := range chunk.Choices {
			delta := StreamDelta{
				Content:          choice.Delta.Content,
				ReasoningContent: choice.Delta.ReasoningContent,
			}
			if reason, ok := choice.FinishReason.(string); ok {
				delta.FinishReason = reason
			}
			if delta.Content == "" && delta.ReasoningContent == "" && delta.FinishReason == "" {
				continue
			}
			if err := onDelta(delta); err != nil {
				return err
			}
			if delta.FinishReason == "length" {
				return &OutputLimitError{}
			}
		}
	}
	return scanner.Err()
}

func streamErrorBody(raw io.ReadCloser, fallback string) string {
	if raw == nil {
		return strings.TrimSpace(fallback)
	}
	defer raw.Close()
	body, err := io.ReadAll(raw)
	if err != nil {
		return strings.TrimSpace(fallback)
	}
	if text := strings.TrimSpace(string(body)); text != "" {
		return text
	}
	return strings.TrimSpace(fallback)
}
