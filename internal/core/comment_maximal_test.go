package core

import (
	"strings"
	"testing"

	"github.com/yottonoko/classic-web-system-lsp/internal/lsp"
)

func TestMaximalCommentToggleHTML(t *testing.T) {
	marked := `<section>
⟦  <p>ready</p>
  <p>done</p>⟧
</section>`
	source, selection, firstLine, lastLine := markedCommentSelection(t, marked)
	want := `<section>
<!--   <p>ready</p>
  <p>done</p> -->
</section>`
	assertMaximalCommentRoundTrip(t, source, want, selection, firstLine, lastLine, Settings{})
}

func TestMaximalCommentToggleCSS(t *testing.T) {
	marked := `<style>
⟦  .card {
    color: red;
  }⟧
</style>`
	source, selection, firstLine, lastLine := markedCommentSelection(t, marked)
	want := `<style>
  /* .card {
    color: red;
  } */
</style>`
	assertMaximalCommentRoundTrip(t, source, want, selection, firstLine, lastLine, Settings{})
}

func TestMaximalCommentToggleClientJavaScript(t *testing.T) {
	marked := `<script>
⟦  const first = 1;
  const second = 2;⟧
</script>`
	source, selection, firstLine, lastLine := markedCommentSelection(t, marked)
	want := `<script>
  // const first = 1;
  // const second = 2;
</script>`
	assertMaximalCommentRoundTrip(t, source, want, selection, firstLine, lastLine, Settings{})
}

func TestMaximalCommentToggleServerVBScript(t *testing.T) {
	marked := `<%
⟦  Dim first
  first = 1⟧
%>`
	source, selection, firstLine, lastLine := markedCommentSelection(t, marked)
	want := `<%
  ' Dim first
  ' first = 1
%>`
	assertMaximalCommentRoundTrip(t, source, want, selection, firstLine, lastLine, Settings{})
}

func TestMaximalCommentToggleAllLanguagesWholeDocument(t *testing.T) {
	source := `<main>ready</main>
<style>
  .card { color: red; }
</style>
<script>
  const ready = true;
</script>
<%
  Response.Write ready
%>
<footer>done</footer>`
	want := `<!-- <main>ready</main>
<style>
  .card { color: red; }
</style>
<script>
  const ready = true;
</script>
<%
  ' Response.Write ready
%>
<footer>done</footer> -->`
	assertMaximalCommentRoundTrip(t, source, want, wholeCommentRange(source), 0, 10, Settings{})
}

func TestMaximalCommentToggleAllLanguagesInline(t *testing.T) {
	source := `<p><%= title %></p><style>.card{color:red}</style><script>const ready=true;</script><% Response.Write title %>`
	want := `<!-- <p><%'= title %></p><style>.card{color:red}</style><script>const ready=true;</script><%' Response.Write title %> -->`
	assertMaximalCommentRoundTrip(t, source, want, wholeCommentRange(source), 0, 0, Settings{})
}

func TestMaximalCommentToggleAllLanguagesAddsLayerToMixedComments(t *testing.T) {
	source := `<!-- <h1>ready</h1> -->
<style>.card{color:red}</style>
<script>
  const ready = true;
</script>
<%
  ' already hidden
  Response.Write ready
%>
<footer>done</footer>`
	transformed := `<!-- <h1>ready</h1> -->
<style>.card{color:red}</style>
<script>
  const ready = true;
</script>
<%
  '' already hidden
  ' Response.Write ready
%>
<footer>done</footer>`
	want := "<!-- " + transformed + " -->"
	assertMaximalCommentRoundTrip(t, source, want, wholeCommentRange(source), 0, 9, Settings{})
}

func TestMaximalCommentToggleAllLanguagesUsesVSCodeRawClientTerminators(t *testing.T) {
	source := `<p>arrow --> text</p>
<style>.x::after { content: "*/"; }</style>
<script>const marker = "-->";</script>
<% Response.Write "*/ -->" %>`
	transformed := `<p>arrow --> text</p>
<style>.x::after { content: "*/"; }</style>
<script>const marker = "-->";</script>
<%' Response.Write "*/ -->" %>`
	want := "<!-- " + transformed + " -->"
	assertMaximalCommentRoundTrip(t, source, want, wholeCommentRange(source), 0, 3, Settings{})
	if !strings.Contains(want, `<%' Response.Write "*/ -->" %>`) {
		t.Fatalf("ASP string was unexpectedly escaped: %s", want)
	}
}

