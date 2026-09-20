package documentparser

import (
	"strings"
)

// NormalizeMarkdownForChunker is the single adaptation boundary before text reaches utils/chunker.
// It preserves headings and valid tables, normalizes line endings, and removes excessive blank lines.
func NormalizeMarkdownForChunker(text string) string {
	text = normalizeText(text)
	if text == "" {
		return ""
	}
	lines := strings.Split(text, "\n")
	output := make([]string, 0, len(lines))
	empty := 0
	for _, line := range lines {
		line = strings.TrimRight(line, " \t")
		if strings.TrimSpace(line) == "" {
			empty++
			if empty <= 1 {
				output = append(output, "")
			}
			continue
		}
		empty = 0
		output = append(output, line)
	}
	return strings.TrimSpace(strings.Join(output, "\n"))
}

func markdownCell(value string) string {
	value = strings.ReplaceAll(value, "\\", "\\\\")
	value = strings.ReplaceAll(value, "|", "\\|")
	value = strings.ReplaceAll(value, "\r\n", "<br>")
	value = strings.ReplaceAll(value, "\n", "<br>")
	return strings.TrimSpace(value)
}

func appendMarkdownTable(builder *strings.Builder, rows [][]string) {
	if len(rows) == 0 {
		return
	}
	width := 0
	for _, row := range rows {
		if len(row) > width {
			width = len(row)
		}
	}
	if width == 0 {
		return
	}
	writeRow := func(row []string) {
		cells := make([]string, width)
		for index := range cells {
			if index < len(row) {
				cells[index] = markdownCell(row[index])
			}
		}
		builder.WriteString("| " + strings.Join(cells, " | ") + " |\n")
	}
	writeRow(rows[0])
	separator := make([]string, width)
	for index := range separator {
		separator[index] = "---"
	}
	writeRow(separator)
	for _, row := range rows[1:] {
		writeRow(row)
	}
}
