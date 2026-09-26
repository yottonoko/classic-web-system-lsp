package htmlservice

import "testing"

func formatText(input string, insertSpaces bool) string {
	return formatTextWithOptions(input, HTMLFormatConfiguration{TabSize: 2, InsertSpaces: insertSpaces})
}

func formatTextWithOptions(input string, options HTMLFormatConfiguration) string {
	ls := GetLanguageService()
	rangeStart := stringsIndex(input, "|")
	var r *Range
	if rangeStart >= 0 {
		rangeEnd := lastIndex(input, "|")
		input = input[:rangeStart] + input[rangeStart+1:rangeEnd] + input[rangeEnd+1:]
		docForRange := NewTextDocument("test://test.html", "html", 0, input)
		rr := NewRange(docForRange.PositionAt(rangeStart), docForRange.PositionAt(rangeEnd-1))
		r = &rr
	}
	doc := NewTextDocument("test://test.html", "html", 0, input)
	edits := ls.Format(doc, r, options)
	return ApplyEdits(doc, edits)
}

func stringPtrForTest(value string) *string {
	return &value
}

func TestFormatterFullDocumentAndRange(t *testing.T) {
	input := "<div  class = \"foo\">\n<br>\n </div>"
	expected := "<div class=\"foo\">\n  <br>\n</div>"
	if got := formatText(input, true); got != expected {
		t.Fatalf("full format:\n%q\nwant:\n%q", got, expected)
	}
	input = "<div  class = \"foo\">\n  |<img  src = \"foo\">|\n </div>"
	expected = "<div  class = \"foo\">\n  <img src=\"foo\">\n </div>"
	if got := formatText(input, true); got != expected {
		t.Fatalf("range format:\n%q\nwant:\n%q", got, expected)
	}
}

func TestFormatterRangeInsideTagNoopAndEmbeddedCSS(t *testing.T) {
	input := "<div|  class = \"foo\">\n  <img  src = \"foo\">\n </div>|"
	expected := "<div  class = \"foo\">\n  <img  src = \"foo\">\n </div>"
	if got := formatText(input, true); got != expected {
		t.Fatalf("inside tag noop:\n%q\nwant:\n%q", got, expected)
	}
	input = "<style>h1, h2 { color:red; } div>span { color: blue; }</style>"
	expected = "<style>\n  h1,\n  h2 {\n    color: red;\n  }\n\n  div>span {\n    color: blue;\n  }\n</style>"
	if got := formatText(input, true); got != expected {
		t.Fatalf("css format:\n%q\nwant:\n%q", got, expected)
	}
}

func TestFormatterAdditionalOptions(t *testing.T) {
	indentInnerHTML := true
	got := formatTextWithOptions(`<html><body></body></html>`, HTMLFormatConfiguration{TabSize: 2, InsertSpaces: true, IndentInnerHTML: &indentInnerHTML})
	want := "<html>\n\n  <body></body>\n\n</html>"
	if got != want {
		t.Fatalf("indentInnerHTML:\n%q\nwant:\n%q", got, want)
	}

	preserveNewLines := false
	got = formatTextWithOptions("<div>\n\n<span></span>\n\n</div>", HTMLFormatConfiguration{TabSize: 2, InsertSpaces: true, PreserveNewLines: &preserveNewLines})
	want = "<div>\n  <span></span>\n</div>"
	if got != want {
		t.Fatalf("preserveNewLines false:\n%q\nwant:\n%q", got, want)
	}

	got = formatTextWithOptions(`<div class="a" id="b"></div>`, HTMLFormatConfiguration{TabSize: 2, InsertSpaces: true, WrapAttributes: stringPtrForTest("force")})
	want = "<div class=\"a\"\n  id=\"b\"></div>"
	if got != want {
		t.Fatalf("wrapAttributes force:\n%q\nwant:\n%q", got, want)
	}

	got = formatTextWithOptions(`<div class="a" id="b"></div>`, HTMLFormatConfiguration{TabSize: 2, InsertSpaces: true, WrapAttributes: stringPtrForTest("force-expand-multiline")})
	want = "<div\n  class=\"a\"\n  id=\"b\"\n></div>"
	if got != want {
		t.Fatalf("wrapAttributes force-expand-multiline:\n%q\nwant:\n%q", got, want)
	}

	wrapLineLength := 120
	got = formatTextWithOptions(`<div class="a" id="b"></div>`, HTMLFormatConfiguration{TabSize: 2, InsertSpaces: true, WrapAttributes: stringPtrForTest("aligned-multiple"), WrapLineLength: &wrapLineLength})
	want = `<div class="a" id="b"></div>`
	if got != want {
		t.Fatalf("wrapAttributes aligned-multiple below limit:\n%q\nwant:\n%q", got, want)
	}

	got = formatTextWithOptions("<div\n  class=\"a\"\n  id=\"b\"></div>", HTMLFormatConfiguration{TabSize: 2, InsertSpaces: true, WrapAttributes: stringPtrForTest("preserve")})
	want = "<div\n  class=\"a\"\n  id=\"b\"></div>"
	if got != want {
		t.Fatalf("wrapAttributes preserve:\n%q\nwant:\n%q", got, want)
	}

	separator := false
	got = formatTextWithOptions(`<style>div>span { color:red; }</style>`, HTMLFormatConfiguration{TabSize: 2, InsertSpaces: true, CSS: &EmbeddedCSSFormatConfiguration{SpaceAroundSelectorSeparator: &separator}})
	want = "<style>\n  div>span {\n    color: red;\n  }\n</style>"
	if got != want {
		t.Fatalf("css selector separator:\n%q\nwant:\n%q", got, want)
	}

	got = formatTextWithOptions(`<style>h1 { color:red; }</style>`, HTMLFormatConfiguration{TabSize: 2, InsertSpaces: true, CSS: &EmbeddedCSSFormatConfiguration{BraceStyle: stringPtrForTest("expand")}})
	want = "<style>\n  h1\n  {\n    color: red;\n  }\n</style>"
	if got != want {
		t.Fatalf("css braceStyle expand:\n%q\nwant:\n%q", got, want)
	}
}

func lastIndex(s, sep string) int {
	last := -1
	for i := 0; i+len(sep) <= len(s); i++ {
		if s[i:i+len(sep)] == sep {
			last = i
		}
	}
	return last
}
