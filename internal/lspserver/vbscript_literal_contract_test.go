package lspserver

import (
	"io"
	"path/filepath"
	"strings"
	"testing"

	"github.com/yottonoko/classic-web-system-lsp/internal/core"
)

func TestVBScriptLiteralAssignmentsRejectValueAndPrimitiveMismatches(t *testing.T) {
	for _, test := range []struct{ name, annotation, accepted, rejected string }{
		{"case-sensitive string", `"Ready"`, `"Ready"`, `"ready"`},
		{"empty string", `""`, `""`, `" "`},
		{"escaped quotes", `"a""b"`, `"a""b"`, `"ab"`},
		{"Unicode", `"😀表示"`, `"😀表示"`, `"表示"`},
		{"numeric identity", "42", "4.2e1", "43"},
		{"numeric radix", "42", "&H2A", "&O53"},
		{"negative number", "-1", "-1", "1"},
		{"true identity", "True", "true", "False"},
		{"false identity", "False", "FALSE", "0"},
		{"mixed union", `"ready" | 200 | False`, "200", `"200"`},
	} {
		t.Run(test.name, func(t *testing.T) {
			source := "<%\n' @type value As " + test.annotation + "\nDim value\nvalue = " + test.accepted + "\nvalue = " + test.rejected + "\n%>"
			parsed := core.ParseDocument("file:///site/literal-types.asp", source, core.Settings{DefaultLanguage: "VBScript"})
			server := &Server{settings: serverSettings{VBScriptTypeChecking: "strict"}}
			diagnostics := server.vbscriptTypeDiagnostics(parsed)
			if len(diagnostics) != 1 || diagnostics[0].Code != "typeMismatch" || diagnostics[0].Range.Start.Line != 4 {
				t.Fatalf("literal assignment contract should reject only line 5: %#v", diagnostics)
			}
		})
	}
}

func TestVBScriptLiteralCompletionResolutionRespectsCursorAndScope(t *testing.T) {
	source := `<%
Dim route
route = "global.asp"
Response.Write route
route = ResolveAtRuntime()
Sub Render()
  Dim route
  route = 404
  Response.Write route
End Sub
Response.Write route
%>`
	uri := "file:///site/scoped-literals.asp"
	server := New(strings.NewReader(""), io.Discard, io.Discard)
	server.documents[uri] = core.NewTextDocument(uri, "classic-asp", 1, source)
	_, parsed := server.parsed(uri)
	for _, test := range []struct {
		offset int
		want   string
	}{
		{strings.Index(source, "Response.Write route") + len("Response.Write ro"), `route As "global.asp"`},
		{strings.Index(source, "  Response.Write route") + len("  Response.Write ro"), "route As 404"},
		{strings.LastIndex(source, "Response.Write route") + len("Response.Write ro"), "route As Variant"},
	} {
		found := false
		for _, item := range server.vbscriptSymbolCompletions(parsed, test.offset) {
			if item.Label != "route" {
				continue
			}
			found = true
			if resolved := server.resolveCompletionItem(item); resolved.Detail != test.want {
				t.Errorf("completion at %d = %q, want %q", test.offset, resolved.Detail, test.want)
			}
		}
		if !found {
			t.Fatalf("route completion missing at %d", test.offset)
		}
	}
}

func TestStdioLiteralTypeInformationInHoverCompletionAndHints(t *testing.T) {
	client := startStdioTestClient(t)
	defer client.close()
	root := t.TempDir()
	uri := pathToFileURI(filepath.Join(root, "literal-types.asp"))
	source := `<%
Dim literalText, literalNumber, literalFlag
literalText = "😀表示"
literalNumber = 42
literalFlag = False
Const literalConst = "ready"
' @type literalChoice As "ready" | 200 | True
Dim literalChoice
Response.Write literalText
Response.Write literalNumber
Response.Write literalFlag
Response.Write literalChoice
Response.Write literalConst
%>`
	client.request("initialize", map[string]any{"processId": nil, "rootUri": pathToFileURI(root), "capabilities": map[string]any{}})
	notifyConfiguration(t, client, map[string]any{"aspLsp": map[string]any{"inlayHints": map[string]any{"variableTypes": true}}})
	openClassicASPDocument(t, client, uri, source)
	for _, test := range []struct{ name, want string }{
		{"literalConst", "String"}, {"literalText", `"😀表示"`}, {"literalNumber", "42"}, {"literalFlag", "False"}, {"literalChoice", `"ready" | 200 | True`},
	} {
		t.Run(test.name, func(t *testing.T) {
			position := positionAt(source, strings.LastIndex(source, test.name)+2)
			hover := client.request("textDocument/hover", map[string]any{"textDocument": map[string]any{"uri": uri}, "position": position})
			want := test.name + " As " + test.want
			if text := hoverMarkdownValue(t, hover.Result); !strings.Contains(text, want) {
				t.Fatalf("hover missing %q: %s", want, text)
			}
			items := completionItems(client.request("textDocument/completion", map[string]any{"textDocument": map[string]any{"uri": uri}, "position": position}).Result)
			item, found := items.find(test.name)
			if !found {
				t.Fatalf("literal completion missing: %#v", completionItemLabels(items))
			}
			resolved := client.request("completionItem/resolve", item)
			encodedWant := strings.Trim(mustJSONText(t, want), `"`)
			if text := mustJSONText(t, resolved.Result); !strings.Contains(text, encodedWant) {
				t.Fatalf("resolved completion missing %q: %s", want, text)
			}
		})
	}
	hints := client.request("textDocument/inlayHint", map[string]any{"textDocument": map[string]any{"uri": uri}, "range": fullDocumentRange(source)})
	for _, want := range []string{`As "😀表示"`, "As 42", "As False", `As "ready" | 200 | True`} {
		if text := mustJSONText(t, hints.Result); !strings.Contains(text, strings.Trim(mustJSONText(t, want), `"`)) {
			t.Errorf("inlay hint missing %q: %s", want, text)
		}
	}
}
