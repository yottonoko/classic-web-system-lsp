package lspserver

import (
	"context"
	"path/filepath"
	"strconv"
	"strings"
	"time"

	"github.com/yottonoko/classic-web-system-lsp/internal/core"
	"github.com/yottonoko/classic-web-system-lsp/internal/embedded"
	"github.com/yottonoko/classic-web-system-lsp/internal/lsp"
	"github.com/yottonoko/classic-web-system-lsp/internal/vbscript"
	workspacepkg "github.com/yottonoko/classic-web-system-lsp/internal/workspace"
)

func (s *Server) definition(uri string, position lsp.Position) []lsp.Location {
	return s.definitionContext(context.Background(), uri, position)
}

func (s *Server) definitionContext(ctx context.Context, uri string, position lsp.Position) []lsp.Location {
	if ctx == nil {
		ctx = context.Background()
	}
	if ctx.Err() != nil {
		return nil
	}
	doc, parsed := s.parsed(uri)
	if doc == nil {
		return nil
	}
	if ctx.Err() != nil {
		return nil
	}
	if locations := s.serverObjectDefinitionContext(ctx, parsed, position); len(locations) > 0 {
		if s.serverObjectDefinitionVisibleAtOffset(ctx, parsed, position, locations) {
			if ctx.Err() != nil {
				return nil
			}
			return locations
		}
		if ctx.Err() != nil {
			return nil
		}
	}
	if locations := s.legacyUndefinedGlobalDefinition(ctx, parsed, position); len(locations) > 0 {
		if ctx.Err() != nil {
			return nil
		}
		return locations
	}
	if region := core.RegionAt(parsed, doc.OffsetAt(position)); region != nil && region.Language == core.LanguageCSS {
		if ctx.Err() != nil {
			return nil
		}
		s.cssMu.Lock()
		defer s.cssMu.Unlock()
		if location := s.css.Definition(parsed, position); location != nil {
			if ctx.Err() != nil {
				return nil
			}
			return []lsp.Location{*location}
		}
		return nil
	}
	if isJavaScriptPosition(doc, parsed, position) {
		var locations []lsp.Location
		if s.javaScriptLanguageServiceRequest(ctx, uri, position, "textDocument/definition", nil, &locations) && ctx.Err() == nil {
			return locations
		}
		return nil
	}
	if ctx.Err() != nil {
		return nil
	}
	if locations, handled := s.vbscriptMemberDefinitionContext(ctx, parsed, position); handled {
		if ctx.Err() != nil {
			return nil
		}
		return locations
	}
	if ctx.Err() != nil {
		return nil
	}
	if locations := scopedVBScriptDefinition(parsed, position); len(locations) > 0 {
		if ctx.Err() != nil {
			return nil
		}
		return locations
	}
	if ctx.Err() != nil {
		return nil
	}
	if locations := positionSensitiveVBScriptDefinition(parsed, position); len(locations) > 0 {
		if ctx.Err() != nil {
			return nil
		}
		return locations
	}
	return s.includedVBScriptDefinitionContext(ctx, parsed, position)
}

func (s *Server) serverObjectDefinitionVisibleAtOffset(ctx context.Context, parsed *core.ParsedDocument, position lsp.Position, locations []lsp.Location) bool {
	if parsed == nil || len(locations) == 0 {
		return false
	}
	if ctx == nil {
		ctx = context.Background()
	}
	if ctx.Err() != nil {
		return false
	}
	location := locations[0]
	if workspacepkg.SameFileIdentityURI(location.URI, parsed.URI) {
		offset := core.SourceDocument(parsed).OffsetAt(position)
		declarationOffset := core.SourceDocument(parsed).OffsetAt(location.Range.Start)
		return declarationOffset <= offset
	}
	offset := core.SourceDocument(parsed).OffsetAt(position)
	documents, complete := s.vbscriptDocumentsThroughExecutionOffsetContext(ctx, parsed, offset)
	if !complete || ctx.Err() != nil {
		return false
	}
	for _, document := range documents {
		if document != nil && workspacepkg.SameFileIdentityURI(document.URI, location.URI) {
			return true
		}
	}
	return false
}

func positionSensitiveVBScriptDefinition(parsed *core.ParsedDocument, position lsp.Position) []lsp.Location {
	if parsed == nil {
		return nil
	}
	doc := core.SourceDocument(parsed)
	offset := doc.OffsetAt(position)
	word := vbscript.WordAt(parsed.Text, offset)
	if word == "" {
		return nil
	}
	start, end := vbscriptIdentifierBoundsAt(parsed.Text, offset)
	if start < 0 || end <= start {
		return nil
	}
	if ownerStart, _ := vbMemberOwnerBeforeOffset(parsed.Text, start); ownerStart >= 0 {
		return nil
	}
	scope := vbscriptScopeAtOffset(parsed, offset)
	declarations := normalizedVBUsageDeclarations(parsed)
	var global *vbUsageDeclaration
	var forward *vbUsageDeclaration
	for index := range declarations {
		declaration := &declarations[index]
		if !strings.EqualFold(declaration.Name, word) || declaration.MemberOf != "" {
			continue
		}
		if declaration.Local {
			if scope != "" && strings.EqualFold(declaration.Scope, scope) && declaration.Start <= offset {
				location := lsp.Location{URI: parsed.URI, Range: declaration.Range}
				return []lsp.Location{location}
			}
			continue
		}
		if declaration.Start <= offset {
			if global == nil {
				global = declaration
			}
			continue
		}
		if forward == nil && vbscriptForwardCallableDeclaration(*declaration) {
			forward = declaration
		}
	}
	if global != nil {
		return []lsp.Location{{URI: parsed.URI, Range: global.Range}}
	}
	if forward != nil && vbForwardCallAt(parsed.Text, start, end) {
		return []lsp.Location{{URI: parsed.URI, Range: forward.Range}}
	}
	return nil
}

func vbscriptIdentifierBoundsAt(text string, offset int) (int, int) {
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
		return -1, -1
	}
	return start, end
}

func vbForwardCallAt(text string, start, end int) bool {
	return isCallTarget(text, start) || isFunctionLikeUsage(text, end) || graphLooksLikeVBCall(text, start, end)
}

