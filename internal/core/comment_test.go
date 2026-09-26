package core

import (
	"sort"
	"strings"
	"testing"

	"github.com/yottonoko/classic-web-system-lsp/internal/lsp"
)

func TestClassicASPLineCommentEditsToggleByEmbeddedRegion(t *testing.T) {
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
	for _, expected := range []string{
		"<!-- <div>hello</div> -->",
		"  // const value = 1;",
		"  /* .x { color: red; } */",
		"  ' Response.Write value",
	} {
		if !strings.Contains(commented, expected) {
			t.Fatalf("commented output missing %q:\n%s", expected, commented)
		}
	}

	uncommented := applyCoreTextEdits(t, commented, ClassicASPLineCommentEdits("file:///site/default.asp", commented, selections))
	if uncommented != source {
		t.Fatalf("uncommented output mismatch:\n%s", uncommented)
	}
}

func TestClassicASPLineCommentEditsToggleWholeSelectedNonEmptyLinesOnce(t *testing.T) {
	source := `<script>
  const first = 1;

  const second = 2;
</script>`
	selection := lsp.Range{
		Start: positionForOffset(source, strings.Index(source, "const first")),
		End:   positionForOffset(source, strings.Index(source, "</script>")),
	}
	commented := applyCoreTextEdits(t, source, ClassicASPLineCommentEdits("file:///site/default.asp", source, []lsp.Range{selection}))
	for _, expected := range []string{"  // const first = 1;", "  // const second = 2;"} {
		if !strings.Contains(commented, expected) {
			t.Fatalf("commented output missing %q:\n%s", expected, commented)
		}
	}
	if !strings.Contains(commented, "\n\n") {
		t.Fatalf("blank line should stay untouched:\n%s", commented)
	}
}

func TestClassicASPLineCommentEditsIgnoresOutOfRangeSelections(t *testing.T) {
	source := "value"
	for _, selection := range []lsp.Range{
		{Start: lsp.Position{Line: 3}, End: lsp.Position{Line: 3}},
		{Start: lsp.Position{Line: -1}, End: lsp.Position{Line: -1}},
	} {
		if edits := ClassicASPLineCommentEdits("file:///site/default.vbs", source, []lsp.Range{selection}); len(edits) != 0 {
			t.Fatalf("out-of-range selection produced edits: %#v", edits)
		}
	}
}

func TestClassicASPLineCommentEditsToggleMixedLanguageLines(t *testing.T) {
	tests := []struct {
		name      string
		source    string
		commented string
	}{
		{
			name:      "html and asp expression",
			source:    `<p><%= title %></p>`,
			commented: `<!-- <p><%'= title %></p> -->`,
		},
		{
			name:      "javascript and asp expression",
			source:    `<script>const x=<%=v%>;</script>`,
			commented: `<!-- <script>const x=<%'=v%>;</script> -->`,
		},
		{
			name:      "css and asp expression",
			source:    `<style>.x{color:<%=color%>}</style>`,
			commented: `<!-- <style>.x{color:<%'=color%>}</style> -->`,
		},
		{
			name:      "inline style",
			source:    `<div style="color:red">x</div>`,
			commented: `<!-- <div style="color:red">x</div> -->`,
		},
		{
			name:      "multiple asp islands",
			source:    `<%=first%>-<%=second%>`,
			commented: `<!-- <%'=first%>-<%'=second%> -->`,
		},
		{
			name:      "server script",
			source:    `<script runat="server">value = 1</script>`,
			commented: `<!-- <script runat="server">' value = 1</script> -->`,
		},
	}
	for _, test := range tests {
		t.Run(test.name, func(t *testing.T) {
			selection := rangeForNeedle(t, test.source, test.source)
			commented := applyCoreTextEdits(t, test.source, ClassicASPLineCommentEdits("file:///site/default.asp", test.source, []lsp.Range{selection}))
			if commented != test.commented {
				t.Fatalf("commented output mismatch:\n got: %s\nwant: %s", commented, test.commented)
			}
			uncommented := applyCoreTextEdits(t, commented, ClassicASPLineCommentEdits("file:///site/default.asp", commented, []lsp.Range{rangeForNeedle(t, commented, commented)}))
			if uncommented != test.source {
				t.Fatalf("uncommented output mismatch:\n got: %s\nwant: %s", uncommented, test.source)
			}
		})
	}
}

