package lspserver

import (
	"context"
	"io"
	"path/filepath"
	"strings"
	"sync/atomic"
	"testing"

	"github.com/yottonoko/classic-web-system-lsp/internal/core"
)

func TestDidOpenIdenticalSourceReusesPublishedArtifact(t *testing.T) {
	root := t.TempDir()
	uri := filePathURI(filepath.Join(root, "indexed.asp"))
	text := "<% Dim IndexedValue %>"
	server, parsed := newIndexedDocumentOpenCacheTestServer(t, root, uri, text)
	var parses atomic.Int32
	server.documentParseTestHook = func(string) { parses.Add(1) }

	if err := server.handleNotification(context.Background(), "textDocument/didOpen", mustRaw(map[string]any{
		"textDocument": map[string]any{
			"uri": uri, "languageId": "classic-asp", "version": 1, "text": text,
		},
	})); err != nil {
		t.Fatal(err)
	}
	server.mu.Lock()
	artifact := server.workspaceArtifacts[workspaceDocumentIDFromURI(uri)]
	artifactRevision := server.workspaceArtifactRevisions[workspaceDocumentIDFromURI(uri)]
	graphGeneration := server.graphGeneration
	referenceGeneration := server.referenceGeneration
	analysisSequence := server.documentOpenAnalysisSequence
	server.mu.Unlock()
	if got := parses.Load(); got != 0 {
		t.Fatalf("first identical didOpen reparsed source %d times", got)
	}

	if err := server.handleNotification(context.Background(), "textDocument/didOpen", mustRaw(map[string]any{
		"textDocument": map[string]any{
			"uri": uri, "languageId": "classic-asp", "version": 2, "text": text,
		},
	})); err != nil {
		t.Fatal(err)
	}
	server.mu.Lock()
	currentArtifact := server.workspaceArtifacts[workspaceDocumentIDFromURI(uri)]
	currentRevision := server.workspaceArtifactRevisions[workspaceDocumentIDFromURI(uri)]
	currentGraphGeneration := server.graphGeneration
	currentReferenceGeneration := server.referenceGeneration
	currentAnalysisSequence := server.documentOpenAnalysisSequence
	currentParsed := server.parsedCache[parsedDocumentCacheKey(uri)].Parsed
	server.mu.Unlock()
	if got := parses.Load(); got != 0 {
		t.Fatalf("repeated identical didOpen reparsed source %d times", got)
	}
	if currentParsed != parsed {
		t.Fatalf("repeated identical didOpen replaced parsed document: got=%p want=%p", currentParsed, parsed)
	}
	if currentArtifact != artifact || currentRevision != artifactRevision {
		t.Fatalf("repeated identical didOpen replaced artifact: got=%p revision=%d want=%p revision=%d", currentArtifact, currentRevision, artifact, artifactRevision)
	}
	if currentGraphGeneration != graphGeneration || currentReferenceGeneration != referenceGeneration || currentAnalysisSequence != analysisSequence {
		t.Fatalf("repeated identical didOpen advanced generations: graph=%d/%d references=%d/%d analysis=%d/%d", currentGraphGeneration, graphGeneration, currentReferenceGeneration, referenceGeneration, currentAnalysisSequence, analysisSequence)
	}
}

func TestDidOpenChangedSourceInvalidatesParsedArtifact(t *testing.T) {
	root := t.TempDir()
	uri := filePathURI(filepath.Join(root, "changed.asp"))
	oldText := "<% Dim OldValue %>"
	server, _ := newIndexedDocumentOpenCacheTestServer(t, root, uri, oldText)
	var parses atomic.Int32
	server.documentParseTestHook = func(string) { parses.Add(1) }

	if err := server.handleNotification(context.Background(), "textDocument/didOpen", mustRaw(map[string]any{
		"textDocument": map[string]any{
			"uri": uri, "languageId": "classic-asp", "version": 1, "text": oldText,
		},
	})); err != nil {
		t.Fatal(err)
	}
	server.mu.Lock()
	oldRevision := server.workspaceArtifactRevisions[workspaceDocumentIDFromURI(uri)]
	server.mu.Unlock()
	newText := "<% Dim NewValue : NewValue = 1 %>"
	if err := server.handleNotification(context.Background(), "textDocument/didOpen", mustRaw(map[string]any{
		"textDocument": map[string]any{
			"uri": uri, "languageId": "classic-asp", "version": 2, "text": newText,
		},
	})); err != nil {
		t.Fatal(err)
	}
	server.documentOpenAnalysisWorkers.Wait()
	server.mu.Lock()
	artifact := server.workspaceArtifacts[workspaceDocumentIDFromURI(uri)]
	newRevision := server.workspaceArtifactRevisions[workspaceDocumentIDFromURI(uri)]
	cached := server.parsedCache[parsedDocumentCacheKey(uri)]
	server.mu.Unlock()
	if got := parses.Load(); got != 1 {
		t.Fatalf("changed didOpen parse calls = %d, want one", got)
	}
	if artifact == nil || artifact.SourceFingerprint != workspaceFingerprint(newText) {
		t.Fatalf("changed didOpen artifact = %#v, want current source", artifact)
	}
	if newRevision <= oldRevision {
		t.Fatalf("changed didOpen artifact revision = %d, want greater than %d", newRevision, oldRevision)
	}
	if cached == (parsedDocumentCacheEntry{}) || cached.Parsed == nil || cached.Text != newText {
		t.Fatalf("changed didOpen parsed cache = %#v, want current source", cached)
	}
}

