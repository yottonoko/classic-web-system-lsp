package lspserver

import (
	"context"
	"strings"

	"github.com/yottonoko/classic-web-system-lsp/internal/core"
	"github.com/yottonoko/classic-web-system-lsp/internal/lsp"
	"github.com/yottonoko/classic-web-system-lsp/internal/vbscript"
)

type vbscriptMemberDeclaration struct {
	document    *core.ParsedDocument
	declaration vbUsageDeclaration
}

type vbscriptMemberTarget struct {
	name         string
	typeNames    []string
	declarations []vbscriptMemberDeclaration
	unsafe       bool
}

//lint:ignore U1000 retained for callers that use the legacy context-free helper.
func (s *Server) vbscriptMemberTargetAt(parsed *core.ParsedDocument, offset int) (vbscriptMemberTarget, bool) {
	return s.vbscriptMemberTargetAtContext(context.Background(), parsed, offset)
}

func (s *Server) vbscriptMemberTargetAtContext(ctx context.Context, parsed *core.ParsedDocument, offset int) (vbscriptMemberTarget, bool) {
	if ctx == nil {
		ctx = context.Background()
	}
	if parsed == nil || ctx.Err() != nil {
		return vbscriptMemberTarget{}, false
	}
	start := offset
	for start > 0 && isVBIdentifierByte(parsed.Text[start-1]) {
		start--
	}
	end := offset
	for end < len(parsed.Text) && isVBIdentifierByte(parsed.Text[end]) {
		end++
	}
	owner, hasOwner := vbCompletionMemberOwnerBefore(parsed.Text, start)
	if ctx.Err() != nil {
		return vbscriptMemberTarget{}, false
	}
	name := ""
	if hasOwner && start < end {
		name = parsed.Text[start:end]
	}
	if !hasOwner {
		if start < end {
			if declaration, ok := vbscriptClassMemberDeclarationAtOffset(parsed, parsed.Text[start:end], offset); ok {
				name = declaration.Name
				owner = declaration.MemberOf
			}
		}
		position := core.SourceDocument(parsed).PositionAt(offset)
		if name == "" {
			for _, declaration := range vbNamingDeclarationsShared(parsed) {
				if ctx.Err() != nil {
					return vbscriptMemberTarget{}, false
				}
				if declaration.MemberOf != "" && lspPositionInRange(position, declaration.Range) {
					name = declaration.Name
					owner = declaration.MemberOf
					break
				}
			}
		}
		if name == "" {
			return vbscriptMemberTarget{}, false
		}
	}
	typeNames := []string{owner}
	if hasOwner {
		typeNames = s.vbscriptMemberOwnerTypesContext(ctx, parsed, owner, offset)
	}
	if ctx.Err() != nil {
		return vbscriptMemberTarget{}, false
	}
	target := vbscriptMemberTarget{name: name, typeNames: typeNames}
	if hasOwner && len(typeNames) > 1 {
		info, complete := s.vbscriptTypeInfoAtOffsetContext(ctx, parsed, offset)
		if !complete || ctx.Err() != nil {
			return vbscriptMemberTarget{}, false
		}
		if member, checked := vbscriptUnionMember(strings.Join(typeNames, " | "), name, info); checked && member == nil {
			target.unsafe = true
		}
	}
	if target.unsafe {
		return target, true
	}
	documents, complete := s.vbscriptDocumentsThroughExecutionOffsetContext(ctx, parsed, offset)
	if ctx.Err() != nil || !complete {
		return vbscriptMemberTarget{}, false
	}
	for _, document := range documents {
		if ctx.Err() != nil {
			return vbscriptMemberTarget{}, false
		}
		for _, declaration := range vbNamingDeclarationsShared(document) {
			if ctx.Err() != nil {
				return vbscriptMemberTarget{}, false
			}
			if document == parsed && declaration.Start > offset {
				continue
			}
			if declaration.MemberOf == "" || !strings.EqualFold(declaration.Name, name) || !containsFold(target.typeNames, declaration.MemberOf) {
				continue
			}
			target.declarations = append(target.declarations, vbscriptMemberDeclaration{document: document, declaration: declaration})
		}
	}
	return target, true
}

