package lspserver

import (
	"path/filepath"
	"strings"
	"testing"
)

func TestStdioParitySupportsVBScriptXMLDocumentationCommentsOverJSONRPC(t *testing.T) {
	client := startStdioTestClient(t)
	defer client.close()

	markedSource := `<%
''' <summary>Builds a display name.</summary>
''' <param name="first">First name.</param>
''' <param name="<<<param>>>"></param>
''' <returns>Display name.</returns>
Function BuildName(first)
  BuildName = first
End Function
Response.Write BuildName("Ada")
''' <<<<tag>>>
''' <see <<<attr>>>/>
''' <summary>Text</<<<closing>>>
''' <see cref="<<<cref>>>" />
%>`
	document := markedDocumentPositions(markedSource, []string{"param", "tag", "attr", "closing", "cref"})
	callPosition := positionAt(document.Text, strings.Index(document.Text, `BuildName("Ada")`)+2)
	signaturePosition := positionAt(document.Text, strings.Index(document.Text, `"Ada"`)+2)
	root := t.TempDir()
	uri := pathToFileURI(filepath.Join(root, "go-vbscript-doc-comments.asp"))
	client.request("initialize", map[string]any{
		"processId":    nil,
		"rootUri":      pathToFileURI(root),
		"capabilities": map[string]any{},
	})
	openClassicASPDocument(t, client, uri, document.Text)

	hover := client.request("textDocument/hover", map[string]any{
		"textDocument": map[string]any{"uri": uri},
		"position":     callPosition,
	})
	hoverText := hoverMarkdownValue(t, hover.Result)
	expectedHover := "```vbscript\nFunction BuildName(ByRef first)\n```\n\nBuilds a display name.\n\n**Parameters**\n- `first`: First name.\n\n**Returns**\n\nDisplay name."
	if hoverText != expectedHover {
		t.Fatalf("XML documentation hover mismatch:\nwant: %s\n got: %s", expectedHover, hoverText)
	}

	completions := completionItems(client.request("textDocument/completion", map[string]any{
		"textDocument": map[string]any{"uri": uri},
		"position":     callPosition,
	}).Result)
	buildCompletion, ok := completions.find("BuildName")
	if !ok {
		t.Fatalf("BuildName completion missing from XML documentation case: %#v", completionItemLabels(completions))
	}
	resolved := client.request("completionItem/resolve", buildCompletion)
	if resolvedText := mustJSONText(t, resolved.Result); !strings.Contains(resolvedText, "Builds a display name.") {
		t.Fatalf("resolved BuildName completion missing XML documentation: %s", resolvedText)
	}

	signature := client.request("textDocument/signatureHelp", map[string]any{
		"textDocument": map[string]any{"uri": uri},
		"position":     signaturePosition,
	})
	if signatureText := mustJSONText(t, signature.Result); !strings.Contains(signatureText, "First name.") {
		t.Fatalf("XML documentation signature help missing parameter docs: %s", signatureText)
	}

	for _, testCase := range []struct {
		name     string
		marker   string
		expected string
	}{
		{name: "tag", marker: "tag", expected: "summary"},
		{name: "param", marker: "param", expected: "first"},
		{name: "attr", marker: "attr", expected: "cref"},
		{name: "closing", marker: "closing", expected: "summary"},
		{name: "cref", marker: "cref", expected: "BuildName"},
	} {
		response := client.request("textDocument/completion", map[string]any{
			"textDocument": map[string]any{"uri": uri},
			"position":     document.Positions[testCase.marker],
		})
		if serialized := mustJSONText(t, response.Result); !strings.Contains(serialized, testCase.expected) {
			t.Fatalf("%s XML documentation completions missing %q: %s", testCase.name, testCase.expected, serialized)
		}
	}
}

