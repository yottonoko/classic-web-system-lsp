package parser

import (
	"strings"
	"unicode/utf8"
)

// Parser builds a lightweight syntax tree for CSS, LESS, or SCSS input.
type Parser struct {
	source []rune
	syntax syntaxMode
}

// NewParser creates a parser configured for CSS input.
func NewParser() *Parser {
	return &Parser{}
}

// NewLESSParser creates a parser configured for LESS input.
func NewLESSParser() *Parser {
	return &Parser{syntax: syntaxLESS}
}

// NewSCSSParser creates a parser configured for SCSS input.
func NewSCSSParser() *Parser {
	return &Parser{syntax: syntaxSCSS}
}

type syntaxMode int

const (
	syntaxCSS syntaxMode = iota
	syntaxLESS
	syntaxSCSS
)

// InternalParse parses input with a parser callback and attaches source text lookup to the result.
func (p *Parser) InternalParse(input string, parse func() *Node) *Node {
	p.source = []rune(input)
	node := parse()
	if node != nil {
		node.TextProvider = p.substring
	}
	return node
}

// ParseStylesheet parses a stylesheet and returns its root syntax node.
func (p *Parser) ParseStylesheet(input string) *Node {
	return p.InternalParse(input, p.parseStylesheet)
}

func (p *Parser) parseStylesheet() *Node {
	root := NewNode(0, len(p.source), NodeTypeStylesheet)
	p.parseChildrenInto(root, 0, len(p.source))
	return root
}

func (p *Parser) parseRuleset() *Node {
	return p.parseRuleLike(0, len(p.source))
}

func (p *Parser) parseLESSMixinDeclaration() *Node {
	open := p.indexRune(0, len(p.source), '{')
	if open == -1 || !isLESSMixinDeclaration(strings.TrimSpace(string(p.source[:open]))) {
		return nil
	}
	return p.parseAtRule(0, len(p.source), NodeTypeMixinDeclaration)
}

func (p *Parser) parseLESSMixinReference() *Node {
	start := p.skipWhitespace(0, len(p.source))
	end := trimRightRunes(p.source, start, len(p.source))
	if !p.isLESSMixinReference(start, end) {
		return nil
	}
	return p.parseLESSMixinReferenceAt(start, end)
}

func (p *Parser) parseLESSMixinParameter() *Node {
	start := p.skipWhitespace(0, len(p.source))
	end := trimRightRunes(p.source, start, len(p.source))
	if start >= end {
		return nil
	}
	node := NewNode(start, end-start, NodeTypeFunctionParameter)
	expr := NewNode(start, end-start, NodeTypeExpression)
	p.parseValueTermsInto(expr, start, end)
	if expr.HasChildren() {
		node.AddChild(expr)
	}
	if variableEnd := p.scanVariableName(start, end); variableEnd > start {
		node.AddChild(NewNode(start, variableEnd-start, NodeTypeVariableName))
	}
	return node
}

func (p *Parser) parseExpression() *Node {
	start := p.skipWhitespace(0, len(p.source))
	end := trimRightRunes(p.source, start, len(p.source))
	if start >= end {
		return nil
	}
	expr := NewNode(start, end-start, NodeTypeExpression)
	p.parseValueTermsInto(expr, start, end)
	if !expr.HasChildren() {
		return nil
	}
	return expr
}

func (p *Parser) parseTerm() *Node {
	start := p.skipWhitespace(0, len(p.source))
	end := trimRightRunes(p.source, start, len(p.source))
	if start >= end {
		return nil
	}
	if term, next, ok := p.parseValueTermAt(start, end); ok && next == end {
		return term
	}
	return nil
}

func (p *Parser) parseOperator() *Node {
	start := p.skipWhitespace(0, len(p.source))
	end := trimRightRunes(p.source, start, len(p.source))
	if start >= end {
		return nil
	}
	if length := scanOperator(p.source[start:end]); length == end-start {
		return NewNode(start, length, NodeTypeOperator)
	}
	return nil
}

func (p *Parser) parsePrio() *Node {
	start := p.skipWhitespaceAndComments(0, len(p.source))
	end := trimRightRunes(p.source, start, len(p.source))
	if start >= end || p.source[start] != '!' {
		return nil
	}
	cursor := p.skipWhitespaceAndComments(start+1, end)
	wordEnd := scanIdentifier(p.source, cursor, end)
	if wordEnd != end || !strings.EqualFold(string(p.source[cursor:wordEnd]), "important") {
		return nil
	}
	return NewNode(start, end-start, NodeTypePrio)
}

func (p *Parser) parseURILiteral() *Node {
	start := p.skipWhitespace(0, len(p.source))
	end := trimRightRunes(p.source, start, len(p.source))
	if term, next, ok := p.parseURLTerm(start, end); ok && next == end && term.HasChildren() {
		return term.GetChild(0)
	}
	return nil
}

func (p *Parser) parseVariable() *Node {
	start := p.skipWhitespace(0, len(p.source))
	end := p.scanVariableName(start, len(p.source))
	if end == start {
		return nil
	}
	return NewNode(start, end-start, NodeTypeVariableName)
}

func (p *Parser) parseKeyframe() *Node {
	return p.parseAtRule(0, len(p.source), NodeTypeKeyframe)
}

func (p *Parser) parseFontFace() *Node {
	return p.parseAtRule(0, len(p.source), NodeTypeFontFace)
}

func (p *Parser) parseStartingStyleAtRule() *Node {
	return p.parseAtRule(0, len(p.source), NodeTypeStartingStyleAtRule)
}

