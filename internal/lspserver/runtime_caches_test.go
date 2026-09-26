package lspserver

import (
	"bytes"
	"context"
	"encoding/json"
	"io"
	"math"
	"os"
	"path/filepath"
	"strconv"
	"strings"
	"sync"
	"testing"
	"time"

	"github.com/yottonoko/classic-web-system-lsp/internal/core"
	"github.com/yottonoko/classic-web-system-lsp/internal/graph"
	"github.com/yottonoko/classic-web-system-lsp/internal/lsp"
	"github.com/yottonoko/classic-web-system-lsp/internal/vbscript"
	workspacepkg "github.com/yottonoko/classic-web-system-lsp/internal/workspace"
)

func TestDiskCacheSweepDoesNotBlockCacheConfiguration(t *testing.T) {
	key := diskCacheSweepKey{directory: t.TempDir(), ttl: time.Hour, maxSize: 4 * 1024 * 1024 * 1024}
	started := make(chan struct{})
	release := make(chan struct{})
	finished := make(chan struct{})
	var releaseOnce sync.Once
	defer releaseOnce.Do(func() { close(release) })

	returned := make(chan struct{})
	go func() {
		scheduleDiskCacheSweep(key, time.Now(), func() error {
			close(started)
			<-release
			close(finished)
			return nil
		})
		close(returned)
	}()

	select {
	case <-started:
	case <-time.After(time.Second):
		t.Fatal("cache sweep did not start")
	}
	select {
	case <-returned:
	case <-time.After(100 * time.Millisecond):
		t.Fatal("cache configuration waited for disk cache sweep")
	}
	releaseOnce.Do(func() { close(release) })
	select {
	case <-finished:
	case <-time.After(time.Second):
		t.Fatal("cache sweep did not finish")
	}
}

func TestRuntimeDiskCacheClearStopsBeforeLifecycleMutationAfterCancellation(t *testing.T) {
	root := t.TempDir()
	server := New(nil, io.Discard, io.Discard)
	server.rootPath = root
	server.rootURI = filePathURI(root)
	server.workspaceRoots = []workspaceRoot{{Path: root, URI: filePathURI(root)}}
	server.settings.CacheEnabled = true
	server.settings.CacheDirectory = filepath.Join(root, "cache")
	server.configureDiskAnalysisCache()
	defer server.shutdownRuntimeCaches()
	server.mu.Lock()
	original := server.diskAnalysisCache
	server.mu.Unlock()
	if original == nil {
		t.Fatal("disk cache was not configured")
	}

	started := make(chan struct{})
	server.runtimeCacheDiskClearBeforeLifecycleTestHook = func(ctx context.Context) {
		close(started)
		<-ctx.Done()
	}
	ctx, cancel := context.WithCancel(context.Background())
	result := make(chan bool, 1)
	go func() { result <- server.clearRuntimeDiskCacheContext(ctx) }()
	waitForConcurrencySignal(t, started, "disk-cache clear did not enter its cancellable body")
	cancel()
	if completed := <-result; completed {
		t.Fatal("cancelled disk-cache clear reported completion")
	}
	server.mu.Lock()
	current := server.diskAnalysisCache
	server.mu.Unlock()
	if current != original {
		t.Fatal("cancelled disk-cache clear replaced the active cache")
	}
}

func TestLegacyDiskCacheCleanupDeletesOnlyCBORShards(t *testing.T) {
	root := filepath.Join(t.TempDir(), "cache")
	shard := filepath.Join(root, "af")
	if err := os.MkdirAll(shard, 0o755); err != nil {
		t.Fatal(err)
	}
	legacyFiles := []string{filepath.Join(root, "loose.cbor"), filepath.Join(shard, "entry.cbor")}
	for _, fileName := range legacyFiles {
		if err := os.WriteFile(fileName, []byte("legacy"), 0o644); err != nil {
			t.Fatal(err)
		}
	}
	keptFile := filepath.Join(shard, "keep.txt")
	if err := os.WriteFile(keptFile, []byte("keep"), 0o644); err != nil {
		t.Fatal(err)
	}
	server := New(strings.NewReader(""), io.Discard, io.Discard)
	server.settings.CacheEnabled = true
	server.settings.CacheDirectory = root
	server.rootPath = t.TempDir()
	server.configureDiskAnalysisCache()
	defer server.closeDiskAnalysisCache()
	cache := server.diskCacheForUse()
	if err := server.cleanupLegacyDiskCache(cache, root); err != nil {
		t.Fatal(err)
	}
	for _, fileName := range legacyFiles {
		if _, err := os.Stat(fileName); !os.IsNotExist(err) {
			t.Fatalf("legacy cache file %q still exists: %v", fileName, err)
		}
	}
	if _, err := os.Stat(keptFile); err != nil {
		t.Fatalf("non-CBOR file was removed: %v", err)
	}
}

func TestDiskIncludeFingerprintTraversesCachedGraphWithoutParsingIncludes(t *testing.T) {
	root := t.TempDir()
	ownerPath := filepath.Join(root, "default.asp")
	childPath := filepath.Join(root, "child.inc")
	grandchildPath := filepath.Join(root, "grandchild.inc")
	ownerText := `<!-- #include file="child.inc" --><% Response.Write ChildValue %>`
	for fileName, text := range map[string]string{
		ownerPath:      ownerText,
		childPath:      `<!-- #include file="grandchild.inc" --><% ChildValue = GrandchildValue %>`,
		grandchildPath: `<% Const GrandchildValue = 1 %>`,
	} {
		if err := os.WriteFile(fileName, []byte(text), 0o644); err != nil {
			t.Fatal(err)
		}
	}
	server := New(strings.NewReader(""), io.Discard, io.Discard)
	defer server.closeDiskAnalysisCache()
	server.rootPath = root
	server.rootURI = filePathURI(root)
	server.workspaceRoots = []workspaceRoot{{Path: root, URI: filePathURI(root)}}
	server.configureFsGateway()
	parsed := core.ParseDocument(filePathURI(ownerPath), ownerText, core.Settings{DefaultLanguage: "VBScript"})
	childInfo, _ := server.fsStat(childPath)
	grandchildInfo, _ := server.fsStat(grandchildPath)
	server.workspaceIncludeGraph.Reset("test")
	server.workspaceIncludeGraph.Upsert(childPath, workspacepkg.SourceMetadata{FileName: childPath, MtimeMS: childInfo.MtimeMS, Size: childInfo.Size}, []string{grandchildPath}, "child-refs")
	server.workspaceIncludeGraph.Upsert(grandchildPath, workspacepkg.SourceMetadata{FileName: grandchildPath, MtimeMS: grandchildInfo.MtimeMS, Size: grandchildInfo.Size}, nil, "grandchild-refs")

	first := server.diskIncludeFingerprint(parsed)
	server.mu.Lock()
	parsedEntries := len(server.parsedCache)
	server.workspaceIncludeGraph.Upsert(childPath, workspacepkg.SourceMetadata{FileName: childPath, MtimeMS: childInfo.MtimeMS, Size: childInfo.Size}, []string{grandchildPath}, "changed-child-refs")
	server.mu.Unlock()
	second := server.diskIncludeFingerprint(parsed)
	if first == "" || first == second {
		t.Fatalf("include graph fingerprint did not reflect cached edge identity: first=%q second=%q", first, second)
	}
	if parsedEntries != 0 {
		t.Fatalf("include fingerprint parsed %d included files; want zero", parsedEntries)
	}
}

func TestDiskIncludeFingerprintWithoutIncludesIsStable(t *testing.T) {
	server := New(strings.NewReader(""), io.Discard, io.Discard)
	parsed := core.ParseDocument("file:///no-includes.asp", `<% value = 1 %>`, core.Settings{DefaultLanguage: "VBScript"})
	server.workspaceIncludeGraph.Reset("test")
	root := t.TempDir()
	for index := range 1000 {
		path := filepath.Join(root, strconv.Itoa(index)+".inc")
		server.workspaceIncludeGraph.Upsert(path, workspacepkg.SourceMetadata{FileName: path}, nil, strconv.Itoa(index))
	}
	if got, want := server.diskIncludeFingerprint(parsed), workspacepkg.DiskContentHash(""); got != want {
		t.Fatalf("include-free fingerprint = %q, want %q", got, want)
	}
}

func TestWorkspaceIncludeGraphPersistenceCoalescesAndFlushesLatestEdit(t *testing.T) {
	root := t.TempDir()
	cacheDirectory := t.TempDir()
	ownerPath := filepath.Join(root, "default.asp")
	firstChildPath := filepath.Join(root, "first.inc")
	secondChildPath := filepath.Join(root, "second.inc")
	for _, path := range []string{ownerPath, firstChildPath, secondChildPath} {
		if err := os.WriteFile(path, []byte(`<% value = 1 %>`), 0o644); err != nil {
			t.Fatal(err)
		}
	}
	var output bytes.Buffer
	server := New(strings.NewReader(""), &output, io.Discard)
	server.rootPath = root
	server.rootURI = filePathURI(root)
	server.workspaceRoots = []workspaceRoot{{Path: root, URI: filePathURI(root)}}
	server.settings.CacheEnabled = true
	server.settings.CacheDirectory = cacheDirectory
	server.settings.DebugOutput = "summary"
	server.configureDiskAnalysisCache()
	server.configureFsGateway()

	ownerURI := filePathURI(ownerPath)
	first := core.ParseDocument(ownerURI, `<!-- #include file="first.inc" -->`, core.Settings{DefaultLanguage: "VBScript"})
	second := core.ParseDocument(ownerURI, `<!-- #include file="second.inc" -->`, core.Settings{DefaultLanguage: "VBScript"})
	server.refreshWorkspaceIncludeGraphFile(first)
	server.refreshWorkspaceIncludeGraphFile(second)
	server.mu.Lock()
	pending := server.workspaceIncludeGraphPersisting
	server.mu.Unlock()
	if !pending {
		t.Fatal("include graph persistence was not scheduled")
	}

	server.shutdownRuntimeCaches()
	reopened := New(strings.NewReader(""), io.Discard, io.Discard)
	reopened.rootPath = root
	reopened.rootURI = filePathURI(root)
	reopened.workspaceRoots = []workspaceRoot{{Path: root, URI: filePathURI(root)}}
	reopened.settings.CacheEnabled = true
	reopened.settings.CacheDirectory = cacheDirectory
	reopened.configureDiskAnalysisCache()
	defer reopened.shutdownRuntimeCaches()
	entry, ok := reopened.diskCacheForUse().ReadWorkspaceIncludeGraph(reopened.workspaceDiskSettingsKey())
	if !ok || len(entry.Entries) != 1 {
		t.Fatalf("persisted include graph = %#v, found=%v", entry, ok)
	}
	if got := entry.Entries[0].TargetFileNames; len(got) != 1 || filepath.Clean(got[0]) != filepath.Clean(secondChildPath) {
		t.Fatalf("persisted targets = %#v, want latest target %q", got, secondChildPath)
	}
	if count := strings.Count(output.String(), "database.workspaceIncludeGraph.write"); count != 1 {
		t.Fatalf("include graph writes = %d, want one coalesced write; logs: %s", count, output.String())
	}
}

