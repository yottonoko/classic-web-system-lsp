package services

import (
	"strings"
	"sync"

	"github.com/yottonoko/vscode-css-languageservice-go/lsp"
)

type highlightToken struct {
	text       string
	start      int
	end        int
	kind       string
	write      bool
	scopeStart int
	scopeEnd   int
}

type cachedHighlightTokens struct {
	text       string
	languageID string
	tokens     []highlightToken
}

// highlightTokenCacheLimit bounds the cache: callers create a new document for
// every edit, so an unbounded cache would retain every revision's text.
const highlightTokenCacheLimit = 8

var highlightTokenCache = struct {
	sync.Mutex
	entries map[*lsp.TextDocument]cachedHighlightTokens
	order   []*lsp.TextDocument
}{entries: map[*lsp.TextDocument]cachedHighlightTokens{}}

func highlightTokensForDocument(document *lsp.TextDocument) []highlightToken {
	text := document.Text()
	highlightTokenCache.Lock()
	entry, ok := highlightTokenCache.entries[document]
	highlightTokenCache.Unlock()
	if ok && entry.text == text && entry.languageID == document.LanguageID {
		return entry.tokens
	}
	tokens := collectHighlightTokens(text, document.LanguageID)
	highlightTokenCache.Lock()
	defer highlightTokenCache.Unlock()
	if _, exists := highlightTokenCache.entries[document]; !exists {
		if len(highlightTokenCache.order) >= highlightTokenCacheLimit {
			delete(highlightTokenCache.entries, highlightTokenCache.order[0])
			highlightTokenCache.order = append(highlightTokenCache.order[:0], highlightTokenCache.order[1:]...)
		}
		highlightTokenCache.order = append(highlightTokenCache.order, document)
	}
	highlightTokenCache.entries[document] = cachedHighlightTokens{text: text, languageID: document.LanguageID, tokens: tokens}
	return tokens
}

func FindDocumentHighlights(document *lsp.TextDocument, position lsp.Position) []lsp.DocumentHighlight {
	tokens := highlightTokensForDocument(document)
	selected, ok := highlightTokenAt(tokens, byteOffsetAtPosition(document, position))
	if !ok {
		return nil
	}
	rootShadowed := variableRootShadowed(selected, tokens)
	var result []lsp.DocumentHighlight
	for _, token := range tokens {
		if rootShadowed && token.scopeStart == -1 && (token.kind == "less-variable" || token.kind == "scss-variable") {
			continue
		}
		if !highlightTokensMatch(selected, token) {
			continue
		}
		kind := lsp.DocumentHighlightKindRead
		if token.write {
			kind = lsp.DocumentHighlightKindWrite
		}
		result = append(result, lsp.DocumentHighlight{Range: rangeFromOffsets(document, token.start, token.end), Kind: kind})
	}
	return result
}

func variableRootShadowed(selected highlightToken, tokens []highlightToken) bool {
	if selected.scopeStart == -1 || selected.kind != "less-variable" && selected.kind != "scss-variable" {
		return false
	}
	for _, token := range tokens {
		if token.kind == selected.kind &&
			token.text == selected.text &&
			token.scopeStart == selected.scopeStart &&
			token.scopeEnd == selected.scopeEnd &&
			token.write {
			return true
		}
	}
	return false
}

func PrepareRename(document *lsp.TextDocument, position lsp.Position) *lsp.Range {
	tokens := highlightTokensForDocument(document)
	selected, ok := highlightTokenAt(tokens, byteOffsetAtPosition(document, position))
	if !ok {
		return nil
	}
	r := rangeFromOffsets(document, selected.start, selected.end)
	return &r
}

func Rename(document *lsp.TextDocument, position lsp.Position, newName string) lsp.WorkspaceEdit {
	highlights := FindDocumentHighlights(document, position)
	edits := make([]lsp.TextEdit, len(highlights))
	for i, highlight := range highlights {
		edits[i] = lsp.Replace(highlight.Range, newName)
	}
	return lsp.WorkspaceEdit{Changes: map[lsp.DocumentURI][]lsp.TextEdit{document.URI: edits}}
}

func collectHighlightTokens(text string, languageID string) []highlightToken {
	blocks := parseCSSBlocks(text)
	tokens := preprocessorHighlightTokens(text, languageID, blocks)
	tokens = append(tokens, highlightTokensForBlocks(text, languageID, blocks)...)
	return tokens
}

