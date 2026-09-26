package lspserver

import (
	"context"
	"strconv"
	"strings"
	"sync"
	"sync/atomic"
	"time"

	"github.com/yottonoko/classic-web-system-lsp/internal/core"
	"github.com/yottonoko/classic-web-system-lsp/internal/lsp"
)

func (s *Server) resolveCodeLens(ctx context.Context, lens lsp.CodeLens) lsp.CodeLens {
	if ctx == nil {
		ctx = context.Background()
	}
	if ctx.Err() != nil {
		return lsp.CodeLens{}
	}
	var data struct {
		URI        string `json:"uri"`
		Name       string `json:"name"`
		Line       int    `json:"line"`
		Character  int    `json:"character"`
		SymbolKind string `json:"symbolKind"`
	}
	if remarshal(lens.Data, &data) != nil || ctx.Err() != nil {
		if ctx.Err() != nil {
			return lsp.CodeLens{}
		}
		return lens
	}
	doc, parsed := s.parsed(data.URI)
	if parsed == nil || data.Name == "" || ctx.Err() != nil {
		if ctx.Err() != nil {
			return lsp.CodeLens{}
		}
		return lens
	}
	position := vbCodeLensCurrentDeclarationPosition(parsed, data.Name, data.SymbolKind, lsp.Position{Line: data.Line, Character: data.Character})
	declaration := vbUsageDeclaration{Name: data.Name, Kind: data.SymbolKind, Range: lsp.Range{Start: position, End: position}}
	plan := s.workspaceReferenceCodeLensPlanContext(ctx, parsed)
	if ctx.Err() != nil || len(parsed.Includes) > 0 && plan.declarations == nil {
		return lsp.CodeLens{}
	}
	for _, candidate := range plan.declarations {
		if ctx.Err() != nil {
			return lsp.CodeLens{}
		}
		if strings.EqualFold(candidate.Name, data.Name) && candidate.Kind == data.SymbolKind && candidate.Range.Start == position {
			declaration = candidate
			break
		}
	}
	if ctx.Err() != nil {
		return lsp.CodeLens{}
	}
	progressState := "completed"
	progressTaskID := ""
	version := 0
	if doc != nil {
		version = doc.Version
	}
	ctx, progressTaskID = s.beginWorkspaceReferenceProgress(ctx, data.Name, data.URI, version)
	defer func() { s.finishProgressTask(progressTaskID, "references", progressState) }()
	count, countComplete := s.workspaceVBScriptReferenceCount(ctx, parsed, declaration)
	if ctx.Err() != nil {
		progressState = "cancelled"
		return lsp.CodeLens{}
	} else if !countComplete {
		progressState = "stale"
	}
	if doc != nil {
		s.logWorkspaceReferenceBatch(data.URI, doc.Version, parsed, data.Name, nil)
	}
	title := referenceCodeLensTitle(count, s.isJapanese())
	if !countComplete {
		title = referenceCodeLensCalculatingTitle(nil, nil, s.isJapanese())
	}
	if countComplete {
		s.rememberWorkspaceReferenceCount(parsed, declaration, count)
		s.requestCodeLensRefresh("references.resolve.complete")
	}
	if ctx.Err() != nil {
		return lsp.CodeLens{}
	}
	lens.Command = &lsp.Command{
		Title:     title,
		Command:   "aspLsp.showReferences",
		Arguments: []any{data.URI, position},
	}
	return lens
}

func (s *Server) workspaceVBScriptReferenceCount(ctx context.Context, parsed *core.ParsedDocument, declaration vbUsageDeclaration) (int, bool) {
	return s.workspaceVBScriptReferenceCountAttempt(ctx, parsed, declaration, 0)
}

