package lspserver

import (
	"context"
	"encoding/json"
	"os"
	"path/filepath"
	"regexp"
	"sort"
	"strings"

	"github.com/yottonoko/classic-web-system-lsp/internal/core"
	"github.com/yottonoko/classic-web-system-lsp/internal/lsp"
	"github.com/yottonoko/classic-web-system-lsp/internal/vbscript"
)

func (s *Server) hover(uri string, position lsp.Position) any {
	return s.hoverContext(context.Background(), uri, position)
}

func (s *Server) hoverContext(ctx context.Context, uri string, position lsp.Position) any {
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
	offset := doc.OffsetAt(position)
	region := core.RegionAt(parsed, offset)
	word := ""
	if region != nil && region.Language == core.LanguageVBScript {
		word = vbscript.WordAt(parsed.Text, offset)
		if tokenKind, ok := vbscriptNonCodeTokenKindAtOffset(parsed, offset); ok {
			if tokenKind != vbscriptHoverTokenComment {
				return nil
			}
			if hover := vbscriptCommentHover(doc.Text, offset); hover != nil {
				return hover
			}
			if !vbscriptXMLDocCrefAtOffset(parsed, word, position) {
				return nil
			}
		}
	}
	objectAt, objectAtName := serverObjectSymbolAt(parsed, position)
	if word == "" && objectAtName {
		word = objectAt.Declaration.Name
	}
	qualifiedMember := word != "" && vbscriptQualifiedMemberAtOffset(parsed.Text, offset)
	if word != "" && !qualifiedMember {
		if hover := vbscriptParameterHoverAtOffset(parsed, offset, s.settings.Locale); hover != nil {
			return hover
		}
		if hover := vbscriptPropertySignatureHoverWithLocale(parsed, offset, s.settings.Locale); hover != nil {
			return hover
		}
	}
	if word != "" && !qualifiedMember {
		if hover := s.vbscriptClassMemberHoverContext(ctx, parsed, offset, word); hover != nil {
			return hover
		}
	}
	rootDeclaration, rootDeclarationFound := vbscriptRootDeclarationAtOffset(parsed, word, offset)
	rootFirstDeclaration, rootDeclarationExists := vbscriptRootDeclarationAtOffset(parsed, word, len(parsed.Text))
	var includedIndex *includedServerObjectIndex
	includedIndexComplete := true
	includedGlobalShadow := false
	includedProcedureShadow := false
	includedObjectShadow := false
	includedPrefixIncomplete := false
	rootDeclarationStart := -1
	if word != "" && !qualifiedMember {
		if rootDeclarationExists {
			rootDeclarationStart = rootFirstDeclaration.Start
		}
		includedIndex, includedIndexComplete = s.includedServerObjectIndexBeforeRootFastPath(ctx, parsed, position, word)
		if includedIndex == nil && !includedIndexComplete && ctx.Err() != nil {
			return nil
		}
		if includedIndex != nil {
			if kind, shadowed := includedNameVisibleKind(includedIndex, word, offset, rootDeclarationStart); shadowed {
				includedGlobalShadow = kind == "global"
				includedProcedureShadow = kind == "procedure"
				includedObjectShadow = kind == "object"
			}
			includedPrefixIncomplete = !includedIndexComplete
		}
	}
	if ctx.Err() != nil {
		return nil
	}
	rootResolutionInconclusive := rootDeclarationExists && !rootDeclarationFound
	if includedPrefixIncomplete && vbscriptRootResolutionInconclusive(parsed, offset, rootDeclarationStart) {
		rootResolutionInconclusive = true
	}
	if includedObjectShadow {
		if includedIndexComplete {
			if hover := s.serverObjectHoverContext(ctx, parsed, position); hover != nil {
				return hover
			}
		}
		if hover, ok := s.includedNameIndexHover(ctx, includedIndex, word); ok {
			return hover
		}
		if ctx.Err() != nil {
			return nil
		}
		return nil
	}
	if includedProcedureShadow {
		if includedIndexComplete {
			if hover, ok := s.vbscriptIncludedProcedureHoverContext(ctx, parsed, offset, word); ok && hover != nil {
				return hover
			}
		}
		if hover, ok := s.includedNameIndexHover(ctx, includedIndex, word); ok {
			return hover
		}
		if ctx.Err() != nil {
			return nil
		}
		return nil
	}
	if includedGlobalShadow {
		if includedIndexComplete {
			if hover := s.includedVBScriptVariableHoverContext(ctx, parsed, offset); hover != nil {
				return hover
			}
		}
		if hover, ok := s.includedNameIndexHover(ctx, includedIndex, word); ok {
			return hover
		}
		if ctx.Err() != nil {
			return nil
		}
		return nil
	}
	if rootDeclarationFound && !qualifiedMember && !vbscriptRootDeclarationShadowedAt(parsed, word, offset) && !rootResolutionInconclusive {
		if hover := s.vbscriptRootDeclarationHoverFastPath(ctx, parsed, position, offset, rootDeclaration); hover != nil {
			return hover
		}
		if ctx.Err() != nil {
			return nil
		}
	}
	if region != nil && (region.Language == core.LanguageJavaScript || region.Language == core.LanguageJScript) {
		var hover lsp.Hover
		if s.javaScriptLanguageServiceRequest(ctx, uri, position, "textDocument/hover", nil, &hover) && ctx.Err() == nil {
			return &hover
		}
		return nil
	}
	if region != nil && region.Language == core.LanguageCSS {
		if ctx.Err() != nil {
			return nil
		}
		s.cssMu.Lock()
		defer s.cssMu.Unlock()
		if hover := s.css.Hover(parsed, position); hover != nil {
			if ctx.Err() != nil {
				return nil
			}
			return hover
		}
	}
	if region != nil && region.Language == core.LanguageHTML {
		if ctx.Err() != nil {
			return nil
		}
		s.htmlMu.Lock()
		defer s.htmlMu.Unlock()
		if hover := s.html.Hover(parsed, position); hover != nil {
			if ctx.Err() != nil {
				return nil
			}
			return hover
		}
	}
	if word != "" && !qualifiedMember && vbLocalDeclarationShadowsNameAt(parsed, word, position) {
		if hover, handled := s.vbscriptVariableHoverAtOffsetContext(ctx, parsed, offset); handled {
			if ctx.Err() != nil {
				return nil
			}
			return hover
		}
	}
	if rootResolutionInconclusive && !qualifiedMember && !vbscriptRootDeclarationShadowedAt(parsed, word, offset) {
		if word != "" && len(parsed.Includes) > 0 {
			scope := vbscriptScopeAtOffset(parsed, offset)
			if scope == "" || !vbscriptNameBoundInScopeAtOffset(parsed, word, scope, offset) {
				hover := s.includedVBScriptVariableHoverContext(ctx, parsed, offset)
				if hover != nil {
					return hover
				}
			}
		}
		// A bounded include prefix cannot prove that an unqualified name is not
		// user-defined. Stop before built-in fallback can mask a later declaration.
		return nil
	}
	var globalBuiltinHover *lsp.Hover
	if isStandaloneVBScriptDocument(parsed) {
		globalBuiltinHover = vbscript.StandaloneHoverAt(doc.Text, offset)
	} else {
		globalBuiltinHover = vbscript.HoverAt(doc.Text, offset)
	}
	if globalBuiltinHover != nil {
		if qualifiedMember {
			if hover := s.vbscriptBuiltinMemberHoverContext(ctx, parsed, offset); hover == nil {
				if hover, handled := s.vbscriptMemberHoverContextRootLocal(ctx, parsed, offset); handled {
					return hover
				}
				return nil
			}
		}
		if ctx.Err() != nil {
			return nil
		}
		return s.enrichVBScriptBuiltInHover(doc.Text, offset, globalBuiltinHover)
	}
	if ctx.Err() != nil {
		return nil
	}
	if hover := vbscriptCommentHover(doc.Text, offset); hover != nil {
		if ctx.Err() != nil {
			return nil
		}
		return hover
	}
	if hover := s.vbscriptBuiltinMemberHoverContext(ctx, parsed, offset); hover != nil {
		if ctx.Err() != nil {
			return nil
		}
		return hover
	}
	if hover, handled := s.vbscriptMemberHoverContextRootLocal(ctx, parsed, offset); handled {
		if ctx.Err() != nil {
			return nil
		}
		return hover
	}
	if ctx.Err() != nil {
		return nil
	}
	if hover := vbscriptParameterDeclarationHover(parsed, offset, s.settings.Locale); hover != nil {
		if ctx.Err() != nil {
			return nil
		}
		return hover
	}
	if word == "" && region != nil && region.Language == core.LanguageVBScript {
		word = vbscript.WordAt(parsed.Text, offset)
	}
	if word != "" && !qualifiedMember && len(parsed.Includes) > 0 {
		scope := vbscriptScopeAtOffset(parsed, offset)
		if scope == "" || !vbscriptNameBoundInScopeAtOffset(parsed, word, scope, offset) {
			hover := s.includedVBScriptVariableHoverContext(ctx, parsed, offset)
			if hover != nil {
				return hover
			}
		}
	}
	if rootResolutionInconclusive && !vbscriptRootDeclarationShadowedAt(parsed, word, offset) {
		// A bounded include prefix cannot prove that a root declaration is
		// unshadowed. Every root fallback must stop here.
		return nil
	}
	if hover, handled := s.vbscriptVariableHoverAtOffsetContext(ctx, parsed, offset); handled && hover != nil {
		if ctx.Err() != nil {
			return nil
		}
		return hover
	}
	if ctx.Err() != nil {
		return nil
	}
	hover := s.vbscriptSignatureHoverContext(ctx, parsed, offset)
	if ctx.Err() != nil {
		return nil
	}
	if hover != nil {
		return hover
	}
	if ctx.Err() != nil {
		return nil
	}
	return s.legacyUndefinedGlobalHover(ctx, parsed, position)
}

