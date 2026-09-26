package htmlservice

import (
	"encoding/json"
	"reflect"
	"testing"
)

func applyRenameForTest(t *testing.T, value, newName string) (string, bool) {
	t.Helper()
	offset := stringsIndex(value, "|")
	value = stringsReplace(value, "|", "")
	ls := GetLanguageService()
	doc := NewTextDocument("test://test/test.html", "html", 0, value)
	htmlDoc := ls.ParseHTMLDocument(doc)
	edit := ls.DoRename(doc, doc.PositionAt(offset), newName, htmlDoc)
	if edit == nil || edit.Changes == nil {
		return value, false
	}
	return ApplyEdits(doc, edit.Changes[doc.URI]), true
}

func TestRenameBaselineParity(t *testing.T) {
	for _, value := range []string{"<|div></div>", "<d|iv></div>", "<di|v></div>", "<div|></div>"} {
		if got, ok := applyRenameForTest(t, value, "h1"); !ok || got != "<h1></h1>" {
			t.Fatalf("rename %q got %q ok %v", value, got, ok)
		}
	}
	for _, value := range []string{
		"|<div></div>", "<div>|</div>", "<div><|/div>", "<div></div>|",
		`<div |id="foo"></div>`, `<div i|d="foo"></div>`, `<div id|="foo"></div>`,
		`<div id=|"foo"></div>`, `<div id="|foo"></div>`, `<div id="f|oo"></div>`,
		`<div id="fo|o"></div>`, `<div id="foo|"></div>`, `<div id="foo"|></div>`,
	} {
		if _, ok := applyRenameForTest(t, value, "h1"); ok {
			t.Fatalf("expected no rename for %q", value)
		}
	}
	for _, tc := range []struct{ value, want string }{
		{"<|br>", "<h1>"}, {"<|br/>", "<h1/>"}, {"<|br />", "<h1 />"},
		{"<div><|h1></h1></div>", "<div><h2></h2></div>"},
		{"<div><|h1></div>", "<div><h2></div>"},
		{"<|div><h1></h1></div>", "<span><h1></h1></span>"},
	} {
		name := "h1"
		if tc.want == "<div><h2></h2></div>" || tc.want == "<div><h2></div>" {
			name = "h2"
		}
		if tc.want == "<span><h1></h1></span>" {
			name = "span"
		}
		if got, ok := applyRenameForTest(t, tc.value, name); !ok || got != tc.want {
			t.Fatalf("rename %q got %q want %q ok %v", tc.value, got, tc.want, ok)
		}
	}
}

func linkedRangesForTest(value string) [][2]any {
	offset := stringsIndex(value, "|")
	value = stringsReplace(value, "|", "")
	ls := GetLanguageService()
	doc := NewTextDocument("test://test/test.html", "html", 0, value)
	htmlDoc := ls.ParseHTMLDocument(doc)
	ranges := ls.FindLinkedEditingRanges(doc, doc.PositionAt(offset), htmlDoc)
	var actual [][2]any
	for _, r := range ranges {
		actual = append(actual, [2]any{doc.OffsetAt(r.Start), doc.GetText(r)})
	}
	return actual
}

func TestLinkedEditingBaselineParity(t *testing.T) {
	cases := []struct {
		value string
		want  [][2]any
	}{
		{"|<div></div>", nil}, {"<|div></div>", [][2]any{{1, "div"}, {7, "div"}}},
		{"<d|iv></div>", [][2]any{{1, "div"}, {7, "div"}}}, {"<di|v></div>", [][2]any{{1, "div"}, {7, "div"}}},
		{"<div|></div>", [][2]any{{1, "div"}, {7, "div"}}}, {"<div>|</div>", nil},
		{"<div><|/div>", nil}, {"<div></|div>", [][2]any{{1, "div"}, {7, "div"}}},
		{"<div></d|iv>", [][2]any{{1, "div"}, {7, "div"}}}, {"<div></di|v>", [][2]any{{1, "div"}, {7, "div"}}},
		{"<div></div|>", [][2]any{{1, "div"}, {7, "div"}}},
		{"<div></div>|", nil}, {"<div><div|</div>", nil}, {"<div><div><div|</div></div>", nil},
		{"<div| ></div>", [][2]any{{1, "div"}, {8, "div"}}},
		{`<div| id="foo"></div>`, [][2]any{{1, "div"}, {16, "div"}}},
		{"<|></>", [][2]any{{1, ""}, {4, ""}}}, {"<><div></div></|>", [][2]any{{1, ""}, {15, ""}}},
	}
	for _, tc := range cases {
		if got := linkedRangesForTest(tc.value); !reflect.DeepEqual(got, tc.want) {
			t.Fatalf("linked %q got %#v want %#v", tc.value, got, tc.want)
		}
	}
}

