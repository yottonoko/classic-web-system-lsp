package lspserver

import (
	"io"
	"os"
	"path/filepath"
	"strings"
	"sync/atomic"
	"testing"
	"time"

	"github.com/fxamacker/cbor/v2"
	"github.com/yottonoko/classic-web-system-lsp/internal/core"
	"github.com/yottonoko/classic-web-system-lsp/internal/lsp"
	"github.com/yottonoko/classic-web-system-lsp/internal/vbscript"
	workspacepkg "github.com/yottonoko/classic-web-system-lsp/internal/workspace"
)

func newWorkspaceReferencePersistenceServer(t *testing.T, root, cacheDirectory string) *Server {
	t.Helper()
	server := New(strings.NewReader(""), io.Discard, io.Discard)
	server.rootPath = root
	server.rootURI = filePathURI(root)
	server.workspaceRoots = []workspaceRoot{{URI: server.rootURI, Path: root}}
	server.settings.CacheEnabled = true
	server.settings.CacheDirectory = cacheDirectory
	server.settings.CacheTTLHours = 24
	server.settings.CacheMaxSizeMB = 16
	server.configureDiskAnalysisCache()
	t.Cleanup(server.closeDiskAnalysisCache)
	return server
}

func persistedReferenceManifestForURI(t *testing.T, server *Server, uri string) *persistedWorkspaceReferenceDocument {
	t.Helper()
	if err := server.diskCacheForUse().Flush(); err != nil {
		t.Fatal(err)
	}
	documentKey := workspacepkg.FileIdentityKeyFromURI(uri)
	payload := server.diskCacheForUse().ReadReferenceValuesAligned(workspacepkg.DiskReferenceDocuments, [][]byte{workspaceReferenceDocumentCacheKey(documentKey)})[0]
	if len(payload) == 0 {
		return nil
	}
	var manifest persistedWorkspaceReferenceDocument
	if err := cbor.Unmarshal(payload, &manifest); err != nil {
		t.Fatal(err)
	}
	return &manifest
}

func TestWorkspaceReferencePersistenceWritesOnlyPublishedBackingDocument(t *testing.T) {
	root := t.TempDir()
	server := newWorkspaceReferencePersistenceServer(t, root, filepath.Join(root, "cache"))
	firstURI := filePathURI(filepath.Join(root, "first.asp"))
	secondURI := filePathURI(filepath.Join(root, "second.asp"))
	first := core.NewTextDocument(firstURI, "classic-asp", 0, `<% Dim FirstValue %>`)
	second := core.NewTextDocument(secondURI, "classic-asp", 0, `<% Dim SecondValue %>`)
	server.workspace[firstURI], server.workspace[secondURI] = first, second
	server.applyWorkspaceDocumentRevision(first, server.parseTextDocument(first, server.settings.DefaultLanguage))
	server.applyWorkspaceDocumentRevision(second, server.parseTextDocument(second, server.settings.DefaultLanguage))
	before := persistedReferenceManifestForURI(t, server, secondURI)
	if before == nil {
		t.Fatal("second reference document was not persisted")
	}

	changed := core.NewTextDocument(firstURI, "classic-asp", 1, `<% Dim ChangedValue : ChangedValue = 1 %>`)
	server.workspace[firstURI] = changed
	server.applyWorkspaceDocumentRevision(changed, server.parseTextDocument(changed, server.settings.DefaultLanguage))
	after := persistedReferenceManifestForURI(t, server, secondURI)
	if after == nil || after.SourceHash != before.SourceHash {
		t.Fatalf("unrelated persisted document changed: before=%#v after=%#v", before, after)
	}
	firstManifest := persistedReferenceManifestForURI(t, server, firstURI)
	if firstManifest == nil || firstManifest.SourceHash != workspacepkg.DiskContentHash(changed.Text) {
		t.Fatalf("changed persisted document = %#v", firstManifest)
	}
}

func TestWorkspaceReferencePersistenceSkipsOverlayAndPromotesDuplicateSave(t *testing.T) {
	root := t.TempDir()
	server := newWorkspaceReferencePersistenceServer(t, root, filepath.Join(root, "cache"))
	uri := filePathURI(filepath.Join(root, "default.asp"))
	backing := core.NewTextDocument(uri, "classic-asp", 0, `<% Dim BackingValue %>`)
	overlay := core.NewTextDocument(uri, "classic-asp", 1, `<% Dim OverlayValue : OverlayValue = 1 %>`)
	server.workspace[uri] = backing
	server.applyWorkspaceDocumentRevision(backing, server.parseTextDocument(backing, server.settings.DefaultLanguage))
	server.documents[uri] = overlay
	overlayParsed := server.parseTextDocument(overlay, server.settings.DefaultLanguage)
	server.applyWorkspaceDocumentRevision(overlay, overlayParsed)
	if manifest := persistedReferenceManifestForURI(t, server, uri); manifest == nil || manifest.SourceHash != workspacepkg.DiskContentHash(backing.Text) {
		t.Fatalf("unsaved overlay replaced backing reference document: %#v", manifest)
	}

	server.workspace[uri] = overlay
	result := server.applyWorkspaceDocumentRevision(overlay, overlayParsed)
	if !result.Duplicate {
		t.Fatalf("save promotion did not reuse the published artifact: %#v", result)
	}
	if manifest := persistedReferenceManifestForURI(t, server, uri); manifest == nil || manifest.SourceHash != workspacepkg.DiskContentHash(overlay.Text) {
		t.Fatalf("duplicate save did not promote reference document: %#v", manifest)
	}
}

