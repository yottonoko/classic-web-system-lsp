package lspserver

import (
	"context"
	"errors"
	"io"
	"os"
	"path/filepath"
	"runtime"
	"strings"
	"testing"
)

func TestExportAnalysisExcelRequestRejectsEmptyDocumentURIPreservingTarget(t *testing.T) {
	server, root, targetPath := newExcelRequestInputTestServer(t)
	result, rpcErr := server.exportAnalysisExcelRequest(context.Background(), executeCommandParams{Arguments: []any{
		analysisExcelExportArg{Scope: "document", TargetPath: targetPath},
	}})
	assertExcelRequestErrorAndTarget(t, result, rpcErr, targetPath, -32602, "fileUris contains no analyzable files.")
	assertNoExcelTempFiles(t, root, targetPath)
}

func TestExportAnalysisExcelRequestRejectsNonexistentTrustedDocumentPreservingTarget(t *testing.T) {
	server, root, targetPath := newExcelRequestInputTestServer(t)
	missingURI := filePathURI(filepath.Join(root, "missing.asp"))
	result, rpcErr := server.exportAnalysisExcelRequest(context.Background(), executeCommandParams{Arguments: []any{
		analysisExcelExportArg{Scope: "document", URI: missingURI, TargetPath: targetPath},
	}})
	assertExcelRequestErrorAndTarget(t, result, rpcErr, targetPath, -32602, "fileUris contains no analyzable files.")
	assertNoExcelTempFiles(t, root, targetPath)
}

func TestExportAnalysisExcelRequestPreservesTrustedReadFailureCauseAndTarget(t *testing.T) {
	if runtime.GOOS == "windows" {
		t.Skip("permission-denied behavior is not portable to Windows")
	}
	server, root, targetPath := newExcelRequestInputTestServer(t)
	sourcePath := filepath.Join(root, "read-failure.asp")
	if err := os.WriteFile(sourcePath, []byte(`<% Dim ExportedValue %>`), 0o600); err != nil {
		t.Fatal(err)
	}
	server.workspaceFileReadTestHook = func(path string) {
		if filepath.Clean(path) == filepath.Clean(sourcePath) {
			if err := os.Chmod(sourcePath, 0); err != nil {
				t.Errorf("chmod source for injected read failure: %v", err)
			}
		}
	}
	defer func() { _ = os.Chmod(sourcePath, 0o600) }()

	uri := filePathURI(sourcePath)
	if _, err := server.parsedGraphDocumentContextResult(context.Background(), uri); !errors.Is(err, os.ErrPermission) {
		t.Fatalf("parsedGraphDocumentContextResult() error = %v, want permission cause", err)
	}
	result, rpcErr := server.exportAnalysisExcelRequest(context.Background(), executeCommandParams{Arguments: []any{
		analysisExcelExportArg{FileURIs: []string{uri}, TargetPath: targetPath},
	}})
	if result != nil {
		t.Fatalf("exportAnalysisExcelRequest result = %#v, want nil", result)
	}
	if rpcErr == nil || rpcErr.Code != -32603 || !strings.Contains(rpcErr.Message, "permission denied") {
		t.Fatalf("exportAnalysisExcelRequest error = %#v, want -32603 with permission cause", rpcErr)
	}
	assertExcelTargetBytes(t, targetPath, []byte("previous workbook"))
	assertNoExcelTempFiles(t, root, targetPath)
}

func TestExportAnalysisExcelRequestSucceedsForValidDocumentReplacingTarget(t *testing.T) {
	server, root, targetPath := newExcelRequestInputTestServer(t)
	sourcePath := filepath.Join(root, "valid.asp")
	if err := os.WriteFile(sourcePath, []byte(`<% Dim ExportedValue %>`), 0o600); err != nil {
		t.Fatal(err)
	}
	uri := filePathURI(sourcePath)
	result, rpcErr := server.exportAnalysisExcelRequest(context.Background(), executeCommandParams{Arguments: []any{
		analysisExcelExportArg{FileURIs: []string{uri}, TargetPath: targetPath},
	}})
	if rpcErr != nil {
		t.Fatalf("exportAnalysisExcelRequest error = %#v", rpcErr)
	}
	payload, ok := result.(map[string]any)
	if !ok || payload["ok"] != true || payload["targetPath"] != targetPath {
		t.Fatalf("exportAnalysisExcelRequest result = %#v, want successful target result", result)
	}
	contents, err := os.ReadFile(targetPath)
	if err != nil {
		t.Fatal(err)
	}
	if len(contents) < 2 || string(contents[:2]) != "PK" {
		t.Fatalf("target workbook header = %q, want XLSX zip", contents[:min(len(contents), 2)])
	}
	assertNoExcelTempFiles(t, root, targetPath)
}

func newExcelRequestInputTestServer(t *testing.T) (*Server, string, string) {
	t.Helper()
	root := t.TempDir()
	server := New(nil, io.Discard, io.Discard)
	server.rootPath = root
	server.rootURI = filePathURI(root)
	server.workspaceRoots = []workspaceRoot{{URI: server.rootURI, Path: root}}
	t.Cleanup(server.shutdownRuntimeCaches)
	targetPath := filepath.Join(root, "analysis.xlsx")
	if err := os.WriteFile(targetPath, []byte("previous workbook"), 0o600); err != nil {
		t.Fatal(err)
	}
	return server, root, targetPath
}

func assertExcelRequestErrorAndTarget(t *testing.T, result any, rpcErr *rpcError, targetPath string, code int, message string) {
	t.Helper()
	if result != nil {
		t.Fatalf("exportAnalysisExcelRequest result = %#v, want nil", result)
	}
	if rpcErr == nil || rpcErr.Code != code || rpcErr.Message != message {
		t.Fatalf("exportAnalysisExcelRequest error = %#v, want code=%d message=%q", rpcErr, code, message)
	}
	assertExcelTargetBytes(t, targetPath, []byte("previous workbook"))
}

func assertExcelTargetBytes(t *testing.T, path string, want []byte) {
	t.Helper()
	got, err := os.ReadFile(path)
	if err != nil {
		t.Fatalf("ReadFile(%q) error = %v", path, err)
	}
	if string(got) != string(want) {
		t.Fatalf("%q contents = %q, want %q", path, got, want)
	}
}

func assertNoExcelTempFiles(t *testing.T, root, targetPath string) {
	t.Helper()
	pattern := filepath.Join(root, "."+filepath.Base(targetPath)+".tmp-*"+filepath.Ext(targetPath))
	matches, err := filepath.Glob(pattern)
	if err != nil {
		t.Fatalf("Glob(%q) error = %v", pattern, err)
	}
	if len(matches) != 0 {
		t.Fatalf("temporary workbook files = %#v, want none", matches)
	}
}
