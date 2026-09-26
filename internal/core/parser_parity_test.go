package core

import (
	"strings"
	"testing"
)

func TestDetectsASPBlocksDirectivesIncludesStyleAndScriptRegions(t *testing.T) {
	source := `<%@ LANGUAGE="VBScript" %>
<!-- #include file="inc/header.inc" -->
<style>.x { color: red; }</style>
<script>const value = document.title;</script>
<% Response.Write value %>`
	parsed := ParseDocument("file:///site/default.asp", source, Settings{DefaultLanguage: "VBScript"})
	if parsed.DefaultLanguage != LanguageVBScript {
		t.Fatalf("default language = %q", parsed.DefaultLanguage)
	}
	if len(parsed.Includes) != 1 || parsed.Includes[0].Path != "inc/header.inc" {
		t.Fatalf("includes = %#v", parsed.Includes)
	}
	for _, want := range []RegionKind{RegionASPDirective, RegionStyle, RegionClientScript, RegionASPBlock} {
		if firstRegionOfKind(parsed, want) == nil {
			t.Fatalf("missing %s region in %#v", want, parsed.Regions)
		}
	}
}

func TestExtractsIncludeReferencesWithoutFullASPParsing(t *testing.T) {
	source := `<!-- #include virtual="/shared/header.inc" -->
<!-- #include file="../footer.inc" -->
<% Response.Write "ok" %>`
	parsed := ParseDocument("file:///site/default.asp", source, Settings{})
	if len(parsed.Includes) != 2 {
		t.Fatalf("includes = %#v", parsed.Includes)
	}
	if parsed.Includes[0].Path != "/shared/header.inc" || parsed.Includes[1].Path != "../footer.inc" {
		t.Fatalf("include paths = %#v", parsed.Includes)
	}
	if parsed.Includes[0].Range.Start.Line != 0 || parsed.Includes[1].Range.Start.Line != 1 {
		t.Fatalf("include ranges = %#v", parsed.Includes)
	}
}

func TestExtractsIncludesOnlyFromTopLevelHTMLCommentsWithVirtualPriority(t *testing.T) {
	source := `<div data-note="<!-- #include file=\"attribute.inc\" -->"></div>
<script>
<!-- #include file="script.inc" -->
</script>
<style>
/* <!-- #include file="style.inc" --> */
</style>
<% Response.Write "<!-- #include file=\"asp.inc\" -->" %>
<!-- #include file="fallback.inc" virtual="/shared/header.inc" -->
<!-- #include virtual="/shared/footer.inc" file="ignored.inc" -->
<!-- #include file="real.inc" -->`
	parsed := ParseDocument("file:///site/includes-context.asp", source, Settings{})
	if len(parsed.Includes) != 3 {
		t.Fatalf("includes = %#v", parsed.Includes)
	}
	want := []struct {
		path string
		mode string
	}{
		{"/shared/header.inc", "virtual"},
		{"/shared/footer.inc", "virtual"},
		{"real.inc", "file"},
	}
	for index, expected := range want {
		got := parsed.Includes[index]
		if got.Path != expected.path || got.Mode != expected.mode {
			t.Fatalf("include[%d] = %#v, want path=%q mode=%q", index, got, expected.path, expected.mode)
		}
	}
}

func TestIncludeRangesFollowTheSelectedAttributeValue(t *testing.T) {
	source := "🌸<!-- #include file = \"shared.inc\" virtual = \"../shared.inc\" -->\n" +
		"<!-- #include virtual = 'shared.inc' file = 'shared.inc' -->"
	parsed := ParseDocument("file:///site/include-ranges.asp", source, Settings{})
	if len(parsed.Includes) != 2 {
		t.Fatalf("includes = %#v", parsed.Includes)
	}
	want := []struct {
		path string
		mode string
		text string
	}{
		{path: "../shared.inc", mode: "virtual", text: "../shared.inc"},
		{path: "shared.inc", mode: "virtual", text: "shared.inc"},
	}
	for index, expected := range want {
		include := parsed.Includes[index]
		if include.Path != expected.path || include.Mode != expected.mode {
			t.Fatalf("include[%d] = %#v, want path=%q mode=%q", index, include, expected.path, expected.mode)
		}
		document := NewTextDocument(parsed.URI, "classic-asp", 0, source)
		start := document.OffsetAt(include.Range.Start)
		end := document.OffsetAt(include.Range.End)
		if source[start:end] != expected.text {
			t.Fatalf("include[%d] range text = %q, want %q", index, source[start:end], expected.text)
		}
	}
}

