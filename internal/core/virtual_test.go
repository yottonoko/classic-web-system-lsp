package core

import (
	"context"
	"strings"
	"testing"
	"time"
)

func TestBuildVirtualDocumentExtractsStyleAttributes(t *testing.T) {
	parsed := ParseDocument("file:///site/default.asp", `<div style="display: block; color: <%= themeColor %>"></div>`, Settings{})
	css := BuildVirtualDocument(parsed, LanguageCSS)
	if !strings.Contains(css.Text, "*{display: block; color: xxxxxxxxxxxxxxxxx}\n") {
		t.Fatalf("css virtual text = %q", css.Text)
	}
	if len(css.Segments) == 0 {
		t.Fatalf("expected CSS source map segments")
	}
}

func TestBuildVirtualDocumentKeepsSourceOrderedMappingsAcrossCSSRegionKinds(t *testing.T) {
	source := `<div style="color: <%= themeColor %>"></div><style>.card { color: red; }</style>`
	parsed := ParseDocument("file:///site/css-order.asp", source, Settings{})
	css := BuildVirtualDocument(parsed, LanguageCSS)
	attributeOffset := strings.Index(source, "color:") + 1
	styleOffset := strings.LastIndex(source, "color:") + 1
	for _, offset := range []int{attributeOffset, styleOffset} {
		virtualOffset, ok := css.ToVirtualOffset(offset)
		if !ok {
			t.Fatalf("source offset %d did not map through ordered CSS segments: %#v", offset, css.Segments)
		}
		if sourceOffset, ok := css.ToSourceOffset(virtualOffset); !ok || sourceOffset != offset {
			t.Fatalf("CSS round trip = %d, %v; want %d", sourceOffset, ok, offset)
		}
	}
	attribute := firstRegionOfKind(parsed, RegionStyleAttribute)
	if attribute == nil {
		t.Fatal("style attribute region missing")
	}
	fragment := BuildEmbeddedRegionVirtualDocument(parsed, *attribute)
	if strings.Contains(fragment.Text, "themeColor") || !strings.Contains(fragment.Text, "*{color: x") {
		t.Fatalf("CSS region fragment did not preserve ASP masking: %q", fragment.Text)
	}
}

func TestBuildEmbeddedRegionVirtualDocumentWithReplacementsAdjustsSourceMappings(t *testing.T) {
	source := `<style>.card { color: <%= color %>; background: <%= background %>; }</style>`
	parsed := ParseDocument("file:///site/css-replacements.asp", source, Settings{})
	owner := firstRegionOfKind(parsed, RegionStyle)
	holes := regionsOfKinds(parsed, RegionASPExpression)
	if owner == nil || len(holes) != 2 {
		t.Fatalf("style owner or ASP holes missing: owner=%#v holes=%#v", owner, holes)
	}
	doc := NewTextDocument(parsed.URI, "classic-asp", 0, source)
	tests := []struct {
		name         string
		replacements []VirtualDocumentReplacement
		want         string
	}{
		{
			name: "shorter",
			replacements: []VirtualDocumentReplacement{{
				SourceStart: holes[0].Start,
				SourceEnd:   holes[0].End,
				Text:        "x",
			}},
			want: "color: x;",
		},
		{
			name: "longer",
			replacements: []VirtualDocumentReplacement{{
				SourceStart: holes[0].Start,
				SourceEnd:   holes[0].End,
				Text:        "generatedColorValue",
			}},
			want: "color: generatedColorValue;",
		},
		{
			name: "multiple",
			replacements: []VirtualDocumentReplacement{
				{SourceStart: holes[0].Start, SourceEnd: holes[0].End, Text: "x"},
				{SourceStart: holes[1].Start, SourceEnd: holes[1].End, Text: "generatedBackgroundValue"},
			},
			want: "background: generatedBackgroundValue;",
		},
	}
	for _, testCase := range tests {
		t.Run(testCase.name, func(t *testing.T) {
			virtual := BuildEmbeddedRegionVirtualDocumentWithReplacements(parsed, *owner, testCase.replacements)
			if !strings.Contains(virtual.Text, testCase.want) {
				t.Fatalf("virtual text = %q, want %q", virtual.Text, testCase.want)
			}
			for _, needle := range []string{"color", "background"} {
				sourceStart := strings.Index(source, needle)
				virtualStart, ok := virtual.ToVirtualOffset(sourceStart)
				if !ok {
					t.Fatalf("source %q did not map into virtual document: %#v", needle, virtual.Segments)
				}
				mapped, ok := virtual.ToSourceOffset(virtualStart)
				if !ok || mapped != sourceStart {
					t.Fatalf("source %q round trip = %d, %t; want %d", needle, mapped, ok, sourceStart)
				}
			}
			if virtual.runtimeDoc == nil {
				t.Fatal("replacement virtual document did not attach its runtime document")
			}
			if testCase.name == "longer" {
				generatedStart := strings.Index(virtual.Text, "generatedColorValue")
				if generatedStart < 0 {
					t.Fatal("longer replacement text missing")
				}
				if _, ok := virtual.SourceRange(doc, generatedStart, generatedStart+len("generatedColorValue")); ok {
					t.Fatal("generated replacement unexpectedly mapped to source")
				}
			}
		})
	}
}