func highlightTokensForBlocks(text string, languageID string, blocks []cssBlock) []highlightToken {
	if shouldParallelizeBlockWork(text, blocks) {
		return parallelBlockMap(blocks, parallelWorkerCount(len(blocks)), func(chunk []cssBlock) []highlightToken {
			return highlightTokensForBlockRange(text, languageID, chunk)
		})
	}
	return highlightTokensForBlockRange(text, languageID, blocks)
}

func highlightTokensForBlockRange(text string, languageID string, blocks []cssBlock) []highlightToken {
	var tokens []highlightToken
	for _, block := range blocks {
		head := strings.TrimSpace(block.head)
		headLower := strings.ToLower(head)
		if strings.HasPrefix(headLower, "@keyframes") || strings.HasPrefix(headLower, "@-") && strings.Contains(headLower, "keyframes") {
			if token, ok := keyframeNameToken(text, block); ok {
				tokens = append(tokens, token)
			}
			continue
		}
		if strings.HasPrefix(headLower, "@") {
			continue
		}
		if !(languageID == "less" && isLESSMixinHead(head)) {
			tokens = append(tokens, selectorHighlightTokens(text, block)...)
		}
		for _, declaration := range parseDeclarations(text, block.bodyStart, block.bodyEnd) {
			writeVariable := strings.HasPrefix(declaration.name, "--")
			tokens = append(tokens, highlightToken{
				text:  declaration.name,
				start: declaration.nameOffset,
				end:   declaration.nameOffset + len(declaration.name),
				kind:  propertyHighlightKind(declaration.name),
				write: writeVariable,
			})
			tokens = append(tokens, valueHighlightTokens(declaration)...)
		}
	}
	return tokens
}

func scssHighlightTokens(text string, blocks []cssBlock) []highlightToken {
	tokens := scssVariableHighlightTokensForBlocks(text, blocks)
	callableTokens, functionNames := scssCallableHighlightTokens(text)
	tokens = append(tokens, callableTokens...)
	tokens = append(tokens, scssFunctionCallHighlightTokens(text, functionNames, callableTokens)...)
	tokens = append(tokens, scssExtendReferenceHighlightTokens(text)...)
	return tokens
}

func preprocessorHighlightTokens(text string, languageID string, blocks []cssBlock) []highlightToken {
	switch languageID {
	case "scss":
		return scssHighlightTokens(text, blocks)
	case "less":
		return lessHighlightTokens(text, blocks)
	default:
		return nil
	}
}

func lessHighlightTokens(text string, blocks []cssBlock) []highlightToken {
	tokens := lessVariableHighlightTokensForBlocks(text, blocks)
	tokens = append(tokens, lessMixinHighlightTokens(text)...)
	tokens = append(tokens, lessSelectorReferenceHighlightTokens(text)...)
	return tokens
}

func lessVariableHighlightTokens(text string) []highlightToken {
	return lessVariableHighlightTokensForBlocks(text, parseCSSBlocks(text))
}

func lessVariableHighlightTokensForBlocks(text string, blocks []cssBlock) []highlightToken {
	var tokens []highlightToken
	for i := 0; i < len(text); i++ {
		next := skipCSSIgnored(text, i)
		if next != i {
			i = next
			continue
		}
		if text[i] != '@' || i+1 >= len(text) || !isSelectorIdentifierByte(text[i+1]) {
			continue
		}
		end := i + 2
		for end < len(text) && isSelectorIdentifierByte(text[end]) {
			end++
		}
		name := text[i:end]
		scopeStart, scopeEnd := variableScopeAtBlocks(blocks, i)
		if isLESSAtRuleName(name) {
			i = end - 1
			continue
		}
		tokens = append(tokens, highlightToken{
			text:       name,
			start:      i,
			end:        end,
			kind:       "less-variable",
			write:      lessVariableIsWrite(text, i, end),
			scopeStart: scopeStart,
			scopeEnd:   scopeEnd,
		})
		i = end - 1
	}
	return tokens
}

func lessVariableIsWrite(text string, start, end int) bool {
	next := end
	for next < len(text) && isCSSSpace(text[next]) {
		next++
	}
	if next < len(text) && text[next] == ':' {
		return true
	}
	prev := previousNonSpace(text, start-1)
	if prev != '(' && prev != ',' && prev != ';' {
		return false
	}
	segmentStart := lastStatementBoundary(text, start)
	segment := text[segmentStart:start]
	return strings.Contains(segment, "(")
}

