package vbscript

import (
	"regexp"
	"sort"
	"strings"
	"unicode"
	"unicode/utf8"

	"github.com/yottonoko/classic-web-system-lsp/internal/core"
	"github.com/yottonoko/classic-web-system-lsp/internal/lsp"
)

const (
	semanticKeyword = iota
	semanticVariable
	semanticParameter
	semanticFunction
	semanticClass
	semanticMethod
	semanticProperty
	semanticComment
	semanticString
	semanticOperator
	semanticNamespace
	semanticInterface
	semanticEnum
	semanticEnumMember
	semanticTypeAlias
	semanticTypeParameter
	semanticConstant
)

const (
	semanticPublic = 1 << iota
	semanticPrivate
	semanticReadonly
	semanticLibrary
	semanticByref
	semanticByval
)

var SemanticTokenTypes = []string{
	"keyword",
	"variable",
	"parameter",
	"function",
	"class",
	"method",
	"property",
	"comment",
	"string",
	"operator",
	"namespace",
	"interface",
	"enum",
	"enumMember",
	"typeAlias",
	"typeParameter",
	"constant",
	"struct",
	"decorator",
	"event",
	"macro",
	"label",
	"number",
	"regexp",
}

var SemanticTokenModifiers = []string{"public", "private", "readonly", "library", "byref", "byval"}

var semanticKeywords = map[string]struct{}{
	"as": {}, "byref": {}, "byval": {}, "call": {}, "case": {}, "class": {}, "const": {}, "debug": {}, "default": {}, "dim": {},
	"do": {}, "each": {}, "else": {}, "elseif": {}, "end": {}, "erase": {}, "error": {}, "execute": {}, "exit": {}, "explicit": {},
	"for": {}, "function": {}, "get": {}, "goto": {}, "if": {}, "in": {}, "let": {}, "loop": {},
	"like": {}, "me": {}, "new": {}, "next": {}, "on": {}, "option": {}, "optional": {}, "paramarray": {}, "preserve": {},
	"private": {}, "property": {}, "public": {}, "redim": {}, "resume": {}, "select": {}, "set": {}, "static": {},
	"step": {}, "stop": {}, "sub": {}, "then": {}, "to": {}, "typeof": {}, "until": {}, "wend": {}, "while": {}, "with": {},
}

var libraryFunctions = builtinFunctionKeySet()

var libraryConstants = builtinConstantKeySet("application", "empty", "false", "nothing", "null", "request", "response", "server", "session", "true")

func builtinFunctionKeySet() map[string]struct{} {
	keys := make(map[string]struct{}, len(builtinFunctionCatalog))
	for _, spec := range builtinFunctionCatalog {
		keys[strings.ToLower(spec.Label)] = struct{}{}
	}
	return keys
}

func builtinConstantKeySet(extra ...string) map[string]struct{} {
	keys := make(map[string]struct{}, len(builtinConstantCatalog)+len(extra))
	for _, spec := range builtinConstantCatalog {
		keys[strings.ToLower(spec.Label)] = struct{}{}
	}
	for _, key := range extra {
		keys[strings.ToLower(key)] = struct{}{}
	}
	return keys
}

var semanticWordOperators = map[string]struct{}{
	"and": {}, "eqv": {}, "imp": {}, "is": {}, "mod": {}, "not": {}, "or": {}, "xor": {},
}

