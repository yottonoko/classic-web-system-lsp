package lspserver

import (
	"os"
	"path/filepath"
	"strings"
	"testing"

	"github.com/yottonoko/classic-web-system-lsp/internal/lsp"
)

func TestStdioParityReturnsCSSCompletionsAndColorsInsideHTMLStyleAttributes(t *testing.T) {
	client := startStdioTestClient(t)
	defer client.close()

	root := t.TempDir()
	uri := pathToFileURI(filepath.Join(root, "go-style-attribute.asp"))
	source := `<div style="colo; background: #ff0000"></div>`
	client.request("initialize", map[string]any{
		"processId":    nil,
		"rootUri":      pathToFileURI(root),
		"capabilities": map[string]any{},
	})
	openClassicASPDocument(t, client, uri, source)
	completions := client.request("textDocument/completion", map[string]any{
		"textDocument": map[string]any{"uri": uri},
		"position":     map[string]any{"line": 0, "character": len(`<div style="colo`)},
	})
	if !strings.Contains(mustJSONText(t, completions.Result), "color") {
		t.Fatalf("style attribute completions missing color: %s", mustJSONText(t, completions.Result))
	}
	colors := client.request("textDocument/documentColor", map[string]any{
		"textDocument": map[string]any{"uri": uri},
	})
	parsedColors := documentColors(t, colors.Result)
	if len(parsedColors) != 1 {
		t.Fatalf("document colors = %d, want 1: %s", len(parsedColors), mustJSONText(t, colors.Result))
	}
	if parsedColors[0].Range.Start.Line != 0 || parsedColors[0].Range.Start.Character != strings.Index(source, "#ff0000") {
		t.Fatalf("document color range start = %#v, want #ff0000 offset", parsedColors[0].Range.Start)
	}
	presentations := client.request("textDocument/colorPresentation", map[string]any{
		"textDocument": map[string]any{"uri": uri},
		"color":        map[string]any{"red": 1, "green": 0, "blue": 0, "alpha": 1},
		"range":        parsedColors[0].Range,
	})
	if !strings.Contains(mustJSONText(t, presentations.Result), "#ff0000") {
		t.Fatalf("color presentations missing #ff0000: %s", mustJSONText(t, presentations.Result))
	}
}

func TestStdioParityReturnsCSSCompletionsAfterTrailingStyleAttributeSemicolon(t *testing.T) {
	client := startStdioTestClient(t)
	defer client.close()

	root := t.TempDir()
	uri := pathToFileURI(filepath.Join(root, "go-css-style-attribute-semicolon.asp"))
	marked := markedDocument(`<div style="display: block; display: block;<<<caret>>>"></div>`)
	client.request("initialize", map[string]any{
		"processId":    nil,
		"rootUri":      pathToFileURI(root),
		"capabilities": map[string]any{},
	})
	openClassicASPDocument(t, client, uri, marked.Text)
	completions := completionItems(client.request("textDocument/completion", map[string]any{
		"textDocument": map[string]any{"uri": uri},
		"position":     marked.Position,
		"context":      map[string]any{"triggerKind": 2, "triggerCharacter": ";"},
	}).Result)
	labels := completionItemLabels(completions)
	if !labels.contains("display") || labels.contains("/div") {
		t.Fatalf("semicolon style attribute completions mismatch: %#v", labels)
	}
	displayItem, ok := completions.find("display")
	if !ok {
		t.Fatalf("missing display completion")
	}
	if displayItem.TextEdit == nil || displayItem.TextEdit.Range != mapPositionRange(marked.Position, marked.Position) {
		t.Fatalf("display completion range = %#v, want caret range %#v", displayItem.TextEdit, marked.Position)
	}
	if !strings.HasPrefix(completionEditNewText(displayItem), " display") {
		t.Fatalf("display completion newText mismatch: %#v", displayItem)
	}
}

func TestStdioParityReturnsCSSCompletionsInsideEmptyHTMLStyleAttributes(t *testing.T) {
	client := startStdioTestClient(t)
	defer client.close()

	root := t.TempDir()
	uri := pathToFileURI(filepath.Join(root, "go-empty-css-style-attribute.asp"))
	marked := markedDocument(`<div style="<<<caret>>>"></div>`)
	client.request("initialize", map[string]any{
		"processId":    nil,
		"rootUri":      pathToFileURI(root),
		"capabilities": map[string]any{},
	})
	openClassicASPDocument(t, client, uri, marked.Text)
	completions := completionItems(client.request("textDocument/completion", map[string]any{
		"textDocument": map[string]any{"uri": uri},
		"position":     marked.Position,
		"context":      map[string]any{"triggerKind": 2, "triggerCharacter": `"`},
	}).Result)
	displayItem, ok := completions.find("display")
	if !ok {
		t.Fatalf("missing display completion")
	}
	if got := completionEditNewText(displayItem); got != "display: $0;" {
		t.Fatalf("display completion newText = %q", got)
	}
	if displayItem.TextEdit == nil || displayItem.TextEdit.Range != mapPositionRange(marked.Position, marked.Position) {
		t.Fatalf("display completion range = %#v, want caret range %#v", displayItem.TextEdit, marked.Position)
	}
}

func TestStdioParityReturnsCSSValueCompletionsInsideIncompleteHTMLStyleAttributes(t *testing.T) {
	client := startStdioTestClient(t)
	defer client.close()

	root := t.TempDir()
	uri := pathToFileURI(filepath.Join(root, "go-incomplete-css-style-attribute.asp"))
	marked := markedDocument(`<div class="card" style="color: <<<caret>>>`)
	client.request("initialize", map[string]any{
		"processId":    nil,
		"rootUri":      pathToFileURI(root),
		"capabilities": map[string]any{},
	})
	openClassicASPDocument(t, client, uri, marked.Text)
	completions := completionItems(client.request("textDocument/completion", map[string]any{
		"textDocument": map[string]any{"uri": uri},
		"position":     marked.Position,
		"context":      map[string]any{"triggerKind": 2, "triggerCharacter": " "},
	}).Result)
	redItem, ok := completions.find("red")
	if !ok {
		t.Fatalf("missing red value completion")
	}
	if redItem.TextEdit == nil || redItem.TextEdit.Range != mapPositionRange(marked.Position, marked.Position) {
		t.Fatalf("red completion range = %#v, want caret range %#v", redItem.TextEdit, marked.Position)
	}
}

func TestStdioParityReturnsCSSCompletionsAfterUnsavedTypedStyleAttributeInsertion(t *testing.T) {
	client := startStdioTestClient(t)
	defer client.close()

	root := t.TempDir()
	uri := pathToFileURI(filepath.Join(root, "go-typed-css-style-attribute-range.asp"))
	initial := "<div ></div>"
	client.request("initialize", map[string]any{
		"processId":    nil,
		"rootUri":      pathToFileURI(root),
		"capabilities": map[string]any{},
	})
	openClassicASPDocument(t, client, uri, initial)
	typed := notifyTypedInsertion(t, client, uri, initial, 1, strings.Index(initial, "<div ")+len("<div "), `style="di"`)
	styleValueOffset := strings.Index(typed.Text, `style="`) + len(`style="`)
	completions := completionLabels(client.request("textDocument/completion", map[string]any{
		"textDocument": map[string]any{"uri": uri},
		"position":     positionAt(typed.Text, styleValueOffset+len("di")),
		"context":      map[string]any{"triggerKind": 1},
	}).Result)
	if !completions.contains("display") {
		t.Fatalf("typed style attribute completions missing display: %#v", completions)
	}
}

