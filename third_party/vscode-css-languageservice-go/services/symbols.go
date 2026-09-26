package services

import (
	"sort"
	"strings"

	"github.com/yottonoko/vscode-css-languageservice-go/lsp"
)

type documentSymbolEntry struct {
	name           string
	kind           lsp.SymbolKind
	rangeStart     int
	rangeEnd       int
	selectionStart int
	selectionEnd   int
	bodyStart      int
	bodyEnd        int
}

func FindDocumentSymbols(document *lsp.TextDocument) []lsp.SymbolInformation {
	entries := collectDocumentSymbolEntries(document.Text(), document.LanguageID)
	result := make([]lsp.SymbolInformation, 0, len(entries))
	for _, entry := range entries {
		result = append(result, lsp.SymbolInformation{
			Name: entry.name,
			Kind: entry.kind,
			Location: lsp.Location{
				URI:   document.URI,
				Range: rangeFromOffsets(document, entry.rangeStart, entry.rangeEnd),
			},
		})
	}
	return result
}

func FindDocumentSymbols2(document *lsp.TextDocument) []lsp.DocumentSymbol {
	entries := collectDocumentSymbolEntries(document.Text(), document.LanguageID)
	symbols := make([]lsp.DocumentSymbol, len(entries))
	for i, entry := range entries {
		symbols[i] = lsp.DocumentSymbol{
			Name:           entry.name,
			Kind:           entry.kind,
			Range:          rangeFromOffsets(document, entry.rangeStart, entry.rangeEnd),
			SelectionRange: rangeFromOffsets(document, entry.selectionStart, entry.selectionEnd),
		}
	}

	var result []lsp.DocumentSymbol
	var parents []struct {
		index int
		body  selectionInterval
	}
	for i, entry := range entries {
		for len(parents) > 0 {
			parent := parents[len(parents)-1]
			if parent.body.start <= entry.rangeStart && entry.rangeEnd <= parent.body.end {
				break
			}
			parents = parents[:len(parents)-1]
		}
		if len(parents) == 0 {
			result = append(result, symbols[i])
			parents = appendSymbolParent(parents, len(result)-1, entry)
			continue
		}
		parentIndex := parents[len(parents)-1].index
		result[parentIndex].Children = append(result[parentIndex].Children, symbols[i])
	}
	return result
}

func appendSymbolParent(parents []struct {
	index int
	body  selectionInterval
}, index int, entry documentSymbolEntry) []struct {
	index int
	body  selectionInterval
} {
	if entry.bodyStart >= 0 && entry.bodyEnd >= entry.bodyStart {
		parents = append(parents, struct {
			index int
			body  selectionInterval
		}{index: index, body: selectionInterval{start: entry.bodyStart, end: entry.bodyEnd}})
	}
	return parents
}

func collectDocumentSymbolEntries(text string, languageID string) []documentSymbolEntry {
	blocks := parseCSSBlocks(text)
	entries := preprocessorDocumentSymbolEntries(text, languageID, blocks)
	entries = append(entries, documentSymbolEntriesForBlocks(text, languageID, blocks)...)
	sort.SliceStable(entries, func(i, j int) bool {
		if entries[i].rangeStart == entries[j].rangeStart {
			return entries[i].rangeEnd > entries[j].rangeEnd
		}
		return entries[i].rangeStart < entries[j].rangeStart
	})
	return entries
}

func documentSymbolEntriesForBlocks(text string, languageID string, blocks []cssBlock) []documentSymbolEntry {
	if shouldParallelizeBlockWork(text, blocks) {
		return parallelBlockMap(blocks, parallelWorkerCount(len(blocks)), func(chunk []cssBlock) []documentSymbolEntry {
			return documentSymbolEntriesForBlockRange(text, languageID, chunk)
		})
	}
	return documentSymbolEntriesForBlockRange(text, languageID, blocks)
}

func documentSymbolEntriesForBlockRange(text string, languageID string, blocks []cssBlock) []documentSymbolEntry {
	var entries []documentSymbolEntry
	for _, block := range blocks {
		head := strings.TrimSpace(block.head)
		headLower := strings.ToLower(head)
		switch {
		case strings.HasPrefix(headLower, "@media"):
			entries = append(entries, mediaSymbolEntry(text, block))
		case strings.HasPrefix(headLower, "@scope"):
			entries = append(entries, scopeSymbolEntry(text, block))
		case strings.HasPrefix(headLower, "@"):
			continue
		case languageID == "less" && isLESSMixinHead(head):
			continue
		default:
			entries = append(entries, selectorSymbolEntries(text, block)...)
		}
	}
	return entries
}

func preprocessorDocumentSymbolEntries(text string, languageID string, blocks []cssBlock) []documentSymbolEntry {
	switch languageID {
	case "scss":
		return scssDocumentSymbolEntries(text, blocks)
	case "less":
		return lessDocumentSymbolEntries(text, blocks)
	default:
		return nil
	}
}

