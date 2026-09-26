package lspserver

import (
	"path/filepath"
	"strings"
	"testing"

	"github.com/yottonoko/classic-web-system-lsp/internal/lsp"
)

func TestStdioParityCoversDocumentLinksFoldingPullDiagnosticsRangeTokensAndFormatting(t *testing.T) {
	client := startStdioTestClient(t)
	defer client.close()

	root := t.TempDir()
	uri := pathToFileURI(filepath.Join(root, "go-structural-surface.asp"))
	source := `<!-- #include file="includes/data.inc" -->
<section>
<style>
.panel { color: red; }
</style>
<%
If ready Then
Response.Write 1+2
End If
%>
</section>`
	client.request("initialize", map[string]any{
		"processId":    nil,
		"rootUri":      pathToFileURI(root),
		"capabilities": map[string]any{},
	})
	notifyOpenClassicASPDocument(t, client, uri, source)

	links := documentLinks(t, client.request("textDocument/documentLink", map[string]any{
		"textDocument": map[string]any{"uri": uri},
	}).Result)
	if len(links) == 0 {
		t.Fatalf("documentLink returned no links")
	}
	includeStart := strings.Index(source, `"includes/data.inc"`)
	if links[0].Range.Start != (lsp.Position{Line: 0, Character: includeStart}) ||
		links[0].Range.End != (lsp.Position{Line: 0, Character: includeStart + len(`"includes/data.inc"`)}) ||
		links[0].Target != pathToFileURI(filepath.Join(root, "includes", "data.inc")) {
		t.Fatalf("documentLink mismatch: %s", mustJSONText(t, links[0]))
	}
	resolvedLink := client.request("documentLink/resolve", links[0])
	if !strings.Contains(mustJSONText(t, resolvedLink.Result), "includes/data.inc") {
		t.Fatalf("resolved document link mismatch: %s", mustJSONText(t, resolvedLink.Result))
	}

	folding := foldingRanges(t, client.request("textDocument/foldingRange", map[string]any{
		"textDocument": map[string]any{"uri": uri},
	}).Result)
	if len(folding) < 2 || !hasFoldingRange(folding, 2, 4) || !hasFoldingRange(folding, 5, 9) {
		t.Fatalf("folding ranges mismatch: %s", mustJSONText(t, folding))
	}

	pulled := client.request("textDocument/diagnostic", map[string]any{
		"textDocument": map[string]any{"uri": uri},
	})
	if !strings.Contains(mustJSONText(t, pulled.Result), "Include file 'includes/data.inc' could not be resolved.") {
		t.Fatalf("pull diagnostics missing include resolution error: %s", mustJSONText(t, pulled.Result))
	}

	rangeTokens := client.request("textDocument/semanticTokens/range", map[string]any{
		"textDocument": map[string]any{"uri": uri},
		"range": map[string]any{
			"start": map[string]any{"line": 5, "character": 0},
			"end":   map[string]any{"line": 9, "character": 2},
		},
	})
	if len(semanticTokenData(t, rangeTokens.Result)) == 0 {
		t.Fatalf("semanticTokens/range returned no data: %s", mustJSONText(t, rangeTokens.Result))
	}

	fullEdits := textEdits(t, client.request("textDocument/formatting", map[string]any{
		"textDocument": map[string]any{"uri": uri},
		"options":      map[string]any{"tabSize": 2, "insertSpaces": true},
	}).Result)
	if len(fullEdits) == 0 || !strings.Contains(fullEdits[0].NewText, "Response.Write 1 + 2") {
		t.Fatalf("document formatting edits mismatch: %s", mustJSONText(t, fullEdits))
	}

	rangeEdits := textEdits(t, client.request("textDocument/rangeFormatting", map[string]any{
		"textDocument": map[string]any{"uri": uri},
		"range": map[string]any{
			"start": positionAt(source, strings.Index(source, "<%")),
			"end":   positionAt(source, strings.Index(source, "%>")+len("%>")),
		},
		"options": map[string]any{"tabSize": 2, "insertSpaces": true},
	}).Result)
	if len(rangeEdits) == 0 || !strings.Contains(rangeEdits[0].NewText, "Response.Write 1 + 2") {
		t.Fatalf("range formatting edits mismatch: %s", mustJSONText(t, rangeEdits))
	}

	inlineValues := client.request("textDocument/inlineValue", map[string]any{
		"textDocument": map[string]any{"uri": uri},
		"range": map[string]any{
			"start": map[string]any{"line": 0, "character": 0},
			"end":   map[string]any{"line": 10, "character": 10},
		},
		"context": map[string]any{
			"frameId": 1,
			"stoppedLocation": map[string]any{
				"start": map[string]any{"line": 0, "character": 0},
				"end":   map[string]any{"line": 0, "character": 0},
			},
		},
	})
	if got := mustJSONText(t, inlineValues.Result); got != `[]` {
		t.Fatalf("inlineValue result = %s, want []", got)
	}

	willSaveEdits := textEdits(t, client.request("textDocument/willSaveWaitUntil", map[string]any{
		"textDocument": map[string]any{"uri": uri},
		"reason":       1,
	}).Result)
	if len(willSaveEdits) != 0 {
		t.Fatalf("willSaveWaitUntil before format-on-save returned edits: %s", mustJSONText(t, willSaveEdits))
	}

	notifyConfiguration(t, client, map[string]any{"aspLsp": map[string]any{
		"format": map[string]any{"onSave": true, "indentSize": 2},
	}})
	onSaveEdits := textEdits(t, client.request("textDocument/willSaveWaitUntil", map[string]any{
		"textDocument": map[string]any{"uri": uri},
		"reason":       1,
	}).Result)
	if len(onSaveEdits) == 0 || !strings.Contains(onSaveEdits[0].NewText, "Response.Write 1 + 2") {
		t.Fatalf("format-on-save edits mismatch: %s", mustJSONText(t, onSaveEdits))
	}
}

