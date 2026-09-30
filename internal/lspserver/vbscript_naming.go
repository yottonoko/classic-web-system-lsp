package lspserver

import (
	"regexp"
	"slices"
	"sort"
	"strings"

	"github.com/yottonoko/classic-web-system-lsp/internal/core"
	"github.com/yottonoko/classic-web-system-lsp/internal/lsp"
)

var (
	vbPropertyLinePattern         = regexp.MustCompile(`(?i)^\s*(?:(?:Public|Private|Default)\s+)*Property\s+(?:Get|Let|Set)\s+([A-Za-z_][A-Za-z0-9_]*)`)
	vbPropertyAccessorLinePattern = regexp.MustCompile(`(?i)^\s*(?:(?:Public|Private|Default)\s+)*Property\s+(Get|Let|Set)\s+([A-Za-z_][A-Za-z0-9_]*)`)
	vbClassConstPattern           = regexp.MustCompile(`(?i)^\s*(?:Public|Private)\s+Const\s+([A-Za-z_][A-Za-z0-9_]*)`)
	vbMemberLinePattern           = regexp.MustCompile(`(?i)^\s*(?:Public|Private)\s+([A-Za-z_][A-Za-z0-9_]*)`)
)

func (s *Server) vbscriptNamingDiagnostics(parsed *core.ParsedDocument) []lsp.Diagnostic {
	declarations := collectVBNamingDeclarations(parsed)
	diagnostics := make([]lsp.Diagnostic, 0, len(declarations))
	for _, declaration := range declarations {
		style := s.identifierCaseForDeclaration(declaration)
		if style == "ignore" {
			continue
		}
		expected := formatVBIdentifierCase(declaration.Name, style)
		if expected == "" || expected == declaration.Name {
			continue
		}
		diagnostics = append(diagnostics, lsp.Diagnostic{
			Range:    declaration.Range,
			Severity: lsp.DiagnosticSeverityHint,
			Code:     "identifierCase",
			Source:   "asp-lsp-vbscript-naming",
			Message:  "Identifier '" + declaration.Name + "' should be '" + expected + "' for " + style + " casing.",
			Data: map[string]any{
				"name":         declaration.Name,
				"expectedName": expected,
				"style":        style,
			},
		})
	}
	return diagnostics
}

func (s *Server) vbscriptNamingCodeActions(params codeActionParams) []lsp.CodeAction {
	doc, parsed := s.parsed(params.TextDocument.URI)
	if doc == nil || parsed == nil {
		return nil
	}
	declarations := collectVBNamingDeclarations(parsed)
	byRange := map[lsp.Range]vbUsageDeclaration{}
	for _, declaration := range declarations {
		byRange[declaration.Range] = declaration
	}
	var actions []lsp.CodeAction
	for _, diagnostic := range params.Context.Diagnostics {
		if diagnostic.Source != "asp-lsp-vbscript-naming" {
			continue
		}
		name, expectedName, ok := namingDiagnosticData(diagnostic)
		if !ok {
			name = strings.TrimSpace(textInRange(doc, diagnostic.Range))
			expectedName = formatVBIdentifierCase(name, s.identifierCaseForDeclaration(vbUsageDeclaration{Name: name, Kind: "variable"}))
		}
		if name == "" || expectedName == "" {
			continue
		}
		declaration, ok := byRange[diagnostic.Range]
		if ok && hasVBScriptNamingCollision(declarations, declaration, expectedName) {
			continue
		}
		changes := map[string][]lsp.TextEdit{}
		if edits := vbIdentifierRenameEditsForDeclaration(parsed, declaration, name, expectedName); len(edits) > 0 {
			changes[params.TextDocument.URI] = edits
		}
		if s.settings.WorkspaceSymbolRename {
			for uri, workspaceDoc := range s.workspaceDocumentsExcept(params.TextDocument.URI) {
				workspaceParsed := s.parseText(uri, workspaceDoc.Text, s.settings.DefaultLanguage)
				if edits := vbIdentifierRenameEdits(workspaceParsed, name, expectedName); len(edits) > 0 {
					changes[uri] = edits
				}
			}
		}
		if len(changes) == 0 {
			continue
		}
		diag := diagnostic
		actions = append(actions, lsp.CodeAction{
			Title:       "Rename " + name + " to " + expectedName,
			Kind:        "quickfix",
			Diagnostics: []lsp.Diagnostic{diag},
			Edit:        &lsp.WorkspaceEdit{Changes: changes},
		})
	}
	return actions
}

