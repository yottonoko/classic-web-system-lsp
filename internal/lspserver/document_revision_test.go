package lspserver

import (
	"context"
	"io"
	"os"
	"path/filepath"
	"testing"
	"time"

	"github.com/yottonoko/classic-web-system-lsp/internal/core"
)

func TestDidChangePublishesNewImmutableDocumentRevision(t *testing.T) {
	const uri = "file:///site/immutable.asp"
	server := New(nil, io.Discard, io.Discard)
	server.settings.DiagnosticsDebounceMS = 60_000
	old := core.NewTextDocument(uri, "classic-asp", 1, "<div>old</div>")
	server.mu.Lock()
	server.rememberOpenDocumentLocked(uri, old)
	server.parsedCache[parsedDocumentCacheKey(uri)] = parsedDocumentCacheEntry{
		Version: 1, Text: old.Text, DefaultLanguage: server.settings.DefaultLanguage,
		Parsed: core.ParseDocument(uri, old.Text, core.Settings{DefaultLanguage: server.settings.DefaultLanguage}),
	}
	server.mu.Unlock()
	t.Cleanup(func() {
		server.cancelDocumentOpenAnalysis(uri)
		server.cancelScheduledDiagnostics(uri)
		server.documentOpenAnalysisWorkers.Wait()
	})

	if err := server.handleNotification(context.Background(), "textDocument/didChange", mustRaw(map[string]any{
		"textDocument": map[string]any{"uri": uri, "version": 2},
		"contentChanges": []map[string]any{{
			"range": map[string]any{
				"start": map[string]any{"line": 0, "character": 5},
				"end":   map[string]any{"line": 0, "character": 8},
			},
			"text": "new",
		}},
	})); err != nil {
		t.Fatal(err)
	}
	current := server.documentByURI(uri)
	if current == old {
		t.Fatal("didChange mutated the published document pointer in place")
	}
	if old.Text != "<div>old</div>" || old.Version != 1 {
		t.Fatalf("captured old revision changed to version=%d text=%q", old.Version, old.Text)
	}
	if current == nil || current.Text != "<div>new</div>" || current.Version != 2 {
		t.Fatalf("current revision = %#v", current)
	}
}

func TestDidSaveReusesPendingDocumentRevisionWithoutWaiting(t *testing.T) {
	root := t.TempDir()
	path := filepath.Join(root, "save.asp")
	const initial = "<% Value = 1 %>"
	const changed = "<% Value = 2 %>"
	if err := os.WriteFile(path, []byte(changed), 0o644); err != nil {
		t.Fatal(err)
	}
	uri := filePathURI(path)
	server := New(nil, io.Discard, io.Discard)
	server.rootPath = root
	server.settings.DiagnosticsDebounceMS = 60_000
	old := core.NewTextDocument(uri, "classic-asp", 1, initial)
	server.mu.Lock()
	server.rememberOpenDocumentLocked(uri, old)
	server.workspace[uri] = core.NewTextDocument(uri, "classic-asp", 0, initial)
	server.parsedCache[parsedDocumentCacheKey(uri)] = parsedDocumentCacheEntry{
		Version: 1, Text: initial, DefaultLanguage: server.settings.DefaultLanguage,
		Parsed: core.ParseDocument(uri, initial, core.Settings{DefaultLanguage: server.settings.DefaultLanguage}),
	}
	server.mu.Unlock()
	started := make(chan struct{})
	release := make(chan struct{})
	server.fileAnalysisSnapshotTestHook = func() {
		select {
		case <-started:
		default:
			close(started)
			<-release
		}
	}
	t.Cleanup(func() {
		select {
		case <-release:
		default:
			close(release)
		}
		server.cancelDocumentOpenAnalysis(uri)
		server.cancelScheduledDiagnostics(uri)
		server.documentOpenAnalysisWorkers.Wait()
	})

	if err := server.handleNotification(context.Background(), "textDocument/didChange", mustRaw(map[string]any{
		"textDocument":   map[string]any{"uri": uri, "version": 2},
		"contentChanges": []map[string]any{{"text": changed}},
	})); err != nil {
		t.Fatal(err)
	}
	select {
	case <-started:
	case <-time.After(2 * time.Second):
		t.Fatal("document revision analysis did not start")
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
		t.Fatal("didSave waited for the pending document revision")
	}
	close(release)
}

func TestDidCloseDoesNotReinsertUnsavedOverlayAnalysis(t *testing.T) {
	const uri = "file:///site/unsaved-only.asp"
	server := New(nil, io.Discard, io.Discard)
	document := core.NewTextDocument(uri, "classic-asp", 4, `<% Dim unsavedOnly %>`)
	server.mu.Lock()
	server.rememberOpenDocumentLocked(uri, document)
	server.rememberDocumentTextLocked(document)
	server.mu.Unlock()

	if err := server.handleNotification(context.Background(), "textDocument/didClose", mustRaw(map[string]any{
		"textDocument": map[string]any{"uri": uri},
	})); err != nil {
		t.Fatal(err)
	}
	server.mu.Lock()
	_, cached := server.parsedCache[parsedDocumentCacheKey(uri)]
	_, textCached := server.parsedCache[parsedTextCacheKey(uri)]
	stored := server.documentStore.Cache[uri]
	server.mu.Unlock()
	if cached || textCached || stored != nil {
		t.Fatalf("didClose retained unsaved overlay analysis: document=%v text=%v stored=%#v", cached, textCached, stored)
	}
}
