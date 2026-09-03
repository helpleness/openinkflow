package chunker

import (
	"os"
	"path/filepath"
	"strings"
	"testing"
	"unicode/utf8"
)

func TestLocalSplitMarkdownHandbookFixtures(t *testing.T) {
	fixtures := []string{
		"异常管理局人才筛选手册 第三版 · 附录一.md",
		"异常管理局人才筛选手册（第三版）· 第六章 数据回传与人才重评估.md",
		"劫难.md",
	}

	for _, name := range fixtures {
		t.Run(name, func(t *testing.T) {
			path := filepath.Join("..", "..", "docs", name)
			data, err := os.ReadFile(path)
			if err != nil {
				t.Fatalf("read fixture %s: %v", path, err)
			}

			// ImportTextDocument stores normalized UTF-8/LF content, so offsets are
			// intentionally verified against the same representation.
			text := normalizeMarkdownText(string(data))
			blocks := LocalSplitMarkdown(text)
			if len(blocks) == 0 {
				t.Fatal("split returned no blocks")
			}

			minTokens, maxTokens, totalTokens, shortBlocks := int(^uint(0)>>1), 0, 0, 0
			for i, block := range blocks {
				if !utf8.ValidString(block.Content) {
					t.Fatalf("block %d contains invalid UTF-8", i+1)
				}
				if strings.TrimSpace(block.Content) == "" {
					t.Fatalf("block %d is empty", i+1)
				}
				if block.StartOffset < 0 || block.EndOffset > len(text) || block.StartOffset >= block.EndOffset {
					t.Fatalf("block %d has invalid offsets [%d,%d), document length %d", i+1, block.StartOffset, block.EndOffset, len(text))
				}
				assertBlockAnchorsInRange(t, i+1, text[block.StartOffset:block.EndOffset], block.Content)
				if strings.Contains(block.Path, "未命名文档") {
					t.Fatalf("block %d unexpectedly uses fallback path: %s", i+1, block.Path)
				}

				tokens := block.TokenEstimate
				if tokens < minTokens {
					minTokens = tokens
				}
				if tokens > maxTokens {
					maxTokens = tokens
				}
				if tokens < defaultMarkdownMinTokens {
					shortBlocks++
				}
				totalTokens += tokens
				t.Logf("block %02d: tokens=%d offsets=[%d,%d) path=%s", i+1, tokens, block.StartOffset, block.EndOffset, block.Path)
			}

			assertFixtureTablesStayWhole(t, text, blocks)
			if name == "劫难.md" {
				if len(blocks) > 20 {
					t.Fatalf("劫难 fixture was split too finely: got %d blocks, want at most 20", len(blocks))
				}
				if average := totalTokens / len(blocks); average < 300 {
					t.Fatalf("劫难 fixture chunks are too small on average: got %d tokens, want at least 300", average)
				}
			}
			t.Logf("summary: blocks=%d min=%d max=%d avg=%d under_%d=%d", len(blocks), minTokens, maxTokens, totalTokens/len(blocks), defaultMarkdownMinTokens, shortBlocks)
		})
	}
}

func assertBlockAnchorsInRange(t *testing.T, index int, sourceRange string, content string) {
	t.Helper()

	lines := strings.Split(content, "\n")
	anchors := make([]string, 0, len(lines))
	for _, line := range lines {
		line = strings.TrimSpace(line)
		if line == "" || line == "---" || isMarkdownTableDelimiter(line) {
			continue
		}
		anchors = append(anchors, line)
	}
	if len(anchors) == 0 {
		return
	}
	for _, anchor := range []string{anchors[0], anchors[len(anchors)-1]} {
		if !strings.Contains(sourceRange, anchor) {
			t.Fatalf("block %d boundary anchor is not recoverable from its source range; anchor=%q range=%q content=%q", index, fixturePreview(anchor), fixturePreview(sourceRange), fixturePreview(content))
		}
	}
}

func fixturePreview(text string) string {
	text = strings.ReplaceAll(strings.TrimSpace(text), "\n", " ")
	runes := []rune(text)
	if len(runes) > 120 {
		return string(runes[:120])
	}
	return text
}

func isMarkdownTableDelimiter(line string) bool {
	line = strings.Trim(line, "| ")
	if line == "" {
		return false
	}
	for _, cell := range strings.Split(line, "|") {
		cell = strings.Trim(strings.TrimSpace(cell), ":")
		if len(cell) < 3 || strings.Trim(cell, "-") != "" {
			return false
		}
	}
	return true
}

func assertFixtureTablesStayWhole(t *testing.T, text string, blocks []MarkdownBlock) {
	t.Helper()

	tableCount := 0
	for _, section := range collectMarkdownSections(text) {
		for _, unit := range splitMarkdownUnits(section.lines) {
			if !unit.isTable {
				continue
			}
			tableCount++
			table := strings.TrimSpace(joinLines(unit.lines))
			if estimateTextTokens(table) > semanticSplitInputMaxTokens {
				continue
			}

			matches := 0
			for _, block := range blocks {
				if strings.Contains(block.Content, table) {
					matches++
				}
			}
			if matches != 1 {
				t.Fatalf("table %d should be contained by exactly one block, got %d matches", tableCount, matches)
			}
		}
	}

	t.Logf("verified %d Markdown tables remain whole", tableCount)
}
