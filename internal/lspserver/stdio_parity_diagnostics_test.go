package lspserver

import (
	"fmt"
	"os"
	"path/filepath"
	"reflect"
	"strings"
	"testing"
	"time"

	"github.com/yottonoko/classic-web-system-lsp/internal/lsp"
)

func TestStdioParityPublishesCSSAndJavaScriptDiagnosticsOverJSONRPC(t *testing.T) {
	client := startStdioTestClient(t)
	defer client.close()

	root := t.TempDir()
	uri := pathToFileURI(filepath.Join(root, "go-diagnostics.asp"))
	source := `<style>.x { color: }</style>
<script>const = ;</script>`
	client.request("initialize", map[string]any{
		"processId":    nil,
		"rootUri":      pathToFileURI(root),
		"capabilities": map[string]any{},
	})
	notifyOpenClassicASPDocument(t, client, uri, source)
	diagnostics := waitForDiagnosticsContaining(t, client, "asp-lsp-css")
	serialized := string(diagnostics.Params)
	if !strings.Contains(serialized, "asp-lsp-css") || !strings.Contains(serialized, "asp-lsp-typescript") {
		t.Fatalf("published diagnostics missing CSS or JavaScript source: %s", serialized)
	}

	pulled := client.request("textDocument/diagnostic", map[string]any{
		"textDocument": map[string]any{"uri": uri},
	})
	if !strings.Contains(mustJSONText(t, pulled.Result), "asp-lsp-typescript") {
		t.Fatalf("pulled diagnostics missing JavaScript source: %s", mustJSONText(t, pulled.Result))
	}
}

func TestStdioParitySerializesEmptyDiagnosticCollectionsAsArrays(t *testing.T) {
	client := startStdioTestClient(t)
	defer client.close()

	root := t.TempDir()
	uri := pathToFileURI(filepath.Join(root, "empty-diagnostics.asp"))
	client.request("initialize", map[string]any{
		"processId":    nil,
		"rootUri":      pathToFileURI(root),
		"capabilities": map[string]any{},
	})
	notifyOpenClassicASPDocument(t, client, uri, "<% Option Explicit %>")

	published := client.waitForNotification("textDocument/publishDiagnostics", uri)
	publishedText := string(published.Params)
	if !strings.Contains(publishedText, `"diagnostics":[]`) || strings.Contains(publishedText, `"diagnostics":null`) {
		t.Fatalf("published empty diagnostics must be an array: %s", publishedText)
	}

	pulled := client.request("textDocument/diagnostic", map[string]any{
		"textDocument": map[string]any{"uri": uri},
	})
	pulledText := mustJSONText(t, pulled.Result)
	if !strings.Contains(pulledText, `"items":[]`) || strings.Contains(pulledText, `"items":null`) {
		t.Fatalf("pulled empty diagnostics must be an array: %s", pulledText)
	}

	for requestNumber := 1; requestNumber <= 2; requestNumber++ {
		workspace := client.request("workspace/diagnostic", map[string]any{})
		workspaceText := mustJSONText(t, workspace.Result)
		if !strings.Contains(workspaceText, `"items":[]`) || strings.Contains(workspaceText, `"items":null`) {
			t.Fatalf("workspace empty diagnostics request %d must contain arrays: %s", requestNumber, workspaceText)
		}
	}
}

func TestStdioParityKeepsASPDelimitersInsideCSSAndJavaScriptFromProducingEmbeddedDiagnostics(t *testing.T) {
	client := startStdioTestClient(t)
	defer client.close()

	root := t.TempDir()
	uri := pathToFileURI(filepath.Join(root, "go-embedded-asp-islands.asp"))
	source := `<style>
.card-<%= className %> { color: <%= themeColor %>; width: <% Response.Write width %>px; background: red; }
</style>
<div style="color: <%= themeColor %>; background: red"></div>
<input title='<%= Response.Write("x") %>'>
<script>
const clientValue = <%= serverValue %>;
const label = "<%= serverLabel %>";
const fromServer = <% Response.Write clientValue %>;
function markTier(tier) { return tier; }
markTier("gold");
console.log(label, fromServer, clientValue, document.querySelector(".card"));
</script>`
	client.request("initialize", map[string]any{
		"processId":    nil,
		"rootUri":      pathToFileURI(root),
		"capabilities": map[string]any{},
	})
	notifyOpenClassicASPDocument(t, client, uri, source)

	published := client.waitForNotification("textDocument/publishDiagnostics", uri)
	publishedText := string(published.Params)
	for _, unexpected := range []string{"asp-lsp-css", "asp-lsp-typescript", "className", "themeColor", "serverValue", "serverLabel"} {
		if strings.Contains(publishedText, unexpected) {
			t.Fatalf("published diagnostics leaked ASP island %q: %s", unexpected, publishedText)
		}
	}

	pulled := mustJSONText(t, client.request("textDocument/diagnostic", map[string]any{
		"textDocument": map[string]any{"uri": uri},
	}).Result)
	for _, unexpected := range []string{"asp-lsp-css", "asp-lsp-typescript", "className", "themeColor", "serverValue", "serverLabel"} {
		if strings.Contains(pulled, unexpected) {
			t.Fatalf("pulled diagnostics leaked ASP island %q: %s", unexpected, pulled)
		}
	}
}

func TestStdioParityIsolatesDirtyVBScriptEditsFromIncludeAwareDiagnosticsAndSemanticTokenCache(t *testing.T) {
	client := startStdioTestClient(t)
	defer client.close()

	root := t.TempDir()
	includesDir := filepath.Join(root, "includes")
	if err := os.Mkdir(includesDir, 0o755); err != nil {
		t.Fatal(err)
	}
	if err := os.WriteFile(filepath.Join(includesDir, "data.inc"), []byte(`<%
Function ReadDashboardFilter()
  Set ReadDashboardFilter = Server.CreateObject("Scripting.Dictionary")
End Function

Function BuildCustomerFixtures()
  BuildCustomerFixtures = Array("northwind")
End Function

Function FilterCustomers(ByVal customers, ByVal dashboardFilter)
  FilterCustomers = customers
End Function

Function BuildMetrics(ByVal filteredCustomers)
  Set BuildMetrics = Server.CreateObject("Scripting.Dictionary")
End Function
%>`), 0o644); err != nil {
		t.Fatal(err)
	}
	source := `<!-- #include file="includes/data.inc" -->
<%
Option Explicit

Dim filter
Set filter = ReadDashboardFilter()

Dim customers
customers = BuildCustomerFixtures()

Dim filteredCustomers
filteredCustomers = FilterCustomers(customers, filter)

Dim metrics
Set metrics = BuildMetrics(filteredCustomers)

Dim selectedCustomerId
selectedCustomerId = Request.QueryString("customer")
%>
<div data-customer="<%= selectedCustomerId %>"></div>
<script>
const clientToken = document.querySelector("[data-customer]");
</script>`
	owner := filepath.Join(root, "default.asp")
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
		"debug":       map[string]any{"output": "verbose"},
		"diagnostics": map[string]any{"debounceMs": 0},
	}})
	if err := client.notify("textDocument/didOpen", map[string]any{
		"textDocument": map[string]any{
			"uri":        uri,
			"languageId": "classic-asp",
			"version":    1,
			"text":       source,
		},
	}); err != nil {
		t.Fatal(err)
	}
	client.waitForNotification("window/logMessage", "LSP check completed")
	initialFull := client.request("textDocument/semanticTokens/full", map[string]any{
		"textDocument": map[string]any{"uri": uri},
	})

	source = notifyNeedleReplacement(t, client, uri, source, 2, "BuildCustomerFixtures(", "BuildCustomerFixtures( ")
	client.waitForNotification("window/logMessage", "LSP check completed")

	diagnostics := client.request("textDocument/diagnostic", map[string]any{
		"textDocument": map[string]any{"uri": uri},
	})
	diagnosticText := mustJSONText(t, diagnostics.Result)
	for _, name := range []string{"customers", "filteredCustomers", "metrics", "selectedCustomerId"} {
		english := fmt.Sprintf("'%s' is not declared under Option Explicit.", name)
		japanese := fmt.Sprintf("'%s' は Option Explicit のもとで宣言されていません。", name)
		if strings.Contains(diagnosticText, english) || strings.Contains(diagnosticText, japanese) {
			t.Fatalf("dirty edit diagnostics leaked undeclared %s: %s", name, diagnosticText)
		}
	}

	delta := client.request("textDocument/semanticTokens/full/delta", map[string]any{
		"textDocument":     map[string]any{"uri": uri},
		"previousResultId": semanticResultID(t, initialFull.Result),
	})
	applied := applySemanticTokenDeltaEdits(t, semanticTokenData(t, initialFull.Result), delta.Result)
	fresh := client.request("textDocument/semanticTokens/full", map[string]any{
		"textDocument": map[string]any{"uri": uri},
	})
	if !reflect.DeepEqual(applied, semanticTokenData(t, fresh.Result)) {
		t.Fatalf("semantic token delta does not match fresh full tokens\napplied=%v\nfresh=%v", applied, semanticTokenData(t, fresh.Result))
	}
	decoded := decodeSemanticTokenData(applied)
	if !hasTokenMatchingText(source, decoded, "querySelector", semanticTokenMethod) {
		t.Fatalf("semantic tokens missing querySelector method token: %#v", decoded)
	}
	if !hasTokenMatchingText(source, decoded, "selectedCustomerId", semanticTokenVariable) {
		t.Fatalf("semantic tokens missing selectedCustomerId variable token: %#v", decoded)
	}
}

