package lspserver

import (
	"path/filepath"
	"reflect"
	"strings"
	"testing"

	"github.com/yottonoko/classic-web-system-lsp/internal/lsp"
)

func TestStdioParityKeepsLanguageFeaturesCurrentAfterRangedASPEdits(t *testing.T) {
	client := startStdioTestClient(t)
	defer client.close()

	root := t.TempDir()
	uri := pathToFileURI(filepath.Join(root, "go-ranged-edits.asp"))
	current := `<%
Option Explicit
Dim known
Response.Write missingName
%>
<style>.x { color: }</style>
<script>const = ;</script>`
	version := 1
	replace := func(needle string, replacement string) {
		start := strings.Index(current, needle)
		if start < 0 {
			t.Fatalf("missing %q in current source", needle)
		}
		end := start + len(needle)
		oldCurrent := current
		current = current[:start] + replacement + current[end:]
		version++
		if err := client.notify("textDocument/didChange", map[string]any{
			"textDocument": map[string]any{"uri": uri, "version": version},
			"contentChanges": []map[string]any{{
				"range": map[string]any{
					"start": positionAt(oldCurrent, start),
					"end":   positionAt(oldCurrent, end),
				},
				"text": replacement,
			}},
		}); err != nil {
			t.Fatal(err)
		}
	}
	client.request("initialize", map[string]any{
		"processId":    nil,
		"rootUri":      pathToFileURI(root),
		"capabilities": map[string]any{},
	})
	notifyConfiguration(t, client, map[string]any{"aspLsp": map[string]any{"diagnostics": map[string]any{"debounceMs": 0}}})
	notifyOpenClassicASPDocument(t, client, uri, current)
	client.waitForNotification("textDocument/publishDiagnostics", "")

	replace("missingName", "known")
	replace("color:", "color: red")
	replace("const = ;", "const ok = 1;")

	pulled := client.request("textDocument/diagnostic", map[string]any{
		"textDocument": map[string]any{"uri": uri},
	})
	serializedPulled := mustJSONText(t, pulled.Result)
	for _, unexpected := range []string{"missingName", "asp-lsp-css", "asp-lsp-typescript"} {
		if strings.Contains(serializedPulled, unexpected) {
			t.Fatalf("pulled diagnostics still contain %q: %s", unexpected, serializedPulled)
		}
	}
	completions := client.request("textDocument/completion", map[string]any{
		"textDocument": map[string]any{"uri": uri},
		"position":     positionAt(current, strings.Index(current, "Response.")+len("Response.")),
	})
	if !strings.Contains(mustJSONText(t, completions.Result), "Write") {
		t.Fatalf("completion missing Write after ranged edits: %s", mustJSONText(t, completions.Result))
	}
	hover := client.request("textDocument/hover", map[string]any{
		"textDocument": map[string]any{"uri": uri},
		"position":     positionAt(current, strings.Index(current[strings.Index(current, "Response.Write"):], "known")+strings.Index(current, "Response.Write")),
	})
	if !strings.Contains(mustJSONText(t, hover.Result), "Dim known") {
		t.Fatalf("hover missing Dim known after ranged edits: %s", mustJSONText(t, hover.Result))
	}
	semanticTokens := client.request("textDocument/semanticTokens/full", map[string]any{
		"textDocument": map[string]any{"uri": uri},
	})
	decoded := decodeSemanticTokens(t, semanticTokens.Result)
	knownOffset := strings.Index(current[strings.Index(current, "Response.Write"):], "known") + strings.Index(current, "Response.Write")
	knownPosition := positionAt(current, knownOffset)
	if !hasSemanticToken(decoded, knownPosition["line"], knownPosition["character"], semanticTokenVariable, 0) {
		t.Fatalf("semantic tokens missing known variable after ranged edits: %#v", decoded)
	}
}

func TestStdioParityReturnsVBScriptFoldingRangesForIfBranchesAndLoops(t *testing.T) {
	client := startStdioTestClient(t)
	defer client.close()

	root := t.TempDir()
	uri := pathToFileURI(filepath.Join(root, "go-vbscript-folding.asp"))
	source := `<%
If ready Then
  Response.Write 1
ElseIf other Then
  Response.Write 2
Else
  Response.Write 3
End If
If inlineReady Then Response.Write inlineReady
Do While ready
  Response.Write 4
Loop
While ready
  Response.Write 5
Wend
For index = 1 To 3
  Response.Write index
Next
For Each item In items
  Response.Write item
Next
%>`
	client.request("initialize", map[string]any{
		"processId":    nil,
		"rootUri":      pathToFileURI(root),
		"capabilities": map[string]any{},
	})
	openClassicASPDocument(t, client, uri, source)

	response := client.request("textDocument/foldingRange", map[string]any{
		"textDocument": map[string]any{"uri": uri},
	})
	var folding []lsp.FoldingRange
	mustDecodeResult(t, response.Result, &folding)
	for _, want := range []struct {
		start int
		end   int
	}{
		{1, 2},
		{3, 4},
		{5, 7},
		{9, 11},
		{12, 14},
		{15, 17},
		{18, 20},
	} {
		if !hasFoldingRange(folding, want.start, want.end) {
			t.Fatalf("folding ranges missing [%d, %d]: %#v", want.start, want.end, folding)
		}
	}
	for _, candidate := range folding {
		if candidate.StartLine == 8 {
			t.Fatalf("folding ranges should not include inline If at line 8: %#v", folding)
		}
	}
}

