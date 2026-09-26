package lspserver

import (
	"bytes"
	"context"
	"errors"
	"fmt"
	"io"
	"os"
	"path/filepath"
	"runtime"
	"strconv"
	"strings"
	"sync"
	"sync/atomic"
	"syscall"
	"testing"
	"time"

	"github.com/yottonoko/classic-web-system-lsp/internal/core"
	workspacepkg "github.com/yottonoko/classic-web-system-lsp/internal/workspace"
)

func TestInitializeDefersWorkspaceIndexAndOpenDocumentsRemainUsable(t *testing.T) {
	root := t.TempDir()
	workspaceFile := filepath.Join(root, "workspace.asp")
	if err := os.WriteFile(workspaceFile, []byte("<% Dim indexedValue %>"), 0o644); err != nil {
		t.Fatal(err)
	}
	server := New(nil, io.Discard, io.Discard)
	server.settings.CacheEnabled = false
	t.Cleanup(server.stopWorkspaceIndexWorkers)

	started := make(chan struct{})
	release := make(chan struct{})
	var startedOnce sync.Once
	server.workspaceIndexTestHook = func(_ context.Context, phase workspaceIndexTestPhase, _ uint64) {
		if phase != workspaceIndexTestPhaseStarted {
			return
		}
		startedOnce.Do(func() { close(started) })
		<-release
	}

	if _, rpcErr := server.handleRequest(context.Background(), "initialize", mustRaw(map[string]any{
		"rootUri": filePathURI(root),
	})); rpcErr != nil {
		t.Fatalf("initialize returned an error: %#v", rpcErr)
	}
	server.mu.Lock()
	workerStartedDuringInitialize := server.workspaceIndexDone != nil
	server.mu.Unlock()
	if workerStartedDuringInitialize {
		t.Fatal("initialize started workspace indexing before initialized")
	}

	if err := server.handleNotification(context.Background(), "initialized", nil); err != nil {
		t.Fatal(err)
	}
	waitForWorkspaceIndexSignal(t, started)

	openURI := filePathURI(filepath.Join(root, "open.asp"))
	if err := server.handleNotification(context.Background(), "textDocument/didOpen", mustRaw(map[string]any{
		"textDocument": map[string]any{
			"uri": openURI, "languageId": "classic-asp", "version": 1, "text": "<% Dim openValue %>",
		},
	})); err != nil {
		t.Fatal(err)
	}
	if _, parsed := server.parsed(openURI); parsed == nil {
		t.Fatal("open document was unavailable while workspace indexing was blocked")
	}

	close(release)
	waitForWorkspaceIndexCompletion(t, server)
	server.mu.Lock()
	_, indexed := server.workspace[filePathURI(workspaceFile)]
	server.mu.Unlock()
	if !indexed {
		t.Fatal("workspace file was not published after indexing completed")
	}
}

func TestWorkspaceIndexReadsDiscoveredFilesInParallel(t *testing.T) {
	root := t.TempDir()
	for _, name := range []string{"first.asp", "second.asp"} {
		if err := os.WriteFile(filepath.Join(root, name), []byte("<% Dim "+name[:len(name)-4]+"Value %>"), 0o644); err != nil {
			t.Fatal(err)
		}
	}
	server := New(nil, io.Discard, io.Discard)
	defer server.shutdownRuntimeCaches()
	server.settings.CacheEnabled = false
	server.workspaceIndexEnabled = true
	server.workspaceIndexGeneration = 1
	server.analysisWorkers.setWorkers(4)

	readStarted := make(chan string, 2)
	releaseReads := make(chan struct{})
	server.workspaceFileReadTestHook = func(path string) {
		readStarted <- path
		<-releaseReads
	}
	type buildResult struct {
		documents map[string]*core.TextDocument
		progress  []int
		ok        bool
	}
	completed := make(chan buildResult, 1)
	go func() {
		progressValues := []int{}
		result := server.buildWorkspaceIndexWithProgress(context.Background(), 1, workspaceIndexRunSettings{
			roots: []workspaceRoot{{Path: root, URI: filePathURI(root)}},
		}, func(label string, current, _ int, _ string) {
			if label == "workspace.index.scanFiles" {
				progressValues = append(progressValues, current)
			}
		})
		completed <- buildResult{documents: result.documents, progress: progressValues, ok: result.complete}
	}()

	<-readStarted
	select {
	case <-readStarted:
	case <-time.After(time.Second):
		close(releaseReads)
		<-completed
		t.Fatal("workspace index started only one file read at a time")
	}
	close(releaseReads)
	result := <-completed
	if !result.ok || len(result.documents) != 2 {
		t.Fatalf("parallel workspace index result = ok:%v documents:%d", result.ok, len(result.documents))
	}
	if len(result.progress) != 3 || result.progress[0] != 0 || result.progress[1] != 1 || result.progress[2] != 2 {
		t.Fatalf("parallel workspace index progress = %#v, want [0 1 2]", result.progress)
	}
}

func TestWorkspaceIndexPreparesTrustedFilesystemRootOnce(t *testing.T) {
	root := t.TempDir()
	for _, name := range []string{"first.asp", "second.asp", "third.asp"} {
		if err := os.WriteFile(filepath.Join(root, name), []byte("<% Dim value %>"), 0o644); err != nil {
			t.Fatal(err)
		}
	}
	server := New(nil, io.Discard, io.Discard)
	defer server.shutdownRuntimeCaches()
	server.rootPath = root
	server.rootURI = filePathURI(root)
	server.workspaceRoots = []workspaceRoot{{Path: root, URI: server.rootURI}}
	server.settings.CacheEnabled = false
	server.workspaceIndexEnabled = true
	server.workspaceIndexGeneration = 1

	var openRootCalls atomic.Int32
	trustedFilesystemOpenRootTestHooks.Store(&trustedFilesystemOpenRootTestHook{fn: func(string) {
		openRootCalls.Add(1)
	}})
	t.Cleanup(func() { trustedFilesystemOpenRootTestHooks.Store(nil) })

	documents, _, ok := server.buildWorkspaceIndex(context.Background(), 1, workspaceIndexRunSettings{
		roots: []workspaceRoot{{Path: root, URI: server.rootURI}},
	})
	if !ok || len(documents) != 3 {
		t.Fatalf("workspace index result = ok:%t documents:%d, want 3 documents", ok, len(documents))
	}
	if got := openRootCalls.Load(); got != 1 {
		t.Fatalf("trusted filesystem root opens = %d, want 1 for the whole index job", got)
	}
}

func TestWorkspaceIndexPreparedRootRejectsReplacementDuringPreparation(t *testing.T) {
	base := t.TempDir()
	root := filepath.Join(base, "workspace")
	movedRoot := filepath.Join(base, "workspace-moved")
	replacementRoot := filepath.Join(base, "workspace-replacement")
	for _, directory := range []string{root, replacementRoot} {
		if err := os.Mkdir(directory, 0o755); err != nil {
			t.Fatal(err)
		}
	}
	name := "target.asp"
	if err := os.WriteFile(filepath.Join(root, name), []byte("original"), 0o644); err != nil {
		t.Fatal(err)
	}
	if err := os.WriteFile(filepath.Join(replacementRoot, name), []byte("replacement"), 0o644); err != nil {
		t.Fatal(err)
	}
	server := New(nil, io.Discard, io.Discard)
	defer server.shutdownRuntimeCaches()
	server.rootPath = root
	server.rootURI = filePathURI(root)
	server.workspaceRoots = []workspaceRoot{{Path: root, URI: server.rootURI}}
	server.settings.CacheEnabled = false
	server.workspaceIndexEnabled = true
	server.workspaceIndexGeneration = 1

	trustedFilesystemOpenRootTestHooks.Store(&trustedFilesystemOpenRootTestHook{fn: func(string) {
		if err := os.Rename(root, movedRoot); err != nil {
			t.Errorf("rename configured root: %v", err)
			return
		}
		if err := os.Rename(replacementRoot, root); err != nil {
			t.Errorf("replace configured root: %v", err)
		}
	}})
	t.Cleanup(func() { trustedFilesystemOpenRootTestHooks.Store(nil) })

	documents, _, ok := server.buildWorkspaceIndex(context.Background(), 1, workspaceIndexRunSettings{
		roots: []workspaceRoot{{Path: root, URI: server.rootURI}},
	})
	if ok || documents != nil {
		t.Fatalf("workspace index through replaced root = ok:%t documents:%d, want rejection", ok, len(documents))
	}
}

