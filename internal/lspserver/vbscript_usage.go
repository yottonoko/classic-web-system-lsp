package lspserver

import (
	"regexp"
	"sort"
	"strconv"
	"strings"

	"github.com/yottonoko/classic-web-system-lsp/internal/core"
	"github.com/yottonoko/classic-web-system-lsp/internal/lsp"
	"github.com/yottonoko/classic-web-system-lsp/internal/vbscript"
)

type vbUsageDeclaration struct {
	Name          string
	Kind          string
	Range         lsp.Range
	Start         int
	End           int
	Line          int
	Local         bool
	Scope         string
	Implicit      bool
	Uncertain     bool
	AssignedValue string
	TypeName      string
	MemberOf      string
	ProcedureKind string
}

type vbIdentifierSpan struct {
	Start int
	End   int
}

type vbSegment struct {
	Start int
	End   int
}

// vbProcedureLinePattern is retained for declaration-name consumers that do
// not need parameter offsets. Parameter-aware parsing uses ProcedureHeaderAt.
var vbProcedureLinePattern = regexp.MustCompile(`(?i)^\s*(?:Public\s+|Private\s+)?(Sub|Function)\s+([A-Za-z_][A-Za-z0-9_]*)\s*(?:\(([^)]*)\))?`)

type vbProcedureHeader struct {
	NameStart   int
	NameEnd     int
	ParamsStart int
	ParamsEnd   int
	Accessor    string
	Kind        string
	HasParams   bool
}

func vbProcedureHeaderAtLine(line string) (vbProcedureHeader, bool) {
	header, ok := vbscript.ProcedureHeaderAt(line)
	if !ok {
		return vbProcedureHeader{}, false
	}
	return vbProcedureHeader{
		NameStart:   header.NameStart,
		NameEnd:     header.NameEnd,
		ParamsStart: header.ParamsStart,
		ParamsEnd:   header.ParamsEnd,
		Accessor:    header.Accessor,
		Kind:        header.Kind,
		HasParams:   header.HasParameterList,
	}, true
}

func vbContinuationToken(tokens []vbscript.Token, index int) bool {
	if index < 0 || index >= len(tokens) || tokens[index].Text != "_" {
		return false
	}
	next := index + 1
	for next < len(tokens) && tokens[next].Kind == "whitespace" {
		next++
	}
	return next < len(tokens) && tokens[next].Kind == "newline"
}

func vbProcedureParameterDisplay(text string) string {
	display := make([]byte, 0, len(text))
	for cursor := 0; cursor < len(text); {
		if text[cursor] == '"' {
			start := cursor
			cursor++
			for cursor < len(text) {
				if text[cursor] != '"' {
					cursor++
					continue
				}
				cursor++
				if cursor < len(text) && text[cursor] == '"' {
					cursor++
					continue
				}
				break
			}
			display = append(display, text[start:cursor]...)
			continue
		}
		if text[cursor] == '_' {
			next := cursor + 1
			for next < len(text) && (text[next] == ' ' || text[next] == '\t') {
				next++
			}
			if next < len(text) && (text[next] == '\r' || text[next] == '\n') {
				for len(display) > 0 && (display[len(display)-1] == ' ' || display[len(display)-1] == '\t') {
					display = display[:len(display)-1]
				}
				cursor = next + 1
				if text[next] == '\r' && cursor < len(text) && text[cursor] == '\n' {
					cursor++
				}
				for cursor < len(text) && (text[cursor] == ' ' || text[cursor] == '\t') {
					cursor++
				}
				if cursor < len(text) && len(display) > 0 {
					display = append(display, ' ')
				}
				continue
			}
		}
		display = append(display, text[cursor])
		cursor++
	}
	return strings.TrimSpace(string(display))
}

func vbPhysicalLineEnd(text string, start, limit int) int {
	if limit > len(text) {
		limit = len(text)
	}
	for end := start; end < limit; end++ {
		if text[end] == '\r' || text[end] == '\n' {
			return end
		}
	}
	return limit
}

func vbNextLineStart(text string, lineEnd, limit int) int {
	if lineEnd >= limit {
		return lineEnd
	}
	lineStart := lineEnd + 1
	if text[lineEnd] == '\r' && lineStart < limit && text[lineStart] == '\n' {
		lineStart++
	}
	return lineStart
}

func vbUnterminatedProcedureHeaderBeforeColon(line string, start, end int) bool {
	if start < 0 || end <= start || end > len(line) {
		return false
	}
	header, ok := vbscript.ProcedureHeaderAt(line[start:end])
	return ok && header.HasParameterList && !header.ParameterListComplete
}

