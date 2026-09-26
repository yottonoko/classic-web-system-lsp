package lspserver

import (
	"context"
	"io"
	"os"
	"path/filepath"
	"reflect"
	"sort"
	"strconv"
	"strings"
	"testing"
)

func TestWorkspaceScanBoundedCollectorRetainsSortedPrefix(t *testing.T) {
	root := t.TempDir()
	for _, relative := range []string{"z.asp", "nested/c.asp", "a.asp", "nested/b.asp", "m.inc"} {
		path := filepath.Join(root, filepath.FromSlash(relative))
		if err := os.MkdirAll(filepath.Dir(path), 0o755); err != nil {
			t.Fatal(err)
		}
		if err := os.WriteFile(path, []byte("<% %>"), 0o644); err != nil {
			t.Fatal(err)
		}
	}

	var visited []string
	if !walkWorkspaceFilesWithContextProgress(context.Background(), root, func(file workspaceFile) {
		visited = append(visited, file.Relative)
	}) {
		t.Fatal("workspace walker did not complete")
	}
	if got, want := visited, []string{"a.asp", "m.inc", "nested/b.asp", "nested/c.asp", "z.asp"}; !reflect.DeepEqual(got, want) {
		t.Fatalf("workspace walker visited %v, want %v", got, want)
	}

	selectedCalls := 0
	files := scanWorkspaceFilesWithContextProgressBounded(context.Background(), root, 2, func(workspaceFile) bool {
		selectedCalls++
		return true
	})
	if selectedCalls != len(visited) {
		t.Fatalf("bounded workspace scan evaluated %d files, want %d", selectedCalls, len(visited))
	}
	got := make([]string, 0, len(files))
	for _, file := range files {
		got = append(got, file.Relative)
	}
	if want := []string{"a.asp", "m.inc"}; !reflect.DeepEqual(got, want) {
		t.Fatalf("bounded workspace scan retained %v, want %v", got, want)
	}
	if len(files) > 2 {
		t.Fatalf("bounded workspace scan retained %d files, want <= 2", len(files))
	}
}

func TestWorkspaceScanBoundedCollectorDropsPartialResultsAfterCancellation(t *testing.T) {
	root := t.TempDir()
	for index := 0; index < 8; index++ {
		path := filepath.Join(root, "file-"+strconv.Itoa(index)+".asp")
		if err := os.WriteFile(path, []byte("<% %>"), 0o644); err != nil {
			t.Fatal(err)
		}
	}
	ctx, cancel := context.WithCancel(context.Background())
	defer cancel()
	files := scanWorkspaceFilesWithContextProgressBounded(ctx, root, 4, func(workspaceFile) bool {
		cancel()
		return true
	})
	if files != nil {
		t.Fatalf("bounded workspace scan returned partial results after cancellation: %#v", files)
	}
}

func TestWorkspaceScanBoundedCollectorSkipsSymlinkedFilesAndDirectories(t *testing.T) {
	root := t.TempDir()
	outside := t.TempDir()
	inside := filepath.Join(root, "real.asp")
	outsideFile := filepath.Join(outside, "outside.asp")
	if err := os.WriteFile(inside, []byte("<% %>"), 0o644); err != nil {
		t.Fatal(err)
	}
	if err := os.WriteFile(outsideFile, []byte("<% %>"), 0o644); err != nil {
		t.Fatal(err)
	}
	if err := os.Symlink(outsideFile, filepath.Join(root, "linked.asp")); err != nil {
		t.Skipf("file symlink creation unavailable: %v", err)
	}
	if err := os.Symlink(outside, filepath.Join(root, "linked-directory")); err != nil {
		t.Skipf("directory symlink creation unavailable: %v", err)
	}

	files := scanWorkspaceFilesWithContextProgressBounded(context.Background(), root, 8, nil)
	if len(files) != 1 || filepath.Clean(files[0].Path) != filepath.Clean(inside) {
		t.Fatalf("bounded workspace scan files = %#v, want only %q", files, inside)
	}
}

