package lspserver

import (
	"os"
	"path/filepath"
	"strings"
	"testing"
)

func TestStdioParityReusesTypeScriptLanguageServiceAcrossJavaScriptDocumentEdits(t *testing.T) {
	client := startStdioTestClient(t)
	defer client.close()

	root := t.TempDir()
	uri := "file:///tmp/go-js-ls-reuse.asp"
	source := `<script>
const alphaValue = 1;
alph
</script>`
	updated := `<script>
const betaValue = 1;
beta
</script>`
	client.request("initialize", map[string]any{
		"processId":    nil,
		"rootUri":      pathToFileURI(root),
		"capabilities": map[string]any{},
	})
	notifyConfiguration(t, client, verboseImmediateDiagnosticsSettings())
	notifyOpenClassicASPDocument(t, client, uri, source)
	client.waitForLogContaining("LSP check completed")
	client.drainNotifications("window/logMessage")

	assertJSCompletionContains(t, client, uri, source, "alph", "alphaValue")
	waitForLogMessages(t, client, []string{"javascript.openProjectFiles.reuse", "javascript.languageService.reuse"})
	client.drainNotifications("window/logMessage")

	assertJSCompletionContains(t, client, uri, source, "alph", "alphaValue")
	waitForLogMessages(t, client, []string{"javascript.openProjectFiles.reuse", "javascript.languageService.reuse"})
	client.drainNotifications("window/logMessage")

	if err := client.notify("textDocument/didChange", map[string]any{
		"textDocument":   map[string]any{"uri": uri, "version": 2},
		"contentChanges": []map[string]any{{"text": updated}},
	}); err != nil {
		t.Fatal(err)
	}
	client.waitForLogContaining("LSP check completed")
	client.drainNotifications("window/logMessage")

	beta := requestCompletionAtSuffix(t, client, uri, updated, "beta")
	betaJSON := mustJSONText(t, beta.Result)
	if !strings.Contains(betaJSON, "betaValue") || strings.Contains(betaJSON, "alphaValue") {
		t.Fatalf("JavaScript completion after edit mismatch: %s", betaJSON)
	}
	waitForLogMessages(t, client, []string{"javascript.openProjectFiles.reuse", "javascript.languageService.reuse"})
	client.drainNotifications("window/logMessage")

	htmlEdited := "<div>shifted</div>\n" + updated
	if err := client.notify("textDocument/didChange", map[string]any{
		"textDocument":   map[string]any{"uri": uri, "version": 3},
		"contentChanges": []map[string]any{{"text": htmlEdited}},
	}); err != nil {
		t.Fatal(err)
	}
	client.waitForLogContaining("LSP check completed")
	client.drainNotifications("window/logMessage")
	assertJSCompletionContains(t, client, uri, htmlEdited, "beta", "betaValue")
	client.waitForLogContaining("javascript.languageService.reuse")
}

func TestStdioParitySharesTypeScriptLanguageServiceAcrossFilesInSameJavaScriptProject(t *testing.T) {
	client := startStdioTestClient(t)
	defer client.close()

	root := t.TempDir()
	firstDir := filepath.Join(root, "first")
	secondDir := filepath.Join(root, "second")
	if err := os.MkdirAll(firstDir, 0o755); err != nil {
		t.Fatal(err)
	}
	if err := os.MkdirAll(secondDir, 0o755); err != nil {
		t.Fatal(err)
	}
	firstFile := filepath.Join(firstDir, "one.asp")
	secondFile := filepath.Join(secondDir, "two.asp")
	firstSource := `<script>
const firstProjectValue = 1;
firstProject
</script>`
	secondSource := `<script>
const secondProjectValue = 1;
secondProject
</script>`
	writeJSProjectFixture(t, firstFile, firstSource)
	writeJSProjectFixture(t, secondFile, secondSource)
	firstURI := pathToFileURI(firstFile)
	secondURI := pathToFileURI(secondFile)
	client.request("initialize", map[string]any{
		"processId":    nil,
		"rootUri":      pathToFileURI(root),
		"capabilities": map[string]any{},
	})
	notifyConfiguration(t, client, verboseImmediateDiagnosticsSettings())
	notifyOpenClassicASPDocument(t, client, firstURI, firstSource)
	client.waitForLogContaining("LSP check completed")

	assertJSCompletionContains(t, client, firstURI, firstSource, "firstProject", "firstProjectValue")
	client.waitForLogContaining("javascript.languageService.reuse")
	client.drainNotifications("window/logMessage")

	notifyOpenClassicASPDocument(t, client, secondURI, secondSource)
	client.waitForLogContaining("LSP check completed")
	assertJSCompletionContains(t, client, secondURI, secondSource, "secondProject", "secondProjectValue")
	client.waitForLogContaining("javascript.languageService.reuse")
}

