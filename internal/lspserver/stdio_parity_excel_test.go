package lspserver

import (
	"os"
	"path/filepath"
	"strings"
	"testing"

	"github.com/xuri/excelize/v2"
)

func TestStdioParityExportsAnalysisXLSXFilesFromGoServer(t *testing.T) {
	client := startStdioTestClient(t)
	defer client.close()

	root := t.TempDir()
	targetPath := filepath.Join(root, "analysis.xlsx")
	uri := pathToFileURI(filepath.Join(root, "go-export.asp"))
	source := `<%
Function BuildName()
End Function
Response.Write BuildName()
%>`
	client.request("initialize", map[string]any{
		"processId":    nil,
		"rootUri":      pathToFileURI(root),
		"capabilities": map[string]any{},
	})
	notifyOpenClassicASPDocument(t, client, uri, source)

	result := executeCommand(t, client, "aspLsp.server.exportAnalysisExcel", []map[string]any{{
		"scope":      "document",
		"uri":        uri,
		"targetPath": targetPath,
	}})
	assertExcelExportResult(t, result, targetPath)
	expectAnalysisWorkbookSheets(t, targetPath)
}

func TestStdioParityReportsExcelExportProgress(t *testing.T) {
	client := startStdioTestClient(t)
	defer client.close()

	root := t.TempDir()
	targetPath := filepath.Join(root, "analysis-progress.xlsx")
	uri := pathToFileURI(filepath.Join(root, "progress.asp"))
	source := `<%
Dim ProgressValue
ProgressValue = 1
Response.Write ProgressValue
%>`
	client.request("initialize", map[string]any{
		"processId":    nil,
		"rootUri":      pathToFileURI(root),
		"capabilities": map[string]any{},
	})
	notifyOpenClassicASPDocument(t, client, uri, source)
	client.waitForNotification("textDocument/publishDiagnostics", uri)
	client.drainNotifications("aspLsp/status")

	result := executeCommand(t, client, "aspLsp.server.exportAnalysisExcel", []map[string]any{{
		"scope":      "document",
		"uri":        uri,
		"targetPath": targetPath,
	}})
	assertExcelExportResult(t, result, targetPath)

	statusText := mustJSONText(t, client.drainNotifications("aspLsp/status"))
	for _, expected := range []string{`"reason":"excel.export"`, "excel.graph", "excel.fileCommit", `"activeItems"`, `"status":"idle"`} {
		if !strings.Contains(statusText, expected) {
			t.Fatalf("Excel export progress missing %q: %s", expected, statusText)
		}
	}
	expectAnalysisWorkbookSheets(t, targetPath)
}

func TestStdioParityExportsActiveDocumentAnalysisWorkbookFromServerCommand(t *testing.T) {
	client := startStdioTestClient(t)
	defer client.close()

	root := t.TempDir()
	page := filepath.Join(root, "default.asp")
	targetPath := filepath.Join(root, "analysis.xlsx")
	folderTargetPath := filepath.Join(root, "folder-analysis.xlsx")
	writeWorkspaceGraphFixture(t, filepath.Join(root, "common.inc"), `<%
Const IncludedValue = 1
%>`)
	source := `<!-- #include file="common.inc" -->
<%
Dim PageValue
PageValue = IncludedValue
Response.Write PageValue
%>`
	writeWorkspaceGraphFixture(t, page, source)
	uri := pathToFileURI(page)
	client.request("initialize", map[string]any{
		"processId":    nil,
		"rootUri":      pathToFileURI(root),
		"capabilities": map[string]any{},
	})
	notifyConfiguration(t, client, map[string]any{"aspLsp": map[string]any{
		"locale": "ja",
		"excel":  map[string]any{"locale": "ja"},
	}})
	notifyOpenClassicASPDocument(t, client, uri, source)

	result := executeCommand(t, client, "aspLsp.server.exportAnalysisExcel", []map[string]any{{
		"scope":      "document",
		"uri":        uri,
		"targetPath": targetPath,
	}})
	assertExcelExportResult(t, result, targetPath)
	expectAnalysisWorkbookSheets(t, targetPath)

	folderResult := executeCommand(t, client, "aspLsp.server.exportAnalysisExcel", []map[string]any{{
		"scope":      "folder",
		"uri":        pathToFileURI(root),
		"targetPath": folderTargetPath,
	}})
	assertExcelExportResult(t, folderResult, folderTargetPath)
	expectAnalysisWorkbookSheets(t, folderTargetPath)
}