func (p *Parser) parseChildrenInto(parent *Node, start, end int) {
	for start < end {
		start = p.skipWhitespace(start, end)
		if start >= end {
			return
		}
		if colon := p.variableDeclarationColon(start, end); colon != -1 {
			next := p.variableDeclarationEnd(start, end)
			parent.AddChild(p.parseVariableDeclaration(start, next))
			start = next
			continue
		}
		if p.source[start] == '@' {
			semi := p.findTopLevelSemicolon(start, end)
			open := p.indexRune(start, end, '{')
			if semi != -1 && (open == -1 || semi < open) {
				parent.AddChild(p.parseAtStatement(start, semi+1))
				start = semi + 1
				continue
			}
		}
		if p.syntax == syntaxLESS {
			semi := p.findTopLevelSemicolon(start, end)
			open := p.indexRune(start, end, '{')
			if semi != -1 && (open == -1 || semi < open) && p.isLESSMixinReference(start, semi) {
				parent.AddChild(p.parseLESSMixinReferenceAt(start, semi))
				start = semi + 1
				continue
			}
			if semi != -1 && (open == -1 || semi < open) {
				if function := p.parseFunctionStatement(start, semi); function != nil {
					parent.AddChild(function)
					start = semi + 1
					continue
				}
			}
		}
		close := p.findTopLevelBrace(start, end)
		if close == -1 {
			if p.source[start] == '@' {
				parent.AddChild(p.parseAtStatement(start, end))
			}
			return
		}
		open := p.indexRune(start, close, '{')
		if open == -1 {
			return
		}
		head := strings.TrimSpace(string(p.source[start:open]))
		var child *Node
		if typ := p.blockAtRuleType(head); typ != NodeTypeUndefined {
			child = p.parseAtRule(start, close+1, typ)
		} else {
			child = p.parseRuleLike(start, close+1)
		}
		parent.AddChild(child)
		start = close + 1
	}
}

func (p *Parser) parseAtStatement(start, end int) *Node {
	if colon := p.indexRune(start, end, ':'); colon != -1 {
		return p.parseVariableDeclaration(start, end)
	}
	head := strings.TrimSpace(string(p.source[start:end]))
	return NewNode(start, end-start, p.statementAtRuleType(head))
}

func (p *Parser) parseVariableDeclaration(start, end int) *Node {
	colon := p.indexRune(start, end, ':')
	if colon == -1 {
		return NewNode(start, end-start, NodeTypeVariableDeclaration)
	}
	node := NewNode(start, end-start, NodeTypeVariableDeclaration)
	nameStart := p.skipWhitespace(start, colon)
	nameEnd := trimRightRunes(p.source, nameStart, colon)
	node.AddChild(NewNode(nameStart, nameEnd-nameStart, NodeTypeVariableName))
	valueStart := p.skipWhitespace(colon+1, end)
	valueEnd := end
	if valueEnd > valueStart && p.source[valueEnd-1] == ';' {
		valueEnd--
	}
	valueEnd = trimRightRunes(p.source, valueStart, valueEnd)
	if valueStart < valueEnd {
		expr := NewNode(valueStart, valueEnd-valueStart, NodeTypeExpression)
		p.parseValueTermsInto(expr, valueStart, valueEnd)
		if !expr.HasChildren() {
			expr.AddChild(NewNode(valueStart, valueEnd-valueStart, NodeTypeTerm))
		}
		node.AddChild(expr)
	}
	return node
}

func (p *Parser) parseRuleLike(start, end int) *Node {
	open := p.indexBlockBrace(start, end)
	if open == -1 {
		return nil
	}
	close := p.matchingBrace(open, end)
	if close == -1 {
		close = end - 1
	}
	node := NewNode(start, close-start+1, NodeTypeRuleset)
	selectorList := NewNode(-1, -1, NodeTypeUndefined)
	selectorList.AddChild(p.parseSelector(start, open))
	node.AddChild(selectorList)
	decls := NewNode(open+1, max(0, close-open-1), NodeTypeDeclarations)
	p.parseDeclarationsInto(decls, open+1, close)
	node.AddChild(decls)
	return node
}

func (p *Parser) parseAtRule(start, end int, typ NodeType) *Node {
	if typ == NodeTypeUnknownAtRule {
		return NewNode(start, p.atKeywordEnd(start, end)-start, typ)
	}
	open := p.indexRune(start, end, '{')
	if open == -1 {
		return NewNode(start, end-start, typ)
	}
	close := p.matchingBrace(open, end)
	if close == -1 {
		close = end - 1
	}
	node := NewNode(start, close-start+1, typ)
	head := strings.TrimSpace(string(p.source[start:open]))
	switch typ {
	case NodeTypeKeyframe:
		parts := strings.Fields(head)
		if len(parts) > 1 {
			node.AddChild(NewNode(start+runeIndex(string(p.source[start:open]), parts[1]), len([]rune(parts[1])), NodeTypeIdentifier))
		}
		selectorList := NewNode(-1, -1, NodeTypeUndefined)
		p.parseKeyframeSelectorsInto(selectorList, open+1, close)
		node.AddChild(selectorList)
	case NodeTypeMedia:
		queryList := NewNode(-1, -1, NodeTypeUndefined)
		queryList.AddChild(NewNode(start+len("@media "), max(0, open-start-len("@media ")), NodeTypeMediaQuery))
		node.AddChild(queryList)
		decls := NewNode(open+1, max(0, close-open-1), NodeTypeDeclarations)
		p.parseDeclarationsInto(decls, open+1, close)
		node.AddChild(decls)
	case NodeTypeSupports:
		node.AddChild(NewNode(start+len("@supports "), max(0, open-start-len("@supports ")), NodeTypeSupportsCondition))
		decls := NewNode(open+1, max(0, close-open-1), NodeTypeDeclarations)
		p.parseDeclarationsInto(decls, open+1, close)
		node.AddChild(decls)
	case NodeTypeLayer:
		wrapper := NewNode(-1, -1, NodeTypeUndefined)
		decls := NewNode(open+1, max(0, close-open-1), NodeTypeDeclarations)
		p.parseDeclarationsInto(decls, open+1, close)
		wrapper.AddChild(decls)
		node.AddChild(wrapper)
	default:
		decls := NewNode(open+1, max(0, close-open-1), NodeTypeDeclarations)
		p.parseDeclarationsInto(decls, open+1, close)
		node.AddChild(decls)
	}
	return node
}

func (p *Parser) atKeywordEnd(start, end int) int {
	if start >= end || p.source[start] != '@' {
		return start
	}
	cursor := start + 1
	for cursor < end && isIdentifierPart(p.source[cursor]) {
		cursor++
	}
	return cursor
}

func (p *Parser) parseKeyframeSelectorsInto(parent *Node, start, end int) {
	for start < end {
		start = p.skipWhitespace(start, end)
		open := p.indexRune(start, end, '{')
		if open == -1 {
			return
		}
		close := p.matchingBrace(open, end)
		if close == -1 {
			return
		}
		selector := NewNode(start, close-start+1, NodeTypeKeyframeSelector)
		decls := NewNode(open+1, max(0, close-open-1), NodeTypeDeclarations)
		p.parseDeclarationsInto(decls, open+1, close)
		selector.AddChild(decls)
		parent.AddChild(selector)
		start = close + 1
	}
}