func (s *Server) vbscriptMemberOwnerTypesContext(ctx context.Context, parsed *core.ParsedDocument, owner string, offset int) []string {
	if ctx == nil {
		ctx = context.Background()
	}
	if parsed == nil || ctx.Err() != nil {
		return nil
	}
	if strings.EqualFold(owner, "Me") {
		position := core.SourceDocument(parsed).PositionAt(offset)
		if classOwner := vbClassMemberLineOwners(parsed)[position.Line]; classOwner != "" {
			return []string{classOwner}
		}
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
	// An empty authoritative state is meaningful: dynamic assignment has
	// invalidated earlier inference, so legacy assignment/type scans must not
	// resurrect a type from a later source position.
	return dedupeFold(vbscriptConcreteTypeNames(typeName))
}

func containsFold(values []string, value string) bool {
	for _, candidate := range values {
		if strings.EqualFold(candidate, value) {
			return true
		}
	}
	return false
}

func dedupeFold(values []string) []string {
	result := make([]string, 0, len(values))
	for _, value := range values {
		if value != "" && !containsFold(result, value) {
			result = append(result, value)
		}
	}
	return result
}

//lint:ignore U1000 retained for callers that use the legacy context-free helper.
func (s *Server) vbscriptMemberDefinition(parsed *core.ParsedDocument, position lsp.Position) ([]lsp.Location, bool) {
	return s.vbscriptMemberDefinitionContext(context.Background(), parsed, position)
}

func (s *Server) vbscriptMemberDefinitionContext(ctx context.Context, parsed *core.ParsedDocument, position lsp.Position) ([]lsp.Location, bool) {
	if ctx == nil {
		ctx = context.Background()
	}
	if parsed == nil || ctx.Err() != nil {
		return nil, false
	}
	doc := core.SourceDocument(parsed)
	target, ok := s.vbscriptMemberTargetAtContext(ctx, parsed, doc.OffsetAt(position))
	if !ok {
		return nil, false
	}
	locations := make([]lsp.Location, 0, len(target.declarations))
	for _, declaration := range target.declarations {
		if ctx.Err() != nil {
			return nil, false
		}
		locations = append(locations, lsp.Location{URI: declaration.document.URI, Range: declaration.declaration.Range})
	}
	return locations, true
}

func (s *Server) vbscriptMemberHover(parsed *core.ParsedDocument, offset int) (*lsp.Hover, bool) {
	return s.vbscriptMemberHoverContext(context.Background(), parsed, offset)
}

func (s *Server) vbscriptMemberHoverContext(ctx context.Context, parsed *core.ParsedDocument, offset int) (*lsp.Hover, bool) {
	if ctx == nil {
		ctx = context.Background()
	}
	if ctx.Err() != nil {
		return nil, false
	}
	target, ok := s.vbscriptMemberTargetAtContext(ctx, parsed, offset)
	if !ok {
		return nil, false
	}
	if target.unsafe {
		return nil, true
	}
	if len(target.declarations) == 0 {
		return nil, true
	}
	if ctx.Err() != nil {
		return nil, false
	}
	match := target.declarations[0]
	declaration := match.declaration
	if declaration.Kind == "method" {
		for _, signature := range vbscript.Signatures(match.document) {
			if ctx.Err() != nil {
				return nil, false
			}
			if signature.NameRange == declaration.Range {
				signature = annotatedVBScriptSignature(match.document, signature)
				return vbscriptSignatureHover(signature, vbscriptXMLDocForSignature(match.document, signature), false, s.settings.Locale), true
			}
		}
	}
	if declaration.Kind == "property" {
		if ctx.Err() != nil {
			return nil, false
		}
		return vbscriptPropertySignatureHoverWithLocale(match.document, declaration.Start, s.settings.Locale), true
	}
	label := "(member) " + declaration.MemberOf + "." + declaration.Name
	analysis := graphAnalysisTypes(match.document)
	if typeName := graphMemberDeclarationType(match.document, declaration, &analysis); typeName != "" && !graphShouldHideTypeName(typeName) {
		label += " As " + typeName
	}
	return &lsp.Hover{Contents: lsp.MarkupContent{Kind: "markdown", Value: markdownVBScriptSignature(label, "")}}, true
}

func vbscriptSignatureForMemberDeclaration(parsed *core.ParsedDocument, declaration vbUsageDeclaration) (vbscript.Signature, bool) {
	if parsed == nil || declaration.Name == "" {
		return vbscript.Signature{}, false
	}
	for _, signature := range graphSignatures(parsed) {
		if !strings.EqualFold(signature.Name, declaration.Name) || !lspRangeEqual(signature.NameRange, declaration.Range) {
			continue
		}
		return signature, true
	}
	return vbscript.Signature{}, false
}

func (s *Server) vbscriptMemberReferences(ctx context.Context, parsed *core.ParsedDocument, position lsp.Position, includeDeclaration bool) ([]lsp.Location, bool) {
	return s.vbscriptMemberReferenceLocations(ctx, parsed, position, includeDeclaration, true)
}

func (s *Server) vbscriptMemberReferenceLocations(ctx context.Context, parsed *core.ParsedDocument, position lsp.Position, includeDeclaration bool, workspaceScope bool) ([]lsp.Location, bool) {
	if ctx == nil {
		ctx = context.Background()
	}
	if parsed == nil || ctx.Err() != nil {
		return nil, false
	}
	doc := core.SourceDocument(parsed)
	target, ok := s.vbscriptMemberTargetAtContext(ctx, parsed, doc.OffsetAt(position))
	if !ok {
		return nil, false
	}
	if target.unsafe {
		return nil, true
	}
	included, complete := s.includedDocumentsContextResult(ctx, parsed)
	if !complete || ctx.Err() != nil {
		return nil, false
	}
	documents := append([]*core.ParsedDocument{parsed}, included...)
	if workspaceScope {
		referenceDocuments, complete := s.workspaceReferenceDocumentsContextResult(ctx, parsed)
		if !complete || ctx.Err() != nil {
			return nil, false
		}
		documents = append(documents, referenceDocuments...)
	}
	documents = dedupeParsedDocumentsByFileIdentity(documents)
	declarations := map[string]map[lsp.Range]struct{}{}
	for _, match := range target.declarations {
		if declarations[match.document.URI] == nil {
			declarations[match.document.URI] = map[lsp.Range]struct{}{}
		}
		declarations[match.document.URI][match.declaration.Range] = struct{}{}
	}
	locations := make([]lsp.Location, 0)
	for _, document := range documents {
		if document == nil || ctx.Err() != nil {
			if ctx.Err() != nil {
				return nil, false
			}
			continue
		}
		source := core.SourceDocument(document)
		for _, posting := range vbscript.BuildReferenceShard(document).PostingsFor(target.name) {
			if ctx.Err() != nil {
				return nil, false
			}
			_, isDeclaration := declarations[document.URI][posting.Range]
			matches := isDeclaration
			if posting.Owner != "" {
				offset := source.OffsetAt(posting.Range.Start)
				matches = intersectsFold(target.typeNames, s.vbscriptMemberOwnerTypesContext(ctx, document, posting.Owner, offset))
			} else if posting.ClassOwner != "" {
				matches = containsFold(target.typeNames, posting.ClassOwner) &&
					!vbLocalDeclarationShadowsNameAt(document, target.name, posting.Range.Start)
			}
			if !matches || isDeclaration && !includeDeclaration {
				continue
			}
			locations = append(locations, lsp.Location{URI: document.URI, Range: posting.Range})
		}
	}
	if ctx.Err() != nil {
		return nil, false
	}
	return locations, true
}

//lint:ignore U1000 retained for callers that use the legacy context-free helper.
func (s *Server) vbscriptMemberSignatureHelp(parsed *core.ParsedDocument, position lsp.Position) (*lsp.SignatureHelp, bool) {
	return s.vbscriptMemberSignatureHelpContext(context.Background(), parsed, position)
}

func (s *Server) vbscriptMemberSignatureHelpContext(ctx context.Context, parsed *core.ParsedDocument, position lsp.Position) (*lsp.SignatureHelp, bool) {
	if ctx == nil || ctx.Err() != nil {
		return nil, false
	}
	if parsed == nil {
		return nil, false
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
		return nil, false
	}
	target, ok := s.vbscriptMemberTargetAtContext(ctx, parsed, nameStart)
	if !ok {
		return nil, false
	}
	if target.unsafe {
		return nil, true
	}
	for _, match := range target.declarations {
		if ctx.Err() != nil {
			return nil, false
		}
		if match.declaration.Kind != "method" {
			continue
		}
		for _, signature := range vbscript.Signatures(match.document) {
			if ctx.Err() != nil {
				return nil, false
			}
			if signature.NameRange != match.declaration.Range {
				continue
			}
			parameters := make([]lsp.ParameterInformation, 0, len(signature.Parameters))
			for _, parameter := range signature.Parameters {
				if ctx.Err() != nil {
					return nil, false
				}
				parameters = append(parameters, lsp.ParameterInformation{Label: parameter.Name})
			}
			if ctx.Err() != nil {
				return nil, false
			}
			information := lsp.SignatureInformation{Label: signature.Label, Parameters: parameters}
			enrichVBScriptSignatureInformation(&information, signature, s.settings.Locale, match.document)
			return &lsp.SignatureHelp{
				Signatures:      []lsp.SignatureInformation{information},
				ActiveParameter: vbActiveParameter(parsed.Text, argsStart, offset),
			}, true
		}
	}
	if !vbscriptQualifiedMemberAtOffset(parsed.Text, nameStart) {
		nameEnd := nameStart
		for nameEnd < len(parsed.Text) && isVBIdentifierByte(parsed.Text[nameEnd]) {
			nameEnd++
		}
		name := parsed.Text[nameStart:nameEnd]
		if vbLocalDeclarationShadowsNameAt(parsed, name, core.SourceDocument(parsed).PositionAt(nameStart)) {
			return nil, true
		}
		if declaration, ok := vbscriptClassMemberDeclarationAtOffset(parsed, name, nameStart); ok && declaration.Kind == "method" {
			if signature, ok := vbscriptSignatureForMemberDeclaration(parsed, declaration); ok {
				parameters := make([]lsp.ParameterInformation, 0, len(signature.Parameters))
				for _, parameter := range signature.Parameters {
					parameters = append(parameters, lsp.ParameterInformation{Label: parameter.Name})
				}
				information := lsp.SignatureInformation{Label: signature.Label, Parameters: parameters}
				enrichVBScriptSignatureInformation(&information, signature, s.settings.Locale, parsed)
				return &lsp.SignatureHelp{
					Signatures:      []lsp.SignatureInformation{information},
					ActiveParameter: vbActiveParameter(parsed.Text, argsStart, offset),
				}, true
			}
		}
	}
	return nil, true
}

func vbscriptNoParenMemberCallAt(text string, offset int) (int, int) {
	if offset < 0 || offset > len(text) {
		return -1, -1
	}
	lineStart := offset
	for lineStart > 0 && text[lineStart-1] != '\n' && text[lineStart-1] != '\r' {
		lineStart--
	}
	statementStart := vbscriptStatementStart(text, lineStart, offset)
	cursor := statementStart
	for cursor < offset && isVBWhitespace(text[cursor]) {
		cursor++
	}
	if end := readVBIdentifier(text, cursor); end > cursor && strings.EqualFold(text[cursor:end], "Call") {
		cursor = end
		for cursor < offset && isVBWhitespace(text[cursor]) {
			cursor++
		}
	}
	ownerEnd := readVBIdentifier(text, cursor)
	if ownerEnd == cursor {
		return -1, -1
	}
	cursor = ownerEnd
	for cursor < offset && isVBWhitespace(text[cursor]) {
		cursor++
	}
	if cursor >= offset || text[cursor] != '.' {
		return -1, -1
	}
	cursor++
	for cursor < offset && isVBWhitespace(text[cursor]) {
		cursor++
	}
	memberStart := cursor
	memberEnd := readVBIdentifier(text, memberStart)
	if memberEnd == memberStart {
		return -1, -1
	}
	argsStart := memberEnd
	for argsStart < offset && isVBWhitespace(text[argsStart]) {
		argsStart++
	}
	if argsStart >= offset || text[argsStart] == '(' || !vbscriptNoParenArgumentCursorAtTopLevel(text, argsStart, offset) {
		return -1, -1
	}
	return memberStart, argsStart
}

func intersectsFold(left, right []string) bool {
	for _, value := range left {
		if containsFold(right, value) {
			return true
		}
	}
	return false
}
