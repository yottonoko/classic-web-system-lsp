package lspserver

import (
	"strings"

	"github.com/yottonoko/classic-web-system-lsp/internal/core"
	"github.com/yottonoko/classic-web-system-lsp/internal/lsp"
)

type dimDeclarationFix struct {
	Title      string
	Range      lsp.Range
	Start      int
	End        int
	NewText    string
	Diagnostic *lsp.Diagnostic
}

func codeActionAllows(only []string, kind string) bool {
	if len(only) == 0 {
		return true
	}
	for _, requested := range only {
		if requested == kind || strings.HasPrefix(kind, requested+".") || strings.HasPrefix(requested, kind+".") {
			return true
		}
	}
	return false
}

func (s *Server) dimDeclarationCodeActions(params codeActionParams, selectionStart, selectionEnd int) []lsp.CodeAction {
	_, parsed := s.parsed(params.TextDocument.URI)
	if parsed == nil {
		return nil
	}
	fixes := dimDeclarationFixes(parsed, s.settings.InitializedDimQuickFixStyle)
	actions := make([]lsp.CodeAction, 0, len(fixes))
	for _, fix := range fixes {
		if selectionEnd <= fix.Start || selectionStart >= fix.End {
			continue
		}
		action := lsp.CodeAction{
			Title: s.dimQuickFixTitle(fix.Title),
			Kind:  "quickfix",
			Edit: &lsp.WorkspaceEdit{Changes: map[string][]lsp.TextEdit{
				params.TextDocument.URI: {{Range: fix.Range, NewText: fix.NewText}},
			}},
		}
		if fix.Diagnostic != nil {
			action.Diagnostics = []lsp.Diagnostic{*fix.Diagnostic}
		}
		actions = append(actions, action)
	}
	return actions
}

func declarationSyntaxDiagnostics(parsed *core.ParsedDocument) []lsp.Diagnostic {
	fixes := dimDeclarationFixes(parsed, "")
	diagnostics := make([]lsp.Diagnostic, 0, len(fixes))
	for _, fix := range fixes {
		if fix.Diagnostic != nil {
			diagnostics = append(diagnostics, *fix.Diagnostic)
		}
	}
	diagnostics = append(diagnostics, unsupportedDeclarationDiagnostics(parsed)...)
	return diagnostics
}

