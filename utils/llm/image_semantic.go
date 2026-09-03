package llm

import (
	"context"
	"encoding/base64"
	"encoding/json"
	"fmt"
	"strings"

	"InkFlow/config"
	"InkFlow/utils/llmclient"
)

// ImageSemantic is the optional external model's text transcription and
// knowledge-oriented description of one image.
type ImageSemantic struct {
	Text     string `json:"text"`
	Semantic string `json:"semantic"`
}

// ImageSemanticAnalyzer calls an OpenAI-compatible multimodal endpoint. It
// does not decide whether an image should be analyzed; that decision belongs
// to the client-side document-layout gate.
type ImageSemanticAnalyzer struct {
	config config.LLM
	model  string
}

func NewImageSemanticAnalyzer(cfg config.LLM) *ImageSemanticAnalyzer {
	model := strings.TrimSpace(cfg.ModelDefault)
	if model == "" || strings.TrimSpace(cfg.BaseUrl) == "" {
		return nil
	}
	return &ImageSemanticAnalyzer{config: cfg, model: model}
}

func (analyzer *ImageSemanticAnalyzer) AnalyzeImage(ctx context.Context, mime string, data []byte) (ImageSemantic, error) {
	if analyzer == nil || len(data) == 0 {
		return ImageSemantic{}, nil
	}
	mime = strings.TrimSpace(mime)
	if mime == "" {
		mime = "image/png"
	}
	body := map[string]any{
		"model":       analyzer.model,
		"temperature": 0,
		"max_tokens":  1200,
		"messages": []any{map[string]any{
			"role": "user",
			"content": []any{
				map[string]any{"type": "text", "text": "先转写图片中可辨认的文字、表头和关键单元格；再提取这张图表或表格的知识库语义，说明指标、单位、时间范围、趋势、结论和关键数值。不要臆测。只返回 JSON：{\"text\":\"...\",\"semantic\":\"...\"}。"},
				map[string]any{"type": "image_url", "image_url": map[string]any{"url": "data:" + mime + ";base64," + base64.StdEncoding.EncodeToString(data)}},
			},
		}},
	}
	response, err := llmclient.ClientFor(analyzer.config).R().SetContext(ctx).SetBody(body).Post("/chat/completions")
	if err != nil {
		return ImageSemantic{}, err
	}
	if response.IsError() {
		return ImageSemantic{}, fmt.Errorf("image semantic model error: %s", response.String())
	}
	var payload struct {
		Choices []struct {
			Message struct {
				Content string `json:"content"`
			} `json:"message"`
		} `json:"choices"`
	}
	if err := json.Unmarshal(response.Body(), &payload); err != nil {
		return ImageSemantic{}, fmt.Errorf("invalid image semantic model response: %w", err)
	}
	if len(payload.Choices) == 0 {
		return ImageSemantic{}, fmt.Errorf("image semantic model returned no choices")
	}
	semantic, err := decodeImageSemantic(payload.Choices[0].Message.Content)
	if err != nil {
		return ImageSemantic{}, err
	}
	semantic.Text = strings.TrimSpace(semantic.Text)
	semantic.Semantic = strings.TrimSpace(semantic.Semantic)
	return semantic, nil
}

// decodeImageSemantic accepts both the requested JSON and the common
// ```json fenced variant returned by otherwise OpenAI-compatible models.
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
