package vbscript

import (
	"regexp"
	"sort"
	"strings"

	"github.com/yottonoko/classic-web-system-lsp/internal/core"
	"github.com/yottonoko/classic-web-system-lsp/internal/lsp"
)

type Symbol struct {
	Name  string
	Kind  string
	Range lsp.Range
}

// Occurrence is the compatibility view of a reference posting used by symbol consumers.
// The alias lets SymbolIndex share immutable posting slices with ReferenceShard.
type Occurrence = ReferencePosting

type SymbolIndex struct {
	Declarations map[string]Symbol
	Occurrences  map[string][]Occurrence
}

// EstimateBytes reports storage unique to the compatibility view. Its maps
// are immutable aliases of ReferenceShard maps, whose backing storage is
// charged by the shard owner.
func (index SymbolIndex) EstimateBytes() int64 {
	return 32
}

type identifierSpan struct {
	Start int
	End   int
}

var xmlDocCrefPattern = regexp.MustCompile(`(?i)\bcref\s*=\s*["']([A-Za-z_][A-Za-z0-9_]*(?:\.[A-Za-z_][A-Za-z0-9_]*)*)["']`)

func BuildSymbolIndex(parsed *core.ParsedDocument) SymbolIndex {
	if cached, ok := parsed.LoadRuntimeAnalysis("vbscript.symbol-index.runtime.v1"); ok {
		if index, valid := cached.(SymbolIndex); valid {
			return index
		}
	}
	shard := BuildReferenceShard(parsed)
	index := symbolIndexFromReferenceShard(shard)
	parsed.StoreRuntimeAnalysis("vbscript.symbol-index.runtime.v1", index)
	return index
}

func symbolIndexFromReferenceShard(shard *ReferenceShard) SymbolIndex {
	return SymbolIndex{Declarations: shard.Declarations, Occurrences: shard.Postings}
}

func cstDeclarationKinds(root *CSTNode) map[int]string {
	declarations := map[int]string{}
	var collect func(*CSTNode)
	collect = func(node *CSTNode) {
		if node.Kind == "VariableDeclaration" && node.NameToken != nil {
			declarations[node.NameToken.Start] = "variable"
		}
		for _, child := range node.Children {
			collect(child)
		}
	}
	collect(root)
	collectCSTParameterDeclarationKinds(declarations, significantTokens(root.Tokens))
	return declarations
}

func collectCSTParameterDeclarationKinds(declarations map[int]string, tokens []Token) {
	for _, statementStart := range cstStatementStartIndexes(tokens) {
		cursor := statementStart
		first := lowerCSTToken(tokens, cursor)
		for first == "public" || first == "private" || first == "default" {
			cursor++
			first = lowerCSTToken(tokens, cursor)
		}

		nameIndex := cursor + 1
		switch first {
		case "sub", "function":
		case "property":
			accessor := lowerCSTToken(tokens, cursor+1)
			if accessor != "get" && accessor != "let" && accessor != "set" {
				continue
			}
			nameIndex++
		default:
			continue
		}
		if tokenKindAt(tokens, nameIndex) != "identifier" {
			continue
		}

		statementEnd := cstStatementEndIndex(tokens, statementStart)
		open := nameIndex + 1
		if open >= statementEnd || tokens[open].Text != "(" {
			continue
		}
		depth := 1
		expectName := true
	parameterLoop:
		for index := open + 1; index < statementEnd; index++ {
			token := tokens[index]
			switch token.Text {
			case "(":
				depth++
				continue
			case ")":
				depth--
				if depth == 0 {
					break parameterLoop
				}
				continue
			case ",":
				if depth == 1 {
					expectName = true
				}
				continue
			}
			if depth != 1 || !expectName || strings.EqualFold(token.Text, "optional") || strings.EqualFold(token.Text, "paramarray") || strings.EqualFold(token.Text, "byval") || strings.EqualFold(token.Text, "byref") {
				continue
			}
			if token.Kind == "identifier" {
				declarations[token.Start] = "parameter"
				expectName = false
			}
		}
	}
}

