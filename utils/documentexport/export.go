// Package documentexport renders immutable Markdown writing versions into
// downloadable office formats. It deliberately has no database or HTTP
// dependency, making output deterministic and straightforward to test.
package documentexport

import (
	"archive/zip"
	"bytes"
	"context"
	"encoding/xml"
	"errors"
	"fmt"
	"os"
	"os/exec"
	"path/filepath"
	"strings"
	"time"
)

// ErrPDFExportBusy is returned instead of queueing a second LibreOffice
// conversion. Starting multiple soffice processes concurrently is expensive
// and can exhaust a small production host.
var ErrPDFExportBusy = errors.New("已有 PDF 导出任务正在进行，请稍后再试")

// pdfExportGate intentionally has process-wide scope: a single running
// InkFlow instance allows one PDF conversion at a time.
var pdfExportGate = make(chan struct{}, 1)

// DOCX renders Markdown text as a portable Office Open XML document.
func DOCX(title, markdown string) ([]byte, error) {
	title = strings.TrimSpace(title)
	if title == "" {
		title = "InkFlow 公文"
	}
	var output bytes.Buffer
	archive := zip.NewWriter(&output)
	files := map[string]string{
		"[Content_Types].xml":          contentTypesXML,
		"_rels/.rels":                  rootRelsXML,
		"word/_rels/document.xml.rels": documentRelsXML,
		"word/styles.xml":              stylesXML,
		"word/document.xml":            documentXML(title, markdown),
		"docProps/core.xml":            corePropertiesXML(title),
		"docProps/app.xml":             appPropertiesXML,
	}
	for name, content := range files {
		writer, err := archive.Create(name)
		if err != nil {
			return nil, fmt.Errorf("create DOCX part %s: %w", name, err)
		}
		if _, err := writer.Write([]byte(content)); err != nil {
			return nil, fmt.Errorf("write DOCX part %s: %w", name, err)
		}
	}
	if err := archive.Close(); err != nil {
		return nil, fmt.Errorf("finalize DOCX: %w", err)
	}
	return output.Bytes(), nil
}

// PDF converts the same deterministic DOCX through LibreOffice. The caller
// must configure a binary path/name; no shell is invoked and temporary source
// files are removed immediately after conversion.
func PDF(ctx context.Context, title, markdown, officeCommand string, timeout time.Duration) ([]byte, error) {
	officeCommand = strings.TrimSpace(officeCommand)
	if officeCommand == "" {
		return nil, fmt.Errorf("未配置正式 PDF 转换器；请安装 LibreOffice 并设置 export.office-command")
	}
	if timeout <= 0 {
		timeout = time.Minute
	}
	release, err := acquirePDFExport()
	if err != nil {
		return nil, err
	}
	defer release()

	docx, err := DOCX(title, markdown)
	if err != nil {
		return nil, err
	}
	directory, err := os.MkdirTemp("", "inkflow-export-*")
	if err != nil {
		return nil, fmt.Errorf("create export workspace: %w", err)
	}
	defer os.RemoveAll(directory)
	input := filepath.Join(directory, "document.docx")
	if err := os.WriteFile(input, docx, 0600); err != nil {
		return nil, fmt.Errorf("write DOCX for conversion: %w", err)
	}
	convertContext, cancel := context.WithTimeout(ctx, timeout)
	defer cancel()
	command := exec.CommandContext(convertContext, officeCommand, "--headless", "--convert-to", "pdf:writer_pdf_Export", "--outdir", directory, input)
	result, runErr := command.CombinedOutput()
	if convertContext.Err() != nil {
		return nil, fmt.Errorf("正式 PDF 转换超时")
	}
	if runErr != nil {
		message := strings.TrimSpace(string(result))
		if len(message) > 400 {
			message = message[:400]
		}
		if message == "" {
			message = runErr.Error()
		}
		return nil, fmt.Errorf("LibreOffice PDF 转换失败: %s", message)
	}
	pdf, err := os.ReadFile(filepath.Join(directory, "document.pdf"))
	if err != nil {
		return nil, fmt.Errorf("读取转换后的 PDF: %w", err)
	}
	if len(pdf) == 0 || !bytes.HasPrefix(pdf, []byte("%PDF-")) {
		return nil, fmt.Errorf("LibreOffice 未生成有效 PDF")
	}
	return pdf, nil
}

func acquirePDFExport() (func(), error) {
	select {
	case pdfExportGate <- struct{}{}:
		return func() { <-pdfExportGate }, nil
	default:
		return nil, ErrPDFExportBusy
	}
}

func documentXML(title, markdown string) string {
	paragraphs := markdownParagraphs(title, markdown)
	var builder strings.Builder
	builder.WriteString(`<?xml version="1.0" encoding="UTF-8" standalone="yes"?><w:document xmlns:w="http://schemas.openxmlformats.org/wordprocessingml/2006/main"><w:body>`)
	for _, paragraph := range paragraphs {
		builder.WriteString(`<w:p>`)
		if paragraph.style != "" {
			builder.WriteString(`<w:pPr><w:pStyle w:val="` + paragraph.style + `"/></w:pPr>`)
		}
		builder.WriteString(`<w:r><w:t xml:space="preserve">` + escapeXML(paragraph.text) + `</w:t></w:r></w:p>`)
	}
	builder.WriteString(`<w:sectPr><w:pgSz w:w="11906" w:h="16838"/><w:pgMar w:top="1440" w:right="1440" w:bottom="1440" w:left="1440" w:header="708" w:footer="708" w:gutter="0"/></w:sectPr></w:body></w:document>`)
	return builder.String()
}

