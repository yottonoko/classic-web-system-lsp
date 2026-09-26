package lspserver

import (
	"path/filepath"
	"strings"
	"testing"

	"github.com/yottonoko/classic-web-system-lsp/internal/lsp"
)

func TestStdioParityReturnsCSSQuickFixesForStyleAttributesOutsideHTMLRoot(t *testing.T) {
	client := startStdioTestClient(t)
	defer client.close()

	root := t.TempDir()
	uri := pathToFileURI(filepath.Join(root, "go-css-code-action.asp"))
	source := `<section style="colr: red; background: #ff0000">x</section>`
	typoRange := mapPositionRange(
		positionAt(source, strings.Index(source, "colr")),
		positionAt(source, strings.Index(source, "colr")+len("colr")),
	)
	client.request("initialize", map[string]any{
		"processId":    nil,
		"rootUri":      pathToFileURI(root),
		"capabilities": map[string]any{},
	})
	openClassicASPDocument(t, client, uri, source)

	actions := client.request("textDocument/codeAction", map[string]any{
		"textDocument": map[string]any{"uri": uri},
		"range":        typoRange,
		"context": map[string]any{
			"diagnostics": []diagnosticResult{{
				Range:   typoRange,
				Message: "Unknown property: colr",
				Source:  "asp-lsp-css",
				Code:    "unknownProperties",
			}},
			"only": []string{"quickfix"},
		},
	})
	serialized := mustJSONText(t, actions.Result)
	if !strings.Contains(serialized, "Rename to 'color'") || !strings.Contains(serialized, `"newText":"color"`) {
		t.Fatalf("CSS quick fix missing rename edit: %s", serialized)
	}
	if strings.Contains(serialized, ".css.virtual") {
		t.Fatalf("CSS quick fix leaked virtual URI: %s", serialized)
	}

	outsideRootSource := `<html><body></body></html>
<section style="colr: red; background: #ff0000">x</section>`
	outsideRootURI := pathToFileURI(filepath.Join(root, "go-css-code-action-outside-root.asp"))
	notifyOpenClassicASPDocument(t, client, outsideRootURI, outsideRootSource)
	outsideDiagnostics := waitForDiagnosticsContaining(t, client, "Unknown property")
	outsideDiagnostic := diagnosticContaining(t, outsideDiagnostics, "Unknown property")
	if outsideDiagnostic == nil {
		t.Fatalf("outside-root CSS diagnostic missing: %s", string(outsideDiagnostics.Params))
	}
	expectedPosition := positionAt(outsideRootSource, strings.Index(outsideRootSource, "colr"))
	if outsideDiagnostic.Range.Start.Line != expectedPosition["line"] || outsideDiagnostic.Range.Start.Character != expectedPosition["character"] {
		t.Fatalf("outside-root CSS diagnostic range = %#v, want %#v", outsideDiagnostic.Range.Start, expectedPosition)
	}
	outsideActions := client.request("textDocument/codeAction", map[string]any{
		"textDocument": map[string]any{"uri": outsideRootURI},
		"range":        outsideDiagnostic.Range,
		"context": map[string]any{
			"diagnostics": []diagnosticResult{*outsideDiagnostic},
			"only":        []string{"quickfix"},
		},
	})
	outsideSerialized := mustJSONText(t, outsideActions.Result)
	if !strings.Contains(outsideSerialized, "Rename to 'color'") || !strings.Contains(outsideSerialized, `"newText":"color"`) {
		t.Fatalf("outside-root CSS quick fix missing rename edit: %s", outsideSerialized)
	}
	if strings.Contains(outsideSerialized, ".css.virtual") {
		t.Fatalf("outside-root CSS quick fix leaked virtual URI: %s", outsideSerialized)
	}
}

func TestStdioParityReturnsAllMappedCSSLanguageServiceCodeActions(t *testing.T) {
	client := startStdioTestClient(t)
	defer client.close()

	root := t.TempDir()
	uri := pathToFileURI(filepath.Join(root, "go-css-multiple-code-actions.asp"))
	source := `<style>
.card { background-colar: coral; }
</style>`
	typoRange := mapPositionRange(
		positionAt(source, strings.Index(source, "background-colar")),
		positionAt(source, strings.Index(source, "background-colar")+len("background-colar")),
	)
	client.request("initialize", map[string]any{
		"processId":    nil,
		"rootUri":      pathToFileURI(root),
		"capabilities": map[string]any{},
	})
	openClassicASPDocument(t, client, uri, source)
	actions := client.request("textDocument/codeAction", map[string]any{
		"textDocument": map[string]any{"uri": uri},
		"range":        typoRange,
		"context": map[string]any{
			"diagnostics": []diagnosticResult{{
				Range:   typoRange,
				Message: "Unknown property: background-colar",
				Source:  "asp-lsp-css",
				Code:    "unknownProperties",
			}},
			"only": []string{"quickfix"},
		},
	})
	serialized := mustJSONText(t, actions.Result)
	for _, expected := range []string{"Rename to 'background-color'", "Rename to 'background-clip'", `"uri":"` + uri + `"`} {
		if !strings.Contains(serialized, expected) {
			t.Fatalf("CSS code actions missing %q: %s", expected, serialized)
		}
	}
	if strings.Contains(serialized, ".css.virtual") {
		t.Fatalf("CSS code actions leaked virtual URI: %s", serialized)
	}

	sourceOnly := client.request("textDocument/codeAction", map[string]any{
		"textDocument": map[string]any{"uri": uri},
		"range":        typoRange,
		"context": map[string]any{
			"diagnostics": []diagnosticResult{{Range: typoRange, Message: "Unknown property", Source: "asp-lsp-css", Code: "unknownProperties"}},
			"only":        []string{"source"},
		},
	})
	if serialized := mustJSONText(t, sourceOnly.Result); strings.Contains(serialized, "Rename to '") {
		t.Fatalf("source-only request returned CSS quick fixes: %s", serialized)
	}
}

