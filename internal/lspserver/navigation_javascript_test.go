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
)

type cancelingNavigationJavaScriptProvider struct {
	calls    int
	cancelAt int
	cancel   context.CancelFunc
}

type countingNavigationJavaScriptProvider struct {
	delegate        typeScriptGoNavigationCandidates
	contextualCalls int
}

func (p *countingNavigationJavaScriptProvider) Candidates(parsed *core.ParsedDocument, region core.Region) ([]navigationFiniteCandidate, error) {
	return p.delegate.Candidates(parsed, region)
}

func (p *countingNavigationJavaScriptProvider) CandidatesWithReplacementsSource(parsed *core.ParsedDocument, source *core.TextDocument, region core.Region, replacements []core.VirtualDocumentReplacement, expressionRegions []core.Region) ([]navigationFiniteCandidate, error) {
	p.contextualCalls++
	return p.delegate.CandidatesWithReplacementsSource(parsed, source, region, replacements, expressionRegions)
}

func (p *cancelingNavigationJavaScriptProvider) Candidates(parsed *core.ParsedDocument, region core.Region) ([]navigationFiniteCandidate, error) {
	p.calls++
	if p.calls == p.cancelAt {
		p.cancel()
		return nil, nil
	}
	return []navigationFiniteCandidate{{
		Kind:  "javascriptLocation",
		Value: navigationValue{Kind: navigationValueLiteral, Text: "first.asp"},
	}}, nil
}

func TestNavigationJavaScriptUsesTypeScriptGoFiniteCandidates(t *testing.T) {
	root := t.TempDir()
	page := filepath.Join(root, "default.asp")
	for _, name := range []string{"new.asp", "old.asp"} {
		if err := os.WriteFile(filepath.Join(root, name), nil, 0o644); err != nil {
			t.Fatal(err)
		}
	}
	source := `<script>
const target = enabled ? "new.asp" : "old.asp";
location.href = target;
</script>`
	parsed := core.ParseDocument(filePathURI(page), source, core.Settings{})
	builder := newNavigationGraphBuilder("document", parsed.URI, []workspaceRoot{{Path: root, URI: filePathURI(root)}})
	builder.javascriptCandidates = typeScriptGoNavigationCandidates{ctx: context.Background()}
	builder.addDocument(parsed, parsed.URI)
	for _, name := range []string{"new.asp", "old.asp"} {
		if !navigationPayloadNodeMatching(builder.payload(1), func(node map[string]any) bool { return node["label"] == name }) {
			t.Fatalf("TypeScript-Go finite target %s missing: %#v", name, builder.payload(1))
		}
	}
	if len(builder.edges) != 2 {
		t.Fatalf("JavaScript finite edges = %d, want 2: %#v", len(builder.edges), builder.edges)
	}
	for _, edge := range builder.edges {
		if edge["confidence"] != "possible" {
			t.Fatalf("conditional JavaScript edge confidence = %#v, want possible", edge["confidence"])
		}
	}
}

func TestNavigationJavaScriptMapsExpressionRangeToASPSourceUTF16(t *testing.T) {
	root := t.TempDir()
	page := filepath.Join(root, "default.asp")
	if err := os.WriteFile(filepath.Join(root, "next.asp"), nil, 0o644); err != nil {
		t.Fatal(err)
	}
	source := "🌸\n<script>location.href = `next.asp`;</script>"
	parsed := core.ParseDocument(filePathURI(page), source, core.Settings{})
	builder := newNavigationGraphBuilder("document", parsed.URI, []workspaceRoot{{Path: root, URI: filePathURI(root)}})
	builder.javascriptCandidates = typeScriptGoNavigationCandidates{ctx: context.Background()}
	builder.addDocument(parsed, parsed.URI)
	if len(builder.edges) != 1 {
		t.Fatalf("JavaScript edges = %#v", builder.edges)
	}
	ranges := builder.edges[0]["ranges"].([]lsp.Range)
	if len(ranges) != 1 || ranges[0].Start.Line != 1 || ranges[0].Start.Character != 24 {
		t.Fatalf("JavaScript source range = %#v, want line 1 character 24", ranges)
	}
}