func TestStdioParityUsesXMLParameterDocumentationForDeclarationHoverAndNoParenSignatureHelp(t *testing.T) {
	client := startStdioTestClient(t)
	defer client.close()

	source := `<%
''' <summary>Renders the metric cards for the dashboard.</summary>
''' <param name="metricMap">A dictionary of metric keys and values to render.</param>
Sub RenderMetricCards(ByVal metricMap)
End Sub
RenderMetricCards metrics
%>`
	root := t.TempDir()
	uri := pathToFileURI(filepath.Join(root, "go-vbscript-xml-param-docs.asp"))
	client.request("initialize", map[string]any{
		"processId":    nil,
		"rootUri":      pathToFileURI(root),
		"capabilities": map[string]any{},
	})
	openClassicASPDocument(t, client, uri, source)

	parameterHover := client.request("textDocument/hover", map[string]any{
		"textDocument": map[string]any{"uri": uri},
		"position":     positionAt(source, strings.Index(source, "metricMap)")),
	})
	hoverText := hoverMarkdownValue(t, parameterHover.Result)
	for _, expected := range []string{
		"```vbscript\nByVal metricMap As Variant\n```",
		"A dictionary of metric keys and values to render.",
	} {
		if !strings.Contains(hoverText, expected) {
			t.Fatalf("XML parameter declaration hover missing %q: %s", expected, hoverText)
		}
	}
	if strings.Contains(hoverText, "Parameter ByVal") {
		t.Fatalf("XML parameter hover included non-VBScript Parameter prefix in highlighted code: %s", hoverText)
	}
	if strings.Contains(hoverText, "XML documentation is descriptive only.") {
		t.Fatalf("XML parameter hover included descriptive-only note: %s", hoverText)
	}

	signature := client.request("textDocument/signatureHelp", map[string]any{
		"textDocument": map[string]any{"uri": uri},
		"position":     positionAt(source, strings.Index(source, "metrics")+2),
	})
	signatureText := mustJSONText(t, signature.Result)
	for _, expected := range []string{
		"Sub RenderMetricCards(ByVal metricMap)",
		"ByVal metricMap",
		"A dictionary of metric keys and values to render.",
	} {
		if !strings.Contains(signatureText, expected) {
			t.Fatalf("no-paren XML signature help missing %q: %s", expected, signatureText)
		}
	}
	if strings.Contains(signatureText, "XML documentation is descriptive only.") {
		t.Fatalf("no-paren XML signature help included descriptive-only note: %s", signatureText)
	}
}

