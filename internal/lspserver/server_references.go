package lspserver

import (
	"context"
	"path/filepath"
	"strconv"
	"strings"
	"sync"
	"time"

	"github.com/yottonoko/classic-web-system-lsp/internal/core"
	"github.com/yottonoko/classic-web-system-lsp/internal/lsp"
	"github.com/yottonoko/classic-web-system-lsp/internal/vbscript"
	workspacepkg "github.com/yottonoko/classic-web-system-lsp/internal/workspace"
)

const workspaceReferenceColdIndexWaitBudget = 200 * time.Millisecond

func (s *Server) workspaceVBScriptReferences(ctx context.Context, parsed *core.ParsedDocument, position lsp.Position, includeDeclaration bool, symbolKind string) []lsp.Location {
	return s.workspaceVBScriptReferencesWithTestDelay(ctx, parsed, position, includeDeclaration, symbolKind, true)
}

func (s *Server) workspaceVBScriptReferencesWithTestDelay(ctx context.Context, parsed *core.ParsedDocument, position lsp.Position, includeDeclaration bool, symbolKind string, applyTestDelay bool) []lsp.Location {
	if ctx == nil {
		ctx = context.Background()
	}
	s.mu.Lock()
	indexReady := s.workspaceReferenceIndexReadyLocked()
	s.mu.Unlock()
	if !indexReady {
		waitCtx, cancel := context.WithTimeout(ctx, workspaceReferenceColdIndexWaitBudget)
		indexReady = s.waitForCompleteWorkspaceReferenceIndex(waitCtx)
		cancel()
		if ctx.Err() != nil {
			return nil
		}
	}
	if !indexReady {
		return s.workspaceVBScriptReferencesCurrentDocument(ctx, parsed, position, includeDeclaration)
	}
	for attempt := 0; attempt < 3; attempt++ {
		if !s.waitForCompleteWorkspaceReferenceIndex(ctx) {
			return nil
		}
		if attempt > 0 {
			currentParsed, current := s.currentWorkspaceReferenceParsed(parsed)
			if !current {
				return nil
			}
			parsed = currentParsed
		}
		locations, stale := s.workspaceVBScriptReferencesOnce(ctx, parsed, position, includeDeclaration, symbolKind, applyTestDelay && attempt == 0, nil, true)
		if ctx.Err() != nil {
			return nil
		}
		if !stale {
			return locations
		}
	}
	return nil
}

// workspaceVBScriptReferencesCurrentDocument resolves a cold-index request
// without consulting workspace topology or publishing workspace reference
// state. The VBScript resolver already carries the document-local binding,
// shadowing, function-return, and XML cref rules needed by the references
// provider.
func (s *Server) workspaceVBScriptReferencesCurrentDocument(ctx context.Context, parsed *core.ParsedDocument, position lsp.Position, includeDeclaration bool) []lsp.Location {
	if ctx == nil {
		ctx = context.Background()
	}
	if parsed == nil || ctx.Err() != nil {
		return nil
	}
	doc := core.SourceDocument(parsed)
	offset := doc.OffsetAt(position)
	name := vbscript.WordAt(parsed.Text, offset)
	if name == "" || ctx.Err() != nil {
		return nil
	}

	implicitTarget, hasImplicitTarget := implicitGlobalDeclarationAt(parsed, name, position)
	locations := vbscript.ReferencesWithOptions(parsed, position, vbscript.ReferenceOptions{
		IncludeDeclaration:               includeDeclaration,
		IncludeFunctionReturnAssignments: true,
	})
	if ctx.Err() != nil {
		return nil
	}
	if hasImplicitTarget && !includeDeclaration {
		filtered := locations[:0]
		for _, location := range locations {
			if workspacepkg.SameFileIdentityURI(location.URI, parsed.URI) && location.Range == implicitTarget.Range {
				continue
			}
			filtered = append(filtered, location)
		}
		locations = filtered
	}

	if progress := workspaceReferenceProgressFromContext(ctx); progress != nil {
		progress(0, 1, "")
		if ctx.Err() != nil {
			return nil
		}
		progress(1, 1, parsed.URI)
	}
	if ctx.Err() != nil {
		return nil
	}
	emitWorkspaceReferenceLocations(ctx, locations)
	return locations
}

func (s *Server) currentWorkspaceReferenceParsed(previous *core.ParsedDocument) (*core.ParsedDocument, bool) {
	if previous == nil {
		return nil, false
	}
	_, current := s.parsed(previous.URI)
	return current, current != nil && current.Text == previous.Text
}

