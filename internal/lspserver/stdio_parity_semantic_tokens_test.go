package lspserver

import (
	"path/filepath"
	"strings"
	"testing"
)

const (
	semanticTokenKeyword = 0
	semanticTokenString  = 8
)

func TestStdioParityReturnsSemanticTokensForClassicASPIncludeDirectives(t *testing.T) {
	client := startStdioTestClient(t)
	defer client.close()

	root := t.TempDir()
	uri := pathToFileURI(filepath.Join(root, "include-semantic.asp"))
	source := `<!-- #include file="includes/data.inc" -->
<% Response.Write "ok" %>`
	client.request("initialize", map[string]any{
		"processId":    nil,
		"rootUri":      pathToFileURI(root),
		"capabilities": map[string]any{},
	})
	notifyOpenClassicASPDocument(t, client, uri, source)
	waitForDiagnosticsContaining(t, client, "")

	links := client.request("textDocument/documentLink", map[string]any{
		"textDocument": map[string]any{"uri": uri},
	})
	linkText := mustJSONText(t, links.Result)
	for _, expected := range []string{`"character":19`, `"character":38`} {
		if !strings.Contains(linkText, expected) {
			t.Fatalf("include document link range missing %s: %s", expected, linkText)
		}
	}

	semanticTokens := client.request("textDocument/semanticTokens/full", map[string]any{
		"textDocument": map[string]any{"uri": uri},
	})
	decoded := decodeSemanticTokens(t, semanticTokens.Result)
	if !hasTokenMatchingText(source, decoded, "#include", semanticTokenKeyword) {
		t.Fatalf("semantic tokens missing #include keyword: %#v", decoded)
	}
	if !hasTokenMatchingText(source, decoded, "file", semanticTokenProperty) {
		t.Fatalf("semantic tokens missing file property: %#v", decoded)
	}
	if !hasTokenMatchingText(source, decoded, `"includes/data.inc"`, semanticTokenString) {
		t.Fatalf("semantic tokens missing include path string: %#v", decoded)
	}
}

func TestStdioParityMarksASPDirectiveDelimitersAsKeywordSemanticTokens(t *testing.T) {
	client := startStdioTestClient(t)
	defer client.close()

	root := t.TempDir()
	uri := pathToFileURI(filepath.Join(root, "directive-semantic.asp"))
	source := `<%@ Language="VBScript" CodePage=65001 %>`
	client.request("initialize", map[string]any{
		"processId":    nil,
		"rootUri":      pathToFileURI(root),
		"capabilities": map[string]any{},
	})
	notifyOpenClassicASPDocument(t, client, uri, source)

	semanticTokens := client.request("textDocument/semanticTokens/full", map[string]any{
		"textDocument": map[string]any{"uri": uri},
	})
	decoded := decodeSemanticTokens(t, semanticTokens.Result)
	if !hasTokenMatchingText(source, decoded, "<%@", semanticTokenKeyword) {
		t.Fatalf("semantic tokens missing directive open keyword: %#v", decoded)
	}
	if !hasTokenMatchingText(source, decoded, "%>", semanticTokenKeyword) {
		t.Fatalf("semantic tokens missing directive close keyword: %#v", decoded)
	}
}

