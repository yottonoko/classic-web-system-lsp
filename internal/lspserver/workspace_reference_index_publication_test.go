package lspserver

import (
	"io"
	"strings"
	"sync/atomic"
	"testing"
	"time"

	"github.com/yottonoko/classic-web-system-lsp/internal/core"
	workspacepkg "github.com/yottonoko/classic-web-system-lsp/internal/workspace"
)

func TestWorkspaceArtifactReferenceIndexRejectsStaleWorkerAfterLatestPublication(t *testing.T) {
	const uri = "file:///workspace/reference-index-latest.asp"
	oldText := `<% Dim SharedValue : Response.Write SharedValue %>`
	newText := `<% Dim SharedValue : Response.Write SharedValue : Response.Write SharedValue %>`
	server := New(strings.NewReader(""), io.Discard, io.Discard)
	t.Cleanup(server.shutdownRuntimeCaches)
	oldDocument := core.NewTextDocument(uri, "classic-asp", 1, oldText)
	newDocument := core.NewTextDocument(uri, "classic-asp", 2, newText)
	oldParsed := server.parseTextDocument(oldDocument, server.settings.DefaultLanguage)
	newParsed := server.parseTextDocument(newDocument, server.settings.DefaultLanguage)
	server.workspace[uri] = oldDocument

	oldReachedIndexGate := make(chan struct{})
	releaseOld := make(chan struct{})
	var hookCalls atomic.Int32
	workspaceArtifactReferenceIndexTestHook.Lock()
	previousHook := workspaceArtifactReferenceIndexTestHook.fn
	workspaceArtifactReferenceIndexTestHook.fn = func() {
		if hookCalls.Add(1) == 1 {
			close(oldReachedIndexGate)
			<-releaseOld
		}
	}
	workspaceArtifactReferenceIndexTestHook.Unlock()
	defer func() {
		workspaceArtifactReferenceIndexTestHook.Lock()
		workspaceArtifactReferenceIndexTestHook.fn = previousHook
		workspaceArtifactReferenceIndexTestHook.Unlock()
	}()

	oldDone := make(chan workspaceDocumentRevisionResult, 1)
	go func() {
		oldDone <- server.applyWorkspaceDocumentRevision(oldDocument, oldParsed)
	}()
	select {
	case <-oldReachedIndexGate:
	case <-time.After(5 * time.Second):
		t.Fatal("stale artifact worker did not reach the reference-index gate")
	}

	server.mu.Lock()
	server.workspace[uri] = newDocument
	server.mu.Unlock()
	newDone := make(chan workspaceDocumentRevisionResult, 1)
	go func() {
		newDone <- server.applyWorkspaceDocumentRevision(newDocument, newParsed)
	}()
	var newResult workspaceDocumentRevisionResult
	select {
	case newResult = <-newDone:
	case <-time.After(5 * time.Second):
		t.Fatal("latest artifact worker did not publish")
	}
	close(releaseOld)

	var oldResult workspaceDocumentRevisionResult
	select {
	case oldResult = <-oldDone:
	case <-time.After(5 * time.Second):
		t.Fatal("stale artifact worker did not resume")
	}
	if newResult.Manifest == nil || newResult.Stale {
		t.Fatalf("latest artifact result = %#v, want current", newResult)
	}
	if oldResult.Manifest == nil || !oldResult.Stale {
		t.Fatalf("stale artifact result = %#v, want stale", oldResult)
	}

	key := workspacepkg.FileIdentityKeyFromURI(uri)
	server.referenceWorkspaceIndex.mu.RLock()
	entry, indexed := server.referenceWorkspaceIndex.documents[key]
	segment := entry.segments["sharedvalue"]
	server.referenceWorkspaceIndex.mu.RUnlock()
	if !indexed || entry.parsed == nil || entry.parsed.Text != newText || entry.sourceHash != workspacepkg.DiskContentHash(newText) {
		t.Fatalf("reference-index entry = %#v, indexed=%v; want latest parsed source", entry, indexed)
	}
	if segment == nil || segment.parsed != entry.parsed || segment.counts.CodeLensReferences != 2 {
		t.Fatalf("latest reference-index segment = %#v; want two current references", segment)
	}
}
