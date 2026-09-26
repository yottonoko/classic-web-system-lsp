package lspserver

import (
	"context"
	"sort"
	"strconv"
	"strings"

	"github.com/yottonoko/classic-web-system-lsp/internal/core"
	"github.com/yottonoko/classic-web-system-lsp/internal/lsp"
	workspacepkg "github.com/yottonoko/classic-web-system-lsp/internal/workspace"
)

func (s *Server) vbscriptConfiguredMemberCompletions(parsed *core.ParsedDocument, offset int) []lsp.CompletionItem {
	return s.vbscriptConfiguredMemberCompletionsContext(context.Background(), parsed, offset)
}

func (s *Server) vbscriptConfiguredMemberCompletionsContext(ctx context.Context, parsed *core.ParsedDocument, offset int) []lsp.CompletionItem {
	if ctx == nil {
		return nil
	}
	if ctx.Err() != nil || parsed == nil {
		return nil
	}
	if len(s.settings.VBScriptComTypes) == 0 {
		return nil
	}
	owner, ok := vbCompletionMemberOwnerBefore(parsed.Text, offset)
	if !ok {
		return nil
	}
	info, complete := s.vbscriptTypeInfoAtOffsetContext(ctx, parsed, offset)
	if !complete || ctx.Err() != nil {
		return nil
	}
	scope := vbscriptScopeAtOffset(parsed, offset)
	typeName := info.scopedVariableTypeNames[vbscriptTypeScopeKey(parsed, scope, owner)]
	if typeName == "" && (scope == "" || !vbscriptNameBoundInScopeAtOffset(parsed, owner, scope, offset)) {
		typeName = info.variableTypes[strings.ToLower(owner)]
	}
	if typeName == "" {
		return nil
	}
	typeNames := vbscriptConcreteTypeNames(typeName)
	if len(typeNames) == 0 {
		return nil
	}
	for _, candidate := range typeNames {
		if ctx.Err() != nil {
			return nil
		}
		if !s.isConfiguredVBScriptComType(candidate) {
			return nil
		}
	}
	members, ok := vbscriptCommonTypedMembersContext(ctx, typeNames, info.members)
	if !ok || len(members) == 0 {
		return nil
	}
	items := make([]lsp.CompletionItem, 0, len(members))
	for _, member := range members {
		if ctx.Err() != nil {
			return nil
		}
		kind := lsp.CompletionItemKindProperty
		if member.Kind == "method" {
			kind = lsp.CompletionItemKindMethod
		}
		items = append(items, lsp.CompletionItem{Label: member.Name, Kind: kind, Detail: typeName})
	}
	sort.SliceStable(items, func(i, j int) bool {
		return strings.ToLower(items[i].Label) < strings.ToLower(items[j].Label)
	})
	if ctx.Err() != nil {
		return nil
	}
	return items
}

func vbscriptCommonTypedMembers(typeNames []string, membersByType map[string]map[string]vbscriptTypedMember) (map[string]vbscriptTypedMember, bool) {
	return vbscriptCommonTypedMembersContext(context.Background(), typeNames, membersByType)
}

func vbscriptCommonTypedMembersContext(ctx context.Context, typeNames []string, membersByType map[string]map[string]vbscriptTypedMember) (map[string]vbscriptTypedMember, bool) {
	if ctx == nil || ctx.Err() != nil {
		return nil, false
	}
	if len(typeNames) == 0 {
		return nil, false
	}
	var common map[string]vbscriptTypedMember
	for _, typeName := range typeNames {
		if ctx.Err() != nil {
			return nil, false
		}
		members := membersByType[strings.ToLower(typeName)]
		if len(members) == 0 {
			return nil, false
		}
		if common == nil {
			common = make(map[string]vbscriptTypedMember, len(members))
			for name, member := range members {
				common[name] = member
			}
			continue
		}
		for name := range common {
			if ctx.Err() != nil {
				return nil, false
			}
			member, ok := vbscriptTypedMemberMapGet(members, name)
			if !ok {
				delete(common, name)
				continue
			}
			merged, compatible := vbscriptIntersectTypedMembers(common[name], member)
			if !compatible {
				delete(common, name)
				continue
			}
			common[name] = merged
		}
	}
	return common, true
}

