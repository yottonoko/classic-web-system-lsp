package services

import (
	"context"
	"reflect"
	"strings"
	"testing"

	"github.com/yottonoko/vscode-css-languageservice-go/languagefacts"
	"github.com/yottonoko/vscode-css-languageservice-go/lsp"
)

func TestCSSCompletionStylesheetSelectorsAndProperties(t *testing.T) {
	type completionCase struct {
		name        string
		markedInput string
		expected    []completionExpectation
	}
	groups := []struct {
		name  string
		tests []completionCase
	}{
		{
			name: "stylesheet",
			tests: []completionCase{
				{
					name:        "empty stylesheet",
					markedInput: "| ",
					expected: []completionExpectation{
						{Label: "@import", ResultText: "@import "},
						{Label: "@keyframes", ResultText: "@keyframes "},
						{Label: "div", ResultText: "div "},
					},
				},
				{
					name:        "before body rule",
					markedInput: "| body {",
					expected: []completionExpectation{
						{Label: "@import", ResultText: "@import body {"},
						{Label: "@keyframes", ResultText: "@keyframes body {"},
						{Label: "html", ResultText: "html body {"},
					},
				},
				{
					name:        "selector prefix",
					markedInput: "h| {",
					expected: []completionExpectation{
						{Label: "html", ResultText: "html {"},
					},
				},
				{
					name:        "selector before block",
					markedInput: ".foo |{ ",
					expected: []completionExpectation{
						{Label: "html", ResultText: ".foo html{ "},
						{Label: "display", NotAvailable: true},
					},
				},
			},
		},
		{
			name: "selectors",
			tests: []completionCase{
				{
					name:        "pseudo selector",
					markedInput: "a:h| ",
					expected: []completionExpectation{
						{Label: ":hover", ResultText: "a:hover "},
						{Label: "::after", ResultText: "a::after "},
					},
				},
				{
					name:        "class pseudo selector",
					markedInput: ".a:| ",
					expected: []completionExpectation{
						{Label: ":hover", ResultText: ".a:hover "},
						{Label: "::after", ResultText: ".a::after "},
					},
				},
				{
					name:        "double colon pseudo selector",
					markedInput: "a::h| ",
					expected: []completionExpectation{
						{Label: ":hover", ResultText: "a:hover "},
						{Label: "::after", ResultText: "a::after "},
					},
				},
				{
					name:        "empty double colon pseudo selector",
					markedInput: "a::| ",
					expected: []completionExpectation{
						{Label: ":hover", ResultText: "a:hover "},
						{Label: "::after", ResultText: "a::after "},
					},
				},
				{
					name:        "empty pseudo selector",
					markedInput: "a:| ",
					expected: []completionExpectation{
						{Label: ":hover", ResultText: "a:hover "},
						{Label: "::after", ResultText: "a::after "},
					},
				},
				{
					name:        "replace pseudo selector suffix",
					markedInput: "a:|hover ",
					expected: []completionExpectation{
						{Label: ":hover", ResultText: "a:hover "},
						{Label: "::after", ResultText: "a::after "},
					},
				},
				{
					name:        "double colon before following rule",
					markedInput: "a::| foo { }",
					expected: []completionExpectation{
						{Label: ":hover", ResultText: "a:hover foo { }"},
						{Label: "::after", ResultText: "a::after foo { }"},
					},
				},
				{
					name:        "colon before following rule",
					markedInput: "a:| foo { }",
					expected: []completionExpectation{
						{Label: ":hover", ResultText: "a:hover foo { }"},
						{Label: "::after", ResultText: "a::after foo { }"},
					},
				},
				{
					name:        "id selector pseudo context",
					markedInput: "a#| ",
					expected: []completionExpectation{
						{Label: ":hover", ResultText: "a:hover "},
						{Label: "::after", ResultText: "a::after "},
					},
				},
				{
					name:        "class selector pseudo context",
					markedInput: "a.| ",
					expected: []completionExpectation{
						{Label: ":hover", ResultText: "a:hover "},
						{Label: "::after", ResultText: "a::after "},
					},
				},
				{
					name:        "nested class pseudo selector",
					markedInput: ".a { .b:| }",
					expected: []completionExpectation{
						{Label: ":hover", ResultText: ".a { .b:hover }"},
						{Label: "::after", ResultText: ".a { .b::after }"},
					},
				},
				{
					name:        "nested element pseudo selector",
					markedInput: ".a { div:h| {} }",
					expected: []completionExpectation{
						{Label: ":hover", ResultText: ".a { div:hover {} }"},
					},
				},
			},
		},
		{
			name: "properties",
			tests: []completionCase{
				{
					name:        "property start",
					markedInput: "body {|",
					expected: []completionExpectation{
						{Label: "display", Kind: lsp.CompletionItemKindProperty, ResultText: "body {display: "},
						{Label: "background", Kind: lsp.CompletionItemKindProperty, ResultText: "body {background: "},
					},
				},
				{
					name:        "property prefix",
					markedInput: "body { ver|",
					expected: []completionExpectation{
						{Label: "vertical-align", Kind: lsp.CompletionItemKindProperty, ResultText: "body { vertical-align: "},
					},
				},
				{
					name:        "property prefix inside word",
					markedInput: "body { vertical-ali|gn",
					expected: []completionExpectation{
						{Label: "vertical-align", Kind: lsp.CompletionItemKindProperty, ResultText: "body { vertical-align: "},
					},
				},
				{
					name:        "property at end of property name",
					markedInput: "body { vertical-align|",
					expected: []completionExpectation{
						{Label: "vertical-align", Kind: lsp.CompletionItemKindProperty, ResultText: "body { vertical-align: "},
					},
				},
				{
					name:        "property before colon",
					markedInput: "body { vertical-align|: bottom;}",
					expected: []completionExpectation{
						{Label: "vertical-align", Kind: lsp.CompletionItemKindProperty, ResultText: "body { vertical-align: bottom;}"},
					},
				},
				{
					name:        "transition",
					markedInput: "body { trans| ",
					expected: []completionExpectation{
						{Label: "transition", Kind: lsp.CompletionItemKindProperty, ResultText: "body { transition:  "},
					},
				},
			},
		},
		{
			name: "MDN properties",
			tests: []completionCase{
				{
					name:        "mask properties",
					markedInput: "body { m|",
					expected: []completionExpectation{
						{Label: "mask", Kind: lsp.CompletionItemKindProperty, ResultText: "body { mask: "},
						{Label: "mask-border", Kind: lsp.CompletionItemKindProperty, ResultText: "body { mask-border: "},
						{Label: "-webkit-mask", Kind: lsp.CompletionItemKindProperty, ResultText: "body { -webkit-mask: "},
					},
				},
			},
		},
	}
	for _, group := range groups {
		t.Run(group.name, func(t *testing.T) {
			for _, tt := range group.tests {
				t.Run(tt.name, func(t *testing.T) {
					assertCompletionItems(t, tt.markedInput, nil, tt.expected)
				})
			}
		})
	}
}

