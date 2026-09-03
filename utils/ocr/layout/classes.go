package layout

// classNames 与 PP-DocLayout-S 导出的类别编号保持一致。不要按字母顺序重排；模型输出的是
// 数字类别编号，修改顺序会导致文字、表格等区域被错误解释。
var classNames = [...]string{
	"paragraph_title",
	"image",
	"text",
	"number",
	"abstract",
	"content",
	"figure_title",
	"formula",
	"table",
	"table_title",
	"reference",
	"doc_title",
	"footnote",
	"header",
	"algorithm",
	"footer",
	"seal",
	"chart_title",
	"chart",
	"formula_number",
	"header_image",
	"footer_image",
	"aside_text",
}

func className(classID int) string {
	if classID < 0 || classID >= len(classNames) {
		return "unknown"
	}
	return classNames[classID]
}