func TestBuildEmbeddedRegionVirtualDocumentWithReplacementsLeavesGeneratedRangesUnmapped(t *testing.T) {
	source := `<style>.card { color: <%= color %>; }</style>`
	parsed := ParseDocument("file:///site/css-generated-range.asp", source, Settings{})
	owner := firstRegionOfKind(parsed, RegionStyle)
	hole := firstRegionOfKind(parsed, RegionASPExpression)
	if owner == nil || hole == nil {
		t.Fatalf("style owner or ASP hole missing: owner=%#v hole=%#v", owner, hole)
	}
	virtual := BuildEmbeddedRegionVirtualDocumentWithReplacements(parsed, *owner, []VirtualDocumentReplacement{{
		SourceStart: hole.Start,
		SourceEnd:   hole.End,
		Text:        "generatedValue",
	}})
	doc := NewTextDocument(parsed.URI, "classic-asp", 0, source)
	generatedStart := strings.Index(virtual.Text, "generatedValue")
	generatedEnd := generatedStart + len("generatedValue")
	if generatedStart < 0 {
		t.Fatalf("replacement text missing from virtual document: %q", virtual.Text)
	}
	if _, ok := virtual.SourceRange(doc, generatedStart, generatedEnd); ok {
		t.Fatalf("generated range unexpectedly mapped: %#v", virtual.Segments)
	}
	if _, ok := virtual.ToSourceOffset(generatedStart + 1); ok {
		t.Fatalf("generated offset unexpectedly mapped: %#v", virtual.Segments)
	}
	after := strings.Index(source, "; }")
	afterVirtual, ok := virtual.ToVirtualOffset(after)
	if !ok {
		t.Fatalf("source after generated range did not map: %#v", virtual.Segments)
	}
	if mapped, ok := virtual.ToSourceOffset(afterVirtual); !ok || mapped != after {
		t.Fatalf("source after generated range round trip = %d, %t; want %d", mapped, ok, after)
	}
}

func TestBuildEmbeddedRegionVirtualDocumentWithReplacementsRejectsInvalidOrOverlappingRanges(t *testing.T) {
	source := `<style>.card { color: <%= color %>; }</style>`
	parsed := ParseDocument("file:///site/css-invalid-replacements.asp", source, Settings{})
	owner := firstRegionOfKind(parsed, RegionStyle)
	hole := firstRegionOfKind(parsed, RegionASPExpression)
	if owner == nil || hole == nil {
		t.Fatalf("style owner or ASP hole missing: owner=%#v hole=%#v", owner, hole)
	}
	base := BuildEmbeddedRegionVirtualDocument(parsed, *owner)
	tests := []struct {
		name         string
		replacements []VirtualDocumentReplacement
	}{
		{
			name: "out of bounds",
			replacements: []VirtualDocumentReplacement{{
				SourceStart: -1,
				SourceEnd:   1,
				Text:        "invalid",
			}},
		},
		{
			name: "non ASP range",
			replacements: []VirtualDocumentReplacement{{
				SourceStart: strings.Index(source, "color"),
				SourceEnd:   strings.Index(source, "color") + len("color"),
				Text:        "invalid",
			}},
		},
		{
			name: "overlap",
			replacements: []VirtualDocumentReplacement{
				{SourceStart: hole.Start, SourceEnd: hole.End, Text: "first"},
				{SourceStart: hole.Start, SourceEnd: hole.End, Text: "second"},
			},
		},
	}
	for _, testCase := range tests {
		t.Run(testCase.name, func(t *testing.T) {
			virtual := BuildEmbeddedRegionVirtualDocumentWithReplacements(parsed, *owner, testCase.replacements)
			if virtual.Text != base.Text {
				t.Fatalf("invalid replacements changed virtual text: got %q, base %q", virtual.Text, base.Text)
			}
			if len(virtual.Segments) != len(base.Segments) {
				t.Fatalf("invalid replacements changed source-map segment count: got %d, base %d", len(virtual.Segments), len(base.Segments))
			}
		})
	}
}

