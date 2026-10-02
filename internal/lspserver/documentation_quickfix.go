package lspserver

import (
	"strings"

	"github.com/yottonoko/classic-web-system-lsp/internal/core"
	"github.com/yottonoko/classic-web-system-lsp/internal/lsp"
	"github.com/yottonoko/classic-web-system-lsp/internal/vbscript"
)

func (s *Server) documentationCodeActions(params codeActionParams) []lsp.CodeAction {
	doc, parsed := s.parsed(params.TextDocument.URI)
	if doc == nil || parsed == nil {
		return nil
	}
	if len(params.Context.Diagnostics) > 0 {
		return nil
	}
	position := params.Range.Start
	for _, signature := range vbscript.Signatures(parsed) {
		if !positionInRange(position, signature.Range) {
			continue
		}
		edits := vbscriptDocumentationEdits(doc, parsed, signature)
		if len(edits) == 0 {
			return nil
		}
		return []lsp.CodeAction{{
			Title: "Generate VBScript documentation",
			Kind:  "quickfix",
			Edit: &lsp.WorkspaceEdit{Changes: map[string][]lsp.TextEdit{
				params.TextDocument.URI: edits,
			}},
		}}
	}
	if declaration, ok := vbscriptDocumentationDeclarationAt(parsed, position); ok {
		edits := vbscriptDeclarationDocumentationEdits(parsed, declaration)
		if len(edits) == 0 {
			return nil
		}
		return []lsp.CodeAction{{
			Title: "Generate VBScript documentation",
			Kind:  "quickfix",
			Edit: &lsp.WorkspaceEdit{Changes: map[string][]lsp.TextEdit{
				params.TextDocument.URI: edits,
			}},
		}}
	}
	return nil
}

func vbscriptDocumentationEdits(doc *core.TextDocument, parsed *core.ParsedDocument, signature vbscript.Signature) []lsp.TextEdit {
	lines := strings.Split(parsed.Text, "\n")
	block := xmlDocBlockBeforeLine(lines, signature.Range.Start.Line)
	existing := vbscriptXMLDoc{Params: map[string]string{}}
	if len(block) > 0 {
		existing = parseVBScriptXMLDoc(strings.Join(block, "\n"))
	}
	annotationLines := missingVBScriptDocumentationAnnotationLines(parsed.Text, signature)
	xmlLines := missingVBScriptDocumentationXMLLines(signature, existing)
	if len(annotationLines) == 0 && len(xmlLines) == 0 {
		return nil
	}
	newLine := preferredDocumentationNewLine(parsed.Text)
	if len(block) == 0 {
		insert := lsp.Range{
			Start: lsp.Position{Line: signature.Range.Start.Line, Character: 0},
			End:   lsp.Position{Line: signature.Range.Start.Line, Character: 0},
		}
		return []lsp.TextEdit{{Range: insert, NewText: strings.Join(append(annotationLines, xmlLines...), newLine) + newLine}}
	}
	startOffset, endOffset, ok := xmlDocBlockOffsetsBeforeLine(parsed.Text, signature.Range.Start.Line)
	if !ok {
		return nil
	}
	existingText := parsed.Text[startOffset:endOffset]
	replacement := strings.Join(annotationLines, newLine)
	if replacement != "" {
		replacement += newLine
	}
	replacement += existingText
	if len(xmlLines) > 0 {
		replacement += newLine + strings.Join(xmlLines, newLine)
	}
	return []lsp.TextEdit{{
		Range:   doc.Range(startOffset, endOffset),
		NewText: replacement,
	}}
}

func missingVBScriptDocumentationAnnotationLines(text string, signature vbscript.Signature) []string {
	var lines []string
	for _, parameter := range signature.Parameters {
		if !hasVBScriptParameterAnnotation(text, signature.Name, parameter.Name) {
			lines = append(lines, "' @param "+signature.Name+"."+parameter.Name+" As Variant")
		}
	}
	if signature.Kind == "function" && !hasVBScriptReturnsAnnotation(text, signature.Name) {
		lines = append(lines, "' @returns "+signature.Name+" Variant")
	}
	return lines
}

func missingVBScriptDocumentationXMLLines(signature vbscript.Signature, existing vbscriptXMLDoc) []string {
	var lines []string
	if existing.Summary == "" {
		lines = append(lines, "''' <summary>TODO: Describe "+signature.Name+".</summary>")
	}
	for _, parameter := range signature.Parameters {
		if existing.Params[strings.ToLower(parameter.Name)] == "" {
			lines = append(lines, "''' <param name=\""+parameter.Name+"\">TODO: Describe "+parameter.Name+".</param>")
		}
	}
	if signature.Kind == "function" && existing.Returns == "" {
		lines = append(lines, "''' <returns>TODO: Describe return value.</returns>")
	}
	return lines
}

func vbscriptDocumentationDeclarationAt(parsed *core.ParsedDocument, position lsp.Position) (vbUsageDeclaration, bool) {
	offset := core.SourceDocument(parsed).OffsetAt(position)
	var found vbUsageDeclaration
	foundSize := 0
	for _, declaration := range vbNamingDeclarationsShared(parsed) {
		if !isDocumentationDeclarationKind(declaration.Kind) {
			continue
		}
		if offset < declaration.Start || offset > declaration.End {
			continue
		}
		size := declaration.End - declaration.Start
		if found.Name == "" || size < foundSize {
			found = declaration
			foundSize = size
		}
	}
	return found, found.Name != ""
}

