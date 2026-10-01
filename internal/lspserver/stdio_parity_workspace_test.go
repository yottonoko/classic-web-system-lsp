package lspserver

import (
	"encoding/json"
	"os"
	"path/filepath"
	"regexp"
	"strconv"
	"strings"
	"testing"
	"time"
)

func TestStdioParityRunsVBScriptProjectDiagnosticsThroughWorkerPool(t *testing.T) {
	client := startStdioTestClient(t)
	defer client.close()

	root := t.TempDir()
	page := filepath.Join(root, "default.asp")
	workerSource := `<%
Sub Worker()
  Dim unusedValue
End Sub
Response.Write "ok"
%>`
	if err := os.WriteFile(page, []byte(workerSource), 0o644); err != nil {
		t.Fatal(err)
	}
	initializeWithConfigurationAndWaitForWorkspaceIndex(t, client, map[string]any{
		"processId":    nil,
		"rootUri":      pathToFileURI(root),
		"capabilities": map[string]any{},
	}, map[string]any{"aspLsp": map[string]any{
		"debug":       map[string]any{"output": "verbose"},
		"diagnostics": map[string]any{"debounceMs": 0},
		"workspace":   map[string]any{"busyAnalysisConcurrency": 1},
	}})

	diagnostics := client.request("workspace/diagnostic", map[string]any{
		"previousResultIds": []any{},
	})
	serialized := mustJSONText(t, diagnostics.Result)
	if !strings.Contains(serialized, "unusedValue") || !strings.Contains(serialized, "asp-lsp-vbscript-unused") {
		t.Fatalf("workspace diagnostics missing VBScript unused result: %s", serialized)
	}
	time.Sleep(100 * time.Millisecond)
	logText := mustJSONText(t, client.drainNotifications("window/logMessage"))
	for _, expected := range []string{
		"vbscript.worker.dispatch",
		"check.workspace.vbscript.diagnostics.worker",
		"vbscript.worker.started",
		"vbscript.worker.completed",
		"worker.payload.bytes",
	} {
		if !strings.Contains(logText, expected) {
			t.Fatalf("workspace diagnostics worker log missing %q: %s", expected, logText)
		}
	}
	if started, completed := strings.Index(logText, "vbscript.worker.started"), strings.Index(logText, "vbscript.worker.completed"); started < 0 || completed <= started {
		t.Fatalf("workspace diagnostics worker lifecycle logs are out of order: %s", logText)
	}
	payloadBytes := payloadBytesFromLogText(t, logText)
	if payloadBytes >= 116_000 {
		t.Fatalf("worker payload bytes = %d, want < 116000: %s", payloadBytes, logText)
	}
}

func TestStdioParityIgnoresUnrelatedWatchedFileChangesWithoutRefreshingOpenASPDocuments(t *testing.T) {
	client := startStdioTestClient(t)
	defer client.close()

	root := t.TempDir()
	uri := "file:///tmp/go-watched-noop.asp"
	client.request("initialize", map[string]any{
		"processId":    nil,
		"rootUri":      pathToFileURI(root),
		"capabilities": map[string]any{},
	})
	notifyConfiguration(t, client, map[string]any{"aspLsp": map[string]any{
		"debug": map[string]any{"output": "verbose"},
	}})
	notifyOpenClassicASPDocument(t, client, uri, `<% Response.Write "ok" %>`)
	time.Sleep(350 * time.Millisecond)
	client.drainNotifications("window/logMessage")

	if err := client.notify("workspace/didChangeWatchedFiles", map[string]any{
		"changes": []map[string]any{{
			"uri":  "file:///tmp/readme.txt",
			"type": 2,
		}},
	}); err != nil {
		t.Fatal(err)
	}
	time.Sleep(350 * time.Millisecond)
	logText := mustJSONText(t, client.drainNotifications("window/logMessage"))
	if strings.Contains(logText, "analysis.parse") || strings.Contains(logText, "LSP check started") {
		t.Fatalf("unrelated watched file change refreshed open ASP document: %s", logText)
	}
}

