package core

import (
	"context"
	"sort"
	"strings"
	"unicode/utf8"

	"github.com/yottonoko/classic-web-system-lsp/internal/lsp"
)

type TextDocument struct {
	URI        string
	LanguageID string
	Version    int
	Text       string
	lineStarts []int
	lineASCII  []int8
}

// SkipPreviousRuntimeInheritance marks the document as revision-specific. Its
// line index belongs to one exact source text and is never reused across
// revisions.
func (*TextDocument) SkipPreviousRuntimeInheritance() {}

// EstimateBytes reports the storage retained by the document index. Text is
// excluded because runtime documents are built from ParsedDocument.Text and
// share that immutable backing string.
func (d *TextDocument) EstimateBytes() int64 {
	if d == nil {
		return 0
	}
	return 96 + int64(len(d.URI)+len(d.LanguageID))*2 + int64(cap(d.lineStarts))*8 + int64(cap(d.lineASCII))
}

// sourceDocumentAnalysisKey caches the source text document for a parsed revision.
const sourceDocumentAnalysisKey = "core.source-document.runtime.v1"

// SourceDocument returns the shared source text document for parsed, building
// its line index once per revision. Callers must not mutate the result; the
// index is retained by the parsed revision and accounted as its memory.
func SourceDocument(parsed *ParsedDocument) *TextDocument {
	if parsed == nil {
		return nil
	}
	if value, ok := parsed.LoadRuntimeAnalysis(sourceDocumentAnalysisKey); ok {
		if doc, ok := value.(*TextDocument); ok && doc != nil && doc.Text == parsed.Text {
			return doc
		}
	}
	doc := NewTextDocument(parsed.URI, "classic-asp", 0, parsed.Text)
	actual, _ := parsed.LoadOrStoreRuntimeAnalysis(sourceDocumentAnalysisKey, doc)
	if shared, ok := actual.(*TextDocument); ok && shared != nil && shared.Text == parsed.Text {
		return shared
	}
	return doc
}

func NewTextDocument(uri, languageID string, version int, text string) *TextDocument {
	starts, ascii := lineStartsAndASCII(text)
	return &TextDocument{
		URI:        uri,
		LanguageID: languageID,
		Version:    version,
		Text:       text,
		lineStarts: starts,
		lineASCII:  ascii,
	}
}

// NewTextDocumentContext builds a text document while observing cancellation
// during line-index construction.
func NewTextDocumentContext(ctx context.Context, uri, languageID string, version int, text string) (*TextDocument, error) {
	starts, ascii, err := lineStartsAndASCIIContext(ctx, text)
	if err != nil {
		return nil, err
	}
	return &TextDocument{URI: uri, LanguageID: languageID, Version: version, Text: text, lineStarts: starts, lineASCII: ascii}, nil
}

// Clone returns an independently editable document revision while sharing the
// immutable text and line index until the clone is changed.
func (d *TextDocument) Clone() *TextDocument {
	if d == nil {
		return nil
	}
	clone := *d
	return &clone
}

func (d *TextDocument) Update(version int, text string) {
	d.Version = version
	d.Text = text
	d.lineStarts, d.lineASCII = lineStartsAndASCII(text)
}

func (d *TextDocument) ApplyChange(r *lsp.Range, text string, version int) {
	if r == nil {
		d.Update(version, text)
		return
	}
	start := d.OffsetAt(r.Start)
	end := d.OffsetAt(r.End)
	if start > end {
		start, end = end, start
	}
	d.applyByteChange(start, end, text, version)
}

func (d *TextDocument) applyByteChange(start, end int, replacement string, version int) {
	start = clamp(start, 0, len(d.Text))
	end = clamp(end, start, len(d.Text))
	startLine := lineAtOffset(d.lineStarts, start)
	endLine := lineAtOffset(d.lineStarts, end)
	// Include a preceding bare CR because the edit may combine it with a new LF.
	if startLine > 0 && d.Text[d.lineStarts[startLine]-1] == '\r' {
		startLine--
	}
	scanStart := d.lineStarts[startLine]
	scanEnd := len(d.Text)
	suffixLine := len(d.lineStarts)
	if endLine+1 < len(d.lineStarts) {
		suffixLine = endLine + 1
		scanEnd = d.lineStarts[suffixLine]
	}
	segment := d.Text[scanStart:start] + replacement + d.Text[end:scanEnd]
	segmentStarts, segmentASCII := lineStartsAndASCII(segment)
	delta := len(segment) - (scanEnd - scanStart)

	starts := make([]int, 0, len(d.lineStarts)+len(segmentStarts))
	ascii := make([]int8, 0, len(d.lineASCII)+len(segmentASCII))
	starts = append(starts, d.lineStarts[:startLine]...)
	ascii = append(ascii, d.lineASCII[:startLine]...)
	for _, offset := range segmentStarts {
		starts = append(starts, scanStart+offset)
	}
	if suffixLine < len(d.lineStarts) && len(segmentASCII) > 0 {
		segmentASCII[len(segmentASCII)-1] = d.lineASCII[suffixLine]
	}
	ascii = append(ascii, segmentASCII...)
	if suffixLine < len(d.lineStarts) {
		for _, offset := range d.lineStarts[suffixLine+1:] {
			starts = append(starts, offset+delta)
		}
		ascii = append(ascii, d.lineASCII[suffixLine+1:]...)
	}

	d.Version = version
	d.Text = d.Text[:start] + replacement + d.Text[end:]
	d.lineStarts = starts
	d.lineASCII = ascii
}