func TestBuildEmbeddedRegionVirtualDocumentWithReplacementsPreservesUTF16Mappings(t *testing.T) {
	source := `<style>.😀 { color: <%= color %>; background: "青"; }</style>`
	parsed := ParseDocument("file:///site/css-utf16-replacements.asp", source, Settings{})
	owner := firstRegionOfKind(parsed, RegionStyle)
	hole := firstRegionOfKind(parsed, RegionASPExpression)
	if owner == nil || hole == nil {
		t.Fatalf("style owner or ASP hole missing: owner=%#v hole=%#v", owner, hole)
	}
	virtual := BuildEmbeddedRegionVirtualDocumentWithReplacements(parsed, *owner, []VirtualDocumentReplacement{{
		SourceStart: hole.Start,
		SourceEnd:   hole.End,
		Text:        "generatedValue",
	}})
	doc := NewTextDocument(parsed.URI, "classic-asp", 0, source)
	sourceStart := strings.Index(source, "background")
	sourceEnd := sourceStart + len("background")
	virtualStart, ok := virtual.ToVirtualOffset(sourceStart)
	if !ok {
		t.Fatal("UTF-16 source range start did not map")
	}
	virtualEnd, ok := virtual.ToVirtualOffset(sourceEnd)
	if !ok {
		t.Fatal("UTF-16 source range end did not map")
	}
	virtualRange := virtual.runtimeTextDocument().Range(virtualStart, virtualEnd)
	mapped, ok := virtual.SourceRangeForVirtualRange(doc, virtualRange)
	if !ok || mapped != doc.Range(sourceStart, sourceEnd) {
		t.Fatalf("UTF-16 virtual range = %#v, %t; want %#v", mapped, ok, doc.Range(sourceStart, sourceEnd))
	}
	sourcePosition := doc.PositionAt(sourceStart)
	virtualPosition, ok := virtual.ToVirtualPosition(sourcePosition, doc)
	if !ok {
		t.Fatal("UTF-16 source position did not map")
	}
	roundTrip, ok := virtual.ToSourcePosition(virtualPosition, doc)
	if !ok || roundTrip != sourcePosition {
		t.Fatalf("UTF-16 position round trip = %#v, %t; want %#v", roundTrip, ok, sourcePosition)
	}
}

