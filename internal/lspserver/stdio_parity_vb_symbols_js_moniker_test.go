package lspserver

import (
	"os"
	"path/filepath"
	"strconv"
	"strings"
	"testing"
	"unicode/utf16"

	"github.com/yottonoko/classic-web-system-lsp/internal/core"
	"github.com/yottonoko/classic-web-system-lsp/internal/lsp"
)

func TestStdioParityReturnsVBProjectWorkspaceSymbolsWithClassOwners(t *testing.T) {
	client := startStdioTestClient(t)
	defer client.close()

	root := t.TempDir()
	uri := pathToFileURI(filepath.Join(root, "default.asp"))
	client.request("initialize", map[string]any{
		"processId":    nil,
		"rootUri":      pathToFileURI(root),
		"capabilities": map[string]any{},
	})
	includePath := filepath.Join(root, "late-shared.inc")
	includeURI := pathToFileURI(includePath)
	includeSource := `<%
Dim IncludedGlobal
ProjectAssigned = "shared"
Class Widget
  Public FieldValue
  Public Sub Save()
  End Sub
  Public Property Get Name()
    Name = FieldValue
  End Property
End Class
%>`
	if err := os.WriteFile(includePath, []byte(includeSource), 0o644); err != nil {
		t.Fatal(err)
	}
	source := `<!-- #include file="late-shared.inc" -->`
	openClassicASPDocument(t, client, uri, source)

	for _, expected := range []struct {
		name      string
		kind      int
		container string
	}{
		{name: "IncludedGlobal", kind: 13},
		{name: "ProjectAssigned", kind: 13},
		{name: "Widget", kind: 5},
		{name: "FieldValue", kind: 8, container: "Widget"},
		{name: "Save", kind: 12, container: "Widget"},
		{name: "Name", kind: 7, container: "Widget"},
	} {
		response := client.request("workspace/symbol", map[string]any{"query": expected.name})
		var symbols []lsp.SymbolInformation
		mustDecodeResult(t, response.Result, &symbols)
		found := false
		for _, symbol := range symbols {
			if symbol.Name == expected.name && symbol.Kind == expected.kind && symbol.ContainerName == expected.container && symbol.Location.URI == includeURI {
				found = true
				break
			}
		}
		if !found {
			t.Fatalf("VB project workspace symbol missing %#v: %s", expected, mustJSONText(t, response.Result))
		}
	}
}

func TestStdioParityReturnsOwnedVBPropertyAccessorDocumentSymbols(t *testing.T) {
	client := startStdioTestClient(t)
	defer client.close()

	root := t.TempDir()
	uri := pathToFileURI(filepath.Join(root, "properties.asp"))
	source := `<%
Class Widget
  Public Sub Save()
  End Sub
  Public Property Get Name()
    Name = m_name
  End Property
  Public Property Let Name(value)
    m_name = value
  End Property
  Public Property Set Name(value)
    Set m_name = value
  End Property
End Class
Function BuildWidget()
End Function
%>`
	client.request("initialize", map[string]any{
		"processId":    nil,
		"rootUri":      pathToFileURI(root),
		"capabilities": map[string]any{},
	})
	openClassicASPDocument(t, client, uri, source)

	response := client.request("textDocument/documentSymbol", map[string]any{"textDocument": map[string]any{"uri": uri}})
	var symbols []lsp.DocumentSymbol
	mustDecodeResult(t, response.Result, &symbols)
	wantNames := map[string]int{"Widget": 5, "Widget.Save": 12, "BuildWidget": 12}
	propertyLines := map[int]struct{}{4: {}, 7: {}, 10: {}}
	propertyCount := 0
	for _, symbol := range symbols {
		if wantKind, ok := wantNames[symbol.Name]; ok {
			if symbol.Kind != wantKind || symbol.Range != symbol.SelectionRange {
				t.Fatalf("VB document symbol %q = %#v", symbol.Name, symbol)
			}
			delete(wantNames, symbol.Name)
		}
		if symbol.Name == "Widget.Name" {
			propertyCount++
			if symbol.Kind != 7 || symbol.Range != symbol.SelectionRange {
				t.Fatalf("VB property symbol = %#v", symbol)
			}
			if _, ok := propertyLines[symbol.Range.Start.Line]; !ok {
				t.Fatalf("VB property symbol line = %d: %#v", symbol.Range.Start.Line, symbol)
			}
			delete(propertyLines, symbol.Range.Start.Line)
		}
	}
	if len(wantNames) != 0 || propertyCount != 3 || len(propertyLines) != 0 {
		t.Fatalf("VB document symbols missing names=%v propertyCount=%d lines=%v: %s", wantNames, propertyCount, propertyLines, mustJSONText(t, response.Result))
	}
}

func TestStdioParityReturnsExactTypeScriptQuickInfoMonikers(t *testing.T) {
	client := startStdioTestClient(t)
	defer client.close()

	root := t.TempDir()
	uri := pathToFileURI(filepath.Join(root, "monikers.asp"))
	source := `<script>
const emoji = "😀";
function renderCard() {}
class Dashboard { constructor() {} }
const localValue = 1;
renderCard();
new Dashboard();
localValue;
missingValue;
</script>`
	client.request("initialize", map[string]any{
		"processId":    nil,
		"rootUri":      pathToFileURI(root),
		"capabilities": map[string]any{},
	})
	openClassicASPDocument(t, client, uri, source)
	parsed := core.ParseDocument(uri, source, core.Settings{DefaultLanguage: "VBScript"})
	virtual := core.BuildVirtualDocument(parsed, core.LanguageJavaScript)

	for _, expected := range []struct {
		name string
		kind string
	}{
		{name: "renderCard", kind: "export"},
		{name: "Dashboard", kind: "export"},
		{name: "constructor", kind: "local"},
		{name: "localValue", kind: "local"},
	} {
		sourceOffset := strings.LastIndex(source, expected.name)
		virtualOffset, ok := virtual.ToVirtualOffset(sourceOffset)
		if !ok {
			t.Fatalf("cannot map %q into JavaScript virtual document", expected.name)
		}
		wantIdentifier := uri + "#javascript#" + expected.name + "#" + strconv.Itoa(utf16Units(virtual.Text[:virtualOffset])) + "#" + strconv.Itoa(len(expected.name))
		response := client.request("textDocument/moniker", map[string]any{
			"textDocument": map[string]any{"uri": uri},
			"position":     positionAt(source, sourceOffset+1),
		})
		var monikers []lsp.Moniker
		mustDecodeResult(t, response.Result, &monikers)
		if len(monikers) != 1 || monikers[0].Scheme != "asp-lsp-js" || monikers[0].Identifier != wantIdentifier || monikers[0].Unique != "project" || monikers[0].Kind != expected.kind {
			t.Fatalf("JavaScript moniker for %q = %#v, want %q/%s: %s", expected.name, monikers, wantIdentifier, expected.kind, mustJSONText(t, response.Result))
		}
	}

	unresolvedOffset := strings.Index(source, "missingValue")
	unresolved := client.request("textDocument/moniker", map[string]any{
		"textDocument": map[string]any{"uri": uri},
		"position":     positionAt(source, unresolvedOffset+1),
	})
	if got := mustJSONText(t, unresolved.Result); got != "[]" {
		t.Fatalf("unresolved JavaScript moniker = %s, want []", got)
	}
}

func utf16Units(value string) int {
	return len(utf16.Encode([]rune(value)))
}
