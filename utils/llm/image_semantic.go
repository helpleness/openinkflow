package llm

import (
	"context"
	"encoding/json"
	"fmt"
	"strings"

	"InkFlow/config"
	domain "InkFlow/internal/ai/llm"
	"InkFlow/internal/ai/llm/providers"
)

// ImageSemantic is the optional external model's text transcription and
// knowledge-oriented description of one image.
type ImageSemantic struct {
	Text     string `json:"text"`
	Semantic string `json:"semantic"`
}

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

func (analyzer *ImageSemanticAnalyzer) AnalyzeImage(ctx context.Context, mime string, data []byte) (ImageSemantic, error) {
	if analyzer == nil || len(data) == 0 {
		return ImageSemantic{}, nil
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
			Content: "先转写图片中可辨认的文字、表头和关键单元格；再提取这张图表或表格的知识库语义，说明指标、单位、时间范围、趋势、结论和关键数值。不要臆测。只返回 JSON：{\"text\":\"...\",\"semantic\":\"...\"}。",
			Images:  []domain.ImageInput{{MIMEType: mime, Data: data}},
		}},
	})
	if err != nil {
		return ImageSemantic{}, fmt.Errorf("image semantic model: %w", err)
	}
	semantic, err := decodeImageSemantic(response.Message.Content)
	if err != nil {
		return ImageSemantic{}, err
	}
	semantic.Text = strings.TrimSpace(semantic.Text)
	semantic.Semantic = strings.TrimSpace(semantic.Semantic)
	return semantic, nil
}

// decodeImageSemantic accepts both the requested JSON and the common fenced
// variant returned by otherwise OpenAI-compatible models.
func decodeImageSemantic(content string) (ImageSemantic, error) {
	content = strings.TrimSpace(content)
	if strings.HasPrefix(content, "```") {
		if firstBreak := strings.IndexByte(content, '\n'); firstBreak >= 0 {
			content = strings.TrimSpace(content[firstBreak+1:])
		}
		content = strings.TrimSuffix(content, "```")
		content = strings.TrimSpace(content)
	}
	start, end := strings.IndexByte(content, '{'), strings.LastIndexByte(content, '}')
	if start < 0 || end < start {
		return ImageSemantic{}, fmt.Errorf("image semantic model returned non-JSON")
	}
	var semantic ImageSemantic
	if err := json.Unmarshal([]byte(content[start:end+1]), &semantic); err != nil {
		return ImageSemantic{}, fmt.Errorf("invalid image semantic model JSON: %w", err)
	}
	return semantic, nil
}
