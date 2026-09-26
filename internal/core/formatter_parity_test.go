package core

import (
	"strings"
	"testing"

	"github.com/yottonoko/classic-web-system-lsp/internal/lsp"
)

func TestFormatsFullDocumentsWithoutErasingASPDelimiters(t *testing.T) {
	parsed := ParseDocument("file:///site/default.asp", `<html>
<body>
<% Option Explicit
If enabled Then
Response.Write "ok"
End If
%>
<%= title %>
</body>
</html>`, Settings{})
	edits := FormatDocument(parsed, FormattingOptions{TabSize: 2, InsertSpaces: true})
	if len(edits) != 1 {
		t.Fatalf("edits = %#v", edits)
	}
	for _, expected := range []string{"<%", "%>", "    Response.Write", "<%= title %>"} {
		if !strings.Contains(edits[0].NewText, expected) {
			t.Fatalf("formatted missing %q: %q", expected, edits[0].NewText)
		}
	}
}

func TestFormatsASPRangesOnCSTNodeBoundaries(t *testing.T) {
	parsed := ParseDocument("file:///site/default.asp", `<div></div>
<%
If enabled Then
Response.Write "ok"
End If
%>`, Settings{})
	edits := FormatRange(parsed, lsp.Range{
		Start: lsp.Position{Line: 1, Character: 0},
		End:   lsp.Position{Line: 5, Character: 2},
	}, FormattingOptions{TabSize: 2, InsertSpaces: true})
	if len(edits) != 1 || !strings.Contains(edits[0].NewText, "    Response.Write") || strings.Contains(edits[0].NewText, "<div>") {
		t.Fatalf("range edits = %#v", edits)
	}
}

func TestFormatRangeRemovesFormatterIndentationFromBlankLines(t *testing.T) {
	source := "<style>.x{color:red}</style>"
	parsed := ParseDocument("file:///site/range-blank-lines.asp", source, Settings{})
	edits := FormatRange(parsed, lsp.Range{
		Start: lsp.Position{},
		End:   NewTextDocument(parsed.URI, "classic-asp", 0, source).PositionAt(len(source)),
	}, FormattingOptions{
		TabSize:      2,
		InsertSpaces: true,
		FormatCSS: func(string, FormattingOptions) (string, error) {
			return ".x {\n  \n  color: red;\n}", nil
		},
	})
	if len(edits) != 1 || strings.Contains(edits[0].NewText, "\n  \n") {
		t.Fatalf("range edits = %#v", edits)
	}
}

func TestKeepsBytesOutsideFormattedASPRangesUnchanged(t *testing.T) {
	source := `<header>Before</header>
<%
If enabled Then
Response.Write "ok"
End If
%>
<footer>After</footer>`
	start := strings.Index(source, "<%")
	end := strings.Index(source, "%>") + len("%>")
	parsed := ParseDocument("file:///site/default.asp", source, Settings{})
	edits := FormatRange(parsed, NewTextDocument(parsed.URI, "classic-asp", 0, source).Range(start, end), FormattingOptions{TabSize: 2, InsertSpaces: true})
	formatted := applyCoreTextEdits(t, source, edits)
	if len(edits) != 1 {
		t.Fatalf("edits = %#v", edits)
	}
	if formatted[:start] != source[:start] || formatted[strings.Index(formatted, "<footer>"):] != source[strings.Index(source, "<footer>"):] {
		t.Fatalf("formatted changed outside range: %q", formatted)
	}
}

func TestDoesNotDuplicateNestedASPExpressionsInsideNonServerRegions(t *testing.T) {
	parsed := ParseDocument("file:///site/default.asp", `<style>.x { color: <%= themeColor %>; }</style>
<%value=1%>`, Settings{})
	edits := FormatDocument(parsed, FormattingOptions{TabSize: 2, InsertSpaces: true})
	if len(edits) != 1 {
		t.Fatalf("edits = %#v", edits)
	}
	if strings.Count(edits[0].NewText, "themeColor") != 1 || !strings.Contains(edits[0].NewText, "<% value = 1 %>") {
		t.Fatalf("formatted = %q", edits[0].NewText)
	}
}

