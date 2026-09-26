package core

import (
	"strings"
	"testing"

	"github.com/yottonoko/classic-web-system-lsp/internal/lsp"
)

func TestParserUpdateParityAppliesHTMLChangesAndShiftsIncludeRanges(t *testing.T) {
	source := `<div>hello</div>
<!-- #include file="common.inc" -->
<% Response.Write "ok" %>`
	next, parsed := parseAfterNeedleReplacement(t, "file:///site/default.asp", source, "hello", "hello world")
	fresh := ParseDocument("file:///site/default.asp", next, Settings{DefaultLanguage: "VBScript"})

	if !parsedDocumentsEqual(parsed, fresh) {
		t.Fatalf("updated parse did not match fresh parse")
	}
	if len(parsed.Includes) != 1 || parsed.Includes[0].Range.Start.Line != 1 {
		t.Fatalf("include range after HTML edit = %#v", parsed.Includes)
	}
}

func TestIncrementalImpactInvalidatesOnlyEditedEmbeddedLanguage(t *testing.T) {
	const uri = "file:///site/impact.asp"
	source := "<div>old</div><style>.x{color:red}</style><script>const value = 1</script><% Dim keep %>"
	parsed := ParseDocument(uri, source, Settings{DefaultLanguage: "VBScript"})
	document := NewTextDocument(uri, "classic-asp", 1, source)
	start := strings.Index(source, "red")
	changeRange := document.Range(start, start+len("red"))
	updated := UpdateParsedDocument(parsed, []IncrementalChange{{Range: &changeRange, Text: "blue"}}, Settings{DefaultLanguage: "VBScript"})
	if !updated.Incremental {
		t.Fatalf("CSS update fell back: %s", updated.Reason)
	}
	if !updated.Impact.Affects(LanguageCSS) {
		t.Fatal("CSS impact did not invalidate CSS")
	}
	for _, language := range []EmbeddedLanguage{LanguageHTML, LanguageJavaScript, LanguageJScript, LanguageVBScript} {
		if updated.Impact.Affects(language) {
			t.Fatalf("CSS update unexpectedly invalidated %s", language)
		}
	}
	if previousText, ok := updated.Parsed.PreviousRevisionText(); !ok || previousText != parsed.Text {
		t.Fatal("incremental revision did not retain its predecessor source")
	}
}

func TestIncrementalImpactTreatsSafeHostGapAsHTMLOnly(t *testing.T) {
	const uri = "file:///site/host-gap-impact.asp"
	source := "<div></div>\n<% Dim keep %>\n<h"
	parsed := ParseDocument(uri, source, Settings{DefaultLanguage: "VBScript"})
	document := NewTextDocument(uri, "classic-asp", 1, source)
	start := strings.LastIndex(source, "h")
	changeRange := document.Range(start, start+1)
	updated := UpdateParsedDocument(parsed, []IncrementalChange{{Range: &changeRange, Text: "H"}}, Settings{DefaultLanguage: "VBScript"})
	if !updated.Incremental {
		t.Fatalf("host HTML update fell back: %s", updated.Reason)
	}
	if !updated.Impact.Affects(LanguageHTML) {
		t.Fatal("host HTML update did not invalidate HTML")
	}
	for _, language := range []EmbeddedLanguage{LanguageCSS, LanguageJavaScript, LanguageJScript, LanguageVBScript} {
		if updated.Impact.Affects(language) {
			t.Fatalf("host HTML update unexpectedly invalidated %s", language)
		}
	}
	fresh := ParseDocument(uri, updated.Parsed.Text, Settings{DefaultLanguage: "VBScript"})
	if !parsedDocumentsEqual(updated.Parsed, fresh) {
		t.Fatal("host HTML incremental parse differs from fresh parse")
	}
}

