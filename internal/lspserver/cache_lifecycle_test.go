package lspserver

import (
	"bytes"
	"context"
	"io"
	"os"
	"path/filepath"
	"strings"
	"sync/atomic"
	"testing"
	"time"

	workspacepkg "github.com/yottonoko/classic-web-system-lsp/internal/workspace"
)

func TestClearRuntimeDiskCachePreservesUnownedFilesAndReopensWorkspaceDatabase(t *testing.T) {
	workspacePath := t.TempDir()
	cacheRoot := t.TempDir()
	server := New(nil, io.Discard, io.Discard)
	server.rootPath = workspacePath
	server.rootURI = filePathURI(workspacePath)
	server.workspaceRoots = []workspaceRoot{{Path: workspacePath, URI: filePathURI(workspacePath)}}
	server.settings.CacheEnabled = true
	server.settings.CacheDirectory = cacheRoot
	server.configureDiskAnalysisCache()
	t.Cleanup(server.shutdownRuntimeCaches)

	server.activateWorkspaceIndexing(context.Background())
	server.scheduleWorkspaceIndex("test.beforeClear")
	waitForWorkspaceIndexCompletion(t, server)

	oldCache := server.diskCacheForUse()
	if oldCache == nil || !oldCache.Enabled() {
		t.Fatal("initial workspace database was not opened")
	}
	if err := oldCache.WriteWorkspaceIndex(workspacepkg.DiskWorkspaceIndexCacheEntry{
		SettingsKey: "before-clear",
		Entries:     []workspacepkg.DiskWorkspaceIndexedDocument{{URI: "file:///cached.asp", Text: "cached"}},
	}); err != nil {
		t.Fatal(err)
	}
	if err := oldCache.Flush(); err != nil {
		t.Fatal(err)
	}

	server.pauseAsyncDiskCacheWrites()
	unownedFile := filepath.Join(cacheRoot, "keep.txt")
	unownedDirectoryFile := filepath.Join(cacheRoot, "user-data", "keep.txt")
	legacyLoose := filepath.Join(cacheRoot, "loose.cbor")
	legacyShard := filepath.Join(cacheRoot, "af")
	legacySharded := filepath.Join(legacyShard, "entry.cbor")
	legacyShardKeep := filepath.Join(legacyShard, "keep.txt")
	for fileName, contents := range map[string]string{
		unownedFile:          "keep",
		unownedDirectoryFile: "keep",
		legacyLoose:          "legacy",
		legacySharded:        "legacy",
		legacyShardKeep:      "keep",
	} {
		if err := os.MkdirAll(filepath.Dir(fileName), 0o755); err != nil {
			t.Fatal(err)
		}
		if err := os.WriteFile(fileName, []byte(contents), 0o644); err != nil {
			t.Fatal(err)
		}
	}
	databasePath := onlyWorkspaceDatabasePath(t, cacheRoot)
	for _, suffix := range []string{".compact", ".backup"} {
		if err := os.WriteFile(databasePath+suffix, []byte("stale"), 0o600); err != nil {
			t.Fatal(err)
		}
	}
	server.resumeAsyncDiskCacheWrites()

	server.mu.Lock()
	generationBeforeClear := server.workspaceIndexGeneration
	server.mu.Unlock()
	server.clearRuntimeDiskCache()
	waitForWorkspaceIndexCompletion(t, server)
	server.pauseAsyncDiskCacheWrites()
	defer server.resumeAsyncDiskCacheWrites()

	if oldCache.Enabled() {
		t.Fatal("clear left the previous database open")
	}
	newCache := server.diskCacheForUse()
	if newCache == nil || !newCache.Enabled() || newCache == oldCache {
		t.Fatal("clear did not reopen a fresh workspace database")
	}
	if _, ok := newCache.ReadWorkspaceIndex("before-clear"); ok {
		t.Fatal("clear preserved a workspace record from the old database")
	}
	if err := newCache.Flush(); err != nil {
		t.Fatalf("reopened database is unusable: %v", err)
	}
	server.mu.Lock()
	generationAfterClear := server.workspaceIndexGeneration
	server.mu.Unlock()
	if generationAfterClear <= generationBeforeClear {
		t.Fatalf("workspace index was not rescheduled: before=%d after=%d", generationBeforeClear, generationAfterClear)
	}

	for _, fileName := range []string{unownedFile, unownedDirectoryFile, legacyShardKeep} {
		if _, err := os.Stat(fileName); err != nil {
			t.Fatalf("clear removed unowned file %q: %v", fileName, err)
		}
	}
	for _, fileName := range []string{legacyLoose, legacySharded, databasePath + ".compact", databasePath + ".backup"} {
		if _, err := os.Stat(fileName); !os.IsNotExist(err) {
			t.Fatalf("cache-owned file %q remains after clear: %v", fileName, err)
		}
	}
	_ = onlyWorkspaceDatabasePath(t, cacheRoot)
}

