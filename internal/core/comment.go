package core

import (
	"sort"
	"strings"

	"github.com/yottonoko/classic-web-system-lsp/internal/lsp"
)

type commentTarget struct {
	line            int
	start           int
	end             int
	kind            RegionKind
	language        EmbeddedLanguage
	lineMark        string
	compactLineMark bool
	open            string
	close           string
}

type commentOperation struct {
	start       int
	end         int
	replacement string
}

type commentSpan struct {
	start int
	end   int
}

type blockCommentSpan struct {
	openStart  int
	openEnd    int
	closeStart int
	closeEnd   int
}

// CommentToggleResult is the edit plan for a Classic ASP comment toggle.
// NoOpReason is set when preserving source is safer than producing a partial edit.
type CommentToggleResult struct {
	Edits      []lsp.TextEdit
	NoOpReason string
}

// ClassicASPLineCommentEdits toggles maximal safe comment ranges across the selected lines.
func ClassicASPLineCommentEdits(uri, text string, selections []lsp.Range) []lsp.TextEdit {
	return maximalClassicASPLineCommentPlan(uri, text, selections, Settings{}).Edits
}

// ClassicASPLineCommentPlan builds maximal safe comment edits from the original document.
// Its comment-aware projection hides only outer markers while retaining ASP islands:
// IIS evaluates those islands even when the surrounding HTML is commented out.
func ClassicASPLineCommentPlan(uri, text string, selections []lsp.Range, settings Settings) CommentToggleResult {
	return maximalClassicASPLineCommentPlan(uri, text, selections, settings)
}

func classicASPLineCommentPlan(uri, text string, selections []lsp.Range, settings Settings, stackExistingLineComments bool) CommentToggleResult {
	doc := NewTextDocument(uri, "classic-asp", 0, text)
	lines := selectedCommentLines(doc, selections)
	if len(lines) == 0 {
		return CommentToggleResult{}
	}
	if edit, ok := toggleSingleASPExpressionInsideHTMLComment(doc, text, lines, settings, stackExistingLineComments); ok {
		return CommentToggleResult{Edits: []lsp.TextEdit{edit}}
	}
	probe := uncommentedCommentProbe(text)
	parsed := ParseDocument(uri, probe, settings)
	if commentSelectionTouchesDirective(doc, parsed, lines) && !commentSelectionCoversDocument(doc, lines) {
		return CommentToggleResult{NoOpReason: "directive-requires-full-document"}
	}
	if edits, ok := toggleMultilineCommentInterior(doc, text, lines); ok {
		return CommentToggleResult{Edits: edits}
	}
	wholeDocumentDirective := commentSelectionTouchesDirective(doc, parsed, lines)
	forceHTML := commentHTMLHostLines(doc, parsed, lines)
	targets := commentTargets(doc, parsed, probe, lines)
	for index := range targets {
		if wholeDocumentDirective && isASPRegion(targets[index].kind) {
			targets[index].language = normalizeServerLanguage(settings.DefaultLanguage)
		}
		if forceHTML[targets[index].line] && targets[index].language != LanguageVBScript && targets[index].language != LanguageJScript {
			targets[index].language = LanguageHTML
		}
	}
	if len(targets) == 0 {
		return CommentToggleResult{}
	}

	targetCountByLine := make(map[int]int, len(lines))
	for index := range targets {
		targetCountByLine[targets[index].line]++
	}
	for index := range targets {
		setCommentMarkers(&targets[index], targetCountByLine[targets[index].line] > 1)
	}

	commentedOperations := make([][]commentOperation, len(targets))
	uncomment := true
	hasLineCommentTarget := false
	for index, target := range targets {
		commentedOperations[index] = commentedTargetOperations(text, target)
		hasLineCommentTarget = hasLineCommentTarget || target.lineMark != ""
		if len(commentedOperations[index]) == 0 {
			uncomment = false
		}
	}
	if stackExistingLineComments && hasLineCommentTarget {
		uncomment = false
	}

	operationsByLine := make(map[int][]commentOperation, len(lines))
	seenOperations := make(map[commentOperation]struct{})
	for index, target := range targets {
		operations := commentedOperations[index]
		commented := len(operations) > 0
		if uncomment {
			for _, operation := range operations {
				appendCommentOperation(doc, operationsByLine, seenOperations, operation)
			}
			continue
		}
		if commented {
			if stackExistingLineComments && target.lineMark != "" {
				appendCommentOperation(doc, operationsByLine, seenOperations, lineCommentOperation(text, target))
			}
			continue
		}
		if target.lineMark != "" {
			if stackExistingLineComments {
				appendCommentOperation(doc, operationsByLine, seenOperations, lineCommentOperation(text, target))
			} else {
				replacement := target.lineMark + " "
				if target.compactLineMark ||
					(target.start < len(text) && isHorizontalCommentWhitespace(text[target.start])) {
					replacement = target.lineMark
				}
				appendCommentOperation(doc, operationsByLine, seenOperations, commentOperation{
					start:       target.start,
					end:         target.start,
					replacement: replacement,
				})
			}
			continue
		}
		appendCommentOperation(doc, operationsByLine, seenOperations,
			commentOperation{start: target.start, end: target.start, replacement: target.open + " "})
		appendCommentOperation(doc, operationsByLine, seenOperations,
			commentOperation{start: target.end, end: target.end, replacement: " " + target.close})
	}

	edits := make([]lsp.TextEdit, 0, len(operationsByLine))
	editLines := make([]int, 0, len(operationsByLine))
	for line := range operationsByLine {
		editLines = append(editLines, line)
	}
	sort.Ints(editLines)
	for _, line := range editLines {
		operations := operationsByLine[line]
		if len(operations) == 0 {
			continue
		}
		start := doc.lineStarts[line]
		end := lineEnd(doc.Text, line, doc.lineStarts)
		replacement := applyCommentOperations(text[start:end], start, operations)
		if replacement != text[start:end] {
			edits = append(edits, lsp.TextEdit{Range: doc.Range(start, end), NewText: replacement})
		}
	}
	return CommentToggleResult{Edits: edits}
}

