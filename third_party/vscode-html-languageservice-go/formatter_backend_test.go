package htmlservice

import (
	"encoding/json"
	"errors"
	"reflect"
	"strings"
	"testing"
)

func TestFormatterUsesConfiguredHTMLBeautifier(t *testing.T) {
	indentEmptyLines := true
	wrapLineLength := 40
	unformatted := "script, code"
	contentUnformatted := "pre, textarea"
	indentInnerHTML := true
	preserveNewLines := false
	maxPreserveNewLines := 3
	indentHandlebars := true
	endWithNewline := false
	extraLiners := "head, /html"
	wrapAttributesIndentSize := 6
	selectorNewline := false
	newlineBetweenRules := true
	spaceAroundSelector := true
	cssPreserveNewLines := false
	cssMaxPreserveNewLines := 2

	backend := &recordingHTMLBeautifier{result: "BEAUTIFIED"}
	ls := GetLanguageService(LanguageServiceOptions{HTMLBeautifier: backend})
	doc := NewTextDocument("test://test.html", "html", 0, " \n\t<div></div>")
	options := HTMLFormatConfiguration{
		TabSize:                     2,
		InsertSpaces:                false,
		IndentEmptyLines:            &indentEmptyLines,
		WrapLineLength:              &wrapLineLength,
		Unformatted:                 &unformatted,
		ContentUnformatted:          &contentUnformatted,
		IndentInnerHTML:             &indentInnerHTML,
		WrapAttributes:              stringPtrForTest("force-aligned"),
		WrapAttributesIndentSize:    &wrapAttributesIndentSize,
		PreserveNewLines:            &preserveNewLines,
		MaxPreserveNewLines:         &maxPreserveNewLines,
		IndentHandlebars:            &indentHandlebars,
		EndWithNewline:              &endWithNewline,
		ExtraLiners:                 &extraLiners,
		IndentScripts:               stringPtrForTest("keep"),
		Templating:                  true,
		UnformattedContentDelimiter: "<!-- html-format-ignore -->",
		CSS: &EmbeddedCSSFormatConfiguration{
			NewlineBetweenSelectors:      &selectorNewline,
			NewlineBetweenRules:          &newlineBetweenRules,
			SpaceAroundSelectorSeparator: &spaceAroundSelector,
			BraceStyle:                   stringPtrForTest("expand"),
			PreserveNewLines:             &cssPreserveNewLines,
			MaxPreserveNewLines:          &cssMaxPreserveNewLines,
		},
	}

	got := ApplyEdits(doc, ls.Format(doc, nil, options))
	if got != "BEAUTIFIED" {
		t.Fatalf("formatted output got %q", got)
	}
	if backend.source != "<div></div>" {
		t.Fatalf("beautifier source got %q", backend.source)
	}

	want := BeautifyHTMLOptions{
		IndentSize:                  2,
		IndentChar:                  "\t",
		IndentEmptyLines:            true,
		WrapLineLength:              40,
		Unformatted:                 []string{"script", "code"},
		ContentUnformatted:          []string{"pre", "textarea"},
		IndentInnerHTML:             true,
		PreserveNewLines:            false,
		MaxPreserveNewLines:         3,
		IndentHandlebars:            true,
		EndWithNewline:              false,
		ExtraLiners:                 []string{"head", "/html"},
		WrapAttributes:              "force-aligned",
		WrapAttributesIndentSize:    &wrapAttributesIndentSize,
		EOL:                         "\n",
		IndentScripts:               "keep",
		Templating:                  []string{"auto"},
		UnformattedContentDelimiter: "<!-- html-format-ignore -->",
		CSS: &BeautifyCSSOptions{
			SelectorSeparatorNewline:     &selectorNewline,
			NewlineBetweenRules:          &newlineBetweenRules,
			SpaceAroundSelectorSeparator: &spaceAroundSelector,
			BraceStyle:                   stringPtrForTest("expand"),
			PreserveNewLines:             &cssPreserveNewLines,
			MaxPreserveNewLines:          &cssMaxPreserveNewLines,
		},
	}
	if !reflect.DeepEqual(backend.options, want) {
		t.Fatalf("beautifier options:\n got: %#v\nwant: %#v", backend.options, want)
	}
}

