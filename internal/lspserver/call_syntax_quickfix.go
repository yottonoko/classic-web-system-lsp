package lspserver

import (
	"strings"

	"github.com/yottonoko/classic-web-system-lsp/internal/core"
	"github.com/yottonoko/classic-web-system-lsp/internal/lsp"
	"github.com/yottonoko/classic-web-system-lsp/internal/vbscript"
)

type callSyntaxFix struct {
	Range      lsp.Range
	Start      int
	End        int
	NewText    string
	Diagnostic lsp.Diagnostic
}

func (s *Server) callSyntaxCodeActions(params codeActionParams) []lsp.CodeAction {
	_, parsed := s.parsed(params.TextDocument.URI)
	if parsed == nil {
		return nil
	}
	fixes := callSyntaxFixes(parsed)
	actions := make([]lsp.CodeAction, 0, len(fixes))
	for _, fix := range fixes {
		actions = append(actions, lsp.CodeAction{
			Title:       "Fix VBScript call syntax",
			Kind:        "quickfix",
			Diagnostics: []lsp.Diagnostic{fix.Diagnostic},
			Edit: &lsp.WorkspaceEdit{Changes: map[string][]lsp.TextEdit{
				params.TextDocument.URI: {{Range: fix.Range, NewText: fix.NewText}},
			}},
		})
	}
	return actions
}

func callSyntaxDiagnostics(parsed *core.ParsedDocument) []lsp.Diagnostic {
	fixes := callSyntaxFixes(parsed)
	diagnostics := make([]lsp.Diagnostic, 0, len(fixes))
	for _, fix := range fixes {
		diagnostics = append(diagnostics, fix.Diagnostic)
	}
	return diagnostics
}

func callSyntaxFixes(parsed *core.ParsedDocument) []callSyntaxFix {
	doc := core.SourceDocument(parsed)
	signatures := vbscript.BuildSignatures(parsed)
	var fixes []callSyntaxFix
	for _, region := range parsed.Regions {
		if region.Language != core.LanguageVBScript {
			continue
		}
		for lineStart := region.ContentStart; lineStart < region.ContentEnd; {
			lineEnd := lineStart
			for lineEnd < region.ContentEnd && parsed.Text[lineEnd] != '\n' && parsed.Text[lineEnd] != '\r' {
				lineEnd++
			}
			if fix, ok := callSyntaxFixForLine(parsed, doc, parsed.Text[lineStart:lineEnd], lineStart, signatures); ok {
				fixes = append(fixes, fix)
			}
			if lineEnd >= region.ContentEnd {
				break
			}
			lineStart = lineEnd + 1
			if parsed.Text[lineEnd] == '\r' && lineStart < region.ContentEnd && parsed.Text[lineStart] == '\n' {
				lineStart++
			}
		}
	}
	return fixes
}

func callSyntaxFixForLine(parsed *core.ParsedDocument, doc *core.TextDocument, line string, lineOffset int, signatures map[string]vbscript.Signature) (callSyntaxFix, bool) {
	trimmedStart := leadingVBWhitespace(line)
	if trimmedStart >= len(line) || line[trimmedStart] == '\'' {
		return callSyntaxFix{}, false
	}
	trimmed := line[trimmedStart:]
	if strings.HasPrefix(strings.ToLower(trimmed), "call ") {
		return callStatementFix(doc, line, lineOffset, trimmedStart, signatures)
	}
	if fix, ok := expressionCallFix(parsed, doc, line, lineOffset, signatures); ok {
		return fix, true
	}
	return parenthesizedStatementCallFix(doc, line, lineOffset, trimmedStart, signatures)
}

