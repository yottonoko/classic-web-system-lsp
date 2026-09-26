package lspserver

import (
	"bytes"
	"context"
	"path/filepath"
	"strings"
	"testing"
	"time"
)

func TestStdioParityGraphIncludesEveryDocumentDespiteLegacyLimitArguments(t *testing.T) {
	client := startStdioTestClient(t)
	defer client.close()
	root := t.TempDir()
	page := filepath.Join(root, "page.asp")
	writeGraphSettingsFixture(t, page, `<!-- #include file="one.inc" -->
<!-- #include file="two.inc" -->
<% Response.Write "page" %>`)
	writeGraphSettingsFixture(t, filepath.Join(root, "one.inc"), `<% Response.Write "one" %>`)
	writeGraphSettingsFixture(t, filepath.Join(root, "two.inc"), `<% Response.Write "two" %>`)
	client.request("initialize", map[string]any{
		"processId": nil, "rootUri": pathToFileURI(root), "capabilities": map[string]any{},
	})
	payload := buildDocumentGraph(t, client, pathToFileURI(page), map[string]any{
		"maxDocuments": 2, "maxTextLength": 1_000,
		"includeTreeMaxDocuments": 2, "includeTreeMaxTextLength": 1_000,
	})
	for _, label := range []string{"page.asp", "one.inc", "two.inc"} {
		if !graphPayloadHasLabel(payload, label) {
			t.Fatalf("graph payload omitted %s: %s", label, mustJSONText(t, payload))
		}
	}
}

func TestStdioParityNavigationIncludesEveryDocumentDespiteLegacyLimitArguments(t *testing.T) {
	client := startStdioTestClient(t)
	defer client.close()
	root := t.TempDir()
	page := filepath.Join(root, "page.asp")
	writeFlowchartFixture(t, page, `<a href="one.asp">one</a>
<a href="two.asp">two</a>`)
	writeFlowchartFixture(t, filepath.Join(root, "one.asp"), "")
	writeFlowchartFixture(t, filepath.Join(root, "two.asp"), "")
	initializeAndWaitForWorkspaceIndex(t, client, map[string]any{
		"processId": nil, "rootUri": pathToFileURI(root), "capabilities": map[string]any{},
	})
	payload := buildNavigationGraph(t, client, map[string]any{
		"scope": "workspace", "maxDocuments": 1, "maxTextLength": 1_000,
	})
	stats := payload["stats"].(map[string]any)
	if stats["documents"].(float64) != 3 {
		t.Fatalf("navigation document count = %v, want 3", stats["documents"])
	}
}

func TestStdioParityUsesFlowchartLabelLineLengthArgument(t *testing.T) {
	client := startStdioTestClient(t)
	defer client.close()
	root := t.TempDir()
	page := filepath.Join(root, "flow.asp")
	uri := pathToFileURI(page)
	writeFlowchartFixture(t, page, `<%
Sub Main()
  value = "one two three four five six seven eight nine ten"
End Sub
%>`)
	client.request("initialize", map[string]any{
		"processId": nil, "rootUri": pathToFileURI(root), "capabilities": map[string]any{},
	})
	payload := buildFlowchart(t, client, map[string]any{"uri": uri, "labelLineLength": 12})
	settings := payload["settings"].(map[string]any)
	if settings["labelLineLength"] != float64(12) {
		t.Fatalf("flowchart labelLineLength = %#v, want 12", settings["labelLineLength"])
	}
	longLabelFound := false
	for _, value := range payload["nodes"].([]any) {
		node := value.(map[string]any)
		if utf16Len(asString(node["label"])) > 12 {
			longLabelFound = true
		}
	}
	if !longLabelFound || !strings.Contains(asString(payload["mermaid"]), "<br/>") {
		t.Fatalf("flowchart label was not preserved and visually wrapped: %s", mustJSONText(t, payload))
	}
}

func TestCancelProgressTaskCancelsBackgroundGraphContext(t *testing.T) {
	server := New(strings.NewReader(""), bytes.NewBuffer(nil), nil)
	ctx, cancel := context.WithCancel(context.Background())
	server.mu.Lock()
	server.graphBackgroundTasks["graph-task"] = cancel
	server.mu.Unlock()
	result := server.cancelProgressTask(executeCommandParams{
		Arguments: []any{map[string]any{"id": "graph-task"}},
	})
	if result["ok"] != true {
		t.Fatalf("cancelProgressTask result = %#v", result)
	}
	select {
	case <-ctx.Done():
	case <-time.After(time.Second):
		t.Fatal("background graph context was not cancelled")
	}
}
