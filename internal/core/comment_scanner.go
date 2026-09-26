package core

import "strings"

func commentASPExclusionRanges(text string) []commentSpan {
	var ranges []commentSpan
	for cursor := 0; cursor < len(text); {
		open := indexASPOpen(text, cursor)
		if open < 0 {
			break
		}
		close := aspCloseDelimiter(text, open)
		if close < 0 {
			ranges = append(ranges, commentSpan{start: open, end: len(text)})
			break
		}
		ranges = append(ranges, commentSpan{start: open, end: close + 2})
		cursor = close + 2
	}
	return ranges
}

func commentMarkerOutsideRanges(text string, offset int, marker string, ranges []commentSpan) int {
	for offset < len(text) {
		found := strings.Index(text[offset:], marker)
		if found < 0 {
			return -1
		}
		found += offset
		insideASP := false
		for _, candidate := range ranges {
			if found >= candidate.start && found < candidate.end {
				offset = candidate.end
				insideASP = true
				break
			}
		}
		if !insideASP {
			if commentMarkerInsideHTMLAttribute(text, found, ranges) ||
				commentMarkerInsideJavaScriptHTMLLineComment(text, found, marker, ranges) ||
				commentMarkerInsideEmbeddedQuotedText(text, found, ranges) {
				offset = found + len(marker)
				continue
			}
			return found
		}
	}
	return -1
}

func commentMarkerInsideHTMLAttribute(text string, offset int, excluded []commentSpan) bool {
	start := strings.LastIndexByte(text[:offset], '<')
	if start < 0 {
		return false
	}
	inTag := false
	var quote byte
	for index := start; index < offset; {
		skipped := false
		for _, candidate := range excluded {
			if index >= candidate.start && index < candidate.end {
				index = candidate.end
				skipped = true
				break
			}
		}
		if skipped {
			continue
		}

		value := text[index]
		if quote != 0 {
			if value == quote && !isEscaped(text, index) {
				quote = 0
			}
			index++
			continue
		}
		if inTag {
			switch value {
			case '"', '\'':
				quote = value
			case '>':
				inTag = false
			}
			index++
			continue
		}
		if value == '<' && index+1 < offset && isHTMLTagStartByte(text[index+1]) {
			inTag = true
		}
		index++
	}
	return inTag && quote != 0
}

func isHTMLTagStartByte(value byte) bool {
	return value >= 'a' && value <= 'z' ||
		value >= 'A' && value <= 'Z' ||
		value == '/' || value == '!' || value == '?'
}

func commentMarkerInsideJavaScriptHTMLLineComment(text string, offset int, marker string, excluded []commentSpan) bool {
	if marker != "<!--" && marker != "-->" {
		return false
	}
	for _, candidate := range excluded {
		if offset >= candidate.start && offset < candidate.end {
			return false
		}
	}
	openEnd, ok := commentEmbeddedElementContentStart(text, offset, "script")
	if !ok {
		return false
	}
	lineStart := strings.LastIndexAny(text[:offset], "\r\n") + 1
	contentStart := max(lineStart, openEnd)
	for index := contentStart; index < offset; index++ {
		if !isHorizontalCommentWhitespace(text[index]) {
			return false
		}
	}
	return true
}

func commentMarkerInsideEmbeddedQuotedText(text string, offset int, excluded []commentSpan) bool {
	if _, ok := commentEmbeddedElementContentStart(text, offset, "script"); ok {
		return commentMarkerInsideQuotedLine(text, offset, excluded) ||
			commentMarkerInsideJavaScriptRegex(text, offset, excluded)
	}
	if _, ok := commentEmbeddedElementContentStart(text, offset, "style"); ok {
		return commentMarkerInsideQuotedLine(text, offset, excluded)
	}
	return false
}

