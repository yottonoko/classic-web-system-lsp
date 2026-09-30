package lspserver

import (
	"context"
	"reflect"
	"strings"

	"github.com/yottonoko/classic-web-system-lsp/internal/core"
	"github.com/yottonoko/classic-web-system-lsp/internal/lsp"
	"github.com/yottonoko/classic-web-system-lsp/internal/vbscript"
)

type vbClassMemberCompletion struct {
	Name                  string
	Kind                  lsp.CompletionItemKind
	TypeName              string
	ParameterCount        int
	MinimumParameterCount int
	MaximumParameterCount int
	ParameterRangeKnown   bool
}

type vbCompletionMemberTarget struct {
	owner    string
	explicit bool
}

const vbTypedMemberCompletionCacheAnalysisKey = "lspserver.vb-typed-member-completion-cache.v1"

type vbTypedMemberCompletionCache struct {
	owner    string
	offset   int
	globals  map[string]vbscriptGlobalSetting
	comTypes map[string]vbscriptComTypeSetting
	items    []lsp.CompletionItem
}

func (cache *vbTypedMemberCompletionCache) matches(owner string, offset int, settings serverSettings) bool {
	return cache != nil && cache.owner == strings.ToLower(owner) && cache.offset == offset &&
		reflect.DeepEqual(cache.globals, settings.VBScriptGlobals) && reflect.DeepEqual(cache.comTypes, settings.VBScriptComTypes)
}

func vbscriptParameterRange(parameters []vbscript.Parameter) (int, int) {
	minimum := 0
	for _, parameter := range parameters {
		if !parameter.Optional {
			minimum++
		}
	}
	return minimum, len(parameters)
}

func (s *Server) vbscriptTypedMemberCompletions(parsed *core.ParsedDocument, owner string, offset int) []lsp.CompletionItem {
	return s.vbscriptTypedMemberCompletionsContext(context.Background(), parsed, owner, offset)
}

func (s *Server) vbscriptTypedMemberCompletionsContext(ctx context.Context, parsed *core.ParsedDocument, owner string, offset int) []lsp.CompletionItem {
	if ctx == nil {
		ctx = context.Background()
	}
	if ctx.Err() != nil {
		return nil
	}
	if parsed == nil || owner == "" {
		return nil
	}
	if value, ok := parsed.LoadRuntimeAnalysis(vbTypedMemberCompletionCacheAnalysisKey); ok {
		if cache, ok := value.(*vbTypedMemberCompletionCache); ok && cache.matches(owner, offset, s.settings) {
			return append([]lsp.CompletionItem(nil), cache.items...)
		}
	}
	if items, handled := s.vbscriptDirectLocalMemberCompletionsContext(ctx, parsed, owner, offset); handled {
		if ctx.Err() != nil {
			return nil
		}
		parsed.StoreRuntimeAnalysis(vbTypedMemberCompletionCacheAnalysisKey, &vbTypedMemberCompletionCache{
			owner:    strings.ToLower(owner),
			offset:   offset,
			globals:  cloneVBScriptGlobals(s.settings.VBScriptGlobals),
			comTypes: cloneVBScriptComTypes(s.settings.VBScriptComTypes),
			items:    append([]lsp.CompletionItem(nil), items...),
		})
		return items
	}
	info, complete := s.vbscriptTypeInfoAtOffsetContext(ctx, parsed, offset)
	if !complete || ctx.Err() != nil {
		return nil
	}
	scope := vbscriptScopeAtOffset(parsed, offset)
	typeName := info.scopedVariableTypeNames[vbscriptTypeScopeKey(parsed, scope, owner)]
	bound := vbscriptNameBoundInScopeAtOffset(parsed, owner, scope, offset)
	localShadow := scope != "" && bound
	if typeName == "" && !localShadow && (scope == "" || !bound) {
		typeName = info.variableTypes[strings.ToLower(owner)]
	}
	if typeName == "" && !localShadow && !bound {
		// Built-in ASP objects are available without a declaration or assignment.
		// Resolve them directly so leading-dot With completion remains available
		// without reopening a source-wide type scan for ordinary variables.
		if builtinType, ok := vbscriptBuiltinGlobalObjectType(parsed, owner); ok {
			typeName = builtinType
		}
	}
	// The offset-aware state is authoritative for position-sensitive requests.
	// An empty type means that no type is visible here (including after a
	// dynamic assignment); do not recover a future/full-document inference.
	if typeName == "" {
		return nil
	}
	typeNames := vbscriptConcreteTypeNames(typeName)
	membersByType := info.members
	membersCopied := false
	for _, typeName := range typeNames {
		key := strings.ToLower(typeName)
		builtinMembers := vbscriptBuiltinTypeMembers(typeName)
		if len(builtinMembers) == 0 {
			continue
		}
		// Cached position states are immutable. Copy only the requested type's
		// member map before supplementing it with built-ins.
		if !membersCopied {
			membersByType = make(map[string]map[string]vbscriptTypedMember, len(info.members))
			for memberType, members := range info.members {
				membersByType[memberType] = members
			}
			membersCopied = true
		}
		members := membersByType[key]
		merged := make(map[string]vbscriptTypedMember, len(members)+len(builtinMembers))
		for memberName, member := range members {
			merged[memberName] = member
		}
		for memberName, member := range builtinMembers {
			if _, exists := merged[memberName]; !exists {
				merged[memberName] = vbscriptTypedMemberFromBuiltin(member)
			}
		}
		membersByType[key] = merged
	}
	if len(typeNames) == 0 {
		return nil
	}
	return s.vbscriptTypedMemberCompletionsFromTypesContext(ctx, typeNames, membersByType)
}

