package documentexport

import (
	"archive/zip"
	"bytes"
	"context"
	"errors"
	"strings"
	"testing"
	"time"
)

func TestDOCXContainsOfficePartsAndEscapedContent(t *testing.T) {
	data, err := DOCX("通知 <测试>", "# 标题\n\n正文 & 内容\n\n- 第一项")
	if err != nil {
		t.Fatal(err)
	}
	archive, err := zip.NewReader(bytes.NewReader(data), int64(len(data)))
	if err != nil {
		t.Fatal(err)
	}
	parts := map[string]bool{}
	for _, file := range archive.File {
		parts[file.Name] = true
	}
	for _, expected := range []string{"[Content_Types].xml", "word/document.xml", "word/styles.xml"} {
		if !parts[expected] {
			t.Fatalf("DOCX missing %s", expected)
		}
	}
	if !strings.Contains(documentXML("测试", "正文 & 内容"), "&amp;") {
		t.Fatal("document XML did not escape text")
	}
}

func TestPDFRequiresConfiguredOfficeConverter(t *testing.T) {
	if _, err := PDF(context.Background(), "测试", "正文", "", time.Second); err == nil {
		t.Fatal("PDF export unexpectedly worked without a configured converter")
	}
}

func TestPDFExportGateAllowsOnlyOneConversion(t *testing.T) {
	release, err := acquirePDFExport()
	if err != nil {
		t.Fatalf("acquire first PDF export slot: %v", err)
	}

	if _, err := acquirePDFExport(); !errors.Is(err, ErrPDFExportBusy) {
		t.Fatalf("second PDF export error = %v, want ErrPDFExportBusy", err)
	}

	release()
	release, err = acquirePDFExport()
	if err != nil {
		t.Fatalf("acquire released PDF export slot: %v", err)
	}
	release()
}
