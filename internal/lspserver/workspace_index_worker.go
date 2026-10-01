package lspserver

import (
	"context"
	"errors"
	"os"
	"path/filepath"
	"runtime"
	"sort"
	"strconv"
	"strings"
	"sync"
	"time"

	"github.com/yottonoko/classic-web-system-lsp/internal/core"
	workspacepkg "github.com/yottonoko/classic-web-system-lsp/internal/workspace"
)

var (
	errWorkspaceIndexGeneration  = errors.New("workspace index generation changed")
	errWorkspaceIndexPersistence = errors.New("workspace index persistence failed")
	errWorkspaceIndexCacheDrift  = errors.New("workspace cache configuration changed")
)

type workspaceIndexBuildResult struct {
	documents             map[string]*core.TextDocument
	includeGraphCandidate *workspaceIncludeGraphDiskCandidate
	cacheSnapshot         *workspaceDiskCacheReadSnapshot
	cacheCandidateUsed    bool
	cacheHit              bool
	complete              bool
	err                   error
}

func completeWorkspaceIndexBuild(documents map[string]*core.TextDocument, cacheHit bool) workspaceIndexBuildResult {
	return workspaceIndexBuildResult{documents: documents, cacheHit: cacheHit, complete: true}
}

func incompleteWorkspaceIndexBuild(err error) workspaceIndexBuildResult {
	return workspaceIndexBuildResult{err: err}
}

type workspaceIndexTestPhase uint8

const (
	workspaceIndexTestPhaseStarted workspaceIndexTestPhase = iota
	workspaceIndexTestPhaseBeforePublish
	workspaceIndexTestPhaseBeforeDiskWrite
	workspaceIndexTestPhaseAfterDiskEnqueueBeforeCommit
)

type workspaceIndexRunSettings struct {
	roots            []workspaceRoot
	includeGlobs     []string
	excludeGlobs     []string
	scanChunkSize    int
	respectGitIgnore bool
	legacyEncoding   string
	cacheDirectory   string
	readLimiter      chan struct{}
	baseDocuments    map[string]workspaceArtifactFingerprint
}

type workspaceIndexProgressReporter func(label string, current, total int, detail string)

func (s *Server) pauseWorkspaceIndexScheduling(paused bool) {
	s.mu.Lock()
	s.workspaceIndexSchedulingPaused = paused
	s.mu.Unlock()
}

func (s *Server) activateWorkspaceIndexing(ctx context.Context) bool {
	if ctx == nil {
		ctx = context.Background()
	}
	s.mu.Lock()
	defer s.mu.Unlock()
	if s.workspaceIndexClosed || s.workspaceIndexSchedulingPaused {
		return false
	}
	activated := !s.workspaceIndexEnabled
	s.workspaceIndexEnabled = true
	if activated || s.workspaceIndexParent == nil {
		s.workspaceIndexParent = ctx
	}
	return activated
}

func (s *Server) scheduleWorkspaceIndex(reason string) {
	s.workspaceIndexDiskCommitMu.Lock()
	s.mu.Lock()
	if !s.workspaceIndexEnabled || s.workspaceIndexSchedulingPaused || s.workspaceIndexClosed {
		s.mu.Unlock()
		s.workspaceIndexDiskCommitMu.Unlock()
		return
	}
	parent := s.workspaceIndexParent
	if parent == nil {
		parent = context.Background()
	}
	previousCancel := s.workspaceIndexCancel
	promotion := s.workspaceReferencePromotionCandidateLocked()
	s.workspaceIndexGeneration++
	generation := s.workspaceIndexGeneration
	s.markWorkspaceVBAutoIncludeCatalogIncompleteLocked(generation)
	ctx, cancel := context.WithCancel(parent)
	done := make(chan struct{})
	s.workspaceIndexCancel = cancel
	s.workspaceIndexDone = done
	// A workspace reference count cannot be final while the document universe
	// for this generation is still changing. Start a fresh reference generation
	// so an older, smaller universe can never be presented as complete.
	s.clearWorkspaceReferenceCacheLocked()
	if promotion != nil {
		promotion.workspaceGeneration = generation
		s.workspaceReferencePendingPromotion = promotion
	}
	s.workspaceIndexWorkers.Add(1)
	s.mu.Unlock()
	s.workspaceIndexDiskCommitMu.Unlock()
	s.requestCodeLensRefresh("workspace.index.started:" + reason)

	if previousCancel != nil {
		previousCancel()
	}
	go s.runWorkspaceIndexWorker(ctx, reason, generation, done)
}