func TestStdioParityKeepsRemappedSelectionRangeParentsContainingChildRanges(t *testing.T) {
	client := startStdioTestClient(t)
	defer client.close()

	root := t.TempDir()
	uri := pathToFileURI(filepath.Join(root, "go-selection-ranges.asp"))
	source := `<style>
.panel { <% If ok Then %>color: red;<% End If %> }
</style>
<script>function boot(){ return <%= value %>; }</script>`
	client.request("initialize", map[string]any{
		"processId":    nil,
		"rootUri":      pathToFileURI(root),
		"capabilities": map[string]any{},
	})
	openClassicASPDocument(t, client, uri, source)

	response := client.request("textDocument/selectionRange", map[string]any{
		"textDocument": map[string]any{"uri": uri},
		"positions": []map[string]int{
			positionAt(source, strings.Index(source, "color")),
			positionAt(source, strings.Index(source, "return")),
		},
	})
	var selection []lsp.SelectionRange
	mustDecodeResult(t, response.Result, &selection)
	if len(selection) != 2 {
		t.Fatalf("selection ranges = %d, want 2: %s", len(selection), mustJSONText(t, response.Result))
	}
	for _, item := range selection {
		assertSelectionRangeParentsContainChildren(t, item)
	}
}

func TestStdioParityRoutesEmbeddedStructuralFeaturesThroughLanguageServices(t *testing.T) {
	client := startStdioTestClient(t)
	defer client.close()

	root := t.TempDir()
	uri := pathToFileURI(filepath.Join(root, "embedded-structures.asp"))
	source := `<main>
  <span>first</span><span>second</span>
</main>
<style>
:root { --accent: red; }
.card { color: var(--accent); }
</style>
<script>
function boot() {
const count = 1;
return count;
}
</script>`
	client.request("initialize", map[string]any{
		"processId":    nil,
		"rootUri":      pathToFileURI(root),
		"capabilities": map[string]any{},
	})
	openClassicASPDocument(t, client, uri, source)

	htmlHighlights := client.request("textDocument/documentHighlight", map[string]any{
		"textDocument": map[string]any{"uri": uri},
		"position":     positionAt(source, strings.Index(source, "span")+1),
	})
	if got := resultArrayLength(t, htmlHighlights.Result); got != 2 {
		t.Fatalf("HTML highlights = %d: %s", got, mustJSONText(t, htmlHighlights.Result))
	}
	cssHighlights := client.request("textDocument/documentHighlight", map[string]any{
		"textDocument": map[string]any{"uri": uri},
		"position":     positionAt(source, strings.Index(source, "--accent")+2),
	})
	if got := resultArrayLength(t, cssHighlights.Result); got != 2 {
		t.Fatalf("CSS highlights = %d: %s", got, mustJSONText(t, cssHighlights.Result))
	}

	symbols := mustJSONText(t, client.request("textDocument/documentSymbol", map[string]any{
		"textDocument": map[string]any{"uri": uri},
	}).Result)
	for _, name := range []string{"main", ".card", "boot"} {
		if !strings.Contains(symbols, name) {
			t.Fatalf("symbols missing %q: %s", name, symbols)
		}
	}

	rename := mustJSONText(t, client.request("textDocument/rename", map[string]any{
		"textDocument": map[string]any{"uri": uri},
		"position":     positionAt(source, strings.Index(source, "span")+1),
		"newName":      "em",
	}).Result)
	if strings.Count(rename, `"newText":"em"`) != 2 {
		t.Fatalf("paired tag rename touched an unrelated sibling: %s", rename)
	}

	fullRange := map[string]any{
		"start": map[string]any{"line": 0, "character": 0},
		"end":   positionAt(source, len(source)),
	}
	inlineValues := mustJSONText(t, client.request("textDocument/inlineValue", map[string]any{
		"textDocument": map[string]any{"uri": uri},
		"range":        fullRange,
		"context":      map[string]any{"frameId": 1, "stoppedLocation": fullRange},
	}).Result)
	if !strings.Contains(inlineValues, `"variableName":"count"`) || !strings.Contains(inlineValues, `"caseSensitiveLookup":true`) {
		t.Fatalf("JavaScript inline values missing count: %s", inlineValues)
	}
	monikers := mustJSONText(t, client.request("textDocument/moniker", map[string]any{
		"textDocument": map[string]any{"uri": uri},
		"position":     positionAt(source, strings.LastIndex(source, "count")+1),
	}).Result)
	if !strings.Contains(monikers, `"scheme":"asp-lsp-js"`) || !strings.Contains(monikers, `"unique":"project"`) {
		t.Fatalf("JavaScript moniker mismatch: %s", monikers)
	}

	cssOnType := client.request("textDocument/onTypeFormatting", map[string]any{
		"textDocument": map[string]any{"uri": uri},
		"position":     positionAt(source, strings.Index(source, ".card")),
		"ch":           "\n",
		"options":      map[string]any{"tabSize": 2, "insertSpaces": true},
	})
	if got := resultArrayLength(t, cssOnType.Result); got != 0 {
		t.Fatalf("CSS on-type formatting received VBScript indent edits: %s", mustJSONText(t, cssOnType.Result))
	}
	jsOnType := mustJSONText(t, client.request("textDocument/onTypeFormatting", map[string]any{
		"textDocument": map[string]any{"uri": uri},
		"position":     positionAt(source, strings.Index(source, "const count")),
		"ch":           "\n",
		"options":      map[string]any{"tabSize": 2, "insertSpaces": true},
	}).Result)
	if !strings.Contains(jsOnType, `"newText":"  "`) {
		t.Fatalf("JavaScript on-type formatting did not use JavaScript brace depth: %s", jsOnType)
	}
}

