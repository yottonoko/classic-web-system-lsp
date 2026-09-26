package lspserver

import (
	"os"
	"path/filepath"
	"strings"
	"testing"
)

func TestStdioParityRestoresWorkspaceDiagnosticsFromDiskCacheAndClearsByCommand(t *testing.T) {
	client := startStdioTestClient(t)
	defer client.close()

	root := t.TempDir()
	cacheDir := filepath.Join(root, ".cache")
	writeWorkspaceCacheFixture(t, filepath.Join(root, "broken.asp"), `<%
Option Explicit
Response.Write missingName
%>`)
	initializeWithConfigurationAndWaitForWorkspaceIndex(t, client, map[string]any{
		"processId":    nil,
		"rootUri":      pathToFileURI(root),
		"capabilities": map[string]any{},
	}, map[string]any{"aspLsp": map[string]any{
		"checkJs": true,
		"debug":   map[string]any{"output": "verbose"},
		"cache":   map[string]any{"enabled": true, "directory": cacheDir},
	}})

	first := workspaceDiagnosticsCacheRequest(t, client)
	if !strings.Contains(first, "missingName") {
		t.Fatalf("first workspace diagnostics missing missingName: %s", first)
	}
	client.waitForLogContaining("database.diagnostics.write")
	client.drainNotifications("window/logMessage")

	second := workspaceDiagnosticsCacheRequest(t, client)
	if !strings.Contains(second, "missingName") {
		t.Fatalf("second workspace diagnostics missing missingName: %s", second)
	}
	client.waitForLogContaining("workspaceDiagnostics.process.hit")

	processClear := client.request("workspace/executeCommand", map[string]any{
		"command": "aspLsp.server.clearProcessCache",
	})
	if got := mustJSONText(t, processClear.Result); got != `{"cleared":"process","ok":true}` {
		t.Fatalf("clearProcessCache result = %s", got)
	}
	client.drainNotifications("window/logMessage")
	third := workspaceDiagnosticsCacheRequest(t, client)
	if !strings.Contains(third, "missingName") {
		t.Fatalf("third workspace diagnostics missing missingName: %s", third)
	}
	client.waitForLogContaining("database.diagnostics.hit")

	diskClear := client.request("workspace/executeCommand", map[string]any{
		"command": "aspLsp.server.clearDiskCache",
	})
	if got := mustJSONText(t, diskClear.Result); got != `{"cleared":"disk","ok":true}` {
		t.Fatalf("clearDiskCache result = %s", got)
	}
	client.drainNotifications("window/logMessage")
	fourth := workspaceDiagnosticsCacheRequest(t, client)
	if !strings.Contains(fourth, "missingName") {
		t.Fatalf("fourth workspace diagnostics missing missingName: %s", fourth)
	}
	client.waitForLogContaining("analysisDatabase.diagnostics.miss")

	allClear := client.request("workspace/executeCommand", map[string]any{
		"command": "aspLsp.server.clearCache",
	})
	if got := mustJSONText(t, allClear.Result); got != `{"cleared":"all","ok":true}` {
		t.Fatalf("clearCache result = %s", got)
	}
	client.drainNotifications("window/logMessage")
	fifth := workspaceDiagnosticsCacheRequest(t, client)
	if !strings.Contains(fifth, "missingName") {
		t.Fatalf("fifth workspace diagnostics missing missingName: %s", fifth)
	}
	client.waitForLogContaining("analysisDatabase.diagnostics.miss")
}

