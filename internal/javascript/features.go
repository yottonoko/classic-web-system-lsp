package javascript

import (
	"regexp"
	"strconv"
	"strings"

	"github.com/yottonoko/classic-web-system-lsp/internal/core"
	"github.com/yottonoko/classic-web-system-lsp/internal/lsp"
	"github.com/yottonoko/classic-web-system-lsp/internal/tsgoadapter"
)

type Symbol struct {
	Name  string
	Kind  string
	Range lsp.Range
}

type Occurrence struct {
	Name  string
	Range lsp.Range
}

type Index struct {
	Declarations map[string]Symbol
	Occurrences  map[string][]Occurrence
}

var functionPattern = regexp.MustCompile(`(?m)\bfunction\s+([A-Za-z_$][A-Za-z0-9_$]*)\s*\(([^)]*)\)`)
var jsDocParamPattern = regexp.MustCompile(`(?s)@param\s+\{([^}]+)\}\s+([A-Za-z_$][A-Za-z0-9_$]*)`)
var newExpressionPattern = regexp.MustCompile(`\b(?:const|let|var)\s+([A-Za-z_$][A-Za-z0-9_$]*)\s*=\s*new\s+([A-Za-z_$][A-Za-z0-9_$.]*)`)
var varDeclarationPattern = regexp.MustCompile(`\b(?:const|let|var)\s+([A-Za-z_$][A-Za-z0-9_$]*)`)
var missingExpressionAssignmentPattern = regexp.MustCompile(`=\s*;`)
var arrowFunctionParamPattern = regexp.MustCompile(`(?:^|[=(,:]\s*)([A-Za-z_$][A-Za-z0-9_$]*)\s*=>`)
var querySelectorPattern = regexp.MustCompile(`\b(?:const|let|var)\s+([A-Za-z_$][A-Za-z0-9_$]*)\s*=\s*document\.querySelector(?:All)?\s*\(`)
var forEachParamPattern = regexp.MustCompile(`\.forEach\s*\(\s*\(?\s*([A-Za-z_$][A-Za-z0-9_$]*)\b`)

func Completions(parsed *core.ParsedDocument) lsp.CompletionList {
	index := BuildIndex(parsed)
	items := make([]lsp.CompletionItem, 0, len(index.Declarations)+6)
	for _, symbol := range index.Declarations {
		kind := lsp.CompletionItemKindVariable
		if symbol.Kind == "function" {
			kind = lsp.CompletionItemKindFunction
		}
		items = append(items, lsp.CompletionItem{Label: symbol.Name, Kind: kind, Detail: "JavaScript"})
	}
	for _, keyword := range []string{"const", "let", "var", "function", "return", "document", "console"} {
		items = append(items, lsp.CompletionItem{Label: keyword, Kind: lsp.CompletionItemKindKeyword, Detail: "JavaScript"})
	}
	items = append(items, lsp.CompletionItem{Label: "log", Kind: lsp.CompletionItemKindMethod, Detail: "console.log"})
	return lsp.CompletionList{Items: items}
}

func DocumentSymbols(parsed *core.ParsedDocument) []lsp.DocumentSymbol {
	index := BuildIndex(parsed)
	symbols := make([]lsp.DocumentSymbol, 0, len(index.Declarations))
	for _, symbol := range index.Declarations {
		symbols = append(symbols, lsp.DocumentSymbol{
			Name:           symbol.Name,
			Kind:           symbolKind(symbol.Kind),
			Range:          symbol.Range,
			SelectionRange: symbol.Range,
		})
	}
	return symbols
}

func WorkspaceSymbols(parsed *core.ParsedDocument, query string) []lsp.SymbolInformation {
	index := BuildIndex(parsed)
	query = strings.ToLower(strings.TrimSpace(query))
	symbols := make([]lsp.SymbolInformation, 0, len(index.Declarations))
	for _, symbol := range index.Declarations {
		if query != "" && !strings.Contains(strings.ToLower(symbol.Name), query) {
			continue
		}
		symbols = append(symbols, lsp.SymbolInformation{
			Name:     symbol.Name,
			Kind:     symbolKind(symbol.Kind),
			Location: lsp.Location{URI: parsed.URI, Range: symbol.Range},
		})
	}
	return symbols
}

