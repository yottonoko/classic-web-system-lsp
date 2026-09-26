package lspserver

import (
	"os"
	"path/filepath"
	"strings"
	"testing"

	"github.com/yottonoko/classic-web-system-lsp/internal/lsp"
	workspacepkg "github.com/yottonoko/classic-web-system-lsp/internal/workspace"
)

func TestStdioParityReturnsHTMLCSSAndJavaScriptCompletionsOverJSONRPC(t *testing.T) {
	client := startStdioTestClient(t)
	defer client.close()

	root := t.TempDir()
	client.request("initialize", map[string]any{
		"processId": nil,
		"rootUri":   pathToFileURI(root),
		"capabilities": map[string]any{
			"workspace": map[string]any{
				"semanticTokens": map[string]any{"refreshSupport": true},
			},
		},
	})
	cases := []struct {
		name         string
		markedSource string
		expected     string
	}{
		{
			name:         "html",
			markedSource: "<<<<caret>>>",
			expected:     "div",
		},
		{
			name:         "css-block",
			markedSource: "<style>.x { colo<<<caret>>> }</style>",
			expected:     "color",
		},
		{
			name:         "css-attribute",
			markedSource: `<div style="colo<<<caret>>>"></div>`,
			expected:     "color",
		},
		{
			name:         "client-js",
			markedSource: "<script>const alphaBeta = 1; alpha<<<caret>>></script>",
			expected:     "alphaBeta",
		},
	}
	for _, testCase := range cases {
		document := markedDocument(testCase.markedSource)
		uri := pathToFileURI(filepath.Join(root, "go-"+testCase.name+".asp"))
		openClassicASPDocument(t, client, uri, document.Text)
		completions := client.request("textDocument/completion", map[string]any{
			"textDocument": map[string]any{"uri": uri},
			"position":     document.Position,
		})
		if !strings.Contains(mustJSONText(t, completions.Result), testCase.expected) {
			t.Fatalf("%s completions missing %q: %s", testCase.name, testCase.expected, mustJSONText(t, completions.Result))
		}
	}
}

func TestStdioParityDelegatesJavaScriptHoverNavigationRenameAndSignatureHelpToTypeScript(t *testing.T) {
	client := startStdioTestClient(t)
	defer client.close()

	root := t.TempDir()
	uri := pathToFileURI(filepath.Join(root, "go-javascript-navigation.asp"))
	marked := markedDocument(`<script>
function greet(name) {
  return name.toUpperCase();
}
const message = gre<<<caret>>>et("Ada");
</script>`)
	source := marked.Text
	callPosition := marked.Position
	client.request("initialize", map[string]any{
		"processId": nil,
		"rootUri":   pathToFileURI(root),
		"capabilities": map[string]any{
			"workspace": map[string]any{
				"semanticTokens": map[string]any{"refreshSupport": true},
			},
		},
	})
	openClassicASPDocument(t, client, uri, source)

	completions := completionItems(client.request("textDocument/completion", map[string]any{
		"textDocument": map[string]any{"uri": uri},
		"position":     callPosition,
	}).Result)
	completionItem, ok := completions.find("greet")
	if !ok {
		t.Fatalf("JavaScript completions missing greet: %#v", completions)
	}
	resolvedCompletion := client.request("completionItem/resolve", completionItem)
	if !strings.Contains(mustJSONText(t, resolvedCompletion.Result), "greet") {
		t.Fatalf("resolved JavaScript completion missing greet: %s", mustJSONText(t, resolvedCompletion.Result))
	}

	hover := client.request("textDocument/hover", map[string]any{
		"textDocument": map[string]any{"uri": uri},
		"position":     callPosition,
	})
	if !strings.Contains(mustJSONText(t, hover.Result), "greet") {
		t.Fatalf("JavaScript hover missing greet: %s", mustJSONText(t, hover.Result))
	}

	definition := client.request("textDocument/definition", map[string]any{
		"textDocument": map[string]any{"uri": uri},
		"position":     callPosition,
	})
	if !strings.Contains(mustJSONText(t, definition.Result), `"line":1`) {
		t.Fatalf("JavaScript definition mismatch: %s", mustJSONText(t, definition.Result))
	}

	references := client.request("textDocument/references", map[string]any{
		"textDocument": map[string]any{"uri": uri},
		"position":     callPosition,
		"context":      map[string]any{"includeDeclaration": true},
	})
	if got := resultArrayLength(t, references.Result); got != 2 {
		t.Fatalf("JavaScript references = %d, want 2: %s", got, mustJSONText(t, references.Result))
	}

	prepareRename := client.request("textDocument/prepareRename", map[string]any{
		"textDocument": map[string]any{"uri": uri},
		"position":     callPosition,
	})
	if !strings.Contains(mustJSONText(t, prepareRename.Result), `"line":4`) {
		t.Fatalf("JavaScript prepareRename mismatch: %s", mustJSONText(t, prepareRename.Result))
	}

	signature := client.request("textDocument/signatureHelp", map[string]any{
		"textDocument": map[string]any{"uri": uri},
		"position":     positionAt(source, strings.Index(source, `"Ada"`)),
	})
	signatureJSON := mustJSONText(t, signature.Result)
	if !strings.Contains(signatureJSON, "name") {
		t.Fatalf("JavaScript signature help missing name: %s", signatureJSON)
	}
	if got := strings.Count(signatureJSON, "greet(name"); got != 1 {
		t.Fatalf("JavaScript structured signature label was remapped %d times, want once: %s", got, signatureJSON)
	}

	documentSymbols := client.request("textDocument/documentSymbol", map[string]any{
		"textDocument": map[string]any{"uri": uri},
	})
	if !strings.Contains(mustJSONText(t, documentSymbols.Result), "greet") {
		t.Fatalf("JavaScript document symbols missing greet: %s", mustJSONText(t, documentSymbols.Result))
	}

	workspaceSymbols := client.request("workspace/symbol", map[string]any{"query": "greet"})
	if !strings.Contains(mustJSONText(t, workspaceSymbols.Result), "greet") {
		t.Fatalf("JavaScript workspace symbols missing greet: %s", mustJSONText(t, workspaceSymbols.Result))
	}

	rename := client.request("textDocument/rename", map[string]any{
		"textDocument": map[string]any{"uri": uri},
		"position":     callPosition,
		"newName":      "formatName",
	})
	if got := strings.Count(mustJSONText(t, rename.Result), `"newText":"formatName"`); got != 2 {
		t.Fatalf("JavaScript rename edit count = %d, want 2: %s", got, mustJSONText(t, rename.Result))
	}
}