func TestStdioParityAppliesWorkspaceGlobsToSelectedDocumentExcelExport(t *testing.T) {
	client := startStdioTestClient(t)
	defer client.close()

	root := t.TempDir()
	includesDir := filepath.Join(root, "includes")
	legacyDir := filepath.Join(root, "legacy")
	if err := os.MkdirAll(includesDir, 0o755); err != nil {
		t.Fatal(err)
	}
	if err := os.MkdirAll(legacyDir, 0o755); err != nil {
		t.Fatal(err)
	}
	page := filepath.Join(root, "default.asp")
	parent := filepath.Join(root, "parent.asp")
	sibling := filepath.Join(root, "sibling.asp")
	unrelated := filepath.Join(root, "unrelated.asp")
	common := filepath.Join(includesDir, "common.inc")
	skipped := filepath.Join(legacyDir, "skip.inc")
	targetPath := filepath.Join(root, "selected-analysis.xlsx")
	source := `<!-- #include file="includes/common.inc" -->
<!-- #include file="legacy/skip.inc" -->
<%
Response.Write CommonValue
Response.Write SkippedValue()
%>`
	writeWorkspaceGraphFixture(t, page, source)
	writeWorkspaceGraphFixture(t, parent, `<!-- #include file="default.asp" -->
<!-- #include file="sibling.asp" -->`)
	writeWorkspaceGraphFixture(t, sibling, `<%
Const SiblingValue = 3
%>`)
	writeWorkspaceGraphFixture(t, unrelated, `<%
Const UnrelatedValue = 4
%>`)
	writeWorkspaceGraphFixture(t, common, `<%
Const CommonValue = 1
%>`)
	writeWorkspaceGraphFixture(t, skipped, `<%
Function SkippedValue()
    SkippedValue = 2
End Function
%>`)
	uri := pathToFileURI(page)
	initializeAndWaitForWorkspaceIndex(t, client, map[string]any{
		"processId":    nil,
		"rootUri":      pathToFileURI(root),
		"capabilities": map[string]any{},
	})
	notifyOpenClassicASPDocument(t, client, uri, source)

	result := executeCommand(t, client, "aspLsp.server.exportAnalysisExcel", []map[string]any{{
		"scope":            "document",
		"uri":              uri,
		"targetPath":       targetPath,
		"includeGlobs":     []string{"**/*.asp", "**/*.inc"},
		"excludeGlobs":     []string{"legacy/**"},
		"respectGitIgnore": false,
	}})
	assertExcelExportResult(t, result, targetPath)
	workbookText := workbookText(t, targetPath)
	for _, expected := range []string{"CommonValue", "sibling.asp", "SkippedValue", "legacy/skip.inc"} {
		if !strings.Contains(workbookText, expected) {
			t.Fatalf("selected export workbook missing %s: %s", expected, workbookText)
		}
	}
	for _, unexpected := range []string{"UnrelatedValue", "unrelated.asp"} {
		if strings.Contains(workbookText, unexpected) {
			t.Fatalf("selected export workbook unexpectedly contained %s: %s", unexpected, workbookText)
		}
	}
}