func vbscriptUsageDiagnosticsWithGlobals(parsed *core.ParsedDocument, locale string, externalGlobals map[string]struct{}) []lsp.Diagnostic {
	usage := collectVBUsageDeclarations(parsed)
	usage.Declarations = append(append([]vbUsageDeclaration(nil), usage.Declarations...), serverObjectDeclarations(parsed)...)
	diagnostics := unusedVBScriptDiagnostics(parsed, usage)
	if hasVBOptionExplicit(parsed) {
		diagnostics = append(diagnostics, undeclaredVBScriptDiagnostics(parsed, usage, locale, externalGlobals)...)
	}
	return dedupeDiagnostics(diagnostics)
}

func implicitGlobalVBScriptDiagnostics(parsed *core.ParsedDocument, locale string, externalGlobals map[string]struct{}) []lsp.Diagnostic {
	var diagnostics []lsp.Diagnostic
	for _, declaration := range implicitVBDeclarations(parsed) {
		if declaration.Local {
			continue
		}
		if _, external := externalGlobals[strings.ToLower(declaration.Name)]; external {
			continue
		}
		message := "'" + declaration.Name + "' is implicitly declared as a global variable because Option Explicit is disabled."
		if locale == "ja" {
			message = "'" + declaration.Name + "' は Option Explicit が無効なため、暗黙の global 変数として宣言されています。"
		}
		diagnostics = append(diagnostics, lsp.Diagnostic{
			Range:    declaration.Range,
			Severity: lsp.DiagnosticSeverityWarning,
			Source:   "asp-lsp-vbscript-implicit-global",
			Message:  message,
			Data:     map[string]any{"name": declaration.Name},
		})
	}
	return diagnostics
}

func (s *Server) vbscriptUsageCodeActions(params codeActionParams) []lsp.CodeAction {
	doc, parsed := s.parsed(params.TextDocument.URI)
	if doc == nil || parsed == nil {
		return nil
	}
	usage := collectVBUsageDeclarations(parsed)
	byRange := map[string]vbUsageDeclaration{}
	for _, declaration := range usage.Declarations {
		byRange[diagnosticRangeKey(declaration.Range)] = declaration
	}
	var actions []lsp.CodeAction
	for _, diagnostic := range params.Context.Diagnostics {
		switch diagnostic.Source {
		case "asp-lsp-vbscript":
			name := strings.TrimSpace(textInRange(doc, diagnostic.Range))
			if name == "" {
				continue
			}
			diag := diagnostic
			line := diagnostic.Range.Start.Line
			actions = append(actions, lsp.CodeAction{
				Title:       s.declareDimTitle(name),
				Kind:        "quickfix",
				Diagnostics: []lsp.Diagnostic{diag},
				Edit: &lsp.WorkspaceEdit{Changes: map[string][]lsp.TextEdit{
					params.TextDocument.URI: {{
						Range:   lsp.Range{Start: lsp.Position{Line: line, Character: 0}, End: lsp.Position{Line: line, Character: 0}},
						NewText: "Dim " + name + "\n",
					}},
				}},
			})
		case "asp-lsp-vbscript-unused":
			declaration, ok := byRange[diagnosticRangeKey(diagnostic.Range)]
			if !ok || declaration.Kind == "sub" || declaration.Kind == "function" || declaration.Kind == "class" {
				continue
			}
			edit, ok := unusedDeclarationRemovalEdit(doc, declaration)
			if !ok {
				continue
			}
			diag := diagnostic
			actions = append(actions, lsp.CodeAction{
				Title:       "Remove unused declaration " + declaration.Name,
				Kind:        "quickfix",
				Diagnostics: []lsp.Diagnostic{diag},
				Edit: &lsp.WorkspaceEdit{Changes: map[string][]lsp.TextEdit{
					params.TextDocument.URI: {edit},
				}},
			})
		}
	}
	return actions
}

type vbUsageDeclarations struct {
	Declarations []vbUsageDeclaration
}

