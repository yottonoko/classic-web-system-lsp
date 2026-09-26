package lspserver

import (
	"context"
	"regexp"
	"sort"
	"strconv"
	"strings"
	"sync/atomic"

	"github.com/yottonoko/classic-web-system-lsp/internal/core"
	"github.com/yottonoko/classic-web-system-lsp/internal/graph"
	"github.com/yottonoko/classic-web-system-lsp/internal/lsp"
	workspacepkg "github.com/yottonoko/classic-web-system-lsp/internal/workspace"
)

func (s *Server) graphConfiguredGlobalTypes() map[string]string {
	types := map[string]string{}
	for name, value := range s.settings.VBScriptGlobals {
		if !isVBIdentifierName(name) {
			continue
		}
		if typeName := value.typeName(); typeName != "" {
			types[strings.ToLower(name)] = typeName
		}
	}
	return types
}

func (s *Server) configuredGraphExternalNodes() []graph.Node {
	var nodes []graph.Node
	for name, value := range s.settings.VBScriptGlobals {
		if !isVBIdentifierName(name) {
			continue
		}
		typeName := value.typeName()
		if typeName == "" {
			continue
		}
		externalKind := "object"
		declarationKind := "variable"
		if value.Kind == "constant" {
			externalKind = "constant"
			declarationKind = "constant"
		}
		nodes = append(nodes, graph.Node{
			ID:              graphExternalNodeID("configured", externalKind, "configuredGlobal", "", name),
			Label:           name,
			Kind:            "vbDeclaration",
			DeclarationKind: declarationKind,
			Group:           "configuredGlobal",
			Origin:          "configured",
			ExternalKind:    externalKind,
			TypeName:        typeName,
		})
	}
	for typeName, config := range s.settings.VBScriptComTypes {
		typeName = strings.TrimSpace(typeName)
		if typeName == "" {
			continue
		}
		nodes = append(nodes, graph.Node{
			ID:              graphExternalNodeID("configured", "object", "configuredComType", "", typeName),
			Label:           typeName,
			Kind:            "vbDeclaration",
			DeclarationKind: "object",
			Group:           "configuredComType",
			Origin:          "configured",
			ExternalKind:    "object",
			TypeName:        typeName,
		})
		memberNames := make([]string, 0, len(config.Members))
		for memberName := range config.Members {
			memberNames = append(memberNames, memberName)
		}
		sort.Strings(memberNames)
		for _, memberName := range memberNames {
			member := config.Members[memberName]
			nodes = append(nodes, graph.Node{
				ID:              graphExternalNodeID("configured", "member", "configuredComType", typeName, memberName),
				Label:           typeName + "." + memberName,
				Kind:            "vbDeclaration",
				DeclarationKind: member.declarationKind(),
				Group:           "configuredComType",
				Origin:          "configured",
				ExternalKind:    "member",
				MemberOf:        typeName,
				TypeName:        member.typeName(),
				Parameters:      graphParametersForConfiguredMember(member),
			})
		}
	}
	sort.SliceStable(nodes, func(i, j int) bool {
		return nodes[i].ID < nodes[j].ID
	})
	return nodes
}

func (s *Server) configuredComMemberGraphNode(typeName, memberName string) (graph.Node, bool) {
	nodes := s.configuredComMemberGraphNodes(typeName, memberName)
	if len(nodes) == 0 {
		return graph.Node{}, false
	}
	return nodes[0], true
}

type configuredGraphComType struct {
	name   string
	config vbscriptComTypeSetting
}

func (s *Server) configuredGraphComTypes(typeNames []string) ([]configuredGraphComType, bool) {
	if len(typeNames) == 0 {
		return nil, false
	}
	configuredNames := make([]string, 0, len(s.settings.VBScriptComTypes))
	for name := range s.settings.VBScriptComTypes {
		configuredNames = append(configuredNames, name)
	}
	sort.SliceStable(configuredNames, func(i, j int) bool {
		left := strings.ToLower(configuredNames[i])
		right := strings.ToLower(configuredNames[j])
		if left != right {
			return left < right
		}
		return configuredNames[i] < configuredNames[j]
	})

	result := make([]configuredGraphComType, 0, len(typeNames))
	seen := map[string]struct{}{}
	for _, typeName := range typeNames {
		key := strings.ToLower(strings.TrimSpace(typeName))
		if key == "" {
			return nil, false
		}
		if _, ok := seen[key]; ok {
			continue
		}
		seen[key] = struct{}{}
		found := false
		for _, configuredName := range configuredNames {
			if !strings.EqualFold(configuredName, typeName) {
				continue
			}
			result = append(result, configuredGraphComType{name: configuredName, config: s.settings.VBScriptComTypes[configuredName]})
			found = true
			break
		}
		if !found {
			return nil, false
		}
	}
	return result, true
}

func configuredGraphComMembers(config vbscriptComTypeSetting) map[string]vbscriptTypedMember {
	members := make(map[string]vbscriptTypedMember, len(config.Members))
	for memberName, setting := range config.Members {
		name := strings.TrimSpace(memberName)
		if name == "" {
			continue
		}
		members[strings.ToLower(name)] = vbscriptTypedMemberFromComSetting(name, setting)
	}
	return members
}

func (s *Server) configuredComMemberGraphNodes(typeName, memberName string) []graph.Node {
	typeNames := vbscriptConcreteTypeNames(typeName)
	configuredTypes, ok := s.configuredGraphComTypes(typeNames)
	if !ok {
		return nil
	}
	membersByType := make(map[string]map[string]vbscriptTypedMember, len(configuredTypes))
	for index, configuredType := range configuredTypes {
		membersByType[strings.ToLower(typeNames[index])] = configuredGraphComMembers(configuredType.config)
	}
	common, ok := vbscriptCommonTypedMembers(typeNames, membersByType)
	if !ok {
		return nil
	}
	if _, ok := vbscriptTypedMemberMapGet(common, memberName); !ok {
		return nil
	}

	nodes := make([]graph.Node, 0, len(configuredTypes))
	for _, configuredType := range configuredTypes {
		memberNames := make([]string, 0, len(configuredType.config.Members))
		for name := range configuredType.config.Members {
			memberNames = append(memberNames, name)
		}
		sort.SliceStable(memberNames, func(i, j int) bool {
			left := strings.ToLower(memberNames[i])
			right := strings.ToLower(memberNames[j])
			if left != right {
				return left < right
			}
			return memberNames[i] < memberNames[j]
		})
		for _, configuredMemberName := range memberNames {
			if !strings.EqualFold(configuredMemberName, memberName) {
				continue
			}
			member := configuredType.config.Members[configuredMemberName]
			nodes = append(nodes, graph.Node{
				ID:              graphExternalNodeID("configured", "member", "configuredComType", configuredType.name, configuredMemberName),
				Label:           configuredType.name + "." + configuredMemberName,
				Kind:            "vbDeclaration",
				DeclarationKind: member.declarationKind(),
				Group:           "configuredComType",
				Origin:          "configured",
				ExternalKind:    "member",
				MemberOf:        configuredType.name,
				TypeName:        member.declaredTypeName(),
				Parameters:      graphParametersForConfiguredMember(member),
			})
			break
		}
	}
	sort.SliceStable(nodes, func(i, j int) bool {
		return nodes[i].ID < nodes[j].ID
	})
	return nodes
}

