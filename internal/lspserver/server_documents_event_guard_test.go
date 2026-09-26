package lspserver

import (
	"context"
	"io"
	"os"
	"path/filepath"
	"strings"
	"sync"
	"sync/atomic"
	"testing"

	"github.com/yottonoko/classic-web-system-lsp/internal/core"
)

func newWorkspaceEventGuardServer(t *testing.T, root string) *Server {
	t.Helper()
	server := New(strings.NewReader(""), io.Discard, io.Discard)
	server.rootPath = root
	server.rootURI = filePathURI(root)
	server.workspaceRoots = []workspaceRoot{{URI: server.rootURI, Path: root}}
	server.workspaceIndexEnabled = true
	server.workspaceIndexGeneration = 7
	server.settings.CacheDirectory = filepath.Join(root, ".asp-lsp-cache")
	server.configureFsGateway()
	t.Cleanup(func() {
		server.stopWorkspaceIndexWorkers()
		server.waitForAsyncDiskCacheWrites()
		server.closeDiskAnalysisCache()
	})
	return server
}

func TestDidChangeWorkspaceFoldersEquivalentEventsDoNotReindex(t *testing.T) {
	rootA := t.TempDir()
	rootB := t.TempDir()
	server := New(strings.NewReader(""), io.Discard, io.Discard)
	server.rootPath = rootA
	server.rootURI = filePathURI(rootA)
	server.workspaceRoots = []workspaceRoot{
		{URI: filePathURI(rootA), Path: rootA},
		{URI: filePathURI(rootB), Path: rootB},
	}
	server.workspaceIndexEnabled = true
	server.workspaceIndexGeneration = 7

	server.didChangeWorkspaceFolders(didChangeWorkspaceFoldersParams{})
	if server.workspaceIndexGeneration != 7 {
		t.Fatalf("empty folder event advanced workspace generation to %d", server.workspaceIndexGeneration)
	}

	server.didChangeWorkspaceFolders(didChangeWorkspaceFoldersParams{Event: struct {
		Added   []workspaceFolderEvent `json:"added"`
		Removed []workspaceFolderEvent `json:"removed"`
	}{
		Added:   []workspaceFolderEvent{{URI: filePathURI(rootA)}},
		Removed: []workspaceFolderEvent{{URI: filePathURI(rootA)}},
	}})
	if server.workspaceIndexGeneration != 7 {
		t.Fatalf("equivalent remove/add advanced workspace generation to %d", server.workspaceIndexGeneration)
	}
	if len(server.workspaceRoots) != 2 || server.workspaceRoots[0].Path != rootA || server.workspaceRoots[1].Path != rootB {
		t.Fatalf("equivalent folder event changed roots: %#v", server.workspaceRoots)
	}
}

func TestFileOperationsDoNotReindexOutsideExcludedOrIrrelevantPaths(t *testing.T) {
	root := t.TempDir()
	excluded := filepath.Join(root, "excluded")
	if err := os.MkdirAll(excluded, 0o755); err != nil {
		t.Fatal(err)
	}
	if err := os.WriteFile(filepath.Join(root, ".gitignore"), []byte("ignored-by-test/\n"), 0o644); err != nil {
		t.Fatal(err)
	}
	for _, testCase := range []struct {
		name string
		path string
	}{
		{name: "outside", path: filepath.Join(t.TempDir(), "outside.txt")},
		{name: "excluded", path: filepath.Join(excluded, "ignored.txt")},
		{name: "irrelevant", path: filepath.Join(root, "notes.txt")},
	} {
		t.Run(testCase.name, func(t *testing.T) {
			server := newWorkspaceEventGuardServer(t, root)
			server.settings.WorkspaceExcludeGlobs = []string{"excluded/**"}
			server.settings.WorkspaceRespectGitIgnore = true
			fsGeneration := server.fsGateway.Generation()
			var gitIgnoreReads atomic.Int32
			server.workspaceFileReadTestHook = func(path string) {
				if filepath.Base(path) == ".gitignore" {
					gitIgnoreReads.Add(1)
				}
			}
			if err := os.WriteFile(testCase.path, []byte("not an index source"), 0o644); err != nil {
				t.Fatal(err)
			}
			if err := server.didFileOperations(didFileOperationParams{Files: []struct {
				URI string `json:"uri"`
			}{{URI: filePathURI(testCase.path)}}}, fileChangeCreated); err != nil {
				t.Fatal(err)
			}
			if server.workspaceIndexGeneration != 7 {
				t.Fatalf("%s create advanced workspace generation to %d", testCase.name, server.workspaceIndexGeneration)
			}
			if got := server.fsGateway.Generation(); got != fsGeneration {
				t.Fatalf("%s create advanced filesystem generation from %d to %d", testCase.name, fsGeneration, got)
			}
			if got := gitIgnoreReads.Load(); got != 0 {
				t.Fatalf("%s create reread .gitignore %d times", testCase.name, got)
			}
		})
	}
}

