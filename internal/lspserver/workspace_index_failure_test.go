package lspserver

import (
	"bytes"
	"context"
	"errors"
	"io"
	"os"
	"path/filepath"
	"strings"
	"sync/atomic"
	"testing"
	"time"

	"github.com/yottonoko/classic-web-system-lsp/internal/core"
)

func TestWorkspaceIndexBuildResultRetainsScanAndReadErrors(t *testing.T) {
	t.Run("scan", func(t *testing.T) {
		root := t.TempDir()
		sourcePath := filepath.Join(root, "source.asp")
		if err := os.WriteFile(sourcePath, []byte(`<% Dim Value %>`), 0o600); err != nil {
			t.Fatal(err)
		}
		walkErr := errors.New("workspace index walk failure")
		server := New(nil, io.Discard, io.Discard)
		server.workspaceIndexGeneration = 1
		server.workspaceWalkDirTestHook = func(path string) error {
			if filepath.Clean(path) == filepath.Clean(sourcePath) {
				return walkErr
			}
			return nil
		}

		result := server.buildWorkspaceIndexWithProgress(context.Background(), 1, workspaceIndexRunSettings{
			roots: []workspaceRoot{{Path: root, URI: filePathURI(root)}},
		}, nil)
		if result.complete || result.documents != nil || !errors.Is(result.err, walkErr) {
			t.Fatalf("scan result = %#v; want incomplete result retaining walk error", result)
		}
	})

	t.Run("read", func(t *testing.T) {
		root := t.TempDir()
		sourcePath := filepath.Join(root, "source.asp")
		movedPath := sourcePath + ".moved"
		if err := os.WriteFile(sourcePath, []byte(`<% Dim Value %>`), 0o600); err != nil {
			t.Fatal(err)
		}
		server := New(nil, io.Discard, io.Discard)
		server.workspaceIndexGeneration = 1
		server.workspaceFileReadTestHook = func(path string) {
			if filepath.Clean(path) != filepath.Clean(sourcePath) {
				return
			}
			if err := os.Rename(sourcePath, movedPath); err != nil {
				t.Errorf("rename source for injected read failure: %v", err)
			}
		}
		defer func() {
			if _, err := os.Stat(movedPath); err == nil {
				if renameErr := os.Rename(movedPath, sourcePath); renameErr != nil {
					t.Errorf("restore source after injected read failure: %v", renameErr)
				}
			}
		}()

		result := server.buildWorkspaceIndexWithProgress(context.Background(), 1, workspaceIndexRunSettings{
			roots: []workspaceRoot{{Path: root, URI: filePathURI(root)}},
		}, nil)
		if result.complete || result.documents != nil || !errors.Is(result.err, os.ErrNotExist) {
			t.Fatalf("read result = %#v; want incomplete result retaining read error", result)
		}
	})
}