func TestClassicASPLineCommentEditsTogglesASPExpressions(t *testing.T) {
	tests := []struct {
		name      string
		source    string
		commented string
	}{
		{
			name:      "single line",
			source:    "<%= title %>",
			commented: "<%'= title %>",
		},
		{
			name:      "multiline",
			source:    "<%=\n  BuildValue(\n    item\n  )\n%>",
			commented: "<%'=\n  ' BuildValue(\n    ' item\n  ' )\n%>",
		},
	}
	for _, test := range tests {
		t.Run(test.name, func(t *testing.T) {
			doc := NewTextDocument("file:///site/default.asp", "classic-asp", 0, test.source)
			selection := lsp.Range{Start: lsp.Position{}, End: doc.PositionAt(len(test.source))}
			commented := applyCoreTextEdits(t, test.source, ClassicASPLineCommentEdits(doc.URI, test.source, []lsp.Range{selection}))
			if commented != test.commented {
				t.Fatalf("ASP expression comment mismatch:\n got: %q\nwant: %q", commented, test.commented)
			}
			uncommented := applyCoreTextEdits(t, commented, ClassicASPLineCommentEdits(doc.URI, commented, []lsp.Range{selection}))
			if uncommented != test.source {
				t.Fatalf("ASP expression round trip mismatch:\n got: %q\nwant: %q", uncommented, test.source)
			}
		})
	}
}

func TestClassicASPLineCommentEditsUncommentsASPExpressionForms(t *testing.T) {
	for _, source := range []string{
		"<!-- <p> --><%'= title %><!-- </p> -->",
		"<!-- <p> --><%' = title %><!-- </p> -->",
	} {
		selection := rangeForNeedle(t, source, source)
		uncommented := applyCoreTextEdits(t, source, ClassicASPLineCommentEdits("file:///site/default.asp", source, []lsp.Range{selection}))
		if uncommented != "<p><%= title %></p>" {
			t.Fatalf("ASP expression form was not fully uncommented:\nsource: %q\noutput: %q", source, uncommented)
		}
	}
}

func TestClassicASPLineCommentEditsUsesSelectionWideState(t *testing.T) {
	source := "<!-- <p>ready</p> -->\n<% value = 1 %>"
	selection := lsp.Range{
		Start: lsp.Position{Line: 0},
		End:   lsp.Position{Line: 1, Character: 15},
	}
	commented := applyCoreTextEdits(t, source, ClassicASPLineCommentEdits("file:///site/default.asp", source, []lsp.Range{selection}))
	expected := "<!-- <!-- <p>ready</p> -->\n<%' value = 1 %> -->"
	if commented != expected {
		t.Fatalf("partially commented selection mismatch:\n got: %s\nwant: %s", commented, expected)
	}
	uncommented := applyCoreTextEdits(t, commented, ClassicASPLineCommentEdits(
		"file:///site/default.asp",
		commented,
		[]lsp.Range{selectedLineCommentRange(commented, 0, 1)},
	))
	if uncommented != source {
		t.Fatalf("selection-wide uncomment mismatch:\n%s", uncommented)
	}
}

func TestClassicASPLineCommentEditsPreservesCRLFAndUTF16(t *testing.T) {
	source := "<p>🌸<%= title %></p>\r\n\r\n<style>色{color:red}</style>\r\n"
	selection := lsp.Range{
		Start: lsp.Position{Line: 0},
		End:   lsp.Position{Line: 3},
	}
	commented := applyCoreTextEdits(t, source, ClassicASPLineCommentEdits("file:///site/default.asp", source, []lsp.Range{selection}))
	if strings.Count(commented, "\r\n") != 3 || !strings.Contains(commented, "\r\n\r\n") {
		t.Fatalf("line endings or blank line changed:\n%q", commented)
	}
	uncommented := applyCoreTextEdits(t, commented, ClassicASPLineCommentEdits("file:///site/default.asp", commented, []lsp.Range{selection}))
	if uncommented != source {
		t.Fatalf("CRLF/UTF-16 round trip mismatch:\n%q", uncommented)
	}
}

func TestClassicASPLineCommentEditsIgnoresFakeASPDelimitersInClientText(t *testing.T) {
	for _, source := range []string{
		`<script>// fake <% value %></script>`,
		`<script>const marker = "<% not asp %>";</script>`,
		`<style>/* fake <% value %> */ .x{color:red}</style>`,
	} {
		selection := rangeForNeedle(t, source, source)
		commented := applyCoreTextEdits(t, source, ClassicASPLineCommentEdits("file:///site/default.asp", source, []lsp.Range{selection, selection}))
		uncommented := applyCoreTextEdits(t, commented, ClassicASPLineCommentEdits("file:///site/default.asp", commented, []lsp.Range{rangeForNeedle(t, commented, commented)}))
		if uncommented != source {
			t.Fatalf("fake ASP delimiter round trip mismatch:\nsource: %s\ncommented: %s\nuncommented: %s", source, commented, uncommented)
		}
	}
}

