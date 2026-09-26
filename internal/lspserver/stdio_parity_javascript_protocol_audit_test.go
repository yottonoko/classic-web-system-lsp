package lspserver

import (
	"os"
	"path/filepath"
	"strings"
	"testing"
)

func TestStdioParityAuditsJavaScriptLanguageFeaturesOverJSONRPC(t *testing.T) {
	client := startStdioTestClient(t)
	defer client.close()

	root := t.TempDir()
	helperPath := filepath.Join(root, "helpers.js")
	helperSource := `/** @param {string} name */
export function greet(name) {
  return name.toUpperCase();
}
`
	if err := os.WriteFile(helperPath, []byte(helperSource), 0o644); err != nil {
		t.Fatal(err)
	}
	uri := pathToFileURI(filepath.Join(root, "protocol-audit.asp"))
	source := `<script type="module">
import { greet } from "./helpers.js";
const localResult = greet("Ada");
localResult.toUpp
greet(42);
greet("Grace");
</script>`

	client.request("initialize", map[string]any{
		"processId":    nil,
		"rootUri":      pathToFileURI(root),
		"capabilities": map[string]any{},
	})
	notifyConfiguration(t, client, map[string]any{"aspLsp": map[string]any{
		"checkJs":     true,
		"diagnostics": map[string]any{"debounceMs": 0},
	}})
	diagnosticsMessage := openClassicASPDocumentWithDiagnostics(t, client, uri, source)

	completionPosition := positionAt(source, strings.Index(source, "toUpp")+len("toUpp"))
	completion := client.request("textDocument/completion", map[string]any{
		"textDocument": map[string]any{"uri": uri},
		"position":     completionPosition,
	})
	auditJavaScriptResponseContains(t, "completion", completion.Result, "toUpperCase")

	callOffset := strings.Index(source, `greet("Ada")`)
	callPosition := positionAt(source, callOffset+len("gre"))
	hover := client.request("textDocument/hover", map[string]any{
		"textDocument": map[string]any{"uri": uri},
		"position":     callPosition,
	})
	auditJavaScriptResponseContains(t, "hover", hover.Result, "greet")

	signature := client.request("textDocument/signatureHelp", map[string]any{
		"textDocument": map[string]any{"uri": uri},
		"position":     positionAt(source, callOffset+len(`greet("`)),
	})
	auditJavaScriptResponseContains(t, "signatureHelp", signature.Result, "name")

	definition := client.request("textDocument/definition", map[string]any{
		"textDocument": map[string]any{"uri": uri},
		"position":     callPosition,
	})
	definitionJSON := mustJSONText(t, definition.Result)
	if !strings.Contains(definitionJSON, "helpers.js") || !strings.Contains(definitionJSON, `"line":1`) {
		t.Fatalf("JavaScript definition did not cross the import boundary: %s", definitionJSON)
	}

	references := client.request("textDocument/references", map[string]any{
		"textDocument": map[string]any{"uri": uri},
		"position":     callPosition,
		"context":      map[string]any{"includeDeclaration": true},
	})
	if got := resultArrayLength(t, references.Result); got < 4 {
		t.Fatalf("JavaScript references = %d, want at least 4 import/call sites: %s", got, mustJSONText(t, references.Result))
	}

	localOffset := strings.Index(source, "localResult")
	localPosition := positionAt(source, localOffset+len("local"))
	prepareRename := client.request("textDocument/prepareRename", map[string]any{
		"textDocument": map[string]any{"uri": uri},
		"position":     localPosition,
	})
	auditJavaScriptResponseContains(t, "prepareRename", prepareRename.Result, `"line":2`)

	rename := client.request("textDocument/rename", map[string]any{
		"textDocument": map[string]any{"uri": uri},
		"position":     localPosition,
		"newName":      "formattedResult",
	})
	if got := strings.Count(mustJSONText(t, rename.Result), `"newText":"formattedResult"`); got != 2 {
		t.Fatalf("JavaScript rename edit count = %d, want 2: %s", got, mustJSONText(t, rename.Result))
	}

	symbols := client.request("textDocument/documentSymbol", map[string]any{
		"textDocument": map[string]any{"uri": uri},
	})
	auditJavaScriptResponseContains(t, "documentSymbol", symbols.Result, "localResult")

	highlights := client.request("textDocument/documentHighlight", map[string]any{
		"textDocument": map[string]any{"uri": uri},
		"position":     localPosition,
	})
	if got := resultArrayLength(t, highlights.Result); got != 2 {
		t.Fatalf("JavaScript document highlights = %d, want 2: %s", got, mustJSONText(t, highlights.Result))
	}

	publishedDiagnostics := diagnosticsFromPublishMessage(t, diagnosticsMessage)
	auditJavaScriptResponseContains(t, "publishDiagnostics", publishedDiagnostics, "number")
	pulledDiagnostics := client.request("textDocument/diagnostic", map[string]any{
		"textDocument": map[string]any{"uri": uri},
	})
	pulledJSON := mustJSONText(t, pulledDiagnostics.Result)
	if !strings.Contains(pulledJSON, "number") || !strings.Contains(pulledJSON, "asp-lsp-typescript") {
		t.Fatalf("JavaScript pull diagnostics missing TypeScript argument error: %s", pulledJSON)
	}

	client.drainNotifications("textDocument/publishDiagnostics")
	updatedSource := `<script type="module">
import { greet } from "./helpers.js";
const freshResult = greet("Bea");
freshResult.toLow
greet(false);
greet("June");
</script>`
	if err := client.notify("textDocument/didChange", map[string]any{
		"textDocument":   map[string]any{"uri": uri, "version": 2},
		"contentChanges": []map[string]any{{"text": updatedSource}},
	}); err != nil {
		t.Fatal(err)
	}
	updatedDiagnosticsMessage := waitForDiagnosticsContaining(t, client, "boolean")

	updatedCompletion := client.request("textDocument/completion", map[string]any{
		"textDocument": map[string]any{"uri": uri},
		"position":     positionAt(updatedSource, strings.Index(updatedSource, "toLow")+len("toLow")),
	})
	updatedCompletionJSON := mustJSONText(t, updatedCompletion.Result)
	if !strings.Contains(updatedCompletionJSON, "toLowerCase") || strings.Contains(updatedCompletionJSON, "localResult") {
		t.Fatalf("JavaScript completion after didChange is stale: %s", updatedCompletionJSON)
	}

	freshOffset := strings.Index(updatedSource, "freshResult")
	freshPosition := positionAt(updatedSource, freshOffset+len("fresh"))
	updatedHover := client.request("textDocument/hover", map[string]any{
		"textDocument": map[string]any{"uri": uri},
		"position":     freshPosition,
	})
	updatedHoverJSON := mustJSONText(t, updatedHover.Result)
	if !strings.Contains(updatedHoverJSON, "freshResult") || strings.Contains(updatedHoverJSON, "localResult") {
		t.Fatalf("JavaScript hover after didChange is stale: %s", updatedHoverJSON)
	}

	updatedCallOffset := strings.Index(updatedSource, `greet("Bea")`)
	updatedCallPosition := positionAt(updatedSource, updatedCallOffset+len("gre"))
	updatedDefinition := client.request("textDocument/definition", map[string]any{
		"textDocument": map[string]any{"uri": uri},
		"position":     updatedCallPosition,
	})
	updatedDefinitionJSON := mustJSONText(t, updatedDefinition.Result)
	if !strings.Contains(updatedDefinitionJSON, "helpers.js") || !strings.Contains(updatedDefinitionJSON, `"line":1`) {
		t.Fatalf("JavaScript definition after didChange lost the import target: %s", updatedDefinitionJSON)
	}

	updatedReferences := client.request("textDocument/references", map[string]any{
		"textDocument": map[string]any{"uri": uri},
		"position":     updatedCallPosition,
		"context":      map[string]any{"includeDeclaration": true},
	})
	if got := resultArrayLength(t, updatedReferences.Result); got < 4 {
		t.Fatalf("JavaScript references after didChange = %d, want at least 4 fresh import/call sites: %s", got, mustJSONText(t, updatedReferences.Result))
	}

	updatedRename := client.request("textDocument/rename", map[string]any{
		"textDocument": map[string]any{"uri": uri},
		"position":     freshPosition,
		"newName":      "latestResult",
	})
	updatedRenameJSON := mustJSONText(t, updatedRename.Result)
	if got := strings.Count(updatedRenameJSON, `"newText":"latestResult"`); got != 2 || strings.Contains(updatedRenameJSON, "localResult") {
		t.Fatalf("JavaScript rename after didChange is stale: edits=%d response=%s", got, updatedRenameJSON)
	}

	updatedSymbols := client.request("textDocument/documentSymbol", map[string]any{
		"textDocument": map[string]any{"uri": uri},
	})
	updatedSymbolsJSON := mustJSONText(t, updatedSymbols.Result)
	if !strings.Contains(updatedSymbolsJSON, "freshResult") || strings.Contains(updatedSymbolsJSON, "localResult") {
		t.Fatalf("JavaScript document symbols after didChange are stale: %s", updatedSymbolsJSON)
	}

	updatedPublishedDiagnostics := mustJSONText(t, diagnosticsFromPublishMessage(t, updatedDiagnosticsMessage))
	if !strings.Contains(updatedPublishedDiagnostics, "boolean") || strings.Contains(updatedPublishedDiagnostics, "number") {
		t.Fatalf("JavaScript publish diagnostics after didChange are stale: %s", updatedPublishedDiagnostics)
	}
	updatedPulledDiagnostics := mustJSONText(t, client.request("textDocument/diagnostic", map[string]any{
		"textDocument": map[string]any{"uri": uri},
	}).Result)
	if !strings.Contains(updatedPulledDiagnostics, "boolean") || strings.Contains(updatedPulledDiagnostics, "number") || !strings.Contains(updatedPulledDiagnostics, "asp-lsp-typescript") {
		t.Fatalf("JavaScript pull diagnostics after didChange are stale: %s", updatedPulledDiagnostics)
	}
}

func auditJavaScriptResponseContains(t *testing.T, method string, result any, expected string) {
	t.Helper()
	serialized := mustJSONText(t, result)
	if !strings.Contains(serialized, expected) {
		t.Fatalf("JavaScript %s response missing %q: %s", method, expected, serialized)
	}
}
