package core

import (
	"context"
	"errors"
	"sort"
	"strings"

	"github.com/yottonoko/classic-web-system-lsp/internal/lsp"
)

type VirtualDocument struct {
	URI        string
	LanguageID string
	Text       string
	Segments   []SourceMapSegment
	runtimeDoc *TextDocument
}

// EstimateExclusiveRuntimeBytes reports the runtime-only line index retained
// by the virtual document. The virtual text, URI, language, and source-map
// backing are accounted for by their owning parsed revision or snapshot;
// runtimeDoc.Text shares the virtual text and is therefore intentionally not
// charged here.
func (v VirtualDocument) EstimateExclusiveRuntimeBytes() int64 {
	if v.runtimeDoc == nil {
		return 0
	}
	return int64(cap(v.runtimeDoc.lineStarts))*8 + int64(cap(v.runtimeDoc.lineASCII))
}

type SourceMapSegment struct {
	VirtualStart int
	VirtualEnd   int
	SourceStart  int
	SourceEnd    int
}

// VirtualDocumentReplacement replaces one exact source range while building
// an embedded-region virtual document. The replacement text is deliberately
// unmapped in the resulting virtual document.
type VirtualDocumentReplacement struct {
	SourceStart int
	SourceEnd   int
	Text        string
}

func BuildVirtualDocument(parsed *ParsedDocument, language EmbeddedLanguage) VirtualDocument {
	key := "core.virtual-document.v1." + string(language)
	runtimeKey := "core.virtual-document.runtime.v1." + string(language)
	if cached, ok := parsed.LoadRuntimeAnalysis(runtimeKey); ok {
		return cached.(VirtualDocument)
	}
	var cached VirtualDocument
	if parsed.LoadAnalysis(key, &cached) {
		cached.attachRuntimeDocument()
		parsed.StoreRuntimeAnalysis(runtimeKey, cached)
		return cached
	}
	virtual := buildVirtualDocument(parsed, language)
	virtual.attachRuntimeDocument()
	parsed.StoreAnalysis(key, virtual)
	parsed.StoreRuntimeAnalysis(runtimeKey, virtual)
	return virtual
}

func inheritUnaffectedVirtualDocuments(previous, updated *ParsedDocument, impact IncrementalImpact) {
	if previous == nil || updated == nil {
		return
	}
	for _, language := range []EmbeddedLanguage{LanguageHTML, LanguageCSS, LanguageJavaScript, LanguageVBScript, LanguageJScript} {
		if language != LanguageHTML && impact.Affects(language) {
			continue
		}
		key := "core.virtual-document.v1." + string(language)
		runtimeKey := "core.virtual-document.runtime.v1." + string(language)
		var virtual VirtualDocument
		if cached, ok := previous.LoadRuntimeAnalysis(runtimeKey); ok {
			virtual = cached.(VirtualDocument)
		} else if !previous.LoadAnalysis(key, &virtual) {
			continue
		}
		var shifted VirtualDocument
		var ok bool
		if language == LanguageHTML {
			shifted, ok = updateMaskedHTMLVirtualDocument(virtual, impact, !impact.Affects(LanguageHTML))
		} else {
			shifted, ok = shiftVirtualDocumentSourceMap(virtual, impact)
		}
		if ok {
			updated.StoreRuntimeAnalysis(runtimeKey, shifted)
		}
	}
}

