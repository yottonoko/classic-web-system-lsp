package lspserver

import (
	"context"
	"errors"
	"io"
	"os"
	"path/filepath"
	"strings"
	"testing"

	"github.com/yottonoko/classic-web-system-lsp/internal/core"
	"github.com/yottonoko/classic-web-system-lsp/internal/graph"
)

func TestAnalysisExcelFileURIsIncludeEverySelectedDocument(t *testing.T) {
	server := benchmarkServerWithWorkspaceSources([]benchmarkScriptSource{
		{URI: "file:///bench/excel/selected.asp", Text: `<% Dim SelectedValue %>`},
		{URI: "file:///bench/excel/omitted.asp", Text: `<% Dim OmittedValue %>`},
	})
	configureExcelTestWorkspaceRoot(server, "/bench")
	payload, ok := server.exportAnalysisPayload(analysisExcelExportArg{
		Scope: "document", FileURIs: []string{"file:///bench/excel/selected.asp", "file:///bench/excel/omitted.asp"},
	})
	if !ok {
		t.Fatal("selected fileUris unexpectedly produced no graph")
	}
	if payload.Scope != "workspace" || !graphPayloadHasLabel(payload, "OmittedValue") || !graphPayloadHasLabel(payload, "SelectedValue") {
		t.Fatalf("fileUris did not include every selected document: %#v", payload)
	}
}

func TestAnalysisExcelRejectsFileURIsWithoutAnalyzableDocuments(t *testing.T) {
	server := benchmarkServerWithWorkspaceSources(nil)
	configureExcelTestWorkspaceRoot(server, "/bench")
	payload, ok := server.exportAnalysisPayload(analysisExcelExportArg{
		FileURIs: []string{"https://example.com/remote.asp", "file:///bench/excel/readme.txt"},
	})
	if ok || len(payload.Nodes) != 0 || payload.Scope != "workspace" {
		t.Fatalf("invalid fileUris unexpectedly produced an analysis graph: %#v", payload)
	}
}

func TestAnalysisExcelDocumentFiltersDoNotScanUnrelatedWorkspaceFiles(t *testing.T) {
	const selectedURI = "file:///bench/excel/selected.asp"
	server := benchmarkServerWithWorkspaceSources([]benchmarkScriptSource{
		{URI: selectedURI, Text: `<% Dim SelectedValue %>`},
		{URI: "file:///bench/excel/unrelated.asp", Text: `<% Dim UnrelatedValue %>`},
	})
	configureExcelTestWorkspaceRoot(server, "/bench")
	respectGitIgnore := false
	payload, ok := server.exportAnalysisPayload(analysisExcelExportArg{
		Scope:            "document",
		URI:              selectedURI,
		IncludeGlobs:     []string{"**/*.asp"},
		ExcludeGlobs:     []string{},
		RespectGitIgnore: &respectGitIgnore,
	})
	if !ok {
		t.Fatal("document export unexpectedly produced no graph")
	}
	if !graphPayloadHasLabel(payload, "SelectedValue") {
		t.Fatalf("document export omitted the selected declaration: %#v", payload)
	}
	if graphPayloadHasLabel(payload, "UnrelatedValue") {
		t.Fatalf("document export included an unrelated workspace declaration: %#v", payload)
	}
}

func TestAnalysisExcelDocumentFiltersKeepDirectlyIncludedExcludedDocuments(t *testing.T) {
	const (
		selectedURI = "file:///bench/excel/selected.asp"
		includedURI = "file:///bench/excel/legacy/direct.inc"
	)
	server := benchmarkServerWithWorkspaceSources([]benchmarkScriptSource{
		{URI: selectedURI, Text: `<!-- #include file="legacy/direct.inc" -->`},
		{URI: includedURI, Text: `<% Function DirectlyIncludedValue(): End Function %>`},
		{URI: "file:///bench/excel/legacy/unrelated.inc", Text: `<% Function UnrelatedValue(): End Function %>`},
	})
	configureExcelTestWorkspaceRoot(server, "/bench")
	respectGitIgnore := false
	payload, ok := server.exportAnalysisPayload(analysisExcelExportArg{
		Scope:            "document",
		URI:              selectedURI,
		IncludeGlobs:     []string{"**/*.asp", "**/*.inc"},
		ExcludeGlobs:     []string{"legacy/**"},
		RespectGitIgnore: &respectGitIgnore,
	})
	if !ok || !graphPayloadHasExplicitLabel(payload, "DirectlyIncludedValue") {
		t.Fatalf("document export omitted directly included excluded declaration: %#v", payload)
	}
	if graphPayloadHasLabel(payload, "UnrelatedValue") {
		t.Fatalf("document export included unrelated excluded declaration: %#v", payload)
	}
}