func TestStdioParityAnswersJavaScriptCompletionsAfterEditsWithoutFlushingProjectPrewarm(t *testing.T) {
	client := startStdioTestClient(t)
	defer client.close()

	root := t.TempDir()
	uri := "file:///tmp/go-js-interactive-no-prewarm-flush.asp"
	source := `<script>
const alphaValue = 1;
alph
</script>`
	updated := `<script>
const betaValue = 1;
bet
</script>`
	client.request("initialize", map[string]any{
		"processId":    nil,
		"rootUri":      pathToFileURI(root),
		"capabilities": map[string]any{},
	})
	notifyConfiguration(t, client, map[string]any{"aspLsp": map[string]any{
		"debug":       map[string]any{"output": "verbose"},
		"diagnostics": map[string]any{"debounceMs": 1000},
	}})
	notifyOpenClassicASPDocument(t, client, uri, source)
	client.waitForLogContaining("LSP check completed")

	assertJSCompletionContains(t, client, uri, source, "alph", "alphaValue")
	client.waitForLogContaining("javascript.languageService.reuse")
	client.drainNotifications("window/logMessage")

	if err := client.notify("textDocument/didChange", map[string]any{
		"textDocument":   map[string]any{"uri": uri, "version": 2},
		"contentChanges": []map[string]any{{"text": updated}},
	}); err != nil {
		t.Fatal(err)
	}
	beta := requestCompletionAtSuffix(t, client, uri, updated, "bet")
	serialized := mustJSONText(t, beta.Result)
	if !strings.Contains(serialized, "betaValue") || strings.Contains(serialized, "alphaValue") {
		t.Fatalf("JavaScript completion after debounced edit mismatch: %s", serialized)
	}
	logs := mustJSONText(t, client.drainNotifications("window/logMessage"))
	for _, expected := range []string{"projectUpdate.scheduled", "javascript.languageService.reuse"} {
		if !strings.Contains(logs, expected) {
			t.Fatalf("JavaScript edit logs missing %q: %s", expected, logs)
		}
	}
	if strings.Contains(logs, "projectUpdate.flushed: reason=document.change") {
		t.Fatalf("JavaScript edit flushed project prewarm: %s", logs)
	}
}

func TestStdioParityReportsVirtualJavaScriptSnapshotChangeRangeReuse(t *testing.T) {
	client := startStdioTestClient(t)
	defer client.close()

	root := t.TempDir()
	uri := "file:///tmp/go-js-snapshot-change-range.asp"
	source := `<script>
const alphaValue = 1;
alph
</script>`
	client.request("initialize", map[string]any{
		"processId":    nil,
		"rootUri":      pathToFileURI(root),
		"capabilities": map[string]any{},
	})
	notifyConfiguration(t, client, verboseImmediateDiagnosticsSettings())
	notifyOpenClassicASPDocument(t, client, uri, source)
	client.waitForLogContaining("LSP check completed")
	requestCompletionAtSuffix(t, client, uri, source, "alph")
	client.waitForLogContaining("js.snapshot.changeRange.miss")
	client.drainNotifications("window/logMessage")

	source = notifyRangedReplacement(t, client, uri, source, 1, "alphaValue", "betaValue")
	client.waitForLogContaining("LSP check completed")
	requestCompletionAtSuffix(t, client, uri, source, "alph")
	client.waitForLogContaining("javascript.languageService.reuse")
}