func TestDidOpenIdenticalUnsavedOverlayWithDifferentBackingPreservesArtifact(t *testing.T) {
	root := t.TempDir()
	uri := filePathURI(filepath.Join(root, "overlay.asp"))
	backingText := "<% Dim BackingValue %>"
	overlayText := "<% Dim OverlayValue %>"
	server, _ := newIndexedDocumentOpenCacheTestServer(t, root, uri, backingText)
	var parses atomic.Int32
	server.documentParseTestHook = func(string) { parses.Add(1) }
	open := func(version int) error {
		return server.handleNotification(context.Background(), "textDocument/didOpen", mustRaw(map[string]any{
			"textDocument": map[string]any{
				"uri": uri, "languageId": "classic-asp", "version": version, "text": overlayText,
			},
		}))
	}
	if err := open(1); err != nil {
		t.Fatal(err)
	}
	server.documentOpenAnalysisWorkers.Wait()
	server.mu.Lock()
	artifact := server.workspaceArtifacts[workspaceDocumentIDFromURI(uri)]
	artifactRevision := server.workspaceArtifactRevisions[workspaceDocumentIDFromURI(uri)]
	parsed := server.parsedCache[parsedDocumentCacheKey(uri)].Parsed
	graphGeneration := server.graphGeneration
	referenceGeneration := server.referenceGeneration
	analysisSequence := server.documentOpenAnalysisSequence
	server.mu.Unlock()
	if artifact == nil || parsed == nil {
		t.Fatal("initial unsaved overlay did not publish an artifact and parse")
	}
	if got := parses.Load(); got != 1 {
		t.Fatalf("initial unsaved overlay parse calls = %d, want one", got)
	}
	if err := open(2); err != nil {
		t.Fatal(err)
	}
	server.documentOpenAnalysisWorkers.Wait()
	server.mu.Lock()
	currentArtifact := server.workspaceArtifacts[workspaceDocumentIDFromURI(uri)]
	currentRevision := server.workspaceArtifactRevisions[workspaceDocumentIDFromURI(uri)]
	currentParsed := server.parsedCache[parsedDocumentCacheKey(uri)].Parsed
	currentGraphGeneration := server.graphGeneration
	currentReferenceGeneration := server.referenceGeneration
	currentAnalysisSequence := server.documentOpenAnalysisSequence
	server.mu.Unlock()
	if got := parses.Load(); got != 1 {
		t.Fatalf("identical unsaved overlay reparsed source %d times", got)
	}
	if currentParsed != parsed {
		t.Fatalf("identical unsaved overlay replaced parsed document: got=%p want=%p", currentParsed, parsed)
	}
	if currentArtifact != artifact || currentRevision != artifactRevision {
		t.Fatalf("identical unsaved overlay replaced artifact: got=%p revision=%d want=%p revision=%d", currentArtifact, currentRevision, artifact, artifactRevision)
	}
	if currentGraphGeneration != graphGeneration || currentReferenceGeneration != referenceGeneration || currentAnalysisSequence != analysisSequence {
		t.Fatalf("identical unsaved overlay advanced generations: graph=%d/%d references=%d/%d analysis=%d/%d", currentGraphGeneration, graphGeneration, currentReferenceGeneration, referenceGeneration, currentAnalysisSequence, analysisSequence)
	}
}