func scopedVBScriptDefinition(parsed *core.ParsedDocument, position lsp.Position) []lsp.Location {
	doc := core.SourceDocument(parsed)
	offset := doc.OffsetAt(position)
	word := vbscript.WordAt(parsed.Text, offset)
	if word == "" {
		return nil
	}
	scope := ""
	for _, procedure := range graphVBProcedureRanges(parsed) {
		if position.Line >= procedure.startLine && position.Line <= procedure.endLine {
			scope = procedure.key()
			break
		}
	}
	if scope == "" {
		return nil
	}
	for _, declaration := range collectVBUsageDeclarations(parsed).Declarations {
		if !declaration.Local || declaration.Start > offset || !strings.EqualFold(declaration.Name, word) || !strings.EqualFold(declaration.Scope, scope) {
			continue
		}
		return []lsp.Location{{URI: parsed.URI, Range: declaration.Range}}
	}
	return nil
}

//lint:ignore U1000 retained for callers that use the legacy context-free helper.
func (s *Server) typeDefinition(uri string, position lsp.Position) []lsp.Location {
	return s.typeDefinitionContext(context.Background(), uri, position)
}

func (s *Server) typeDefinitionContext(ctx context.Context, uri string, position lsp.Position) []lsp.Location {
	if ctx == nil {
		ctx = context.Background()
	}
	if ctx.Err() != nil {
		return nil
	}
	doc, parsed := s.parsed(uri)
	if doc == nil {
		return nil
	}
	if ctx.Err() != nil {
		return nil
	}
	if region := core.RegionAt(parsed, doc.OffsetAt(position)); region != nil && region.Language == core.LanguageCSS {
		if ctx.Err() != nil {
			return nil
		}
		s.cssMu.Lock()
		defer s.cssMu.Unlock()
		if location := s.css.Definition(parsed, position); location != nil {
			if ctx.Err() != nil {
				return nil
			}
			return []lsp.Location{*location}
		}
		return nil
	}
	if isJavaScriptPosition(doc, parsed, position) {
		var locations []lsp.Location
		if !s.javaScriptLanguageServiceRequest(ctx, uri, position, "textDocument/typeDefinition", nil, &locations) || ctx.Err() != nil {
			return nil
		}
		if len(locations) == 0 {
			if !s.javaScriptLanguageServiceRequest(ctx, uri, position, "textDocument/definition", nil, &locations) || ctx.Err() != nil {
				return nil
			}
		}
		return locations
	}
	if len(parsed.Includes) > 0 {
		offset := doc.OffsetAt(position)
		if _, complete := s.vbscriptIncludeExecutionUnitsThroughOffsetContextResult(ctx, parsed, offset); ctx.Err() != nil || !complete {
			return nil
		}
	}
	if locations := s.vbscriptTypeDefinitionContext(ctx, parsed, position); len(locations) > 0 {
		if ctx.Err() != nil {
			return nil
		}
		return locations
	}
	return s.definitionContext(ctx, uri, position)
}

func (s *Server) implementationContext(ctx context.Context, uri string, position lsp.Position) []lsp.Location {
	if ctx == nil {
		ctx = context.Background()
	}
	if ctx.Err() != nil {
		return nil
	}
	doc, parsed := s.parsed(uri)
	if doc == nil {
		return nil
	}
	if region := core.RegionAt(parsed, doc.OffsetAt(position)); region != nil && region.Language == core.LanguageCSS {
		return nil
	}
	if isJavaScriptPosition(doc, parsed, position) {
		var locations []lsp.Location
		if !s.javaScriptLanguageServiceRequest(ctx, uri, position, "textDocument/implementation", nil, &locations) || ctx.Err() != nil {
			return nil
		}
		return locations
	}
	return s.definitionContext(ctx, uri, position)
}

//lint:ignore U1000 retained for callers that use the legacy context-free helper.
func (s *Server) implementation(uri string, position lsp.Position) []lsp.Location {
	return s.implementationContext(context.Background(), uri, position)
}

func (s *Server) vbscriptTypeDefinition(parsed *core.ParsedDocument, position lsp.Position) []lsp.Location {
	return s.vbscriptTypeDefinitionContext(context.Background(), parsed, position)
}

func (s *Server) vbscriptTypeDefinitionContext(ctx context.Context, parsed *core.ParsedDocument, position lsp.Position) []lsp.Location {
	if ctx == nil {
		ctx = context.Background()
	}
	if ctx.Err() != nil {
		return nil
	}
	if parsed == nil {
		return nil
	}
	doc := core.SourceDocument(parsed)
	offset := doc.OffsetAt(position)
	if region := core.RegionAt(parsed, offset); region != nil && region.Language != core.LanguageVBScript {
		return nil
	}
	word := vbscript.WordAt(parsed.Text, offset)
	if word == "" {
		return nil
	}
	documents, complete := s.vbscriptDocumentsThroughExecutionOffsetContext(ctx, parsed, offset)
	if ctx.Err() != nil || !complete {
		return nil
	}
	// Never resolve a type from an incomplete include prefix. The omitted
	// occurrence may redefine the variable or provide the only class body.
	documents = dedupeParsedDocumentsByFileIdentity(documents)
	typeNames, declared := s.vbscriptTypeDefinitionNamesContext(ctx, parsed, offset, word)
	if ctx.Err() != nil {
		return nil
	}
	if declared {
		return vbscriptClassLocationsContext(ctx, documents, typeNames, parsed, offset)
	}
	return vbscriptClassLocationsContext(ctx, documents, vbscriptConcreteTypeNames(word), parsed, offset)
}

// vbscriptTypeDefinitionNamesContext resolves the effective type at a
// reference. Include-backed globals must use the same complete, source-ordered
// state as variable hover and member completion; collecting the first textual
// declaration can instead return a superseded type.
func (s *Server) vbscriptTypeDefinitionNamesContext(ctx context.Context, parsed *core.ParsedDocument, offset int, word string) ([]string, bool) {
	if ctx == nil {
		ctx = context.Background()
	}
	if parsed == nil || word == "" || ctx.Err() != nil {
		return nil, false
	}
	declarations, declared := visibleVBScriptTypeDeclarations(parsed, offset, word)
	if declared {
		// Local variables and class members are lexical declarations in the
		// request document. Include globals must not shadow either category.
		lexical := false
		for _, declaration := range declarations {
			if declaration.Local || declaration.Scope != "" || declaration.MemberOf != "" {
				lexical = true
				break
			}
		}
		if !lexical && len(parsed.Includes) > 0 {
			if typeNames, ok := s.vbscriptIncludeTypeDefinitionNamesContext(ctx, parsed, offset, word); ok {
				return typeNames, true
			}
		}
		info, complete := s.vbscriptTypeInfoAtOffsetContext(ctx, parsed, offset)
		if !complete || ctx.Err() != nil {
			return nil, false
		}
		scope := vbscriptScopeAtOffset(parsed, offset)
		var typeNames []string
		for _, declaration := range declarations {
			if ctx.Err() != nil {
				return nil, false
			}
			typeName := s.vbscriptTypeNameForDeclarationAtOffset(info, parsed, declaration, scope, offset)
			typeNames = append(typeNames, vbscriptConcreteTypeNames(typeName)...)
		}
		return typeNames, true
	}

	if len(parsed.Includes) > 0 {
		if typeNames, ok := s.vbscriptIncludeTypeDefinitionNamesContext(ctx, parsed, offset, word); ok {
			return typeNames, true
		}
		// The include traversal was complete, but no visible global state was
		// established before this reference. Do not fall back to a later or
		// incomplete assignment.
		return nil, false
	}

	info, complete := s.vbscriptTypeInfoAtOffsetContext(ctx, parsed, offset)
	if !complete || ctx.Err() != nil {
		return nil, false
	}
	scope := vbscriptScopeAtOffset(parsed, offset)
	typeName := info.scopedVariableTypeNames[vbscriptTypeScopeKey(parsed, scope, word)]
	if typeName == "" && (scope == "" || !vbscriptNameBoundInScopeAtOffset(parsed, word, scope, offset)) {
		typeName = info.variableTypes[strings.ToLower(word)]
	}
	if typeName == "" {
		return nil, false
	}
	return vbscriptConcreteTypeNames(typeName), true
}

