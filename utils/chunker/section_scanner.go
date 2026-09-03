package chunker

import (
	"regexp"
	"strings"
)

var markdownHeadingRE = regexp.MustCompile(`^(#{1,6})\s+(.+?)\s*$`)

func collectMarkdownSections(text string) []markdownSection {
	var sections []markdownSection
	var current *markdownSection
	var headings [6]string

	offset := 0
	for _, rawLine := range strings.SplitAfter(text, "\n") {
		line := strings.TrimSuffix(rawLine, "\n")
		lineEnd := offset + len(line)
		trimmed := strings.TrimSpace(line)

		if matches := markdownHeadingRE.FindStringSubmatch(trimmed); len(matches) == 3 {
			if current != nil && hasSectionContent(*current) {
				sections = append(sections, *current)
			}

			level := len(matches[1])
			title := strings.TrimSpace(matches[2])
			headings[level-1] = title
			for i := level; i < len(headings); i++ {
				headings[i] = ""
			}

			current = &markdownSection{
				path:        compactHeadings(headings[:level]),
				level:       level,
				startOffset: offset,
			}
			offset += len(rawLine)
			continue
		}

		if current == nil {
			current = &markdownSection{
				path:        []string{"全文"},
				level:       0,
				startOffset: offset,
			}
		}

		current.lines = append(current.lines, lineSpan{
			text:  line,
			start: offset,
			end:   lineEnd,
		})
		offset += len(rawLine)
	}

	if current != nil && hasSectionContent(*current) {
		sections = append(sections, *current)
	}
	return sections
}

func joinLines(lines []lineSpan) string {
	var b strings.Builder
	for i, line := range lines {
		if i > 0 {
			b.WriteByte('\n')
		}
		b.WriteString(line.text)
	}
	return b.String()
}

func hasSectionContent(section markdownSection) bool {
	for _, line := range section.lines {
		if strings.TrimSpace(line.text) != "" {
			return true
		}
	}
	return false
}

func compactHeadings(headings []string) []string {
	out := make([]string, 0, len(headings))
	for _, heading := range headings {
		if strings.TrimSpace(heading) != "" {
			out = append(out, strings.TrimSpace(heading))
		}
	}
	return out
}