func TestWorkspaceIndexWorkerFailurePublishesCauseWithoutPartialStateAndRecovers(t *testing.T) {
	root := t.TempDir()
	sourcePath := filepath.Join(root, "source.asp")
	uri := filePathURI(sourcePath)
	if err := os.WriteFile(sourcePath, []byte(`<% Dim Value %>`), 0o600); err != nil {
		t.Fatal(err)
	}
	var output bytes.Buffer
	server := New(nil, &output, io.Discard)
	server.settings.CacheEnabled = false
	server.workspaceIndexEnabled = true
	server.workspaceIndexGeneration = 1
	server.rootPath = root
	server.rootURI = filePathURI(root)
	server.workspaceRoots = []workspaceRoot{{Path: root, URI: filePathURI(root)}}
	server.workspace = map[string]*core.TextDocument{
		"file:///workspace/previous.asp": core.NewTextDocument("file:///workspace/previous.asp", "classic-asp", 0, `<% Dim Previous %>`),
	}
	walkErr := errors.New("deterministic workspace scan failure")
	server.workspaceWalkDirTestHook = func(path string) error {
		if filepath.Clean(path) == filepath.Clean(sourcePath) {
			return walkErr
		}
		return nil
	}

	runWorkspaceIndexFailureTestWorker(t, server, 1, "test.scanFailure")
	if _, ok := server.workspace[uri]; ok {
		t.Fatal("failed workspace index published a scanned document")
	}
	if _, ok := server.workspace["file:///workspace/previous.asp"]; !ok {
		t.Fatal("failed workspace index removed the previous published document")
	}
	server.mu.Lock()
	readyGeneration := server.workspaceReferenceIndexReadyGeneration
	server.mu.Unlock()
	if readyGeneration == 1 {
		t.Fatal("failed workspace index published reference readiness")
	}
	logged := output.String()
	if !strings.Contains(logged, "workspace.index.failed") || !strings.Contains(logged, walkErr.Error()) {
		t.Fatalf("workspace index failure output = %q; want operation and cause", logged)
	}
	if !strings.Contains(logged, `"state":"failed"`) {
		t.Fatalf("workspace index failure output = %q; want failed progress state", logged)
	}

	server.workspaceWalkDirTestHook = nil
	server.mu.Lock()
	server.workspaceIndexGeneration = 2
	server.mu.Unlock()
	runWorkspaceIndexFailureTestWorker(t, server, 2, "test.scanRecovery")
	if server.workspace[uri] == nil {
		t.Fatal("workspace index did not recover after the scan failure was removed")
	}
	server.mu.Lock()
	readyGeneration = server.workspaceReferenceIndexReadyGeneration
	server.mu.Unlock()
	if readyGeneration != 2 {
		t.Fatalf("recovered workspace reference readiness generation = %d, want 2", readyGeneration)
	}
}

func TestWorkspaceIndexWorkerReadFailurePublishesCauseAndRecovers(t *testing.T) {
	root := t.TempDir()
	sourcePath := filepath.Join(root, "source.asp")
	sourceURI := filePathURI(sourcePath)
	movedPath := sourcePath + ".moved"
	if err := os.WriteFile(sourcePath, []byte(`<% Dim Value %>`), 0o600); err != nil {
		t.Fatal(err)
	}
	var output bytes.Buffer
	server := New(nil, &output, io.Discard)
	server.settings.CacheEnabled = false
	server.workspaceIndexEnabled = true
	server.workspaceIndexGeneration = 1
	server.rootPath = root
	server.rootURI = filePathURI(root)
	server.workspaceRoots = []workspaceRoot{{Path: root, URI: filePathURI(root)}}
	server.workspaceFileReadTestHook = func(path string) {
		if filepath.Clean(path) != filepath.Clean(sourcePath) {
			return
		}
		if err := os.Rename(sourcePath, movedPath); err != nil {
			t.Errorf("rename source for injected read failure: %v", err)
		}
	}

	runWorkspaceIndexFailureTestWorker(t, server, 1, "test.readFailure")
	if _, ok := server.workspace[sourceURI]; ok {
		t.Fatal("failed workspace read published a partial document")
	}
	logged := strings.ToLower(output.String())
	if !strings.Contains(logged, "workspace.index.failed") ||
		!strings.Contains(logged, "no such file") && !strings.Contains(logged, "cannot find the file") {
		t.Fatalf("workspace read failure output = %q; want failed status and read cause", output.String())
	}
	if !strings.Contains(logged, `"state":"failed"`) {
		t.Fatalf("workspace read failure output = %q; want failed progress state", output.String())
	}

	if err := os.Rename(movedPath, sourcePath); err != nil {
		t.Fatal(err)
	}
	server.workspaceFileReadTestHook = nil
	server.mu.Lock()
	server.workspaceIndexGeneration = 2
	server.mu.Unlock()
	runWorkspaceIndexFailureTestWorker(t, server, 2, "test.readRecovery")
	if server.workspace[sourceURI] == nil {
		t.Fatal("workspace index did not recover after the read failure was removed")
	}
}

