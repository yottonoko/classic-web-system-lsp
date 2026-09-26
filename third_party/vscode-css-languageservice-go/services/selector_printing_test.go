package services

import (
	"reflect"
	"strings"
	"testing"
)

func TestSelectorElementsCSSPortedNamedCases(t *testing.T) {
	t.Run("class/hash/elementname/attr", func(t *testing.T) {
		assertElementAttributes(t, "element", []ElementAttribute{elementAttr("name", "element")})
		assertElementAttributes(t, ".div", []ElementAttribute{elementAttr("class", "div")})
		assertElementAttributes(t, "#first", []ElementAttribute{elementAttr("id", "first")})
		assertElementAttributes(t, "element.on", []ElementAttribute{elementAttr("name", "element"), elementAttr("class", "on")})
		assertElementAttributes(t, "element.on#first", []ElementAttribute{elementAttr("name", "element"), elementAttr("class", "on"), elementAttr("id", "first")})
		assertElementAttributes(t, ".on#first", []ElementAttribute{elementAttr("class", "on"), elementAttr("id", "first")})
		assertElementAttributes(t, "[lang='de']", []ElementAttribute{elementAttr("lang", "de")})
		assertElementAttributes(t, "[enabled]", []ElementAttribute{elementAttrUndefined("enabled")})
	})
	t.Run("simple selector", func(t *testing.T) {
		assertSelectorElement(t, "css", "element { }", "element", "{element}")
		assertSelectorElement(t, "css", "element.div { }", "element", "{element[class=div]}")
		assertSelectorElement(t, "css", "element.on#first { }", "element", "{element[class=on|id=first]}")
		assertSelectorElement(t, "css", "element:hover { }", "element", "{element[:hover=]}")
		assertSelectorElement(t, "css", "element[lang='de'] { }", "element", "{element[lang=de]}")
		assertSelectorElement(t, "css", "element[enabled] { }", "element", "{element[enabled=undefined]}")
		assertSelectorElement(t, "css", "element[foo~=\"warning\"] { }", "element", "{element[foo= … warning … ]}")
		assertSelectorElement(t, "css", "element[lang|=\"en\"] { }", "element", "{element[lang=en-…]}")
		assertSelectorElement(t, "css", "* { }", "*", "{element}")
	})
	t.Run("selector", func(t *testing.T) {
		assertSelectorElement(t, "css", "e1 e2 { }", "e1", "{e1{…{e2}}}")
		assertSelectorElement(t, "css", "e1 .div { }", "e1", "{e1{…{[class=div]}}}")
		assertSelectorElement(t, "css", "e1 > e2 { }", "e2", "{e1{e2}}")
		assertSelectorElement(t, "css", "e1, e2 { }", "e1", "{e1}")
		assertSelectorElement(t, "css", "e1, e2 { }", "e2", "{e2}")
		assertSelectorElement(t, "css", "e1 + e2 { }", "e2", "{e1|e2}")
		assertSelectorElement(t, "css", "e1 ~ e2 { }", "e2", "{e1|⋮|e2}")
	})
	t.Run("escaping", func(t *testing.T) {
		assertSelectorElement(t, "css", "#\\34 04-error { }", "#\\34 04-error", "{[id=404-error]}")
	})
}