func TestNavigationJavaScriptASTIgnoresNavigationTextInCommentsAndStrings(t *testing.T) {
	root := t.TempDir()
	page := filepath.Join(root, "default.asp")
	source := `<script>
// location.href = "comment.asp";
const example = "window.open('string.asp')";
</script>`
	parsed := core.ParseDocument(filePathURI(page), source, core.Settings{})
	builder := newNavigationGraphBuilder("document", parsed.URI, []workspaceRoot{{Path: root, URI: filePathURI(root)}})
	builder.javascriptCandidates = typeScriptGoNavigationCandidates{ctx: context.Background()}
	builder.addDocument(parsed, parsed.URI)
	if len(builder.edges) != 0 {
		t.Fatalf("comment/string navigation text produced edges: %#v", builder.edges)
	}
}

func TestNavigationJavaScriptCancellationOnLastExtractionDropsPartialGraph(t *testing.T) {
	root := t.TempDir()
	first := core.ParseDocument(filePathURI(filepath.Join(root, "first.asp")), `<script>location.href = "first.asp";</script>`, core.Settings{})
	last := core.ParseDocument(filePathURI(filepath.Join(root, "last.asp")), `<script>location.href = "last.asp";</script>`, core.Settings{})
	ctx, cancel := context.WithCancel(context.Background())
	defer cancel()
	provider := &cancelingNavigationJavaScriptProvider{cancelAt: 2, cancel: cancel}
	builder := newNavigationGraphBuilder("document", first.URI, []workspaceRoot{{Path: root, URI: filePathURI(root)}})
	builder.cancelContext = ctx
	builder.javascriptCandidates = provider
	builder.addDocument(first, first.URI)
	builder.addDocument(last, first.URI)
	if provider.calls != 2 {
		t.Fatalf("JavaScript extraction calls = %d, want cancellation during last extraction", provider.calls)
	}
	if !errors.Is(builder.navigationError, context.Canceled) {
		t.Fatalf("last-extraction cancellation error = %v, want context.Canceled", builder.navigationError)
	}
	if ctx.Err() == nil {
		t.Fatal("last-extraction cancellation did not reach graph context")
	}
	if len(builder.edges) == 0 {
		t.Fatal("test provider did not leave a detectable partial edge before cancellation")
	}
	// The production graph command checks navigationError before payload, so a
	// cancellation cannot publish the already accumulated partial edges.
	if builder.navigationError == nil {
		t.Fatalf("cancelled builder unexpectedly looked complete: %#v", builder.payload(2))
	}
}