func TestStdioParityKeepsCSSCompletionsAndColorsAfterStyleAttributeCompletionEdits(t *testing.T) {
	client := startStdioTestClient(t)
	defer client.close()

	root := t.TempDir()
	uri := pathToFileURI(filepath.Join(root, "go-css-style-attribute-completion-chain.asp"))
	marked := markedDocument(`<div style="color: #ff0000; <<<caret>>>"></div>`)
	source := marked.Text
	client.request("initialize", map[string]any{
		"processId":    nil,
		"rootUri":      pathToFileURI(root),
		"capabilities": map[string]any{},
	})
	openClassicASPDocument(t, client, uri, source)
	propertyCompletions := completionItems(client.request("textDocument/completion", map[string]any{
		"textDocument": map[string]any{"uri": uri},
		"position":     marked.Position,
		"context":      map[string]any{"triggerKind": 1},
	}).Result)
	displayItem, ok := propertyCompletions.find("display")
	if !ok || displayItem.TextEdit == nil {
		t.Fatalf("missing display completion edit: %#v", displayItem)
	}
	displaySnippet := completionEditNewText(displayItem)
	displayTabstop := strings.Index(displaySnippet, "$0")
	if displayTabstop < 0 {
		t.Fatalf("display completion missing tabstop: %q", displaySnippet)
	}
	displayText := strings.Replace(displaySnippet, "$0", "", 1)
	displayStart := offsetAt(source, displayItem.TextEdit.Range.Start)
	source = applyTextEdit(source, displayItem.TextEdit.Range, displayText)
	if err := client.notify("textDocument/didChange", map[string]any{
		"textDocument":   map[string]any{"uri": uri, "version": 2},
		"contentChanges": []map[string]any{{"range": displayItem.TextEdit.Range, "text": displayText}},
	}); err != nil {
		t.Fatal(err)
	}

	valuePosition := positionAt(source, displayStart+displayTabstop)
	valueCompletions := completionItems(client.request("textDocument/completion", map[string]any{
		"textDocument": map[string]any{"uri": uri},
		"position":     valuePosition,
		"context":      map[string]any{"triggerKind": 2, "triggerCharacter": " "},
	}).Result)
	blockItem, ok := valueCompletions.find("block")
	if !ok || blockItem.TextEdit == nil {
		t.Fatalf("missing block completion edit: %#v", blockItem)
	}
	blockText := completionEditNewText(blockItem)
	source = applyTextEdit(source, blockItem.TextEdit.Range, blockText)
	if err := client.notify("textDocument/didChange", map[string]any{
		"textDocument":   map[string]any{"uri": uri, "version": 3},
		"contentChanges": []map[string]any{{"range": blockItem.TextEdit.Range, "text": blockText}},
	}); err != nil {
		t.Fatal(err)
	}

	nextCompletions := completionLabels(client.request("textDocument/completion", map[string]any{
		"textDocument": map[string]any{"uri": uri},
		"position":     positionAt(source, strings.Index(source[strings.Index(source, "block"):], ";")+strings.Index(source, "block")+1),
		"context":      map[string]any{"triggerKind": 2, "triggerCharacter": ";"},
	}).Result)
	if !nextCompletions.contains("display") {
		t.Fatalf("next property completions missing display: %#v", nextCompletions)
	}
	colors := documentColors(t, client.request("textDocument/documentColor", map[string]any{
		"textDocument": map[string]any{"uri": uri},
	}).Result)
	if len(colors) != 1 || colors[0].Range.Start != mapPosition(positionAt(source, strings.Index(source, "#ff0000"))) {
		t.Fatalf("style attribute colors mismatch: %#v", colors)
	}
}

func TestStdioParityRefreshesCompletionEditRangesAfterTypedPrefixChanges(t *testing.T) {
	client := startStdioTestClient(t)
	defer client.close()

	root := t.TempDir()
	uri := pathToFileURI(filepath.Join(root, "go-html-class-completion-cache-range.asp"))
	marked := markedDocument(`<style>.lead { color: red; }</style>
<div class="le<<<caret>>>"></div>`)
	client.request("initialize", map[string]any{
		"processId":    nil,
		"rootUri":      pathToFileURI(root),
		"capabilities": map[string]any{},
	})
	openClassicASPDocument(t, client, uri, marked.Text)
	firstCompletions := completionLabels(client.request("textDocument/completion", map[string]any{
		"textDocument": map[string]any{"uri": uri},
		"position":     marked.Position,
		"context":      map[string]any{"triggerKind": 1},
	}).Result)
	if !firstCompletions.contains("lead") {
		t.Fatalf("initial class completions missing lead: %#v", firstCompletions)
	}
	caretOffset := offsetAt(marked.Text, mapPosition(marked.Position))
	typedText := marked.Text[:caretOffset] + "a" + marked.Text[caretOffset:]
	typedPosition := positionAt(typedText, caretOffset+1)
	if err := client.notify("textDocument/didChange", map[string]any{
		"textDocument": map[string]any{"uri": uri, "version": 2},
		"contentChanges": []map[string]any{{
			"range": map[string]any{"start": marked.Position, "end": marked.Position},
			"text":  "a",
		}},
	}); err != nil {
		t.Fatal(err)
	}
	client.waitForNotification("textDocument/publishDiagnostics", "")
	secondCompletions := completionItems(client.request("textDocument/completion", map[string]any{
		"textDocument": map[string]any{"uri": uri},
		"position":     typedPosition,
		"context":      map[string]any{"triggerKind": 1},
	}).Result)
	leadItem, ok := secondCompletions.find("lead")
	if !ok || leadItem.TextEdit == nil {
		t.Fatalf("missing lead completion edit after typed prefix: %#v", leadItem)
	}
	prefixStart := positionAt(typedText, strings.LastIndex(typedText, "lea"))
	if leadItem.TextEdit.Range != mapPositionRange(prefixStart, typedPosition) {
		t.Fatalf("lead completion range = %#v, want %#v -> %#v", leadItem.TextEdit.Range, prefixStart, typedPosition)
	}
}

