package lsp

import "testing"

func TestTextDocumentUTF16PositionConversions(t *testing.T) {
	document := NewTextDocument("test://test/test.css", "css", 0, "a\né💜b\nc")

	if got := document.PositionAt(5); got != (Position{Line: 1, Character: 3}) {
		t.Fatalf("PositionAt(5) = %#v, want line 1 character 3", got)
	}
	if got := document.OffsetAt(Position{Line: 1, Character: 3}); got != 5 {
		t.Fatalf("OffsetAt(line 1 character 3) = %d, want 5", got)
	}
	if got := document.GetText(&Range{Start: Position{Line: 1, Character: 0}, End: Position{Line: 1, Character: 3}}); got != "é💜" {
		t.Fatalf("GetText unicode range = %q, want %q", got, "é💜")
	}
	if got := document.PositionAtByteOffset(len("a\né💜")); got != (Position{Line: 1, Character: 3}) {
		t.Fatalf("PositionAtByteOffset(after unicode) = %#v, want line 1 character 3", got)
	}
	if got := document.ByteOffsetAt(Position{Line: 1, Character: 3}); got != len("a\né💜") {
		t.Fatalf("ByteOffsetAt(line 1 character 3) = %d, want %d", got, len("a\né💜"))
	}
}

func TestApplyEditsUsesUTF16Ranges(t *testing.T) {
	document := NewTextDocument("test://test/test.css", "css", 0, ".💜 { color: red; }")
	edit := Replace(Range{Start: Position{Line: 0, Character: 1}, End: Position{Line: 0, Character: 3}}, ".card")

	if got := ApplyEdits(document, []TextEdit{edit}); got != "..card { color: red; }" {
		t.Fatalf("ApplyEdits unicode range = %q", got)
	}
}
