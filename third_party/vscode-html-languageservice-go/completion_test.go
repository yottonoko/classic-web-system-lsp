package htmlservice

import (
	"encoding/json"
	"testing"
)

func completionFor(value string, options *CompletionConfiguration) (CompletionList, *TextDocument) {
	offset := stringsIndex(value, "|")
	value = value[:offset] + value[offset+1:]
	ls := GetLanguageService()
	doc := NewTextDocument("test://test/test.html", "html", 0, value)
	pos := doc.PositionAt(offset)
	htmlDoc := ls.ParseHTMLDocument(doc)
	return ls.DoComplete(doc, pos, htmlDoc, options), doc
}

func TestCompletionTagsAttributesValuesAndEndTags(t *testing.T) {
	list, doc := completionFor("<|", nil)
	assertCompletionApplies(t, list, doc, "div", "<div")
	assertCompletionApplies(t, list, doc, "!DOCTYPE", "<!DOCTYPE html>")

	list, doc = completionFor(`<input |`, nil)
	assertCompletionApplies(t, list, doc, "type", `<input type="$1"`)
	assertCompletionApplies(t, list, doc, "disabled", `<input disabled`)

	list, doc = completionFor(`<input type="c|`, nil)
	assertCompletionApplies(t, list, doc, "checkbox", `<input type="checkbox`)
	assertCompletionApplies(t, list, doc, "color", `<input type="color`)

	list, doc = completionFor(`<ul><li></|>`, nil)
	assertCompletionApplies(t, list, doc, "/li", `<ul><li></li>`)
}

func TestCompletionHideAutoCompleteOnlyHidesAutoCloseProposal(t *testing.T) {
	hideAuto := true
	list, doc := completionFor("<|", &CompletionConfiguration{HideAutoCompleteProposals: &hideAuto})
	assertCompletionApplies(t, list, doc, "div", "<div")

	list, doc = completionFor(`<input |`, &CompletionConfiguration{HideAutoCompleteProposals: &hideAuto})
	assertCompletionApplies(t, list, doc, "type", `<input type="$1"`)

	list, _ = completionFor(`<div>|`, &CompletionConfiguration{HideAutoCompleteProposals: &hideAuto})
	assertNoCompletion(t, list, "</div>")

	hideEnd := true
	list, doc = completionFor(`<div>|`, &CompletionConfiguration{HideEndTagSuggestions: &hideEnd})
	assertCompletionApplies(t, list, doc, "</div>", `<div>$0</div>`)
}

func TestCompletionItemKindsMatchForkSource(t *testing.T) {
	list, _ := completionFor(`<input |`, nil)
	if got := mustCompletion(t, list, "type").Kind; got != CompletionItemKindValue {
		t.Fatalf("attribute kind got %v want %v", got, CompletionItemKindValue)
	}

	useDefault := false
	provider := NewHTMLDataProvider("kind-test", HTMLDataV1{
		Version: 1,
		Tags: []TagData{{
			Name:       "x-kind",
			Attributes: []AttributeData{{Name: "run", ValueSet: "handler"}},
		}},
	})
	customLS := GetLanguageService(LanguageServiceOptions{UseDefaultDataProvider: &useDefault, CustomDataProviders: []HTMLDataProvider{provider}})
	list, _ = completionForWithLS(customLS, `<x-kind |`, nil)
	if got := mustCompletion(t, list, "run").Kind; got != CompletionItemKindFunction {
		t.Fatalf("handler kind got %v want %v", got, CompletionItemKindFunction)
	}

	list, _ = completionFor(`<div d|`, nil)
	if got := mustCompletion(t, list, "data-").Kind; got != CompletionItemKindValue {
		t.Fatalf("data-* kind got %v want %v", got, CompletionItemKindValue)
	}

	list, _ = completionFor(`<input type=|`, nil)
	if got := mustCompletion(t, list, "text").Kind; got != CompletionItemKindUnit {
		t.Fatalf("attribute value kind got %v want %v", got, CompletionItemKindUnit)
	}
}

func TestCompletionObservableItemFieldsMatchForkSource(t *testing.T) {
	list, _ := completionFor(`<input |`, nil)
	typeItem := mustCompletion(t, list, "type")
	if typeItem.Command == nil || typeItem.Command.Title != "Suggest" || typeItem.Command.Command != "editor.action.triggerSuggest" {
		t.Fatalf("type command got %#v", typeItem.Command)
	}
	styleItem := mustCompletion(t, list, "style")
	if styleItem.Command == nil || styleItem.Command.Title != "Suggest" || styleItem.Command.Command != "editor.action.triggerSuggest" {
		t.Fatalf("style command got %#v", styleItem.Command)
	}
	if disabled := mustCompletion(t, list, "disabled"); disabled.Command != nil {
		t.Fatalf("disabled command got %#v want nil", disabled.Command)
	}

	list, doc := completionFor(`<script a|`, nil)
	assertCompletionApplies(t, list, doc, "async", `<script async`)

	list, _ = completionFor(`<input type=|`, nil)
	textItem := mustCompletion(t, list, "text")
	if textItem.FilterText != `"text"` {
		t.Fatalf("attribute value filterText got %q want %q", textItem.FilterText, `"text"`)
	}
	if textItem.InsertTextFormat != InsertTextFormatPlainText {
		t.Fatalf("attribute value insertTextFormat got %v want %v", textItem.InsertTextFormat, InsertTextFormatPlainText)
	}

	list, doc = completionFor(`</|`, nil)
	divItem := mustCompletion(t, list, "/div")
	if divItem.TextEdit == nil {
		t.Fatalf("/div has no text edit")
	}
	if got := doc.OffsetAt(divItem.TextEdit.Range.Start); got != 1 {
		t.Fatalf("/div edit start got %d want 1", got)
	}
	if divItem.TextEdit.NewText != "/div>" {
		t.Fatalf("/div edit newText got %q want %q", divItem.TextEdit.NewText, "/div>")
	}

	list, doc = completionFor(`<goo></|>`, nil)
	gooItem := mustCompletion(t, list, "/goo")
	if gooItem.TextEdit == nil {
		t.Fatalf("/goo has no text edit")
	}
	if got := doc.OffsetAt(gooItem.TextEdit.Range.Start); got != 6 {
		t.Fatalf("/goo edit start got %d want 6", got)
	}
	if gooItem.TextEdit.NewText != "/goo" {
		t.Fatalf("/goo edit newText got %q want %q", gooItem.TextEdit.NewText, "/goo")
	}

	list, _ = completionFor(`<ul><|>`, nil)
	assertCompletionOrder(t, list, []string{"li", "/ul"})
}