func (p *Parser) parseSelector(start, end int) *Node {
	selector := NewNode(start, end-start, NodeTypeSelector)
	simple := NewNode(start, end-start, NodeTypeSimpleSelector)
	text := strings.TrimSpace(string(p.source[start:end]))
	trimStart := start + leadingWhitespace(p.source[start:end])
	switch {
	case strings.HasPrefix(text, "."):
		simple.AddChild(NewNode(trimStart, len([]rune(firstSelectorPart(text))), NodeTypeClassSelector))
	case strings.HasPrefix(text, ":"):
		simple.AddChild(NewNode(trimStart, len([]rune(firstSelectorPart(text))), NodeTypePseudoSelector))
	case strings.HasPrefix(text, "["):
		simple.AddChild(NewNode(trimStart, len([]rune(firstSelectorPart(text))), NodeTypeAttributeSelector))
	case p.syntax == syntaxSCSS && strings.HasPrefix(text, "%"):
		simple.AddChild(NewNode(trimStart, len([]rune(firstSelectorPart(text))), NodeTypeSelectorPlaceholder))
	case strings.HasPrefix(text, "&"):
		simple.AddChild(NewNode(trimStart, 1, NodeTypeSelectorCombinator))
		simple.AddChild(NewNode(trimStart, 1, NodeTypeSelectorCombinatorParent))
		if rest := strings.TrimSpace(text[1:]); rest != "" {
			restOffset := trimStart + 1 + runeIndex(string(p.source[trimStart+1:end]), rest)
			elem := NewNode(restOffset, len([]rune(firstSelectorPart(rest))), NodeTypeElementNameSelector)
			elem.AddChild(NewNode(restOffset, elem.Length, NodeTypeIdentifier))
			wrapper := NewNode(-1, -1, NodeTypeUndefined)
			wrapper.AddChild(elem)
			simple.AddChild(wrapper)
		}
	default:
		part := firstSelectorPart(text)
		elem := NewNode(trimStart, len([]rune(part)), NodeTypeElementNameSelector)
		elem.AddChild(NewNode(trimStart, len([]rune(part)), NodeTypeIdentifier))
		simple.AddChild(elem)
	}
	p.addInterpolations(simple, start, end, NodeTypeSelectorInterpolation)
	p.addExtendsReferences(simple, start, end)
	selector.AddChild(simple)
	return selector
}

func (p *Parser) parseDeclarationsInto(parent *Node, start, end int) {
	cursor := start
	for cursor < end {
		cursor = p.skipWhitespace(cursor, end)
		if cursor >= end {
			return
		}
		open := p.indexBlockBrace(cursor, end)
		semi := p.indexRune(cursor, end, ';')
		colon := p.indexRune(cursor, end, ':')
		if semi != -1 && (colon == -1 || semi < colon) {
			if p.source[cursor] == '@' {
				statement := p.parseAtStatement(cursor, semi+1)
				if statement.Type() != NodeTypeUnknownAtRule {
					parent.AddChild(statement)
					cursor = semi + 1
					continue
				}
			}
			if p.syntax == syntaxLESS && p.isLESSMixinReference(cursor, semi) {
				parent.AddChild(p.parseLESSMixinReferenceAt(cursor, semi))
				cursor = semi + 1
				continue
			}
		}
		if p.syntax == syntaxLESS && semi != -1 {
			if extend := p.parseExtendsReferenceStatement(cursor, semi); extend != nil {
				parent.AddChild(extend)
				cursor = semi + 1
				continue
			}
		}
		if open != -1 && (semi == -1 || open < semi) {
			close := p.matchingBrace(open, end)
			if close == -1 {
				return
			}
			parent.AddChild(p.parseNested(cursor, close+1))
			cursor = close + 1
			continue
		}
		if colon == -1 {
			if p.syntax == syntaxLESS && semi != -1 {
				parent.AddChild(p.parsePropertyStatement(cursor, semi))
				cursor = semi + 1
				continue
			}
			return
		}
		stmtEnd := semi
		if stmtEnd == -1 || stmtEnd > end {
			stmtEnd = end
		}
		if (p.source[cursor] == '@' || p.source[cursor] == '$') && p.variableDeclarationColon(cursor, stmtEnd) != -1 {
			parent.AddChild(p.parseVariableDeclaration(cursor, stmtEnd))
			cursor = stmtEnd + 1
			continue
		}
		parent.AddChild(p.parseDeclaration(cursor, stmtEnd))
		cursor = stmtEnd + 1
	}
}

func (p *Parser) parseNested(start, end int) *Node {
	// indexRune does not skip quoted text, so a quoted parenthesis can hide the
	// brace that indexBlockBrace found for the caller.
	open := p.indexRune(start, end, '{')
	if open == -1 {
		return p.parseRuleLike(start, end)
	}
	head := strings.TrimSpace(string(p.source[start:open]))
	if typ := p.blockAtRuleType(head); typ != NodeTypeUndefined {
		return p.parseAtRule(start, end, typ)
	}
	return p.parseRuleLike(start, end)
}

func (p *Parser) parseDeclaration(start, end int) *Node {
	colon := p.indexRune(start, end, ':')
	decl := NewNode(start, end-start, NodeTypeDeclaration)
	if colon == -1 {
		// The statement ends before the colon the caller saw, so it is a bare
		// property without a value.
		colon = end
	}
	propStart := p.skipWhitespace(start, colon)
	propEnd := trimRightRunes(p.source, propStart, colon)
	property := NewNode(propStart, propEnd-propStart, NodeTypeProperty)
	property.AddChild(NewNode(propStart, propEnd-propStart, NodeTypeIdentifier))
	p.addInterpolations(property, propStart, propEnd, NodeTypeInterpolation)
	decl.AddChild(property)
	valueStart := p.skipWhitespace(min(colon+1, end), end)
	if valueStart < end {
		expr := NewNode(valueStart, end-valueStart, NodeTypeExpression)
		if idx := indexUnicodeRangePrefix(p.source[valueStart:end]); idx >= 0 {
			bin := NewNode(valueStart, end-valueStart, NodeTypeBinaryExpression)
			term := NewNode(valueStart, end-valueStart, NodeTypeTerm)
			term.AddChild(NewNode(valueStart+idx, lenUntilDelimiter(p.source[valueStart+idx:end]), NodeTypeUnicodeRange))
			bin.AddChild(term)
			expr.AddChild(bin)
		} else {
			p.parseValueTermsInto(expr, valueStart, end)
			if !expr.HasChildren() {
				expr.AddChild(NewNode(valueStart, end-valueStart, NodeTypeTerm))
			}
		}
		decl.AddChild(expr)
		if prioStart, prioEnd := p.priorityRange(valueStart, end); prioStart != -1 {
			decl.AddChild(NewNode(prioStart, prioEnd-prioStart, NodeTypePrio))
		}
	}
	return decl
}