func TestStdioParityReturnsExecutableCreateFileEditForMissingIncludes(t *testing.T) {
	client := startStdioTestClient(t)
	defer client.close()

	root := t.TempDir()
	uri := pathToFileURI(filepath.Join(root, "default.asp"))
	source := `<!-- #include file="missing.inc" -->
<% Response.Write "ok" %>`
	includeRange := mapPositionRange(
		positionAt(source, strings.Index(source, "missing.inc")),
		positionAt(source, strings.Index(source, "missing.inc")+len("missing.inc")),
	)
	client.request("initialize", map[string]any{
		"processId":    nil,
		"rootUri":      pathToFileURI(root),
		"capabilities": map[string]any{},
	})
	notifyOpenClassicASPDocument(t, client, uri, source)
	diagnostics := waitForDiagnosticsContaining(t, client, "Include file 'missing.inc' could not be resolved.")
	if !strings.Contains(string(diagnostics.Params), "Include file 'missing.inc' could not be resolved.") {
		t.Fatalf("missing include diagnostic mismatch: %s", string(diagnostics.Params))
	}

	actions := client.request("textDocument/codeAction", map[string]any{
		"textDocument": map[string]any{"uri": uri},
		"range":        includeRange,
		"context": map[string]any{
			"diagnostics": []diagnosticResult{{
				Range:   includeRange,
				Message: "Missing include: missing.inc",
				Source:  "asp-lsp-include",
			}},
			"only": []string{"quickfix"},
		},
	})
	serialized := mustJSONText(t, actions.Result)
	if !strings.Contains(serialized, "Create missing include missing.inc") ||
		!strings.Contains(serialized, `"kind":"create"`) ||
		!strings.Contains(serialized, pathToFileURI(filepath.Join(root, "missing.inc"))) {
		t.Fatalf("missing include create-file action mismatch: %s", serialized)
	}
}

func TestStdioParityReturnsCSSAndJavaScriptSourceCodeActions(t *testing.T) {
	client := startStdioTestClient(t)
	defer client.close()

	root := t.TempDir()
	uri := pathToFileURI(filepath.Join(root, "go-code-actions.asp"))
	source := `<style>.x { color: #ff0000; }</style>
<script>
import { z } from "z";
import { a } from "a";
console.log(z, a);
</script>`
	client.request("initialize", map[string]any{
		"processId":    nil,
		"rootUri":      pathToFileURI(root),
		"capabilities": map[string]any{},
	})
	openClassicASPDocumentWithDiagnostics(t, client, uri, source)

	response := client.request("textDocument/codeAction", map[string]any{
		"textDocument": map[string]any{"uri": uri},
		"range": lsp.Range{
			Start: lsp.Position{Line: 1, Character: 0},
			End:   lsp.Position{Line: 5, Character: 0},
		},
		"context": map[string]any{
			"diagnostics": []diagnosticResult{},
			"only":        []string{"source.organizeImports"},
		},
	})
	var actions []lsp.CodeAction
	mustDecodeResult(t, response.Result, &actions)
	organize := codeActionByTitle(actions, "Organize JavaScript imports")
	if organize == nil {
		t.Fatalf("JavaScript organize imports action missing: %s", mustJSONText(t, response.Result))
	}
	newText := ""
	if organize.Edit != nil && len(organize.Edit.Changes[uri]) > 0 {
		newText = organize.Edit.Changes[uri][0].NewText
	}
	if !strings.Contains(newText, `import { a } from "a";`) || !strings.Contains(newText, `import { z } from "z";`) {
		t.Fatalf("JavaScript organize imports edit missing imports: %s", mustJSONText(t, organize))
	}
	if strings.Index(newText, `import { a } from "a";`) > strings.Index(newText, `import { z } from "z";`) {
		t.Fatalf("JavaScript organize imports edit did not sort imports: %q", newText)
	}
}

