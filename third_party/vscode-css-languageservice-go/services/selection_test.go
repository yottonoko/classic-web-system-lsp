package services

import (
	"reflect"
	"testing"

	"github.com/yottonoko/vscode-css-languageservice-go/lsp"
)

func TestSelectionRangeBasic(t *testing.T) {
	t.Run("Basic", func(t *testing.T) {
		for _, input := range []string{".foo { |color: blue; }", ".foo { c|olor: blue; }", ".foo { color|: blue; }"} {
			assertSelectionRanges(t, input, []offsetText{
				{7, "color"},
				{7, "color: blue"},
				{6, " color: blue; "},
				{5, "{ color: blue; }"},
				{0, ".foo { color: blue; }"},
			})
		}
		for _, input := range []string{".foo { color: |blue; }", ".foo { color: b|lue; }", ".foo { color: blue|; }"} {
			assertSelectionRanges(t, input, []offsetText{
				{14, "blue"},
				{7, "color: blue"},
				{6, " color: blue; "},
				{5, "{ color: blue; }"},
				{0, ".foo { color: blue; }"},
			})
		}
		for _, input := range []string{".|foo { color: blue; }", ".fo|o { color: blue; }", ".foo| { color: blue; }"} {
			assertSelectionRanges(t, input, []offsetText{
				{1, "foo"},
				{0, ".foo"},
				{0, ".foo { color: blue; }"},
			})
		}
	})
}

func TestSelectionRangeMultipleValues(t *testing.T) {
	t.Run("Multiple values", func(t *testing.T) {
		assertSelectionRanges(t, ".foo { font-family: '|Courier New', Courier, monospace; }", []offsetText{
			{20, "'Courier New'"},
			{20, "'Courier New', Courier, monospace"},
			{7, "font-family: 'Courier New', Courier, monospace"},
			{6, " font-family: 'Courier New', Courier, monospace; "},
			{5, "{ font-family: 'Courier New', Courier, monospace; }"},
			{0, ".foo { font-family: 'Courier New', Courier, monospace; }"},
		})
	})
}

func TestSelectionRangeDeclarationEdges(t *testing.T) {
	t.Run("Edge behavior for Declaration", func(t *testing.T) {
		assertSelectionRanges(t, ".foo |{ }", []offsetText{
			{5, "{ }"},
			{0, ".foo { }"},
		})
		assertSelectionRanges(t, ".foo { }|", []offsetText{
			{5, "{ }"},
			{0, ".foo { }"},
		})
		assertSelectionRanges(t, ".foo {| }", []offsetText{
			{6, " "},
			{5, "{ }"},
			{0, ".foo { }"},
		})
		assertSelectionRanges(t, ".foo { | }", []offsetText{
			{6, "  "},
			{5, "{  }"},
			{0, ".foo {  }"},
		})
		assertSelectionRanges(t, ".foo { |}", []offsetText{
			{6, " "},
			{5, "{ }"},
			{0, ".foo { }"},
		})
	})
}

func assertSelectionRanges(t *testing.T, markedContent string, expected []offsetText) {
	t.Helper()
	offset := stringsIndex(markedContent, "|")
	content := markedContent[:offset] + markedContent[offset+1:]
	document := lsp.NewTextDocument("test://foo/bar.css", "css", 1, content)
	actualRanges := GetSelectionRanges(document, []lsp.Position{document.PositionAt(offset)})
	if len(actualRanges) != 1 {
		t.Fatalf("selection range count = %d", len(actualRanges))
	}
	actual := flattenSelectionRange(document, actualRanges[0])
	if !reflect.DeepEqual(actual, expected) {
		t.Fatalf("%s\nactual: %#v\nwant:   %#v", content, actual, expected)
	}
}

type offsetText struct {
	Offset int
	Text   string
}

func flattenSelectionRange(document *lsp.TextDocument, selection lsp.SelectionRange) []offsetText {
	var result []offsetText
	current := &selection
	for current != nil {
		result = append(result, offsetText{
			Offset: document.OffsetAt(current.Range.Start),
			Text:   document.GetText(&current.Range),
		})
		current = current.Parent
	}
	return result
}

func stringsIndex(text, needle string) int {
	for i := 0; i+len(needle) <= len(text); i++ {
		if text[i:i+len(needle)] == needle {
			return i
		}
	}
	return -1
}