var semanticIncludePattern = regexp.MustCompile(`(?is)<!--\s*(#include)\s+(file|virtual)\s*=\s*("[^"]+"|'[^']+'|[^\s>]+)\s*-->`)
var jsClassDeclPattern = regexp.MustCompile(`\bclass\s+([A-Za-z_$][A-Za-z0-9_$]*)`)
var jsFunctionDeclPattern = regexp.MustCompile(`\bfunction\s+([A-Za-z_$][A-Za-z0-9_$]*)\s*\(([^)]*)\)`)
var jsMethodDeclPattern = regexp.MustCompile(`(?m)^\s*([A-Za-z_$][A-Za-z0-9_$]*)\s*\(([^)]*)\)\s*\{`)
var jsMethodCallPattern = regexp.MustCompile(`\.([A-Za-z_$][A-Za-z0-9_$]*)\s*\(`)
var jsPropertyPattern = regexp.MustCompile(`\.([A-Za-z_$][A-Za-z0-9_$]*)`)
var jsVariableDeclPattern = regexp.MustCompile(`\b(?:const|let|var)\s+([A-Za-z_$][A-Za-z0-9_$]*)`)

type semanticToken struct {
	Line      int
	Character int
	Length    int
	Type      int
	Modifiers int
}

type classScopeTransition struct {
	offset int
	inside bool
}

type classScopeIndex []classScopeTransition

func SemanticTokens(parsed *core.ParsedDocument) lsp.SemanticTokens {
	var cached lsp.SemanticTokens
	if parsed.LoadAnalysis("vbscript.semantic-tokens.v1", &cached) {
		return cached
	}
	tokens := semanticTokens(parsed, nil, nil, true)
	parsed.StoreAnalysis("vbscript.semantic-tokens.v1", tokens)
	return tokens
}

func SemanticTokensWithExtraDeclarations(parsed *core.ParsedDocument, extra map[string]string) lsp.SemanticTokens {
	return semanticTokens(parsed, nil, extra, true)
}

func SemanticTokensWithoutJavaScriptWithExtraDeclarations(parsed *core.ParsedDocument, extra map[string]string) lsp.SemanticTokens {
	return semanticTokens(parsed, nil, extra, false)
}

func SemanticTokensRange(parsed *core.ParsedDocument, r lsp.Range) lsp.SemanticTokens {
	return semanticTokens(parsed, &r, nil, true)
}

func SemanticTokensRangeWithExtraDeclarations(parsed *core.ParsedDocument, r lsp.Range, extra map[string]string) lsp.SemanticTokens {
	return semanticTokens(parsed, &r, extra, true)
}

func semanticTokens(parsed *core.ParsedDocument, tokenRange *lsp.Range, extra map[string]string, includeJavaScript bool) lsp.SemanticTokens {
	doc := core.NewTextDocument(parsed.URI, "classic-asp", 0, parsed.Text)
	index := BuildSymbolIndex(parsed)
	params := semanticParameters(parsed)
	var classScopes classScopeIndex
	if symbolIndexNeedsClassScope(index) {
		classScopes = newClassScopeIndex(parsed.Text)
	}
	tokens := make([]semanticToken, 0, len(index.Occurrences))
	for _, region := range parsed.Regions {
		switch region.Kind {
		case core.RegionASPDirective:
			tokens = append(tokens, semanticTokenForRange(doc, region.Start, region.ContentStart, semanticKeyword, 0))
			tokens = append(tokens, semanticTokenForRange(doc, region.ContentEnd, region.End, semanticKeyword, 0))
			continue
		case core.RegionASPExpression:
			if region.ContentStart > region.Start {
				tokens = append(tokens, semanticTokenForRange(doc, region.ContentStart-1, region.ContentStart, semanticKeyword, 0))
			}
		}
		if region.Language == core.LanguageCSS {
			tokens = append(tokens, cssSemanticTokens(doc, parsed.Text, region)...)
			continue
		}
		if region.Language == core.LanguageJavaScript || region.Language == core.LanguageJScript {
			if includeJavaScript {
				tokens = append(tokens, javascriptSemanticTokens(doc, parsed.Text, region)...)
			}
			continue
		}
		if region.Language != core.LanguageVBScript {
			continue
		}
		text := parsed.Text[region.ContentStart:region.ContentEnd]
		spans := identifierSpans(text)
		for _, span := range spans {
			start := region.ContentStart + span.Start
			end := region.ContentStart + span.End
			name := parsed.Text[start:end]
			tokenType, modifiers, ok := classifyIdentifier(parsed.Text, start, name, index, params, extra, classScopes)
			if !ok {
				continue
			}
			position := doc.PositionAt(start)
			tokens = append(tokens, semanticToken{
				Line:      position.Line,
				Character: position.Character,
				Length:    end - start,
				Type:      tokenType,
				Modifiers: modifiers,
			})
		}
		for _, span := range operatorSpans(text) {
			start := region.ContentStart + span.Start
			position := doc.PositionAt(start)
			tokens = append(tokens, semanticToken{
				Line:      position.Line,
				Character: position.Character,
				Length:    span.End - span.Start,
				Type:      semanticOperator,
			})
		}
	}
	tokens = append(tokens, includeDirectiveSemanticTokens(doc, parsed.Text)...)
	tokens = append(tokens, sqlSemanticTokens(doc, parsed)...)
	if tokenRange != nil {
		tokens = filterSemanticTokens(tokens, *tokenRange)
	}
	return lsp.SemanticTokens{ResultID: "go-vbscript-0", Data: encodeSemanticTokens(tokens)}
}

