package htmlservice

import "testing"

func formatParity(input string, insertSpaces bool, css *EmbeddedCSSFormatConfiguration) string {
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
	options := HTMLFormatConfiguration{TabSize: 2, InsertSpaces: insertSpaces, CSS: css}
	edits := GetLanguageService().Format(doc, r, options)
	return ApplyEdits(doc, edits)
}

func TestFormatterBaselineRangeParity(t *testing.T) {
	cases := []struct {
		name  string
		input string
		want  string
	}{
		{"range2", stringsJoin([]string{`<div  class = "foo">`, `  |<img  src = "foo">|`, `  `, ` </div>`}, "\n"), stringsJoin([]string{`<div  class = "foo">`, `  <img src="foo">`, `  `, ` </div>`}, "\n")},
		{"range3", stringsJoin([]string{`<div  class = "foo">`, `  |<img  src = "foo">|    `, `  `, ` </div>`}, "\n"), stringsJoin([]string{`<div  class = "foo">`, `  <img src="foo">`, `  `, ` </div>`}, "\n")},
		{"range4", stringsJoin([]string{`<div  class = "foo">`, `  |<img  src = "foo"|>    `, `  `, ` </div>`}, "\n"), stringsJoin([]string{`<div  class = "foo">`, `  <img src="foo">    `, `  `, ` </div>`}, "\n")},
		{"range5", stringsJoin([]string{`<div  class = "foo">`, `  <|img  src = "foo">|    `, `  `, ` </div>`}, "\n"), stringsJoin([]string{`<div  class = "foo">`, `  <img  src = "foo">    `, `  `, ` </div>`}, "\n")},
		{"range6", stringsJoin([]string{`<div|  class = "foo">`, `  <img  src = "foo">    `, `  `, ` </div>|`}, "\n"), stringsJoin([]string{`<div  class = "foo">`, `  <img  src = "foo">    `, `  `, ` </div>`}, "\n")},
		{"range7", `<div |class= "foo"|>`, `<div class= "foo">`},
		{"indent", stringsJoin([]string{`<div  class = "foo">`, `  |<img src = "foo">`, `  <img  src = "foo">|`, ` </div>`}, "\n"), stringsJoin([]string{`<div  class = "foo">`, `  <img src="foo">`, `  <img src="foo">`, ` </div>`}, "\n")},
		{"indent2", stringsJoin([]string{`<div  class = "foo">`, `|  <img  src = "foo">`, `  <img  src = "foo">|`, ` </div>`}, "\n"), stringsJoin([]string{`<div  class = "foo">`, `  <img src="foo">`, `  <img src="foo">`, ` </div>`}, "\n")},
		{"indent3", stringsJoin([]string{`<div  class = "foo">`, `  <div></div>   |<img  src = "foo"|>`, ` </div>`}, "\n"), stringsJoin([]string{`<div  class = "foo">`, `  <div></div> <img src="foo">`, ` </div>`}, "\n")},
		{"indent4", stringsJoin([]string{`<div  class = "foo">`, `  <div>|</div>   <img  src = "foo"|>`, ` </div>`}, "\n"), stringsJoin([]string{`<div  class = "foo">`, `  <div></div> <img src="foo">`, ` </div>`}, "\n")},
		{"indent4b", stringsJoin([]string{`<div  class = "foo">`, `  <div></div>|<img src = "foo">`, `    <img  src = "foo">|`, ` </div>`}, "\n"), stringsJoin([]string{`<div  class = "foo">`, `  <div></div><img src="foo">`, `  <img src="foo">`, ` </div>`}, "\n")},
		{"indent5", stringsJoin([]string{`<div  class = "foo">`, `   |<img src = "foo">`, `   <img  src = "foo">|`, ` </div>`}, "\n"), stringsJoin([]string{`<div  class = "foo">`, `  <img src="foo">`, `  <img src="foo">`, ` </div>`}, "\n")},
		{"indent6", stringsJoin([]string{`<div  class = "foo">`, `    |<img src = "foo">`, `    <img  src = "foo">|`, ` </div>`}, "\n"), stringsJoin([]string{`<div  class = "foo">`, `    <img src="foo">`, `    <img src="foo">`, ` </div>`}, "\n")},
		{"indent7", stringsJoin([]string{`<div  class = "foo">`, `    <div></div>|<img src = "foo">`, `      <img  src = "foo">|`, ` </div>`}, "\n"), stringsJoin([]string{`<div  class = "foo">`, `    <div></div><img src="foo">`, `    <img src="foo">`, ` </div>`}, "\n")},
		{"bug36574", `<script src="/js/main.js"> </script>`, `<script src="/js/main.js"> </script>`},
		{"beautify1491", stringsJoin([]string{`<head>`, `    <script src="one.js"></script> <!-- one -->`, `    <script src="two.js"></script> <!-- two-->`, `</head>`}, "\n"), stringsJoin([]string{`<head>`, `  <script src="one.js"></script> <!-- one -->`, `  <script src="two.js"></script> <!-- two-->`, `</head>`}, "\n")},
		{"bug58693", `<a class="btn| btn-link|"></a>`, `<a class="btn btn-link"></a>`},
	}
	for _, tc := range cases {
		if got := formatParity(tc.input, true, nil); got != tc.want {
			t.Fatalf("%s\n got:\n%q\nwant:\n%q", tc.name, got, tc.want)
		}
	}
}

