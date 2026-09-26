package lspserver

import (
	"context"
	"sort"
	"strings"

	"github.com/yottonoko/classic-web-system-lsp/internal/core"
	"github.com/yottonoko/classic-web-system-lsp/internal/lsp"
	"github.com/yottonoko/classic-web-system-lsp/internal/tsgoadapter"
)

type typeScriptGoNavigationCandidates struct {
	ctx context.Context
}

func (p typeScriptGoNavigationCandidates) Candidates(parsed *core.ParsedDocument, region core.Region) ([]navigationFiniteCandidate, error) {
	return p.CandidatesWithReplacements(parsed, region, nil, nil)
}

// CandidatesWithReplacements analyzes a JavaScript region after the caller
// has rendered occurrence-specific ASP expressions. Generated replacement
// text is intentionally unmapped, so expressionRegions supplies the source
// ranges to use when a sink range crosses generated text.
func (p typeScriptGoNavigationCandidates) CandidatesWithReplacements(parsed *core.ParsedDocument, region core.Region, replacements []core.VirtualDocumentReplacement, expressionRegions []core.Region) ([]navigationFiniteCandidate, error) {
	if parsed == nil || p.ctx == nil {
		return nil, nil
	}
	source, err := core.NewTextDocumentContext(p.ctx, parsed.URI, "classic-asp", 0, parsed.Text)
	if err != nil {
		return nil, err
	}
	return p.CandidatesWithReplacementsSource(parsed, source, region, replacements, expressionRegions)
}

