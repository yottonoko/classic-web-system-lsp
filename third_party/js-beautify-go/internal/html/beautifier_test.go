package html

import (
	"testing"

	"github.com/yottonoko/js-beautify-go/internal/css"
	"github.com/yottonoko/js-beautify-go/internal/javascript"
)

func TestBeautifyBasicHTML(t *testing.T) {
	got, err := Beautify("<div><div></div></div>", nil, nil, nil)
	if err != nil {
		t.Fatal(err)
	}
	want := "<div>\n    <div></div>\n</div>"
	if got != want {
		t.Fatalf("Beautify() = %q, want %q", got, want)
	}
}

func TestBeautifyNormalizesIncompleteStartTagAttributes(t *testing.T) {
	got, err := Beautify(`<img  src = "foo"`, map[string]any{
		"indent_size": 2,
		"indent_char": " ",
	}, nil, nil)
	if err != nil {
		t.Fatal(err)
	}
	want := `<img src="foo"`
	if got != want {
		t.Fatalf("Beautify() = %q, want %q", got, want)
	}
}

func TestBeautifyKeepsInlineTagAfterCloseTagFragment(t *testing.T) {
	got, err := Beautify(`</div>   <img  src = "foo"`, map[string]any{
		"indent_size": 2,
		"indent_char": " ",
	}, nil, nil)
	if err != nil {
		t.Fatal(err)
	}
	want := `</div> <img src="foo"`
	if got != want {
		t.Fatalf("Beautify() = %q, want %q", got, want)
	}
}

func TestBeautifyKeepsSingleSpaceDisabledRawScriptBody(t *testing.T) {
	got, err := Beautify(`<script src="/js/main.js"> </script>`, map[string]any{
		"indent_size": 2,
		"indent_char": " ",
		"js": map[string]any{
			"disabled": true,
		},
	}, nil, nil)
	if err != nil {
		t.Fatal(err)
	}
	want := `<script src="/js/main.js"> </script>`
	if got != want {
		t.Fatalf("Beautify() = %q, want %q", got, want)
	}
}

func TestBeautifyKeepsSingleSpaceJavaScriptRawBody(t *testing.T) {
	got, err := Beautify(`<script src="/js/main.js"> </script>`, map[string]any{
		"indent_size": 2,
		"indent_char": " ",
	}, javascript.Beautify, nil)
	if err != nil {
		t.Fatal(err)
	}
	want := `<script src="/js/main.js"> </script>`
	if got != want {
		t.Fatalf("Beautify() = %q, want %q", got, want)
	}
}

func TestBeautifyNoopJavaScriptRawBodyIndentsLikeVSCodeFork(t *testing.T) {
	noop := func(source string, _ map[string]any) (string, error) {
		return source, nil
	}
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
		got, err := Beautify(tc.input, map[string]any{
			"indent_size": 2,
			"indent_char": " ",
		}, noop, nil)
		if err != nil {
			t.Fatal(err)
		}
		if got != tc.want {
			t.Fatalf("%s Beautify() = %q, want %q", tc.name, got, tc.want)
		}
	}
}

func TestBeautifyDoesNotTreatNotJSONSubtypeAsJavaScript(t *testing.T) {
	got, err := Beautify(`<script type="notjson">function f(){return 1+2;}</script>`, map[string]any{
		"indent_size": 2,
		"indent_char": " ",
	}, javascript.Beautify, nil)
	if err != nil {
		t.Fatal(err)
	}
	want := `<script type="notjson">function f(){return 1+2;}</script>`
	if got != want {
		t.Fatalf("Beautify() = %q, want %q", got, want)
	}
}

func TestBeautifyDoesNotTreatImportmapAsJavaScript(t *testing.T) {
	got, err := Beautify(`<script type="importmap">{"imports":{"x":"/x.js"}}</script>`, map[string]any{
		"indent_size": 2,
		"indent_char": " ",
	}, javascript.Beautify, nil)
	if err != nil {
		t.Fatal(err)
	}
	want := `<script type="importmap">{"imports":{"x":"/x.js"}}</script>`
	if got != want {
		t.Fatalf("Beautify() = %q, want %q", got, want)
	}
}

func TestBeautifyUnknownRawContentKeepsIndentedBody(t *testing.T) {
	cases := []struct {
		name  string
		input string
		want  string
	}{
		{
			name:  "plain script",
			input: "<script type=\"text/plain\">\n  a\n</script>",
			want:  "<script type=\"text/plain\">\n  a\n</script>",
		},
		{
			name:  "less style",
			input: "<style type=\"text/less\">\n  @x:red;\n</style>",
			want:  "<style type=\"text/less\">\n  @x:red;\n</style>",
		},
	}
	for _, tc := range cases {
		got, err := Beautify(tc.input, map[string]any{
			"indent_size": 2,
			"indent_char": " ",
		}, javascript.Beautify, css.Beautify)
		if err != nil {
			t.Fatal(err)
		}
		if got != tc.want {
			t.Fatalf("%s Beautify() = %q, want %q", tc.name, got, tc.want)
		}
	}
}