func TestStdioParityAddsSameFileHTMLAndCSSClassIDNamesToEmbeddedCompletions(t *testing.T) {
	client := startStdioTestClient(t)
	defer client.close()

	root := t.TempDir()
	client.request("initialize", map[string]any{
		"processId":    nil,
		"rootUri":      pathToFileURI(root),
		"capabilities": map[string]any{},
	})
	cases := []struct {
		name         string
		markedSource string
		includes     []string
		excludes     []string
	}{
		{
			name:         "css-class-from-html",
			markedSource: `<div class="card lead" id="hero"></div>` + "\n" + `<style>.<<<caret>>></style>`,
			includes:     []string{"card", "lead"},
			excludes:     []string{"hero"},
		},
		{
			name:         "css-id-from-html",
			markedSource: `<div class="card" id="hero"></div>` + "\n" + `<style>#<<<caret>>></style>`,
			includes:     []string{"hero"},
			excludes:     []string{"card"},
		},
		{
			name:         "css-property-kept",
			markedSource: `<style>.card { colo<<<caret>>> }</style>`,
			includes:     []string{"color"},
		},
		{
			name:         "html-class-from-css",
			markedSource: `<style>.card { color: red; }</style>` + "\n" + `<div class="<<<caret>>>"></div>`,
			includes:     []string{"card"},
		},
		{
			name:         "html-class-from-html",
			markedSource: `<div class="used elsewhere"></div>` + "\n" + `<section class="<<<caret>>>"></section>`,
			includes:     []string{"used", "elsewhere"},
		},
		{
			name:         "html-id-from-css",
			markedSource: `<style>#hero { color: red; }</style>` + "\n" + `<div id="<<<caret>>>"></div>`,
			includes:     []string{"hero"},
		},
		{
			name:         "html-tag-selector-excluded",
			markedSource: `<style>section { color: red; }</style>` + "\n" + `<div class="<<<caret>>>" id=""></div>`,
			excludes:     []string{"section"},
		},
	}
	for _, testCase := range cases {
		document := markedDocument(testCase.markedSource)
		uri := pathToFileURI(filepath.Join(root, "go-"+testCase.name+".asp"))
		openClassicASPDocument(t, client, uri, document.Text)
		labels := completionLabels(client.request("textDocument/completion", map[string]any{
			"textDocument": map[string]any{"uri": uri},
			"position":     document.Position,
		}).Result)
		for _, label := range testCase.includes {
			if !labels.contains(label) {
				t.Fatalf("%s completions missing %q: %#v", testCase.name, label, labels)
			}
		}
		for _, label := range testCase.excludes {
			if labels.contains(label) {
				t.Fatalf("%s completions unexpectedly include %q: %#v", testCase.name, label, labels)
			}
		}
	}

	multipleClass := markedDocument(`<style>.lead { color: red; }</style>` + "\n" + `<div class="card le<<<caret>>>"></div>`)
	multipleClassURI := pathToFileURI(filepath.Join(root, "go-html-class-token-range.asp"))
	openClassicASPDocument(t, client, multipleClassURI, multipleClass.Text)
	multipleClassCompletions := completionItems(client.request("textDocument/completion", map[string]any{
		"textDocument": map[string]any{"uri": multipleClassURI},
		"position":     multipleClass.Position,
	}).Result)
	labels := completionItemLabels(multipleClassCompletions)
	if !labels.contains("lead") || labels.contains("card") {
		t.Fatalf("multiple class completions = %#v, want lead only", labels)
	}
	leadItem, ok := multipleClassCompletions.find("lead")
	if !ok || leadItem.TextEdit == nil {
		t.Fatalf("missing lead completion edit: %#v", leadItem)
	}
	if got := completionEditNewText(leadItem); got != "lead" {
		t.Fatalf("lead completion newText = %q, want lead", got)
	}
	prefixStart := positionAt(multipleClass.Text, strings.LastIndex(multipleClass.Text, "le"))
	if leadItem.TextEdit.Range != mapPositionRange(prefixStart, multipleClass.Position) {
		t.Fatalf("lead completion range = %#v, want %#v -> %#v", leadItem.TextEdit.Range, prefixStart, multipleClass.Position)
	}
}

func TestStdioParityReturnsCSSCompletionsAndColorsForStyleAttributesOutsideHTMLRoot(t *testing.T) {
	client := startStdioTestClient(t)
	defer client.close()

	root := t.TempDir()
	uri := pathToFileURI(filepath.Join(root, "go-css-style-attribute-outside-root.asp"))
	marked := markedDocument(`<header style="colo<<<caret>>>; border-color: #00ff00"></header>
<html><body></body></html>
<footer style='background: #ff0000; accent-color: #0000ff'></footer>`)
	client.request("initialize", map[string]any{
		"processId":    nil,
		"rootUri":      pathToFileURI(root),
		"capabilities": map[string]any{},
	})
	openClassicASPDocument(t, client, uri, marked.Text)
	completions := completionItems(client.request("textDocument/completion", map[string]any{
		"textDocument": map[string]any{"uri": uri},
		"position":     marked.Position,
	}).Result)
	colorItem, ok := completions.find("color")
	if !ok || colorItem.TextEdit == nil {
		t.Fatalf("missing color completion edit: %#v", colorItem)
	}
	colorStart := positionAt(marked.Text, strings.Index(marked.Text, "colo"))
	colorEnd := positionAt(marked.Text, strings.Index(marked.Text, "colo")+len("colo"))
	if colorItem.TextEdit.Range != mapPositionRange(colorStart, colorEnd) {
		t.Fatalf("color completion range = %#v, want %#v -> %#v", colorItem.TextEdit.Range, colorStart, colorEnd)
	}
	colors := documentColors(t, client.request("textDocument/documentColor", map[string]any{
		"textDocument": map[string]any{"uri": uri},
	}).Result)
	wantedColorStarts := []lsp.Position{
		mapPosition(positionAt(marked.Text, strings.Index(marked.Text, "#00ff00"))),
		mapPosition(positionAt(marked.Text, strings.Index(marked.Text, "#ff0000"))),
		mapPosition(positionAt(marked.Text, strings.Index(marked.Text, "#0000ff"))),
	}
	if len(colors) != len(wantedColorStarts) {
		t.Fatalf("document colors = %#v, want %d colors", colors, len(wantedColorStarts))
	}
	for _, want := range wantedColorStarts {
		if !colorStartsContain(colors, want) {
			t.Fatalf("document colors missing start %#v: %#v", want, colors)
		}
	}
	footerRange := mapPositionRange(
		positionAt(marked.Text, strings.Index(marked.Text, "#ff0000")),
		positionAt(marked.Text, strings.Index(marked.Text, "#ff0000")+len("#ff0000")),
	)
	var footerColor lsp.Color
	for _, color := range colors {
		if color.Range == footerRange {
			footerColor = color.Color
			break
		}
	}
	presentations := colorPresentations(t, client.request("textDocument/colorPresentation", map[string]any{
		"textDocument": map[string]any{"uri": uri},
		"color":        footerColor,
		"range":        footerRange,
	}).Result)
	if !colorPresentationLabels(presentations).contains("rgb(255, 0, 0)") {
		t.Fatalf("color presentations missing rgb label: %#v", presentations)
	}
	for _, presentation := range presentations {
		if presentation.TextEdit == nil {
			t.Fatalf("color presentation missing textEdit range: %#v", presentation)
		}
	}
}

func TestStdioParityKeepsQuotedASPIslandsFromLeakingIntoEmbeddedDiagnostics(t *testing.T) {
	client := startStdioTestClient(t)
	defer client.close()

	root := t.TempDir()
	uri := pathToFileURI(filepath.Join(root, "go-quoted-asp-island-diagnostics.asp"))
	source := strings.Join([]string{
		`<div title="<%= "double title" %>" style="content: '<%= "css title" %>'; color: #fff"></div>`,
		"<style>",
		`.banner::before { content: "<%= "double css" %>"; }`,
		`.banner::after { content: '<% 'single css %>'; }`,
		"</style>",
		"<script>",
		`const doubleQuoted = "<%= "double js" %>";`,
		`const singleQuoted = '<% 'single js %>';`,
		"const templated = `<% template js %>`;",
		"document.querySelector('.banner');",
		"</script>",
	}, "\n")
	client.request("initialize", map[string]any{
		"processId":    nil,
		"rootUri":      pathToFileURI(root),
		"capabilities": map[string]any{},
	})
	if err := client.notify("workspace/didChangeConfiguration", map[string]any{
		"settings": map[string]any{"aspLsp": map[string]any{"checkJs": true}},
	}); err != nil {
		t.Fatal(err)
	}
	openClassicASPDocument(t, client, uri, source)
	diagnostics := client.request("textDocument/diagnostic", map[string]any{
		"textDocument": map[string]any{"uri": uri},
	})
	serialized := mustJSONText(t, diagnostics.Result)
	for _, unwanted := range []string{"Unterminated string literal", "Declaration or statement expected", "asp-lsp-css", "asp-lsp-typescript"} {
		if strings.Contains(serialized, unwanted) {
			t.Fatalf("diagnostics contain %q: %s", unwanted, serialized)
		}
	}
}