func TestMultipleLSPChangesApplySequentialRangesAndUTF16Offsets(t *testing.T) {
	const uri = "file:///site/multiple-sequential-changes.asp"
	source := "<div>😀ab</div>"
	parsed := ParseDocument(uri, source, Settings{DefaultLanguage: "VBScript"})
	document := NewTextDocument(uri, "classic-asp", 1, source)
	changes := make([]IncrementalChange, 0, 2)
	apply := func(start, end int, text string) {
		changeRange := document.Range(start, end)
		changes = append(changes, IncrementalChange{
			Range:        &changeRange,
			Text:         text,
			ByteStart:    document.OffsetAt(changeRange.Start),
			ByteEnd:      document.OffsetAt(changeRange.End),
			HasByteRange: true,
		})
		document.ApplyChange(&changeRange, text, document.Version+1)
	}
	apply(strings.Index(document.Text, "a"), strings.Index(document.Text, "a"), "X")
	apply(strings.Index(document.Text, "b"), strings.Index(document.Text, "b")+1, "YZ")

	updated := UpdateParsedDocument(parsed, changes, Settings{DefaultLanguage: "VBScript"})
	if updated.Parsed.Text != document.Text {
		t.Fatalf("multi-change text = %q, want sequential result %q", updated.Parsed.Text, document.Text)
	}
	fresh := ParseDocument(uri, document.Text, Settings{DefaultLanguage: "VBScript"})
	if !parsedDocumentsEqual(updated.Parsed, fresh) {
		t.Fatal("multi-change fallback differs from fresh parse")
	}
}

func TestIncrementalImpactCarriesUnaffectedVirtualDocumentsBySourceMapDelta(t *testing.T) {
	const uri = "file:///site/virtual-impact.asp"
	source := "<main>old</main><style>.x{color:red}</style><script>const value = 1</script><% Dim keep %>"
	previous := ParseDocument(uri, source, Settings{DefaultLanguage: "VBScript"})
	previousJavaScript := BuildVirtualDocument(previous, LanguageJavaScript)
	previousCSS := BuildVirtualDocument(previous, LanguageCSS)
	previousVBScript := BuildVirtualDocument(previous, LanguageVBScript)
	document := NewTextDocument(uri, "classic-asp", 1, source)
	start := strings.Index(source, "old")
	changeRange := document.Range(start, start+len("old"))
	updated := UpdateParsedDocument(previous, []IncrementalChange{{Range: &changeRange, Text: "new value"}}, Settings{DefaultLanguage: "VBScript"})
	if !updated.Incremental {
		t.Fatalf("HTML update fell back: %s", updated.Reason)
	}

	delta := len("new value") - len("old")
	for _, testCase := range []struct {
		language EmbeddedLanguage
		previous VirtualDocument
	}{
		{LanguageJavaScript, previousJavaScript},
		{LanguageCSS, previousCSS},
		{LanguageVBScript, previousVBScript},
	} {
		runtimeKey := "core.virtual-document.runtime.v1." + string(testCase.language)
		if _, ok := updated.Parsed.LoadRuntimeAnalysis(runtimeKey); !ok {
			t.Fatalf("%s virtual document was not inherited", testCase.language)
		}
		current := BuildVirtualDocument(updated.Parsed, testCase.language)
		if current.Text != testCase.previous.Text || len(current.Segments) != len(testCase.previous.Segments) {
			t.Fatalf("%s virtual content changed: before=%#v after=%#v", testCase.language, testCase.previous, current)
		}
		for index := range current.Segments {
			if current.Segments[index].SourceStart != testCase.previous.Segments[index].SourceStart+delta || current.Segments[index].SourceEnd != testCase.previous.Segments[index].SourceEnd+delta {
				t.Fatalf("%s segment %d was not shifted by %d: before=%#v after=%#v", testCase.language, index, delta, testCase.previous.Segments[index], current.Segments[index])
			}
		}
	}
	if _, ok := updated.Parsed.LoadRuntimeAnalysis("core.virtual-document.runtime.v1." + string(LanguageHTML)); ok {
		t.Fatal("affected HTML virtual document was inherited")
	}
}