func xmlDocCrefOccurrences(source *core.TextDocument, text string, regionStart int) []Occurrence {
	var occurrences []Occurrence
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
			lineEnd := skipLine(text, i)
			comment := text[i:lineEnd]
			for _, match := range xmlDocCrefPattern.FindAllStringSubmatchIndex(comment, -1) {
				if len(match) < 4 {
					continue
				}
				occurrences = append(occurrences, xmlDocCrefSegmentOccurrences(source, comment, regionStart+i, match[2], match[3])...)
			}
			i = lineEnd
		default:
			i++
		}
	}
	return occurrences
}

func xmlDocCrefSegmentOccurrences(source *core.TextDocument, comment string, commentStart int, valueStart int, valueEnd int) []Occurrence {
	value := comment[valueStart:valueEnd]
	parts := strings.Split(value, ".")
	occurrences := make([]Occurrence, 0, len(parts))
	cursor := valueStart
	for _, part := range parts {
		if part == "" {
			cursor++
			continue
		}
		start := commentStart + cursor
		end := start + len(part)
		occurrences = append(occurrences, Occurrence{
			Name:  part,
			Range: source.Range(start, end),
		})
		cursor += len(part) + 1
	}
	return occurrences
}

// DeclarationSymbols returns VBScript declarations in source order for outline-like consumers.
func DeclarationSymbols(parsed *core.ParsedDocument) []Symbol {
	signatures := Signatures(parsed)
	signatureByName := make(map[string]Signature, len(signatures))
	for _, signature := range signatures {
		signatureByName[strings.ToLower(signature.Name)] = signature
	}

	index := BuildSymbolIndex(parsed)
	symbols := make([]Symbol, 0, len(index.Declarations)+len(signatures))
	for _, symbol := range index.Declarations {
		lower := strings.ToLower(symbol.Name)
		if signature, ok := signatureByName[lower]; ok {
			symbol = Symbol{Name: signature.Name, Kind: signature.Kind, Range: signature.Range}
		}
		symbols = append(symbols, symbol)
	}

	seen := make(map[string]struct{}, len(symbols))
	for _, symbol := range symbols {
		seen[strings.ToLower(symbol.Name)] = struct{}{}
	}
	for _, signature := range signatures {
		lower := strings.ToLower(signature.Name)
		if _, ok := seen[lower]; ok {
			continue
		}
		seen[lower] = struct{}{}
		symbols = append(symbols, Symbol{Name: signature.Name, Kind: signature.Kind, Range: signature.Range})
	}

	sort.SliceStable(symbols, func(i, j int) bool {
		if symbols[i].Range.Start.Line != symbols[j].Range.Start.Line {
			return symbols[i].Range.Start.Line < symbols[j].Range.Start.Line
		}
		if symbols[i].Range.Start.Character != symbols[j].Range.Start.Character {
			return symbols[i].Range.Start.Character < symbols[j].Range.Start.Character
		}
		return strings.ToLower(symbols[i].Name) < strings.ToLower(symbols[j].Name)
	})
	return symbols
}

func Definition(parsed *core.ParsedDocument, position lsp.Position) []lsp.Location {
	doc := core.NewTextDocument(parsed.URI, "classic-asp", 0, parsed.Text)
	offset := doc.OffsetAt(position)
	word := WordAt(parsed.Text, offset)
	if word == "" {
		return nil
	}
	start, _ := identifierBoundsAt(parsed.Text, offset)
	if memberOwner(parsed.Text, start) != "" {
		return nil
	}
	index := BuildSymbolIndex(parsed)
	symbol, ok := index.Declarations[strings.ToLower(word)]
	if !ok {
		return nil
	}
	return []lsp.Location{{URI: parsed.URI, Range: symbol.Range}}
}

// ReferenceOptions controls which VBScript symbol occurrences are returned.
type ReferenceOptions struct {
	IncludeDeclaration               bool
	IncludeFunctionReturnAssignments bool
}

func References(parsed *core.ParsedDocument, position lsp.Position, includeDeclaration bool) []lsp.Location {
	return ReferencesWithOptions(parsed, position, ReferenceOptions{
		IncludeDeclaration:               includeDeclaration,
		IncludeFunctionReturnAssignments: true,
	})
}