func TestStdioParityDebouncesDiagnosticsAfterRapidTextChangesAndPublishesOnlyLatestVersion(t *testing.T) {
	client := startStdioTestClient(t)
	defer client.close()

	root := t.TempDir()
	uri := pathToFileURI(filepath.Join(root, "go-debounced-diagnostics.asp"))
	client.request("initialize", map[string]any{
		"processId":    nil,
		"rootUri":      pathToFileURI(root),
		"capabilities": map[string]any{},
	})
	notifyConfiguration(t, client, map[string]any{"aspLsp": map[string]any{"diagnostics": map[string]any{"debounceMs": 80}}})
	if err := client.notify("textDocument/didOpen", map[string]any{
		"textDocument": map[string]any{
			"uri":        uri,
			"languageId": "classic-asp",
			"version":    1,
			"text": `<% Option Explicit
Dim known
Response.Write known
%>`,
		},
	}); err != nil {
		t.Fatal(err)
	}
	client.waitForNotification("textDocument/publishDiagnostics", "")

	if err := client.notify("textDocument/didChange", map[string]any{
		"textDocument":   map[string]any{"uri": uri, "version": 2},
		"contentChanges": []map[string]any{{"text": "<% Option Explicit\nResponse.Write staleName\n%>"}},
	}); err != nil {
		t.Fatal(err)
	}
	if err := client.notify("textDocument/didChange", map[string]any{
		"textDocument":   map[string]any{"uri": uri, "version": 3},
		"contentChanges": []map[string]any{{"text": "<% Option Explicit\nResponse.Write finalName\n%>"}},
	}); err != nil {
		t.Fatal(err)
	}

	diagnostics := waitForDiagnosticsContaining(t, client, "finalName")
	serialized := string(diagnostics.Params)
	if !strings.Contains(serialized, "finalName") || strings.Contains(serialized, "staleName") {
		t.Fatalf("debounced diagnostics mismatch: %s", serialized)
	}
}

func TestStdioParityCoalescesRapidDiagnosticsAndSkipsEmptyFastPublication(t *testing.T) {
	client := startStdioTestClient(t)
	defer client.close()

	root := t.TempDir()
	uri := pathToFileURI(filepath.Join(root, "go-debounced-analysis.asp"))
	client.request("initialize", map[string]any{
		"processId":    nil,
		"rootUri":      pathToFileURI(root),
		"capabilities": map[string]any{},
	})
	notifyConfiguration(t, client, map[string]any{"aspLsp": map[string]any{
		"debug":       map[string]any{"output": "verbose"},
		"diagnostics": map[string]any{"debounceMs": 80},
	}})
	if err := client.notify("textDocument/didOpen", map[string]any{
		"textDocument": map[string]any{
			"uri":        uri,
			"languageId": "classic-asp",
			"version":    1,
			"text": `<% Option Explicit
Dim known
Response.Write known
%>`,
		},
	}); err != nil {
		t.Fatal(err)
	}
	client.waitForNotification("textDocument/publishDiagnostics", "")
	client.waitForNotification("window/logMessage", "LSP check completed")
	client.drainNotifications("window/logMessage")

	if err := client.notify("textDocument/didChange", map[string]any{
		"textDocument":   map[string]any{"uri": uri, "version": 2},
		"contentChanges": []map[string]any{{"text": "<% Option Explicit\nResponse.Write staleName\n%>"}},
	}); err != nil {
		t.Fatal(err)
	}
	if err := client.notify("textDocument/didChange", map[string]any{
		"textDocument":   map[string]any{"uri": uri, "version": 3},
		"contentChanges": []map[string]any{{"text": "<% Option Explicit\nResponse.Write finalName\n%>"}},
	}); err != nil {
		t.Fatal(err)
	}

	_, seen := client.waitForNotificationWithSeen("window/logMessage", "LSP check completed")
	serializedNotifications := mustJSONText(t, seen)
	if !strings.Contains(serializedNotifications, "finalName") || strings.Contains(serializedNotifications, "staleName") {
		t.Fatalf("debounced diagnostics notifications mismatch: %s", serializedNotifications)
	}
	if got := countOccurrences(serializedNotifications, "LSP analysis started"); got != 2 {
		t.Fatalf("LSP analysis started count = %d, want 2: %s", got, serializedNotifications)
	}
	if !strings.Contains(serializedNotifications, "LSP analysis completed") {
		t.Fatalf("missing LSP analysis completed log: %s", serializedNotifications)
	}
	if got := countOccurrences(serializedNotifications, "diagnostics.fast.published"); got != 0 {
		t.Fatalf("empty fast diagnostics published %d times: %s", got, serializedNotifications)
	}
	if got := countOccurrences(serializedNotifications, "diagnostics.final.published"); got != 1 {
		t.Fatalf("diagnostics.final.published count = %d, want 1: %s", got, serializedNotifications)
	}
	if got := countOccurrences(serializedNotifications, "LSP check completed"); got != 1 {
		t.Fatalf("LSP check completed count = %d, want 1: %s", got, serializedNotifications)
	}
}

func TestStdioParityRefreshesPendingChangesBeforeHoverDuringDiagnosticDebounce(t *testing.T) {
	client := startStdioTestClient(t)
	defer client.close()

	root := t.TempDir()
	uri := pathToFileURI(filepath.Join(root, "go-hover-before-debounce.asp"))
	source := `<% Response.Write CStr(1) %>`
	client.request("initialize", map[string]any{
		"processId":    nil,
		"rootUri":      pathToFileURI(root),
		"capabilities": map[string]any{},
	})
	notifyConfiguration(t, client, map[string]any{"aspLsp": map[string]any{"diagnostics": map[string]any{"debounceMs": 1000}}})
	openClassicASPDocument(t, client, uri, source)

	source = strings.Replace(source, "CStr", "Date", 1)
	if err := client.notify("textDocument/didChange", map[string]any{
		"textDocument":   map[string]any{"uri": uri, "version": 2},
		"contentChanges": []map[string]any{{"text": source}},
	}); err != nil {
		t.Fatal(err)
	}
	hover := client.request("textDocument/hover", map[string]any{
		"textDocument": map[string]any{"uri": uri},
		"position":     positionAt(source, strings.Index(source, "Date")),
	})
	serialized := mustJSONText(t, hover.Result)
	if !strings.Contains(serialized, "Date()") || strings.Contains(serialized, "CStr(value)") {
		t.Fatalf("hover did not refresh pending document change: %s", serialized)
	}
}

