package lspserver

import (
	"os"
	"path/filepath"
	"strings"
	"testing"

	"github.com/yottonoko/classic-web-system-lsp/internal/lsp"
)

func TestStdioParityKeepsJavaScriptSemanticDiagnosticsOutsideManyASPIslandSourceMapHoles(t *testing.T) {
	client := startStdioTestClient(t)
	defer client.close()

	root := t.TempDir()
	uri := pathToFileURI(filepath.Join(root, "go-js-many-islands-semantic.asp"))
	source := `<div>before</div>
<script>
const fromAsp = <%= Request("id") %> + <% Response.Write ServerSideNumber %>;
const object = { key: "<%= ServerKey %>", more: <% implicitValue = 1 : Response.Write implicitValue %> };
const fake = "<% not an island";
missingAfterIslands.toFixed();
const after = maybeMissingAgain(<%= AfterArg %>);
</script>`
	client.request("initialize", map[string]any{
		"processId":    nil,
		"rootUri":      pathToFileURI(root),
		"capabilities": map[string]any{},
	})
	notifyConfiguration(t, client, map[string]any{"aspLsp": map[string]any{"checkJs": true}})
	diagnosticsMessage := openClassicASPDocumentWithDiagnostics(t, client, uri, source)
	semanticDiagnostics := diagnosticsFromSourceLSP(t, diagnosticsMessage, "asp-lsp-typescript")
	expectDiagnosticRangeLSP(t, semanticDiagnostics, "missingAfterIslands", lsp.Range{
		Start: lsp.Position{Line: 5, Character: 0},
		End:   lsp.Position{Line: 5, Character: len("missingAfterIslands")},
	})
	for _, diagnostic := range semanticDiagnostics {
		if strings.Contains(diagnostic.Message, "AfterArg") {
			t.Fatalf("semantic diagnostics should not mention ASP island symbol AfterArg: %s", mustJSONText(t, semanticDiagnostics))
		}
	}
	expectDiagnosticsOutsideAspIslands(t, source, semanticDiagnostics, []string{
		"<%= Request",
		"<% Response.Write ServerSideNumber",
		"<%= ServerKey",
		"<% implicitValue",
		"<%= AfterArg",
	})
}

func TestStdioParityMapsJavaScriptSemanticDiagnosticsAfterMultilineASPIslands(t *testing.T) {
	client := startStdioTestClient(t)
	defer client.close()

	root := t.TempDir()
	uri := pathToFileURI(filepath.Join(root, "go-js-multiline-island-range.asp"))
	source := `<script>
const before = <%=
  BuildValue(
    Request("id"))
%>;
<%
Dim shadowedName
shadowedName = "global"
Function LocalShadow()
  Dim shadowedName
  shadowedName = "local"
  LocalShadow = shadowedName
End Function
Response.Write LocalShadow()
%>
missingAfterMultilineIsland();
</script>`
	client.request("initialize", map[string]any{
		"processId":    nil,
		"rootUri":      pathToFileURI(root),
		"capabilities": map[string]any{},
	})
	notifyConfiguration(t, client, map[string]any{"aspLsp": map[string]any{"checkJs": true}})
	diagnosticsMessage := openClassicASPDocumentWithDiagnostics(t, client, uri, source)
	semanticDiagnostics := diagnosticsFromSourceLSP(t, diagnosticsMessage, "asp-lsp-typescript")
	start := mapPosition(positionAt(source, strings.Index(source, "missingAfterMultilineIsland")))
	expectDiagnosticRangeLSP(t, semanticDiagnostics, "missingAfterMultilineIsland", lsp.Range{
		Start: start,
		End: lsp.Position{
			Line:      start.Line,
			Character: start.Character + len("missingAfterMultilineIsland"),
		},
	})
	expectDiagnosticsOutsideAspIslands(t, source, semanticDiagnostics, []string{
		"<%=\n  BuildValue",
		"<%\nDim shadowedName",
	})
}

func TestStdioParityReturnsJavaScriptAutoImportCompletionEditsAndAddImportQuickFixes(t *testing.T) {
	client := startStdioTestClient(t)
	defer client.close()

	root := t.TempDir()
	if err := os.WriteFile(filepath.Join(root, "helpers.js"), []byte(`export function helperThing() {
  return "ok";
}
`), 0o644); err != nil {
		t.Fatal(err)
	}
	if err := os.WriteFile(filepath.Join(root, "jsconfig.json"), []byte(`{"include":["*.js","*.asp"]}`), 0o644); err != nil {
		t.Fatal(err)
	}
	marked := markedDocument(`<script type="module">
help<<<caret>>>
</script>`)
	uri := pathToFileURI(filepath.Join(root, "default.asp"))
	client.request("initialize", map[string]any{
		"processId":    nil,
		"rootUri":      pathToFileURI(root),
		"capabilities": map[string]any{},
	})
	notifyConfiguration(t, client, map[string]any{"aspLsp": map[string]any{
		"checkJs": true,
		"javascript": map[string]any{
			"autoImports": true,
		},
	}})
	openClassicASPDocumentWithDiagnostics(t, client, uri, marked.Text)

	completions := completionItems(client.request("textDocument/completion", map[string]any{
		"textDocument": map[string]any{"uri": uri},
		"position":     marked.Position,
	}).Result)
	helperItem, ok := completions.find("helperThing")
	if !ok {
		t.Fatalf("JavaScript auto-import completion missing helperThing: %s", mustJSONText(t, completions))
	}
	resolved := client.request("completionItem/resolve", helperItem)
	resolvedJSON := mustJSONText(t, resolved.Result)
	if !strings.Contains(resolvedJSON, "additionalTextEdits") || !strings.Contains(resolvedJSON, "./helpers") {
		t.Fatalf("JavaScript auto-import completion resolve missing import edit: %s", resolvedJSON)
	}

	callDocument := markedDocument(`<script type="module">
helper<<<caret>>>Thing();
</script>`)
	if err := client.notify("textDocument/didChange", map[string]any{
		"textDocument": map[string]any{"uri": uri, "version": 2},
		"contentChanges": []map[string]any{{
			"text": callDocument.Text,
		}},
	}); err != nil {
		t.Fatal(err)
	}
	diagnosticsMessage := client.waitForNotification("textDocument/publishDiagnostics", uri)
	diagnostics := diagnosticsFromPublishMessage(t, diagnosticsMessage)
	if !strings.Contains(mustJSONText(t, diagnostics), "helperThing") {
		t.Fatalf("JavaScript auto-import diagnostic missing helperThing: %s", mustJSONText(t, diagnostics))
	}
	actions := client.request("textDocument/codeAction", map[string]any{
		"textDocument": map[string]any{"uri": uri},
		"range": map[string]any{
			"start": callDocument.Position,
			"end":   callDocument.Position,
		},
		"context": map[string]any{
			"diagnostics": diagnostics,
		},
	})
	actionsJSON := mustJSONText(t, actions.Result)
	if !strings.Contains(actionsJSON, "Add import") || !strings.Contains(actionsJSON, "./helpers") {
		t.Fatalf("JavaScript auto-import quick fix missing import action: %s", actionsJSON)
	}
}