func TestNavigationJavaScriptInterpolationVariantsRenderPrimitiveValues(t *testing.T) {
	tests := []struct {
		name      string
		source    string
		primitive navigationPrimitiveKind
		value     navigationValue
		want      string
	}{
		{
			name:      "raw number",
			source:    `<script>location.href = <%= b %>;</script>`,
			primitive: navigationPrimitiveNumber,
			value:     navigationValue{Kind: navigationValueLiteral, Primitive: navigationPrimitiveNumber, Text: "10"},
			want:      "10",
		},
		{
			name:      "raw negative number",
			source:    `<script>location.href = <%= b %>;</script>`,
			primitive: navigationPrimitiveNumber,
			value:     navigationValue{Kind: navigationValueLiteral, Primitive: navigationPrimitiveNumber, Text: "-10"},
			want:      "-10",
		},
		{
			name:      "raw VB hexadecimal",
			source:    `<script>location.href = <%= b %>;</script>`,
			primitive: navigationPrimitiveNumber,
			value:     navigationValue{Kind: navigationValueLiteral, Primitive: navigationPrimitiveNumber, Text: "&H10"},
			want:      "16",
		},
		{
			name:      "raw VB octal",
			source:    `<script>location.href = <%= b %>;</script>`,
			primitive: navigationPrimitiveNumber,
			value:     navigationValue{Kind: navigationValueLiteral, Primitive: navigationPrimitiveNumber, Text: "&O10"},
			want:      "8",
		},
		{
			name:      "raw legacy VB octal",
			source:    `<script>location.href = <%= b %>;</script>`,
			primitive: navigationPrimitiveNumber,
			value:     navigationValue{Kind: navigationValueLiteral, Primitive: navigationPrimitiveNumber, Text: "&077"},
			want:      "63",
		},
		{
			name:      "raw signed VB hexadecimal",
			source:    `<script>location.href = <%= b %>;</script>`,
			primitive: navigationPrimitiveNumber,
			value:     navigationValue{Kind: navigationValueLiteral, Primitive: navigationPrimitiveNumber, Text: "-&H10"},
			want:      "-16",
		},
		{
			name:      "quoted signed VB octal",
			source:    `<script>location.href = "<%= b %>";</script>`,
			primitive: navigationPrimitiveNumber,
			value:     navigationValue{Kind: navigationValueLiteral, Primitive: navigationPrimitiveNumber, Text: "-&O10"},
			want:      "-8",
		},
		{
			name:      "raw large integer stays exact",
			source:    `<script>location.href = <%= b %>;</script>`,
			primitive: navigationPrimitiveNumber,
			value:     navigationValue{Kind: navigationValueLiteral, Primitive: navigationPrimitiveNumber, Text: "9007199254740993"},
			want:      "9007199254740993",
		},
		{
			name:      "raw boolean",
			source:    `<script>location.href = <%= b %>;</script>`,
			primitive: navigationPrimitiveBoolean,
			value:     navigationValue{Kind: navigationValueLiteral, Primitive: navigationPrimitiveBoolean, Text: "TRUE"},
			want:      "true",
		},
		{
			name:      "raw unknown is undefined",
			source:    `<script>location.href = <%= b %>;</script>`,
			primitive: navigationPrimitiveUnknown,
			value:     navigationValue{Kind: navigationValueUnknown, Text: "{unknown}"},
			want:      "undefined",
		},
		{
			name:      "raw template is undefined",
			source:    `<script>location.href = <%= b %>;</script>`,
			primitive: navigationPrimitiveString,
			value:     navigationValue{Kind: navigationValueTemplate, Primitive: navigationPrimitiveString, Text: "{Request:b}"},
			want:      "undefined",
		},
		{
			name:      "quoted number",
			source:    `<script>location.href = "/item/<%= b %>.asp";</script>`,
			primitive: navigationPrimitiveNumber,
			value:     navigationValue{Kind: navigationValueLiteral, Primitive: navigationPrimitiveNumber, Text: "10"},
			want:      "10",
		},
		{
			name:      "quoted escaped string",
			source:    `<script>location.href = "/item/<%= b %>.asp";</script>`,
			primitive: navigationPrimitiveString,
			value:     navigationValue{Kind: navigationValueLiteral, Primitive: navigationPrimitiveString, Text: `a"b\c`},
			want:      `a\"b\\c`,
		},
		{
			name:      "quoted content after dangling escape",
			source:    `<script>location.href = "\<%= b %>";</script>`,
			primitive: navigationPrimitiveString,
			value:     navigationValue{Kind: navigationValueLiteral, Primitive: navigationPrimitiveString, Text: "next.asp"},
			want:      `\next.asp`,
		},
	}
	for _, testCase := range tests {
		t.Run(testCase.name, func(t *testing.T) {
			parsed := core.ParseDocument("file:///site/interpolation.asp", testCase.source, core.Settings{})
			var script core.Region
			var expression core.Region
			for _, region := range parsed.Regions {
				if region.Language == core.LanguageJavaScript {
					script = region
				}
				if region.Kind == core.RegionASPExpression {
					expression = region
				}
			}
			if script.ContentEnd == 0 || expression.End == 0 {
				t.Fatalf("regions = %#v", parsed.Regions)
			}
			variants, err := navigationJavaScriptInterpolationVariantsContext(context.Background(), parsed, script, map[int][]navigationValue{
				expression.Start: {testCase.value},
			})
			if err != nil {
				t.Fatal(err)
			}
			if len(variants) != 1 {
				t.Fatalf("variants = %#v, want one", variants)
			}
			if got := variants[0].replacements[0].Text; got != testCase.want {
				t.Fatalf("replacement = %q, want %q", got, testCase.want)
			}
			if testCase.primitive == navigationPrimitiveUnknown && variants[0].replacements[0].Text == "0" {
				t.Fatal("unknown interpolation was replaced with zero")
			}
		})
	}
}

