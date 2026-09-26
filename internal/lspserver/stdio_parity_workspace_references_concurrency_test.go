package lspserver

import (
	"context"
	"io"
	"os"
	"path/filepath"
	"strings"
	"testing"
	"time"

	"github.com/yottonoko/classic-web-system-lsp/internal/lsp"
)

func TestStdioParityReusesInflightWorkspaceReferenceRequestsBeforeRepeatingWorkerWork(t *testing.T) {
	t.Setenv("ASP_LSP_TEST_VB_REFERENCES_WORKER_DELAY_MS", "150")
	root := t.TempDir()
	common := filepath.Join(root, "common.inc")
	page := filepath.Join(root, "default.asp")
	commonSource := `<%
Private Function SharedTitle()
  SharedTitle = "Dashboard"
End Function
%>`
	writeWorkspaceReferenceFixture(t, common, commonSource)
	writeWorkspaceReferenceFixture(t, page, `<!-- #include file="common.inc" -->
<%
Response.Write SharedTitle()
%>`)
	commonURI := pathToFileURI(common)
	pageURI := pathToFileURI(page)

	client := startWorkspaceReferenceClient(t, map[string]any{
		"processId":    nil,
		"rootUri":      pathToFileURI(root),
		"capabilities": map[string]any{},
	})
	defer client.close()
	notifyConfiguration(t, client, map[string]any{"aspLsp": map[string]any{
		"debug": map[string]any{"output": "summary"},
	}})
	openClassicASPDocument(t, client, commonURI, commonSource)

	params := map[string]any{
		"textDocument": map[string]any{"uri": commonURI},
		"position":     map[string]any{"line": 1, "character": 20},
		"context":      map[string]any{"includeDeclaration": false},
	}
	first := client.requestAsync("textDocument/references", params)
	second := client.requestAsync("textDocument/references", params)
	firstReferences := client.waitForResponse("textDocument/references", first)
	secondReferences := client.waitForResponse("textDocument/references", second)
	if !strings.Contains(mustJSONText(t, firstReferences.Result), pageURI) {
		t.Fatalf("first in-flight references missing page URI: %s", mustJSONText(t, firstReferences.Result))
	}
	if !strings.Contains(mustJSONText(t, secondReferences.Result), pageURI) {
		t.Fatalf("second in-flight references missing page URI: %s", mustJSONText(t, secondReferences.Result))
	}
	client.waitForLogContaining("vb.references.workspace.reuse")
}

func TestWorkspaceReferenceInflightJoinHonorsCancellation(t *testing.T) {
	server := New(nil, io.Discard, io.Discard)
	parsed := server.parseText("file:///workspace/cancel.asp", "<% Dim SharedValue : Response.Write SharedValue %>", "VBScript")
	position := lsp.Position{Line: 0, Character: 7}
	position = server.workspaceReferenceCachePosition(parsed, "SharedValue", "variable", position)
	key := workspaceReferenceRequestKey(parsed.URI, position, false, "variable", server.referenceGeneration, "SharedValue")
	inflight := &workspaceReferenceInflight{done: make(chan struct{}), generation: server.referenceGeneration}
	server.referenceInflight[key] = inflight
	ctx, cancel := context.WithCancel(context.Background())
	cancel()
	started := time.Now()
	locations := server.workspaceVBScriptReferencesWithTestDelay(ctx, parsed, position, false, "variable", false)
	if elapsed := time.Since(started); elapsed > 100*time.Millisecond {
		t.Fatalf("cancelled in-flight join took %v", elapsed)
	}
	if locations != nil {
		t.Fatalf("cancelled in-flight join returned locations: %#v", locations)
	}
	close(inflight.done)
}

func TestStdioParityDoesNotAdoptStaleWorkerBackedWorkspaceReferenceResultsAfterFileChanges(t *testing.T) {
	t.Setenv("ASP_LSP_TEST_VB_REFERENCES_WORKER_DELAY_MS", "200")
	root := t.TempDir()
	common := filepath.Join(root, "common.inc")
	page := filepath.Join(root, "default.asp")
	commonSource := `<%
Private Function SharedTitle()
  SharedTitle = "Dashboard"
End Function
%>`
	writeWorkspaceReferenceFixture(t, common, commonSource)
	writeWorkspaceReferenceFixture(t, page, `<!-- #include file="common.inc" -->
<%
Response.Write SharedTitle()
%>`)
	commonURI := pathToFileURI(common)
	pageURI := pathToFileURI(page)

	client := startWorkspaceReferenceClient(t, map[string]any{
		"processId":    nil,
		"rootUri":      pathToFileURI(root),
		"capabilities": map[string]any{},
	})
	defer client.close()
	notifyConfiguration(t, client, map[string]any{"aspLsp": map[string]any{
		"debug": map[string]any{"output": "summary"},
	}})
	openClassicASPDocument(t, client, commonURI, commonSource)

	pendingReferences := client.requestAsync("textDocument/references", map[string]any{
		"textDocument": map[string]any{"uri": commonURI},
		"position":     map[string]any{"line": 1, "character": 20},
		"context":      map[string]any{"includeDeclaration": false},
	})
	client.waitForLogContaining("vb.references.workspace.candidates")
	if err := os.WriteFile(page, []byte("<%\nResponse.Write \"changed\"\n%>"), 0o644); err != nil {
		t.Fatal(err)
	}
	if err := client.notify("workspace/didChangeWatchedFiles", map[string]any{
		"changes": []map[string]any{{"uri": pageURI, "type": 2}},
	}); err != nil {
		t.Fatal(err)
	}

	var references *rpcMessage
	select {
	case references = <-pendingReferences:
	case <-time.After(10 * time.Second):
		t.Fatal("timed out waiting for textDocument/references response")
	}
	if references.Error != nil {
		if references.Error.Code != -32800 {
			t.Fatalf("textDocument/references returned error: %#v", references.Error)
		}
		// A watched-file revision now cancels older requests before applying the
		// new workspace state. Cancellation is also a valid rejection of the stale
		// worker result; the client can retry against the new generation.
		return
	}
	if strings.Contains(mustJSONText(t, references.Result), pageURI) {
		t.Fatalf("stale worker references adopted changed page URI: %s", mustJSONText(t, references.Result))
	}
	client.waitForLogContaining("vb.references.worker.stale")
}
