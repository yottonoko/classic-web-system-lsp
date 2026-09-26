package vbscript

import (
	"strings"
	"unsafe"

	"github.com/yottonoko/classic-web-system-lsp/internal/core"
	"github.com/yottonoko/classic-web-system-lsp/internal/lsp"
)

// SyntaxOptions controls optional VBScript syntax diagnostics.
type SyntaxOptions struct {
	IfSyntaxDiagnostics string
}

// SyntaxDiagnostics reports VBScript syntax diagnostics that do not require workspace context.
func SyntaxDiagnostics(parsed *core.ParsedDocument, options SyntaxOptions) []lsp.Diagnostic {
	ifLevel := options.IfSyntaxDiagnostics
	if ifLevel == "" {
		ifLevel = "basic"
	}
	doc := core.SourceDocument(parsed)
	if ifLevel == "off" {
		return blockEndSyntaxDiagnostics(parsed, doc, nil, false)
	}
	statements := vbStatements(parsed)
	diagnostics := make([]lsp.Diagnostic, 0)
	for _, statement := range statements {
		first := lowerTokenText(statement.Tokens, 0)
		switch first {
		case "if", "elseif":
			if diagnostic, ok := ifStatementSyntaxDiagnostic(doc, statement.Tokens, ifLevel); ok {
				diagnostics = append(diagnostics, diagnostic)
			}
		case "on":
			if diagnostic, ok := onErrorSyntaxDiagnostic(doc, statement.Tokens); ok {
				diagnostics = append(diagnostics, diagnostic)
			}
		}
	}
	diagnostics = append(diagnostics, blockEndSyntaxDiagnostics(parsed, doc, statements, true)...)
	return diagnostics
}

type vbStatement struct {
	Tokens      []Token
	Terminator  string
	RegionStart int
	RegionEnd   int
}

type openBlock struct {
	Kind          string
	ProcedureKind string
	Token         Token
}

// vbStatementsAnalysisKey caches the statement split for a revision.
const vbStatementsAnalysisKey = "vbscript.statements.runtime.v1"

// vbStatementsRuntime wraps a cached statement split with a precomputed
// memory-owner view. The generic accounting walk must never reflect over
// thousands of tokens on every cache estimate.
type vbStatementsRuntime struct {
	statements []vbStatement
	tokens     *vbscriptDocumentTokensRuntime
	bytes      int64
}

// SkipPreviousRuntimeInheritance marks statements as revision-specific. Token
// offsets and region boundaries belong to one exact parsed source.
func (*vbStatementsRuntime) SkipPreviousRuntimeInheritance() {}

// RuntimeAnalysisMemoryOwnerSet implements
// core.RuntimeAnalysisMemoryOwnerProvider without charging shared tokens twice.
func (v *vbStatementsRuntime) RuntimeAnalysisMemoryOwnerSet() []core.RuntimeAnalysisMemoryOwner {
	if v == nil {
		return nil
	}
	owners := []core.RuntimeAnalysisMemoryOwner{{Identity: v, Bytes: v.bytes}}
	if v.tokens != nil {
		owners = append(owners, core.RuntimeAnalysisMemoryOwner{Identity: v.tokens, Bytes: v.tokens.bytes})
	}
	return owners
}

func vbStatements(parsed *core.ParsedDocument) []vbStatement {
	if parsed == nil {
		return nil
	}
	if cached, ok := parsed.LoadRuntimeAnalysis(vbStatementsAnalysisKey); ok {
		if runtime, valid := cached.(*vbStatementsRuntime); valid && runtime != nil {
			return runtime.statements
		}
	}
	candidate := buildVBStatements(parsed)
	actual, _ := parsed.LoadOrStoreRuntimeAnalysis(vbStatementsAnalysisKey, candidate)
	if runtime, valid := actual.(*vbStatementsRuntime); valid && runtime != nil {
		return runtime.statements
	}
	return candidate.statements
}