func collectVBUsageDeclarations(parsed *core.ParsedDocument) vbUsageDeclarations {
	const analysisKey = "lspserver.vb-usage.v2"
	if value, ok := parsed.LoadRuntimeAnalysis(analysisKey); ok {
		if cached, ok := value.(vbUsageDeclarations); ok {
			return cached
		}
	}
	if !parsed.ChangeImpact.Affects(core.LanguageVBScript) {
		if value, ok := parsed.LoadPreviousRuntimeAnalysis(analysisKey); ok {
			if cached, ok := value.(vbUsageDeclarations); ok {
				if core.IncrementalChangeAfterLanguage(parsed, core.LanguageVBScript) {
					parsed.StoreRuntimeAnalysis(analysisKey, cached)
					return cached
				}
				if shifted, ok := remapVBUsageDeclarations(parsed, cached); ok {
					parsed.StoreRuntimeAnalysis(analysisKey, shifted)
					return shifted
				}
			}
		}
	}
	var cached vbUsageDeclarations
	if parsed.LoadAnalysis(analysisKey, &cached) {
		parsed.StoreRuntimeAnalysis(analysisKey, cached)
		return cached
	}
	doc := core.SourceDocument(parsed)
	usage := vbUsageDeclarations{}
	add := func(declaration vbUsageDeclaration) {
		if declaration.Name == "" {
			return
		}
		usage.Declarations = append(usage.Declarations, declaration)
	}
	for _, declaration := range vbscript.BuildSymbolIndex(parsed).Declarations {
		start := doc.OffsetAt(declaration.Range.Start)
		end := doc.OffsetAt(declaration.Range.End)
		add(vbUsageDeclaration{
			Name:  declaration.Name,
			Kind:  declaration.Kind,
			Range: declaration.Range,
			Start: start,
			End:   end,
			Line:  declaration.Range.Start.Line,
			Local: false,
		})
	}
	procedureScopes := vbProcedureScopes(parsed)
	for _, region := range parsed.Regions {
		if region.Language != core.LanguageVBScript {
			continue
		}
		for lineStart := region.ContentStart; lineStart < region.ContentEnd; {
			lineEnd := vbPhysicalLineEnd(parsed.Text, lineStart, region.ContentEnd)
			if _, logicalEnd, ok := vbscript.ProcedureHeaderAtLogical(parsed.Text, lineStart); ok && logicalEnd <= region.ContentEnd {
				lineEnd = logicalEnd
			}
			line := parsed.Text[lineStart:lineEnd]
			for _, statement := range splitVBStatementSegments(line, lineStart) {
				trimmed := statement.Text
				currentScope := vbProcedureScopeAtOffset(procedureScopes, statement.Start)
				inProcedure := currentScope != ""
				if header, ok := vbProcedureHeaderAtLine(trimmed); ok && header.HasParams && header.ParamsEnd > header.ParamsStart {
					for _, declaration := range vbParameterDeclarations(doc, trimmed, statement.Start, header.ParamsStart, header.ParamsEnd, currentScope) {
						add(declaration)
					}
				}
				if inProcedure {
					for _, declaration := range vbLocalLineDeclarations(doc, trimmed, statement.Start, currentScope) {
						add(declaration)
					}
				} else {
					for _, declaration := range vbGlobalLineDeclarations(doc, trimmed, statement.Start) {
						add(declaration)
					}
				}
			}
			if lineEnd >= region.ContentEnd {
				break
			}
			lineStart = vbNextLineStart(parsed.Text, lineEnd, region.ContentEnd)
		}
	}
	sort.SliceStable(usage.Declarations, func(i, j int) bool {
		if usage.Declarations[i].Start != usage.Declarations[j].Start {
			return usage.Declarations[i].Start < usage.Declarations[j].Start
		}
		return usage.Declarations[i].End < usage.Declarations[j].End
	})
	parsed.StoreRuntimeAnalysis(analysisKey, usage)
	parsed.StoreAnalysis(analysisKey, usage)
	return usage
}

func remapVBUsageDeclarations(parsed *core.ParsedDocument, previous vbUsageDeclarations) (vbUsageDeclarations, bool) {
	mapper, ok := core.IncrementalRangeMapperFor(parsed)
	if !ok {
		return vbUsageDeclarations{}, false
	}
	shifted := vbUsageDeclarations{Declarations: make([]vbUsageDeclaration, len(previous.Declarations))}
	for index, declaration := range previous.Declarations {
		declaration.Range, ok = mapper.Range(declaration.Range)
		if !ok {
			return vbUsageDeclarations{}, false
		}
		declaration.Start, ok = mapper.CurrentOffset(declaration.Range.Start)
		if !ok {
			return vbUsageDeclarations{}, false
		}
		declaration.End, ok = mapper.CurrentOffset(declaration.Range.End)
		if !ok {
			return vbUsageDeclarations{}, false
		}
		declaration.Line = declaration.Range.Start.Line
		shifted.Declarations[index] = declaration
	}
	return shifted, true
}

