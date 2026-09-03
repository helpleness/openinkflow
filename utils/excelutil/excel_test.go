package excelutil

import (
	"bytes"
	"context"
	"fmt"
	"testing"
)

type testExport struct{}

func (testExport) SheetName() string { return "Data" }
func (testExport) SheetA1() []string { return []string{"value"} }
func (testExport) Rows(context.Context) ([][]string, error) {
	return [][]string{{"portable"}}, nil
}

type testHeaderResolver struct{}

func (testHeaderResolver) ResolveHeaders(_ string, standard []string, actual []string) (map[string]int, error) {
	if len(standard) != 1 || standard[0] != "value" || len(actual) != 1 || actual[0] != "value" {
		return nil, fmt.Errorf("unexpected headers")
	}
	return map[string]int{"value": 0}, nil
}

type testImport struct{ value string }

func (*testImport) SheetName() string { return "Data" }
func (*testImport) SheetA1() []string { return []string{"value"} }
func (importer *testImport) Handle(_ context.Context, rows []Row) error {
	if len(rows) > 0 {
		importer.value = rows[0]["value"]
	}
	return nil
}

func TestExportImportDelegatesToInterfaces(t *testing.T) {
	data, err := ExportWorkbook(context.Background(), testExport{})
	if err != nil {
		t.Fatalf("export: %v", err)
	}
	importer := &testImport{}
	if err := ImportWorkbook(context.Background(), bytes.NewReader(data), testHeaderResolver{}, importer); err != nil {
		t.Fatalf("import: %v", err)
	}
	if importer.value != "portable" {
		t.Fatalf("unexpected imported value %q", importer.value)
	}
}

func TestValidateHeaderIndexesRejectsAmbiguousMappings(t *testing.T) {
	tests := []struct {
		name    string
		indexes map[string]int
		wantErr bool
	}{
		{name: "valid", indexes: map[string]int{"name": 0, "content": 1}},
		{name: "empty key", indexes: map[string]int{"": 0}, wantErr: true},
		{name: "negative index", indexes: map[string]int{"name": -1}, wantErr: true},
		{name: "out of range", indexes: map[string]int{"name": 2}, wantErr: true},
		{name: "duplicate index", indexes: map[string]int{"name": 0, "content": 0}, wantErr: true},
	}
	for _, test := range tests {
		t.Run(test.name, func(t *testing.T) {
			err := validateHeaderIndexes(test.indexes, 2)
			if (err != nil) != test.wantErr {
				t.Fatalf("validateHeaderIndexes() error = %v, wantErr %t", err, test.wantErr)
			}
		})
	}
}
