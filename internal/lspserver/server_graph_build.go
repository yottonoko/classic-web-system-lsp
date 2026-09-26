package lspserver

import (
	"context"
	"path/filepath"
	"regexp"
	"sort"
	"strconv"
	"strings"
	"sync/atomic"

	"github.com/yottonoko/classic-web-system-lsp/internal/core"
	"github.com/yottonoko/classic-web-system-lsp/internal/graph"
	"github.com/yottonoko/classic-web-system-lsp/internal/lsp"
	"github.com/yottonoko/classic-web-system-lsp/internal/vbscript"
	workspacepkg "github.com/yottonoko/classic-web-system-lsp/internal/workspace"
)

func (s *Server) buildWorkspaceGraph(includeAnalysisTypeDetails bool) graph.Payload {
	cacheKey := s.graphCacheKey(graphCommandArg{Scope: "workspace", IncludeAnalysisTypeDetails: includeAnalysisTypeDetails})
	if cached, ok := s.cachedGraphPayload(cacheKey); ok {
		return cached
	}
	collection := s.workspaceGraphDocumentsContextWithProgressResult(context.Background(), true, nil)
	if !collection.complete {
		return graph.Payload{}
	}
	parsed, rootURI := collection.documents, collection.rootURI
	s.logDebugVerbose("[asp-lsp] asp.graph.bulk.spill.write: documents=" + strconv.Itoa(len(parsed)))
	payload, complete := s.buildDocumentSetGraphWithProgressAtGeneration(context.Background(), collection.generation, "workspace", rootURI, parsed, true, includeAnalysisTypeDetails, nil)
	if !complete {
		return graph.Payload{}
	}
	s.logDebugVerbose("[asp-lsp] asp.graph.bulk.complete: documents=" + strconv.Itoa(len(parsed)))
	if !s.storeGraphPayloadContext(context.Background(), collection.generation, cacheKey, payload) {
		return graph.Payload{}
	}
	return payload
}

func (s *Server) buildDocumentSetGraph(scope, rootURI string, documents []*core.ParsedDocument, includeExternalFiles bool, includeAnalysisTypeDetails bool) graph.Payload {
	payload, _ := s.buildDocumentSetGraphWithProgress(context.Background(), scope, rootURI, documents, includeExternalFiles, includeAnalysisTypeDetails, nil)
	return payload
}

func (s *Server) buildDocumentSetGraphWithProgress(ctx context.Context, scope, rootURI string, documents []*core.ParsedDocument, includeExternalFiles bool, includeAnalysisTypeDetails bool, report graphProgressReporter) (graph.Payload, bool) {
	return s.buildDocumentSetGraphWithProgressAtGeneration(ctx, s.graphGenerationSnapshot(), scope, rootURI, documents, includeExternalFiles, includeAnalysisTypeDetails, report)
}

