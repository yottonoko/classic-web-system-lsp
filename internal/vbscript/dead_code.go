package vbscript

import (
	"strconv"

	"github.com/yottonoko/classic-web-system-lsp/internal/core"
	"github.com/yottonoko/classic-web-system-lsp/internal/lsp"
)

// DeadCodeDiagnostics reports VBScript statements that cannot be reached after terminal flow statements.
func DeadCodeDiagnostics(parsed *core.ParsedDocument) []lsp.Diagnostic {
	statements := vbStatements(parsed)
	if len(statements) == 0 {
		return nil
	}
	blocks := deadCodeBlocks(statements, len(parsed.Text))
	doc := core.SourceDocument(parsed)
	var diagnostics []lsp.Diagnostic
	reported := map[string]struct{}{}
	var unreachable *deadCodeUnreachableRange
	for _, statement := range statements {
		if len(statement.Tokens) == 0 {
			continue
		}
		first := statement.Tokens[0]
		last := statement.Tokens[len(statement.Tokens)-1]
		if unreachable != nil && first.Start >= unreachable.Until {
			unreachable = nil
		}
		if unreachable != nil && unreachable.ResetAt >= 0 && first.Start >= unreachable.ResetAt {
			unreachable = nil
		}
		isUnreachable := unreachable != nil && first.Start < unreachable.Until
		if isUnreachable {
			key := strconv.Itoa(first.Start) + ":" + strconv.Itoa(last.End)
			if _, ok := reported[key]; !ok {
				reported[key] = struct{}{}
				diagnostics = append(diagnostics, lsp.Diagnostic{
					Range:    doc.Range(first.Start, last.End),
					Severity: lsp.DiagnosticSeverityHint,
					Source:   "asp-lsp-vbscript-dead-code",
					Code:     "unreachableCode",
					Message:  "This VBScript code is unreachable.",
					Tags:     []lsp.DiagnosticTag{lsp.DiagnosticTagUnnecessary},
				})
			}
		}
		if terminalUntil, ok := deadCodeTerminalEnd(parsed, statement, blocks); ok && !isUnreachable {
			unreachable = &deadCodeUnreachableRange{
				Until:   terminalUntil,
				ResetAt: deadCodeConditionalBoundaryStart(statement, statements, blocks, terminalUntil),
			}
		}
	}
	return diagnostics
}

type deadCodeUnreachableRange struct {
	Until   int
	ResetAt int
}

type deadCodeBlock struct {
	Kind          string
	ProcedureKind string
	Start         int
	End           int
}

func deadCodeBlocks(statements []vbStatement, documentEnd int) []deadCodeBlock {
	var blocks []deadCodeBlock
	var stack []deadCodeBlock
	for _, statement := range statements {
		if len(statement.Tokens) == 0 {
			continue
		}
		first := lowerTokenText(statement.Tokens, 0)
		second := lowerTokenText(statement.Tokens, 1)
		start := statement.Tokens[0].Start
		end := statement.Tokens[len(statement.Tokens)-1].End
		switch {
		case first == "class" && tokenKindAt(statement.Tokens, 1) == "identifier":
			stack = append(stack, deadCodeBlock{Kind: "Class", Start: start})
		case first == "sub" && tokenKindAt(statement.Tokens, 1) == "identifier":
			stack = append(stack, deadCodeBlock{Kind: "Procedure", ProcedureKind: "sub", Start: start})
		case first == "function" && tokenKindAt(statement.Tokens, 1) == "identifier":
			stack = append(stack, deadCodeBlock{Kind: "Procedure", ProcedureKind: "function", Start: start})
		case (first == "public" || first == "private") && second == "sub" && tokenKindAt(statement.Tokens, 2) == "identifier":
			stack = append(stack, deadCodeBlock{Kind: "Procedure", ProcedureKind: "sub", Start: start})
		case (first == "public" || first == "private") && second == "function" && tokenKindAt(statement.Tokens, 2) == "identifier":
			stack = append(stack, deadCodeBlock{Kind: "Procedure", ProcedureKind: "function", Start: start})
		case first == "property" || ((first == "public" || first == "private") && second == "property"):
			stack = append(stack, deadCodeBlock{Kind: "Property", Start: start})
		case first == "select" && second == "case":
			stack = append(stack, deadCodeBlock{Kind: "Select", Start: start})
		case first == "do":
			stack = append(stack, deadCodeBlock{Kind: "DoLoop", Start: start})
		case first == "while":
			stack = append(stack, deadCodeBlock{Kind: "While", Start: start})
		case first == "for" && second == "each":
			stack = append(stack, deadCodeBlock{Kind: "ForEach", Start: start})
		case first == "for":
			stack = append(stack, deadCodeBlock{Kind: "For", Start: start})
		case first == "if" && validMultilineIfStatement(statement):
			stack = append(stack, deadCodeBlock{Kind: "If", Start: start})
		case first == "end":
			stack, blocks = closeDeadCodeBlock(stack, blocks, endBlockKind(second), end)
		case first == "loop":
			stack, blocks = closeDeadCodeBlock(stack, blocks, "DoLoop", end)
		case first == "wend":
			stack, blocks = closeDeadCodeBlock(stack, blocks, "While", end)
		case first == "next":
			stack, blocks = closeDeadCodeBlock(stack, blocks, "For", end)
			stack, blocks = closeDeadCodeBlock(stack, blocks, "ForEach", end)
		}
	}
	for i := len(stack) - 1; i >= 0; i-- {
		block := stack[i]
		block.End = documentEnd
		blocks = append(blocks, block)
	}
	return blocks
}