func TestStdioParityReturnsVBScriptQuickFixForInitializedDimDeclarations(t *testing.T) {
	client := startStdioTestClient(t)
	defer client.close()

	root := t.TempDir()
	initializedURI := pathToFileURI(filepath.Join(root, "go-vb-split-dim.asp"))
	initializedSource := `<%
Dim value = 1
Response.Write value
%>`
	multiURI := pathToFileURI(filepath.Join(root, "go-vb-split-multi-dim.asp"))
	multiSource := `<%
Dim first, items(1, 2), dynamicItems()
Response.Write first
%>`
	client.request("initialize", map[string]any{
		"processId":    nil,
		"rootUri":      pathToFileURI(root),
		"capabilities": map[string]any{},
	})
	notifyOpenClassicASPDocument(t, client, initializedURI, initializedSource)
	diagnostics := waitForDiagnosticsContaining(t, client, "initializers")
	syntaxDiagnostics := diagnosticsFromSource(t, diagnostics, "asp-lsp-vbscript-syntax")
	syntaxText := mustJSONText(t, syntaxDiagnostics)
	if !strings.Contains(syntaxText, "initializers") || !strings.Contains(string(diagnostics.Params), "initializedDeclaration") {
		t.Fatalf("initialized Dim syntax diagnostics mismatch: extracted=%s full=%s", syntaxText, string(diagnostics.Params))
	}

	initializedRange := mapPositionRange(
		positionAt(initializedSource, strings.Index(initializedSource, "value")),
		positionAt(initializedSource, strings.Index(initializedSource, "value")+len("value")),
	)
	initializedActions := client.request("textDocument/codeAction", map[string]any{
		"textDocument": map[string]any{"uri": initializedURI},
		"range":        initializedRange,
		"context": map[string]any{
			"diagnostics": syntaxDiagnostics,
			"only":        []string{"quickfix"},
		},
	})
	initializedSerialized := mustJSONText(t, initializedActions.Result)
	if !strings.Contains(initializedSerialized, "Split initialized Dim declaration") ||
		!strings.Contains(initializedSerialized, `Dim value : value = 1`) ||
		strings.Contains(initializedSerialized, `Dim value\nvalue = 1`) ||
		resultArrayLength(t, initializedActions.Result) != 1 {
		t.Fatalf("initialized Dim quick fix mismatch: %s", initializedSerialized)
	}
	fixedURI := pathToFileURI(filepath.Join(root, "go-vb-split-dim-fixed.asp"))
	fixedDiagnostics := openClassicASPDocumentWithDiagnostics(t, client, fixedURI, `<%
Dim value : value = 1
Response.Write value
%>`)
	if strings.Contains(string(fixedDiagnostics.Params), "initializedDeclaration") {
		t.Fatalf("fixed initialized Dim source still reported initializer diagnostic: %s", string(fixedDiagnostics.Params))
	}

	notifyConfiguration(t, client, map[string]any{"aspLsp": map[string]any{"vbscript": map[string]any{"initializedDimQuickFixStyle": "newline"}}})
	newlineActions := client.request("textDocument/codeAction", map[string]any{
		"textDocument": map[string]any{"uri": initializedURI},
		"range":        initializedRange,
		"context": map[string]any{
			"diagnostics": syntaxDiagnostics,
			"only":        []string{"quickfix"},
		},
	})
	newlineSerialized := mustJSONText(t, newlineActions.Result)
	if !strings.Contains(newlineSerialized, `Dim value\nvalue = 1`) || strings.Contains(newlineSerialized, `Dim value : value = 1`) {
		t.Fatalf("newline initialized Dim quick fix mismatch: %s", newlineSerialized)
	}

	notifyOpenClassicASPDocument(t, client, multiURI, multiSource)
	multiRange := mapPositionRange(
		positionAt(multiSource, strings.Index(multiSource, "items")),
		positionAt(multiSource, strings.Index(multiSource, "items")+len("items")),
	)
	multiActions := client.request("textDocument/codeAction", map[string]any{
		"textDocument": map[string]any{"uri": multiURI},
		"range":        multiRange,
		"context": map[string]any{
			"diagnostics": []diagnosticResult{},
			"only":        []string{"quickfix"},
		},
	})
	multiSerialized := mustJSONText(t, multiActions.Result)
	if !strings.Contains(multiSerialized, "Split Dim declarations") ||
		!strings.Contains(multiSerialized, `Dim first\nDim items(1, 2)\nDim dynamicItems()`) {
		t.Fatalf("multi-name Dim quick fix mismatch: %s", multiSerialized)
	}
}

