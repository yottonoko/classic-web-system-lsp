package lspserver

import (
	"encoding/json"
	"os"
	"path/filepath"
	"reflect"
	"sort"
	"strings"
	"testing"
)

type formatterSettingCase struct {
	contains    []string
	covers      []string
	equals      *string
	format      map[string]any
	notContains []string
	request     string
	source      string
	uriPath     string
}

func TestStdioParityAppliesEveryContributedFormatterSettingThroughLSPFormatting(t *testing.T) {
	cases := []formatterSettingCase{
		{
			uriPath:  "go-format-setting-indent-size.asp",
			covers:   []string{"indentSize"},
			format:   map[string]any{"indentSize": 3},
			source:   `<div><section><p>x</p></section></div>`,
			contains: []string{"\n   <section>", "\n      <p>x</p>"},
		},
		{
			uriPath:  "go-format-setting-indent-style.asp",
			covers:   []string{"indentStyle"},
			format:   map[string]any{"indentSize": 2, "indentStyle": "tab"},
			source:   `<div><section><p>x</p></section></div>`,
			contains: []string{"\n\t<section>", "\n\t\t<p>x</p>"},
		},
		{
			uriPath: "go-format-setting-eol.asp",
			covers:  []string{"endOfLine"},
			format:  map[string]any{"endOfLine": "crlf", "indentSize": 2},
			source: `<div>
<span>x</span>
</div>`,
			contains: []string{"\r\n  <span>x</span>\r\n"},
		},
		{
			uriPath:  "go-format-setting-final-newline.asp",
			covers:   []string{"insertFinalNewline"},
			format:   map[string]any{"insertFinalNewline": true, "indentSize": 2},
			source:   `<div><span>x</span></div>`,
			contains: []string{"</div>\n"},
		},
		{
			uriPath: "go-format-setting-preserve-empty-lines.asp",
			covers:  []string{"preserveNewLines", "maxPreserveNewLines", "indentEmptyLines"},
			format: map[string]any{
				"indentEmptyLines":    true,
				"indentSize":          2,
				"maxPreserveNewLines": 1,
				"preserveNewLines":    true,
			},
			source: `<div>


<span>x</span>
</div>`,
			contains:    []string{"<div>\n\n  <span>x</span>\n</div>"},
			notContains: []string{"\n  \n", "\n\n\n"},
		},
		{
			uriPath:     "go-format-setting-enabled-languages.asp",
			covers:      []string{"enabledLanguages"},
			format:      map[string]any{"enabledLanguages": []string{"css"}, "indentSize": 2},
			source:      `<div><style>.x{color:red}</style><%if enabled then%></div>`,
			contains:    []string{".x {\n  color: red", "<%if enabled then%>"},
			notContains: []string{"\n  <style>"},
		},
		{
			uriPath: "go-format-setting-embedded-off.asp",
			covers:  []string{"embeddedLanguageFormatting"},
			format:  map[string]any{"embeddedLanguageFormatting": "off", "indentSize": 2},
			source:  `<div><style>.x{color:red}</style><script>function greet(){console.log("x");}</script><%if enabled then%></div>`,
			contains: []string{
				".x{color:red}",
				`function greet(){console.log("x");}`,
				"<% if enabled then %>",
			},
		},
		{
			uriPath: "go-format-setting-disable-regions.asp",
			covers:  []string{"respectDisableRegions"},
			format:  map[string]any{"indentSize": 2, "respectDisableRegions": false},
			source: `<%
' asp-format off
If enabled Then
Response.Write "ok"
End If
' asp-format on
%>`,
			contains: []string{`  If enabled Then
    Response.Write "ok"
  End If`},
		},
		{
			uriPath:  "go-format-setting-html-indent-size.asp",
			covers:   []string{"htmlIndentSize"},
			format:   map[string]any{"htmlIndentSize": 3, "indentSize": 2},
			source:   `<div><section><p>x</p></section></div>`,
			contains: []string{"\n   <section>", "\n      <p>x</p>"},
		},
		{
			uriPath:  "go-format-setting-html-indent-style.asp",
			covers:   []string{"htmlIndentStyle"},
			format:   map[string]any{"htmlIndentStyle": "tab", "indentSize": 2},
			source:   `<div><section><p>x</p></section></div>`,
			contains: []string{"\n\t<section>", "\n\t\t<p>x</p>"},
		},
		{
			uriPath:  "go-format-setting-print-width.asp",
			covers:   []string{"printWidth"},
			format:   map[string]any{"indentSize": 2, "printWidth": 20},
			source:   `<div class="a" id="b" data-x="c"></div>`,
			contains: []string{`<div class="a"` + "\n  " + `id="b" data-x="c">`},
		},
		{
			uriPath:  "go-format-setting-html-wrap-line.asp",
			covers:   []string{"htmlWrapLineLength"},
			format:   map[string]any{"htmlWrapLineLength": 20, "indentSize": 2},
			source:   `<div class="a" id="b" data-x="c"></div>`,
			contains: []string{`<div class="a"` + "\n  " + `id="b" data-x="c">`},
		},
		{
			uriPath: "go-format-setting-html-wrap-attributes.asp",
			covers:  []string{"htmlWrapAttributes", "htmlWrapAttributesIndentSize"},
			format: map[string]any{
				"htmlWrapAttributes":           "force",
				"htmlWrapAttributesIndentSize": 6,
				"indentSize":                   2,
			},
			source:   `<div class="a" id="b" data-x="c"></div>`,
			contains: []string{`<div class="a"` + "\n      " + `id="b"` + "\n      " + `data-x="c">`},
		},
		{
			uriPath:  "go-format-setting-html-inner.asp",
			covers:   []string{"htmlIndentInnerHtml"},
			format:   map[string]any{"htmlIndentInnerHtml": true, "indentSize": 2},
			source:   `<html><body><main><p>x</p></main></body></html>`,
			contains: []string{"<html>\n\n  <body>\n    <main>"},
		},
		{
			uriPath:  "go-format-setting-html-unformatted.asp",
			covers:   []string{"htmlUnformatted"},
			format:   map[string]any{"htmlUnformatted": "custom", "indentSize": 2},
			source:   `<div><custom><span>x</span></custom></div>`,
			contains: []string{"<custom><span>x</span></custom>"},
		},
		{
			uriPath:  "go-format-setting-html-content-unformatted.asp",
			covers:   []string{"htmlContentUnformatted"},
			format:   map[string]any{"htmlContentUnformatted": "custom", "indentSize": 2},
			source:   `<div><custom><span>x</span></custom></div>`,
			contains: []string{"<custom><span>x</span></custom>"},
		},
		{
			uriPath:  "go-format-setting-html-extra-liners.asp",
			covers:   []string{"htmlExtraLiners"},
			format:   map[string]any{"htmlExtraLiners": "body", "indentSize": 2},
			source:   `<html><head></head><body></body></html>`,
			contains: []string{"<head></head>\n\n<body></body>"},
		},
		{
			uriPath:  "go-format-setting-css-indent-size.asp",
			covers:   []string{"cssIndentSize"},
			format:   map[string]any{"cssIndentSize": 3, "indentSize": 2},
			source:   `<style>.x{color:red}</style><%Response.Write ""%>`,
			contains: []string{"<style>\n  .x {\n     color: red"},
		},
		{
			uriPath:  "go-format-setting-css-indent-style.asp",
			covers:   []string{"cssIndentStyle"},
			format:   map[string]any{"cssIndentStyle": "tab", "indentSize": 2},
			source:   `<style>.x{color:red}</style><%Response.Write ""%>`,
			contains: []string{"<style>\n  .x {\n  \tcolor: red"},
		},
		{
			uriPath:  "go-format-setting-css-wrap-line.asp",
			covers:   []string{"cssWrapLineLength"},
			format:   map[string]any{"cssWrapLineLength": 20, "indentSize": 2},
			source:   `<style>.x{box-shadow:0 0 0 1px red, 0 0 0 2px blue}</style><%Response.Write ""%>`,
			contains: []string{"box-shadow: 0 0 0 1px red, 0 0 0 2px blue"},
		},
		{
			uriPath:     "go-format-setting-css-rules.asp",
			covers:      []string{"cssNewlineBetweenRules"},
			format:      map[string]any{"cssNewlineBetweenRules": false, "indentSize": 2},
			source:      `<style>.a{color:red}.b{color:blue}</style><%Response.Write ""%>`,
			contains:    []string{"color: red\n  }\n  .b {"},
			notContains: []string{"color: red\n  }\n\n  .b {"},
		},
		{
			uriPath: "go-format-setting-css-selectors.asp",
			covers:  []string{"cssNewlineBetweenSelectors", "cssSpaceAroundSelectorSeparator"},
			format: map[string]any{
				"cssNewlineBetweenSelectors":      false,
				"cssSpaceAroundSelectorSeparator": true,
				"indentSize":                      2,
			},
			source:      `<style>.a,.b{color:red}</style><%Response.Write ""%>`,
			contains:    []string{".a, .b {"},
			notContains: []string{".a,\n  .b {"},
		},
		{
			uriPath:  "go-format-setting-css-brace-style.asp",
			covers:   []string{"cssBraceStyle"},
			format:   map[string]any{"cssBraceStyle": "expand", "indentSize": 2},
			source:   `<style>.x{color:red}</style><%Response.Write ""%>`,
			contains: []string{".x\n  {"},
		},
		{
			uriPath: "go-format-setting-js-indent-size.asp",
			covers:  []string{"javascriptIndentSize"},
			format:  map[string]any{"indentSize": 2, "javascriptIndentSize": 3},
			source: `<script>
if (x) {
foo();
}
</script><%Response.Write ""%>`,
			contains: []string{"\n     foo();"},
		},
		{
			uriPath: "go-format-setting-js-indent-style.asp",
			covers:  []string{"javascriptIndentStyle"},
			format:  map[string]any{"indentSize": 2, "javascriptIndentStyle": "tab"},
			source: `<script>
if (x) {
foo();
}
</script><%Response.Write ""%>`,
			contains: []string{"\n  \tfoo();"},
		},
		{
			uriPath: "go-format-setting-jscript-indent-size.asp",
			covers:  []string{"jscriptIndentSize"},
			format:  map[string]any{"indentSize": 2, "jscriptIndentSize": 3},
			source: `<%@ LANGUAGE="JScript" %>
<%
if (x) {
foo();
}
%>`,
			contains: []string{"\n   foo();"},
		},
		{
			uriPath: "go-format-setting-jscript-indent-style.asp",
			covers:  []string{"jscriptIndentStyle"},
			format:  map[string]any{"indentSize": 2, "jscriptIndentStyle": "tab"},
			source: `<%@ LANGUAGE="JScript" %>
<%
if (x) {
foo();
}
%>`,
			contains: []string{"\n\tfoo();"},
		},
		{
			uriPath:  "go-format-setting-js-semi.asp",
			covers:   []string{"javascriptSemicolons"},
			format:   map[string]any{"indentSize": 2, "javascriptSemicolons": "insert"},
			source:   `<script>const a = 1</script><%Response.Write ""%>`,
			contains: []string{"const a = 1;"},
		},
		{
			uriPath: "go-format-setting-js-switch.asp",
			covers:  []string{"javascriptIndentSwitchCase"},
			format:  map[string]any{"indentSize": 2, "javascriptIndentSwitchCase": true},
			source: `<script>
switch(x){
case 1:
foo();
break;
}
</script><%Response.Write ""%>`,
			contains: []string{"\n    case 1:\n      foo();"},
		},
		{
			uriPath: "go-format-setting-js-function-brace.asp",
			covers:  []string{"javascriptPlaceOpenBraceOnNewLineForFunctions"},
			format:  map[string]any{"indentSize": 2, "javascriptPlaceOpenBraceOnNewLineForFunctions": true},
			source: `<script>
function f() {
return 1;
}
</script><%Response.Write ""%>`,
			contains: []string{"function f()\n  {"},
		},
		{
			uriPath: "go-format-setting-js-control-brace.asp",
			covers:  []string{"javascriptPlaceOpenBraceOnNewLineForControlBlocks"},
			format:  map[string]any{"indentSize": 2, "javascriptPlaceOpenBraceOnNewLineForControlBlocks": true},
			source: `<script>
if (x) {
foo();
}
</script><%Response.Write ""%>`,
			contains: []string{"if (x)\n  {"},
		},
		{
			uriPath:  "go-format-setting-js-comma.asp",
			covers:   []string{"javascriptInsertSpaceAfterCommaDelimiter"},
			format:   map[string]any{"indentSize": 2, "javascriptInsertSpaceAfterCommaDelimiter": false},
			source:   `<script>foo(a,b);</script><%Response.Write ""%>`,
			contains: []string{"foo(a,b);"},
		},
		{
			uriPath:  "go-format-setting-js-for-semi.asp",
			covers:   []string{"javascriptInsertSpaceAfterSemicolonInForStatements"},
			format:   map[string]any{"indentSize": 2, "javascriptInsertSpaceAfterSemicolonInForStatements": false},
			source:   `<script>for(let i=0;i<1;i++){foo();}</script><%Response.Write ""%>`,
			contains: []string{"for (let i = 0;i < 1;i++)"},
		},
		{
			uriPath:  "go-format-setting-js-binary.asp",
			covers:   []string{"javascriptInsertSpaceBeforeAndAfterBinaryOperators"},
			format:   map[string]any{"indentSize": 2, "javascriptInsertSpaceBeforeAndAfterBinaryOperators": false},
			source:   `<script>const a = x + 1;</script><%Response.Write ""%>`,
			contains: []string{"const a=x+1;"},
		},
		{
			uriPath: "go-format-setting-js-keyword.asp",
			covers:  []string{"javascriptInsertSpaceAfterKeywordsInControlFlowStatements"},
			format: map[string]any{
				"indentSize": 2,
				"javascriptInsertSpaceAfterKeywordsInControlFlowStatements": true,
			},
			source: `<script>
if (x) {
foo();
}
</script><%Response.Write ""%>`,
			contains: []string{"if (x) {"},
		},
		{
			uriPath: "go-format-setting-js-anonymous-function.asp",
			covers:  []string{"javascriptInsertSpaceAfterFunctionKeywordForAnonymousFunctions"},
			format: map[string]any{
				"indentSize": 2,
				"javascriptInsertSpaceAfterFunctionKeywordForAnonymousFunctions": true,
			},
			source:   `<script>const f=function(){return 1;}</script><%Response.Write ""%>`,
			contains: []string{"const f = function ()"},
		},
		{
			uriPath: "go-format-setting-js-parenthesis.asp",
			covers:  []string{"javascriptInsertSpaceAfterOpeningAndBeforeClosingNonemptyParenthesis"},
			format: map[string]any{
				"indentSize": 2,
				"javascriptInsertSpaceAfterOpeningAndBeforeClosingNonemptyParenthesis": true,
			},
			source:   `<script>foo(a);</script><%Response.Write ""%>`,
			contains: []string{"foo( a );"},
		},
		{
			uriPath: "go-format-setting-js-brackets.asp",
			covers:  []string{"javascriptInsertSpaceAfterOpeningAndBeforeClosingNonemptyBrackets"},
			format: map[string]any{
				"indentSize": 2,
				"javascriptInsertSpaceAfterOpeningAndBeforeClosingNonemptyBrackets": true,
			},
			source:   `<script>const x=[1];</script><%Response.Write ""%>`,
			contains: []string{"const x = [ 1 ];"},
		},
		{
			uriPath: "go-format-setting-js-braces.asp",
			covers:  []string{"javascriptInsertSpaceAfterOpeningAndBeforeClosingNonemptyBraces"},
			format: map[string]any{
				"indentSize": 2,
				"javascriptInsertSpaceAfterOpeningAndBeforeClosingNonemptyBraces": true,
			},
			source:   `<script>const x={a:1};</script><%Response.Write ""%>`,
			contains: []string{"const x = { a: 1 };"},
		},
		{
			uriPath: "go-format-setting-js-empty-braces.asp",
			covers:  []string{"javascriptInsertSpaceAfterOpeningAndBeforeClosingEmptyBraces"},
			format: map[string]any{
				"indentSize": 2,
				"javascriptInsertSpaceAfterOpeningAndBeforeClosingEmptyBraces": true,
			},
			source:   `<script>function f(){}</script><%Response.Write ""%>`,
			contains: []string{"function f() { }"},
		},
		{
			uriPath:  "go-format-setting-js-function-paren.asp",
			covers:   []string{"javascriptInsertSpaceBeforeFunctionParenthesis"},
			format:   map[string]any{"indentSize": 2, "javascriptInsertSpaceBeforeFunctionParenthesis": true},
			source:   `<script>function f(){return 1;}</script><%Response.Write ""%>`,
			contains: []string{"function f ()"},
		},
		{
			uriPath: "go-format-setting-vbs-indent-size.asp",
			covers:  []string{"vbscriptIndentSize"},
			format:  map[string]any{"indentSize": 4, "vbscriptIndentSize": 2},
			source: `<%
If enabled Then
Response.Write "ok"
End If
%>`,
			contains: []string{`  If enabled Then
    Response.Write "ok"`},
		},
		{
			uriPath: "go-format-setting-vbs-indent-style.asp",
			covers:  []string{"vbscriptIndentStyle"},
			format:  map[string]any{"indentSize": 2, "vbscriptIndentStyle": "tab"},
			source: `<%
If enabled Then
Response.Write "ok"
End If
%>`,
			contains: []string{"\tIf enabled Then\n\t\tResponse.Write \"ok\""},
		},
		{
			uriPath: "go-format-setting-vbs-keyword-case.asp",
			covers:  []string{"vbscriptKeywordCase"},
			format:  map[string]any{"indentSize": 2, "vbscriptKeywordCase": "lower"},
			source: `<%
IF enabled THEN
Response.Write "ok"
END IF
%>`,
			contains: []string{`  if enabled then
    Response.Write "ok"
  end if`},
		},
		{
			uriPath: "go-format-setting-vbs-continuation.asp",
			covers:  []string{"vbscriptLineContinuationIndentSize"},
			format:  map[string]any{"indentSize": 2, "vbscriptLineContinuationIndentSize": 6},
			source: `<%
a = _
"aaa"
%>`,
			contains: []string{`  a = _
        "aaa"`},
		},
		{
			uriPath: "go-format-setting-vbs-select.asp",
			covers:  []string{"vbscriptSelectCaseIndent"},
			format:  map[string]any{"indentSize": 2, "vbscriptSelectCaseIndent": "caseAligned"},
			source: `<%
Select Case kind
Case "a"
Response.Write "a"
End Select
%>`,
			contains: []string{`  Select Case kind
  Case "a"
    Response.Write "a"`},
		},
		{
			uriPath:  "go-format-setting-vbs-uppercase-legacy.asp",
			covers:   []string{"uppercaseKeywords"},
			format:   map[string]any{"indentSize": 2, "uppercaseKeywords": true},
			source:   `<%if enabled then%>`,
			contains: []string{"<% IF enabled THEN %>"},
		},
		{
			uriPath:  "go-format-setting-vbs-align.asp",
			covers:   []string{"alignAssignments"},
			format:   map[string]any{"alignAssignments": true, "indentSize": 2},
			source:   "<%\nfirst=1\nlongerName=2\n%>",
			contains: []string{"  first      = 1\n  longerName = 2"},
		},
		{
			uriPath: "go-format-setting-vbs-block-indent.asp",
			covers:  []string{"vbscriptBlockIndent"},
			format:  map[string]any{"indentSize": 2, "vbscriptBlockIndent": "alignWithDelimiter"},
			source: `<%
If enabled Then
Response.Write "ok"
End If
%>`,
			contains: []string{`<%
If enabled Then
  Response.Write "ok"`},
		},
		{
			uriPath: "go-format-setting-vbs-tag-indent-mode.asp",
			covers:  []string{"vbscriptTagIndentMode"},
			format:  map[string]any{"indentSize": 2, "vbscriptTagIndentMode": "ignoreTag"},
			source: `<div>
  <%
If enabled Then
Response.Write "ok"
End If
  %>
</div>`,
			contains: []string{"  <%\n  If enabled Then\n    Response.Write \"ok\"\n  End If\n%>"},
		},
		{
			uriPath:  "go-format-setting-css-tag-indent-mode.asp",
			covers:   []string{"cssTagIndentMode"},
			format:   map[string]any{"cssTagIndentMode": "ignoreTag", "indentSize": 2},
			source:   "<div>\n  <style>.x{color:red}</style>\n  <%Response.Write \"\"%>\n</div>",
			contains: []string{"  <style>\n.x {\n  color: red"},
		},
		{
			uriPath: "go-format-setting-js-tag-indent-mode.asp",
			covers:  []string{"javascriptTagIndentMode"},
			format:  map[string]any{"indentSize": 2, "javascriptTagIndentMode": "ignoreTag"},
			source: `<div>
  <script>
if (x) {
foo();
}
  </script>
  <%Response.Write ""%>
</div>`,
			contains: []string{"  <script>\nif (x) {\n  foo();"},
		},
		{
			uriPath:  "go-format-setting-asp-delimiter.asp",
			covers:   []string{"aspDelimiterSpacing"},
			format:   map[string]any{"aspDelimiterSpacing": "compact", "indentSize": 2},
			source:   "<%= title %>\n<%if enabled then%>",
			contains: []string{"<%=title%>\n<%if enabled then%>"},
		},
		{
			uriPath:  "go-format-setting-asp-newline.asp",
			covers:   []string{"aspBlockNewline"},
			format:   map[string]any{"aspBlockNewline": "alwaysMultiline", "indentSize": 2},
			source:   `<% Response.Write "ok" %>`,
			contains: []string{"<%\n  Response.Write \"ok\"\n%>"},
		},
		{
			uriPath:  "go-format-setting-nested-asp.asp",
			covers:   []string{"nestedAspInCssJs"},
			format:   map[string]any{"indentSize": 2, "nestedAspInCssJs": "protectAspOnly"},
			source:   `<style>.x{color:<%= color %>}</style>`,
			contains: []string{"<style>\n  .x {\n    color: <%= color %>"},
		},
		{
			uriPath: "go-format-setting-fragment.asp",
			covers:  []string{"fragmentMode"},
			format:  map[string]any{"fragmentMode": "fragment", "indentSize": 2},
			source: `<div><span>one</span></div>
<section><p>two</p></section>`,
			contains:    []string{"<section>\n  <p>two</p>\n</section>"},
			notContains: []string{"asp-lsp-fragment"},
		},
		{
			uriPath: "go-format-setting-ignore-vbs.asp",
			covers:  []string{"ignoreVbscriptTagIndent"},
			format:  map[string]any{"ignoreVbscriptTagIndent": true, "indentSize": 2},
			source: `<div>
  <%
If enabled Then
Response.Write "ok"
End If
  %>
</div>`,
			contains: []string{"  <%\n  If enabled Then\n    Response.Write \"ok\"\n  End If\n%>"},
		},
		{
			uriPath:  "go-format-setting-ignore-css.asp",
			covers:   []string{"ignoreCssTagIndent"},
			format:   map[string]any{"ignoreCssTagIndent": true, "indentSize": 2},
			source:   "<div>\n  <style>.x{color:red}</style>\n  <%Response.Write \"\"%>\n</div>",
			contains: []string{"  <style>\n.x {\n  color: red"},
		},
		{
			uriPath: "go-format-setting-ignore-js.asp",
			covers:  []string{"ignoreJavaScriptTagIndent"},
			format:  map[string]any{"ignoreJavaScriptTagIndent": true, "indentSize": 2},
			source: `<div>
  <script>
if (x) {
foo();
}
  </script>
  <%Response.Write ""%>
</div>`,
			contains: []string{"  <script>\nif (x) {\n  foo();"},
		},
		{
			uriPath:  "go-format-setting-on-save.asp",
			covers:   []string{"onSave"},
			format:   map[string]any{"indentSize": 2, "onSave": true},
			request:  "willSave",
			source:   "<%\nIf enabled Then\nResponse.Write \"ok\"\nEnd If\n%>",
			contains: []string{"  Response.Write"},
		},
	}
	coveredSettings := coveredFormatterSettings(cases)
	formatSettings := contributedFormatterSettings(t)
	if !reflect.DeepEqual(coveredSettings, formatSettings) {
		t.Fatalf("covered formatter settings mismatch\ncovered: %v\ncontributed: %v", coveredSettings, formatSettings)
	}

	root := t.TempDir()

	for index, testCase := range cases {
		func() {
			client := startStdioTestClient(t)
			defer client.close()
			client.request("initialize", map[string]any{
				"processId":    nil,
				"rootUri":      pathToFileURI(root),
				"capabilities": map[string]any{},
			})
			notifyConfiguration(t, client, map[string]any{"aspLsp": map[string]any{"format": testCase.format}})
			uri := pathToFileURI(filepath.Join(root, testCase.uriPath))
			if err := client.notify("textDocument/didOpen", map[string]any{
				"textDocument": map[string]any{
					"uri":        uri,
					"languageId": "classic-asp",
					"version":    index + 1,
					"text":       testCase.source,
				},
			}); err != nil {
				t.Fatal(err)
			}
			client.waitForNotification("textDocument/publishDiagnostics", "")

			var formatted string
			if testCase.request == "willSave" {
				result := client.request("textDocument/willSaveWaitUntil", map[string]any{
					"textDocument": map[string]any{"uri": uri},
					"reason":       1,
				})
				formatted = mustJSONText(t, result.Result)
			} else {
				edits := textEdits(t, client.request("textDocument/formatting", map[string]any{
					"textDocument": map[string]any{"uri": uri},
					"options":      map[string]any{"tabSize": 2, "insertSpaces": true},
				}).Result)
				formatted = firstEditText(edits, testCase.source)
			}

			if testCase.equals != nil && formatted != *testCase.equals {
				t.Fatalf("%s formatted text mismatch\ngot:\n%s\nwant:\n%s", testCase.uriPath, formatted, *testCase.equals)
			}
			for _, expected := range testCase.contains {
				if !strings.Contains(formatted, expected) {
					t.Fatalf("%s formatted text missing %q:\n%s", testCase.uriPath, expected, formatted)
				}
			}
			for _, unexpected := range testCase.notContains {
				if strings.Contains(formatted, unexpected) {
					t.Fatalf("%s formatted text unexpectedly contains %q:\n%s", testCase.uriPath, unexpected, formatted)
				}
			}
		}()
	}
}

func contributedFormatterSettings(t *testing.T) []string {
	t.Helper()
	data, err := os.ReadFile(filepath.Join(repoRoot(t), "apps", "vscode", "package.json"))
	if err != nil {
		t.Fatal(err)
	}
	var pkg struct {
		Contributes struct {
			Configuration struct {
				Properties map[string]json.RawMessage `json:"properties"`
			} `json:"configuration"`
		} `json:"contributes"`
	}
	if err := json.Unmarshal(data, &pkg); err != nil {
		t.Fatal(err)
	}
	var settings []string
	for key := range pkg.Contributes.Configuration.Properties {
		if strings.HasPrefix(key, "aspLsp.format.") {
			settings = append(settings, strings.TrimPrefix(key, "aspLsp.format."))
		}
	}
	sort.Strings(settings)
	return settings
}

func coveredFormatterSettings(cases []formatterSettingCase) []string {
	seen := make(map[string]bool)
	for _, testCase := range cases {
		for _, setting := range testCase.covers {
			seen[setting] = true
		}
	}
	settings := make([]string, 0, len(seen))
	for setting := range seen {
		settings = append(settings, setting)
	}
	sort.Strings(settings)
	return settings
}
