package htmlservice

import (
	"fmt"
	"path"
	"strings"
	"testing"
)

func TestHoverBaselineParity(t *testing.T) {
	ls := GetLanguageService()
	descriptionAndReference := "The html element represents the root of an HTML document." +
		"\n\n" +
		"![Baseline icon](" + baselineHighImage + ") _Widely available across major browsers (Baseline since 2015)_" +
		"\n\n" +
		"[MDN Reference](https://developer.mozilla.org/docs/Web/HTML/Reference/Elements/html)"

	assertNoHover(t, ls, `|<html></html>`, nil)
	assertNoHover(t, ls, `<html>|</html>`, nil)
	assertNoHover(t, ls, `<html><|/html>`, nil)
	assertNoHover(t, ls, `<html></html>|`, nil)

	for _, tc := range []struct {
		input     string
		rangeText string
	}{
		{`<|html></html>`, "html"},
		{`<h|tml></html>`, "html"},
		{`<htm|l></html>`, "html"},
		{`<html|></html>`, "html"},
		{`<html></|html>`, "html"},
		{`<html></h|tml>`, "html"},
		{`<html></ht|ml>`, "html"},
		{`<html></htm|l>`, "html"},
		{`<html></html|>`, "html"},
	} {
		hover := hoverAt(t, ls, tc.input, nil)
		content := hoverMarkup(t, hover)
		if content.Kind != MarkupKindMarkdown || content.Value != descriptionAndReference {
			t.Fatalf("unexpected html hover content: got %#v want %q", content, descriptionAndReference)
		}
		if got := hoverRangeText(t, hover, tc.input); got != tc.rangeText {
			t.Fatalf("hover range text: got %q want %q", got, tc.rangeText)
		}
	}

	hover := hoverAt(t, ls, `<html>&|nbsp;</html>`, nil)
	if got, ok := hover.Contents.(string); !ok || got != "Character entity representing '\u00A0', unicode equivalent 'U+00A0'" {
		t.Fatalf("unexpected entity hover: %#v", hover.Contents)
	}
	if got := hoverRangeText(t, hover, `<html>&|nbsp;</html>`); got != "nbsp;" {
		t.Fatalf("entity hover range: got %q", got)
	}
	for _, input := range []string{`<html>&n|bsp;</html>`, `<html>&nb|sp;</html>`, `<html>&nbs|p;</html>`, `<html>&nbsp|;</html>`} {
		hover = hoverAt(t, ls, input, nil)
		if got, ok := hover.Contents.(string); !ok || got != "Character entity representing '\u00A0', unicode equivalent 'U+00A0'" {
			t.Fatalf("unexpected entity hover for %s: %#v", input, hover.Contents)
		}
		if got := hoverRangeText(t, hover, input); got != "nbsp;" {
			t.Fatalf("entity hover range for %s: got %q", input, got)
		}
	}
	hover = hoverAt(t, ls, `<html>&frac|78;</html>`, nil)
	if got, ok := hover.Contents.(string); !ok || got != "Character entity representing '\u215E', unicode equivalent 'U+215E'" {
		t.Fatalf("unexpected full entity hover: %#v", hover.Contents)
	}
	if got := hoverRangeText(t, hover, `<html>&frac|78;</html>`); got != "frac78;" {
		t.Fatalf("full entity hover range: got %q", got)
	}
	hover = hoverAt(t, ls, `<html>&As|cr;</html>`, nil)
	wantAstralEntity := "Character entity representing '" + "\U0001D49C" + "', unicode equivalent 'U+D835'"
	if got, ok := hover.Contents.(string); !ok || got != wantAstralEntity {
		t.Fatalf("unexpected astral entity hover: %#v", hover.Contents)
	}
	assertNoHover(t, ls, `<html>|&nbsp;</html>`, nil)
	assertNoHover(t, ls, `<html>&nbsp;|</html>`, nil)
	assertNoHover(t, ls, `&|nbsp;`, nil)
	assertNoHover(t, ls, `<div data-&|nbsp;></div>`, nil)
	assertNoHover(t, ls, `<div></|span>`, nil)

	plainLS := GetLanguageService(LanguageServiceOptions{ClientCapabilities: &ClientCapabilities{}})
	hover = hoverAt(t, plainLS, `<html|></html>`, nil)
	content := hoverMarkup(t, hover)
	wantPlainText := "The html element represents the root of an HTML document." +
		"\n\n" +
		"Widely available across major browsers (Baseline since 2015)" +
		"\n\n" +
		"MDN Reference: https://developer.mozilla.org/docs/Web/HTML/Reference/Elements/html"
	if content.Kind != MarkupKindPlainText || content.Value != wantPlainText {
		t.Fatalf("unexpected plaintext hover: got %#v want %q", content, wantPlainText)
	}

	off := false
	hover = hoverAt(t, ls, `<html|></html>`, &HoverSettings{Documentation: &off})
	content = hoverMarkup(t, hover)
	if content.Kind != MarkupKindMarkdown || content.Value != "[MDN Reference](https://developer.mozilla.org/docs/Web/HTML/Reference/Elements/html)" {
		t.Fatalf("unexpected references-only hover: %#v", content)
	}
	if got := hoverRangeText(t, hover, `<html|></html>`); got != "html" {
		t.Fatalf("references-only hover range: got %q", got)
	}

	caps := &ClientCapabilities{TextDocument: &TextDocumentClientCapabilities{Hover: &HoverClientCapabilities{ContentFormat: []MarkupKind{MarkupKindMarkdown}}}}
	cachedLS := GetLanguageService(LanguageServiceOptions{ClientCapabilities: caps})
	hover = hoverAt(t, cachedLS, `<html|></html>`, nil)
	if content := hoverMarkup(t, hover); content.Kind != MarkupKindMarkdown {
		t.Fatalf("initial cached hover kind got %q want markdown", content.Kind)
	}
	caps.TextDocument.Hover.ContentFormat = nil
	hover = hoverAt(t, cachedLS, `<html|></html>`, nil)
	if content := hoverMarkup(t, hover); content.Kind != MarkupKindMarkdown {
		t.Fatalf("mutated capabilities hover kind got %q want markdown", content.Kind)
	}

	hover = hoverAt(t, ls, `<html|></html>`, &HoverSettings{References: &off})
	content = hoverMarkup(t, hover)
	wantNoReferences := "The html element represents the root of an HTML document." +
		"\n\n![Baseline icon](" + baselineHighImage + ") _Widely available across major browsers (Baseline since 2015)_"
	if content.Kind != MarkupKindMarkdown || content.Value != wantNoReferences {
		t.Fatalf("unexpected documentation-only hover: got %#v want %q", content, wantNoReferences)
	}
	if got := hoverRangeText(t, hover, `<html|></html>`); got != "html" {
		t.Fatalf("documentation-only hover range: got %q", got)
	}
}

