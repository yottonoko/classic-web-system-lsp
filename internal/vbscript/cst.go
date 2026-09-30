package vbscript

import (
	"strings"

	"github.com/yottonoko/classic-web-system-lsp/internal/core"
)

// Token is a lossless lexical VBScript token with byte offsets in the source text.
type Token struct {
	Kind  string
	Start int
	End   int
	Text  string
}

// ParseCST builds the lightweight VBScript CST used for parser parity and node lookup.
func ParseCST(text string) *CSTNode {
	tokens := Tokenize(text)
	document := &CSTNode{
		Kind:   "Document",
		Start:  0,
		End:    len(text),
		Tokens: tokens,
	}
	significant := significantTokens(tokens)
	stack := []*CSTNode{document}
	for _, index := range cstStatementStartIndexes(significant) {
		token := significant[index]
		first := strings.ToLower(token.Text)
		second := lowerCSTToken(significant, index+1)

		if first == "elseif" {
			stack = appendCSTBranch(stack, "If", newStructuredCSTNode("ElseIf", CSTStatementElseIf, CSTStatementRoleBranch, token, significant, index))
			continue
		}
		if first == "else" {
			stack = appendCSTBranch(stack, "If", newStructuredCSTNode("Else", CSTStatementElse, CSTStatementRoleBranch, token, significant, index))
			continue
		}
		if first == "case" {
			kind := CSTStatementCase
			if second == "else" {
				kind = CSTStatementCaseElse
			}
			stack = appendCSTBranch(stack, "Select", newStructuredCSTNode("Case", kind, CSTStatementRoleBranch, token, significant, index))
			continue
		}
		if first == "end" {
			if target, kind, ok := cstEndStatement(second); ok {
				terminator := newStructuredCSTNode("Terminator", kind, CSTStatementRoleTerminator, token, significant, index)
				stack = closeCSTWithTerminator(stack, terminator, target)
			} else {
				appendCSTExecutable(stack[len(stack)-1], token, significant, index)
			}
			continue
		}
		if first == "loop" {
			terminator := newStructuredCSTNode("Terminator", CSTStatementLoop, CSTStatementRoleTerminator, token, significant, index)
			stack = closeCSTWithTerminator(stack, terminator, "DoLoop")
			continue
		}
		if first == "wend" {
			terminator := newStructuredCSTNode("Terminator", CSTStatementWend, CSTStatementRoleTerminator, token, significant, index)
			stack = closeCSTWithTerminator(stack, terminator, "While")
			continue
		}
		if first == "next" {
			terminator := newStructuredCSTNode("Terminator", CSTStatementNext, CSTStatementRoleTerminator, token, significant, index)
			stack = closeCSTWithTerminator(stack, terminator, "For", "ForEach")
			continue
		}

		current := stack[len(stack)-1]
		declarationStart := first
		declarationOffset := 0
		for declarationStart == "public" || declarationStart == "private" || declarationStart == "default" {
			declarationOffset++
			declarationStart = lowerCSTToken(significant, index+declarationOffset)
		}
		if declarationStart == "class" && tokenKindAt(significant, index+declarationOffset+1) == "identifier" {
			node := newStructuredCSTNode("Class", CSTStatementClass, CSTStatementRoleHeader, token, significant, index)
			node.NameToken = tokenPtrFromNode(node, significant[index+declarationOffset+1].Start)
			current.Children = append(current.Children, node)
			stack = append(stack, node)
			continue
		}
		if declarationStart == "sub" || declarationStart == "function" {
			nameIndex := index + declarationOffset + 1
			if tokenKindAt(significant, nameIndex) == "identifier" {
				kind := CSTStatementSub
				if declarationStart == "function" {
					kind = CSTStatementFunction
				}
				node := newStructuredCSTNode("Procedure", kind, CSTStatementRoleHeader, token, significant, index)
				node.NameToken = tokenPtrFromNode(node, significant[nameIndex].Start)
				current.Children = append(current.Children, node)
				stack = append(stack, node)
			}
			continue
		}
		if declarationStart == "property" {
			propertyKind := lowerCSTToken(significant, index+declarationOffset+1)
			nameIndex := index + declarationOffset + 2
			kind := CSTStatementPropertyGet
			switch propertyKind {
			case "let":
				kind = CSTStatementPropertyLet
			case "set":
				kind = CSTStatementPropertySet
			}
			if (propertyKind == "get" || propertyKind == "let" || propertyKind == "set") && tokenKindAt(significant, nameIndex) == "identifier" {
				node := newStructuredCSTNode("Property", kind, CSTStatementRoleHeader, token, significant, index)
				node.NameToken = tokenPtrFromNode(node, significant[nameIndex].Start)
				current.Children = append(current.Children, node)
				stack = append(stack, node)
				continue
			}
		}
		switch {
		case first == "if":
			node := newStructuredCSTNode("If", CSTStatementIf, CSTStatementRoleHeader, token, significant, index)
			current.Children = append(current.Children, node)
			if cstStatementHasKeyword(significant, index, "then") && cstStatementKeywordIsLast(significant, index, "then") {
				stack = append(stack, node)
			} else {
				appendInlineIfChildren(node)
			}
		case first == "select" && second == "case":
			node := newStructuredCSTNode("Select", CSTStatementSelect, CSTStatementRoleHeader, token, significant, index)
			current.Children = append(current.Children, node)
			stack = append(stack, node)
		case first == "do":
			node := newStructuredCSTNode("DoLoop", CSTStatementDo, CSTStatementRoleHeader, token, significant, index)
			current.Children = append(current.Children, node)
			stack = append(stack, node)
		case first == "while":
			node := newStructuredCSTNode("While", CSTStatementWhile, CSTStatementRoleHeader, token, significant, index)
			current.Children = append(current.Children, node)
			stack = append(stack, node)
		case first == "for" && second == "each":
			node := newStructuredCSTNode("ForEach", CSTStatementForEach, CSTStatementRoleHeader, token, significant, index)
			current.Children = append(current.Children, node)
			if tokenKindAt(significant, index+2) == "identifier" {
				node.NameToken = tokenPtrFromNode(node, significant[index+2].Start)
			}
			stack = append(stack, node)
		case first == "for":
			node := newStructuredCSTNode("For", CSTStatementFor, CSTStatementRoleHeader, token, significant, index)
			current.Children = append(current.Children, node)
			if tokenKindAt(significant, index+1) == "identifier" {
				node.NameToken = tokenPtrFromNode(node, significant[index+1].Start)
			}
			stack = append(stack, node)
		case first == "call":
			current.Children = append(current.Children, newExecutableCSTNode("Call", token, significant, index))
		case first == "dim":
			for nameIndex, nameToken := range cstDimNameTokens(significant, index) {
				node := newCSTNode("VariableDeclaration", token, significant, index)
				if nameIndex == 0 {
					node.Statement = newCSTStatement(CSTStatementExecutable, CSTStatementRoleExecutable, node.Tokens)
				}
				node.NameToken = tokenPtrFromNode(node, nameToken.Start)
				current.Children = append(current.Children, node)
			}
		case cstStatementHasSymbol(significant, index, "="):
			current.Children = append(current.Children, newExecutableCSTNode("Assignment", token, significant, index))
		default:
			appendCSTExecutable(current, token, significant, index)
		}
	}
	closeOpenCSTNodes(stack, len(text))
	return document
}