func (s *Server) workspaceVBScriptReferencesOnce(ctx context.Context, parsed *core.ParsedDocument, position lsp.Position, includeDeclaration bool, symbolKind string, applyTestDelay bool, candidateDocuments []*core.ParsedDocument, publishResult bool) ([]lsp.Location, bool) {
	doc := core.SourceDocument(parsed)
	offset := doc.OffsetAt(position)
	name := vbscript.WordAt(parsed.Text, offset)
	if name == "" {
		return nil, false
	}
	if symbolKind == "" {
		symbolKind = vbReferenceSymbolKindAt(parsed, name)
	}
	position = s.workspaceReferenceCachePositionContext(ctx, parsed, name, symbolKind, position)
	s.mu.Lock()
	generation := s.referenceGeneration
	requestKey := workspaceReferenceRequestKey(parsed.URI, position, includeDeclaration, symbolKind, generation, name)
	if cached, ok := s.referenceResults[requestKey]; publishResult && ok {
		locations := cached
		s.mu.Unlock()
		emitWorkspaceReferenceLocations(ctx, locations)
		s.logDebugSummaryEvent("referenceCache", "[asp-lsp] referenceCache.hit vb.references.workspace.reuse uri="+parsed.URI+", symbol="+name+", references="+strconv.Itoa(len(locations)), map[string]any{
			"generation": generation, "references": len(locations), "symbol": name, "uri": parsed.URI,
		})
		if progress := workspaceReferenceProgressFromContext(ctx); progress != nil {
			progress(1, 1, parsed.URI)
		}
		return locations, false
	}
	if inflight := s.referenceInflight[requestKey]; publishResult && inflight != nil {
		s.mu.Unlock()
		select {
		case <-inflight.done:
		case <-ctx.Done():
			return nil, false
		}
		locations := inflight.locations
		emitWorkspaceReferenceLocations(ctx, locations)
		s.logDebugSummaryEvent("referenceCache", "[asp-lsp] referenceCache.inflight.join vb.references.workspace.reuse uri="+parsed.URI+", symbol="+name+", references="+strconv.Itoa(len(locations)), map[string]any{
			"generation": generation, "references": len(locations), "symbol": name, "uri": parsed.URI,
		})
		if progress := workspaceReferenceProgressFromContext(ctx); progress != nil {
			progress(1, 1, parsed.URI)
		}
		return locations, inflight.stale
	}
	nameRevision := s.referenceNameRevisions[strings.ToLower(name)]
	inflight := &workspaceReferenceInflight{done: make(chan struct{}), generation: generation, nameRevision: nameRevision}
	if publishResult {
		s.storeWorkspaceReferenceInflightLocked(requestKey, inflight)
	}
	s.mu.Unlock()
	// Every preparation exit must release callers sharing this query, including
	// cancellation before any segments have been materialized.
	defer func() {
		s.mu.Lock()
		defer s.mu.Unlock()
		select {
		case <-inflight.done:
			return
		default:
		}
		inflight.stale = true
		if current := s.referenceInflight[requestKey]; current == inflight {
			s.deleteWorkspaceReferenceTargetKindLocked(requestKey, workspaceReferenceInflightCache)
		}
		close(inflight.done)
	}()
	started := time.Now()
	delay := time.Duration(0)
	if applyTestDelay {
		delay = vbReferencesBatchDelay()
	}
	if delay > 0 {
		s.logDebugSummary("[asp-lsp] vb.references.workspace.candidates: " + parsed.URI + ", symbol=" + name)
		timer := time.NewTimer(delay)
		select {
		case <-timer.C:
		case <-ctx.Done():
			timer.Stop()
			s.mu.Lock()
			inflight.stale = true
			if current := s.referenceInflight[requestKey]; current == inflight {
				s.deleteWorkspaceReferenceTargetKindLocked(requestKey, workspaceReferenceInflightCache)
			}
			close(inflight.done)
			s.mu.Unlock()
			return nil, false
		}
		s.indexWorkspaceForReferenceTest()
		s.logDebugSummary("[asp-lsp] vb.references.worker.stale: " + parsed.URI + ", symbol=" + name)
	}
	implicitTarget, hasImplicitTarget := implicitGlobalDeclarationAt(parsed, name, position)
	implicitReferenceDocuments := map[string]struct{}{}
	if hasImplicitTarget {
		var complete bool
		implicitReferenceDocuments, complete = s.implicitGlobalReferenceDocumentKeysContext(ctx, parsed, implicitTarget)
		if !complete || ctx.Err() != nil {
			return nil, false
		}
	}
	documents := candidateDocuments
	if documents == nil {
		var complete bool
		documents, complete = s.workspaceReferenceDocumentsContextResult(ctx, parsed)
		if !complete || ctx.Err() != nil {
			return nil, false
		}
	}
	segments := s.referenceWorkspaceIndex.segmentsForNameContext(ctx, name, documents)
	segments = s.hydrateWorkspaceReferenceSegments(segments)
	progress := workspaceReferenceProgressFromContext(ctx)
	if progress != nil {
		progress(0, len(segments), "")
	}
	parsedKey := workspacepkg.FileIdentityKeyFromURI(parsed.URI)
	var implicitTargetPointer *vbUsageDeclaration
	if hasImplicitTarget {
		implicitTargetPointer = &implicitTarget
	}
	locations, shadowed := s.materializeWorkspaceReferenceSegments(ctx, segments, workspaceReferenceLocationPlan{
		originURI: parsed.URI, originDocumentKey: parsedKey, symbolKind: symbolKind,
		includeDeclaration: includeDeclaration, implicitTarget: implicitTargetPointer,
		implicitDocumentKeys: implicitReferenceDocuments,
		unqualifiedTarget:    vbReferenceTargetIsUnqualified(parsed, name, position),
	}, progress)
	if shadowed > 0 {
		s.logDebugSummaryEvent("referenceCache.shadowed", "[asp-lsp] vb.references.workspace.shadowed symbol="+name+", documents="+strconv.FormatInt(shadowed, 10), map[string]any{
			"documents": shadowed, "symbol": name,
		})
	}
	s.mu.Lock()
	cacheable := publishResult && ctx.Err() == nil && s.referenceGeneration == generation && s.referenceNameRevisions[strings.ToLower(name)] == nameRevision
	if cacheable {
		s.storeWorkspaceReferenceResultLocked(requestKey, locations)
		s.scheduleMemoryPressureCheckLocked("workspaceReferences.store")
		s.storeWorkspaceReferenceCountLocked(requestKey, len(locations))
	}
	inflight.locations = locations
	inflight.stale = !cacheable
	if publishResult {
		if current := s.referenceInflight[requestKey]; current == inflight {
			s.deleteWorkspaceReferenceTargetKindLocked(requestKey, workspaceReferenceInflightCache)
		}
	}
	close(inflight.done)
	s.mu.Unlock()
	if cacheable && !includeDeclaration {
		s.requestCodeLensRefresh("references.workspace.complete")
	}
	status := "computed"
	if ctx.Err() != nil {
		status = "cancelled"
	} else if publishResult && !cacheable {
		status = "stale"
	} else if !publishResult {
		status = "partial"
	}
	s.logDebugSummaryEvent("referenceCache", "[asp-lsp] referenceCache."+status+" uri="+parsed.URI+", symbol="+name+", references="+strconv.Itoa(len(locations))+" "+formatElapsedSince(started), map[string]any{
		"durationMs": float64(time.Since(started).Microseconds()) / 1000, "generation": generation, "references": len(locations), "symbol": name, "uri": parsed.URI,
	})
	return locations, publishResult && !cacheable
}