func TestBuildEmbeddedRegionVirtualDocumentWithReplacementsHandlesManyHolesInOnePass(t *testing.T) {
	const holeCount = 1024
	var source strings.Builder
	source.WriteString("<script>")
	for range holeCount {
		source.WriteString(`x=<%= value %>;`)
	}
	source.WriteString("tail();</script>")
	parsed := ParseDocument("file:///site/many-replacements.asp", source.String(), Settings{})
	owner := firstRegionOfKind(parsed, RegionClientScript)
	if owner == nil {
		t.Fatalf("client script region missing: %#v", parsed.Regions)
	}
	replacements := make([]VirtualDocumentReplacement, 0, holeCount)
	for _, region := range parsed.Regions {
		if region.Kind == RegionASPExpression && region.Start >= owner.ContentStart && region.End <= owner.ContentEnd {
			replacements = append(replacements, VirtualDocumentReplacement{SourceStart: region.Start, SourceEnd: region.End, Text: `"x"`})
		}
	}
	if len(replacements) != holeCount {
		t.Fatalf("replacement holes = %d, want %d", len(replacements), holeCount)
	}
	virtual := BuildEmbeddedRegionVirtualDocumentWithReplacements(parsed, *owner, replacements)
	if got := strings.Count(virtual.Text, `x="x";`); got != holeCount {
		t.Fatalf("rendered replacements = %d, want %d", got, holeCount)
	}
	tailOffset := strings.Index(source.String(), "tail()")
	mapped, ok := virtual.ToVirtualOffset(tailOffset)
	if !ok || virtual.Text[mapped:mapped+len("tail()")] != "tail()" {
		t.Fatalf("tail mapping = (%d, %v), text=%q", mapped, ok, virtual.Text)
	}
}

func TestBuildEmbeddedRegionVirtualDocumentWithReplacementsCancelsDuringManyHoles(t *testing.T) {
	const holeCount = 256
	var source strings.Builder
	source.WriteString("<script>")
	for range holeCount {
		source.WriteString(`x=<%= value %>;`)
	}
	source.WriteString("</script>")
	parsed := ParseDocument("file:///site/cancel-replacements.asp", source.String(), Settings{})
	owner := firstRegionOfKind(parsed, RegionClientScript)
	if owner == nil {
		t.Fatal("client script region missing")
	}
	replacements := make([]VirtualDocumentReplacement, 0, holeCount)
	for _, region := range parsed.Regions {
		if region.Kind == RegionASPExpression {
			replacements = append(replacements, VirtualDocumentReplacement{SourceStart: region.Start, SourceEnd: region.End, Text: `"x"`})
		}
	}
	ctx := &cancelAfterContext{after: 32}
	if _, err := BuildEmbeddedRegionVirtualDocumentWithReplacementsContext(ctx, parsed, *owner, replacements); err != context.Canceled {
		t.Fatalf("mid-build cancellation error = %v, want %v", err, context.Canceled)
	}
	if ctx.checks <= ctx.after {
		t.Fatalf("context checks = %d, want more than %d", ctx.checks, ctx.after)
	}
}

type cancelAfterContext struct {
	checks int
	after  int
}

func (*cancelAfterContext) Deadline() (time.Time, bool) { return time.Time{}, false }
func (*cancelAfterContext) Done() <-chan struct{}       { return nil }
func (ctx *cancelAfterContext) Err() error {
	ctx.checks++
	if ctx.checks > ctx.after {
		return context.Canceled
	}
	return nil
}
func (*cancelAfterContext) Value(any) any { return nil }

func TestVirtualDocumentSourceRangeRejectsRangesAcrossMaskedASPHoles(t *testing.T) {
	source := `<style>.card { color: red; <% If enabled Then %> background: blue; }</style>`
	parsed := ParseDocument("file:///site/css-range.asp", source, Settings{})
	doc := NewTextDocument(parsed.URI, "classic-asp", 0, source)
	css := BuildVirtualDocument(parsed, LanguageCSS)
	start := strings.Index(css.Text, "color")
	end := strings.Index(css.Text, "background") + len("background")
	if _, ok := css.SourceRange(doc, start, end); ok {
		t.Fatalf("CSS range crossing a masked ASP hole was mapped: %#v", css.Segments)
	}
	backgroundStart := strings.Index(css.Text, "background")
	mapped, ok := css.SourceRange(doc, backgroundStart, backgroundStart+len("background"))
	if !ok || mapped != doc.Range(strings.Index(source, "background"), strings.Index(source, "background")+len("background")) {
		t.Fatalf("CSS range after a masked ASP hole = %#v, ok=%t", mapped, ok)
	}
}