func TestHoverAttributeAndValueMatchingMatchesForkSource(t *testing.T) {
	ls := GetLanguageService()
	assertNoHover(t, ls, `<input |TYPE="text">`, nil)
	assertNoHover(t, ls, `<input type="|TEXT">`, nil)

	provider := NewHTMLDataProvider("hover-edge", HTMLDataV1{
		Version: 1,
		Tags: []TagData{{
			Name: "x-hover",
			Attributes: []AttributeData{
				{
					Name:       "refonly",
					References: []Reference{{Name: "Ref", URL: "https://example.test/refonly"}},
				},
				{
					Name: "mode",
					Values: []ValueData{{
						Name:       "refonly",
						References: []Reference{{Name: "Ref", URL: "https://example.test/value"}},
					}},
				},
			},
		}},
	})
	useDefault := false
	customLS := GetLanguageService(LanguageServiceOptions{UseDefaultDataProvider: &useDefault, CustomDataProviders: []HTMLDataProvider{provider}})
	assertNoHover(t, customLS, `<x-hover |refonly></x-hover>`, nil)
	assertNoHover(t, customLS, `<x-hover mode="|refonly"></x-hover>`, nil)
}

func TestCustomProviderBaselineParity(t *testing.T) {
	provider := NewHTMLDataProvider("test", HTMLDataV1{
		Version: 1,
		Tags: []TagData{
			{
				Name:        "foo",
				Description: MarkupContent{Kind: MarkupKindMarkdown, Value: "The `<foo>` element"},
				Attributes: []AttributeData{{
					Name:        "bar",
					Description: MarkupContent{Kind: MarkupKindMarkdown, Value: "The `<foo bar>` attribute"},
					Values: []ValueData{{
						Name:        "baz",
						Description: MarkupContent{Kind: MarkupKindMarkdown, Value: "The `<foo bar=\"baz\">` attribute"},
					}},
				}},
			},
			{
				Name:        "Bar",
				Description: MarkupContent{Kind: MarkupKindMarkdown, Value: "The `<Bar>` element"},
				Attributes:  []AttributeData{{Name: "Xoo"}},
			},
		},
		GlobalAttributes: []AttributeData{
			{Name: "fooAttr", Description: MarkupContent{Kind: MarkupKindMarkdown, Value: "`fooAttr` Attribute"}},
			{Name: "xattr", Description: MarkupContent{Kind: MarkupKindMarkdown, Value: "`xattr` attributes"}, ValueSet: "x"},
		},
		ValueSets: []ValueSet{{
			Name: "x",
			Values: []ValueData{{
				Name:        "xval",
				Description: MarkupContent{Kind: MarkupKindMarkdown, Value: "`xval` value"},
			}},
		}},
	})
	ls := GetLanguageService(LanguageServiceOptions{CustomDataProviders: []HTMLDataProvider{provider}})

	list, doc := completionForWithLS(ls, `<|`, nil)
	assertUniqueCompletionLabels(t, list)
	assertCompletionApplies(t, list, doc, "foo", `<foo`)
	assertCompletionApplies(t, list, doc, "Bar", `<Bar`)
	assertDocumentationValue(t, mustCompletion(t, list, "foo"), "The `<foo>` element")
	assertDocumentationValue(t, mustCompletion(t, list, "Bar"), "The `<Bar>` element")

	list, doc = completionForWithLS(ls, `<foo |`, nil)
	assertUniqueCompletionLabels(t, list)
	assertCompletionApplies(t, list, doc, "bar", `<foo bar="$1"`)
	assertCompletionApplies(t, list, doc, "fooAttr", `<foo fooAttr="$1"`)
	assertCompletionApplies(t, list, doc, "xattr", `<foo xattr="$1"`)
	assertDocumentationValue(t, mustCompletion(t, list, "bar"), "The `<foo bar>` attribute")
	assertDocumentationValue(t, mustCompletion(t, list, "fooAttr"), "`fooAttr` Attribute")
	assertDocumentationValue(t, mustCompletion(t, list, "xattr"), "`xattr` attributes")

	list, doc = completionForWithLS(ls, `<foo bar=|`, nil)
	assertUniqueCompletionLabels(t, list)
	assertCompletionApplies(t, list, doc, "baz", `<foo bar="baz"`)
	assertDocumentationValue(t, mustCompletion(t, list, "baz"), "The `<foo bar=\"baz\">` attribute")

	list, doc = completionForWithLS(ls, `<foo xattr=|`, nil)
	assertUniqueCompletionLabels(t, list)
	assertCompletionApplies(t, list, doc, "xval", `<foo xattr="xval"`)
	assertDocumentationValue(t, mustCompletion(t, list, "xval"), "`xval` value")

	list, doc = completionForWithLS(ls, `<Bar |`, nil)
	assertUniqueCompletionLabels(t, list)
	assertCompletionApplies(t, list, doc, "Xoo", `<Bar Xoo="$1"`)

	list, doc = completionForWithLS(ls, `<other |`, nil)
	assertUniqueCompletionLabels(t, list)
	assertCompletionApplies(t, list, doc, "fooAttr", `<other fooAttr="$1"`)
	assertCompletionApplies(t, list, doc, "xattr", `<other xattr="$1"`)
	assertDocumentationValue(t, mustCompletion(t, list, "fooAttr"), "`fooAttr` Attribute")
	assertDocumentationValue(t, mustCompletion(t, list, "xattr"), "`xattr` attributes")

	list, doc = completionForWithLS(ls, `<other xattr=|`, nil)
	assertUniqueCompletionLabels(t, list)
	assertCompletionApplies(t, list, doc, "xval", `<other xattr="xval"`)
	assertDocumentationValue(t, mustCompletion(t, list, "xval"), "`xval` value")

	assertHoverValue(t, ls, `<f|oo></foo>`, "foo", "The `<foo>` element")
	assertHoverValue(t, ls, `<foo |bar></foo>`, "bar", "The `<foo bar>` attribute")
	assertHoverValue(t, ls, `<foo |xattr></foo>`, "xattr", "`xattr` attributes")
	assertHoverValue(t, ls, `<foo bar="|baz"></foo>`, `"baz"`, "The `<foo bar=\"baz\">` attribute")
	assertHoverValue(t, ls, `<foo bar="b|az`, `"baz`, "The `<foo bar=\"baz\">` attribute")
	assertHoverValue(t, ls, `<foo xattr="|xval"></foo>`, `"xval"`, "`xval` value")
	assertHoverValue(t, ls, `<foo foo="xval" xattr="|xval"></foo>`, `"xval"`, "`xval` value")
}

