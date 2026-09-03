package chunker

import (
	"fmt"
	"os"
	"path/filepath"
	"strings"
	"testing"
	"unicode/utf8"
)

func TestLocalSplitMarkdownHeadingPath(t *testing.T) {
	rootText := strings.Repeat("root intro ", 220)
	ruleText := strings.Repeat("sealed city rule ", 150)
	teaText := strings.Repeat("tea house commission ", 150)
	input := strings.Join([]string{
		"# Root",
		"",
		rootText,
		"",
		"## Rules",
		"",
		ruleText,
		"",
		"### Tea House",
		"",
		teaText,
	}, "\n")

	blocks := LocalSplitMarkdown(input)
	if len(blocks) != 3 {
		t.Fatalf("expected 3 blocks, got %d: %#v", len(blocks), blocks)
	}
	if blocks[1].Path != "Root > Rules" {
		t.Fatalf("unexpected second block path: %q", blocks[1].Path)
	}
	if blocks[2].Path != "Root > Rules > Tea House" {
		t.Fatalf("unexpected third block path: %q", blocks[2].Path)
	}
	if blocks[1].SectionType != "rule" {
		t.Fatalf("expected rule section type, got %q", blocks[1].SectionType)
	}
}

func TestLocalSplitMarkdownMergesAdjacentShortSections(t *testing.T) {
	input := strings.Join([]string{
		"# Handbook",
		"",
		"## 第一条 准入条件",
		"申请人需要完成登记。",
		"",
		"## 第二条 培养周期",
		"培养周期原则上不超过三年。",
		"",
		"## 第三条 实战考核",
		"实战考核按季度进行。",
	}, "\n")

	blocks := LocalSplitMarkdown(input)
	if len(blocks) != 1 {
		t.Fatalf("expected adjacent short sections to merge, got %d: %#v", len(blocks), blocks)
	}
	for _, heading := range []string{"第一条 准入条件", "第二条 培养周期", "第三条 实战考核"} {
		if !strings.Contains(blocks[0].Content, heading) {
			t.Fatalf("merged content lost heading %q: %q", heading, blocks[0].Content)
		}
	}
}

func TestMergeShortBlocksInsideMajorChapter(t *testing.T) {
	source := strings.Repeat("甲", 90) + "\n\n" + strings.Repeat("乙", 110) + "\n\n" + strings.Repeat("丙", 130)
	blocks := []MarkdownBlock{
		{Title: "第六章 数据回传", ParentTitle: "手册", HeadingPath: []string{"手册", "第六章 数据回传"}, Content: strings.Repeat("甲", 90), StartOffset: 0, EndOffset: 90 * len("甲"), TokenEstimate: 90},
		{Title: "闭环枢纽", ParentTitle: "手册 > 第六章 数据回传", HeadingPath: []string{"手册", "第六章 数据回传", "闭环枢纽"}, Content: strings.Repeat("乙", 110), StartOffset: 90*len("甲") + 2, EndOffset: 90*len("甲") + 2 + 110*len("乙"), TokenEstimate: 110},
		{Title: "动态校准", ParentTitle: "手册 > 第六章 数据回传", HeadingPath: []string{"手册", "第六章 数据回传", "动态校准"}, Content: strings.Repeat("丙", 130), StartOffset: 90*len("甲") + 2 + 110*len("乙") + 2, EndOffset: len(source), TokenEstimate: 130},
	}

	merged := mergeShortMarkdownBlocks(source, blocks)
	if len(merged) != 1 {
		t.Fatalf("expected short blocks inside one major chapter to merge, got %d", len(merged))
	}
	if merged[0].TokenEstimate != 330 {
		t.Fatalf("unexpected merged token estimate: %d", merged[0].TokenEstimate)
	}
}

func TestSemanticItemsInheritMarkdownSectionPath(t *testing.T) {
	section := markdownSection{path: []string{"人才筛选手册", "第六章 数据回传"}, level: 2}
	batch := semanticBatch{text: "第一段。\n\n第二段。", start: 10, end: 38}
	items := []semanticSplitItem{
		{Title: "闭环枢纽", ParentTitle: "未命名文档", Content: "第一段。", SectionType: "lore"},
		{Title: "动态校准", ParentTitle: "未命名文档", Content: "第二段。", SectionType: "rule"},
	}

	blocks := semanticItemsToBlocks(items, batch, section)
	if len(blocks) != 2 {
		t.Fatalf("expected 2 semantic blocks, got %d", len(blocks))
	}
	for _, block := range blocks {
		if !strings.HasPrefix(block.Path, "人才筛选手册 > 第六章 数据回传 > ") {
			t.Fatalf("semantic block lost section path: %q", block.Path)
		}
		if strings.Contains(block.Path, "未命名文档") {
			t.Fatalf("semantic block kept fallback parent: %q", block.Path)
		}
	}
}