func TestAnalysisExcelRelatedIncludeTreesControlGraphConstruction(t *testing.T) {
	root := t.TempDir()
	mainPath := filepath.Join(root, "main.inc")
	parentPath := filepath.Join(root, "parent.asp")
	siblingPath := filepath.Join(root, "sibling.inc")
	writeWorkspaceGraphFixture(t, mainPath, `<% Response.Write SharedValue %>`)
	writeWorkspaceGraphFixture(t, parentPath, `<!-- #include file="main.inc" -->
<!-- #include file="sibling.inc" -->`)
	writeWorkspaceGraphFixture(t, siblingPath, `<% Const SharedValue = 1 %>`)
	server := New(nil, io.Discard, nil)
	server.rootPath = root
	server.rootURI = pathToFileURI(root)
	server.workspaceRoots = []workspaceRoot{{URI: server.rootURI, Path: root}}
	for _, path := range []string{mainPath, parentPath, siblingPath} {
		text, err := os.ReadFile(path)
		if err != nil {
			t.Fatal(err)
		}
		uri := pathToFileURI(path)
		server.workspace[uri] = core.NewTextDocument(uri, "classic-asp", 1, string(text))
	}

	includeRelated := false
	withoutRelated, ok := server.exportAnalysisPayload(analysisExcelExportArg{
		Scope: "document", URI: pathToFileURI(mainPath), IncludeRelatedIncludeTreesForUnresolved: &includeRelated,
	})
	if !ok || graphPayloadHasExplicitLabel(withoutRelated, "SharedValue") {
		t.Fatalf("disabled related include tree leaked sibling declaration: %#v", withoutRelated)
	}
	includeRelated = true
	withRelated, ok := server.exportAnalysisPayload(analysisExcelExportArg{
		Scope: "document", URI: pathToFileURI(mainPath), IncludeRelatedIncludeTreesForUnresolved: &includeRelated,
	})
	if !ok || !graphPayloadHasExplicitLabel(withRelated, "SharedValue") {
		t.Fatalf("enabled related include tree did not affect graph construction: %#v", withRelated)
	}
}

func TestAnalysisExcelSkipTypeInferenceControlsGraphNodeDetails(t *testing.T) {
	const uri = "file:///bench/excel/types.asp"
	server := benchmarkServerWithWorkspaceSources([]benchmarkScriptSource{{
		URI: uri,
		Text: `<%
' @param BuildCustomer.customerId As CustomerRecord
Function BuildCustomer(customerId)
End Function
%>`,
	}})
	configureExcelTestWorkspaceRoot(server, "/bench")
	includeTypes := false
	withTypes, ok := server.exportAnalysisPayload(analysisExcelExportArg{FileURIs: []string{uri}, SkipTypeInference: &includeTypes})
	if !ok || !graphPayloadHasParameterType(withTypes, "BuildCustomer", "customerId", "CustomerRecord") {
		t.Fatalf("type details were not included in Excel graph: %#v", withTypes)
	}
	skipTypes := true
	withoutTypes, ok := server.exportAnalysisPayload(analysisExcelExportArg{FileURIs: []string{uri}, SkipTypeInference: &skipTypes})
	if !ok || graphPayloadHasParameterType(withoutTypes, "BuildCustomer", "customerId", "CustomerRecord") {
		t.Fatalf("skipTypeInference did not remove graph type details: %#v", withoutTypes)
	}
}