func vbReferenceTargetIsUnqualified(parsed *core.ParsedDocument, name string, position lsp.Position) bool {
	if parsed != nil {
		shard := vbscript.BuildReferenceShard(parsed)
		postings := shard.PostingsFor(name)
		resolutions := shard.GlobalResolutionsFor(name)
		if len(resolutions) != len(postings) {
			resolutions = vbscript.GlobalReferenceResolutions(postings)
		}
		for index, posting := range postings {
			if !lspPositionInRange(position, posting.Range) {
				continue
			}
			return resolutions[index]
		}
	}
	for _, declaration := range normalizedVBUsageDeclarations(parsed) {
		if strings.EqualFold(declaration.Name, name) && lspPositionInRange(position, declaration.Range) {
			return !declaration.Local && declaration.MemberOf == ""
		}
	}
	return true
}

func (s *Server) workspaceReferenceCachePosition(parsed *core.ParsedDocument, name, symbolKind string, fallback lsp.Position) lsp.Position {
	return s.workspaceReferenceCachePositionContext(context.Background(), parsed, name, symbolKind, fallback)
}

func (s *Server) workspaceReferenceCachePositionContext(ctx context.Context, parsed *core.ParsedDocument, name, symbolKind string, fallback lsp.Position) lsp.Position {
	declarations := s.vbscriptReferenceCodeLensDeclarationsContext(ctx, parsed)
	matches := make([]vbUsageDeclaration, 0, 1)
	for _, declaration := range declarations {
		if !strings.EqualFold(declaration.Name, name) || declaration.Kind != symbolKind {
			continue
		}
		if lspPositionInRange(fallback, declaration.Range) {
			return declaration.Range.Start
		}
		matches = append(matches, declaration)
	}
	if len(matches) == 1 {
		return matches[0].Range.Start
	}
	return fallback
}