func TestFormatterBackendErrorPanics(t *testing.T) {
	backend := &recordingHTMLBeautifier{err: errors.New("format failed")}
	ls := GetLanguageService(LanguageServiceOptions{HTMLBeautifier: backend})
	doc := NewTextDocument("test://test.html", "html", 0, " \n\t<div></div>")
	defer func() {
		if recover() == nil {
			t.Fatalf("format error should panic")
		}
	}()
	_ = ls.Format(doc, nil, HTMLFormatConfiguration{TabSize: 2, InsertSpaces: true})
}

func TestFormatterInvalidOptionPanicsLikeForkSource(t *testing.T) {
	ls := GetLanguageService()
	doc := NewTextDocument("test://test.html", "html", 0, `<div a="1" b="2"></div>`)
	defer func() {
		if recover() == nil {
			t.Fatalf("invalid formatter option should panic")
		}
	}()
	_ = ls.Format(doc, nil, HTMLFormatConfiguration{TabSize: 2, InsertSpaces: true, WrapAttributes: stringPtrForTest("bogus")})
}

func TestFormatterEmptyEnumOptionsPanicLikeForkSource(t *testing.T) {
	cases := []struct {
		name    string
		source  string
		options HTMLFormatConfiguration
		message string
	}{
		{
			name:    "wrapAttributes",
			source:  `<div a="1" b="2"></div>`,
			options: HTMLFormatConfiguration{TabSize: 2, InsertSpaces: true, WrapAttributes: stringPtrForTest("")},
			message: "Invalid Option Value: The option 'wrap_attributes' can contain only the following values:\nauto,force,force-aligned,force-expand-multiline,aligned-multiple,preserve,preserve-aligned\nYou passed in: ''",
		},
		{
			name:    "indentScripts",
			source:  `<script>function f(){return 1+2;}</script>`,
			options: HTMLFormatConfiguration{TabSize: 2, InsertSpaces: true, IndentScripts: stringPtrForTest("")},
			message: "Invalid Option Value: The option 'indent_scripts' can contain only the following values:\nnormal,keep,separate\nYou passed in: ''",
		},
		{
			name:    "cssBraceStyle",
			source:  `<style>h1 { color:red; }</style>`,
			options: HTMLFormatConfiguration{TabSize: 2, InsertSpaces: true, CSS: &EmbeddedCSSFormatConfiguration{BraceStyle: stringPtrForTest("")}},
			message: "Invalid Option Value: The option 'brace_style' can contain only the following values:\ncollapse,expand,end-expand,none,preserve-inline\nYou passed in: ''",
		},
		{
			name:    "templating",
			source:  `<div></div>`,
			options: HTMLFormatConfiguration{TabSize: 2, InsertSpaces: true, Templating: []string{}},
			message: "Invalid Option Value: The option 'templating' can contain only the following values:\nauto,none,angular,django,erb,handlebars,php,smarty\nYou passed in: ''",
		},
	}
	for _, tc := range cases {
		t.Run(tc.name, func(t *testing.T) {
			doc := NewTextDocument("test://test.html", "html", 0, tc.source)
			defer func() {
				recovered := recover()
				if recovered == nil {
					t.Fatalf("empty enum option should panic")
				}
				if got := recovered.(error).Error(); got != tc.message {
					t.Fatalf("panic message got:\n%q\nwant:\n%q", got, tc.message)
				}
			}()
			_ = GetLanguageService().Format(doc, nil, tc.options)
		})
	}
}

func TestFormatterEmbeddedScriptNoopMatchesForkSource(t *testing.T) {
	got := formatTextWithOptions("<script>\n    if(a){b();}\n</script>", HTMLFormatConfiguration{TabSize: 2, InsertSpaces: true})
	want := "<script>\n  if(a){b();}\n</script>"
	if got != want {
		t.Fatalf("script no-op formatting got:\n%q\nwant:\n%q", got, want)
	}

	indentScripts := "separate"
	got = formatTextWithOptions("<script>\n    if(a){b();}\n</script>", HTMLFormatConfiguration{TabSize: 2, InsertSpaces: true, IndentScripts: &indentScripts})
	want = "<script>\nif(a){b();}\n</script>"
	if got != want {
		t.Fatalf("separate script no-op formatting got:\n%q\nwant:\n%q", got, want)
	}
}