func semanticTokenForRange(doc *core.TextDocument, start, end int, tokenType int, modifiers int) semanticToken {
	position := doc.PositionAt(start)
	return semanticToken{
		Line:      position.Line,
		Character: position.Character,
		Length:    end - start,
		Type:      tokenType,
		Modifiers: modifiers,
	}
}

func includeDirectiveSemanticTokens(doc *core.TextDocument, text string) []semanticToken {
	var tokens []semanticToken
	for _, match := range semanticIncludePattern.FindAllStringSubmatchIndex(text, -1) {
		if match[2] >= 0 {
			tokens = append(tokens, semanticTokenForRange(doc, match[2], match[3], semanticKeyword, 0))
		}
		if match[4] >= 0 {
			tokens = append(tokens, semanticTokenForRange(doc, match[4], match[5], semanticProperty, 0))
		}
		if match[6] >= 0 {
			tokens = append(tokens, semanticTokenForRange(doc, match[6], match[7], semanticString, 0))
		}
	}
	return tokens
}

func cssSemanticTokens(doc *core.TextDocument, text string, region core.Region) []semanticToken {
	content := text[region.ContentStart:region.ContentEnd]
	tokens := make([]semanticToken, 0, 4)
	depth := 0
	for i := 0; i < len(content); {
		switch content[i] {
		case '"', '\'':
			i = skipCSSString(content, i)
			continue
		case '/':
			if i+1 < len(content) && content[i+1] == '*' {
				i = skipCSSComment(content, i+2)
				continue
			}
		case '{':
			depth++
			i++
			continue
		case '}':
			if depth > 0 {
				depth--
			}
			i++
			continue
		}
		if (depth > 0 || region.Kind == core.RegionStyleAttribute) && isIdentifierStart(content[i]) {
			start := i
			i++
			for i < len(content) && isIdent(content[i]) {
				i++
			}
			cursor := i
			for cursor < len(content) && isCSSWhitespace(content[cursor]) {
				cursor++
			}
			if cursor < len(content) && content[cursor] == ':' {
				tokens = append(tokens, semanticTokenForRange(doc, region.ContentStart+start, region.ContentStart+i, semanticProperty, 0))
			}
			continue
		}
		i++
	}
	return tokens
}

func skipCSSString(text string, offset int) int {
	quote := text[offset]
	offset++
	for offset < len(text) {
		if text[offset] == '\\' {
			offset += 2
			continue
		}
		if text[offset] == quote {
			return offset + 1
		}
		offset++
	}
	return offset
}

func skipCSSComment(text string, offset int) int {
	for offset+1 < len(text) {
		if text[offset] == '*' && text[offset+1] == '/' {
			return offset + 2
		}
		offset++
	}
	return len(text)
}