// vbscriptDirectLocalMemberCompletionsContext handles the common cold path for
// a local variable assigned directly from one completed source class. It does
// not publish partial type state: annotations, includes, unions, aliases, and
// dynamic values fall back to the full source-ordered evaluator.
func (s *Server) vbscriptDirectLocalMemberCompletionsContext(ctx context.Context, parsed *core.ParsedDocument, owner string, offset int) ([]lsp.CompletionItem, bool) {
	if ctx == nil {
		ctx = context.Background()
	}
	if parsed == nil || ctx.Err() != nil || len(parsed.Includes) != 0 || readVBIdentifier(owner, 0) != len(owner) {
		return nil, false
	}
	if offset < 0 {
		offset = 0
	}
	if offset > len(parsed.Text) {
		offset = len(parsed.Text)
	}
	scope, ok := vbProcedureScopeContainingOffset(parsed, offset)
	if !ok || scope.StartOffset >= offset {
		return nil, false
	}
	if scope.Owner != "" {
		return nil, false
	}
	prefix := parsed.Text[scope.StartOffset:offset]
	prefixTokens := vbscript.Tokenize(prefix)
	if !vbscriptDirectProcedureBindsName(parsed, scope, owner, offset, prefixTokens) {
		return nil, false
	}
	for _, annotation := range []string{"@type", "@param", "@member", "@returns"} {
		if core.ContainsASCIIFold(parsed.Text[:offset], annotation) {
			return nil, false
		}
	}
	for _, token := range prefixTokens {
		if token.Kind != "keyword" {
			continue
		}
		switch strings.ToLower(token.Text) {
		case "if", "elseif", "else", "for", "do", "loop", "while", "wend", "select", "case", "with", "as":
			return nil, false
		}
	}

	document := vbTextDocument(parsed)
	typeName := ""
	foundAssignment := false
	for lineStart := scope.StartOffset; lineStart < offset; {
		if ctx.Err() != nil {
			return nil, false
		}
		lineEnd := vbPhysicalLineEnd(parsed.Text, lineStart, offset)
		for _, statement := range splitVBTypeAssignmentStatementSegments(parsed.Text[lineStart:lineEnd], lineStart) {
			if vbscriptTypeStatementStartsRemComment(statement.Text) {
				break
			}
			assignment, assigned := parseVBAssignment(statement.Text, statement.Start, document)
			if !assigned || !strings.EqualFold(assignment.Name, owner) {
				continue
			}
			directType, direct := vbscriptDirectNewType(assignment.Value)
			if !direct {
				return nil, false
			}
			if foundAssignment && !strings.EqualFold(typeName, directType) {
				return nil, false
			}
			foundAssignment = true
			typeName = directType
		}
		if lineEnd >= offset {
			break
		}
		lineStart = vbNextLineStart(parsed.Text, lineEnd, offset)
	}
	if !foundAssignment || typeName == "" {
		return nil, false
	}
	if len(vbscriptBuiltinTypeMembers(typeName)) != 0 {
		return nil, false
	}
	for configuredType := range s.settings.VBScriptComTypes {
		if strings.EqualFold(strings.TrimSpace(configuredType), typeName) {
			return nil, false
		}
	}
	members, foundClass := vbscriptDirectClassMembersBeforeOffset(ctx, parsed, typeName, offset)
	if !foundClass || ctx.Err() != nil {
		return nil, false
	}
	return s.vbscriptTypedMemberCompletionsFromTypesContext(ctx, []string{typeName}, map[string]map[string]vbscriptTypedMember{
		strings.ToLower(typeName): members,
	}), true
}

