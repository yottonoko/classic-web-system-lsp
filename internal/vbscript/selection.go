package vbscript

import (
	"sort"

	"github.com/yottonoko/classic-web-system-lsp/internal/core"
	"github.com/yottonoko/classic-web-system-lsp/internal/lsp"
)

// SelectionRange returns the CST-backed smart-selection chain at position.
func SelectionRange(parsed *core.ParsedDocument, position lsp.Position) *lsp.SelectionRange {
	if parsed == nil {
		return nil
	}
	source := core.NewTextDocument(parsed.URI, "classic-asp", 0, parsed.Text)
	offset := source.OffsetAt(position)
	region := core.RegionAt(parsed, offset)
	if region == nil || region.Language != core.LanguageVBScript {
		return nil
	}
	content := parsed.Text[region.ContentStart:region.ContentEnd]
	localOffset := offset - region.ContentStart
	ranges := []lsp.Range{}
	for _, token := range Tokenize(content) {
		if token.Kind == "whitespace" || token.Kind == "newline" || localOffset < token.Start || localOffset > token.End {
			continue
		}
		ranges = append(ranges, source.Range(region.ContentStart+token.Start, region.ContentStart+token.End))
		break
	}
	var nodes []*CSTNode
	collectContainingCSTNodes(ParseCST(content), localOffset, &nodes)
	sort.SliceStable(nodes, func(i, j int) bool {
		left := nodes[i].End - nodes[i].Start
		right := nodes[j].End - nodes[j].Start
		return left < right
	})
	for _, node := range nodes {
		if node.Kind == "Document" {
			continue
		}
		ranges = appendUniqueSelectionRange(ranges, source.Range(region.ContentStart+node.Start, region.ContentStart+node.End))
	}
	ranges = appendUniqueSelectionRange(ranges, source.Range(region.ContentStart, region.ContentEnd))
	ranges = appendUniqueSelectionRange(ranges, source.Range(0, len(parsed.Text)))
	if len(ranges) == 0 {
		return nil
	}
	var chain *lsp.SelectionRange
	for i := len(ranges) - 1; i >= 0; i-- {
		chain = &lsp.SelectionRange{Range: ranges[i], Parent: chain}
	}
	return chain
}

func collectContainingCSTNodes(node *CSTNode, offset int, result *[]*CSTNode) {
	if node == nil || offset < node.Start || offset > node.End {
		return
	}
	*result = append(*result, node)
	for _, child := range node.Children {
		collectContainingCSTNodes(child, offset, result)
	}
}

func appendUniqueSelectionRange(ranges []lsp.Range, candidate lsp.Range) []lsp.Range {
	for _, existing := range ranges {
		if existing == candidate {
			return ranges
		}
	}
	return append(ranges, candidate)
}
