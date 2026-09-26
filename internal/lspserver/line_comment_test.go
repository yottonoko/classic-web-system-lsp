package lspserver

import (
	"context"
	"encoding/json"
	"io"
	"strings"
	"testing"

	"github.com/yottonoko/classic-web-system-lsp/internal/core"
	"github.com/yottonoko/classic-web-system-lsp/internal/lsp"
)

func TestLineCommentEditsRequestUsesMatchingOpenDocumentVersion(t *testing.T) {
	server := New(strings.NewReader(""), io.Discard, io.Discard)
	defer server.shutdownRuntimeCaches()
	uri := "file:///site/default.asp"
	source := `<p><%= title %></p>`
	server.documents[uri] = core.NewTextDocument(uri, "classic-asp", 7, source)

	result, rpcErr := server.handleRequest(context.Background(), "aspLsp/textDocument/lineCommentEdits", mustRaw(map[string]any{
		"textDocument": map[string]any{"uri": uri, "version": 7},
		"selections": []lsp.Range{{
			Start: lsp.Position{},
			End:   core.NewTextDocument(uri, "classic-asp", 7, source).PositionAt(len(source)),
		}},
	}))
	if rpcErr != nil {
		t.Fatalf("line comment request failed: %#v", rpcErr)
	}
	response, ok := result.(*lineCommentEditsResult)
	if !ok || response.Version != 7 || len(response.Edits) != 1 {
		t.Fatalf("line comment result = %#v", result)
	}
	if response.Edits[0].NewText != `<!-- <p><%'= title %></p> -->` {
		t.Fatalf("line comment edit = %#v", response.Edits[0])
	}
}

func TestLineCommentEditsRequestUsesVSCodeRawHTMLCommentDelimiters(t *testing.T) {
	server := New(strings.NewReader(""), io.Discard, io.Discard)
	defer server.shutdownRuntimeCaches()
	uri := "file:///site/default.asp"
	source := `<p>arrow --> text</p>`
	server.documents[uri] = core.NewTextDocument(uri, "classic-asp", 8, source)

	result, rpcErr := server.handleRequest(context.Background(), "aspLsp/textDocument/lineCommentEdits", mustRaw(map[string]any{
		"textDocument": map[string]any{"uri": uri, "version": 8},
		"selections":   []lsp.Range{{Start: lsp.Position{}, End: core.NewTextDocument(uri, "classic-asp", 8, source).PositionAt(len(source))}},
	}))
	if rpcErr != nil {
		t.Fatalf("line comment request failed: %#v", rpcErr)
	}
	response, ok := result.(*lineCommentEditsResult)
	if !ok || response.Version != 8 || len(response.Edits) != 1 {
		t.Fatalf("line comment result = %#v", result)
	}
	if response.Edits[0].NewText != `<!-- <p>arrow --> text</p> -->` {
		t.Fatalf("line comment edit = %#v", response.Edits[0])
	}
}

func TestLineCommentEditsRequestRejectsMissingOrStaleDocument(t *testing.T) {
	server := New(strings.NewReader(""), io.Discard, io.Discard)
	defer server.shutdownRuntimeCaches()
	uri := "file:///site/default.asp"
	server.documents[uri] = core.NewTextDocument(uri, "classic-asp", 4, "<p>ready</p>")

	for _, test := range []struct {
		name    string
		uri     string
		version int
	}{
		{name: "stale", uri: uri, version: 3},
		{name: "missing", uri: "file:///site/missing.asp", version: 4},
	} {
		t.Run(test.name, func(t *testing.T) {
			result, rpcErr := server.handleRequest(context.Background(), "aspLsp/textDocument/lineCommentEdits", mustRaw(map[string]any{
				"textDocument": map[string]any{"uri": test.uri, "version": test.version},
				"selections":   []lsp.Range{{}},
			}))
			if rpcErr != nil || result != nil {
				t.Fatalf("stale request result = %#v, error = %#v", result, rpcErr)
			}
		})
	}
}