func TestStdioParityKeepsVBScriptCompletionsAndCSSColorsCurrentAfterDocumentEdits(t *testing.T) {
	client := startStdioTestClient(t)
	defer client.close()

	root := t.TempDir()
	aspURI := pathToFileURI(filepath.Join(root, "go-typed-asp-region.asp"))
	colorURI := pathToFileURI(filepath.Join(root, "go-css-color-delete.asp"))
	client.request("initialize", map[string]any{
		"processId":    nil,
		"rootUri":      pathToFileURI(root),
		"capabilities": map[string]any{},
	})
	initialASP := "<div></div>"
	openClassicASPDocument(t, client, aspURI, initialASP)
	typedASP := "<div><% Response.Wri %></div>"
	if err := client.notify("textDocument/didChange", map[string]any{
		"textDocument":   map[string]any{"uri": aspURI, "version": 2},
		"contentChanges": []map[string]any{{"text": typedASP}},
	}); err != nil {
		t.Fatal(err)
	}
	client.waitForNotification("textDocument/publishDiagnostics", "")
	completions := completionLabels(client.request("textDocument/completion", map[string]any{
		"textDocument": map[string]any{"uri": aspURI},
		"position":     positionAt(typedASP, strings.Index(typedASP, "Wri")+len("Wri")),
		"context":      map[string]any{"triggerKind": 1},
	}).Result)
	if !completions.contains("Write") {
		t.Fatalf("VBScript completions missing Write after edit: %#v", completions)
	}

	initialColors := `<style>.x { color: #ff0000; }</style>
<div style="background: #00ff00"></div>`
	openClassicASPDocument(t, client, colorURI, initialColors)
	firstColors := documentColors(t, client.request("textDocument/documentColor", map[string]any{
		"textDocument": map[string]any{"uri": colorURI},
	}).Result)
	if len(firstColors) != 2 {
		t.Fatalf("initial document colors = %#v, want 2", firstColors)
	}
	withoutHexColors := `<style>.x { color: red; }</style>
<div style="background: transparent"></div>`
	if err := client.notify("textDocument/didChange", map[string]any{
		"textDocument":   map[string]any{"uri": colorURI, "version": 2},
		"contentChanges": []map[string]any{{"text": withoutHexColors}},
	}); err != nil {
		t.Fatal(err)
	}
	client.waitForNotification("textDocument/publishDiagnostics", "")
	secondColors := documentColors(t, client.request("textDocument/documentColor", map[string]any{
		"textDocument": map[string]any{"uri": colorURI},
	}).Result)
	redStart := mapPosition(positionAt(withoutHexColors, strings.Index(withoutHexColors, "red")))
	if len(secondColors) != 1 || secondColors[0].Range.Start != redStart || secondColors[0].Color.Red != 1 {
		t.Fatalf("document colors after edit = %#v, want named red", secondColors)
	}
}

func TestStdioParityRoutesColorsThroughCSSLanguageService(t *testing.T) {
	client := startStdioTestClient(t)
	defer client.close()

	root := t.TempDir()
	uri := pathToFileURI(filepath.Join(root, "go-css-language-service-colors.asp"))
	source := `<% Response.Write "color: blue" %>
<style>.named { color: red; }</style>
<div style="background: rgba(77, 33, 111, 0.5)"></div>`
	client.request("initialize", map[string]any{
		"processId": nil, "rootUri": pathToFileURI(root), "capabilities": map[string]any{},
	})
	openClassicASPDocument(t, client, uri, source)

	colors := documentColors(t, client.request("textDocument/documentColor", map[string]any{
		"textDocument": map[string]any{"uri": uri},
	}).Result)
	redRange := mapPositionRange(
		positionAt(source, strings.Index(source, "red")),
		positionAt(source, strings.Index(source, "red")+len("red")),
	)
	rgbaRange := mapPositionRange(
		positionAt(source, strings.Index(source, "rgba")),
		positionAt(source, strings.Index(source, "rgba")+len("rgba(77, 33, 111, 0.5)")),
	)
	if len(colors) != 2 || !colorStartsContain(colors, redRange.Start) || !colorStartsContain(colors, rgbaRange.Start) {
		t.Fatalf("CSS language service colors = %#v, want named and rgba colors", colors)
	}
	if colorStartsContain(colors, mapPosition(positionAt(source, strings.Index(source, "blue")))) {
		t.Fatalf("server-side string leaked into CSS colors: %#v", colors)
	}

	presentations := colorPresentations(t, client.request("textDocument/colorPresentation", map[string]any{
		"textDocument": map[string]any{"uri": uri},
		"color":        map[string]any{"red": 1, "green": 0, "blue": 0, "alpha": 1},
		"range":        redRange,
	}).Result)
	for _, label := range []string{
		"rgb(255, 0, 0)", "#ff0000", "hsl(0, 100%, 50%)", "hwb(0 0% 0%)",
		"lab(53.23% 80.11 67.22)", "lch(53.23% 104.58 40)",
		"oklab(62.793% 0.22489 0.1258)", "oklch(62.793% 0.25768 29.223)",
	} {
		if !colorPresentationLabels(presentations).contains(label) {
			t.Fatalf("color presentations missing %q: %#v", label, presentations)
		}
	}
	for _, presentation := range presentations {
		if presentation.TextEdit == nil || presentation.TextEdit.Range != redRange {
			t.Fatalf("presentation edit was not source-mapped: %#v", presentation)
		}
	}

	outside := colorPresentations(t, client.request("textDocument/colorPresentation", map[string]any{
		"textDocument": map[string]any{"uri": uri},
		"color":        map[string]any{"red": 1, "green": 0, "blue": 0, "alpha": 1},
		"range": mapPositionRange(
			positionAt(source, strings.Index(source, "blue")),
			positionAt(source, strings.Index(source, "blue")+len("blue")),
		),
	}).Result)
	if len(outside) != 0 {
		t.Fatalf("non-CSS color presentations = %#v, want none", outside)
	}
	missing := colorPresentations(t, client.request("textDocument/colorPresentation", map[string]any{
		"textDocument": map[string]any{"uri": pathToFileURI(filepath.Join(root, "missing.asp"))},
		"color":        map[string]any{"red": 1, "green": 0, "blue": 0, "alpha": 1},
		"range":        redRange,
	}).Result)
	if len(missing) != 0 {
		t.Fatalf("missing-document color presentations = %#v, want none", missing)
	}
}

func TestStdioParityReturnsVBScriptCompletionsAfterUnsavedTypedASPRegionInsertion(t *testing.T) {
	client := startStdioTestClient(t)
	defer client.close()

	root := t.TempDir()
	uri := pathToFileURI(filepath.Join(root, "go-typed-asp-region-range.asp"))
	initial := "<div></div>"
	client.request("initialize", map[string]any{
		"processId":    nil,
		"rootUri":      pathToFileURI(root),
		"capabilities": map[string]any{},
	})
	openClassicASPDocument(t, client, uri, initial)
	typed := notifyTypedInsertion(t, client, uri, initial, 1, strings.Index(initial, "</div>"), "<% Response.Wri %>")
	completions := completionLabels(client.request("textDocument/completion", map[string]any{
		"textDocument": map[string]any{"uri": uri},
		"position":     positionAt(typed.Text, strings.Index(typed.Text, "Wri")+len("Wri")),
		"context":      map[string]any{"triggerKind": 1},
	}).Result)
	if !completions.contains("Write") {
		t.Fatalf("typed ASP region completions missing Write: %#v", completions)
	}
}