func TestWorkspaceIncludeGraphRestoreRejectsNewlyResolvableInclude(t *testing.T) {
	root := t.TempDir()
	cacheDirectory := t.TempDir()
	ownerPath := filepath.Join(root, "default.asp")
	ownerText := `<!-- #include file="later.inc" -->`
	if err := os.WriteFile(ownerPath, []byte(ownerText), 0o644); err != nil {
		t.Fatal(err)
	}
	first := New(strings.NewReader(""), io.Discard, io.Discard)
	first.rootPath = root
	first.rootURI = filePathURI(root)
	first.workspaceRoots = []workspaceRoot{{Path: root, URI: filePathURI(root)}}
	first.settings.CacheEnabled = true
	first.settings.CacheDirectory = cacheDirectory
	first.configureDiskAnalysisCache()
	first.configureFsGateway()
	first.refreshWorkspaceIncludeGraphFile(core.ParseDocument(filePathURI(ownerPath), ownerText, core.Settings{DefaultLanguage: "VBScript"}))
	first.shutdownRuntimeCaches()

	includePath := filepath.Join(root, "later.inc")
	if err := os.WriteFile(includePath, []byte(`<% value = 1 %>`), 0o644); err != nil {
		t.Fatal(err)
	}
	second := New(strings.NewReader(""), io.Discard, io.Discard)
	second.rootPath = root
	second.rootURI = filePathURI(root)
	second.workspaceRoots = []workspaceRoot{{Path: root, URI: filePathURI(root)}}
	second.settings.CacheEnabled = true
	second.settings.CacheDirectory = cacheDirectory
	second.configureDiskAnalysisCache()
	second.configureFsGateway()
	defer second.shutdownRuntimeCaches()
	if second.restoreWorkspaceIncludeGraphFromDisk() {
		t.Fatal("restored include graph after a missing include became resolvable")
	}
}

func TestWorkspaceIncludeGraphRestoreDoesNotOverwriteLiveMutation(t *testing.T) {
	root := t.TempDir()
	cacheDirectory := t.TempDir()
	ownerPath := filepath.Join(root, "default.asp")
	for _, path := range []string{ownerPath, filepath.Join(root, "first.inc"), filepath.Join(root, "second.inc")} {
		if err := os.WriteFile(path, []byte(`<% value = 1 %>`), 0o644); err != nil {
			t.Fatal(err)
		}
	}
	newServer := func() *Server {
		server := New(strings.NewReader(""), io.Discard, io.Discard)
		server.rootPath = root
		server.rootURI = filePathURI(root)
		server.workspaceRoots = []workspaceRoot{{Path: root, URI: filePathURI(root)}}
		server.settings.CacheEnabled = true
		server.settings.CacheDirectory = cacheDirectory
		server.configureDiskAnalysisCache()
		server.configureFsGateway()
		return server
	}
	first := newServer()
	first.refreshWorkspaceIncludeGraphFile(core.ParseDocument(filePathURI(ownerPath), `<!-- #include file="first.inc" -->`, core.Settings{DefaultLanguage: "VBScript"}))
	first.shutdownRuntimeCaches()

	second := newServer()
	defer second.shutdownRuntimeCaches()
	second.refreshWorkspaceIncludeGraphFile(core.ParseDocument(filePathURI(ownerPath), `<!-- #include file="second.inc" -->`, core.Settings{DefaultLanguage: "VBScript"}))
	if second.restoreWorkspaceIncludeGraphFromDisk() {
		t.Fatal("restored stale include graph over a live mutation")
	}
	second.mu.Lock()
	targets := second.workspaceIncludeGraph.TargetFileNamesForOwner(ownerPath)
	second.mu.Unlock()
	if len(targets) != 1 || workspacepkg.FileIdentityKeyFromFileName(targets[0]) != workspacepkg.FileIdentityKeyFromFileName(filepath.Join(root, "second.inc")) {
		t.Fatalf("live include graph targets = %#v", targets)
	}
}

func TestWorkspaceIncludeGraphRestoreValidatesSourceFreshness(t *testing.T) {
	tests := []struct {
		name   string
		mutate func(t *testing.T, entry *workspacepkg.DiskWorkspaceIncludeGraphCacheEntry, ownerPath string)
		want   bool
	}{
		{
			name: "content hash rejects changed content with unchanged metadata",
			mutate: func(t *testing.T, entry *workspacepkg.DiskWorkspaceIncludeGraphCacheEntry, ownerPath string) {
				t.Helper()
				if len(entry.Entries) != 1 {
					t.Fatalf("persisted include graph entries = %d, want one", len(entry.Entries))
				}
				mtime := time.UnixMilli(entry.Entries[0].Source.MtimeMS)
				if err := os.WriteFile(ownerPath, []byte(`<!-- #include file="shared.inc" -->
<% value = 2 %>`), 0o644); err != nil {
					t.Fatal(err)
				}
				if err := os.Chtimes(ownerPath, mtime, mtime); err != nil {
					t.Fatal(err)
				}
			},
			want: false,
		},
		{
			name: "legacy source metadata remains compatible",
			mutate: func(t *testing.T, entry *workspacepkg.DiskWorkspaceIncludeGraphCacheEntry, _ string) {
				t.Helper()
				if len(entry.Entries) != 1 {
					t.Fatalf("persisted include graph entries = %d, want one", len(entry.Entries))
				}
				entry.Entries[0].Source.ContentHash = ""
			},
			want: true,
		},
		{
			name: "unknown source metadata is rejected",
			mutate: func(t *testing.T, entry *workspacepkg.DiskWorkspaceIncludeGraphCacheEntry, _ string) {
				t.Helper()
				if len(entry.Entries) != 1 {
					t.Fatalf("persisted include graph entries = %d, want one", len(entry.Entries))
				}
				entry.Entries[0].Source.ContentHash = ""
				entry.Entries[0].Source.MtimeMS = 0
				entry.Entries[0].Source.Size = 0
			},
			want: false,
		},
	}

	for _, test := range tests {
		t.Run(test.name, func(t *testing.T) {
			root := t.TempDir()
			cacheDirectory := t.TempDir()
			ownerPath := filepath.Join(root, "default.asp")
			ownerText := "<!-- #include file=\"shared.inc\" -->\n<% value = 1 %>"
			if err := os.WriteFile(ownerPath, []byte(ownerText), 0o644); err != nil {
				t.Fatal(err)
			}
			if err := os.WriteFile(filepath.Join(root, "shared.inc"), []byte(`<% value = 1 %>`), 0o644); err != nil {
				t.Fatal(err)
			}
			newServer := func() *Server {
				server := New(strings.NewReader(""), io.Discard, io.Discard)
				server.rootPath = root
				server.rootURI = filePathURI(root)
				server.workspaceRoots = []workspaceRoot{{Path: root, URI: filePathURI(root)}}
				server.settings.CacheEnabled = true
				server.settings.CacheDirectory = cacheDirectory
				server.configureDiskAnalysisCache()
				server.configureFsGateway()
				return server
			}

			first := newServer()
			first.refreshWorkspaceIncludeGraphFile(core.ParseDocument(filePathURI(ownerPath), ownerText, core.Settings{DefaultLanguage: "VBScript"}))
			first.waitForAsyncDiskCacheWrites()
			entry, ok := first.diskCacheForUse().ReadWorkspaceIncludeGraph(first.workspaceDiskSettingsKey())
			if !ok {
				first.closeDiskAnalysisCache()
				t.Fatal("include graph was not persisted")
			}
			test.mutate(t, &entry, ownerPath)
			if err := first.diskCacheForUse().WriteWorkspaceIncludeGraph(entry); err != nil {
				first.closeDiskAnalysisCache()
				t.Fatal(err)
			}
			if err := first.diskCacheForUse().Flush(); err != nil {
				first.closeDiskAnalysisCache()
				t.Fatal(err)
			}
			first.closeDiskAnalysisCache()

			second := newServer()
			defer second.closeDiskAnalysisCache()
			second.sourceSnapshots[sourceSnapshotKey(ownerPath)] = &sourceFileSnapshot{
				path:  ownerPath,
				raw:   []byte(ownerText),
				valid: true,
			}
			if got := second.restoreWorkspaceIncludeGraphFromDisk(); got != test.want {
				t.Fatalf("restore result = %t, want %t", got, test.want)
			}
		})
	}
}

func TestWorkspaceIncludeGraphRestoreStopsReadingAfterCancellation(t *testing.T) {
	root := t.TempDir()
	cacheDirectory := t.TempDir()
	parsed := make([]*core.ParsedDocument, 0, 8)
	for index := range 8 {
		path := filepath.Join(root, "file-"+strconv.Itoa(index)+".asp")
		text := `<% Dim value` + strconv.Itoa(index) + ` %>`
		if err := os.WriteFile(path, []byte(text), 0o644); err != nil {
			t.Fatal(err)
		}
		parsed = append(parsed, core.ParseDocument(filePathURI(path), text, core.Settings{DefaultLanguage: "VBScript"}))
	}
	newServer := func() *Server {
		server := New(strings.NewReader(""), io.Discard, io.Discard)
		server.rootPath = root
		server.rootURI = filePathURI(root)
		server.workspaceRoots = []workspaceRoot{{Path: root, URI: filePathURI(root)}}
		server.settings.CacheEnabled = true
		server.settings.CacheDirectory = cacheDirectory
		server.configureDiskAnalysisCache()
		server.configureFsGateway()
		return server
	}

	first := newServer()
	first.syncWorkspaceIncludeGraphCache(parsed)
	first.shutdownRuntimeCaches()

	second := newServer()
	defer second.shutdownRuntimeCaches()
	readStarted := make(chan struct{})
	releaseRead := make(chan struct{})
	reads := 0
	second.workspaceFileReadTestHook = func(string) {
		reads++
		if reads == 1 {
			close(readStarted)
			<-releaseRead
		}
	}
	ctx, cancel := context.WithCancel(context.Background())
	result := make(chan bool, 1)
	go func() { result <- second.restoreWorkspaceIncludeGraphFromDiskContext(ctx) }()
	select {
	case <-readStarted:
	case <-time.After(time.Second):
		t.Fatal("include graph restore did not start its first source read")
	}
	cancel()
	close(releaseRead)
	select {
	case restored := <-result:
		if restored {
			t.Fatal("cancelled include graph restore reported success")
		}
	case <-time.After(time.Second):
		t.Fatal("cancelled include graph restore did not stop")
	}
	if reads != 1 {
		t.Fatalf("cancelled include graph restore read %d files, want 1", reads)
	}
}