func commentEmbeddedElementContentStart(text string, offset int, tag string) (int, bool) {
	openStart := lastEmbeddedTagStart(text, "<"+tag, offset)
	if openStart < 0 {
		return 0, false
	}
	closeStart := lastEmbeddedTagStart(text, "</"+tag, offset)
	if closeStart > openStart {
		return 0, false
	}
	openEnd := includeOpeningTagEnd(text, openStart)
	if openEnd < 0 || openEnd > offset {
		return 0, false
	}
	return openEnd, true
}

func lastEmbeddedTagStart(text, needle string, end int) int {
	for end > 0 {
		start := lastCaseInsensitiveIndex(text, needle, end)
		if start < 0 {
			return -1
		}
		if commentMarkerInsideHTMLAttribute(text, start, nil) {
			end = start
			continue
		}
		nameEnd := start + len(needle)
		if nameEnd < len(text) {
			next := text[nameEnd]
			if next != '>' && !isHTMLWhitespace(next) {
				end = start
				continue
			}
		}
		return start
	}
	return -1
}

func lastCaseInsensitiveIndex(text, needle string, end int) int {
	end = min(end, len(text))
	for index := end - len(needle); index >= 0; index-- {
		if strings.EqualFold(text[index:index+len(needle)], needle) {
			return index
		}
	}
	return -1
}

func commentMarkerInsideQuotedLine(text string, offset int, excluded []commentSpan) bool {
	lineStart := strings.LastIndexAny(text[:offset], "\r\n") + 1
	var quote byte
	for index := 0; index < offset; index++ {
		skipped := false
		for _, candidate := range excluded {
			if index >= candidate.start && index < candidate.end {
				if quote != '`' && strings.ContainsAny(text[index:candidate.end], "\r\n") {
					quote = 0
				}
				index = candidate.end - 1
				skipped = true
				break
			}
		}
		if skipped {
			continue
		}
		value := text[index]
		if (value == '\r' || value == '\n') && quote != '`' {
			quote = 0
			continue
		}
		if quote != 0 {
			if value == quote && !isEscaped(text, index) {
				quote = 0
			}
			continue
		}
		if value == '/' && index+1 < offset && text[index+1] == '/' &&
			!isEscaped(text, index) && commentLineCommentStartsAt(text, index, lineStart) {
			return true
		}
		if (value == '"' || value == '\'' || value == '`') && !isEscaped(text, index) {
			quote = value
		}
	}
	return quote != 0
}

func commentMarkerInsideJavaScriptRegex(text string, offset int, excluded []commentSpan) bool {
	lineStart := strings.LastIndexAny(text[:offset], "\r\n") + 1
	var quote byte
	regex := false
	regexClass := false
	canStartRegex := true
	for index := lineStart; index < offset; {
		skipped := false
		for _, candidate := range excluded {
			if index >= candidate.start && index < candidate.end {
				index = candidate.end
				quote = 0
				regex = false
				regexClass = false
				canStartRegex = true
				skipped = true
				break
			}
		}
		if skipped {
			continue
		}

		value := text[index]
		if quote != 0 {
			if value == quote && !isEscaped(text, index) {
				quote = 0
			}
			index++
			continue
		}
		if regex {
			if value == '\\' {
				index += min(2, offset-index)
				continue
			}
			if value == '[' {
				regexClass = true
			} else if value == ']' && regexClass {
				regexClass = false
			} else if value == '/' && !regexClass {
				regex = false
				canStartRegex = false
			}
			index++
			continue
		}
		if isHorizontalCommentWhitespace(value) {
			index++
			continue
		}
		if value == '/' && index+1 < offset && text[index+1] == '/' &&
			commentLineCommentStartsAt(text, index, lineStart) {
			return true
		}
		if value == '/' && index+1 < offset && text[index+1] == '*' {
			canStartRegex = true
			index++
			continue
		}
		if value == '/' {
			if index > lineStart && (text[index-1] == '*' || text[index-1] == '<') {
				canStartRegex = false
				index++
				continue
			}
			if canStartRegex {
				regex = true
				index++
				continue
			}
			canStartRegex = true
			index++
			continue
		}
		if value == '"' || value == '\'' || value == '`' {
			quote = value
			index++
			continue
		}
		if isJavaScriptIdentifierByte(value) {
			start := index
			for index < offset && isJavaScriptIdentifierByte(text[index]) {
				index++
			}
			canStartRegex = javascriptRegexCanFollowKeyword(text[start:index])
			continue
		}
		switch value {
		case ')', ']', '}':
			canStartRegex = false
		default:
			canStartRegex = true
		}
		index++
	}
	return regex
}