func TestStdioParityReturnsVBScriptQuickFixForMultiNameDimDeclarations(t *testing.T) {
	client := startStdioTestClient(t)
	defer client.close()

	root := t.TempDir()
	source := `<%
Dim first, second
Response.Write first
%>`
	arraySource := `<%
Dim first, items(1, 2), dynamicItems()
Response.Write first
%>`
	unsupportedSource := `<%
Dim single
ReDim first, second
%>`
	client.request("initialize", map[string]any{
		"processId":    nil,
		"rootUri":      pathToFileURI(root),
		"capabilities": map[string]any{},
	})
	uri := pathToFileURI(filepath.Join(root, "go-vb-split-multi-dim-focused.asp"))
	openClassicASPDocument(t, client, uri, source)
	actions := client.request("textDocument/codeAction", map[string]any{
		"textDocument": map[string]any{"uri": uri},
		"range": mapPositionRange(
			positionAt(source, strings.Index(source, "first")),
			positionAt(source, strings.Index(source, "first")+len("first")),
		),
		"context": map[string]any{"diagnostics": []diagnosticResult{}, "only": []string{"quickfix"}},
	})
	serialized := mustJSONText(t, actions.Result)
	if !strings.Contains(serialized, "Split Dim declarations") || !strings.Contains(serialized, `Dim first\nDim second`) {
		t.Fatalf("multi-name Dim quick fix mismatch: %s", serialized)
	}

	arrayURI := pathToFileURI(filepath.Join(root, "go-vb-split-multi-array-dim-focused.asp"))
	openClassicASPDocument(t, client, arrayURI, arraySource)
	arrayActions := client.request("textDocument/codeAction", map[string]any{
		"textDocument": map[string]any{"uri": arrayURI},
		"range": mapPositionRange(
			positionAt(arraySource, strings.Index(arraySource, "items")),
			positionAt(arraySource, strings.Index(arraySource, "items")+len("items")),
		),
		"context": map[string]any{"diagnostics": []diagnosticResult{}, "only": []string{"quickfix"}},
	})
	if !strings.Contains(mustJSONText(t, arrayActions.Result), `Dim first\nDim items(1, 2)\nDim dynamicItems()`) {
		t.Fatalf("array Dim quick fix mismatch: %s", mustJSONText(t, arrayActions.Result))
	}

	unsupportedURI := pathToFileURI(filepath.Join(root, "go-vb-split-multi-dim-unsupported-focused.asp"))
	openClassicASPDocument(t, client, unsupportedURI, unsupportedSource)
	singleActions := client.request("textDocument/codeAction", map[string]any{
		"textDocument": map[string]any{"uri": unsupportedURI},
		"range": mapPositionRange(
			positionAt(unsupportedSource, strings.Index(unsupportedSource, "single")),
			positionAt(unsupportedSource, strings.Index(unsupportedSource, "single")+len("single")),
		),
		"context": map[string]any{"diagnostics": []diagnosticResult{}, "only": []string{"quickfix"}},
	})
	redimActions := client.request("textDocument/codeAction", map[string]any{
		"textDocument": map[string]any{"uri": unsupportedURI},
		"range": mapPositionRange(
			positionAt(unsupportedSource, strings.Index(unsupportedSource, "first")),
			positionAt(unsupportedSource, strings.Index(unsupportedSource, "first")+len("first")),
		),
		"context": map[string]any{"diagnostics": []diagnosticResult{}, "only": []string{"quickfix"}},
	})
	if strings.Contains(mustJSONText(t, singleActions.Result), "Split Dim declarations") ||
		strings.Contains(mustJSONText(t, redimActions.Result), "Split Dim declarations") {
		t.Fatalf("unsupported Dim quick fixes should stay empty: single=%s redim=%s", mustJSONText(t, singleActions.Result), mustJSONText(t, redimActions.Result))
	}
}

func TestStdioParityReturnsVBScriptDocumentationGenerationQuickFixWithoutDiagnostics(t *testing.T) {
	client := startStdioTestClient(t)
	defer client.close()

	root := t.TempDir()
	uri := pathToFileURI(filepath.Join(root, "go-vb-doc-action.asp"))
	source := `<%
Function BuildName(first)
  BuildName = first
End Function
%>`
	client.request("initialize", map[string]any{
		"processId":    nil,
		"rootUri":      pathToFileURI(root),
		"capabilities": map[string]any{},
	})
	openClassicASPDocument(t, client, uri, source)
	actions := client.request("textDocument/codeAction", map[string]any{
		"textDocument": map[string]any{"uri": uri},
		"range": mapPositionRange(
			positionAt(source, strings.Index(source, "BuildName")),
			positionAt(source, strings.Index(source, "BuildName")),
		),
		"context": map[string]any{"diagnostics": []diagnosticResult{}, "only": []string{"quickfix"}},
	})
	serialized := mustJSONText(t, actions.Result)
	for _, expected := range []string{
		"Generate VBScript documentation",
		"' @param BuildName.first As Variant",
		"' @returns BuildName Variant",
		`''' \u003csummary\u003eTODO: Describe BuildName.\u003c/summary\u003e`,
		`''' \u003cparam name=\"first\"\u003eTODO: Describe first.\u003c/param\u003e`,
		`''' \u003creturns\u003eTODO: Describe return value.\u003c/returns\u003e`,
	} {
		if !strings.Contains(serialized, expected) {
			t.Fatalf("documentation quick fix missing %q: %s", expected, serialized)
		}
	}
}

func TestStdioParityAddsOnlyMissingVBScriptDocumentationItemsToExistingSummaryBlock(t *testing.T) {
	client := startStdioTestClient(t)
	defer client.close()

	root := t.TempDir()
	uri := pathToFileURI(filepath.Join(root, "go-vb-doc-existing-summary.asp"))
	source := `<%
''' <summary>Builds a display name.</summary>
Function BuildName(first)
  BuildName = first
End Function
%>`
	client.request("initialize", map[string]any{
		"processId":    nil,
		"rootUri":      pathToFileURI(root),
		"capabilities": map[string]any{},
	})
	openClassicASPDocument(t, client, uri, source)
	actionsResponse := client.request("textDocument/codeAction", map[string]any{
		"textDocument": map[string]any{"uri": uri},
		"range": mapPositionRange(
			positionAt(source, strings.Index(source, "BuildName(first)")),
			positionAt(source, strings.Index(source, "BuildName(first)")),
		),
		"context": map[string]any{"diagnostics": []diagnosticResult{}, "only": []string{"quickfix"}},
	})
	var actions []lsp.CodeAction
	mustDecodeResult(t, actionsResponse.Result, &actions)
	action := codeActionByTitle(actions, "Generate VBScript documentation")
	if action == nil {
		t.Fatalf("documentation quick fix missing: %s", mustJSONText(t, actionsResponse.Result))
	}
	updated := applyWorkspaceEditForURI(t, source, uri, action.Edit)
	if got := strings.Count(updated, "<summary>"); got != 1 {
		t.Fatalf("summary duplicated %d times: %s", got, updated)
	}
	for _, expected := range []string{
		"''' <summary>Builds a display name.</summary>",
		"' @param BuildName.first As Variant",
		"' @returns BuildName Variant",
		`''' <param name="first">TODO: Describe first.</param>`,
		"''' <returns>TODO: Describe return value.</returns>",
	} {
		if !strings.Contains(updated, expected) {
			t.Fatalf("updated documentation missing %q: %s", expected, updated)
		}
	}
}