func TestDetectsRootScriptRegionsBetweenASPProcedureBlocks(t *testing.T) {
	source := `<%
Sub Render()
End Sub
%>
<script>const clientValue = 1;</script>
<%
Render()
%>`
	parsed := ParseDocument("file:///site/default.asp", source, Settings{})
	script := firstRegionOfKind(parsed, RegionClientScript)
	if script == nil {
		t.Fatalf("missing client script region: %#v", parsed.Regions)
	}
	if got := source[script.ContentStart:script.ContentEnd]; !strings.Contains(got, "clientValue") {
		t.Fatalf("script content = %q", got)
	}
	if countRegionsOfKind(parsed, RegionASPBlock) != 2 {
		t.Fatalf("ASP block regions = %#v", parsed.Regions)
	}
}

func TestBuildsVirtualDocumentsWithSourceMaps(t *testing.T) {
	source := `<html><body>
<style>.card { color: <%= themeColor %>; }</style>
<script>const value = <%= serverValue %>;</script>
<div style="display: <%= displayValue %>;">Hello <%= name %></div>
</body></html>`
	parsed := ParseDocument("file:///site/default.asp", source, Settings{})
	html := BuildVirtualDocument(parsed, LanguageHTML)
	css := BuildVirtualDocument(parsed, LanguageCSS)
	javascript := BuildVirtualDocument(parsed, LanguageJavaScript)
	if strings.Contains(html.Text, "themeColor") || strings.Contains(html.Text, "<%= name %>") {
		t.Fatalf("HTML virtual document leaked ASP: %q", html.Text)
	}
	if !strings.Contains(css.Text, ".card") || !strings.Contains(css.Text, "*{display:") {
		t.Fatalf("CSS virtual document = %q", css.Text)
	}
	if strings.Contains(css.Text, "themeColor") || strings.Contains(css.Text, "displayValue") {
		t.Fatalf("CSS virtual document leaked ASP: %q", css.Text)
	}
	if !strings.Contains(javascript.Text, "const value") || strings.Contains(javascript.Text, "serverValue") {
		t.Fatalf("JavaScript virtual document = %q", javascript.Text)
	}
	if len(html.Segments) == 0 || len(css.Segments) == 0 || len(javascript.Segments) == 0 {
		t.Fatalf("source map segments missing: html=%#v css=%#v javascript=%#v", html.Segments, css.Segments, javascript.Segments)
	}
}

func TestKeepsMixedClassicASPIslandsMaskedWhileRetainingVBScriptSymbols(t *testing.T) {
	source := `<script>
const value = <%= clientValue %>;
</script>
<%
Dim serverValue
Response.Write serverValue
%>`
	parsed := ParseDocument("file:///site/default.asp", source, Settings{})
	javascript := BuildVirtualDocument(parsed, LanguageJavaScript)
	if strings.Contains(javascript.Text, "clientValue") {
		t.Fatalf("JavaScript virtual document leaked ASP expression: %q", javascript.Text)
	}
	block := firstRegionOfKind(parsed, RegionASPBlock)
	if block == nil {
		t.Fatalf("missing VBScript ASP block: %#v", parsed.Regions)
	}
	if got := source[block.ContentStart:block.ContentEnd]; !strings.Contains(got, "serverValue") {
		t.Fatalf("VBScript content = %q", got)
	}
}

