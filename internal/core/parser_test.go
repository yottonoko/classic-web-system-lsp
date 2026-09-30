package core

import (
	"strings"
	"testing"
)

func TestParseDocumentRegionsAndIncludes(t *testing.T) {
	source := `<!-- #include file="inc/header.inc" -->
<html><body><%= title %><% If ok Then %>ok<% End If %></body></html>`
	parsed := ParseDocument("file:///default.asp", source, Settings{DefaultLanguage: "VBScript"})
	if len(parsed.Includes) != 1 || parsed.Includes[0].Path != "inc/header.inc" {
		t.Fatalf("includes = %#v", parsed.Includes)
	}
	var expressions, blocks int
	for _, region := range parsed.Regions {
		if region.Kind == RegionASPExpression {
			expressions++
		}
		if region.Kind == RegionASPBlock {
			blocks++
		}
	}
	if expressions != 1 || blocks != 2 {
		t.Fatalf("expression regions = %d, block regions = %d", expressions, blocks)
	}
}

func TestParseDocumentReportsUnclosedASPBlock(t *testing.T) {
	parsed := ParseDocument("file:///broken.asp", "<% If ok Then", Settings{})
	if len(parsed.Errors) != 1 {
		t.Fatalf("errors = %#v", parsed.Errors)
	}
	diagnostics := Diagnostics(parsed)
	if len(diagnostics) != 1 || diagnostics[0].Source != "asp-lsp-go" {
		t.Fatalf("diagnostics = %#v", diagnostics)
	}
}

func TestParseDocumentUsesASPDirectiveLanguage(t *testing.T) {
	source := `<%@ LANGUAGE="JScript" %><% var text = "a=b"; %>`
	parsed := ParseDocument("file:///jscript.asp", source, Settings{DefaultLanguage: "VBScript"})
	if parsed.DefaultLanguage != LanguageJScript {
		t.Fatalf("default language = %q", parsed.DefaultLanguage)
	}
	var block *Region
	for i := range parsed.Regions {
		if parsed.Regions[i].Kind == RegionASPBlock {
			block = &parsed.Regions[i]
			break
		}
	}
	if block == nil || block.Language != LanguageJScript {
		t.Fatalf("ASP block region = %#v", block)
	}
}

func TestParseDocumentUsesASPDirectiveLanguageWithSpacingAroundEquals(t *testing.T) {
	source := `<%@ LANGUAGE = "JScript" %><% var value = 1; %>`
	parsed := ParseDocument("file:///spaced-jscript.asp", source, Settings{DefaultLanguage: "VBScript"})
	if parsed.DefaultLanguage != LanguageJScript {
		t.Fatalf("default language = %q", parsed.DefaultLanguage)
	}
	block := firstRegionOfKind(parsed, RegionASPBlock)
	if block == nil || block.Language != LanguageJScript {
		t.Fatalf("ASP block region = %#v", block)
	}
}

func TestParseDocumentSkipsQuotedLiteralASPOpen(t *testing.T) {
	source := `<script>
const fake = "<% not an island";
missingAfterLiteral();
const fromAsp = <%= value %>;
</script>`
	parsed := ParseDocument("file:///quoted.asp", source, Settings{})
	if len(parsed.Errors) != 0 {
		t.Fatalf("errors = %#v", parsed.Errors)
	}
	var expressions int
	for _, region := range parsed.Regions {
		if region.Kind == RegionASPExpression {
			expressions++
			if region.ContentStart == 0 || source[region.ContentStart-1] != '=' {
				t.Fatalf("unexpected expression region = %#v", region)
			}
		}
	}
	if expressions != 1 {
		t.Fatalf("expression regions = %d in %#v", expressions, parsed.Regions)
	}
	virtual := BuildVirtualDocument(parsed, LanguageJavaScript)
	if !strings.Contains(virtual.Text, "missingAfterLiteral") {
		t.Fatalf("virtual JavaScript = %q", virtual.Text)
	}
}

func TestParseDocumentIgnoresClientScriptAndStyleCommentASPOpen(t *testing.T) {
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
	parsed := ParseDocument("file:///client-literal-delimiters.asp", source, Settings{})
	if len(parsed.Errors) != 0 {
		t.Fatalf("errors = %#v", parsed.Errors)
	}
	var expressions int
	for _, region := range parsed.Regions {
		if region.Kind == RegionASPExpression {
			expressions++
		}
	}
	if expressions != 2 {
		t.Fatalf("expression regions = %d in %#v", expressions, parsed.Regions)
	}
	javascript := BuildVirtualDocument(parsed, LanguageJavaScript)
	if !strings.Contains(javascript.Text, `const literal = "<%";`) ||
		!strings.Contains(javascript.Text, `// <% not an ASP island`) {
		t.Fatalf("virtual JavaScript did not retain client literals/comments: %q", javascript.Text)
	}
	css := BuildVirtualDocument(parsed, LanguageCSS)
	if !strings.Contains(css.Text, `content: "<%";`) ||
		!strings.Contains(css.Text, `/* <% not an ASP island */`) {
		t.Fatalf("virtual CSS did not retain client literals/comments: %q", css.Text)
	}
}