func TestFormatterMultilineEmbeddedScriptNoopMatchesForkSource(t *testing.T) {
	cases := []struct {
		name  string
		input string
		want  string
	}{
		{
			name:  "plain",
			input: "<script>if(a){b();}\nx();</script>",
			want:  "<script>\n  if(a){b();}\nx();\n</script>",
		},
		{
			name:  "comment wrapped",
			input: "<script><!--\nif(a){b();}\nx();\n//--></script>",
			want:  "<script>\n  <!--\n  if(a){b();}\nx();\n//\n  -->\n</script>",
		},
		{
			name:  "cdata wrapped",
			input: "<script><![CDATA[\nif(a){b();}\nx();\n]]></script>",
			want:  "<script>\n  <![CDATA[\n  if(a){b();}\nx();\n  ]]>\n</script>",
		},
	}
	for _, tc := range cases {
		got := formatTextWithOptions(tc.input, HTMLFormatConfiguration{TabSize: 2, InsertSpaces: true})
		if got != tc.want {
			t.Fatalf("%s script formatting got:\n%q\nwant:\n%q", tc.name, got, tc.want)
		}
	}
}

func TestFormatterEmbeddedCSSDefaultsDoNotInheritHTMLPreserveNewLines(t *testing.T) {
	preserveNewLines := false
	blankRun := strings.Repeat("\n", 15)
	got := formatTextWithOptions("<style>a{"+blankRun+"color:red;}</style>", HTMLFormatConfiguration{
		TabSize:          2,
		InsertSpaces:     true,
		PreserveNewLines: &preserveNewLines,
		CSS:              &EmbeddedCSSFormatConfiguration{},
	})
	want := "<style>\n  a {" + blankRun + "    color: red;\n  }\n</style>"
	if got != want {
		t.Fatalf("css default preserve_newlines got:\n%q\nwant:\n%q", got, want)
	}
}

func TestFormatterEmbeddedRawContentMatchesForkSource(t *testing.T) {
	got := formatTextWithOptions(`<script type="importmap">{"imports":{"x":"/x.js"}}</script>`, HTMLFormatConfiguration{TabSize: 2, InsertSpaces: true})
	want := `<script type="importmap">{"imports":{"x":"/x.js"}}</script>`
	if got != want {
		t.Fatalf("importmap script formatting got:\n%q\nwant:\n%q", got, want)
	}

	got = formatTextWithOptions("<script type=\"text/plain\">\n  a\n</script>", HTMLFormatConfiguration{TabSize: 2, InsertSpaces: true})
	want = "<script type=\"text/plain\">\n  a\n</script>"
	if got != want {
		t.Fatalf("unknown script formatting got:\n%q\nwant:\n%q", got, want)
	}

	got = formatTextWithOptions("<style type=\"text/less\">\n  @x:red;\n</style>", HTMLFormatConfiguration{TabSize: 2, InsertSpaces: true})
	want = "<style type=\"text/less\">\n  @x:red;\n</style>"
	if got != want {
		t.Fatalf("unknown style formatting got:\n%q\nwant:\n%q", got, want)
	}
}

func TestFormatterRawIgnoreSpansMatchForkSource(t *testing.T) {
	delimiter := "<!-- html-format-ignore -->"
	input := `<div>a<!-- html-format-ignore -->   b   <!-- html-format-ignore -->c</div>`
	got := formatTextWithOptions(input, HTMLFormatConfiguration{
		TabSize:                     2,
		InsertSpaces:                true,
		UnformattedContentDelimiter: delimiter,
	})
	if got != input {
		t.Fatalf("unformatted content delimiter got:\n%q\nwant:\n%q", got, input)
	}

	input = `<div><!-- beautify ignore:start --><span> x </span><!-- beautify ignore:end --></div>`
	got = formatTextWithOptions(input, HTMLFormatConfiguration{TabSize: 2, InsertSpaces: true})
	if got != input {
		t.Fatalf("beautify ignore directive got:\n%q\nwant:\n%q", got, input)
	}
}