func BenchmarkDiskIncludeFingerprintReachableGraph(b *testing.B) {
	root := b.TempDir()
	childPath := filepath.Join(root, "child.inc")
	if err := os.WriteFile(childPath, []byte(`<% ChildValue = 1 %>`), 0o644); err != nil {
		b.Fatal(err)
	}
	server := New(strings.NewReader(""), io.Discard, io.Discard)
	server.rootPath = root
	server.rootURI = filePathURI(root)
	server.workspaceRoots = []workspaceRoot{{Path: root, URI: filePathURI(root)}}
	server.configureFsGateway()
	parsed := core.ParseDocument(filePathURI(filepath.Join(root, "default.asp")), `<!-- #include file="child.inc" -->`, core.Settings{DefaultLanguage: "VBScript"})
	server.workspaceIncludeGraph.Reset("test")
	server.workspaceIncludeGraph.Upsert(childPath, workspacepkg.SourceMetadata{FileName: childPath, ContentHash: "child"}, nil, "child-refs")
	for index := range 10000 {
		path := filepath.Join(root, "unrelated", strconv.Itoa(index)+".inc")
		server.workspaceIncludeGraph.Upsert(path, workspacepkg.SourceMetadata{FileName: path, ContentHash: "unrelated"}, nil, "unrelated-refs")
	}
	b.ReportAllocs()
	b.ResetTimer()
	for b.Loop() {
		if fingerprint := server.diskIncludeFingerprint(parsed); fingerprint == "" {
			b.Fatal("empty fingerprint")
		}
	}
}

func TestJavaScriptProjectFingerprintRestoresPersistedIdentityWithoutWorkspaceWalk(t *testing.T) {
	root := t.TempDir()
	cacheDir := t.TempDir()
	fileName := filepath.Join(root, "app.js")
	if err := os.WriteFile(fileName, []byte(`export const cachedIdentity = 1;`), 0o644); err != nil {
		t.Fatal(err)
	}
	newServer := func(output io.Writer) *Server {
		server := New(strings.NewReader(""), output, io.Discard)
		server.rootPath = root
		server.rootURI = filePathURI(root)
		server.workspaceRoots = []workspaceRoot{{Path: root, URI: filePathURI(root)}}
		server.settings.CacheEnabled = true
		server.settings.CacheDirectory = cacheDir
		server.settings.DebugOutput = "summary"
		server.configureDiskAnalysisCache()
		server.configureFsGateway()
		return server
	}
	first := newServer(io.Discard)
	want := first.javascriptProjectFingerprint()
	first.waitForAsyncDiskCacheWrites()
	first.closeDiskAnalysisCache()

	var output bytes.Buffer
	second := newServer(&output)
	got := second.javascriptProjectFingerprint()
	second.waitForAsyncDiskCacheWrites()
	second.closeDiskAnalysisCache()
	if got != want {
		t.Fatalf("restored JavaScript project fingerprint = %q, want %q", got, want)
	}
	if !strings.Contains(output.String(), "javascriptProjectIdentity.restore") {
		t.Fatalf("JavaScript project identity was rescanned instead of restored: %s", output.String())
	}
}

func TestJavaScriptProjectFingerprintUpdatesOnlyWatchedIdentity(t *testing.T) {
	root := t.TempDir()
	cacheDir := t.TempDir()
	changedPath := filepath.Join(root, "changed.js")
	unchangedPath := filepath.Join(root, "unchanged.js")
	if err := os.WriteFile(changedPath, []byte(`export const changed = 1;`), 0o644); err != nil {
		t.Fatal(err)
	}
	if err := os.WriteFile(unchangedPath, []byte(`export const unchanged = 1;`), 0o644); err != nil {
		t.Fatal(err)
	}

	server := New(strings.NewReader(""), io.Discard, io.Discard)
	server.rootPath = root
	server.rootURI = filePathURI(root)
	server.workspaceRoots = []workspaceRoot{{Path: root, URI: filePathURI(root)}}
	server.settings.CacheEnabled = true
	server.settings.CacheDirectory = cacheDir
	server.configureDiskAnalysisCache()
	server.configureFsGateway()
	defer func() {
		server.waitForAsyncDiskCacheWrites()
		server.closeDiskAnalysisCache()
	}()

	var hookMu sync.Mutex
	walked := []string{}
	read := []string{}
	javascriptProjectIdentityTestHooks.Store(&javascriptProjectIdentityTestHook{
		walkDir: func(path string) {
			hookMu.Lock()
			walked = append(walked, filepath.Clean(path))
			hookMu.Unlock()
		},
		readFile: func(path string) {
			hookMu.Lock()
			read = append(read, filepath.Clean(path))
			hookMu.Unlock()
		},
	})
	defer javascriptProjectIdentityTestHooks.Store(nil)

	before := server.javascriptProjectFingerprint()
	server.waitForAsyncDiskCacheWrites()
	hookMu.Lock()
	walked = nil
	read = nil
	hookMu.Unlock()

	if err := os.WriteFile(changedPath, []byte(`export const changed = 2;`), 0o644); err != nil {
		t.Fatal(err)
	}
	if err := server.didChangeWatchedFiles(didChangeWatchedFilesParams{Changes: []fileEvent{{URI: filePathURI(changedPath), Type: fileChangeChanged}}}); err != nil {
		t.Fatal(err)
	}
	after := server.javascriptProjectFingerprint()
	if before == after {
		t.Fatalf("JavaScript project fingerprint did not change: %q", before)
	}

	hookMu.Lock()
	defer hookMu.Unlock()
	if len(walked) != 0 {
		t.Fatalf("targeted identity update walked workspace: %v", walked)
	}
	if len(read) != 1 || read[0] != filepath.Clean(changedPath) {
		t.Fatalf("targeted identity update read = %v, want only %q", read, changedPath)
	}
	for _, path := range read {
		if path == filepath.Clean(unchangedPath) {
			t.Fatalf("targeted identity update read unchanged project file %q", path)
		}
	}
}

func TestRuntimeDiskCacheRestoresParsedDocuments(t *testing.T) {
	server := newRuntimeCacheTestServer(t)
	doc := core.NewTextDocument("file:///site/default.asp", "classic-asp", 1, `<% Response.Write "ok" %>`)
	parsed := core.ParseDocument(doc.URI, doc.Text, core.Settings{DefaultLanguage: "VBScript"})
	parsed.Errors = []core.ParseError{{Start: 0, End: 1, Message: "restored parsed document"}}
	cache := server.diskCacheForUse()
	if err := cache.WriteFileBundle(workspacepkg.DiskFileBundleCacheEntry{
		DiskAnalysisCacheLookup: server.parsedDiskLookup(doc, "VBScript"),
		Parsed:                  parsed,
		Summary:                 workspacepkg.DiskFileAnalysisSummary{URI: doc.URI},
	}); err != nil {
		t.Fatal(err)
	}
	if err := cache.Flush(); err != nil {
		t.Fatal(err)
	}

	restored := server.parseTextDocument(doc, "VBScript")
	if len(restored.Errors) != 1 || restored.Errors[0].Message != "restored parsed document" {
		t.Fatalf("parseTextDocument() did not restore the persisted parse: %#v", restored.Errors)
	}
}

func TestRuntimeDiskCacheRestoresDiagnosticsWithoutReanalysis(t *testing.T) {
	server := newRuntimeCacheTestServer(t)
	doc := core.NewTextDocument("file:///site/default.asp", "classic-asp", 1, `<% Response.Write "ok" %>`)
	parsed := core.ParseDocument(doc.URI, doc.Text, core.Settings{DefaultLanguage: "VBScript"})
	server.workspace[doc.URI] = doc
	want := []lsp.Diagnostic{{
		Range:   lsp.Range{Start: lsp.Position{}, End: lsp.Position{Character: 1}},
		Source:  "runtime-cache-test",
		Message: "restored diagnostics",
	}}
	cache := server.diskCacheForUse()
	if err := cache.WriteFileBundle(workspacepkg.DiskFileBundleCacheEntry{
		DiskAnalysisCacheLookup: server.diagnosticsDiskLookup(doc, parsed),
		Diagnostics:             want,
	}); err != nil {
		t.Fatal(err)
	}
	if err := cache.Flush(); err != nil {
		t.Fatal(err)
	}

	snapshot := server.diagnosticsSnapshot(t.Context(), doc.URI)
	if !snapshot.ok || len(snapshot.diagnostics) != 1 || snapshot.diagnostics[0].Message != want[0].Message {
		t.Fatalf("diagnosticsSnapshot() = %#v, want persisted diagnostics", snapshot)
	}
}

func TestRuntimeDiskCachePersistsIncludeSummaryAndReferences(t *testing.T) {
	server := newRuntimeCacheTestServer(t)
	doc := core.NewTextDocument("file:///site/default.asp", "classic-asp", 1, `<!-- #include file="shared.inc" -->
	<% Response.Write SharedValue %>`)
	parsed := server.parseTextDocument(doc, "VBScript")
	server.waitForAsyncDiskCacheWrites()
	lookup := server.parsedDiskLookup(doc, "VBScript")
	bundle, ok := server.diskCacheForUse().ReadFileBundle(lookup)
	if !ok || bundle.Summary.URI != doc.URI {
		t.Fatalf("ReadFileBundle() = %#v, %v; want parsed document summary", bundle, ok)
	}
	if len(bundle.Summary.IncludeRefs) != 1 || bundle.Summary.IncludeRefs[0].Path != "shared.inc" {
		t.Fatalf("ReadFileBundle().Summary.IncludeRefs = %#v; want shared.inc", bundle.Summary.IncludeRefs)
	}
	if len(parsed.Includes) != 1 {
		t.Fatalf("parsed includes = %#v, want one include", parsed.Includes)
	}
}

func TestGraphPayloadDiskWriteRunsAsynchronouslyAndRejectsStaleGeneration(t *testing.T) {
	server := newRuntimeCacheTestServer(t)
	payload := graph.Payload{Scope: "workspace"}
	server.storeGraphPayload("current", payload)
	server.waitForAsyncDiskCacheWrites()
	if _, ok := server.diskCacheForUse().ReadGraphPayload(server.graphPayloadDiskSettingsKey("current")); !ok {
		t.Fatal("current graph payload was not persisted asynchronously")
	}

	server.resumeAsyncDiskCacheWrites()
	server.pauseAsyncDiskCacheWrites()
	server.storeGraphPayload("stale", payload)
	server.invalidateGraphBackground()
	server.resumeAsyncDiskCacheWrites()
	server.waitForAsyncDiskCacheWrites()
	if _, ok := server.diskCacheForUse().ReadGraphPayload(server.graphPayloadDiskSettingsKey("stale")); ok {
		t.Fatal("stale graph generation was persisted")
	}
}

