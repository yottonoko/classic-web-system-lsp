package core

import (
	"strings"

	"github.com/yottonoko/classic-web-system-lsp/internal/lsp"
)

const disabledIncludeMarker = "asp-lsp-disabled-include:"

type maximalCommentPieceKind uint8

const (
	maximalCommentHost maximalCommentPieceKind = iota
	maximalCommentCSS
	maximalCommentJavaScript
	maximalCommentServer
	maximalCommentInclude
)

type maximalCommentPiece struct {
	start              int
	end                int
	kind               maximalCommentPieceKind
	includeMarkerStart int
	includeDisabled    bool
}

type maximalPreparedPiece struct {
	piece       maximalCommentPiece
	lineTargets []maximalLineTarget
	hasHost     bool
	actionable  bool
	commented   bool
}

type maximalIncludeDirective struct {
	start       int
	end         int
	markerStart int
	disabled    bool
}

type maximalLineTarget struct {
	marker               string
	commentStart         int
	commentEnd           int
	insertAt             int
	insertText           string
	preserveFollowingGap bool
	normalizeExpression  bool
}

func maximalClassicASPLineCommentPlan(uri, text string, selections []lsp.Range, settings Settings) CommentToggleResult {
	doc := NewTextDocument(uri, "classic-asp", 0, text)
	intervals, lines := maximalCommentIntervals(doc, selections)
	if len(intervals) == 0 {
		return CommentToggleResult{}
	}
	if maximalSelectionUsesLegacyFragmentComments(text, intervals) {
		return classicASPLineCommentPlan(uri, text, selections, settings, false)
	}

	probe := uncommentedCommentProbe(text)
	parsed := ParseDocument(uri, probe, settings)
	if len(parsed.Errors) > 0 {
		return CommentToggleResult{}
	}
	touchesDirective := commentSelectionTouchesDirective(doc, parsed, lines)
	if touchesDirective {
		if !commentSelectionCoversDocument(doc, lines) {
			return CommentToggleResult{NoOpReason: "directive-requires-full-document"}
		}
		clone := *parsed
		clone.Regions = append([]Region(nil), parsed.Regions...)
		for index := range clone.Regions {
			if isASPRegion(clone.Regions[index].Kind) {
				clone.Regions[index].Language = normalizeServerLanguage(settings.DefaultLanguage)
			}
		}
		parsed = &clone
	}
	if edits, ok := toggleMultilineCommentInterior(doc, text, lines); ok {
		return CommentToggleResult{Edits: edits}
	}
	if maximalSelectionTouchesPartialBlockComment(text, intervals) {
		return CommentToggleResult{NoOpReason: "unsafe-structure"}
	}

	includes := scanMaximalIncludeDirectives(doc, text)
	piecesByInterval := make([][]maximalPreparedPiece, len(intervals))
	actionableCount := 0
	for index, interval := range intervals {
		pieces := maximalPiecesForInterval(text, parsed, includes, interval)
		prepared := make([]maximalPreparedPiece, 0, len(pieces))
		for _, piece := range pieces {
			candidate := prepareMaximalPiece(doc, text, parsed, piece, settings)
			prepared = append(prepared, candidate)
			if candidate.actionable {
				actionableCount++
			}
		}
		piecesByInterval[index] = prepared
	}
	if actionableCount == 0 {
		return CommentToggleResult{}
	}

	uncomment := true
	for _, pieces := range piecesByInterval {
		for _, piece := range pieces {
			if piece.actionable && !piece.commented {
				uncomment = false
				break
			}
		}
		if !uncomment {
			break
		}
	}

	edits := make([]lsp.TextEdit, 0, len(intervals))
	for index, interval := range intervals {
		replacement := transformMaximalInterval(text, interval, piecesByInterval[index], uncomment)
		if replacement == text[interval.start:interval.end] {
			continue
		}
		edits = append(edits, lsp.TextEdit{
			Range:   doc.Range(interval.start, interval.end),
			NewText: replacement,
		})
	}
	return CommentToggleResult{Edits: edits}
}

