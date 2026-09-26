package lspserver

import (
	"os"
	"path/filepath"
	"strings"
	"testing"
)

func TestStdioParityUsesSymbolsFromDirectIncludesForVBScriptCompletionAndDefinition(t *testing.T) {
	client := startStdioTestClient(t)
	defer client.close()

	root := t.TempDir()
	owner := filepath.Join(root, "default.asp")
	include := filepath.Join(root, "includes", "common.inc")
	source := `<!-- #include file="includes/common.inc" -->
<%
Function LocalTitle(ByVal value)
LocalTitle = value
End Function
Response.Write LocalTitle("Ada")
Response.Write SharedTitle()
%>`
	if err := os.MkdirAll(filepath.Dir(include), 0o755); err != nil {
		t.Fatal(err)
	}
	if err := os.WriteFile(include, []byte(`<%
Function SharedTitle()
End Function
%>`), 0o644); err != nil {
		t.Fatal(err)
	}
	if err := os.WriteFile(owner, []byte(source), 0o644); err != nil {
		t.Fatal(err)
	}
	uri := pathToFileURI(owner)
	client.request("initialize", map[string]any{
		"processId":    nil,
		"rootUri":      pathToFileURI(root),
		"capabilities": map[string]any{},
	})
	diagnosticsMessage := openClassicASPDocumentWithDiagnostics(t, client, uri, source)
	if strings.Contains(mustJSONText(t, diagnosticsMessage.Params), "Missing include") {
		t.Fatalf("direct include unexpectedly reported missing include: %s", string(diagnosticsMessage.Params))
	}

	callPosition := positionAt(source, strings.LastIndex(source, "SharedTitle")+2)
	completions := completionItems(client.request("textDocument/completion", map[string]any{
		"textDocument": map[string]any{"uri": uri},
		"position":     callPosition,
	}).Result)
	sharedCompletion, ok := completions.find("SharedTitle")
	if !ok {
		t.Fatalf("included symbol completion missing SharedTitle")
	}
	resolved := mustJSONText(t, client.request("completionItem/resolve", sharedCompletion).Result)
	for _, expected := range []string{"Defined in [includes/common.inc]", pathToFileURI(include)} {
		if !strings.Contains(resolved, expected) {
			t.Fatalf("included symbol completion resolve missing %q: %s", expected, resolved)
		}
	}
	assertRequestContains(t, client, "textDocument/hover", map[string]any{
		"textDocument": map[string]any{"uri": uri},
		"position":     positionAt(source, strings.Index(source, "LocalTitle")+2),
	}, "Function LocalTitle(ByVal value)")
	assertRequestContains(t, client, "textDocument/signatureHelp", map[string]any{
		"textDocument": map[string]any{"uri": uri},
		"position":     positionAt(source, strings.Index(source, `"Ada"`)+1),
	}, "LocalTitle(ByVal value)")
	assertRequestContains(t, client, "textDocument/inlayHint", map[string]any{
		"textDocument": map[string]any{"uri": uri},
		"range": map[string]any{
			"start": map[string]any{"line": 0, "character": 0},
			"end":   map[string]any{"line": 8, "character": 0},
		},
	}, "value:")
	definition := mustJSONText(t, client.request("textDocument/definition", map[string]any{
		"textDocument": map[string]any{"uri": uri},
		"position":     callPosition,
	}).Result)
	if !strings.Contains(definition, pathToFileURI(include)) || !strings.Contains(definition, `"line":1`) {
		t.Fatalf("included symbol definition mismatch: %s", definition)
	}
	assertRequestContains(t, client, "textDocument/hover", map[string]any{
		"textDocument": map[string]any{"uri": uri},
		"position":     callPosition,
	}, "Function SharedTitle()")
	sharedHover := hoverMarkdownValue(t, client.request("textDocument/hover", map[string]any{
		"textDocument": map[string]any{"uri": uri},
		"position":     callPosition,
	}).Result)
	for _, expected := range []string{"Defined in [includes/common.inc]", pathToFileURI(include)} {
		if !strings.Contains(sharedHover, expected) {
			t.Fatalf("included symbol hover missing %q: %s", expected, sharedHover)
		}
	}
}