func TestStdioParityHonorsFormatterEndOfLineAndFinalNewlineSettings(t *testing.T) {
	client := startStdioTestClient(t)
	defer client.close()

	root := t.TempDir()
	client.request("initialize", map[string]any{
		"processId":    nil,
		"rootUri":      pathToFileURI(root),
		"capabilities": map[string]any{},
	})

	crlfURI := pathToFileURI(filepath.Join(root, "go-format-eol.asp"))
	crlfSource := `<%
If enabled Then
Response.Write "ok"
End If
%>`
	notifyConfiguration(t, client, map[string]any{"aspLsp": map[string]any{
		"format": map[string]any{"endOfLine": "crlf", "indentSize": 2},
	}})
	notifyOpenClassicASPDocument(t, client, crlfURI, crlfSource)
	crlfEdits := textEdits(t, client.request("textDocument/formatting", map[string]any{
		"textDocument": map[string]any{"uri": crlfURI},
		"options":      map[string]any{"tabSize": 2, "insertSpaces": true},
	}).Result)
	if len(crlfEdits) == 0 || !strings.Contains(crlfEdits[0].NewText, "<%\r\n  If enabled Then\r\n    Response.Write \"ok\"\r\n  End If\r\n%>") {
		t.Fatalf("CRLF formatting edits mismatch: %s", mustJSONText(t, crlfEdits))
	}

	finalNewlineURI := pathToFileURI(filepath.Join(root, "go-format-final-newline.asp"))
	notifyConfiguration(t, client, map[string]any{"aspLsp": map[string]any{
		"format": map[string]any{"endOfLine": "lf", "indentSize": 2, "insertFinalNewline": true},
	}})
	notifyOpenClassicASPDocument(t, client, finalNewlineURI, `<% if enabled then %>`)
	finalNewlineEdits := textEdits(t, client.request("textDocument/formatting", map[string]any{
		"textDocument": map[string]any{"uri": finalNewlineURI},
		"options":      map[string]any{"tabSize": 2, "insertSpaces": true},
	}).Result)
	if len(finalNewlineEdits) == 0 || finalNewlineEdits[0].NewText != "<% if enabled then %>\n" {
		t.Fatalf("final newline formatting edits mismatch: %s", mustJSONText(t, finalNewlineEdits))
	}
}

func TestStdioParityFormatsEmbeddedCSSClientJavaScriptAndServerSideJScriptThroughGoPorts(t *testing.T) {
	client := startStdioTestClient(t)
	defer client.close()

	root := t.TempDir()
	uri := pathToFileURI(filepath.Join(root, "go-embedded-formatting.asp"))
	source := `<style>.x{color:red;}</style>
<script>function boot(){return 1;}</script>
<%
function greet(name){return "a=b";}
%>`
	client.request("initialize", map[string]any{
		"processId":    nil,
		"rootUri":      pathToFileURI(root),
		"capabilities": map[string]any{},
	})
	notifyConfiguration(t, client, map[string]any{"aspLsp": map[string]any{
		"defaultLanguage": "JScript",
	}})
	notifyOpenClassicASPDocument(t, client, uri, source)

	edits := textEdits(t, client.request("textDocument/formatting", map[string]any{
		"textDocument": map[string]any{"uri": uri},
		"options":      map[string]any{"tabSize": 2, "insertSpaces": true},
	}).Result)
	if len(edits) == 0 {
		t.Fatalf("embedded formatting returned no edits")
	}
	formatted := edits[0].NewText
	for _, expected := range []string{
		".x {",
		"color: red;",
		"function boot() {",
		"return 1;",
		"function greet(name) {",
		`return "a=b";`,
	} {
		if !strings.Contains(formatted, expected) {
			t.Fatalf("embedded formatting missing %q: %s", expected, formatted)
		}
	}
}

func TestStdioParityReturnsFormatEditsFromWillSaveWaitUntilWhenFormatOnSaveEnabled(t *testing.T) {
	client := startStdioTestClient(t)
	defer client.close()

	root := t.TempDir()
	uri := pathToFileURI(filepath.Join(root, "go-will-save-format.asp"))
	source := `<style>.x{color:red}</style>
<%
If enabled Then
Response.Write "ok"
End If
%>`
	client.request("initialize", map[string]any{
		"processId":    nil,
		"rootUri":      pathToFileURI(root),
		"capabilities": map[string]any{},
	})
	notifyConfiguration(t, client, map[string]any{"aspLsp": map[string]any{
		"format": map[string]any{"onSave": true, "indentSize": 2},
	}})
	notifyOpenClassicASPDocument(t, client, uri, source)

	edits := textEdits(t, client.request("textDocument/willSaveWaitUntil", map[string]any{
		"textDocument": map[string]any{"uri": uri},
		"reason":       1,
	}).Result)
	serialized := mustJSONText(t, edits)
	if len(edits) == 0 || !strings.Contains(serialized, "color: red") || !strings.Contains(serialized, "  Response.Write") {
		t.Fatalf("format-on-save willSave edits mismatch: %s", serialized)
	}
}

