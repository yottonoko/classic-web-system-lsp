package excel

import (
	"archive/zip"
	"context"
	"errors"
	"io"
	"os"
	"path/filepath"
	"strings"
	"testing"

	"github.com/xuri/excelize/v2"
)

func TestWriteAnalysisWorkbookFileWritesSheetMetadata(t *testing.T) {
	path := filepath.Join(t.TempDir(), "analysis-sheets.xlsx")
	sheets := CreateAnalysisSheets(testSheetPayload(), LocaleJapanese, AnalysisSheetsOptions{
		TargetURI: "file:///workspace/main.asp",
	})
	if err := WriteAnalysisWorkbookFile(path, sheets); err != nil {
		t.Fatalf("WriteAnalysisWorkbookFile() error = %v", err)
	}
	content, err := os.ReadFile(path)
	if err != nil {
		t.Fatalf("ReadFile() error = %v", err)
	}
	if len(content) < 2 || string(content[:2]) != "PK" {
		t.Fatalf("workbook header = %q, want XLSX zip", content[:min(len(content), 2)])
	}
	file, err := excelize.OpenFile(path)
	if err != nil {
		t.Fatalf("OpenFile() error = %v", err)
	}
	defer file.Close()
	for _, sheet := range []string{"概要", "インクルードツリー図", "分析サマリ", "チャート元データ", "宣言"} {
		index, err := file.GetSheetIndex(sheet)
		if err != nil || index < 0 {
			t.Fatalf("workbook missing sheet %q: index=%d err=%v", sheet, index, err)
		}
	}
	if visible, err := file.GetSheetVisible("チャート元データ"); err != nil || visible {
		t.Fatalf("chart data visibility = %v, %v; want hidden", visible, err)
	}
	if got, err := file.GetCellValue("概要", "A1"); err != nil || got != "名前" {
		t.Fatalf("概要!A1 = %q, %v; want 名前", got, err)
	}
	if got, err := file.GetCellValue("宣言", "B2"); err != nil || got != "TargetValue" {
		t.Fatalf("宣言!B2 = %q, %v; want TargetValue", got, err)
	}
	assertStyledWorksheet(t, file, "概要", "A1", "A2")
	assertColumnHasReadableWidth(t, file, "概要", "A")
	if panes, err := file.GetPanes("概要"); err != nil || !panes.Freeze || panes.YSplit != 1 {
		t.Fatalf("summary panes = %#v, %v; want frozen first row", panes, err)
	}
	if panes, err := file.GetPanes("分析サマリ"); err != nil || panes.Freeze {
		t.Fatalf("analysis summary panes = %#v, %v; want unfrozen", panes, err)
	}
	if panes, err := file.GetPanes("チャート元データ"); err != nil || !panes.Freeze || panes.YSplit != 2 {
		t.Fatalf("chart data panes = %#v, %v; want first two rows frozen", panes, err)
	}
	if !xlsxContains(t, path, `autoFilter ref="$A$1:$Q$5"`) &&
		!xlsxContains(t, path, `autoFilter ref="A1:Q5"`) {
		t.Fatal("workbook XML missing declaration autoFilter ref A1:Q5")
	}
	if !xlsxContains(t, path, `rgb="FF1F4E79"`) ||
		!xlsxContains(t, path, `rgb="FFFFFFFF"`) ||
		!xlsxContains(t, path, `rgb="FFF9FAFB"`) ||
		!xlsxContains(t, path, `rgb="FF374151"`) ||
		!xlsxContains(t, path, `rgb="FFEAF2F8"`) ||
		!xlsxContains(t, path, `rgb="FF17365D"`) {
		t.Fatal("workbook XML missing explicit presentation colors")
	}
	if !xlsxContains(t, path, `mergeCell ref="A1:D1"`) || !xlsxContains(t, path, `formatCode="0.0%"`) {
		t.Fatal("workbook XML missing TypeScript-compatible section merge or percentage format")
	}
	if !xlsxHasFile(t, path, "xl/media/image") {
		t.Fatal("workbook missing analysis summary chart images")
	}
	if !xlsxContains(t, path, `prst="straightConnector1"`) || !xlsxContains(t, path, "main.asp") {
		t.Fatal("workbook missing native include-tree nodes or connectors")
	}
}