func TestStdioParityPublishesDiagnosticsImmediatelyWhenDebounceIsDisabled(t *testing.T) {
	client := startStdioTestClient(t)
	defer client.close()

	root := t.TempDir()
	uri := pathToFileURI(filepath.Join(root, "go-immediate-diagnostics.asp"))
	client.request("initialize", map[string]any{
		"processId":    nil,
		"rootUri":      pathToFileURI(root),
		"capabilities": map[string]any{},
	})
	notifyConfiguration(t, client, map[string]any{"aspLsp": map[string]any{"diagnostics": map[string]any{"debounceMs": 0}}})
	openClassicASPDocument(t, client, uri, `<% Option Explicit
Dim known
Response.Write known
%>`)

	if err := client.notify("textDocument/didChange", map[string]any{
		"textDocument":   map[string]any{"uri": uri, "version": 2},
		"contentChanges": []map[string]any{{"text": "<% Option Explicit\nResponse.Write immediateName\n%>"}},
	}); err != nil {
		t.Fatal(err)
	}
	diagnostics := waitForDiagnosticsContaining(t, client, "immediateName")
	if !strings.Contains(string(diagnostics.Params), "immediateName") {
		t.Fatalf("immediate diagnostics missing immediateName: %s", diagnostics.Params)
	}
}