func TestStdioParityRespectsASPFormatterDisableMarkers(t *testing.T) {
	client := startStdioTestClient(t)
	defer client.close()

	root := t.TempDir()
	uri := pathToFileURI(filepath.Join(root, "go-format-disable-markers.asp"))
	source := `<%
' asp-format off
If enabled Then
Response.Write   "keep spacing"
End If
' asp-format on
If enabled Then
Response.Write "ok"
End If
%>`
	client.request("initialize", map[string]any{
		"processId":    nil,
		"rootUri":      pathToFileURI(root),
		"capabilities": map[string]any{},
	})
	notifyConfiguration(t, client, map[string]any{"aspLsp": map[string]any{
		"format": map[string]any{"indentSize": 2},
	}})
	notifyOpenClassicASPDocument(t, client, uri, source)

	edits := textEdits(t, client.request("textDocument/formatting", map[string]any{
		"textDocument": map[string]any{"uri": uri},
		"options":      map[string]any{"tabSize": 2, "insertSpaces": true},
	}).Result)
	formatted := firstEditText(edits, "")
	if !strings.Contains(formatted, `' asp-format off
If enabled Then
Response.Write   "keep spacing"
End If
' asp-format on`) {
		t.Fatalf("disable marker block was reformatted: %s", formatted)
	}
	if !strings.Contains(formatted, `  If enabled Then
    Response.Write "ok"
  End If`) {
		t.Fatalf("enabled block was not formatted: %s", formatted)
	}

	rangeEdits := textEdits(t, client.request("textDocument/rangeFormatting", map[string]any{
		"textDocument": map[string]any{"uri": uri},
		"range": map[string]any{
			"start": map[string]any{"line": 1, "character": 0},
			"end":   map[string]any{"line": 4, "character": 0},
		},
		"options": map[string]any{"tabSize": 2, "insertSpaces": true},
	}).Result)
	if len(rangeEdits) != 0 {
		t.Fatalf("range formatting inside disabled block returned edits: %s", mustJSONText(t, rangeEdits))
	}
}

func TestStdioParityFormatsFullASPDocumentsAndASPRangesOverJSONRPC(t *testing.T) {
	client := startStdioTestClient(t)
	defer client.close()

	root := t.TempDir()
	uri := pathToFileURI(filepath.Join(root, "go-format-parity.asp"))
	source := `<html>
<body>
<style>.x{color:red}</style>
<% Option Explicit
If enabled Then
Response.Write "ok"
End If
%>
<div>done</div>
</body>
</html>`
	client.request("initialize", map[string]any{
		"processId":    nil,
		"rootUri":      pathToFileURI(root),
		"capabilities": map[string]any{},
	})
	notifyConfiguration(t, client, map[string]any{"aspLsp": map[string]any{
		"debug": map[string]any{"output": "summary"},
	}})
	notifyOpenClassicASPDocument(t, client, uri, source)
	analysisCompleted, seenAnalysisLogs := client.waitForNotificationWithSeen("window/logMessage", "LSP analysis completed")
	analysisLogText := mustJSONText(t, append(seenAnalysisLogs, analysisCompleted))
	if !strings.Contains(analysisLogText, "LSP analysis started") {
		t.Fatalf("analysis logs missing start: %s", analysisLogText)
	}
	expectElapsedLogWithoutHeat(t, analysisCompleted)
	checkCompleted, seenCheckLogs := client.waitForNotificationWithSeen("window/logMessage", "LSP check completed")
	checkLogs := append([]*rpcMessage{}, seenAnalysisLogs...)
	checkLogs = append(checkLogs, analysisCompleted)
	checkLogs = append(checkLogs, seenCheckLogs...)
	checkLogs = append(checkLogs, checkCompleted)
	checkLogText := mustJSONText(t, checkLogs)
	if !strings.Contains(checkLogText, "LSP check started") {
		t.Fatalf("check logs missing start: %s", checkLogText)
	}
	expectElapsedLogWithoutHeat(t, checkCompleted)

	fullEdits := textEdits(t, client.request("textDocument/formatting", map[string]any{
		"textDocument": map[string]any{"uri": uri},
		"options":      map[string]any{"tabSize": 2, "insertSpaces": true},
	}).Result)
	client.waitForLogContaining("Formatting conversion started (document)")
	expectElapsedLogWithoutHeat(t, client.waitForLogContaining("Formatting conversion completed (document)"))
	if len(fullEdits) == 0 {
		t.Fatalf("full formatting returned no edits")
	}
	fullText := fullEdits[0].NewText
	for _, expected := range []string{"<%", "  Response.Write", ".x {", "color: red"} {
		if !strings.Contains(fullText, expected) {
			t.Fatalf("full formatting edits missing %q: %s", expected, fullText)
		}
	}

	islandURI := pathToFileURI(filepath.Join(root, "go-format-embedded-asp-islands.asp"))
	islandSource := `<style>.x{color:<%= themeColor %>;width:<% Response.Write width %>px}</style>
<div style="color:<%= themeColor %>;background:red"></div>
<script>const value=<%= serverValue %>;const dynamic=<% Response.Write clientValue %>;</script>`
	notifyOpenClassicASPDocument(t, client, islandURI, islandSource)
	islandEdits := textEdits(t, client.request("textDocument/formatting", map[string]any{
		"textDocument": map[string]any{"uri": islandURI},
		"options":      map[string]any{"tabSize": 2, "insertSpaces": true},
	}).Result)
	islandText := islandSource
	if len(islandEdits) > 0 {
		islandText = islandEdits[0].NewText
	}
	for _, expected := range []string{"<%= themeColor %>", "<% Response.Write width %>", "<% Response.Write clientValue %>"} {
		if !strings.Contains(islandText, expected) {
			t.Fatalf("formatted island text lost %q: %s", expected, islandText)
		}
	}

	rangeEdits := textEdits(t, client.request("textDocument/rangeFormatting", map[string]any{
		"textDocument": map[string]any{"uri": uri},
		"range": map[string]any{
			"start": map[string]any{"line": 2, "character": 0},
			"end":   map[string]any{"line": 6, "character": 2},
		},
		"options": map[string]any{"tabSize": 2, "insertSpaces": true},
	}).Result)
	client.waitForLogContaining("Formatting conversion started (range)")
	expectElapsedLogWithoutHeat(t, client.waitForLogContaining("Formatting conversion completed (range)"))
	rangeText := mustJSONText(t, rangeEdits)
	if !strings.Contains(rangeText, "  Response.Write") || strings.Contains(rangeText, "<html>") {
		t.Fatalf("range formatting edits mismatch: %s", rangeText)
	}
}