func TestRuntimeDiskCachePersistsCompleteFileAnalysisSnapshot(t *testing.T) {
	root := t.TempDir()
	cacheDir := filepath.Join(root, "cache")
	fileName := filepath.Join(root, "default.asp")
	text := `<% Dim GlobalValue
Const GlobalLimit = 10 %><% Sub Main() : Dim LocalValue : LocalValue = GlobalLimit : End Sub
Sub Other()
  Dim LocalValue
  LocalValue = 2
End Sub
Main() %>`
	if err := os.WriteFile(fileName, []byte(text), 0o644); err != nil {
		t.Fatal(err)
	}
	doc := core.NewTextDocument(filePathURI(fileName), "classic-asp", 1, text)
	newServer := func() *Server {
		server := New(strings.NewReader(""), io.Discard, io.Discard)
		server.rootPath = root
		server.rootURI = filePathURI(root)
		server.workspaceRoots = []workspaceRoot{{Path: root, URI: filePathURI(root)}}
		server.settings.CacheEnabled = true
		server.settings.CacheDirectory = cacheDir
		server.settings.CacheTTLHours = 24
		server.settings.CacheMaxSizeMB = 16
		server.configureDiskAnalysisCache()
		return server
	}
	first := newServer()
	parsed := first.parseTextDocument(doc, "VBScript")
	first.waitForAsyncDiskCacheWrites()
	entry, ok := first.diskCacheForUse().ReadFileBundle(first.parsedDiskLookup(doc, "VBScript"))
	if !ok {
		t.Fatal("complete file analysis snapshot was not persisted")
	}
	var snapshot fileAnalysisSnapshot
	if err := json.Unmarshal(entry.AnalysisSnapshot, &snapshot); err != nil {
		t.Fatal(err)
	}
	if snapshot.SchemaVersion != fileAnalysisSnapshotSchemaVersion || len(snapshot.Assignments) != 2 || len(snapshot.Signatures) != 2 || len(snapshot.VirtualDocuments) == 0 || len(snapshot.VBDocumentSymbols) == 0 {
		t.Fatalf("persisted snapshot is incomplete: %#v", snapshot)
	}
	if bytes.Contains(entry.AnalysisSnapshot, []byte("VBSemanticTokens")) {
		t.Fatalf("persisted normal-analysis snapshot contains semantic tokens: %s", entry.AnalysisSnapshot)
	}
	if entry.Parsed == nil || len(entry.Parsed.Analysis) != 0 {
		t.Fatalf("persisted parsed analysis duplicates the typed snapshot: %#v", entry.Parsed)
	}
	if fact := snapshotSymbolFact(t, snapshot, "globalvalue", ""); fact.Kind == "" || fact.Declaration == (lsp.Range{}) {
		t.Fatalf("global declaration fact is incomplete: %#v", fact)
	}
	if fact := snapshotSymbolFact(t, snapshot, "globallimit", ""); fact.ReadCount != 1 {
		t.Fatalf("constant usage count = %#v, want one read", fact)
	}
	if fact := snapshotSymbolFact(t, snapshot, "localvalue", "main"); fact.WriteCount != 1 {
		t.Fatalf("Main.LocalValue fact = %#v, want one write", fact)
	}
	if fact := snapshotSymbolFact(t, snapshot, "localvalue", "other"); fact.WriteCount != 1 {
		t.Fatalf("Other.LocalValue fact = %#v, want one write", fact)
	}
	if len(parsed.Analysis) == 0 {
		t.Fatal("parsed document did not retain reusable analysis facts")
	}
	first.closeDiskAnalysisCache()

	second := newServer()
	defer second.closeDiskAnalysisCache()
	restored := second.parseTextDocument(doc, "VBScript")
	second.waitForAsyncDiskCacheWrites()
	if len(restored.Analysis) == 0 || second.cachedFileAnalysisSnapshot(restored) == nil {
		t.Fatal("persisted analysis snapshot was not restored")
	}
	restoredWithoutSource := *restored
	restoredWithoutSource.Text = ""
	restoredWithoutSource.Regions = nil
	var restoredSemanticTokens lsp.SemanticTokens
	if restoredWithoutSource.LoadAnalysis("vbscript.semantic-tokens.v1", &restoredSemanticTokens) {
		t.Fatal("normal-analysis snapshot hydrated semantic tokens")
	}
	if got := vbscript.DocumentSymbols(&restoredWithoutSource); len(got) != len(snapshot.VBDocumentSymbols) {
		t.Fatalf("document symbols were recomputed instead of restored: got=%d want=%d", len(got), len(snapshot.VBDocumentSymbols))
	}
}

func TestParseTextDocumentBuildsCompleteSnapshotOffTheCallPath(t *testing.T) {
	server, doc := newAsyncSnapshotTestServer(t, 1, `<% Dim first, second : first = 1 : second = first %>`)
	started := make(chan struct{})
	release := make(chan struct{})
	var startedOnce sync.Once
	var releaseOnce sync.Once
	server.fileAnalysisSnapshotTestHook = func() {
		startedOnce.Do(func() { close(started) })
		<-release
	}
	t.Cleanup(func() { releaseOnce.Do(func() { close(release) }) })

	parsedResult := make(chan *core.ParsedDocument, 1)
	go func() { parsedResult <- server.parseTextDocument(doc, "VBScript") }()
	var parsed *core.ParsedDocument
	select {
	case parsed = <-parsedResult:
		if parsed == nil {
			t.Fatal("parseTextDocument() returned nil")
		}
	case <-time.After(2 * time.Second):
		releaseOnce.Do(func() { close(release) })
		t.Fatal("parseTextDocument() waited for the complete analysis snapshot")
	}
	select {
	case <-started:
	case <-time.After(2 * time.Second):
		t.Fatal("background analysis snapshot did not start")
	}
	if snapshot := server.cachedFileAnalysisSnapshot(parsed); snapshot != nil {
		t.Fatal("snapshot completed while its test hook was blocked")
	}
	releaseOnce.Do(func() { close(release) })
	server.waitForAsyncDiskCacheWrites()
	if bundle, ok := server.diskCacheForUse().ReadFileBundle(server.parsedDiskLookup(doc, "VBScript")); !ok || bundle.Parsed == nil || len(bundle.AnalysisSnapshot) == 0 {
		t.Fatalf("complete background bundle = %#v, %v", bundle, ok)
	}
}

func TestFileAnalysisSnapshotRetriesAfterCacheWritesResume(t *testing.T) {
	server, doc := newAsyncSnapshotTestServer(t, 1, `<% Dim first, second : first = 1 : second = first %>`)
	server.pauseAsyncDiskCacheWrites()
	parsed := server.parseTextDocument(doc, "VBScript")
	if snapshot := server.cachedFileAnalysisSnapshot(parsed); snapshot != nil {
		t.Fatal("snapshot was built while cache writes were paused")
	}
	server.resumeAsyncDiskCacheWrites()
	server.waitForAsyncDiskCacheWrites()
	defer server.resumeAsyncDiskCacheWrites()
	if bundle, ok := server.diskCacheForUse().ReadFileBundle(server.parsedDiskLookup(doc, "VBScript")); !ok || len(bundle.AnalysisSnapshot) == 0 {
		t.Fatalf("deferred analysis snapshot bundle = %#v, found=%v", bundle, ok)
	}
}

func TestFileAnalysisSnapshotQueueCoalescesToLatestDocumentVersion(t *testing.T) {
	server, first := newAsyncSnapshotTestServer(t, 1, `<% Dim value : value = 1 %>`)
	started := make(chan struct{})
	release := make(chan struct{})
	if !server.runAsyncDiskCacheWrite(func() {
		close(started)
		<-release
	}) {
		t.Fatal("failed to queue cache-write blocker")
	}
	<-started
	server.parseTextDocument(first, "VBScript")
	second := core.NewTextDocument(first.URI, first.LanguageID, 2, `<% Dim value : value = 2 : Response.Write value %>`)
	server.parseTextDocument(second, "VBScript")
	close(release)
	server.waitForAsyncDiskCacheWrites()

	cache := server.diskCacheForUse()
	if bundle, ok := cache.ReadFileBundle(server.parsedDiskLookup(first, "VBScript")); ok {
		t.Fatalf("superseded document version was persisted: %#v", bundle)
	}
	if bundle, ok := cache.ReadFileBundle(server.parsedDiskLookup(second, "VBScript")); !ok || bundle.Parsed == nil || bundle.Parsed.Text != second.Text || len(bundle.AnalysisSnapshot) == 0 {
		t.Fatalf("latest document bundle = %#v, %v", bundle, ok)
	}
}

func newAsyncSnapshotTestServer(t *testing.T, version int, text string) (*Server, *core.TextDocument) {
	t.Helper()
	root := t.TempDir()
	server := New(strings.NewReader(""), io.Discard, io.Discard)
	server.rootPath = root
	server.rootURI = filePathURI(root)
	server.workspaceRoots = []workspaceRoot{{Path: root, URI: filePathURI(root)}}
	server.settings.CacheEnabled = true
	server.settings.CacheDirectory = filepath.Join(root, "cache")
	server.settings.CacheTTLHours = 24
	server.settings.CacheMaxSizeMB = 16
	server.configureDiskAnalysisCache()
	t.Cleanup(server.shutdownRuntimeCaches)
	fileName := filepath.Join(root, "snapshot.asp")
	return server, core.NewTextDocument(filePathURI(fileName), "classic-asp", version, text)
}

func TestParseTextDocumentCoalescesConcurrentFullAnalysis(t *testing.T) {
	server := New(strings.NewReader(""), io.Discard, io.Discard)
	server.settings.CacheEnabled = false
	server.configureDiskAnalysisCache()
	text := `<% Dim SharedValue : SharedValue = 1 : Response.Write SharedValue %>` + strings.Repeat("\n<div>cached</div>", 20_000)
	doc := core.NewTextDocument("file:///site/concurrent.asp", "classic-asp", 1, text)
	const workers = 16
	joinFailure := make(chan int, 1)
	server.documentParseTestHook = func(string) {
		deadline := time.Now().Add(2 * time.Second)
		for {
			server.mu.Lock()
			joined := 0
			for _, flight := range server.parsedInflight {
				joined += len(flight.publications)
			}
			server.mu.Unlock()
			if joined == workers {
				return
			}
			if time.Now().After(deadline) {
				joinFailure <- joined
				return
			}
			time.Sleep(time.Millisecond)
		}
	}
	start := make(chan struct{})
	results := make([]*core.ParsedDocument, workers)
	var wait sync.WaitGroup
	wait.Add(workers)
	for index := range results {
		go func() {
			defer wait.Done()
			<-start
			if index%2 == 0 {
				results[index] = server.parseTextDocument(doc, "VBScript")
			} else {
				results[index] = server.parseText(doc.URI, doc.Text, "VBScript")
			}
		}()
	}
	close(start)
	wait.Wait()
	select {
	case joined := <-joinFailure:
		t.Fatalf("only %d of %d requests joined the parse flight", joined, workers)
	default:
	}
	for index, parsed := range results {
		if parsed != results[0] {
			t.Fatalf("worker %d received a duplicate parsed document", index)
		}
	}
	server.mu.Lock()
	inflight := len(server.parsedInflight)
	server.mu.Unlock()
	if inflight != 0 {
		t.Fatalf("completed parse left %d in-flight analyses", inflight)
	}
}

