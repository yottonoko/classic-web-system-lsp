package services

import (
	"fmt"
	"reflect"
	"sync"
	"testing"

	beautify "github.com/yottonoko/js-beautify-go"
	"github.com/yottonoko/vscode-css-languageservice-go/lsp"
)

func TestCSSFormatter(t *testing.T) {
	tests := []formatTest{
		{name: "full document", unformatted: "@font-face { src: url(http://test) }\n.monaco  .list { background: \"#FFF\"  ; }", expected: "@font-face {\n  src: url(http://test)\n}\n\n.monaco .list {\n  background: \"#FFF\";\n}"},
		{name: "range", unformatted: "@font-face { src: url(http://test) }\n|.monaco  .list { background: \"#FFF\"  ; }|", expected: "@font-face { src: url(http://test) }\n.monaco .list {\n  background: \"#FFF\";\n}"},
		{name: "range2", unformatted: "div {\n|  color:green|\n}", expected: "div {\n  color: green\n}"},
		{name: "@media", unformatted: "@media print { @page { margin: 10% } blockquote, pre { page-break-inside: avoid } }", expected: "@media print {\n  @page {\n    margin: 10%\n  }\n\n  blockquote,\n  pre {\n    page-break-inside: avoid\n  }\n}"},
		{name: "selectors and functions", unformatted: ".foo,.bar,li:first-of-type + li{--widthB:  calc(  const(--widthA)   / 2);}", expected: ".foo,\n.bar,\nli:first-of-type+li {\n  --widthB: calc(const(--widthA) / 2);\n}"},
		{name: "selectors and functions, options", unformatted: ".foo,.bar,li:first-of-type + li{--widthB:  calc(  const(--widthA)   / 2);}", expected: ".foo, .bar, li:first-of-type + li {\n  --widthB: calc(const(--widthA) / 2);\n}", options: &FormatOptions{NewlineBetweenSelectors: false, InsertSpaces: true, TabSize: 2, SpaceAroundSelectorSeparator: true}},
		{name: "insertFinalNewline", unformatted: ".emptyMarkdownCell::before { outline:  1px  solid -webkit-focus-ring-color;  }", expected: ".emptyMarkdownCell::before {\n  outline: 1px solid -webkit-focus-ring-color;\n}\n", options: &FormatOptions{InsertSpaces: true, TabSize: 2, InsertFinalNewline: true}},
		{name: "preserveNewLines", unformatted: ".foo { display: node;\n\n\n\n}", expected: ".foo {\n  display: node;\n\n\n}", options: &FormatOptions{InsertSpaces: true, TabSize: 2, PreserveNewLines: true, MaxPreserveNewLines: 3}},
		{name: "preserveNewLines false", unformatted: ".foo { display: node;\n\n\n\n}", expected: ".foo {\n  display: node;\n}", options: &FormatOptions{InsertSpaces: true, TabSize: 2, PreserveNewLines: false}},
		{name: "spaces", unformatted: ".body {\n font-size: @fs  !important; // 2 space -> BUG\n}", expected: ".body {\n  font-size: @fs !important; // 2 space -> BUG\n}", options: &FormatOptions{InsertSpaces: true, TabSize: 2, PreserveNewLines: true, MaxPreserveNewLines: 3}},
	}
	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			assertFormat(t, tt.unformatted, tt.expected, tt.options)
		})
	}
	t.Run("braceStyle", func(t *testing.T) {
		assertFormat(t, ".foo { display:  node;  }", ".foo\n{\n  display: node;\n}", &FormatOptions{InsertSpaces: true, TabSize: 2, BraceStyle: "expand"})
		assertFormat(t, ".foo { display:  node;  }", ".foo {\n  display: node;\n}", &FormatOptions{InsertSpaces: true, TabSize: 2, BraceStyle: "collapse"})
	})
}

func TestDialectFormatter(t *testing.T) {
	tests := []formatTest{
		{name: "LESS full document", unformatted: "@leftwrap:200px;\n.box-shadow(@x:0, @y:0, @blur:1px, @color:#000){\n-webkit-box-shadow: @arguments;\n}", expected: "@leftwrap: 200px;\n\n.box-shadow(@x: 0, @y: 0, @blur: 1px, @color: #000) {\n  -webkit-box-shadow: @arguments;\n}"},
		{name: "SCSS full document", unformatted: "@mixin  themable( $theme-name,  $theme-map) {\n  @if ($section == container) {\n.container {background-color: map-get($map, bg);}\n}\n}", expected: "@mixin themable($theme-name, $theme-map) {\n  @if ($section ==container) {\n    .container {\n      background-color: map-get($map, bg);\n    }\n  }\n}"},
	}
	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			assertFormat(t, tt.unformatted, tt.expected, tt.options)
		})
	}
}