// ParseDocumentCST returns the immutable document CST retained by the reference
// shard when one is available. It preserves absolute byte offsets and block
// nesting across ASP islands.
func ParseDocumentCST(parsed *core.ParsedDocument) *CSTNode {
	if parsed == nil {
		return ParseCST("")
	}
	if shard := cachedReferenceShard(parsed); shard != nil {
		return shard.documentCSTFor(parsed)
	}
	return parseDocumentCST(parsed)
}

func parseDocumentCST(parsed *core.ParsedDocument) *CSTNode {
	masked := []byte(parsed.Text)
	for index, value := range masked {
		if value != '\r' && value != '\n' {
			masked[index] = ' '
		}
	}
	for _, region := range parsed.Regions {
		if region.Language != core.LanguageVBScript || region.Kind == core.RegionASPExpression {
			continue
		}
		start := max(0, min(region.ContentStart, len(masked)))
		end := max(start, min(region.ContentEnd, len(masked)))
		if start > 0 && masked[start-1] != '\r' && masked[start-1] != '\n' {
			masked[start-1] = '\n'
		}
		copy(masked[start:end], parsed.Text[start:end])
	}
	return ParseCST(string(masked))
}

// Tokenize returns lossless VBScript tokens, including comments, whitespace, and newlines.
func Tokenize(text string) []Token {
	return tokenizeWithBase(text, 0)
}