func TestVirtualDocumentSourceRangeMapsEmptyRangesAtSegmentBoundaries(t *testing.T) {
	source := `<div style=""></div>`
	parsed := ParseDocument("file:///site/empty-style-range.asp", source, Settings{})
	doc := NewTextDocument(parsed.URI, "classic-asp", 0, source)
	css := BuildVirtualDocument(parsed, LanguageCSS)
	virtualOffset := strings.Index(css.Text, "*{") + len("*{")
	sourceOffset := strings.Index(source, `style="`) + len(`style="`)
	mapped, ok := css.SourceRange(doc, virtualOffset, virtualOffset)
	if !ok || mapped != doc.Range(sourceOffset, sourceOffset) {
		t.Fatalf("empty CSS range = %#v, ok=%t, want source offset %d", mapped, ok, sourceOffset)
	}
}

func TestBuildVirtualDocumentMasksASPInHTML(t *testing.T) {
	parsed := ParseDocument("file:///site/default.asp", `<main><%= title %></main>`, Settings{})
	html := BuildVirtualDocument(parsed, LanguageHTML)
	if strings.Contains(html.Text, "<%= title %>") {
		t.Fatalf("html virtual should mask ASP expression: %q", html.Text)
	}
	if !strings.Contains(html.Text, "<main>") || !strings.Contains(html.Text, "</main>") {
		t.Fatalf("html virtual should preserve HTML: %q", html.Text)
	}
}

func TestBuildVirtualDocumentMasksEmbeddedScriptAndStyleBodiesInHTML(t *testing.T) {
	source := `<main>before</main><style>.card { color: red; }</style><script>const value = 1;</script><footer>after</footer>`
	parsed := ParseDocument("file:///site/html-embedded-bodies.asp", source, Settings{})
	html := BuildVirtualDocument(parsed, LanguageHTML)
	if strings.Contains(html.Text, ".card { color: red; }") || strings.Contains(html.Text, "const value = 1;") {
		t.Fatalf("HTML virtual document leaked embedded bodies: %q", html.Text)
	}
	for _, tag := range []string{"<style>", "</style>", "<script>", "</script>", "before", "after"} {
		if !strings.Contains(html.Text, tag) {
			t.Fatalf("HTML virtual document lost %q: %q", tag, html.Text)
		}
	}
	styleOffset := strings.Index(source, ".card") + 1
	scriptOffset := strings.Index(source, "const value") + 1
	if _, ok := html.ToVirtualOffset(styleOffset); ok {
		t.Fatalf("style body should not map into HTML virtual document: %#v", html.Segments)
	}
	if _, ok := html.ToVirtualOffset(scriptOffset); ok {
		t.Fatalf("script body should not map into HTML virtual document: %#v", html.Segments)
	}
	if _, ok := html.ToVirtualOffset(strings.Index(source, "</script>")); !ok {
		t.Fatalf("script closing tag should map into HTML virtual document: %#v", html.Segments)
	}
}

func TestBuildVirtualDocumentPreservesJavaScriptAroundASPHoles(t *testing.T) {
	source := `<script>
const fromAsp = <%= value %>;
missingAfterIsland();
</script>`
	parsed := ParseDocument("file:///site/default.asp", source, Settings{})
	js := BuildVirtualDocument(parsed, LanguageJavaScript)
	if !strings.Contains(js.Text, "const fromAsp") || !strings.Contains(js.Text, "missingAfterIsland") {
		t.Fatalf("javascript virtual text = %q", js.Text)
	}
	if strings.Contains(js.Text, "<%= value %>") {
		t.Fatalf("javascript virtual should mask ASP expression: %q", js.Text)
	}
}

func TestBuildVirtualDocumentMasksQuotedJavaScriptASPHoles(t *testing.T) {
	source := `<script>
const doubleQuoted = "<%= "double js" %>";
const singleQuoted = '<% 'single js %>';
const templated = ` + "`<% template js %>`" + `;
const fromAsp = <%= value %>;
</script>`
	parsed := ParseDocument("file:///site/default.asp", source, Settings{})
	js := BuildVirtualDocument(parsed, LanguageJavaScript)
	for _, leaked := range []string{"double js", "single js"} {
		if strings.Contains(js.Text, leaked) {
			t.Fatalf("javascript virtual leaked %q in %q", leaked, js.Text)
		}
	}
	if strings.Contains(js.Text, "<%= value %>") {
		t.Fatalf("javascript virtual leaked real ASP expression in %q", js.Text)
	}
	if !strings.Contains(js.Text, "<% template js %>") {
		t.Fatalf("javascript virtual lost template literal text in %q", js.Text)
	}
}