func (s *Server) workspaceVBScriptReferenceCountAttempt(ctx context.Context, parsed *core.ParsedDocument, declaration vbUsageDeclaration, attempt int) (int, bool) {
	if parsed == nil {
		return 0, false
	}
	if !s.waitForCompleteWorkspaceReferenceIndex(ctx) {
		return 0, false
	}
	if count, ok := s.cachedWorkspaceReferenceCodeLensCount(parsed, declaration); ok {
		return count, true
	}
	s.mu.Lock()
	generation := s.referenceGeneration
	nameRevision := s.referenceNameRevisions[strings.ToLower(declaration.Name)]
	s.mu.Unlock()
	documents, complete := s.workspaceReferenceDocumentsContextResult(ctx, parsed)
	if !complete || ctx.Err() != nil {
		return 0, false
	}
	progressReporter := workspaceReferenceProgressFromContext(ctx)
	results := s.workspaceVBScriptReferenceBatch(ctx, parsed, []vbUsageDeclaration{declaration}, documents, generation, func(progress workspaceReferenceBatchProgress) {
		if progressReporter != nil {
			progressReporter(progress.Completed, progress.Total, progress.URI)
		}
	})
	if len(results) == 0 || results[0].stale || ctx.Err() != nil {
		return 0, false
	}
	count := results[0].count
	key := workspaceReferenceRequestKey(parsed.URI, declaration.Range.Start, false, declaration.Kind, generation, declaration.Name)
	s.mu.Lock()
	valid := s.referenceGeneration == generation && s.referenceNameRevisions[strings.ToLower(declaration.Name)] == nameRevision
	if valid {
		s.storeWorkspaceReferenceCountLocked(key, count)
	}
	s.mu.Unlock()
	if !valid {
		if attempt == 0 && ctx.Err() == nil {
			_, current := s.parsed(parsed.URI)
			if current == nil {
				return 0, false
			}
			for _, candidate := range s.vbscriptReferenceCodeLensDeclarations(current) {
				if strings.EqualFold(candidate.Name, declaration.Name) && candidate.Kind == declaration.Kind {
					return s.workspaceVBScriptReferenceCountAttempt(ctx, current, candidate, attempt+1)
				}
			}
		}
		return 0, false
	}
	return count, true
}

func referenceCodeLensTitle(count int, japanese bool) string {
	if japanese {
		return strconv.Itoa(count) + " 件の参照"
	}
	title := strconv.Itoa(count) + " references"
	if count == 1 {
		title = "1 reference"
	}
	return title
}

func referenceCodeLensCalculatingTitle(partial, previous *int, japanese bool) string {
	if japanese {
		if partial != nil {
			title := strconv.Itoa(*partial) + "+ 件の参照 (計算中"
			if previous != nil {
				title += "; 前回 " + strconv.Itoa(*previous)
			}
			return title + ")"
		}
		if previous == nil {
			return "参照数を計算中"
		}
		return strconv.Itoa(*previous) + " 件の参照 (計算中)"
	}
	if partial != nil {
		title := strconv.Itoa(*partial) + "+ references (calculating"
		if previous != nil {
			title += "; previous " + strconv.Itoa(*previous)
		}
		return title + ")"
	}
	if previous == nil {
		return "Calculating references"
	}
	title := strconv.Itoa(*previous) + " references"
	if *previous == 1 {
		title = "1 reference"
	}
	return title + " (calculating)"
}

func (s *Server) logWorkspaceReferenceBatch(uri string, version int, parsed *core.ParsedDocument, requestedName string, declarations []vbUsageDeclaration) {
	if parsed == nil {
		return
	}
	s.mu.Lock()
	if s.shutdown {
		s.mu.Unlock()
		return
	}
	generation := s.referenceGeneration
	key := referenceBatchCacheKey(uri, version, generation)
	if state := s.referenceBatch[key]; state != nil {
		complete := state.complete
		warmed := state.warmed
		total := state.total
		s.mu.Unlock()
		operation := "inflight.join"
		if complete {
			operation = "cache.hit"
		}
		s.logDebugSummaryEvent("referenceCache.batch", "[asp-lsp] vb.references.batch."+operation+": "+uri+", symbol="+requestedName+", warmed="+strconv.Itoa(warmed)+", symbols="+strconv.Itoa(total), map[string]any{
			"generation": generation, "requestedSymbol": requestedName, "symbolsTotal": total, "symbolsWarmed": warmed, "uri": uri,
		})
		return
	}
	s.mu.Unlock()
	if declarations == nil {
		declarations = s.workspaceReferenceCodeLensPlan(parsed).declarations
	}
	s.mu.Lock()
	if s.shutdown || s.referenceGeneration != generation {
		s.mu.Unlock()
		return
	}
	if state := s.referenceBatch[key]; state != nil {
		s.mu.Unlock()
		return
	}
	s.mu.Unlock()
	progressTaskID, _ := s.beginDocumentProgressTask("references.batch", "analyzing", "references", "references.waitIndex", progressDetailForURI(uri), uri, version, true, 0, false)
	batchContext, cancel := context.WithCancel(context.Background())
	nameRevisions := make(map[string]uint64, len(declarations))
	s.mu.Lock()
	if s.shutdown || s.referenceGeneration != generation || s.referenceBatch[key] != nil {
		s.mu.Unlock()
		cancel()
		s.finishProgressTask(progressTaskID, "references", "stale")
		return
	}
	for _, declaration := range declarations {
		name := strings.ToLower(declaration.Name)
		nameRevisions[name] = s.referenceNameRevisions[name]
	}
	state := &workspaceReferenceBatchState{
		generation: generation, done: make(chan struct{}), ctx: batchContext, cancel: cancel, nameRevisions: nameRevisions,
		total: len(declarations), started: time.Now(), progressTaskID: progressTaskID, declarations: declarations,
	}
	s.storeWorkspaceReferenceBatchLocked(key, state)
	s.backgroundAnalysisWorkers.Add(1)
	s.mu.Unlock()
	s.logDebugSummaryEvent("referenceCache.batch", "[asp-lsp] vb.references.batch.requested: "+uri+", symbol="+requestedName+", symbols=1, symbolsTotal="+strconv.Itoa(len(declarations)), map[string]any{
		"generation": generation, "requestedSymbol": requestedName, "symbolsTotal": len(declarations), "uri": uri,
	})
	go func() {
		defer s.backgroundAnalysisWorkers.Done()
		s.warmWorkspaceReferenceBatch(key, state, uri, version, parsed, declarations, requestedName)
	}()
}