func (s *Server) buildDocumentSetGraphWithProgressAtGeneration(ctx context.Context, generation uint64, scope, rootURI string, documents []*core.ParsedDocument, includeExternalFiles bool, includeAnalysisTypeDetails bool, report graphProgressReporter) (graph.Payload, bool) {
	if ctx == nil {
		ctx = context.Background()
	}
	incomplete := func() (graph.Payload, bool) {
		return graph.Payload{}, false
	}
	if ctx.Err() != nil || !s.graphGenerationCurrent(ctx, generation) {
		return incomplete()
	}
	payload := emptyGraphPayload(scope, "", rootURI)
	payload.Settings = s.graphPayloadSettings()
	declarationsByURI := make(map[string][]vbUsageDeclaration, len(documents))
	declarationLists := make([][]vbUsageDeclaration, len(documents))
	legacyDeclarationsByURI := map[string][]vbUsageDeclaration{}
	if catalog, ok := s.WorkspaceLegacyUndefinedGlobals(ctx); ok {
		for _, symbol := range catalog.Symbols {
			key := workspacepkg.FileIdentityKeyFromURI(symbol.OriginURI)
			legacyDeclarationsByURI[key] = append(legacyDeclarationsByURI[key], legacyUndefinedGlobalDeclaration(symbol))
		}
	}
	s.mu.Lock()
	workerSymbolExtraction := s.settings.GraphWorkerSymbolExtraction
	workers := s.analysisWorkers
	s.mu.Unlock()
	var extracted atomic.Int64
	collectDeclarations := func(workerCtx context.Context, index int) {
		if workerCtx.Err() != nil {
			return
		}
		if documents[index] != nil {
			declarationLists[index] = graphVBDeclarations(documents[index])
			key := workspacepkg.FileIdentityKeyFromURI(documents[index].URI)
			for _, declaration := range legacyDeclarationsByURI[key] {
				if !vbUsageDeclarationsContain(declarationLists[index], declaration) {
					declarationLists[index] = append(declarationLists[index], declaration)
				}
			}
		}
		if report != nil {
			current := int(extracted.Add(1))
			detail := ""
			if documents[index] != nil {
				detail = progressDetailForURI(documents[index].URI)
			}
			report("graph."+scope+".indexDeclarations", detail, current, len(documents))
		}
	}
	if workerSymbolExtraction && workers != nil {
		s.logDebugSummary("[asp-lsp] graphVbIndex.workerSymbolExtraction: documents=" + strconv.Itoa(len(documents)))
		workers.parallelForBulk(ctx, len(documents), collectDeclarations)
	} else {
		for index := range documents {
			collectDeclarations(ctx, index)
		}
	}
	for index, parsed := range documents {
		if ctx.Err() != nil || !s.graphGenerationCurrent(ctx, generation) {
			return incomplete()
		}
		if parsed != nil {
			declarationsByURI[parsed.URI] = declarationLists[index]
		}
		if report != nil {
			detail := ""
			if parsed != nil {
				detail = progressDetailForURI(parsed.URI)
			}
			report("graph."+scope+".prepareDocuments", detail, index+1, len(documents))
		}
	}
	declarationsFor := func(parsed *core.ParsedDocument) []vbUsageDeclaration {
		if parsed == nil {
			return nil
		}
		return declarationsByURI[parsed.URI]
	}
	documentURIs := map[string]string{}
	for _, parsed := range documents {
		if parsed != nil {
			documentURIs[workspacepkg.FileIdentityKeyFromURI(parsed.URI)] = parsed.URI
		}
	}
	seenNodes := map[string]struct{}{}
	fileNodeURIs := map[string]string{}
	fileCount := 0
	declarationCount := 0
	includeCount := 0
	addNode := func(node graph.Node) bool {
		if node.ID == "" {
			return false
		}
		if _, ok := seenNodes[node.ID]; ok {
			return false
		}
		seenNodes[node.ID] = struct{}{}
		payload.Nodes = append(payload.Nodes, node)
		return true
	}
	addFileNode := func(uri string) string {
		if uri == "" {
			return ""
		}
		identity := workspacepkg.FileIdentityKeyFromURI(uri)
		if existing := fileNodeURIs[identity]; existing != "" {
			return existing
		}
		fileNodeURIs[identity] = uri
		fileName := filepath.Base(fileURIPath(uri))
		if addNode(graph.Node{ID: uri, Label: fileName, Kind: "file", URI: uri, FileName: fileName}) {
			fileCount++
		}
		return uri
	}
	addEdge := func(edge graph.Edge) {
		payload.AddEdge(edge)
	}
	analysisTypesByURI := map[string]vbGraphAnalysisTypes{}
	analysisTypesFor := func(parsed *core.ParsedDocument) *vbGraphAnalysisTypes {
		if !includeAnalysisTypeDetails || parsed == nil {
			return nil
		}
		if details, ok := analysisTypesByURI[parsed.URI]; ok {
			return &details
		}
		s.logDebugSummary("[asp-lsp] graphVbIndex.extendTypeHints: " + parsed.URI)
		details := graphAnalysisTypes(parsed)
		analysisTypesByURI[parsed.URI] = details
		return &details
	}
	addDeclarationNode := func(parsed *core.ParsedDocument, declaration vbUsageDeclaration) {
		if parsed == nil || declaration.Name == "" {
			return
		}
		id := graphDeclarationNodeID(parsed.URI, declaration.Name, declaration.Range)
		node := graphNodeForVBDeclaration(parsed, declaration, id, analysisTypesFor(parsed))
		if !addNode(node) {
			return
		}
		ownerID := graphDeclarationOwnerNodeID(parsed, declaration)
		if ownerID == "" {
			ownerID = parsed.URI
		}
		addEdge(graph.Edge{ID: id, Source: id, Target: ownerID, Kind: "declares", Label: "declares", Count: 1, Ranges: []lsp.Location{{URI: parsed.URI, Range: declaration.Range}}})
		declarationCount++
	}
	sourceDeclarationNames := map[string]struct{}{}
	for _, parsed := range documents {
		if parsed == nil {
			continue
		}
		for _, declaration := range declarationsFor(parsed) {
			if !declaration.Implicit {
				sourceDeclarationNames[strings.ToLower(declaration.Name)] = struct{}{}
			}
		}
	}

	for index, parsed := range documents {
		if ctx.Err() != nil || !s.graphGenerationCurrent(ctx, generation) {
			return incomplete()
		}
		if parsed == nil {
			if report != nil {
				report("graph."+scope+".addFiles", "", index+1, len(documents))
			}
			continue
		}
		addFileNode(parsed.URI)
		if report != nil {
			report("graph."+scope+".addFiles", progressDetailForURI(parsed.URI), index+1, len(documents))
		}
	}
	for index, parsed := range documents {
		if ctx.Err() != nil || !s.graphGenerationCurrent(ctx, generation) {
			return incomplete()
		}
		if parsed == nil {
			if report != nil {
				report("graph."+scope+".addDeclarations", "", index+1, len(documents))
			}
			continue
		}
		for _, declaration := range declarationsFor(parsed) {
			if declaration.Implicit {
				if _, hasSource := sourceDeclarationNames[strings.ToLower(declaration.Name)]; hasSource {
					continue
				}
			}
			addDeclarationNode(parsed, declaration)
		}
		if report != nil {
			report("graph."+scope+".addDeclarations", progressDetailForURI(parsed.URI), index+1, len(documents))
		}
	}
	for _, node := range s.configuredGraphExternalNodes() {
		addNode(node)
	}
	for documentIndex, parsed := range documents {
		if ctx.Err() != nil || !s.graphGenerationCurrent(ctx, generation) {
			return incomplete()
		}
		if parsed == nil {
			if report != nil {
				report("graph."+scope+".resolveIncludes", "", documentIndex+1, len(documents))
			}
			continue
		}
		for index, include := range parsed.Includes {
			includeCount++
			includeID := parsed.URI + "#include:" + strconv.Itoa(index) + ":" + include.Path
			targetID := includeID
			includeInfo := &graph.IncludeInfo{Path: include.Path, Mode: include.Mode, PathCaseMatches: true}
			details, ok := s.includeTargetDetailsForMode(parsed.URI, include.Path, include.Mode)
			if ok {
				includeInfo.Exists = details.Exists
				includeInfo.PathCaseMatches = !details.CaseMismatch
				if details.Path != "" {
					includeInfo.ResolvedPath = details.Path
					includeInfo.ResolvedURI = filePathURI(details.Path)
					if details.Exists {
						includeInfo.ActualPath = s.graphDisplayFileName(details.Path)
					}
				}
				if details.Exists && includeInfo.ResolvedURI != "" {
					identity := workspacepkg.FileIdentityKeyFromURI(includeInfo.ResolvedURI)
					_, inGraph := documentURIs[identity]
					if inGraph || includeExternalFiles {
						targetID = addFileNode(includeInfo.ResolvedURI)
						includeInfo.ResolvedURI = targetID
					}
				}
			}
			if targetID == includeID {
				exists := false
				kind := "missingInclude"
				if includeInfo.Exists {
					exists = true
					kind = "include"
				} else if payload.Stats != nil {
					payload.Stats["missingIncludes"]++
				}
				addNode(graph.Node{ID: includeID, Label: include.Path, Kind: kind, Exists: &exists})
			}
			label := include.Path
			if strings.EqualFold(include.Mode, "virtual") {
				label = "virtual " + label
			}
			addEdge(graph.Edge{ID: includeID, Source: parsed.URI, Target: targetID, Kind: "include", Label: label, Count: 1, Ranges: []lsp.Location{{URI: parsed.URI, Range: include.Range}}, Include: includeInfo})
		}
		if report != nil {
			report("graph."+scope+".resolveIncludes", progressDetailForURI(parsed.URI), documentIndex+1, len(documents))
		}
	}
	memberNodes, memberEdges := s.graphMemberReferencesWithProgress(ctx, documents, report, "graph."+scope)
	if ctx.Err() != nil || !s.graphGenerationCurrent(ctx, generation) {
		return incomplete()
	}
	for _, node := range memberNodes {
		addNode(node)
	}
	for _, edge := range memberEdges {
		addEdge(edge)
	}
	s.addGraphReferenceLinksWithProgress(ctx, &payload, documents, report, "graph."+scope)
	if ctx.Err() != nil || !s.graphGenerationCurrent(ctx, generation) {
		return incomplete()
	}
	unresolvedNodes, unresolvedEdges := graphUnresolvedNewReferencesWithProgress(ctx, documents, s.settings.VBScriptComTypes, report, "graph."+scope)
	if ctx.Err() != nil || !s.graphGenerationCurrent(ctx, generation) {
		return incomplete()
	}
	for _, node := range unresolvedNodes {
		addNode(node)
	}
	for _, edge := range unresolvedEdges {
		addEdge(edge)
	}
	payload.Stats["files"] = fileCount
	payload.Stats["declarations"] = declarationCount
	payload.Stats["includes"] = includeCount
	payload.Stats["nodes"] = len(payload.Nodes)
	payload.Stats["links"] = len(payload.Links)
	if ctx.Err() != nil || !s.graphGenerationCurrent(ctx, generation) {
		return incomplete()
	}
	return payload, true
}

