package lspserver

import (
	"bytes"
	"context"
	"encoding/json"
	"io"
	"reflect"
	"testing"

	"github.com/yottonoko/classic-web-system-lsp/internal/core"
	"github.com/yottonoko/classic-web-system-lsp/internal/lsp"
)

func TestNormalAnalysisSnapshotsDoNotComputeOrHydrateSemanticTokens(t *testing.T) {
	server := New(nil, io.Discard, io.Discard)
	uri := "file:///workspace/normal-analysis.asp"
	doc := core.NewTextDocument(uri, "classic-asp", 1, `<% Dim value : value = 1 %>`)
	parsed := core.ParseDocument(uri, doc.Text, core.Settings{DefaultLanguage: server.settings.DefaultLanguage})

	snapshot := server.buildFileAnalysisSnapshot(parsed)
	assertNoVBSemanticTokenAnalysis(t, parsed)
	raw, err := json.Marshal(snapshot)
	if err != nil {
		t.Fatal(err)
	}
	if bytes.Contains(raw, []byte("VBSemanticTokens")) {
		t.Fatalf("normal-analysis snapshot serialized semantic tokens: %s", raw)
	}

	restored := core.ParseDocument(uri, doc.Text, core.Settings{DefaultLanguage: server.settings.DefaultLanguage})
	seedParsedAnalysis(restored, snapshot)
	assertNoVBSemanticTokenAnalysis(t, restored)

	server.documents[uri] = doc
	server.applyWorkspaceDocumentRevision(doc, parsed)
	assertNoVBSemanticTokenAnalysis(t, parsed)
}

func TestSemanticTokenRequestsAreLazyCachedAndInvalidatedPerEditedFile(t *testing.T) {
	server := New(nil, io.Discard, io.Discard)
	server.settings.DiagnosticsDebounceMS = 0
	physicalReads := 0
	server.workspaceFileReadTestHook = func(string) { physicalReads++ }
	firstURI := "file:///workspace/first.asp"
	secondURI := "file:///workspace/second.asp"
	firstText := `<% Dim firstValue : firstValue = 1 : Response.Write firstValue %>`
	secondText := `<% Dim secondValue : secondValue = 1 : Response.Write secondValue %>`
	first := core.NewTextDocument(firstURI, "classic-asp", 1, firstText)
	second := core.NewTextDocument(secondURI, "classic-asp", 1, secondText)
	server.documents[firstURI] = first
	server.documents[secondURI] = second

	server.applyWorkspaceDocumentRevision(first, server.parseTextDocument(first, server.settings.DefaultLanguage))
	server.applyWorkspaceDocumentRevision(second, server.parseTextDocument(second, server.settings.DefaultLanguage))
	if len(server.semantic) != 0 {
		t.Fatalf("normal workspace analysis eagerly populated semantic cache: %#v", server.semantic)
	}
	if tokens := server.semanticTokens("file:///workspace/unopened.asp"); len(tokens.Data) != 0 || len(server.semantic) != 0 || physicalReads != 0 {
		t.Fatalf("unopened semantic request loaded source: tokens=%#v cache=%#v reads=%d", tokens, server.semantic, physicalReads)
	}
	closedURI := "file:///workspace/indexed-but-closed.asp"
	closed := core.NewTextDocument(closedURI, "classic-asp", 0, `<% Dim closedValue : closedValue = 1 %>`)
	server.workspace[closedURI] = closed
	if tokens := server.semanticTokens(closedURI); len(tokens.Data) != 0 || len(server.semantic) != 0 || physicalReads != 0 {
		t.Fatalf("workspace-indexed closed semantic request loaded source: tokens=%#v cache=%#v reads=%d", tokens, server.semantic, physicalReads)
	}
	if tokens := server.semanticTokensRange(closedURI, closed.Range(0, len(closed.Text))); len(tokens.Data) != 0 || len(server.semantic) != 0 || physicalReads != 0 {
		t.Fatalf("workspace-indexed closed semantic range loaded source: tokens=%#v cache=%#v reads=%d", tokens, server.semantic, physicalReads)
	}

	fullTokens := server.semanticTokens(firstURI)
	firstCached, ok := server.semantic[firstURI]
	if !ok || firstCached.Version != 1 {
		t.Fatalf("first full request did not populate revision cache: %#v", server.semantic)
	}
	if !reflect.DeepEqual(fullTokens.Data, firstCached.Tokens.Data) || fullTokens.ResultID != firstURI+"#1" {
		t.Fatalf("repeated full request did not reuse revision cache: full=%#v cached=%#v", fullTokens, firstCached.Tokens)
	}
	rangeTokens := server.semanticTokensRange(firstURI, first.Range(0, len(first.Text)))
	if len(rangeTokens.Data) == 0 {
		t.Fatalf("range semantic tokens = %#v", rangeTokens)
	}
	if cached := server.semantic[firstURI]; cached.Version != firstCached.Version || !reflect.DeepEqual(cached.Tokens.Data, firstCached.Tokens.Data) {
		t.Fatalf("range request replaced the full revision cache: before=%#v after=%#v", firstCached, cached)
	}

	delta, rpcErr := server.handleRequest(context.Background(), "textDocument/semanticTokens/full/delta", mustRaw(map[string]any{
		"textDocument":     map[string]any{"uri": firstURI},
		"previousResultId": fullTokens.ResultID,
	}))
	if rpcErr != nil {
		t.Fatal(rpcErr)
	}
	if result, ok := delta.(map[string]any); !ok || result["edits"] == nil {
		t.Fatalf("delta request did not reuse cached full result: %#v", delta)
	}

	secondTokens := server.semanticTokens(secondURI)
	secondCached := server.semantic[secondURI]
	if len(secondTokens.Data) == 0 || secondCached.Version != 1 || len(server.semantic) != 2 {
		t.Fatalf("second file semantic cache = %#v", server.semantic)
	}

	oneOffset := bytes.IndexByte([]byte(firstText), '1')
	changeRange := first.Range(oneOffset, oneOffset+1)
	if err := server.handleNotification(context.Background(), "textDocument/didChange", mustRaw(map[string]any{
		"textDocument": map[string]any{"uri": firstURI, "version": 2},
		"contentChanges": []map[string]any{{
			"range": changeRange,
			"text":  "2",
		}},
	})); err != nil {
		t.Fatal(err)
	}
	if _, ok := server.semantic[firstURI]; ok {
		t.Fatal("edited file retained its current semantic cache")
	}
	if cached, ok := server.semantic[secondURI]; !ok || !reflect.DeepEqual(cached.Tokens.Data, secondCached.Tokens.Data) {
		t.Fatalf("editing first file invalidated unrelated semantic cache: %#v", server.semantic)
	}
	updated := server.semanticTokens(firstURI)
	if updated.ResultID != firstURI+"#2" || server.semantic[firstURI].Version != 2 {
		t.Fatalf("edited file semantic cache was not rebuilt for its new revision: %#v", updated)
	}
	if physicalReads != 0 {
		t.Fatalf("semantic requests reread open overlays from disk: reads=%d", physicalReads)
	}
}

func assertNoVBSemanticTokenAnalysis(t *testing.T, parsed *core.ParsedDocument) {
	t.Helper()
	var tokens lsp.SemanticTokens
	if parsed.LoadAnalysis("vbscript.semantic-tokens.v1", &tokens) {
		t.Fatal("normal analysis computed or hydrated VBScript semantic tokens")
	}
}