func ReferencesWithOptions(parsed *core.ParsedDocument, position lsp.Position, options ReferenceOptions) []lsp.Location {
	doc := core.NewTextDocument(parsed.URI, "classic-asp", 0, parsed.Text)
	offset := doc.OffsetAt(position)
	word := WordAt(parsed.Text, offset)
	if word == "" {
		return nil
	}
	start, _ := identifierBoundsAt(parsed.Text, offset)
	queryOwner := memberOwner(parsed.Text, start)
	shard := BuildReferenceShard(parsed)
	lower := strings.ToLower(word)
	target, targetFound := shard.ReferenceTargetAt(lower, position)
	queryCref := false
	for _, posting := range shard.Postings[lower] {
		if posting.HasRole(ReferenceRoleCref) && positionInLSPRange(position, posting.Range) {
			queryCref = true
			break
		}
	}
	var locations []lsp.Location
	postings := shard.Postings[lower]
	globalResolutions := shard.GlobalResolutionsFor(lower)
	for index, posting := range postings {
		if targetFound && target.ProcedureLocal && !referencePostingMatchesProcedureLocalTarget(posting, target) ||
			(!targetFound || !target.ProcedureLocal) && queryOwner == "" && !globalResolutions[index] ||
			queryOwner != "" && !strings.EqualFold(posting.Owner, queryOwner) && !(queryCref && posting.HasRole(ReferenceRoleDeclaration)) {
			continue
		}
		if !options.IncludeDeclaration {
			if posting.HasRole(ReferenceRoleDeclaration) {
				continue
			}
		}
		if !options.IncludeFunctionReturnAssignments && posting.HasRole(ReferenceRoleFunctionReturn) {
			continue
		}
		locations = append(locations, lsp.Location{URI: parsed.URI, Range: posting.Range})
	}
	return locations
}

func referencePostingMatchesProcedureLocalTarget(posting ReferencePosting, target ReferenceTarget) bool {
	return posting.Owner == "" &&
		strings.EqualFold(posting.ClassOwner, target.ClassOwner) &&
		strings.EqualFold(posting.Scope, target.Scope) &&
		strings.EqualFold(posting.ScopeKind, target.ScopeKind)
}

func positionInLSPRange(position lsp.Position, r lsp.Range) bool {
	return (position.Line > r.Start.Line || position.Line == r.Start.Line && position.Character >= r.Start.Character) &&
		(position.Line < r.End.Line || position.Line == r.End.Line && position.Character <= r.End.Character)
}

func RenameRange(parsed *core.ParsedDocument, position lsp.Position) *lsp.Range {
	doc := core.NewTextDocument(parsed.URI, "classic-asp", 0, parsed.Text)
	offset := doc.OffsetAt(position)
	word := WordAt(parsed.Text, offset)
	if word == "" {
		return nil
	}
	start := offset
	for start > 0 && isIdent(parsed.Text[start-1]) {
		start--
	}
	end := start + len(word)
	r := doc.Range(start, end)
	return &r
}

func RenameEdit(parsed *core.ParsedDocument, position lsp.Position, newName string) map[string]any {
	if !isValidIdentifier(newName) {
		return map[string]any{"changes": map[string]any{}}
	}
	refs := References(parsed, position, true)
	if len(refs) == 0 {
		return map[string]any{"changes": map[string]any{}}
	}
	edits := make([]lsp.TextEdit, 0, len(refs))
	for _, ref := range refs {
		edits = append(edits, lsp.TextEdit{Range: ref.Range, NewText: newName})
	}
	return map[string]any{"changes": map[string]any{parsed.URI: edits}}
}

func Highlights(parsed *core.ParsedDocument, position lsp.Position) []lsp.DocumentHighlight {
	refs := References(parsed, position, true)
	highlights := make([]lsp.DocumentHighlight, 0, len(refs))
	for _, ref := range refs {
		highlights = append(highlights, lsp.DocumentHighlight{Range: ref.Range, Kind: 2})
	}
	return highlights
}