func (s *Server) vbscriptIncludeTypeDefinitionNamesContext(ctx context.Context, parsed *core.ParsedDocument, offset int, word string) ([]string, bool) {
	info, complete := s.vbscriptTypeInfoAtOffsetContext(ctx, parsed, offset)
	if !complete || ctx.Err() != nil {
		return nil, false
	}
	scope := vbscriptScopeAtOffset(parsed, offset)
	typeName := info.scopedVariableTypeNames[vbscriptTypeScopeKey(parsed, scope, word)]
	if typeName == "" && (scope == "" || !vbscriptNameBoundInScopeAtOffset(parsed, word, scope, offset)) {
		typeName = info.variableTypes[strings.ToLower(word)]
	}
	// The prefix state is complete even when no type is visible. Returning an
	// empty, handled result prevents a later declaration or assignment from
	// leaking backward through the legacy candidate path.
	return vbscriptConcreteTypeNames(typeName), true
}

func visibleVBScriptTypeDeclarations(parsed *core.ParsedDocument, offset int, name string) ([]vbUsageDeclaration, bool) {
	if parsed == nil || name == "" {
		return nil, false
	}
	scope := vbscriptScopeAtOffset(parsed, offset)
	className := vbscriptClassOwnerAtOffset(parsed, offset, scope)
	declarations := append([]vbUsageDeclaration(nil), variableInlayDeclarations(parsed, true, nil)...)
	declarations = append(declarations, collectVBNamingDeclarations(parsed)...)
	active := declarations[:0]
	for _, declaration := range declarations {
		if declaration.Start <= offset {
			active = append(active, declaration)
		}
	}
	return selectVisibleVBScriptTypeDeclarations(active, name, scope, className)
}

func selectVisibleVBScriptTypeDeclarations(declarations []vbUsageDeclaration, name, scope, className string) ([]vbUsageDeclaration, bool) {
	bestRank := 0
	result := make([]vbUsageDeclaration, 0, 2)
	seen := map[string]struct{}{}
	for _, declaration := range declarations {
		if declaration.Kind == "class" || !strings.EqualFold(declaration.Name, name) {
			continue
		}
		rank := 0
		switch {
		case declaration.Local:
			if scope == "" || !strings.EqualFold(declaration.Scope, scope) {
				continue
			}
			rank = 3
		case declaration.MemberOf != "":
			if className == "" || declaration.Scope != "" || !strings.EqualFold(declaration.MemberOf, className) {
				continue
			}
			rank = 2
		case declaration.Scope == "":
			rank = 1
		default:
			continue
		}
		if rank < bestRank {
			continue
		}
		key := strings.Join([]string{declaration.Kind, declaration.Scope, declaration.MemberOf, strconv.Itoa(declaration.Start), strconv.Itoa(declaration.End)}, "\x00")
		if rank > bestRank {
			bestRank = rank
			result = result[:0]
			seen = map[string]struct{}{}
		}
		if _, ok := seen[key]; ok {
			continue
		}
		seen[key] = struct{}{}
		result = append(result, declaration)
	}
	return result, len(result) > 0
}

func vbscriptClassOwnerAtOffset(parsed *core.ParsedDocument, offset int, scope string) string {
	if parsed == nil {
		return ""
	}
	if owner := vbscriptClassScopeForProcedure(parsed, scope); owner != "" {
		return owner
	}
	doc := core.SourceDocument(parsed)
	return strings.ToLower(strings.TrimSpace(vbClassMemberLineOwners(parsed)[doc.PositionAt(offset).Line]))
}

func (s *Server) vbscriptTypeNameForDeclarationAtOffset(info vbscriptTypeInfo, parsed *core.ParsedDocument, declaration vbUsageDeclaration, scope string, offset int) string {
	if parsed == nil || declaration.Start > offset {
		return ""
	}
	if typeName := strings.TrimSpace(declaration.TypeName); typeName != "" && !declaration.Implicit {
		return typeName
	}
	if typeName := strings.TrimSpace(precedingVBTypeAnnotation(parsed, declaration.Line, declaration.Name)); typeName != "" {
		return typeName
	}
	if typeName := strings.TrimSpace(info.scopedVariableTypeNames[vbscriptTypeScopeKey(parsed, declaration.Scope, declaration.Name)]); typeName != "" {
		return typeName
	}
	if declaration.Scope == "" && (scope == "" || !vbscriptNameBoundInScopeAtOffset(parsed, declaration.Name, scope, offset)) {
		if typeName := strings.TrimSpace(info.variableTypes[strings.ToLower(declaration.Name)]); typeName != "" {
			return typeName
		}
	}
	analysis := graphAnalysisTypes(parsed)
	if typeName := graphTypeAnnotationForDeclaration(declaration, &analysis); typeName != "" {
		return typeName
	}
	if declaration.MemberOf != "" {
		if members := analysis.Members[strings.ToLower(declaration.MemberOf)]; members != nil {
			if typeName := strings.TrimSpace(members[strings.ToLower(declaration.Name)]); typeName != "" {
				return typeName
			}
		}
	}
	if declaration.Kind == "parameter" {
		if signature, ok := vbscriptSignatureForUsageDeclaration(parsed, declaration); ok {
			if typeName := graphParameterTypeForDeclaration(parsed, declaration, signature); typeName != "" {
				return typeName
			}
		}
	}
	if declaration.Kind == "function" || declaration.Kind == "property" || declaration.Kind == "method" {
		if typeName := graphReturnTypeForDeclaration(declaration, &analysis); typeName != "" {
			return typeName
		}
	}
	return ""
}