const vbscriptHoverNonCodeRuntimeKey = "lspserver.vbscript-hover-non-code.runtime.v1"

const (
	vbscriptHoverTokenComment uint8 = iota + 1
	vbscriptHoverTokenString
	vbscriptHoverTokenDate
)

type vbscriptHoverNonCodeSpan struct {
	start int
	end   int
	kind  uint8
}

type vbscriptHoverNonCodeIndex struct {
	spans []vbscriptHoverNonCodeSpan
}

func (index vbscriptHoverNonCodeIndex) EstimateBytes() int64 {
	return int64(32 + cap(index.spans)*24)
}

func vbscriptNonCodeTokenKindAtOffset(parsed *core.ParsedDocument, offset int) (uint8, bool) {
	if parsed == nil || offset < 0 || offset >= len(parsed.Text) {
		return 0, false
	}
	index := vbscriptHoverNonCodeIndexFor(parsed)
	spanIndex := sort.Search(len(index.spans), func(spanIndex int) bool {
		return index.spans[spanIndex].end > offset
	})
	if spanIndex >= len(index.spans) {
		return 0, false
	}
	span := index.spans[spanIndex]
	if span.start <= offset {
		return span.kind, true
	}
	return 0, false
}

func vbscriptXMLDocCrefAtOffset(parsed *core.ParsedDocument, name string, position lsp.Position) bool {
	if parsed == nil || name == "" {
		return false
	}
	for _, posting := range vbscript.BuildReferenceShard(parsed).PostingsFor(name) {
		if posting.HasRole(vbscript.ReferenceRoleCref) && lspPositionInRange(position, posting.Range) {
			return true
		}
	}
	return false
}

func vbscriptHoverNonCodeIndexFor(parsed *core.ParsedDocument) vbscriptHoverNonCodeIndex {
	if cached, ok := parsed.LoadRuntimeAnalysis(vbscriptHoverNonCodeRuntimeKey); ok {
		if index, valid := cached.(vbscriptHoverNonCodeIndex); valid {
			return index
		}
	}
	index := vbscriptHoverNonCodeIndex{spans: make([]vbscriptHoverNonCodeSpan, 0)}
	for _, region := range parsed.Regions {
		if region.Language != core.LanguageVBScript {
			continue
		}
		start := max(0, min(region.ContentStart, len(parsed.Text)))
		end := max(start, min(region.ContentEnd, len(parsed.Text)))
		for _, token := range vbscript.Tokenize(parsed.Text[start:end]) {
			var kind uint8
			switch token.Kind {
			case "comment":
				kind = vbscriptHoverTokenComment
			case "string":
				kind = vbscriptHoverTokenString
			case "date":
				kind = vbscriptHoverTokenDate
			default:
				continue
			}
			index.spans = append(index.spans, vbscriptHoverNonCodeSpan{
				start: start + token.Start,
				end:   start + token.End,
				kind:  kind,
			})
		}
	}
	sort.Slice(index.spans, func(left, right int) bool {
		return index.spans[left].start < index.spans[right].start
	})
	parsed.StoreRuntimeAnalysis(vbscriptHoverNonCodeRuntimeKey, index)
	return index
}

func includedNameVisibleKind(index *includedServerObjectIndex, name string, offset, rootStart int) (string, bool) {
	if index == nil || name == "" {
		return "", false
	}
	lowerName := strings.ToLower(name)
	if entry, ok := index.First[lowerName]; ok && entry.VisibleAt < offset && (rootStart < 0 || entry.VisibleAt < rootStart) {
		return entry.Kind, true
	}
	// Keep compatibility with indexes created before First was added. Complete
	// indexes built by the current code always take the path above.
	visibleAt := offset
	kind := ""
	for _, candidate := range []struct {
		kind string
		at   int
		ok   bool
	}{
		{kind: "global", at: index.Globals[lowerName], ok: hasIncludedName(index.Globals, lowerName)},
		{kind: "procedure", at: index.Procedures[lowerName], ok: hasIncludedName(index.Procedures, lowerName)},
		{kind: "object", at: index.Objects[lowerName], ok: hasIncludedName(index.Objects, lowerName)},
	} {
		if !candidate.ok || candidate.at >= offset || candidate.at >= visibleAt || rootStart >= 0 && candidate.at >= rootStart {
			continue
		}
		visibleAt = candidate.at
		kind = candidate.kind
	}
	return kind, kind != ""
}

// includedNameIndexHover renders a declaration that the bounded include walk
// already proved to be the first visible name. It deliberately consumes only
// immutable metadata retained by includedServerObjectIndex; no parsed include
// document graph is recovered for an incomplete prefix.
func (s *Server) includedNameIndexHover(ctx context.Context, index *includedServerObjectIndex, name string) (*lsp.Hover, bool) {
	if index == nil || name == "" || ctx != nil && ctx.Err() != nil {
		return nil, false
	}
	entry, ok := index.First[strings.ToLower(name)]
	if !ok || entry.URI == "" {
		return nil, false
	}
	if ctx != nil && ctx.Err() != nil {
		return nil, false
	}
	switch entry.Kind {
	case "global":
		declaration := entry.Declaration
		if declaration.Name == "" {
			declaration.Name = name
		}
		typeName := strings.TrimSpace(entry.TypeName)
		if typeName == "" {
			typeName = "Variant"
		}
		keyword := "Dim"
		if declaration.Kind == "constant" || declaration.Kind == "const" {
			keyword = "Const"
		}
		value := markdownVBScriptSignature("(global) "+keyword+" "+declaration.Name+" As "+typeName, "")
		if note := s.vbscriptDefinedInNote(entry.URI); note != "" {
			value += "\n\n" + note
		}
		return &lsp.Hover{Contents: lsp.MarkupContent{Kind: "markdown", Value: value}}, true
	case "procedure":
		signature := entry.Signature
		if signature.Name == "" {
			signature.Name = name
		}
		hover := vbscriptSignatureHover(signature, vbscriptXMLDoc{}, false, s.settings.Locale)
		return s.appendVBScriptDefinedInHover(hover, entry.URI), true
	case "object":
		declaration := entry.Declaration
		if declaration.Name == "" {
			declaration.Name = name
		}
		typeName := strings.TrimSpace(entry.TypeName)
		if typeName == "" {
			typeName = "Object"
		}
		lines := []string{"```vbscript", "(global) Dim " + declaration.Name + " As " + typeName, "```", "", "Server OBJECT declaration."}
		for _, attribute := range entry.ObjectAttributes {
			lines = append(lines, "", "- `"+attribute.Name+"`: `"+attribute.Value+"`")
		}
		if note := s.vbscriptDefinedInNote(entry.URI); note != "" {
			lines = append(lines, "", note)
		}
		return &lsp.Hover{Contents: lsp.MarkupContent{Kind: "markdown", Value: strings.Join(lines, "\n")}}, true
	default:
		return nil, false
	}
}

func hasIncludedName(declarations map[string]int, name string) bool {
	_, ok := declarations[name]
	return ok
}