func TestClassicASPLineCommentEditsRoundTripsHTMLAfterVBScript(t *testing.T) {
	for _, source := range []string{
		`<% value = 1 %><p>after</p>`,
		`<div><% value = 1 %><span>after</span></div>`,
		`<% If ready Then %><div>ready</div><% End If %>`,
		`<% Response.Write value %>after`,
		`<% Response.Write "<!--" %><p>after</p>`,
		`<% Response.Write "/*" %><p>after</p>`,
		`<% Response.Write value %><!-- existing --><p>after</p>`,
		`<script runat="server">value = 1</script><p>after</p>`,
	} {
		t.Run(source, func(t *testing.T) {
			selection := rangeForNeedle(t, source, source)
			commented := applyCoreTextEdits(t, source, ClassicASPLineCommentEdits("file:///site/default.asp", source, []lsp.Range{selection}))
			uncommented := applyCoreTextEdits(t, commented, ClassicASPLineCommentEdits("file:///site/default.asp", commented, []lsp.Range{rangeForNeedle(t, commented, commented)}))
			if uncommented != source {
				t.Fatalf("VBScript/HTML round trip mismatch:\nsource: %s\ncommented: %s\nuncommented: %s", source, commented, uncommented)
			}
		})
	}
}

func TestClassicASPLineCommentEditsRoundTripsHTMLLinesAfterVBScript(t *testing.T) {
	for _, source := range []string{
		"<%\n  value = 1\n%>\n<div>after</div>",
		"<%\n  value = 1 %><div>after</div>\n",
		"<% If ready Then\n  Response.Write ready %><div>after</div>\n",
		"<% If ready Then %>\n  <div>ready</div>\n<% End If %>\n<p>after</p>",
		"<script runat=\"server\">\n  value = 1\n</script>\n<p>after</p>",
		"<% value = 1 %>\n\n<p>after</p>",
	} {
		t.Run(source, func(t *testing.T) {
			doc := NewTextDocument("file:///site/default.asp", "classic-asp", 0, source)
			selection := lsp.Range{Start: lsp.Position{}, End: doc.PositionAt(len(source))}
			commented := applyCoreTextEdits(t, source, ClassicASPLineCommentEdits(doc.URI, source, []lsp.Range{selection, selection}))
			commentedDoc := NewTextDocument(doc.URI, "classic-asp", 0, commented)
			commentedSelection := lsp.Range{Start: lsp.Position{}, End: commentedDoc.PositionAt(len(commented))}
			uncommented := applyCoreTextEdits(t, commented, ClassicASPLineCommentEdits(doc.URI, commented, []lsp.Range{commentedSelection}))
			if uncommented != source {
				t.Fatalf("VBScript/following HTML line round trip mismatch:\nsource:\n%s\ncommented:\n%s\nuncommented:\n%s", source, commented, uncommented)
			}
		})
	}
}

func TestClassicASPLineCommentEditsUncommentsFormattedMarkersAfterVBScript(t *testing.T) {
	source := `<% value = 1 %><p>after</p>`
	selection := rangeForNeedle(t, source, source)
	commented := applyCoreTextEdits(t, source, ClassicASPLineCommentEdits("file:///site/default.asp", source, []lsp.Range{selection}))
	commented = strings.ReplaceAll(commented, "<!-- ", "<!--\t  ")
	commented = strings.ReplaceAll(commented, " -->", "  \t-->")

	uncommented := applyCoreTextEdits(t, commented, ClassicASPLineCommentEdits("file:///site/default.asp", commented, []lsp.Range{rangeForNeedle(t, commented, commented)}))
	if uncommented != source {
		t.Fatalf("formatted VBScript/HTML comment markers were not fully removed:\ncommented: %s\nuncommented: %s", commented, uncommented)
	}
}