func TestMergeShortSemanticBlocksWithGeneratedParents(t *testing.T) {
	source := strings.Repeat("甲", 120) + "\n\n" + strings.Repeat("乙", 130) + "\n\n" + strings.Repeat("丙", 140)
	blocks := []MarkdownBlock{
		{Title: "协议抬头", ParentTitle: "协议", HeadingPath: []string{"协议", "协议抬头"}, Content: strings.Repeat("甲", 120), StartOffset: 0, EndOffset: 120 * len("甲"), TokenEstimate: 120},
		{Title: "准入条件", ParentTitle: "培养计划", HeadingPath: []string{"培养计划", "准入条件"}, Content: strings.Repeat("乙", 130), StartOffset: 120*len("甲") + 2, EndOffset: 120*len("甲") + 2 + 130*len("乙"), TokenEstimate: 130},
		{Title: "签署要求", ParentTitle: "签约", HeadingPath: []string{"签约", "签署要求"}, Content: strings.Repeat("丙", 140), StartOffset: 120*len("甲") + 2 + 130*len("乙") + 2, EndOffset: len(source), TokenEstimate: 140},
	}

	merged := mergeShortMarkdownBlocks(source, blocks)
	if len(merged) != 1 {
		t.Fatalf("expected adjacent short semantic blocks to merge despite generated parents, got %d", len(merged))
	}
	if merged[0].TokenEstimate != 390 {
		t.Fatalf("unexpected merged token estimate: %d", merged[0].TokenEstimate)
	}
}

func TestLocalSplitMarkdownKeepsMajorChapterBoundaries(t *testing.T) {
	input := strings.Join([]string{
		"# Handbook",
		"",
		"## 第一章 准入",
		"本章介绍准入规则。",
		"",
		"## 第二章 培养",
		"本章介绍培养规则。",
	}, "\n")

	blocks := LocalSplitMarkdown(input)
	if len(blocks) != 2 {
		t.Fatalf("expected major chapters to remain separate, got %d: %#v", len(blocks), blocks)
	}
}

func TestLocalSplitMarkdownWithNovelFixture(t *testing.T) {
	path := filepath.Join("..", "..", "测试数据.md")
	content, err := os.ReadFile(path)
	if err != nil {
		t.Skipf("fixture %s is not available: %v", path, err)
	}

	blocks := LocalSplitMarkdown(string(content))
	if len(blocks) < 2 {
		t.Fatalf("expected fixture to retain multiple meaningful blocks, got %d", len(blocks))
	}

	for _, block := range blocks {
		if strings.TrimSpace(block.Content) == "" {
			t.Fatalf("block %d has empty content", block.Index)
		}
		if block.Path == "" {
			t.Fatalf("block %d has empty path", block.Index)
		}
		if block.TokenEstimate <= 0 {
			t.Fatalf("block %d has invalid token estimate %d", block.Index, block.TokenEstimate)
		}
		if block.StartOffset < 0 || block.EndOffset <= block.StartOffset {
			t.Fatalf("block %d has invalid offsets: %d-%d", block.Index, block.StartOffset, block.EndOffset)
		}
	}

	t.Logf("fixture split into %d blocks; first=%q; last=%q", len(blocks), blocks[0].Path, blocks[len(blocks)-1].Path)
}

func TestLocalSplitMarkdownLimitsChunkSize(t *testing.T) {
	line := strings.Repeat("设定内容，", 80)
	input := "# Root\n\n## Lore\n\n" + strings.Join([]string{
		line,
		line,
		line,
		line,
		line,
		line,
	}, "\n")

	blocks := LocalSplitMarkdown(input)
	if len(blocks) < 2 {
		t.Fatalf("expected oversized section to split, got %d block(s)", len(blocks))
	}

	for _, block := range blocks {
		if block.TokenEstimate > defaultMarkdownMaxTokens {
			t.Fatalf("block %d token estimate = %d, want <= %d", block.Index, block.TokenEstimate, defaultMarkdownMaxTokens)
		}
	}
}

func TestLocalSplitMarkdownSplitsLongSingleLine(t *testing.T) {
	input := "# Root\n\n" + strings.Repeat("这是一条很长的设定句子，用来测试自动切分能力。", 120)

	blocks := LocalSplitMarkdown(input)
	if len(blocks) < 2 {
		t.Fatalf("expected long single line to split, got %d block(s)", len(blocks))
	}

	for _, block := range blocks {
		if block.TokenEstimate > defaultMarkdownMaxTokens {
			t.Fatalf("block %d token estimate = %d, want <= %d", block.Index, block.TokenEstimate, defaultMarkdownMaxTokens)
		}
		if strings.TrimSpace(block.Content) == "" {
			t.Fatalf("block %d has empty content", block.Index)
		}
	}
}