func graphParametersForConfiguredMember(member vbscriptComMemberSetting) []graph.Parameter {
	if len(member.Parameters) == 0 {
		return nil
	}
	parameters := make([]graph.Parameter, 0, len(member.Parameters))
	for _, parameter := range member.Parameters {
		parameters = append(parameters, graph.Parameter{
			Name:     strings.TrimSpace(parameter.Name),
			Mode:     parameter.modeName(),
			TypeName: strings.TrimSpace(parameter.Type),
			Optional: parameter.Optional,
		})
	}
	return parameters
}

func graphExternalNodeID(origin, externalKind, category, memberOf, name string) string {
	return strings.Join([]string{
		"external",
		origin,
		externalKind,
		category,
		strings.ToLower(memberOf),
		strings.ToLower(name),
	}, ":")
}

func (s *Server) graphMemberReferencesWithProgress(ctx context.Context, documents []*core.ParsedDocument, report graphProgressReporter, labelPrefix string) ([]graph.Node, []graph.Edge) {
	orderedDocuments := graphMemberDocumentsInStableOrder(documents)
	configuredTypes := s.graphConfiguredGlobalTypes()
	configuredSettings := map[string]vbscriptGlobalSetting{}
	for name, setting := range s.settings.VBScriptGlobals {
		configuredSettings[strings.ToLower(name)] = setting
	}
	includeOwners := graphMemberIncludeOwners(ctx, s, orderedDocuments)
	documentsByIdentity := make(map[string]*core.ParsedDocument, len(orderedDocuments))
	for _, parsed := range orderedDocuments {
		if parsed != nil {
			documentsByIdentity[workspacepkg.FileIdentityKeyFromURI(parsed.URI)] = parsed
		}
	}
	typeContexts := map[string]graphMemberTypeContext{}
	contextFor := func(parsed *core.ParsedDocument, includeExecution bool) graphMemberTypeContext {
		if parsed == nil {
			return graphMemberTypeContext{}
		}
		key := workspacepkg.FileIdentityKeyFromURI(parsed.URI)
		if cached, ok := typeContexts[key]; ok {
			if includeExecution && !cached.attempted {
				cached = s.graphMemberTypeContext(ctx, parsed, true)
				typeContexts[key] = cached
			}
			return cached
		}
		cached := s.graphMemberTypeContext(ctx, parsed, includeExecution)
		typeContexts[key] = cached
		return cached
	}
	for index, parsed := range orderedDocuments {
		if ctx.Err() != nil {
			return nil, nil
		}
		if parsed == nil {
			if report != nil {
				report(labelPrefix+".indexMembers", "", index+1, len(orderedDocuments))
			}
			continue
		}
		contextFor(parsed, len(parsed.Includes) > 0)
		if report != nil {
			report(labelPrefix+".indexMembers", progressDetailForURI(parsed.URI), index+1, len(orderedDocuments))
		}
	}
	nodeByID := map[string]graph.Node{}
	edgeByKey := map[string]graph.Edge{}
	addNode := func(node graph.Node) {
		if _, ok := nodeByID[node.ID]; ok {
			return
		}
		nodeByID[node.ID] = node
	}
	addEdge := func(source, target, uri string, occurrenceRange lsp.Range) {
		if source == "" || target == "" {
			return
		}
		key := source + "\x00" + target + "\x00member"
		if edge, ok := edgeByKey[key]; ok {
			edge.Count++
			edge.Ranges = append(edge.Ranges, lsp.Location{URI: uri, Range: occurrenceRange})
			edgeByKey[key] = edge
			return
		}
		edgeByKey[key] = graph.Edge{
			ID:     "member:" + strconv.Itoa(len(edgeByKey)),
			Source: source,
			Target: target,
			Kind:   "calls",
			Label:  "member",
			Role:   "member",
			Count:  1,
			Ranges: []lsp.Location{{URI: uri, Range: occurrenceRange}},
		}
	}
	for index, parsed := range orderedDocuments {
		if ctx.Err() != nil {
			return nil, nil
		}
		if parsed == nil {
			if report != nil {
				report(labelPrefix+".linkMembers", "", index+1, len(orderedDocuments))
			}
			continue
		}
		for _, occurrence := range graphMemberOccurrences(parsed) {
			if len(occurrence.Parts) >= 2 {
				receiverName := occurrence.Parts[0]
				memberName := occurrence.Parts[len(occurrence.Parts)-1]
				offset := core.NewTextDocument(parsed.URI, "classic-asp", 0, parsed.Text).OffsetAt(occurrence.Range.Start)
				ownerURIs := includeOwners[workspacepkg.FileIdentityKeyFromURI(parsed.URI)]
				typeNames := make([]string, 0, len(ownerURIs)+1)
				appendTypeName := func(typeName string) {
					if typeName == "" {
						return
					}
					for _, existing := range typeNames {
						if strings.EqualFold(existing, typeName) {
							return
						}
					}
					typeNames = append(typeNames, typeName)
				}
				if len(ownerURIs) > 0 {
					for _, ownerURI := range ownerURIs {
						owner := documentsByIdentity[workspacepkg.FileIdentityKeyFromURI(ownerURI)]
						if owner == nil {
							continue
						}
						typeName, _ := graphMemberTypeAtContext(parsed, offset, receiverName, contextFor(owner, true), configuredTypes, true)
						appendTypeName(typeName)
					}
					// A local declaration belongs to the fragment itself and must
					// remain visible under every owner.
					if len(typeNames) == 0 {
						localType, localBound := graphMemberTypeAtContext(parsed, offset, receiverName, contextFor(parsed, false), configuredTypes, false)
						if localBound {
							appendTypeName(localType)
						}
					}
				} else {
					typeName, _ := graphMemberTypeAtContext(parsed, offset, receiverName, contextFor(parsed, false), configuredTypes, true)
					appendTypeName(typeName)
				}
				for _, typeName := range typeNames {
					if len(vbscriptConcreteTypeNames(typeName)) == 1 {
						if node, ok := s.configuredComMemberGraphNode(typeName, memberName); ok {
							addNode(node)
							addEdge(graphReferenceSourceID(parsed, occurrence.Range), node.ID, parsed.URI, occurrence.Range)
						}
					} else {
						for _, node := range s.configuredComMemberGraphNodes(typeName, memberName) {
							addNode(node)
							addEdge(graphReferenceSourceID(parsed, occurrence.Range), node.ID, parsed.URI, occurrence.Range)
						}
					}
				}
			}
			nodeIDs := make([]string, 0, len(occurrence.Parts)-1)
			for index := 1; index < len(occurrence.Parts); index++ {
				fullPath := strings.Join(occurrence.Parts[:index+1], ".")
				receiver := strings.Join(occurrence.Parts[:index], ".")
				member := occurrence.Parts[index]
				id := graphMemberNodeID(parsed.URI, fullPath)
				nodeIDs = append(nodeIDs, id)
				addNode(graph.Node{
					ID:           id,
					Label:        member,
					Kind:         "vbMemberReference",
					URI:          parsed.URI,
					Role:         "member",
					ReceiverName: receiver,
					MemberName:   member,
					FullPath:     fullPath,
				})
			}
			deepestID := nodeIDs[len(nodeIDs)-1]
			addEdge(graphReferenceSourceID(parsed, occurrence.Range), deepestID, parsed.URI, occurrence.Range)
			for index := len(nodeIDs) - 1; index >= 0; index-- {
				target := ""
				if index == 0 {
					baseTargets := graphMemberDeclarationIDsForOccurrence(parsed, occurrence, includeOwners[workspacepkg.FileIdentityKeyFromURI(parsed.URI)], documentsByIdentity, configuredSettings)
					for _, baseTarget := range baseTargets {
						addEdge(nodeIDs[index], baseTarget, parsed.URI, occurrence.Range)
					}
					continue
				} else {
					target = nodeIDs[index-1]
				}
				addEdge(nodeIDs[index], target, parsed.URI, occurrence.Range)
			}
		}
		if report != nil {
			report(labelPrefix+".linkMembers", progressDetailForURI(parsed.URI), index+1, len(orderedDocuments))
		}
	}
	nodes := make([]graph.Node, 0, len(nodeByID))
	for _, node := range nodeByID {
		nodes = append(nodes, node)
	}
	sort.SliceStable(nodes, func(i, j int) bool {
		return nodes[i].ID < nodes[j].ID
	})
	edges := make([]graph.Edge, 0, len(edgeByKey))
	for _, edge := range edgeByKey {
		edges = append(edges, edge)
	}
	sort.SliceStable(edges, func(i, j int) bool {
		return edges[i].ID < edges[j].ID
	})
	return nodes, edges
}