func TestPathCompletionBaselineParity(t *testing.T) {
	ls := GetLanguageService(LanguageServiceOptions{FileSystemProvider: fixtureFS{}})
	indexURI := "file:///workspace/index.html"
	aboutURI := "file:///workspace/about/about.html"
	ctx := fixtureDocumentContext{root: "file:///workspace"}

	for _, input := range []string{
		`<script src="http:|">`,
		`<script src="http:/|">`,
		`<script src="http://|">`,
		`<script src="https:|">`,
		`<script src="https:/|">`,
		`<script src="https://|">`,
		`<script src="//|">`,
		`<script src="httpfoo|">`,
	} {
		list, _ := pathCompletionFor(t, ls, ctx, indexURI, input)
		if len(list.Items) != 0 {
			t.Fatalf("expected no remote path completions for %q, got %#v", input, list.Items)
		}
	}
	remoteNoCursor := `<script src="http:">`
	doc := NewTextDocument(indexURI, "html", 0, remoteNoCursor)
	htmlDoc := ls.ParseHTMLDocument(doc)
	list := ls.DoComplete2(doc, doc.PositionAt(stringsIndex(remoteNoCursor, "http:")+len("http:")), htmlDoc, ctx, nil)
	if len(list.Items) != 0 {
		t.Fatalf("expected no remote path completions for %q, got %#v", remoteNoCursor, list.Items)
	}

	list, doc = pathCompletionFor(t, ls, ctx, indexURI, `<script src="./|">`)
	assertPathCompletion(t, list, doc, "about/", CompletionItemKindFolder, `<script src="./about/">`, true)
	assertPathCompletion(t, list, doc, "index.html", CompletionItemKindFile, `<script src="./index.html">`, false)
	assertPathCompletion(t, list, doc, "src/", CompletionItemKindFolder, `<script src="./src/">`, true)

	list, doc = pathCompletionFor(t, ls, ctx, indexURI, `<script src='./|'>`)
	assertPathCompletion(t, list, doc, "about/", CompletionItemKindFolder, `<script src='./about/'>`, true)
	assertPathCompletion(t, list, doc, "index.html", CompletionItemKindFile, `<script src='./index.html'>`, false)
	assertPathCompletion(t, list, doc, "src/", CompletionItemKindFolder, `<script src='./src/'>`, true)

	list, doc = pathCompletionFor(t, ls, ctx, aboutURI, `<script src="../|">`)
	assertPathCompletion(t, list, doc, "about/", CompletionItemKindFolder, `<script src="../about/">`, true)
	assertPathCompletion(t, list, doc, "index.html", CompletionItemKindFile, `<script src="../index.html">`, false)
	assertPathCompletion(t, list, doc, "src/", CompletionItemKindFolder, `<script src="../src/">`, true)

	list, doc = pathCompletionFor(t, ls, ctx, aboutURI, `<script src="../src/|">`)
	assertPathCompletion(t, list, doc, "feature.js", CompletionItemKindFile, `<script src="../src/feature.js">`, false)
	assertPathCompletion(t, list, doc, "test.js", CompletionItemKindFile, `<script src="../src/test.js">`, false)

	list, doc = pathCompletionFor(t, ls, ctx, indexURI, `<script src="/|">`)
	assertPathCompletion(t, list, doc, "about/", CompletionItemKindFolder, `<script src="/about/">`, true)
	assertPathCompletion(t, list, doc, "index.html", CompletionItemKindFile, `<script src="/index.html">`, false)
	assertPathCompletion(t, list, doc, "src/", CompletionItemKindFolder, `<script src="/src/">`, true)

	list, doc = pathCompletionFor(t, ls, ctx, aboutURI, `<script src="/src/|">`)
	assertPathCompletion(t, list, doc, "feature.js", CompletionItemKindFile, `<script src="/src/feature.js">`, false)
	assertPathCompletion(t, list, doc, "test.js", CompletionItemKindFile, `<script src="/src/test.js">`, false)

	list, doc = pathCompletionFor(t, ls, ctx, indexURI, `<script src="|">`)
	assertPathCompletion(t, list, doc, "about/", CompletionItemKindFolder, `<script src="about/">`, true)
	assertPathCompletion(t, list, doc, "index.html", CompletionItemKindFile, `<script src="index.html">`, false)
	assertPathCompletion(t, list, doc, "src/", CompletionItemKindFolder, `<script src="src/">`, true)

	list, doc = pathCompletionFor(t, ls, ctx, aboutURI, `<script src="|">`)
	assertPathCompletion(t, list, doc, "about.css", CompletionItemKindFile, `<script src="about.css">`, false)
	assertPathCompletion(t, list, doc, "about.html", CompletionItemKindFile, `<script src="about.html">`, false)
	assertPathCompletion(t, list, doc, "media/", CompletionItemKindFolder, `<script src="media/">`, true)

	list, doc = pathCompletionFor(t, ls, ctx, indexURI, `<script src="/src/f|">`)
	assertPathCompletion(t, list, doc, "feature.js", CompletionItemKindFile, `<script src="/src/feature.js">`, false)
	assertPathCompletion(t, list, doc, "test.js", CompletionItemKindFile, `<script src="/src/test.js">`, false)

	list, doc = pathCompletionFor(t, ls, ctx, aboutURI, `<script src="../src/f|">`)
	assertPathCompletion(t, list, doc, "feature.js", CompletionItemKindFile, `<script src="../src/feature.js">`, false)
	assertPathCompletion(t, list, doc, "test.js", CompletionItemKindFile, `<script src="../src/test.js">`, false)

	list, doc = pathCompletionFor(t, ls, ctx, indexURI, `<script src="s|">`)
	assertPathCompletion(t, list, doc, "about/", CompletionItemKindFolder, `<script src="about/">`, true)
	assertPathCompletion(t, list, doc, "index.html", CompletionItemKindFile, `<script src="index.html">`, false)
	assertPathCompletion(t, list, doc, "src/", CompletionItemKindFolder, `<script src="src/">`, true)

	list, doc = pathCompletionFor(t, ls, ctx, indexURI, `<script src="src/|">`)
	assertPathCompletion(t, list, doc, "feature.js", CompletionItemKindFile, `<script src="src/feature.js">`, false)
	assertPathCompletion(t, list, doc, "test.js", CompletionItemKindFile, `<script src="src/test.js">`, false)

	list, doc = pathCompletionFor(t, ls, ctx, indexURI, `<script src="src/f|">`)
	assertPathCompletion(t, list, doc, "feature.js", CompletionItemKindFile, `<script src="src/feature.js">`, false)
	assertPathCompletion(t, list, doc, "test.js", CompletionItemKindFile, `<script src="src/test.js">`, false)

	list, doc = pathCompletionFor(t, ls, ctx, aboutURI, `<script src="s|">`)
	assertPathCompletion(t, list, doc, "about.css", CompletionItemKindFile, `<script src="about.css">`, false)
	assertPathCompletion(t, list, doc, "about.html", CompletionItemKindFile, `<script src="about.html">`, false)
	assertPathCompletion(t, list, doc, "media/", CompletionItemKindFolder, `<script src="media/">`, true)

	list, doc = pathCompletionFor(t, ls, ctx, indexURI, `<script src="src/f|eature.js">`)
	assertPathCompletion(t, list, doc, "feature.js", CompletionItemKindFile, `<script src="src/feature.js">`, false)
	assertPathCompletion(t, list, doc, "test.js", CompletionItemKindFile, `<script src="src/test.js">`, false)

	list, doc = pathCompletionFor(t, ls, ctx, indexURI, `<script src="s|rc/feature.js">`)
	assertPathCompletion(t, list, doc, "about/", CompletionItemKindFolder, `<script src="about/">`, true)
	assertPathCompletion(t, list, doc, "index.html", CompletionItemKindFile, `<script src="index.html">`, false)
	assertPathCompletion(t, list, doc, "src/", CompletionItemKindFolder, `<script src="src/">`, true)

	list, doc = pathCompletionFor(t, ls, ctx, aboutURI, `<script src="media/f|eature.js">`)
	assertPathCompletion(t, list, doc, "icon.pic", CompletionItemKindFile, `<script src="media/icon.pic">`, false)

	list, doc = pathCompletionFor(t, ls, ctx, aboutURI, `<script src="m|edia/feature.js">`)
	assertPathCompletion(t, list, doc, "about.css", CompletionItemKindFile, `<script src="about.css">`, false)
	assertPathCompletion(t, list, doc, "about.html", CompletionItemKindFile, `<script src="about.html">`, false)
	assertPathCompletion(t, list, doc, "media/", CompletionItemKindFolder, `<script src="media/">`, true)

	list, doc = pathCompletionFor(t, ls, ctx, indexURI, `<script src="./| about/about.html>`)
	assertPathCompletion(t, list, doc, "about/", CompletionItemKindFolder, `<script src="./about/ about/about.html>`, true)
	assertPathCompletion(t, list, doc, "index.html", CompletionItemKindFile, `<script src="./index.html about/about.html>`, false)
	assertPathCompletion(t, list, doc, "src/", CompletionItemKindFolder, `<script src="./src/ about/about.html>`, true)

	list, doc = pathCompletionFor(t, ls, ctx, indexURI, `<script src="./a|bout /about.html>`)
	assertPathCompletion(t, list, doc, "about/", CompletionItemKindFolder, `<script src="./about/ /about.html>`, true)
	assertPathCompletion(t, list, doc, "index.html", CompletionItemKindFile, `<script src="./index.html /about.html>`, false)
	assertPathCompletion(t, list, doc, "src/", CompletionItemKindFolder, `<script src="./src/ /about.html>`, true)

	list, doc = pathCompletionFor(t, ls, ctx, aboutURI, `<script src="media/|">`)
	assertPathCompletion(t, list, doc, "icon.pic", CompletionItemKindFile, `<script src="media/icon.pic">`, false)

	list, doc = pathCompletionFor(t, ls, ctx, aboutURI, `<script src="media/f|">`)
	assertPathCompletion(t, list, doc, "icon.pic", CompletionItemKindFile, `<script src="media/icon.pic">`, false)

	list, _ = pathCompletionFor(t, ls, ctx, indexURI, `<script src="./|"`)
	if len(list.Items) != 4 {
		t.Fatalf("expected four visible root entries without dotfiles, got %#v", list.Items)
	}
	for _, item := range list.Items {
		if strings.HasPrefix(item.Label, ".") {
			t.Fatalf("dotfile should not be suggested: %#v", list.Items)
		}
	}

	list, _ = pathCompletionFor(t, ls, ctx, indexURI, `<script src="./|`)
	if !list.IsIncomplete || len(list.Items) != 0 {
		t.Fatalf("unterminated dot path got incomplete=%v items=%#v want incomplete with no items", list.IsIncomplete, list.Items)
	}

	list, doc = pathCompletionFor(t, ls, ctx, aboutURI, `<my-custom-element src="../|">`)
	assertPathCompletion(t, list, doc, "about/", CompletionItemKindFolder, `<my-custom-element src="../about/">`, true)
	assertPathCompletion(t, list, doc, "index.html", CompletionItemKindFile, `<my-custom-element src="../index.html">`, false)
	assertPathCompletion(t, list, doc, "src/", CompletionItemKindFolder, `<my-custom-element src="../src/">`, true)

	list, doc = pathCompletionFor(t, ls, ctx, aboutURI, `<my-custom-element href="../src/|">`)
	assertPathCompletion(t, list, doc, "feature.js", CompletionItemKindFile, `<my-custom-element href="../src/feature.js">`, false)
	assertPathCompletion(t, list, doc, "test.js", CompletionItemKindFile, `<my-custom-element href="../src/test.js">`, false)

	list, doc = pathCompletionFor(t, ls, ctx, aboutURI, `<link rel="stylesheet" href="|">`)
	assertPathCompletion(t, list, doc, "about.css", CompletionItemKindFile, `<link rel="stylesheet" href="about.css">`, false)
	assertPathCompletion(t, list, doc, "about.html", CompletionItemKindFile, `<link rel="stylesheet" href="about.html">`, false)
	assertPathCompletion(t, list, doc, "media/", CompletionItemKindFolder, `<link rel="stylesheet" href="media/">`, true)

	list, doc = pathCompletionFor(t, ls, ctx, indexURI, `<link rel="stylesheet" href="./|">`)
	assertPathCompletion(t, list, doc, "about/", CompletionItemKindFolder, `<link rel="stylesheet" href="./about/">`, true)
	assertPathCompletion(t, list, doc, "styles.css", CompletionItemKindFile, `<link rel="stylesheet" href="./styles.css">`, false)
	assertPathCompletion(t, list, doc, "index.html", CompletionItemKindFile, `<link rel="stylesheet" href="./index.html">`, false)
	assertPathCompletion(t, list, doc, "src/", CompletionItemKindFolder, `<link rel="stylesheet" href="./src/">`, true)

	list, doc = pathCompletionFor(t, ls, ctx, indexURI, `<link rel='stylesheet' href="./|">`)
	assertPathCompletion(t, list, doc, "about/", CompletionItemKindFolder, `<link rel='stylesheet' href="./about/">`, true)
	assertPathCompletion(t, list, doc, "styles.css", CompletionItemKindFile, `<link rel='stylesheet' href="./styles.css">`, false)
	assertPathCompletion(t, list, doc, "index.html", CompletionItemKindFile, `<link rel='stylesheet' href="./index.html">`, false)
	assertPathCompletion(t, list, doc, "src/", CompletionItemKindFolder, `<link rel='stylesheet' href="./src/">`, true)

	list, doc = pathCompletionFor(t, ls, ctx, indexURI, `<script src="./src/|">`)
	assertPathCompletion(t, list, doc, "feature.js", CompletionItemKindFile, `<script src="./src/feature.js">`, false)
	assertPathCompletion(t, list, doc, "test.js", CompletionItemKindFile, `<script src="./src/test.js">`, false)

	list, doc = pathCompletionFor(t, ls, ctx, indexURI, `<link href="./|">`)
	assertPathCompletion(t, list, doc, "about/", CompletionItemKindFolder, `<link href="./about/">`, true)
	assertPathCompletion(t, list, doc, "styles.css", CompletionItemKindFile, `<link href="./styles.css">`, false)
	assertPathCompletion(t, list, doc, "index.html", CompletionItemKindFile, `<link href="./index.html">`, false)
	assertPathCompletion(t, list, doc, "src/", CompletionItemKindFolder, `<link href="./src/">`, true)

	list, doc = pathCompletionFor(t, ls, ctx, aboutURI, `<link rel="icon" href="|">`)
	assertPathCompletion(t, list, doc, "about.css", CompletionItemKindFile, `<link rel="icon" href="about.css">`, false)
	assertPathCompletion(t, list, doc, "about.html", CompletionItemKindFile, `<link rel="icon" href="about.html">`, false)
	assertPathCompletion(t, list, doc, "media/", CompletionItemKindFolder, `<link rel="icon" href="media/">`, true)
}