func TestCSSCompletionValuesColorsAndVariables(t *testing.T) {
	type completionCase struct {
		name        string
		markedInput string
		expected    []completionExpectation
		count       *int
	}
	zero := 0
	groups := []struct {
		name  string
		tests []completionCase
	}{
		{
			name: "values",
			tests: []completionCase{
				{
					name:        "value after colon without space",
					markedInput: "body { vertical-align:| bottom;}",
					expected: []completionExpectation{
						{Label: "bottom", Kind: lsp.CompletionItemKindValue, ResultText: "body { vertical-align:bottom bottom;}"},
						{Label: "0cm", Kind: lsp.CompletionItemKindUnit, ResultText: "body { vertical-align:0cm bottom;}"},
					},
				},
				{
					name:        "property values and units",
					markedInput: "body { vertical-align: |bottom;}",
					expected: []completionExpectation{
						{Label: "bottom", Kind: lsp.CompletionItemKindValue, ResultText: "body { vertical-align: bottom;}"},
						{Label: "0cm", Kind: lsp.CompletionItemKindUnit, ResultText: "body { vertical-align: 0cm;}"},
					},
				},
				{
					name:        "partial enum value at end",
					markedInput: "body { vertical-align: bott|",
					expected: []completionExpectation{
						{Label: "bottom", Kind: lsp.CompletionItemKindValue, ResultText: "body { vertical-align: bottom"},
					},
				},
				{
					name:        "partial enum value",
					markedInput: "body { vertical-align: bott|om }",
					expected: []completionExpectation{
						{Label: "bottom", Kind: lsp.CompletionItemKindValue, ResultText: "body { vertical-align: bottom }"},
					},
				},
				{
					name:        "complete enum value before whitespace",
					markedInput: "body { vertical-align: bottom| }",
					expected: []completionExpectation{
						{Label: "bottom", Kind: lsp.CompletionItemKindValue, ResultText: "body { vertical-align: bottom }"},
					},
				},
				{
					name:        "partial enum value without space",
					markedInput: "body { vertical-align:bott|",
					expected: []completionExpectation{
						{Label: "bottom", Kind: lsp.CompletionItemKindValue, ResultText: "body { vertical-align:bottom"},
					},
				},
				{
					name:        "complete enum value before semicolon",
					markedInput: "body { vertical-align: bottom|; }",
					expected: []completionExpectation{
						{Label: "bottom", Kind: lsp.CompletionItemKindValue, ResultText: "body { vertical-align: bottom; }"},
					},
				},
				{
					name:        "after completed declaration semicolon",
					markedInput: "body { vertical-align: bottom;| }",
					count:       &zero,
				},
				{
					name:        "after semicolon returns properties",
					markedInput: "body { vertical-align: bottom; |}",
					expected: []completionExpectation{
						{Label: "display", Kind: lsp.CompletionItemKindProperty, ResultText: "body { vertical-align: bottom; display: }"},
					},
				},
				{
					name:        "url function value",
					markedInput: ".head { background-image: |}",
					expected: []completionExpectation{
						{Label: "url()", Kind: lsp.CompletionItemKindFunction, ResultText: ".head { background-image: url($1)}"},
					},
				},
				{
					name:        "enum values",
					markedInput: "#id { justify-content: |",
					expected: []completionExpectation{
						{Label: "center", Kind: lsp.CompletionItemKindValue, ResultText: "#id { justify-content: center"},
						{Label: "start", Kind: lsp.CompletionItemKindValue, ResultText: "#id { justify-content: start"},
						{Label: "end", Kind: lsp.CompletionItemKindValue, ResultText: "#id { justify-content: end"},
						{Label: "left", Kind: lsp.CompletionItemKindValue, ResultText: "#id { justify-content: left"},
						{Label: "right", Kind: lsp.CompletionItemKindValue, ResultText: "#id { justify-content: right"},
						{Label: "space-evenly", Kind: lsp.CompletionItemKindValue, ResultText: "#id { justify-content: space-evenly"},
					},
				},
				{
					name:        "unknown current value not repeated",
					markedInput: ".foo { te:n| }",
					expected: []completionExpectation{
						{Label: "n", NotAvailable: true},
					},
				},
			},
		},
		{
			name: "functions",
			tests: []completionCase{
				{
					name:        "transform function",
					markedInput: "@keyframes fadeIn { 0% { transform: s|",
					expected: []completionExpectation{
						{Label: "scaleX()", Kind: lsp.CompletionItemKindFunction, InsertTextFormat: lsp.InsertTextFormatSnippet, ResultText: "@keyframes fadeIn { 0% { transform: scaleX($1)"},
					},
				},
			},
		},
		{
			name: "positions",
			tests: []completionCase{
				{
					name:        "background position",
					markedInput: "html { background-position: t|",
					expected: []completionExpectation{
						{Label: "top", Kind: lsp.CompletionItemKindValue, ResultText: "html { background-position: top"},
						{Label: "right", Kind: lsp.CompletionItemKindValue, ResultText: "html { background-position: right"},
					},
				},
			},
		},
		{
			name: "units",
			tests: []completionCase{
				{
					name:        "integer unit stem",
					markedInput: "body { vertical-align: 9| }",
					expected: []completionExpectation{
						{Label: "9cm", Kind: lsp.CompletionItemKindUnit, ResultText: "body { vertical-align: 9cm }"},
					},
				},
				{
					name:        "decimal unit stem",
					markedInput: "body { vertical-align: 1.2| }",
					expected: []completionExpectation{
						{Label: "1.2em", Kind: lsp.CompletionItemKindUnit, ResultText: "body { vertical-align: 1.2em }"},
					},
				},
				{
					name:        "replace partial number",
					markedInput: "body { vertical-align: 1|0 }",
					expected: []completionExpectation{
						{Label: "1cm", Kind: lsp.CompletionItemKindUnit, ResultText: "body { vertical-align: 1cm }"},
					},
				},
				{
					name:        "replace partial unit",
					markedInput: "body { vertical-align: 10c| }",
					expected: []completionExpectation{
						{Label: "10cm", Kind: lsp.CompletionItemKindUnit, ResultText: "body { vertical-align: 10cm }"},
					},
				},
				{
					name:        "negative unit stem",
					markedInput: "body { top: -2px| }",
					expected: []completionExpectation{
						{Label: "-2px", Kind: lsp.CompletionItemKindUnit, ResultText: "body { top: -2px }"},
					},
				},
			},
		},
		{
			name: "unknown",
			tests: []completionCase{
				{
					name:        "unknown property no values",
					markedInput: "body { notexisting: |;}",
					count:       &zero,
				},
				{
					name:        "unknown property reused values",
					markedInput: ".foo { unknown: foo; } .bar { unknown:| }",
					expected: []completionExpectation{
						{Label: "foo", Kind: lsp.CompletionItemKindValue, ResultText: ".foo { unknown: foo; } .bar { unknown:foo }"},
					},
				},
			},
		},
		{
			name: "colors",
			tests: []completionCase{
				{
					name:        "border colors",
					markedInput: "body { border-right: |",
					expected: []completionExpectation{
						{Label: "cyan", Kind: lsp.CompletionItemKindColor, ResultText: "body { border-right: cyan"},
						{Label: "dotted", Kind: lsp.CompletionItemKindValue, ResultText: "body { border-right: dotted"},
						{Label: "0em", Kind: lsp.CompletionItemKindUnit, ResultText: "body { border-right: 0em"},
					},
				},
				{
					name:        "replace border color before other values",
					markedInput: "body { border-right: cyan| dotted 2em ",
					expected: []completionExpectation{
						{Label: "cyan", Kind: lsp.CompletionItemKindColor, ResultText: "body { border-right: cyan dotted 2em "},
						{Label: "darkcyan", Kind: lsp.CompletionItemKindColor, ResultText: "body { border-right: darkcyan dotted 2em "},
					},
				},
				{
					name:        "border color after width and style",
					markedInput: "body { border-right: dotted 2em |",
					expected: []completionExpectation{
						{Label: "cyan", Kind: lsp.CompletionItemKindColor, ResultText: "body { border-right: dotted 2em cyan"},
					},
				},
				{
					name:        "reuse document colors",
					markedInput: ".foo { background-color: #123456; } .bar { background-color:| }",
					expected: []completionExpectation{
						{Label: "#123456", Kind: lsp.CompletionItemKindColor, ResultText: ".foo { background-color: #123456; } .bar { background-color:#123456 }"},
					},
				},
				{
					name:        "do not offer current partial color",
					markedInput: ".bar { background-color: #123| }",
					expected: []completionExpectation{
						{Label: "#123", NotAvailable: true},
					},
				},
				{
					name:        "color functions and names",
					markedInput: ".foo { background-color: r|",
					expected: []completionExpectation{
						{Label: "rgb", Kind: lsp.CompletionItemKindFunction, ResultText: ".foo { background-color: rgb(${1:red}, ${2:green}, ${3:blue})"},
						{Label: "rgba", Kind: lsp.CompletionItemKindFunction, ResultText: ".foo { background-color: rgba(${1:red}, ${2:green}, ${3:blue}, ${4:alpha})"},
						{Label: "rgb relative", Kind: lsp.CompletionItemKindFunction, ResultText: ".foo { background-color: rgb(from ${1:color} ${2:r} ${3:g} ${4:b})"},
						{Label: "red", Kind: lsp.CompletionItemKindColor, ResultText: ".foo { background-color: red"},
					},
				},
			},
		},
		{
			name: "variables",
			tests: []completionCase{
				{
					name:        "css variable color",
					markedInput: ":root { --myvar: red; } body { color: |",
					expected: []completionExpectation{
						{Label: "--myvar", Kind: lsp.CompletionItemKindColor, Documentation: "red", ResultText: ":root { --myvar: red; } body { color: var(--myvar)"},
					},
				},
				{
					name:        "css variable function prefix",
					markedInput: "body { --myvar: 0px; border-right: var| ",
					expected: []completionExpectation{
						{Label: "--myvar", Kind: lsp.CompletionItemKindVariable, Documentation: "0px", ResultText: "body { --myvar: 0px; border-right: var(--myvar) "},
					},
				},
				{
					name:        "css variable inside var",
					markedInput: "body { --myvar: 0px; border-right: var(| ",
					expected: []completionExpectation{
						{Label: "--myvar", Kind: lsp.CompletionItemKindVariable, Documentation: "0px", ResultText: "body { --myvar: 0px; border-right: var(--myvar "},
					},
				},
				{
					name:        "later root variable",
					markedInput: "a { color: | } :root { --bg-color: red; } ",
					expected: []completionExpectation{
						{Label: "--bg-color", Kind: lsp.CompletionItemKindColor, Documentation: "red", ResultText: "a { color: var(--bg-color) } :root { --bg-color: red; } "},
					},
				},
				{
					name:        "undocumented variable",
					markedInput: "body { border-left: --borderwidth; border-right: var(| ",
					expected: []completionExpectation{
						{Label: "--borderwidth", ResultText: "body { border-left: --borderwidth; border-right: var(--borderwidth "},
					},
				},
				{
					name:        "color-valued custom properties",
					markedInput: "a { color: | } :root { --color-hex3: #f00; --color-hex4: #F007; --color-hex6: #ff0000; --color-hex8: #ff000077; --color-named: black; --color-keyword: currentColor; }",
					expected: []completionExpectation{
						{Label: "--color-hex3", Kind: lsp.CompletionItemKindColor, ResultText: "a { color: var(--color-hex3) } :root { --color-hex3: #f00; --color-hex4: #F007; --color-hex6: #ff0000; --color-hex8: #ff000077; --color-named: black; --color-keyword: currentColor; }"},
						{Label: "--color-hex4", Kind: lsp.CompletionItemKindColor, ResultText: "a { color: var(--color-hex4) } :root { --color-hex3: #f00; --color-hex4: #F007; --color-hex6: #ff0000; --color-hex8: #ff000077; --color-named: black; --color-keyword: currentColor; }"},
						{Label: "--color-hex6", Kind: lsp.CompletionItemKindColor, ResultText: "a { color: var(--color-hex6) } :root { --color-hex3: #f00; --color-hex4: #F007; --color-hex6: #ff0000; --color-hex8: #ff000077; --color-named: black; --color-keyword: currentColor; }"},
						{Label: "--color-hex8", Kind: lsp.CompletionItemKindColor, ResultText: "a { color: var(--color-hex8) } :root { --color-hex3: #f00; --color-hex4: #F007; --color-hex6: #ff0000; --color-hex8: #ff000077; --color-named: black; --color-keyword: currentColor; }"},
						{Label: "--color-named", Kind: lsp.CompletionItemKindColor, ResultText: "a { color: var(--color-named) } :root { --color-hex3: #f00; --color-hex4: #F007; --color-hex6: #ff0000; --color-hex8: #ff000077; --color-named: black; --color-keyword: currentColor; }"},
						{Label: "--color-keyword", Kind: lsp.CompletionItemKindColor, ResultText: "a { color: var(--color-keyword) } :root { --color-hex3: #f00; --color-hex4: #F007; --color-hex6: #ff0000; --color-hex8: #ff000077; --color-named: black; --color-keyword: currentColor; }"},
					},
				},
				{
					name:        "border-valued custom properties",
					markedInput: "a { color: | } :root { --border-hex3: solid #f00 1px; --border-hex4: solid #F007 1px; --border-hex6: 1px #ff0000 solid; --border-hex8: #ff000077 #ff000077; --border-named: solid black 1px; --border-keyword: currentColor wavy; }",
					expected: []completionExpectation{
						{Label: "--border-hex3", Kind: lsp.CompletionItemKindVariable, ResultText: "a { color: var(--border-hex3) } :root { --border-hex3: solid #f00 1px; --border-hex4: solid #F007 1px; --border-hex6: 1px #ff0000 solid; --border-hex8: #ff000077 #ff000077; --border-named: solid black 1px; --border-keyword: currentColor wavy; }"},
						{Label: "--border-hex4", Kind: lsp.CompletionItemKindVariable, ResultText: "a { color: var(--border-hex4) } :root { --border-hex3: solid #f00 1px; --border-hex4: solid #F007 1px; --border-hex6: 1px #ff0000 solid; --border-hex8: #ff000077 #ff000077; --border-named: solid black 1px; --border-keyword: currentColor wavy; }"},
						{Label: "--border-hex6", Kind: lsp.CompletionItemKindVariable, ResultText: "a { color: var(--border-hex6) } :root { --border-hex3: solid #f00 1px; --border-hex4: solid #F007 1px; --border-hex6: 1px #ff0000 solid; --border-hex8: #ff000077 #ff000077; --border-named: solid black 1px; --border-keyword: currentColor wavy; }"},
						{Label: "--border-hex8", Kind: lsp.CompletionItemKindVariable, ResultText: "a { color: var(--border-hex8) } :root { --border-hex3: solid #f00 1px; --border-hex4: solid #F007 1px; --border-hex6: 1px #ff0000 solid; --border-hex8: #ff000077 #ff000077; --border-named: solid black 1px; --border-keyword: currentColor wavy; }"},
						{Label: "--border-named", Kind: lsp.CompletionItemKindVariable, ResultText: "a { color: var(--border-named) } :root { --border-hex3: solid #f00 1px; --border-hex4: solid #F007 1px; --border-hex6: 1px #ff0000 solid; --border-hex8: #ff000077 #ff000077; --border-named: solid black 1px; --border-keyword: currentColor wavy; }"},
						{Label: "--border-keyword", Kind: lsp.CompletionItemKindVariable, ResultText: "a { color: var(--border-keyword) } :root { --border-hex3: solid #f00 1px; --border-hex4: solid #F007 1px; --border-hex6: 1px #ff0000 solid; --border-hex8: #ff000077 #ff000077; --border-named: solid black 1px; --border-keyword: currentColor wavy; }"},
					},
				},
				{
					name:        "modern rgb custom property",
					markedInput: "a { color: | } :root { --color-rgb: rgb(255 0 125 / 50%); }",
					expected: []completionExpectation{
						{Label: "--color-rgb", Kind: lsp.CompletionItemKindVariable, ResultText: "a { color: var(--color-rgb) } :root { --color-rgb: rgb(255 0 125 / 50%); }"},
					},
				},
				{
					name:        "modern rgb border custom property",
					markedInput: "a { color: | } :root { --border-rgb: solid rgb(255 0 125 / 50%) 2px; }",
					expected: []completionExpectation{
						{Label: "--border-rgb", Kind: lsp.CompletionItemKindVariable, ResultText: "a { color: var(--border-rgb) } :root { --border-rgb: solid rgb(255 0 125 / 50%) 2px; }"},
					},
				},
			},
		},
	}
	for _, group := range groups {
		t.Run(group.name, func(t *testing.T) {
			for _, tt := range group.tests {
				t.Run(tt.name, func(t *testing.T) {
					if tt.count != nil {
						assertCompletionCount(t, tt.markedInput, nil, CompletionOptions{}, *tt.count)
					}
					assertCompletionItems(t, tt.markedInput, nil, tt.expected)
				})
			}
		})
	}
}