func tokenizeWithBase(text string, base int) []Token {
	tokens := make([]Token, 0, len(text)/4)
	for offset := 0; offset < len(text); {
		switch text[offset] {
		case ' ', '\t':
			start := offset
			for offset < len(text) && (text[offset] == ' ' || text[offset] == '\t') {
				offset++
			}
			tokens = append(tokens, Token{Kind: "whitespace", Start: base + start, End: base + offset, Text: text[start:offset]})
		case '\r', '\n':
			start := offset
			if text[offset] == '\r' && offset+1 < len(text) && text[offset+1] == '\n' {
				offset += 2
			} else {
				offset++
			}
			tokens = append(tokens, Token{Kind: "newline", Start: base + start, End: base + offset, Text: text[start:offset]})
		case '\'':
			end := skipLine(text, offset)
			tokens = append(tokens, Token{Kind: "comment", Start: base + offset, End: base + end, Text: text[offset:end]})
			offset = end
		case '"':
			start := offset
			offset++
			for offset < len(text) {
				if text[offset] == '"' {
					offset++
					if offset < len(text) && text[offset] == '"' {
						offset++
						continue
					}
					break
				}
				offset++
			}
			tokens = append(tokens, Token{Kind: "string", Start: base + start, End: base + offset, Text: text[start:offset]})
		case '#':
			start := offset
			if end, ok := skipDateLiteral(text, offset); ok {
				offset = end
				tokens = append(tokens, Token{Kind: "date", Start: base + start, End: base + offset, Text: text[start:offset]})
				continue
			}
			tokens = append(tokens, Token{Kind: "symbol", Start: base + offset, End: base + offset + 1, Text: text[offset : offset+1]})
			offset++
		default:
			if end, ok := skipSignedDecimalLiteral(text, offset); ok {
				tokens = append(tokens, Token{Kind: "number", Start: base + offset, End: base + end, Text: text[offset:end]})
				offset = end
				continue
			}
			if end, ok := skipDecimalLiteral(text, offset); ok {
				tokens = append(tokens, Token{Kind: "number", Start: base + offset, End: base + end, Text: text[offset:end]})
				offset = end
				continue
			}
			if end, ok := skipVBNumericLiteral(text, offset); ok {
				tokens = append(tokens, Token{Kind: "number", Start: base + offset, End: base + end, Text: text[offset:end]})
				offset = end
				continue
			}
			if isIdentifierStart(text[offset]) {
				start := offset
				offset++
				for offset < len(text) && isIdent(text[offset]) {
					offset++
				}
				if isRemComment(text, start, offset) {
					end := skipLine(text, start)
					tokens = append(tokens, Token{Kind: "comment", Start: base + start, End: base + end, Text: text[start:end]})
					offset = end
					continue
				}
				kind := "identifier"
				if isVBKeyword(text[start:offset]) {
					kind = "keyword"
				}
				tokens = append(tokens, Token{Kind: kind, Start: base + start, End: base + offset, Text: text[start:offset]})
				continue
			}
			tokens = append(tokens, Token{Kind: "symbol", Start: base + offset, End: base + offset + 1, Text: text[offset : offset+1]})
			offset++
		}
	}
	return tokens
}