func TestStdioParityDoesNotReuseCompletionResultsAfterPrefixDocumentEdits(t *testing.T) {
	client := startStdioTestClient(t)
	defer client.close()

	root := t.TempDir()
	uri := "file:///tmp/go-completion-cache.asp"
	source := `<%
Response.W
%>`
	client.request("initialize", map[string]any{
		"processId":    nil,
		"rootUri":      pathToFileURI(root),
		"capabilities": map[string]any{},
	})
	notifyConfiguration(t, client, verboseImmediateDiagnosticsSettings())
	notifyOpenClassicASPDocument(t, client, uri, source)
	client.waitForLogContaining("LSP check completed")

	first := client.request("textDocument/completion", map[string]any{
		"textDocument": map[string]any{"uri": uri},
		"position":     positionAt(source, strings.Index(source, "Response.W")+len("Response.W")),
	})
	if !strings.Contains(mustJSONText(t, first.Result), "Write") {
		t.Fatalf("initial completion missing Write: %s", mustJSONText(t, first.Result))
	}
	client.drainNotifications("window/logMessage")

	insertOffset := strings.Index(source, "Response.W") + len("Response.W")
	if err := client.notify("textDocument/didChange", map[string]any{
		"textDocument": map[string]any{"uri": uri, "version": 2},
		"contentChanges": []map[string]any{{
			"range": map[string]any{
				"start": positionAt(source, insertOffset),
				"end":   positionAt(source, insertOffset),
			},
			"text": "r",
		}},
	}); err != nil {
		t.Fatal(err)
	}
	source = source[:insertOffset] + "r" + source[insertOffset:]
	client.waitForLogContaining("LSP check completed")
	refreshed := client.request("textDocument/completion", map[string]any{
		"textDocument": map[string]any{"uri": uri},
		"position":     positionAt(source, strings.Index(source, "Response.Wr")+len("Response.Wr")),
	})
	if !strings.Contains(mustJSONText(t, refreshed.Result), "Write") {
		t.Fatalf("refreshed completion missing Write: %s", mustJSONText(t, refreshed.Result))
	}
}

func TestStdioParityDoesNotAdvanceWorkspaceGenerationForDocumentEdits(t *testing.T) {
	client := startStdioTestClient(t)
	defer client.close()

	root := t.TempDir()
	uri := "file:///tmp/go-document-edit-generation.asp"
	source := `<%
Option Explicit
Dim known
Response.Write known
%>`
	client.request("initialize", map[string]any{
		"processId":    nil,
		"rootUri":      pathToFileURI(root),
		"capabilities": map[string]any{},
	})
	notifyConfiguration(t, client, verboseImmediateDiagnosticsSettings())
	notifyOpenClassicASPDocument(t, client, uri, source)
	client.waitForLogContaining("LSP check completed")
	client.drainNotifications("window/logMessage")

	source = notifyRangedReplacement(t, client, uri, source, 2, "known", "renamed")
	client.waitForNotification("textDocument/publishDiagnostics", "")
	client.waitForLogContaining("LSP check completed")
	logs := mustJSONText(t, client.drainNotifications("window/logMessage"))
	if !strings.Contains(source, "Dim renamed") {
		t.Fatalf("ranged replacement did not update source: %s", source)
	}
	if strings.Contains(logs, "invalidation.workspaceIndex") {
		t.Fatalf("document edit advanced workspace generation: %s", logs)
	}
}

func requestCompletionAtSuffix(t *testing.T, client *stdioTestClient, uri string, source string, suffix string) *rpcMessage {
	t.Helper()
	return client.request("textDocument/completion", map[string]any{
		"textDocument": map[string]any{"uri": uri},
		"position":     positionAt(source, strings.LastIndex(source, suffix)+len(suffix)),
	})
}

func assertJSCompletionContains(t *testing.T, client *stdioTestClient, uri string, source string, suffix string, expected string) {
	t.Helper()
	response := requestCompletionAtSuffix(t, client, uri, source, suffix)
	if !strings.Contains(mustJSONText(t, response.Result), expected) {
		t.Fatalf("JavaScript completion missing %q: %s", expected, mustJSONText(t, response.Result))
	}
}

func writeJSProjectFixture(t *testing.T, path string, text string) {
	t.Helper()
	if err := os.WriteFile(path, []byte(text), 0o644); err != nil {
		t.Fatal(err)
	}
}