func graphMemberOccurrences(parsed *core.ParsedDocument) []graphMemberOccurrence {
	var cached []graphMemberOccurrence
	if parsed.LoadAnalysis("lspserver.graph-member-occurrences.v1", &cached) {
		return cached
	}
	doc := core.NewTextDocument(parsed.URI, "classic-asp", 0, parsed.Text)
	occurrences := []graphMemberOccurrence{}
	for _, region := range parsed.Regions {
		if region.Language != core.LanguageVBScript {
			continue
		}
		text := parsed.Text[region.ContentStart:region.ContentEnd]
		for _, match := range graphMemberChainPattern.FindAllStringIndex(text, -1) {
			start := region.ContentStart + match[0]
			end := region.ContentStart + match[1]
			if previousNonSpace(parsed.Text, start) == '.' {
				continue
			}
			raw := parsed.Text[start:end]
			parts := graphMemberPathParts(raw)
			if len(parts) < 2 || isDeclaredVBBuiltinOrKeyword(strings.ToLower(parts[0])) {
				continue
			}
			occurrences = append(occurrences, graphMemberOccurrence{
				URI:          parsed.URI,
				Range:        doc.Range(start, end),
				FullPath:     strings.Join(parts, "."),
				ReceiverName: strings.Join(parts[:len(parts)-1], "."),
				MemberName:   parts[len(parts)-1],
				Parts:        parts,
			})
		}
	}
	parsed.StoreAnalysis("lspserver.graph-member-occurrences.v1", occurrences)
	return occurrences
}

func graphMemberPathParts(raw string) []string {
	segments := strings.Split(raw, ".")
	parts := make([]string, 0, len(segments))
	for _, segment := range segments {
		segment = strings.TrimSpace(segment)
		if segment == "" {
			continue
		}
		parts = append(parts, segment)
	}
	return parts
}

func graphMemberNodeID(uri, fullPath string) string {
	return uri + "#member:" + strings.ToLower(fullPath)
}

type graphMemberTypeContext struct {
	root      *core.ParsedDocument
	analysis  vbGraphAnalysisTypes
	info      vbscriptTypeInfo
	complete  bool
	attempted bool
}

// graphMemberDocumentsInStableOrder keeps member edge IDs and range order
// independent of the order in which a caller collected workspace documents.
func graphMemberDocumentsInStableOrder(documents []*core.ParsedDocument) []*core.ParsedDocument {
	ordered := append([]*core.ParsedDocument(nil), documents...)
	sort.SliceStable(ordered, func(left, right int) bool {
		leftURI, rightURI := "", ""
		if ordered[left] != nil {
			leftURI = strings.ToLower(ordered[left].URI)
		}
		if ordered[right] != nil {
			rightURI = strings.ToLower(ordered[right].URI)
		}
		if leftURI != rightURI {
			return leftURI < rightURI
		}
		if ordered[left] == nil || ordered[right] == nil {
			return ordered[left] == nil
		}
		return ordered[left].Text < ordered[right].Text
	})
	return ordered
}

func (s *Server) graphMemberTypeContext(ctx context.Context, parsed *core.ParsedDocument, includeExecution bool) graphMemberTypeContext {
	result := graphMemberTypeContext{root: parsed}
	if parsed == nil {
		return result
	}
	result.analysis = graphAnalysisTypes(parsed)
	if !includeExecution || ctx == nil || ctx.Err() != nil {
		return result
	}
	result.attempted = true
	info, complete := s.vbscriptTypeInfoContext(ctx, parsed)
	if ctx.Err() != nil {
		return result
	}
	result.info = info
	result.complete = complete
	return result
}