func TestPreservesVBScriptStringsAndCommentsWhileFormattingOperators(t *testing.T) {
	parsed := ParseDocument("file:///site/default.asp", `<%
Response.Write "a=b"
' keep x=y
value=1
%>
<%= "x=y" %>`, Settings{})
	edits := FormatDocument(parsed, FormattingOptions{TabSize: 2, InsertSpaces: true})
	if len(edits) != 1 {
		t.Fatalf("edits = %#v", edits)
	}
	for _, expected := range []string{`Response.Write "a=b"`, `' keep x=y`, `value = 1`, `<%= "x=y" %>`} {
		if !strings.Contains(edits[0].NewText, expected) {
			t.Fatalf("formatted missing %q: %q", expected, edits[0].NewText)
		}
	}
}

func TestIsIdempotentForMixedASPDocuments(t *testing.T) {
	source := `<html>
<style>.x { color: <%= themeColor %>; }</style>
<script>const value = 1;</script>
<%
If enabled Then ' keep
Response.Write "ok"
End If
%>
<%= title %>
</html>`
	first := applyCoreTextEdits(t, source, FormatDocument(ParseDocument("file:///site/default.asp", source, Settings{}), FormattingOptions{TabSize: 2, InsertSpaces: true}))
	if edits := FormatDocument(ParseDocument("file:///site/default.asp", first, Settings{}), FormattingOptions{TabSize: 2, InsertSpaces: true}); len(edits) != 0 {
		t.Fatalf("second format edits = %#v after %q", edits, first)
	}
}

func TestPreservesStringWhitespaceInsideSingleLineASPBlocks(t *testing.T) {
	edits := FormatDocument(ParseDocument("file:///site/default.asp", `<%Response.Write "a   b"%>`, Settings{}), FormattingOptions{TabSize: 2, InsertSpaces: true})
	if len(edits) != 1 || edits[0].NewText != `<% Response.Write "a   b" %>` {
		t.Fatalf("edits = %#v", edits)
	}
}

func TestLeavesServerSideJScriptRegionsUnchanged(t *testing.T) {
	source := `<%@ LANGUAGE="JScript" %>
<%
var s = "a=b";
%>`
	edits := FormatDocument(ParseDocument("file:///site/default.asp", source, Settings{}), FormattingOptions{TabSize: 2, InsertSpaces: true})
	if len(edits) != 0 {
		t.Fatalf("JScript edits = %#v", edits)
	}
}

func TestAlignsSimpleVBScriptAssignmentsWhenRequested(t *testing.T) {
	edits := FormatDocument(ParseDocument("file:///site/default.asp", `<%
Dim first
Dim longerName
first=1
longerName=2
%>`, Settings{}), FormattingOptions{TabSize: 2, InsertSpaces: true, AlignAssignments: true})
	if len(edits) != 1 || !strings.Contains(edits[0].NewText, "  first      = 1\n  longerName = 2") {
		t.Fatalf("edits = %#v", edits)
	}
}

func TestIndentsVBScriptLineContinuationsOneLevelDeeper(t *testing.T) {
	edits := FormatDocument(ParseDocument("file:///site/default.asp", `<%
a = _
"aaa" & _
"bbb"
%>`, Settings{}), FormattingOptions{TabSize: 4, InsertSpaces: true})
	if len(edits) != 1 || !strings.Contains(edits[0].NewText, "    a = _\n        \"aaa\" & _\n        \"bbb\"") {
		t.Fatalf("edits = %#v", edits)
	}
}

func TestCanUseACustomVBScriptLineContinuationIndent(t *testing.T) {
	edits := FormatDocument(ParseDocument("file:///site/default.asp", `<%
a = _
"aaa"
%>`, Settings{}), FormattingOptions{TabSize: 2, InsertSpaces: true, VBScriptLineContinuationIndentSize: 6})
	if len(edits) != 1 || !strings.Contains(edits[0].NewText, "  a = _\n        \"aaa\"") {
		t.Fatalf("edits = %#v", edits)
	}
}

