package core

import (
	"strings"

	"github.com/yottonoko/classic-web-system-lsp/internal/lsp"
)

// IncrementalChange describes one LSP text change against a parsed document.
// Ranged changes are eligible for structural reuse; a nil Range requests a
// complete document replacement.
type IncrementalChange struct {
	Range        *lsp.Range
	Text         string
	ByteStart    int
	ByteEnd      int
	HasByteRange bool
}

// IncrementalUpdateResult is the result of updating a parsed document.  Parsed
// is always usable; Incremental reports whether structural data was shifted and
// reused instead of rescanning the complete source.
type IncrementalUpdateResult struct {
	Parsed        *ParsedDocument
	Incremental   bool
	ReusedRegions int
	Reason        string
	Impact        IncrementalImpact
}

// IncrementalRangeMapper remaps source offsets and ranges from the immediate
// predecessor retained by an incremental parsed document.
type IncrementalRangeMapper struct {
	previous *TextDocument
	current  *TextDocument
	impact   IncrementalImpact
}

// SkipPreviousRuntimeInheritance marks the mapper as revision-specific. Its
// offsets and source documents belong to one exact incremental revision.
func (*IncrementalRangeMapper) SkipPreviousRuntimeInheritance() {}

// incrementalRangeMapperAnalysisKey caches the range mapper for a revision.
const incrementalRangeMapperAnalysisKey = "core.incremental-range-mapper.runtime.v1"

// NewIncrementalRangeMapper creates a mapper for the current incremental
// revision. It returns false for a full parse without a retained predecessor.
func NewIncrementalRangeMapper(current *ParsedDocument) (*IncrementalRangeMapper, bool) {
	if current == nil {
		return nil, false
	}
	previousText, ok := current.PreviousRevisionText()
	if !ok {
		return nil, false
	}
	return &IncrementalRangeMapper{
		previous: NewTextDocument(current.URI, "classic-asp", 0, previousText),
		current:  SourceDocument(current),
		impact:   current.ChangeImpact,
	}, true
}

// IncrementalRangeMapperFor returns the shared range mapper for the current
// incremental revision, building it once per revision. Callers must not
// mutate the mapper or the documents it references.
func IncrementalRangeMapperFor(current *ParsedDocument) (*IncrementalRangeMapper, bool) {
	if current == nil {
		return nil, false
	}
	if value, ok := current.LoadRuntimeAnalysis(incrementalRangeMapperAnalysisKey); ok {
		if mapper, ok := value.(*IncrementalRangeMapper); ok && mapper != nil {
			return mapper, true
		}
	}
	mapper, ok := NewIncrementalRangeMapper(current)
	if !ok {
		return nil, false
	}
	actual, _ := current.LoadOrStoreRuntimeAnalysis(incrementalRangeMapperAnalysisKey, mapper)
	if shared, ok := actual.(*IncrementalRangeMapper); ok && shared != nil {
		return shared, true
	}
	return mapper, true
}

// Offset remaps one predecessor byte offset that does not overlap the edit.
func (m *IncrementalRangeMapper) Offset(offset int) (int, bool) {
	if m == nil {
		return 0, false
	}
	switch {
	case offset <= m.impact.OldStart:
		return offset, true
	case offset >= m.impact.OldEnd:
		return offset + m.impact.NewEnd - m.impact.OldEnd, true
	default:
		return 0, false
	}
}

// CurrentOffset converts a position in the current revision to a byte offset.
func (m *IncrementalRangeMapper) CurrentOffset(position lsp.Position) (int, bool) {
	if m == nil {
		return 0, false
	}
	return m.current.OffsetAt(position), true
}

// Range remaps one predecessor LSP range that does not overlap the edit.
func (m *IncrementalRangeMapper) Range(value lsp.Range) (lsp.Range, bool) {
	if m == nil {
		return lsp.Range{}, false
	}
	start := m.previous.OffsetAt(value.Start)
	end := m.previous.OffsetAt(value.End)
	if end <= m.impact.OldStart {
		return value, true
	}
	if start < m.impact.OldEnd {
		return lsp.Range{}, false
	}
	delta := m.impact.NewEnd - m.impact.OldEnd
	return m.current.Range(start+delta, end+delta), true
}