func TestStdioParitySupportsSelectionRangesLinkedEditingAndOnTypeFormatting(t *testing.T) {
	client := startStdioTestClient(t)
	defer client.close()

	root := t.TempDir()
	uri := pathToFileURI(filepath.Join(root, "go-structural-lsp.asp"))
	source := `<div><span>linked</span></div>
<%
Dim customerName
Response.Write customerName
%>`
	client.request("initialize", map[string]any{
		"processId":    nil,
		"rootUri":      pathToFileURI(root),
		"capabilities": map[string]any{},
	})
	openClassicASPDocument(t, client, uri, source)

	selectionResponse := client.request("textDocument/selectionRange", map[string]any{
		"textDocument": map[string]any{"uri": uri},
		"positions":    []map[string]int{positionAt(source, strings.LastIndex(source, "customerName")+2)},
	})
	var selection []lsp.SelectionRange
	mustDecodeResult(t, selectionResponse.Result, &selection)
	if len(selection) != 1 || selection[0].Parent == nil || !selectionRangeContainsLine(selection[0], 3) {
		t.Fatalf("selection range mismatch: %s", mustJSONText(t, selectionResponse.Result))
	}

	linkedResponse := client.request("textDocument/linkedEditingRange", map[string]any{
		"textDocument": map[string]any{"uri": uri},
		"position":     positionAt(source, strings.Index(source, "span")+1),
	})
	var linked lsp.LinkedEditingRanges
	mustDecodeResult(t, linkedResponse.Result, &linked)
	if len(linked.Ranges) < 2 {
		t.Fatalf("linked editing ranges mismatch: %s", mustJSONText(t, linkedResponse.Result))
	}

	onTypeEdits := textEdits(t, client.request("textDocument/onTypeFormatting", map[string]any{
		"textDocument": map[string]any{"uri": uri},
		"position":     map[string]any{"line": 4, "character": 0},
		"ch":           "\n",
		"options":      map[string]any{"tabSize": 2, "insertSpaces": true},
	}).Result)
	if len(onTypeEdits) != 0 {
		t.Fatalf("on-type newline formatting mismatch: %s", mustJSONText(t, onTypeEdits))
	}
}

func TestStdioParityReturnsHTMLCloseTagEditsOnlyForHTMLOpeningTagsOnGreaterThan(t *testing.T) {
	client := startStdioTestClient(t)
	defer client.close()

	root := t.TempDir()
	client.request("initialize", map[string]any{
		"processId":    nil,
		"rootUri":      pathToFileURI(root),
		"capabilities": map[string]any{},
	})
	for _, testCase := range []struct {
		name     string
		text     string
		expected []lsp.TextEdit
	}{
		{
			name: "div",
			text: "<div>",
			expected: []lsp.TextEdit{{
				Range:   lsp.Range{Start: lsp.Position{Line: 0, Character: 5}, End: lsp.Position{Line: 0, Character: 5}},
				NewText: "</div>",
			}},
		},
		{name: "br", text: "<br>", expected: []lsp.TextEdit{}},
		{name: "closing", text: "</div>", expected: []lsp.TextEdit{}},
		{
			name: "quoted-greater",
			text: `<div title=">">`,
			expected: []lsp.TextEdit{{
				Range:   lsp.Range{Start: lsp.Position{Line: 0, Character: 15}, End: lsp.Position{Line: 0, Character: 15}},
				NewText: "</div>",
			}},
		},
		{name: "asp-close", text: `<% Response.Write "ok" %>`, expected: []lsp.TextEdit{}},
	} {
		t.Run(testCase.name, func(t *testing.T) {
			uri := pathToFileURI(filepath.Join(root, "go-tag-complete-"+testCase.name+".asp"))
			edits := requestOnTypeFormatting(t, client, uri, testCase.text, positionAt(testCase.text, len(testCase.text)), ">")
			if !reflect.DeepEqual(edits, testCase.expected) {
				t.Fatalf("%s on-type edits = %#v, want %#v", testCase.name, edits, testCase.expected)
			}
		})
	}
}