func TestStdioParityShowsXMLDocumentationTypeNoteOnlyOnDeclarationsMissingMetadata(t *testing.T) {
	client := startStdioTestClient(t)
	defer client.close()

	source := `<%
''' <summary>Customer id.</summary>
Dim customerId
Response.Write customerId

' @type typedValue As String
''' <summary>Typed value.</summary>
Dim typedValue

''' <summary>Renders metrics.</summary>
''' <param name="metricMap">Metric values.</param>
Sub RenderMetricCards(ByVal metricMap)
End Sub
RenderMetricCards metrics

' @param first As String
' @returns BuildName String
''' <summary>Builds a display name.</summary>
''' <param name="first">First name.</param>
''' <returns>Display name.</returns>
Function BuildName(first)
End Function
Response.Write BuildName("Ada")
%>`
	root := t.TempDir()
	uri := pathToFileURI(filepath.Join(root, "go-vbscript-xml-type-note.asp"))
	client.request("initialize", map[string]any{
		"processId":    nil,
		"rootUri":      pathToFileURI(root),
		"capabilities": map[string]any{},
	})
	openClassicASPDocument(t, client, uri, source)

	missingVariableHover := hoverMarkdownValue(t, client.request("textDocument/hover", map[string]any{
		"textDocument": map[string]any{"uri": uri},
		"position":     positionAt(source, strings.Index(source, "customerId")),
	}).Result)
	if !strings.Contains(missingVariableHover, "```vbscript\n(global) Dim customerId As Variant\n```") ||
		!strings.Contains(missingVariableHover, "Customer id.") {
		t.Fatalf("missing variable hover did not include highlighted declaration and XML summary: %s", missingVariableHover)
	}
	if !strings.Contains(missingVariableHover, "XML documentation is descriptive only.") {
		t.Fatalf("missing variable hover did not include XML type note: %s", missingVariableHover)
	}
	missingVariableReferenceHover := hoverMarkdownValue(t, client.request("textDocument/hover", map[string]any{
		"textDocument": map[string]any{"uri": uri},
		"position":     positionAt(source, strings.LastIndex(source, "customerId")),
	}).Result)
	if strings.Contains(missingVariableReferenceHover, "XML documentation is descriptive only.") {
		t.Fatalf("variable reference hover included XML type note: %s", missingVariableReferenceHover)
	}

	typedVariableHover := hoverMarkdownValue(t, client.request("textDocument/hover", map[string]any{
		"textDocument": map[string]any{"uri": uri},
		"position":     positionAt(source, strings.Index(source, "Dim typedValue")+len("Dim ")),
	}).Result)
	if !strings.Contains(typedVariableHover, "```vbscript\n(global) Dim typedValue As String\n```") ||
		!strings.Contains(typedVariableHover, "Typed value.") {
		t.Fatalf("typed variable hover did not include highlighted declaration and XML summary: %s", typedVariableHover)
	}
	if strings.Contains(typedVariableHover, "XML documentation is descriptive only.") {
		t.Fatalf("typed variable hover included XML type note: %s", typedVariableHover)
	}

	missingParamHover := hoverMarkdownValue(t, client.request("textDocument/hover", map[string]any{
		"textDocument": map[string]any{"uri": uri},
		"position":     positionAt(source, strings.Index(source, "RenderMetricCards(ByVal")),
	}).Result)
	if !strings.Contains(missingParamHover, "```vbscript\nSub RenderMetricCards(ByVal metricMap)\n```") ||
		!strings.Contains(missingParamHover, "**Parameters**\n- `metricMap`: Metric values.") {
		t.Fatalf("declaration hover missing highlighted signature docs: %s", missingParamHover)
	}
	if !strings.Contains(missingParamHover, "XML documentation is descriptive only.") {
		t.Fatalf("declaration hover missing XML type note for untyped param: %s", missingParamHover)
	}

	callHover := hoverMarkdownValue(t, client.request("textDocument/hover", map[string]any{
		"textDocument": map[string]any{"uri": uri},
		"position":     positionAt(source, strings.LastIndex(source, "RenderMetricCards")),
	}).Result)
	if strings.Contains(callHover, "XML documentation is descriptive only.") {
		t.Fatalf("call hover included XML type note: %s", callHover)
	}

	typedFunctionHover := hoverMarkdownValue(t, client.request("textDocument/hover", map[string]any{
		"textDocument": map[string]any{"uri": uri},
		"position":     positionAt(source, strings.Index(source, "BuildName(first)")),
	}).Result)
	if strings.Contains(typedFunctionHover, "XML documentation is descriptive only.") {
		t.Fatalf("typed function hover included XML type note: %s", typedFunctionHover)
	}
}