func isCSSWhitespace(b byte) bool {
	return b == ' ' || b == '\t' || b == '\r' || b == '\n' || b == '\f'
}

func javascriptSemanticTokens(doc *core.TextDocument, text string, region core.Region) []semanticToken {
	content := text[region.ContentStart:region.ContentEnd]
	tokens := make([]semanticToken, 0, 16)
	for _, match := range jsClassDeclPattern.FindAllStringSubmatchIndex(content, -1) {
		if match[2] >= 0 {
			tokens = append(tokens, semanticTokenForRange(doc, region.ContentStart+match[2], region.ContentStart+match[3], semanticClass, 0))
		}
	}
	for _, match := range jsFunctionDeclPattern.FindAllStringSubmatchIndex(content, -1) {
		if match[4] >= 0 {
			tokens = append(tokens, javascriptParameterSemanticTokens(doc, content[match[4]:match[5]], region.ContentStart+match[4])...)
		}
	}
	for _, match := range jsMethodDeclPattern.FindAllStringSubmatchIndex(content, -1) {
		if match[2] >= 0 {
			tokens = append(tokens, semanticTokenForRange(doc, region.ContentStart+match[2], region.ContentStart+match[3], semanticMethod, 0))
		}
		if match[4] >= 0 {
			tokens = append(tokens, javascriptParameterSemanticTokens(doc, content[match[4]:match[5]], region.ContentStart+match[4])...)
		}
	}
	for _, match := range jsVariableDeclPattern.FindAllStringSubmatchIndex(content, -1) {
		if match[2] >= 0 {
			tokens = append(tokens, semanticTokenForRange(doc, region.ContentStart+match[2], region.ContentStart+match[3], semanticVariable, 0))
		}
	}
	for _, match := range jsMethodCallPattern.FindAllStringSubmatchIndex(content, -1) {
		if match[2] >= 0 {
			tokens = append(tokens, semanticTokenForRange(doc, region.ContentStart+match[2], region.ContentStart+match[3], semanticMethod, 0))
		}
	}
	for _, match := range jsPropertyPattern.FindAllStringSubmatchIndex(content, -1) {
		if match[2] >= 0 {
			if isJavaScriptCallAfter(content, match[3]) {
				continue
			}
			tokens = append(tokens, semanticTokenForRange(doc, region.ContentStart+match[2], region.ContentStart+match[3], semanticProperty, 0))
		}
	}
	return tokens
}

func isJavaScriptCallAfter(text string, offset int) bool {
	for offset < len(text) {
		switch text[offset] {
		case ' ', '\t', '\r', '\n':
			offset++
			continue
		case '(':
			return true
		default:
			return false
		}
	}
	return false
}

func javascriptParameterSemanticTokens(doc *core.TextDocument, params string, base int) []semanticToken {
	var tokens []semanticToken
	for i := 0; i < len(params); {
		if !isJSIdentifierStart(params[i]) {
			i++
			continue
		}
		start := i
		i++
		for i < len(params) && isJSIdentifier(params[i]) {
			i++
		}
		tokens = append(tokens, semanticTokenForRange(doc, base+start, base+i, semanticParameter, 0))
	}
	return tokens
}

func isJSIdentifierStart(b byte) bool {
	return b == '_' || b == '$' || b >= 'A' && b <= 'Z' || b >= 'a' && b <= 'z'
}

func isJSIdentifier(b byte) bool {
	return isJSIdentifierStart(b) || b >= '0' && b <= '9'
}

func filterSemanticTokens(tokens []semanticToken, r lsp.Range) []semanticToken {
	filtered := tokens[:0]
	for _, token := range tokens {
		if semanticTokenInRange(token, r) {
			filtered = append(filtered, token)
		}
	}
	return filtered
}