func TestIncrementalImpactCarriesMaskedHTMLAcrossEmbeddedLineInsertion(t *testing.T) {
	const uri = "file:///site/html-mask-impact.asp"
	source := "<main>before</main><style>.x{color:red}</style><footer>after</footer>"
	previous := ParseDocument(uri, source, Settings{DefaultLanguage: "VBScript"})
	previousHTML := BuildVirtualDocument(previous, LanguageHTML)
	document := NewTextDocument(uri, "classic-asp", 1, source)
	start := strings.Index(source, "red")
	changeRange := document.Range(start, start+len("red"))
	updated := UpdateParsedDocument(previous, []IncrementalChange{{Range: &changeRange, Text: "blue\n"}}, Settings{DefaultLanguage: "VBScript"})
	if !updated.Incremental {
		t.Fatalf("CSS update fell back: %s", updated.Reason)
	}
	runtimeKey := "core.virtual-document.runtime.v1." + string(LanguageHTML)
	if _, ok := updated.Parsed.LoadRuntimeAnalysis(runtimeKey); !ok {
		t.Fatal("masked HTML virtual document was not inherited")
	}
	current := BuildVirtualDocument(updated.Parsed, LanguageHTML)
	fresh := BuildVirtualDocument(ParseDocument(uri, updated.Parsed.Text, Settings{DefaultLanguage: "VBScript"}), LanguageHTML)
	if current.Text != fresh.Text {
		t.Fatalf("incremental HTML mask differs from fresh parse:\ncurrent=%q\nfresh=%q", current.Text, fresh.Text)
	}
	if previousHTML.Text == current.Text {
		t.Fatal("masked HTML virtual document did not reflect inserted line")
	}
	if len(current.Segments) != len(fresh.Segments) {
		t.Fatalf("HTML segment count = %d, want %d", len(current.Segments), len(fresh.Segments))
	}
	for index := range current.Segments {
		if current.Segments[index] != fresh.Segments[index] {
			t.Fatalf("HTML segment %d = %#v, want %#v", index, current.Segments[index], fresh.Segments[index])
		}
	}
}

func TestParserUpdateParityMapsShiftedIncludeRangesAgainstUpdatedText(t *testing.T) {
	source := `<div>top</div>
<!-- #include file="common.inc" -->
<% Response.Write "ok" %>`
	next, parsed := parseAfterNeedleReplacement(t, "file:///site/default.asp", source, "top", "top\nnext")
	fresh := ParseDocument("file:///site/default.asp", next, Settings{DefaultLanguage: "VBScript"})
	if len(parsed.Includes) != 1 || len(fresh.Includes) != 1 || parsed.Includes[0].Range != fresh.Includes[0].Range {
		t.Fatalf("updated include range = %#v, fresh = %#v", parsed.Includes, fresh.Includes)
	}
}

func TestParserUpdateParityAppliesVBScriptChangesLikeFreshParse(t *testing.T) {
	source := `<%
Dim message
message = "ok"
Response.Write message
%>`
	next, parsed := parseAfterNeedleReplacement(t, "file:///site/default.asp", source, `"ok"`, `"ready"`)
	fresh := ParseDocument("file:///site/default.asp", next, Settings{DefaultLanguage: "VBScript"})

	if !parsedDocumentsEqual(parsed, fresh) {
		t.Fatalf("updated VBScript parse did not match fresh parse")
	}
	block := firstRegionOfKind(parsed, RegionASPBlock)
	if block == nil || !strings.Contains(next[block.ContentStart:block.ContentEnd], `"ready"`) {
		t.Fatalf("VBScript block after edit = %#v", block)
	}
}

func TestParserUpdateParityKeepsAstralCharactersAlignedAcrossVBScriptChanges(t *testing.T) {
	source := `<section>prefix 🍰</section>
<%
Dim message
message = "ok 🍬"
Response.Write message
%>
<div data-title="<%= message %>">suffix 🍡</div>`
	next, parsed := parseAfterNeedleReplacement(t, "file:///site/astral-incremental.asp", source, `"ok 🍬"`, `"ready 🍮"`)
	fresh := ParseDocument("file:///site/astral-incremental.asp", next, Settings{DefaultLanguage: "VBScript"})

	if !parsedDocumentsEqual(parsed, fresh) {
		t.Fatalf("updated astral parse did not match fresh parse")
	}
	doc := NewTextDocument("file:///site/astral-incremental.asp", "classic-asp", 0, next)
	position := doc.PositionAt(strings.Index(next, "ready 🍮"))
	if position.Line != 3 || position.Character != len(`message = "`) {
		t.Fatalf("astral edit position = %#v", position)
	}
}

