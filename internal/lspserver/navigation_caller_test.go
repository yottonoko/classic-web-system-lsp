package lspserver

import (
	"context"
	"path/filepath"
	"strings"
	"testing"

	"github.com/yottonoko/classic-web-system-lsp/internal/core"
	"github.com/yottonoko/classic-web-system-lsp/internal/lsp"
	workspacepkg "github.com/yottonoko/classic-web-system-lsp/internal/workspace"
)

func TestNavigationCalledHelpersConnectEachOwningPageToItsAssignedTarget(t *testing.T) {
	for _, scope := range []string{"workspace", "folder", "document"} {
		t.Run(scope, func(t *testing.T) {
			root := t.TempDir()
			firstURI := filePathURI(filepath.Join(root, "orders", "first.asp"))
			secondURI := filePathURI(filepath.Join(root, "account", "second.asp"))
			helperURI := filePathURI(filepath.Join(root, "shared", "navigate.inc"))
			first := core.ParseDocument(firstURI, `<!-- #include file="../shared/navigate.inc" -->
<% target = "first-result.asp" : Call Navigate(target) %>`, core.Settings{})
			second := core.ParseDocument(secondURI, `<!-- #include file="../shared/navigate.inc" -->
<% target = "second-result.asp" : Call Navigate(target) %>`, core.Settings{})
			helper := core.ParseDocument(helperURI, `<%
Sub Navigate(destination)
  Call Forward(destination)
End Sub
Sub Forward(destination)
  Response.Redirect destination
End Sub
Function Uncalled()
  Response.Redirect "unused.asp"
End Function
%>`, core.Settings{})
			owners := map[string][]string{workspacepkg.FileIdentityKeyFromURI(helperURI): {firstURI, secondURI}}
			builder := newNavigationGraphBuilder(scope, firstURI, []workspaceRoot{{Path: root, URI: filePathURI(root)}})
			builder.prepareVBScriptFunctions([]*core.ParsedDocument{first, second, helper}, owners)
			builder.addDocument(first, firstURI)
			builder.addDocument(second, secondURI)
			builder.addDocument(helper, firstURI)
			if builder.navigationError != nil {
				t.Fatal(builder.navigationError)
			}
			want := map[string]string{firstURI: filePathURI(filepath.Join(root, "orders", "first-result.asp"))}
			if scope != "document" {
				want[secondURI] = filePathURI(filepath.Join(root, "account", "second-result.asp"))
			}
			if len(builder.edges) != len(want) {
				t.Fatalf("edges = %#v, want %d caller transitions", builder.edges, len(want))
			}
			for _, edge := range builder.edges {
				source := builder.nodeByID[navigationString(edge["source"])]
				target := builder.nodeByID[navigationString(edge["target"])]
				uri := navigationString(source["uri"])
				if target["uri"] != want[uri] {
					t.Fatalf("caller %q target = %#v, want %q", uri, target, want[uri])
				}
				evidence := edge["evidence"].([]map[string]any)
				if evidence[0]["uri"] != uri || !strings.Contains(navigationString(evidence[0]["snippet"]), "Navigate(target)") {
					t.Fatalf("caller evidence = %#v", evidence)
				}
			}
		})
	}
}

func TestNavigationIncludedSinksResolveRelativeToExecutingPage(t *testing.T) {
	for name, source := range map[string]string{
		"VBScript":       `<% Response.Redirect "next.asp" %>`,
		"generated HTML": `<% Response.Write "<a href=""next.asp"">Next</a>" %>`,
		"HTML":           `<a href="next.asp">Next</a>`,
		"JavaScript":     `<script>function navigate(target) { location.href = target; } navigate("next.asp");</script>`,
	} {
		t.Run(name, func(t *testing.T) {
			root := t.TempDir()
			pageURI := filePathURI(filepath.Join(root, "pages", "default.asp"))
			includeURI := filePathURI(filepath.Join(root, "includes", "nav.inc"))
			page := core.ParseDocument(pageURI, `<!-- #include file="../includes/nav.inc" -->`, core.Settings{})
			include := core.ParseDocument(includeURI, source, core.Settings{})
			builder := newNavigationGraphBuilder("workspace", "", []workspaceRoot{{Path: root, URI: filePathURI(root)}})
			builder.javascriptCandidates = typeScriptGoNavigationCandidates{ctx: context.Background()}
			builder.prepareVBScriptFunctions([]*core.ParsedDocument{page, include}, map[string][]string{workspacepkg.FileIdentityKeyFromURI(includeURI): {pageURI}})
			builder.addDocument(page, pageURI)
			if builder.navigationError != nil {
				t.Fatal(builder.navigationError)
			}
			want := filePathURI(filepath.Join(root, "pages", "next.asp"))
			if len(builder.edges) != 1 {
				t.Fatalf("edges = %#v, want one transition", builder.edges)
			}
			edge := builder.edges[0]
			target := builder.nodeByID[navigationString(edge["target"])]
			if target["uri"] != want {
				t.Fatalf("target = %#v, want executing-page relative URI %q", target, want)
			}
			if builder.nodeByID[navigationString(edge["source"])]["uri"] != pageURI {
				t.Fatalf("source = %#v", edge)
			}
			if edge["evidence"].([]map[string]any)[0]["uri"] != includeURI {
				t.Fatalf("include evidence = %#v", edge)
			}
		})
	}
}