func graphPayloadHasLabel(payload graph.Payload, label string) bool {
	for _, node := range payload.Nodes {
		if node.Label == label {
			return true
		}
	}
	return false
}

func graphPayloadHasExplicitLabel(payload graph.Payload, label string) bool {
	for _, node := range payload.Nodes {
		if node.Label == label && !node.Implicit {
			return true
		}
	}
	return false
}

func graphPayloadHasParameterType(payload graph.Payload, label, parameterName, typeName string) bool {
	for _, node := range payload.Nodes {
		if node.Label != label {
			continue
		}
		for _, parameter := range node.Parameters {
			if parameter.Name == parameterName && parameter.TypeName == typeName {
				return true
			}
		}
	}
	return false
}

func configureExcelTestWorkspaceRoot(server *Server, path string) {
	server.rootPath = path
	server.rootURI = pathToFileURI(path)
	server.workspaceRoots = []workspaceRoot{{URI: server.rootURI, Path: path}}
}

func TestCommitAnalysisExcelWorkbookRejectsLateCancellation(t *testing.T) {
	server := New(nil, nil, nil)
	directory := t.TempDir()
	targetPath := filepath.Join(directory, "analysis.xlsx")
	tempPath := filepath.Join(directory, ".analysis.xlsx.tmp-test.xlsx")
	previous := []byte("previous workbook")
	if err := os.WriteFile(targetPath, previous, 0o600); err != nil {
		t.Fatal(err)
	}
	if err := os.WriteFile(tempPath, []byte("new workbook"), 0o600); err != nil {
		t.Fatal(err)
	}
	ctx, cancel := context.WithCancel(context.Background())
	cancel()
	err := server.commitAnalysisExcelWorkbook(ctx, targetPath, tempPath, server.graphGenerationSnapshot())
	if !errors.Is(err, context.Canceled) {
		t.Fatalf("commitAnalysisExcelWorkbook() error = %v, want context.Canceled", err)
	}
	assertServerExcelFileBytes(t, targetPath, previous)
	if _, statErr := os.Stat(tempPath); statErr != nil {
		t.Fatalf("temporary workbook stat error = %v, want callback to leave it for caller cleanup", statErr)
	}
}

func TestCommitAnalysisExcelWorkbookRejectsGenerationDrift(t *testing.T) {
	server := New(nil, nil, nil)
	directory := t.TempDir()
	targetPath := filepath.Join(directory, "analysis.xlsx")
	tempPath := filepath.Join(directory, ".analysis.xlsx.tmp-test.xlsx")
	previous := []byte("previous workbook")
	if err := os.WriteFile(targetPath, previous, 0o600); err != nil {
		t.Fatal(err)
	}
	if err := os.WriteFile(tempPath, []byte("new workbook"), 0o600); err != nil {
		t.Fatal(err)
	}
	server.graphGeneration = 1
	err := server.commitAnalysisExcelWorkbook(context.Background(), targetPath, tempPath, 0)
	if !errors.Is(err, errAnalysisExcelCommitStale) {
		t.Fatalf("commitAnalysisExcelWorkbook() error = %v, want stale-generation error", err)
	}
	assertServerExcelFileBytes(t, targetPath, previous)
	if _, statErr := os.Stat(tempPath); statErr != nil {
		t.Fatalf("temporary workbook stat error = %v, want callback to leave it for caller cleanup", statErr)
	}
}

func TestCommitAnalysisExcelWorkbookReplacesTarget(t *testing.T) {
	server := New(nil, nil, nil)
	directory := t.TempDir()
	targetPath := filepath.Join(directory, "analysis.xlsx")
	tempPath := filepath.Join(directory, ".analysis.xlsx.tmp-test.xlsx")
	if err := os.WriteFile(targetPath, []byte("previous workbook"), 0o600); err != nil {
		t.Fatal(err)
	}
	if err := os.WriteFile(tempPath, []byte("new workbook"), 0o600); err != nil {
		t.Fatal(err)
	}
	if err := server.commitAnalysisExcelWorkbook(context.Background(), targetPath, tempPath, server.graphGenerationSnapshot()); err != nil {
		t.Fatalf("commitAnalysisExcelWorkbook() error = %v", err)
	}
	assertServerExcelFileBytes(t, targetPath, []byte("new workbook"))
	if _, statErr := os.Stat(tempPath); !os.IsNotExist(statErr) {
		t.Fatalf("temporary workbook stat error = %v, want committed temp removed", statErr)
	}
}