func TestStdioParityKeepsJavaScriptProjectChangesFreshWithoutDroppingASPParseCaches(t *testing.T) {
	client := startStdioTestClient(t)
	defer client.close()

	root := t.TempDir()
	helper := filepath.Join(root, "helper.js")
	page := filepath.Join(root, "default.asp")
	if err := os.WriteFile(helper, []byte("const oldProjectGlobal = 1;\n"), 0o644); err != nil {
		t.Fatal(err)
	}
	if err := os.WriteFile(filepath.Join(root, "jsconfig.json"), []byte(`{"include":["*.js","*.asp"]}`), 0o644); err != nil {
		t.Fatal(err)
	}
	source := `<script>
newProjectGlobal;
</script>`
	if err := os.WriteFile(page, []byte(source), 0o644); err != nil {
		t.Fatal(err)
	}
	uri := pathToFileURI(page)
	client.request("initialize", map[string]any{
		"processId":    nil,
		"rootUri":      pathToFileURI(root),
		"capabilities": map[string]any{},
	})
	notifyConfiguration(t, client, map[string]any{"aspLsp": map[string]any{
		"checkJs":     true,
		"debug":       map[string]any{"output": "verbose"},
		"diagnostics": map[string]any{"debounceMs": 0},
	}})
	notifyOpenClassicASPDocument(t, client, uri, source)
	waitForDiagnosticsAndLogMessages(t, client, "newProjectGlobal", []string{"javascriptSemantic.worker", "LSP check completed"})
	client.drainNotifications("window/logMessage")

	if err := os.WriteFile(helper, []byte("const newProjectGlobal = 1;\n"), 0o644); err != nil {
		t.Fatal(err)
	}
	if err := client.notify("workspace/didChangeWatchedFiles", map[string]any{
		"changes": []map[string]any{{
			"uri":  pathToFileURI(helper),
			"type": 2,
		}},
	}); err != nil {
		t.Fatal(err)
	}
	logs := waitForLogMessages(t, client, []string{"invalidation.jsProject", "LSP check completed"})
	pulled := client.request("textDocument/diagnostic", map[string]any{
		"textDocument": map[string]any{"uri": uri},
	})
	if strings.Contains(mustJSONText(t, pulled.Result), "Cannot find name 'newProjectGlobal'") {
		t.Fatalf("JavaScript project diagnostic remained stale: %s", mustJSONText(t, pulled.Result))
	}
	logText := mustJSONText(t, append(logs, client.drainNotifications("window/logMessage")...))
	if strings.Contains(logText, "analysis.parse.full") || strings.Contains(logText, "invalidation.workspaceIndex") {
		t.Fatalf("JavaScript invalidation dropped ASP parse caches or workspace index: %s", logText)
	}
}

func TestStdioParityRefreshesOpenFilesAfterChangedIncludeExports(t *testing.T) {
	client := startStdioTestClient(t)
	defer client.close()

	root := t.TempDir()
	include := filepath.Join(root, "shared.inc")
	affected := filepath.Join(root, "affected.asp")
	unaffected := filepath.Join(root, "unaffected.asp")
	unrelated := filepath.Join(root, "unrelated.asp")
	writeWorkspaceFixture(t, include, `<%
Function SharedValue()
  SharedValue = "old"
End Function

Function OtherValue()
  OtherValue = "same"
End Function
%>`)
	writeWorkspaceFixture(t, affected, `<!-- #include file="shared.inc" -->
<% Response.Write SharedValue() %>`)
	writeWorkspaceFixture(t, unaffected, `<!-- #include file="shared.inc" -->
<% Response.Write OtherValue() %>`)
	writeWorkspaceFixture(t, unrelated, `<% Response.Write "standalone" %>`)
	affectedURI := pathToFileURI(affected)
	unaffectedURI := pathToFileURI(unaffected)
	unrelatedURI := pathToFileURI(unrelated)
	client.request("initialize", map[string]any{
		"processId":    nil,
		"rootUri":      pathToFileURI(root),
		"capabilities": map[string]any{},
	})
	notifyConfiguration(t, client, verboseImmediateDiagnosticsSettings())
	for _, fileName := range []string{affected, unaffected, unrelated} {
		notifyOpenClassicASPDocument(t, client, pathToFileURI(fileName), mustReadText(t, fileName))
	}
	waitForLogMessages(t, client, []string{"LSP check completed"})
	waitForLogMessages(t, client, []string{"LSP check completed"})
	waitForLogMessages(t, client, []string{"LSP check completed"})
	client.drainNotifications("window/logMessage")

	writeWorkspaceFixture(t, include, `<%
Function SharedValueRenamed()
  SharedValueRenamed = "new"
End Function

Function OtherValue()
  OtherValue = "same"
End Function
%>`)
	if err := client.notify("workspace/didChangeWatchedFiles", map[string]any{
		"changes": []map[string]any{{
			"uri":  pathToFileURI(include),
			"type": 2,
		}},
	}); err != nil {
		t.Fatal(err)
	}
	firstAnalysisLog := waitForLogMessages(t, client, []string{"LSP check completed"})
	time.Sleep(500 * time.Millisecond)
	logText := mustJSONText(t, append(firstAnalysisLog, client.drainNotifications("window/logMessage")...))
	if !strings.Contains(logText, affectedURI) || !strings.Contains(logText, unaffectedURI) {
		t.Fatalf("include export change did not refresh dependents: %s", logText)
	}
	if strings.Contains(logText, unrelatedURI) {
		t.Fatalf("include export change refreshed unrelated file: %s", logText)
	}
}