func TestStdioParityInvalidatesDiskCacheWhenIncludeDependenciesChange(t *testing.T) {
	client := startStdioTestClient(t)
	defer client.close()

	root := t.TempDir()
	cacheDir := filepath.Join(root, ".cache")
	owner := filepath.Join(root, "default.asp")
	include := filepath.Join(root, "shared.inc")
	writeWorkspaceCacheFixture(t, owner, `<!-- #include file="shared.inc" -->
<% Response.Write "ok" %>`)
	writeWorkspaceCacheFixture(t, include, `<% Const SharedValue = "ok" %>`)
	initializeWithConfigurationAndWaitForWorkspaceIndex(t, client, map[string]any{
		"processId":    nil,
		"rootUri":      pathToFileURI(root),
		"capabilities": map[string]any{},
	}, map[string]any{"aspLsp": map[string]any{
		"debug": map[string]any{"output": "verbose"},
		"cache": map[string]any{"enabled": true, "directory": cacheDir},
	}})

	first := workspaceDiagnosticsCacheRequest(t, client)
	if strings.Contains(first, "include.missing") {
		t.Fatalf("first workspace diagnostics unexpectedly reported missing include: %s", first)
	}
	client.waitForLogContaining("database.diagnostics.write")
	client.drainNotifications("window/logMessage")

	processClear := client.request("workspace/executeCommand", map[string]any{
		"command": "aspLsp.server.clearProcessCache",
	})
	if got := mustJSONText(t, processClear.Result); got != `{"cleared":"process","ok":true}` {
		t.Fatalf("clearProcessCache result = %s", got)
	}
	client.drainNotifications("window/logMessage")
	unchanged := workspaceDiagnosticsCacheRequest(t, client)
	if strings.Contains(unchanged, "include.missing") {
		t.Fatalf("unchanged workspace diagnostics unexpectedly reported missing include: %s", unchanged)
	}
	client.waitForLogContaining("database.diagnostics.hit")
	client.drainNotifications("window/logMessage")

	if err := os.Remove(include); err != nil {
		t.Fatal(err)
	}
	if err := client.notify("workspace/didChangeWatchedFiles", map[string]any{
		"changes": []map[string]any{{"uri": pathToFileURI(include), "type": 3}},
	}); err != nil {
		t.Fatal(err)
	}
	second := workspaceDiagnosticsCacheRequest(t, client)
	if !strings.Contains(second, "include.missing") {
		t.Fatalf("changed workspace diagnostics missing include.missing: %s", second)
	}
	client.waitForLogContaining("analysisDatabase.diagnostics.miss")
}

func TestStdioParityRestoresIncludeSummariesFromDiskCacheAfterRestart(t *testing.T) {
	root := t.TempDir()
	cacheDir := filepath.Join(root, ".cache")
	owner := filepath.Join(root, "default.asp")
	include := filepath.Join(root, "shared.inc")
	writeWorkspaceCacheFixture(t, owner, `<!-- #include file="shared.inc" -->
<%
Response.Write Sha
%>`)
	writeWorkspaceCacheFixture(t, include, `<%
Function SharedCached()
End Function
%>`)
	uri := pathToFileURI(owner)
	settings := workspaceCacheSummarySettings(cacheDir)

	firstClient := startStdioTestClient(t)
	firstClient.request("initialize", map[string]any{
		"processId":    nil,
		"rootUri":      pathToFileURI(root),
		"capabilities": map[string]any{},
	})
	notifyConfiguration(t, firstClient, settings)
	notifyOpenClassicASPDocument(t, firstClient, uri, mustReadText(t, owner))
	firstClient.request("textDocument/completion", map[string]any{
		"textDocument": map[string]any{"uri": uri},
		"position":     map[string]any{"line": 2, "character": len("Response.Write Sha")},
	})
	firstClient.waitForLogContaining("database.fileBundle.write")
	firstClient.close()

	secondClient := startStdioTestClient(t)
	defer secondClient.close()
	secondClient.request("initialize", map[string]any{
		"processId":    nil,
		"rootUri":      pathToFileURI(root),
		"capabilities": map[string]any{},
	})
	notifyConfiguration(t, secondClient, settings)
	notifyOpenClassicASPDocument(t, secondClient, uri, mustReadText(t, owner))
	completions := completionLabels(secondClient.request("textDocument/completion", map[string]any{
		"textDocument": map[string]any{"uri": uri},
		"position":     map[string]any{"line": 2, "character": len("Response.Write Sha")},
	}).Result)
	if !completions.contains("SharedCached") {
		t.Fatalf("restored include summary completions missing SharedCached: %#v", completions)
	}
	secondClient.waitForLogContaining("database.fileBundle.hit")
}