func toggleSingleASPExpressionInsideHTMLComment(doc *TextDocument, text string, lines []int, settings Settings, stackExistingLineComments bool) (lsp.TextEdit, bool) {
	if len(lines) != 1 {
		return lsp.TextEdit{}, false
	}
	line := lines[0]
	lineStart := doc.lineStarts[line]
	lineEndOffset := lineEnd(text, line, doc.lineStarts)
	start, end, ok := trimmedCommentSpan(text, lineStart, lineEndOffset)
	if !ok || !strings.HasPrefix(text[start:end], "<!-- ") || !strings.HasSuffix(text[start:end], " -->") {
		return lsp.TextEdit{}, false
	}
	innerStart := start + len("<!-- ")
	innerEnd := end - len(" -->")
	inner := text[innerStart:innerEnd]
	if !strings.HasPrefix(inner, "<%") || !strings.HasSuffix(inner, "%>") {
		return lsp.TextEdit{}, false
	}
	if strings.HasPrefix(inner, "<%'=") {
		if stackExistingLineComments {
			return lsp.TextEdit{Range: doc.Range(start, end), NewText: "<!-- <%''" + inner[len("<%'"):] + " -->"}, true
		}
		return lsp.TextEdit{Range: doc.Range(start, end), NewText: "<%=" + inner[len("<%'="):]}, true
	}
	if strings.HasPrefix(inner, "<%' =") {
		if stackExistingLineComments {
			return lsp.TextEdit{Range: doc.Range(start, end), NewText: "<!-- <%''" + inner[len("<%'"):] + " -->"}, true
		}
		return lsp.TextEdit{Range: doc.Range(start, end), NewText: "<%=" + inner[len("<%' ="):]}, true
	}
	if strings.HasPrefix(inner, "<%//=") {
		if stackExistingLineComments {
			return lsp.TextEdit{Range: doc.Range(start, end), NewText: "<!-- <%////" + inner[len("<%//"):] + " -->"}, true
		}
		return lsp.TextEdit{Range: doc.Range(start, end), NewText: "<%=" + inner[len("<%//="):]}, true
	}
	if !strings.HasPrefix(inner, "<%=") {
		return lsp.TextEdit{}, false
	}
	marker := "'"
	if normalizeServerLanguage(settings.DefaultLanguage) == LanguageJScript {
		marker = "//"
	}
	return lsp.TextEdit{Range: doc.Range(start, end), NewText: "<!-- <" + "%" + marker + "=" + inner[len("<%="):] + " -->"}, true
}