func Diagnostics(parsed *core.ParsedDocument) []lsp.Diagnostic {
	index := BuildIndex(parsed)
	var diagnostics []lsp.Diagnostic
	seen := map[string]struct{}{}
	source := core.SourceDocument(parsed)
	diagnostics = append(diagnostics, syntaxDiagnostics(parsed, source)...)
	for lower, symbol := range index.Declarations {
		if symbol.Kind != "variable" {
			continue
		}
		if len(index.Occurrences[lower]) > 1 {
			continue
		}
		if !isBlockScopedJSDeclaration(parsed, source.OffsetAt(symbol.Range.Start)) {
			continue
		}
		diagnostics = append(diagnostics, unusedDiagnostic(symbol.Name, symbol.Range))
		seen[rangeKey(symbol.Range)] = struct{}{}
	}
	for _, region := range parsed.Regions {
		if region.Language != core.LanguageJavaScript && region.Language != core.LanguageJScript {
			continue
		}
		text := parsed.Text[region.ContentStart:region.ContentEnd]
		for _, match := range varDeclarationPattern.FindAllStringSubmatchIndex(text, -1) {
			nameStart := region.ContentStart + match[2]
			nameEnd := region.ContentStart + match[3]
			r := source.Range(nameStart, nameEnd)
			if _, ok := seen[rangeKey(r)]; ok {
				continue
			}
			name := parsed.Text[nameStart:nameEnd]
			if countJSIdentifierInRegions(parsed, name) > 1 {
				continue
			}
			if !isBlockScopedJSDeclaration(parsed, nameStart) {
				continue
			}
			diagnostics = append(diagnostics, unusedDiagnostic(name, r))
			seen[rangeKey(r)] = struct{}{}
		}
	}
	return diagnostics
}

func syntaxDiagnostics(parsed *core.ParsedDocument, source *core.TextDocument) []lsp.Diagnostic {
	diagnostics := []lsp.Diagnostic{}
	for _, region := range parsed.Regions {
		if region.Language != core.LanguageJavaScript && region.Language != core.LanguageJScript {
			continue
		}
		text := parsed.Text[region.ContentStart:region.ContentEnd]
		for _, match := range missingExpressionAssignmentPattern.FindAllStringIndex(text, -1) {
			start := region.ContentStart + match[0] + 1
			end := region.ContentStart + match[1] - 1
			for start < end && (parsed.Text[start] == ' ' || parsed.Text[start] == '\t' || parsed.Text[start] == '\r' || parsed.Text[start] == '\n') {
				start++
			}
			diagnostics = append(diagnostics, lsp.Diagnostic{
				Range:    source.Range(start, start),
				Severity: lsp.DiagnosticSeverityError,
				Source:   "asp-lsp-typescript",
				Message:  "Expression expected.",
			})
		}
	}
	return diagnostics
}

func isBlockScopedJSDeclaration(parsed *core.ParsedDocument, offset int) bool {
	for _, region := range parsed.Regions {
		if region.Language != core.LanguageJavaScript && region.Language != core.LanguageJScript {
			continue
		}
		if offset < region.ContentStart || offset > region.ContentEnd {
			continue
		}
		return jsBraceDepth(parsed.Text, region.ContentStart, offset) > 0
	}
	return false
}

func jsBraceDepth(text string, start int, end int) int {
	depth := 0
	quote := byte(0)
	for i := start; i < end && i < len(text); i++ {
		ch := text[i]
		if quote != 0 {
			if ch == quote && !jsIsEscaped(text, i) {
				quote = 0
			}
			continue
		}
		switch ch {
		case '"', '\'', '`':
			quote = ch
		case '{':
			depth++
		case '}':
			if depth > 0 {
				depth--
			}
		}
	}
	return depth
}

func jsIsEscaped(text string, offset int) bool {
	backslashes := 0
	for i := offset - 1; i >= 0 && text[i] == '\\'; i-- {
		backslashes++
	}
	return backslashes%2 == 1
}

func SemanticDiagnostics(parsed *core.ParsedDocument) []lsp.Diagnostic {
	source := core.SourceDocument(parsed)
	var diagnostics []lsp.Diagnostic
	seen := map[string]struct{}{}
	for _, language := range []core.EmbeddedLanguage{core.LanguageJavaScript, core.LanguageJScript} {
		virtual := core.BuildVirtualDocument(parsed, language)
		if virtual.Text == "" {
			continue
		}
		analysis := tsgoadapter.AnalyzeJavaScript(virtual.Text)
		declared := declaredIdentifiers(virtual.Text, analysis.Identifiers)
		for i, identifier := range analysis.Identifiers {
			if _, ok := declared[strings.ToLower(identifier.Text)]; ok {
				continue
			}
			if isKnownGlobal(identifier.Text) || isIgnoredSemanticIdentifier(virtual.Text, analysis.Identifiers, i) {
				continue
			}
			r, ok := virtual.SourceRange(source, identifier.Start, identifier.End)
			if !ok {
				continue
			}
			key := identifier.Text + ":" + rangeKey(r)
			if _, ok := seen[key]; ok {
				continue
			}
			diagnostics = append(diagnostics, lsp.Diagnostic{
				Range:    r,
				Severity: lsp.DiagnosticSeverityError,
				Source:   "asp-lsp-typescript",
				Message:  "Cannot find name '" + identifier.Text + "'.",
			})
			seen[key] = struct{}{}
		}
	}
	return diagnostics
}

