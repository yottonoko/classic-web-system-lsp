package lspserver

import (
	"io"
	"path/filepath"
	"strings"
	"testing"

	"github.com/yottonoko/classic-web-system-lsp/internal/core"
	"github.com/yottonoko/classic-web-system-lsp/internal/lsp"
)

func TestParameterHoverResolvesReferencesByProcedureAndClassScope(t *testing.T) {
	source := `<%
' @param Root.rp As String
' @param Root.second As Number
Function Root(ByVal rp, Optional ByRef second)
  Response.Write rp
  Response.Write second
End Function
Sub Other(ByRef value)
  Response.Write value
End Sub
Function Defaulted(Optional ByVal defaultValue = "fallback")
  Response.Write defaultValue
End Function
Class Widget
  ' @param Method.cp As String
  Public Function Method(ByVal cp)
    Response.Write cp
    Response.Write cp
  End Function
End Class
Class Gadget
  ' @param Method.cp As Number
  Public Function Method(ByVal cp)
    Response.Write cp
  End Function
End Class
%>`
	uri := "file:///tmp/parameter-scope-hover.asp"
	doc := core.NewTextDocument(uri, "classic-asp", 1, source)
	server := New(strings.NewReader(""), io.Discard, io.Discard)
	server.documents[uri] = doc

	cases := []struct {
		name       string
		needle     string
		occurrence int
		nameOffset int
		nameLength int
		want       string
	}{
		{name: "function first parameter declaration", needle: "ByVal rp", occurrence: 0, nameOffset: len("ByVal "), nameLength: 2, want: "ByVal rp As String"},
		{name: "function first parameter reference", needle: "Response.Write rp", occurrence: 0, nameOffset: len("Response.Write "), nameLength: 2, want: "ByVal rp As String"},
		{name: "function second parameter reference", needle: "Response.Write second", occurrence: 0, nameOffset: len("Response.Write "), nameLength: len("second"), want: "ByRef second As Number"},
		{name: "sub parameter reference", needle: "Response.Write value", occurrence: 0, nameOffset: len("Response.Write "), nameLength: len("value"), want: "ByRef value As Variant"},
		{name: "optional default parameter reference", needle: "Response.Write defaultValue", occurrence: 0, nameOffset: len("Response.Write "), nameLength: len("defaultValue"), want: "ByVal defaultValue As Variant"},
		{name: "first class parameter reference", needle: "Response.Write cp", occurrence: 0, nameOffset: len("Response.Write "), nameLength: 2, want: "ByVal cp As String"},
		{name: "first class parameter second reference", needle: "Response.Write cp", occurrence: 1, nameOffset: len("Response.Write "), nameLength: 2, want: "ByVal cp As String"},
		{name: "second class parameter reference", needle: "Response.Write cp", occurrence: 2, nameOffset: len("Response.Write "), nameLength: 2, want: "ByVal cp As Number"},
	}
	for _, test := range cases {
		t.Run(test.name, func(t *testing.T) {
			offset := nthIndex(source, test.needle, test.occurrence)
			if offset < 0 {
				t.Fatalf("%q occurrence %d not found", test.needle, test.occurrence)
			}
			offset += test.nameOffset
			position := doc.PositionAt(offset)
			hover, ok := server.hover(uri, position).(*lsp.Hover)
			if !ok || hover == nil {
				t.Fatalf("hover = %#v, want parameter hover", hover)
			}
			value, ok := hover.Contents.(lsp.MarkupContent)
			if !ok || !strings.Contains(value.Value, test.want) {
				t.Fatalf("hover contents = %#v, want %q", hover.Contents, test.want)
			}
			if hover.Range == nil {
				t.Fatal("parameter hover range is nil")
			}
			wantRange := doc.Range(offset, offset+test.nameLength)
			if *hover.Range != wantRange {
				t.Fatalf("hover range = %#v, want request range %#v", *hover.Range, wantRange)
			}
		})
	}
}

func TestParameterHoverAllowsLocalDimToWinWhenItShadowsParameter(t *testing.T) {
	source := `<%
Function Shadow(ByVal value)
  Dim value
  Response.Write value
End Function
%>`
	uri := "file:///tmp/parameter-local-shadow-hover.asp"
	doc := core.NewTextDocument(uri, "classic-asp", 1, source)
	server := New(strings.NewReader(""), io.Discard, io.Discard)
	server.documents[uri] = doc

	offset := strings.Index(source, "Response.Write value") + len("Response.Write ")
	hover, ok := server.hover(uri, doc.PositionAt(offset)).(*lsp.Hover)
	if !ok || hover == nil {
		t.Fatalf("local shadow hover = %#v, want hover", hover)
	}
	value, ok := hover.Contents.(lsp.MarkupContent)
	if !ok || !strings.Contains(value.Value, "(local) Dim value As Variant") || strings.Contains(value.Value, "ByVal value") {
		t.Fatalf("local shadow hover = %#v, want local variable", hover.Contents)
	}
}