func TestClassicASPLineCommentEditsUncommentsEnclosingMultilineBlockComments(t *testing.T) {
	tests := []struct {
		name      string
		source    string
		selection string
		expected  string
	}{
		{
			name:      "html",
			source:    "<!--\n<section>\n  <p>ready</p>\n</section>\n-->",
			selection: "<section>\n  <p>ready</p>\n</section>",
			expected:  "<!--\n-->\n<section>\n  <p>ready</p>\n</section>\n<!--\n-->",
		},
		{
			name:      "css",
			source:    "<style>\n  /*\n  .first { color: red; }\n  .second { color: blue; }\n  */\n</style>",
			selection: ".first { color: red; }\n  .second { color: blue; }",
			expected:  "<style>\n  /*\n*/\n  .first { color: red; }\n  .second { color: blue; }\n/*\n  */\n</style>",
		},
		{
			name:      "javascript",
			source:    "<script>\n  /*\n  const first = 1;\n  const second = 2;\n  */\n</script>",
			selection: "const first = 1;\n  const second = 2;",
			expected:  "<script>\n  /*\n*/\n  const first = 1;\n  const second = 2;\n/*\n  */\n</script>",
		},
	}
	for _, test := range tests {
		t.Run(test.name, func(t *testing.T) {
			selection := rangeForNeedle(t, test.source, test.selection)
			uncommented := applyCoreTextEdits(t, test.source, ClassicASPLineCommentEdits("file:///site/default.asp", test.source, []lsp.Range{selection}))
			if uncommented != test.expected {
				t.Fatalf("multiline comment was not removed:\n got:\n%s\nwant:\n%s", uncommented, test.expected)
			}
			rejoinSelection := rangeForNeedle(t, uncommented, test.selection)
			rejoined := applyCoreTextEdits(t, uncommented, ClassicASPLineCommentEdits("file:///site/default.asp", uncommented, []lsp.Range{rejoinSelection}))
			if rejoined != test.source {
				t.Fatalf("multiline selection did not rejoin its original comment:\n got:\n%s\nwant:\n%s", rejoined, test.source)
			}
			resplitSelection := rangeForNeedle(t, rejoined, test.selection)
			resplit := applyCoreTextEdits(t, rejoined, ClassicASPLineCommentEdits("file:///site/default.asp", rejoined, []lsp.Range{resplitSelection}))
			if resplit != test.expected {
				t.Fatalf("multiline selection did not remain stable:\n got:\n%s\nwant:\n%s", resplit, test.expected)
			}
		})
	}
}

func TestClassicASPLineCommentEditsPreservesCRLFWhenSplittingBlockComments(t *testing.T) {
	tests := []struct {
		name   string
		source string
		want   string
	}{
		{
			name:   "HTML",
			source: "<!--\r\nselected\r\n-->",
			want:   "<!--\r\n-->\r\nselected\r\n<!--\r\n-->",
		},
		{
			name:   "CSS",
			source: "<style>\r\n/*\r\nselected\r\n*/\r\n</style>",
			want:   "<style>\r\n/*\r\n*/\r\nselected\r\n/*\r\n*/\r\n</style>",
		},
	}
	for _, test := range tests {
		t.Run(test.name, func(t *testing.T) {
			commented := applyCoreTextEdits(t, test.source, ClassicASPLineCommentEdits(
				"file:///site/default.asp", test.source, []lsp.Range{rangeForNeedle(t, test.source, "selected")},
			))
			if commented != test.want {
				t.Fatalf("CRLF block split = %q, want %q", commented, test.want)
			}
			uncommented := applyCoreTextEdits(t, commented, ClassicASPLineCommentEdits(
				"file:///site/default.asp", commented, []lsp.Range{rangeForNeedle(t, commented, "selected")},
			))
			if uncommented != test.source {
				t.Fatalf("CRLF block round trip = %q, want %q", uncommented, test.source)
			}
		})
	}
}

func TestClassicASPLineCommentEditsIgnoresBlockMarkersInEmbeddedStrings(t *testing.T) {
	tests := []struct {
		name     string
		source   string
		selected string
	}{
		{
			name:     "javascript slash markers",
			source:   "<script>\nconst before = \"/*\";\nconst selected = 1;\nconst after = \"*/\";\n</script>",
			selected: "const selected = 1;",
		},
		{
			name:     "javascript html markers",
			source:   "<script>\nconst before = \"<!--\";\nconst selected = 1;\nconst after = \"-->\";\n</script>",
			selected: "const selected = 1;",
		},
		{
			name:     "css slash markers",
			source:   "<style>\n.before { content: \"/*\"; }\n.selected { color: red; }\n.after { content: \"*/\"; }\n</style>",
			selected: ".selected { color: red; }",
		},
	}
	for _, test := range tests {
		t.Run(test.name, func(t *testing.T) {
			selection := rangeForNeedle(t, test.source, test.selected)
			commented := applyCoreTextEdits(t, test.source, ClassicASPLineCommentEdits("file:///site/default.asp", test.source, []lsp.Range{selection}))
			if !strings.Contains(commented, test.selected) || commented == test.source {
				t.Fatalf("selected line was not commented:\n%s", commented)
			}
			for _, literal := range []string{`"/*"`, `"*/"`, `"<!--"`, `"-->"`} {
				if strings.Contains(test.source, literal) && !strings.Contains(commented, literal) {
					t.Fatalf("string marker %s changed:\n%s", literal, commented)
				}
			}
			commentedSelection := rangeForNeedle(t, commented, test.selected)
			uncommented := applyCoreTextEdits(t, commented, ClassicASPLineCommentEdits("file:///site/default.asp", commented, []lsp.Range{commentedSelection}))
			if uncommented != test.source {
				t.Fatalf("embedded string marker round trip mismatch:\nsource:\n%s\ncommented:\n%s\nuncommented:\n%s", test.source, commented, uncommented)
			}
		})
	}
}