// graphMemberIncludeOwners returns transitive include owners keyed by the
// included document identity. An included fragment can be executed under
// more than one owner, and each owner may provide a different global type
// contract for the same name.
func graphMemberIncludeOwners(ctx context.Context, s *Server, documents []*core.ParsedDocument) map[string][]string {
	direct, _ := navigationIncludeRelationsWithProgress(ctx, s, documents, nil)
	if ctx != nil && ctx.Err() != nil {
		return nil
	}
	byIdentity := make(map[string]*core.ParsedDocument, len(documents))
	for _, document := range documents {
		if document == nil {
			continue
		}
		byIdentity[workspacepkg.FileIdentityKeyFromURI(document.URI)] = document
	}
	parents := make(map[string][]string, len(direct))
	for childKey, parentURIs := range direct {
		for _, parentURI := range parentURIs {
			if parent := byIdentity[workspacepkg.FileIdentityKeyFromURI(parentURI)]; parent != nil {
				parents[childKey] = append(parents[childKey], parent.URI)
			}
		}
		sort.Strings(parents[childKey])
	}
	ancestors := make(map[string][]string, len(parents))
	var visit func(string, map[string]struct{}) []string
	visit = func(identity string, visiting map[string]struct{}) []string {
		if cached, ok := ancestors[identity]; ok {
			return cached
		}
		if _, cycle := visiting[identity]; cycle {
			return nil
		}
		visiting[identity] = struct{}{}
		values := make([]string, 0, len(parents[identity]))
		for _, parentURI := range parents[identity] {
			if !containsStringFold(values, parentURI) {
				values = append(values, parentURI)
			}
			parentKey := workspacepkg.FileIdentityKeyFromURI(parentURI)
			for _, ancestor := range visit(parentKey, visiting) {
				if !containsStringFold(values, ancestor) {
					values = append(values, ancestor)
				}
			}
		}
		delete(visiting, identity)
		sort.SliceStable(values, func(left, right int) bool {
			return strings.ToLower(values[left]) < strings.ToLower(values[right])
		})
		ancestors[identity] = values
		return values
	}
	owners := make(map[string][]string, len(byIdentity))
	for identity := range byIdentity {
		if values := visit(identity, map[string]struct{}{}); len(values) > 0 {
			owners[identity] = values
		}
	}
	return owners
}

func containsStringFold(values []string, value string) bool {
	for _, candidate := range values {
		if strings.EqualFold(candidate, value) {
			return true
		}
	}
	return false
}

func graphMemberDeclarationType(parsed *core.ParsedDocument, declaration vbUsageDeclaration, analysis *vbGraphAnalysisTypes) string {
	if parsed != nil {
		switch graphDeclarationKind(declaration.Kind) {
		case "variable", "constant", "field", "parameter":
			return inferVBDeclarationType(parsed, declaration)
		}
	}
	fallback := strings.TrimSpace(declaration.TypeName)
	if fallback == "" && parsed != nil && (graphDeclarationKind(declaration.Kind) != "property" || graphDeclarationAccessor(parsed, declaration) == "get") {
		fallback = strings.TrimSpace(inferVBDeclarationType(parsed, declaration))
	}
	return graphAnalysisTypeForDeclaration(declaration, graphDeclarationKind(declaration.Kind), fallback, analysis)
}

func graphMemberDeclarationCandidates(parsed *core.ParsedDocument, name string, offset int) (local, classMember, global *vbUsageDeclaration, bound bool) {
	if parsed == nil || name == "" {
		return nil, nil, nil, false
	}
	scope := vbscriptScopeAtOffset(parsed, offset)
	classScope := vbscriptClassScopeForProcedure(parsed, scope)
	var localCandidates, classCandidates, globalCandidates []vbUsageDeclaration
	for _, declaration := range graphVBDeclarations(parsed) {
		if !strings.EqualFold(declaration.Name, name) {
			continue
		}
		if scope != "" && declaration.Local && strings.EqualFold(declaration.Scope, scope) {
			localCandidates = append(localCandidates, declaration)
			continue
		}
		if classScope != "" && !declaration.Local && declaration.Scope == "" && strings.EqualFold(declaration.MemberOf, classScope) {
			classCandidates = append(classCandidates, declaration)
			continue
		}
		if !declaration.Local && declaration.Scope == "" && declaration.MemberOf == "" {
			globalCandidates = append(globalCandidates, declaration)
		}
	}
	selectCandidate := func(candidates []vbUsageDeclaration) *vbUsageDeclaration {
		if len(candidates) == 0 {
			return nil
		}
		selected := candidates[0]
		selectedBefore := selected.Start <= offset
		for index := 1; index < len(candidates); index++ {
			candidate := candidates[index]
			candidateBefore := candidate.Start <= offset
			if candidateBefore && !selectedBefore || candidateBefore == selectedBefore && candidate.Start > selected.Start {
				selected = candidate
				selectedBefore = candidateBefore
			}
		}
		return &selected
	}
	local = selectCandidate(localCandidates)
	classMember = selectCandidate(classCandidates)
	global = selectCandidate(globalCandidates)
	bound = local != nil || classMember != nil
	if !bound && scope != "" {
		bound = vbscriptNameBoundInScope(parsed, name, scope)
	}
	return local, classMember, global, bound
}

func graphMemberTypeAtContext(parsed *core.ParsedDocument, offset int, name string, context graphMemberTypeContext, configured map[string]string, allowGlobal bool) (string, bool) {
	if parsed == nil || name == "" {
		return "", false
	}
	analysis := context.analysis
	local, classMember, global, bound := graphMemberDeclarationCandidates(parsed, name, offset)
	scope := vbscriptScopeAtOffset(parsed, offset)
	if typeName := context.info.scopedVariableTypeNames[vbscriptTypeScopeKey(parsed, scope, name)]; typeName != "" {
		return typeName, true
	}
	if local != nil {
		if typeName := graphMemberDeclarationType(parsed, *local, &analysis); typeName != "" {
			return typeName, true
		}
	}
	if classMember != nil {
		if typeName := graphMemberDeclarationType(parsed, *classMember, &analysis); typeName != "" {
			return typeName, true
		}
	}
	if bound {
		return "", true
	}
	if !allowGlobal {
		return "", false
	}
	if typeName := context.info.scopedVariableTypeNames[vbscriptTypeScopeKey(parsed, "", name)]; typeName != "" {
		return typeName, true
	}
	if global != nil && (context.root == nil || context.root == parsed) {
		// An implicit Variant candidate must not hide a configured global
		// contract. Explicit source declarations still take precedence below.
		if !global.Implicit || configured[strings.ToLower(name)] == "" {
			if typeName := graphMemberDeclarationType(parsed, *global, &analysis); typeName != "" {
				return typeName, true
			}
		}
	}
	if context.root != nil && context.root != parsed {
		rootAnalysis := context.analysis
		if typeName := context.info.scopedVariableTypeNames[vbscriptTypeScopeKey(context.root, "", name)]; typeName != "" {
			return typeName, true
		}
		_, _, rootGlobal, _ := graphMemberDeclarationCandidates(context.root, name, len(context.root.Text))
		if rootGlobal != nil {
			if typeName := graphMemberDeclarationType(context.root, *rootGlobal, &rootAnalysis); typeName != "" {
				return typeName, true
			}
		}
	}
	if typeName := context.info.variableTypes[strings.ToLower(name)]; typeName != "" {
		return typeName, true
	}
	if typeName := configured[strings.ToLower(name)]; typeName != "" {
		return typeName, true
	}
	return "", false
}