func vbParameterDeclarations(doc *core.TextDocument, line string, lineOffset int, paramsStart int, paramsEnd int, scope string) []vbUsageDeclaration {
	var declarations []vbUsageDeclaration
	for _, segment := range splitVBSegments(line[paramsStart:paramsEnd]) {
		part := line[paramsStart+segment.Start : paramsStart+segment.End]
		nameStart, nameEnd, ok := firstVBParameterIdentifierSpan(part)
		if !ok {
			continue
		}
		start := lineOffset + paramsStart + segment.Start + nameStart
		end := lineOffset + paramsStart + segment.Start + nameEnd
		declarations = append(declarations, vbUsageDeclaration{
			Name:  line[paramsStart+segment.Start+nameStart : paramsStart+segment.Start+nameEnd],
			Kind:  "parameter",
			Range: doc.Range(start, end),
			Start: start,
			End:   end,
			Line:  doc.PositionAt(start).Line,
			Local: true,
			Scope: scope,
		})
	}
	return declarations
}

func firstVBParameterIdentifierSpan(text string) (int, int, bool) {
	tokens := vbscript.Tokenize(text)
	for index, token := range tokens {
		if vbContinuationToken(tokens, index) || token.Kind != "identifier" {
			continue
		}
		switch strings.ToLower(token.Text) {
		case "optional", "byref", "byval", "paramarray":
			continue
		default:
			return token.Start, token.End, true
		}
	}
	return 0, 0, false
}

func vbLocalLineDeclarations(doc *core.TextDocument, line string, lineOffset int, scope string) []vbUsageDeclaration {
	return vbLineDeclarations(doc, line, lineOffset, true, scope)
}

func vbGlobalLineDeclarations(doc *core.TextDocument, line string, lineOffset int) []vbUsageDeclaration {
	return vbLineDeclarations(doc, line, lineOffset, false, "")
}

func vbLineDeclarations(doc *core.TextDocument, line string, lineOffset int, local bool, scope string) []vbUsageDeclaration {
	keywordStart := leadingVBWhitespace(line)
	if keywordStart >= len(line) || line[keywordStart] == '\'' {
		return nil
	}
	keywordEnd := readVBIdentifier(line, keywordStart)
	if keywordEnd == keywordStart {
		return nil
	}
	keyword := strings.ToLower(line[keywordStart:keywordEnd])
	kind := ""
	switch keyword {
	case "dim":
		kind = "variable"
	case "const":
		kind = "constant"
	default:
		return nil
	}
	cursor := keywordEnd
	if cursor >= len(line) || !isVBWhitespace(line[cursor]) {
		return nil
	}
	for cursor < len(line) && isVBWhitespace(line[cursor]) {
		cursor++
	}
	var declarations []vbUsageDeclaration
	for _, segment := range splitVBSegments(line[cursor:]) {
		part := line[cursor+segment.Start : cursor+segment.End]
		nameStart, nameEnd, ok := firstVBIdentifierSpan(part)
		if !ok {
			continue
		}
		start := lineOffset + cursor + segment.Start + nameStart
		end := lineOffset + cursor + segment.Start + nameEnd
		declarations = append(declarations, vbUsageDeclaration{
			Name:  line[cursor+segment.Start+nameStart : cursor+segment.Start+nameEnd],
			Kind:  kind,
			Range: doc.Range(start, end),
			Start: start,
			End:   end,
			Line:  doc.PositionAt(start).Line,
			Local: local,
			Scope: scope,
		})
	}
	return declarations
}

func unusedVBScriptDiagnostics(parsed *core.ParsedDocument, usage vbUsageDeclarations) []lsp.Diagnostic {
	candidates := map[string][]vbUsageDeclaration{}
	var candidateList []vbUsageDeclaration
	declarationRanges := map[string]struct{}{}
	for _, declaration := range usage.Declarations {
		declarationRanges[offsetRangeKey(declaration.Start, declaration.End)] = struct{}{}
		if !declaration.Local || (declaration.Kind != "variable" && declaration.Kind != "constant" && declaration.Kind != "parameter") {
			continue
		}
		candidates[strings.ToLower(declaration.Name)] = append(candidates[strings.ToLower(declaration.Name)], declaration)
		candidateList = append(candidateList, declaration)
	}
	if len(candidates) == 0 {
		return nil
	}
	// Usage is matched by name only, so every same-named candidate shares one
	// use set; tracking names avoids a names x occurrences blowup.
	used := map[string]struct{}{}
	for _, region := range parsed.Regions {
		if region.Language != core.LanguageVBScript {
			continue
		}
		text := parsed.Text[region.ContentStart:region.ContentEnd]
		for _, span := range vbIdentifierSpans(text) {
			start := region.ContentStart + span.Start
			end := region.ContentStart + span.End
			name := strings.ToLower(parsed.Text[start:end])
			if _, ok := candidates[name]; !ok {
				continue
			}
			if _, ok := used[name]; ok {
				continue
			}
			if _, ok := declarationRanges[offsetRangeKey(start, end)]; ok {
				continue
			}
			used[name] = struct{}{}
		}
	}
	var diagnostics []lsp.Diagnostic
	for _, declaration := range candidateList {
		if _, ok := used[strings.ToLower(declaration.Name)]; ok {
			continue
		}
		message := "'" + declaration.Name + "' is declared but never used."
		if declaration.Kind == "parameter" {
			message = "Parameter '" + declaration.Name + "' is never used."
		}
		diagnostics = append(diagnostics, lsp.Diagnostic{
			Range:    declaration.Range,
			Severity: lsp.DiagnosticSeverityHint,
			Source:   "asp-lsp-vbscript-unused",
			Message:  message,
			Tags:     []lsp.DiagnosticTag{lsp.DiagnosticTagUnnecessary},
			Data:     map[string]any{"name": declaration.Name, "kind": declaration.Kind},
		})
	}
	return diagnostics
}