func TestClassicASPLineCommentEditsIgnoresPaddedBlockMarkersInEmbeddedStrings(t *testing.T) {
	tests := []struct {
		name     string
		source   string
		selected string
	}{
		{
			name:     "JavaScript HTML markers",
			source:   "<script>\nconst before = \" <!-- \";\nselected\nconst after = \" --> \";\n</script>",
			selected: "selected",
		},
		{
			name:     "JavaScript CSS markers",
			source:   "<script>\nconst before = \" /* \";\nselected\nconst after = \" */ \";\n</script>",
			selected: "selected",
		},
		{
			name:     "CSS HTML markers",
			source:   "<style>\n.before { content: \" <!-- \"; }\nselected { color: red; }\n.after { content: \" --> \"; }\n</style>",
			selected: "selected { color: red; }",
		},
	}
	for _, test := range tests {
		t.Run(test.name, func(t *testing.T) {
			selection := rangeForNeedle(t, test.source, test.selected)
			commented := applyCoreTextEdits(t, test.source, ClassicASPLineCommentEdits(
				"file:///site/default.asp", test.source, []lsp.Range{selection},
			))
			if !strings.Contains(commented, test.selected) || strings.Contains(commented, "\n-->\n"+test.selected) || strings.Contains(commented, "\n*/\n"+test.selected) {
				t.Fatalf("padded string marker changed the comment scope:\n%s", commented)
			}
			uncommented := applyCoreTextEdits(t, commented, ClassicASPLineCommentEdits(
				"file:///site/default.asp", commented, []lsp.Range{rangeForNeedle(t, commented, test.selected)},
			))
			if uncommented != test.source {
				t.Fatalf("padded string marker round trip = %q, want %q", uncommented, test.source)
			}
		})
	}
}

func TestClassicASPLineCommentEditsIgnoresBlockMarkersInJavaScriptRegexLiterals(t *testing.T) {
	tests := []string{
		"<script>\nconst before = /<!--/;\nselected\nconst after = /-->/;\n</script>",
		"<script>\nconst before = /[/*]/;\nselected\nconst after = /[*/]/;\n</script>",
	}
	for _, source := range tests {
		t.Run(source, func(t *testing.T) {
			selection := rangeForNeedle(t, source, "selected")
			commented := applyCoreTextEdits(t, source, ClassicASPLineCommentEdits(
				"file:///site/default.asp", source, []lsp.Range{selection},
			))
			if !strings.Contains(commented, "// selected") || strings.Contains(commented, "\n-->\nselected") || strings.Contains(commented, "\n*/\nselected") {
				t.Fatalf("regex literal marker changed the comment scope:\n%s", commented)
			}
			uncommented := applyCoreTextEdits(t, commented, ClassicASPLineCommentEdits(
				"file:///site/default.asp", commented, []lsp.Range{rangeForNeedle(t, commented, "// selected")},
			))
			if uncommented != source {
				t.Fatalf("regex literal marker round trip = %q, want %q", uncommented, source)
			}
		})
	}
}

func TestClassicASPLineCommentEditsHandlesJavaScriptLegacyHTMLLineComments(t *testing.T) {
	source := "<script>\n<!--\nselected\n-->\n</script>"
	selection := rangeForNeedle(t, source, "selected")
	commented := applyCoreTextEdits(t, source, ClassicASPLineCommentEdits(
		"file:///site/default.asp", source, []lsp.Range{selection},
	))
	want := "<script>\n<!--\n// selected\n-->\n</script>"
	if commented != want {
		t.Fatalf("JavaScript legacy HTML line comment = %q, want %q", commented, want)
	}
	uncommented := applyCoreTextEdits(t, commented, ClassicASPLineCommentEdits(
		"file:///site/default.asp", commented, []lsp.Range{rangeForNeedle(t, commented, "// selected")},
	))
	if uncommented != source {
		t.Fatalf("JavaScript legacy HTML line comment round trip = %q, want %q", uncommented, source)
	}
}

