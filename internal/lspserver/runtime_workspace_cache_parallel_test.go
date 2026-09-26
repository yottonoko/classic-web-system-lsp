package lspserver

import (
	"context"
	"io"
	"os"
	"path/filepath"
	"reflect"
	"sync"
	"sync/atomic"
	"testing"
	"time"

	"github.com/yottonoko/classic-web-system-lsp/internal/core"
	workspacepkg "github.com/yottonoko/classic-web-system-lsp/internal/workspace"
)

func TestWorkspaceIncludeGraphSyncPreparesDocumentsInParallelAndCommitsInOrder(t *testing.T) {
	root, documents := workspaceIncludeGraphSyncTestDocuments(t)
	server := newWorkspaceIncludeGraphSyncTestServer(t, root, 3)
	var active atomic.Int64
	var maximum atomic.Int64
	started := make(chan struct{}, len(documents))
	release := make(chan struct{})
	var releaseOnce sync.Once
	defer releaseOnce.Do(func() { close(release) })
	server.workspaceIncludeGraphDocumentTestHook = func(ctx context.Context, _ string) {
		current := active.Add(1)
		for {
			previous := maximum.Load()
			if current <= previous || maximum.CompareAndSwap(previous, current) {
				break
			}
		}
		started <- struct{}{}
		select {
		case <-release:
		case <-ctx.Done():
		}
		active.Add(-1)
	}

	done := make(chan bool, 1)
	go func() {
		done <- server.syncWorkspaceIncludeGraphCacheGuarded(context.Background(), 0, false, documents)
	}()
	for range 3 {
		select {
		case <-started:
		case <-time.After(time.Second):
			t.Fatal("include graph document preparation did not use all configured workers")
		}
	}
	if got := maximum.Load(); got < 2 {
		t.Fatalf("maximum concurrent document preparations = %d, want at least 2", got)
	}
	releaseOnce.Do(func() { close(release) })
	if !<-done {
		t.Fatal("include graph synchronization failed")
	}

	snapshot, ok := server.workspaceIncludeGraph.Snapshot("")
	if !ok || len(snapshot.Entries) != 3 {
		t.Fatalf("include graph snapshot = %#v, %v; want three entries", snapshot, ok)
	}
	for index, entry := range snapshot.Entries {
		want := filepath.Join(root, []string{"first.asp", "second.asp", "third.asp"}[index])
		if filepath.Clean(entry.FileName) != filepath.Clean(want) {
			t.Fatalf("snapshot entry %d owner = %q, want %q", index, entry.FileName, want)
		}
	}
}

func TestWorkspaceIncludeGraphSyncWorkersOneMatchesParallelResults(t *testing.T) {
	root, documents := workspaceIncludeGraphSyncTestDocuments(t)
	serial := newWorkspaceIncludeGraphSyncTestServer(t, root, 1)
	parallel := newWorkspaceIncludeGraphSyncTestServer(t, root, 4)

	if !serial.syncWorkspaceIncludeGraphCacheGuarded(context.Background(), 0, false, documents) {
		t.Fatal("workers=1 include graph synchronization failed")
	}
	if !parallel.syncWorkspaceIncludeGraphCacheGuarded(context.Background(), 0, false, documents) {
		t.Fatal("parallel include graph synchronization failed")
	}
	serialSnapshot, serialOK := serial.workspaceIncludeGraph.Snapshot("")
	parallelSnapshot, parallelOK := parallel.workspaceIncludeGraph.Snapshot("")
	if !serialOK || !parallelOK || !reflect.DeepEqual(serialSnapshot, parallelSnapshot) {
		t.Fatalf("workers=1 and parallel snapshots differ: serial=%#v/%v parallel=%#v/%v", serialSnapshot, serialOK, parallelSnapshot, parallelOK)
	}
}