func TestMatchingTagPositionBaselineParity(t *testing.T) {
	ls := GetLanguageService()
	doc := NewTextDocument("test://test/test.html", "html", 0, "<></>")
	htmlDoc := ls.ParseHTMLDocument(doc)
	if pos := ls.FindMatchingTagPosition(doc, doc.PositionAt(1), htmlDoc); pos != nil {
		t.Fatalf("empty tag matching position got %#v want nil", pos)
	}
	for _, value := range []string{
		"<|div></$div>", "<d|iv></d$iv>", "<di|v></di$v>", "<div|></div$>",
		"<$div></|div>", "<d$iv></d|iv>", "<di$v></di|v>", "<div$></div|>",
		"<div| ></div$>", `<div| id="foo"></div$>`, "<div$ ></div|>", `<div$ id="foo"></div|>`,
	} {
		offset := stringsIndex(value, "|")
		value = stringsReplace(value, "|", "")
		mirrorOffset := stringsIndex(value, "$")
		value = stringsReplace(value, "$", "")
		if mirrorOffset < offset {
			offset--
		}
		doc := NewTextDocument("test://test/test.html", "html", 0, value)
		htmlDoc := ls.ParseHTMLDocument(doc)
		pos := ls.FindMatchingTagPosition(doc, doc.PositionAt(offset), htmlDoc)
		if pos == nil || doc.OffsetAt(*pos) != mirrorOffset {
			t.Fatalf("matching %q got %#v want offset %d", value, pos, mirrorOffset)
		}
	}
}