func significantTokens(tokens []Token) []Token {
	return appendSignificantTokens(make([]Token, 0, len(tokens)), tokens)
}

// appendSignificantTokens also supports compacting tokens in place by passing
// tokens[:0] as significant. Callers must own the input in that case.
func appendSignificantTokens(significant, tokens []Token) []Token {
	for _, token := range tokens {
		if token.Kind == "whitespace" {
			continue
		}
		if token.Kind == "comment" {
			if len(significant) > 0 && significant[len(significant)-1].Text == "_" {
				significant = append(significant, token)
			}
			continue
		}
		significant = append(significant, token)
	}
	return significant
}

func isCSTStatementStart(tokens []Token, index int) bool {
	if index == 0 {
		return true
	}
	previous := tokens[index-1]
	return previous.Text == ":" || previous.Kind == "newline" && !cstNewlineIsContinued(tokens, index-1)
}

func skipSignedDecimalLiteral(text string, offset int) (int, bool) {
	if offset >= len(text) || text[offset] != '-' {
		return offset, false
	}
	if offset > 0 {
		previous := offset - 1
		for previous >= 0 && (text[previous] == ' ' || text[previous] == '\t') {
			previous--
		}
		if previous >= 0 && (isIdent(text[previous]) || text[previous] >= '0' && text[previous] <= '9' || text[previous] == ')' || text[previous] == '#') {
			return offset, false
		}
	}
	cursor := offset + 1
	if cursor >= len(text) || !isVBDecimalLiteralStart(text, cursor) {
		return offset, false
	}
	return skipDecimalLiteral(text, cursor)
}

func skipDecimalLiteral(text string, offset int) (int, bool) {
	if !isVBDecimalLiteralStart(text, offset) {
		return offset, false
	}
	cursor := offset
	if text[cursor] == '.' {
		cursor += 2
	} else {
		cursor++
	}
	for cursor < len(text) && text[cursor] >= '0' && text[cursor] <= '9' {
		cursor++
	}
	if cursor < len(text) && text[cursor] == '.' && cursor+1 < len(text) && text[cursor+1] >= '0' && text[cursor+1] <= '9' {
		cursor += 2
		for cursor < len(text) && text[cursor] >= '0' && text[cursor] <= '9' {
			cursor++
		}
	}
	if cursor < len(text) && (text[cursor] == 'e' || text[cursor] == 'E') {
		exponent := cursor + 1
		if exponent < len(text) && (text[exponent] == '+' || text[exponent] == '-') {
			exponent++
		}
		digits := exponent
		for exponent < len(text) && text[exponent] >= '0' && text[exponent] <= '9' {
			exponent++
		}
		if exponent > digits {
			cursor = exponent
		}
	}
	return cursor, true
}

func isVBDecimalLiteralStart(text string, offset int) bool {
	if offset < 0 || offset >= len(text) {
		return false
	}
	if text[offset] >= '0' && text[offset] <= '9' {
		return true
	}
	return text[offset] == '.' && offset+1 < len(text) && text[offset+1] >= '0' && text[offset+1] <= '9'
}

func skipDateLiteral(text string, offset int) (int, bool) {
	if offset < 0 || offset >= len(text) || text[offset] != '#' {
		return offset, false
	}
	for cursor := offset + 1; cursor < len(text); cursor++ {
		if text[cursor] == '#' {
			return cursor + 1, true
		}
		if text[cursor] == '\r' || text[cursor] == '\n' {
			return cursor, true
		}
	}
	return len(text), true
}

func cstStatementStartIndexes(tokens []Token) []int {
	starts := make([]int, 0, len(tokens)/4)
	for cursor := 0; cursor < len(tokens); {
		for cursor < len(tokens) && (tokens[cursor].Kind == "newline" || tokens[cursor].Text == ":") {
			cursor++
		}
		if cursor >= len(tokens) {
			break
		}
		starts = append(starts, cursor)
		end := cstStatementEndIndex(tokens, cursor)
		if end <= cursor {
			cursor++
		} else {
			cursor = end + 1
		}
	}
	return starts
}