func TestCSSFormatterMatchesJSBeautifyGoForFullDocuments(t *testing.T) {
	tests := []struct {
		name        string
		unformatted string
		options     FormatOptions
		beautify    beautify.Options
	}{
		{
			name:        "css at-rules and selectors",
			unformatted: "@media print { @page { margin: 10% } blockquote, pre { page-break-inside: avoid } }",
			options:     FormatOptions{InsertSpaces: true, TabSize: 2},
			beautify: baseJSBeautifyCSSOptions(beautify.Options{
				"indent_size": 2,
			}),
		},
		{
			name:        "scss and comments",
			unformatted: "@mixin  themable( $theme-name,  $theme-map) {\n  @if ($section == container) {\n.container {background-color: map-get($map, bg);}\n}\n}",
			options:     FormatOptions{InsertSpaces: true, TabSize: 2},
			beautify: baseJSBeautifyCSSOptions(beautify.Options{
				"indent_size": 2,
			}),
		},
		{
			name:        "selector separator options",
			unformatted: ".foo,.bar,li:first-of-type + li{--widthB:  calc(  const(--widthA)   / 2);}",
			options:     FormatOptions{InsertSpaces: true, TabSize: 2, NewlineBetweenSelectors: false, SpaceAroundSelectorSeparator: true},
			beautify: baseJSBeautifyCSSOptions(beautify.Options{
				"indent_size":                     2,
				"selector_separator_newline":      false,
				"space_around_selector_separator": true,
				"space_around_combinator":         true,
			}),
		},
		{
			name:        "preserved newlines",
			unformatted: ".foo { display: node;\n\n\n\n}",
			options:     FormatOptions{InsertSpaces: true, TabSize: 2, PreserveNewLines: true, MaxPreserveNewLines: 3},
			beautify: baseJSBeautifyCSSOptions(beautify.Options{
				"indent_size":           2,
				"preserve_newlines":     true,
				"max_preserve_newlines": 3,
			}),
		},
	}
	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			expected := mustBeautifyCSS(t, tt.unformatted, tt.beautify)
			assertFormat(t, tt.unformatted, expected, &tt.options)
		})
	}
}

func TestCSSFormatterIsSafeForConcurrentFormatting(t *testing.T) {
	input := ".foo,.bar{color:red;background:linear-gradient( to right, #fff, #000 );}"
	options := FormatOptions{InsertSpaces: true, TabSize: 2, PreserveNewLines: true, MaxPreserveNewLines: 3}
	expected := mustBeautifyCSS(t, input, baseJSBeautifyCSSOptions(beautify.Options{
		"indent_size":           2,
		"preserve_newlines":     true,
		"max_preserve_newlines": 3,
	}))

	var wg sync.WaitGroup
	errs := make(chan string, 32)
	for i := 0; i < 32; i++ {
		wg.Add(1)
		go func(i int) {
			defer wg.Done()
			doc := lsp.NewTextDocument(lsp.DocumentURI(fmt.Sprintf("test://concurrent-%d.css", i)), "css", i, input)
			got := lsp.ApplyEdits(doc, Format(doc, nil, options))
			if got != expected {
				errs <- fmt.Sprintf("worker %d:\nactual:\n%s\nwant:\n%s", i, got, expected)
			}
		}(i)
	}
	wg.Wait()
	close(errs)
	if err := <-errs; err != "" {
		t.Fatal(err)
	}
}

func TestCSSBeautifierOptionsFromFormatOptions(t *testing.T) {
	options := CSSBeautifierOptionsFromFormatOptions(FormatOptions{
		TabSize:                      2,
		InsertSpaces:                 true,
		InsertFinalNewline:           true,
		NewlineBetweenSelectors:      false,
		SpaceAroundSelectorSeparator: true,
		BraceStyle:                   "expand",
		PreserveNewLines:             true,
		MaxPreserveNewLines:          3,
		WrapLineLength:               80,
		IndentEmptyLines:             true,
	}, 1)

	if options.IndentSize != 2 ||
		options.IndentChar != " " ||
		!options.EndWithNewline ||
		options.SelectorSeparatorNewline ||
		!options.NewlineBetweenRules ||
		!options.SpaceAroundSelectorSeparator ||
		options.BraceStyle != "expand" ||
		!options.PreserveNewlines ||
		options.MaxPreserveNewlines != 3 ||
		options.WrapLineLength != 80 ||
		!options.IndentEmptyLines ||
		options.EOL != "\n" ||
		options.BaseIndentLevel != 1 {
		t.Fatalf("CSSBeautifierOptionsFromFormatOptions = %#v", options)
	}
}