func TestStdioParityGeneratesVBScriptDocumentationForBroadDeclarationSymbolKinds(t *testing.T) {
	cases := []struct {
		name     string
		source   string
		expected []string
	}{
		{
			name:   "variable",
			source: "<%\nDim <<<caret>>>customerName\n%>",
			expected: []string{
				"' @type customerName As Variant",
				"TODO: Describe customerName.",
				"<value>",
			},
		},
		{
			name:   "constant",
			source: "<%\nConst <<<caret>>>MaxItems = 10\n%>",
			expected: []string{
				"' @type MaxItems As Number",
				"TODO: Describe MaxItems.",
				"<value>",
			},
		},
		{
			name:   "class",
			source: "<%\nClass <<<caret>>>Customer\nEnd Class\n%>",
			expected: []string{
				"<summary>TODO: Describe Customer.</summary>",
			},
		},
		{
			name:   "field",
			source: "<%\nClass Customer\n  Public <<<caret>>>Name\nEnd Class\n%>",
			expected: []string{
				"' @type Name As Variant",
				"TODO: Describe Name.",
				"<value>",
			},
		},
		{
			name: "property",
			source: `<%
Class Customer
  Public Property Get <<<caret>>>Name()
    Name = mName
  End Property
End Class
%>`,
			expected: []string{
				"' @returns Name Variant",
				"TODO: Describe Name.",
				"<returns>",
				"<value>",
			},
		},
		{
			name:   "parameter",
			source: "<%\nSub Save(<<<caret>>>force)\nEnd Sub\n%>",
			expected: []string{
				"' @param Save.force As Variant",
				`<param name="force">`,
			},
		},
	}
	for _, testCase := range cases {
		t.Run(testCase.name, func(t *testing.T) {
			client := startStdioTestClient(t)
			defer client.close()

			root := t.TempDir()
			uri := pathToFileURI(filepath.Join(root, "go-vb-doc-"+testCase.name+".asp"))
			marked := markedDocument(testCase.source)
			client.request("initialize", map[string]any{
				"processId":    nil,
				"rootUri":      pathToFileURI(root),
				"capabilities": map[string]any{},
			})
			openClassicASPDocument(t, client, uri, marked.Text)
			actionsResponse := client.request("textDocument/codeAction", map[string]any{
				"textDocument": map[string]any{"uri": uri},
				"range":        mapPositionRange(marked.Position, marked.Position),
				"context":      map[string]any{"diagnostics": []diagnosticResult{}, "only": []string{"quickfix"}},
			})
			var actions []lsp.CodeAction
			mustDecodeResult(t, actionsResponse.Result, &actions)
			action := codeActionByTitle(actions, "Generate VBScript documentation")
			if action == nil {
				t.Fatalf("documentation quick fix missing: %s", mustJSONText(t, actionsResponse.Result))
			}
			updated := applyWorkspaceEditForURI(t, marked.Text, uri, action.Edit)
			for _, expected := range testCase.expected {
				if !strings.Contains(updated, expected) {
					t.Fatalf("%s documentation missing %q: %s", testCase.name, expected, updated)
				}
			}
		})
	}
}

func TestStdioParityAvoidsAmbiguousXMLDocumentationForMultiNameVBScriptDeclarations(t *testing.T) {
	client := startStdioTestClient(t)
	defer client.close()

	root := t.TempDir()
	uri := pathToFileURI(filepath.Join(root, "go-vb-doc-multi-name.asp"))
	marked := markedDocument("<%\nDim <<<caret>>>first, second\n%>")
	client.request("initialize", map[string]any{
		"processId":    nil,
		"rootUri":      pathToFileURI(root),
		"capabilities": map[string]any{},
	})
	openClassicASPDocument(t, client, uri, marked.Text)
	actionsResponse := client.request("textDocument/codeAction", map[string]any{
		"textDocument": map[string]any{"uri": uri},
		"range":        mapPositionRange(marked.Position, marked.Position),
		"context":      map[string]any{"diagnostics": []diagnosticResult{}, "only": []string{"quickfix"}},
	})
	var actions []lsp.CodeAction
	mustDecodeResult(t, actionsResponse.Result, &actions)
	action := codeActionByTitle(actions, "Generate VBScript documentation")
	if action == nil {
		t.Fatalf("documentation quick fix missing: %s", mustJSONText(t, actionsResponse.Result))
	}
	updated := applyWorkspaceEditForURI(t, marked.Text, uri, action.Edit)
	if !strings.Contains(updated, "' @type first As Variant") {
		t.Fatalf("multi-name declaration missing type annotation: %s", updated)
	}
	for _, unexpected := range []string{"''' <summary>", "''' <value>"} {
		if strings.Contains(updated, unexpected) {
			t.Fatalf("multi-name declaration included ambiguous XML docs %q: %s", unexpected, updated)
		}
	}
}