func undeclaredVBScriptDiagnostics(parsed *core.ParsedDocument, usage vbUsageDeclarations, locale string, externalGlobals map[string]struct{}) []lsp.Diagnostic {
	doc := core.SourceDocument(parsed)
	declared := map[string]struct{}{}
	declarationRanges := map[string]struct{}{}
	for _, declaration := range usage.Declarations {
		declared[strings.ToLower(declaration.Name)] = struct{}{}
		declarationRanges[offsetRangeKey(declaration.Start, declaration.End)] = struct{}{}
	}
	for name := range externalGlobals {
		declared[strings.ToLower(name)] = struct{}{}
	}
	var diagnostics []lsp.Diagnostic
	for _, region := range parsed.Regions {
		if region.Language != core.LanguageVBScript {
			continue
		}
		text := parsed.Text[region.ContentStart:region.ContentEnd]
		for _, span := range vbIdentifierSpans(text) {
			start := region.ContentStart + span.Start
			end := region.ContentStart + span.End
			name := parsed.Text[start:end]
			lower := strings.ToLower(name)
			if _, ok := declarationRanges[offsetRangeKey(start, end)]; ok {
				continue
			}
			if _, ok := declared[lower]; ok || isDeclaredVBBuiltinOrKeywordForDocument(parsed, lower) {
				continue
			}
			if previousNonSpace(parsed.Text, start) == '.' {
				continue
			}
			if next := nextNonSpaceByte(parsed.Text, end); next >= 0 && parsed.Text[next] == '(' && name[0] >= 'A' && name[0] <= 'Z' {
				continue
			}
			message := "'" + name + "' is not declared under Option Explicit."
			if locale == "ja" {
				message = "'" + name + "' は Option Explicit のもとで宣言されていません。"
			}
			diagnostics = append(diagnostics, lsp.Diagnostic{
				Range:    doc.Range(start, end),
				Severity: lsp.DiagnosticSeverityWarning,
				Source:   "asp-lsp-vbscript",
				Message:  message,
				Data:     map[string]any{"name": name},
			})
		}
	}
	return diagnostics
}

func hasVBOptionExplicit(parsed *core.ParsedDocument) bool {
	const analysisKey = "lspserver.vb-option-explicit.v1"
	if value, ok := parsed.LoadRuntimeAnalysis(analysisKey); ok {
		if cached, ok := value.(bool); ok {
			return cached
		}
	}
	var cached bool
	if parsed.LoadAnalysis(analysisKey, &cached) {
		parsed.StoreRuntimeAnalysis(analysisKey, cached)
		return cached
	}
	for _, region := range parsed.Regions {
		if region.Language != core.LanguageVBScript {
			continue
		}
		text := parsed.Text[region.ContentStart:region.ContentEnd]
		for _, span := range vbIdentifierSpans(text) {
			if !strings.EqualFold(text[span.Start:span.End], "option") {
				continue
			}
			next := nextVBIdentifierSpan(text, span.End)
			if next.Start >= 0 && strings.EqualFold(text[next.Start:next.End], "explicit") {
				parsed.StoreRuntimeAnalysis(analysisKey, true)
				parsed.StoreAnalysis(analysisKey, true)
				return true
			}
		}
	}
	parsed.StoreRuntimeAnalysis(analysisKey, false)
	parsed.StoreAnalysis(analysisKey, false)
	return false
}

func nextVBIdentifierSpan(text string, offset int) vbIdentifierSpan {
	for offset < len(text) {
		if isVBIdentifierStart(text[offset]) {
			end := readVBIdentifier(text, offset)
			return vbIdentifierSpan{Start: offset, End: end}
		}
		offset++
	}
	return vbIdentifierSpan{Start: -1, End: -1}
}

