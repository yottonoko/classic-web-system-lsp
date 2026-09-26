package lspserver

import (
	"context"
	"io"
	"path/filepath"
	"sync"
	"testing"
	"time"

	"github.com/yottonoko/classic-web-system-lsp/internal/core"
	"github.com/yottonoko/classic-web-system-lsp/internal/graph"
)

func TestWorkspaceGraphBackgroundRequestsShareOneBuild(t *testing.T) {
	t.Setenv("ASP_LSP_TEST_GRAPH_BACKGROUND_MIN_DOCUMENTS", "1")
	t.Setenv("ASP_LSP_TEST_GRAPH_BACKGROUND_DEBOUNCE_MS", "10000")
	root := t.TempDir()
	path := filepath.Join(root, "default.asp")
	uri := filePathURI(path)
	server := New(nil, io.Discard, io.Discard)
	server.rootPath = root
	server.rootURI = filePathURI(root)
	server.workspaceRoots = []workspaceRoot{{Path: root, URI: filePathURI(root)}}
	server.workspace[uri] = core.NewTextDocument(uri, "classic-asp", 0, `<% Dim SharedValue %>`)
	defer func() {
		server.invalidateGraphBackground()
		deadline := time.Now().Add(time.Second)
		for time.Now().Before(deadline) {
			server.mu.Lock()
			remaining := len(server.graphBackgroundTasks)
			server.mu.Unlock()
			if remaining == 0 {
				break
			}
			time.Sleep(time.Millisecond)
		}
		server.shutdownRuntimeCaches()
	}()

	arg := graphCommandArg{Scope: "workspace"}
	first, ok := server.maybeBuildGraphInBackgroundContext(context.Background(), arg)
	if !ok || first.Pending == nil || !*first.Pending || first.BackgroundTaskID == "" {
		t.Fatalf("first workspace graph did not start in background: %#v", first)
	}
	second, ok := server.maybeBuildGraphInBackgroundContext(context.Background(), arg)
	if !ok || second.Pending == nil || !*second.Pending {
		t.Fatalf("second workspace graph did not return pending background work: %#v", second)
	}
	if second.BackgroundTaskID != first.BackgroundTaskID || second.CorrelationID != first.CorrelationID {
		t.Fatalf("duplicate workspace graph builds = first:%q/%q second:%q/%q", first.BackgroundTaskID, first.CorrelationID, second.BackgroundTaskID, second.CorrelationID)
	}
	server.mu.Lock()
	backgroundTasks := len(server.graphBackgroundTasks)
	server.mu.Unlock()
	if backgroundTasks != 1 {
		t.Fatalf("workspace graph background tasks = %d, want 1", backgroundTasks)
	}
}

func TestWorkspaceGraphBackgroundShutdownJoinsRegisteredBuild(t *testing.T) {
	t.Setenv("ASP_LSP_TEST_GRAPH_BACKGROUND_MIN_DOCUMENTS", "1")
	server, arg := backgroundGraphTestServer(t)
	registered := make(chan struct{})
	release := make(chan struct{})
	var once sync.Once
	server.graphBackgroundTransitionTestHook = func(phase string) {
		if phase != "registered" {
			return
		}
		once.Do(func() { close(registered) })
		<-release
	}
	buildDone := make(chan struct{})
	go func() {
		defer close(buildDone)
		server.maybeBuildGraphInBackgroundContext(context.Background(), arg)
	}()
	waitForConcurrencySignal(t, registered, "background graph build was not registered")
	shutdownDone := make(chan struct{})
	go func() {
		defer close(shutdownDone)
		server.shutdownRuntimeCaches()
	}()
	select {
	case <-shutdownDone:
		t.Fatal("shutdown returned before the registered graph build exited")
	case <-time.After(50 * time.Millisecond):
	}
	close(release)
	waitForConcurrencySignal(t, buildDone, "cancelled graph owner did not exit")
	waitForConcurrencySignal(t, shutdownDone, "shutdown did not join the registered graph build")
}

func TestWorkspaceGraphBackgroundInvalidationRejectsUnpublishedPartial(t *testing.T) {
	t.Setenv("ASP_LSP_TEST_GRAPH_BACKGROUND_MIN_DOCUMENTS", "1")
	t.Setenv("ASP_LSP_TEST_GRAPH_BACKGROUND_DEBOUNCE_MS", "10000")
	server, arg := backgroundGraphTestServer(t)
	beforePublish := make(chan struct{})
	waiting := make(chan struct{})
	release := make(chan struct{})
	var publishOnce sync.Once
	var waitingOnce sync.Once
	server.graphBackgroundTransitionTestHook = func(phase string) {
		switch phase {
		case "beforePublish":
			publishOnce.Do(func() { close(beforePublish) })
			<-release
		case "waiting":
			waitingOnce.Do(func() { close(waiting) })
		}
	}
	type buildResult struct {
		payload graph.Payload
		ok      bool
	}
	ownerResult := make(chan buildResult, 1)
	go func() {
		payload, ok := server.maybeBuildGraphInBackgroundContext(context.Background(), arg)
		ownerResult <- buildResult{payload: payload, ok: ok}
	}()
	waitForConcurrencySignal(t, beforePublish, "background graph did not reach publication")
	waiterResult := make(chan buildResult, 1)
	go func() {
		payload, ok := server.maybeBuildGraphInBackgroundContext(context.Background(), arg)
		waiterResult <- buildResult{payload: payload, ok: ok}
	}()
	waitForConcurrencySignal(t, waiting, "second graph request did not join the registered build")
	server.invalidateGraphBackground()
	close(release)
	for name, resultChannel := range map[string]<-chan buildResult{
		"owner":  ownerResult,
		"waiter": waiterResult,
	} {
		select {
		case result := <-resultChannel:
			if result.payload.Pending != nil && *result.payload.Pending {
				t.Fatalf("%s adopted an invalidated pending graph: %#v", name, result.payload)
			}
		case <-time.After(time.Second):
			t.Fatalf("%s did not unblock after graph invalidation", name)
		}
	}
	server.shutdownRuntimeCaches()
}

func backgroundGraphTestServer(t *testing.T) (*Server, graphCommandArg) {
	t.Helper()
	root := t.TempDir()
	path := filepath.Join(root, "default.asp")
	uri := filePathURI(path)
	server := New(nil, io.Discard, io.Discard)
	server.rootPath = root
	server.rootURI = filePathURI(root)
	server.workspaceRoots = []workspaceRoot{{Path: root, URI: filePathURI(root)}}
	server.workspace[uri] = core.NewTextDocument(uri, "classic-asp", 0, `<% Dim SharedValue %>`)
	return server, graphCommandArg{Scope: "workspace"}
}