func maximalSelectionUsesLegacyFragmentComments(text string, intervals []commentSpan) bool {
	for _, interval := range intervals {
		selected := strings.TrimSpace(text[interval.start:interval.end])
		if !strings.HasPrefix(selected, "<!--") {
			continue
		}
		if maximalSelectionHasCompleteHTMLWrapper(selected) {
			continue
		}
		for cursor := len("<!--"); cursor < len(selected); {
			open := indexASPOpen(selected, cursor)
			if open < 0 {
				break
			}
			if strings.Contains(selected[:open], "-->") {
				close := aspCloseDelimiter(selected, open)
				if close >= 0 && strings.Contains(selected[close+2:], "<!--") {
					return true
				}
			}
			cursor = open + 2
		}
	}
	return false
}

func maximalSelectionTouchesPartialBlockComment(text string, intervals []commentSpan) bool {
	aspRanges := commentASPExclusionRanges(text)
	for _, markers := range [][2]string{{"<!--", "-->"}, {"/*", "*/"}} {
		for _, span := range blockCommentSpans(text, markers[0], markers[1], aspRanges) {
			for _, interval := range intervals {
				intersects := span.openStart < interval.end && span.closeEnd > interval.start
				if !intersects {
					continue
				}
				fullySelected := interval.start <= span.openStart && interval.end >= span.closeEnd
				strictlyInterior := interval.start >= span.openEnd && interval.end <= span.closeStart
				if !fullySelected && !strictlyInterior {
					return true
				}
			}
		}
	}
	return false
}

func maximalSelectionHasCompleteHTMLWrapper(selected string) bool {
	if !strings.HasPrefix(selected, "<!--") || !strings.HasSuffix(selected, "-->") {
		return false
	}
	hasCommentSpan := false
	hasTrailingComment := false
	for _, span := range blockCommentSpans(selected, "<!--", "-->", commentASPExclusionRanges(selected)) {
		hasCommentSpan = true
		if span.openStart == 0 && span.closeEnd == len(selected) {
			return true
		}
		if span.closeEnd == len(selected) {
			hasTrailingComment = true
		}
	}
	// A raw HTML terminator inside the generated wrapper can consume its opener,
	// leaving the final wrapper terminator unmatched by the scanner. A separate
	// trailing fragment always has a span ending at the selected interval end.
	return hasCommentSpan && !hasTrailingComment
}

func maximalCommentIntervals(doc *TextDocument, selections []lsp.Range) ([]commentSpan, []int) {
	lines := selectedCommentLines(doc, selections)
	if len(lines) == 0 {
		return nil, nil
	}
	intervals := make([]commentSpan, 0, len(lines))
	first, previous := lines[0], lines[0]
	appendInterval := func(startLine, endLine int) {
		start := doc.lineStarts[startLine]
		end := lineEnd(doc.Text, endLine, doc.lineStarts)
		if start < end {
			intervals = append(intervals, commentSpan{start: start, end: end})
		}
	}
	for _, line := range lines[1:] {
		if line == previous+1 {
			previous = line
			continue
		}
		appendInterval(first, previous)
		first, previous = line, line
	}
	appendInterval(first, previous)
	return intervals, lines
}

func maximalPiecesForInterval(
	text string,
	parsed *ParsedDocument,
	includes []maximalIncludeDirective,
	interval commentSpan,
) []maximalCommentPiece {
	first, last, ok := trimmedCommentSpan(text, interval.start, interval.end)
	if !ok {
		return nil
	}
	startRegion := maximalBoundaryRegionAt(parsed, first)
	endRegion := maximalBoundaryRegionAt(parsed, last-1)

	var pieces []maximalCommentPiece
	if startRegion != nil && endRegion == startRegion {
		pieces = append(pieces, maximalCommentPiece{
			start: interval.start,
			end:   interval.end,
			kind:  maximalKindForRegion(startRegion),
		})
		return splitMaximalHostPiecesAtIncludes(pieces, includes)
	}

	cursor := interval.start
	if startRegion != nil {
		end := min(interval.end, startRegion.ContentEnd)
		pieces = append(pieces, maximalCommentPiece{
			start: interval.start,
			end:   end,
			kind:  maximalKindForRegion(startRegion),
		})
		cursor = max(cursor, min(interval.end, startRegion.End))
	}

	hostEnd := interval.end
	if endRegion != nil {
		hostEnd = min(hostEnd, endRegion.Start)
	}
	if cursor < hostEnd {
		pieces = append(pieces, maximalCommentPiece{start: cursor, end: hostEnd, kind: maximalCommentHost})
	}
	if endRegion != nil {
		start := max(interval.start, endRegion.ContentStart)
		if start < interval.end {
			pieces = append(pieces, maximalCommentPiece{
				start: start,
				end:   interval.end,
				kind:  maximalKindForRegion(endRegion),
			})
		}
	}
	if len(pieces) == 0 {
		pieces = append(pieces, maximalCommentPiece{start: interval.start, end: interval.end, kind: maximalCommentHost})
	}
	return splitMaximalHostPiecesAtIncludes(pieces, includes)
}

