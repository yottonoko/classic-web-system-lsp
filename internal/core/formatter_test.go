package core

import (
	"strings"
	"testing"

	"github.com/yottonoko/classic-web-system-lsp/internal/lsp"
)

func TestFormatDocumentKeepsASPDelimiters(t *testing.T) {
	parsed := ParseDocument("file:///format.asp", "<html><body><%if a=1 then%></body></html>", Settings{})
	edits := FormatDocument(parsed, FormattingOptions{TabSize: 2, InsertSpaces: true})
	if len(edits) != 1 {
		t.Fatalf("edits = %#v", edits)
	}
	if edits[0].NewText != "<html><body><% if a = 1 then %></body></html>" {
		t.Fatalf("formatted = %q", edits[0].NewText)
	}
}

func TestFormatDocumentDelegatesEmbeddedRegions(t *testing.T) {
	parsed := ParseDocument("file:///format.asp", `<style>.x{color:red;}</style><script>if(a){b();}</script><%if(a){b();}%>`, Settings{DefaultLanguage: "JScript"})
	edits := FormatDocument(parsed, FormattingOptions{
		TabSize:      2,
		InsertSpaces: true,
		FormatCSS: func(source string, _ FormattingOptions) (string, error) {
			if source != ".x{color:red;}" {
				t.Fatalf("css callback source = %q", source)
			}
			return ".x { color: red; }", nil
		},
		FormatJavaScript: func(source string, _ FormattingOptions, _ EmbeddedLanguage) (string, error) {
			switch source {
			case "if(a){b();}":
				return "if (a) {\n  b();\n}", nil
			default:
				t.Fatalf("js callback source = %q", source)
				return source, nil
			}
		},
	})
	if len(edits) != 1 {
		t.Fatalf("edits = %#v", edits)
	}
	if edits[0].NewText != "<style>.x { color: red; }</style><script>if (a) {\n  b();\n}</script><%\nif (a) {\n  b();\n}\n%>" {
		t.Fatalf("formatted = %q", edits[0].NewText)
	}
}

func TestFormatDocumentRemovesFormatterIndentationFromBlankLines(t *testing.T) {
	tests := []struct {
		name    string
		source  string
		options FormattingOptions
	}{
		{
			name:   "html",
			source: "<div>x</div>",
			options: FormattingOptions{FormatHTML: func(string, FormattingOptions) (string, error) {
				return "<div>\n  \n\tx\n</div>", nil
			}},
		},
		{
			name:   "css",
			source: "<style>.x{color:red}</style>",
			options: FormattingOptions{FormatCSS: func(string, FormattingOptions) (string, error) {
				return ".x {\n  \n  color: red;\n}", nil
			}},
		},
		{
			name:   "javascript",
			source: "<script>const x=1;</script>",
			options: FormattingOptions{FormatJavaScript: func(string, FormattingOptions, EmbeddedLanguage) (string, error) {
				return "const x = 1;\n\t", nil
			}},
		},
		{
			name:   "vbscript",
			source: "<%\nIf ready Then\n    \nResponse.Write ready\nEnd If\n%>",
		},
	}

	for _, test := range tests {
		t.Run(test.name, func(t *testing.T) {
			test.options.TabSize = 2
			test.options.InsertSpaces = true
			edits := FormatDocument(ParseDocument("file:///blank-lines.asp", test.source, Settings{}), test.options)
			if len(edits) != 1 {
				t.Fatalf("edits = %#v", edits)
			}
			for _, line := range strings.Split(edits[0].NewText, "\n") {
				if line != "" && strings.Trim(line, " \t") == "" {
					t.Fatalf("formatted output contains whitespace-only line: %q", edits[0].NewText)
				}
			}
		})
	}
}

func TestFinalizeFormattedTextRemovesBlankLineWhitespaceAndPreservesCRLF(t *testing.T) {
	formatted := finalizeFormattedText("one\r\n  \r\n\t\r\ntwo", "one\r\n\r\ntwo", FormattingOptions{})
	if formatted != "one\r\n\r\n\r\ntwo" {
		t.Fatalf("formatted = %q", formatted)
	}
}

