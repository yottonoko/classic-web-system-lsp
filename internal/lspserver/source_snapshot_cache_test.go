package lspserver

import (
	"context"
	"errors"
	"io"
	"os"
	"path/filepath"
	"sync"
	"sync/atomic"
	"testing"

	"github.com/yottonoko/classic-web-system-lsp/internal/core"
)

func TestSourceSnapshotRepeatedAndConcurrentReadsUseOnePhysicalRead(t *testing.T) {
	path := writeSourceSnapshotFixture(t, "singleflight.asp", "cached source")
	server := New(nil, io.Discard, nil)
	configureSourceSnapshotBoundary(server, path)
	var physicalReads atomic.Int32
	firstReadStarted := make(chan struct{})
	releaseFirstRead := make(chan struct{})
	server.workspaceFileReadTestHook = func(string) {
		if physicalReads.Add(1) == 1 {
			close(firstReadStarted)
			<-releaseFirstRead
		}
	}

	const readers = 32
	results := make(chan sourceSnapshotReadResult, readers)
	start := make(chan struct{})
	var ready sync.WaitGroup
	ready.Add(readers)
	for range readers {
		go func() {
			ready.Done()
			<-start
			raw, err := server.readSourceFileBytes(context.Background(), path, nil)
			results <- sourceSnapshotReadResult{raw: raw, err: err}
		}()
	}
	ready.Wait()
	close(start)
	<-firstReadStarted
	close(releaseFirstRead)
	for range readers {
		result := <-results
		if result.err != nil {
			t.Fatalf("concurrent source read returned error: %v", result.err)
		}
		if got := string(result.raw); got != "cached source" {
			t.Fatalf("concurrent source read = %q, want cached source", got)
		}
	}
	if got := physicalReads.Load(); got != 1 {
		t.Fatalf("concurrent physical reads = %d, want 1", got)
	}

	raw, err := server.readSourceFileBytes(context.Background(), path, nil)
	if err != nil {
		t.Fatalf("repeated source read returned error: %v", err)
	}
	if got := string(raw); got != "cached source" {
		t.Fatalf("repeated source read = %q, want cached source", got)
	}
	if got := physicalReads.Load(); got != 1 {
		t.Fatalf("repeated physical reads = %d, want 1", got)
	}
}

func TestSourceSnapshotCachedReadsOwnReturnedBytes(t *testing.T) {
	path := writeSourceSnapshotFixture(t, "mutable-result.asp", "cached source")
	server := New(nil, io.Discard, nil)
	configureSourceSnapshotBoundary(server, path)
	var physicalReads atomic.Int32
	server.workspaceFileReadTestHook = func(string) { physicalReads.Add(1) }

	first, err := server.readSourceFileBytes(context.Background(), path, nil)
	if err != nil {
		t.Fatalf("first source read returned error: %v", err)
	}
	first[0] = 'X'

	second, err := server.readSourceFileBytes(context.Background(), path, nil)
	if err != nil {
		t.Fatalf("second source read returned error: %v", err)
	}
	if got := string(second); got != "cached source" {
		t.Fatalf("cached source changed after first result mutation: %q", got)
	}
	second[1] = 'Y'

	third, err := server.readSourceFileBytes(context.Background(), path, nil)
	if err != nil {
		t.Fatalf("third source read returned error: %v", err)
	}
	if got := string(third); got != "cached source" {
		t.Fatalf("cached source changed after second result mutation: %q", got)
	}
	if got := physicalReads.Load(); got != 1 {
		t.Fatalf("physical reads = %d, want 1", got)
	}
}

