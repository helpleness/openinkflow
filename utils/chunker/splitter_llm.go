package chunker

import (
	"InkFlow/config"
	llmutil "InkFlow/utils/llm"
	"InkFlow/utils/promptstore"
	"encoding/json"
	"fmt"
	"strings"
)

type semanticSplitter struct {
	cfg    *config.LLM
	userID uint
}

func (s *semanticSplitter) Split(fullText string) ([]MarkdownBlock, error) {
	text := normalizeMarkdownText(fullText)
	if strings.TrimSpace(text) == "" {
		return nil, nil
	}

	var blocks []MarkdownBlock
	var firstErr error
	for _, section := range collectMarkdownSections(text) {
		for _, unit := range splitMarkdownUnits(section.lines) {
			if !hasNonEmptyLines(unit.lines) {
				continue
			}
			unitSection := markdownSection{
				path:        append([]string(nil), section.path...),
				level:       section.level,
				startOffset: unit.lines[0].start,
				lines:       unit.lines,
			}

			if unit.isTable {
				blocks = append(blocks, splitTableSection(unitSection)...)
				continue
			}
			if !needsSemanticSplitSection(unitSection) {
				blocks = append(blocks, splitPlainSectionWithLangchain(unitSection)...)
				continue
			}

			var unitBlocks []MarkdownBlock
			unitFailed := false
			for _, batch := range makeSemanticBatches(unit.lines, semanticSplitInputMaxTokens) {
				batchBlocks, err := semanticSplitBatch(batch, unitSection, s.cfg, s.userID)
				if err != nil {
					if firstErr == nil {
						firstErr = err
					}
					unitFailed = true
					break
				}
				unitBlocks = append(unitBlocks, batchBlocks...)
			}
			if unitFailed {
				blocks = append(blocks, splitPlainSectionWithLangchain(unitSection)...)
			} else {
				blocks = append(blocks, unitBlocks...)
			}
		}
	}

	return mergeShortMarkdownBlocks(text, blocks), firstErr
}

func needsSemanticSplitSection(section markdownSection) bool {
	if len(section.path) == 0 || (len(section.path) == 1 && section.path[0] == "全文") {
		return true
	}
	return estimateLinesTokens(section.lines) > defaultMarkdownMaxTokens
}

func makeSemanticBatches(lines []lineSpan, maxTokens int) []semanticBatch {
	if len(lines) == 0 {
		return nil
	}

	content := strings.TrimSpace(joinLines(lines))
	if content == "" {
		return nil
	}

	splitter := newLangchainRecursiveSplitter(maxTokens)
	chunks, err := splitter.SplitText(content)
	if err != nil {
		return []semanticBatch{{
			text:  content,
			start: lines[0].start,
			end:   lines[len(lines)-1].end,
		}}
	}

	var batches []semanticBatch
	searchStart := 0
	for _, chunk := range chunks {
		chunk = strings.TrimSpace(chunk)
		if chunk == "" {
			continue
		}
		start, end := locateChunk(content, chunk, searchStart)
		searchStart = end
		batches = append(batches, semanticBatch{
			text:  chunk,
			start: lines[0].start + start,
			end:   lines[0].start + end,
		})
	}
	return batches
}