func TestClassicASPLineCommentEditsKeepsHTMLCommentScopeAfterScript(t *testing.T) {
	source := "<script>const ready = true;</script>\n<!--\nselected\n-->"
	commented := applyCoreTextEdits(t, source, ClassicASPLineCommentEdits(
		"file:///site/default.asp", source, []lsp.Range{rangeForNeedle(t, source, "selected")},
	))
	want := "<script>const ready = true;</script>\n<!--\n-->\nselected\n<!--\n-->"
	if commented != want {
		t.Fatalf("HTML comment after script = %q, want %q", commented, want)
	}
	uncommented := applyCoreTextEdits(t, commented, ClassicASPLineCommentEdits(
		"file:///site/default.asp", commented, []lsp.Range{rangeForNeedle(t, commented, "selected")},
	))
	if uncommented != source {
		t.Fatalf("HTML comment after script round trip = %q, want %q", uncommented, source)
	}
}

func TestClassicASPLineCommentEditsIgnoresMarkersInMultilineHTMLAttributes(t *testing.T) {
	source := "<div\n title=\"\n <!--\n\"\n>selected</div>\n<div\n title=\"\n -->\n\"\n>after</div>"
	selection := rangeForNeedle(t, source, "selected")
	commented := applyCoreTextEdits(t, source, ClassicASPLineCommentEdits(
		"file:///site/default.asp", source, []lsp.Range{selection},
	))
	want := "<div\n title=\"\n <!--\n\"\n<!-- >selected</div> -->\n<div\n title=\"\n -->\n\"\n>after</div>"
	if commented != want {
		t.Fatalf("multiline HTML attribute marker = %q, want %q", commented, want)
	}
	uncommented := applyCoreTextEdits(t, commented, ClassicASPLineCommentEdits(
		"file:///site/default.asp", commented, []lsp.Range{rangeForNeedle(t, commented, "selected")},
	))
	if uncommented != source {
		t.Fatalf("multiline HTML attribute marker round trip = %q, want %q", uncommented, source)
	}
}

func TestClassicASPLineCommentEditsIgnoresBlockMarkersInEmbeddedLineComments(t *testing.T) {
	tests := []struct {
		name   string
		source string
	}{
		{
			name:   "HTML markers",
			source: "<script>\n// <!--\nselected\n// -->\n</script>",
		},
		{
			name:   "CSS markers",
			source: "<script>\n// /*\nselected\n// */\n</script>",
		},
	}
	for _, test := range tests {
		t.Run(test.name, func(t *testing.T) {
			selection := rangeForNeedle(t, test.source, "selected")
			commented := applyCoreTextEdits(t, test.source, ClassicASPLineCommentEdits("file:///site/default.asp", test.source, []lsp.Range{selection}))
			if strings.Contains(commented, "\n-->\nselected") || strings.Contains(commented, "\n*/\nselected") {
				t.Fatalf("block boundary was inserted around a line comment:\n%s", commented)
			}
			if !strings.Contains(commented, "// selected") {
				t.Fatalf("selected line was not commented:\n%s", commented)
			}
			commentedSelection := rangeForNeedle(t, commented, "// selected")
			uncommented := applyCoreTextEdits(t, commented, ClassicASPLineCommentEdits("file:///site/default.asp", commented, []lsp.Range{commentedSelection}))
			if uncommented != test.source {
				t.Fatalf("line comment marker round trip mismatch:\nsource:\n%s\ncommented:\n%s\nuncommented:\n%s", test.source, commented, uncommented)
			}
		})
	}
}

func TestClassicASPLineCommentEditsIgnoresCrossLanguageBlockMarkers(t *testing.T) {
	tests := []struct {
		name      string
		source    string
		commented string
	}{
		{
			name:      "HTML markers inside JavaScript block comment",
			source:    "<script>\n/* <!--\nselected\n--> */\n</script>",
			commented: "<script>\n/* <!--\n*/\nselected\n/*\n--> */\n</script>",
		},
		{
			name:      "CSS markers inside HTML block comment",
			source:    "<!-- /*\nselected\n*/ -->",
			commented: "<!-- /*\n-->\nselected\n<!--\n*/ -->",
		},
	}
	for _, test := range tests {
		t.Run(test.name, func(t *testing.T) {
			selection := rangeForNeedle(t, test.source, "selected")
			commented := applyCoreTextEdits(t, test.source, ClassicASPLineCommentEdits(
				"file:///site/default.asp", test.source, []lsp.Range{selection},
			))
			if commented != test.commented {
				t.Fatalf("cross-language block toggle = %q, want %q", commented, test.commented)
			}
			uncommented := applyCoreTextEdits(t, commented, ClassicASPLineCommentEdits(
				"file:///site/default.asp", commented, []lsp.Range{rangeForNeedle(t, commented, "selected")},
			))
			if uncommented != test.source {
				t.Fatalf("cross-language block round trip = %q, want %q", uncommented, test.source)
			}
		})
	}
}