func TestDuplicateInitializedDoesNotRestartWorkspaceIndex(t *testing.T) {
	root := t.TempDir()
	server := New(strings.NewReader(""), io.Discard, io.Discard)
	server.settings.CacheEnabled = false
	var starts atomic.Int32
	server.workspaceIndexTestHook = func(_ context.Context, phase workspaceIndexTestPhase, _ uint64) {
		if phase == workspaceIndexTestPhaseStarted {
			starts.Add(1)
		}
	}
	t.Cleanup(server.stopWorkspaceIndexWorkers)
	if _, rpcErr := server.handleRequest(context.Background(), "initialize", mustRaw(map[string]any{
		"rootUri": filePathURI(root),
	})); rpcErr != nil {
		t.Fatalf("initialize returned an error: %#v", rpcErr)
	}
	if err := server.handleNotification(context.Background(), "initialized", nil); err != nil {
		t.Fatal(err)
	}
	waitForWorkspaceIndexCompletion(t, server)
	server.mu.Lock()
	firstGeneration := server.workspaceIndexGeneration
	server.mu.Unlock()
	if got := starts.Load(); got != 1 {
		t.Fatalf("initial workspace index starts = %d, want one", got)
	}
	if err := server.handleNotification(context.Background(), "initialized", nil); err != nil {
		t.Fatal(err)
	}
	server.mu.Lock()
	secondGeneration := server.workspaceIndexGeneration
	server.mu.Unlock()
	if got := starts.Load(); got != 1 {
		t.Fatalf("duplicate initialized restarted workspace index %d times", got)
	}
	if secondGeneration != firstGeneration {
		t.Fatalf("duplicate initialized advanced workspace generation from %d to %d", firstGeneration, secondGeneration)
	}
}

func TestParsedDocumentAndTextNamespacesShareImmutableParse(t *testing.T) {
	server := New(strings.NewReader(""), io.Discard, io.Discard)
	server.settings.CacheEnabled = false
	var parses atomic.Int32
	server.documentParseTestHook = func(string) { parses.Add(1) }
	const uri = "file:///workspace/alias.asp"
	const text = "<% Dim SharedValue %>"
	doc := core.NewTextDocument(uri, "classic-asp", 1, text)
	parsed := server.parseTextDocument(doc, "VBScript")
	textParsed := server.parseText(uri, text, "VBScript")
	if parsed == nil || textParsed != parsed {
		t.Fatalf("doc/text cache returned different parses: doc=%p text=%p", parsed, textParsed)
	}
	if got := parses.Load(); got != 1 {
		t.Fatalf("doc/text parse calls = %d, want one", got)
	}
	server.mu.Lock()
	docEntry := server.parsedCache[parsedDocumentCacheKey(uri)]
	textEntry := server.parsedCache[parsedTextCacheKey(uri)]
	server.mu.Unlock()
	if docEntry.Parsed != parsed || textEntry.Parsed != parsed {
		t.Fatalf("doc/text cache entries do not share parse: doc=%p text=%p want=%p", docEntry.Parsed, textEntry.Parsed, parsed)
	}

	otherURI := "file:///workspace/other-alias.asp"
	other := core.NewTextDocument(otherURI, "classic-asp", 1, "<% Dim OtherValue %>")
	server.parseTextDocument(other, "VBScript")
	server.documents[uri] = doc
	server.documents[otherURI] = other
	collection := server.workspaceGraphDocumentsContextWithProgressResult(context.Background(), false, nil)
	if !collection.complete {
		t.Fatalf("graph collection failed: %#v", collection.err)
	}
	if got := parses.Load(); got != 2 {
		t.Fatalf("graph collection reparsed cached source: parse calls = %d, want two", got)
	}
}

func TestWorkspaceVBAutoIncludeUsesDocumentCacheAlias(t *testing.T) {
	server := New(strings.NewReader(""), io.Discard, io.Discard)
	server.settings.CacheEnabled = false
	const uri = "file:///workspace/auto-include-alias.asp"
	const text = "<% PublicValue = 1 %>"
	doc := core.NewTextDocument(uri, "classic-asp", 1, text)
	server.parseTextDocument(doc, "VBScript")
	var transientParses atomic.Int32
	server.workspaceVBAutoIncludeTransientParseTestHook = func(string) { transientParses.Add(1) }
	sources := []workspaceVBAutoIncludeCatalogSource{{
		DocumentID:        workspaceDocumentIDFromURI(uri),
		Document:          doc,
		SourceFingerprint: workspaceFingerprint(text),
	}}
	build, ok := server.buildWorkspaceVBAutoIncludeCatalog(context.Background(), 1, sources)
	if !ok || build.catalog == nil {
		t.Fatalf("auto-include catalog build failed: ok=%t build=%#v", ok, build)
	}
	if got := transientParses.Load(); got != 0 {
		t.Fatalf("auto-include reparsed aliased source %d times", got)
	}
}

