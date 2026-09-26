package htmlservice

import (
	"sort"
	"strings"
	"unicode/utf8"
)

// TextDocument mirrors the small document API used by the TypeScript service.
type TextDocument struct {
	URI        DocumentUri
	LanguageID string
	Version    int
	text       string
	lineStarts []int
	lineAtByte []int
	textIndex  utf16Index
}

func NewTextDocument(uri DocumentUri, languageID string, version int, text string) *TextDocument {
	lineStarts := computeLineStarts(text)
	return &TextDocument{URI: uri, LanguageID: languageID, Version: version, text: text, lineStarts: lineStarts, lineAtByte: computeLineAtByte(text, lineStarts), textIndex: newUTF16Index(text)}
}

func (d *TextDocument) GetText(r ...Range) string {
	if len(r) == 0 {
		return d.text
	}
	start := d.OffsetAt(r[0].Start)
	end := d.OffsetAt(r[0].End)
	if start > end {
		start, end = end, start
	}
	return d.text[start:end]
}

func (d *TextDocument) PositionAt(offset int) Position {
	return d.positionAtByteOffset(offset)
}

func (d *TextDocument) positionAtByteOffset(offset int) Position {
	if offset < 0 {
		offset = 0
	}
	if offset > len(d.text) {
		offset = len(d.text)
	}
	line := d.lineAtByte[offset]
	return Position{Line: line, Character: d.textIndex.codeUnitsAt(offset) - d.textIndex.codeUnitsAt(d.lineStarts[line])}
}

func (d *TextDocument) lineAtByteOffset(offset int) int {
	if offset < 0 {
		return 0
	}
	if offset > len(d.text) {
		offset = len(d.text)
	}
	return d.lineAtByte[offset]
}

func (d *TextDocument) rangeAtByteOffsets(start, end int) Range {
	return Range{Start: d.positionAtByteOffset(start), End: d.positionAtByteOffset(end)}
}

func (d *TextDocument) OffsetAt(position Position) int {
	if position.Line <= 0 {
		lineEnd := d.lineEnd(0)
		return d.byteOffsetAtLineCharacter(0, lineEnd, position.Character)
	}
	if position.Line >= len(d.lineStarts) {
		return len(d.text)
	}
	lineStart := d.lineStarts[position.Line]
	lineEnd := d.lineEnd(position.Line)
	return d.byteOffsetAtLineCharacter(lineStart, lineEnd, position.Character)
}

func (d *TextDocument) byteOffsetAtLineCharacter(lineStart, lineEnd, character int) int {
	if character <= 0 {
		return lineStart
	}
	targetCU := d.textIndex.codeUnitsAt(lineStart) + character
	offset := d.textIndex.byteOffsetForCodeUnit(targetCU)
	if offset < lineStart {
		return lineStart
	}
	if offset > lineEnd {
		return lineEnd
	}
	return offset
}

func (d *TextDocument) lineEnd(line int) int {
	if line+1 >= len(d.lineStarts) {
		return len(d.text)
	}
	lineStart := d.lineStarts[line]
	lineEnd := d.lineStarts[line+1]
	for lineEnd > lineStart && (d.text[lineEnd-1] == '\n' || d.text[lineEnd-1] == '\r') {
		lineEnd--
	}
	return lineEnd
}

func ApplyEdits(document *TextDocument, edits []TextEdit) string {
	type editWithOffset struct {
		start int
		end   int
		text  string
	}
	converted := make([]editWithOffset, 0, len(edits))
	for _, edit := range edits {
		converted = append(converted, editWithOffset{
			start: document.OffsetAt(edit.Range.Start),
			end:   document.OffsetAt(edit.Range.End),
			text:  edit.NewText,
		})
	}
	sort.SliceStable(converted, func(i, j int) bool {
		return converted[i].start > converted[j].start
	})
	result := document.text
	for _, edit := range converted {
		result = result[:edit.start] + edit.text + result[edit.end:]
	}
	return result
}

func computeLineStarts(text string) []int {
	starts := []int{0}
	for i := 0; i < len(text); i++ {
		switch text[i] {
		case '\r':
			if i+1 < len(text) && text[i+1] == '\n' {
				i++
			}
			starts = append(starts, i+1)
		case '\n':
			starts = append(starts, i+1)
		}
	}
	return starts
}

func computeLineAtByte(text string, lineStarts []int) []int {
	lineAtByte := make([]int, len(text)+1)
	for line, start := range lineStarts {
		end := len(text)
		if line+1 < len(lineStarts) {
			end = lineStarts[line+1]
		}
		for offset := start; offset < end; offset++ {
			lineAtByte[offset] = line
		}
	}
	lineAtByte[len(text)] = len(lineStarts) - 1
	return lineAtByte
}

func utf16Len(text string) int {
	length := 0
	for _, r := range text {
		if r > 0xFFFF {
			length += 2
		} else {
			length++
		}
	}
	return length
}

type utf16Index struct {
	text     string
	ascii    bool
	byteToCU []int
	cuToByte []int
}

