package lspserver

import (
	"context"
	"os"
	"path/filepath"
	"runtime"
	"strings"
	"testing"

	"github.com/yottonoko/classic-web-system-lsp/internal/core"
	"github.com/yottonoko/classic-web-system-lsp/internal/lsp"
)

func TestNavigationJavaScriptUsesVBValuesInRawAndQuotedAssignments(t *testing.T) {
	root := t.TempDir()
	page := filepath.Join(root, "default.asp")
	wantTargets := []string{"number-10.asp", "quoted-10.asp", "bool-true.asp", "bool-True.asp", "next.asp", "fraction-0.5.asp", "exponent-100.asp", "plus-10.asp", "radix-16.asp"}
	if runtime.GOOS == "windows" {
		wantTargets = []string{"number-10.asp", "quoted-10.asp", "bool-true.asp", "next.asp", "fraction-0.5.asp", "exponent-100.asp", "plus-10.asp", "radix-16.asp"}
	}
	for _, name := range wantTargets {
		if err := os.WriteFile(filepath.Join(root, name), nil, 0o644); err != nil {
			t.Fatal(err)
		}
	}
	source := `<%
Dim b, enabled, target, fraction, exponent, positive, radix
b = 10
enabled = True
target = "next.asp"
fraction = .5
exponent = 1e2
positive = +10
radix = &H10
%>
<script>
const rawNumber = <%= b %>;
const quotedNumber = "<%= b %>";
const rawBoolean = <%= enabled %>;
const quotedBoolean = "<%= enabled %>";
const rawTarget = <%= target %>;
const quotedFraction = "<%= fraction %>";
const quotedExponent = "<%= exponent %>";
const quotedPositive = "<%= positive %>";
const quotedRadix = "<%= radix %>";
location.href = "number-" + rawNumber + ".asp";
window.open("quoted-" + quotedNumber + ".asp");
window.location.assign("bool-" + rawBoolean + ".asp");
window.open("bool-" + quotedBoolean + ".asp");
window.location.replace(rawTarget);
window.open("fraction-" + quotedFraction + ".asp");
window.open("exponent-" + quotedExponent + ".asp");
window.open("plus-" + quotedPositive + ".asp");
window.open("radix-" + quotedRadix + ".asp");
</script>`
	parsed := core.ParseDocument(filePathURI(page), source, core.Settings{})
	builder := newNavigationGraphBuilder("document", parsed.URI, []workspaceRoot{{Path: root, URI: filePathURI(root)}})
	builder.javascriptCandidates = typeScriptGoNavigationCandidates{ctx: context.Background()}
	builder.addDocument(parsed, parsed.URI)
	if builder.navigationError != nil {
		t.Fatal(builder.navigationError)
	}
	payload := builder.payload(1)
	for _, name := range wantTargets {
		if !navigationPayloadNodeMatching(payload, func(node map[string]any) bool { return node["label"] == name }) {
			t.Fatalf("contextual JavaScript target %q missing: nodes=%#v edges=%#v", name, builder.nodes, builder.edges)
		}
	}
	if len(builder.edges) != len(wantTargets) {
		t.Fatalf("contextual JavaScript edges = %d, want %d: %#v", len(builder.edges), len(wantTargets), builder.edges)
	}
}