func TestStdioParityRoutesJavaScriptLanguageFeaturesAcrossImportedModule(t *testing.T) {
	client := startStdioTestClient(t)
	defer client.close()

	root := t.TempDir()
	modelsPath := filepath.Join(root, "models.js")
	models := `export class BaseWidget {
  render(value) { return value; }
}
export class DashboardWidget extends BaseWidget {
  render(value) { return super.render(value); }
}
export function formatWidget(widget, prefix) { return prefix + widget.render(prefix); }
`
	if err := os.WriteFile(modelsPath, []byte(models), 0o644); err != nil {
		t.Fatal(err)
	}
	if err := os.WriteFile(filepath.Join(root, "jsconfig.json"), []byte(`{"include":["*.js","*.asp"]}`), 0o644); err != nil {
		t.Fatal(err)
	}
	uri := pathToFileURI(filepath.Join(root, "default.asp"))
	modelsURI := pathToFileURI(modelsPath)
	source := `<script type="module">
import * as models from "./models.js";
const widget = new models.DashboardWidget();
const output = models.formatWidget(widget, "x");
/** @type {models.BaseWidget} */
const baseWidget = widget;
baseWidget.render(output);
widget.render(output);
</script>`
	client.request("initialize", map[string]any{
		"processId":    nil,
		"rootUri":      pathToFileURI(root),
		"capabilities": map[string]any{},
	})
	notifyConfiguration(t, client, map[string]any{"aspLsp": map[string]any{"checkJs": true}})
	openClassicASPDocumentWithDiagnostics(t, client, uri, source)

	position := func(needle string, last bool) map[string]int {
		offset := strings.Index(source, needle)
		if last {
			offset = strings.LastIndex(source, needle)
		}
		if offset < 0 {
			t.Fatalf("source missing %q", needle)
		}
		return positionAt(source, offset)
	}
	dashboardPosition := position("DashboardWidget", false)
	widgetUsePosition := position("widget.render", true)
	baseRenderPosition := position("baseWidget.render", false)
	baseRenderPosition["character"] += len("baseWidget.")
	formatCallPosition := position("formatWidget", false)
	formatCallPosition["character"] += len("formatWidget(")
	renderCompletionPosition := position("widget.render", true)
	renderCompletionPosition["character"] += len("widget.")

	completions := mustJSONText(t, client.request("textDocument/completion", map[string]any{
		"textDocument": map[string]any{"uri": uri},
		"position":     renderCompletionPosition,
	}).Result)
	if !strings.Contains(completions, `"render"`) {
		t.Fatalf("multi-file JavaScript completion missing render: %s", completions)
	}
	hover := mustJSONText(t, client.request("textDocument/hover", map[string]any{
		"textDocument": map[string]any{"uri": uri},
		"position":     dashboardPosition,
	}).Result)
	if !strings.Contains(hover, "DashboardWidget") {
		t.Fatalf("multi-file JavaScript hover missing DashboardWidget: %s", hover)
	}
	signature := mustJSONText(t, client.request("textDocument/signatureHelp", map[string]any{
		"textDocument": map[string]any{"uri": uri},
		"position":     formatCallPosition,
	}).Result)
	if !strings.Contains(signature, "formatWidget") || !strings.Contains(signature, "prefix") {
		t.Fatalf("multi-file JavaScript signature help mismatch: %s", signature)
	}
	for _, method := range []string{"textDocument/definition", "textDocument/typeDefinition"} {
		response := client.request(method, map[string]any{
			"textDocument": map[string]any{"uri": uri},
			"position":     dashboardPosition,
		})
		var locations []lsp.Location
		if err := remarshal(response.Result, &locations); err != nil {
			t.Fatalf("decode %s locations: %v", method, err)
		}
		if !containsLocationURI(locations, modelsURI) {
			t.Fatalf("%s did not reach imported module: %s", method, mustJSONText(t, response.Result))
		}
	}
	implementationResponse := client.request("textDocument/implementation", map[string]any{
		"textDocument": map[string]any{"uri": uri},
		"position":     baseRenderPosition,
	})
	var implementation []lsp.Location
	if err := remarshal(implementationResponse.Result, &implementation); err != nil {
		t.Fatalf("decode implementation locations: %v", err)
	}
	if !containsLocationURI(implementation, modelsURI) || !containsLocationLine(implementation, modelsURI, 4) {
		t.Fatalf("multi-file JavaScript implementation mismatch: %s", mustJSONText(t, implementationResponse.Result))
	}
	referencesResponse := client.request("textDocument/references", map[string]any{
		"textDocument": map[string]any{"uri": uri},
		"position":     dashboardPosition,
		"context":      map[string]any{"includeDeclaration": true},
	})
	var references []lsp.Location
	if err := remarshal(referencesResponse.Result, &references); err != nil {
		t.Fatalf("decode reference locations: %v", err)
	}
	if !containsLocationURI(references, modelsURI) || !containsLocationURI(references, uri) {
		t.Fatalf("multi-file JavaScript references did not map both files: %s", mustJSONText(t, referencesResponse.Result))
	}
	prepareRename := mustJSONText(t, client.request("textDocument/prepareRename", map[string]any{
		"textDocument": map[string]any{"uri": uri},
		"position":     dashboardPosition,
	}).Result)
	if !strings.Contains(prepareRename, `"line":2`) {
		t.Fatalf("multi-file JavaScript prepareRename was not source-mapped: %s", prepareRename)
	}
	renameResponse := client.request("textDocument/rename", map[string]any{
		"textDocument": map[string]any{"uri": uri},
		"position":     dashboardPosition,
		"newName":      "RenamedWidget",
	})
	var rename lsp.WorkspaceEdit
	if err := remarshal(renameResponse.Result, &rename); err != nil {
		t.Fatalf("decode rename workspace edit: %v", err)
	}
	if !workspaceEditContainsURI(rename, modelsURI) || !workspaceEditContainsURI(rename, uri) || workspaceEditTextCount(rename, "RenamedWidget") < 2 {
		t.Fatalf("multi-file JavaScript rename did not map both files: %s", mustJSONText(t, renameResponse.Result))
	}
	highlights := mustJSONText(t, client.request("textDocument/documentHighlight", map[string]any{
		"textDocument": map[string]any{"uri": uri},
		"position":     widgetUsePosition,
	}).Result)
	if strings.Count(highlights, `"range"`) < 3 {
		t.Fatalf("multi-file JavaScript highlights mismatch: %s", highlights)
	}
	symbols := mustJSONText(t, client.request("textDocument/documentSymbol", map[string]any{
		"textDocument": map[string]any{"uri": uri},
	}).Result)
	for _, name := range []string{"widget", "output", "baseWidget"} {
		if !strings.Contains(symbols, name) {
			t.Fatalf("multi-file JavaScript document symbols missing %q: %s", name, symbols)
		}
	}
	diagnostics := mustJSONText(t, client.request("textDocument/diagnostic", map[string]any{
		"textDocument": map[string]any{"uri": uri},
	}).Result)
	if strings.Contains(diagnostics, "Cannot find") || strings.Contains(diagnostics, "Property 'render'") {
		t.Fatalf("multi-file JavaScript diagnostics did not resolve imported module: %s", diagnostics)
	}
}

