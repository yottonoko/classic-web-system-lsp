package lspserver

import (
	"context"
	"encoding/json"
	"errors"
	"os"
	"path/filepath"
	"time"

	"github.com/yottonoko/classic-web-system-lsp/internal/core"
	"github.com/yottonoko/classic-web-system-lsp/internal/graph"
	"github.com/yottonoko/classic-web-system-lsp/internal/lsp"
	workspacepkg "github.com/yottonoko/classic-web-system-lsp/internal/workspace"
)

func (s *Server) readDiskDiagnosticsContext(ctx context.Context, doc *core.TextDocument, parsed *core.ParsedDocument) ([]lsp.Diagnostic, bool) {
	cache := s.diskCacheForUse()
	if cache == nil || !cache.Enabled() {
		return nil, false
	}
	entry, ok := cache.ReadFileBundle(s.diagnosticsDiskLookupContext(ctx, doc, parsed))
	if ok {
		s.logAnalysisDatabaseEvent("diagnostics", "hit", map[string]any{
			"diagnostics": len(entry.Diagnostics), "includes": len(parsed.Includes), "settingsKey": shortLogKey(entry.SettingsKey), "uri": doc.URI,
		})
	}
	return entry.Diagnostics, ok
}

func (s *Server) writeDiskDiagnosticsContext(ctx context.Context, doc *core.TextDocument, parsed *core.ParsedDocument, diagnostics []lsp.Diagnostic) {
	cache := s.diskCacheForUse()
	if cache == nil || !cache.Enabled() || doc == nil || parsed == nil {
		return
	}
	snapshot := core.NewTextDocument(doc.URI, doc.LanguageID, doc.Version, doc.Text)
	lookup := s.diagnosticsDiskLookupContext(ctx, snapshot, parsed)
	validateSource := s.diskCacheUsesDefaultDirectory()
	write := func() {
		s.writeDiskDiagnosticsToCache(cache, snapshot, parsed, diagnostics, lookup, validateSource)
	}
	if validateSource {
		s.runAsyncDiskCacheWrite(write)
		return
	}
	write()
}

func (s *Server) writeDiskDiagnosticsToCache(cache *workspacepkg.DiskAnalysisCache, doc *core.TextDocument, parsed *core.ParsedDocument, diagnostics []lsp.Diagnostic, lookup workspacepkg.DiskAnalysisCacheLookup, validateSource bool) {
	if cache == nil || !cache.Enabled() || doc == nil || parsed == nil {
		return
	}
	if validateSource && !s.diskSourceStillCurrent(doc, lookup.Source) {
		return
	}
	entry := workspacepkg.DiskFileBundleCacheEntry{
		DiskAnalysisCacheLookup: lookup,
		UpdateDiagnostics:       true,
		Diagnostics:             diagnostics,
	}
	if s.diskCacheForUse() != cache {
		return
	}
	if err := cache.WriteFileBundle(entry); err != nil {
		s.logServerWarning("[asp-lsp] analysisDatabase.diagnostics.write.failed: " + err.Error())
		return
	}
	s.logAnalysisDatabaseEvent("diagnostics", "write", map[string]any{
		"diagnostics": len(diagnostics), "includes": len(parsed.Includes), "settingsKey": shortLogKey(lookup.SettingsKey), "uri": doc.URI,
	})
}

func (s *Server) checkMemoryPressure(reason string) workspacepkg.MemoryPressureResult {
	s.mu.Lock()
	manager := s.memoryBudget
	maxBytes := s.settings.MemoryMaxCacheBytes
	debugTelemetry := s.settings.MemoryDebugTelemetry
	s.mu.Unlock()
	if manager == nil {
		return workspacepkg.MemoryPressureResult{}
	}
	result := manager.CheckPressure(reason, maxBytes)
	if debugTelemetry || result.EvictedBytes > 0 {
		payload, _ := json.Marshal(result)
		s.logDebugSummary("[asp-lsp] memory.pressure: " + string(payload))
	}
	return result
}

// scheduleMemoryPressureCheckLocked coalesces allocation-triggered checks so
// graph, reference, and semantic hot paths only pay for resetting one timer.
// Checks never overlap: a request made while one runs is deferred until it
// finishes. The delay grows with the cost of the previous check so accounting,
// which walks every cached document under s.mu, stays a small share of server
// time. The caller must hold s.mu.
func (s *Server) scheduleMemoryPressureCheckLocked(reason string) {
	if s.shutdown {
		return
	}
	if reason != "" {
		s.memoryPressureReason = reason
	}
	s.memoryPressurePending = true
	if s.memoryPressureTimer != nil || s.memoryPressureCheckRunning {
		return
	}
	s.memoryPressureTimer = time.AfterFunc(memoryPressureCheckDelay(s.memoryPressureCheckCost), s.runScheduledMemoryPressureCheck)
}