func TestStdioParityLogsDetailedDocumentChangeTimingStepsInVerboseDebugOutput(t *testing.T) {
	client := startStdioTestClient(t)
	defer client.close()

	root := t.TempDir()
	uri := pathToFileURI(filepath.Join(root, "go-document-change-timing.asp"))
	source := `<% Option Explicit
' benchmark x
Dim known
Response.Write known
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
	if err := client.notify("textDocument/didOpen", map[string]any{
		"textDocument": map[string]any{
			"uri":        uri,
			"languageId": "classic-asp",
			"version":    1,
			"text":       source,
		},
	}); err != nil {
		t.Fatal(err)
	}
	client.waitForNotification("textDocument/publishDiagnostics", "")
	client.waitForNotification("window/logMessage", "LSP check completed")
	client.drainNotifications("window/logMessage")

	oldText := "Response.Write known"
	newText := "Response.Write known2"
	startOffset := strings.Index(source, oldText)
	if startOffset < 0 {
		t.Fatalf("missing %q in source", oldText)
	}
	oldSource := source
	endOffset := startOffset + len(oldText)
	source = source[:startOffset] + newText + source[endOffset:]
	if err := client.notify("textDocument/didChange", map[string]any{
		"textDocument": map[string]any{"uri": uri, "version": 2},
		"contentChanges": []map[string]any{{
			"range": map[string]any{
				"start": positionAt(oldSource, startOffset),
				"end":   positionAt(oldSource, endOffset),
			},
			"text": newText,
		}},
	}); err != nil {
		t.Fatal(err)
	}

	_, seen := client.waitForNotificationWithSeen("window/logMessage", "LSP check completed")
	serialized := mustJSONText(t, seen)
	for _, expected := range []string{
		"LSP analysis completed",
		"analysis.parse.incremental",
		"documentChange.scheduleDiagnostics",
		"check.parser",
		"check.vbscript.syntax",
		"LSP check completed",
	} {
		if !strings.Contains(serialized, expected) {
			t.Fatalf("document change timing logs missing %q: %s", expected, serialized)
		}
	}
	if !strings.Contains(source, "Response.Write known2") {
		t.Fatalf("source was not updated: %s", source)
	}
}

func TestStdioParityEmitsVerboseLSPTimingBreakdownsWhenDebugOutputIsVerbose(t *testing.T) {
	client := startStdioTestClient(t)
	defer client.close()

	root := t.TempDir()
	uri := pathToFileURI(filepath.Join(root, "go-debug-verbose.asp"))
	source := `<html>
<head>
<style>.x{color:red}</style>
<script>
const value = 1;
</script>
</head>
<body>
<% Option Explicit
Dim enabled
enabled = True
Response.Write enabled
%>
</body>
</html>`
	client.request("initialize", map[string]any{
		"processId":    nil,
		"rootUri":      pathToFileURI(root),
		"capabilities": map[string]any{},
	})
	notifyConfiguration(t, client, map[string]any{"aspLsp": map[string]any{"debug": map[string]any{"output": "verbose"}}})
	if err := client.notify("textDocument/didOpen", map[string]any{
		"textDocument": map[string]any{
			"uri":        uri,
			"languageId": "classic-asp",
			"version":    1,
			"text":       source,
		},
	}); err != nil {
		t.Fatal(err)
	}
	lastLog, seen := client.waitForNotificationWithSeen("window/logMessage", "check.diagnostics in ")
	logs := mustJSONText(t, seen)
	for _, expected := range []string{
		"analysis.parse",
		"analysis.parse.started",
		"analysisDatabase.diagnostics.miss",
		"check.css",
		"check.javascript",
		"check.javascript.started",
		"check.vbscript.syntax",
		"check.vbscript.types",
		"check.vbscript.unused",
		"check.diagnostics",
	} {
		if !strings.Contains(logs, expected) {
			t.Fatalf("verbose timing logs missing %q: %s", expected, logs)
		}
	}
	javascriptStarted := strings.Index(logs, "check.javascript.started")
	javascriptCompleted := strings.LastIndex(logs, "check.javascript in ")
	if javascriptStarted < 0 || javascriptCompleted <= javascriptStarted {
		t.Fatalf("JavaScript check lifecycle logs are out of order: %s", logs)
	}
	expectElapsedLogWithoutHeat(t, lastLog)

	formatting := client.request("textDocument/formatting", map[string]any{
		"textDocument": map[string]any{"uri": uri},
		"options":      map[string]any{"tabSize": 2, "insertSpaces": true},
	})
	if formatting.Result == nil {
		t.Fatalf("formatting returned nil result")
	}
	expectElapsedLogWithoutHeat(t, client.waitForLogContaining("format.embedded"))
}

func TestStdioParityLogsSingleDiagnosticsCheckInClassicASPDashboardSmokeScenario(t *testing.T) {
	client := startStdioTestClient(t)
	defer client.close()

	root := repoRoot(t)
	samplePath := filepath.Join(root, "samples", "classic-asp-dashboard", "customers.asp")
	source := mustReadText(t, samplePath)
	uri := pathToFileURI(samplePath)
	client.request("initialize", map[string]any{
		"processId":    nil,
		"rootUri":      pathToFileURI(root),
		"capabilities": map[string]any{},
	})
	notifyConfiguration(t, client, map[string]any{"aspLsp": map[string]any{"debug": map[string]any{"output": "verbose"}}})
	if err := client.notify("textDocument/didOpen", map[string]any{
		"textDocument": map[string]any{
			"uri":        uri,
			"languageId": "classic-asp",
			"version":    1,
			"text":       source,
		},
	}); err != nil {
		t.Fatal(err)
	}
	checkLog, seen := client.waitForNotificationWithSeen("window/logMessage", "LSP check completed")
	expectElapsedLogWithoutHeat(t, checkLog)
	if !strings.Contains(mustJSONText(t, seen), "check.javascript") {
		t.Fatalf("dashboard smoke logs missing check.javascript: %s", mustJSONText(t, seen))
	}
}

func TestStdioParityDoesNotEmitLSPTimingLogsWhenDebugOutputIsOff(t *testing.T) {
	client := startStdioTestClient(t)
	defer client.close()

	root := t.TempDir()
	uri := pathToFileURI(filepath.Join(root, "go-debug-off.asp"))
	client.request("initialize", map[string]any{
		"processId":    nil,
		"rootUri":      pathToFileURI(root),
		"capabilities": map[string]any{},
	})
	notifyConfiguration(t, client, map[string]any{"aspLsp": map[string]any{"debug": map[string]any{"output": "off"}}})
	openClassicASPDocument(t, client, uri, `<style>.x{color:red}</style>
<script>const value = 1;</script>
<% Response.Write value %>`)
	client.request("textDocument/formatting", map[string]any{
		"textDocument": map[string]any{"uri": uri},
		"options":      map[string]any{"tabSize": 2, "insertSpaces": true},
	})
	debugLogs := mustJSONText(t, client.drainNotifications("window/logMessage"))
	if debugTimingLogPattern.MatchString(debugLogs) {
		t.Fatalf("debug output off emitted timing logs: %s", debugLogs)
	}
}

func TestStdioParityKeepsParseAndDiagnosticsIdleForFormattingOnlySettingsChanges(t *testing.T) {
	client := startStdioTestClient(t)
	defer client.close()

	root := t.TempDir()
	uri := pathToFileURI(filepath.Join(root, "format-only-settings.asp"))
	client.request("initialize", map[string]any{
		"processId":    nil,
		"rootUri":      pathToFileURI(root),
		"capabilities": map[string]any{},
	})
	notifyConfiguration(t, client, map[string]any{"aspLsp": map[string]any{
		"debug":       map[string]any{"output": "verbose"},
		"diagnostics": map[string]any{"debounceMs": 0},
	}})
	if err := client.notify("textDocument/didOpen", map[string]any{
		"textDocument": map[string]any{
			"uri":        uri,
			"languageId": "classic-asp",
			"version":    1,
			"text":       `<% Response.Write "ok" %>`,
		},
	}); err != nil {
		t.Fatal(err)
	}
	client.waitForLogContaining("LSP check completed")
	client.drainNotifications("window/logMessage")

	notifyConfiguration(t, client, map[string]any{"aspLsp": map[string]any{
		"debug":       map[string]any{"output": "verbose"},
		"diagnostics": map[string]any{"debounceMs": 0},
		"format":      map[string]any{"uppercaseKeywords": true},
	}})
	time.Sleep(350 * time.Millisecond)

	logs := mustJSONText(t, client.drainNotifications("window/logMessage"))
	for _, unexpected := range []string{"analysis.parse", "LSP check started", "invalidation."} {
		if strings.Contains(logs, unexpected) {
			t.Fatalf("format-only settings change emitted %q: %s", unexpected, logs)
		}
	}
}

func TestStdioParityInvalidatesIncludeResolutionSettingsWithoutReparsingOrClearingJavaScriptProjects(t *testing.T) {
	client := startStdioTestClient(t)
	defer client.close()

	root := t.TempDir()
	pageDir := filepath.Join(root, "pages")
	includeDir := filepath.Join(root, "includes")
	if err := os.MkdirAll(pageDir, 0o755); err != nil {
		t.Fatal(err)
	}
	if err := os.MkdirAll(includeDir, 0o755); err != nil {
		t.Fatal(err)
	}
	page := filepath.Join(pageDir, "default.asp")
	include := filepath.Join(includeDir, "shared.inc")
	if err := os.WriteFile(page, []byte("<!-- #include file=\"shared.inc\" -->\n<% Response.Write \"ok\" %>"), 0o644); err != nil {
		t.Fatal(err)
	}
	if err := os.WriteFile(include, []byte(`<% Const SharedValue = "ok" %>`), 0o644); err != nil {
		t.Fatal(err)
	}
	uri := pathToFileURI(page)
	client.request("initialize", map[string]any{
		"processId":    nil,
		"rootUri":      pathToFileURI(root),
		"capabilities": map[string]any{},
	})
	notifyConfiguration(t, client, map[string]any{"aspLsp": map[string]any{
		"debug":       map[string]any{"output": "verbose"},
		"diagnostics": map[string]any{"debounceMs": 0},
	}})
	if err := client.notify("textDocument/didOpen", map[string]any{
		"textDocument": map[string]any{
			"uri":        uri,
			"languageId": "classic-asp",
			"version":    1,
			"text":       mustReadText(t, page),
		},
	}); err != nil {
		t.Fatal(err)
	}
	waitForDiagnosticsContaining(t, client, "shared.inc")
	client.waitForLogContaining("LSP check completed")
	client.drainNotifications("window/logMessage")
	client.drainNotifications("textDocument/publishDiagnostics")

	notifyConfiguration(t, client, map[string]any{"aspLsp": map[string]any{
		"debug":        map[string]any{"output": "verbose"},
		"diagnostics":  map[string]any{"debounceMs": 0},
		"includePaths": []string{includeDir},
	}})
	includeLog := client.waitForLogContaining("invalidation.includeResolution")
	checkLog := client.waitForLogContaining("LSP check completed")
	pulled := client.request("textDocument/diagnostic", map[string]any{
		"textDocument": map[string]any{"uri": uri},
	})
	if strings.Contains(mustJSONText(t, pulled.Result), "shared.inc") {
		t.Fatalf("include diagnostic remained after includePaths update: %s", mustJSONText(t, pulled.Result))
	}
	logs := mustJSONText(t, append([]*rpcMessage{includeLog, checkLog}, client.drainNotifications("window/logMessage")...))
	for _, unexpected := range []string{"analysis.parse.full", "invalidation.jsProject", "invalidation.workspaceIndex"} {
		if strings.Contains(logs, unexpected) {
			t.Fatalf("include resolution update emitted %q: %s", unexpected, logs)
		}
	}
}

func TestStdioParityKeepsDebugLogFileOutputDisabledUnlessExplicitlyEnabled(t *testing.T) {
	client := startStdioTestClient(t)
	defer client.close()

	root := t.TempDir()
	logPath := filepath.Join(root, "asp-lsp-debug.log")
	uri := pathToFileURI(filepath.Join(root, "disabled.asp"))
	client.request("initialize", map[string]any{
		"processId":    nil,
		"rootUri":      pathToFileURI(root),
		"capabilities": map[string]any{},
	})
	notifyConfiguration(t, client, map[string]any{"aspLsp": map[string]any{
		"debug":       map[string]any{"output": "verbose", "logFile": map[string]any{"path": logPath}},
		"diagnostics": map[string]any{"debounceMs": 0},
	}})
	if err := client.notify("textDocument/didOpen", map[string]any{
		"textDocument": map[string]any{
			"uri":        uri,
			"languageId": "classic-asp",
			"version":    1,
			"text":       `<% Response.Write "disabled" %>`,
		},
	}); err != nil {
		t.Fatal(err)
	}
	client.waitForLogContaining("LSP analysis completed")
	time.Sleep(100 * time.Millisecond)
	if _, err := os.Stat(logPath); err == nil {
		t.Fatalf("debug log file was created without explicit enable: %s", logPath)
	} else if !os.IsNotExist(err) {
		t.Fatal(err)
	}
}

func TestStdioParityWritesDebugLogAndTraceEntriesToExplicitFileIndependentlyOfDebugOutput(t *testing.T) {
	client := startStdioTestClient(t)
	defer client.close()

	root := t.TempDir()
	logPath := filepath.Join(root, "asp-lsp-debug.log")
	uri := pathToFileURI(filepath.Join(root, "explicit.asp"))
	client.request("initialize", map[string]any{
		"processId":    nil,
		"rootUri":      pathToFileURI(root),
		"capabilities": map[string]any{},
	})
	notifyConfiguration(t, client, map[string]any{"aspLsp": map[string]any{
		"debug":       map[string]any{"output": "off", "logFile": map[string]any{"enabled": true, "path": logPath}},
		"diagnostics": map[string]any{"debounceMs": 0},
	}})
	waitForFileContaining(t, logPath, "configuration.changed")
	if err := client.notify("textDocument/didOpen", map[string]any{
		"textDocument": map[string]any{
			"uri":        uri,
			"languageId": "classic-asp",
			"version":    1,
			"text":       `<% Response.Write "explicit" %>`,
		},
	}); err != nil {
		t.Fatal(err)
	}
	client.waitForNotification("textDocument/publishDiagnostics", "")
	logText := waitForFileContaining(t, logPath, "diagnostics.start")
	for _, expected := range []string{"DEBUG debug.summary", "LSP analysis started", "TRACE document.open", "TRACE diagnostics.start"} {
		if !strings.Contains(logText, expected) {
			t.Fatalf("debug log missing %q: %s", expected, logText)
		}
	}
}

func TestStdioParityUsesDefaultDebugLogPathAndRotatesLogsByFileSize(t *testing.T) {
	client := startStdioTestClient(t)
	defer client.close()

	root := t.TempDir()
	logPath := filepath.Join(root, "default-debug.log")
	uri := pathToFileURI(filepath.Join(root, "rotate.asp"))
	t.Setenv("ASP_LSP_DEFAULT_DEBUG_LOG_FILE", logPath)
	t.Setenv("ASP_LSP_TEST_DEBUG_LOG_MAX_BYTES", "700")
	t.Setenv("ASP_LSP_TEST_DEBUG_LOG_MAX_BACKUPS", "2")
	client.request("initialize", map[string]any{
		"processId":    nil,
		"rootUri":      pathToFileURI(root),
		"capabilities": map[string]any{},
	})
	notifyConfiguration(t, client, map[string]any{"aspLsp": map[string]any{
		"debug":       map[string]any{"output": "off", "logFile": map[string]any{"enabled": true, "path": ""}},
		"diagnostics": map[string]any{"debounceMs": 0},
	}})
	waitForFileContaining(t, logPath, "configuration.changed")
	source := `<% Response.Write "rotate-0" %>`
	if err := client.notify("textDocument/didOpen", map[string]any{
		"textDocument": map[string]any{"uri": uri, "languageId": "classic-asp", "version": 1, "text": source},
	}); err != nil {
		t.Fatal(err)
	}
	client.waitForNotification("textDocument/publishDiagnostics", "")
	for version := 2; version <= 8; version++ {
		source = fmt.Sprintf(`<%% Response.Write "rotate-%d" %%>`, version)
		if err := client.notify("textDocument/didChange", map[string]any{
			"textDocument":   map[string]any{"uri": uri, "version": version},
			"contentChanges": []map[string]any{{"text": source}},
		}); err != nil {
			t.Fatal(err)
		}
		client.waitForNotification("textDocument/publishDiagnostics", "")
	}
	waitForPathExists(t, logPath+".1")
	waitForPathExists(t, logPath)
	if _, err := os.Stat(logPath + ".3"); err == nil {
		t.Fatalf("unexpected third rotated debug log exists: %s", logPath+".3")
	} else if !os.IsNotExist(err) {
		t.Fatal(err)
	}
}

func TestStdioParityWritesConfigurationLogsToDebugLogFile(t *testing.T) {
	client := startStdioTestClient(t)
	defer client.close()

	root := t.TempDir()
	logPath := filepath.Join(root, "asp-lsp-debug.log")
	if err := os.WriteFile(filepath.Join(root, "first.asp"), []byte("<% Function FirstTitle()\nEnd Function %>"), 0o644); err != nil {
		t.Fatal(err)
	}
	if err := os.WriteFile(filepath.Join(root, "second.asp"), []byte("<% Function SecondTitle()\nEnd Function %>"), 0o644); err != nil {
		t.Fatal(err)
	}
	initializeAndWaitForWorkspaceIndex(t, client, map[string]any{
		"processId":    nil,
		"rootUri":      pathToFileURI(root),
		"capabilities": map[string]any{},
	})
	notifyConfiguration(t, client, map[string]any{"aspLsp": map[string]any{
		"debug":     map[string]any{"output": "off", "logFile": map[string]any{"enabled": true, "path": logPath}},
		"workspace": map[string]any{"scanChunkSize": 1},
	}})
	waitForWorkspaceIndexRefresh(t, client)
	waitForFileContaining(t, logPath, "configuration.changed")
	logText := waitForFileContaining(t, logPath, "configuration.changed")
	if !strings.Contains(logText, "configuration.changed") {
		t.Fatalf("debug log missing configuration event: %s", logText)
	}
}

func TestStdioParityFallsBackToSkeletonParsingForBoundarySensitiveDocumentEdits(t *testing.T) {
	client := startStdioTestClient(t)
	defer client.close()

	root := t.TempDir()
	uri := pathToFileURI(filepath.Join(root, "go-boundary-edit-fallback.asp"))
	source := `<div>safe</div>`
	client.request("initialize", map[string]any{
		"processId":    nil,
		"rootUri":      pathToFileURI(root),
		"capabilities": map[string]any{},
	})
	notifyConfiguration(t, client, verboseImmediateDiagnosticsSettings())
	openClassicASPDocument(t, client, uri, source)
	client.waitForLogContaining("LSP check completed")
	client.drainNotifications("window/logMessage")

	source = notifyNeedleReplacement(t, client, uri, source, 2, "safe", "safe <%")
	_, seen := client.waitForNotificationWithSeen("window/logMessage", "analysis.parse.impact")
	logs := mustJSONText(t, seen)
	for _, expected := range []string{"analysis.parse.skeleton", "analysis.parse.impact", "mode=full", "incremental resync failed"} {
		if !strings.Contains(logs, expected) {
			t.Fatalf("boundary edit logs missing %q: %s", expected, logs)
		}
	}
	if !strings.Contains(source, "safe <%") {
		t.Fatalf("source was not updated: %s", source)
	}
}

func TestStdioParityDoesNotTreatFullDocumentReplacementsAsIncrementalEdits(t *testing.T) {
	client := startStdioTestClient(t)
	defer client.close()

	root := t.TempDir()
	uri := pathToFileURI(filepath.Join(root, "go-full-replacement-fallback.asp"))
	source := `<div>safe</div>`
	client.request("initialize", map[string]any{
		"processId":    nil,
		"rootUri":      pathToFileURI(root),
		"capabilities": map[string]any{},
	})
	notifyConfiguration(t, client, verboseImmediateDiagnosticsSettings())
	openClassicASPDocument(t, client, uri, source)
	client.waitForLogContaining("LSP check completed")
	client.drainNotifications("window/logMessage")

	if err := client.notify("textDocument/didChange", map[string]any{
		"textDocument":   map[string]any{"uri": uri, "version": 2},
		"contentChanges": []map[string]any{{"text": `<div>changed</div>`}},
	}); err != nil {
		t.Fatal(err)
	}
	_, seen := client.waitForNotificationWithSeen("window/logMessage", "analysis.parse.impact")
	logs := mustJSONText(t, seen)
	for _, expected := range []string{"analysis.parse.skeleton", "analysis.parse.impact", "full document replacement"} {
		if !strings.Contains(logs, expected) {
			t.Fatalf("full replacement logs missing %q: %s", expected, logs)
		}
	}
	if strings.Contains(logs, "analysis.parse.incremental") {
		t.Fatalf("full replacement was logged as incremental: %s", logs)
	}
}

func TestStdioParityReusesVBScriptDiagnosticsAfterOrdinaryVBScriptCommentEdits(t *testing.T) {
	client := startStdioTestClient(t)
	defer client.close()

	root := t.TempDir()
	uri := pathToFileURI(filepath.Join(root, "go-vb-reuse-after-comment-edit.asp"))
	source := `<% Option Explicit
' benchmark x
Dim known
Response.Write missingName
%>`
	client.request("initialize", map[string]any{
		"processId":    nil,
		"rootUri":      pathToFileURI(root),
		"capabilities": map[string]any{},
	})
	notifyConfiguration(t, client, verboseImmediateDiagnosticsSettings())
	notifyOpenClassicASPDocument(t, client, uri, source)
	waitForDiagnosticsContaining(t, client, "missingName")
	client.waitForLogContaining("LSP check completed")
	client.drainNotifications("window/logMessage")
	client.drainNotifications("textDocument/publishDiagnostics")

	source = notifyNeedleReplacement(t, client, uri, source, 2, "benchmark x", "benchmark y")
	_, seen := client.waitForNotificationWithSeen("window/logMessage", "LSP check completed")
	logs := mustJSONText(t, seen)
	if findNotificationContaining(seen, "textDocument/publishDiagnostics", "missingName") == nil {
		t.Fatalf("reuse edit did not republish missingName diagnostics: %s", logs)
	}
	for _, expected := range []string{"analysis.vbscript.reuse", "check.vbscript.diagnostics.reuse"} {
		if !strings.Contains(logs, expected) {
			t.Fatalf("VBScript comment reuse logs missing %q: %s", expected, logs)
		}
	}
	for _, unexpected := range []string{"analysis.vbscript.hydrate", "check.vbscript.diagnostics.symbols", "check.vbscript.projectContext", "projectUpdate.scheduled"} {
		if strings.Contains(logs, unexpected) {
			t.Fatalf("VBScript comment reuse logs included %q: %s", unexpected, logs)
		}
	}
	if !strings.Contains(source, "' benchmark y") {
		t.Fatalf("source was not updated: %s", source)
	}
}

func TestStdioParityReusesAndShiftsHTMLAndCSSDiagnosticsAfterVBScriptEdits(t *testing.T) {
	client := startStdioTestClient(t)
	defer client.close()

	root := t.TempDir()
	uri := pathToFileURI(filepath.Join(root, "go-embedded-diagnostics-reuse-after-vb-edit.asp"))
	source := `<%
Dim value
Response.Write value
%>
<style>
.broken { color: }
</style>`
	client.request("initialize", map[string]any{
		"processId":    nil,
		"rootUri":      pathToFileURI(root),
		"capabilities": map[string]any{},
	})
	notifyConfiguration(t, client, verboseImmediateDiagnosticsSettings())
	notifyOpenClassicASPDocument(t, client, uri, source)
	firstDiagnostics := waitForDiagnosticsContaining(t, client, "asp-lsp-css")
	client.waitForLogContaining("LSP check completed")
	if firstCSS := diagnosticFromSource(t, firstDiagnostics, "asp-lsp-css"); firstCSS == nil || firstCSS.Range.Start.Line != 5 {
		t.Fatalf("initial CSS diagnostic = %#v, want line 5", firstCSS)
	}
	client.drainNotifications("window/logMessage")
	client.drainNotifications("textDocument/publishDiagnostics")

	notifyNeedleReplacement(t, client, uri, source, 2, "Dim value", "Dim value\nDim nextValue")
	_, seen := client.waitForNotificationWithSeen("window/logMessage", "LSP check completed")
	logs := mustJSONText(t, seen)
	nextDiagnostics := findNotificationContaining(seen, "textDocument/publishDiagnostics", "asp-lsp-css")
	if nextDiagnostics == nil {
		t.Fatalf("missing shifted CSS diagnostics: %s", logs)
	}
	if nextCSS := diagnosticFromSource(t, nextDiagnostics, "asp-lsp-css"); nextCSS == nil || nextCSS.Range.Start.Line != 6 {
		t.Fatalf("shifted CSS diagnostic = %#v, want line 6", nextCSS)
	}
	for _, expected := range []string{"htmlDiagnostics.reuse", "cssDiagnostics.reuse"} {
		if !strings.Contains(logs, expected) {
			t.Fatalf("embedded reuse logs missing %q: %s", expected, logs)
		}
	}
	if strings.Contains(logs, "css.context.create") {
		t.Fatalf("embedded reuse logs recreated CSS context: %s", logs)
	}
}

func TestStdioParityReusesAndShiftsIncludeDiagnosticsAfterSafeIncrementalEdits(t *testing.T) {
	client := startStdioTestClient(t)
	defer client.close()

	root := t.TempDir()
	uri := pathToFileURI(filepath.Join(root, "go-include-reuse-after-html-edit.asp"))
	source := `<div>top</div>
<!--#include file="missing.inc"-->
<% Response.Write "ok" %>`
	client.request("initialize", map[string]any{
		"processId":    nil,
		"rootUri":      pathToFileURI(root),
		"capabilities": map[string]any{},
	})
	notifyConfiguration(t, client, verboseImmediateDiagnosticsSettings())
	notifyOpenClassicASPDocument(t, client, uri, source)
	firstDiagnostics := waitForDiagnosticsContaining(t, client, "missing.inc")
	client.waitForLogContaining("LSP check completed")
	if firstMissing := diagnosticContaining(t, firstDiagnostics, "missing.inc"); firstMissing == nil || firstMissing.Range.Start.Line != 1 {
		t.Fatalf("initial include diagnostic = %#v, want line 1", firstMissing)
	}
	client.drainNotifications("window/logMessage")
	client.drainNotifications("textDocument/publishDiagnostics")

	source = notifyNeedleReplacement(t, client, uri, source, 2, "top", "top\nnext")
	_, seen := client.waitForNotificationWithSeen("window/logMessage", "LSP check completed")
	logs := mustJSONText(t, seen)
	nextDiagnostics := findNotificationContaining(seen, "textDocument/publishDiagnostics", "missing.inc")
	if nextDiagnostics == nil {
		t.Fatalf("missing shifted include diagnostics: %s", logs)
	}
	if nextMissing := diagnosticContaining(t, nextDiagnostics, "missing.inc"); nextMissing == nil || nextMissing.Range.Start.Line != 2 {
		t.Fatalf("shifted include diagnostic = %#v, want line 2", nextMissing)
	}
	if !strings.Contains(logs, uri) {
		t.Fatalf("include diagnostics reuse log missing uri %s: %s", uri, logs)
	}
	if !strings.Contains(source, "top\nnext") {
		t.Fatalf("source was not updated: %s", source)
	}
}

func TestStdioParityReusesVBScriptDiagnosticsAfterHTMLEditsOutsideVBScriptRegions(t *testing.T) {
	client := startStdioTestClient(t)
	defer client.close()

	root := t.TempDir()
	uri := pathToFileURI(filepath.Join(root, "go-vb-reuse-after-html-edit.asp"))
	source := `<div>top</div>
<%
Option Explicit
Response.Write missingName
%>`
	client.request("initialize", map[string]any{
		"processId":    nil,
		"rootUri":      pathToFileURI(root),
		"capabilities": map[string]any{},
	})
	notifyConfiguration(t, client, verboseImmediateDiagnosticsSettings())
	notifyOpenClassicASPDocument(t, client, uri, source)
	firstDiagnostics := waitForDiagnosticsContaining(t, client, "missingName")
	client.waitForLogContaining("LSP check completed")
	if firstMissing := diagnosticContaining(t, firstDiagnostics, "missingName"); firstMissing == nil || firstMissing.Range.Start.Line != 3 {
		t.Fatalf("initial VBScript diagnostic = %#v, want line 3", firstMissing)
	}
	client.drainNotifications("window/logMessage")
	client.drainNotifications("textDocument/publishDiagnostics")

	source = notifyNeedleReplacement(t, client, uri, source, 2, "top", "top\nnext")
	_, seen := client.waitForNotificationWithSeen("window/logMessage", "LSP check completed")
	logs := mustJSONText(t, seen)
	nextDiagnostics := findNotificationContaining(seen, "textDocument/publishDiagnostics", "missingName")
	if nextDiagnostics == nil {
		t.Fatalf("missing shifted VBScript diagnostics: %s", logs)
	}
	if nextMissing := diagnosticContaining(t, nextDiagnostics, "missingName"); nextMissing == nil || nextMissing.Range.Start.Line != 4 {
		t.Fatalf("shifted VBScript diagnostic = %#v, want line 4", nextMissing)
	}
	for _, expected := range []string{"analysis.vbscript.reuse", "check.vbscript.diagnostics.reuse"} {
		if !strings.Contains(logs, expected) {
			t.Fatalf("HTML edit reuse logs missing %q: %s", expected, logs)
		}
	}
	for _, unexpected := range []string{"check.vbscript.diagnostics.symbols", "check.vbscript.projectContext"} {
		if strings.Contains(logs, unexpected) {
			t.Fatalf("HTML edit reuse logs included %q: %s", unexpected, logs)
		}
	}
	if !strings.Contains(source, "top\nnext") {
		t.Fatalf("source was not updated: %s", source)
	}
}

func TestStdioParityReusesVBScriptDiagnosticsAfterCSSAndClientJavaScriptEdits(t *testing.T) {
	cases := []struct {
		name        string
		source      string
		needle      string
		replacement string
	}{
		{
			name: "css",
			source: `<style>.card { color: red; }</style>
<%
Option Explicit
Response.Write missingName
%>`,
			needle:      "red",
			replacement: "blue",
		},
		{
			name: "client-js",
			source: `<script>const clientValue = 1;</script>
<%
Option Explicit
Response.Write missingName
%>`,
			needle:      "clientValue",
			replacement: "renamedClientValue",
		},
	}
	for _, testCase := range cases {
		t.Run(testCase.name, func(t *testing.T) {
			client := startStdioTestClient(t)
			defer client.close()

			root := t.TempDir()
			uri := pathToFileURI(filepath.Join(root, "go-vb-reuse-after-"+testCase.name+"-edit.asp"))
			source := testCase.source
			client.request("initialize", map[string]any{
				"processId":    nil,
				"rootUri":      pathToFileURI(root),
				"capabilities": map[string]any{},
			})
			notifyConfiguration(t, client, verboseImmediateDiagnosticsSettings())
			notifyOpenClassicASPDocument(t, client, uri, source)
			waitForDiagnosticsContaining(t, client, "missingName")
			client.waitForLogContaining("LSP check completed")
			client.drainNotifications("window/logMessage")
			client.drainNotifications("textDocument/publishDiagnostics")

			source = notifyNeedleReplacement(t, client, uri, source, 2, testCase.needle, testCase.replacement)
			_, seen := client.waitForNotificationWithSeen("window/logMessage", "LSP check completed")
			logs := mustJSONText(t, seen)
			if findNotificationContaining(seen, "textDocument/publishDiagnostics", "missingName") == nil {
				t.Fatalf("missing reused VBScript diagnostics: %s", logs)
			}
			for _, expected := range []string{"analysis.vbscript.reuse", "check.vbscript.diagnostics.reuse"} {
				if !strings.Contains(logs, expected) {
					t.Fatalf("%s edit reuse logs missing %q: %s", testCase.name, expected, logs)
				}
			}
			for _, unexpected := range []string{"check.vbscript.diagnostics.symbols", "check.vbscript.projectContext"} {
				if strings.Contains(logs, unexpected) {
					t.Fatalf("%s edit reuse logs included %q: %s", testCase.name, unexpected, logs)
				}
			}
			if !strings.Contains(source, testCase.replacement) {
				t.Fatalf("source was not updated: %s", source)
			}
		})
	}
}

func TestStdioParityDoesNotScheduleProjectUpdatesForIncrementalClientJavaScriptEdits(t *testing.T) {
	cases := []struct {
		name               string
		checkJS            bool
		source             string
		needle             string
		replacement        string
		expectedDiagnostic string
	}{
		{
			name: "check-js-off",
			source: `<script>
const beforeName = 1;
</script>`,
			needle:      "beforeName",
			replacement: "afterName",
		},
		{
			name:    "check-js-on",
			checkJS: true,
			source: `<script>
missingBefore.toFixed();
</script>`,
			needle:             "missingBefore",
			replacement:        "missingAfter",
			expectedDiagnostic: "missingAfter",
		},
	}
	for _, testCase := range cases {
		t.Run(testCase.name, func(t *testing.T) {
			client := startStdioTestClient(t)
			defer client.close()

			root := t.TempDir()
			uri := pathToFileURI(filepath.Join(root, "go-client-js-no-project-update-"+testCase.name+".asp"))
			source := testCase.source
			client.request("initialize", map[string]any{
				"processId":    nil,
				"rootUri":      pathToFileURI(root),
				"capabilities": map[string]any{},
			})
			notifyConfiguration(t, client, map[string]any{"aspLsp": map[string]any{
				"checkJs":     testCase.checkJS,
				"debug":       map[string]any{"output": "verbose"},
				"diagnostics": map[string]any{"debounceMs": 0},
			}})
			notifyOpenClassicASPDocument(t, client, uri, source)
			client.waitForLogContaining("LSP check completed")
			client.drainNotifications("window/logMessage")
			client.drainNotifications("textDocument/publishDiagnostics")

			source = notifyNeedleReplacement(t, client, uri, source, 2, testCase.needle, testCase.replacement)
			_, seen := client.waitForNotificationWithSeen("window/logMessage", "LSP check completed")
			logs := mustJSONText(t, seen)
			if testCase.expectedDiagnostic != "" && findNotificationContaining(seen, "textDocument/publishDiagnostics", testCase.expectedDiagnostic) == nil {
				t.Fatalf("missing expected JavaScript diagnostic %q: %s", testCase.expectedDiagnostic, logs)
			}
			if !strings.Contains(logs, "documentChange.scheduleDiagnostics.postScheduleProjectUpdate") {
				t.Fatalf("client JS edit logs missing postScheduleProjectUpdate: %s", logs)
			}
			if strings.Contains(logs, "projectUpdate.scheduled") {
				t.Fatalf("client JS edit scheduled project update: %s", logs)
			}
			if !strings.Contains(source, testCase.replacement) {
				t.Fatalf("source was not updated: %s", source)
			}
		})
	}
}

func TestStdioParityReportsJavaScriptUnusedDiagnosticsAsHintsEvenWhenCheckJSIsOff(t *testing.T) {
	client := startStdioTestClient(t)
	defer client.close()

	root := t.TempDir()
	uri := pathToFileURI(filepath.Join(root, "go-js-unused.asp"))
	source := `<script>
function demo(unusedParam) {
  const unusedLocal = 1;
  return 1;
}
</script>`
	client.request("initialize", map[string]any{
		"processId":    nil,
		"rootUri":      pathToFileURI(root),
		"capabilities": map[string]any{},
	})
	diagnosticsMessage := openClassicASPDocumentWithDiagnostics(t, client, uri, source)
	diagnostics := diagnosticsFromPublishMessage(t, diagnosticsMessage)
	serialized := mustJSONText(t, diagnostics)
	for _, expected := range []string{"asp-lsp-typescript-unused", "unusedLocal", `"severity":4`, `"tags":[1]`} {
		if !strings.Contains(serialized, expected) {
			t.Fatalf("JavaScript unused diagnostics missing %q: %s", expected, serialized)
		}
	}
}

func TestStdioParityMapsJavaScriptSemanticDiagnosticsDirectlyToSourceRanges(t *testing.T) {
	client := startStdioTestClient(t)
	defer client.close()

	root := t.TempDir()
	uri := pathToFileURI(filepath.Join(root, "go-js-semantic-range.asp"))
	source := `<div>before</div>
<script>
const fromAsp = <%= Request("id") %>;
missingThing.toFixed();
</script>`
	client.request("initialize", map[string]any{
		"processId":    nil,
		"rootUri":      pathToFileURI(root),
		"capabilities": map[string]any{},
	})
	notifyConfiguration(t, client, map[string]any{"aspLsp": map[string]any{"checkJs": true}})
	diagnosticsMessage := openClassicASPDocumentWithDiagnostics(t, client, uri, source)
	semanticDiagnostics := diagnosticsFromSourceLSP(t, diagnosticsMessage, "asp-lsp-typescript")
	expectDiagnosticRangeLSP(t, semanticDiagnostics, "missingThing", lsp.Range{
		Start: lsp.Position{Line: 3, Character: 0},
		End:   lsp.Position{Line: 3, Character: len("missingThing")},
	})
	expectDiagnosticsOutsideAspIslands(t, source, semanticDiagnostics, []string{"<%= Request"})
}

func TestStdioParityReportsJavaScriptUnusedDiagnosticsAfterMultipleASPIslandLineShifts(t *testing.T) {
	client := startStdioTestClient(t)
	defer client.close()

	root := t.TempDir()
	uri := pathToFileURI(filepath.Join(root, "go-js-unused-many-islands.asp"))
	source := `<script>
const first = <%= ServerFirst %>;
<%
Response.Write ServerBlock
%>
function demoWithIsland(unusedParam) {
  const unusedBetweenIslands = "<%= ServerString %>";
  return first;
}
</script>`
	client.request("initialize", map[string]any{
		"processId":    nil,
		"rootUri":      pathToFileURI(root),
		"capabilities": map[string]any{},
	})
	diagnosticsMessage := openClassicASPDocumentWithDiagnostics(t, client, uri, source)
	unusedDiagnostics := diagnosticsFromSourceLSP(t, diagnosticsMessage, "asp-lsp-typescript-unused")
	expectDiagnosticRangeLSP(t, unusedDiagnostics, "unusedBetweenIslands", lsp.Range{
		Start: lsp.Position{Line: 6, Character: 8},
		End:   lsp.Position{Line: 6, Character: 8 + len("unusedBetweenIslands")},
	})
}

func diagnosticsFromSourceLSP(t *testing.T, message *rpcMessage, source string) []lsp.Diagnostic {
	t.Helper()
	var result []lsp.Diagnostic
	for _, diagnostic := range diagnosticsFromPublishMessage(t, message) {
		if diagnostic.Source == source {
			result = append(result, diagnostic)
		}
	}
	return result
}

func expectDiagnosticRangeLSP(t *testing.T, diagnostics []lsp.Diagnostic, expectedMessage string, expectedRange lsp.Range) {
	t.Helper()
	for _, diagnostic := range diagnostics {
		if strings.Contains(diagnostic.Message, expectedMessage) {
			if !reflect.DeepEqual(diagnostic.Range, expectedRange) {
				t.Fatalf("diagnostic %q range = %#v, want %#v", expectedMessage, diagnostic.Range, expectedRange)
			}
			return
		}
	}
	t.Fatalf("diagnostic %q missing: %s", expectedMessage, mustJSONText(t, diagnostics))
}

func expectDiagnosticsOutsideAspIslands(t *testing.T, source string, diagnostics []lsp.Diagnostic, islandStartNeedles []string) {
	t.Helper()
	type islandRange struct {
		needle string
		start  int
		end    int
	}
	var islands []islandRange
	for _, needle := range islandStartNeedles {
		start := strings.Index(source, needle)
		if start < 0 {
			t.Fatalf("ASP island start %q missing", needle)
		}
		close := strings.Index(source[start:], "%>")
		if close < 0 {
			t.Fatalf("ASP island close %q missing", needle)
		}
		islands = append(islands, islandRange{needle: needle, start: start, end: start + close + len("%>")})
	}
	for _, diagnostic := range diagnostics {
		start := offsetAtLSPPosition(t, source, diagnostic.Range.Start)
		end := offsetAtLSPPosition(t, source, diagnostic.Range.End)
		for _, island := range islands {
			if !(end <= island.start || start >= island.end) {
				t.Fatalf("%s %s overlaps %s: range=%#v island=%#v", diagnostic.Source, diagnostic.Message, island.needle, diagnostic.Range, island)
			}
		}
	}
}

func TestStdioParityPublishesCompleteDiagnosticsOnceForPushDiagnostics(t *testing.T) {
	client := startStdioTestClient(t)
	defer client.close()

	root := t.TempDir()
	uri := pathToFileURI(filepath.Join(root, "go-complete-diagnostics.asp"))
	client.request("initialize", map[string]any{
		"processId":    nil,
		"rootUri":      pathToFileURI(root),
		"capabilities": map[string]any{},
	})
	notifyOpenClassicASPDocument(t, client, uri, `<style>.broken { color: }</style>
<!-- #include file="missing.inc" -->
<% Option Explicit
Response.Write missingName
%>`)
	diagnostics := waitForDiagnosticsContaining(t, client, "missingName")
	text := string(diagnostics.Params)
	for _, expected := range []string{"missing.inc", "asp-lsp-css", "missingName"} {
		if !strings.Contains(text, expected) {
			t.Fatalf("complete diagnostics missing %q: %s", expected, text)
		}
	}
}

func TestStdioParityDoesNotPublishStaleDiagnosticsAfterRapidImmediateChanges(t *testing.T) {
	client := startStdioTestClient(t)
	defer client.close()

	root := t.TempDir()
	uri := pathToFileURI(filepath.Join(root, "go-stale-diagnostics.asp"))
	client.request("initialize", map[string]any{
		"processId":    nil,
		"rootUri":      pathToFileURI(root),
		"capabilities": map[string]any{},
	})
	notifyConfiguration(t, client, map[string]any{"aspLsp": map[string]any{"diagnostics": map[string]any{"debounceMs": 0}}})
	openClassicASPDocument(t, client, uri, `<% Option Explicit
Dim known
Response.Write known
%>`)
	if err := client.notify("textDocument/didChange", map[string]any{
		"textDocument":   map[string]any{"uri": uri, "version": 2},
		"contentChanges": []map[string]any{{"text": "<% Option Explicit\nResponse.Write staleName\n%>"}},
	}); err != nil {
		t.Fatal(err)
	}
	if err := client.notify("textDocument/didChange", map[string]any{
		"textDocument":   map[string]any{"uri": uri, "version": 3},
		"contentChanges": []map[string]any{{"text": "<% Option Explicit\nResponse.Write finalName\n%>"}},
	}); err != nil {
		t.Fatal(err)
	}
	diagnostics := waitForDiagnosticsContaining(t, client, "finalName")
	if strings.Contains(string(diagnostics.Params), "staleName") {
		t.Fatalf("final diagnostics included staleName: %s", diagnostics.Params)
	}
	time.Sleep(30 * time.Millisecond)
	pending := mustJSONText(t, client.drainNotifications("textDocument/publishDiagnostics"))
	if strings.Contains(pending, "staleName") {
		t.Fatalf("pending diagnostics included staleName: %s", pending)
	}
}

func TestStdioParityDoesNotPublishPendingDiagnosticsAfterDocumentCloses(t *testing.T) {
	client := startStdioTestClient(t)
	defer client.close()

	root := t.TempDir()
	uri := pathToFileURI(filepath.Join(root, "go-closed-diagnostics.asp"))
	client.request("initialize", map[string]any{
		"processId":    nil,
		"rootUri":      pathToFileURI(root),
		"capabilities": map[string]any{},
	})
	notifyOpenClassicASPDocument(t, client, uri, "<% Option Explicit\nResponse.Write closedName\n%>")
	if err := client.notify("textDocument/didClose", map[string]any{"textDocument": map[string]any{"uri": uri}}); err != nil {
		t.Fatal(err)
	}
	cleared := waitForDiagnosticsCleared(t, client, uri)
	if !diagnosticsAreCleared(t, cleared, uri) {
		t.Fatalf("didClose diagnostics were not cleared: %s", cleared.Params)
	}
	staleDiagnostics := client.drainNotifications("textDocument/publishDiagnostics")
	for _, message := range staleDiagnostics {
		if diagnosticsURI(t, message) == uri && len(diagnosticsFromMessage(t, message)) > 0 {
			t.Fatalf("didClose left stale diagnostics: %s", message.Params)
		}
	}
}

func TestStdioParityDropsStaleStagedAsyncDiagnosticsWhenNewerDocumentVersionWins(t *testing.T) {
	client := startStdioTestClient(t)
	defer client.close()

	root := t.TempDir()
	const includeCount = 32
	for index := 0; index < includeCount; index++ {
		next := "inc0.inc"
		if index != includeCount-1 {
			next = fmt.Sprintf("inc%d.inc", index+1)
		}
		content := fmt.Sprintf("<!-- #include file=\"%s\" -->\n<%% Const Value%d = %d %%>", next, index, index)
		if err := os.WriteFile(filepath.Join(root, fmt.Sprintf("inc%d.inc", index)), []byte(content), 0o644); err != nil {
			t.Fatal(err)
		}
	}
	uri := pathToFileURI(filepath.Join(root, "default.asp"))
	client.request("initialize", map[string]any{
		"processId":    nil,
		"rootUri":      pathToFileURI(root),
		"capabilities": map[string]any{},
	})
	notifyConfiguration(t, client, verboseImmediateDiagnosticsSettings())
	notifyOpenClassicASPDocument(t, client, uri, `<!-- #include file="inc0.inc" -->
<%
Option Explicit
Response.Write staleName
%>`)
	client.waitForNotification("textDocument/publishDiagnostics", uri)
	client.drainNotifications("aspLsp/status")
	if err := client.notify("textDocument/didChange", map[string]any{
		"textDocument": map[string]any{"uri": uri, "version": 2},
		"contentChanges": []map[string]any{{"text": `<%
Option Explicit
Response.Write finalName
%>`}},
	}); err != nil {
		t.Fatal(err)
	}
	diagnostics, seen := client.waitForNotificationWithSeen("textDocument/publishDiagnostics", "finalName")
	logs := mustJSONText(t, seen)
	if version := diagnosticVersion(t, diagnostics); version != 2 {
		t.Fatalf("staged diagnostics version = %d, want 2: %s", version, diagnostics.Params)
	}
	if !strings.Contains(logs, "diagnostics.include.stale") {
		_, moreSeen := client.waitForNotificationWithSeen("window/logMessage", "diagnostics.include.stale")
		seen = append(seen, moreSeen...)
		logs = mustJSONText(t, seen)
	}
	if !strings.Contains(logs, "diagnostics.include.stale") {
		t.Fatalf("staged stale edit logs missing diagnostics.include.stale: %s", logs)
	}
	time.Sleep(80 * time.Millisecond)
	pending := mustJSONText(t, client.drainNotifications("textDocument/publishDiagnostics"))
	for _, unexpected := range []string{"staleName", "Include cycle detected"} {
		if strings.Contains(pending, unexpected) {
			t.Fatalf("pending diagnostics included %q: %s", unexpected, pending)
		}
	}
}
