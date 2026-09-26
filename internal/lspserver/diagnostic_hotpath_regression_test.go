package lspserver

import (
	"context"
	"encoding/json"
	"io"
	"path/filepath"
	"sync"
	"testing"
	"time"

	"github.com/yottonoko/classic-web-system-lsp/internal/core"
)

func TestDidChangeNotificationPreparationReusesDecodedParams(t *testing.T) {
	server := New(nil, io.Discard, io.Discard)
	defer server.shutdownRuntimeCaches()
	server.settings.DiagnosticsDebounceMS = int(time.Hour / time.Millisecond)
	uri := "file:///cached-did-change.asp"
	server.documents[uri] = core.NewTextDocument(uri, "classic-asp", 1, "<% Dim oldValue %>")
	params := mustRaw(map[string]any{
		"textDocument": map[string]any{"uri": uri, "version": 2},
		"contentChanges": []map[string]any{{
			"text": "<% Dim value %>",
		}},
	})
	message := &rpcMessage{Method: "textDocument/didChange", Params: params}
	if !prepareRevisionAdvancingNotification(message) {
		t.Fatal("valid didChange notification was rejected")
	}
	decoded := message.didChangeParams
	if decoded == nil {
		t.Fatal("didChange params were not cached")
	}
	message.Params = json.RawMessage(`{"not":"the original payload"}`)
	if !prepareRevisionAdvancingNotification(message) || message.didChangeParams != decoded {
		t.Fatal("cached didChange params were decoded again")
	}
	if err := server.handleNotificationMessage(context.Background(), message); err != nil {
		t.Fatalf("cached didChange dispatch failed: %v", err)
	}
	updated := server.documentByURI(uri)
	if updated == nil || updated.Version != 2 || updated.Text != "<% Dim value %>" {
		t.Fatalf("cached didChange dispatch produced %#v, want version 2 with the decoded text", updated)
	}
	server.cancelScheduledDiagnostics(uri)
}

func TestScheduleDiagnosticsBoundsSupersededCallbacksWithoutBlockingDidChange(t *testing.T) {
	server := New(nil, io.Discard, io.Discard)
	server.settings.DiagnosticsDebounceMS = 10
	root := t.TempDir()
	uri := filePathURI(filepath.Join(root, "edit.asp"))
	server.rootPath = root
	server.rootURI = filePathURI(root)
	server.workspaceRoots = []workspaceRoot{{Path: root, URI: server.rootURI}}
	server.configureFsGateway()
	server.documents[uri] = core.NewTextDocument(uri, "classic-asp", 1, "<% Dim firstValue %>")

	entered := make(chan struct{})
	release := make(chan struct{})
	oldCallbackDone := make(chan struct{})
	var enteredOnce sync.Once
	var callbackDoneOnce sync.Once
	var releaseOnce sync.Once
	releaseHook := func() { releaseOnce.Do(func() { close(release) }) }
	t.Cleanup(releaseHook)
	server.diagnosticPublicationBatchTestHook = func() {
		enteredOnce.Do(func() { close(entered) })
		<-release
		callbackDoneOnce.Do(func() { close(oldCallbackDone) })
	}
	if err := server.scheduleDiagnostics(uri); err != nil {
		t.Fatal(err)
	}
	select {
	case <-entered:
	case <-time.After(time.Second):
		t.Fatal("superseded diagnostic callback did not reach the publication hook")
	}

	returned := make(chan error, 1)
	go func() {
		returned <- server.handleNotification(context.Background(), "textDocument/didChange", mustRaw(map[string]any{
			"textDocument": map[string]any{"uri": uri, "version": 2},
			"contentChanges": []map[string]any{{
				"text": "<% Dim secondValue %>",
			}},
		}))
	}()
	select {
	case err := <-returned:
		if err != nil {
			t.Fatal(err)
		}
	case <-time.After(time.Second):
		t.Fatal("didChange synchronously drained a superseded diagnostic callback")
	}
	for version := 3; version <= 10; version++ {
		if err := server.handleNotification(context.Background(), "textDocument/didChange", mustRaw(map[string]any{
			"textDocument": map[string]any{"uri": uri, "version": version},
			"contentChanges": []map[string]any{{
				"text": "<% Dim currentValue %>",
			}},
		})); err != nil {
			t.Fatal(err)
		}
		time.Sleep(15 * time.Millisecond)
	}
	deadline := time.Now().Add(time.Second)
	for {
		server.mu.Lock()
		retired := len(server.diagnosticRetiredJobs)
		server.mu.Unlock()
		if retired <= 1 {
			break
		}
		if time.Now().After(deadline) {
			t.Fatalf("superseded diagnostic jobs retained = %d, want at most the one blocked writer", retired)
		}
		time.Sleep(time.Millisecond)
	}

	shutdownDone := make(chan struct{})
	go func() {
		server.shutdownRuntimeCaches()
		close(shutdownDone)
	}()
	select {
	case <-shutdownDone:
		t.Fatal("shutdown lost ownership of the blocked diagnostic callback")
	case <-time.After(50 * time.Millisecond):
	}
	releaseHook()
	select {
	case <-oldCallbackDone:
	case <-time.After(time.Second):
		t.Fatal("superseded diagnostic callback did not finish after release")
	}
	select {
	case <-shutdownDone:
	case <-time.After(time.Second):
		t.Fatal("shutdown did not finish after the diagnostic callback was released")
	}
}

func TestDiagnosticRevisionFastPathRetainsInMemoryFence(t *testing.T) {
	server := New(nil, io.Discard, io.Discard)
	defer server.shutdownRuntimeCaches()
	uri := "file:///diagnostic-revision-fast-path.asp"
	text := "<% Dim value %>"
	document := core.NewTextDocument(uri, "classic-asp", 7, text)
	server.documents[uri] = document

	revision, ok := server.diagnosticTargetRevisionContext(context.Background(), uri)
	if !ok || revision.document != document {
		t.Fatalf("in-memory diagnostic revision = %#v, want document identity", revision)
	}
	if !server.diagnosticTargetRevisionMatchesContext(context.Background(), uri, revision) {
		t.Fatal("matching in-memory diagnostic revision was rejected")
	}
	server.documents[uri] = core.NewTextDocument(uri, "classic-asp", 8, text)
	if server.diagnosticTargetRevisionMatchesContext(context.Background(), uri, revision) {
		t.Fatal("replaced in-memory diagnostic revision crossed the fence")
	}
}