func TestWorkspaceReferencePersistenceCloseRestoresBackingAndDeleteTombstones(t *testing.T) {
	root := t.TempDir()
	server := newWorkspaceReferencePersistenceServer(t, root, filepath.Join(root, "cache"))
	uri := filePathURI(filepath.Join(root, "default.asp"))
	backing := core.NewTextDocument(uri, "classic-asp", 0, `<% Dim BackingValue %>`)
	overlay := core.NewTextDocument(uri, "classic-asp", 1, `<% Dim OverlayValue %>`)
	server.workspace[uri], server.documents[uri] = backing, overlay
	server.applyWorkspaceDocumentRevision(overlay, server.parseTextDocument(overlay, server.settings.DefaultLanguage))
	if err := server.handleNotification(t.Context(), "textDocument/didClose", mustRaw(map[string]any{
		"textDocument": map[string]any{"uri": uri},
	})); err != nil {
		t.Fatal(err)
	}
	manifest := persistedReferenceManifestForURI(t, server, uri)
	if manifest == nil || manifest.SourceHash != workspacepkg.DiskContentHash(backing.Text) {
		t.Fatalf("close did not restore backing reference document: %#v", manifest)
	}
	postingKeys := append([]string(nil), manifest.PostingKeys...)

	server.removeWorkspaceDocumentRevision(uri)
	if got := persistedReferenceManifestForURI(t, server, uri); got != nil {
		t.Fatalf("deleted reference manifest survived: %#v", got)
	}
	for _, name := range postingKeys {
		key := []byte(workspaceReferencePostingCacheKey(workspacepkg.FileIdentityKeyFromURI(uri), name))
		if value := server.diskCacheForUse().ReadReferenceValuesAligned(workspacepkg.DiskReferencePostings, [][]byte{key})[0]; value != nil {
			t.Fatalf("deleted posting %q survived", name)
		}
	}
}

func TestWorkspaceReferencePersistenceRenameTombstonesOldDocument(t *testing.T) {
	root := t.TempDir()
	server := newWorkspaceReferencePersistenceServer(t, root, filepath.Join(root, "cache"))
	oldURI := filePathURI(filepath.Join(root, "old.asp"))
	newURI := filePathURI(filepath.Join(root, "new.asp"))
	oldDoc := core.NewTextDocument(oldURI, "classic-asp", 0, `<% Dim RenamedValue %>`)
	server.workspace[oldURI] = oldDoc
	server.applyWorkspaceDocumentRevision(oldDoc, server.parseTextDocument(oldDoc, server.settings.DefaultLanguage))
	if persistedReferenceManifestForURI(t, server, oldURI) == nil {
		t.Fatal("old reference document was not persisted")
	}
	server.removeWorkspaceDocumentRevision(oldURI)
	delete(server.workspace, oldURI)
	newDoc := core.NewTextDocument(newURI, "classic-asp", 0, oldDoc.Text)
	server.workspace[newURI] = newDoc
	server.applyWorkspaceDocumentRevision(newDoc, server.parseTextDocument(newDoc, server.settings.DefaultLanguage))
	if oldManifest := persistedReferenceManifestForURI(t, server, oldURI); oldManifest != nil {
		t.Fatalf("old renamed reference document survived: %#v", oldManifest)
	}
	if newManifest := persistedReferenceManifestForURI(t, server, newURI); newManifest == nil || newManifest.DocumentKey != workspacepkg.FileIdentityKeyFromURI(newURI) {
		t.Fatalf("new renamed reference document = %#v", newManifest)
	}
}

func TestWorkspaceReferencePersistenceDeleteWinsFinalQueueRace(t *testing.T) {
	root := t.TempDir()
	server := newWorkspaceReferencePersistenceServer(t, root, filepath.Join(root, "cache"))
	uri := filePathURI(filepath.Join(root, "default.asp"))
	doc := core.NewTextDocument(uri, "classic-asp", 0, `<% Dim SharedValue : SharedValue = 1 %>`)
	server.workspace[uri] = doc
	parsed := server.parseTextDocument(doc, server.settings.DefaultLanguage)
	server.applyWorkspaceDocumentRevision(doc, parsed)
	if persistedReferenceManifestForURI(t, server, uri) == nil {
		t.Fatal("reference document was not persisted")
	}
	documentID := workspaceDocumentIDFromURI(uri)
	server.mu.Lock()
	manifest := server.workspaceArtifacts[documentID]
	revision := server.workspaceArtifactRevisions[documentID]
	server.mu.Unlock()

	reachedQueue := make(chan struct{})
	releaseQueue := make(chan struct{})
	server.workspaceReferencePersistenceBeforeQueueTestHook = func() {
		close(reachedQueue)
		<-releaseQueue
	}
	persistDone := make(chan struct{})
	go func() {
		defer close(persistDone)
		server.persistWorkspaceReferenceDocument(manifest, vbscript.BuildReferenceShard(parsed), revision)
	}()
	<-reachedQueue
	deleteStarted := make(chan struct{})
	deleteDone := make(chan struct{})
	go func() {
		defer close(deleteDone)
		close(deleteStarted)
		server.removeWorkspaceDocumentRevision(uri)
	}()
	<-deleteStarted
	close(releaseQueue)
	<-persistDone
	<-deleteDone
	server.workspaceReferencePersistenceBeforeQueueTestHook = nil
	if got := persistedReferenceManifestForURI(t, server, uri); got != nil {
		t.Fatalf("stale document write won over tombstone: %#v", got)
	}
}