func closeDeadCodeBlock(stack []deadCodeBlock, blocks []deadCodeBlock, kind string, end int) ([]deadCodeBlock, []deadCodeBlock) {
	if kind == "" {
		return stack, blocks
	}
	for index := len(stack) - 1; index >= 0; index-- {
		if stack[index].Kind != kind {
			continue
		}
		block := stack[index]
		block.End = end
		blocks = append(blocks, block)
		stack = append(stack[:index], stack[index+1:]...)
		return stack, blocks
	}
	return stack, blocks
}

func deadCodeTerminalEnd(parsed *core.ParsedDocument, statement vbStatement, blocks []deadCodeBlock) (int, bool) {
	first := lowerTokenText(statement.Tokens, 0)
	second := lowerTokenText(statement.Tokens, 1)
	if first == "" || isSingleLineConditionalStatement(statement) {
		return 0, false
	}
	start := statement.Tokens[0].Start
	switch first {
	case "exit":
		switch second {
		case "sub", "function":
			if block, ok := innermostDeadCodeBlock(blocks, start, "Procedure"); ok && block.ProcedureKind == second {
				return block.End, true
			}
		case "property":
			if block, ok := innermostDeadCodeBlock(blocks, start, "Property"); ok {
				return block.End, true
			}
		case "for":
			if block, ok := innermostDeadCodeBlock(blocks, start, "For", "ForEach"); ok {
				return block.End, true
			}
		case "do":
			if block, ok := innermostDeadCodeBlock(blocks, start, "DoLoop"); ok {
				return block.End, true
			}
		}
	case "end":
		if second == "" {
			if block, ok := innermostDeadCodeBlock(blocks, start, "Procedure", "Property"); ok {
				return block.End, true
			}
			return len(parsed.Text), true
		}
	}
	return 0, false
}

func isSingleLineConditionalStatement(statement vbStatement) bool {
	first := lowerTokenText(statement.Tokens, 0)
	if first != "if" && first != "elseif" {
		return false
	}
	thenIndex := topLevelKeywordIndex(statement.Tokens, "then")
	return thenIndex != -1 && thenIndex < len(statement.Tokens)-1
}

func deadCodeConditionalBoundaryStart(statement vbStatement, statements []vbStatement, blocks []deadCodeBlock, terminalUntil int) int {
	if len(statement.Tokens) == 0 {
		return -1
	}
	first := statement.Tokens[0]
	last := statement.Tokens[len(statement.Tokens)-1]
	branch, ok := innermostDeadCodeBlock(blocks, first.Start, "If", "Select")
	if !ok || branch.End > terminalUntil {
		return -1
	}
	for _, candidate := range statements {
		if len(candidate.Tokens) == 0 {
			continue
		}
		start := candidate.Tokens[0].Start
		if start <= last.End || start >= branch.End {
			continue
		}
		if isDeadCodeBranchBoundary(branch.Kind, candidate) {
			return start
		}
	}
	return -1
}

func isDeadCodeBranchBoundary(kind string, statement vbStatement) bool {
	first := lowerTokenText(statement.Tokens, 0)
	second := lowerTokenText(statement.Tokens, 1)
	if kind == "If" {
		return first == "else" || first == "elseif" || first == "end" && second == "if"
	}
	if kind == "Select" {
		return first == "case" || first == "end" && second == "select"
	}
	return false
}

func innermostDeadCodeBlock(blocks []deadCodeBlock, offset int, kinds ...string) (deadCodeBlock, bool) {
	var best deadCodeBlock
	found := false
	for _, block := range blocks {
		if !deadCodeKindMatches(block.Kind, kinds) || block.Start > offset || offset >= block.End {
			continue
		}
		if !found || block.End-block.Start < best.End-best.Start {
			best = block
			found = true
		}
	}
	return best, found
}

func deadCodeKindMatches(kind string, candidates []string) bool {
	for _, candidate := range candidates {
		if kind == candidate {
			return true
		}
	}
	return false
}