func TestMaximalCommentToggleRoundTripsOuterHTMLBoundaryAroundExistingComments(t *testing.T) {
	tests := []struct {
		name      string
		source    string
		firstWant string
	}{
		{
			name:      "HTML",
			source:    `<p>one</p> <!-- old --> <% value %> <!-- tail -->`,
			firstWant: `<!-- <p>one</p> <!-- old --> <%' value %> <!-- tail --> -->`,
		},
		{
			name:      "HTML raw terminator",
			source:    `<p>a --> b</p> <% value %> <!-- tail -->`,
			firstWant: `<!-- <p>a --> b</p> <%' value %> <!-- tail --> -->`,
		},
		{
			name:   "HTML and script",
			source: `<p>one</p> <!-- old --> <script>const value = true;</script> <% value %> <!-- tail -->`,
		},
		{
			name:   "HTML and style",
			source: `<p>one</p> <!-- old --> <style>.x { color: red; }</style> <% value %> <!-- tail -->`,
		},
	}
	for _, test := range tests {
		t.Run(test.name, func(t *testing.T) {
			first := applyCoreTextEdits(t, test.source, ClassicASPLineCommentPlan(
				"file:///site/default.asp", test.source, []lsp.Range{wholeCommentRange(test.source)}, Settings{},
			).Edits)
			if test.firstWant != "" {
				if first != test.firstWant {
					t.Fatalf("outer HTML boundary first toggle = %q, want %q", first, test.firstWant)
				}
			} else if !strings.HasPrefix(first, "<!-- ") || !strings.HasSuffix(first, " -->") {
				t.Fatalf("embedded outer HTML boundary first toggle = %q", first)
			}
			second := applyCoreTextEdits(t, first, ClassicASPLineCommentPlan(
				"file:///site/default.asp", first, []lsp.Range{wholeCommentRange(first)}, Settings{},
			).Edits)
			if second != test.source {
				t.Fatalf("outer HTML boundary second toggle = %q, want %q", second, test.source)
			}
		})
	}
}

func TestMaximalCommentToggleAllLanguagesPreservesCRLFUTF16AndBlankLines(t *testing.T) {
	source := "<p>🌸</p>\r\n\r\n<style>色{color:red}</style>\r\n<script>const 花=\"🌸\";</script>\r\n<% Response.Write \"🌸\" %>\r\n"
	want := "<!-- <p>🌸</p>\r\n\r\n<style>色{color:red}</style>\r\n<script>const 花=\"🌸\";</script>\r\n<%' Response.Write \"🌸\" %> -->\r\n"
	assertMaximalCommentRoundTrip(t, source, want, wholeCommentRange(source), 0, 4, Settings{})
}

func TestMaximalCommentToggleIgnoresEmptyAndASPBoundaryOnlyLines(t *testing.T) {
	for _, source := range []string{"", "\n", "%> <%", "<%> <%"} {
		t.Run(source, func(t *testing.T) {
			plan := ClassicASPLineCommentPlan(
				"file:///site/default.asp",
				source,
				[]lsp.Range{wholeCommentRange(source)},
				Settings{},
			)
			if len(plan.Edits) != 0 {
				t.Fatalf("boundary-only source produced edits: %#v", plan.Edits)
			}
		})
	}
}

func TestMaximalCommentToggleLeavesStandaloneASPBoundaryLineUntouched(t *testing.T) {
	source := "<%\n  value = 1\n%> <%\n  value = 2\n%>"
	plan := ClassicASPLineCommentPlan(
		"file:///site/default.asp",
		source,
		[]lsp.Range{selectedLineCommentRange(source, 2, 2)},
		Settings{},
	)
	if len(plan.Edits) != 0 {
		t.Fatalf("standalone ASP boundary line produced edits: %#v", plan.Edits)
	}
}

func TestMaximalCommentToggleMatchesVSCodeBoundaryRemovalForExistingHostComments(t *testing.T) {
	source := "<!-- existing -->\n<!-- another -->"
	want := "existing -->\n<!-- another"
	assertMaximalCommentRoundTrip(t, source, want, wholeCommentRange(source), 0, 1, Settings{})
}