func lessMixinHighlightTokens(text string) []highlightToken {
	var tokens []highlightToken
	for i := 0; i < len(text); i++ {
		next := skipCSSIgnored(text, i)
		if next != i {
			i = next
			continue
		}
		if text[i] != '.' && text[i] != '#' {
			continue
		}
		start := i
		end := i + 1
		for end < len(text) && isSelectorIdentifierByte(text[end]) {
			end++
		}
		if end == i+1 {
			continue
		}
		after := end
		for after < len(text) && isCSSSpace(text[after]) {
			after++
		}
		if after >= len(text) || text[after] != '(' {
			continue
		}
		tokens = append(tokens, highlightToken{
			text:  text[start:end],
			start: start,
			end:   end,
			kind:  "less-mixin",
			write: lessMixinIsWrite(text, after),
		})
		i = end - 1
	}
	return tokens
}

func lessMixinIsWrite(text string, openParen int) bool {
	closeParen := matchingParenString(text, openParen)
	if closeParen == -1 {
		return false
	}
	next := closeParen + 1
	for next < len(text) && isCSSSpace(text[next]) {
		next++
	}
	if strings.HasPrefix(strings.ToLower(text[next:]), "when") {
		return true
	}
	return next < len(text) && text[next] == '{'
}

func lessSelectorReferenceHighlightTokens(text string) []highlightToken {
	var tokens []highlightToken
	for i := 0; i < len(text); i++ {
		next := skipCSSIgnored(text, i)
		if next != i {
			i = next
			continue
		}
		if text[i] != '.' && text[i] != '#' {
			continue
		}
		start := i
		end := i + 1
		for end < len(text) && isSelectorIdentifierByte(text[end]) {
			end++
		}
		if end == i+1 {
			continue
		}
		after := end
		for after < len(text) && isCSSSpace(text[after]) {
			after++
		}
		if after < len(text) && text[after] == ';' {
			tokens = append(tokens, highlightToken{text: text[start:end], start: start, end: end, kind: "selector"})
		}
		i = end - 1
	}
	return tokens
}

func isLESSAtRuleName(name string) bool {
	switch strings.ToLower(name) {
	case "@charset", "@container", "@document", "@font-face", "@import", "@import-once", "@keyframes", "@media", "@namespace", "@page", "@plugin", "@supports", "@viewport":
		return true
	default:
		return strings.HasPrefix(strings.ToLower(name), "@-")
	}
}

func scssVariableHighlightTokens(text string) []highlightToken {
	return scssVariableHighlightTokensForBlocks(text, parseCSSBlocks(text))
}

func scssVariableHighlightTokensForBlocks(text string, blocks []cssBlock) []highlightToken {
	var tokens []highlightToken
	for i := 0; i < len(text); i++ {
		next := skipCSSIgnored(text, i)
		if next != i {
			i = next
			continue
		}
		if text[i] != '$' || i+1 >= len(text) || !isSelectorIdentifierByte(text[i+1]) {
			continue
		}
		end := i + 2
		for end < len(text) && isSelectorIdentifierByte(text[end]) {
			end++
		}
		scopeStart, scopeEnd := variableScopeAtBlocks(blocks, i)
		if scssVariableIsDeclarationDefaultValue(text, i) {
			scopeStart, scopeEnd = -1, -1
		} else if scssVariableIsNamedArgument(text, i) {
			scopeStart, scopeEnd = -2, -2
		}
		tokens = append(tokens, highlightToken{
			text:       text[i:end],
			start:      i,
			end:        end,
			kind:       "scss-variable",
			write:      scssVariableIsWrite(text, i, end),
			scopeStart: scopeStart,
			scopeEnd:   scopeEnd,
		})
		i = end - 1
	}
	return tokens
}

func scssVariableIsDeclarationDefaultValue(text string, start int) bool {
	prev := previousNonSpace(text, start-1)
	if prev != ':' {
		return false
	}
	segmentStart := lastStatementBoundary(text, start)
	segment := strings.ToLower(text[segmentStart:start])
	return strings.Contains(segment, "@mixin") || strings.Contains(segment, "@function")
}

func scssVariableIsWrite(text string, start, end int) bool {
	next := end
	for next < len(text) && isCSSSpace(text[next]) {
		next++
	}
	if next < len(text) && text[next] == ':' {
		return !scssVariableIsNamedArgument(text, start)
	}
	prev := previousNonSpace(text, start-1)
	if prev != '(' && prev != ',' {
		return false
	}
	segmentStart := lastStatementBoundary(text, start)
	segment := strings.ToLower(text[segmentStart:start])
	return strings.Contains(segment, "@mixin") || strings.Contains(segment, "@function") || strings.Contains(segment, "using")
}

