package lspserver

import (
	"context"
	"encoding/json"
	"path/filepath"
	"sort"
	"strings"
	"sync"

	"github.com/yottonoko/classic-web-system-lsp/internal/core"
	"github.com/yottonoko/classic-web-system-lsp/internal/vbscript"
	workspacepkg "github.com/yottonoko/classic-web-system-lsp/internal/workspace"
)

const workspaceDocumentArtifactSchemaVersion uint32 = 1

type workspaceDocumentRevisionResult struct {
	Manifest  *workspaceDocumentArtifactManifest
	Delta     workspaceDocumentArtifactDelta
	Duplicate bool
	Stale     bool
}

type workspaceArtifactQueueKey struct {
	server     *Server
	documentID workspaceDocumentID
}

type workspaceArtifactQueueState struct {
	mu   sync.Mutex
	refs int
}

var workspaceArtifactQueueStates = struct {
	sync.Mutex
	states map[workspaceArtifactQueueKey]*workspaceArtifactQueueState
}{
	states: make(map[workspaceArtifactQueueKey]*workspaceArtifactQueueState),
}

var workspaceArtifactQueueTestHook struct {
	sync.Mutex
	fn func()
}

var workspaceArtifactReferenceIndexTestHook struct {
	sync.Mutex
	fn func()
}

func workspaceArtifactQueueStateFor(key workspaceArtifactQueueKey) *workspaceArtifactQueueState {
	workspaceArtifactQueueStates.Lock()
	state := workspaceArtifactQueueStates.states[key]
	if state == nil {
		state = &workspaceArtifactQueueState{}
		workspaceArtifactQueueStates.states[key] = state
	}
	state.refs++
	workspaceArtifactQueueStates.Unlock()
	return state
}

func releaseWorkspaceArtifactQueueState(key workspaceArtifactQueueKey, state *workspaceArtifactQueueState) {
	workspaceArtifactQueueStates.Lock()
	state.refs--
	if state.refs == 0 {
		delete(workspaceArtifactQueueStates.states, key)
	}
	workspaceArtifactQueueStates.Unlock()
}

// lockWorkspaceArtifactQueueIfCurrent atomically validates ownership of the
// current artifact revision and reserves its per-document queue position. The
// queue lock is distinct from s.mu so the cache enqueue can wait on disk/cache
// internals without blocking publication or cancellation of other revisions.
func (s *Server) lockWorkspaceArtifactQueueIfCurrent(manifest *workspaceDocumentArtifactManifest, revision uint64) (func(), bool) {
	if manifest == nil {
		return nil, false
	}
	key := workspaceArtifactQueueKey{server: s, documentID: manifest.DocumentID}
	s.mu.Lock()
	if !s.workspaceDocumentArtifactRevisionCurrentLocked(manifest, revision) {
		s.mu.Unlock()
		return nil, false
	}
	state := workspaceArtifactQueueStateFor(key)
	s.mu.Unlock()

	// Waiting for an older queue owner must not hold s.mu: its cache enqueue can
	// block on the disk writer. Revalidate after taking the queue position so a
	// superseded revision cancels instead of overtaking the newer owner.
	state.mu.Lock()
	s.mu.Lock()
	if !s.workspaceDocumentArtifactRevisionCurrentLocked(manifest, revision) {
		s.mu.Unlock()
		state.mu.Unlock()
		releaseWorkspaceArtifactQueueState(key, state)
		return nil, false
	}
	s.mu.Unlock()
	return func() {
		state.mu.Unlock()
		releaseWorkspaceArtifactQueueState(key, state)
	}, true
}

func runWorkspaceArtifactQueueTestHook() {
	workspaceArtifactQueueTestHook.Lock()
	hook := workspaceArtifactQueueTestHook.fn
	workspaceArtifactQueueTestHook.Unlock()
	if hook != nil {
		hook()
	}
}

func runWorkspaceArtifactReferenceIndexTestHook() {
	workspaceArtifactReferenceIndexTestHook.Lock()
	hook := workspaceArtifactReferenceIndexTestHook.fn
	workspaceArtifactReferenceIndexTestHook.Unlock()
	if hook != nil {
		hook()
	}
}

