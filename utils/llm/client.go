package llm

import (
	"InkFlow/config"
	"InkFlow/global"
	"InkFlow/utils/inference"
	"InkFlow/utils/llmclient"
	"context"
	"encoding/json"
	"errors"
	"fmt"
	"github.com/go-resty/resty/v2"
	"strings"
	"time"
)

// OpenAI 兼容的请求结构 (llama.cpp server 兼容 OpenAI API)
type ChatRequest struct {
	Model       string    `json:"model,omitempty"`
	Messages    []Message `json:"messages"`
	Temperature float64   `json:"temperature"`
	MaxTokens   int       `json:"max_tokens"`
	Stream      bool      `json:"stream"`
	Seed        int64     `json:"seed,omitempty"`  // 支持随机种子
	TopP        float64   `json:"top_p,omitempty"` // [新增] 注意 JSON tag 是 top_p
	TopK        int       `json:"top_k,omitempty"` // [新增] 注意 JSON tag 是 top_k
	Tools       []Tool    `json:"tools,omitempty"`
	ToolChoice  any       `json:"tool_choice,omitempty"`
	Thinking    *Thinking `json:"thinking,omitempty"`
}

// Thinking 控制支持该字段的模型是否启用思考模式。目前 DeepSeek V4 使用
// {"thinking":{"type":"enabled|disabled"}} 这一 OpenAI 兼容扩展。
type Thinking struct {
	Type string `json:"type"`
}

type Message struct {
	Role             string     `json:"role"`
	Content          string     `json:"content"`
	ReasoningContent string     `json:"reasoning_content,omitempty"`
	ToolCallID       string     `json:"tool_call_id,omitempty"`
	ToolCalls        []ToolCall `json:"tool_calls,omitempty"`
}
type RerankResult struct {
	Index int
	Score float32
	Text  string // 可选
}
type RerankResponse struct {
	Results []RerankResult
}
type GenerateOptions struct {
	Temperature float64
	MaxTokens   int
	Context     context.Context
	Timeout     time.Duration
	Seed        int64
	Model       string
	OnDelta     func(delta string)
	TopP        float64 // [新增]
	TopK        int     // [新增]
	LLM         *config.LLM
	Tools       []Tool
	ToolChoice  any
	// Thinking 仅在当前模型支持该扩展时写入请求。工具编排会自动关闭
	// DeepSeek V4 的思考模式，避免非流式工具选择阶段长期无可见输出。
	Thinking *Thinking
}

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
	ctx := context.Background()
	if opt.Context != nil {
		ctx = opt.Context
	}
	if opt.Timeout > 0 {
		return context.WithTimeout(ctx, opt.Timeout)
	}
	return ctx, func() {}
}

// 1. 同步对话生成 (用于 Agent 思考，非流式)
func Generate(systemPrompt string, userPrompt string, temp float64) (string, error) {
	return GenerateWithOptions(systemPrompt, userPrompt, GenerateOptions{
		Temperature: temp,
		MaxTokens:   8192,
	}, false)
}

func GenerateWithOptions(systemPrompt string, userPrompt string, opt GenerateOptions, stream bool) (string, error) {
	messages := []Message{
		{Role: "system", Content: systemPrompt},
		{Role: "user", Content: userPrompt},
	}
	if !stream {
		resp, err := GenerateMessages(messages, opt)
		return resp, err
	}

	var content strings.Builder
	if err := GenerateMessagesStream(messages, opt, func(delta StreamDelta) error {
		content.WriteString(delta.Content)
		emitDelta(opt, delta.ReasoningContent+delta.Content)
		return nil
	}); err != nil {
		return CleanJSON(content.String()), err
	}

	resp := content.String()
	return CleanJSON(resp), nil
}

func emitDelta(opt GenerateOptions, delta string) {
	if opt.OnDelta == nil || delta == "" {
		return
	}
	opt.OnDelta(delta)
}

func GenerateMessages(messages []Message, opt GenerateOptions) (string, error) {
	cfg, client := resolveClient(opt)
	if client == nil {
		return "", errors.New("LLM client is not initialized")
	}
	if opt.MaxTokens <= 0 {
		opt.MaxTokens = 1024
	}
	if opt.Temperature < 0 {
		opt.Temperature = 0.7 // 建议给个合理的默认值，而不是 0
	}
	if opt.TopP == 0 {
		opt.TopP = cfg.TopP
		// 如果全局配置也是 0 (未配置)，给一个通用默认值，或者保持 0 让 API 决定
		if opt.TopP == 0 {
			opt.TopP = 0.9 // 建议默认值
		}
	}
	if supportsTopK(cfg.BaseUrl) && opt.TopK == 0 {
		opt.TopK = cfg.TopK
		if opt.TopK == 0 {
			opt.TopK = 40 // 建议默认值
		}
	}
	modelName := opt.Model
	if modelName == "" {
		modelName = cfg.ModelDefault
	}
	if modelName == "" {
		return "", fmt.Errorf("calling LLM API requires a model name")
	}

	req := ChatRequest{
		Model:       modelName, // 使用处理后的 modelName
		Messages:    messages,
		Temperature: opt.Temperature,
		MaxTokens:   opt.MaxTokens,
		Stream:      false,
		Seed:        opt.Seed,
		TopP:        opt.TopP,
		TopK:        opt.TopK,
		Tools:       opt.Tools,
		ToolChoice:  opt.ToolChoice,
		Thinking:    resolveThinking(cfg, modelName, opt),
	}

	ctx, cancel := requestContext(opt)
	defer cancel()
	resp, err := client.R().
		SetContext(ctx).
		SetHeader("Accept", "application/json").
		SetBody(req).
		Post("/chat/completions")

	if err != nil {
		return "", err
	}
	if resp.IsError() {
		return "", fmt.Errorf("LLM API Error: %s", resp.String())
	}

	// 解析 OpenAI 格式的返回
	var result struct {
		Choices []struct {
			Message      Message `json:"message"`
			FinishReason string  `json:"finish_reason"`
		} `json:"choices"`
	}

	if err := decodeLLMJSONResponse(resp.Body(), resp.StatusCode(), resp.Header().Get("Content-Type"), resp.Request.URL, &result); err != nil {
		return "", err
	}

	if len(result.Choices) == 0 {
		return "", errors.New("empty response from LLM")
	}
	// 清洗 Markdown 代码块 (防止 JSON 解析失败)
	content := CleanJSON(result.Choices[0].Message.Content)
	if result.Choices[0].FinishReason == "length" {
		return content, &OutputLimitError{Partial: content}
	}
	return content, nil
}