func TestWorkspaceIncludeGraphSyncCancellationPreservesPreviousGraph(t *testing.T) {
	root, documents := workspaceIncludeGraphSyncTestDocuments(t)
	server := newWorkspaceIncludeGraphSyncTestServer(t, root, 2)
	server.workspaceIncludeGraph.Reset("previous")
	server.workspaceIncludeGraph.Upsert(filepath.Join(root, "previous.asp"), workspacepkg.SourceMetadata{FileName: filepath.Join(root, "previous.asp")}, nil, "previous")
	server.workspaceIncludeGraphComplete = true
	beforeGraph := server.workspaceIncludeGraph
	beforeRevision := server.workspaceIncludeGraphRevision

	ctx, cancel := context.WithCancel(context.Background())
	defer cancel()
	started := make(chan struct{})
	var startedOnce sync.Once
	server.workspaceIncludeGraphDocumentTestHook = func(ctx context.Context, _ string) {
		startedOnce.Do(func() { close(started) })
		<-ctx.Done()
	}
	done := make(chan bool, 1)
	go func() {
		done <- server.syncWorkspaceIncludeGraphCacheIfCurrent(ctx, server.graphGenerationSnapshot(), documents)
	}()
	select {
	case <-started:
	case <-time.After(time.Second):
		cancel()
		t.Fatal("include graph synchronization did not start")
	}
	cancel()
	if <-done {
		t.Fatal("cancelled include graph synchronization reported success")
	}
	if server.workspaceIncludeGraph != beforeGraph || server.workspaceIncludeGraphRevision != beforeRevision || !server.workspaceIncludeGraphComplete {
		t.Fatal("cancelled include graph synchronization mutated the previous graph")
	}
}

func TestWorkspaceIncludeGraphSyncGenerationDriftPreservesPreviousGraph(t *testing.T) {
	root, documents := workspaceIncludeGraphSyncTestDocuments(t)
	server := newWorkspaceIncludeGraphSyncTestServer(t, root, 2)
	server.workspaceIncludeGraph.Reset("previous")
	server.workspaceIncludeGraph.Upsert(filepath.Join(root, "previous.asp"), workspacepkg.SourceMetadata{FileName: filepath.Join(root, "previous.asp")}, nil, "previous")
	server.workspaceIncludeGraphComplete = true
	beforeGraph := server.workspaceIncludeGraph
	beforeRevision := server.workspaceIncludeGraphRevision
	generation := server.graphGenerationSnapshot()

	started := make(chan struct{})
	var startedOnce sync.Once
	release := make(chan struct{})
	server.workspaceIncludeGraphDocumentTestHook = func(context.Context, string) {
		startedOnce.Do(func() { close(started) })
		<-release
	}
	done := make(chan bool, 1)
	go func() {
		done <- server.syncWorkspaceIncludeGraphCacheIfCurrent(context.Background(), generation, documents)
	}()
	select {
	case <-started:
	case <-time.After(time.Second):
		close(release)
		t.Fatal("include graph synchronization did not start")
	}
	server.mu.Lock()
	server.graphGeneration++
	server.mu.Unlock()
	close(release)
	if <-done {
		t.Fatal("generation-drifted include graph synchronization reported success")
	}
	if server.workspaceIncludeGraph != beforeGraph || server.workspaceIncludeGraphRevision != beforeRevision || !server.workspaceIncludeGraphComplete {
		t.Fatal("generation-drifted include graph synchronization mutated the previous graph")
	}
}

func workspaceIncludeGraphSyncTestDocuments(t *testing.T) (string, []*core.ParsedDocument) {
	t.Helper()
	root := t.TempDir()
	names := []string{"first.asp", "second.asp", "third.asp"}
	documents := make([]*core.ParsedDocument, 0, len(names))
	for index, name := range names {
		includeName := name[:len(name)-len(filepath.Ext(name))] + ".inc"
		ownerText := `<!-- #include file="` + includeName + `" -->` + "\n<% Dim Value" + string(rune('A'+index)) + " %>"
		ownerPath := filepath.Join(root, name)
		if err := os.WriteFile(ownerPath, []byte(ownerText), 0o600); err != nil {
			t.Fatal(err)
		}
		if err := os.WriteFile(filepath.Join(root, includeName), []byte("<% Dim IncludedValue %>"), 0o600); err != nil {
			t.Fatal(err)
		}
		documents = append(documents, core.ParseDocument(filePathURI(ownerPath), ownerText, core.Settings{DefaultLanguage: "VBScript"}))
	}
	// Keep the invalid inputs in the worker batch to verify that they remain
	// ignored while valid documents retain their original order.
	documents = append([]*core.ParsedDocument{nil, &core.ParsedDocument{}}, documents...)
	return root, documents
}

func newWorkspaceIncludeGraphSyncTestServer(t *testing.T, root string, workers int) *Server {
	t.Helper()
	server := New(nil, io.Discard, io.Discard)
	t.Cleanup(server.shutdownRuntimeCaches)
	server.rootPath = root
	server.rootURI = filePathURI(root)
	server.workspaceRoots = []workspaceRoot{{Path: root, URI: server.rootURI}}
	server.configureFsGateway()
	server.analysisWorkers.setWorkers(workers)
	return server
}