func TestSCSSCompletionVariables(t *testing.T) {
	assertCompletionItemsForLanguage(t, "scss", "$i: 0; body { width: |", nil, CompletionOptions{}, []completionExpectation{
		{Label: "$i", Kind: lsp.CompletionItemKindVariable, Documentation: "0", ResultText: "$i: 0; body { width: $i"},
	})
	assertCompletionItemsForLanguage(t, "scss", "@for $i from 1 through 3 { .item-#{|} { width: 2em * $i; } }", nil, CompletionOptions{}, []completionExpectation{
		{Label: "$i", Kind: lsp.CompletionItemKindVariable, ResultText: "@for $i from 1 through 3 { .item-#{$i} { width: 2em * $i; } }"},
	})
	assertCompletionItemsForLanguage(t, "scss", "@for $i from 1 through 3 { .item-#{|$i} { width: 2em * $i; } }", nil, CompletionOptions{}, []completionExpectation{
		{Label: "$i", Kind: lsp.CompletionItemKindVariable, ResultText: "@for $i from 1 through 3 { .item-#{$i} { width: 2em * $i; } }"},
	})
	assertCompletionItemsForLanguage(t, "scss", "@mixin mixin($a: 1, $b) { content: $|}", nil, CompletionOptions{}, []completionExpectation{
		{Label: "$a", Kind: lsp.CompletionItemKindVariable, Documentation: "1", Detail: "argument from 'mixin'", ResultText: "@mixin mixin($a: 1, $b) { content: $a}"},
		{Label: "$b", Kind: lsp.CompletionItemKindVariable, Detail: "argument from 'mixin'", ResultText: "@mixin mixin($a: 1, $b) { content: $b}"},
	})
	assertCompletionItemsForLanguage(t, "scss", "di| span { } ", nil, CompletionOptions{}, []completionExpectation{
		{Label: "div", ResultText: "div span { } "},
		{Label: "display", NotAvailable: true},
	})
	assertCompletionItemsForLanguage(t, "scss", "span { di|} ", nil, CompletionOptions{}, []completionExpectation{
		{Label: "div", NotAvailable: true},
		{Label: "display", ResultText: "span { display: } "},
	})
	assertCompletionItemsForLanguage(t, "scss", ".foo { .|", nil, CompletionOptions{}, []completionExpectation{
		{Label: ".foo", ResultText: ".foo { .foo"},
	})
	assertCompletionCountForLanguage(t, "scss", ".foo { display: block;|", nil, CompletionOptions{}, 0)
	assertCompletionItemsForLanguage(t, "scss", ".foo { &:|", nil, CompletionOptions{}, []completionExpectation{
		{Label: ":last-of-type", ResultText: ".foo { &:last-of-type"},
	})
	assertCompletionItemsForLanguage(t, "scss", ".foo { &:l|", nil, CompletionOptions{}, []completionExpectation{
		{Label: ":last-of-type", ResultText: ".foo { &:last-of-type"},
	})
	assertCompletionItemsForLanguage(t, "scss", ".test { &::|  }", nil, CompletionOptions{}, []completionExpectation{
		{Label: ":hover", ResultText: ".test { &:hover  }"},
		{Label: "::after", ResultText: ".test { &::after  }"},
	})
	assertCompletionItemsForLanguage(t, "scss", "@include media('ddd') { dis| &:not(:first-child) {", nil, CompletionOptions{}, []completionExpectation{
		{Label: "display"},
	})
	assertCompletionItemsForLanguage(t, "scss", ".foo { } @mixin bar { @extend | }", nil, CompletionOptions{}, []completionExpectation{
		{Label: ".foo"},
	})
	assertCompletionItemsForLanguage(t, "scss", ".foo { } @mixin bar { @extend fo| }", nil, CompletionOptions{}, []completionExpectation{
		{Label: ".foo"},
	})
	assertCompletionItemsForLanguage(t, "scss", ".foo { mask: no|", nil, CompletionOptions{}, []completionExpectation{
		{Label: "round"},
	})
	assertCompletionItemsForLanguage(t, "scss", ".foo { .foobar { .foobar2 {  outline-color: blue; cool  }| } .fokzlb {} .baaaa { counter - reset: unset;}", nil, CompletionOptions{}, []completionExpectation{
		{Label: "display"},
	})
	assertCompletionItemsForLanguage(t, "scss", "div { &:hover { } | }", nil, CompletionOptions{}, []completionExpectation{
		{Label: "display"},
	})
}

