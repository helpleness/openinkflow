package chunker

import (
	"InkFlow/config"
	"strings"
)

// DocumentSplitter 定义了文档切分器的标准接口
type DocumentSplitter interface {
	Split(text string) ([]MarkdownBlock, error)
}

// 供外部调用的工厂函数
func NewLocalSplitter() DocumentSplitter {
	return &localSplitter{}
}

func NewSemanticSplitter(cfg *config.LLM) DocumentSplitter {
	return &semanticSplitter{cfg: cfg}
}

func NewSemanticSplitterForUser(cfg *config.LLM, userID uint) DocumentSplitter {
	return &semanticSplitter{cfg: cfg, userID: userID}
}

func LocalSplitMarkdown(text string) []MarkdownBlock {
	blocks, err := NewLocalSplitter().Split(text)
	if err != nil {
		return nil
	}
	return blocks
}

func SplitMarkdownWithLLM(text string, cfg *config.LLM) ([]MarkdownBlock, error) {
	return NewSemanticSplitter(cfg).Split(text)
}

func SplitMarkdownWithLLMForUser(text string, cfg *config.LLM, userID uint) ([]MarkdownBlock, error) {
	return NewSemanticSplitterForUser(cfg, userID).Split(text)
}

func normalizeMarkdownText(fullText string) string {
	fullText = strings.ToValidUTF8(fullText, "\uFFFD")
	text := strings.ReplaceAll(fullText, "\r\n", "\n")
	return strings.ReplaceAll(text, "\r", "\n")
}