func (s *Server) runWorkspaceIndexWorker(ctx context.Context, reason string, generation uint64, done chan struct{}) {
	started := time.Now()
	s.logDebugSummaryEvent("workspaceIndex", "[asp-lsp] workspaceIndex.started"+formatLogFields(map[string]any{
		"generation": generation, "reason": reason,
	}), map[string]any{"generation": generation, "reason": reason})
	taskID, _ := s.beginProgressTask("workspace.index", "loading", "workspace.index", "workspace.index", reason, 0, false)
	progressState := "cancelled"
	defer close(done)
	defer s.workspaceIndexWorkers.Done()
	defer func() {
		s.mu.Lock()
		if s.workspaceIndexGeneration == generation {
			s.workspaceIndexCancel = nil
		}
		s.mu.Unlock()
	}()
	defer func() { s.finishProgressTask(taskID, "workspace.index", progressState) }()
	defer func() {
		if progressState == "completed" {
			return
		}
		metadata := map[string]any{
			"durationMs": float64(time.Since(started).Microseconds()) / 1000,
			"generation": generation,
			"reason":     reason,
			"state":      progressState,
		}
		s.logDebugSummaryEvent("workspaceIndex", "[asp-lsp] workspaceIndex."+progressState+formatLogFields(metadata), metadata)
	}()

	s.runWorkspaceIndexTestHook(ctx, workspaceIndexTestPhaseStarted, generation)
	if !s.workspaceIndexGenerationCurrent(ctx, generation) {
		result := s.workspaceIndexBuildInterruption(ctx, generation)
		progressState = s.workspaceIndexBuildProgressState(result.err)
		s.reportWorkspaceIndexBuildFailure(taskID, reason, result.err, progressState)
		return
	}
	s.workspaceIndexStateMu.RLock()
	settings := s.workspaceIndexRunSettings()
	result := s.buildWorkspaceIndexWithProgress(ctx, generation, settings, func(label string, current, total int, detail string) {
		activeItems := []string(nil)
		if detail != "" {
			activeItems = []string{detail}
		}
		s.updateProgressTask(taskID, "workspace.index", label, detail, current, total, activeItems, "running")
	})
	s.workspaceIndexStateMu.RUnlock()
	if !result.complete {
		progressState = s.workspaceIndexBuildProgressState(result.err)
		s.reportWorkspaceIndexBuildFailure(taskID, reason, result.err, progressState)
		return
	}
	docs, cacheHit := result.documents, result.cacheHit
	s.runWorkspaceIndexTestHook(ctx, workspaceIndexTestPhaseBeforePublish, generation)
	cacheSnapshot := (*workspaceDiskCacheReadSnapshot)(nil)
	if result.cacheCandidateUsed {
		cacheSnapshot = result.cacheSnapshot
	}
	published, cacheDrift := s.publishWorkspaceIndexCandidate(ctx, generation, docs, settings.baseDocuments, cacheSnapshot)
	if !published {
		if cacheDrift {
			s.scheduleWorkspaceIndex("workspace.cache.changed")
			progressState = s.workspaceIndexBuildProgressState(errWorkspaceIndexCacheDrift)
			s.reportWorkspaceIndexBuildFailure(taskID, reason, errWorkspaceIndexCacheDrift, progressState)
			return
		}
		result := s.workspaceIndexBuildInterruption(ctx, generation)
		progressState = s.workspaceIndexBuildProgressState(result.err)
		s.reportWorkspaceIndexBuildFailure(taskID, reason, result.err, progressState)
		return
	}
	s.workspaceIndexStateMu.RLock()
	if !s.workspaceIndexGenerationCurrent(ctx, generation) {
		s.workspaceIndexStateMu.RUnlock()
		result := s.workspaceIndexBuildInterruption(ctx, generation)
		progressState = s.workspaceIndexBuildProgressState(result.err)
		s.reportWorkspaceIndexBuildFailure(taskID, reason, result.err, progressState)
		return
	}
	if cacheHit {
		s.logAnalysisDatabaseEvent("workspaceIndex", "hit", map[string]any{
			"documents": len(docs), "generation": generation, "reason": reason,
		})
		s.workspaceIndexStateMu.RUnlock()
	} else {
		s.updateProgressTask(taskID, "workspace.index", "workspace.index.writeCache", "analysis database", 0, len(docs), nil, "running")
		s.workspaceIndexStateMu.RUnlock()
		s.runWorkspaceIndexTestHook(ctx, workspaceIndexTestPhaseBeforeDiskWrite, generation)
		if !s.writeWorkspaceIndexToDiskIfCurrent(ctx, generation, docs) {
			result := s.workspaceIndexBuildInterruption(ctx, generation)
			if result.err == nil {
				result.err = errWorkspaceIndexPersistence
			}
			progressState = s.workspaceIndexBuildProgressState(result.err)
			s.reportWorkspaceIndexBuildFailure(taskID, reason, result.err, progressState)
			return
		}
		s.updateProgressTask(taskID, "workspace.index", "workspace.index.writeCache", "analysis database", len(docs), len(docs), nil, "running")
	}
	if !s.workspaceIndexGenerationCurrent(ctx, generation) {
		result := s.workspaceIndexBuildInterruption(ctx, generation)
		progressState = s.workspaceIndexBuildProgressState(result.err)
		s.reportWorkspaceIndexBuildFailure(taskID, reason, result.err, progressState)
		return
	}
	if done := s.updateProgressTaskImmediate(taskID, "workspace.index", "workspace.index.includeGraph", "include graph", 0, 1, nil, "running"); done != nil {
		<-done
	}
	// Restore the complete reverse include index before hydrating file bundles.
	// A valid persisted graph already has everything workspace features need;
	// parsing is reserved for a cache miss and later on-demand requests.
	if failureErr := s.synchronizeWorkspaceIncludeGraphForWorkspaceIndexWithProgress(ctx, generation, result.includeGraphCandidate, func(label, detail string, current, total int) {
		s.updateProgressTask(taskID, "workspace.index", label, detail, current, total, nil, "running")
	}); failureErr != nil {
		progressState = s.workspaceIndexBuildProgressState(failureErr)
		s.reportWorkspaceIndexBuildFailure(taskID, reason, failureErr, progressState)
		return
	}
	if done := s.updateProgressTaskImmediate(taskID, "workspace.index", "workspace.index.includeGraph", "include graph", 1, 1, nil, "running"); done != nil {
		<-done
	}
	if !s.workspaceIndexGenerationCurrent(ctx, generation) {
		result := s.workspaceIndexBuildInterruption(ctx, generation)
		progressState = s.workspaceIndexBuildProgressState(result.err)
		s.reportWorkspaceIndexBuildFailure(taskID, reason, result.err, progressState)
		return
	}
	s.updateProgressTask(taskID, "workspace.index", "workspace.index.catalog", "", 0, 0, nil, "running")
	if !s.rebuildWorkspaceVBAutoIncludeCatalog(ctx, generation) {
		result := s.workspaceIndexBuildInterruption(ctx, generation)
		if result.err == nil {
			result.err = errors.New("workspace VBScript auto-include catalog rebuild failed")
		}
		progressState = s.workspaceIndexBuildProgressState(result.err)
		s.reportWorkspaceIndexBuildFailure(taskID, reason, result.err, progressState)
		return
	}
	if done := s.updateProgressTaskImmediate(taskID, "workspace.index", "workspace.index.finalize", "reference universe", 0, 1, nil, "running"); done != nil {
		<-done
	}
	referenceFingerprint := s.workspaceReferenceUniverseFingerprint()
	if done := s.updateProgressTaskImmediate(taskID, "workspace.index", "workspace.index.finalize", "reference universe", 1, 1, nil, "running"); done != nil {
		<-done
	}
	s.workspaceIndexStateMu.RLock()
	defer s.workspaceIndexStateMu.RUnlock()
	if !s.workspaceIndexGenerationCurrent(ctx, generation) {
		result := s.workspaceIndexBuildInterruption(ctx, generation)
		progressState = s.workspaceIndexBuildProgressState(result.err)
		s.reportWorkspaceIndexBuildFailure(taskID, reason, result.err, progressState)
		return
	}
	s.logDebugSummaryEvent("workspaceIndex", "[asp-lsp] workspaceIndex.complete"+formatLogFields(map[string]any{
		"documents": len(docs), "generation": generation, "reason": reason, "source": map[bool]string{true: "analysisDatabase", false: "filesystem"}[cacheHit],
	}), map[string]any{
		"documents": len(docs), "generation": generation, "reason": reason, "source": map[bool]string{true: "analysisDatabase", false: "filesystem"}[cacheHit],
	})
	promoted := 0
	s.mu.Lock()
	if s.workspaceIndexGeneration == generation && !s.workspaceIndexClosed && s.workspaceIncludeGraphComplete {
		promoted = s.promoteWorkspaceReferenceCountsLocked(generation, referenceFingerprint)
		s.workspaceReferenceReadyFingerprint = referenceFingerprint
		s.workspaceReferenceIndexReadyGeneration = generation
	}
	s.mu.Unlock()
	if promoted > 0 {
		s.logDebugSummaryEvent("referenceCache.promotion", "[asp-lsp] referenceCache.promoted unchanged workspace counts="+strconv.Itoa(promoted), map[string]any{
			"counts": promoted, "generation": generation,
		})
	}
	s.reportAsyncRPCWriteError(s.requestVisualRefresh("workspace.index.complete:" + reason))
	progressState = "completed"
}