func completionForWithLS(ls LanguageService, value string, options *CompletionConfiguration) (CompletionList, *TextDocument) {
	offset := stringsIndex(value, "|")
	value = value[:offset] + value[offset+1:]
	doc := NewTextDocument("test://test/test.html", "html", 0, value)
	htmlDoc := ls.ParseHTMLDocument(doc)
	return ls.DoComplete(doc, doc.PositionAt(offset), htmlDoc, options), doc
}

func hoverAt(t *testing.T, ls LanguageService, value string, options *HoverSettings) *Hover {
	t.Helper()
	offset := stringsIndex(value, "|")
	value = value[:offset] + value[offset+1:]
	doc := NewTextDocument("test://test/test.html", "html", 0, value)
	htmlDoc := ls.ParseHTMLDocument(doc)
	hover := ls.DoHover(doc, doc.PositionAt(offset), htmlDoc, options)
	if hover == nil {
		t.Fatalf("expected hover for %q", value)
	}
	return hover
}

func assertNoHover(t *testing.T, ls LanguageService, value string, options *HoverSettings) {
	t.Helper()
	offset := stringsIndex(value, "|")
	value = value[:offset] + value[offset+1:]
	doc := NewTextDocument("test://test/test.html", "html", 0, value)
	htmlDoc := ls.ParseHTMLDocument(doc)
	if hover := ls.DoHover(doc, doc.PositionAt(offset), htmlDoc, options); hover != nil {
		t.Fatalf("expected no hover for %q, got %#v", value, hover)
	}
}

