package htmlservice

import (
	"reflect"
	"sort"
	"testing"
)

type expectedFold struct {
	StartLine int
	EndLine   int
	Kind      FoldingRangeKind
}

func assertFoldingRanges(t *testing.T, lines []string, expected []expectedFold, limit int) {
	t.Helper()
	doc := NewTextDocument("test://foo/bar.html", "html", 1, stringsJoin(lines, "\n"))
	ls := GetLanguageService()
	actualRanges := ls.GetFoldingRanges(doc, limit)
	var actual []expectedFold
	for _, r := range actualRanges {
		actual = append(actual, expectedFold{StartLine: r.StartLine, EndLine: r.EndLine, Kind: r.Kind})
	}
	sort.Slice(actual, func(i, j int) bool { return actual[i].StartLine < actual[j].StartLine })
	if !reflect.DeepEqual(actual, expected) {
		t.Fatalf("folding\n got: %#v\nwant: %#v", actual, expected)
	}
}

func fold(start, end int, kind ...FoldingRangeKind) expectedFold {
	var k FoldingRangeKind
	if len(kind) > 0 {
		k = kind[0]
	}
	return expectedFold{StartLine: start, EndLine: end, Kind: k}
}

func TestFoldingBaselineParity(t *testing.T) {
	assertFoldingRanges(t, []string{"<html>", "Hello", "</html>"}, []expectedFold{fold(0, 1)}, 0)
	assertFoldingRanges(t, []string{"<html>", "<head>", "Hello", "</head>", "</html>"}, []expectedFold{fold(0, 3), fold(1, 2)}, 0)
	assertFoldingRanges(t, []string{"<html>", "<head>", "Head", "</head>", `<body class="f">`, "Body", "</body>", "</html>"}, []expectedFold{fold(0, 6), fold(1, 2), fold(4, 5)}, 0)
	assertFoldingRanges(t, []string{"<div>", `<a href="top"/>`, `<img src="s">`, "<br/>", "<br>", `<img class="c"`, `     src="top"`, ">", "</div>"}, []expectedFold{fold(0, 7), fold(5, 6)}, 0)
	assertFoldingRanges(t, []string{"<!--", " multi line", "-->", "<!-- some stuff", " some more stuff -->"}, []expectedFold{fold(0, 2, FoldingRangeKindComment), fold(3, 4, FoldingRangeKindComment)}, 0)
	assertFoldingRanges(t, []string{"<!-- #region -->", "<!-- #region -->", "<!-- #endregion -->", "<!-- #endregion -->"}, []expectedFold{fold(0, 3, FoldingRangeKindRegion), fold(1, 2, FoldingRangeKindRegion)}, 0)
	assertFoldingRanges(t, []string{"<!-- \u00a0#region -->", "text", "<!-- #endregion -->"}, []expectedFold{fold(0, 2, FoldingRangeKindRegion)}, 0)
	assertFoldingRanges(t, []string{"<!-- \v#region -->", "text", "<!-- #endregion -->"}, []expectedFold{fold(0, 2, FoldingRangeKindRegion)}, 0)
	assertFoldingRanges(t, []string{"<!-- \u0085#region -->", "text", "<!-- #endregion -->"}, nil, 0)
	assertFoldingRanges(t, []string{"<!-- #regional -->", "<!-- #endregion -->"}, nil, 0)
	assertFoldingRanges(t, []string{"<!-- #region -->", "<!-- not #endregion -->"}, []expectedFold{fold(0, 1, FoldingRangeKindRegion)}, 0)
	assertFoldingRanges(t, []string{"<body>", "<div></div>", "Hello", "</div>", "</body>"}, []expectedFold{fold(0, 3)}, 0)
	assertFoldingRanges(t, []string{"<be><div>", "<!-- #endregion -->", "</div>"}, []expectedFold{fold(0, 1)}, 0)
	assertFoldingRanges(t, []string{"<body>", "<!-- #region -->", "Hello", "<div></div>", "</body>", "<!-- #endregion -->"}, []expectedFold{fold(0, 3)}, 0)
	assertFoldingRanges(t, []string{"<!-- #region -->", "<body>", "Hello", "<!-- #endregion -->", "<div></div>", "</body>"}, []expectedFold{fold(0, 3, FoldingRangeKindRegion)}, 0)
}