const workspaceIncludeGraphRetryDelay = 10 * time.Millisecond

// synchronizeWorkspaceIncludeGraphForWorkspaceIndex keeps graph collection
// inside the workspace-index worker's generation. A graph invalidation does
// not change the discovered document universe, so retry only the collection
// and synchronization after the document-open publication barrier settles.

func (s *Server) synchronizeWorkspaceIncludeGraphForWorkspaceIndexWithProgress(ctx context.Context, generation uint64, candidate *workspaceIncludeGraphDiskCandidate, report graphProgressReporter) error {
	for {
		if interruption := s.workspaceIndexBuildInterruption(ctx, generation); interruption.err != nil {
			return interruption.err
		}
		if report != nil {
			report("workspace.index.waitDocuments", "", 0, 0)
		}
		if !s.waitForDocumentOpenAnalysisContext(ctx) {
			if interruption := s.workspaceIndexBuildInterruption(ctx, generation); interruption.err != nil {
				return interruption.err
			}
			if ctx != nil {
				if ctxErr := ctx.Err(); ctxErr != nil {
					return ctxErr
				}
			}
			return errWorkspaceIndexGeneration
		}
		if report != nil {
			report("workspace.index.includeGraph", "", 0, 0)
		}
		if candidate != nil {
			if s.restoreWorkspaceIncludeGraphCandidateIfWorkspaceCurrent(ctx, generation, candidate) {
				return nil
			}
			candidate = nil
		}

		collection := s.workspaceGraphDocumentsContextWithProgressResult(ctx, false, func(_ string, detail string, current, total int) {
			if report != nil {
				report("workspace.index.parseFiles", detail, current, total)
			}
		})
		if collection.complete {
			if interruption := s.workspaceIndexBuildInterruption(ctx, generation); interruption.err != nil {
				return interruption.err
			}
			return nil
		}
		if !errors.Is(collection.err, errGraphCollectionGeneration) {
			if collection.err != nil {
				return collection.err
			}
			return errGraphCollectionIncludeSync
		}
		if interruption := s.workspaceIndexBuildInterruption(ctx, generation); interruption.err != nil {
			return interruption.err
		}
		if !waitForWorkspaceIncludeGraphRetry(ctx) {
			if interruption := s.workspaceIndexBuildInterruption(ctx, generation); interruption.err != nil {
				return interruption.err
			}
			if ctx != nil {
				if ctxErr := ctx.Err(); ctxErr != nil {
					return ctxErr
				}
			}
			return errWorkspaceIndexGeneration
		}
	}
}

func waitForWorkspaceIncludeGraphRetry(ctx context.Context) bool {
	if ctx == nil {
		ctx = context.Background()
	}
	timer := time.NewTimer(workspaceIncludeGraphRetryDelay)
	defer func() {
		if !timer.Stop() {
			select {
			case <-timer.C:
			default:
			}
		}
	}()
	select {
	case <-timer.C:
		return true
	case <-ctx.Done():
		return false
	}
}

func (s *Server) workspaceReferencePromotionCandidateLocked() *workspaceReferenceCountPromotion {
	if pending := s.workspaceReferencePendingPromotion; pending != nil && len(pending.counts) > 0 {
		return pending
	}
	if s.workspaceReferenceReadyFingerprint == "" ||
		s.workspaceReferenceIndexReadyGeneration != s.workspaceIndexGeneration ||
		len(s.referenceCounts) == 0 {
		return nil
	}
	counts := make(map[workspaceReferenceTargetKey]int, len(s.referenceCounts))
	for key, count := range s.referenceCounts {
		if key.Generation == s.referenceGeneration {
			counts[key] = count
		}
	}
	if len(counts) == 0 {
		return nil
	}
	return &workspaceReferenceCountPromotion{fingerprint: s.workspaceReferenceReadyFingerprint, counts: counts}
}

func (s *Server) workspaceIndexBuildProgressState(err error) string {
	if errors.Is(err, errWorkspaceIndexGeneration) || errors.Is(err, errWorkspaceIndexCacheDrift) {
		return "stale"
	}
	if errors.Is(err, context.Canceled) || errors.Is(err, context.DeadlineExceeded) {
		return "cancelled"
	}
	return "failed"
}

func (s *Server) reportWorkspaceIndexBuildFailure(taskID, reason string, err error, state string) {
	label := "workspace.index"
	detail := ""
	if state == "failed" {
		message := s.workspaceIndexFailureMessage(err)
		label = "workspace.index.failed"
		detail = message
		s.logServerWarning("[asp-lsp] workspace.index.failed: " + message)
	}
	progressReason := "workspace.index." + state
	if reason != "" {
		progressReason += ":" + reason
	}
	if done := s.updateProgressTaskImmediate(taskID, progressReason, label, detail, 0, 0, nil, state); done != nil {
		<-done
	}
}