func TestSourceSnapshotInvalidationCausesExactlyOneConcurrentReread(t *testing.T) {
	path := writeSourceSnapshotFixture(t, "invalidate.asp", "before invalidation")
	server := New(nil, io.Discard, nil)
	configureSourceSnapshotBoundary(server, path)
	var physicalReads atomic.Int32
	server.workspaceFileReadTestHook = func(string) {
		physicalReads.Add(1)
	}

	assertSourceSnapshotBytes(t, server, path, "before invalidation")
	if got := physicalReads.Load(); got != 1 {
		t.Fatalf("initial physical reads = %d, want 1", got)
	}
	if err := os.WriteFile(path, []byte("after invalidation"), 0o644); err != nil {
		t.Fatal(err)
	}
	server.invalidateSourceSnapshot(path)

	const readers = 32
	results := make(chan sourceSnapshotReadResult, readers)
	var group sync.WaitGroup
	group.Add(readers)
	for range readers {
		go func() {
			defer group.Done()
			raw, err := server.readSourceFileBytes(context.Background(), path, nil)
			results <- sourceSnapshotReadResult{raw: raw, err: err}
		}()
	}
	group.Wait()
	close(results)
	for result := range results {
		if result.err != nil {
			t.Fatalf("invalidated source read returned error: %v", result.err)
		}
		if got := string(result.raw); got != "after invalidation" {
			t.Fatalf("invalidated source read = %q, want after invalidation", got)
		}
	}
	if got := physicalReads.Load(); got != 2 {
		t.Fatalf("physical reads after invalidation = %d, want 2", got)
	}
}

func TestSourceSnapshotMemoryPressureNeverPermitsASecondPhysicalRead(t *testing.T) {
	path := writeSourceSnapshotFixture(t, "memory-pressure.asp", "cached source")
	server := New(nil, io.Discard, nil)
	configureSourceSnapshotBoundary(server, path)
	var physicalReads atomic.Int32
	server.workspaceFileReadTestHook = func(string) { physicalReads.Add(1) }
	if _, err := server.readWorkspaceTextFileCached(context.Background(), path, "auto", nil, false); err != nil {
		t.Fatal(err)
	}
	if freed := server.registeredSourceSnapshotCache().Evict(1); freed <= 0 {
		t.Fatal("decoded source was not evicted")
	}
	if raw, err := server.readSourceFileBytes(context.Background(), path, nil); err != nil || string(raw) != "cached source" {
		t.Fatalf("raw source after pressure = %q, %v", raw, err)
	}
	if got := physicalReads.Load(); got != 1 {
		t.Fatalf("physical reads after memory pressure = %d, want 1", got)
	}
}

func TestSourceSnapshotRejectsOversizedFilesBeforeCaching(t *testing.T) {
	path := filepath.Join(t.TempDir(), "oversized.asp")
	if err := os.WriteFile(path, []byte("x"), 0o644); err != nil {
		t.Fatal(err)
	}
	if err := os.Truncate(path, maxSourceFileBytes+1); err != nil {
		t.Fatal(err)
	}
	server := New(nil, io.Discard, nil)
	configureSourceSnapshotBoundary(server, path)
	raw, err := server.readSourceFileBytes(context.Background(), path, nil)
	if !errors.Is(err, errSourceFileTooLarge) {
		t.Fatalf("oversized source read error = %v, want errSourceFileTooLarge", err)
	}
	if raw != nil {
		t.Fatalf("oversized source read returned %d bytes, want nil", len(raw))
	}
	if snapshot := server.sourceSnapshots[sourceSnapshotKey(path)]; snapshot != nil && snapshot.valid {
		t.Fatal("oversized source read populated a valid snapshot")
	}
}

func TestSourceSnapshotRejectsUnscopedPhysicalReads(t *testing.T) {
	path := writeSourceSnapshotFixture(t, "unscoped.asp", "private source")
	server := New(nil, io.Discard, nil)
	if _, err := server.readSourceFileBytes(context.Background(), path, nil); !errors.Is(err, errWorkspacePathOutsideBoundary) {
		t.Fatalf("unscoped source read error = %v, want errWorkspacePathOutsideBoundary", err)
	}
}