func graphMemberDeclarationIDsForOccurrence(parsed *core.ParsedDocument, occurrence graphMemberOccurrence, ownerURIs []string, documentsByIdentity map[string]*core.ParsedDocument, configured map[string]vbscriptGlobalSetting) []string {
	if parsed == nil || len(occurrence.Parts) == 0 {
		return nil
	}
	name := occurrence.Parts[0]
	offset := core.NewTextDocument(parsed.URI, "classic-asp", 0, parsed.Text).OffsetAt(occurrence.Range.Start)
	local, classMember, global, _ := graphMemberDeclarationCandidates(parsed, name, offset)
	declarations := make([]string, 0, len(ownerURIs)+1)
	appendDeclaration := func(document *core.ParsedDocument, declaration *vbUsageDeclaration) {
		if document == nil || declaration == nil {
			return
		}
		id := graphDeclarationNodeID(document.URI, declaration.Name, declaration.Range)
		for _, existing := range declarations {
			if existing == id {
				return
			}
		}
		declarations = append(declarations, id)
	}
	if local != nil {
		appendDeclaration(parsed, local)
		return declarations
	}
	if classMember != nil {
		appendDeclaration(parsed, classMember)
		return declarations
	}
	if global != nil {
		appendDeclaration(parsed, global)
		return declarations
	}
	for _, ownerURI := range ownerURIs {
		owner := documentsByIdentity[workspacepkg.FileIdentityKeyFromURI(ownerURI)]
		if owner == nil {
			continue
		}
		_, _, ownerGlobal, _ := graphMemberDeclarationCandidates(owner, name, len(owner.Text))
		appendDeclaration(owner, ownerGlobal)
	}
	if len(declarations) == 0 {
		setting, ok := configured[strings.ToLower(name)]
		if ok && setting.typeName() != "" {
			externalKind := "object"
			if setting.Kind == "constant" {
				externalKind = "constant"
			}
			declarations = append(declarations, graphExternalNodeID("configured", externalKind, "configuredGlobal", "", name))
		}
	}
	sort.Strings(declarations)
	return declarations
}

type graphImplicitDeclarationEvent struct {
	Offset      int
	IncludeURI  string
	Declaration *vbUsageDeclaration
}

func (s *Server) graphCanonicalImplicitDeclarationIDs(documents []*core.ParsedDocument, documentByURI map[string]*core.ParsedDocument) map[string]string {
	canonical := map[string]string{}
	if len(documents) == 0 {
		return canonical
	}
	visited := map[string]struct{}{}
	visiting := map[string]struct{}{}
	var visit func(*core.ParsedDocument)
	visit = func(parsed *core.ParsedDocument) {
		if parsed == nil {
			return
		}
		if _, ok := visiting[parsed.URI]; ok {
			return
		}
		if _, ok := visited[parsed.URI]; ok {
			return
		}
		visiting[parsed.URI] = struct{}{}
		doc := core.NewTextDocument(parsed.URI, "classic-asp", 0, parsed.Text)
		events := make([]graphImplicitDeclarationEvent, 0, len(parsed.Includes)+4)
		for _, declaration := range s.cachedVBDeclarations(parsed) {
			if !declaration.Implicit {
				continue
			}
			declaration := declaration
			events = append(events, graphImplicitDeclarationEvent{Offset: declaration.Start, Declaration: &declaration})
		}
		for _, include := range parsed.Includes {
			details, ok := s.includeTargetDetailsForMode(parsed.URI, include.Path, include.Mode)
			if !ok || !details.Exists || details.Path == "" {
				continue
			}
			includeURI := filePathURI(details.Path)
			if _, ok := documentByURI[includeURI]; !ok {
				continue
			}
			events = append(events, graphImplicitDeclarationEvent{Offset: doc.OffsetAt(include.Range.Start), IncludeURI: includeURI})
		}
		sort.SliceStable(events, func(i, j int) bool {
			if events[i].Offset != events[j].Offset {
				return events[i].Offset < events[j].Offset
			}
			if events[i].IncludeURI != "" && events[j].IncludeURI == "" {
				return true
			}
			if events[i].IncludeURI == "" && events[j].IncludeURI != "" {
				return false
			}
			return events[i].IncludeURI < events[j].IncludeURI
		})
		for _, event := range events {
			if event.IncludeURI != "" {
				visit(documentByURI[event.IncludeURI])
				continue
			}
			if event.Declaration == nil {
				continue
			}
			lowerName := strings.ToLower(event.Declaration.Name)
			if _, exists := canonical[lowerName]; exists {
				continue
			}
			canonical[lowerName] = graphDeclarationNodeID(parsed.URI, event.Declaration.Name, event.Declaration.Range)
		}
		delete(visiting, parsed.URI)
		visited[parsed.URI] = struct{}{}
	}
	visit(documents[0])
	for _, document := range documents {
		visit(document)
	}
	return canonical
}

type graphReferenceLinkCount struct {
	source string
	target string
	kind   string
	role   string
	count  int
	ranges []lsp.Location
	ranks  map[string]int
}

type graphReferenceTarget struct {
	declaration vbUsageDeclaration
	id          string
	documents   map[string]int
}

type graphReferenceTraversalStats struct {
	Documents         int
	Names             int
	Postings          int
	TargetResolutions int
}

func (s *Server) addGraphReferenceLinksWithProgress(ctx context.Context, payload *graph.Payload, documents []*core.ParsedDocument, report graphProgressReporter, labelPrefix string) {
	s.addGraphReferenceLinksWithProgressAndStats(ctx, payload, documents, report, labelPrefix, nil)
}