func vbscriptTypedMemberMapGet(members map[string]vbscriptTypedMember, name string) (vbscriptTypedMember, bool) {
	if member, ok := members[strings.ToLower(name)]; ok {
		return member, true
	}
	for candidate := range members {
		if strings.EqualFold(candidate, name) {
			return members[candidate], true
		}
	}
	return vbscriptTypedMember{}, false
}

func (s *Server) isConfiguredVBScriptComType(typeName string) bool {
	for configuredTypeName := range s.settings.VBScriptComTypes {
		if strings.EqualFold(configuredTypeName, typeName) {
			return true
		}
	}
	return false
}

//lint:ignore U1000 retained for callers that use the legacy context-free helper.
func (s *Server) vbscriptConfiguredTypeHierarchyItem(doc *core.TextDocument, parsed *core.ParsedDocument, position lsp.Position) (lsp.TypeHierarchyItem, bool) {
	return s.vbscriptConfiguredTypeHierarchyItemContext(context.Background(), doc, parsed, position)
}

func (s *Server) vbscriptConfiguredTypeHierarchyItemContext(ctx context.Context, doc *core.TextDocument, parsed *core.ParsedDocument, position lsp.Position) (lsp.TypeHierarchyItem, bool) {
	if ctx == nil || ctx.Err() != nil {
		return lsp.TypeHierarchyItem{}, false
	}
	if doc == nil || parsed == nil {
		return lsp.TypeHierarchyItem{}, false
	}
	offset := doc.OffsetAt(position)
	word := vbscriptWordAtOffset(parsed.Text, offset)
	if word == "" {
		return lsp.TypeHierarchyItem{}, false
	}
	info, complete := s.vbscriptTypeInfoAtOffsetContext(ctx, parsed, offset)
	if !complete || ctx.Err() != nil {
		return lsp.TypeHierarchyItem{}, false
	}
	scope := vbscriptScopeAtOffset(parsed, doc.OffsetAt(position))
	typeName := info.scopedVariableTypeNames[vbscriptTypeScopeKey(parsed, scope, word)]
	if typeName == "" && (scope == "" || !vbscriptNameBoundInScopeAtOffset(parsed, word, scope, offset)) {
		typeName = info.variableTypes[strings.ToLower(word)]
	}
	if typeName == "" {
		for _, declaration := range variableInlayDeclarations(parsed, true, nil) {
			if declaration.Start > offset || offset > declaration.End || !isVBVariableInlayDeclaration(declaration) || !strings.EqualFold(declaration.Name, word) {
				continue
			}
			if declaration.Local && !strings.EqualFold(declaration.Scope, scope) {
				continue
			}
			// Declaration-token requests must use the state reconstructed through
			// this position. Full-document inference would pull a later assignment
			// backward into an untyped declaration.
			typeName = s.vbscriptTypeNameForDeclarationAtOffset(info, parsed, declaration, scope, offset)
			break
		}
	}
	typeNames := vbscriptConcreteTypeNames(typeName)
	if len(typeNames) != 1 || !s.isConfiguredVBScriptComType(typeNames[0]) {
		return lsp.TypeHierarchyItem{}, false
	}
	typeName = typeNames[0]
	start := offset
	for start > 0 && isVBIdentifierByte(parsed.Text[start-1]) {
		start--
	}
	end := start + len(word)
	if ctx.Err() != nil {
		return lsp.TypeHierarchyItem{}, false
	}
	r := doc.Range(start, end)
	return lsp.TypeHierarchyItem{
		Name:           typeName,
		Kind:           5,
		URI:            parsed.URI,
		Range:          r,
		SelectionRange: r,
		Data:           map[string]any{"kind": "vbscript-configured-type", "typeName": typeName},
	}, true
}

//lint:ignore U1000 retained for callers that use the legacy context-free helper.
func (s *Server) vbscriptConfiguredTypeHierarchyRelations(item lsp.TypeHierarchyItem) []lsp.TypeHierarchyItem {
	return s.vbscriptConfiguredTypeHierarchyRelationsContext(context.Background(), item)
}

