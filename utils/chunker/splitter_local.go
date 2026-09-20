package chunker

import (
	"errors"
	"fmt"
	"strings"
	"unicode/utf8"

	"github.com/tmc/langchaingo/textsplitter"
)

type localSplitter struct{}

func (s *localSplitter) Split(fullText string) ([]MarkdownBlock, error) {
	text := normalizeMarkdownText(fullText)
	if strings.TrimSpace(text) == "" {
		return nil, errors.New("输入的文档为空")
	}

	blocks, err := splitTextWithLangchain(text)
	if err != nil {
		return nil, err
	}
	return mergeShortMarkdownBlocks(text, blocks), nil
}

func splitTextWithLangchain(text string) ([]MarkdownBlock, error) {
	sections := collectMarkdownSections(text)
	blocks := make([]MarkdownBlock, 0, len(sections))
	for _, section := range sections {
		blocks = append(blocks, splitSectionWithLangchain(section)...)
	}
	for i := range blocks {
		blocks[i].Index = i
	}
	return blocks, nil
}

func newLangchainMarkdownSplitter(headingHierarchy bool) *textsplitter.MarkdownTextSplitter {
	secondSplitter := newLangchainRecursiveSplitter(defaultMarkdownMaxTokens)

	return textsplitter.NewMarkdownTextSplitter(
		textsplitter.WithChunkSize(defaultMarkdownMaxTokens),
		textsplitter.WithChunkOverlap(defaultMarkdownOverlapTokens),
		textsplitter.WithHeadingHierarchy(headingHierarchy),
		textsplitter.WithLenFunc(estimateTextTokens),
		textsplitter.WithSecondSplitter(secondSplitter),
	)
}

func newLangchainRecursiveSplitter(chunkSize int) textsplitter.RecursiveCharacter {
	return textsplitter.NewRecursiveCharacter(
		textsplitter.WithChunkSize(chunkSize),
		textsplitter.WithChunkOverlap(defaultMarkdownOverlapTokens),
		textsplitter.WithLenFunc(estimateTextTokens),
		textsplitter.WithSeparators([]string{
			"\n\n",
			"\n",
			"。",
			"！",
			"？",
			"；",
			"，",
			"、",
			" ",
			"",
		}),
	)
}

func splitSectionWithLangchain(section markdownSection) []MarkdownBlock {
	var blocks []MarkdownBlock
	firstUnit := true
	for _, unit := range splitMarkdownUnits(section.lines) {
		if !hasNonEmptyLines(unit.lines) {
			continue
		}
		startOffset := unit.lines[0].start
		if firstUnit {
			startOffset = section.startOffset
			firstUnit = false
		}
		unitSection := markdownSection{
			path:        append([]string(nil), section.path...),
			level:       section.level,
			startOffset: startOffset,
			lines:       unit.lines,
		}
		if unit.isTable {
			blocks = append(blocks, splitTableSection(unitSection)...)
			continue
		}
		blocks = append(blocks, splitPlainSectionWithLangchain(unitSection)...)
	}
	return blocks
}

func splitPlainSectionWithLangchain(section markdownSection) []MarkdownBlock {
	rawContent := joinLines(section.lines)
	content := strings.TrimSpace(rawContent)
	if content == "" {
		return nil
	}
	contentStart := strings.Index(rawContent, content)
	if contentStart > 0 {
		section.lines = append([]lineSpan(nil), section.lines...)
		section.lines[0].start += contentStart
	}

	chunks, err := newLangchainMarkdownSplitter(false).SplitText(content)
	if err != nil {
		chunks = []string{content}
	}
	if estimateTextTokens(content) > defaultMarkdownMaxTokens && chunksTokenTotal(chunks) < estimateTextTokens(content) {
		chunks, err = newLangchainRecursiveSplitter(defaultMarkdownMaxTokens).SplitText(content)
		if err != nil {
			chunks = []string{content}
		}
	}
	return makeBlocksFromChunks(content, chunks, &section)
}

func chunksTokenTotal(chunks []string) int {
	total := 0
	for _, chunk := range chunks {
		total += estimateTextTokens(chunk)
	}
	return total
}

func splitTableSection(section markdownSection) []MarkdownBlock {
	pages := paginateMarkdownTable(section.lines, semanticSplitInputMaxTokens)
	blocks := make([]MarkdownBlock, 0, len(pages))
	for _, page := range pages {
		content := strings.TrimSpace(joinLines(page))
		if content == "" {
			continue
		}
		start := page[0].start
		end := page[len(page)-1].end
		blocks = append(blocks, makeSectionBlock(content, section, start, end))
	}
	return blocks
}

func makeSectionBlock(content string, section markdownSection, start int, end int) MarkdownBlock {
	path := append([]string(nil), section.path...)
	if len(path) == 0 {
		path = []string{"正文"}
	}
	title := path[len(path)-1]
	parent := title
	if len(path) > 1 {
		parent = strings.Join(path[:len(path)-1], " > ")
	}
	return MarkdownBlock{
		ParentTitle:   parent,
		Title:         title,
		Content:       content,
		HeadingPath:   path,
		Path:          strings.Join(path, " > "),
		Level:         section.level,
		SectionType:   DetectSectionType(path, content),
		StartOffset:   start,
		EndOffset:     end,
		TokenEstimate: estimateTextTokens(content),
	}
}