func TestSCSSCompletionAtRules(t *testing.T) {
	t.Run("at rules", func(t *testing.T) {
		assertCompletionItemsForLanguage(t, "scss", "@|", nil, CompletionOptions{}, []completionExpectation{
			{Label: "@extend", Kind: lsp.CompletionItemKindKeyword, ResultText: "@extend"},
			{Label: "@at-root", Kind: lsp.CompletionItemKindKeyword},
			{Label: "@debug", Kind: lsp.CompletionItemKindKeyword},
			{Label: "@warn", Kind: lsp.CompletionItemKindKeyword},
			{Label: "@error", Kind: lsp.CompletionItemKindKeyword},
			{Label: "@if", Kind: lsp.CompletionItemKindKeyword, InsertTextFormat: lsp.InsertTextFormatSnippet},
			{Label: "@for", Kind: lsp.CompletionItemKindKeyword, InsertTextFormat: lsp.InsertTextFormatSnippet},
			{Label: "@each", Kind: lsp.CompletionItemKindKeyword, InsertTextFormat: lsp.InsertTextFormatSnippet},
			{Label: "@while", Kind: lsp.CompletionItemKindKeyword, InsertTextFormat: lsp.InsertTextFormatSnippet},
			{Label: "@mixin", Kind: lsp.CompletionItemKindKeyword, InsertTextFormat: lsp.InsertTextFormatSnippet},
			{Label: "@include", Kind: lsp.CompletionItemKindKeyword, ResultText: "@include"},
			{Label: "@function", Kind: lsp.CompletionItemKindKeyword},
		})
		assertCompletionItemsForLanguage(t, "scss", ".foo { | }", nil, CompletionOptions{}, []completionExpectation{
			{Label: "@include", Kind: lsp.CompletionItemKindKeyword, ResultText: ".foo { @include }"},
			{Label: "@extend", Kind: lsp.CompletionItemKindKeyword, ResultText: ".foo { @extend }"},
			{Label: "@at-root", Kind: lsp.CompletionItemKindKeyword},
			{Label: "@debug", Kind: lsp.CompletionItemKindKeyword},
			{Label: "@warn", Kind: lsp.CompletionItemKindKeyword},
			{Label: "@error", Kind: lsp.CompletionItemKindKeyword},
			{Label: "@if", Kind: lsp.CompletionItemKindKeyword},
			{Label: "@for", Kind: lsp.CompletionItemKindKeyword},
			{Label: "@each", Kind: lsp.CompletionItemKindKeyword},
			{Label: "@while", Kind: lsp.CompletionItemKindKeyword},
			{Label: "@mixin", Kind: lsp.CompletionItemKindKeyword},
			{Label: "@function", Kind: lsp.CompletionItemKindKeyword},
			{Label: "@use", NotAvailable: true},
			{Label: "@forward", NotAvailable: true},
		})
		assertCompletionItemsForLanguage(t, "scss", ".foo { @| }", nil, CompletionOptions{}, []completionExpectation{
			{Label: "@include", Kind: lsp.CompletionItemKindKeyword, ResultText: ".foo { @include }"},
			{Label: "@if", Kind: lsp.CompletionItemKindKeyword, InsertTextFormat: lsp.InsertTextFormatSnippet},
			{Label: "@use", NotAvailable: true},
			{Label: "@forward", NotAvailable: true},
		})
		assertCompletionItemsForLanguage(t, "scss", ".foo { @f| }", nil, CompletionOptions{}, []completionExpectation{
			{Label: "@for", Kind: lsp.CompletionItemKindKeyword, InsertTextFormat: lsp.InsertTextFormatSnippet},
		})
		for _, input := range []string{
			"@for $i from 1 through 3 { .item-#{$i} { width: 2em * $i; } } @|",
			".foo { @if $a = 5 { } @| }",
			".foo { @debug 10em + 22em; @| }",
		} {
			assertCompletionItemsForLanguage(t, "scss", input, nil, CompletionOptions{}, []completionExpectation{
				{Label: "@extend"},
				{Label: "@at-root"},
				{Label: "@debug"},
				{Label: "@warn"},
				{Label: "@error"},
				{Label: "@if"},
				{Label: "@for"},
				{Label: "@each"},
				{Label: "@while"},
				{Label: "@mixin"},
				{Label: "@include"},
				{Label: "@function"},
			})
		}
		assertCompletionItemsForLanguage(t, "scss", ".foo { @if $a = 5 { } @f| }", nil, CompletionOptions{}, []completionExpectation{
			{Label: "@for", Kind: lsp.CompletionItemKindKeyword, InsertTextFormat: lsp.InsertTextFormatSnippet},
		})
	})
}

func TestSCSSCompletionFunctionsAndMixins(t *testing.T) {
	assertCompletionItemsForLanguage(t, "scss", ".foo { background-color: d|", nil, CompletionOptions{}, []completionExpectation{
		{Label: "darken", Kind: lsp.CompletionItemKindFunction, InsertTextFormat: lsp.InsertTextFormatSnippet, ResultText: ".foo { background-color: darken(\\$color: ${1:#000000}, \\$amount: ${2:0})"},
		{Label: "desaturate", Kind: lsp.CompletionItemKindFunction},
	})
	assertCompletionItemsForLanguage(t, "scss", "@function foo($x, $y) { @return $x + $y; } .foo { background-color: f|", nil, CompletionOptions{}, []completionExpectation{
		{Label: "foo", Kind: lsp.CompletionItemKindFunction, InsertTextFormat: lsp.InsertTextFormatSnippet, ResultText: "@function foo($x, $y) { @return $x + $y; } .foo { background-color: foo(${1:$x}, ${2:$y})"},
	})
	assertCompletionItemsForLanguage(t, "scss", "@mixin mixin($a: 1, $b) { content: $a + $b; } @include m|", nil, CompletionOptions{}, []completionExpectation{
		{Label: "mixin", Kind: lsp.CompletionItemKindFunction, InsertTextFormat: lsp.InsertTextFormatSnippet, ResultText: "@mixin mixin($a: 1, $b) { content: $a + $b; } @include mixin(${1:$a}, ${2:$b})"},
	})
}

func TestSCSSCompletionModules(t *testing.T) {
	mathDocumentation := lsp.MarkupContent{
		Kind:  lsp.MarkupKindMarkdown,
		Value: "Provides functions that operate on numbers.\n\n[Sass documentation](https://sass-lang.com/documentation/modules/math)",
	}
	t.Run("module-loading at-rules", func(t *testing.T) {
		assertCompletionItemsForLanguage(t, "scss", "@|", nil, CompletionOptions{}, []completionExpectation{
			{Label: "@use", Kind: lsp.CompletionItemKindKeyword, InsertTextFormat: lsp.InsertTextFormatSnippet, DocumentationIncludes: "[Sass documentation](https://sass-lang.com/documentation/at-rules/use)"},
			{Label: "@forward", Kind: lsp.CompletionItemKindKeyword, InsertTextFormat: lsp.InsertTextFormatSnippet, DocumentationIncludes: "[Sass documentation](https://sass-lang.com/documentation/at-rules/forward)"},
		})
		assertCompletionItemsForLanguage(t, "scss", ".foo { @| }", nil, CompletionOptions{}, []completionExpectation{
			{Label: "@use", NotAvailable: true},
			{Label: "@forward", NotAvailable: true},
		})
	})
	assertCompletionItemsForLanguage(t, "scss", `@use '|'`, nil, CompletionOptions{}, []completionExpectation{
		{Label: "sass:math", Kind: lsp.CompletionItemKindModule, ResultText: `@use 'sass:math'`, Documentation: mathDocumentation},
		{Label: "sass:string", Kind: lsp.CompletionItemKindModule, DocumentationIncludes: "[Sass documentation](https://sass-lang.com/documentation/modules/string)"},
		{Label: "sass:color", Kind: lsp.CompletionItemKindModule, DocumentationIncludes: "[Sass documentation](https://sass-lang.com/documentation/modules/color)"},
		{Label: "sass:list", Kind: lsp.CompletionItemKindModule, DocumentationIncludes: "[Sass documentation](https://sass-lang.com/documentation/modules/list)"},
		{Label: "sass:map", Kind: lsp.CompletionItemKindModule, DocumentationIncludes: "[Sass documentation](https://sass-lang.com/documentation/modules/map)"},
		{Label: "sass:selector", Kind: lsp.CompletionItemKindModule, DocumentationIncludes: "[Sass documentation](https://sass-lang.com/documentation/modules/selector)"},
		{Label: "sass:meta", Kind: lsp.CompletionItemKindModule, DocumentationIncludes: "[Sass documentation](https://sass-lang.com/documentation/modules/meta)"},
	})
	assertCompletionItemsForLanguage(t, "scss", `@forward '|'`, nil, CompletionOptions{}, []completionExpectation{
		{Label: "sass:math", Kind: lsp.CompletionItemKindModule, ResultText: `@forward 'sass:math'`, DocumentationIncludes: "[Sass documentation](https://sass-lang.com/documentation/modules/math)"},
		{Label: "sass:string", Kind: lsp.CompletionItemKindModule, DocumentationIncludes: "[Sass documentation](https://sass-lang.com/documentation/modules/string)"},
		{Label: "sass:color", Kind: lsp.CompletionItemKindModule, DocumentationIncludes: "[Sass documentation](https://sass-lang.com/documentation/modules/color)"},
		{Label: "sass:list", Kind: lsp.CompletionItemKindModule, DocumentationIncludes: "[Sass documentation](https://sass-lang.com/documentation/modules/list)"},
		{Label: "sass:map", Kind: lsp.CompletionItemKindModule, DocumentationIncludes: "[Sass documentation](https://sass-lang.com/documentation/modules/map)"},
		{Label: "sass:selector", Kind: lsp.CompletionItemKindModule, DocumentationIncludes: "[Sass documentation](https://sass-lang.com/documentation/modules/selector)"},
		{Label: "sass:meta", Kind: lsp.CompletionItemKindModule, DocumentationIncludes: "[Sass documentation](https://sass-lang.com/documentation/modules/meta)"},
	})
	assertCompletionItemsForLanguage(t, "scss", `@use 'sass:|'`, nil, CompletionOptions{}, []completionExpectation{
		{Label: "sass:math", Kind: lsp.CompletionItemKindModule, ResultText: `@use 'sass:math'`},
	})
	assertCompletionItemsForLanguage(t, "scss", `@use '|`, nil, CompletionOptions{}, []completionExpectation{
		{Label: "sass:math", Kind: lsp.CompletionItemKindModule, ResultText: `@use 'sass:math'`},
	})
	assertCompletionItemsForLanguage(t, "scss", `@import '|'`, nil, CompletionOptions{}, []completionExpectation{
		{Label: "sass:math", NotAvailable: true},
	})
}