func vbscriptClassLocationsContext(ctx context.Context, documents []*core.ParsedDocument, names []string, root *core.ParsedDocument, offset int) []lsp.Location {
	if ctx == nil {
		ctx = context.Background()
	}
	if ctx.Err() != nil {
		return nil
	}
	if len(documents) == 0 || len(names) == 0 {
		return nil
	}
	locations := make([]lsp.Location, 0, len(names))
	seenNames := map[string]struct{}{}
	seenLocations := map[string]struct{}{}
	for _, name := range names {
		if ctx.Err() != nil {
			return nil
		}
		keyName := strings.ToLower(strings.TrimSpace(name))
		if keyName == "" {
			continue
		}
		if _, ok := seenNames[keyName]; ok {
			continue
		}
		for _, document := range documents {
			if ctx.Err() != nil {
				return nil
			}
			if document == nil {
				continue
			}
			rootDocument := document == root || root != nil && workspacepkg.SameFileIdentityURI(document.URI, root.URI)
			source := core.SourceDocument(document)
			for _, symbol := range vbscript.DeclarationSymbols(document) {
				if ctx.Err() != nil {
					return nil
				}
				if symbol.Kind != "class" || !strings.EqualFold(symbol.Name, name) {
					continue
				}
				if rootDocument && source.OffsetAt(symbol.Range.Start) > offset {
					continue
				}
				location := lsp.Location{URI: document.URI, Range: symbol.Range}
				locationKey := strings.Join([]string{workspacepkg.FileIdentityKeyFromURI(location.URI), strconv.Itoa(location.Range.Start.Line), strconv.Itoa(location.Range.Start.Character), strconv.Itoa(location.Range.End.Line), strconv.Itoa(location.Range.End.Character)}, "\x00")
				if _, duplicate := seenLocations[locationKey]; duplicate {
					seenNames[keyName] = struct{}{}
					break
				}
				seenNames[keyName] = struct{}{}
				seenLocations[locationKey] = struct{}{}
				locations = append(locations, location)
				break
			}
			if _, found := seenNames[keyName]; found {
				break
			}
		}
	}
	return locations
}

func vbscriptConcreteTypeNames(typeName string) []string {
	parsed, err := parseVBScriptType(typeName)
	if err != nil {
		return nil
	}
	names := []string{}
	seen := map[string]struct{}{}
	var add func(v vbscriptType)
	add = func(v vbscriptType) {
		if v.kind == vbscriptTypeUnion {
			for _, part := range v.parts {
				add(part)
			}
			return
		}
		name := v.primitiveName()
		if name == "" || isLooseVBTypeName(name) {
			return
		}
		key := strings.ToLower(name)
		if _, ok := seen[key]; ok {
			return
		}
		seen[key] = struct{}{}
		names = append(names, name)
	}
	add(parsed)
	return names
}

func isLooseVBTypeName(name string) bool {
	switch strings.ToLower(strings.TrimSpace(name)) {
	case "", "variant", "unknown", "nothing":
		return true
	default:
		return false
	}
}

func (s *Server) references(uri string, position lsp.Position, includeDeclaration bool) []lsp.Location {
	return s.referencesContext(context.Background(), uri, position, includeDeclaration)
}

func (s *Server) referencesContext(ctx context.Context, uri string, position lsp.Position, includeDeclaration bool) []lsp.Location {
	doc, parsed := s.parsed(uri)
	if doc == nil {
		return nil
	}
	if locations, ok := s.serverObjectReferences(ctx, parsed, position, includeDeclaration); ok {
		return locations
	}
	if locations, ok := s.legacyUndefinedGlobalReferences(ctx, parsed, position, includeDeclaration); ok {
		return locations
	}
	if isJavaScriptPosition(doc, parsed, position) {
		var locations []lsp.Location
		s.javaScriptLanguageServiceRequest(ctx, uri, position, "textDocument/references", map[string]any{"context": map[string]any{"includeDeclaration": includeDeclaration}}, &locations)
		return locations
	}
	if locations, handled := s.vbscriptMemberReferences(ctx, parsed, position, includeDeclaration); handled {
		return locations
	}
	word := vbscript.WordAt(parsed.Text, doc.OffsetAt(position))
	progressContext, taskID := s.beginWorkspaceReferenceProgress(ctx, word, uri, doc.Version)
	state := "completed"
	// CodeLens applies category-specific counting rules, while the references
	// provider returns every lexical reference.
	symbolKind := "reference:" + vbReferenceSymbolKindAt(parsed, word)
	locations := s.workspaceVBScriptReferences(progressContext, parsed, position, includeDeclaration, symbolKind)
	if progressContext.Err() != nil {
		state = "cancelled"
	}
	s.finishProgressTask(taskID, "references", state)
	return locations
}

//lint:ignore U1000 retained for callers that use the legacy context-free helper.
func (s *Server) highlights(uri string, position lsp.Position) []lsp.DocumentHighlight {
	return s.highlightsContext(context.Background(), uri, position)
}

func (s *Server) highlightsContext(ctx context.Context, uri string, position lsp.Position) []lsp.DocumentHighlight {
	if ctx == nil || ctx.Err() != nil {
		return nil
	}
	doc, parsed := s.parsed(uri)
	if doc == nil || parsed == nil || ctx.Err() != nil {
		return nil
	}
	region := core.RegionAt(parsed, doc.OffsetAt(position))
	if region == nil {
		return nil
	}
	switch region.Language {
	case core.LanguageJavaScript, core.LanguageJScript:
		var highlights []lsp.DocumentHighlight
		if !s.javaScriptLanguageServiceRequest(ctx, uri, position, "textDocument/documentHighlight", nil, &highlights) || ctx.Err() != nil {
			return nil
		}
		return highlights
	case core.LanguageHTML:
		if ctx.Err() != nil {
			return nil
		}
		s.htmlMu.Lock()
		highlights := s.html.Highlights(parsed, position)
		s.htmlMu.Unlock()
		if ctx.Err() != nil {
			return nil
		}
		return highlights
	case core.LanguageCSS:
		if ctx.Err() != nil {
			return nil
		}
		s.cssMu.Lock()
		highlights := s.css.Highlights(parsed, position)
		s.cssMu.Unlock()
		if ctx.Err() != nil {
			return nil
		}
		return highlights
	case core.LanguageVBScript:
		if locations, handled := s.vbscriptMemberReferenceLocations(ctx, parsed, position, true, false); handled {
			if ctx.Err() != nil {
				return nil
			}
			highlights := make([]lsp.DocumentHighlight, 0, len(locations))
			for _, location := range locations {
				if ctx.Err() != nil {
					return nil
				}
				if workspacepkg.SameFileIdentityURI(location.URI, parsed.URI) {
					highlights = append(highlights, lsp.DocumentHighlight{Range: location.Range, Kind: 2})
				}
			}
			return highlights
		}
		highlights := vbscript.Highlights(parsed, position)
		if ctx.Err() != nil {
			return nil
		}
		return highlights
	default:
		return nil
	}
}