func TestStdioParityDelegatesServerSideJScriptFormattingToTypeScriptFormatter(t *testing.T) {
	client := startStdioTestClient(t)
	defer client.close()

	root := t.TempDir()
	uri := pathToFileURI(filepath.Join(root, "go-jscript-format.asp"))
	source := `<%@ LANGUAGE="JScript" %>
<%
function greet(name){return "a=b";}
%>`
	client.request("initialize", map[string]any{
		"processId":    nil,
		"rootUri":      pathToFileURI(root),
		"capabilities": map[string]any{},
	})
	notifyOpenClassicASPDocument(t, client, uri, source)

	fullEdits := textEdits(t, client.request("textDocument/formatting", map[string]any{
		"textDocument": map[string]any{"uri": uri},
		"options":      map[string]any{"tabSize": 2, "insertSpaces": true},
	}).Result)
	fullText := firstEditText(fullEdits, "")
	if !strings.Contains(fullText, "function greet(name) {") || !strings.Contains(fullText, `"a=b"`) {
		t.Fatalf("full JScript formatting mismatch: %s", fullText)
	}

	rangeEdits := textEdits(t, client.request("textDocument/rangeFormatting", map[string]any{
		"textDocument": map[string]any{"uri": uri},
		"range": map[string]any{
			"start": map[string]any{"line": 1, "character": 0},
			"end":   map[string]any{"line": 3, "character": 2},
		},
		"options": map[string]any{"tabSize": 2, "insertSpaces": true},
	}).Result)
	rangeText := firstEditText(rangeEdits, "")
	if !strings.Contains(rangeText, "function greet(name) {") || strings.Contains(rangeText, "<%@") {
		t.Fatalf("range JScript formatting mismatch: %s", rangeText)
	}
}

func TestStdioParityFormatsEmbeddedLanguagesRelativeToTagIndentationByDefault(t *testing.T) {
	source := `<html>
<body>
  <style>.x{color:red}</style>
  <script>
function greet(){
console.log("x");
}
  </script>
  <%
If enabled Then
Response.Write "ok"
End If
  %>
</body>
</html>`
	client := startStdioTestClient(t)
	defer client.close()
	root := t.TempDir()
	uri := pathToFileURI(filepath.Join(root, "go-format-tag-indent-parity.asp"))
	client.request("initialize", map[string]any{
		"processId":    nil,
		"rootUri":      pathToFileURI(root),
		"capabilities": map[string]any{},
	})
	notifyConfiguration(t, client, map[string]any{"aspLsp": map[string]any{
		"format": map[string]any{"indentSize": 2},
	}})
	notifyOpenClassicASPDocument(t, client, uri, source)

	fullText := firstEditText(textEdits(t, client.request("textDocument/formatting", map[string]any{
		"textDocument": map[string]any{"uri": uri},
		"options":      map[string]any{"tabSize": 2, "insertSpaces": true},
	}).Result), source)
	for _, expected := range []string{
		"  <style>\n    .x {\n      color: red\n    }\n  </style>",
		"  <script>\n    function greet() {\n      console.log(\"x\");\n    }\n  </script>",
		"  <%\n    If enabled Then\n      Response.Write \"ok\"\n    End If\n  %>",
	} {
		if !strings.Contains(fullText, expected) {
			t.Fatalf("tag-indented formatting missing %q:\n%s", expected, fullText)
		}
	}

	rangeText := mustJSONText(t, client.request("textDocument/rangeFormatting", map[string]any{
		"textDocument": map[string]any{"uri": uri},
		"range": map[string]any{
			"start": map[string]any{"line": 2, "character": 0},
			"end":   map[string]any{"line": 3, "character": 0},
		},
		"options": map[string]any{"tabSize": 2, "insertSpaces": true},
	}).Result)
	if !strings.Contains(rangeText, ".x {") || strings.Contains(rangeText, "<html>") {
		t.Fatalf("tag-indented range formatting mismatch: %s", rangeText)
	}
}