func TestNavigationWorkspaceOmitsIncludeOnlyDocuments(t *testing.T) {
	root := t.TempDir()
	pageURI := filePathURI(filepath.Join(root, "default.asp"))
	helperURI := filePathURI(filepath.Join(root, "helper.asp"))
	fragmentURI := filePathURI(filepath.Join(root, "unused.inc"))
	page := core.ParseDocument(pageURI, `<!-- #include file="helper.asp" -->
<a href="next.asp">Next</a>`, core.Settings{})
	helper := core.ParseDocument(helperURI, `<% Function BuildTarget() : BuildTarget = "next.asp" : End Function %>`, core.Settings{})
	fragment := core.ParseDocument(fragmentURI, `<% Const APP_NAME = "Example" %>`, core.Settings{})
	builder := newNavigationGraphBuilder("workspace", "", []workspaceRoot{{Path: root, URI: filePathURI(root)}})
	builder.prepareVBScriptFunctions([]*core.ParsedDocument{page, helper, fragment}, map[string][]string{workspacepkg.FileIdentityKeyFromURI(helperURI): {pageURI}})
	for _, document := range []*core.ParsedDocument{page, helper, fragment} {
		builder.addDocument(document, document.URI)
	}
	payload := builder.payload(3)
	nodes := payload["nodes"].([]map[string]any)
	if len(nodes) != 2 {
		t.Fatalf("nodes = %#v, want source and destination pages only", nodes)
	}
	for _, node := range nodes {
		if node["uri"] == helperURI || node["uri"] == fragmentURI {
			t.Fatalf("include-only node = %#v", node)
		}
	}
}

func TestNavigationJavaScriptSharedHelpersUseCallerValuesAndSourceRanges(t *testing.T) {
	root := t.TempDir()
	helperURI := filePathURI(filepath.Join(root, "shared", "navigate.inc"))
	helper := core.ParseDocument(helperURI, `<script>function navigate(target) { forward(target); } function forward(target) { location.href = target; }</script>`, core.Settings{})
	firstURI := filePathURI(filepath.Join(root, "orders", "first.asp"))
	secondURI := filePathURI(filepath.Join(root, "account", "second.asp"))
	first := core.ParseDocument(firstURI, `<!-- #include file="../shared/navigate.inc" -->
<script>var target = "first-result.asp";</script>
<script>/* 😀 */ navigate(target); navigate("other.asp");</script>`, core.Settings{})
	second := core.ParseDocument(secondURI, `<!-- #include file="../shared/navigate.inc" -->
<script>navigate("second-result.asp");</script>`, core.Settings{})
	builder := newNavigationGraphBuilder("workspace", "", []workspaceRoot{{Path: root, URI: filePathURI(root)}})
	builder.javascriptCandidates = typeScriptGoNavigationCandidates{ctx: context.Background()}
	builder.prepareVBScriptFunctions([]*core.ParsedDocument{first, second, helper}, map[string][]string{workspacepkg.FileIdentityKeyFromURI(helperURI): {firstURI, secondURI}})
	for _, parsed := range []*core.ParsedDocument{first, second, helper} {
		builder.addDocument(parsed, parsed.URI)
	}
	if builder.navigationError != nil {
		t.Fatal(builder.navigationError)
	}
	want := map[string]string{
		filePathURI(filepath.Join(root, "orders", "first-result.asp")):   firstURI,
		filePathURI(filepath.Join(root, "orders", "other.asp")):          firstURI,
		filePathURI(filepath.Join(root, "account", "second-result.asp")): secondURI,
	}
	if len(builder.edges) != len(want) {
		t.Fatalf("edges = %#v, want three resolved caller transitions", builder.edges)
	}
	for _, edge := range builder.edges {
		target := navigationString(builder.nodeByID[navigationString(edge["target"])]["uri"])
		source := navigationString(builder.nodeByID[navigationString(edge["source"])]["uri"])
		if want[target] != source || source == "" {
			t.Fatalf("unexpected transition %q -> %q", source, target)
		}
		evidence := edge["evidence"].([]map[string]any)[0]
		if evidence["uri"] != source || !strings.Contains(navigationString(evidence["snippet"]), "navigate(") {
			t.Fatalf("caller evidence = %#v", evidence)
		}
		parsed := first
		if source == secondURI {
			parsed = second
		}
		doc := core.NewTextDocument(parsed.URI, "classic-asp", 0, parsed.Text)
		location := evidence["range"].(lsp.Range)
		text := parsed.Text[doc.OffsetAt(location.Start):doc.OffsetAt(location.End)]
		if !strings.HasPrefix(text, "navigate(") {
			t.Fatalf("mapped caller source = %q, range = %#v", text, location)
		}
	}
}