func TestSelectorElementsSCSSPortedNamedCases(t *testing.T) {
	t.Run("simple selector", func(t *testing.T) {
		assertSelectorElement(t, "scss", "o1 { }", "o1", "{o1}")
		assertSelectorElement(t, "scss", ".div { } ", ".div", "{[class=div]}")
		assertSelectorElement(t, "scss", "#div { } ", "#div", "{[id=div]}")
		assertSelectorElement(t, "scss", "o1.div { } ", "o1", "{o1[class=div]}")
		assertSelectorElement(t, "scss", "o1#div { }", "o1", "{o1[id=div]}")
		assertSelectorElement(t, "scss", "#div.o1 { }", "o1", "{[id=div|class=o1]}")
		assertSelectorElement(t, "scss", ".o1#div { }", "o1", "{[class=o1|id=div]}")
	})
	t.Run("nested selector", func(t *testing.T) {
		assertSelectorElement(t, "scss", "o1 { e1 { } }", "e1", "{o1{…{e1}}}")
		assertSelectorElement(t, "scss", "o1 { e1.div { } }", "e1", "{o1{…{e1[class=div]}}}")
		assertSelectorElement(t, "scss", "o1 o2 { e1 { } }", "e1", "{o1{…{o2{…{e1}}}}}")
		assertSelectorElement(t, "scss", "o1, o2 { e1 { } }", "e1", "{o1{…{e1}}}")
		assertSelectorElement(t, "scss", "o1 { @if $a { e1 { } } }", "e1", "{o1{…{e1}}}")
		assertSelectorElement(t, "scss", "o1 { @mixin a { e1 { } } }", "e1", "{e1}")
		assertSelectorElement(t, "scss", "o1 { @mixin a { e1 { } } }", "e1", "{e1}")
	})
	t.Run("referencing selector", func(t *testing.T) {
		assertSelectorElement(t, "scss", "o1 { &:hover { }}", "&", "{o1[:hover=]}")
		assertSelectorElement(t, "scss", "o1 { &:hover & { }}", "&", "{o1[:hover=]{…{o1}}}")
		assertSelectorElement(t, "scss", "o1 { &__bar {}}", "&", "{o1__bar}")
		assertSelectorElement(t, "scss", ".c1 { &__bar {}}", "&", "{[class=c1__bar]}")
		assertSelectorElement(t, "scss", "o.c1 { &__bar {}}", "&", "{o[class=c1__bar]}")
	})
	t.Run("placeholders", func(t *testing.T) {
		assertSelectorElement(t, "scss", "%o1 { e1 { } }", "e1", "{%o1{…{e1}}}")
	})
}

func TestSelectorMarkdownCombinators(t *testing.T) {
	t.Run("descendant selector", func(t *testing.T) {
		assertSelectorMarkedStringCases(t, []selectorMarkedStringCase{
			{
				selector: "e1 e2",
				expected: []MarkedString{
					{Language: "html", Value: "<e1>\n  …\n    <e2>"},
					{Value: "[Selector Specificity](https://developer.mozilla.org/docs/Web/CSS/Specificity): (0, 0, 2)"},
				},
			},
			{
				selector: "e1 .div",
				expected: []MarkedString{
					{Language: "html", Value: "<e1>\n  …\n    <element class=\"div\">"},
					{Value: "[Selector Specificity](https://developer.mozilla.org/docs/Web/CSS/Specificity): (0, 1, 1)"},
				},
			},
		})
	})
	t.Run("child selector", func(t *testing.T) {
		assertSelectorMarkedStringCases(t, []selectorMarkedStringCase{
			{
				selector: "e1 > e2",
				expected: []MarkedString{
					{Language: "html", Value: "<e1>\n  <e2>"},
					{Value: "[Selector Specificity](https://developer.mozilla.org/docs/Web/CSS/Specificity): (0, 0, 2)"},
				},
			},
		})
	})
	t.Run("group selector", func(t *testing.T) {
		assertSelectorMarkedStringCases(t, []selectorMarkedStringCase{
			{
				selector: "e1, e2",
				expected: []MarkedString{
					{Language: "html", Value: "<e1>"},
					{Value: "[Selector Specificity](https://developer.mozilla.org/docs/Web/CSS/Specificity): (0, 0, 1)"},
				},
			},
		})
		assertSelectorMarkedStringsAt(t, "e1, e2", strings.Index("e1, e2", "e2"), []MarkedString{
			{Language: "html", Value: "<e2>"},
			{Value: "[Selector Specificity](https://developer.mozilla.org/docs/Web/CSS/Specificity): (0, 0, 1)"},
		})
	})
	t.Run("sibling selector", func(t *testing.T) {
		assertSelectorMarkedStringCases(t, []selectorMarkedStringCase{
			{
				selector: "e1 + e2",
				expected: []MarkedString{
					{Language: "html", Value: "<e1>\n<e2>"},
					{Value: "[Selector Specificity](https://developer.mozilla.org/docs/Web/CSS/Specificity): (0, 0, 2)"},
				},
			},
			{
				selector: "e1 ~ e2",
				expected: []MarkedString{
					{Language: "html", Value: "<e1>\n⋮\n<e2>"},
					{Value: "[Selector Specificity](https://developer.mozilla.org/docs/Web/CSS/Specificity): (0, 0, 2)"},
				},
			},
		})
	})
}

