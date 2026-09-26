package lspserver

import (
	"context"
	"io"
	"os"
	"path/filepath"
	"strings"
	"testing"
	"time"
)

func TestWorkspaceIndexIncludeSyncFailureDoesNotFinalizeReferencesAndRecovers(t *testing.T) {
	root := t.TempDir()
	commonPath := filepath.Join(root, "common.inc")
	pagePath := filepath.Join(root, "page.asp")
	commonSource := "<%\nDim SharedValue\n%>"
	pageSource := "<!-- #include file=\"common.inc\" -->\n<%\nResponse.Write SharedValue\n%>"
	for path, source := range map[string]string{commonPath: commonSource, pagePath: pageSource} {
		if err := os.WriteFile(path, []byte(source), 0o600); err != nil {
			t.Fatal(err)
		}
	}

	server := New(nil, io.Discard, io.Discard)
	defer server.shutdownRuntimeCaches()
	server.settings.CacheEnabled = false
	server.rootPath = root
	server.rootURI = filePathURI(root)
	server.workspaceRoots = []workspaceRoot{{Path: root, URI: server.rootURI}}
	server.configureFsGateway()
	server.workspaceIndexEnabled = true
	server.workspaceIndexGeneration = 1
	server.settings.CodeLensReferences = true

	syncAttempts := 0
	server.workspaceIncludeGraphSyncTestHook = func() bool {
		syncAttempts++
		return false
	}
	runWorkspaceIndexWorkerForIncludeSyncTest(t, server, 1, "test.includeGraph.syncFailure")
	if syncAttempts != 1 {
		t.Fatalf("include graph sync attempts after failure = %d, want one terminal attempt", syncAttempts)
	}

	commonURI := filePathURI(commonPath)
	_, parsed := server.parsed(commonURI)
	if parsed == nil {
		t.Fatal("failed to parse indexed declaration document")
	}
	declarations := server.vbscriptReferenceCodeLensDeclarations(parsed)
	if len(declarations) != 1 || !strings.EqualFold(declarations[0].Name, "SharedValue") {
		t.Fatalf("reference declarations = %#v, want SharedValue", declarations)
	}

	server.mu.Lock()
	ready := server.workspaceReferenceIndexReadyLocked()
	readyGeneration := server.workspaceReferenceIndexReadyGeneration
	graphComplete := server.workspaceIncludeGraphComplete
	graphSize := server.workspaceIncludeGraph.Size()
	server.mu.Unlock()
	if ready || readyGeneration == server.workspaceIndexGeneration {
		t.Fatalf("sync failure published reference readiness: ready=%t readyGeneration=%d workspaceGeneration=%d", ready, readyGeneration, server.workspaceIndexGeneration)
	}
	if graphComplete || graphSize != 0 {
		t.Fatalf("sync failure published incomplete include graph: complete=%t size=%d", graphComplete, graphSize)
	}

	lenses := server.codeLens(commonURI)
	if len(lenses) != 1 {
		t.Fatalf("cold reference CodeLens count = %d, want one", len(lenses))
	}
	resolvedBeforeRecovery := server.resolveCodeLens(context.Background(), lenses[0])
	if resolvedBeforeRecovery.Command == nil || !strings.Contains(strings.ToLower(resolvedBeforeRecovery.Command.Title), "calculating") {
		t.Fatalf("sync failure finalized CodeLens unexpectedly: %#v", resolvedBeforeRecovery)
	}
	if state := server.snapshotWorkspaceReferenceCodeLensCounts(parsed, declarations)[0]; state.final {
		t.Fatalf("sync failure finalized reference count: %#v", state)
	}

	server.workspaceIncludeGraphSyncTestHook = nil
	server.scheduleWorkspaceIndex("test.includeGraph.syncRecovery")
	waitForWorkspaceIndexCompletion(t, server)

	server.mu.Lock()
	ready = server.workspaceReferenceIndexReadyLocked()
	readyGeneration = server.workspaceReferenceIndexReadyGeneration
	graphComplete = server.workspaceIncludeGraphComplete
	graphSize = server.workspaceIncludeGraph.Size()
	workspaceGeneration := server.workspaceIndexGeneration
	server.mu.Unlock()
	if !ready || readyGeneration != workspaceGeneration {
		t.Fatalf("successful reindex readiness = ready:%t readyGeneration:%d workspaceGeneration:%d", ready, readyGeneration, workspaceGeneration)
	}
	if !graphComplete || graphSize != 2 {
		t.Fatalf("successful reindex include graph = complete:%t size:%d, want complete graph with two documents", graphComplete, graphSize)
	}

	resolvedAfterRecovery := server.resolveCodeLens(context.Background(), lenses[0])
	if resolvedAfterRecovery.Command == nil || resolvedAfterRecovery.Command.Title != "1 reference" {
		t.Fatalf("successful reindex CodeLens = %#v, want one finalized reference", resolvedAfterRecovery)
	}
}

func runWorkspaceIndexWorkerForIncludeSyncTest(t *testing.T, server *Server, generation uint64, reason string) {
	t.Helper()
	done := make(chan struct{})
	server.mu.Lock()
	server.workspaceIndexDone = done
	server.mu.Unlock()
	server.workspaceIndexWorkers.Add(1)
	go server.runWorkspaceIndexWorker(context.Background(), reason, generation, done)
	waitContext, cancel := context.WithTimeout(context.Background(), 5*time.Second)
	defer cancel()
	if !server.waitForWorkspaceIndex(waitContext) {
		t.Fatal("workspace index worker did not finish")
	}
}
