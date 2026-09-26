package lspserver

import (
	"context"
	"path/filepath"
	"strings"
	"testing"

	"github.com/yottonoko/classic-web-system-lsp/internal/core"
	"github.com/yottonoko/classic-web-system-lsp/internal/lsp"
)

func TestNavigationVBStringBuildersResolveRedirects(t *testing.T) {
	for _, test := range []struct{ expression, want string }{
		{`Replace("old.asp", "old", "new")`, "new.asp"},
		{`Left("next.asp?debug=1", 8)`, "next.asp"},
		{`Right("prefix/next.asp", 8)`, "next.asp"},
		{`Mid("prefix/next.asp", 8)`, "next.asp"},
		{`Mid("prefix/next.asp?x", 8, 8)`, "next.asp"},
		{`LTrim("  next.asp")`, "next.asp"},
		{`RTrim("next.asp  ")`, "next.asp"},
		{`"next" & Chr(46) & "asp"`, "next.asp"},
		{`ChrW(34920) & "示.asp"`, "表示.asp"},
		{`Mid("😀next.asp", 3)`, "next.asp"},
		{`Replace("next.html.html", ".html", ".asp", 1, 1)`, "next.asp.html"},
		{`Replace("/next.html", ".html", ".asp", 2, -1, 0)`, "next.asp"},
	} {
		t.Run(test.expression, func(t *testing.T) {
			source := "<% Response.Redirect " + test.expression + " %>"
			candidates := extractVBScriptNavigationCandidates(source[2:len(source)-2], 2, source)
			if len(candidates) != 1 || candidates[0].Value.Kind != navigationVBValueLiteral || candidates[0].Value.Text != test.want {
				t.Fatalf("redirect for %s = %#v, want %q", test.expression, candidates, test.want)
			}
		})
	}
}

func TestNavigationVBStringBuildersBoundExpansionAndCancellation(t *testing.T) {
	source := "<% Response.Redirect Replace(" + strings.Repeat(`"x",`, 1000) + `"y") %>`
	candidates := extractVBScriptNavigationCandidates(source[2:len(source)-2], 2, source)
	if len(candidates) != 1 || candidates[0].Value.Kind != navigationVBValueUnknown {
		t.Fatalf("invalid arity should remain unresolved: %#v", candidates)
	}
	literal := func(text string) navigationVBValue {
		return navigationVBValue{Kind: navigationVBValueLiteral, Primitive: navigationPrimitiveString, Text: text}
	}
	state := newNavigationVBState()
	if _, ok := navigationVBTransformString("replace", []navigationVBValue{literal(strings.Repeat("x", 8192)), literal("x"), literal(strings.Repeat("y", 8192))}, state); ok {
		t.Fatal("oversized replacement result should remain unresolved")
	}
	ctx, cancel := context.WithCancel(context.Background())
	cancel()
	state = newNavigationVBState()
	state.cancelContext = ctx
	if _, ok := navigationVBTransformString("ltrim", []navigationVBValue{literal(" next.asp")}, state); ok {
		t.Fatal("cancelled string evaluation returned a result")
	}
}

func TestNavigationJavaScriptUnknownEvidenceRetainsWhitespace(t *testing.T) {
	root := t.TempDir()
	source := "<script>\nwindow.location.href = resolveTarget(\"a  b\",\n  runtimeValue);\n</script>"
	parsed := core.ParseDocument(filePathURI(filepath.Join(root, "default.asp")), source, core.Settings{})
	builder := newNavigationGraphBuilder("document", parsed.URI, []workspaceRoot{{Path: root, URI: filePathURI(root)}})
	builder.javascriptCandidates = typeScriptGoNavigationCandidates{ctx: context.Background()}
	builder.addDocument(parsed, parsed.URI)
	if len(builder.edges) != 1 {
		t.Fatalf("edges = %#v", builder.edges)
	}
	evidence := builder.edges[0]["evidence"].([]map[string]any)[0]
	if !strings.Contains(evidence["snippet"].(string), "\"a  b\",\n  runtimeValue") {
		t.Fatalf("JavaScript evidence rewrote source whitespace: %#v", evidence)
	}
}

func TestNavigationVBStringBuildersHonorUserFunctionsAndArgumentEffects(t *testing.T) {
	source := `<%
Function Left(value, count)
  Left = "custom.asp"
End Function
Function SideEffect()
  Response.Redirect "audit.asp"
  SideEffect = "old"
End Function
Response.Redirect Left("ignored.asp", 1)
Response.Redirect Replace("old.asp", SideEffect(), "new")
%>`
	candidates := extractVBScriptNavigationCandidates(source[2:len(source)-2], 2, source)
	counts := map[string]int{}
	for _, candidate := range candidates {
		counts[candidate.Value.Text]++
	}
	if len(counts) != 3 || counts["custom.asp"] != 1 || counts["audit.asp"] != 1 || counts["new.asp"] != 1 {
		t.Fatalf("function overrides and argument effects = %#v", counts)
	}
}