func TestParseTextDocumentRejectsStaleConcurrentRevisionPublication(t *testing.T) {
	server := New(strings.NewReader(""), io.Discard, io.Discard)
	server.settings.CacheEnabled = false
	server.configureDiskAnalysisCache()
	const uri = "file:///site/concurrent-revisions.asp"
	oldDocument := core.NewTextDocument(uri, "classic-asp", 1, `<% Dim value : value = 1 %>`)
	newDocument := core.NewTextDocument(uri, "classic-asp", 2, `<% Dim value : value = 2 %>`)
	started := make(chan struct{})
	release := make(chan struct{})
	var first sync.Once
	var callsMu sync.Mutex
	parseCalls := 0
	server.documentParseTestHook = func(string) {
		callsMu.Lock()
		parseCalls++
		callsMu.Unlock()
		blocked := false
		first.Do(func() {
			blocked = true
			close(started)
		})
		if blocked {
			<-release
		}
	}

	oldResult := make(chan *core.ParsedDocument, 1)
	go func() { oldResult <- server.parseTextDocument(oldDocument, "VBScript") }()
	<-started
	newParsed := server.parseTextDocument(newDocument, "VBScript")
	close(release)
	if parsed := <-oldResult; parsed == nil || parsed.Text != oldDocument.Text {
		t.Fatalf("old request result = %#v", parsed)
	}

	server.mu.Lock()
	cached := server.parsedCache[parsedDocumentCacheKey(uri)]
	stored := server.documentStore.Cache[uri]
	server.mu.Unlock()
	if cached.Version != newDocument.Version || cached.Text != newDocument.Text || cached.Parsed != newParsed {
		t.Fatalf("latest parsed cache was replaced by stale analysis: %#v", cached)
	}
	if stored == nil || stored.Version != newDocument.Version || stored.Text != newDocument.Text || stored.Parsed != newParsed {
		t.Fatalf("latest document store was replaced by stale analysis: %#v", stored)
	}
	if reused := server.parseTextDocument(newDocument, "VBScript"); reused != newParsed {
		t.Fatal("latest revision was reparsed after stale completion")
	}
	callsMu.Lock()
	defer callsMu.Unlock()
	if parseCalls != 2 {
		t.Fatalf("full parse calls = %d, want 2", parseCalls)
	}
}

func TestParsedCacheInvalidationRejectsInflightPublication(t *testing.T) {
	server := New(strings.NewReader(""), io.Discard, io.Discard)
	server.settings.CacheEnabled = false
	server.configureDiskAnalysisCache()
	document := core.NewTextDocument("file:///site/closing-overlay.asp", "classic-asp", 7, `<% Dim unsaved %>`)
	started := make(chan struct{})
	release := make(chan struct{})
	server.documentParseTestHook = func(string) {
		close(started)
		<-release
	}

	result := make(chan *core.ParsedDocument, 1)
	go func() { result <- server.parseTextDocument(document, "VBScript") }()
	<-started
	server.mu.Lock()
	server.deleteParsedCacheForURILocked(document.URI)
	server.mu.Unlock()
	close(release)
	if parsed := <-result; parsed == nil || parsed.Text != document.Text {
		t.Fatalf("captured request result = %#v", parsed)
	}

	server.mu.Lock()
	_, cached := server.parsedCache[parsedDocumentCacheKey(document.URI)]
	stored := server.documentStore.Cache[document.URI]
	server.mu.Unlock()
	if cached || stored != nil {
		t.Fatalf("invalidated inflight analysis was published: cached=%v stored=%#v", cached, stored)
	}
}

func TestParseTextDocumentOlderSameSourceCannotDowngradeRevision(t *testing.T) {
	server := New(strings.NewReader(""), io.Discard, io.Discard)
	const uri = "file:///site/same-source-revision.asp"
	newDocument := core.NewTextDocument(uri, "classic-asp", 2, `<% Dim value %>`)
	parsed := server.parseTextDocument(newDocument, "VBScript")
	oldDocument := core.NewTextDocument(uri, "classic-asp", 1, newDocument.Text)
	if reused := server.parseTextDocument(oldDocument, "VBScript"); reused != parsed {
		t.Fatal("same source did not reuse parsed analysis")
	}
	server.mu.Lock()
	cached := server.parsedCache[parsedDocumentCacheKey(uri)]
	server.mu.Unlock()
	if cached.Version != newDocument.Version {
		t.Fatalf("same-source cache revision regressed to %d", cached.Version)
	}
}

func TestInflightOlderSameSourceCannotWinPublication(t *testing.T) {
	server := New(strings.NewReader(""), io.Discard, io.Discard)
	const uri = "file:///site/inflight-same-source.asp"
	text := `<% Dim value %>`
	newDocument := core.NewTextDocument(uri, "classic-asp", 2, text)
	newEntry := parsedDocumentCacheEntry{Version: 2, Text: text, DefaultLanguage: "VBScript"}
	flight, current, cached, leader := server.beginParsedAnalysis(parsedDocumentCacheKey(uri), newDocument, newEntry)
	if !leader || cached != nil {
		t.Fatalf("new revision did not lead the parse: leader=%v cached=%#v", leader, cached)
	}
	oldDocument := core.NewTextDocument(uri, "classic-asp", 1, text)
	oldEntry := parsedDocumentCacheEntry{Version: 1, Text: text, DefaultLanguage: "VBScript"}
	joined, stale, cached, leader := server.beginParsedAnalysis(parsedDocumentCacheKey(uri), oldDocument, oldEntry)
	if leader || cached != nil || joined != flight {
		t.Fatalf("old revision did not join the current parse: leader=%v cached=%#v joined=%p flight=%p", leader, cached, joined, flight)
	}
	parsed := core.ParseDocument(uri, text, core.Settings{DefaultLanguage: "VBScript"})
	server.finishParsedAnalysis(uri, text, "VBScript", flight, parsed)
	if !current.published || stale.published {
		t.Fatalf("publication state: current=%v stale=%v", current.published, stale.published)
	}
	server.mu.Lock()
	entry := server.parsedCache[parsedDocumentCacheKey(uri)]
	server.mu.Unlock()
	if entry.Version != newDocument.Version || entry.Parsed != parsed {
		t.Fatalf("inflight publication regressed: %#v", entry)
	}
}

func TestParsedTextSnapshotDoesNotReplaceCurrentOpenDocumentStore(t *testing.T) {
	server := New(strings.NewReader(""), io.Discard, io.Discard)
	const uri = "file:///site/current-overlay.asp"
	current := core.NewTextDocument(uri, "classic-asp", 3, `<% Dim currentValue %>`)
	server.mu.Lock()
	server.rememberOpenDocumentLocked(uri, current)
	server.rememberDocumentTextLocked(current)
	server.mu.Unlock()

	server.parseText(uri, `<% Dim staleValue %>`, "VBScript")
	server.mu.Lock()
	stored := server.documentStore.Cache[uri]
	server.mu.Unlock()
	if stored == nil || stored.Text != current.Text || stored.Version != current.Version || stored.Parsed != nil {
		t.Fatalf("stale text analysis replaced current document store: %#v", stored)
	}
}

func TestFileAnalysisSnapshotInvalidatesWhenIncludeResolutionChanges(t *testing.T) {
	root := t.TempDir()
	cacheDir := filepath.Join(root, "cache")
	ownerPath := filepath.Join(root, "default.asp")
	includePath := filepath.Join(root, "shared.inc")
	text := `<!-- #include file="shared.inc" --><% Response.Write SharedValue %>`
	if err := os.WriteFile(ownerPath, []byte(text), 0o644); err != nil {
		t.Fatal(err)
	}
	doc := core.NewTextDocument(filePathURI(ownerPath), "classic-asp", 1, text)
	newServer := func() *Server {
		server := New(strings.NewReader(""), io.Discard, io.Discard)
		server.rootPath = root
		server.rootURI = filePathURI(root)
		server.workspaceRoots = []workspaceRoot{{Path: root, URI: filePathURI(root)}}
		server.settings.CacheEnabled = true
		server.settings.CacheDirectory = cacheDir
		server.configureDiskAnalysisCache()
		server.configureFsGateway()
		return server
	}
	first := newServer()
	firstParsed := first.parseTextDocument(doc, "VBScript")
	first.waitForAsyncDiskCacheWrites()
	firstSnapshot := first.cachedFileAnalysisSnapshot(firstParsed)
	if len(firstSnapshot.Includes) != 1 || firstSnapshot.Includes[0].Exists {
		t.Fatalf("missing include snapshot = %#v", firstSnapshot.Includes)
	}
	if err := os.WriteFile(includePath, []byte(`<% Const SharedValue = 1 %>`), 0o644); err != nil {
		t.Fatal(err)
	}
	first.invalidateFsPath(includePath)
	refreshed := first.ensureFileAnalysisSnapshot(doc, firstParsed, "VBScript")
	if refreshed == firstSnapshot || len(refreshed.Includes) != 1 || !refreshed.Includes[0].Exists {
		t.Fatalf("in-memory include resolution change did not rebuild snapshot: %#v", refreshed.Includes)
	}
	first.waitForAsyncDiskCacheWrites()
	first.closeDiskAnalysisCache()
	second := newServer()
	defer second.closeDiskAnalysisCache()
	secondParsed := second.parseTextDocument(doc, "VBScript")
	second.waitForAsyncDiskCacheWrites()
	secondSnapshot := second.cachedFileAnalysisSnapshot(secondParsed)
	if len(secondSnapshot.Includes) != 1 || !secondSnapshot.Includes[0].Exists || filepath.Clean(secondSnapshot.Includes[0].ResolvedPath) != filepath.Clean(includePath) {
		t.Fatalf("include resolution change did not rebuild snapshot: %#v", secondSnapshot.Includes)
	}
}

func snapshotSymbolFact(t *testing.T, snapshot fileAnalysisSnapshot, name, scope string) symbolAnalysisFact {
	t.Helper()
	matches := []symbolAnalysisFact{}
	for _, fact := range snapshot.SymbolFacts {
		if fact.NormalizedName == name && strings.EqualFold(fact.Scope, scope) {
			matches = append(matches, fact)
		}
	}
	if len(matches) != 1 {
		t.Fatalf("symbol facts for %s scope %s = %#v, want exactly one", name, scope, matches)
	}
	return matches[0]
}

func TestRuntimeCacheConfigurationDecodesOperationalLimits(t *testing.T) {
	server := New(strings.NewReader(""), io.Discard, io.Discard)
	cacheDir := t.TempDir()
	params := mustRaw(map[string]any{"settings": map[string]any{"aspLsp": map[string]any{
		"cache": map[string]any{
			"enabled":   true,
			"directory": cacheDir,
			"freshness": "watch",
			"ttlHours":  9,
			"maxSizeMb": 17,
			"gzip":      true,
		},
		"memory": map[string]any{"maxCacheBytes": 4096, "debugTelemetry": true},
		"network": map[string]any{
			"profile":                "network",
			"statCacheTtlMs":         123,
			"readdirCacheTtlMs":      456,
			"includeReadConcurrency": 7,
			"caseResolution":         "fast",
		},
		"workspace": map[string]any{"scanChunkSize": 17, "busyAnalysisConcurrency": 3},
	}}})
	if err := server.handleNotification(t.Context(), "workspace/didChangeConfiguration", params); err != nil {
		t.Fatal(err)
	}
	if !server.settings.CacheEnabled || server.settings.CacheTTLHours != 9 || server.settings.CacheMaxSizeMB != 17 || !server.settings.CacheGzip ||
		server.settings.MemoryMaxCacheBytes != 4096 || !server.settings.MemoryDebugTelemetry ||
		server.settings.NetworkProfile != "network" || server.settings.NetworkStatCacheTTLMS != 123 ||
		server.settings.NetworkReadDirCacheTTLMS != 456 || server.settings.NetworkIncludeReadConcurrency != 7 ||
		server.settings.NetworkCaseResolution != "fast" {
		t.Fatalf("runtime settings were not decoded: %#v", server.settings)
	}
	if server.settings.WorkspaceScanChunkSize != 17 || server.settings.WorkspaceBusyAnalysisConcurrency != 3 || server.analysisWorkers.workerCount() != 3 {
		t.Fatalf("workspace analysis settings were not decoded: chunk=%d concurrency=%d workers=%d", server.settings.WorkspaceScanChunkSize, server.settings.WorkspaceBusyAnalysisConcurrency, server.analysisWorkers.workerCount())
	}
	if got := server.diskCacheForUse().Directory(); got != cacheDir {
		t.Fatalf("disk cache directory = %q, want %q", got, cacheDir)
	}
}

