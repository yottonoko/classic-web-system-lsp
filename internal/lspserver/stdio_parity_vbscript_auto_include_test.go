package lspserver

import (
	"os"
	"path/filepath"
	"strings"
	"testing"
)

func TestStdioParityAddsWorkspaceVBScriptAutoIncludeCompletionEditsAndCodeActions(t *testing.T) {
	client := startStdioTestClient(t)
	defer client.close()

	root := t.TempDir()
	helper := filepath.Join(root, "includes", "helpers.inc")
	pageProvider := filepath.Join(root, "shared.asp")
	if err := os.MkdirAll(filepath.Dir(helper), 0o755); err != nil {
		t.Fatal(err)
	}
	if err := os.WriteFile(helper, []byte(`<%
Public Function SharedHelper()
End Function
Const SharedConstant = 1
Dim SharedVariable
Class SharedClass
End Class
%>`), 0o644); err != nil {
		t.Fatal(err)
	}
	if err := os.WriteFile(pageProvider, []byte(`<% Public Function SharedPageFunction(): End Function %>`), 0o644); err != nil {
		t.Fatal(err)
	}

	marked := markedDocument(`<%@ Language="VBScript" %>
<%
Response.Write Shared<<<caret>>>
%>`)
	owner := filepath.Join(root, "default.asp")
	uri := pathToFileURI(owner)
	initializeWithConfigurationAndWaitForWorkspaceIndex(t, client, map[string]any{
		"processId":    nil,
		"rootUri":      pathToFileURI(root),
		"capabilities": map[string]any{},
	}, map[string]any{"aspLsp": map[string]any{
		"vbscript": map[string]any{"autoIncludes": true},
	}})
	openClassicASPDocumentWithDiagnostics(t, client, uri, marked.Text)

	completions := completionItems(client.request("textDocument/completion", map[string]any{
		"textDocument": map[string]any{"uri": uri},
		"position":     marked.Position,
	}).Result)
	item, ok := completions.find("SharedHelper")
	if !ok || !strings.Contains(item.Detail, "includes/helpers.inc") {
		t.Fatalf("workspace auto-include completion = %#v", completions)
	}
	resolved := mustJSONText(t, client.request("completionItem/resolve", item).Result)
	for _, expected := range []string{"additionalTextEdits", `#include file=\"includes/helpers.inc\"`, "Defined in [includes/helpers.inc]"} {
		if !strings.Contains(resolved, expected) {
			t.Fatalf("resolved auto-include completion missing %q: %s", expected, resolved)
		}
	}
	if !completions.hasLabel("SharedPageFunction") {
		t.Fatalf("workspace .asp provider completion missing: %#v", completions)
	}
	for _, expected := range []string{"SharedConstant", "SharedVariable", "SharedClass"} {
		if !completions.hasLabel(expected) {
			t.Fatalf("workspace VBScript export completion missing %s: %#v", expected, completions)
		}
	}
	if !completionItems(client.request("textDocument/completion", map[string]any{
		"textDocument": map[string]any{"uri": uri},
		"position":     marked.Position,
	}).Result).hasLabel("SharedHelper") {
		t.Fatal("auto-include completion disappeared on repeated request")
	}

	call := markedDocument(`<%
Response.Write SharedHelper<<<caret>>>()
%>`)
	if err := client.notify("textDocument/didChange", map[string]any{
		"textDocument":   map[string]any{"uri": uri, "version": 2},
		"contentChanges": []map[string]any{{"text": call.Text}},
	}); err != nil {
		t.Fatal(err)
	}
	client.waitForNotification("textDocument/publishDiagnostics", uri)
	actions := mustJSONText(t, client.request("textDocument/codeAction", map[string]any{
		"textDocument": map[string]any{"uri": uri},
		"range":        mapPositionRange(call.Position, call.Position),
		"context":      map[string]any{"diagnostics": []any{}, "only": []string{"quickfix"}},
	}).Result)
	for _, expected := range []string{"Include includes/helpers.inc for SharedHelper", `#include file=\"includes/helpers.inc\"`} {
		if !strings.Contains(actions, expected) {
			t.Fatalf("selection auto-include action missing %q: %s", expected, actions)
		}
	}

	diagnosticCall := markedDocument(`<%
Option Explicit
Response.Write sharedHelper<<<caret>>>()
%>`)
	if err := client.notify("textDocument/didChange", map[string]any{
		"textDocument":   map[string]any{"uri": uri, "version": 3},
		"contentChanges": []map[string]any{{"text": diagnosticCall.Text}},
	}); err != nil {
		t.Fatal(err)
	}
	diagnosticMessage := client.waitForNotification("textDocument/publishDiagnostics", uri)
	diagnostics := diagnosticsFromPublishMessage(t, diagnosticMessage)
	diagnosticActions := mustJSONText(t, client.request("textDocument/codeAction", map[string]any{
		"textDocument": map[string]any{"uri": uri},
		"range":        mapPositionRange(diagnosticCall.Position, diagnosticCall.Position),
		"context":      map[string]any{"diagnostics": diagnostics, "only": []string{"quickfix"}},
	}).Result)
	for _, expected := range []string{"Include includes/helpers.inc for SharedHelper", `"diagnostics"`} {
		if !strings.Contains(diagnosticActions, expected) {
			t.Fatalf("diagnostic auto-include action missing %q: %s", expected, diagnosticActions)
		}
	}
}

