package lspserver

import (
	"context"
	"io"
	"os"
	"path/filepath"
	"reflect"
	"strconv"
	"sync"
	"sync/atomic"
	"testing"
	"time"

	"github.com/yottonoko/classic-web-system-lsp/internal/core"
)

func TestScanJavaScriptProjectIdentityReadsOverlapWithParallelWorkers(t *testing.T) {
	root := t.TempDir()
	writeJavaScriptProjectIdentityFiles(t, root, 4)
	server := newJavaScriptProjectIdentityScanTestServer(t, root, 3)

	entered := make(chan string, 4)
	release := make(chan struct{})
	var releaseOnce sync.Once
	closeRelease := func() { releaseOnce.Do(func() { close(release) }) }
	var activeMu sync.Mutex
	active := 0
	maxActive := 0
	javascriptProjectIdentityTestHooks.Store(&javascriptProjectIdentityTestHook{
		readFile: func(path string) {
			activeMu.Lock()
			active++
			if active > maxActive {
				maxActive = active
			}
			activeMu.Unlock()
			entered <- path
			<-release
			activeMu.Lock()
			active--
			activeMu.Unlock()
		},
	})
	defer javascriptProjectIdentityTestHooks.Store(nil)

	done := make(chan javascriptProjectIdentityRecord, 1)
	go func() { done <- server.scanJavaScriptProjectIdentity([]string{root}) }()
	finished := false
	defer func() {
		closeRelease()
		if !finished {
			<-done
		}
	}()
	for range 3 {
		select {
		case <-entered:
		case <-time.After(time.Second):
			t.Fatal("parallel identity scan did not start three reads")
		}
	}
	activeMu.Lock()
	gotMaxActive := maxActive
	activeMu.Unlock()
	if gotMaxActive < 2 {
		t.Fatalf("parallel identity scan max concurrent reads = %d, want at least 2", gotMaxActive)
	}
	closeRelease()
	result := <-done
	finished = true
	if len(result.Files) != 4 {
		t.Fatalf("parallel identity scan read %d files, want 4", len(result.Files))
	}
}

func TestScanJavaScriptProjectIdentityReadsSequentiallyWithOneWorker(t *testing.T) {
	root := t.TempDir()
	writeJavaScriptProjectIdentityFiles(t, root, 2)
	server := newJavaScriptProjectIdentityScanTestServer(t, root, 1)

	entered := make(chan string, 2)
	release := make(chan struct{})
	var releaseOnce sync.Once
	closeRelease := func() { releaseOnce.Do(func() { close(release) }) }
	javascriptProjectIdentityTestHooks.Store(&javascriptProjectIdentityTestHook{
		readFile: func(path string) {
			entered <- path
			<-release
		},
	})
	defer javascriptProjectIdentityTestHooks.Store(nil)

	done := make(chan javascriptProjectIdentityRecord, 1)
	go func() { done <- server.scanJavaScriptProjectIdentity([]string{root}) }()
	finished := false
	defer func() {
		closeRelease()
		if !finished {
			<-done
		}
	}()
	select {
	case <-entered:
	case <-time.After(time.Second):
		t.Fatal("sequential identity scan did not start its first read")
	}
	select {
	case path := <-entered:
		closeRelease()
		<-done
		t.Fatalf("one-worker identity scan started a second read before the first finished: %q", path)
	default:
	}
	closeRelease()
	result := <-done
	finished = true
	if len(result.Files) != 2 {
		t.Fatalf("sequential identity scan read %d files, want 2", len(result.Files))
	}
}