func TestFoldingBaselineLimitParity(t *testing.T) {
	input := []string{"<div>", " <span>", "  <b>", "  ", "  </b>,", "  <b>", "   <pre>", "  ", "   </pre>,", "   <pre>", "  ", "   </pre>,", "  </b>,", "  <b>", "  ", "  </b>,", "  <b>", "  ", "  </b>", " </span>", "</div>"}
	assertFoldingRanges(t, input, []expectedFold{fold(0, 19), fold(1, 18), fold(2, 3), fold(5, 11), fold(6, 7), fold(9, 10), fold(13, 14), fold(16, 17)}, 0)
	assertFoldingRanges(t, input, nil, -1)
	assertFoldingRanges(t, input, []expectedFold{fold(0, 19), fold(1, 18), fold(2, 3), fold(5, 11), fold(6, 7), fold(9, 10), fold(13, 14), fold(16, 17)}, 8)
	assertFoldingRanges(t, input, []expectedFold{fold(0, 19), fold(1, 18), fold(2, 3), fold(5, 11), fold(6, 7), fold(13, 14), fold(16, 17)}, 7)
	assertFoldingRanges(t, input, []expectedFold{fold(0, 19), fold(1, 18), fold(2, 3), fold(5, 11), fold(13, 14), fold(16, 17)}, 6)
	assertFoldingRanges(t, input, []expectedFold{fold(0, 19), fold(1, 18), fold(2, 3), fold(5, 11), fold(13, 14)}, 5)
	assertFoldingRanges(t, input, []expectedFold{fold(0, 19), fold(1, 18), fold(2, 3), fold(5, 11)}, 4)
	assertFoldingRanges(t, input, []expectedFold{fold(0, 19), fold(1, 18), fold(2, 3)}, 3)
	assertFoldingRanges(t, input, []expectedFold{fold(0, 19), fold(1, 18)}, 2)
	assertFoldingRanges(t, input, []expectedFold{fold(0, 19)}, 1)
}

func assertSelectionOffsets(t *testing.T, content string, expected [][2]any) {
	t.Helper()
	offset := stringsIndex(content, "|")
	content = stringsReplace(content, "|", "")
	ls := GetLanguageService()
	doc := NewTextDocument("test://foo.html", "html", 1, content)
	actualRanges := ls.GetSelectionRanges(doc, []Position{doc.PositionAt(offset)})
	if len(actualRanges) != 1 {
		t.Fatalf("expected one selection range")
	}
	var actual [][2]any
	current := &actualRanges[0]
	for current != nil {
		actual = append(actual, [2]any{doc.OffsetAt(current.Range.Start), doc.GetText(current.Range)})
		current = current.Parent
	}
	if !reflect.DeepEqual(actual, expected) {
		t.Fatalf("%s selection\n got: %#v\nwant: %#v", content, actual, expected)
	}
}

