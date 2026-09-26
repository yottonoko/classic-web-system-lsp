package lspserver

import (
	"encoding/json"
	"os"
	"path/filepath"
	"strings"
	"testing"
)

func TestStdioParityInitializeCapabilities(t *testing.T) {
	client := startStdioTestClient(t)
	defer client.close()

	initialize := client.request("initialize", map[string]any{
		"processId": nil,
		"rootUri":   "file:///tmp/asp-lsp-go-parity",
		"capabilities": map[string]any{
			"textDocument": map[string]any{},
			"workspace":    map[string]any{},
		},
	})
	text := mustJSONText(t, initialize.Result)
	for _, expected := range []string{
		"asp-lsp-go",
		"completionProvider",
		"signatureHelpProvider",
		"selectionRangeProvider",
		"inlayHintProvider",
		"callHierarchyProvider",
		"typeHierarchyProvider",
		"monikerProvider",
		"inlineValueProvider",
		"willSaveWaitUntil",
		"aspLsp.server.reindexWorkspace",
		"aspLsp.server.clearCache",
		"aspLsp.server.clearDiskCache",
		"aspLsp.server.clearProcessCache",
		"aspLsp.server.buildFlowchart",
		"aspLsp.server.buildNavigationGraph",
	} {
		if !strings.Contains(text, expected) {
			t.Fatalf("initialize result missing %q: %s", expected, text)
		}
	}
	var initializeResult struct {
		Capabilities struct {
			CompletionProvider struct {
				TriggerCharacters []string `json:"triggerCharacters"`
			} `json:"completionProvider"`
			SignatureHelpProvider struct {
				TriggerCharacters []string `json:"triggerCharacters"`
			} `json:"signatureHelpProvider"`
			InlayHintProvider struct {
				ResolveProvider *bool `json:"resolveProvider"`
			} `json:"inlayHintProvider"`
		} `json:"capabilities"`
	}
	mustDecodeResult(t, initialize.Result, &initializeResult)
	for _, expected := range []string{"<", ".", ";"} {
		if !stringSliceContains(initializeResult.Capabilities.CompletionProvider.TriggerCharacters, expected) {
			t.Fatalf("completion triggerCharacters missing %q: %s", expected, text)
		}
	}
	if stringSliceContains(initializeResult.Capabilities.CompletionProvider.TriggerCharacters, " ") {
		t.Fatalf("completion triggerCharacters unexpectedly contains space: %s", text)
	}
	if !stringSliceContains(initializeResult.Capabilities.SignatureHelpProvider.TriggerCharacters, " ") {
		t.Fatalf("signature triggerCharacters missing space: %s", text)
	}
	resolveProvider := initializeResult.Capabilities.InlayHintProvider.ResolveProvider
	if resolveProvider == nil || *resolveProvider {
		t.Fatalf("inlay hint resolveProvider should be explicitly false: %s", text)
	}
}

func TestStdioParityNormalizesWindowsFileURIIdentityAcrossDocumentChanges(t *testing.T) {
	client := startStdioTestClient(t)
	defer client.close()

	upperURI := "file:///C:/site/default.asp"
	lowerURI := "file:///c:/site/default.asp"
	escapedURI := "file:///c%3A/site/default.asp"
	initialSource := `<%
Response.Write 1
%>`
	lowerSource := `<%
Function AddedName()
End Function
Response.Write AddedName()
%>`
	escapedSource := `<%
Function EscapedName()
End Function
Response.Write EscapedName()
%>`
	client.request("initialize", map[string]any{
		"processId":    nil,
		"rootUri":      "file:///C:/site",
		"capabilities": map[string]any{},
	})
	notifyOpenClassicASPDocument(t, client, upperURI, initialSource)
	client.waitForNotification("textDocument/publishDiagnostics", "")

	if err := client.notify("textDocument/didChange", map[string]any{
		"textDocument":   map[string]any{"uri": lowerURI, "version": 2},
		"contentChanges": []map[string]any{{"text": lowerSource}},
	}); err != nil {
		t.Fatal(err)
	}
	client.waitForNotification("textDocument/publishDiagnostics", "")
	addedDefinition := client.request("textDocument/definition", map[string]any{
		"textDocument": map[string]any{"uri": upperURI},
		"position":     positionAt(lowerSource, strings.LastIndex(lowerSource, "AddedName")+2),
	})
	if !strings.Contains(mustJSONText(t, addedDefinition.Result), `"line":1`) {
		t.Fatalf("definition after lower-case URI change mismatch: %s", mustJSONText(t, addedDefinition.Result))
	}

	if err := client.notify("textDocument/didChange", map[string]any{
		"textDocument":   map[string]any{"uri": escapedURI, "version": 3},
		"contentChanges": []map[string]any{{"text": escapedSource}},
	}); err != nil {
		t.Fatal(err)
	}
	client.waitForNotification("textDocument/publishDiagnostics", "")
	escapedDefinition := client.request("textDocument/definition", map[string]any{
		"textDocument": map[string]any{"uri": lowerURI},
		"position":     positionAt(escapedSource, strings.LastIndex(escapedSource, "EscapedName")+2),
	})
	if !strings.Contains(mustJSONText(t, escapedDefinition.Result), `"line":1`) {
		t.Fatalf("definition after escaped URI change mismatch: %s", mustJSONText(t, escapedDefinition.Result))
	}
}