func containsLocationURI(locations []lsp.Location, uri string) bool {
	for _, location := range locations {
		if workspacepkg.SameFileIdentityURI(location.URI, uri) {
			return true
		}
	}
	return false
}

func containsLocationLine(locations []lsp.Location, uri string, line int) bool {
	for _, location := range locations {
		if workspacepkg.SameFileIdentityURI(location.URI, uri) && location.Range.Start.Line == line {
			return true
		}
	}
	return false
}

func workspaceEditContainsURI(edit lsp.WorkspaceEdit, uri string) bool {
	for changedURI := range edit.Changes {
		if workspacepkg.SameFileIdentityURI(changedURI, uri) {
			return true
		}
	}
	return false
}

func workspaceEditTextCount(edit lsp.WorkspaceEdit, newText string) int {
	count := 0
	for _, edits := range edit.Changes {
		for _, textEdit := range edits {
			if textEdit.NewText == newText {
				count++
			}
		}
	}
	return count
}

func TestStdioParityDelegatesServerSideJScriptNavigationToTypeScript(t *testing.T) {
	client := startStdioTestClient(t)
	defer client.close()

	root := t.TempDir()
	uri := pathToFileURI(filepath.Join(root, "go-server-jscript.asp"))
	marked := markedDocument(`<%@ LANGUAGE="JScript" %>
<%
function serverGreet(name) {
  return name;
}
var message = serverGre<<<caret>>>et("Ada");
%>`)
	client.request("initialize", map[string]any{
		"processId":    nil,
		"rootUri":      pathToFileURI(root),
		"capabilities": map[string]any{},
	})
	openClassicASPDocument(t, client, uri, marked.Text)

	definition := client.request("textDocument/definition", map[string]any{
		"textDocument": map[string]any{"uri": uri},
		"position":     marked.Position,
	})
	if !strings.Contains(mustJSONText(t, definition.Result), `"line":2`) {
		t.Fatalf("definition missing serverGreet declaration: %s", mustJSONText(t, definition.Result))
	}
	typeDefinition := client.request("textDocument/typeDefinition", map[string]any{
		"textDocument": map[string]any{"uri": uri},
		"position":     marked.Position,
	})
	if !strings.Contains(mustJSONText(t, typeDefinition.Result), `"line":2`) {
		t.Fatalf("typeDefinition missing serverGreet declaration: %s", mustJSONText(t, typeDefinition.Result))
	}
}