func TestParseDocumentIgnoresMultipleASPOpensInsideClientBlockComment(t *testing.T) {
	source := `<style>
/* <% fake %> <% still-comment */
.card { color: <%= color %>; }
</style>`
	parsed := ParseDocument("file:///multiple-comment-delimiters.asp", source, Settings{})
	if len(parsed.Errors) != 0 {
		t.Fatalf("errors = %#v", parsed.Errors)
	}
	if countRegionsOfKind(parsed, RegionASPBlock) != 0 {
		t.Fatalf("unexpected ASP blocks = %#v", parsed.Regions)
	}
	if countRegionsOfKind(parsed, RegionASPExpression) != 1 {
		t.Fatalf("ASP expressions = %#v", parsed.Regions)
	}
	css := BuildVirtualDocument(parsed, LanguageCSS)
	if !strings.Contains(css.Text, `/* <% fake %> <% still-comment */`) {
		t.Fatalf("CSS virtual document lost the client comment: %q", css.Text)
	}
}

func TestParseDocumentIgnoresUnclosedASPOpensInHTMLCommentsAndRawText(t *testing.T) {
	source := `<!-- fake <% not an ASP island -->
<textarea>fake <% not an ASP island</textarea>
<% value = 1 %>`
	parsed := ParseDocument("file:///html-false-asp.asp", source, Settings{})
	if len(parsed.Errors) != 0 {
		t.Fatalf("errors = %#v", parsed.Errors)
	}
	if countRegionsOfKind(parsed, RegionASPBlock) != 1 {
		t.Fatalf("ASP regions = %#v", parsed.Regions)
	}
	block := firstRegionOfKind(parsed, RegionASPBlock)
	if block == nil || !strings.Contains(source[block.ContentStart:block.ContentEnd], "value = 1") {
		t.Fatalf("ASP block = %#v", block)
	}
}

func TestParseDocumentTreatsHTMLCommentTerminatorsOutsideASPQuotes(t *testing.T) {
	tests := []struct {
		name       string
		source     string
		aspContent string
		comment    string
	}{
		{
			name:       "double quote",
			source:     `<!-- " --> <% Function VisibleAfterComment() %> <div>after</div>`,
			aspContent: "Function VisibleAfterComment()",
			comment:    `<!-- " -->`,
		},
		{
			name:       "backtick",
			source:     "<!-- ` --> <% Function VisibleAfterBacktickComment() %> <div>after</div>",
			aspContent: "Function VisibleAfterBacktickComment()",
			comment:    "<!-- ` -->",
		},
	}
	for _, testCase := range tests {
		t.Run(testCase.name, func(t *testing.T) {
			parsed := ParseDocument("file:///comment-terminator-quote.asp", testCase.source, Settings{})
			if len(parsed.Errors) != 0 {
				t.Fatalf("errors = %#v", parsed.Errors)
			}
			block := firstRegionOfKind(parsed, RegionASPBlock)
			open := strings.Index(testCase.source, "<%")
			close := strings.Index(testCase.source[open+2:], "%>") + open + 2
			if block == nil || block.Start != open || block.End != close+len("%>") {
				t.Fatalf("ASP block = %#v, want [%d, %d)", block, open, close+len("%>"))
			}
			if got := testCase.source[block.ContentStart:block.ContentEnd]; got != " "+testCase.aspContent+" " {
				t.Fatalf("ASP content = %q, want %q", got, " "+testCase.aspContent+" ")
			}
			if !strings.Contains(htmlText(parsed), testCase.comment) || !strings.Contains(htmlText(parsed), "<div>after</div>") {
				t.Fatalf("HTML regions = %q", htmlText(parsed))
			}
		})
	}
}

func TestParseDocumentKeepsASPOpensAfterOrdinaryHTMLCommentQuotes(t *testing.T) {
	tests := []struct {
		name       string
		source     string
		aspContent string
	}{
		{
			name:       "double quote",
			source:     `<!-- prose " <% Function VisibleInsideComment() %> -->`,
			aspContent: "Function VisibleInsideComment()",
		},
		{
			name:       "backtick",
			source:     "<!-- prose ` <% Function VisibleInsideBacktickComment() %> -->",
			aspContent: "Function VisibleInsideBacktickComment()",
		},
	}
	for _, testCase := range tests {
		t.Run(testCase.name, func(t *testing.T) {
			parsed := ParseDocument("file:///comment-asp-after-quote.asp", testCase.source, Settings{})
			if len(parsed.Errors) != 0 {
				t.Fatalf("errors = %#v", parsed.Errors)
			}
			if countRegionsOfKind(parsed, RegionASPBlock) != 1 {
				t.Fatalf("ASP regions = %#v", parsed.Regions)
			}
			block := firstRegionOfKind(parsed, RegionASPBlock)
			if block == nil || !strings.Contains(testCase.source[block.ContentStart:block.ContentEnd], testCase.aspContent) {
				t.Fatalf("ASP block = %#v", block)
			}
			if got := htmlText(parsed); !strings.Contains(got, "<!-- prose ") || !strings.HasSuffix(got, " -->") {
				t.Fatalf("HTML regions = %q", got)
			}
		})
	}
}