func TestNavigationVBUnknownEvidenceRetainsExpressionAndWhitespace(t *testing.T) {
	root := t.TempDir()
	source := "<%\nmessage = \"😀\": Response.Redirect Resolve(\"a  b\", _\n  runtimeValue)\n%>"
	builder := navigationHTMLTestBuilder(t, root, filepath.Join(root, "default.asp"), source)
	if len(builder.edges) != 1 {
		t.Fatalf("edges = %#v", builder.edges)
	}
	evidence := builder.edges[0]["evidence"].([]map[string]any)[0]
	valueRange, ok := evidence["valueRange"].(lsp.Range)
	document := core.NewTextDocument("", "classic-asp", 0, source)
	if !ok || strings.TrimSpace(source[document.OffsetAt(valueRange.Start):document.OffsetAt(valueRange.End)]) != "Resolve(\"a  b\", _\n  runtimeValue)" {
		t.Fatalf("unresolved expression range = %#v", evidence)
	}
	if !strings.Contains(evidence["snippet"].(string), "\"a  b\", _\n  runtimeValue") {
		t.Fatalf("evidence rewrote source whitespace: %#v", evidence)
	}
}

func TestNavigationVBStringBuildersRetainUncertainty(t *testing.T) {
	for _, expression := range []string{
		`Left(Request("page"), 8)`, `Mid("😀next.asp", 2)`, `Left("next.asp", -1)`,
		`Chr(256)`, `ChrW(55296)`, `Replace("next.asp", "NEXT", "old", 1, -1, 1)`,
		`Mid("next.asp", 0)`, `Replace("next.asp", "n")`,
	} {
		source := "<% Response.Redirect " + expression + " %>"
		candidates := extractVBScriptNavigationCandidates(source[2:len(source)-2], 2, source)
		if len(candidates) != 1 || candidates[0].Value.Kind != navigationVBValueUnknown {
			t.Errorf("unsupported or runtime expression %s must retain an unknown occurrence: %#v", expression, candidates)
		}
	}
}

func TestNavigationVBStringBuildersPreserveFiniteBranches(t *testing.T) {
	source := `<%
If enabled Then target = "new.html" Else target = "old.html"
Response.Redirect Replace(target, ".html", ".asp")
%>`
	candidates := extractVBScriptNavigationCandidates(source[2:len(source)-2], 2, source)
	want := map[string]bool{"new.asp": false, "old.asp": false}
	if len(candidates) != 1 {
		t.Fatalf("candidates = %#v", candidates)
	}
	for _, value := range candidates[0].Value.finiteCandidates() {
		want[value.Text] = true
	}
	if len(want) != 2 || !want["new.asp"] || !want["old.asp"] {
		t.Fatalf("finite targets = %#v", candidates)
	}
}

func TestNavigationHTMLSelfSubmitAndFormOverrides(t *testing.T) {
	root := t.TempDir()
	page := filepath.Join(root, "default.asp")
	writeNavigationHTMLTarget(t, root, "override.asp")
	source := `<p>😀</p>
<form id="edit" method="post" target="preview"><input type="hidden" name="id" value="42"></form>
<button form="edit" formaction="override.asp">Save</button>
<button form="edit" formaction="" name="save" value="1">Save here</button>
<form action="" method="get"></form>`
	builder := navigationHTMLTestBuilder(t, root, page, source)
	if len(builder.edges) != 3 {
		t.Fatalf("form edges = %#v, want self POST, override POST and self GET", builder.edges)
	}
	for _, edge := range builder.edges {
		if edge["method"] != "POST" {
			continue
		}
		if edge["targetFrame"] != "preview" {
			t.Fatalf("inherited target missing: %#v", edge)
		}
		if edge["target"] == builder.rootNodeID {
			continue
		}
		params, _ := edge["parameters"].([]map[string]any)
		if len(params) != 1 || params[0]["name"] != "id" {
			t.Fatalf("inherited hidden parameters = %#v", params)
		}
	}
	for _, edge := range builder.edges {
		if edge["target"] == builder.rootNodeID && edge["method"] == "POST" {
			found := false
			for _, parameter := range edge["parameters"].([]map[string]any) {
				if parameter["name"] == "save" && parameter["source"] == "formControl" && parameter["value"] == "1" {
					found = true
				}
			}
			if !found {
				t.Fatalf("empty-action submit control parameter missing: %#v", edge)
			}
			ranges := edge["ranges"].([]lsp.Range)
			if ranges[0].Start.Line != 1 {
				t.Fatalf("missing action should reveal form tag, got %#v", ranges)
			}
		}
	}
}