func TestScanJavaScriptProjectIdentityFingerprintAndOutputStayDeterministic(t *testing.T) {
	root := t.TempDir()
	writeJavaScriptProjectIdentityFiles(t, root, 8)
	outside := filepath.Join(t.TempDir(), "outside.js")
	if err := os.WriteFile(outside, []byte("export const outside = true;"), 0o600); err != nil {
		t.Fatal(err)
	}
	link := filepath.Join(root, "linked.js")
	if err := os.Symlink(outside, link); err != nil {
		t.Skipf("cannot create symlink for unreadable-file coverage: %v", err)
	}

	serial := newJavaScriptProjectIdentityScanTestServer(t, root, 1)
	parallel := newJavaScriptProjectIdentityScanTestServer(t, root, 4)
	want := serial.scanJavaScriptProjectIdentity([]string{root})
	got := parallel.scanJavaScriptProjectIdentity([]string{root})
	if got.Fingerprint != want.Fingerprint {
		t.Fatalf("parallel identity fingerprint = %q, want serial fingerprint %q", got.Fingerprint, want.Fingerprint)
	}
	if !reflect.DeepEqual(got, want) {
		t.Fatalf("parallel identity record differs from serial record\n got: %#v\nwant: %#v", got, want)
	}
	for _, metadata := range got.Files {
		if filepath.Clean(metadata.FileName) == filepath.Clean(link) {
			t.Fatalf("unreadable symlink was included in identity record: %#v", metadata)
		}
	}
	for repeat := 0; repeat < 3; repeat++ {
		if repeated := parallel.scanJavaScriptProjectIdentity([]string{root}); !reflect.DeepEqual(repeated, got) {
			t.Fatalf("parallel identity scan changed on repeat %d\n got: %#v\nwant: %#v", repeat, repeated, got)
		}
	}
}

func TestScanJavaScriptProjectIdentitySkipsExcludedFilesBeforeReading(t *testing.T) {
	root := t.TempDir()
	excludedDir := filepath.Join(root, "ignored", "nested")
	if err := os.MkdirAll(excludedDir, 0o755); err != nil {
		t.Fatal(err)
	}
	includedPath := filepath.Join(root, "included.js")
	excludedPath := filepath.Join(excludedDir, "excluded.js")
	for _, path := range []string{includedPath, excludedPath} {
		if err := os.WriteFile(path, []byte("export const value = 1;\n"), 0o600); err != nil {
			t.Fatal(err)
		}
	}
	server := newJavaScriptProjectIdentityScanTestServer(t, root, 2)
	server.settings.WorkspaceExcludeGlobs = []string{"ignored/**"}
	var readMu sync.Mutex
	readPaths := []string{}
	javascriptProjectIdentityTestHooks.Store(&javascriptProjectIdentityTestHook{
		readFile: func(path string) {
			readMu.Lock()
			readPaths = append(readPaths, filepath.Clean(path))
			readMu.Unlock()
		},
	})
	defer javascriptProjectIdentityTestHooks.Store(nil)

	result := server.scanJavaScriptProjectIdentity([]string{root})
	if len(result.Files) != 1 || filepath.Clean(result.Files[0].FileName) != filepath.Clean(includedPath) {
		t.Fatalf("filtered identity files = %#v, want only %q", result.Files, includedPath)
	}
	readMu.Lock()
	defer readMu.Unlock()
	for _, path := range readPaths {
		if path == filepath.Clean(excludedPath) {
			t.Fatalf("excluded JavaScript file was read: %v", readPaths)
		}
	}
}

func TestScanJavaScriptProjectIdentityPrunesCacheDirectoryButKeepsEqualRoot(t *testing.T) {
	root := t.TempDir()
	cacheDir := filepath.Join(root, "cache")
	if err := os.MkdirAll(cacheDir, 0o755); err != nil {
		t.Fatal(err)
	}
	includedPath := filepath.Join(root, "included.js")
	cachedPath := filepath.Join(cacheDir, "cached.js")
	for _, path := range []string{includedPath, cachedPath} {
		if err := os.WriteFile(path, []byte("export const value = 1;\n"), 0o600); err != nil {
			t.Fatal(err)
		}
	}
	server := newJavaScriptProjectIdentityScanTestServer(t, root, 2)
	server.settings.CacheDirectory = cacheDir
	var readMu sync.Mutex
	readPaths := []string{}
	javascriptProjectIdentityTestHooks.Store(&javascriptProjectIdentityTestHook{
		readFile: func(path string) {
			readMu.Lock()
			readPaths = append(readPaths, filepath.Clean(path))
			readMu.Unlock()
		},
	})
	defer javascriptProjectIdentityTestHooks.Store(nil)

	result := server.scanJavaScriptProjectIdentity([]string{root})
	for _, metadata := range result.Files {
		if filepath.Clean(metadata.FileName) == filepath.Clean(cachedPath) {
			t.Fatalf("cache descendant entered identity record: %#v", result.Files)
		}
	}
	readMu.Lock()
	for _, path := range readPaths {
		if path == filepath.Clean(cachedPath) {
			readMu.Unlock()
			t.Fatalf("cache descendant was read: %v", readPaths)
		}
	}
	readMu.Unlock()

	equalRoot := newJavaScriptProjectIdentityScanTestServer(t, root, 1)
	equalRoot.settings.CacheDirectory = root
	result = equalRoot.scanJavaScriptProjectIdentity([]string{root})
	foundIncluded := false
	foundCached := false
	for _, metadata := range result.Files {
		switch filepath.Clean(metadata.FileName) {
		case filepath.Clean(includedPath):
			foundIncluded = true
		case filepath.Clean(cachedPath):
			foundCached = true
		}
	}
	if !foundIncluded || !foundCached {
		t.Fatalf("cache directory equal to root suppressed files: %#v", result.Files)
	}
}

