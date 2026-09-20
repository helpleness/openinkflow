package excelutil

import (
	"context"
	"fmt"
	"io"
	"strings"

	"github.com/xuri/excelize/v2"
)

// ImportWorkbook 读取 XLSX，使用 HeaderResolver 映射表头后，把每个工作表
// 的数据交给对应的业务 Import 实现。工作簿中的额外工作表会被安全忽略。
func ImportWorkbook(ctx context.Context, reader io.Reader, resolver HeaderResolver, imports ...Import) error {
	if len(imports) == 0 {
		return fmt.Errorf("excel imports are empty")
	}
	if resolver == nil {
		resolver = ExactHeaderResolver{}
	}
	workbook, err := Read(reader)
	if err != nil {
		return err
	}
	handlers := make(map[string]Import, len(imports))
	for index, importer := range imports {
		if importer == nil {
			return fmt.Errorf("excel import %d is nil", index+1)
		}
		name := strings.TrimSpace(importer.SheetName())
		if name == "" {
			return fmt.Errorf("excel import %d has an empty sheet name", index+1)
		}
		if _, exists := handlers[name]; exists {
			return fmt.Errorf("duplicate excel import sheet %q", name)
		}
		if err := validateHeaders(importer.SheetA1()); err != nil {
			return fmt.Errorf("validate expected headers for sheet %s: %w", name, err)
		}
		handlers[name] = importer
	}
	for _, sheet := range workbook.Sheets {
		importer, accepted := handlers[sheet.Name]
		if !accepted || len(sheet.Rows) == 0 {
			continue
		}
		indexes, err := resolver.ResolveHeaders(sheet.Name, importer.SheetA1(), sheet.Rows[0])
		if err != nil {
			return fmt.Errorf("resolve headers for sheet %s: %w", sheet.Name, err)
		}
		if err := validateHeaderIndexes(indexes, len(sheet.Rows[0])); err != nil {
			return fmt.Errorf("validate headers for sheet %s: %w", sheet.Name, err)
		}
		rows, err := mapRows(sheet.Rows[1:], indexes)
		if err != nil {
			return fmt.Errorf("map rows for sheet %s: %w", sheet.Name, err)
		}
		if err := importer.Handle(ctx, rows); err != nil {
			return fmt.Errorf("import sheet %s: %w", sheet.Name, err)
		}
	}
	for _, importer := range imports {
		if finalizer, ok := importer.(ImportFinalizer); ok {
			if err := finalizer.FinishImport(ctx); err != nil {
				return err
			}
		}
	}
	return nil
}

// ExactHeaderResolver 只接受标准表头。需要兼容别名或本地化时，
// 由业务包实现 HeaderResolver 并在 ImportWorkbook 中传入。
type ExactHeaderResolver struct{}

func (ExactHeaderResolver) ResolveHeaders(_ string, standard []string, actual []string) (map[string]int, error) {
	indexes := make(map[string]int, len(standard))
	available := make(map[string]int, len(actual))
	for index, header := range actual {
		if value := normalizeHeader(header); value != "" {
			available[value] = index
		}
	}
	for _, header := range standard {
		index, exists := available[normalizeHeader(header)]
		if !exists {
			return nil, fmt.Errorf("required column %q is missing", header)
		}
		indexes[header] = index
	}
	return indexes, nil
}

// Read 将全部工作表读为通用字符串行。
func Read(reader io.Reader) (Workbook, error) {
	file, err := excelize.OpenReader(reader)
	if err != nil {
		return Workbook{}, fmt.Errorf("open workbook: %w", err)
	}
	defer func() { _ = file.Close() }()
	workbook := Workbook{Sheets: make([]Sheet, 0, len(file.GetSheetList()))}
	for _, name := range file.GetSheetList() {
		rows, err := file.GetRows(name)
		if err != nil {
			return Workbook{}, fmt.Errorf("read sheet %s: %w", name, err)
		}
		workbook.Sheets = append(workbook.Sheets, Sheet{Name: name, Rows: rows})
	}
	return workbook, nil
}

func mapRows(rows [][]string, indexes map[string]int) ([]Row, error) {
	mapped := make([]Row, 0, len(rows))
	for _, source := range rows {
		row := make(Row, len(indexes))
		for key, index := range indexes {
			if index < len(source) {
				row[key] = source[index]
			} else {
				row[key] = ""
			}
		}
		mapped = append(mapped, row)
	}
	return mapped, nil
}

// validateHeaderIndexes 确保自定义表头解析器给出的映射完整且无歧义，
// 这样错误的解析实现不会静默地把数据导入到错误字段。
func validateHeaderIndexes(indexes map[string]int, headerCount int) error {
	if len(indexes) == 0 {
		return fmt.Errorf("header resolver returned no columns")
	}
	used := make(map[int]string, len(indexes))
	for key, index := range indexes {
		if strings.TrimSpace(key) == "" || index < 0 || index >= headerCount {
			return fmt.Errorf("invalid resolved column %q", key)
		}
		if previous, exists := used[index]; exists {
			return fmt.Errorf("resolved columns %q and %q use the same index %d", previous, key, index)
		}
		used[index] = key
	}
	return nil
}

func normalizeHeader(value string) string { return strings.ToLower(strings.TrimSpace(value)) }