func TestStdioParityDoesNotAutoCloseApostrophesOnType(t *testing.T) {
	client := startStdioTestClient(t)
	defer client.close()

	root := t.TempDir()
	initialize := client.request("initialize", map[string]any{
		"processId":    nil,
		"rootUri":      pathToFileURI(root),
		"capabilities": map[string]any{},
	})
	var initializeResult struct {
		Capabilities struct {
			DocumentOnTypeFormattingProvider struct {
				MoreTriggerCharacter []string `json:"moreTriggerCharacter"`
			} `json:"documentOnTypeFormattingProvider"`
		} `json:"capabilities"`
	}
	mustDecodeResult(t, initialize.Result, &initializeResult)
	for _, trigger := range initializeResult.Capabilities.DocumentOnTypeFormattingProvider.MoreTriggerCharacter {
		if trigger == "'" {
			t.Fatalf("initialize on-type formatting provider should not advertise apostrophe trigger: %s", mustJSONText(t, initialize.Result))
		}
	}

	for _, testCase := range []struct {
		name        string
		text        string
		quoteOffset int
	}{
		{name: "vbscript", text: "<% Response.Write '\n%>"},
		{name: "html", text: "<div title='>"},
		{name: "css", text: "<style>.x::before { content: '</style>"},
		{name: "javascript", text: "<script>const value = '</script>"},
		{name: "jscript", text: "<%@ LANGUAGE=\"JScript\" %>\n<% var value = '\n%>"},
		{name: "existing-close", text: "<script>const value = '';</script>"},
		{name: "generated-close", text: "<script>const value = '';</script>", quoteOffset: len("<script>const value = ''")},
	} {
		t.Run(testCase.name, func(t *testing.T) {
			offset := testCase.quoteOffset
			if offset == 0 {
				offset = strings.Index(testCase.text, "'") + 1
			}
			uri := pathToFileURI(filepath.Join(root, "go-apostrophe-"+testCase.name+".asp"))
			edits := requestOnTypeFormatting(t, client, uri, testCase.text, positionAt(testCase.text, offset), "'")
			if len(edits) != 0 {
				t.Fatalf("%s apostrophe on-type edits = %#v, want none", testCase.name, edits)
			}
		})
	}
}

func TestStdioParityReturnsHoverInlayHintsAndSemanticTokensForImplicitVBScriptVariables(t *testing.T) {
	client := startStdioTestClient(t)
	defer client.close()

	root := t.TempDir()
	uri := pathToFileURI(filepath.Join(root, "go-implicit-vbscript.asp"))
	source := `<%
a = 1
currencyValue = CCur(1)
nullValue = Null
emptyValue = Empty
Response.Write a
%>`
	client.request("initialize", map[string]any{
		"processId":    nil,
		"rootUri":      pathToFileURI(root),
		"capabilities": map[string]any{},
	})
	notifyConfiguration(t, client, map[string]any{"aspLsp": map[string]any{
		"inlayHints": map[string]any{"functionReturnTypes": true, "variableTypes": true},
	}})
	openClassicASPDocument(t, client, uri, source)
	hover := client.request("textDocument/hover", map[string]any{
		"textDocument": map[string]any{"uri": uri},
		"position":     map[string]any{"line": 1, "character": 0},
	})
	hoverText := mustJSONText(t, hover.Result)
	if !strings.Contains(hoverText, vbscriptHoverCodeBlockJSON("(global) Dim a As 1")) || strings.Contains(hoverText, "Implicit VBScript variable") {
		t.Fatalf("implicit variable hover mismatch: %s", hoverText)
	}
	currencyHover := client.request("textDocument/hover", map[string]any{
		"textDocument": map[string]any{"uri": uri},
		"position":     positionAt(source, strings.Index(source, "CCur")),
	})
	if !strings.Contains(mustJSONText(t, currencyHover.Result), "Function CCur(value) As Currency") {
		t.Fatalf("currency hover mismatch: %s", mustJSONText(t, currencyHover.Result))
	}
	inlayHints := client.request("textDocument/inlayHint", map[string]any{
		"textDocument": map[string]any{"uri": uri},
		"range":        map[string]any{"start": map[string]any{"line": 0, "character": 0}, "end": map[string]any{"line": 7, "character": 0}},
	})
	serializedInlayHints := mustJSONText(t, inlayHints.Result)
	for _, expected := range []string{"As 1", "As Currency", "As Null", "As Empty"} {
		if !strings.Contains(serializedInlayHints, expected) {
			t.Fatalf("implicit variable inlay hints missing %q: %s", expected, serializedInlayHints)
		}
	}
	semanticTokens := client.request("textDocument/semanticTokens/full", map[string]any{
		"textDocument": map[string]any{"uri": uri},
	})
	decoded := decodeSemanticTokens(t, semanticTokens.Result)
	if !hasSemanticToken(decoded, 5, len("Response.Write "), semanticTokenVariable, 0) {
		t.Fatalf("semantic tokens missing implicit variable usage: %#v", decoded)
	}
}