func implicitGlobalDeclarationAt(parsed *core.ParsedDocument, name string, position lsp.Position) (vbUsageDeclaration, bool) {
	if parsed == nil || name == "" {
		return vbUsageDeclaration{}, false
	}
	for _, declaration := range graphVBDeclarations(parsed) {
		if !declaration.Implicit || !strings.EqualFold(declaration.Name, name) {
			continue
		}
		if lspPositionInRange(position, declaration.Range) {
			return declaration, true
		}
	}
	return vbUsageDeclaration{}, false
}

func lspPositionInRange(position lsp.Position, r lsp.Range) bool {
	return compareLSPPositions(position, r.Start) >= 0 && compareLSPPositions(position, r.End) <= 0
}

func (s *Server) implicitGlobalReferenceDocumentKeys(parsed *core.ParsedDocument, declaration vbUsageDeclaration) map[string]struct{} {
	keys, _ := s.implicitGlobalReferenceDocumentKeysContext(context.Background(), parsed, declaration)
	return keys
}

func (s *Server) implicitGlobalReferenceDocumentKeysContext(ctx context.Context, parsed *core.ParsedDocument, declaration vbUsageDeclaration) (map[string]struct{}, bool) {
	if parsed == nil || !declaration.Implicit {
		return map[string]struct{}{}, true
	}
	if ctx == nil {
		ctx = context.Background()
	}
	if ctx.Err() != nil {
		return nil, false
	}
	targetID := graphDeclarationNodeID(parsed.URI, declaration.Name, declaration.Range)
	plans, ok := s.implicitGlobalReferencePlans(parsed)
	if ok {
		if accepted := plans[targetID]; accepted != nil {
			return accepted, true
		}
		return map[string]struct{}{}, true
	}
	return s.legacyImplicitGlobalReferenceDocumentKeysContext(ctx, parsed, declaration)
}

func (s *Server) implicitGlobalReferencePlans(parsed *core.ParsedDocument) (map[string]map[string]struct{}, bool) {
	if parsed == nil {
		return nil, false
	}
	targetPath := cleanFileURIPath(parsed.URI)
	if targetPath == "" {
		return nil, false
	}
	s.mu.Lock()
	if s.workspaceIncludeGraph == nil || s.workspaceIncludeGraph.Size() == 0 || !s.workspaceIncludeGraphComplete || s.rootPath == "" {
		s.mu.Unlock()
		return nil, false
	}
	graphRevision := s.workspaceIncludeGraphRevision
	indexRevision := s.referenceWorkspaceIndex.revisionNumber()
	key := workspaceReferenceImplicitPlanKey{
		DocumentKey:   workspacepkg.FileIdentityKeyFromFileName(targetPath),
		GraphRevision: graphRevision,
		IndexRevision: indexRevision,
	}
	if cached := s.referenceImplicitPlans[key]; cached != nil {
		plans := cloneWorkspaceReferenceImplicitPlan(cached)
		s.mu.Unlock()
		return plans, true
	}
	roots := s.workspaceIncludeGraph.ReverseClosure([]string{targetPath}).FileNames
	trees := make([][]string, 0, len(roots))
	for _, root := range roots {
		trees = append(trees, append([]string(nil), s.workspaceIncludeGraph.ForwardClosure([]string{root}).FileNames...))
	}
	s.mu.Unlock()

	documentsByKey := map[string]*core.ParsedDocument{
		workspacepkg.FileIdentityKeyFromURI(parsed.URI): parsed,
	}
	implicitDeclarations := make([]vbUsageDeclaration, 0)
	for _, candidate := range graphVBDeclarations(parsed) {
		if candidate.Implicit {
			implicitDeclarations = append(implicitDeclarations, candidate)
		}
	}
	plans := make(map[string]map[string]struct{}, len(implicitDeclarations))
	for _, declaration := range implicitDeclarations {
		plans[graphDeclarationNodeID(parsed.URI, declaration.Name, declaration.Range)] = map[string]struct{}{}
	}
	for _, treeFileNames := range trees {
		documents := make([]*core.ParsedDocument, 0, len(treeFileNames))
		documentByURI := make(map[string]*core.ParsedDocument, len(treeFileNames))
		for _, fileName := range treeFileNames {
			documentKey := workspacepkg.FileIdentityKeyFromFileName(fileName)
			document := documentsByKey[documentKey]
			if document == nil {
				document = s.parsedIncludeFile(fileName)
				if document == nil {
					continue
				}
				documentsByKey[documentKey] = document
			}
			documents = append(documents, document)
			documentByURI[document.URI] = document
		}
		canonicalIDs := s.graphCanonicalImplicitDeclarationIDs(documents, documentByURI)
		for _, declaration := range implicitDeclarations {
			targetID := graphDeclarationNodeID(parsed.URI, declaration.Name, declaration.Range)
			if canonicalIDs[strings.ToLower(declaration.Name)] != targetID {
				continue
			}
			accepted := plans[targetID]
			for _, document := range documents {
				accepted[workspacepkg.FileIdentityKeyFromURI(document.URI)] = struct{}{}
			}
		}
	}
	s.mu.Lock()
	current := s.workspaceIncludeGraphRevision == graphRevision && s.referenceWorkspaceIndex.revisionNumber() == indexRevision
	if current {
		s.storeWorkspaceReferenceImplicitPlanLocked(key, plans)
	}
	s.mu.Unlock()
	return plans, current
}