func vbscriptDirectProcedureBindsName(parsed *core.ParsedDocument, scope vbProcedureScope, owner string, offset int, prefixTokens []vbscript.Token) bool {
	if parsed == nil || owner == "" {
		return false
	}
	lineStart := scope.StartOffset
	for lineStart > 0 && parsed.Text[lineStart-1] != '\r' && parsed.Text[lineStart-1] != '\n' {
		lineStart--
	}
	header, logicalEnd, ok := vbscript.ProcedureHeaderAtLogical(parsed.Text, lineStart)
	if ok && logicalEnd <= offset && header.NameStart == scope.NameStart && header.HasParameterList && header.ParamsEnd >= header.ParamsStart {
		for _, segment := range splitVBSegments(parsed.Text[header.ParamsStart:header.ParamsEnd]) {
			part := parsed.Text[header.ParamsStart+segment.Start : header.ParamsStart+segment.End]
			for _, token := range vbscript.Tokenize(part) {
				if token.Kind != "identifier" {
					continue
				}
				lower := strings.ToLower(token.Text)
				if lower == "optional" || lower == "byref" || lower == "byval" || lower == "paramarray" {
					continue
				}
				if strings.EqualFold(token.Text, owner) {
					return true
				}
				break
			}
		}
	}
	for _, statement := range splitNavigationVBStatements(prefixTokens) {
		significant := make([]vbscript.Token, 0, len(statement))
		for _, token := range statement {
			if token.Kind == "whitespace" || token.Kind == "comment" || token.Kind == "newline" || token.Text == "_" {
				continue
			}
			significant = append(significant, token)
		}
		if len(significant) < 2 || !strings.EqualFold(significant[0].Text, "dim") {
			continue
		}
		expectName := true
		depth := 0
		for _, token := range significant[1:] {
			switch token.Text {
			case "(":
				depth++
			case ")":
				if depth > 0 {
					depth--
				}
			case ",":
				if depth == 0 {
					expectName = true
				}
			default:
				if expectName && depth == 0 && token.Kind == "identifier" {
					if strings.EqualFold(token.Text, owner) {
						return true
					}
					expectName = false
				}
			}
		}
	}
	return false
}