func TestFormatterContentUnformattedInlineWhitespaceMatchesForkSource(t *testing.T) {
	cases := []struct {
		name  string
		input string
		tags  string
	}{
		{
			name:  "code",
			input: `<div><code> a   b </code></div>`,
			tags:  "code",
		},
		{
			name:  "span",
			input: `<div><span> a   b </span></div>`,
			tags:  "span",
		},
	}
	for _, tc := range cases {
		got := formatTextWithOptions(tc.input, HTMLFormatConfiguration{
			TabSize:            2,
			InsertSpaces:       true,
			ContentUnformatted: stringPtrForTest(tc.tags),
		})
		if got != tc.input {
			t.Fatalf("%s contentUnformatted got:\n%q\nwant:\n%q", tc.name, got, tc.input)
		}
	}
}

func TestFormatterIndentHandlebarsInlineParentMatchesForkSource(t *testing.T) {
	indentHandlebars := true
	got := formatTextWithOptions("<div>{{#if a}}\n<span>b</span>\n{{/if}}</div>", HTMLFormatConfiguration{
		TabSize:          2,
		InsertSpaces:     true,
		IndentHandlebars: &indentHandlebars,
	})
	want := "<div>{{#if a}}\n    <span>b</span>\n  {{/if}}</div>"
	if got != want {
		t.Fatalf("indentHandlebars inline parent got:\n%q\nwant:\n%q", got, want)
	}
}

func TestDefaultFormatterUsesJSBeautifyBackend(t *testing.T) {
	got := formatTextWithOptions(`<script>if(a){b();}</script>`, HTMLFormatConfiguration{TabSize: 2, InsertSpaces: true})
	want := "<script>\n  if(a){b();}\n</script>"
	if got != want {
		t.Fatalf("script body should preserve fork-compatible no-op JS formatting:\n%q\nwant:\n%q", got, want)
	}

	got = formatTextWithOptions(`<style>div>span { color:red; }</style>`, HTMLFormatConfiguration{TabSize: 2, InsertSpaces: true})
	want = "<style>\n  div>span {\n    color: red;\n  }\n</style>"
	if got != want {
		t.Fatalf("style body should be formatted by js-beautify-go CSS backend:\n%q\nwant:\n%q", got, want)
	}
}

func TestNewBeautifyHTMLOptionsDefaultsMatchForkFormatter(t *testing.T) {
	got := NewBeautifyHTMLOptions(HTMLFormatConfiguration{TabSize: 2, InsertSpaces: true}, 2, true)
	want := BeautifyHTMLOptions{
		IndentSize:          2,
		IndentChar:          " ",
		WrapLineLength:      120,
		PreserveNewLines:    true,
		MaxPreserveNewLines: 32786,
		WrapAttributes:      "auto",
		EOL:                 "\n",
		IndentScripts:       "normal",
		Templating:          []string{"none"},
	}
	if !reflect.DeepEqual(got, want) {
		t.Fatalf("default beautify options:\n got: %#v\nwant: %#v", got, want)
	}
}

func TestNewBeautifyHTMLOptionsPreservesForkFormatterOptionEdges(t *testing.T) {
	unformatted := "SCRIPT, "
	extraLiners := " , /html"
	got := NewBeautifyHTMLOptions(HTMLFormatConfiguration{
		TabSize:      2,
		InsertSpaces: true,
		Unformatted:  &unformatted,
		ExtraLiners:  &extraLiners,
		Templating:   []string{},
	}, 2, true)

	if !reflect.DeepEqual(got.Unformatted, []string{"script", ""}) {
		t.Fatalf("unformatted got %#v want %#v", got.Unformatted, []string{"script", ""})
	}
	if !reflect.DeepEqual(got.ExtraLiners, []string{"", "/html"}) {
		t.Fatalf("extra liners got %#v want %#v", got.ExtraLiners, []string{"", "/html"})
	}
	if !reflect.DeepEqual(got.Templating, []string{}) {
		t.Fatalf("templating got %#v want empty array", got.Templating)
	}
}