func (s *Server) legacyImplicitGlobalReferenceDocumentKeysContext(ctx context.Context, parsed *core.ParsedDocument, declaration vbUsageDeclaration) (map[string]struct{}, bool) {
	accepted := map[string]struct{}{}
	if ctx == nil {
		ctx = context.Background()
	}
	if ctx.Err() != nil {
		return nil, false
	}
	targetID := graphDeclarationNodeID(parsed.URI, declaration.Name, declaration.Range)
	lowerName := strings.ToLower(declaration.Name)
	candidates := append([]*core.ParsedDocument{parsed}, s.workspaceReferenceDocuments(parsed)...)
	candidates = append(candidates, s.allWorkspaceReferenceDocuments()...)
	candidates = append(candidates, s.rootlessWorkspaceReferenceDocuments(parsed)...)
	candidates = dedupeParsedDocumentsByFileIdentity(candidates)
	for _, candidate := range candidates {
		if ctx.Err() != nil {
			return nil, false
		}
		if candidate == nil {
			continue
		}
		documents, complete := s.documentGraphIncludeTreeContextResult(ctx, candidate)
		if !complete || ctx.Err() != nil {
			return nil, false
		}
		documentByURI := map[string]*core.ParsedDocument{}
		hasTarget := false
		for _, document := range documents {
			if ctx.Err() != nil {
				return nil, false
			}
			if document == nil {
				continue
			}
			documentByURI[document.URI] = document
			if workspacepkg.SameFileIdentityURI(document.URI, parsed.URI) {
				hasTarget = true
			}
		}
		if !hasTarget {
			continue
		}
		canonicalIDs := s.graphCanonicalImplicitDeclarationIDs(documents, documentByURI)
		if canonicalIDs[lowerName] != targetID {
			continue
		}
		for _, document := range documents {
			if document == nil {
				continue
			}
			accepted[workspacepkg.FileIdentityKeyFromURI(document.URI)] = struct{}{}
		}
	}
	return accepted, true
}

func workspaceReferenceRequestKey(uri string, position lsp.Position, includeDeclaration bool, symbolKind string, generation uint64, name ...string) workspaceReferenceTargetKey {
	normalizedName := ""
	var nameHash uint64
	if len(name) > 0 {
		normalizedName = strings.ToLower(name[0])
		nameHash = workspaceReferenceNameHash(normalizedName)
	}
	return workspaceReferenceTargetKey{
		URI: workspacepkg.FileIdentityKeyFromURI(uri), Name: normalizedName, NameHash: nameHash, Line: position.Line, Character: position.Character,
		SymbolKind: symbolKind, IncludeDeclaration: includeDeclaration, Generation: generation,
	}
}

func workspaceReferenceNameHash(name string) uint64 {
	const offset64 = 14695981039346656037
	const prime64 = 1099511628211
	hash := uint64(offset64)
	for index := 0; index < len(name); index++ {
		value := name[index]
		if value >= 'A' && value <= 'Z' {
			value += 'a' - 'A'
		}
		hash ^= uint64(value)
		hash *= prime64
	}
	return hash
}

func vbReferenceSymbolKindAt(parsed *core.ParsedDocument, name string) string {
	if _, ok := serverObjectSymbolNamed(parsed, name); ok {
		return "variable"
	}
	for _, signature := range vbscript.Signatures(parsed) {
		if strings.EqualFold(signature.Name, name) {
			return signature.Kind
		}
	}
	for _, declaration := range normalizedVBUsageDeclarations(parsed) {
		if strings.EqualFold(declaration.Name, name) && declaration.Kind == "property" {
			return "property"
		}
	}
	if symbol, ok := vbscript.BuildReferenceShard(parsed).Declarations[strings.ToLower(name)]; ok {
		return vbCodeLensSymbolKind(vbUsageDeclaration{Name: symbol.Name, Kind: symbol.Kind, Line: symbol.Range.Start.Line}, vbClassLineSet(parsed))
	}
	return ""
}

