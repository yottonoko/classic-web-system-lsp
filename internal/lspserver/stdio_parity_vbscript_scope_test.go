package lspserver

import (
	"path/filepath"
	"strings"
	"testing"

	"github.com/yottonoko/classic-web-system-lsp/internal/lsp"
)

func TestStdioParityKeepsLocalDefinitionAheadOfDeferredGlobals(t *testing.T) {
	client := startStdioTestClient(t)
	defer client.close()

	root := t.TempDir()
	uri := pathToFileURI(filepath.Join(root, "shadow.asp"))
	source := `<%
Dim Value
Sub UseValue()
  Dim Value
  Value = IncludedValue
End Sub
%>`
	client.request("initialize", map[string]any{
		"processId":    nil,
		"rootUri":      pathToFileURI(root),
		"capabilities": map[string]any{},
	})
	openClassicASPDocumentWithDiagnostics(t, client, uri, source)

	definition := mustJSONText(t, client.request("textDocument/definition", map[string]any{
		"textDocument": map[string]any{"uri": uri},
		"position":     positionAt(source, strings.LastIndex(source, "Value =")),
	}).Result)
	if !strings.Contains(definition, `"line":3`) || strings.Contains(definition, `"line":1`) {
		t.Fatalf("local shadow definition mismatch: %s", definition)
	}
}

func TestStdioParityJumpsToEveryNameInMultipleDimDeclaration(t *testing.T) {
	client := startStdioTestClient(t)
	defer client.close()

	root := t.TempDir()
	uri := pathToFileURI(filepath.Join(root, "multiple-dim.asp"))
	source := `<%
Dim first, second, third
Response.Write first
Response.Write second
Response.Write third
%>`
	client.request("initialize", map[string]any{
		"processId":    nil,
		"rootUri":      pathToFileURI(root),
		"capabilities": map[string]any{},
	})
	openClassicASPDocumentWithDiagnostics(t, client, uri, source)

	for _, name := range []string{"first", "second", "third"} {
		declarationOffset := strings.Index(source, name)
		usageOffset := strings.LastIndex(source, name)
		response := client.request("textDocument/definition", map[string]any{
			"textDocument": map[string]any{"uri": uri},
			"position":     positionAt(source, usageOffset),
		})
		var locations []lsp.Location
		mustDecodeResult(t, response.Result, &locations)
		wantRange := mapPositionRange(
			positionAt(source, declarationOffset),
			positionAt(source, declarationOffset+len(name)),
		)
		if len(locations) != 1 || locations[0].URI != uri || locations[0].Range != wantRange {
			t.Fatalf("definition for %s = %#v, want %s %#v", name, locations, uri, wantRange)
		}
	}
}
