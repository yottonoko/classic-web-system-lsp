package lspserver

import (
	"path/filepath"
	"strings"
	"testing"
)

func TestStdioParityRefreshesWorkspaceConfigurationAfterInitialized(t *testing.T) {
	client := startStdioTestClient(t)
	defer client.close()

	root := t.TempDir()
	client.request("initialize", map[string]any{
		"processId": nil,
		"rootUri":   pathToFileURI(root),
		"capabilities": map[string]any{
			"workspace": map[string]any{"configuration": true},
		},
	})
	if err := client.notify("initialized", map[string]any{}); err != nil {
		t.Fatal(err)
	}
	request := client.waitForServerRequest("workspace/configuration")
	if got := string(request.Params); got != `{"items":[{"section":"aspLsp"}]}` {
		t.Fatalf("workspace/configuration params = %s", got)
	}
	client.respondToServerRequest(request, []any{map[string]any{
		"defaultLanguage": "JScript",
		"diagnostics":     map[string]any{"debounceMs": 0},
	}})

	uri := pathToFileURI(filepath.Join(root, "initialized-config.asp"))
	source := `<% con %>`
	notifyOpenClassicASPDocument(t, client, uri, source)
	completion := client.request("textDocument/completion", map[string]any{
		"textDocument": map[string]any{"uri": uri},
		"position":     positionAt(source, strings.Index(source, "con")+len("con")),
	})
	completionText := mustJSONText(t, completion.Result)
	if !strings.Contains(completionText, `"label":"const"`) {
		t.Fatalf("initialized configuration did not select JScript: %s", completionText)
	}
}

func TestStdioParityExitCancelsPendingWorkspaceConfigurationRequest(t *testing.T) {
	client := startStdioTestClient(t)
	client.request("initialize", map[string]any{
		"processId": nil,
		"rootUri":   pathToFileURI(t.TempDir()),
		"capabilities": map[string]any{
			"workspace": map[string]any{"configuration": true},
		},
	})
	if err := client.notify("initialized", map[string]any{}); err != nil {
		t.Fatal(err)
	}
	client.waitForServerRequest("workspace/configuration")
	client.close()
}

func TestStdioParityWillSaveValidatesPendingDocumentChanges(t *testing.T) {
	client := startStdioTestClient(t)
	defer client.close()

	root := t.TempDir()
	uri := pathToFileURI(filepath.Join(root, "will-save.asp"))
	initial := `<%
Option Explicit
Dim known
Response.Write known
%>`
	changed := `<%
Option Explicit
Response.Write missingName
%>`
	client.request("initialize", map[string]any{
		"processId":    nil,
		"rootUri":      pathToFileURI(root),
		"capabilities": map[string]any{},
	})
	notifyConfiguration(t, client, map[string]any{"aspLsp": map[string]any{
		"diagnostics": map[string]any{"debounceMs": 60_000},
	}})
	notifyOpenClassicASPDocument(t, client, uri, initial)
	client.waitForNotification("textDocument/publishDiagnostics", "")
	if err := client.notify("textDocument/didChange", map[string]any{
		"textDocument": map[string]any{"uri": uri, "version": 2},
		"contentChanges": []map[string]any{{
			"text": changed,
		}},
	}); err != nil {
		t.Fatal(err)
	}
	if err := client.notify("textDocument/willSave", map[string]any{
		"textDocument": map[string]any{"uri": uri},
		"reason":       1,
	}); err != nil {
		t.Fatal(err)
	}
	diagnostics := client.waitForNotification("textDocument/publishDiagnostics", "missingName")
	if !strings.Contains(string(diagnostics.Params), "asp-lsp-vbscript") {
		t.Fatalf("willSave diagnostics mismatch: %s", diagnostics.Params)
	}
}