// IncrementalImpact describes the query families invalidated by one source
// revision. An empty or AllLanguages impact is conservative and invalidates
// every embedded language.
type IncrementalImpact struct {
	Languages    []EmbeddedLanguage
	AllLanguages bool
	OldStart     int
	OldEnd       int
	NewEnd       int
	Replacement  string
}

// Affects reports whether an embedded-language query must be rebuilt.
func (i IncrementalImpact) Affects(language EmbeddedLanguage) bool {
	if i.AllLanguages || len(i.Languages) == 0 {
		return true
	}
	for _, affected := range i.Languages {
		if affected == language {
			return true
		}
	}
	return false
}

// IncrementalChangeAfterLanguage reports whether the edit occurs after every
// region of an unaffected embedded language, so its cached source locations
// remain valid without remapping.
func IncrementalChangeAfterLanguage(parsed *ParsedDocument, language EmbeddedLanguage) bool {
	if parsed == nil || parsed.ChangeImpact.Affects(language) {
		return false
	}
	for _, region := range parsed.Regions {
		if region.Language == language && region.ContentEnd > parsed.ChangeImpact.OldStart {
			return false
		}
	}
	return true
}

// UpdateParsedDocument applies a single safe ranged edit while retaining the
// existing region/include/error metadata.  Boundary edits fall back to a
// normal ParseDocument so callers can use this function without a separate
// safety check.  Multiple changes are applied in order and parsed as a full
// document, matching the TypeScript runtime's conservative fallback path.
func UpdateParsedDocument(previous *ParsedDocument, changes []IncrementalChange, settings Settings) IncrementalUpdateResult {
	if previous == nil {
		return IncrementalUpdateResult{
			Parsed: ParseDocument("", "", settings),
			Reason: "missing previous document",
		}
	}
	if len(changes) != 1 {
		next := applyIncrementalChanges(previous.URI, previous.Text, changes)
		return IncrementalUpdateResult{
			Parsed: ParseDocument(previous.URI, next, settings),
			Reason: "multiple changes",
		}
	}
	change := changes[0]
	if change.Range == nil {
		return IncrementalUpdateResult{
			Parsed: ParseDocument(previous.URI, change.Text, settings),
			Reason: "full document replacement",
		}
	}
	start, end := change.ByteStart, change.ByteEnd
	var previousDocument *TextDocument
	if !change.HasByteRange {
		previousDocument = NewTextDocument(previous.URI, "classic-asp", 0, previous.Text)
		start = previousDocument.OffsetAt(change.Range.Start)
		end = previousDocument.OffsetAt(change.Range.End)
	}
	if start > end {
		start, end = end, start
	}
	if start < 0 || end < start || end > len(previous.Text) {
		return IncrementalUpdateResult{
			Parsed: ParseDocument(previous.URI, applyIncrementalChange(previous.Text, start, end, change.Text), settings),
			Reason: "invalid change range",
		}
	}
	nextText := previous.Text[:start] + change.Text + previous.Text[end:]
	if len(previous.Includes) > 0 && previousDocument == nil {
		previousDocument = NewTextDocument(previous.URI, "classic-asp", 0, previous.Text)
	}
	if reason := incrementalChangeUnsafe(previous, previousDocument, nextText, start, end, change.Text); reason != "" {
		return IncrementalUpdateResult{
			Parsed: ParseDocument(previous.URI, nextText, settings),
			Reason: reason,
		}
	}
	if previous.DefaultLanguage != normalizeServerLanguage(settings.DefaultLanguage) && strings.TrimSpace(settings.DefaultLanguage) != "" {
		return IncrementalUpdateResult{
			Parsed: ParseDocument(previous.URI, nextText, settings),
			Reason: "default language changed",
		}
	}
	delta := len(change.Text) - (end - start)
	updated := &ParsedDocument{
		URI:             previous.URI,
		Text:            nextText,
		DefaultLanguage: previous.DefaultLanguage,
		Regions:         shiftRegions(previous.Regions, start, end, delta),
		Includes:        shiftIncludes(previous, previousDocument, nextText, start, end, delta),
		Errors:          shiftParseErrors(previous.Errors, start, end, delta),
	}
	updated.inheritPreviousRevision(previous)
	owner := RegionAt(previous, start)
	impact := IncrementalImpact{OldStart: start, OldEnd: end, NewEnd: start + len(change.Text), Replacement: change.Text}
	if owner == nil {
		// Safe source outside an embedded owner is host HTML. ASP delimiter and
		// region-boundary edits have already taken the conservative full-parse path.
		impact.Languages = []EmbeddedLanguage{LanguageHTML}
	} else {
		impact.Languages = []EmbeddedLanguage{owner.Language}
	}
	updated.ChangeImpact = impact
	inheritUnaffectedVirtualDocuments(previous, updated, impact)
	return IncrementalUpdateResult{
		Parsed:        updated,
		Incremental:   true,
		ReusedRegions: len(updated.Regions),
		Reason:        "safe content edit",
		Impact:        impact,
	}
}