func TestParserUpdateParityHandlesLargeSafeVBScriptReplacements(t *testing.T) {
	payload := strings.Repeat("x", 1200)
	source := `<%
Dim message
message = "` + payload + `"
Response.Write message
%>`
	for _, length := range []int{256, 512, 1024} {
		start := strings.Index(source, payload) + 8
		end := start + length
		next, parsed := parseAfterReplacement(t, "file:///site/default.asp", source, start, end, strings.Repeat("y", length))
		fresh := ParseDocument("file:///site/default.asp", next, Settings{DefaultLanguage: "VBScript"})
		if !parsedDocumentsEqual(parsed, fresh) {
			t.Fatalf("updated parse for %d-byte replacement did not match fresh parse", length)
		}
	}
}

func TestParserUpdateParityRescansTypedStyleAttributeRegions(t *testing.T) {
	source := `<div ></div>`
	insertAt := strings.Index(source, "<div ") + len("<div ")
	next, parsed := parseAfterReplacement(t, "file:///site/typed-style-attribute.asp", source, insertAt, insertAt, `style="display: block;"`)
	fresh := ParseDocument("file:///site/typed-style-attribute.asp", next, Settings{DefaultLanguage: "VBScript"})

	if !parsedDocumentsEqual(parsed, fresh) {
		t.Fatalf("updated style attribute parse did not match fresh parse")
	}
	style := firstRegionOfKind(parsed, RegionStyleAttribute)
	if style == nil || next[style.ContentStart:style.ContentEnd] != "display: block;" {
		t.Fatalf("style attribute region after edit = %#v", style)
	}
	if css := BuildVirtualDocument(parsed, LanguageCSS).Text; !strings.Contains(css, "*{display: block;}") {
		t.Fatalf("CSS virtual document after style edit = %q", css)
	}
}

func TestParserUpdateParityRescansStyleAttributeTypedOneCharacterAtATime(t *testing.T) {
	text := `<div ></div>`
	parsed := ParseDocument("file:///site/typed-style-attribute.asp", text, Settings{DefaultLanguage: "VBScript"})
	offset := strings.Index(text, "<div ") + len("<div ")
	for _, char := range `style="di"` {
		change := IncrementalChange{
			Range: &lsp.Range{
				Start: NewTextDocument(parsed.URI, "classic-asp", 0, text).PositionAt(offset),
				End:   NewTextDocument(parsed.URI, "classic-asp", 0, text).PositionAt(offset),
			},
			Text: string(char),
		}
		parsed = UpdateParsedDocument(parsed, []IncrementalChange{change}, Settings{DefaultLanguage: "VBScript"}).Parsed
		text = text[:offset] + string(char) + text[offset:]
		offset += len(string(char))
	}

	fresh := ParseDocument(parsed.URI, text, Settings{DefaultLanguage: "VBScript"})
	if !parsedDocumentsEqual(parsed, fresh) {
		t.Fatalf("character-wise style attribute parse did not match fresh parse")
	}
	if css := BuildVirtualDocument(parsed, LanguageCSS).Text; !strings.Contains(css, "*{di}") {
		t.Fatalf("CSS virtual document after character-wise edit = %q", css)
	}
}

func TestParserUpdateParityRescansTypedASPRegions(t *testing.T) {
	source := `<div></div>`
	insertAt := strings.Index(source, "</div>")
	next, parsed := parseAfterReplacement(t, "file:///site/typed-asp-region.asp", source, insertAt, insertAt, "<% Response.Wri %>")
	fresh := ParseDocument("file:///site/typed-asp-region.asp", next, Settings{DefaultLanguage: "VBScript"})

	if !parsedDocumentsEqual(parsed, fresh) {
		t.Fatalf("updated ASP region parse did not match fresh parse")
	}
	region := RegionAt(parsed, strings.Index(next, "Response.Wri"))
	if region == nil || region.Language != LanguageVBScript {
		t.Fatalf("typed ASP region after edit = %#v", region)
	}
}