func requestOnTypeFormatting(t *testing.T, client *stdioTestClient, uri string, text string, position map[string]int, ch string) []lsp.TextEdit {
	t.Helper()
	openClassicASPDocument(t, client, uri, text)
	response := client.request("textDocument/onTypeFormatting", map[string]any{
		"textDocument": map[string]any{"uri": uri},
		"position":     position,
		"ch":           ch,
		"options":      map[string]any{"tabSize": 2, "insertSpaces": true},
	})
	var edits []lsp.TextEdit
	mustDecodeResult(t, response.Result, &edits)
	return edits
}

func TestStdioParityReusesSemanticTokensFullForSameDocumentVersion(t *testing.T) {
	client := startStdioTestClient(t)
	defer client.close()

	root := t.TempDir()
	uri := pathToFileURI(filepath.Join(root, "go-semantic-cache.asp"))
	source := `<%
Dim total
total = 1
Response.Write total
%>`
	client.request("initialize", map[string]any{
		"processId":    nil,
		"rootUri":      pathToFileURI(root),
		"capabilities": map[string]any{},
	})
	notifyConfiguration(t, client, map[string]any{"aspLsp": map[string]any{
		"debug":       map[string]any{"output": "verbose"},
		"diagnostics": map[string]any{"debounceMs": 0},
	}})
	openClassicASPDocument(t, client, uri, source)

	first := client.request("textDocument/semanticTokens/full", map[string]any{
		"textDocument": map[string]any{"uri": uri},
	})
	firstData := semanticTokenData(t, first.Result)
	if len(firstData) == 0 {
		t.Fatalf("first semantic tokens/full returned no data: %s", mustJSONText(t, first.Result))
	}
	second := client.request("textDocument/semanticTokens/full", map[string]any{
		"textDocument": map[string]any{"uri": uri},
	})
	if !reflect.DeepEqual(semanticTokenData(t, second.Result), firstData) {
		t.Fatalf("second semantic tokens/full did not reuse data\nfirst:  %s\nsecond: %s", mustJSONText(t, first.Result), mustJSONText(t, second.Result))
	}
	client.waitForLogContaining("semanticTokens.full.cacheHit")
}

func hasFoldingRange(ranges []lsp.FoldingRange, startLine int, endLine int) bool {
	for _, candidate := range ranges {
		if candidate.StartLine == startLine && candidate.EndLine == endLine {
			return true
		}
	}
	return false
}

func assertSelectionRangeParentsContainChildren(t *testing.T, item lsp.SelectionRange) {
	t.Helper()
	for current := item; current.Parent != nil; current = *current.Parent {
		if !rangeContains(current.Parent.Range, current.Range) {
			t.Fatalf("selection parent does not contain child: parent=%#v child=%#v", current.Parent.Range, current.Range)
		}
	}
}

func selectionRangeContainsLine(item lsp.SelectionRange, line int) bool {
	for current := &item; current != nil; current = current.Parent {
		if current.Range.Start.Line <= line && current.Range.End.Line >= line {
			return true
		}
	}
	return false
}

func rangeContains(parent lsp.Range, child lsp.Range) bool {
	return comparePosition(parent.Start, child.Start) <= 0 && comparePosition(parent.End, child.End) >= 0
}

func comparePosition(left lsp.Position, right lsp.Position) int {
	if left.Line != right.Line {
		return left.Line - right.Line
	}
	return left.Character - right.Character
}

func TestStdioParityReturnsHoverInlayHintsAndCompletionsForVBScriptUnionTypes(t *testing.T) {
	client := startStdioTestClient(t)
	defer client.close()

	root := t.TempDir()
	uri := pathToFileURI(filepath.Join(root, "go-union-vbscript.asp"))
	source := `<%
Class FirstThing
  Public SharedName
  Public OnlyFirst
End Class
Class SecondThing
  Public SharedName
End Class
x = 1
x = "a"
Dim unknownGlobal
Function UnknownReturn()
End Function
Dim both
Set both = New FirstThing
Set both = New SecondThing
both.SharedName
%>`
	client.request("initialize", map[string]any{
		"processId":    nil,
		"rootUri":      pathToFileURI(root),
		"capabilities": map[string]any{},
	})
	notifyConfiguration(t, client, map[string]any{"aspLsp": map[string]any{
		"inlayHints": map[string]any{
			"functionReturnTypes": true,
			"scopeMarkers":        map[string]any{"global": true},
			"variableTypes":       true,
		},
	}})
	openClassicASPDocument(t, client, uri, source)
	hover := client.request("textDocument/hover", map[string]any{
		"textDocument": map[string]any{"uri": uri},
		"position":     positionAt(source, strings.Index(source, "x = 1")),
	})
	hoverText := mustJSONText(t, hover.Result)
	if !strings.Contains(hoverText, vbscriptHoverCodeBlockJSON("(global) Dim x As Number | String")) || strings.Contains(hoverText, "variable (global)") {
		t.Fatalf("union hover mismatch: %s", hoverText)
	}
	inlayHints := client.request("textDocument/inlayHint", map[string]any{
		"textDocument": map[string]any{"uri": uri},
		"range":        map[string]any{"start": map[string]any{"line": 0, "character": 0}, "end": map[string]any{"line": 20, "character": 0}},
	})
	serializedInlayHints := mustJSONText(t, inlayHints.Result)
	for _, expected := range []string{"(global) As Number | String", "(global) As Variant", "As Variant"} {
		if !strings.Contains(serializedInlayHints, expected) {
			t.Fatalf("union inlay hints missing %q: %s", expected, serializedInlayHints)
		}
	}
	completions := completionLabels(client.request("textDocument/completion", map[string]any{
		"textDocument": map[string]any{"uri": uri},
		"position":     positionAt(source, strings.Index(source, "both.SharedName")+len("both.")),
	}).Result)
	if !completions.contains("SharedName") || completions.contains("OnlyFirst") {
		t.Fatalf("union member completions mismatch: %#v", completions)
	}
}