func TestStdioParityHonorsDefaultJScriptServerSideLanguageSetting(t *testing.T) {
	client := startStdioTestClient(t)
	defer client.close()

	root := t.TempDir()
	uri := pathToFileURI(filepath.Join(root, "go-server-default-jscript.asp"))
	marked := markedDocument(`<%
function formatName(value) {
  return value;
}
var rendered = for<<<caret>>>matName(customer.name);
%>`)
	client.request("initialize", map[string]any{
		"processId":    nil,
		"rootUri":      pathToFileURI(root),
		"capabilities": map[string]any{},
	})
	if err := client.notify("workspace/didChangeConfiguration", map[string]any{
		"settings": map[string]any{"aspLsp": map[string]any{"defaultLanguage": "JScript"}},
	}); err != nil {
		t.Fatalf("didChangeConfiguration failed: %v", err)
	}
	openClassicASPDocument(t, client, uri, marked.Text)

	completions := client.request("textDocument/completion", map[string]any{
		"textDocument": map[string]any{"uri": uri},
		"position":     marked.Position,
	})
	if !strings.Contains(mustJSONText(t, completions.Result), "formatName") {
		t.Fatalf("server-side JScript completions missing formatName: %s", mustJSONText(t, completions.Result))
	}

	hover := client.request("textDocument/hover", map[string]any{
		"textDocument": map[string]any{"uri": uri},
		"position":     marked.Position,
	})
	if !strings.Contains(mustJSONText(t, hover.Result), "formatName(value") {
		t.Fatalf("server-side JScript hover missing signature: %s", mustJSONText(t, hover.Result))
	}

	definition := client.request("textDocument/definition", map[string]any{
		"textDocument": map[string]any{"uri": uri},
		"position":     marked.Position,
	})
	if !strings.Contains(mustJSONText(t, definition.Result), `"line":1`) {
		t.Fatalf("server-side JScript definition missing declaration: %s", mustJSONText(t, definition.Result))
	}

	references := client.request("textDocument/references", map[string]any{
		"textDocument": map[string]any{"uri": uri},
		"position":     marked.Position,
		"context":      map[string]any{"includeDeclaration": true},
	})
	if got := resultArrayLength(t, references.Result); got != 2 {
		t.Fatalf("server-side JScript references = %d, want 2: %s", got, mustJSONText(t, references.Result))
	}

	rename := client.request("textDocument/rename", map[string]any{
		"textDocument": map[string]any{"uri": uri},
		"position":     marked.Position,
		"newName":      "renderName",
	})
	if got := strings.Count(mustJSONText(t, rename.Result), `"newText":"renderName"`); got != 2 {
		t.Fatalf("server-side JScript rename edit count = %d, want 2: %s", got, mustJSONText(t, rename.Result))
	}
}

func TestStdioParityDelegatesJavaScriptHighlightsAndInlayHints(t *testing.T) {
	client := startStdioTestClient(t)
	defer client.close()

	root := t.TempDir()
	uri := pathToFileURI(filepath.Join(root, "go-js-highlight-inlay.asp"))
	marked := markedDocument(`<script>
function add(first, second) {
  return first + second;
}
const result = ad<<<caret>>>d(1, 2);
</script>`)
	client.request("initialize", map[string]any{
		"processId":    nil,
		"rootUri":      pathToFileURI(root),
		"capabilities": map[string]any{},
	})
	openClassicASPDocument(t, client, uri, marked.Text)

	highlights := client.request("textDocument/documentHighlight", map[string]any{
		"textDocument": map[string]any{"uri": uri},
		"position":     marked.Position,
	})
	if got := resultArrayLength(t, highlights.Result); got <= 1 {
		t.Fatalf("JavaScript highlights = %d, want more than 1: %s", got, mustJSONText(t, highlights.Result))
	}

	hints := client.request("textDocument/inlayHint", map[string]any{
		"textDocument": map[string]any{"uri": uri},
		"range": map[string]any{
			"start": map[string]any{"line": 0, "character": 0},
			"end":   map[string]any{"line": 6, "character": 0},
		},
	})
	var decodedHints []lsp.InlayHint
	mustDecodeResult(t, hints.Result, &decodedHints)
	labels := map[string]bool{}
	for _, hint := range decodedHints {
		labels[javaScriptInlayHintLabelText(hint.Label)] = true
	}
	if !labels["first:"] || !labels["second:"] {
		t.Fatalf("JavaScript inlay hints mismatch: %s", mustJSONText(t, hints.Result))
	}
}