func TestSymbolsBaselineParity(t *testing.T) {
	ls := GetLanguageService()
	for _, tc := range []struct {
		content string
		info    []SymbolInformation
		tree    []DocumentSymbol
	}{
		{
			"<div></div>",
			[]SymbolInformation{symbolInfo("div", "", 0, 11)},
			[]DocumentSymbol{docSymbol("div", 0, 11)},
		},
		{
			`<div><input checked id="test" class="checkbox"></div>`,
			[]SymbolInformation{symbolInfo("div", "", 0, 53), symbolInfo("input#test.checkbox", "div", 5, 47)},
			[]DocumentSymbol{docSymbol("div", 0, 53, docSymbol("input#test.checkbox", 5, 47))},
		},
		{
			`<html id='root'><body id="Foo" class="bar"><div class="a b"></div></body></html>`,
			[]SymbolInformation{symbolInfo("html#root", "", 0, 80), symbolInfo("body#Foo.bar", "html#root", 16, 73), symbolInfo("div.a.b", "body#Foo.bar", 43, 66)},
			[]DocumentSymbol{docSymbol("html#root", 0, 80, docSymbol("body#Foo.bar", 16, 73, docSymbol("div.a.b", 43, 66)))},
		},
		{
			`<html><br id="Foo"><br id=Bar></html>`,
			[]SymbolInformation{symbolInfo("html", "", 0, 37), symbolInfo("br#Foo", "html", 6, 19), symbolInfo("br#Bar", "html", 19, 30)},
			[]DocumentSymbol{docSymbol("html", 0, 37, docSymbol("br#Foo", 6, 19), docSymbol("br#Bar", 19, 30))},
		},
		{
			`<html><body><div></div></body></html>`,
			[]SymbolInformation{symbolInfo("html", "", 0, 37), symbolInfo("body", "html", 6, 30), symbolInfo("div", "body", 12, 23)},
			[]DocumentSymbol{docSymbol("html", 0, 37, docSymbol("body", 6, 30, docSymbol("div", 12, 23)))},
		},
		{
			`<div class=""></div>`,
			[]SymbolInformation{symbolInfo("div.", "", 0, 20)},
			[]DocumentSymbol{docSymbol("div.", 0, 20)},
		},
		{
			`<div class=" a b "></div>`,
			[]SymbolInformation{symbolInfo("div..a.b.", "", 0, 25)},
			[]DocumentSymbol{docSymbol("div..a.b.", 0, 25)},
		},
	} {
		doc := NewTextDocument("test://test/test.html", "html", 0, tc.content)
		htmlDoc := ls.ParseHTMLDocument(doc)
		if symbols := ls.FindDocumentSymbols(doc, htmlDoc); !reflect.DeepEqual(symbols, tc.info) {
			t.Fatalf("symbol info for %q\n got: %#v\nwant: %#v", tc.content, symbols, tc.info)
		}
		if symbols := ls.FindDocumentSymbols2(doc, htmlDoc); !reflect.DeepEqual(symbols, tc.tree) {
			t.Fatalf("document symbols for %q\n got: %#v\nwant: %#v", tc.content, symbols, tc.tree)
		}
	}
	for _, content := range []string{"<div class=\"a\vb\"></div>", "<div class=\"a\u00a0b\"></div>"} {
		doc := NewTextDocument("test://test/test.html", "html", 0, content)
		htmlDoc := ls.ParseHTMLDocument(doc)
		symbols := ls.FindDocumentSymbols2(doc, htmlDoc)
		if len(symbols) != 1 || symbols[0].Name != "div.a.b" {
			t.Fatalf("JS whitespace class symbol for %q got %#v want div.a.b", content, symbols)
		}
	}
	doc := NewTextDocument("test://test/test.html", "html", 0, "<div class=\"a\u0085b\"></div>")
	htmlDoc := ls.ParseHTMLDocument(doc)
	symbols := ls.FindDocumentSymbols2(doc, htmlDoc)
	if len(symbols) != 1 || symbols[0].Name != "div.a\u0085b" {
		t.Fatalf("non-JS whitespace class symbol got %#v want div.a\\u0085b", symbols)
	}
	emptyDoc := NewTextDocument("test://test/test.html", "html", 0, "<!-- foo -->")
	emptyHTML := ls.ParseHTMLDocument(emptyDoc)
	if data, err := json.Marshal(ls.FindDocumentSymbols(emptyDoc, emptyHTML)); err != nil || string(data) != "[]" {
		t.Fatalf("empty flat symbols json got %q err=%v want []", data, err)
	}
	if data, err := json.Marshal(ls.FindDocumentSymbols2(emptyDoc, emptyHTML)); err != nil || string(data) != "[]" {
		t.Fatalf("empty tree symbols json got %q err=%v want []", data, err)
	}
	flatDoc := NewTextDocument("file:///test/data/abc/test.html", "html", 0, "<div></div>")
	flatHTML := ls.ParseHTMLDocument(flatDoc)
	data, err := json.Marshal(ls.FindDocumentSymbols(flatDoc, flatHTML))
	if err != nil {
		t.Fatal(err)
	}
	if want := `[{"name":"div","kind":8,"location":{"uri":"file:///test/data/abc/test.html","range":{"start":{"line":0,"character":0},"end":{"line":0,"character":11}}},"containerName":""}]`; string(data) != want {
		t.Fatalf("flat symbols json got %s want %s", data, want)
	}
}

func symbolInfo(name, container string, start, end int) SymbolInformation {
	return SymbolInformation{
		Name:          name,
		Kind:          SymbolKindField,
		ContainerName: container,
		Location: Location{
			URI:   "test://test/test.html",
			Range: NewRange(NewPosition(0, start), NewPosition(0, end)),
		},
	}
}

func docSymbol(name string, start, end int, children ...DocumentSymbol) DocumentSymbol {
	return DocumentSymbol{
		Name:           name,
		Kind:           SymbolKindField,
		Range:          NewRange(NewPosition(0, start), NewPosition(0, end)),
		SelectionRange: NewRange(NewPosition(0, start), NewPosition(0, end)),
		Children:       children,
	}
}