func assertServerExcelFileBytes(t *testing.T, path string, want []byte) {
	t.Helper()
	got, err := os.ReadFile(path)
	if err != nil {
		t.Fatalf("ReadFile(%q) error = %v", path, err)
	}
	if string(got) != string(want) {
		t.Fatalf("%q contents = %q, want %q", path, got, want)
	}
}

func TestAnalysisExcelPayloadResultPreservesCancellation(t *testing.T) {
	server := New(nil, nil, nil)
	ctx, cancel := context.WithCancel(context.Background())
	cancel()
	result := server.exportAnalysisPayloadContextWithProgressResult(ctx, analysisExcelExportArg{
		FileURIs: []string{"file:///workspace/cancelled.asp"},
	}, nil)
	if result.complete || !errors.Is(result.err, context.Canceled) {
		t.Fatalf("cancelled Excel payload result = complete:%t err:%v; want incomplete context.Canceled", result.complete, result.err)
	}
}

func TestAnalysisExcelPayloadResultPreservesGenerationDrift(t *testing.T) {
	server := New(nil, nil, nil)
	uri := "file:///workspace/excel-generation-drift.asp"
	server.workspaceRoots = []workspaceRoot{{URI: "file:///workspace", Path: "/workspace"}}
	server.documents[uri] = core.NewTextDocument(uri, "classic-asp", 1, `<% Dim Value : Value = 1 %>`)
	advanced := false
	result := server.exportAnalysisPayloadContextWithProgressResult(context.Background(), analysisExcelExportArg{URI: uri}, func(string, string, int, int) {
		if advanced {
			return
		}
		advanced = true
		server.mu.Lock()
		server.graphGeneration++
		server.mu.Unlock()
	})
	if result.complete || result.err != errGraphCollectionGeneration {
		t.Fatalf("generation-drift Excel payload result = complete:%t err:%v; want incomplete generation error", result.complete, result.err)
	}
}

func TestAnalysisExcelPayloadResultPreservesIncludeSyncFailure(t *testing.T) {
	server := New(nil, nil, nil)
	defer server.shutdownRuntimeCaches()
	root := t.TempDir()
	server.rootPath = root
	server.rootURI = filePathURI(root)
	server.workspaceRoots = []workspaceRoot{{URI: server.rootURI, Path: root}}
	uri := filePathURI(filepath.Join(root, "workspace.asp"))
	server.workspace[uri] = core.NewTextDocument(uri, "classic-asp", 1, `<% Dim Value %>`)
	server.workspaceIncludeGraphSyncTestHook = func() bool { return false }
	result := server.exportAnalysisPayloadContextWithProgressResult(context.Background(), analysisExcelExportArg{Scope: "workspace"}, nil)
	if result.complete || result.err != errGraphCollectionIncludeSync {
		t.Fatalf("include-sync Excel payload result = complete:%t err:%v; want incomplete include-sync error", result.complete, result.err)
	}
}

func TestAnalysisExcelPayloadResultPreservesIncompleteExpansion(t *testing.T) {
	server := New(nil, nil, nil)
	defer server.shutdownRuntimeCaches()
	root := t.TempDir()
	server.rootPath = root
	server.rootURI = filePathURI(root)
	server.workspaceRoots = []workspaceRoot{{URI: server.rootURI, Path: root}}
	uri := filePathURI(filepath.Join(root, "expansion.asp"))
	source := strings.Repeat(`<!-- #include file="missing.inc" -->`+"\n", includeExpansionUnitBudget+1)
	server.documents[uri] = core.NewTextDocument(uri, "classic-asp", 1, source)
	result := server.exportAnalysisPayloadContextWithProgressResult(context.Background(), analysisExcelExportArg{
		Scope: "document", URI: uri,
	}, nil)
	if result.complete || result.err != errGraphCollectionIncludeExpansion {
		t.Fatalf("incomplete-expansion Excel payload result = complete:%t err:%v; want incomplete expansion error", result.complete, result.err)
	}
}