func TestParseDocumentFindsMultipleASPOpensInsideOneHTMLComment(t *testing.T) {
	source := `<!-- <% first = 1 %> <% second = 2 %> -->`
	parsed := ParseDocument("file:///multiple-comment-asp.asp", source, Settings{})
	if len(parsed.Errors) != 0 {
		t.Fatalf("errors = %#v", parsed.Errors)
	}
	if countRegionsOfKind(parsed, RegionASPBlock) != 2 {
		t.Fatalf("ASP blocks = %#v", parsed.Regions)
	}
	if strings.Contains(htmlText(parsed), "first") || strings.Contains(htmlText(parsed), "second") {
		t.Fatalf("HTML retained ASP comment islands = %q", htmlText(parsed))
	}
}

func TestParseDocumentKeepsClientCommentFilteringAcrossHTMLCommentASPOpens(t *testing.T) {
	source := `<!-- <% first = 1 %> // <% second = 2 %> -->`
	parsed := ParseDocument("file:///comment-client-state.asp", source, Settings{})
	if len(parsed.Errors) != 0 {
		t.Fatalf("errors = %#v", parsed.Errors)
	}
	if countRegionsOfKind(parsed, RegionASPBlock) != 1 {
		t.Fatalf("ASP blocks = %#v", parsed.Regions)
	}
	if !strings.Contains(htmlText(parsed), "second = 2") {
		t.Fatalf("client-comment ASP text was not retained = %q", htmlText(parsed))
	}
}

func TestParseDocumentClosesASPOpenInsideMultilineHTMLAttributeAfterQuotedDelimiter(t *testing.T) {
	source := `<div
 data="<%= BuildUrl("%>") %>"
><span>after</span></div>`
	parsed := ParseDocument("file:///multiline-attribute.asp", source, Settings{})
	if len(parsed.Errors) != 0 {
		t.Fatalf("errors = %#v", parsed.Errors)
	}
	if countRegionsOfKind(parsed, RegionASPExpression) != 1 {
		t.Fatalf("ASP expressions = %#v", parsed.Regions)
	}
	expression := firstRegionOfKind(parsed, RegionASPExpression)
	if expression == nil || expression.End != strings.LastIndex(source, "%>")+len("%>") {
		t.Fatalf("ASP expression = %#v", expression)
	}
	if strings.Contains(htmlText(parsed), "BuildUrl") || !strings.Contains(htmlText(parsed), "<span>after</span>") {
		t.Fatalf("HTML after multiline attribute expression = %q", htmlText(parsed))
	}
}

func TestParseDocumentDoesNotCarryFakeRawTextTagAttributeStateIntoLaterASP(t *testing.T) {
	testCases := []struct {
		name   string
		source string
	}{
		{
			name: "script",
			source: `<script>
const fake = "<textarea data="unterminated";
</script>
<% Response.Write "%>`,
		},
		{
			name: "style",
			source: `<style>
.fake::before { content: "<textarea data="unterminated"; }
</style>
<% Response.Write "%>`,
		},
		{
			name: "textarea",
			source: `<textarea>fake <div data="unterminated</textarea>
<% Response.Write "%>`,
		},
	}
	for _, testCase := range testCases {
		t.Run(testCase.name, func(t *testing.T) {
			parsed := ParseDocument("file:///raw-state-contamination.asp", testCase.source, Settings{})
			if len(parsed.Errors) != 0 {
				t.Fatalf("errors = %#v", parsed.Errors)
			}
			block := firstRegionOfKind(parsed, RegionASPBlock)
			if block == nil || block.End != strings.Index(testCase.source, "%>")+len("%>") {
				t.Fatalf("ASP block = %#v", block)
			}
		})
	}
}

func TestParseDocumentIgnoresRawTextTagLookalikesInsideScriptStringsAndComments(t *testing.T) {
	source := `<script>
const fakeTag = "<textarea>";
// <textarea>
</script>
<% Response.Write "after script" %>`
	parsed := ParseDocument("file:///script-raw-text-lookalike.asp", source, Settings{})
	if len(parsed.Errors) != 0 {
		t.Fatalf("errors = %#v", parsed.Errors)
	}
	if countRegionsOfKind(parsed, RegionASPBlock) != 1 {
		t.Fatalf("ASP blocks = %#v", parsed.Regions)
	}
	block := firstRegionOfKind(parsed, RegionASPBlock)
	if block == nil || !strings.Contains(source[block.ContentStart:block.ContentEnd], "after script") {
		t.Fatalf("ASP block = %#v", block)
	}
}