func TestDidCloseReopenIdenticalSourceReusesReferenceBatchForCodeLens(t *testing.T) {
	root := t.TempDir()
	uri := filePathURI(filepath.Join(root, "references.asp"))
	text := "<% Dim SharedValue : Response.Write SharedValue %>"
	server, parsed := newIndexedDocumentOpenCacheTestServer(t, root, uri, text)
	server.settings.CodeLensReferences = true
	declarations := server.vbscriptReferenceCodeLensDeclarations(parsed)
	if len(declarations) != 1 {
		t.Fatalf("reference declarations = %#v, want one", declarations)
	}
	declaration := declarations[0]
	server.mu.Lock()
	referenceGeneration := server.referenceGeneration
	requestKey := workspaceReferenceRequestKey(uri, declaration.Range.Start, false, declaration.Kind, referenceGeneration, declaration.Name)
	server.referenceCounts[requestKey] = 4
	server.referenceBatch[referenceBatchCacheKey(uri, 0, referenceGeneration)] = &workspaceReferenceBatchState{
		generation: referenceGeneration, complete: true, declarations: declarations, finalCounts: []int{4},
	}
	artifactRevision := server.workspaceArtifactRevisions[workspaceDocumentIDFromURI(uri)]
	server.mu.Unlock()

	if err := server.handleNotification(context.Background(), "textDocument/didOpen", mustRaw(map[string]any{
		"textDocument": map[string]any{
			"uri": uri, "languageId": "classic-asp", "version": 1, "text": text,
		},
	})); err != nil {
		t.Fatal(err)
	}
	if err := server.handleNotification(context.Background(), "textDocument/didClose", mustRaw(map[string]any{
		"textDocument": map[string]any{"uri": uri},
	})); err != nil {
		t.Fatal(err)
	}
	if err := server.handleNotification(context.Background(), "textDocument/didOpen", mustRaw(map[string]any{
		"textDocument": map[string]any{
			"uri": uri, "languageId": "classic-asp", "version": 2, "text": text,
		},
	})); err != nil {
		t.Fatal(err)
	}
	lenses := server.codeLens(uri)
	if len(lenses) != 1 || lenses[0].Command == nil || lenses[0].Command.Title != "4 references" {
		t.Fatalf("reopened CodeLens = %#v, want completed cached count", lenses)
	}
	server.mu.Lock()
	gotGeneration := server.referenceGeneration
	gotArtifactRevision := server.workspaceArtifactRevisions[workspaceDocumentIDFromURI(uri)]
	gotBatch := server.referenceBatch[referenceBatchCacheKey(uri, 0, referenceGeneration)]
	_, gotCount := server.referenceCounts[requestKey]
	server.mu.Unlock()
	if gotGeneration != referenceGeneration || gotArtifactRevision != artifactRevision || gotBatch == nil || !gotBatch.complete || !gotCount {
		t.Fatalf("reopen discarded completed reference state: generation=%d artifactRevision=%d batch=%#v count=%t", gotGeneration, gotArtifactRevision, gotBatch, gotCount)
	}
}

func newIndexedDocumentOpenCacheTestServer(t *testing.T, root, uri, text string) (*Server, *core.ParsedDocument) {
	t.Helper()
	server := New(strings.NewReader(""), io.Discard, io.Discard)
	server.settings.CacheEnabled = false
	server.rootPath = root
	server.rootURI = filePathURI(root)
	server.workspaceRoots = []workspaceRoot{{Path: root, URI: server.rootURI}}
	backing := core.NewTextDocument(uri, "classic-asp", 0, text)
	server.mu.Lock()
	server.workspace[uri] = backing
	server.mu.Unlock()
	parsed := server.parseTextDocument(backing, server.settings.DefaultLanguage)
	if result := server.applyWorkspaceDocumentRevision(backing, parsed); result.Manifest == nil || result.Stale {
		t.Fatalf("failed to seed workspace artifact: %#v", result)
	}
	t.Cleanup(func() {
		server.cancelDocumentOpenAnalysis(uri)
		server.documentOpenAnalysisWorkers.Wait()
		server.shutdownRuntimeCaches()
	})
	return server, parsed
}