func TestClassicASPLineCommentPlanCompletesASPLayerInsideHTMLComment(t *testing.T) {
	source := "<!-- <%= title %> -->"
	selection := rangeForNeedle(t, source, source)
	first := applyCoreTextEdits(t, source, ClassicASPLineCommentPlan("file:///site/default.asp", source, []lsp.Range{selection}, Settings{}).Edits)
	want := "<!-- <!-- <%'= title %> --> -->"
	if first != want {
		t.Fatalf("first toggle = %q", first)
	}
	second := applyCoreTextEdits(t, first, ClassicASPLineCommentPlan("file:///site/default.asp", first, []lsp.Range{wholeCommentRange(first)}, Settings{}).Edits)
	if second != source {
		t.Fatalf("second toggle = %q", second)
	}
}

func TestClassicASPLineCommentPlanUsesVSCodeRawBlockCommentDelimiters(t *testing.T) {
	for _, test := range []struct {
		source string
		want   string
	}{
		{
			source: "<style>\n.x::after { content: \"*/\"; color: red; }\n</style>",
			want:   "<style>\n/* .x::after { content: \"*/\"; color: red; } */\n</style>",
		},
		{
			source: "<p>arrow --> text</p>",
			want:   "<!-- <p>arrow --> text</p> -->",
		},
	} {
		t.Run(test.source, func(t *testing.T) {
			source := test.source
			needle := source
			if strings.Contains(source, "content:") {
				needle = ".x::after"
			} else if strings.Contains(source, "arrow") {
				needle = "arrow"
			}
			selection := rangeForNeedle(t, source, needle)
			commented := applyCoreTextEdits(t, source, ClassicASPLineCommentPlan("file:///site/default.asp", source, []lsp.Range{selection}, Settings{}).Edits)
			if commented != test.want {
				t.Fatalf("raw block comment = %q, want %q", commented, test.want)
			}
			uncommented := applyCoreTextEdits(t, commented, ClassicASPLineCommentPlan("file:///site/default.asp", commented, []lsp.Range{rangeForNeedle(t, commented, needle)}, Settings{}).Edits)
			if uncommented != source {
				t.Fatalf("round trip mismatch:\n got: %s\nwant: %s", uncommented, source)
			}
		})
	}
}

func TestClassicASPLineCommentPlanStacksREMComment(t *testing.T) {
	source := "<% REM hidden value %>"
	selection := rangeForNeedle(t, source, source)
	plan := ClassicASPLineCommentPlan("file:///site/default.asp", source, []lsp.Range{selection}, Settings{})
	if got := applyCoreTextEdits(t, source, plan.Edits); got != "<%' REM hidden value %>" {
		t.Fatalf("REM comment = %q", got)
	}
}

func TestClassicASPLineCommentPlanUsesConfiguredJScriptLanguage(t *testing.T) {
	source := `<% Response.Write("ready"); %>`
	selection := rangeForNeedle(t, source, source)
	commented := applyCoreTextEdits(t, source, ClassicASPLineCommentPlan("file:///site/default.asp", source, []lsp.Range{selection}, Settings{DefaultLanguage: "JScript"}).Edits)
	if commented != `<%// Response.Write("ready"); %>` {
		t.Fatalf("JScript comment = %q", commented)
	}
}

func TestClassicASPLineCommentPlanPreservesLegacyFragmentSpacing(t *testing.T) {
	tests := []struct {
		name      string
		source    string
		commented string
	}{
		{
			name:      "space-separated fragment",
			source:    "<!-- x --><% value %><!-- z -->",
			commented: "<!-- x --><%' value %><!-- z -->",
		},
		{
			name:      "tab-separated fragment",
			source:    "<!--\tx\t--><%\tvalue %><!--\tz\t-->",
			commented: "<!--\tx\t--><%'\tvalue %><!--\tz\t-->",
		},
	}
	for _, test := range tests {
		t.Run(test.name, func(t *testing.T) {
			selection := rangeForNeedle(t, test.source, test.source)
			commented := applyCoreTextEdits(t, test.source, ClassicASPLineCommentPlan(
				"file:///site/default.asp", test.source, []lsp.Range{selection}, Settings{},
			).Edits)
			if commented != test.commented {
				t.Fatalf("legacy fragment comment = %q, want %q", commented, test.commented)
			}
		})
	}
}