func TestSourceSnapshotRejectsSymlinkedFilesAndDirectories(t *testing.T) {
	root := t.TempDir()
	target := filepath.Join(root, "target.asp")
	if err := os.WriteFile(target, []byte("target"), 0o644); err != nil {
		t.Fatal(err)
	}
	symlink := filepath.Join(root, "symlink.asp")
	if err := os.Symlink(target, symlink); err != nil {
		t.Skipf("symlink creation unavailable: %v", err)
	}
	linkedDirectory := filepath.Join(root, "linked")
	if err := os.Symlink(root, linkedDirectory); err != nil {
		t.Skipf("directory symlink creation unavailable: %v", err)
	}
	server := New(nil, io.Discard, nil)
	configureSourceSnapshotBoundary(server, target)
	if _, err := server.readSourceFileBytes(context.Background(), symlink, nil); !errors.Is(err, errWorkspacePathOutsideBoundary) && !errors.Is(err, errSourceFileNotRegular) && !errors.Is(err, errSourceFileSymlink) {
		t.Errorf("symlinked source %q error = %v, want a symlink rejection", symlink, err)
	}
	server.rootPath = root
	server.rootURI = filePathURI(root)
	server.workspaceRoots = []workspaceRoot{{Path: root, URI: filePathURI(root)}}
	if _, err := server.readSourceFileBytes(context.Background(), filepath.Join(linkedDirectory, "target.asp"), nil); !errors.Is(err, errWorkspacePathOutsideBoundary) {
		t.Errorf("source through symlinked directory error = %v, want errWorkspacePathOutsideBoundary", err)
	}
}

func TestPreparedSourceReadRetainsSymlinkBoundaryChecks(t *testing.T) {
	root := t.TempDir()
	target := filepath.Join(root, "target.asp")
	if err := os.WriteFile(target, []byte("target"), 0o644); err != nil {
		t.Fatal(err)
	}
	symlink := filepath.Join(root, "symlink.asp")
	if err := os.Symlink(target, symlink); err != nil {
		t.Skipf("symlink creation unavailable: %v", err)
	}
	server := New(nil, io.Discard, nil)
	configureSourceSnapshotBoundary(server, target)
	trustedRoots, complete := server.trustedFilesystemRootEntriesContext(context.Background(), []string{root})
	if !complete || len(trustedRoots) != 1 {
		t.Fatalf("trusted roots = (%#v, %t), want one complete root", trustedRoots, complete)
	}
	preparedRoots, complete := prepareTrustedFilesystemRoots(context.Background(), trustedRoots, []string{root})
	if !complete {
		t.Fatal("preparing trusted roots was unexpectedly cancelled")
	}
	defer closeTrustedFilesystemRoots(preparedRoots)
	ctx := withSourceReadRoots(withSourceReadBoundaries(context.Background(), root), preparedRoots)
	if _, err := server.readSourceFileBytes(ctx, symlink, nil); !errors.Is(err, errWorkspacePathOutsideBoundary) {
		t.Fatalf("prepared symlink source read error = %v, want errWorkspacePathOutsideBoundary", err)
	}
}

func TestPreparedSourceReadRejectsStaleWorkspaceIndexGeneration(t *testing.T) {
	path := writeSourceSnapshotFixture(t, "stale-prepared-generation.asp", "source")
	server := New(nil, io.Discard, nil)
	configureSourceSnapshotBoundary(server, path)
	server.workspaceIndexEnabled = true
	server.workspaceIndexGeneration = 1
	trustedRoots, complete := server.trustedFilesystemRootEntriesContext(context.Background(), []string{filepath.Dir(path)})
	if !complete || len(trustedRoots) != 1 {
		t.Fatalf("trusted roots = (%#v, %t), want one complete root", trustedRoots, complete)
	}
	preparedRoots, complete := prepareTrustedFilesystemRoots(context.Background(), trustedRoots, []string{filepath.Dir(path)})
	if !complete {
		t.Fatal("preparing trusted roots was unexpectedly cancelled")
	}
	defer closeTrustedFilesystemRoots(preparedRoots)
	ctx := withSourceReadGeneration(
		withSourceReadRoots(withSourceReadBoundaries(context.Background(), filepath.Dir(path)), preparedRoots),
		1,
	)
	server.workspaceIndexGeneration = 2
	if _, err := server.readSourceFileBytes(ctx, path, nil); !errors.Is(err, errWorkspaceIndexGeneration) {
		t.Fatalf("stale prepared source read error = %v, want errWorkspaceIndexGeneration", err)
	}
}