func updateMaskedHTMLVirtualDocument(virtual VirtualDocument, impact IncrementalImpact, maskReplacement bool) (VirtualDocument, bool) {
	if impact.OldStart < 0 || impact.OldEnd < impact.OldStart || impact.OldEnd > len(virtual.Text) {
		return VirtualDocument{}, false
	}
	delta := impact.NewEnd - impact.OldEnd
	segments := append([]SourceMapSegment(nil), virtual.Segments...)
	overlapped := false
	for index := range segments {
		segment := &segments[index]
		containsEdit := segment.SourceStart <= impact.OldStart && segment.SourceEnd >= impact.OldEnd
		if impact.OldStart == impact.OldEnd {
			containsEdit = segment.SourceStart <= impact.OldStart && segment.SourceEnd > impact.OldStart
		}
		switch {
		case !maskReplacement && !overlapped && containsEdit:
			segment.SourceEnd += delta
			segment.VirtualEnd += delta
			overlapped = true
		case segment.SourceEnd <= impact.OldStart:
		case segment.SourceStart >= impact.OldEnd:
			segment.SourceStart += delta
			segment.SourceEnd += delta
			segment.VirtualStart += delta
			segment.VirtualEnd += delta
		default:
			return VirtualDocument{}, false
		}
	}
	replacement := impact.Replacement
	if maskReplacement {
		replacement = preserveLineEndingsRange(replacement, 0, len(replacement), ' ')
	} else if !overlapped {
		return VirtualDocument{}, false
	}
	virtual.Text = virtual.Text[:impact.OldStart] + replacement + virtual.Text[impact.OldEnd:]
	virtual.Segments = segments
	virtual.runtimeDoc = nil
	virtual.attachRuntimeDocument()
	return virtual, true
}

func shiftVirtualDocumentSourceMap(virtual VirtualDocument, impact IncrementalImpact) (VirtualDocument, bool) {
	delta := impact.NewEnd - impact.OldEnd
	segments := append([]SourceMapSegment(nil), virtual.Segments...)
	for index := range segments {
		segment := &segments[index]
		if impact.OldStart == impact.OldEnd && segment.SourceStart == impact.OldStart && segment.SourceEnd == impact.OldStart {
			return VirtualDocument{}, false
		}
		switch {
		case segment.SourceEnd <= impact.OldStart:
		case segment.SourceStart >= impact.OldEnd:
			segment.SourceStart += delta
			segment.SourceEnd += delta
		default:
			return VirtualDocument{}, false
		}
	}
	virtual.Segments = segments
	return virtual, true
}

func buildVirtualDocument(parsed *ParsedDocument, language EmbeddedLanguage) VirtualDocument {
	if language == LanguageHTML {
		return buildMaskedHTMLVirtualDocument(parsed, language)
	}
	var text strings.Builder
	text.Grow(len(parsed.Text))
	var segments []SourceMapSegment
	sorted := append([]Region(nil), parsed.Regions...)
	sort.Slice(sorted, func(i, j int) bool {
		if sorted[i].Start == sorted[j].Start {
			return sorted[i].End < sorted[j].End
		}
		return sorted[i].Start < sorted[j].Start
	})
	for _, region := range sorted {
		if region.Language != language {
			continue
		}
		prefix := ""
		suffix := "\n"
		if language == LanguageCSS {
			if region.Kind == RegionStyleAttribute {
				prefix = "*{"
				suffix = "}\n"
			} else {
				prefix = "\n"
			}
		}
		start := text.Len() + len(prefix)
		nested := nestedRegionsForOwner(region, sorted)
		content := maskNestedRegions(parsed.Text, region, nested, language)
		text.WriteString(prefix)
		text.WriteString(content)
		text.WriteString(suffix)
		segments = append(segments, sourceMapSegmentsForRegion(region, nested, start)...)
	}
	return VirtualDocument{
		URI:        parsed.URI + "." + string(language) + ".virtual",
		LanguageID: string(language),
		Text:       text.String(),
		Segments:   segments,
	}
}

// BuildEmbeddedRegionVirtualDocument builds one embedded owner region with the
// same masking and wrapper rules as BuildVirtualDocument.
func BuildEmbeddedRegionVirtualDocument(parsed *ParsedDocument, region Region) VirtualDocument {
	return buildEmbeddedRegionVirtualDocument(parsed, region, nil)
}

// BuildEmbeddedRegionVirtualDocumentWithReplacements builds one embedded owner
// region and applies replacements whose source ranges exactly identify nested
// ASP holes. Replaced text is not included in the source map.
func BuildEmbeddedRegionVirtualDocumentWithReplacements(parsed *ParsedDocument, region Region, replacements []VirtualDocumentReplacement) VirtualDocument {
	virtual, err := BuildEmbeddedRegionVirtualDocumentWithReplacementsContext(context.Background(), parsed, region, replacements)
	if err != nil {
		return buildEmbeddedRegionVirtualDocument(parsed, region, nil)
	}
	return virtual
}

