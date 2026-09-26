package lspserver

import (
	"context"
	"io"
	"os"
	"path/filepath"
	"testing"

	"github.com/yottonoko/classic-web-system-lsp/internal/core"
)

func TestExportAnalysisExcelRequestRejectsEmptyFolderAndWorkspaceScopes(t *testing.T) {
	tests := []struct {
		name  string
		scope string
	}{
		{name: "folder", scope: "folder"},
		{name: "workspace", scope: "workspace"},
	}
	for _, test := range tests {
		t.Run(test.name, func(t *testing.T) {
			server, root, targetPath := newReview39ExcelRequestServer(t)
			result, rpcErr := server.exportAnalysisExcelRequest(context.Background(), executeCommandParams{Arguments: []any{
				analysisExcelExportArg{Scope: test.scope, URI: filePathURI(root), TargetPath: targetPath},
			}})
			assertReview39ExcelRequestError(t, result, rpcErr, targetPath)
			assertReview39NoExcelTempFiles(t, root, targetPath)
		})
	}
}

func TestExportAnalysisExcelRequestRejectsFolderWithNoAnalyzableFilesAfterFilters(t *testing.T) {
	tests := []struct {
		name  string
		setup func(t *testing.T, root string)
		arg   func(root, targetPath string) analysisExcelExportArg
	}{
		{
			name: "all files excluded",
			setup: func(t *testing.T, root string) {
				review39WriteFile(t, filepath.Join(root, "excluded.asp"), `<% Dim ExcludedValue %>`)
			},
			arg: func(root, targetPath string) analysisExcelExportArg {
				return analysisExcelExportArg{
					Scope: "folder", URI: filePathURI(root), TargetPath: targetPath,
					IncludeGlobs: []string{"**/*.asp"}, ExcludeGlobs: []string{"**/*.asp"},
					RespectGitIgnore: review39BoolPtr(false),
				}
			},
		},
		{
			name: "all files gitignored",
			setup: func(t *testing.T, root string) {
				review39WriteFile(t, filepath.Join(root, ".gitignore"), "ignored.asp\n")
				review39WriteFile(t, filepath.Join(root, "ignored.asp"), `<% Dim IgnoredValue %>`)
			},
			arg: func(root, targetPath string) analysisExcelExportArg {
				return analysisExcelExportArg{
					Scope: "folder", URI: filePathURI(root), TargetPath: targetPath,
					IncludeGlobs: []string{"**/*.asp"}, RespectGitIgnore: review39BoolPtr(true),
				}
			},
		},
		{
			name: "only unsupported files",
			setup: func(t *testing.T, root string) {
				review39WriteFile(t, filepath.Join(root, "readme.txt"), "not Classic ASP")
			},
			arg: func(root, targetPath string) analysisExcelExportArg {
				return analysisExcelExportArg{Scope: "folder", URI: filePathURI(root), TargetPath: targetPath}
			},
		},
	}
	for _, test := range tests {
		t.Run(test.name, func(t *testing.T) {
			server, root, targetPath := newReview39ExcelRequestServer(t)
			test.setup(t, root)
			result, rpcErr := server.exportAnalysisExcelRequest(context.Background(), executeCommandParams{Arguments: []any{
				test.arg(root, targetPath),
			}})
			assertReview39ExcelRequestError(t, result, rpcErr, targetPath)
			assertReview39NoExcelTempFiles(t, root, targetPath)
		})
	}
}

func TestExportAnalysisExcelRequestRejectsWorkspaceAfterDeduplicationAndFiltering(t *testing.T) {
	server, root, targetPath := newReview39ExcelRequestServer(t)
	uri := filePathURI(filepath.Join(root, "duplicate.asp"))
	document := core.NewTextDocument(uri, "classic-asp", 1, `<% Dim DuplicateValue %>`)
	server.workspace[uri] = document
	server.documents[uri] = document
	result, rpcErr := server.exportAnalysisExcelRequest(context.Background(), executeCommandParams{Arguments: []any{
		analysisExcelExportArg{
			Scope: "workspace", URI: filePathURI(root), TargetPath: targetPath,
			IncludeGlobs: []string{"**/*.asp"}, ExcludeGlobs: []string{"**/*.asp"},
			RespectGitIgnore: review39BoolPtr(false),
		},
	}})
	assertReview39ExcelRequestError(t, result, rpcErr, targetPath)
	assertReview39NoExcelTempFiles(t, root, targetPath)
}

func TestExportAnalysisExcelRequestSucceedsForNonemptyFolder(t *testing.T) {
	server, root, targetPath := newReview39ExcelRequestServer(t)
	review39WriteFile(t, filepath.Join(root, "valid.asp"), `<% Dim ValidValue %>`)
	result, rpcErr := server.exportAnalysisExcelRequest(context.Background(), executeCommandParams{Arguments: []any{
		analysisExcelExportArg{Scope: "folder", URI: filePathURI(root), TargetPath: targetPath},
	}})
	if rpcErr != nil {
		t.Fatalf("exportAnalysisExcelRequest error = %#v, want success", rpcErr)
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
	assertReview39NoExcelTempFiles(t, root, targetPath)
}

func newReview39ExcelRequestServer(t *testing.T) (*Server, string, string) {
	t.Helper()
	root := t.TempDir()
	server := New(nil, io.Discard, io.Discard)
	server.rootPath = root
	server.rootURI = filePathURI(root)
	server.workspaceRoots = []workspaceRoot{{URI: server.rootURI, Path: root}}
	t.Cleanup(server.shutdownRuntimeCaches)
	targetPath := filepath.Join(root, "analysis.xlsx")
	review39WriteFile(t, targetPath, "previous workbook")
	return server, root, targetPath
}

func review39WriteFile(t *testing.T, path, contents string) {
	t.Helper()
	if err := os.WriteFile(path, []byte(contents), 0o600); err != nil {
		t.Fatal(err)
	}
}

func review39BoolPtr(value bool) *bool {
	return &value
}

func assertReview39ExcelRequestError(t *testing.T, result any, rpcErr *rpcError, targetPath string) {
	t.Helper()
	if result != nil {
		t.Fatalf("exportAnalysisExcelRequest result = %#v, want nil RPC error", result)
	}
	if rpcErr == nil || rpcErr.Code != -32602 || rpcErr.Message != "fileUris contains no analyzable files." {
		t.Fatalf("exportAnalysisExcelRequest error = %#v, want -32602 no-analyzable-files", rpcErr)
	}
	contents, err := os.ReadFile(targetPath)
	if err != nil {
		t.Fatal(err)
	}
	if string(contents) != "previous workbook" {
		t.Fatalf("existing target contents = %q, want preserved workbook", contents)
	}
}

func assertReview39NoExcelTempFiles(t *testing.T, root, targetPath string) {
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