func TestStdioParityKeepsDiskRestoredIncludeSummariesIncludeAwareForInlayMarkers(t *testing.T) {
	root := t.TempDir()
	cacheDir := filepath.Join(root, ".cache")
	owner := filepath.Join(root, "default.asp")
	include := filepath.Join(root, "common.inc")
	source := `<!-- #include file="common.inc" -->
<%
implicitValue = 1
Response.Write implicitValue
%>`
	writeWorkspaceCacheFixture(t, owner, source)
	writeWorkspaceCacheFixture(t, include, "<!-- shared markup only -->")
	uri := pathToFileURI(owner)

	readHints := func() (*stdioTestClient, string) {
		t.Helper()
		client := startStdioTestClient(t)
		client.request("initialize", map[string]any{
			"processId":    nil,
			"rootUri":      pathToFileURI(root),
			"capabilities": map[string]any{},
		})
		notifyConfiguration(t, client, map[string]any{"aspLsp": map[string]any{
			"cache":       map[string]any{"enabled": true, "directory": cacheDir},
			"debug":       map[string]any{"output": "summary"},
			"diagnostics": map[string]any{"debounceMs": 0},
			"inlayHints": map[string]any{
				"functionReturnTypes": true,
				"scopeMarkers":        map[string]any{"global": true, "local": true, "uncertain": true},
				"variableTypes":       true,
			},
		}})
		notifyOpenClassicASPDocument(t, client, uri, source)
		hints := requestInlayHintsText(t, client, uri, 0, 5)
		return client, hints
	}

	warmClient, warmHints := readHints()
	if strings.Contains(warmHints, "(?)") {
		warmClient.close()
		t.Fatalf("warm inlay hints were not include-aware: %s", warmHints)
	}
	warmClient.waitForLogContaining("database.fileBundle.write")
	warmClient.close()

	restoredClient, restoredHints := readHints()
	defer restoredClient.close()
	if strings.Contains(restoredHints, "(?)") {
		t.Fatalf("restored inlay hints were not include-aware: %s", restoredHints)
	}
	restoredClient.waitForLogContaining("database.fileBundle.hit")
}

func TestStdioParityRestoresIncludeSummaryRefsFromDiskCacheAfterRestart(t *testing.T) {
	root := t.TempDir()
	cacheDir := filepath.Join(root, ".cache")
	owner := filepath.Join(root, "default.asp")
	include := filepath.Join(root, "loop.inc")
	writeWorkspaceCacheFixture(t, owner, `<!-- #include file="loop.inc" -->`)
	writeWorkspaceCacheFixture(t, include, `<!-- #include file="default.asp" -->`)
	uri := pathToFileURI(owner)
	settings := workspaceCacheSummarySettings(cacheDir)

	firstClient := startStdioTestClient(t)
	firstClient.request("initialize", map[string]any{
		"processId":    nil,
		"rootUri":      pathToFileURI(root),
		"capabilities": map[string]any{},
	})
	notifyConfiguration(t, firstClient, settings)
	notifyOpenClassicASPDocument(t, firstClient, uri, mustReadText(t, owner))
	_, firstSeen := firstClient.waitForNotificationWithSeen("textDocument/publishDiagnostics", "Include cycle detected")
	if !strings.Contains(mustJSONText(t, firstSeen), "database.fileBundle.write") {
		firstClient.waitForLogContaining("database.fileBundle.write")
	}
	firstClient.close()

	secondClient := startStdioTestClient(t)
	defer secondClient.close()
	secondClient.request("initialize", map[string]any{
		"processId":    nil,
		"rootUri":      pathToFileURI(root),
		"capabilities": map[string]any{},
	})
	notifyConfiguration(t, secondClient, settings)
	notifyOpenClassicASPDocument(t, secondClient, uri, mustReadText(t, owner))
	_, secondSeen := secondClient.waitForNotificationWithSeen("textDocument/publishDiagnostics", "Include cycle detected")
	if !strings.Contains(mustJSONText(t, secondSeen), "database.fileBundle.hit") {
		secondClient.waitForLogContaining("database.fileBundle.hit")
	}
}

func workspaceDiagnosticsCacheRequest(t *testing.T, client *stdioTestClient) string {
	t.Helper()
	response := client.request("workspace/diagnostic", map[string]any{
		"previousResultIds": []any{},
	})
	return mustJSONText(t, response.Result)
}

func workspaceCacheSummarySettings(cacheDir string) map[string]any {
	return map[string]any{"aspLsp": map[string]any{
		"debug":       map[string]any{"output": "verbose"},
		"cache":       map[string]any{"enabled": true, "directory": cacheDir},
		"diagnostics": map[string]any{"debounceMs": 0},
	}}
}

func writeWorkspaceCacheFixture(t *testing.T, path string, text string) {
	t.Helper()
	if err := os.WriteFile(path, []byte(text), 0o644); err != nil {
		t.Fatal(err)
	}
}