func buildVBStatements(parsed *core.ParsedDocument) *vbStatementsRuntime {
	runtime := vbscriptDocumentTokensRuntimeFor(parsed)
	if runtime == nil {
		return nil
	}
	statements := make([]vbStatement, 0)
	for _, item := range runtime.regions {
		tokens := item.tokens
		start := 0
		for index, token := range tokens {
			if token.Kind != "newline" && token.Text != ":" {
				continue
			}
			if token.Kind == "newline" && continuedStatementBefore(tokens, index) {
				continue
			}
			if start < index {
				statements = append(statements, vbStatement{
					Tokens:      tokens[start:index:index],
					Terminator:  token.Text,
					RegionStart: item.start,
					RegionEnd:   item.end,
				})
			}
			start = index + 1
		}
		if start < len(tokens) {
			statements = append(statements, vbStatement{
				Tokens:      tokens[start:len(tokens):len(tokens)],
				RegionStart: item.start,
				RegionEnd:   item.end,
			})
		}
	}
	statements, mergedBytes := mergeAdjacentIslandIfThenStatements(statements)
	return &vbStatementsRuntime{
		statements: statements,
		tokens:     runtime,
		bytes: int64(unsafe.Sizeof(vbStatementsRuntime{})) +
			int64(cap(statements))*int64(unsafe.Sizeof(vbStatement{})) + mergedBytes,
	}
}

func mergeAdjacentIslandIfThenStatements(statements []vbStatement) ([]vbStatement, int64) {
	merged := statements[:0]
	var mergedBytes int64
	for index := 0; index < len(statements); index++ {
		statement := statements[index]
		if index+1 < len(statements) && canMergeIslandIfThen(statement, statements[index+1]) {
			next := statements[index+1]
			tokens := make([]Token, len(statement.Tokens)+len(next.Tokens))
			copy(tokens, statement.Tokens)
			copy(tokens[len(statement.Tokens):], next.Tokens)
			statement.Tokens = tokens
			mergedBytes += int64(cap(tokens)) * int64(unsafe.Sizeof(Token{}))
			statement.Terminator = next.Terminator
			statement.RegionEnd = next.RegionEnd
			index++
		}
		merged = append(merged, statement)
	}
	clear(statements[len(merged):])
	return merged, mergedBytes
}

func canMergeIslandIfThen(left vbStatement, right vbStatement) bool {
	if left.RegionEnd != right.RegionStart || len(left.Tokens) == 0 || len(right.Tokens) == 0 {
		return false
	}
	first := lowerTokenText(left.Tokens, 0)
	return (first == "if" || first == "elseif") &&
		topLevelKeywordIndex(left.Tokens, "then") == -1 &&
		lowerTokenText(right.Tokens, 0) == "then"
}

func continuedStatementBefore(tokens []Token, newlineIndex int) bool {
	if newlineIndex <= 0 {
		return false
	}
	return tokens[newlineIndex-1].Text == "_"
}