func TestStdioInitializedWorkspaceIndexSurvivesNotificationReturn(t *testing.T) {
	client := startStdioTestClient(t)
	defer client.close()

	root := t.TempDir()
	writeWorkspaceIndexFixture(t, filepath.Join(root, "notification-lifetime.asp"), `<%
Function NotificationLifetimeSymbol()
End Function
%>`)
	initializeAndWaitForWorkspaceIndex(t, client, map[string]any{
		"processId":    nil,
		"rootUri":      pathToFileURI(root),
		"capabilities": map[string]any{},
	})

	symbols := mustJSONText(t, client.request("workspace/symbol", map[string]any{"query": "NotificationLifetime"}).Result)
	if !strings.Contains(symbols, "NotificationLifetimeSymbol") {
		t.Fatalf("workspace worker was cancelled with initialized notification: %s", symbols)
	}
}

func TestShutdownWaitsForAsyncCacheWritesAndClosesDatabase(t *testing.T) {
	root := t.TempDir()
	server := New(nil, io.Discard, io.Discard)
	server.rootPath = root
	server.rootURI = filePathURI(root)
	server.workspaceRoots = []workspaceRoot{{Path: root, URI: filePathURI(root)}}
	server.settings.CacheEnabled = true
	server.settings.CacheDirectory = t.TempDir()
	server.configureDiskAnalysisCache()
	cache := server.diskCacheForUse()
	if cache == nil || !cache.Enabled() {
		t.Fatal("workspace database was not opened")
	}

	writeStarted := make(chan struct{})
	releaseWrite := make(chan struct{})
	if !server.runAsyncDiskCacheWrite(func() {
		close(writeStarted)
		<-releaseWrite
	}) {
		t.Fatal("async cache write was not started")
	}
	waitForWorkspaceIndexSignal(t, writeStarted)

	shutdownDone := make(chan struct{})
	go func() {
		_, _ = server.handleRequest(context.Background(), "shutdown", nil)
		close(shutdownDone)
	}()
	select {
	case <-shutdownDone:
		t.Fatal("shutdown returned before an active cache write completed")
	case <-time.After(50 * time.Millisecond):
	}
	close(releaseWrite)
	waitForWorkspaceIndexSignal(t, shutdownDone)

	if cache.Enabled() {
		t.Fatal("shutdown left the workspace database open")
	}
	server.mu.Lock()
	activeCache := server.diskAnalysisCache
	writesClosed := server.diskCacheWritesClosed
	server.mu.Unlock()
	if activeCache != nil || !writesClosed {
		t.Fatalf("shutdown lifecycle state = cache:%p writesClosed:%v", activeCache, writesClosed)
	}
}

func TestAsyncCacheWritesUseBoundedWorker(t *testing.T) {
	server := New(nil, io.Discard, io.Discard)
	const writes = 32
	start := make(chan struct{}, writes)
	release := make(chan struct{})
	var active atomic.Int32
	var maximum atomic.Int32
	for range writes {
		if !server.runAsyncDiskCacheWrite(func() {
			current := active.Add(1)
			for {
				previous := maximum.Load()
				if current <= previous || maximum.CompareAndSwap(previous, current) {
					break
				}
			}
			start <- struct{}{}
			<-release
			active.Add(-1)
		}) {
			t.Fatal("async cache write was rejected")
		}
	}
	waitForWorkspaceIndexSignal(t, start)
	if got := maximum.Load(); got != 1 {
		t.Fatalf("concurrent cache writes = %d, want 1", got)
	}
	close(release)
	server.pauseAsyncDiskCacheWrites()
	if got := maximum.Load(); got != 1 {
		t.Fatalf("maximum concurrent cache writes = %d, want 1", got)
	}
}