func TestCompletionAuditParityEdges(t *testing.T) {
	list, doc := completionFor(`  <|`, nil)
	doctype := mustCompletion(t, list, "!DOCTYPE")
	if doctype.Documentation != "A preamble for an HTML document." {
		t.Fatalf("doctype documentation got %#v", doctype.Documentation)
	}
	assertCompletionApplies(t, list, doc, "!DOCTYPE", `  <!DOCTYPE html>`)
	list, _ = completionFor(`<!-- | -->`, nil)
	if len(list.Items) != 0 {
		t.Fatalf("comment completion got %#v want none", list.Items)
	}
	list, _ = completionFor(`<!DOCTYPE |>`, nil)
	if len(list.Items) != 0 {
		t.Fatalf("doctype body completion got %#v want none", list.Items)
	}

	list, doc = completionFor(`<input type="&|">`, nil)
	assertCompletionApplies(t, list, doc, "text", `<input type="text">`)
	entity := mustCompletion(t, list, "&amp;")
	if entity.TextEdit == nil || entity.TextEdit.NewText != "&amp;" {
		t.Fatalf("entity completion got %#v", entity.TextEdit)
	}
	assertCompletionApplies(t, list, doc, "&amp;", `<input type="&amp;">`)

	list, doc = completionFor(`<div>&|</div>`, nil)
	assertCompletionApplies(t, list, doc, "&amp;", `<div>&amp;</div>`)
	list, _ = completionFor(`<div>&amp;|</div>`, nil)
	assertNoCompletion(t, list, "&amp;")
	list, doc = completionFor(`<div>&am|p;</div>`, nil)
	assertCompletionApplies(t, list, doc, "&amp;", `<div>&amp;p;</div>`)
	list, doc = completionFor(`<input type="c<|">`, nil)
	assertCompletionApplies(t, list, doc, "color", `<input type="color">`)
	list, doc = completionFor(`<input type="c>|">`, nil)
	assertCompletionApplies(t, list, doc, "color", `<input type="color">`)
	list, _ = completionFor(`<div>|</div>`, nil)
	mustCompletion(t, list, "</div>")
	list, _ = completionFor(`<style>|`, nil)
	mustCompletion(t, list, "</style>")
	list, _ = completionFor(`<script>|`, nil)
	mustCompletion(t, list, "</script>")
	list, doc = completionFor(`<script TYPE="text/html"> <| </script>`, nil)
	assertCompletionApplies(t, list, doc, "div", `<script TYPE="text/html"> <div </script>`)
	list, _ = completionFor(`<script type="TEXT/HTML"> <| </script>`, nil)
	if len(list.Items) != 0 {
		t.Fatalf("uppercase script type raw text completion got %#v want none", list.Items)
	}
	list, _ = completionFor(`<style> <| </style>`, nil)
	if len(list.Items) != 0 {
		t.Fatalf("style raw text completion got %#v want none", list.Items)
	}
	list, _ = completionFor(`<script> <| </script>`, nil)
	if len(list.Items) != 0 {
		t.Fatalf("script raw text completion got %#v want none", list.Items)
	}
	list, _ = completionFor("<input\u00a0|", nil)
	if len(list.Items) != 0 {
		t.Fatalf("NBSP tag completion got %#v want none", list.Items)
	}
	list, _ = completionFor("<div&d|", nil)
	if len(list.Items) != 0 {
		t.Fatalf("attribute without whitespace completion got %#v want none", list.Items)
	}
	list, _ = completionFor(`<!-- <| -->`, nil)
	if len(list.Items) != 0 {
		t.Fatalf("comment inner tag completion got %#v want none", list.Items)
	}
	for _, input := range []string{`<!-- &| -->`, `<!DOCTYPE &|>`, `<![CDATA[ &| ]]>`} {
		list, _ = completionFor(input, nil)
		if len(list.Items) != 0 {
			t.Fatalf("ignored markup entity completion for %q got %#v want none", input, list.Items)
		}
	}
	list, _ = completionFor(`<input type="text"t|>`, nil)
	if len(list.Items) != 0 {
		t.Fatalf("attribute without separator after quoted value got %#v want none", list.Items)
	}
	list, _ = completionFor("<input type=text\u00a0|>", nil)
	if len(list.Items) != 0 {
		t.Fatalf("attribute value after NBSP got %#v want none", list.Items)
	}
	list, _ = pathCompletionForFS("<script src=foo\u00a0|>", fakeFS{})
	if len(list.Items) != 0 {
		t.Fatalf("path value after NBSP got %#v want none", list.Items)
	}
	list, doc = completionFor(`< |d`, nil)
	assertCompletionApplies(t, list, doc, "div", `<div`)
	list, doc = completionFor(`<foo></ |f`, nil)
	assertCompletionApplies(t, list, doc, "/foo", `<foo></foo>f`)
	list, doc = completionFor(`<ul><li></|>`, nil)
	assertCompletionEdit(t, list, doc, "/li", 9, 10, "/li", "/li")
	if item := mustCompletion(t, list, "/li"); item.InsertTextFormat != InsertTextFormatPlainText {
		t.Fatalf("/li insertTextFormat got %d want %d", item.InsertTextFormat, InsertTextFormatPlainText)
	}
	list, doc = completionFor(`<input type= |c>`, nil)
	assertCompletionApplies(t, list, doc, "color", `<input type= "color"c>`)
	list, doc = pathCompletionForFS(`<script src= |foo>`, fakeFS{})
	item := mustCompletion(t, list, "app.js")
	if item.TextEdit == nil {
		t.Fatalf("path whitespace item has no edit")
	}
	if start, end := doc.OffsetAt(item.TextEdit.Range.Start), doc.OffsetAt(item.TextEdit.Range.End); start != 14 || end != 12 {
		t.Fatalf("path whitespace range got %d..%d want 14..12", start, end)
	}
	for _, input := range []string{"<input type=c=|>", "<input type=c`|>", "<input type=c'|>", "<input type=c\"|>"} {
		list, _ = completionFor(input, nil)
		if len(list.Items) != 0 {
			t.Fatalf("unquoted value delimiter completion for %q got %#v want none", input, list.Items)
		}
	}
	for _, input := range []string{"<script src=foo/|>", "<script src=foo=|>"} {
		list, _ = pathCompletionForFS(input, fakeFS{})
		if len(list.Items) != 0 {
			t.Fatalf("unquoted path delimiter completion for %q got %#v want none", input, list.Items)
		}
	}
	for _, input := range []string{"<div \x7f|>", "<div \u0080|>"} {
		list, _ = completionFor(input, nil)
		if len(list.Items) != 0 {
			t.Fatalf("invalid attribute name completion for %q got %#v want none", input, list.Items)
		}
	}
	list, doc = completionFor(`<section><div <|>`, nil)
	assertCompletionApplies(t, list, doc, "/section", `<section><div </section>`)
	assertNoCompletion(t, list, "/div")
	list, doc = completionFor(`<div a< b|>`, nil)
	assertCompletionEdit(t, list, doc, "div", 8, 9, "div", "")
	assertNoCompletion(t, list, "/div")
	list, doc = completionFor(`<div <a|>`, nil)
	assertCompletionEdit(t, list, doc, "div", 6, 7, "div", "")
	assertNoCompletion(t, list, "/div")
	for _, input := range []string{`<div </|>`, `<div <<|>`, `<div <<d|>`} {
		list, _ = completionFor(input, nil)
		if len(list.Items) != 0 {
			t.Fatalf("malformed nested tag completion for %q got %#v want none", input, list.Items)
		}
	}
	list, doc = completionFor(`<div / |>`, nil)
	assertCompletionApplies(t, list, doc, "class", `<div / class="$1">`)
	list, doc = completionFor(`<div foo=bar |baz>`, nil)
	assertCompletionApplies(t, list, doc, "class", `<div foo=bar class="$1"baz>`)
	list, _ = completionFor(`<div></d |>`, nil)
	if len(list.Items) != 0 {
		t.Fatalf("end tag whitespace after partial name got %#v want none", list.Items)
	}
	for _, input := range []string{`<?|`, `<-|`, `<div></?|`} {
		list, _ = completionFor(input, nil)
		if len(list.Items) != 0 {
			t.Fatalf("invalid tag-name-start completion for %q got %#v want none", input, list.Items)
		}
	}
	list, _ = completionFor(`<div title="<" |>`, nil)
	assertNoCompletion(t, list, "title")
	list, _ = completionFor(`<input | title=">" type="text">`, nil)
	assertNoCompletion(t, list, "type")
	list, _ = pathCompletionForFS(`<link title="<" rel="stylesheet" href="|">`, fakeFS{})
	if item := mustCompletion(t, list, "style.css"); item.SortText != "0_style.css" {
		t.Fatalf("quoted less-than rel style.css sortText got %q want 0_style.css", item.SortText)
	}
	list, _ = pathCompletionForFS(`<link href="|" title=">" rel="stylesheet">`, fakeFS{})
	if item := mustCompletion(t, list, "style.css"); item.SortText != "0_style.css" {
		t.Fatalf("quoted greater-than rel style.css sortText got %q want 0_style.css", item.SortText)
	}

	list, _ = completionFor(`<input TYPE="text" |`, nil)
	mustCompletion(t, list, "type")

	list, _ = completionFor(`<div DATA-X=""></div><div d|`, nil)
	assertNoCompletion(t, list, "DATA-X")
	singleQuotes := &CompletionConfiguration{AttributeDefaultValue: "singlequotes"}
	list, doc = completionFor(`<div data-custom=""></div><div d|`, singleQuotes)
	assertCompletionApplies(t, list, doc, "data-custom", `<div data-custom=""></div><div data-custom="$1"`)
	list, doc = completionFor(`<div data-custom=""></div><div data-c|="">`, singleQuotes)
	assertCompletionApplies(t, list, doc, "data-custom", `<div data-custom=""></div><div data-custom="$1"="">`)

	provider := NewHTMLDataProvider("custom", HTMLDataV1{
		Version: 1,
		Tags: []TagData{{
			Name:       "x-bool",
			Attributes: []AttributeData{{Name: "required"}},
		}},
	})
	ls := GetLanguageService(LanguageServiceOptions{CustomDataProviders: []HTMLDataProvider{provider}})
	list, doc = completionForWithLS(ls, `<x-bool |`, nil)
	assertCompletionApplies(t, list, doc, "required", `<x-bool required="$1"`)

	voidProvider := NewHTMLDataProvider("custom-void", HTMLDataV1{
		Version: 1,
		Tags:    []TagData{{Name: "x-void", Void: true}},
	})
	ls = GetLanguageService(LanguageServiceOptions{CustomDataProviders: []HTMLDataProvider{voidProvider}})
	list, _ = completionForWithLS(ls, `<x-void><|`, nil)
	assertNoCompletion(t, list, "/x-void")
	disabled := &CompletionConfiguration{Provider: map[string]bool{"custom-void": false}}
	list, _ = completionForWithLS(ls, `<x-void><|`, disabled)
	assertNoCompletion(t, list, "/x-void")
	html5 := false
	list, _ = completionFor(`<br><|`, &CompletionConfiguration{HTML5: &html5})
	assertNoCompletion(t, list, "/br")

	list, doc = completionFor("<div>\r  <|", nil)
	assertCompletionApplies(t, list, doc, "/div", "<div>\r</div>")
	list, doc = completionFor("<div>\n<|", nil)
	assertCompletionEdit(t, list, doc, "/div", 7, 7, "/div>", "/div")
	list, doc = completionFor("<div>\n</|", nil)
	assertCompletionEdit(t, list, doc, "/div", 7, 8, "/div>", "/div")
	list, doc = completionFor("<div>\n  <|", nil)
	assertCompletionEdit(t, list, doc, "/div", 6, 9, "</div>", "  </div")
	list, doc = completionFor("<iframe sandbox=\"allow-forms\f|\">", nil)
	assertCompletionApplies(t, list, doc, "allow-modals", "<iframe sandbox=\"allow-forms\fallow-modals\">")
	assertTagCompletion(t, `<div></| >`, "div>")

	if isRemotePath("HTTP://example.test") || isRemotePath(" http://example.test") || !isRemotePath("httpfoo") {
		t.Fatalf("remote path predicate does not match fork source")
	}

	list, _ = pathCompletionForFS(`<link REL="stylesheet" href="|">`, fakeFS{})
	if item := mustCompletion(t, list, "style.css"); item.SortText != "" {
		t.Fatalf("uppercase REL style.css sortText got %q want empty", item.SortText)
	}

	list, doc = pathCompletionForFS(`<script src=s|>`, fakeFS{})
	item = mustCompletion(t, list, "app.js")
	if item.TextEdit == nil {
		t.Fatalf("unquoted path item has no edit")
	}
	start := doc.OffsetAt(item.TextEdit.Range.Start)
	end := doc.OffsetAt(item.TextEdit.Range.End)
	if start != 13 || end != 12 {
		t.Fatalf("unquoted path range got %d..%d want 13..12", start, end)
	}
	list, doc = pathCompletionForFS(`<script src=foo/|bar>`, nestedPathFS{})
	assertCompletionApplies(t, list, doc, "child.js", `<script src=foochild.jsr>`)
	assertCompletionApplies(t, list, doc, "dir/", `<script src=foodir/r>`)
	list, doc = pathCompletionForFS(`<script src=foo/bar|>`, fakeFS{})
	assertCompletionEdit(t, list, doc, "app.js", 15, 18, "app.js", "")
	assertCompletionApplies(t, list, doc, "app.js", `<script src=fooapp.jsr>`)
	list, _ = pathCompletionForFS(`<script src=./|>`, fakeFS{})
	if len(list.Items) != 0 {
		t.Fatalf("unquoted ./ path completions got %#v want none", list.Items)
	}
	list, _ = pathCompletionForFS(`<script src=foo/|>`, nestedPathFS{})
	if len(list.Items) != 0 {
		t.Fatalf("unquoted trailing slash path completions got %#v want none", list.Items)
	}
}