func semanticSplitBatch(batch semanticBatch, section markdownSection, cfg *config.LLM, userID uint) ([]MarkdownBlock, error) {
	systemPrompt := promptstore.Get(promptstore.WithUserID(nil, userID), promptstore.SemanticMarkdownSplit, `你是小说/世界观资料的语义切分器。只按语义边界切分，不改写原文。必须只返回 JSON 数组，不要输出 Markdown 或解释。`)
	userPrompt := fmt.Sprintf(`请把下面文档切分为适合后续抽取设定的语义片段。
要求：
1. 每个片段必须保留原文顺序，content 必须逐字摘自原文，不能总结、改写、补写。
2. 尽量按完整人物、地点、规则、物品、事件或连续说明切分，避免在一句话、一个因果链或一个设定说明中间断开。
3. 优先保留完整小节、完整设定和连续论述，不要把短段或表格说明拆成碎片。单个片段建议 300-500 字；只有主题明显变化或接近长度上限时才切分。
4. 没有标题时，请为 title 生成简短语义标题。能判断上级主题时填 parent_title，否则填“未命名文档”。
5. section_type 只能是 rule、character、location、entity、lore 之一。

返回 JSON 结构：
[
  {"parent_title":"未命名文档","title":"片段标题","section_type":"lore","content":"原文片段"}
]

待切分原文：
%s`, batch.text)

	resp, err := llmutil.GenerateWithOptions(systemPrompt, userPrompt, llmutil.GenerateOptions{
		Temperature: 0.1,
		MaxTokens:   semanticSplitOutputMaxTokens,
		LLM:         cfg,
	}, false)
	if err != nil {
		return nil, err
	}

	var items []semanticSplitItem
	if err := unmarshalModelJSON(resp, &items); err != nil {
		return nil, err
	}
	blocks := semanticItemsToBlocks(items, batch, section)
	if len(blocks) == 0 {
		return nil, fmt.Errorf("semantic splitter returned no usable chunks")
	}
	return blocks, nil
}

func semanticItemsToBlocks(items []semanticSplitItem, batch semanticBatch, section markdownSection) []MarkdownBlock {
	var blocks []MarkdownBlock
	searchStart := 0
	for _, item := range items {
		content := strings.TrimSpace(item.Content)
		if content == "" {
			continue
		}
		relativeStart := strings.Index(batch.text[searchStart:], content)
		if relativeStart >= 0 {
			relativeStart += searchStart
		} else {
			relativeStart = strings.Index(batch.text, content)
		}
		start := batch.start
		end := batch.end
		if relativeStart >= 0 {
			start = batch.start + relativeStart
			end = start + len(content)
			searchStart = relativeStart + len(content)
		}

		title := strings.TrimSpace(item.Title)
		if title == "" {
			title = semanticFallbackTitle(content)
		}
		path := append([]string(nil), section.path...)
		if len(path) == 0 || (len(path) == 1 && path[0] == "全文") {
			parent := strings.TrimSpace(item.ParentTitle)
			if parent == "" {
				parent = "未命名文档"
			}
			path = []string{parent}
		}
		if len(path) == 0 || path[len(path)-1] != title {
			path = append(path, title)
		}
		parent := path[0]
		if len(path) > 1 {
			parent = strings.Join(path[:len(path)-1], " > ")
		}
		blocks = append(blocks, MarkdownBlock{
			ParentTitle:   parent,
			Title:         title,
			Content:       content,
			HeadingPath:   path,
			Path:          strings.Join(path, " > "),
			Level:         section.level,
			SectionType:   normalizeSemanticSectionType(item.SectionType, path, content),
			StartOffset:   start,
			EndOffset:     end,
			TokenEstimate: estimateTextTokens(content),
		})
	}
	return blocks
}

func semanticFallbackTitle(content string) string {
	runes := []rune(strings.TrimSpace(content))
	if len(runes) > 15 {
		return string(runes[:15]) + "..."
	}
	return string(runes)
}

func normalizeSemanticSectionType(sectionType string, path []string, content string) string {
	switch strings.ToLower(strings.TrimSpace(sectionType)) {
	case "rule", "character", "location", "entity", "lore":
		return strings.ToLower(strings.TrimSpace(sectionType))
	default:
		return DetectSectionType(path, content)
	}
}

func unmarshalModelJSON(resp string, out any) error {
	clean := llmutil.RepairJSON(llmutil.ExtractJSON(resp))
	if clean == "" {
		return fmt.Errorf("模型返回了空 JSON")
	}
	return json.Unmarshal([]byte(clean), out)
}