func newCSTNode(kind string, first Token, tokens []Token, index int) *CSTNode {
	statementTokens := cstStatementTokens(tokens, index)
	return &CSTNode{
		Kind:   kind,
		Start:  first.Start,
		End:    cstStatementEnd(tokens, index),
		Tokens: statementTokens,
	}
}

func newStructuredCSTNode(nodeKind string, statementKind CSTStatementKind, role CSTStatementRole, first Token, tokens []Token, index int) *CSTNode {
	node := newCSTNode(nodeKind, first, tokens, index)
	node.Statement = newCSTStatement(statementKind, role, node.Tokens)
	return node
}

func newExecutableCSTNode(nodeKind string, first Token, tokens []Token, index int) *CSTNode {
	return newStructuredCSTNode(nodeKind, CSTStatementExecutable, CSTStatementRoleExecutable, first, tokens, index)
}

func newCSTStatement(kind CSTStatementKind, role CSTStatementRole, tokens []Token) *CSTStatement {
	statementTokens := append([]Token(nil), tokens...)
	statement := &CSTStatement{Kind: kind, Role: role, Tokens: statementTokens}
	if len(statementTokens) > 0 {
		statement.Start = statementTokens[0].Start
		statement.End = statementTokens[len(statementTokens)-1].End
	}
	populateCSTStatementParts(statement)
	return statement
}

func populateCSTStatementParts(statement *CSTStatement) {
	tokens := statement.Tokens
	if len(tokens) == 0 {
		return
	}
	switch statement.Kind {
	case CSTStatementIf, CSTStatementElseIf:
		thenIndex := cstTokenIndex(tokens, "then", 1)
		if thenIndex > 1 {
			statement.Parts.Condition = append([]Token(nil), tokens[1:thenIndex]...)
		}
		if thenIndex >= 0 && thenIndex+1 < len(tokens) {
			elseIndex := cstInlineElseIndex(tokens, thenIndex+1)
			if elseIndex < 0 {
				statement.Parts.InlineThen = append([]Token(nil), tokens[thenIndex+1:]...)
			} else {
				statement.Parts.InlineThen = append([]Token(nil), tokens[thenIndex+1:elseIndex]...)
				statement.Parts.InlineElse = append([]Token(nil), tokens[elseIndex+1:]...)
			}
		}
	case CSTStatementSelect:
		if len(tokens) > 2 {
			statement.Parts.Selector = append([]Token(nil), tokens[2:]...)
		}
	case CSTStatementCase:
		if len(tokens) > 1 {
			statement.Parts.CaseValues = append([]Token(nil), tokens[1:]...)
		}
	case CSTStatementDo, CSTStatementLoop:
		if len(tokens) > 2 && (strings.EqualFold(tokens[1].Text, "while") || strings.EqualFold(tokens[1].Text, "until")) {
			statement.Parts.Until = strings.EqualFold(tokens[1].Text, "until")
			statement.Parts.LoopCondition = append([]Token(nil), tokens[2:]...)
		}
	case CSTStatementWhile:
		if len(tokens) > 1 {
			statement.Parts.LoopCondition = append([]Token(nil), tokens[1:]...)
		}
	case CSTStatementFor, CSTStatementForEach:
		if len(tokens) > 1 {
			statement.Parts.LoopCondition = append([]Token(nil), tokens[1:]...)
		}
	case CSTStatementExecutable:
		statement.Parts.Callee = cstExecutableCallee(tokens)
	}
}

func cstExecutableCallee(tokens []Token) []Token {
	start := 0
	if strings.EqualFold(tokens[0].Text, "call") {
		start = 1
	} else {
		for _, token := range tokens {
			if token.Text == "=" {
				return nil
			}
		}
	}
	if start >= len(tokens) || tokens[start].Kind != "identifier" {
		return nil
	}
	end := start + 1
	for end+1 < len(tokens) && tokens[end].Text == "." && tokens[end+1].Kind == "identifier" {
		end += 2
	}
	return append([]Token(nil), tokens[start:end]...)
}

