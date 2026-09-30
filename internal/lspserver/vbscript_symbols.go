package lspserver

import (
	"context"
	"path/filepath"
	"regexp"
	"sort"
	"strconv"
	"strings"

	htmlservice "github.com/yottonoko/vscode-html-languageservice-go"

	"github.com/yottonoko/classic-web-system-lsp/internal/core"
	"github.com/yottonoko/classic-web-system-lsp/internal/lsp"
	"github.com/yottonoko/classic-web-system-lsp/internal/vbscript"
	workspacepkg "github.com/yottonoko/classic-web-system-lsp/internal/workspace"
)

type serverObjectAttribute struct {
	Name  string
	Value string
	Range lsp.Range
	Start int
	End   int
}

type serverObjectSymbol struct {
	Declaration vbUsageDeclaration
	Attributes  map[string]serverObjectAttribute
	TagRange    lsp.Range
}

const (
	serverObjectSymbolsAnalysisKey    = "lspserver.server-object-symbols.v1"
	includedServerObjectIndexKey      = "lspserver.included-server-object-index.v1"
	implicitVBDeclarationsAnalysisKey = "lspserver.implicit-vb-declarations.v3"
	vbProcedureScopesAnalysisKey      = "lspserver.vb-procedure-scopes.v4"
	vbLocalShadowNamesAnalysisKey     = "lspserver.vb-local-shadow-names.v1"
)

func vbTextDocument(parsed *core.ParsedDocument) *core.TextDocument {
	return core.SourceDocument(parsed)
}

// includedServerObjectIndex contains only included server-side OBJECT,
// procedure, and global Dim/Const names, plus the transitive include facts
// needed to validate the cached answer. A budget-truncated index is usable as
// a conservative prefix for source-order shadow checks but is never cached.
// It never retains included ParsedDocument graphs.
type includedServerObjectIndex struct {
	Objects      map[string]int
	Globals      map[string]int
	Procedures   map[string]int
	First        map[string]includedNameIndexEntry
	Dependencies []includedServerObjectDependency
}

type includedNameIndexEntry struct {
	Kind             string
	VisibleAt        int
	Start            int
	URI              string
	TypeName         string
	Declaration      vbUsageDeclaration
	Signature        vbscript.Signature
	ObjectAttributes []includedNameIndexAttribute
}

type includedNameIndexAttribute struct {
	Name  string
	Value string
}

type includedNameIndexEvent struct {
	Name    string
	Kind    string
	Start   int
	End     int
	Include core.Include
	Entry   includedNameIndexEntry
}

type includedServerObjectDependency struct {
	ParentURI    string
	Path         string
	Mode         string
	Resolved     bool
	ResolvedPath string
	Exists       bool
	ContentHash  string
}

type includedServerObjectDependencyValidation struct {
	Details     includeTargetDetails
	Resolved    bool
	ContentHash string
}

func includedServerObjectDependencyKey(parentURI, includePath, mode string) string {
	return workspacepkg.FileIdentityKeyFromURI(parentURI) + "\x00" + mode + "\x00" + includePath
}

// EstimateBytes reports the storage retained by this runtime index. Core's
// ParsedDocument estimator detects this interface without importing the LSP
// package, so memory pressure includes the real index size.
func (index *includedServerObjectIndex) EstimateBytes() int64 {
	if index == nil {
		return 0
	}
	bytes := int64(128 + (len(index.Objects)+len(index.Globals)+len(index.Procedures)+len(index.First))*64 + cap(index.Dependencies)*128)
	for name := range index.Objects {
		bytes += int64(len(name))*2 + 16
	}
	for name := range index.Globals {
		bytes += int64(len(name))*2 + 16
	}
	for name := range index.Procedures {
		bytes += int64(len(name))*2 + 16
	}
	for name, entry := range index.First {
		bytes += int64(len(name)+len(entry.Kind)+len(entry.URI))*2 + 96
		bytes += int64(len(entry.TypeName)+len(entry.Declaration.Name)+len(entry.Declaration.Kind)+len(entry.Declaration.TypeName))*2 + 64
		bytes += int64(len(entry.Signature.Name)+len(entry.Signature.Kind)+len(entry.Signature.Label))*2 + 64
		bytes += int64(cap(entry.Signature.Parameters)) * 64
		for _, parameter := range entry.Signature.Parameters {
			bytes += int64(len(parameter.Name)+len(parameter.Mode))*2 + 16
		}
		for _, attribute := range entry.ObjectAttributes {
			bytes += int64(len(attribute.Name)+len(attribute.Value))*2 + 32
		}
	}
	for _, dependency := range index.Dependencies {
		bytes += int64(len(dependency.ParentURI)+len(dependency.Path)+len(dependency.Mode)+len(dependency.ResolvedPath)+len(dependency.ContentHash))*2 + 80
	}
	return bytes
}

// SkipPreviousRuntimeInheritance marks the index as revision-specific. Its
// dependency fingerprint is revalidated on the owning immutable revision, so
// retaining it through the next revision only prolongs the old include graph.
func (*includedServerObjectIndex) SkipPreviousRuntimeInheritance() {}

type vbProcedureScope struct {
	Name        string
	Owner       string
	Accessor    string
	NameStart   int
	NameEnd     int
	StartLine   int
	EndLine     int
	StartOffset int
	EndOffset   int
}

func (scope vbProcedureScope) Key() string {
	key := vbProcedureScopeKey(scope.Owner, scope.Name)
	if scope.Accessor != "" {
		key += "#" + strings.ToLower(strings.TrimSpace(scope.Accessor))
	}
	return key
}

func vbProcedureScopeKey(owner, name string) string {
	owner = strings.ToLower(strings.TrimSpace(owner))
	name = strings.ToLower(strings.TrimSpace(name))
	if owner == "" {
		return name
	}
	return owner + "." + name
}

func vbProcedureScopeName(scope string) string {
	value := strings.TrimSpace(scope)
	if index := strings.IndexByte(value, '#'); index >= 0 {
		value = value[:index]
	}
	if index := strings.LastIndexByte(value, '.'); index >= 0 {
		return strings.TrimSpace(value[index+1:])
	}
	return value
}

func vbProcedureScopeOwner(scope string) string {
	value := strings.TrimSpace(scope)
	if index := strings.IndexByte(value, '#'); index >= 0 {
		value = value[:index]
	}
	if index := strings.LastIndexByte(value, '.'); index > 0 {
		return strings.TrimSpace(value[:index])
	}
	return ""
}

func vbProcedureScopes(parsed *core.ParsedDocument) []vbProcedureScope {
	if parsed == nil {
		return nil
	}
	if value, ok := parsed.LoadRuntimeAnalysis(vbProcedureScopesAnalysisKey); ok {
		if cached, ok := value.([]vbProcedureScope); ok {
			return cached
		}
	}
	var cached []vbProcedureScope
	if parsed.LoadAnalysis(vbProcedureScopesAnalysisKey, &cached) {
		parsed.StoreRuntimeAnalysis(vbProcedureScopesAnalysisKey, cached)
		return cached
	}
	scopes := vbProcedureScopesFromCST(parsed)
	if len(scopes) == 0 {
		doc := core.SourceDocument(parsed)
		scopes = make([]vbProcedureScope, 0)
		for _, procedure := range graphVBProcedureRanges(parsed) {
			scopes = append(scopes, vbProcedureScope{
				Name:        strings.ToLower(procedure.name),
				Owner:       strings.ToLower(procedure.owner),
				Accessor:    procedure.accessor,
				NameStart:   doc.OffsetAt(procedure.nameRange.Start),
				NameEnd:     doc.OffsetAt(procedure.nameRange.End),
				StartLine:   procedure.startLine,
				EndLine:     procedure.endLine,
				StartOffset: doc.OffsetAt(procedure.sourceRange.Start),
				EndOffset:   doc.OffsetAt(procedure.sourceRange.End),
			})
		}
	}
	scopes = mergeVBSourceProcedureScopes(parsed, scopes)
	owners := vbClassMemberLineOwners(parsed)
	for index := range scopes {
		if scopes[index].Owner == "" {
			scopes[index].Owner = strings.ToLower(owners[scopes[index].StartLine])
		}
	}
	sort.SliceStable(scopes, func(i, j int) bool {
		if scopes[i].StartLine != scopes[j].StartLine {
			return scopes[i].StartLine < scopes[j].StartLine
		}
		if scopes[i].StartOffset != scopes[j].StartOffset {
			return scopes[i].StartOffset < scopes[j].StartOffset
		}
		return scopes[i].EndLine < scopes[j].EndLine
	})
	parsed.StoreRuntimeAnalysis(vbProcedureScopesAnalysisKey, scopes)
	parsed.StoreAnalysis(vbProcedureScopesAnalysisKey, scopes)
	return scopes
}