func TestSelectorMarkdownSpecificity(t *testing.T) {
	t.Run("attribute selector", func(t *testing.T) {
		assertSelectorMarkedStringCases(t, []selectorMarkedStringCase{{selector: "h1 + *[rel=up]", expected: []MarkedString{{Language: "html", Value: "<h1>\n<element rel=\"up\">"}, {Value: "[Selector Specificity](https://developer.mozilla.org/docs/Web/CSS/Specificity): (0, 1, 1)"}}}})
	})
	t.Run("class selector", func(t *testing.T) {
		assertSelectorMarkedStringCases(t, []selectorMarkedStringCase{
			{selector: "ul ol li.red", expected: []MarkedString{{Language: "html", Value: "<ul>\n  …\n    <ol>\n      …\n        <li class=\"red\">"}, {Value: "[Selector Specificity](https://developer.mozilla.org/docs/Web/CSS/Specificity): (0, 1, 3)"}}},
			{selector: "li.red.level", expected: []MarkedString{{Language: "html", Value: "<li class=\"red level\">"}, {Value: "[Selector Specificity](https://developer.mozilla.org/docs/Web/CSS/Specificity): (0, 2, 1)"}}},
		})
	})
	t.Run("pseudo class selector", func(t *testing.T) {
		assertSelectorMarkedStringCases(t, []selectorMarkedStringCase{{selector: "p:focus", expected: []MarkedString{{Language: "html", Value: "<p :focus>"}, {Value: "[Selector Specificity](https://developer.mozilla.org/docs/Web/CSS/Specificity): (0, 1, 1)"}}}})
	})
	t.Run("element selector", func(t *testing.T) {
		assertSelectorMarkedStringCases(t, []selectorMarkedStringCase{
			{selector: "li", expected: []MarkedString{{Language: "html", Value: "<li>"}, {Value: "[Selector Specificity](https://developer.mozilla.org/docs/Web/CSS/Specificity): (0, 0, 1)"}}},
			{selector: "ul li", expected: []MarkedString{{Language: "html", Value: "<ul>\n  …\n    <li>"}, {Value: "[Selector Specificity](https://developer.mozilla.org/docs/Web/CSS/Specificity): (0, 0, 2)"}}},
			{selector: "ul ol+li", expected: []MarkedString{{Language: "html", Value: "<ul>\n  …\n    <ol>\n    <li>"}, {Value: "[Selector Specificity](https://developer.mozilla.org/docs/Web/CSS/Specificity): (0, 0, 3)"}}},
		})
	})
	t.Run("pseudo element selector", func(t *testing.T) {
		assertSelectorMarkedStringCases(t, []selectorMarkedStringCase{
			{selector: "p::after", expected: []MarkedString{{Language: "html", Value: "<p ::after>"}, {Value: "[Selector Specificity](https://developer.mozilla.org/docs/Web/CSS/Specificity): (0, 0, 2)"}}},
			{selector: "p:after", expected: []MarkedString{{Language: "html", Value: "<p :after>"}, {Value: "[Selector Specificity](https://developer.mozilla.org/docs/Web/CSS/Specificity): (0, 0, 2)"}}},
		})
	})
	t.Run("identifier selector", func(t *testing.T) {
		assertSelectorMarkedStringCases(t, []selectorMarkedStringCase{{selector: "#x34y", expected: []MarkedString{{Language: "html", Value: "<element id=\"x34y\">"}, {Value: "[Selector Specificity](https://developer.mozilla.org/docs/Web/CSS/Specificity): (1, 0, 0)"}}}})
	})
	t.Run("ignore universal and not selector", func(t *testing.T) {
		assertSelectorMarkedStringCases(t, []selectorMarkedStringCase{
			{selector: "*", expected: []MarkedString{{Language: "html", Value: "<element>"}, {Value: "[Selector Specificity](https://developer.mozilla.org/docs/Web/CSS/Specificity): (0, 0, 0)"}}},
			{selector: "#s12:not(foo)", expected: []MarkedString{{Language: "html", Value: "<element id=\"s12\" :not>"}, {Value: "[Selector Specificity](https://developer.mozilla.org/docs/Web/CSS/Specificity): (1, 0, 1)"}}},
		})
	})
	t.Run("where specificity", func(t *testing.T) {
		assertSelectorMarkedStringCases(t, []selectorMarkedStringCase{
			{selector: "#s12:where(foo)", expected: []MarkedString{{Language: "html", Value: "<element id=\"s12\" :where>"}, {Value: "[Selector Specificity](https://developer.mozilla.org/docs/Web/CSS/Specificity): (1, 0, 0)"}}},
			{selector: "#s12:where(foo > foo, .bar > baz)", expected: []MarkedString{{Language: "html", Value: "<element id=\"s12\" :where>"}, {Value: "[Selector Specificity](https://developer.mozilla.org/docs/Web/CSS/Specificity): (1, 0, 0)"}}},
		})
	})
	t.Run("has, not, is specificity", func(t *testing.T) {
		assertSelectorMarkedStringCases(t, []selectorMarkedStringCase{
			{selector: "#s12:not(foo > foo, .bar > baz)", expected: []MarkedString{{Language: "html", Value: "<element id=\"s12\" :not>"}, {Value: "[Selector Specificity](https://developer.mozilla.org/docs/Web/CSS/Specificity): (1, 1, 1)"}}},
			{selector: "#s12:has(foo > foo, .bar > baz)", expected: []MarkedString{{Language: "html", Value: "<element id=\"s12\" :has>"}, {Value: "[Selector Specificity](https://developer.mozilla.org/docs/Web/CSS/Specificity): (1, 1, 1)"}}},
			{selector: "#s12:is(foo > foo, .bar > baz)", expected: []MarkedString{{Language: "html", Value: "<element id=\"s12\" :is>"}, {Value: "[Selector Specificity](https://developer.mozilla.org/docs/Web/CSS/Specificity): (1, 1, 1)"}}},
		})
	})
	t.Run("nth-child, nth-last-child specificity", func(t *testing.T) {
		assertSelectorMarkedStringCases(t, []selectorMarkedStringCase{
			{selector: "#foo:nth-child(2)", expected: []MarkedString{{Language: "html", Value: "<element id=\"foo\" :nth-child>"}, {Value: "[Selector Specificity](https://developer.mozilla.org/docs/Web/CSS/Specificity): (1, 1, 0)"}}},
			{selector: "#foo:nth-last-child(-n+3 of li, .important)", expected: []MarkedString{{Language: "html", Value: "<element id=\"foo\" :nth-last-child>"}, {Value: "[Selector Specificity](https://developer.mozilla.org/docs/Web/CSS/Specificity): (1, 2, 0)"}}},
		})
	})
	t.Run("host, host-context specificity", func(t *testing.T) {
		assertSelectorMarkedStringCases(t, []selectorMarkedStringCase{
			{selector: "#foo:host(.foo)", expected: []MarkedString{{Language: "html", Value: "<element id=\"foo\" :host>"}, {Value: "[Selector Specificity](https://developer.mozilla.org/docs/Web/CSS/Specificity): (1, 2, 0)"}}},
			{selector: "#foo:host-context(foo)", expected: []MarkedString{{Language: "html", Value: "<element id=\"foo\" :host-context>"}, {Value: "[Selector Specificity](https://developer.mozilla.org/docs/Web/CSS/Specificity): (1, 1, 1)"}}},
		})
	})
	t.Run("slotted specificity", func(t *testing.T) {
		assertSelectorMarkedStringCases(t, []selectorMarkedStringCase{{selector: "#foo::slotted(foo)", expected: []MarkedString{{Language: "html", Value: "<element id=\"foo\" ::slotted>"}, {Value: "[Selector Specificity](https://developer.mozilla.org/docs/Web/CSS/Specificity): (1, 0, 2)"}}}})
	})
	assertSelectorMarkedStringCases(t, []selectorMarkedStringCase{
		{selector: "h1 + *[rel=up]", expected: []MarkedString{{Language: "html", Value: "<h1>\n<element rel=\"up\">"}, {Value: "[Selector Specificity](https://developer.mozilla.org/docs/Web/CSS/Specificity): (0, 1, 1)"}}},
		{selector: "ul ol li.red", expected: []MarkedString{{Language: "html", Value: "<ul>\n  …\n    <ol>\n      …\n        <li class=\"red\">"}, {Value: "[Selector Specificity](https://developer.mozilla.org/docs/Web/CSS/Specificity): (0, 1, 3)"}}},
		{selector: "li.red.level", expected: []MarkedString{{Language: "html", Value: "<li class=\"red level\">"}, {Value: "[Selector Specificity](https://developer.mozilla.org/docs/Web/CSS/Specificity): (0, 2, 1)"}}},
		{selector: "p:focus", expected: []MarkedString{{Language: "html", Value: "<p :focus>"}, {Value: "[Selector Specificity](https://developer.mozilla.org/docs/Web/CSS/Specificity): (0, 1, 1)"}}},
		{selector: "li", expected: []MarkedString{{Language: "html", Value: "<li>"}, {Value: "[Selector Specificity](https://developer.mozilla.org/docs/Web/CSS/Specificity): (0, 0, 1)"}}},
		{selector: "ul li", expected: []MarkedString{{Language: "html", Value: "<ul>\n  …\n    <li>"}, {Value: "[Selector Specificity](https://developer.mozilla.org/docs/Web/CSS/Specificity): (0, 0, 2)"}}},
		{selector: "ul ol+li", expected: []MarkedString{{Language: "html", Value: "<ul>\n  …\n    <ol>\n    <li>"}, {Value: "[Selector Specificity](https://developer.mozilla.org/docs/Web/CSS/Specificity): (0, 0, 3)"}}},
		{selector: "p::after", expected: []MarkedString{{Language: "html", Value: "<p ::after>"}, {Value: "[Selector Specificity](https://developer.mozilla.org/docs/Web/CSS/Specificity): (0, 0, 2)"}}},
		{selector: "p:after", expected: []MarkedString{{Language: "html", Value: "<p :after>"}, {Value: "[Selector Specificity](https://developer.mozilla.org/docs/Web/CSS/Specificity): (0, 0, 2)"}}},
		{selector: "#x34y", expected: []MarkedString{{Language: "html", Value: "<element id=\"x34y\">"}, {Value: "[Selector Specificity](https://developer.mozilla.org/docs/Web/CSS/Specificity): (1, 0, 0)"}}},
		{selector: "*", expected: []MarkedString{{Language: "html", Value: "<element>"}, {Value: "[Selector Specificity](https://developer.mozilla.org/docs/Web/CSS/Specificity): (0, 0, 0)"}}},
		{selector: "#s12:not(foo)", expected: []MarkedString{{Language: "html", Value: "<element id=\"s12\" :not>"}, {Value: "[Selector Specificity](https://developer.mozilla.org/docs/Web/CSS/Specificity): (1, 0, 1)"}}},
		{selector: "#s12:not(foo > foo)", expected: []MarkedString{{Language: "html", Value: "<element id=\"s12\" :not>"}, {Value: "[Selector Specificity](https://developer.mozilla.org/docs/Web/CSS/Specificity): (1, 0, 2)"}}},
		{selector: "#s12:not(foo > foo, .bar > baz)", expected: []MarkedString{{Language: "html", Value: "<element id=\"s12\" :not>"}, {Value: "[Selector Specificity](https://developer.mozilla.org/docs/Web/CSS/Specificity): (1, 1, 1)"}}},
		{selector: "#s12:where(foo)", expected: []MarkedString{{Language: "html", Value: "<element id=\"s12\" :where>"}, {Value: "[Selector Specificity](https://developer.mozilla.org/docs/Web/CSS/Specificity): (1, 0, 0)"}}},
		{selector: "#s12:where(foo > foo, .bar > baz)", expected: []MarkedString{{Language: "html", Value: "<element id=\"s12\" :where>"}, {Value: "[Selector Specificity](https://developer.mozilla.org/docs/Web/CSS/Specificity): (1, 0, 0)"}}},
		{selector: "#s12:has(foo)", expected: []MarkedString{{Language: "html", Value: "<element id=\"s12\" :has>"}, {Value: "[Selector Specificity](https://developer.mozilla.org/docs/Web/CSS/Specificity): (1, 0, 1)"}}},
		{selector: "#s12:has(foo > foo)", expected: []MarkedString{{Language: "html", Value: "<element id=\"s12\" :has>"}, {Value: "[Selector Specificity](https://developer.mozilla.org/docs/Web/CSS/Specificity): (1, 0, 2)"}}},
		{selector: "#s12:has(foo > foo, .bar > baz)", expected: []MarkedString{{Language: "html", Value: "<element id=\"s12\" :has>"}, {Value: "[Selector Specificity](https://developer.mozilla.org/docs/Web/CSS/Specificity): (1, 1, 1)"}}},
		{selector: "#s12:is(foo)", expected: []MarkedString{{Language: "html", Value: "<element id=\"s12\" :is>"}, {Value: "[Selector Specificity](https://developer.mozilla.org/docs/Web/CSS/Specificity): (1, 0, 1)"}}},
		{selector: "#s12:is(foo > foo)", expected: []MarkedString{{Language: "html", Value: "<element id=\"s12\" :is>"}, {Value: "[Selector Specificity](https://developer.mozilla.org/docs/Web/CSS/Specificity): (1, 0, 2)"}}},
		{selector: "#s12:is(foo > foo, .bar > baz)", expected: []MarkedString{{Language: "html", Value: "<element id=\"s12\" :is>"}, {Value: "[Selector Specificity](https://developer.mozilla.org/docs/Web/CSS/Specificity): (1, 1, 1)"}}},
		{selector: "#s12:lang(en, fr)", expected: []MarkedString{{Language: "html", Value: "<element id=\"s12\" :lang>"}, {Value: "[Selector Specificity](https://developer.mozilla.org/docs/Web/CSS/Specificity): (1, 1, 0)"}}},
		{selector: "#s12:is(foo > foo, :not(.bar > baz, :has(.bar > .baz)))", expected: []MarkedString{{Language: "html", Value: "<element id=\"s12\" :is>"}, {Value: "[Selector Specificity](https://developer.mozilla.org/docs/Web/CSS/Specificity): (1, 2, 0)"}}},
		{selector: "#foo:nth-child(2)", expected: []MarkedString{{Language: "html", Value: "<element id=\"foo\" :nth-child>"}, {Value: "[Selector Specificity](https://developer.mozilla.org/docs/Web/CSS/Specificity): (1, 1, 0)"}}},
		{selector: "#foo:nth-child(even)", expected: []MarkedString{{Language: "html", Value: "<element id=\"foo\" :nth-child>"}, {Value: "[Selector Specificity](https://developer.mozilla.org/docs/Web/CSS/Specificity): (1, 1, 0)"}}},
		{selector: "#foo:nth-child(-n + 2)", expected: []MarkedString{{Language: "html", Value: "<element id=\"foo\" :nth-child>"}, {Value: "[Selector Specificity](https://developer.mozilla.org/docs/Web/CSS/Specificity): (1, 1, 0)"}}},
		{selector: "#foo:nth-child(n of.li)", expected: []MarkedString{{Language: "html", Value: "<element id=\"foo\" :nth-child>"}, {Value: "[Selector Specificity](https://developer.mozilla.org/docs/Web/CSS/Specificity): (1, 2, 0)"}}},
		{selector: "#foo:nth-child(n of.li,.li.li)", expected: []MarkedString{{Language: "html", Value: "<element id=\"foo\" :nth-child>"}, {Value: "[Selector Specificity](https://developer.mozilla.org/docs/Web/CSS/Specificity): (1, 3, 0)"}}},
		{selector: "#foo:nth-child(n of.li, .li.li)", expected: []MarkedString{{Language: "html", Value: "<element id=\"foo\" :nth-child>"}, {Value: "[Selector Specificity](https://developer.mozilla.org/docs/Web/CSS/Specificity): (1, 3, 0)"}}},
		{selector: "#foo:nth-child(n of li)", expected: []MarkedString{{Language: "html", Value: "<element id=\"foo\" :nth-child>"}, {Value: "[Selector Specificity](https://developer.mozilla.org/docs/Web/CSS/Specificity): (1, 1, 1)"}}},
		{selector: "#foo:nth-child(-n+3 of li.important)", expected: []MarkedString{{Language: "html", Value: "<element id=\"foo\" :nth-child>"}, {Value: "[Selector Specificity](https://developer.mozilla.org/docs/Web/CSS/Specificity): (1, 2, 1)"}}},
		{selector: "#foo:nth-child(-n+3 of li.important, .class1.class2.class3)", expected: []MarkedString{{Language: "html", Value: "<element id=\"foo\" :nth-child>"}, {Value: "[Selector Specificity](https://developer.mozilla.org/docs/Web/CSS/Specificity): (1, 4, 0)"}}},
		{selector: "#foo:nth-last-child(-n+3 of li, .important)", expected: []MarkedString{{Language: "html", Value: "<element id=\"foo\" :nth-last-child>"}, {Value: "[Selector Specificity](https://developer.mozilla.org/docs/Web/CSS/Specificity): (1, 2, 0)"}}},
		{selector: "#foo:host(.foo)", expected: []MarkedString{{Language: "html", Value: "<element id=\"foo\" :host>"}, {Value: "[Selector Specificity](https://developer.mozilla.org/docs/Web/CSS/Specificity): (1, 2, 0)"}}},
		{selector: "#foo:host-context(foo)", expected: []MarkedString{{Language: "html", Value: "<element id=\"foo\" :host-context>"}, {Value: "[Selector Specificity](https://developer.mozilla.org/docs/Web/CSS/Specificity): (1, 1, 1)"}}},
		{selector: "#foo::slotted(foo)", expected: []MarkedString{{Language: "html", Value: "<element id=\"foo\" ::slotted>"}, {Value: "[Selector Specificity](https://developer.mozilla.org/docs/Web/CSS/Specificity): (1, 0, 2)"}}},
	})
}

