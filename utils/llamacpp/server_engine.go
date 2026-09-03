package llamacpp

import (
	"encoding/json"
	"errors"
	"fmt"
	"strings"

	"github.com/go-resty/resty/v2"
)

type ServerEngine struct {
	c *resty.Client
	o Options
}

func newServerEngine(opt Options) *ServerEngine {
	c := resty.New()
	c.SetBaseURL(strings.TrimRight(opt.BaseURL, "/"))
	c.SetHeader("Content-Type", "application/json")
	c.SetHeader("Accept", "application/json")
	return &ServerEngine{c: c, o: opt}
}

func (e *ServerEngine) Close() error { return nil }

func (e *ServerEngine) Reset() error { return nil }

func (e *ServerEngine) Chat(messages []Message, opt Options) (string, error) {
	opt.applyDefaults()
	if e == nil || e.c == nil {
		return "", errors.New("llamacpp: nil ServerEngine")
	}

	// 若调用方未显式带 system，则使用 opt.SystemPrompt 兜底。
	if len(messages) == 0 || strings.ToLower(strings.TrimSpace(messages[0].Role)) != "system" {
		messages = append([]Message{{Role: "system", Content: opt.SystemPrompt}}, messages...)
	}

	req := map[string]interface{}{
		"model":       opt.Model,
		"messages":    messages,
		"temperature": opt.Temperature,
		"max_tokens":  opt.MaxTokens,
		"stream":      false,
	}
	if opt.Seed != 0 {
		req["seed"] = opt.Seed
	}

	resp, err := e.c.R().SetBody(req).Post("/v1/chat/completions")
	if err != nil {
		return "", err
	}
	if resp.IsError() {
		return "", fmt.Errorf("llamacpp server error: %s", resp.String())
	}

	var parsed struct {
		Choices []struct {
			Message struct {
				Content string `json:"content"`
			} `json:"message"`
		} `json:"choices"`
	}
	if err := json.Unmarshal(resp.Body(), &parsed); err != nil {
		return "", err
	}
	if len(parsed.Choices) == 0 {
		return "", errors.New("llamacpp: empty response")
	}
	return strings.TrimSpace(parsed.Choices[0].Message.Content), nil
}

// Complete 通过 llama-server 的 OpenAI-compatible `/v1/chat/completions` 做单轮封装。
func (e *ServerEngine) Complete(prompt string, opt Options) (string, error) {
	return e.Chat([]Message{{Role: "user", Content: prompt}}, opt)
}
func (e *ServerEngine) Embedding(text string, opt Options) ([]float32, error) {
	// 1. 构造 OpenAI 格式请求
	req := map[string]interface{}{
		"input": text,
		"model": opt.Model, // 如果启动 server 时指定了别名，这里需要匹配
	}

	// 2. 调用 /v1/embeddings
	resp, err := e.c.R().SetBody(req).Post("/v1/embeddings")
	if err != nil {
		return nil, err
	}
	if resp.IsError() {
		return nil, fmt.Errorf("llamacpp embedding error: %s", resp.String())
	}

	// 3. 解析响应
	var parsed struct {
		Data []struct {
			Embedding []float32 `json:"embedding"`
		} `json:"data"`
	}
	if err := json.Unmarshal(resp.Body(), &parsed); err != nil {
		return nil, err
	}

	if len(parsed.Data) == 0 {
		return nil, errors.New("empty embedding response")
	}

	return parsed.Data[0].Embedding, nil
}
