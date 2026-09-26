package lspserver

import (
	"context"
	"fmt"
	"io"
	"path/filepath"
	"strings"
	"sync/atomic"
	"testing"
	"time"

	"github.com/fxamacker/cbor/v2"
	"github.com/yottonoko/classic-web-system-lsp/internal/core"
	"github.com/yottonoko/classic-web-system-lsp/internal/vbscript"
	workspacepkg "github.com/yottonoko/classic-web-system-lsp/internal/workspace"
)

func newWorkspaceReferenceCountPersistenceServer(t *testing.T, root, cacheDirectory string) *Server {
	t.Helper()
	server := New(strings.NewReader(""), io.Discard, io.Discard)
	server.rootPath = root
	server.rootURI = filePathURI(root)
	server.workspaceRoots = []workspaceRoot{{Path: root, URI: server.rootURI}}
	server.settings.CacheEnabled = true
	server.settings.CacheDirectory = cacheDirectory
	server.settings.CacheTTLHours = 24
	server.settings.CacheMaxSizeMB = 16
	server.configureDiskAnalysisCache()
	return server
}

func TestWorkspaceReferenceCountSummaryRestoresWithoutPostingBlobs(t *testing.T) {
	root := t.TempDir()
	cacheDirectory := filepath.Join(root, "cache")
	uri := filePathURI(filepath.Join(root, "default.asp"))
	source := `<% Dim SharedValue : Response.Write SharedValue : SharedValue = 1 %>`
	doc := core.NewTextDocument(uri, "classic-asp", 1, source)

	first := newWorkspaceReferenceCountPersistenceServer(t, root, cacheDirectory)
	first.workspace[uri] = doc
	firstParsed := first.parseTextDocument(doc, "VBScript")
	if result := first.applyWorkspaceDocumentRevision(doc, firstParsed); result.Manifest == nil || result.Stale {
		t.Fatalf("initial workspace revision = %#v", result)
	}
	first.waitForAsyncDiskCacheWrites()
	if err := first.diskCacheForUse().Flush(); err != nil {
		t.Fatal(err)
	}
	first.closeDiskAnalysisCache()

	second := newWorkspaceReferenceCountPersistenceServer(t, root, cacheDirectory)
	t.Cleanup(second.closeDiskAnalysisCache)
	second.workspace[uri] = doc
	parsed := core.ParseDocument(uri, source, core.Settings{DefaultLanguage: "VBScript"})
	if restored := second.restoreWorkspaceReferenceCountSummaries([]*core.ParsedDocument{parsed}); restored != 1 {
		t.Fatalf("restored count summaries = %d, want 1", restored)
	}
	if restored := second.restoreWorkspaceReferenceCountSummaries([]*core.ParsedDocument{parsed}); restored != 0 {
		t.Fatalf("repeated count summary restore = %d, want 0", restored)
	}
	if _, cached := vbscript.CachedReferenceShard(parsed); cached {
		t.Fatal("count summary restore eagerly seeded the full reference shard")
	}
	declarations := second.vbscriptReferenceCodeLensDeclarations(parsed)
	if len(declarations) != 1 {
		t.Fatalf("declarations = %d, want 1", len(declarations))
	}
	segments := second.referenceWorkspaceIndex.segmentsForName("SharedValue", []*core.ParsedDocument{parsed})
	if len(segments) != 1 || segments[0].postingsLoaded || segments[0].postings != nil {
		t.Fatalf("restored segment eagerly loaded postings: %#v", segments)
	}
	results := second.workspaceVBScriptReferenceBatch(context.Background(), parsed, declarations, []*core.ParsedDocument{parsed}, second.referenceGeneration, nil)
	if len(results) != 1 || results[0].stale || results[0].count != 2 {
		t.Fatalf("count-only batch = %#v, want count 2", results)
	}
	segments = second.referenceWorkspaceIndex.segmentsForName("SharedValue", []*core.ParsedDocument{parsed})
	if segments[0].postingsLoaded || segments[0].postings != nil {
		t.Fatal("count-only batch hydrated persisted posting blobs")
	}

	hydrated := second.hydrateWorkspaceReferenceSegments(segments)
	if len(hydrated) != 1 || !hydrated[0].postingsLoaded || len(hydrated[0].postings) != 3 {
		t.Fatalf("hydrated segment = %#v", hydrated)
	}
	reused := second.hydrateWorkspaceReferenceSegments(segments)
	if len(reused) != 1 || len(reused[0].postings) != 3 || &reused[0].postings[0] != &hydrated[0].postings[0] {
		t.Fatalf("repeated hydration did not reuse the immutable posting slice: %#v", reused)
	}
	locations, _ := second.materializeWorkspaceReferenceSegments(context.Background(), hydrated, workspaceReferenceLocationPlan{
		originURI: uri, originDocumentKey: hydrated[0].documentKey, symbolKind: declarations[0].Kind,
	}, nil)
	if len(locations) != 2 {
		t.Fatalf("hydrated locations = %#v, want 2", locations)
	}
}