func TestStdioParityDropsCSSDocumentColorsAfterColorLiteralsAreDeleted(t *testing.T) {
	client := startStdioTestClient(t)
	defer client.close()

	root := t.TempDir()
	uri := pathToFileURI(filepath.Join(root, "go-css-color-delete-ranged.asp"))
	source := `<style>.x { color: #ff0000; }</style>
<div style="background: #00ff00"></div>`
	client.request("initialize", map[string]any{
		"processId":    nil,
		"rootUri":      pathToFileURI(root),
		"capabilities": map[string]any{},
	})
	openClassicASPDocument(t, client, uri, source)
	firstColors := documentColors(t, client.request("textDocument/documentColor", map[string]any{
		"textDocument": map[string]any{"uri": uri},
	}).Result)
	if len(firstColors) != 2 ||
		!colorStartsContain(firstColors, mapPosition(positionAt(source, strings.Index(source, "#ff0000")))) ||
		!colorStartsContain(firstColors, mapPosition(positionAt(source, strings.Index(source, "#00ff00")))) {
		t.Fatalf("initial document colors = %#v, want both hex colors", firstColors)
	}

	firstRange := mapPositionRange(
		positionAt(source, strings.Index(source, "#ff0000")),
		positionAt(source, strings.Index(source, "#ff0000")+len("#ff0000")),
	)
	source = strings.Replace(source, "#ff0000", "", 1)
	if err := client.notify("textDocument/didChange", map[string]any{
		"textDocument":   map[string]any{"uri": uri, "version": 2},
		"contentChanges": []map[string]any{{"range": firstRange, "text": ""}},
	}); err != nil {
		t.Fatal(err)
	}
	styleColors := documentColors(t, client.request("textDocument/documentColor", map[string]any{
		"textDocument": map[string]any{"uri": uri},
	}).Result)
	if len(styleColors) != 1 || styleColors[0].Range.Start != mapPosition(positionAt(source, strings.Index(source, "#00ff00"))) {
		t.Fatalf("document colors after first delete = %#v, want only #00ff00", styleColors)
	}

	secondRange := mapPositionRange(
		positionAt(source, strings.Index(source, "#00ff00")),
		positionAt(source, strings.Index(source, "#00ff00")+len("#00ff00")),
	)
	if err := client.notify("textDocument/didChange", map[string]any{
		"textDocument":   map[string]any{"uri": uri, "version": 3},
		"contentChanges": []map[string]any{{"range": secondRange, "text": ""}},
	}); err != nil {
		t.Fatal(err)
	}
	finalColors := documentColors(t, client.request("textDocument/documentColor", map[string]any{
		"textDocument": map[string]any{"uri": uri},
	}).Result)
	if len(finalColors) != 0 {
		t.Fatalf("final document colors = %#v, want none", finalColors)
	}
}

func TestStdioParityKeepsCSSOnlyCompletionTriggersQuietOutsideCSSRegions(t *testing.T) {
	client := startStdioTestClient(t)
	defer client.close()

	root := t.TempDir()
	uri := pathToFileURI(filepath.Join(root, "go-css-space-trigger-outside-css.asp"))
	marked := markedDocument("<% Dim value <<<caret>>>%>")
	client.request("initialize", map[string]any{
		"processId":    nil,
		"rootUri":      pathToFileURI(root),
		"capabilities": map[string]any{},
	})
	openClassicASPDocument(t, client, uri, marked.Text)
	completions := completionItems(client.request("textDocument/completion", map[string]any{
		"textDocument": map[string]any{"uri": uri},
		"position":     marked.Position,
		"context":      map[string]any{"triggerKind": 2, "triggerCharacter": " "},
	}).Result)
	if len(completions) != 0 {
		t.Fatalf("CSS space trigger outside CSS returned completions: %#v", completions)
	}
}

func TestStdioParityResolvesHTMLAndCSSCompletionItems(t *testing.T) {
	client := startStdioTestClient(t)
	defer client.close()

	root := t.TempDir()
	uri := pathToFileURI(filepath.Join(root, "go-html-css-resolve.asp"))
	source := "<\n<style>.x { colo }</style>"
	client.request("initialize", map[string]any{
		"processId":    nil,
		"rootUri":      pathToFileURI(root),
		"capabilities": map[string]any{},
	})
	openClassicASPDocument(t, client, uri, source)
	htmlCompletions := completionItems(client.request("textDocument/completion", map[string]any{
		"textDocument": map[string]any{"uri": uri},
		"position":     map[string]any{"line": 0, "character": 1},
	}).Result)
	htmlItem, ok := htmlCompletions.find("div")
	if !ok {
		t.Fatalf("missing div completion: %#v", htmlCompletions)
	}
	if resolved := client.request("completionItem/resolve", htmlItem); !strings.Contains(mustJSONText(t, resolved.Result), "no special meaning") {
		t.Fatalf("HTML completion resolve lost service documentation: %s", mustJSONText(t, resolved.Result))
	}

	cssCompletions := completionItems(client.request("textDocument/completion", map[string]any{
		"textDocument": map[string]any{"uri": uri},
		"position":     map[string]any{"line": 1, "character": 16},
	}).Result)
	cssItem, ok := cssCompletions.find("color")
	if !ok {
		t.Fatalf("missing color completion: %#v", cssCompletions)
	}
	if resolved := client.request("completionItem/resolve", cssItem); !strings.Contains(mustJSONText(t, resolved.Result), "Sets the color") {
		t.Fatalf("CSS completion resolve lost service documentation: %s", mustJSONText(t, resolved.Result))
	}

	htmlItem.Detail = ""
	htmlItem.Documentation = nil
	if resolved := client.request("completionItem/resolve", htmlItem); !strings.Contains(mustJSONText(t, resolved.Result), "Completion provided by vscode-html-languageservice.") {
		t.Fatalf("HTML completion fallback mismatch: %s", mustJSONText(t, resolved.Result))
	}
	cssItem.Detail = ""
	cssItem.Documentation = nil
	if resolved := client.request("completionItem/resolve", cssItem); !strings.Contains(mustJSONText(t, resolved.Result), "Completion provided by vscode-css-languageservice.") {
		t.Fatalf("CSS completion fallback mismatch: %s", mustJSONText(t, resolved.Result))
	}
}

func TestStdioParityLocalizesEmbeddedCompletionResolveFallback(t *testing.T) {
	client := startStdioTestClient(t)
	defer client.close()

	root := t.TempDir()
	uri := pathToFileURI(filepath.Join(root, "go-html-css-resolve-ja.asp"))
	source := "<"
	client.request("initialize", map[string]any{
		"processId":    nil,
		"rootUri":      pathToFileURI(root),
		"locale":       "ja",
		"capabilities": map[string]any{},
	})
	openClassicASPDocument(t, client, uri, source)
	items := completionItems(client.request("textDocument/completion", map[string]any{
		"textDocument": map[string]any{"uri": uri},
		"position":     map[string]any{"line": 0, "character": 1},
	}).Result)
	item, ok := items.find("div")
	if !ok {
		t.Fatalf("missing div completion: %#v", items)
	}
	item.Detail = ""
	item.Documentation = nil
	resolved := mustJSONText(t, client.request("completionItem/resolve", item).Result)
	for _, expected := range []string{"HTML 補完", "vscode-html-languageservice による補完です。"} {
		if !strings.Contains(resolved, expected) {
			t.Fatalf("localized HTML completion resolve missing %q: %s", expected, resolved)
		}
	}
}