func TestNavigationJavaScriptInterpolationVariantsAreBoundedAndDeterministic(t *testing.T) {
	source := `<script>location.href = "/<%= one %>/<%= two %>";</script>`
	parsed := core.ParseDocument("file:///site/interpolation-limit.asp", source, core.Settings{})
	var script core.Region
	var expressions []core.Region
	for _, region := range parsed.Regions {
		if region.Language == core.LanguageJavaScript {
			script = region
		}
		if region.Kind == core.RegionASPExpression {
			expressions = append(expressions, region)
		}
	}
	values := make([]navigationValue, 0, 10)
	for index := 0; index < 10; index++ {
		values = append(values, navigationValue{Kind: navigationValueLiteral, Primitive: navigationPrimitiveNumber, Text: strconv.Itoa(index)})
	}
	byOffset := map[int][]navigationValue{expressions[0].Start: values, expressions[1].Start: values}
	first, err := navigationJavaScriptInterpolationVariantsContext(context.Background(), parsed, script, byOffset)
	if err != nil {
		t.Fatal(err)
	}
	second, err := navigationJavaScriptInterpolationVariantsContext(context.Background(), parsed, script, byOffset)
	if err != nil {
		t.Fatal(err)
	}
	if len(first) != navigationJavaScriptInterpolationVariantLimit {
		t.Fatalf("variants = %d, want %d", len(first), navigationJavaScriptInterpolationVariantLimit)
	}
	if got := first[len(first)-1].values; len(got) != 2 || got[0].Kind != navigationValueUnknown || got[1].Kind != navigationValueUnknown {
		t.Fatalf("last capped variant = %#v, want deterministic unknown fallback", first[len(first)-1])
	}
	for index := range first {
		if first[index].replacements[0].Text != second[index].replacements[0].Text || first[index].replacements[1].Text != second[index].replacements[1].Text {
			t.Fatalf("variant order changed at %d: first=%#v second=%#v", index, first[index], second[index])
		}
	}
}

func TestNavigationJavaScriptInterpolationManyHolesUsesSingleUnknownFallback(t *testing.T) {
	var source strings.Builder
	source.WriteString(`<script>location.href = "`)
	for index := 0; index < navigationJavaScriptInterpolationElementLimit+1; index++ {
		source.WriteString(`<%= value`)
		source.WriteString(strconv.Itoa(index))
		source.WriteString(` %>`)
	}
	source.WriteString(`";</script>`)
	parsed := core.ParseDocument("file:///site/interpolation-many-holes.asp", source.String(), core.Settings{})
	var script core.Region
	valuesByOffset := map[int][]navigationValue{}
	for _, region := range parsed.Regions {
		if region.Language == core.LanguageJavaScript {
			script = region
		}
		if region.Kind == core.RegionASPExpression {
			valuesByOffset[region.Start] = []navigationValue{{Kind: navigationValueLiteral, Primitive: navigationPrimitiveString, Text: "known"}}
		}
	}
	variants, err := navigationJavaScriptInterpolationVariantsContext(context.Background(), parsed, script, valuesByOffset)
	if err != nil {
		t.Fatal(err)
	}
	if len(variants) != 1 || len(variants[0].values) != navigationJavaScriptInterpolationElementLimit+1 {
		t.Fatalf("many-hole variants = %#v, want one complete fallback", variants)
	}
	for _, value := range variants[0].values {
		if value.Kind != navigationValueUnknown {
			t.Fatalf("many-hole fallback retained a concrete value: %#v", variants[0].values)
		}
	}
}

func TestNavigationJavaScriptGraphBudgetLimitsExtraContextualAnalyses(t *testing.T) {
	source := `<script>location.href = <%= target %>;</script>`
	parsed := core.ParseDocument("file:///site/interpolation-graph-budget.asp", source, core.Settings{})
	var script, expression core.Region
	for _, region := range parsed.Regions {
		if region.Language == core.LanguageJavaScript {
			script = region
		}
		if region.Kind == core.RegionASPExpression {
			expression = region
		}
	}
	valuesByOffset := map[int][]navigationValue{expression.Start: {
		{Kind: navigationValueLiteral, Primitive: navigationPrimitiveString, Text: "a.asp"},
		{Kind: navigationValueLiteral, Primitive: navigationPrimitiveString, Text: "b.asp"},
		{Kind: navigationValueLiteral, Primitive: navigationPrimitiveString, Text: "c.asp"},
	}}
	variants, err := navigationJavaScriptInterpolationVariantsContext(context.Background(), parsed, script, valuesByOffset)
	if err != nil {
		t.Fatal(err)
	}
	builder := newNavigationGraphBuilder("document", parsed.URI, nil)
	builder.javascriptContextualExtra = navigationJavaScriptContextualExtraAnalysisLimit - 1
	limited, err := builder.limitJavaScriptInterpolationVariants(parsed, script, valuesByOffset, variants)
	if err != nil {
		t.Fatal(err)
	}
	if len(limited) != 1 || limited[0].values[0].Kind != navigationValueUnknown || builder.javascriptContextualExtra != navigationJavaScriptContextualExtraAnalysisLimit {
		t.Fatalf("limited contextual variants = %#v, used=%d", limited, builder.javascriptContextualExtra)
	}
	limited, err = builder.limitJavaScriptInterpolationVariants(parsed, script, valuesByOffset, variants)
	if err != nil {
		t.Fatal(err)
	}
	if len(limited) != 1 || !limited[0].budgetFallback || builder.javascriptContextualExtra != navigationJavaScriptContextualExtraAnalysisLimit {
		t.Fatalf("exhausted contextual variants = %#v, used=%d", limited, builder.javascriptContextualExtra)
	}
}