func isDeclaredVBBuiltinOrKeyword(lower string) bool {
	if lower == "_" {
		return true
	}
	_, ok := vbBuiltinsAndKeywords[lower]
	return ok
}

func isDeclaredVBBuiltinOrKeywordForDocument(parsed *core.ParsedDocument, lower string) bool {
	if lower == "_" {
		return true
	}
	if isStandaloneVBScriptDocument(parsed) {
		_, ok := standaloneVBBuiltinsAndKeywords[lower]
		return ok
	}
	return isDeclaredVBBuiltinOrKeyword(lower)
}

var vbBuiltinsAndKeywords = map[string]struct{}{
	"and": {}, "application": {}, "array": {}, "as": {}, "ascb": {}, "ascw": {}, "byref": {}, "byval": {}, "call": {}, "case": {}, "class": {},
	"cbyte": {}, "ccur": {}, "cdate": {}, "cdbl": {}, "cdec": {}, "chrb": {}, "chrw": {}, "cint": {}, "clng": {}, "const": {}, "createobject": {}, "csng": {}, "cstr": {}, "cvar": {}, "cverr": {}, "date": {}, "datepart": {}, "default": {}, "dim": {}, "do": {}, "each": {}, "else": {}, "elseif": {}, "empty": {}, "end": {},
	"eqv": {}, "erase": {}, "err": {}, "error": {}, "exit": {}, "explicit": {}, "false": {}, "for": {}, "function": {}, "goto": {}, "if": {}, "imp": {}, "in": {},
	"is": {}, "loop": {}, "me": {}, "mod": {}, "new": {}, "next": {}, "not": {}, "nothing": {}, "null": {}, "on": {},
	"get": {}, "let": {}, "option": {}, "or": {}, "preserve": {}, "private": {}, "property": {}, "public": {}, "randomize": {}, "redim": {}, "rem": {}, "resume": {},
	"formatcurrency": {}, "getlocale": {}, "getobject": {}, "getref": {}, "inputbox": {}, "instrb": {}, "isobject": {}, "join": {}, "leftb": {}, "lenb": {}, "loadpicture": {}, "midb": {}, "msgbox": {}, "regexp": {}, "request": {}, "response": {}, "rightb": {}, "select": {}, "server": {}, "session": {}, "set": {}, "setlocale": {}, "split": {}, "step": {}, "sub": {},
	"stop": {}, "then": {}, "timer": {}, "to": {}, "true": {}, "ubound": {}, "until": {}, "vartype": {}, "wend": {}, "while": {}, "with": {}, "xor": {},
	"adinteger": {}, "vbapplicationmodal": {}, "vbcr": {}, "vbcrlf": {}, "vbcritical": {}, "vbdefaultbutton1": {}, "vbdefaultbutton2": {},
	"vbdefaultbutton3": {}, "vbdefaultbutton4": {}, "vbexclamation": {}, "vbfalse": {}, "vbformfeed": {}, "vbinformation": {},
	"vblf": {}, "vbnewline": {}, "vbnullchar": {}, "vbnullstring": {}, "vbokcancel": {}, "vbokonly": {}, "vbquestion": {},
	"vbobjecterror": {}, "vbretrycancel": {}, "vbsystemmodal": {}, "vbtab": {}, "vbtextcompare": {}, "vbtrue": {}, "vbusedefault": {}, "vbverticaltab": {},
	"vbyesno": {}, "vbyesnocancel": {},
}