// BuildEmbeddedRegionVirtualDocumentWithReplacementsContext is the
// cancellable form of BuildEmbeddedRegionVirtualDocumentWithReplacements.
func BuildEmbeddedRegionVirtualDocumentWithReplacementsContext(ctx context.Context, parsed *ParsedDocument, region Region, replacements []VirtualDocumentReplacement) (VirtualDocument, error) {
	if ctx == nil {
		ctx = context.Background()
	}
	if err := ctx.Err(); err != nil {
		return VirtualDocument{}, err
	}
	if parsed == nil || region.ContentStart < 0 || region.ContentEnd < region.ContentStart || region.ContentEnd > len(parsed.Text) {
		return VirtualDocument{}, nil
	}
	prefix := ""
	suffix := "\n"
	if region.Language == LanguageCSS {
		if region.Kind == RegionStyleAttribute {
			prefix = "*{"
			suffix = "}\n"
		} else {
			prefix = "\n"
		}
	}
	nested, err := nestedRegionsForOwnerContext(ctx, region, parsed.Regions)
	if err != nil {
		return VirtualDocument{}, err
	}
	content, err := maskNestedRegionsContext(ctx, parsed.Text, region, nested, region.Language)
	if err != nil {
		return VirtualDocument{}, err
	}
	segments, err := sourceMapSegmentsForRegionContext(ctx, region, nested, len(prefix))
	if err != nil {
		return VirtualDocument{}, err
	}
	base := VirtualDocument{
		URI:        parsed.URI + "." + string(region.Language) + ".region.virtual",
		LanguageID: string(region.Language),
		Text:       prefix + content + suffix,
		Segments:   segments,
	}
	valid, err := validEmbeddedRegionReplacementsContext(ctx, parsed, region, nested, replacements)
	if err != nil {
		return VirtualDocument{}, err
	}
	if len(valid) == 0 {
		if err := ctx.Err(); err != nil {
			return VirtualDocument{}, err
		}
		runtimeDoc, err := NewTextDocumentContext(ctx, base.URI, base.LanguageID, 0, base.Text)
		if err != nil {
			return VirtualDocument{}, err
		}
		base.runtimeDoc = runtimeDoc
		return base, nil
	}
	virtual := base
	if err := applyEmbeddedRegionReplacementsContext(ctx, &virtual, region, len(prefix), valid); err != nil {
		return VirtualDocument{}, err
	}
	virtual.runtimeDoc = nil
	if err := ctx.Err(); err != nil {
		return VirtualDocument{}, err
	}
	runtimeDoc, err := NewTextDocumentContext(ctx, virtual.URI, virtual.LanguageID, 0, virtual.Text)
	if err != nil {
		return VirtualDocument{}, err
	}
	virtual.runtimeDoc = runtimeDoc
	return virtual, nil
}

func buildEmbeddedRegionVirtualDocument(parsed *ParsedDocument, region Region, replacements []VirtualDocumentReplacement) VirtualDocument {
	if parsed == nil || region.ContentStart < 0 || region.ContentEnd < region.ContentStart || region.ContentEnd > len(parsed.Text) {
		return VirtualDocument{}
	}
	prefix := ""
	suffix := "\n"
	if region.Language == LanguageCSS {
		if region.Kind == RegionStyleAttribute {
			prefix = "*{"
			suffix = "}\n"
		} else {
			prefix = "\n"
		}
	}
	sorted := append([]Region(nil), parsed.Regions...)
	sort.Slice(sorted, func(i, j int) bool {
		if sorted[i].Start == sorted[j].Start {
			return sorted[i].End < sorted[j].End
		}
		return sorted[i].Start < sorted[j].Start
	})
	nested := nestedRegionsForOwner(region, sorted)
	content := maskNestedRegions(parsed.Text, region, nested, region.Language)
	base := VirtualDocument{
		URI:        parsed.URI + "." + string(region.Language) + ".region.virtual",
		LanguageID: string(region.Language),
		Text:       prefix + content + suffix,
		Segments:   sourceMapSegmentsForRegion(region, nested, len(prefix)),
	}
	base.attachRuntimeDocument()

	valid := validEmbeddedRegionReplacements(parsed, region, nested, replacements)
	if len(valid) == 0 {
		return base
	}
	virtual := base
	if !applyEmbeddedRegionReplacements(&virtual, region, len(prefix), valid) {
		return base
	}
	virtual.runtimeDoc = nil
	virtual.attachRuntimeDocument()
	return virtual
}