func TestWorkspaceReferencePersistenceRejectsStaleOverlayOnRestart(t *testing.T) {
	root := t.TempDir()
	cacheDirectory := filepath.Join(root, "cache")
	uri := filePathURI(filepath.Join(root, "default.asp"))
	overlay := core.NewTextDocument(uri, "classic-asp", 1, `<% Dim OverlayOnlyValue %>`)
	first := New(strings.NewReader(""), io.Discard, io.Discard)
	first.rootPath, first.rootURI = root, filePathURI(root)
	first.workspaceRoots = []workspaceRoot{{URI: first.rootURI, Path: root}}
	first.settings.CacheEnabled, first.settings.CacheDirectory = true, cacheDirectory
	first.settings.CacheTTLHours, first.settings.CacheMaxSizeMB = 24, 16
	first.configureDiskAnalysisCache()
	first.workspace[uri] = overlay
	first.applyWorkspaceDocumentRevision(overlay, first.parseTextDocument(overlay, first.settings.DefaultLanguage))
	if err := first.diskCacheForUse().Flush(); err != nil {
		t.Fatal(err)
	}
	first.closeDiskAnalysisCache()

	backing := core.NewTextDocument(uri, "classic-asp", 0, `<% Dim BackingOnlyValue %>`)
	second := newWorkspaceReferencePersistenceServer(t, root, cacheDirectory)
	second.workspace[uri] = backing
	parsed := core.ParseDocument(uri, backing.Text, core.Settings{DefaultLanguage: second.settings.DefaultLanguage})
	second.restoreWorkspaceReferenceShards([]*core.ParsedDocument{parsed})
	if _, restored := vbscript.CachedReferenceShard(parsed); restored {
		t.Fatal("stale overlay reference shard was restored for different backing source")
	}
}

func TestWorkspaceDocumentRevisionDeduplicatesSameSourceAcrossVersions(t *testing.T) {
	server := New(strings.NewReader(""), io.Discard, io.Discard)
	uri := "file:///workspace/default.asp"
	text := `<% Dim Value : Value = 1 %>`
	first := core.NewTextDocument(uri, "classic-asp", 1, text)
	server.documents[uri] = first
	parses := 0
	artifacts := 0
	server.documentParseTestHook = func(string) { parses++ }
	server.fileAnalysisSnapshotTestHook = func() { artifacts++ }

	firstParsed := server.parseTextDocument(first, server.settings.DefaultLanguage)
	firstResult := server.applyWorkspaceDocumentRevision(first, firstParsed)
	if firstResult.Duplicate || firstResult.Stale || firstResult.Manifest == nil {
		t.Fatalf("first revision = %#v", firstResult)
	}
	server.mu.Lock()
	firstGeneration := server.javascriptDocumentGeneration
	server.mu.Unlock()

	second := core.NewTextDocument(uri, "classic-asp", 2, text)
	server.mu.Lock()
	server.rememberOpenDocumentLocked(uri, second)
	server.mu.Unlock()
	secondParsed := server.parseTextDocument(second, server.settings.DefaultLanguage)
	secondResult := server.applyWorkspaceDocumentRevision(second, secondParsed)
	if !secondResult.Duplicate || secondResult.Stale {
		t.Fatalf("same-source revision = %#v", secondResult)
	}
	if secondParsed != firstParsed || parses != 1 || artifacts != 1 {
		t.Fatalf("same-source work: sameParsed=%v parses=%d artifacts=%d", secondParsed == firstParsed, parses, artifacts)
	}
	server.mu.Lock()
	generation := server.javascriptDocumentGeneration
	revision := server.workspaceArtifactRevisions[workspaceDocumentIDFromURI(uri)]
	server.mu.Unlock()
	if generation != firstGeneration || revision != 1 {
		t.Fatalf("duplicate publish changed state: generation=%d/%d revision=%d", generation, firstGeneration, revision)
	}
}

