// Package documentparser extracts normalized text and embedded media from uploaded documents.
// It is deliberately storage-agnostic: callers decide whether extracted images go to local disk or OSS.
package documentparser

import (
	"context"
	"errors"
	"io"
)

var (
	ErrUnsupportedFormat = errors.New("unsupported document format")
	ErrLegacyOffice      = errors.New("legacy Office binary formats are not supported; please save as DOCX, XLSX, or PPTX")
)

type Image struct {
	Name string
	MIME string
	Data []byte
}

type Result struct {
	Text   string
	Images []Image
}

// Parser turns one document stream into normalized Markdown-compatible text and images.
type Parser interface {
	Parse(context.Context, string, io.Reader) (Result, error)
}