func TestParameterHoverWinsOverIncludedNameCollision(t *testing.T) {
	root := t.TempDir()
	ownerURI := filePathURI(filepath.Join(root, "default.asp"))
	includeURI := filePathURI(filepath.Join(root, "common.inc"))
	ownerSource := `<!-- #include file="common.inc" -->
<%
Function Use(ByVal shared)
  Response.Write shared
End Function
%>`
	includeSource := `<%
shared = "included"
%>`
	server := New(strings.NewReader(""), io.Discard, io.Discard)
	server.rootPath = root
	server.rootURI = filePathURI(root)
	server.workspaceRoots = []workspaceRoot{{Path: root, URI: server.rootURI}}
	server.documents[ownerURI] = core.NewTextDocument(ownerURI, "classic-asp", 1, ownerSource)
	server.documents[includeURI] = core.NewTextDocument(includeURI, "classic-asp", 1, includeSource)

	offset := strings.Index(ownerSource, "Response.Write shared") + len("Response.Write ")
	doc := server.documents[ownerURI]
	hover, ok := server.hover(ownerURI, doc.PositionAt(offset)).(*lsp.Hover)
	if !ok || hover == nil {
		t.Fatalf("included collision hover = %#v, want parameter hover", hover)
	}
	value, ok := hover.Contents.(lsp.MarkupContent)
	if !ok || !strings.Contains(value.Value, "ByVal shared As Variant") || strings.Contains(value.Value, "included") {
		t.Fatalf("included collision hover = %#v, want parameter metadata", hover.Contents)
	}
}

func TestParameterHoverUsesDocumentationFromResolvedClassMethod(t *testing.T) {
	source := `<%
Class Widget
  ''' <param name="cp">Widget parameter documentation.</param>
  Public Function Method(ByVal cp)
    Response.Write cp
  End Function
End Class
Class Gadget
  ''' <param name="cp">Gadget parameter documentation.</param>
  Public Function Method(ByVal cp)
    Response.Write cp
  End Function
End Class
%>`
	uri := "file:///tmp/parameter-scoped-xml-doc-hover.asp"
	doc := core.NewTextDocument(uri, "classic-asp", 1, source)
	server := New(strings.NewReader(""), io.Discard, io.Discard)
	server.documents[uri] = doc

	for _, test := range []struct {
		name       string
		occurrence int
		want       string
		reject     string
	}{
		{name: "widget reference", occurrence: 0, want: "Widget parameter documentation.", reject: "Gadget parameter documentation."},
		{name: "gadget reference", occurrence: 1, want: "Gadget parameter documentation.", reject: "Widget parameter documentation."},
	} {
		t.Run(test.name, func(t *testing.T) {
			offset := nthIndex(source, "Response.Write cp", test.occurrence) + len("Response.Write ")
			hover, ok := server.hover(uri, doc.PositionAt(offset)).(*lsp.Hover)
			if !ok || hover == nil {
				t.Fatalf("hover = %#v, want scoped parameter hover", hover)
			}
			value, ok := hover.Contents.(lsp.MarkupContent)
			if !ok || !strings.Contains(value.Value, test.want) || strings.Contains(value.Value, test.reject) {
				t.Fatalf("hover contents = %#v, want %q without %q", hover.Contents, test.want, test.reject)
			}
		})
	}
}

func TestStdioParameterReferenceHoverReturnsLocalMetadata(t *testing.T) {
	client := startStdioTestClient(t)
	defer client.close()

	root := t.TempDir()
	uri := pathToFileURI(filepath.Join(root, "parameter-reference.asp"))
	source := `<%
Function Root(ByVal rp)
  Response.Write "日本語" & rp
  Response.Write rp
End Function
Sub RootSub(ByRef rq)
  Response.Write rq
End Sub
Class Widget
  Public Function Method(ByVal cp)
    Response.Write cp
  End Function
End Class
%>`
	client.request("initialize", map[string]any{
		"processId":    nil,
		"rootUri":      pathToFileURI(root),
		"capabilities": map[string]any{},
	})
	openClassicASPDocument(t, client, uri, source)
	doc := core.NewTextDocument(uri, "classic-asp", 1, source)

	cases := []struct {
		name       string
		needle     string
		nameLength int
		want       string
	}{
		{name: "function first reference", needle: `& rp`, nameLength: 2, want: "ByVal rp As Variant"},
		{name: "function second reference", needle: "Response.Write rp", nameLength: 2, want: "ByVal rp As Variant"},
		{name: "sub reference", needle: "Response.Write rq", nameLength: 2, want: "ByRef rq As Variant"},
		{name: "class method reference", needle: "Response.Write cp", nameLength: 2, want: "ByVal cp As Variant"},
	}
	for _, test := range cases {
		t.Run(test.name, func(t *testing.T) {
			offset := strings.Index(source, test.needle)
			if test.name == "function first reference" {
				offset += len(test.needle) - 2
			} else {
				offset += len(test.needle) - len(strings.TrimSpace(test.needle[strings.LastIndex(test.needle, " ")+1:]))
			}
			response := client.request("textDocument/hover", map[string]any{
				"textDocument": map[string]any{"uri": uri},
				"position":     positionAt(source, offset),
			})
			if response.Result == nil || mustJSONText(t, response.Result) == "null" {
				t.Fatalf("stdio hover result = %s, want parameter hover", mustJSONText(t, response.Result))
			}
			value := hoverMarkdownValue(t, response.Result)
			if !strings.Contains(value, test.want) {
				t.Fatalf("stdio hover contents = %q, want %q", value, test.want)
			}
			var hover lsp.Hover
			mustDecodeResult(t, response.Result, &hover)
			if hover.Range == nil {
				t.Fatal("stdio parameter hover range is nil")
			}
			wantRange := lsp.Range{Start: doc.PositionAt(offset), End: doc.PositionAt(offset + test.nameLength)}
			if *hover.Range != wantRange {
				t.Fatalf("stdio parameter hover range = %#v, want UTF-16 request range %#v", *hover.Range, wantRange)
			}
		})
	}
}

func nthIndex(text, needle string, occurrence int) int {
	start := 0
	for index := 0; index <= occurrence; index++ {
		found := strings.Index(text[start:], needle)
		if found < 0 {
			return -1
		}
		start += found
		if index == occurrence {
			return start
		}
		start += len(needle)
	}
	return -1
}