func TestSourceSnapshotInvalidationPreventsStaleInflightReadFromRepopulating(t *testing.T) {
	path := writeSourceSnapshotFixture(t, "stale-inflight.asp", "initial")
	server := New(nil, io.Discard, nil)
	configureSourceSnapshotBoundary(server, path)
	var physicalReads atomic.Int32
	staleReadStarted := make(chan struct{})
	releaseStaleRead := make(chan struct{})
	server.workspaceFileReadTestHook = func(string) {
		if physicalReads.Add(1) == 1 {
			close(staleReadStarted)
			<-releaseStaleRead
		}
	}

	staleResult := make(chan sourceSnapshotReadResult, 1)
	go func() {
		raw, err := server.readSourceFileBytes(context.Background(), path, nil)
		staleResult <- sourceSnapshotReadResult{raw: raw, err: err}
	}()
	<-staleReadStarted

	server.invalidateSourceSnapshot(path)
	if err := os.WriteFile(path, []byte("fresh generation"), 0o644); err != nil {
		t.Fatal(err)
	}
	assertSourceSnapshotBytes(t, server, path, "fresh generation")
	if got := physicalReads.Load(); got != 2 {
		t.Fatalf("physical reads before stale completion = %d, want 2", got)
	}

	if err := os.WriteFile(path, []byte("late stale read"), 0o644); err != nil {
		t.Fatal(err)
	}
	close(releaseStaleRead)
	result := <-staleResult
	if result.err != nil {
		t.Fatalf("stale source read returned error: %v", result.err)
	}
	if got := string(result.raw); got != "late stale read" {
		t.Fatalf("stale source read = %q, want late stale read", got)
	}

	assertSourceSnapshotBytes(t, server, path, "fresh generation")
	if got := physicalReads.Load(); got != 2 {
		t.Fatalf("stale completion caused another physical read: got %d, want 2", got)
	}
}

func TestSourceSnapshotClearPreventsInflightReadFromRepopulating(t *testing.T) {
	path := writeSourceSnapshotFixture(t, "clear-inflight.asp", "before clear")
	server := New(nil, io.Discard, nil)
	configureSourceSnapshotBoundary(server, path)
	var physicalReads atomic.Int32
	readStarted := make(chan struct{})
	releaseRead := make(chan struct{})
	server.workspaceFileReadTestHook = func(string) {
		if physicalReads.Add(1) == 1 {
			close(readStarted)
			<-releaseRead
		}
	}

	firstResult := make(chan sourceSnapshotReadResult, 1)
	go func() {
		raw, err := server.readSourceFileBytes(context.Background(), path, nil)
		firstResult <- sourceSnapshotReadResult{raw: raw, err: err}
	}()
	<-readStarted
	server.clearSourceSnapshots()
	close(releaseRead)
	result := <-firstResult
	if result.err != nil {
		t.Fatalf("cleared inflight source read returned error: %v", result.err)
	}

	if err := os.WriteFile(path, []byte("after clear"), 0o644); err != nil {
		t.Fatal(err)
	}
	assertSourceSnapshotBytes(t, server, path, "after clear")
	if got := physicalReads.Load(); got != 2 {
		t.Fatalf("physical reads after clear = %d, want 2", got)
	}
}