func TestStdioParityMarksASPExpressionEqualsDelimiterAsKeywordSemanticToken(t *testing.T) {
	client := startStdioTestClient(t)
	defer client.close()

	root := t.TempDir()
	uri := pathToFileURI(filepath.Join(root, "asp-expression-semantic.asp"))
	source := `<%= title %>`
	equalsPosition := positionAt(source, strings.Index(source, "<%=")+len("<%"))
	client.request("initialize", map[string]any{
		"processId":    nil,
		"rootUri":      pathToFileURI(root),
		"capabilities": map[string]any{},
	})
	notifyOpenClassicASPDocument(t, client, uri, source)
	waitForDiagnosticsContaining(t, client, "")

	full := client.request("textDocument/semanticTokens/full", map[string]any{
		"textDocument": map[string]any{"uri": uri},
	})
	decodedFull := decodeSemanticTokens(t, full.Result)
	if !hasSemanticToken(decodedFull, equalsPosition["line"], equalsPosition["character"], semanticTokenKeyword, 0) {
		t.Fatalf("full semantic tokens missing ASP expression equals keyword: %#v", decodedFull)
	}

	rangeTokens := client.request("textDocument/semanticTokens/range", map[string]any{
		"textDocument": map[string]any{"uri": uri},
		"range": map[string]any{
			"start": map[string]any{"line": 0, "character": 0},
			"end":   map[string]any{"line": 0, "character": len(source)},
		},
	})
	decodedRange := decodeSemanticTokens(t, rangeTokens.Result)
	if !hasSemanticToken(decodedRange, equalsPosition["line"], equalsPosition["character"], semanticTokenKeyword, 0) {
		t.Fatalf("range semantic tokens missing ASP expression equals keyword: %#v", decodedRange)
	}

	delta := client.request("textDocument/semanticTokens/full/delta", map[string]any{
		"textDocument":     map[string]any{"uri": uri},
		"previousResultId": semanticResultID(t, full.Result),
	})
	if !strings.Contains(mustJSONText(t, delta.Result), "edits") {
		t.Fatalf("semantic token delta missing edits: %s", mustJSONText(t, delta.Result))
	}
}

func TestStdioParityMarksVBScriptParenthesesAndComparisonOperatorsOnInitialSemanticTokenRequests(t *testing.T) {
	client := startStdioTestClient(t)
	defer client.close()

	root := t.TempDir()
	uri := pathToFileURI(filepath.Join(root, "vbscript-startup-operators.asp"))
	source := `<%
If (count <> 0) And (count <= 10) Then
  Response.Write(count >= 1)
End If
%>`
	client.request("initialize", map[string]any{
		"processId":    nil,
		"rootUri":      pathToFileURI(root),
		"capabilities": map[string]any{},
	})
	notifyOpenClassicASPDocument(t, client, uri, source)
	waitForDiagnosticsContaining(t, client, "")

	semanticTokens := client.request("textDocument/semanticTokens/full", map[string]any{
		"textDocument": map[string]any{"uri": uri},
	})
	decoded := decodeSemanticTokens(t, semanticTokens.Result)
	for _, operator := range []string{"(", ")", "<>", "<=", ">="} {
		position := positionAt(source, strings.Index(source, operator))
		if !hasSemanticToken(decoded, position["line"], position["character"], semanticTokenOperator, 0) {
			t.Fatalf("semantic tokens missing %s operator at %#v: %#v", operator, position, decoded)
		}
	}
}

func TestStdioParityAddsVBScriptSymbolSemanticTokenTypesAndModifiers(t *testing.T) {
	client := startStdioTestClient(t)
	defer client.close()

	root := t.TempDir()
	uri := pathToFileURI(filepath.Join(root, "vbscript-symbol-semantic-tokens.asp"))
	source := `<%
Class Customer
  Public Name
  Public Default Property Get DisplayName()
    DisplayName = Name
  End Property
End Class
Const MaxCount = 10
Dim flags
flags = MaxCount Xor 2 Eqv 1
flags = (MaxCount <> 0) And (MaxCount <= 20) And (MaxCount >= 1)
Sub Render(ByVal metricMap, output)
  Response.Write MaxCount + vbCrLf
  Set output = Nothing
End Sub
%>`
	client.request("initialize", map[string]any{
		"processId":    nil,
		"rootUri":      pathToFileURI(root),
		"capabilities": map[string]any{},
	})
	notifyOpenClassicASPDocument(t, client, uri, source)
	waitForDiagnosticsContaining(t, client, "")

	semanticTokens := client.request("textDocument/semanticTokens/full", map[string]any{
		"textDocument": map[string]any{"uri": uri},
	})
	decoded := decodeSemanticTokens(t, semanticTokens.Result)
	expectToken := func(text string, tokenType int, modifiers int) {
		t.Helper()
		if !hasTokenAtOffset(source, decoded, strings.Index(source, text), tokenType, modifiers) {
			t.Fatalf("semantic tokens missing %q type %d modifiers %d: %#v", text, tokenType, modifiers, decoded)
		}
	}
	expectToken("metricMap", semanticTokenParameter, semanticModifierByVal)
	expectToken("Default", semanticTokenKeyword, 0)
	expectToken("output", semanticTokenParameter, semanticModifierByRef)
	expectToken("MaxCount", semanticTokenConstant, semanticModifierReadonly)
	expectToken("Name", semanticTokenProperty, semanticModifierPublic)
	expectToken("DisplayName", semanticTokenProperty, semanticModifierPublic)
	expectToken("Response", semanticTokenConstant, semanticModifierReadonly|semanticModifierLibrary)
	expectToken("Write", semanticTokenMethod, semanticModifierLibrary)
	expectToken("vbCrLf", semanticTokenConstant, semanticModifierReadonly|semanticModifierLibrary)
	expectToken("Nothing", semanticTokenConstant, semanticModifierReadonly|semanticModifierLibrary)
	for _, operator := range []string{"+", "Xor", "Eqv", "(", ")", "<>", "<=", ">="} {
		offset := strings.Index(source, operator)
		if operator == "(" || operator == ")" {
			offset = strings.Index(source[strings.Index(source, "MaxCount <>"):], operator) + strings.Index(source, "MaxCount <>")
		}
		if !hasTokenAtOffset(source, decoded, offset, semanticTokenOperator, 0) {
			t.Fatalf("semantic tokens missing operator %q: %#v", operator, decoded)
		}
	}
}