func TestWorkspaceIndexPreparedRootRejectsReplacementBeforeFileOpen(t *testing.T) {
	base := t.TempDir()
	root := filepath.Join(base, "workspace")
	movedRoot := filepath.Join(base, "workspace-moved")
	replacementRoot := filepath.Join(base, "workspace-replacement")
	for _, directory := range []string{root, replacementRoot} {
		if err := os.Mkdir(directory, 0o755); err != nil {
			t.Fatal(err)
		}
	}
	name := "target.asp"
	if err := os.WriteFile(filepath.Join(root, name), []byte("original"), 0o644); err != nil {
		t.Fatal(err)
	}
	if err := os.WriteFile(filepath.Join(replacementRoot, name), []byte("replacement"), 0o644); err != nil {
		t.Fatal(err)
	}
	originalInfo, err := os.Stat(root)
	if err != nil {
		t.Fatal(err)
	}
	if !os.SameFile(originalInfo, originalInfo) {
		t.Fatal("configured root identity is not stable")
	}
	server := New(nil, io.Discard, io.Discard)
	defer server.shutdownRuntimeCaches()
	server.rootPath = root
	server.rootURI = filePathURI(root)
	server.workspaceRoots = []workspaceRoot{{Path: root, URI: server.rootURI}}
	server.settings.CacheEnabled = false
	server.workspaceIndexEnabled = true
	server.workspaceIndexGeneration = 1
	var replaced atomic.Bool
	var replacementBlocked atomic.Bool
	server.workspaceFileReadTestHook = func(string) {
		if replaced.Swap(true) {
			return
		}
		if err := os.Rename(root, movedRoot); err != nil {
			const windowsSharingViolation syscall.Errno = 32
			if runtime.GOOS == "windows" && errors.Is(err, windowsSharingViolation) {
				replacementBlocked.Store(true)
				return
			}
			t.Errorf("rename configured root: %v", err)
			return
		}
		if err := os.Rename(replacementRoot, root); err != nil {
			t.Errorf("replace configured root: %v", err)
		}
	}

	documents, _, ok := server.buildWorkspaceIndex(context.Background(), 1, workspaceIndexRunSettings{
		roots: []workspaceRoot{{Path: root, URI: server.rootURI}},
	})
	if replacementBlocked.Load() {
		currentInfo, err := os.Stat(root)
		if err != nil || !os.SameFile(originalInfo, currentInfo) {
			t.Fatalf("configured root after blocked replacement = (%v, %v), want original root", currentInfo, err)
		}
		document := documents[filePathURI(filepath.Join(root, name))]
		if !ok || len(documents) != 1 || document == nil || document.Text != "original" {
			t.Fatalf("workspace index after blocked root replacement = ok:%t documents:%d document:%#v, want original document", ok, len(documents), document)
		}
		return
	}
	if ok || documents != nil {
		t.Fatalf("workspace index after prepared-root replacement = ok:%t documents:%d, want rejection", ok, len(documents))
	}
}

func TestColdWorkspaceIndexReadsAndParsesEachFileOnce(t *testing.T) {
	root := t.TempDir()
	files := map[string]string{
		filepath.Join(root, "default.asp"): `<!-- #include file="shared.inc" --><% Response.Write SharedValue %>`,
		filepath.Join(root, "shared.inc"):  `<% Const SharedValue = 1 %>`,
	}
	for path, text := range files {
		if err := os.WriteFile(path, []byte(text), 0o644); err != nil {
			t.Fatal(err)
		}
	}

	server := New(nil, io.Discard, io.Discard)
	server.rootPath = root
	server.rootURI = filePathURI(root)
	server.workspaceRoots = []workspaceRoot{{Path: root, URI: filePathURI(root)}}
	server.settings.CacheEnabled = false
	server.analysisWorkers.setWorkers(4)
	t.Cleanup(server.stopWorkspaceIndexWorkers)
	defer server.shutdownRuntimeCaches()

	var countsMu sync.Mutex
	reads := make(map[string]int, len(files))
	parses := make(map[string]int, len(files))
	transientParses := make(map[string]int)
	server.workspaceFileReadTestHook = func(path string) {
		countsMu.Lock()
		reads[filepath.Clean(path)]++
		countsMu.Unlock()
	}
	server.documentParseTestHook = func(uri string) {
		countsMu.Lock()
		parses[filepath.Clean(fileURIPath(uri))]++
		countsMu.Unlock()
	}
	server.workspaceVBAutoIncludeTransientParseTestHook = func(uri string) {
		countsMu.Lock()
		transientParses[filepath.Clean(fileURIPath(uri))]++
		countsMu.Unlock()
	}

	server.activateWorkspaceIndexing(context.Background())
	server.scheduleWorkspaceIndex("test.coldSingleReadAndParse")
	waitForWorkspaceIndexCompletion(t, server)
	if documents, _ := server.workspaceGraphDocumentsContext(context.Background()); len(documents) != len(files) {
		t.Fatalf("workspace graph documents = %d, want %d", len(documents), len(files))
	}
	if !server.rebuildWorkspaceVBAutoIncludeCatalog(context.Background(), server.workspaceIndexGeneration) {
		t.Fatal("rebuilding the unchanged VBScript auto-include catalog failed")
	}

	countsMu.Lock()
	defer countsMu.Unlock()
	for path := range files {
		cleanPath := filepath.Clean(path)
		if reads[cleanPath] != 1 {
			t.Errorf("physical reads for %s = %d, want 1", cleanPath, reads[cleanPath])
		}
		if parses[cleanPath] != 1 {
			t.Errorf("syntax parses for %s = %d, want 1", cleanPath, parses[cleanPath])
		}
	}
	if len(reads) != len(files) || len(parses) != len(files) {
		t.Fatalf("counted paths = reads:%#v parses:%#v, want only workspace files", reads, parses)
	}
	if len(transientParses) != 0 {
		t.Fatalf("cold workspace index used transient fallback parses: %#v", transientParses)
	}
}

func TestWorkspaceScanSkipsSymlinkedFilesAndDirectories(t *testing.T) {
	root := t.TempDir()
	realFile := filepath.Join(root, "real.asp")
	if err := os.WriteFile(realFile, []byte("<% Dim realValue %>"), 0o644); err != nil {
		t.Fatal(err)
	}
	outside := t.TempDir()
	outsideFile := filepath.Join(outside, "outside.asp")
	if err := os.WriteFile(outsideFile, []byte("<% Dim outsideValue %>"), 0o644); err != nil {
		t.Fatal(err)
	}
	if err := os.Symlink(outsideFile, filepath.Join(root, "linked.asp")); err != nil {
		t.Skipf("file symlink creation unavailable: %v", err)
	}
	if err := os.Symlink(outside, filepath.Join(root, "linked-directory")); err != nil {
		t.Skipf("directory symlink creation unavailable: %v", err)
	}

	files := scanWorkspaceFilesWithContextProgress(context.Background(), root, 0, nil)
	if len(files) != 1 || filepath.Clean(files[0].Path) != filepath.Clean(realFile) {
		t.Fatalf("workspace scan files = %#v, want only %q", files, realFile)
	}
}

func TestWorkspaceScanStopsAndDropsPartialResultsAfterCancellation(t *testing.T) {
	root := t.TempDir()
	const fileCount = 8
	for index := 0; index < fileCount; index++ {
		path := filepath.Join(root, "file-"+strconv.Itoa(index)+".asp")
		if err := os.WriteFile(path, []byte("<% Dim value %>"), 0o644); err != nil {
			t.Fatal(err)
		}
	}
	ctx, cancel := context.WithCancel(context.Background())
	defer cancel()
	seen := 0
	files := scanWorkspaceFilesWithContextProgress(ctx, root, 0, func(workspaceFile) {
		seen++
		cancel()
	})
	if seen == 0 || seen >= fileCount {
		t.Fatalf("workspace scan visited %d files after cancellation, want between 1 and %d", seen, fileCount-1)
	}
	if files != nil {
		t.Fatalf("workspace scan returned partial results after cancellation: %#v", files)
	}
}