func (s *Server) addGraphReferenceLinksWithProgressAndStats(ctx context.Context, payload *graph.Payload, documents []*core.ParsedDocument, report graphProgressReporter, labelPrefix string, stats *graphReferenceTraversalStats) {
	documentByURI := map[string]*core.ParsedDocument{}
	for _, document := range documents {
		if document == nil {
			continue
		}
		documentByURI[document.URI] = document
	}
	canonicalImplicitIDs := s.graphCanonicalImplicitDeclarationIDs(documents, documentByURI)
	analyses := s.graphDocumentAnalysesWithProgress(ctx, documents, report, labelPrefix)
	if ctx.Err() != nil {
		return
	}
	referenceDocumentsByURI := map[string][]*core.ParsedDocument{}
	referenceDocumentsComplete := true
	referenceDocumentsFor := func(parsed *core.ParsedDocument, declaration vbUsageDeclaration) []*core.ParsedDocument {
		if declaration.Implicit {
			return documents
		}
		if parsed == nil {
			return nil
		}
		if cached, ok := referenceDocumentsByURI[parsed.URI]; ok {
			return cached
		}
		referenceDocuments, complete := s.workspaceReferenceDocumentsContextResult(ctx, parsed)
		if !complete || ctx.Err() != nil {
			referenceDocumentsComplete = false
			return nil
		}
		referenceDocumentsByURI[parsed.URI] = referenceDocuments
		return referenceDocuments
	}
	allDocumentRanks := make(map[string]int, len(documents))
	for rank, document := range documents {
		if document == nil {
			continue
		}
		if _, exists := allDocumentRanks[document.URI]; !exists {
			allDocumentRanks[document.URI] = rank
		}
	}
	referenceDocumentRanksByURI := map[string]map[string]int{}
	referenceDocumentRanksFor := func(parsed *core.ParsedDocument, declaration vbUsageDeclaration) map[string]int {
		if declaration.Implicit {
			return allDocumentRanks
		}
		if parsed == nil {
			return nil
		}
		if cached, ok := referenceDocumentRanksByURI[parsed.URI]; ok {
			return cached
		}
		referenceDocuments := referenceDocumentsFor(parsed, declaration)
		documentRanks := make(map[string]int, len(referenceDocuments))
		for rank, document := range referenceDocuments {
			if document == nil {
				continue
			}
			if _, inGraph := documentByURI[document.URI]; !inGraph {
				continue
			}
			if _, exists := documentRanks[document.URI]; !exists {
				documentRanks[document.URI] = rank
			}
		}
		referenceDocumentRanksByURI[parsed.URI] = documentRanks
		return documentRanks
	}
	targetsByName := map[string][]graphReferenceTarget{}
	for _, parsed := range documents {
		if parsed == nil {
			continue
		}
		for _, declaration := range graphVBDeclarations(parsed) {
			targetID := graphDeclarationNodeID(parsed.URI, declaration.Name, declaration.Range)
			if declaration.Implicit {
				canonicalID := canonicalImplicitIDs[strings.ToLower(declaration.Name)]
				if canonicalID != "" && canonicalID != targetID {
					continue
				}
			}
			documentRanks := referenceDocumentRanksFor(parsed, declaration)
			name := strings.ToLower(declaration.Name)
			targetsByName[name] = append(targetsByName[name], graphReferenceTarget{
				declaration: declaration,
				id:          targetID,
				documents:   documentRanks,
			})
		}
	}
	if !referenceDocumentsComplete || ctx.Err() != nil {
		return
	}
	linkGroups := make([]map[string]*graphReferenceLinkCount, len(documents))
	localStats := make([]graphReferenceTraversalStats, len(documents))
	var completed atomic.Int64
	s.analysisWorkers.parallelForBulk(ctx, len(documents), func(workerCtx context.Context, index int) {
		if workerCtx.Err() != nil {
			return
		}
		document := documents[index]
		local := map[string]*graphReferenceLinkCount{}
		if document != nil {
			analysis, ok := analyses[document.URI]
			if !ok || analysis.referenceShard == nil {
				return
			}
			localStats[index].Documents = 1
			for _, name := range analysis.referenceShard.NormalizedNames() {
				targets := targetsByName[name]
				if len(targets) == 0 {
					continue
				}
				localStats[index].Names++
				declarations := analysis.declarationRanges[name]
				for _, posting := range analysis.referenceShard.PostingsFor(name) {
					if workerCtx.Err() != nil {
						return
					}
					localStats[index].Postings++
					if _, declaration := declarations[posting.Range]; declaration {
						continue
					}
					sourceID := graphReferenceSourceIDFromRanges(document.URI, analysis.procedureRanges, posting.Range)
					for targetIndex := range targets {
						target := &targets[targetIndex]
						if _, accepted := target.documents[document.URI]; !accepted {
							continue
						}
						localStats[index].TargetResolutions++
						kind := vbGraphReferenceLinkKindWithSource(document, analysis.sourceDocument, posting.Range, target.declaration.Kind)
						role := vbGraphReferenceRole(kind)
						if sourceID == target.id && kind == "assignments" {
							continue
						}
						key := sourceID + "\x00" + target.id + "\x00" + kind + "\x00" + role
						link := local[key]
						if link == nil {
							link = &graphReferenceLinkCount{source: sourceID, target: target.id, kind: kind, role: role, ranks: target.documents}
							local[key] = link
						}
						link.count++
						link.ranges = append(link.ranges, lsp.Location{URI: document.URI, Range: posting.Range})
					}
				}
			}
		}
		linkGroups[index] = local
		if report != nil {
			detail := ""
			if document != nil {
				detail = progressDetailForURI(document.URI)
			}
			report(labelPrefix+".linkReferences", detail, int(completed.Add(1)), len(documents))
		}
	})
	if stats != nil {
		for _, current := range localStats {
			stats.Documents += current.Documents
			stats.Names += current.Names
			stats.Postings += current.Postings
			stats.TargetResolutions += current.TargetResolutions
		}
	}
	links := map[string]*graphReferenceLinkCount{}
	for _, group := range linkGroups {
		for key, incoming := range group {
			if incoming == nil {
				continue
			}
			link := links[key]
			if link == nil {
				copied := *incoming
				links[key] = &copied
				continue
			}
			link.count += incoming.count
			link.ranges = append(link.ranges, incoming.ranges...)
		}
	}
	keys := make([]string, 0, len(links))
	for key := range links {
		keys = append(keys, key)
	}
	sort.Strings(keys)
	for _, key := range keys {
		link := links[key]
		sort.SliceStable(link.ranges, func(i, j int) bool {
			leftRank, leftOK := link.ranks[link.ranges[i].URI]
			rightRank, rightOK := link.ranks[link.ranges[j].URI]
			if leftOK != rightOK {
				return leftOK
			}
			if leftRank != rightRank {
				return leftRank < rightRank
			}
			if link.ranges[i].URI != link.ranges[j].URI {
				return link.ranges[i].URI < link.ranges[j].URI
			}
			return graphRangeLess(link.ranges[i].Range, link.ranges[j].Range)
		})
		payload.AddEdge(graph.Edge{
			ID:     "reference:" + strconv.Itoa(len(payload.Links)),
			Source: link.source,
			Target: link.target,
			Kind:   link.kind,
			Label:  link.role,
			Role:   link.role,
			Count:  link.count,
			Ranges: link.ranges,
		})
		if payload.Stats != nil {
			payload.Stats[link.kind] += link.count
		}
	}
}