func applyIncrementalChanges(uri, text string, changes []IncrementalChange) string {
	result := text
	for _, change := range changes {
		if change.Range == nil {
			result = change.Text
			continue
		}
		start, end := change.ByteStart, change.ByteEnd
		if !change.HasByteRange {
			document := NewTextDocument(uri, "classic-asp", 0, result)
			start = document.OffsetAt(change.Range.Start)
			end = document.OffsetAt(change.Range.End)
		}
		if start > end {
			start, end = end, start
		}
		result = applyIncrementalChange(result, start, end, change.Text)
	}
	return result
}

func applyIncrementalChange(text string, start, end int, replacement string) string {
	start = max(0, min(start, len(text)))
	end = max(start, min(end, len(text)))
	return text[:start] + replacement + text[end:]
}

func incrementalChangeUnsafe(previous *ParsedDocument, previousDocument *TextDocument, nextText string, start, end int, replacement string) string {
	if end-start > 1024 || len(replacement) > 1024 {
		return "large edit"
	}
	changed := previous.Text[start:end] + replacement
	if boundarySensitiveIncrementalText(changed) {
		return "boundary text edit"
	}
	if aspDelimiterCrossesEditBoundary(previous.Text, start, end) ||
		aspDelimiterCrossesEditBoundary(nextText, start, start+len(replacement)) {
		return "ASP delimiter boundary edit"
	}
	if embeddedBoundaryTagEdit(previous.Text, start, end) || embeddedBoundaryTagEdit(nextText, start, start+len(replacement)) {
		return "embedded boundary tag edit"
	}
	for _, include := range previous.Includes {
		includeStart := previousDocument.OffsetAt(include.Range.Start)
		includeEnd := previousDocument.OffsetAt(include.Range.End)
		if rangesOverlapOrTouch(start, end, includeStart, includeEnd) {
			return "include directive edit"
		}
	}
	if start == end && start == len(previous.Text) && len(previous.Regions) > 0 {
		for _, finalRegion := range previous.Regions {
			if finalRegion.End == len(previous.Text) && finalRegion.Kind != RegionHTML {
				return "final embedded region boundary edit"
			}
		}
	}
	owner := RegionAt(previous, start)
	if owner == nil {
		if structuralMarkerIntroduced(previous.Text, nextText, start) || hasMarkerNear(nextText, "style=", start) {
			return "host structure edit"
		}
		return ""
	}
	if owner.Kind == RegionASPDirective {
		return "ASP directive edit"
	}
	if end > start {
		endOwner := RegionAt(previous, end-1)
		if endOwner == nil || endOwner.Kind != owner.Kind || endOwner.Start != owner.Start || endOwner.End != owner.End {
			return "edit crosses language boundary"
		}
	}
	if start == end && start == owner.ContentStart {
		return "region content boundary edit"
	}
	touchesFinalHTMLBoundary := owner.Kind == RegionHTML && owner.End == len(previous.Text) && start > owner.Start && end <= owner.End
	if (start <= owner.Start || end >= owner.End) && !touchesFinalHTMLBoundary {
		return "region boundary edit"
	}
	if owner.Kind == RegionHTML && structuralMarkerIntroduced(previous.Text, nextText, start) {
		return "structural marker edit"
	}
	if owner.Kind == RegionHTML && hasMarkerNear(nextText, "style=", start) {
		return "style attribute rescan"
	}
	if owner.Kind == RegionStyleAttribute && strings.ContainsAny(changed, `"'`) {
		return "style attribute boundary edit"
	}
	return ""
}