func vbProcedureScopeContainingOffset(parsed *core.ParsedDocument, offset int) (vbProcedureScope, bool) {
	var best *vbProcedureScope
	scopes := vbProcedureScopes(parsed)
	for index := range scopes {
		scope := &scopes[index]
		if offset < scope.StartOffset || offset > scope.EndOffset {
			continue
		}
		if best == nil || scope.EndOffset-scope.StartOffset < best.EndOffset-best.StartOffset {
			copy := *scope
			best = &copy
		}
	}
	if best == nil {
		return vbProcedureScope{}, false
	}
	return *best, true
}

func vbscriptDirectNewType(value string) (string, bool) {
	value = strings.TrimSpace(value)
	keywordEnd := readVBIdentifier(value, 0)
	if keywordEnd == 0 || !strings.EqualFold(value[:keywordEnd], "New") {
		return "", false
	}
	cursor := keywordEnd
	for cursor < len(value) && isVBWhitespace(value[cursor]) {
		cursor++
	}
	nameEnd := readVBIdentifier(value, cursor)
	if nameEnd == cursor || strings.TrimSpace(value[nameEnd:]) != "" {
		return "", false
	}
	return value[cursor:nameEnd], true
}

func vbscriptDirectClassMembersBeforeOffset(ctx context.Context, parsed *core.ParsedDocument, typeName string, offset int) (map[string]vbscriptTypedMember, bool) {
	if parsed == nil || typeName == "" {
		return nil, false
	}
	document := vbTextDocument(parsed)
	members := map[string]vbscriptTypedMember{}
	found := false
	for _, region := range parsed.Regions {
		if region.Language != core.LanguageVBScript || region.ContentStart >= offset {
			continue
		}
		regionEnd := min(region.ContentEnd, offset)
		inTargetClass := false
		for lineStart := region.ContentStart; lineStart < regionEnd; {
			if ctx.Err() != nil {
				return nil, false
			}
			lineEnd := vbPhysicalLineEnd(parsed.Text, lineStart, regionEnd)
			line := parsed.Text[lineStart:lineEnd]
			trimmed := strings.TrimSpace(line)
			lower := strings.ToLower(trimmed)
			if strings.HasPrefix(lower, "class ") {
				name := strings.TrimSpace(trimmed[len("class "):])
				nameEnd := readVBIdentifier(name, 0)
				inTargetClass = nameEnd == len(name) && strings.EqualFold(name, typeName)
				if inTargetClass && found {
					return nil, false
				}
			} else if inTargetClass && lower == "end class" {
				found = true
				inTargetClass = false
			} else if inTargetClass {
				if declaration, ok := vbClassMemberDeclaration(document, line, lineStart, typeName); ok {
					kind := "property"
					if declaration.Kind == "method" {
						kind = "method"
					}
					members[strings.ToLower(declaration.Name)] = vbscriptTypedMember{Name: declaration.Name, Kind: kind}
				}
			}
			if lineEnd >= regionEnd {
				break
			}
			lineStart = vbNextLineStart(parsed.Text, lineEnd, regionEnd)
		}
		if inTargetClass {
			return nil, false
		}
	}
	return members, found
}

func (s *Server) vbscriptTypedMemberCompletionsFromTypesContext(ctx context.Context, typeNames []string, membersByType map[string]map[string]vbscriptTypedMember) []lsp.CompletionItem {
	if ctx == nil {
		ctx = context.Background()
	}
	if ctx.Err() != nil {
		return nil
	}
	var common map[string]vbscriptTypedMember
	for index, typeName := range typeNames {
		if ctx.Err() != nil {
			return nil
		}
		members := membersByType[strings.ToLower(typeName)]
		if len(members) == 0 {
			return nil
		}
		if index == 0 {
			common = map[string]vbscriptTypedMember{}
			for key, member := range members {
				common[key] = member
			}
			continue
		}
		for key := range common {
			member, ok := vbscriptTypedMemberMapGet(members, key)
			if !ok {
				delete(common, key)
				continue
			}
			merged, compatible := vbscriptIntersectTypedMembers(common[key], member)
			if !compatible {
				delete(common, key)
				continue
			}
			common[key] = merged
		}
	}
	items := make([]lsp.CompletionItem, 0, len(common))
	for _, member := range common {
		if ctx.Err() != nil {
			return nil
		}
		kind := lsp.CompletionItemKindProperty
		if member.Kind == "method" {
			kind = lsp.CompletionItemKindMethod
		}
		items = append(items, lsp.CompletionItem{Label: member.Name, Kind: kind, Detail: strings.Join(typeNames, " | ")})
	}
	return items
}

