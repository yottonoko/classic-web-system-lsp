package embedded

import (
	"strings"
	"testing"

	htmlservice "github.com/yottonoko/vscode-html-languageservice-go"

	"github.com/yottonoko/classic-web-system-lsp/internal/core"
)

func TestHTMLHoverMapsRangeBackToASPSource(t *testing.T) {
	source := `<% If enabled Then %><html><body></body></html>`
	parsed := core.ParseDocument("file:///site/hover.asp", source, core.Settings{})
	doc := core.NewTextDocument(parsed.URI, "classic-asp", 0, source)
	start := strings.Index(source, "html")
	hover := NewHTML().Hover(parsed, doc.PositionAt(start+1))
	if hover == nil || hover.Range == nil {
		t.Fatalf("hover = %#v, want a mapped range", hover)
	}
	want := doc.Range(start, start+len("html"))
	if *hover.Range != want {
		t.Fatalf("hover range = %#v, want %#v", *hover.Range, want)
	}
}

func TestRemapHTMLRangeRejectsRangesAcrossMaskedASPHoles(t *testing.T) {
	source := `<div><%= dynamicTag %></div>`
	parsed := core.ParseDocument("file:///site/html-range.asp", source, core.Settings{})
	virtual := core.BuildVirtualDocument(parsed, core.LanguageHTML)
	sourceDocument := core.NewTextDocument(parsed.URI, "classic-asp", 0, source)
	virtualDocument := core.NewTextDocument(virtual.URI, "html", 0, virtual.Text)
	start := strings.Index(virtual.Text, "<div>")
	end := strings.Index(virtual.Text, "</div>") + len("</div>")

	if mapped, ok := remapHTMLRange(virtual, sourceDocument, htmlservice.Range{
		Start: htmlservice.Position(virtualDocument.PositionAt(start)),
		End:   htmlservice.Position(virtualDocument.PositionAt(end)),
	}); ok {
		t.Fatalf("cross-hole HTML range mapped to %#v", mapped)
	}
	if symbols := NewHTML().DocumentSymbols(parsed); len(symbols) != 0 {
		t.Fatalf("HTML symbols = %#v, want no symbol containing a masked ASP hole", symbols)
	}
}

func TestRemapHTMLRangePreservesUTF16OffsetsAfterMaskedASPHole(t *testing.T) {
	source := `😀<div><%= dynamicTag %></div>`
	parsed := core.ParseDocument("file:///site/html-range-utf16.asp", source, core.Settings{})
	virtual := core.BuildVirtualDocument(parsed, core.LanguageHTML)
	sourceDocument := core.NewTextDocument(parsed.URI, "classic-asp", 0, source)
	virtualDocument := core.NewTextDocument(virtual.URI, "html", 0, virtual.Text)
	start := strings.Index(virtual.Text, "</div>")
	end := start + len("</div>")
	mapped, ok := remapHTMLRange(virtual, sourceDocument, htmlservice.Range{
		Start: htmlservice.Position(virtualDocument.PositionAt(start)),
		End:   htmlservice.Position(virtualDocument.PositionAt(end)),
	})
	wantStart := strings.Index(source, "</div>")
	want := sourceDocument.Range(wantStart, wantStart+len("</div>"))
	if !ok || mapped != want {
		t.Fatalf("HTML range after masked ASP hole = %#v, ok=%t, want %#v", mapped, ok, want)
	}
}

func TestFormatHTMLRoutesEmbeddedCSSSettings(t *testing.T) {
	source := `<style>h1,h2{color:red}</style>`
	defaultFormatted, err := FormatHTML(source, core.FormattingOptions{TabSize: 2, InsertSpaces: true})
	if err != nil {
		t.Fatal(err)
	}
	newlineBetweenSelectors := false
	compactFormatted, err := FormatHTML(source, core.FormattingOptions{
		TabSize:                    2,
		InsertSpaces:               true,
		CSSNewlineBetweenSelectors: &newlineBetweenSelectors,
	})
	if err != nil {
		t.Fatal(err)
	}
	if defaultFormatted == compactFormatted || !strings.Contains(compactFormatted, "h1, h2") {
		t.Fatalf("embedded CSS setting was not routed:\ndefault=%q\ncompact=%q", defaultFormatted, compactFormatted)
	}
}

func TestFormattersTolerateOutOfRangeOptions(t *testing.T) {
	negative := -5
	source := "<div class=\"a\" id=\"b\"><style>a{b:c}</style><script>var a=1;</script><p>x</p>\n\n\n</div>"
	for name, options := range map[string]core.FormattingOptions{
		"unknown wrap attributes": {HTMLWrapAttributes: "bogus"},
		"unknown brace style":     {CSSBraceStyle: "bogus"},
		"negative indent":         {TabSize: -3, InsertSpaces: true, HTMLTabSize: -1, CSSTabSize: -1, JavaScriptTabSize: -1},
		"oversized indent":        {TabSize: 5000, InsertSpaces: true, HTMLWrapAttributes: "force-aligned", HTMLWrapAttributesIndentSize: 5000},
		"negative newlines":       {MaxPreserveNewLines: &negative},
	} {
		formatted, err := FormatHTML(source, options)
		if err != nil || formatted == "" {
			t.Fatalf("%s: FormatHTML() = %q, %v", name, formatted, err)
		}
		for _, line := range strings.Split(formatted, "\n") {
			if indent := len(line) - len(strings.TrimLeft(line, " ")); indent > 8*32 {
				t.Fatalf("%s: line indented by %d columns", name, indent)
			}
		}
		if _, err := FormatCSS("a{b:c}", options); err != nil {
			t.Fatalf("%s: FormatCSS() error = %v", name, err)
		}
		if _, err := FormatJavaScript("function f(){return 1}", options, core.LanguageJavaScript); err != nil {
			t.Fatalf("%s: FormatJavaScript() error = %v", name, err)
		}
		parsed := core.ParseDocument("file:///site/options.asp", "<% If a Then %>\n<p>x</p>\n<% End If %>\n", core.Settings{})
		options.FormatHTML, options.FormatCSS, options.FormatJavaScript = FormatHTML, FormatCSS, FormatJavaScript
		core.FormatDocument(parsed, options)
	}
}

func TestFormatDocumentDoesNotTurnLiteralTextIntoASPBlocks(t *testing.T) {
	for _, source := range []string{"< %0", "<p>a < %b</p>", "<p>1 < %</p><%= x %>"} {
		parsed := core.ParseDocument("file:///site/literal.asp", source, core.Settings{})
		edits := core.FormatDocument(parsed, core.FormattingOptions{
			TabSize:          2,
			InsertSpaces:     true,
			FormatHTML:       FormatHTML,
			FormatCSS:        FormatCSS,
			FormatJavaScript: FormatJavaScript,
		})
		for _, edit := range edits {
			formatted := core.ParseDocument(parsed.URI, edit.NewText, core.Settings{})
			if len(formatted.Regions) != len(parsed.Regions) || strings.Contains(edit.NewText, "<%b") || strings.Count(edit.NewText, "<%") != strings.Count(source, "<%") {
				t.Fatalf("FormatDocument(%q) = %q, which changes the server regions", source, edit.NewText)
			}
		}
	}
}