func TestParseDocumentUsesFirstHTMLScriptAndStyleCloserForRawTextFiltering(t *testing.T) {
	testCases := []struct {
		name       string
		source     string
		kind       RegionKind
		closingTag string
	}{
		{
			name: "script",
			source: `<script>
const fakeTag = "<textarea>";
// <textarea>
const label = "日本語";
const firstCloser = "</script>";
<textarea>fake <% no close</textarea><% Response.Write "after script" %>`,
			kind:       RegionClientScript,
			closingTag: "</script>",
		},
		{
			name: "style",
			source: `<style>
.fake::before { content: "<textarea>"; }
/* <textarea> */
.label::before { content: "日本語"; }
.first-closer::before { content: "</style>"; }
<textarea>fake <% no close</textarea><% Response.Write "after style" %>`,
			kind:       RegionStyle,
			closingTag: "</style>",
		},
	}
	for _, testCase := range testCases {
		t.Run(testCase.name, func(t *testing.T) {
			parsed := ParseDocument("file:///first-html-closer.asp", testCase.source, Settings{})
			if len(parsed.Errors) != 0 {
				t.Fatalf("errors = %#v", parsed.Errors)
			}
			region := firstRegionOfKind(parsed, testCase.kind)
			close := strings.Index(testCase.source, testCase.closingTag)
			if region == nil || region.End != close+len(testCase.closingTag) {
				t.Fatalf("embedded region = %#v", region)
			}
			block := firstRegionOfKind(parsed, RegionASPBlock)
			if block == nil || !strings.Contains(testCase.source[block.ContentStart:block.ContentEnd], "after "+testCase.name) {
				t.Fatalf("ASP block = %#v", block)
			}
		})
	}
}

func TestParseDocumentDoesNotUseLaterHTMLClosersForFalseASPOpens(t *testing.T) {
	testCases := []struct {
		name   string
		source string
	}{
		{
			name:   "comment",
			source: `<!-- fake <% no close --> <!-- later %> --><% value = 1 %>`,
		},
		{
			name:   "raw text",
			source: `<textarea>fake <% no close</textarea><textarea>later %></textarea><% value = 1 %>`,
		},
	}
	for _, testCase := range testCases {
		t.Run(testCase.name, func(t *testing.T) {
			parsed := ParseDocument("file:///html-false-asp-later-close.asp", testCase.source, Settings{})
			if len(parsed.Errors) != 0 {
				t.Fatalf("errors = %#v", parsed.Errors)
			}
			if countRegionsOfKind(parsed, RegionASPBlock) != 1 {
				t.Fatalf("ASP regions = %#v", parsed.Regions)
			}
			block := firstRegionOfKind(parsed, RegionASPBlock)
			if block == nil || !strings.Contains(testCase.source[block.ContentStart:block.ContentEnd], "value = 1") {
				t.Fatalf("ASP block = %#v", block)
			}
		})
	}
}

func TestParseDocumentKeepsASPOpensInsideMultilineJavaScriptTemplatesAsText(t *testing.T) {
	source := `<script>
const fake = ` + "`" + `
<% not an ASP island
` + "`" + `;
const value = <%= serverValue %>;
</script>`
	parsed := ParseDocument("file:///template-asp.asp", source, Settings{})
	if len(parsed.Errors) != 0 {
		t.Fatalf("errors = %#v", parsed.Errors)
	}
	if countRegionsOfKind(parsed, RegionASPExpression) != 1 {
		t.Fatalf("ASP regions = %#v", parsed.Regions)
	}
	if !strings.Contains(source[firstRegionOfKind(parsed, RegionClientScript).ContentStart:firstRegionOfKind(parsed, RegionClientScript).ContentEnd], "not an ASP island") {
		t.Fatalf("template text was not retained: %#v", parsed.Regions)
	}
}

func TestParseDocumentKeepsSameLineASPOpensInsideJavaScriptTemplatesAsText(t *testing.T) {
	source := `<script>const fake = ` + "`" + `<% not an ASP island %>` + "`" + `; const value = <%= serverValue %>;</script>`
	parsed := ParseDocument("file:///same-line-template-asp.asp", source, Settings{})
	if len(parsed.Errors) != 0 {
		t.Fatalf("errors = %#v", parsed.Errors)
	}
	if countRegionsOfKind(parsed, RegionASPExpression) != 1 || countRegionsOfKind(parsed, RegionASPBlock) != 0 {
		t.Fatalf("ASP regions = %#v", parsed.Regions)
	}
	javascript := BuildVirtualDocument(parsed, LanguageJavaScript)
	if !strings.Contains(javascript.Text, "<% not an ASP island %>") {
		t.Fatalf("same-line template text was not retained: %q", javascript.Text)
	}
}