func TestAnalysisExcelRequestErrorMappingPreservesTarget(t *testing.T) {
	tests := []struct {
		name    string
		setup   func(*Server, string)
		ctx     context.Context
		arg     analysisExcelExportArg
		code    int
		message string
	}{
		{
			name: "invalid fileUris",
			arg:  analysisExcelExportArg{FileURIs: []string{"https://example.com/remote.asp"}},
			code: -32602, message: "fileUris contains no analyzable files.",
		},
		{
			name: "cancelled",
			ctx:  cancelledExcelTestContext(),
			arg:  analysisExcelExportArg{FileURIs: []string{"file:///workspace/cancelled.asp"}},
			code: -32800, message: "request cancelled",
		},
		{
			name: "generation drift",
			setup: func(server *Server, uri string) {
				server.documents[uri] = core.NewTextDocument(uri, "classic-asp", 1, `<% Dim Value %>`)
				advanced := false
				server.documentParseTestHook = func(string) {
					if advanced {
						return
					}
					advanced = true
					server.mu.Lock()
					server.graphGeneration++
					server.mu.Unlock()
				}
			},
			arg:  analysisExcelExportArg{Scope: "document"},
			code: -32803, message: "analysis graph became stale; retry the request.",
		},
		{
			name: "include sync",
			setup: func(server *Server, uri string) {
				server.workspace[uri] = core.NewTextDocument(uri, "classic-asp", 1, `<% Dim Value %>`)
				server.workspaceIncludeGraphSyncTestHook = func() bool { return false }
			},
			arg:  analysisExcelExportArg{Scope: "workspace"},
			code: -32603, message: errGraphCollectionIncludeSync.Error(),
		},
		{
			name: "incomplete expansion",
			setup: func(server *Server, uri string) {
				server.documents[uri] = core.NewTextDocument(uri, "classic-asp", 1,
					strings.Repeat(`<!-- #include file="missing.inc" -->`+"\n", includeExpansionUnitBudget+1))
			},
			arg:  analysisExcelExportArg{Scope: "document"},
			code: -32603, message: errGraphCollectionIncludeExpansion.Error(),
		},
	}
	for _, test := range tests {
		t.Run(test.name, func(t *testing.T) {
			server := New(nil, io.Discard, io.Discard)
			defer server.shutdownRuntimeCaches()
			root := t.TempDir()
			server.rootPath = root
			server.rootURI = filePathURI(root)
			server.workspaceRoots = []workspaceRoot{{URI: server.rootURI, Path: root}}
			uri := filePathURI(filepath.Join(root, "source.asp"))
			if test.arg.URI == "" && test.arg.Scope == "document" {
				test.arg.URI = uri
			}
			targetPath := filepath.Join(root, "analysis.xlsx")
			previous := []byte("previous workbook")
			if err := os.WriteFile(targetPath, previous, 0o600); err != nil {
				t.Fatal(err)
			}
			if test.setup != nil {
				test.setup(server, uri)
			}
			test.arg.TargetPath = targetPath
			ctx := test.ctx
			if ctx == nil {
				ctx = context.Background()
			}
			result, rpcErr := server.exportAnalysisExcelRequest(ctx, executeCommandParams{Arguments: []any{test.arg}})
			if result != nil {
				t.Fatalf("exportAnalysisExcelRequest result = %#v, want nil RPC error", result)
			}
			if rpcErr == nil || rpcErr.Code != test.code || rpcErr.Message != test.message {
				t.Fatalf("exportAnalysisExcelRequest error = %#v, want code=%d message=%q", rpcErr, test.code, test.message)
			}
			assertServerExcelFileBytes(t, targetPath, previous)
		})
	}
}

func cancelledExcelTestContext() context.Context {
	ctx, cancel := context.WithCancel(context.Background())
	cancel()
	return ctx
}