func (s *Server) signatureHelp(uri string, position lsp.Position) *lsp.SignatureHelp {
	return s.signatureHelpContext(context.Background(), uri, position)
}

func (s *Server) signatureHelpContext(ctx context.Context, uri string, position lsp.Position) *lsp.SignatureHelp {
	if ctx == nil || ctx.Err() != nil {
		return nil
	}
	doc, parsed := s.parsed(uri)
	if doc == nil || parsed == nil || ctx.Err() != nil {
		return nil
	}
	if isJavaScriptPosition(doc, parsed, position) {
		var help lsp.SignatureHelp
		if s.javaScriptLanguageServiceRequest(ctx, uri, position, "textDocument/signatureHelp", nil, &help) && ctx.Err() == nil {
			return &help
		}
		return nil
	}
	if _, suppressed := vbCallOpenParenBeforeResult(parsed.Text, doc.OffsetAt(position)); suppressed {
		return nil
	}
	if isStandaloneVBScriptDocument(parsed) {
		if ctx.Err() != nil {
			return nil
		}
		if help := s.vbscriptBuiltinMemberSignatureHelpContext(ctx, parsed, position); help != nil {
			if ctx.Err() != nil {
				return nil
			}
			return help
		}
		if help := s.vbscriptPropertyMemberSignatureHelpContext(ctx, parsed, position); help != nil {
			if ctx.Err() != nil {
				return nil
			}
			return help
		}
		if help, handled := s.vbscriptMemberSignatureHelpContext(ctx, parsed, position); handled {
			if ctx.Err() != nil {
				return nil
			}
			return help
		}
		if vbscriptParenthesizedCallShadowedAt(parsed, position) {
			return nil
		}
		if help := vbscriptRootSignatureHelp(parsed, position, s.settings.Locale); help != nil {
			return help
		}
		if help := s.enrichVBScriptBuiltInSignatureHelp(vbscript.StandaloneBuiltinSignatureHelp(parsed, position)); help != nil {
			if ctx.Err() != nil {
				return nil
			}
			return help
		}
		if ctx.Err() != nil {
			return nil
		}
		return enrichVBScriptSignatureHelp(parsed, vbscriptNoParenSignatureHelp(parsed, position, s.settings.Locale), s.settings.Locale)
	}
	if help := s.vbscriptBuiltinMemberSignatureHelpContext(ctx, parsed, position); help != nil {
		if ctx.Err() != nil {
			return nil
		}
		return help
	}
	if help := s.vbscriptPropertyMemberSignatureHelpContext(ctx, parsed, position); help != nil {
		if ctx.Err() != nil {
			return nil
		}
		return help
	}
	if help, handled := s.vbscriptMemberSignatureHelpContext(ctx, parsed, position); handled {
		if ctx.Err() != nil {
			return nil
		}
		return help
	}
	if vbscriptParenthesizedCallShadowedAt(parsed, position) {
		return nil
	}
	if help := vbscriptRootSignatureHelp(parsed, position, s.settings.Locale); help != nil {
		return help
	}
	if help := s.enrichVBScriptBuiltInSignatureHelp(vbscript.BuiltinSignatureHelp(parsed, position)); help != nil {
		if ctx.Err() != nil {
			return nil
		}
		return help
	}
	if ctx.Err() != nil {
		return nil
	}
	return enrichVBScriptSignatureHelp(parsed, vbscriptNoParenSignatureHelp(parsed, position, s.settings.Locale), s.settings.Locale)
}

func vbscriptParenthesizedCallShadowedAt(parsed *core.ParsedDocument, position lsp.Position) bool {
	if parsed == nil {
		return false
	}
	document := core.SourceDocument(parsed)
	offset := document.OffsetAt(position)
	open := vbCallOpenParenBefore(parsed.Text, offset)
	if open < 0 {
		return false
	}
	nameStart, nameEnd := vbIdentifierBefore(parsed.Text, open)
	if nameStart < 0 || vbscriptQualifiedMemberAtOffset(parsed.Text, nameStart) {
		return false
	}
	return vbLocalDeclarationShadowsNameAt(parsed, parsed.Text[nameStart:nameEnd], document.PositionAt(nameStart))
}

func vbscriptRootSignatureHelp(parsed *core.ParsedDocument, position lsp.Position, locale string) *lsp.SignatureHelp {
	if parsed == nil {
		return nil
	}
	document := core.SourceDocument(parsed)
	offset := document.OffsetAt(position)
	open := vbCallOpenParenBefore(parsed.Text, offset)
	if open < 0 {
		return nil
	}
	nameStart, nameEnd := vbIdentifierBefore(parsed.Text, open)
	if nameStart < 0 || vbscriptQualifiedMemberAtOffset(parsed.Text, nameStart) {
		return nil
	}
	signature, ok := vbscriptRootSignatureByName(parsed, parsed.Text[nameStart:nameEnd])
	if !ok {
		return nil
	}
	parameters := make([]lsp.ParameterInformation, 0, len(signature.Parameters))
	for _, parameter := range signature.Parameters {
		label := parameter.Name
		if parameter.Mode != "" {
			label = parameter.Mode + " " + label
		}
		parameters = append(parameters, lsp.ParameterInformation{Label: label})
	}
	information := lsp.SignatureInformation{Label: signature.Label, Parameters: parameters}
	enrichVBScriptSignatureInformation(&information, signature, locale, parsed)
	return &lsp.SignatureHelp{
		Signatures:      []lsp.SignatureInformation{information},
		ActiveSignature: 0,
		ActiveParameter: vbActiveParameter(parsed.Text, open+1, offset),
	}
}

//lint:ignore U1000 retained for callers that use the legacy context-free helper.
func (s *Server) vbscriptPropertyMemberSignatureHelp(parsed *core.ParsedDocument, position lsp.Position) *lsp.SignatureHelp {
	return s.vbscriptPropertyMemberSignatureHelpContext(context.Background(), parsed, position)
}

