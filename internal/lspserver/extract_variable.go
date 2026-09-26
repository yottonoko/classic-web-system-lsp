package lspserver

import (
	"strconv"
	"strings"

	"github.com/yottonoko/classic-web-system-lsp/internal/core"
	"github.com/yottonoko/classic-web-system-lsp/internal/lsp"
	"github.com/yottonoko/classic-web-system-lsp/internal/vbscript"
)

func (s *Server) extractVariableCodeActions(params codeActionParams, selectionStart, selectionEnd int) []lsp.CodeAction {
	doc, parsed := s.parsed(params.TextDocument.URI)
	if doc == nil || parsed == nil {
		return nil
	}
	if selectionStart >= selectionEnd || params.Range.Start.Line != params.Range.End.Line {
		return nil
	}
	selected := doc.Text[selectionStart:selectionEnd]
	if selected == "" || strings.TrimSpace(selected) != selected {
		return nil
	}
	region := core.RegionAt(parsed, selectionStart)
	if region == nil || region.Language != core.LanguageVBScript || selectionEnd > region.ContentEnd {
		return nil
	}
	name := nextExtractedVariableName(parsed)
	insert := lsp.Range{
		Start: lsp.Position{Line: params.Range.Start.Line, Character: 0},
		End:   lsp.Position{Line: params.Range.Start.Line, Character: 0},
	}
	newText := "Dim " + name + "\n" + name + " = " + selected + "\n"
	return []lsp.CodeAction{{
		Title: "Extract VBScript variable",
		Kind:  "refactor.extract",
		Edit: &lsp.WorkspaceEdit{Changes: map[string][]lsp.TextEdit{
			params.TextDocument.URI: {
				{Range: insert, NewText: newText},
				{Range: params.Range, NewText: name},
			},
		}},
	}}
}

func nextExtractedVariableName(parsed *core.ParsedDocument) string {
	const base = "extractedValue"
	used := map[string]struct{}{}
	for name := range vbscript.BuildSymbolIndex(parsed).Declarations {
		used[strings.ToLower(name)] = struct{}{}
	}
	if _, ok := used[strings.ToLower(base)]; !ok {
		return base
	}
	for index := 1; ; index++ {
		candidate := base + strconv.Itoa(index)
		if _, ok := used[strings.ToLower(candidate)]; !ok {
			return candidate
		}
	}
}