func TestLESSCompletionVariables(t *testing.T) {
	assertCompletionItemsForLanguage(t, "less", "body { |", nil, CompletionOptions{}, []completionExpectation{
		{Label: "display"},
		{Label: "background"},
	})
	assertCompletionItemsForLanguage(t, "less", "body { ver|", nil, CompletionOptions{}, []completionExpectation{
		{Label: "vertical-align"},
	})
	assertCompletionItemsForLanguage(t, "less", "body { word-break: |", nil, CompletionOptions{}, []completionExpectation{
		{Label: "keep-all"},
	})
	assertCompletionItemsForLanguage(t, "less", "body { inner { vertical-align: |}", nil, CompletionOptions{}, []completionExpectation{
		{Label: "bottom"},
	})
	assertCompletionItemsForLanguage(t, "less", "@var1: 3; body { inner { vertical-align: |}", nil, CompletionOptions{}, []completionExpectation{
		{Label: "@var1", Kind: lsp.CompletionItemKindVariable, Documentation: "3", ResultText: "@var1: 3; body { inner { vertical-align: @var1}"},
	})
	assertCompletionItemsForLanguage(t, "less", "@var1: { content: 1; }; body { inner { vertical-align: |}", nil, CompletionOptions{}, []completionExpectation{
		{Label: "@var1", Kind: lsp.CompletionItemKindVariable, Documentation: "{ content: 1; }", ResultText: "@var1: { content: 1; }; body { inner { vertical-align: @var1}"},
	})
	assertCompletionItemsForLanguage(t, "less", ".mixin(@a: 1, @b) { content: @|}", nil, CompletionOptions{}, []completionExpectation{
		{Label: "@a", Kind: lsp.CompletionItemKindVariable, Documentation: "1", Detail: "argument from '.mixin'", ResultText: ".mixin(@a: 1, @b) { content: @a}"},
		{Label: "@b", Kind: lsp.CompletionItemKindVariable, Detail: "argument from '.mixin'", ResultText: ".mixin(@a: 1, @b) { content: @b}"},
	})
	assertCompletionItemsForLanguage(t, "less", ".foo { background-color: d|", nil, CompletionOptions{}, []completionExpectation{
		{Label: "darken"},
		{Label: "desaturate"},
	})
	assertCompletionItemsForLanguage(t, "less", ".btn-group { .btn:| }", nil, CompletionOptions{}, []completionExpectation{
		{Label: "::after", ResultText: ".btn-group { .btn::after }"},
	})
	assertCompletionItemsForLanguage(t, "less", ".foo { &:|", nil, CompletionOptions{}, []completionExpectation{
		{Label: ":last-of-type", ResultText: ".foo { &:last-of-type"},
	})
	assertCompletionItemsForLanguage(t, "less", ".foo { &:l|", nil, CompletionOptions{}, []completionExpectation{
		{Label: ":last-of-type", ResultText: ".foo { &:last-of-type"},
	})
	assertCompletionItemsForLanguage(t, "less", ".foo { appearance:| }", nil, CompletionOptions{}, []completionExpectation{
		{Label: "inherit", ResultText: ".foo { appearance:inherit }"},
	})
	assertCompletionItemsForLanguage(t, "less", ".foo { mask: no|", nil, CompletionOptions{}, []completionExpectation{
		{Label: "round"},
	})
}

func TestCSSCompletionCustomData(t *testing.T) {
	t.Run("Completion", func(t *testing.T) {
		manager := languagefacts.NewDataManager(languagefacts.DataManagerOptions{
			CustomDataProviders: []languagefacts.CSSDataProvider{languagefacts.NewCSSDataProvider(languagefacts.CSSDataV1{
				Version: 1.1,
				Properties: []languagefacts.PropertyData{{
					Name:        "foo",
					Description: []byte(`{"kind":"markdown","value":"Foo property. See link on [MDN](https://developer.mozilla.org/)."}`),
					References:  []languagefacts.Reference{{Name: "MDN Reference", URL: "https://developer.mozilla.org/docs/Web/CSS/foo"}},
				}},
				AtDirectives:   []languagefacts.AtDirectiveData{{Name: "@foo", Description: []byte(`"Foo at directive"`)}},
				PseudoClasses:  []languagefacts.PseudoClassData{{Name: ":foo", Description: []byte(`"Foo pseudo class"`)}},
				PseudoElements: []languagefacts.PseudoElementData{{Name: "::foo", Description: []byte(`"Foo pseudo element"`)}},
			})},
		})
		assertCompletionItems(t, "body { | }", manager, []completionExpectation{
			{Label: "foo", Kind: lsp.CompletionItemKindProperty, ResultText: "body { foo:  }"},
		})
		assertCompletionItemsWithOptions(t, "body { | }", manager, CompletionOptions{CompletePropertyWithSemicolon: true}, []completionExpectation{
			{
				Label:      "foo",
				Kind:       lsp.CompletionItemKindProperty,
				ResultText: "body { foo: $0; }",
				Documentation: lsp.MarkupContent{
					Kind:  lsp.MarkupKindMarkdown,
					Value: "Foo property. See link on [MDN](https://developer.mozilla.org/).\n\n[MDN Reference](https://developer.mozilla.org/docs/Web/CSS/foo)",
				},
			},
		})
		assertCompletionItems(t, "|", manager, []completionExpectation{
			{Label: "@foo", ResultText: "@foo"},
		})
		assertCompletionItems(t, ":|", manager, []completionExpectation{
			{Label: ":foo", ResultText: ":foo"},
		})
		assertCompletionItems(t, "::foo|", manager, []completionExpectation{
			{Label: "::foo", ResultText: "::foo"},
		})
	})
}

func TestCSSCompletionSortText(t *testing.T) {
	t.Run("Properties sorted by relevance", func(t *testing.T) {
		manager := languagefacts.NewDataManager(languagefacts.DataManagerOptions{
			UseDefaultDataProvider: boolPtr(false),
			CustomDataProviders: []languagefacts.CSSDataProvider{languagefacts.NewCSSDataProvider(languagefacts.CSSDataV1{
				Version: 1,
				Properties: []languagefacts.PropertyData{
					{Name: "foo", Relevance: floatPtr(93)},
					{Name: "bar", Relevance: floatPtr(1)},
					{Name: "-webkit-bar", Relevance: floatPtr(12)},
					{Name: "xoo"},
					{Name: "bar2", Relevance: floatPtr(0)},
				},
			})},
		})
		assertCompletionItems(t, ".foo { | }", manager, []completionExpectation{
			{Label: "foo", SortText: "d_a2"},
			{Label: "bar", SortText: "d_fe"},
			{Label: "-webkit-bar", SortText: "x_f3"},
			{Label: "xoo", SortText: "d_cd"},
			{Label: "bar2", SortText: "d_ff"},
		})
		_, list := completionListForDocument(t, "test://test/pathCompletionFixtures/about/about.css", "css", ".foo { | }", manager, CompletionOptions{})
		foo := completionItemsByLabel(list.Items, "foo")[0]
		bar := completionItemsByLabel(list.Items, "bar")[0]
		if !(foo.SortText < bar.SortText) {
			t.Fatalf("expected foo sortText %q to sort before bar %q", foo.SortText, bar.SortText)
		}
	})

	t.Run("Items that start with ", func(t *testing.T) {
		assertCompletionItems(t, ".foo { display: | }", nil, []completionExpectation{
			{Label: "grid", SortText: " "},
			{Label: "-moz-grid", SortText: " x"},
			{Label: "-ms-grid", SortText: " x"},
			{Label: "inherit"},
		})
	})

	t.Run("Enum + color restrictions are sorted properly", func(t *testing.T) {
		assertCompletionItemsForLanguage(t, "scss", ".foo { text-decoration: | }", nil, CompletionOptions{}, []completionExpectation{
			{Label: "dashed", SortText: " "},
			{Label: "aqua"},
			{Label: "inherit"},
		})
	})
}

func TestCSSCompletionMediaConditions(t *testing.T) {
	t.Run("@media at rule completion", func(t *testing.T) {
		assertCompletionItems(t, "@media (|) {", nil, []completionExpectation{
			{Label: "prefers-color-scheme", Kind: lsp.CompletionItemKindKeyword, ResultText: "@media (prefers-color-scheme: ) {"},
			{Label: "hover", Kind: lsp.CompletionItemKindKeyword, ResultText: "@media (hover: ) {"},
			{Label: "color", Kind: lsp.CompletionItemKindKeyword, ResultText: "@media (color) {"},
		})
		assertCompletionItems(t, "@media (prefers-color-scheme: |) {", nil, []completionExpectation{
			{Label: "light", Kind: lsp.CompletionItemKindValue, ResultText: "@media (prefers-color-scheme: light) {"},
			{Label: "dark", Kind: lsp.CompletionItemKindValue, ResultText: "@media (prefers-color-scheme: dark) {"},
		})
	})
}

func TestCSSCompletionPropertySettings(t *testing.T) {
	t.Run("Property completeness", func(t *testing.T) {
		assertCompletionItems(t, "html { text-decoration:|", nil, []completionExpectation{
			{Label: "none"},
		})
		assertCompletionItemsWithOptions(t, "body { disp| ", nil, CompletionOptions{TriggerPropertyValueCompletion: true}, []completionExpectation{
			{Label: "display", ResultText: "body { display:  ", Command: "editor.action.triggerSuggest"},
		})
		assertCompletionItemsWithOptions(t, "body { disp| ", nil, CompletionOptions{TriggerPropertyValueCompletion: true, CompletePropertyWithSemicolon: true}, []completionExpectation{
			{Label: "display", ResultText: "body { display: $0; ", Command: "editor.action.triggerSuggest"},
		})
		assertCompletionItemsWithOptions(t, "body { disp| ", nil, CompletionOptions{TriggerPropertyValueCompletion: true, CompletePropertyWithSemicolon: true}, []completionExpectation{
			{Label: "display", ResultText: "body { display: $0; ", Command: "editor.action.triggerSuggest"},
		})
		assertCompletionItemsWithOptions(t, "body { disp| ", nil, CompletionOptions{CompletePropertyWithSemicolon: true}, []completionExpectation{
			{Label: "display", ResultText: "body { display: $0; "},
		})
		assertCompletionItemsWithOptions(t, "body { disp| ", nil, CompletionOptions{TriggerPropertyValueCompletion: false, CompletePropertyWithSemicolon: false}, []completionExpectation{
			{Label: "display", ResultText: "body { display:  "},
		})
	})

	t.Run("Seimicolon on property completion", func(t *testing.T) {
		options := CompletionOptions{TriggerPropertyValueCompletion: true, CompletePropertyWithSemicolon: true}
		assertCompletionItemsWithOptions(t, ".foo { | }", nil, options, []completionExpectation{
			{Label: "position", ResultText: ".foo { position: $0; }", Command: "editor.action.triggerSuggest"},
		})
		assertCompletionItemsWithOptions(t, ".foo { p| }", nil, options, []completionExpectation{
			{Label: "position", ResultText: ".foo { position: $0; }", Command: "editor.action.triggerSuggest"},
		})
		assertCompletionItemsWithOptions(t, ".foo { p|o }", nil, options, []completionExpectation{
			{Label: "position", ResultText: ".foo { position:  }", Command: "editor.action.triggerSuggest"},
		})
		assertCompletionItemsWithOptions(t, ".foo { p|os: relative; }", nil, options, []completionExpectation{
			{Label: "position", ResultText: ".foo { position: relative; }"},
		})
		assertCompletionItemsWithOptions(t, ".foo { p|: ; }", nil, options, []completionExpectation{
			{Label: "position", ResultText: ".foo { position: ; }"},
		})
		assertCompletionItemsWithOptions(t, ".foo { p|; }", nil, options, []completionExpectation{
			{Label: "position", ResultText: ".foo { position: ; }"},
		})
	})
}

