package officialdoc

import (
	"context"
	"fmt"
	"path/filepath"
	"strings"
	"time"
	"unicode"

	"InkFlow/global"
	"InkFlow/utils/documentexport"
)

// ExportVersion renders a version only after the same tenant, organization and
// membership checks used for viewing and editing that task.
func (service *WritingTaskService) ExportVersion(ctx context.Context, tenantID, taskID, versionID, userID uint, format string) ([]byte, string, string, error) {
	task, err := service.findTaskForMember(ctx, tenantID, taskID, userID)
	if err != nil {
		return nil, "", "", err
	}
	version, err := findTaskVersion(ctx, task, versionID)
	if err != nil {
		return nil, "", "", err
	}
	name := exportFilename(task.Title)
	switch strings.ToLower(strings.TrimSpace(format)) {
	case "md", "markdown":
		// Markdown is the exact immutable version body: unlike DOCX/PDF it is
		// not rendered or normalized again, preserving reviewable source text.
		return []byte(version.Content), name + ".md", "text/markdown; charset=utf-8", nil
	case "docx":
		data, exportErr := documentexport.DOCX(task.Title, version.Content)
		return data, name + ".docx", "application/vnd.openxmlformats-officedocument.wordprocessingml.document", exportErr
	case "pdf":
		timeout := time.Duration(global.GVA_CONFIG.Export.TimeoutSeconds) * time.Second
		data, exportErr := documentexport.PDF(ctx, task.Title, version.Content, global.GVA_CONFIG.Export.OfficeCommand, timeout)
		return data, name + ".pdf", "application/pdf", exportErr
	default:
		return nil, "", "", fmt.Errorf("仅支持导出 Markdown、DOCX 或 PDF")
	}
}

func exportFilename(title string) string {
	title = strings.TrimSpace(title)
	var builder strings.Builder
	for _, value := range title {
		if unicode.IsLetter(value) || unicode.IsDigit(value) || value == '-' || value == '_' {
			builder.WriteRune(value)
		} else if unicode.IsSpace(value) {
			builder.WriteByte('_')
		}
		if builder.Len() >= 100 {
			break
		}
	}
	name := strings.Trim(builder.String(), "._-")
	if name == "" {
		name = "InkFlow_公文"
	}
	return filepath.Base(name)
}
