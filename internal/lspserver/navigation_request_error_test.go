package lspserver

import (
	"bytes"
	"context"
	"errors"
	"io"
	"os"
	"path/filepath"
	"strings"
	"testing"
)

func TestBuildNavigationGraphRequestPropagatesFolderWalkFailure(t *testing.T) {
	root := t.TempDir()
	sourcePath := filepath.Join(root, "source.asp")
	if err := os.WriteFile(sourcePath, []byte(`<% Response.Write "ready" %>`), 0o600); err != nil {
		t.Fatal(err)
	}
	var output bytes.Buffer
	server := newNavigationRequestErrorTestServer(t, root, &output)
	walkErr := errors.New("injected navigation walk failure")
	server.workspaceWalkDirTestHook = func(path string) error {
		if filepath.Clean(path) == filepath.Clean(sourcePath) {
			return walkErr
		}
		return nil
	}

	result, rpcErr := server.handleRequest(context.Background(), "workspace/executeCommand", mustRaw(map[string]any{
		"command": "aspLsp.server.buildNavigationGraph",
		"arguments": []any{map[string]any{
			"scope": "folder",
			"uri":   filePathURI(root),
		}},
	}))
	if result != nil || rpcErr == nil || rpcErr.Code != -32603 || !strings.Contains(rpcErr.Message, walkErr.Error()) {
		t.Fatalf("navigation walk failure = result:%#v error:%#v; want -32603 with cause", result, rpcErr)
	}
	if !strings.Contains(output.String(), `"state":"failed"`) {
		t.Fatalf("navigation progress output = %s; want failed terminal state", output.String())
	}
}

func TestBuildNavigationGraphRequestPropagatesFolderReadFailure(t *testing.T) {
	root := t.TempDir()
	sourcePath := filepath.Join(root, "source.asp")
	movedPath := sourcePath + ".moved"
	if err := os.WriteFile(sourcePath, []byte(`<% Response.Write "ready" %>`), 0o600); err != nil {
		t.Fatal(err)
	}
	var output bytes.Buffer
	server := newNavigationRequestErrorTestServer(t, root, &output)
	server.workspaceFileReadTestHook = func(path string) {
		if filepath.Clean(path) != filepath.Clean(sourcePath) {
			return
		}
		if err := os.Rename(sourcePath, movedPath); err != nil {
			t.Errorf("rename source for injected navigation read failure: %v", err)
		}
	}
	defer func() {
		if _, err := os.Stat(movedPath); err == nil {
			if renameErr := os.Rename(movedPath, sourcePath); renameErr != nil {
				t.Errorf("restore source after injected navigation read failure: %v", renameErr)
			}
		}
	}()

	result, rpcErr := server.handleRequest(context.Background(), "workspace/executeCommand", mustRaw(map[string]any{
		"command": "aspLsp.server.buildNavigationGraph",
		"arguments": []any{map[string]any{
			"scope": "folder",
			"uri":   filePathURI(root),
		}},
	}))
	message := ""
	if rpcErr != nil {
		message = strings.ToLower(rpcErr.Message)
	}
	if result != nil || rpcErr == nil || rpcErr.Code != -32603 ||
		!strings.Contains(message, "no such file") && !strings.Contains(message, "cannot find the file") {
		t.Fatalf("navigation read failure = result:%#v error:%#v; want -32603 with cause", result, rpcErr)
	}
	if !strings.Contains(output.String(), `"state":"failed"`) {
		t.Fatalf("navigation progress output = %s; want failed terminal state", output.String())
	}
}

func TestBuildNavigationGraphRequestPreservesCancellation(t *testing.T) {
	root := t.TempDir()
	sourcePath := filepath.Join(root, "source.asp")
	if err := os.WriteFile(sourcePath, []byte(`<% Response.Write "ready" %>`), 0o600); err != nil {
		t.Fatal(err)
	}
	var output bytes.Buffer
	server := newNavigationRequestErrorTestServer(t, root, &output)
	ctx, cancel := context.WithCancel(context.Background())
	defer cancel()
	server.workspaceWalkDirTestHook = func(path string) error {
		if filepath.Clean(path) == filepath.Clean(sourcePath) {
			cancel()
		}
		return nil
	}

	result, rpcErr := server.handleRequest(ctx, "workspace/executeCommand", mustRaw(map[string]any{
		"command": "aspLsp.server.buildNavigationGraph",
		"arguments": []any{map[string]any{
			"scope": "folder",
			"uri":   filePathURI(root),
		}},
	}))
	if result != nil || rpcErr == nil || rpcErr.Code != requestCancelledError().Code {
		t.Fatalf("cancelled navigation = result:%#v error:%#v; want request cancellation", result, rpcErr)
	}
	if strings.Contains(output.String(), `"state":"failed"`) {
		t.Fatalf("cancelled navigation progress output = %s; want no failed terminal state", output.String())
	}
}