// mergeVBSourceProcedureScopes supplements the CST scopes with declarations
// recovered from explicit-continuation boundaries. A malformed continued
// declaration can make the lossless CST retain the following declaration as
// part of its open node; the source-ordered header scan still identifies the
// later procedure and gives its body a concrete, smaller scope.
func mergeVBSourceProcedureScopes(parsed *core.ParsedDocument, scopes []vbProcedureScope) []vbProcedureScope {
	if parsed == nil {
		return scopes
	}
	sourceScopes := vbSourceProcedureScopes(parsed)
	if len(sourceScopes) == 0 {
		return scopes
	}
	byNameStart := make(map[int]int, len(scopes)+len(sourceScopes))
	for index, scope := range scopes {
		if scope.NameStart >= 0 {
			byNameStart[scope.NameStart] = index
		}
	}
	for _, scope := range sourceScopes {
		if index, exists := byNameStart[scope.Scope.NameStart]; exists {
			// CST nodes normally provide the most accurate nesting, but an
			// error-tolerant node can end at the declaration header. A complete
			// source header supplies the missing body range without widening an
			// incomplete outer declaration.
			if scope.Complete && scope.Scope.EndOffset > scopes[index].EndOffset {
				scopes[index].EndOffset = scope.Scope.EndOffset
				doc := core.SourceDocument(parsed)
				scopes[index].EndLine = doc.PositionAt(scope.Scope.EndOffset).Line
			}
			continue
		}
		scopes = append(scopes, scope.Scope)
		byNameStart[scope.Scope.NameStart] = len(scopes) - 1
	}
	// A CST node for an incomplete header may still contain the recovered
	// declaration. Stop that containing scope immediately before the next
	// source declaration so unrelated statements cannot inherit its locals.
	for index := range scopes {
		for _, source := range sourceScopes {
			sourceScope := source.Scope
			if sourceScope.NameStart == scopes[index].NameStart ||
				sourceScope.StartOffset <= scopes[index].StartOffset || sourceScope.StartOffset > scopes[index].EndOffset {
				continue
			}
			end := sourceScope.StartOffset - 1
			if end < scopes[index].EndOffset {
				scopes[index].EndOffset = end
				if scopes[index].EndOffset < scopes[index].StartOffset {
					scopes[index].EndOffset = scopes[index].StartOffset
				}
				doc := core.SourceDocument(parsed)
				scopes[index].EndLine = doc.PositionAt(scopes[index].EndOffset).Line
			}
		}
	}
	return scopes
}

type vbSourceProcedureScope struct {
	Scope    vbProcedureScope
	Kind     string
	Complete bool
}

func vbSourceProcedureScopes(parsed *core.ParsedDocument) []vbSourceProcedureScope {
	if parsed == nil {
		return nil
	}
	doc := core.SourceDocument(parsed)
	var scopes []vbSourceProcedureScope
	for _, region := range parsed.Regions {
		if region.Language != core.LanguageVBScript {
			continue
		}
		active := -1
		for lineStart := region.ContentStart; lineStart < region.ContentEnd; {
			lineEnd := vbPhysicalLineEnd(parsed.Text, lineStart, region.ContentEnd)
			header, logicalEnd, ok := vbscript.ProcedureHeaderAtLogical(parsed.Text, lineStart)
			if ok && logicalEnd <= region.ContentEnd {
				lineEnd = logicalEnd
			} else {
				ok = false
			}
			if ok && header.NameStart >= region.ContentStart && header.NameEnd <= region.ContentEnd &&
				header.NameEnd > header.NameStart {
				if active >= 0 && scopes[active].Scope.EndOffset >= header.KeywordStart {
					scopes[active].Scope.EndOffset = header.KeywordStart - 1
					scopes[active].Scope.EndLine = doc.PositionAt(header.KeywordStart - 1).Line
				}
				scopes = append(scopes, vbSourceProcedureScope{
					Scope: vbProcedureScope{
						Name:        strings.ToLower(parsed.Text[header.NameStart:header.NameEnd]),
						Accessor:    strings.ToLower(header.Accessor),
						NameStart:   header.NameStart,
						NameEnd:     header.NameEnd,
						StartLine:   doc.PositionAt(header.KeywordStart).Line,
						EndLine:     doc.PositionAt(region.ContentEnd).Line,
						StartOffset: header.KeywordStart,
						EndOffset:   region.ContentEnd,
					},
					Kind:     header.Kind,
					Complete: !header.HasParameterList || header.ParameterListComplete,
				})
				active = len(scopes) - 1
			}
			if active >= 0 && lineStart > scopes[active].Scope.StartOffset &&
				vbProcedureTerminatorLine(parsed.Text[lineStart:lineEnd], scopes[active].Kind) {
				scopes[active].Scope.EndOffset = lineEnd
				scopes[active].Scope.EndLine = doc.PositionAt(lineEnd).Line
				active = -1
			}
			if lineEnd >= region.ContentEnd {
				break
			}
			lineStart = vbNextLineStart(parsed.Text, lineEnd, region.ContentEnd)
		}
	}
	return scopes
}

func vbProcedureTerminatorLine(line, kind string) bool {
	words := make([]string, 0, 2)
	for _, token := range vbscript.Tokenize(line) {
		if token.Kind == "whitespace" || token.Kind == "comment" {
			continue
		}
		words = append(words, token.Text)
		if len(words) == 2 {
			break
		}
	}
	return len(words) == 2 && strings.EqualFold(words[0], "end") && strings.EqualFold(words[1], kind)
}

func vbProcedureScopesFromCST(parsed *core.ParsedDocument) []vbProcedureScope {
	if parsed == nil {
		return nil
	}
	doc := core.SourceDocument(parsed)
	scopes := make([]vbProcedureScope, 0)
	var collect func(*vbscript.CSTNode)
	collect = func(node *vbscript.CSTNode) {
		if node == nil {
			return
		}
		if (node.Kind == "Procedure" || node.Kind == "Property") && node.NameToken != nil {
			start := max(0, min(node.Start, len(parsed.Text)))
			end := max(start, min(node.End, len(parsed.Text)))
			accessor := ""
			if node.Kind == "Property" && node.Statement != nil {
				switch node.Statement.Kind {
				case vbscript.CSTStatementPropertyGet:
					accessor = "get"
				case vbscript.CSTStatementPropertyLet:
					accessor = "let"
				case vbscript.CSTStatementPropertySet:
					accessor = "set"
				}
			}
			scopes = append(scopes, vbProcedureScope{
				Name:        strings.ToLower(node.NameToken.Text),
				Accessor:    accessor,
				NameStart:   node.NameToken.Start,
				NameEnd:     node.NameToken.End,
				StartLine:   doc.PositionAt(start).Line,
				EndLine:     doc.PositionAt(end).Line,
				StartOffset: start,
				EndOffset:   end,
			})
		}
		for _, child := range node.Children {
			collect(child)
		}
	}
	collect(vbscript.ParseDocumentCST(parsed))
	return scopes
}