func TestMasksInlineASPInsideCSSRegionsWhileKeepingASPCompletionsRoutable(t *testing.T) {
	source := `<style>
.card-<%= className %> { color: <%= themeColor %>; width: <% Response.Write width %>px; }
</style>`
	parsed := ParseDocument("file:///site/default.asp", source, Settings{})
	css := BuildVirtualDocument(parsed, LanguageCSS)
	for _, leaked := range []string{"className", "themeColor", "Response.Write"} {
		if strings.Contains(css.Text, leaked) {
			t.Fatalf("CSS virtual document leaked %q: %q", leaked, css.Text)
		}
	}
	for _, needle := range []string{"className", "themeColor", "Response.Write"} {
		region := RegionAt(parsed, strings.Index(source, needle))
		if region == nil || region.Language != LanguageVBScript {
			t.Fatalf("ASP island for %q not routable: %#v", needle, region)
		}
	}
}

func TestMasksInlineASPInsideJavaScriptRegionsWithSourceMapStablePlaceholders(t *testing.T) {
	source := `<script>
const value = <%= serverValue %>;
const rendered = <% Response.Write clientValue %>;
afterIsland();
</script>`
	parsed := ParseDocument("file:///site/default.asp", source, Settings{})
	js := BuildVirtualDocument(parsed, LanguageJavaScript)
	for _, leaked := range []string{"serverValue", "clientValue", "<%"} {
		if strings.Contains(js.Text, leaked) {
			t.Fatalf("JavaScript virtual document leaked %q: %q", leaked, js.Text)
		}
	}
	if !strings.Contains(js.Text, "afterIsland") || len(js.Segments) < 3 {
		t.Fatalf("JavaScript virtual document/source map = %#v %q", js.Segments, js.Text)
	}
}

func TestKeepsCSSAndJavaScriptVirtualDocumentsStableAfterASPCommentsInsideComments(t *testing.T) {
	source := `<style>
/* <% Response.Write "not css" %> */
.ok { color: red; }
</style>
<script>
// <% Response.Write "not js" %>
const ok = true;
</script>`
	parsed := ParseDocument("file:///site/default.asp", source, Settings{})
	css := BuildVirtualDocument(parsed, LanguageCSS)
	js := BuildVirtualDocument(parsed, LanguageJavaScript)
	if !strings.Contains(css.Text, ".ok") || !strings.Contains(js.Text, "const ok") {
		t.Fatalf("virtual docs lost host content: css=%q js=%q", css.Text, js.Text)
	}
	if !strings.Contains(css.Text, `/* <% Response.Write "not css" %> */`) ||
		!strings.Contains(js.Text, `// <% Response.Write "not js" %>`) {
		t.Fatalf("virtual docs did not preserve host comments: css=%q js=%q", css.Text, js.Text)
	}
}

func TestLeavesInlineASPIslandsUnmappedInsideJavaScriptVirtualDocuments(t *testing.T) {
	source := `<script>
const before = 1;
<% Response.Write "hole" %>
const after = 2;
</script>`
	parsed := ParseDocument("file:///site/default.asp", source, Settings{})
	js := BuildVirtualDocument(parsed, LanguageJavaScript)
	holeStart := strings.Index(source, "<%")
	for _, segment := range js.Segments {
		if segment.SourceStart <= holeStart && holeStart < segment.SourceEnd {
			t.Fatalf("ASP island should not be mapped in JavaScript virtual document: %#v", js.Segments)
		}
	}
	if !strings.Contains(js.Text, "const before") || !strings.Contains(js.Text, "const after") {
		t.Fatalf("JavaScript virtual document lost surrounding content: %q", js.Text)
	}
}

func TestMasksASPExpressionsInsideHTMLTagAttributes(t *testing.T) {
	source := `<input value="<%= value %>" data-id="<%= id %>">`
	parsed := ParseDocument("file:///site/default.asp", source, Settings{})
	html := BuildVirtualDocument(parsed, LanguageHTML)
	if strings.Contains(html.Text, "value %>") || strings.Contains(html.Text, "id %>") {
		t.Fatalf("HTML virtual document leaked ASP attribute expressions: %q", html.Text)
	}
	if !strings.Contains(html.Text, "<input") {
		t.Fatalf("HTML virtual document lost tag: %q", html.Text)
	}
}