func callStatementFix(doc *core.TextDocument, line string, lineOffset int, keywordStart int, signatures map[string]vbscript.Signature) (callSyntaxFix, bool) {
	afterCall := keywordStart + len("Call")
	for afterCall < len(line) && isVBWhitespace(line[afterCall]) {
		afterCall++
	}
	nameStart := afterCall
	nameEnd := readVBIdentifier(line, nameStart)
	if nameEnd == nameStart {
		return callSyntaxFix{}, false
	}
	name := line[nameStart:nameEnd]
	if _, ok := signatures[strings.ToLower(name)]; !ok {
		return callSyntaxFix{}, false
	}
	args := strings.TrimSpace(line[nameEnd:])
	if args == "" || strings.HasPrefix(args, "(") {
		return callSyntaxFix{}, false
	}
	return makeCallSyntaxFix(doc, lineOffset, line, "callStatementRequiresParentheses", line[:keywordStart]+"Call "+name+"("+args+")", name), true
}

func expressionCallFix(parsed *core.ParsedDocument, doc *core.TextDocument, line string, lineOffset int, signatures map[string]vbscript.Signature) (callSyntaxFix, bool) {
	equal := topLevelEqual(line)
	if equal < 0 {
		return callSyntaxFix{}, false
	}
	cursor := equal + 1
	for cursor < len(line) && isVBWhitespace(line[cursor]) {
		cursor++
	}
	nameStart := cursor
	nameEnd := readVBIdentifier(line, nameStart)
	if nameEnd == nameStart {
		return callSyntaxFix{}, false
	}
	name := line[nameStart:nameEnd]
	if _, ok := signatures[strings.ToLower(name)]; !ok {
		return callSyntaxFix{}, false
	}
	if vbscript.IsReturnValueSlot(parsed, doc.PositionAt(lineOffset+nameStart)) {
		return callSyntaxFix{}, false
	}
	args := strings.TrimSpace(line[nameEnd:])
	if args == "" || strings.HasPrefix(args, "(") {
		return callSyntaxFix{}, false
	}
	return makeCallSyntaxFix(doc, lineOffset, line, "expressionCallRequiresParentheses", line[:nameStart]+name+"("+args+")", name), true
}

func parenthesizedStatementCallFix(doc *core.TextDocument, line string, lineOffset int, trimmedStart int, signatures map[string]vbscript.Signature) (callSyntaxFix, bool) {
	nameStart := trimmedStart
	nameEnd := readVBIdentifier(line, nameStart)
	if nameEnd == nameStart {
		return callSyntaxFix{}, false
	}
	name := line[nameStart:nameEnd]
	signature, ok := signatures[strings.ToLower(name)]
	if !ok || signature.Kind != "sub" {
		return callSyntaxFix{}, false
	}
	rest := strings.TrimSpace(line[nameEnd:])
	if !strings.HasPrefix(rest, "(") || !strings.HasSuffix(rest, ")") {
		return callSyntaxFix{}, false
	}
	args := strings.TrimSpace(rest[1 : len(rest)-1])
	return makeCallSyntaxFix(doc, lineOffset, line, "statementCallDisallowsParenthesizedArguments", line[:nameStart]+name+" "+args, name), true
}

func makeCallSyntaxFix(doc *core.TextDocument, lineOffset int, line string, code string, newText string, name string) callSyntaxFix {
	r := doc.Range(lineOffset, lineOffset+len(line))
	return callSyntaxFix{
		Range:   r,
		Start:   lineOffset,
		End:     lineOffset + len(line),
		NewText: newText,
		Diagnostic: lsp.Diagnostic{
			Range:    r,
			Severity: lsp.DiagnosticSeverityError,
			Code:     code,
			Source:   "asp-lsp-vbscript-syntax",
			Message:  "VBScript call syntax is invalid for '" + name + "'.",
			Data:     map[string]any{"fixKind": "vbscriptCallSyntax", "name": name, "newText": newText},
		},
	}
}

func readVBIdentifier(text string, offset int) int {
	if offset >= len(text) || !isVBIdentifierStart(text[offset]) {
		return offset
	}
	offset++
	for offset < len(text) && isVBIdentifier(text[offset]) {
		offset++
	}
	return offset
}

func isVBIdentifierStart(b byte) bool {
	return b == '_' || b >= 'A' && b <= 'Z' || b >= 'a' && b <= 'z'
}

func isVBIdentifier(b byte) bool {
	return isVBIdentifierStart(b) || b >= '0' && b <= '9'
}