// applyWorkspaceDocumentRevision is the single publish point for source-derived
// workspace state. Parsing and artifact construction happen before the server
// lock; the lock only performs the current-source check and immutable swap.
func (s *Server) applyWorkspaceDocumentRevision(doc *core.TextDocument, parsed *core.ParsedDocument) workspaceDocumentRevisionResult {
	return s.applyWorkspaceDocumentRevisionIfCurrent(doc, parsed, nil)
}

func (s *Server) applyWorkspaceDocumentRevisionIfCurrent(doc *core.TextDocument, parsed *core.ParsedDocument, currentLocked func() bool) workspaceDocumentRevisionResult {
	return s.applyWorkspaceDocumentRevisionWithSnapshotIfCurrent(doc, parsed, nil, currentLocked)
}

func (s *Server) applyWorkspaceDocumentRevisionWithSnapshotIfCurrent(doc *core.TextDocument, parsed *core.ParsedDocument, snapshot *fileAnalysisSnapshot, currentLocked func() bool) workspaceDocumentRevisionResult {
	return s.applyWorkspaceDocumentRevisionWithSnapshotIfCurrentAndPublished(doc, parsed, snapshot, currentLocked, nil)
}

// applyWorkspaceDocumentRevisionWithSnapshotIfCurrentAndPublished is the
// internal variant used by coalesced document analysis. publishedLocked runs
// while s.mu is held, immediately after the immutable manifest swap, so a
// cancellation cannot race past the publication side-effect barrier.
func (s *Server) applyWorkspaceDocumentRevisionWithSnapshotIfCurrentAndPublished(doc *core.TextDocument, parsed *core.ParsedDocument, snapshot *fileAnalysisSnapshot, currentLocked func() bool, publishedLocked func()) workspaceDocumentRevisionResult {
	if doc == nil || parsed == nil {
		return workspaceDocumentRevisionResult{}
	}
	s.mu.Lock()
	if currentLocked != nil && !currentLocked() {
		s.mu.Unlock()
		return workspaceDocumentRevisionResult{Stale: true}
	}
	currentDocument := s.openDocumentByURILocked(doc.URI)
	if currentDocument == nil {
		currentDocument = s.workspaceDocumentByURILocked(doc.URI)
	}
	staleBeforeBuild := currentLocked == nil && currentDocument != nil && workspaceFingerprint(currentDocument.Text) != workspaceFingerprint(doc.Text)
	s.mu.Unlock()
	if staleBeforeBuild {
		return workspaceDocumentRevisionResult{Stale: true}
	}
	sourceFingerprint := workspaceFingerprint(doc.Text)
	documentID := workspaceDocumentIDFromURI(doc.URI)
	s.mu.Lock()
	parserSettings := s.settings.DefaultLanguage
	previous := s.workspaceArtifacts[documentID]
	parserSettingsFingerprint := workspaceFingerprint(struct {
		Settings        string
		DefaultLanguage core.EmbeddedLanguage
	}{parserSettings, parsed.DefaultLanguage})
	if previous != nil && previous.SourceFingerprint == sourceFingerprint && previous.ParserSettingsFingerprint == parserSettingsFingerprint {
		revision := s.workspaceArtifactRevisions[documentID]
		persistBacking := s.workspaceReferenceManifestMatchesBackingLocked(previous)
		s.mu.Unlock()
		if persistBacking {
			s.persistWorkspaceReferenceDocument(previous, vbscript.BuildReferenceShard(parsed), revision)
		}
		return workspaceDocumentRevisionResult{Manifest: previous, Duplicate: true}
	}
	s.mu.Unlock()
	if snapshot == nil {
		snapshot = s.ensureFileAnalysisSnapshot(doc, parsed, parserSettings)
	}
	manifest := buildWorkspaceDocumentArtifactManifest(parsed, snapshot, parserSettings)
	if manifest == nil {
		return workspaceDocumentRevisionResult{}
	}

	s.mu.Lock()
	if currentLocked != nil && !currentLocked() {
		s.mu.Unlock()
		return workspaceDocumentRevisionResult{Manifest: manifest, Stale: true}
	}
	current := s.openDocumentByURILocked(doc.URI)
	if current == nil {
		current = s.workspaceDocumentByURILocked(doc.URI)
	}
	if currentLocked == nil && current != nil && workspaceFingerprint(current.Text) != manifest.SourceFingerprint {
		s.mu.Unlock()
		return workspaceDocumentRevisionResult{Manifest: manifest, Stale: true}
	}
	previous = s.workspaceArtifacts[manifest.DocumentID]
	if previous != nil && previous.SourceFingerprint == manifest.SourceFingerprint && previous.ParserSettingsFingerprint == manifest.ParserSettingsFingerprint {
		revision := s.workspaceArtifactRevisions[manifest.DocumentID]
		persistBacking := s.workspaceReferenceManifestMatchesBackingLocked(previous)
		s.mu.Unlock()
		if persistBacking {
			s.persistWorkspaceReferenceDocument(previous, snapshot.ReferenceShard, revision)
		}
		return workspaceDocumentRevisionResult{Manifest: previous, Duplicate: true}
	}
	delta := compareWorkspaceDocumentArtifacts(previous, manifest)
	s.updateWorkspaceArtifactConsumersLocked(previous, manifest)
	s.workspaceArtifacts[manifest.DocumentID] = manifest
	s.workspaceArtifactRevisions[manifest.DocumentID]++
	s.updateWorkspaceVBAutoIncludeCatalogLocked(previous, manifest)
	revision := s.workspaceArtifactRevisions[manifest.DocumentID]
	persistBacking := s.workspaceReferenceManifestMatchesBackingLocked(manifest)
	if publishedLocked != nil {
		publishedLocked()
	}
	s.mu.Unlock()

	if !s.workspaceDocumentArtifactRevisionCurrent(manifest, revision) {
		return workspaceDocumentRevisionResult{Manifest: manifest, Delta: delta, Stale: true}
	}
	if !s.applyWorkspaceArtifactDelta(previous, manifest, snapshot.ReferenceShard, delta, revision) {
		return workspaceDocumentRevisionResult{Manifest: manifest, Delta: delta, Stale: true}
	}
	if !s.queueWorkspaceDocumentArtifactDeltaIfCurrent(manifest, delta, revision) {
		return workspaceDocumentRevisionResult{Manifest: manifest, Delta: delta, Stale: true}
	}
	if persistBacking {
		s.persistWorkspaceReferenceDocument(manifest, snapshot.ReferenceShard, revision)
	}
	s.logDebugSummaryEvent("workspaceArtifact.apply", "[asp-lsp] workspaceArtifact.apply uri="+manifest.URI, map[string]any{
		"artifactBuilds":   1,
		"artifactsChanged": workspaceArtifactDeltaChangeCount(delta),
		"dbRecordsChanged": workspaceArtifactDeltaPersistenceWriteCount(delta),
		"namesChanged":     len(workspaceArtifactChangedNames(delta)),
		"sourceRevisions":  1,
		"uri":              manifest.URI,
	})
	return workspaceDocumentRevisionResult{Manifest: manifest, Delta: delta}
}