func (p *Parser) parsePropertyStatement(start, end int) *Node {
	propStart := p.skipWhitespace(start, end)
	propEnd := trimRightRunes(p.source, propStart, end)
	property := NewNode(propStart, propEnd-propStart, NodeTypeProperty)
	property.AddChild(NewNode(propStart, propEnd-propStart, NodeTypeIdentifier))
	return property
}

func (p *Parser) parseLESSMixinReferenceAt(start, end int) *Node {
	refStart := p.skipWhitespace(start, end)
	refEnd := trimRightRunes(p.source, refStart, end)
	node := NewNode(refStart, refEnd-refStart, NodeTypeMixinReference)
	if open := p.indexRune(refStart, refEnd, '('); open != -1 {
		if close := p.matchingParen(open, refEnd); close != -1 {
			args := NewNode(open+1, max(0, close-open-1), NodeTypeUndefined)
			p.parseValueTermsInto(args, open+1, close)
			node.AddChild(args)
		}
	}
	return node
}

func (p *Parser) parseFunctionStatement(start, end int) *Node {
	start = p.skipWhitespace(start, end)
	end = trimRightRunes(p.source, start, end)
	if start >= end || !isIdentifierStart(p.source[start]) {
		return nil
	}
	nameEnd := scanIdentifier(p.source, start, end)
	if nameEnd >= end || p.source[nameEnd] != '(' {
		return nil
	}
	close := p.matchingParen(nameEnd, end)
	if close != end-1 {
		return nil
	}
	function := NewNode(start, end-start, NodeTypeFunction)
	p.addNumericChildren(function, nameEnd+1, close)
	return function
}

func (p *Parser) parseExtendsReferenceStatement(start, end int) *Node {
	start = p.skipWhitespace(start, end)
	end = trimRightRunes(p.source, start, end)
	if start >= end {
		return nil
	}
	text := strings.ToLower(string(p.source[start:end]))
	index := strings.Index(text, ":extend(")
	if index == -1 {
		return nil
	}
	refStart := start + index
	open := refStart + len(":extend")
	close := p.matchingParen(open, end)
	if close == -1 {
		return nil
	}
	return NewNode(refStart, close-refStart+1, NodeTypeExtendsReference)
}

func (p *Parser) parseValueTermsInto(expr *Node, start, end int) {
	for cursor := start; cursor < end; {
		cursor = p.skipWhitespace(cursor, end)
		if cursor >= end {
			return
		}
		if term, next, ok := p.parseValueTermAt(cursor, end); ok {
			expr.AddChild(term)
			cursor = next
			continue
		}
		if length := scanOperator(p.source[cursor:end]); length > 0 {
			expr.AddChild(NewNode(cursor, length, NodeTypeOperator))
			cursor += length
			continue
		}
		cursor++
	}
}

func (p *Parser) parseValueTermAt(cursor, end int) (*Node, int, bool) {
	switch p.source[cursor] {
	case '\'', '"':
		if close := p.quotedEnd(cursor, end); close != -1 {
			return valueTermWithChild(cursor, close-cursor+1, NodeTypeStringLiteral), close + 1, true
		}
	case '#':
		if length := scanHexColor(p.source[cursor:end]); length > 1 {
			return valueTermWithChild(cursor, length, NodeTypeHexColorValue), cursor + length, true
		}
	case '~':
		if p.syntax == syntaxLESS {
			if term, next, ok := p.parseEscapedValueTerm(cursor, end); ok {
				return term, next, true
			}
		}
	case '%':
		if p.syntax == syntaxLESS {
			if term, next, ok := p.parsePercentFunctionTerm(cursor, end); ok {
				return term, next, true
			}
		}
	}
	if unicodeEnd := scanUnicodeRange(p.source, cursor, end); unicodeEnd > cursor {
		return valueTermWithChild(cursor, unicodeEnd-cursor, NodeTypeUnicodeRange), unicodeEnd, true
	}
	if isNumberStart(p.source, cursor, end) {
		length := scanNumericValue(p.source[cursor:end])
		return valueTermWithChild(cursor, length, NodeTypeNumericValue), cursor + length, true
	}
	if term, next, ok := p.parseModuleMemberTerm(cursor, end); ok {
		return term, next, true
	}
	if variableEnd := p.scanVariableName(cursor, end); variableEnd > cursor {
		return valueTermWithChild(cursor, variableEnd-cursor, NodeTypeVariableName), variableEnd, true
	}
	if term, next, ok := p.parseURLTerm(cursor, end); ok {
		return term, next, true
	}
	if isIdentifierStart(p.source[cursor]) {
		if looksLikeUnicodeRangeStart(p.source, cursor, end) {
			return nil, cursor, false
		}
		if operatorLength := scanOperator(p.source[cursor:end]); operatorLength > 0 {
			nameEnd := scanIdentifier(p.source, cursor, end)
			if nameEnd == cursor+operatorLength {
				return nil, cursor, false
			}
		}
		nameEnd := scanIdentifier(p.source, cursor, end)
		if nameEnd < end && p.source[nameEnd] == '(' {
			if close := p.matchingParen(nameEnd, end); close != -1 {
				term := NewNode(cursor, close-cursor+1, NodeTypeTerm)
				function := NewNode(cursor, close-cursor+1, NodeTypeFunction)
				p.addNumericChildren(function, nameEnd+1, close)
				term.AddChild(function)
				return term, close + 1, true
			}
		}
		return valueTermWithChild(cursor, nameEnd-cursor, NodeTypeIdentifier), nameEnd, true
	}
	return nil, cursor, false
}

