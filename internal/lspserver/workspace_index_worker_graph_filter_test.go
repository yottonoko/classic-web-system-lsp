package lspserver

import (
	"context"
	"io"
	"os"
	"path/filepath"
	"sort"
	"strings"
	"sync"
	"sync/atomic"
	"testing"

	"github.com/yottonoko/classic-web-system-lsp/internal/core"
)

func TestWorkspaceIndexGraphGenerationDriftRetriesGraphOnly(t *testing.T) {
	root := t.TempDir()
	for _, name := range []string{"first.asp", "second.asp"} {
		if err := os.WriteFile(filepath.Join(root, name), []byte(`<% Dim Value %>`), 0o600); err != nil {
			t.Fatal(err)
		}
	}
	var output strings.Builder
	server := New(nil, &output, io.Discard)
	server.settings.CacheEnabled = false
	server.rootPath = root
	server.rootURI = filePathURI(root)
	server.workspaceRoots = []workspaceRoot{{Path: root, URI: server.rootURI}}
	t.Cleanup(server.stopWorkspaceIndexWorkers)

	var rootScans atomic.Int32
	server.workspaceWalkDirTestHook = func(path string) error {
		if filepath.Clean(path) == filepath.Clean(root) {
			rootScans.Add(1)
		}
		return nil
	}
	var driftOnce sync.Once
	server.workspaceIncludeGraphDocumentTestHook = func(context.Context, string) {
		driftOnce.Do(func() {
			server.mu.Lock()
			server.graphGeneration++
			server.mu.Unlock()
		})
	}
	server.activateWorkspaceIndexing(context.Background())
	server.scheduleWorkspaceIndex("test.graphGenerationDrift")
	waitForWorkspaceIndexCompletion(t, server)

	server.mu.Lock()
	workspaceGeneration := server.workspaceIndexGeneration
	graphComplete := server.workspaceIncludeGraphComplete
	graphSize := server.workspaceIncludeGraph.Size()
	workspaceSize := len(server.workspace)
	server.mu.Unlock()
	if workspaceGeneration != 1 {
		t.Fatalf("graph-generation retry advanced workspace generation to %d, want one generation", workspaceGeneration)
	}
	if rootScans.Load() != 1 {
		t.Fatalf("graph-generation retry scanned workspace root %d times, want once", rootScans.Load())
	}
	if !graphComplete || graphSize != 2 || workspaceSize != 2 {
		t.Fatalf("graph-generation retry state = graphComplete:%t graphSize:%d workspaceSize:%d; want complete graph/workspace size 2", graphComplete, graphSize, workspaceSize)
	}
	if strings.Contains(output.String(), "workspace.includeGraph.retry") {
		t.Fatalf("graph-generation retry emitted obsolete reschedule reason: %s", output.String())
	}
}

func TestWorkspaceIndexDiscoveryFiltersBeforeProgressAndRead(t *testing.T) {
	root := t.TempDir()
	includedPath := filepath.Join(root, "included.asp")
	excludedPath := filepath.Join(root, "excluded.asp")
	for _, path := range []string{includedPath, excludedPath} {
		if err := os.WriteFile(path, []byte(`<% Dim Value %>`), 0o600); err != nil {
			t.Fatal(err)
		}
	}
	server := New(nil, io.Discard, io.Discard)
	defer server.shutdownRuntimeCaches()
	server.workspaceIndexGeneration = 1
	var progressMu sync.Mutex
	var discovered []string
	server.workspaceFileReadTestHook = func(path string) {
		if filepath.Clean(path) == filepath.Clean(excludedPath) {
			t.Errorf("excluded file was read: %s", path)
		}
	}
	result := server.buildWorkspaceIndexWithProgress(context.Background(), 1, workspaceIndexRunSettings{
		roots:        []workspaceRoot{{Path: root, URI: filePathURI(root)}},
		excludeGlobs: []string{"excluded.asp"},
	}, func(label string, _ int, _ int, detail string) {
		if label != "workspace.index.scanRoot" || detail == filepath.Base(root) {
			return
		}
		progressMu.Lock()
		discovered = append(discovered, detail)
		progressMu.Unlock()
	})
	if !result.complete || result.err != nil {
		t.Fatalf("filtered workspace index result = %#v; want complete success", result)
	}
	progressMu.Lock()
	sort.Strings(discovered)
	progressMu.Unlock()
	if len(discovered) != 1 || discovered[0] != "included.asp" {
		t.Fatalf("discovered files = %#v, want only included.asp", discovered)
	}
	if len(result.documents) != 1 || result.documents[filePathURI(includedPath)] == nil {
		t.Fatalf("filtered workspace documents = %#v, want only included.asp", result.documents)
	}
}