func vbCompletionMemberTargetAt(parsed *core.ParsedDocument, offset int) (vbCompletionMemberTarget, bool) {
	if parsed == nil {
		return vbCompletionMemberTarget{}, false
	}
	if owner, ok := vbCompletionMemberOwnerBefore(parsed.Text, offset); ok {
		return vbCompletionMemberTarget{owner: owner, explicit: true}, true
	}
	if !vbCompletionHasLeadingDot(parsed.Text, offset) {
		return vbCompletionMemberTarget{}, false
	}
	return vbCompletionMemberTarget{owner: activeVBWithOwnerBefore(parsed, offset)}, true
}

func vbCompletionHasLeadingDot(text string, offset int) bool {
	if offset < 0 {
		offset = 0
	}
	if offset > len(text) {
		offset = len(text)
	}
	cursor := offset
	for cursor > 0 && isVBIdentifierByte(text[cursor-1]) {
		cursor--
	}
	for cursor > 0 && isVBWhitespace(text[cursor-1]) {
		cursor--
	}
	if cursor == 0 || text[cursor-1] != '.' {
		return false
	}
	for cursor--; cursor > 0 && isVBWhitespace(text[cursor-1]); cursor-- {
	}
	return cursor == 0 || text[cursor-1] == '\n' || text[cursor-1] == '\r' || text[cursor-1] == ':'
}

func activeVBWithOwnerBefore(parsed *core.ParsedDocument, offset int) string {
	if parsed == nil {
		return ""
	}
	if offset < 0 {
		offset = 0
	}
	if offset > len(parsed.Text) {
		offset = len(parsed.Text)
	}
	procedureScopes := vbProcedureScopes(parsed)
	procedureScopeIndex := newVBProcedureScopeIndex(procedureScopes)
	activeScope := procedureScopeIndex.at(offset)
	stack := []string{}
	for _, region := range parsed.Regions {
		if region.Language != core.LanguageVBScript || region.ContentStart >= offset {
			continue
		}
		regionEnd := region.ContentEnd
		if regionEnd > offset {
			regionEnd = offset
		}
		for lineStart := region.ContentStart; lineStart < regionEnd; {
			lineEnd := lineStart
			for lineEnd < regionEnd && parsed.Text[lineEnd] != '\n' && parsed.Text[lineEnd] != '\r' {
				lineEnd++
			}
			for _, statement := range splitVBStatementSegments(parsed.Text[lineStart:lineEnd], lineStart) {
				if procedureScopeIndex.at(statement.Start) != activeScope {
					continue
				}
				trimmed := strings.TrimSpace(statement.Text)
				lower := strings.ToLower(trimmed)
				if lower == "end with" {
					if len(stack) > 0 {
						stack = stack[:len(stack)-1]
					}
					continue
				}
				keywordEnd := readVBIdentifier(trimmed, 0)
				if keywordEnd == 0 || !strings.EqualFold(trimmed[:keywordEnd], "with") {
					continue
				}
				expression := strings.TrimSpace(trimmed[keywordEnd:])
				if readVBIdentifier(expression, 0) != len(expression) {
					expression = ""
				}
				stack = append(stack, expression)
			}
			if lineEnd >= regionEnd {
				break
			}
			lineStart = lineEnd + 1
			if parsed.Text[lineEnd] == '\r' && lineStart < regionEnd && parsed.Text[lineStart] == '\n' {
				lineStart++
			}
		}
	}
	if len(stack) == 0 {
		return ""
	}
	return stack[len(stack)-1]
}