func TestStdioParityRoutesCSSDefinitionLikeRequestsThroughLanguageService(t *testing.T) {
	client := startStdioTestClient(t)
	defer client.close()

	root := t.TempDir()
	uri := pathToFileURI(filepath.Join(root, "go-css-definition.asp"))
	source := `<style>
:root { --theme-color: coral; }
.card { color: var(--theme-color); }
</style>`
	client.request("initialize", map[string]any{
		"processId":    nil,
		"rootUri":      pathToFileURI(root),
		"capabilities": map[string]any{},
	})
	openClassicASPDocument(t, client, uri, source)
	position := positionAt(source, strings.LastIndex(source, "--theme-color")+3)
	wantRange := mapPositionRange(
		positionAt(source, strings.Index(source, "--theme-color")),
		positionAt(source, strings.Index(source, "--theme-color")+len("--theme-color")),
	)
	for _, method := range []string{"textDocument/definition", "textDocument/declaration", "textDocument/typeDefinition"} {
		response := client.request(method, map[string]any{
			"textDocument": map[string]any{"uri": uri},
			"position":     position,
		})
		var locations []lsp.Location
		mustDecodeResult(t, response.Result, &locations)
		if len(locations) != 1 || locations[0].URI != uri || locations[0].Range != wantRange {
			t.Fatalf("%s locations = %#v", method, locations)
		}
	}
	response := client.request("textDocument/implementation", map[string]any{
		"textDocument": map[string]any{"uri": uri},
		"position":     position,
	})
	var implementations []lsp.Location
	mustDecodeResult(t, response.Result, &implementations)
	if len(implementations) != 0 {
		t.Fatalf("CSS implementation locations = %#v, want none", implementations)
	}
}

func TestStdioParityMapsCSSCompletionHoverAndStyleCloseTagPositionsBackToASPSource(t *testing.T) {
	client := startStdioTestClient(t)
	defer client.close()

	root := t.TempDir()
	client.request("initialize", map[string]any{
		"processId":    nil,
		"rootUri":      pathToFileURI(root),
		"capabilities": map[string]any{},
	})
	cssDocument := markedDocument(`<style>
.card { colo<<<caret>>>r: red; }
</style>`)
	cssURI := pathToFileURI(filepath.Join(root, "go-css-source-map.asp"))
	openClassicASPDocument(t, client, cssURI, cssDocument.Text)
	sourceMappedCompletions := completionItems(client.request("textDocument/completion", map[string]any{
		"textDocument": map[string]any{"uri": cssURI},
		"position":     cssDocument.Position,
	}).Result)
	colorCompletion, ok := sourceMappedCompletions.find("color")
	if !ok || colorCompletion.TextEdit == nil || colorCompletion.TextEdit.Range.Start.Line != 1 {
		t.Fatalf("source mapped color completion mismatch: %#v", colorCompletion)
	}
	hover := client.request("textDocument/hover", map[string]any{
		"textDocument": map[string]any{"uri": cssURI},
		"position":     cssDocument.Position,
	})
	if !strings.Contains(mustJSONText(t, hover.Result), "Sets the color") {
		t.Fatalf("CSS hover missing documentation: %s", mustJSONText(t, hover.Result))
	}
	var parsedHover lsp.Hover
	mustDecodeResult(t, hover.Result, &parsedHover)
	if parsedHover.Range == nil || parsedHover.Range.Start.Line != 1 {
		t.Fatalf("CSS hover range = %#v, want line 1", parsedHover.Range)
	}

	closeTagDocument := markedDocument(`<style>
.card { color: red; }
</<<<caret>>>style>`)
	closeTagURI := pathToFileURI(filepath.Join(root, "go-css-close-tag.asp"))
	openClassicASPDocument(t, client, closeTagURI, closeTagDocument.Text)
	closeTagCompletions := completionLabels(client.request("textDocument/completion", map[string]any{
		"textDocument": map[string]any{"uri": closeTagURI},
		"position":     closeTagDocument.Position,
	}).Result)
	if !closeTagCompletions.contains("/style") {
		t.Fatalf("close tag completions missing /style: %#v", closeTagCompletions)
	}
}

func TestStdioParityReusesCSSContextAcrossDiagnosticsAndCSSFeatureRequests(t *testing.T) {
	client := startStdioTestClient(t)
	defer client.close()

	root := t.TempDir()
	uri := pathToFileURI(filepath.Join(root, "go-css-context-cache.asp"))
	marked := markedDocument(`<style>
.card { color: red; }
.broken { color: }
.next { colo<<<caret>>> }
</style>`)
	client.request("initialize", map[string]any{
		"processId":    nil,
		"rootUri":      pathToFileURI(root),
		"capabilities": map[string]any{},
	})
	notifyConfiguration(t, client, map[string]any{"aspLsp": map[string]any{
		"debug":       map[string]any{"output": "verbose"},
		"diagnostics": map[string]any{"debounceMs": 0},
	}})
	openClassicASPDocument(t, client, uri, marked.Text)
	pulledDiagnostics := client.request("textDocument/diagnostic", map[string]any{
		"textDocument": map[string]any{"uri": uri},
	})
	var diagnosticReport struct {
		Items []diagnosticResult `json:"items"`
	}
	mustDecodeResult(t, pulledDiagnostics.Result, &diagnosticReport)
	var cssDiagnostic *diagnosticResult
	for i := range diagnosticReport.Items {
		if diagnosticReport.Items[i].Source == "asp-lsp-css" {
			cssDiagnostic = &diagnosticReport.Items[i]
			break
		}
	}
	if cssDiagnostic == nil || cssDiagnostic.Range.Start.Line != 2 {
		t.Fatalf("CSS diagnostic = %#v, want line 2", cssDiagnostic)
	}
	hover := client.request("textDocument/hover", map[string]any{
		"textDocument": map[string]any{"uri": uri},
		"position":     positionAt(marked.Text, strings.Index(marked.Text, "color: red")+1),
	})
	if !strings.Contains(mustJSONText(t, hover.Result), "Sets the color") {
		t.Fatalf("CSS hover missing documentation: %s", mustJSONText(t, hover.Result))
	}
	var parsedHover lsp.Hover
	mustDecodeResult(t, hover.Result, &parsedHover)
	if parsedHover.Range == nil || parsedHover.Range.Start.Line != 1 {
		t.Fatalf("CSS hover range = %#v, want line 1", parsedHover.Range)
	}
	completions := completionItems(client.request("textDocument/completion", map[string]any{
		"textDocument": map[string]any{"uri": uri},
		"position":     marked.Position,
	}).Result)
	colorItem, ok := completions.find("color")
	if !ok || colorItem.TextEdit == nil || colorItem.TextEdit.Range.Start.Line != 3 {
		t.Fatalf("CSS completion after diagnostics = %#v, want line 3", colorItem)
	}

	noCSSURI := pathToFileURI(filepath.Join(root, "go-css-context-cache-no-css.asp"))
	openClassicASPDocument(t, client, noCSSURI, `<% Response.Write "ok" %>`)
	firstColors := documentColors(t, client.request("textDocument/documentColor", map[string]any{
		"textDocument": map[string]any{"uri": noCSSURI},
	}).Result)
	nextColors := documentColors(t, client.request("textDocument/documentColor", map[string]any{
		"textDocument": map[string]any{"uri": noCSSURI},
	}).Result)
	if len(firstColors) != 0 || len(nextColors) != 0 {
		t.Fatalf("non-CSS document colors = %#v then %#v, want none", firstColors, nextColors)
	}
}