func TestFormatterBaselineCSSOptionsParity(t *testing.T) {
	falseValue := false
	trueValue := true
	cases := []struct {
		name  string
		css   *EmbeddedCSSFormatConfiguration
		input string
		want  string
	}{
		{"selectorsFalse", &EmbeddedCSSFormatConfiguration{NewlineBetweenSelectors: &falseValue}, stringsJoin([]string{"<html>", "", "<head>", "  <style>", "    h1, h2 { color: red; }", "  </style>", "</head>", "", "</html>"}, "\n"), stringsJoin([]string{"<html>", "", "<head>", "  <style>", "    h1, h2 {", "      color: red;", "    }", "  </style>", "</head>", "", "</html>"}, "\n")},
		{"selectorsTrue", &EmbeddedCSSFormatConfiguration{NewlineBetweenSelectors: &trueValue}, stringsJoin([]string{"<html>", "", "<head>", "  <style>", "    h1, h2 { color: red; }", "  </style>", "</head>", "", "</html>"}, "\n"), stringsJoin([]string{"<html>", "", "<head>", "  <style>", "    h1,", "    h2 {", "      color: red;", "    }", "  </style>", "</head>", "", "</html>"}, "\n")},
		{"rulesFalse", &EmbeddedCSSFormatConfiguration{NewlineBetweenRules: &falseValue}, stringsJoin([]string{"<html>", "", "<head>", "  <style>", "    h1 { color: red; }", "    h2 { color: blue; }", "  </style>", "</head>", "", "</html>"}, "\n"), stringsJoin([]string{"<html>", "", "<head>", "  <style>", "    h1 {", "      color: red;", "    }", "    h2 {", "      color: blue;", "    }", "  </style>", "</head>", "", "</html>"}, "\n")},
		{"rulesTrue", &EmbeddedCSSFormatConfiguration{NewlineBetweenRules: &trueValue}, stringsJoin([]string{"<html>", "", "<head>", "  <style>", "    h1 { color: red; }", "    h2 { color: blue; }", "  </style>", "</head>", "", "</html>"}, "\n"), stringsJoin([]string{"<html>", "", "<head>", "  <style>", "    h1 {", "      color: red;", "    }", "", "    h2 {", "      color: blue;", "    }", "  </style>", "</head>", "", "</html>"}, "\n")},
		{"separator", &EmbeddedCSSFormatConfiguration{SpaceAroundSelectorSeparator: &trueValue}, stringsJoin([]string{"<html>", "", "<head>", "  <style>", "    div>span { color: red; }", "  </style>", "</head>", "", "</html>"}, "\n"), stringsJoin([]string{"<html>", "", "<head>", "  <style>", "    div > span {", "      color: red;", "    }", "  </style>", "</head>", "", "</html>"}, "\n")},
		{"defaults", nil, stringsJoin([]string{"<html>", "", "<head>", "  <style>", "    h1, h2 { color: red; }", "  </style>", "</head>", "", "</html>"}, "\n"), stringsJoin([]string{"<html>", "", "<head>", "  <style>", "    h1,", "    h2 {", "      color: red;", "    }", "  </style>", "</head>", "", "</html>"}, "\n")},
	}
	for _, tc := range cases {
		if got := formatParity(tc.input, true, tc.css); got != tc.want {
			t.Fatalf("%s\n got:\n%q\nwant:\n%q", tc.name, got, tc.want)
		}
	}
}
