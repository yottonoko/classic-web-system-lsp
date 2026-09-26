package lspserver

import (
	"context"
	"fmt"
	"io"
	"os"
	"path/filepath"
	"strings"
	"testing"

	"github.com/yottonoko/classic-web-system-lsp/internal/core"
	"github.com/yottonoko/classic-web-system-lsp/internal/lsp"
)

type featureCompletenessFixture struct {
	server    *Server
	uri       string
	source    string
	testRange lsp.Range
}

func newFeatureCompletenessFixture(t *testing.T, depth int) featureCompletenessFixture {
	t.Helper()
	root := t.TempDir()
	server := New(strings.NewReader(""), io.Discard, io.Discard)
	server.rootPath = root
	server.rootURI = filePathURI(root)
	server.workspaceRoots = []workspaceRoot{{Path: root, URI: server.rootURI}}
	for index := depth; index >= 1; index-- {
		next := ""
		if index > 1 {
			next = fmt.Sprintf("<!-- #include file=\"level-%d.inc\" -->\n<!-- #include file=\"level-%d.inc\" -->\n", index-1, index-1)
		}
		path := filepath.Join(root, fmt.Sprintf("level-%d.inc", index))
		if err := os.WriteFile(path, []byte(next+"<% Dim includedValue %>\n"), 0o600); err != nil {
			t.Fatal(err)
		}
	}
	source := fmt.Sprintf("<!-- #include file=\"level-%d.inc\" -->\n<!-- #include file=\"level-%d.inc\" -->\n<%%\nFunction BuildName(firstName)\n  BuildName = firstName\nEnd Function\nDim rootValue\nrootValue = BuildName(\"Dashboard\")\n%%>\n", depth, depth)
	ownerPath := filepath.Join(root, "default.asp")
	if err := os.WriteFile(ownerPath, []byte(source), 0o600); err != nil {
		t.Fatal(err)
	}
	uri := filePathURI(ownerPath)
	server.documents[uri] = core.NewTextDocument(uri, "classic-asp", 1, source)
	document := core.NewTextDocument(uri, "classic-asp", 0, source)
	return featureCompletenessFixture{server: server, uri: uri, source: source, testRange: document.Range(0, len(source))}
}

func assertNoFeatureCaches(t *testing.T, server *Server, uri string, parsed *core.ParsedDocument) {
	t.Helper()
	server.mu.Lock()
	defer server.mu.Unlock()
	if _, ok := server.semantic[uri]; ok {
		t.Fatalf("semantic token cache contains incomplete request URI %q", uri)
	}
	for resultID := range server.semanticHistory {
		if strings.HasPrefix(resultID, uri+"#") {
			t.Fatalf("semantic token history contains incomplete request result %q", resultID)
		}
	}
	for key, inflight := range server.semanticInflight {
		if inflight != nil && inflight.uri == uri {
			t.Fatalf("semantic token inflight cache contains incomplete request key %q", key)
		}
	}
	if parsed != nil {
		if _, ok := server.referenceDeclarationPlans[parsed]; ok {
			t.Fatalf("CodeLens declaration plan cached incomplete request URI %q", uri)
		}
	}
	if len(server.referenceBatch) != 0 {
		t.Fatalf("reference batch cache contains incomplete request state: %d entries", len(server.referenceBatch))
	}
}