func maximalBoundaryRegionAt(parsed *ParsedDocument, offset int) *Region {
	var best *Region
	for index := range parsed.Regions {
		region := &parsed.Regions[index]
		if !maximalPartialRegion(region.Kind) || offset < region.ContentStart || offset >= region.ContentEnd {
			continue
		}
		if best == nil || region.ContentEnd-region.ContentStart < best.ContentEnd-best.ContentStart {
			best = region
		}
	}
	return best
}

func maximalPartialRegion(kind RegionKind) bool {
	return kind == RegionStyle ||
		kind == RegionStyleAttribute ||
		kind == RegionClientScript ||
		kind == RegionServerScript ||
		isASPRegion(kind)
}

func maximalKindForRegion(region *Region) maximalCommentPieceKind {
	switch region.Language {
	case LanguageCSS:
		return maximalCommentCSS
	case LanguageJavaScript:
		return maximalCommentJavaScript
	case LanguageVBScript, LanguageJScript, LanguageASPDirective:
		return maximalCommentServer
	default:
		return maximalCommentHost
	}
}

func splitMaximalHostPiecesAtIncludes(
	pieces []maximalCommentPiece,
	includes []maximalIncludeDirective,
) []maximalCommentPiece {
	var result []maximalCommentPiece
	for _, piece := range pieces {
		if piece.kind != maximalCommentHost {
			result = append(result, piece)
			continue
		}
		cursor := piece.start
		for _, include := range includes {
			if include.end <= piece.start || include.start >= piece.end {
				continue
			}
			if cursor < include.start {
				result = append(result, maximalCommentPiece{start: cursor, end: include.start, kind: maximalCommentHost})
			}
			result = append(result, maximalCommentPiece{
				start:              max(include.start, piece.start),
				end:                min(include.end, piece.end),
				kind:               maximalCommentInclude,
				includeMarkerStart: include.markerStart,
				includeDisabled:    include.disabled,
			})
			cursor = min(include.end, piece.end)
		}
		if cursor < piece.end {
			result = append(result, maximalCommentPiece{start: cursor, end: piece.end, kind: maximalCommentHost})
		}
	}
	return result
}

func scanMaximalIncludeDirectives(doc *TextDocument, text string) []maximalIncludeDirective {
	var directives []maximalIncludeDirective
	for cursor := 0; cursor < len(text); {
		start, token := nextIncludeScanToken(text, cursor)
		if start < 0 {
			break
		}
		if includeTokenInsideHTMLTag(text, start) {
			cursor = start + 1
			continue
		}
		switch token {
		case "asp":
			close := aspCloseDelimiter(text, start)
			if close < 0 {
				return directives
			}
			cursor = close + 2
		case "comment":
			close := strings.Index(text[start+4:], "-->")
			if close < 0 {
				return directives
			}
			end := start + 4 + close + len("-->")
			if _, ok := parseIncludeComment(doc, text, start, end); ok {
				hash := start + 4
				for hash < end-len("-->") && isCommentWhitespace(text[hash]) {
					hash++
				}
				directives = append(directives, maximalIncludeDirective{
					start:       start,
					end:         end,
					markerStart: hash,
				})
			} else if markerStart, ok := disabledIncludeComment(doc, text, start, end); ok {
				directives = append(directives, maximalIncludeDirective{
					start:       start,
					end:         end,
					markerStart: markerStart,
					disabled:    true,
				})
			}
			cursor = end
		case "script", "style":
			end := includeOpeningTagEnd(text, start)
			if end < 0 {
				return directives
			}
			if includeOpeningTagIsSelfClosing(text, start, end) {
				cursor = end
			} else {
				cursor = includeEmbeddedElementEnd(text, token, end)
			}
		default:
			cursor = start + 1
		}
	}
	return directives
}