func TestPathCompletionEmptyResolvedBaseMatchesForkSource(t *testing.T) {
	ls := GetLanguageService(LanguageServiceOptions{FileSystemProvider: fakeFS{}})
	doc := NewTextDocument("file:///site/index.html", "html", 0, `<script src="">`)
	cursor := stringsIndex(doc.GetText(), `""`) + 1
	htmlDoc := ls.ParseHTMLDocument(doc)
	list := ls.DoComplete2(doc, doc.PositionAt(cursor), htmlDoc, emptyDocumentContext{}, nil)
	if len(list.Items) != 0 {
		t.Fatalf("empty resolved base path completions got %#v want none", list.Items)
	}
}

func TestDoCompleteDoesNotRunPathCompletionMatchesForkSource(t *testing.T) {
	ls := GetLanguageService(LanguageServiceOptions{FileSystemProvider: fakeFS{}})
	for _, marked := range []string{`<script src=".|">`, `<script src="a|">`} {
		cursor := stringsIndex(marked, "|")
		text := stringsReplace(marked, "|", "")
		doc := NewTextDocument("file:///site/index.html", "html", 0, text)
		htmlDoc := ls.ParseHTMLDocument(doc)
		list := ls.DoComplete(doc, doc.PositionAt(cursor), htmlDoc, nil)
		if list.IsIncomplete || len(list.Items) != 0 {
			t.Fatalf("DoComplete path items for %q got incomplete=%v items=%#v want none", marked, list.IsIncomplete, list.Items)
		}
	}
}