func TestFeatureRequestsRejectBudgetTruncationWithoutPartialResultsOrCaches(t *testing.T) {
	fixture := newFeatureCompletenessFixture(t, 14)
	defer fixture.server.shutdownRuntimeCaches()
	fixture.server.settings.CodeLensReferences = true
	fixture.server.settings.CodeLensIncludes = true
	_, parsed := fixture.server.parsed(fixture.uri)
	if _, complete := fixture.server.includedDocumentsContextResult(context.Background(), parsed); complete {
		t.Fatal("depth-14 fanout unexpectedly completed within include budget")
	}

	request := func(method string, params map[string]any) any {
		t.Helper()
		result, rpcErr := fixture.server.handleRequest(context.Background(), method, mustRaw(params))
		if rpcErr != nil {
			t.Fatalf("%s request error = %#v", method, rpcErr)
		}
		return result
	}
	full := request("textDocument/semanticTokens/full", map[string]any{
		"textDocument": map[string]any{"uri": fixture.uri},
	})
	if tokens, ok := full.(lsp.SemanticTokens); !ok || len(tokens.Data) != 0 || tokens.ResultID != "" {
		t.Fatalf("incomplete semantic full result = %#v, want empty result", full)
	}
	assertNoFeatureCaches(t, fixture.server, fixture.uri, parsed)

	rangeResult := request("textDocument/semanticTokens/range", map[string]any{
		"textDocument": map[string]any{"uri": fixture.uri},
		"range":        fixture.testRange,
	})
	if tokens, ok := rangeResult.(lsp.SemanticTokens); !ok || len(tokens.Data) != 0 || tokens.ResultID != "" {
		t.Fatalf("incomplete semantic range result = %#v, want empty result", rangeResult)
	}
	assertNoFeatureCaches(t, fixture.server, fixture.uri, parsed)

	delta := request("textDocument/semanticTokens/full/delta", map[string]any{
		"textDocument":     map[string]any{"uri": fixture.uri},
		"previousResultId": semanticTokensResultID(fixture.uri, 1),
	})
	if tokens, ok := delta.(lsp.SemanticTokens); !ok || len(tokens.Data) != 0 || tokens.ResultID != "" {
		t.Fatalf("incomplete semantic delta result = %#v, want empty result", delta)
	}
	assertNoFeatureCaches(t, fixture.server, fixture.uri, parsed)

	inlay := request("textDocument/inlayHint", map[string]any{
		"textDocument": map[string]any{"uri": fixture.uri},
		"range":        fixture.testRange,
	})
	if hints, ok := inlay.([]lsp.InlayHint); !ok || len(hints) != 0 {
		t.Fatalf("incomplete inlay result = %#v, want empty result", inlay)
	}
	assertNoFeatureCaches(t, fixture.server, fixture.uri, parsed)

	lenses := request("textDocument/codeLens", map[string]any{
		"textDocument": map[string]any{"uri": fixture.uri},
	})
	if values, ok := lenses.([]lsp.CodeLens); !ok || len(values) != 0 {
		t.Fatalf("incomplete CodeLens generation result = %#v, want empty result", lenses)
	}
	assertNoFeatureCaches(t, fixture.server, fixture.uri, parsed)

	resolved := request("codeLens/resolve", map[string]any{
		"range": fixture.testRange,
		"data": map[string]any{
			"uri": fixture.uri, "name": "rootValue", "line": 5, "character": 4, "symbolKind": "variable",
		},
	})
	if lens, ok := resolved.(lsp.CodeLens); !ok || lens.Command != nil || lens.Data != nil {
		t.Fatalf("incomplete CodeLens resolve result = %#v, want empty result", resolved)
	}
	assertNoFeatureCaches(t, fixture.server, fixture.uri, parsed)
}

func TestFeatureRequestsPublishNormalResultsBelowIncludeBudget(t *testing.T) {
	fixture := newFeatureCompletenessFixture(t, 2)
	defer fixture.server.shutdownRuntimeCaches()
	fixture.server.settings.CodeLensReferences = true
	fixture.server.settings.CodeLensIncludes = true
	fixture.server.settings.InlayParameterNames = true

	full := fixture.server.semanticTokensContext(context.Background(), fixture.uri)
	if len(full.Data) == 0 || full.ResultID == "" {
		t.Fatalf("complete semantic full result = %#v, want data and result ID", full)
	}
	rangeResult := fixture.server.semanticTokensRangeContext(context.Background(), fixture.uri, fixture.testRange)
	if len(rangeResult.Data) == 0 {
		t.Fatalf("complete semantic range result = %#v, want data", rangeResult)
	}
	hints := fixture.server.inlayHintsContext(context.Background(), fixture.uri, fixture.testRange)
	if len(hints) == 0 {
		t.Fatal("complete inlay result is empty")
	}
	lenses := fixture.server.codeLensContext(context.Background(), fixture.uri)
	if len(lenses) == 0 {
		t.Fatal("complete CodeLens result is empty")
	}
	_, parsed := fixture.server.parsed(fixture.uri)
	fixture.server.mu.Lock()
	_, semanticCached := fixture.server.semantic[fixture.uri]
	_, planCached := fixture.server.referenceDeclarationPlans[parsed]
	fixture.server.mu.Unlock()
	if !semanticCached || !planCached {
		t.Fatalf("complete feature caches = semantic=%v CodeLensPlan=%v, want both", semanticCached, planCached)
	}
}