func TestScanJavaScriptProjectIdentityPreservesGitIgnoreNegatedDescendant(t *testing.T) {
	root := t.TempDir()
	ignoredDir := filepath.Join(root, "ignored")
	if err := os.MkdirAll(ignoredDir, 0o755); err != nil {
		t.Fatal(err)
	}
	if err := os.WriteFile(filepath.Join(root, ".gitignore"), []byte("ignored/**\n!ignored/keep.js\n"), 0o600); err != nil {
		t.Fatal(err)
	}
	keepPath := filepath.Join(ignoredDir, "keep.js")
	dropPath := filepath.Join(ignoredDir, "drop.js")
	for _, path := range []string{keepPath, dropPath} {
		if err := os.WriteFile(path, []byte("export const value = 1;\n"), 0o600); err != nil {
			t.Fatal(err)
		}
	}
	server := newJavaScriptProjectIdentityScanTestServer(t, root, 2)
	server.settings.WorkspaceRespectGitIgnore = true
	var readMu sync.Mutex
	readPaths := []string{}
	javascriptProjectIdentityTestHooks.Store(&javascriptProjectIdentityTestHook{
		readFile: func(path string) {
			readMu.Lock()
			readPaths = append(readPaths, filepath.Clean(path))
			readMu.Unlock()
		},
	})
	defer javascriptProjectIdentityTestHooks.Store(nil)

	result := server.scanJavaScriptProjectIdentity([]string{root})
	foundKeep := false
	for _, metadata := range result.Files {
		cleaned := filepath.Clean(metadata.FileName)
		if cleaned == filepath.Clean(keepPath) {
			foundKeep = true
		}
		if cleaned == filepath.Clean(dropPath) {
			t.Fatalf("gitignored descendant entered identity record: %#v", result.Files)
		}
	}
	if !foundKeep {
		t.Fatalf("gitignore negation did not preserve descendant: %#v", result.Files)
	}
	readMu.Lock()
	defer readMu.Unlock()
	for _, path := range readPaths {
		if path == filepath.Clean(dropPath) {
			t.Fatalf("gitignored descendant was read: %v", readPaths)
		}
	}
}

func TestJavaScriptProjectIdentityNestedDiagnosticsScansDoNotDeadlock(t *testing.T) {
	root := t.TempDir()
	writeJavaScriptProjectIdentityFiles(t, root, 4)
	server := newJavaScriptProjectIdentityScanTestServer(t, root, 2)
	document := core.NewTextDocument(filePathURI(filepath.Join(root, "default.asp")), "classic-asp", 1, `<script>const value = 1;</script>`)
	parsed := core.ParseDocument(document.URI, document.Text, core.Settings{DefaultLanguage: "VBScript"})

	done := make(chan struct{})
	go func() {
		server.analysisWorkers.parallelForBulk(context.Background(), 2, func(workerCtx context.Context, _ int) {
			server.diagnosticsDiskLookupContext(workerCtx, document, parsed)
		})
		close(done)
	}()
	select {
	case <-done:
	case <-time.After(2 * time.Second):
		t.Fatal("nested JavaScript project identity scans deadlocked the analysis worker pool")
	}
}

