package services

import (
	"reflect"
	"testing"

	"github.com/yottonoko/vscode-css-languageservice-go/lsp"
)

func TestCSSSymbolInformations(t *testing.T) {
	assertSymbolInfos(t, ".foo {}", []lsp.SymbolInformation{
		{Name: ".foo", Kind: lsp.SymbolKindClass, Location: lsp.Location{URI: "test://test/test.css", Range: offsetRange(0, 7)}},
	})
	assertSymbolInfos(t, ".foo:not(.selected) {}", []lsp.SymbolInformation{
		{Name: ".foo:not(.selected)", Kind: lsp.SymbolKindClass, Location: lsp.Location{URI: "test://test/test.css", Range: offsetRange(0, 22)}},
	})
	assertSymbolInfos(t, ".voo.doo, .bar {}", []lsp.SymbolInformation{
		{Name: ".voo.doo", Kind: lsp.SymbolKindClass, Location: lsp.Location{URI: "test://test/test.css", Range: offsetRange(0, 17)}},
		{Name: ".bar", Kind: lsp.SymbolKindClass, Location: lsp.Location{URI: "test://test/test.css", Range: offsetRange(10, 17)}},
	})
	assertSymbolInfos(t, "@media screen, print {}", []lsp.SymbolInformation{
		{Name: "@media screen, print", Kind: lsp.SymbolKindModule, Location: lsp.Location{URI: "test://test/test.css", Range: offsetRange(0, 23)}},
	})
	assertSymbolInfos(t, "@scope (.foo) to (.bar) {}", []lsp.SymbolInformation{
		{Name: "@scope .foo → .bar", Kind: lsp.SymbolKindModule, Location: lsp.Location{URI: "test://test/test.css", Range: offsetRange(0, 26)}},
	})
}

func TestCSSNavigationSymbolsPortedNamedCases(t *testing.T) {
	t.Run("basic symbol infos", func(t *testing.T) {
		assertSymbolInfos(t, ".foo {}", []lsp.SymbolInformation{
			{Name: ".foo", Kind: lsp.SymbolKindClass, Location: lsp.Location{URI: "test://test/test.css", Range: offsetRange(0, 7)}},
		})
		assertSymbolInfos(t, ".foo:not(.selected) {}", []lsp.SymbolInformation{
			{Name: ".foo:not(.selected)", Kind: lsp.SymbolKindClass, Location: lsp.Location{URI: "test://test/test.css", Range: offsetRange(0, 22)}},
		})
		assertSymbolInfos(t, ".voo.doo, .bar {}", []lsp.SymbolInformation{
			{Name: ".voo.doo", Kind: lsp.SymbolKindClass, Location: lsp.Location{URI: "test://test/test.css", Range: offsetRange(0, 17)}},
			{Name: ".bar", Kind: lsp.SymbolKindClass, Location: lsp.Location{URI: "test://test/test.css", Range: offsetRange(10, 17)}},
		})
		assertSymbolInfos(t, "@media screen, print {}", []lsp.SymbolInformation{
			{Name: "@media screen, print", Kind: lsp.SymbolKindModule, Location: lsp.Location{URI: "test://test/test.css", Range: offsetRange(0, 23)}},
		})
		assertSymbolInfos(t, "@scope (.foo) to (.bar) {}", []lsp.SymbolInformation{
			{Name: "@scope .foo → .bar", Kind: lsp.SymbolKindModule, Location: lsp.Location{URI: "test://test/test.css", Range: offsetRange(0, 26)}},
		})
	})
	t.Run("basic document symbols", func(t *testing.T) {
		assertDocumentSymbols(t, ".foo {}", []lsp.DocumentSymbol{
			{Name: ".foo", Kind: lsp.SymbolKindClass, Range: offsetRange(0, 7), SelectionRange: offsetRange(0, 4)},
		})
		assertDocumentSymbols(t, ".foo:not(.selected) {}", []lsp.DocumentSymbol{
			{Name: ".foo:not(.selected)", Kind: lsp.SymbolKindClass, Range: offsetRange(0, 22), SelectionRange: offsetRange(0, 19)},
		})
		assertDocumentSymbols(t, ".voo.doo, .bar {}", []lsp.DocumentSymbol{
			{Name: ".voo.doo", Kind: lsp.SymbolKindClass, Range: offsetRange(0, 17), SelectionRange: offsetRange(0, 8)},
			{Name: ".bar", Kind: lsp.SymbolKindClass, Range: offsetRange(10, 17), SelectionRange: offsetRange(10, 14)},
		})
		assertDocumentSymbols(t, "@media screen, print {}", []lsp.DocumentSymbol{
			{Name: "@media screen, print", Kind: lsp.SymbolKindModule, Range: offsetRange(0, 23), SelectionRange: offsetRange(7, 20)},
		})
		assertDocumentSymbols(t, "@scope (.foo) to (.bar) {}", []lsp.DocumentSymbol{
			{Name: "@scope .foo → .bar", Kind: lsp.SymbolKindModule, Range: offsetRange(0, 26), SelectionRange: offsetRange(7, 23)},
		})
	})
}