func hoverMarkup(t *testing.T, hover *Hover) MarkupContent {
	t.Helper()
	content, ok := hover.Contents.(MarkupContent)
	if !ok {
		t.Fatalf("expected MarkupContent hover, got %#v", hover.Contents)
	}
	return content
}

func hoverRangeText(t *testing.T, hover *Hover, marked string) string {
	t.Helper()
	offset := stringsIndex(marked, "|")
	text := marked[:offset] + marked[offset+1:]
	doc := NewTextDocument("test://test/test.html", "html", 0, text)
	if hover.Range == nil {
		t.Fatalf("expected hover range")
	}
	return doc.GetText(*hover.Range)
}

func assertHoverValue(t *testing.T, ls LanguageService, marked, rangeText, contentValue string) {
	t.Helper()
	hover := hoverAt(t, ls, marked, nil)
	if got := hoverRangeText(t, hover, marked); got != rangeText {
		t.Fatalf("hover range text: got %q want %q", got, rangeText)
	}
	content := hoverMarkup(t, hover)
	if content.Value != contentValue {
		t.Fatalf("hover content: got %#v want %q", content, contentValue)
	}
}

func mustCompletion(t *testing.T, list CompletionList, label string) CompletionItem {
	t.Helper()
	var match *CompletionItem
	for _, item := range list.Items {
		if item.Label == label {
			if match != nil {
				t.Fatalf("completion %q should exist only once in %#v", label, list.Items)
			}
			copy := item
			match = &copy
		}
	}
	if match == nil {
		t.Fatalf("missing completion %q in %#v", label, list.Items)
	}
	return *match
}