func TestMaximalCommentToggleRejectsPartialBlockCommentBoundaries(t *testing.T) {
	tests := []struct {
		name      string
		source    string
		firstLine int
		lastLine  int
	}{
		{
			name:      "HTML opener and body",
			source:    "<!--\nfoo\n-->",
			firstLine: 0,
			lastLine:  1,
		},
		{
			name:      "HTML body and closer",
			source:    "<!--\nfoo\n-->",
			firstLine: 1,
			lastLine:  2,
		},
		{
			name:      "CSS opener and body",
			source:    "<style>\n/*\nfoo\n*/\n</style>",
			firstLine: 1,
			lastLine:  2,
		},
		{
			name:      "CSS body and closer",
			source:    "<style>\n/*\nfoo\n*/\n</style>",
			firstLine: 2,
			lastLine:  3,
		},
	}
	for _, test := range tests {
		t.Run(test.name, func(t *testing.T) {
			plan := ClassicASPLineCommentPlan(
				"file:///site/default.asp",
				test.source,
				[]lsp.Range{selectedLineCommentRange(test.source, test.firstLine, test.lastLine)},
				Settings{},
			)
			if len(plan.Edits) != 0 || plan.NoOpReason != "unsafe-structure" {
				t.Fatalf("partial block boundary plan = %#v", plan)
			}
		})
	}
}

func TestMaximalCommentToggleTreatsMultilineJavaScriptTemplateMarkersAsText(t *testing.T) {
	source := `<script>
const template = ` + "`" + `
<!--
selected
-->
` + "`" + `;
</script>`
	selection := rangeForNeedle(t, source, "selected")
	commented := applyCoreTextEdits(t, source, ClassicASPLineCommentPlan(
		"file:///site/default.asp", source, []lsp.Range{selection}, Settings{},
	).Edits)
	want := `<script>
const template = ` + "`" + `
<!--
// selected
-->
` + "`" + `;
</script>`
	if commented != want {
		t.Fatalf("template literal toggle = %q, want %q", commented, want)
	}
	selection = rangeForNeedle(t, commented, "// selected")
	uncommented := applyCoreTextEdits(t, commented, ClassicASPLineCommentPlan(
		"file:///site/default.asp", commented, []lsp.Range{selection}, Settings{},
	).Edits)
	if uncommented != source {
		t.Fatalf("template literal round trip = %q, want %q", uncommented, source)
	}
}

func TestMaximalCommentToggleRecognizesCompactPlainBlockComments(t *testing.T) {
	tests := []struct {
		name   string
		source string
		want   string
	}{
		{name: "HTML compact", source: "<!--foo-->", want: "foo"},
		{name: "HTML empty", source: "<!-- -->", want: ""},
		{name: "HTML multiline", source: "<!--\nfoo\n-->", want: "\nfoo\n"},
		{name: "CSS compact", source: "<style>\n/*foo*/\n</style>", want: "<style>\nfoo\n</style>"},
		{name: "CSS empty", source: "<style>\n/* */\n</style>", want: "<style>\n\n</style>"},
		{name: "CSS multiline", source: "<style>\n/*\nfoo\n*/\n</style>", want: "<style>\n\nfoo\n\n</style>"},
	}
	for _, test := range tests {
		t.Run(test.name, func(t *testing.T) {
			selection := wholeCommentRange(test.source)
			if strings.Contains(test.source, "/*") {
				lastLine := 1
				if strings.Contains(test.source, "\nfoo\n") {
					lastLine = 3
				}
				selection = selectedLineCommentRange(test.source, 1, lastLine)
			}
			uncommented := applyCoreTextEdits(t, test.source, ClassicASPLineCommentPlan(
				"file:///site/default.asp", test.source, []lsp.Range{selection}, Settings{},
			).Edits)
			if uncommented != test.want {
				t.Fatalf("plain block toggle = %q, want %q", uncommented, test.want)
			}
		})
	}
}

func TestMaximalCommentTogglePreservesIndentedHTMLCommentPrefix(t *testing.T) {
	source := "  <!-- ready -->"
	uncommented := applyCoreTextEdits(t, source, ClassicASPLineCommentPlan(
		"file:///site/default.asp", source, []lsp.Range{wholeCommentRange(source)}, Settings{},
	).Edits)
	if uncommented != "  ready" {
		t.Fatalf("indented HTML comment = %q, want %q", uncommented, "  ready")
	}
}