func TestParserUpdateParityFullyReparsesUnsafeBoundaryChanges(t *testing.T) {
	source := `<%@ LANGUAGE="VBScript" %>
<script>const value = 1;</script>
<!-- #include file="common.inc" -->
<% Response.Write "ok" %>`
	cases := []struct {
		needle      string
		replacement string
	}{
		{"VBScript", "JScript"},
		{"value = 1", "value = '</script>'"},
		{"common.inc", "other.inc"},
		{`"ok"`, strings.Repeat("a", 1025)},
	}
	for _, testCase := range cases {
		next, parsed := parseAfterNeedleReplacement(t, "file:///site/default.asp", source, testCase.needle, testCase.replacement)
		fresh := ParseDocument("file:///site/default.asp", next, Settings{DefaultLanguage: "VBScript"})
		if !parsedDocumentsEqual(parsed, fresh) {
			t.Fatalf("updated parse for %q did not match fresh parse", testCase.needle)
		}
	}
}

func TestParserUpdateParityRescansTopLevelHTMLInsertions(t *testing.T) {
	source := `<main>
<p>content</p>
</main>`
	insertOffset := strings.Index(source, "</main>")
	inserted := `<!-- #include file="common.inc" -->
<% Response.Write "created" %>
`
	next, parsed := parseAfterReplacement(t, "file:///site/full-incremental-include.asp", source, insertOffset, insertOffset, inserted)
	fresh := ParseDocument("file:///site/full-incremental-include.asp", next, Settings{DefaultLanguage: "VBScript"})

	if !parsedDocumentsEqual(parsed, fresh) {
		t.Fatalf("updated top-level HTML parse did not match fresh parse")
	}
	if len(parsed.Includes) != 1 || parsed.Includes[0].Path != "common.inc" {
		t.Fatalf("includes after top-level insertion = %#v", parsed.Includes)
	}
	region := RegionAt(parsed, strings.Index(next, "Response.Write"))
	if region == nil || region.Language != LanguageVBScript {
		t.Fatalf("created ASP region = %#v", region)
	}
}

func TestParserUpdateParityKeepsRegionsAlignedThroughOperatorEdits(t *testing.T) {
	text := `<%@ LANGUAGE="VBScript" %>
<section>(dashboard)</section>
<%
Option Explicit
Dim count
Dim total
count = (1)
total = count
If (count <> 0) And (count < 10) Then
  Response.Write "(" & CStr(total)
End If
%>
<div data-count="<%= count %>">(<span>ok</span>)</div>`
	edits := []struct {
		label        string
		needle       string
		needleOffset int
		deleteLength int
		replacement  string
	}{
		{"replace numeric literal inside parentheses", "count = (1)", len("count = ("), 1, "2"},
		{"insert whitespace inside a call", "CStr(total)", len("CStr("), 0, " "},
		{"delete whitespace before not-equals", "count <> 0", len("count"), 1, ""},
		{"replace comparison literal", "count < 10", len("count < "), 1, "2"},
		{"insert text in HTML between VBScript regions", "ok</span>", len("ok"), 0, "!"},
		{"delete a parenthesis character inside a string", `"("`, 1, 1, ""},
	}
	for _, edit := range edits {
		start := strings.Index(text, edit.needle)
		if start < 0 {
			t.Fatalf("%s needle %q not found", edit.label, edit.needle)
		}
		start += edit.needleOffset
		next, parsed := parseAfterReplacement(t, "file:///site/full-incremental-operators.asp", text, start, start+edit.deleteLength, edit.replacement)
		fresh := ParseDocument("file:///site/full-incremental-operators.asp", next, Settings{DefaultLanguage: "VBScript"})
		if !parsedDocumentsEqual(parsed, fresh) {
			t.Fatalf("%s updated parse did not match fresh parse", edit.label)
		}
		text = next
	}
}

func TestParserUpdateParityReparsesDirectiveDamageThatCanChangeDefaults(t *testing.T) {
	source := `<%@ LANGUAGE="VBScript" %>
<% Response.Write "ok" %>`
	const uri = "file:///site/full-incremental-directive.asp"
	start := strings.Index(source, "VBScript")
	next, updated := updateParsedDocumentAfterReplacement(t, uri, source, start, start+len("VBScript"), "JScript")
	fresh := ParseDocument(uri, next, Settings{DefaultLanguage: "VBScript"})

	if updated.Incremental {
		t.Fatalf("directive language edit was reused incrementally: %#v", updated)
	}
	if !parsedDocumentsEqual(updated.Parsed, fresh) {
		t.Fatalf("directive edit parse did not match fresh parse")
	}
	parsed := updated.Parsed
	if parsed.DefaultLanguage != LanguageJScript {
		t.Fatalf("default language after directive edit = %q", parsed.DefaultLanguage)
	}
	if block := firstRegionOfKind(parsed, RegionASPBlock); block == nil || block.Language != LanguageJScript {
		t.Fatalf("ASP block after directive edit = %#v", block)
	}
}

