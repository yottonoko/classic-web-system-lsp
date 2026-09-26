package htmlservice

import (
	"strings"
	"testing"
)

func TestTextDocumentPositionsUseUTF16Characters(t *testing.T) {
	text := "a😀b\n日本<div></div>"
	doc := NewTextDocument("test://test.html", "html", 0, text)

	emojiOffset := strings.Index(text, "😀")
	bOffset := strings.Index(text, "b")
	divOffset := strings.Index(text, "<div>")

	if got := doc.PositionAt(emojiOffset); got != NewPosition(0, 1) {
		t.Fatalf("emoji position got %#v want %#v", got, NewPosition(0, 1))
	}
	if got := doc.PositionAt(bOffset); got != NewPosition(0, 3) {
		t.Fatalf("post-emoji position got %#v want %#v", got, NewPosition(0, 3))
	}
	if got := doc.PositionAt(divOffset); got != NewPosition(1, 2) {
		t.Fatalf("post-BMP position got %#v want %#v", got, NewPosition(1, 2))
	}
	if got := doc.OffsetAt(NewPosition(0, 1)); got != emojiOffset {
		t.Fatalf("emoji offset got %d want %d", got, emojiOffset)
	}
	if got := doc.OffsetAt(NewPosition(0, 3)); got != bOffset {
		t.Fatalf("post-emoji offset got %d want %d", got, bOffset)
	}
	if got := doc.OffsetAt(NewPosition(1, 2)); got != divOffset {
		t.Fatalf("post-BMP offset got %d want %d", got, divOffset)
	}
	if got := doc.GetText(NewRange(NewPosition(0, 1), NewPosition(0, 3))); got != "😀" {
		t.Fatalf("UTF-16 range text got %q want %q", got, "😀")
	}
}
