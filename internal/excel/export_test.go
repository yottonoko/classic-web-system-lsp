package excel

import (
	"encoding/csv"
	"os"
	"path/filepath"
	"reflect"
	"testing"

	"github.com/xuri/excelize/v2"

	"github.com/yottonoko/classic-web-system-lsp/internal/graph"
)

func TestWriteCSVExportsNodesAndEdges(t *testing.T) {
	path := filepath.Join(t.TempDir(), "analysis.csv")
	payload := testAnalysisPayload()
	if err := WriteCSV(path, payload); err != nil {
		t.Fatalf("WriteCSV() error = %v", err)
	}
	file, err := os.Open(path)
	if err != nil {
		t.Fatalf("Open() error = %v", err)
	}
	defer file.Close()
	rows, err := csv.NewReader(file).ReadAll()
	if err != nil {
		t.Fatalf("ReadAll() error = %v", err)
	}
	want := [][]string{
		{"kind", "id", "label", "source", "target"},
		{"node", "file:///site/default.asp", "default.asp", "", ""},
		{"node", "file:///site/default.asp#symbol:main", "Main", "", ""},
		{"edge", "contains-main", "contains", "file:///site/default.asp", "file:///site/default.asp#symbol:main"},
		{"edge", "include-shared", "include", "file:///site/default.asp", "file:///site/shared.inc"},
	}
	if !reflect.DeepEqual(rows, want) {
		t.Fatalf("CSV rows = %#v, want %#v", rows, want)
	}
}

func TestWriteXLSXExportsWorkbookSheetsAndMetadata(t *testing.T) {
	path := filepath.Join(t.TempDir(), "analysis.xlsx")
	payload := testAnalysisPayload()
	if err := WriteXLSX(path, payload); err != nil {
		t.Fatalf("WriteXLSX() error = %v", err)
	}
	file, err := excelize.OpenFile(path)
	if err != nil {
		t.Fatalf("OpenFile() error = %v", err)
	}
	defer file.Close()
	if got := file.GetSheetList(); !reflect.DeepEqual(got, []string{"概要", "宣言", "チャート元データ"}) {
		t.Fatalf("sheet list = %#v, want summary/declarations/chart data", got)
	}
	if visible, err := file.GetSheetVisible("チャート元データ"); err != nil || visible {
		t.Fatalf("chart data visibility = %v, %v; want hidden", visible, err)
	}
	assertCellValue(t, file, "概要", "A1", "項目")
	assertCellValue(t, file, "概要", "B2", "document")
	assertCellValue(t, file, "概要", "B3", "file:///site/default.asp")
	assertCellValue(t, file, "概要", "B4", "2")
	assertCellValue(t, file, "概要", "B5", "2")
	assertCellValue(t, file, "宣言", "A1", "kind")
	assertCellValue(t, file, "宣言", "B2", "file:///site/default.asp")
	assertCellValue(t, file, "宣言", "C3", "Main")
	assertCellValue(t, file, "宣言", "D3", "vbDeclaration")
	assertCellValue(t, file, "チャート元データ", "A1", "kind")
	assertCellValue(t, file, "チャート元データ", "A2", "contains")
	assertCellValue(t, file, "チャート元データ", "C3", "file:///site/default.asp")
	assertCellValue(t, file, "チャート元データ", "D3", "file:///site/shared.inc")
	assertStyledWorksheet(t, file, "概要", "A1", "A2")
	assertStyledWorksheet(t, file, "宣言", "A1", "A2")
	assertColumnHasReadableWidth(t, file, "宣言", "B")
}

func testAnalysisPayload() graph.Payload {
	payload := graph.Payload{
		Scope: "document",
		URI:   "file:///site/default.asp",
		Nodes: []graph.Node{
			{ID: "file:///site/default.asp", Label: "default.asp", Kind: "file"},
			{ID: "file:///site/default.asp#symbol:main", Label: "Main", Kind: "vbDeclaration"},
		},
	}
	payload.AddEdge(graph.Edge{
		ID:     "contains-main",
		Source: "file:///site/default.asp",
		Target: "file:///site/default.asp#symbol:main",
		Kind:   "contains",
	})
	payload.AddEdge(graph.Edge{
		ID:     "include-shared",
		Source: "file:///site/default.asp",
		Target: "file:///site/shared.inc",
		Kind:   "include",
	})
	return payload
}

func assertCellValue(t *testing.T, file *excelize.File, sheet string, cell string, want string) {
	t.Helper()
	got, err := file.GetCellValue(sheet, cell)
	if err != nil {
		t.Fatalf("GetCellValue(%q, %q) error = %v", sheet, cell, err)
	}
	if got != want {
		t.Fatalf("cell %s!%s = %q, want %q", sheet, cell, got, want)
	}
}
