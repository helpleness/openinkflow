package documentparser

import (
	"bytes"
	"context"
	"fmt"
	"io"
	"path/filepath"
	"strings"
)

type parser struct{}

// New returns the built-in parser for Markdown, text, PDF, DOCX, XLSX and PPTX.
func New() Parser { return parser{} }

// IsSupportedFilename is the upload whitelist. Parsing still validates actual
// content after the object has been stored, so a forged extension cannot become
// indexed text.
func IsSupportedFilename(filename string) bool {
	switch strings.ToLower(strings.TrimSpace(filepath.Ext(filename))) {
	case ".md", ".markdown", ".txt", ".csv", ".docx", ".xlsx", ".pptx", ".pdf":
		return true
	default:
		return false
	}
}

func (parser) Parse(ctx context.Context, filename string, input io.Reader) (Result, error) {
	if err := ctx.Err(); err != nil {
		return Result{}, err
	}
	data, err := io.ReadAll(io.LimitReader(input, 200<<20))
	if err != nil {
		return Result{}, fmt.Errorf("read document: %w", err)
	}
	extension := strings.ToLower(strings.TrimSpace(filepath.Ext(filename)))
	switch extension {
	case ".md", ".markdown", ".txt", ".csv":
		return Result{Text: NormalizeMarkdownForChunker(string(data))}, nil
	case ".docx":
		return parseDOCX(data)
	case ".xlsx":
		return parseXLSX(data)
	case ".pptx":
		return parsePPTX(data)
	case ".pdf":
		return parsePDF(data)
	case ".doc", ".xls", ".ppt":
		return Result{}, ErrLegacyOffice
	default:
		return Result{}, fmt.Errorf("%w: %s", ErrUnsupportedFormat, extension)
	}
}

func normalizeText(text string) string {
	text = strings.ToValidUTF8(text, "�")
	// PostgreSQL 的 text/varchar 不接受 NUL（0x00）。PDF 的文本流可能通过
	// 八进制转义或不完整的 UTF-16 字节序列带入它，因此在所有格式共用的
	// 规范化边界统一删除，避免污染后续标题、切片内容和向量索引。
	text = strings.ReplaceAll(text, "\x00", "")
	text = strings.ReplaceAll(text, "\r\n", "\n")
	text = strings.ReplaceAll(text, "\r", "\n")
	lines := strings.Split(text, "\n")
	for index, line := range lines {
		lines[index] = strings.TrimRight(line, " \t")
	}
	return strings.TrimSpace(strings.Join(lines, "\n"))
}

func newReader(data []byte) *bytes.Reader { return bytes.NewReader(data) }
