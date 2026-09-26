package lspserver

import (
	"regexp"
	"strconv"
	"strings"
	"testing"
)

func TestStdioParityDoesNotDuplicateActiveVirtualDocumentInJavaScriptWorkerPayloads(t *testing.T) {
	client := startStdioTestClient(t)
	defer client.close()

	root := t.TempDir()
	client.request("initialize", map[string]any{
		"processId":    nil,
		"rootUri":      pathToFileURI(root),
		"capabilities": map[string]any{},
	})
	notifyConfiguration(t, client, map[string]any{"aspLsp": map[string]any{
		"checkJs":     true,
		"debug":       map[string]any{"output": "verbose"},
		"diagnostics": map[string]any{"debounceMs": 0},
		"javascript":  map[string]any{"ignoreProjectConfig": true},
	}})

	filler := strings.Repeat("x", 24_000)
	uri := "file:///tmp/go-js-worker-payload.asp"
	source := `<script>
const payloadMarker = "` + filler + `";
missingPayloadName.toFixed();
</script>`
	notifyOpenClassicASPDocument(t, client, uri, source)

	diagnostics, seen := client.waitForNotificationWithSeen("textDocument/publishDiagnostics", "missingPayloadName")
	if serialized := string(diagnostics.Params); !strings.Contains(serialized, "asp-lsp-typescript") {
		t.Fatalf("JavaScript semantic diagnostics missing source: %s", serialized)
	}
	workerLog := findNotificationContaining(seen, "window/logMessage", "javascript.diagnostics.worker")
	if workerLog == nil {
		workerLog = client.waitForLogContaining("javascript.diagnostics.worker")
	}
	payloadBytes := jsDiagnosticsWorkerPayloadBytesFromLog(t, string(workerLog.Params))
	if payloadBytes <= len(filler) {
		t.Fatalf("JavaScript worker payload bytes = %d, want > filler length %d: %s", payloadBytes, len(filler), workerLog.Params)
	}
	if float64(payloadBytes) >= float64(len(source))*1.7 {
		t.Fatalf("JavaScript worker payload bytes = %d, want < source length * 1.7 (%d): %s", payloadBytes, len(source), workerLog.Params)
	}
}

func TestStdioParityPrewarmsJavaScriptDiagnosticsWorkerAfterDidOpenWhenCheckJSIsEnabled(t *testing.T) {
	client := startStdioTestClient(t)
	defer client.close()

	root := t.TempDir()
	client.request("initialize", map[string]any{
		"processId":    nil,
		"rootUri":      pathToFileURI(root),
		"capabilities": map[string]any{},
	})
	notifyConfiguration(t, client, map[string]any{"aspLsp": map[string]any{
		"checkJs":     true,
		"debug":       map[string]any{"output": "verbose"},
		"diagnostics": map[string]any{"debounceMs": 0},
		"javascript":  map[string]any{"ignoreProjectConfig": true},
	}})

	notifyOpenClassicASPDocument(t, client, "file:///tmp/go-js-worker-prewarm.asp", `<script>
const workerPrewarmValue = 1;
</script>`)

	client.waitForLogContaining("javascript.diagnostics.prewarm.completed")
}

func TestStdioParityReusesJavaScriptDiagnosticsAfterHTMLOnlySourceShifts(t *testing.T) {
	client := startStdioTestClient(t)
	defer client.close()

	root := t.TempDir()
	uri := "file:///tmp/go-js-diagnostics-cache.asp"
	source := `<div>before</div>
<script>
function demo(unusedParam) {
  const unusedLocal = 1;
  return 1;
}
</script>
<script runat="server" language="JScript">
var broken = ;
</script>`
	client.request("initialize", map[string]any{
		"processId":    nil,
		"rootUri":      pathToFileURI(root),
		"capabilities": map[string]any{},
	})
	notifyConfiguration(t, client, map[string]any{"aspLsp": map[string]any{
		"debug":       map[string]any{"output": "verbose"},
		"diagnostics": map[string]any{"debounceMs": 0},
	}})
	notifyOpenClassicASPDocument(t, client, uri, source)
	firstDiagnostics := waitForDiagnosticsContaining(t, client, "asp-lsp-typescript-unused")
	firstUnused := diagnosticContaining(t, firstDiagnostics, "unusedLocal")
	if firstUnused == nil || firstUnused.Range.Start.Line != 3 {
		t.Fatalf("initial unusedLocal diagnostic range mismatch: %#v in %s", firstUnused, firstDiagnostics.Params)
	}
	if !strings.Contains(string(firstDiagnostics.Params), "Expression expected") {
		t.Fatalf("initial diagnostics missing JScript syntax error: %s", firstDiagnostics.Params)
	}
	client.waitForLogContaining("LSP check completed")
	client.drainNotifications("window/logMessage")
	client.drainNotifications("textDocument/publishDiagnostics")

	source = notifyRangedReplacement(t, client, uri, source, 0, "before", "before\nshifted")

	nextDiagnostics, seen := client.waitForNotificationWithSeen("textDocument/publishDiagnostics", "asp-lsp-typescript-unused")
	syntaxReuseLog := findNotificationContaining(seen, "window/logMessage", "check.javascriptSyntax.reuse")
	if syntaxReuseLog == nil {
		syntaxReuseLog = client.waitForLogContaining("check.javascriptSyntax.reuse")
	}
	diagnosticsReuseLog := findNotificationContaining(seen, "window/logMessage", "check.javascriptDiagnostics.reuse")
	if diagnosticsReuseLog == nil {
		diagnosticsReuseLog = client.waitForLogContaining("check.javascriptDiagnostics.reuse")
	}
	nextUnused := diagnosticContaining(t, nextDiagnostics, "unusedLocal")
	if nextUnused == nil || nextUnused.Range.Start.Line != 4 {
		t.Fatalf("shifted unusedLocal diagnostic range mismatch: %#v in %s", nextUnused, nextDiagnostics.Params)
	}
	if !strings.Contains(string(nextDiagnostics.Params), "Expression expected") {
		t.Fatalf("shifted diagnostics missing JScript syntax error: %s", nextDiagnostics.Params)
	}
	logs := mustJSONText(t, append([]*rpcMessage{syntaxReuseLog, diagnosticsReuseLog}, client.drainNotifications("window/logMessage")...))
	for _, unexpected := range []string{"check.javascriptSemantic", "check.javascriptUnused"} {
		if strings.Contains(logs, unexpected) {
			t.Fatalf("HTML-only shift reran %s: %s", unexpected, logs)
		}
	}
	if !strings.Contains(source, "before\nshifted") {
		t.Fatalf("ranged replacement did not update source: %s", source)
	}
}

func jsDiagnosticsWorkerPayloadBytesFromLog(t *testing.T, text string) int {
	t.Helper()
	matches := regexp.MustCompile(`payloadBytes=(\d+)`).FindStringSubmatch(text)
	if len(matches) != 2 {
		t.Fatalf("JavaScript worker payload log missing payloadBytes: %s", text)
	}
	value, err := strconv.Atoi(matches[1])
	if err != nil {
		t.Fatal(err)
	}
	return value
}