func TestWorkspaceDocumentRevisionDeduplicatesDidChangeSaveAndWatcher(t *testing.T) {
	root := t.TempDir()
	fileName := filepath.Join(root, "default.asp")
	uri := filePathURI(fileName)
	initialText := `<% Value = 1 %>`
	changedText := `<% Value = 2 %>`
	if err := os.WriteFile(fileName, []byte(initialText), 0o644); err != nil {
		t.Fatal(err)
	}
	server := New(strings.NewReader(""), io.Discard, io.Discard)
	server.rootPath = root
	server.rootURI = filePathURI(root)
	server.workspaceRoots = []workspaceRoot{{URI: server.rootURI, Path: root}}
	doc := core.NewTextDocument(uri, "classic-asp", 1, initialText)
	server.documents[uri] = doc
	server.workspace[uri] = core.NewTextDocument(uri, "classic-asp", 0, initialText)
	server.applyWorkspaceDocumentRevision(doc, server.parseTextDocument(doc, server.settings.DefaultLanguage))
	documentID := workspaceDocumentIDFromURI(uri)
	baselineRevision := server.workspaceArtifactRevisions[documentID]
	parses := 0
	artifacts := 0
	server.documentParseTestHook = func(string) { parses++ }
	server.fileAnalysisSnapshotTestHook = func() { artifacts++ }

	if err := server.handleNotification(t.Context(), "textDocument/didChange", mustRaw(map[string]any{
		"textDocument":   map[string]any{"uri": uri, "version": 2},
		"contentChanges": []map[string]any{{"text": changedText}},
	})); err != nil {
		t.Fatal(err)
	}
	if err := os.WriteFile(fileName, []byte(changedText), 0o644); err != nil {
		t.Fatal(err)
	}
	if err := server.handleNotification(t.Context(), "textDocument/didSave", mustRaw(map[string]any{"textDocument": map[string]any{"uri": uri}})); err != nil {
		t.Fatal(err)
	}
	if err := server.didChangeWatchedFiles(didChangeWatchedFilesParams{Changes: []fileEvent{{URI: uri, Type: fileChangeChanged}}}); err != nil {
		t.Fatal(err)
	}

	if parses != 1 || artifacts != 1 {
		t.Fatalf("duplicate event work: parses=%d artifacts=%d", parses, artifacts)
	}
	if revision := server.workspaceArtifactRevisions[documentID]; revision != baselineRevision+1 {
		t.Fatalf("semantic revisions = %d, want %d", revision, baselineRevision+1)
	}
}

func TestWorkspaceReferenceLocationDeltaPreservesCountCache(t *testing.T) {
	server := New(strings.NewReader(""), io.Discard, io.Discard)
	foo := workspaceReferenceTargetKey{URI: "file:///workspace/default.asp", Name: "Foo", NameHash: workspaceReferenceNameHash("foo")}
	bar := workspaceReferenceTargetKey{URI: "file:///workspace/default.asp", Name: "Bar", NameHash: workspaceReferenceNameHash("bar")}
	server.referenceCounts[foo] = 12
	server.referenceCounts[bar] = 4
	server.referenceResults[foo] = []lsp.Location{{URI: foo.URI}}
	server.referenceResults[bar] = []lsp.Location{{URI: bar.URI}}

	server.invalidateWorkspaceReferenceLocations([]string{"foo"})
	if server.referenceCounts[foo] != 12 || server.referenceCounts[bar] != 4 {
		t.Fatalf("location-only invalidation changed counts: %#v", server.referenceCounts)
	}
	if _, ok := server.referenceResults[foo]; ok {
		t.Fatal("changed location result was preserved")
	}
	if _, ok := server.referenceResults[bar]; !ok {
		t.Fatal("unrelated location result was invalidated")
	}
}

func TestWorkspaceDocumentRevisionRejectsStalePublishBeforeArtifactBuild(t *testing.T) {
	server := New(strings.NewReader(""), io.Discard, io.Discard)
	uri := "file:///workspace/default.asp"
	stale := core.NewTextDocument(uri, "classic-asp", 1, `<% Value = 1 %>`)
	parsed := core.ParseDocument(uri, stale.Text, core.Settings{DefaultLanguage: server.settings.DefaultLanguage})
	current := core.NewTextDocument(uri, "classic-asp", 2, `<% Value = 2 %>`)
	server.documents[uri] = current
	builds := 0
	server.fileAnalysisSnapshotTestHook = func() { builds++ }

	result := server.applyWorkspaceDocumentRevision(stale, parsed)
	if !result.Stale || result.Manifest != nil || builds != 0 {
		t.Fatalf("stale revision = %#v, builds=%d", result, builds)
	}
	if len(server.workspaceArtifacts) != 0 {
		t.Fatalf("stale revision was published: %#v", server.workspaceArtifacts)
	}
}