func TestNavigationJavaScriptEvidenceSnippetsStayLinearAndCancellable(t *testing.T) {
	const count = 1024
	const expressionText = `<%=x%>`
	text := strings.Repeat(expressionText, count)
	document := core.NewTextDocument("file:///evidence.asp", "classic-asp", 0, text)
	regions := make([]core.Region, count)
	indices := make([]int, count)
	for index := range regions {
		start := index * len(expressionText)
		regions[index] = core.Region{Kind: core.RegionASPExpression, Start: start, End: start + len(expressionText)}
		indices[index] = index
	}
	candidateContext := &navigationCandidateContext{}
	if err := navigationJavaScriptAppendDependencyEvidence(context.Background(), candidateContext, &core.ParsedDocument{URI: document.URI, Text: text}, document, regions, indices); err != nil {
		t.Fatal(err)
	}
	total := 0
	for _, snippet := range candidateContext.snippets {
		if len(snippet) > 256 {
			t.Fatalf("evidence snippet length = %d, want <= 256", len(snippet))
		}
		total += len(snippet)
	}
	if total > count*256 {
		t.Fatalf("evidence snippet bytes = %d, want <= %d", total, count*256)
	}
	cancelled, cancel := context.WithCancel(context.Background())
	cancel()
	if err := navigationJavaScriptAppendDependencyEvidence(cancelled, &navigationCandidateContext{}, &core.ParsedDocument{URI: document.URI, Text: text}, document, regions, indices); !errors.Is(err, context.Canceled) {
		t.Fatalf("cancelled evidence error = %v, want %v", err, context.Canceled)
	}
}

func TestNavigationJavaScriptGraphBudgetPreservesUnknownOccurrences(t *testing.T) {
	const count = navigationJavaScriptContextualExtraAnalysisLimit + 1
	var source strings.Builder
	source.WriteString(`<% target = "next.asp" %>`)
	for range count {
		source.WriteString(`<script>location.href = <%= target %>;</script>`)
	}
	parsed := core.ParseDocument("file:///budget-occurrences.asp", source.String(), core.Settings{})
	builder := newNavigationGraphBuilder("document", parsed.URI, nil)
	provider := &countingNavigationJavaScriptProvider{delegate: typeScriptGoNavigationCandidates{ctx: context.Background()}}
	builder.javascriptCandidates = provider
	builder.addDocument(parsed, parsed.URI)
	if builder.navigationError != nil {
		t.Fatal(builder.navigationError)
	}
	total := 0
	for _, edge := range builder.edges {
		if edge["kind"] == "javascriptLocation" {
			edgeCount, _ := edge["count"].(int)
			total += edgeCount
		}
	}
	if total != count {
		t.Fatalf("JavaScript occurrence count = %d, want %d: %#v", total, count, builder.edges)
	}
	if provider.contextualCalls != navigationJavaScriptContextualExtraAnalysisLimit {
		t.Fatalf("contextual analysis calls = %d, want %d", provider.contextualCalls, navigationJavaScriptContextualExtraAnalysisLimit)
	}
	if !navigationPayloadNodeMatching(builder.payload(1), func(node map[string]any) bool { return node["kind"] == "unknown" }) {
		t.Fatalf("budget exhaustion omitted conservative unknown target: %#v", builder.nodes)
	}
}