func (s *Server) vbscriptIncludedProcedureHoverContext(ctx context.Context, parsed *core.ParsedDocument, offset int, name string) (*lsp.Hover, bool) {
	if parsed == nil || name == "" {
		return nil, false
	}
	if ctx == nil {
		ctx = context.Background()
	}
	units, complete := s.vbscriptIncludeExecutionUnitsThroughOffsetContextResult(ctx, parsed, offset)
	if !complete || ctx.Err() != nil {
		return nil, false
	}
	lowerName := strings.ToLower(name)
	for _, unit := range units {
		if ctx.Err() != nil {
			return nil, false
		}
		if unit.document == nil || unit.start >= unit.end {
			continue
		}
		document := core.NewTextDocument(unit.document.URI, "classic-asp", 0, unit.document.Text)
		for _, signature := range vbscript.Signatures(unit.document) {
			if strings.ToLower(signature.Name) != lowerName || vbClassMemberLineOwners(unit.document)[signature.NameRange.Start.Line] != "" {
				continue
			}
			start := document.OffsetAt(signature.NameRange.Start)
			if start < unit.start || start >= unit.end {
				continue
			}
			signature = annotatedVBScriptSignature(unit.document, signature)
			hover := vbscriptSignatureHover(signature, vbscriptXMLDocForSignature(unit.document, signature), false, s.settings.Locale)
			if unit.document != parsed {
				hover.Range = nil
				hover = s.appendVBScriptDefinedInHover(hover, unit.document.URI)
			}
			return hover, true
		}
	}
	return nil, false
}

func (s *Server) includedServerObjectIndexBeforeRootFastPath(ctx context.Context, parsed *core.ParsedDocument, position lsp.Position, word string) (*includedServerObjectIndex, bool) {
	if parsed == nil || word == "" || len(parsed.Includes) == 0 || ctx != nil && ctx.Err() != nil {
		return nil, ctx == nil || ctx.Err() == nil
	}
	// Procedure locals, parameters, and members remain authoritative even when
	// an included document exposes the same name. Avoid the include lookup for
	// those shadowed scopes.
	document := core.NewTextDocument(parsed.URI, "classic-asp", 0, parsed.Text)
	if vbLocalDeclarationShadowsNameAt(parsed, word, position) {
		return nil, true
	}
	if _, ok := vbscriptClassMemberDeclarationAtOffset(parsed, word, document.OffsetAt(position)); ok {
		return nil, true
	}
	return s.includedServerObjectIndexContext(ctx, parsed)
}

// vbscriptClassMemberDeclarationAtOffset returns the enclosing class member
// declaration for an unqualified name. Class members are visible throughout
// their class, while procedure locals and parameters retain precedence.
func vbscriptClassMemberDeclarationAtOffset(parsed *core.ParsedDocument, name string, offset int) (vbUsageDeclaration, bool) {
	if parsed == nil || name == "" {
		return vbUsageDeclaration{}, false
	}
	document := core.NewTextDocument(parsed.URI, "classic-asp", 0, parsed.Text)
	if offset < 0 {
		offset = 0
	}
	if offset > len(parsed.Text) {
		offset = len(parsed.Text)
	}
	position := document.PositionAt(offset)
	if vbLocalDeclarationShadowsNameAt(parsed, name, position) {
		return vbUsageDeclaration{}, false
	}
	classScope := ""
	if scope := vbscriptScopeAtOffset(parsed, offset); scope != "" {
		classScope = vbscriptClassScopeForProcedure(parsed, scope)
	}
	if classScope == "" {
		classScope = strings.TrimSpace(vbClassMemberLineOwners(parsed)[position.Line])
	}
	if classScope == "" {
		return vbUsageDeclaration{}, false
	}
	var fallback vbUsageDeclaration
	for _, declaration := range collectVBNamingDeclarations(parsed) {
		if declaration.Scope != "" || declaration.MemberOf == "" ||
			!strings.EqualFold(declaration.MemberOf, classScope) || !strings.EqualFold(declaration.Name, name) {
			continue
		}
		switch strings.ToLower(declaration.Kind) {
		case "field", "property", "method", "constant", "const", "variable":
			if positionInRangeOffset(offset, declaration.Range, parsed) {
				return declaration, true
			}
			if fallback.Name == "" {
				fallback = declaration
			}
		}
	}
	return fallback, fallback.Name != ""
}

func (s *Server) vbscriptClassMemberHoverContext(ctx context.Context, parsed *core.ParsedDocument, offset int, name string) *lsp.Hover {
	declaration, ok := vbscriptClassMemberDeclarationAtOffset(parsed, name, offset)
	if !ok || ctx != nil && ctx.Err() != nil {
		return nil
	}
	hover, handled := s.vbscriptMemberHoverContext(ctx, parsed, declaration.Start)
	if !handled {
		return nil
	}
	return hover
}

func (s *Server) vbscriptMemberHoverContextRootLocal(ctx context.Context, parsed *core.ParsedDocument, offset int) (*lsp.Hover, bool) {
	target, ok := s.vbscriptMemberTargetAtContext(ctx, parsed, offset)
	if !ok {
		return nil, false
	}
	hover, handled := s.vbscriptMemberHoverContext(ctx, parsed, offset)
	if hover == nil || len(target.declarations) == 0 || target.declarations[0].document == parsed {
		return hover, handled
	}
	hover.Range = nil
	return hover, handled
}

// vbscriptRootDeclarationHoverFastPath handles root declarations without
// waiting for include-expanded type state. References still use the
// source-ordered include path so included declarations retain precedence.
type vbscriptRootDeclaration struct {
	Kind      string
	Start     int
	End       int
	Variable  vbUsageDeclaration
	Signature vbscript.Signature
	Object    serverObjectSymbol
}

// vbscriptRootDeclarationAtOffset returns the first root-level declaration for
// name that is visible at offset. The declaration kind is part of the result so
// hover rendering cannot accidentally select a later variable, object, or
// signature through a kind-specific fallback.
func vbscriptRootDeclarationAtOffset(parsed *core.ParsedDocument, name string, offset int) (vbscriptRootDeclaration, bool) {
	if parsed == nil || name == "" {
		return vbscriptRootDeclaration{}, false
	}
	if offset < 0 {
		offset = 0
	}
	if offset > len(parsed.Text) {
		offset = len(parsed.Text)
	}
	lowerName := strings.ToLower(name)
	winner := vbscriptRootDeclaration{}
	found := false
	forwardProcedure := vbscriptRootDeclaration{}
	forwardProcedureFound := false
	consider := func(candidate vbscriptRootDeclaration) {
		if candidate.Start > offset || candidate.Start < 0 || found && candidate.Start > winner.Start {
			return
		}
		if found && candidate.Start == winner.Start && candidate.Kind != "object" {
			return
		}
		winner = candidate
		found = true
	}
	document := core.NewTextDocument(parsed.URI, "classic-asp", 0, parsed.Text)
	classOwners := vbClassMemberLineOwners(parsed)
	for _, signature := range vbscript.Signatures(parsed) {
		if !strings.EqualFold(signature.Name, lowerName) || classOwners[signature.NameRange.Start.Line] != "" {
			continue
		}
		start := document.OffsetAt(signature.Range.Start)
		candidate := vbscriptRootDeclaration{Kind: "procedure", Start: start, End: document.OffsetAt(signature.Range.End), Signature: signature}
		if start > offset {
			if !forwardProcedureFound || start < forwardProcedure.Start {
				forwardProcedure = candidate
				forwardProcedureFound = true
			}
			continue
		}
		consider(candidate)
	}
	for _, declaration := range variableInlayDeclarations(parsed, true, nil) {
		if declaration.Implicit || declaration.Local || declaration.Scope != "" || declaration.MemberOf != "" || !isVBVariableInlayDeclaration(declaration) || vbscriptDeclarationIsClassMember(parsed, declaration) || !strings.EqualFold(declaration.Name, lowerName) {
			continue
		}
		consider(vbscriptRootDeclaration{Kind: declaration.Kind, Start: declaration.Start, End: declaration.End, Variable: declaration})
	}
	for _, symbol := range serverObjectSymbols(parsed) {
		if !strings.EqualFold(symbol.Declaration.Name, lowerName) {
			continue
		}
		consider(vbscriptRootDeclaration{Kind: "object", Start: symbol.Declaration.Start, End: symbol.Declaration.End, Object: symbol})
	}
	if found {
		return winner, true
	}
	return forwardProcedure, forwardProcedureFound
}

func vbscriptQualifiedMemberAtOffset(text string, offset int) bool {
	if offset < 0 || offset > len(text) {
		return false
	}
	start := offset
	if start == len(text) || start < len(text) && !isVBIdentifier(text[start]) {
		start--
	}
	if start < 0 || !isVBIdentifier(text[start]) {
		return false
	}
	for start > 0 && isVBIdentifier(text[start-1]) {
		start--
	}
	for start > 0 && isVBWhitespace(text[start-1]) {
		start--
	}
	return start > 0 && text[start-1] == '.'
}