func TestStdioParityMarksVBScriptControlFlowKeywordsAsSemanticTokens(t *testing.T) {
	client := startStdioTestClient(t)
	defer client.close()

	root := t.TempDir()
	uri := pathToFileURI(filepath.Join(root, "vbscript-control-flow-keywords.asp"))
	source := `<%
Select Case metricKey
Case "active"
  label = "Active"
Case Else
  label = "Other"
End Select
%>`
	client.request("initialize", map[string]any{
		"processId":    nil,
		"rootUri":      pathToFileURI(root),
		"capabilities": map[string]any{},
	})
	notifyOpenClassicASPDocument(t, client, uri, source)
	waitForDiagnosticsContaining(t, client, "")

	semanticTokens := client.request("textDocument/semanticTokens/full", map[string]any{
		"textDocument": map[string]any{"uri": uri},
	})
	decoded := decodeSemanticTokens(t, semanticTokens.Result)
	for _, text := range []string{"Select Case", "Case \"active\"", "Case Else", "Else", "End Select"} {
		position := positionAt(source, strings.Index(source, text))
		if text == "Else" {
			position = positionAt(source, strings.Index(source, "Case Else")+len("Case "))
		}
		if !hasSemanticToken(decoded, position["line"], position["character"], semanticTokenKeyword, 0) {
			t.Fatalf("semantic tokens missing keyword %q at %#v: %#v", text, position, decoded)
		}
	}
	endSelect := strings.LastIndex(source, "Select")
	position := positionAt(source, endSelect)
	if !hasSemanticToken(decoded, position["line"], position["character"], semanticTokenKeyword, 0) {
		t.Fatalf("semantic tokens missing closing Select keyword: %#v", decoded)
	}
}