func structuralMarkerIntroduced(previousText, nextText string, offset int) bool {
	for _, marker := range []string{
		"<%", "%>", "<!--", "#include", "<script", "</script", "<style", "</style",
		"style=", "runat=", "language=", "progid=", "classid=",
	} {
		if hasMarkerNear(nextText, marker, offset) && !hasMarkerNear(previousText, marker, offset) {
			return true
		}
	}
	return false
}

func aspDelimiterCrossesEditBoundary(text string, start, end int) bool {
	return aspDelimiterAtBoundary(text, start) || aspDelimiterAtBoundary(text, end)
}

func aspDelimiterAtBoundary(text string, offset int) bool {
	if offset <= 0 || offset >= len(text) {
		return false
	}
	pair := text[offset-1 : offset+1]
	return pair == "<%" || pair == "%>"
}

func embeddedBoundaryTagEdit(text string, start, end int) bool {
	if text == "" {
		return false
	}
	start = max(0, min(start, len(text)))
	end = max(start, min(end, len(text)))
	searchThrough := min(len(text), start+1)
	open := strings.LastIndexByte(text[:searchThrough], '<')
	if open < 0 {
		return false
	}
	if close := strings.LastIndexByte(text[:searchThrough], '>'); close > open {
		return false
	}
	// The edit belongs to the tag only while no closing angle bracket occurs
	// before it. Content edits near a script or style opening tag stay eligible
	// for incremental reuse.
	if strings.Contains(text[open:start], ">") {
		return false
	}
	searchEnd := min(len(text), max(end, open+len("</script")))
	tag := strings.ToLower(strings.TrimSpace(text[open:searchEnd]))
	for _, prefix := range []string{"<script", "</script", "<style", "</style"} {
		if strings.HasPrefix(tag, prefix) {
			return true
		}
	}
	return false
}

func hasMarkerNear(text, marker string, offset int) bool {
	if marker == "" || text == "" {
		return false
	}
	const window = 128
	start := max(0, offset-window)
	end := min(len(text), offset+window+len(marker))
	return strings.Contains(strings.ToLower(text[start:end]), strings.ToLower(marker))
}

func boundarySensitiveIncrementalText(text string) bool {
	for _, marker := range []string{
		"<%", "%>", "<!--", "#include", "<script", "</script", "<style", "</style",
		"runat=", "language=", "style=", "progid=", "classid=",
	} {
		if strings.Contains(strings.ToLower(text), marker) {
			return true
		}
	}
	return false
}

func rangesOverlapOrTouch(start, end, otherStart, otherEnd int) bool {
	if start == end {
		return start >= otherStart && start <= otherEnd
	}
	return start < otherEnd && end > otherStart
}

func shiftOffset(offset, start, end, delta int) int {
	if offset < start {
		return offset
	}
	if offset >= end {
		return offset + delta
	}
	return start
}

func shiftRegions(regions []Region, start, end, delta int) []Region {
	shifted := make([]Region, len(regions))
	for index, region := range regions {
		shifted[index] = region
		shifted[index].Start = shiftOffset(region.Start, start, end, delta)
		shifted[index].End = shiftOffset(region.End, start, end, delta)
		shifted[index].ContentStart = shiftOffset(region.ContentStart, start, end, delta)
		shifted[index].ContentEnd = shiftOffset(region.ContentEnd, start, end, delta)
	}
	return shifted
}

func shiftIncludes(previous *ParsedDocument, previousDocument *TextDocument, nextText string, start, end, delta int) []Include {
	if len(previous.Includes) == 0 {
		return previous.Includes
	}
	shifted := make([]Include, len(previous.Includes))
	document := NewTextDocument(previous.URI, "classic-asp", 0, nextText)
	for index, include := range previous.Includes {
		shifted[index] = include
		includeStart := previousDocument.OffsetAt(include.Range.Start)
		includeEnd := previousDocument.OffsetAt(include.Range.End)
		shifted[index].Range = document.Range(
			shiftOffset(includeStart, start, end, delta),
			shiftOffset(includeEnd, start, end, delta),
		)
	}
	return shifted
}

func shiftParseErrors(errors []ParseError, start, end, delta int) []ParseError {
	shifted := make([]ParseError, len(errors))
	for index, parseError := range errors {
		shifted[index] = parseError
		shifted[index].Start = shiftOffset(parseError.Start, start, end, delta)
		shifted[index].End = shiftOffset(parseError.End, start, end, delta)
	}
	return shifted
}
