package lspserver

import (
	"context"
	"io"
	"sync"
	"testing"

	"github.com/yottonoko/classic-web-system-lsp/internal/core"
	workspacepkg "github.com/yottonoko/classic-web-system-lsp/internal/workspace"
)

func TestWorkspaceGraphCollectionCancellationDoesNotPublishPartialGraph(t *testing.T) {
	server := New(nil, io.Discard, io.Discard)
	firstURI := "file:///workspace/a.asp"
	secondURI := "file:///workspace/b.asp"
	server.workspace[firstURI] = core.NewTextDocument(firstURI, "classic-asp", 0, "<% Dim FirstValue %>")
	server.workspace[secondURI] = core.NewTextDocument(secondURI, "classic-asp", 0, "<% Dim SecondValue %>")

	ctx, cancel := context.WithCancel(context.Background())
	var cancelOnce sync.Once
	server.documentParseTestHook = func(uri string) {
		if workspacepkg.SameFileIdentityURI(uri, firstURI) {
			cancelOnce.Do(cancel)
		}
	}
	documents, _ := server.workspaceGraphDocumentsContextWithRestore(ctx, false)
	if documents != nil {
		t.Fatalf("cancelled graph collection returned %d partial documents", len(documents))
	}
	server.mu.Lock()
	revision := server.workspaceIncludeGraphRevision
	complete := server.workspaceIncludeGraphComplete
	graphSize := server.workspaceIncludeGraph.Size()
	server.mu.Unlock()
	if revision != 0 || complete || graphSize != 0 {
		t.Fatalf("cancelled graph collection published graph state: revision=%d complete=%t size=%d", revision, complete, graphSize)
	}
}

func TestWorkspaceGraphCollectionRejectsStaleGeneration(t *testing.T) {
	server := New(nil, io.Discard, io.Discard)
	firstURI := "file:///workspace/a.asp"
	secondURI := "file:///workspace/b.asp"
	server.workspace[firstURI] = core.NewTextDocument(firstURI, "classic-asp", 0, "<% Dim FirstValue %>")
	server.workspace[secondURI] = core.NewTextDocument(secondURI, "classic-asp", 0, "<% Dim SecondValue %>")

	var advanceOnce sync.Once
	server.documentParseTestHook = func(uri string) {
		if !workspacepkg.SameFileIdentityURI(uri, firstURI) {
			return
		}
		advanceOnce.Do(func() {
			server.mu.Lock()
			server.graphGeneration++
			server.mu.Unlock()
		})
	}
	documents, _ := server.workspaceGraphDocumentsContextWithRestore(context.Background(), false)
	if documents != nil {
		t.Fatalf("stale graph collection returned %d documents", len(documents))
	}
	server.mu.Lock()
	revision := server.workspaceIncludeGraphRevision
	complete := server.workspaceIncludeGraphComplete
	server.mu.Unlock()
	if revision != 0 || complete {
		t.Fatalf("stale graph collection published graph state: revision=%d complete=%t", revision, complete)
	}
}

func TestWorkspaceGraphCollectionReusesCompleteIncludeGraphWhenRestoreIsAllowed(t *testing.T) {
	server := New(nil, io.Discard, io.Discard)
	uri := "file:///workspace/a.asp"
	server.workspace[uri] = core.NewTextDocument(uri, "classic-asp", 0, "<% Dim FirstValue %>")

	documents, _ := server.workspaceGraphDocumentsContextWithRestore(context.Background(), true)
	if len(documents) != 1 {
		t.Fatalf("first graph collection returned %d documents, want 1", len(documents))
	}
	server.mu.Lock()
	firstGraph := server.workspaceIncludeGraph
	firstRevision := server.workspaceIncludeGraphRevision
	server.mu.Unlock()

	documents, _ = server.workspaceGraphDocumentsContextWithRestore(context.Background(), true)
	if len(documents) != 1 {
		t.Fatalf("second graph collection returned %d documents, want 1", len(documents))
	}
	server.mu.Lock()
	reusedGraph := server.workspaceIncludeGraph
	reusedRevision := server.workspaceIncludeGraphRevision
	server.mu.Unlock()
	if reusedGraph != firstGraph || reusedRevision != firstRevision {
		t.Fatalf("complete include graph was rebuilt: pointerChanged=%t revision=%d/%d", reusedGraph != firstGraph, reusedRevision, firstRevision)
	}

	if documents, _ := server.workspaceGraphDocumentsContextWithRestore(context.Background(), false); len(documents) != 1 {
		t.Fatalf("forced graph collection returned %d documents, want 1", len(documents))
	}
	server.mu.Lock()
	forcedGraph := server.workspaceIncludeGraph
	forcedRevision := server.workspaceIncludeGraphRevision
	server.mu.Unlock()
	if forcedGraph == reusedGraph || forcedRevision <= reusedRevision {
		t.Fatalf("forced include graph rebuild was skipped: pointerChanged=%t revision=%d/%d", forcedGraph != reusedGraph, forcedRevision, reusedRevision)
	}
}
