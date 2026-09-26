package services

import (
	"sort"
	"strings"
)

type scopeSymbolKind string

const (
	scopeSymbolRule     scopeSymbolKind = "rule"
	scopeSymbolVariable scopeSymbolKind = "variable"
	scopeSymbolMixin    scopeSymbolKind = "mixin"
	scopeSymbolFunction scopeSymbolKind = "function"
	scopeSymbolKeyframe scopeSymbolKind = "keyframe"
)

type scopeSymbol struct {
	name   string
	kind   scopeSymbolKind
	offset int
}

type symbolScope struct {
	offset   int
	length   int
	parent   *symbolScope
	children []*symbolScope
	symbols  []scopeSymbol
}

func newGlobalSymbolScope() *symbolScope {
	return &symbolScope{offset: 0, length: -1}
}

func newSymbolScope(offset, length int) *symbolScope {
	return &symbolScope{offset: offset, length: length}
}

func (s *symbolScope) addChild(child *symbolScope) {
	if s == nil || child == nil {
		return
	}
	child.parent = s
	s.children = append(s.children, child)
}

func (s *symbolScope) addSymbol(name string, kind scopeSymbolKind, offsets ...int) {
	if s == nil || name == "" {
		return
	}
	offset := 1 << 30
	if len(offsets) > 0 {
		offset = offsets[0]
	}
	for _, symbol := range s.symbols {
		if symbol.name == name && symbol.kind == kind {
			return
		}
	}
	s.symbols = append(s.symbols, scopeSymbol{name: name, kind: kind, offset: offset})
}

func (s *symbolScope) findScope(offset int) *symbolScope {
	if s == nil {
		return nil
	}
	if s.parent == nil {
		if offset < 0 {
			return nil
		}
		for _, child := range s.children {
			if found := child.findScope(offset); found != nil {
				return found
			}
		}
		return s
	}
	if offset < s.offset || offset >= s.offset+s.length {
		return nil
	}
	for _, child := range s.children {
		if found := child.findScope(offset); found != nil {
			return found
		}
	}
	return s
}

func (s *symbolScope) getSymbol(name string, kind scopeSymbolKind) (scopeSymbol, bool) {
	if s == nil {
		return scopeSymbol{}, false
	}
	for _, symbol := range s.symbols {
		if symbol.name == name && symbol.kind == kind {
			return symbol, true
		}
	}
	return scopeSymbol{}, false
}

func buildSymbolScope(text, languageID string) *symbolScope {
	global := newGlobalSymbolScope()
	blocks := parseCSSBlocks(text)
	scopeByStart := map[int]*symbolScope{}
	for _, block := range blocks {
		scope := newSymbolScope(block.start, block.end-block.start+1)
		scopeByStart[block.start] = scope
		parent := global
		if parentBlock, ok := innermostParentBlock(blocks, block); ok {
			parent = scopeByStart[parentBlock.start]
		}
		parent.addChild(scope)
	}
	for _, block := range blocks {
		parent := global
		if parentScope := parentScopeForBlock(blocks, scopeByStart, block); parentScope != nil {
			parent = parentScope
		}
		scope := scopeByStart[block.start]
		addBlockSymbols(text, languageID, block, parent, scope)
	}
	addVariableSymbols(text, languageID, blocks, scopeByStart, global)
	return global
}

func innermostParentBlock(blocks []cssBlock, block cssBlock) (cssBlock, bool) {
	var parent cssBlock
	found := false
	for _, candidate := range blocks {
		if candidate.start == block.start && candidate.end == block.end {
			continue
		}
		if candidate.bodyStart <= block.headStart && block.end <= candidate.bodyEnd {
			if !found || candidate.bodyStart >= parent.bodyStart && candidate.bodyEnd <= parent.bodyEnd {
				parent = candidate
				found = true
			}
		}
	}
	return parent, found
}

func parentScopeForBlock(blocks []cssBlock, scopeByStart map[int]*symbolScope, block cssBlock) *symbolScope {
	if parent, ok := innermostParentBlock(blocks, block); ok {
		return scopeByStart[parent.start]
	}
	return nil
}