func (p typeScriptGoNavigationCandidates) CandidatesWithReplacementsSource(parsed *core.ParsedDocument, source *core.TextDocument, region core.Region, replacements []core.VirtualDocumentReplacement, expressionRegions []core.Region) ([]navigationFiniteCandidate, error) {
	if parsed == nil || p.ctx == nil {
		return nil, nil
	}
	if err := p.ctx.Err(); err != nil {
		return nil, err
	}
	virtual := core.BuildEmbeddedRegionVirtualDocument(parsed, region)
	if len(replacements) > 0 {
		var err error
		virtual, err = core.BuildEmbeddedRegionVirtualDocumentWithReplacementsContext(p.ctx, parsed, region, replacements)
		if err != nil {
			return nil, err
		}
	}
	analysis, err := tsgoadapter.AnalyzeJavaScriptNavigation(p.ctx, virtual.Text)
	if err != nil {
		return nil, err
	}
	if analysis.Cancelled {
		if err := p.ctx.Err(); err != nil {
			return nil, err
		}
		return nil, context.Canceled
	}
	if err := p.ctx.Err(); err != nil {
		return nil, err
	}
	if source == nil || source.Text != parsed.Text || source.URI != parsed.URI {
		var err error
		source, err = core.NewTextDocumentContext(p.ctx, parsed.URI, "classic-asp", 0, parsed.Text)
		if err != nil {
			return nil, err
		}
	}
	if len(expressionRegions) == 0 && len(replacements) > 0 {
		expressionRegions = navigationJavaScriptReplacementRegions(parsed, region, replacements)
	}
	if len(expressionRegions) > 1 {
		expressionRegions = append([]core.Region(nil), expressionRegions...)
		sort.SliceStable(expressionRegions, func(left, right int) bool {
			if expressionRegions[left].Start != expressionRegions[right].Start {
				return expressionRegions[left].Start < expressionRegions[right].Start
			}
			return expressionRegions[left].End < expressionRegions[right].End
		})
	}
	replacementSpans := navigationJavaScriptReplacementSpans(region, replacements)
	candidates := make([]navigationFiniteCandidate, 0, len(analysis.Sinks))
	for _, sink := range analysis.Sinks {
		if err := p.ctx.Err(); err != nil {
			return nil, err
		}
		// An action assignment is state collected by the adapter. The graph edge
		// is emitted only when that form is actually submitted.
		if sink.Kind == "javascriptFormAction" || len(sink.Expression.Values) == 0 {
			continue
		}
		rangeValue, ok := virtual.SourceRange(source, sink.Expression.Range.ByteStart, sink.Expression.Range.ByteEnd)
		if !ok {
			rangeValue, ok = virtual.SourceRange(source, sink.Range.ByteStart, sink.Range.ByteEnd)
		}
		expressionRanges, expressionSnippets, _, err := navigationJavaScriptReplacementSourceRanges(p.ctx, source, replacementSpans, expressionRegions, sink.Expression.Range.ByteStart, sink.Expression.Range.ByteEnd)
		if err != nil {
			return nil, err
		}
		var fallbackRange lsp.Range
		hasFallbackRange := false
		if !ok && len(expressionRanges) > 0 {
			for _, offset := range []int{sink.Expression.Range.ByteEnd, sink.Range.ByteEnd, sink.Expression.Range.ByteStart, sink.Range.ByteStart} {
				if mapped, mappedOK := virtual.SourceRange(source, offset, offset); mappedOK {
					fallbackRange, hasFallbackRange = mapped, true
					break
				}
			}
			rangeValue, ok = expressionRanges[0], true
		}
		if !ok {
			continue
		}
		values := make([]navigationValue, 0, len(sink.Expression.Values))
		dependencyMarkers := make([]string, 0)
		for _, value := range sink.Expression.Values {
			dependencyMarkers = append(dependencyMarkers, value.Dependencies...)
			text := value.Text
			if value.Kind == tsgoadapter.NavigationValueUnknown && (text == "" || text == "{unknown}") {
				text = strings.TrimSpace(sink.Expression.Text)
				if text == "" {
					text = "{unknown}"
				}
			}
			converted := navigationValue{
				Kind:      navigationValueKindFromTypeScriptGo(value.Kind),
				Primitive: navigationJavaScriptInferPrimitive(text),
				Text:      text,
			}
			values = append(values, converted)
		}
		value := navigationMergeVBValueList(values)
		extra := map[string]any{"confidence": "probable"}
		if hasFallbackRange {
			extra[navigationJavaScriptFallbackRangeKey] = fallbackRange
		}
		if len(dependencyMarkers) > 0 {
			extra[navigationJavaScriptDependencyMarkersKey] = dependencyMarkers
		}
		if len(values) > 1 {
			extra["confidence"] = "possible"
		}
		if sink.TargetFrame != "" {
			extra["targetFrame"] = sink.TargetFrame
		}
		if sink.Kind == "javascriptFormSubmit" {
			method := strings.ToUpper(strings.TrimSpace(sink.Method))
			if method == "" {
				method = "GET"
			}
			extra["method"] = method
		}
		if len(expressionRanges) > 0 {
			extra["javascriptExpressionRanges"] = expressionRanges
			extra["javascriptExpressionSnippets"] = expressionSnippets
		}
		candidates = append(candidates, navigationFiniteCandidate{
			Kind: sink.Kind, Value: value, Range: rangeValue,
			Snippet: strings.TrimSpace(sink.Snippet),
			Extra:   extra,
		})
	}
	return candidates, nil
}

func navigationJavaScriptReplacementRegions(parsed *core.ParsedDocument, owner core.Region, replacements []core.VirtualDocumentReplacement) []core.Region {
	if parsed == nil || len(replacements) == 0 {
		return nil
	}
	regions := make([]core.Region, 0, len(replacements))
	for _, replacement := range replacements {
		for _, region := range parsed.Regions {
			if region.Kind == core.RegionASPExpression && region.Start == replacement.SourceStart && region.End == replacement.SourceEnd && region.Start >= owner.ContentStart && region.End <= owner.ContentEnd {
				regions = append(regions, region)
				break
			}
		}
	}
	return regions
}

func navigationValueKindFromTypeScriptGo(kind tsgoadapter.NavigationValueKind) navigationValueKind {
	switch kind {
	case tsgoadapter.NavigationValueLiteral:
		return navigationValueLiteral
	case tsgoadapter.NavigationValueTemplate:
		return navigationValueTemplate
	default:
		return navigationValueUnknown
	}
}
