package documentparser

import (
	"bytes"
	"context"
	"fmt"
	"io"
	"os"
	"os/exec"
	"path/filepath"
	"strings"
	"time"
)

const (
	pdfTextExtractionTimeout = 2 * time.Minute
	maxPDFExtractedTextSize  = 200 << 20
)

// extractPDFText is replaceable in package tests. Production always resolves
// the fixed pdftotext executable from PATH; no uploaded filename or request
// value is ever used to build the command.
var extractPDFText = extractPDFTextWithPoppler

// extractPDFTextWithPoppler invokes Poppler's mature pdftotext utility. It is
// the primary path for PDFs because it handles page trees, compressed object
// streams and Chinese ToUnicode CMaps that the compatibility reader cannot.
// The boolean reports whether the executable was available.
func extractPDFTextWithPoppler(ctx context.Context, data []byte) (string, bool, error) {
	binary, err := exec.LookPath("pdftotext")
	if err != nil {
		return "", false, nil
	}

	directory, err := os.MkdirTemp("", "inkflow-pdf-")
	if err != nil {
		return "", true, fmt.Errorf("创建 PDF 临时目录失败: %w", err)
	}
	defer os.RemoveAll(directory)

	inputPath := filepath.Join(directory, "source.pdf")
	outputPath := filepath.Join(directory, "source.txt")
	if err := os.WriteFile(inputPath, data, 0o600); err != nil {
		return "", true, fmt.Errorf("写入 PDF 临时文件失败: %w", err)
	}

	parseContext, cancel := context.WithTimeout(ctx, pdfTextExtractionTimeout)
	defer cancel()
	command := exec.CommandContext(parseContext, binary,
		"-enc", "UTF-8",
		"-eol", "unix",
		"-layout",
		"-nopgbrk",
		inputPath,
		outputPath,
	)
	var stderr bytes.Buffer
	command.Stderr = &stderr
	if err := command.Run(); err != nil {
		details := strings.TrimSpace(stderr.String())
		if details != "" {
			return "", true, fmt.Errorf("Poppler pdftotext 解析 PDF 失败: %s: %w", details, err)
		}
		return "", true, fmt.Errorf("Poppler pdftotext 解析 PDF 失败: %w", err)
	}
	if err := parseContext.Err(); err != nil {
		return "", true, fmt.Errorf("PDF 文本解析超时: %w", err)
	}

	output, err := os.Open(outputPath)
	if err != nil {
		return "", true, fmt.Errorf("读取 PDF 文本解析结果失败: %w", err)
	}
	defer output.Close()
	text, err := io.ReadAll(io.LimitReader(output, maxPDFExtractedTextSize+1))
	if err != nil {
		return "", true, fmt.Errorf("读取 PDF 文本解析结果失败: %w", err)
	}
	if len(text) > maxPDFExtractedTextSize {
		return "", true, fmt.Errorf("PDF 文本解析结果不能超过 %d MB", maxPDFExtractedTextSize>>20)
	}
	return string(text), true, nil
}