func TestStdioParityTreatsRootScriptTagsBetweenASPProcedureBlocksAsJavaScript(t *testing.T) {
	client := startStdioTestClient(t)
	defer client.close()

	root := t.TempDir()
	uri := pathToFileURI(filepath.Join(root, "go-root-sub-script.asp"))
	source := `<% Sub A() %>
<script>
const aValue = 10;
let bValue = aValue + 1;
console.log(aValue, bValue);
missingThing();
</script>
<% End Sub %>`
	client.request("initialize", map[string]any{
		"processId":    nil,
		"rootUri":      pathToFileURI(root),
		"capabilities": map[string]any{},
	})
	notifyConfiguration(t, client, map[string]any{"aspLsp": map[string]any{
		"checkJs":    true,
		"javascript": map[string]any{"ignoreProjectConfig": true},
	}})
	notifyOpenClassicASPDocument(t, client, uri, source)
	diagnostics := waitForDiagnosticsContaining(t, client, "missingThing")
	if !strings.Contains(string(diagnostics.Params), "asp-lsp-typescript") {
		t.Fatalf("JavaScript diagnostics source mismatch: %s", diagnostics.Params)
	}

	completions := completionLabels(client.request("textDocument/completion", map[string]any{
		"textDocument": map[string]any{"uri": uri},
		"position":     positionAt(source, strings.Index(source, "console.")+len("console.")),
	}).Result)
	if !completions.contains("log") {
		t.Fatalf("console completions missing log: %#v", completions)
	}

	hover := client.request("textDocument/hover", map[string]any{
		"textDocument": map[string]any{"uri": uri},
		"position":     positionAt(source, strings.Index(source, "aValue")),
	})
	if !strings.Contains(mustJSONText(t, hover.Result), "aValue") {
		t.Fatalf("JavaScript hover missing aValue: %s", mustJSONText(t, hover.Result))
	}

	semanticTokens := client.request("textDocument/semanticTokens/full", map[string]any{
		"textDocument": map[string]any{"uri": uri},
	})
	if !hasTokenMatchingText(source, decodeSemanticTokens(t, semanticTokens.Result), "aValue", semanticTokenVariable) {
		t.Fatalf("semantic tokens missing aValue variable token: %s", mustJSONText(t, semanticTokens.Result))
	}
}

func TestStdioParityKeepsBrowserJavaScriptGlobalsAvailableWithoutProjectConfig(t *testing.T) {
	client := startStdioTestClient(t)
	defer client.close()

	root := t.TempDir()
	uri := pathToFileURI(filepath.Join(root, "go-browser-js-globals.asp"))
	source := `<script>
const clock = document.querySelector("#clientClock");
const formatter = new Intl.DateTimeFormat("en");
</script>`
	client.request("initialize", map[string]any{
		"processId":    nil,
		"rootUri":      pathToFileURI(root),
		"capabilities": map[string]any{},
	})
	notifyConfiguration(t, client, map[string]any{"aspLsp": map[string]any{"checkJs": true}})
	openClassicASPDocument(t, client, uri, source)
	pulled := client.request("textDocument/diagnostic", map[string]any{
		"textDocument": map[string]any{"uri": uri},
	})
	serialized := mustJSONText(t, pulled.Result)
	for _, unexpected := range []string{"Cannot find name 'document'", "Cannot find name 'Intl'"} {
		if strings.Contains(serialized, unexpected) {
			t.Fatalf("browser JavaScript globals diagnostic leaked %q: %s", unexpected, serialized)
		}
	}
}

func TestStdioParityKeepsBrowserJavaScriptLibsWhenProjectConfigDisablesDefaultLibs(t *testing.T) {
	client := startStdioTestClient(t)
	defer client.close()

	root := t.TempDir()
	if err := os.WriteFile(filepath.Join(root, "jsconfig.json"), []byte(`{"compilerOptions":{"checkJs":true,"noLib":true,"lib":[]}}`), 0o644); err != nil {
		t.Fatal(err)
	}
	uri := pathToFileURI(filepath.Join(root, "default.asp"))
	source := `<script>
document.querySelector("#clientClock");
new Intl.DateTimeFormat("en");
</script>`
	client.request("initialize", map[string]any{
		"processId":    nil,
		"rootUri":      pathToFileURI(root),
		"capabilities": map[string]any{},
	})
	notifyConfiguration(t, client, map[string]any{"aspLsp": map[string]any{"checkJs": true}})
	openClassicASPDocument(t, client, uri, source)
	pulled := client.request("textDocument/diagnostic", map[string]any{
		"textDocument": map[string]any{"uri": uri},
	})
	serialized := mustJSONText(t, pulled.Result)
	for _, unexpected := range []string{"Cannot find name 'document'", "Cannot find name 'Intl'"} {
		if strings.Contains(serialized, unexpected) {
			t.Fatalf("browser JavaScript libs diagnostic leaked %q: %s", unexpected, serialized)
		}
	}
}

func TestStdioParityCanIgnoreJavaScriptProjectConfigFiles(t *testing.T) {
	client := startStdioTestClient(t)
	defer client.close()

	root := t.TempDir()
	if err := os.WriteFile(filepath.Join(root, "jsconfig.json"), []byte(`{"compilerOptions":{"lib":["es5"]}}`), 0o644); err != nil {
		t.Fatal(err)
	}
	uri := pathToFileURI(filepath.Join(root, "default.asp"))
	marked := markedDocument(`<script>
docu<<<caret>>>
</script>`)
	client.request("initialize", map[string]any{
		"processId":    nil,
		"rootUri":      pathToFileURI(root),
		"capabilities": map[string]any{},
	})
	notifyConfiguration(t, client, map[string]any{"aspLsp": map[string]any{
		"javascript": map[string]any{"ignoreProjectConfig": true},
	}})
	openClassicASPDocument(t, client, uri, marked.Text)
	completions := completionLabels(client.request("textDocument/completion", map[string]any{
		"textDocument": map[string]any{"uri": uri},
		"position":     marked.Position,
	}).Result)
	if !completions.contains("document") {
		t.Fatalf("ignore project config completions missing document: %#v", completions)
	}
}