func TestWorkspaceReferenceCountSummaryRejectsChangedSource(t *testing.T) {
	root := t.TempDir()
	cacheDirectory := filepath.Join(root, "cache")
	uri := filePathURI(filepath.Join(root, "default.asp"))
	backing := core.NewTextDocument(uri, "classic-asp", 1, `<% Dim SharedValue : Response.Write SharedValue %>`)
	first := newWorkspaceReferenceCountPersistenceServer(t, root, cacheDirectory)
	first.workspace[uri] = backing
	if result := first.applyWorkspaceDocumentRevision(backing, first.parseTextDocument(backing, "VBScript")); result.Manifest == nil || result.Stale {
		t.Fatalf("initial workspace revision = %#v", result)
	}
	first.waitForAsyncDiskCacheWrites()
	if err := first.diskCacheForUse().Flush(); err != nil {
		t.Fatal(err)
	}
	first.closeDiskAnalysisCache()

	second := newWorkspaceReferenceCountPersistenceServer(t, root, cacheDirectory)
	t.Cleanup(second.closeDiskAnalysisCache)
	changedSource := `<% Dim SharedValue : Response.Write SharedValue : Response.Write SharedValue %>`
	changed := core.ParseDocument(uri, changedSource, core.Settings{DefaultLanguage: "VBScript"})
	if restored := second.restoreWorkspaceReferenceCountSummaries([]*core.ParsedDocument{changed}); restored != 0 {
		t.Fatalf("restored stale count summaries = %d", restored)
	}
	segments := second.referenceWorkspaceIndex.segmentsForName("SharedValue", []*core.ParsedDocument{changed})
	if len(segments) != 1 || segments[0].postingsLoaded || segments[0].postings != nil || segments[0].counts.CodeLensReferences != 2 {
		t.Fatalf("changed source segment = %#v", segments)
	}
	full := second.referenceWorkspaceIndex.segmentsForNameContext(context.Background(), "SharedValue", []*core.ParsedDocument{changed})
	if len(full) != 1 || !full[0].postingsLoaded || len(full[0].postings) != 3 {
		t.Fatalf("upgraded changed source segment = %#v", full)
	}
}