func TestDefaultServerSettingsUseSixteenGiBDiskCacheTarget(t *testing.T) {
	settings := defaultServerSettings()
	if settings.CacheMaxSizeMB != 16384 {
		t.Fatalf("default disk cache target size = %d MiB, want 16384 MiB", settings.CacheMaxSizeMB)
	}
}

func TestRuntimeMemoryBudgetEvictsRegisteredServerCaches(t *testing.T) {
	server := New(strings.NewReader(""), io.Discard, io.Discard)
	server.settings.MemoryMaxCacheBytes = 1
	server.parsedCache["doc:file:///site/default.asp"] = parsedDocumentCacheEntry{
		Text:   strings.Repeat("x", 4096),
		Parsed: core.ParseDocument("file:///site/default.asp", `<% Response.Write "ok" %>`, core.Settings{}),
	}
	result := server.checkMemoryPressure("test")
	if result.RequestedBytes == 0 || result.EvictedBytes == 0 || len(server.parsedCache) != 0 {
		t.Fatalf("memory pressure did not evict parsed cache: result=%#v entries=%d", result, len(server.parsedCache))
	}
}

func TestRuntimeMemoryBudgetDemotesDocumentStoreBeforeDroppingParsedCache(t *testing.T) {
	server := New(strings.NewReader(""), io.Discard, io.Discard)
	doc := core.NewTextDocument("file:///site/demote.asp", "classic-asp", 1, `<% Response.Write "ok" %>`)
	parsed := core.ParseDocument(doc.URI, doc.Text, core.Settings{DefaultLanguage: "VBScript"})
	server.mu.Lock()
	server.rememberOpenDocumentLocked(doc.URI, doc)
	server.parsedCache[parsedDocumentCacheKey(doc.URI)] = parsedDocumentCacheEntry{
		Version: doc.Version,
		Text:    doc.Text,
		Parsed:  parsed,
	}
	server.rememberParsedDocumentLocked(doc, parsed, "VBScript")
	server.settings.MemoryMaxCacheBytes = 1
	server.mu.Unlock()

	result := server.checkMemoryPressure("test.documentStore")
	if result.EvictedBytes == 0 {
		t.Fatalf("memory pressure did not evict parsed cache: %#v", result)
	}
	server.mu.Lock()
	cached := server.documentStore.Cache[doc.URI]
	_, parsedStillCached := server.parsedCache[parsedDocumentCacheKey(doc.URI)]
	server.mu.Unlock()
	if parsedStillCached || cached == nil || cached.ParseDepth != "skeleton" || cached.Analysis != nil {
		t.Fatalf("document store was not demoted: cached=%#v parsedStillCached=%v", cached, parsedStillCached)
	}
}

func TestRegisteredParsedCacheEvictionPreservesOriginalURIIdentity(t *testing.T) {
	for _, test := range []struct {
		name string
		uri  string
	}{
		{name: "windows drive", uri: "file:///C:/site/default.asp"},
		{name: "POSIX root", uri: "file:///site/default.asp"},
		{name: "UNC share", uri: "file://server/share/default.asp"},
	} {
		t.Run(test.name, func(t *testing.T) {
			server := New(nil, io.Discard, io.Discard)
			defer server.shutdownRuntimeCaches()
			parsed := core.ParseDocument(test.uri, `<% Dim value %>`, core.Settings{DefaultLanguage: "VBScript"})
			key := parsedDocumentCacheKey(test.uri)
			server.mu.Lock()
			server.parsedCache[key] = parsedDocumentCacheEntry{Version: 1, Text: parsed.Text, DefaultLanguage: "VBScript", Parsed: parsed}
			server.mu.Unlock()

			if freed := server.registeredParsedCache().Evict(0); freed == 0 {
				t.Fatal("parsed cache eviction did not report freed bytes")
			}
			server.mu.Lock()
			cached := server.documentStore.Cache[test.uri]
			server.mu.Unlock()
			if cached == nil || cached.URI != test.uri {
				t.Fatalf("demoted document URI = %#v, want %q", cached, test.uri)
			}
		})
	}
}

func TestRegisteredParsedCacheEstimateMatchesEviction(t *testing.T) {
	server := New(nil, io.Discard, io.Discard)
	defer server.shutdownRuntimeCaches()
	parsed := core.ParseDocument("file:///site/parsed-accounting.asp", strings.Repeat("<% Dim value %>\n", 128), core.Settings{DefaultLanguage: "VBScript"})
	parsed.StoreAnalysis("test.persisted", strings.Repeat("analysis", 256))
	parsed.StoreRuntimeAnalysis("test.runtime", strings.Repeat("runtime", 256))
	key := parsedDocumentCacheKey(parsed.URI)
	entry := parsedDocumentCacheEntry{Version: 1, Text: parsed.Text, DefaultLanguage: "VBScript", Parsed: parsed}
	server.mu.Lock()
	server.parsedCache[key] = entry
	server.mu.Unlock()

	cache := server.registeredParsedCache()
	want := estimateParsedDocumentCacheEntryBytes(key, entry)
	if got := cache.EstimateBytes(); got != want || cache.EntryCount() != 1 {
		t.Fatalf("parsed cache estimate = %d bytes, %d entries; want %d bytes, 1 entry", got, cache.EntryCount(), want)
	}
	if want <= int64(len(key)+len(entry.Text))*2+1024 {
		t.Fatalf("parsed cache estimate = %d, want materialized analysis above legacy fixed estimate", want)
	}
	storeCache := server.registeredDocumentStoreCache()
	beforeTotal := addRuntimeCacheBytes(want, storeCache.EstimateBytes())
	freed := cache.Evict(0)
	server.mu.Lock()
	cached := server.documentStore.Cache[parsed.URI]
	server.mu.Unlock()
	if cached == nil {
		t.Fatal("demotion did not retain a document-store entry")
	}
	skeleton, ok := cached.Parsed.(*core.ParsedDocument)
	if !ok || skeleton == nil {
		t.Fatalf("demotion did not retain a parsed skeleton: %#v", cached)
	}
	afterTotal := addRuntimeCacheBytes(cache.EstimateBytes(), storeCache.EstimateBytes())
	wantFreed := subtractRuntimeCacheBytes(beforeTotal, afterTotal)
	if freed != wantFreed {
		t.Fatalf("parsed cache eviction freed %d bytes; want %d after retaining skeleton=%d", freed, wantFreed, skeleton.EstimateBytes())
	}
	if got := cache.EstimateBytes(); got != 0 || cache.EntryCount() != 0 {
		t.Fatalf("evicted parsed cache = %d bytes, %d entries; want empty", got, cache.EntryCount())
	}
}

func TestRegisteredParsedCacheDeduplicatesSharedParsedOwnerDuringEviction(t *testing.T) {
	server := New(nil, io.Discard, io.Discard)
	defer server.shutdownRuntimeCaches()
	parsed := core.ParseDocument("file:///site/shared-parsed-owner.asp", strings.Repeat("<% Dim value %>\n", 128), core.Settings{DefaultLanguage: "VBScript"})
	entry := parsedDocumentCacheEntry{Version: 1, Text: parsed.Text, DefaultLanguage: "VBScript", Parsed: parsed}
	firstKey := parsedDocumentCacheKey(parsed.URI)
	secondKey := parsedTextCacheKey(parsed.URI)
	server.mu.Lock()
	server.parsedCache[firstKey] = entry
	server.parsedCache[secondKey] = entry
	server.mu.Unlock()

	cache := server.registeredParsedCache()
	wantBefore := estimateParsedDocumentCacheEntryBytes(firstKey, entry) + estimateParsedDocumentCacheEntryBytes(secondKey, entry) - parsed.EstimateBytes()
	if got := cache.EstimateBytes(); got != wantBefore {
		t.Fatalf("shared parsed cache estimate = %d, want %d with one parsed owner", got, wantBefore)
	}
	storeCache := server.registeredDocumentStoreCache()
	beforeTotal := addRuntimeCacheBytes(cache.EstimateBytes(), storeCache.EstimateBytes())
	freed := cache.Evict(0)
	server.mu.Lock()
	cached := server.documentStore.Cache[parsed.URI]
	server.mu.Unlock()
	if cached == nil {
		t.Fatal("shared parsed eviction did not retain a document-store skeleton")
	}
	skeleton, ok := cached.Parsed.(*core.ParsedDocument)
	if !ok || skeleton == nil {
		t.Fatalf("shared parsed eviction retained %#v, want skeleton", cached.Parsed)
	}
	afterTotal := addRuntimeCacheBytes(cache.EstimateBytes(), storeCache.EstimateBytes())
	wantFreed := subtractRuntimeCacheBytes(beforeTotal, afterTotal)
	if freed != wantFreed {
		t.Fatalf("shared parsed eviction freed %d, want %d after retaining one skeleton=%d", freed, wantFreed, skeleton.EstimateBytes())
	}
}

func TestRegisteredParsedCacheEvictionPreservesExistingDocumentStoreOwner(t *testing.T) {
	server := New(nil, io.Discard, io.Discard)
	defer server.shutdownRuntimeCaches()
	uri := "file:///site/existing-store-owner.asp"
	text := strings.Repeat("<% Dim value %>\n", 64)
	parsed := core.ParseDocument(uri, text, core.Settings{DefaultLanguage: "VBScript"})
	key := parsedDocumentCacheKey(uri)
	entry := parsedDocumentCacheEntry{Version: 1, Text: text, DefaultLanguage: "VBScript", Parsed: parsed}
	server.mu.Lock()
	server.documentStore.Cache[uri] = &workspacepkg.CachedDocument{URI: uri, Text: text, Parsed: parsed, ParseDepth: "skeleton"}
	server.parsedCache[key] = entry
	server.mu.Unlock()

	cache := server.registeredParsedCache()
	before := cache.EstimateBytes()
	storeCache := server.registeredDocumentStoreCache()
	beforeTotal := addRuntimeCacheBytes(before, storeCache.EstimateBytes())
	freed := cache.Evict(0)
	afterTotal := addRuntimeCacheBytes(cache.EstimateBytes(), storeCache.EstimateBytes())
	wantFreed := subtractRuntimeCacheBytes(beforeTotal, afterTotal)
	if freed != wantFreed || freed >= before {
		t.Fatalf("existing document-store owner eviction freed %d, want global delta %d below parsed estimate %d", freed, wantFreed, before)
	}
	server.mu.Lock()
	_, cached := server.documentStore.Cache[uri]
	server.mu.Unlock()
	if !cached {
		t.Fatal("existing document-store owner was removed")
	}
}