func TestNavigationJavaScriptDoesNotTaintIndependentSinkWithUnrelatedInterpolation(t *testing.T) {
	root := t.TempDir()
	page := filepath.Join(root, "default.asp")
	for _, name := range []string{"known.asp", "next.asp"} {
		if err := os.WriteFile(filepath.Join(root, name), nil, 0o644); err != nil {
			t.Fatal(err)
		}
	}
	source := `<% target = "next.asp" %>
<script>
const unused = <%= unresolved %>;
const assigned = <%= target %>;
location.href = "known.asp";
window.open(assigned);
</script>`
	parsed := core.ParseDocument(filePathURI(page), source, core.Settings{})
	builder := newNavigationGraphBuilder("document", parsed.URI, []workspaceRoot{{Path: root, URI: filePathURI(root)}})
	builder.javascriptCandidates = typeScriptGoNavigationCandidates{ctx: context.Background()}
	builder.addDocument(parsed, parsed.URI)
	if builder.navigationError != nil {
		t.Fatal(builder.navigationError)
	}
	payload := builder.payload(1)
	for _, name := range []string{"known.asp", "next.asp"} {
		if !navigationPayloadNodeMatching(payload, func(node map[string]any) bool { return node["label"] == name }) {
			t.Fatalf("independent contextual JavaScript target %q missing: nodes=%#v edges=%#v", name, builder.nodes, builder.edges)
		}
	}
	if navigationPayloadNodeMatching(payload, func(node map[string]any) bool { return node["kind"] == "unknown" }) {
		t.Fatalf("unrelated interpolation produced an unknown node: nodes=%#v edges=%#v", builder.nodes, builder.edges)
	}
	if len(builder.edges) != 2 {
		t.Fatalf("independent contextual JavaScript edges = %d, want 2: %#v", len(builder.edges), builder.edges)
	}
}

func TestNavigationJavaScriptAttributesASPControlFlowDependency(t *testing.T) {
	root := t.TempDir()
	page := filepath.Join(root, "default.asp")
	for _, name := range []string{"new.asp", "old.asp"} {
		if err := os.WriteFile(filepath.Join(root, name), nil, 0o644); err != nil {
			t.Fatal(err)
		}
	}
	source := `<% flag = IIf(enabled, True, False) %>
<script>
const target = <%= flag %> ? "new.asp" : "old.asp";
location.href = target;
</script>`
	parsed := core.ParseDocument(filePathURI(page), source, core.Settings{})
	var expression core.Region
	for _, region := range parsed.Regions {
		if region.Kind == core.RegionASPExpression {
			expression = region
			break
		}
	}
	document := core.NewTextDocument(parsed.URI, "classic-asp", 0, source)
	wantExpressionRange := document.Range(expression.Start, expression.End)
	builder := newNavigationGraphBuilder("document", parsed.URI, []workspaceRoot{{Path: root, URI: filePathURI(root)}})
	builder.javascriptCandidates = typeScriptGoNavigationCandidates{ctx: context.Background()}
	builder.addDocument(parsed, parsed.URI)
	if builder.navigationError != nil {
		t.Fatal(builder.navigationError)
	}
	if len(builder.edges) != 2 {
		t.Fatalf("control-dependent JavaScript edges = %d, want 2: %#v", len(builder.edges), builder.edges)
	}
	for _, edge := range builder.edges {
		if edge["confidence"] != "possible" {
			t.Fatalf("control-dependent edge confidence = %#v, want possible: %#v", edge["confidence"], edge)
		}
		ranges, _ := edge["ranges"].([]lsp.Range)
		found := false
		for _, rangeValue := range ranges {
			found = found || rangeValue == wantExpressionRange
		}
		if !found {
			t.Fatalf("control-dependent edge omitted ASP range %#v: %#v", wantExpressionRange, edge)
		}
	}
}

