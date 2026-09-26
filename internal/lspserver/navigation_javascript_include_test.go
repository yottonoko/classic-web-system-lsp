package lspserver

import (
	"context"
	"errors"
	"os"
	"path/filepath"
	"strconv"
	"strings"
	"testing"

	"github.com/yottonoko/classic-web-system-lsp/internal/core"
	"github.com/yottonoko/classic-web-system-lsp/internal/lsp"
	workspacepkg "github.com/yottonoko/classic-web-system-lsp/internal/workspace"
)

func TestNavigationJavaScriptRepeatedIncludePreservesOccurrenceEvidence(t *testing.T) {
	root := t.TempDir()
	pageURI := filePathURI(filepath.Join(root, "default.asp"))
	includeURI := filePathURI(filepath.Join(root, "shared.inc"))
	targetURI := filepath.Join(root, "target.asp")
	if err := os.WriteFile(targetURI, nil, 0o644); err != nil {
		t.Fatal(err)
	}
	page := core.ParseDocument(pageURI, `<!-- #include file="shared.inc" -->
<!-- #include file="shared.inc" -->`, core.Settings{})
	include := core.ParseDocument(includeURI, `<script>location.href = "target.asp";</script>`, core.Settings{})
	documents := []*core.ParsedDocument{page, include}
	owners := map[string][]string{workspacepkg.FileIdentityKeyFromURI(includeURI): {pageURI}}

	builder := newNavigationGraphBuilder("document", pageURI, []workspaceRoot{{Path: root, URI: filePathURI(root)}})
	builder.javascriptCandidates = typeScriptGoNavigationCandidates{ctx: context.Background()}
	builder.prepareVBScriptFunctions(documents, owners)
	builder.addDocument(page, pageURI)
	if builder.navigationError != nil {
		t.Fatalf("repeated JavaScript include navigation error = %v", builder.navigationError)
	}
	if len(builder.edges) != 1 {
		t.Fatalf("repeated JavaScript include edges = %#v, want one merged edge", builder.edges)
	}
	edge := builder.edges[0]
	if edge["kind"] != "javascriptLocation" || navigationHTMLTestEdgeTarget(builder, edge) != "target.asp" {
		t.Fatalf("repeated JavaScript include edge = %#v, want target.asp location edge", edge)
	}
	if edge["count"] != 2 {
		t.Fatalf("repeated JavaScript include count = %#v, want two occurrences", edge["count"])
	}
	evidence, _ := edge["evidence"].([]map[string]any)
	if len(evidence) != 2 {
		t.Fatalf("repeated JavaScript include evidence = %#v, want two occurrences", edge["evidence"])
	}
	for index, item := range evidence {
		if item["uri"] != includeURI || item["extractor"] != "javascript" {
			t.Fatalf("JavaScript include evidence[%d] = %#v, want shared.inc JavaScript evidence", index, item)
		}
		if !strings.Contains(navigationString(item["snippet"]), "target.asp") {
			t.Fatalf("JavaScript include evidence[%d] snippet = %#v, want target.asp", index, item["snippet"])
		}
	}
	ranges, _ := edge["ranges"].([]lsp.Range)
	if len(ranges) != 2 || ranges[0] != ranges[1] || ranges[0] == (lsp.Range{}) {
		t.Fatalf("repeated JavaScript include ranges = %#v, want two equal source ranges", ranges)
	}
}

func TestNavigationJavaScriptManyExecutionUnitsReuseIndexesAndSkipNonScriptRanges(t *testing.T) {
	root := t.TempDir()
	pageURI := filePathURI(filepath.Join(root, "many-units.asp"))
	prefix := strings.Repeat("plain text\n", 4095)
	source := prefix + `<script>location.href = "target.asp";</script>`
	parsed := core.ParseDocument(pageURI, source, core.Settings{})
	units := make([]navigationVBExecutionUnit, 0, 4096)
	for index := 0; index < 4095; index++ {
		start := index * len("plain text\n")
		units = append(units, navigationVBExecutionUnit{parsed: parsed, start: start, end: start + len("plain text\n"), ownerURI: pageURI, occurrenceID: strconv.Itoa(index)})
	}
	units = append(units, navigationVBExecutionUnit{parsed: parsed, start: len(prefix), end: len(source), ownerURI: pageURI, occurrenceID: "script"})

	builder := newNavigationGraphBuilder("document", pageURI, []workspaceRoot{{Path: root, URI: filePathURI(root)}})
	builder.javascriptCandidates = typeScriptGoNavigationCandidates{ctx: context.Background()}
	key := workspacepkg.FileIdentityKeyFromURI(pageURI)
	builder.addJavaScriptNavigationForPrograms(parsed, []navigationVBHTMLProgram{{key: key, program: units}})
	if builder.navigationError != nil {
		t.Fatal(builder.navigationError)
	}
	if len(builder.javascriptRegions) != 1 || len(builder.javascriptDocuments) != 1 {
		t.Fatalf("JavaScript caches = regions:%d documents:%d, want one of each", len(builder.javascriptRegions), len(builder.javascriptDocuments))
	}
	if len(builder.edges) != 1 {
		t.Fatalf("many-unit JavaScript edges = %#v, want one", builder.edges)
	}

	ctx, cancel := context.WithCancel(context.Background())
	cancel()
	cancelled := newNavigationGraphBuilder("document", pageURI, nil)
	cancelled.cancelContext = ctx
	cancelled.javascriptCandidates = typeScriptGoNavigationCandidates{ctx: ctx}
	cancelled.addJavaScriptNavigationForPrograms(parsed, []navigationVBHTMLProgram{{key: key, program: units}})
	if !errors.Is(cancelled.navigationError, context.Canceled) {
		t.Fatalf("many-unit cancellation error = %v, want context.Canceled", cancelled.navigationError)
	}
	if len(cancelled.javascriptDocuments) != 0 {
		t.Fatalf("cancelled traversal built %d line indexes, want none", len(cancelled.javascriptDocuments))
	}
}

