package lspserver

import (
	"context"
	"encoding/json"
	"sort"

	"github.com/yottonoko/classic-web-system-lsp/internal/core"
	"github.com/yottonoko/classic-web-system-lsp/internal/lsp"
)

var javaScriptSemanticTypeMap = map[int]int{
	0: 10, 1: 4, 2: 12, 3: 11, 4: 17, 5: 15, 6: 14, 7: 2, 8: 1, 9: 6,
	10: 13, 11: 18, 12: 19, 13: 3, 14: 5, 15: 20, 16: 21, 17: 7, 18: 8,
	19: 0, 20: 22, 21: 23, 22: 9,
}

type absoluteSemanticToken struct {
	line      int
	character int
	length    int
	tokenType int
	modifiers int
}

func (s *Server) javaScriptSemanticTokensContext(ctx context.Context, uri string) []absoluteSemanticToken {
	if ctx == nil {
		ctx = context.Background()
	}
	if !lockMutexContext(ctx, &s.javascriptMu) {
		return nil
	}
	defer s.javascriptMu.Unlock()
	doc, parsed := s.parsed(uri)
	if doc == nil || parsed == nil {
		return nil
	}
	var result []absoluteSemanticToken
	seenLanguages := map[core.EmbeddedLanguage]struct{}{}
	for _, region := range parsed.Regions {
		if ctx.Err() != nil {
			return nil
		}
		if region.Language != core.LanguageJavaScript && region.Language != core.LanguageJScript {
			continue
		}
		if _, seen := seenLanguages[region.Language]; seen {
			continue
		}
		seenLanguages[region.Language] = struct{}{}
		position := doc.PositionAt(region.ContentStart)
		request, ok := s.prepareJavaScriptRequestContext(ctx, uri, position)
		if !ok {
			continue
		}
		params := mustRaw(map[string]any{"textDocument": map[string]any{"uri": request.active.uri}})
		raw, err := request.project.Request(ctx, "textDocument/semanticTokens/full", params)
		if err != nil {
			continue
		}
		var tokens lsp.SemanticTokens
		if json.Unmarshal(raw, &tokens) != nil {
			continue
		}
		result = append(result, mapJavaScriptSemanticData(tokens.Data, request.active)...)
	}
	return result
}

func mapJavaScriptSemanticData(data []int, mapping *javaScriptVirtualFile) []absoluteSemanticToken {
	virtualDocument := mapping.virtualDocument
	if virtualDocument == nil {
		virtualDocument = core.NewTextDocument(mapping.uri, mapping.virtual.LanguageID, 0, mapping.virtual.Text)
	}
	line, character := 0, 0
	result := make([]absoluteSemanticToken, 0, len(data)/5)
	for index := 0; index+4 < len(data); index += 5 {
		line += data[index]
		if data[index] == 0 {
			character += data[index+1]
		} else {
			character = data[index+1]
		}
		virtualStart := lsp.Position{Line: line, Character: character}
		virtualEnd := lsp.Position{Line: line, Character: character + data[index+2]}
		startOffset := virtualDocument.OffsetAt(virtualStart)
		endOffset := virtualDocument.OffsetAt(virtualEnd)
		sourceStartOffset, startOK := mapping.virtual.ToSourceOffset(startOffset)
		sourceEndOffset, endOK := mapping.virtual.ToSourceOffset(endOffset)
		if !startOK || !endOK {
			continue
		}
		sourceStart := mapping.source.PositionAt(sourceStartOffset)
		sourceEnd := mapping.source.PositionAt(sourceEndOffset)
		mappedType, typeOK := javaScriptSemanticTypeMap[data[index+3]]
		if !typeOK || sourceStart.Line != sourceEnd.Line {
			continue
		}
		modifiers := 0
		if data[index+4]&(1<<2) != 0 {
			modifiers |= 1 << 2
		}
		if data[index+4]&(1<<9) != 0 {
			modifiers |= 1 << 3
		}
		result = append(result, absoluteSemanticToken{line: sourceStart.Line, character: sourceStart.Character, length: sourceEnd.Character - sourceStart.Character, tokenType: mappedType, modifiers: modifiers})
	}
	return result
}

func mergeSemanticTokenData(base []int, extra []absoluteSemanticToken) []int {
	tokens := decodeAbsoluteSemanticTokenData(base)
	tokens = append(tokens, extra...)
	sort.SliceStable(tokens, func(i, j int) bool {
		if tokens[i].line != tokens[j].line {
			return tokens[i].line < tokens[j].line
		}
		if tokens[i].character != tokens[j].character {
			return tokens[i].character < tokens[j].character
		}
		return tokens[i].length < tokens[j].length
	})
	result := make([]int, 0, len(tokens)*5)
	previousLine, previousCharacter := 0, 0
	for _, token := range tokens {
		deltaLine := token.line - previousLine
		deltaCharacter := token.character
		if deltaLine == 0 {
			deltaCharacter -= previousCharacter
		}
		result = append(result, deltaLine, deltaCharacter, token.length, token.tokenType, token.modifiers)
		previousLine, previousCharacter = token.line, token.character
	}
	return result
}

func decodeAbsoluteSemanticTokenData(data []int) []absoluteSemanticToken {
	line, character := 0, 0
	result := make([]absoluteSemanticToken, 0, len(data)/5)
	for index := 0; index+4 < len(data); index += 5 {
		line += data[index]
		if data[index] == 0 {
			character += data[index+1]
		} else {
			character = data[index+1]
		}
		result = append(result, absoluteSemanticToken{line: line, character: character, length: data[index+2], tokenType: data[index+3], modifiers: data[index+4]})
	}
	return result
}