func lineCommentOperation(text string, target commentTarget) commentOperation {
	replacement := target.lineMark
	commentStart := target.start
	for commentStart < len(text) && isHorizontalCommentWhitespace(text[commentStart]) {
		commentStart++
	}
	if target.compactLineMark {
		return commentOperation{start: target.start, end: target.start, replacement: replacement}
	}
	if commentStart < len(text) && strings.HasPrefix(text[commentStart:], target.lineMark) {
		return commentOperation{start: commentStart, end: commentStart, replacement: replacement}
	}
	if target.start >= len(text) || !isHorizontalCommentWhitespace(text[target.start]) {
		replacement += " "
	}
	return commentOperation{start: target.start, end: target.start, replacement: replacement}
}

// toggleMultilineCommentInterior keeps the unselected portions of an enclosing
// block comment disabled. A second toggle recognizes the two boundary markers and
// joins the same comment again, so the operation is stable without metadata.
func toggleMultilineCommentInterior(doc *TextDocument, text string, lines []int) ([]lsp.TextEdit, bool) {
	if len(lines) == 0 {
		return nil, false
	}
	firstLine, lastLine := lines[0], lines[len(lines)-1]
	firstStart := doc.lineStarts[firstLine]
	lastEnd := lineEnd(text, lastLine, doc.lineStarts)

	for _, markers := range [][2]string{{"<!--", "-->"}, {"/*", "*/"}} {
		if firstLine > 0 && lastLine+1 < len(doc.lineStarts) &&
			strings.TrimSpace(text[doc.lineStarts[firstLine-1]:lineEnd(text, firstLine-1, doc.lineStarts)]) == markers[1] &&
			strings.TrimSpace(text[doc.lineStarts[lastLine+1]:lineEnd(text, lastLine+1, doc.lineStarts)]) == markers[0] {
			beforeStart := doc.lineStarts[firstLine-1]
			afterStart := doc.lineStarts[lastLine+1]
			afterEnd := lineEnd(text, lastLine+1, doc.lineStarts)
			if lastLine+2 < len(doc.lineStarts) {
				afterEnd = doc.lineStarts[lastLine+2]
			}
			return []lsp.TextEdit{
				{Range: doc.Range(beforeStart, firstStart), NewText: ""},
				{Range: doc.Range(afterStart, afterEnd), NewText: ""},
			}, true
		}
	}

	selectedStart, selectedEnd := firstStart, lastEnd
	aspRanges := commentASPExclusionRanges(text)
	var enclosing *blockCommentSpan
	var open, close string
	for _, markers := range [][2]string{{"<!--", "-->"}, {"/*", "*/"}} {
		for _, span := range blockCommentSpans(text, markers[0], markers[1], aspRanges) {
			if span.openEnd > selectedStart || span.closeStart < selectedEnd ||
				doc.PositionAt(span.openStart).Line >= firstLine || doc.PositionAt(span.closeStart).Line <= lastLine {
				continue
			}
			if enclosing == nil || preferEnclosingBlockComment(*enclosing, open, close, span, markers[0], markers[1]) {
				candidate := span
				enclosing, open, close = &candidate, markers[0], markers[1]
			}
		}
	}
	if enclosing == nil {
		return nil, false
	}
	beforeEnding := commentLineEndingAt(text, selectedStart)
	afterEnding := commentLineEndingAt(text, selectedEnd)
	if beforeEnding == "" {
		beforeEnding = afterEnding
	}
	if afterEnding == "" {
		afterEnding = beforeEnding
	}
	return []lsp.TextEdit{
		{Range: doc.Range(selectedStart, selectedStart), NewText: close + beforeEnding},
		{Range: doc.Range(selectedEnd, selectedEnd), NewText: afterEnding + open},
	}, true
}