func vbProcedureScopeAtLine(scopes []vbProcedureScope, line int) string {
	index := sort.Search(len(scopes), func(index int) bool {
		return scopes[index].StartLine > line
	}) - 1
	if index < 0 || line > scopes[index].EndLine {
		return ""
	}
	return scopes[index].Key()
}

func vbProcedureScopeAtOffset(scopes []vbProcedureScope, offset int) string {
	var best *vbProcedureScope
	for index := range scopes {
		scope := &scopes[index]
		if scope.StartOffset == 0 && scope.EndOffset == 0 || offset < scope.StartOffset || offset > scope.EndOffset {
			continue
		}
		if best == nil || scope.EndOffset-scope.StartOffset < best.EndOffset-best.StartOffset {
			best = scope
		}
	}
	if best == nil {
		return ""
	}
	return best.Key()
}

// vbProcedureScopeIndex answers repeated vbProcedureScopeAtOffset queries.
// Well-formed documents have disjoint procedure ranges, which allows a binary
// search; overlapping (malformed) ranges fall back to the linear scan.
type vbProcedureScopeIndex struct {
	scopes   []vbProcedureScope
	sorted   []vbProcedureScope
	disjoint bool
}

func newVBProcedureScopeIndex(scopes []vbProcedureScope) vbProcedureScopeIndex {
	sorted := make([]vbProcedureScope, 0, len(scopes))
	for _, scope := range scopes {
		if scope.StartOffset == 0 && scope.EndOffset == 0 || scope.StartOffset > scope.EndOffset {
			continue
		}
		sorted = append(sorted, scope)
	}
	sort.SliceStable(sorted, func(i, j int) bool { return sorted[i].StartOffset < sorted[j].StartOffset })
	disjoint := true
	for index := 1; disjoint && index < len(sorted); index++ {
		disjoint = sorted[index].StartOffset > sorted[index-1].EndOffset
	}
	return vbProcedureScopeIndex{scopes: scopes, sorted: sorted, disjoint: disjoint}
}

func (index vbProcedureScopeIndex) at(offset int) string {
	if !index.disjoint {
		return vbProcedureScopeAtOffset(index.scopes, offset)
	}
	position := sort.Search(len(index.sorted), func(i int) bool { return index.sorted[i].StartOffset > offset }) - 1
	if position < 0 || offset > index.sorted[position].EndOffset {
		return ""
	}
	return index.sorted[position].Key()
}

// vbProcedureScopeForUsageDeclaration retains the concrete procedure
// declaration behind a scope key. Scope keys intentionally identify a
// procedure by owner/name/accessor for lookup, so duplicate procedures need
// their source range to distinguish which signature owns a local declaration.
func vbProcedureScopeForUsageDeclaration(parsed *core.ParsedDocument, declaration vbUsageDeclaration) (vbProcedureScope, bool) {
	if parsed == nil || declaration.Scope == "" {
		return vbProcedureScope{}, false
	}
	key := strings.ToLower(strings.TrimSpace(declaration.Scope))
	var lineMatch *vbProcedureScope
	var offsetMatch *vbProcedureScope
	scopes := vbProcedureScopes(parsed)
	for index := range scopes {
		scope := &scopes[index]
		if !strings.EqualFold(scope.Key(), key) {
			continue
		}
		if declaration.Line >= scope.StartLine && declaration.Line <= scope.EndLine {
			if lineMatch == nil || scope.EndOffset-scope.StartOffset < lineMatch.EndOffset-lineMatch.StartOffset {
				lineMatch = scope
			}
		}
		if (declaration.Start != 0 || declaration.End != 0 || declaration.Range != (lsp.Range{})) &&
			declaration.Start >= scope.StartOffset && declaration.Start <= scope.EndOffset {
			if offsetMatch == nil || scope.EndOffset-scope.StartOffset < offsetMatch.EndOffset-offsetMatch.StartOffset {
				offsetMatch = scope
			}
		}
	}
	if offsetMatch != nil {
		return *offsetMatch, true
	}
	if lineMatch != nil {
		return *lineMatch, true
	}
	return vbProcedureScope{}, false
}

// serverObjectSymbols extracts the global VBScript symbols declared by
// server-side OBJECT elements. The HTML scanner keeps attribute ranges exact
// while the masked virtual document prevents ASP islands from becoming tags.
func serverObjectSymbols(parsed *core.ParsedDocument) []serverObjectSymbol {
	if parsed == nil || isStandaloneVBScriptDocument(parsed) {
		return nil
	}
	if value, ok := parsed.LoadRuntimeAnalysis(serverObjectSymbolsAnalysisKey); ok {
		if cached, ok := value.([]serverObjectSymbol); ok {
			return cached
		}
	}
	var cached []serverObjectSymbol
	if parsed.LoadAnalysis(serverObjectSymbolsAnalysisKey, &cached) {
		parsed.StoreRuntimeAnalysis(serverObjectSymbolsAnalysisKey, cached)
		return cached
	}
	if !core.ContainsASCIIFold(parsed.Text, "<object") {
		empty := []serverObjectSymbol{}
		parsed.StoreRuntimeAnalysis(serverObjectSymbolsAnalysisKey, empty)
		parsed.StoreAnalysis(serverObjectSymbolsAnalysisKey, empty)
		return empty
	}
	virtual := core.BuildVirtualDocument(parsed, core.LanguageHTML)
	scanner := htmlservice.GetLanguageService().CreateScanner(virtual.Text)
	doc := core.SourceDocument(parsed)
	symbols := make([]serverObjectSymbol, 0)
	var tagName string
	var tagStart int
	var attributeName string
	attributes := map[string]serverObjectAttribute{}
	reset := func() {
		tagName = ""
		tagStart = -1
		attributeName = ""
		attributes = map[string]serverObjectAttribute{}
	}
	finish := func(tagEnd int) {
		defer reset()
		if !strings.EqualFold(tagName, "object") {
			return
		}
		runat, ok := attributes["runat"]
		if !ok || !strings.EqualFold(strings.TrimSpace(runat.Value), "server") {
			return
		}
		nameAttribute, ok := attributes["id"]
		if !ok || !isVBIdentifierName(nameAttribute.Value) {
			nameAttribute, ok = attributes["name"]
		}
		if !ok || !isVBIdentifierName(nameAttribute.Value) {
			return
		}
		typeName := "Object"
		if attribute, ok := attributes["progid"]; ok && strings.TrimSpace(attribute.Value) != "" {
			typeName = strings.TrimSpace(attribute.Value)
		} else if attribute, ok := attributes["classid"]; ok && strings.TrimSpace(attribute.Value) != "" {
			typeName = strings.TrimSpace(attribute.Value)
		}
		start, startOK := virtual.ToSourceOffset(nameAttribute.Start)
		end, endOK := virtual.ToSourceOffset(nameAttribute.End)
		tagSourceStart, tagStartOK := virtual.ToSourceOffset(tagStart)
		tagSourceEnd, tagEndOK := virtual.ToSourceOffset(tagEnd)
		if !startOK || !endOK || !tagStartOK || !tagEndOK {
			return
		}
		mappedAttributes := make(map[string]serverObjectAttribute, len(attributes))
		for name, attribute := range attributes {
			attributeStart, attributeStartOK := virtual.ToSourceOffset(attribute.Start)
			attributeEnd, attributeEndOK := virtual.ToSourceOffset(attribute.End)
			if !attributeStartOK || !attributeEndOK {
				continue
			}
			attribute.Start = attributeStart
			attribute.End = attributeEnd
			attribute.Range = doc.Range(attributeStart, attributeEnd)
			mappedAttributes[name] = attribute
		}
		declaration := vbUsageDeclaration{
			Name: nameAttribute.Value, Kind: "variable", Range: doc.Range(start, end),
			Start: start, End: end, Line: doc.PositionAt(start).Line, TypeName: typeName,
		}
		symbols = append(symbols, serverObjectSymbol{
			Declaration: declaration,
			Attributes:  mappedAttributes,
			TagRange:    doc.Range(tagSourceStart, tagSourceEnd),
		})
	}
	reset()
	for token := scanner.Scan(); token != htmlservice.TokenTypeEOS; token = scanner.Scan() {
		switch token {
		case htmlservice.TokenTypeStartTagOpen:
			reset()
			tagStart = scanner.GetTokenByteOffset()
		case htmlservice.TokenTypeStartTag:
			tagName = scanner.GetTokenText()
		case htmlservice.TokenTypeAttributeName:
			attributeName = strings.ToLower(scanner.GetTokenText())
		case htmlservice.TokenTypeAttributeValue:
			if attributeName == "" {
				continue
			}
			text := scanner.GetTokenText()
			start := scanner.GetTokenByteOffset()
			end := scanner.GetTokenByteEnd()
			if len(text) >= 2 && (text[0] == '"' && text[len(text)-1] == '"' || text[0] == '\'' && text[len(text)-1] == '\'') {
				start++
				end--
				text = text[1 : len(text)-1]
			}
			if _, duplicate := attributes[attributeName]; !duplicate {
				attributes[attributeName] = serverObjectAttribute{Name: attributeName, Value: text, Start: start, End: end}
			}
			attributeName = ""
		case htmlservice.TokenTypeStartTagClose, htmlservice.TokenTypeStartTagSelfClose:
			finish(scanner.GetTokenByteEnd())
		}
	}
	sort.SliceStable(symbols, func(i, j int) bool {
		return symbols[i].Declaration.Start < symbols[j].Declaration.Start
	})
	parsed.StoreRuntimeAnalysis(serverObjectSymbolsAnalysisKey, symbols)
	parsed.StoreAnalysis(serverObjectSymbolsAnalysisKey, symbols)
	return symbols
}