func TestWorkspaceDocumentArtifactQueueOrdersRevisionsAcrossFinalCheck(t *testing.T) {
	root := t.TempDir()
	server := newWorkspaceReferencePersistenceServer(t, root, filepath.Join(root, "cache"))
	uri := filePathURI(filepath.Join(root, "default.asp"))
	documentID := workspaceDocumentIDFromURI(uri)
	oldManifest := &workspaceDocumentArtifactManifest{
		DocumentID:        documentID,
		URI:               uri,
		SourceFingerprint: "old-source",
	}
	newManifest := &workspaceDocumentArtifactManifest{
		DocumentID:        documentID,
		URI:               uri,
		SourceFingerprint: "new-source",
	}
	server.mu.Lock()
	server.workspaceArtifacts[documentID] = oldManifest
	server.workspaceArtifactRevisions[documentID] = 1
	server.mu.Unlock()

	oldReachedFinalCheck := make(chan struct{})
	newReachedFinalCheck := make(chan struct{})
	releaseOld := make(chan struct{})
	var hookCalls atomic.Int32
	workspaceArtifactQueueTestHook.Lock()
	previousHook := workspaceArtifactQueueTestHook.fn
	workspaceArtifactQueueTestHook.fn = func() {
		switch hookCalls.Add(1) {
		case 1:
			close(oldReachedFinalCheck)
			<-releaseOld
		case 2:
			close(newReachedFinalCheck)
		}
	}
	workspaceArtifactQueueTestHook.Unlock()
	defer func() {
		workspaceArtifactQueueTestHook.Lock()
		workspaceArtifactQueueTestHook.fn = previousHook
		workspaceArtifactQueueTestHook.Unlock()
	}()

	oldDone := make(chan bool, 1)
	go func() {
		oldDone <- server.queueWorkspaceDocumentArtifactDeltaIfCurrent(oldManifest, workspaceDocumentArtifactDelta{}, 1)
	}()
	select {
	case <-oldReachedFinalCheck:
	case <-time.After(5 * time.Second):
		t.Fatal("old artifact queue did not reach the final currentness gate")
	}

	server.mu.Lock()
	server.workspaceArtifacts[documentID] = newManifest
	server.workspaceArtifactRevisions[documentID] = 2
	server.mu.Unlock()
	newDone := make(chan bool, 1)
	go func() {
		newDone <- server.queueWorkspaceDocumentArtifactDeltaIfCurrent(newManifest, workspaceDocumentArtifactDelta{}, 2)
	}()

	select {
	case <-newReachedFinalCheck:
		select {
		case <-newDone:
		case <-time.After(5 * time.Second):
			t.Fatal("new artifact queue did not complete after its final check")
		}
	case <-time.After(100 * time.Millisecond):
		// The newer revision is waiting for the older queue owner. Releasing
		// the owner below lets the queues commit in revision order.
	}
	close(releaseOld)
	select {
	case <-oldDone:
	case <-time.After(5 * time.Second):
		t.Fatal("old artifact queue did not complete")
	}
	select {
	case <-newDone:
	case <-time.After(5 * time.Second):
		t.Fatal("new artifact queue did not complete")
	}

	if err := server.diskCacheForUse().Flush(); err != nil {
		t.Fatal(err)
	}
	head := server.diskCacheForUse().ReadDocumentHeadsAligned([]string{string(documentID)})[0]
	if head == nil || head.SourceHash != string(newManifest.SourceFingerprint) {
		t.Fatalf("persisted artifact head = %#v, want latest source", head)
	}
}

