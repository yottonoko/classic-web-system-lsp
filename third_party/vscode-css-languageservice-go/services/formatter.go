package services

import (
	"strings"
	"unicode"

	"github.com/yottonoko/vscode-css-languageservice-go/lsp"
)

// FormatOptions configures standalone formatter output.
type FormatOptions struct {
	TabSize                      int
	InsertSpaces                 bool
	InsertFinalNewline           bool
	NewlineBetweenSelectors      bool
	NewlineBetweenRules          bool
	SpaceAroundSelectorSeparator bool
	BraceStyle                   string
	PreserveNewLines             bool
	MaxPreserveNewLines          int
	WrapLineLength               int
	IndentEmptyLines             bool
	SelectorSeparatorNewline     bool
}

func Format(document *lsp.TextDocument, r *lsp.Range, options FormatOptions) []lsp.TextEdit {
	value := document.Text()
	includesEnd := true
	initialIndentLevel := 0
	inRule := false
	options = normalizeFormatOptions(options)
	var editRange lsp.Range
	if r != nil {
		startOffset := byteOffsetAtPosition(document, r.Start)
		extendedStart := startOffset
		for extendedStart > 0 && isInlineWhitespace(rune(value[extendedStart-1])) {
			extendedStart--
		}
		if extendedStart == 0 || isEOL(rune(value[extendedStart-1])) {
			startOffset = extendedStart
		} else if extendedStart < startOffset {
			startOffset = extendedStart + 1
		}
		endOffset := byteOffsetAtPosition(document, r.End)
		extendedEnd := endOffset
		for extendedEnd < len(value) && isInlineWhitespace(rune(value[extendedEnd])) {
			extendedEnd++
		}
		if extendedEnd == len(value) || isEOL(rune(value[extendedEnd])) {
			endOffset = extendedEnd
		}
		editRange = rangeFromOffsets(document, startOffset, endOffset)
		inRule = isInRule(value, startOffset)
		includesEnd = endOffset == len(value)
		value = value[startOffset:endOffset]
		if startOffset != 0 {
			startOfLineOffset := byteOffsetAtPosition(document, lsp.Position{Line: editRange.Start.Line, Character: 0})
			initialIndentLevel = computeIndentLevel(document.Text(), startOfLineOffset, options)
		}
		if inRule {
			value = "{\n" + strings.TrimLeftFunc(value, unicode.IsSpace)
		}
	} else {
		editRange = lsp.Range{Start: lsp.Position{}, End: positionAtByteOffset(document, len(value))}
	}
	beautifierOptions := CSSBeautifierOptionsFromFormatOptions(options, 0)
	beautifierOptions.EndWithNewline = includesEnd && options.InsertFinalNewline
	result := defaultCSSBeautifier.BeautifyCSS(value, beautifierOptions)
	if inRule {
		if len(result) >= 2 {
			result = result[2:]
		}
		result = strings.TrimLeftFunc(result, unicode.IsSpace)
	}
	if !includesEnd {
		result = strings.TrimSuffix(result, "\n")
	}
	if initialIndentLevel > 0 {
		indent := strings.Repeat(indentUnit(options), initialIndentLevel)
		result = strings.ReplaceAll(result, "\n", "\n"+indent)
		if editRange.Start.Character == 0 {
			result = indent + result
		}
	}
	return []lsp.TextEdit{{Range: editRange, NewText: result}}
}

func normalizeFormatOptions(options FormatOptions) FormatOptions {
	if options.TabSize == 0 {
		options.TabSize = 4
	}
	if !options.InsertSpaces {
		options.InsertSpaces = true
	}
	if !options.NewlineBetweenSelectors && !options.SpaceAroundSelectorSeparator {
		// zero value means "default true" unless selector separator mode is explicitly customized.
		options.NewlineBetweenSelectors = true
	}
	options.NewlineBetweenRules = true
	if options.BraceStyle == "" {
		options.BraceStyle = "collapse"
	}
	return options
}

func indentUnit(options FormatOptions) string {
	if options.InsertSpaces {
		return strings.Repeat(" ", options.TabSize)
	}
	return "\t"
}

func isInRule(value string, offset int) bool {
	runes := []rune(value)
	for offset >= 0 && offset < len(runes) {
		switch runes[offset] {
		case '{':
			return true
		case '}':
			return false
		}
		offset--
	}
	return false
}

func computeIndentLevel(content string, offset int, options FormatOptions) int {
	runes := []rune(content)
	nChars := 0
	for offset < len(runes) {
		switch runes[offset] {
		case ' ':
			nChars++
		case '\t':
			nChars += options.TabSize
		default:
			return nChars / options.TabSize
		}
		offset++
	}
	return nChars / options.TabSize
}

func isEOL(ch rune) bool {
	return ch == '\r' || ch == '\n'
}

func isInlineWhitespace(ch rune) bool {
	return ch == ' ' || ch == '\t'
}