func TestStdioParityDiagnosticsAndCompletion(t *testing.T) {
	client := startStdioTestClient(t)
	defer client.close()

	root := t.TempDir()
	uri := pathToFileURI(filepath.Join(root, "default.asp"))
	client.request("initialize", map[string]any{
		"processId":    nil,
		"rootUri":      pathToFileURI(root),
		"capabilities": map[string]any{},
	})
	if err := client.notify("textDocument/didOpen", map[string]any{
		"textDocument": map[string]any{
			"uri":        uri,
			"languageId": "classic-asp",
			"version":    1,
			"text":       "<% Option Explicit\nResponse.\nResponse.Write missingName\n%>",
		},
	}); err != nil {
		t.Fatal(err)
	}
	diagnostics := client.waitForNotification("textDocument/publishDiagnostics", "missingName")
	if !strings.Contains(string(diagnostics.Params), uri) {
		t.Fatalf("diagnostics did not include uri %q: %s", uri, diagnostics.Params)
	}

	completion := client.request("textDocument/completion", map[string]any{
		"textDocument": map[string]any{"uri": uri},
		"position":     map[string]any{"line": 1, "character": len("Response.")},
	})
	text := mustJSONText(t, completion.Result)
	if !strings.Contains(text, `"Write"`) {
		t.Fatalf("completion result missing Response.Write: %s", text)
	}
}

func TestStdioParityVBScriptDiagnosticsWorkspaceDiagnosticsAndCompletion(t *testing.T) {
	client := startStdioTestClient(t)
	defer client.close()

	root := t.TempDir()
	uri := pathToFileURI(filepath.Join(root, "default.asp"))
	client.request("initialize", map[string]any{
		"processId":    nil,
		"rootUri":      pathToFileURI(root),
		"capabilities": map[string]any{},
	})
	if err := client.notify("textDocument/didOpen", map[string]any{
		"textDocument": map[string]any{
			"uri":        uri,
			"languageId": "classic-asp",
			"version":    1,
			"text":       "<%\nResponse.",
		},
	}); err != nil {
		t.Fatal(err)
	}
	client.waitForNotification("textDocument/publishDiagnostics", "Classic ASP block is missing")

	workspaceDiagnostics := client.request("workspace/diagnostic", map[string]any{
		"previousResultIds": []any{},
	})
	workspaceText := mustJSONText(t, workspaceDiagnostics.Result)
	for _, expected := range []string{uri, "Classic ASP block is missing"} {
		if !strings.Contains(workspaceText, expected) {
			t.Fatalf("workspace diagnostics missing %q: %s", expected, workspaceText)
		}
	}

	completion := client.request("textDocument/completion", map[string]any{
		"textDocument": map[string]any{"uri": uri},
		"position":     map[string]any{"line": 1, "character": len("Response.")},
	})
	if !completionLabels(completion.Result).contains("Write") {
		t.Fatalf("completion result missing Write: %s", mustJSONText(t, completion.Result))
	}
}

