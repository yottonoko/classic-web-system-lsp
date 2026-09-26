package services

import (
	"reflect"
	"testing"

	"github.com/yottonoko/vscode-css-languageservice-go/languagefacts"
	"github.com/yottonoko/vscode-css-languageservice-go/lsp"
)

func TestHoverPropertyDescription(t *testing.T) {
	t.Run("basic", func(t *testing.T) {
		hover := assertHoverAt(t, ".test { |color: blue; }", HoverOptions{})
		content := hoverMarkdownContent(t, hover)
		want := "Sets the color of an element's text\n\n" +
			"![Baseline icon](" + baselineHighImage + ") _Widely available across major browsers (Baseline since 2015)_\n\n" +
			"Syntax: &lt;color&gt;\n\n" +
			"[MDN Reference](https://developer.mozilla.org/docs/Web/CSS/Reference/Properties/color)"
		if content.Value != want {
			t.Fatalf("hover content = %q, want %q", content.Value, want)
		}

		disabled := false
		hover = assertHoverAt(t, ".test { |color: blue; }", HoverOptions{Documentation: &disabled})
		if got := hoverMarkdownContent(t, hover).Value; got != "[MDN Reference](https://developer.mozilla.org/docs/Web/CSS/Reference/Properties/color)" {
			t.Fatalf("documentation disabled hover = %q", got)
		}

		hover = assertHoverAt(t, ".test { |color: blue; }", HoverOptions{References: &disabled})
		want = "Sets the color of an element's text\n\n" +
			"![Baseline icon](" + baselineHighImage + ") _Widely available across major browsers (Baseline since 2015)_\n\n" +
			"Syntax: &lt;color&gt;"
		if got := hoverMarkdownContent(t, hover).Value; got != want {
			t.Fatalf("references disabled hover = %q, want %q", got, want)
		}
	})
}

func TestHoverSelectorSpecificity(t *testing.T) {
	t.Run("specificity", func(t *testing.T) {
		hover := assertHoverAt(t, ".|foo {}", HoverOptions{})
		want := []any{
			lsp.MarkedString{Language: "html", Value: "<element class=\"foo\">"},
			"[Selector Specificity](https://developer.mozilla.org/docs/Web/CSS/Specificity): (0, 1, 0)",
		}
		if !reflect.DeepEqual(hover.Contents, want) {
			t.Fatalf("selector hover = %#v, want %#v", hover.Contents, want)
		}
	})
}

func TestHoverNestedSelectorContext(t *testing.T) {
	t.Run("nested", func(t *testing.T) {
		hover := assertHoverAt(t, "div { d|iv {} }", HoverOptions{})
		want := []any{
			lsp.MarkedString{Language: "html", Value: "<div>\n  …\n    <div>"},
			"[Selector Specificity](https://developer.mozilla.org/docs/Web/CSS/Specificity): (0, 0, 1)",
		}
		if !reflect.DeepEqual(hover.Contents, want) {
			t.Fatalf("selector hover = %#v, want %#v", hover.Contents, want)
		}

		hover = assertHoverAt(t, ".foo{ .bar{ @media only screen{ .|bar{ } } } }", HoverOptions{})
		want = []any{
			lsp.MarkedString{Language: "html", Value: "@media only screen\n<element class=\"foo\">\n  …\n    <element class=\"bar\">\n      …\n        <element class=\"bar\">"},
			"[Selector Specificity](https://developer.mozilla.org/docs/Web/CSS/Specificity): (0, 1, 0)",
		}
		if !reflect.DeepEqual(hover.Contents, want) {
			t.Fatalf("selector hover = %#v, want %#v", hover.Contents, want)
		}

		hover = assertHoverAt(t, "@scope (.foo) to (.bar) { .|baz{ } }", HoverOptions{})
		want = []any{
			lsp.MarkedString{Language: "html", Value: "@scope .foo → .bar\n<element class=\"baz\">"},
			"[Selector Specificity](https://developer.mozilla.org/docs/Web/CSS/Specificity): (0, 1, 0)",
		}
		if !reflect.DeepEqual(hover.Contents, want) {
			t.Fatalf("selector hover = %#v, want %#v", hover.Contents, want)
		}

		hover = assertHoverAt(t, "@scope (.from) to (.to) { .foo { @media print { .bar { @media only screen{ .|bar{ } } } } } }", HoverOptions{})
		want = []any{
			lsp.MarkedString{Language: "html", Value: "@scope .from → .to\n@media print\n@media only screen\n<element class=\"foo\">\n  …\n    <element class=\"bar\">\n      …\n        <element class=\"bar\">"},
			"[Selector Specificity](https://developer.mozilla.org/docs/Web/CSS/Specificity): (0, 1, 0)",
		}
		if !reflect.DeepEqual(hover.Contents, want) {
			t.Fatalf("selector hover = %#v, want %#v", hover.Contents, want)
		}
	})
}

func TestHoverAtRuleSelectorContext(t *testing.T) {
	hover := assertHoverAt(t, "@scope (.foo) to (.bar) { .|baz{ } }", HoverOptions{})
	want := []any{
		lsp.MarkedString{Language: "html", Value: "@scope .foo → .bar\n<element class=\"baz\">"},
		"[Selector Specificity](https://developer.mozilla.org/docs/Web/CSS/Specificity): (0, 1, 0)",
	}
	if !reflect.DeepEqual(hover.Contents, want) {
		t.Fatalf("selector hover = %#v, want %#v", hover.Contents, want)
	}

	hover = assertHoverAt(t, ".foo{ @media print{ .|bar{ } } }", HoverOptions{})
	want = []any{
		lsp.MarkedString{Language: "html", Value: "@media print\n<element class=\"foo\">\n  …\n    <element class=\"bar\">"},
		"[Selector Specificity](https://developer.mozilla.org/docs/Web/CSS/Specificity): (0, 1, 0)",
	}
	if !reflect.DeepEqual(hover.Contents, want) {
		t.Fatalf("selector hover = %#v, want %#v", hover.Contents, want)
	}
}

func TestSCSSHoverAtRoot(t *testing.T) {
	t.Run("@at-root", func(t *testing.T) {
		hover := assertHoverAtForLanguage(t, "scss", ".test { @|at-root { }", HoverOptions{})
		want := []any{}
		if !reflect.DeepEqual(hover.Contents, want) {
			t.Fatalf("selector hover = %#v, want %#v", hover.Contents, want)
		}
	})
}

func assertHoverAt(t *testing.T, markedInput string, options HoverOptions) *lsp.Hover {
	t.Helper()
	return assertHoverAtForLanguage(t, "css", markedInput, options)
}

func assertHoverAtForLanguage(t *testing.T, languageID string, markedInput string, options HoverOptions) *lsp.Hover {
	t.Helper()
	offset := stringsIndex(markedInput, "|")
	input := markedInput[:offset] + markedInput[offset+1:]
	document := lsp.NewTextDocument(lsp.DocumentURI("test://test/test."+languageID), languageID, 0, input)
	hover := Hover(document, document.PositionAt(offset), languagefacts.NewDataManager(languagefacts.DataManagerOptions{}), options)
	if hover == nil {
		t.Fatal("hover is nil")
	}
	return hover
}

func hoverMarkdownContent(t *testing.T, hover *lsp.Hover) lsp.MarkupContent {
	t.Helper()
	content, ok := hover.Contents.(lsp.MarkupContent)
	if !ok {
		t.Fatalf("hover content = %#v", hover.Contents)
	}
	if content.Kind != lsp.MarkupKindMarkdown {
		t.Fatalf("hover content kind = %q", content.Kind)
	}
	return content
}