func TestRegisteredDocumentStoreAccountsDemotedSkeleton(t *testing.T) {
	server := New(nil, io.Discard, io.Discard)
	defer server.shutdownRuntimeCaches()
	uri := "file:///site/document-store-skeleton.asp"
	text := strings.Repeat("<% Dim value %>\n", 128)
	parsed := core.ParseDocument(uri, text, core.Settings{DefaultLanguage: "VBScript"})
	key := parsedDocumentCacheKey(uri)
	server.mu.Lock()
	server.parsedCache[key] = parsedDocumentCacheEntry{Version: 1, Text: text, Parsed: parsed}
	server.rememberParsedDocumentLocked(core.NewTextDocument(uri, "classic-asp", 1, text), parsed, "VBScript")
	server.mu.Unlock()

	parsedCache := server.registeredParsedCache()
	if parsedCache.EstimateBytes() == 0 {
		t.Fatal("parsed cache did not account for the full parsed revision")
	}
	if freed := parsedCache.Evict(0); freed == 0 {
		t.Fatal("parsed cache demotion freed no bytes")
	}
	storeCache := server.registeredDocumentStoreCache()
	if got := storeCache.EntryCount(); got != 1 {
		t.Fatalf("document-store entries = %d, want one retained skeleton", got)
	}
	storeBytes := storeCache.EstimateBytes()
	if storeBytes <= 0 {
		t.Fatalf("document-store estimate = %d, want retained source/skeleton bytes", storeBytes)
	}
	if got := parsedCache.EstimateBytes(); got != 0 {
		t.Fatalf("parsed cache estimate after demotion = %d, want zero", got)
	}
	snapshot := server.memoryBudget.Snapshot(1)
	var registeredBytes int64
	for _, cache := range snapshot.Caches {
		if cache.Name == "documentStore" {
			registeredBytes = cache.EstimatedBytes
			break
		}
	}
	if registeredBytes != storeBytes || snapshot.TotalEstimatedBytes < storeBytes {
		t.Fatalf("memory snapshot omitted retained document-store ownership: store=%d registered=%d total=%d", storeBytes, registeredBytes, snapshot.TotalEstimatedBytes)
	}
}

func TestRegisteredDocumentStoreEvictionReportsExactFreedBytes(t *testing.T) {
	server := New(nil, io.Discard, io.Discard)
	defer server.shutdownRuntimeCaches()
	uri := "file:///site/document-store-eviction.asp"
	text := strings.Repeat("<% Dim value %>\n", 128)
	parsed := core.ParseDocument(uri, text, core.Settings{DefaultLanguage: "VBScript"})
	server.mu.Lock()
	server.documentStore.Cache[uri] = &workspacepkg.CachedDocument{
		URI: uri, Text: text, Parsed: parsed, ParseDepth: "skeleton", Virtuals: map[string]any{"html": strings.Repeat("x", 1024)},
		Analysis: "analysis", CSSContext: map[string]any{"css": strings.Repeat("x", 512)},
	}
	server.mu.Unlock()

	cache := server.registeredDocumentStoreCache()
	before := cache.EstimateBytes()
	if before <= 0 || cache.EntryCount() != 1 {
		t.Fatalf("document-store before eviction = %d bytes, %d entries", before, cache.EntryCount())
	}
	freed := cache.Evict(0)
	if freed != before {
		t.Fatalf("document-store eviction freed %d bytes; want exact before estimate %d", freed, before)
	}
	server.mu.Lock()
	remaining := len(server.documentStore.Cache)
	server.mu.Unlock()
	if remaining != 0 || cache.EstimateBytes() != 0 || cache.EntryCount() != 0 {
		t.Fatalf("document-store retained entries after eviction: map=%d bytes=%d entries=%d", remaining, cache.EstimateBytes(), cache.EntryCount())
	}
}

func TestDocumentStoreOwnerPrecedesAnalysisForSharedParsedRevision(t *testing.T) {
	server := New(nil, io.Discard, io.Discard)
	defer server.shutdownRuntimeCaches()
	uri := "file:///site/document-store-shared-owner.asp"
	text := strings.Repeat("<% Dim value %>\n", 64)
	parsed := core.ParseDocument(uri, text, core.Settings{DefaultLanguage: "VBScript"})
	key := parsedDocumentCacheKey(uri)
	server.mu.Lock()
	server.parsedCache[key] = parsedDocumentCacheEntry{Version: 1, Text: text, Parsed: parsed}
	server.documentStore.Cache[uri] = &workspacepkg.CachedDocument{URI: uri, Text: text, Parsed: parsed, ParseDepth: "skeleton"}
	server.mu.Unlock()
	server.analysisCache.rememberSnapshot(parsed, &fileAnalysisSnapshot{URI: uri, runtimeBacked: true})

	parsedOwnership := parsedDocumentCacheOwnershipForEntries(server.parsedCache)
	storeOwnership := documentStoreCacheOwnershipForStore(server.documentStore, parsedOwnership)
	if len(storeOwnership.parsed) != 0 || len(storeOwnership.source) != 0 || len(storeOwnership.runtime) != 0 {
		t.Fatalf("document-store duplicated parsed-cache owners: parsed=%d source=%d runtime=%d", len(storeOwnership.parsed), len(storeOwnership.source), len(storeOwnership.runtime))
	}
	analysisCache := server.registeredAnalysisSnapshotCache()
	wantAnalysis := estimateAnalysisCacheFileSnapshotBytes(parsed, &fileAnalysisSnapshot{URI: uri, runtimeBacked: true}) + estimateAnalysisCacheDeclarationsBytes(nil)
	if got := analysisCache.EstimateBytes(); got != wantAnalysis {
		t.Fatalf("analysis estimate = %d, want snapshot-only ownership %d", got, wantAnalysis)
	}

	server.mu.Lock()
	delete(server.parsedCache, key)
	server.mu.Unlock()
	storeAfterParsedDrop := server.registeredDocumentStoreCache().EstimateBytes()
	if storeAfterParsedDrop <= storeOwnership.bytes() {
		t.Fatalf("document-store did not assume parsed ownership after parsed-cache drop: before=%d after=%d", storeOwnership.bytes(), storeAfterParsedDrop)
	}
	if got := analysisCache.EstimateBytes(); got != wantAnalysis {
		t.Fatalf("analysis estimate changed while document-store retained parsed owner: got=%d want=%d", got, wantAnalysis)
	}

	beforeTotal := addRuntimeCacheBytes(server.registeredDocumentStoreCache().EstimateBytes(), analysisCache.EstimateBytes())
	freed := server.registeredDocumentStoreCache().Evict(0)
	afterTotal := addRuntimeCacheBytes(server.registeredDocumentStoreCache().EstimateBytes(), analysisCache.EstimateBytes())
	if freed != subtractRuntimeCacheBytes(beforeTotal, afterTotal) {
		t.Fatalf("document-store eviction reported %d, global registered delta is %d", freed, subtractRuntimeCacheBytes(beforeTotal, afterTotal))
	}
	if server.registeredDocumentStoreCache().EntryCount() != 0 {
		t.Fatal("document-store owner remained after eviction")
	}
}

func TestParsedCacheEvictionReportsClearedAnalysisBytes(t *testing.T) {
	server := New(nil, io.Discard, io.Discard)
	defer server.shutdownRuntimeCaches()
	uri := "file:///site/parsed-analysis-eviction.asp"
	text := strings.Repeat("<% Dim value %>\n", 64)
	parsed := core.ParseDocument(uri, text, core.Settings{DefaultLanguage: "VBScript"})
	key := parsedDocumentCacheKey(uri)
	server.mu.Lock()
	server.parsedCache[key] = parsedDocumentCacheEntry{Version: 1, Text: text, Parsed: parsed}
	server.rememberParsedDocumentLocked(core.NewTextDocument(uri, "classic-asp", 1, text), parsed, "VBScript")
	server.mu.Unlock()
	declarations := []vbUsageDeclaration{{Name: "value", Kind: "variable"}}
	server.analysisCache.rememberSnapshot(parsed, &fileAnalysisSnapshot{URI: uri, GraphDeclarations: declarations, runtimeBacked: true})

	parsedCache := server.registeredParsedCache()
	storeCache := server.registeredDocumentStoreCache()
	analysisCache := server.registeredAnalysisSnapshotCache()
	before := addRuntimeCacheBytes(addRuntimeCacheBytes(parsedCache.EstimateBytes(), storeCache.EstimateBytes()), analysisCache.EstimateBytes())
	freed := parsedCache.Evict(0)
	after := addRuntimeCacheBytes(addRuntimeCacheBytes(parsedCache.EstimateBytes(), storeCache.EstimateBytes()), analysisCache.EstimateBytes())
	if freed != subtractRuntimeCacheBytes(before, after) {
		t.Fatalf("parsed eviction reported %d, global registered delta is %d", freed, subtractRuntimeCacheBytes(before, after))
	}
	if analysisCache.EstimateBytes() != 0 {
		t.Fatalf("analysis cache remained after parsed eviction: %d bytes", analysisCache.EstimateBytes())
	}
}

func TestRepeatedParsedCacheDemotionsKeepOneDocumentStoreEntryPerURI(t *testing.T) {
	server := New(nil, io.Discard, io.Discard)
	defer server.shutdownRuntimeCaches()
	uri := "file:///site/repeated-demotion.asp"
	for version := 1; version <= 32; version++ {
		text := strings.Repeat("<% Dim value %>\n", version)
		parsed := core.ParseDocument(uri, text, core.Settings{DefaultLanguage: "VBScript"})
		server.mu.Lock()
		server.parsedCache[parsedDocumentCacheKey(uri)] = parsedDocumentCacheEntry{Version: version, Text: text, Parsed: parsed}
		server.mu.Unlock()
		server.registeredParsedCache().Evict(0)
		server.mu.Lock()
		entries := len(server.documentStore.Cache)
		server.mu.Unlock()
		if entries != 1 {
			t.Fatalf("after demotion %d, document-store entries = %d, want one", version, entries)
		}
	}
	if got := server.registeredDocumentStoreCache().EntryCount(); got != 1 {
		t.Fatalf("final document-store entry count = %d, want one", got)
	}
}

func TestParsedCacheEstimateCountsEqualTextWithDistinctBacking(t *testing.T) {
	text := strings.Repeat("<% Dim value %>\n", 128)
	parsed := core.ParseDocument("file:///site/distinct-text.asp", text, core.Settings{DefaultLanguage: "VBScript"})
	entry := parsedDocumentCacheEntry{Text: strings.Clone(parsed.Text), Parsed: parsed}
	bytes := estimateParsedDocumentCacheEntryBytes(parsedDocumentCacheKey(parsed.URI), entry)
	minimum := parsed.EstimateBytes() + int64(len(entry.Text))*2
	if bytes < minimum {
		t.Fatalf("distinct equal text estimate = %d, want at least %d", bytes, minimum)
	}
}