func vbUsageDeclarationsContain(declarations []vbUsageDeclaration, target vbUsageDeclaration) bool {
	for _, declaration := range declarations {
		if strings.EqualFold(declaration.Name, target.Name) && declaration.Range == target.Range {
			return true
		}
	}
	return false
}

func graphDeclarationNodeID(uri, name string, ranges ...lsp.Range) string {
	id := uri + "#symbol:" + strings.ToLower(name)
	if len(ranges) == 0 {
		return id
	}
	r := ranges[0]
	return id + ":" + strconv.Itoa(r.Start.Line) + ":" + strconv.Itoa(r.Start.Character)
}

func graphVBDeclarations(parsed *core.ParsedDocument) []vbUsageDeclaration {
	if parsed == nil {
		return nil
	}
	var cached []vbUsageDeclaration
	if parsed.LoadAnalysis("lspserver.graph-vb-declarations.v2", &cached) {
		return cached
	}
	seen := map[string]int{}
	declarations := make([]vbUsageDeclaration, 0)
	add := func(declaration vbUsageDeclaration) {
		if declaration.Name == "" {
			return
		}
		if declaration.Implicit {
			declaration.Kind = "variable"
		}
		key := strings.ToLower(declaration.Name) + ":" + strconv.Itoa(declaration.Start) + ":" + strconv.Itoa(declaration.End)
		if index, ok := seen[key]; ok {
			if shouldPreferGraphDeclaration(declaration, declarations[index]) {
				declarations[index] = declaration
			}
			return
		}
		seen[key] = len(declarations)
		declarations = append(declarations, declaration)
	}
	for _, declaration := range collectVBNamingDeclarations(parsed) {
		add(declaration)
	}
	for _, declaration := range variableInlayDeclarations(parsed, true, nil) {
		add(declaration)
	}
	for _, declaration := range graphLoopVariableDeclarations(parsed) {
		add(declaration)
	}
	for _, declaration := range graphReDimDeclarations(parsed) {
		add(declaration)
	}
	for _, declaration := range serverObjectDeclarations(parsed) {
		add(declaration)
	}
	declaredNames := map[string]struct{}{}
	for _, declaration := range declarations {
		declaredNames[strings.ToLower(declaration.Name)] = struct{}{}
	}
	for _, declaration := range graphImplicitGlobalDeclarations(parsed, declaredNames) {
		add(declaration)
	}
	sort.SliceStable(declarations, func(i, j int) bool {
		if declarations[i].Start != declarations[j].Start {
			return declarations[i].Start < declarations[j].Start
		}
		return declarations[i].End < declarations[j].End
	})
	parsed.StoreAnalysis("lspserver.graph-vb-declarations.v2", declarations)
	return declarations
}

func shouldPreferGraphDeclaration(candidate, current vbUsageDeclaration) bool {
	if candidate.MemberOf != current.MemberOf {
		return candidate.MemberOf != ""
	}
	if candidate.Implicit != current.Implicit {
		return !candidate.Implicit
	}
	if candidate.Local != current.Local {
		return candidate.Local
	}
	return current.Kind == "" && candidate.Kind != ""
}

func graphNodeForVBDeclaration(parsed *core.ParsedDocument, declaration vbUsageDeclaration, id string, analysisTypes *vbGraphAnalysisTypes) graph.Node {
	kind := graphDeclarationKind(declaration.Kind)
	scope := "global"
	if declaration.Local {
		scope = "local"
	}
	node := graph.Node{
		ID:              id,
		Label:           graphDeclarationLabel(declaration),
		Kind:            "vbDeclaration",
		URI:             parsed.URI,
		Range:           lspRangePtr(declaration.Range),
		DeclarationKind: kind,
		Group:           kind,
		BindingScope:    scope,
		Implicit:        declaration.Implicit,
		MemberOf:        declaration.MemberOf,
		ProcedureKind:   declaration.ProcedureKind,
		Origin:          "source",
	}
	if sourceRange, ok := graphDeclarationSourceRange(parsed, declaration); ok {
		node.SourceRange = lspRangePtr(sourceRange)
	}
	if declaration.Implicit && !declaration.Local {
		node.ImplicitGlobal = true
		node.ImplicitGlobalCandidate = true
	}
	if graphDeclarationCanHaveType(kind) {
		typeName := graphMemberDeclarationType(parsed, declaration, analysisTypes)
		if !graphShouldHideTypeName(typeName) {
			node.TypeName = typeName
		}
	}
	if analysisTypes != nil {
		node.Parameters = graphParametersForDeclaration(parsed, declaration, analysisTypes)
	}
	if arrayKind, dimensions := graphVBArrayInfo(parsed.Text, declaration.Name); arrayKind != "" {
		node.TypeName = "Array"
		node.ArrayKind = arrayKind
		node.ArrayDimensions = &dimensions
	}
	return node
}

func lspRangePtr(r lsp.Range) *lsp.Range {
	return &r
}

func graphDeclarationLabel(declaration vbUsageDeclaration) string {
	if declaration.MemberOf != "" {
		return declaration.MemberOf + "." + declaration.Name
	}
	return declaration.Name
}

func graphDeclarationOwnerNodeID(parsed *core.ParsedDocument, declaration vbUsageDeclaration) string {
	if parsed == nil {
		return ""
	}
	if declaration.MemberOf != "" {
		for _, candidate := range graphVBDeclarations(parsed) {
			if candidate.Kind == "class" && strings.EqualFold(candidate.Name, declaration.MemberOf) {
				return graphDeclarationNodeID(parsed.URI, candidate.Name, candidate.Range)
			}
		}
	}
	if declaration.Local && declaration.Scope != "" {
		return graphReferenceSourceID(parsed, declaration.Range)
	}
	return ""
}