func TestNavigationJavaScriptGraphBudgetPreservesDynamicHistoryOccurrences(t *testing.T) {
	const prefixCount = navigationJavaScriptContextualExtraAnalysisLimit / 2
	var source strings.Builder
	source.WriteString(`<% target = "next.asp" %>`)
	for range prefixCount {
		source.WriteString(`<script>location.href = <%= target %>;</script>`)
	}
	source.WriteString(`<script>history.pushState({}, "", <%= target %>);</script>`)
	source.WriteString(`<script>history.replaceState({}, "", "prefix-<%= target %>");</script>`)
	parsed := core.ParseDocument("file:///budget-history.asp", source.String(), core.Settings{})
	builder := newNavigationGraphBuilder("document", parsed.URI, nil)
	provider := &countingNavigationJavaScriptProvider{delegate: typeScriptGoNavigationCandidates{ctx: context.Background()}}
	builder.javascriptCandidates = provider
	builder.addDocument(parsed, parsed.URI)
	if builder.navigationError != nil {
		t.Fatal(builder.navigationError)
	}
	historyCount := 0
	for _, edge := range builder.edges {
		if edge["kind"] == "javascriptHistory" {
			count, _ := edge["count"].(int)
			historyCount += count
		}
	}
	if historyCount != 2 {
		t.Fatalf("budget history occurrence count = %d, want 2: %#v", historyCount, builder.edges)
	}
	if provider.contextualCalls != navigationJavaScriptContextualExtraAnalysisLimit {
		t.Fatalf("contextual analysis calls = %d, want %d", provider.contextualCalls, navigationJavaScriptContextualExtraAnalysisLimit)
	}
}

func TestNavigationJavaScriptDependencyMarkersCannotUseFixedSourceSentinel(t *testing.T) {
	candidate := navigationFiniteCandidate{
		Kind:  "javascriptLocation",
		Value: navigationValue{Kind: navigationValueLiteral, Text: "known.asp\x00ASP_NAV_DEP_0\x00"},
	}
	dependencies := navigationJavaScriptCandidateDependencies([]navigationFiniteCandidate{candidate}, "\x00ASP_NAV_DEP_runtime_nonce_", 1)
	if got := dependencies[navigationJavaScriptCandidateSinkKey(candidate)]; len(got) != 0 {
		t.Fatalf("source-controlled fixed sentinel dependencies = %#v, want none", got)
	}
}

func TestNavigationJavaScriptProviderUsesOriginalExpressionRangeForGeneratedText(t *testing.T) {
	root := t.TempDir()
	page := filepath.Join(root, "default.asp")
	target := filepath.Join(root, "next10.asp")
	if err := os.WriteFile(target, nil, 0o644); err != nil {
		t.Fatal(err)
	}
	source := "🌸\n<script>location.href = \"next<%= b %>.asp\";</script>"
	parsed := core.ParseDocument(filePathURI(page), source, core.Settings{})
	var script, expression core.Region
	for _, region := range parsed.Regions {
		if region.Language == core.LanguageJavaScript {
			script = region
		}
		if region.Kind == core.RegionASPExpression {
			expression = region
		}
	}
	provider := typeScriptGoNavigationCandidates{ctx: context.Background()}
	candidates, err := provider.CandidatesWithReplacements(parsed, script, []core.VirtualDocumentReplacement{{
		SourceStart: expression.Start,
		SourceEnd:   expression.End,
		Text:        "10",
	}}, []core.Region{expression})
	if err != nil {
		t.Fatal(err)
	}
	if len(candidates) != 1 {
		t.Fatalf("candidates = %#v, want one", candidates)
	}
	wantRange := core.NewTextDocument(parsed.URI, "classic-asp", 0, parsed.Text).Range(expression.Start, expression.End)
	if candidates[0].Range != wantRange {
		t.Fatalf("candidate range = %#v, want original expression range %#v", candidates[0].Range, wantRange)
	}
	ranges, _ := candidates[0].Extra["javascriptExpressionRanges"].([]lsp.Range)
	if len(ranges) != 1 || ranges[0] != wantRange {
		t.Fatalf("candidate evidence ranges = %#v, want %#v", candidates[0].Extra["javascriptExpressionRanges"], []lsp.Range{wantRange})
	}
}