func TestIndentsNestedIfElseIfAndElseBlocksConsistently(t *testing.T) {
	edits := FormatDocument(ParseDocument("file:///site/default.asp", `<%
If outer Then
If inner Then
a=1
ElseIf fallback Then
a=2
Else
a=3
End If
Else
a=4
End If
%>`, Settings{}), FormattingOptions{TabSize: 2, InsertSpaces: true})
	if len(edits) != 1 || !strings.Contains(edits[0].NewText, "    If inner Then\n      a = 1\n    ElseIf fallback Then") {
		t.Fatalf("edits = %#v", edits)
	}
}

func TestFormatsCommentsAfterContinuedVBScriptLinesFromTokenizedBlocks(t *testing.T) {
	edits := FormatDocument(ParseDocument("file:///site/default.asp", `<%
If enabled Then
value = _
"ok" ' inline
' keep comment
Response.Write value
End If
%>`, Settings{}), FormattingOptions{TabSize: 2, InsertSpaces: true})
	want := `<%
  If enabled Then
    value = _
      "ok" ' inline
    ' keep comment
    Response.Write value
  End If
%>`
	if len(edits) != 1 || edits[0].NewText != want {
		t.Fatalf("edits = %#v", edits)
	}
}

func TestIndentsBlockIfBodiesWhenThenIsFollowedByAComment(t *testing.T) {
	edits := FormatDocument(ParseDocument("file:///site/default.asp", `<%
If enabled Then ' keep comment
Response.Write "ok"
End If
%>`, Settings{}), FormattingOptions{TabSize: 2, InsertSpaces: true})
	if len(edits) != 1 || !strings.Contains(edits[0].NewText, "  If enabled Then ' keep comment\n    Response.Write \"ok\"") {
		t.Fatalf("edits = %#v", edits)
	}
}

func TestFormatsIndentedASPBlocksRelativeToTheirTagIndentation(t *testing.T) {
	edits := FormatDocument(ParseDocument("file:///site/default.asp", `<div>
    <%
If enabled Then
Response.Write "ok"
End If
    %>
</div>`, Settings{}), FormattingOptions{TabSize: 2, InsertSpaces: true})
	if len(edits) != 1 || !strings.Contains(edits[0].NewText, "    <%\n      If enabled Then\n        Response.Write \"ok\"\n      End If\n    %>") {
		t.Fatalf("edits = %#v", edits)
	}
}

func TestCanAlignASPBlockStatementsWithDelimitersWhenConfigured(t *testing.T) {
	edits := FormatDocument(ParseDocument("file:///site/default.asp", `<%
If enabled Then
Response.Write "ok"
End If
%>`, Settings{}), FormattingOptions{TabSize: 2, InsertSpaces: true, VBScriptBlockIndent: "alignWithDelimiter"})
	want := `<%
If enabled Then
  Response.Write "ok"
End If
%>`
	if len(edits) != 1 || edits[0].NewText != want {
		t.Fatalf("edits = %#v", edits)
	}
}

func TestKeepsLegacyTagIndentNormalizationWithoutSeparateVBScriptIndentation(t *testing.T) {
	edits := FormatDocument(ParseDocument("file:///site/default.asp", `<div>
   <%
If enabled Then
Response.Write "ok"
End If
   %>
</div>`, Settings{}), FormattingOptions{TabSize: 2, InsertSpaces: true})
	if len(edits) != 1 || !strings.Contains(edits[0].NewText, "   <%\n     If enabled Then\n       Response.Write \"ok\"\n     End If\n   %>") {
		t.Fatalf("edits = %#v", edits)
	}
}

func TestCanUseASeparateVBScriptIndentSize(t *testing.T) {
	edits := FormatDocument(ParseDocument("file:///site/default.asp", `<%
If enabled Then
Response.Write "ok"
End If
%>`, Settings{}), FormattingOptions{TabSize: 4, InsertSpaces: true, VBScriptTabSize: 2})
	want := `<%
  If enabled Then
    Response.Write "ok"
  End If
%>`
	if len(edits) != 1 || edits[0].NewText != want {
		t.Fatalf("edits = %#v", edits)
	}
}