func TestMaximalCommentTogglePartialHTMLToInlineVBScriptUsesTouchedLines(t *testing.T) {
	marked := `<main>re⟦ady</main>
<style>.card{color:red}</style>
<script>const ready=true;</script>
<% Response.Write rea⟧dy %>
<footer>outside</footer>`
	source, selection, firstLine, lastLine := markedCommentSelection(t, marked)
	want := `<!-- <main>ready</main>
<style>.card{color:red}</style>
<script>const ready=true;</script>
<%' Response.Write ready %> -->
<footer>outside</footer>`
	assertMaximalCommentRoundTrip(t, source, want, selection, firstLine, lastLine, Settings{})
}

func TestMaximalCommentTogglePartialCSSToVBScriptSplitsSafeRegions(t *testing.T) {
	marked := `<style>
  .card { col⟦or: red; }
</style>
<p>ready</p>
<script>const ready=true;</script>
<% Response.Write rea⟧dy %>
<footer>outside</footer>`
	source, selection, firstLine, lastLine := markedCommentSelection(t, marked)
	want := `<style>
  /* .card { color: red; } */
</style>
<!-- <p>ready</p>
<script>const ready=true;</script>
<%' Response.Write ready %> -->
<footer>outside</footer>`
	assertMaximalCommentRoundTrip(t, source, want, selection, firstLine, lastLine, Settings{})
}

func TestMaximalCommentTogglePartialHTMLToCSSSplitsSafeRegions(t *testing.T) {
	marked := `<p>re⟦ady</p>
<script>const ready=true;</script>
<% Response.Write ready %>
<style>
  .card { color: re⟧d; }
</style>
<footer>outside</footer>`
	source, selection, firstLine, lastLine := markedCommentSelection(t, marked)
	want := `<!-- <p>ready</p>
<script>const ready=true;</script>
<%' Response.Write ready %> -->
<style>
  /* .card { color: red; } */
</style>
<footer>outside</footer>`
	assertMaximalCommentRoundTrip(t, source, want, selection, firstLine, lastLine, Settings{})
}

func TestMaximalCommentTogglePartialJavaScriptToCSSSplitsSafeRegions(t *testing.T) {
	marked := `<script>
  const rea⟦dy = true;
</script>
<p>ready</p>
<% Response.Write ready %>
<style>
  .card { color: re⟧d; }
</style>
<footer>outside</footer>`
	source, selection, firstLine, lastLine := markedCommentSelection(t, marked)
	want := `<script>
  // const ready = true;
</script>
<!-- <p>ready</p>
<%' Response.Write ready %> -->
<style>
  /* .card { color: red; } */
</style>
<footer>outside</footer>`
	assertMaximalCommentRoundTrip(t, source, want, selection, firstLine, lastLine, Settings{})
}

func TestMaximalCommentTogglePartialVBScriptToJavaScriptSplitsSafeRegions(t *testing.T) {
	marked := `<%
  Response.Wr⟦ite ready
  Response.Write second
%>
<p>ready</p>
<style>.card{color:red}</style>
<script>
  const ready = tr⟧ue;
</script>
<footer>outside</footer>`
	source, selection, firstLine, lastLine := markedCommentSelection(t, marked)
	want := `<%
  ' Response.Write ready
  ' Response.Write second
%>
<!-- <p>ready</p>
<style>.card{color:red}</style> -->
<script>
  // const ready = true;
</script>
<footer>outside</footer>`
	assertMaximalCommentRoundTrip(t, source, want, selection, firstLine, lastLine, Settings{})
}

func TestMaximalCommentToggleIncludeDirectiveUsesDedicatedMarker(t *testing.T) {
	source := `<!-- #include file="shared/header.inc" -->`
	want := `<!-- asp-lsp-disabled-include:#include file="shared/header.inc" -->`
	assertMaximalCommentRoundTrip(t, source, want, wholeCommentRange(source), 0, 0, Settings{})
}

func TestMaximalCommentToggleIncludeDirectiveSplitsHostComments(t *testing.T) {
	source := `<header>ready</header>
<!-- #include file="shared/header.inc" -->
<% Response.Write ready %>
<footer>done</footer>`
	want := `<!-- <header>ready</header> -->
<!-- asp-lsp-disabled-include:#include file="shared/header.inc" -->
<!-- <%' Response.Write ready %>
<footer>done</footer> -->`
	assertMaximalCommentRoundTrip(t, source, want, wholeCommentRange(source), 0, 3, Settings{})
}