func TestWriteAnalysisWorkbookFileRejectsArchivesBeyondDirectWriteLimit(t *testing.T) {
	path := filepath.Join(t.TempDir(), "too-large.xlsx")
	sheets := []AnalysisSheet{{Sheet: "Data", Data: [][]Cell{{"value"}, {strings.Repeat("not-compressible-enough-", 32)}}}}
	err := WriteAnalysisWorkbookFileWithOptions(path, sheets, AnalysisWorkbookWriteOptions{zipLimit: 128})
	if !errors.Is(err, ErrWorkbookTooLarge) {
		t.Fatalf("WriteAnalysisWorkbookFileWithOptions() error = %v, want ErrWorkbookTooLarge", err)
	}
	if _, statErr := os.Stat(path); !os.IsNotExist(statErr) {
		t.Fatalf("partial workbook remains after size failure: %v", statErr)
	}
}

func TestWriteAnalysisWorkbookFileLateCancellationPreservesExistingTarget(t *testing.T) {
	directory := t.TempDir()
	path := filepath.Join(directory, "late-cancel.xlsx")
	previous := []byte("previous workbook")
	if err := os.WriteFile(path, previous, 0o600); err != nil {
		t.Fatal(err)
	}
	cancelled := false
	err := WriteAnalysisWorkbookFileWithOptions(path, []AnalysisSheet{{Sheet: "Data", Data: [][]Cell{{"new"}}}}, AnalysisWorkbookWriteOptions{
		Progress: func(event AnalysisProgressEvent) {
			if event.Label == "excel.fileCommit" && event.Current == 0 {
				cancelled = true
			}
		},
		Cancelled: func() bool { return cancelled },
	})
	if !errors.Is(err, context.Canceled) {
		t.Fatalf("WriteAnalysisWorkbookFileWithOptions() error = %v, want context.Canceled", err)
	}
	assertFileBytes(t, path, previous)
	assertNoWorkbookTempFiles(t, path)
}

func TestWriteAnalysisWorkbookFileWriteFailurePreservesExistingTarget(t *testing.T) {
	directory := t.TempDir()
	path := filepath.Join(directory, "write-failure.xlsx")
	previous := []byte("previous workbook")
	if err := os.WriteFile(path, previous, 0o600); err != nil {
		t.Fatal(err)
	}
	err := WriteAnalysisWorkbookFileWithOptions(path, []AnalysisSheet{{Sheet: "Data", Data: [][]Cell{{"value"}, {strings.Repeat("not-compressible-enough-", 32)}}}}, AnalysisWorkbookWriteOptions{zipLimit: 128})
	if !errors.Is(err, ErrWorkbookTooLarge) {
		t.Fatalf("WriteAnalysisWorkbookFileWithOptions() error = %v, want ErrWorkbookTooLarge", err)
	}
	assertFileBytes(t, path, previous)
	assertNoWorkbookTempFiles(t, path)
}

func TestWriteAnalysisWorkbookFileSuccessfulReplacementCleansTemp(t *testing.T) {
	directory := t.TempDir()
	path := filepath.Join(directory, "replacement.xlsx")
	if err := os.WriteFile(path, []byte("previous workbook"), 0o600); err != nil {
		t.Fatal(err)
	}
	if err := WriteAnalysisWorkbookFile(path, []AnalysisSheet{{Sheet: "Data", Data: [][]Cell{{"new"}}}}); err != nil {
		t.Fatalf("WriteAnalysisWorkbookFile() error = %v", err)
	}
	content, err := os.ReadFile(path)
	if err != nil {
		t.Fatal(err)
	}
	if len(content) < 2 || string(content[:2]) != "PK" {
		t.Fatalf("replacement header = %q, want XLSX zip", content[:min(len(content), 2)])
	}
	assertNoWorkbookTempFiles(t, path)
}