func vbscriptRootDeclarationShadowedAt(parsed *core.ParsedDocument, name string, offset int) bool {
	if parsed == nil || name == "" {
		return false
	}
	scope := vbscriptScopeAtOffset(parsed, offset)
	if scope == "" {
		return false
	}
	document := core.NewTextDocument(parsed.URI, "classic-asp", 0, parsed.Text)
	position := document.PositionAt(offset)
	return vbLocalDeclarationShadowsNameAt(parsed, name, position) || vbscriptClassMemberShadowsNameAt(parsed, name, scope)
}

func vbscriptRootResolutionInconclusive(parsed *core.ParsedDocument, offset, rootStart int) bool {
	if parsed == nil || len(parsed.Includes) == 0 {
		return false
	}
	document := core.NewTextDocument(parsed.URI, "classic-asp", 0, parsed.Text)
	firstInclude := len(parsed.Text) + 1
	for _, include := range parsed.Includes {
		start := document.OffsetAt(include.Range.Start)
		if start < offset && start < firstInclude {
			firstInclude = start
		}
	}
	if firstInclude == len(parsed.Text)+1 {
		return false
	}
	return rootStart < 0 || rootStart >= firstInclude
}

func (s *Server) vbscriptRootDeclarationHoverFastPath(ctx context.Context, parsed *core.ParsedDocument, position lsp.Position, offset int, candidate vbscriptRootDeclaration) *lsp.Hover {
	if ctx == nil {
		ctx = context.Background()
	}
	if parsed == nil || ctx.Err() != nil {
		return nil
	}
	switch candidate.Kind {
	case "variable", "constant", "const":
		declaration := candidate.Variable
		typeName := vbscriptFastDeclarationTypeAtOffset(parsed, declaration, offset)
		return s.vbscriptVariableHoverForDeclarationWithTypeAtOffset(parsed, offset, "", true, s.settings.Locale, &declaration, typeName)
	case "procedure":
		signature := candidate.Signature
		annotations := graphAnalysisTypes(parsed)
		if ctx.Err() != nil {
			return nil
		}
		missingTypeMetadata := positionInRangeOffset(offset, signature.NameRange, parsed) && vbscriptSignatureMissingTypeMetadata(parsed, signature, &annotations)
		signature = annotatedVBScriptSignature(parsed, signature)
		if ctx.Err() != nil {
			return nil
		}
		return vbscriptSignatureHover(signature, vbscriptXMLDocForSignature(parsed, signature), missingTypeMetadata, s.settings.Locale)
	case "object":
		return s.serverObjectHoverContext(ctx, parsed, position)
	default:
		return nil
	}
}

func vbscriptFastDeclarationTypeAtOffset(parsed *core.ParsedDocument, declaration vbUsageDeclaration, offset int) string {
	if parsed == nil {
		return "Variant"
	}
	if typeName := strings.TrimSpace(declaration.TypeName); typeName != "" && !declaration.Implicit {
		return typeName
	}
	if typeName := strings.TrimSpace(precedingVBTypeAnnotation(parsed, declaration.Line, declaration.Name)); typeName != "" {
		return typeName
	}
	if declaration.Kind == "constant" || declaration.Kind == "const" {
		if typeName := strings.TrimSpace(inferVBDeclarationType(parsed, declaration)); typeName != "" {
			return typeName
		}
	}
	document := core.NewTextDocument(parsed.URI, "classic-asp", 0, parsed.Text)
	var inferred vbscriptType
	foundAssignment := false
	unknownAssignment := false
	for _, assignment := range vbscriptAssignments(parsed) {
		if !strings.EqualFold(assignment.Name, declaration.Name) || !strings.EqualFold(assignment.Scope, declaration.Scope) || document.OffsetAt(assignment.NameRange.Start) > offset {
			continue
		}
		foundAssignment = true
		value := inferVBScriptValueLiteralType(assignment.Value)
		if value.isUnknown() {
			unknownAssignment = true
			continue
		}
		inferred = mergeVBScriptMutableTypes(inferred, value)
	}
	if foundAssignment && !unknownAssignment && !inferred.isUnknown() {
		return inferred.String()
	}
	return "Variant"
}

type vbscriptVariableHoverMatch struct {
	declaration vbUsageDeclaration
	stateOffset int
	scope       string
}

func (s *Server) vbscriptVariableHoverMatchAtOffset(ctx context.Context, parsed *core.ParsedDocument, offset int, word string) (vbscriptVariableHoverMatch, bool) {
	if ctx == nil {
		ctx = context.Background()
	}
	if parsed == nil || word == "" || ctx.Err() != nil {
		return vbscriptVariableHoverMatch{}, false
	}
	stateOffset := offset
	document := core.NewTextDocument(parsed.URI, "classic-asp", 0, parsed.Text)
	for _, assignment := range vbscriptAssignments(parsed) {
		if ctx.Err() != nil {
			return vbscriptVariableHoverMatch{}, false
		}
		assignmentOffset := document.OffsetAt(assignment.NameRange.Start)
		if assignmentOffset == offset && strings.EqualFold(assignment.Name, word) {
			stateOffset = document.OffsetAt(assignment.NameRange.End)
			break
		}
	}
	scope := vbscriptScopeAtOffset(parsed, offset)
	localShadow := scope != "" && vbscriptNameBoundInScopeAtOffset(parsed, word, scope, offset)
	for _, declaration := range variableInlayDeclarations(parsed, true, nil) {
		if ctx.Err() != nil {
			return vbscriptVariableHoverMatch{}, false
		}
		if declaration.Start > offset || !isVBVariableInlayDeclaration(declaration) || !strings.EqualFold(declaration.Name, word) {
			continue
		}
		if declaration.Local && !strings.EqualFold(declaration.Scope, scope) {
			continue
		}
		if !declaration.Local && localShadow {
			continue
		}
		return vbscriptVariableHoverMatch{
			declaration: declaration,
			stateOffset: stateOffset,
			scope:       scope,
		}, true
	}
	return vbscriptVariableHoverMatch{}, false
}

func (s *Server) vbscriptVariableHover(parsed *core.ParsedDocument, offset int, sourceURI string, includeLocal bool, locale string) *lsp.Hover {
	return s.vbscriptVariableHoverForDeclaration(parsed, offset, sourceURI, includeLocal, locale, nil)
}

func (s *Server) vbscriptVariableHoverForDeclaration(parsed *core.ParsedDocument, offset int, sourceURI string, includeLocal bool, locale string, target *vbUsageDeclaration) *lsp.Hover {
	return s.vbscriptVariableHoverForDeclarationWithType(parsed, offset, sourceURI, includeLocal, locale, target, "")
}

func (s *Server) vbscriptVariableHoverForDeclarationWithType(parsed *core.ParsedDocument, offset int, sourceURI string, includeLocal bool, locale string, target *vbUsageDeclaration, typeOverride string) *lsp.Hover {
	return s.vbscriptVariableHoverForDeclarationWithTypeMode(parsed, offset, sourceURI, includeLocal, locale, target, typeOverride, false)
}

func (s *Server) vbscriptVariableHoverForDeclarationWithTypeAtOffset(parsed *core.ParsedDocument, offset int, sourceURI string, includeLocal bool, locale string, target *vbUsageDeclaration, typeOverride string) *lsp.Hover {
	return s.vbscriptVariableHoverForDeclarationWithTypeMode(parsed, offset, sourceURI, includeLocal, locale, target, typeOverride, true)
}