func TestUpdateParsedDocumentFallsBackForInsertionsAtRegionContentStart(t *testing.T) {
	tests := []struct {
		name        string
		source      string
		kind        RegionKind
		replacement string
	}{
		{
			name:        "ASP block",
			source:      `<% Response.Write "ok" %>`,
			kind:        RegionASPBlock,
			replacement: "' header\n",
		},
		{
			name:        "client script",
			source:      `<script>const value = 1;</script>`,
			kind:        RegionClientScript,
			replacement: "/* header */",
		},
		{
			name:        "style",
			source:      `<style>.value { color: red; }</style>`,
			kind:        RegionStyle,
			replacement: "/* header */",
		},
		{
			name:        "style attribute",
			source:      `<div style="color: red"></div>`,
			kind:        RegionStyleAttribute,
			replacement: "display: block; ",
		},
	}
	for _, testCase := range tests {
		t.Run(testCase.name, func(t *testing.T) {
			const uri = "file:///site/content-start.asp"
			previous := ParseDocument(uri, testCase.source, Settings{DefaultLanguage: "VBScript"})
			region := firstRegionOfKind(previous, testCase.kind)
			if region == nil {
				t.Fatalf("%s region missing: %#v", testCase.kind, previous.Regions)
			}
			next, updated := updateParsedDocumentAfterReplacement(
				t,
				uri,
				testCase.source,
				region.ContentStart,
				region.ContentStart,
				testCase.replacement,
			)
			fresh := ParseDocument(uri, next, Settings{DefaultLanguage: "VBScript"})
			if updated.Incremental {
				t.Fatalf("content-start insertion was reused incrementally: %#v", updated)
			}
			if !parsedDocumentsEqual(updated.Parsed, fresh) {
				t.Fatalf("content-start insertion differs from fresh parse: updated=%#v fresh=%#v", updated.Parsed.Regions, fresh.Regions)
			}
			nextRegion := firstRegionOfKind(updated.Parsed, testCase.kind)
			if nextRegion == nil || !strings.HasPrefix(next[nextRegion.ContentStart:nextRegion.ContentEnd], testCase.replacement) {
				t.Fatalf("inserted content fell outside %s region: %#v", testCase.kind, nextRegion)
			}
		})
	}
}

func TestUpdateParsedDocumentFallsBackForEOFInsertionsAfterEmbeddedRegions(t *testing.T) {
	tests := []struct {
		name   string
		source string
	}{
		{name: "ASP block", source: `<% Response.Write "ok" %>`},
		{name: "client script", source: `<script>const value = 1;</script>`},
		{name: "style", source: `<style>.value { color: red; }</style>`},
		{name: "ASP block after client script", source: `<script>const value = 1;</script><% Response.Write "ok" %>`},
	}
	for _, testCase := range tests {
		t.Run(testCase.name, func(t *testing.T) {
			const uri = "file:///site/eof-embedded.asp"
			previous := ParseDocument(uri, testCase.source, Settings{DefaultLanguage: "VBScript"})
			document := NewTextDocument(uri, "classic-asp", 1, testCase.source)
			position := document.PositionAt(len(testCase.source))
			rangeAtEOF := lsp.Range{Start: position, End: position}
			updated := UpdateParsedDocument(previous, []IncrementalChange{{Range: &rangeAtEOF, Text: "\n<!-- trailing host text -->"}}, Settings{DefaultLanguage: "VBScript"})
			if updated.Incremental {
				t.Fatalf("EOF insertion after embedded region was reused incrementally: %#v", updated)
			}
			fresh := ParseDocument(uri, updated.Parsed.Text, Settings{DefaultLanguage: "VBScript"})
			if !parsedDocumentsEqual(updated.Parsed, fresh) {
				t.Fatalf("EOF insertion differs from fresh parse: updated=%#v fresh=%#v", updated.Parsed.Regions, fresh.Regions)
			}
		})
	}
}