func TestWorkspaceFileEligibilityRejectsTraversalOutsideConfiguredBoundaries(t *testing.T) {
	root := t.TempDir()
	outside := t.TempDir()
	outsideFile := filepath.Join(outside, "outside.asp")
	if err := os.WriteFile(outsideFile, []byte("<% Dim outsideValue %>"), 0o644); err != nil {
		t.Fatal(err)
	}
	server := New(nil, io.Discard, nil)
	server.rootPath = root
	server.rootURI = filePathURI(root)
	server.workspaceRoots = []workspaceRoot{{Path: root, URI: filePathURI(root)}}

	if server.workspaceFileEligibleForAutomaticIndex(filepath.Join(root, "..", filepath.Base(outside), "outside.asp")) {
		t.Fatal("workspace eligibility allowed a path outside the workspace root")
	}
	if _, err := server.readSourceFileBytes(context.Background(), outsideFile, nil); !errors.Is(err, errWorkspacePathOutsideBoundary) {
		t.Fatalf("outside source read error = %v, want errWorkspacePathOutsideBoundary", err)
	}
}

func TestParallelWorkspaceIndexCancellationReturnsNoPartialIndex(t *testing.T) {
	root := t.TempDir()
	for _, name := range []string{"first.asp", "second.asp", "third.asp"} {
		if err := os.WriteFile(filepath.Join(root, name), []byte("<% Dim value %>"), 0o644); err != nil {
			t.Fatal(err)
		}
	}
	server := New(nil, io.Discard, io.Discard)
	defer server.shutdownRuntimeCaches()
	server.settings.CacheEnabled = false
	server.workspaceIndexEnabled = true
	server.workspaceIndexGeneration = 1
	server.analysisWorkers.setWorkers(4)
	readLimiter := make(chan struct{}, 1)
	readLimiter <- struct{}{}
	ctx, cancel := context.WithCancel(context.Background())
	type buildResult struct {
		documents map[string]*core.TextDocument
		ok        bool
	}
	completed := make(chan buildResult, 1)
	go func() {
		documents, _, ok := server.buildWorkspaceIndex(ctx, 1, workspaceIndexRunSettings{
			roots:       []workspaceRoot{{Path: root, URI: filePathURI(root)}},
			readLimiter: readLimiter,
		})
		completed <- buildResult{documents: documents, ok: ok}
	}()

	deadline := time.Now().Add(time.Second)
	for {
		server.mu.Lock()
		pending := len(server.sourceReadInflight)
		server.mu.Unlock()
		if pending > 0 {
			break
		}
		if time.Now().After(deadline) {
			cancel()
			<-readLimiter
			t.Fatal("parallel workspace index did not start a file read")
		}
		time.Sleep(time.Millisecond)
	}
	cancel()
	select {
	case result := <-completed:
		if result.ok || result.documents != nil {
			t.Fatalf("cancelled workspace index published a partial result: ok=%v documents=%d", result.ok, len(result.documents))
		}
	case <-time.After(time.Second):
		<-readLimiter
		t.Fatal("parallel workspace index did not stop after cancellation")
	}
	<-readLimiter
}

func BenchmarkWorkspaceIndexFileReads(b *testing.B) {
	root := b.TempDir()
	const fileCount = 128
	for index := 0; index < fileCount; index++ {
		name := filepath.Join(root, "file-"+strconv.Itoa(index)+".asp")
		if err := os.WriteFile(name, []byte("<% Dim value %>"), 0o644); err != nil {
			b.Fatal(err)
		}
	}
	cpuWorkers := max(1, runtime.NumCPU())
	for _, benchmark := range []struct {
		name    string
		workers int
	}{
		{name: "workers-1", workers: 1},
		{name: "workers-cpu-half", workers: max(1, cpuWorkers/2)},
		{name: "workers-cpu", workers: cpuWorkers},
		{name: "workers-cpu-double", workers: cpuWorkers * 2},
	} {
		b.Run(benchmark.name, func(b *testing.B) {
			server := New(nil, io.Discard, io.Discard)
			defer server.shutdownRuntimeCaches()
			server.settings.CacheEnabled = false
			server.workspaceIndexEnabled = true
			server.workspaceIndexGeneration = 1
			server.analysisWorkers.setWorkers(benchmark.workers)
			server.workspaceFileReadTestHook = func(string) { time.Sleep(200 * time.Microsecond) }
			settings := workspaceIndexRunSettings{roots: []workspaceRoot{{Path: root, URI: filePathURI(root)}}}
			b.ReportMetric(fileCount, "files/op")
			b.ResetTimer()
			for b.Loop() {
				server.clearSourceSnapshots()
				documents, _, ok := server.buildWorkspaceIndex(context.Background(), 1, settings)
				if !ok || len(documents) != fileCount {
					b.Fatalf("workspace index result = ok:%v documents:%d", ok, len(documents))
				}
			}
		})
	}
}

func BenchmarkWarmWorkspaceIndexMetadataValidation(b *testing.B) {
	root := b.TempDir()
	cacheDirectory := b.TempDir()
	const fileCount = 256
	documents := make(map[string]*core.TextDocument, fileCount)
	for index := range fileCount {
		fileName := filepath.Join(root, fmt.Sprintf("cached-%03d.asp", index))
		text := fmt.Sprintf("<%% Dim cachedValue%d %%>", index)
		if err := os.WriteFile(fileName, []byte(text), 0o644); err != nil {
			b.Fatal(err)
		}
		uri := filePathURI(fileName)
		documents[uri] = core.NewTextDocument(uri, "classic-asp", 0, text)
	}
	newServer := func() *Server {
		server := New(nil, io.Discard, io.Discard)
		server.rootPath = root
		server.rootURI = filePathURI(root)
		server.workspaceRoots = []workspaceRoot{{Path: root, URI: filePathURI(root)}}
		server.settings.CacheEnabled = true
		server.settings.CacheDirectory = cacheDirectory
		server.settings.CacheFreshness = "metadata"
		server.configureDiskAnalysisCache()
		server.configureFsGateway()
		return server
	}
	first := newServer()
	first.writeWorkspaceIndexToDisk(documents)
	first.shutdownRuntimeCaches()

	for _, benchmark := range []struct {
		name    string
		workers int
	}{
		{name: "workers-1", workers: 1},
		{name: "workers-cpu", workers: runtime.GOMAXPROCS(0)},
	} {
		b.Run(benchmark.name, func(b *testing.B) {
			server := newServer()
			defer server.shutdownRuntimeCaches()
			server.analysisWorkers.setWorkers(benchmark.workers)
			b.ReportAllocs()
			b.ReportMetric(fileCount, "files/op")
			b.ResetTimer()
			for b.Loop() {
				restored, complete, stale := server.restoreWorkspaceIndexFromDiskContext(context.Background(), "metadata")
				if !complete || stale || len(restored) != fileCount {
					b.Fatalf("warm restore = complete:%t stale:%t documents:%d", complete, stale, len(restored))
				}
			}
		})
	}
}