func (s *Server) promoteWorkspaceReferenceCountsLocked(workspaceGeneration uint64, fingerprint string) int {
	promotion := s.workspaceReferencePendingPromotion
	s.workspaceReferencePendingPromotion = nil
	if promotion == nil || promotion.workspaceGeneration != workspaceGeneration ||
		promotion.fingerprint == "" || promotion.fingerprint != fingerprint {
		return 0
	}
	promoted := 0
	for key, count := range promotion.counts {
		key.Generation = s.referenceGeneration
		if _, exists := s.referenceCounts[key]; exists {
			continue
		}
		s.storeWorkspaceReferenceCountLocked(key, count)
		promoted++
	}
	return promoted
}

func (s *Server) workspaceReferenceUniverseFingerprint() string {
	s.mu.Lock()
	type documentFingerprint struct {
		URI         string
		Fingerprint workspaceArtifactFingerprint
	}
	type documentSource struct {
		URI  string
		Text string
	}
	sources := make([]documentSource, 0, len(s.workspace))
	for uri, document := range s.workspace {
		if document == nil {
			continue
		}
		sources = append(sources, documentSource{URI: uri, Text: document.Text})
	}
	var includeGraph any
	if s.workspaceIncludeGraph != nil && s.workspaceIncludeGraphComplete {
		if snapshot, ok := s.workspaceIncludeGraph.Snapshot(s.workspaceIncludeGraph.SettingsKey()); ok {
			includeGraph = snapshot
		}
	}
	settings := struct {
		DefaultLanguage string
		IncludeRelated  bool
	}{s.settings.DefaultLanguage, s.settings.CodeLensIncludeRelatedIncludeTrees}
	s.mu.Unlock()
	documents := make([]documentFingerprint, len(sources))
	for index, source := range sources {
		documents[index] = documentFingerprint{URI: source.URI, Fingerprint: workspaceArtifactFingerprint(workspacepkg.DiskContentHash(source.Text))}
	}
	sort.Slice(documents, func(i, j int) bool { return documents[i].URI < documents[j].URI })
	return string(workspaceFingerprint(struct {
		Documents    []documentFingerprint
		IncludeGraph any
		Settings     any
	}{documents, includeGraph, settings}))
}

func (s *Server) workspaceIndexRunSettings() workspaceIndexRunSettings {
	s.mu.Lock()
	settings := workspaceIndexRunSettings{
		roots:            append([]workspaceRoot(nil), s.workspaceRoots...),
		includeGlobs:     append([]string(nil), s.settings.WorkspaceIncludeGlobs...),
		excludeGlobs:     append([]string(nil), s.settings.WorkspaceExcludeGlobs...),
		scanChunkSize:    s.settings.WorkspaceScanChunkSize,
		respectGitIgnore: s.settings.WorkspaceRespectGitIgnore,
		legacyEncoding:   s.settings.LegacyEncoding,
		cacheDirectory:   s.settings.CacheDirectory,
		readLimiter:      s.includeReadLimiter,
		baseDocuments:    make(map[string]workspaceArtifactFingerprint, len(s.workspace)),
	}
	for uri, document := range s.workspace {
		if document != nil {
			settings.baseDocuments[uri] = workspaceFingerprint(document.Text)
		}
	}
	if len(settings.roots) == 0 && s.rootPath != "" {
		settings.roots = []workspaceRoot{{URI: s.rootURI, Path: s.rootPath}}
	}
	s.mu.Unlock()
	return settings
}

func (s *Server) workspaceIndexBuildInterruption(ctx context.Context, generation uint64) workspaceIndexBuildResult {
	return s.classifyWorkspaceIndexBuildResult(ctx, generation, nil, false, nil)
}

func (s *Server) classifyWorkspaceIndexBuildResult(ctx context.Context, generation uint64, documents map[string]*core.TextDocument, cacheHit bool, err error) workspaceIndexBuildResult {
	s.mu.Lock()
	current := !s.workspaceIndexClosed && s.workspaceIndexGeneration == generation
	s.mu.Unlock()
	if !current {
		return incompleteWorkspaceIndexBuild(errWorkspaceIndexGeneration)
	}
	if ctx != nil {
		if ctxErr := ctx.Err(); ctxErr != nil {
			return incompleteWorkspaceIndexBuild(ctxErr)
		}
	}
	if err != nil {
		return incompleteWorkspaceIndexBuild(err)
	}
	return completeWorkspaceIndexBuild(documents, cacheHit)
}

func (s *Server) buildWorkspaceIndex(ctx context.Context, generation uint64, settings workspaceIndexRunSettings) (map[string]*core.TextDocument, bool, bool) {
	result := s.buildWorkspaceIndexWithProgress(ctx, generation, settings, nil)
	return result.documents, result.cacheHit, result.complete
}