var standaloneVBBuiltinsAndKeywords = map[string]struct{}{
	"and": {}, "array": {}, "as": {}, "ascb": {}, "ascw": {}, "byref": {}, "byval": {}, "call": {}, "case": {}, "class": {},
	"cbyte": {}, "ccur": {}, "cdate": {}, "cdbl": {}, "cdec": {}, "chrb": {}, "chrw": {}, "cint": {}, "clng": {}, "const": {}, "createobject": {}, "csng": {}, "cstr": {}, "cvar": {}, "cverr": {}, "date": {}, "datepart": {}, "default": {}, "dim": {}, "do": {}, "each": {}, "else": {}, "elseif": {}, "empty": {}, "end": {},
	"eqv": {}, "erase": {}, "err": {}, "error": {}, "exit": {}, "explicit": {}, "false": {}, "for": {}, "function": {}, "goto": {}, "if": {}, "imp": {}, "in": {},
	"is": {}, "loop": {}, "me": {}, "mod": {}, "new": {}, "next": {}, "not": {}, "nothing": {}, "null": {}, "on": {},
	"get": {}, "let": {}, "option": {}, "or": {}, "preserve": {}, "private": {}, "property": {}, "public": {}, "randomize": {}, "redim": {}, "rem": {}, "resume": {},
	"formatcurrency": {}, "getlocale": {}, "getobject": {}, "getref": {}, "inputbox": {}, "instrb": {}, "isobject": {}, "join": {}, "leftb": {}, "lenb": {}, "loadpicture": {}, "midb": {}, "msgbox": {}, "regexp": {}, "rightb": {}, "select": {}, "set": {}, "setlocale": {}, "split": {}, "step": {}, "sub": {},
	"stop": {}, "then": {}, "timer": {}, "to": {}, "true": {}, "ubound": {}, "until": {}, "vartype": {}, "wend": {}, "while": {}, "with": {}, "wscript": {}, "xor": {},
	"adinteger":          {},
	"vbapplicationmodal": {}, "vbcr": {}, "vbcrlf": {}, "vbcritical": {}, "vbdefaultbutton1": {}, "vbdefaultbutton2": {},
	"vbdefaultbutton3": {}, "vbdefaultbutton4": {}, "vbexclamation": {}, "vbfalse": {}, "vbformfeed": {}, "vbinformation": {},
	"vblf": {}, "vbnewline": {}, "vbnullchar": {}, "vbnullstring": {}, "vbokcancel": {}, "vbokonly": {}, "vbquestion": {},
	"vbobjecterror": {}, "vbretrycancel": {}, "vbsystemmodal": {}, "vbtab": {}, "vbtrue": {}, "vbusedefault": {}, "vbverticaltab": {},
	"vbyesno": {}, "vbyesnocancel": {},
}

func init() {
	addVBScriptBuiltinCatalogKeys(vbBuiltinsAndKeywords)
	addVBScriptBuiltinCatalogKeys(standaloneVBBuiltinsAndKeywords)
}

func addVBScriptBuiltinCatalogKeys(target map[string]struct{}) {
	for _, key := range vbscript.BuiltinIdentifierKeys() {
		target[key] = struct{}{}
	}
}