func TestMasksQuotedAndGeneratedASPTagAttributesWithDelimiterLikeContent(t *testing.T) {
	source := `<a href="<%= BuildUrl("%>") %>" title="<% Response.Write "<tag>" %>">link</a>`
	parsed := ParseDocument("file:///site/default.asp", source, Settings{})
	html := BuildVirtualDocument(parsed, LanguageHTML)
	for _, leaked := range []string{"BuildUrl", "Response.Write", "%>"} {
		if strings.Contains(html.Text, leaked) {
			t.Fatalf("HTML virtual document leaked %q: %q", leaked, html.Text)
		}
	}
	if !strings.Contains(html.Text, "link</a>") {
		t.Fatalf("HTML virtual document lost HTML content: %q", html.Text)
	}
}

func TestMasksInlineASPInsideCSSValuesAndStyleAttributes(t *testing.T) {
	source := `<style>.x { width: <%= width %>px; color: red; }</style>
<div style="color: <%= color %>; display: block"></div>`
	parsed := ParseDocument("file:///site/default.asp", source, Settings{})
	css := BuildVirtualDocument(parsed, LanguageCSS)
	if strings.Contains(css.Text, "width %>") || strings.Contains(css.Text, "color %>") {
		t.Fatalf("CSS virtual document leaked ASP values: %q", css.Text)
	}
	if !strings.Contains(css.Text, "color: red") || !strings.Contains(css.Text, "*{color:") {
		t.Fatalf("CSS virtual document lost CSS content: %q", css.Text)
	}
}

func TestMasksCSSStatementBoundaryASPBlocksAsWhitespace(t *testing.T) {
	source := `<style>
.a { color: red; }
<% If enabled Then %>
.b { color: blue; }
<% End If %>
</style>`
	parsed := ParseDocument("file:///site/default.asp", source, Settings{})
	css := BuildVirtualDocument(parsed, LanguageCSS)
	if strings.Contains(css.Text, "If enabled") || strings.Contains(css.Text, "End If") {
		t.Fatalf("CSS virtual document leaked ASP blocks: %q", css.Text)
	}
	if !strings.Contains(css.Text, ".a") || !strings.Contains(css.Text, ".b") {
		t.Fatalf("CSS virtual document lost rules: %q", css.Text)
	}
}

func TestMasksASPIslandsThatContainHostQuoteCharactersInsideAttributesAndEmbeddedStrings(t *testing.T) {
	source := strings.Join([]string{
		`<div data-value="<%= Replace(value, """", "'") %>"></div>`,
		"<script>const value = \"<%= Replace(value, `\\\"`, \" + `\"'\"` + \") %>\";</script>",
		`<style>.x::before { content: "<%= Replace(value, "\"", "'") %>"; }</style>`,
	}, "\n")
	parsed := ParseDocument("file:///site/default.asp", source, Settings{})
	html := BuildVirtualDocument(parsed, LanguageHTML)
	js := BuildVirtualDocument(parsed, LanguageJavaScript)
	css := BuildVirtualDocument(parsed, LanguageCSS)
	for name, text := range map[string]string{"html": html.Text, "js": js.Text, "css": css.Text} {
		if strings.Contains(text, "Replace") || strings.Contains(text, "<%") {
			t.Fatalf("%s virtual document leaked host-quote ASP island: %q", name, text)
		}
	}
}

func TestExtractsInlineStyleAttributesAsCSSVirtualDocuments(t *testing.T) {
	parsed := ParseDocument("file:///site/default.asp", `<div style="display: block; color: red"></div>`, Settings{})
	css := BuildVirtualDocument(parsed, LanguageCSS)
	if !strings.Contains(css.Text, "*{display: block; color: red}") || len(css.Segments) == 0 {
		t.Fatalf("CSS virtual style attribute = %#v %q", css.Segments, css.Text)
	}
}

func TestKeepsInlineStyleAttributesAfterCompletionStyleRangeRescans(t *testing.T) {
	source := `<div style="display: block"></div><span style="color: red"></span>`
	parsed := ParseDocument("file:///site/default.asp", source, Settings{})
	if countRegionsOfKind(parsed, RegionStyleAttribute) != 2 {
		t.Fatalf("style attribute regions = %#v", parsed.Regions)
	}
	css := BuildVirtualDocument(parsed, LanguageCSS)
	if strings.Count(css.Text, "*{") != 2 || !strings.Contains(css.Text, "display: block") || !strings.Contains(css.Text, "color: red") {
		t.Fatalf("CSS virtual style attributes = %q", css.Text)
	}
}