func TestCSSDocumentSymbols(t *testing.T) {
	assertDocumentSymbols(t, ".foo {}", []lsp.DocumentSymbol{
		{Name: ".foo", Kind: lsp.SymbolKindClass, Range: offsetRange(0, 7), SelectionRange: offsetRange(0, 4)},
	})
	assertDocumentSymbols(t, ".foo:not(.selected) {}", []lsp.DocumentSymbol{
		{Name: ".foo:not(.selected)", Kind: lsp.SymbolKindClass, Range: offsetRange(0, 22), SelectionRange: offsetRange(0, 19)},
	})
	assertDocumentSymbols(t, ".voo.doo, .bar {}", []lsp.DocumentSymbol{
		{Name: ".voo.doo", Kind: lsp.SymbolKindClass, Range: offsetRange(0, 17), SelectionRange: offsetRange(0, 8)},
		{Name: ".bar", Kind: lsp.SymbolKindClass, Range: offsetRange(10, 17), SelectionRange: offsetRange(10, 14)},
	})
	assertDocumentSymbols(t, "@media screen, print {}", []lsp.DocumentSymbol{
		{Name: "@media screen, print", Kind: lsp.SymbolKindModule, Range: offsetRange(0, 23), SelectionRange: offsetRange(7, 20)},
	})
	assertDocumentSymbols(t, "@scope (.foo) to (.bar) {}", []lsp.DocumentSymbol{
		{Name: "@scope .foo → .bar", Kind: lsp.SymbolKindModule, Range: offsetRange(0, 26), SelectionRange: offsetRange(7, 23)},
	})
}

func TestLESSNavigationSymbolsPortedNamedCases(t *testing.T) {
	t.Run("basic symbols", func(t *testing.T) {
		assertSymbolInfosForLanguage(t, "less", ".a(@gutter: @gutter-width) { &:extend(.b); }", []lsp.SymbolInformation{
			{Name: ".a", Kind: lsp.SymbolKindMethod, Location: lsp.Location{URI: "test://test/test.less", Range: offsetRange(0, 44)}},
		})
		assertDocumentSymbolsForLanguage(t, "less", ".a(@gutter: @gutter-width) { &:extend(.b); }", []lsp.DocumentSymbol{
			{Name: ".a", Kind: lsp.SymbolKindMethod, Range: offsetRange(0, 44), SelectionRange: offsetRange(0, 2)},
		})
		assertSymbolInfosForLanguage(t, "less", ".mixin() { .nested() {} }", []lsp.SymbolInformation{
			{Name: ".mixin", Kind: lsp.SymbolKindMethod, Location: lsp.Location{URI: "test://test/test.less", Range: offsetRange(0, 25)}},
			{Name: ".nested", Kind: lsp.SymbolKindMethod, Location: lsp.Location{URI: "test://test/test.less", Range: offsetRange(11, 23)}},
		})
		assertDocumentSymbolsForLanguage(t, "less", ".mixin() { .nested() {} }", []lsp.DocumentSymbol{
			{Name: ".mixin", Kind: lsp.SymbolKindMethod, Range: offsetRange(0, 25), SelectionRange: offsetRange(0, 6), Children: []lsp.DocumentSymbol{
				{Name: ".nested", Kind: lsp.SymbolKindMethod, Range: offsetRange(11, 23), SelectionRange: offsetRange(11, 18)},
			}},
		})
	})
}

func TestSCSSDocumentSymbols(t *testing.T) {
	assertDocumentSymbolsForLanguage(t, "scss", "@mixin foo { }", []lsp.DocumentSymbol{
		{Name: "foo", Kind: lsp.SymbolKindMethod, Range: offsetRange(0, 14), SelectionRange: offsetRange(7, 10)},
	})
	assertDocumentSymbolsForLanguage(t, "scss", "@function square($x) { @return $x * $x; }", []lsp.DocumentSymbol{
		{Name: "square", Kind: lsp.SymbolKindFunction, Range: offsetRange(0, 41), SelectionRange: offsetRange(10, 16), Children: []lsp.DocumentSymbol{
			{Name: "$x", Kind: lsp.SymbolKindVariable, Range: offsetRange(17, 19), SelectionRange: offsetRange(17, 19)},
		}},
	})
	assertDocumentSymbolsForLanguage(t, "scss", "$var1: 1; .foo { $var2: 2; }", []lsp.DocumentSymbol{
		{Name: "$var1", Kind: lsp.SymbolKindVariable, Range: offsetRange(0, 5), SelectionRange: offsetRange(0, 5)},
		{Name: ".foo", Kind: lsp.SymbolKindClass, Range: offsetRange(10, 28), SelectionRange: offsetRange(10, 14), Children: []lsp.DocumentSymbol{
			{Name: "$var2", Kind: lsp.SymbolKindVariable, Range: offsetRange(17, 22), SelectionRange: offsetRange(17, 22)},
		}},
	})
}