func TestMaximalCommentToggleAddsOneLayerToDisabledIncludeInMixedSelection(t *testing.T) {
	source := `<!-- asp-lsp-disabled-include:#include file="shared/header.inc" -->
<p>ready</p>`
	want := `<!-- asp-lsp-disabled-include:asp-lsp-disabled-include:#include file="shared/header.inc" -->
<!-- <p>ready</p> -->`
	assertMaximalCommentRoundTrip(t, source, want, wholeCommentRange(source), 0, 1, Settings{})
}

func TestMaximalCommentToggleOrdinaryHTMLCommentIsNotInclude(t *testing.T) {
	source := `<!-- explanation -->`
	want := `explanation`
	assertMaximalCommentRoundTrip(t, source, want, wholeCommentRange(source), 0, 0, Settings{})
}

func TestMaximalCommentToggleIncludeDirectiveVariants(t *testing.T) {
	tests := []struct {
		name   string
		source string
		want   string
	}{
		{
			name:   "compact virtual single quote",
			source: `<!--#include virtual='/shared/header.inc'-->`,
			want:   `<!--asp-lsp-disabled-include:#include virtual='/shared/header.inc'-->`,
		},
		{
			name:   "uppercase file with spaces",
			source: `<!--  #INCLUDE FILE = "shared/header.inc"  -->`,
			want:   `<!--  asp-lsp-disabled-include:#INCLUDE FILE = "shared/header.inc"  -->`,
		},
	}
	for _, test := range tests {
		t.Run(test.name, func(t *testing.T) {
			assertMaximalCommentRoundTrip(t, test.source, test.want, wholeCommentRange(test.source), 0, 0, Settings{})
		})
	}
}

func TestMaximalCommentToggleDisabledIncludeLeavesAndRestoresParserEdge(t *testing.T) {
	source := `<!-- #include virtual="/shared/header.inc" -->`
	disabled := `<!-- asp-lsp-disabled-include:#include virtual="/shared/header.inc" -->`
	activeParsed := ParseDocument("file:///site/default.asp", source, Settings{})
	if len(activeParsed.Includes) != 1 {
		t.Fatalf("active includes = %#v", activeParsed.Includes)
	}
	disabledParsed := ParseDocument("file:///site/default.asp", disabled, Settings{})
	if len(disabledParsed.Includes) != 0 {
		t.Fatalf("disabled includes = %#v", disabledParsed.Includes)
	}
	restored := applyCoreTextEdits(t, disabled, ClassicASPLineCommentPlan(
		"file:///site/default.asp",
		disabled,
		[]lsp.Range{wholeCommentRange(disabled)},
		Settings{},
	).Edits)
	if restored != source {
		t.Fatalf("restored include = %q, want %q", restored, source)
	}
	restoredParsed := ParseDocument("file:///site/default.asp", restored, Settings{})
	if len(restoredParsed.Includes) != 1 || restoredParsed.Includes[0] != activeParsed.Includes[0] {
		t.Fatalf("restored includes = %#v, want %#v", restoredParsed.Includes, activeParsed.Includes)
	}
}

func TestMaximalCommentToggleServerJScript(t *testing.T) {
	marked := `<%
⟦  Response.Write("ready");⟧
%>`
	source, selection, firstLine, lastLine := markedCommentSelection(t, marked)
	want := `<%
  // Response.Write("ready");
%>`
	assertMaximalCommentRoundTrip(t, source, want, selection, firstLine, lastLine, Settings{DefaultLanguage: "JScript"})
}

func TestMaximalCommentToggleASPExpression(t *testing.T) {
	source := `<p><%= title %></p>`
	want := `<!-- <p><%'= title %></p> -->`
	assertMaximalCommentRoundTrip(t, source, want, wholeCommentRange(source), 0, 0, Settings{})
}

