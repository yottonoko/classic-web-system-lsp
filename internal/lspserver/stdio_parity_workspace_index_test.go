package lspserver

import (
	"os"
	"path/filepath"
	"strings"
	"testing"
	"time"
)

func TestStdioParityIndexesUnopenedWorkspaceASPFilesForWorkspaceSymbolsAndSupportsReindexCommand(t *testing.T) {
	client := startStdioTestClient(t)
	defer client.close()

	root := t.TempDir()
	page := filepath.Join(root, "unopened.asp")
	writeWorkspaceIndexFixture(t, page, `<%
Function IndexedTitle()
End Function
%>`)
	initializeAndWaitForWorkspaceIndex(t, client, map[string]any{
		"processId":    nil,
		"rootUri":      pathToFileURI(root),
		"capabilities": map[string]any{},
	})

	symbols := client.request("workspace/symbol", map[string]any{"query": "Indexed"})
	if !strings.Contains(mustJSONText(t, symbols.Result), "IndexedTitle") {
		t.Fatalf("workspace symbols missing IndexedTitle: %s", mustJSONText(t, symbols.Result))
	}

	writeWorkspaceIndexFixture(t, page, `<%
Function ReindexedTitle()
End Function
%>`)
	executeCommand(t, client, "aspLsp.server.reindexWorkspace", nil)
	waitForWorkspaceIndexRefresh(t, client)
	reindexed := client.request("workspace/symbol", map[string]any{"query": "Reindexed"})
	if !strings.Contains(mustJSONText(t, reindexed.Result), "ReindexedTitle") {
		t.Fatalf("workspace symbols missing ReindexedTitle after reindex: %s", mustJSONText(t, reindexed.Result))
	}
}

func initializeAndWaitForWorkspaceIndex(t *testing.T, client *stdioTestClient, params map[string]any) {
	t.Helper()
	initializeForWorkspaceIndex(t, client, params)
	notifyInitializedAndWaitForWorkspaceIndex(t, client)
}

func initializeWithConfigurationAndWaitForWorkspaceIndex(t *testing.T, client *stdioTestClient, params, settings map[string]any) {
	t.Helper()
	capabilities, _ := params["capabilities"].(map[string]any)
	if capabilities == nil {
		capabilities = map[string]any{}
		params["capabilities"] = capabilities
	}
	workspaceCapabilities, _ := capabilities["workspace"].(map[string]any)
	if workspaceCapabilities == nil {
		workspaceCapabilities = map[string]any{}
		capabilities["workspace"] = workspaceCapabilities
	}
	workspaceCapabilities["configuration"] = true
	initializeForWorkspaceIndex(t, client, params)
	if err := client.notify("initialized", map[string]any{}); err != nil {
		t.Fatal(err)
	}
	request := client.waitForServerRequest("workspace/configuration")
	client.respondToServerRequest(request, []any{settings["aspLsp"]})
	waitForWorkspaceIndexRefresh(t, client)
	waitForWorkspaceIndexRefresh(t, client)
}

func initializeForWorkspaceIndex(t *testing.T, client *stdioTestClient, params map[string]any) {
	t.Helper()
	capabilities, _ := params["capabilities"].(map[string]any)
	if capabilities == nil {
		capabilities = map[string]any{}
		params["capabilities"] = capabilities
	}
	workspaceCapabilities, _ := capabilities["workspace"].(map[string]any)
	if workspaceCapabilities == nil {
		workspaceCapabilities = map[string]any{}
		capabilities["workspace"] = workspaceCapabilities
	}
	workspaceCapabilities["semanticTokens"] = map[string]any{"refreshSupport": true}
	client.request("initialize", params)
}

func notifyInitializedAndWaitForWorkspaceIndex(t *testing.T, client *stdioTestClient) {
	t.Helper()
	if err := client.notify("initialized", map[string]any{}); err != nil {
		t.Fatal(err)
	}
	waitForWorkspaceIndexRefresh(t, client)
}

func waitForWorkspaceIndexRefresh(t *testing.T, client *stdioTestClient) {
	t.Helper()
	deadline := time.After(10 * time.Second)
	seen := []*rpcMessage{}
	defer func() {
		for _, message := range seen {
			client.notifications <- message
		}
	}()
	for {
		select {
		case message := <-client.notifications:
			if message.Method == "workspace/semanticTokens/refresh" {
				return
			}
			seen = append(seen, message)
		case <-deadline:
			t.Fatal("timed out waiting for workspace index refresh")
		}
	}
}

func TestStdioParityHonorsWorkspaceIncludeExcludeGlobsAndGitIgnoreForWorkspaceAnalysis(t *testing.T) {
	client := startStdioTestClient(t)
	defer client.close()

	root := t.TempDir()
	appDir := filepath.Join(root, "app")
	generatedDir := filepath.Join(appDir, "generated")
	legacyDir := filepath.Join(root, "legacy")
	ignoredDir := filepath.Join(root, "ignored")
	for _, dir := range []string{generatedDir, legacyDir, ignoredDir} {
		if err := os.MkdirAll(dir, 0o755); err != nil {
			t.Fatal(err)
		}
	}
	writeWorkspaceIndexFixture(t, filepath.Join(root, ".gitignore"), "ignored/\n")
	writeWorkspaceIndexFixture(t, filepath.Join(appDir, "default.asp"), `<%
Function IncludedTitle()
End Function
%>`)
	writeWorkspaceIndexFixture(t, filepath.Join(generatedDir, "generated.asp"), `<%
Function GeneratedTitle()
End Function
%>`)
	writeWorkspaceIndexFixture(t, filepath.Join(legacyDir, "legacy.asp"), `<%
Function LegacyTitle()
End Function
%>`)
	writeWorkspaceIndexFixture(t, filepath.Join(ignoredDir, "ignored.asp"), `<%
Function IgnoredTitle()
End Function
%>`)
	initializeWithConfigurationAndWaitForWorkspaceIndex(t, client, map[string]any{
		"processId":    nil,
		"rootUri":      pathToFileURI(root),
		"capabilities": map[string]any{},
	}, map[string]any{"aspLsp": map[string]any{
		"workspace": map[string]any{
			"includes":         []string{"app/**/*.asp", "ignored/**/*.asp"},
			"excludes":         []string{"app/generated/**"},
			"respectGitIgnore": true,
		},
	}})

	symbols := client.request("workspace/symbol", map[string]any{"query": "Title"})
	serialized := mustJSONText(t, symbols.Result)
	if !strings.Contains(serialized, "IncludedTitle") {
		t.Fatalf("workspace symbols missing IncludedTitle: %s", serialized)
	}
	for _, unexpected := range []string{"GeneratedTitle", "LegacyTitle", "IgnoredTitle"} {
		if strings.Contains(serialized, unexpected) {
			t.Fatalf("workspace symbols included %s despite workspace patterns: %s", unexpected, serialized)
		}
	}
}

func writeWorkspaceIndexFixture(t *testing.T, path string, text string) {
	t.Helper()
	if err := os.WriteFile(path, []byte(text), 0o644); err != nil {
		t.Fatal(err)
	}
}