func (s *Server) warmWorkspaceReferenceBatch(key workspaceReferenceBatchKey, state *workspaceReferenceBatchState, uri string, version int, parsed *core.ParsedDocument, declarations []vbUsageDeclaration, requestedName string) {
	batchContext := state.ctx
	if batchContext == nil {
		batchContext = context.Background()
	}
	if !s.waitForCompleteWorkspaceReferenceIndex(batchContext) {
		s.finishWorkspaceReferenceBatch(key, state, uri, "stale")
		return
	}
	if done := s.updateProgressTaskImmediate(state.progressTaskID, "references", "references.relatedIncludeTree", progressDetailForURI(uri), 0, 0, []string{progressDetailForURI(uri)}, "running"); done != nil {
		<-done
	}
	preparationContext := context.WithValue(batchContext, workspaceReferenceProgressContextKey{}, workspaceReferenceProgressReporter(func(current, total int, documentURI string) {
		s.updateProgressTask(state.progressTaskID, "references", "references.relatedIncludeTree", progressDetailForURI(documentURI), current, total, nil, "running")
	}))
	documents, complete := s.workspaceReferenceDocumentsContextResult(preparationContext, parsed)
	if !complete || batchContext.Err() != nil {
		s.finishWorkspaceReferenceBatch(key, state, uri, "stale")
		return
	}
	if done := s.updateProgressTaskImmediate(state.progressTaskID, "references", "references.relatedIncludeTree", progressDetailForURI(uri), len(documents), len(documents), nil, "running"); done != nil {
		<-done
	}
	s.updateProgressTask(state.progressTaskID, "references", "references.restoreCache", progressDetailForURI(uri), 0, 0, nil, "running")
	s.restoreWorkspaceReferenceCountSummariesContext(batchContext, documents, state.generation)
	s.updateProgressTask(state.progressTaskID, "references", "references.prepareQueries", progressDetailForURI(uri), 0, len(declarations), nil, "running")
	descriptors := s.workspaceReferenceQueryDescriptorsContext(batchContext, parsed, declarations, documents)
	if batchContext.Err() != nil {
		s.finishWorkspaceReferenceBatch(key, state, uri, "cancelled")
		return
	}
	s.mu.Lock()
	if s.referenceGeneration != state.generation || s.referenceBatch[key] != state {
		s.mu.Unlock()
		s.finishWorkspaceReferenceBatch(key, state, uri, "cancelled")
		return
	}
	state.queryDescriptors = descriptors
	s.mu.Unlock()
	restored := s.restoreWorkspaceReferenceQueriesWithDescriptors(parsed, descriptors, state.generation)
	if restored > 0 {
		s.mu.Lock()
		state.dbRestored = restored
		s.mu.Unlock()
		s.logAnalysisDatabaseEvent("referenceQueries", "restore", map[string]any{"restored": restored, "symbols": len(declarations), "uri": parsed.URI})
	}
	pending := make([]vbUsageDeclaration, 0, len(declarations))
	for _, declaration := range declarations {
		s.mu.Lock()
		if s.referenceGeneration != state.generation || s.referenceBatch[key] != state {
			s.mu.Unlock()
			s.finishWorkspaceReferenceBatch(key, state, uri, "cancelled")
			return
		}
		requestKey := workspaceReferenceRequestKey(uri, declaration.Range.Start, false, declaration.Kind, state.generation, declaration.Name)
		_, resultCached := s.referenceResults[requestKey]
		_, countCached := s.referenceCounts[requestKey]
		cached := resultCached || countCached
		s.mu.Unlock()
		if cached {
			s.recordWorkspaceReferenceBatchWarm(state, true, declaration.Name)
			continue
		}
		pending = append(pending, declaration)
	}
	partialCounts := make([]atomic.Int64, len(pending))
	var partialPublishMu sync.Mutex
	var lastPartialPublish time.Time
	if len(pending) > 0 && len(documents) > 0 {
		s.mu.Lock()
		state.documentProgress = true
		taskID := state.progressTaskID
		s.mu.Unlock()
		s.updateProgressTask(taskID, "references", "references.countDocuments", "", 0, len(documents), nil, "running")
	}
	results := s.workspaceVBScriptReferenceBatch(batchContext, parsed, pending, documents, state.generation, func(progress workspaceReferenceBatchProgress) {
		for _, delta := range progress.Deltas {
			if delta.TargetIndex >= 0 && delta.TargetIndex < len(partialCounts) && delta.Count != 0 {
				partialCounts[delta.TargetIndex].Add(int64(delta.Count))
			}
		}
		label := "references.countDocuments"
		if progress.Segments {
			label = "references.countSegments"
		}
		if done := s.updateProgressTaskImmediate(state.progressTaskID, "references", label, progressDetailForURI(progress.URI), progress.Completed, progress.Total, []string{progress.URI}, "running"); done != nil {
			<-done
		}
		s.mu.Lock()
		if progress.Total > state.examinedSegments {
			state.examinedSegments = progress.Total
		}
		s.mu.Unlock()
		partialPublishMu.Lock()
		now := time.Now()
		if shouldPublishWorkspaceReferencePartial(lastPartialPublish, now, progress.Completed, progress.Total) {
			lastPartialPublish = now
			s.publishWorkspaceReferencePartialCounts(parsed, pending, state, partialCounts)
		}
		partialPublishMu.Unlock()
	})
	for index, declaration := range pending {
		if results[index].stale {
			s.finishWorkspaceReferenceBatch(key, state, uri, "stale")
			return
		}
		requestKey := workspaceReferenceRequestKey(uri, declaration.Range.Start, false, declaration.Kind, state.generation, declaration.Name)
		s.mu.Lock()
		if s.referenceGeneration != state.generation || s.referenceBatch[key] != state {
			s.mu.Unlock()
			s.finishWorkspaceReferenceBatch(key, state, uri, "stale")
			return
		}
		s.deleteWorkspaceReferenceTargetKindLocked(requestKey, workspaceReferencePartialCountCache)
		s.storeWorkspaceReferenceCountLocked(requestKey, results[index].count)
		s.mu.Unlock()
		s.rememberWorkspaceReferenceCount(parsed, declaration, results[index].count)
		s.recordWorkspaceReferenceBatchWarm(state, false, declaration.Name)
	}
	s.mu.Lock()
	if s.referenceGeneration == state.generation && s.referenceBatch[key] == state {
		counts := make([]int, len(declarations))
		complete := true
		for index, declaration := range declarations {
			requestKey := workspaceReferenceRequestKey(uri, declaration.Range.Start, false, declaration.Kind, state.generation, declaration.Name)
			count, ok := s.referenceCounts[requestKey]
			if !ok {
				complete = false
				break
			}
			counts[index] = count
		}
		if complete {
			state.finalCounts = counts
		}
	}
	s.mu.Unlock()
	_ = version
	_ = requestedName
	s.finishWorkspaceReferenceBatch(key, state, uri, "complete")
}