func vbReferenceLocationsInDocumentWithIndex(parsed *core.ParsedDocument, index vbscript.SymbolIndex, name string, symbolKind string, includeDeclaration bool) []lsp.Location {
	if vbReferenceUsesCallRanges(symbolKind) {
		ranges := vbscript.UnqualifiedCallRanges(parsed, name)
		locations := make([]lsp.Location, 0, len(ranges))
		for _, r := range ranges {
			locations = append(locations, lsp.Location{URI: parsed.URI, Range: r})
		}
		if includeDeclaration {
			for _, signature := range vbscript.Signatures(parsed) {
				if strings.EqualFold(signature.Name, name) {
					locations = append(locations, lsp.Location{URI: parsed.URI, Range: signature.NameRange})
				}
			}
		}
		return locations
	}
	lower := strings.ToLower(name)
	occurrences := index.Occurrences[lower]
	locations := make([]lsp.Location, 0, len(occurrences))
	declarationRanges := vbReferenceDeclarationRanges(parsed, name)
	var objectInitializations map[lsp.Range]struct{}
	if !includeDeclaration && symbolKind == "variable" {
		objectInitializations = vbReferenceObjectInitializationRanges(parsed, name)
	}
	for _, occurrence := range occurrences {
		if !includeDeclaration {
			if _, declaration := declarationRanges[occurrence.Range]; declaration {
				continue
			}
		}
		if _, objectInitialization := objectInitializations[occurrence.Range]; objectInitialization {
			continue
		}
		locations = append(locations, lsp.Location{URI: parsed.URI, Range: occurrence.Range})
	}
	return locations
}

func vbReferenceDeclarationRanges(parsed *core.ParsedDocument, name string) map[lsp.Range]struct{} {
	return vbReferenceDocumentIndexFor(parsed).declarationRanges[strings.ToLower(name)]
}

func vbReferenceUsesCallRanges(symbolKind string) bool {
	switch symbolKind {
	case "function":
		return true
	default:
		return false
	}
}

func (s *Server) workspaceReferenceDocuments(parsed *core.ParsedDocument) []*core.ParsedDocument {
	documents, _ := s.workspaceReferenceDocumentsContextResult(context.Background(), parsed)
	return documents
}

func (s *Server) workspaceReferenceDocumentsContext(ctx context.Context, parsed *core.ParsedDocument) []*core.ParsedDocument {
	documents, _ := s.workspaceReferenceDocumentsContextResult(ctx, parsed)
	return documents
}

func (s *Server) workspaceReferenceDocumentsContextResult(ctx context.Context, parsed *core.ParsedDocument) ([]*core.ParsedDocument, bool) {
	if parsed == nil {
		return nil, true
	}
	if ctx == nil {
		ctx = context.Background()
	}
	if ctx.Err() != nil {
		return nil, false
	}
	s.mu.Lock()
	generation := s.referenceGeneration
	cacheKey := workspacepkg.FileIdentityKeyFromURI(parsed.URI) + "#" + strconv.FormatUint(generation, 10)
	if cached, ok := s.referenceDocuments[cacheKey]; ok {
		documents := append([]*core.ParsedDocument(nil), cached...)
		s.mu.Unlock()
		s.logDebugSummaryEvent("referenceCache", "[asp-lsp] referenceCache.hit kind=documents uri="+parsed.URI+", documents="+strconv.Itoa(len(documents)), map[string]any{
			"documents": len(documents), "generation": generation, "kind": "documents", "uri": parsed.URI,
		})
		return documents, true
	}
	s.mu.Unlock()
	started := time.Now()
	s.mu.Lock()
	includeRelated := s.settings.CodeLensIncludeRelatedIncludeTrees
	s.mu.Unlock()
	if scope, ok := s.workspaceReferenceScopeSnapshotFor(parsed, includeRelated); ok {
		documents, complete := s.workspaceReferenceDocumentsForScope(ctx, parsed, scope)
		if !complete || ctx.Err() != nil {
			return nil, false
		}
		return s.storeWorkspaceReferenceDocuments(cacheKey, generation, parsed.URI, documents, started), true
	}
	allDocuments := s.allWorkspaceReferenceDocumentsContext(ctx)
	if ctx.Err() != nil {
		return nil, false
	}
	allDocuments = append(allDocuments, s.rootlessWorkspaceReferenceDocuments(parsed)...)
	allDocuments = dedupeParsedDocumentsByFileIdentity(allDocuments)
	documents := make([]*core.ParsedDocument, 0, len(allDocuments))
	seen := map[string]struct{}{}
	add := func(document *core.ParsedDocument) {
		if document == nil {
			return
		}
		key := workspacepkg.FileIdentityKeyFromURI(document.URI)
		if _, ok := seen[key]; ok {
			return
		}
		seen[key] = struct{}{}
		documents = append(documents, document)
	}
	add(parsed)
	included, complete := s.includedDocumentsContextResult(ctx, parsed)
	if !complete || ctx.Err() != nil {
		return nil, false
	}
	for _, included := range included {
		add(included)
	}
	targetPath := cleanFileURIPath(parsed.URI)
	if targetPath == "" {
		if ctx.Err() != nil {
			return nil, false
		}
		return s.storeWorkspaceReferenceDocuments(cacheKey, generation, parsed.URI, documents, started), true
	}
	for _, candidate := range allDocuments {
		if candidate == nil {
			continue
		}
		if workspacepkg.FileIdentityKeyFromURI(candidate.URI) == workspacepkg.FileIdentityKeyFromURI(parsed.URI) {
			continue
		}
		if !s.parsedDocumentIncludesPath(candidate, targetPath) {
			s.logDebugSummary("[asp-lsp] vb.references.reachability.skip: " + candidate.URI + ", target=" + parsed.URI)
			continue
		}
		add(candidate)
		if includeRelated {
			included, complete := s.includedDocumentsContextResult(ctx, candidate)
			if !complete || ctx.Err() != nil {
				return nil, false
			}
			for _, included := range included {
				add(included)
			}
		}
	}
	if ctx.Err() != nil {
		return nil, false
	}
	return s.storeWorkspaceReferenceDocuments(cacheKey, generation, parsed.URI, documents, started), true
}