func TestCanUseASeparateVBScriptIndentStyle(t *testing.T) {
	insertSpaces := false
	edits := FormatDocument(ParseDocument("file:///site/default.asp", `<%
If enabled Then
Response.Write "ok"
End If
%>`, Settings{}), FormattingOptions{TabSize: 4, InsertSpaces: true, VBScriptInsertSpaces: &insertSpaces})
	want := "<%\n\tIf enabled Then\n\t\tResponse.Write \"ok\"\n\tEnd If\n%>"
	if len(edits) != 1 || edits[0].NewText != want {
		t.Fatalf("edits = %#v", edits)
	}
}

func TestCanIgnoreTagIndentationWhenFormattingASPBlocks(t *testing.T) {
	edits := FormatDocument(ParseDocument("file:///site/default.asp", `<div>
    <%
If enabled Then
Response.Write "ok"
End If
    %>
</div>`, Settings{}), FormattingOptions{TabSize: 2, InsertSpaces: true, IgnoreVBScriptTagIndent: true})
	if len(edits) != 1 || !strings.Contains(edits[0].NewText, "    <%\n      If enabled Then\n        Response.Write \"ok\"\n      End If\n    %>") {
		t.Fatalf("edits = %#v", edits)
	}
}

func TestFormatsServerSideVBScriptTagsRelativeToTheirTagIndentation(t *testing.T) {
	edits := FormatDocument(ParseDocument("file:///site/default.asp", `<div>
  <script runat="server">
If enabled Then
Response.Write "ok"
End If
  </script>
</div>`, Settings{}), FormattingOptions{TabSize: 2, InsertSpaces: true})
	if len(edits) != 1 || !strings.Contains(edits[0].NewText, "  <script runat=\"server\">\n    If enabled Then\n      Response.Write \"ok\"\n    End If\n  </script>") {
		t.Fatalf("edits = %#v", edits)
	}
}

func TestIndentsCommonNonIfVBScriptBlockStatementsConsistently(t *testing.T) {
	edits := FormatDocument(ParseDocument("file:///site/default.asp", `<%
For i=0 To 1
Response.Write i
Next
Do While ready
ready=False
Loop
While active
active=False
Wend
With Response
.Write "ok"
End With
Sub Render()
Response.Write "done"
End Sub
%>`, Settings{}), FormattingOptions{TabSize: 2, InsertSpaces: true})
	for _, expected := range []string{"  For i = 0 To 1\n    Response.Write i\n  Next", "  With Response\n    .Write \"ok\"\n  End With", "  Sub Render()\n    Response.Write \"done\"\n  End Sub"} {
		if len(edits) != 1 || !strings.Contains(edits[0].NewText, expected) {
			t.Fatalf("formatted missing %q: %#v", expected, edits)
		}
	}
}

func TestIndentsProcedureAndPropertyBlocksWithDeclarationModifiers(t *testing.T) {
	edits := FormatDocument(ParseDocument("file:///site/default.asp", `<%
Class Customer
Private name
Public Sub Render()
Response.Write name
End Sub
Private Function Build()
Build=name
End Function
Public Default Property Get Name
Name=name
End Property
End Class
%>`, Settings{}), FormattingOptions{TabSize: 2, InsertSpaces: true})
	for _, expected := range []string{"  Class Customer\n    Private name", "    Public Sub Render()\n      Response.Write name\n    End Sub", "    Public Default Property Get Name\n      Name = name\n    End Property"} {
		if len(edits) != 1 || !strings.Contains(edits[0].NewText, expected) {
			t.Fatalf("formatted missing %q: %#v", expected, edits)
		}
	}
}