func TestStdioParityParsesXMLDocumentationWithTokenizerAndKeepsVariableDocsUnambiguous(t *testing.T) {
	client := startStdioTestClient(t)
	defer client.close()

	source := `<%
''' <summary>Outer <summary>inner</summary> tail</summary>
Function BuildName()
End Function

''' <summary>One value.</summary>
Dim oneValue

''' <summary>Ambiguous values.</summary>
Dim firstValue, secondValue
%>`
	root := t.TempDir()
	uri := pathToFileURI(filepath.Join(root, "go-vbscript-xml-tokenizer.asp"))
	client.request("initialize", map[string]any{
		"processId":    nil,
		"rootUri":      pathToFileURI(root),
		"capabilities": map[string]any{},
	})
	openClassicASPDocument(t, client, uri, source)

	functionHover := hoverMarkdownValue(t, client.request("textDocument/hover", map[string]any{
		"textDocument": map[string]any{"uri": uri},
		"position":     positionAt(source, strings.Index(source, "BuildName")),
	}).Result)
	if !strings.Contains(functionHover, "Outer inner tail") {
		t.Fatalf("nested XML summary hover mismatch: %s", functionHover)
	}

	singleVariableHover := hoverMarkdownValue(t, client.request("textDocument/hover", map[string]any{
		"textDocument": map[string]any{"uri": uri},
		"position":     positionAt(source, strings.Index(source, "oneValue")),
	}).Result)
	if !strings.Contains(singleVariableHover, "```vbscript\n(global) Dim oneValue As Variant\n```") ||
		!strings.Contains(singleVariableHover, "One value.") {
		t.Fatalf("single variable hover missing XML docs: %s", singleVariableHover)
	}

	ambiguousVariableHover := hoverMarkdownValue(t, client.request("textDocument/hover", map[string]any{
		"textDocument": map[string]any{"uri": uri},
		"position":     positionAt(source, strings.Index(source, "firstValue")),
	}).Result)
	if strings.Contains(ambiguousVariableHover, "Ambiguous values.") {
		t.Fatalf("multi-name variable hover included ambiguous XML docs: %s", ambiguousVariableHover)
	}
}

func TestStdioParityTreatsSingleQuoteXMLLookingCommentsAsPlainTextAndToleratesBrokenXMLDocumentation(t *testing.T) {
	client := startStdioTestClient(t)
	defer client.close()

	root := t.TempDir()
	client.request("initialize", map[string]any{
		"processId":    nil,
		"rootUri":      pathToFileURI(root),
		"capabilities": map[string]any{},
	})

	singleSource := `<%
' <summary>Not documentation.</summary>
Function BuildName()
End Function
%>`
	singleURI := pathToFileURI(filepath.Join(root, "go-vbscript-plain-xml-looking-doc.asp"))
	openClassicASPDocument(t, client, singleURI, singleSource)
	singleHover := hoverMarkdownValue(t, client.request("textDocument/hover", map[string]any{
		"textDocument": map[string]any{"uri": singleURI},
		"position":     positionAt(singleSource, strings.Index(singleSource, "BuildName")),
	}).Result)
	if !strings.Contains(singleHover, `&lt;summary&gt;Not documentation\.&lt;/summary&gt;`) {
		t.Fatalf("single-quote XML-looking plain doc was not escaped: %s", singleHover)
	}
	if strings.Contains(singleHover, "<summary>Not documentation.</summary>") {
		t.Fatalf("single-quote XML-looking plain doc was rendered as XML: %s", singleHover)
	}

	brokenSource := `<%
''' <summary>Broken but useful
Function BuildName()
End Function
%>`
	brokenURI := pathToFileURI(filepath.Join(root, "go-vbscript-broken-xml-doc.asp"))
	openClassicASPDocument(t, client, brokenURI, brokenSource)
	brokenHover := hoverMarkdownValue(t, client.request("textDocument/hover", map[string]any{
		"textDocument": map[string]any{"uri": brokenURI},
		"position":     positionAt(brokenSource, strings.Index(brokenSource, "BuildName")),
	}).Result)
	if !strings.Contains(brokenHover, "Broken but useful") {
		t.Fatalf("broken XML documentation hover missing useful text: %s", brokenHover)
	}
}