func TestWorkspaceIndexDiscoveryPrunesSafelyExcludedDirectory(t *testing.T) {
	root := t.TempDir()
	keptPath := filepath.Join(root, "kept.asp")
	prunedPath := filepath.Join(root, "excluded", "nested.asp")
	if err := os.MkdirAll(filepath.Dir(prunedPath), 0o755); err != nil {
		t.Fatal(err)
	}
	for _, path := range []string{keptPath, prunedPath} {
		if err := os.WriteFile(path, []byte(`<% Dim Value %>`), 0o600); err != nil {
			t.Fatal(err)
		}
	}
	server := New(nil, io.Discard, io.Discard)
	defer server.shutdownRuntimeCaches()
	server.workspaceIndexGeneration = 1
	var visitedMu sync.Mutex
	var visited []string
	server.workspaceWalkDirTestHook = func(path string) error {
		visitedMu.Lock()
		visited = append(visited, filepath.Clean(path))
		visitedMu.Unlock()
		return nil
	}
	documents, _, complete := server.buildWorkspaceIndex(context.Background(), 1, workspaceIndexRunSettings{
		roots:        []workspaceRoot{{Path: root, URI: filePathURI(root)}},
		excludeGlobs: []string{"excluded/**"},
	})
	if !complete {
		t.Fatal("pruned workspace index did not complete")
	}
	visitedMu.Lock()
	visitedNested := false
	for _, path := range visited {
		if path == filepath.Clean(prunedPath) {
			visitedNested = true
			break
		}
	}
	visitedMu.Unlock()
	if visitedNested {
		t.Fatalf("safely excluded directory was descended into: visited=%#v", visited)
	}
	if len(documents) != 1 || documents[filePathURI(keptPath)] == nil {
		t.Fatalf("pruned workspace documents = %#v, want only kept.asp", documents)
	}
}

func TestWorkspaceIndexDiscoveryKeepsNegatedGitIgnoreDescendant(t *testing.T) {
	root := t.TempDir()
	keepPath := filepath.Join(root, "ignored", "keep.asp")
	dropPath := filepath.Join(root, "ignored", "drop.asp")
	if err := os.MkdirAll(filepath.Dir(keepPath), 0o755); err != nil {
		t.Fatal(err)
	}
	for _, path := range []string{keepPath, dropPath} {
		if err := os.WriteFile(path, []byte(`<% Dim Value %>`), 0o600); err != nil {
			t.Fatal(err)
		}
	}
	if err := os.WriteFile(filepath.Join(root, ".gitignore"), []byte("ignored/\n!ignored/keep.asp\n"), 0o600); err != nil {
		t.Fatal(err)
	}
	server := New(nil, io.Discard, io.Discard)
	defer server.shutdownRuntimeCaches()
	server.workspaceIndexGeneration = 1
	var readsMu sync.Mutex
	var reads []string
	server.workspaceFileReadTestHook = func(path string) {
		readsMu.Lock()
		reads = append(reads, filepath.Clean(path))
		readsMu.Unlock()
	}
	documents, _, complete := server.buildWorkspaceIndex(context.Background(), 1, workspaceIndexRunSettings{
		roots:            []workspaceRoot{{Path: root, URI: filePathURI(root)}},
		respectGitIgnore: true,
	})
	if !complete {
		t.Fatal("negated gitignore workspace index did not complete")
	}
	if len(documents) != 1 || documents[filePathURI(keepPath)] == nil {
		t.Fatalf("negated gitignore workspace documents = %#v, want only keep.asp", documents)
	}
	readsMu.Lock()
	defer readsMu.Unlock()
	for _, path := range reads {
		if path == filepath.Clean(dropPath) {
			t.Fatalf("ignored descendant was read: %s", path)
		}
	}
}

