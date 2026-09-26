package services

import (
	"regexp"
	"sort"
	"strings"

	"github.com/yottonoko/vscode-css-languageservice-go/lsp"
)

type delimiterType string

const (
	delimiterBrace   delimiterType = "brace"
	delimiterComment delimiterType = "comment"
)

type delimiter struct {
	line    int
	typ     delimiterType
	isStart bool
}

type foldToken struct {
	kind      string
	text      string
	offset    int
	length    int
	startLine int
	endLine   int
}

func GetFoldingRanges(document *lsp.TextDocument, rangeLimit int) []lsp.FoldingRange {
	ranges := computeFoldingRanges(document)
	return limitFoldingRanges(ranges, rangeLimit)
}

func computeFoldingRanges(document *lsp.TextDocument) []lsp.FoldingRange {
	var ranges []lsp.FoldingRange
	var stack []delimiter
	for _, token := range scanFoldTokens(document) {
		switch token.kind {
		case "{":
			stack = append(stack, delimiter{line: token.startLine, typ: delimiterBrace, isStart: true})
		case "}":
			prevDelimiter, ok := popPrevStartDelimiterOfType(&stack, delimiterBrace)
			if !ok {
				break
			}
			endLine := token.endLine
			if !hasNonWhitespaceBeforeOnLine(document.Text(), token.offset) {
				endLine--
			}
			if prevDelimiter.line != endLine {
				ranges = append(ranges, lsp.FoldingRange{StartLine: prevDelimiter.line, EndLine: endLine})
			}
		case "comment":
			currDelimiter, ok := commentRegionDelimiter(document.LanguageID, token)
			if ok {
				if currDelimiter.isStart {
					stack = append(stack, currDelimiter)
				} else {
					prevDelimiter, ok := popPrevStartDelimiterOfType(&stack, delimiterComment)
					if ok && prevDelimiter.line != currDelimiter.line {
						kind := lsp.FoldingRangeKindRegion
						ranges = append(ranges, lsp.FoldingRange{StartLine: prevDelimiter.line, EndLine: currDelimiter.line, Kind: &kind})
					}
				}
			} else if token.startLine != token.endLine {
				kind := lsp.FoldingRangeKindComment
				ranges = append(ranges, lsp.FoldingRange{StartLine: token.startLine, EndLine: token.endLine, Kind: &kind})
			}
		}
	}
	return ranges
}

func scanFoldTokens(document *lsp.TextDocument) []foldToken {
	text := document.Text()
	var tokens []foldToken
	for i := 0; i < len(text); {
		if i+1 < len(text) && text[i] == '/' && text[i+1] == '*' {
			start := i
			i += 2
			for i < len(text) {
				if i+1 < len(text) && text[i] == '*' && text[i+1] == '/' {
					i += 2
					break
				}
				i++
			}
			tokens = append(tokens, newFoldToken(document, "comment", string(text[start:i]), start, i-start))
			continue
		}
		if (document.LanguageID == "scss" || document.LanguageID == "less") && i+1 < len(text) && text[i] == '/' && text[i+1] == '/' {
			start := i
			i += 2
			for i < len(text) && text[i] != '\n' && text[i] != '\r' {
				i++
			}
			tokens = append(tokens, newFoldToken(document, "comment", string(text[start:i]), start, i-start))
			continue
		}
		switch text[i] {
		case '{':
			tokens = append(tokens, newFoldToken(document, "{", "{", i, 1))
		case '}':
			tokens = append(tokens, newFoldToken(document, "}", "}", i, 1))
		}
		i++
	}
	return tokens
}

func newFoldToken(document *lsp.TextDocument, kind, text string, offset, length int) foldToken {
	return foldToken{
		kind:      kind,
		text:      text,
		offset:    offset,
		length:    length,
		startLine: positionAtByteOffset(document, offset).Line,
		endLine:   positionAtByteOffset(document, offset+length).Line,
	}
}

var blockRegionPattern = regexp.MustCompile(`^\s*/\*\s*(#region|#endregion)\b\s*(.*?)\s*\*/`)
var lineRegionPattern = regexp.MustCompile(`^\s*//\s*(#region|#endregion)\b\s*(.*?)\s*`)

func commentRegionDelimiter(languageID string, token foldToken) (delimiter, bool) {
	if matches := blockRegionPattern.FindStringSubmatch(token.text); matches != nil {
		return markerToDelimiter(matches[1], token), true
	}
	if languageID == "scss" || languageID == "less" {
		if matches := lineRegionPattern.FindStringSubmatch(token.text); matches != nil {
			return markerToDelimiter(matches[1], token), true
		}
	}
	return delimiter{}, false
}

func markerToDelimiter(marker string, token foldToken) delimiter {
	if marker == "#region" {
		return delimiter{line: token.startLine, typ: delimiterComment, isStart: true}
	}
	return delimiter{line: token.endLine, typ: delimiterComment, isStart: false}
}

func popPrevStartDelimiterOfType(stack *[]delimiter, typ delimiterType) (delimiter, bool) {
	for i := len(*stack) - 1; i >= 0; i-- {
		if (*stack)[i].typ == typ && (*stack)[i].isStart {
			result := (*stack)[i]
			*stack = append((*stack)[:i], (*stack)[i+1:]...)
			return result, true
		}
	}
	return delimiter{}, false
}

func limitFoldingRanges(ranges []lsp.FoldingRange, rangeLimit int) []lsp.FoldingRange {
	if rangeLimit <= 0 {
		rangeLimit = int(^uint(0) >> 1)
	}
	sort.SliceStable(ranges, func(i, j int) bool {
		if ranges[i].StartLine == ranges[j].StartLine {
			return ranges[i].EndLine < ranges[j].EndLine
		}
		return ranges[i].StartLine < ranges[j].StartLine
	})
	validRanges := make([]lsp.FoldingRange, 0, len(ranges))
	prevEndLine := -1
	for _, r := range ranges {
		if !(r.StartLine < prevEndLine && prevEndLine < r.EndLine) {
			validRanges = append(validRanges, r)
			prevEndLine = r.EndLine
		}
	}
	if len(validRanges) < rangeLimit {
		return validRanges
	}
	return validRanges[:rangeLimit]
}

func hasNonWhitespaceBeforeOnLine(text string, offset int) bool {
	for i := offset - 1; i >= 0; i-- {
		switch text[i] {
		case '\n', '\r':
			return false
		case ' ', '\t', '\f':
			continue
		default:
			return true
		}
	}
	return false
}

func FoldingRange(startLine, endLine int, kind ...string) lsp.FoldingRange {
	result := lsp.FoldingRange{StartLine: startLine, EndLine: endLine}
	if len(kind) > 0 && strings.TrimSpace(kind[0]) != "" {
		k := lsp.FoldingRangeKind(kind[0])
		result.Kind = &k
	}
	return result
}