func TestWriteAnalysisWorkbookFileCommitFailurePreservesExistingTarget(t *testing.T) {
	directory := t.TempDir()
	path := filepath.Join(directory, "commit-failure.xlsx")
	previous := []byte("previous workbook")
	if err := os.WriteFile(path, previous, 0o600); err != nil {
		t.Fatal(err)
	}
	commitErr := errors.New("commit rejected")
	err := WriteAnalysisWorkbookFileWithOptions(path, []AnalysisSheet{{Sheet: "Data", Data: [][]Cell{{"new"}}}}, AnalysisWorkbookWriteOptions{
		Commit: func(string) error { return commitErr },
	})
	if !errors.Is(err, commitErr) {
		t.Fatalf("WriteAnalysisWorkbookFileWithOptions() error = %v, want %v", err, commitErr)
	}
	assertFileBytes(t, path, previous)
	assertNoWorkbookTempFiles(t, path)
}

func TestWriteAnalysisWorkbookFileStreamsRowsBeforeAttachingImages(t *testing.T) {
	path := filepath.Join(t.TempDir(), "streamed-image-sheet.xlsx")
	rows := make([][]Cell, 4097)
	rows[0] = []Cell{"ID", "Value"}
	for index := 1; index < len(rows); index++ {
		rows[index] = []Cell{index, strings.Repeat("row-value-", 8)}
	}
	images := analysisSummaryImages(analysisContext{}, LocaleEnglish, 2)
	sheets := []AnalysisSheet{{Sheet: "Summary", Data: rows, Images: images, StickyRowsCount: 1, AutoFilterRef: "A1:B4097"}}
	if err := WriteAnalysisWorkbookFile(path, sheets); err != nil {
		t.Fatalf("WriteAnalysisWorkbookFile() error = %v", err)
	}
	file, err := excelize.OpenFile(path)
	if err != nil {
		t.Fatalf("OpenFile() error = %v", err)
	}
	defer file.Close()
	if got, err := file.GetCellValue("Summary", "B4097"); err != nil || got != strings.Repeat("row-value-", 8) {
		t.Fatalf("Summary!B4097 = %q, %v", got, err)
	}
	if !xlsxHasFile(t, path, "xl/media/image") || !xlsxHasFile(t, path, "xl/drawings/drawing") {
		t.Fatal("streamed image sheet is missing drawing parts")
	}
}

func TestWriteAnalysisWorkbookFileKeepsFirstSheetActive(t *testing.T) {
	path := filepath.Join(t.TempDir(), "active-first-sheet.xlsx")
	sheets := []AnalysisSheet{
		{Sheet: "First", Data: [][]Cell{{"first"}}},
		{Sheet: "Second", Data: [][]Cell{{"second"}}},
		{Sheet: "Third", Data: [][]Cell{{"third"}}},
	}
	if err := WriteAnalysisWorkbookFile(path, sheets); err != nil {
		t.Fatalf("WriteAnalysisWorkbookFile() error = %v", err)
	}
	file, err := excelize.OpenFile(path)
	if err != nil {
		t.Fatalf("OpenFile() error = %v", err)
	}
	defer file.Close()
	active := file.GetActiveSheetIndex()
	if got := file.GetSheetName(active); got != "First" {
		t.Fatalf("active sheet = %q at index %d, want First", got, active)
	}
}