func TestCSSCompletionDescription(t *testing.T) {
	t.Run("Completion description should include status, browser compat and references", func(t *testing.T) {
		assertCompletionItems(t, ".foo { | }", nil, []completionExpectation{
			{
				Label: "text-decoration-skip",
				Documentation: lsp.MarkupContent{
					Kind: lsp.MarkupKindMarkdown,
					Value: "The text\\-decoration\\-skip CSS property specifies what parts of the element’s content any text decoration affecting the element must skip over\\. It controls all text decoration lines drawn by the element and also any text decoration lines drawn by its ancestors\\.\n\n" +
						"![Baseline icon](" + baselineLimitedImage + ") _Limited availability across major browsers (Not fully implemented in Chrome, Edge, or Firefox)_\n\n" +
						"Syntax: none | \\[ objects || \\[ spaces | \\[ leading\\-spaces || trailing\\-spaces \\] \\] || edges || box\\-decoration \\]\n\n" +
						"[MDN Reference](https://developer.mozilla.org/docs/Web/CSS/Reference/Properties/text-decoration-skip)",
				},
			},
			{
				Label: "box-ordinal-group",
				Documentation: lsp.MarkupContent{
					Kind: lsp.MarkupKindMarkdown,
					Value: "🚨️️️ Property is obsolete. Avoid using it.\n\n" +
						"The box\\-ordinal\\-group CSS property assigns the flexbox's child elements to an ordinal group\\.\n\n" +
						"Syntax: &lt;integer&gt;\n\n" +
						"[MDN Reference](https://developer.mozilla.org/docs/Web/CSS/Reference/Properties/box-ordinal-group)",
				},
			},
			{
				Label: "-webkit-mask-image",
				Documentation: lsp.MarkupContent{
					Kind: lsp.MarkupKindMarkdown,
					Value: "🚨️ Property is nonstandard. Avoid using it.\n\n" +
						"Sets the mask layer image of an element\\.\n\n" +
						"Syntax: &lt;mask\\-reference&gt;\\#",
				},
			},
		})
	})
}

func TestCSSCompletionSupportsConditions(t *testing.T) {
	t.Run("support", func(t *testing.T) {
		assertCompletionItems(t, "@supports (display: flex) { |", nil, []completionExpectation{
			{Label: "html", ResultText: "@supports (display: flex) { html"},
			{Label: "display", NotAvailable: true},
		})
		assertCompletionItems(t, "@supports (| ) { }", nil, []completionExpectation{
			{Label: "display", Kind: lsp.CompletionItemKindProperty, ResultText: "@supports (display:  ) { }"},
		})
		assertCompletionItems(t, "@supports (di| ) { }", nil, []completionExpectation{
			{Label: "display", Kind: lsp.CompletionItemKindProperty, ResultText: "@supports (display:  ) { }"},
		})
		assertCompletionItems(t, "@supports (display: | ) { }", nil, []completionExpectation{
			{Label: "flex", Kind: lsp.CompletionItemKindValue, ResultText: "@supports (display: flex ) { }"},
		})
		assertCompletionItems(t, "@supports (display: flex ) | { }", nil, []completionExpectation{
			{Label: "display", NotAvailable: true},
		})
		assertCompletionItems(t, "@supports |(display: flex ) { }", nil, []completionExpectation{
			{Label: "display", NotAvailable: true},
		})
	})
}

func TestCompletionParticipants(t *testing.T) {
	t.Run("suggestParticipants", func(t *testing.T) {
		t.Run("css property", func(t *testing.T) {
			recorder, _ := completionParticipantsForLanguage(t, "css", "html { bac|")
			assertParticipantContexts(t, recorder.properties, []PropertyCompletionContext{{
				PropertyName: "bac",
				Range:        singleLineRange(7, 10),
			}})
			assertParticipantContexts(t, recorder.propertyValues, nil)
		})

		t.Run("css property prefix inside word", func(t *testing.T) {
			recorder, _ := completionParticipantsForLanguage(t, "css", "html { disp|lay: none")
			assertParticipantContexts(t, recorder.properties, []PropertyCompletionContext{{
				PropertyName: "disp",
				Range:        singleLineRange(7, 11),
			}})
		})

		t.Run("css property value", func(t *testing.T) {
			recorder, _ := completionParticipantsForLanguage(t, "css", "html { background-position: t|")
			assertParticipantContexts(t, recorder.properties, nil)
			assertParticipantContexts(t, recorder.propertyValues, []PropertyValueCompletionContext{{
				PropertyName:  "background-position",
				PropertyValue: "t",
				Range:         singleLineRange(28, 29),
			}})
		})
		t.Run("css property value completion item", func(t *testing.T) {
			_, list := completionParticipantsForLanguage(t, "css", "html { background-position: t|")
			if matches := completionItemsByLabel(list.Items, "center"); len(matches) != 1 {
				t.Fatalf("center count = %d in labels=%v", len(matches), completionLabels(list.Items))
			}
		})

		urlTests := []struct {
			name     string
			input    string
			expected URILiteralCompletionContext
		}{
			{
				name:  "empty unquoted url",
				input: `html { background-image: url(|)`,
				expected: URILiteralCompletionContext{
					URIValue: "",
					Position: lsp.Position{Line: 0, Character: 29},
					Range:    singleLineRange(29, 29),
				},
			},
			{
				name:  "empty quoted url",
				input: `html { background-image: url('|')`,
				expected: URILiteralCompletionContext{
					URIValue: `''`,
					Position: lsp.Position{Line: 0, Character: 30},
					Range:    singleLineRange(29, 31),
				},
			},
			{
				name:  "quoted url with prefix",
				input: `html { background-image: url("b|")`,
				expected: URILiteralCompletionContext{
					URIValue: `"b"`,
					Position: lsp.Position{Line: 0, Character: 31},
					Range:    singleLineRange(29, 32),
				},
			},
			{
				name:  "unclosed quoted url with prefix",
				input: `html { background: url("b|"`,
				expected: URILiteralCompletionContext{
					URIValue: `"b"`,
					Position: lsp.Position{Line: 0, Character: 25},
					Range:    singleLineRange(23, 26),
				},
			},
		}
		for _, tt := range urlTests {
			t.Run(tt.name, func(t *testing.T) {
				recorder, list := completionParticipantsForLanguage(t, "css", tt.input)
				assertParticipantContexts(t, recorder.uriLiterals, []URILiteralCompletionContext{tt.expected})
				if len(list.Items) != 0 {
					t.Fatalf("completion count = %d, want 0", len(list.Items))
				}
			})
		}

		importTests := []struct {
			name     string
			language string
			input    string
			expected ImportPathCompletionContext
		}{
			{
				name:     "css single quoted import",
				language: "css",
				input:    `@import './|'`,
				expected: ImportPathCompletionContext{
					PathValue: `'./'`,
					Position:  lsp.Position{Line: 0, Character: 11},
					Range:     singleLineRange(8, 12),
				},
			},
			{
				name:     "css double quoted import",
				language: "css",
				input:    `@import "./|";`,
				expected: ImportPathCompletionContext{
					PathValue: `"./"`,
					Position:  lsp.Position{Line: 0, Character: 11},
					Range:     singleLineRange(8, 12),
				},
			},
			{
				name:     "css import with suffix",
				language: "css",
				input:    `@import "./|foo";`,
				expected: ImportPathCompletionContext{
					PathValue: `"./foo"`,
					Position:  lsp.Position{Line: 0, Character: 11},
					Range:     singleLineRange(8, 15),
				},
			},
			{
				name:     "scss use path",
				language: "scss",
				input:    `@use './|'`,
				expected: ImportPathCompletionContext{
					PathValue: `'./'`,
					Position:  lsp.Position{Line: 0, Character: 8},
					Range:     singleLineRange(5, 9),
				},
			},
			{
				name:     "scss forward path",
				language: "scss",
				input:    `@forward './|'`,
				expected: ImportPathCompletionContext{
					PathValue: `'./'`,
					Position:  lsp.Position{Line: 0, Character: 12},
					Range:     singleLineRange(9, 13),
				},
			},
		}
		for _, tt := range importTests {
			t.Run(tt.name, func(t *testing.T) {
				recorder, list := completionParticipantsForLanguage(t, tt.language, tt.input)
				assertParticipantContexts(t, recorder.importPaths, []ImportPathCompletionContext{tt.expected})
				if len(list.Items) != 0 {
					t.Fatalf("completion count = %d, want 0", len(list.Items))
				}
			})
		}

		mixinTests := []struct {
			name     string
			language string
			input    string
			expected MixinReferenceCompletionContext
		}{
			{
				name:     "less selector-like mixin",
				language: "less",
				input:    `html { .m| }`,
				expected: MixinReferenceCompletionContext{
					MixinName: ".m",
					Range:     singleLineRange(7, 9),
				},
			},
			{
				name:     "less mixin arguments",
				language: "less",
				input:    `html { .mixin(|) }`,
				expected: MixinReferenceCompletionContext{
					MixinName: "",
					Range:     singleLineRange(14, 14),
				},
			},
			{
				name:     "scss empty include",
				language: "scss",
				input:    `html { @include | }`,
				expected: MixinReferenceCompletionContext{
					MixinName: "",
					Range:     singleLineRange(16, 16),
				},
			},
			{
				name:     "scss include prefix",
				language: "scss",
				input:    `html { @include m| }`,
				expected: MixinReferenceCompletionContext{
					MixinName: "m",
					Range:     singleLineRange(16, 17),
				},
			},
			{
				name:     "scss include arguments",
				language: "scss",
				input:    `html { @include mixin(|) }`,
				expected: MixinReferenceCompletionContext{
					MixinName: "",
					Range:     singleLineRange(22, 22),
				},
			},
		}
		for _, tt := range mixinTests {
			t.Run(tt.name, func(t *testing.T) {
				recorder, _ := completionParticipantsForLanguage(t, tt.language, tt.input)
				assertParticipantContexts(t, recorder.mixinReferences, []MixinReferenceCompletionContext{tt.expected})
				assertParticipantContexts(t, recorder.properties, nil)
			})
		}
	})
}

