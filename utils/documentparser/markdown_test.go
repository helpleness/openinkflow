package documentparser

import (
	"strings"
	"testing"
)

func TestAppendMarkdownTableProducesChunkerCompatibleHeader(t *testing.T) {
	var builder strings.Builder
	appendMarkdownTable(&builder, [][]string{{"名称", "数值"}, {"A|B", "10"}})
	want := "| 名称 | 数值 |\n| --- | --- |\n| A\\|B | 10 |\n"
	if builder.String() != want {
		t.Fatalf("table = %q, want %q", builder.String(), want)
	}
}

func TestNormalizeMarkdownForChunkerKeepsOneBlankLine(t *testing.T) {
	got := NormalizeMarkdownForChunker("# 标题\r\n\r\n\r\n正文  \r\n")
	if got != "# 标题\n\n正文" {
		t.Fatalf("normalized = %q", got)
	}
}