func graphDeclarationRanges(parsed *core.ParsedDocument) map[string]map[lsp.Range]struct{} {
	return vbReferenceRangeIndex(vbReferenceDeclarationFacts(parsed, false))
}

func vbGraphReferenceLinkKind(parsed *core.ParsedDocument, r lsp.Range, declarationKind string) string {
	source := core.NewTextDocument(parsed.URI, "classic-asp", 0, parsed.Text)
	return vbGraphReferenceLinkKindWithSource(parsed, source, r, declarationKind)
}

func vbGraphReferenceLinkKindWithSource(parsed *core.ParsedDocument, source *core.TextDocument, r lsp.Range, declarationKind string) string {
	start := source.OffsetAt(r.Start)
	end := source.OffsetAt(r.End)
	next := nextNonWhitespaceSameLine(parsed.Text, end)
	if next >= 0 && parsed.Text[next] == '=' {
		return "assignments"
	}
	if graphLooksLikeVBReDimAssignment(parsed.Text, start) {
		return "assignments"
	}
	switch graphDeclarationKind(declarationKind) {
	case "sub", "function", "method", "property":
		if graphLooksLikeVBCall(parsed.Text, start, end) {
			return "calls"
		}
	}
	return "references"
}

func graphLooksLikeVBReDimAssignment(text string, start int) bool {
	lineStart := strings.LastIndexAny(text[:start], "\r\n")
	if lineStart < 0 {
		lineStart = 0
	} else {
		lineStart++
	}
	prefix := strings.ToLower(strings.TrimSpace(text[lineStart:start]))
	return prefix == "redim" || prefix == "redim preserve"
}

func vbGraphReferenceRole(kind string) string {
	switch kind {
	case "assignments":
		return "write"
	case "calls":
		return "call"
	default:
		return "read"
	}
}

var graphNewExpressionPattern = regexp.MustCompile(`(?i)\bNew\s+([A-Za-z_][A-Za-z0-9_]*)`)

func graphUnresolvedNewReferencesWithProgress(ctx context.Context, documents []*core.ParsedDocument, comTypes map[string]vbscriptComTypeSetting, report graphProgressReporter, labelPrefix string) ([]graph.Node, []graph.Edge) {
	knownClasses := map[string]struct{}{}
	for typeName := range comTypes {
		knownClasses[strings.ToLower(typeName)] = struct{}{}
	}
	for index, parsed := range documents {
		if ctx.Err() != nil {
			return nil, nil
		}
		if parsed == nil {
			if report != nil {
				report(labelPrefix+".indexTypes", "", index+1, len(documents))
			}
			continue
		}
		for _, declaration := range graphVBDeclarations(parsed) {
			if graphDeclarationKind(declaration.Kind) == "class" {
				knownClasses[strings.ToLower(declaration.Name)] = struct{}{}
			}
		}
		if report != nil {
			report(labelPrefix+".indexTypes", progressDetailForURI(parsed.URI), index+1, len(documents))
		}
	}
	nodeByID := map[string]graph.Node{}
	linkByKey := map[string]graph.Edge{}
	for index, parsed := range documents {
		if ctx.Err() != nil {
			return nil, nil
		}
		if parsed == nil {
			if report != nil {
				report(labelPrefix+".linkUnresolved", "", index+1, len(documents))
			}
			continue
		}
		doc := core.NewTextDocument(parsed.URI, "classic-asp", 0, parsed.Text)
		for _, region := range parsed.Regions {
			if region.Language != core.LanguageVBScript {
				continue
			}
			text := parsed.Text[region.ContentStart:region.ContentEnd]
			for _, match := range graphNewExpressionPattern.FindAllStringSubmatchIndex(text, -1) {
				name := text[match[2]:match[3]]
				lower := strings.ToLower(name)
				if _, known := knownClasses[lower]; known || isDeclaredVBBuiltinOrKeyword(lower) {
					continue
				}
				start := region.ContentStart + match[2]
				end := region.ContentStart + match[3]
				r := doc.Range(start, end)
				nodeID := "unresolved:new:" + lower
				nodeByID[nodeID] = graph.Node{
					ID:    nodeID,
					Label: name,
					Kind:  "vbUnresolved",
					URI:   parsed.URI,
					Range: lspRangePtr(r),
					Role:  "new",
				}
				sourceID := graphReferenceSourceID(parsed, r)
				key := sourceID + "\x00" + nodeID
				if edge, ok := linkByKey[key]; ok {
					edge.Count++
					edge.Ranges = append(edge.Ranges, lsp.Location{URI: parsed.URI, Range: r})
					linkByKey[key] = edge
					continue
				}
				linkByKey[key] = graph.Edge{
					ID:     "unresolvedReference:" + strconv.Itoa(len(linkByKey)),
					Source: sourceID,
					Target: nodeID,
					Kind:   "unresolvedReference",
					Label:  "new",
					Role:   "new",
					Count:  1,
					Ranges: []lsp.Location{{URI: parsed.URI, Range: r}},
				}
			}
		}
		if report != nil {
			report(labelPrefix+".linkUnresolved", progressDetailForURI(parsed.URI), index+1, len(documents))
		}
	}
	nodes := make([]graph.Node, 0, len(nodeByID))
	for _, node := range nodeByID {
		nodes = append(nodes, node)
	}
	sort.SliceStable(nodes, func(i, j int) bool {
		return nodes[i].ID < nodes[j].ID
	})
	edges := make([]graph.Edge, 0, len(linkByKey))
	for _, edge := range linkByKey {
		edges = append(edges, edge)
	}
	sort.SliceStable(edges, func(i, j int) bool {
		return edges[i].ID < edges[j].ID
	})
	return nodes, edges
}