func TestStdioParityKeepsVBScriptObjectInferenceAfterSetNothingAssignments(t *testing.T) {
	client := startStdioTestClient(t)
	defer client.close()

	root := t.TempDir()
	uri := pathToFileURI(filepath.Join(root, "go-vbscript-set-nothing-object.asp"))
	source := `<%
Class Customer
  Public Name
End Class
Sub Demo()
  Dim a
  Set a = New Customer
  Set a = Nothing
  a.Name
End Sub
%>`
	client.request("initialize", map[string]any{
		"processId":    nil,
		"rootUri":      pathToFileURI(root),
		"capabilities": map[string]any{},
	})
	notifyConfiguration(t, client, map[string]any{"aspLsp": map[string]any{
		"inlayHints": map[string]any{"variableTypes": true},
		"vbscript":   map[string]any{"typeChecking": "strict"},
	}})
	openClassicASPDocument(t, client, uri, source)

	completions := completionLabels(client.request("textDocument/completion", map[string]any{
		"textDocument": map[string]any{"uri": uri},
		"position":     positionAt(source, strings.Index(source, "a.Name")+len("a.")),
	}).Result)
	if !completions.contains("Name") {
		t.Fatalf("Set Nothing member completions missing Name: %#v", completions)
	}

	typeDefinition := client.request("textDocument/typeDefinition", map[string]any{
		"textDocument": map[string]any{"uri": uri},
		"position":     positionAt(source, strings.Index(source, "a.Name")),
	})
	if !strings.Contains(mustJSONText(t, typeDefinition.Result), `"line":1`) {
		t.Fatalf("Set Nothing typeDefinition did not resolve Customer: %s", mustJSONText(t, typeDefinition.Result))
	}

	inlayHints := client.request("textDocument/inlayHint", map[string]any{
		"textDocument": map[string]any{"uri": uri},
		"range":        map[string]any{"start": map[string]any{"line": 0, "character": 0}, "end": map[string]any{"line": 11, "character": 0}},
	})
	if !strings.Contains(mustJSONText(t, inlayHints.Result), "As Customer | Nothing") {
		t.Fatalf("Set Nothing inlay hints missing union type: %s", mustJSONText(t, inlayHints.Result))
	}

	diagnostics := client.request("textDocument/diagnostic", map[string]any{
		"textDocument": map[string]any{"uri": uri},
	})
	diagnosticText := mustJSONText(t, diagnostics.Result)
	for _, forbidden := range []string{"setScalar", "typeMismatch", "missingMember"} {
		if strings.Contains(diagnosticText, forbidden) {
			t.Fatalf("Set Nothing diagnostics included %s: %s", forbidden, diagnosticText)
		}
	}
}