func TestWorkspaceReferenceCountSummaryNegativeCachesDiskMissPerRevision(t *testing.T) {
	root := t.TempDir()
	server := newWorkspaceReferenceCountPersistenceServer(t, root, filepath.Join(root, "cache"))
	t.Cleanup(server.closeDiskAnalysisCache)
	uri := filePathURI(filepath.Join(root, "missing.asp"))
	first := core.ParseDocument(uri, `<% Dim MissingValue %>`, core.Settings{DefaultLanguage: "VBScript"})
	if restored := server.restoreWorkspaceReferenceCountSummaries([]*core.ParsedDocument{first}); restored != 0 {
		t.Fatalf("restored missing summary = %d, want 0", restored)
	}
	server.mu.Lock()
	_, firstAttempted := server.referenceCountSummariesRestored[first]
	server.mu.Unlock()
	if !firstAttempted {
		t.Fatal("missing summary was not negative-cached")
	}
	if restored := server.restoreWorkspaceReferenceCountSummaries([]*core.ParsedDocument{first}); restored != 0 {
		t.Fatalf("repeated missing summary restore = %d, want 0", restored)
	}

	second := core.ParseDocument(uri, `<% Dim MissingValue : MissingValue = 1 %>`, core.Settings{DefaultLanguage: "VBScript"})
	if restored := server.restoreWorkspaceReferenceCountSummaries([]*core.ParsedDocument{second}); restored != 0 {
		t.Fatalf("new revision missing summary restore = %d, want 0", restored)
	}
	server.mu.Lock()
	_, secondAttempted := server.referenceCountSummariesRestored[second]
	attempts := len(server.referenceCountSummariesRestored)
	server.mu.Unlock()
	if !secondAttempted || attempts != 2 {
		t.Fatalf("revision attempts = %d, second attempted = %t", attempts, secondAttempted)
	}
}

func installWorkspaceReferenceCountSummaryPreparationTestHook(t *testing.T, hook func(string)) {
	t.Helper()
	workspaceReferenceCountSummaryPreparationTestHook.Lock()
	previous := workspaceReferenceCountSummaryPreparationTestHook.fn
	workspaceReferenceCountSummaryPreparationTestHook.fn = hook
	workspaceReferenceCountSummaryPreparationTestHook.Unlock()
	t.Cleanup(func() {
		workspaceReferenceCountSummaryPreparationTestHook.Lock()
		workspaceReferenceCountSummaryPreparationTestHook.fn = previous
		workspaceReferenceCountSummaryPreparationTestHook.Unlock()
	})
}

func installWorkspaceReferenceCountSummaryPublicationTestHook(t *testing.T, hook func(string)) {
	t.Helper()
	workspaceReferenceCountSummaryPublicationTestHook.Lock()
	previous := workspaceReferenceCountSummaryPublicationTestHook.fn
	workspaceReferenceCountSummaryPublicationTestHook.fn = hook
	workspaceReferenceCountSummaryPublicationTestHook.Unlock()
	t.Cleanup(func() {
		workspaceReferenceCountSummaryPublicationTestHook.Lock()
		workspaceReferenceCountSummaryPublicationTestHook.fn = previous
		workspaceReferenceCountSummaryPublicationTestHook.Unlock()
	})
}

func persistedWorkspaceReferenceCountSummaryFixtures(t *testing.T, server *Server, count int) []*core.ParsedDocument {
	t.Helper()
	source := `<% Dim SharedValue : Response.Write SharedValue %>`
	documents := make([]*core.ParsedDocument, count)
	replacements := make([]workspacepkg.DiskReferenceDocumentReplacement, count)
	for index := range documents {
		document := core.ParseDocument(fmt.Sprintf("file:///workspace/reference-summary-%03d.asp", index), source, core.Settings{DefaultLanguage: "VBScript"})
		documents[index] = document
		documentKey := workspacepkg.FileIdentityKeyFromURI(document.URI)
		payload, err := cbor.Marshal(persistedWorkspaceReferenceDocument{
			SchemaVersion: workspaceReferenceDocumentSchemaVersion,
			DocumentKey:   documentKey,
			SourceHash:    workspacepkg.DiskContentHash(source),
			CountSummaries: map[string]persistedWorkspaceReferenceCountSummary{
				"sharedvalue": {
					CountFingerprint:    fmt.Sprintf("count-%03d", index),
					LocationFingerprint: fmt.Sprintf("location-%03d", index),
					Counts:              workspaceReferenceCountSummary{CodeLensReferences: 1},
				},
			},
		})
		if err != nil {
			t.Fatal(err)
		}
		replacements[index] = workspacepkg.DiskReferenceDocumentReplacement{
			DocumentKey: workspaceReferenceDocumentCacheKey(documentKey), DocumentValue: payload,
		}
	}
	if err := server.diskCacheForUse().ReplaceReferenceDocuments(replacements); err != nil {
		t.Fatal(err)
	}
	return documents
}