func TestConfigureDiskCacheWaitsForWritesBeforeCloseAndReopen(t *testing.T) {
	root := t.TempDir()
	firstCacheRoot := t.TempDir()
	secondCacheRoot := t.TempDir()
	server := New(nil, io.Discard, io.Discard)
	server.rootPath = root
	server.rootURI = filePathURI(root)
	server.workspaceRoots = []workspaceRoot{{Path: root, URI: filePathURI(root)}}
	server.settings.CacheEnabled = true
	server.settings.CacheDirectory = firstCacheRoot
	server.configureDiskAnalysisCache()
	t.Cleanup(server.shutdownRuntimeCaches)
	firstCache := server.diskCacheForUse()
	if firstCache == nil || !firstCache.Enabled() {
		t.Fatal("first workspace database was not opened")
	}

	writeStarted := make(chan struct{})
	releaseWrite := make(chan struct{})
	if !server.runAsyncDiskCacheWrite(func() {
		close(writeStarted)
		<-releaseWrite
	}) {
		t.Fatal("async cache write was not started")
	}
	waitForWorkspaceIndexSignal(t, writeStarted)
	server.mu.Lock()
	server.settings.CacheDirectory = secondCacheRoot
	server.mu.Unlock()

	configured := make(chan struct{})
	go func() {
		server.configureDiskAnalysisCache()
		close(configured)
	}()
	waitForServerCondition(t, func() bool {
		server.mu.Lock()
		defer server.mu.Unlock()
		return server.diskCacheWritesClosed
	})
	if server.runAsyncDiskCacheWrite(func() {}) {
		t.Fatal("configuration change accepted a new write after cache writes were paused")
	}
	select {
	case <-configured:
		t.Fatal("configuration returned before the active cache write completed")
	case <-time.After(50 * time.Millisecond):
	}
	close(releaseWrite)
	waitForWorkspaceIndexSignal(t, configured)

	secondCache := server.diskCacheForUse()
	if firstCache.Enabled() {
		t.Fatal("configuration left the previous database open")
	}
	if secondCache == nil || !secondCache.Enabled() || secondCache == firstCache {
		t.Fatal("configuration did not open a replacement database")
	}
	if secondCache.Directory() != secondCacheRoot {
		t.Fatalf("replacement database directory = %q, want %q", secondCache.Directory(), secondCacheRoot)
	}
	server.mu.Lock()
	writesClosed := server.diskCacheWritesClosed
	server.mu.Unlock()
	if writesClosed {
		t.Fatal("configuration did not resume cache writes")
	}
}

func TestDiskCacheOpenWarningIsLoggedOncePerServer(t *testing.T) {
	root := t.TempDir()
	cacheRoot := t.TempDir()
	newServer := func(output io.Writer) *Server {
		server := New(nil, output, io.Discard)
		server.rootPath = root
		server.rootURI = filePathURI(root)
		server.workspaceRoots = []workspaceRoot{{Path: root, URI: filePathURI(root)}}
		server.settings.CacheEnabled = true
		server.settings.CacheDirectory = cacheRoot
		return server
	}

	holder := newServer(io.Discard)
	holder.configureDiskAnalysisCache()
	defer holder.shutdownRuntimeCaches()
	if holder.diskCacheForUse() == nil {
		t.Fatal("lock holder did not open its database")
	}

	var output bytes.Buffer
	contender := newServer(&output)
	contender.configureDiskAnalysisCache()
	contender.configureDiskAnalysisCache()
	defer contender.shutdownRuntimeCaches()
	if got := strings.Count(output.String(), "analysisDatabase.open.failed"); got != 1 {
		t.Fatalf("disk cache open warning count = %d, want 1; output=%s", got, output.String())
	}
}