func (s *Server) buildWorkspaceIndexWithProgress(ctx context.Context, generation uint64, settings workspaceIndexRunSettings, progress workspaceIndexProgressReporter) workspaceIndexBuildResult {
	if len(settings.roots) == 0 {
		if result := s.workspaceIndexBuildInterruption(ctx, generation); result.err != nil {
			return result
		}
		return completeWorkspaceIndexBuild(map[string]*core.TextDocument{}, false)
	}
	if progress != nil {
		progress("workspace.index.readCache", 0, 0, "")
	}
	cacheFreshness := s.effectiveCacheFreshness()
	cacheSnapshot := s.workspaceDiskCacheReadSnapshot(cacheFreshness)
	includeGraphCandidate := make(chan *workspaceIncludeGraphDiskCandidate, 1)
	go func() {
		includeGraphCandidate <- s.readWorkspaceIncludeGraphDiskCandidateBounded(ctx, cacheSnapshot)
	}()
	var joinedIncludeGraphCandidate *workspaceIncludeGraphDiskCandidate
	var joinIncludeGraphCandidateOnce sync.Once
	joinIncludeGraphCandidate := func() *workspaceIncludeGraphDiskCandidate {
		joinIncludeGraphCandidateOnce.Do(func() {
			joinedIncludeGraphCandidate = <-includeGraphCandidate
		})
		return joinedIncludeGraphCandidate
	}
	defer joinIncludeGraphCandidate()
	if cacheFreshness == "metadata" {
		s.invalidateChangedSourceSnapshots()
	}
	restored, complete, stale := s.restoreWorkspaceIndexFromDiskSnapshot(ctx, cacheFreshness, cacheSnapshot)
	cacheCandidateUsed := complete || stale
	if result := s.workspaceIndexBuildInterruption(ctx, generation); result.err != nil {
		return result
	}
	if complete {
		if progress != nil {
			progress("workspace.index.readCache", len(restored), len(restored), "analysis database")
		}
		result := completeWorkspaceIndexBuild(restored, true)
		result.includeGraphCandidate = joinIncludeGraphCandidate()
		result.cacheSnapshot = cacheSnapshot
		result.cacheCandidateUsed = cacheCandidateUsed || result.includeGraphCandidate != nil
		return result
	}
	docs := restored
	if docs == nil {
		docs = map[string]*core.TextDocument{}
	}
	type workspaceIndexScan struct {
		files          []workspaceFile
		gitIgnoreGlobs []string
		err            error
	}
	scans := make([]workspaceIndexScan, len(settings.roots))
	discovered := 0
	var discoveryProgressMu sync.Mutex
	var lastDiscoveryProgress time.Time
	s.analysisWorkers.parallelForBulk(ctx, len(settings.roots), func(workerCtx context.Context, rootIndex int) {
		root := settings.roots[rootIndex]
		if root.Path == "" {
			return
		}
		gitIgnoreGlobs := []string{}
		if settings.respectGitIgnore {
			gitIgnoreGlobs = s.readGitIgnoreGlobsContext(withSourceReadBoundaries(workerCtx, root.Path), root.Path)
		}
		filter := newWorkspaceIndexFileDiscoveryFilter(root.Path, settings.includeGlobs, settings.excludeGlobs, gitIgnoreGlobs, settings.cacheDirectory)
		scan := s.scanWorkspaceFilesWithFilterContextProgressResult(workerCtx, root.Path, settings.scanChunkSize, filter, func(file workspaceFile) {
			discoveryProgressMu.Lock()
			defer discoveryProgressMu.Unlock()
			discovered++
			if progress != nil && workspaceIndexItemProgressDue(&lastDiscoveryProgress, false) {
				progress("workspace.index.scanRoot", discovered, 0, file.Relative)
			}
		})
		scans[rootIndex] = workspaceIndexScan{files: scan.files, gitIgnoreGlobs: gitIgnoreGlobs, err: scan.err}
		discoveryProgressMu.Lock()
		defer discoveryProgressMu.Unlock()
		if progress != nil {
			progress("workspace.index.scanRoot", discovered, 0, filepath.Base(root.Path))
		}
	})
	if result := s.workspaceIndexBuildInterruption(ctx, generation); result.err != nil {
		return result
	}
	for _, scan := range scans {
		if scan.err != nil {
			return s.classifyWorkspaceIndexBuildResult(ctx, generation, nil, false, scan.err)
		}
	}
	indexedRootPaths := workspaceIndexRootPaths(settings.roots)
	trustedRoots, complete := s.trustedFilesystemRootEntriesContext(ctx, indexedRootPaths)
	if !complete {
		return s.workspaceIndexBuildInterruption(ctx, generation)
	}
	preparedTrustedRoots, complete := prepareTrustedFilesystemRoots(ctx, trustedRoots, indexedRootPaths)
	if !complete {
		return s.workspaceIndexBuildInterruption(ctx, generation)
	}
	defer closeTrustedFilesystemRoots(preparedTrustedRoots)
	readContext := withSourceReadGeneration(withSourceReadRoots(withSourceReadBoundaries(ctx, indexedRootPaths...), preparedTrustedRoots), generation)
	type workspaceIndexRead struct {
		file workspaceFile
		read bool
	}
	totalFiles := 0
	for _, scan := range scans {
		totalFiles += len(scan.files)
	}
	reads := make([]workspaceIndexRead, 0, totalFiles)
	seen := make(map[string]struct{}, len(docs)+totalFiles)
	for uri := range docs {
		seen[uri] = struct{}{}
	}
	for _, scan := range scans {
		for _, file := range scan.files {
			_, restored := seen[file.URI]
			reads = append(reads, workspaceIndexRead{file: file, read: !restored})
		}
	}
	type workspaceIndexReadResult struct {
		content string
		ok      bool
		err     error
	}
	results := make([]workspaceIndexReadResult, len(reads))
	processedFiles := 0
	var fileProgressMu sync.Mutex
	var lastFileProgress time.Time
	if progress != nil {
		progress("workspace.index.scanFiles", 0, totalFiles, "")
	}
	s.analysisWorkers.parallelForBulk(readContext, len(reads), func(workerCtx context.Context, index int) {
		read := reads[index]
		defer func() {
			fileProgressMu.Lock()
			defer fileProgressMu.Unlock()
			processedFiles++
			if progress != nil && workspaceIndexItemProgressDue(&lastFileProgress, processedFiles == totalFiles) {
				progress("workspace.index.scanFiles", processedFiles, totalFiles, read.file.Relative)
			}
		}()
		if !read.read || workerCtx.Err() != nil {
			return
		}
		content, err := s.readWorkspaceIndexFile(workerCtx, read.file.Path, settings.legacyEncoding, settings.readLimiter)
		if err == nil {
			results[index] = workspaceIndexReadResult{content: content, ok: true}
		} else {
			results[index] = workspaceIndexReadResult{err: err}
		}
	})
	if result := s.workspaceIndexBuildInterruption(ctx, generation); result.err != nil {
		return result
	}
	for index, result := range results {
		if result.err != nil {
			return s.classifyWorkspaceIndexBuildResult(ctx, generation, nil, false, result.err)
		}
		if !result.ok {
			continue
		}
		file := reads[index].file
		if _, exists := docs[file.URI]; exists {
			continue
		}
		docs[file.URI] = core.NewTextDocument(file.URI, "classic-asp", 0, result.content)
	}
	result := s.classifyWorkspaceIndexBuildResult(ctx, generation, docs, false, nil)
	result.includeGraphCandidate = joinIncludeGraphCandidate()
	result.cacheSnapshot = cacheSnapshot
	result.cacheCandidateUsed = cacheCandidateUsed || result.includeGraphCandidate != nil
	return result
}

func (s *Server) publishWorkspaceIndex(ctx context.Context, generation uint64, docs map[string]*core.TextDocument, baseDocuments map[string]workspaceArtifactFingerprint) bool {
	published, _ := s.publishWorkspaceIndexCandidate(ctx, generation, docs, baseDocuments, nil)
	return published
}