func (s *Server) workspaceDocumentArtifactRevisionCurrent(manifest *workspaceDocumentArtifactManifest, revision uint64) bool {
	s.mu.Lock()
	defer s.mu.Unlock()
	return s.workspaceDocumentArtifactRevisionCurrentLocked(manifest, revision)
}

func (s *Server) workspaceDocumentArtifactRevisionCurrentLocked(manifest *workspaceDocumentArtifactManifest, revision uint64) bool {
	return manifest != nil && s.workspaceArtifacts[manifest.DocumentID] == manifest && s.workspaceArtifactRevisions[manifest.DocumentID] == revision
}

func (s *Server) workspaceReferenceManifestMatchesBackingLocked(manifest *workspaceDocumentArtifactManifest) bool {
	if manifest == nil {
		return false
	}
	backing := s.workspaceDocumentByURILocked(manifest.URI)
	return backing != nil && workspaceFingerprint(backing.Text) == manifest.SourceFingerprint
}

func (s *Server) updateWorkspaceArtifactConsumersLocked(previous, current *workspaceDocumentArtifactManifest) {
	if current == nil {
		return
	}
	documentID := current.DocumentID
	if previous != nil {
		for name := range previous.ExternalUsages {
			consumers := s.consumersByExportName[name]
			delete(consumers, documentID)
			if len(consumers) == 0 {
				delete(s.consumersByExportName, name)
			}
		}
	}
	queries := make(map[string]struct{}, len(current.ExternalUsages))
	for name := range current.ExternalUsages {
		name = strings.ToLower(name)
		queries[name] = struct{}{}
		consumers := s.consumersByExportName[name]
		if consumers == nil {
			consumers = map[workspaceDocumentID]struct{}{}
			s.consumersByExportName[name] = consumers
		}
		consumers[documentID] = struct{}{}
	}
	s.queriesByDocumentName[documentID] = queries
}