func TestStdioParityAppliesTypeScriptCompilerOptionsFromSettingsToEmbeddedJavaScriptProjects(t *testing.T) {
	client := startStdioTestClient(t)
	defer client.close()

	root := t.TempDir()
	writeAmbientTypes(t, root, "jquery", `declare const $: { ready(callback: () => void): void };
`)
	uri := pathToFileURI(filepath.Join(root, "default.asp"))
	jqueryMarked := markedDocument(`<script>
$<<<caret>>>
</script>`)
	domMarked := markedDocument(`<script>
docu<<<caret>>>
</script>`)
	client.request("initialize", map[string]any{
		"processId":    nil,
		"rootUri":      pathToFileURI(root),
		"capabilities": map[string]any{},
	})
	notifyConfiguration(t, client, map[string]any{"aspLsp": map[string]any{
		"javascript": map[string]any{"compilerOptions": map[string]any{"types": []any{}}},
	}})
	openClassicASPDocument(t, client, uri, jqueryMarked.Text)
	jqueryCompletions := completionLabels(client.request("textDocument/completion", map[string]any{
		"textDocument": map[string]any{"uri": uri},
		"position":     jqueryMarked.Position,
	}).Result)
	if jqueryCompletions.contains("$") {
		t.Fatalf("compilerOptions types=[] completions should not include jquery global: %#v", jqueryCompletions)
	}

	if err := client.notify("textDocument/didChange", map[string]any{
		"textDocument":   map[string]any{"uri": uri, "version": 2},
		"contentChanges": []map[string]any{{"text": domMarked.Text}},
	}); err != nil {
		t.Fatal(err)
	}
	client.waitForNotification("textDocument/publishDiagnostics", "")
	domCompletions := completionLabels(client.request("textDocument/completion", map[string]any{
		"textDocument": map[string]any{"uri": uri},
		"position":     domMarked.Position,
	}).Result)
	if !domCompletions.contains("document") {
		t.Fatalf("compilerOptions types=[] completions missing document: %#v", domCompletions)
	}
}

func TestStdioParityKeepsEmbeddedJavaScriptBrowserFocusedWhenNodeAmbientTypesExist(t *testing.T) {
	client := startStdioTestClient(t)
	defer client.close()

	root := t.TempDir()
	writeAmbientTypes(t, root, "node", "declare var __dirname: string;\ndeclare var __filename: string;\n")
	writeAmbientTypes(t, root, "jquery", `declare const $: { ready(callback: () => void): void };
`)
	if err := os.WriteFile(filepath.Join(root, "jsconfig.json"), []byte(`{"compilerOptions":{"types":["node","jquery"],"module":"ESNext","moduleResolution":"Bundler"}}`), 0o644); err != nil {
		t.Fatal(err)
	}
	uri := pathToFileURI(filepath.Join(root, "default.asp"))
	nodeMarked := markedDocument(`<script>
__<<<caret>>>
</script>`)
	domMarked := markedDocument(`<script>
docu<<<caret>>>
</script>`)
	jqueryMarked := markedDocument(`<script>
$<<<caret>>>
</script>`)
	client.request("initialize", map[string]any{
		"processId":    nil,
		"rootUri":      pathToFileURI(root),
		"capabilities": map[string]any{},
	})
	openClassicASPDocument(t, client, uri, nodeMarked.Text)
	nodeCompletions := completionLabels(client.request("textDocument/completion", map[string]any{
		"textDocument": map[string]any{"uri": uri},
		"position":     nodeMarked.Position,
	}).Result)
	if nodeCompletions.contains("__dirname") || nodeCompletions.contains("__filename") {
		t.Fatalf("browser-focused completions leaked Node globals: %#v", nodeCompletions)
	}

	if err := client.notify("textDocument/didChange", map[string]any{
		"textDocument":   map[string]any{"uri": uri, "version": 2},
		"contentChanges": []map[string]any{{"text": domMarked.Text}},
	}); err != nil {
		t.Fatal(err)
	}
	client.waitForNotification("textDocument/publishDiagnostics", "")
	domCompletions := completionLabels(client.request("textDocument/completion", map[string]any{
		"textDocument": map[string]any{"uri": uri},
		"position":     domMarked.Position,
	}).Result)
	if !domCompletions.contains("document") {
		t.Fatalf("browser-focused completions missing document: %#v", domCompletions)
	}

	if err := client.notify("textDocument/didChange", map[string]any{
		"textDocument":   map[string]any{"uri": uri, "version": 3},
		"contentChanges": []map[string]any{{"text": jqueryMarked.Text}},
	}); err != nil {
		t.Fatal(err)
	}
	client.waitForNotification("textDocument/publishDiagnostics", "")
	jqueryCompletions := completionLabels(client.request("textDocument/completion", map[string]any{
		"textDocument": map[string]any{"uri": uri},
		"position":     jqueryMarked.Position,
	}).Result)
	if !jqueryCompletions.contains("$") {
		t.Fatalf("browser-focused completions missing jquery global: %#v", jqueryCompletions)
	}
}

