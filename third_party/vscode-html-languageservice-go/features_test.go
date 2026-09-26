package htmlservice

import "testing"

func TestSymbolsRenameLinkedEditingHighlightsAndMatchingTag(t *testing.T) {
	ls := GetLanguageService()
	doc := NewTextDocument("test://test.html", "html", 0, `<div id="app" class="foo bar"><span></span></div>`)
	htmlDoc := ls.ParseHTMLDocument(doc)
	symbols := ls.FindDocumentSymbols2(doc, htmlDoc)
	if len(symbols) != 1 || symbols[0].Name != "div#app.foo.bar" || len(symbols[0].Children) != 1 {
		t.Fatalf("unexpected symbols: %#v", symbols)
	}
	pos := doc.PositionAt(2)
	edit := ls.DoRename(doc, pos, "section", htmlDoc)
	if edit == nil || len(edit.Changes[doc.URI]) != 2 {
		t.Fatalf("expected rename edits, got %#v", edit)
	}
	ranges := ls.FindLinkedEditingRanges(doc, pos, htmlDoc)
	if len(ranges) != 2 {
		t.Fatalf("expected linked ranges, got %#v", ranges)
	}
	highlights := ls.FindDocumentHighlights(doc, pos, htmlDoc)
	if len(highlights) != 2 {
		t.Fatalf("expected start/end highlights, got %#v", highlights)
	}
	match := ls.FindMatchingTagPosition(doc, pos, htmlDoc)
	if match == nil || match.Line != 0 || match.Character <= pos.Character {
		t.Fatalf("unexpected matching position: %#v", match)
	}
}

func TestFoldingSelectionRangesLinksAndHover(t *testing.T) {
	ls := GetLanguageService()
	text := "<div>\n  <!-- hi\n  there -->\n  <a id=\"target\" href=\"#target\">x</a>\n</div>"
	doc := NewTextDocument("file:///tmp/test.html", "html", 0, text)
	htmlDoc := ls.ParseHTMLDocument(doc)
	folds := ls.GetFoldingRanges(doc)
	if len(folds) < 2 {
		t.Fatalf("expected html and comment folds, got %#v", folds)
	}
	selections := ls.GetSelectionRanges(doc, []Position{doc.PositionAt(stringsIndex(text, "target"))})
	if len(selections) != 1 || selections[0].Parent == nil {
		t.Fatalf("expected nested selection range, got %#v", selections)
	}
	links := ls.FindDocumentLinks(doc, identityContext{})
	if len(links) != 1 || links[0].Target != "file:///tmp/test.html#4,9" {
		t.Fatalf("unexpected links: %#v", links)
	}
	hover := ls.DoHover(doc, doc.PositionAt(1), htmlDoc, nil)
	if hover == nil {
		t.Fatalf("expected hover")
	}
}

type identityContext struct{}

func (identityContext) ResolveReference(ref, base string) (string, bool) {
	if stringsIndex(ref, "#") == 0 {
		return base + ref, true
	}
	if stringsIndex(ref, "://") >= 0 {
		return ref, true
	}
	return base + "/" + ref, true
}