func (s *Server) removeWorkspaceDocumentRevision(uri string) workspaceDocumentRevisionResult {
	documentID := workspaceDocumentIDFromURI(uri)
	s.mu.Lock()
	previous := s.workspaceArtifacts[documentID]
	s.workspaceArtifactRevisions[documentID]++
	revision := s.workspaceArtifactRevisions[documentID]
	if previous == nil {
		s.removeWorkspaceVBAutoIncludeCatalogDocumentLocked(documentID)
		update := s.referenceWorkspaceIndex.removeDocument(uri)
		s.mu.Unlock()
		s.queueWorkspaceReferenceDocumentTombstoneIfCurrent(uri, documentID, revision)
		s.queueWorkspaceVBExportArtifactTombstoneIfCurrent(documentID, revision)
		if len(update.CountAffectedNames) > 0 {
			s.invalidateWorkspaceReferenceNames(nil, nil, update.CountAffectedNames)
		}
		if len(update.LocationAffectedNames) > 0 {
			s.invalidateWorkspaceReferenceLocations(update.LocationAffectedNames)
		}
		return workspaceDocumentRevisionResult{}
	}
	for name := range previous.ExternalUsages {
		consumers := s.consumersByExportName[name]
		delete(consumers, documentID)
		if len(consumers) == 0 {
			delete(s.consumersByExportName, name)
		}
	}
	delete(s.queriesByDocumentName, documentID)
	delete(s.workspaceArtifacts, documentID)
	s.removeWorkspaceVBAutoIncludeCatalogDocumentLocked(documentID)
	update := s.referenceWorkspaceIndex.removeDocument(uri)
	s.mu.Unlock()
	s.queueWorkspaceReferenceDocumentTombstoneIfCurrent(uri, documentID, revision)
	s.queueWorkspaceVBExportArtifactTombstoneIfCurrent(documentID, revision)

	delta := compareWorkspaceDocumentArtifacts(previous, nil)
	countNames := workspaceUniqueNames(
		delta.ChangedPublicNames,
		delta.ChangedUsageNames,
		delta.ChangedImplicitNames,
		delta.ChangedObjectTagNames,
		delta.ChangedReferenceCounts,
		update.CountAffectedNames,
	)
	locationNames := workspaceUniqueNames(delta.ChangedReferenceLocations, update.LocationAffectedNames)
	if len(countNames) > 0 {
		s.invalidateWorkspaceReferenceNames(previous.CST, nil, countNames)
	}
	if len(locationNames) > 0 {
		s.invalidateWorkspaceReferenceLocations(locationNames)
	}
	return workspaceDocumentRevisionResult{Manifest: previous, Delta: delta}
}

