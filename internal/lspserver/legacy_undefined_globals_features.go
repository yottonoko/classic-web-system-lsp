package lspserver

import (
	"context"
	"strconv"
	"strings"

	"github.com/yottonoko/classic-web-system-lsp/internal/core"
	"github.com/yottonoko/classic-web-system-lsp/internal/lsp"
	workspacepkg "github.com/yottonoko/classic-web-system-lsp/internal/workspace"
)

func (s *Server) legacyUndefinedGlobalNames(ctx context.Context) map[string]struct{} {
	catalog, ok := s.workspaceLegacyUndefinedGlobals(ctx)
	if !ok {
		return nil
	}
	names := make(map[string]struct{}, len(catalog.Symbols))
	for _, symbol := range catalog.Symbols {
		names[strings.ToLower(symbol.Name)] = struct{}{}
	}
	return names
}

func (s *Server) legacyUndefinedGlobalHover(ctx context.Context, parsed *core.ParsedDocument, position lsp.Position) *lsp.Hover {
	if parsed == nil {
		return nil
	}
	symbol, ok := s.workspaceLegacyUndefinedGlobalAtInteractive(ctx, parsed.URI, position)
	if !ok {
		return nil
	}
	declaration := legacyUndefinedGlobalDeclaration(symbol)
	label := "(legacy global) Dim " + symbol.Name + " As Variant"
	if symbol.Kind == LegacyUndefinedGlobalFunction {
		label = "(legacy global) Function " + symbol.Name + "(...) As Variant"
	} else if symbol.Kind == LegacyUndefinedGlobalClass {
		label = "(legacy global) Class " + symbol.Name
	}
	value := "```vbscript\n" + label + "\n```\n\n" +
		"Assumed external global (`aspLsp.vbscript.assumeUndefinedGlobals`).\n\n" +
		"Observed references: " + strconv.Itoa(len(symbol.Locations))
	return &lsp.Hover{Contents: lsp.MarkupContent{Kind: "markdown", Value: value}, Range: &declaration.Range}
}

func (s *Server) legacyUndefinedGlobalDefinition(ctx context.Context, parsed *core.ParsedDocument, position lsp.Position) []lsp.Location {
	if parsed == nil {
		return nil
	}
	symbol, ok := s.workspaceLegacyUndefinedGlobalAtInteractive(ctx, parsed.URI, position)
	if !ok {
		return nil
	}
	return []lsp.Location{{URI: symbol.OriginURI, Range: symbol.Range}}
}

func (s *Server) legacyUndefinedGlobalReferences(ctx context.Context, parsed *core.ParsedDocument, position lsp.Position, includeDeclaration bool) ([]lsp.Location, bool) {
	if parsed == nil {
		return nil, false
	}
	symbol, ok := s.WorkspaceLegacyUndefinedGlobalAt(ctx, parsed.URI, position)
	if !ok {
		return nil, false
	}
	locations := make([]lsp.Location, 0, len(symbol.Locations))
	for _, location := range symbol.Locations {
		if !includeDeclaration && workspacepkg.SameFileIdentityURI(location.URI, symbol.OriginURI) && location.Range == symbol.Range {
			continue
		}
		locations = append(locations, location)
	}
	return locations, true
}

func (s *Server) legacyUndefinedGlobalRenameRange(ctx context.Context, parsed *core.ParsedDocument, position lsp.Position) (*lsp.Range, bool) {
	if parsed == nil {
		return nil, false
	}
	symbol, ok := s.workspaceLegacyUndefinedGlobalAtInteractive(ctx, parsed.URI, position)
	if !ok {
		return nil, false
	}
	r := symbol.Range
	for _, location := range symbol.Locations {
		if workspacepkg.SameFileIdentityURI(location.URI, parsed.URI) && lspPositionInRange(position, location.Range) {
			r = location.Range
			break
		}
	}
	return &r, true
}