func TestNavigationJavaScriptIgnoresDiscardedCommaOperandInterpolation(t *testing.T) {
	root := t.TempDir()
	page := filepath.Join(root, "default.asp")
	if err := os.WriteFile(filepath.Join(root, "known.asp"), nil, 0o644); err != nil {
		t.Fatal(err)
	}
	source := `<script>location.href = (<%= unresolved %>, "known.asp");</script>`
	parsed := core.ParseDocument(filePathURI(page), source, core.Settings{})
	builder := newNavigationGraphBuilder("document", parsed.URI, []workspaceRoot{{Path: root, URI: filePathURI(root)}})
	builder.javascriptCandidates = typeScriptGoNavigationCandidates{ctx: context.Background()}
	builder.addDocument(parsed, parsed.URI)
	if builder.navigationError != nil {
		t.Fatal(builder.navigationError)
	}
	payload := builder.payload(1)
	if !navigationPayloadNodeMatching(payload, func(node map[string]any) bool { return node["label"] == "known.asp" }) {
		t.Fatalf("comma expression known target missing: nodes=%#v edges=%#v", builder.nodes, builder.edges)
	}
	if navigationPayloadNodeMatching(payload, func(node map[string]any) bool { return node["kind"] == "unknown" }) {
		t.Fatalf("discarded comma interpolation produced unknown target: nodes=%#v edges=%#v", builder.nodes, builder.edges)
	}
	if len(builder.edges) != 1 || builder.edges[0]["confidence"] != "probable" {
		t.Fatalf("comma expression edge = %#v, want one probable known edge", builder.edges)
	}
}

func TestNavigationJavaScriptTemplateInterpolationRemainsUnknownWithASPEvidence(t *testing.T) {
	root := t.TempDir()
	page := filepath.Join(root, "default.asp")
	source := `<% target = "next.asp" %><script>location.href = ` + "`prefix-<%= target %>`" + `;</script>`
	parsed := core.ParseDocument(filePathURI(page), source, core.Settings{})
	var expression core.Region
	for _, region := range parsed.Regions {
		if region.Kind == core.RegionASPExpression {
			expression = region
			break
		}
	}
	wantRange := core.NewTextDocument(parsed.URI, "classic-asp", 0, source).Range(expression.Start, expression.End)
	builder := newNavigationGraphBuilder("document", parsed.URI, []workspaceRoot{{Path: root, URI: filePathURI(root)}})
	builder.javascriptCandidates = typeScriptGoNavigationCandidates{ctx: context.Background()}
	builder.addDocument(parsed, parsed.URI)
	if builder.navigationError != nil {
		t.Fatal(builder.navigationError)
	}
	if len(builder.edges) != 1 || !navigationPayloadNodeMatching(builder.payload(1), func(node map[string]any) bool { return node["kind"] == "unknown" }) {
		t.Fatalf("template interpolation graph = nodes=%#v edges=%#v, want one unknown target", builder.nodes, builder.edges)
	}
	ranges, _ := builder.edges[0]["ranges"].([]lsp.Range)
	found := false
	for _, rangeValue := range ranges {
		found = found || rangeValue == wantRange
	}
	if !found {
		t.Fatalf("template interpolation edge omitted ASP range %#v: %#v", wantRange, builder.edges[0])
	}
}

func TestNavigationJavaScriptExpandsVBUnionAndMultipleQuotedHoles(t *testing.T) {
	root := t.TempDir()
	page := filepath.Join(root, "default.asp")
	for _, name := range []string{"new.asp", "old.asp"} {
		if err := os.WriteFile(filepath.Join(root, name), nil, 0o644); err != nil {
			t.Fatal(err)
		}
	}
	if err := os.MkdirAll(filepath.Join(root, "admin"), 0o755); err != nil {
		t.Fatal(err)
	}
	if err := os.WriteFile(filepath.Join(root, "admin", "index.asp"), nil, 0o644); err != nil {
		t.Fatal(err)
	}
	source := `<%
target = IIf(enabled, "new.asp", "old.asp")
folder = "admin"
fileName = "index"
%>
<script>
location.href = <%= target %>;
window.open("<%= folder %>/<%= fileName %>.asp");
</script>`
	parsed := core.ParseDocument(filePathURI(page), source, core.Settings{})
	builder := newNavigationGraphBuilder("document", parsed.URI, []workspaceRoot{{Path: root, URI: filePathURI(root)}})
	builder.javascriptCandidates = typeScriptGoNavigationCandidates{ctx: context.Background()}
	builder.addDocument(parsed, parsed.URI)
	if builder.navigationError != nil {
		t.Fatal(builder.navigationError)
	}
	payload := builder.payload(1)
	for _, name := range []string{"new.asp", "old.asp", "index.asp"} {
		if !navigationPayloadNodeMatching(payload, func(node map[string]any) bool { return node["label"] == name }) {
			t.Fatalf("contextual JavaScript target %q missing: nodes=%#v edges=%#v", name, builder.nodes, builder.edges)
		}
	}
	if len(builder.edges) != 3 {
		t.Fatalf("contextual JavaScript union edges = %d, want 3: %#v", len(builder.edges), builder.edges)
	}
}