func (s *Server) runScheduledMemoryPressureCheck() {
	s.mu.Lock()
	reason := s.memoryPressureReason
	s.memoryPressureReason = ""
	s.memoryPressurePending = false
	s.memoryPressureTimer = nil
	if s.shutdown {
		s.mu.Unlock()
		return
	}
	s.memoryPressureCheckRunning = true
	manager := s.memoryBudget
	maxBytes := s.settings.MemoryMaxCacheBytes
	s.mu.Unlock()
	checked := false
	var cost time.Duration
	if manager == nil || !manager.HeapBelowBudget(maxBytes) {
		started := time.Now()
		s.checkMemoryPressure(reason)
		cost = time.Since(started)
		checked = true
	}
	s.mu.Lock()
	s.finishMemoryPressureCheckLocked(checked, cost)
	s.mu.Unlock()
}

// finishMemoryPressureCheckLocked records a finished check and schedules the
// requests deferred while it ran. The caller must hold s.mu.
func (s *Server) finishMemoryPressureCheckLocked(checked bool, cost time.Duration) {
	s.memoryPressureCheckRunning = false
	if checked {
		s.memoryPressureCheckCost = cost
	}
	if s.memoryPressurePending {
		s.scheduleMemoryPressureCheckLocked("")
	}
}

const (
	memoryPressureCheckCostFactor = 10
	memoryPressureCheckMaxDelay   = 30 * time.Second
)

func memoryPressureCheckDelay(previousCost time.Duration) time.Duration {
	delay := previousCost * memoryPressureCheckCostFactor
	return min(max(delay, runtimeMemoryPressureDebounce), memoryPressureCheckMaxDelay)
}

func (s *Server) scheduleMemoryPressureCheck(reason string) {
	s.mu.Lock()
	s.scheduleMemoryPressureCheckLocked(reason)
	s.mu.Unlock()
}

func (s *Server) clearRuntimeDiskCache() {
	s.clearRuntimeDiskCacheContext(context.Background())
}

func (s *Server) clearRuntimeDiskCacheContext(ctx context.Context) bool {
	if ctx == nil {
		ctx = context.Background()
	}
	if ctx.Err() != nil {
		return false
	}
	s.pauseWorkspaceIndexScheduling(true)
	schedulingPaused := true
	defer func() {
		if schedulingPaused {
			s.pauseWorkspaceIndexScheduling(false)
		}
	}()
	s.cancelWorkspaceIndexWorker()
	s.workspaceIndexWorkers.Wait()
	if hook := s.runtimeCacheDiskClearBeforeLifecycleTestHook; hook != nil {
		hook(ctx)
	}
	if ctx.Err() != nil {
		return false
	}
	s.workspaceIndexDiskCacheUseMu.Lock()
	defer s.workspaceIndexDiskCacheUseMu.Unlock()
	s.pauseAsyncDiskCacheWrites()
	writesPaused := true
	defer func() {
		if writesPaused {
			s.resumeAsyncDiskCacheWrites()
		}
	}()

	s.mu.Lock()
	directory := s.settings.CacheDirectory
	indexEnabled := s.workspaceIndexEnabled && !s.workspaceIndexClosed && !s.shutdown
	s.mu.Unlock()
	s.closeDiskAnalysisCache()
	root := diskCacheRootDirectory(directory)
	removeErr := os.RemoveAll(filepath.Join(root, "bbolt-v1"))
	legacyErr := s.cleanupLegacyDiskCacheEntries(root, nil)
	if legacyErr != nil && os.IsNotExist(legacyErr) {
		legacyErr = nil
	}
	if err := errors.Join(removeErr, legacyErr); err != nil {
		s.logServerWarning("[asp-lsp] analysisDatabase.clear.failed: " + err.Error())
	}

	cache, settings := s.replaceDiskAnalysisCache()
	s.resumeAsyncDiskCacheWrites()
	writesPaused = false
	s.pauseWorkspaceIndexScheduling(false)
	schedulingPaused = false
	if cache != nil {
		ttl := time.Duration(settings.CacheTTLHours) * time.Hour
		maxSize := int64(settings.CacheMaxSizeMB) * 1024 * 1024
		s.sweepDiskCacheIfDue(cache, ttl, maxSize)
		s.scheduleLegacyDiskCacheCleanup(cache, settings.CacheDirectory)
	}
	if indexEnabled && ctx.Err() == nil {
		s.scheduleWorkspaceIndex("command.clearDiskCache")
	}
	return ctx.Err() == nil
}