func TestWarmWorkspaceIndexRestoresIncludeGraphWithoutHydratingFileBundles(t *testing.T) {
	root := t.TempDir()
	cacheDirectory := t.TempDir()
	fileName := filepath.Join(root, "cached.asp")
	text := `<% Dim cachedValue %>`
	if err := os.WriteFile(fileName, []byte(text), 0o644); err != nil {
		t.Fatal(err)
	}
	newServer := func() *Server {
		server := New(nil, io.Discard, io.Discard)
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
	uri := filePathURI(fileName)
	doc := core.NewTextDocument(uri, "classic-asp", 0, text)
	first.writeWorkspaceIndexToDisk(map[string]*core.TextDocument{uri: doc})
	first.syncWorkspaceIncludeGraphCache([]*core.ParsedDocument{
		core.ParseDocument(uri, text, core.Settings{DefaultLanguage: "VBScript"}),
	})
	first.shutdownRuntimeCaches()

	second := newServer()
	defer second.shutdownRuntimeCaches()
	var physicalReads atomic.Int32
	var parses atomic.Int32
	var transientParses atomic.Int32
	second.workspaceFileReadTestHook = func(string) { physicalReads.Add(1) }
	second.documentParseTestHook = func(string) { parses.Add(1) }
	second.workspaceVBAutoIncludeTransientParseTestHook = func(string) { transientParses.Add(1) }
	second.activateWorkspaceIndexing(context.Background())
	second.scheduleWorkspaceIndex("test.warmLazyRestore")
	waitForWorkspaceIndexCompletion(t, second)
	if got := physicalReads.Load(); got != 0 {
		t.Fatalf("warm workspace startup physically reread %d unchanged files, want zero", got)
	}
	if got := parses.Load(); got != 0 {
		t.Fatalf("warm workspace startup hydrated %d file bundles, want zero", got)
	}
	if got := transientParses.Load(); got != 1 {
		t.Fatalf("warm workspace startup transient summary parses = %d, want 1", got)
	}
	second.mu.Lock()
	graphSize := second.workspaceIncludeGraph.Size()
	parsedEntries := len(second.parsedCache)
	second.mu.Unlock()
	if graphSize != 1 || parsedEntries != 0 {
		t.Fatalf("warm restore state = graph:%d parsed:%d, want graph:1 parsed:0", graphSize, parsedEntries)
	}
}

func TestWarmWorkspaceIndexOverlapsIncludeGraphReadWithMetadataValidation(t *testing.T) {
	root := t.TempDir()
	cacheDirectory := t.TempDir()
	fileName := filepath.Join(root, "cached.asp")
	text := `<% Dim cachedValue %>`
	if err := os.WriteFile(fileName, []byte(text), 0o644); err != nil {
		t.Fatal(err)
	}
	newServer := func() *Server {
		server := New(nil, io.Discard, io.Discard)
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
	uri := filePathURI(fileName)
	doc := core.NewTextDocument(uri, "classic-asp", 0, text)
	first.writeWorkspaceIndexToDisk(map[string]*core.TextDocument{uri: doc})
	first.syncWorkspaceIncludeGraphCache([]*core.ParsedDocument{
		core.ParseDocument(uri, text, core.Settings{DefaultLanguage: "VBScript"}),
	})
	first.shutdownRuntimeCaches()

	second := newServer()
	defer second.shutdownRuntimeCaches()
	second.analysisWorkers.setWorkers(4)
	graphReadStarted := make(chan struct{})
	releaseGraphRead := make(chan struct{})
	metadataValidationStarted := make(chan struct{})
	var graphReadOnce sync.Once
	var metadataOnce sync.Once
	var graphReads atomic.Int32
	second.workspaceCacheReadTestHook = func(kind string) {
		if kind != "workspaceIncludeGraph" {
			return
		}
		graphReads.Add(1)
		graphReadOnce.Do(func() { close(graphReadStarted) })
		<-releaseGraphRead
	}
	second.workspaceMetadataValidationTestHook = func(_ context.Context, _ string) {
		metadataOnce.Do(func() { close(metadataValidationStarted) })
	}

	second.activateWorkspaceIndexing(context.Background())
	second.scheduleWorkspaceIndex("test.warmRestoreOverlap")
	waitForWorkspaceIndexSignal(t, graphReadStarted)
	waitForWorkspaceIndexSignal(t, metadataValidationStarted)
	close(releaseGraphRead)
	waitForWorkspaceIndexCompletion(t, second)
	if got := graphReads.Load(); got != 1 {
		t.Fatalf("workspace include graph cache reads = %d, want 1", got)
	}
	second.mu.Lock()
	graphComplete := second.workspaceIncludeGraphComplete
	graphSize := second.workspaceIncludeGraph.Size()
	second.mu.Unlock()
	if !graphComplete || graphSize != 1 {
		t.Fatalf("overlapped include graph restore = complete:%t size:%d, want complete size 1", graphComplete, graphSize)
	}
}

func TestWarmWorkspaceIndexRejectsIncludeGraphCandidateAfterGraphGenerationChange(t *testing.T) {
	root := t.TempDir()
	cacheDirectory := t.TempDir()
	fileName := filepath.Join(root, "cached.asp")
	text := `<% Dim cachedValue %>`
	if err := os.WriteFile(fileName, []byte(text), 0o644); err != nil {
		t.Fatal(err)
	}
	newServer := func() *Server {
		server := New(nil, io.Discard, io.Discard)
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
	uri := filePathURI(fileName)
	first.writeWorkspaceIndexToDisk(map[string]*core.TextDocument{
		uri: core.NewTextDocument(uri, "classic-asp", 0, text),
	})
	first.syncWorkspaceIncludeGraphCache([]*core.ParsedDocument{
		core.ParseDocument(uri, text, core.Settings{DefaultLanguage: "VBScript"}),
	})
	first.shutdownRuntimeCaches()

	second := newServer()
	defer second.shutdownRuntimeCaches()
	second.workspaceIndexGeneration = 1
	candidate := second.readWorkspaceIncludeGraphDiskCandidate()
	if candidate == nil {
		t.Fatal("readWorkspaceIncludeGraphDiskCandidate() = nil; want cached candidate")
	}
	readStarted := make(chan struct{})
	releaseRead := make(chan struct{})
	var readOnce sync.Once
	second.workspaceFileReadTestHook = func(path string) {
		if filepath.Clean(path) != filepath.Clean(fileName) {
			return
		}
		readOnce.Do(func() { close(readStarted) })
		<-releaseRead
	}
	result := make(chan bool, 1)
	go func() {
		result <- second.restoreWorkspaceIncludeGraphCandidateIfWorkspaceCurrent(context.Background(), 1, candidate)
	}()
	waitForWorkspaceIndexSignal(t, readStarted)
	second.mu.Lock()
	second.graphGeneration++
	second.mu.Unlock()
	close(releaseRead)
	select {
	case restored := <-result:
		if restored {
			t.Fatal("stale include graph candidate restored after graph generation changed")
		}
	case <-time.After(5 * time.Second):
		t.Fatal("include graph candidate restore did not finish")
	}
	second.mu.Lock()
	graphComplete := second.workspaceIncludeGraphComplete
	graphSize := second.workspaceIncludeGraph.Size()
	second.mu.Unlock()
	if graphComplete || graphSize != 0 {
		t.Fatalf("stale include graph candidate state = complete:%t size:%d, want incomplete empty graph", graphComplete, graphSize)
	}
}

func TestWarmWorkspaceIndexValidatesCachedMetadataInParallel(t *testing.T) {
	root := t.TempDir()
	cacheDirectory := t.TempDir()
	documents := make(map[string]*core.TextDocument)
	for index := range 4 {
		fileName := filepath.Join(root, fmt.Sprintf("cached-%d.asp", index))
		text := fmt.Sprintf("<%% Dim cachedValue%d %%>", index)
		if err := os.WriteFile(fileName, []byte(text), 0o644); err != nil {
			t.Fatal(err)
		}
		uri := filePathURI(fileName)
		documents[uri] = core.NewTextDocument(uri, "classic-asp", 0, text)
	}
	newServer := func() *Server {
		server := New(nil, io.Discard, io.Discard)
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
	first.writeWorkspaceIndexToDisk(documents)
	first.shutdownRuntimeCaches()

	second := newServer()
	defer second.shutdownRuntimeCaches()
	second.analysisWorkers.setWorkers(4)
	validationStarted := make(chan struct{}, len(documents))
	releaseValidation := make(chan struct{})
	var active atomic.Int32
	var maximum atomic.Int32
	second.workspaceMetadataValidationTestHook = func(_ context.Context, _ string) {
		current := active.Add(1)
		for observed := maximum.Load(); current > observed && !maximum.CompareAndSwap(observed, current); observed = maximum.Load() {
		}
		validationStarted <- struct{}{}
		<-releaseValidation
		active.Add(-1)
	}

	second.activateWorkspaceIndexing(context.Background())
	second.scheduleWorkspaceIndex("test.parallelMetadataValidation")
	waitForWorkspaceIndexSignal(t, validationStarted)
	waitForWorkspaceIndexSignal(t, validationStarted)
	close(releaseValidation)
	waitForWorkspaceIndexCompletion(t, second)
	if got := maximum.Load(); got < 2 {
		t.Fatalf("maximum concurrent metadata validations = %d, want at least 2", got)
	}
	second.mu.Lock()
	workspaceSize := len(second.workspace)
	second.mu.Unlock()
	if workspaceSize != len(documents) {
		t.Fatalf("parallel metadata restore workspace size = %d, want %d", workspaceSize, len(documents))
	}
}

func TestWarmWorkspaceIndexDoesNotPublishCandidateBeforeFreshnessValidation(t *testing.T) {
	root := t.TempDir()
	cacheDirectory := t.TempDir()
	fileName := filepath.Join(root, "cached.asp")
	const oldText = `<% Dim oldValue %>`
	const newText = `<% Dim replacementValueWithDifferentSize %>`
	if err := os.WriteFile(fileName, []byte(oldText), 0o644); err != nil {
		t.Fatal(err)
	}
	newServer := func() *Server {
		server := New(nil, io.Discard, io.Discard)
		server.rootPath = root
		server.rootURI = filePathURI(root)
		server.workspaceRoots = []workspaceRoot{{Path: root, URI: filePathURI(root)}}
		server.settings.CacheEnabled = true
		server.settings.CacheDirectory = cacheDirectory
		server.settings.CacheFreshness = "metadata"
		server.configureDiskAnalysisCache()
		server.configureFsGateway()
		return server
	}

	first := newServer()
	uri := filePathURI(fileName)
	first.writeWorkspaceIndexToDisk(map[string]*core.TextDocument{
		uri: core.NewTextDocument(uri, "classic-asp", 0, oldText),
	})
	first.shutdownRuntimeCaches()

	second := newServer()
	defer second.shutdownRuntimeCaches()
	validationStarted := make(chan struct{})
	releaseValidation := make(chan struct{})
	var validationOnce sync.Once
	second.workspaceMetadataValidationTestHook = func(_ context.Context, _ string) {
		validationOnce.Do(func() { close(validationStarted) })
		<-releaseValidation
	}

	second.activateWorkspaceIndexing(context.Background())
	second.scheduleWorkspaceIndex("test.candidatePublicationFence")
	waitForWorkspaceIndexSignal(t, validationStarted)
	second.mu.Lock()
	publishedDuringValidation := second.workspace[uri]
	second.mu.Unlock()
	if publishedDuringValidation != nil {
		close(releaseValidation)
		waitForWorkspaceIndexCompletion(t, second)
		t.Fatalf("unvalidated cached document was published: %#v", publishedDuringValidation)
	}
	if err := os.WriteFile(fileName, []byte(newText), 0o644); err != nil {
		close(releaseValidation)
		waitForWorkspaceIndexCompletion(t, second)
		t.Fatal(err)
	}
	close(releaseValidation)
	waitForWorkspaceIndexCompletion(t, second)
	second.mu.Lock()
	published := second.workspace[uri]
	second.mu.Unlock()
	if published == nil || published.Text != newText {
		t.Fatalf("published workspace document = %#v, want updated source", published)
	}
}

func TestWarmWorkspaceIndexRejectsCacheCandidateAfterCacheReconfiguration(t *testing.T) {
	root := t.TempDir()
	cacheDirectory := t.TempDir()
	fileName := filepath.Join(root, "cached.asp")
	text := `<% Dim cachedValue %>`
	if err := os.WriteFile(fileName, []byte(text), 0o644); err != nil {
		t.Fatal(err)
	}
	newServer := func() *Server {
		server := New(nil, io.Discard, io.Discard)
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
	uri := filePathURI(fileName)
	first.writeWorkspaceIndexToDisk(map[string]*core.TextDocument{
		uri: core.NewTextDocument(uri, "classic-asp", 0, text),
	})
	first.shutdownRuntimeCaches()

	second := newServer()
	defer second.shutdownRuntimeCaches()
	reconfigured := make(chan struct{})
	var reconfigureOnce sync.Once
	second.workspaceIndexTestHook = func(_ context.Context, phase workspaceIndexTestPhase, generation uint64) {
		if phase != workspaceIndexTestPhaseBeforePublish || generation != 1 {
			return
		}
		reconfigureOnce.Do(func() {
			second.mu.Lock()
			second.settings.CacheEnabled = false
			second.mu.Unlock()
			second.configureDiskAnalysisCache()
			if err := os.Remove(fileName); err != nil {
				t.Errorf("remove cached source: %v", err)
			}
			close(reconfigured)
		})
	}

	second.activateWorkspaceIndexing(context.Background())
	second.scheduleWorkspaceIndex("test.cacheReconfigurationFence")
	waitForWorkspaceIndexSignal(t, reconfigured)
	deadline := time.Now().Add(5 * time.Second)
	for {
		second.mu.Lock()
		generation := second.workspaceIndexGeneration
		done := second.workspaceIndexDone
		second.mu.Unlock()
		if generation >= 2 {
			waitForWorkspaceIndexSignal(t, done)
			break
		}
		if time.Now().After(deadline) {
			t.Fatal("workspace index did not schedule a cache-drift retry")
		}
		runtime.Gosched()
	}
	second.mu.Lock()
	_, published := second.workspace[uri]
	generation := second.workspaceIndexGeneration
	second.mu.Unlock()
	if published {
		t.Fatal("cached workspace candidate was published after cache reconfiguration")
	}
	if generation < 2 {
		t.Fatalf("workspace index generation = %d, want retry generation at least 2", generation)
	}
}

func TestWarmWorkspaceIndexRejectsGraphOnlyCandidateAfterCacheReconfiguration(t *testing.T) {
	root := t.TempDir()
	cacheDirectory := t.TempDir()
	fileName := filepath.Join(root, "cached.asp")
	text := `<% Dim cachedValue %>`
	if err := os.WriteFile(fileName, []byte(text), 0o644); err != nil {
		t.Fatal(err)
	}
	newServer := func() *Server {
		server := New(nil, io.Discard, io.Discard)
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
	info, ok := first.fsStat(fileName)
	if !ok {
		first.shutdownRuntimeCaches()
		t.Fatal("cached source metadata is unavailable")
	}
	cache := first.diskCacheForUse()
	if cache == nil {
		first.shutdownRuntimeCaches()
		t.Fatal("disk analysis cache is unavailable")
	}
	if err := cache.WriteWorkspaceIncludeGraph(workspacepkg.DiskWorkspaceIncludeGraphCacheEntry{
		SettingsKey: first.workspaceDiskSettingsKey(),
		Entries: []workspacepkg.IncludeGraphEntry{{
			FileName: fileName,
			Source: workspacepkg.SourceMetadata{
				FileName: fileName, MtimeMS: info.MtimeMS, Size: info.Size,
				ContentHash: workspacepkg.DiskContentHash(text),
			},
			RefsFingerprint: workspacepkg.DiskContentHash("null"),
		}},
	}); err != nil {
		first.shutdownRuntimeCaches()
		t.Fatal(err)
	}
	first.shutdownRuntimeCaches()

	second := newServer()
	defer second.shutdownRuntimeCaches()
	reconfigured := make(chan struct{})
	var reconfigureOnce sync.Once
	second.workspaceIndexTestHook = func(_ context.Context, phase workspaceIndexTestPhase, generation uint64) {
		if phase != workspaceIndexTestPhaseBeforePublish || generation != 1 {
			return
		}
		reconfigureOnce.Do(func() {
			second.mu.Lock()
			second.settings.CacheEnabled = false
			second.mu.Unlock()
			second.configureDiskAnalysisCache()
			if err := os.Remove(fileName); err != nil {
				t.Errorf("remove cached source: %v", err)
			}
			close(reconfigured)
		})
	}

	second.activateWorkspaceIndexing(context.Background())
	second.scheduleWorkspaceIndex("test.graphOnlyCacheReconfigurationFence")
	waitForWorkspaceIndexSignal(t, reconfigured)
	deadline := time.Now().Add(5 * time.Second)
	for {
		second.mu.Lock()
		generation := second.workspaceIndexGeneration
		done := second.workspaceIndexDone
		second.mu.Unlock()
		if generation >= 2 {
			waitForWorkspaceIndexSignal(t, done)
			break
		}
		if time.Now().After(deadline) {
			t.Fatal("workspace index did not schedule a graph-only cache-drift retry")
		}
		runtime.Gosched()
	}
	second.mu.Lock()
	workspaceSize := len(second.workspace)
	graphSize := second.workspaceIncludeGraph.Size()
	generation := second.workspaceIndexGeneration
	second.mu.Unlock()
	if workspaceSize != 0 || graphSize != 0 {
		t.Fatalf("stale graph-only cache state = workspace:%d graph:%d, want both empty", workspaceSize, graphSize)
	}
	if generation < 2 {
		t.Fatalf("workspace index generation = %d, want retry generation at least 2", generation)
	}
}

func TestWarmWorkspaceIndexJoinsSpeculativeGraphReadOnScanFailure(t *testing.T) {
	root := t.TempDir()
	cacheDirectory := t.TempDir()
	fileName := filepath.Join(root, "cached.asp")
	const oldText = `<% Dim oldValue %>`
	const newText = `<% Dim replacementValueWithDifferentSize %>`
	if err := os.WriteFile(fileName, []byte(oldText), 0o644); err != nil {
		t.Fatal(err)
	}
	newServer := func() *Server {
		server := New(nil, io.Discard, io.Discard)
		server.rootPath = root
		server.rootURI = filePathURI(root)
		server.workspaceRoots = []workspaceRoot{{Path: root, URI: filePathURI(root)}}
		server.settings.CacheEnabled = true
		server.settings.CacheDirectory = cacheDirectory
		server.settings.CacheFreshness = "metadata"
		server.configureDiskAnalysisCache()
		server.configureFsGateway()
		return server
	}

	first := newServer()
	uri := filePathURI(fileName)
	first.writeWorkspaceIndexToDisk(map[string]*core.TextDocument{
		uri: core.NewTextDocument(uri, "classic-asp", 0, oldText),
	})
	first.syncWorkspaceIncludeGraphCache([]*core.ParsedDocument{
		core.ParseDocument(uri, oldText, core.Settings{DefaultLanguage: "VBScript"}),
	})
	first.shutdownRuntimeCaches()
	if err := os.WriteFile(fileName, []byte(newText), 0o644); err != nil {
		t.Fatal(err)
	}

	second := newServer()
	defer second.shutdownRuntimeCaches()
	second.workspaceIndexEnabled = true
	second.workspaceIndexGeneration = 1
	graphReadStarted := make(chan struct{})
	releaseGraphRead := make(chan struct{})
	scanFailed := make(chan struct{})
	var graphReadOnce sync.Once
	var scanOnce sync.Once
	second.workspaceCacheReadTestHook = func(kind string) {
		if kind != "workspaceIncludeGraph" {
			return
		}
		graphReadOnce.Do(func() { close(graphReadStarted) })
		<-releaseGraphRead
	}
	second.workspaceWalkDirTestHook = func(string) error {
		scanOnce.Do(func() { close(scanFailed) })
		return errors.New("forced workspace scan failure")
	}

	completed := make(chan workspaceIndexBuildResult, 1)
	go func() {
		completed <- second.buildWorkspaceIndexWithProgress(context.Background(), 1, second.workspaceIndexRunSettings(), nil)
	}()
	waitForWorkspaceIndexSignal(t, graphReadStarted)
	waitForWorkspaceIndexSignal(t, scanFailed)
	select {
	case result := <-completed:
		close(releaseGraphRead)
		t.Fatalf("workspace build returned before speculative graph read joined: %#v", result.err)
	case <-time.After(100 * time.Millisecond):
	}
	close(releaseGraphRead)
	select {
	case result := <-completed:
		if result.complete || result.err == nil {
			t.Fatalf("workspace build result = complete:%t err:%v, want scan failure", result.complete, result.err)
		}
	case <-time.After(5 * time.Second):
		t.Fatal("workspace build did not finish after speculative graph read was released")
	}
}

func TestWarmWorkspaceIndexReusesFreshnessReadDuringStaleFallback(t *testing.T) {
	root := t.TempDir()
	cacheDirectory := t.TempDir()
	fileName := filepath.Join(root, "cached.asp")
	const oldText = `<% Dim oldValue %>`
	const newText = `<% Dim newValue %>`
	if len(oldText) != len(newText) {
		t.Fatal("test source texts must have equal sizes")
	}
	if err := os.WriteFile(fileName, []byte(oldText), 0o644); err != nil {
		t.Fatal(err)
	}
	newServer := func() *Server {
		server := New(nil, io.Discard, io.Discard)
		server.rootPath = root
		server.rootURI = filePathURI(root)
		server.workspaceRoots = []workspaceRoot{{Path: root, URI: filePathURI(root)}}
		server.settings.CacheEnabled = true
		server.settings.CacheDirectory = cacheDirectory
		server.settings.CacheFreshness = "watch"
		server.configureDiskAnalysisCache()
		server.configureFsGateway()
		return server
	}

	first := newServer()
	uri := filePathURI(fileName)
	info, ok := first.fsStat(fileName)
	if !ok {
		first.shutdownRuntimeCaches()
		t.Fatal("cached source metadata is unavailable")
	}
	cache := first.diskCacheForUse()
	if cache == nil {
		first.shutdownRuntimeCaches()
		t.Fatal("disk analysis cache is unavailable")
	}
	if err := cache.WriteWorkspaceIndex(workspacepkg.DiskWorkspaceIndexCacheEntry{
		SettingsKey: first.workspaceDiskSettingsKey(),
		Entries: []workspacepkg.DiskWorkspaceIndexedDocument{{
			URI: uri, FileName: fileName, MtimeMS: info.MtimeMS, Size: info.Size,
			ContentHash: workspacepkg.DiskContentHash(oldText),
		}},
	}); err != nil {
		first.shutdownRuntimeCaches()
		t.Fatal(err)
	}
	first.shutdownRuntimeCaches()
	if err := os.WriteFile(fileName, []byte(newText), 0o644); err != nil {
		t.Fatal(err)
	}
	mtime := time.UnixMilli(info.MtimeMS)
	if err := os.Chtimes(fileName, mtime, mtime); err != nil {
		t.Fatal(err)
	}

	second := newServer()
	defer second.shutdownRuntimeCaches()
	var physicalReads atomic.Int32
	second.workspaceFileReadTestHook = func(path string) {
		if filepath.Clean(path) == filepath.Clean(fileName) {
			physicalReads.Add(1)
		}
	}
	second.activateWorkspaceIndexing(context.Background())
	second.scheduleWorkspaceIndex("test.reuseFreshnessRead")
	waitForWorkspaceIndexCompletion(t, second)
	if got := physicalReads.Load(); got != 1 {
		t.Fatalf("physical source reads = %d, want 1", got)
	}
	second.mu.Lock()
	published := second.workspace[uri]
	second.mu.Unlock()
	if published == nil || published.Text != newText {
		t.Fatalf("published workspace document = %#v, want fresh source", published)
	}
}

func TestWorkspaceIndexGenerationRejectsStaleWorkerPublication(t *testing.T) {
	rootA := t.TempDir()
	rootB := t.TempDir()
	fileA := filepath.Join(rootA, "a.asp")
	fileB := filepath.Join(rootB, "b.asp")
	if err := os.WriteFile(fileA, []byte("<% Dim fromA %>"), 0o644); err != nil {
		t.Fatal(err)
	}
	if err := os.WriteFile(fileB, []byte("<% Dim fromB %>"), 0o644); err != nil {
		t.Fatal(err)
	}
	server := New(nil, io.Discard, io.Discard)
	server.settings.CacheEnabled = false
	t.Cleanup(server.stopWorkspaceIndexWorkers)

	firstBeforePublish := make(chan struct{})
	releaseFirst := make(chan struct{})
	server.workspaceIndexTestHook = func(_ context.Context, phase workspaceIndexTestPhase, generation uint64) {
		if phase == workspaceIndexTestPhaseBeforePublish && generation == 1 {
			close(firstBeforePublish)
			<-releaseFirst
		}
	}
	if _, rpcErr := server.handleRequest(context.Background(), "initialize", mustRaw(map[string]any{
		"rootUri": filePathURI(rootA),
	})); rpcErr != nil {
		t.Fatalf("initialize returned an error: %#v", rpcErr)
	}
	if err := server.handleNotification(context.Background(), "initialized", nil); err != nil {
		t.Fatal(err)
	}
	waitForWorkspaceIndexSignal(t, firstBeforePublish)

	if err := server.handleNotification(context.Background(), "workspace/didChangeWorkspaceFolders", mustRaw(map[string]any{
		"event": map[string]any{
			"removed": []map[string]any{{"uri": filePathURI(rootA)}},
			"added":   []map[string]any{{"uri": filePathURI(rootB)}},
		},
	})); err != nil {
		t.Fatal(err)
	}
	waitForWorkspaceIndexCompletion(t, server)
	close(releaseFirst)
	server.workspaceIndexWorkers.Wait()

	server.mu.Lock()
	_, hasA := server.workspace[filePathURI(fileA)]
	_, hasB := server.workspace[filePathURI(fileB)]
	server.mu.Unlock()
	if hasA || !hasB {
		t.Fatalf("stale worker publication changed workspace: hasA=%v hasB=%v", hasA, hasB)
	}
}

func TestWorkspaceIndexDiskWriteRejectsGenerationChangedAtWriteHook(t *testing.T) {
	root := t.TempDir()
	fileName := filepath.Join(root, "indexed.asp")
	if err := os.WriteFile(fileName, []byte("<% Dim IndexedValue %>"), 0o644); err != nil {
		t.Fatal(err)
	}
	server := newRuntimeCacheTestServer(t)
	server.rootPath = root
	server.rootURI = filePathURI(root)
	server.workspaceRoots = []workspaceRoot{{Path: root, URI: filePathURI(root)}}
	server.configureFsGateway()
	server.workspaceIndexEnabled = true
	server.workspaceIndexGeneration = 1
	server.workspaceIndexTestHook = func(_ context.Context, phase workspaceIndexTestPhase, generation uint64) {
		if phase != workspaceIndexTestPhaseBeforeDiskWrite || generation != 1 {
			return
		}
		server.mu.Lock()
		server.workspaceIndexGeneration = 2
		server.mu.Unlock()
	}
	done := make(chan struct{})
	server.workspaceIndexWorkers.Add(1)
	go server.runWorkspaceIndexWorker(context.Background(), "test.generationChangedAtWrite", 1, done)
	select {
	case <-done:
	case <-time.After(5 * time.Second):
		t.Fatal("workspace index worker did not finish")
	}
	server.waitForAsyncDiskCacheWrites()
	if _, ok := server.diskCacheForUse().ReadWorkspaceIndex(server.workspaceDiskSettingsKey()); ok {
		t.Fatal("obsolete workspace index generation was persisted")
	}
}

func TestWorkspaceIndexDiskWriteRejectsGenerationChangedAfterEnqueue(t *testing.T) {
	root := t.TempDir()
	fileName := filepath.Join(root, "indexed.asp")
	if err := os.WriteFile(fileName, []byte("<% Dim IndexedValue %>"), 0o644); err != nil {
		t.Fatal(err)
	}
	server := newRuntimeCacheTestServer(t)
	server.rootPath = root
	server.rootURI = filePathURI(root)
	server.workspaceRoots = []workspaceRoot{{Path: root, URI: filePathURI(root)}}
	server.configureFsGateway()
	server.workspaceIndexEnabled = true
	server.workspaceIndexGeneration = 1
	server.workspaceIndexTestHook = func(_ context.Context, phase workspaceIndexTestPhase, generation uint64) {
		if phase != workspaceIndexTestPhaseAfterDiskEnqueueBeforeCommit || generation != 1 {
			return
		}
		server.mu.Lock()
		server.workspaceIndexGeneration = 2
		server.mu.Unlock()
	}
	done := make(chan struct{})
	server.workspaceIndexWorkers.Add(1)
	go server.runWorkspaceIndexWorker(context.Background(), "test.generationChangedAfterEnqueue", 1, done)
	select {
	case <-done:
	case <-time.After(5 * time.Second):
		t.Fatal("workspace index worker did not finish")
	}
	server.waitForAsyncDiskCacheWrites()
	cache := server.diskCacheForUse()
	if _, ok := cache.ReadWorkspaceIndex(server.workspaceDiskSettingsKey()); ok {
		t.Fatal("obsolete workspace index generation was persisted")
	}
	if _, ok := cache.ReadWorkspaceMembershipManifest(server.workspaceDiskSettingsKey()); ok {
		t.Fatal("obsolete workspace membership manifest was persisted")
	}
}

func TestWorkspaceIndexDiskWriteGenerationRaceDropsStagedState(t *testing.T) {
	root := t.TempDir()
	fileName := filepath.Join(root, "indexed.asp")
	if err := os.WriteFile(fileName, []byte("<% Dim IndexedValue %>"), 0o644); err != nil {
		t.Fatal(err)
	}
	server := newRuntimeCacheTestServer(t)
	server.rootPath = root
	server.rootURI = filePathURI(root)
	server.workspaceRoots = []workspaceRoot{{Path: root, URI: filePathURI(root)}}
	server.configureFsGateway()
	server.workspaceIndexEnabled = true
	server.workspaceIndexGeneration = 1
	enqueued := make(chan struct{})
	release := make(chan struct{})
	changed := make(chan struct{})
	var once sync.Once
	server.workspaceIndexTestHook = func(_ context.Context, phase workspaceIndexTestPhase, generation uint64) {
		if phase != workspaceIndexTestPhaseAfterDiskEnqueueBeforeCommit || generation != 1 {
			return
		}
		once.Do(func() { close(enqueued) })
		<-release
	}
	done := make(chan struct{})
	server.workspaceIndexWorkers.Add(1)
	go server.runWorkspaceIndexWorker(context.Background(), "test.generationRaceAfterEnqueue", 1, done)
	select {
	case <-enqueued:
	case <-time.After(5 * time.Second):
		t.Fatal("workspace index write did not reach the enqueue hook")
	}
	go func() {
		server.workspaceIndexDiskCommitMu.Lock()
		server.mu.Lock()
		server.workspaceIndexGeneration = 2
		server.mu.Unlock()
		server.workspaceIndexDiskCommitMu.Unlock()
		close(changed)
	}()
	select {
	case <-changed:
	case <-time.After(5 * time.Second):
		t.Fatal("generation change did not complete while the transaction was staged")
	}
	close(release)
	select {
	case <-done:
	case <-time.After(5 * time.Second):
		t.Fatal("workspace index worker did not finish")
	}
	server.waitForAsyncDiskCacheWrites()
	cache := server.diskCacheForUse()
	if _, ok := cache.ReadWorkspaceIndex(server.workspaceDiskSettingsKey()); ok {
		t.Fatal("obsolete workspace index generation was persisted after the race")
	}
	if _, ok := cache.ReadWorkspaceMembershipManifest(server.workspaceDiskSettingsKey()); ok {
		t.Fatal("obsolete workspace membership manifest was persisted after the race")
	}
}

func TestWorkspaceIndexDiskWriteRejectsShutdownAfterEnqueue(t *testing.T) {
	root := t.TempDir()
	fileName := filepath.Join(root, "indexed.asp")
	if err := os.WriteFile(fileName, []byte("<% Dim IndexedValue %>"), 0o644); err != nil {
		t.Fatal(err)
	}
	cacheDirectory := t.TempDir()
	server := New(nil, io.Discard, io.Discard)
	server.rootPath = root
	server.rootURI = filePathURI(root)
	server.workspaceRoots = []workspaceRoot{{Path: root, URI: filePathURI(root)}}
	server.settings.CacheEnabled = true
	server.settings.CacheDirectory = cacheDirectory
	server.settings.CacheTTLHours = 24
	server.settings.CacheMaxSizeMB = 16
	server.configureFsGateway()
	server.configureDiskAnalysisCache()

	server.workspaceIndexEnabled = true
	server.workspaceIndexGeneration = 1
	enqueued := make(chan struct{})
	release := make(chan struct{})
	var enqueueOnce sync.Once
	var releaseOnce sync.Once
	releaseHook := func() { releaseOnce.Do(func() { close(release) }) }
	t.Cleanup(func() {
		releaseHook()
		server.shutdownRuntimeCaches()
	})
	server.workspaceIndexTestHook = func(_ context.Context, phase workspaceIndexTestPhase, generation uint64) {
		if phase != workspaceIndexTestPhaseAfterDiskEnqueueBeforeCommit || generation != 1 {
			return
		}
		enqueueOnce.Do(func() { close(enqueued) })
		<-release
	}

	workerDone := make(chan struct{})
	server.workspaceIndexWorkers.Add(1)
	go server.runWorkspaceIndexWorker(context.Background(), "test.shutdownAfterEnqueue", 1, workerDone)
	select {
	case <-enqueued:
	case <-time.After(5 * time.Second):
		t.Fatal("workspace index write did not reach the enqueue hook")
	}

	// Hold the commit gate while dispatching shutdown. A shutdown transition
	// must wait for this gate rather than changing s.shutdown independently.
	server.workspaceIndexDiskCommitMu.Lock()
	dispatchStarted := make(chan struct{})
	server.requestDispatchTestHook = func(_ context.Context, method string) {
		if method == "shutdown" {
			close(dispatchStarted)
		}
	}
	shutdownResult := make(chan *rpcError, 1)
	go func() {
		_, rpcErr := server.handleRequest(context.Background(), "shutdown", nil)
		shutdownResult <- rpcErr
	}()
	select {
	case <-dispatchStarted:
	case <-time.After(5 * time.Second):
		server.workspaceIndexDiskCommitMu.Unlock()
		releaseHook()
		t.Fatal("shutdown request was not dispatched")
	}
	select {
	case <-shutdownResult:
		server.workspaceIndexDiskCommitMu.Unlock()
		releaseHook()
		t.Fatal("shutdown returned while the workspace commit gate was held")
	case <-time.After(50 * time.Millisecond):
	}
	server.mu.Lock()
	shutdownStarted := server.shutdown
	server.mu.Unlock()
	if shutdownStarted {
		server.workspaceIndexDiskCommitMu.Unlock()
		releaseHook()
		t.Fatal("shutdown changed state without acquiring the workspace commit gate")
	}
	server.workspaceIndexDiskCommitMu.Unlock()

	waitForServerCondition(t, func() bool {
		server.mu.Lock()
		defer server.mu.Unlock()
		return server.shutdown
	})
	releaseHook()
	select {
	case rpcErr := <-shutdownResult:
		if rpcErr != nil {
			t.Fatalf("shutdown returned an error: %#v", rpcErr)
		}
	case <-time.After(5 * time.Second):
		t.Fatal("shutdown did not finish after the staged write was released")
	}
	select {
	case <-workerDone:
	case <-time.After(5 * time.Second):
		t.Fatal("workspace index worker did not finish")
	}

	probe := New(nil, io.Discard, io.Discard)
	probe.rootPath = root
	probe.rootURI = filePathURI(root)
	probe.workspaceRoots = []workspaceRoot{{Path: root, URI: filePathURI(root)}}
	probe.settings.CacheEnabled = true
	probe.settings.CacheDirectory = cacheDirectory
	probe.settings.CacheTTLHours = 24
	probe.settings.CacheMaxSizeMB = 16
	probe.configureDiskAnalysisCache()
	defer probe.shutdownRuntimeCaches()
	cache := probe.diskCacheForUse()
	if cache == nil || !cache.Enabled() {
		t.Fatal("probe workspace database was not opened")
	}
	key := probe.workspaceDiskSettingsKey()
	if _, ok := cache.ReadWorkspaceIndex(key); ok {
		t.Fatal("workspace index was persisted after shutdown began with a staged transaction")
	}
	if _, ok := cache.ReadWorkspaceMembershipManifest(key); ok {
		t.Fatal("workspace membership manifest was persisted after shutdown began with a staged transaction")
	}
}

func TestWorkspaceIndexPublishMergesNewerPerFileChanges(t *testing.T) {
	server := New(nil, io.Discard, io.Discard)
	server.workspaceIndexEnabled = true
	server.workspaceIndexGeneration = 1
	uri := "file:///workspace/default.asp"
	createdURI := "file:///workspace/created.asp"
	deletedURI := "file:///workspace/deleted.asp"
	baseText := "<% Dim baseValue %>"
	server.workspace[uri] = core.NewTextDocument(uri, "classic-asp", 0, "<% Dim changedValue %>")
	server.workspace[createdURI] = core.NewTextDocument(createdURI, "classic-asp", 0, "<% Dim createdValue %>")
	docs := map[string]*core.TextDocument{
		uri:        core.NewTextDocument(uri, "classic-asp", 0, baseText),
		deletedURI: core.NewTextDocument(deletedURI, "classic-asp", 0, "<% Dim deletedValue %>"),
	}
	base := map[string]workspaceArtifactFingerprint{
		uri:        workspaceFingerprint(baseText),
		deletedURI: workspaceFingerprint("<% Dim deletedValue %>"),
	}

	if !server.publishWorkspaceIndex(context.Background(), 1, docs, base) {
		t.Fatal("workspace index was not published")
	}
	if got := server.workspace[uri].Text; got != "<% Dim changedValue %>" {
		t.Fatalf("newer change was overwritten: %q", got)
	}
	if got := server.workspace[createdURI]; got == nil {
		t.Fatal("concurrently created document was dropped")
	}
	if _, ok := server.workspace[deletedURI]; ok {
		t.Fatal("concurrently deleted document was restored")
	}
}

func TestWorkspaceReferenceCountPromotionReusesOnlyIdenticalUniverse(t *testing.T) {
	server := New(nil, io.Discard, io.Discard)
	server.referenceGeneration = 7
	oldKey := workspaceReferenceTargetKey{
		URI: "file:///workspace/default.asp", Name: "sharedvalue", NameHash: 41,
		Line: 2, Character: 4, SymbolKind: "variable", Generation: 3,
	}
	server.workspaceReferencePendingPromotion = &workspaceReferenceCountPromotion{
		workspaceGeneration: 11,
		fingerprint:         "same-universe",
		counts:              map[workspaceReferenceTargetKey]int{oldKey: 23},
	}

	if promoted := server.promoteWorkspaceReferenceCountsLocked(11, "same-universe"); promoted != 1 {
		t.Fatalf("promoted counts = %d, want 1", promoted)
	}
	currentKey := oldKey
	currentKey.Generation = server.referenceGeneration
	if got := server.referenceCounts[currentKey]; got != 23 {
		t.Fatalf("promoted count = %d, want 23", got)
	}
	if server.workspaceReferencePendingPromotion != nil {
		t.Fatal("completed promotion was retained")
	}

	server.workspaceReferencePendingPromotion = &workspaceReferenceCountPromotion{
		workspaceGeneration: 12,
		fingerprint:         "old-universe",
		counts:              map[workspaceReferenceTargetKey]int{oldKey: 99},
	}
	if promoted := server.promoteWorkspaceReferenceCountsLocked(12, "changed-universe"); promoted != 0 {
		t.Fatalf("changed universe promoted %d counts", promoted)
	}
	if got := server.referenceCounts[currentKey]; got != 23 {
		t.Fatalf("changed universe replaced current count with %d", got)
	}
}

func TestWorkspaceReferencePromotionCandidateRequiresReadyGeneration(t *testing.T) {
	server := New(nil, io.Discard, io.Discard)
	server.workspaceIndexGeneration = 4
	server.workspaceReferenceIndexReadyGeneration = 3
	server.workspaceReferenceReadyFingerprint = "ready"
	server.referenceGeneration = 8
	server.referenceCounts[workspaceReferenceTargetKey{URI: "file:///workspace/default.asp", Generation: 8}] = 1
	if candidate := server.workspaceReferencePromotionCandidateLocked(); candidate != nil {
		t.Fatalf("incomplete workspace produced promotion candidate: %#v", candidate)
	}

	server.workspaceReferenceIndexReadyGeneration = 4
	candidate := server.workspaceReferencePromotionCandidateLocked()
	if candidate == nil || candidate.fingerprint != "ready" || len(candidate.counts) != 1 {
		t.Fatalf("ready workspace promotion candidate = %#v", candidate)
	}
}

func TestShutdownCancelsAndWaitsForWorkspaceIndexWorker(t *testing.T) {
	root := t.TempDir()
	var output bytes.Buffer
	server := New(nil, &output, io.Discard)
	server.settings.CacheEnabled = false
	server.settings.DebugOutput = "summary"

	started := make(chan struct{})
	cancelled := make(chan struct{})
	server.workspaceIndexTestHook = func(ctx context.Context, phase workspaceIndexTestPhase, _ uint64) {
		if phase != workspaceIndexTestPhaseStarted {
			return
		}
		close(started)
		<-ctx.Done()
		close(cancelled)
	}
	if _, rpcErr := server.handleRequest(context.Background(), "initialize", mustRaw(map[string]any{
		"rootUri": filePathURI(root),
	})); rpcErr != nil {
		t.Fatalf("initialize returned an error: %#v", rpcErr)
	}
	if err := server.handleNotification(context.Background(), "initialized", nil); err != nil {
		t.Fatal(err)
	}
	waitForWorkspaceIndexSignal(t, started)
	if _, rpcErr := server.handleRequest(context.Background(), "shutdown", nil); rpcErr != nil {
		t.Fatalf("shutdown returned an error: %#v", rpcErr)
	}
	waitForWorkspaceIndexSignal(t, cancelled)
	if logs := output.String(); !strings.Contains(logs, "workspaceIndex.stale") {
		t.Fatalf("invalidated workspace index log missing stale terminal state: %s", logs)
	}
}

func waitForWorkspaceIndexSignal(t *testing.T, signal <-chan struct{}) {
	t.Helper()
	select {
	case <-signal:
	case <-time.After(5 * time.Second):
		t.Fatal("timed out waiting for workspace index signal")
	}
}

func waitForWorkspaceIndexCompletion(t *testing.T, server *Server) {
	t.Helper()
	ctx, cancel := context.WithTimeout(context.Background(), 5*time.Second)
	defer cancel()
	if !server.waitForWorkspaceIndex(ctx) {
		t.Fatal("timed out waiting for workspace index completion")
	}
}