func vbCompletionMemberOwnerBefore(text string, offset int) (string, bool) {
	if offset < 0 {
		offset = 0
	}
	if offset > len(text) {
		offset = len(text)
	}
	cursor := offset
	for cursor > 0 && isVBIdentifierByte(text[cursor-1]) {
		cursor--
	}
	for cursor > 0 && isVBWhitespace(text[cursor-1]) {
		cursor--
	}
	if cursor == 0 || text[cursor-1] != '.' {
		return "", false
	}
	cursor--
	for cursor > 0 && isVBWhitespace(text[cursor-1]) {
		cursor--
	}
	end := cursor
	for cursor > 0 && isVBIdentifierByte(text[cursor-1]) {
		cursor--
	}
	if cursor == end {
		return "", false
	}
	return text[cursor:end], true
}

func vbClassMemberCompletionsByClass(parsed *core.ParsedDocument) map[string]map[string]vbClassMemberCompletion {
	const analysisKey = "lspserver.vb-class-member-completions.v2"
	if value, ok := parsed.LoadRuntimeAnalysis(analysisKey); ok {
		if cached, ok := value.(map[string]map[string]vbClassMemberCompletion); ok {
			return cached
		}
	}
	result := map[string]map[string]vbClassMemberCompletion{}
	doc := core.NewTextDocument(parsed.URI, "classic-asp", 0, parsed.Text)
	analysis := graphAnalysisTypes(parsed)
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
			if strings.HasPrefix(lowerTrimmed, "class ") {
				nameStart := strings.Index(strings.ToLower(line), "class") + len("class")
				for nameStart < len(line) && isVBWhitespace(line[nameStart]) {
					nameStart++
				}
				nameEnd := readVBIdentifier(line, nameStart)
				if nameEnd > nameStart {
					currentClass = line[nameStart:nameEnd]
					key := strings.ToLower(currentClass)
					if result[key] == nil {
						result[key] = map[string]vbClassMemberCompletion{}
					}
				}
			} else if lowerTrimmed == "end class" {
				currentClass = ""
			} else if currentClass != "" {
				if declaration, ok := vbClassMemberDeclaration(doc, line, lineStart, currentClass); ok {
					kind := lsp.CompletionItemKindProperty
					if declaration.Kind == "method" {
						kind = lsp.CompletionItemKindMethod
					}
					classKey := strings.ToLower(currentClass)
					member := vbClassMemberCompletion{
						Name:     declaration.Name,
						Kind:     kind,
						TypeName: analysis.Members[classKey][strings.ToLower(declaration.Name)],
					}
					if member.TypeName == "" {
						member.TypeName = graphTypeAnnotationForDeclaration(declaration, &analysis)
					}
					if declaration.Kind == "method" {
						member.TypeName = graphReturnTypeForDeclaration(declaration, &analysis)
						if signature, ok := analysis.ScopedSignatures[graphSignatureKey(classKey, declaration.Name, graphDeclarationAccessor(parsed, declaration))]; ok {
							minimum, maximum := vbscriptParameterRange(signature.Parameters)
							member.ParameterCount = maximum
							member.MinimumParameterCount = minimum
							member.MaximumParameterCount = maximum
							member.ParameterRangeKnown = true
						}
					}
					result[classKey][strings.ToLower(declaration.Name)] = member
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
	parsed.StoreRuntimeAnalysis(analysisKey, result)
	parsed.StoreAnalysis(analysisKey, result)
	return result
}

func isVBIdentifierByte(value byte) bool {
	return value == '_' || value >= 'A' && value <= 'Z' || value >= 'a' && value <= 'z' || value >= '0' && value <= '9'
}