func TestWorkspacePreviewGlobalLimitPreservesRootAndFileOrdering(t *testing.T) {
	roots := []string{t.TempDir(), t.TempDir()}
	sort.Strings(roots)
	for _, root := range roots {
		for _, relative := range []string{"z.asp", "a.asp", "excluded.asp", "unmatched.inc"} {
			path := filepath.Join(root, relative)
			if err := os.WriteFile(path, []byte("<% %>"), 0o644); err != nil {
				t.Fatal(err)
			}
		}
	}

	server := New(nil, io.Discard, io.Discard)
	defer server.shutdownRuntimeCaches()
	server.workspaceRoots = []workspaceRoot{
		{Path: roots[1], URI: filePathURI(roots[1])},
		{Path: roots[0], URI: filePathURI(roots[0])},
	}
	payload, ok := server.previewWorkspaceFiles(context.Background(), executeCommandParams{Arguments: []any{
		map[string]any{
			"includeGlobs":     []string{"**/*.asp"},
			"excludeGlobs":     []string{"excluded.asp"},
			"showUnmatched":    true,
			"respectGitIgnore": false,
			"maxFiles":         3,
		},
	}}).(map[string]any)
	if !ok {
		t.Fatal("workspace preview did not return a payload")
	}

	previewRoots, ok := payload["roots"].([]map[string]any)
	if !ok {
		t.Fatalf("preview roots = %#v, want []map[string]any", payload["roots"])
	}
	if len(previewRoots) != len(roots) {
		t.Fatalf("preview root count = %d, want %d", len(previewRoots), len(roots))
	}
	visible := 0
	for index, root := range previewRoots {
		if root["fileName"] != roots[index] {
			t.Fatalf("preview root[%d] = %#v, want %q", index, root["fileName"], roots[index])
		}
		files, ok := root["files"].([]map[string]any)
		if !ok {
			t.Fatalf("preview root[%d] files = %#v, want []map[string]any", index, root["files"])
		}
		visible += len(files)
		for fileIndex := 1; fileIndex < len(files); fileIndex++ {
			previous := files[fileIndex-1]["relativePath"].(string)
			current := files[fileIndex]["relativePath"].(string)
			if previous > current {
				t.Fatalf("preview root[%d] files are not ordered: %#v", index, files)
			}
		}
	}
	if visible != 3 {
		t.Fatalf("preview visible file count = %d, want 3", visible)
	}
	if truncated, ok := payload["truncated"].(map[string]any); !ok || truncated["reason"] != "files>3" {
		t.Fatalf("preview truncation = %#v, want files>3", payload["truncated"])
	}
	stats, ok := payload["stats"].(map[string]any)
	if !ok || stats["files"] != 4 || stats["totalBytes"] != int64(4*len("<% %>")) {
		t.Fatalf("preview stats = %#v, want four matching files and %d total bytes", payload["stats"], 4*len("<% %>"))
	}
}

func TestWorkspacePreviewBoundsInputLimits(t *testing.T) {
	root := t.TempDir()
	server := New(nil, io.Discard, io.Discard)
	defer server.shutdownRuntimeCaches()
	server.workspaceRoots = []workspaceRoot{{Path: root, URI: filePathURI(root)}}

	patterns := make([]string, maxWorkspacePreviewGlobs+1)
	for index := range patterns {
		patterns[index] = strings.Repeat("x", maxWorkspacePreviewGlobLength+1)
	}
	payload, ok := server.previewWorkspaceFiles(context.Background(), executeCommandParams{Arguments: []any{
		map[string]any{
			"includeGlobs": patterns,
			"excludeGlobs": patterns,
			"maxFiles":     int(^uint(0) >> 1),
		},
	}}).(map[string]any)
	if !ok {
		t.Fatal("workspace preview did not return a payload")
	}
	for _, key := range []string{"includeGlobs", "excludeGlobs"} {
		globs, ok := payload[key].([]string)
		if !ok {
			t.Fatalf("%s = %#v, want []string", key, payload[key])
		}
		if len(globs) != maxWorkspacePreviewGlobs {
			t.Fatalf("%s count = %d, want %d", key, len(globs), maxWorkspacePreviewGlobs)
		}
		for index, glob := range globs {
			if len(glob) > maxWorkspacePreviewGlobLength {
				t.Fatalf("%s[%d] length = %d, want <= %d", key, index, len(glob), maxWorkspacePreviewGlobLength)
			}
		}
	}
}

func TestWorkspacePreviewBoundsInvalidMaxFiles(t *testing.T) {
	for _, value := range []int{0, -1} {
		if got := boundedWorkspacePreviewMaxFiles(&value); got != defaultWorkspacePreviewMaxFiles {
			t.Fatalf("boundedWorkspacePreviewMaxFiles(%d) = %d, want default %d", value, got, defaultWorkspacePreviewMaxFiles)
		}
	}
	if got := boundedWorkspacePreviewMaxFiles(nil); got != defaultWorkspacePreviewMaxFiles {
		t.Fatalf("boundedWorkspacePreviewMaxFiles(nil) = %d, want default %d", got, defaultWorkspacePreviewMaxFiles)
	}
	value := maxWorkspacePreviewFiles + 1
	if got := boundedWorkspacePreviewMaxFiles(&value); got != maxWorkspacePreviewFiles {
		t.Fatalf("boundedWorkspacePreviewMaxFiles(%d) = %d, want maximum %d", value, got, maxWorkspacePreviewFiles)
	}
}