func TestNewBeautifyHTMLOptionsAcceptsJSONDecodedTemplatingArray(t *testing.T) {
	var templating any
	if err := json.Unmarshal([]byte(`["angular"]`), &templating); err != nil {
		t.Fatal(err)
	}
	got := NewBeautifyHTMLOptions(HTMLFormatConfiguration{
		TabSize:      2,
		InsertSpaces: true,
		Templating:   templating,
	}, 2, true)

	if !reflect.DeepEqual(got.Templating, []string{"angular"}) {
		t.Fatalf("templating got %#v want %#v", got.Templating, []string{"angular"})
	}
}

func TestFormatTrimsJavaScriptWhitespaceBeforeBeautifier(t *testing.T) {
	beautifier := &recordingHTMLBeautifier{result: "<div></div>"}
	doc := NewTextDocument("test://test/test.html", "html", 0, "\f\v<div></div>")
	edits := FormatWithBeautifier(doc, nil, HTMLFormatConfiguration{TabSize: 2, InsertSpaces: true}, beautifier)
	if len(edits) != 1 {
		t.Fatalf("edit count got %d want 1", len(edits))
	}
	if beautifier.source != "<div></div>" {
		t.Fatalf("beautifier source got %q want %q", beautifier.source, "<div></div>")
	}
}

func TestJSBeautifyOptionsUseNoopScriptDelegateAndIncludeCSSSettings(t *testing.T) {
	selectorNewline := false
	newlineBetweenRules := true
	spaceAroundSelector := true
	cssPreserveNewLines := false
	cssMaxPreserveNewLines := 2
	wrapAttributesIndentSize := 6
	options := BeautifyHTMLOptions{
		IndentSize:               2,
		IndentChar:               " ",
		WrapLineLength:           120,
		PreserveNewLines:         true,
		MaxPreserveNewLines:      32786,
		WrapAttributes:           "force",
		WrapAttributesIndentSize: &wrapAttributesIndentSize,
		EOL:                      "\n",
		IndentScripts:            "normal",
		Templating:               []string{"none"},
		CSS: &BeautifyCSSOptions{
			SelectorSeparatorNewline:     &selectorNewline,
			NewlineBetweenRules:          &newlineBetweenRules,
			SpaceAroundSelectorSeparator: &spaceAroundSelector,
			PreserveNewLines:             &cssPreserveNewLines,
			MaxPreserveNewLines:          &cssMaxPreserveNewLines,
		},
	}
	got := jsBeautifyOptions(options)
	if _, ok := got["js"]; ok {
		t.Fatalf("did not expect js child options because the adapter supplies a no-op JS delegate, got %#v", got["js"])
	}
	css, ok := got["css"].(map[string]any)
	if !ok {
		t.Fatalf("expected css child options, got %#v", got["css"])
	}
	for key, want := range map[string]any{
		"selector_separator_newline":      false,
		"newline_between_rules":           true,
		"space_around_selector_separator": true,
		"preserve_newlines":               false,
		"max_preserve_newlines":           2,
		"brace_style":                     "collapse",
	} {
		if css[key] != want {
			t.Fatalf("css option %s got %#v want %#v", key, css[key], want)
		}
	}
	if got["wrap_attributes_indent_size"] != 6 {
		t.Fatalf("wrap_attributes_indent_size got %#v", got["wrap_attributes_indent_size"])
	}
}

type recordingHTMLBeautifier struct {
	source  string
	options BeautifyHTMLOptions
	result  string
	err     error
}

func (b *recordingHTMLBeautifier) BeautifyHTML(source string, options BeautifyHTMLOptions) (string, error) {
	b.source = source
	b.options = options
	if b.err != nil {
		return "", b.err
	}
	return b.result, nil
}