func TestStdioParityUsesSeparateVBScriptIndentationSettingsFromHTMLCSSAndJavaScript(t *testing.T) {
	source := `<html>
<body>
<style>.x{color:red}</style>
<script>
function greet(){
console.log("x");
}
</script>
<%
If enabled Then
Response.Write "ok"
End If
%>
</body>
</html>`
	formatted := formatClassicASPDocumentForParity(t, source, map[string]any{
		"format": map[string]any{"indentSize": 4, "vbscriptIndentSize": 2},
	}, "go-format-vbscript-indent-parity.asp")
	for _, expected := range []string{
		"<body>\n    <style>",
		"    <style>\n        .x {\n            color: red\n        }\n    </style>",
		"    <script>\n        function greet() {\n            console.log(\"x\");\n        }\n    </script>",
		"    <%\n      If enabled Then\n        Response.Write \"ok\"\n      End If\n    %>",
	} {
		if !strings.Contains(formatted, expected) {
			t.Fatalf("separate indent formatting missing %q:\n%s", expected, formatted)
		}
	}
}

func TestStdioParityHonorsFormatterEnabledLanguagesAndVBScriptIndentationSettings(t *testing.T) {
	client := startStdioTestClient(t)
	defer client.close()

	root := t.TempDir()
	client.request("initialize", map[string]any{
		"processId":    nil,
		"rootUri":      pathToFileURI(root),
		"capabilities": map[string]any{},
	})

	uri := pathToFileURI(filepath.Join(root, "go-format-settings.asp"))
	source := `<style>.x{color:red;}</style>
<script>function greet(){console.log("x");}</script>
<%
If enabled Then
Response.Write "ok"
End If
%>`
	notifyConfiguration(t, client, map[string]any{"aspLsp": map[string]any{
		"format": map[string]any{
			"enabledLanguages":   []string{"vbscript"},
			"indentSize":         4,
			"vbscriptIndentSize": 2,
		},
	}})
	notifyOpenClassicASPDocument(t, client, uri, source)
	formatted := firstEditText(textEdits(t, client.request("textDocument/formatting", map[string]any{
		"textDocument": map[string]any{"uri": uri},
		"options":      map[string]any{"tabSize": 4, "insertSpaces": true},
	}).Result), "")
	if !strings.Contains(formatted, "<style>.x{color:red;}</style>") ||
		!strings.Contains(formatted, `<script>function greet(){console.log("x");}</script>`) ||
		!strings.Contains(formatted, "<%\n  If enabled Then\n    Response.Write \"ok\"\n  End If\n%>") {
		t.Fatalf("enabled language formatting mismatch: %s", formatted)
	}

	embeddedOffURI := pathToFileURI(filepath.Join(root, "go-format-embedded-off.asp"))
	notifyConfiguration(t, client, map[string]any{"aspLsp": map[string]any{
		"format": map[string]any{
			"embeddedLanguageFormatting": "off",
			"enabledLanguages":           []string{"vbscript", "css", "javascript"},
			"indentSize":                 2,
		},
	}})
	notifyOpenClassicASPDocument(t, client, embeddedOffURI, `<style>.x{color:red}</style><script>function greet(){console.log("x");}</script><%if enabled then%>`)
	embeddedOffText := firstEditText(textEdits(t, client.request("textDocument/formatting", map[string]any{
		"textDocument": map[string]any{"uri": embeddedOffURI},
		"options":      map[string]any{"tabSize": 2, "insertSpaces": true},
	}).Result), "")
	for _, expected := range []string{".x{color:red}", `function greet(){console.log("x");}`, "<% if enabled then %>"} {
		if !strings.Contains(embeddedOffText, expected) {
			t.Fatalf("embedded off formatting missing %q: %s", expected, embeddedOffText)
		}
	}

	vbscriptTabURI := pathToFileURI(filepath.Join(root, "go-format-vbscript-tab.asp"))
	notifyConfiguration(t, client, map[string]any{"aspLsp": map[string]any{
		"format": map[string]any{
			"embeddedLanguageFormatting": "auto",
			"enabledLanguages":           []string{"vbscript"},
			"indentSize":                 2,
			"vbscriptIndentStyle":        "tab",
		},
	}})
	notifyOpenClassicASPDocument(t, client, vbscriptTabURI, `<%
If enabled Then
Response.Write "ok"
End If
%>`)
	vbscriptTabText := firstEditText(textEdits(t, client.request("textDocument/formatting", map[string]any{
		"textDocument": map[string]any{"uri": vbscriptTabURI},
		"options":      map[string]any{"tabSize": 2, "insertSpaces": true},
	}).Result), "")
	if !strings.Contains(vbscriptTabText, "\tIf enabled Then\n\t\tResponse.Write \"ok\"\n\tEnd If") {
		t.Fatalf("VBScript tab formatting mismatch: %s", vbscriptTabText)
	}
}

func TestStdioParityFormatsTopLevelHTMLAsFragmentWhenConfigured(t *testing.T) {
	source := `<div><span>one</span></div>
<section><p>two</p></section>`
	formatted := formatClassicASPDocumentForParity(t, source, map[string]any{
		"format": map[string]any{"indentSize": 2, "fragmentMode": "fragment"},
	}, "go-format-fragment-mode.asp")
	expected := `<div><span>one</span></div>
<section>
  <p>two</p>
</section>`
	if formatted != expected || strings.Contains(formatted, "asp-lsp-fragment") {
		t.Fatalf("fragment formatting mismatch:\n got: %q\nwant: %q", formatted, expected)
	}
}

