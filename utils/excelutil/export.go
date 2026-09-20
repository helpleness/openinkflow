package excelutil

import (
	"bytes"
	"context"
	"fmt"
	"strings"

	"github.com/xuri/excelize/v2"
)

const maxCellRunes = 32767

// ExportWorkbook 将多个业务 Export 实现写成一个 XLSX 工作簿。
func ExportWorkbook(ctx context.Context, exports ...Export) ([]byte, error) {
	if len(exports) == 0 {
		return nil, fmt.Errorf("excel exports are empty")
	}
	sheets := make([]Sheet, 0, len(exports))
	for index, exporter := range exports {
		if exporter == nil {
			return nil, fmt.Errorf("excel export %d is nil", index+1)
		}
		headers := exporter.SheetA1()
		if err := validateHeaders(headers); err != nil {
			return nil, fmt.Errorf("validate headers for sheet %s: %w", exporter.SheetName(), err)
		}
		rows, err := exporter.Rows(ctx)
		if err != nil {
			return nil, fmt.Errorf("export rows for sheet %s: %w", exporter.SheetName(), err)
		}
		sheets = append(sheets, Sheet{Name: exporter.SheetName(), Rows: append([][]string{headers}, rows...)})
	}
	return Write(Workbook{Sheets: sheets})
}

// Write 将通用 Workbook 安全地序列化为 XLSX。
func Write(workbook Workbook) ([]byte, error) {
	if len(workbook.Sheets) == 0 {
		return nil, fmt.Errorf("workbook has no sheets")
	}
	file := excelize.NewFile()
	defer func() { _ = file.Close() }()
	seen := make(map[string]struct{}, len(workbook.Sheets))
	for index, sheet := range workbook.Sheets {
		name := strings.TrimSpace(sheet.Name)
		if name == "" {
			return nil, fmt.Errorf("sheet %d has an empty name", index+1)
		}
		if len([]rune(name)) > 31 {
			return nil, fmt.Errorf("sheet name %q exceeds Excel's 31-character limit", name)
		}
		if _, exists := seen[name]; exists {
			return nil, fmt.Errorf("duplicate sheet name %q", name)
		}
		seen[name] = struct{}{}
		if index == 0 {
			if err := file.SetSheetName(file.GetSheetName(0), name); err != nil {
				return nil, err
			}
		} else if _, err := file.NewSheet(name); err != nil {
			return nil, err
		}
		if err := writeSheet(file, Sheet{Name: name, Rows: sheet.Rows}); err != nil {
			return nil, err
		}
	}
	var output bytes.Buffer
	if err := file.Write(&output); err != nil {
		return nil, fmt.Errorf("write workbook: %w", err)
	}
	return output.Bytes(), nil
}

func writeSheet(file *excelize.File, sheet Sheet) error {
	columns := 0
	for rowIndex, row := range sheet.Rows {
		if len(row) > columns {
			columns = len(row)
		}
		for columnIndex, value := range row {
			if len([]rune(value)) > maxCellRunes {
				return fmt.Errorf("sheet %s row %d column %d exceeds Excel's 32767-character cell limit", sheet.Name, rowIndex+1, columnIndex+1)
			}
			cell, err := excelize.CoordinatesToCellName(columnIndex+1, rowIndex+1)
			if err != nil {
				return err
			}
			// SetCellStr 将所有输入按文本保存，避免以 '=' 开头的内容被当作公式。
			if err := file.SetCellStr(sheet.Name, cell, value); err != nil {
				return fmt.Errorf("write %s row %d: %w", sheet.Name, rowIndex+1, err)
			}
		}
	}
	if len(sheet.Rows) == 0 || columns == 0 {
		return nil
	}
	style, err := file.NewStyle(&excelize.Style{
		Font:      &excelize.Font{Bold: true, Color: "FFFFFF"},
		Fill:      excelize.Fill{Type: "pattern", Color: []string{"2F7968"}, Pattern: 1},
		Alignment: &excelize.Alignment{Vertical: "center"},
	})
	if err != nil {
		return err
	}
	lastColumn, err := excelize.ColumnNumberToName(columns)
	if err != nil {
		return err
	}
	if err := file.SetCellStyle(sheet.Name, "A1", lastColumn+"1", style); err != nil {
		return err
	}
	return file.SetPanes(sheet.Name, &excelize.Panes{Freeze: true, Split: false, YSplit: 1, TopLeftCell: "A2", ActivePane: "bottomLeft"})
}

func validateHeaders(headers []string) error {
	if len(headers) == 0 {
		return fmt.Errorf("sheet headers are empty")
	}
	seen := make(map[string]struct{}, len(headers))
	for _, header := range headers {
		name := strings.TrimSpace(header)
		if name == "" {
			return fmt.Errorf("sheet header is empty")
		}
		key := strings.ToLower(name)
		if _, exists := seen[key]; exists {
			return fmt.Errorf("duplicate sheet header %q", name)
		}
		seen[key] = struct{}{}
	}
	return nil
}