func TestNavigationWorkspaceFilteringCancelsGitIgnoreCollection(t *testing.T) {
	root := t.TempDir()
	nested := filepath.Join(root, "nested")
	if err := os.MkdirAll(nested, 0o755); err != nil {
		t.Fatal(err)
	}
	for _, path := range []string{filepath.Join(root, ".gitignore"), filepath.Join(nested, ".gitignore")} {
		if err := os.WriteFile(path, []byte("ignored.asp\n"), 0o600); err != nil {
			t.Fatal(err)
		}
	}
	var output bytes.Buffer
	server := newNavigationRequestErrorTestServer(t, root, &output)
	server.settings.WorkspaceRespectGitIgnore = true
	ctx, cancel := context.WithCancel(context.Background())
	defer cancel()
	gitIgnoreReads := 0
	gitIgnorePaths := []string{}
	server.workspaceFileReadTestHook = func(path string) {
		if filepath.Base(path) != ".gitignore" {
			return
		}
		gitIgnoreReads++
		gitIgnorePaths = append(gitIgnorePaths, path)
		if gitIgnoreReads == 1 {
			cancel()
		}
	}

	result := server.navigationWorkspaceDocumentsContextWithProgressResult(ctx, nil)
	if result.complete || result.documents != nil || !errors.Is(result.err, context.Canceled) {
		t.Fatalf("cancelled project filtering = %#v; want cancellation and no documents", result)
	}
	if gitIgnoreReads != 1 {
		t.Fatalf("gitignore reads after cancellation = %d (%v), want 1", gitIgnoreReads, gitIgnorePaths)
	}
}

func TestBuildNavigationGraphRequestReturnsEmptyScopePayloads(t *testing.T) {
	root := t.TempDir()
	var output bytes.Buffer
	server := newNavigationRequestErrorTestServer(t, root, &output)

	result, rpcErr := server.handleRequest(context.Background(), "workspace/executeCommand", mustRaw(map[string]any{
		"command": "aspLsp.server.buildNavigationGraph",
		"arguments": []any{map[string]any{
			"scope": "folder",
			"uri":   filePathURI(root),
		}},
	}))
	if rpcErr != nil {
		t.Fatalf("empty folder navigation error = %#v, want nil", rpcErr)
	}
	payload, ok := result.(map[string]any)
	if !ok || payload == nil {
		t.Fatalf("empty folder navigation result = %#v, want non-nil payload", result)
	}
	if payload["scope"] != "folder" {
		t.Fatalf("empty folder navigation scope = %#v, want folder", payload["scope"])
	}
	stats, ok := payload["stats"].(map[string]int)
	if !ok || stats["documents"] != 0 {
		t.Fatalf("empty folder navigation stats = %#v, want zero documents", payload["stats"])
	}

	result, rpcErr = server.handleRequest(context.Background(), "workspace/executeCommand", mustRaw(map[string]any{
		"command": "aspLsp.server.buildNavigationGraph",
		"arguments": []any{map[string]any{
			"scope": "document",
			"uri":   filePathURI(filepath.Join(root, "missing.asp")),
		}},
	}))
	if rpcErr != nil {
		t.Fatalf("missing document navigation error = %#v, want nil", rpcErr)
	}
	payload, ok = result.(map[string]any)
	if !ok || payload == nil {
		t.Fatalf("missing document navigation result = %#v, want non-nil payload", result)
	}
	stats, ok = payload["stats"].(map[string]int)
	if !ok || stats["documents"] != 0 || stats["nodes"] != 0 || stats["edges"] != 0 {
		t.Fatalf("missing document navigation stats = %#v, want empty payload", payload["stats"])
	}
}

func newNavigationRequestErrorTestServer(t *testing.T, root string, output io.Writer) *Server {
	t.Helper()
	server := New(nil, output, io.Discard)
	server.rootPath = root
	server.rootURI = filePathURI(root)
	server.workspaceRoots = []workspaceRoot{{Path: root, URI: server.rootURI}}
	t.Cleanup(server.shutdownRuntimeCaches)
	return server
}