func TestPathCompletionNilDocumentContextMatchesForkSource(t *testing.T) {
	ls := GetLanguageService(LanguageServiceOptions{FileSystemProvider: fakeFS{}})
	doc := NewTextDocument("file:///site/index.html", "html", 0, `<script src=".">`)
	cursor := stringsIndex(doc.GetText(), `"."`) + 2
	htmlDoc := ls.ParseHTMLDocument(doc)
	list := ls.DoComplete2(doc, doc.PositionAt(cursor), htmlDoc, nil, nil)
	if !list.IsIncomplete || len(list.Items) != 0 {
		t.Fatalf("dot path with nil context got %#v want incomplete empty", list)
	}

	doc = NewTextDocument("file:///site/index.html", "html", 0, `<script src="a">`)
	cursor = stringsIndex(doc.GetText(), `"a"`) + 2
	htmlDoc = ls.ParseHTMLDocument(doc)
	defer func() {
		if recover() == nil {
			t.Fatalf("non-dot path with nil context should panic")
		}
	}()
	_ = ls.DoComplete2(doc, doc.PositionAt(cursor), htmlDoc, nil, nil)
}

func TestPathCompletionAcceptsNumericFileTypeTuples(t *testing.T) {
	list, doc := pathCompletionForFS(`<script src="|">`, numericFileTypeFS{})
	assertCompletionApplies(t, list, doc, "dir/", `<script src="dir/">`)
	item := mustCompletion(t, list, "dir/")
	if item.Kind != CompletionItemKindFolder || item.Command == nil {
		t.Fatalf("numeric directory item got kind=%v command=%#v", item.Kind, item.Command)
	}
	if item := mustCompletion(t, list, "file.js"); item.SortText != "0_file.js" {
		t.Fatalf("numeric file sortText got %q want 0_file.js", item.SortText)
	}
}

func TestCompletionProviderOrderMatchesForkSource(t *testing.T) {
	useDefault := false
	provider := NewHTMLDataProvider("order-test", HTMLDataV1{
		Version: 1,
		Tags: []TagData{
			{
				Name: "z-tag",
				Attributes: []AttributeData{
					{
						Name:   "zed",
						Values: []ValueData{{Name: "z-value"}, {Name: "a-value"}},
					},
					{Name: "alpha"},
				},
			},
			{Name: "a-tag"},
		},
	})
	ls := GetLanguageService(LanguageServiceOptions{UseDefaultDataProvider: &useDefault, CustomDataProviders: []HTMLDataProvider{provider}})

	list, _ := completionForWithLS(ls, `<|`, nil)
	assertCompletionOrder(t, list, []string{"!DOCTYPE", "z-tag", "a-tag"})

	list, _ = completionForWithLS(ls, `<z-tag |`, nil)
	assertCompletionOrder(t, list, []string{"zed", "alpha", "data-"})

	list, _ = completionForWithLS(ls, `<z-tag zed=|`, nil)
	assertCompletionOrder(t, list, []string{"z-value", "a-value"})
}

func TestCompletionDataAttributeOrderMatchesForkSource(t *testing.T) {
	useDefault := false
	ls := GetLanguageService(LanguageServiceOptions{UseDefaultDataProvider: &useDefault})
	list, _ := completionForWithLS(ls, `<div data-zed=""></div><div data-alpha=""></div><div d|`, nil)
	assertCompletionOrder(t, list, []string{"data-", "data-zed", "data-alpha"})
}

func TestCompletionDuplicateProviderEntriesMatchForkSource(t *testing.T) {
	useDefault := false
	provider := NewHTMLDataProvider("duplicate-test", HTMLDataV1{
		Version: 1,
		Tags: []TagData{
			{Name: "x-dup"},
			{
				Name: "x-dup",
				Attributes: []AttributeData{{
					Name:   "mode",
					Values: []ValueData{{Name: "same"}, {Name: "same"}},
				}},
			},
		},
	})
	ls := GetLanguageService(LanguageServiceOptions{UseDefaultDataProvider: &useDefault, CustomDataProviders: []HTMLDataProvider{provider}})

	list, _ := completionForWithLS(ls, `<|`, nil)
	if got := completionLabelCount(list, "x-dup"); got != 2 {
		t.Fatalf("duplicate tag completion count got %d want 2: %#v", got, list.Items)
	}

	list, _ = completionForWithLS(ls, `<x-dup mode=|`, nil)
	if got := completionLabelCount(list, "same"); got != 2 {
		t.Fatalf("duplicate value completion count got %d want 2: %#v", got, list.Items)
	}
}

func TestCompletionDisabledCustomVoidProviderAllowsAutoClose(t *testing.T) {
	provider := NewHTMLDataProvider("custom", HTMLDataV1{
		Version: 1,
		Tags: []TagData{{
			Name: "x-void",
			Void: true,
		}},
	})
	ls := GetLanguageService(LanguageServiceOptions{CustomDataProviders: []HTMLDataProvider{provider}})
	list, _ := completionForWithLS(ls, `<x-void>|`, nil)
	assertNoCompletion(t, list, "</x-void>")

	disabled := &CompletionConfiguration{Provider: map[string]bool{"custom": false}}
	list, doc := completionForWithLS(ls, `<x-void>|`, disabled)
	assertCompletionApplies(t, list, doc, "</x-void>", `<x-void>$0</x-void>`)
}