func commentSelectionTouchesDirective(doc *TextDocument, parsed *ParsedDocument, lines []int) bool {
	selected := make(map[int]struct{}, len(lines))
	for _, line := range lines {
		selected[line] = struct{}{}
	}
	for _, region := range parsed.Regions {
		if region.Kind != RegionASPDirective {
			continue
		}
		for line := doc.PositionAt(region.Start).Line; line <= doc.PositionAt(max(region.End-1, region.Start)).Line; line++ {
			if _, ok := selected[line]; ok {
				return true
			}
		}
	}
	return false
}

func commentSelectionCoversDocument(doc *TextDocument, lines []int) bool {
	selected := make(map[int]struct{}, len(lines))
	for _, line := range lines {
		selected[line] = struct{}{}
	}
	for line, start := range doc.lineStarts {
		if strings.TrimSpace(doc.Text[start:lineEnd(doc.Text, line, doc.lineStarts)]) == "" {
			continue
		}
		if _, ok := selected[line]; !ok {
			return false
		}
	}
	return true
}

// commentHTMLHostLines keeps a complete script/style element in one client
// comment layer. Commenting its tags while leaving body lines as JS/CSS comments
// would otherwise expose those lines as HTML text.
func commentHTMLHostLines(doc *TextDocument, parsed *ParsedDocument, lines []int) map[int]bool {
	selected := make(map[int]struct{}, len(lines))
	for _, line := range lines {
		selected[line] = struct{}{}
	}
	forced := make(map[int]bool)
	for _, region := range parsed.Regions {
		if region.Kind != RegionClientScript && region.Kind != RegionStyle {
			continue
		}
		startLine := doc.PositionAt(region.Start).Line
		endLine := doc.PositionAt(max(region.End-1, region.Start)).Line
		if _, ok := selected[startLine]; !ok {
			continue
		}
		if _, ok := selected[endLine]; !ok {
			continue
		}
		for line := startLine; line <= endLine; line++ {
			if _, ok := selected[line]; ok {
				forced[line] = true
			}
		}
	}
	return forced
}

func selectedCommentLines(doc *TextDocument, selections []lsp.Range) []int {
	if len(selections) == 0 {
		selections = []lsp.Range{{}}
	}
	seen := make(map[int]struct{})
	var lines []int
	if len(doc.lineStarts) == 0 {
		return nil
	}
	for _, selection := range selections {
		if selection.Start.Line >= len(doc.lineStarts) || selection.End.Line < 0 {
			continue
		}
		startLine := max(selection.Start.Line, 0)
		endLine := selection.End.Line
		if selection.End.Character == 0 && endLine > startLine {
			endLine--
		}
		startLine = min(startLine, len(doc.lineStarts)-1)
		endLine = min(max(endLine, 0), len(doc.lineStarts)-1)
		if endLine < startLine {
			continue
		}
		for line := startLine; line <= endLine; line++ {
			if _, exists := seen[line]; exists {
				continue
			}
			seen[line] = struct{}{}
			lines = append(lines, line)
		}
	}
	sort.Ints(lines)
	return lines
}