func unsupportedDeclarationDiagnostics(parsed *core.ParsedDocument) []lsp.Diagnostic {
	doc := core.SourceDocument(parsed)
	var diagnostics []lsp.Diagnostic
	for _, region := range parsed.Regions {
		if region.Language != core.LanguageVBScript {
			continue
		}
		for lineStart := region.ContentStart; lineStart < region.ContentEnd; {
			lineEnd := lineStart
			for lineEnd < region.ContentEnd && parsed.Text[lineEnd] != '\n' && parsed.Text[lineEnd] != '\r' {
				lineEnd++
			}
			line := parsed.Text[lineStart:lineEnd]
			for _, segment := range splitVBStatementSegments(line, lineStart) {
				if diagnostic, ok := unsupportedDeclarationDiagnosticForLine(doc, segment.Text, segment.Start); ok {
					diagnostics = append(diagnostics, diagnostic)
				}
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
	return diagnostics
}

func unsupportedDeclarationDiagnosticForLine(doc *core.TextDocument, line string, lineOffset int) (lsp.Diagnostic, bool) {
	keywordStart := leadingVBWhitespace(line)
	if keywordStart >= len(line) {
		return lsp.Diagnostic{}, false
	}
	trimmed := line[keywordStart:]
	lower := strings.ToLower(trimmed)
	if strings.HasPrefix(lower, "dim ") && topLevelKeywordIndex(trimmed, "as") >= 0 {
		return declarationDiagnostic(doc, lineOffset, line, "typedDeclaration", "VBScript As types are not supported in declarations.", "dim"), true
	}
	if strings.HasPrefix(lower, "public ") || strings.HasPrefix(lower, "private ") {
		parts := strings.Fields(trimmed)
		if len(parts) >= 2 && (strings.EqualFold(parts[1], "sub") || strings.EqualFold(parts[1], "function")) {
			return lsp.Diagnostic{}, false
		}
		if topLevelEqual(trimmed) >= 0 {
			return declarationDiagnostic(doc, lineOffset, line, "initializedDeclaration", "VBScript declarations cannot use initializers.", parts[0]), true
		}
		if topLevelKeywordIndex(trimmed, "as") >= 0 {
			return declarationDiagnostic(doc, lineOffset, line, "typedDeclaration", "VBScript As types are not supported in declarations.", parts[0]), true
		}
	}
	return lsp.Diagnostic{}, false
}

func declarationDiagnostic(doc *core.TextDocument, lineOffset int, line string, code string, message string, declarationKind string) lsp.Diagnostic {
	return lsp.Diagnostic{
		Range:    doc.Range(lineOffset, lineOffset+len(line)),
		Severity: lsp.DiagnosticSeverityError,
		Code:     code,
		Source:   "asp-lsp-vbscript-syntax",
		Message:  message,
		Data:     map[string]any{"declarationKind": strings.ToLower(declarationKind)},
	}
}

func dimDeclarationFixes(parsed *core.ParsedDocument, initializedStyle string) []dimDeclarationFix {
	doc := core.SourceDocument(parsed)
	var fixes []dimDeclarationFix
	for _, region := range parsed.Regions {
		if region.Language != core.LanguageVBScript {
			continue
		}
		for lineStart := region.ContentStart; lineStart < region.ContentEnd; {
			lineEnd := lineStart
			for lineEnd < region.ContentEnd && parsed.Text[lineEnd] != '\n' && parsed.Text[lineEnd] != '\r' {
				lineEnd++
			}
			line := parsed.Text[lineStart:lineEnd]
			for _, segment := range splitVBStatementSegments(line, lineStart) {
				if fix, ok := dimDeclarationFixForLine(doc, segment.Text, segment.Start, initializedStyle); ok {
					fixes = append(fixes, fix)
				}
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

func dimDeclarationFixForLine(doc *core.TextDocument, line string, lineOffset int, initializedStyle string) (dimDeclarationFix, bool) {
	keywordStart := leadingVBWhitespace(line)
	if len(line)-keywordStart < len("Dim") || !strings.EqualFold(line[keywordStart:keywordStart+len("Dim")], "Dim") {
		return dimDeclarationFix{}, false
	}
	afterKeyword := keywordStart + len("Dim")
	if afterKeyword >= len(line) || !isVBWhitespace(line[afterKeyword]) {
		return dimDeclarationFix{}, false
	}
	indent := line[:keywordStart]
	declarationText := strings.TrimSpace(line[afterKeyword:])
	parts := splitDimDeclarations(declarationText)
	if len(parts) == 0 {
		return dimDeclarationFix{}, false
	}
	if fix, ok := initializedDimFix(doc, line, lineOffset, indent, parts, initializedStyle); ok {
		return fix, true
	}
	if len(parts) <= 1 {
		return dimDeclarationFix{}, false
	}
	lines := make([]string, 0, len(parts))
	for _, part := range parts {
		lines = append(lines, indent+"Dim "+strings.TrimSpace(part))
	}
	return dimDeclarationFix{
		Title:   "Split Dim declarations",
		Range:   doc.Range(lineOffset, lineOffset+len(line)),
		Start:   lineOffset,
		End:     lineOffset + len(line),
		NewText: strings.Join(lines, "\n"),
	}, true
}

func initializedDimFix(doc *core.TextDocument, line string, lineOffset int, indent string, parts []string, style string) (dimDeclarationFix, bool) {
	declarations := make([]string, 0, len(parts))
	assignments := make([]string, 0, len(parts))
	for _, part := range parts {
		trimmed := strings.TrimSpace(part)
		equal := topLevelEqual(trimmed)
		if equal < 0 {
			declarations = append(declarations, trimmed)
			continue
		}
		declaration := strings.TrimSpace(trimmed[:equal])
		expression := strings.TrimSpace(trimmed[equal+1:])
		if declaration == "" || expression == "" {
			return dimDeclarationFix{}, false
		}
		declarations = append(declarations, declaration)
		assignments = append(assignments, dimAssignmentTarget(declaration)+" = "+expression)
	}
	if len(assignments) == 0 {
		return dimDeclarationFix{}, false
	}
	declarationLine := indent + "Dim " + strings.Join(declarations, ", ")
	assignmentSeparator := " : "
	if style == "newline" {
		assignmentSeparator = "\n" + indent
	}
	newText := declarationLine + assignmentSeparator + strings.Join(assignments, assignmentSeparator)
	diagnostic := lsp.Diagnostic{
		Range:    doc.Range(lineOffset, lineOffset+len(line)),
		Severity: lsp.DiagnosticSeverityError,
		Code:     "initializedDeclaration",
		Source:   "asp-lsp-vbscript-syntax",
		Message:  "VBScript Dim declarations cannot use initializers.",
		Data:     map[string]any{"declarationKind": "dim"},
	}
	return dimDeclarationFix{
		Title:      "Split initialized Dim declaration",
		Range:      diagnostic.Range,
		Start:      lineOffset,
		End:        lineOffset + len(line),
		NewText:    newText,
		Diagnostic: &diagnostic,
	}, true
}

func leadingVBWhitespace(text string) int {
	for i := 0; i < len(text); i++ {
		if !isVBWhitespace(text[i]) {
			return i
		}
	}
	return len(text)
}

func isVBWhitespace(b byte) bool {
	return b == ' ' || b == '\t'
}

func splitDimDeclarations(text string) []string {
	var parts []string
	start := 0
	depth := 0
	for i := 0; i < len(text); i++ {
		switch text[i] {
		case '"':
			i = skipVBString(text, i) - 1
		case '(':
			depth++
		case ')':
			if depth > 0 {
				depth--
			}
		case ',':
			if depth == 0 {
				parts = append(parts, strings.TrimSpace(text[start:i]))
				start = i + 1
			}
		}
	}
	parts = append(parts, strings.TrimSpace(text[start:]))
	filtered := parts[:0]
	for _, part := range parts {
		if part != "" {
			filtered = append(filtered, part)
		}
	}
	return filtered
}

func topLevelEqual(text string) int {
	depth := 0
	for i := 0; i < len(text); i++ {
		switch text[i] {
		case '\'':
			return -1
		case '"':
			i = skipVBString(text, i) - 1
		case '(':
			depth++
		case ')':
			if depth > 0 {
				depth--
			}
		case '=':
			if depth == 0 {
				return i
			}
		}
	}
	return -1
}

func topLevelKeywordIndex(text string, keyword string) int {
	depth := 0
	for i := 0; i < len(text); i++ {
		switch text[i] {
		case '\'':
			return -1
		case '"':
			i = skipVBString(text, i) - 1
		case '(':
			depth++
		case ')':
			if depth > 0 {
				depth--
			}
		default:
			if depth == 0 && isVBIdentifierStart(text[i]) {
				start := i
				i = readVBIdentifier(text, i)
				if strings.EqualFold(text[start:i], keyword) {
					return start
				}
				i--
			}
		}
	}
	return -1
}

func skipVBString(text string, offset int) int {
	offset++
	for offset < len(text) {
		if text[offset] == '"' {
			offset++
			if offset < len(text) && text[offset] == '"' {
				offset++
				continue
			}
			return offset
		}
		offset++
	}
	return offset
}

func dimAssignmentTarget(declaration string) string {
	declaration = strings.TrimSpace(declaration)
	for i := 0; i < len(declaration); i++ {
		if declaration[i] == '(' || isVBWhitespace(declaration[i]) {
			return strings.TrimSpace(declaration[:i])
		}
	}
	return declaration
}

type vbStatementSegment struct {
	Text  string
	Start int
	End   int
}

func splitVBStatementSegments(line string, lineOffset int) []vbStatementSegment {
	segments := make([]vbStatementSegment, 0, 1)
	start := 0
	depth := 0
	for i := 0; i < len(line); i++ {
		switch line[i] {
		case '\'':
			i = len(line)
		case '"':
			i = skipVBString(line, i) - 1
		case '(':
			depth++
		case ')':
			if depth > 0 {
				depth--
			}
		case ':':
			if depth == 0 || vbUnterminatedProcedureHeaderBeforeColon(line, start, i) {
				segments = appendVBStatementSegment(segments, line, lineOffset, start, i)
				start = i + 1
				depth = 0
			}
		}
	}
	segments = appendVBStatementSegment(segments, line, lineOffset, start, len(line))
	return segments
}

func appendVBStatementSegment(segments []vbStatementSegment, line string, lineOffset, start, end int) []vbStatementSegment {
	for start < end && isVBWhitespace(line[start]) {
		start++
	}
	for end > start && isVBWhitespace(line[end-1]) {
		end--
	}
	if start >= end {
		return segments
	}
	return append(segments, vbStatementSegment{
		Text:  line[start:end],
		Start: lineOffset + start,
		End:   lineOffset + end,
	})
}
