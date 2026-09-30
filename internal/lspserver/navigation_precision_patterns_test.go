package lspserver

import (
	"context"
	"os"
	"path/filepath"
	"sort"
	"strings"
	"testing"

	"github.com/yottonoko/classic-web-system-lsp/internal/core"
)

// TestNavigationRecognizesCommonClassicASPTransitions covers transitions that
// Classic ASP pages commonly express outside plain anchors and forms.
func TestNavigationRecognizesCommonClassicASPTransitions(t *testing.T) {
	for _, test := range []struct {
		name, source string
		want         []string
	}{
		{"redirect keeps path with unknown query operand", `<% Response.Redirect("next.asp?x=" & id) %>`, []string{"next.asp serverRedirect possible"}},
		{"redirect keeps path through Server.URLEncode", `<% Response.Redirect "next.asp?id=" & Server.URLEncode(id) %>`, []string{"next.asp serverRedirect possible"}},
		{"redirect inside With Response", "<%\nWith Response\n  .Redirect \"next.asp\"\nEnd With\n%>", []string{"next.asp serverRedirect certain"}},
		{"anchor with server query value", `<a href='next.asp?id=<%=id%>'>x</a>`, []string{"next.asp htmlAnchor possible"}},
		{"onclick location", `<input type="button" onclick="location.href='next.asp'">`, []string{"next.asp javascriptLocation probable"}},
		{"onclick with entities", `<button type="button" onclick="location.href=&quot;next.asp&quot;">b</button>`, []string{"next.asp javascriptLocation probable"}},
		{"onclick rendered server literal", `<% nextPage = "next.asp" %><button type="button" onclick="location.href='<%= nextPage %>'">b</button>`, []string{"next.asp javascriptLocation probable"}},
		{"onclick calls page function", `<script>function goPage(p){ location.href = p; }</script><input type="button" onclick="goPage('next.asp')">`, []string{"next.asp javascriptLocation probable", "p javascriptLocation unknown"}},
		{"onclick submits form", `<form name="f1" method="post" action="save.asp"><input type="button" onclick="document.f1.action='list.asp'; document.f1.submit();"></form>`, []string{"list.asp javascriptFormSubmit probable", "save.asp htmlForm certain"}},
		{"javascript href", `<a href="javascript:location.href='next.asp'">x</a>`, []string{"next.asp javascriptLocation probable"}},
		{"javascript void href", `<a href="javascript:void(0)">x</a>`, nil},
		{"top and parent frames", `<script>top.location.href = 'next.asp'; parent.location = 'list.asp';</script>`, []string{"list.asp javascriptLocation probable", "next.asp javascriptLocation probable"}},
		{"concatenated JavaScript query", `<script>function go(id){ location.href = 'next.asp?id=' + id; }</script>`, []string{"next.asp javascriptLocation possible"}},
		{"template JavaScript query", "<script>function go(id){ location.href = `next.asp?id=${id}`; }</script>", []string{"next.asp javascriptLocation possible"}},
		{"indexed form submit", `<form method="post"></form><script>function go(){ document.forms[0].action = "save.asp"; document.forms[0].submit(); }</script>`, []string{"default.asp htmlForm certain", "save.asp javascriptFormSubmit probable"}},
		{"Response.Write script", `<% Response.Write "<script>location.href='next.asp';</script>" %>`, []string{"next.asp javascriptLocation probable"}},
	} {
		t.Run(test.name, func(t *testing.T) {
			got := navigationPatternEdges(t, test.source)
			want := append([]string(nil), test.want...)
			sort.Strings(want)
			if strings.Join(got, "\n") != strings.Join(want, "\n") {
				t.Fatalf("edges =\n%s\nwant\n%s", strings.Join(got, "\n"), strings.Join(want, "\n"))
			}
		})
	}
}

func navigationPatternEdges(t *testing.T, source string) []string {
	t.Helper()
	root := t.TempDir()
	for _, name := range []string{"next.asp", "save.asp", "list.asp"} {
		if err := os.WriteFile(filepath.Join(root, name), nil, 0o644); err != nil {
			t.Fatal(err)
		}
	}
	parsed := core.ParseDocument(filePathURI(filepath.Join(root, "default.asp")), source, core.Settings{DefaultLanguage: "VBScript"})
	builder := newNavigationGraphBuilder("document", parsed.URI, []workspaceRoot{{Path: root, URI: filePathURI(root)}})
	builder.cancelContext = context.Background()
	builder.javascriptCandidates = typeScriptGoNavigationCandidates{ctx: context.Background()}
	if err := builder.prepareVBScriptFunctionsWithIncludesContext(context.Background(), []*core.ParsedDocument{parsed}, map[string][]string{}, nil); err != nil {
		t.Fatal(err)
	}
	builder.addDocument(parsed, parsed.URI)
	if builder.navigationError != nil {
		t.Fatalf("navigation error = %v", builder.navigationError)
	}
	got := make([]string, 0, len(builder.edges))
	for _, edge := range builder.edges {
		target := builder.nodeByID[navigationString(edge["target"])]
		got = append(got, navigationString(target["label"])+" "+navigationString(edge["kind"])+" "+navigationString(edge["confidence"]))
	}
	sort.Strings(got)
	return got
}
