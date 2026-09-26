package lspserver

import (
	"github.com/yottonoko/classic-web-system-lsp/internal/lsp"
)

type colorPresentationParams struct {
	TextDocument lsp.TextDocumentIdentifier `json:"textDocument"`
	Color        lsp.Color                  `json:"color"`
	Range        lsp.Range                  `json:"range"`
}

func (s *Server) documentColors(uri string) []lsp.ColorInformation {
	_, parsed := s.parsed(uri)
	if parsed == nil {
		return []lsp.ColorInformation{}
	}
	s.cssMu.Lock()
	defer s.cssMu.Unlock()
	return s.css.DocumentColors(parsed)
}

func (s *Server) colorPresentations(uri string, color lsp.Color, r lsp.Range) []lsp.ColorPresentation {
	_, parsed := s.parsed(uri)
	if parsed == nil {
		return []lsp.ColorPresentation{}
	}
	s.cssMu.Lock()
	defer s.cssMu.Unlock()
	return s.css.ColorPresentations(parsed, color, r)
}