func TestCompletionBaselineRepresentativeParity(t *testing.T) {
	for _, tc := range []struct {
		input  string
		label  string
		result string
	}{
		{`< |`, "div", `<div`},
		{"\n<|", "div", "\n<div"},
		{`<h|`, "html", `<html`},
		{`<d|`, "div", `<div`},
		{`<input|`, "input", `<input`},
		{`<inp|ut`, "input", `<input`},
		{`<|inp`, "input", `<input`},
		{`<input t|`, "type", `<input type="$1"`},
		{`<input t|ype`, "type", `<input type="$1"`},
		{`<input t|ype="text"`, "type", `<input type="text"`},
		{`<input type="text" s|`, "style", `<input type="text" style="$1"`},
		{`<input | type="text"`, "style", `<input style="$1" type="text"`},
		{`<input type="text" type="number" |`, "style", `<input type="text" type="number" style="$1"`},
		{`<input di| type="text"`, "disabled", `<input disabled type="text"`},
		{`<input disabled | type="text"`, "dir", `<input disabled dir="$1" type="text"`},
		{`<input type=|`, "text", `<input type="text"`},
		{`<input type="|`, "checkbox", `<input type="checkbox`},
		{`<input type= |`, "checkbox", `<input type= "checkbox"`},
		{`<input src="c" type="color|" `, "color", `<input src="c" type="color" `},
		{`<input src="c" type=color| `, "color", `<input src="c" type="color" `},
		{`<iframe sandbox="allow-forms |`, "allow-modals", `<iframe sandbox="allow-forms allow-modals`},
		{`<iframe sandbox="allow-forms allow-modals|`, "allow-modals", `<iframe sandbox="allow-forms allow-modals`},
		{`<iframe sandbox="allow-forms all|"`, "allow-modals", `<iframe sandbox="allow-forms allow-modals"`},
		{`<iframe sandbox="allow-forms a|llow-modals "`, "allow-modals", `<iframe sandbox="allow-forms allow-modals "`},
		{`<th><input type="che|</th>`, "checkbox", `<th><input type="checkbox</th>`},
		{`<th><input type="che|</th><td></td>`, "checkbox", `<th><input type="checkbox</th><td></td>`},
		{`<div dir=|></div>`, "ltr", `<div dir="ltr"></div>`},
		{`<ul><|>`, "/ul", `<ul></ul>`},
		{`<ul><li><|`, "/li", `<ul><li></li>`},
		{`<goo></|>`, "/goo", `<goo></goo>`},
		{`<foo></|>`, "/foo", `<foo></foo>`},
		{`<foo></f|`, "/foo", `<foo></foo>`},
		{`<foo></f|o`, "/foo", `<foo></foo>`},
		{`<foo></|fo`, "/foo", `<foo></foo>`},
		{`<foo></ |>`, "/foo", `<foo></foo>`},
		{`<span></ s|`, "/span", `<span></span>`},
		{`<li><br></ |>`, "/li", `<li><br></li>`},
		{`<foo><br/></ f|>`, "/foo", `<foo><br/></foo>`},
		{`<li><div/></|`, "/li", `<li><div/></li>`},
		{`<foo><bar></bar></|   `, "/foo", `<foo><bar></bar></foo>   `},
		{"<div>\n  <form>\n    <div>\n      <label></label>\n      <|\n    </div>\n  </form></div>", "/div", "<div>\n  <form>\n    <div>\n      <label></label>\n    </div>\n    </div>\n  </form></div>"},
		{`<body><div><div></div></div></|  >`, "/body", `<body><div><div></div></div></body  >`},
		{"<body>\n  <div>\n    </|", "/div", "<body>\n  <div>\n  </div>"},
		{`<div><a hre|</div>`, "href", `<div><a href="$1"</div>`},
		{`<a><b>foo</b><|f>`, "/a", `<a><b>foo</b></a>`},
		{`<a><b>foo</b><| bar.`, "/a", `<a><b>foo</b></a> bar.`},
		{`<div><h1><br><span></span><img></| </h1></div>`, "/h1", `<div><h1><br><span></span><img></h1> </h1></div>`},
		{`<div>|`, "</div>", `<div>$0</div>`},
		{`<dIv>|`, "</dIv>", `<dIv>$0</dIv>`},
		{`<script id="entry-template" type="text/x-handlebars-template"> <| </script>`, "div", `<script id="entry-template" type="text/x-handlebars-template"> <div </script>`},
		{`<script id="html-template" type="text/html"> <| </script>`, "div", `<script id="html-template" type="text/html"> <div </script>`},
	} {
		t.Run(tc.input+"/"+tc.label, func(t *testing.T) {
			list, doc := completionFor(tc.input, nil)
			assertCompletionApplies(t, list, doc, tc.label, tc.result)
		})
	}

	list, _ := completionFor("\n<|", nil)
	assertNoCompletion(t, list, "!DOCTYPE")

	list, _ = completionFor(`<input type="text" |`, nil)
	for _, item := range list.Items {
		if item.Label == "type" {
			t.Fatalf("type should not be suggested twice: %#v", list.Items)
		}
	}
	list, _ = completionFor(`<li/|>`, nil)
	if len(list.Items) != 0 {
		t.Fatalf("expected no completion in self-close slash, got %#v", list.Items)
	}
	for _, input := range []string{`  <div/|   `, `<li><br/|>`, `<li><br>a/|`} {
		list, _ = completionFor(input, nil)
		if len(list.Items) != 0 {
			t.Fatalf("expected no completion for %q, got %#v", input, list.Items)
		}
	}
	list, doc := completionFor(`</|`, nil)
	assertCompletionApplies(t, list, doc, "/a", `</a>`)
	assertCompletionApplies(t, list, doc, "/div", `</div>`)
	list, doc = completionFor(`<div></div></|`, nil)
	assertCompletionApplies(t, list, doc, "/a", `<div></div></a>`)
	assertCompletionApplies(t, list, doc, "/div", `<div></div></div>`)

	hideAutoComplete := true
	list, _ = completionFor(`<div>|`, &CompletionConfiguration{HideAutoCompleteProposals: &hideAutoComplete})
	assertNoCompletion(t, list, "</div>")
}

func TestCompletionCloseTagFilterTextBaselineParity(t *testing.T) {
	list, _ := completionFor(`<div>|`, nil)
	if item := mustCompletion(t, list, "</div>"); item.FilterText != "</div>" {
		t.Fatalf("auto close tag filterText: got %q", item.FilterText)
	}
	list, _ = completionFor(`<div> <| </div>`, nil)
	if item := mustCompletion(t, list, "/div"); item.FilterText != "/div" {
		t.Fatalf("single-line close tag filterText: got %q", item.FilterText)
	}
	list, _ = completionFor(`<foo></f|`, nil)
	if item := mustCompletion(t, list, "/foo"); item.FilterText != "/foo" {
		t.Fatalf("typed close tag filterText: got %q", item.FilterText)
	}
	list, _ = completionFor("<div>\n  <|\n</div>", nil)
	if item := mustCompletion(t, list, "/div"); item.FilterText != "  </div" {
		t.Fatalf("indented close tag filterText: got %q", item.FilterText)
	}
	list, _ = completionFor("<body>\n  <div>\n    </|", nil)
	if item := mustCompletion(t, list, "/div"); item.FilterText != "    </div" {
		t.Fatalf("indented current close tag filterText: got %q", item.FilterText)
	}
}