func TestParseDocumentIgnoresScriptTemplateLookalikesInHTMLAttributesBeforeASP(t *testing.T) {
	source := `<div data-example="<script>` + "`" + `"></div><% value = 1 %>`
	open := strings.LastIndex(source, "<%")
	candidate, ok := htmlASPOpenCandidate(source, open)
	if !ok || candidate.context != htmlScanData {
		t.Fatalf("ASP candidate = %#v, found=%t", candidate, ok)
	}
	if aspOpenLooksLikeJSTemplateLiteral(source, open) {
		t.Fatal("HTML attribute script lookalike was treated as an active JavaScript template")
	}

	parsed := ParseDocument("file:///attribute-script-lookalike.asp", source, Settings{})
	if len(parsed.Errors) != 0 || countRegionsOfKind(parsed, RegionASPBlock) != 1 {
		t.Fatalf("parsed document = errors %#v, regions %#v", parsed.Errors, parsed.Regions)
	}
}

func TestParseDocumentRecognizesASPOutputExpressionsInsideJavaScriptTemplates(t *testing.T) {
	source := `<script>const target = ` + "`prefix-<%= serverValue %>`" + `;</script>`
	parsed := ParseDocument("file:///template-output-expression.asp", source, Settings{})
	if len(parsed.Errors) != 0 {
		t.Fatalf("errors = %#v", parsed.Errors)
	}
	if countRegionsOfKind(parsed, RegionASPExpression) != 1 {
		t.Fatalf("ASP regions = %#v", parsed.Regions)
	}
	expression := firstRegionOfKind(parsed, RegionASPExpression)
	if expression == nil || source[expression.ContentStart:expression.ContentEnd] != " serverValue " {
		t.Fatalf("ASP output expression = %#v", expression)
	}
}

func TestParseDocumentRecognizesMultilineASPOutputExpressionsInsideJavaScriptLiterals(t *testing.T) {
	source := `<!-- #include file="shared.inc" -->
<textarea>raw text feature</textarea>
<script>
const quoted = "<%=
quotedTarget
%>";
const templated = ` + "`<%=\ntemplateTarget\n%>`" + `;
</script>`
	parsed := ParseDocument("file:///multiline-literal-output-expressions.asp", source, Settings{})
	if len(parsed.Errors) != 0 {
		t.Fatalf("errors = %#v", parsed.Errors)
	}
	if countRegionsOfKind(parsed, RegionASPExpression) != 2 {
		t.Fatalf("ASP regions = %#v, want two output expressions", parsed.Regions)
	}
}

func TestParseDocumentSkipsScriptAndStyleElementsInsideHTMLCommentsAndRawText(t *testing.T) {
	source := `<!-- <script>fake()</script><style>.fake{}</style> -->
<textarea><script>fake()</script><style>.fake{}</style></textarea>
<script>actual()</script><style>.actual{}</style>`
	parsed := ParseDocument("file:///embedded-false-regions.asp", source, Settings{})
	if countRegionsOfKind(parsed, RegionClientScript) != 1 || countRegionsOfKind(parsed, RegionStyle) != 1 {
		t.Fatalf("embedded regions = %#v", parsed.Regions)
	}
	script := firstRegionOfKind(parsed, RegionClientScript)
	style := firstRegionOfKind(parsed, RegionStyle)
	if script == nil || !strings.Contains(source[script.ContentStart:script.ContentEnd], "actual()") {
		t.Fatalf("script region = %#v", script)
	}
	if style == nil || !strings.Contains(source[style.ContentStart:style.ContentEnd], ".actual") {
		t.Fatalf("style region = %#v", style)
	}
}

func TestParseDocumentKeepsEmbeddedRegionOffsetsAfterLengthChangingUnicodeFold(t *testing.T) {
	source := "K<STYLE>.card { color: red; }</STYLE><SCRIPT>const value = 1;</SCRIPT>"
	parsed := ParseDocument("file:///unicode-before-embedded.asp", source, Settings{})

	style := firstRegionOfKind(parsed, RegionStyle)
	if style == nil {
		t.Fatalf("style region missing: %#v", parsed.Regions)
	}
	if style.Start != strings.Index(source, "<STYLE>") ||
		source[style.ContentStart:style.ContentEnd] != ".card { color: red; }" {
		t.Fatalf("style region = %#v, content = %q", style, source[style.ContentStart:style.ContentEnd])
	}

	script := firstRegionOfKind(parsed, RegionClientScript)
	if script == nil {
		t.Fatalf("script region missing: %#v", parsed.Regions)
	}
	if script.Start != strings.Index(source, "<SCRIPT>") ||
		source[script.ContentStart:script.ContentEnd] != "const value = 1;" {
		t.Fatalf("script region = %#v, content = %q", script, source[script.ContentStart:script.ContentEnd])
	}
}