func (s *Server) publishWorkspaceIndexCandidate(ctx context.Context, generation uint64, docs map[string]*core.TextDocument, baseDocuments map[string]workspaceArtifactFingerprint, cacheSnapshot *workspaceDiskCacheReadSnapshot) (bool, bool) {
	if ctx.Err() != nil {
		return false, false
	}
	s.mu.Lock()
	defer s.mu.Unlock()
	if s.workspaceIndexClosed || s.workspaceIndexGeneration != generation || ctx.Err() != nil {
		return false, false
	}
	if cacheSnapshot != nil && !s.workspaceDiskCacheReadSnapshotCurrentLocked(cacheSnapshot) {
		return false, true
	}
	merged := make(map[string]*core.TextDocument, len(docs)+len(s.workspace))
	for uri, document := range docs {
		merged[uri] = document
	}
	for uri, current := range s.workspace {
		baseFingerprint, existedAtStart := baseDocuments[uri]
		if !existedAtStart || current == nil || workspaceFingerprint(current.Text) != baseFingerprint {
			if current != nil {
				merged[uri] = current
			}
		}
	}
	for uri := range baseDocuments {
		if _, stillPresent := s.workspace[uri]; !stillPresent {
			delete(merged, uri)
		}
	}
	for uri := range docs {
		delete(docs, uri)
	}
	for uri, document := range merged {
		docs[uri] = document
	}
	s.workspace = merged
	s.markJavaScriptDocumentsChangedLocked()
	s.clearJavaScriptProjectConfigCacheLocked()
	return true, false
}

func (s *Server) workspaceIndexGenerationCurrent(ctx context.Context, generation uint64) bool {
	if ctx.Err() != nil {
		return false
	}
	s.mu.Lock()
	current := !s.workspaceIndexClosed && s.workspaceIndexGeneration == generation
	s.mu.Unlock()
	return current
}

func (s *Server) runWorkspaceIndexTestHook(ctx context.Context, phase workspaceIndexTestPhase, generation uint64) {
	s.mu.Lock()
	hook := s.workspaceIndexTestHook
	s.mu.Unlock()
	if hook != nil {
		hook(ctx, phase, generation)
	}
}

func (s *Server) stopWorkspaceIndexWorkers() {
	s.workspaceIndexDiskCommitMu.Lock()
	s.mu.Lock()
	if !s.workspaceIndexClosed {
		s.workspaceIndexClosed = true
		s.workspaceIndexGeneration++
		s.markWorkspaceVBAutoIncludeCatalogIncompleteLocked(s.workspaceIndexGeneration)
	}
	cancel := s.workspaceIndexCancel
	s.workspaceIndexCancel = nil
	s.mu.Unlock()
	s.workspaceIndexDiskCommitMu.Unlock()
	if cancel != nil {
		cancel()
	}
	s.workspaceIndexWorkers.Wait()
}

func (s *Server) cancelWorkspaceIndexWorker() {
	s.workspaceIndexDiskCommitMu.Lock()
	s.mu.Lock()
	if s.workspaceIndexClosed || !s.workspaceIndexEnabled || s.workspaceIndexCancel == nil {
		s.mu.Unlock()
		s.workspaceIndexDiskCommitMu.Unlock()
		return
	}
	s.workspaceIndexGeneration++
	s.markWorkspaceVBAutoIncludeCatalogIncompleteLocked(s.workspaceIndexGeneration)
	cancel := s.workspaceIndexCancel
	s.workspaceIndexCancel = nil
	s.mu.Unlock()
	s.workspaceIndexDiskCommitMu.Unlock()
	if cancel != nil {
		cancel()
	}
}

func (s *Server) waitForWorkspaceIndex(ctx context.Context) bool {
	s.mu.Lock()
	done := s.workspaceIndexDone
	s.mu.Unlock()
	if done == nil {
		return true
	}
	select {
	case <-done:
		return true
	case <-ctx.Done():
		return false
	}
}

// waitForCompleteWorkspaceReferenceIndex waits until the current workspace
// generation has published both its documents and include topology. It returns
// false for a cancelled generation so callers cannot finalize a partial count.
func (s *Server) waitForCompleteWorkspaceReferenceIndex(ctx context.Context) bool {
	if ctx == nil {
		ctx = context.Background()
	}
	for {
		s.mu.Lock()
		done := s.workspaceIndexDone
		generation := s.workspaceIndexGeneration
		ready := s.workspaceReferenceIndexReadyLocked()
		s.mu.Unlock()
		if ready {
			return true
		}
		select {
		case <-done:
		case <-ctx.Done():
			return false
		}
		s.mu.Lock()
		currentDone := s.workspaceIndexDone
		currentGeneration := s.workspaceIndexGeneration
		currentReady := s.workspaceReferenceIndexReadyGeneration == currentGeneration
		s.mu.Unlock()
		if currentDone == done && currentGeneration == generation {
			return currentReady
		}
	}
}

func (s *Server) workspaceReferenceIndexReadyLocked() bool {
	return !s.workspaceIndexEnabled || s.workspaceIndexDone == nil ||
		s.workspaceReferenceIndexReadyGeneration == s.workspaceIndexGeneration
}

func (s *Server) indexWorkspaceForReferenceTest() {
	// The delayed reference-worker test changes a file before its watcher
	// notification can be dispatched. Drop test snapshots so the synchronous
	// test reindex observes the already-written source.
	if vbReferencesBatchDelay() > 0 {
		s.clearSourceSnapshots()
	}
	s.indexWorkspace()
}

func (s *Server) indexWorkspace() {
	s.activateWorkspaceIndexing(context.Background())
	s.scheduleWorkspaceIndex("internal.synchronous")
	s.waitForWorkspaceIndex(context.Background())
}

const workspaceIndexItemProgressInterval = 50 * time.Millisecond

// workspaceIndexItemProgressDue limits per-file progress updates, which take
// Server.mu, so parallel file workers do not serialize on progress reporting.
// The caller must hold the mutex that guards last.
func workspaceIndexItemProgressDue(last *time.Time, final bool) bool {
	now := time.Now()
	if !final && !last.IsZero() && now.Sub(*last) < workspaceIndexItemProgressInterval {
		return false
	}
	*last = now
	return true
}

func (s *Server) readWorkspaceIndexFile(ctx context.Context, path, legacyEncoding string, limiter chan struct{}, boundaries ...string) (string, error) {
	if len(boundaries) > 0 {
		ctx = withSourceReadBoundaries(ctx, boundaries...)
	}
	return s.readWorkspaceTextFileCached(ctx, path, legacyEncoding, limiter, false)
}