func TestExtractsInlineStyleAttributesFromIncompleteOpenTags(t *testing.T) {
	parsed := ParseDocument("file:///site/default.asp", `<div class="x" style="color: red`, Settings{})
	css := BuildVirtualDocument(parsed, LanguageCSS)
	if !strings.Contains(css.Text, "*{color: red}") || firstRegionOfKind(parsed, RegionStyleAttribute) == nil {
		t.Fatalf("incomplete style attribute virtual CSS = %#v %q", parsed.Regions, css.Text)
	}
}

func TestMapsEmptyAndASPOnlyInlineStyleAttributePositionsToCSSVirtualDocuments(t *testing.T) {
	source := `<div style=""><span style="<%= dynamicStyle %>"></span></div>`
	parsed := ParseDocument("file:///site/default.asp", source, Settings{})
	css := BuildVirtualDocument(parsed, LanguageCSS)
	if strings.Count(css.Text, "*{") != 2 || len(css.Segments) == 0 {
		t.Fatalf("CSS virtual empty/ASP-only style attributes = %#v %q", css.Segments, css.Text)
	}
}

func TestExtractsInlineStyleAttributesBeforeAndAfterTheHTMLRoot(t *testing.T) {
	source := `<div style="color: red"></div><html><body></body></html><span style="display: block"></span>`
	parsed := ParseDocument("file:///site/default.asp", source, Settings{})
	css := BuildVirtualDocument(parsed, LanguageCSS)
	if strings.Count(css.Text, "*{") != 2 || !strings.Contains(css.Text, "color: red") || !strings.Contains(css.Text, "display: block") {
		t.Fatalf("CSS virtual attributes before/after root = %q", css.Text)
	}
}

func TestReportsMissingASPCloseDelimiter(t *testing.T) {
	parsed := ParseDocument("file:///broken.asp", "<html><% Response.Write 1", Settings{})
	diagnostics := Diagnostics(parsed)
	if len(diagnostics) != 1 || !strings.Contains(diagnostics[0].Message, "closing %>") {
		t.Fatalf("diagnostics = %#v", diagnostics)
	}
}

func TestIgnoresASPOpenDelimitersInsideClientScriptAndStyleStringsOrComments(t *testing.T) {
	source := `<script>
const literal = "<%";
const angleText = "<>";
// <% not an ASP island
const value = <%= serverValue %>;
</script>
<style>
.literal::before { content: "<%"; }
/* <% not an ASP island */
.dynamic { width: <%= width %>px; }
</style>`
	parsed := ParseDocument("file:///site/client-literal-delimiters.asp", source, Settings{})
	if len(parsed.Errors) != 0 {
		t.Fatalf("errors = %#v", parsed.Errors)
	}
	if countRegionsOfKind(parsed, RegionASPExpression) != 2 {
		t.Fatalf("expression regions = %#v", parsed.Regions)
	}
	if firstRegionOfKind(parsed, RegionClientScript) == nil || firstRegionOfKind(parsed, RegionStyle) == nil {
		t.Fatalf("embedded regions = %#v", parsed.Regions)
	}
	if js := BuildVirtualDocument(parsed, LanguageJavaScript).Text; !strings.Contains(js, `const literal = "<%";`) {
		t.Fatalf("JavaScript virtual document = %q", js)
	}
	if css := BuildVirtualDocument(parsed, LanguageCSS).Text; !strings.Contains(css, `content: "<%";`) {
		t.Fatalf("CSS virtual document = %q", css)
	}
}

