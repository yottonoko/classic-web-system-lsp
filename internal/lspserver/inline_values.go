package lspserver

import (
	"strconv"

	"github.com/yottonoko/classic-web-system-lsp/internal/javascript"
	"github.com/yottonoko/classic-web-system-lsp/internal/lsp"
)

func (s *Server) inlineValues(uri string, r lsp.Range) []lsp.InlineValueVariableLookup {
	_, parsed := s.parsed(uri)
	if parsed == nil {
		return nil
	}
	seen := map[string]struct{}{}
	values := []lsp.InlineValueVariableLookup{}
	for _, declaration := range collectVBUsageDeclarations(parsed).Declarations {
		if !isVBInlineValueSymbol(declaration.Kind) || !lspRangesOverlap(declaration.Range, r) {
			continue
		}
		key := declaration.Name + ":" + strconv.Itoa(declaration.Range.Start.Line) + ":" + strconv.Itoa(declaration.Range.Start.Character)
		if _, ok := seen[key]; ok {
			continue
		}
		seen[key] = struct{}{}
		values = append(values, lsp.InlineValueVariableLookup{
			Range:               declaration.Range,
			VariableName:        declaration.Name,
			CaseSensitiveLookup: false,
		})
	}
	for _, value := range javascript.InlineValues(parsed, r) {
		key := value.VariableName + ":" + strconv.Itoa(value.Range.Start.Line) + ":" + strconv.Itoa(value.Range.Start.Character)
		if _, ok := seen[key]; ok {
			continue
		}
		seen[key] = struct{}{}
		values = append(values, value)
	}
	return values
}

func isVBInlineValueSymbol(kind string) bool {
	switch kind {
	case "variable", "parameter", "constant", "field", "property":
		return true
	default:
		return false
	}
}

func lspRangesOverlap(left, right lsp.Range) bool {
	return compareLSPPositions(left.Start, right.End) < 0 && compareLSPPositions(left.End, right.Start) > 0
}

func compareLSPPositions(left, right lsp.Position) int {
	if left.Line != right.Line {
		return left.Line - right.Line
	}
	return left.Character - right.Character
}