func TestCSSCompletionColorSwatchForVariables(t *testing.T) {
	t.Run("Color swatch for variables that", func(t *testing.T) {
		assertCompletionItems(t, ".foo { --foo: #bbb; color: --| }", nil, []completionExpectation{
			{Label: "--foo", Documentation: "#bbb", Kind: lsp.CompletionItemKindColor},
		})
		assertCompletionItems(t, ".foo { --foo: #bbbbbb; color: --| }", nil, []completionExpectation{
			{Label: "--foo", Documentation: "#bbbbbb", Kind: lsp.CompletionItemKindColor},
		})
		assertCompletionItems(t, ".foo { --foo: red; color: --| }", nil, []completionExpectation{
			{Label: "--foo", Documentation: "red", Kind: lsp.CompletionItemKindColor},
		})
		assertCompletionItems(t, ".foo { --foo: RED; color: --| }", nil, []completionExpectation{
			{Label: "--foo", Documentation: "RED", Kind: lsp.CompletionItemKindColor},
		})
		assertCompletionItems(t, ".foo { --foo: #bbb; color: var(|) }", nil, []completionExpectation{
			{Label: "--foo", Documentation: "#bbb", Kind: lsp.CompletionItemKindColor},
		})
	})
}

func TestCSSCompletionScopeSelector(t *testing.T) {
	t.Run("@scope selector completion", func(t *testing.T) {
		assertCompletionItems(t, "@scope (|) {", nil, []completionExpectation{
			{Label: "html", ResultText: "@scope (html) {"},
			{Label: ":has", ResultText: "@scope (:has) {"},
		})
		assertCompletionItems(t, "@scope to (|) {", nil, []completionExpectation{
			{Label: "html", ResultText: "@scope to (html) {"},
			{Label: ":has", ResultText: "@scope to (:has) {"},
		})
	})
}

func TestCSSPathCompletionURLAndImport(t *testing.T) {
	options := CompletionOptions{
		ReadDirectory: fakeCompletionReadDirectory(map[string][]FileEntry{
			"test://test/": {
				{Name: "pathCompletionFixtures", Type: FileTypeDirectory},
			},
			"test://test/pathCompletionFixtures/about/": {
				{Name: "about.css", Type: FileTypeFile},
				{Name: "about.html", Type: FileTypeFile},
			},
			"test://test/pathCompletionFixtures/": {
				{Name: ".foo.js", Type: FileTypeFile},
				{Name: "about", Type: FileTypeDirectory},
				{Name: "index.html", Type: FileTypeFile},
				{Name: "scss", Type: FileTypeDirectory},
				{Name: "src", Type: FileTypeDirectory},
			},
			"test://test/pathCompletionFixtures/src/": {
				{Name: "data", Type: FileTypeDirectory},
				{Name: "feature.js", Type: FileTypeFile},
				{Name: "test.js", Type: FileTypeFile},
			},
			"test://test/pathCompletionFixtures/src/data/": {
				{Name: "foo.asar", Type: FileTypeFile},
			},
		}),
		ResolveReference: fakeCompletionResolveReference("test://test/"),
	}

	t.Run("CSS url() Path completion", func(t *testing.T) {
		assertCompletionItemsWithOptions(t, `html { background-image: url('|')`, nil, options, []completionExpectation{
			{Label: "about.html", Kind: lsp.CompletionItemKindFile, ResultText: `html { background-image: url('about.html')`},
		})
		assertCompletionItemsWithOptions(t, `html { background-image: url("./|")`, nil, options, []completionExpectation{
			{Label: "about.html", Kind: lsp.CompletionItemKindFile, ResultText: `html { background-image: url("./about.html")`},
		})
		assertCompletionItemsWithOptions(t, `html { background-image: url('../|')`, nil, options, []completionExpectation{
			{Label: "about/", Kind: lsp.CompletionItemKindFolder, ResultText: `html { background-image: url('../about/')`},
			{Label: "index.html", Kind: lsp.CompletionItemKindFile, ResultText: `html { background-image: url('../index.html')`},
			{Label: "src/", Kind: lsp.CompletionItemKindFolder, ResultText: `html { background-image: url('../src/')`},
		})
		assertCompletionItemsWithOptions(t, `html { background-image: url('../src/a|')`, nil, options, []completionExpectation{
			{Label: "feature.js", Kind: lsp.CompletionItemKindFile, ResultText: `html { background-image: url('../src/feature.js')`},
			{Label: "data/", Kind: lsp.CompletionItemKindFolder, ResultText: `html { background-image: url('../src/data/')`},
			{Label: "test.js", Kind: lsp.CompletionItemKindFile, ResultText: `html { background-image: url('../src/test.js')`},
		})
		assertCompletionItemsWithOptions(t, `html { background-image: url('../src/data/f|.asar')`, nil, options, []completionExpectation{
			{Label: "foo.asar", Kind: lsp.CompletionItemKindFile, ResultText: `html { background-image: url('../src/data/foo.asar')`},
		})
		assertCompletionItemsWithOptions(t, `html { background-image: url('/|')`, nil, options, []completionExpectation{
			{Label: "pathCompletionFixtures/", Kind: lsp.CompletionItemKindFolder, ResultText: `html { background-image: url('/pathCompletionFixtures/')`},
		})
		assertCompletionItemsWithOptions(t, `html { background-image: url("/|")`, nil, options, []completionExpectation{
			{Label: "pathCompletionFixtures/", Kind: lsp.CompletionItemKindFolder, ResultText: `html { background-image: url("/pathCompletionFixtures/")`},
		})
		assertCompletionItemsWithOptions(t, `html { background-image: url('/pathCompletionFixtures/|')`, nil, options, []completionExpectation{
			{Label: "about/", Kind: lsp.CompletionItemKindFolder, ResultText: `html { background-image: url('/pathCompletionFixtures/about/')`},
			{Label: "index.html", Kind: lsp.CompletionItemKindFile, ResultText: `html { background-image: url('/pathCompletionFixtures/index.html')`},
			{Label: "src/", Kind: lsp.CompletionItemKindFolder, ResultText: `html { background-image: url('/pathCompletionFixtures/src/')`},
		})
	})

	t.Run("CSS url() Path Completion - Unquoted url", func(t *testing.T) {
		assertCompletionItemsWithOptions(t, `html { background-image: url(./|)`, nil, options, []completionExpectation{
			{Label: "about.html", Kind: lsp.CompletionItemKindFile, ResultText: `html { background-image: url(./about.html)`},
		})
		assertCompletionItemsWithOptions(t, `html { background-image: url(./a|)`, nil, options, []completionExpectation{
			{Label: "about.html", Kind: lsp.CompletionItemKindFile, ResultText: `html { background-image: url(./about.html)`},
		})
		assertCompletionItemsWithOptions(t, `html { background-image: url(../|src/)`, nil, options, []completionExpectation{
			{Label: "about/", Kind: lsp.CompletionItemKindFolder, ResultText: `html { background-image: url(../about/)`},
		})
		assertCompletionItemsWithOptions(t, `html { background-image: url(../s|rc/)`, nil, options, []completionExpectation{
			{Label: "about/", Kind: lsp.CompletionItemKindFolder, ResultText: `html { background-image: url(../about/)`},
		})
	})

	t.Run("CSS @import Path completion", func(t *testing.T) {
		assertCompletionItemsWithOptions(t, `@import './|'`, nil, options, []completionExpectation{
			{Label: "about.html", Kind: lsp.CompletionItemKindFile, ResultText: `@import './about.html'`},
		})
		assertCompletionItemsWithOptions(t, `@import '../|'`, nil, options, []completionExpectation{
			{Label: "about/", Kind: lsp.CompletionItemKindFolder, ResultText: `@import '../about/'`},
			{Label: "scss/", Kind: lsp.CompletionItemKindFolder, ResultText: `@import '../scss/'`},
			{Label: "index.html", Kind: lsp.CompletionItemKindFile, ResultText: `@import '../index.html'`},
			{Label: "src/", Kind: lsp.CompletionItemKindFolder, ResultText: `@import '../src/'`},
		})
	})

	t.Run("Completion should ignore files/folders starting with dot", func(t *testing.T) {
		assertCompletionItemsWithOptions(t, `html { background-image: url('../|')`, nil, options, []completionExpectation{
			{Label: ".foo.js", NotAvailable: true},
		})
		assertCompletionCount(t, `html { background-image: url("../|")`, nil, options, 4)
	})
}

func TestSCSSPathCompletionImportPartials(t *testing.T) {
	options := CompletionOptions{
		ReadDirectory: fakeCompletionReadDirectory(map[string][]FileEntry{
			"test://test/pathCompletionFixtures/scss/": {
				{Name: "main.scss", Type: FileTypeFile},
				{Name: "_foo.scss", Type: FileTypeFile},
			},
		}),
		ResolveReference: fakeCompletionResolveReference("test://test/pathCompletionFixtures/"),
	}

	t.Run("SCSS @import Path completion", func(t *testing.T) {
		assertCompletionItemsForDocument(t, "test://test/pathCompletionFixtures/about/about.css", "css", `@import '../scss/|'`, nil, options, []completionExpectation{
			{Label: "main.scss", Kind: lsp.CompletionItemKindFile, ResultText: `@import '../scss/main.scss'`},
			{Label: "_foo.scss", Kind: lsp.CompletionItemKindFile, ResultText: `@import '../scss/_foo.scss'`},
		})
		assertCompletionItemsForDocument(t, "test://test/pathCompletionFixtures/scss/main.scss", "scss", `@import './|'`, nil, options, []completionExpectation{
			{Label: "_foo.scss", Kind: lsp.CompletionItemKindFile, ResultText: `@import './foo'`},
		})
		assertCompletionItemsForDocument(t, "test://test/pathCompletionFixtures/scss/main.scss", "scss", `@use './|'`, nil, options, []completionExpectation{
			{Label: "_foo.scss", Kind: lsp.CompletionItemKindFile, ResultText: `@use './foo'`},
		})
		assertCompletionItemsForDocument(t, "test://test/pathCompletionFixtures/scss/main.scss", "scss", `@forward './|'`, nil, options, []completionExpectation{
			{Label: "_foo.scss", Kind: lsp.CompletionItemKindFile, ResultText: `@forward './foo'`},
		})
	})
}