func TestWorkspaceReferenceCountSummaryPreparationOverlapsWithConfiguredWorkers(t *testing.T) {
	root := t.TempDir()
	server := newWorkspaceReferenceCountPersistenceServer(t, root, filepath.Join(root, "cache"))
	t.Cleanup(server.closeDiskAnalysisCache)
	server.analysisWorkers.setWorkers(2)
	documents := persistedWorkspaceReferenceCountSummaryFixtures(t, server, 2)

	var active atomic.Int32
	var maxActive atomic.Int32
	entered := make(chan struct{}, len(documents))
	release := make(chan struct{})
	installWorkspaceReferenceCountSummaryPreparationTestHook(t, func(string) {
		current := active.Add(1)
		for {
			previous := maxActive.Load()
			if current <= previous || maxActive.CompareAndSwap(previous, current) {
				break
			}
		}
		entered <- struct{}{}
		<-release
		active.Add(-1)
	})

	result := make(chan int, 1)
	go func() {
		result <- server.restoreWorkspaceReferenceCountSummaries(documents)
	}()
	for range documents {
		select {
		case <-entered:
		case <-time.After(2 * time.Second):
			close(release)
			t.Fatal("count summary preparation did not overlap")
		}
	}
	close(release)
	select {
	case restored := <-result:
		if restored != len(documents) {
			t.Fatalf("restored count summaries = %d, want %d", restored, len(documents))
		}
	case <-time.After(2 * time.Second):
		t.Fatal("count summary restore did not complete")
	}
	if got := maxActive.Load(); got < 2 {
		t.Fatalf("maximum concurrent preparations = %d, want at least 2", got)
	}
}

func TestWorkspaceReferenceCountSummaryPreparationStaysSequentialWithOneWorker(t *testing.T) {
	root := t.TempDir()
	server := newWorkspaceReferenceCountPersistenceServer(t, root, filepath.Join(root, "cache"))
	t.Cleanup(server.closeDiskAnalysisCache)
	server.analysisWorkers.setWorkers(1)
	documents := persistedWorkspaceReferenceCountSummaryFixtures(t, server, 2)

	var active atomic.Int32
	var maxActive atomic.Int32
	installWorkspaceReferenceCountSummaryPreparationTestHook(t, func(string) {
		current := active.Add(1)
		for {
			previous := maxActive.Load()
			if current <= previous || maxActive.CompareAndSwap(previous, current) {
				break
			}
		}
		active.Add(-1)
	})
	if restored := server.restoreWorkspaceReferenceCountSummaries(documents); restored != len(documents) {
		t.Fatalf("restored count summaries = %d, want %d", restored, len(documents))
	}
	if got := maxActive.Load(); got != 1 {
		t.Fatalf("maximum concurrent preparations = %d, want 1", got)
	}
}