func (s *Server) shutdownRuntimeCaches() {
	invalidateJavaScriptProjectDiscoverySettings(s)
	// Serialize the shutdown transition with a staged workspace-index commit.
	// The commit path takes workspaceIndexDiskCommitMu before s.mu and never
	// holds s.mu across cache I/O, so this transition must use the same order.
	s.workspaceIndexDiskCommitMu.Lock()
	s.mu.Lock()
	s.shutdown = true
	for _, state := range s.referenceBatch {
		if state.cancel != nil {
			state.cancel()
		}
	}
	for key, flight := range s.semanticInflight {
		flight.invalidate()
		delete(s.semanticInflight, key)
	}
	if s.legacyUndefinedGlobalBackgroundCancel != nil {
		s.legacyUndefinedGlobalBackgroundCancel()
	}
	s.mu.Unlock()
	s.workspaceIndexDiskCommitMu.Unlock()
	s.stopGraphBackgroundTasks()
	s.stopProgressPublisher()
	s.stopScheduledDiagnostics()
	s.mu.Lock()
	if s.memoryPressureTimer != nil {
		s.memoryPressureTimer.Stop()
		s.memoryPressureTimer = nil
		s.memoryPressureReason = ""
	}
	s.memoryPressurePending = false
	s.codeLensRefreshSequence++
	if s.codeLensRefreshTimer != nil {
		s.codeLensRefreshTimer.Stop()
		s.codeLensRefreshTimer = nil
	}
	s.mu.Unlock()
	s.stopWorkspaceIndexWorkers()
	s.stopDocumentOpenAnalysisWorkers()
	s.backgroundAnalysisWorkers.Wait()
	s.waitForAsyncDiskCacheWrites()
	s.trustedPaths.closeRoots()
	s.closeDiskAnalysisCache()
}

func (s *Server) clearRuntimeProcessCachesContext(ctx context.Context) bool {
	if ctx == nil {
		ctx = context.Background()
	}
	if ctx.Err() != nil {
		return false
	}
	s.invalidateGitIgnoreGlobs()
	s.stopDocumentOpenAnalysisWorkers()
	if ctx.Err() != nil {
		return false
	}
	s.mu.Lock()
	for _, state := range s.referenceBatch {
		if state.cancel != nil {
			state.cancel()
		}
	}
	s.parsedCache = map[string]parsedDocumentCacheEntry{}
	for key := range s.parsedCacheRevisions {
		s.advanceParsedCacheRevisionLocked(key)
	}
	s.semantic = map[string]semanticTokenCache{}
	s.semanticHistory = map[string][]int{}
	s.referenceBatch = map[workspaceReferenceBatchKey]*workspaceReferenceBatchState{}
	s.referenceCounts = map[workspaceReferenceTargetKey]int{}
	s.referencePartialCounts = map[workspaceReferenceTargetKey]int{}
	s.referenceResults = map[workspaceReferenceTargetKey][]lsp.Location{}
	s.referenceInflight = map[workspaceReferenceTargetKey]*workspaceReferenceInflight{}
	s.referenceDocuments = map[string][]*core.ParsedDocument{}
	s.referenceScopes = map[workspaceReferenceScopeCacheKey]workspaceReferenceScopeSnapshot{}
	s.referenceImplicitPlans = map[workspaceReferenceImplicitPlanKey]map[string]map[string]struct{}{}
	s.referencePreviousCounts = map[workspaceReferencePreviousKey]int{}
	s.referenceNameRevisions = map[string]uint64{}
	s.referenceCountSummariesRestored = map[*core.ParsedDocument]struct{}{}
	s.referenceDeclarationPlans = map[*core.ParsedDocument]workspaceReferenceDeclarationPlan{}
	s.referenceDescriptorFingerprints = map[string]*workspaceReferenceDescriptorFingerprintCache{}
	s.resetWorkspaceReferenceNameIndexLocked()
	s.referenceGeneration++
	s.validatedDocumentVersions = map[string]int{}
	s.publishedDiagnosticTargets = map[string]map[string]string{}
	s.publishedDiagnosticItems = map[string]map[string][]lsp.Diagnostic{}
	s.publishedDiagnosticRevisions = map[string]map[string]diagnosticTargetRevision{}
	s.workspaceDiagnosticsItems = map[string]workspaceDiagnosticsItemCacheEntry{}
	s.workspaceDiagnosticsRevisions = map[string]uint64{}
	s.markWorkspaceDiagnosticsChanged()
	s.workspaceArtifacts = map[workspaceDocumentID]*workspaceDocumentArtifactManifest{}
	s.workspaceArtifactRevisions = map[workspaceDocumentID]uint64{}
	s.markWorkspaceVBAutoIncludeCatalogIncompleteLocked(s.workspaceIndexGeneration)
	s.consumersByExportName = map[string]map[workspaceDocumentID]struct{}{}
	s.queriesByDocumentName = map[workspaceDocumentID]map[string]struct{}{}
	s.scopesByDocument = map[workspaceDocumentID]map[string]struct{}{}
	s.implicitPlansByFamilyName = map[string]map[string]struct{}{}
	s.graphCache = map[string]graph.Payload{}
	s.resetJavaScriptProjectLocked()
	s.workspaceIncludeGraph = workspacepkg.NewWorkspaceIncludeGraph()
	s.workspaceIncludeGraphRevision = 0
	s.workspaceIncludeGraphComplete = false
	s.documentStore = workspacepkg.NewDocumentStore()
	s.clearSourceSnapshotsLocked()
	s.mu.Unlock()
	s.referenceWorkspaceIndex.clear()
	s.clearAnalysisCache()
	return ctx.Err() == nil
}
