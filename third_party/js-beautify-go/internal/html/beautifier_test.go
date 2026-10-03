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

func TestBeautifyExtraLinersAddExactlyOneBlankLine(t *testing.T) {
	options := map[string]any{"indent_size": 2, "indent_char": " "}
	tests := []struct {
		input string
		want  string
	}{
		{"<html><head></head><body></body></html>", "<html>\n\n<head></head>\n\n<body></body>\n\n</html>"},
		{"<html>\n\n<head></head>\n\n<body></body>\n\n</html>", "<html>\n\n<head></head>\n\n<body></body>\n\n</html>"},
		{"<html><head><title>x</title></head><body><p>a</p></body></html>", "<html>\n\n<head>\n  <title>x</title>\n</head>\n\n<body>\n  <p>a</p>\n</body>\n\n</html>"},
		{"0<body>", "0\n\n<body>"},
		{"<p>a</p>\n\n<body>\n</body>", "<p>a</p>\n\n<body>\n</body>"},
	}
	for _, test := range tests {
		got, err := Beautify(test.input, options, nil, nil)
		if err != nil {
			t.Fatal(err)
		}
		if got != test.want {
			t.Fatalf("Beautify(%q) = %q, want %q", test.input, got, test.want)
		}
		again, err := Beautify(got, options, nil, nil)
		if err != nil {
			t.Fatal(err)
		}
		if again != got {
			t.Fatalf("Beautify is not idempotent for %q: %q then %q", test.input, got, again)
		}
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

// Expected outputs come from js-beautify 1.15.4 with indent_size 2.
func TestBeautifyMatchesUpstreamForBlankLinesAndMalformedHTML(t *testing.T) {
	tests := []struct {
		input   string
		options map[string]any
		want    string
	}{
		{
			input: "<html>\n\n\n\n<head></head>\n\n\n<body></body>\n\n\n</html>",
			want:  "<html>\n\n\n\n<head></head>\n\n\n<body></body>\n\n\n</html>",
		},
		{
			input:   "<html>\n\n\n\n<head></head>\n\n\n<body></body>\n\n\n</html>",
			options: map[string]any{"max_preserve_newlines": 1},
			want:    "<html>\n\n<head></head>\n\n<body></body>\n\n</html>",
		},
		{input: "<A><", want: "<A>\n  <"},
		{input: "<p>a < b > c</p>", want: "<p>a < b> c</p>"},
		{input: "<div><p>x</div>", want: "<div>\n  <p>x\n</div>"},
		{input: "<ul><li>a<li>b</ul>", want: "<ul>\n  <li>a\n  <li>b\n</ul>"},
		{input: "<select><option>a<option>b</select>", want: "<select>\n  <option>a\n  <option>b\n</select>"},
		{input: "<p>a</p></div><p>b", want: "<p>a</p>\n</div>\n<p>b"},
		{input: "<div <span>x</span>", want: "<div <span>x</span>"},
		{
			input:   "<div></ ></div>",
			options: map[string]any{"wrap_attributes": "force"},
			want:    "<div>\n  </>\n</div>",
		},
		{
			input: "<table><tr><td>1<td>2<tr><td>3</table>",
			want:  "<table>\n  <tr>\n    <td>1\n    <td>2\n  <tr>\n    <td>3\n</table>",
		},
	}
	for _, test := range tests {
		options := map[string]any{"indent_size": 2, "indent_char": " "}
		for key, value := range test.options {
			options[key] = value
		}
		got, err := Beautify(test.input, options, nil, nil)
		if err != nil {
			t.Fatal(err)
		}
		if got != test.want {
			t.Fatalf("Beautify(%q) = %q, want %q", test.input, got, test.want)
		}
	}
}