func TestStdioParityUsesWorkspaceJavaScriptFilesInTheTypeScriptProjectModel(t *testing.T) {
	client := startStdioTestClient(t)
	defer client.close()

	root := t.TempDir()
	if err := os.WriteFile(filepath.Join(root, "helper.js"), []byte(`export function externalHelper(value) {
  return value.toUpperCase();
}
`), 0o644); err != nil {
		t.Fatal(err)
	}
	if err := os.WriteFile(filepath.Join(root, "jsconfig.json"), []byte(`{"include":["*.js","*.asp"]}`), 0o644); err != nil {
		t.Fatal(err)
	}
	uri := pathToFileURI(filepath.Join(root, "page.asp"))
	marked := markedDocument(`<script type="module">
import { externalHelper } from "./helper.js";
const value = externalHe<<<caret>>>lper("ada");
</script>`)
	client.request("initialize", map[string]any{
		"processId":    nil,
		"rootUri":      pathToFileURI(root),
		"capabilities": map[string]any{},
	})
	openClassicASPDocument(t, client, uri, marked.Text)
	definition := client.request("textDocument/definition", map[string]any{
		"textDocument": map[string]any{"uri": uri},
		"position":     marked.Position,
	})
	serialized := mustJSONText(t, definition.Result)
	for _, expected := range []string{"helper.js", `"line":0`, `"character":16`} {
		if !strings.Contains(serialized, expected) {
			t.Fatalf("workspace JavaScript definition missing %q: %s", expected, serialized)
		}
	}
}

func TestStdioParityReturnsJavaScriptCallHierarchyPlusCSSAndJavaScriptSymbols(t *testing.T) {
	client := startStdioTestClient(t)
	defer client.close()

	root := t.TempDir()
	uri := pathToFileURI(filepath.Join(root, "js-symbols.asp"))
	marked := markedDocument(`<style>
.panel { color: red; }
</style>
<script>
function renderCard(value) {
  return value;
}
function boot() {
  return render<<<caret>>>Card("ready");
}
</script>`)
	client.request("initialize", map[string]any{
		"processId":    nil,
		"rootUri":      pathToFileURI(root),
		"capabilities": map[string]any{},
	})
	openClassicASPDocument(t, client, uri, marked.Text)
	symbols := client.request("textDocument/documentSymbol", map[string]any{
		"textDocument": map[string]any{"uri": uri},
	})
	symbolText := mustJSONText(t, symbols.Result)
	if !strings.Contains(symbolText, "panel") || !strings.Contains(symbolText, "renderCard") {
		t.Fatalf("document symbols missing CSS or JS symbols: %s", symbolText)
	}

	folding := client.request("textDocument/foldingRange", map[string]any{
		"textDocument": map[string]any{"uri": uri},
	})
	if got := resultArrayLength(t, folding.Result); got <= 1 {
		t.Fatalf("folding ranges = %d, want more than 1: %s", got, mustJSONText(t, folding.Result))
	}

	hierarchy := client.request("textDocument/prepareCallHierarchy", map[string]any{
		"textDocument": map[string]any{"uri": uri},
		"position":     marked.Position,
	})
	if !strings.Contains(mustJSONText(t, hierarchy.Result), "renderCard") {
		t.Fatalf("call hierarchy missing renderCard: %s", mustJSONText(t, hierarchy.Result))
	}
	var hierarchyItems []any
	mustDecodeResult(t, hierarchy.Result, &hierarchyItems)
	if len(hierarchyItems) == 0 {
		t.Fatalf("empty call hierarchy: %s", mustJSONText(t, hierarchy.Result))
	}
	incoming := client.request("callHierarchy/incomingCalls", map[string]any{"item": hierarchyItems[0]})
	if !strings.Contains(mustJSONText(t, incoming.Result), "boot") {
		t.Fatalf("incoming calls missing boot: %s", mustJSONText(t, incoming.Result))
	}

	workspaceSymbols := client.request("workspace/symbol", map[string]any{"query": "render"})
	if !strings.Contains(mustJSONText(t, workspaceSymbols.Result), "renderCard") {
		t.Fatalf("workspace symbols missing renderCard: %s", mustJSONText(t, workspaceSymbols.Result))
	}
}

func TestStdioParityReturnsRichJavaScriptTypeInfoAndSemanticTokensForScriptTags(t *testing.T) {
	client := startStdioTestClient(t)
	defer client.close()

	root := t.TempDir()
	uri := pathToFileURI(filepath.Join(root, "go-js-rich-types.asp"))
	source := `<script>
/** @param {HTMLElement} element */
function activate(element) {
  element.dataset.active = "true";
}
class DashboardWidget {
  render(row) {
    return row.textContent;
  }
}
const formatter = new Intl.DateTimeFormat("en");
const clock = document.querySelector("#clientClock");
document.querySelectorAll(".customer-row").forEach((row) => {
  activate(row);
});
</script>`
	client.request("initialize", map[string]any{
		"processId":    nil,
		"rootUri":      pathToFileURI(root),
		"capabilities": map[string]any{},
	})
	openClassicASPDocument(t, client, uri, source)

	hoverCases := []struct {
		name     string
		offset   int
		expected string
	}{
		{name: "formatter", offset: strings.Index(source, "formatter"), expected: "Intl.DateTimeFormat"},
		{name: "clock", offset: strings.Index(source, "clock"), expected: "Element"},
		{name: "row", offset: strings.Index(source, "forEach((row)") + len("forEach(("), expected: "Element"},
		{name: "element", offset: strings.Index(source, "element)"), expected: "HTMLElement"},
	}
	for _, testCase := range hoverCases {
		hover := client.request("textDocument/hover", map[string]any{
			"textDocument": map[string]any{"uri": uri},
			"position":     positionAt(source, testCase.offset),
		})
		if !strings.Contains(mustJSONText(t, hover.Result), testCase.expected) {
			t.Fatalf("%s hover missing %q: %s", testCase.name, testCase.expected, mustJSONText(t, hover.Result))
		}
	}

	semanticTokens := client.request("textDocument/semanticTokens/full", map[string]any{
		"textDocument": map[string]any{"uri": uri},
	})
	decoded := decodeSemanticTokens(t, semanticTokens.Result)
	for _, want := range []struct {
		text      string
		tokenType int
	}{
		{"DashboardWidget", semanticTokenClass},
		{"render", semanticTokenMethod},
		{"dataset", semanticTokenProperty},
	} {
		if !hasTokenMatchingText(source, decoded, want.text, want.tokenType) {
			t.Fatalf("semantic tokens missing %s token type %d: %#v", want.text, want.tokenType, decoded)
		}
	}
	rowPosition := positionAt(source, strings.Index(source, "(row)")+1)
	if !hasSemanticToken(decoded, rowPosition["line"], rowPosition["character"], semanticTokenParameter, 0) {
		t.Fatalf("semantic tokens missing row parameter token at %#v: %#v", rowPosition, decoded)
	}
}