func TestNavigationJavaScriptUnknownInterpolationNeverBecomesZero(t *testing.T) {
	root := t.TempDir()
	page := filepath.Join(root, "default.asp")
	source := `<script>
location.href = <%= unresolvedTarget %>;
window.open("prefix-<%= unresolvedTarget %>.asp");
</script>`
	parsed := core.ParseDocument(filePathURI(page), source, core.Settings{})
	builder := newNavigationGraphBuilder("document", parsed.URI, []workspaceRoot{{Path: root, URI: filePathURI(root)}})
	builder.javascriptCandidates = typeScriptGoNavigationCandidates{ctx: context.Background()}
	builder.addDocument(parsed, parsed.URI)
	if builder.navigationError != nil {
		t.Fatal(builder.navigationError)
	}
	for _, node := range builder.nodes {
		if node["label"] == "0" {
			t.Fatalf("unknown interpolation became the legacy zero placeholder: %#v", builder.nodes)
		}
	}
	if len(builder.edges) != 2 {
		t.Fatalf("unknown interpolation edges = %#v, want two conservative edges", builder.edges)
	}
	for _, edge := range builder.edges {
		targetID, _ := edge["target"].(string)
		if len(targetID) < len("unknown:") || targetID[:len("unknown:")] != "unknown:" {
			t.Fatalf("unknown interpolation target = %q, want unknown node: %#v", targetID, edge)
		}
	}
}

func TestNavigationJavaScriptRegexLiteralDoesNotChangeRawInterpolationContext(t *testing.T) {
	root := t.TempDir()
	page := filepath.Join(root, "default.asp")
	if err := os.WriteFile(filepath.Join(root, "next.asp"), nil, 0o644); err != nil {
		t.Fatal(err)
	}
	source := `<% target = "next.asp" %>
<script>
const quotePattern = /"/;
function apostrophePattern() { return /'/; }
if (enabled) /"/.test(value);
location.href = <%= target %>;
</script>`
	parsed := core.ParseDocument(filePathURI(page), source, core.Settings{})
	builder := newNavigationGraphBuilder("document", parsed.URI, []workspaceRoot{{Path: root, URI: filePathURI(root)}})
	builder.javascriptCandidates = typeScriptGoNavigationCandidates{ctx: context.Background()}
	builder.addDocument(parsed, parsed.URI)
	if builder.navigationError != nil {
		t.Fatal(builder.navigationError)
	}
	if len(builder.edges) != 1 || navigationHTMLTestEdgeTarget(builder, builder.edges[0]) != "next.asp" {
		t.Fatalf("regex-prefixed raw interpolation edges = %#v, want next.asp", builder.edges)
	}
}

func TestNavigationJavaScriptPostfixDivisionKeepsLaterInterpolationRaw(t *testing.T) {
	root := t.TempDir()
	page := filepath.Join(root, "default.asp")
	if err := os.WriteFile(filepath.Join(root, "next.asp"), nil, 0o644); err != nil {
		t.Fatal(err)
	}
	source := `<% target = "next.asp" %><script>let i = 4; i++ / 2; location.href = <%= target %>;</script>`
	parsed := core.ParseDocument(filePathURI(page), source, core.Settings{})
	builder := newNavigationGraphBuilder("document", parsed.URI, []workspaceRoot{{Path: root, URI: filePathURI(root)}})
	builder.javascriptCandidates = typeScriptGoNavigationCandidates{ctx: context.Background()}
	builder.addDocument(parsed, parsed.URI)
	if builder.navigationError != nil {
		t.Fatal(builder.navigationError)
	}
	if len(builder.edges) != 1 || navigationHTMLTestEdgeTarget(builder, builder.edges[0]) != "next.asp" {
		t.Fatalf("postfix-division interpolation edges = %#v, want next.asp", builder.edges)
	}
}