func TestFormatDocumentDelegatesDirectiveJScriptASPBlocks(t *testing.T) {
	parsed := ParseDocument("file:///format.asp", `<%@ LANGUAGE="JScript" %><% function greet(name){return "a=b";} %>`, Settings{})
	edits := FormatDocument(parsed, FormattingOptions{
		TabSize:      2,
		InsertSpaces: true,
		FormatJavaScript: func(source string, _ FormattingOptions, language EmbeddedLanguage) (string, error) {
			if language != LanguageJScript {
				t.Fatalf("js callback language = %q", language)
			}
			if source != `function greet(name){return "a=b";}` {
				t.Fatalf("js callback source = %q", source)
			}
			return `function greet(name) {
  return "a=b";
}`, nil
		},
	})
	if len(edits) != 1 {
		t.Fatalf("edits = %#v", edits)
	}
	if !strings.Contains(edits[0].NewText, `function greet(name) {`) || !strings.Contains(edits[0].NewText, `"a=b"`) {
		t.Fatalf("formatted = %q", edits[0].NewText)
	}
}

func TestFormatDocumentHonorsEnabledLanguages(t *testing.T) {
	parsed := ParseDocument("file:///format.asp", `<style>.x{color:red;}</style><%if a=1 then%>`, Settings{})
	edits := FormatDocument(parsed, FormattingOptions{
		TabSize:          2,
		InsertSpaces:     true,
		EnabledLanguages: []string{"css"},
		FormatCSS: func(source string, _ FormattingOptions) (string, error) {
			return ".x { color: red; }", nil
		},
	})
	if len(edits) != 1 {
		t.Fatalf("edits = %#v", edits)
	}
	if edits[0].NewText != `<style>.x { color: red; }</style><%if a=1 then%>` {
		t.Fatalf("formatted = %q", edits[0].NewText)
	}
}

func TestFormatDocumentCanDisableEmbeddedLanguageFormatting(t *testing.T) {
	parsed := ParseDocument("file:///format.asp", `<style>.x{color:red;}</style><script>if(a){b();}</script><%if a=1 then%>`, Settings{})
	edits := FormatDocument(parsed, FormattingOptions{
		TabSize:                    2,
		InsertSpaces:               true,
		EmbeddedLanguageFormatting: "off",
		FormatCSS: func(source string, _ FormattingOptions) (string, error) {
			t.Fatalf("css callback should not run when embedded formatting is off: %q", source)
			return source, nil
		},
		FormatJavaScript: func(source string, _ FormattingOptions, _ EmbeddedLanguage) (string, error) {
			t.Fatalf("javascript callback should not run when embedded formatting is off: %q", source)
			return source, nil
		},
	})
	if len(edits) != 1 {
		t.Fatalf("edits = %#v", edits)
	}
	if edits[0].NewText != `<style>.x{color:red;}</style><script>if(a){b();}</script><% if a = 1 then %>` {
		t.Fatalf("formatted = %q", edits[0].NewText)
	}
}

func TestFormatDocumentDelegatesWholeHTMLWhenNoASPRegions(t *testing.T) {
	parsed := ParseDocument("file:///format.asp", `<div><span>x</span></div>`, Settings{})
	edits := FormatDocument(parsed, FormattingOptions{
		TabSize:      2,
		InsertSpaces: true,
		FragmentMode: "fragment",
		FormatHTML: func(source string, options FormattingOptions) (string, error) {
			if source != `<div><span>x</span></div>` {
				t.Fatalf("html callback source = %q", source)
			}
			if options.TabSize != 2 || !options.InsertSpaces || options.FragmentMode != "fragment" {
				t.Fatalf("html callback options = %d %v %q", options.TabSize, options.InsertSpaces, options.FragmentMode)
			}
			return "<div>\n  <span>x</span>\n</div>", nil
		},
	})
	if len(edits) != 1 {
		t.Fatalf("edits = %#v", edits)
	}
	if edits[0].NewText != "<div>\n  <span>x</span>\n</div>" {
		t.Fatalf("formatted = %q", edits[0].NewText)
	}
}

func TestFormatDocumentRejectsHTMLOutputThatChangesServerRegions(t *testing.T) {
	tests := []struct {
		name   string
		source string
		html   func(string) string
	}{
		{
			name:   "creates an ASP block",
			source: "<p>a < %b</p>",
			html:   func(source string) string { return strings.ReplaceAll(source, "< %", "<%") },
		},
		{
			name:   "drops an ASP block around HTML",
			source: "<div><%= value %></div>",
			html: func(source string) string {
				return strings.ReplaceAll(source, aspHTMLPlaceholderToken("", 0), "")
			},
		},
	}
	for _, test := range tests {
		t.Run(test.name, func(t *testing.T) {
			parsed := ParseDocument("file:///format.asp", test.source, Settings{})
			edits := FormatDocument(parsed, FormattingOptions{
				TabSize:      2,
				InsertSpaces: true,
				FormatHTML: func(source string, _ FormattingOptions) (string, error) {
					return test.html(source), nil
				},
			})
			if len(edits) != 0 {
				t.Fatalf("edits = %#v, want none when server regions change", edits)
			}
		})
	}
}