func TestStdioParityDefersFullJavaScriptSemanticTokensForLargeScriptTagsWhileRangeStaysImmediate(t *testing.T) {
	previousThreshold, hadThreshold := os.LookupEnv("ASP_LSP_TEST_SEMANTIC_TOKENS_LARGE_JAVASCRIPT_THRESHOLD")
	if err := os.Setenv("ASP_LSP_TEST_SEMANTIC_TOKENS_LARGE_JAVASCRIPT_THRESHOLD", "128"); err != nil {
		t.Fatal(err)
	}
	defer func() {
		if hadThreshold {
			_ = os.Setenv("ASP_LSP_TEST_SEMANTIC_TOKENS_LARGE_JAVASCRIPT_THRESHOLD", previousThreshold)
		} else {
			_ = os.Unsetenv("ASP_LSP_TEST_SEMANTIC_TOKENS_LARGE_JAVASCRIPT_THRESHOLD")
		}
	}()

	client := startStdioTestClient(t)
	defer client.close()

	root := t.TempDir()
	uri := pathToFileURI(filepath.Join(root, "go-js-large-semantic.asp"))
	filler := strings.Repeat(" ", 512)
	source := `<script>
` + filler + `
class LargeDashboardWidget {
  render(row) {
    return row.textContent;
  }
}
</script>`
	client.request("initialize", map[string]any{
		"processId": nil,
		"rootUri":   pathToFileURI(root),
		"capabilities": map[string]any{
			"workspace": map[string]any{
				"semanticTokens": map[string]any{"refreshSupport": true},
			},
		},
	})
	openClassicASPDocument(t, client, uri, source)

	classPosition := positionAt(source, strings.Index(source, "LargeDashboardWidget"))
	rangeTokens := client.request("textDocument/semanticTokens/range", map[string]any{
		"textDocument": map[string]any{"uri": uri},
		"range": map[string]any{
			"start": map[string]any{"line": classPosition["line"], "character": 0},
			"end":   map[string]any{"line": classPosition["line"] + 4, "character": 0},
		},
	})
	decodedRange := decodeSemanticTokens(t, rangeTokens.Result)
	if !hasTokenMatchingText(source, decodedRange, "LargeDashboardWidget", semanticTokenClass) ||
		!hasTokenMatchingText(source, decodedRange, "render", semanticTokenMethod) {
		t.Fatalf("range semantic tokens missing large JS class or method: %#v", decodedRange)
	}

	firstFull := client.request("textDocument/semanticTokens/full", map[string]any{
		"textDocument": map[string]any{"uri": uri},
	})
	decodedFirstFull := decodeSemanticTokens(t, firstFull.Result)
	if hasTokenMatchingText(source, decodedFirstFull, "LargeDashboardWidget", semanticTokenClass) {
		t.Fatalf("first full semantic tokens should defer large JS class token: %#v", decodedFirstFull)
	}
	client.waitForNotification("workspace/semanticTokens/refresh", "")

	secondFull := client.request("textDocument/semanticTokens/full", map[string]any{
		"textDocument": map[string]any{"uri": uri},
	})
	decodedSecondFull := decodeSemanticTokens(t, secondFull.Result)
	if !hasTokenMatchingText(source, decodedSecondFull, "LargeDashboardWidget", semanticTokenClass) ||
		!hasTokenMatchingText(source, decodedSecondFull, "render", semanticTokenMethod) {
		t.Fatalf("second full semantic tokens missing deferred JS tokens: %#v", decodedSecondFull)
	}
}

func writeAmbientTypes(t *testing.T, root string, packageName string, declaration string) {
	t.Helper()
	directory := filepath.Join(root, "node_modules", "@types", packageName)
	if err := os.MkdirAll(directory, 0o755); err != nil {
		t.Fatal(err)
	}
	if err := os.WriteFile(filepath.Join(directory, "package.json"), []byte(`{"name":"@types/`+packageName+`","version":"1.0.0","types":"index.d.ts"}`), 0o644); err != nil {
		t.Fatal(err)
	}
	if err := os.WriteFile(filepath.Join(directory, "index.d.ts"), []byte(declaration), 0o644); err != nil {
		t.Fatal(err)
	}
}