func (s *Server) vbscriptConfiguredTypeHierarchyRelationsContext(ctx context.Context, item lsp.TypeHierarchyItem) []lsp.TypeHierarchyItem {
	if ctx == nil || ctx.Err() != nil {
		return nil
	}
	typeName := strings.TrimSpace(item.Name)
	if typeName == "" {
		return nil
	}
	memberTypes := map[string]string{}
	for configuredTypeName, config := range s.settings.VBScriptComTypes {
		if ctx.Err() != nil {
			return nil
		}
		if !strings.EqualFold(configuredTypeName, typeName) {
			continue
		}
		for _, member := range config.Members {
			if ctx.Err() != nil {
				return nil
			}
			for _, memberType := range vbscriptConcreteTypeNames(member.declaredTypeName()) {
				if ctx.Err() != nil {
					return nil
				}
				if _, ok := s.settings.VBScriptComTypes[memberType]; ok {
					memberTypes[strings.ToLower(memberType)] = memberType
					continue
				}
				for configuredChild := range s.settings.VBScriptComTypes {
					if ctx.Err() != nil {
						return nil
					}
					if strings.EqualFold(configuredChild, memberType) {
						memberTypes[strings.ToLower(configuredChild)] = configuredChild
						break
					}
				}
			}
		}
	}
	if len(memberTypes) == 0 {
		return nil
	}
	names := make([]string, 0, len(memberTypes))
	for _, name := range memberTypes {
		names = append(names, name)
	}
	sort.Strings(names)
	items := make([]lsp.TypeHierarchyItem, 0, len(names))
	for _, name := range names {
		if ctx.Err() != nil {
			return nil
		}
		items = append(items, lsp.TypeHierarchyItem{
			Name:           name,
			Kind:           5,
			URI:            item.URI,
			Range:          item.Range,
			SelectionRange: item.SelectionRange,
			Data:           map[string]any{"kind": "vbscript-configured-type", "typeName": name},
		})
	}
	if ctx.Err() != nil {
		return nil
	}
	return items
}

func (s *Server) vbscriptTypeCodeActions(params codeActionParams) []lsp.CodeAction {
	var actions []lsp.CodeAction
	for _, diagnostic := range params.Context.Diagnostics {
		if diagnostic.Source != vbscriptTypeDiagnosticSource {
			continue
		}
		targetURI, ok := vbscriptDiagnosticTargetURI(params.TextDocument.URI, diagnostic)
		if !ok {
			continue
		}
		doc, _ := s.parsed(targetURI)
		if doc == nil || !diagnosticRangeWithinDocument(doc, diagnostic.Range) {
			continue
		}
		code := diagnosticCodeString(diagnostic)
		name := diagnosticDataString(diagnostic, "name")
		if name == "" {
			name = strings.TrimSpace(textInRange(doc, diagnostic.Range))
		}
		diag := diagnostic
		switch code {
		case "objectNeedsSet":
			actions = append(actions, lsp.CodeAction{
				Title:       "Use Set for object assignment to " + name,
				Kind:        "quickfix",
				Diagnostics: []lsp.Diagnostic{diag},
				Edit: &lsp.WorkspaceEdit{Changes: map[string][]lsp.TextEdit{
					targetURI: {{Range: lsp.Range{Start: diagnostic.Range.Start, End: diagnostic.Range.Start}, NewText: "Set "}},
				}},
			})
		case "setScalar":
			if removeRange, ok := setKeywordRangeOnLine(doc, diagnostic.Range.Start.Line); ok {
				actions = append(actions, lsp.CodeAction{
					Title:       "Remove Set from scalar assignment to " + name,
					Kind:        "quickfix",
					Diagnostics: []lsp.Diagnostic{diag},
					Edit: &lsp.WorkspaceEdit{Changes: map[string][]lsp.TextEdit{
						targetURI: {{Range: removeRange, NewText: ""}},
					}},
				})
			}
		case "typeMismatch":
			actualType := diagnosticDataString(diagnostic, "actualType")
			if actualType == "" {
				actualType = "Variant"
			}
			action := lsp.CodeAction{
				Title:       "Annotate " + name + " as " + actualType,
				Kind:        "quickfix",
				Diagnostics: []lsp.Diagnostic{diag},
			}
			if editRange, ok := typeAnnotationRange(doc, name); ok {
				action.Edit = &lsp.WorkspaceEdit{Changes: map[string][]lsp.TextEdit{
					targetURI: {{Range: editRange, NewText: actualType}},
				}}
			}
			actions = append(actions, action)
		}
	}
	return actions
}

