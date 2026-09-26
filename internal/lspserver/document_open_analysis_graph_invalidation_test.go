package lspserver

import (
	"context"
	"path/filepath"
	"sync"
	"testing"
	"time"

	"github.com/yottonoko/classic-web-system-lsp/internal/core"
	"github.com/yottonoko/classic-web-system-lsp/internal/graph"
)

func TestDocumentOpenAnalysisSaveAfterManifestPublicationInvalidatesGraph(t *testing.T) {
	root := t.TempDir()
	uri := filePathURI(filepath.Join(root, "save.asp"))
	text := `<% Dim currentValue : currentValue = 1 %>`
	server := newWorkspaceReferencePersistenceServer(t, root, filepath.Join(root, "cache"))

	document := core.NewTextDocument(uri, "classic-asp", 1, text)
	parsed := core.ParseDocument(uri, text, core.Settings{DefaultLanguage: server.settings.DefaultLanguage})
	server.mu.Lock()
	server.rememberOpenDocumentLocked(uri, document)
	server.graphCache["stale"] = graph.Payload{}
	initialGeneration := server.graphGeneration
	server.mu.Unlock()

	manifestPublished := make(chan struct{})
	releaseQueue := make(chan struct{})
	var publishOnce sync.Once
	workspaceArtifactQueueTestHook.Lock()
	previousHook := workspaceArtifactQueueTestHook.fn
	workspaceArtifactQueueTestHook.fn = func() {
		publishOnce.Do(func() { close(manifestPublished) })
		<-releaseQueue
	}
	workspaceArtifactQueueTestHook.Unlock()
	defer func() {
		select {
		case <-releaseQueue:
		default:
			close(releaseQueue)
		}
		workspaceArtifactQueueTestHook.Lock()
		workspaceArtifactQueueTestHook.fn = previousHook
		workspaceArtifactQueueTestHook.Unlock()
	}()

	server.scheduleDocumentChangeAnalysis(document, parsed)
	select {
	case <-manifestPublished:
	case <-time.After(2 * time.Second):
		t.Fatal("document analysis did not reach the post-publication queue hook")
	}
	server.mu.Lock()
	manifest := server.workspaceArtifacts[workspaceDocumentIDFromURI(uri)]
	generation := server.graphGeneration
	cacheEntries := len(server.graphCache)
	server.mu.Unlock()
	if manifest == nil || generation != initialGeneration || cacheEntries != 1 {
		t.Fatalf("post-publication state = manifest:%#v generation:%d cacheEntries:%d; want published manifest with unchanged graph", manifest, generation, cacheEntries)
	}

	saved := make(chan error, 1)
	go func() {
		saved <- server.handleNotification(context.Background(), "textDocument/didSave", mustRaw(map[string]any{
			"textDocument": map[string]any{"uri": uri},
		}))
	}()
	select {
	case err := <-saved:
		if err != nil {
			t.Fatal(err)
		}
	case <-time.After(2 * time.Second):
		t.Fatal("didSave waited for the blocked post-publication worker")
	}

	server.mu.Lock()
	job := server.documentOpenAnalysisJobs[diagnosticTimerKey(uri)]
	if job == nil {
		server.mu.Unlock()
		t.Fatal("didSave removed the in-flight document analysis job")
	}
	done := job.generationDone
	inFlight := job.publicationInFlight
	active := job.active
	server.mu.Unlock()
	if !inFlight || active {
		t.Fatalf("cancelled post-publication job = inFlight:%t active:%t; want in-flight inactive job", inFlight, active)
	}
	select {
	case <-done:
		t.Fatal("document-open barrier signalled before queued publication and graph invalidation completed")
	default:
	}

	barrierEntered := make(chan struct{})
	barrierResult := make(chan bool, 1)
	barrierContext := &documentOpenAnalysisBarrierProbeContext{Context: context.Background(), entered: barrierEntered}
	go func() { barrierResult <- server.waitForDocumentOpenAnalysisContext(barrierContext) }()
	select {
	case <-barrierEntered:
	case <-time.After(2 * time.Second):
		t.Fatal("document-open barrier did not start")
	}
	select {
	case got := <-barrierResult:
		t.Fatalf("document-open barrier returned %t before publication completed", got)
	default:
	}

	close(releaseQueue)
	waitForDocumentOpenAnalysisWorkers(t, server)
	select {
	case got := <-barrierResult:
		if !got {
			t.Fatal("document-open barrier rejected the completed publication")
		}
	case <-time.After(2 * time.Second):
		t.Fatal("document-open barrier did not observe completed publication")
	}

	server.mu.Lock()
	finalGeneration := server.graphGeneration
	finalCacheEntries := len(server.graphCache)
	server.mu.Unlock()
	if finalGeneration != initialGeneration+1 || finalCacheEntries != 0 {
		t.Fatalf("final graph state = generation:%d cacheEntries:%d; want one invalidation and an empty cache", finalGeneration, finalCacheEntries)
	}
}

type documentOpenAnalysisBarrierProbeContext struct {
	context.Context
	entered     chan struct{}
	enteredOnce sync.Once
}

func (c *documentOpenAnalysisBarrierProbeContext) Err() error {
	c.enteredOnce.Do(func() { close(c.entered) })
	return c.Context.Err()
}