func isDocumentationDeclarationKind(kind string) bool {
	switch kind {
	case "variable", "constant", "class", "field", "property", "method":
		return true
	default:
		return false
	}
}

func vbscriptDeclarationDocumentationEdits(parsed *core.ParsedDocument, declaration vbUsageDeclaration) []lsp.TextEdit {
	lines := missingVBScriptDeclarationDocumentationLines(parsed, declaration)
	if len(lines) == 0 {
		return nil
	}
	insert := lsp.Range{
		Start: lsp.Position{Line: declaration.Line, Character: 0},
		End:   lsp.Position{Line: declaration.Line, Character: 0},
	}
	return []lsp.TextEdit{{Range: insert, NewText: strings.Join(lines, preferredDocumentationNewLine(parsed.Text)) + preferredDocumentationNewLine(parsed.Text)}}
}

func missingVBScriptDeclarationDocumentationLines(parsed *core.ParsedDocument, declaration vbUsageDeclaration) []string {
	typeName := inferVBDeclarationType(parsed, declaration)
	if strings.TrimSpace(typeName) == "" {
		typeName = "Variant"
	}
	switch declaration.Kind {
	case "variable", "constant", "field":
		lines := []string{
			"' @type " + declaration.Name + " As " + typeName,
		}
		if vbscriptDeclarationHasAmbiguousXMLDocumentation(parsed, declaration) {
			return lines
		}
		return append(lines,
			"''' <summary>TODO: Describe "+declaration.Name+".</summary>",
			"''' <value>TODO: Describe "+declaration.Name+".</value>",
		)
	case "class":
		return []string{"''' <summary>TODO: Describe " + declaration.Name + ".</summary>"}
	case "property":
		return []string{
			"' @returns " + declaration.Name + " " + typeName,
			"''' <summary>TODO: Describe " + declaration.Name + ".</summary>",
			"''' <returns>TODO: Describe return value.</returns>",
			"''' <value>TODO: Describe " + declaration.Name + ".</value>",
		}
	case "method":
		return []string{
			"''' <summary>TODO: Describe " + declaration.Name + ".</summary>",
		}
	default:
		return nil
	}
}

func vbscriptDeclarationHasAmbiguousXMLDocumentation(parsed *core.ParsedDocument, declaration vbUsageDeclaration) bool {
	switch declaration.Kind {
	case "variable", "constant":
		lineStart := declaration.Start
		for lineStart > 0 && parsed.Text[lineStart-1] != '\n' && parsed.Text[lineStart-1] != '\r' {
			lineStart--
		}
		lineEnd := declaration.End
		for lineEnd < len(parsed.Text) && parsed.Text[lineEnd] != '\n' && parsed.Text[lineEnd] != '\r' {
			lineEnd++
		}
		line := parsed.Text[lineStart:lineEnd]
		declarations := vbLineDeclarations(core.SourceDocument(parsed), line, lineStart, declaration.Local, declaration.Scope)
		count := 0
		for _, candidate := range declarations {
			if candidate.Kind == declaration.Kind {
				count++
			}
		}
		return count > 1
	case "field":
		return false
	default:
		return false
	}
}

func xmlDocBlockOffsetsBeforeLine(text string, line int) (int, int, bool) {
	lines := strings.SplitAfter(text, "\n")
	if line > len(lines) {
		line = len(lines)
	}
	endLine := line - 1
	for endLine >= 0 && strings.TrimSpace(lines[endLine]) == "" {
		endLine--
	}
	if endLine < 0 {
		return 0, 0, false
	}
	startLine := endLine
	for startLine >= 0 {
		trimmed := strings.TrimSpace(strings.TrimRight(lines[startLine], "\r\n"))
		if !strings.HasPrefix(trimmed, "'''") {
			break
		}
		startLine--
	}
	startLine++
	if startLine > endLine {
		return 0, 0, false
	}
	startOffset := 0
	for i := 0; i < startLine; i++ {
		startOffset += len(lines[i])
	}
	endOffset := startOffset
	for i := startLine; i <= endLine; i++ {
		endOffset += len(strings.TrimRight(lines[i], "\r\n"))
	}
	return startOffset, endOffset, true
}

func preferredDocumentationNewLine(text string) string {
	if strings.Contains(text, "\r\n") {
		return "\r\n"
	}
	return "\n"
}

func hasVBScriptParameterAnnotation(text string, signatureName string, parameterName string) bool {
	needle := "@param " + strings.ToLower(signatureName) + "." + strings.ToLower(parameterName) + " "
	return strings.Contains(strings.ToLower(text), needle)
}

func hasVBScriptReturnsAnnotation(text string, signatureName string) bool {
	needle := "@returns " + strings.ToLower(signatureName) + " "
	return strings.Contains(strings.ToLower(text), needle)
}

func positionInRange(position lsp.Position, r lsp.Range) bool {
	if position.Line < r.Start.Line || position.Line == r.Start.Line && position.Character < r.Start.Character {
		return false
	}
	if position.Line > r.End.Line || position.Line == r.End.Line && position.Character > r.End.Character {
		return false
	}
	return true
}