type selectorMarkedStringCase struct {
	selector string
	expected []MarkedString
}

func assertSelectorMarkedStringCases(t *testing.T, tests []selectorMarkedStringCase) {
	t.Helper()
	for _, tt := range tests {
		t.Run(tt.selector, func(t *testing.T) {
			assertSelectorMarkedStrings(t, tt.selector, tt.expected)
		})
	}
}

func assertSelectorMarkedStrings(t *testing.T, selector string, expected []MarkedString) {
	t.Helper()
	actual := SelectorToMarkedStrings(selector)
	if !reflect.DeepEqual(actual, expected) {
		t.Fatalf("%s\nactual: %#v\nwant:   %#v", selector, actual, expected)
	}
}

func assertSelectorMarkedStringsAt(t *testing.T, selector string, offset int, expected []MarkedString) {
	t.Helper()
	actual := SelectorToMarkedStringsAt(selector, offset)
	if !reflect.DeepEqual(actual, expected) {
		t.Fatalf("%s\nactual: %#v\nwant:   %#v", selector, actual, expected)
	}
}

func assertElementAttributes(t *testing.T, selector string, expected []ElementAttribute) {
	t.Helper()
	actual := ToElement(selector).Attributes
	if !reflect.DeepEqual(actual, expected) {
		t.Fatalf("%s\nactual: %#v\nwant:   %#v", selector, actual, expected)
	}
}