func blockEndSyntaxDiagnostics(parsed *core.ParsedDocument, doc *core.TextDocument, statements []vbStatement, includeIf bool) []lsp.Diagnostic {
	if statements == nil {
		statements = vbStatements(parsed)
	}
	stack := make([]openBlock, 0)
	for _, statement := range statements {
		if len(statement.Tokens) == 0 {
			continue
		}
		first := lowerTokenText(statement.Tokens, 0)
		second := lowerTokenText(statement.Tokens, 1)
		switch {
		case first == "class" && tokenKindAt(statement.Tokens, 1) == "identifier":
			stack = append(stack, openBlock{Kind: "Class", Token: statement.Tokens[0]})
		case first == "sub" && tokenKindAt(statement.Tokens, 1) == "identifier":
			stack = append(stack, openBlock{Kind: "Procedure", ProcedureKind: "sub", Token: statement.Tokens[0]})
		case first == "function" && tokenKindAt(statement.Tokens, 1) == "identifier":
			stack = append(stack, openBlock{Kind: "Procedure", ProcedureKind: "function", Token: statement.Tokens[0]})
		case (first == "public" || first == "private") && second == "sub" && tokenKindAt(statement.Tokens, 2) == "identifier":
			stack = append(stack, openBlock{Kind: "Procedure", ProcedureKind: "sub", Token: statement.Tokens[0]})
		case (first == "public" || first == "private") && second == "function" && tokenKindAt(statement.Tokens, 2) == "identifier":
			stack = append(stack, openBlock{Kind: "Procedure", ProcedureKind: "function", Token: statement.Tokens[0]})
		case first == "property":
			stack = append(stack, openBlock{Kind: "Property", Token: statement.Tokens[0]})
		case (first == "public" || first == "private") && second == "property":
			stack = append(stack, openBlock{Kind: "Property", Token: statement.Tokens[0]})
		case first == "select" && second == "case":
			stack = append(stack, openBlock{Kind: "Select", Token: statement.Tokens[0]})
		case first == "with":
			stack = append(stack, openBlock{Kind: "With", Token: statement.Tokens[0]})
		case first == "do":
			stack = append(stack, openBlock{Kind: "DoLoop", Token: statement.Tokens[0]})
		case first == "while":
			stack = append(stack, openBlock{Kind: "While", Token: statement.Tokens[0]})
		case first == "for" && second == "each":
			stack = append(stack, openBlock{Kind: "ForEach", Token: statement.Tokens[0]})
		case first == "for":
			stack = append(stack, openBlock{Kind: "For", Token: statement.Tokens[0]})
		case first == "if" && validMultilineIfStatement(statement):
			stack = append(stack, openBlock{Kind: "If", Token: statement.Tokens[0]})
		case first == "end":
			stack = closeSyntaxBlock(stack, endBlockKind(second))
		case first == "loop":
			stack = closeSyntaxBlock(stack, "DoLoop")
		case first == "wend":
			stack = closeSyntaxBlock(stack, "While")
		case first == "next":
			stack = closeSyntaxBlock(stack, "For", "ForEach")
		}
	}
	diagnostics := make([]lsp.Diagnostic, 0, len(stack))
	for _, block := range stack {
		if block.Kind == "If" && !includeIf {
			continue
		}
		code := blockEndSyntaxDiagnosticCode(block)
		if code == "" {
			continue
		}
		diagnostics = append(diagnostics, lsp.Diagnostic{
			Range:    doc.Range(block.Token.Start, block.Token.End),
			Severity: lsp.DiagnosticSeverityError,
			Source:   "asp-lsp-vbscript-syntax",
			Code:     code,
			Message:  code,
			Data: map[string]any{
				"kind":       blockDisplayName(block),
				"terminator": blockTerminator(block),
			},
		})
	}
	return diagnostics
}

func blockDisplayName(block openBlock) string {
	switch block.Kind {
	case "Procedure":
		if block.ProcedureKind == "function" {
			return "Function"
		}
		return "Sub"
	case "Select":
		return "Select Case"
	case "DoLoop":
		return "Do"
	case "ForEach":
		return "For Each"
	default:
		return block.Kind
	}
}

func blockTerminator(block openBlock) string {
	switch block.Kind {
	case "Class":
		return "End Class"
	case "Procedure":
		if block.ProcedureKind == "function" {
			return "End Function"
		}
		return "End Sub"
	case "Property":
		return "End Property"
	case "Select":
		return "End Select"
	case "With":
		return "End With"
	case "DoLoop":
		return "Loop"
	case "While":
		return "Wend"
	case "For", "ForEach":
		return "Next"
	case "If":
		return "End If"
	default:
		return ""
	}
}

func ifStatementSyntaxDiagnostic(doc *core.TextDocument, tokens []Token, level string) (lsp.Diagnostic, bool) {
	thenIndex := topLevelKeywordIndex(tokens, "then")
	anyThenIndex := thenIndex
	if anyThenIndex == -1 {
		anyThenIndex = keywordIndex(tokens, "then")
	}
	if anyThenIndex == -1 {
		return syntaxDiagnostic(doc, tokens[0].Start, tokens[len(tokens)-1].End, "missingThen"), true
	}
	condition := tokens[1:anyThenIndex]
	if len(condition) == 0 {
		return syntaxDiagnostic(doc, tokens[0].Start, tokens[anyThenIndex].End, "missingIfCondition"), true
	}
	if !validIfConditionShape(condition, level) {
		return syntaxDiagnostic(doc, condition[0].Start, condition[len(condition)-1].End, "invalidIfCondition"), true
	}
	return lsp.Diagnostic{}, false
}

func onErrorSyntaxDiagnostic(doc *core.TextDocument, tokens []Token) (lsp.Diagnostic, bool) {
	if lowerTokenText(tokens, 1) != "error" || isValidOnErrorStatement(tokens) {
		return lsp.Diagnostic{}, false
	}
	return syntaxDiagnostic(doc, tokens[0].Start, tokens[len(tokens)-1].End, "invalidOnErrorStatement"), true
}