func TestParseDocumentFindsStyleAttributesOnlyInHTMLOpeningTags(t *testing.T) {
	source := `<script>const literal = 'style="color: red"';</script>
<style>.card::before { content: "style=display:block"; }</style>
<script/>const slashLiteral = '<div style="position: fixed"></div>';</script>
<style/>.slash::before { content: '<div style="position: sticky">'; }</style>
<textarea><div style="text-align: center"></div></textarea>
<title><div style="text-decoration: none"></div></title>
<!-- <div style="visibility: hidden"></div> -->
<p>style="font-weight: bold"</p>
<div data-example='style="border: 0"'></div>
<div style="color: <%= color %>"></div>`
	parsed := ParseDocument("file:///style-attribute-contexts.asp", source, Settings{})

	if count := countRegionsOfKind(parsed, RegionStyleAttribute); count != 1 {
		t.Fatalf("style attribute regions = %d in %#v", count, parsed.Regions)
	}
	attribute := firstRegionOfKind(parsed, RegionStyleAttribute)
	if attribute == nil {
		t.Fatal("style attribute region missing")
	}
	if attribute.Start != strings.LastIndex(source, "style=") ||
		source[attribute.ContentStart:attribute.ContentEnd] != "color: <%= color %>" {
		t.Fatalf("style attribute region = %#v, content = %q", attribute, source[attribute.ContentStart:attribute.ContentEnd])
	}
}

func TestParseDocumentClosesASPRegionsAtRawDelimiters(t *testing.T) {
	source := `<%
Response.Write "%>"
Response.Write "done"
%>
<%@ LANGUAGE="JScript" %><% var text = '%>'; Response.Write(text); %>`
	parsed := ParseDocument("file:///delimiters.asp", source, Settings{})
	if len(parsed.Errors) != 0 {
		t.Fatalf("errors = %#v", parsed.Errors)
	}
	blocks := []Region{}
	for _, region := range parsed.Regions {
		if region.Kind == RegionASPBlock || region.Kind == RegionASPDirective {
			blocks = append(blocks, region)
		}
	}
	if len(blocks) != 3 {
		t.Fatalf("blocks = %#v", blocks)
	}
	if blocks[0].End != strings.Index(source, "%>")+len("%>") {
		t.Fatalf("first block end = %d, want first raw delimiter", blocks[0].End)
	}
	firstBlock := source[blocks[0].ContentStart:blocks[0].ContentEnd]
	if !strings.Contains(firstBlock, `Response.Write "`) || strings.Contains(firstBlock, `Response.Write "done"`) {
		t.Fatalf("first block content = %q", firstBlock)
	}
	jsBlock := source[blocks[2].ContentStart:blocks[2].ContentEnd]
	if !strings.Contains(jsBlock, `var text = '`) || strings.Contains(jsBlock, `Response.Write(text)`) {
		t.Fatalf("JScript block content = %q", jsBlock)
	}
	html := htmlText(parsed)
	if !strings.Contains(html, `Response.Write "done"`) || !strings.Contains(html, `Response.Write(text)`) {
		t.Fatalf("HTML after raw delimiters = %q", html)
	}
}

func TestParseDocumentClosesASPRegionsAtCommentLineDelimiters(t *testing.T) {
	source := `<%
' comment with %>
<div>done</div>`
	parsed := ParseDocument("file:///vb-comment-close.asp", source, Settings{})
	if len(parsed.Errors) != 0 {
		t.Fatalf("errors = %#v", parsed.Errors)
	}
	block := firstRegionOfKind(parsed, RegionASPBlock)
	if block == nil {
		t.Fatal("missing ASP block")
	}
	if block.End != strings.Index(source, "%>")+len("%>") {
		t.Fatalf("block end = %d, want first raw delimiter", block.End)
	}
	content := source[block.ContentStart:block.ContentEnd]
	if !strings.Contains(content, "' comment with ") || strings.Contains(content, "<div>") {
		t.Fatalf("block content = %q", content)
	}
	if !strings.Contains(htmlText(parsed), "<div>done</div>") {
		t.Fatalf("HTML after VBScript comment delimiter = %q", htmlText(parsed))
	}
}

func TestParseDocumentClosesScriptAndStyleRegionsAtRawClosingTags(t *testing.T) {
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
	parsed := ParseDocument("file:///tags.asp", source, Settings{})
	script := firstRegionOfKind(parsed, RegionClientScript)
	style := firstRegionOfKind(parsed, RegionStyle)
	if script == nil || script.End != strings.Index(source, "</script>")+len("</script>") {
		t.Fatalf("script region = %#v", script)
	}
	if style == nil || style.End != strings.Index(source, "</style>")+len("</style>") {
		t.Fatalf("style region = %#v", style)
	}
	if strings.Contains(source[script.ContentStart:script.ContentEnd], "const ok = true;") {
		t.Fatalf("script content did not close at raw tag: %q", source[script.ContentStart:script.ContentEnd])
	}
	if strings.Contains(source[style.ContentStart:style.ContentEnd], "color: red") {
		t.Fatalf("style content did not close at raw tag: %q", source[style.ContentStart:style.ContentEnd])
	}
}