func TestWorkspaceReferenceCountSummaryRestoreRejectsOlderPublicationTicket(t *testing.T) {
	root := t.TempDir()
	server := newWorkspaceReferenceCountPersistenceServer(t, root, filepath.Join(root, "cache"))
	t.Cleanup(server.closeDiskAnalysisCache)
	server.analysisWorkers.setWorkers(2)
	documents := persistedWorkspaceReferenceCountSummaryFixtures(t, server, 2)

	entered := make(chan struct{}, len(documents))
	release := make(chan struct{})
	installWorkspaceReferenceCountSummaryPreparationTestHook(t, func(string) {
		entered <- struct{}{}
		<-release
	})
	result := make(chan int, 1)
	go func() {
		result <- server.restoreWorkspaceReferenceCountSummaries(documents)
	}()
	for range documents {
		select {
		case <-entered:
		case <-time.After(2 * time.Second):
			close(release)
			t.Fatal("persisted count summary preparation did not start")
		}
	}
	if got := server.referenceWorkspaceIndex.sequence.Load(); got != 1 {
		close(release)
		t.Fatalf("restore batch reserved %d publication tickets, want 1", got)
	}
	fresh := core.ParseDocument(documents[1].URI, `<% Dim NewValue : Response.Write NewValue %>`, core.Settings{DefaultLanguage: "VBScript"})
	freshKey := workspacepkg.FileIdentityKeyFromURI(fresh.URI)
	freshPrepared := prepareWorkspaceReferenceDocument(
		freshKey,
		fresh,
		workspacepkg.DiskContentHash(fresh.Text),
		vbscript.BuildReferenceShard(fresh),
		server.referenceWorkspaceIndex.sequence.Add(1),
	)
	if update := server.referenceWorkspaceIndex.applyPrepared([]workspaceReferencePreparedDocument{freshPrepared}); update.ChangedDocuments != 1 {
		close(release)
		t.Fatalf("newer full index update = %#v", update)
	}
	close(release)
	select {
	case restored := <-result:
		if restored != 1 {
			t.Fatalf("older persisted summary restored %d documents, want 1 unaffected document", restored)
		}
	case <-time.After(2 * time.Second):
		t.Fatal("persisted count summary restore did not complete")
	}
	server.referenceWorkspaceIndex.mu.RLock()
	entry := server.referenceWorkspaceIndex.documents[freshKey]
	server.referenceWorkspaceIndex.mu.RUnlock()
	if entry.parsed != fresh || entry.countSummaryOnly {
		t.Fatalf("older persisted summary replaced newer full index entry: %#v", entry)
	}
}

func TestWorkspaceReferenceCountSummaryGenerationCheckIsAtomicWithPublication(t *testing.T) {
	root := t.TempDir()
	server := newWorkspaceReferenceCountPersistenceServer(t, root, filepath.Join(root, "cache"))
	t.Cleanup(server.closeDiskAnalysisCache)
	documents := persistedWorkspaceReferenceCountSummaryFixtures(t, server, 1)
	server.mu.Lock()
	generation := server.referenceGeneration
	server.mu.Unlock()

	entered := make(chan struct{}, 1)
	release := make(chan struct{})
	installWorkspaceReferenceCountSummaryPublicationTestHook(t, func(string) {
		entered <- struct{}{}
		<-release
	})
	result := make(chan int, 1)
	go func() {
		result <- server.restoreWorkspaceReferenceCountSummariesContext(context.Background(), documents, generation)
	}()
	select {
	case <-entered:
	case <-time.After(2 * time.Second):
		close(release)
		t.Fatal("persisted count summary publication did not start")
	}
	drifted := make(chan struct{})
	go func() {
		server.mu.Lock()
		server.referenceGeneration++
		server.mu.Unlock()
		close(drifted)
	}()
	select {
	case <-drifted:
		close(release)
		t.Fatal("reference generation changed during atomic summary publication")
	case <-time.After(50 * time.Millisecond):
	}
	close(release)
	if restored := <-result; restored != 1 {
		t.Fatalf("atomically published summaries = %d, want 1", restored)
	}
	select {
	case <-drifted:
	case <-time.After(2 * time.Second):
		t.Fatal("reference generation change remained blocked after publication")
	}
}