func TestCompletionDataAriaCaseAndSettingsBaselineParity(t *testing.T) {
	list, doc := completionFor(`<div d|`, nil)
	assertCompletionApplies(t, list, doc, "data-", `<div data-$1="$2"`)

	list, _ = completionFor(`<div no-data-test="no-data" d|`, nil)
	assertNoCompletion(t, list, "no-data-test")

	list, doc = completionFor(`<div data-custom="test"><div d|`, nil)
	assertCompletionApplies(t, list, doc, "data-", `<div data-custom="test"><div data-$1="$2"`)
	assertCompletionApplies(t, list, doc, "data-custom", `<div data-custom="test"><div data-custom="$1"`)

	list, doc = completionFor("<div data-custom=\"test\"><div data-custom-two=\"2\"></div></div>\n <div d|", nil)
	assertCompletionApplies(t, list, doc, "data-", "<div data-custom=\"test\"><div data-custom-two=\"2\"></div></div>\n <div data-$1=\"$2\"")
	assertCompletionApplies(t, list, doc, "data-custom", "<div data-custom=\"test\"><div data-custom-two=\"2\"></div></div>\n <div data-custom=\"$1\"")
	assertCompletionApplies(t, list, doc, "data-custom-two", "<div data-custom=\"test\"><div data-custom-two=\"2\"></div></div>\n <div data-custom-two=\"$1\"")

	list, doc = completionFor(`<body data-ng-app=""><div id="first" data-ng-include=" 'firstdoc.html' "></div><div id="second" inc|></div></body>`, nil)
	assertCompletionApplies(t, list, doc, "data-ng-include", `<body data-ng-app=""><div id="first" data-ng-include=" 'firstdoc.html' "></div><div id="second" data-ng-include="$1"></div></body>`)

	for _, input := range []string{`<div  |> </div >`, `<span  |> </span >`, `<input  |> </input >`} {
		list, _ = completionFor(input, nil)
		for _, label := range []string{"aria-activedescendant", "aria-atomic", "aria-valuetext"} {
			mustCompletion(t, list, label)
		}
	}

	list, doc = completionFor(`<LI></|`, nil)
	assertCompletionApplies(t, list, doc, "/LI", `<LI></LI>`)
	assertNoCompletion(t, list, "/li")

	list, doc = completionFor(`<lI></|`, nil)
	assertCompletionApplies(t, list, doc, "/lI", `<lI></lI>`)

	list, doc = completionFor(`<iNpUt |`, nil)
	assertCompletionApplies(t, list, doc, "type", `<iNpUt type="$1"`)

	list, doc = completionFor(`<INPUT TYPE=|`, nil)
	assertCompletionApplies(t, list, doc, "color", `<INPUT TYPE="color"`)

	hide := true
	list, _ = completionFor(`<body>
<|`, &CompletionConfiguration{HideEndTagSuggestions: &hide})
	assertNoCompletion(t, list, "/body")
	mustCompletion(t, list, "div")

	html5 := false
	list, _ = completionFor(`<body>
<|`, &CompletionConfiguration{HideEndTagSuggestions: &hide, HTML5: &html5})
	assertNoCompletion(t, list, "/body")
	assertNoCompletion(t, list, "div")

	list, _ = completionFor(`</|`, &CompletionConfiguration{HideEndTagSuggestions: &hide})
	assertNoCompletion(t, list, "/a")

	list, _ = completionFor(`<|`, &CompletionConfiguration{HTML5: &html5})
	assertNoCompletion(t, list, "div")
}

func TestCompletionAttributeDefaultValueBaselineParity(t *testing.T) {
	list, doc := completionFor(`<div clas|`, &CompletionConfiguration{AttributeDefaultValue: "doublequotes"})
	assertCompletionApplies(t, list, doc, "class", `<div class="$1"`)

	list, doc = completionFor(`<div clas|`, &CompletionConfiguration{AttributeDefaultValue: "singlequotes"})
	assertCompletionApplies(t, list, doc, "class", `<div class='$1'`)

	list, doc = completionFor(`<div clas|`, &CompletionConfiguration{AttributeDefaultValue: "empty"})
	assertCompletionApplies(t, list, doc, "class", `<div class=$1`)
}

func TestCompletionSettingsQuoteTagAndEntities(t *testing.T) {
	hide := true
	list, _ := completionFor(`<div></|>`, &CompletionConfiguration{HideEndTagSuggestions: &hide})
	if len(list.Items) != 0 {
		t.Fatalf("expected hidden end tag suggestions, got %#v", list.Items)
	}
	ls := GetLanguageService()
	doc := NewTextDocument("test://test/test.html", "html", 0, `<div>`)
	htmlDoc := ls.ParseHTMLDocument(doc)
	if got := ls.DoTagComplete(doc, doc.PositionAt(len(`<div>`)), htmlDoc); got == nil || *got != "$0</div>" {
		t.Fatalf("unexpected tag completion: %#v", got)
	}
	doc = NewTextDocument("test://test/test.html", "html", 0, `<input type=`)
	htmlDoc = ls.ParseHTMLDocument(doc)
	if got := ls.DoQuoteComplete(doc, doc.PositionAt(len(`<input type=`)), htmlDoc, nil); got == nil || *got != `"$1"` {
		t.Fatalf("unexpected quote completion: %#v", got)
	}
	list, doc = completionFor(`&|`, nil)
	assertCompletionApplies(t, list, doc, "&amp;", `&amp;`)
}

func TestCompletionQuoteAndTagCompleteBaselineParity(t *testing.T) {
	assertQuoteCompletion(t, `<a foo=|`, `"$1"`, nil)
	assertQuoteCompletion(t, `<a foo=|`, `'$1'`, &CompletionConfiguration{AttributeDefaultValue: "singlequotes"})
	assertQuoteCompletion(t, `<a foo=|`, "", &CompletionConfiguration{AttributeDefaultValue: "empty"})
	assertQuoteCompletion(t, `<a foo=|=`, "", nil)
	assertQuoteCompletion(t, `<a foo==|>`, "", nil)
	assertQuoteCompletion(t, `<a foo=|"bar"`, "", nil)
	assertQuoteCompletion(t, `<a foo=|></a>`, `"$1"`, nil)
	assertQuoteCompletion(t, `<a foo="bar=|"`, "", nil)
	assertQuoteCompletion(t, `<a baz=| foo="bar">`, `"$1"`, nil)
	assertQuoteCompletion(t, `<a>< foo=| /a>`, "", nil)
	assertQuoteCompletion(t, `<a></ foo=| a>`, "", nil)
	assertQuoteCompletion(t, "<a foo=\"bar\" \n baz=| ></a>", `"$1"`, nil)

	assertTagCompletion(t, `<div>|`, `$0</div>`)
	assertTagCompletion(t, `<div>|</div>`, "")
	assertTagCompletion(t, `<div class="">|`, `$0</div>`)
	assertTagCompletion(t, `<img>|`, "")
	assertTagCompletion(t, `<div title="</|">`, "")
	assertTagCompletion(t, `<div><br></|`, `div>`)
	assertTagCompletion(t, `<div><br><span></span></|`, `div>`)
	assertTagCompletion(t, `<div><h1><br><span></span><img></| </h1></div>`, `h1>`)
	assertTagCompletion(t, `<ng-template><td><ng-template></|   </td> </ng-template>`, `ng-template>`)
	assertTagCompletion(t, `<div><br></|>`, `div`)
}

func TestCompletionEntityBaselineParity(t *testing.T) {
	for _, tc := range []struct {
		input  string
		label  string
		result string
	}{
		{`<div>&|`, "&hookrightarrow;", `<div>&hookrightarrow;`},
		{`<div>&|`, "&plus;", `<div>&plus;`},
		{`<div>Hello&|`, "&ZeroWidthSpace;", `<div>Hello&ZeroWidthSpace;`},
		{`<div>Hello&gt|`, "&gtrdot;", `<div>Hello&gtrdot;`},
		{`<div class="&g|"`, "&grave;", `<div class="&grave;"`},
		{`<div class=&d|`, "&duarr;", `<div class=&duarr;`},
	} {
		list, doc := completionFor(tc.input, nil)
		assertCompletionApplies(t, list, doc, tc.label, tc.result)
	}

	list, _ := completionFor(`<div>&amp|</div>`, nil)
	item := mustCompletion(t, list, "&amp;")
	if item.Documentation != "Character entity representing '&'" {
		t.Fatalf("entity completion documentation: got %#v", item.Documentation)
	}
	list, _ = completionFor(`&|`, nil)
	assertCompletionOrder(t, list, []string{"&Aacute;", "&aacute;", "&Abreve;", "&abreve;", "&ac;"})

	list, _ = completionFor(`<div &d|`, nil)
	assertNoCompletion(t, list, "&duarr;")
	list, _ = completionFor(`<div&d|`, nil)
	assertNoCompletion(t, list, "&duarr;")
}