func (s *Server) vbscriptPropertyMemberSignatureHelpContext(ctx context.Context, parsed *core.ParsedDocument, position lsp.Position) *lsp.SignatureHelp {
	if ctx == nil || ctx.Err() != nil {
		return nil
	}
	if parsed == nil {
		return nil
	}
	doc := core.SourceDocument(parsed)
	offset := doc.OffsetAt(position)
	open := vbCallOpenParenBefore(parsed.Text, offset)
	nameStart := -1
	argsStart := -1
	if open >= 0 {
		nameStart, _ = vbIdentifierBefore(parsed.Text, open)
		argsStart = open + 1
	} else {
		nameStart, argsStart = vbscriptNoParenMemberCallAt(parsed.Text, offset)
	}
	if nameStart < 0 || argsStart < 0 {
		return nil
	}
	if target, ok := s.vbscriptMemberTargetAtContext(ctx, parsed, nameStart); ok {
		if ctx.Err() != nil {
			return nil
		}
		for _, match := range target.declarations {
			if ctx.Err() != nil {
				return nil
			}
			if match.declaration.Kind != "property" {
				continue
			}
			if help := vbscriptPropertySignatureHelpForDeclarationWithLocale(match.document, match.declaration, parsed.Text, argsStart, offset, s.settings.Locale); help != nil {
				return help
			}
		}
	}
	if declaration, ok := vbscriptUnqualifiedPropertyDeclarationAt(parsed, nameStart, position); ok {
		if ctx.Err() != nil {
			return nil
		}
		return vbscriptPropertySignatureHelpForDeclarationWithLocale(parsed, declaration, parsed.Text, argsStart, offset, s.settings.Locale)
	}
	return nil
}

func vbscriptPropertySignatureHelpForDeclarationWithLocale(parsed *core.ParsedDocument, declaration vbUsageDeclaration, callText string, argsStart, offset int, locale string) *lsp.SignatureHelp {
	signature, ok := vbPropertyAccessorSignatureAt(parsed, declaration.Range.Start)
	if !ok || !strings.EqualFold(signature.Name, declaration.Name) {
		return nil
	}
	parameters := make([]lsp.ParameterInformation, 0, len(signature.Parameters))
	for _, parameter := range signature.Parameters {
		label := parameter.Name
		if parameter.Mode != "" {
			label = parameter.Mode + " " + label
		}
		parameters = append(parameters, lsp.ParameterInformation{Label: label})
	}
	information := lsp.SignatureInformation{Label: signature.Label, Parameters: parameters}
	enrichVBScriptSignatureInformation(&information, signature, locale, parsed)
	return &lsp.SignatureHelp{
		Signatures:      []lsp.SignatureInformation{information},
		ActiveSignature: 0,
		ActiveParameter: vbActiveParameter(callText, argsStart, offset),
	}
}

func vbscriptUnqualifiedPropertyDeclarationAt(parsed *core.ParsedDocument, nameStart int, position lsp.Position) (vbUsageDeclaration, bool) {
	if parsed == nil || nameStart < 0 || nameStart >= len(parsed.Text) {
		return vbUsageDeclaration{}, false
	}
	nameEnd := nameStart
	for nameEnd < len(parsed.Text) && isVBIdentifierByte(parsed.Text[nameEnd]) {
		nameEnd++
	}
	if nameStart >= nameEnd {
		return vbUsageDeclaration{}, false
	}
	name := parsed.Text[nameStart:nameEnd]
	if declaration, ok := vbscriptClassMemberDeclarationAtOffset(parsed, name, nameStart); ok && declaration.Kind == "property" {
		return declaration, true
	}
	return vbUsageDeclaration{}, false
}

func (s *Server) renameRange(uri string, position lsp.Position) *lsp.Range {
	return s.renameRangeContext(context.Background(), uri, position)
}

func (s *Server) renameRangeContext(ctx context.Context, uri string, position lsp.Position) *lsp.Range {
	if ctx == nil || ctx.Err() != nil {
		return nil
	}
	doc, parsed := s.parsed(uri)
	if doc == nil || parsed == nil || ctx.Err() != nil {
		return nil
	}
	if r, ok := s.serverObjectRenameRangeContext(ctx, parsed, position); ok {
		if ctx.Err() != nil {
			return nil
		}
		return r
	}
	if r, ok := s.legacyUndefinedGlobalRenameRange(ctx, parsed, position); ok {
		if ctx.Err() != nil {
			return nil
		}
		return r
	}
	if region := core.RegionAt(parsed, doc.OffsetAt(position)); region != nil && region.Language == core.LanguageCSS {
		if ctx.Err() != nil {
			return nil
		}
		s.cssMu.Lock()
		r := s.css.PrepareRename(parsed, position)
		s.cssMu.Unlock()
		if ctx.Err() != nil {
			return nil
		}
		return r
	}
	if target, ok := embeddedRenameTarget(parsed, doc.OffsetAt(position)); ok {
		if ctx.Err() != nil {
			return nil
		}
		if target.Kind == embeddedRenameTag {
			s.htmlMu.Lock()
			r := s.html.PrepareRename(parsed, position)
			s.htmlMu.Unlock()
			if ctx.Err() != nil {
				return nil
			}
			return r
		}
		return &target.Range
	}
	if isJavaScriptPosition(doc, parsed, position) {
		var result lsp.Range
		if s.javaScriptLanguageServiceRequest(ctx, uri, position, "textDocument/prepareRename", nil, &result) && ctx.Err() == nil {
			return &result
		}
		return nil
	}
	result := vbscript.RenameRange(parsed, position)
	if ctx.Err() != nil {
		return nil
	}
	return result
}

func (s *Server) rename(uri string, position lsp.Position, newName string) map[string]any {
	return s.renameContext(context.Background(), uri, position, newName)
}