func (s *Server) applyWorkspaceArtifactDelta(previous, current *workspaceDocumentArtifactManifest, referenceShard *vbscript.ReferenceShard, delta workspaceDocumentArtifactDelta, revision uint64) bool {
	if current == nil || !s.workspaceDocumentArtifactRevisionCurrent(current, revision) {
		return false
	}
	countReuse := s.prepareWorkspaceReferenceDocumentCountReuse(previous, current, delta)
	if !s.workspaceDocumentArtifactRevisionCurrent(current, revision) {
		return false
	}
	runWorkspaceArtifactReferenceIndexTestHook()
	indexUpdate, indexCurrent := s.updateWorkspaceReferenceIndexIfCurrent(current.CST, referenceShard, current, revision)
	if !indexCurrent {
		return false
	}
	if countReuse != nil {
		countReuse.expectedIndexRevision = s.referenceWorkspaceIndex.revisionNumber()
	}
	countNames := workspaceUniqueNames(
		delta.ChangedPublicNames,
		delta.ChangedUsageNames,
		delta.ChangedImplicitNames,
		delta.ChangedObjectTagNames,
		delta.ChangedReferenceCounts,
		indexUpdate.CountAffectedNames,
	)
	locationNames := workspaceUniqueNames(delta.ChangedReferenceLocations, indexUpdate.LocationAffectedNames)
	if len(countNames) > 0 {
		var previousParsed *core.ParsedDocument
		if previous != nil {
			previousParsed = previous.CST
		}
		s.invalidateWorkspaceReferenceNames(previousParsed, current.CST, countNames)
	}
	if !s.workspaceDocumentArtifactRevisionCurrent(current, revision) {
		return false
	}
	if len(locationNames) > 0 {
		s.invalidateWorkspaceReferenceLocations(locationNames)
	}
	if !s.workspaceDocumentArtifactRevisionCurrent(current, revision) {
		return false
	}
	s.publishWorkspaceReferenceDocumentCountReuse(countReuse)
	if len(delta.ChangedVirtualLanguages) > 0 && workspaceVirtualDeltaHasJavaScript(delta.ChangedVirtualLanguages) {
		s.mu.Lock()
		if !s.workspaceDocumentArtifactRevisionCurrentLocked(current, revision) {
			s.mu.Unlock()
			return false
		}
		// didChange registers an open overlay synchronously before requests can
		// observe the new revision. The later artifact publish must not advance
		// the project generation a second time for that same owner.
		if s.openDocumentByURILocked(current.URI) == nil {
			s.markJavaScriptDocumentChangedLocked(current.URI)
		}
		s.mu.Unlock()
	}
	// Source metadata belongs to the changed owner. ApplyEdgeDelta keeps this a
	// metadata-only update when the ordered direct edges are unchanged.
	if !s.refreshWorkspaceIncludeGraphFileIfCurrent(current.CST, current, revision) {
		return false
	}
	if delta.IncludeEdgesChanged {
		if !s.workspaceDocumentArtifactRevisionCurrent(current, revision) {
			return false
		}
		if !s.invalidateWorkspaceReferenceTopology(previousCST(previous), current.CST) && previous != nil {
			s.clearWorkspaceReferenceCache()
		}
	}
	if len(delta.ChangedDiagnosticLayers) > 0 || len(delta.ChangedPublicNames) > 0 || len(delta.ChangedImplicitNames) > 0 || len(delta.ChangedObjectTagNames) > 0 || delta.IncludeEdgesChanged {
		if !s.workspaceDocumentArtifactRevisionCurrent(current, revision) {
			return false
		}
		s.invalidateWorkspaceDiagnosticsForArtifactDelta(current, delta)
	}
	return s.workspaceDocumentArtifactRevisionCurrent(current, revision)
}

// updateWorkspaceReferenceIndexIfCurrent prepares reference segments without
// holding the server lock, then atomically validates the owning artifact and
// applies the prepared segments while holding s.mu. This prevents a stale
// artifact worker from mutating the index after a newer artifact publishes.
func (s *Server) updateWorkspaceReferenceIndexIfCurrent(parsed *core.ParsedDocument, referenceShard *vbscript.ReferenceShard, manifest *workspaceDocumentArtifactManifest, revision uint64) (workspaceReferenceIndexUpdate, bool) {
	if manifest == nil {
		return workspaceReferenceIndexUpdate{}, false
	}
	preparedUpdate, prepared := s.referenceWorkspaceIndex.prepareContextModeWithShards(
		context.Background(),
		[]*core.ParsedDocument{parsed},
		map[*core.ParsedDocument]*vbscript.ReferenceShard{parsed: referenceShard},
		true,
	)
	s.mu.Lock()
	if !s.workspaceDocumentArtifactRevisionCurrentLocked(manifest, revision) {
		s.mu.Unlock()
		return workspaceReferenceIndexUpdate{}, false
	}
	if prepared == nil {
		s.mu.Unlock()
		return preparedUpdate, true
	}
	update := s.referenceWorkspaceIndex.applyPrepared(prepared)
	s.mu.Unlock()
	return update, true
}

