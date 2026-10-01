package lspserver

import (
	"context"
	"io"
	"os"
	"path/filepath"
	"slices"
	"strings"
	"sync"
	"sync/atomic"
	"testing"
)

func newGitIgnoreCacheTestServer(t *testing.T) (*Server, string, *atomic.Int32) {
	t.Helper()
	root := t.TempDir()
	if err := os.MkdirAll(filepath.Join(root, "nested"), 0o755); err != nil {
		t.Fatal(err)
	}
	for path, text := range map[string]string{".gitignore": "first.asp\n", filepath.Join("nested", ".gitignore"): "local/\n"} {
		if err := os.WriteFile(filepath.Join(root, path), []byte(text), 0o644); err != nil {
			t.Fatal(err)
		}
	}
	server := New(strings.NewReader(""), io.Discard, io.Discard)
	server.rootPath = root
	server.rootURI = filePathURI(root)
	server.workspaceRoots = []workspaceRoot{{Path: root, URI: filePathURI(root)}}
	server.settings.WorkspaceRespectGitIgnore = true
	reads := &atomic.Int32{}
	server.workspaceFileReadTestHook = func(path string) {
		if filepath.Base(path) == ".gitignore" {
			reads.Add(1)
		}
	}
	return server, root, reads
}

func TestGitIgnoreGlobsAreScannedOnceUntilAGitIgnoreChanges(t *testing.T) {
	server, root, reads := newGitIgnoreCacheTestServer(t)
	first := server.readGitIgnoreGlobs(root)
	if got := reads.Load(); got != 2 || !slices.Contains(first, "first.asp") {
		t.Fatalf("first scan read %d files and returned %v", got, first)
	}
	for range 5 {
		server.readGitIgnoreGlobs(root)
	}
	if got := reads.Load(); got != 2 {
		t.Fatalf("cached rules were rescanned: %d reads", got)
	}

	ignorePath := filepath.Join(root, ".gitignore")
	if err := os.WriteFile(ignorePath, []byte("second.asp\n"), 0o644); err != nil {
		t.Fatal(err)
	}
	if err := server.didChangeWatchedFiles(didChangeWatchedFilesParams{Changes: []fileEvent{{URI: filePathURI(ignorePath), Type: fileChangeChanged}}}); err != nil {
		t.Fatal(err)
	}
	second := server.readGitIgnoreGlobs(root)
	if slices.Contains(second, "first.asp") || !slices.Contains(second, "second.asp") {
		t.Fatalf("rules after a .gitignore change = %v", second)
	}
}

func TestGitIgnoreGlobsShareConcurrentScansAndSkipCancelledOnes(t *testing.T) {
	server, root, reads := newGitIgnoreCacheTestServer(t)
	cancelled, cancel := context.WithCancel(t.Context())
	cancel()
	server.readGitIgnoreGlobsContext(cancelled, root)
	if got := reads.Load(); got != 0 {
		t.Fatalf("cancelled scan read %d files", got)
	}

	var wait sync.WaitGroup
	for range 8 {
		wait.Go(func() {
			if globs := server.readGitIgnoreGlobs(root); !slices.Contains(globs, "first.asp") {
				t.Errorf("concurrent reader got %v", globs)
			}
		})
	}
	wait.Wait()
	// Late readers may start after the first scan was stored; either way each
	// .gitignore is read by one scan only.
	if got := reads.Load(); got != 2 {
		t.Fatalf("concurrent readers read .gitignore files %d times, want 2", got)
	}
}