func TestSelectionRangeBaselineParity(t *testing.T) {
	assertSelectionOffsets(t, `<div|>foo</div>`, [][2]any{{1, "div"}, {0, "<div>foo</div>"}})
	assertSelectionOffsets(t, `<|div>foo</div>`, [][2]any{{1, "div"}, {0, "<div>foo</div>"}})
	assertSelectionOffsets(t, `<d|iv>foo</div>`, [][2]any{{1, "div"}, {0, "<div>foo</div>"}})
	assertSelectionOffsets(t, `<div>|foo</div>`, [][2]any{{5, "foo"}, {0, "<div>foo</div>"}})
	assertSelectionOffsets(t, `<div>f|oo</div>`, [][2]any{{5, "foo"}, {0, "<div>foo</div>"}})
	assertSelectionOffsets(t, `<div>foo|</div>`, [][2]any{{5, "foo"}, {0, "<div>foo</div>"}})
	assertSelectionOffsets(t, `<div>foo<|/div>`, [][2]any{{0, "<div>foo</div>"}})
	assertSelectionOffsets(t, `<div>foo</|div>`, [][2]any{{10, "div"}, {0, "<div>foo</div>"}})
	assertSelectionOffsets(t, `<div>foo</di|v>`, [][2]any{{10, "div"}, {0, "<div>foo</div>"}})
	assertSelectionOffsets(t, `<div>foo</div|>`, [][2]any{{10, "div"}, {0, "<div>foo</div>"}})
	assertSelectionOffsets(t, `<div |class="foo">foo</div>`, [][2]any{{5, "class"}, {5, `class="foo"`}, {1, `div class="foo"`}, {0, `<div class="foo">foo</div>`}})
	assertSelectionOffsets(t, `<div cl|ass="foo">foo</div>`, [][2]any{{5, "class"}, {5, `class="foo"`}, {1, `div class="foo"`}, {0, `<div class="foo">foo</div>`}})
	assertSelectionOffsets(t, `<div class|="foo">foo</div>`, [][2]any{{5, "class"}, {5, `class="foo"`}, {1, `div class="foo"`}, {0, `<div class="foo">foo</div>`}})
	assertSelectionOffsets(t, `<div class=|"foo">foo</div>`, [][2]any{{11, `"foo"`}, {5, `class="foo"`}, {1, `div class="foo"`}, {0, `<div class="foo">foo</div>`}})
	assertSelectionOffsets(t, `<div class="foo"|>foo</div>`, [][2]any{{11, `"foo"`}, {5, `class="foo"`}, {1, `div class="foo"`}, {0, `<div class="foo">foo</div>`}})
	assertSelectionOffsets(t, `<div class="|foo">foo</div>`, [][2]any{{12, "foo"}, {11, `"foo"`}, {5, `class="foo"`}, {1, `div class="foo"`}, {0, `<div class="foo">foo</div>`}})
	assertSelectionOffsets(t, `<div class="f|oo">foo</div>`, [][2]any{{12, "foo"}, {11, `"foo"`}, {5, `class="foo"`}, {1, `div class="foo"`}, {0, `<div class="foo">foo</div>`}})
	assertSelectionOffsets(t, `<div class=|foo>foo</div>`, [][2]any{{11, "foo"}, {5, "class=foo"}, {1, "div class=foo"}, {0, "<div class=foo>foo</div>"}})
	assertSelectionOffsets(t, `<div class="foo" id="|bar">foo</div>`, [][2]any{{21, "bar"}, {20, `"bar"`}, {17, `id="bar"`}, {1, `div class="foo" id="bar"`}, {0, `<div class="foo" id="bar">foo</div>`}})
	assertSelectionOffsets(t, `<br class="|foo"/>`, [][2]any{{11, "foo"}, {10, `"foo"`}, {4, `class="foo"`}, {1, `br class="foo"`}, {0, `<br class="foo"/>`}})
	assertSelectionOffsets(t, `<b|r class="foo"/>`, [][2]any{{1, `br class="foo"`}, {0, `<br class="foo"/>`}})
	assertSelectionOffsets(t, `<div><div>|foo</div></div>`, [][2]any{{10, "foo"}, {5, "<div>foo</div>"}, {0, "<div><div>foo</div></div>"}})
	assertSelectionOffsets(t, "<div>\n<p>|foo</p>\n</div>", [][2]any{{9, "foo"}, {6, "<p>foo</p>"}, {5, "\n<p>foo</p>\n"}, {0, "<div>\n<p>foo</p>\n</div>"}})
	assertSelectionOffsets(t, `<meta charset='|UTF-8'>`, [][2]any{{15, "UTF-8"}, {14, "'UTF-8'"}, {6, "charset='UTF-8'"}, {1, "meta charset='UTF-8'"}, {0, "<meta charset='UTF-8'>"}})
	assertSelectionOffsets(t, `<meta c|harset='UTF-8'>`, [][2]any{{6, "charset"}, {6, "charset='UTF-8'"}, {1, "meta charset='UTF-8'"}, {0, "<meta charset='UTF-8'>"}})
	assertSelectionOffsets(t, `<html><meta c|harset='UTF-8'></html>`, [][2]any{{12, "charset"}, {12, "charset='UTF-8'"}, {7, "meta charset='UTF-8'"}, {6, "<meta charset='UTF-8'>"}, {0, "<html><meta charset='UTF-8'></html>"}})
	assertSelectionOffsets(t, `<div></div|1>`, [][2]any{{0, "<div></div1>"}})
	assertSelectionOffsets(t, `<!-- f|oo -->`, [][2]any{{6, ""}})
	assertSelectionOffsets(t, `<!DOCTYPE h|tml>`, [][2]any{{11, ""}})
}

func stringsJoin(values []string, sep string) string {
	if len(values) == 0 {
		return ""
	}
	out := values[0]
	for _, v := range values[1:] {
		out += sep + v
	}
	return out
}