func assertDocumentationValue(t *testing.T, item CompletionItem, want string) {
	t.Helper()
	content, ok := item.Documentation.(MarkupContent)
	if !ok {
		t.Fatalf("expected MarkupContent documentation, got %#v", item.Documentation)
	}
	if content.Value != want {
		t.Fatalf("documentation value: got %q want %q", content.Value, want)
	}
}

func pathCompletionFor(t *testing.T, ls LanguageService, ctx DocumentContext, uri, value string) (CompletionList, *TextDocument) {
	t.Helper()
	offset := stringsIndex(value, "|")
	value = value[:offset] + value[offset+1:]
	doc := NewTextDocument(uri, "html", 0, value)
	htmlDoc := ls.ParseHTMLDocument(doc)
	return ls.DoComplete2(doc, doc.PositionAt(offset), htmlDoc, ctx, nil), doc
}

func assertPathCompletion(t *testing.T, list CompletionList, doc *TextDocument, label string, kind CompletionItemKind, result string, expectCommand bool) {
	t.Helper()
	item := mustCompletion(t, list, label)
	if item.Kind != kind {
		t.Fatalf("%s kind: got %v want %v", label, item.Kind, kind)
	}
	if item.TextEdit == nil {
		t.Fatalf("%s has no text edit", label)
	}
	if got := ApplyEdits(doc, []TextEdit{*item.TextEdit}); got != result {
		t.Fatalf("%s applied text: got %q want %q", label, got, result)
	}
	if expectCommand {
		if item.Command == nil || item.Command.Command != "editor.action.triggerSuggest" || item.Command.Title != "Suggest" {
			t.Fatalf("%s expected trigger suggest command, got %#v", label, item.Command)
		}
	} else if item.Command != nil {
		t.Fatalf("%s did not expect command, got %#v", label, item.Command)
	}
}