func scanUnicodeRange(runes []rune, start, end int) int {
	if start+2 >= end || (runes[start] != 'U' && runes[start] != 'u') || runes[start+1] != '+' {
		return start
	}
	cursor := start + 2
	for cursor < end && (isHexRune(runes[cursor]) || runes[cursor] == '?') {
		cursor++
	}
	if cursor == start+2 {
		return start
	}
	if cursor < end && runes[cursor] == '-' {
		cursor++
		rangeStart := cursor
		for cursor < end && isHexRune(runes[cursor]) {
			cursor++
		}
		if cursor == rangeStart {
			return start
		}
	}
	return cursor
}

func looksLikeUnicodeRangeStart(runes []rune, start, end int) bool {
	return start+1 < end && (runes[start] == 'U' || runes[start] == 'u') && runes[start+1] == '+'
}

func isHexRune(r rune) bool {
	return r >= '0' && r <= '9' || r >= 'a' && r <= 'f' || r >= 'A' && r <= 'F'
}

func (p *Parser) skipWhitespaceAndComments(start, end int) int {
	for start < end {
		next := p.skipWhitespace(start, end)
		if next != start {
			start = next
			continue
		}
		if start+1 < end && p.source[start] == '/' && p.source[start+1] == '*' {
			start += 2
			for start+1 < end && !(p.source[start] == '*' && p.source[start+1] == '/') {
				start++
			}
			if start+1 < end {
				start += 2
			}
			continue
		}
		return start
	}
	return start
}

func (p *Parser) parseURLTerm(cursor, end int) (*Node, int, bool) {
	if !isIdentifierStart(p.source[cursor]) {
		return nil, cursor, false
	}
	nameEnd := scanIdentifier(p.source, cursor, end)
	if !strings.EqualFold(string(p.source[cursor:nameEnd]), "url") || nameEnd >= end || p.source[nameEnd] != '(' {
		return nil, cursor, false
	}
	close := p.matchingParen(nameEnd, end)
	if close == -1 {
		return nil, cursor, false
	}
	term := NewNode(cursor, close-cursor+1, NodeTypeTerm)
	term.AddChild(NewNode(cursor, close-cursor+1, NodeTypeURILiteral))
	return term, close + 1, true
}

func (p *Parser) parseEscapedValueTerm(cursor, end int) (*Node, int, bool) {
	if cursor+1 >= end {
		return nil, cursor, false
	}
	quote := p.source[cursor+1]
	if quote != '\'' && quote != '"' && quote != '`' {
		return nil, cursor, false
	}
	close := p.quotedEndWithQuote(cursor+1, end, quote)
	if close == -1 {
		return nil, cursor, false
	}
	return valueTermWithChild(cursor, close-cursor+1, NodeTypeEscapedValue), close + 1, true
}

func (p *Parser) parsePercentFunctionTerm(cursor, end int) (*Node, int, bool) {
	if cursor+1 >= end || p.source[cursor+1] != '(' {
		return nil, cursor, false
	}
	close := p.matchingParen(cursor+1, end)
	if close == -1 {
		return nil, cursor, false
	}
	return valueTermWithChild(cursor, close-cursor+1, NodeTypeFunction), close + 1, true
}

func (p *Parser) parseModuleMemberTerm(start, end int) (*Node, int, bool) {
	if !isIdentifierStart(p.source[start]) {
		return nil, start, false
	}
	moduleNameEnd := scanIdentifier(p.source, start, end)
	if moduleNameEnd >= end || p.source[moduleNameEnd] != '.' || moduleNameEnd+1 >= end {
		return nil, start, false
	}
	memberStart := moduleNameEnd + 1
	memberEnd := memberStart
	module := NewNode(start, 0, NodeTypeModule)
	module.AddChild(NewNode(start, moduleNameEnd-start, NodeTypeIdentifier))
	if p.source[memberStart] == '$' {
		memberEnd = memberStart + 1
		if memberEnd >= end || !isIdentifierStart(p.source[memberEnd]) {
			return nil, start, false
		}
		memberEnd = scanIdentifier(p.source, memberEnd, end)
		module.Length = memberEnd - start
		module.AddChild(NewNode(memberStart, memberEnd-memberStart, NodeTypeVariableName))
		term := NewNode(start, memberEnd-start, NodeTypeTerm)
		term.AddChild(module)
		return term, memberEnd, true
	}
	if !isIdentifierStart(p.source[memberStart]) {
		return nil, start, false
	}
	memberEnd = scanIdentifier(p.source, memberStart, end)
	if memberEnd < end && p.source[memberEnd] == '(' {
		close := p.matchingParen(memberEnd, end)
		if close == -1 {
			return nil, start, false
		}
		module.Length = close - start + 1
		function := NewNode(memberStart, close-memberStart+1, NodeTypeFunction)
		p.addNumericChildren(function, memberEnd+1, close)
		module.AddChild(function)
		term := NewNode(start, close-start+1, NodeTypeTerm)
		term.AddChild(module)
		return term, close + 1, true
	}
	module.Length = memberEnd - start
	module.AddChild(NewNode(memberStart, memberEnd-memberStart, NodeTypeIdentifier))
	term := NewNode(start, memberEnd-start, NodeTypeTerm)
	term.AddChild(module)
	return term, memberEnd, true
}

func (p *Parser) priorityRange(start, end int) (int, int) {
	for cursor := start; cursor < end; cursor++ {
		if p.source[cursor] != '!' {
			continue
		}
		nameStart := p.skipWhitespace(cursor+1, end)
		if nameStart >= end || !isIdentifierStart(p.source[nameStart]) {
			continue
		}
		nameEnd := scanIdentifier(p.source, nameStart, end)
		if strings.EqualFold(string(p.source[nameStart:nameEnd]), "important") {
			return cursor, nameEnd
		}
	}
	return -1, -1
}

func (p *Parser) variableDeclarationColon(start, end int) int {
	if p.source[start] == '@' && p.isKnownAtRuleHead(start, end) {
		return -1
	}
	varEnd := p.scanVariableName(start, end)
	if varEnd == start {
		return -1
	}
	colon := p.skipWhitespace(varEnd, end)
	if colon < end && p.source[colon] == ':' {
		return colon
	}
	return -1
}