func TestStdioParityFallsBackUnknownVBScriptTypesToVariantAndMarksGlobalVariables(t *testing.T) {
	client := startStdioTestClient(t)
	defer client.close()

	root := t.TempDir()
	uri := pathToFileURI(filepath.Join(root, "go-vbscript-unknown-types-global-markers.asp"))
	source := `<%
Dim rowClass
rowClass = "customer-row"
rowClass = 1
Dim unknownGlobal
Const UnknownConst = MissingValue
Function UnknownReturn()
End Function
Dim sharedValue
Sub Render()
  sharedValue = 1
  Dim localValue
  nestedImplicit = 1
End Sub
%>`
	client.request("initialize", map[string]any{
		"processId":    nil,
		"rootUri":      pathToFileURI(root),
		"capabilities": map[string]any{},
	})
	notifyConfiguration(t, client, map[string]any{"aspLsp": map[string]any{
		"inlayHints": map[string]any{"variableTypes": true},
		"vbscript":   map[string]any{"typeChecking": "strict"},
	}})
	openClassicASPDocument(t, client, uri, source)

	defaultHints := mustJSONText(t, client.request("textDocument/inlayHint", map[string]any{
		"textDocument": map[string]any{"uri": uri},
		"range":        map[string]any{"start": map[string]any{"line": 0, "character": 0}, "end": positionAt(source, len(source))},
	}).Result)
	for _, expected := range []string{"As String | Number", "As Variant"} {
		if !strings.Contains(defaultHints, expected) {
			t.Fatalf("unknown/global default hints missing %q: %s", expected, defaultHints)
		}
	}
	if strings.Contains(defaultHints, "(global)") || strings.Contains(defaultHints, "(local)") {
		t.Fatalf("unknown/global default hints included scope marker: %s", defaultHints)
	}

	notifyConfiguration(t, client, map[string]any{"aspLsp": map[string]any{
		"inlayHints": map[string]any{
			"variableTypes": true,
			"scopeMarkers":  map[string]any{"global": true, "local": true, "uncertain": true},
		},
		"vbscript": map[string]any{"typeChecking": "strict"},
	}})
	markedHints := mustJSONText(t, client.request("textDocument/inlayHint", map[string]any{
		"textDocument": map[string]any{"uri": uri},
		"range":        map[string]any{"start": map[string]any{"line": 0, "character": 0}, "end": positionAt(source, len(source))},
	}).Result)
	for _, expected := range []string{"(local) As Variant", "(global) As 1"} {
		if !strings.Contains(markedHints, expected) {
			t.Fatalf("unknown/global marked hints missing %q: %s", expected, markedHints)
		}
	}

	for _, hoverCase := range []struct {
		needle   string
		expected string
	}{
		{needle: "unknownGlobal", expected: vbscriptHoverCodeBlockJSON("(global) Dim unknownGlobal As Variant")},
		{needle: "UnknownConst", expected: vbscriptHoverCodeBlockJSON("(global) Const UnknownConst As Variant")},
		{needle: "sharedValue =", expected: vbscriptHoverCodeBlockJSON("(global) Dim sharedValue As 1")},
		{needle: "nestedImplicit", expected: vbscriptHoverCodeBlockJSON("(global) Dim nestedImplicit As 1")},
	} {
		hover := mustJSONText(t, client.request("textDocument/hover", map[string]any{
			"textDocument": map[string]any{"uri": uri},
			"position":     positionAt(source, strings.Index(source, hoverCase.needle)),
		}).Result)
		if !strings.Contains(hover, hoverCase.expected) {
			t.Fatalf("hover for %s missing %q: %s", hoverCase.needle, hoverCase.expected, hover)
		}
		if strings.Contains(hover, "VBScript variable") {
			t.Fatalf("hover for %s included legacy variable wording: %s", hoverCase.needle, hover)
		}
	}

	diagnostics := mustJSONText(t, client.request("textDocument/diagnostic", map[string]any{
		"textDocument": map[string]any{"uri": uri},
	}).Result)
	if strings.Contains(diagnostics, "asp-lsp-vbscript-type") {
		t.Fatalf("unknown/global strict diagnostics mismatch: %s", diagnostics)
	}
}

func TestStdioParityAppliesSemanticTokenDeltaEditsAfterRangedDocumentChanges(t *testing.T) {
	client := startStdioTestClient(t)
	defer client.close()

	root := t.TempDir()
	uri := pathToFileURI(filepath.Join(root, "go-vbscript-semantic-delta-edits.asp"))
	version := 1
	source := `<%
Dim count
count = (1)
If (count <> 0) And (count < 10) Then
  Response.Write CStr(count)
End If
%>`
	client.request("initialize", map[string]any{
		"processId":    nil,
		"rootUri":      pathToFileURI(root),
		"capabilities": map[string]any{},
	})
	notifyOpenClassicASPDocument(t, client, uri, source)
	client.waitForNotification("textDocument/publishDiagnostics", "")
	initialFull := client.request("textDocument/semanticTokens/full", map[string]any{
		"textDocument": map[string]any{"uri": uri},
	})
	initialData := semanticTokenData(t, initialFull.Result)
	oldText := "count <> 0"
	newText := "count <> 1"
	startOffset := strings.Index(source, oldText)
	if startOffset < 0 {
		t.Fatalf("missing %q", oldText)
	}
	oldSource := source
	source = source[:startOffset] + newText + source[startOffset+len(oldText):]
	version++
	if err := client.notify("textDocument/didChange", map[string]any{
		"textDocument": map[string]any{"uri": uri, "version": version},
		"contentChanges": []map[string]any{{
			"range": map[string]any{
				"start": positionAt(oldSource, startOffset),
				"end":   positionAt(oldSource, startOffset+len(oldText)),
			},
			"text": newText,
		}},
	}); err != nil {
		t.Fatal(err)
	}
	client.waitForNotification("textDocument/publishDiagnostics", "")

	firstDelta := client.request("textDocument/semanticTokens/full/delta", map[string]any{
		"textDocument":     map[string]any{"uri": uri},
		"previousResultId": semanticResultID(t, initialFull.Result),
	})
	firstApplied := applySemanticTokenDeltaEdits(t, initialData, firstDelta.Result)
	firstFresh := client.request("textDocument/semanticTokens/full", map[string]any{
		"textDocument": map[string]any{"uri": uri},
	})
	if !reflect.DeepEqual(firstApplied, semanticTokenData(t, firstFresh.Result)) {
		t.Fatalf("first semantic delta did not match fresh full tokens")
	}
	if !hasTokenMatchingText(source, decodeSemanticTokenData(firstApplied), "<>", semanticTokenOperator) {
		t.Fatalf("semantic tokens missing <> operator after ranged edit: %#v", decodeSemanticTokenData(firstApplied))
	}

	insertOffset := strings.Index(source, "CStr(count)") + len("CStr(")
	inserted := notifyTypedInsertion(t, client, uri, source, version, insertOffset, " ")
	source = inserted.Text
	secondDelta := client.request("textDocument/semanticTokens/full/delta", map[string]any{
		"textDocument":     map[string]any{"uri": uri},
		"previousResultId": semanticResultID(t, firstFresh.Result),
	})
	secondApplied := applySemanticTokenDeltaEdits(t, semanticTokenData(t, firstFresh.Result), secondDelta.Result)
	secondFresh := client.request("textDocument/semanticTokens/full", map[string]any{
		"textDocument": map[string]any{"uri": uri},
	})
	if !reflect.DeepEqual(secondApplied, semanticTokenData(t, secondFresh.Result)) {
		t.Fatalf("second semantic delta did not match fresh full tokens")
	}
	decoded := decodeSemanticTokenData(secondApplied)
	callOpenOffset := strings.Index(source[strings.Index(source, "CStr"):], "(") + strings.Index(source, "CStr")
	callOpen := positionAt(source, callOpenOffset)
	if !hasSemanticToken(decoded, callOpen["line"], callOpen["character"], semanticTokenOperator, 0) {
		t.Fatalf("semantic tokens missing CStr open paren operator: %#v", decoded)
	}
}