func TestStdioParityFormatsHTMLTagsWithoutRewritingASPCSSOrJavaScriptBodiesAsHTML(t *testing.T) {
	client := startStdioTestClient(t)
	defer client.close()

	root := t.TempDir()
	source := `<html><body><main><section><h1><%= title %></h1></section></main><style>.card{color:red}</style><script>
function greet(){
console.log("x");
}
</script><%
If enabled Then
Response.Write "ok"
End If
%></body></html>`
	uri := pathToFileURI(filepath.Join(root, "go-format-html-tags.asp"))
	client.request("initialize", map[string]any{
		"processId":    nil,
		"rootUri":      pathToFileURI(root),
		"capabilities": map[string]any{},
	})
	notifyConfiguration(t, client, map[string]any{"aspLsp": map[string]any{
		"format": map[string]any{"indentSize": 2},
	}})
	notifyOpenClassicASPDocument(t, client, uri, source)

	fullText := firstEditText(textEdits(t, client.request("textDocument/formatting", map[string]any{
		"textDocument": map[string]any{"uri": uri},
		"options":      map[string]any{"tabSize": 2, "insertSpaces": true},
	}).Result), source)
	for _, expected := range []string{
		"<body>\n  <main>\n    <section>",
		"<h1><%= title %></h1>",
		"  <style>\n    .card {\n      color: red\n    }\n  </style>",
		"  <script>\n    function greet() {\n      console.log(\"x\");\n    }\n  </script>",
		"  <%\n    If enabled Then\n      Response.Write \"ok\"\n    End If\n  %>",
	} {
		if !strings.Contains(fullText, expected) {
			t.Fatalf("HTML tag formatting missing %q:\n%s", expected, fullText)
		}
	}
	if strings.Contains(fullText, "<% If enabled Then Response.Write") {
		t.Fatalf("HTML formatter rewrote ASP body as HTML: %s", fullText)
	}

	secondURI := pathToFileURI(filepath.Join(root, "go-format-html-tags-idempotent.asp"))
	notifyOpenClassicASPDocument(t, client, secondURI, fullText)
	secondEdits := textEdits(t, client.request("textDocument/formatting", map[string]any{
		"textDocument": map[string]any{"uri": secondURI},
		"options":      map[string]any{"tabSize": 2, "insertSpaces": true},
	}).Result)
	if len(secondEdits) != 0 {
		t.Fatalf("idempotent HTML tag formatting returned edits: %s", mustJSONText(t, secondEdits))
	}
}

func TestStdioParityCanIgnoreEmbeddedTagIndentationPerLanguage(t *testing.T) {
	source := `<html>
<body>
  <style>.x{color:red}</style>
  <script>
function greet(){
console.log("x");
}
  </script>
  <%
If enabled Then
Response.Write "ok"
End If
  %>
</body>
</html>`
	baseFormat := map[string]any{
		"indentSize":                2,
		"cssTagIndentMode":          "",
		"javascriptTagIndentMode":   "",
		"vbscriptTagIndentMode":     "",
		"ignoreCssTagIndent":        false,
		"ignoreJavaScriptTagIndent": false,
		"ignoreVbscriptTagIndent":   false,
	}
	formatWith := func(name string, settings map[string]any) string {
		format := map[string]any{}
		for key, value := range baseFormat {
			format[key] = value
		}
		for key, value := range settings {
			format[key] = value
		}
		return formatClassicASPDocumentForParity(t, source, map[string]any{"format": format}, "go-format-ignore-tag-indent-"+name+".asp")
	}

	cssIgnored := formatWith("css-bool", map[string]any{"ignoreCssTagIndent": true})
	if !strings.Contains(cssIgnored, "  <style>\n.x {") ||
		!strings.Contains(cssIgnored, "  <script>\n    function greet() {") ||
		!strings.Contains(cssIgnored, "  <%\n    If enabled Then") {
		t.Fatalf("ignoreCssTagIndent formatting mismatch:\n%s", cssIgnored)
	}
	cssModeIgnored := formatWith("css-mode", map[string]any{"cssTagIndentMode": "ignoreTag"})
	if !strings.Contains(cssModeIgnored, "  <style>\n.x {") {
		t.Fatalf("cssTagIndentMode ignoreTag formatting mismatch:\n%s", cssModeIgnored)
	}

	jsIgnored := formatWith("js-bool", map[string]any{"ignoreJavaScriptTagIndent": true})
	if !strings.Contains(jsIgnored, "  <style>\n    .x {") ||
		!strings.Contains(jsIgnored, "  <script>\nfunction greet() {") ||
		!strings.Contains(jsIgnored, "  <%\n    If enabled Then") {
		t.Fatalf("ignoreJavaScriptTagIndent formatting mismatch:\n%s", jsIgnored)
	}
	jsModeIgnored := formatWith("js-mode", map[string]any{"javascriptTagIndentMode": "ignoreTag"})
	if !strings.Contains(jsModeIgnored, "  <script>\nfunction greet() {") {
		t.Fatalf("javascriptTagIndentMode ignoreTag formatting mismatch:\n%s", jsModeIgnored)
	}

	vbscriptIgnored := formatWith("vb-bool", map[string]any{"ignoreVbscriptTagIndent": true})
	if !strings.Contains(vbscriptIgnored, "  <style>\n    .x {") ||
		!strings.Contains(vbscriptIgnored, "  <script>\n    function greet() {") ||
		!strings.Contains(vbscriptIgnored, "  <%\n  If enabled Then") {
		t.Fatalf("ignoreVbscriptTagIndent formatting mismatch:\n%s", vbscriptIgnored)
	}
	vbscriptModeIgnored := formatWith("vb-mode", map[string]any{"vbscriptTagIndentMode": "ignoreTag"})
	if !strings.Contains(vbscriptModeIgnored, "  <%\n  If enabled Then") {
		t.Fatalf("vbscriptTagIndentMode ignoreTag formatting mismatch:\n%s", vbscriptModeIgnored)
	}
	vbscriptAligned := formatWith("vb-align", map[string]any{"vbscriptBlockIndent": "alignWithDelimiter"})
	if !strings.Contains(vbscriptAligned, "  <%\n  If enabled Then") {
		t.Fatalf("vbscriptBlockIndent alignWithDelimiter formatting mismatch:\n%s", vbscriptAligned)
	}
}