func appendCSTExecutable(parent *CSTNode, first Token, tokens []Token, index int) {
	parent.Children = append(parent.Children, newExecutableCSTNode("Statement", first, tokens, index))
}

func appendCSTBranch(stack []*CSTNode, targetKind string, branch *CSTNode) []*CSTNode {
	for index := len(stack) - 1; index > 0; index-- {
		if stack[index].Kind != targetKind {
			continue
		}
		for openIndex := index + 1; openIndex < len(stack); openIndex++ {
			if stack[openIndex].End < branch.Start {
				stack[openIndex].End = branch.Start
			}
		}
		stack = stack[:index+1]
		stack[index].Children = append(stack[index].Children, branch)
		return append(stack, branch)
	}
	stack[len(stack)-1].Children = append(stack[len(stack)-1].Children, branch)
	return stack
}

func closeCSTWithTerminator(stack []*CSTNode, terminator *CSTNode, targetKinds ...string) []*CSTNode {
	for index := len(stack) - 1; index > 0; index-- {
		matches := false
		for _, targetKind := range targetKinds {
			if stack[index].Kind == targetKind {
				matches = true
				break
			}
		}
		if !matches {
			continue
		}
		for openIndex := index + 1; openIndex < len(stack); openIndex++ {
			if stack[openIndex].End < terminator.Start {
				stack[openIndex].End = terminator.Start
			}
		}
		stack[index].Children = append(stack[index].Children, terminator)
		if stack[index].End < terminator.End {
			stack[index].End = terminator.End
		}
		return stack[:index]
	}
	stack[len(stack)-1].Children = append(stack[len(stack)-1].Children, terminator)
	return stack
}

func cstEndStatement(second string) (string, CSTStatementKind, bool) {
	switch second {
	case "if":
		return "If", CSTStatementEndIf, true
	case "select":
		return "Select", CSTStatementEndSelect, true
	case "sub":
		return "Procedure", CSTStatementEndSub, true
	case "function":
		return "Procedure", CSTStatementEndFunction, true
	case "property":
		return "Property", CSTStatementEndProperty, true
	case "class":
		return "Class", CSTStatementEndClass, true
	default:
		return "", "", false
	}
}

func appendInlineIfChildren(node *CSTNode) {
	thenIndex := cstTokenIndex(node.Tokens, "then", 0)
	if thenIndex < 0 {
		return
	}
	elseIndex := cstInlineElseIndex(node.Tokens, thenIndex+1)
	headerTokens := node.Tokens[:thenIndex+1]
	fullStatement := newCSTStatement(CSTStatementIf, CSTStatementRoleHeader, node.Tokens)
	node.Statement = newCSTStatement(CSTStatementIf, CSTStatementRoleHeader, headerTokens)
	node.Statement.Parts.InlineThen = fullStatement.Parts.InlineThen
	node.Statement.Parts.InlineElse = fullStatement.Parts.InlineElse
	thenEnd := len(node.Tokens)
	if elseIndex >= 0 {
		thenEnd = elseIndex
	}
	appendInlineExecutableChildren(node, node.Tokens[thenIndex+1:thenEnd])
	if elseIndex < 0 {
		return
	}
	branch := &CSTNode{Kind: "Else", Start: node.Tokens[elseIndex].Start, End: node.Tokens[len(node.Tokens)-1].End, Tokens: append([]Token(nil), node.Tokens[elseIndex:]...)}
	branch.Statement = newCSTStatement(CSTStatementElse, CSTStatementRoleBranch, node.Tokens[elseIndex:elseIndex+1])
	appendInlineExecutableChildren(branch, node.Tokens[elseIndex+1:])
	node.Children = append(node.Children, branch)
}