func (s *Server) vbscriptVariableHoverForDeclarationWithTypeMode(parsed *core.ParsedDocument, offset int, sourceURI string, includeLocal bool, locale string, target *vbUsageDeclaration, typeOverride string, positionAware bool) *lsp.Hover {
	word := vbscript.WordAt(parsed.Text, offset)
	if word == "" {
		return nil
	}
	annotations := graphAnalysisTypes(parsed)
	variableDeclarations := variableInlayDeclarations(parsed, true, nil)
	scope := vbscriptScopeAtOffset(parsed, offset)
	localShadow := scope != "" && vbscriptNameBoundInScope(parsed, word, scope)
	if positionAware {
		localShadow = scope != "" && vbscriptNameBoundInScopeAtOffset(parsed, word, scope, offset)
	}
	for _, declaration := range variableDeclarations {
		if positionAware && declaration.Start > offset {
			continue
		}
		if target != nil && (declaration.Start != target.Start || declaration.End != target.End) {
			continue
		}
		if declaration.Local && !includeLocal {
			continue
		}
		if declaration.Local && !strings.EqualFold(declaration.Scope, scope) {
			continue
		}
		if !declaration.Local && localShadow {
			continue
		}
		if !strings.EqualFold(declaration.Name, word) {
			continue
		}
		// Display the same declaration-wide type as inlay hints. Position-aware
		// lookup above still selects the visible declaration and its scope.
		typeName := inferVBDeclarationType(parsed, declaration)
		scope := "(global)"
		if declaration.Local {
			scope = "(local)"
		}
		keyword := "Dim"
		if declaration.Kind == "constant" || declaration.Kind == "const" {
			keyword = "Const"
		}
		documentation := ""
		if sourceURI == "" || sourceURI == "." {
			if doc := vbscriptVariableXMLDocBeforeLine(parsed, declaration, variableDeclarations); doc.hasContent() {
				documentation = doc.markdown(vbscript.Signature{}, locale)
				if offset >= declaration.Start && offset <= declaration.End && vbscriptDeclarationMissingTypeMetadata(declaration, &annotations) {
					documentation += "\n\n_" + vbscriptXMLDocumentationTypeNoteForLocale(locale) + "_"
				}
			}
		}
		value := markdownVBScriptSignature(scope+" "+keyword+" "+declaration.Name+" As "+typeName, documentation)
		if sourceURI != "" && sourceURI != "." {
			if note := s.vbscriptDefinedInNote(sourceURI); note != "" {
				value += "\n\n" + note
			}
		}
		return &lsp.Hover{Contents: lsp.MarkupContent{Kind: "markdown", Value: value}}
	}
	return nil
}

func (s *Server) includedVBScriptVariableHover(parsed *core.ParsedDocument, offset int) *lsp.Hover {
	return s.includedVBScriptVariableHoverContext(context.Background(), parsed, offset)
}

func (s *Server) includedVBScriptVariableHoverContext(ctx context.Context, parsed *core.ParsedDocument, offset int) *lsp.Hover {
	return s.includedVBScriptVariableHoverContextMode(ctx, parsed, offset, false)
}

func (s *Server) includedVBScriptVariableHoverContextMode(ctx context.Context, parsed *core.ParsedDocument, offset int, allowIncompletePrefix bool) *lsp.Hover {
	if ctx == nil {
		ctx = context.Background()
	}
	if ctx.Err() != nil {
		return nil
	}
	if parsed == nil {
		return nil
	}
	word := vbscript.WordAt(parsed.Text, offset)
	if word == "" {
		return nil
	}
	candidate, ok := s.vbscriptIncludedGlobalHoverCandidateContextMode(ctx, parsed, offset, word, allowIncompletePrefix)
	if !ok {
		return nil
	}
	sourceURI := candidate.document.URI
	if candidate.document == parsed {
		sourceURI = ""
	}
	return s.vbscriptVariableHoverForDeclarationWithType(candidate.document, candidate.declaration.Start, sourceURI, false, s.settings.Locale, &candidate.declaration, candidate.typeName)
}

func (s *Server) vbscriptVariableHoverAtOffsetContext(ctx context.Context, parsed *core.ParsedDocument, offset int) (*lsp.Hover, bool) {
	if ctx == nil {
		ctx = context.Background()
	}
	if parsed == nil || ctx.Err() != nil {
		return nil, false
	}
	word := vbscript.WordAt(parsed.Text, offset)
	if word == "" {
		return nil, true
	}
	match, found := s.vbscriptVariableHoverMatchAtOffset(ctx, parsed, offset, word)
	if ctx.Err() != nil {
		return nil, false
	}
	if !found {
		return nil, true
	}
	info, complete := s.vbscriptTypeInfoAtOffsetContext(ctx, parsed, match.stateOffset)
	if ctx.Err() != nil {
		return nil, false
	}
	declaration := match.declaration
	if !complete {
		if !declaration.Local {
			return nil, false
		}
		typeName := vbscriptFastDeclarationTypeAtOffset(parsed, declaration, offset)
		return s.vbscriptVariableHoverForDeclarationWithTypeAtOffset(parsed, offset, "", true, s.settings.Locale, &declaration, typeName), true
	}
	// Hovering the declaration token describes the declaration as a whole,
	// including its inferred assignment type. References elsewhere still use
	// the source-ordered prefix below, so later assignments cannot affect an
	// earlier reference.
	typeName := s.vbscriptTypeNameForDeclarationAtOffset(info, parsed, declaration, match.scope, offset)
	key := vbscriptTypeScopeKey(parsed, declaration.Scope, declaration.Name)
	if typeExpr, ok := info.scopedVariableTypes[key]; ok && !typeExpr.isUnknown() {
		typeName = typeExpr.String()
	}
	if strings.TrimSpace(typeName) == "" {
		typeName = "Variant"
	}
	hover := s.vbscriptVariableHoverForDeclarationWithTypeAtOffset(parsed, offset, "", true, s.settings.Locale, &declaration, typeName)
	return hover, true
}

type vbscriptIncludedGlobalHoverCandidate struct {
	document    *core.ParsedDocument
	declaration vbUsageDeclaration
	typeName    string
}

func (s *Server) vbscriptIncludedGlobalHoverCandidate(parsed *core.ParsedDocument, offset int, name string) (vbscriptIncludedGlobalHoverCandidate, bool) {
	return s.vbscriptIncludedGlobalHoverCandidateContext(context.Background(), parsed, offset, name)
}

func (s *Server) vbscriptIncludedGlobalHoverCandidateContext(ctx context.Context, parsed *core.ParsedDocument, offset int, name string) (vbscriptIncludedGlobalHoverCandidate, bool) {
	return s.vbscriptIncludedGlobalHoverCandidateContextMode(ctx, parsed, offset, name, false)
}