func TestStdioParityKeepsDependentDiagnosticsIdleAfterPrivateIncludeImplementationChanges(t *testing.T) {
	client := startStdioTestClient(t)
	defer client.close()

	root := t.TempDir()
	include := filepath.Join(root, "shared.inc")
	page := filepath.Join(root, "default.asp")
	writeWorkspaceFixture(t, include, `<%
Function SharedValue()
  Dim privateValue
  privateValue = "old"
  SharedValue = privateValue
End Function
%>`)
	writeWorkspaceFixture(t, page, `<!-- #include file="shared.inc" -->
<% Response.Write SharedValue() %>`)
	uri := pathToFileURI(page)
	initializeWithConfigurationAndWaitForWorkspaceIndex(t, client, map[string]any{
		"processId":    nil,
		"rootUri":      pathToFileURI(root),
		"capabilities": map[string]any{},
	}, verboseImmediateDiagnosticsSettings())
	notifyOpenClassicASPDocument(t, client, uri, mustReadText(t, page))
	client.waitForLogContaining("LSP check completed")
	client.drainNotifications("window/logMessage")
	client.drainNotifications("textDocument/publishDiagnostics")

	writeWorkspaceFixture(t, include, `<%
Function SharedValue()
  Dim privateValue
  privateValue = "new"
  SharedValue = privateValue
End Function
%>`)
	if err := client.notify("workspace/didChangeWatchedFiles", map[string]any{
		"changes": []map[string]any{{
			"uri":  pathToFileURI(include),
			"type": 2,
		}},
	}); err != nil {
		t.Fatal(err)
	}
	time.Sleep(350 * time.Millisecond)
	logText := mustJSONText(t, client.drainNotifications("window/logMessage"))
	diagnosticsText := mustJSONText(t, client.drainNotifications("textDocument/publishDiagnostics"))
	if !strings.Contains(logText, "include.publicBoundary.reuse") {
		t.Fatalf("private include change did not reuse public boundary: %s", logText)
	}
	if strings.Contains(logText, "LSP check started: "+uri) {
		t.Fatalf("private include change refreshed dependent diagnostics: %s", logText)
	}
	if strings.Contains(diagnosticsText, uri) {
		t.Fatalf("private include change published dependent diagnostics: %s", diagnosticsText)
	}
}

func TestStdioParityRefreshesDependentsAfterIncludeImplicitGlobalCandidateChanges(t *testing.T) {
	client := startStdioTestClient(t)
	defer client.close()

	root := t.TempDir()
	include := filepath.Join(root, "shared.inc")
	page := filepath.Join(root, "default.asp")
	writeWorkspaceFixture(t, include, `<%
Function SharedValue()
  nestedValue = 1
  SharedValue = nestedValue
End Function
%>`)
	writeWorkspaceFixture(t, page, `<!-- #include file="shared.inc" -->
<% Response.Write SharedValue() %>`)
	uri := pathToFileURI(page)
	client.request("initialize", map[string]any{
		"processId":    nil,
		"rootUri":      pathToFileURI(root),
		"capabilities": map[string]any{},
	})
	notifyConfiguration(t, client, verboseImmediateDiagnosticsSettings())
	notifyOpenClassicASPDocument(t, client, uri, mustReadText(t, page))
	client.waitForLogContaining("LSP check completed")
	client.drainNotifications("window/logMessage")

	writeWorkspaceFixture(t, include, `<%
Function SharedValue()
  renamedNestedValue = 1
  SharedValue = renamedNestedValue
End Function
%>`)
	if err := client.notify("workspace/didChangeWatchedFiles", map[string]any{
		"changes": []map[string]any{{
			"uri":  pathToFileURI(include),
			"type": 2,
		}},
	}); err != nil {
		t.Fatal(err)
	}
	checkLogs := waitForLogMessages(t, client, []string{"LSP check completed"})
	time.Sleep(350 * time.Millisecond)
	logText := mustJSONText(t, append(checkLogs, client.drainNotifications("window/logMessage")...))
	if !strings.Contains(logText, "invalidation.includePublicBoundary") || !strings.Contains(logText, "LSP check started: "+uri) {
		t.Fatalf("implicit global include change did not refresh dependent: %s", logText)
	}
}

