package lspserver

import (
	"context"
	"sort"
	"strings"

	"github.com/yottonoko/classic-web-system-lsp/internal/core"
	"github.com/yottonoko/classic-web-system-lsp/internal/lsp"
)

type navigationJavaScriptProgramSegment struct {
	unit       navigationVBExecutionUnit
	region     core.Region
	start, end int
}

// Classic scripts share page scope. Inline modules execute after classic scripts
// in isolated function scopes; offsets inside each original script stay mapped.
func (b *navigationGraphBuilder) addJavaScriptNavigationProgram(selected navigationVBHTMLProgram) bool {
	provider, ok := b.javascriptCandidates.(typeScriptGoNavigationCandidates)
	if !ok {
		return false
	}
	var classic, modules []navigationJavaScriptProgramSegment
	for _, unit := range selected.program {
		if err := b.navigationContextError(); err != nil {
			b.navigationError = err
			return true
		}
		if unit.parsed == nil || unit.truncated {
			continue
		}
		for _, region := range b.javascriptRegionsForRange(unit.parsed, unit.start, unit.end) {
			if region.Kind != core.RegionClientScript {
				return false
			}
			part := navigationJavaScriptProgramSegment{unit: unit, region: region}
			header := unit.parsed.Text[region.Start:region.ContentStart]
			if strings.EqualFold(strings.TrimSpace(htmlAttributeValue(header, "type")), "module") {
				// Async modules do not have a known order relative to classic scripts.
				for _, attr := range scanNavigationHTMLAttributes(header, 0, len(header)) {
					if attr.Name == "async" {
						return false
					}
				}
				modules = append(modules, part)
			} else {
				classic = append(classic, part)
			}
		}
	}
	if len(classic)+len(modules) < 2 && len(modules) == 0 {
		return false
	}
	key := "program\x00" + selected.key
	if _, exists := b.javascriptRendered[key]; exists {
		return true
	}
	ctx := b.cancelContext
	if ctx == nil {
		ctx = context.Background()
	}
	var source strings.Builder
	var segments []navigationJavaScriptProgramSegment
	var regions []core.Region
	valuesByOffset := make(map[int][]navigationValue)
	for index, part := range append(classic, modules...) {
		if err := b.navigationContextError(); err != nil {
			b.navigationError = err
			return true
		}
		if source.Len()+part.region.ContentEnd-part.region.ContentStart+40 > 8<<20 {
			return false
		}
		module := index >= len(classic)
		if module {
			source.WriteString("\n(function () {\n\"use strict\";\n")
		}
		part.start = source.Len()
		source.WriteString(part.unit.parsed.Text[part.region.ContentStart:part.region.ContentEnd])
		part.end = source.Len()
		segments = append(segments, part)
		for _, nested := range part.unit.parsed.Regions {
			if nested == part.region || nested.Start < part.region.ContentStart || nested.End > part.region.ContentEnd {
				continue
			}
			shifted := nested
			shift := part.start - part.region.ContentStart
			shifted.Start += shift
			shifted.End += shift
			shifted.ContentStart += shift
			shifted.ContentEnd += shift
			regions = append(regions, shifted)
			if nested.Kind == core.RegionASPExpression {
				valuesByOffset[shifted.Start] = b.vbExpressionValuesForRegion(part.unit.parsed, part.unit.ownerURI, nested, part.unit.occurrenceID)
			}
		}
		if module {
			source.WriteString("\n})();\n")
		} else {
			source.WriteString("\n;\n")
		}
	}
	if len(segments) == 0 {
		return false
	}
	combined := &core.ParsedDocument{URI: segments[0].unit.ownerURI, Text: source.String(), Regions: regions}
	region := core.Region{Kind: core.RegionClientScript, Language: core.LanguageJavaScript, End: len(combined.Text), ContentEnd: len(combined.Text)}
	document, err := core.NewTextDocumentContext(ctx, combined.URI, "javascript", 0, combined.Text)
	if err != nil {
		b.navigationError = err
		return true
	}
	analyzed, fallback, err := b.analyzeJavaScriptProgramVariants(provider, combined, document, region, valuesByOffset)
	if err != nil {
		b.navigationError = err
		return true
	}
	if fallback {
		return false
	}
	b.javascriptRendered[key] = struct{}{}
	previousCurrent, previousDocument, previousOccurrence := b.current, b.document, b.currentOccurrence
	defer func() {
		b.current, b.document, b.currentOccurrence = previousCurrent, previousDocument, previousOccurrence
	}()
	emitted := make(map[string]map[string]struct{})
	for _, analysis := range analyzed {
		variant := analysis.variant
		if len(variant.regions) > 0 {
			variant.sourceEvidence = make([]navigationJavaScriptSourceEvidence, len(variant.regions))
			for index, expression := range variant.regions {
				part, location, ok := b.mapJavaScriptProgramRange(segments, document, document.Range(expression.Start, expression.End))
				if !ok {
					if b.navigationError != nil {
						return true
					}
					continue
				}
				variant.sourceEvidence[index] = navigationJavaScriptSourceEvidence{uri: part.unit.parsed.URI, rangeValue: location, snippet: navigationJavaScriptEvidenceSnippet(part.unit.parsed.Text, part.region.ContentStart+expression.Start-part.start, part.region.ContentStart+expression.End-part.start)}
			}
		}
		for _, candidate := range analysis.candidates {
			if err := b.navigationContextError(); err != nil {
				b.navigationError = err
				return true
			}
			part, location, ok := b.mapJavaScriptProgramRange(segments, document, candidate.Range)
			if !ok {
				continue
			}
			candidate.Range = location
			b.current, b.document, b.currentOccurrence = part.unit.parsed, b.javascriptDocuments[part.unit.parsed], part.unit.occurrenceID
			sourceID := b.navigationSourceID(part.unit.ownerURI)
			if sourceID != "" {
				// Include the occurrence in deduplication: equal source coordinates in
				// repeated includes still represent distinct executions.
				occurrenceKey := part.unit.parsed.URI + "\x00" + part.unit.occurrenceID
				scoped := emitted[occurrenceKey]
				if scoped == nil {
					scoped = make(map[string]struct{})
					emitted[occurrenceKey] = scoped
				}
				b.addJavaScriptFiniteCandidates(part.unit.parsed, sourceID, []navigationFiniteCandidate{candidate}, &variant, scoped)
			}
			if b.navigationError != nil {
				return true
			}
		}
	}
	return true
}