func TestLineCommentEditsRequestReportsPartialDirectiveNoOp(t *testing.T) {
	server := New(strings.NewReader(""), io.Discard, io.Discard)
	defer server.shutdownRuntimeCaches()
	uri := "file:///site/default.asp"
	source := "<%@ LANGUAGE=\"JScript\" %>\n<%= title %>"
	server.documents[uri] = core.NewTextDocument(uri, "classic-asp", 4, source)
	result, rpcErr := server.handleRequest(context.Background(), "aspLsp/textDocument/lineCommentEdits", mustRaw(map[string]any{
		"textDocument": map[string]any{"uri": uri, "version": 4},
		"selections":   []lsp.Range{rangeForNeedleLineComment(t, source, "LANGUAGE")},
	}))
	if rpcErr != nil {
		t.Fatalf("line comment request failed: %#v", rpcErr)
	}
	response, ok := result.(*lineCommentEditsResult)
	if !ok || len(response.Edits) != 0 || response.NoOpReason != "directive-requires-full-document" {
		t.Fatalf("line comment result = %#v", result)
	}
}

func TestLineCommentEditsRequestReportsPartialBlockBoundaryNoOp(t *testing.T) {
	server := New(strings.NewReader(""), io.Discard, io.Discard)
	defer server.shutdownRuntimeCaches()
	uri := "file:///site/default.asp"
	source := "<!--\nfoo\n-->"
	server.documents[uri] = core.NewTextDocument(uri, "classic-asp", 4, source)
	result, rpcErr := server.handleRequest(context.Background(), "aspLsp/textDocument/lineCommentEdits", mustRaw(map[string]any{
		"textDocument": map[string]any{"uri": uri, "version": 4},
		"selections":   []lsp.Range{{Start: lsp.Position{Line: 0}, End: lsp.Position{Line: 2}}},
	}))
	if rpcErr != nil {
		t.Fatalf("line comment request failed: %#v", rpcErr)
	}
	response, ok := result.(*lineCommentEditsResult)
	if !ok || len(response.Edits) != 0 || response.NoOpReason != "unsafe-structure" {
		t.Fatalf("line comment result = %#v", result)
	}
}

func TestLineCommentEditsRequestTogglesExistingVBScriptComments(t *testing.T) {
	server := New(strings.NewReader(""), io.Discard, io.Discard)
	defer server.shutdownRuntimeCaches()
	uri := "file:///site/default.asp"
	text := "<% a %>"
	for _, test := range []struct {
		version int
		want    string
	}{{version: 1, want: "<%' a %>"}, {version: 2, want: "<% a %>"}} {
		version, want := test.version, test.want
		server.documents[uri] = core.NewTextDocument(uri, "classic-asp", version, text)
		result, rpcErr := server.handleRequest(context.Background(), "aspLsp/textDocument/lineCommentEdits", mustRaw(map[string]any{
			"textDocument": map[string]any{"uri": uri, "version": version},
			"selections":   []lsp.Range{{Start: lsp.Position{}, End: core.NewTextDocument(uri, "classic-asp", version, text).PositionAt(len(text))}},
		}))
		if rpcErr != nil {
			t.Fatalf("version %d request failed: %#v", version, rpcErr)
		}
		response, ok := result.(*lineCommentEditsResult)
		if !ok || len(response.Edits) != 1 || response.Edits[0].NewText != want {
			t.Fatalf("version %d toggled comment = %#v, want %q", version, result, want)
		}
		text = response.Edits[0].NewText
	}
}

func TestLineCommentEditsRequestSerializesNoOpEditsAsAnEmptyArray(t *testing.T) {
	server := New(strings.NewReader(""), io.Discard, io.Discard)
	defer server.shutdownRuntimeCaches()
	uri := "file:///site/default.asp"
	for version, source := range map[int]string{1: "\n", 2: "%> <%"} {
		server.documents[uri] = core.NewTextDocument(uri, "classic-asp", version, source)
		result, rpcErr := server.handleRequest(context.Background(), "aspLsp/textDocument/lineCommentEdits", mustRaw(map[string]any{
			"textDocument": map[string]any{"uri": uri, "version": version},
			"selections":   []lsp.Range{{}},
		}))
		if rpcErr != nil {
			t.Fatalf("version %d request failed: %#v", version, rpcErr)
		}
		response, ok := result.(*lineCommentEditsResult)
		if !ok || response.Edits == nil || len(response.Edits) != 0 {
			t.Fatalf("version %d no-op result = %#v", version, result)
		}
		serialized, err := json.Marshal(response)
		if err != nil {
			t.Fatalf("version %d response serialization failed: %v", version, err)
		}
		if !strings.Contains(string(serialized), `"edits":[]`) {
			t.Fatalf("version %d serialized no-op result = %s", version, serialized)
		}
	}
}