func (s *Server) vbscriptIncludedGlobalHoverCandidateContextMode(ctx context.Context, parsed *core.ParsedDocument, offset int, name string, allowIncompletePrefix bool) (vbscriptIncludedGlobalHoverCandidate, bool) {
	if parsed == nil || name == "" {
		return vbscriptIncludedGlobalHoverCandidate{}, false
	}
	if ctx == nil {
		ctx = context.Background()
	}
	units, complete := s.vbscriptIncludeExecutionUnitsThroughOffsetContextResult(ctx, parsed, offset)
	if ctx.Err() != nil || len(units) == 0 || !complete && !allowIncompletePrefix {
		return vbscriptIncludedGlobalHoverCandidate{}, false
	}
	var info vbscriptTypeInfo
	if complete {
		typeInfo, typeComplete := s.vbscriptTypeInfoAtOffsetContext(ctx, parsed, offset)
		if ctx.Err() != nil {
			return vbscriptIncludedGlobalHoverCandidate{}, false
		}
		if !typeComplete && !allowIncompletePrefix {
			return vbscriptIncludedGlobalHoverCandidate{}, false
		}
		info, complete = typeInfo, typeComplete
	}
	lowerName := strings.ToLower(name)
	candidate := vbscriptIncludedGlobalHoverCandidate{}
	currentDeclarations := map[string]vbUsageDeclaration{}
	currentDeclarationDocuments := map[string]*core.ParsedDocument{}
	ownerContracts := vbscriptOwnerGlobalTypeContracts(parsed)
	for _, unit := range units {
		if ctx.Err() != nil {
			return vbscriptIncludedGlobalHoverCandidate{}, false
		}
		if unit.document == nil || unit.start >= unit.end {
			continue
		}
		document := core.NewTextDocument(unit.document.URI, "classic-asp", 0, unit.document.Text)
		declarations := variableInlayDeclarations(unit.document, true, nil)
		globalDeclarations := make([]vbUsageDeclaration, 0, len(declarations))
		for _, declaration := range declarations {
			if declaration.Local || declaration.Scope != "" || declaration.MemberOf != "" || !isVBVariableInlayDeclaration(declaration) {
				continue
			}
			globalDeclarations = append(globalDeclarations, declaration)
		}
		sort.SliceStable(globalDeclarations, func(left, right int) bool {
			if globalDeclarations[left].Start != globalDeclarations[right].Start {
				return globalDeclarations[left].Start < globalDeclarations[right].Start
			}
			return globalDeclarations[left].End < globalDeclarations[right].End
		})
		assignments := vbscriptAssignments(unit.document)
		type vbscriptHoverEvent struct {
			offset      int
			declaration *vbUsageDeclaration
			assignment  *vbAssignment
		}
		events := make([]vbscriptHoverEvent, 0, len(globalDeclarations)+len(assignments))
		for _, declaration := range globalDeclarations {
			if declaration.Start < unit.start || declaration.Start >= unit.end {
				continue
			}
			copy := declaration
			events = append(events, vbscriptHoverEvent{offset: declaration.Start, declaration: &copy})
		}
		for index := range assignments {
			assignment := assignments[index]
			if assignment.Scope != "" {
				continue
			}
			assignmentOffset := document.OffsetAt(assignment.NameRange.Start)
			if assignmentOffset < unit.start || assignmentOffset >= unit.end {
				continue
			}
			copy := assignment
			events = append(events, vbscriptHoverEvent{offset: assignmentOffset, assignment: &copy})
		}
		sort.SliceStable(events, func(left, right int) bool {
			if events[left].offset != events[right].offset {
				return events[left].offset < events[right].offset
			}
			return events[left].declaration != nil
		})
		for _, event := range events {
			if event.declaration != nil {
				declaration := *event.declaration
				if strings.EqualFold(declaration.Name, name) {
					_, exists := currentDeclarations[lowerName]
					if !exists || declaration.Implicit {
						currentDeclarations[lowerName] = declaration
						currentDeclarationDocuments[lowerName] = unit.document
					}
					if candidate.document == nil || candidate.declaration.Implicit {
						candidate = vbscriptIncludedGlobalHoverCandidate{document: unit.document, declaration: declaration}
					}
				}
				continue
			}
			if event.assignment == nil || !strings.EqualFold(event.assignment.Name, name) {
				continue
			}
			declaration, exists := currentDeclarations[lowerName]
			declarationDocument := currentDeclarationDocuments[lowerName]
			if !exists {
				continue
			}
			if candidate.document == nil || candidate.declaration.Implicit {
				candidate = vbscriptIncludedGlobalHoverCandidate{document: declarationDocument, declaration: declaration}
			}
		}
	}
	if candidate.document == nil || !strings.EqualFold(candidate.declaration.Name, name) {
		return vbscriptIncludedGlobalHoverCandidate{}, false
	}
	if contract, ok := ownerContracts[lowerName]; ok && vbscriptGlobalContractActiveAtOffset(parsed, offset, contract) {
		for _, declaration := range variableInlayDeclarations(parsed, true, nil) {
			if declaration.Local || declaration.Scope != "" || declaration.MemberOf != "" || declaration.Implicit || !isVBVariableInlayDeclaration(declaration) || declaration.Start > offset || !strings.EqualFold(declaration.Name, name) || vbscriptDeclarationIsClassMember(parsed, declaration) {
				continue
			}
			candidate = vbscriptIncludedGlobalHoverCandidate{document: parsed, declaration: declaration}
			break
		}
		candidate.typeName = contract.TypeName
	}
	if complete {
		key := vbscriptTypeScopeKey(candidate.document, candidate.declaration.Scope, candidate.declaration.Name)
		if typeExpr, ok := info.scopedVariableTypes[key]; ok && !typeExpr.isUnknown() {
			candidate.typeName = typeExpr.String()
		} else if typeName := info.scopedVariableTypeNames[key]; typeName != "" {
			candidate.typeName = typeName
		} else if candidate.document == parsed && candidate.declaration.Scope == "" {
			candidate.typeName = info.variableTypes[lowerName]
		} else {
			// The prefix state is authoritative. An included declaration with no
			// active type is Variant, even if a later unit inferred one.
			candidate.typeName = ""
		}
	} else {
		// A budget-truncated prefix cannot provide complete type state. Keep the
		// declaration hover useful with its local declaration inference instead
		// of discarding the known source-ordered shadow.
		candidate.typeName = inferVBDeclarationType(candidate.document, candidate.declaration)
	}
	if strings.TrimSpace(candidate.typeName) == "" {
		candidate.typeName = "Variant"
	}
	return candidate, true
}

func vbscriptGlobalContractActiveAtOffset(parsed *core.ParsedDocument, offset int, contract vbscriptGlobalTypeContract) bool {
	if contract.Start < 0 {
		return true
	}
	if parsed == nil || contract.URI == "" || !strings.EqualFold(contract.URI, parsed.URI) {
		return false
	}
	return contract.Start <= offset
}

func (s *Server) resolveCompletionItem(item lsp.CompletionItem) lsp.CompletionItem {
	return s.resolveCompletionItemContext(context.Background(), item)
}

func (s *Server) resolveCompletionItemContext(ctx context.Context, item lsp.CompletionItem) lsp.CompletionItem {
	if ctx == nil {
		ctx = context.Background()
	}
	if ctx.Err() != nil {
		return lsp.CompletionItem{}
	}
	if resolved, ok := s.resolveJavaScriptCompletionItem(ctx, item); ok {
		if ctx.Err() != nil {
			return lsp.CompletionItem{}
		}
		return resolved
	}
	if ctx.Err() != nil {
		return lsp.CompletionItem{}
	}
	if resolved, ok := s.resolveVBScriptAutoIncludeCompletionItem(item); ok {
		if ctx.Err() != nil {
			return lsp.CompletionItem{}
		}
		return resolved
	}
	item = s.enrichVBScriptBuiltInCompletionItem(item)
	var embeddedData embeddedCompletionData
	if remarshal(item.Data, &embeddedData) == nil && embeddedData.URI != "" {
		switch embeddedData.Kind {
		case "html":
			if item.Detail == "" {
				item.Detail = embeddedCompletionDetail("html", embeddedData.Locale)
			}
			if item.Documentation == nil {
				item.Documentation = embeddedCompletionDocumentation("html", embeddedData.Locale)
			}
		case "css":
			if item.Detail == "" {
				item.Detail = embeddedCompletionDetail("css", embeddedData.Locale)
			}
			if item.Documentation == nil {
				item.Documentation = embeddedCompletionDocumentation("css", embeddedData.Locale)
			}
		}
	}
	var importData javascriptAutoImportData
	if remarshal(item.Data, &importData) == nil && importData.Kind == "javascript-auto-import" {
		item.AdditionalTextEdits = []lsp.TextEdit{{
			Range:   importData.Range,
			NewText: `import { ` + importData.Name + ` } from "` + importData.Module + `";` + "\n",
		}}
	}
	var vbData vbscriptCompletionData
	if remarshal(item.Data, &vbData) == nil && vbData.Kind == "vbscript-symbol" && vbData.URI != "" {
		if vbData.Position != nil && (item.Kind == lsp.CompletionItemKindVariable || item.Kind == lsp.CompletionItemKindValue) {
			if document, parsed := s.parsed(vbData.URI); document != nil && parsed != nil {
				offset := document.OffsetAt(*vbData.Position)
				scope := vbscriptScopeAtOffset(parsed, offset)
				localShadow := scope != "" && vbscriptNameBoundInScopeAtOffset(parsed, item.Label, scope, offset)
				for _, declaration := range variableInlayDeclarations(parsed, true, nil) {
					if ctx.Err() != nil {
						return lsp.CompletionItem{}
					}
					if !strings.EqualFold(declaration.Name, item.Label) || declaration.Start > offset ||
						(declaration.Local && !strings.EqualFold(declaration.Scope, scope)) || (!declaration.Local && localShadow) {
						continue
					}
					typeName := vbscriptFastDeclarationTypeAtOffset(parsed, declaration, offset)
					item.Detail = declaration.Name + " As " + typeName
					if hover := s.vbscriptVariableHoverForDeclarationWithType(parsed, declaration.Start, "", true, s.settings.Locale, &declaration, typeName); hover != nil {
						item.Documentation = hover.Contents
					}
					break
				}
			}
		}
		item.Documentation = appendCompletionDocumentation(
			item.Documentation,
			s.vbscriptDefinedInNote(vbData.URI),
		)
	}
	if ctx.Err() != nil {
		return lsp.CompletionItem{}
	}
	resolved := s.resolveVBScriptBuiltinMemberCompletionItem(item)
	if ctx.Err() != nil {
		return lsp.CompletionItem{}
	}
	return resolved
}

type embeddedCompletionData struct {
	Kind   string `json:"kind"`
	URI    string `json:"uri"`
	Locale string `json:"locale"`
}

func appendCompletionDocumentation(documentation any, suffix string) any {
	switch value := documentation.(type) {
	case lsp.MarkupContent:
		if strings.Contains(value.Value, suffix) {
			return value
		}
		value.Value = strings.TrimSpace(value.Value) + "\n\n" + suffix
		return value
	default:
		if documentation != nil {
			text := strings.TrimSpace(toString(documentation))
			if text != "" && !strings.Contains(text, suffix) {
				return lsp.MarkupContent{Kind: "markdown", Value: text + "\n\n" + suffix}
			}
		}
		return lsp.MarkupContent{Kind: "markdown", Value: suffix}
	}
}