func TestWorkspaceIndexWorkerCancellationAndStalenessAreNotOperationalFailures(t *testing.T) {
	for _, test := range []struct {
		name       string
		generation uint64
		context    func() context.Context
		state      string
	}{
		{name: "cancelled", generation: 1, context: func() context.Context {
			ctx, cancel := context.WithCancel(context.Background())
			cancel()
			return ctx
		}, state: "cancelled"},
		{name: "stale", generation: 1, context: context.Background, state: "stale"},
	} {
		t.Run(test.name, func(t *testing.T) {
			root := t.TempDir()
			if err := os.WriteFile(filepath.Join(root, "source.asp"), []byte(`<% Dim Value %>`), 0o600); err != nil {
				t.Fatal(err)
			}
			var output bytes.Buffer
			server := New(nil, &output, io.Discard)
			server.settings.CacheEnabled = false
			server.workspaceIndexEnabled = true
			server.workspaceIndexGeneration = 2
			server.rootPath = root
			server.rootURI = filePathURI(root)
			server.workspaceRoots = []workspaceRoot{{Path: root, URI: filePathURI(root)}}
			if test.name == "cancelled" {
				server.workspaceIndexGeneration = 1
			}
			runWorkspaceIndexFailureTestWorkerWithContext(t, server, test.context(), test.generation, "test."+test.name)
			logged := output.String()
			if !strings.Contains(logged, `"state":"`+test.state+`"`) {
				t.Fatalf("workspace index %s output = %q; want %s state", test.name, logged, test.state)
			}
			if strings.Contains(logged, "workspace.index.failed") {
				t.Fatalf("workspace index %s output = %q; unexpected operational failure", test.name, logged)
			}
		})
	}
}

func TestWorkspaceIndexBuildSkipsIgnoredPathsBeforeRead(t *testing.T) {
	root := t.TempDir()
	includedPath := filepath.Join(root, "included.asp")
	excludedPath := filepath.Join(root, "excluded.asp")
	for _, path := range []string{includedPath, excludedPath} {
		if err := os.WriteFile(path, []byte(`<% Dim Value %>`), 0o600); err != nil {
			t.Fatal(err)
		}
	}
	server := New(nil, io.Discard, io.Discard)
	server.workspaceIndexGeneration = 1
	server.settings.WorkspaceExcludeGlobs = []string{"excluded.asp"}
	var excludedReads atomic.Int32
	server.workspaceFileReadTestHook = func(path string) {
		if filepath.Clean(path) == filepath.Clean(excludedPath) {
			excludedReads.Add(1)
		}
	}
	result := server.buildWorkspaceIndexWithProgress(context.Background(), 1, workspaceIndexRunSettings{
		roots:        []workspaceRoot{{Path: root, URI: filePathURI(root)}},
		excludeGlobs: []string{"excluded.asp"},
	}, nil)
	if !result.complete || result.err != nil {
		t.Fatalf("ignored-path workspace result = %#v; want complete success", result)
	}
	if excludedReads.Load() != 0 {
		t.Fatalf("ignored path was read %d times", excludedReads.Load())
	}
	if len(result.documents) != 1 || result.documents[filePathURI(includedPath)] == nil {
		t.Fatalf("ignored-path workspace documents = %#v; want only included file", result.documents)
	}
}

func runWorkspaceIndexFailureTestWorker(t *testing.T, server *Server, generation uint64, reason string) {
	runWorkspaceIndexFailureTestWorkerWithContext(t, server, context.Background(), generation, reason)
}

func runWorkspaceIndexFailureTestWorkerWithContext(t *testing.T, server *Server, ctx context.Context, generation uint64, reason string) {
	t.Helper()
	done := make(chan struct{})
	server.workspaceIndexWorkers.Add(1)
	go server.runWorkspaceIndexWorker(ctx, reason, generation, done)
	select {
	case <-done:
	case <-time.After(5 * time.Second):
		t.Fatal("workspace index worker did not finish")
	}
}