func TestBeautifyIndentHandlebarsKeepsInlineParentLayout(t *testing.T) {
	got, err := Beautify("<div>{{#if a}}\n<span>b</span>\n{{/if}}</div>", map[string]any{
		"indent_size":           2,
		"indent_char":           " ",
		"indent_handlebars":     true,
		"wrap_line_length":      0,
		"preserve_newlines":     true,
		"max_preserve_newlines": 32786,
	}, nil, nil)
	if err != nil {
		t.Fatal(err)
	}
	want := "<div>{{#if a}}\n    <span>b</span>\n  {{/if}}</div>"
	if got != want {
		t.Fatalf("Beautify() = %q, want %q", got, want)
	}
}

func TestBeautifyPreservesEmbeddedCSSBlankLines(t *testing.T) {
	got, err := Beautify(`<style>h1 { color: red; } h2 { color: blue; }</style>`, map[string]any{
		"indent_size": 2,
		"indent_char": " ",
		"css": map[string]any{
			"newline_between_rules": true,
		},
	}, nil, css.Beautify)
	if err != nil {
		t.Fatal(err)
	}
	want := "<style>\n  h1 {\n    color: red;\n  }\n\n  h2 {\n    color: blue;\n  }\n</style>"
	if got != want {
		t.Fatalf("Beautify() = %q, want %q", got, want)
	}
}

func TestBeautifyPreservesUnformattedContentDelimiterSpan(t *testing.T) {
	input := `<div>a<!-- html-format-ignore -->   b   <!-- html-format-ignore -->c</div>`
	got, err := Beautify(input, map[string]any{
		"indent_size":                   2,
		"indent_char":                   " ",
		"unformatted_content_delimiter": "<!-- html-format-ignore -->",
	}, nil, nil)
	if err != nil {
		t.Fatal(err)
	}
	if got != input {
		t.Fatalf("Beautify() = %q, want %q", got, input)
	}
}

func TestBeautifyPreservesHTMLIgnoreDirectiveSpan(t *testing.T) {
	input := `<div><!-- beautify ignore:start --><span> x </span><!-- beautify ignore:end --></div>`
	got, err := Beautify(input, map[string]any{
		"indent_size": 2,
		"indent_char": " ",
	}, nil, nil)
	if err != nil {
		t.Fatal(err)
	}
	if got != input {
		t.Fatalf("Beautify() = %q, want %q", got, input)
	}
}

func TestBeautifyPreservesInlineContentUnformattedWhitespace(t *testing.T) {
	cases := []struct {
		name  string
		input string
		tag   string
	}{
		{
			name:  "code",
			input: `<div><code> a   b </code></div>`,
			tag:   "code",
		},
		{
			name:  "span",
			input: `<div><span> a   b </span></div>`,
			tag:   "span",
		},
	}
	for _, tc := range cases {
		got, err := Beautify(tc.input, map[string]any{
			"indent_size":         2,
			"indent_char":         " ",
			"content_unformatted": []string{tc.tag},
		}, nil, nil)
		if err != nil {
			t.Fatal(err)
		}
		if got != tc.input {
			t.Fatalf("%s Beautify() = %q, want %q", tc.name, got, tc.input)
		}
	}
}

func TestBeautifyTerminatesOnUnterminatedTemplateMarkers(t *testing.T) {
	for _, source := range []string{"{#f0x0xff", "a {% b", "<p>{# x</p>", "{#{#{%"} {
		got, err := Beautify(source, nil, nil, nil)
		if err != nil {
			t.Fatalf("Beautify(%q) error = %v", source, err)
		}
		if got == "" {
			t.Fatalf("Beautify(%q) returned no output", source)
		}
	}
}

func TestSplitTagRejectsEmptyClosingTag(t *testing.T) {
	if _, _, _, _, ok := splitTag("</ >"); ok {
		t.Fatal("splitTag accepted a closing tag without a name")
	}
	got, err := Beautify("<div></ ></div>", map[string]any{"wrap_attributes": "force"}, nil, nil)
	if err != nil {
		t.Fatal(err)
	}
	if want := "<div>\n</ >\n</div>"; got != want {
		t.Fatalf("Beautify() = %q, want %q", got, want)
	}
}

func TestIndexedSimpleCloseSpanMatchesScan(t *testing.T) {
	sources := []string{
		"<table><tr><td>a<td>b<tr><td>c</td><td>d</table>",
		"<ul><li>a<li>b<ul><li>c</li></ul><li>d</ul>",
		"<p>a<p>b</p><P>c</P ><p>d",
		`<td title="</td>">a<td>b</td></td>`,
		"<select><option>a<option selected>b</option></select>",
		"<span>a<span>b</span>c</span><span/>d</span>",
	}
	for _, source := range sources {
		b := NewBeautifier(source, nil, nil, nil)
		for start := 0; start <= len(source); start++ {
			for _, closeTag := range []string{"</td>", "</li>", "</p>", "</option>", "</span>", "</ul>"} {
				gotStart, gotEnd, gotOK := b.findSimpleCloseSpan(source, start, closeTag)
				wantStart, wantEnd, wantOK := findSimpleCloseSpan(source[start:], closeTag)
				if gotOK != wantOK || (gotOK && (gotStart != wantStart || gotEnd != wantEnd)) {
					t.Fatalf("findSimpleCloseSpan(%q, %d, %q) = %d, %d, %v; scan = %d, %d, %v", source, start, closeTag, gotStart, gotEnd, gotOK, wantStart, wantEnd, wantOK)
				}
			}
		}
	}
}