func TestStdioParityKeepsDependentJavaScriptIslandDiagnosticsIdleAfterPrivateIncludeChanges(t *testing.T) {
	client := startStdioTestClient(t)
	defer client.close()

	root := t.TempDir()
	include := filepath.Join(root, "shared.inc")
	page := filepath.Join(root, "default.asp")
	writeWorkspaceFixture(t, include, `<%
Function SharedValue()
  Dim privateValue
  privateValue = "old"
  SharedValue = privateValue
End Function

Function PrivateOnlyUtility()
  Dim unusedPrivateLocal
  PrivateOnlyUtility = "old"
End Function
%>`)
	writeWorkspaceFixture(t, page, `<!-- #include file="shared.inc" -->
<script>
const fromServer = <%= SharedValue() %>;
const stableClient = 1;
</script>
<% Response.Write SharedValue() %>`)
	uri := pathToFileURI(page)
	initializeWithConfigurationAndWaitForWorkspaceIndex(t, client, map[string]any{
		"processId":    nil,
		"rootUri":      pathToFileURI(root),
		"capabilities": map[string]any{},
	}, verboseImmediateDiagnosticsSettings())
	notifyOpenClassicASPDocument(t, client, uri, mustReadText(t, page))
	client.waitForLogContaining("LSP check completed")
	client.drainNotifications("window/logMessage")
	client.drainNotifications("textDocument/publishDiagnostics")

	writeWorkspaceFixture(t, include, `<%
Function SharedValue()
  Dim privateValue
  privateValue = "old"
  SharedValue = privateValue
End Function

Function PrivateOnlyUtility()
  Dim unusedPrivateLocal
  unusedPrivateLocal = "changed"
  PrivateOnlyUtility = unusedPrivateLocal
End Function
%>`)
	if err := client.notify("workspace/didChangeWatchedFiles", map[string]any{
		"changes": []map[string]any{{
			"uri":  pathToFileURI(include),
			"type": 2,
		}},
	}); err != nil {
		t.Fatal(err)
	}
	time.Sleep(350 * time.Millisecond)
	logText := mustJSONText(t, client.drainNotifications("window/logMessage"))
	diagnosticsText := mustJSONText(t, client.drainNotifications("textDocument/publishDiagnostics"))
	if !strings.Contains(logText, "include.publicBoundary.reuse") {
		t.Fatalf("private include JavaScript island change did not reuse public boundary: %s", logText)
	}
	if strings.Contains(logText, "LSP check started: "+uri) {
		t.Fatalf("private include JavaScript island change refreshed dependent diagnostics: %s", logText)
	}
	if strings.Contains(diagnosticsText, uri) {
		t.Fatalf("private include JavaScript island change published dependent diagnostics: %s", diagnosticsText)
	}
}

func waitForLogMessages(t *testing.T, client *stdioTestClient, expected []string) []*rpcMessage {
	t.Helper()
	deadline := time.After(10 * time.Second)
	logs := []*rpcMessage{}
	seenText := []string{}
	matched := map[string]bool{}
	for {
		select {
		case message := <-client.notifications:
			seenText = append(seenText, message.Method+" "+string(message.Params))
			if message.Method != "window/logMessage" {
				continue
			}
			logs = append(logs, message)
			for _, needle := range expected {
				if strings.Contains(string(message.Params), needle) {
					matched[needle] = true
				}
			}
			if len(matched) == len(expected) {
				return logs
			}
		case <-deadline:
			t.Fatalf("timed out waiting for logs %v; seen: %s", expected, strings.Join(seenText, "\n"))
		}
	}
}

