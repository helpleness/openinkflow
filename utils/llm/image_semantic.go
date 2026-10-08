package llm

import (
	"context"
	"fmt"
	"strings"

	"InkFlow/config"
	domain "InkFlow/internal/ai/llm"
	"InkFlow/internal/ai/llm/providers"
)

// ImageSemanticAnalyzer is a small application adapter over the shared LLM
// provider contract. It has no HTTP or vendor DTO knowledge.
type ImageSemanticAnalyzer struct {
	config   config.LLM
	model    string
	provider domain.Provider
}

func NewImageSemanticAnalyzer(cfg config.LLM) *ImageSemanticAnalyzer {
	model := strings.TrimSpace(cfg.ModelDefault)
	if model == "" || strings.TrimSpace(cfg.BaseUrl) == "" {
		return nil
	}
	provider, err := providers.New(cfg)
	if err != nil {
		return nil
	}
	return &ImageSemanticAnalyzer{config: cfg, model: model, provider: provider}
}

// AnalyzeImage returns the model's complete, human-readable knowledge text.
// The result can be a transcription, a chart summary, or both. It deliberately
// has no structured-output contract because vision models do not reliably
// follow one across OpenAI-compatible providers.
func (analyzer *ImageSemanticAnalyzer) AnalyzeImage(ctx context.Context, mime string, data []byte) (string, error) {
	if analyzer == nil || len(data) == 0 {
		return "", nil
	}
	if ctx == nil {
		ctx = context.Background()
	}
	mime = strings.TrimSpace(mime)
	if mime == "" {
		mime = "image/png"
	}
	temperature, maxTokens := 0.0, 1200
	response, err := analyzer.provider.Chat(ctx, domain.ChatRequest{
		Model:       analyzer.model,
		Temperature: &temperature,
		MaxTokens:   &maxTokens,
		Messages: []domain.Message{{
			Role:    domain.RoleUser,
			Content: "请输出可直接用于知识库检索的纯文本，不要返回 JSON、XML、代码块或固定字段。图片中有可辨认文字、表格、表头或关键单元格时，优先按原有阅读顺序完整转写；图片主要是图表、流程图、照片或无法可靠逐字转写时，说明主题、指标、单位、时间范围、关键数值、趋势和结论。不要臆测；无法确认的信息明确写“无法确认”。",
			Images:  []domain.ImageInput{{MIMEType: mime, Data: data}},
		}},
	})
	if err != nil {
		return "", fmt.Errorf("image semantic model: %w", err)
	}
	return normalizeImageKnowledge(response.Message.Content)
}

func normalizeImageKnowledge(content string) (string, error) {
	content = strings.TrimSpace(content)
	if content == "" {
		return "", fmt.Errorf("image semantic model returned empty content")
	}
	return content, nil
}