func TestSCSSNavigationSymbolsPortedNamedCases(t *testing.T) {
	t.Run("scss document symbols", func(t *testing.T) {
		assertDocumentSymbolsForLanguage(t, "scss", "@mixin foo { }", []lsp.DocumentSymbol{
			{Name: "foo", Kind: lsp.SymbolKindMethod, Range: offsetRange(0, 14), SelectionRange: offsetRange(7, 10)},
		})
		assertDocumentSymbolsForLanguage(t, "scss", "@mixin {}", []lsp.DocumentSymbol{
			{Name: "<undefined>", Kind: lsp.SymbolKindMethod, Range: offsetRange(0, 9), SelectionRange: offsetRange(0, 0)},
		})
	})
}

func TestLESSDocumentSymbols(t *testing.T) {
	assertSymbolInfosForLanguage(t, "less", ".a(@gutter: @gutter-width) { &:extend(.b); }", []lsp.SymbolInformation{
		{Name: ".a", Kind: lsp.SymbolKindMethod, Location: lsp.Location{URI: "test://test/test.less", Range: offsetRange(0, 44)}},
	})
	assertDocumentSymbolsForLanguage(t, "less", ".a(@gutter: @gutter-width) { &:extend(.b); }", []lsp.DocumentSymbol{
		{Name: ".a", Kind: lsp.SymbolKindMethod, Range: offsetRange(0, 44), SelectionRange: offsetRange(0, 2)},
	})
	assertSymbolInfosForLanguage(t, "less", ".mixin() { .nested() {} }", []lsp.SymbolInformation{
		{Name: ".mixin", Kind: lsp.SymbolKindMethod, Location: lsp.Location{URI: "test://test/test.less", Range: offsetRange(0, 25)}},
		{Name: ".nested", Kind: lsp.SymbolKindMethod, Location: lsp.Location{URI: "test://test/test.less", Range: offsetRange(11, 23)}},
	})
	assertDocumentSymbolsForLanguage(t, "less", ".mixin() { .nested() {} }", []lsp.DocumentSymbol{
		{Name: ".mixin", Kind: lsp.SymbolKindMethod, Range: offsetRange(0, 25), SelectionRange: offsetRange(0, 6), Children: []lsp.DocumentSymbol{
			{Name: ".nested", Kind: lsp.SymbolKindMethod, Range: offsetRange(11, 23), SelectionRange: offsetRange(11, 18)},
		}},
	})
}

func assertSymbolInfos(t *testing.T, input string, expected []lsp.SymbolInformation) {
	t.Helper()
	assertSymbolInfosForLanguage(t, "css", input, expected)
}

func assertSymbolInfosForLanguage(t *testing.T, languageID string, input string, expected []lsp.SymbolInformation) {
	t.Helper()
	document := lsp.NewTextDocument(lsp.DocumentURI("test://test/test."+languageID), languageID, 0, input)
	actual := FindDocumentSymbols(document)
	if !reflect.DeepEqual(actual, expected) {
		t.Fatalf("%s\nactual: %#v\nwant:   %#v", input, actual, expected)
	}
}

func assertDocumentSymbols(t *testing.T, input string, expected []lsp.DocumentSymbol) {
	t.Helper()
	assertDocumentSymbolsForLanguage(t, "css", input, expected)
}

func assertDocumentSymbolsForLanguage(t *testing.T, languageID string, input string, expected []lsp.DocumentSymbol) {
	t.Helper()
	document := lsp.NewTextDocument(lsp.DocumentURI("test://test/test."+languageID), languageID, 0, input)
	actual := FindDocumentSymbols2(document)
	if !reflect.DeepEqual(actual, expected) {
		t.Fatalf("%s\nactual: %#v\nwant:   %#v", input, actual, expected)
	}
}

func offsetRange(start, end int) lsp.Range {
	return lsp.Range{
		Start: lsp.Position{Line: 0, Character: start},
		End:   lsp.Position{Line: 0, Character: end},
	}
}