func TestWorkspaceReferenceCountSummaryRestoreCancelsOnGenerationDriftAndCanRetry(t *testing.T) {
	root := t.TempDir()
	server := newWorkspaceReferenceCountPersistenceServer(t, root, filepath.Join(root, "cache"))
	t.Cleanup(server.closeDiskAnalysisCache)
	server.analysisWorkers.setWorkers(2)
	documents := persistedWorkspaceReferenceCountSummaryFixtures(t, server, 1)

	entered := make(chan struct{}, 1)
	release := make(chan struct{})
	var attempts atomic.Int32
	installWorkspaceReferenceCountSummaryPreparationTestHook(t, func(string) {
		if attempts.Add(1) == 1 {
			entered <- struct{}{}
			<-release
		}
	})
	server.mu.Lock()
	generation := server.referenceGeneration
	server.mu.Unlock()
	ctx, cancel := context.WithCancel(context.Background())
	result := make(chan int, 1)
	go func() {
		result <- server.restoreWorkspaceReferenceCountSummariesContext(ctx, documents, generation)
	}()
	select {
	case <-entered:
	case <-time.After(2 * time.Second):
		close(release)
		t.Fatal("persisted count summary preparation did not start")
	}
	server.mu.Lock()
	server.referenceGeneration++
	currentGeneration := server.referenceGeneration
	server.mu.Unlock()
	cancel()
	close(release)
	select {
	case restored := <-result:
		if restored != 0 {
			t.Fatalf("stale generation restored %d documents, want 0", restored)
		}
	case <-time.After(2 * time.Second):
		t.Fatal("cancelled persisted count summary restore did not complete")
	}
	server.mu.Lock()
	_, reserved := server.referenceCountSummariesRestored[documents[0]]
	server.mu.Unlock()
	if reserved {
		t.Fatal("cancelled persisted count summary restore retained its retry reservation")
	}
	if restored := server.restoreWorkspaceReferenceCountSummariesContext(context.Background(), documents, currentGeneration); restored != 1 {
		t.Fatalf("retry restored %d documents, want 1", restored)
	}
}

func TestWorkspaceReferenceCountSummaryDoesNotDowngradeFullIndex(t *testing.T) {
	uri := "file:///workspace/default.asp"
	source := `<% Dim SharedValue : Response.Write SharedValue %>`
	parsed := core.ParseDocument(uri, source, core.Settings{DefaultLanguage: "VBScript"})
	index := newWorkspaceReferenceIndex()
	full := index.segmentsForNameContext(context.Background(), "SharedValue", []*core.ParsedDocument{parsed})
	if len(full) != 1 || !full[0].postingsLoaded {
		t.Fatalf("full index segment = %#v", full)
	}
	server := New(strings.NewReader(""), io.Discard, io.Discard)
	summaries := server.workspaceReferencePersistedCountSummaries(parsed, vbscript.BuildReferenceShard(parsed))
	if update := index.seedPersistedCountSummary(parsed, workspacepkg.DiskContentHash(source), summaries); update.ChangedDocuments != 0 {
		t.Fatalf("persisted summary downgraded a full index: %#v", update)
	}
	after := index.segmentsForNameContext(context.Background(), "SharedValue", []*core.ParsedDocument{parsed})
	if len(after) != 1 || !after[0].postingsLoaded {
		t.Fatalf("segment after summary seed = %#v", after)
	}
}

func TestWorkspaceReferenceCountSummaryReplacementPublishesLatestSource(t *testing.T) {
	root := t.TempDir()
	cacheDirectory := filepath.Join(root, "cache")
	uri := filePathURI(filepath.Join(root, "default.asp"))
	server := newWorkspaceReferenceCountPersistenceServer(t, root, cacheDirectory)
	t.Cleanup(server.closeDiskAnalysisCache)
	first := core.NewTextDocument(uri, "classic-asp", 1, `<% Dim SharedValue : Response.Write SharedValue %>`)
	server.workspace[uri] = first
	if result := server.applyWorkspaceDocumentRevision(first, server.parseTextDocument(first, "VBScript")); result.Manifest == nil || result.Stale {
		t.Fatalf("first workspace revision = %#v", result)
	}
	server.waitForAsyncDiskCacheWrites()
	second := core.NewTextDocument(uri, "classic-asp", 2, `<% Dim SharedValue : Response.Write SharedValue : Response.Write SharedValue %>`)
	server.workspace[uri] = second
	if result := server.applyWorkspaceDocumentRevision(second, server.parseTextDocument(second, "VBScript")); result.Manifest == nil || result.Stale {
		t.Fatalf("second workspace revision = %#v", result)
	}
	server.waitForAsyncDiskCacheWrites()
	if err := server.diskCacheForUse().Flush(); err != nil {
		t.Fatal(err)
	}
	manifest := persistedReferenceManifestForURI(t, server, uri)
	if manifest == nil || manifest.SourceHash != workspacepkg.DiskContentHash(second.Text) {
		t.Fatalf("persisted manifest = %#v", manifest)
	}
	if got := manifest.CountSummaries["sharedvalue"].Counts.CodeLensReferences; got != 2 {
		t.Fatalf("persisted latest count = %d, want 2", got)
	}
}