func (p *Parser) isKnownAtRuleHead(start, end int) bool {
	headEnd := start
	for headEnd < end {
		switch p.source[headEnd] {
		case ' ', '\t', '\n', '\r', '\f', ':', '(', '{', ';':
			goto done
		default:
			headEnd++
		}
	}
done:
	head := string(p.source[start:headEnd])
	if typ := p.blockAtRuleType(head); typ != NodeTypeUndefined && typ != NodeTypeUnknownAtRule {
		return true
	}
	if typ := p.statementAtRuleType(head); typ != NodeTypeUndefined && typ != NodeTypeUnknownAtRule {
		return true
	}
	return false
}

func (p *Parser) variableDeclarationEnd(start, end int) int {
	if semi := p.findTopLevelSemicolon(start, end); semi != -1 {
		return semi + 1
	}
	if open := p.indexRune(start, end, '{'); open != -1 {
		if close := p.matchingBrace(open, end); close != -1 {
			next := p.skipWhitespace(close+1, end)
			if next < end && p.source[next] == ';' {
				return next + 1
			}
			if lineEnd := p.lineEndAfter(close+1, end); lineEnd != -1 {
				return lineEnd
			}
			return close + 1
		}
	}
	if lineEnd := p.lineEndAfter(start, end); lineEnd != -1 {
		return lineEnd
	}
	return end
}

func (p *Parser) lineEndAfter(start, end int) int {
	for i := start; i < end; i++ {
		if p.source[i] == '\n' || p.source[i] == '\r' {
			return i + 1
		}
	}
	return -1
}

func (p *Parser) isLESSMixinReference(start, end int) bool {
	start = p.skipWhitespace(start, end)
	end = trimRightRunes(p.source, start, end)
	if start >= end {
		return false
	}
	if p.source[start] != '.' && p.source[start] != '#' && p.source[start] != '@' {
		return false
	}
	if p.source[start] == '@' {
		headEnd := start
		for headEnd < end && !isWhitespace(p.source[headEnd]) && p.source[headEnd] != '(' {
			headEnd++
		}
		if statementAtRuleType(string(p.source[start:headEnd])) != NodeTypeUnknownAtRule {
			return false
		}
	}
	return true
}

func (p *Parser) scanVariableName(start, end int) int {
	if start >= end || p.source[start] != '@' && p.source[start] != '$' {
		return start
	}
	cursor := start
	for cursor < end && (p.source[cursor] == '@' || p.source[cursor] == '$') {
		cursor++
	}
	if cursor < end && p.source[cursor] == '-' {
		cursor++
	}
	nameStart := cursor
	for cursor < end && isVariableNamePart(p.source[cursor]) {
		cursor++
	}
	if cursor == nameStart {
		return start
	}
	for cursor < end && p.source[cursor] == '[' {
		close := p.matchingBracket(cursor, end)
		if close == -1 {
			break
		}
		cursor = close + 1
	}
	return cursor
}

func (p *Parser) substring(offset, length int) string {
	if offset < 0 {
		offset = 0
	}
	end := offset + length
	if end > len(p.source) {
		end = len(p.source)
	}
	if offset > end {
		offset = end
	}
	return string(p.source[offset:end])
}

func (p *Parser) skipWhitespace(start, end int) int {
	for start < end {
		switch p.source[start] {
		case ' ', '\t', '\n', '\r', '\f':
			start++
		default:
			return start
		}
	}
	return start
}

func (p *Parser) indexRune(start, end int, ch rune) int {
	depthParen := 0
	depthBracket := 0
	for i := start; i < end; i++ {
		switch p.source[i] {
		case '(':
			depthParen++
		case ')':
			if depthParen > 0 {
				depthParen--
			}
		case '[':
			depthBracket++
		case ']':
			if depthBracket > 0 {
				depthBracket--
			}
		default:
			if p.source[i] == ch && depthParen == 0 && depthBracket == 0 {
				return i
			}
		}
	}
	return -1
}

func (p *Parser) findTopLevelBrace(start, end int) int {
	open := p.indexBlockBrace(start, end)
	if open == -1 {
		return -1
	}
	return p.matchingBrace(open, end)
}

func (p *Parser) findTopLevelSemicolon(start, end int) int {
	depthParen := 0
	depthBracket := 0
	for i := start; i < end; i++ {
		switch p.source[i] {
		case '\'', '"':
			quote := p.source[i]
			for i++; i < end; i++ {
				if p.source[i] == '\\' {
					i++
					continue
				}
				if p.source[i] == quote {
					break
				}
			}
		case '(':
			depthParen++
		case ')':
			if depthParen > 0 {
				depthParen--
			}
		case '[':
			depthBracket++
		case ']':
			if depthBracket > 0 {
				depthBracket--
			}
		case ';':
			if depthParen == 0 && depthBracket == 0 {
				return i
			}
		case '{':
			if depthParen == 0 && depthBracket == 0 {
				return -1
			}
		}
	}
	return -1
}