func (s *Server) workspaceReferenceDocumentsForScope(ctx context.Context, parsed *core.ParsedDocument, scope workspaceReferenceScopeSnapshot) ([]*core.ParsedDocument, bool) {
	documents := make([]*core.ParsedDocument, len(scope.FileNames))
	progress := workspaceReferenceProgressFromContext(ctx)
	completed := 0
	var progressMu sync.Mutex
	if progress != nil {
		progress(0, len(scope.FileNames), "")
	}
	parsedKey := workspacepkg.FileIdentityKeyFromURI(parsed.URI)
	// Pages in the scope usually share include targets; resolve each one once.
	s.analysisWorkers.parallelForBulk(withIncludeResolutionMemo(ctx), len(scope.FileNames), func(workerCtx context.Context, index int) {
		if workerCtx.Err() != nil {
			return
		}
		defer func() {
			if progress != nil && workerCtx.Err() == nil {
				progressMu.Lock()
				defer progressMu.Unlock()
				completed++
				progress(completed, len(scope.FileNames), filePathURI(scope.FileNames[index]))
			}
		}()
		if index < len(scope.DocumentKeys) && scope.DocumentKeys[index] == parsedKey {
			documents[index] = parsed
			return
		}
		documents[index] = s.parsedIncludeFileContext(workerCtx, scope.FileNames[index])
	})
	if ctx.Err() != nil {
		return nil, false
	}
	return dedupeParsedDocumentsByFileIdentity(documents), true
}

func (s *Server) workspaceReferenceScopeSnapshotFor(parsed *core.ParsedDocument, includeRelated bool) (workspaceReferenceScopeSnapshot, bool) {
	if parsed == nil {
		return workspaceReferenceScopeSnapshot{}, false
	}
	targetPath := cleanFileURIPath(parsed.URI)
	if targetPath == "" {
		return workspaceReferenceScopeSnapshot{}, false
	}
	s.mu.Lock()
	if s.workspaceIncludeGraph == nil || s.workspaceIncludeGraph.Size() == 0 || !s.workspaceIncludeGraphComplete || s.rootPath == "" {
		s.mu.Unlock()
		return workspaceReferenceScopeSnapshot{}, false
	}
	key := workspaceReferenceScopeCacheKey{
		DocumentKey: workspacepkg.FileIdentityKeyFromFileName(targetPath), IncludeRelated: includeRelated,
	}
	if cached, ok := s.referenceScopes[key]; ok {
		s.mu.Unlock()
		return cached, true
	}
	graph := s.workspaceIncludeGraph
	reverse := graph.ReverseClosure([]string{targetPath})
	closure := reverse
	if includeRelated {
		closure = graph.ForwardClosure(reverse.FileNames)
	} else {
		forward := graph.ForwardClosure([]string{targetPath})
		closure.FileNames = append(append([]string(nil), reverse.FileNames...), forward.FileNames...)
		closure.IdentityKeys = append(append([]string(nil), reverse.IdentityKeys...), forward.IdentityKeys...)
	}
	snapshot := workspaceReferenceScopeSnapshot{Membership: map[string]struct{}{}}
	for index, identity := range closure.IdentityKeys {
		if _, exists := snapshot.Membership[identity]; exists {
			continue
		}
		snapshot.Membership[identity] = struct{}{}
		snapshot.DocumentKeys = append(snapshot.DocumentKeys, identity)
		if index < len(closure.FileNames) {
			snapshot.FileNames = append(snapshot.FileNames, closure.FileNames[index])
		}
	}
	snapshot.Fingerprint = workspacepkg.DiskContentHash(strings.Join(snapshot.DocumentKeys, "\x00"))
	s.referenceScopes[key] = snapshot
	s.mu.Unlock()
	return snapshot, true
}

