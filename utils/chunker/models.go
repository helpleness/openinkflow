package chunker

const (
	defaultMarkdownMinTokens     = 200
	defaultMarkdownTargetTokens  = 400
	defaultMarkdownMaxTokens     = 600
	defaultMarkdownOverlapTokens = 60
	semanticSplitInputMaxTokens  = 2200
	semanticSplitOutputMaxTokens = 8192
)

type MarkdownBlock struct {
	ParentTitle   string
	Title         string
	Content       string
	HeadingPath   []string
	Path          string
	Level         int
	SectionType   string
	StartOffset   int
	EndOffset     int
	TokenEstimate int
	Index         int
}

type markdownSection struct {
	path        []string
	level       int
	startOffset int
	lines       []lineSpan
}

type lineSpan struct {
	text  string
	start int
	end   int
}

type semanticBatch struct {
	text  string
	start int
	end   int
}

type semanticSplitItem struct {
	Title       string `json:"title"`
	ParentTitle string `json:"parent_title"`
	Content     string `json:"content"`
	SectionType string `json:"section_type"`
}