func TestParseDocumentRetainsUnclosedScriptAndStyleRegionsToEOF(t *testing.T) {
	testCases := []struct {
		name     string
		source   string
		kind     RegionKind
		language EmbeddedLanguage
		content  string
	}{
		{
			name:     "script",
			source:   `<script>const value = 1;`,
			kind:     RegionClientScript,
			language: LanguageJavaScript,
			content:  "const value = 1;",
		},
		{
			name:     "style",
			source:   `<style>.card { color: red; }`,
			kind:     RegionStyle,
			language: LanguageCSS,
			content:  ".card { color: red; }",
		},
	}
	for _, testCase := range testCases {
		t.Run(testCase.name, func(t *testing.T) {
			parsed := ParseDocument("file:///unclosed-embedded.asp", testCase.source, Settings{})
			if len(parsed.Errors) != 0 {
				t.Fatalf("errors = %#v", parsed.Errors)
			}
			region := firstRegionOfKind(parsed, testCase.kind)
			if region == nil || region.Language != testCase.language || region.End != len(testCase.source) || region.ContentEnd != len(testCase.source) {
				t.Fatalf("embedded region = %#v", region)
			}
			if got := testCase.source[region.ContentStart:region.ContentEnd]; got != testCase.content {
				t.Fatalf("embedded content = %q", got)
			}
		})
	}
}

func TestParseDocumentDoesNotCloseScriptAndStyleRegionsAtASPIslandRawTags(t *testing.T) {
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
	parsed := ParseDocument("file:///asp-island-raw-script-close.asp", source, Settings{})
	if len(parsed.Errors) != 0 {
		t.Fatalf("errors = %#v", parsed.Errors)
	}
	script := firstRegionOfKind(parsed, RegionClientScript)
	style := firstRegionOfKind(parsed, RegionStyle)
	scriptClose := strings.Index(source, "const afterAsp") + strings.Index(source[strings.Index(source, "const afterAsp"):], "</script>") + len("</script>")
	styleClose := strings.Index(source, ".after-asp") + strings.Index(source[strings.Index(source, ".after-asp"):], "</style>") + len("</style>")
	if script == nil || script.End != scriptClose {
		t.Fatalf("script region = %#v", script)
	}
	if style == nil || style.End != styleClose {
		t.Fatalf("style region = %#v", style)
	}
	if !strings.Contains(source[script.ContentStart:script.ContentEnd], "const afterAsp = true;") {
		t.Fatalf("script content = %q", source[script.ContentStart:script.ContentEnd])
	}
	if !strings.Contains(source[style.ContentStart:style.ContentEnd], ".after-asp { color: red; }") {
		t.Fatalf("style content = %q", source[style.ContentStart:style.ContentEnd])
	}
	if countRegionsOfKind(parsed, RegionASPBlock) != 4 {
		t.Fatalf("ASP block regions = %#v", parsed.Regions)
	}
}

func TestParseDocumentServerScriptLanguageOverridesPageDefault(t *testing.T) {
	source := `<%@ LANGUAGE="JScript" %><script runat="server" language="VBScript">Dim value</script>`
	parsed := ParseDocument("file:///mixed.asp", source, Settings{})
	serverScript := firstRegionOfKind(parsed, RegionServerScript)
	if serverScript == nil || serverScript.Language != LanguageVBScript {
		t.Fatalf("server script region = %#v", serverScript)
	}
}

func TestParseDocumentServerScriptTypeJScriptOverridesVBScriptDefault(t *testing.T) {
	source := `<script runat="server" type="JScript">
function fromJScriptTag() {}
</script>`
	parsed := ParseDocument("file:///server-jscript-type.asp", source, Settings{DefaultLanguage: "VBScript"})
	serverScript := firstRegionOfKind(parsed, RegionServerScript)
	if serverScript == nil || serverScript.Language != LanguageJScript {
		t.Fatalf("server script region = %#v", serverScript)
	}
}

func TestParseDocumentClosesServerJScriptRegionAtRawScriptEndTag(t *testing.T) {
	source := `<%@ LANGUAGE="JScript" %>
<script runat="server" language="JScript">
function render() {
  var literal = "</script>";
  Response.Write(literal);
}
</script>`
	parsed := ParseDocument("file:///server-jscript-script-close.asp", source, Settings{})
	if len(parsed.Errors) != 0 {
		t.Fatalf("errors = %#v", parsed.Errors)
	}
	serverScript := firstRegionOfKind(parsed, RegionServerScript)
	if serverScript == nil || serverScript.Language != LanguageJScript {
		t.Fatalf("server script region = %#v", serverScript)
	}
	if serverScript.End != strings.Index(source, "</script>")+len("</script>") {
		t.Fatalf("server script end = %d, want raw script close", serverScript.End)
	}
	content := source[serverScript.ContentStart:serverScript.ContentEnd]
	if !strings.Contains(content, `var literal = "`) || strings.Contains(content, "Response.Write(literal)") {
		t.Fatalf("server script content = %q", content)
	}
}