const workspaceReferencePartialPublishInterval = 50 * time.Millisecond

func shouldPublishWorkspaceReferencePartial(last, now time.Time, completed, total int) bool {
	return last.IsZero() || total > 0 && completed >= total || now.Sub(last) >= workspaceReferencePartialPublishInterval
}

func (s *Server) publishWorkspaceReferencePartialCounts(parsed *core.ParsedDocument, declarations []vbUsageDeclaration, state *workspaceReferenceBatchState, counts []atomic.Int64) {
	s.mu.Lock()
	if s.referenceGeneration != state.generation || state.ctx != nil && state.ctx.Err() != nil {
		s.mu.Unlock()
		return
	}
	total := 0
	for index, declaration := range declarations {
		if index >= len(counts) {
			break
		}
		count := int(counts[index].Load())
		name := strings.ToLower(declaration.Name)
		if s.referenceNameRevisions[name] != state.nameRevisions[name] {
			continue
		}
		key := workspaceReferenceRequestKey(parsed.URI, declaration.Range.Start, false, declaration.Kind, state.generation, declaration.Name)
		if previous, ok := s.referencePartialCounts[key]; !ok || count > previous {
			s.storeWorkspaceReferencePartialCountLocked(key, count)
		}
		total += count
	}
	s.mu.Unlock()
	s.logDebugVerboseEvent("referenceCache.partial", "[asp-lsp] referenceCache.partial uri="+parsed.URI+", references="+strconv.Itoa(total), map[string]any{
		"generation": state.generation, "references": total, "symbols": len(declarations), "uri": parsed.URI,
	})
	s.requestCodeLensRefresh("references.batch.partial")
}