func TestNavigationJavaScriptRegexInterpolationUsesSyntaxSafeContent(t *testing.T) {
	for _, testCase := range []struct {
		name      string
		pattern   string
		value     string
		primitive navigationPrimitiveKind
		want      string
	}{
		{name: "delimiter", pattern: `/<%= value %>/`, value: "a/b", want: `/a\x2fb/`},
		{name: "control statement lexical goal", pattern: `(() => { if (enabled) /<%= value %>/.test(input); return /x/; })()`, value: "a/b", want: `/a\x2fb/`},
		{name: "unicode hyphen", pattern: `/<%= value %>/u`, value: "-", want: `/\x2d/u`},
		{name: "unicode set hyphen", pattern: `/[<%= value %>]/u`, value: "-", want: `/[\x2d]/u`},
		{name: "unicode sets hyphen", pattern: `/<%= value %>/v`, value: "-", want: `/\x2d/v`},
		{name: "unicode sets class hyphen", pattern: `/[<%= value %>]/v`, value: "-", want: `/[\x2d]/v`},
		{name: "unicode sets double punctuation", pattern: `/[<%= value %>]/v`, value: "&&", want: `/[\x26\x26]/v`},
		{name: "numeric runtime content", pattern: `/<%= value %>/`, value: ".5", primitive: navigationPrimitiveNumber, want: `/0\x2e5/`},
	} {
		t.Run(testCase.name, func(t *testing.T) {
			source := `<% value = "placeholder" %><script>const pattern = ` + testCase.pattern + `;</script>`
			parsed := core.ParseDocument("file:///regex-interpolation.asp", source, core.Settings{})
			var owner, expression core.Region
			for _, region := range parsed.Regions {
				switch region.Kind {
				case core.RegionClientScript:
					owner = region
				case core.RegionASPExpression:
					expression = region
				}
			}
			primitive := testCase.primitive
			if primitive == navigationPrimitiveUnknown {
				primitive = navigationPrimitiveString
			}
			variants, err := navigationJavaScriptInterpolationVariantsContext(context.Background(), parsed, owner, map[int][]navigationValue{
				expression.Start: {{Kind: navigationValueLiteral, Primitive: primitive, Text: testCase.value}},
			})
			if err != nil {
				t.Fatal(err)
			}
			if len(variants) != 1 {
				t.Fatalf("regex interpolation variants = %#v, want one", variants)
			}
			virtual, err := core.BuildEmbeddedRegionVirtualDocumentWithReplacementsContext(context.Background(), parsed, owner, variants[0].replacements)
			if err != nil {
				t.Fatal(err)
			}
			if !strings.Contains(virtual.Text, testCase.want) {
				t.Fatalf("regex interpolation virtual source = %q, want %q", virtual.Text, testCase.want)
			}
		})
	}
}