func TestUpdateParsedDocumentFallsBackForASPSplitDelimiterEdits(t *testing.T) {
	tests := []struct {
		name        string
		source      string
		start       int
		end         int
		replacement string
	}{
		{
			name:        "create opening delimiter beside existing block",
			source:      `<% keep = 1 %><span < value`,
			replacement: "%",
		},
		{
			name:        "create closing delimiter inside block",
			source:      `<% value = 100% + 1 %>tail`,
			replacement: ">",
		},
		{
			name:   "break opening delimiter",
			source: `<% value = 1 %>tail`,
		},
		{
			name:   "break closing delimiter",
			source: `<% value = 1 %>tail`,
		},
	}
	tests[0].start = strings.LastIndex(tests[0].source, "<") + 1
	tests[0].end = tests[0].start
	tests[1].start = strings.Index(tests[1].source, "100%") + len("100%")
	tests[1].end = tests[1].start
	tests[2].start = strings.Index(tests[2].source, "<%") + 1
	tests[2].end = tests[2].start + 1
	tests[3].start = strings.LastIndex(tests[3].source, "%>")
	tests[3].end = tests[3].start + 1

	for _, testCase := range tests {
		t.Run(testCase.name, func(t *testing.T) {
			const uri = "file:///site/split-delimiter.asp"
			next, updated := updateParsedDocumentAfterReplacement(
				t,
				uri,
				testCase.source,
				testCase.start,
				testCase.end,
				testCase.replacement,
			)
			fresh := ParseDocument(uri, next, Settings{DefaultLanguage: "VBScript"})
			if updated.Incremental {
				t.Fatalf("split delimiter edit was reused incrementally: %#v", updated)
			}
			if !parsedDocumentsEqual(updated.Parsed, fresh) {
				t.Fatalf("split delimiter edit differs from fresh parse: updated=%#v fresh=%#v", updated.Parsed.Regions, fresh.Regions)
			}
		})
	}
}

func TestParserUpdateParityReparsesEmbeddedRegionBoundaryEdits(t *testing.T) {
	source := `<script>const value = 1;</script>`
	parsed := ParseDocument("file:///site/default.asp", source, Settings{DefaultLanguage: "VBScript"})
	script := firstRegionOfKind(parsed, RegionClientScript)
	if script == nil {
		t.Fatalf("script region missing: %#v", parsed.Regions)
	}
	next, updated := parseAfterReplacement(t, "file:///site/default.asp", source, script.ContentStart, script.ContentStart, "/* header */")
	fresh := ParseDocument("file:///site/default.asp", next, Settings{DefaultLanguage: "VBScript"})

	if !parsedDocumentsEqual(updated, fresh) {
		t.Fatalf("region boundary edit parse did not match fresh parse")
	}
	nextScript := firstRegionOfKind(updated, RegionClientScript)
	if nextScript == nil || !strings.HasPrefix(next[nextScript.ContentStart:nextScript.ContentEnd], "/* header */") {
		t.Fatalf("script region after boundary edit = %#v", nextScript)
	}
}