func TestCompletionUsesFullEntityData(t *testing.T) {
	list, doc := completionFor(`<div>&frac|</div>`, nil)
	assertCompletionApplies(t, list, doc, "&frac78;", `<div>&frac78;</div>`)
}

func TestCompletionDocumentationRespectsClientCapabilities(t *testing.T) {
	plainOnlyCapabilities := &ClientCapabilities{TextDocument: &TextDocumentClientCapabilities{
		Completion: &CompletionClientCapabilities{CompletionItem: &CompletionItemClientCapabilities{
			DocumentationFormat: []MarkupKind{MarkupKindPlainText},
		}},
	}}
	ls := GetLanguageService(LanguageServiceOptions{ClientCapabilities: plainOnlyCapabilities})
	doc := NewTextDocument("test://test/test.html", "html", 0, "<|")
	htmlDoc := ls.ParseHTMLDocument(doc)
	list := ls.DoComplete(doc, NewPosition(0, 1), htmlDoc, nil)
	item := mustCompletion(t, list, "div")
	content, ok := item.Documentation.(MarkupContent)
	if !ok {
		t.Fatalf("expected MarkupContent documentation, got %#v", item.Documentation)
	}
	if content.Kind != MarkupKindPlainText {
		t.Fatalf("completion documentation kind: got %q want %q", content.Kind, MarkupKindPlainText)
	}

	emptyCapabilitiesLS := GetLanguageService(LanguageServiceOptions{ClientCapabilities: &ClientCapabilities{}})
	doc = NewTextDocument("test://test/test.html", "html", 0, "<|")
	htmlDoc = emptyCapabilitiesLS.ParseHTMLDocument(doc)
	list = emptyCapabilitiesLS.DoComplete(doc, NewPosition(0, 1), htmlDoc, nil)
	item = mustCompletion(t, list, "div")
	content, ok = item.Documentation.(MarkupContent)
	if !ok {
		t.Fatalf("expected MarkupContent documentation for empty capabilities, got %#v", item.Documentation)
	}
	if content.Kind != MarkupKindPlainText {
		t.Fatalf("empty capabilities completion documentation kind: got %q want %q", content.Kind, MarkupKindPlainText)
	}
}

func TestCustomProviderParticipantAndPathCompletion(t *testing.T) {
	custom := NewHTMLDataProvider("custom", HTMLDataV1{
		Version: 1,
		Tags: []TagData{{
			Name:        "x-card",
			Description: "Custom card.",
			Attributes:  []AttributeData{{Name: "tone", Values: []ValueData{{Name: "soft"}}}},
		}},
	})
	useDefault := false
	ls := GetLanguageService(LanguageServiceOptions{
		UseDefaultDataProvider: &useDefault,
		CustomDataProviders:    []HTMLDataProvider{custom},
		FileSystemProvider:     fakeFS{},
	})
	marked := `<|`
	cursor := stringsIndex(marked, "|")
	doc := NewTextDocument("file:///site/index.html", "html", 0, stringsReplace(marked, "|", ""))
	htmlDoc := ls.ParseHTMLDocument(doc)
	list := ls.DoComplete(doc, doc.PositionAt(cursor), htmlDoc, nil)
	assertCompletionApplies(t, list, doc, "x-card", `<x-card`)

	participant := &recordingParticipant{}
	ls.SetCompletionParticipants([]CompletionParticipant{participant})
	marked = `<x-card tone="|">`
	cursor = stringsIndex(marked, "|")
	doc = NewTextDocument("file:///site/index.html", "html", 0, stringsReplace(marked, "|", ""))
	htmlDoc = ls.ParseHTMLDocument(doc)
	list = ls.DoComplete(doc, doc.PositionAt(cursor), htmlDoc, nil)
	if !participant.sawAttributeValue {
		t.Fatalf("expected attribute value participant to be called")
	}
	assertCompletionApplies(t, list, doc, "soft", `<x-card tone="soft">`)

	ls = GetLanguageService(LanguageServiceOptions{FileSystemProvider: fakeFS{}})
	marked = `<script src="|"></script>`
	cursor = stringsIndex(marked, "|")
	doc = NewTextDocument("file:///site/index.html", "html", 0, stringsReplace(marked, "|", ""))
	htmlDoc = ls.ParseHTMLDocument(doc)
	list = ls.DoComplete2(doc, doc.PositionAt(cursor), htmlDoc, identityContext{}, nil)
	assertCompletionApplies(t, list, doc, "app.js", `<script src="app.js"></script>`)
}

func TestPathCompletionMatchesForkSourceBehavior(t *testing.T) {
	list, doc := pathCompletionForFS(`<img src="|">`, priorityFS{})
	assertCompletionApplies(t, list, doc, "icon.png", `<img src="icon.png">`)
	assertCompletionApplies(t, list, doc, "app.js", `<img src="app.js">`)
	if item := mustCompletion(t, list, "icon.png"); item.SortText != "0_icon.png" {
		t.Fatalf("matching image sortText: got %q", item.SortText)
	}
	if item := mustCompletion(t, list, "app.js"); item.SortText != "1_app.js" {
		t.Fatalf("non-matching image sortText: got %q", item.SortText)
	}

	list, doc = pathCompletionForFS(`<script src="s|rc/app.js">`, priorityFS{})
	assertCompletionApplies(t, list, doc, "src/", `<script src="src/">`)

	list, _ = pathCompletionForFS(`<script src=".|">`, priorityFS{})
	if !list.IsIncomplete || len(list.Items) != 0 {
		t.Fatalf("dot path should be incomplete without items, got incomplete=%v items=%#v", list.IsIncomplete, list.Items)
	}

	list, _ = pathCompletionForFS(`<script src="|">`, statOnlyFS{})
	if list.IsIncomplete {
		t.Fatalf("stat-only file system should behave like doComplete without path items, got %#v", list)
	}
	assertNoCompletion(t, list, "app.js")
}

func TestCompletionEmptyListJSONMatchesForkSource(t *testing.T) {
	list, _ := completionFor(`<!-- | -->`, nil)
	got, err := json.Marshal(list)
	if err != nil {
		t.Fatal(err)
	}
	want := `{"isIncomplete":false,"items":[]}`
	if string(got) != want {
		t.Fatalf("empty completion JSON got %s want %s", got, want)
	}
}