func commentTargets(doc *TextDocument, parsed *ParsedDocument, probe string, lines []int) []commentTarget {
	var targets []commentTarget
	for _, line := range lines {
		lineStart := doc.lineStarts[line]
		lineEndOffset := lineEnd(doc.Text, line, doc.lineStarts)
		if strings.TrimSpace(probe[lineStart:lineEndOffset]) == "" {
			continue
		}

		for index := range parsed.Regions {
			region := &parsed.Regions[index]
			if !isASPRegion(region.Kind) || region.End <= lineStart || region.Start >= lineEndOffset {
				continue
			}
			contentStart := max(region.ContentStart, lineStart)
			contentEnd := min(region.ContentEnd, lineEndOffset)
			if region.Start >= lineStart && region.Start < lineEndOffset {
				contentStart = region.Start + 2
			}
			start, end, ok := trimmedCommentSpan(probe, contentStart, contentEnd)
			if !ok && region.Start >= lineStart && region.Start+2 < region.End {
				start, end, ok = region.Start+2, region.Start+2, true
			}
			if ok {
				if region.Start >= lineStart && region.Start < lineEndOffset {
					start = region.Start + 2
				}
				targets = append(targets, commentTarget{
					line:            line,
					start:           start,
					end:             end,
					kind:            region.Kind,
					language:        region.Language,
					compactLineMark: region.Kind == RegionASPExpression && region.Start >= lineStart,
				})
			}
		}

		boundaries := []int{lineStart, lineEndOffset}
		for index := range parsed.Regions {
			region := &parsed.Regions[index]
			if isASPRegion(region.Kind) {
				boundaries = appendCommentBoundary(boundaries, region.Start, lineStart, lineEndOffset)
				boundaries = appendCommentBoundary(boundaries, region.End, lineStart, lineEndOffset)
				continue
			}
			if isEmbeddedCommentRegion(region.Kind) {
				boundaries = appendCommentBoundary(boundaries, region.ContentStart, lineStart, lineEndOffset)
				boundaries = appendCommentBoundary(boundaries, region.ContentEnd, lineStart, lineEndOffset)
			}
		}
		sort.Ints(boundaries)
		boundaries = compactCommentBoundaries(boundaries)

		for index := 0; index+1 < len(boundaries); index++ {
			start, end := boundaries[index], boundaries[index+1]
			if start >= end || aspRegionAt(parsed, start, end) != nil {
				continue
			}
			language := effectiveCommentLanguageAt(parsed, start, end)
			trimmedStart, trimmedEnd, ok := trimmedCommentSpan(probe, start, end)
			if !ok {
				continue
			}
			if len(targets) > 0 {
				last := &targets[len(targets)-1]
				if last.line == line && last.language == language && last.end == trimmedStart {
					last.end = trimmedEnd
					continue
				}
			}
			targets = append(targets, commentTarget{line: line, start: trimmedStart, end: trimmedEnd, language: language})
		}
	}
	sort.SliceStable(targets, func(i, j int) bool {
		if targets[i].line != targets[j].line {
			return targets[i].line < targets[j].line
		}
		return targets[i].start < targets[j].start
	})
	return targets
}

func setCommentMarkers(target *commentTarget, mixed bool) {
	switch target.language {
	case LanguageVBScript, LanguageASPDirective:
		target.lineMark = "'"
	case LanguageJScript:
		target.lineMark = "//"
	case LanguageJavaScript:
		if !mixed {
			target.lineMark = "//"
		} else {
			target.open, target.close = "/*", "*/"
		}
	case LanguageCSS:
		target.open, target.close = "/*", "*/"
	default:
		target.open, target.close = "<!--", "-->"
	}
}