func TestWriteAnalysisWorkbookFileKeepsHeadersOnOneLine(t *testing.T) {
	path := filepath.Join(t.TempDir(), "single-line-header.xlsx")
	header := "インクルード先シンボル使用元ファイル"
	sheets := []AnalysisSheet{{
		Sheet: "Header", Data: [][]Cell{{header}, {"short"}},
		StickyRowsCount: 1, AutoFilterRef: "A1:A2",
	}}
	if err := WriteAnalysisWorkbookFile(path, sheets); err != nil {
		t.Fatalf("WriteAnalysisWorkbookFile() error = %v", err)
	}
	file, err := excelize.OpenFile(path)
	if err != nil {
		t.Fatalf("OpenFile() error = %v", err)
	}
	defer file.Close()
	width, err := file.GetColWidth("Header", "A")
	if err != nil {
		t.Fatalf("GetColWidth() error = %v", err)
	}
	if minimum := displayWidth(header) + 4; width < minimum {
		t.Fatalf("header width = %f, want at least %f", width, minimum)
	}
	styleID, err := file.GetCellStyle("Header", "A1")
	if err != nil {
		t.Fatalf("GetCellStyle() error = %v", err)
	}
	style, err := file.GetStyle(styleID)
	if err != nil {
		t.Fatalf("GetStyle() error = %v", err)
	}
	if style.Alignment != nil && style.Alignment.WrapText {
		t.Fatalf("header WrapText = true, want false: %#v", style.Alignment)
	}
}

func TestWriteAnalysisWorkbookFileBatchesLargeSheetProgress(t *testing.T) {
	path := filepath.Join(t.TempDir(), "progress.xlsx")
	rows := make([][]Cell, 1025)
	for index := range rows {
		rows[index] = []Cell{index}
	}
	rowEvents := []AnalysisProgressEvent{}
	if err := WriteAnalysisWorkbookFileWithOptions(path, []AnalysisSheet{{Sheet: "Data", Data: rows}}, AnalysisWorkbookWriteOptions{
		Progress: func(event AnalysisProgressEvent) {
			if event.Label == "excel.fileRows" {
				rowEvents = append(rowEvents, event)
			}
		},
	}); err != nil {
		t.Fatalf("WriteAnalysisWorkbookFileWithOptions() error = %v", err)
	}
	if len(rowEvents) >= len(rows) {
		t.Fatalf("row progress events = %d, want fewer than %d", len(rowEvents), len(rows))
	}
	if first := rowEvents[0]; first.Current != 1 || first.Total != len(rows) {
		t.Fatalf("first row progress = %#v", first)
	}
	if last := rowEvents[len(rowEvents)-1]; last.Current != len(rows) || last.Total != len(rows) {
		t.Fatalf("last row progress = %#v", last)
	}
}

