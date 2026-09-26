package lspserver

import (
	"os"
	"path/filepath"
	"strings"
	"testing"
)

func TestStdioParityReturnsGoCustomCommandPayloads(t *testing.T) {
	client := startStdioTestClient(t)
	defer client.close()

	root := t.TempDir()
	uri := pathToFileURI(filepath.Join(root, "go-command-payloads.asp"))
	source := `<%
Function BuildName()
End Function
Response.Write BuildName()
%>`
	client.request("initialize", map[string]any{
		"processId":    nil,
		"rootUri":      pathToFileURI(root),
		"capabilities": map[string]any{},
	})
	notifyOpenClassicASPDocument(t, client, uri, source)

	expectCommandPayload(t, client, "aspLsp.server.reindexWorkspace", `{"ok":true}`)
	expectCommandPayload(t, client, "aspLsp.server.clearProcessCache", `{"cleared":"process","ok":true}`)
	expectCommandPayload(t, client, "aspLsp.server.clearDiskCache", `{"cleared":"disk","ok":true}`)
	expectCommandPayload(t, client, "aspLsp.server.clearCache", `{"cleared":"all","ok":true}`)

	graph := client.request("workspace/executeCommand", map[string]any{
		"command":   "aspLsp.server.buildGraph",
		"arguments": []map[string]any{{"uri": uri}},
	})
	if !strings.Contains(mustJSONText(t, graph.Result), "BuildName") {
		t.Fatalf("custom command graph missing BuildName: %s", mustJSONText(t, graph.Result))
	}
	flowchart := buildFlowchart(t, client, map[string]any{"uri": uri, "labelMode": "raw"})
	if flowchart["uri"] != uri ||
		flowchart["fileName"] != "go-command-payloads.asp" ||
		flowchart["labelMode"] != "raw" ||
		!strings.Contains(mustJSONText(t, flowchart), "flowchart TB") {
		t.Fatalf("custom command flowchart mismatch: %s", mustJSONText(t, flowchart))
	}
	navigation := buildNavigationGraph(t, client, map[string]any{"scope": "document", "uri": uri})
	if navigation["scope"] != "document" ||
		navigation["uri"] != uri ||
		len(navigationList(navigation["edges"])) != 0 {
		t.Fatalf("custom command navigation mismatch: %s", mustJSONText(t, navigation))
	}
	preview := executeCommand(t, client, "aspLsp.server.previewWorkspaceFiles", []map[string]any{{}})
	previewText := mustJSONText(t, preview)
	if !strings.Contains(previewText, `"stats"`) || strings.Contains(previewText, `"truncated"`) {
		t.Fatalf("custom command preview payload mismatch: %s", previewText)
	}
	unknown := executeCommand(t, client, "aspLsp.unknown", nil)
	if !strings.Contains(mustJSONText(t, unknown), "Unknown command") {
		t.Fatalf("unknown command payload mismatch: %s", mustJSONText(t, unknown))
	}
}

func TestStdioParityPreviewsWorkspaceFilesWithUnmatchedVisibility(t *testing.T) {
	client := startStdioTestClient(t)
	defer client.close()

	root := t.TempDir()
	for _, dir := range []string{"includes", "legacy"} {
		if err := os.MkdirAll(filepath.Join(root, dir), 0o755); err != nil {
			t.Fatal(err)
		}
	}
	writeWorkspaceGraphFixture(t, filepath.Join(root, "default.asp"), `<%
Response.Write "ok"
%>`)
	writeWorkspaceGraphFixture(t, filepath.Join(root, "includes", "common.inc"), `<%
Const CommonValue = 1
%>`)
	writeWorkspaceGraphFixture(t, filepath.Join(root, "legacy", "old.asp"), `<%
Response.Write "old"
%>`)
	writeWorkspaceGraphFixture(t, filepath.Join(root, "notes.txt"), "ignored")
	client.request("initialize", map[string]any{
		"processId":    nil,
		"rootUri":      pathToFileURI(root),
		"capabilities": map[string]any{},
	})

	visible := executeCommand(t, client, "aspLsp.server.previewWorkspaceFiles", []map[string]any{{
		"includeGlobs":     []string{"**/*.asp"},
		"excludeGlobs":     []string{"legacy/**"},
		"respectGitIgnore": false,
		"showUnmatched":    true,
		"maxFiles":         64,
	}})
	visibleText := mustJSONText(t, visible)
	if !strings.Contains(visibleText, `"showUnmatched":true`) ||
		!strings.Contains(visibleText, `"files":1`) ||
		!previewHasFile(visible, "default.asp", true) ||
		!previewHasFile(visible, "includes/common.inc", false) ||
		!previewHasFile(visible, "legacy/old.asp", false) ||
		strings.Contains(visibleText, "notes.txt") {
		t.Fatalf("visible preview payload mismatch: %s", visibleText)
	}

	matchedOnly := executeCommand(t, client, "aspLsp.server.previewWorkspaceFiles", []map[string]any{{
		"includeGlobs":     []string{"**/*.asp"},
		"excludeGlobs":     []string{"legacy/**"},
		"respectGitIgnore": false,
		"showUnmatched":    false,
		"maxFiles":         64,
	}})
	if !strings.Contains(mustJSONText(t, matchedOnly), `"showUnmatched":false`) ||
		!previewHasFile(matchedOnly, "default.asp", true) ||
		previewHasFile(matchedOnly, "includes/common.inc", false) ||
		previewHasFile(matchedOnly, "legacy/old.asp", false) {
		t.Fatalf("matched-only preview payload mismatch: %s", mustJSONText(t, matchedOnly))
	}
}

func expectCommandPayload(t *testing.T, client *stdioTestClient, command string, expected string) {
	t.Helper()
	if got := mustJSONText(t, executeCommand(t, client, command, nil)); got != expected {
		t.Fatalf("%s result = %s, want %s", command, got, expected)
	}
}

func executeCommand(t *testing.T, client *stdioTestClient, command string, arguments any) any {
	t.Helper()
	params := map[string]any{"command": command}
	if arguments != nil {
		params["arguments"] = arguments
	}
	return client.request("workspace/executeCommand", params).Result
}

func navigationList(value any) []any {
	list, _ := value.([]any)
	return list
}

func previewHasFile(payload any, relativePath string, matchesFilter bool) bool {
	object, _ := payload.(map[string]any)
	roots, _ := object["roots"].([]any)
	for _, rootItem := range roots {
		root, _ := rootItem.(map[string]any)
		files, _ := root["files"].([]any)
		for _, fileItem := range files {
			file, _ := fileItem.(map[string]any)
			if file["relativePath"] == relativePath && file["matchesFilter"] == matchesFilter {
				return true
			}
		}
	}
	return false
}