func semanticTokenInRange(token semanticToken, r lsp.Range) bool {
	if token.Line < r.Start.Line || token.Line == r.Start.Line && token.Character < r.Start.Character {
		return false
	}
	if token.Line > r.End.Line || token.Line == r.End.Line && token.Character >= r.End.Character {
		return false
	}
	return true
}

func semanticParameters(parsed *core.ParsedDocument) map[string]int {
	params := map[string]int{}
	for _, signature := range BuildSignatures(parsed) {
		for _, param := range signature.Parameters {
			modifier := semanticByref
			if strings.EqualFold(param.Mode, "ByVal") {
				modifier = semanticByval
			}
			params[strings.ToLower(param.Name)] = modifier
		}
	}
	return params
}

func classifyIdentifier(text string, start int, name string, index SymbolIndex, params map[string]int, extra map[string]string, classScopes classScopeIndex) (int, int, bool) {
	lower := strings.ToLower(name)
	if _, ok := semanticWordOperators[lower]; ok {
		return semanticOperator, 0, true
	}
	if _, ok := semanticKeywords[lower]; ok {
		return semanticKeyword, 0, true
	}
	if ownerStart, ownerEnd := memberOwnerBeforeOffset(text, start); ownerStart >= 0 {
		owner := strings.ToLower(text[ownerStart:ownerEnd])
		switch owner {
		case "response":
			if lower == "write" || lower == "redirect" || lower == "end" {
				return semanticMethod, semanticLibrary, true
			}
		case "err":
			switch lower {
			case "raise", "clear":
				return semanticMethod, semanticLibrary, true
			case "number", "description", "source":
				return semanticProperty, semanticLibrary, true
			}
		}
		if next := nextNonSpace(text, start+len(name)); next >= 0 && text[next] == '(' {
			return semanticMethod, 0, true
		}
		return semanticProperty, 0, true
	}
	if modifier, ok := params[lower]; ok {
		return semanticParameter, modifier, true
	}
	if _, ok := libraryFunctions[lower]; ok {
		return semanticFunction, semanticLibrary, true
	}
	if _, ok := libraryConstants[lower]; ok {
		return semanticConstant, semanticReadonly | semanticLibrary, true
	}
	if kind, ok := extra[lower]; ok {
		switch kind {
		case "class":
			return semanticClass, 0, true
		case "function", "sub":
			return semanticFunction, 0, true
		case "const":
			return semanticConstant, semanticReadonly, true
		default:
			return semanticVariable, 0, true
		}
	}
	if lower == "err" {
		return semanticConstant, semanticReadonly | semanticLibrary, true
	}
	if symbol, ok := index.Declarations[lower]; ok {
		switch symbol.Kind {
		case "class":
			return semanticClass, 0, true
		case "function", "sub":
			if classScopes.contains(start) {
				return semanticMethod, semanticVisibilityModifier(text, start), true
			}
			return semanticFunction, 0, true
		case "property":
			return semanticProperty, semanticVisibilityModifier(text, start), true
		case "const":
			return semanticConstant, semanticReadonly, true
		default:
			if classScopes.contains(start) {
				return semanticProperty, semanticVisibilityModifier(text, start), true
			}
			return semanticVariable, 0, true
		}
	}
	return semanticVariable, 0, true
}

func semanticVisibilityModifier(text string, start int) int {
	lineStart := strings.LastIndex(text[:start], "\n") + 1
	lineEnd := strings.Index(text[start:], "\n")
	if lineEnd < 0 {
		lineEnd = len(text)
	} else {
		lineEnd += start
	}
	line := strings.ToLower(text[lineStart:lineEnd])
	if strings.Contains(line, "private ") {
		return semanticPrivate
	}
	if strings.Contains(line, "public ") {
		return semanticPublic
	}
	return semanticPublic
}