func isValidOnErrorStatement(tokens []Token) bool {
	if len(tokens) != 4 || lowerTokenText(tokens, 0) != "on" || lowerTokenText(tokens, 1) != "error" {
		return false
	}
	return lowerTokenText(tokens, 2) == "resume" && lowerTokenText(tokens, 3) == "next" ||
		lowerTokenText(tokens, 2) == "goto" && tokens[3].Kind == "number" && tokens[3].Text == "0"
}

func syntaxDiagnostic(doc *core.TextDocument, start int, end int, code string) lsp.Diagnostic {
	return lsp.Diagnostic{
		Range:    doc.Range(start, end),
		Severity: lsp.DiagnosticSeverityError,
		Source:   "asp-lsp-vbscript-syntax",
		Code:     code,
		Message:  code,
	}
}

func validMultilineIfStatement(statement vbStatement) bool {
	if statement.Terminator == ":" {
		return false
	}
	tokens := statement.Tokens
	thenIndex := topLevelKeywordIndex(tokens, "then")
	if thenIndex == -1 || thenIndex != len(tokens)-1 {
		return false
	}
	condition := tokens[1:thenIndex]
	return len(condition) > 0 && validIfConditionShape(condition, "strict")
}

func validIfConditionShape(tokens []Token, level string) bool {
	if !balancedParentheses(tokens) {
		return false
	}
	if level != "strict" {
		return true
	}
	first := strings.ToLower(tokens[0].Text)
	last := strings.ToLower(tokens[len(tokens)-1].Text)
	return !invalidIfConditionStart(first) && !expressionTrailingOperator(last)
}

func balancedParentheses(tokens []Token) bool {
	depth := 0
	for _, token := range tokens {
		switch token.Text {
		case "(":
			depth++
		case ")":
			depth--
			if depth < 0 {
				return false
			}
		}
	}
	return depth == 0
}

func invalidIfConditionStart(value string) bool {
	switch value {
	case "case", "class", "const", "dim", "do", "else", "elseif", "end", "for", "function", "loop", "next", "private", "property", "public", "select", "set", "sub", "wend", "while", "with":
		return true
	default:
		return false
	}
}

func expressionTrailingOperator(value string) bool {
	switch value {
	case "and", "eqv", "imp", "is", "mod", "not", "or", "xor", "=", "<>", "<", ">", "<=", ">=", "&", "+", "-", "*", "/", "\\", "^":
		return true
	default:
		return false
	}
}

func keywordIndex(tokens []Token, keyword string) int {
	for index, token := range tokens {
		if strings.EqualFold(token.Text, keyword) {
			return index
		}
	}
	return -1
}

func topLevelKeywordIndex(tokens []Token, keyword string) int {
	depth := 0
	for index, token := range tokens {
		switch token.Text {
		case "(":
			depth++
		case ")":
			if depth > 0 {
				depth--
			}
		default:
			if depth == 0 && strings.EqualFold(token.Text, keyword) {
				return index
			}
		}
	}
	return -1
}

func lowerTokenText(tokens []Token, index int) string {
	if index < 0 || index >= len(tokens) {
		return ""
	}
	return strings.ToLower(tokens[index].Text)
}

func closeSyntaxBlock(stack []openBlock, kinds ...string) []openBlock {
	for index := len(stack) - 1; index >= 0; index-- {
		for _, kind := range kinds {
			if kind != "" && stack[index].Kind == kind {
				return append(stack[:index], stack[index+1:]...)
			}
		}
	}
	return stack
}

func endBlockKind(second string) string {
	switch second {
	case "class":
		return "Class"
	case "sub", "function":
		return "Procedure"
	case "property":
		return "Property"
	case "select":
		return "Select"
	case "with":
		return "With"
	case "if":
		return "If"
	default:
		return ""
	}
}

func blockEndSyntaxDiagnosticCode(block openBlock) string {
	switch block.Kind {
	case "Class":
		return "missingEndClass"
	case "Procedure":
		if block.ProcedureKind == "function" {
			return "missingEndFunction"
		}
		return "missingEndSub"
	case "Property":
		return "missingEndProperty"
	case "Select":
		return "missingEndSelect"
	case "With":
		return "missingEndWith"
	case "DoLoop":
		return "missingLoop"
	case "While":
		return "missingWend"
	case "For", "ForEach":
		return "missingNext"
	case "If":
		return "missingEndIf"
	default:
		return ""
	}
}