func TestStdioParityDoesNotMarkHTMLTagNamesAsKeywordSemanticTokens(t *testing.T) {
	client := startStdioTestClient(t)
	defer client.close()

	root := t.TempDir()
	uri := pathToFileURI(filepath.Join(root, "html-tag-semantic.asp"))
	source := `<div class="card"><span><%= title %></span></div>`
	client.request("initialize", map[string]any{
		"processId":    nil,
		"rootUri":      pathToFileURI(root),
		"capabilities": map[string]any{},
	})
	notifyOpenClassicASPDocument(t, client, uri, source)
	waitForDiagnosticsContaining(t, client, "")

	full := client.request("textDocument/semanticTokens/full", map[string]any{
		"textDocument": map[string]any{"uri": uri},
	})
	decodedFull := decodeSemanticTokens(t, full.Result)
	for _, tag := range []string{"div", "span"} {
		if hasTokenMatchingText(source, decodedFull, tag, semanticTokenKeyword) {
			t.Fatalf("full semantic tokens marked HTML tag %s as keyword: %#v", tag, decodedFull)
		}
	}
	equalsPosition := positionAt(source, strings.Index(source, "<%=")+len("<%"))
	if !hasSemanticToken(decodedFull, equalsPosition["line"], equalsPosition["character"], semanticTokenKeyword, 0) {
		t.Fatalf("full semantic tokens missing ASP expression equals keyword: %#v", decodedFull)
	}

	rangeTokens := client.request("textDocument/semanticTokens/range", map[string]any{
		"textDocument": map[string]any{"uri": uri},
		"range": map[string]any{
			"start": map[string]any{"line": 0, "character": 0},
			"end":   map[string]any{"line": 0, "character": len(source)},
		},
	})
	decodedRange := decodeSemanticTokens(t, rangeTokens.Result)
	for _, tag := range []string{"div", "span"} {
		if hasTokenMatchingText(source, decodedRange, tag, semanticTokenKeyword) {
			t.Fatalf("range semantic tokens marked HTML tag %s as keyword: %#v", tag, decodedRange)
		}
	}
}

func TestStdioParityLimitsRangeSemanticTokensToRequestedASPAndEmbeddedSpans(t *testing.T) {
	client := startStdioTestClient(t)
	defer client.close()

	root := t.TempDir()
	uri := pathToFileURI(filepath.Join(root, "range-semantic.asp"))
	source := `<style>
.a { color: red; }
</style>
<%
Dim beforeValue
Dim targetValue
targetValue = beforeValue + 1
Dim afterValue
%>`
	client.request("initialize", map[string]any{
		"processId":    nil,
		"rootUri":      pathToFileURI(root),
		"capabilities": map[string]any{},
	})
	notifyOpenClassicASPDocument(t, client, uri, source)
	waitForDiagnosticsContaining(t, client, "")

	cssTokens := client.request("textDocument/semanticTokens/range", map[string]any{
		"textDocument": map[string]any{"uri": uri},
		"range": map[string]any{
			"start": map[string]any{"line": 1, "character": 0},
			"end":   map[string]any{"line": 1, "character": 20},
		},
	})
	decodedCSS := decodeSemanticTokens(t, cssTokens.Result)
	if len(decodedCSS) == 0 || !allSemanticTokensOnLine(decodedCSS, 1) {
		t.Fatalf("CSS range semantic tokens escaped requested line: %#v", decodedCSS)
	}

	vbTokens := client.request("textDocument/semanticTokens/range", map[string]any{
		"textDocument": map[string]any{"uri": uri},
		"range": map[string]any{
			"start": map[string]any{"line": 6, "character": 0},
			"end":   map[string]any{"line": 6, "character": 32},
		},
	})
	decodedVB := decodeSemanticTokens(t, vbTokens.Result)
	if len(decodedVB) == 0 || !allSemanticTokensOnLine(decodedVB, 6) {
		t.Fatalf("VB range semantic tokens escaped requested line: %#v", decodedVB)
	}
	if !hasSemanticToken(decodedVB, 6, 12, semanticTokenOperator, 0) {
		t.Fatalf("VB range semantic tokens missing assignment operator: %#v", decodedVB)
	}

	boundaryTokens := client.request("textDocument/semanticTokens/range", map[string]any{
		"textDocument": map[string]any{"uri": uri},
		"range": map[string]any{
			"start": map[string]any{"line": 5, "character": 0},
			"end":   map[string]any{"line": 6, "character": 0},
		},
	})
	decodedBoundary := decodeSemanticTokens(t, boundaryTokens.Result)
	if len(decodedBoundary) == 0 || !allSemanticTokensOnLine(decodedBoundary, 5) {
		t.Fatalf("boundary range semantic tokens escaped requested line: %#v", decodedBoundary)
	}
}

func allSemanticTokensOnLine(tokens []decodedSemanticToken, line int) bool {
	for _, token := range tokens {
		if token.Line != line {
			return false
		}
	}
	return true
}