func TestMaximalCommentToggleRoundTripsASPTagsAndExpressionFormsWithStableSelection(t *testing.T) {
	tests := []struct {
		name     string
		source   string
		first    string
		second   string
		settings Settings
	}{
		{
			name: "VBScript tags on their own lines",
			source: `<%
  Response.Write title
%>`,
			first: `<%
  ' Response.Write title
%>`,
		},
		{
			name: "multiline ASP expression tags on their own lines",
			source: `<%=
  BuildTitle(
    item
  )
%>`,
			first: `<%'=
  ' BuildTitle(
    ' item
  ' )
%>`,
		},
		{
			name:   "compact ASP expression comment",
			source: `<%'= title %>`,
			first:  `<%= title %>`,
		},
		{
			name:   "spaced ASP expression comment",
			source: `<%' = title %>`,
			first:  `<%= title %>`,
			second: `<%'= title %>`,
		},
		{
			name:   "tab-spaced ASP expression comment",
			source: "<%'\t = title %>",
			first:  `<%= title %>`,
			second: `<%'= title %>`,
		},
		{
			name:     "JScript expression comment",
			source:   `<%// = title %>`,
			first:    `<%= title %>`,
			second:   `<%//= title %>`,
			settings: Settings{DefaultLanguage: "JScript"},
		},
	}
	for _, test := range tests {
		t.Run(test.name, func(t *testing.T) {
			selection := wholeCommentRange(test.source)
			first := applyCoreTextEdits(t, test.source, ClassicASPLineCommentPlan(
				"file:///site/default.asp", test.source, []lsp.Range{selection}, test.settings,
			).Edits)
			if first != test.first {
				t.Fatalf("first toggle mismatch:\n got: %q\nwant: %q", first, test.first)
			}
			second := applyCoreTextEdits(t, first, ClassicASPLineCommentPlan(
				"file:///site/default.asp", first, []lsp.Range{selection}, test.settings,
			).Edits)
			wantSecond := test.source
			if test.second != "" {
				wantSecond = test.second
			}
			if second != wantSecond {
				t.Fatalf("second toggle mismatch:\n got: %q\nwant: %q", second, wantSecond)
			}
			third := applyCoreTextEdits(t, second, ClassicASPLineCommentPlan(
				"file:///site/default.asp", second, []lsp.Range{selection}, test.settings,
			).Edits)
			if third != test.first {
				t.Fatalf("third toggle mismatch:\n got: %q\nwant: %q", third, test.first)
			}
		})
	}
}

func TestMaximalCommentToggleKeepsStableSelectionAcrossStandaloneASPTagsAndHostComments(t *testing.T) {
	source := `<header>before</header>
<%
  Response.Write title
%>
<footer>after</footer>`
	commented := `<!-- <header>before</header>
<%
  ' Response.Write title
%>
<footer>after</footer> -->`
	selection := wholeCommentRange(source)
	current := source
	for index := 0; index < 6; index++ {
		current = applyCoreTextEdits(t, current, ClassicASPLineCommentPlan(
			"file:///site/default.asp", current, []lsp.Range{selection}, Settings{},
		).Edits)
		want := commented
		if index%2 == 1 {
			want = source
		}
		if current != want {
			t.Fatalf("toggle %d mismatch:\n got:\n%s\nwant:\n%s", index+1, current, want)
		}
	}
}

func TestMaximalCommentToggleREMAddsAndRemovesOneLayer(t *testing.T) {
	marked := `<%
⟦  REM hidden value⟧
%>`
	source, selection, firstLine, lastLine := markedCommentSelection(t, marked)
	want := `<%
  ' REM hidden value
%>`
	assertMaximalCommentRoundTrip(t, source, want, selection, firstLine, lastLine, Settings{})
}

func TestMaximalCommentToggleMultipleSelections(t *testing.T) {
	source := `<p>first</p>
<p>middle</p>
<style>
  .last { color: red; }
</style>`
	doc := NewTextDocument("file:///site/default.asp", "classic-asp", 0, source)
	selections := []lsp.Range{
		{Start: lsp.Position{Line: 0}, End: lsp.Position{Line: 0, Character: len("<p>first</p>")}},
		{Start: lsp.Position{Line: 3}, End: lsp.Position{Line: 3, Character: len("  .last { color: red; }")}},
	}
	first := applyCoreTextEdits(t, source, ClassicASPLineCommentPlan(doc.URI, source, selections, Settings{}).Edits)
	want := `<!-- <p>first</p> -->
<p>middle</p>
<style>
  /* .last { color: red; } */
</style>`
	if first != want {
		t.Fatalf("multiple-selection toggle:\n got:\n%s\nwant:\n%s", first, want)
	}
	second := applyCoreTextEdits(t, first, ClassicASPLineCommentPlan(doc.URI, first, []lsp.Range{
		{Start: lsp.Position{Line: 0}, End: lsp.Position{Line: 0, Character: len("<!-- <p>first</p> -->")}},
		{Start: lsp.Position{Line: 3}, End: lsp.Position{Line: 3, Character: len("  /* .last { color: red; } */")}},
	}, Settings{}).Edits)
	if second != source {
		t.Fatalf("multiple-selection round trip:\n got:\n%s\nwant:\n%s", second, source)
	}
}

