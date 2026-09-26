package lspserver

import (
	"path/filepath"
	"slices"
	"strings"
	"testing"

	"github.com/xuri/excelize/v2"
	"github.com/yottonoko/classic-web-system-lsp/internal/core"
	"github.com/yottonoko/classic-web-system-lsp/internal/excel"
	"github.com/yottonoko/classic-web-system-lsp/internal/lsp"
)

func TestVBScriptTypeDisplayMatchesInlayAcrossHoverAndExcel(t *testing.T) {
	const uri = "file:///bench/types/display.asp"
	const source = `<%
Dim route
route = "home"
Response.Write route
route = "login"
Dim count
count = 42
Dim enabled
enabled = True
Dim dynamic
dynamic = "known"
dynamic = ResolveAtRuntime()
' @type annotated As String
Dim annotated
annotated = "value"
Response.Write route & count & enabled & dynamic & annotated
%>`
	server := benchmarkServerWithWorkspaceSources([]benchmarkScriptSource{{URI: uri, Text: source}})
	configureExcelTestWorkspaceRoot(server, "/bench")
	parsed := core.ParseDocument(uri, source, core.Settings{DefaultLanguage: "VBScript"})
	doc := core.SourceDocument(parsed)
	hints := vbscriptVariableTypeInlayHints(parsed, lsp.Range{End: doc.PositionAt(len(source))}, vbscriptVariableTypeInlayOptions{VariableTypes: true})
	want := map[string]string{"route": `"home" | "login"`, "count": "42", "enabled": "True", "dynamic": "Variant", "annotated": "String"}
	for _, declaration := range variableInlayDeclarations(parsed, true, nil) {
		expected, ok := want[declaration.Name]
		if !ok {
			continue
		}
		found := false
		for _, hint := range hints {
			if hint.Position == doc.PositionAt(declaration.End) {
				found = true
				if hint.Label != " As "+expected {
					t.Errorf("inlay %s = %v", declaration.Name, hint.Label)
				}
			}
		}
		if !found {
			t.Errorf("missing inlay for %s", declaration.Name)
		}
		for _, offset := range []int{declaration.Start, strings.LastIndex(source, declaration.Name)} {
			hover := server.hover(uri, doc.PositionAt(offset))
			typed, _ := hover.(*lsp.Hover)
			value, _ := hoverContentsValue(typed)
			if !strings.Contains(value, " As "+expected+"\n") {
				t.Errorf("hover %s at %d = %q, want %q", declaration.Name, offset, value, expected)
			}
		}
	}
	payload, ok := server.exportAnalysisPayload(analysisExcelExportArg{FileURIs: []string{uri}})
	if !ok {
		t.Fatal("Excel graph failed")
	}
	for name, expected := range want {
		found := false
		for _, node := range payload.Nodes {
			if node.Kind == "vbDeclaration" && node.Label == name {
				found = true
				if node.TypeName != expected {
					t.Errorf("graph %s = %q, want %q", name, node.TypeName, expected)
				}
			}
		}
		if !found {
			t.Errorf("missing graph node %s", name)
		}
	}
	sheets := excel.CreateAnalysisSheets(payload, excel.LocaleEnglish, excel.AnalysisSheetsOptions{})
	target := filepath.Join(t.TempDir(), "types.xlsx")
	if err := excel.WriteAnalysisWorkbookFile(target, sheets); err != nil {
		t.Fatal(err)
	}
	book, err := excelize.OpenFile(target)
	if err != nil {
		t.Fatal(err)
	}
	defer book.Close()
	for name, expected := range want {
		found := false
		for _, sheet := range book.GetSheetList() {
			rows, err := book.GetRows(sheet)
			if err != nil {
				t.Fatal(err)
			}
			for _, row := range rows {
				if slices.Contains(row, name) && slices.Contains(row, expected) {
					found = true
				}
			}
		}
		if !found {
			t.Errorf("workbook missing %s with type %s", name, expected)
		}
	}
}