func assertSelectorElement(t *testing.T, languageID, input, selectorName, expected string) {
	t.Helper()
	offset := strings.Index(input, selectorName)
	if offset == -1 {
		t.Fatalf("selector %q not found in %q", selectorName, input)
	}
	actual := elementDebugString(SelectorToElementInDocument(input, offset, languageID))
	if actual != expected {
		t.Fatalf("%s\nactual: %s\nwant:   %s", input, actual, expected)
	}
}

func elementAttr(name, value string) ElementAttribute {
	return ElementAttribute{Name: name, Value: stringPtr(value)}
}

func elementAttrUndefined(name string) ElementAttribute {
	return ElementAttribute{Name: name}
}

func elementDebugString(element *Element) string {
	if element == nil {
		return ""
	}
	label, _ := element.FindAttribute("name")
	var attributes []ElementAttribute
	for _, attribute := range element.Attributes {
		if attribute.Name != "name" {
			attributes = append(attributes, attribute)
		}
	}
	if len(attributes) > 0 {
		label += "["
		for index, attribute := range attributes {
			if index > 0 {
				label += "|"
			}
			value := "undefined"
			if attribute.Value != nil {
				value = *attribute.Value
			}
			label += attribute.Name + "=" + value
		}
		label += "]"
	}
	if len(element.Children) > 0 {
		label += "{"
		for index, child := range element.Children {
			if index > 0 {
				label += "|"
			}
			label += elementDebugString(child)
		}
		label += "}"
	}
	return label
}