func appendInlineExecutableChildren(parent *CSTNode, tokens []Token) {
	start := 0
	for end := 0; end <= len(tokens); end++ {
		if end < len(tokens) && tokens[end].Text != ":" {
			continue
		}
		if child := newInlineExecutableCSTNode(tokens[start:end]); child != nil {
			parent.Children = append(parent.Children, child)
		}
		start = end + 1
	}
}

func newInlineExecutableCSTNode(tokens []Token) *CSTNode {
	if len(tokens) == 0 {
		return nil
	}
	kind := "Statement"
	if strings.EqualFold(tokens[0].Text, "call") {
		kind = "Call"
	} else {
		for _, token := range tokens {
			if token.Text == "=" {
				kind = "Assignment"
				break
			}
		}
	}
	copyTokens := append([]Token(nil), tokens...)
	node := &CSTNode{Kind: kind, Start: copyTokens[0].Start, End: copyTokens[len(copyTokens)-1].End, Tokens: copyTokens}
	node.Statement = newCSTStatement(CSTStatementExecutable, CSTStatementRoleExecutable, copyTokens)
	return node
}

func cstTokenIndex(tokens []Token, text string, start int) int {
	for index := start; index < len(tokens); index++ {
		if strings.EqualFold(tokens[index].Text, text) {
			return index
		}
	}
	return -1
}

func cstInlineElseIndex(tokens []Token, start int) int {
	for index := start; index < len(tokens); index++ {
		if !strings.EqualFold(tokens[index].Text, "else") {
			continue
		}
		if index > start && strings.EqualFold(tokens[index-1].Text, "case") {
			continue
		}
		return index
	}
	return -1
}

func cstStatementTokens(tokens []Token, index int) []Token {
	end := cstStatementEndIndex(tokens, index)
	statement := make([]Token, 0, end-index)
	for cursor := index; cursor < end; cursor++ {
		statement = append(statement, tokens[cursor])
	}
	return statement
}

func cstStatementEnd(tokens []Token, index int) int {
	end := cstStatementEndIndex(tokens, index)
	if end == index {
		return tokens[index].End
	}
	return tokens[end-1].End
}

func cstStatementEndIndex(tokens []Token, index int) int {
	inlineIf := cstStartsInlineIf(tokens, index)
	for cursor := index; cursor < len(tokens); cursor++ {
		if tokens[cursor].Text == ":" && (!inlineIf || cstInlineIfColonEndsStatement(tokens, cursor)) || tokens[cursor].Kind == "newline" && !cstNewlineIsContinued(tokens, cursor) {
			return cursor
		}
	}
	return len(tokens)
}

func cstStartsInlineIf(tokens []Token, index int) bool {
	if index >= len(tokens) || !strings.EqualFold(tokens[index].Text, "if") {
		return false
	}
	for cursor := index + 1; cursor < len(tokens); cursor++ {
		if tokens[cursor].Kind == "newline" {
			return false
		}
		if strings.EqualFold(tokens[cursor].Text, "then") {
			return cursor+1 < len(tokens) && tokens[cursor+1].Kind != "newline" && tokens[cursor+1].Text != ":"
		}
	}
	return false
}

func cstInlineIfColonEndsStatement(tokens []Token, colonIndex int) bool {
	if colonIndex+1 >= len(tokens) {
		return true
	}
	first := strings.ToLower(tokens[colonIndex+1].Text)
	switch first {
	case "case", "class", "do", "end", "for", "function", "loop", "next", "property", "select", "sub", "wend", "while":
		return true
	default:
		return false
	}
}

func cstNewlineIsContinued(tokens []Token, newlineIndex int) bool {
	return newlineIndex > 0 && tokens[newlineIndex-1].Text == "_"
}

func closeOpenCSTNodes(stack []*CSTNode, end int) {
	for _, node := range stack[1:] {
		if node.End < end {
			node.End = end
		}
	}
}

func tokenPtrFromNode(node *CSTNode, start int) *Token {
	for index := range node.Tokens {
		if node.Tokens[index].Start == start {
			return &node.Tokens[index]
		}
	}
	return nil
}