func (b *navigationGraphBuilder) mapJavaScriptProgramRange(segments []navigationJavaScriptProgramSegment, document *core.TextDocument, location lsp.Range) (navigationJavaScriptProgramSegment, lsp.Range, bool) {
	start, end := document.OffsetAt(location.Start), document.OffsetAt(location.End)
	index := sort.Search(len(segments), func(index int) bool { return segments[index].end > start })
	if index == len(segments) {
		return navigationJavaScriptProgramSegment{}, lsp.Range{}, false
	}
	part := segments[index]
	if start < part.start || end > part.end {
		return part, lsp.Range{}, false
	}
	original := b.javascriptDocuments[part.unit.parsed]
	if original == nil {
		var err error
		ctx := b.cancelContext
		if ctx == nil {
			ctx = context.Background()
		}
		original, err = core.NewTextDocumentContext(ctx, part.unit.parsed.URI, "classic-asp", 0, part.unit.parsed.Text)
		if err != nil {
			b.navigationError = err
			return part, lsp.Range{}, false
		}
		b.javascriptDocuments[part.unit.parsed] = original
	}
	return part, original.Range(part.region.ContentStart+start-part.start, part.region.ContentStart+end-part.start), true
}

func (b *navigationGraphBuilder) analyzeJavaScriptProgramVariants(provider typeScriptGoNavigationCandidates, parsed *core.ParsedDocument, document *core.TextDocument, region core.Region, values map[int][]navigationValue) ([]navigationJavaScriptAnalyzedVariant, bool, error) {
	variants, err := navigationJavaScriptInterpolationVariantsContext(b.cancelContext, parsed, region, values)
	if err != nil {
		return nil, false, err
	}
	if len(variants) == 0 {
		candidates, err := provider.CandidatesWithReplacementsSource(parsed, document, region, nil, nil)
		return []navigationJavaScriptAnalyzedVariant{{candidates: candidates}}, false, err
	}
	variants, err = b.limitJavaScriptInterpolationVariants(parsed, region, values, variants)
	if err != nil {
		return nil, false, err
	}
	if len(variants) == 0 || len(variants) == 1 && variants[0].budgetFallback {
		return nil, true, nil
	}
	dependency, prefix, count, err := navigationJavaScriptInterpolationDependencyVariantContext(b.cancelContext, parsed, region)
	if err != nil {
		return nil, false, err
	}
	dependencyCandidates, err := provider.CandidatesWithReplacementsSource(parsed, document, region, dependency.replacements, dependency.regions)
	if err != nil {
		return nil, false, err
	}
	dependencies := navigationJavaScriptCandidateDependencies(dependencyCandidates, prefix, count)
	analyzed := make([]navigationJavaScriptAnalyzedVariant, 0, len(variants))
	for _, variant := range variants {
		candidates, err := provider.CandidatesWithReplacementsSource(parsed, document, region, variant.replacements, variant.regions)
		if err != nil {
			return nil, false, err
		}
		analyzed = append(analyzed, navigationJavaScriptAnalyzedVariant{variant: variant, candidates: candidates})
	}
	if err := navigationJavaScriptInferVariantDependencies(b.cancelContext, analyzed, dependencyCandidates, dependencies); err != nil {
		return nil, false, err
	}
	for index := range analyzed {
		navigationJavaScriptAttachCandidateDependencies(analyzed[index].candidates, dependencies)
	}
	return analyzed, false, nil
}