func isJavaScriptIdentifierByte(value byte) bool {
	return value >= 'a' && value <= 'z' ||
		value >= 'A' && value <= 'Z' ||
		value >= '0' && value <= '9' ||
		value == '_' || value == '$'
}

func javascriptRegexCanFollowKeyword(keyword string) bool {
	switch keyword {
	case "await", "case", "delete", "do", "else", "in", "instanceof", "new", "of", "return", "throw", "typeof", "void", "yield":
		return true
	default:
		return false
	}
}

func commentLineCommentStartsAt(text string, offset, lineStart int) bool {
	if offset <= lineStart {
		return true
	}
	if isHorizontalCommentWhitespace(text[offset-1]) {
		return true
	}
	switch text[offset-1] {
	case ';', '{', '}', '(', ')', '[', ']', '=', ',', '!', '&', '|', '?', '+', '-', '*', '%', '<', '>':
		return true
	default:
		return false
	}
}

func allASPOpensAreClientText(text string) bool {
	for cursor := 0; cursor < len(text); {
		open := strings.Index(text[cursor:], "<%")
		if open < 0 {
			return true
		}
		open += cursor
		if !aspOpenLooksLikeQuotedLiteral(text, open) && !aspOpenLooksLikeClientCommentSince(text, open, 0) {
			return false
		}
		cursor = open + 2
	}
	return true
}

func trimmedCommentSpan(text string, start, end int) (int, int, bool) {
	for start < end && isCommentWhitespace(text[start]) {
		start++
	}
	for end > start && isCommentWhitespace(text[end-1]) {
		end--
	}
	return start, end, start < end
}

func isCommentWhitespace(value byte) bool {
	return value == ' ' || value == '\t' || value == '\r' || value == '\n'
}

func appendCommentBoundary(boundaries []int, value, start, end int) []int {
	if value > start && value < end {
		return append(boundaries, value)
	}
	return boundaries
}

func compactCommentBoundaries(boundaries []int) []int {
	result := boundaries[:0]
	for _, boundary := range boundaries {
		if len(result) == 0 || result[len(result)-1] != boundary {
			result = append(result, boundary)
		}
	}
	return result
}

func isASPRegion(kind RegionKind) bool {
	return kind == RegionASPBlock || kind == RegionASPExpression || kind == RegionASPDirective
}

func isEmbeddedCommentRegion(kind RegionKind) bool {
	return kind == RegionStyle || kind == RegionStyleAttribute || kind == RegionClientScript || kind == RegionServerScript
}

func aspRegionAt(parsed *ParsedDocument, start, end int) *Region {
	for index := range parsed.Regions {
		region := &parsed.Regions[index]
		if isASPRegion(region.Kind) && start < region.End && end > region.Start {
			return region
		}
	}
	return nil
}

func effectiveCommentLanguageAt(parsed *ParsedDocument, start, end int) EmbeddedLanguage {
	offset := start + (end-start)/2
	bestSpan := int(^uint(0) >> 1)
	language := LanguageHTML
	for _, region := range parsed.Regions {
		if !isEmbeddedCommentRegion(region.Kind) || offset < region.ContentStart || offset >= region.ContentEnd {
			continue
		}
		if span := region.ContentEnd - region.ContentStart; span < bestSpan {
			bestSpan = span
			language = region.Language
		}
	}
	return language
}