func graphDeclarationSourceRange(parsed *core.ParsedDocument, declaration vbUsageDeclaration) (lsp.Range, bool) {
	switch graphDeclarationKind(declaration.Kind) {
	case "sub", "function", "method":
		for _, procedure := range graphVBProcedureRanges(parsed) {
			if lspRangeEqual(procedure.nameRange, declaration.Range) {
				return procedure.sourceRange, true
			}
		}
	case "property":
		return graphVBPropertySourceRange(parsed, declaration)
	}
	return lsp.Range{}, false
}

func lspRangeEqual(a, b lsp.Range) bool {
	return a.Start.Line == b.Start.Line &&
		a.Start.Character == b.Start.Character &&
		a.End.Line == b.End.Line &&
		a.End.Character == b.End.Character
}

func graphVBPropertySourceRange(parsed *core.ParsedDocument, declaration vbUsageDeclaration) (lsp.Range, bool) {
	doc := core.NewTextDocument(parsed.URI, "classic-asp", 0, parsed.Text)
	for _, region := range parsed.Regions {
		if region.Language != core.LanguageVBScript {
			continue
		}
		var current *lsp.Range
		for lineStart := region.ContentStart; lineStart < region.ContentEnd; {
			lineEnd := lineStart
			for lineEnd < region.ContentEnd && parsed.Text[lineEnd] != '\n' && parsed.Text[lineEnd] != '\r' {
				lineEnd++
			}
			line := parsed.Text[lineStart:lineEnd]
			trimmed := strings.ToLower(strings.TrimSpace(line))
			if matches := vbPropertyLinePattern.FindStringSubmatchIndex(line); matches != nil {
				start := lineStart + matches[2]
				end := lineStart + matches[3]
				if lspRangeEqual(doc.Range(start, end), declaration.Range) {
					r := doc.Range(lineStart, lineEnd)
					current = &r
				}
			}
			if current != nil {
				current.End = doc.PositionAt(lineEnd)
				if trimmed == "end property" {
					return *current, true
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
	return lsp.Range{}, false
}

func graphDeclarationKind(kind string) string {
	switch strings.ToLower(kind) {
	case "const":
		return "constant"
	case "sub", "function", "class", "property", "parameter", "method", "field", "constant", "variable":
		return strings.ToLower(kind)
	default:
		if kind == "" {
			return "variable"
		}
		return strings.ToLower(kind)
	}
}

func graphDeclarationCanHaveType(kind string) bool {
	switch kind {
	case "variable", "constant", "parameter", "field", "property", "function", "method":
		return true
	default:
		return false
	}
}

type vbGraphAnalysisTypes struct {
	Types            map[string]string
	TypeAnnotations  map[string][]vbTypeAnnotation
	Returns          map[string]string
	Params           map[string]map[string]string
	Members          map[string]map[string]string
	Signatures       map[string]vbscript.Signature
	ScopedReturns    map[string]string
	ScopedParams     map[string]map[string]string
	ScopedSignatures map[string]vbscript.Signature
}

type vbTypeAnnotation struct {
	TypeName string
	Line     int
	Scope    string
	MemberOf string
	Accessor string
}

// graphPropertySignatures returns one signature for each property accessor.
// Property accessors share a user-facing property name, but their parameter
// lists and return contracts are independent procedures.
func graphPropertySignatures(parsed *core.ParsedDocument) []vbscript.Signature {
	if parsed == nil {
		return nil
	}
	doc := core.NewTextDocument(parsed.URI, "classic-asp", 0, parsed.Text)
	signatures := make([]vbscript.Signature, 0)
	for _, region := range parsed.Regions {
		if region.Language != core.LanguageVBScript {
			continue
		}
		var current *vbscript.Signature
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
				paramsText := ""
				if header.HasParameterList {
					paramsText = parsed.Text[header.ParamsStart:header.ParamsEnd]
				}
				visibility := header.Visibility
				prefix := ""
				if visibility != "" {
					prefix = visibility + " "
				}
				accessor := parsed.Text[header.AccessorStart:header.AccessorEnd]
				name := parsed.Text[header.NameStart:header.NameEnd]
				label := prefix + "Property " + accessor + " " + name + "(" + vbProcedureParameterDisplay(paramsText) + ")"
				signatures = append(signatures, vbscript.Signature{
					Name: name, Kind: "property", Label: label,
					Range: doc.Range(lineStart, lineEnd), NameRange: doc.Range(nameStart, nameEnd),
					Parameters: vbPropertyParameters(paramsText),
				})
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
	return signatures
}

func graphSignatures(parsed *core.ParsedDocument) []vbscript.Signature {
	if parsed == nil {
		return nil
	}
	signatures := append([]vbscript.Signature(nil), vbscript.Signatures(parsed)...)
	signatures = append(signatures, graphPropertySignatures(parsed)...)
	sort.SliceStable(signatures, func(i, j int) bool {
		if signatures[i].Range.Start.Line != signatures[j].Range.Start.Line {
			return signatures[i].Range.Start.Line < signatures[j].Range.Start.Line
		}
		if signatures[i].Range.Start.Character != signatures[j].Range.Start.Character {
			return signatures[i].Range.Start.Character < signatures[j].Range.Start.Character
		}
		if signatures[i].NameRange.Start.Character != signatures[j].NameRange.Start.Character {
			return signatures[i].NameRange.Start.Character < signatures[j].NameRange.Start.Character
		}
		return strings.ToLower(signatures[i].Kind) < strings.ToLower(signatures[j].Kind)
	})
	return signatures
}

var (
	graphTypeAnnotationPattern = regexp.MustCompile(`(?i)^@type\s+([A-Za-z_][A-Za-z0-9_]*)\s+As\s+(.+)$`)
)

func graphAnalysisTypes(parsed *core.ParsedDocument) vbGraphAnalysisTypes {
	const analysisKey = "lspserver.graph-analysis-types.v6"
	details := vbGraphAnalysisTypes{
		Types:            map[string]string{},
		TypeAnnotations:  map[string][]vbTypeAnnotation{},
		Returns:          map[string]string{},
		Params:           map[string]map[string]string{},
		Members:          map[string]map[string]string{},
		Signatures:       map[string]vbscript.Signature{},
		ScopedReturns:    map[string]string{},
		ScopedParams:     map[string]map[string]string{},
		ScopedSignatures: map[string]vbscript.Signature{},
	}
	if parsed == nil {
		return details
	}
	if value, ok := parsed.LoadRuntimeAnalysis(analysisKey); ok {
		if cached, ok := value.(vbGraphAnalysisTypes); ok {
			return cached
		}
	}
	if parsed.LoadAnalysis(analysisKey, &details) {
		parsed.StoreRuntimeAnalysis(analysisKey, details)
		return details
	}
	signatures := graphSignatures(parsed)
	hasTypeAnnotations := vbscriptHasGraphTypeAnnotations(parsed)
	for _, signature := range signatures {
		owner := graphSignatureOwner(parsed, signature)
		accessor := graphSignatureAccessorFromLabel(signature)
		if hasTypeAnnotations {
			accessor = graphSignatureAccessor(parsed, signature)
		}
		key := graphSignatureKey(owner, signature.Name, accessor)
		details.ScopedSignatures[key] = signature
		if owner == "" && accessor == "" {
			details.Signatures[strings.ToLower(signature.Name)] = signature
		}
	}
	if !hasTypeAnnotations {
		parsed.StoreRuntimeAnalysis(analysisKey, details)
		parsed.StoreAnalysis(analysisKey, details)
		return details
	}
	procedureScopes := vbProcedureScopes(parsed)
	classOwners := vbClassMemberLineOwners(parsed)
	doc := core.NewTextDocument(parsed.URI, "classic-asp", 0, parsed.Text)
	seenLines := map[int]struct{}{}
	for _, region := range parsed.Regions {
		if region.Language != core.LanguageVBScript {
			continue
		}
		for lineStart := region.ContentStart; lineStart < region.ContentEnd; {
			lineEnd := lineStart
			for lineEnd < region.ContentEnd && parsed.Text[lineEnd] != '\n' && parsed.Text[lineEnd] != '\r' {
				lineEnd++
			}
			lineIndex := doc.PositionAt(lineStart).Line
			line := parsed.Text[lineStart:lineEnd]
			text := strings.TrimSpace(line)
			if _, seen := seenLines[lineIndex]; !seen {
				seenLines[lineIndex] = struct{}{}
				if strings.HasPrefix(text, "'") {
					annotation := strings.TrimSpace(strings.TrimPrefix(text, "'"))
					parsedAnnotation, ok := parseVBScriptTypeAnnotationText(annotation)
					if ok && parsedAnnotation.err == nil {
						switch parsedAnnotation.kind {
						case "type":
							lowerName := strings.ToLower(parsedAnnotation.name)
							scope := vbProcedureScopeAtOffset(procedureScopes, lineStart)
							memberOf := ""
							accessor := ""
							if scope == "" {
								memberOf = strings.ToLower(strings.TrimSpace(classOwners[lineIndex]))
								if memberOf != "" {
									if signature, signatureOwner, found := graphSignatureAfterLine(parsed, signatures, lineIndex, parsedAnnotation.name); found && strings.EqualFold(signatureOwner, memberOf) {
										accessor = graphSignatureAccessor(parsed, signature)
									}
								}
							}
							if scope == "" && memberOf == "" {
								details.Types[lowerName] = parsedAnnotation.typeText
							}
							details.TypeAnnotations[lowerName] = append(details.TypeAnnotations[lowerName], vbTypeAnnotation{TypeName: parsedAnnotation.typeText, Line: lineIndex, Scope: scope, MemberOf: memberOf, Accessor: accessor})
						case "param":
							owner, paramName := graphQualifiedAnnotationName(parsedAnnotation.name)
							signature, signatureOwner, found := graphSignatureAfterLine(parsed, signatures, lineIndex, owner)
							if found {
								scopedKey := graphSignatureKey(signatureOwner, signature.Name, graphSignatureAccessor(parsed, signature))
								if details.ScopedParams[scopedKey] == nil {
									details.ScopedParams[scopedKey] = map[string]string{}
								}
								details.ScopedParams[scopedKey][strings.ToLower(paramName)] = parsedAnnotation.typeText
								if signatureOwner == "" {
									if _, ok := details.Params[owner]; !ok {
										details.Params[owner] = map[string]string{}
									}
									details.Params[owner][strings.ToLower(paramName)] = parsedAnnotation.typeText
								}
								break
							}
							if _, ok := details.Params[owner]; !ok {
								details.Params[owner] = map[string]string{}
							}
							details.Params[owner][strings.ToLower(paramName)] = parsedAnnotation.typeText
						case "member":
							owner, memberName := graphQualifiedAnnotationName(parsedAnnotation.name)
							if owner == "" || memberName == "" {
								break
							}
							if _, ok := details.Members[owner]; !ok {
								details.Members[owner] = map[string]string{}
							}
							details.Members[owner][strings.ToLower(memberName)] = parsedAnnotation.typeText
						case "returns":
							ownerName := strings.ToLower(parsedAnnotation.name)
							signature, signatureOwner, found := graphSignatureAfterLine(parsed, signatures, lineIndex, ownerName)
							if found {
								details.ScopedReturns[graphSignatureKey(signatureOwner, signature.Name, graphSignatureAccessor(parsed, signature))] = parsedAnnotation.typeText
								if signatureOwner == "" {
									details.Returns[strings.ToLower(signature.Name)] = parsedAnnotation.typeText
								}
								break
							}
							if parsedAnnotation.name != "" {
								details.Returns[strings.ToLower(parsedAnnotation.name)] = parsedAnnotation.typeText
							}
						}
					}
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
	parsed.StoreRuntimeAnalysis(analysisKey, details)
	parsed.StoreAnalysis(analysisKey, details)
	return details
}

func vbscriptHasGraphTypeAnnotations(parsed *core.ParsedDocument) bool {
	if parsed == nil {
		return false
	}
	for _, region := range parsed.Regions {
		if region.Language != core.LanguageVBScript {
			continue
		}
		for lineStart := region.ContentStart; lineStart < region.ContentEnd; {
			lineEnd := vbPhysicalLineEnd(parsed.Text, lineStart, region.ContentEnd)
			line := strings.TrimSpace(parsed.Text[lineStart:lineEnd])
			if strings.HasPrefix(line, "'") {
				annotation, ok := parseVBScriptTypeAnnotationText(strings.TrimSpace(strings.TrimPrefix(line, "'")))
				if ok && annotation.err == nil {
					return true
				}
			}
			if lineEnd >= region.ContentEnd {
				break
			}
			lineStart = vbNextLineStart(parsed.Text, lineEnd, region.ContentEnd)
		}
	}
	return false
}

func graphSignatureAccessorFromLabel(signature vbscript.Signature) string {
	if !strings.EqualFold(signature.Kind, "property") {
		return ""
	}
	fields := strings.Fields(signature.Label)
	for index := 0; index+1 < len(fields); index++ {
		if !strings.EqualFold(fields[index], "property") {
			continue
		}
		accessor := strings.ToLower(fields[index+1])
		if accessor == "get" || accessor == "let" || accessor == "set" {
			return accessor
		}
		break
	}
	return ""
}

func graphQualifiedAnnotationName(name string) (string, string) {
	name = strings.TrimSpace(name)
	separator := strings.IndexByte(name, '.')
	if separator <= 0 || separator+1 >= len(name) {
		return "", name
	}
	return strings.ToLower(name[:separator]), name[separator+1:]
}

func graphSignatureKey(owner, name string, accessors ...string) string {
	name = strings.TrimSpace(name)
	if len(accessors) > 0 {
		accessor := strings.ToLower(strings.TrimSpace(accessors[0]))
		if accessor != "" {
			name += "#" + accessor
		}
	}
	return strings.ToLower(strings.TrimSpace(owner)) + "\x00" + strings.ToLower(name)
}

func graphSignatureOwner(parsed *core.ParsedDocument, signature vbscript.Signature) string {
	if parsed == nil {
		return ""
	}
	return strings.ToLower(strings.TrimSpace(vbClassMemberLineOwners(parsed)[signature.Range.Start.Line]))
}

func graphSignatureAccessor(parsed *core.ParsedDocument, signature vbscript.Signature) string {
	if parsed == nil || !strings.EqualFold(signature.Kind, "property") {
		return ""
	}
	doc := core.NewTextDocument(parsed.URI, "classic-asp", 0, parsed.Text)
	nameStart := doc.OffsetAt(signature.NameRange.Start)
	for _, scope := range vbProcedureScopes(parsed) {
		if scope.NameStart == nameStart && scope.Accessor != "" {
			return strings.ToLower(strings.TrimSpace(scope.Accessor))
		}
	}
	return vbPropertyAccessorForLine(lineTextAtOffset(parsed.Text, nameStart))
}

func graphDeclarationAccessor(parsed *core.ParsedDocument, declaration vbUsageDeclaration) string {
	procedureKind := strings.ToLower(strings.TrimSpace(declaration.ProcedureKind))
	for _, accessor := range []string{"get", "let", "set"} {
		if procedureKind == accessor || procedureKind == "property-"+accessor {
			return accessor
		}
	}
	if parsed == nil || !strings.EqualFold(declaration.Kind, "property") {
		return ""
	}
	for _, procedure := range graphVBProcedureRanges(parsed) {
		if lspRangeEqual(procedure.nameRange, declaration.Range) {
			return strings.ToLower(strings.TrimSpace(procedure.accessor))
		}
	}
	return ""
}

func vbProcedureScopeAccessor(scope string) string {
	index := strings.IndexByte(strings.TrimSpace(scope), '#')
	if index < 0 {
		return ""
	}
	return strings.ToLower(strings.TrimSpace(scope[index+1:]))
}

func graphSignatureAfterLine(parsed *core.ParsedDocument, signatures []vbscript.Signature, line int, name string) (vbscript.Signature, string, bool) {
	var best vbscript.Signature
	bestOwner := ""
	found := false
	for _, signature := range signatures {
		if signature.Range.Start.Line <= line || name != "" && !strings.EqualFold(signature.Name, name) {
			continue
		}
		if found && signature.Range.Start.Line >= best.Range.Start.Line {
			continue
		}
		best = signature
		bestOwner = graphSignatureOwner(parsed, signature)
		found = true
	}
	return best, bestOwner, found
}

func vbscriptSignatureForUsageDeclaration(parsed *core.ParsedDocument, declaration vbUsageDeclaration) (vbscript.Signature, bool) {
	if parsed == nil {
		return vbscript.Signature{}, false
	}
	name := vbProcedureScopeName(declaration.Scope)
	if name == "" {
		name = declaration.Name
	}
	owner := declaration.MemberOf
	if owner == "" && declaration.Scope != "" {
		owner = vbClassMemberLineOwners(parsed)[declaration.Line]
	}
	accessor := graphDeclarationAccessor(parsed, declaration)
	if accessor == "" {
		accessor = vbProcedureScopeAccessor(declaration.Scope)
	}
	procedureNameStart := -1
	if scope, ok := vbProcedureScopeForUsageDeclaration(parsed, declaration); ok {
		procedureNameStart = scope.NameStart
	}
	doc := core.NewTextDocument(parsed.URI, "classic-asp", 0, parsed.Text)
	var fallback vbscript.Signature
	for _, signature := range graphSignatures(parsed) {
		if !strings.EqualFold(signature.Name, name) {
			continue
		}
		signatureOwner := graphSignatureOwner(parsed, signature)
		if owner != "" && !strings.EqualFold(owner, signatureOwner) {
			continue
		}
		if owner == "" && signatureOwner != "" {
			continue
		}
		if accessor != "" && !strings.EqualFold(accessor, graphSignatureAccessor(parsed, signature)) {
			continue
		}
		if procedureNameStart >= 0 && doc.OffsetAt(signature.NameRange.Start) != procedureNameStart {
			continue
		}
		if declaration.Line >= signature.Range.Start.Line && declaration.Line <= signature.Range.End.Line {
			return signature, true
		}
		fallback = signature
	}
	if fallback.Name != "" {
		return fallback, true
	}
	signature, ok := vbscript.BuildSignatures(parsed)[strings.ToLower(name)]
	return signature, ok
}

func graphAnalysisTypeForDeclaration(declaration vbUsageDeclaration, kind string, fallback string, details *vbGraphAnalysisTypes) string {
	if details == nil {
		return fallback
	}
	if typeName := graphTypeAnnotationForDeclaration(declaration, details); typeName != "" {
		return typeName
	}
	lowerName := strings.ToLower(declaration.Name)
	switch kind {
	case "function", "property", "method":
		if typeName := graphReturnTypeForDeclaration(declaration, details); typeName != "" {
			return typeName
		}
	default:
		if declaration.Scope == "" && declaration.MemberOf == "" {
			if typeName := details.Types[lowerName]; typeName != "" {
				return typeName
			}
		}
	}
	if fallback != "" && !isLooseVBTypeName(fallback) {
		return fallback
	}
	return fallback
}

func graphTypeAnnotationForDeclaration(declaration vbUsageDeclaration, details *vbGraphAnalysisTypes) string {
	if details == nil {
		return ""
	}
	annotations := details.TypeAnnotations[strings.ToLower(declaration.Name)]
	if len(annotations) == 0 {
		return ""
	}
	bestDistance := -1
	bestType := ""
	for _, annotation := range annotations {
		if !strings.EqualFold(annotation.Scope, declaration.Scope) || annotation.Line >= declaration.Line {
			continue
		}
		if !strings.EqualFold(annotation.MemberOf, declaration.MemberOf) {
			continue
		}
		if annotation.Accessor != "" && !strings.EqualFold(annotation.Accessor, graphDeclarationAccessor(nil, declaration)) {
			continue
		}
		distance := declaration.Line - annotation.Line
		if bestDistance < 0 || distance < bestDistance {
			bestDistance = distance
			bestType = annotation.TypeName
		}
	}
	return strings.TrimSpace(bestType)
}

func graphReturnTypeForDeclaration(declaration vbUsageDeclaration, details *vbGraphAnalysisTypes) string {
	return graphReturnTypeForDeclarationWithParsed(nil, declaration, details)
}

func graphReturnTypeForDeclarationWithParsed(parsed *core.ParsedDocument, declaration vbUsageDeclaration, details *vbGraphAnalysisTypes) string {
	if details == nil {
		return ""
	}
	accessor := graphDeclarationAccessor(parsed, declaration)
	if typeName := details.ScopedReturns[graphSignatureKey(declaration.MemberOf, declaration.Name, accessor)]; typeName != "" {
		return strings.TrimSpace(typeName)
	}
	if accessor != "" {
		return ""
	}
	return strings.TrimSpace(details.Returns[strings.ToLower(declaration.Name)])
}

func graphParameterTypeForDeclaration(parsed *core.ParsedDocument, declaration vbUsageDeclaration, signature vbscript.Signature) string {
	if parsed == nil {
		return ""
	}
	if typeName, found := graphParameterTypeAnnotationForSignature(parsed, signature, declaration.Name); found {
		return typeName
	}
	analysis := graphAnalysisTypes(parsed)
	owner := graphSignatureOwner(parsed, signature)
	accessor := graphSignatureAccessor(parsed, signature)
	if graphHasDuplicateSignatureIdentity(parsed, signature) {
		return ""
	}
	if typeName := analysis.ScopedParams[graphSignatureKey(owner, signature.Name, accessor)][strings.ToLower(declaration.Name)]; typeName != "" {
		return strings.TrimSpace(typeName)
	}
	if typeName := analysis.Params[strings.ToLower(signature.Name)][strings.ToLower(declaration.Name)]; typeName != "" {
		return strings.TrimSpace(typeName)
	}
	return strings.TrimSpace(analysis.Params[""][strings.ToLower(declaration.Name)])
}

func graphParameterTypeAnnotationForSignature(parsed *core.ParsedDocument, signature vbscript.Signature, parameterName string) (string, bool) {
	if parsed == nil || parameterName == "" || signature.Range.Start.Line <= 0 {
		return "", false
	}
	lines := strings.Split(parsed.Text, "\n")
	line := signature.Range.Start.Line
	if line > len(lines) {
		line = len(lines)
	}
	for index := line - 1; index >= 0; index-- {
		trimmed := strings.TrimSpace(strings.TrimRight(lines[index], "\r"))
		if !strings.HasPrefix(trimmed, "'") {
			break
		}
		annotation := strings.TrimSpace(strings.TrimPrefix(trimmed, "'"))
		parsedAnnotation, ok := parseVBScriptTypeAnnotationText(annotation)
		if !ok || parsedAnnotation.err != nil || parsedAnnotation.kind != "param" {
			continue
		}
		owner, name := graphQualifiedAnnotationName(parsedAnnotation.name)
		if !strings.EqualFold(name, parameterName) || owner != "" && !strings.EqualFold(owner, signature.Name) {
			continue
		}
		return strings.TrimSpace(parsedAnnotation.typeText), true
	}
	return "", false
}

func graphHasDuplicateSignatureIdentity(parsed *core.ParsedDocument, target vbscript.Signature) bool {
	if parsed == nil {
		return false
	}
	targetOwner := graphSignatureOwner(parsed, target)
	targetAccessor := graphSignatureAccessor(parsed, target)
	count := 0
	for _, signature := range graphSignatures(parsed) {
		if !strings.EqualFold(signature.Name, target.Name) ||
			!strings.EqualFold(graphSignatureOwner(parsed, signature), targetOwner) ||
			!strings.EqualFold(graphSignatureAccessor(parsed, signature), targetAccessor) {
			continue
		}
		count++
		if count > 1 {
			return true
		}
	}
	return false
}

func graphParametersForDeclaration(parsed *core.ParsedDocument, declaration vbUsageDeclaration, details *vbGraphAnalysisTypes) []graph.Parameter {
	if details == nil {
		return nil
	}
	owner := declaration.MemberOf
	if owner == "" && declaration.Scope != "" {
		owner = vbscriptClassScopeForProcedure(parsed, declaration.Scope)
	}
	accessor := graphDeclarationAccessor(parsed, declaration)
	if accessor == "" {
		accessor = vbProcedureScopeAccessor(declaration.Scope)
	}
	signature, ok := details.ScopedSignatures[graphSignatureKey(owner, declaration.Name, accessor)]
	if !ok {
		signature, ok = details.Signatures[strings.ToLower(declaration.Name)]
	}
	if !ok || len(signature.Parameters) == 0 {
		return nil
	}
	typedParams := details.ScopedParams[graphSignatureKey(owner, signature.Name, graphSignatureAccessor(parsed, signature))]
	legacyParams := details.Params[strings.ToLower(signature.Name)]
	globalParams := details.Params[""]
	parameters := make([]graph.Parameter, 0, len(signature.Parameters))
	for _, parameter := range signature.Parameters {
		item := graph.Parameter{Name: parameter.Name, Mode: strings.ToLower(parameter.Mode), Optional: parameter.Optional}
		if typedParams != nil {
			item.TypeName = typedParams[strings.ToLower(parameter.Name)]
		}
		if item.TypeName == "" && legacyParams != nil {
			item.TypeName = legacyParams[strings.ToLower(parameter.Name)]
		}
		if item.TypeName == "" && globalParams != nil {
			item.TypeName = globalParams[strings.ToLower(parameter.Name)]
		}
		parameters = append(parameters, item)
	}
	return parameters
}

func graphShouldHideTypeName(typeName string) bool {
	switch strings.ToLower(strings.TrimSpace(typeName)) {
	case "regexp":
		return true
	default:
		return false
	}
}

func graphVBArrayInfo(text, name string) (string, []string) {
	if name == "" {
		return "", nil
	}
	dynamicPattern := regexp.MustCompile(`(?i)\bReDim(?:\s+Preserve)?\s+` + regexp.QuoteMeta(name) + `\s*\(([^)]*)\)`)
	if match := dynamicPattern.FindStringSubmatch(text); len(match) == 2 {
		return "dynamic", graphArrayDimensions(match[1])
	}
	fixedPattern := regexp.MustCompile(`(?i)\bDim\b[^\r\n:]*\b` + regexp.QuoteMeta(name) + `\s*\(([^)]*)\)`)
	if match := fixedPattern.FindStringSubmatch(text); len(match) == 2 {
		dimensions := graphArrayDimensions(match[1])
		if len(dimensions) == 0 {
			return "dynamic", []string{}
		}
		return "fixed", dimensions
	}
	return "", nil
}

func graphArrayDimensions(raw string) []string {
	parts := strings.Split(raw, ",")
	dimensions := make([]string, 0, len(parts))
	for _, part := range parts {
		part = strings.TrimSpace(part)
		if part == "" {
			continue
		}
		dimensions = append(dimensions, part)
	}
	return dimensions
}

func graphImplicitGlobalDeclarations(parsed *core.ParsedDocument, declaredNames map[string]struct{}) []vbUsageDeclaration {
	_ = declaredNames
	declarations := make([]vbUsageDeclaration, 0)
	for _, declaration := range implicitVBDeclarations(parsed) {
		if !declaration.Local {
			declarations = append(declarations, declaration)
		}
	}
	return declarations
}

var (
	graphForLoopPattern     = regexp.MustCompile(`(?i)^\s*For\s+([A-Za-z_][A-Za-z0-9_]*)\s*=`)
	graphForEachLoopPattern = regexp.MustCompile(`(?i)^\s*For\s+Each\s+([A-Za-z_][A-Za-z0-9_]*)\s+In\b`)
	graphReDimPattern       = regexp.MustCompile(`(?i)^\s*ReDim(?:\s+Preserve)?\s+([A-Za-z_][A-Za-z0-9_]*)`)
)

func graphLoopVariableDeclarations(parsed *core.ParsedDocument) []vbUsageDeclaration {
	doc := core.NewTextDocument(parsed.URI, "classic-asp", 0, parsed.Text)
	procedureScopes := vbProcedureScopes(parsed)
	var declarations []vbUsageDeclaration
	for _, region := range parsed.Regions {
		if region.Language != core.LanguageVBScript {
			continue
		}
		for lineStart := region.ContentStart; lineStart < region.ContentEnd; {
			lineEnd := lineStart
			for lineEnd < region.ContentEnd && parsed.Text[lineEnd] != '\n' && parsed.Text[lineEnd] != '\r' {
				lineEnd++
			}
			line := parsed.Text[lineStart:lineEnd]
			currentScope := vbProcedureScopeAtOffset(procedureScopes, lineStart)
			inProcedure := currentScope != ""
			if inProcedure {
				for _, pattern := range []*regexp.Regexp{graphForLoopPattern, graphForEachLoopPattern} {
					if matches := pattern.FindStringSubmatchIndex(line); matches != nil {
						start := lineStart + matches[2]
						end := lineStart + matches[3]
						declarations = append(declarations, vbUsageDeclaration{
							Name:  line[matches[2]:matches[3]],
							Kind:  "variable",
							Range: doc.Range(start, end),
							Start: start,
							End:   end,
							Line:  doc.PositionAt(start).Line,
							Local: true,
							Scope: currentScope,
						})
						break
					}
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
	return declarations
}

func graphReDimDeclarations(parsed *core.ParsedDocument) []vbUsageDeclaration {
	doc := core.NewTextDocument(parsed.URI, "classic-asp", 0, parsed.Text)
	procedureScopes := vbProcedureScopes(parsed)
	globalNames := map[string]struct{}{}
	localNamesByScope := map[string]map[string]struct{}{}
	for _, declaration := range collectVBUsageDeclarations(parsed).Declarations {
		if declaration.Name == "" {
			continue
		}
		lower := strings.ToLower(declaration.Name)
		if declaration.Local {
			scope := strings.ToLower(declaration.Scope)
			if _, ok := localNamesByScope[scope]; !ok {
				localNamesByScope[scope] = map[string]struct{}{}
			}
			localNamesByScope[scope][lower] = struct{}{}
			continue
		}
		globalNames[lower] = struct{}{}
	}
	var declarations []vbUsageDeclaration
	for _, region := range parsed.Regions {
		if region.Language != core.LanguageVBScript {
			continue
		}
		for lineStart := region.ContentStart; lineStart < region.ContentEnd; {
			lineEnd := lineStart
			for lineEnd < region.ContentEnd && parsed.Text[lineEnd] != '\n' && parsed.Text[lineEnd] != '\r' {
				lineEnd++
			}
			line := parsed.Text[lineStart:lineEnd]
			currentScope := vbProcedureScopeAtOffset(procedureScopes, lineStart)
			inProcedure := currentScope != ""
			if matches := graphReDimPattern.FindStringSubmatchIndex(line); matches != nil {
				start := lineStart + matches[2]
				end := lineStart + matches[3]
				lower := strings.ToLower(line[matches[2]:matches[3]])
				skipDeclaration := false
				local := inProcedure
				scope := ""
				if inProcedure {
					scope = currentScope
					if _, hasLocal := localNamesByScope[strings.ToLower(currentScope)][lower]; !hasLocal {
						if _, hasGlobal := globalNames[lower]; hasGlobal {
							skipDeclaration = true
						}
					}
				} else if _, hasGlobal := globalNames[lower]; hasGlobal {
					skipDeclaration = true
				}
				if !skipDeclaration {
					declarations = append(declarations, vbUsageDeclaration{
						Name:  line[matches[2]:matches[3]],
						Kind:  "variable",
						Range: doc.Range(start, end),
						Start: start,
						End:   end,
						Line:  doc.PositionAt(start).Line,
						Local: local,
						Scope: scope,
					})
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
	return declarations
}

func isVBIdentifierName(value string) bool {
	if value == "" {
		return false
	}
	if !(value[0] == '_' || value[0] >= 'A' && value[0] <= 'Z' || value[0] >= 'a' && value[0] <= 'z') {
		return false
	}
	for index := 1; index < len(value); index++ {
		ch := value[index]
		if !(ch == '_' || ch >= 'A' && ch <= 'Z' || ch >= 'a' && ch <= 'z' || ch >= '0' && ch <= '9') {
			return false
		}
	}
	return true
}

type graphMemberOccurrence struct {
	URI          string
	Range        lsp.Range
	FullPath     string
	ReceiverName string
	MemberName   string
	Parts        []string
}

var graphMemberChainPattern = regexp.MustCompile(`\b[A-Za-z_][A-Za-z0-9_]*(?:\s*\.\s*[A-Za-z_][A-Za-z0-9_]*)+`)