func TestStdioParityKeepsJavaScriptDiagnosticsStableWhenVirtualFileNamesAreNormalized(t *testing.T) {
	client := startStdioTestClient(t)
	defer client.close()

	root := t.TempDir()
	uri := "relative/a.asp"
	client.request("initialize", map[string]any{
		"processId":    nil,
		"rootUri":      pathToFileURI(root),
		"capabilities": map[string]any{},
	})
	notifyConfiguration(t, client, map[string]any{"aspLsp": map[string]any{
		"checkJs":     true,
		"diagnostics": map[string]any{"debounceMs": 0},
		"javascript":  map[string]any{"ignoreProjectConfig": true},
	}})
	openClassicASPDocument(t, client, uri, `<script>const title = document.title;</script>`)
	diagnostics := client.request("textDocument/diagnostic", map[string]any{
		"textDocument": map[string]any{"uri": uri},
	})
	if strings.Contains(mustJSONText(t, diagnostics.Result), "Could not find source file") {
		t.Fatalf("virtual filename diagnostic leaked: %s", mustJSONText(t, diagnostics.Result))
	}
}

func TestStdioParitySkipsUnreadableWorkspaceDirectoriesWhenBuildingJavaScriptProjects(t *testing.T) {
	client := startStdioTestClient(t)
	defer client.close()

	root := t.TempDir()
	unreadableDir := filepath.Join(root, "blocked")
	if err := os.Mkdir(unreadableDir, 0o755); err != nil {
		t.Fatal(err)
	}
	restoreUnreadableDir := false
	if err := os.Chmod(unreadableDir, 0o000); err == nil {
		restoreUnreadableDir = true
		defer func() {
			_ = os.Chmod(unreadableDir, 0o700)
		}()
	}
	uri := pathToFileURI(filepath.Join(root, "page.asp"))
	marked := markedDocument("<script>const safeName = 1; safe<<<caret>>></script>")
	client.request("initialize", map[string]any{
		"processId":    nil,
		"rootUri":      pathToFileURI(root),
		"capabilities": map[string]any{},
	})
	openClassicASPDocument(t, client, uri, marked.Text)
	completions := completionLabels(client.request("textDocument/completion", map[string]any{
		"textDocument": map[string]any{"uri": uri},
		"position":     marked.Position,
	}).Result)
	if !completions.contains("safeName") {
		t.Fatalf("JavaScript completions missing safeName with unreadable dir restore=%v: %#v", restoreUnreadableDir, completions)
	}
}

func TestStdioParityRenamesHTMLTagsAndCSSSelectorsThroughEmbeddedLanguageServices(t *testing.T) {
	client := startStdioTestClient(t)
	defer client.close()

	root := t.TempDir()
	uri := pathToFileURI(filepath.Join(root, "go-html-css-rename.asp"))
	source := `<div><span>name</span></div>
<style>.oldName { color: red; }</style>`
	client.request("initialize", map[string]any{
		"processId":    nil,
		"rootUri":      pathToFileURI(root),
		"capabilities": map[string]any{},
	})
	openClassicASPDocument(t, client, uri, source)
	htmlRename := client.request("textDocument/rename", map[string]any{
		"textDocument": map[string]any{"uri": uri},
		"position":     map[string]any{"line": 0, "character": 7},
		"newName":      "strong",
	})
	htmlText := mustJSONText(t, htmlRename.Result)
	if !strings.Contains(htmlText, "strong") || strings.Count(htmlText, `"newText":"strong"`) != 2 {
		t.Fatalf("HTML tag rename mismatch: %s", htmlText)
	}
	cssRename := client.request("textDocument/rename", map[string]any{
		"textDocument": map[string]any{"uri": uri},
		"position":     map[string]any{"line": 1, "character": 9},
		"newName":      "newName",
	})
	if !strings.Contains(mustJSONText(t, cssRename.Result), "newName") {
		t.Fatalf("CSS selector rename mismatch: %s", mustJSONText(t, cssRename.Result))
	}
}

func TestStdioParityRenamesHTMLClassSelectorsAcrossCSSAndJavaScriptSelectorStrings(t *testing.T) {
	client := startStdioTestClient(t)
	defer client.close()

	root := t.TempDir()
	uri := pathToFileURI(filepath.Join(root, "go-cross-rename.asp"))
	marked := markedDocument(`<div class="card ol<<<caret>>>dName"></div>
<style>.oldName { color: red; }</style>
<script>
document.querySelector(".oldName");
</script>`)
	client.request("initialize", map[string]any{
		"processId":    nil,
		"rootUri":      pathToFileURI(root),
		"capabilities": map[string]any{},
	})
	openClassicASPDocument(t, client, uri, marked.Text)
	rename := client.request("textDocument/rename", map[string]any{
		"textDocument": map[string]any{"uri": uri},
		"position":     marked.Position,
		"newName":      "newName",
	})
	serialized := mustJSONText(t, rename.Result)
	if strings.Count(serialized, "newName") < 3 ||
		!strings.Contains(serialized, `"line":0`) ||
		!strings.Contains(serialized, `"line":1`) ||
		!strings.Contains(serialized, `"line":3`) {
		t.Fatalf("cross selector rename mismatch: %s", serialized)
	}
}

func TestStdioParityRenamesHTMLClassSelectorsOnlyWithinTheActiveFile(t *testing.T) {
	client := startStdioTestClient(t)
	defer client.close()

	root := t.TempDir()
	owner := filepath.Join(root, "default.asp")
	style := filepath.Join(root, "style.inc")
	script := filepath.Join(root, "script.asp")
	marked := markedDocument(`<div class="card ol<<<caret>>>dName"></div>`)
	if err := os.WriteFile(owner, []byte(marked.Text), 0o644); err != nil {
		t.Fatal(err)
	}
	if err := os.WriteFile(style, []byte("<style>.oldName { color: red; }</style>"), 0o644); err != nil {
		t.Fatal(err)
	}
	if err := os.WriteFile(script, []byte(`<script>document.querySelector(".oldName");</script>`), 0o644); err != nil {
		t.Fatal(err)
	}
	ownerURI := pathToFileURI(owner)
	client.request("initialize", map[string]any{
		"processId":    nil,
		"rootUri":      pathToFileURI(root),
		"capabilities": map[string]any{},
	})
	openClassicASPDocument(t, client, ownerURI, marked.Text)
	rename := client.request("textDocument/rename", map[string]any{
		"textDocument": map[string]any{"uri": ownerURI},
		"position":     marked.Position,
		"newName":      "newName",
	})
	serialized := mustJSONText(t, rename.Result)
	if !strings.Contains(serialized, "default.asp") ||
		strings.Contains(serialized, "style.inc") ||
		strings.Contains(serialized, "script.asp") ||
		strings.Count(serialized, "newName") != 1 {
		t.Fatalf("active-file selector rename mismatch: %s", serialized)
	}
}