func validEmbeddedRegionReplacements(parsed *ParsedDocument, owner Region, nested []Region, replacements []VirtualDocumentReplacement) []VirtualDocumentReplacement {
	if parsed == nil || len(replacements) == 0 {
		return nil
	}
	allowed := make(map[[2]int]struct{})
	for _, child := range nested {
		if isASPHole(child) && child.Start >= owner.ContentStart && child.End <= owner.ContentEnd {
			allowed[[2]int{child.Start, child.End}] = struct{}{}
		}
	}
	if len(allowed) == 0 {
		return nil
	}
	valid := make([]VirtualDocumentReplacement, 0, len(replacements))
	for _, replacement := range replacements {
		if replacement.SourceStart < owner.ContentStart || replacement.SourceEnd <= replacement.SourceStart || replacement.SourceEnd > owner.ContentEnd {
			continue
		}
		if _, ok := allowed[[2]int{replacement.SourceStart, replacement.SourceEnd}]; !ok {
			continue
		}
		valid = append(valid, replacement)
	}
	if len(valid) < 2 {
		return valid
	}
	sort.Slice(valid, func(i, j int) bool {
		if valid[i].SourceStart == valid[j].SourceStart {
			return valid[i].SourceEnd < valid[j].SourceEnd
		}
		return valid[i].SourceStart < valid[j].SourceStart
	})
	for index := 1; index < len(valid); index++ {
		if valid[index].SourceStart < valid[index-1].SourceEnd {
			return nil
		}
	}
	return valid
}

func validEmbeddedRegionReplacementsContext(ctx context.Context, parsed *ParsedDocument, owner Region, nested []Region, replacements []VirtualDocumentReplacement) ([]VirtualDocumentReplacement, error) {
	if parsed == nil || len(replacements) == 0 {
		return nil, nil
	}
	allowed := make(map[[2]int]struct{}, len(nested))
	for _, child := range nested {
		if err := ctx.Err(); err != nil {
			return nil, err
		}
		if isASPHole(child) && child.Start >= owner.ContentStart && child.End <= owner.ContentEnd {
			allowed[[2]int{child.Start, child.End}] = struct{}{}
		}
	}
	valid := make([]VirtualDocumentReplacement, 0, len(replacements))
	previousEnd := -1
	for _, replacement := range replacements {
		if err := ctx.Err(); err != nil {
			return nil, err
		}
		if replacement.SourceStart < owner.ContentStart || replacement.SourceEnd <= replacement.SourceStart || replacement.SourceEnd > owner.ContentEnd {
			continue
		}
		if _, ok := allowed[[2]int{replacement.SourceStart, replacement.SourceEnd}]; !ok {
			continue
		}
		if previousEnd > replacement.SourceStart {
			return nil, errors.New("embedded replacements are not in source order")
		}
		valid = append(valid, replacement)
		previousEnd = replacement.SourceEnd
	}
	return valid, nil
}

func applyEmbeddedRegionReplacements(virtual *VirtualDocument, owner Region, prefixLength int, replacements []VirtualDocumentReplacement) bool {
	return applyEmbeddedRegionReplacementsContext(context.Background(), virtual, owner, prefixLength, replacements) == nil
}