func TestWriteAnalysisWorkbookFileReportsProgress(t *testing.T) {
	path := filepath.Join(t.TempDir(), "analysis-progress.xlsx")
	sheets := CreateAnalysisSheets(testSheetPayload(), LocaleJapanese, AnalysisSheetsOptions{
		TargetURI: "file:///workspace/main.asp",
	})
	events := []AnalysisProgressEvent{}
	if err := WriteAnalysisWorkbookFileWithOptions(path, sheets, AnalysisWorkbookWriteOptions{
		Progress: func(event AnalysisProgressEvent) {
			events = append(events, event)
		},
	}); err != nil {
		t.Fatalf("WriteAnalysisWorkbookFileWithOptions() error = %v", err)
	}
	content, err := os.ReadFile(path)
	if err != nil {
		t.Fatalf("ReadFile() error = %v", err)
	}
	if len(content) < 2 || string(content[:2]) != "PK" {
		t.Fatalf("workbook header = %q, want XLSX zip", content[:min(len(content), 2)])
	}
	labels := make([]string, 0, len(events))
	for _, event := range events {
		labels = append(labels, event.Label)
	}
	for _, label := range []string{"excel.file", "excel.fileSheet", "excel.fileRows", "excel.fileCommit"} {
		if !contains(labels, label) {
			t.Fatalf("writer progress labels missing %q in %#v", label, labels)
		}
	}
	if events[0].Label != "excel.file" || events[0].Current != 1 || events[0].Total != 1 {
		t.Fatalf("first writer progress event = %#v, want completed excel.file 1/1", events[0])
	}
	foundSummaryRows := false
	sheetCurrent := 0
	rowsBySheet := map[string]int{}
	for _, event := range events {
		if event.Label == "excel.fileSheet" {
			sheetCurrent++
			if event.Current != sheetCurrent || event.Total != len(sheets) || event.Detail == "" || len(event.ActiveItems) != 1 {
				t.Fatalf("file sheet progress = %#v, want completed sheet %d/%d", event, sheetCurrent, len(sheets))
			}
		}
		if event.Label == "excel.fileRows" {
			rowsBySheet[event.Detail]++
			if event.Current != rowsBySheet[event.Detail] || event.Total != len(sheetDataByName(t, sheets, event.Detail)) {
				t.Fatalf("row progress = %#v, want completed row %d", event, rowsBySheet[event.Detail])
			}
		}
		if event.Label == "excel.fileRows" && event.Detail == "概要" {
			foundSummaryRows = true
			if len(event.ActiveItems) != 1 || event.ActiveItems[0] != "概要" {
				t.Fatalf("summary row progress = %#v, want active summary sheet", event)
			}
		}
	}
	if !foundSummaryRows {
		t.Fatalf("writer progress missing summary rows: %#v", events)
	}
	if sheetCurrent != len(sheets) {
		t.Fatalf("file sheet progress count = %d, want %d", sheetCurrent, len(sheets))
	}
	last := events[len(events)-1]
	if last.Label != "excel.fileCommit" || last.Current != last.Total || len(last.ActiveItems) != 0 {
		t.Fatalf("last writer progress event = %#v, want complete excel.fileCommit", last)
	}
	commitEvents := []AnalysisProgressEvent{}
	for _, event := range events {
		if event.Label == "excel.fileCommit" {
			commitEvents = append(commitEvents, event)
		}
	}
	if len(commitEvents) != 2 || commitEvents[0].Current != 0 || commitEvents[0].Total != 1 ||
		commitEvents[1].Current != 1 || commitEvents[1].Total != 1 {
		t.Fatalf("commit progress = %#v, want 0/1 then completed 1/1", commitEvents)
	}
}

func TestWriteAnalysisWorkbookFilePaginatesRowsColumnsAndLongCells(t *testing.T) {
	path := filepath.Join(t.TempDir(), "paginated.xlsx")
	sheets := []AnalysisSheet{{
		Sheet: "Very Long Analysis Sheet Name That Needs Continuations",
		Data: [][]Cell{
			{"ID", "A", "B", "C"},
			{1, "one", "two", "abcdefghij"},
			{2, "three", "four", "klmnopqrst"},
			{3, "five", "six", "uvwxyz"},
		},
		StickyRowsCount: 1,
		AutoFilterRef:   "A1:D4",
	}}
	limits := workbookFormatLimits{rows: 3, columns: 2, cellUnits: 5}
	if err := WriteAnalysisWorkbookFileWithOptions(path, sheets, AnalysisWorkbookWriteOptions{limits: &limits}); err != nil {
		t.Fatalf("WriteAnalysisWorkbookFileWithOptions() error = %v", err)
	}
	file, err := excelize.OpenFile(path)
	if err != nil {
		t.Fatalf("OpenFile() error = %v", err)
	}
	defer file.Close()
	names := file.GetSheetList()
	if len(names) < 5 {
		t.Fatalf("physical sheet count = %d, want at least 5; names=%#v", len(names), names)
	}
	for _, name := range names {
		if len([]rune(name)) > xlsxMaximumSheetRunes {
			t.Fatalf("sheet name exceeds XLSX limit: %q", name)
		}
	}
	if got, err := file.GetCellValue(names[0], "A1"); err != nil || got != "ID" {
		t.Fatalf("first continuation header = %q, %v; want ID", got, err)
	}
	if got, err := file.GetCellValue(names[1], "A2"); err != nil || got != "3" {
		t.Fatalf("second row continuation first value = %q, %v; want 3", got, err)
	}
	if got, err := file.GetCellValue(names[2], "B2"); err != nil || !strings.HasPrefix(got, "[Overflow:") {
		t.Fatalf("long cell reference = %q, %v; want overflow reference", got, err)
	}
	foundChunk := false
	for _, name := range names[4:] {
		rows, err := file.GetRows(name)
		if err != nil {
			t.Fatalf("GetRows(%q) error = %v", name, err)
		}
		for _, row := range rows {
			for _, cell := range row {
				foundChunk = foundChunk || cell == "abcde"
			}
		}
	}
	if !foundChunk {
		t.Fatalf("overflow sheets do not retain first long-cell chunk: %#v", names)
	}
}