func TestSourceSnapshotOpenDocumentOverlayFallsBackToPhysicalSnapshotAfterClose(t *testing.T) {
	path := writeSourceSnapshotFixture(t, "overlay.asp", "physical source")
	uri := filePathURI(path)
	server := New(nil, io.Discard, nil)
	configureSourceSnapshotBoundary(server, path)
	var physicalReads atomic.Int32
	server.workspaceFileReadTestHook = func(string) {
		physicalReads.Add(1)
	}

	server.mu.Lock()
	server.rememberOpenDocumentLocked(uri, core.NewTextDocument(uri, "classic-asp", 1, "open overlay"))
	server.mu.Unlock()
	text, err := server.readWorkspaceTextFileCached(context.Background(), path, "auto", nil, true)
	if err != nil {
		t.Fatalf("overlay source read returned error: %v", err)
	}
	if text != "open overlay" {
		t.Fatalf("overlay source read = %q, want open overlay", text)
	}
	if got := physicalReads.Load(); got != 0 {
		t.Fatalf("overlay caused %d physical reads, want 0", got)
	}

	server.mu.Lock()
	server.deleteOpenDocumentLocked(uri)
	server.mu.Unlock()
	text, err = server.readWorkspaceTextFileCached(context.Background(), path, "auto", nil, true)
	if err != nil {
		t.Fatalf("closed-overlay fallback returned error: %v", err)
	}
	if text != "physical source" {
		t.Fatalf("closed-overlay fallback = %q, want physical source", text)
	}
	if got := physicalReads.Load(); got != 1 {
		t.Fatalf("closed-overlay physical reads = %d, want 1", got)
	}

	text, err = server.readWorkspaceTextFileCached(context.Background(), path, "auto", nil, true)
	if err != nil {
		t.Fatalf("cached fallback returned error: %v", err)
	}
	if text != "physical source" {
		t.Fatalf("cached fallback = %q, want physical source", text)
	}
	if got := physicalReads.Load(); got != 1 {
		t.Fatalf("cached fallback physical reads = %d, want 1", got)
	}
}

func TestDidSaveInvalidatesPhysicalSourceSnapshot(t *testing.T) {
	path := writeSourceSnapshotFixture(t, "saved.asp", "before save")
	uri := filePathURI(path)
	server := New(nil, io.Discard, io.Discard)
	defer server.shutdownRuntimeCaches()
	configureSourceSnapshotBoundary(server, path)
	var physicalReads atomic.Int32
	server.workspaceFileReadTestHook = func(string) { physicalReads.Add(1) }
	assertSourceSnapshotBytes(t, server, path, "before save")
	if err := os.WriteFile(path, []byte("after save"), 0o644); err != nil {
		t.Fatal(err)
	}
	if err := server.handleNotification(context.Background(), "textDocument/didSave", mustRaw(map[string]any{
		"textDocument": map[string]any{"uri": uri},
	})); err != nil {
		t.Fatal(err)
	}
	assertSourceSnapshotBytes(t, server, path, "after save")
	if got := physicalReads.Load(); got != 2 {
		t.Fatalf("physical reads after save = %d, want 2", got)
	}
}

func TestWatchedDiskChangeUnderOpenOverlayBecomesCloseFallback(t *testing.T) {
	path := writeSourceSnapshotFixture(t, "overlay-watcher.asp", "indexed source")
	uri := filePathURI(path)
	server := New(nil, io.Discard, io.Discard)
	defer server.shutdownRuntimeCaches()
	configureSourceSnapshotBoundary(server, path)
	var physicalReads atomic.Int32
	server.workspaceFileReadTestHook = func(string) { physicalReads.Add(1) }
	server.workspace[uri] = core.NewTextDocument(uri, "classic-asp", 0, "indexed source")
	assertSourceSnapshotBytes(t, server, path, "indexed source")
	server.mu.Lock()
	server.rememberOpenDocumentLocked(uri, core.NewTextDocument(uri, "classic-asp", 1, "open overlay"))
	server.mu.Unlock()
	if err := os.WriteFile(path, []byte("external source"), 0o644); err != nil {
		t.Fatal(err)
	}
	if err := server.didChangeWatchedFiles(didChangeWatchedFilesParams{Changes: []fileEvent{{URI: uri, Type: fileChangeChanged}}}); err != nil {
		t.Fatal(err)
	}
	if err := server.handleNotification(context.Background(), "textDocument/didClose", mustRaw(map[string]any{
		"textDocument": map[string]any{"uri": uri},
	})); err != nil {
		t.Fatal(err)
	}
	text, err := server.readWorkspaceTextFileCached(context.Background(), path, "auto", nil, true)
	if err != nil {
		t.Fatal(err)
	}
	if text != "external source" {
		t.Fatalf("closed overlay fallback = %q, want external source", text)
	}
	if got := physicalReads.Load(); got != 2 {
		t.Fatalf("physical reads after watched overlay change = %d, want 2", got)
	}
}