func TestNavigationJavaScriptRepeatedIncludeUsesOccurrenceSpecificInterpolationValues(t *testing.T) {
	root := t.TempDir()
	pageURI := filePathURI(filepath.Join(root, "default.asp"))
	includeURI := filePathURI(filepath.Join(root, "shared.inc"))
	include := core.ParseDocument(includeURI, `<script>location.href = "/target/<%= b %>.asp";</script>`, core.Settings{})
	var script, expression core.Region
	for _, region := range include.Regions {
		if region.Language == core.LanguageJavaScript {
			script = region
		}
		if region.Kind == core.RegionASPExpression {
			expression = region
		}
	}
	if script.ContentEnd == 0 || expression.End == 0 {
		t.Fatalf("include regions = %#v", include.Regions)
	}
	builder := newNavigationGraphBuilder("document", pageURI, []workspaceRoot{{Path: root, URI: filePathURI(root)}})
	firstOccurrence, secondOccurrence := "include-0", "include-1"
	builder.vbExpressionValues[navigationVBExpressionKey(pageURI, includeURI, expression.Start, firstOccurrence)] = []navigationValue{{Kind: navigationValueLiteral, Primitive: navigationPrimitiveString, Text: "first"}}
	builder.vbExpressionValues[navigationVBExpressionKey(pageURI, includeURI, expression.Start, secondOccurrence)] = []navigationValue{{Kind: navigationValueLiteral, Primitive: navigationPrimitiveString, Text: "second"}}
	for _, testCase := range []struct {
		occurrence string
		want       string
	}{
		{occurrence: firstOccurrence, want: "first"},
		{occurrence: secondOccurrence, want: "second"},
	} {
		values := builder.vbExpressionValuesForRegion(include, pageURI, expression, testCase.occurrence)
		variants, err := navigationJavaScriptInterpolationVariantsContext(context.Background(), include, script, map[int][]navigationValue{
			expression.Start: values,
		})
		if err != nil {
			t.Fatal(err)
		}
		if len(variants) != 1 || len(variants[0].replacements) != 1 || variants[0].replacements[0].Text != testCase.want {
			t.Fatalf("occurrence %q variants = %#v, want replacement %q", testCase.occurrence, variants, testCase.want)
		}
	}
}

func TestNavigationJavaScriptRepeatedIncludePropagatesSourceOrderedVBValues(t *testing.T) {
	root := t.TempDir()
	pageURI := filePathURI(filepath.Join(root, "default.asp"))
	includeURI := filePathURI(filepath.Join(root, "shared.inc"))
	for _, target := range []string{"first.asp", "second.asp"} {
		if err := os.WriteFile(filepath.Join(root, target), nil, 0o644); err != nil {
			t.Fatal(err)
		}
	}
	page := core.ParseDocument(pageURI, `<%
target = "first.asp"
%>
<!-- #include file="shared.inc" -->
<%
target = "second.asp"
%>
<!-- #include file="shared.inc" -->`, core.Settings{})
	include := core.ParseDocument(includeURI, `<script>location.href = <%= target %>;</script>`, core.Settings{})
	owners := map[string][]string{workspacepkg.FileIdentityKeyFromURI(includeURI): {pageURI}}
	builder := newNavigationGraphBuilder("document", pageURI, []workspaceRoot{{Path: root, URI: filePathURI(root)}})
	builder.javascriptCandidates = typeScriptGoNavigationCandidates{ctx: context.Background()}
	builder.prepareVBScriptFunctions([]*core.ParsedDocument{page, include}, owners)
	builder.addDocument(page, pageURI)
	if builder.navigationError != nil {
		t.Fatal(builder.navigationError)
	}
	if len(builder.edges) != 2 {
		t.Fatalf("source-ordered JavaScript include edges = %#v, want two", builder.edges)
	}
	for _, target := range []string{"first.asp", "second.asp"} {
		found := false
		for _, edge := range builder.edges {
			if navigationHTMLTestEdgeTarget(builder, edge) == target && edge["count"] == 1 {
				found = true
				break
			}
		}
		if !found {
			t.Fatalf("source-ordered JavaScript include target %q missing: %#v", target, builder.edges)
		}
	}
}