func GenerateMessagesWithToolCalls(messages []Message, opt GenerateOptions) (Message, error) {
	cfg, client := resolveClient(opt)
	if client == nil {
		return Message{}, errors.New("LLM client is not initialized")
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
		return Message{}, fmt.Errorf("calling LLM API requires a model name")
	}

	req := ChatRequest{
		Model:       modelName,
		Messages:    messages,
		Temperature: opt.Temperature,
		MaxTokens:   opt.MaxTokens,
		Stream:      false,
		Seed:        opt.Seed,
		TopP:        opt.TopP,
		TopK:        opt.TopK,
		Tools:       opt.Tools,
		ToolChoice:  opt.ToolChoice,
		Thinking:    resolveThinking(cfg, modelName, opt),
	}

	ctx, cancel := requestContext(opt)
	defer cancel()
	resp, err := client.R().
		SetContext(ctx).
		SetHeader("Accept", "application/json").
		SetBody(req).
		Post("/chat/completions")
	if err != nil {
		return Message{}, err
	}
	if resp.IsError() {
		return Message{}, fmt.Errorf("LLM API Error: %s", resp.String())
	}

	var result struct {
		Choices []struct {
			Message      Message `json:"message"`
			FinishReason string  `json:"finish_reason"`
		} `json:"choices"`
	}
	if err := decodeLLMJSONResponse(resp.Body(), resp.StatusCode(), resp.Header().Get("Content-Type"), resp.Request.URL, &result); err != nil {
		return Message{}, err
	}
	if len(result.Choices) == 0 {
		return Message{}, errors.New("empty response from LLM")
	}
	if result.Choices[0].FinishReason == "length" {
		return Message{}, &OutputLimitError{ToolCall: true}
	}
	msg := result.Choices[0].Message
	msg.Content = CleanJSON(msg.Content)
	return msg, nil
}

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

func resolveClient(opt GenerateOptions) (config.LLM, *resty.Client) {
	if opt.LLM == nil {
		return global.GVA_CONFIG.LLM, global.GVA_LLM
	}
	cfg := llmclient.Normalize(*opt.LLM)
	return cfg, llmclient.ClientFor(cfg)
}

// resolveThinking 只向 DeepSeek V4 发送 thinking 扩展，避免污染其他 OpenAI
// 兼容服务。DeepSeek V4 默认开启思考模式；对于非流式工具决策，关闭思考可让
// 模型更快返回 tool_calls，也不需要在每一轮工具结果中保留隐藏推理。
func resolveThinking(cfg config.LLM, modelName string, opt GenerateOptions) *Thinking {
	if !isDeepSeekV4(cfg.BaseUrl, modelName) {
		return nil
	}
	if opt.Thinking != nil {
		mode := strings.ToLower(strings.TrimSpace(opt.Thinking.Type))
		if mode == "enabled" || mode == "disabled" {
			return &Thinking{Type: mode}
		}
	}
	if len(opt.Tools) > 0 {
		return &Thinking{Type: "disabled"}
	}
	return nil
}

func isDeepSeekV4(baseURL string, modelName string) bool {
	if !strings.Contains(strings.ToLower(baseURL), "api.deepseek.com") {
		return false
	}
	return strings.HasPrefix(strings.ToLower(strings.TrimSpace(modelName)), "deepseek-v4-")
}

// GetEmbedding 获取文本向量。ctx 用于传递超时、取消信号和请求生命周期。
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

// Rerank 使用交叉编码模型重新排序召回结果。
// ctx 用于传递超时、取消信号和请求生命周期。
func Rerank(ctx context.Context, query string, docs []string, topN int) (*RerankResponse, error) {
	if ctx == nil {
		ctx = context.Background()
	}
	results, err := inference.ActiveProvider().Rerank(ctx, query, docs, topN)
	if err != nil {
		return nil, err
	}
	resp := &RerankResponse{Results: make([]RerankResult, 0, len(results))}
	for _, result := range results {
		text := result.Text
		if text == "" && result.Index >= 0 && result.Index < len(docs) {
			text = docs[result.Index]
		}
		resp.Results = append(resp.Results, RerankResult{
			Index: result.Index,
			Score: result.Score,
			Text:  text,
		})
	}
	return resp, nil
}

// 辅助工具: 去除 ```json 包裹
func CleanJSON(input string) string {
	input = strings.TrimSpace(input)
	input = strings.TrimPrefix(input, "```json")
	input = strings.TrimPrefix(input, "```")
	input = strings.TrimSuffix(input, "```")
	return strings.TrimSpace(input)
}