func TestMetadataReindexInvalidatesSameServerSourceSnapshot(t *testing.T) {
	root := t.TempDir()
	path := filepath.Join(root, "metadata.asp")
	if err := os.WriteFile(path, []byte("<% Sub OldName(): End Sub %>"), 0o644); err != nil {
		t.Fatal(err)
	}
	uri := filePathURI(path)
	server := New(nil, io.Discard, io.Discard)
	defer server.shutdownRuntimeCaches()
	configureSourceSnapshotBoundary(server, path)
	server.rootPath = root
	server.rootURI = filePathURI(root)
	server.workspaceRoots = []workspaceRoot{{Path: root, URI: filePathURI(root)}}
	server.settings.CacheEnabled = true
	server.settings.CacheDirectory = filepath.Join(root, "cache")
	server.settings.CacheFreshness = "metadata"
	server.settings.CacheTTLHours = 24
	server.settings.CacheMaxSizeMB = 16
	server.configureDiskAnalysisCache()
	server.configureFsGateway()
	var physicalReads atomic.Int32
	server.workspaceFileReadTestHook = func(string) { physicalReads.Add(1) }
	assertSourceSnapshotBytes(t, server, path, "<% Sub OldName(): End Sub %>")
	server.writeWorkspaceIndexToDisk(map[string]*core.TextDocument{
		uri: core.NewTextDocument(uri, "classic-asp", 0, "<% Sub OldName(): End Sub %>"),
	})
	const fresh = "<% Sub NewNameWithDifferentLength(): End Sub %>"
	if err := os.WriteFile(path, []byte(fresh), 0o644); err != nil {
		t.Fatal(err)
	}
	documents, cacheHit, ok := server.buildWorkspaceIndex(context.Background(), 0, workspaceIndexRunSettings{
		roots: server.workspaceRoots, legacyEncoding: "auto",
	})
	if !ok || cacheHit {
		t.Fatalf("metadata reindex result ok=%t cacheHit=%t", ok, cacheHit)
	}
	if got := documents[uri]; got == nil || got.Text != fresh {
		t.Fatalf("metadata reindex document = %#v, want fresh source", got)
	}
	if got := physicalReads.Load(); got != 2 {
		t.Fatalf("metadata reindex physical reads = %d, want 2", got)
	}
}

func TestMetadataReindexWithoutDatabaseEntryInvalidatesSameServerSourceSnapshot(t *testing.T) {
	root := t.TempDir()
	path := filepath.Join(root, "uncached-metadata.asp")
	if err := os.WriteFile(path, []byte("<% Sub OldUncachedName(): End Sub %>"), 0o644); err != nil {
		t.Fatal(err)
	}
	uri := filePathURI(path)
	server := New(nil, io.Discard, io.Discard)
	defer server.shutdownRuntimeCaches()
	server.rootPath = root
	server.rootURI = filePathURI(root)
	server.workspaceRoots = []workspaceRoot{{Path: root, URI: filePathURI(root)}}
	server.settings.CacheFreshness = "metadata"
	var physicalReads atomic.Int32
	server.workspaceFileReadTestHook = func(string) { physicalReads.Add(1) }
	assertSourceSnapshotBytes(t, server, path, "<% Sub OldUncachedName(): End Sub %>")
	const fresh = "<% Sub NewUncachedNameWithDifferentLength(): End Sub %>"
	if err := os.WriteFile(path, []byte(fresh), 0o644); err != nil {
		t.Fatal(err)
	}
	documents, cacheHit, ok := server.buildWorkspaceIndex(context.Background(), 0, workspaceIndexRunSettings{
		roots: server.workspaceRoots, legacyEncoding: "auto",
	})
	if !ok || cacheHit {
		t.Fatalf("uncached metadata reindex result ok=%t cacheHit=%t", ok, cacheHit)
	}
	if got := documents[uri]; got == nil || got.Text != fresh {
		t.Fatalf("uncached metadata reindex document = %#v, want fresh source", got)
	}
	if got := physicalReads.Load(); got != 2 {
		t.Fatalf("uncached metadata reindex physical reads = %d, want 2", got)
	}
}

