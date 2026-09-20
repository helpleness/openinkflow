// Package excelutil 提供可复用的 XLSX 导入、导出、表头解析和行映射能力。
// 本包不包含任何应用业务表、字段或数据模型。
package excelutil

import "context"

// Sheet 是一个通用工作表。Rows 的第一行由调用方定义为表头。
type Sheet struct {
	Name string
	Rows [][]string
}

// Workbook 是一个通用工作簿。
type Workbook struct {
	Sheets []Sheet
}

// Row 是按标准表头映射后的通用行数据。
type Row map[string]string

// Export 由业务包中的单个导出对象实现。每个实现只描述一个工作表，
// 文件创建、XLSX 写入和安全校验由本包统一完成。
type Export interface {
	SheetName() string
	SheetA1() []string
	Rows(context.Context) ([][]string, error)
}

// Import 由业务包中的单个导入对象实现。每个实现声明一个工作表的标准表头，
// 并只处理本工作表已完成表头映射后的行数据。
type Import interface {
	SheetName() string
	SheetA1() []string
	Handle(context.Context, []Row) error
}

// HeaderResolver 将实际表头解析为“标准表头 -> 列索引”。业务包可实现此接口，
// 以支持自定义表头、别名、本地化和任意列顺序。
type HeaderResolver interface {
	ResolveHeaders(sheetName string, standard []string, actual []string) (map[string]int, error)
}

// ImportFinalizer 可由 Import 实现，用于在全部工作表处理完成后做跨表校验。
type ImportFinalizer interface {
	FinishImport(context.Context) error
}