func vbscriptWordAtOffset(text string, offset int) string {
	if offset < 0 {
		offset = 0
	}
	if offset > len(text) {
		offset = len(text)
	}
	start := offset
	for start > 0 && isVBIdentifierByte(text[start-1]) {
		start--
	}
	end := offset
	for end < len(text) && isVBIdentifierByte(text[end]) {
		end++
	}
	if start == end {
		return ""
	}
	return text[start:end]
}

func diagnosticCodeString(diagnostic lsp.Diagnostic) string {
	switch value := diagnostic.Code.(type) {
	case string:
		return value
	case map[string]any:
		if text, ok := value["value"].(string); ok {
			return text
		}
	}
	return ""
}

func diagnosticDataString(diagnostic lsp.Diagnostic, key string) string {
	data, ok := diagnostic.Data.(map[string]any)
	if !ok {
		return ""
	}
	value, ok := data[key].(string)
	if !ok {
		return ""
	}
	return value
}

func vbscriptDiagnosticTargetURI(requestURI string, diagnostic lsp.Diagnostic) (string, bool) {
	targetURI := diagnosticDataString(diagnostic, "uri")
	if targetURI == "" {
		return requestURI, requestURI != ""
	}
	if requestURI == "" || !workspacepkg.SameFileIdentityURI(requestURI, targetURI) {
		return "", false
	}
	return targetURI, true
}

func diagnosticRangeWithinDocument(doc *core.TextDocument, r lsp.Range) bool {
	if doc == nil {
		return false
	}
	start := doc.OffsetAt(r.Start)
	end := doc.OffsetAt(r.End)
	return start <= end && doc.PositionAt(start) == r.Start && doc.PositionAt(end) == r.End
}

func setKeywordRangeOnLine(doc *core.TextDocument, line int) (lsp.Range, bool) {
	if line < 0 {
		return lsp.Range{}, false
	}
	lineStart := offsetAtLine(doc.Text, line)
	if lineStart < 0 {
		return lsp.Range{}, false
	}
	cursor := lineStart
	for cursor < len(doc.Text) && (doc.Text[cursor] == ' ' || doc.Text[cursor] == '\t') {
		cursor++
	}
	end := cursor + len("Set")
	if end > len(doc.Text) || !strings.EqualFold(doc.Text[cursor:end], "Set") {
		return lsp.Range{}, false
	}
	for end < len(doc.Text) && (doc.Text[end] == ' ' || doc.Text[end] == '\t') {
		end++
		break
	}
	return doc.Range(cursor, end), true
}

func typeAnnotationRange(doc *core.TextDocument, name string) (lsp.Range, bool) {
	lowerName := strings.ToLower(name)
	offset := 0
	for _, rawLine := range strings.SplitAfter(doc.Text, "\n") {
		line := strings.TrimRight(rawLine, "\r\n")
		trimmed := strings.TrimSpace(line)
		if strings.HasPrefix(trimmed, "'") {
			annotation := strings.TrimSpace(strings.TrimPrefix(trimmed, "'"))
			if match := graphTypeAnnotationPattern.FindStringSubmatchIndex(annotation); len(match) == 6 && strings.EqualFold(annotation[match[2]:match[3]], lowerName) {
				asIndex := strings.LastIndex(strings.ToLower(line), " as ")
				if asIndex >= 0 {
					start := offset + asIndex + len(" as ")
					end := offset + len(line)
					return doc.Range(start, end), true
				}
			}
		}
		offset += len(rawLine)
	}
	return lsp.Range{}, false
}

func offsetAtLine(text string, line int) int {
	if line == 0 {
		return 0
	}
	currentLine := 0
	for index, ch := range text {
		if ch == '\n' {
			currentLine++
			if currentLine == line {
				return index + 1
			}
		}
	}
	return -1
}

func intString(value int) string {
	return strconv.Itoa(value)
}