func TestCacheDirectoryWatchersDoNotInvalidateASPOrJavaScriptState(t *testing.T) {
	root := t.TempDir()
	cacheDirectory := filepath.Join(root, ".asp-lsp-cache")
	if err := os.MkdirAll(cacheDirectory, 0o755); err != nil {
		t.Fatal(err)
	}
	server := newWorkspaceEventGuardServer(t, root)
	server.settings.CacheDirectory = cacheDirectory
	server.javascriptDocumentGeneration = 5
	preparation := &javaScriptProjectPreparation{}
	server.javascriptPreparation = preparation
	for _, testCase := range []struct {
		name string
		path string
	}{
		{name: "asp", path: filepath.Join(cacheDirectory, "generated.asp")},
		{name: "javascript", path: filepath.Join(cacheDirectory, "generated.js")},
	} {
		t.Run(testCase.name, func(t *testing.T) {
			if err := os.WriteFile(testCase.path, []byte("<script>const generated = 1;</script>\n"), 0o600); err != nil {
				t.Fatal(err)
			}
			fsGeneration := server.fsGateway.Generation()
			if err := server.didChangeWatchedFiles(didChangeWatchedFilesParams{Changes: []fileEvent{{URI: filePathURI(testCase.path), Type: fileChangeChanged}}}); err != nil {
				t.Fatal(err)
			}
			if got := server.fsGateway.Generation(); got != fsGeneration {
				t.Fatalf("cache %s watcher advanced filesystem generation from %d to %d", testCase.name, fsGeneration, got)
			}
			if server.javascriptDocumentGeneration != 5 {
				t.Fatalf("cache %s watcher advanced JavaScript document generation to %d", testCase.name, server.javascriptDocumentGeneration)
			}
			if server.javascriptPreparation != preparation {
				t.Fatalf("cache %s watcher invalidated JavaScript preparation", testCase.name)
			}
		})
	}
}

func TestFileOperationDirectoryReindexesOnce(t *testing.T) {
	root := t.TempDir()
	server := newWorkspaceEventGuardServer(t, root)
	directory := filepath.Join(root, "new-directory")
	if err := os.Mkdir(directory, 0o755); err != nil {
		t.Fatal(err)
	}
	var starts atomic.Int32
	started := make(chan struct{})
	var startedOnce sync.Once
	server.workspaceIndexTestHook = func(_ context.Context, phase workspaceIndexTestPhase, _ uint64) {
		if phase == workspaceIndexTestPhaseStarted {
			starts.Add(1)
			startedOnce.Do(func() { close(started) })
		}
	}
	if err := server.didFileOperations(didFileOperationParams{Files: []struct {
		URI string `json:"uri"`
	}{{URI: filePathURI(directory)}}}, fileChangeCreated); err != nil {
		t.Fatal(err)
	}
	waitForWorkspaceIndexSignal(t, started)
	waitForWorkspaceIndexCompletion(t, server)
	if server.workspaceIndexGeneration != 8 {
		t.Fatalf("directory create advanced workspace generation to %d, want 8", server.workspaceIndexGeneration)
	}
	if got := starts.Load(); got != 1 {
		t.Fatalf("directory create started %d workspace indexes, want 1", got)
	}
}

func TestGitIgnoreFileOperationReindexesOnce(t *testing.T) {
	for _, testCase := range []struct {
		name string
		call func(*Server, string) error
	}{
		{
			name: "watcher",
			call: func(server *Server, uri string) error {
				return server.didChangeWatchedFiles(didChangeWatchedFilesParams{Changes: []fileEvent{{URI: uri, Type: fileChangeChanged}}})
			},
		},
		{
			name: "create operation",
			call: func(server *Server, uri string) error {
				return server.didFileOperations(didFileOperationParams{Files: []struct {
					URI string `json:"uri"`
				}{{URI: uri}}}, fileChangeCreated)
			},
		},
	} {
		t.Run(testCase.name, func(t *testing.T) {
			root := t.TempDir()
			path := filepath.Join(root, ".gitignore")
			if err := os.WriteFile(path, []byte("ignored.asp\n"), 0o644); err != nil {
				t.Fatal(err)
			}
			server := newWorkspaceEventGuardServer(t, root)
			server.settings.WorkspaceRespectGitIgnore = true
			var starts atomic.Int32
			started := make(chan struct{})
			var startedOnce sync.Once
			server.workspaceIndexTestHook = func(_ context.Context, phase workspaceIndexTestPhase, _ uint64) {
				if phase == workspaceIndexTestPhaseStarted {
					starts.Add(1)
					startedOnce.Do(func() { close(started) })
				}
			}
			if err := testCase.call(server, filePathURI(path)); err != nil {
				t.Fatal(err)
			}
			waitForWorkspaceIndexSignal(t, started)
			waitForWorkspaceIndexCompletion(t, server)
			if server.workspaceIndexGeneration != 8 {
				t.Fatalf("gitignore %s advanced workspace generation to %d, want 8", testCase.name, server.workspaceIndexGeneration)
			}
			if got := starts.Load(); got != 1 {
				t.Fatalf("gitignore %s started %d workspace indexes, want 1", testCase.name, got)
			}
		})
	}
}