func serverObjectDeclarations(parsed *core.ParsedDocument) []vbUsageDeclaration {
	symbols := serverObjectSymbols(parsed)
	declarations := make([]vbUsageDeclaration, 0, len(symbols))
	for _, symbol := range symbols {
		declarations = append(declarations, symbol.Declaration)
	}
	return declarations
}

func serverObjectSymbolAt(parsed *core.ParsedDocument, position lsp.Position) (serverObjectSymbol, bool) {
	for _, symbol := range serverObjectSymbols(parsed) {
		if lspPositionInRange(position, symbol.Declaration.Range) {
			return symbol, true
		}
	}
	return serverObjectSymbol{}, false
}

func serverObjectSymbolNamed(parsed *core.ParsedDocument, name string) (serverObjectSymbol, bool) {
	for _, symbol := range serverObjectSymbols(parsed) {
		if strings.EqualFold(symbol.Declaration.Name, name) {
			return symbol, true
		}
	}
	return serverObjectSymbol{}, false
}

func (s *Server) includedServerObjectIndexContext(ctx context.Context, root *core.ParsedDocument) (*includedServerObjectIndex, bool) {
	if root == nil || len(root.Includes) == 0 {
		return &includedServerObjectIndex{Objects: map[string]int{}, Globals: map[string]int{}, Procedures: map[string]int{}, First: map[string]includedNameIndexEntry{}}, true
	}
	if ctx == nil {
		ctx = context.Background()
	}
	if ctx.Err() != nil {
		return nil, false
	}
	if value, loaded := root.LoadRuntimeAnalysis(includedServerObjectIndexKey); loaded {
		if cached, valid := value.(*includedServerObjectIndex); valid && cached != nil && s.includedServerObjectIndexCurrent(ctx, cached) {
			return cached, true
		}
	}

	s.mu.Lock()
	hook := s.serverObjectLookupTestHook
	s.mu.Unlock()
	if hook != nil {
		hook()
	}
	index := &includedServerObjectIndex{
		Objects:    map[string]int{},
		Globals:    map[string]int{},
		Procedures: map[string]int{},
		First:      map[string]includedNameIndexEntry{},
	}
	active := map[string]struct{}{}
	seenDocuments := map[string]struct{}{workspacepkg.FileIdentityKeyFromURI(root.URI): {}}
	documents := map[string]*core.ParsedDocument{workspacepkg.FileIdentityKeyFromURI(root.URI): root}
	resolvedIncludes := map[string]*core.ParsedDocument{}
	resolvedDependencies := map[string]includedServerObjectDependency{}
	work := 0
	complete := true

	consumeWork := func() bool {
		if ctx.Err() != nil || work >= includeExpansionUnitBudget {
			complete = false
			return false
		}
		work++
		return true
	}
	resolveInclude := func(parent *core.ParsedDocument, include core.Include) *core.ParsedDocument {
		if parent == nil || ctx.Err() != nil {
			return nil
		}
		includeKey := includedServerObjectDependencyKey(parent.URI, include.Path, include.Mode)
		if dependency, resolved := resolvedDependencies[includeKey]; resolved {
			index.Dependencies = append(index.Dependencies, dependency)
			return resolvedIncludes[includeKey]
		}
		dependency := includedServerObjectDependency{ParentURI: parent.URI, Path: include.Path, Mode: include.Mode}
		if details, resolved := s.includeTargetDetailsForModeContext(ctx, parent.URI, include.Path, include.Mode); resolved {
			dependency.Resolved = true
			dependency.ResolvedPath = details.Path
			dependency.Exists = details.Exists
		}
		var included *core.ParsedDocument
		if targetPath, resolved := s.includeTargetPathForModeContext(ctx, parent.URI, include.Path, include.Mode); resolved && targetPath != "" {
			key := workspacepkg.FileIdentityKeyFromURI(filePathURI(targetPath))
			included = documents[key]
		}
		if included == nil {
			included = s.vbscriptIncludedDocumentContext(ctx, parent, include)
		}
		if included != nil {
			key := workspacepkg.FileIdentityKeyFromURI(included.URI)
			documents[key] = included
			dependency.ContentHash = workspacepkg.DiskContentHash(included.Text)
		}
		resolvedIncludes[includeKey] = included
		resolvedDependencies[includeKey] = dependency
		index.Dependencies = append(index.Dependencies, dependency)
		return included
	}

	var visit func(*core.ParsedDocument, bool, int) bool
	visit = func(document *core.ParsedDocument, rootDocument bool, visibleAt int) bool {
		if document == nil || ctx.Err() != nil {
			complete = false
			return false
		}
		key := workspacepkg.FileIdentityKeyFromURI(document.URI)
		if _, visiting := active[key]; visiting {
			return true
		}
		active[key] = struct{}{}
		defer delete(active, key)
		if !rootDocument {
			if _, visited := seenDocuments[key]; visited {
				return true
			}
			seenDocuments[key] = struct{}{}
			if !consumeWork() {
				return false
			}
		}
		textDocument := core.SourceDocument(document)
		events := make([]includedNameIndexEvent, 0, len(document.Includes))
		if !rootDocument {
			classOwners := vbClassMemberLineOwners(document)
			for _, symbol := range serverObjectSymbols(document) {
				if ctx.Err() != nil {
					complete = false
					return false
				}
				name := strings.ToLower(symbol.Declaration.Name)
				if name != "" {
					attributes := make([]includedNameIndexAttribute, 0, 5)
					for _, attributeName := range []string{"progid", "classid", "runat", "id", "name"} {
						if attribute, ok := symbol.Attributes[attributeName]; ok {
							attributes = append(attributes, includedNameIndexAttribute{Name: attributeName, Value: attribute.Value})
						}
					}
					events = append(events, includedNameIndexEvent{
						Name: name, Kind: "object", Start: symbol.Declaration.Start, End: symbol.Declaration.End,
						Entry: includedNameIndexEntry{
							Kind: "object", URI: document.URI, TypeName: symbol.Declaration.TypeName, Declaration: symbol.Declaration,
							ObjectAttributes: attributes,
						},
					})
				}
			}
			for _, declaration := range collectVBUsageDeclarations(document).Declarations {
				if declaration.Local || declaration.Scope != "" || declaration.MemberOf != "" {
					continue
				}
				switch strings.ToLower(declaration.Kind) {
				case "variable", "constant", "const":
				default:
					continue
				}
				name := strings.ToLower(declaration.Name)
				if name == "" {
					continue
				}
				events = append(events, includedNameIndexEvent{
					Name: name, Kind: "global", Start: declaration.Start, End: declaration.End,
					Entry: includedNameIndexEntry{Kind: "global", URI: document.URI, TypeName: inferVBDeclarationType(document, declaration), Declaration: declaration},
				})
			}
			doc := core.SourceDocument(document)
			for _, signature := range vbscript.Signatures(document) {
				if ctx.Err() != nil {
					complete = false
					return false
				}
				if classOwners[signature.NameRange.Start.Line] != "" {
					continue
				}
				name := strings.ToLower(signature.Name)
				if name == "" {
					continue
				}
				start := doc.OffsetAt(signature.NameRange.Start)
				events = append(events, includedNameIndexEvent{
					Name: name, Kind: "procedure", Start: start, End: start,
					Entry: includedNameIndexEntry{Kind: "procedure", URI: document.URI, Signature: signature},
				})
			}
		}
		for _, include := range document.Includes {
			start := textDocument.OffsetAt(include.Range.Start)
			end := textDocument.OffsetAt(include.Range.End)
			events = append(events, includedNameIndexEvent{Kind: "include", Start: start, End: end, Include: include})
		}
		sort.SliceStable(events, func(left, right int) bool {
			if events[left].Start != events[right].Start {
				return events[left].Start < events[right].Start
			}
			return events[left].End < events[right].End
		})
		for _, event := range events {
			if ctx.Err() != nil {
				complete = false
				return false
			}
			if event.Kind == "include" {
				if !consumeWork() {
					return false
				}
				includedVisibleAt := visibleAt
				if rootDocument {
					includedVisibleAt = event.Start
				}
				if included := resolveInclude(document, event.Include); included != nil && ctx.Err() == nil {
					if !visit(included, false, includedVisibleAt) {
						return false
					}
				}
				if ctx.Err() != nil {
					complete = false
					return false
				}
				continue
			}
			if event.Name == "" {
				continue
			}
			switch event.Kind {
			case "object":
				if current, exists := index.Objects[event.Name]; !exists || visibleAt < current {
					index.Objects[event.Name] = visibleAt
				}
			case "global":
				if current, exists := index.Globals[event.Name]; !exists || visibleAt < current {
					index.Globals[event.Name] = visibleAt
				}
			case "procedure":
				if current, exists := index.Procedures[event.Name]; !exists || visibleAt < current {
					index.Procedures[event.Name] = visibleAt
				}
			default:
				continue
			}
			// Nested includes inherit the root include's visibleAt. Keep the first
			// expanded event for that visibility; event.Start is local to one
			// document and must not order declarations across files.
			current, exists := index.First[event.Name]
			if !exists || visibleAt < current.VisibleAt {
				entry := event.Entry
				entry.Kind = event.Kind
				entry.VisibleAt = visibleAt
				entry.Start = event.Start
				if entry.URI == "" {
					entry.URI = document.URI
				}
				index.First[event.Name] = entry
			}
		}
		return true
	}
	visit(root, true, 0)
	if ctx.Err() != nil {
		return nil, false
	}
	if !complete {
		return index, false
	}
	// Use one fixed runtime key per immutable owner revision. A stale index is
	// replaced rather than accumulated under resolution-specific keys.
	root.StoreRuntimeAnalysis(includedServerObjectIndexKey, index)
	return index, true
}