func TestFormatsSelectCaseBlocksWithCaseBodiesIndented(t *testing.T) {
	edits := FormatDocument(ParseDocument("file:///site/default.asp", `<%
Select Case kind
Case "a"
Response.Write "a"
Case Else
Response.Write "else"
End Select
%>`, Settings{}), FormattingOptions{TabSize: 2, InsertSpaces: true})
	if len(edits) != 1 || !strings.Contains(edits[0].NewText, "Select Case kind\n    Case \"a\"\n      Response.Write \"a\"\n    Case Else\n      Response.Write \"else\"\n  End Select") {
		t.Fatalf("edits = %#v", edits)
	}
}

func TestCanAlignVBScriptCaseLinesWithSelectCase(t *testing.T) {
	edits := FormatDocument(ParseDocument("file:///site/default.asp", `<%
Select Case kind
Case "a"
Response.Write "a"
End Select
%>`, Settings{}), FormattingOptions{TabSize: 2, InsertSpaces: true, VBScriptSelectCaseIndent: "caseAligned"})
	if len(edits) != 1 || !strings.Contains(edits[0].NewText, "  Select Case kind\n  Case \"a\"\n    Response.Write \"a\"\n  End Select") {
		t.Fatalf("edits = %#v", edits)
	}
}

func TestFormatsSplitASPControlFlowIslandsWithoutRewritingCSSOrJavaScriptBodies(t *testing.T) {
	source := `<ul>
<%If showItems Then%>
<li class="<%= itemClass %>"><%= itemTitle %></li>
<%Else%>
<li class="empty">None</li>
<%End If%>
</ul>
<style>.badge { color: <%= badgeColor %>; }</style>
<script>const state = <%= stateJson %>;</script>`
	formatted := applyCoreTextEdits(t, source, FormatDocument(ParseDocument("file:///site/split-format.asp", source, Settings{}), FormattingOptions{TabSize: 2, InsertSpaces: true}))
	for _, expected := range []string{"<% If showItems Then %>", "<% Else %>", "<% End If %>", `<li class="<%= itemClass %>"><%= itemTitle %></li>`, ".badge { color: <%= badgeColor %>; }", "const state = <%= stateJson %>;"} {
		if !strings.Contains(formatted, expected) {
			t.Fatalf("formatted missing %q: %q", expected, formatted)
		}
	}
	if strings.Count(formatted, "itemTitle") != 1 || strings.Count(formatted, "stateJson") != 1 {
		t.Fatalf("formatted duplicated ASP expressions: %q", formatted)
	}
	if edits := FormatDocument(ParseDocument("file:///site/split-format.asp", formatted, Settings{}), FormattingOptions{TabSize: 2, InsertSpaces: true}); len(edits) != 0 {
		t.Fatalf("second format edits = %#v after %q", edits, formatted)
	}
}

func TestFormatsOneASPBlockRangeWithoutNormalizingLaterASPBlocks(t *testing.T) {
	source := `<%
If first Then
Response.Write "first"
End If
%>
<p>middle</p>
<%
If second Then
Response.Write "second"
End If
%>`
	firstBlockEnd := strings.Index(source, "%>") + len("%>")
	edits := FormatRange(ParseDocument("file:///site/range-blocks.asp", source, Settings{}), NewTextDocument("", "classic-asp", 0, source).Range(0, firstBlockEnd), FormattingOptions{TabSize: 2, InsertSpaces: true})
	formatted := applyCoreTextEdits(t, source, edits)
	if !strings.Contains(formatted, "  If first Then\n    Response.Write \"first\"\n  End If") ||
		!strings.Contains(formatted, "If second Then\nResponse.Write \"second\"\nEnd If") {
		t.Fatalf("formatted = %q", formatted)
	}
}

func TestFormatRangeLeavesPartiallyCoveredRegionsUntouched(t *testing.T) {
	source := `<div>before</div>
<%
If enabled Then
Response.Write "ok"
End If
%>
<div>after</div>`
	parsed := ParseDocument("file:///site/partial-range.asp", source, Settings{})
	document := NewTextDocument(parsed.URI, "classic-asp", 0, source)
	block := firstRegionOfKind(parsed, RegionASPBlock)
	if block == nil {
		t.Fatal("ASP block region missing")
	}
	partialStart := document.Range(block.ContentStart, block.End)
	if edits := FormatRange(parsed, partialStart, FormattingOptions{TabSize: 2, InsertSpaces: true}); len(edits) != 0 {
		t.Fatalf("partial-start range edits = %#v", edits)
	}
	partialEnd := document.Range(block.Start, block.ContentEnd)
	if edits := FormatRange(parsed, partialEnd, FormattingOptions{TabSize: 2, InsertSpaces: true}); len(edits) != 0 {
		t.Fatalf("partial-end range edits = %#v", edits)
	}
}