type documentParagraph struct{ style, text string }

func markdownParagraphs(title, markdown string) []documentParagraph {
	result := []documentParagraph{{style: "Title", text: title}}
	for _, raw := range strings.Split(strings.ReplaceAll(markdown, "\r\n", "\n"), "\n") {
		line := strings.TrimSpace(raw)
		if line == "" {
			continue
		}
		style := ""
		switch {
		case strings.HasPrefix(line, "### "):
			style, line = "Heading3", strings.TrimSpace(strings.TrimPrefix(line, "### "))
		case strings.HasPrefix(line, "## "):
			style, line = "Heading2", strings.TrimSpace(strings.TrimPrefix(line, "## "))
		case strings.HasPrefix(line, "# "):
			style, line = "Heading1", strings.TrimSpace(strings.TrimPrefix(line, "# "))
		case strings.HasPrefix(line, "- ") || strings.HasPrefix(line, "* "):
			line = "• " + strings.TrimSpace(line[2:])
		case strings.HasPrefix(line, "| ") || strings.HasPrefix(line, "|"):
			line = strings.Trim(strings.ReplaceAll(line, "|", "  "), " ")
		}
		if strings.Trim(line, "-:| ") == "" {
			continue
		}
		result = append(result, documentParagraph{style: style, text: line})
	}
	return result
}

func escapeXML(value string) string {
	var output bytes.Buffer
	_ = xml.EscapeText(&output, []byte(value))
	return output.String()
}

const contentTypesXML = `<?xml version="1.0" encoding="UTF-8" standalone="yes"?><Types xmlns="http://schemas.openxmlformats.org/package/2006/content-types"><Default Extension="rels" ContentType="application/vnd.openxmlformats-package.relationships+xml"/><Default Extension="xml" ContentType="application/xml"/><Override PartName="/word/document.xml" ContentType="application/vnd.openxmlformats-officedocument.wordprocessingml.document.main+xml"/><Override PartName="/word/styles.xml" ContentType="application/vnd.openxmlformats-officedocument.wordprocessingml.styles+xml"/><Override PartName="/docProps/core.xml" ContentType="application/vnd.openxmlformats-package.core-properties+xml"/><Override PartName="/docProps/app.xml" ContentType="application/vnd.openxmlformats-officedocument.extended-properties+xml"/></Types>`
const rootRelsXML = `<?xml version="1.0" encoding="UTF-8" standalone="yes"?><Relationships xmlns="http://schemas.openxmlformats.org/package/2006/relationships"><Relationship Id="rId1" Type="http://schemas.openxmlformats.org/officeDocument/2006/relationships/officeDocument" Target="word/document.xml"/><Relationship Id="rId2" Type="http://schemas.openxmlformats.org/package/2006/relationships/metadata/core-properties" Target="docProps/core.xml"/><Relationship Id="rId3" Type="http://schemas.openxmlformats.org/officeDocument/2006/relationships/extended-properties" Target="docProps/app.xml"/></Relationships>`
const documentRelsXML = `<?xml version="1.0" encoding="UTF-8" standalone="yes"?><Relationships xmlns="http://schemas.openxmlformats.org/package/2006/relationships"><Relationship Id="rId1" Type="http://schemas.openxmlformats.org/officeDocument/2006/relationships/styles" Target="styles.xml"/></Relationships>`
const stylesXML = `<?xml version="1.0" encoding="UTF-8" standalone="yes"?><w:styles xmlns:w="http://schemas.openxmlformats.org/wordprocessingml/2006/main"><w:docDefaults/><w:style w:type="paragraph" w:default="1" w:styleId="Normal"><w:name w:val="Normal"/></w:style><w:style w:type="paragraph" w:styleId="Title"><w:name w:val="Title"/><w:rPr><w:b/><w:sz w:val="36"/><w:szCs w:val="36"/></w:rPr></w:style><w:style w:type="paragraph" w:styleId="Heading1"><w:name w:val="heading 1"/><w:rPr><w:b/><w:sz w:val="28"/><w:szCs w:val="28"/></w:rPr></w:style><w:style w:type="paragraph" w:styleId="Heading2"><w:name w:val="heading 2"/><w:rPr><w:b/><w:sz w:val="24"/><w:szCs w:val="24"/></w:rPr></w:style><w:style w:type="paragraph" w:styleId="Heading3"><w:name w:val="heading 3"/><w:rPr><w:b/><w:sz w:val="22"/><w:szCs w:val="22"/></w:rPr></w:style></w:styles>`
const appPropertiesXML = `<?xml version="1.0" encoding="UTF-8" standalone="yes"?><Properties xmlns="http://schemas.openxmlformats.org/officeDocument/2006/extended-properties"><Application>InkFlow</Application></Properties>`

func corePropertiesXML(title string) string {
	return `<?xml version="1.0" encoding="UTF-8" standalone="yes"?><cp:coreProperties xmlns:cp="http://schemas.openxmlformats.org/package/2006/metadata/core-properties" xmlns:dc="http://purl.org/dc/elements/1.1/"><dc:title>` + escapeXML(title) + `</dc:title><dc:creator>InkFlow</dc:creator></cp:coreProperties>`
}