func vbIdentifierSpans(text string) []vbIdentifierSpan {
	spans := make([]vbIdentifierSpan, 0, 16)
	for i := 0; i < len(text); {
		switch text[i] {
		case '"':
			i = skipVBString(text, i)
		case '\'':
			i = skipVBLine(text, i)
		default:
			if end, ok := skipVBNumericLiteral(text, i); ok {
				i = end
				continue
			}
			if isVBIdentifierStart(text[i]) {
				start := i
				end := readVBIdentifier(text, start)
				if isVBRemComment(text, start, end) {
					i = skipVBLine(text, end)
					continue
				}
				spans = append(spans, vbIdentifierSpan{Start: start, End: end})
				i = end
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
	return offset, false
}

func isASCIIHexDigit(value byte) bool {
	return value >= '0' && value <= '9' || value >= 'A' && value <= 'F' || value >= 'a' && value <= 'f'
}

func isVBRemComment(text string, start int, end int) bool {
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

func skipVBLine(text string, offset int) int {
	for offset < len(text) && text[offset] != '\n' && text[offset] != '\r' {
		offset++
	}
	return offset
}

func splitVBSegments(text string) []vbSegment {
	var segments []vbSegment
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
				segments = appendTrimmedVBSegment(segments, text, start, i)
				start = i + 1
			}
		}
	}
	return appendTrimmedVBSegment(segments, text, start, len(text))
}

func appendTrimmedVBSegment(segments []vbSegment, text string, start int, end int) []vbSegment {
	for start < end && isVBWhitespace(text[start]) {
		start++
	}
	for end > start && isVBWhitespace(text[end-1]) {
		end--
	}
	if start < end {
		segments = append(segments, vbSegment{Start: start, End: end})
	}
	return segments
}

func firstVBIdentifierSpan(text string) (int, int, bool) {
	for i := 0; i < len(text); i++ {
		if isVBIdentifierStart(text[i]) {
			return i, readVBIdentifier(text, i), true
		}
	}
	return 0, 0, false
}

func previousNonSpace(text string, offset int) byte {
	for i := offset - 1; i >= 0; i-- {
		if text[i] == ' ' || text[i] == '\t' || text[i] == '\r' || text[i] == '\n' {
			continue
		}
		return text[i]
	}
	return 0
}

func nextNonSpaceByte(text string, offset int) int {
	for offset < len(text) {
		if text[offset] != ' ' && text[offset] != '\t' && text[offset] != '\r' && text[offset] != '\n' {
			return offset
		}
		offset++
	}
	return -1
}

func unusedDeclarationRemovalEdit(doc *core.TextDocument, declaration vbUsageDeclaration) (lsp.TextEdit, bool) {
	if declaration.Kind == "parameter" {
		return removeVBParameterEdit(doc, declaration)
	}
	return removeLineEdit(doc, declaration.Line), true
}

func removeVBParameterEdit(doc *core.TextDocument, declaration vbUsageDeclaration) (lsp.TextEdit, bool) {
	lineStart := doc.OffsetAt(lsp.Position{Line: declaration.Line, Character: 0})
	lineEnd := lineEndOffset(doc.Text, lineStart)
	removeStart := parameterRemovalStartOffset(doc.Text, declaration.Start, lineStart)
	removeEnd := declaration.End
	after := doc.Text[removeEnd:lineEnd]
	before := doc.Text[lineStart:removeStart]
	if match := leadingCommaPattern.FindString(after); match != "" {
		removeEnd += len(match)
	} else if loc := trailingCommaPattern.FindStringIndex(before); loc != nil && loc[1] == len(before) {
		removeStart = lineStart + loc[0]
	}
	if removeStart < lineStart || removeEnd > lineEnd || removeStart >= removeEnd {
		return lsp.TextEdit{}, false
	}
	return lsp.TextEdit{Range: doc.Range(removeStart, removeEnd), NewText: ""}, true
}

var leadingCommaPattern = regexp.MustCompile(`^\s*,\s*`)
var trailingCommaPattern = regexp.MustCompile(`,\s*$`)
var parameterKeywordPrefixPattern = regexp.MustCompile(`(?i)(?:^|\s)((?:(?:Optional|ByRef|ByVal)\s+)+)$`)

func parameterRemovalStartOffset(text string, startOffset int, lineStart int) int {
	before := text[lineStart:startOffset]
	segmentStart := strings.LastIndex(before, "(")
	if comma := strings.LastIndex(before, ","); comma > segmentStart {
		segmentStart = comma
	}
	segmentStart++
	segmentPrefix := before[segmentStart:]
	match := parameterKeywordPrefixPattern.FindStringSubmatchIndex(segmentPrefix)
	if match == nil {
		return startOffset
	}
	return lineStart + segmentStart + match[2]
}

func removeLineEdit(doc *core.TextDocument, line int) lsp.TextEdit {
	end := lsp.Position{Line: line + 1, Character: 0}
	if line+1 >= documentLineCount(doc.Text) {
		end = lsp.Position{Line: line, Character: lineTextLength(doc, line)}
	}
	return lsp.TextEdit{
		Range:   lsp.Range{Start: lsp.Position{Line: line, Character: 0}, End: end},
		NewText: "",
	}
}

func documentLineCount(text string) int {
	count := 1
	for i := 0; i < len(text); i++ {
		if text[i] == '\n' {
			count++
		}
	}
	return count
}

func lineTextLength(doc *core.TextDocument, line int) int {
	start := doc.OffsetAt(lsp.Position{Line: line, Character: 0})
	end := lineEndOffset(doc.Text, start)
	return doc.PositionAt(end).Character
}

func lineEndOffset(text string, start int) int {
	for start < len(text) && text[start] != '\n' && text[start] != '\r' {
		start++
	}
	return start
}

func textInRange(doc *core.TextDocument, r lsp.Range) string {
	start := doc.OffsetAt(r.Start)
	end := doc.OffsetAt(r.End)
	if start < 0 {
		start = 0
	}
	if end > len(doc.Text) {
		end = len(doc.Text)
	}
	if start > end {
		start, end = end, start
	}
	return doc.Text[start:end]
}

func dedupeDiagnostics(diagnostics []lsp.Diagnostic) []lsp.Diagnostic {
	seen := map[string]struct{}{}
	deduped := make([]lsp.Diagnostic, 0, len(diagnostics))
	for _, diagnostic := range diagnostics {
		key := diagnostic.Source + "|" + diagnosticDataText(diagnostic, "uri") + "|" + diagnosticRangeKey(diagnostic.Range) + "|" + diagnostic.Message
		if _, ok := seen[key]; ok {
			continue
		}
		seen[key] = struct{}{}
		deduped = append(deduped, diagnostic)
	}
	return deduped
}

func diagnosticRangeKey(r lsp.Range) string {
	return offsetRangeKey(r.Start.Line*1_000_000+r.Start.Character, r.End.Line*1_000_000+r.End.Character)
}

func offsetRangeKey(start int, end int) string {
	return strconvItoa(start) + ":" + strconvItoa(end)
}

func strconvItoa(value int) string {
	if value == 0 {
		return "0"
	}
	return strconv.FormatInt(int64(value), 10)
}