func (s *Server) renameContext(ctx context.Context, uri string, position lsp.Position, newName string) map[string]any {
	emptyEdit := map[string]any{"changes": map[string]any{}}
	if ctx == nil || ctx.Err() != nil {
		return emptyEdit
	}
	doc, parsed := s.parsed(uri)
	if doc == nil || parsed == nil || ctx.Err() != nil {
		return emptyEdit
	}
	if edit, ok := s.serverObjectRenameEditContext(ctx, parsed, position, newName); ok {
		if ctx.Err() != nil {
			return emptyEdit
		}
		return edit
	}
	if edit, ok := s.legacyUndefinedGlobalRename(ctx, parsed, position, newName); ok {
		if ctx.Err() != nil {
			return emptyEdit
		}
		return edit
	}
	if region := core.RegionAt(parsed, doc.OffsetAt(position)); region != nil && region.Language == core.LanguageCSS {
		if ctx.Err() != nil {
			return emptyEdit
		}
		s.cssMu.Lock()
		serviceEdit := s.css.Rename(parsed, position, newName)
		s.cssMu.Unlock()
		if ctx.Err() != nil {
			return emptyEdit
		}
		if serviceEdit != nil {
			changes := serviceEdit.Changes
			if target, ok := embeddedRenameTarget(parsed, doc.OffsetAt(position)); ok && target.Kind == embeddedRenameClass {
				for changedURI, edits := range s.embeddedClassRenameChanges(parsed, target.Name, newName) {
					if ctx.Err() != nil {
						return emptyEdit
					}
					changes[changedURI] = appendDistinctTextEdits(changes[changedURI], edits...)
				}
				if ctx.Err() != nil {
					return emptyEdit
				}
			}
			return map[string]any{"changes": changes}
		}
	}
	if target, ok := embeddedRenameTarget(parsed, doc.OffsetAt(position)); ok && target.Kind == embeddedRenameTag {
		if ctx.Err() != nil {
			return emptyEdit
		}
		s.htmlMu.Lock()
		edit := s.html.Rename(parsed, position, newName)
		s.htmlMu.Unlock()
		if ctx.Err() != nil {
			return emptyEdit
		}
		if edit != nil {
			return map[string]any{"changes": edit.Changes}
		}
	}
	if edit, ok := s.embeddedRenameEdit(parsed, doc.OffsetAt(position), newName); ok {
		if ctx.Err() != nil {
			return emptyEdit
		}
		return edit
	}
	if isJavaScriptPosition(doc, parsed, position) {
		var edit map[string]any
		if s.javaScriptLanguageServiceRequest(ctx, uri, position, "textDocument/rename", map[string]any{"newName": newName}, &edit) && ctx.Err() == nil {
			return edit
		}
		return emptyEdit
	}
	edit := s.vbscriptRenameEditContext(ctx, parsed, position, newName)
	if ctx.Err() != nil {
		return emptyEdit
	}
	return edit
}

//lint:ignore U1000 retained for callers that use the legacy context-free helper.
func (s *Server) vbscriptRenameEdit(parsed *core.ParsedDocument, position lsp.Position, newName string) map[string]any {
	return s.vbscriptRenameEditContext(context.Background(), parsed, position, newName)
}

func (s *Server) vbscriptRenameEditContext(ctx context.Context, parsed *core.ParsedDocument, position lsp.Position, newName string) map[string]any {
	emptyEdit := map[string]any{"changes": map[string]any{}}
	if ctx == nil || ctx.Err() != nil || parsed == nil {
		return emptyEdit
	}
	if _, handled := s.vbscriptMemberDefinitionContext(ctx, parsed, position); handled {
		if ctx.Err() != nil {
			return emptyEdit
		}
		if !isVBIdentifierName(newName) {
			return map[string]any{"changes": map[string][]lsp.TextEdit{}}
		}
		locations, _ := s.vbscriptMemberReferenceLocations(ctx, parsed, position, true, s.settings.WorkspaceSymbolRename)
		if ctx.Err() != nil {
			return emptyEdit
		}
		changes := map[string][]lsp.TextEdit{}
		for _, location := range locations {
			if ctx.Err() != nil {
				return emptyEdit
			}
			changes[location.URI] = append(changes[location.URI], lsp.TextEdit{Range: location.Range, NewText: newName})
		}
		return map[string]any{"changes": changes}
	}
	edit := vbscript.RenameEdit(parsed, position, newName)
	if ctx.Err() != nil {
		return emptyEdit
	}
	if !s.settings.WorkspaceSymbolRename {
		return edit
	}
	doc := core.SourceDocument(parsed)
	word := vbscript.WordAt(parsed.Text, doc.OffsetAt(position))
	if ctx.Err() != nil {
		return emptyEdit
	}
	if word == "" {
		return edit
	}
	lower := strings.ToLower(word)
	if target, ok := vbscript.BuildReferenceShard(parsed).ReferenceTargetAt(lower, position); ok && target.ProcedureLocal {
		if ctx.Err() != nil {
			return emptyEdit
		}
		return edit
	}
	if ctx.Err() != nil {
		return emptyEdit
	}
	changes := map[string][]lsp.TextEdit{}
	if rawChanges, ok := edit["changes"].(map[string]any); ok {
		for uri, rawEdits := range rawChanges {
			if edits, ok := rawEdits.([]lsp.TextEdit); ok && len(edits) > 0 {
				changes[uri] = append([]lsp.TextEdit(nil), edits...)
			}
		}
	}
	if len(changes) == 0 {
		return edit
	}
	includedDocuments, complete := s.includedDocumentsContextResult(ctx, parsed)
	if !complete || ctx.Err() != nil {
		return emptyEdit
	}
	for _, included := range includedDocuments {
		if ctx.Err() != nil {
			return emptyEdit
		}
		occurrences := vbscript.BuildReferenceShard(included).PostingsFor(lower)
		if len(occurrences) == 0 {
			continue
		}
		edits := make([]lsp.TextEdit, 0, len(occurrences))
		for _, occurrence := range occurrences {
			edits = append(edits, lsp.TextEdit{Range: occurrence.Range, NewText: newName})
		}
		changes[included.URI] = edits
	}
	if ctx.Err() != nil {
		return emptyEdit
	}
	return map[string]any{"changes": changes}
}

func (s *Server) formatting(uri string, r *lsp.Range, options core.FormattingOptions) []lsp.TextEdit {
	scope := "document"
	if r != nil {
		scope = "range"
	}
	started := time.Now()
	_, parsed := s.parsed(uri)
	if parsed == nil {
		return nil
	}
	s.logDebugSummary("[asp-lsp] Formatting conversion started (" + scope + "): " + uri)
	options = s.formattingOptions(options)
	if strings.EqualFold(filepath.Ext(fileURIPath(uri)), ".inc") && (options.FragmentMode == "" || strings.EqualFold(options.FragmentMode, "auto")) {
		options.FragmentMode = "fragment"
	}
	options.FormatHTML = embedded.FormatHTML
	options.FormatCSS = embedded.FormatCSS
	options.FormatJavaScript = embedded.FormatJavaScript
	embeddedStarted := time.Now()
	var edits []lsp.TextEdit
	if r != nil {
		edits = core.FormatRange(parsed, *r, options)
	} else {
		edits = core.FormatDocument(parsed, options)
	}
	s.logDebugVerbose("[asp-lsp] format.embedded: " + uri + " " + formatElapsedSince(embeddedStarted))
	editCount := len(edits)
	s.logDebugSummary("[asp-lsp] Formatting conversion completed (" + scope + "): " + uri + " " + formatElapsedSince(started) + ", edits=" + strconv.Itoa(editCount))
	if edits == nil {
		return []lsp.TextEdit{}
	}
	return edits
}