func commentedTargetOperations(text string, target commentTarget) []commentOperation {
	if target.lineMark != "" {
		if strings.HasPrefix(text[target.start:], target.lineMark+" ") {
			return []commentOperation{{start: target.start, end: target.start + len(target.lineMark) + 1}}
		} else if strings.HasPrefix(text[target.start:], target.lineMark) {
			return []commentOperation{{start: target.start, end: target.start + len(target.lineMark)}}
		} else if target.start >= len(target.lineMark)+1 && text[target.start-len(target.lineMark)-1:target.start] == target.lineMark+" " {
			return []commentOperation{{start: target.start - len(target.lineMark) - 1, end: target.start}}
		}
		if target.language == LanguageVBScript {
			if end, ok := vbscriptREMEnd(text, target.start, target.end); ok {
				return []commentOperation{{start: target.start, end: end}}
			}
		}
	}

	if target.open != "" {
		openStart, openEnd, openOK := surroundingOpenMarker(text, target.start, target.open)
		closeStart, closeEnd, closeOK := surroundingCloseMarker(text, target.end, target.close)
		if openOK && closeOK && openEnd <= target.start && closeStart >= target.end {
			return []commentOperation{
				{start: openStart, end: openEnd},
				{start: closeStart, end: closeEnd},
			}
		}
	}

	aspRanges := commentASPExclusionRanges(text)
	var enclosing *blockCommentSpan
	var enclosingOpen, enclosingClose string
	for _, markers := range [][2]string{{"<!--", "-->"}, {"/*", "*/"}} {
		for _, span := range blockCommentSpans(text, markers[0], markers[1], aspRanges) {
			if span.openEnd > target.start || span.closeStart < target.end {
				continue
			}
			if enclosing == nil || preferEnclosingBlockComment(*enclosing, enclosingOpen, enclosingClose, span, markers[0], markers[1]) {
				candidate := span
				enclosing = &candidate
				enclosingOpen, enclosingClose = markers[0], markers[1]
			}
		}
	}
	if enclosing == nil {
		return nil
	}

	openEnd := enclosing.openEnd
	for openEnd < enclosing.closeStart && isHorizontalCommentWhitespace(text[openEnd]) {
		openEnd++
	}
	closeStart := enclosing.closeStart
	for closeStart > enclosing.openEnd && isHorizontalCommentWhitespace(text[closeStart-1]) {
		closeStart--
	}
	lineStart := strings.LastIndexByte(text[:enclosing.closeStart], '\n') + 1
	if strings.Trim(text[lineStart:enclosing.closeStart], " \t\r") == "" {
		closeStart = enclosing.closeStart
	}
	return []commentOperation{
		{start: enclosing.openStart, end: openEnd},
		{start: closeStart, end: enclosing.closeEnd},
	}
}

func preferEnclosingBlockComment(
	current blockCommentSpan,
	currentOpen, currentClose string,
	candidate blockCommentSpan,
	candidateOpen, candidateClose string,
) bool {
	// Cross-language marker pairs can nest textually; keep their outer span while same-language nesting stays innermost.
	currentContainsCandidate := current.openStart <= candidate.openStart && candidate.closeEnd <= current.closeEnd
	candidateContainsCurrent := candidate.openStart <= current.openStart && current.closeEnd <= candidate.closeEnd
	if candidateContainsCurrent != currentContainsCandidate {
		return candidateContainsCurrent
	}
	if currentOpen == candidateOpen && currentClose == candidateClose {
		return candidate.closeEnd-candidate.openStart < current.closeEnd-current.openStart
	}
	return candidate.openStart < current.openStart
}

func vbscriptREMEnd(text string, start, end int) (int, bool) {
	for start < end && isHorizontalCommentWhitespace(text[start]) {
		start++
	}
	if end-start < len("REM") || !strings.EqualFold(text[start:start+len("REM")], "REM") {
		return 0, false
	}
	if start+len("REM") < end && !isCommentWhitespace(text[start+len("REM")]) {
		return 0, false
	}
	return start + len("REM"), true
}

func surroundingOpenMarker(text string, start int, marker string) (int, int, bool) {
	markerEnd := start
	for markerEnd > 0 && isHorizontalCommentWhitespace(text[markerEnd-1]) {
		markerEnd--
	}
	markerStart := markerEnd - len(marker)
	if markerStart >= 0 && text[markerStart:markerEnd] == marker {
		return markerStart, start, true
	}
	return 0, 0, false
}

func surroundingCloseMarker(text string, end int, marker string) (int, int, bool) {
	markerStart := end
	for markerStart < len(text) && isHorizontalCommentWhitespace(text[markerStart]) {
		markerStart++
	}
	markerEnd := markerStart + len(marker)
	if markerEnd <= len(text) && text[markerStart:markerEnd] == marker {
		return end, markerEnd, true
	}
	return 0, 0, false
}