func applyEmbeddedRegionReplacementsContext(ctx context.Context, virtual *VirtualDocument, owner Region, prefixLength int, replacements []VirtualDocumentReplacement) error {
	if virtual == nil {
		return errors.New("virtual document is nil")
	}
	type virtualReplacement struct {
		start int
		end   int
		text  string
	}
	virtualReplacements := make([]virtualReplacement, 0, len(replacements))
	totalDelta := 0
	previousEnd := -1
	for _, replacement := range replacements {
		if err := ctx.Err(); err != nil {
			return err
		}
		sourceLength := replacement.SourceEnd - replacement.SourceStart
		start := prefixLength + replacement.SourceStart - owner.ContentStart
		end := start + sourceLength
		if start < 0 || end < start || end > len(virtual.Text) || previousEnd > start {
			return errors.New("invalid embedded replacement range")
		}
		virtualReplacements = append(virtualReplacements, virtualReplacement{start: start, end: end, text: replacement.Text})
		totalDelta += len(replacement.Text) - sourceLength
		previousEnd = end
	}
	segments := append([]SourceMapSegment(nil), virtual.Segments...)
	replacementIndex := 0
	delta := 0
	for index := range segments {
		if err := ctx.Err(); err != nil {
			return err
		}
		segment := &segments[index]
		for replacementIndex < len(virtualReplacements) && virtualReplacements[replacementIndex].end <= segment.VirtualStart {
			replacement := virtualReplacements[replacementIndex]
			delta += len(replacement.text) - (replacement.end - replacement.start)
			replacementIndex++
		}
		if replacementIndex < len(virtualReplacements) {
			replacement := virtualReplacements[replacementIndex]
			if segment.VirtualStart < replacement.end && replacement.start < segment.VirtualEnd {
				return errors.New("embedded replacement overlaps a source-map segment")
			}
		}
		segment.VirtualStart += delta
		segment.VirtualEnd += delta
	}
	var text strings.Builder
	text.Grow(max(0, len(virtual.Text)+totalDelta))
	cursor := 0
	for _, replacement := range virtualReplacements {
		if err := ctx.Err(); err != nil {
			return err
		}
		text.WriteString(virtual.Text[cursor:replacement.start])
		text.WriteString(replacement.text)
		cursor = replacement.end
	}
	text.WriteString(virtual.Text[cursor:])
	virtual.Text = text.String()
	virtual.Segments = segments
	return ctx.Err()
}

func buildMaskedHTMLVirtualDocument(parsed *ParsedDocument, language EmbeddedLanguage) VirtualDocument {
	var text strings.Builder
	text.Grow(len(parsed.Text))
	var segments []SourceMapSegment
	masks := htmlVirtualMaskRanges(parsed)
	cursor := 0
	virtualCursor := 0
	for _, mask := range masks {
		if cursor < mask.Start {
			chunk := parsed.Text[cursor:mask.Start]
			text.WriteString(chunk)
			segments = append(segments, SourceMapSegment{
				VirtualStart: virtualCursor,
				VirtualEnd:   virtualCursor + len(chunk),
				SourceStart:  cursor,
				SourceEnd:    mask.Start,
			})
			virtualCursor += len(chunk)
		}
		if cursor < mask.End {
			masked := preserveLineEndingsRange(parsed.Text, max(cursor, mask.Start), mask.End, ' ')
			text.WriteString(masked)
			virtualCursor += len(masked)
			cursor = mask.End
		}
	}
	if cursor < len(parsed.Text) {
		chunk := parsed.Text[cursor:]
		text.WriteString(chunk)
		segments = append(segments, SourceMapSegment{
			VirtualStart: virtualCursor,
			VirtualEnd:   virtualCursor + len(chunk),
			SourceStart:  cursor,
			SourceEnd:    len(parsed.Text),
		})
	}
	return VirtualDocument{
		URI:        parsed.URI + "." + string(language) + ".virtual",
		LanguageID: string(language),
		Text:       text.String(),
		Segments:   segments,
	}
}

type htmlVirtualMask struct {
	Start int
	End   int
}

func htmlVirtualMaskRanges(parsed *ParsedDocument) []htmlVirtualMask {
	if parsed == nil || len(parsed.Regions) == 0 {
		return nil
	}
	masks := make([]htmlVirtualMask, 0)
	for _, region := range parsed.Regions {
		start, end := -1, -1
		switch {
		case isASPHole(region):
			start, end = region.Start, region.End
		case region.Kind == RegionStyle || region.Kind == RegionClientScript || region.Kind == RegionServerScript:
			// Keep the element tags available to the HTML service while removing
			// embedded source from the HTML virtual document.
			start, end = region.ContentStart, region.ContentEnd
		}
		if start < 0 || end <= start {
			continue
		}
		start = max(0, min(start, len(parsed.Text)))
		end = max(start, min(end, len(parsed.Text)))
		masks = append(masks, htmlVirtualMask{Start: start, End: end})
	}
	if len(masks) < 2 {
		return masks
	}
	sort.Slice(masks, func(i, j int) bool {
		if masks[i].Start == masks[j].Start {
			return masks[i].End < masks[j].End
		}
		return masks[i].Start < masks[j].Start
	})
	merged := masks[:1]
	for _, mask := range masks[1:] {
		last := &merged[len(merged)-1]
		if mask.Start <= last.End {
			if mask.End > last.End {
				last.End = mask.End
			}
			continue
		}
		merged = append(merged, mask)
	}
	return merged
}