func workspaceIndexRootPaths(roots []workspaceRoot) []string {
	paths := make([]string, 0, len(roots))
	for _, root := range roots {
		if root.Path != "" {
			paths = append(paths, root.Path)
		}
	}
	return paths
}

type workspaceFileScanResult struct {
	files []workspaceFile
	err   error
}

type workspaceIndexFileDiscoveryFilter struct {
	rootPath       string
	graph          workspaceGraphFileFilter
	cacheDirectory string
}

func newWorkspaceIndexFileDiscoveryFilter(rootPath string, includeGlobs, excludeGlobs, gitIgnoreGlobs []string, cacheDirectory string) workspaceIndexFileDiscoveryFilter {
	rootPath = filepath.Clean(rootPath)
	cacheDirectory = strings.TrimSpace(cacheDirectory)
	if cacheDirectory != "" && !filepath.IsAbs(cacheDirectory) {
		cacheDirectory = filepath.Join(rootPath, cacheDirectory)
	}
	filter := workspaceIndexFileDiscoveryFilter{
		rootPath: rootPath,
		graph:    newWorkspaceGraphFileFilter(includeGlobs, excludeGlobs, map[string][]string{rootPath: gitIgnoreGlobs}),
	}
	if cacheDirectory != "" {
		filter.cacheDirectory = filepath.Clean(cacheDirectory)
	}
	return filter
}

func (filter workspaceIndexFileDiscoveryFilter) allowsFile(relative string) bool {
	if filter.isCacheDescendant(filepath.Join(filter.rootPath, filepath.FromSlash(relative))) {
		return false
	}
	return filter.graph.allows(relative, filter.rootPath)
}

func (filter workspaceIndexFileDiscoveryFilter) skipsDirectory(relative string) bool {
	if strings.Trim(relative, "/") == "" {
		return false
	}
	path := filepath.Join(filter.rootPath, filepath.FromSlash(relative))
	if filter.isCacheDescendant(path) {
		return true
	}
	if filter.configuredExcludeMatchesDirectory(relative) {
		return true
	}
	return filter.gitIgnoreDirectoryIgnoredWithoutNegation(relative)
}

func (filter workspaceIndexFileDiscoveryFilter) isCacheDescendant(path string) bool {
	if filter.cacheDirectory == "" {
		return false
	}
	rootPath := filepath.Clean(filter.rootPath)
	cacheDirectory := filepath.Clean(filter.cacheDirectory)
	return cacheDirectory != rootPath && pathWithinRoot(rootPath, cacheDirectory) && pathWithinRoot(cacheDirectory, filepath.Clean(path))
}

func (filter workspaceIndexFileDiscoveryFilter) configuredExcludeMatchesDirectory(relative string) bool {
	for _, matcher := range filter.graph.exclude {
		if workspaceGlobMatcherMatchesDirectorySubtree(matcher, relative) {
			return true
		}
	}
	return false
}

func workspaceGlobMatcherMatchesDirectorySubtree(matcher workspaceGlobMatcher, relative string) bool {
	if matcher.matches(relative) || matcher.matches(relative+"/") {
		return true
	}
	pattern := strings.TrimSuffix(matcher.pattern, "/")
	return pattern == relative+"/*" || pattern == relative+"/**" || pattern == relative+"/**/*"
}

func (filter workspaceIndexFileDiscoveryFilter) gitIgnoreDirectoryIgnoredWithoutNegation(relative string) bool {
	ignored := false
	for _, matcher := range filter.graph.gitIgnoreByRoot[filter.rootPath] {
		if workspaceGitIgnoreMatcherMatchesDirectorySubtree(matcher, relative) || workspaceGitIgnoreMatcherMatchesAncestor(matcher, relative) {
			if matcher.negated {
				ignored = false
			} else {
				ignored = true
			}
		}
	}
	if !ignored {
		return false
	}
	return !filter.gitIgnoreNegationCanReach(relative)
}

func workspaceGitIgnoreMatcherMatchesDirectorySubtree(matcher workspaceGitIgnoreMatcher, relative string) bool {
	if workspaceGitIgnoreMatcherMatches(matcher, relative) || workspaceGitIgnoreMatcherMatches(matcher, relative+"/") {
		return true
	}
	pattern := strings.TrimSuffix(matcher.matcher.pattern, "/")
	return pattern == relative+"/*" || pattern == relative+"/**" || pattern == relative+"/**/*"
}

func (filter workspaceIndexFileDiscoveryFilter) gitIgnoreNegationCanReach(relative string) bool {
	for _, matcher := range filter.graph.gitIgnoreByRoot[filter.rootPath] {
		if !matcher.negated {
			continue
		}
		pattern := strings.Trim(matcher.matcher.pattern, "/")
		if pattern == "" || strings.ContainsAny(pattern, "*?[{") {
			return true
		}
		if !strings.Contains(pattern, "/") || pattern == relative || strings.HasPrefix(pattern, relative+"/") || strings.HasPrefix(relative, pattern+"/") {
			return true
		}
	}
	return false
}

func scanWorkspaceFilesWithContextProgress(ctx context.Context, rootPath string, chunkSize int, progress func(workspaceFile)) []workspaceFile {
	result := scanWorkspaceFilesWithContextProgressResult(ctx, rootPath, chunkSize, progress, nil)
	if result.err != nil {
		return nil
	}
	return result.files
}

func (s *Server) scanWorkspaceFilesWithContextProgressResult(ctx context.Context, rootPath string, chunkSize int, progress func(workspaceFile)) workspaceFileScanResult {
	if s == nil {
		return scanWorkspaceFilesWithContextProgressResult(ctx, rootPath, chunkSize, progress, nil)
	}
	s.mu.Lock()
	hook := s.workspaceWalkDirTestHook
	s.mu.Unlock()
	return scanWorkspaceFilesWithContextProgressResult(ctx, rootPath, chunkSize, progress, hook)
}

func (s *Server) scanWorkspaceFilesWithFilterContextProgressResult(ctx context.Context, rootPath string, chunkSize int, filter workspaceIndexFileDiscoveryFilter, progress func(workspaceFile)) workspaceFileScanResult {
	if s == nil {
		return scanWorkspaceFilesWithContextProgressFilteredResult(ctx, rootPath, chunkSize, progress, nil, &filter)
	}
	s.mu.Lock()
	hook := s.workspaceWalkDirTestHook
	s.mu.Unlock()
	return scanWorkspaceFilesWithContextProgressFilteredResult(ctx, rootPath, chunkSize, progress, hook, &filter)
}