func toString(value any) string {
	switch text := value.(type) {
	case string:
		return text
	default:
		bytes, err := json.Marshal(value)
		if err != nil {
			return ""
		}
		return string(bytes)
	}
}

type javascriptAutoImportCandidate struct {
	Name   string
	Module string
}

type javascriptAutoImportData struct {
	Kind   string    `json:"kind"`
	Name   string    `json:"name"`
	Module string    `json:"module"`
	Range  lsp.Range `json:"range"`
}

type vbscriptCompletionData struct {
	Kind     string        `json:"kind"`
	URI      string        `json:"uri"`
	Position *lsp.Position `json:"position,omitempty"`
}

var jsNamedExportPattern = regexp.MustCompile(`(?m)\bexport\s+(?:async\s+)?(?:function|class|const|let|var)\s+([A-Za-z_$][A-Za-z0-9_$]*)|\bexport\s*\{([^}]+)\}`)

func (s *Server) javascriptAutoImportCompletions(ctx context.Context, parsed *core.ParsedDocument, document *core.TextDocument, position lsp.Position, offset int) []lsp.CompletionItem {
	insertRange, ok := javascriptImportInsertRange(parsed, document, position)
	if !ok {
		return nil
	}
	start := offset
	for start > 0 && isCompletionIdentifier(parsed.Text[start-1]) {
		start--
	}
	prefix := parsed.Text[start:offset]
	candidates := s.javascriptAutoImportCandidates(ctx, parsed.URI, prefix)
	items := make([]lsp.CompletionItem, 0, len(candidates))
	for _, candidate := range candidates {
		items = append(items, lsp.CompletionItem{
			Label:  candidate.Name,
			Kind:   lsp.CompletionItemKindFunction,
			Detail: "Auto import from " + candidate.Module,
			Data: javascriptAutoImportData{
				Kind:   "javascript-auto-import",
				Name:   candidate.Name,
				Module: candidate.Module,
				Range:  insertRange,
			},
		})
	}
	return items
}

func (s *Server) javascriptAutoImportCodeAction(ctx context.Context, uri, name string, diagnostic lsp.Diagnostic) (lsp.CodeAction, bool) {
	document, parsed := s.parsed(uri)
	if parsed == nil || document == nil {
		return lsp.CodeAction{}, false
	}
	insertRange, ok := javascriptImportInsertRange(parsed, document, diagnostic.Range.Start)
	if !ok {
		return lsp.CodeAction{}, false
	}
	for _, candidate := range s.javascriptAutoImportCandidates(ctx, uri, name) {
		if candidate.Name != name {
			continue
		}
		return lsp.CodeAction{
			Title:       `Add import from "` + candidate.Module + `"`,
			Kind:        "quickfix",
			Diagnostics: []lsp.Diagnostic{diagnostic},
			Edit: &lsp.WorkspaceEdit{Changes: map[string][]lsp.TextEdit{
				uri: {{
					Range:   insertRange,
					NewText: `import { ` + candidate.Name + ` } from "` + candidate.Module + `";` + "\n",
				}},
			}},
		}, true
	}
	return lsp.CodeAction{}, false
}

func (s *Server) javascriptJQueryCompletionEnabled() bool {
	s.mu.Lock()
	if preparation := s.javascriptPreparation; preparation != nil {
		enabled := preparation.jqueryCompletion
		s.mu.Unlock()
		return enabled
	}
	rootPath := s.rootPath
	typesConfigured := s.settings.JavaScriptCompilerOptionTypes != nil
	types := append([]string(nil), s.settings.JavaScriptCompilerOptionTypes...)
	ignoreProjectConfig := s.settings.JavaScriptIgnoreProjectConfig
	s.mu.Unlock()
	if typesConfigured {
		return containsTypeName(types, "jquery")
	}
	if !ignoreProjectConfig && rootPath != "" {
		projectConfig := s.cachedJavaScriptProjectConfigAt(rootPath, rootPath)
		if projectConfig.Types != nil {
			return containsTypeName(projectConfig.Types, "jquery")
		}
	}
	if rootPath == "" {
		return false
	}
	return javaScriptAmbientTypeExists(rootPath, "jquery")
}

func javaScriptJQueryCompletionFromProject(typesConfigured bool, types []string, rootPath string) bool {
	if typesConfigured {
		return containsTypeName(types, "jquery")
	}
	return rootPath != "" && javaScriptAmbientTypeExists(rootPath, "jquery")
}

func javaScriptAmbientTypeExists(rootPath, packageName string) bool {
	search := filepath.Clean(rootPath)
	for {
		if _, err := os.Stat(filepath.Join(search, "node_modules", "@types", packageName)); err == nil {
			return true
		}
		parent := filepath.Dir(search)
		if parent == search {
			return false
		}
		search = parent
	}
}

func containsTypeName(values []string, expected string) bool {
	for _, value := range values {
		if strings.EqualFold(strings.TrimSpace(value), expected) {
			return true
		}
	}
	return false
}

func (s *Server) javascriptAutoImportCandidates(ctx context.Context, currentURI, prefix string) []javascriptAutoImportCandidate {
	if ctx == nil {
		ctx = context.Background()
	}
	s.mu.Lock()
	preparation := s.javascriptPreparation
	var exports map[string][]string
	if preparation != nil {
		exports = preparation.autoImportExports
	}
	s.mu.Unlock()
	currentPath := fileURIPath(currentURI)
	if currentPath == "" || len(exports) == 0 {
		return nil
	}
	currentPath = filepath.ToSlash(filepath.Clean(currentPath))
	prefix = strings.ToLower(prefix)
	seen := map[string]struct{}{}
	var candidates []javascriptAutoImportCandidate
	for path, names := range exports {
		if ctx.Err() != nil {
			return nil
		}
		path = filepath.ToSlash(filepath.Clean(path))
		if path == currentPath {
			continue
		}
		module := javascriptModuleSpecifier(currentPath, path)
		for _, name := range names {
			if prefix != "" && !strings.HasPrefix(strings.ToLower(name), prefix) {
				continue
			}
			key := name + "\x00" + module
			if _, ok := seen[key]; ok {
				continue
			}
			seen[key] = struct{}{}
			candidates = append(candidates, javascriptAutoImportCandidate{Name: name, Module: module})
		}
	}
	sort.Slice(candidates, func(i, j int) bool {
		if candidates[i].Name == candidates[j].Name {
			return candidates[i].Module < candidates[j].Module
		}
		return candidates[i].Name < candidates[j].Name
	})
	return candidates
}

func cloneJavaScriptAutoImportExports(source map[string][]string) map[string][]string {
	if source == nil {
		return map[string][]string{}
	}
	clone := make(map[string][]string, len(source))
	for path, names := range source {
		clone[path] = append([]string(nil), names...)
	}
	return clone
}

func javascriptImportInsertRange(parsed *core.ParsedDocument, source *core.TextDocument, requestPosition lsp.Position) (lsp.Range, bool) {
	if parsed == nil || source == nil || source.Text != parsed.Text {
		return lsp.Range{}, false
	}
	region := core.RegionAt(parsed, source.OffsetAt(requestPosition))
	if region == nil || region.Language != core.LanguageJavaScript {
		return lsp.Range{}, false
	}
	position := source.PositionAt(region.ContentStart)
	return lsp.Range{Start: position, End: position}, true
}

func isJavaScriptModuleFile(path string) bool {
	if strings.HasSuffix(strings.ToLower(path), ".d.ts") {
		return true
	}
	switch strings.ToLower(filepath.Ext(path)) {
	case ".js", ".mjs", ".cjs", ".jsx", ".ts", ".tsx", ".mts", ".cts":
		return true
	default:
		return false
	}
}

func javascriptModuleSpecifier(currentPath, targetPath string) string {
	rel, err := filepath.Rel(filepath.Dir(currentPath), targetPath)
	if err != nil {
		rel = filepath.Base(targetPath)
	}
	rel = filepath.ToSlash(rel)
	if strings.HasSuffix(strings.ToLower(rel), ".d.ts") {
		rel = rel[:len(rel)-len(".d.ts")]
	} else {
		rel = strings.TrimSuffix(rel, filepath.Ext(rel))
	}
	if !strings.HasPrefix(rel, ".") {
		rel = "./" + rel
	}
	return rel
}

func exportedJavaScriptNames(source string) []string {
	seen := map[string]struct{}{}
	var names []string
	for _, match := range jsNamedExportPattern.FindAllStringSubmatch(source, -1) {
		if match[1] != "" {
			if _, ok := seen[match[1]]; !ok {
				seen[match[1]] = struct{}{}
				names = append(names, match[1])
			}
			continue
		}
		for _, part := range strings.Split(match[2], ",") {
			name := strings.TrimSpace(part)
			if fields := strings.Fields(name); len(fields) > 0 {
				name = fields[len(fields)-1]
			}
			if name == "" {
				continue
			}
			if _, ok := seen[name]; ok {
				continue
			}
			seen[name] = struct{}{}
			names = append(names, name)
		}
	}
	return names
}