func TestStdioParityUsesExcelLocaleForAnalysisWorkbook(t *testing.T) {
	client := startStdioTestClient(t)
	defer client.close()

	root := t.TempDir()
	page := filepath.Join(root, "default.asp")
	targetPath := filepath.Join(root, "analysis-en.xlsx")
	source := `<%
Dim PageValue
PageValue = 1
%>`
	writeWorkspaceGraphFixture(t, page, source)
	uri := pathToFileURI(page)
	client.request("initialize", map[string]any{
		"processId":    nil,
		"rootUri":      pathToFileURI(root),
		"capabilities": map[string]any{},
	})
	notifyConfiguration(t, client, map[string]any{"aspLsp": map[string]any{
		"locale": "ja",
		"excel": map[string]any{
			"locale": "en",
			"includeRelatedIncludeTreesForUnresolved": false,
			"skipTypeInference":                       true,
		},
	}})
	notifyOpenClassicASPDocument(t, client, uri, source)

	result := executeCommand(t, client, "aspLsp.server.exportAnalysisExcel", []map[string]any{{
		"scope":      "document",
		"uri":        uri,
		"targetPath": targetPath,
	}})
	assertExcelExportResult(t, result, targetPath)
	file, err := excelize.OpenFile(targetPath)
	if err != nil {
		t.Fatal(err)
	}
	defer file.Close()
	if index, err := file.GetSheetIndex("Summary"); err != nil || index < 0 {
		t.Fatalf("English workbook missing Summary sheet: %d %v", index, err)
	}
	if got, err := file.GetCellValue("Summary", "A1"); err != nil || got != "Name" {
		t.Fatalf("English summary header = %q, %v; want Name", got, err)
	}
	text := workbookText(t, targetPath)
	for _, expected := range []string{
		"Excel language",
		"en",
		"Related include tree analysis",
		"Disabled",
		"Skip type inference",
		"Enabled",
		"Editor inference type details",
		"Disabled",
	} {
		if !strings.Contains(text, expected) {
			t.Fatalf("English workbook metadata missing %q: %s", expected, text)
		}
	}
}

func expectXLSXZipHeader(t *testing.T, path string) {
	t.Helper()
	content, err := os.ReadFile(path)
	if err != nil {
		t.Fatal(err)
	}
	if len(content) < 2 || string(content[:2]) != "PK" {
		t.Fatalf("%s is not an XLSX zip file", path)
	}
}

func assertExcelExportResult(t *testing.T, result any, targetPath string) {
	t.Helper()
	var payload struct {
		OK         bool   `json:"ok"`
		TargetPath string `json:"targetPath"`
	}
	if err := remarshal(result, &payload); err != nil {
		t.Fatalf("exportAnalysisExcel result could not be decoded: %v: %s", err, mustJSONText(t, result))
	}
	if !payload.OK || filepath.Clean(payload.TargetPath) != filepath.Clean(targetPath) {
		t.Fatalf("exportAnalysisExcel result = %#v, want ok with target path %q", payload, targetPath)
	}
}

func expectAnalysisWorkbookSheets(t *testing.T, path string) {
	t.Helper()
	expectXLSXZipHeader(t, path)
	file, err := excelize.OpenFile(path)
	if err != nil {
		t.Fatal(err)
	}
	defer file.Close()
	for _, sheet := range []string{"概要", "宣言", "チャート元データ"} {
		index, err := file.GetSheetIndex(sheet)
		if err != nil || index < 0 {
			t.Fatalf("workbook %s missing sheet %s", path, sheet)
		}
	}
	for _, sheet := range []string{"インクルードツリー", "分析サマリ", "ファイル内使用", "外部ファイルからの使用", "include 先シンボル使用", "未使用", "未解決"} {
		index, err := file.GetSheetIndex(sheet)
		if err != nil || index < 0 {
			t.Fatalf("analysis workbook %s missing rich sheet %s", path, sheet)
		}
	}
	if visible, err := file.GetSheetVisible("チャート元データ"); err != nil || visible {
		t.Fatalf("chart data sheet visibility = %v, %v", visible, err)
	}
	if got, err := file.GetCellValue("概要", "A1"); err != nil || got != "名前" {
		t.Fatalf("summary header = %q, %v; want 名前", got, err)
	}
	if got, err := file.GetCellValue("概要", "B2"); err != nil || got == "" {
		t.Fatalf("summary scope value = %q, %v; want non-empty localized scope", got, err)
	}
}

func workbookText(t *testing.T, path string) string {
	t.Helper()
	file, err := excelize.OpenFile(path)
	if err != nil {
		t.Fatal(err)
	}
	defer file.Close()
	values := []string{}
	for _, sheet := range file.GetSheetList() {
		rows, err := file.GetRows(sheet)
		if err != nil {
			t.Fatal(err)
		}
		for _, row := range rows {
			values = append(values, row...)
		}
	}
	return strings.Join(values, "\n")
}