func TestStdioParityJumpsFromAssignmentsToIncludeDefinedImplicitGlobals(t *testing.T) {
	client := startStdioTestClient(t)
	defer client.close()

	root := t.TempDir()
	include := filepath.Join(root, "shared.inc")
	owner := filepath.Join(root, "default.asp")
	if err := os.WriteFile(include, []byte("<%\nsharedTitle = \"include\"\n%>"), 0o644); err != nil {
		t.Fatal(err)
	}
	source := `<!-- #include file="shared.inc" -->
<%
sharedTitle = "page"
Function Render()
  sharedTitle = "function"
End Function
Class Widget
  Public Sub Save()
    sharedTitle = "method"
  End Sub
End Class
%>`
	if err := os.WriteFile(owner, []byte(source), 0o644); err != nil {
		t.Fatal(err)
	}
	uri := pathToFileURI(owner)
	client.request("initialize", map[string]any{
		"processId":    nil,
		"rootUri":      pathToFileURI(root),
		"capabilities": map[string]any{},
	})
	notifyConfiguration(t, client, map[string]any{"aspLsp": map[string]any{
		"diagnostics": map[string]any{"debounceMs": 0},
	}})
	openClassicASPDocumentWithDiagnostics(t, client, uri, source)
	client.request("textDocument/diagnostic", map[string]any{
		"textDocument": map[string]any{"uri": uri},
	})

	assignmentOffsets := []int{
		strings.Index(source, "sharedTitle ="),
		strings.Index(source[strings.Index(source, "Function Render"):], "sharedTitle =") + strings.Index(source, "Function Render"),
		strings.LastIndex(source, "sharedTitle ="),
	}
	for _, offset := range assignmentOffsets {
		definition := mustJSONText(t, client.request("textDocument/definition", map[string]any{
			"textDocument": map[string]any{"uri": uri},
			"position":     positionAt(source, offset),
		}).Result)
		if !strings.Contains(definition, "shared.inc") || strings.Contains(definition, "default.asp") {
			t.Fatalf("implicit global assignment definition mismatch: %s", definition)
		}
	}

	hints := mustJSONText(t, client.request("textDocument/inlayHint", map[string]any{
		"textDocument": map[string]any{"uri": uri},
		"range": map[string]any{
			"start": map[string]any{"line": 0, "character": 0},
			"end":   positionAt(source, len(source)),
		},
	}).Result)
	if strings.Contains(hints, "(global) As String") || strings.Contains(hints, "(local) As String") {
		t.Fatalf("include-defined implicit global hints mismatch: %s", hints)
	}
}

func TestStdioParityDoesNotAddFallbackVBScriptSemanticTokensInsideStringsOrComments(t *testing.T) {
	client := startStdioTestClient(t)
	defer client.close()

	root := t.TempDir()
	owner := filepath.Join(root, "default.asp")
	include := filepath.Join(root, "common.inc")
	if err := os.WriteFile(include, []byte(`<%
Function SharedTitle()
End Function
%>`), 0o644); err != nil {
		t.Fatal(err)
	}
	source := `<!-- #include file="common.inc" -->
<%
Response.Write "SharedTitle"
' SharedTitle should stay a comment
Rem SharedTitle should stay a comment
Response.Write SharedTitle()
%>`
	if err := os.WriteFile(owner, []byte(source), 0o644); err != nil {
		t.Fatal(err)
	}
	uri := pathToFileURI(owner)
	client.request("initialize", map[string]any{
		"processId":    nil,
		"rootUri":      pathToFileURI(root),
		"capabilities": map[string]any{},
	})
	openClassicASPDocumentWithDiagnostics(t, client, uri, source)
	if !completionLabels(client.request("textDocument/completion", map[string]any{
		"textDocument": map[string]any{"uri": uri},
		"position":     positionAt(source, strings.LastIndex(source, "SharedTitle")+len("Shared")),
	}).Result).contains("SharedTitle") {
		t.Fatalf("included symbol completion missing SharedTitle")
	}

	semanticTokens := client.request("textDocument/semanticTokens/full", map[string]any{
		"textDocument": map[string]any{"uri": uri},
	})
	decoded := decodeSemanticTokens(t, semanticTokens.Result)
	hasFunctionTokenAt := func(offset int) bool {
		position := positionAt(source, offset)
		for _, token := range decoded {
			if token.Line == position["line"] && token.Character == position["character"] && token.TokenType == semanticTokenFunction {
				return true
			}
		}
		return false
	}
	if hasFunctionTokenAt(strings.Index(source, "SharedTitle")) {
		t.Fatalf("semantic tokens included fallback function token inside string: %#v", decoded)
	}
	if hasFunctionTokenAt(strings.Index(source, "SharedTitle should stay a comment")) {
		t.Fatalf("semantic tokens included fallback function token inside apostrophe comment: %#v", decoded)
	}
	if hasFunctionTokenAt(strings.Index(source[strings.Index(source, "Rem "):], "SharedTitle") + strings.Index(source, "Rem ")) {
		t.Fatalf("semantic tokens included fallback function token inside Rem comment: %#v", decoded)
	}
	if !hasFunctionTokenAt(strings.LastIndex(source, "SharedTitle")) {
		t.Fatalf("semantic tokens missing included function usage token: %#v", decoded)
	}
}