func scssVariableIsNamedArgument(text string, start int) bool {
	prev := previousNonSpace(text, start-1)
	if prev != '(' && prev != ',' {
		return false
	}
	end := start + 1
	for end < len(text) && isSelectorIdentifierByte(text[end]) {
		end++
	}
	next := end
	for next < len(text) && isCSSSpace(text[next]) {
		next++
	}
	if next >= len(text) || text[next] != ':' {
		return false
	}
	segmentStart := lastStatementBoundary(text, start)
	segment := strings.ToLower(text[segmentStart:start])
	return !strings.Contains(segment, "@mixin") && !strings.Contains(segment, "@function")
}

func scssCallableHighlightTokens(text string) ([]highlightToken, map[string]bool) {
	var tokens []highlightToken
	functionNames := map[string]bool{}
	for i := 0; i < len(text); i++ {
		next := skipCSSIgnored(text, i)
		if next != i {
			i = next
			continue
		}
		if text[i] != '@' {
			continue
		}
		keyword, kind, write := "", "", true
		switch {
		case hasASCIIPrefixFold(text, i, "@mixin"):
			keyword, kind = "@mixin", "scss-mixin"
		case hasASCIIPrefixFold(text, i, "@function"):
			keyword, kind = "@function", "scss-function"
		case hasASCIIPrefixFold(text, i, "@include"):
			keyword, kind, write = "@include", "scss-mixin", false
		default:
			continue
		}
		nameStart := skipCSSIgnoredAndSpaces(text, i+len(keyword))
		if nameStart >= len(text) || !isSelectorIdentifierByte(text[nameStart]) {
			continue
		}
		nameEnd := nameStart + 1
		for nameEnd < len(text) && isSelectorIdentifierByte(text[nameEnd]) {
			nameEnd++
		}
		name := text[nameStart:nameEnd]
		tokens = append(tokens, highlightToken{text: name, start: nameStart, end: nameEnd, kind: kind, write: write})
		if kind == "scss-function" && write {
			functionNames[name] = true
		}
		i = nameEnd - 1
	}
	return tokens, functionNames
}

func scssFunctionCallHighlightTokens(text string, functionNames map[string]bool, callableTokens []highlightToken) []highlightToken {
	definitionStarts := map[int]bool{}
	for _, token := range callableTokens {
		if token.kind == "scss-function" && token.write {
			definitionStarts[token.start] = true
		}
	}
	var tokens []highlightToken
	for i := 0; i < len(text); i++ {
		next := skipCSSIgnored(text, i)
		if next != i {
			i = next
			continue
		}
		if !isSelectorIdentifierByte(text[i]) {
			continue
		}
		start := i
		end := i + 1
		for end < len(text) && isSelectorIdentifierByte(text[end]) {
			end++
		}
		name := text[start:end]
		after := end
		for after < len(text) && isCSSSpace(text[after]) {
			after++
		}
		if functionNames[name] && after < len(text) && text[after] == '(' && !definitionStarts[start] {
			tokens = append(tokens, highlightToken{text: name, start: start, end: end, kind: "scss-function"})
		}
		i = end - 1
	}
	return tokens
}

func scssExtendReferenceHighlightTokens(text string) []highlightToken {
	var tokens []highlightToken
	lower := strings.ToLower(text)
	for i := 0; i < len(text); i++ {
		next := skipCSSIgnored(text, i)
		if next != i {
			i = next
			continue
		}
		if !strings.HasPrefix(lower[i:], "@extend") {
			continue
		}
		start := i + len("@extend")
		start = skipCSSIgnoredAndSpaces(text, start)
		if start >= len(text) || text[start] != '.' && text[start] != '%' {
			continue
		}
		end := start + 1
		for end < len(text) && isSelectorIdentifierByte(text[end]) {
			end++
		}
		if end > start+1 {
			tokens = append(tokens, highlightToken{text: text[start:end], start: start, end: end, kind: "selector"})
		}
		i = end - 1
	}
	return tokens
}

func skipCSSIgnoredAndSpaces(text string, index int) int {
	for index < len(text) {
		next := skipCSSIgnored(text, index)
		if next != index {
			index = next + 1
			continue
		}
		if isCSSSpace(text[index]) {
			index++
			continue
		}
		return index
	}
	return index
}