func TestParserUpdateParityReparsesCharacterWiseEmbeddedBoundaryChanges(t *testing.T) {
	tests := []struct {
		name        string
		source      string
		needle      string
		replacement string
	}{
		{name: "complete script opening tag", source: `<script`, needle: "", replacement: ">"},
		{name: "complete style opening tag", source: `<style`, needle: "", replacement: ">"},
		{name: "complete long script opening tag", source: `<script data-long="` + strings.Repeat("x", 256) + `"`, needle: "", replacement: ">"},
		{name: "break script opening tag", source: `<script>const value = 1;</script>`, needle: "t", replacement: ""},
		{name: "break style opening tag", source: `<style>.value { color: red; }</style>`, needle: "t", replacement: ""},
	}
	for _, testCase := range tests {
		t.Run(testCase.name, func(t *testing.T) {
			start := len(testCase.source)
			if testCase.needle != "" {
				start = strings.Index(testCase.source, testCase.needle)
				if start < 0 {
					t.Fatalf("needle %q not found", testCase.needle)
				}
			}
			previous := ParseDocument("file:///site/embedded-boundary.asp", testCase.source, Settings{DefaultLanguage: "VBScript"})
			document := NewTextDocument(previous.URI, "classic-asp", 1, previous.Text)
			end := start + len(testCase.needle)
			changeRange := lsp.Range{Start: document.PositionAt(start), End: document.PositionAt(end)}
			result := UpdateParsedDocument(previous, []IncrementalChange{{
				Range:        &changeRange,
				Text:         testCase.replacement,
				ByteStart:    start,
				ByteEnd:      end,
				HasByteRange: true,
			}}, Settings{DefaultLanguage: "VBScript"})
			next := testCase.source[:start] + testCase.replacement + testCase.source[end:]
			fresh := ParseDocument(previous.URI, next, Settings{DefaultLanguage: "VBScript"})
			if result.Incremental {
				t.Fatalf("embedded boundary edit was reused incrementally: %#v", result)
			}
			if !parsedDocumentsEqual(result.Parsed, fresh) {
				t.Fatalf("embedded boundary edit parse did not match fresh parse: updated=%#v fresh=%#v", result.Parsed.Regions, fresh.Regions)
			}
		})
	}
}

func TestParserUpdateParityShiftsUTF16RangesAfterChanges(t *testing.T) {
	source := "😀\nResponse.Write value"
	doc := NewTextDocument("file:///site/range.asp", "classic-asp", 1, source)
	start := strings.Index(source, "Response")
	original := doc.Range(start, start+len("Response"))
	changeRange := lsp.Range{Start: doc.PositionAt(0), End: doc.PositionAt(0)}
	doc.ApplyChange(&changeRange, "prefix\n", 2)
	shifted := doc.Range(strings.Index(doc.Text, "Response"), strings.Index(doc.Text, "Response")+len("Response"))

	if original.Start != (lsp.Position{Line: 1, Character: 0}) {
		t.Fatalf("original UTF-16 range = %#v", original)
	}
	if shifted.Start != (lsp.Position{Line: 2, Character: 0}) || shifted.End != (lsp.Position{Line: 2, Character: len("Response")}) {
		t.Fatalf("shifted UTF-16 range = %#v", shifted)
	}
}

func parseAfterNeedleReplacement(t *testing.T, uri string, source string, needle string, replacement string) (string, *ParsedDocument) {
	t.Helper()
	start := strings.Index(source, needle)
	if start < 0 {
		t.Fatalf("needle %q not found", needle)
	}
	return parseAfterReplacement(t, uri, source, start, start+len(needle), replacement)
}

func parseAfterReplacement(t *testing.T, uri string, source string, start int, end int, replacement string) (string, *ParsedDocument) {
	t.Helper()
	doc := NewTextDocument(uri, "classic-asp", 1, source)
	changeRange := lsp.Range{
		Start: doc.PositionAt(start),
		End:   doc.PositionAt(end),
	}
	doc.ApplyChange(&changeRange, replacement, 2)
	next := source[:start] + replacement + source[end:]
	if doc.Text != next {
		t.Fatalf("document text after change = %q, want %q", doc.Text, next)
	}
	return next, ParseDocument(doc.URI, doc.Text, Settings{DefaultLanguage: "VBScript"})
}

func updateParsedDocumentAfterReplacement(t *testing.T, uri string, source string, start int, end int, replacement string) (string, IncrementalUpdateResult) {
	t.Helper()
	previous := ParseDocument(uri, source, Settings{DefaultLanguage: "VBScript"})
	document := NewTextDocument(uri, "classic-asp", 1, source)
	changeRange := document.Range(start, end)
	next := source[:start] + replacement + source[end:]
	result := UpdateParsedDocument(previous, []IncrementalChange{{
		Range:        &changeRange,
		Text:         replacement,
		ByteStart:    start,
		ByteEnd:      end,
		HasByteRange: true,
	}}, Settings{DefaultLanguage: "VBScript"})
	if result.Parsed.Text != next {
		t.Fatalf("updated text = %q, want %q", result.Parsed.Text, next)
	}
	return next, result
}