func TestWorkspaceDocumentRevisionRejectsOutOfOrderArtifactEffects(t *testing.T) {
	root := t.TempDir()
	ownerPath := filepath.Join(root, "default.asp")
	firstIncludePath := filepath.Join(root, "first.inc")
	secondIncludePath := filepath.Join(root, "second.inc")
	for _, path := range []string{ownerPath, firstIncludePath, secondIncludePath} {
		if err := os.WriteFile(path, nil, 0o644); err != nil {
			t.Fatal(err)
		}
	}
	uri := filePathURI(ownerPath)
	oldText := `<div class="old"></div><!-- #include file="first.inc" --><% Dim OldValue %>`
	newText := `<div class="new"></div><!-- #include file="second.inc" --><% Dim NewValue %>`
	oldDocument := core.NewTextDocument(uri, "classic-asp", 1, oldText)
	newDocument := core.NewTextDocument(uri, "classic-asp", 2, newText)
	server := newWorkspaceReferencePersistenceServer(t, root, filepath.Join(root, "cache"))
	server.rootPath = root
	server.rootURI = filePathURI(root)
	server.workspaceRoots = []workspaceRoot{{URI: server.rootURI, Path: root}}
	server.workspace[uri] = oldDocument
	oldParsed := server.parseTextDocument(oldDocument, server.settings.DefaultLanguage)
	newParsed := server.parseTextDocument(newDocument, server.settings.DefaultLanguage)

	oldEntered := make(chan struct{})
	newEntered := make(chan struct{})
	releaseOld := make(chan struct{})
	releaseNew := make(chan struct{})
	var hookCalls atomic.Int32
	server.referenceWorkspaceIndex.embeddedBuildTestHook = func(string) {
		switch hookCalls.Add(1) {
		case 1:
			close(oldEntered)
			<-releaseOld
		case 2:
			close(newEntered)
			<-releaseNew
		default:
			panic("unexpected embedded reference index build")
		}
	}
	defer func() { server.referenceWorkspaceIndex.embeddedBuildTestHook = nil }()

	oldDone := make(chan workspaceDocumentRevisionResult, 1)
	go func() {
		oldDone <- server.applyWorkspaceDocumentRevision(oldDocument, oldParsed)
	}()
	select {
	case <-oldEntered:
	case <-time.After(5 * time.Second):
		t.Fatal("old artifact analysis did not reach the ordering gate")
	}

	server.mu.Lock()
	server.workspace[uri] = newDocument
	server.mu.Unlock()
	newDone := make(chan workspaceDocumentRevisionResult, 1)
	go func() {
		newDone <- server.applyWorkspaceDocumentRevision(newDocument, newParsed)
	}()
	select {
	case <-newEntered:
	case <-time.After(5 * time.Second):
		t.Fatal("new artifact analysis did not reach the ordering gate")
	}

	close(releaseNew)
	var newResult workspaceDocumentRevisionResult
	select {
	case newResult = <-newDone:
	case <-time.After(5 * time.Second):
		t.Fatal("new artifact analysis did not complete")
	}
	close(releaseOld)
	var oldResult workspaceDocumentRevisionResult
	select {
	case oldResult = <-oldDone:
	case <-time.After(5 * time.Second):
		t.Fatal("old artifact analysis did not complete")
	}

	if oldResult.Manifest == nil || !oldResult.Stale {
		t.Fatalf("old out-of-order result = %#v, want stale", oldResult)
	}
	if newResult.Manifest == nil || newResult.Stale {
		t.Fatalf("new out-of-order result = %#v, want current", newResult)
	}
	server.mu.Lock()
	entry, ok := server.workspaceIncludeGraph.Get(ownerPath)
	server.mu.Unlock()
	if !ok || len(entry.TargetFileNames) != 1 || entry.TargetFileNames[0] != filepath.Clean(secondIncludePath) {
		t.Fatalf("include graph entry = %#v, ok=%v; want latest include", entry, ok)
	}
	if err := server.diskCacheForUse().Flush(); err != nil {
		t.Fatal(err)
	}
	head := server.diskCacheForUse().ReadDocumentHeadsAligned([]string{string(workspaceDocumentIDFromURI(uri))})[0]
	if head == nil || head.SourceHash != workspacepkg.DiskContentHash(newText) {
		t.Fatalf("persisted artifact head = %#v, want latest source", head)
	}
}

func TestDidChangeIncludeTopologyInvalidatesOnlyAffectedFamily(t *testing.T) {
	root := t.TempDir()
	ownerPath := filepath.Join(root, "default.asp")
	firstInclude := filepath.Join(root, "first.inc")
	secondInclude := filepath.Join(root, "second.inc")
	for _, fileName := range []string{ownerPath, firstInclude, secondInclude} {
		if err := os.WriteFile(fileName, nil, 0o644); err != nil {
			t.Fatal(err)
		}
	}
	uri := filePathURI(ownerPath)
	initialText := `<!-- #include file="first.inc" --><% SharedValue = 1 %>`
	changedText := `<!-- #include file="second.inc" --><% SharedValue = 1 %>`
	server := New(strings.NewReader(""), io.Discard, io.Discard)
	server.rootPath = root
	server.rootURI = filePathURI(root)
	server.workspaceRoots = []workspaceRoot{{URI: server.rootURI, Path: root}}
	server.workspaceIncludeGraph.Reset(server.workspaceDiskSettingsKey())
	server.workspaceIncludeGraphComplete = true
	doc := core.NewTextDocument(uri, "classic-asp", 1, initialText)
	server.documents[uri] = doc
	server.applyWorkspaceDocumentRevision(doc, server.parseTextDocument(doc, server.settings.DefaultLanguage))
	affected := workspaceReferenceTargetKey{URI: uri, Name: "sharedvalue"}
	unrelated := workspaceReferenceTargetKey{URI: "file:///workspace/unrelated.asp", Name: "sentinel"}
	server.referenceCounts[affected] = 3
	server.referenceCounts[unrelated] = 12

	if err := server.handleNotification(t.Context(), "textDocument/didChange", mustRaw(map[string]any{
		"textDocument":   map[string]any{"uri": uri, "version": 2},
		"contentChanges": []map[string]any{{"text": changedText}},
	})); err != nil {
		t.Fatal(err)
	}
	if _, ok := server.referenceCounts[affected]; ok {
		t.Fatal("affected include-family count survived didChange")
	}
	if got := server.referenceCounts[unrelated]; got != 12 {
		t.Fatalf("unrelated count = %d, want 12", got)
	}
}