func tokenKindAt(tokens []Token, index int) string {
	if index < 0 || index >= len(tokens) {
		return ""
	}
	return tokens[index].Kind
}

func lowerCSTToken(tokens []Token, index int) string {
	if index < 0 || index >= len(tokens) {
		return ""
	}
	return strings.ToLower(tokens[index].Text)
}

func cstStatementHasSymbol(tokens []Token, index int, symbol string) bool {
	end := cstStatementEndIndex(tokens, index)
	for cursor := index; cursor < end; cursor++ {
		if tokens[cursor].Text == symbol {
			return true
		}
	}
	return false
}

func cstStatementHasKeyword(tokens []Token, index int, keyword string) bool {
	end := cstStatementEndIndex(tokens, index)
	for cursor := index; cursor < end; cursor++ {
		if strings.EqualFold(tokens[cursor].Text, keyword) {
			return true
		}
	}
	return false
}

func cstStatementKeywordIsLast(tokens []Token, index int, keyword string) bool {
	end := cstStatementEndIndex(tokens, index)
	for cursor := index; cursor < end; cursor++ {
		if !strings.EqualFold(tokens[cursor].Text, keyword) {
			continue
		}
		return cursor+1 >= end
	}
	return false
}

func cstDimNameTokens(tokens []Token, index int) []Token {
	end := cstStatementEndIndex(tokens, index)
	declarations := make([]Token, 0, 1)
	expectName := true
	parenthesisDepth := 0
	for cursor := index + 1; cursor < end; cursor++ {
		token := tokens[cursor]
		if expectName && parenthesisDepth == 0 && token.Kind == "identifier" {
			declarations = append(declarations, token)
			expectName = false
			continue
		}
		switch token.Text {
		case "(":
			parenthesisDepth++
		case ")":
			if parenthesisDepth > 0 {
				parenthesisDepth--
			}
		case ",":
			if parenthesisDepth == 0 {
				expectName = true
			}
		}
	}
	return declarations
}

func isVBKeyword(value string) bool {
	// Every keyword is short ASCII; lower-case into a stack buffer so the
	// tokenizer does not allocate for each identifier.
	if len(value) > vbKeywordMaxLength {
		return false
	}
	var buffer [vbKeywordMaxLength]byte
	for index := 0; index < len(value); index++ {
		character := value[index]
		if character >= 'A' && character <= 'Z' {
			character += 'a' - 'A'
		}
		buffer[index] = character
	}
	_, ok := vbKeywords[string(buffer[:len(value)])]
	return ok
}

// vbKeywordMaxLength bounds the lower-casing buffer; it must be at least as
// long as the longest entry of vbKeywords.
const vbKeywordMaxLength = 16

var vbKeywords = map[string]struct{}{
	"and": {}, "as": {}, "byref": {}, "byval": {}, "call": {}, "case": {},
	"class": {}, "const": {}, "debug": {}, "dim": {}, "do": {}, "each": {}, "else": {},
	"elseif": {}, "end": {}, "eqv": {}, "erase": {}, "error": {}, "execute": {},
	"exit": {}, "explicit": {}, "false": {}, "for": {}, "function": {}, "get": {}, "goto": {},
	"if": {}, "imp": {}, "in": {}, "is": {}, "let": {}, "like": {}, "line": {},
	"loop": {}, "me": {}, "mod": {}, "new": {}, "next": {}, "not": {}, "nothing": {}, "null": {},
	"on": {}, "option": {}, "or": {}, "optional": {}, "paramarray": {}, "preserve": {}, "private": {},
	"property": {}, "public": {}, "redim": {}, "rem": {}, "resume": {}, "select": {},
	"set": {}, "static": {}, "step": {}, "stop": {}, "sub": {}, "then": {}, "to": {}, "true": {},
	"typeof": {}, "until": {}, "wend": {}, "while": {}, "with": {}, "xor": {},
}