func addBlockSymbols(text, languageID string, block cssBlock, parent, scope *symbolScope) {
	head := strings.TrimSpace(block.head)
	lower := strings.ToLower(head)
	switch {
	case strings.HasPrefix(lower, "@keyframes") || strings.HasPrefix(lower, "@-") && strings.Contains(lower, "keyframes"):
		if name := atRuleName(head); name != "" {
			parent.addSymbol(name, scopeSymbolKeyframe, block.headStart)
		}
	case languageID == "less" && isLESSMixinHead(head):
		name := lessMixinName(head)
		parent.addSymbol(name, scopeSymbolMixin, block.headStart)
		addParameterSymbols(head, scope, '@', block.headStart)
	case languageID == "scss" && strings.HasPrefix(lower, "@mixin"):
		name, params := scssCallableNameAndParams(head, "@mixin")
		parent.addSymbol(name, scopeSymbolMixin, block.headStart)
		addParameterSymbols(params, scope, '$', block.headStart+strings.Index(head, params))
	case languageID == "scss" && strings.HasPrefix(lower, "@function"):
		name, params := scssCallableNameAndParams(head, "@function")
		parent.addSymbol(name, scopeSymbolFunction, block.headStart)
		addParameterSymbols(params, scope, '$', block.headStart+strings.Index(head, params))
	case languageID == "scss" && strings.HasPrefix(lower, "@each"):
		addSCSSAtRuleVariables(head, scope, "@each", block.headStart)
	case languageID == "scss" && strings.HasPrefix(lower, "@for"):
		addSCSSAtRuleVariables(head, scope, "@for", block.headStart)
	case strings.HasPrefix(lower, "@"):
		return
	case isKeyframeSelectorHead(head):
		return
	default:
		if languageID == "less" && isLESSMixinHead(head) {
			return
		}
		for _, part := range splitSelectorParts(text, block.headStart, block.start) {
			start, end := trimRange(text, part.start, part.end)
			if start < end {
				name := strings.TrimSpace(text[start:end])
				if languageID != "css" && isComplexScopeSelector(name) {
					continue
				}
				parent.addSymbol(name, scopeSymbolRule, start)
			}
		}
	}
}

func addVariableSymbols(text, languageID string, blocks []cssBlock, scopeByStart map[int]*symbolScope, global *symbolScope) {
	switch languageID {
	case "less":
		for _, token := range lessVariableHighlightTokens(text) {
			if token.write {
				scopeForOffset(blocks, scopeByStart, token.start, global).addSymbol(token.text, scopeSymbolVariable, token.start)
			}
		}
	case "scss":
		for _, token := range scssVariableHighlightTokens(text) {
			if token.write {
				scopeForOffset(blocks, scopeByStart, token.start, global).addSymbol(token.text, scopeSymbolVariable, token.start)
			}
		}
	default:
		for _, block := range blocks {
			for _, declaration := range parseDeclarations(text, block.bodyStart, block.bodyEnd) {
				if strings.HasPrefix(declaration.name, "--") {
					global.addSymbol(declaration.name, scopeSymbolVariable, declaration.nameOffset)
				}
			}
		}
	}
}

func scopeForOffset(blocks []cssBlock, scopeByStart map[int]*symbolScope, offset int, global *symbolScope) *symbolScope {
	var selected *cssBlock
	for i := range blocks {
		block := &blocks[i]
		if block.headStart <= offset && offset <= block.end {
			if selected == nil || block.headStart >= selected.headStart && block.end <= selected.end {
				selected = block
			}
		}
	}
	if selected == nil {
		return global
	}
	return scopeByStart[selected.start]
}

func atRuleName(head string) string {
	fields := strings.Fields(head)
	if len(fields) < 2 {
		return ""
	}
	return fields[1]
}

func lessMixinName(head string) string {
	head = strings.TrimSpace(head)
	if head == "" || head[0] != '.' && head[0] != '#' {
		return ""
	}
	end := 1
	for end < len(head) && isSelectorIdentifierByte(head[end]) {
		end++
	}
	return head[:end]
}

func scssCallableNameAndParams(head, keyword string) (string, string) {
	rest := strings.TrimSpace(head[len(keyword):])
	if rest == "" {
		return "<undefined>", ""
	}
	nameEnd := 0
	for nameEnd < len(rest) && isSelectorIdentifierByte(rest[nameEnd]) {
		nameEnd++
	}
	name := "<undefined>"
	if nameEnd > 0 {
		name = rest[:nameEnd]
	}
	return name, rest[nameEnd:]
}

func addParameterSymbols(text string, scope *symbolScope, prefix byte, baseOffset int) {
	for i := 0; i < len(text); i++ {
		if text[i] != prefix || i+1 >= len(text) || !isSelectorIdentifierByte(text[i+1]) {
			continue
		}
		end := i + 2
		for end < len(text) && isSelectorIdentifierByte(text[end]) {
			end++
		}
		scope.addSymbol(text[i:end], scopeSymbolVariable, baseOffset+i)
		i = end - 1
	}
}

func addSCSSAtRuleVariables(head string, scope *symbolScope, keyword string, baseOffset int) {
	rest := strings.TrimSpace(head[len(keyword):])
	restOffset := baseOffset + strings.Index(head, rest)
	limit := len(rest)
	if index := strings.Index(rest, " in "); index != -1 {
		limit = index
	}
	if keyword == "@for" {
		if index := strings.Index(rest, " from "); index != -1 {
			limit = index
		}
	}
	addParameterSymbols(rest[:limit], scope, '$', restOffset)
}

func isKeyframeSelectorHead(head string) bool {
	lower := strings.ToLower(strings.TrimSpace(head))
	return lower == "from" || lower == "to" || strings.HasSuffix(lower, "%")
}

func isComplexScopeSelector(selector string) bool {
	return strings.ContainsAny(selector, " \t\r\n\f>+~")
}

func sortedScopeSymbols(symbols []scopeSymbol) []scopeSymbol {
	result := append([]scopeSymbol(nil), symbols...)
	sort.SliceStable(result, func(i, j int) bool {
		return result[i].offset < result[j].offset
	})
	return result
}