func TestStdioParitySuppressesCycleFormingVBScriptAutoIncludeCandidates(t *testing.T) {
	client := startStdioTestClient(t)
	defer client.close()

	root := t.TempDir()
	owner := filepath.Join(root, "default.asp")
	helper := filepath.Join(root, "helpers.inc")
	source := `<%
SharedUnsafe<<<caret>>>
%>`
	marked := markedDocument(source)
	if err := os.WriteFile(owner, []byte(marked.Text), 0o644); err != nil {
		t.Fatal(err)
	}
	if err := os.WriteFile(helper, []byte(`<!-- #include file="default.asp" -->
<% Public Function SharedUnsafe(): End Function %>`), 0o644); err != nil {
		t.Fatal(err)
	}
	uri := pathToFileURI(owner)
	initializeWithConfigurationAndWaitForWorkspaceIndex(t, client, map[string]any{
		"processId":    nil,
		"rootUri":      pathToFileURI(root),
		"capabilities": map[string]any{},
	}, map[string]any{"aspLsp": map[string]any{
		"vbscript": map[string]any{"autoIncludes": true},
	}})
	openClassicASPDocumentWithDiagnostics(t, client, uri, marked.Text)
	if completions := completionLabels(client.request("textDocument/completion", map[string]any{
		"textDocument": map[string]any{"uri": uri},
		"position":     marked.Position,
	}).Result); completions.contains("SharedUnsafe") {
		t.Fatalf("cycle-forming auto-include candidate was offered: %#v", completions)
	}
}

func TestStdioParityKeepsAlreadyIncludedVBScriptSymbolsAheadOfAutoIncludeCandidates(t *testing.T) {
	client := startStdioTestClient(t)
	defer client.close()

	root := t.TempDir()
	helper := filepath.Join(root, "helpers.inc")
	if err := os.WriteFile(helper, []byte(`<% Public Function SharedVisible(): End Function %>`), 0o644); err != nil {
		t.Fatal(err)
	}
	source := markedDocument(`<!-- #include file="helpers.inc" -->
<%
SharedVis<<<caret>>>
%>`)
	owner := filepath.Join(root, "default.asp")
	if err := os.WriteFile(owner, []byte(source.Text), 0o644); err != nil {
		t.Fatal(err)
	}
	uri := pathToFileURI(owner)
	initializeWithConfigurationAndWaitForWorkspaceIndex(t, client, map[string]any{
		"processId":    nil,
		"rootUri":      pathToFileURI(root),
		"capabilities": map[string]any{},
	}, map[string]any{"aspLsp": map[string]any{
		"vbscript": map[string]any{"autoIncludes": true},
	}})
	openClassicASPDocumentWithDiagnostics(t, client, uri, source.Text)
	item, ok := completionItems(client.request("textDocument/completion", map[string]any{
		"textDocument": map[string]any{"uri": uri},
		"position":     source.Position,
	}).Result).find("SharedVisible")
	if !ok || item.Detail != "VBScript" {
		t.Fatalf("already included symbol should stay ordinary completion: %#v", item)
	}
}