func TestStdioParityAppliesGoHTMLFormatterIndentationSettings(t *testing.T) {
	source := `<div><section><p>x</p></section></div>`
	sized := formatClassicASPDocumentForParity(t, source, map[string]any{
		"format": map[string]any{"indentSize": 2, "htmlIndentSize": 3},
	}, "go-format-html-indent-size.asp")
	if !strings.Contains(sized, "\n   <section>") || !strings.Contains(sized, "\n      <p>x</p>") {
		t.Fatalf("HTML indent size formatting mismatch:\n%s", sized)
	}

	tabbed := formatClassicASPDocumentForParity(t, source, map[string]any{
		"format": map[string]any{"indentSize": 2, "htmlIndentStyle": "tab"},
	}, "go-format-html-indent-style.asp")
	if !strings.Contains(tabbed, "\n\t<section>") || !strings.Contains(tabbed, "\n\t\t<p>x</p>") {
		t.Fatalf("HTML indent style formatting mismatch:\n%s", tabbed)
	}
}

func TestStdioParityAppliesGoHTMLFormatterWrappingAndAdvancedSettings(t *testing.T) {
	source := `<div class="a" id="b" data-x="c"></div>`
	printWidth := formatClassicASPDocumentForParity(t, source, map[string]any{
		"format": map[string]any{"indentSize": 2, "printWidth": 20},
	}, "go-format-html-print-width.asp")
	if !strings.Contains(printWidth, "<div class=\"a\"\n  id=\"b\" data-x=\"c\">") {
		t.Fatalf("HTML print width formatting mismatch:\n%s", printWidth)
	}

	forced := formatClassicASPDocumentForParity(t, source, map[string]any{
		"format": map[string]any{
			"htmlWrapAttributes":           "force",
			"htmlWrapAttributesIndentSize": 6,
			"indentSize":                   2,
		},
	}, "go-format-html-wrap-attributes.asp")
	if !strings.Contains(forced, "<div class=\"a\"\n      id=\"b\"\n      data-x=\"c\">") {
		t.Fatalf("HTML forced attribute wrapping mismatch:\n%s", forced)
	}

	preserved := formatClassicASPDocumentForParity(t, `<div>


<span>x</span>
</div>`, map[string]any{
		"format": map[string]any{
			"indentEmptyLines":    true,
			"indentSize":          2,
			"maxPreserveNewLines": 1,
			"preserveNewLines":    true,
		},
	}, "go-format-html-advanced-preserve.asp")
	if !strings.Contains(preserved, "<div>\n\n  <span>x</span>\n</div>") || strings.Contains(preserved, "\n  \n") || strings.Contains(preserved, "\n\n\n") {
		t.Fatalf("HTML preserved newline formatting mismatch:\n%s", preserved)
	}

	innerHTML := formatClassicASPDocumentForParity(t, `<html><body><main><p>x</p></main></body></html>`, map[string]any{
		"format": map[string]any{
			"htmlIndentInnerHtml": true,
			"indentEmptyLines":    false,
			"indentSize":          2,
		},
	}, "go-format-html-advanced-inner.asp")
	if !strings.Contains(innerHTML, "<html>\n\n  <body>\n    <main>") {
		t.Fatalf("HTML innerHTML indentation mismatch:\n%s", innerHTML)
	}
}

func TestStdioParityAppliesGoCSSFormatterSettings(t *testing.T) {
	formatWith := func(name string, source string, format map[string]any) string {
		return formatClassicASPDocumentForParity(t, source, map[string]any{"format": format}, "go-format-css-"+name+".asp")
	}

	sized := formatWith("indent-size", `<style>.x{color:red}</style><%Response.Write ""%>`, map[string]any{
		"cssIndentSize": 3,
		"indentSize":    2,
	})
	if !strings.Contains(sized, "<style>\n  .x {") || !strings.Contains(sized, "\n     color: red") {
		t.Fatalf("CSS indent size formatting mismatch:\n%s", sized)
	}

	tabbed := formatWith("indent-style", `<style>.x{color:red}</style><%Response.Write ""%>`, map[string]any{
		"cssIndentStyle": "tab",
		"indentSize":     2,
	})
	if !strings.Contains(tabbed, "<style>\n  .x {\n  \tcolor: red") {
		t.Fatalf("CSS indent style formatting mismatch:\n%s", tabbed)
	}

	compactRules := formatWith("compact-rules", `<style>.a{color:red}.b{color:blue}</style><%Response.Write ""%>`, map[string]any{
		"cssIndentSize":          2,
		"cssIndentStyle":         "space",
		"cssNewlineBetweenRules": false,
		"indentSize":             2,
	})
	if !strings.Contains(compactRules, "color: red\n  }\n  .b {") ||
		strings.Contains(compactRules, "color: red\n  }\n\n  .b {") {
		t.Fatalf("CSS compact rules formatting mismatch:\n%s", compactRules)
	}

	selectorLine := formatWith("selector-line", `<style>.a,.b{color:red}</style><%Response.Write ""%>`, map[string]any{
		"cssNewlineBetweenSelectors":      false,
		"cssSpaceAroundSelectorSeparator": true,
		"indentSize":                      2,
	})
	if !strings.Contains(selectorLine, ".a, .b {") || strings.Contains(selectorLine, ".a,\n.b {") {
		t.Fatalf("CSS selector separator formatting mismatch:\n%s", selectorLine)
	}

	expandedBrace := formatWith("expanded-brace", `<style>.x{color:red}</style><%Response.Write ""%>`, map[string]any{
		"cssBraceStyle": "expand",
		"indentSize":    2,
	})
	if !strings.Contains(expandedBrace, ".x\n  {") {
		t.Fatalf("CSS expanded brace formatting mismatch:\n%s", expandedBrace)
	}
}