// refreshWorkspaceIncludeGraphFileIfCurrent prepares filesystem-derived include
// metadata outside the server lock, then checks the owning artifact revision in
// the same critical section as the graph mutation.
func (s *Server) refreshWorkspaceIncludeGraphFileIfCurrent(parsed *core.ParsedDocument, manifest *workspaceDocumentArtifactManifest, revision uint64) bool {
	if parsed == nil {
		return s.workspaceDocumentArtifactRevisionCurrent(manifest, revision)
	}
	ownerPath := fileURIPath(parsed.URI)
	if ownerPath == "" {
		return s.workspaceDocumentArtifactRevisionCurrent(manifest, revision)
	}
	targets := make([]string, 0, len(parsed.Includes))
	for _, include := range parsed.Includes {
		details, ok := s.includeTargetDetailsForMode(parsed.URI, include.Path, include.Mode)
		if ok && details.Exists && details.Path != "" {
			targets = append(targets, filepath.Clean(details.Path))
		}
	}
	refsPayload, _ := json.Marshal(parsed.Includes)
	references := includeGraphReferences(parsed.Includes)
	metadata := workspacepkg.SourceMetadata{
		FileName:    ownerPath,
		Size:        int64(len(parsed.Text)),
		ContentHash: workspacepkg.DiskContentHash(parsed.Text),
	}
	if info, ok := s.fsStat(ownerPath); ok && info != nil {
		metadata.MtimeMS = info.MtimeMS
		metadata.Size = info.Size
	}
	settingsKey := s.workspaceDiskSettingsKey()
	refsFingerprint := workspacepkg.DiskContentHash(string(refsPayload))
	s.mu.Lock()
	if !s.workspaceDocumentArtifactRevisionCurrentLocked(manifest, revision) {
		s.mu.Unlock()
		return false
	}
	if s.workspaceIncludeGraph == nil || s.workspaceIncludeGraph.SettingsKey() != settingsKey {
		s.workspaceIncludeGraph = workspacepkg.NewWorkspaceIncludeGraph()
		s.workspaceIncludeGraph.Reset(settingsKey)
		s.workspaceIncludeGraphComplete = false
	}
	affectedRoots, topologyChanged := s.workspaceIncludeGraph.ApplyEdgeDelta(ownerPath, metadata, targets, references, refsFingerprint)
	if topologyChanged {
		s.workspaceIncludeGraphRevision++
		affected := affectedRoots.IdentityMembership()
		for key := range s.referenceScopes {
			if _, ok := affected[key.DocumentKey]; ok {
				delete(s.referenceScopes, key)
			}
		}
		for key := range s.referenceImplicitPlans {
			if _, ok := affected[key.DocumentKey]; ok {
				delete(s.referenceImplicitPlans, key)
			}
		}
		s.resetWorkspaceReferenceNameIndexLocked()
	}
	s.mu.Unlock()
	s.scheduleWorkspaceIncludeGraphPersistence(settingsKey)
	return true
}

func previousCST(manifest *workspaceDocumentArtifactManifest) *core.ParsedDocument {
	if manifest == nil {
		return nil
	}
	return manifest.CST
}

func (s *Server) invalidateWorkspaceDiagnosticsForArtifactDelta(current *workspaceDocumentArtifactManifest, delta workspaceDocumentArtifactDelta) {
	if current == nil {
		return
	}
	paths := map[string]struct{}{}
	if path := cleanFileURIPath(current.URI); path != "" {
		paths[path] = struct{}{}
	}
	changedExports := workspaceUniqueNames(delta.ChangedPublicNames, delta.ChangedImplicitNames, delta.ChangedObjectTagNames)
	s.mu.Lock()
	for _, name := range changedExports {
		for documentID := range s.consumersByExportName[name] {
			paths[string(documentID)] = struct{}{}
		}
	}
	s.mu.Unlock()
	if delta.IncludeEdgesChanged {
		if affected, ready := s.includeInvalidationPaths(paths); ready {
			paths = affected
		}
	}
	s.invalidateWorkspaceDiagnosticsPaths(paths)
}

func (s *Server) invalidateWorkspaceReferenceLocations(names []string) {
	affected := make(map[string]struct{}, len(names))
	for _, name := range names {
		if normalized := strings.ToLower(name); normalized != "" {
			affected[normalized] = struct{}{}
		}
	}
	if len(affected) == 0 {
		return
	}
	s.mu.Lock()
	s.invalidateWorkspaceReferenceLocationsByNameLocked(affected)
	s.mu.Unlock()
}