func (s *Server) includedServerObjectIndexCurrent(ctx context.Context, index *includedServerObjectIndex) bool {
	if index == nil {
		return false
	}
	validations := map[string]includedServerObjectDependencyValidation{}
	for _, dependency := range index.Dependencies {
		if ctx != nil && ctx.Err() != nil {
			return false
		}
		key := includedServerObjectDependencyKey(dependency.ParentURI, dependency.Path, dependency.Mode)
		validation, validated := validations[key]
		if !validated {
			validation.Details, validation.Resolved = s.includeTargetDetailsForModeContext(ctx, dependency.ParentURI, dependency.Path, dependency.Mode)
			if validation.Resolved && validation.Details.Exists && validation.Details.Path != "" {
				uri := filePathURI(validation.Details.Path)
				content := ""
				if document := s.documentByURI(uri); document != nil {
					content = document.Text
				} else {
					var err error
					content, err = s.readWorkspaceTextFileWithinBoundaries(ctx, validation.Details.Path, filepath.Dir(fileURIPath(dependency.ParentURI)))
					if err != nil {
						return false
					}
				}
				validation.ContentHash = workspacepkg.DiskContentHash(content)
			}
			validations[key] = validation
		}
		if validation.Resolved != dependency.Resolved {
			return false
		}
		if !validation.Resolved {
			continue
		}
		if validation.Details.Path != dependency.ResolvedPath || validation.Details.Exists != dependency.Exists {
			return false
		}
		if !validation.Details.Exists || validation.Details.Path == "" {
			continue
		}
		if validation.ContentHash != dependency.ContentHash {
			return false
		}
	}
	return ctx == nil || ctx.Err() == nil
}