func TestDidClosePublishesBackingWorkspaceArtifact(t *testing.T) {
	root := t.TempDir()
	fileName := filepath.Join(root, "default.asp")
	uri := filePathURI(fileName)
	backingText := `<% Dim BackingValue : BackingValue = 1 %>`
	overlayText := `<% Dim OverlayValue : OverlayValue = 1 %>`
	if err := os.WriteFile(fileName, []byte(backingText), 0o644); err != nil {
		t.Fatal(err)
	}
	server := New(strings.NewReader(""), io.Discard, io.Discard)
	server.rootPath = root
	server.rootURI = filePathURI(root)
	backing := core.NewTextDocument(uri, "classic-asp", 0, backingText)
	overlay := core.NewTextDocument(uri, "classic-asp", 1, overlayText)
	server.workspace[uri] = backing
	server.documents[uri] = overlay
	server.applyWorkspaceDocumentRevision(overlay, server.parseTextDocument(overlay, server.settings.DefaultLanguage))
	documentID := workspaceDocumentIDFromURI(uri)
	beforeRevision := server.workspaceArtifactRevisions[documentID]

	if err := server.handleNotification(t.Context(), "textDocument/didClose", mustRaw(map[string]any{
		"textDocument": map[string]any{"uri": uri},
	})); err != nil {
		t.Fatal(err)
	}
	manifest := server.workspaceArtifacts[documentID]
	if manifest == nil || manifest.CST == nil || manifest.CST.Text != backingText {
		t.Fatalf("backing artifact was not published: %#v", manifest)
	}
	if got := server.workspaceArtifactRevisions[documentID]; got != beforeRevision+1 {
		t.Fatalf("artifact revision = %d, want %d", got, beforeRevision+1)
	}
}

func TestWorkspaceReferenceRequestKeyKeepsExactNormalizedName(t *testing.T) {
	key := workspaceReferenceRequestKey("file:///workspace/default.asp", lsp.Position{}, false, "variable", 1, "SharedValue")
	if key.Name != "sharedvalue" {
		t.Fatalf("normalized name = %q, want sharedvalue", key.Name)
	}
	collidingHash := workspaceReferenceTargetKey{Name: "other", NameHash: workspaceReferenceNameHash("sharedvalue")}
	if workspaceReferenceTargetAffected(collidingHash, map[string]struct{}{"sharedvalue": {}}) {
		t.Fatal("exact name was ignored in favor of the legacy hash")
	}
}

func TestASPFileOperationsStayIncremental(t *testing.T) {
	newServer := func(t *testing.T, root string) *Server {
		t.Helper()
		server := New(strings.NewReader(""), io.Discard, io.Discard)
		server.rootPath = root
		server.rootURI = filePathURI(root)
		server.workspaceRoots = []workspaceRoot{{URI: server.rootURI, Path: root}}
		server.workspaceIncludeGraph.Reset(server.workspaceDiskSettingsKey())
		server.workspaceIncludeGraphComplete = true
		server.workspaceIndexEnabled = true
		server.workspaceIndexGeneration = 7
		return server
	}

	t.Run("create", func(t *testing.T) {
		root := t.TempDir()
		fileName := filepath.Join(root, "created.asp")
		uri := filePathURI(fileName)
		if err := os.WriteFile(fileName, []byte(`<% Dim CreatedValue %>`), 0o644); err != nil {
			t.Fatal(err)
		}
		server := newServer(t, root)
		referenceGeneration := server.referenceGeneration
		indexGeneration := server.workspaceIndexGeneration

		if err := server.handleNotification(t.Context(), "workspace/didCreateFiles", mustRaw(map[string]any{
			"files": []map[string]any{{"uri": uri}},
		})); err != nil {
			t.Fatal(err)
		}
		if server.referenceGeneration != referenceGeneration || server.workspaceIndexGeneration != indexGeneration {
			t.Fatalf("create used broad recovery: reference=%d/%d index=%d/%d", server.referenceGeneration, referenceGeneration, server.workspaceIndexGeneration, indexGeneration)
		}
		if manifest := server.workspaceArtifacts[workspaceDocumentIDFromURI(uri)]; manifest == nil {
			t.Fatal("created document artifact was not published")
		}
	})

	t.Run("delete", func(t *testing.T) {
		root := t.TempDir()
		fileName := filepath.Join(root, "deleted.asp")
		uri := filePathURI(fileName)
		text := `<% Dim DeletedValue : DeletedValue = 1 %>`
		if err := os.WriteFile(fileName, []byte(text), 0o644); err != nil {
			t.Fatal(err)
		}
		server := newServer(t, root)
		doc := core.NewTextDocument(uri, "classic-asp", 0, text)
		server.workspace[uri] = doc
		parsed := server.parseTextDocument(doc, server.settings.DefaultLanguage)
		server.applyWorkspaceDocumentRevision(doc, parsed)
		server.referenceWorkspaceIndex.update([]*core.ParsedDocument{parsed})
		referenceGeneration := server.referenceGeneration
		indexGeneration := server.workspaceIndexGeneration
		if err := os.Remove(fileName); err != nil {
			t.Fatal(err)
		}

		if err := server.handleNotification(t.Context(), "workspace/didDeleteFiles", mustRaw(map[string]any{
			"files": []map[string]any{{"uri": uri}},
		})); err != nil {
			t.Fatal(err)
		}
		if server.referenceGeneration != referenceGeneration || server.workspaceIndexGeneration != indexGeneration {
			t.Fatalf("delete used broad recovery: reference=%d/%d index=%d/%d", server.referenceGeneration, referenceGeneration, server.workspaceIndexGeneration, indexGeneration)
		}
		if manifest := server.workspaceArtifacts[workspaceDocumentIDFromURI(uri)]; manifest != nil {
			t.Fatal("deleted document artifact survived")
		}
		server.referenceWorkspaceIndex.mu.RLock()
		_, indexed := server.referenceWorkspaceIndex.documents[workspacepkg.FileIdentityKeyFromURI(uri)]
		server.referenceWorkspaceIndex.mu.RUnlock()
		if indexed {
			t.Fatal("deleted document reference segment survived")
		}
	})

	t.Run("rename", func(t *testing.T) {
		root := t.TempDir()
		oldName := filepath.Join(root, "old.asp")
		newName := filepath.Join(root, "new.asp")
		oldURI := filePathURI(oldName)
		newURI := filePathURI(newName)
		text := `<% Dim RenamedValue : RenamedValue = 1 %>`
		if err := os.WriteFile(oldName, []byte(text), 0o644); err != nil {
			t.Fatal(err)
		}
		server := newServer(t, root)
		doc := core.NewTextDocument(oldURI, "classic-asp", 0, text)
		server.workspace[oldURI] = doc
		server.applyWorkspaceDocumentRevision(doc, server.parseTextDocument(doc, server.settings.DefaultLanguage))
		referenceGeneration := server.referenceGeneration
		indexGeneration := server.workspaceIndexGeneration
		if err := os.Rename(oldName, newName); err != nil {
			t.Fatal(err)
		}

		if err := server.handleNotification(t.Context(), "workspace/didRenameFiles", mustRaw(map[string]any{
			"files": []map[string]any{{"oldUri": oldURI, "newUri": newURI}},
		})); err != nil {
			t.Fatal(err)
		}
		if server.referenceGeneration != referenceGeneration || server.workspaceIndexGeneration != indexGeneration {
			t.Fatalf("rename used broad recovery: reference=%d/%d index=%d/%d", server.referenceGeneration, referenceGeneration, server.workspaceIndexGeneration, indexGeneration)
		}
		if server.workspaceArtifacts[workspaceDocumentIDFromURI(oldURI)] != nil {
			t.Fatal("old rename artifact survived")
		}
		if server.workspaceArtifacts[workspaceDocumentIDFromURI(newURI)] == nil {
			t.Fatal("new rename artifact was not published")
		}
	})
}