func TestClosesASPRegionsAtRawDelimitersInsideScriptStrings(t *testing.T) {
	source := `<%
Response.Write "%>"
Response.Write "done"
%>
<%@ LANGUAGE="JScript" %><% var text = '%>'; Response.Write(text); %>`
	parsed := ParseDocument("file:///site/delimiters.asp", source, Settings{})
	if len(parsed.Errors) != 0 {
		t.Fatalf("errors = %#v", parsed.Errors)
	}
	blocks := regionsOfKinds(parsed, RegionASPBlock, RegionASPDirective)
	if len(blocks) != 3 {
		t.Fatalf("blocks = %#v", blocks)
	}
	if blocks[0].End != strings.Index(source, "%>")+len("%>") {
		t.Fatalf("first block end = %d", blocks[0].End)
	}
	firstBlock := source[blocks[0].ContentStart:blocks[0].ContentEnd]
	if !strings.Contains(firstBlock, `Response.Write "`) || strings.Contains(firstBlock, `Response.Write "done"`) {
		t.Fatalf("first block content = %q", firstBlock)
	}
	jsBlock := source[blocks[2].ContentStart:blocks[2].ContentEnd]
	if !strings.Contains(jsBlock, `var text = '`) || strings.Contains(jsBlock, "Response.Write(text)") {
		t.Fatalf("JScript block content = %q", jsBlock)
	}
	html := htmlText(parsed)
	if !strings.Contains(html, `Response.Write "done"`) || !strings.Contains(html, "Response.Write(text)") {
		t.Fatalf("HTML after raw delimiters = %q", html)
	}
}

func TestClosesASPRegionsAtDelimitersOnVBScriptCommentLines(t *testing.T) {
	source := `<%
' comment with %>
<div>done</div>`
	parsed := ParseDocument("file:///site/vb-comment-close.asp", source, Settings{})
	if len(parsed.Errors) != 0 {
		t.Fatalf("errors = %#v", parsed.Errors)
	}
	block := firstRegionOfKind(parsed, RegionASPBlock)
	if block == nil || block.End != strings.Index(source, "%>")+len("%>") {
		t.Fatalf("block = %#v", block)
	}
	content := source[block.ContentStart:block.ContentEnd]
	if !strings.Contains(content, "' comment with ") || strings.Contains(content, "<div>") {
		t.Fatalf("block content = %q", content)
	}
	if !strings.Contains(htmlText(parsed), "<div>done</div>") {
		t.Fatalf("HTML after VBScript comment delimiter = %q", htmlText(parsed))
	}
}

func TestKeepsHTMLTextAfterInlineVBScriptCommentDelimiters(t *testing.T) {
	source := `<div>
<%' あいうえお %>
テキスト
</div>`
	parsed := ParseDocument("file:///site/inline-vb-comment-close.asp", source, Settings{})
	if len(parsed.Errors) != 0 {
		t.Fatalf("errors = %#v", parsed.Errors)
	}
	block := firstRegionOfKind(parsed, RegionASPBlock)
	if block == nil || block.End != strings.Index(source, "%>")+len("%>") {
		t.Fatalf("block = %#v", block)
	}
	if !strings.Contains(htmlText(parsed), "テキスト") {
		t.Fatalf("HTML after inline comment delimiter = %q", htmlText(parsed))
	}
}

func TestClosesJScriptASPRegionsAtDelimitersInsideComments(t *testing.T) {
	source := `<%@ LANGUAGE="JScript" %>
<%
// line comment with %>
Response.Write("line")
%>
<%
/* block comment with %> */
Response.Write("block")
%>`
	parsed := ParseDocument("file:///site/jscript-comment-close.asp", source, Settings{})
	if len(parsed.Errors) != 0 {
		t.Fatalf("errors = %#v", parsed.Errors)
	}
	blocks := regionsOfKinds(parsed, RegionASPBlock)
	if len(blocks) != 2 {
		t.Fatalf("blocks = %#v", blocks)
	}
	lineBlock := source[blocks[0].ContentStart:blocks[0].ContentEnd]
	blockCommentBlock := source[blocks[1].ContentStart:blocks[1].ContentEnd]
	if !strings.Contains(lineBlock, "// line comment with ") || strings.Contains(lineBlock, `Response.Write("line")`) {
		t.Fatalf("line block = %q", lineBlock)
	}
	if !strings.Contains(blockCommentBlock, "/* block comment with ") || strings.Contains(blockCommentBlock, `Response.Write("block")`) {
		t.Fatalf("block comment block = %q", blockCommentBlock)
	}
	html := htmlText(parsed)
	if !strings.Contains(html, `Response.Write("line")`) || !strings.Contains(html, `Response.Write("block")`) {
		t.Fatalf("HTML after JScript comment delimiters = %q", html)
	}
}