func TestConfigureDiskCacheResetsOnlyStaleReferenceSchema(t *testing.T) {
	root := t.TempDir()
	cacheRoot := t.TempDir()
	var output bytes.Buffer
	server := New(nil, &output, io.Discard)
	server.rootPath = root
	server.rootURI = filePathURI(root)
	server.workspaceRoots = []workspaceRoot{{Path: root, URI: filePathURI(root)}}
	server.settings.CacheEnabled = true
	server.settings.CacheDirectory = cacheRoot
	server.settings.DebugOutput = "summary"
	server.configureDiskAnalysisCache()
	t.Cleanup(server.shutdownRuntimeCaches)

	cache := server.diskCacheForUse()
	if cache == nil || !cache.Enabled() {
		t.Fatal("disk cache was not opened")
	}
	graphPayload := []byte(`{"nodes":[{"id":"keep"}]}`)
	if err := cache.WriteGraphPayload(workspacepkg.DiskGraphPayloadCacheEntry{SettingsKey: "graph", Payload: graphPayload}); err != nil {
		t.Fatal(err)
	}
	if _, err := cache.EnsureReferenceSchema(diskReferenceCacheSchemaVersion + 1); err != nil {
		t.Fatal(err)
	}
	server.configureDiskAnalysisCache()

	reopened := server.diskCacheForUse()
	if reopened == nil {
		t.Fatal("disk cache was not reopened")
	}
	if reset, err := reopened.EnsureReferenceSchema(diskReferenceCacheSchemaVersion); err != nil || reset {
		t.Fatalf("reference schema after configure = %v, %v; want false, nil", reset, err)
	}
	if graph, ok := reopened.ReadGraphPayload("graph"); !ok || !bytes.Equal(graph.Payload, graphPayload) {
		t.Fatalf("unrelated graph payload = %#v, %v; want preserved", graph, ok)
	}
	logged := output.String()
	for _, field := range []string{"database.referenceSchema.reset", "action=rebuild", "reason=missingOrStale", "schemaVersion=1"} {
		if !strings.Contains(logged, field) {
			t.Fatalf("reference schema reset event missing %q: %s", field, logged)
		}
	}
}

func TestDiskCacheNamespaceNormalizesSortsAndDeduplicatesWorkspaceRoots(t *testing.T) {
	cacheRoot := t.TempDir()
	firstRoot := filepath.Join(t.TempDir(), "first")
	secondRoot := filepath.Join(t.TempDir(), "second")
	for _, root := range []string{firstRoot, secondRoot} {
		if err := os.MkdirAll(root, 0o755); err != nil {
			t.Fatal(err)
		}
	}
	open := func(roots []workspaceRoot) {
		server := New(nil, io.Discard, io.Discard)
		server.workspaceRoots = roots
		server.settings.CacheEnabled = true
		server.settings.CacheDirectory = cacheRoot
		server.configureDiskAnalysisCache()
		server.shutdownRuntimeCaches()
	}
	open([]workspaceRoot{
		{Path: filepath.Join(firstRoot, "child", "..")},
		{Path: secondRoot},
		{Path: firstRoot},
	})
	firstDatabase := onlyWorkspaceDatabasePath(t, cacheRoot)
	open([]workspaceRoot{{Path: secondRoot}, {Path: firstRoot}})
	if secondDatabase := onlyWorkspaceDatabasePath(t, cacheRoot); secondDatabase != firstDatabase {
		t.Fatalf("normalized workspace roots selected different databases: %q != %q", firstDatabase, secondDatabase)
	}
}

func onlyWorkspaceDatabasePath(t *testing.T, cacheRoot string) string {
	t.Helper()
	entries, err := os.ReadDir(filepath.Join(cacheRoot, "bbolt-v1"))
	if err != nil {
		t.Fatal(err)
	}
	var databases []string
	for _, entry := range entries {
		if !entry.IsDir() && filepath.Ext(entry.Name()) == ".db" {
			databases = append(databases, filepath.Join(cacheRoot, "bbolt-v1", entry.Name()))
		}
	}
	if len(databases) != 1 {
		t.Fatalf("workspace database count = %d, want 1: %#v", len(databases), databases)
	}
	return databases[0]
}

func waitForServerCondition(t *testing.T, condition func() bool) {
	t.Helper()
	deadline := time.Now().Add(5 * time.Second)
	for time.Now().Before(deadline) {
		if condition() {
			return
		}
		time.Sleep(time.Millisecond)
	}
	t.Fatal("timed out waiting for server condition")
}