func TestWorkspaceReferenceCountSummaryPreservesImplicitDeclarationAdjustment(t *testing.T) {
	root := t.TempDir()
	cacheDirectory := filepath.Join(root, "cache")
	uri := filePathURI(filepath.Join(root, "default.asp"))
	source := `<% SharedValue = 1 : Response.Write SharedValue %>`
	doc := core.NewTextDocument(uri, "classic-asp", 1, source)
	first := newWorkspaceReferenceCountPersistenceServer(t, root, cacheDirectory)
	first.workspace[uri] = doc
	if result := first.applyWorkspaceDocumentRevision(doc, first.parseTextDocument(doc, "VBScript")); result.Manifest == nil || result.Stale {
		t.Fatalf("initial workspace revision = %#v", result)
	}
	first.waitForAsyncDiskCacheWrites()
	if err := first.diskCacheForUse().Flush(); err != nil {
		t.Fatal(err)
	}
	first.closeDiskAnalysisCache()

	second := newWorkspaceReferenceCountPersistenceServer(t, root, cacheDirectory)
	t.Cleanup(second.closeDiskAnalysisCache)
	second.workspace[uri] = doc
	parsed := core.ParseDocument(uri, source, core.Settings{DefaultLanguage: "VBScript"})
	if restored := second.restoreWorkspaceReferenceCountSummaries([]*core.ParsedDocument{parsed}); restored != 1 {
		t.Fatalf("restored count summaries = %d, want 1", restored)
	}
	declarations := second.vbscriptReferenceCodeLensDeclarations(parsed)
	if len(declarations) != 1 || !declarations[0].Implicit {
		t.Fatalf("implicit declarations = %#v", declarations)
	}
	results := second.workspaceVBScriptReferenceBatch(context.Background(), parsed, declarations, []*core.ParsedDocument{parsed}, second.referenceGeneration, nil)
	if len(results) != 1 || results[0].stale || results[0].count != 1 {
		t.Fatalf("persisted implicit count = %#v, want 1", results)
	}
}

func BenchmarkWorkspaceReferenceCountSummarySeed2000Documents(b *testing.B) {
	const documentCount = 2000
	source := `<% Dim SharedValue : Response.Write SharedValue %>`
	documents := make([]*core.ParsedDocument, documentCount)
	for index := range documents {
		documents[index] = core.ParseDocument(fmt.Sprintf("file:///workspace/%05d.asp", index), source, core.Settings{DefaultLanguage: "VBScript"})
	}
	server := New(strings.NewReader(""), io.Discard, io.Discard)
	summaries := server.workspaceReferencePersistedCountSummaries(documents[0], vbscript.BuildReferenceShard(documents[0]))
	sourceHash := workspacepkg.DiskContentHash(source)
	b.ReportAllocs()
	b.ResetTimer()
	for iteration := 0; iteration < b.N; iteration++ {
		index := newWorkspaceReferenceIndex()
		for _, document := range documents {
			index.seedPersistedCountSummary(document, sourceHash, summaries)
		}
		if got := len(index.segmentsForName("SharedValue", documents)); got != documentCount {
			b.Fatalf("segments = %d, want %d", got, documentCount)
		}
	}
}