func disabledIncludeComment(doc *TextDocument, text string, start, end int) (int, bool) {
	bodyStart, bodyEnd := start+len("<!--"), end-len("-->")
	markerStart := bodyStart
	for markerStart < bodyEnd && isCommentWhitespace(text[markerStart]) {
		markerStart++
	}
	cursor := markerStart
	for strings.HasPrefix(text[cursor:bodyEnd], disabledIncludeMarker) {
		cursor += len(disabledIncludeMarker)
	}
	if cursor == markerStart {
		return 0, false
	}
	masked := []byte(text)
	for index := markerStart; index < cursor; index++ {
		masked[index] = ' '
	}
	if _, ok := parseIncludeComment(doc, string(masked), start, end); !ok {
		return 0, false
	}
	return markerStart, true
}

func prepareMaximalPiece(
	doc *TextDocument,
	text string,
	parsed *ParsedDocument,
	piece maximalCommentPiece,
	settings Settings,
) maximalPreparedPiece {
	prepared := maximalPreparedPiece{piece: piece}
	if piece.start >= piece.end {
		return prepared
	}
	switch piece.kind {
	case maximalCommentInclude:
		prepared.actionable = true
		prepared.commented = piece.includeDisabled
	case maximalCommentCSS:
		_, _, ok := maximalBlockBounds(text, piece.start, piece.end)
		prepared.actionable = ok
		if ok {
			_, prepared.commented = unwrapMaximalBlock(text[piece.start:piece.end], "/*", "*/")
		}
	case maximalCommentJavaScript:
		prepared.lineTargets = maximalGenericLineTargets(doc, text, piece, "//")
		prepared.actionable = len(prepared.lineTargets) > 0
		prepared.commented = prepared.actionable && maximalLineTargetsCommented(prepared.lineTargets)
	case maximalCommentServer:
		prepared.lineTargets = maximalServerLineTargets(doc, text, parsed, piece, settings)
		prepared.actionable = len(prepared.lineTargets) > 0
		prepared.commented = prepared.actionable && maximalLineTargetsCommented(prepared.lineTargets)
	case maximalCommentHost:
		prepared.lineTargets = maximalServerLineTargets(doc, text, parsed, piece, settings)
		prepared.hasHost = maximalHostHasContent(text, parsed, piece)
		prepared.actionable = prepared.hasHost || len(prepared.lineTargets) > 0
		serverCommented := len(prepared.lineTargets) == 0 || maximalLineTargetsCommented(prepared.lineTargets)
		if prepared.hasHost {
			_, prepared.commented = unwrapMaximalBlock(text[piece.start:piece.end], "<!--", "-->")
			prepared.commented = prepared.commented && serverCommented
		} else {
			prepared.commented = len(prepared.lineTargets) > 0 && serverCommented
		}
	}
	return prepared
}

func transformMaximalInterval(
	text string,
	interval commentSpan,
	pieces []maximalPreparedPiece,
	uncomment bool,
) string {
	operations := make([]commentOperation, 0, len(pieces))
	for _, prepared := range pieces {
		if !prepared.actionable {
			continue
		}
		piece := prepared.piece
		replacement := transformMaximalPiece(text, prepared, uncomment)
		if replacement == text[piece.start:piece.end] {
			continue
		}
		operations = append(operations, commentOperation{
			start:       piece.start,
			end:         piece.end,
			replacement: replacement,
		})
	}
	return applyCommentOperations(text[interval.start:interval.end], interval.start, operations)
}

func transformMaximalPiece(
	text string,
	prepared maximalPreparedPiece,
	uncomment bool,
) string {
	piece := prepared.piece
	switch piece.kind {
	case maximalCommentInclude:
		return transformMaximalInclude(text, prepared, uncomment)
	case maximalCommentCSS:
		return transformMaximalBlock(text, piece, "/*", "*/", uncomment)
	case maximalCommentJavaScript:
		return transformMaximalLinePiece(text, piece, prepared.lineTargets, uncomment)
	case maximalCommentServer:
		return transformMaximalLinePiece(text, piece, prepared.lineTargets, uncomment)
	case maximalCommentHost:
		return transformMaximalHost(text, piece, prepared.lineTargets, prepared.hasHost, uncomment)
	default:
		return text[piece.start:piece.end]
	}
}