func sourceMapSegmentsForRegion(owner Region, nested []Region, virtualStart int) []SourceMapSegment {
	var segments []SourceMapSegment
	cursor := owner.ContentStart
	pushBoundary := func(sourceOffset int) {
		segment := sourceMapSegment(owner, virtualStart, sourceOffset, sourceOffset)
		if len(segments) > 0 {
			prev := segments[len(segments)-1]
			if prev == segment {
				return
			}
		}
		segments = append(segments, segment)
	}
	pushBoundary(cursor)
	for _, hole := range nested {
		if !isASPHole(hole) {
			continue
		}
		if cursor < hole.Start {
			segments = append(segments, sourceMapSegment(owner, virtualStart, cursor, hole.Start))
		}
		if hole.End > cursor {
			cursor = hole.End
		}
		pushBoundary(cursor)
	}
	if cursor < owner.ContentEnd {
		segments = append(segments, sourceMapSegment(owner, virtualStart, cursor, owner.ContentEnd))
	} else {
		pushBoundary(owner.ContentEnd)
	}
	return segments
}

func sourceMapSegmentsForRegionContext(ctx context.Context, owner Region, nested []Region, virtualStart int) ([]SourceMapSegment, error) {
	var segments []SourceMapSegment
	cursor := owner.ContentStart
	pushBoundary := func(sourceOffset int) {
		segment := sourceMapSegment(owner, virtualStart, sourceOffset, sourceOffset)
		if len(segments) == 0 || segments[len(segments)-1] != segment {
			segments = append(segments, segment)
		}
	}
	pushBoundary(cursor)
	for _, hole := range nested {
		if err := ctx.Err(); err != nil {
			return nil, err
		}
		if !isASPHole(hole) {
			continue
		}
		if cursor < hole.Start {
			segments = append(segments, sourceMapSegment(owner, virtualStart, cursor, hole.Start))
		}
		if hole.End > cursor {
			cursor = hole.End
		}
		pushBoundary(cursor)
	}
	if cursor < owner.ContentEnd {
		segments = append(segments, sourceMapSegment(owner, virtualStart, cursor, owner.ContentEnd))
	} else {
		pushBoundary(owner.ContentEnd)
	}
	return segments, nil
}

func sourceMapSegment(owner Region, virtualStart int, sourceStart int, sourceEnd int) SourceMapSegment {
	offset := sourceStart - owner.ContentStart
	return SourceMapSegment{
		VirtualStart: virtualStart + offset,
		VirtualEnd:   virtualStart + offset + (sourceEnd - sourceStart),
		SourceStart:  sourceStart,
		SourceEnd:    sourceEnd,
	}
}

func nestedRegionsForOwner(owner Region, sorted []Region) []Region {
	if isASPHole(owner) {
		return nil
	}
	var result []Region
	for _, nested := range sorted {
		if nested.Start >= owner.ContentEnd {
			break
		}
		if nested.Start >= owner.ContentStart && nested.End <= owner.ContentEnd && nested != owner {
			if nested.Kind == RegionHTML {
				continue
			}
			result = append(result, nested)
		}
	}
	return result
}

func nestedRegionsForOwnerContext(ctx context.Context, owner Region, sorted []Region) ([]Region, error) {
	if isASPHole(owner) {
		return nil, nil
	}
	var result []Region
	for _, nested := range sorted {
		if err := ctx.Err(); err != nil {
			return nil, err
		}
		if nested.Start >= owner.ContentStart && nested.End <= owner.ContentEnd && nested != owner && nested.Kind != RegionHTML {
			if len(result) > 0 {
				previous := result[len(result)-1]
				if nested.Start < previous.Start || nested.Start == previous.Start && nested.End < previous.End {
					return nil, errors.New("nested regions are not in source order")
				}
			}
			result = append(result, nested)
		}
	}
	return result, nil
}