func scssDocumentSymbolEntries(text string, blocks []cssBlock) []documentSymbolEntry {
	var entries []documentSymbolEntry
	entries = append(entries, scssCallableSymbolEntries(text)...)
	for _, token := range scssVariableHighlightTokensForBlocks(text, blocks) {
		if !token.write {
			continue
		}
		entries = append(entries, documentSymbolEntry{
			name:           token.text,
			kind:           lsp.SymbolKindVariable,
			rangeStart:     token.start,
			rangeEnd:       token.end,
			selectionStart: token.start,
			selectionEnd:   token.end,
			bodyStart:      -1,
			bodyEnd:        -1,
		})
	}
	return entries
}

func lessDocumentSymbolEntries(text string, blocks []cssBlock) []documentSymbolEntry {
	if shouldParallelizeBlockWork(text, blocks) {
		return parallelBlockMap(blocks, parallelWorkerCount(len(blocks)), func(chunk []cssBlock) []documentSymbolEntry {
			return lessMixinSymbolEntries(text, chunk)
		})
	}
	return lessMixinSymbolEntries(text, blocks)
}

func lessMixinSymbolEntries(text string, blocks []cssBlock) []documentSymbolEntry {
	var entries []documentSymbolEntry
	for _, block := range blocks {
		start, end := trimRange(text, block.headStart, block.start)
		if start >= end || text[start] != '.' && text[start] != '#' {
			continue
		}
		nameEnd := start + 1
		for nameEnd < end && isSelectorIdentifierByte(text[nameEnd]) {
			nameEnd++
		}
		next := nameEnd
		for next < end && isCSSSpace(text[next]) {
			next++
		}
		if next >= end || text[next] != '(' {
			continue
		}
		entries = append(entries, documentSymbolEntry{
			name:           text[start:nameEnd],
			kind:           lsp.SymbolKindMethod,
			rangeStart:     block.headStart,
			rangeEnd:       block.end + 1,
			selectionStart: start,
			selectionEnd:   nameEnd,
			bodyStart:      block.bodyStart,
			bodyEnd:        block.bodyEnd,
		})
	}
	return entries
}

func isLESSMixinHead(head string) bool {
	head = strings.TrimSpace(head)
	if head == "" || head[0] != '.' && head[0] != '#' {
		return false
	}
	nameEnd := 1
	for nameEnd < len(head) && isSelectorIdentifierByte(head[nameEnd]) {
		nameEnd++
	}
	next := nameEnd
	for next < len(head) && isCSSSpace(head[next]) {
		next++
	}
	return next < len(head) && head[next] == '('
}

func scssCallableSymbolEntries(text string) []documentSymbolEntry {
	var entries []documentSymbolEntry
	for i := 0; i < len(text); i++ {
		next := skipCSSIgnored(text, i)
		if next != i {
			i = next
			continue
		}
		if text[i] != '@' {
			continue
		}
		keyword := ""
		kind := lsp.SymbolKindMethod
		switch {
		case hasASCIIPrefixFold(text, i, "@mixin"):
			keyword = "@mixin"
			kind = lsp.SymbolKindMethod
		case hasASCIIPrefixFold(text, i, "@function"):
			keyword = "@function"
			kind = lsp.SymbolKindFunction
		default:
			continue
		}
		nameStart := i + len(keyword)
		for nameStart < len(text) && isCSSSpace(text[nameStart]) {
			nameStart++
		}
		if nameStart >= len(text) || !isSelectorIdentifierByte(text[nameStart]) {
			rangeEnd, bodyStart, bodyEnd := scssCallableSymbolRange(text, nameStart)
			entries = append(entries, documentSymbolEntry{
				name:           "<undefined>",
				kind:           kind,
				rangeStart:     i,
				rangeEnd:       rangeEnd,
				selectionStart: 0,
				selectionEnd:   0,
				bodyStart:      bodyStart,
				bodyEnd:        bodyEnd,
			})
			continue
		}
		nameEnd := nameStart + 1
		for nameEnd < len(text) && isSelectorIdentifierByte(text[nameEnd]) {
			nameEnd++
		}
		rangeEnd, bodyStart, bodyEnd := scssCallableSymbolRange(text, nameEnd)
		if bodyStart != -1 {
			bodyStart = nameEnd
		}
		entries = append(entries, documentSymbolEntry{
			name:           text[nameStart:nameEnd],
			kind:           kind,
			rangeStart:     i,
			rangeEnd:       rangeEnd,
			selectionStart: nameStart,
			selectionEnd:   nameEnd,
			bodyStart:      bodyStart,
			bodyEnd:        bodyEnd,
		})
		i = nameEnd - 1
	}
	return entries
}