func TestStdioParityAppliesGoJavaScriptFormatterSettings(t *testing.T) {
	formatWith := func(name string, source string, format map[string]any, extra map[string]any) string {
		settings := map[string]any{"format": format}
		for key, value := range extra {
			settings[key] = value
		}
		return formatClassicASPDocumentForParity(t, source, settings, "go-format-js-"+name+".asp")
	}

	sized := formatWith("indent-size", `<script>
if(x){
foo();
}
</script><%Response.Write ""%>`, map[string]any{
		"indentSize":           2,
		"javascriptIndentSize": 3,
	}, nil)
	if !strings.Contains(sized, "\n     foo();") {
		t.Fatalf("JavaScript indent size formatting mismatch:\n%s", sized)
	}

	tabbed := formatWith("indent-style", `<script>
if(x){
foo();
}
</script><%Response.Write ""%>`, map[string]any{
		"indentSize":            2,
		"javascriptIndentStyle": "tab",
	}, nil)
	if !strings.Contains(tabbed, "\n  \tfoo();") {
		t.Fatalf("JavaScript indent style formatting mismatch:\n%s", tabbed)
	}

	jscriptSized := formatWith("jscript-indent", `<%@ LANGUAGE="JScript" %>
<%
if(x){
foo();
}
%>`, map[string]any{
		"indentSize":         2,
		"jscriptIndentSize":  3,
		"jscriptIndentStyle": "space",
	}, map[string]any{"defaultLanguage": "JScript"})
	if !strings.Contains(jscriptSized, "\n   foo();") {
		t.Fatalf("JScript indent size formatting mismatch:\n%s", jscriptSized)
	}

	anonSpace := formatWith("anon-space", `<script>const f=function(){return 1;}</script><%Response.Write ""%>`, map[string]any{
		"indentSize": 2,
		"javascriptInsertSpaceAfterFunctionKeywordForAnonymousFunctions": true,
	}, nil)
	if !strings.Contains(anonSpace, "function ()") {
		t.Fatalf("JavaScript anonymous function space formatting mismatch:\n%s", anonSpace)
	}

	expandedBrace := formatWith("expanded-brace", `<script>
function f(){
return 1;
}
</script><%Response.Write ""%>`, map[string]any{
		"indentSize":            2,
		"javascriptIndentSize":  2,
		"javascriptIndentStyle": "space",
		"javascriptPlaceOpenBraceOnNewLineForFunctions": true,
	}, nil)
	if !strings.Contains(expandedBrace, "function f()\n  {") {
		t.Fatalf("JavaScript expanded brace formatting mismatch:\n%s", expandedBrace)
	}
}

func documentLinks(t *testing.T, value any) []lsp.DocumentLink {
	t.Helper()
	var links []lsp.DocumentLink
	mustDecodeResult(t, value, &links)
	return links
}

func foldingRanges(t *testing.T, value any) []lsp.FoldingRange {
	t.Helper()
	var ranges []lsp.FoldingRange
	mustDecodeResult(t, value, &ranges)
	return ranges
}

func textEdits(t *testing.T, value any) []lsp.TextEdit {
	t.Helper()
	var edits []lsp.TextEdit
	mustDecodeResult(t, value, &edits)
	return edits
}

func firstEditText(edits []lsp.TextEdit, fallback string) string {
	if len(edits) == 0 {
		return fallback
	}
	return edits[0].NewText
}

func formatClassicASPDocumentForParity(t *testing.T, source string, aspLspSettings map[string]any, fileName string) string {
	t.Helper()
	client := startStdioTestClient(t)
	defer client.close()
	root := t.TempDir()
	uri := pathToFileURI(filepath.Join(root, fileName))
	client.request("initialize", map[string]any{
		"processId":    nil,
		"rootUri":      pathToFileURI(root),
		"capabilities": map[string]any{},
	})
	notifyConfiguration(t, client, map[string]any{"aspLsp": aspLspSettings})
	notifyOpenClassicASPDocument(t, client, uri, source)
	return firstEditText(textEdits(t, client.request("textDocument/formatting", map[string]any{
		"textDocument": map[string]any{"uri": uri},
		"options":      map[string]any{"tabSize": 2, "insertSpaces": true},
	}).Result), source)
}