func maskNestedRegions(source string, owner Region, nested []Region, language EmbeddedLanguage) string {
	var text strings.Builder
	text.Grow(owner.ContentEnd - owner.ContentStart)
	cursor := owner.ContentStart
	for _, child := range nested {
		if child.Language == language || child.End <= cursor {
			continue
		}
		if cursor < child.Start {
			text.WriteString(source[cursor:child.Start])
		}
		text.WriteString(nestedRegionMask(source, owner, child, language))
		cursor = child.End
	}
	if cursor < owner.ContentEnd {
		text.WriteString(source[cursor:owner.ContentEnd])
	}
	return text.String()
}

func maskNestedRegionsContext(ctx context.Context, source string, owner Region, nested []Region, language EmbeddedLanguage) (string, error) {
	var text strings.Builder
	text.Grow(owner.ContentEnd - owner.ContentStart)
	cursor := owner.ContentStart
	for _, child := range nested {
		if err := ctx.Err(); err != nil {
			return "", err
		}
		if child.Language == language || child.End <= cursor {
			continue
		}
		if cursor < child.Start {
			text.WriteString(source[cursor:child.Start])
		}
		text.WriteString(nestedRegionMask(source, owner, child, language))
		cursor = child.End
	}
	if err := ctx.Err(); err != nil {
		return "", err
	}
	if cursor < owner.ContentEnd {
		text.WriteString(source[cursor:owner.ContentEnd])
	}
	return text.String(), ctx.Err()
}

func nestedRegionMask(source string, owner Region, nested Region, language EmbeddedLanguage) string {
	if !isASPHole(nested) {
		return preserveLineEndingsRange(source, nested.Start, nested.End, ' ')
	}
	switch language {
	case LanguageCSS:
		if (nested.Kind == RegionASPBlock || nested.Kind == RegionASPDirective) && !cssBlockNeedsValuePlaceholder(source, owner, nested) {
			return preserveLineEndingsRange(source, nested.Start, nested.End, ' ')
		}
		return preserveLineEndingsRange(source, nested.Start, nested.End, 'x')
	case LanguageJavaScript, LanguageJScript:
		if nested.Kind == RegionASPBlock && !javascriptBlockNeedsValuePlaceholder(source, owner, nested) {
			return preserveLineEndingsRange(source, nested.Start, nested.End, ' ')
		}
		return firstValuePlaceholderRange(source, nested.Start, nested.End, '0')
	default:
		return preserveLineEndingsRange(source, nested.Start, nested.End, ' ')
	}
}

func isASPHole(region Region) bool {
	return region.Kind == RegionASPBlock || region.Kind == RegionASPExpression || region.Kind == RegionASPDirective
}

func cssBlockNeedsValuePlaceholder(source string, owner Region, nested Region) bool {
	prev, ok := previousSignificantChar(source, owner.ContentStart, nested.Start)
	return ok && prev != '{' && prev != ';' && prev != '}'
}

func javascriptBlockNeedsValuePlaceholder(source string, owner Region, nested Region) bool {
	prev, ok := previousSignificantChar(source, owner.ContentStart, nested.Start)
	if !ok {
		return false
	}
	return prev == '=' || prev == '(' || prev == '[' || prev == ',' || prev == ':' || prev == '?' || prev == '!' || prev == '~' || prev == '+' || prev == '-' || prev == '*' || prev == '/' || prev == '%' || prev == '&' || prev == '|' || prev == '^' || prev == '<' || prev == '>'
}

func previousSignificantChar(source string, start int, end int) (byte, bool) {
	for i := end - 1; i >= start; i-- {
		switch source[i] {
		case ' ', '\t', '\n', '\r':
			continue
		default:
			return source[i], true
		}
	}
	return 0, false
}

func firstValuePlaceholderRange(source string, start int, end int, value byte) string {
	bytes := make([]byte, 0, end-start)
	placed := false
	for i := start; i < end; i++ {
		ch := source[i]
		if ch == '\n' || ch == '\r' {
			bytes = append(bytes, ch)
			continue
		}
		if !placed {
			placed = true
			bytes = append(bytes, value)
			continue
		}
		bytes = append(bytes, ' ')
	}
	return string(bytes)
}

