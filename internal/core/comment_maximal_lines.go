package core

import (
	"sort"
	"strings"
)

func maximalGenericLineTargets(doc *TextDocument, text string, piece maximalCommentPiece, marker string) []maximalLineTarget {
	var targets []maximalLineTarget
	for line, lineStart := range doc.lineStarts {
		lineEndOffset := lineEnd(text, line, doc.lineStarts)
		start, end := max(piece.start, lineStart), min(piece.end, lineEndOffset)
		if start >= end {
			continue
		}
		codeStart, _, ok := trimmedCommentSpan(text, start, end)
		if !ok {
			continue
		}
		targets = append(targets, maximalLineTargetAt(text, marker, codeStart, false))
	}
	return targets
}

func maximalServerLineTargets(
	doc *TextDocument,
	text string,
	parsed *ParsedDocument,
	piece maximalCommentPiece,
	settings Settings,
) []maximalLineTarget {
	var targets []maximalLineTarget
	for index := range parsed.Regions {
		region := &parsed.Regions[index]
		if !isASPRegion(region.Kind) && region.Kind != RegionServerScript {
			continue
		}
		if region.ContentEnd <= piece.start || region.ContentStart >= piece.end {
			continue
		}
		marker := "'"
		language := region.Language
		if region.Kind == RegionASPDirective {
			language = normalizeServerLanguage(settings.DefaultLanguage)
		}
		if language == LanguageJScript {
			marker = "//"
		}
		firstRegionLine := doc.PositionAt(region.Start).Line
		firstLine := doc.PositionAt(max(region.ContentStart, piece.start)).Line
		lastLine := doc.PositionAt(max(min(region.ContentEnd, piece.end)-1, max(region.ContentStart, piece.start))).Line
		for line := firstLine; line <= lastLine; line++ {
			lineStart := doc.lineStarts[line]
			lineEndOffset := lineEnd(text, line, doc.lineStarts)
			start := max(max(region.ContentStart, piece.start), lineStart)
			end := min(min(region.ContentEnd, piece.end), lineEndOffset)
			if line == firstRegionLine && isASPRegion(region.Kind) {
				start = max(max(region.Start+2, piece.start), lineStart)
			}
			if start >= end {
				continue
			}
			codeStart, _, ok := trimmedCommentSpan(text, start, end)
			if !ok {
				continue
			}
			firstASPLine := isASPRegion(region.Kind) && line == firstRegionLine && region.Start+2 >= piece.start
			insertAt := codeStart
			compact := false
			if firstASPLine {
				insertAt = region.Start + 2
				compact = region.Kind == RegionASPExpression ||
					region.Kind == RegionASPDirective ||
					insertAt < len(text) && isHorizontalCommentWhitespace(text[insertAt])
			}
			target := maximalLineTargetAt(text, marker, insertAt, compact)
			if target.commentStart < 0 && codeStart != insertAt {
				if alternative := maximalLineTargetAt(text, marker, codeStart, false); alternative.commentStart >= 0 {
					target = alternative
				}
			}
			if marker == "'" && target.commentStart < 0 {
				if _, ok := vbscriptREMEnd(text, codeStart, end); ok {
					if firstASPLine {
						target.insertAt = region.Start + 2
						target.insertText = marker
					} else {
						target.insertAt = codeStart
						target.insertText = marker + " "
					}
				}
			}
			target.preserveFollowingGap = firstASPLine && target.commentStart == region.Start+2
			target.normalizeExpression = target.preserveFollowingGap &&
				maximalCommentPrefixPrecedesExpression(text, target.commentEnd)
			targets = append(targets, target)
		}
	}
	sort.SliceStable(targets, func(i, j int) bool {
		return targets[i].insertAt < targets[j].insertAt
	})
	return compactMaximalLineTargets(targets)
}

func maximalCommentPrefixPrecedesExpression(text string, offset int) bool {
	for offset < len(text) && isHorizontalCommentWhitespace(text[offset]) {
		offset++
	}
	return offset < len(text) && text[offset] == '='
}