func (s *Server) recordWorkspaceReferenceBatchWarm(state *workspaceReferenceBatchState, cached bool, name string) {
	s.mu.Lock()
	state.warmed++
	warmed := state.warmed
	total := state.total
	taskID := state.progressTaskID
	documentProgress := state.documentProgress
	if cached {
		state.cacheHits++
	}
	s.mu.Unlock()
	if !documentProgress {
		s.updateProgressTask(taskID, "references", "references.countSymbols", name, warmed, total, []string{name}, "running")
	}
}

func (s *Server) finishWorkspaceReferenceBatch(key workspaceReferenceBatchKey, state *workspaceReferenceBatchState, uri, operation string) {
	s.mu.Lock()
	owned := s.referenceBatch[key] == state
	if owned {
		state.complete = operation == "complete"
		if operation != "complete" {
			s.deleteWorkspaceReferenceBatchLocked(key)
		}
	}
	warmed := state.warmed
	cacheHits := state.cacheHits
	dbRestored := state.dbRestored
	examinedSegments := state.examinedSegments
	descriptors := state.queryDescriptors
	total := state.total
	state.doneOnce.Do(func() { close(state.done) })
	s.mu.Unlock()
	if state.cancel != nil {
		state.cancel()
	}
	s.logDebugSummaryEvent("referenceCache.batch", "[asp-lsp] vb.references.batch."+operation+": "+uri+", symbols="+strconv.Itoa(total)+", warmed="+strconv.Itoa(warmed)+", cacheHits="+strconv.Itoa(cacheHits)+", dbRestored="+strconv.Itoa(dbRestored)+", computed="+strconv.Itoa(max(0, warmed-cacheHits))+", examinedSegments="+strconv.Itoa(examinedSegments)+" "+formatElapsedSince(state.started), map[string]any{
		"cacheHits": cacheHits, "computedSymbols": max(0, warmed-cacheHits), "dbRestored": dbRestored,
		"durationMs": float64(time.Since(state.started).Microseconds()) / 1000, "examinedSegments": examinedSegments,
		"generation": state.generation, "symbolsTotal": total, "symbolsWarmed": warmed, "uri": uri,
	})
	if operation == "complete" && owned {
		if done := s.updateProgressTaskImmediate(state.progressTaskID, "references", "references.finalize", progressDetailForURI(uri), 0, 1, []string{progressDetailForURI(uri)}, "running"); done != nil {
			<-done
		}
		s.persistWorkspaceReferenceBatchWithDescriptors(uri, state.generation, descriptors)
		s.requestCodeLensRefresh("references.batch.complete")
		if done := s.updateProgressTaskImmediate(state.progressTaskID, "references", "references.finalize", progressDetailForURI(uri), 1, 1, nil, "running"); done != nil {
			<-done
		}
	}
	progressState := "completed"
	if operation != "complete" {
		progressState = operation
	}
	s.finishProgressTask(state.progressTaskID, "references", progressState)
}