func TestFormattingRejectsRegionFormatterOutputThatAddsASPBlocks(t *testing.T) {
	source := "<style>.a { b: < % }</style>"
	parsed := ParseDocument("file:///format.asp", source, Settings{})
	options := FormattingOptions{
		TabSize:      2,
		InsertSpaces: true,
		FormatCSS: func(css string, _ FormattingOptions) (string, error) {
			return strings.ReplaceAll(css, "< %", "<%"), nil
		},
	}
	if edits := FormatDocument(parsed, options); len(edits) != 0 {
		t.Fatalf("document edits = %#v, want none when server regions change", edits)
	}
	whole := lsp.Range{End: NewTextDocument(parsed.URI, "classic-asp", 0, source).PositionAt(len(source))}
	if edits := FormatRange(parsed, whole, options); len(edits) != 0 {
		t.Fatalf("range edits = %#v, want none when server regions change", edits)
	}
}

func TestFormatDocumentFormatsHTMLAroundASPRegions(t *testing.T) {
	parsed := ParseDocument("file:///format.asp", `<div><%if a=1 then%></div>`, Settings{})
	edits := FormatDocument(parsed, FormattingOptions{
		TabSize:      2,
		InsertSpaces: true,
		FormatHTML: func(source string, options FormattingOptions) (string, error) {
			if source != `<div>__ASP_LSP_FORMAT_HOLE_0__</div>` {
				t.Fatalf("html callback source = %q", source)
			}
			return "<div>\n  __ASP_LSP_FORMAT_HOLE_0__\n</div>", nil
		},
	})
	if len(edits) != 1 {
		t.Fatalf("edits = %#v", edits)
	}
	if edits[0].NewText != "<div>\n  <% if a = 1 then %>\n</div>" {
		t.Fatalf("formatted = %q", edits[0].NewText)
	}
}

func TestFormatDocumentProtectsServerScriptFromHTMLFormatting(t *testing.T) {
	source := `<script runat="server">
If enabled Then
Response.Write "ok"
End If
</script>`
	edits := FormatDocument(ParseDocument("file:///format.asp", source, Settings{}), FormattingOptions{
		TabSize:      2,
		InsertSpaces: true,
		FormatHTML: func(source string, _ FormattingOptions) (string, error) {
			if source != `<asp-lsp-format-hole-0></asp-lsp-format-hole-0>` {
				t.Fatalf("html callback source = %q", source)
			}
			return source, nil
		},
	})
	if len(edits) != 1 {
		t.Fatalf("edits = %#v", edits)
	}
	want := `<script runat="server">
  If enabled Then
    Response.Write "ok"
  End If
</script>`
	if edits[0].NewText != want {
		t.Fatalf("formatted = %q", edits[0].NewText)
	}
}

func TestFormatDocumentHonorsVBScriptTabSize(t *testing.T) {
	parsed := ParseDocument("file:///format.asp", "<%\nIf a Then\nResponse.Write a\nEnd If\n%>", Settings{})
	edits := FormatDocument(parsed, FormattingOptions{TabSize: 4, VBScriptTabSize: 2, InsertSpaces: true})
	if len(edits) != 1 {
		t.Fatalf("edits = %#v", edits)
	}
	if edits[0].NewText != "<%\n  If a Then\n    Response.Write a\n  End If\n%>" {
		t.Fatalf("formatted = %q", edits[0].NewText)
	}
}

func TestFormatDocumentHonorsVBScriptIndentStyle(t *testing.T) {
	insertSpaces := false
	parsed := ParseDocument("file:///format.asp", "<%\nIf a Then\nResponse.Write a\nEnd If\n%>", Settings{})
	edits := FormatDocument(parsed, FormattingOptions{TabSize: 2, InsertSpaces: true, VBScriptInsertSpaces: &insertSpaces})
	if len(edits) != 1 {
		t.Fatalf("edits = %#v", edits)
	}
	if edits[0].NewText != "<%\n\tIf a Then\n\t\tResponse.Write a\n\tEnd If\n%>" {
		t.Fatalf("formatted = %q", edits[0].NewText)
	}
}

func TestFormatDocumentHonorsVBScriptKeywordCase(t *testing.T) {
	parsed := ParseDocument("file:///format.asp", "<%\nIF enabled THEN\nResponse.Write \"a=b\"\nEND IF\n%>", Settings{})
	edits := FormatDocument(parsed, FormattingOptions{TabSize: 2, InsertSpaces: true, VBScriptKeywordCase: "lower"})
	if len(edits) != 1 {
		t.Fatalf("edits = %#v", edits)
	}
	if !strings.Contains(edits[0].NewText, "  if enabled then\n    Response.Write \"a=b\"\n  end if") {
		t.Fatalf("formatted = %q", edits[0].NewText)
	}
}