func TestRegisteredAnalysisCacheOwnsRevisionsOutsideParsedCache(t *testing.T) {
	server := New(nil, io.Discard, io.Discard)
	defer server.shutdownRuntimeCaches()
	uri := "file:///site/replaced.asp"
	old := core.ParseDocument(uri, strings.Repeat("<% Dim oldValue %>\n", 64), core.Settings{DefaultLanguage: "VBScript"})
	current := core.ParseDocument(uri, "<% Dim currentValue %>", core.Settings{DefaultLanguage: "VBScript"})
	oldSnapshot := &workspaceArtifactSnapshot{URI: uri, VirtualDocuments: map[core.EmbeddedLanguage]core.VirtualDocument{}}
	currentSnapshot := &workspaceArtifactSnapshot{URI: uri, VirtualDocuments: map[core.EmbeddedLanguage]core.VirtualDocument{}}
	server.analysisCache.rememberWorkspaceSnapshot(old, "old", oldSnapshot)
	server.analysisCache.rememberWorkspaceSnapshot(current, "current", currentSnapshot)
	server.mu.Lock()
	server.parsedCache[parsedDocumentCacheKey(uri)] = parsedDocumentCacheEntry{Text: current.Text, Parsed: current}
	server.mu.Unlock()

	cache := server.registeredAnalysisSnapshotCache()
	want := estimateAnalysisCacheWorkspaceSnapshotBytes(old, oldSnapshot) + old.EstimateBytes() +
		estimateAnalysisCacheWorkspaceSnapshotBytes(current, currentSnapshot)
	if got := cache.EstimateBytes(); got != want {
		t.Fatalf("registered analysis estimate = %d, want %d", got, want)
	}
	if freed := cache.Evict(0); freed != want {
		t.Fatalf("registered analysis eviction = %d, want %d", freed, want)
	}
}

func TestFiniteRuntimeMemoryLimitRejectsUnlimitedSentinel(t *testing.T) {
	if got := finiteRuntimeMemoryLimit(math.MaxInt64); got != 0 {
		t.Fatalf("unlimited runtime memory limit = %d, want 0", got)
	}
	if got := finiteRuntimeMemoryLimit(-1); got != 0 {
		t.Fatalf("negative runtime memory limit = %d, want 0", got)
	}
	if got := finiteRuntimeMemoryLimit(256 * 1024 * 1024); got != 256*1024*1024 {
		t.Fatalf("finite runtime memory limit = %d", got)
	}
}

func TestRegisteredAnalysisSnapshotCacheEvictsSnapshotBeforeDeclarations(t *testing.T) {
	server := New(strings.NewReader(""), io.Discard, io.Discard)
	parsed := core.ParseDocument("file:///site/analysis.asp", `<% Dim value %>`, core.Settings{})
	declarations := []vbUsageDeclaration{{Name: "value", Kind: "variable"}}
	snapshot := &fileAnalysisSnapshot{
		URI:               parsed.URI,
		GraphDeclarations: declarations,
		VirtualDocuments: map[core.EmbeddedLanguage]core.VirtualDocument{
			core.LanguageHTML: {URI: parsed.URI + ".html", Text: strings.Repeat("x", 4096)},
		},
	}
	server.analysisCache.rememberSnapshot(parsed, snapshot)
	cache := server.registeredAnalysisSnapshotCache()
	before := cache.EstimateBytes()
	if before <= 4096 || cache.EntryCount() != 2 {
		t.Fatalf("analysis cache estimate = %d, entries=%d", before, cache.EntryCount())
	}
	if freed := cache.Evict(1); freed <= 0 {
		t.Fatal("analysis snapshot eviction freed no bytes")
	}
	if server.cachedFileAnalysisSnapshot(parsed) != nil {
		t.Fatal("analysis snapshot remained cached")
	}
	if got := server.cachedVBDeclarations(parsed); len(got) != 1 || got[0].Name != "value" {
		t.Fatalf("hot declarations were not retained: %#v", got)
	}
}

func TestRegisteredSourceSnapshotCacheEvictsDecodedTextButRetainsOneReadRawSource(t *testing.T) {
	server := New(strings.NewReader(""), io.Discard, io.Discard)
	openPath := "/site/open.asp"
	closedPath := "/site/closed.asp"
	server.documents[filePathURI(openPath)] = core.NewTextDocument(filePathURI(openPath), "classic-asp", 1, "open")
	server.sourceSnapshots[sourceSnapshotKey(openPath)] = &sourceFileSnapshot{path: openPath, raw: []byte("open"), decoded: map[string]string{"auto": "open"}, loadedAt: time.Now().Add(-time.Hour), valid: true}
	server.sourceSnapshots[sourceSnapshotKey(closedPath)] = &sourceFileSnapshot{path: closedPath, raw: []byte(strings.Repeat("x", 1024)), decoded: map[string]string{"auto": strings.Repeat("x", 1024)}, loadedAt: time.Now(), valid: true}
	cache := server.registeredSourceSnapshotCache()
	if cache.EstimateBytes() <= 1024 {
		t.Fatalf("source snapshot estimate = %d", cache.EstimateBytes())
	}
	if freed := cache.Evict(1); freed <= 0 {
		t.Fatal("source snapshot eviction freed no bytes")
	}
	closed := server.sourceSnapshots[sourceSnapshotKey(closedPath)]
	if closed == nil || len(closed.raw) != 1024 {
		t.Fatal("closed raw source was evicted and could be read from disk again")
	}
	if len(closed.decoded) != 0 {
		t.Fatal("closed decoded source remained cached")
	}
	if open := server.sourceSnapshots[sourceSnapshotKey(openPath)]; open == nil || len(open.decoded) == 0 {
		t.Fatal("open decoded source was evicted before a closed file")
	}
}

func TestRegisteredSemanticCacheAccountsForAndEvictsHistory(t *testing.T) {
	server := New(strings.NewReader(""), io.Discard, io.Discard)
	uri := "file:///site/semantic.asp"
	server.semantic[uri] = semanticTokenCache{Version: 1, Tokens: lsp.SemanticTokens{ResultID: uri + "#1", Data: []int{1, 2, 3}}}
	server.semanticHistory[uri+"#1"] = []int{1, 2, 3}
	cache := server.registeredSemanticCache()
	if cache.EntryCount() != 2 {
		t.Fatalf("semantic cache entries = %d, want token and history", cache.EntryCount())
	}
	if freed := cache.Evict(1); freed <= 0 {
		t.Fatal("semantic cache eviction freed no bytes")
	}
	if len(server.semantic) != 0 || len(server.semanticHistory) != 0 {
		t.Fatalf("semantic cache retained data: tokens=%d history=%d", len(server.semantic), len(server.semanticHistory))
	}
}

func TestMemoryPressureSchedulingCoalescesHotPathInsertions(t *testing.T) {
	server := New(strings.NewReader(""), io.Discard, io.Discard)
	server.mu.Lock()
	server.scheduleMemoryPressureCheckLocked("graphPayload.store")
	first := server.memoryPressureTimer
	server.scheduleMemoryPressureCheckLocked("semanticTokens.store")
	second := server.memoryPressureTimer
	server.memoryPressureTimer.Stop()
	server.memoryPressureTimer = nil
	server.mu.Unlock()
	if first == nil || first != second {
		t.Fatal("memory pressure checks were not coalesced")
	}
}

func TestRuntimeWorkspaceIndexCacheRestoresAndRejectsChangedMetadata(t *testing.T) {
	root := t.TempDir()
	cacheDir := filepath.Join(root, ".cache")
	fileName := filepath.Join(root, "default.asp")
	if err := os.WriteFile(fileName, []byte(`<% Function CachedName() %>`), 0o644); err != nil {
		t.Fatal(err)
	}
	newServer := func() *Server {
		server := New(strings.NewReader(""), io.Discard, io.Discard)
		server.rootPath = root
		server.rootURI = filePathURI(root)
		server.workspaceRoots = []workspaceRoot{{Path: root, URI: filePathURI(root)}}
		server.settings.CacheEnabled = true
		server.settings.CacheDirectory = cacheDir
		server.settings.CacheFreshness = "metadata"
		server.configureDiskAnalysisCache()
		return server
	}
	first := newServer()
	first.indexWorkspace()
	if len(first.workspace) != 1 {
		t.Fatalf("first index workspace entries = %d, want 1", len(first.workspace))
	}
	first.waitForAsyncDiskCacheWrites()
	first.closeDiskAnalysisCache()
	second := newServer()
	second.indexWorkspace()
	if len(second.workspace) != 1 || second.workspace[filePathURI(fileName)] == nil {
		t.Fatalf("restored workspace index = %#v, want %s", second.workspace, filePathURI(fileName))
	}
	second.waitForAsyncDiskCacheWrites()
	second.closeDiskAnalysisCache()
	if err := os.WriteFile(fileName, []byte(`<% Function ChangedNameLonger() %>`), 0o644); err != nil {
		t.Fatal(err)
	}
	third := newServer()
	defer third.closeDiskAnalysisCache()
	third.indexWorkspace()
	if got := third.workspace[filePathURI(fileName)]; got == nil || !strings.Contains(got.Text, "ChangedNameLonger") {
		t.Fatalf("changed workspace file was not reindexed: %#v", got)
	}
}

func TestReferenceCacheEvictionAccountsForDerivedNameIndex(t *testing.T) {
	server := New(strings.NewReader(""), io.Discard, io.Discard)
	key := workspaceReferenceTargetKey{URI: "file:///workspace/default.asp", Name: "SharedValue", SymbolKind: "variable"}
	server.referenceCounts[key] = 1
	server.mu.Lock()
	server.ensureWorkspaceReferenceNameIndexLocked()
	nameIndexBytes, nameIndexEntries := server.estimateWorkspaceReferenceNameIndexLocked()
	server.mu.Unlock()
	if nameIndexBytes <= 0 || nameIndexEntries != 1 {
		t.Fatalf("name index estimate = %d bytes, %d entries", nameIndexBytes, nameIndexEntries)
	}

	freed := server.registeredReferenceCache().Evict(1)
	if freed < nameIndexBytes {
		t.Fatalf("reported freed bytes = %d, want at least %d", freed, nameIndexBytes)
	}
	server.mu.Lock()
	ready := server.referenceNameIndexReady
	_, countPreserved := server.referenceCounts[key]
	server.mu.Unlock()
	if ready || !countPreserved {
		t.Fatalf("derived index ready = %t, source count preserved = %t", ready, countPreserved)
	}
}

func newRuntimeCacheTestServer(t *testing.T) *Server {
	t.Helper()
	server := New(strings.NewReader(""), io.Discard, io.Discard)
	cacheDir := filepath.Join(t.TempDir(), "cache")
	server.settings.CacheEnabled = true
	server.settings.CacheDirectory = cacheDir
	server.settings.CacheTTLHours = 24
	server.settings.CacheMaxSizeMB = 16
	server.rootPath = t.TempDir()
	server.configureDiskAnalysisCache()
	t.Cleanup(func() {
		server.closeDiskAnalysisCache()
		_ = os.RemoveAll(cacheDir)
	})
	return server
}