func sheetDataByName(t *testing.T, sheets []AnalysisSheet, name string) [][]Cell {
	t.Helper()
	for _, sheet := range sheets {
		if sheet.Sheet == name {
			return sheet.Data
		}
	}
	t.Fatalf("progress references unknown sheet %q", name)
	return nil
}

func xlsxContains(t *testing.T, path string, needle string) bool {
	t.Helper()
	reader, err := zip.OpenReader(path)
	if err != nil {
		t.Fatalf("OpenReader() error = %v", err)
	}
	defer reader.Close()
	for _, file := range reader.File {
		content, err := readZipFile(file)
		if err != nil {
			t.Fatalf("readZipFile(%q) error = %v", file.Name, err)
		}
		if strings.Contains(string(content), needle) {
			return true
		}
	}
	return false
}

func xlsxHasFile(t *testing.T, path, prefix string) bool {
	t.Helper()
	reader, err := zip.OpenReader(path)
	if err != nil {
		t.Fatalf("OpenReader() error = %v", err)
	}
	defer reader.Close()
	for _, file := range reader.File {
		if strings.HasPrefix(file.Name, prefix) {
			return true
		}
	}
	return false
}

func assertFileBytes(t *testing.T, path string, want []byte) {
	t.Helper()
	got, err := os.ReadFile(path)
	if err != nil {
		t.Fatalf("ReadFile(%q) error = %v", path, err)
	}
	if string(got) != string(want) {
		t.Fatalf("%q contents = %q, want %q", path, got, want)
	}
}

func assertNoWorkbookTempFiles(t *testing.T, path string) {
	t.Helper()
	pattern := filepath.Join(filepath.Dir(path), "."+filepath.Base(path)+".tmp-*"+filepath.Ext(path))
	matches, err := filepath.Glob(pattern)
	if err != nil {
		t.Fatalf("Glob(%q) error = %v", pattern, err)
	}
	if len(matches) != 0 {
		t.Fatalf("temporary workbook artifacts remain: %#v", matches)
	}
}

func readZipFile(file *zip.File) ([]byte, error) {
	reader, err := file.Open()
	if err != nil {
		return nil, err
	}
	defer reader.Close()
	return io.ReadAll(reader)
}

func assertStyledWorksheet(t *testing.T, file *excelize.File, sheet, headerCell, bodyCell string) {
	t.Helper()
	headerStyle, err := file.GetCellStyle(sheet, headerCell)
	if err != nil {
		t.Fatalf("GetCellStyle(%q, %q) error = %v", sheet, headerCell, err)
	}
	bodyStyle, err := file.GetCellStyle(sheet, bodyCell)
	if err != nil {
		t.Fatalf("GetCellStyle(%q, %q) error = %v", sheet, bodyCell, err)
	}
	if headerStyle == 0 {
		t.Fatalf("%s!%s header style = 0, want explicit style", sheet, headerCell)
	}
	if headerStyle == bodyStyle {
		t.Fatalf("%s header/body styles both = %d, want distinct presentation", sheet, headerStyle)
	}
}

func assertColumnHasReadableWidth(t *testing.T, file *excelize.File, sheet, column string) {
	t.Helper()
	width, err := file.GetColWidth(sheet, column)
	if err != nil {
		t.Fatalf("GetColWidth(%q, %q) error = %v", sheet, column, err)
	}
	if width < 10 {
		t.Fatalf("%s!%s width = %f, want readable width", sheet, column, width)
	}
}
