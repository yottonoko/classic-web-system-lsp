package services

import (
	"strings"

	"github.com/yottonoko/vscode-css-languageservice-go/lsp"
)

type selectionInterval struct {
	start int
	end   int
}

func GetSelectionRanges(document *lsp.TextDocument, positions []lsp.Position) []lsp.SelectionRange {
	ranges := make([]lsp.SelectionRange, len(positions))
	for i, position := range positions {
		ranges[i] = selectionRangeAt(document, byteOffsetAtPosition(document, position))
	}
	return ranges
}

func selectionRangeAt(document *lsp.TextDocument, offset int) lsp.SelectionRange {
	intervals := selectionIntervalsAt(document.Text(), offset)
	if len(intervals) == 0 {
		empty := lsp.Range{Start: positionAtByteOffset(document, offset), End: positionAtByteOffset(document, offset)}
		return lsp.SelectionRange{Range: empty}
	}
	var current *lsp.SelectionRange
	for i := len(intervals) - 1; i >= 0; i-- {
		interval := intervals[i]
		next := lsp.SelectionRange{
			Range:  rangeFromOffsets(document, interval.start, interval.end),
			Parent: current,
		}
		current = &next
	}
	return *current
}

func selectionIntervalsAt(text string, offset int) []selectionInterval {
	blocks := parseCSSBlocks(text)
	var selected *cssBlock
	for i := range blocks {
		block := blocks[i]
		if offset >= block.headStart && offset <= block.end+1 {
			if selected == nil || block.start >= selected.start {
				selected = &blocks[i]
			}
		}
	}
	if selected == nil {
		return nil
	}

	intervals := blockSpecificSelectionIntervals(text, *selected, offset)
	brace := selectionInterval{start: selected.start, end: selected.end + 1}
	rule := selectionInterval{start: selected.headStart, end: selected.end + 1}
	if offset >= selected.start && offset <= selected.end+1 {
		intervals = appendIfDistinct(intervals, brace)
	}
	intervals = appendIfDistinct(intervals, rule)
	return intervals
}

func blockSpecificSelectionIntervals(text string, block cssBlock, offset int) []selectionInterval {
	if offset >= block.bodyStart && offset <= block.bodyEnd {
		if declaration, ok := declarationAt(text, block, offset); ok {
			intervals := declarationSelectionIntervals(text, declaration, offset)
			body := selectionInterval{start: block.bodyStart, end: block.bodyEnd}
			return appendIfDistinct(intervals, body)
		}
		return []selectionInterval{{start: block.bodyStart, end: block.bodyEnd}}
	}
	if offset >= block.headStart && offset < block.start {
		return selectorSelectionIntervals(text, block, offset)
	}
	return nil
}

func declarationAt(text string, block cssBlock, offset int) (cssDeclaration, bool) {
	declarations := parseDeclarations(text, block.bodyStart, block.bodyEnd)
	for _, declaration := range declarations {
		if offset >= declaration.offset && offset <= declaration.offset+declaration.length {
			return declaration, true
		}
	}
	return cssDeclaration{}, false
}

func declarationSelectionIntervals(text string, declaration cssDeclaration, offset int) []selectionInterval {
	var intervals []selectionInterval
	nameEnd := declaration.nameOffset + len(declaration.name)
	if offset >= declaration.nameOffset && offset <= nameEnd {
		intervals = append(intervals, selectionInterval{start: declaration.nameOffset, end: nameEnd})
	} else if token, ok := valueTokenAt(text, declaration, offset); ok {
		intervals = append(intervals, token)
	}

	valueEnd := declaration.valueStart + len(declaration.value)
	if offset >= declaration.valueStart && offset <= valueEnd {
		intervals = appendIfDistinct(intervals, selectionInterval{start: declaration.valueStart, end: valueEnd})
	}
	declEnd := valueEnd
	if declEnd > declaration.offset && text[declEnd-1] == ';' {
		declEnd--
	}
	return appendIfDistinct(intervals, selectionInterval{start: declaration.offset, end: declEnd})
}

func valueTokenAt(text string, declaration cssDeclaration, offset int) (selectionInterval, bool) {
	start := declaration.valueStart
	end := declaration.valueStart + len(declaration.value)
	if offset < start || offset > end {
		return selectionInterval{}, false
	}
	if token, ok := quotedTokenAt(text, start, end, offset); ok {
		return token, true
	}
	tokenStart := offset
	for tokenStart > start && !isValueDelimiter(text[tokenStart-1]) {
		tokenStart--
	}
	tokenEnd := offset
	for tokenEnd < end && !isValueDelimiter(text[tokenEnd]) {
		tokenEnd++
	}
	tokenStart, tokenEnd = trimRange(text, tokenStart, tokenEnd)
	if tokenStart < tokenEnd {
		return selectionInterval{start: tokenStart, end: tokenEnd}, true
	}
	return selectionInterval{}, false
}

func quotedTokenAt(text string, start, end, offset int) (selectionInterval, bool) {
	for i := start; i < end; i++ {
		if text[i] != '\'' && text[i] != '"' {
			continue
		}
		quoteStart := i
		quoteEnd := skipCSSIgnored(text, i)
		if quoteEnd < quoteStart {
			continue
		}
		if offset >= quoteStart && offset <= quoteEnd+1 {
			return selectionInterval{start: quoteStart, end: quoteEnd + 1}, true
		}
		i = quoteEnd
	}
	return selectionInterval{}, false
}

func selectorSelectionIntervals(text string, block cssBlock, offset int) []selectionInterval {
	headEnd := block.start
	headStart, headEnd := trimRange(text, block.headStart, headEnd)
	if headStart >= headEnd {
		return nil
	}
	var intervals []selectionInterval
	partStart := offset
	if partStart < headStart {
		partStart = headStart
	}
	if partStart > headEnd {
		partStart = headEnd
	}
	for partStart > headStart && isSelectorIdentifierByte(text[partStart-1]) {
		partStart--
	}
	partEnd := offset
	if partEnd < headStart {
		partEnd = headStart
	}
	if partEnd > headEnd {
		partEnd = headEnd
	}
	for partEnd < headEnd && isSelectorIdentifierByte(text[partEnd]) {
		partEnd++
	}
	if partStart < partEnd {
		intervals = append(intervals, selectionInterval{start: partStart, end: partEnd})
	}
	simpleStart := partStart
	for simpleStart > headStart && strings.ContainsRune(".#:%", rune(text[simpleStart-1])) {
		simpleStart--
	}
	if simpleStart < partEnd {
		intervals = appendIfDistinct(intervals, selectionInterval{start: simpleStart, end: partEnd})
	}
	return intervals
}

func appendIfDistinct(intervals []selectionInterval, interval selectionInterval) []selectionInterval {
	if interval.start > interval.end {
		return intervals
	}
	for _, existing := range intervals {
		if existing == interval {
			return intervals
		}
	}
	return append(intervals, interval)
}

func isValueDelimiter(b byte) bool {
	return isCSSSpace(b) || b == ',' || b == ';' || b == '(' || b == ')' || b == '{' || b == '}'
}

func isSelectorIdentifierByte(b byte) bool {
	return b == '_' || b == '-' || b >= 'a' && b <= 'z' || b >= 'A' && b <= 'Z' || b >= '0' && b <= '9'
}