func TestStdioParityReusesSemanticTokenFullDataAcrossSingleEmbeddedLanguageEdits(t *testing.T) {
	client := startStdioTestClient(t)
	defer client.close()

	root := t.TempDir()
	uri := pathToFileURI(filepath.Join(root, "go-semantic-dirty-range-reuse.asp"))
	source := `<div class="box"><span data-name="before">Title</span></div>
<style>
.box { color: red; }
</style>
<script>
const button = document.querySelector(".box");
button.dataset.name = "before";
</script>
<%
Function SharedToken()
End Function
Dim localValue
Response.Write SharedToken()
%>`
	version := 1
	client.request("initialize", map[string]any{
		"processId":    nil,
		"rootUri":      pathToFileURI(root),
		"capabilities": map[string]any{},
	})
	notifyConfiguration(t, client, verboseImmediateDiagnosticsSettings())
	notifyOpenClassicASPDocument(t, client, uri, source)
	client.waitForNotification("textDocument/publishDiagnostics", "")
	full := client.request("textDocument/semanticTokens/full", map[string]any{
		"textDocument": map[string]any{"uri": uri},
	})
	editAndAssert := func(needle string, replacement string, expectedNeedle string, expectedType int) {
		version++
		source = notifyNeedleReplacement(t, client, uri, source, version, needle, replacement)
		delta := client.request("textDocument/semanticTokens/full/delta", map[string]any{
			"textDocument":     map[string]any{"uri": uri},
			"previousResultId": semanticResultID(t, full.Result),
		})
		applied := applySemanticTokenDeltaEdits(t, semanticTokenData(t, full.Result), delta.Result)
		fresh := client.request("textDocument/semanticTokens/full", map[string]any{
			"textDocument": map[string]any{"uri": uri},
		})
		if !reflect.DeepEqual(applied, semanticTokenData(t, fresh.Result)) {
			t.Fatalf("semantic delta for %q did not match fresh full tokens", needle)
		}
		decoded := decodeSemanticTokenData(applied)
		for _, expected := range []struct {
			text      string
			tokenType int
		}{
			{"querySelector", semanticTokenMethod},
			{"color", semanticTokenProperty},
			{"SharedToken", semanticTokenFunction},
			{expectedNeedle, expectedType},
		} {
			if !hasTokenMatchingText(source, decoded, expected.text, expected.tokenType) {
				t.Fatalf("semantic tokens missing %s type %d after %q edit: %#v", expected.text, expected.tokenType, needle, decoded)
			}
		}
		full = fresh
	}
	editAndAssert("before", "after", "SharedToken", semanticTokenFunction)
	editAndAssert("red", "blue", "color", semanticTokenProperty)
	editAndAssert("button.dataset.name", "button.dataset.value", "querySelector", semanticTokenMethod)
	editAndAssert("localValue", "otherValue", "otherValue", semanticTokenVariable)
	if !strings.Contains(mustJSONText(t, client.drainNotifications("window/logMessage")), "semanticTokens.full.incrementalReuse") {
		t.Fatalf("semantic token reuse log was not emitted")
	}
}