type vbNamingDeclarationsCache struct {
	declarations []vbUsageDeclaration
	bytes        int64
}

// EstimateBytes reports the immutable cache size without walking declarations again.
func (cache *vbNamingDeclarationsCache) EstimateBytes() int64 { return cache.bytes }

func storeVBNamingDeclarations(parsed *core.ParsedDocument, declarations []vbUsageDeclaration) {
	bytes := int64(32) + estimateAnalysisDeclarationsBytes(nil, declarations)
	bytes += int64(cap(declarations)-len(declarations)) * 160
	parsed.StoreRuntimeAnalysis("lspserver.vb-naming-declarations.v1", &vbNamingDeclarationsCache{declarations: declarations, bytes: bytes})
}

func collectVBNamingDeclarations(parsed *core.ParsedDocument) []vbUsageDeclaration {
	const analysisKey = "lspserver.vb-naming-declarations.v1"
	if value, ok := parsed.LoadRuntimeAnalysis(analysisKey); ok {
		if cached, ok := value.(*vbNamingDeclarationsCache); ok {
			return slices.Clone(cached.declarations)
		}
	}
	var cached []vbUsageDeclaration
	if parsed.LoadAnalysis(analysisKey, &cached) {
		storeVBNamingDeclarations(parsed, cached)
		return slices.Clone(cached)
	}
	declarations := append([]vbUsageDeclaration(nil), collectVBUsageDeclarations(parsed).Declarations...)
	doc := core.SourceDocument(parsed)
	for _, region := range parsed.Regions {
		if region.Language != core.LanguageVBScript {
			continue
		}
		currentClass := ""
		for lineStart := region.ContentStart; lineStart < region.ContentEnd; {
			lineEnd := lineStart
			for lineEnd < region.ContentEnd && parsed.Text[lineEnd] != '\n' && parsed.Text[lineEnd] != '\r' {
				lineEnd++
			}
			line := parsed.Text[lineStart:lineEnd]
			trimmed := strings.TrimSpace(line)
			lowerTrimmed := strings.ToLower(trimmed)
			if matches := vbClassDeclarationLinePattern.FindStringSubmatchIndex(line); matches != nil {
				currentClass = line[matches[2]:matches[3]]
			}
			if currentClass != "" {
				if declaration, ok := vbClassMemberDeclaration(doc, line, lineStart, currentClass); ok {
					declarations = append(declarations, declaration)
				}
			}
			if lowerTrimmed == "end class" {
				currentClass = ""
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
	declarations = dedupeAndSortVBDeclarations(declarations)
	// Callers may append or modify declarations; keep the cached slice private.
	storeVBNamingDeclarations(parsed, declarations)
	return slices.Clone(declarations)
}

func vbClassMemberDeclaration(doc *core.TextDocument, line string, lineOffset int, memberOf string) (vbUsageDeclaration, bool) {
	if matches := vbPropertyLinePattern.FindStringSubmatchIndex(line); matches != nil {
		start := lineOffset + matches[2]
		end := lineOffset + matches[3]
		procedureKind := "property"
		if accessor := vbPropertyAccessorForLine(line); accessor != "" {
			procedureKind = "property-" + accessor
		}
		return vbUsageDeclaration{Name: line[matches[2]:matches[3]], Kind: "property", Range: doc.Range(start, end), Start: start, End: end, Line: doc.PositionAt(start).Line, MemberOf: memberOf, ProcedureKind: procedureKind}, true
	}
	if matches := vbProcedureLinePattern.FindStringSubmatchIndex(line); matches != nil {
		start := lineOffset + matches[4]
		end := lineOffset + matches[5]
		return vbUsageDeclaration{Name: line[matches[4]:matches[5]], Kind: "method", Range: doc.Range(start, end), Start: start, End: end, Line: doc.PositionAt(start).Line, MemberOf: memberOf, ProcedureKind: strings.ToLower(line[matches[2]:matches[3]])}, true
	}
	if matches := vbClassConstPattern.FindStringSubmatchIndex(line); matches != nil {
		start := lineOffset + matches[2]
		end := lineOffset + matches[3]
		return vbUsageDeclaration{Name: line[matches[2]:matches[3]], Kind: "constant", Range: doc.Range(start, end), Start: start, End: end, Line: doc.PositionAt(start).Line, MemberOf: memberOf}, true
	}
	if matches := vbMemberLinePattern.FindStringSubmatchIndex(line); matches != nil {
		name := line[matches[2]:matches[3]]
		lower := strings.ToLower(name)
		if lower == "property" || lower == "sub" || lower == "function" || lower == "const" {
			return vbUsageDeclaration{}, false
		}
		start := lineOffset + matches[2]
		end := lineOffset + matches[3]
		return vbUsageDeclaration{Name: name, Kind: "field", Range: doc.Range(start, end), Start: start, End: end, Line: doc.PositionAt(start).Line, MemberOf: memberOf}, true
	}
	return vbUsageDeclaration{}, false
}

func vbPropertyAccessorForLine(line string) string {
	matches := vbPropertyAccessorLinePattern.FindStringSubmatchIndex(line)
	if matches == nil {
		return ""
	}
	return strings.ToLower(line[matches[2]:matches[3]])
}

func dedupeAndSortVBDeclarations(declarations []vbUsageDeclaration) []vbUsageDeclaration {
	result := make([]vbUsageDeclaration, 0, len(declarations))
	type declarationKey struct {
		kind string
		span offsetRange
	}
	indexByKey := map[declarationKey]int{}
	for _, declaration := range declarations {
		key := declarationKey{kind: declaration.Kind, span: offsetRangeKey(declaration.Start, declaration.End)}
		if index, ok := indexByKey[key]; ok {
			if declaration.MemberOf != "" && result[index].MemberOf == "" {
				result[index] = declaration
			} else if declaration.Local && !result[index].Local {
				result[index] = declaration
			}
			continue
		}
		indexByKey[key] = len(result)
		result = append(result, declaration)
	}
	sort.SliceStable(result, func(i, j int) bool {
		if result[i].Start != result[j].Start {
			return result[i].Start < result[j].Start
		}
		return result[i].End < result[j].End
	})
	return result
}

func (s *Server) identifierCaseForDeclaration(declaration vbUsageDeclaration) string {
	kind := declaration.Kind
	if kind == "const" {
		kind = "constant"
	}
	if s.settings.IdentifierCaseByKind != nil {
		if style := s.settings.IdentifierCaseByKind[kind]; style != "" {
			return style
		}
	}
	if s.settings.IdentifierCase != "" {
		return s.settings.IdentifierCase
	}
	if kind == "variable" || kind == "parameter" {
		return "camelCase"
	}
	return "PascalCase"
}

func normalizeVBIdentifierCase(value string) string {
	switch value {
	case "PascalCase", "UPPERCASE", "camelCase", "lowercase", "snake_case", "UPPER_SNAKE", "ignore":
		return value
	case "pascal":
		return "PascalCase"
	case "upper":
		return "UPPERCASE"
	case "camel":
		return "camelCase"
	case "lower":
		return "lowercase"
	case "snake":
		return "snake_case"
	case "upperSnake":
		return "UPPER_SNAKE"
	default:
		return ""
	}
}

func normalizeVBIdentifierCaseByKind(input map[string]string) map[string]string {
	if len(input) == 0 {
		return nil
	}
	result := map[string]string{}
	for kind, value := range input {
		switch kind {
		case "variable", "parameter", "class", "function", "sub", "constant", "field", "property", "method":
			if normalized := normalizeVBIdentifierCase(value); normalized != "" {
				result[kind] = normalized
			}
		}
	}
	if len(result) == 0 {
		return nil
	}
	return result
}

func normalizeVBScriptSQLInjectionDiagnostics(value string) string {
	switch strings.ToLower(strings.TrimSpace(value)) {
	case "hint", "information", "warning":
		return strings.ToLower(strings.TrimSpace(value))
	default:
		return "off"
	}
}

// vbscriptSQLInjectionSeverity maps the setting to a severity; ok is false
// when the diagnostic is turned off.
func vbscriptSQLInjectionSeverity(setting string) (lsp.DiagnosticSeverity, bool) {
	switch setting {
	case "hint":
		return lsp.DiagnosticSeverityHint, true
	case "information":
		return lsp.DiagnosticSeverityInformation, true
	case "warning":
		return lsp.DiagnosticSeverityWarning, true
	default:
		return 0, false
	}
}

func normalizeVBScriptIfSyntaxDiagnostics(value string) string {
	switch strings.ToLower(strings.TrimSpace(value)) {
	case "off", "basic", "strict":
		return strings.ToLower(strings.TrimSpace(value))
	default:
		return "basic"
	}
}

func formatVBIdentifierCase(name string, style string) string {
	words := vbIdentifierWords(name)
	if len(words) == 0 {
		return ""
	}
	switch style {
	case "PascalCase":
		return joinVBCapitalized(words, "")
	case "UPPERCASE":
		return strings.ToUpper(strings.Join(words, ""))
	case "camelCase":
		pascal := joinVBCapitalized(words, "")
		if pascal == "" {
			return ""
		}
		return strings.ToLower(pascal[:1]) + pascal[1:]
	case "lowercase":
		return strings.ToLower(strings.Join(words, ""))
	case "snake_case":
		return strings.ToLower(strings.Join(words, "_"))
	case "UPPER_SNAKE":
		return strings.ToUpper(strings.Join(words, "_"))
	default:
		return name
	}
}

func vbIdentifierWords(name string) []string {
	var words []string
	for _, part := range strings.Split(name, "_") {
		if part == "" {
			continue
		}
		start := 0
		for i := 1; i < len(part); i++ {
			if isUpperASCII(part[i]) && (isLowerASCII(part[i-1]) || (i+1 < len(part) && isLowerASCII(part[i+1]))) {
				words = append(words, strings.ToLower(part[start:i]))
				start = i
			}
		}
		words = append(words, strings.ToLower(part[start:]))
	}
	return words
}

func joinVBCapitalized(words []string, separator string) string {
	parts := make([]string, 0, len(words))
	for _, word := range words {
		if word == "" {
			continue
		}
		parts = append(parts, strings.ToUpper(word[:1])+strings.ToLower(word[1:]))
	}
	return strings.Join(parts, separator)
}

func isUpperASCII(b byte) bool {
	return b >= 'A' && b <= 'Z'
}

func isLowerASCII(b byte) bool {
	return b >= 'a' && b <= 'z'
}

func namingDiagnosticData(diagnostic lsp.Diagnostic) (string, string, bool) {
	data, ok := diagnostic.Data.(map[string]any)
	if !ok {
		return "", "", false
	}
	name, _ := data["name"].(string)
	expectedName, _ := data["expectedName"].(string)
	return name, expectedName, name != "" && expectedName != ""
}

func hasVBScriptNamingCollision(declarations []vbUsageDeclaration, declaration vbUsageDeclaration, expectedName string) bool {
	lower := strings.ToLower(expectedName)
	for _, candidate := range declarations {
		if candidate.Start == declaration.Start && candidate.End == declaration.End {
			continue
		}
		if declaration.Local && candidate.Local && declaration.Scope != "" && candidate.Scope != "" && declaration.Scope != candidate.Scope {
			continue
		}
		if strings.ToLower(candidate.Name) == lower {
			return true
		}
	}
	return false
}

func vbIdentifierRenameEditsForDeclaration(parsed *core.ParsedDocument, declaration vbUsageDeclaration, name string, expectedName string) []lsp.TextEdit {
	if declaration.Kind == "field" {
		return vbFieldIdentifierRenameEdits(parsed, declaration, name, expectedName)
	}
	if declaration.Local && declaration.Scope != "" {
		return vbScopedIdentifierRenameEdits(parsed, declaration, name, expectedName)
	}
	return vbIdentifierRenameEdits(parsed, name, expectedName)
}

func vbScopedIdentifierRenameEdits(parsed *core.ParsedDocument, declaration vbUsageDeclaration, name string, expectedName string) []lsp.TextEdit {
	scopeStart, scopeEnd, ok := vbEnclosingProcedureOffsets(parsed, declaration.Start)
	if !ok {
		return vbIdentifierRenameEdits(parsed, name, expectedName)
	}
	return vbIdentifierRenameEditsInOffsets(parsed, name, expectedName, scopeStart, scopeEnd, false)
}

func vbFieldIdentifierRenameEdits(parsed *core.ParsedDocument, declaration vbUsageDeclaration, name string, expectedName string) []lsp.TextEdit {
	return vbIdentifierRenameEditsInOffsets(parsed, name, expectedName, 0, len(parsed.Text), true)
}

func vbIdentifierRenameEdits(parsed *core.ParsedDocument, name string, expectedName string) []lsp.TextEdit {
	return vbIdentifierRenameEditsInOffsets(parsed, name, expectedName, 0, len(parsed.Text), false)
}

func vbIdentifierRenameEditsInOffsets(parsed *core.ParsedDocument, name string, expectedName string, minOffset int, maxOffset int, memberOnly bool) []lsp.TextEdit {
	doc := core.SourceDocument(parsed)
	lower := strings.ToLower(name)
	var edits []lsp.TextEdit
	for _, region := range parsed.Regions {
		if region.Language != core.LanguageVBScript {
			continue
		}
		text := parsed.Text[region.ContentStart:region.ContentEnd]
		for _, span := range vbIdentifierSpans(text) {
			start := region.ContentStart + span.Start
			end := region.ContentStart + span.End
			if start < minOffset || end > maxOffset {
				continue
			}
			if strings.ToLower(parsed.Text[start:end]) != lower {
				continue
			}
			if memberOnly && start != minOffset && previousNonSpace(parsed.Text, start) != '.' {
				if start != declarationStartForName(parsed, name, start) {
					continue
				}
			}
			edits = append(edits, lsp.TextEdit{Range: doc.Range(start, end), NewText: expectedName})
		}
	}
	return edits
}

func declarationStartForName(parsed *core.ParsedDocument, name string, offset int) int {
	for _, declaration := range collectVBNamingDeclarations(parsed) {
		if declaration.Start == offset && strings.EqualFold(declaration.Name, name) {
			return declaration.Start
		}
	}
	return -1
}

func vbEnclosingProcedureOffsets(parsed *core.ParsedDocument, offset int) (int, int, bool) {
	for _, region := range parsed.Regions {
		if region.Language != core.LanguageVBScript {
			continue
		}
		procedureStart := -1
		for lineStart := region.ContentStart; lineStart < region.ContentEnd; {
			lineEnd := lineStart
			for lineEnd < region.ContentEnd && parsed.Text[lineEnd] != '\n' && parsed.Text[lineEnd] != '\r' {
				lineEnd++
			}
			line := parsed.Text[lineStart:lineEnd]
			trimmed := strings.ToLower(strings.TrimSpace(line))
			if header, ok := vbProcedureHeaderAtLine(line); ok {
				procedureStart = lineStart + header.NameStart
			}
			if (trimmed == "end sub" || trimmed == "end function" || trimmed == "end property") && procedureStart >= 0 {
				end := lineEnd
				if lineEnd < len(parsed.Text) && (parsed.Text[lineEnd] == '\n' || parsed.Text[lineEnd] == '\r') {
					end = lineEnd + 1
					if parsed.Text[lineEnd] == '\r' && end < len(parsed.Text) && parsed.Text[end] == '\n' {
						end++
					}
				}
				if offset >= procedureStart && offset <= end {
					return procedureStart, end, true
				}
				procedureStart = -1
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
	return 0, 0, false
}

func (s *Server) workspaceDocumentsExcept(uri string) map[string]*core.TextDocument {
	s.mu.Lock()
	defer s.mu.Unlock()
	docs := map[string]*core.TextDocument{}
	for workspaceURI, doc := range s.workspace {
		if workspaceURI == uri {
			continue
		}
		docs[workspaceURI] = doc
	}
	return docs
}