func TestIncompleteIncludeGraphInvalidatesAllWorkspaceDiagnosticItems(t *testing.T) {
	root := t.TempDir()
	ownerPath := filepath.Join(root, "default.asp")
	includePath := filepath.Join(root, "shared.inc")
	unrelatedPath := filepath.Join(root, "unrelated.asp")
	ownerText := `<!-- #include file="shared.inc" --><% Response.Write "ok" %>`
	for path, text := range map[string]string{
		ownerPath:     ownerText,
		includePath:   `<% Const SharedValue = "ok" %>`,
		unrelatedPath: `<% Response.Write "unrelated" %>`,
	} {
		if err := os.WriteFile(path, []byte(text), 0o644); err != nil {
			t.Fatal(err)
		}
	}
	server := New(strings.NewReader(""), io.Discard, io.Discard)
	server.rootPath = root
	server.rootURI = filePathURI(root)
	server.workspaceRoots = []workspaceRoot{{URI: server.rootURI, Path: root}}
	owner := core.NewTextDocument(filePathURI(ownerPath), "classic-asp", 0, ownerText)
	include := core.NewTextDocument(filePathURI(includePath), "classic-asp", 0, `<% Const SharedValue = "ok" %>`)
	unrelated := core.NewTextDocument(filePathURI(unrelatedPath), "classic-asp", 0, `<% Response.Write "unrelated" %>`)
	server.workspace[owner.URI] = owner
	server.workspace[include.URI] = include
	server.workspace[unrelated.URI] = unrelated
	server.workspaceIncludeGraph.Reset(server.workspaceDiskSettingsKey())
	server.workspaceIncludeGraphComplete = false
	settingsKey := server.workspaceDiagnosticsSettingsFingerprint()
	for _, document := range []*core.TextDocument{owner, unrelated} {
		key := workspacepkg.FileIdentityKeyFromURI(document.URI)
		server.workspaceDiagnosticsItems[key] = workspaceDiagnosticsItemCacheEntry{document: document, settingsKey: settingsKey}
	}
	if err := os.Remove(includePath); err != nil {
		t.Fatal(err)
	}

	if err := server.didChangeWatchedFiles(didChangeWatchedFilesParams{Changes: []fileEvent{{URI: include.URI, Type: fileChangeDeleted}}}); err != nil {
		t.Fatal(err)
	}
	for _, document := range []*core.TextDocument{owner, unrelated} {
		key := workspacepkg.FileIdentityKeyFromURI(document.URI)
		if _, ok := server.workspaceDiagnosticsItems[key]; ok {
			t.Fatalf("incomplete graph preserved stale workspace diagnostics for %s", document.URI)
		}
		if server.workspaceDiagnosticsRevisions[key] == 0 {
			t.Fatalf("workspace diagnostic revision was not advanced for %s", document.URI)
		}
	}
}