func newUTF16Index(text string) utf16Index {
	ascii := true
	for i := 0; i < len(text); i++ {
		if text[i] >= utf8.RuneSelf {
			ascii = false
			break
		}
	}
	if ascii {
		return utf16Index{text: text, ascii: true}
	}
	byteToCU := make([]int, len(text)+1)
	cuToByte := []int{0}
	units := 0
	for i := 0; i < len(text); {
		byteToCU[i] = units
		r, size := utf8.DecodeRuneInString(text[i:])
		if size <= 0 {
			size = 1
		}
		if r < utf8.RuneSelf {
			i++
			units++
			cuToByte = append(cuToByte, i)
			continue
		}
		width := 1
		if r > 0xFFFF {
			width = 2
		}
		for j := 1; j < size && i+j < len(byteToCU); j++ {
			byteToCU[i+j] = units
		}
		if width == 2 {
			cuToByte = append(cuToByte, i)
		}
		i += size
		units += width
		cuToByte = append(cuToByte, i)
	}
	byteToCU[len(text)] = units
	return utf16Index{text: text, byteToCU: byteToCU, cuToByte: cuToByte}
}

func (i utf16Index) len() int {
	if i.ascii {
		return len(i.text)
	}
	return len(i.cuToByte) - 1
}

func (i utf16Index) codeUnitsAt(byteOffset int) int {
	if byteOffset <= 0 {
		return 0
	}
	if byteOffset >= len(i.text) {
		return i.len()
	}
	if i.ascii {
		return byteOffset
	}
	return i.byteToCU[byteOffset]
}

func (i utf16Index) byteOffsetForCodeUnit(character int) int {
	if character <= 0 {
		return 0
	}
	if i.ascii {
		if character > len(i.text) {
			return len(i.text)
		}
		return character
	}
	if character >= len(i.cuToByte) {
		return len(i.text)
	}
	return i.cuToByte[character]
}

func byteOffsetForUTF16Position(text string, character int) int {
	if character <= 0 {
		return 0
	}
	units := 0
	for offset, r := range text {
		width := 1
		if r > 0xFFFF {
			width = 2
		}
		if units+width > character {
			return offset
		}
		units += width
		_, size := utf8.DecodeRuneInString(text[offset:])
		if units >= character {
			return offset + size
		}
	}
	return len(text)
}

type utf16OffsetInfo struct {
	byteOffset int
	unitStart  int
	unit       uint16
	inside     bool
}

func utf16OffsetInfoForPosition(text string, character int) utf16OffsetInfo {
	if character <= 0 {
		return utf16OffsetInfo{byteOffset: 0}
	}
	units := 0
	for offset, r := range text {
		width := 1
		if r > 0xFFFF {
			width = 2
		}
		_, size := utf8.DecodeRuneInString(text[offset:])
		if units+width > character {
			if r > 0xFFFF && character == units+1 {
				_, low := utf16EncodeRune(r)
				return utf16OffsetInfo{byteOffset: offset + size, unitStart: units + 1, unit: low, inside: true}
			}
			return utf16OffsetInfo{byteOffset: offset}
		}
		units += width
		if units >= character {
			return utf16OffsetInfo{byteOffset: offset + size}
		}
	}
	return utf16OffsetInfo{byteOffset: len(text)}
}

func utf16EncodeRune(r rune) (uint16, uint16) {
	r -= 0x10000
	return uint16(0xD800 + (r >> 10)), uint16(0xDC00 + (r & 0x3FF))
}

func isJSWhitespaceRune(r rune) bool {
	switch r {
	case '\t', '\v', '\f', ' ', '\n', '\r', '\u00a0', '\u1680', '\u2028', '\u2029', '\u202f', '\u205f', '\u3000', '\ufeff':
		return true
	case 0x2000, 0x2001, 0x2002, 0x2003, 0x2004, 0x2005, 0x2006, 0x2007, 0x2008, 0x2009, 0x200a:
		return true
	default:
		return false
	}
}

func trimLeftJSWhitespace(value string) string {
	for i, r := range value {
		if !isJSWhitespaceRune(r) {
			return value[i:]
		}
	}
	return ""
}

func splitJSWhitespaceRuns(value string) []string {
	var parts []string
	start := 0
	for i := 0; i < len(value); {
		r, size := utf8.DecodeRuneInString(value[i:])
		if !isJSWhitespaceRune(r) {
			i += size
			continue
		}
		parts = append(parts, value[start:i])
		i += size
		for i < len(value) {
			r, size = utf8.DecodeRuneInString(value[i:])
			if !isJSWhitespaceRune(r) {
				break
			}
			i += size
		}
		start = i
	}
	return append(parts, value[start:])
}

func lineIndent(text string) string {
	i := 0
	for i < len(text) && (text[i] == ' ' || text[i] == '\t') {
		i++
	}
	return text[:i]
}

func stringsTrimQuotes(value string) string {
	if len(value) >= 2 {
		first := value[0]
		last := value[len(value)-1]
		if first == last && (first == '"' || first == '\'') {
			return value[1 : len(value)-1]
		}
	}
	return value
}

func startsWithIgnoreCase(s, prefix string) bool {
	return len(s) >= len(prefix) && strings.EqualFold(s[:len(prefix)], prefix)
}