func (s *Server) willSaveWaitUntil(params willSaveWaitUntilParams) []lsp.TextEdit {
	if !s.settings.FormatOnSave {
		return []lsp.TextEdit{}
	}
	return s.formatting(params.TextDocument.URI, nil, core.FormattingOptions{
		TabSize:      s.settings.FormatTabSize,
		InsertSpaces: s.settings.FormatInsertSpaces,
	})
}

func (s *Server) formattingOptions(options core.FormattingOptions) core.FormattingOptions {
	if s.settings.FormatTabSize > 0 {
		options.TabSize = s.settings.FormatTabSize
	}
	options.InsertSpaces = s.settings.FormatInsertSpaces
	options.CSSBraceStyle = s.settings.FormatCSSBraceStyle
	options.CSSInsertSpaces = s.settings.FormatCSSInsertSpaces
	options.CSSNewlineBetweenRules = s.settings.FormatCSSNewlineBetweenRules
	options.CSSNewlineBetweenSelectors = s.settings.FormatCSSNewlineBetweenSelectors
	options.CSSSpaceAroundSelectorSeparator = s.settings.FormatCSSSpaceAroundSelectorSeparator
	options.CSSTabSize = s.settings.FormatCSSTabSize
	options.CSSTagIndentMode = s.settings.FormatCSSTagIndentMode
	options.IgnoreCSSTagIndent = s.settings.FormatIgnoreCSSTagIndent
	options.CSSWrapLineLength = s.settings.FormatCSSWrapLineLength
	options.EmbeddedLanguageFormatting = s.settings.FormatEmbeddedLanguageFormatting
	options.EndOfLine = s.settings.FormatEndOfLine
	options.FragmentMode = s.settings.FormatFragmentMode
	options.IndentEmptyLines = s.settings.FormatIndentEmptyLines
	options.MaxPreserveNewLines = s.settings.FormatMaxPreserveNewLines
	options.PreserveNewLines = s.settings.FormatPreserveNewLines
	options.HTMLContentUnformatted = s.settings.FormatHTMLContentUnformatted
	options.HTMLExtraLiners = s.settings.FormatHTMLExtraLiners
	options.HTMLIndentInnerHTML = s.settings.FormatHTMLIndentInnerHTML
	options.HTMLInsertSpaces = s.settings.FormatHTMLInsertSpaces
	options.HTMLTabSize = s.settings.FormatHTMLTabSize
	options.HTMLUnformatted = s.settings.FormatHTMLUnformatted
	options.HTMLWrapAttributes = s.settings.FormatHTMLWrapAttributes
	options.HTMLWrapAttributesIndentSize = s.settings.FormatHTMLWrapAttributesIndentSize
	options.HTMLWrapLineLength = s.settings.FormatHTMLWrapLineLength
	options.JavaScriptBraceStyle = s.settings.FormatJavaScriptBraceStyle
	options.JavaScriptBraceFunctionsNewLine = s.settings.FormatJavaScriptBraceFunctionsNewLine
	options.JavaScriptBraceControlNewLine = s.settings.FormatJavaScriptBraceControlNewLine
	options.JavaScriptInsertSpaces = s.settings.FormatJavaScriptInsertSpaces
	options.JavaScriptTagIndentMode = s.settings.FormatJavaScriptTagIndentMode
	options.IgnoreJavaScriptTagIndent = s.settings.FormatIgnoreJavaScriptTagIndent
	options.JavaScriptSemicolons = s.settings.FormatJavaScriptSemicolons
	options.JavaScriptIndentSwitchCase = s.settings.FormatJavaScriptIndentSwitchCase
	options.JavaScriptSpaceAfterComma = s.settings.FormatJavaScriptSpaceAfterComma
	options.JavaScriptSpaceAfterForSemicolon = s.settings.FormatJavaScriptSpaceAfterForSemicolon
	options.JavaScriptSpaceAroundBinaryOps = s.settings.FormatJavaScriptSpaceAroundBinaryOps
	options.JavaScriptSpaceAfterAnonFunction = s.settings.FormatJavaScriptSpaceAfterAnonFunction
	options.JavaScriptSpaceAfterNamedFunction = s.settings.FormatJavaScriptSpaceAfterNamedFunction
	options.JavaScriptSpaceBeforeConditional = s.settings.FormatJavaScriptSpaceBeforeConditional
	options.JavaScriptSpaceInsideParentheses = s.settings.FormatJavaScriptSpaceInsideParentheses
	options.JavaScriptSpaceInsideBrackets = s.settings.FormatJavaScriptSpaceInsideBrackets
	options.JavaScriptSpaceInsideBraces = s.settings.FormatJavaScriptSpaceInsideBraces
	options.JavaScriptSpaceInsideEmptyBraces = s.settings.FormatJavaScriptSpaceInsideEmptyBraces
	options.JavaScriptTabSize = s.settings.FormatJavaScriptTabSize
	options.NestedASPInCSSJS = s.settings.FormatNestedASPInCSSJS
	options.JScriptInsertSpaces = s.settings.FormatJScriptInsertSpaces
	options.JScriptTabSize = s.settings.FormatJScriptTabSize
	options.InsertFinalNewline = s.settings.FormatInsertFinalNewline
	options.PrintWidth = s.settings.FormatPrintWidth
	if s.settings.FormatVBScriptTabSize > 0 {
		options.VBScriptTabSize = s.settings.FormatVBScriptTabSize
	}
	options.VBScriptInsertSpaces = s.settings.FormatVBScriptInsertSpaces
	options.VBScriptKeywordCase = s.settings.FormatVBScriptKeywordCase
	options.VBScriptLineContinuationIndentSize = s.settings.FormatVBScriptLineContinuationIndentSize
	options.VBScriptSelectCaseIndent = s.settings.FormatVBScriptSelectCaseIndent
	options.VBScriptBlockIndent = s.settings.FormatVBScriptBlockIndent
	options.VBScriptTagIndentMode = s.settings.FormatVBScriptTagIndentMode
	options.IgnoreVBScriptTagIndent = s.settings.FormatIgnoreVBScriptTagIndent
	options.UppercaseKeywords = s.settings.FormatUppercaseKeywords
	options.AlignAssignments = s.settings.FormatAlignAssignments
	options.ASPDelimiterSpacing = s.settings.FormatASPDelimiterSpacing
	options.ASPBlockNewline = s.settings.FormatASPBlockNewline
	options.RespectDisableRegions = s.settings.FormatRespectDisableRegions
	if s.settings.FormatEnabledLanguages != nil {
		options.EnabledLanguages = append([]string(nil), s.settings.FormatEnabledLanguages...)
	}
	return options
}