func transformMaximalInclude(text string, prepared maximalPreparedPiece, uncomment bool) string {
	piece := prepared.piece
	value := text[piece.start:piece.end]
	relative := piece.includeMarkerStart - piece.start
	if uncomment {
		if !piece.includeDisabled {
			return value
		}
		return value[:relative] + value[relative+len(disabledIncludeMarker):]
	}
	return value[:relative] + disabledIncludeMarker + value[relative:]
}

func transformMaximalHost(
	text string,
	piece maximalCommentPiece,
	targets []maximalLineTarget,
	hasHost bool,
	uncomment bool,
) string {
	value := text[piece.start:piece.end]
	transformed := transformMaximalLineValue(value, piece.start, targets, uncomment)
	if !hasHost {
		return transformed
	}
	if uncomment {
		unwrapped, ok := unwrapMaximalBlock(transformed, "<!--", "-->")
		if ok {
			return unwrapped
		}
		return transformed
	}
	start, end, ok := maximalHostBounds(transformed)
	if !ok {
		return transformed
	}
	_, _, originalOK := maximalHostBounds(value)
	if !originalOK {
		return transformed
	}
	wrapped := wrapMaximalBlock(transformed[start:end], "<!--", "-->")
	return transformed[:start] + wrapped + transformed[end:]
}

func transformMaximalBlock(text string, piece maximalCommentPiece, open, close string, uncomment bool) string {
	value := text[piece.start:piece.end]
	if uncomment {
		unwrapped, ok := unwrapMaximalBlock(value, open, close)
		if ok {
			return unwrapped
		}
		return value
	}
	start, end, ok := maximalBlockBounds(value, 0, len(value))
	if !ok {
		return value
	}
	wrapped := wrapMaximalBlock(value[start:end], open, close)
	return value[:start] + wrapped + value[end:]
}

func wrapMaximalBlock(content, open, close string) string {
	// Match VS Code's HTML/CSS comment commands by inserting configured delimiters verbatim.
	return open + " " + content + " " + close
}

func unwrapMaximalBlock(value, open, close string) (string, bool) {
	var start, end int
	var ok bool
	if open != "<!--" {
		start, end, ok = maximalBlockBounds(value, 0, len(value))
	} else {
		start, end, _ = maximalWrapperBounds(value)
		for start < end && isHorizontalCommentWhitespace(value[start]) {
			start++
		}
		for end > start && isHorizontalCommentWhitespace(value[end-1]) {
			end--
		}
		ok = start < end
	}
	if !ok {
		return "", false
	}
	trimmed := value[start:end]
	if contentStart, contentEnd, ok := maximalPlainCommentContent(trimmed, open, close); ok {
		content := trimmed[contentStart:contentEnd]
		return value[:start] + content + value[end:], true
	}
	return "", false
}
func maximalPlainCommentContent(value, open, close string) (int, int, bool) {
	if !strings.HasPrefix(value, open) || !strings.HasSuffix(value, close) {
		return 0, 0, false
	}
	start, end := len(open), len(value)-len(close)
	if start < end && value[start] == '\t' {
		for start < end && isHorizontalCommentWhitespace(value[start]) {
			start++
		}
	} else if start < end && value[start] == ' ' {
		start++
	}
	if start < end && value[end-1] == '\t' {
		for end > start && isHorizontalCommentWhitespace(value[end-1]) {
			end--
		}
	} else if start < end && value[end-1] == ' ' {
		end--
	}
	return start, end, start <= end
}

func maximalWrapperBounds(value string) (int, int, bool) {
	start, end := 0, len(value)
	for start < end && (value[start] == '\r' || value[start] == '\n') {
		start++
	}
	for end > start && (value[end-1] == '\r' || value[end-1] == '\n') {
		end--
	}
	return start, end, start < end
}

func maximalHostBounds(value string) (int, int, bool) {
	return maximalWrapperBounds(value)
}

func maximalBlockBounds(text string, start, end int) (int, int, bool) {
	for start < end && isCommentWhitespace(text[start]) {
		start++
	}
	for end > start && isCommentWhitespace(text[end-1]) {
		end--
	}
	return start, end, start < end
}
