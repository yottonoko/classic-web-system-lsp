package excel

import (
	"fmt"
	"path/filepath"
	"testing"
)

func BenchmarkWriteAnalysisWorkbookFileTwentyThousandRows(b *testing.B) {
	rows := make([][]Cell, 20_001)
	rows[0] = []Cell{"ID", "Name", "Kind", "File", "Line", "Column", "References", "Assignments", "Calls"}
	for index := 1; index < len(rows); index++ {
		rows[index] = []Cell{index, fmt.Sprintf("Declaration%d", index), "variable", "large-workspace.asp", index, 1, index % 7, index % 3, index % 2}
	}
	sheets := []AnalysisSheet{{Sheet: "Declarations", Data: rows, StickyRowsCount: 1, AutoFilterRef: "A1:I20001"}}
	path := filepath.Join(b.TempDir(), "large.xlsx")
	b.ReportAllocs()
	b.ResetTimer()
	for b.Loop() {
		if err := WriteAnalysisWorkbookFile(path, sheets); err != nil {
			b.Fatal(err)
		}
	}
}