func (s *Server) resolveJavaScriptModulePath(rootPath, currentPath, specifier string) (string, bool) {
	var base string
	switch {
	case strings.HasPrefix(specifier, "."):
		base = filepath.Clean(filepath.Join(filepath.Dir(currentPath), filepath.FromSlash(specifier)))
	case strings.HasPrefix(specifier, "/") && rootPath != "":
		base = filepath.Clean(filepath.Join(rootPath, strings.TrimPrefix(filepath.FromSlash(specifier), string(os.PathSeparator))))
	default:
		return s.resolveJavaScriptPackagePath(rootPath, currentPath, specifier)
	}
	if rootPath != "" && !pathWithinRoot(rootPath, base) {
		return "", false
	}
	if resolved, ok := s.resolveJavaScriptPackageDirectory(rootPath, base); ok {
		return resolved, true
	}
	for _, candidate := range javaScriptModulePathCandidates(base) {
		if info, err := os.Stat(candidate); err == nil && !info.IsDir() {
			return candidate, true
		}
	}
	return "", false
}

func javaScriptModulePathCandidates(base string) []string {
	if filepath.Ext(base) != "" {
		return []string{base}
	}
	return []string{
		base,
		base + ".js",
		base + ".mjs",
		base + ".cjs",
		base + ".jsx",
		base + ".ts",
		base + ".tsx",
		base + ".mts",
		base + ".cts",
		base + ".d.ts",
		filepath.Join(base, "index.js"),
		filepath.Join(base, "index.mjs"),
		filepath.Join(base, "index.cjs"),
		filepath.Join(base, "index.jsx"),
		filepath.Join(base, "index.ts"),
		filepath.Join(base, "index.tsx"),
		filepath.Join(base, "index.mts"),
		filepath.Join(base, "index.cts"),
		filepath.Join(base, "index.d.ts"),
	}
}

func (s *Server) resolveJavaScriptPackagePath(rootPath, currentPath, specifier string) (string, bool) {
	if rootPath == "" || specifier == "" || strings.HasPrefix(specifier, "#") {
		return "", false
	}
	search := filepath.Dir(currentPath)
	rootPath = filepath.Clean(rootPath)
	for {
		candidate := filepath.Join(search, "node_modules", filepath.FromSlash(specifier))
		if path, ok := s.resolveJavaScriptPackageDirectory(rootPath, candidate); ok && pathWithinRoot(rootPath, path) {
			return path, true
		}
		if search == rootPath || search == filepath.Dir(search) {
			break
		}
		search = filepath.Dir(search)
	}
	return "", false
}

func (s *Server) resolveJavaScriptPackageDirectory(rootPath, directory string) (string, bool) {
	if info, err := os.Stat(directory); err == nil && !info.IsDir() {
		return directory, true
	}
	boundary := rootPath
	if boundary == "" {
		boundary = filepath.Dir(directory)
	}
	if data, err := s.readSourceFileBytes(withSourceReadBoundaries(context.Background(), boundary), filepath.Join(directory, "package.json"), s.includeReadLimiter); err == nil {
		var packageMetadata struct {
			Types   string `json:"types"`
			Typings string `json:"typings"`
			Module  string `json:"module"`
			Main    string `json:"main"`
		}
		if json.Unmarshal(data, &packageMetadata) == nil {
			for _, entry := range []string{packageMetadata.Types, packageMetadata.Typings, packageMetadata.Module, packageMetadata.Main} {
				if entry == "" {
					continue
				}
				if path, ok := resolveJavaScriptFileOrDirectory(filepath.Join(directory, filepath.FromSlash(entry))); ok {
					return path, true
				}
			}
		}
	}
	return resolveJavaScriptFileOrDirectory(directory)
}

func resolveJavaScriptFileOrDirectory(base string) (string, bool) {
	for _, candidate := range javaScriptModulePathCandidates(base) {
		if info, err := os.Stat(candidate); err == nil && !info.IsDir() {
			return candidate, true
		}
	}
	return "", false
}

func pathWithinRoot(rootPath, targetPath string) bool {
	rel, err := filepath.Rel(filepath.Clean(rootPath), filepath.Clean(targetPath))
	if err != nil {
		return false
	}
	return rel == "." || rel != ".." && !strings.HasPrefix(rel, ".."+string(os.PathSeparator))
}

func missingJavaScriptName(message string) (string, bool) {
	const prefix = "Cannot find name '"
	if !strings.HasPrefix(message, prefix) {
		return "", false
	}
	rest := strings.TrimPrefix(message, prefix)
	end := strings.Index(rest, "'")
	if end <= 0 {
		return "", false
	}
	return rest[:end], true
}

func (s *Server) organizeJavaScriptImportsAction(uri string) (lsp.CodeAction, bool) {
	_, parsed := s.parsed(uri)
	if parsed == nil {
		return lsp.CodeAction{}, false
	}
	source := core.NewTextDocument(parsed.URI, "classic-asp", 0, parsed.Text)
	for _, region := range parsed.Regions {
		if region.Language != core.LanguageJavaScript && region.Language != core.LanguageJScript {
			continue
		}
		edits, ok := organizeImportEdits(source, parsed.Text, region, parsed.Regions)
		if !ok {
			continue
		}
		return lsp.CodeAction{
			Title: "Organize JavaScript imports",
			Kind:  "source.organizeImports",
			Edit: &lsp.WorkspaceEdit{Changes: map[string][]lsp.TextEdit{
				uri: edits,
			}},
		}, true
	}
	return lsp.CodeAction{}, false
}

func organizeImportEdits(source *core.TextDocument, text string, region core.Region, regions []core.Region) ([]lsp.TextEdit, bool) {
	type importLine struct {
		text       string
		lineEnding string
		start      int
		end        int
	}
	var blocks [][]importLine
	var block []importLine
	flushBlock := func() {
		if len(block) == 0 {
			return
		}
		blocks = append(blocks, block)
		block = nil
	}
	var aspRegions []core.Region
	for _, candidate := range regions {
		switch candidate.Kind {
		case core.RegionASPBlock, core.RegionASPExpression, core.RegionASPDirective:
			if candidate.Start < region.ContentEnd && candidate.End > region.ContentStart {
				aspRegions = append(aspRegions, candidate)
			}
		}
	}
	sort.Slice(aspRegions, func(i, j int) bool {
		return aspRegions[i].Start < aspRegions[j].Start
	})
	aspRegionIndex := 0
	for lineStart := region.ContentStart; lineStart < region.ContentEnd; {
		lineEnd := lineStart
		for lineEnd < region.ContentEnd && text[lineEnd] != '\n' && text[lineEnd] != '\r' {
			lineEnd++
		}
		next := lineEnd
		if next < region.ContentEnd && text[next] == '\r' {
			next++
			if next < region.ContentEnd && text[next] == '\n' {
				next++
			}
		} else if next < region.ContentEnd && text[next] == '\n' {
			next++
		}
		lineText := text[lineStart:lineEnd]
		for aspRegionIndex < len(aspRegions) && aspRegions[aspRegionIndex].End <= lineStart {
			aspRegionIndex++
		}
		lineOverlapsASP := aspRegionIndex < len(aspRegions) && aspRegions[aspRegionIndex].Start < next
		if strings.HasPrefix(strings.TrimSpace(lineText), "import ") &&
			!lineOverlapsASP {
			block = append(block, importLine{
				text:       lineText,
				lineEnding: text[lineEnd:next],
				start:      lineStart,
				end:        next,
			})
		} else {
			flushBlock()
		}
		lineStart = next
	}
	flushBlock()
	if len(blocks) == 0 {
		return nil, false
	}

	edits := make([]lsp.TextEdit, 0, len(blocks))
	for _, imports := range blocks {
		sorted := append([]importLine(nil), imports...)
		sort.SliceStable(sorted, func(i, j int) bool {
			return strings.ToLower(sorted[i].text) < strings.ToLower(sorted[j].text)
		})
		var replacement strings.Builder
		for i, item := range sorted {
			replacement.WriteString(item.text)
			replacement.WriteString(imports[i].lineEnding)
		}
		edits = append(edits, lsp.TextEdit{
			Range:   source.Range(imports[0].start, imports[len(imports)-1].end),
			NewText: replacement.String(),
		})
	}
	return edits, true
}