func TestFormatDocumentHonorsLegacyUppercaseKeywords(t *testing.T) {
	parsed := ParseDocument("file:///format.asp", "<% if enabled then %>", Settings{})
	edits := FormatDocument(parsed, FormattingOptions{TabSize: 2, InsertSpaces: true, UppercaseKeywords: true})
	if len(edits) != 1 {
		t.Fatalf("edits = %#v", edits)
	}
	if edits[0].NewText != "<% IF enabled THEN %>" {
		t.Fatalf("formatted = %q", edits[0].NewText)
	}
}

func TestFormatDocumentAppliesEndOfLineSetting(t *testing.T) {
	parsed := ParseDocument("file:///format.asp", "<%\nIf a Then\nResponse.Write a\nEnd If\n%>", Settings{})
	edits := FormatDocument(parsed, FormattingOptions{TabSize: 2, InsertSpaces: true, EndOfLine: "crlf"})
	if len(edits) != 1 {
		t.Fatalf("edits = %#v", edits)
	}
	if edits[0].NewText != "<%\r\n  If a Then\r\n    Response.Write a\r\n  End If\r\n%>" {
		t.Fatalf("formatted = %q", edits[0].NewText)
	}
}

func TestFormatDocumentAppliesFinalNewlineSetting(t *testing.T) {
	parsed := ParseDocument("file:///format.asp", "<% if a then %>", Settings{})
	edits := FormatDocument(parsed, FormattingOptions{TabSize: 2, InsertSpaces: true, InsertFinalNewline: true})
	if len(edits) != 1 {
		t.Fatalf("edits = %#v", edits)
	}
	if edits[0].NewText != "<% if a then %>\n" {
		t.Fatalf("formatted = %q", edits[0].NewText)
	}
}

func TestFormatDocumentRespectsDisableMarkers(t *testing.T) {
	parsed := ParseDocument("file:///format.asp", `<%
' asp-format off
If enabled Then
Response.Write   "keep spacing"
End If
' asp-format on
If enabled Then
Response.Write "ok"
End If
%>`, Settings{})
	edits := FormatDocument(parsed, FormattingOptions{TabSize: 2, InsertSpaces: true, RespectDisableRegions: true})
	if len(edits) != 1 {
		t.Fatalf("edits = %#v", edits)
	}
	if !strings.Contains(edits[0].NewText, `' asp-format off
If enabled Then
Response.Write   "keep spacing"
End If
' asp-format on`) {
		t.Fatalf("disabled block was changed: %q", edits[0].NewText)
	}
	if !strings.Contains(edits[0].NewText, `  If enabled Then
    Response.Write "ok"
  End If`) {
		t.Fatalf("enabled block was not formatted: %q", edits[0].NewText)
	}
}

func TestFormatDocumentHonorsAdvancedVBScriptSettings(t *testing.T) {
	source := `<%
first=1
longerName=2
a = _
"aaa"
Select Case kind
Case "a"
Response.Write "a"
End Select
%>`
	parsed := ParseDocument("file:///format.asp", source, Settings{})
	edits := FormatDocument(parsed, FormattingOptions{
		AlignAssignments:                   true,
		InsertSpaces:                       true,
		TabSize:                            2,
		VBScriptLineContinuationIndentSize: 6,
		VBScriptSelectCaseIndent:           "caseAligned",
	})
	if len(edits) != 1 {
		t.Fatalf("edits = %#v", edits)
	}
	formatted := edits[0].NewText
	if !strings.Contains(formatted, "  first      = 1\n  longerName = 2") {
		t.Fatalf("assignments were not aligned: %q", formatted)
	}
	if !strings.Contains(formatted, "  a          = _\n        \"aaa\"") {
		t.Fatalf("continuation indent was not applied: %q", formatted)
	}
	if !strings.Contains(formatted, "  Select Case kind\n  Case \"a\"\n    Response.Write \"a\"\n  End Select") {
		t.Fatalf("select case indent was not applied: %q", formatted)
	}
}