func TestNavigationHTMLNonNavigatingControlsAndMethodDefaults(t *testing.T) {
	root := t.TempDir()
	source := `<form method="dialog"><button formaction="ignored.asp"></button></form>
<button type="button" formaction="ignored.asp"></button>
<input type="text" formaction="ignored.asp">
<button disabled formaction="ignored.asp"></button>
<form method="invalid" action="valid.asp">
<button formaction="override.asp" formmethod="post"></button></form>
<form action="unfinished.asp">`
	builder := navigationHTMLTestBuilder(t, root, filepath.Join(root, "default.asp"), source)
	if len(builder.edges) != 3 {
		t.Fatalf("form method/control extraction = %#v", builder.edges)
	}
	for _, edge := range builder.edges {
		if edge["method"] != "GET" && edge["method"] != "POST" {
			t.Fatalf("invalid form method retained: %#v", edge)
		}
	}
}

func TestNavigationUnknownTargetsKeepDistinctSourceLocations(t *testing.T) {
	root := t.TempDir()
	source := "<%\nResponse.Redirect firstTarget\nResponse.Redirect secondTarget\n%>"
	builder := navigationHTMLTestBuilder(t, root, filepath.Join(root, "default.asp"), source)
	if len(builder.edges) != 2 {
		t.Fatalf("unknown occurrences collapsed: %#v", builder.edges)
	}
	if builder.edges[0]["target"] == builder.edges[1]["target"] {
		t.Fatal("unrelated unresolved expressions share a target node")
	}
	for index, edge := range builder.edges {
		evidence := edge["evidence"].([]map[string]any)[0]
		if evidence["range"].(lsp.Range).Start.Line != index+1 || !strings.Contains(evidence["snippet"].(string), []string{"firstTarget", "secondTarget"}[index]) {
			t.Fatalf("source evidence = %#v", evidence)
		}
	}
}

func TestStdioNavigationPrecisionAndUnknownSourceRanges(t *testing.T) {
	client := startStdioTestClient(t)
	defer client.close()
	root := t.TempDir()
	page := filepath.Join(root, "default.asp")
	uri := pathToFileURI(page)
	source := `<form id="edit" method="post"></form>
<button form="edit" formaction="save.asp">Save</button>
<%
Response.Redirect Replace("next.html", ".html", ".asp")
Response.Redirect firstRuntimeTarget
Response.Redirect secondRuntimeTarget
%>`
	writeFlowchartFixture(t, page, source)
	writeFlowchartFixture(t, filepath.Join(root, "save.asp"), "")
	writeFlowchartFixture(t, filepath.Join(root, "next.asp"), "")
	client.request("initialize", map[string]any{"processId": nil, "rootUri": pathToFileURI(root), "capabilities": map[string]any{}})
	payload := buildNavigationGraph(t, client, map[string]any{"scope": "document", "uri": uri})
	for _, want := range []struct{ label, kind, method string }{{"default.asp", "htmlForm", "POST"}, {"save.asp", "htmlForm", "POST"}, {"next.asp", "serverRedirect", ""}} {
		if !navigationEdgeMatching(payload, func(edge map[string]any) bool {
			return navigationNodeByID(payload, asString(edge["target"]))["label"] == want.label && edge["kind"] == want.kind && asString(edge["method"]) == want.method
		}) {
			t.Fatalf("missing transition %#v: %s", want, mustJSONText(t, payload))
		}
	}
	unknownTargets := map[string]bool{}
	document := core.NewTextDocument(uri, "classic-asp", 0, source)
	navigationEdgeMatching(payload, func(edge map[string]any) bool {
		id := asString(edge["target"])
		if navigationNodeByID(payload, id)["kind"] != "unknown" {
			return false
		}
		unknownTargets[id] = true
		var evidence []struct {
			URI        string     `json:"uri"`
			ValueRange *lsp.Range `json:"valueRange"`
		}
		if err := remarshal(edge["evidence"], &evidence); err != nil || len(evidence) != 1 || evidence[0].URI != uri || evidence[0].ValueRange == nil {
			t.Fatalf("missing JSON-RPC source range: %#v", edge)
		}
		rangeValue := *evidence[0].ValueRange
		expression := strings.TrimSpace(source[document.OffsetAt(rangeValue.Start):document.OffsetAt(rangeValue.End)])
		if expression != "firstRuntimeTarget" && expression != "secondRuntimeTarget" {
			t.Fatalf("source range selects %q", expression)
		}
		return false
	})
	if len(unknownTargets) != 2 {
		t.Fatalf("unknown destinations = %#v", unknownTargets)
	}
}
