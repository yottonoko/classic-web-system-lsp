package lspserver

import (
	"reflect"
	"strings"
	"testing"

	"github.com/yottonoko/classic-web-system-lsp/internal/core"
	"github.com/yottonoko/classic-web-system-lsp/internal/lsp"
)

func TestHTMLCompletionNamesCollectAcrossEmbeddedCSSRegions(t *testing.T) {
	const source = `<style>
/* .comment-only #comment-only */
.first, .duplicate { content: ".string-only #string-only"; }
<% If enabled Then : .asp-only { content: ".asp-string" } : End If %>
</style>
<div id="html-name" title="日本語" class="html-name duplicate"></div>
<span style="--class-ref: .inline-name; --id-ref: #inline-name;"></span>
<style>
#first, #duplicate, .last, .duplicate {}
</style>`
	parsed := core.ParseDocument("file:///site/completion-names.asp", source, core.Settings{})
	doc := core.NewTextDocument(parsed.URI, "classic-asp", 1, source)

	if got, want := collectHTMLClassCompletionNames(parsed, doc), []string{"duplicate", "first", "html-name", "inline-name", "last"}; !reflect.DeepEqual(got, want) {
		t.Fatalf("class completion names = %#v, want %#v", got, want)
	}
	if got, want := collectHTMLIDCompletionNames(parsed, doc), []string{"duplicate", "first", "html-name", "inline-name"}; !reflect.DeepEqual(got, want) {
		t.Fatalf("id completion names = %#v, want %#v", got, want)
	}
}

func TestHTMLCompletionNamesKeepIncompleteStyleAttributeFragments(t *testing.T) {
	const source = `<div style="--class-ref: .partial-class; --id-ref: #partial-id`
	parsed := core.ParseDocument("file:///site/incomplete-completion-names.asp", source, core.Settings{})
	doc := core.NewTextDocument(parsed.URI, "classic-asp", 1, source)

	if got, want := collectHTMLClassCompletionNames(parsed, doc), []string{"partial-class"}; !reflect.DeepEqual(got, want) {
		t.Fatalf("incomplete class completion names = %#v, want %#v", got, want)
	}
	if got, want := collectHTMLIDCompletionNames(parsed, doc), []string{"partial-id"}; !reflect.DeepEqual(got, want) {
		t.Fatalf("incomplete id completion names = %#v, want %#v", got, want)
	}
}

func TestHTMLCompletionNamesResetCSSLexicalStateAtRegionBoundaries(t *testing.T) {
	const source = `<style>/* .hidden-comment #hidden-comment</style>
<style>".hidden-string #hidden-string</style>
<style>.visible-class {} #visible-id {}</style>`
	parsed := core.ParseDocument("file:///site/region-boundary-completion-names.asp", source, core.Settings{})
	doc := core.NewTextDocument(parsed.URI, "classic-asp", 1, source)

	if got, want := collectHTMLClassCompletionNames(parsed, doc), []string{"visible-class"}; !reflect.DeepEqual(got, want) {
		t.Fatalf("boundary class completion names = %#v, want %#v", got, want)
	}
	if got, want := collectHTMLIDCompletionNames(parsed, doc), []string{"visible-id"}; !reflect.DeepEqual(got, want) {
		t.Fatalf("boundary id completion names = %#v, want %#v", got, want)
	}
	classOffset := strings.Index(source, ".visible-class") + 1
	target, ok := embeddedRenameTarget(parsed, classOffset)
	if !ok || target.Kind != embeddedRenameClass || target.Name != "visible-class" {
		t.Fatalf("boundary CSS rename target = %#v, %t", target, ok)
	}
}

func TestHTMLCompletionNamesResetCSSLexicalStateAfterStyleAttribute(t *testing.T) {
	const source = `<div style="content: 'unterminated"></div>
<style>.visible-class {} #visible-id {}</style>`
	parsed := core.ParseDocument("file:///site/style-attribute-boundary-completion-names.asp", source, core.Settings{})
	doc := core.NewTextDocument(parsed.URI, "classic-asp", 1, source)

	if got, want := collectHTMLClassCompletionNames(parsed, doc), []string{"visible-class"}; !reflect.DeepEqual(got, want) {
		t.Fatalf("style attribute boundary class completion names = %#v, want %#v", got, want)
	}
	if got, want := collectHTMLIDCompletionNames(parsed, doc), []string{"visible-id"}; !reflect.DeepEqual(got, want) {
		t.Fatalf("style attribute boundary id completion names = %#v, want %#v", got, want)
	}
}

func TestHTMLClassCompletionItemsKeepUTF16RangesAfterCSSCollection(t *testing.T) {
	const source = `<div title="日本語" class="al"></div><style>.alpha {}</style>`
	doc := core.NewTextDocument("file:///site/utf16-completion-names.asp", "classic-asp", 1, source)
	parsed := core.ParseDocument(doc.URI, source, core.Settings{})
	classStart := strings.Index(source, `class="`) + len(`class="`)
	position := doc.PositionAt(classStart + len("al"))
	items := htmlClassCompletionItems(parsed, doc, position)
	var alpha *lsp.CompletionItem
	for index := range items {
		if items[index].Label == "alpha" {
			alpha = &items[index]
			break
		}
	}
	if alpha == nil || alpha.TextEdit == nil {
		t.Fatalf("alpha completion item = %#v", alpha)
	}
	if got, want := alpha.TextEdit.Range, doc.Range(classStart, classStart+len("al")); got != want {
		t.Fatalf("alpha completion range = %#v, want %#v", got, want)
	}
}