func workspaceVirtualDeltaHasJavaScript(languages []core.EmbeddedLanguage) bool {
	for _, language := range languages {
		if language == core.LanguageJavaScript || language == core.LanguageJScript {
			return true
		}
	}
	return false
}

func workspaceUniqueNames(groups ...[]string) []string {
	seen := map[string]struct{}{}
	for _, group := range groups {
		for _, name := range group {
			if normalized := strings.ToLower(name); normalized != "" {
				seen[normalized] = struct{}{}
			}
		}
	}
	result := make([]string, 0, len(seen))
	for name := range seen {
		result = append(result, name)
	}
	sort.Strings(result)
	return result
}

func workspaceArtifactChangedNames(delta workspaceDocumentArtifactDelta) []string {
	return workspaceUniqueNames(delta.ChangedPublicNames, delta.ChangedUsageNames, delta.ChangedImplicitNames, delta.ChangedObjectTagNames, delta.ChangedReferenceCounts, delta.ChangedReferenceLocations)
}

func workspaceArtifactDeltaChangeCount(delta workspaceDocumentArtifactDelta) int {
	count := len(workspaceArtifactChangedNames(delta)) + len(delta.ChangedVirtualLanguages) + len(delta.ChangedDiagnosticLayers)
	if delta.IncludeEdgesChanged {
		count++
	}
	if delta.ExecutionTapeChanged {
		count++
	}
	if delta.VBExportsChanged {
		count++
	}
	return count
}

func workspaceArtifactDeltaChangesGraph(delta workspaceDocumentArtifactDelta) bool {
	return delta.IncludeEdgesChanged || len(delta.ChangedPublicNames) > 0 || len(delta.ChangedUsageNames) > 0 || len(delta.ChangedImplicitNames) > 0 || len(delta.ChangedObjectTagNames) > 0 || len(delta.ChangedReferenceCounts) > 0 || len(delta.ChangedReferenceLocations) > 0
}

func workspaceArtifactDeltaPersistenceWriteCount(delta workspaceDocumentArtifactDelta) int {
	count := 1 // document head
	for _, changed := range []bool{
		delta.CSTChanged,
		len(delta.ChangedPublicNames) > 0,
		len(delta.ChangedUsageNames) > 0,
		len(delta.ChangedImplicitNames) > 0,
		len(delta.ChangedObjectTagNames) > 0,
		len(delta.ChangedReferenceCounts) > 0 || len(delta.ChangedReferenceLocations) > 0,
		len(delta.ChangedVirtualLanguages) > 0,
		len(delta.ChangedDiagnosticLayers) > 0,
		delta.ExecutionTapeChanged,
		delta.VBExportsChanged,
		delta.IncludeEdgesChanged,
	} {
		if changed {
			count++
		}
	}
	return count
}

func (s *Server) queueWorkspaceDocumentArtifactDelta(manifest *workspaceDocumentArtifactManifest, delta workspaceDocumentArtifactDelta) {
	_ = s.queueWorkspaceDocumentArtifactDeltaIfCurrent(manifest, delta, 0)
}