func TestWatchedReadFailureDropsBackingDocumentBeforeOverlayClose(t *testing.T) {
	path := writeSourceSnapshotFixture(t, "overlay-read-failure.asp", "indexed source")
	uri := filePathURI(path)
	server := New(nil, io.Discard, io.Discard)
	defer server.shutdownRuntimeCaches()
	configureSourceSnapshotBoundary(server, path)
	var physicalReads atomic.Int32
	server.workspaceFileReadTestHook = func(string) { physicalReads.Add(1) }
	server.workspace[uri] = core.NewTextDocument(uri, "classic-asp", 0, "indexed source")
	assertSourceSnapshotBytes(t, server, path, "indexed source")
	server.mu.Lock()
	server.rememberOpenDocumentLocked(uri, core.NewTextDocument(uri, "classic-asp", 1, "open overlay"))
	server.mu.Unlock()
	if err := os.Remove(path); err != nil {
		t.Fatal(err)
	}
	if err := server.didChangeWatchedFiles(didChangeWatchedFilesParams{Changes: []fileEvent{{URI: uri, Type: fileChangeChanged}}}); err != nil {
		t.Fatal(err)
	}
	server.mu.Lock()
	backing := server.workspaceDocumentByURILocked(uri)
	server.mu.Unlock()
	if backing != nil {
		t.Fatalf("failed watcher read retained stale backing document: %#v", backing)
	}
	if err := os.WriteFile(path, []byte("recovered source"), 0o644); err != nil {
		t.Fatal(err)
	}
	if err := server.handleNotification(context.Background(), "textDocument/didClose", mustRaw(map[string]any{
		"textDocument": map[string]any{"uri": uri},
	})); err != nil {
		t.Fatal(err)
	}
	text, err := server.readWorkspaceTextFileCached(context.Background(), path, "auto", nil, true)
	if err != nil {
		t.Fatal(err)
	}
	if text != "recovered source" {
		t.Fatalf("recovered close fallback = %q, want recovered source", text)
	}
	if got := physicalReads.Load(); got != 3 {
		t.Fatalf("physical reads after watcher recovery = %d, want 3", got)
	}
}

type sourceSnapshotReadResult struct {
	raw []byte
	err error
}

func writeSourceSnapshotFixture(t *testing.T, name, text string) string {
	t.Helper()
	path := filepath.Join(t.TempDir(), name)
	if err := os.WriteFile(path, []byte(text), 0o644); err != nil {
		t.Fatal(err)
	}
	return path
}

func assertSourceSnapshotBytes(t *testing.T, server *Server, path, want string) {
	t.Helper()
	raw, err := server.readSourceFileBytes(context.Background(), path, nil)
	if err != nil {
		t.Fatalf("source read returned error: %v", err)
	}
	if got := string(raw); got != want {
		t.Fatalf("source read = %q, want %q", got, want)
	}
}

func configureSourceSnapshotBoundary(server *Server, path string) {
	root := filepath.Dir(filepath.Clean(path))
	server.rootPath = root
	server.rootURI = filePathURI(root)
	server.workspaceRoots = []workspaceRoot{{Path: root, URI: server.rootURI}}
}