func TestStdioParityResolvesVBScriptXMLDocumentationSymbolReferencesOverJSONRPC(t *testing.T) {
	client := startStdioTestClient(t)
	defer client.close()

	source := `<%
Function BuildName(first)
End Function
''' <see cref="BuildName" />
Sub Save()
End Sub
%>`
	root := t.TempDir()
	uri := pathToFileURI(filepath.Join(root, "go-vbscript-doc-cref-references.asp"))
	client.request("initialize", map[string]any{
		"processId":    nil,
		"rootUri":      pathToFileURI(root),
		"capabilities": map[string]any{},
	})
	openClassicASPDocument(t, client, uri, source)

	crefPosition := positionAt(source, strings.Index(source, `BuildName"`)+len("Build"))
	definition := client.request("textDocument/definition", map[string]any{
		"textDocument": map[string]any{"uri": uri},
		"position":     crefPosition,
	})
	if definitionText := mustJSONText(t, definition.Result); !strings.Contains(definitionText, `"line":1`) {
		t.Fatalf("XML documentation cref definition mismatch: %s", definitionText)
	}

	hover := client.request("textDocument/hover", map[string]any{
		"textDocument": map[string]any{"uri": uri},
		"position":     crefPosition,
	})
	if hoverText := mustJSONText(t, hover.Result); !strings.Contains(hoverText, "Function BuildName") {
		t.Fatalf("XML documentation cref hover mismatch: %s", hoverText)
	}

	references := client.request("textDocument/references", map[string]any{
		"textDocument": map[string]any{"uri": uri},
		"position":     crefPosition,
		"context":      map[string]any{"includeDeclaration": true},
	})
	referencesText := mustJSONText(t, references.Result)
	if countOccurrences(referencesText, `"line":1`) < 1 || countOccurrences(referencesText, `"line":3`) < 1 {
		t.Fatalf("XML documentation cref references should include declaration and cref: %s", referencesText)
	}
}