func TestClosesASPRegionsAtRawDelimitersInsideNonPrimaryCommentSyntax(t *testing.T) {
	source := `<%
/* block comment with %> */
// line comment with %>
Response.Write "done"
%>`
	parsed := ParseDocument("file:///site/comment-delimiters.asp", source, Settings{})
	if len(parsed.Errors) != 0 {
		t.Fatalf("errors = %#v", parsed.Errors)
	}
	blocks := regionsOfKinds(parsed, RegionASPBlock)
	if len(blocks) != 1 || blocks[0].End != strings.Index(source, "%>")+len("%>") {
		t.Fatalf("blocks = %#v", blocks)
	}
	content := source[blocks[0].ContentStart:blocks[0].ContentEnd]
	if !strings.Contains(content, "/* block comment with ") || strings.Contains(content, `Response.Write "done"`) {
		t.Fatalf("block content = %q", content)
	}
}

func TestClosesScriptAndStyleRegionsAtRawClosingTagsInsideStringsOrComments(t *testing.T) {
	source := `<script>
const literal = "</script>";
// </script>
const ok = true;
</script>
<style>
.x::before { content: "</style>"; }
/* </style> */
.x { color: red; }
</style>`
	parsed := ParseDocument("file:///site/tags.asp", source, Settings{})
	script := firstRegionOfKind(parsed, RegionClientScript)
	style := firstRegionOfKind(parsed, RegionStyle)
	if script == nil || script.End != strings.Index(source, "</script>")+len("</script>") {
		t.Fatalf("script region = %#v", script)
	}
	if style == nil || style.End != strings.Index(source, "</style>")+len("</style>") {
		t.Fatalf("style region = %#v", style)
	}
	if strings.Contains(source[script.ContentStart:script.ContentEnd], "const ok = true;") {
		t.Fatalf("script content = %q", source[script.ContentStart:script.ContentEnd])
	}
	if strings.Contains(source[style.ContentStart:style.ContentEnd], "color: red") {
		t.Fatalf("style content = %q", source[style.ContentStart:style.ContentEnd])
	}
}

func TestDoesNotCloseScriptAndStyleRegionsAtRawClosingTagsInsideASPIslands(t *testing.T) {
	source := `<script>
<%
Do While ready
Response.Write "</script>"
%>
const afterAsp = true;
<%
Loop
%>
</script>
<style>
<%
Do While ready
Response.Write "</style>"
%>
.after-asp { color: red; }
<%
Loop
%>
</style>`
	parsed := ParseDocument("file:///site/asp-island-raw-script-close.asp", source, Settings{})
	if len(parsed.Errors) != 0 {
		t.Fatalf("errors = %#v", parsed.Errors)
	}
	script := firstRegionOfKind(parsed, RegionClientScript)
	style := firstRegionOfKind(parsed, RegionStyle)
	if script == nil || script.End != strings.Index(source[strings.Index(source, "const afterAsp"):], "</script>")+strings.Index(source, "const afterAsp")+len("</script>") {
		t.Fatalf("script region = %#v", script)
	}
	if style == nil || style.End != strings.Index(source[strings.Index(source, ".after-asp"):], "</style>")+strings.Index(source, ".after-asp")+len("</style>") {
		t.Fatalf("style region = %#v", style)
	}
	if !strings.Contains(source[script.ContentStart:script.ContentEnd], "const afterAsp = true;") {
		t.Fatalf("script content = %q", source[script.ContentStart:script.ContentEnd])
	}
	if !strings.Contains(source[style.ContentStart:style.ContentEnd], ".after-asp { color: red; }") {
		t.Fatalf("style content = %q", source[style.ContentStart:style.ContentEnd])
	}
	if countRegionsOfKind(parsed, RegionASPBlock) != 4 {
		t.Fatalf("ASP blocks = %#v", parsed.Regions)
	}
}