func preserveLineEndingsRange(source string, start int, end int, fill byte) string {
	bytes := make([]byte, 0, end-start)
	for i := start; i < end; i++ {
		ch := source[i]
		if ch == '\n' || ch == '\r' {
			bytes = append(bytes, ch)
		} else {
			bytes = append(bytes, fill)
		}
	}
	return string(bytes)
}

func (v VirtualDocument) ToSourceOffset(virtualOffset int) (int, bool) {
	index := sort.Search(len(v.Segments), func(index int) bool {
		return v.Segments[index].VirtualEnd >= virtualOffset
	})
	if index < len(v.Segments) {
		segment := v.Segments[index]
		if virtualOffset >= segment.VirtualStart {
			return segment.SourceStart + (virtualOffset - segment.VirtualStart), true
		}
	}
	return 0, false
}

// SourceRange maps a byte-offset range in the virtual document to the source
// document when the whole range belongs to one source-map segment.
func (v VirtualDocument) SourceRange(source *TextDocument, start, end int) (lsp.Range, bool) {
	if source == nil || start < 0 || end < start || end > len(v.Text) {
		return lsp.Range{}, false
	}
	if start == end {
		sourceOffset, ok := v.ToSourceOffset(start)
		if !ok {
			return lsp.Range{}, false
		}
		return source.Range(sourceOffset, sourceOffset), true
	}
	lastOffset := max(start, end-1)
	var segment SourceMapSegment
	found := false
	for _, candidate := range v.Segments {
		if start >= candidate.VirtualStart && start < candidate.VirtualEnd {
			segment = candidate
			found = true
			break
		}
	}
	if !found || lastOffset >= segment.VirtualEnd {
		return lsp.Range{}, false
	}
	sourceStart := segment.SourceStart + (start - segment.VirtualStart)
	sourceEnd := segment.SourceStart + (end - segment.VirtualStart)
	return source.Range(sourceStart, sourceEnd), true
}

// SourceRangeForVirtualRange maps an LSP range in the virtual document to the
// source document only when the range does not cross a masked region.
func (v VirtualDocument) SourceRangeForVirtualRange(source *TextDocument, r lsp.Range) (lsp.Range, bool) {
	virtual := v.runtimeTextDocument()
	return v.SourceRange(source, virtual.OffsetAt(r.Start), virtual.OffsetAt(r.End))
}

func (v VirtualDocument) ToVirtualOffset(sourceOffset int) (int, bool) {
	index := sort.Search(len(v.Segments), func(index int) bool {
		return v.Segments[index].SourceEnd >= sourceOffset
	})
	if index < len(v.Segments) {
		segment := v.Segments[index]
		if sourceOffset >= segment.SourceStart {
			return segment.VirtualStart + (sourceOffset - segment.SourceStart), true
		}
	}
	return 0, false
}

func (v VirtualDocument) ToSourcePosition(position lsp.Position, source *TextDocument) (lsp.Position, bool) {
	virtual := v.runtimeTextDocument()
	sourceOffset, ok := v.ToSourceOffset(virtual.OffsetAt(position))
	if !ok {
		return lsp.Position{}, false
	}
	return source.PositionAt(sourceOffset), true
}

func (v VirtualDocument) ToVirtualPosition(position lsp.Position, source *TextDocument) (lsp.Position, bool) {
	sourceOffset := source.OffsetAt(position)
	virtualOffset, ok := v.ToVirtualOffset(sourceOffset)
	if !ok {
		return lsp.Position{}, false
	}
	virtual := v.runtimeTextDocument()
	return virtual.PositionAt(virtualOffset), true
}

func (v *VirtualDocument) attachRuntimeDocument() {
	if v == nil || v.runtimeDoc != nil {
		return
	}
	v.runtimeDoc = NewTextDocument(v.URI, v.LanguageID, 0, v.Text)
}

func (v VirtualDocument) runtimeTextDocument() *TextDocument {
	if v.runtimeDoc != nil {
		return v.runtimeDoc
	}
	return NewTextDocument(v.URI, v.LanguageID, 0, v.Text)
}