func TestGitIgnoreWatcherDoesNotInvalidateWhenRespectIsDisabled(t *testing.T) {
	root := t.TempDir()
	server := newWorkspaceEventGuardServer(t, root)
	server.settings.WorkspaceRespectGitIgnore = false
	gitIgnorePath := filepath.Join(root, ".gitignore")
	if err := os.WriteFile(gitIgnorePath, []byte("generated/\n"), 0o600); err != nil {
		t.Fatal(err)
	}
	fsGeneration := server.fsGateway.Generation()
	if err := server.didChangeWatchedFiles(didChangeWatchedFilesParams{Changes: []fileEvent{{
		URI: filePathURI(gitIgnorePath), Type: fileChangeChanged,
	}}}); err != nil {
		t.Fatal(err)
	}
	if got := server.fsGateway.Generation(); got != fsGeneration {
		t.Fatalf("disabled .gitignore watcher advanced filesystem generation from %d to %d", fsGeneration, got)
	}
}

func TestExcludedJavaScriptWatcherDoesNotInvalidateProject(t *testing.T) {
	root := t.TempDir()
	excluded := filepath.Join(root, "excluded")
	if err := os.MkdirAll(excluded, 0o755); err != nil {
		t.Fatal(err)
	}
	server := New(strings.NewReader(""), io.Discard, io.Discard)
	server.rootPath = root
	server.rootURI = filePathURI(root)
	server.workspaceRoots = []workspaceRoot{{URI: server.rootURI, Path: root}}
	server.settings.WorkspaceExcludeGlobs = []string{"excluded/**"}
	server.javascriptDocumentGeneration = 5
	preparation := &javaScriptProjectPreparation{}
	server.javascriptPreparation = preparation
	path := filepath.Join(excluded, "ignored.js")
	if err := os.WriteFile(path, []byte("export const ignored = true;\n"), 0o644); err != nil {
		t.Fatal(err)
	}
	if err := server.didChangeWatchedFiles(didChangeWatchedFilesParams{Changes: []fileEvent{{URI: filePathURI(path), Type: fileChangeCreated}}}); err != nil {
		t.Fatal(err)
	}
	if server.javascriptDocumentGeneration != 5 {
		t.Fatalf("excluded JavaScript watcher advanced document generation to %d", server.javascriptDocumentGeneration)
	}
	if server.javascriptPreparation != preparation {
		t.Fatal("excluded JavaScript watcher invalidated project preparation")
	}
}

func TestDidCloseUnchangedJavaScriptOverlayPreservesProject(t *testing.T) {
	root := t.TempDir()
	uri := filePathURI(filepath.Join(root, "page.asp"))
	text := "<script>const unchanged = 1;</script>"
	server := New(strings.NewReader(""), io.Discard, io.Discard)
	server.rootPath = root
	server.rootURI = filePathURI(root)
	server.workspaceRoots = []workspaceRoot{{URI: server.rootURI, Path: root}}
	server.workspace[uri] = core.NewTextDocument(uri, "classic-asp", 0, text)
	server.documents[uri] = core.NewTextDocument(uri, "classic-asp", 1, text)
	server.javascriptDocumentGeneration = 5
	preparation := &javaScriptProjectPreparation{}
	server.javascriptPreparation = preparation
	if err := server.handleNotification(t.Context(), "textDocument/didClose", mustRaw(map[string]any{
		"textDocument": map[string]any{"uri": uri},
	})); err != nil {
		t.Fatal(err)
	}
	if server.javascriptDocumentGeneration != 5 {
		t.Fatalf("unchanged JavaScript overlay close advanced document generation to %d", server.javascriptDocumentGeneration)
	}
	if server.javascriptPreparation != preparation {
		t.Fatal("unchanged JavaScript overlay close invalidated project preparation")
	}
}

func TestDidCloseChangedJavaScriptOverlayInvalidatesProject(t *testing.T) {
	root := t.TempDir()
	uri := filePathURI(filepath.Join(root, "page.asp"))
	server := New(strings.NewReader(""), io.Discard, io.Discard)
	server.rootPath = root
	server.rootURI = filePathURI(root)
	server.workspaceRoots = []workspaceRoot{{URI: server.rootURI, Path: root}}
	server.workspace[uri] = core.NewTextDocument(uri, "classic-asp", 0, "<script>const backing = 1;</script>")
	server.documents[uri] = core.NewTextDocument(uri, "classic-asp", 1, "<script>const overlay = 2;</script>")
	server.javascriptDocumentGeneration = 5
	if err := server.handleNotification(t.Context(), "textDocument/didClose", mustRaw(map[string]any{
		"textDocument": map[string]any{"uri": uri},
	})); err != nil {
		t.Fatal(err)
	}
	if server.javascriptDocumentGeneration <= 5 {
		t.Fatalf("changed JavaScript overlay close left document generation at %d", server.javascriptDocumentGeneration)
	}
}