func graphLooksLikeVBCall(text string, start, end int) bool {
	lineStart := strings.LastIndexAny(text[:start], "\r\n")
	if lineStart < 0 {
		lineStart = 0
	} else {
		lineStart++
	}
	lineEnd := end
	for lineEnd < len(text) && text[lineEnd] != '\r' && text[lineEnd] != '\n' {
		lineEnd++
	}
	line := strings.TrimSpace(text[lineStart:lineEnd])
	if line == "" || strings.HasPrefix(line, "'") {
		return false
	}
	name := text[start:end]
	lowerLine := strings.ToLower(line)
	lowerName := strings.ToLower(name)
	if strings.HasPrefix(lowerLine, "call ") {
		rest := strings.TrimSpace(line[len("call "):])
		return strings.HasPrefix(strings.ToLower(rest), lowerName)
	}
	firstEnd := readVBIdentifier(line, 0)
	if firstEnd > 0 && strings.EqualFold(line[:firstEnd], name) {
		rest := strings.TrimSpace(line[firstEnd:])
		return !strings.HasPrefix(rest, "=")
	}
	next := nextNonWhitespaceSameLine(text, end)
	return next >= 0 && text[next] == '('
}

func graphReferenceSourceID(parsed *core.ParsedDocument, r lsp.Range) string {
	return graphReferenceSourceIDFromRanges(parsed.URI, graphVBProcedureRanges(parsed), r)
}

func graphReferenceSourceIDFromRanges(uri string, procedures []graphVBProcedureRange, r lsp.Range) string {
	for _, procedure := range procedures {
		if r.Start.Line >= procedure.startLine && r.Start.Line <= procedure.endLine {
			return graphDeclarationNodeID(uri, procedure.name, procedure.nameRange)
		}
	}
	return uri
}

func graphRangeLess(left, right lsp.Range) bool {
	if left.Start.Line != right.Start.Line {
		return left.Start.Line < right.Start.Line
	}
	if left.Start.Character != right.Start.Character {
		return left.Start.Character < right.Start.Character
	}
	if left.End.Line != right.End.Line {
		return left.End.Line < right.End.Line
	}
	return left.End.Character < right.End.Character
}

type graphVBProcedureRange struct {
	name        string
	owner       string
	accessor    string
	nameRange   lsp.Range
	sourceRange lsp.Range
	startLine   int
	endLine     int
}

func graphVBProcedureRanges(parsed *core.ParsedDocument) []graphVBProcedureRange {
	if ranges := graphVBProcedureRangesFromCST(parsed); len(ranges) > 0 {
		return ranges
	}
	doc := core.NewTextDocument(parsed.URI, "classic-asp", 0, parsed.Text)
	var ranges []graphVBProcedureRange
	for _, region := range parsed.Regions {
		if region.Language != core.LanguageVBScript {
			continue
		}
		var current *graphVBProcedureRange
		for lineStart := region.ContentStart; lineStart < region.ContentEnd; {
			lineEnd := lineStart
			for lineEnd < region.ContentEnd && parsed.Text[lineEnd] != '\n' && parsed.Text[lineEnd] != '\r' {
				lineEnd++
			}
			line := parsed.Text[lineStart:lineEnd]
			trimmed := strings.ToLower(strings.TrimSpace(line))
			if header, ok := vbProcedureHeaderAtLine(line); ok {
				start := lineStart + header.NameStart
				end := lineStart + header.NameEnd
				r := doc.Range(start, end)
				ranges = append(ranges, graphVBProcedureRange{
					name:        line[header.NameStart:header.NameEnd],
					accessor:    header.Accessor,
					nameRange:   r,
					sourceRange: doc.Range(lineStart, lineEnd),
					startLine:   r.Start.Line,
					endLine:     r.Start.Line,
				})
				current = &ranges[len(ranges)-1]
			}
			if current != nil {
				current.endLine = doc.PositionAt(lineStart).Line
				current.sourceRange.End = doc.PositionAt(lineEnd)
			}
			if trimmed == "end sub" || trimmed == "end function" || trimmed == "end property" {
				current = nil
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
	owners := vbClassMemberLineOwners(parsed)
	for index := range ranges {
		ranges[index].owner = strings.ToLower(owners[ranges[index].startLine])
	}
	return ranges
}

func graphVBProcedureRangesFromCST(parsed *core.ParsedDocument) []graphVBProcedureRange {
	if parsed == nil {
		return nil
	}
	scopes := vbProcedureScopesFromCST(parsed)
	if len(scopes) == 0 {
		return nil
	}
	doc := core.NewTextDocument(parsed.URI, "classic-asp", 0, parsed.Text)
	owners := vbClassMemberLineOwners(parsed)
	ranges := make([]graphVBProcedureRange, 0, len(scopes))
	for _, scope := range scopes {
		if scope.NameStart < 0 || scope.NameEnd < scope.NameStart || scope.StartOffset < 0 || scope.EndOffset < scope.StartOffset {
			continue
		}
		start := min(scope.StartOffset, len(parsed.Text))
		end := min(scope.EndOffset, len(parsed.Text))
		nameStart := min(scope.NameStart, len(parsed.Text))
		nameEnd := min(scope.NameEnd, len(parsed.Text))
		if nameEnd < nameStart || nameStart < start || nameEnd > end {
			continue
		}
		owner := strings.ToLower(scope.Owner)
		if owner == "" {
			owner = strings.ToLower(owners[scope.StartLine])
		}
		nameRange := doc.Range(nameStart, nameEnd)
		ranges = append(ranges, graphVBProcedureRange{
			name:        parsed.Text[nameStart:nameEnd],
			owner:       owner,
			accessor:    scope.Accessor,
			nameRange:   nameRange,
			sourceRange: doc.Range(start, end),
			startLine:   doc.PositionAt(start).Line,
			endLine:     doc.PositionAt(end).Line,
		})
	}
	return ranges
}

func (procedure graphVBProcedureRange) key() string {
	key := vbProcedureScopeKey(procedure.owner, procedure.name)
	if procedure.accessor != "" {
		key += "#" + strings.ToLower(strings.TrimSpace(procedure.accessor))
	}
	return key
}

func nextNonWhitespaceSameLine(text string, offset int) int {
	for offset < len(text) {
		switch text[offset] {
		case ' ', '\t':
			offset++
			continue
		case '\r', '\n':
			return -1
		default:
			return offset
		}
	}
	return -1
}