func TestRegionAtChoosesMostSpecificRegion(t *testing.T) {
	source := `<style>.x { color: red; }</style>`
	parsed := ParseDocument("file:///site/default.asp", source, Settings{})
	offset := strings.Index(source, "color")
	region := RegionAt(parsed, offset)
	if region == nil || region.Kind != RegionStyle {
		t.Fatalf("region at style content = %#v", region)
	}
}

func TestRegionAtKeepsStyleAfterASPHoles(t *testing.T) {
	source := `<style>.card-<%= className %> { color: <%= themeColor %>; background: red; }</style>`
	parsed := ParseDocument("file:///site/default.asp", source, Settings{})
	styleOffset := strings.Index(source, "background")
	styleRegion := RegionAt(parsed, styleOffset)
	if styleRegion == nil || styleRegion.Kind != RegionStyle {
		t.Fatalf("region at style content after ASP holes = %#v", styleRegion)
	}
	aspOffset := strings.Index(source, "themeColor")
	aspRegion := RegionAt(parsed, aspOffset)
	if aspRegion == nil || aspRegion.Kind != RegionASPExpression {
		t.Fatalf("region at ASP expression inside style = %#v", aspRegion)
	}
}

func TestRegionAtKeepsStyleCloseTagAsHTML(t *testing.T) {
	source := `<style>.x { color: red; }</style>`
	parsed := ParseDocument("file:///site/default.asp", source, Settings{})
	offset := strings.Index(source, "</style>") + 2
	region := RegionAt(parsed, offset)
	if region == nil || region.Kind != RegionHTML {
		t.Fatalf("region at style close tag = %#v", region)
	}
}

func TestRegionAtUsesHalfOpenContentAndRegionBoundaries(t *testing.T) {
	source := `<div><style>.x { color: red; }</style><script>const value = 1;</script><% value = 1 %></div>`
	parsed := ParseDocument("file:///site/region-boundaries.asp", source, Settings{})
	style := firstRegionOfKind(parsed, RegionStyle)
	script := firstRegionOfKind(parsed, RegionClientScript)
	block := firstRegionOfKind(parsed, RegionASPBlock)
	if style == nil || script == nil || block == nil {
		t.Fatalf("regions = %#v", parsed.Regions)
	}
	for _, testCase := range []struct {
		name   string
		offset int
		kind   RegionKind
	}{
		{name: "style tag start", offset: style.Start, kind: RegionHTML},
		{name: "style content start", offset: style.ContentStart, kind: RegionStyle},
		{name: "style content end", offset: style.ContentEnd, kind: RegionHTML},
		{name: "style region end", offset: style.End, kind: RegionHTML},
		{name: "script content start", offset: script.ContentStart, kind: RegionClientScript},
		{name: "script content end", offset: script.ContentEnd, kind: RegionHTML},
		{name: "ASP region end", offset: block.End, kind: RegionHTML},
	} {
		t.Run(testCase.name, func(t *testing.T) {
			region := RegionAt(parsed, testCase.offset)
			if region == nil || region.Kind != testCase.kind {
				t.Fatalf("RegionAt(%d) = %#v, want %s", testCase.offset, region, testCase.kind)
			}
		})
	}
	if RegionAt(parsed, -1) != nil {
		t.Fatal("negative offset unexpectedly matched a region")
	}
}

func TestBuildVirtualDocumentManyScriptsKeepsNestedMasks(t *testing.T) {
	text := strings.Repeat("<script>var a = '<%= x %>';</script>\n", 4000)
	parsed := ParseDocument("file:///many.asp", text, Settings{DefaultLanguage: "VBScript"})
	virtual := BuildVirtualDocument(parsed, LanguageJavaScript)
	if strings.Contains(virtual.Text, "<%") {
		t.Fatal("nested ASP expression leaked into JavaScript virtual document")
	}
	if got := strings.Count(virtual.Text, "var a = "); got != 4000 {
		t.Fatalf("script count = %d, want 4000", got)
	}
}