func assertCompletionApplies(t *testing.T, list CompletionList, doc *TextDocument, label, expected string) {
	t.Helper()
	item := mustCompletion(t, list, label)
	if item.TextEdit == nil {
		t.Fatalf("%s has no text edit", label)
	}
	got := ApplyEdits(doc, []TextEdit{*item.TextEdit})
	if got != expected {
		t.Fatalf("%s applied text: got %q want %q", label, got, expected)
	}
}

func assertCompletionEdit(t *testing.T, list CompletionList, doc *TextDocument, label string, start, end int, newText, filterText string) {
	t.Helper()
	item := mustCompletion(t, list, label)
	if item.TextEdit == nil {
		t.Fatalf("%s has no text edit", label)
	}
	gotStart := doc.OffsetAt(item.TextEdit.Range.Start)
	gotEnd := doc.OffsetAt(item.TextEdit.Range.End)
	if gotStart != start || gotEnd != end || item.TextEdit.NewText != newText || item.FilterText != filterText {
		t.Fatalf("%s edit got range %d..%d newText %q filterText %q want range %d..%d newText %q filterText %q", label, gotStart, gotEnd, item.TextEdit.NewText, item.FilterText, start, end, newText, filterText)
	}
}

func assertNoCompletion(t *testing.T, list CompletionList, label string) {
	t.Helper()
	for _, item := range list.Items {
		if item.Label == label {
			t.Fatalf("completion %q should not be available in %#v", label, list.Items)
		}
	}
}

func assertCompletionOrder(t *testing.T, list CompletionList, labels []string) {
	t.Helper()
	next := 0
	for _, item := range list.Items {
		if next < len(labels) && item.Label == labels[next] {
			next++
		}
	}
	if next != len(labels) {
		t.Fatalf("completion order did not contain %v in order: %#v", labels, list.Items)
	}
}

func completionLabelCount(list CompletionList, label string) int {
	count := 0
	for _, item := range list.Items {
		if item.Label == label {
			count++
		}
	}
	return count
}

func assertUniqueCompletionLabels(t *testing.T, list CompletionList) {
	t.Helper()
	seen := map[string]bool{}
	for _, item := range list.Items {
		if seen[item.Label] {
			t.Fatalf("duplicate completion label %q in %#v", item.Label, list.Items)
		}
		seen[item.Label] = true
	}
}

func stringsIndex(s, sep string) int {
	for i := 0; i+len(sep) <= len(s); i++ {
		if s[i:i+len(sep)] == sep {
			return i
		}
	}
	return -1
}

func stringsReplace(s, old, new string) string {
	idx := stringsIndex(s, old)
	if idx < 0 {
		return s
	}
	return s[:idx] + new + s[idx+len(old):]
}

func assertQuoteCompletion(t *testing.T, marked, expected string, options *CompletionConfiguration) {
	t.Helper()
	offset := stringsIndex(marked, "|")
	text := stringsReplace(marked, "|", "")
	ls := GetLanguageService()
	doc := NewTextDocument("test://test/test.html", "html", 0, text)
	htmlDoc := ls.ParseHTMLDocument(doc)
	got := ls.DoQuoteComplete(doc, doc.PositionAt(offset), htmlDoc, options)
	if expected == "" {
		if got != nil {
			t.Fatalf("%s quote completion: got %#v want nil", marked, *got)
		}
		return
	}
	if got == nil || *got != expected {
		t.Fatalf("%s quote completion: got %#v want %q", marked, got, expected)
	}
}

func assertTagCompletion(t *testing.T, marked, expected string) {
	t.Helper()
	offset := stringsIndex(marked, "|")
	text := stringsReplace(marked, "|", "")
	ls := GetLanguageService()
	doc := NewTextDocument("test://test/test.html", "html", 0, text)
	htmlDoc := ls.ParseHTMLDocument(doc)
	got := ls.DoTagComplete(doc, doc.PositionAt(offset), htmlDoc)
	if expected == "" {
		if got != nil {
			t.Fatalf("%s tag completion: got %#v want nil", marked, *got)
		}
		return
	}
	if got == nil || *got != expected {
		gotText := "<nil>"
		if got != nil {
			gotText = *got
		}
		t.Fatalf("%s tag completion: got %q want %q", marked, gotText, expected)
	}
}

type recordingParticipant struct {
	sawAttributeValue bool
}

func (p *recordingParticipant) OnHTMLAttributeValue(context HtmlAttributeValueContext) {
	p.sawAttributeValue = true
}

func (p *recordingParticipant) OnHTMLContent(context HtmlContentContext) {}

type fakeFS struct{}

func (fakeFS) Stat(uri DocumentUri) (FileStat, error) {
	return FileStat{Type: FileTypeDirectory}, nil
}

func (fakeFS) ReadDirectory(uri DocumentUri) ([][2]any, error) {
	return [][2]any{
		{"app.js", FileTypeFile},
		{"style.css", FileTypeFile},
		{"img", FileTypeDirectory},
		{".secret.js", FileTypeFile},
	}, nil
}

type eventFS struct {
	events *[]string
}

func (fs eventFS) Stat(uri DocumentUri) (FileStat, error) {
	return FileStat{Type: FileTypeDirectory}, nil
}

func (fs eventFS) ReadDirectory(uri DocumentUri) ([][2]any, error) {
	if fs.events != nil {
		*fs.events = append(*fs.events, "fs")
	}
	return fakeFS{}.ReadDirectory(uri)
}

type nestedPathFS struct{}

func (nestedPathFS) Stat(uri DocumentUri) (FileStat, error) {
	return FileStat{Type: FileTypeDirectory}, nil
}

func (nestedPathFS) ReadDirectory(uri DocumentUri) ([][2]any, error) {
	return [][2]any{
		{"child.js", FileTypeFile},
		{"dir", FileTypeDirectory},
	}, nil
}

type numericFileTypeFS struct{}

func (numericFileTypeFS) Stat(uri DocumentUri) (FileStat, error) {
	return FileStat{Type: FileTypeDirectory}, nil
}

func (numericFileTypeFS) ReadDirectory(uri DocumentUri) ([][2]any, error) {
	return [][2]any{
		{"dir", 2},
		{"file.js", 1},
	}, nil
}

type priorityFS struct{}

func (priorityFS) Stat(uri DocumentUri) (FileStat, error) {
	return FileStat{Type: FileTypeDirectory}, nil
}

func (priorityFS) ReadDirectory(uri DocumentUri) ([][2]any, error) {
	return [][2]any{
		{"icon.png", FileTypeFile},
		{"app.js", FileTypeFile},
		{"src", FileTypeDirectory},
		{".secret.png", FileTypeFile},
	}, nil
}

type statOnlyFS struct{}

func (statOnlyFS) Stat(uri DocumentUri) (FileStat, error) {
	return FileStat{Type: FileTypeDirectory}, nil
}

func pathCompletionForFS(marked string, fs FileSystemProvider) (CompletionList, *TextDocument) {
	cursor := stringsIndex(marked, "|")
	text := stringsReplace(marked, "|", "")
	ls := GetLanguageService(LanguageServiceOptions{FileSystemProvider: fs})
	doc := NewTextDocument("file:///site/index.html", "html", 0, text)
	htmlDoc := ls.ParseHTMLDocument(doc)
	return ls.DoComplete2(doc, doc.PositionAt(cursor), htmlDoc, identityContext{}, nil), doc
}