// implicitVBDeclarations returns the first unresolved identifier in each
// VBScript scope when Option Explicit is disabled. Undeclared assignment
// targets are script-global even when the assignment is inside a procedure.
func implicitVBDeclarations(parsed *core.ParsedDocument) []vbUsageDeclaration {
	if parsed == nil {
		return nil
	}
	if value, ok := parsed.LoadRuntimeAnalysis(implicitVBDeclarationsAnalysisKey); ok {
		if cached, ok := value.([]vbUsageDeclaration); ok {
			return cached
		}
	}
	var cached []vbUsageDeclaration
	if parsed.LoadAnalysis(implicitVBDeclarationsAnalysisKey, &cached) {
		parsed.StoreRuntimeAnalysis(implicitVBDeclarationsAnalysisKey, cached)
		return cached
	}
	if hasVBOptionExplicit(parsed) {
		empty := []vbUsageDeclaration{}
		parsed.StoreRuntimeAnalysis(implicitVBDeclarationsAnalysisKey, empty)
		parsed.StoreAnalysis(implicitVBDeclarationsAnalysisKey, empty)
		return empty
	}
	doc := core.SourceDocument(parsed)
	declarationRanges := map[offsetRange]struct{}{}
	explicitGlobals := map[string]struct{}{}
	explicitLocals := map[string]struct{}{}
	usageDeclarations := normalizedVBUsageDeclarations(parsed)
	localDeclarationRanges := map[offsetRange]struct{}{}
	for _, declaration := range usageDeclarations {
		if declaration.Local {
			localDeclarationRanges[offsetRangeKey(declaration.Start, declaration.End)] = struct{}{}
		}
	}
	for _, declaration := range usageDeclarations {
		declarationRanges[offsetRangeKey(declaration.Start, declaration.End)] = struct{}{}
		if declaration.Local {
			explicitLocals[implicitDeclarationScopeKey(true, declaration.Scope, declaration.Name)] = struct{}{}
		} else {
			if _, isLocalDuplicate := localDeclarationRanges[offsetRangeKey(declaration.Start, declaration.End)]; isLocalDuplicate {
				continue
			}
			explicitGlobals[strings.ToLower(declaration.Name)] = struct{}{}
		}
	}
	for _, declaration := range serverObjectDeclarations(parsed) {
		explicitGlobals[strings.ToLower(declaration.Name)] = struct{}{}
	}
	assignments := map[string]vbUsageDeclaration{}
	globalAssignmentNames := map[string]struct{}{}
	for _, assignment := range implicitAssignmentInlayDeclarations(parsed, true, nil) {
		assignments[implicitAssignmentInlayKey(assignment)] = assignment
		if !assignment.Local {
			globalAssignmentNames[strings.ToLower(assignment.Name)] = struct{}{}
		}
	}
	procedures := vbProcedureScopes(parsed)
	procedureIndex := newVBProcedureScopeIndex(procedures)
	candidates := map[string]vbUsageDeclaration{}
	for _, region := range parsed.Regions {
		if region.Language != core.LanguageVBScript {
			continue
		}
		text := parsed.Text[region.ContentStart:region.ContentEnd]
		for _, span := range vbIdentifierSpansForAnalysis(text) {
			start := region.ContentStart + span.Start
			end := region.ContentStart + span.End
			if _, declaration := declarationRanges[offsetRangeKey(start, end)]; declaration {
				continue
			}
			name := parsed.Text[start:end]
			lower := strings.ToLower(name)
			if isDeclaredVBBuiltinOrKeywordForDocument(parsed, lower) || previousNonSpace(parsed.Text, start) == '.' || previousNonSpace(parsed.Text, start) == '&' {
				continue
			}
			if implicitIdentifierIsCallOrNamedArgument(parsed.Text, start, end) {
				continue
			}
			position := doc.PositionAt(start)
			scope := procedureIndex.at(start)
			if _, explicitGlobal := explicitGlobals[lower]; explicitGlobal {
				continue
			}
			if scope != "" {
				if _, explicitLocal := explicitLocals[implicitDeclarationScopeKey(true, scope, lower)]; explicitLocal {
					continue
				}
			}
			local := scope != ""
			if _, assignedGlobal := globalAssignmentNames[lower]; assignedGlobal {
				local = false
				scope = ""
			}
			key := implicitDeclarationScopeKey(local, scope, lower)
			if _, exists := candidates[key]; exists {
				continue
			}
			candidates[key] = vbUsageDeclaration{
				Name: name, Kind: "variable", Range: doc.Range(start, end), Start: start, End: end,
				Line: position.Line, Local: local, Scope: scope, Implicit: true,
			}
		}
	}
	declarations := make([]vbUsageDeclaration, 0, len(candidates))
	for key, declaration := range candidates {
		if assignment, ok := assignments[key]; ok {
			declaration.AssignedValue = assignment.AssignedValue
			declaration.TypeName = assignment.TypeName
		}
		declarations = append(declarations, declaration)
	}
	sort.SliceStable(declarations, func(i, j int) bool {
		return declarations[i].Start < declarations[j].Start
	})
	parsed.StoreRuntimeAnalysis(implicitVBDeclarationsAnalysisKey, declarations)
	parsed.StoreAnalysis(implicitVBDeclarationsAnalysisKey, declarations)
	return declarations
}

func implicitIdentifierIsCallOrNamedArgument(text string, start, end int) bool {
	next := nextNonSpaceByte(text, end)
	if next >= 0 {
		if text[next] == '(' {
			depth := 0
			closeIndex := -1
			for index := next; index < len(text); index++ {
				switch text[index] {
				case '"':
					index = skipVBString(text, index) - 1
				case '(':
					depth++
				case ')':
					depth--
					if depth == 0 {
						closeIndex = index
						index = len(text)
					}
				}
			}
			if closeIndex < 0 || nextNonSpaceByte(text, closeIndex+1) < 0 || text[nextNonSpaceByte(text, closeIndex+1)] != '=' {
				return true
			}
		}
		if text[next] == ':' {
			afterColon := nextNonSpaceByte(text, next+1)
			if afterColon >= 0 && text[afterColon] == '=' {
				return true
			}
		}
	}
	previousEnd := start
	for previousEnd > 0 && isVBWhitespace(text[previousEnd-1]) {
		previousEnd--
	}
	previousStart := previousEnd
	for previousStart > 0 && isVBIdentifier(text[previousStart-1]) {
		previousStart--
	}
	return previousStart < previousEnd && strings.EqualFold(text[previousStart:previousEnd], "call")
}

func (s *Server) serverObjectTarget(parsed *core.ParsedDocument, position lsp.Position) (*core.ParsedDocument, serverObjectSymbol, bool) {
	return s.serverObjectTargetContext(context.Background(), parsed, position)
}

func (s *Server) serverObjectTargetContext(ctx context.Context, parsed *core.ParsedDocument, position lsp.Position) (*core.ParsedDocument, serverObjectSymbol, bool) {
	if ctx == nil {
		ctx = context.Background()
	}
	if ctx.Err() != nil {
		return nil, serverObjectSymbol{}, false
	}
	if parsed == nil {
		return nil, serverObjectSymbol{}, false
	}
	if symbol, ok := serverObjectSymbolAt(parsed, position); ok {
		return parsed, symbol, true
	}
	doc := core.SourceDocument(parsed)
	offset := doc.OffsetAt(position)
	region := core.RegionAt(parsed, offset)
	if region == nil || region.Language != core.LanguageVBScript {
		return nil, serverObjectSymbol{}, false
	}
	name := vbWordAtOffset(parsed.Text, offset)
	if name == "" {
		return nil, serverObjectSymbol{}, false
	}
	if vbLocalDeclarationShadowsNameAt(parsed, name, position) {
		return nil, serverObjectSymbol{}, false
	}
	units, complete := s.vbscriptIncludeExecutionUnitsThroughOffsetContextResult(ctx, parsed, offset)
	if !complete || ctx.Err() != nil {
		return nil, serverObjectSymbol{}, false
	}
	for _, unit := range units {
		if ctx.Err() != nil {
			return nil, serverObjectSymbol{}, false
		}
		if unit.document == nil || unit.start >= unit.end {
			continue
		}
		for _, symbol := range serverObjectSymbols(unit.document) {
			if symbol.Declaration.Start < unit.start || symbol.Declaration.Start >= unit.end {
				continue
			}
			if strings.EqualFold(symbol.Declaration.Name, name) {
				return unit.document, symbol, true
			}
		}
	}
	return nil, serverObjectSymbol{}, false
}

func vbLocalDeclarationShadowsNameAt(parsed *core.ParsedDocument, name string, position lsp.Position) bool {
	if parsed == nil {
		return false
	}
	doc := vbTextDocument(parsed)
	scope := vbProcedureScopeAtOffset(vbProcedureScopes(parsed), doc.OffsetAt(position))
	if scope == "" {
		return false
	}
	if names := vbLocalDeclarationShadowNames(parsed)[strings.ToLower(scope)]; names != nil {
		_, shadowed := names[strings.ToLower(name)]
		return shadowed
	}
	return false
}