func previousNonSpace(text string, index int) byte {
	for index >= 0 {
		for index >= 0 && isCSSSpace(text[index]) {
			index--
		}
		if index > 0 && text[index] == '/' && text[index-1] == '*' {
			if start := strings.LastIndex(text[:index-1], "/*"); start != -1 {
				index = start - 1
				continue
			}
		}
		break
	}
	if index < 0 {
		return 0
	}
	return text[index]
}

func variableScopeAtBlocks(blocks []cssBlock, offset int) (int, int) {
	scopeStart, scopeEnd := -1, -1
	for _, block := range blocks {
		if block.headStart <= offset && offset <= block.end {
			if scopeStart == -1 || block.headStart >= scopeStart && block.end <= scopeEnd {
				scopeStart, scopeEnd = block.headStart, block.end
			}
		}
	}
	return scopeStart, scopeEnd
}

func selectorHighlightTokens(text string, block cssBlock) []highlightToken {
	var tokens []highlightToken
	for _, part := range splitSelectorParts(text, block.headStart, block.start) {
		start, end := trimRange(text, part.start, part.end)
		start = skipCSSIgnoredAndSpaces(text, start)
		if start >= end {
			continue
		}
		selector := text[start:end]
		if selector[0] == '.' || selector[0] == '#' || selector[0] == '%' {
			tokenEnd := start + 1
			for tokenEnd < end && isSelectorIdentifierByte(text[tokenEnd]) {
				tokenEnd++
			}
			tokens = append(tokens, highlightToken{text: text[start:tokenEnd], start: start, end: tokenEnd, kind: "selector", write: true})
			continue
		}
		if isSelectorIdentifierByte(selector[0]) {
			tokenEnd := start
			for tokenEnd < end && isSelectorIdentifierByte(text[tokenEnd]) {
				tokenEnd++
			}
			tokens = append(tokens, highlightToken{text: text[start:tokenEnd], start: start, end: tokenEnd, kind: "selector", write: true})
		}
	}
	return tokens
}

func valueHighlightTokens(declaration cssDeclaration) []highlightToken {
	var tokens []highlightToken
	value := declaration.value
	for i := 0; i < len(value); {
		if value[i] == '-' && i+1 < len(value) && value[i+1] == '-' || isSelectorIdentifierByte(value[i]) {
			start := i
			for i < len(value) && isSelectorIdentifierByte(value[i]) {
				i++
			}
			text := value[start:i]
			if text != "" {
				kind := "value"
				if strings.HasPrefix(text, "--") {
					kind = "variable"
				} else if declaration.lowerName == "animation" || declaration.lowerName == "animation-name" {
					kind = "keyframe-reference"
				}
				tokens = append(tokens, highlightToken{text: text, start: declaration.valueStart + start, end: declaration.valueStart + i, kind: kind})
			}
			continue
		}
		i++
	}
	return tokens
}

func keyframeNameToken(text string, block cssBlock) (highlightToken, bool) {
	headStart, headEnd := trimRange(text, block.headStart, block.start)
	fields := strings.Fields(text[headStart:headEnd])
	if len(fields) < 2 {
		return highlightToken{}, false
	}
	name := fields[1]
	nameStart := headStart + strings.Index(text[headStart:headEnd], name)
	return highlightToken{text: name, start: nameStart, end: nameStart + len(name), kind: "keyframe", write: true}, true
}

func propertyHighlightKind(name string) string {
	if strings.HasPrefix(name, "--") {
		return "variable"
	}
	return "property"
}

func highlightTokenAt(tokens []highlightToken, offset int) (highlightToken, bool) {
	for _, token := range tokens {
		if offset >= token.start && offset <= token.end {
			return token, true
		}
	}
	return highlightToken{}, false
}

func highlightTokensMatch(selected, candidate highlightToken) bool {
	if selected.kind == "keyframe" || selected.kind == "keyframe-reference" {
		return candidate.text == selected.text && (candidate.kind == "keyframe" || candidate.kind == "keyframe-reference")
	}
	if selected.kind == "less-variable" || selected.kind == "scss-variable" {
		if candidate.kind != selected.kind || candidate.text != selected.text {
			return false
		}
		if selected.scopeStart == -2 || candidate.scopeStart == -2 {
			return true
		}
		if selected.scopeStart == -1 {
			return candidate.scopeStart == -1
		}
		return candidate.scopeStart == -1 || candidate.scopeStart == selected.scopeStart && candidate.scopeEnd == selected.scopeEnd
	}
	return candidate.kind == selected.kind && candidate.text == selected.text
}