func TestWorkspaceIndexDiscoveryPreservesDirectIncludeTargets(t *testing.T) {
	root := t.TempDir()
	ownerPath := filepath.Join(root, "owner.asp")
	targetPath := filepath.Join(root, "excluded", "target.inc")
	if err := os.MkdirAll(filepath.Dir(targetPath), 0o755); err != nil {
		t.Fatal(err)
	}
	if err := os.WriteFile(ownerPath, []byte(`<!-- #include file="excluded/target.inc" -->`), 0o600); err != nil {
		t.Fatal(err)
	}
	if err := os.WriteFile(targetPath, []byte(`<% Dim IncludedValue %>`), 0o600); err != nil {
		t.Fatal(err)
	}
	server := New(nil, io.Discard, io.Discard)
	defer server.shutdownRuntimeCaches()
	server.settings.CacheEnabled = false
	server.rootPath = root
	server.rootURI = filePathURI(root)
	server.workspaceRoots = []workspaceRoot{{Path: root, URI: server.rootURI}}
	server.configureFsGateway()
	server.workspaceIndexGeneration = 1
	var targetReads atomic.Int32
	server.workspaceFileReadTestHook = func(path string) {
		if filepath.Clean(path) == filepath.Clean(targetPath) {
			targetReads.Add(1)
		}
	}
	documents, _, complete := server.buildWorkspaceIndex(context.Background(), 1, workspaceIndexRunSettings{
		roots:        []workspaceRoot{{Path: root, URI: server.rootURI}},
		excludeGlobs: []string{"excluded/**"},
	})
	if !complete {
		t.Fatal("direct-include workspace index did not complete")
	}
	if len(documents) != 1 || documents[filePathURI(ownerPath)] == nil {
		t.Fatalf("direct-include workspace documents = %#v, want only owner.asp", documents)
	}
	owner := core.ParseDocument(filePathURI(ownerPath), documents[filePathURI(ownerPath)].Text, core.Settings{DefaultLanguage: server.settings.DefaultLanguage})
	included, expanded := server.includedDocumentsContextResult(context.Background(), owner)
	if !expanded || len(included) != 1 || !strings.EqualFold(fileURIPath(included[0].URI), targetPath) {
		t.Fatalf("direct-include expansion = expanded:%t documents:%#v, want excluded target", expanded, included)
	}
	if targetReads.Load() == 0 {
		t.Fatal("direct include target was not read on demand after discovery filtering")
	}
}

func TestWorkspaceIndexDiscoveryPrunesCacheDirectoryOnlyBelowRoot(t *testing.T) {
	t.Run("strict descendant", func(t *testing.T) {
		root := t.TempDir()
		cacheFile := filepath.Join(root, "cache", "hidden.asp")
		visibleFile := filepath.Join(root, "visible.asp")
		if err := os.MkdirAll(filepath.Dir(cacheFile), 0o755); err != nil {
			t.Fatal(err)
		}
		for _, path := range []string{cacheFile, visibleFile} {
			if err := os.WriteFile(path, []byte(`<% Dim Value %>`), 0o600); err != nil {
				t.Fatal(err)
			}
		}
		server := New(nil, io.Discard, io.Discard)
		defer server.shutdownRuntimeCaches()
		server.workspaceIndexGeneration = 1
		documents, _, complete := server.buildWorkspaceIndex(context.Background(), 1, workspaceIndexRunSettings{
			roots:          []workspaceRoot{{Path: root, URI: filePathURI(root)}},
			cacheDirectory: filepath.Join(root, "cache"),
		})
		if !complete {
			t.Fatal("cache-pruned workspace index did not complete")
		}
		if len(documents) != 1 || documents[filePathURI(visibleFile)] == nil {
			t.Fatalf("cache-pruned workspace documents = %#v, want only visible.asp", documents)
		}
	})

	t.Run("root itself", func(t *testing.T) {
		root := t.TempDir()
		file := filepath.Join(root, "visible.asp")
		if err := os.WriteFile(file, []byte(`<% Dim Value %>`), 0o600); err != nil {
			t.Fatal(err)
		}
		server := New(nil, io.Discard, io.Discard)
		defer server.shutdownRuntimeCaches()
		server.workspaceIndexGeneration = 1
		documents, _, complete := server.buildWorkspaceIndex(context.Background(), 1, workspaceIndexRunSettings{
			roots:          []workspaceRoot{{Path: root, URI: filePathURI(root)}},
			cacheDirectory: root,
		})
		if !complete || len(documents) != 1 || documents[filePathURI(file)] == nil {
			t.Fatalf("root cache-directory workspace index result = complete:%t documents:%#v; want root file", complete, documents)
		}
	})
}