func TestNavigationSharedScriptInterpolationAndModules(t *testing.T) {
	for _, tc := range []struct {
		name, source string
		targets      []string
		unknown      bool
	}{
		{name: "interpolated caller", source: `<% target = "next.asp" %><script>function go(value) { location.href = value; }</script><script>const target = "<%= target %>";</script><script>/* 😀 */ go(target);</script>`, targets: []string{"next.asp"}},
		{name: "isolated module variables", source: `<script>var target = "global.asp"; function go(value) { location.href = value; }</script><script type="module">var target = "local.asp"; go(target);</script><script type="module">go(target);</script>`, targets: []string{"local.asp", "global.asp"}},
		{name: "module reads interpolated classic value", source: `<% target = "rendered.asp" %><script type="module">go(target);</script><script>var target = "<%= target %>"; function go(value) { location.assign(value); }</script>`, targets: []string{"rendered.asp"}},
		{name: "module local helper", source: `<script type="module">export function go(value) { location.assign(value); } const target = "local.asp"; go(target);</script><script type="module">go("must-not-leak.asp");</script>`, targets: []string{"local.asp"}},
		{name: "import shadows global", source: `<script>var target = "must-not-resolve.asp";</script><script type="module">import {target} from "./missing.js"; location.href = target;</script>`, unknown: true},
	} {
		t.Run(tc.name, func(t *testing.T) {
			root := t.TempDir()
			uri := filePathURI(filepath.Join(root, "page.asp"))
			parsed := core.ParseDocument(uri, tc.source, core.Settings{})
			b := newNavigationGraphBuilder("document", uri, []workspaceRoot{{Path: root, URI: filePathURI(root)}})
			b.javascriptCandidates = typeScriptGoNavigationCandidates{ctx: context.Background()}
			b.prepareVBScriptFunctions([]*core.ParsedDocument{parsed}, nil)
			b.addDocument(parsed, uri)
			if b.navigationError != nil {
				t.Fatal(b.navigationError)
			}
			wantCount := len(tc.targets)
			if tc.unknown {
				wantCount++
			}
			if len(b.edges) != wantCount {
				t.Fatalf("edges = %#v, want %d", b.edges, wantCount)
			}
			seen := map[string]bool{}
			for _, edge := range b.edges {
				target := b.nodeByID[navigationString(edge["target"])]
				seen[navigationString(target["uri"])] = true
				if tc.unknown && target["kind"] != "unknown" {
					t.Fatalf("import resolved to global: %#v", target)
				}
				for _, evidence := range edge["evidence"].([]map[string]any) {
					if evidence["uri"] != uri {
						t.Fatalf("source URI = %#v", evidence)
					}
					r := evidence["range"].(lsp.Range)
					doc := core.NewTextDocument(uri, "classic-asp", 0, tc.source)
					text := tc.source[doc.OffsetAt(r.Start):doc.OffsetAt(r.End)]
					if text == "" || strings.Contains(text, "(function ()") {
						t.Fatalf("invalid original source: %q", text)
					}
				}
			}
			for _, target := range tc.targets {
				if !seen[filePathURI(filepath.Join(root, target))] {
					t.Fatalf("missing %q: %#v", target, b.edges)
				}
			}
		})
	}
}

func TestNavigationSharedScriptInterpolationKeepsIncludeEvidence(t *testing.T) {
	root := t.TempDir()
	uri := filePathURI(filepath.Join(root, "page.asp"))
	includeURI := filePathURI(filepath.Join(root, "value.inc"))
	page := core.ParseDocument(uri, `<% target = "one.asp" %><script>function go(value) { location.href = value; }</script><!-- #include file="value.inc" --><script>go(target);</script>`, core.Settings{})
	include := core.ParseDocument(includeURI, `<script>var target = "<%= target %>";</script>`, core.Settings{})
	b := newNavigationGraphBuilder("document", uri, []workspaceRoot{{Path: root, URI: filePathURI(root)}})
	b.javascriptCandidates = typeScriptGoNavigationCandidates{ctx: context.Background()}
	b.prepareVBScriptFunctions([]*core.ParsedDocument{page, include}, map[string][]string{workspacepkg.FileIdentityKeyFromURI(includeURI): {uri}})
	b.addDocument(page, uri)
	if b.navigationError != nil {
		t.Fatal(b.navigationError)
	}
	if len(b.edges) != 1 {
		t.Fatalf("edges = %#v", b.edges)
	}
	edge := b.edges[0]
	if b.nodeByID[navigationString(edge["target"])]["uri"] != filePathURI(filepath.Join(root, "one.asp")) {
		t.Fatalf("target = %#v", edge)
	}
	evidence := edge["evidence"].([]map[string]any)
	if len(evidence) != 2 || evidence[0]["uri"] != uri || evidence[1]["uri"] != includeURI || !strings.Contains(navigationString(evidence[1]["snippet"]), "<%= target %>") || strings.Contains(navigationString(evidence[1]["snippet"]), "function go") {
		t.Fatalf("evidence = %#v", evidence)
	}
}