func TestHoverKnownTagWithoutDocumentationMatchesForkSource(t *testing.T) {
	provider := NewHTMLDataProvider("empty-doc", HTMLDataV1{
		Version: 1,
		Tags:    []TagData{{Name: "x-empty"}},
	})
	useDefault := false
	ls := GetLanguageService(LanguageServiceOptions{UseDefaultDataProvider: &useDefault, CustomDataProviders: []HTMLDataProvider{provider}})
	hover := hoverAt(t, ls, `<x-|empty></x-empty>`, nil)
	content := hoverMarkup(t, hover)
	if content.Kind != MarkupKindMarkdown || content.Value != "" {
		t.Fatalf("empty tag documentation hover: %#v", content)
	}
}

type fixtureDocumentContext struct {
	root string
}

func (c fixtureDocumentContext) ResolveReference(ref, base string) (string, bool) {
	if isRemotePath(ref) {
		return "", false
	}
	if strings.HasPrefix(ref, "/") {
		return normalizeFixtureURI(c.root + ref), true
	}
	dir := base
	if idx := strings.LastIndex(dir, "/"); idx > len("file://") {
		dir = dir[:idx]
	}
	return normalizeFixtureURI(dir + "/" + ref), true
}

type fixtureFS struct{}

func (fixtureFS) Stat(uri DocumentUri) (FileStat, error) {
	return FileStat{Type: FileTypeDirectory}, nil
}

func (fixtureFS) ReadDirectory(uri DocumentUri) ([][2]any, error) {
	switch normalizeFixtureURI(uri) {
	case "file:///workspace":
		return [][2]any{{".foo.js", FileTypeFile}, {"about", FileTypeDirectory}, {"index.html", FileTypeFile}, {"src", FileTypeDirectory}, {"styles.css", FileTypeFile}}, nil
	case "file:///workspace/about":
		return [][2]any{{"about.css", FileTypeFile}, {"about.html", FileTypeFile}, {"media", FileTypeDirectory}}, nil
	case "file:///workspace/about/media":
		return [][2]any{{"icon.pic", FileTypeFile}}, nil
	case "file:///workspace/src":
		return [][2]any{{"feature.js", FileTypeFile}, {"test.js", FileTypeFile}}, nil
	default:
		return nil, fmt.Errorf("missing fixture directory %s", uri)
	}
}

func normalizeFixtureURI(uri DocumentUri) string {
	const prefix = "file://"
	if !strings.HasPrefix(uri, prefix) {
		return uri
	}
	cleaned := path.Clean(strings.TrimPrefix(uri, prefix))
	if cleaned == "." {
		cleaned = "/"
	}
	return prefix + cleaned
}