func TestStdioParityUsesPlainCommentDocsAndAnnotationCompletionsOverJSONRPC(t *testing.T) {
	client := startStdioTestClient(t)
	defer client.close()

	text := `<%
' **Plain** docs with <summary>tag</summary>.
' Second plain line.
Function PlainDocumented()
End Function
Response.Write PlainDocumented()
' Plain variable docs.
Dim plainValue
Response.Write plainValue
Dim inlineCommentValue ' aa
Response.Write inlineCommentValue
Dim inlineAnnotationValue ' @type inlineAnnotationValue As String
Response.Write inlineAnnotationValue
' Response.
' @
' @type customerId As String
%>`
	ordinaryCommentPosition := positionAt(text, strings.LastIndex(text, "Response.")+len("Response."))
	annotationPosition := positionAt(text, strings.Index(text, "' @")+len("' @"))
	annotationHoverPosition := positionAt(text, strings.Index(text, "@type")+1)
	callPosition := positionAt(text, strings.Index(text, "PlainDocumented()")+2)
	root := t.TempDir()
	uri := pathToFileURI(filepath.Join(root, "go-vbscript-plain-comments.asp"))
	client.request("initialize", map[string]any{
		"processId":    nil,
		"rootUri":      pathToFileURI(root),
		"capabilities": map[string]any{},
	})
	openClassicASPDocument(t, client, uri, text)

	hover := client.request("textDocument/hover", map[string]any{
		"textDocument": map[string]any{"uri": uri},
		"position":     callPosition,
	})
	serializedHover := hoverMarkdownValue(t, hover.Result)
	for _, expected := range []string{
		`\*\*Plain\*\* docs`,
		"&lt;summary&gt;tag&lt;/summary&gt;",
		`&lt;/summary&gt;\.  `,
		`Second plain line\.`,
	} {
		if !strings.Contains(serializedHover, expected) {
			t.Fatalf("plain comment hover missing %q: %s", expected, serializedHover)
		}
	}
	for _, unexpected := range []string{"**Plain** docs", "<summary>tag</summary>"} {
		if strings.Contains(serializedHover, unexpected) {
			t.Fatalf("plain comment hover should escape %q: %s", unexpected, serializedHover)
		}
	}

	variableHover := hoverMarkdownValue(t, client.request("textDocument/hover", map[string]any{
		"textDocument": map[string]any{"uri": uri},
		"position":     positionAt(text, strings.Index(text, "plainValue")),
	}).Result)
	for _, expected := range []string{
		"```vbscript\n(global) Dim plainValue As Variant\n```",
		`Plain variable docs\.`,
		"XML documentation is descriptive only.",
	} {
		if !strings.Contains(variableHover, expected) {
			t.Fatalf("plain variable comment hover missing %q: %s", expected, variableHover)
		}
	}

	inlineCommentHover := hoverMarkdownValue(t, client.request("textDocument/hover", map[string]any{
		"textDocument": map[string]any{"uri": uri},
		"position":     positionAt(text, strings.Index(text, "inlineCommentValue")),
	}).Result)
	for _, expected := range []string{
		"```vbscript\n(global) Dim inlineCommentValue As Variant\n```",
		`aa`,
		"XML documentation is descriptive only.",
	} {
		if !strings.Contains(inlineCommentHover, expected) {
			t.Fatalf("inline variable comment hover missing %q: %s", expected, inlineCommentHover)
		}
	}
	inlineCommentReferenceHover := hoverMarkdownValue(t, client.request("textDocument/hover", map[string]any{
		"textDocument": map[string]any{"uri": uri},
		"position":     positionAt(text, strings.LastIndex(text, "inlineCommentValue")),
	}).Result)
	if strings.Contains(inlineCommentReferenceHover, "XML documentation is descriptive only.") {
		t.Fatalf("inline variable reference hover included XML type note: %s", inlineCommentReferenceHover)
	}
	inlineAnnotationHover := hoverMarkdownValue(t, client.request("textDocument/hover", map[string]any{
		"textDocument": map[string]any{"uri": uri},
		"position":     positionAt(text, strings.Index(text, "inlineAnnotationValue")),
	}).Result)
	if strings.Contains(inlineAnnotationHover, "@type inlineAnnotationValue") {
		t.Fatalf("inline annotation should not become descriptive hover text: %s", inlineAnnotationHover)
	}

	ordinaryCompletions := completionItems(client.request("textDocument/completion", map[string]any{
		"textDocument": map[string]any{"uri": uri},
		"position":     ordinaryCommentPosition,
	}).Result)
	if completionItemLabels(ordinaryCompletions).contains("Write") {
		t.Fatalf("ordinary comment completions should not include Response.Write: %#v", completionItemLabels(ordinaryCompletions))
	}

	annotationCompletions := completionItems(client.request("textDocument/completion", map[string]any{
		"textDocument": map[string]any{"uri": uri},
		"position":     annotationPosition,
	}).Result)
	annotationLabels := completionItemLabels(annotationCompletions)
	for _, expected := range []string{"@type", "@param", "@returns"} {
		if !annotationLabels.contains(expected) {
			t.Fatalf("annotation completions missing %q: %#v", expected, annotationLabels)
		}
	}
	annotationText := mustJSONText(t, annotationCompletions)
	for _, expected := range []string{"VBScript type annotation", "' @type name As Type"} {
		if !strings.Contains(annotationText, expected) {
			t.Fatalf("annotation completions missing %q: %s", expected, annotationText)
		}
	}

	annotationHover := client.request("textDocument/hover", map[string]any{
		"textDocument": map[string]any{"uri": uri},
		"position":     annotationHoverPosition,
	})
	if hoverText := mustJSONText(t, annotationHover.Result); !strings.Contains(hoverText, "' @type name As Type") {
		t.Fatalf("annotation hover missing snippet: %s", hoverText)
	}
}

type markedPositionsDocument struct {
	Text      string
	Positions map[string]map[string]int
}

func markedDocumentPositions(source string, markers []string) markedPositionsDocument {
	text := source
	positions := make(map[string]map[string]int, len(markers))
	for _, marker := range markers {
		token := "<<<" + marker + ">>>"
		offset := strings.Index(text, token)
		text = text[:offset] + text[offset+len(token):]
		positions[marker] = positionAt(text, offset)
	}
	return markedPositionsDocument{Text: text, Positions: positions}
}

func hoverMarkdownValue(t *testing.T, value any) string {
	t.Helper()
	var result struct {
		Contents struct {
			Value string `json:"value"`
		} `json:"contents"`
	}
	mustDecodeResult(t, value, &result)
	if result.Contents.Value == "" {
		t.Fatalf("hover has no markdown value: %s", mustJSONText(t, value))
	}
	return result.Contents.Value
}