func lineAtOffset(starts []int, offset int) int {
	line := sort.Search(len(starts), func(index int) bool { return starts[index] > offset }) - 1
	return max(0, line)
}

func (d *TextDocument) PositionAt(offset int) lsp.Position {
	if offset < 0 {
		offset = 0
	}
	if offset > len(d.Text) {
		offset = len(d.Text)
	}
	line := sort.Search(len(d.lineStarts), func(i int) bool {
		return d.lineStarts[i] > offset
	}) - 1
	if line < 0 {
		line = 0
	}
	if d.isLineASCII(line) {
		return lsp.Position{Line: line, Character: offset - d.lineStarts[line]}
	}
	return lsp.Position{Line: line, Character: utf16Length(d.Text[d.lineStarts[line]:runeStartAtOrBefore(d.Text, d.lineStarts[line], offset)])}
}

// runeStartAtOrBefore maps an offset inside a multi-byte rune to the rune
// start; counting a truncated UTF-8 prefix would make positions non-monotonic.
// It stays out of line because inlining it into PositionAt slowed the ASCII
// fast path by about 25% (BenchmarkClassicASPPositionAt200KBFixture).
//
//go:noinline
func runeStartAtOrBefore(text string, lineStart, offset int) int {
	for offset > lineStart && offset < len(text) && !utf8.RuneStart(text[offset]) {
		offset--
	}
	return offset
}

func (d *TextDocument) OffsetAt(position lsp.Position) int {
	if position.Line <= 0 {
		end := lineEnd(d.Text, 0, d.lineStarts)
		if d.isLineASCII(0) {
			return clamp(position.Character, 0, end)
		}
		return byteOffsetForUTF16Character(d.Text[:end], position.Character)
	}
	if position.Line >= len(d.lineStarts) {
		return len(d.Text)
	}
	lineStart := d.lineStarts[position.Line]
	end := lineEnd(d.Text, position.Line, d.lineStarts)
	if d.isLineASCII(position.Line) {
		return lineStart + clamp(position.Character, 0, end-lineStart)
	}
	return lineStart + byteOffsetForUTF16Character(d.Text[lineStart:end], position.Character)
}

func (d *TextDocument) Range(start, end int) lsp.Range {
	return lsp.Range{Start: d.PositionAt(start), End: d.PositionAt(end)}
}

func lineStartsAndASCII(text string) ([]int, []int8) {
	// Count CRLF once and reserve exact storage instead of repeatedly copying
	// growing indexes. strings.Count uses optimized byte searches for LF/CR.
	lines := 1 + strings.Count(text, "\n")
	if carriageReturns := strings.Count(text, "\r"); carriageReturns != 0 {
		lines += carriageReturns - strings.Count(text, "\r\n")
	}
	starts := make([]int, 1, lines)
	ascii := make([]int8, 1, lines)
	ascii[0] = 1
	for i := 0; i < len(text); i++ {
		if text[i] >= utf8.RuneSelf {
			ascii[len(ascii)-1] = -1
		}
		switch text[i] {
		case '\r':
			if i+1 < len(text) && text[i+1] == '\n' {
				i++
			}
			starts = append(starts, i+1)
			ascii = append(ascii, 1)
		case '\n':
			starts = append(starts, i+1)
			ascii = append(ascii, 1)
		}
	}
	return starts, ascii
}

func lineStartsAndASCIIContext(ctx context.Context, text string) ([]int, []int8, error) {
	starts := []int{0}
	ascii := []int8{1}
	for i := 0; i < len(text); i++ {
		if i&4095 == 0 {
			if err := ctx.Err(); err != nil {
				return nil, nil, err
			}
		}
		if text[i] >= utf8.RuneSelf {
			ascii[len(ascii)-1] = -1
		}
		switch text[i] {
		case '\r':
			if i+1 < len(text) && text[i+1] == '\n' {
				i++
			}
			starts = append(starts, i+1)
			ascii = append(ascii, 1)
		case '\n':
			starts = append(starts, i+1)
			ascii = append(ascii, 1)
		}
	}
	if err := ctx.Err(); err != nil {
		return nil, nil, err
	}
	return starts, ascii, nil
}

func (d *TextDocument) isLineASCII(line int) bool {
	if line < 0 || line >= len(d.lineStarts) {
		return true
	}
	return line >= len(d.lineASCII) || d.lineASCII[line] == 1
}

func lineEnd(text string, line int, starts []int) int {
	if line+1 >= len(starts) {
		return len(text)
	}
	end := starts[line+1]
	start := starts[line]
	for end > start && (text[end-1] == '\n' || text[end-1] == '\r') {
		end--
	}
	return end
}

func utf16Length(text string) int {
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

func byteOffsetForUTF16Character(text string, character int) int {
	if character <= 0 {
		return 0
	}
	units := 0
	for offset, r := range text {
		width := 1
		if r > 0xFFFF {
			width = 2
		}
		_, size := utf8.DecodeRuneInString(text[offset:])
		if units+width >= character {
			return offset + size
		}
		units += width
	}
	return len(text)
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