func hasNonEmptyLines(lines []lineSpan) bool {
	for _, line := range lines {
		if strings.TrimSpace(line.text) != "" {
			return true
		}
	}
	return false
}

func makeBlocksFromChunks(source string, chunks []string, section *markdownSection) []MarkdownBlock {
	var blocks []MarkdownBlock
	searchStart := 0
	for _, rawChunk := range chunks {
		content := strings.TrimSpace(rawChunk)
		if content == "" {
			continue
		}

		start, end := locateChunk(source, content, searchStart)
		// Adjacent chunks may overlap. Advancing to the previous end skips the
		// overlap and can make a later chunk resolve to an earlier duplicate.
		if start >= searchStart {
			searchStart = nextUTF8Boundary(source, start)
		}

		path, level := chunkHeadingPath(content)
		if section != nil {
			path = append([]string(nil), section.path...)
			level = section.level
			start += section.lines[0].start
			end += section.lines[0].start
			if len(blocks) == 0 {
				start = section.startOffset
			}
		}
		if len(path) == 0 {
			path = []string{"正文"}
		}

		title := path[len(path)-1]
		parent := title
		if len(path) > 1 {
			parent = strings.Join(path[:len(path)-1], " > ")
		} else if title == "正文" {
			parent = "未命名文档"
			title = semanticFallbackTitle(content)
			path = []string{"正文", title}
		}

		blocks = append(blocks, MarkdownBlock{
			ParentTitle:   parent,
			Title:         title,
			Content:       content,
			HeadingPath:   path,
			Path:          strings.Join(path, " > "),
			Level:         level,
			SectionType:   DetectSectionType(path, content),
			StartOffset:   start,
			EndOffset:     end,
			TokenEstimate: estimateTextTokens(content),
		})
	}

	for i := range blocks {
		blocks[i].Index = i
	}
	return blocks
}

func nextUTF8Boundary(text string, offset int) int {
	if offset < 0 {
		return 0
	}
	if offset >= len(text) {
		return len(text)
	}
	_, size := utf8.DecodeRuneInString(text[offset:])
	if size <= 0 {
		return offset
	}
	return offset + size
}

func mergeShortMarkdownBlocks(source string, blocks []MarkdownBlock) []MarkdownBlock {
	if len(blocks) < 2 {
		return blocks
	}

	merged := make([]MarkdownBlock, 0, len(blocks))
	current := blocks[0]
	for _, next := range blocks[1:] {
		start, end := current.StartOffset, next.EndOffset
		canSlice := validUTF8Range(source, start, end)
		content := current.Content + "\n\n" + next.Content
		if canSlice {
			sourceRange := source[start:end]
			if strings.Contains(sourceRange, current.Content) && strings.Contains(sourceRange, next.Content) {
				content = strings.TrimSpace(sourceRange)
			}
		}
		tokens := estimateTextTokens(content)

		sameBranch := sameMarkdownBranch(current.HeadingPath, next.HeadingPath) ||
			(current.Level == 0 && next.Level == 0 &&
				!isMajorMarkdownHeading(lastHeading(current.HeadingPath)) &&
				!isMajorMarkdownHeading(lastHeading(next.HeadingPath)))
		shortSide := current.TokenEstimate < defaultMarkdownTargetTokens || next.TokenEstimate < defaultMarkdownMinTokens
		if !sameBranch || !shortSide || tokens > defaultMarkdownMaxTokens {
			merged = append(merged, current)
			current = next
			continue
		}

		samePath := strings.Join(current.HeadingPath, "\x00") == strings.Join(next.HeadingPath, "\x00")
		currentIsAncestor := isHeadingAncestor(current.HeadingPath, next.HeadingPath)
		if !samePath && !currentIsAncestor {
			path := commonHeadingPrefix(current.HeadingPath, next.HeadingPath)
			if current.Title != next.Title && !strings.HasSuffix(current.Title, "等相邻小节") {
				current.Title += "等相邻小节"
			}
			if len(path) == 0 {
				path = []string{current.ParentTitle}
			}
			current.ParentTitle = strings.Join(path, " > ")
			current.HeadingPath = append(append([]string(nil), path...), current.Title)
			current.Path = strings.Join(current.HeadingPath, " > ")
		}
		current.Content = content
		current.EndOffset = end
		current.TokenEstimate = tokens
		current.SectionType = DetectSectionType(current.HeadingPath, content)
	}
	merged = append(merged, current)

	for i := range merged {
		merged[i].Index = i
	}
	return merged
}