func waitForDiagnosticsAndLogMessages(t *testing.T, client *stdioTestClient, diagnostic string, logs []string) []*rpcMessage {
	t.Helper()
	deadline := time.After(10 * time.Second)
	seenText := []string{}
	matchedLogs := map[string]bool{}
	sawDiagnostic := false
	collectedLogs := []*rpcMessage{}
	for {
		select {
		case message := <-client.notifications:
			seenText = append(seenText, message.Method+" "+string(message.Params))
			if message.Method == "textDocument/publishDiagnostics" {
				sawDiagnostic = sawDiagnostic || diagnosticsContainMessage(t, message, diagnostic) || strings.Contains(string(message.Params), diagnostic)
			}
			if message.Method == "window/logMessage" {
				collectedLogs = append(collectedLogs, message)
				for _, needle := range logs {
					if strings.Contains(string(message.Params), needle) {
						matchedLogs[needle] = true
					}
				}
			}
			if sawDiagnostic && len(matchedLogs) == len(logs) {
				return collectedLogs
			}
		case <-deadline:
			t.Fatalf("timed out waiting for diagnostic %q and logs %v; diagnostic=%t logs=%v seen: %s", diagnostic, logs, sawDiagnostic, matchedLogs, strings.Join(seenText, "\n"))
		}
	}
}

func writeWorkspaceFixture(t *testing.T, path string, text string) {
	t.Helper()
	if err := os.WriteFile(path, []byte(text), 0o644); err != nil {
		t.Fatal(err)
	}
}

func payloadBytesFromLogText(t *testing.T, text string) int {
	t.Helper()
	re := regexp.MustCompile(`payload=(\d+)`)
	matches := re.FindStringSubmatch(text)
	if len(matches) != 2 {
		t.Fatalf("worker payload log missing payload bytes: %s", text)
	}
	value, err := strconv.Atoi(matches[1])
	if err != nil {
		t.Fatal(err)
	}
	return value
}

func TestStdioParityWorkspaceDiagnosticsSkipJavaScriptUnusedHintsForClosedDocuments(t *testing.T) {
	client := startStdioTestClient(t)
	defer client.close()

	root := t.TempDir()
	source := `<script>
function demo(unusedParam) {
  const unusedLocal = 1;
  return 1;
}
var broken = ;
</script>`
	closedPath := filepath.Join(root, "closed.asp")
	openPath := filepath.Join(root, "open.asp")
	for _, path := range []string{closedPath, openPath} {
		if err := os.WriteFile(path, []byte(source), 0o644); err != nil {
			t.Fatal(err)
		}
	}
	initializeWithConfigurationAndWaitForWorkspaceIndex(t, client, map[string]any{
		"processId":    nil,
		"rootUri":      pathToFileURI(root),
		"capabilities": map[string]any{},
	}, map[string]any{"aspLsp": map[string]any{"diagnostics": map[string]any{"debounceMs": 0}}})
	openClassicASPDocumentWithDiagnostics(t, client, pathToFileURI(openPath), source)

	response := client.request("workspace/diagnostic", map[string]any{"previousResultIds": []any{}})
	var report struct {
		Items []struct {
			URI   string `json:"uri"`
			Items []struct {
				Source string `json:"source"`
			} `json:"items"`
		} `json:"items"`
	}
	if err := json.Unmarshal([]byte(mustJSONText(t, response.Result)), &report); err != nil {
		t.Fatal(err)
	}
	sources := map[string][]string{}
	for _, item := range report.Items {
		for _, diagnostic := range item.Items {
			sources[item.URI] = append(sources[item.URI], diagnostic.Source)
		}
	}
	closed := strings.Join(sources[pathToFileURI(closedPath)], ",")
	open := strings.Join(sources[pathToFileURI(openPath)], ",")
	// Syntax errors still reach the Problems view for every document; the
	// unused hints, which need a full type check, are kept for open editors.
	if !strings.Contains(closed, "asp-lsp-typescript") || strings.Contains(closed, "asp-lsp-typescript-unused") {
		t.Fatalf("closed document sources = %q, want JavaScript syntax diagnostics without unused hints", closed)
	}
	if !strings.Contains(open, "asp-lsp-typescript-unused") {
		t.Fatalf("open document sources = %q, want JavaScript unused hints", open)
	}
}