func newClassScopeIndex(text string) classScopeIndex {
	lower := strings.ToLower(text)
	type match struct {
		end    int
		inside bool
	}
	matches := make([]match, 0, 4)
	for offset := 0; offset < len(lower); offset++ {
		if strings.HasPrefix(lower[offset:], "end class") {
			matches = append(matches, match{end: offset + len("end class")})
		}
		if strings.HasPrefix(lower[offset:], "class ") {
			matches = append(matches, match{end: offset + len("class "), inside: true})
		}
	}
	if len(matches) == 0 {
		return nil
	}

	transitions := make(classScopeIndex, 0, len(matches))
	lowerOffset := 0
	matchIndex := 0
	for originalOffset, r := range text {
		if originalOffset > 0 {
			for matchIndex < len(matches) && matches[matchIndex].end <= lowerOffset {
				transitions = append(transitions, classScopeTransition{offset: originalOffset, inside: matches[matchIndex].inside})
				matchIndex++
			}
		}
		lowerOffset += utf8.RuneLen(unicode.ToLower(r))
	}
	for matchIndex < len(matches) {
		transitions = append(transitions, classScopeTransition{offset: len(text), inside: matches[matchIndex].inside})
		matchIndex++
	}
	return transitions
}

func symbolIndexNeedsClassScope(index SymbolIndex) bool {
	for _, symbol := range index.Declarations {
		switch symbol.Kind {
		case "class", "const", "property":
			continue
		default:
			return true
		}
	}
	return false
}

func (index classScopeIndex) contains(offset int) bool {
	transition := sort.Search(len(index), func(i int) bool {
		return index[i].offset > offset
	})
	return transition > 0 && index[transition-1].inside
}

func operatorSpans(text string) []identifierSpan {
	spans := make([]identifierSpan, 0, 8)
	for i := 0; i < len(text); {
		switch text[i] {
		case '"':
			i++
			for i < len(text) {
				if text[i] == '"' {
					i++
					if i < len(text) && text[i] == '"' {
						i++
						continue
					}
					break
				}
				i++
			}
		case '\'':
			i = skipLine(text, i)
		case '#':
			if end, ok := skipDateLiteral(text, i); ok {
				i = end
				continue
			}
		case '(', ')':
			spans = append(spans, identifierSpan{Start: i, End: i + 1})
			i++
		case '<', '>':
			start := i
			i++
			if i < len(text) && (text[i] == '>' || text[i] == '=') {
				i++
			}
			spans = append(spans, identifierSpan{Start: start, End: i})
		case '+', '-', '*', '/', '\\', '&', '=', '^':
			spans = append(spans, identifierSpan{Start: i, End: i + 1})
			i++
		default:
			if end, ok := skipDecimalLiteral(text, i); ok {
				i = end
				continue
			}
			if end, ok := skipVBNumericLiteral(text, i); ok {
				i = end
				continue
			}
			i++
		}
	}
	return spans
}

func encodeSemanticTokens(tokens []semanticToken) []int {
	sort.Slice(tokens, func(i, j int) bool {
		if tokens[i].Line != tokens[j].Line {
			return tokens[i].Line < tokens[j].Line
		}
		if tokens[i].Character != tokens[j].Character {
			return tokens[i].Character < tokens[j].Character
		}
		return tokens[i].Length < tokens[j].Length
	})
	data := make([]int, 0, len(tokens)*5)
	prevLine := 0
	prevCharacter := 0
	hasPrevious := false
	for _, token := range tokens {
		if token.Length <= 0 {
			continue
		}
		deltaLine := token.Line
		deltaCharacter := token.Character
		if hasPrevious {
			deltaLine = token.Line - prevLine
			if deltaLine == 0 {
				deltaCharacter = token.Character - prevCharacter
			}
		}
		if deltaLine < 0 || deltaCharacter < 0 {
			continue
		}
		data = append(data, deltaLine, deltaCharacter, token.Length, token.Type, token.Modifiers)
		prevLine = token.Line
		prevCharacter = token.Character
		hasPrevious = true
	}
	return data
}