func TestNavigationJavaScriptAttributesDirectControlSinkDependencies(t *testing.T) {
	for _, testCase := range []struct {
		name string
		body string
	}{
		{name: "if", body: `if (<%= flag %>) { location.href = "next.asp"; }`},
		{name: "switch", body: `switch (<%= flag %>) { case true: location.href = "next.asp"; break; default: break; }`},
		{name: "logical", body: `(<%= flag %>) && (location.href = "next.asp");`},
		{name: "ternary", body: `(<%= flag %>) ? (location.href = "next.asp") : undefined;`},
		{name: "function-if", body: `function go(value) { if (value) { location.href = "next.asp"; } } go(<%= flag %>);`},
		{name: "function-logical", body: `function go(value) { value && (location.href = "next.asp"); } go(<%= flag %>);`},
		{name: "regex", body: `if (/<%= flag %>/.test(input)) { location.href = "next.asp"; }`},
		{name: "function-regex", body: `function go(value) { if (/<%= flag %>/.test(value)) { location.href = "next.asp"; } } go(input);`},
	} {
		t.Run(testCase.name, func(t *testing.T) {
			root := t.TempDir()
			page := filepath.Join(root, "default.asp")
			if err := os.WriteFile(filepath.Join(root, "next.asp"), nil, 0o644); err != nil {
				t.Fatal(err)
			}
			source := `<% flag = IIf(enabled, True, False) %><script>` + testCase.body + `</script>`
			parsed := core.ParseDocument(filePathURI(page), source, core.Settings{})
			var expression core.Region
			for _, region := range parsed.Regions {
				if region.Kind == core.RegionASPExpression {
					expression = region
					break
				}
			}
			wantRange := core.NewTextDocument(parsed.URI, "classic-asp", 0, source).Range(expression.Start, expression.End)
			builder := newNavigationGraphBuilder("document", parsed.URI, []workspaceRoot{{Path: root, URI: filePathURI(root)}})
			builder.javascriptCandidates = typeScriptGoNavigationCandidates{ctx: context.Background()}
			builder.addDocument(parsed, parsed.URI)
			if builder.navigationError != nil {
				t.Fatal(builder.navigationError)
			}
			if len(builder.edges) != 1 || navigationHTMLTestEdgeTarget(builder, builder.edges[0]) != "next.asp" {
				t.Fatalf("direct control sink edges = %#v, want one next.asp edge", builder.edges)
			}
			if builder.edges[0]["confidence"] != "possible" {
				t.Fatalf("direct control sink confidence = %#v, want possible", builder.edges[0]["confidence"])
			}
			ranges, _ := builder.edges[0]["ranges"].([]lsp.Range)
			found := false
			for _, rangeValue := range ranges {
				found = found || rangeValue == wantRange
			}
			if !found {
				t.Fatalf("direct control sink omitted ASP range %#v: %#v", wantRange, builder.edges[0])
			}
		})
	}
}

func TestNavigationJavaScriptDeduplicatedAlternativesMergeParameterMetadata(t *testing.T) {
	root := t.TempDir()
	page := filepath.Join(root, "default.asp")
	source := `<%
target = IIf(flag, Request("id"), Request.QueryString("id"))
%>
<script>location.href = "item-<%= target %>.asp";</script>`
	parsed := core.ParseDocument(filePathURI(page), source, core.Settings{})
	builder := newNavigationGraphBuilder("document", parsed.URI, []workspaceRoot{{Path: root, URI: filePathURI(root)}})
	builder.javascriptCandidates = typeScriptGoNavigationCandidates{ctx: context.Background()}
	builder.addDocument(parsed, parsed.URI)
	if builder.navigationError != nil {
		t.Fatal(builder.navigationError)
	}
	if len(builder.edges) != 1 {
		t.Fatalf("deduplicated parameter alternatives edges = %#v, want one", builder.edges)
	}
	if builder.edges[0]["count"] != 1 {
		t.Fatalf("deduplicated parameter alternative count = %#v, want one source occurrence", builder.edges[0]["count"])
	}
	parameters, _ := builder.edges[0]["parameters"].([]map[string]any)
	sources := map[string]bool{}
	for _, parameter := range parameters {
		sources[navigationString(parameter["source"])] = true
	}
	if !sources["request"] || !sources["queryString"] {
		t.Fatalf("deduplicated parameter metadata = %#v, want request and queryString", parameters)
	}
}