func TestCSSBeautifierOptionsMap(t *testing.T) {
	got := cssBeautifierOptionsMap(CSSBeautifierOptions{
		IndentSize:                   2,
		IndentChar:                   "\t",
		EndWithNewline:               true,
		SelectorSeparatorNewline:     false,
		NewlineBetweenRules:          true,
		SpaceAroundSelectorSeparator: true,
		BraceStyle:                   "expand",
		PreserveNewlines:             true,
		MaxPreserveNewlines:          3,
		WrapLineLength:               80,
		IndentEmptyLines:             true,
		EOL:                          "\r\n",
		BaseIndentLevel:              2,
	})
	want := beautify.Options{
		"indent_size":                     2,
		"indent_char":                     "\t",
		"indent_with_tabs":                true,
		"end_with_newline":                true,
		"selector_separator_newline":      false,
		"newline_between_rules":           true,
		"space_around_selector_separator": true,
		"space_around_combinator":         true,
		"brace_style":                     "expand",
		"preserve_newlines":               true,
		"max_preserve_newlines":           3,
		"wrap_line_length":                80,
		"indent_empty_lines":              true,
		"eol":                             "\r\n",
		"indent_level":                    2,
	}
	if !reflect.DeepEqual(got, want) {
		t.Fatalf("cssBeautifierOptionsMap = %#v, want %#v", got, want)
	}

	got = cssBeautifierOptionsMap(CSSBeautifierOptions{IndentSize: 2, IndentChar: " ", BraceStyle: "collapse", EOL: "\n"})
	if _, ok := got["max_preserve_newlines"]; ok {
		t.Fatalf("max_preserve_newlines should be omitted when zero: %#v", got)
	}
	if _, ok := got["wrap_line_length"]; ok {
		t.Fatalf("wrap_line_length should be omitted when zero: %#v", got)
	}
	if _, ok := got["indent_level"]; ok {
		t.Fatalf("indent_level should be omitted when zero: %#v", got)
	}
}

func TestFormatRoutesThroughCSSBeautifierBackend(t *testing.T) {
	previous := defaultCSSBeautifier
	recorder := &recordingCSSBeautifier{output: ".foo {\n  color: red;\n}\n"}
	defaultCSSBeautifier = recorder
	defer func() {
		defaultCSSBeautifier = previous
	}()

	document := lsp.NewTextDocument("test://test.css", "css", 0, ".foo{color:red}")
	edits := Format(document, nil, FormatOptions{InsertSpaces: true, TabSize: 2, InsertFinalNewline: true})
	if len(edits) != 1 {
		t.Fatalf("edits = %#v", edits)
	}
	if edits[0].NewText != recorder.output {
		t.Fatalf("new text = %q, want %q", edits[0].NewText, recorder.output)
	}
	if recorder.input != document.Text() {
		t.Fatalf("backend input = %q, want %q", recorder.input, document.Text())
	}
	if recorder.options.IndentSize != 2 || recorder.options.IndentChar != " " || !recorder.options.EndWithNewline {
		t.Fatalf("backend options = %#v", recorder.options)
	}
}

func baseJSBeautifyCSSOptions(overrides beautify.Options) beautify.Options {
	options := beautify.Options{
		"indent_size":                     2,
		"indent_char":                     " ",
		"end_with_newline":                false,
		"selector_separator_newline":      true,
		"newline_between_rules":           true,
		"space_around_selector_separator": false,
		"space_around_combinator":         false,
		"brace_style":                     "collapse",
		"preserve_newlines":               false,
		"indent_empty_lines":              false,
		"eol":                             "\n",
	}
	for key, value := range overrides {
		options[key] = value
	}
	return options
}

func mustBeautifyCSS(t *testing.T, input string, options beautify.Options) string {
	t.Helper()
	result, err := beautify.CSS(input, options)
	if err != nil {
		t.Fatal(err)
	}
	return result
}

type recordingCSSBeautifier struct {
	input   string
	options CSSBeautifierOptions
	output  string
}

func (b *recordingCSSBeautifier) BeautifyCSS(input string, options CSSBeautifierOptions) string {
	b.input = input
	b.options = options
	return b.output
}

type formatTest struct {
	name        string
	unformatted string
	expected    string
	options     *FormatOptions
}

func assertFormat(t *testing.T, unformatted, expected string, options *FormatOptions) {
	t.Helper()
	var editRange *lsp.Range
	rangeStart := runeIndex(unformatted, '|')
	rangeEnd := lastRuneIndex(unformatted, '|')
	if rangeStart != -1 && rangeEnd != -1 && rangeStart != rangeEnd {
		runes := []rune(unformatted)
		unformatted = string(runes[:rangeStart]) + string(runes[rangeStart+1:rangeEnd]) + string(runes[rangeEnd+1:])
		doc := lsp.NewTextDocument("test://test.html", "html", 0, unformatted)
		r := lsp.Range{Start: doc.PositionAt(rangeStart), End: doc.PositionAt(rangeEnd - 1)}
		editRange = &r
	}
	opts := FormatOptions{TabSize: 2, InsertSpaces: true}
	if options != nil {
		opts = *options
	}
	doc := lsp.NewTextDocument("test://test.html", "html", 0, unformatted)
	edits := Format(doc, editRange, opts)
	formatted := lsp.ApplyEdits(doc, edits)
	if formatted != expected {
		t.Fatalf("\nactual:\n%s\nwant:\n%s", formatted, expected)
	}
}

func runeIndex(value string, target rune) int {
	for i, ch := range []rune(value) {
		if ch == target {
			return i
		}
	}
	return -1
}

func lastRuneIndex(value string, target rune) int {
	runes := []rune(value)
	for i := len(runes) - 1; i >= 0; i-- {
		if runes[i] == target {
			return i
		}
	}
	return -1
}