func markedCommentSelection(t *testing.T, marked string) (string, lsp.Range, int, int) {
	t.Helper()
	const open, close = "⟦", "⟧"
	start := strings.Index(marked, open)
	if start < 0 {
		t.Fatalf("missing %s marker", open)
	}
	withoutOpen := marked[:start] + marked[start+len(open):]
	end := strings.Index(withoutOpen, close)
	if end < 0 {
		t.Fatalf("missing %s marker", close)
	}
	source := withoutOpen[:end] + withoutOpen[end+len(close):]
	doc := NewTextDocument("file:///site/default.asp", "classic-asp", 0, source)
	selection := lsp.Range{Start: doc.PositionAt(start), End: doc.PositionAt(end)}
	return source, selection, selection.Start.Line, selection.End.Line
}

func wholeCommentRange(source string) lsp.Range {
	doc := NewTextDocument("file:///site/default.asp", "classic-asp", 0, source)
	return lsp.Range{Start: lsp.Position{}, End: doc.PositionAt(len(source))}
}

func selectedLineCommentRange(source string, firstLine, lastLine int) lsp.Range {
	doc := NewTextDocument("file:///site/default.asp", "classic-asp", 0, source)
	start := lsp.Position{Line: firstLine}
	if lastLine+1 < len(doc.lineStarts) {
		return lsp.Range{Start: start, End: lsp.Position{Line: lastLine + 1}}
	}
	return lsp.Range{Start: start, End: doc.PositionAt(len(source))}
}

func assertMaximalCommentRoundTrip(
	t *testing.T,
	source string,
	want string,
	selection lsp.Range,
	firstLine int,
	lastLine int,
	settings Settings,
) {
	t.Helper()
	uri := "file:///site/default.asp"
	first := applyCoreTextEdits(t, source, ClassicASPLineCommentPlan(uri, source, []lsp.Range{selection}, settings).Edits)
	if first != want {
		t.Fatalf("first toggle mismatch:\n got:\n%s\nwant:\n%s", first, want)
	}
	secondSelection := selectedLineCommentRange(first, firstLine, lastLine)
	second := applyCoreTextEdits(t, first, ClassicASPLineCommentPlan(uri, first, []lsp.Range{secondSelection}, settings).Edits)
	if second != source {
		t.Fatalf("second toggle mismatch:\n got:\n%s\nwant:\n%s", second, source)
	}
	thirdSelection := selectedLineCommentRange(second, firstLine, lastLine)
	third := applyCoreTextEdits(t, second, ClassicASPLineCommentPlan(uri, second, []lsp.Range{thirdSelection}, settings).Edits)
	if third != want {
		t.Fatalf("third toggle mismatch:\n got:\n%s\nwant:\n%s", third, want)
	}
}

func BenchmarkClassicASPLineCommentPlanMaximalMixedDocument(b *testing.B) {
	source := benchmarkMixedCommentDocument()
	selection := wholeCommentRange(source)
	b.ReportAllocs()
	b.SetBytes(int64(len(source)))
	b.ResetTimer()
	for range b.N {
		plan := ClassicASPLineCommentPlan(
			"file:///site/default.asp",
			source,
			[]lsp.Range{selection},
			Settings{},
		)
		if len(plan.Edits) != 1 {
			b.Fatalf("edits = %d", len(plan.Edits))
		}
	}
}

func BenchmarkClassicASPLineCommentPlanLegacyMixedDocument(b *testing.B) {
	source := benchmarkMixedCommentDocument()
	selection := wholeCommentRange(source)
	b.ReportAllocs()
	b.SetBytes(int64(len(source)))
	b.ResetTimer()
	for range b.N {
		plan := classicASPLineCommentPlan(
			"file:///site/default.asp",
			source,
			[]lsp.Range{selection},
			Settings{},
			false,
		)
		if len(plan.Edits) == 0 {
			b.Fatal("missing edits")
		}
	}
}

func benchmarkMixedCommentDocument() string {
	const block = `<section>ready</section>
<style>.card{color:red}</style>
<script>const ready=true;</script>
<% Response.Write ready %>
`
	return strings.Repeat(block, 256)
}