func (s *Server) legacyUndefinedGlobalRename(ctx context.Context, parsed *core.ParsedDocument, position lsp.Position, newName string) (map[string]any, bool) {
	if parsed == nil {
		return nil, false
	}
	symbol, ok := s.workspaceLegacyUndefinedGlobalAtInteractive(ctx, parsed.URI, position)
	if !ok {
		return nil, false
	}
	changes := map[string][]lsp.TextEdit{}
	if !isVBIdentifierName(newName) {
		return map[string]any{"changes": changes}, true
	}
	for _, location := range symbol.Locations {
		changes[location.URI] = append(changes[location.URI], lsp.TextEdit{Range: location.Range, NewText: newName})
	}
	return map[string]any{"changes": changes}, true
}

func (s *Server) legacyUndefinedGlobalCompletions(ctx context.Context) []lsp.CompletionItem {
	catalog, ok := s.workspaceLegacyUndefinedGlobalsInteractive(ctx)
	if !ok {
		return nil
	}
	items := make([]lsp.CompletionItem, 0, len(catalog.Symbols))
	for _, symbol := range catalog.Symbols {
		kind := lsp.CompletionItemKindVariable
		detail := "Legacy external global variable or constant"
		switch symbol.Kind {
		case LegacyUndefinedGlobalFunction:
			kind = lsp.CompletionItemKindFunction
			detail = "Legacy external global function"
		case LegacyUndefinedGlobalClass:
			kind = lsp.CompletionItemKindClass
			detail = "Legacy external global class"
		}
		items = append(items, lsp.CompletionItem{Label: symbol.Name, Kind: kind, Detail: detail})
	}
	return items
}

func legacyUndefinedGlobalDeclaration(symbol LegacyUndefinedGlobalSymbol) vbUsageDeclaration {
	kind := "variable"
	typeName := "Variant"
	if symbol.Kind == LegacyUndefinedGlobalFunction {
		kind = "function"
	} else if symbol.Kind == LegacyUndefinedGlobalClass {
		kind = "class"
		typeName = symbol.Name
	}
	return vbUsageDeclaration{
		Name: symbol.Name, Kind: kind, Range: symbol.Range, Line: symbol.Range.Start.Line,
		Implicit: true, Uncertain: true, TypeName: typeName,
	}
}

func (s *Server) legacyUndefinedGlobalDocumentSymbols(ctx context.Context, uri string) []lsp.DocumentSymbol {
	catalog, ok := s.workspaceLegacyUndefinedGlobals(ctx)
	if !ok {
		return nil
	}
	var symbols []lsp.DocumentSymbol
	for _, symbol := range catalog.Symbols {
		if !workspacepkg.SameFileIdentityURI(symbol.OriginURI, uri) {
			continue
		}
		kind := 13
		if symbol.Kind == LegacyUndefinedGlobalFunction {
			kind = 12
		} else if symbol.Kind == LegacyUndefinedGlobalClass {
			kind = 5
		}
		symbols = append(symbols, lsp.DocumentSymbol{
			Name: symbol.Name, Detail: "Legacy external global", Kind: kind,
			Range: symbol.Range, SelectionRange: symbol.Range,
		})
	}
	return symbols
}

func (s *Server) legacyUndefinedGlobalWorkspaceSymbols(ctx context.Context, query string) []lsp.SymbolInformation {
	catalog, ok := s.workspaceLegacyUndefinedGlobals(ctx)
	if !ok {
		return nil
	}
	normalizedQuery := strings.ToLower(query)
	var symbols []lsp.SymbolInformation
	for _, symbol := range catalog.Symbols {
		if normalizedQuery != "" && !strings.Contains(strings.ToLower(symbol.Name), normalizedQuery) {
			continue
		}
		kind := 13
		if symbol.Kind == LegacyUndefinedGlobalFunction {
			kind = 12
		} else if symbol.Kind == LegacyUndefinedGlobalClass {
			kind = 5
		}
		symbols = append(symbols, lsp.SymbolInformation{
			Name: symbol.Name, Kind: kind,
			Location:      lsp.Location{URI: symbol.OriginURI, Range: symbol.Range},
			ContainerName: "Legacy external globals",
		})
	}
	return symbols
}