func TestStdioParityReturnsVBScriptQuickFixesForInvalidProcedureCallSyntax(t *testing.T) {
	client := startStdioTestClient(t)
	defer client.close()

	root := t.TempDir()
	uri := pathToFileURI(filepath.Join(root, "go-vb-call-syntax.asp"))
	source := `<%
Function Func1(hoge)
  Func1 = hoge
End Function
Sub Func2(hoge, fuga)
End Sub
Call Func1 hoge
Z = Func1 hoge
Func2(hoge, fuga)
Call Func2 hoge, fuga
Z = Func2 hoge, fuga
%>`
	client.request("initialize", map[string]any{
		"processId":    nil,
		"rootUri":      pathToFileURI(root),
		"capabilities": map[string]any{},
	})
	notifyOpenClassicASPDocument(t, client, uri, source)
	diagnostics := waitForDiagnosticsContaining(t, client, "call syntax")
	syntaxDiagnostics := diagnosticsFromSource(t, diagnostics, "asp-lsp-vbscript-syntax")
	diagnosticText := string(diagnostics.Params)
	for _, expected := range []string{
		"callStatementRequiresParentheses",
		"expressionCallRequiresParentheses",
		"statementCallDisallowsParenthesizedArguments",
	} {
		if !strings.Contains(diagnosticText, expected) {
			t.Fatalf("call syntax diagnostics missing %q: %s", expected, diagnosticText)
		}
	}

	actions := client.request("textDocument/codeAction", map[string]any{
		"textDocument": map[string]any{"uri": uri},
		"range": mapPositionRange(
			positionAt(source, strings.Index(source, "Call Func1")),
			positionAt(source, strings.Index(source, "Call Func1")+len("Call Func1")),
		),
		"context": map[string]any{
			"diagnostics": syntaxDiagnostics,
			"only":        []string{"quickfix"},
		},
	})
	serialized := mustJSONText(t, actions.Result)
	for _, expected := range []string{
		"Fix VBScript call syntax",
		`"newText":"Call Func1(hoge)"`,
		`"newText":"Z = Func1(hoge)"`,
		`"newText":"Func2 hoge, fuga"`,
		`"newText":"Call Func2(hoge, fuga)"`,
		`"newText":"Z = Func2(hoge, fuga)"`,
	} {
		if !strings.Contains(serialized, expected) {
			t.Fatalf("call syntax quick fix missing %q: %s", expected, serialized)
		}
	}
}

func TestStdioParityDoesNotReturnSplitQuickFixesForUnsupportedDeclarationSyntaxErrors(t *testing.T) {
	client := startStdioTestClient(t)
	defer client.close()

	root := t.TempDir()
	uri := pathToFileURI(filepath.Join(root, "go-vb-declaration-syntax.asp"))
	source := `<%
Public value = 1
Dim typed As Integer
%>`
	client.request("initialize", map[string]any{
		"processId":    nil,
		"rootUri":      pathToFileURI(root),
		"capabilities": map[string]any{},
	})
	notifyOpenClassicASPDocument(t, client, uri, source)
	diagnostics := waitForDiagnosticsContaining(t, client, "As types")
	syntaxDiagnostics := diagnosticsFromSource(t, diagnostics, "asp-lsp-vbscript-syntax")
	diagnosticText := string(diagnostics.Params)
	for _, expected := range []string{"initializedDeclaration", "typedDeclaration", "As types"} {
		if !strings.Contains(diagnosticText, expected) {
			t.Fatalf("unsupported declaration diagnostics missing %q: %s", expected, diagnosticText)
		}
	}

	actions := client.request("textDocument/codeAction", map[string]any{
		"textDocument": map[string]any{"uri": uri},
		"range": mapPositionRange(
			positionAt(source, strings.Index(source, "Public value")),
			positionAt(source, strings.Index(source, "Public value")+len("Public value")),
		),
		"context": map[string]any{
			"diagnostics": syntaxDiagnostics,
			"only":        []string{"quickfix"},
		},
	})
	if strings.Contains(mustJSONText(t, actions.Result), "Split initialized Dim declaration") {
		t.Fatalf("unsupported declaration should not return split Dim quick fix: %s", mustJSONText(t, actions.Result))
	}
}

func TestStdioParityExtractsInlineStylesToNearbyCSSClasses(t *testing.T) {
	source := `<div style="display:flex;color:red">あいうえお</div>`
	uri, actions := inlineStyleCodeActionsForSource(t, source, strings.Index(source, "<div")+1, nil)
	classAction := codeActionByTitle(actions, "class")
	if classAction == nil {
		t.Fatalf("inline style class extract action missing: %s", mustJSONText(t, actions))
	}
	if codeActionByTitle(actions, "ID") == nil {
		t.Fatalf("inline style ID extract action missing: %s", mustJSONText(t, actions))
	}

	got := applyWorkspaceEditForURI(t, source, uri, classAction.Edit)
	want := `<style>
  .style-1 {
    display: flex;
    color: red;
  }
</style>
<div class="style-1">あいうえお</div>`
	if got != want {
		t.Fatalf("inline style class extract edit mismatch:\n got: %q\nwant: %q", got, want)
	}
}