func TestStdioParityStatusNotifications(t *testing.T) {
	client := startStdioTestClient(t)
	defer client.close()

	root := t.TempDir()
	uri := pathToFileURI(filepath.Join(root, "status.asp"))
	client.request("initialize", map[string]any{
		"processId":    nil,
		"rootUri":      pathToFileURI(root),
		"capabilities": map[string]any{},
	})
	if err := client.notify("textDocument/didOpen", map[string]any{
		"textDocument": map[string]any{
			"uri":        uri,
			"languageId": "classic-asp",
			"version":    1,
			"text":       `<% Dim value` + "\n" + `value = "ok" %>`,
		},
	}); err != nil {
		t.Fatal(err)
	}

	analyzing := client.waitForNotification("aspLsp/status", `"analyzing"`)
	var analyzingParams struct {
		Status   string `json:"status"`
		Progress struct {
			Current *int `json:"current"`
			Total   *int `json:"total"`
		} `json:"progress"`
		Tasks []struct {
			ID        string `json:"id"`
			Current   *int   `json:"current"`
			Total     *int   `json:"total"`
			UpdatedAt *int64 `json:"updatedAt"`
		} `json:"tasks"`
	}
	if err := json.Unmarshal(analyzing.Params, &analyzingParams); err != nil {
		t.Fatalf("decode analyzing status: %v; params=%s", err, analyzing.Params)
	}
	if analyzingParams.Status != "analyzing" ||
		analyzingParams.Progress.Current == nil ||
		analyzingParams.Progress.Total == nil ||
		len(analyzingParams.Tasks) == 0 ||
		analyzingParams.Tasks[0].ID == "" ||
		analyzingParams.Tasks[0].Current == nil ||
		analyzingParams.Tasks[0].Total == nil ||
		analyzingParams.Tasks[0].UpdatedAt == nil {
		t.Fatalf("analyzing status missing tasks: %s", analyzing.Params)
	}
	client.waitForNotification("textDocument/publishDiagnostics", uri)
	idle := client.waitForNotification("aspLsp/status", `"idle"`)
	if !strings.Contains(string(idle.Params), `"tasks":[]`) {
		t.Fatalf("idle status did not clear tasks: %s", idle.Params)
	}
}

func TestStdioParityWorkspaceDiagnosticsForUnopenedFiles(t *testing.T) {
	root := t.TempDir()
	broken := filepath.Join(root, "broken.asp")
	if err := os.WriteFile(broken, []byte(`<style>.x { color: }</style>`), 0o644); err != nil {
		t.Fatal(err)
	}
	client := startStdioTestClient(t)
	defer client.close()

	initializeAndWaitForWorkspaceIndex(t, client, map[string]any{
		"processId":    nil,
		"rootUri":      pathToFileURI(root),
		"capabilities": map[string]any{},
	})
	diagnostics := client.request("workspace/diagnostic", map[string]any{
		"previousResultIds": []any{},
	})
	text := mustJSONText(t, diagnostics.Result)
	for _, expected := range []string{"broken.asp", "asp-lsp-css"} {
		if !strings.Contains(text, expected) {
			t.Fatalf("workspace diagnostics missing %q: %s", expected, text)
		}
	}
}

func TestStdioParityOpenDocumentsFirstForWorkspaceDiagnostics(t *testing.T) {
	root := t.TempDir()
	openFile := filepath.Join(root, "open.asp")
	indexedFile := filepath.Join(root, "indexed.asp")
	if err := os.WriteFile(openFile, []byte(`<style>.open { color: }</style>`), 0o644); err != nil {
		t.Fatal(err)
	}
	if err := os.WriteFile(indexedFile, []byte(`<style>.indexed { color: }</style>`), 0o644); err != nil {
		t.Fatal(err)
	}
	openURI := pathToFileURI(openFile)
	client := startStdioTestClient(t)
	defer client.close()

	initializeAndWaitForWorkspaceIndex(t, client, map[string]any{
		"processId":    nil,
		"rootUri":      pathToFileURI(root),
		"capabilities": map[string]any{},
	})
	if err := client.notify("textDocument/didOpen", map[string]any{
		"textDocument": map[string]any{
			"uri":        openURI,
			"languageId": "classic-asp",
			"version":    1,
			"text":       mustReadText(t, openFile),
		},
	}); err != nil {
		t.Fatal(err)
	}

	diagnostics := client.request("workspace/diagnostic", map[string]any{
		"previousResultIds": []any{},
	})
	var result struct {
		Items []struct {
			URI string `json:"uri"`
		} `json:"items"`
	}
	mustDecodeResult(t, diagnostics.Result, &result)
	if len(result.Items) == 0 || result.Items[0].URI != openURI {
		t.Fatalf("workspace diagnostics did not return open document first: %s", mustJSONText(t, diagnostics.Result))
	}
	if !strings.Contains(mustJSONText(t, diagnostics.Result), "indexed.asp") {
		t.Fatalf("workspace diagnostics missing indexed file: %s", mustJSONText(t, diagnostics.Result))
	}
}
