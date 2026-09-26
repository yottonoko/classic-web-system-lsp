package lsp

import (
	"sort"
	"unicode/utf16"
	"unicode/utf8"
)

// TextDocument stores LSP text document content and position conversions.
type TextDocument struct {
	URI         DocumentURI
	LanguageID  string
	Version     int
	text        string
	utf16       []uint16
	lineStarts  []int
	byteToUTF16 []int
	utf16ToByte []int
}

func NewTextDocument(uri DocumentURI, languageID string, version int, text string) *TextDocument {
	d := &TextDocument{URI: uri, LanguageID: languageID, Version: version, text: text}
	d.utf16 = utf16.Encode([]rune(text))
	d.lineStarts = computeLineStarts(d.utf16)
	d.byteToUTF16, d.utf16ToByte = computeOffsetMaps(text)
	return d
}

func (d *TextDocument) Text() string {
	return d.text
}

func (d *TextDocument) GetText(r *Range) string {
	if r == nil {
		return d.text
	}
	start := d.OffsetAt(r.Start)
	end := d.OffsetAt(r.End)
	if start < 0 {
		start = 0
	}
	if end > len(d.utf16) {
		end = len(d.utf16)
	}
	if start > end {
		start = end
	}
	return string(utf16.Decode(d.utf16[start:end]))
}

func (d *TextDocument) PositionAt(offset int) Position {
	if offset < 0 {
		offset = 0
	}
	if offset > len(d.utf16) {
		offset = len(d.utf16)
	}
	line := sort.Search(len(d.lineStarts), func(i int) bool {
		return d.lineStarts[i] > offset
	}) - 1
	if line < 0 {
		line = 0
	}
	return Position{Line: line, Character: offset - d.lineStarts[line]}
}

func (d *TextDocument) OffsetAt(position Position) int {
	if position.Line <= 0 {
		return clamp(position.Character, 0, len(d.utf16))
	}
	if position.Line >= len(d.lineStarts) {
		return len(d.utf16)
	}
	nextLine := len(d.utf16)
	if position.Line+1 < len(d.lineStarts) {
		nextLine = d.lineStarts[position.Line+1]
	}
	return clamp(d.lineStarts[position.Line]+position.Character, d.lineStarts[position.Line], nextLine)
}

// PositionAtByteOffset converts a byte offset in the document text to an LSP UTF-16 position.
func (d *TextDocument) PositionAtByteOffset(byteOffset int) Position {
	return d.PositionAt(d.UTF16OffsetAtByteOffset(byteOffset))
}

// ByteOffsetAt converts an LSP UTF-16 position to a byte offset in the document text.
func (d *TextDocument) ByteOffsetAt(position Position) int {
	return d.byteOffsetAtUTF16Offset(d.OffsetAt(position))
}

// UTF16OffsetAtByteOffset converts a byte offset in the document text to a UTF-16 offset.
func (d *TextDocument) UTF16OffsetAtByteOffset(byteOffset int) int {
	if byteOffset < 0 {
		return 0
	}
	if byteOffset >= len(d.byteToUTF16) {
		return d.byteToUTF16[len(d.byteToUTF16)-1]
	}
	return d.byteToUTF16[byteOffset]
}

func (d *TextDocument) byteOffsetAtUTF16Offset(utf16Offset int) int {
	if utf16Offset < 0 {
		return 0
	}
	if utf16Offset >= len(d.utf16ToByte) {
		return len(d.text)
	}
	return d.utf16ToByte[utf16Offset]
}

func ApplyEdits(document *TextDocument, edits []TextEdit) string {
	if len(edits) == 0 {
		return document.Text()
	}
	sorted := append([]TextEdit(nil), edits...)
	sort.SliceStable(sorted, func(i, j int) bool {
		return document.OffsetAt(sorted[i].Range.Start) > document.OffsetAt(sorted[j].Range.Start)
	})
	units := append([]uint16(nil), document.utf16...)
	for _, edit := range sorted {
		start := document.OffsetAt(edit.Range.Start)
		end := document.OffsetAt(edit.Range.End)
		replacement := utf16.Encode([]rune(edit.NewText))
		next := make([]uint16, 0, len(units)-end+start+len(replacement))
		next = append(next, units[:start]...)
		next = append(next, replacement...)
		next = append(next, units[end:]...)
		units = next
	}
	return string(utf16.Decode(units))
}

func computeLineStarts(units []uint16) []int {
	starts := []int{0}
	for i := 0; i < len(units); i++ {
		switch units[i] {
		case '\r':
			if i+1 < len(units) && units[i+1] == '\n' {
				i++
			}
			starts = append(starts, i+1)
		case '\n':
			starts = append(starts, i+1)
		}
	}
	return starts
}

func computeOffsetMaps(text string) ([]int, []int) {
	byteToUTF16 := make([]int, len(text)+1)
	var utf16ToByte []int
	utf16Offset := 0
	prevByte := 0
	for byteOffset, r := range text {
		for i := prevByte; i <= byteOffset; i++ {
			byteToUTF16[i] = utf16Offset
		}
		_, width := utf8.DecodeRuneInString(text[byteOffset:])
		if width <= 0 {
			width = 1
		}
		unitWidth := utf16RuneWidth(r)
		for i := 0; i < unitWidth; i++ {
			utf16ToByte = append(utf16ToByte, byteOffset)
		}
		utf16Offset += unitWidth
		prevByte = byteOffset + width
	}
	for i := prevByte; i <= len(text); i++ {
		byteToUTF16[i] = utf16Offset
	}
	utf16ToByte = append(utf16ToByte, len(text))
	return byteToUTF16, utf16ToByte
}

func utf16RuneWidth(r rune) int {
	if r >= 0x10000 {
		return 2
	}
	return 1
}

func clamp(value, min, max int) int {
	if value < min {
		return min
	}
	if value > max {
		return max
	}
	return value
}