func TestJavaScriptProjectFingerprintRetriesAfterFilesystemGenerationDrift(t *testing.T) {
	root := t.TempDir()
	writeJavaScriptProjectIdentityFiles(t, root, 4)
	server := newJavaScriptProjectIdentityScanTestServer(t, root, 2)

	var invalidated atomic.Bool
	var reads atomic.Int32
	javascriptProjectIdentityTestHooks.Store(&javascriptProjectIdentityTestHook{
		readFile: func(path string) {
			reads.Add(1)
			if invalidated.CompareAndSwap(false, true) {
				server.fsGateway.InvalidatePath(path)
			}
		},
	})
	defer javascriptProjectIdentityTestHooks.Store(nil)

	if fingerprint := server.javascriptProjectFingerprintContext(context.Background()); fingerprint == "" {
		t.Fatal("JavaScript project fingerprint is empty after generation retry")
	}
	if got := reads.Load(); got < 8 {
		t.Fatalf("JavaScript project identity reads = %d, want at least 8 across the stale scan and retry", got)
	}
	if got := reads.Load(); got > 12 {
		t.Fatalf("JavaScript project identity reads = %d, want no more than three bounded attempts", got)
	}
}

func TestJavaScriptProjectFingerprintCancellationStopsPersistedValidation(t *testing.T) {
	root := t.TempDir()
	cacheDir := t.TempDir()
	server := newJavaScriptProjectIdentityScanTestServer(t, root, 2)
	server.settings.CacheEnabled = true
	server.settings.CacheDirectory = cacheDir
	server.configureDiskAnalysisCache()
	defer func() {
		server.waitForAsyncDiskCacheWrites()
		server.closeDiskAnalysisCache()
	}()
	filePath := filepath.Join(root, "module.js")
	if err := os.WriteFile(filePath, []byte("export const value = 1;\n"), 0o600); err != nil {
		t.Fatal(err)
	}
	if fingerprint := server.javascriptProjectFingerprintContext(context.Background()); fingerprint == "" {
		t.Fatal("initial JavaScript project fingerprint is empty")
	}
	server.waitForAsyncDiskCacheWrites()
	ctx, cancel := context.WithCancel(context.Background())
	cancel()
	if fingerprint := server.javascriptProjectFingerprintContext(ctx); fingerprint != "" {
		t.Fatalf("cancelled persisted JavaScript identity validation returned %q", fingerprint)
	}
}

func BenchmarkScanJavaScriptProjectIdentityReads(b *testing.B) {
	root := b.TempDir()
	for index := 0; index < 128; index++ {
		path := filepath.Join(root, "file-"+strconv.Itoa(index)+".js")
		if err := os.WriteFile(path, []byte("export const value = 1;"), 0o600); err != nil {
			b.Fatal(err)
		}
	}
	for _, workers := range []int{1, 4} {
		b.Run("workers="+strconv.Itoa(workers), func(b *testing.B) {
			server := New(nil, io.Discard, io.Discard)
			b.Cleanup(server.shutdownRuntimeCaches)
			server.rootPath = root
			server.rootURI = filePathURI(root)
			server.workspaceRoots = []workspaceRoot{{Path: root, URI: server.rootURI}}
			server.configureFsGateway()
			server.analysisWorkers.setWorkers(workers)
			b.ReportAllocs()
			b.ResetTimer()
			for b.Loop() {
				server.mu.Lock()
				server.sourceSnapshots = map[string]*sourceFileSnapshot{}
				server.sourceReadInflight = map[string]*sourceReadInflight{}
				server.mu.Unlock()
				if result := server.scanJavaScriptProjectIdentity([]string{root}); result.Fingerprint == "" {
					b.Fatal("empty JavaScript project identity fingerprint")
				}
			}
		})
	}
}

func newJavaScriptProjectIdentityScanTestServer(t *testing.T, root string, workers int) *Server {
	t.Helper()
	server := New(nil, io.Discard, io.Discard)
	t.Cleanup(server.shutdownRuntimeCaches)
	server.rootPath = root
	server.rootURI = filePathURI(root)
	server.workspaceRoots = []workspaceRoot{{Path: root, URI: server.rootURI}}
	server.configureFsGateway()
	server.analysisWorkers.setWorkers(workers)
	return server
}

func writeJavaScriptProjectIdentityFiles(t *testing.T, root string, count int) {
	t.Helper()
	for index := 0; index < count; index++ {
		path := filepath.Join(root, "file-"+strconv.Itoa(index)+".js")
		if err := os.WriteFile(path, []byte("export const value = 1;"), 0o600); err != nil {
			t.Fatal(err)
		}
	}
}