func vbLocalDeclarationShadowNames(parsed *core.ParsedDocument) map[string]map[string]bool {
	if parsed == nil {
		return nil
	}
	if value, ok := parsed.LoadRuntimeAnalysis(vbLocalShadowNamesAnalysisKey); ok {
		if cached, ok := value.(map[string]map[string]bool); ok {
			return cached
		}
	}
	shadowNames := map[string]map[string]bool{}
	for _, declaration := range normalizedVBUsageDeclarations(parsed) {
		if !declaration.Local {
			continue
		}
		scope := strings.ToLower(strings.TrimSpace(declaration.Scope))
		name := strings.ToLower(strings.TrimSpace(declaration.Name))
		if name == "" {
			continue
		}
		if shadowNames[scope] == nil {
			shadowNames[scope] = map[string]bool{}
		}
		shadowNames[scope][name] = true
	}
	parsed.StoreRuntimeAnalysis(vbLocalShadowNamesAnalysisKey, shadowNames)
	return shadowNames
}

// vbIdentifierSpansForAnalysis uses the shared lexer so date literals and
// their contents cannot become implicit declarations.
func vbIdentifierSpansForAnalysis(text string) []vbIdentifierSpan {
	spans := make([]vbIdentifierSpan, 0, 16)
	for _, token := range vbscript.Tokenize(text) {
		if token.Kind != "identifier" {
			continue
		}
		spans = append(spans, vbIdentifierSpan{Start: token.Start, End: token.End})
	}
	return spans
}

func init() {
	// Keep the existing declaration consumers in sync with VBScript's default
	// Public property accessors while preserving their capture indexes.
	vbPropertyLinePattern = regexp.MustCompile(`(?i)^\s*(?:(?:Public|Private|Default)\s+)*Property\s+(?:Get|Let|Set)\s+([A-Za-z_][A-Za-z0-9_]*)`)
}

func normalizedVBUsageDeclarations(parsed *core.ParsedDocument) []vbUsageDeclaration {
	if parsed == nil {
		return nil
	}
	base := collectVBUsageDeclarations(parsed).Declarations
	parameters := vbParameterDeclarationsFromTokens(parsed)
	if len(parameters) == 0 {
		return base
	}
	declarations := make([]vbUsageDeclaration, 0, len(base)+len(parameters))
	for _, declaration := range base {
		if declaration.Kind == "parameter" {
			continue
		}
		declarations = append(declarations, declaration)
	}
	declarations = append(declarations, parameters...)
	sort.SliceStable(declarations, func(i, j int) bool {
		if declarations[i].Start != declarations[j].Start {
			return declarations[i].Start < declarations[j].Start
		}
		return declarations[i].End < declarations[j].End
	})
	return declarations
}

func vbParameterDeclarationsFromTokens(parsed *core.ParsedDocument) []vbUsageDeclaration {
	doc := core.SourceDocument(parsed)
	scopes := vbProcedureScopes(parsed)
	scopeIndex := newVBProcedureScopeIndex(scopes)
	declarations := make([]vbUsageDeclaration, 0)
	seen := map[offsetRange]struct{}{}
	for _, region := range parsed.Regions {
		if region.Language != core.LanguageVBScript {
			continue
		}
		for lineStart := region.ContentStart; lineStart < region.ContentEnd; {
			lineEnd := vbPhysicalLineEnd(parsed.Text, lineStart, region.ContentEnd)
			header, logicalEnd, headerOK := vbscript.ProcedureHeaderAtLogical(parsed.Text, lineStart)
			if headerOK {
				if logicalEnd <= region.ContentEnd {
					lineEnd = logicalEnd
				} else {
					headerOK = false
				}
			}
			if headerOK && header.HasParameterList && header.ParamsEnd >= header.ParamsStart {
				nameStart, nameEnd := header.NameStart, header.NameEnd
				paramsStart, paramsEnd := header.ParamsStart, header.ParamsEnd
				// CST procedure nodes start at the first keyword rather than the
				// line's indentation. Use the declaration name as an in-scope
				// offset so property accessors retain their qualified scope key.
				scope := scopeIndex.at(nameStart)
				if scope == "" {
					scope = strings.ToLower(parsed.Text[nameStart:nameEnd])
				}
				for _, segment := range splitVBSegments(parsed.Text[paramsStart:paramsEnd]) {
					part := parsed.Text[paramsStart+segment.Start : paramsStart+segment.End]
					tokens := vbscript.Tokenize(part)
					for index, token := range tokens {
						if vbContinuationToken(tokens, index) || token.Kind != "identifier" {
							continue
						}
						lower := strings.ToLower(token.Text)
						if lower == "optional" || lower == "byref" || lower == "byval" || lower == "paramarray" {
							continue
						}
						start := paramsStart + segment.Start + token.Start
						end := paramsStart + segment.Start + token.End
						key := offsetRangeKey(start, end)
						if _, duplicate := seen[key]; duplicate {
							break
						}
						seen[key] = struct{}{}
						declarations = append(declarations, vbUsageDeclaration{
							Name: parsed.Text[start:end], Kind: "parameter", Range: doc.Range(start, end),
							Start: start, End: end, Line: doc.PositionAt(start).Line, Local: true, Scope: scope,
						})
						break
					}
				}
			}
			if lineEnd >= region.ContentEnd {
				break
			}
			lineStart = vbNextLineStart(parsed.Text, lineEnd, region.ContentEnd)
		}
	}
	sort.SliceStable(declarations, func(i, j int) bool { return declarations[i].Start < declarations[j].Start })
	return declarations
}

func vbPropertyAccessorSignatureAt(parsed *core.ParsedDocument, position lsp.Position) (vbscript.Signature, bool) {
	if parsed == nil {
		return vbscript.Signature{}, false
	}
	doc := core.SourceDocument(parsed)
	var signatures []vbscript.Signature
	var current *vbscript.Signature
	for _, region := range parsed.Regions {
		if region.Language != core.LanguageVBScript {
			continue
		}
		for lineStart := region.ContentStart; lineStart < region.ContentEnd; {
			lineEnd := vbPhysicalLineEnd(parsed.Text, lineStart, region.ContentEnd)
			header, logicalEnd, headerOK := vbscript.ProcedureHeaderAtLogical(parsed.Text, lineStart)
			if headerOK {
				if logicalEnd <= region.ContentEnd {
					lineEnd = logicalEnd
				} else {
					headerOK = false
				}
			}
			line := parsed.Text[lineStart:lineEnd]
			trimmed := strings.ToLower(strings.TrimSpace(line))
			if headerOK && header.Kind == "property" {
				nameStart := header.NameStart
				nameEnd := header.NameEnd
				params := ""
				if header.HasParameterList {
					params = parsed.Text[header.ParamsStart:header.ParamsEnd]
				}
				visibility := header.Visibility
				prefix := ""
				if visibility != "" {
					prefix = visibility + " "
				}
				accessor := parsed.Text[header.AccessorStart:header.AccessorEnd]
				name := parsed.Text[header.NameStart:header.NameEnd]
				label := prefix + "Property " + accessor + " " + name + "(" + vbProcedureParameterDisplay(params) + ")"
				signature := vbscript.Signature{
					Name: name, Kind: "property", Label: label,
					Range: doc.Range(lineStart, lineEnd), NameRange: doc.Range(nameStart, nameEnd),
					Parameters: vbPropertyParameters(params),
				}
				signatures = append(signatures, signature)
				current = &signatures[len(signatures)-1]
			}
			if current != nil {
				current.Range.End = doc.PositionAt(lineEnd)
				if trimmed == "end property" {
					current = nil
				}
			}
			if lineEnd >= region.ContentEnd {
				break
			}
			lineStart = vbNextLineStart(parsed.Text, lineEnd, region.ContentEnd)
		}
	}
	for _, signature := range signatures {
		if position.Line >= signature.Range.Start.Line && position.Line <= signature.Range.End.Line {
			return signature, true
		}
	}
	return vbscript.Signature{}, false
}