func TestFormatRangeNormalizesReversedRangesWithoutSlicingOutOfBounds(t *testing.T) {
	source := `<%
If enabled Then
Response.Write "ok"
End If
%>`
	parsed := ParseDocument("file:///site/reversed-range.asp", source, Settings{})
	document := NewTextDocument(parsed.URI, "classic-asp", 0, source)
	end := strings.Index(source, "%>") + len("%>")
	edits := FormatRange(parsed, document.Range(end, 0), FormattingOptions{TabSize: 2, InsertSpaces: true})
	if len(edits) != 1 {
		t.Fatalf("reversed range edits = %#v", edits)
	}
	if edits[0].Range != document.Range(0, end) {
		t.Fatalf("normalized range = %#v, want %#v", edits[0].Range, document.Range(0, end))
	}
	if !strings.Contains(edits[0].NewText, "  If enabled Then\n    Response.Write \"ok\"") {
		t.Fatalf("formatted reversed range = %q", edits[0].NewText)
	}
}

func TestCanCompactASPDelimiterSpacingAndUppercaseVBScriptKeywords(t *testing.T) {
	edits := FormatDocument(ParseDocument("file:///site/default.asp", `<%if enabled then%>
<%= title %>`, Settings{}), FormattingOptions{TabSize: 2, InsertSpaces: true, ASPDelimiterSpacing: "compact", VBScriptKeywordCase: "upper"})
	if len(edits) != 1 || edits[0].NewText != "<%IF enabled THEN%>\n<%=title%>" {
		t.Fatalf("edits = %#v", edits)
	}
}

func TestTogglesLineCommentsByEmbeddedClassicASPRegion(t *testing.T) {
	source := `<div>hello</div>
<script>
  const value = 1;
</script>
<style>
  .x { color: red; }
</style>
<%
  Response.Write value
%>`
	selections := []lsp.Range{
		rangeForNeedle(t, source, "hello"),
		rangeForNeedle(t, source, "const value"),
		rangeForNeedle(t, source, ".x {"),
		rangeForNeedle(t, source, "Response.Write"),
	}
	commented := applyCoreTextEdits(t, source, ClassicASPLineCommentEdits("file:///site/default.asp", source, selections))
	for _, expected := range []string{"<!-- <div>hello</div> -->", "  // const value = 1;", "  /* .x { color: red; } */", "  ' Response.Write value"} {
		if !strings.Contains(commented, expected) {
			t.Fatalf("commented missing %q: %q", expected, commented)
		}
	}
	if uncommented := applyCoreTextEdits(t, commented, ClassicASPLineCommentEdits("file:///site/default.asp", commented, selections)); uncommented != source {
		t.Fatalf("uncommented = %q", uncommented)
	}
}

func TestTogglesWholeSelectedNonEmptyLinesOnce(t *testing.T) {
	source := `<script>
  const first = 1;

  const second = 2;
</script>`
	commented := applyCoreTextEdits(t, source, ClassicASPLineCommentEdits("file:///site/default.asp", source, []lsp.Range{{
		Start: positionForOffset(source, strings.Index(source, "const first")),
		End:   positionForOffset(source, strings.Index(source, "</script>")),
	}}))
	for _, expected := range []string{"  // const first = 1;", "\n\n", "  // const second = 2;"} {
		if !strings.Contains(commented, expected) {
			t.Fatalf("commented missing %q: %q", expected, commented)
		}
	}
	if strings.Count(commented, "// const first") != 1 || strings.Count(commented, "// const second") != 1 {
		t.Fatalf("commented duplicated line comments: %q", commented)
	}
}