func scanWorkspaceFilesWithContextProgressResult(ctx context.Context, rootPath string, chunkSize int, progress func(workspaceFile), walkHook func(string) error) workspaceFileScanResult {
	return scanWorkspaceFilesWithContextProgressFilteredResult(ctx, rootPath, chunkSize, progress, walkHook, nil)
}

func scanWorkspaceFilesWithContextProgressFilteredResult(ctx context.Context, rootPath string, chunkSize int, progress func(workspaceFile), walkHook func(string) error, filter *workspaceIndexFileDiscoveryFilter) workspaceFileScanResult {
	if ctx == nil {
		ctx = context.Background()
	}
	if rootPath == "" {
		return workspaceFileScanResult{}
	}
	rootPath = filepath.Clean(rootPath)
	if ctx.Err() != nil {
		return workspaceFileScanResult{err: ctx.Err()}
	}
	files := []workspaceFile{}
	processed := 0
	walkErr := filepath.WalkDir(rootPath, func(path string, entry os.DirEntry, err error) error {
		if ctx.Err() != nil {
			return ctx.Err()
		}
		if walkHook != nil {
			if hookErr := walkHook(path); hookErr != nil {
				return hookErr
			}
		}
		if err != nil {
			if workspaceWalkPathIsIgnored(rootPath, path) {
				if entry != nil && entry.IsDir() {
					return filepath.SkipDir
				}
				return nil
			}
			return err
		}
		if entry == nil || entry.Type()&os.ModeSymlink != 0 {
			return nil
		}
		relative, relativeErr := filepath.Rel(rootPath, path)
		if relativeErr != nil {
			return relativeErr
		}
		if !pathWithinRoot(rootPath, path) {
			return nil
		}
		relativeSlash := filepath.ToSlash(relative)
		if entry.IsDir() {
			name := entry.Name()
			if name == ".git" || name == "node_modules" || name == "dist" || name == "out" {
				return filepath.SkipDir
			}
			if filter != nil && filter.skipsDirectory(relativeSlash) {
				return filepath.SkipDir
			}
			return nil
		}
		if !isWorkspaceASPFile(path) {
			return nil
		}
		if filter != nil && !filter.allowsFile(relativeSlash) {
			return nil
		}
		file := workspaceFile{Path: path, URI: filePathURI(path), Relative: relativeSlash}
		files = append(files, file)
		if progress != nil {
			progress(file)
		}
		processed++
		if chunkSize > 0 && processed%chunkSize == 0 {
			runtime.Gosched()
		}
		return nil
	})
	if walkErr != nil {
		if ctx.Err() != nil {
			return workspaceFileScanResult{err: ctx.Err()}
		}
		return workspaceFileScanResult{err: walkErr}
	}
	if ctx.Err() != nil {
		return workspaceFileScanResult{err: ctx.Err()}
	}
	sort.Slice(files, func(i, j int) bool { return files[i].Relative < files[j].Relative })
	if ctx.Err() != nil {
		return workspaceFileScanResult{err: ctx.Err()}
	}
	return workspaceFileScanResult{files: files}
}

func workspaceWalkPathIsIgnored(rootPath, path string) bool {
	relative, err := filepath.Rel(rootPath, path)
	if err != nil || relative == "." || !pathWithinRoot(rootPath, path) {
		return false
	}
	for _, part := range strings.Split(relative, string(filepath.Separator)) {
		switch part {
		case ".git", "node_modules", "dist", "out":
			return true
		}
	}
	return false
}

// walkWorkspaceFilesWithContextProgress visits eligible files without retaining
// them. The preview command uses this path so a large workspace cannot be
// accumulated before its response limit is applied.
func walkWorkspaceFilesWithContextProgress(ctx context.Context, rootPath string, progress func(workspaceFile)) bool {
	if ctx == nil {
		ctx = context.Background()
	}
	if ctx.Err() != nil {
		return false
	}
	if rootPath == "" {
		return true
	}
	rootPath = filepath.Clean(rootPath)
	if ctx.Err() != nil {
		return false
	}
	walkErr := filepath.WalkDir(rootPath, func(path string, entry os.DirEntry, err error) error {
		if ctx.Err() != nil {
			return ctx.Err()
		}
		if err != nil {
			if workspaceWalkPathIsIgnored(rootPath, path) {
				if entry != nil && entry.IsDir() {
					return filepath.SkipDir
				}
				return nil
			}
			return err
		}
		if entry == nil || entry.Type()&os.ModeSymlink != 0 {
			return nil
		}
		if entry.IsDir() {
			name := entry.Name()
			if name == ".git" || name == "node_modules" || name == "dist" || name == "out" {
				return filepath.SkipDir
			}
			return nil
		}
		if !isWorkspaceASPFile(path) {
			return nil
		}
		relative, err := filepath.Rel(rootPath, path)
		if err != nil {
			return err
		}
		if !pathWithinRoot(rootPath, path) {
			return nil
		}
		if progress != nil {
			progress(workspaceFile{Path: path, URI: filePathURI(path), Relative: filepath.ToSlash(relative)})
		}
		return nil
	})
	if walkErr != nil {
		return false
	}
	return ctx.Err() == nil
}

// scanWorkspaceFilesWithContextProgressBounded retains at most maxFiles files
// selected by keep while still visiting the complete tree. A cancelled walk
// returns nil, never the files collected before cancellation.
func scanWorkspaceFilesWithContextProgressBounded(ctx context.Context, rootPath string, maxFiles int, keep func(workspaceFile) bool) []workspaceFile {
	if maxFiles < 0 {
		maxFiles = 0
	}
	files := make([]workspaceFile, 0, min(maxFiles, 64))
	completed := walkWorkspaceFilesWithContextProgress(ctx, rootPath, func(file workspaceFile) {
		selected := keep == nil || keep(file)
		if !selected || len(files) >= maxFiles {
			return
		}
		files = append(files, file)
	})
	if !completed {
		return nil
	}
	sort.Slice(files, func(i, j int) bool { return files[i].Relative < files[j].Relative })
	return files
}