func identifierSpans(text string) []identifierSpan {
	spans := make([]identifierSpan, 0, 16)
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
		default:
			if end, ok := skipDecimalLiteral(text, i); ok {
				i = end
				continue
			}
			if end, ok := skipVBNumericLiteral(text, i); ok {
				i = end
				continue
			}
			if isIdentifierStart(text[i]) {
				start := i
				i++
				for i < len(text) && isIdent(text[i]) {
					i++
				}
				if isRemComment(text, start, i) {
					i = skipLine(text, i)
					continue
				}
				spans = append(spans, identifierSpan{Start: start, End: i})
				continue
			}
			i++
		}
	}
	return spans
}

func skipVBNumericLiteral(text string, offset int) (int, bool) {
	if offset >= len(text) {
		return offset, false
	}
	if text[offset] == '&' {
		cursor := offset + 1
		if cursor < len(text) && (text[cursor] == 'H' || text[cursor] == 'h') {
			cursor++
			start := cursor
			for cursor < len(text) && isASCIIHexDigit(text[cursor]) {
				cursor++
			}
			return cursor, cursor > start
		}
		if cursor < len(text) && (text[cursor] == 'O' || text[cursor] == 'o') {
			cursor++
		}
		start := cursor
		for cursor < len(text) && text[cursor] >= '0' && text[cursor] <= '7' {
			cursor++
		}
		return cursor, cursor > start
	}
	if text[offset] == '-' {
		return skipSignedDecimalLiteral(text, offset)
	}
	return offset, false
}

func isASCIIHexDigit(value byte) bool {
	return value >= '0' && value <= '9' || value >= 'A' && value <= 'F' || value >= 'a' && value <= 'f'
}

func skipLine(text string, offset int) int {
	for offset < len(text) && text[offset] != '\n' && text[offset] != '\r' {
		offset++
	}
	return offset
}

func isRemComment(text string, start, end int) bool {
	if !strings.EqualFold(text[start:end], "rem") {
		return false
	}
	for i := start - 1; i >= 0; i-- {
		switch text[i] {
		case ' ', '\t':
			continue
		case '\n', '\r', ':':
			return true
		default:
			return false
		}
	}
	return true
}

func declarationKindAt(text string, spans []identifierSpan, index int) (string, bool) {
	if index == 0 {
		return "", false
	}
	current := strings.ToLower(text[spans[index].Start:spans[index].End])
	previous := strings.ToLower(text[spans[index-1].Start:spans[index-1].End])
	if !sameStatement(text, spans[index-1].End, spans[index].Start) {
		return "", false
	}
	switch previous {
	case "sub", "function", "class", "const":
		return previous, true
	case "get", "let", "set":
		if index >= 2 && strings.EqualFold(text[spans[index-2].Start:spans[index-2].End], "property") {
			return "property", true
		}
	case "optional", "byval", "byref":
		return "parameter", true
	case "public", "private":
		if isDeclarationModifierTarget(current) {
			return "", false
		}
		return "variable", true
	}
	if previous == "dim" {
		return "variable", true
	}
	return "", false
}

func sameStatement(text string, start, end int) bool {
	if start > end {
		start, end = end, start
	}
	for i := start; i < end; i++ {
		switch text[i] {
		case '\n', '\r', ':':
			return false
		}
	}
	return true
}

func isDeclarationModifierTarget(value string) bool {
	switch value {
	case "sub", "function", "class", "const", "property", "default", "get", "let", "set":
		return true
	default:
		return false
	}
}

func isValidIdentifier(value string) bool {
	if value == "" || isVBKeyword(value) {
		return false
	}
	for i := 0; i < len(value); i++ {
		if i == 0 {
			if !(value[i] == '_' || value[i] >= 'A' && value[i] <= 'Z' || value[i] >= 'a' && value[i] <= 'z') {
				return false
			}
			continue
		}
		if !isIdent(value[i]) {
			return false
		}
	}
	return true
}

func isIdentifierStart(b byte) bool {
	return b == '_' || b >= 'A' && b <= 'Z' || b >= 'a' && b <= 'z'
}