func (s *Server) queueWorkspaceDocumentArtifactDeltaIfCurrent(manifest *workspaceDocumentArtifactManifest, delta workspaceDocumentArtifactDelta, revision uint64) bool {
	cache := s.diskCacheForUse()
	if cache == nil || !cache.Enabled() || manifest == nil {
		return true
	}
	if revision == 0 {
		s.mu.Lock()
		revision = s.workspaceArtifactRevisions[manifest.DocumentID]
		current := s.workspaceDocumentArtifactRevisionCurrentLocked(manifest, revision)
		s.mu.Unlock()
		if !current {
			return false
		}
	}
	if !s.workspaceDocumentArtifactRevisionCurrent(manifest, revision) {
		return false
	}
	documentID := string(manifest.DocumentID)
	sourceHash := string(manifest.SourceFingerprint)
	headPayload, _ := json.Marshal(struct {
		URI            string `json:"uri"`
		ParserSettings string `json:"parserSettingsFingerprint"`
	}{manifest.URI, string(manifest.ParserSettingsFingerprint)})
	diskDelta := workspacepkg.DiskDocumentCacheDelta{
		DocumentID: documentID,
		Head:       &workspacepkg.DiskDocumentHead{DocumentID: documentID, SchemaVersion: workspaceDocumentArtifactSchemaVersion, SourceHash: sourceHash, Fingerprint: sourceHash, Payload: headPayload},
	}
	appendArtifactWithSource := func(kind string, fingerprint workspaceArtifactFingerprint, artifactSourceHash string, payload any) {
		encoded, err := json.Marshal(payload)
		if err != nil {
			return
		}
		diskDelta.ArtifactUpserts = append(diskDelta.ArtifactUpserts, workspacepkg.DiskDocumentArtifact{
			DocumentID: documentID, Kind: kind, SchemaVersion: workspaceDocumentArtifactSchemaVersion,
			SourceHash: artifactSourceHash, Fingerprint: string(fingerprint), Payload: encoded,
		})
	}
	appendArtifact := func(kind string, fingerprint workspaceArtifactFingerprint, payload any) {
		appendArtifactWithSource(kind, fingerprint, sourceHash, payload)
	}
	if delta.CSTChanged {
		appendArtifact("cst", manifest.SourceFingerprint, manifest.CST)
	}
	if len(delta.ChangedPublicNames) > 0 {
		appendArtifact("public-symbols", workspaceFingerprint(manifest.PublicSymbols), manifest.PublicSymbols)
	}
	if len(delta.ChangedUsageNames) > 0 {
		appendArtifact("external-usages", workspaceFingerprint(manifest.ExternalUsages), manifest.ExternalUsages)
	}
	if len(delta.ChangedImplicitNames) > 0 {
		appendArtifact("implicit-globals", workspaceFingerprint(manifest.ImplicitGlobalCandidates), manifest.ImplicitGlobalCandidates)
	}
	if len(delta.ChangedObjectTagNames) > 0 {
		appendArtifact("object-tags", workspaceFingerprint(manifest.ObjectTagVariables), manifest.ObjectTagVariables)
	}
	if len(delta.ChangedReferenceCounts) > 0 || len(delta.ChangedReferenceLocations) > 0 {
		appendArtifact("references", workspaceFingerprint(manifest.References), manifest.References)
	}
	if len(delta.ChangedVirtualLanguages) > 0 {
		appendArtifact("virtual-documents", workspaceFingerprint(manifest.VirtualDocuments), manifest.VirtualDocuments)
	}
	if len(delta.ChangedDiagnosticLayers) > 0 {
		appendArtifact("local-diagnostics", workspaceFingerprint(manifest.LocalDiagnostics), manifest.LocalDiagnostics)
	}
	if delta.ExecutionTapeChanged {
		appendArtifact("execution-tape", manifest.ExecutionTapeFingerprint, manifest.ExecutionTape)
	}
	if delta.VBExportsChanged {
		appendArtifactWithSource(workspaceVBExportsArtifactKind, manifest.VBExportsFingerprint, manifest.VBExportsCacheSourceHash, manifest.VBExports)
	}
	if delta.IncludeEdgesChanged {
		edges := make([]workspacepkg.DiskIncludeEdge, 0, len(manifest.IncludeEdges))
		document := core.NewTextDocument(manifest.URI, "classic-asp", 0, manifest.CST.Text)
		for _, edge := range manifest.IncludeEdges {
			edges = append(edges, workspacepkg.DiskIncludeEdge{DocumentID: documentID, TargetDocumentID: string(edge.DocumentID), Offset: document.OffsetAt(edge.Range.Start), Mode: edge.Mode, Path: edge.Path})
		}
		diskDelta.IncludeEdges = &workspacepkg.DiskDocumentIncludeEdges{DocumentID: documentID, SchemaVersion: workspaceDocumentArtifactSchemaVersion, SourceHash: sourceHash, Fingerprint: string(manifest.IncludeFingerprint), Edges: edges}
	}
	unlockQueue, current := s.lockWorkspaceArtifactQueueIfCurrent(manifest, revision)
	if !current {
		return false
	}
	runWorkspaceArtifactQueueTestHook()
	err := cache.QueueDocumentCacheDeltas([]workspacepkg.DiskDocumentCacheDelta{diskDelta})
	unlockQueue()
	if err != nil {
		s.logServerWarning("[asp-lsp] workspaceArtifact.write.failed: " + err.Error())
	}
	return true
}