func TestStdioParityRenamesJavaScriptSelectorStringsAcrossIndexedWorkspaceFiles(t *testing.T) {
	client := startStdioTestClient(t)
	defer client.close()

	root := t.TempDir()
	owner := filepath.Join(root, "default.asp")
	style := filepath.Join(root, "style.inc")
	script := filepath.Join(root, "script.asp")
	marked := markedDocument(`<script>document.querySelector(".ol<<<caret>>>dName");</script>`)
	if err := os.WriteFile(owner, []byte(`<div class="card oldName"></div>`), 0o644); err != nil {
		t.Fatal(err)
	}
	if err := os.WriteFile(style, []byte("<style>.oldName { color: red; }</style>"), 0o644); err != nil {
		t.Fatal(err)
	}
	if err := os.WriteFile(script, []byte(marked.Text), 0o644); err != nil {
		t.Fatal(err)
	}
	scriptURI := pathToFileURI(script)
	initializeAndWaitForWorkspaceIndex(t, client, map[string]any{
		"processId":    nil,
		"rootUri":      pathToFileURI(root),
		"capabilities": map[string]any{},
	})
	notifyConfiguration(t, client, map[string]any{"aspLsp": map[string]any{"rename": map[string]any{"workspaceSymbolRename": true}}})
	waitForWorkspaceIndexRefresh(t, client)
	openClassicASPDocument(t, client, scriptURI, marked.Text)
	rename := client.request("textDocument/rename", map[string]any{
		"textDocument": map[string]any{"uri": scriptURI},
		"position":     marked.Position,
		"newName":      "newName",
	})
	serialized := mustJSONText(t, rename.Result)
	if !strings.Contains(serialized, "default.asp") ||
		!strings.Contains(serialized, "style.inc") ||
		!strings.Contains(serialized, "script.asp") ||
		strings.Count(serialized, "newName") < 3 {
		t.Fatalf("workspace JavaScript selector rename mismatch: %s", serialized)
	}
}

func TestStdioParityReturnsClassicASPIncludeCompletionsInHTMLCommentContexts(t *testing.T) {
	client := startStdioTestClient(t)
	defer client.close()

	root := t.TempDir()
	client.request("initialize", map[string]any{
		"processId":    nil,
		"rootUri":      pathToFileURI(root),
		"capabilities": map[string]any{},
	})
	cases := []struct {
		name         string
		markedSource string
		assert       func(*testing.T, completionItemList)
	}{
		{
			name:         "snippet",
			markedSource: "inc<<<caret>>>",
			assert: func(t *testing.T, items completionItemList) {
				fileSnippet, ok := items.find("#include file")
				if !ok || fileSnippet.Kind != 15 || fileSnippet.InsertTextFormat != 2 ||
					fileSnippet.FilterText != "include file inc #include file" ||
					fileSnippet.InsertText != `<!-- #include file="${1:path}" -->` ||
					fileSnippet.TextEdit == nil ||
					fileSnippet.TextEdit.NewText != `<!-- #include file="${1:path}" -->` ||
					fileSnippet.TextEdit.Range != mapPositionRange(map[string]int{"line": 0, "character": 0}, map[string]int{"line": 0, "character": 3}) {
					t.Fatalf("include file snippet mismatch: %#v", fileSnippet)
				}
			},
		},
		{
			name:         "comment-prefix",
			markedSource: "<!-- inc<<<caret>>>",
			assert: func(t *testing.T, items completionItemList) {
				fileSnippet, ok := items.find("#include file")
				if !ok || fileSnippet.TextEdit == nil ||
					fileSnippet.TextEdit.NewText != `#include file="${1:path}" -->` ||
					fileSnippet.TextEdit.Range != mapPositionRange(map[string]int{"line": 0, "character": 5}, map[string]int{"line": 0, "character": 8}) {
					t.Fatalf("include comment-prefix snippet mismatch: %#v", fileSnippet)
				}
			},
		},
		{
			name:         "comment",
			markedSource: "<!-- <<<caret>>>",
			assert: func(t *testing.T, items completionItemList) {
				includeItem, ok := items.find("#include")
				if !ok || includeItem.Kind != 14 {
					t.Fatalf("include item mismatch: %#v", includeItem)
				}
				fileSnippet, ok := items.find("#include file")
				if !ok || fileSnippet.InsertText != `#include file="${1:path}" -->` {
					t.Fatalf("include file insertText mismatch: %#v", fileSnippet)
				}
			},
		},
		{
			name:         "mode",
			markedSource: "<!-- #include <<<caret>>>",
			assert: func(t *testing.T, items completionItemList) {
				fileItem, ok := items.find("file")
				if !ok || fileItem.Kind != 10 || fileItem.InsertTextFormat != 2 || fileItem.InsertText != `file="${1:path}"` {
					t.Fatalf("include file mode item mismatch: %#v", fileItem)
				}
				virtualItem, ok := items.find("virtual")
				if !ok || virtualItem.InsertText != `virtual="${1:path}"` {
					t.Fatalf("include virtual mode item mismatch: %#v", virtualItem)
				}
			},
		},
	}
	for _, testCase := range cases {
		document := markedDocument(testCase.markedSource)
		uri := pathToFileURI(filepath.Join(root, "go-include-"+testCase.name+".asp"))
		openClassicASPDocument(t, client, uri, document.Text)
		items := completionItems(client.request("textDocument/completion", map[string]any{
			"textDocument": map[string]any{"uri": uri},
			"position":     document.Position,
		}).Result)
		testCase.assert(t, items)
	}
}

func TestStdioParityReturnsClassicASPDirectiveCompletionsByCaretContext(t *testing.T) {
	client := startStdioTestClient(t)
	defer client.close()

	root := t.TempDir()
	client.request("initialize", map[string]any{
		"processId":    nil,
		"rootUri":      pathToFileURI(root),
		"capabilities": map[string]any{},
	})
	cases := []struct {
		name           string
		markedSource   string
		expectedLabels []string
	}{
		{
			name:           "open",
			markedSource:   "<%<<<caret>>>",
			expectedLabels: []string{`<%@ Language="VBScript" CodePage=65001 %>`},
		},
		{
			name:           "attributes",
			markedSource:   "<%@ <<<caret>>> %>",
			expectedLabels: []string{"Language", "CodePage", "LCID", "Transaction", "EnableSessionState"},
		},
		{
			name:           "language-value",
			markedSource:   "<%@ Language=<<<caret>>> %>",
			expectedLabels: []string{"VBScript", "JScript", "JavaScript"},
		},
		{
			name:           "quoted-language-value",
			markedSource:   `<%@ Language="<<<caret>>>" %>`,
			expectedLabels: []string{"VBScript", "JScript", "JavaScript"},
		},
		{
			name:           "codepage-value",
			markedSource:   "<%@ CodePage=<<<caret>>> %>",
			expectedLabels: []string{"65001", "932", "1252"},
		},
	}
	for _, testCase := range cases {
		document := markedDocument(testCase.markedSource)
		uri := pathToFileURI(filepath.Join(root, "go-directive-"+testCase.name+".asp"))
		openClassicASPDocument(t, client, uri, document.Text)
		labels := completionLabels(client.request("textDocument/completion", map[string]any{
			"textDocument": map[string]any{"uri": uri},
			"position":     document.Position,
		}).Result)
		for _, expected := range testCase.expectedLabels {
			if !labels.contains(expected) {
				t.Fatalf("%s completions missing %q: %#v", testCase.name, expected, labels)
			}
		}
	}
}
