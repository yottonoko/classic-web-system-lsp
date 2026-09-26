package lspserver

import (
	"path/filepath"
	"strings"
	"testing"
)

func TestStdioParityBuildsVBScriptLSPHelperDataForTypesHintsSelectionAndResolve(t *testing.T) {
	client := startStdioTestClient(t)
	defer client.close()

	root := t.TempDir()
	uri := pathToFileURI(filepath.Join(root, "vbscript-lsp-helper-data.asp"))
	source := `<%
Class Customer
  Public Name
End Class
Function BuildName(firstName)
  Dim c
  Set c = New Customer
  BuildName = c.Name
End Function
Response.Write BuildName("Ada")
%>`
	client.request("initialize", map[string]any{
		"processId":    nil,
		"rootUri":      pathToFileURI(root),
		"capabilities": map[string]any{},
	})
	notifyConfiguration(t, client, map[string]any{"aspLsp": map[string]any{
		"inlayHints": map[string]any{"functionReturnTypes": true, "variableTypes": true, "implicitByRef": true},
	}})
	openClassicASPDocument(t, client, uri, source)

	typeDefinition := client.request("textDocument/typeDefinition", map[string]any{
		"textDocument": map[string]any{"uri": uri},
		"position":     positionAt(source, strings.Index(source, "c.Name")),
	})
	if !strings.Contains(mustJSONText(t, typeDefinition.Result), `"line":1`) {
		t.Fatalf("typeDefinition did not resolve c.Name to Customer: %s", mustJSONText(t, typeDefinition.Result))
	}

	inlayHints := requestInlayHintsText(t, client, uri, 0, 11)
	for _, expected := range []string{`"label":" As Customer"`, "firstName:", `"label":"ByRef "`} {
		if !strings.Contains(inlayHints, expected) {
			t.Fatalf("inlay hints missing %q: %s", expected, inlayHints)
		}
	}
	if strings.Contains(inlayHints, `"label":" As Variant","paddingLeft":false,"paddingRight":true,"position":{"character":28,"line":4}`) {
		t.Fatalf("inlay hints added declaration parameter type hint: %s", inlayHints)
	}

	selection := client.request("textDocument/selectionRange", map[string]any{
		"textDocument": map[string]any{"uri": uri},
		"positions":    []map[string]int{positionAt(source, strings.Index(source, "BuildName =")+2)},
	})
	selectionText := mustJSONText(t, selection.Result)
	if !strings.Contains(selectionText, `"parent"`) {
		t.Fatalf("selection range missing parent: %s", mustJSONText(t, selection.Result))
	}

	completions := completionItems(client.request("textDocument/completion", map[string]any{
		"textDocument": map[string]any{"uri": uri},
		"position":     positionAt(source, strings.LastIndex(source, "Bui")+len("Bui")),
	}).Result)
	buildCompletion, ok := completions.find("BuildName")
	if !ok {
		t.Fatalf("BuildName completion missing: %#v", completionItemLabels(completions))
	}
	resolved := client.request("completionItem/resolve", buildCompletion)
	for _, expected := range []string{"Defined in [vbscript-lsp-helper-data.asp]", uri} {
		if !strings.Contains(mustJSONText(t, resolved.Result), expected) {
			t.Fatalf("resolved completion missing %q: %s", expected, mustJSONText(t, resolved.Result))
		}
	}

	returnURI := pathToFileURI(filepath.Join(root, "vbscript-return-docs.asp"))
	returnSource := `<%
' @returns String
Function BuildHtml()
End Function
%>`
	openClassicASPDocument(t, client, returnURI, returnSource)
	returnHints := requestInlayHintsText(t, client, returnURI, 0, 5)
	if !strings.Contains(returnHints, `"label":" As String"`) {
		t.Fatalf("return type inlay hint missing: %s", returnHints)
	}
}
