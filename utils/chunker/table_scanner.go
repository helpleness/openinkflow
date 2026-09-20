package chunker

import (
	"regexp"
	"strings"
)

var markdownTableSeparatorCellRE = regexp.MustCompile(`^:?-{3,}:?$`)

type markdownUnit struct {
	lines   []lineSpan
	isTable bool
}

// splitMarkdownUnits keeps complete Markdown tables out of generic line-based splitters.
func splitMarkdownUnits(lines []lineSpan) []markdownUnit {
	if len(lines) == 0 {
		return nil
	}

	units := make([]markdownUnit, 0, 4)
	plainStart := 0
	inFence := false
	fence := byte(0)

	for i := 0; i < len(lines); {
		if marker, ok := markdownFenceMarker(lines[i].text); ok {
			if !inFence {
				inFence = true
				fence = marker
			} else if marker == fence {
				inFence = false
				fence = 0
			}
			i++
			continue
		}

		if inFence || i+1 >= len(lines) || !isMarkdownTableHeader(lines[i].text, lines[i+1].text) {
			i++
			continue
		}

		if plainStart < i {
			units = append(units, markdownUnit{lines: lines[plainStart:i]})
		}

		end := i + 2
		for end < len(lines) && isMarkdownTableRow(lines[end].text) {
			end++
		}
		units = append(units, markdownUnit{lines: lines[i:end], isTable: true})
		i = end
		plainStart = end
	}

	if plainStart < len(lines) {
		units = append(units, markdownUnit{lines: lines[plainStart:]})
	}
	return units
}

func isMarkdownTableHeader(header string, separator string) bool {
	headerCells, headerOK := splitMarkdownTableCells(header)
	separatorCells, separatorOK := splitMarkdownTableCells(separator)
	if !headerOK || !separatorOK || len(headerCells) != len(separatorCells) {
		return false
	}
	for _, cell := range separatorCells {
		if !markdownTableSeparatorCellRE.MatchString(strings.TrimSpace(cell)) {
			return false
		}
	}
	return true
}

func isMarkdownTableRow(line string) bool {
	if strings.TrimSpace(line) == "" {
		return false
	}
	_, ok := splitMarkdownTableCells(line)
	return ok
}

func splitMarkdownTableCells(line string) ([]string, bool) {
	line = strings.TrimSpace(line)
	if line == "" || !containsUnescapedPipe(line) {
		return nil, false
	}

	if strings.HasPrefix(line, "|") {
		line = strings.TrimPrefix(line, "|")
	}
	if strings.HasSuffix(line, "|") && !strings.HasSuffix(line, `\|`) {
		line = strings.TrimSuffix(line, "|")
	}

	var cells []string
	var cell strings.Builder
	escaped := false
	for _, r := range line {
		switch {
		case escaped:
			cell.WriteRune(r)
			escaped = false
		case r == '\\':
			cell.WriteRune(r)
			escaped = true
		case r == '|':
			cells = append(cells, strings.TrimSpace(cell.String()))
			cell.Reset()
		default:
			cell.WriteRune(r)
		}
	}
	cells = append(cells, strings.TrimSpace(cell.String()))
	return cells, len(cells) > 0
}

func containsUnescapedPipe(line string) bool {
	escaped := false
	for _, r := range line {
		switch {
		case escaped:
			escaped = false
		case r == '\\':
			escaped = true
		case r == '|':
			return true
		}
	}
	return false
}

func markdownFenceMarker(line string) (byte, bool) {
	trimmed := strings.TrimSpace(line)
	if strings.HasPrefix(trimmed, "```") {
		return '`', true
	}
	if strings.HasPrefix(trimmed, "~~~") {
		return '~', true
	}
	return 0, false
}

func paginateMarkdownTable(lines []lineSpan, maxTokens int) [][]lineSpan {
	if len(lines) <= 2 || estimateLinesTokens(lines) <= maxTokens {
		return [][]lineSpan{lines}
	}

	header := append([]lineSpan(nil), lines[:2]...)
	pages := make([][]lineSpan, 0, 2)
	page := append([]lineSpan(nil), header...)
	for _, row := range lines[2:] {
		candidate := append(append([]lineSpan(nil), page...), row)
		if len(page) > len(header) && estimateLinesTokens(candidate) > maxTokens {
			pages = append(pages, page)
			page = append(append([]lineSpan(nil), header...), row)
			continue
		}
		page = candidate
	}
	if len(page) > len(header) {
		pages = append(pages, page)
	}
	return pages
}