func TestStdioParityFormatsExtractedInlineStylesWithoutTouchingSurroundingHTML(t *testing.T) {
	source := `<p data-x="1">before</p>
<section style="color:red;background-image:url(data:image/svg+xml;charset=utf8,%3Csvg%3E%3C/svg%3E); border:1px solid #000">x</section>
<p style-not="color:red">after</p>`
	uri, actions := inlineStyleCodeActionsForSource(t, source, strings.Index(source, "style=")+2, nil)
	classAction := codeActionByTitle(actions, "class")
	if classAction == nil {
		t.Fatalf("inline style class extract action missing: %s", mustJSONText(t, actions))
	}

	got := applyWorkspaceEditForURI(t, source, uri, classAction.Edit)
	want := `<p data-x="1">before</p>
<style>
  .style-1 {
    color: red;
    background-image: url(data:image/svg+xml;charset=utf8,%3Csvg%3E%3C/svg%3E);
    border: 1px solid #000;
  }
</style>
<section class="style-1">x</section>
<p style-not="color:red">after</p>`
	if got != want {
		t.Fatalf("inline style formatting extract edit mismatch:\n got: %q\nwant: %q", got, want)
	}
}

func TestStdioParityAppendsExtractedStyleClassNamesWithoutColliding(t *testing.T) {
	source := `<div class="card style-1" style="color: red;">x</div>`
	uri, actions := inlineStyleCodeActionsForSource(t, source, strings.Index(source, "style=")+2, nil)
	classAction := codeActionByTitle(actions, "class")
	if classAction == nil {
		t.Fatalf("inline style class extract action missing: %s", mustJSONText(t, actions))
	}

	got := applyWorkspaceEditForURI(t, source, uri, classAction.Edit)
	want := `<style>
  .style-2 {
    color: red;
  }
</style>
<div class="card style-1 style-2">x</div>`
	if got != want {
		t.Fatalf("inline style class collision edit mismatch:\n got: %q\nwant: %q", got, want)
	}
}

func TestStdioParityExtractsInlineStylesToExistingAndGeneratedCSSIDs(t *testing.T) {
	existingIDSource := `<div id="hero" style="color: red;">x</div>`
	existingURI, existingActions := inlineStyleCodeActionsForSource(t, existingIDSource, strings.Index(existingIDSource, "style=")+2, nil)
	existingIDAction := codeActionByTitle(existingActions, "ID")
	if existingIDAction == nil {
		t.Fatalf("inline style ID extract action missing for existing ID: %s", mustJSONText(t, existingActions))
	}
	existingGot := applyWorkspaceEditForURI(t, existingIDSource, existingURI, existingIDAction.Edit)
	existingWant := `<style>
  #hero {
    color: red;
  }
</style>
<div id="hero">x</div>`
	if existingGot != existingWant {
		t.Fatalf("existing ID inline style extract edit mismatch:\n got: %q\nwant: %q", existingGot, existingWant)
	}

	generatedIDSource := `<div id="style-1"></div>
<div style="color: red;">x</div>`
	generatedURI, generatedActions := inlineStyleCodeActionsForSource(t, generatedIDSource, strings.Index(generatedIDSource, "style=")+2, nil)
	generatedIDAction := codeActionByTitle(generatedActions, "ID")
	if generatedIDAction == nil {
		t.Fatalf("inline style ID extract action missing for generated ID: %s", mustJSONText(t, generatedActions))
	}
	generatedGot := applyWorkspaceEditForURI(t, generatedIDSource, generatedURI, generatedIDAction.Edit)
	generatedWant := `<div id="style-1"></div>
<style>
  #style-2 {
    color: red;
  }
</style>
<div id="style-2">x</div>`
	if generatedGot != generatedWant {
		t.Fatalf("generated ID inline style extract edit mismatch:\n got: %q\nwant: %q", generatedGot, generatedWant)
	}
}

func TestStdioParityAppendsExtractedInlineStylesToNearestExistingStyleElement(t *testing.T) {
	settings := map[string]any{
		"aspLsp": map[string]any{
			"styleExtraction": map[string]any{"insertionMode": "reuseExistingStyleTag"},
		},
	}

	source := `<style>
  .existing {
    color: blue;
  }
</style>
<div style="display:flex;color:red">x</div>`
	uri, actions := inlineStyleCodeActionsForSource(t, source, strings.Index(source, "style=")+2, settings)
	classAction := codeActionByTitle(actions, "class")
	if classAction == nil {
		t.Fatalf("inline style class extract action missing for reuse mode: %s", mustJSONText(t, actions))
	}
	got := applyWorkspaceEditForURI(t, source, uri, classAction.Edit)
	want := `<style>
  .existing {
    color: blue;
  }
  .style-1 {
    display: flex;
    color: red;
  }
</style>
<div class="style-1">x</div>`
	if got != want {
		t.Fatalf("nearest style element extract edit mismatch:\n got: %q\nwant: %q", got, want)
	}

	nearestSource := `<style>
</style>
<div>this gap keeps the first style farther away from the target</div>
<div style="color: red;">x</div>
<style>
</style>`
	nearestURI, nearestActions := inlineStyleCodeActionsForSource(t, nearestSource, strings.Index(nearestSource, "style=")+2, settings)
	nearestAction := codeActionByTitle(nearestActions, "class")
	if nearestAction == nil {
		t.Fatalf("inline style class extract action missing for nearest style element: %s", mustJSONText(t, nearestActions))
	}
	nearestGot := applyWorkspaceEditForURI(t, nearestSource, nearestURI, nearestAction.Edit)
	nearestWant := `<style>
</style>
<div>this gap keeps the first style farther away from the target</div>
<div class="style-1">x</div>
<style>
  .style-1 {
    color: red;
  }
</style>`
	if nearestGot != nearestWant {
		t.Fatalf("nearest among several style elements edit mismatch:\n got: %q\nwant: %q", nearestGot, nearestWant)
	}

	fallbackSource := `<div style="color: red;">x</div>`
	fallbackURI, fallbackActions := inlineStyleCodeActionsForSource(t, fallbackSource, strings.Index(fallbackSource, "style=")+2, settings)
	fallbackAction := codeActionByTitle(fallbackActions, "class")
	if fallbackAction == nil {
		t.Fatalf("inline style class extract action missing for fallback mode: %s", mustJSONText(t, fallbackActions))
	}
	fallbackGot := applyWorkspaceEditForURI(t, fallbackSource, fallbackURI, fallbackAction.Edit)
	fallbackWant := `<style>
  .style-1 {
    color: red;
  }
</style>
<div class="style-1">x</div>`
	if fallbackGot != fallbackWant {
		t.Fatalf("reuse mode fallback edit mismatch:\n got: %q\nwant: %q", fallbackGot, fallbackWant)
	}
}