func TestClassicASPLineCommentPlanRejectsPartialDirectiveSelection(t *testing.T) {
	source := "<%@ LANGUAGE=\"JScript\" %>\n<%= title %>"
	plan := ClassicASPLineCommentPlan("file:///site/default.asp", source, []lsp.Range{rangeForNeedle(t, source, "LANGUAGE")}, Settings{DefaultLanguage: "VBScript"})
	if len(plan.Edits) != 0 || plan.NoOpReason != "directive-requires-full-document" {
		t.Fatalf("directive plan = %#v", plan)
	}
}

func TestClassicASPLineCommentPlanCommentsWholeDirectiveDocumentInFallbackLanguage(t *testing.T) {
	source := "<%@ LANGUAGE=\"JScript\" %>\n<%= title %>"
	doc := NewTextDocument("file:///site/default.asp", "classic-asp", 0, source)
	selection := lsp.Range{Start: lsp.Position{}, End: doc.PositionAt(len(source))}
	commented := applyCoreTextEdits(t, source, ClassicASPLineCommentPlan(doc.URI, source, []lsp.Range{selection}, Settings{DefaultLanguage: "VBScript"}).Edits)
	if !strings.Contains(commented, "<%'@ LANGUAGE=\"JScript\" %>") || !strings.Contains(commented, "<%'= title %>") {
		t.Fatalf("whole directive document did not use VBScript comments:\n%s", commented)
	}
	commentedDoc := NewTextDocument(doc.URI, "classic-asp", 0, commented)
	if restored := applyCoreTextEdits(t, commented, ClassicASPLineCommentPlan(doc.URI, commented, []lsp.Range{{Start: lsp.Position{}, End: commentedDoc.PositionAt(len(commented))}}, Settings{DefaultLanguage: "VBScript"}).Edits); restored != source {
		t.Fatalf("whole directive document did not restore its comments:\n%s", restored)
	}
}

func TestClassicASPLineCommentPlanTogglesLineCommentsAndKeepsWhitespaceNatural(t *testing.T) {
	for _, test := range []struct {
		source     string
		wantFirst  string
		wantSecond string
	}{
		{source: "a", wantFirst: "' a", wantSecond: "a"},
		{source: "'a", wantFirst: "a", wantSecond: "' a"},
		{source: "' a", wantFirst: "a", wantSecond: "' a"},
		{source: "  a", wantFirst: "  ' a", wantSecond: "  a"},
		{source: "<% value = 1 %>", wantFirst: "<%' value = 1 %>", wantSecond: "<% value = 1 %>"},
	} {
		t.Run(test.source, func(t *testing.T) {
			selection := rangeForNeedle(t, test.source, test.source)
			uri := "file:///site/default.vbs"
			if strings.Contains(test.source, "<%") {
				uri = "file:///site/default.asp"
			}
			got := applyCoreTextEdits(t, test.source, ClassicASPLineCommentPlan(uri, test.source, []lsp.Range{selection}, Settings{}).Edits)
			if got != test.wantFirst {
				t.Fatalf("first toggle = %q, want %q", got, test.wantFirst)
			}
			second := applyCoreTextEdits(t, got, ClassicASPLineCommentPlan(uri, got, []lsp.Range{rangeForNeedle(t, got, got)}, Settings{}).Edits)
			if second != test.wantSecond {
				t.Fatalf("second toggle = %q, want %q", second, test.wantSecond)
			}
		})
	}
}

func rangeForNeedle(t *testing.T, source, needle string) lsp.Range {
	t.Helper()
	offset := strings.Index(source, needle)
	if offset < 0 {
		t.Fatalf("needle %q not found", needle)
	}
	return lsp.Range{
		Start: positionForOffset(source, offset),
		End:   positionForOffset(source, offset+len(needle)),
	}
}

func positionForOffset(source string, offset int) lsp.Position {
	doc := NewTextDocument("file:///site/default.asp", "classic-asp", 0, source)
	return doc.PositionAt(offset)
}

func applyCoreTextEdits(t *testing.T, source string, edits []lsp.TextEdit) string {
	t.Helper()
	sort.SliceStable(edits, func(i, j int) bool {
		left := NewTextDocument("file:///site/default.asp", "classic-asp", 0, source).OffsetAt(edits[i].Range.Start)
		right := NewTextDocument("file:///site/default.asp", "classic-asp", 0, source).OffsetAt(edits[j].Range.Start)
		return left > right
	})
	result := source
	for _, edit := range edits {
		doc := NewTextDocument("file:///site/default.asp", "classic-asp", 0, result)
		start := doc.OffsetAt(edit.Range.Start)
		end := doc.OffsetAt(edit.Range.End)
		result = result[:start] + edit.NewText + result[end:]
	}
	return result
}