type completionExpectation struct {
	Label                 string
	Kind                  lsp.CompletionItemKind
	Detail                string
	Documentation         any
	DocumentationIncludes string
	InsertTextFormat      lsp.InsertTextFormat
	SortText              string
	Command               string
	ResultText            string
	NotAvailable          bool
}

type completionParticipantRecorder struct {
	properties      []PropertyCompletionContext
	propertyValues  []PropertyValueCompletionContext
	uriLiterals     []URILiteralCompletionContext
	importPaths     []ImportPathCompletionContext
	mixinReferences []MixinReferenceCompletionContext
}

func (r *completionParticipantRecorder) options() CompletionOptions {
	return CompletionOptions{Participants: []CompletionParticipant{{
		OnProperty: func(context PropertyCompletionContext) {
			r.properties = append(r.properties, context)
		},
		OnPropertyValue: func(context PropertyValueCompletionContext) {
			r.propertyValues = append(r.propertyValues, context)
		},
		OnURILiteralValue: func(context URILiteralCompletionContext) {
			r.uriLiterals = append(r.uriLiterals, context)
		},
		OnImportPath: func(context ImportPathCompletionContext) {
			r.importPaths = append(r.importPaths, context)
		},
		OnMixinReference: func(context MixinReferenceCompletionContext) {
			r.mixinReferences = append(r.mixinReferences, context)
		},
	}}}
}

func completionParticipantsForLanguage(t *testing.T, languageID string, markedInput string) (*completionParticipantRecorder, lsp.CompletionList) {
	t.Helper()
	recorder := &completionParticipantRecorder{}
	_, list := completionListForDocument(t, lsp.DocumentURI("test://test/pathCompletionFixtures/about/about."+languageID), languageID, markedInput, nil, recorder.options())
	return recorder, list
}

func assertParticipantContexts[T comparable](t *testing.T, got []T, want []T) {
	t.Helper()
	if !reflect.DeepEqual(got, want) {
		t.Fatalf("participant contexts = %#v, want %#v", got, want)
	}
}

func singleLineRange(start, end int) lsp.Range {
	return lsp.Range{
		Start: lsp.Position{Line: 0, Character: start},
		End:   lsp.Position{Line: 0, Character: end},
	}
}

func assertCompletionItems(t *testing.T, markedInput string, manager *languagefacts.DataManager, expected []completionExpectation) {
	t.Helper()
	assertCompletionItemsWithOptions(t, markedInput, manager, CompletionOptions{}, expected)
}

func assertCompletionItemsWithOptions(t *testing.T, markedInput string, manager *languagefacts.DataManager, options CompletionOptions, expected []completionExpectation) {
	t.Helper()
	assertCompletionItemsForLanguage(t, "css", markedInput, manager, options, expected)
}

func assertCompletionItemsForLanguage(t *testing.T, languageID string, markedInput string, manager *languagefacts.DataManager, options CompletionOptions, expected []completionExpectation) {
	t.Helper()
	assertCompletionItemsForDocument(t, lsp.DocumentURI("test://test/pathCompletionFixtures/about/about."+languageID), languageID, markedInput, manager, options, expected)
}

func assertCompletionItemsForDocument(t *testing.T, uri lsp.DocumentURI, languageID string, markedInput string, manager *languagefacts.DataManager, options CompletionOptions, expected []completionExpectation) {
	t.Helper()
	document, list := completionListForDocument(t, uri, languageID, markedInput, manager, options)
	for _, want := range expected {
		matches := completionItemsByLabel(list.Items, want.Label)
		if want.NotAvailable {
			if len(matches) != 0 {
				t.Fatalf("%s should not be present in labels=%v", want.Label, completionLabels(list.Items))
			}
			continue
		}
		if len(matches) != 1 {
			t.Fatalf("%s count = %d in labels=%v", want.Label, len(matches), completionLabels(list.Items))
		}
		got := matches[0]
		if want.Kind != 0 && got.Kind != want.Kind {
			t.Fatalf("%s kind = %#v, want %#v", want.Label, got.Kind, want.Kind)
		}
		if want.Detail != "" && got.Detail != want.Detail {
			t.Fatalf("%s detail = %q, want %q", want.Label, got.Detail, want.Detail)
		}
		if want.InsertTextFormat != 0 && got.InsertTextFormat != want.InsertTextFormat {
			t.Fatalf("%s insertTextFormat = %#v, want %#v", want.Label, got.InsertTextFormat, want.InsertTextFormat)
		}
		if want.SortText != "" && got.SortText != want.SortText {
			t.Fatalf("%s sortText = %q, want %q", want.Label, got.SortText, want.SortText)
		}
		if want.Command != "" {
			if got.Command == nil || got.Command.Command != want.Command {
				t.Fatalf("%s command = %#v, want %q", want.Label, got.Command, want.Command)
			}
		}
		if want.Documentation != nil && !reflect.DeepEqual(got.Documentation, want.Documentation) {
			t.Fatalf("%s documentation = %#v, want %#v", want.Label, got.Documentation, want.Documentation)
		}
		if want.DocumentationIncludes != "" && !completionDocumentationContains(got.Documentation, want.DocumentationIncludes) {
			t.Fatalf("%s documentation = %#v, want substring %q", want.Label, got.Documentation, want.DocumentationIncludes)
		}
		if want.ResultText != "" {
			if got.TextEdit == nil {
				t.Fatalf("%s has no text edit", want.Label)
			}
			if actual := lsp.ApplyEdits(document, []lsp.TextEdit{*got.TextEdit}); actual != want.ResultText {
				t.Fatalf("%s result = %q, want %q", want.Label, actual, want.ResultText)
			}
		}
	}
}

func completionDocumentationContains(documentation any, want string) bool {
	switch value := documentation.(type) {
	case string:
		return strings.Contains(value, want)
	case lsp.MarkupContent:
		return strings.Contains(value.Value, want)
	case *lsp.MarkupContent:
		return value != nil && strings.Contains(value.Value, want)
	default:
		return false
	}
}

func assertCompletionCount(t *testing.T, markedInput string, manager *languagefacts.DataManager, options CompletionOptions, expected int) {
	t.Helper()
	assertCompletionCountForLanguage(t, "css", markedInput, manager, options, expected)
}

func assertCompletionCountForLanguage(t *testing.T, languageID string, markedInput string, manager *languagefacts.DataManager, options CompletionOptions, expected int) {
	t.Helper()
	_, list := completionListForDocument(t, lsp.DocumentURI("test://test/pathCompletionFixtures/about/about."+languageID), languageID, markedInput, manager, options)
	if len(list.Items) != expected {
		t.Fatalf("%s\ncompletion count = %d, want %d: %v", markedInput, len(list.Items), expected, completionLabels(list.Items))
	}
}

func completionListForDocument(t *testing.T, uri lsp.DocumentURI, languageID string, markedInput string, manager *languagefacts.DataManager, options CompletionOptions) (*lsp.TextDocument, lsp.CompletionList) {
	t.Helper()
	offset := stringsIndex(markedInput, "|")
	input := markedInput[:offset] + markedInput[offset+1:]
	document := lsp.NewTextDocument(uri, languageID, 0, input)
	if manager == nil {
		manager = languagefacts.NewDataManager(languagefacts.DataManagerOptions{})
	}
	list, err := Complete(context.Background(), document, document.PositionAt(offset), manager, options)
	if err != nil {
		t.Fatal(err)
	}
	return document, list
}

func completionItemsByLabel(items []lsp.CompletionItem, label string) []lsp.CompletionItem {
	var result []lsp.CompletionItem
	for _, item := range items {
		if item.Label == label {
			result = append(result, item)
		}
	}
	return result
}

func completionLabels(items []lsp.CompletionItem) []string {
	labels := make([]string, 0, len(items))
	for _, item := range items {
		labels = append(labels, item.Label)
	}
	return labels
}

func fakeCompletionReadDirectory(entries map[string][]FileEntry) ReadDirectoryFunc {
	return func(ctx context.Context, uri string) ([]FileEntry, error) {
		return entries[uri], nil
	}
}

func fakeCompletionResolveReference(workspaceFolder string) ResolveReferenceFunc {
	return func(ref, baseURL string) (string, bool) {
		if strings.HasPrefix(ref, "/") {
			return joinCompletionURI(workspaceFolder, ref[1:]), true
		}
		if ref == "." || ref == "" {
			return parentCompletionURI(baseURL), true
		}
		if strings.HasPrefix(ref, "./") {
			return joinCompletionURI(parentCompletionURI(baseURL), ref[2:]), true
		}
		if strings.HasPrefix(ref, "../") {
			parent := parentCompletionURI(parentCompletionURI(baseURL))
			return joinCompletionURI(parent, ref[3:]), true
		}
		return joinCompletionURI(parentCompletionURI(baseURL), ref), true
	}
}

func parentCompletionURI(uri string) string {
	trimmed := strings.TrimRight(uri, "/")
	slash := strings.LastIndex(trimmed, "/")
	if slash == -1 {
		return uri
	}
	return trimmed[:slash+1]
}

func joinCompletionURI(base, ref string) string {
	if ref == "" {
		return base
	}
	if !strings.HasSuffix(base, "/") {
		base += "/"
	}
	return base + ref
}

func boolPtr(value bool) *bool {
	return &value
}

func floatPtr(value float64) *float64 {
	return &value
}