func (s *Server) storeWorkspaceReferenceDocuments(cacheKey string, generation uint64, uri string, documents []*core.ParsedDocument, started time.Time) []*core.ParsedDocument {
	documents = append([]*core.ParsedDocument(nil), documents...)
	s.mu.Lock()
	if s.referenceGeneration == generation {
		s.referenceDocuments[cacheKey] = append([]*core.ParsedDocument(nil), documents...)
	}
	s.mu.Unlock()
	s.logDebugVerboseEvent("referenceCache.documents", "[asp-lsp] referenceCache.documents.computed uri="+uri+", documents="+strconv.Itoa(len(documents))+" "+formatElapsedSince(started), map[string]any{
		"documents": len(documents), "durationMs": float64(time.Since(started).Microseconds()) / 1000, "generation": generation, "uri": uri,
	})
	return documents
}

func (s *Server) allWorkspaceReferenceDocuments() []*core.ParsedDocument {
	return s.allWorkspaceReferenceDocumentsContext(context.Background())
}

func (s *Server) allWorkspaceReferenceDocumentsContext(ctx context.Context) []*core.ParsedDocument {
	if ctx == nil {
		ctx = context.Background()
	}
	s.mu.Lock()
	defaultLanguage := s.settings.DefaultLanguage
	openDocs := make([]*core.TextDocument, 0, len(s.documents))
	openKeys := map[string]struct{}{}
	for uri, doc := range s.documents {
		openDocs = append(openDocs, doc)
		openKeys[workspacepkg.FileIdentityKeyFromURI(uri)] = struct{}{}
	}
	workspaceDocs := make([]*core.TextDocument, 0, len(s.workspace))
	for uri, doc := range s.workspace {
		if _, open := openKeys[workspacepkg.FileIdentityKeyFromURI(uri)]; open {
			continue
		}
		workspaceDocs = append(workspaceDocs, doc)
	}
	s.mu.Unlock()
	sourceDocuments := append(openDocs, workspaceDocs...)
	documents := make([]*core.ParsedDocument, len(sourceDocuments))
	s.analysisWorkers.parallelForBulk(ctx, len(sourceDocuments), func(workerCtx context.Context, index int) {
		if workerCtx.Err() != nil {
			return
		}
		documents[index] = s.parseTextDocument(sourceDocuments[index], defaultLanguage)
	})
	return dedupeParsedDocumentsByFileIdentity(documents)
}

func (s *Server) rootlessWorkspaceReferenceDocuments(parsed *core.ParsedDocument) []*core.ParsedDocument {
	if parsed == nil {
		return nil
	}
	s.mu.Lock()
	rootPath := s.rootPath
	defaultLanguage := s.settings.DefaultLanguage
	includeGlobs := append([]string(nil), s.settings.WorkspaceIncludeGlobs...)
	excludeGlobs := append([]string(nil), s.settings.WorkspaceExcludeGlobs...)
	respectGitIgnore := s.settings.WorkspaceRespectGitIgnore
	s.mu.Unlock()
	if rootPath != "" {
		return nil
	}
	sourcePath := cleanFileURIPath(parsed.URI)
	if sourcePath == "" {
		return nil
	}
	sourceDir := filepath.Dir(sourcePath)
	gitIgnoreGlobs := []string{}
	if respectGitIgnore {
		gitIgnoreGlobs = s.readGitIgnoreGlobs(sourceDir)
	}
	documents := make([]*core.ParsedDocument, 0)
	for _, file := range scanWorkspaceFiles(sourceDir) {
		if !workspaceGraphFileAllowed(file.Relative, includeGlobs, excludeGlobs, gitIgnoreGlobs) && filepath.Clean(file.Path) != filepath.Clean(sourcePath) {
			continue
		}
		content, err := s.readWorkspaceTextFile(file.Path)
		if err != nil {
			continue
		}
		documents = append(documents, s.parseText(file.URI, content, defaultLanguage))
	}
	return documents
}

func (s *Server) parsedDocumentIncludesPath(parsed *core.ParsedDocument, targetPath string) bool {
	targetPath = filepath.Clean(targetPath)
	for _, include := range parsed.Includes {
		details, ok := s.includeTargetDetailsForMode(parsed.URI, include.Path, include.Mode)
		if !ok || !details.Exists {
			continue
		}
		if filepath.Clean(details.Path) == targetPath {
			return true
		}
	}
	return false
}