func TestFormatDocumentHonorsASPDelimiterAndBlockSettings(t *testing.T) {
	compact := FormatDocument(ParseDocument("file:///format.asp", "<%= title %>\n<%if enabled then%>", Settings{}), FormattingOptions{
		ASPDelimiterSpacing: "compact",
		InsertSpaces:        true,
		TabSize:             2,
	})
	if len(compact) != 1 || compact[0].NewText != "<%=title%>\n<%if enabled then%>" {
		t.Fatalf("compact = %#v", compact)
	}
	multiline := FormatDocument(ParseDocument("file:///format.asp", `<% Response.Write "ok" %>`, Settings{}), FormattingOptions{
		ASPBlockNewline: "alwaysMultiline",
		InsertSpaces:    true,
		TabSize:         2,
	})
	if len(multiline) != 1 || multiline[0].NewText != "<%\n  Response.Write \"ok\"\n%>" {
		t.Fatalf("multiline = %#v", multiline)
	}
	alignDelimiter := FormatDocument(ParseDocument("file:///format.asp", "<%\nIf enabled Then\nResponse.Write \"ok\"\nEnd If\n%>", Settings{}), FormattingOptions{
		VBScriptBlockIndent: "alignWithDelimiter",
		InsertSpaces:        true,
		TabSize:             2,
	})
	if len(alignDelimiter) != 1 || !strings.Contains(alignDelimiter[0].NewText, "<%\nIf enabled Then\n  Response.Write \"ok\"") {
		t.Fatalf("align delimiter = %#v", alignDelimiter)
	}
}

func TestFormatDocumentDistinguishesNestedASPInCSSJSModes(t *testing.T) {
	const source = `<style>.x{color:<%= color %>}</style>`
	for _, testCase := range []struct {
		mode          string
		wantCallback  bool
		wantProtected bool
	}{
		{mode: "skipRegion"},
		{mode: "protectAspOnly", wantCallback: true, wantProtected: true},
		{mode: "formatAroundAsp", wantCallback: true},
	} {
		t.Run(testCase.mode, func(t *testing.T) {
			called := false
			parsed := ParseDocument("file:///nested.asp", source, Settings{})
			edits := FormatDocument(parsed, FormattingOptions{
				InsertSpaces:     true,
				TabSize:          2,
				NestedASPInCSSJS: testCase.mode,
				EnabledLanguages: []string{"css"},
				FormatCSS: func(value string, _ FormattingOptions) (string, error) {
					called = true
					if strings.Contains(value, "<%") == testCase.wantProtected {
						t.Fatalf("callback input = %q, wantProtected=%v", value, testCase.wantProtected)
					}
					return strings.Replace(value, ".x{", ".x {", 1), nil
				},
			})
			if called != testCase.wantCallback {
				t.Fatalf("callback called = %v, want %v", called, testCase.wantCallback)
			}
			if testCase.mode == "skipRegion" && len(edits) != 0 {
				t.Fatalf("skipRegion edits = %#v", edits)
			}
			if testCase.mode == "protectAspOnly" && (len(edits) != 1 || !strings.Contains(edits[0].NewText, "<%= color %>")) {
				t.Fatalf("protectAspOnly edits = %#v", edits)
			}
		})
	}
}

func TestNestedASPSkipModeDoesNotTreatClientLiteralAsServerRegion(t *testing.T) {
	const source = `<script>const marker="<%";</script>`
	called := false
	edits := FormatDocument(ParseDocument("file:///literal.asp", source, Settings{}), FormattingOptions{
		InsertSpaces:     true,
		TabSize:          2,
		NestedASPInCSSJS: "skipRegion",
		FormatJavaScript: func(value string, _ FormattingOptions, _ EmbeddedLanguage) (string, error) {
			called = true
			if value != `const marker="<%";` {
				t.Fatalf("client literal was protected: %q", value)
			}
			return `const marker = "<%";`, nil
		},
	})
	if !called || len(edits) != 1 || !strings.Contains(edits[0].NewText, `const marker = "<%";`) {
		t.Fatalf("literal formatting = called:%v edits:%#v", called, edits)
	}
}

func TestFormatVBLinePreservesPlaceholderShapedStringContent(t *testing.T) {
	for _, line := range []string{
		`a = "__ASP_LSP_VB_LITERAL_1__" & "x"`,
		`a = "__ASP_LSP_VB_LITERAL_01__" & "x" ' __ASP_LSP_VB_LITERAL_0__`,
		`b = "__ASP_LSP_VB_LITERAL_" & "__"`,
	} {
		if got := formatVBLine(line, FormattingOptions{}); got != line {
			t.Fatalf("formatVBLine(%q) = %q", line, got)
		}
	}
}

func TestFormatVBLineRestoresManyLiterals(t *testing.T) {
	line := strings.Repeat(`"x" & `, 3000) + `"y"`
	if got := formatVBLine(line, FormattingOptions{}); got != line {
		t.Fatalf("literal restoration changed a %d-byte line", len(line))
	}
}