func (p *Parser) matchingBrace(open, end int) int {
	depth := 0
	for i := open; i < end; i++ {
		switch p.source[i] {
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

func (p *Parser) matchingParen(open, end int) int {
	if open < 0 || open >= end || p.source[open] != '(' {
		return -1
	}
	depth := 0
	for i := open; i < end; i++ {
		switch p.source[i] {
		case '\'', '"':
			if close := p.quotedEnd(i, end); close != -1 {
				i = close
			}
		case '(':
			depth++
		case ')':
			depth--
			if depth == 0 {
				return i
			}
		}
	}
	return -1
}

func (p *Parser) matchingBracket(open, end int) int {
	if open < 0 || open >= end || p.source[open] != '[' {
		return -1
	}
	depth := 0
	for i := open; i < end; i++ {
		switch p.source[i] {
		case '\'', '"':
			if close := p.quotedEnd(i, end); close != -1 {
				i = close
			}
		case '[':
			depth++
		case ']':
			depth--
			if depth == 0 {
				return i
			}
		}
	}
	return -1
}

func (p *Parser) indexBlockBrace(start, end int) int {
	depthParen := 0
	depthBracket := 0
	for i := start; i < end; i++ {
		switch p.source[i] {
		case '\'', '"':
			if close := p.quotedEnd(i, end); close != -1 {
				i = close
			}
		case '(':
			depthParen++
		case ')':
			if depthParen > 0 {
				depthParen--
			}
		case '[':
			depthBracket++
		case ']':
			if depthBracket > 0 {
				depthBracket--
			}
		case '@', '$':
			if i+1 < end && p.source[i+1] == '{' && depthParen == 0 && depthBracket == 0 {
				if close := p.matchingBrace(i+1, end); close != -1 {
					i = close
				}
			}
		case '{':
			if depthParen == 0 && depthBracket == 0 {
				return i
			}
		}
	}
	return -1
}

func (p *Parser) quotedEnd(start, end int) int {
	if start >= end || p.source[start] != '\'' && p.source[start] != '"' {
		return -1
	}
	return p.quotedEndWithQuote(start, end, p.source[start])
}

func (p *Parser) quotedEndWithQuote(start, end int, quote rune) int {
	if start >= end || p.source[start] != quote {
		return -1
	}
	for i := start + 1; i < end; i++ {
		if p.source[i] == '\\' {
			i++
			continue
		}
		if p.source[i] == quote {
			return i
		}
	}
	return -1
}

func (p *Parser) addInterpolations(parent *Node, start, end int, nodeType NodeType) {
	for cursor := start; cursor < end-1; cursor++ {
		isVariableInterpolation := (p.source[cursor] == '@' || p.source[cursor] == '$') && p.source[cursor+1] == '{'
		isSCSSInterpolation := p.syntax == syntaxSCSS && p.source[cursor] == '#' && p.source[cursor+1] == '{'
		if !isVariableInterpolation && !isSCSSInterpolation {
			continue
		}
		close := p.matchingBrace(cursor+1, end)
		if close == -1 {
			continue
		}
		parent.AddChild(NewNode(cursor, close-cursor+1, nodeType))
		cursor = close
	}
}

func (p *Parser) blockAtRuleType(head string) NodeType {
	typ := blockAtRuleType(head)
	if typ != NodeTypeUnknownAtRule {
		return typ
	}
	if p.syntax != syntaxSCSS {
		return typ
	}
	lower := strings.ToLower(strings.TrimSpace(head))
	switch {
	case strings.HasPrefix(lower, "@if"):
		return NodeTypeIf
	case strings.HasPrefix(lower, "@for"):
		return NodeTypeFor
	case strings.HasPrefix(lower, "@each"):
		return NodeTypeEach
	case strings.HasPrefix(lower, "@while"):
		return NodeTypeWhile
	case strings.HasPrefix(lower, "@include"):
		return NodeTypeMixinReference
	case strings.HasPrefix(lower, "@content"):
		return NodeTypeMixinContentDeclaration
	case strings.HasPrefix(lower, "@at-root"):
		return NodeTypeSelectorPlaceholder
	case strings.HasPrefix(lower, "@debug") || strings.HasPrefix(lower, "@warn") || strings.HasPrefix(lower, "@error"):
		return NodeTypeDebug
	default:
		return typ
	}
}

func (p *Parser) statementAtRuleType(head string) NodeType {
	typ := statementAtRuleType(head)
	if typ != NodeTypeUnknownAtRule {
		return typ
	}
	if p.syntax != syntaxSCSS {
		return typ
	}
	lower := strings.ToLower(strings.TrimSpace(head))
	switch {
	case strings.HasPrefix(lower, "@debug") || strings.HasPrefix(lower, "@warn") || strings.HasPrefix(lower, "@error"):
		return NodeTypeDebug
	case strings.HasPrefix(lower, "@include"):
		return NodeTypeMixinReference
	case strings.HasPrefix(lower, "@content"):
		return NodeTypeMixinContentReference
	case strings.HasPrefix(lower, "@return"):
		return NodeTypeReturnStatement
	default:
		return typ
	}
}

func (p *Parser) addExtendsReferences(parent *Node, start, end int) {
	text := strings.ToLower(string(p.source[start:end]))
	searchStart := 0
	for {
		index := strings.Index(text[searchStart:], ":extend(")
		if index == -1 {
			return
		}
		refStart := start + searchStart + index
		open := refStart + len(":extend")
		close := p.matchingParen(open, end)
		if close == -1 {
			return
		}
		parent.AddChild(NewNode(refStart, close-refStart+1, NodeTypeExtendsReference))
		searchStart = close - start + 1
	}
}

func (p *Parser) addNumericChildren(parent *Node, start, end int) {
	for cursor := start; cursor < end; cursor++ {
		if !isNumberStart(p.source, cursor, end) {
			continue
		}
		length := scanNumericValue(p.source[cursor:end])
		parent.AddChild(NewNode(cursor, length, NodeTypeNumericValue))
		cursor += length - 1
	}
}

func blockAtRuleType(head string) NodeType {
	lower := strings.ToLower(strings.TrimSpace(head))
	switch {
	case isLESSMixinDeclaration(lower):
		return NodeTypeMixinDeclaration
	case isKeyframesAtRule(lower):
		return NodeTypeKeyframe
	case strings.HasPrefix(lower, "@mixin"):
		return NodeTypeMixinDeclaration
	case strings.HasPrefix(lower, "@function"):
		return NodeTypeFunctionDeclaration
	case strings.HasPrefix(lower, "@font-face"):
		return NodeTypeFontFace
	case strings.HasPrefix(lower, "@starting-style"):
		return NodeTypeStartingStyleAtRule
	case strings.HasPrefix(lower, "@media"):
		return NodeTypeMedia
	case strings.HasPrefix(lower, "@supports"):
		return NodeTypeSupports
	case strings.HasPrefix(lower, "@layer"):
		return NodeTypeLayer
	case strings.HasPrefix(lower, "@scope"):
		return NodeTypeScope
	case strings.HasPrefix(lower, "@container"):
		return NodeTypeContainer
	case strings.HasPrefix(lower, "@property"):
		return NodeTypePropertyAtRule
	case strings.HasPrefix(lower, "@page"):
		return NodeTypePage
	case strings.HasPrefix(lower, "@viewport") || strings.HasPrefix(lower, "@-ms-viewport"):
		return NodeTypeViewPort
	case strings.HasPrefix(lower, "@document") || strings.HasPrefix(lower, "@-moz-document"):
		return NodeTypeDocument
	case strings.HasPrefix(lower, "@"):
		return NodeTypeUnknownAtRule
	default:
		return NodeTypeUndefined
	}
}

func statementAtRuleType(head string) NodeType {
	lower := strings.ToLower(strings.TrimSpace(head))
	switch {
	case strings.HasPrefix(lower, "@import"):
		return NodeTypeImport
	case strings.HasPrefix(lower, "@plugin"):
		return NodeTypePlugin
	case strings.HasPrefix(lower, "@use"):
		return NodeTypeUse
	case strings.HasPrefix(lower, "@forward"):
		return NodeTypeForward
	case strings.HasPrefix(lower, "@namespace"):
		return NodeTypeNamespace
	case strings.HasPrefix(lower, "@"):
		return NodeTypeUnknownAtRule
	default:
		return NodeTypeUndefined
	}
}

func isKeyframesAtRule(lowerHead string) bool {
	if strings.HasPrefix(lowerHead, "@keyframes") {
		return true
	}
	if !strings.HasPrefix(lowerHead, "@-") {
		return false
	}
	nameEnd := strings.IndexFunc(lowerHead, func(r rune) bool {
		return r == ' ' || r == '\t' || r == '\n' || r == '\r' || r == '\f' || r == '{'
	})
	if nameEnd == -1 {
		nameEnd = len(lowerHead)
	}
	return strings.HasSuffix(lowerHead[:nameEnd], "keyframes")
}

func isLESSMixinDeclaration(lowerHead string) bool {
	if !strings.HasPrefix(lowerHead, ".") && !strings.HasPrefix(lowerHead, "#") {
		return false
	}
	runes := []rune(lowerHead)
	cursor := 1
	for cursor < len(runes) && isIdentifierPart(runes[cursor]) {
		cursor++
	}
	if cursor == 1 {
		return false
	}
	cursor = skipWhitespaceRunes(runes, cursor, len(runes))
	return cursor < len(runes) && runes[cursor] == '('
}

func valueTermWithChild(offset, length int, childType NodeType) *Node {
	term := NewNode(offset, length, NodeTypeTerm)
	term.AddChild(NewNode(offset, length, childType))
	return term
}

func isIdentifierStart(r rune) bool {
	return r == '_' || r == '-' || r >= 'a' && r <= 'z' || r >= 'A' && r <= 'Z' || r >= 0x80
}

func isIdentifierPart(r rune) bool {
	return isIdentifierStart(r) || r >= '0' && r <= '9'
}

func scanOperator(runes []rune) int {
	if len(runes) == 0 {
		return 0
	}
	if len(runes) >= 2 {
		switch string(runes[:2]) {
		case ">=", "=<", "<=", "==", "!=":
			return 2
		}
	}
	if len(runes) >= 3 {
		switch strings.ToLower(string(runes[:3])) {
		case "and", "not":
			return 3
		}
	}
	switch runes[0] {
	case '>', '<', '+', '-', '*', '/', '%':
		return 1
	default:
		return 0
	}
}

func isWhitespace(r rune) bool {
	switch r {
	case ' ', '\t', '\n', '\r', '\f':
		return true
	default:
		return false
	}
}

func skipWhitespaceRunes(runes []rune, start, end int) int {
	for start < end && isWhitespace(runes[start]) {
		start++
	}
	return start
}

func isVariableNamePart(r rune) bool {
	return isIdentifierPart(r) || r >= '0' && r <= '9'
}

func scanIdentifier(runes []rune, start, end int) int {
	for start < end && isIdentifierPart(runes[start]) {
		start++
	}
	return start
}

func scanHexColor(runes []rune) int {
	if len(runes) == 0 || runes[0] != '#' {
		return 0
	}
	i := 1
	for i < len(runes) && isHexDigit(runes[i]) {
		i++
	}
	switch i - 1 {
	case 3, 4, 6, 8:
		return i
	default:
		return 0
	}
}

func isNumberStart(runes []rune, index, end int) bool {
	if index >= end {
		return false
	}
	if runes[index] >= '0' && runes[index] <= '9' {
		return true
	}
	if (runes[index] == '+' || runes[index] == '-') && index+1 < end {
		return runes[index+1] >= '0' && runes[index+1] <= '9' || runes[index+1] == '.'
	}
	return runes[index] == '.' && index+1 < end && runes[index+1] >= '0' && runes[index+1] <= '9'
}

func scanNumericValue(runes []rune) int {
	i := 0
	if i < len(runes) && (runes[i] == '+' || runes[i] == '-') {
		i++
	}
	for i < len(runes) && runes[i] >= '0' && runes[i] <= '9' {
		i++
	}
	if i < len(runes) && runes[i] == '.' {
		i++
		for i < len(runes) && runes[i] >= '0' && runes[i] <= '9' {
			i++
		}
	}
	if i < len(runes) && runes[i] == '%' {
		i++
	}
	for i < len(runes) && isIdentifierStart(runes[i]) {
		i++
	}
	return i
}

func firstSelectorPart(text string) string {
	fields := strings.FieldsFunc(text, func(r rune) bool {
		return r == ' ' || r == ',' || r == '{'
	})
	if len(fields) == 0 {
		return text
	}
	return fields[0]
}

func leadingWhitespace(runes []rune) int {
	i := 0
	for i < len(runes) {
		switch runes[i] {
		case ' ', '\t', '\n', '\r', '\f':
			i++
		default:
			return i
		}
	}
	return i
}

func trimRightRunes(runes []rune, start, end int) int {
	for end > start {
		switch runes[end-1] {
		case ' ', '\t', '\n', '\r', '\f':
			end--
		default:
			return end
		}
	}
	return end
}

// runeIndex is strings.Index measured in runes, matching Parser.source offsets.
func runeIndex(text, substr string) int {
	index := strings.Index(text, substr)
	if index <= 0 {
		return index
	}
	return utf8.RuneCountInString(text[:index])
}

// indexUnicodeRangePrefix returns the rune index of the first "u+" or "U+",
// so callers can index the rune source without byte/rune offset drift.
func indexUnicodeRangePrefix(runes []rune) int {
	for i := 0; i+1 < len(runes); i++ {
		if (runes[i] == 'u' || runes[i] == 'U') && runes[i+1] == '+' {
			return i
		}
	}
	return -1
}

func lenUntilDelimiter(runes []rune) int {
	i := 0
	for i < len(runes) {
		switch runes[i] {
		case ' ', '\t', '\n', '\r', '\f', ',', ';', '}':
			return i
		default:
			i++
		}
	}
	return i
}

func max(a, b int) int {
	if a > b {
		return a
	}
	return b
}