func unusedDiagnostic(name string, r lsp.Range) lsp.Diagnostic {
	return lsp.Diagnostic{
		Range:    r,
		Severity: lsp.DiagnosticSeverityHint,
		Source:   "asp-lsp-typescript-unused",
		Message:  "'" + name + "' is declared but its value is never read.",
		Tags:     []lsp.DiagnosticTag{lsp.DiagnosticTagUnnecessary},
	}
}

func countJSIdentifierInRegions(parsed *core.ParsedDocument, name string) int {
	pattern := regexp.MustCompile(`\b` + regexp.QuoteMeta(name) + `\b`)
	count := 0
	for _, region := range parsed.Regions {
		if region.Language != core.LanguageJavaScript && region.Language != core.LanguageJScript {
			continue
		}
		count += len(pattern.FindAllStringIndex(parsed.Text[region.ContentStart:region.ContentEnd], -1))
	}
	return count
}

func declaredIdentifiers(text string, identifiers []tsgoadapter.Identifier) map[string]struct{} {
	declared := map[string]struct{}{}
	for i, identifier := range identifiers {
		if declarationKind(text, identifiers, i) != "" {
			declared[strings.ToLower(identifier.Text)] = struct{}{}
		}
	}
	for _, match := range functionPattern.FindAllStringSubmatch(text, -1) {
		for _, name := range parameterNames(match[2]) {
			declared[strings.ToLower(name)] = struct{}{}
		}
	}
	for _, match := range arrowFunctionParamPattern.FindAllStringSubmatch(text, -1) {
		declared[strings.ToLower(match[1])] = struct{}{}
	}
	return declared
}

func parameterNames(params string) []string {
	var names []string
	for _, raw := range strings.Split(params, ",") {
		name := strings.TrimSpace(raw)
		name = strings.TrimPrefix(name, "...")
		if eq := strings.Index(name, "="); eq >= 0 {
			name = strings.TrimSpace(name[:eq])
		}
		if colon := strings.Index(name, ":"); colon >= 0 {
			name = strings.TrimSpace(name[:colon])
		}
		if name != "" && isValidIdentifier(name) {
			names = append(names, name)
		}
	}
	return names
}

func isIgnoredSemanticIdentifier(text string, identifiers []tsgoadapter.Identifier, index int) bool {
	identifier := identifiers[index]
	if declarationKind(text, identifiers, index) != "" {
		return true
	}
	if previousNonSpace(text, identifier.Start) == '.' {
		return true
	}
	if nextNonSpace(text, identifier.End) == ':' {
		return true
	}
	prev := strings.ToLower(previousWord(text, identifier.Start))
	switch prev {
	case "function", "class", "const", "let", "var", "new", "catch", "import", "from", "as":
		return true
	default:
		return false
	}
}

func previousNonSpace(text string, offset int) byte {
	for i := offset - 1; i >= 0; i-- {
		if !isSpace(text[i]) {
			return text[i]
		}
	}
	return 0
}

func nextNonSpace(text string, offset int) byte {
	for i := offset; i < len(text); i++ {
		if !isSpace(text[i]) {
			return text[i]
		}
	}
	return 0
}

func isKnownGlobal(name string) bool {
	switch name {
	case "Array", "Boolean", "Date", "Element", "Error", "Event", "HTMLElement", "Intl", "JSON", "Map", "Math", "Number", "Object", "Promise", "RegExp", "Set", "String", "URL", "console", "document", "globalThis", "localStorage", "location", "navigator", "sessionStorage", "undefined", "window":
		return true
	default:
		return false
	}
}

func rangeKey(r lsp.Range) string {
	return strconv.Itoa(r.Start.Line) + ":" + strconv.Itoa(r.Start.Character) + ":" + strconv.Itoa(r.End.Line) + ":" + strconv.Itoa(r.End.Character)
}