func blockCommentSpans(text, open, close string, excluded []commentSpan) []blockCommentSpan {
	var spans []blockCommentSpan
	var openOffsets []int
	for offset := 0; offset < len(text); {
		nextOpen := commentMarkerOutsideRanges(text, offset, open, excluded)
		nextClose := commentMarkerOutsideRanges(text, offset, close, excluded)
		if nextOpen >= 0 && (nextClose < 0 || nextOpen < nextClose) {
			openOffsets = append(openOffsets, nextOpen)
			offset = nextOpen + len(open)
			continue
		}
		if nextClose < 0 {
			break
		}
		if len(openOffsets) > 0 {
			openStart := openOffsets[len(openOffsets)-1]
			openOffsets = openOffsets[:len(openOffsets)-1]
			spans = append(spans, blockCommentSpan{
				openStart:  openStart,
				openEnd:    openStart + len(open),
				closeStart: nextClose,
				closeEnd:   nextClose + len(close),
			})
		}
		offset = nextClose + len(close)
	}
	return spans
}

func appendCommentOperation(doc *TextDocument, operationsByLine map[int][]commentOperation, seen map[commentOperation]struct{}, operation commentOperation) {
	if _, exists := seen[operation]; exists {
		return
	}
	seen[operation] = struct{}{}
	line := doc.PositionAt(operation.start).Line
	operationsByLine[line] = append(operationsByLine[line], operation)
}

func isHorizontalCommentWhitespace(value byte) bool {
	return value == ' ' || value == '\t'
}

func commentLineEndingAt(text string, offset int) string {
	if offset >= 0 && offset+1 < len(text) && text[offset] == '\r' && text[offset+1] == '\n' {
		return "\r\n"
	}
	if offset >= 0 && offset < len(text) && text[offset] == '\n' {
		return "\n"
	}
	if offset >= 2 && text[offset-2:offset] == "\r\n" {
		return "\r\n"
	}
	if offset >= 1 && text[offset-1] == '\n' {
		return "\n"
	}
	return ""
}

func applyCommentOperations(lineText string, lineStart int, operations []commentOperation) string {
	sort.SliceStable(operations, func(i, j int) bool {
		if operations[i].start != operations[j].start {
			return operations[i].start < operations[j].start
		}
		return operations[i].end < operations[j].end
	})
	var result strings.Builder
	result.Grow(len(lineText) + len(operations)*5)
	cursor := 0
	for _, operation := range operations {
		start := operation.start - lineStart
		end := operation.end - lineStart
		if start < cursor {
			continue
		}
		result.WriteString(lineText[cursor:start])
		result.WriteString(operation.replacement)
		cursor = end
	}
	result.WriteString(lineText[cursor:])
	return result.String()
}

func uncommentedCommentProbe(text string) string {
	probe := []byte(text)
	aspRanges := commentASPExclusionRanges(text)
	for _, markers := range [][2]string{{"<!--", "-->"}, {"/*", "*/"}} {
		depth := 0
		openOffset := -1
		for offset := 0; offset < len(text); {
			nextOpen := commentMarkerOutsideRanges(text, offset, markers[0], aspRanges)
			nextClose := commentMarkerOutsideRanges(text, offset, markers[1], aspRanges)
			if nextOpen >= 0 && (nextClose < 0 || nextOpen < nextClose) {
				if depth == 0 {
					openOffset = nextOpen
				}
				depth++
				offset = nextOpen + len(markers[0])
				continue
			}
			if nextClose < 0 || depth == 0 {
				break
			}
			depth--
			if depth == 0 {
				contentStart := openOffset + len(markers[0])
				for contentStart < nextClose && isHorizontalCommentWhitespace(text[contentStart]) {
					contentStart++
				}
				contentEnd := nextClose
				for contentEnd > contentStart && isHorizontalCommentWhitespace(text[contentEnd-1]) {
					contentEnd--
				}
				content := text[contentStart:contentEnd]
				if allASPOpensAreClientText(content) {
					for index := openOffset; index < contentStart; index++ {
						probe[index] = ' '
					}
					for index := contentEnd; index < nextClose+len(markers[1]); index++ {
						probe[index] = ' '
					}
				}
			}
			offset = nextClose + len(markers[1])
		}
	}
	return string(probe)
}