func firstRegionOfKind(parsed *ParsedDocument, kind RegionKind) *Region {
	for i := range parsed.Regions {
		if parsed.Regions[i].Kind == kind {
			return &parsed.Regions[i]
		}
	}
	return nil
}

func countRegionsOfKind(parsed *ParsedDocument, kind RegionKind) int {
	count := 0
	for _, region := range parsed.Regions {
		if region.Kind == kind {
			count++
		}
	}
	return count
}

func TestNewASPOpenScanFeaturesMatchesLowercaseOracle(t *testing.T) {
	texts := []string{
		"",
		"plain text without tags",
		"<html><body><%= value %></body></html>",
		"<SCRIPT>const ready=true;</SCRIPT>",
		"<ScRiPt src=\"app.js\"></sCrIpT>",
		"<TEXTAREA>code sample</textarea>",
		"<Title>mixed</TITLE><XMP>x</xmp>",
		"<IFRAME src=\"x\"></iframe><NoEmbed></noembed>",
		"<NoFrames></noframes><PLAINTEXT>rest",
		"<!-- comment -->",
		"// line comment /* block */",
		"a < b and <% code %> with <scripts leftover",
		"<scriptx><titley>",
		"日本語 <SCRIPT>テスト</SCRIPT> テキスト",
		"<\n<script\tdefer>",
	}
	for _, text := range texts {
		lower := strings.ToLower(text)
		want := aspOpenScanFeatures{
			hasHTMLComments:    strings.Contains(text, "<!--"),
			hasRawTextElements: strings.Contains(lower, "<textarea") || strings.Contains(lower, "<title") || strings.Contains(lower, "<xmp") || strings.Contains(lower, "<iframe") || strings.Contains(lower, "<noembed") || strings.Contains(lower, "<noframes") || strings.Contains(lower, "<plaintext"),
			hasScriptElements:  strings.Contains(lower, "<script"),
			hasClientComments:  strings.Contains(text, "//") || strings.Contains(text, "/*"),
		}
		if got := newASPOpenScanFeatures(text); got != want {
			t.Fatalf("newASPOpenScanFeatures(%q) = %#v, want %#v", text, got, want)
		}
	}
}

func TestIndexASCIIFoldMatchesCaseInsensitiveContains(t *testing.T) {
	texts := []string{
		"",
		"plain ASP document body",
		"<!-- #INCLUDE file=\"inc/header.inc\" -->",
		"<SCRIPT>const ready=true;</SCRIPT>",
		"object <OBJECT id=\"x\"> tail",
		"日本語テキスト #Include テスト",
		"abc",
	}
	needles := []string{"", "#include", "<script", "<object", "INCLUDE", "ABC", "abcd", "日本語", "テキスト"}
	for _, text := range texts {
		for _, needle := range needles {
			want := strings.Index(strings.ToLower(text), strings.ToLower(needle))
			if got := indexASCIIFold(text, needle); got != want {
				t.Fatalf("indexASCIIFold(%q, %q) = %d, want %d", text, needle, got, want)
			}
			if got, want := ContainsASCIIFold(text, needle), want >= 0; got != want {
				t.Fatalf("ContainsASCIIFold(%q, %q) = %v, want %v", text, needle, got, want)
			}
		}
	}
}

func htmlText(parsed *ParsedDocument) string {
	var builder strings.Builder
	for _, region := range parsed.Regions {
		if region.Kind == RegionHTML {
			builder.WriteString(parsed.Text[region.Start:region.End])
		}
	}
	return builder.String()
}

func TestLineQuoteScannerMatchesLineRescan(t *testing.T) {
	text := "a \"b<%c\" 'd\\'e' `f<%g`\r\nh \"i\n<% j = \"k\" %>'l\rm \"n\" <%o"
	scanner := &lineQuoteScanner{text: text}
	offsets := make([]int, 0, 2*len(text)+1)
	for offset := 0; offset <= len(text); offset++ {
		offsets = append(offsets, offset)
	}
	// Revisit earlier offsets to exercise the restart path.
	for offset := len(text); offset >= 0; offset -= 3 {
		offsets = append(offsets, offset)
	}
	for _, offset := range offsets {
		wantQuote, wantOK := activeQuoteOnLine(text, offset)
		gotQuote, gotOK := scanner.activeQuote(offset)
		if gotQuote != wantQuote || gotOK != wantOK {
			t.Fatalf("offset %d: got (%q, %v), want (%q, %v)", offset, gotQuote, gotOK, wantQuote, wantOK)
		}
	}
}

func TestParseDocumentLongSingleLineStaysLinear(t *testing.T) {
	text := strings.Repeat("<% a = 1 %>", 20000)
	parsed := ParseDocument("file:///long.asp", text, Settings{DefaultLanguage: "VBScript"})
	if len(parsed.Regions) != 20000 {
		t.Fatalf("regions = %d, want 20000", len(parsed.Regions))
	}
}