func scssCallableSymbolRange(text string, afterName int) (int, int, int) {
	semicolon := -1
	for i := afterName; i < len(text); i++ {
		next := skipCSSIgnored(text, i)
		if next != i {
			i = next
			continue
		}
		switch text[i] {
		case ';':
			semicolon = i
			return semicolon + 1, -1, -1
		case '{':
			if close := matchingBraceInText(text, i); close != -1 {
				return close + 1, i + 1, close
			}
			return len(text), i + 1, len(text)
		case '}':
			return i, -1, -1
		}
	}
	return len(text), -1, -1
}

func matchingBraceInText(text string, open int) int {
	depth := 0
	for i := open; i < len(text); i++ {
		next := skipCSSIgnored(text, i)
		if next != i {
			i = next
			continue
		}
		switch text[i] {
		case '{':
			depth++
		case '}':
			depth--
			if depth == 0 {
				return i
			}
		}
	}
	return -1
}

func selectorSymbolEntries(text string, block cssBlock) []documentSymbolEntry {
	var entries []documentSymbolEntry
	for _, part := range splitSelectorParts(text, block.headStart, block.start) {
		name := strings.TrimSpace(text[part.start:part.end])
		if name == "" {
			continue
		}
		start, end := trimRange(text, part.start, part.end)
		entries = append(entries, documentSymbolEntry{
			name:           name,
			kind:           lsp.SymbolKindClass,
			rangeStart:     start,
			rangeEnd:       block.end + 1,
			selectionStart: start,
			selectionEnd:   end,
			bodyStart:      block.bodyStart,
			bodyEnd:        block.bodyEnd,
		})
	}
	return entries
}

func mediaSymbolEntry(text string, block cssBlock) documentSymbolEntry {
	headStart, headEnd := trimRange(text, block.headStart, block.start)
	selectionStart := headStart + len("@media")
	if selectionStart < headEnd && isCSSSpace(text[selectionStart]) {
		selectionStart++
	}
	return documentSymbolEntry{
		name:           strings.TrimSpace(text[headStart:headEnd]),
		kind:           lsp.SymbolKindModule,
		rangeStart:     block.headStart,
		rangeEnd:       block.end + 1,
		selectionStart: selectionStart,
		selectionEnd:   headEnd,
		bodyStart:      block.bodyStart,
		bodyEnd:        block.bodyEnd,
	}
}

func scopeSymbolEntry(text string, block cssBlock) documentSymbolEntry {
	headStart, headEnd := trimRange(text, block.headStart, block.start)
	selectionStart := headStart + len("@scope")
	if selectionStart < headEnd && isCSSSpace(text[selectionStart]) {
		selectionStart++
	}
	scopeName := scopeDisplayName(strings.TrimSpace(text[selectionStart:headEnd]))
	return documentSymbolEntry{
		name:           strings.TrimSpace("@scope " + scopeName),
		kind:           lsp.SymbolKindModule,
		rangeStart:     block.headStart,
		rangeEnd:       block.end + 1,
		selectionStart: selectionStart,
		selectionEnd:   headEnd,
		bodyStart:      block.bodyStart,
		bodyEnd:        block.bodyEnd,
	}
}

func scopeDisplayName(text string) string {
	if text == "" {
		return ""
	}
	parts := strings.Split(text, " to ")
	if len(parts) == 2 {
		return trimScopeSelector(parts[0]) + " → " + trimScopeSelector(parts[1])
	}
	return trimScopeSelector(text)
}

func trimScopeSelector(text string) string {
	text = strings.TrimSpace(text)
	if strings.HasPrefix(text, "(") && strings.HasSuffix(text, ")") && len(text) >= 2 {
		text = strings.TrimSpace(text[1 : len(text)-1])
	}
	return text
}

func splitSelectorParts(text string, start, end int) []selectionInterval {
	var parts []selectionInterval
	partStart := start
	parenDepth := 0
	bracketDepth := 0
	for i := start; i < end; i++ {
		next := skipCSSIgnored(text, i)
		if next != i {
			i = next
			continue
		}
		switch text[i] {
		case '(':
			parenDepth++
		case ')':
			if parenDepth > 0 {
				parenDepth--
			}
		case '[':
			bracketDepth++
		case ']':
			if bracketDepth > 0 {
				bracketDepth--
			}
		case ',':
			if parenDepth == 0 && bracketDepth == 0 {
				partEndStart, partEnd := trimRange(text, partStart, i)
				if partEndStart < partEnd {
					parts = append(parts, selectionInterval{start: partEndStart, end: partEnd})
				}
				partStart = i + 1
			}
		}
	}
	partEndStart, partEnd := trimRange(text, partStart, end)
	if partEndStart < partEnd {
		parts = append(parts, selectionInterval{start: partEndStart, end: partEnd})
	}
	return parts
}

func rangeFromOffsets(document *lsp.TextDocument, start, end int) lsp.Range {
	return rangeFromByteOffsets(document, start, end)
}