func TestClosesServerSideJScriptRegionsAtRawScriptEndTagsInsideStrings(t *testing.T) {
	source := `<%@ LANGUAGE="JScript" %>
<script runat="server" language="JScript">
function render() {
  var literal = "</script>";
  Response.Write(literal);
}
</script>`
	parsed := ParseDocument("file:///site/server-jscript-script-close.asp", source, Settings{})
	if len(parsed.Errors) != 0 {
		t.Fatalf("errors = %#v", parsed.Errors)
	}
	script := firstRegionOfKind(parsed, RegionServerScript)
	if script == nil || script.Language != LanguageJScript || script.End != strings.Index(source, "</script>")+len("</script>") {
		t.Fatalf("server script region = %#v", script)
	}
	content := source[script.ContentStart:script.ContentEnd]
	if !strings.Contains(content, `var literal = "`) || strings.Contains(content, "Response.Write(literal)") {
		t.Fatalf("server script content = %q", content)
	}
}

func TestKeepsExplicitServerScriptLanguageEvenWhenPageDefaultIsDifferent(t *testing.T) {
	parsed := ParseDocument(
		"file:///mixed.asp",
		`<%@ LANGUAGE="JScript" %><script runat="server" language="VBScript">Dim value</script>`,
		Settings{},
	)
	serverScript := firstRegionOfKind(parsed, RegionServerScript)
	if serverScript == nil || serverScript.Language != LanguageVBScript {
		t.Fatalf("server script region = %#v", serverScript)
	}
}

func TestDoesNotParseServerSideJScriptAsVBScriptCST(t *testing.T) {
	parsed := ParseDocument(
		"file:///server-jscript.asp",
		`<%@ LANGUAGE="JScript" %><% var missingName = 1; %>`,
		Settings{},
	)
	block := firstRegionOfKind(parsed, RegionASPBlock)
	if block == nil || block.Language != LanguageJScript {
		t.Fatalf("JScript ASP block = %#v", block)
	}
	if diagnostics := Diagnostics(parsed); len(diagnostics) != 0 {
		t.Fatalf("parser diagnostics = %#v", diagnostics)
	}
}

func TestModelsStandaloneVBSAsMappableVBScriptVirtualDocument(t *testing.T) {
	source := `Option Explicit
Dim shell
Set shell = WScript.CreateObject("WScript.Shell")
WScript.Echo shell.ExpandEnvironmentStrings("%TEMP%")
`
	parsed := ParseDocument("file:///site/startup.vbs", source, Settings{DefaultLanguage: "VBScript"})
	if len(parsed.Regions) != 1 {
		t.Fatalf("standalone VBS regions = %#v", parsed.Regions)
	}
	region := parsed.Regions[0]
	if region.Kind != RegionServerScript || region.Language != LanguageVBScript ||
		region.Start != 0 || region.End != len(source) ||
		region.ContentStart != 0 || region.ContentEnd != len(source) {
		t.Fatalf("standalone VBS region mismatch: %#v", region)
	}
	virtual := BuildVirtualDocument(parsed, LanguageVBScript)
	if !strings.Contains(virtual.Text, "WScript.Echo shell.ExpandEnvironmentStrings") {
		t.Fatalf("standalone VBS virtual document lost content: %q", virtual.Text)
	}
	target := strings.Index(source, "WScript.Echo")
	mapped := false
	for _, segment := range virtual.Segments {
		if segment.SourceStart <= target && target < segment.SourceEnd && segment.VirtualStart+(target-segment.SourceStart) == target {
			mapped = true
			break
		}
	}
	if !mapped {
		t.Fatalf("standalone VBS source map does not preserve offset %d: %#v", target, virtual.Segments)
	}
}

func regionsOfKinds(parsed *ParsedDocument, kinds ...RegionKind) []Region {
	want := map[RegionKind]bool{}
	for _, kind := range kinds {
		want[kind] = true
	}
	var regions []Region
	for _, region := range parsed.Regions {
		if want[region.Kind] {
			regions = append(regions, region)
		}
	}
	return regions
}