func vbPropertyParameters(text string) []vbscript.Parameter {
	parameters := make([]vbscript.Parameter, 0)
	for _, segment := range splitVBSegments(text) {
		part := text[segment.Start:segment.End]
		parameter := vbscript.Parameter{Mode: "ByRef"}
		tokens := vbscript.Tokenize(part)
		for index, token := range tokens {
			if vbContinuationToken(tokens, index) {
				continue
			}
			if token.Kind != "identifier" && token.Kind != "keyword" {
				continue
			}
			switch strings.ToLower(token.Text) {
			case "optional":
				parameter.Optional = true
			case "byval":
				parameter.Mode = "ByVal"
			case "byref", "paramarray":
				if strings.EqualFold(token.Text, "paramarray") {
					parameter.Optional = true
				}
				parameter.Mode = "ByRef"
			default:
				if parameter.Name == "" {
					parameter.Name = token.Text
				}
			}
		}
		if parameter.Name != "" {
			parameters = append(parameters, parameter)
		}
	}
	return parameters
}

func vbWordAtOffset(text string, offset int) string {
	if offset < 0 || offset > len(text) {
		return ""
	}
	start := offset
	if start == len(text) || start < len(text) && !isVBIdentifier(text[start]) {
		start--
	}
	if start < 0 || !isVBIdentifier(text[start]) {
		return ""
	}
	for start > 0 && isVBIdentifier(text[start-1]) {
		start--
	}
	end := start
	for end < len(text) && isVBIdentifier(text[end]) {
		end++
	}
	return text[start:end]
}

func (s *Server) serverObjectHover(parsed *core.ParsedDocument, position lsp.Position) *lsp.Hover {
	return s.serverObjectHoverContext(context.Background(), parsed, position)
}

func (s *Server) serverObjectHoverContext(ctx context.Context, parsed *core.ParsedDocument, position lsp.Position) *lsp.Hover {
	if ctx == nil {
		ctx = context.Background()
	}
	if ctx.Err() != nil {
		return nil
	}
	owner, symbol, ok := s.serverObjectTargetContext(ctx, parsed, position)
	if !ok {
		return nil
	}
	declaration := symbol.Declaration
	locations := s.serverObjectReferenceLocations(ctx, parsed, owner, symbol, false)
	if ctx.Err() != nil {
		return nil
	}
	lines := []string{"```vbscript", "(global) Dim " + declaration.Name + " As " + declaration.TypeName, "```", "", "Server OBJECT declaration."}
	for _, name := range []string{"progid", "classid", "runat", "id", "name"} {
		if attribute, exists := symbol.Attributes[name]; exists {
			lines = append(lines, "", "- `"+name+"`: `"+attribute.Value+"`")
		}
	}
	lines = append(lines, "", "References: "+strconv.Itoa(len(locations)))
	if owner != parsed {
		if note := s.vbscriptDefinedInNote(owner.URI); note != "" {
			lines = append(lines, "", note)
		}
	}
	hover := &lsp.Hover{Contents: lsp.MarkupContent{Kind: "markdown", Value: strings.Join(lines, "\n")}}
	if owner == parsed {
		hover.Range = &declaration.Range
	}
	return hover
}

func (s *Server) serverObjectDefinition(parsed *core.ParsedDocument, position lsp.Position) []lsp.Location {
	return s.serverObjectDefinitionContext(context.Background(), parsed, position)
}

func (s *Server) serverObjectDefinitionContext(ctx context.Context, parsed *core.ParsedDocument, position lsp.Position) []lsp.Location {
	owner, symbol, ok := s.serverObjectTargetContext(ctx, parsed, position)
	if !ok {
		return nil
	}
	return []lsp.Location{{URI: owner.URI, Range: symbol.Declaration.Range}}
}

func (s *Server) serverObjectReferenceLocations(ctx context.Context, parsed, owner *core.ParsedDocument, symbol serverObjectSymbol, includeDeclaration bool) []lsp.Location {
	if parsed == nil || owner == nil {
		return nil
	}
	if ctx == nil {
		ctx = context.Background()
	}
	if ctx.Err() != nil {
		return nil
	}
	included, complete := s.includedDocumentsContextResult(ctx, parsed)
	if !complete || ctx.Err() != nil {
		return nil
	}
	documents := append([]*core.ParsedDocument{parsed}, included...)
	referenceDocuments, referenceComplete := s.workspaceReferenceDocumentsContextResult(ctx, parsed)
	if !referenceComplete || ctx.Err() != nil {
		return nil
	}
	documents = append(documents, referenceDocuments...)
	documents = dedupeParsedDocumentsByFileIdentity(documents)
	lower := strings.ToLower(symbol.Declaration.Name)
	locations := make([]lsp.Location, 0)
	if includeDeclaration {
		locations = append(locations, lsp.Location{URI: owner.URI, Range: symbol.Declaration.Range})
	}
	for _, document := range documents {
		if document == nil || ctx.Err() != nil {
			continue
		}
		for _, posting := range vbscript.BuildReferenceShard(document).PostingsFor(lower) {
			if vbLocalDeclarationShadowsNameAt(document, symbol.Declaration.Name, posting.Range.Start) {
				continue
			}
			locations = append(locations, lsp.Location{URI: document.URI, Range: posting.Range})
		}
	}
	if ctx.Err() != nil {
		return nil
	}
	sort.SliceStable(locations, func(i, j int) bool {
		if locations[i].URI != locations[j].URI {
			return locations[i].URI < locations[j].URI
		}
		if locations[i].Range.Start.Line != locations[j].Range.Start.Line {
			return locations[i].Range.Start.Line < locations[j].Range.Start.Line
		}
		return locations[i].Range.Start.Character < locations[j].Range.Start.Character
	})
	return locations
}

func (s *Server) serverObjectReferences(ctx context.Context, parsed *core.ParsedDocument, position lsp.Position, includeDeclaration bool) ([]lsp.Location, bool) {
	owner, symbol, ok := s.serverObjectTargetContext(ctx, parsed, position)
	if !ok {
		return nil, false
	}
	return s.serverObjectReferenceLocations(ctx, parsed, owner, symbol, includeDeclaration), true
}

func (s *Server) serverObjectRenameRangeContext(ctx context.Context, parsed *core.ParsedDocument, position lsp.Position) (*lsp.Range, bool) {
	_, symbol, ok := s.serverObjectTargetContext(ctx, parsed, position)
	if !ok {
		return nil, false
	}
	r := symbol.Declaration.Range
	return &r, true
}

func (s *Server) serverObjectRenameEdit(parsed *core.ParsedDocument, position lsp.Position, newName string) (map[string]any, bool) {
	return s.serverObjectRenameEditContext(context.Background(), parsed, position, newName)
}

func (s *Server) serverObjectRenameEditContext(ctx context.Context, parsed *core.ParsedDocument, position lsp.Position, newName string) (map[string]any, bool) {
	owner, symbol, ok := s.serverObjectTargetContext(ctx, parsed, position)
	if !ok {
		return nil, false
	}
	if !isVBIdentifierName(newName) {
		return map[string]any{"changes": map[string][]lsp.TextEdit{}}, true
	}
	locations := s.serverObjectReferenceLocations(ctx, parsed, owner, symbol, true)
	changes := map[string][]lsp.TextEdit{}
	for _, location := range locations {
		changes[location.URI] = append(changes[location.URI], lsp.TextEdit{Range: location.Range, NewText: newName})
	}
	return map[string]any{"changes": changes}, true
}
