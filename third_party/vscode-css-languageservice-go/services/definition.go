package services

import "github.com/yottonoko/vscode-css-languageservice-go/lsp"

func Definition(document *lsp.TextDocument, position lsp.Position) *lsp.Location {
	tokens := highlightTokensForDocument(document)
	selected, ok := highlightTokenAt(tokens, byteOffsetAtPosition(document, position))
	if !ok {
		return nil
	}
	for _, token := range tokens {
		if !token.write || !highlightTokensMatch(selected, token) {
			continue
		}
		location := lsp.Location{URI: document.URI, Range: rangeFromOffsets(document, token.start, token.end)}
		return &location
	}
	return nil
}