func sameMarkdownBranch(left, right []string) bool {
	if strings.Join(left, "\x00") == strings.Join(right, "\x00") {
		return true
	}
	if isHeadingAncestor(left, right) {
		return true
	}
	if isHeadingAncestor(right, left) {
		return false
	}

	leftParent := left
	rightParent := right
	if len(leftParent) > 0 {
		leftParent = leftParent[:len(leftParent)-1]
	}
	if len(rightParent) > 0 {
		rightParent = rightParent[:len(rightParent)-1]
	}
	if strings.Join(leftParent, "\x00") != strings.Join(rightParent, "\x00") {
		return false
	}
	return !isMajorMarkdownHeading(lastHeading(left)) && !isMajorMarkdownHeading(lastHeading(right))
}

func isHeadingAncestor(parent, child []string) bool {
	if len(parent) == 0 || len(parent) >= len(child) {
		return false
	}
	for i := range parent {
		if parent[i] != child[i] {
			return false
		}
	}
	return true
}

func commonHeadingPrefix(left, right []string) []string {
	limit := len(left)
	if len(right) < limit {
		limit = len(right)
	}
	common := make([]string, 0, limit)
	for i := 0; i < limit && left[i] == right[i]; i++ {
		common = append(common, left[i])
	}
	return common
}

func lastHeading(path []string) string {
	if len(path) == 0 {
		return ""
	}
	return path[len(path)-1]
}

func isMajorMarkdownHeading(title string) bool {
	title = strings.TrimSpace(title)
	return strings.HasPrefix(title, "附录") || (strings.HasPrefix(title, "第") && strings.Contains(title, "章"))
}

func locateChunk(source string, chunk string, searchStart int) (int, int) {
	if searchStart < 0 || searchStart > len(source) {
		searchStart = 0
	}
	searchStart = utf8BoundaryAtOrBefore(source, searchStart)
	if at := strings.Index(source[searchStart:], chunk); at >= 0 {
		start := searchStart + at
		return start, start + len(chunk)
	}
	if at := strings.Index(source, chunk); at >= 0 {
		return at, at + len(chunk)
	}

	prefix, suffix := chunkBoundaryAnchors(chunk, 64)
	if prefix != "" {
		start := strings.Index(source[searchStart:], prefix)
		if start >= 0 {
			start += searchStart
			end := locateChunkEnd(source, suffix, start+len(prefix))
			return start, end
		}
		if start = strings.Index(source, prefix); start >= 0 {
			end := locateChunkEnd(source, suffix, start+len(prefix))
			return start, end
		}
	}
	end := utf8BoundaryAtOrBefore(source, searchStart+len(chunk))
	if end <= searchStart && searchStart < len(source) {
		_, size := utf8.DecodeRuneInString(source[searchStart:])
		end = searchStart + size
	}
	return searchStart, end
}

func chunkBoundaryAnchors(chunk string, maxRunes int) (string, string) {
	chunk = strings.TrimSpace(chunk)
	if chunk == "" {
		return "", ""
	}
	lines := strings.Split(chunk, "\n")
	first, last := "", ""
	for _, line := range lines {
		line = strings.TrimSpace(line)
		if line != "" {
			first = line
			break
		}
	}
	for i := len(lines) - 1; i >= 0; i-- {
		line := strings.TrimSpace(lines[i])
		if line != "" {
			last = line
			break
		}
	}
	return truncateAnchor(first, maxRunes, false), truncateAnchor(last, maxRunes, true)
}

func truncateAnchor(text string, maxRunes int, fromEnd bool) string {
	runes := []rune(text)
	if len(runes) <= maxRunes {
		return text
	}
	if fromEnd {
		return string(runes[len(runes)-maxRunes:])
	}
	return string(runes[:maxRunes])
}

func locateChunkEnd(source string, suffix string, afterPrefix int) int {
	if suffix != "" && afterPrefix <= len(source) {
		if at := strings.Index(source[afterPrefix:], suffix); at >= 0 {
			return afterPrefix + at + len(suffix)
		}
	}
	end := utf8BoundaryAtOrBefore(source, afterPrefix)
	if end <= 0 {
		return len(source)
	}
	return end
}

func validUTF8Range(source string, start int, end int) bool {
	if start < 0 || end <= start || end > len(source) {
		return false
	}
	startOK := start == 0 || utf8.RuneStart(source[start])
	endOK := end == len(source) || utf8.RuneStart(source[end])
	return startOK && endOK
}

func utf8BoundaryAtOrBefore(text string, offset int) int {
	if offset <= 0 {
		return 0
	}
	if offset >= len(text) {
		return len(text)
	}
	for offset > 0 && !utf8.RuneStart(text[offset]) {
		offset--
	}
	return offset
}

func chunkHeadingPath(chunk string) ([]string, int) {
	var path []string
	level := 0
	for _, line := range strings.Split(chunk, "\n") {
		matches := markdownHeadingRE.FindStringSubmatch(strings.TrimSpace(line))
		if len(matches) != 3 {
			continue
		}
		headingLevel := len(matches[1])
		title := strings.TrimSpace(matches[2])
		if headingLevel <= len(path) {
			path = path[:headingLevel-1]
		}
		for len(path) < headingLevel-1 {
			path = append(path, fmt.Sprintf("第%d级标题", len(path)+1))
		}
		path = append(path, title)
		level = headingLevel
	}
	return compactHeadings(path), level
}