func TestStdioParityDoesNotReturnInlineStyleExtractionActionsForUnsupportedRanges(t *testing.T) {
	noStyle := `<div class="card">x</div>`
	if _, actions := inlineStyleCodeActionsForSource(t, noStyle, strings.Index(noStyle, "card"), nil); len(actions) != 0 {
		t.Fatalf("no-style target returned inline style actions: %s", mustJSONText(t, actions))
	}

	emptyStyle := `<div style="">x</div>`
	if _, actions := inlineStyleCodeActionsForSource(t, emptyStyle, strings.Index(emptyStyle, "style="), nil); len(actions) != 0 {
		t.Fatalf("empty style target returned inline style actions: %s", mustJSONText(t, actions))
	}

	outsideTag := `<div style="color: red;">x</div>`
	if _, actions := inlineStyleCodeActionsForSource(t, outsideTag, strings.Index(outsideTag, "x"), nil); len(actions) != 0 {
		t.Fatalf("outside tag target returned inline style actions: %s", mustJSONText(t, actions))
	}

	aspDelimiter := `<div style="color: <%= color %>;">x</div>`
	if _, actions := inlineStyleCodeActionsForSource(t, aspDelimiter, strings.Index(aspDelimiter, "style="), nil); len(actions) != 0 {
		t.Fatalf("ASP delimiter style target returned inline style actions: %s", mustJSONText(t, actions))
	}
}

func diagnosticsFromSource(t *testing.T, message *rpcMessage, source string) []diagnosticResult {
	t.Helper()
	var result []diagnosticResult
	for _, diagnostic := range diagnosticsFromMessage(t, message) {
		if diagnostic.Source == source {
			result = append(result, diagnostic)
		}
	}
	return result
}

func inlineStyleCodeActionsForSource(t *testing.T, source string, offset int, settings map[string]any) (string, []lsp.CodeAction) {
	t.Helper()
	client := startStdioTestClient(t)
	defer client.close()

	root := t.TempDir()
	uri := pathToFileURI(filepath.Join(root, "inline-style.asp"))
	client.request("initialize", map[string]any{
		"processId":    nil,
		"rootUri":      pathToFileURI(root),
		"capabilities": map[string]any{},
	})
	if settings != nil {
		notifyConfiguration(t, client, settings)
	}
	openClassicASPDocument(t, client, uri, source)
	response := client.request("textDocument/codeAction", map[string]any{
		"textDocument": map[string]any{"uri": uri},
		"range": mapPositionRange(
			positionAt(source, offset),
			positionAt(source, offset),
		),
		"context": map[string]any{
			"diagnostics": []diagnosticResult{},
			"only":        []string{"refactor.extract"},
		},
	})
	var actions []lsp.CodeAction
	mustDecodeResult(t, response.Result, &actions)
	return uri, actions
}

func codeActionByTitle(actions []lsp.CodeAction, contains string) *lsp.CodeAction {
	for i := range actions {
		if strings.Contains(actions[i].Title, contains) {
			return &actions[i]
		}
	}
	return nil
}

func applyWorkspaceEditForURI(t *testing.T, source string, uri string, edit *lsp.WorkspaceEdit) string {
	t.Helper()
	if edit == nil {
		t.Fatalf("workspace edit is nil")
	}
	changes := edit.Changes[uri]
	if len(changes) != 1 {
		t.Fatalf("workspace edit changes for %s = %d, want 1: %s", uri, len(changes), mustJSONText(t, edit))
	}
	return applyTextEditToString(t, source, changes[0])
}

func applyTextEditToString(t *testing.T, source string, edit lsp.TextEdit) string {
	t.Helper()
	start := offsetAtLSPPosition(t, source, edit.Range.Start)
	end := offsetAtLSPPosition(t, source, edit.Range.End)
	if start < 0 || end < start || end > len(source) {
		t.Fatalf("invalid edit range %#v for source length %d", edit.Range, len(source))
	}
	return source[:start] + edit.NewText + source[end:]
}

func offsetAtLSPPosition(t *testing.T, source string, position lsp.Position) int {
	t.Helper()
	line := 0
	character := 0
	for offset, r := range source {
		if line == position.Line && character == position.Character {
			return offset
		}
		if r == '\n' {
			line++
			character = 0
			continue
		}
		if r <= 0xFFFF {
			character++
		} else {
			character += 2
		}
	}
	if line == position.Line && character == position.Character {
		return len(source)
	}
	t.Fatalf("position %#v is outside source", position)
	return 0
}