func maximalLineTargetAt(text, marker string, insertAt int, compact bool) maximalLineTarget {
	target := maximalLineTarget{
		marker:       marker,
		commentStart: -1,
		commentEnd:   -1,
		insertAt:     insertAt,
		insertText:   marker + " ",
	}
	if compact || insertAt < len(text) && isHorizontalCommentWhitespace(text[insertAt]) {
		target.insertText = marker
	}
	if insertAt+len(marker) <= len(text) && text[insertAt:insertAt+len(marker)] == marker {
		target.commentStart = insertAt
		target.commentEnd = insertAt + len(marker)
		return target
	}
	return target
}

func compactMaximalLineTargets(targets []maximalLineTarget) []maximalLineTarget {
	result := targets[:0]
	for _, target := range targets {
		if len(result) > 0 && result[len(result)-1].insertAt == target.insertAt && result[len(result)-1].marker == target.marker {
			if result[len(result)-1].commentStart < 0 && target.commentStart >= 0 {
				result[len(result)-1] = target
			}
			continue
		}
		result = append(result, target)
	}
	return result
}

func maximalLineTargetsCommented(targets []maximalLineTarget) bool {
	if len(targets) == 0 {
		return false
	}
	for _, target := range targets {
		if target.commentStart < 0 {
			return false
		}
	}
	return true
}

func transformMaximalLinePiece(
	text string,
	piece maximalCommentPiece,
	targets []maximalLineTarget,
	uncomment bool,
) string {
	return transformMaximalLineValue(text[piece.start:piece.end], piece.start, targets, uncomment)
}

func transformMaximalLineValue(
	value string,
	base int,
	targets []maximalLineTarget,
	uncomment bool,
) string {
	operations := make([]commentOperation, 0, len(targets))
	for _, target := range targets {
		if uncomment {
			if target.commentStart < 0 {
				continue
			}
			end := target.commentEnd
			if strings.EqualFold(value[target.commentStart-base:target.commentEnd-base], "REM") {
				if end < base+len(value) && textByteAt(value, end-base) == ' ' {
					end++
				}
			} else if target.normalizeExpression {
				for end < base+len(value) && isHorizontalCommentWhitespace(textByteAt(value, end-base)) {
					end++
				}
			} else if !target.preserveFollowingGap &&
				end < base+len(value) &&
				textByteAt(value, end-base) == ' ' &&
				!strings.HasPrefix(value[end-base:], target.marker) {
				end++
			}
			operations = append(operations, commentOperation{start: target.commentStart, end: end})
			continue
		}
		insertAt := target.insertAt
		insertText := target.insertText
		if target.commentStart >= 0 {
			insertAt = target.commentStart
			insertText = target.marker
		}
		operations = append(operations, commentOperation{start: insertAt, end: insertAt, replacement: insertText})
	}
	return applyCommentOperations(value, base, operations)
}

func textByteAt(value string, offset int) byte {
	if offset < 0 || offset >= len(value) {
		return 0
	}
	return value[offset]
}

func maximalHostHasContent(text string, parsed *ParsedDocument, piece maximalCommentPiece) bool {
	excluded := make([]commentSpan, 0)
	for _, region := range parsed.Regions {
		if !isASPRegion(region.Kind) {
			continue
		}
		if region.End <= piece.start || region.Start >= piece.end {
			continue
		}
		excluded = append(excluded, commentSpan{start: max(region.Start, piece.start), end: min(region.End, piece.end)})
	}
	for offset := piece.start; offset < piece.end; {
		skip := false
		for _, span := range excluded {
			if offset >= span.start && offset < span.end {
				offset = span.end
				skip = true
				break
			}
		}
		if skip {
			continue
		}
		if !isCommentWhitespace(text[offset]) {
			return true
		}
		offset++
	}
	return false
}