func rangeForNeedleLineComment(t *testing.T, text, needle string) lsp.Range {
	t.Helper()
	offset := strings.Index(text, needle)
	if offset < 0 {
		t.Fatalf("needle %q not found", needle)
	}
	doc := core.NewTextDocument("file:///site/default.asp", "classic-asp", 0, text)
	return lsp.Range{Start: doc.PositionAt(offset), End: doc.PositionAt(offset + len(needle))}
}

func TestLineCommentEditsRequestUsesInteractiveQueueOrdering(t *testing.T) {
	if serialLSPRequest("aspLsp/textDocument/lineCommentEdits", nil) {
		t.Fatal("line comment request must not wait for unrelated bulk requests")
	}
	if class := classifyRequestWork("aspLsp/textDocument/lineCommentEdits", nil); class != requestWorkInteractive {
		t.Fatalf("line comment request class = %d, want interactive", class)
	}
}

func TestStdioLineCommentEditsRequestReturnsVersionedMixedLanguageEdit(t *testing.T) {
	client := startStdioTestClient(t)
	defer client.close()
	uri := "file:///site/default.asp"
	source := `<script>const x=<%=v%>;</script>`
	client.request("initialize", map[string]any{"capabilities": map[string]any{}})
	notifyOpenClassicASPDocument(t, client, uri, source)

	message := client.request("aspLsp/textDocument/lineCommentEdits", map[string]any{
		"textDocument": map[string]any{"uri": uri, "version": 1},
		"selections": []lsp.Range{{
			Start: lsp.Position{},
			End:   core.NewTextDocument(uri, "classic-asp", 1, source).PositionAt(len(source)),
		}},
	})
	var result lineCommentEditsResult
	if err := json.Unmarshal([]byte(mustJSONText(t, message.Result)), &result); err != nil {
		t.Fatal(err)
	}
	if result.Version != 1 || len(result.Edits) != 1 {
		t.Fatalf("stdio line comment result = %#v", result)
	}
	want := `<!-- <script>const x=<%'=v%>;</script> -->`
	if result.Edits[0].NewText != want {
		t.Fatalf("stdio line comment edit = %q, want %q", result.Edits[0].NewText, want)
	}
}

func TestLineCommentEditsRequestSplitsAroundIncludeDirective(t *testing.T) {
	server := New(strings.NewReader(""), io.Discard, io.Discard)
	defer server.shutdownRuntimeCaches()
	uri := "file:///site/default.asp"
	source := `<header>ready</header>
<!-- #include file="shared/header.inc" -->
<% Response.Write ready %>
<footer>done</footer>`
	server.documents[uri] = core.NewTextDocument(uri, "classic-asp", 11, source)

	result, rpcErr := server.handleRequest(context.Background(), "aspLsp/textDocument/lineCommentEdits", mustRaw(map[string]any{
		"textDocument": map[string]any{"uri": uri, "version": 11},
		"selections":   []lsp.Range{{Start: lsp.Position{}, End: core.NewTextDocument(uri, "classic-asp", 11, source).PositionAt(len(source))}},
	}))
	if rpcErr != nil {
		t.Fatalf("line comment request failed: %#v", rpcErr)
	}
	response, ok := result.(*lineCommentEditsResult)
	if !ok || response.Version != 11 || len(response.Edits) != 1 {
		t.Fatalf("line comment result = %#v", result)
	}
	want := `<!-- <header>ready</header> -->
<!-- asp-lsp-disabled-include:#include file="shared/header.inc" -->
<!-- <%' Response.Write ready %>
<footer>done</footer> -->`
	if response.Edits[0].NewText != want {
		t.Fatalf("line comment edit:\n got:\n%s\nwant:\n%s", response.Edits[0].NewText, want)
	}
}
