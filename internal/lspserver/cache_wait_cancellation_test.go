package lspserver

import (
	"context"
	"io"
	"os"
	"path/filepath"
	"slices"
	"sync"
	"testing"
	"time"

	"github.com/yottonoko/classic-web-system-lsp/internal/core"
	workspacepkg "github.com/yottonoko/classic-web-system-lsp/internal/workspace"
)

func TestWorkspaceIndexDiskWriteCancelsWhileCacheIsBusy(t *testing.T) {
	server := &Server{}
	server.workspaceIndexDiskCacheUseMu.Lock()
	var unlock sync.Once
	release := func() { unlock.Do(server.workspaceIndexDiskCacheUseMu.Unlock) }
	defer release()
	ctx, cancel := context.WithCancel(context.Background())
	defer cancel()
	done := make(chan bool, 1)
	go func() { done <- server.writeWorkspaceIndexToDiskGuarded(ctx, 0, true, false, nil) }()
	cancel()
	select {
	case committed := <-done:
		if committed {
			t.Fatal("cancelled write committed")
		}
	case <-time.After(time.Second):
		release()
		<-done
		t.Fatal("cancelled workspace index write remained blocked by cache ownership")
	}
}

func TestDocumentAnalysisIsNotAdmittedAfterShutdown(t *testing.T) {
	server := newDocumentOpenCancellationTestServer(t, t.TempDir(), io.Discard)
	server.shutdownRuntimeCaches()
	doc := core.NewTextDocument("file:///closed.asp", "classic-asp", 1, "<% Dim value %>")
	parsed := core.ParseDocument(doc.URI, doc.Text, core.Settings{})
	server.scheduleDocumentChangeAnalysis(doc, parsed)
	server.documentOpenAnalysisWorkers.Wait()
	server.mu.Lock()
	sequence := server.documentOpenAnalysisSequence
	server.mu.Unlock()
	if sequence != 0 {
		t.Fatalf("shutdown server admitted document analysis generation %d", sequence)
	}
}

func TestCancelledReferencePreparationReleasesInflightWaiters(t *testing.T) {
	server := newDocumentOpenCancellationTestServer(t, t.TempDir(), io.Discard)
	doc := core.NewTextDocument("file:///references.asp", "classic-asp", 1, "<% Dim value : value = 1 %>")
	parsed := core.ParseDocument(doc.URI, doc.Text, core.Settings{})
	ctx, cancel := context.WithCancel(context.Background())
	cancel()
	server.workspaceVBScriptReferencesOnce(ctx, parsed, doc.PositionAt(7), false, "variable", false, nil, true)
	server.mu.Lock()
	defer server.mu.Unlock()
	if len(server.referenceInflight) != 0 {
		t.Fatalf("cancelled preparation left %d reference queries waiting forever", len(server.referenceInflight))
	}
}

func TestReferenceScopeCancellationDoesNotWaitForSharedFileRead(t *testing.T) {
	root := t.TempDir()
	server := newDocumentOpenCancellationTestServer(t, root, io.Discard)
	path := filepath.Join(root, "shared.inc")
	if err := os.WriteFile(path, []byte("<% Dim sharedValue %>"), 0600); err != nil {
		t.Fatal(err)
	}
	started, release := make(chan struct{}), make(chan struct{})
	server.workspaceFileReadTestHook = func(string) { close(started); <-release }
	leader := make(chan struct{})
	go func() { defer close(leader); server.parsedIncludeFileContext(context.Background(), path) }()
	<-started
	var once sync.Once
	unblock := func() { once.Do(func() { close(release) }) }
	defer func() { unblock(); <-leader }()
	parsed := core.ParseDocument(filePathURI(filepath.Join(root, "page.asp")), "<% Dim value %>", core.Settings{})
	ctx, cancel := context.WithTimeout(context.Background(), 50*time.Millisecond)
	defer cancel()
	done := make(chan bool, 1)
	go func() {
		_, complete := server.workspaceReferenceDocumentsForScope(ctx, parsed, workspaceReferenceScopeSnapshot{FileNames: []string{path}})
		done <- complete
	}()
	select {
	case complete := <-done:
		if complete {
			t.Fatal("blocked scope preparation unexpectedly completed")
		}
	case <-time.After(time.Second):
		unblock()
		<-done
		t.Fatal("cancelled scope preparation remained blocked by a shared file read")
	}
}

func TestReferenceScopeReportsPreparedFileCounts(t *testing.T) {
	root := t.TempDir()
	server := newDocumentOpenCancellationTestServer(t, root, io.Discard)
	first := filepath.Join(root, "first.asp")
	second := filepath.Join(root, "second.inc")
	parsed := core.ParseDocument(filePathURI(first), "<% Dim value %>", core.Settings{})
	server.workspace[filePathURI(second)] = core.NewTextDocument(filePathURI(second), "classic-asp", 0, "<% Dim other %>")
	var counts []int
	ctx := context.WithValue(context.Background(), workspaceReferenceProgressContextKey{}, workspaceReferenceProgressReporter(func(current, total int, uri string) {
		if total != 2 {
			t.Errorf("progress total = %d, want 2 files", total)
		}
		counts = append(counts, current)
	}))
	documents, complete := server.workspaceReferenceDocumentsForScope(ctx, parsed, workspaceReferenceScopeSnapshot{FileNames: []string{first, second}, DocumentKeys: []string{workspacepkg.FileIdentityKeyFromURI(parsed.URI)}})
	if !complete || len(documents) != 2 || !slices.Equal(counts, []int{0, 1, 2}) {
		t.Fatalf("scope completion=%v documents=%d progress=%v", complete, len(documents), counts)
	}
}