func TestLocateChunkFallbackKeepsUTF8Boundaries(t *testing.T) {
	source := "甲 A。中文段落"
	start, end := locateChunk(source, "不存在的混合长度片段", 4)
	if !validUTF8Range(source, start, end) {
		t.Fatalf("fallback range %d-%d is not aligned to UTF-8 boundaries", start, end)
	}
	if !utf8.ValidString(source[start:end]) {
		t.Fatalf("fallback range produced invalid UTF-8: %q", source[start:end])
	}
}

func TestLocalSplitMarkdownAlwaysReturnsValidUTF8(t *testing.T) {
	input := "# UTF-8\n\n" + strings.Repeat("甲 A。中文说明、规则；继续。\u3000", 240)
	blocks := LocalSplitMarkdown(input)
	if len(blocks) < 2 {
		t.Fatalf("expected long UTF-8 document to split, got %d block(s)", len(blocks))
	}
	for _, block := range blocks {
		for name, value := range map[string]string{
			"content": block.Content,
			"title":   block.Title,
			"path":    block.Path,
		} {
			if !utf8.ValidString(value) {
				t.Fatalf("block %d has invalid UTF-8 in %s", block.Index, name)
			}
		}
	}
}

func TestLocalSplitMarkdownKeepsTableTogether(t *testing.T) {
	rows := []string{
		"# Handbook",
		"",
		"## Grades",
		"",
		"| Grade | Name | Code |",
		"| :--- | --- | ---: |",
	}
	for i := 0; i < 80; i++ {
		rows = append(rows, "| level | a deliberately descriptive table cell | L9 |")
	}

	blocks := LocalSplitMarkdown(strings.Join(rows, "\n"))
	if len(blocks) != 1 {
		t.Fatalf("expected table to remain one block, got %d", len(blocks))
	}
	if got := strings.Count(blocks[0].Content, "| level |"); got != 80 {
		t.Fatalf("expected all table rows in one block, got %d", got)
	}
	if blocks[0].Path != "Handbook > Grades" {
		t.Fatalf("unexpected table path: %q", blocks[0].Path)
	}
}

func TestSemanticSplitMarkdownKeepsTableAwayFromLLM(t *testing.T) {
	input := strings.Join([]string{
		"# Handbook",
		"",
		"| Grade | Name | Code |",
		"| --- | --- | --- |",
		"| Nine | Mortal | L9 |",
		"| Eight | Awareness | L8 |",
	}, "\n")

	blocks, err := NewSemanticSplitter(nil).Split(input)
	if err != nil {
		t.Fatalf("table-only semantic split should not call LLM: %v", err)
	}
	if len(blocks) != 1 {
		t.Fatalf("expected one table block, got %d", len(blocks))
	}
	if !strings.Contains(blocks[0].Content, "| Eight | Awareness | L8 |") {
		t.Fatalf("table tail was lost: %q", blocks[0].Content)
	}
}

func TestLocalSplitMarkdownPaginatesOnlyOversizedTables(t *testing.T) {
	rows := []string{
		"# Handbook",
		"",
		"| Index | Description |",
		"| --- | --- |",
	}
	for i := 0; i < 500; i++ {
		rows = append(rows, fmt.Sprintf("| row-%03d | %s |", i, strings.Repeat("内容", 8)))
	}

	blocks := LocalSplitMarkdown(strings.Join(rows, "\n"))
	if len(blocks) < 2 {
		t.Fatalf("expected oversized table to paginate, got %d block(s)", len(blocks))
	}
	joined := strings.Builder{}
	for i, block := range blocks {
		if !strings.HasPrefix(block.Content, "| Index | Description |\n| --- | --- |") {
			t.Fatalf("page %d does not repeat the table header: %q", i, block.Content)
		}
		joined.WriteString(block.Content)
		joined.WriteByte('\n')
	}
	for i := 0; i < 500; i++ {
		row := fmt.Sprintf("row-%03d", i)
		if got := strings.Count(joined.String(), row); got != 1 {
			t.Fatalf("expected %s exactly once, got %d", row, got)
		}
	}
}

func TestLocalSplitMarkdownDoesNotTreatFencedPipesAsTable(t *testing.T) {
	input := strings.Join([]string{
		"# Example",
		"",
		"```text",
		"| looks | like | a table |",
		"| --- | --- | --- |",
		"| but | is | code |",
		"```",
	}, "\n")

	units := splitMarkdownUnits(collectMarkdownSections(input)[0].lines)
	for _, unit := range units {
		if unit.isTable {
			t.Fatal("fenced code was detected as a Markdown table")
		}
	}
}
