package lspserver

import (
	"context"
	"encoding/json"
	"path/filepath"
	"sort"
	"strings"
	"time"

	"github.com/yottonoko/classic-web-system-lsp/internal/core"
	"github.com/yottonoko/classic-web-system-lsp/internal/graph"
	workspacepkg "github.com/yottonoko/classic-web-system-lsp/internal/workspace"
)

// workspaceDiskSettingsKey identifies the settings and roots that affect the
// persisted workspace index and include graph.  It intentionally excludes
// volatile debug/cache settings so a cache can be reused after a restart.
func (s *Server) workspaceDiskSettingsKey() string {
	s.mu.Lock()
	roots := make([]string, 0, len(s.workspaceRoots)+1)
	for _, root := range s.workspaceRoots {
		roots = append(roots, filepath.Clean(root.Path))
	}
	if len(roots) == 0 && s.rootPath != "" {
		roots = append(roots, filepath.Clean(s.rootPath))
	}
	settings := struct {
		Roots             []string `json:"roots"`
		Includes          []string `json:"includes"`
		Excludes          []string `json:"excludes"`
		RespectGitIgnore  bool     `json:"respectGitIgnore"`
		GitIgnoreHash     string   `json:"gitIgnoreHash,omitempty"`
		DefaultLanguage   string   `json:"defaultLanguage"`
		LegacyEncoding    string   `json:"legacyEncoding"`
		IncludePaths      []string `json:"includePaths"`
		VirtualRoots      []string `json:"virtualRoots"`
		WindowsResolution bool     `json:"windowsResolution"`
		CaseResolution    string   `json:"caseResolution"`
	}{
		Roots:             roots,
		Includes:          append([]string(nil), s.settings.WorkspaceIncludeGlobs...),
		Excludes:          append([]string(nil), s.settings.WorkspaceExcludeGlobs...),
		RespectGitIgnore:  s.settings.WorkspaceRespectGitIgnore,
		DefaultLanguage:   s.settings.DefaultLanguage,
		LegacyEncoding:    s.settings.LegacyEncoding,
		IncludePaths:      append([]string(nil), s.settings.IncludePaths...),
		VirtualRoots:      append([]string(nil), s.settings.VirtualRoots...),
		WindowsResolution: s.settings.WindowsPathResolution,
		CaseResolution:    s.settings.NetworkCaseResolution,
	}
	s.mu.Unlock()
	sort.Strings(settings.Roots)
	if settings.RespectGitIgnore {
		var gitIgnoreState strings.Builder
		for _, root := range settings.Roots {
			gitIgnoreState.WriteString(root)
			gitIgnoreState.WriteByte(0)
			for _, rule := range s.readGitIgnoreGlobs(root) {
				gitIgnoreState.WriteString(rule)
				gitIgnoreState.WriteByte(0)
			}
		}
		settings.GitIgnoreHash = workspacepkg.DiskContentHash(gitIgnoreState.String())
	}
	payload, _ := json.Marshal(settings)
	return workspacepkg.DiskContentHash(string(payload))
}

func (s *Server) workspaceIndexDiskEntries(docs map[string]*core.TextDocument, freshness string) []workspacepkg.DiskWorkspaceIndexedDocument {
	entries := make([]workspacepkg.DiskWorkspaceIndexedDocument, 0, len(docs))
	keys := make([]string, 0, len(docs))
	for uri := range docs {
		keys = append(keys, uri)
	}
	sort.Strings(keys)
	for _, uri := range keys {
		doc := docs[uri]
		if doc == nil {
			continue
		}
		path := fileURIPath(uri)
		entry := workspacepkg.DiskWorkspaceIndexedDocument{URI: uri, FileName: path, Size: int64(len(doc.Text)), Text: doc.Text}
		if info, ok := s.fsStat(path); ok {
			entry.MtimeMS = info.MtimeMS
			entry.Size = info.Size
		}
		if freshness == "watch" || freshness == "ttl" {
			entry.ContentHash = workspacepkg.DiskContentHash(doc.Text)
		}
		entries = append(entries, entry)
	}
	return entries
}

func (s *Server) restoreWorkspaceIndexFromDiskContext(ctx context.Context, freshness string) (map[string]*core.TextDocument, bool, bool) {
	return s.restoreWorkspaceIndexFromDiskSnapshot(ctx, freshness, s.workspaceDiskCacheReadSnapshot(freshness))
}

type workspaceDiskCacheReadSnapshot struct {
	cache      *workspacepkg.DiskAnalysisCache
	key        string
	freshness  string
	validation workspaceDiskCacheValidation
}

type workspaceDiskCacheValidation struct {
	enabled        bool
	directory      string
	freshness      string
	ttlHours       int
	maxSizeMB      int
	gzip           bool
	networkProfile string
}

func (s *Server) workspaceDiskCacheValidationLocked() workspaceDiskCacheValidation {
	return workspaceDiskCacheValidation{
		enabled:        s.settings.CacheEnabled,
		directory:      s.settings.CacheDirectory,
		freshness:      s.settings.CacheFreshness,
		ttlHours:       s.settings.CacheTTLHours,
		maxSizeMB:      s.settings.CacheMaxSizeMB,
		gzip:           s.settings.CacheGzip,
		networkProfile: s.settings.NetworkProfile,
	}
}

func (s *Server) workspaceDiskCacheReadSnapshot(freshness string) *workspaceDiskCacheReadSnapshot {
	s.mu.Lock()
	cache := s.diskAnalysisCache
	validation := s.workspaceDiskCacheValidationLocked()
	s.mu.Unlock()
	if cache == nil || !cache.Enabled() {
		return nil
	}
	return &workspaceDiskCacheReadSnapshot{
		cache: cache, key: s.workspaceDiskSettingsKey(), freshness: freshness, validation: validation,
	}
}

func (s *Server) workspaceDiskCacheReadSnapshotCurrentLocked(snapshot *workspaceDiskCacheReadSnapshot) bool {
	return snapshot != nil && s.diskAnalysisCache == snapshot.cache &&
		s.workspaceDiskCacheValidationLocked() == snapshot.validation &&
		s.effectiveCacheFreshnessLocked() == snapshot.freshness
}

func (s *Server) restoreWorkspaceIndexFromDiskSnapshot(ctx context.Context, freshness string, snapshot *workspaceDiskCacheReadSnapshot) (map[string]*core.TextDocument, bool, bool) {
	if ctx == nil {
		ctx = context.Background()
	}
	if snapshot == nil || snapshot.cache == nil || !snapshot.cache.Enabled() {
		return nil, false, false
	}
	s.mu.Lock()
	cacheReadHook := s.workspaceCacheReadTestHook
	s.mu.Unlock()
	if cacheReadHook != nil {
		cacheReadHook("workspaceIndex")
	}
	entry, ok := snapshot.cache.ReadWorkspaceIndex(snapshot.key)
	if !ok {
		return nil, false, false
	}
	type validationResult struct {
		document *core.TextDocument
		path     string
		stale    bool
	}
	results := make([]validationResult, len(entry.Entries))
	s.mu.Lock()
	metadataValidationHook := s.workspaceMetadataValidationTestHook
	s.mu.Unlock()
	workers := s.analysisWorkers
	if workers == nil {
		workers = &analysisWorkerPool{}
	}
	workers.parallelForBulk(ctx, len(entry.Entries), func(workerCtx context.Context, index int) {
		indexed := entry.Entries[index]
		path := indexed.FileName
		if path == "" {
			path = fileURIPath(indexed.URI)
		}
		if path == "" {
			results[index].stale = true
			return
		}
		if metadataValidationHook != nil {
			metadataValidationHook(workerCtx, path)
		}
		if workerCtx.Err() != nil {
			return
		}
		if !workspaceIndexFileMetadataMatches(indexed, path, s) {
			results[index] = validationResult{path: path, stale: true}
			return
		}
		content := indexed.Text
		readFromSource := false
		if content == "" && indexed.Size > 0 {
			var err error
			content, err = s.readWorkspaceTextFileContext(workerCtx, path)
			if err != nil {
				results[index] = validationResult{path: path, stale: true}
				return
			}
			readFromSource = true
		}
		uri := indexed.URI
		if uri == "" {
			uri = filePathURI(path)
		}
		document := core.NewTextDocument(uri, "classic-asp", 0, content)
		if !workspaceIndexContentMatches(indexed, content, freshness) {
			if readFromSource {
				results[index] = validationResult{path: path, document: document, stale: true}
				return
			}
			results[index] = validationResult{path: path, stale: true}
			return
		}
		results[index] = validationResult{path: path, document: document}
	})
	if ctx.Err() != nil {
		return nil, false, false
	}
	docs := make(map[string]*core.TextDocument, len(entry.Entries))
	stale := false
	for _, result := range results {
		if result.stale {
			if result.path != "" && result.document == nil {
				s.invalidateSourceSnapshot(result.path)
			}
			stale = true
		}
		if result.document != nil {
			docs[result.document.URI] = result.document
		}
	}
	if stale {
		return docs, false, true
	}
	s.logAnalysisDatabaseEvent("workspaceIndex", "restore", map[string]any{
		"documents": len(docs), "freshness": freshness, "settingsKey": shortLogKey(snapshot.key),
	})
	return docs, true, false
}

func workspaceIndexFileMetadataMatches(indexed workspacepkg.DiskWorkspaceIndexedDocument, path string, s *Server) bool {
	if indexed.FileName != "" && filepath.Clean(indexed.FileName) != filepath.Clean(path) {
		return false
	}
	if info, ok := s.fsStat(path); !ok {
		return false
	} else if indexed.MtimeMS != 0 && indexed.MtimeMS != info.MtimeMS || indexed.Size != 0 && indexed.Size != info.Size {
		return false
	}
	return true
}

func workspaceIndexContentMatches(indexed workspacepkg.DiskWorkspaceIndexedDocument, content, freshness string) bool {
	return freshness != "watch" && freshness != "ttl" ||
		indexed.ContentHash == "" || indexed.ContentHash == workspacepkg.DiskContentHash(content)
}

func (s *Server) writeWorkspaceIndexToDisk(docs map[string]*core.TextDocument) {
	s.writeWorkspaceIndexToDiskGuarded(context.Background(), 0, false, false, docs)
}

func (s *Server) writeWorkspaceIndexToDiskIfCurrent(ctx context.Context, generation uint64, docs map[string]*core.TextDocument) bool {
	return s.writeWorkspaceIndexToDiskGuarded(ctx, generation, true, false, docs)
}

func (s *Server) writeWorkspaceIndexToDiskIfGraphCurrent(ctx context.Context, generation uint64, docs map[string]*core.TextDocument) bool {
	return s.writeWorkspaceIndexToDiskGuarded(ctx, generation, false, true, docs)
}

func (s *Server) workspaceIndexDiskWriteCurrentLocked(ctx context.Context, generation uint64, cache *workspacepkg.DiskAnalysisCache, guardWorkspaceGeneration, guardGraphGeneration bool) bool {
	if s.shutdown {
		return false
	}
	if !guardWorkspaceGeneration && !guardGraphGeneration {
		return true
	}
	if ctx == nil || ctx.Err() != nil {
		return false
	}
	return !s.diskCacheWritesClosed && s.diskAnalysisCache == cache &&
		(!guardWorkspaceGeneration || (!s.workspaceIndexClosed && s.workspaceIndexGeneration == generation)) &&
		(!guardGraphGeneration || s.graphGeneration == generation)
}

func (s *Server) writeWorkspaceIndexToDiskGuarded(ctx context.Context, generation uint64, guardWorkspaceGeneration, guardGraphGeneration bool, docs map[string]*core.TextDocument) bool {
	if ctx == nil {
		ctx = context.Background()
	}
	if !lockMutexContext(ctx, &s.workspaceIndexDiskCacheUseMu) {
		return false
	}
	defer s.workspaceIndexDiskCacheUseMu.Unlock()
	cache := s.diskCacheForUse()
	if cache == nil || !cache.Enabled() {
		if guardWorkspaceGeneration {
			return s.workspaceIndexGenerationCurrent(ctx, generation)
		}
		if guardGraphGeneration {
			return s.graphGenerationCurrent(ctx, generation)
		}
		return true
	}
	key := s.workspaceDiskSettingsKey()
	freshness := s.effectiveCacheFreshness()
	entry := workspacepkg.DiskWorkspaceIndexCacheEntry{
		SettingsKey: key,
		Entries:     s.workspaceIndexDiskEntries(docs, freshness),
	}
	documentIDs := make([]string, 0, len(docs))
	for uri := range docs {
		documentIDs = append(documentIDs, string(workspaceDocumentIDFromURI(uri)))
	}
	sort.Strings(documentIDs)
	manifest := workspacepkg.DiskWorkspaceMembershipManifest{
		SchemaVersion: workspaceDocumentArtifactSchemaVersion,
		SettingsKey:   key,
		DocumentIDs:   documentIDs,
		Fingerprint:   workspacepkg.DiskContentHash(strings.Join(documentIDs, "\x00")),
	}
	transaction, transactionErr := cache.BeginWorkspaceIndexWrite()
	if transactionErr != nil {
		s.logServerWarning("[asp-lsp] workspaceIndex.transaction.begin.failed: " + transactionErr.Error())
		return false
	}
	s.mu.Lock()
	if !s.workspaceIndexDiskWriteCurrentLocked(ctx, generation, cache, guardWorkspaceGeneration, guardGraphGeneration) {
		s.mu.Unlock()
		return false
	}
	s.mu.Unlock()
	// Stage both records before the commit hook so the membership and index are
	// committed by one cache transaction. Staging is private to this
	// transaction and does not enter the shared writer queue.
	if err := transaction.QueueWorkspaceMembershipManifest(manifest); err != nil {
		s.logServerWarning("[asp-lsp] workspaceManifest.write.failed: " + err.Error())
		return false
	}
	if err := transaction.WriteWorkspaceIndex(entry); err != nil {
		s.logServerWarning("[asp-lsp] workspaceIndex.write.failed: " + err.Error())
		return false
	}
	s.runWorkspaceIndexTestHook(ctx, workspaceIndexTestPhaseAfterDiskEnqueueBeforeCommit, generation)
	if !lockMutexContext(ctx, &s.workspaceIndexDiskCommitMu) {
		return false
	}
	committed, commitErr := transaction.CommitIf(func() bool {
		s.mu.Lock()
		defer s.mu.Unlock()
		return s.workspaceIndexDiskWriteCurrentLocked(ctx, generation, cache, guardWorkspaceGeneration, guardGraphGeneration)
	})
	s.workspaceIndexDiskCommitMu.Unlock()
	if commitErr != nil {
		s.logServerWarning("[asp-lsp] workspaceIndex.flush.failed: " + commitErr.Error())
		return false
	}
	if !committed {
		return false
	}
	s.logAnalysisDatabaseEvent("workspaceIndex", "write", map[string]any{
		"documents": len(entry.Entries), "freshness": freshness, "settingsKey": shortLogKey(key),
	})
	return true
}

func (s *Server) persistCurrentWorkspaceIndex() {
	s.mu.Lock()
	docs := make(map[string]*core.TextDocument, len(s.workspace))
	for uri, doc := range s.workspace {
		docs[uri] = doc
	}
	s.mu.Unlock()
	s.writeWorkspaceIndexToDisk(docs)
}

func (s *Server) persistCurrentWorkspaceIndexIfGraphCurrent(ctx context.Context, generation uint64) bool {
	s.mu.Lock()
	docs := make(map[string]*core.TextDocument, len(s.workspace))
	for uri, doc := range s.workspace {
		docs[uri] = doc
	}
	s.mu.Unlock()
	return s.writeWorkspaceIndexToDiskIfGraphCurrent(ctx, generation, docs)
}

func (s *Server) syncWorkspaceIncludeGraphCache(documents []*core.ParsedDocument) {
	s.syncWorkspaceIncludeGraphCacheGuarded(context.Background(), 0, false, documents)
}

func (s *Server) syncWorkspaceIncludeGraphCacheIfCurrent(ctx context.Context, generation uint64, documents []*core.ParsedDocument) bool {
	return s.syncWorkspaceIncludeGraphCacheGuarded(ctx, generation, true, documents)
}

func (s *Server) syncWorkspaceIncludeGraphCacheGuarded(ctx context.Context, generation uint64, guardGeneration bool, documents []*core.ParsedDocument) bool {
	if ctx == nil {
		ctx = context.Background()
	}
	s.mu.Lock()
	testHook := s.workspaceIncludeGraphSyncTestHook
	s.mu.Unlock()
	if testHook != nil && !testHook() {
		return false
	}
	if guardGeneration && !s.graphGenerationCurrent(ctx, generation) {
		return false
	}
	cache := s.diskCacheForUse()
	settingsKey := s.workspaceDiskSettingsKey()
	if cache != nil && cache.Enabled() {
		if _, ok := cache.ReadWorkspaceIndex(settingsKey); !ok {
			if guardGeneration {
				if !s.persistCurrentWorkspaceIndexIfGraphCurrent(ctx, generation) {
					return false
				}
			} else {
				s.persistCurrentWorkspaceIndex()
			}
		}
	}
	if cache != nil && !cache.Enabled() {
		cache = nil
	}
	graph := workspacepkg.NewWorkspaceIncludeGraph()
	graph.Reset(settingsKey)
	s.mu.Lock()
	documentTestHook := s.workspaceIncludeGraphDocumentTestHook
	s.mu.Unlock()
	prepared := make([]workspaceIncludeGraphPreparedDocument, len(documents))
	workers := s.analysisWorkers
	if workers == nil {
		workers = &analysisWorkerPool{}
	}
	workers.parallelForBulk(ctx, len(documents), func(workerCtx context.Context, index int) {
		prepared[index] = s.prepareWorkspaceIncludeGraphDocument(workerCtx, documents[index], documentTestHook)
	})
	if ctx.Err() != nil {
		return false
	}
	for _, document := range prepared {
		if !document.valid {
			continue
		}
		graph.UpsertWithReferences(document.ownerPath, document.source, document.targets, document.references, document.refsFingerprint)
	}
	snapshot, hasSnapshot := graph.Snapshot(settingsKey)
	s.mu.Lock()
	if guardGeneration && (ctx.Err() != nil || s.graphGeneration != generation) {
		s.mu.Unlock()
		return false
	}
	s.workspaceIncludeGraph = graph
	s.workspaceIncludeGraphRevision++
	revision := s.workspaceIncludeGraphRevision
	cache = s.diskAnalysisCache
	s.workspaceIncludeGraphComplete = true
	s.referenceScopes = map[workspaceReferenceScopeCacheKey]workspaceReferenceScopeSnapshot{}
	s.referenceImplicitPlans = map[workspaceReferenceImplicitPlanKey]map[string]map[string]struct{}{}
	s.referenceDocuments = map[string][]*core.ParsedDocument{}
	s.mu.Unlock()
	if cache != nil && hasSnapshot {
		s.writeWorkspaceIncludeGraphSnapshotIfCurrent(ctx, generation, guardGeneration, settingsKey, revision, graph, cache, snapshot)
	}
	return true
}

type workspaceIncludeGraphPreparedDocument struct {
	valid           bool
	ownerPath       string
	source          workspacepkg.SourceMetadata
	targets         []string
	references      []workspacepkg.IncludeReference
	refsFingerprint string
}

func (s *Server) prepareWorkspaceIncludeGraphDocument(ctx context.Context, parsed *core.ParsedDocument, testHook func(context.Context, string)) workspaceIncludeGraphPreparedDocument {
	if parsed == nil || ctx.Err() != nil {
		return workspaceIncludeGraphPreparedDocument{}
	}
	ownerPath := fileURIPath(parsed.URI)
	if ownerPath == "" {
		return workspaceIncludeGraphPreparedDocument{}
	}
	if testHook != nil {
		testHook(ctx, ownerPath)
	}
	if ctx.Err() != nil {
		return workspaceIncludeGraphPreparedDocument{}
	}
	targets := make([]string, 0, len(parsed.Includes))
	for _, include := range parsed.Includes {
		if ctx.Err() != nil {
			return workspaceIncludeGraphPreparedDocument{}
		}
		details, ok := s.includeTargetDetailsForModeContext(ctx, parsed.URI, include.Path, include.Mode)
		if ok && details.Exists && details.Path != "" {
			targets = append(targets, filepath.Clean(details.Path))
		}
	}
	var mtimeMS, size int64
	if info, ok := s.fsStat(ownerPath); ok && info != nil {
		mtimeMS = info.MtimeMS
		size = info.Size
	}
	refsPayload, _ := json.Marshal(parsed.Includes)
	return workspaceIncludeGraphPreparedDocument{
		valid:     true,
		ownerPath: ownerPath,
		source: workspacepkg.SourceMetadata{
			FileName:    ownerPath,
			MtimeMS:     mtimeMS,
			Size:        size,
			ContentHash: workspacepkg.DiskContentHash(parsed.Text),
		},
		targets:         targets,
		references:      includeGraphReferences(parsed.Includes),
		refsFingerprint: workspacepkg.DiskContentHash(string(refsPayload)),
	}
}

func (s *Server) refreshWorkspaceIncludeGraphFile(parsed *core.ParsedDocument) {
	if parsed == nil {
		return
	}
	ownerPath := fileURIPath(parsed.URI)
	if ownerPath == "" {
		return
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
	s.mu.Lock()
	if s.workspaceIncludeGraph == nil || s.workspaceIncludeGraph.SettingsKey() != settingsKey {
		s.workspaceIncludeGraph = workspacepkg.NewWorkspaceIncludeGraph()
		s.workspaceIncludeGraph.Reset(settingsKey)
		s.workspaceIncludeGraphComplete = false
	}
	refsFingerprint := workspacepkg.DiskContentHash(string(refsPayload))
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
}

func (s *Server) scheduleWorkspaceIncludeGraphPersistence(settingsKey string) {
	s.mu.Lock()
	if s.diskCacheWritesClosed {
		s.mu.Unlock()
		return
	}
	s.workspaceIncludeGraphPersistKey = settingsKey
	s.workspaceIncludeGraphPersistAt = time.Now()
	if s.workspaceIncludeGraphPersisting {
		s.mu.Unlock()
		return
	}
	s.workspaceIncludeGraphPersisting = true
	s.diskCacheWrites.Add(1)
	s.mu.Unlock()
	go s.persistWorkspaceIncludeGraphAfterDebounce()
}

func (s *Server) persistWorkspaceIncludeGraphAfterDebounce() {
	defer s.diskCacheWrites.Done()
	for {
		s.mu.Lock()
		wait := time.Until(s.workspaceIncludeGraphPersistAt.Add(workspaceIncludeGraphPersistDebounce))
		if wait <= 0 {
			settingsKey := s.workspaceIncludeGraphPersistKey
			persistAt := s.workspaceIncludeGraphPersistAt
			revision := s.workspaceIncludeGraphRevision
			cache := s.diskAnalysisCache
			s.mu.Unlock()
			s.persistWorkspaceIncludeGraphSnapshotAtRevision(settingsKey, revision)

			s.mu.Lock()
			updated := s.workspaceIncludeGraphPersistAt != persistAt ||
				s.workspaceIncludeGraphPersistKey != settingsKey ||
				s.workspaceIncludeGraphRevision != revision ||
				s.diskAnalysisCache != cache
			if s.diskCacheWritesClosed {
				s.workspaceIncludeGraphPersisting = false
				s.mu.Unlock()
				return
			}
			if !updated {
				s.workspaceIncludeGraphPersisting = false
				s.mu.Unlock()
				return
			}
			s.mu.Unlock()
			continue
		}
		s.mu.Unlock()
		timer := time.NewTimer(wait)
		<-timer.C
	}
}

func (s *Server) persistWorkspaceIncludeGraphSnapshotAtRevision(settingsKey string, revision uint64) {
	s.mu.Lock()
	graph := s.workspaceIncludeGraph
	var snapshot workspacepkg.IncludeGraphSnapshot
	cache := s.diskAnalysisCache
	if s.workspaceIncludeGraphRevision != revision ||
		s.workspaceIncludeGraphPersistKey != settingsKey || graph == nil || graph.SettingsKey() != settingsKey ||
		cache == nil || !cache.Enabled() {
		s.mu.Unlock()
		return
	}
	snapshot, _ = graph.Snapshot("")
	if snapshot.SettingsKey == "" {
		s.mu.Unlock()
		return
	}
	// Keep validation and queueing under the server lock so a revision or cache
	// replacement cannot interleave between the check and the queued snapshot.
	err := cache.WriteWorkspaceIncludeGraph(workspacepkg.DiskWorkspaceIncludeGraphCacheEntry{
		SettingsKey: settingsKey,
		Entries:     snapshot.Entries,
	})
	s.mu.Unlock()
	if err != nil {
		s.logServerWarning("[asp-lsp] workspaceIncludeGraph.write.failed: " + err.Error())
		return
	}
	s.logAnalysisDatabaseEvent("workspaceIncludeGraph", "write", map[string]any{
		"entries": len(snapshot.Entries), "settingsKey": shortLogKey(settingsKey),
	})
}

func (s *Server) writeWorkspaceIncludeGraphSnapshotIfCurrent(ctx context.Context, generation uint64, guardGeneration bool, settingsKey string, revision uint64, graph *workspacepkg.WorkspaceIncludeGraph, cache *workspacepkg.DiskAnalysisCache, snapshot workspacepkg.IncludeGraphSnapshot) bool {
	if ctx == nil {
		ctx = context.Background()
	}
	if snapshot.SettingsKey == "" || cache == nil || !cache.Enabled() {
		return false
	}
	s.mu.Lock()
	if ctx.Err() != nil || s.workspaceIncludeGraph != graph || graph.SettingsKey() != settingsKey ||
		s.workspaceIncludeGraphRevision != revision ||
		s.diskAnalysisCache != cache || (guardGeneration && s.graphGeneration != generation) {
		s.mu.Unlock()
		return false
	}
	err := cache.WriteWorkspaceIncludeGraph(workspacepkg.DiskWorkspaceIncludeGraphCacheEntry{
		SettingsKey: settingsKey,
		Entries:     snapshot.Entries,
	})
	s.mu.Unlock()
	if err != nil {
		s.logServerWarning("[asp-lsp] workspaceIncludeGraph.write.failed: " + err.Error())
		return false
	}
	s.logAnalysisDatabaseEvent("workspaceIncludeGraph", "write", map[string]any{
		"entries": len(snapshot.Entries), "settingsKey": shortLogKey(settingsKey),
	})
	return true
}

func (s *Server) graphPayloadDiskSettingsKey(cacheKey string) string {
	s.mu.Lock()
	parts := make([]string, 0, len(s.workspace)+len(s.documents))
	seen := map[string]struct{}{}
	for uri, doc := range s.workspace {
		if doc == nil {
			continue
		}
		identity := workspacepkg.FileIdentityKeyFromURI(uri)
		if _, ok := seen[identity]; ok {
			continue
		}
		seen[identity] = struct{}{}
		parts = append(parts, identity+"\x00"+workspacepkg.DiskContentHash(doc.Text))
	}
	for uri, doc := range s.documents {
		if doc == nil {
			continue
		}
		identity := workspacepkg.FileIdentityKeyFromURI(uri)
		if _, ok := seen[identity]; ok {
			continue
		}
		seen[identity] = struct{}{}
		parts = append(parts, identity+"\x00"+workspacepkg.DiskContentHash(doc.Text))
	}
	s.mu.Unlock()
	sort.Strings(parts)
	graphSettings, _ := json.Marshal(s.graphPayloadSettings())
	return workspacepkg.DiskContentHash(s.workspaceDiskSettingsKey() + "\x00" + cacheKey + "\x00" + string(graphSettings) + "\x00" + strings.Join(parts, "\x00"))
}

func (s *Server) readDiskGraphPayload(cacheKey string) (graph.Payload, bool) {
	cache := s.diskCacheForUse()
	if cache == nil || !cache.Enabled() {
		return graph.Payload{}, false
	}
	entry, ok := cache.ReadGraphPayload(s.graphPayloadDiskSettingsKey(cacheKey))
	if !ok {
		return graph.Payload{}, false
	}
	var payload graph.Payload
	if err := json.Unmarshal(entry.Payload, &payload); err != nil {
		return graph.Payload{}, false
	}
	s.logAnalysisDatabaseEvent("graphPayload", "hit", map[string]any{
		"edges": len(payload.Edges), "nodes": len(payload.Nodes), "settingsKey": shortLogKey(entry.SettingsKey),
	})
	return payload, true
}

func (s *Server) writeDiskGraphPayload(cacheKey string, payload graph.Payload, generation uint64) {
	s.writeDiskGraphPayloadContext(context.Background(), cacheKey, payload, generation)
}

func (s *Server) writeDiskGraphPayloadContext(ctx context.Context, cacheKey string, payload graph.Payload, generation uint64) {
	if ctx == nil {
		ctx = context.Background()
	}
	if ctx.Err() != nil {
		return
	}
	// Serialize the generation check and disk write with graph invalidation.
	// Otherwise an invalidation could advance the generation after the check
	// and leave a stale background payload on disk.
	s.workspaceIndexDiskCommitMu.Lock()
	defer s.workspaceIndexDiskCommitMu.Unlock()
	cache := s.diskCacheForUse()
	if cache == nil || !cache.Enabled() {
		return
	}
	s.mu.Lock()
	current := s.graphGeneration == generation && s.diskAnalysisCache == cache && ctx.Err() == nil
	s.mu.Unlock()
	if !current {
		return
	}
	encoded, err := json.Marshal(payload)
	if err != nil {
		return
	}
	settingsKey := s.graphPayloadDiskSettingsKey(cacheKey)
	s.mu.Lock()
	if s.graphGeneration != generation || s.diskAnalysisCache != cache || ctx.Err() != nil {
		s.mu.Unlock()
		return
	}
	if err := cache.WriteGraphPayload(workspacepkg.DiskGraphPayloadCacheEntry{SettingsKey: settingsKey, Payload: encoded}); err != nil {
		s.mu.Unlock()
		s.logServerWarning("[asp-lsp] analysisDatabase.graphPayload.write.failed: " + err.Error())
		return
	}
	s.mu.Unlock()
	s.logAnalysisDatabaseEvent("graphPayload", "write", map[string]any{
		"bytes": len(encoded), "edges": len(payload.Edges), "nodes": len(payload.Nodes), "settingsKey": shortLogKey(settingsKey),
	})
}

func (s *Server) restoreWorkspaceIncludeGraphFromDisk() bool {
	return s.restoreWorkspaceIncludeGraphFromDiskGuarded(context.Background(), 0, false)
}

func (s *Server) restoreWorkspaceIncludeGraphFromDiskContext(ctx context.Context) bool {
	return s.restoreWorkspaceIncludeGraphFromDiskGuarded(ctx, 0, false)
}

func (s *Server) restoreWorkspaceIncludeGraphFromDiskIfCurrent(ctx context.Context, generation uint64) bool {
	return s.restoreWorkspaceIncludeGraphFromDiskGuarded(ctx, generation, true)
}

type workspaceIncludeGraphDiskCandidate struct {
	cache           *workspacepkg.DiskAnalysisCache
	cacheSnapshot   *workspaceDiskCacheReadSnapshot
	entry           workspacepkg.DiskWorkspaceIncludeGraphCacheEntry
	key             string
	revision        uint64
	graphGeneration uint64
}

func (s *Server) readWorkspaceIncludeGraphDiskCandidate() *workspaceIncludeGraphDiskCandidate {
	freshness := s.effectiveCacheFreshness()
	return s.readWorkspaceIncludeGraphDiskCandidateContext(context.Background(), s.workspaceDiskCacheReadSnapshot(freshness))
}

func (s *Server) readWorkspaceIncludeGraphDiskCandidateContext(ctx context.Context, snapshot *workspaceDiskCacheReadSnapshot) *workspaceIncludeGraphDiskCandidate {
	if ctx == nil {
		ctx = context.Background()
	}
	if ctx.Err() != nil || snapshot == nil || snapshot.cache == nil || !snapshot.cache.Enabled() {
		return nil
	}
	s.mu.Lock()
	revision := s.workspaceIncludeGraphRevision
	graphGeneration := s.graphGeneration
	s.mu.Unlock()
	if revision != 0 {
		return nil
	}
	s.mu.Lock()
	cacheReadHook := s.workspaceCacheReadTestHook
	s.mu.Unlock()
	if cacheReadHook != nil {
		cacheReadHook("workspaceIncludeGraph")
	}
	entry, ok := snapshot.cache.ReadWorkspaceIncludeGraph(snapshot.key)
	if !ok || ctx.Err() != nil {
		return nil
	}
	return &workspaceIncludeGraphDiskCandidate{
		cache:           snapshot.cache,
		cacheSnapshot:   snapshot,
		entry:           entry,
		key:             snapshot.key,
		revision:        revision,
		graphGeneration: graphGeneration,
	}
}

// readWorkspaceIncludeGraphDiskCandidateBounded keeps at most one speculative
// graph decode active without consuming the bulk or editor-facing worker slots.
func (s *Server) readWorkspaceIncludeGraphDiskCandidateBounded(ctx context.Context, snapshot *workspaceDiskCacheReadSnapshot) *workspaceIncludeGraphDiskCandidate {
	if ctx == nil {
		ctx = context.Background()
	}
	limiter := s.workspaceCacheRestoreLimiter
	if limiter == nil {
		return s.readWorkspaceIncludeGraphDiskCandidateContext(ctx, snapshot)
	}
	select {
	case limiter <- struct{}{}:
		defer func() { <-limiter }()
	case <-ctx.Done():
		return nil
	}
	return s.readWorkspaceIncludeGraphDiskCandidateContext(ctx, snapshot)
}

func (s *Server) restoreWorkspaceIncludeGraphCandidateIfWorkspaceCurrent(ctx context.Context, generation uint64, candidate *workspaceIncludeGraphDiskCandidate) bool {
	return s.restoreWorkspaceIncludeGraphDiskCandidate(ctx, 0, false, generation, true, candidate)
}

func (s *Server) restoreWorkspaceIncludeGraphFromDiskGuarded(ctx context.Context, generation uint64, guardGeneration bool) bool {
	if ctx == nil {
		ctx = context.Background()
	}
	if guardGeneration && !s.graphGenerationCurrent(ctx, generation) {
		return false
	}
	return s.restoreWorkspaceIncludeGraphDiskCandidate(ctx, generation, guardGeneration, 0, false, s.readWorkspaceIncludeGraphDiskCandidate())
}

func (s *Server) restoreWorkspaceIncludeGraphDiskCandidate(ctx context.Context, graphGeneration uint64, guardGraphGeneration bool, workspaceGeneration uint64, guardWorkspaceGeneration bool, candidate *workspaceIncludeGraphDiskCandidate) bool {
	if ctx == nil {
		ctx = context.Background()
	}
	if candidate == nil || ctx.Err() != nil {
		return false
	}
	if guardGraphGeneration && !s.graphGenerationCurrent(ctx, graphGeneration) {
		return false
	}
	if guardWorkspaceGeneration && !s.workspaceIndexGenerationCurrent(ctx, workspaceGeneration) {
		return false
	}
	for _, value := range candidate.entry.Entries {
		if ctx.Err() != nil {
			return false
		}
		if value.FileName == "" || (value.Source.FileName != "" &&
			workspacepkg.FileIdentityKeyFromFileName(value.Source.FileName) != workspacepkg.FileIdentityKeyFromFileName(value.FileName)) {
			return false
		}
		if !s.workspaceIncludeGraphSourceMatches(ctx, value.FileName, value.Source) {
			return false
		}
		for _, target := range value.TargetFileNames {
			if ctx.Err() != nil {
				return false
			}
			if target == "" {
				continue
			}
			if _, exists := s.fsStat(target); !exists {
				return false
			}
		}
		if len(value.References) == 0 && value.RefsFingerprint != workspacepkg.DiskContentHash("null") && value.RefsFingerprint != workspacepkg.DiskContentHash("[]") {
			return false
		}
		resolvedTargets := make([]string, 0, len(value.References))
		for _, reference := range value.References {
			if ctx.Err() != nil {
				return false
			}
			details, resolved := s.includeTargetDetailsForMode(filePathURI(value.FileName), reference.Path, reference.Mode)
			if resolved && details.Exists && details.Path != "" {
				resolvedTargets = append(resolvedTargets, filepath.Clean(details.Path))
			}
		}
		if !sameFileIdentityList(resolvedTargets, value.TargetFileNames) {
			return false
		}
	}
	s.mu.Lock()
	if ctx.Err() != nil || s.workspaceIncludeGraphRevision != candidate.revision || s.diskAnalysisCache != candidate.cache ||
		!s.workspaceDiskCacheReadSnapshotCurrentLocked(candidate.cacheSnapshot) ||
		s.graphGeneration != candidate.graphGeneration ||
		(guardGraphGeneration && s.graphGeneration != graphGeneration) ||
		(guardWorkspaceGeneration && (s.workspaceIndexClosed || s.workspaceIndexGeneration != workspaceGeneration)) {
		s.mu.Unlock()
		return false
	}
	if s.workspaceIncludeGraph == nil {
		s.workspaceIncludeGraph = workspacepkg.NewWorkspaceIncludeGraph()
	}
	s.workspaceIncludeGraph.Restore(workspacepkg.IncludeGraphSnapshot{SettingsKey: candidate.key, Entries: candidate.entry.Entries})
	s.workspaceIncludeGraphComplete = true
	s.mu.Unlock()
	s.logAnalysisDatabaseEvent("workspaceIncludeGraph", "restore", map[string]any{
		"entries": len(candidate.entry.Entries), "settingsKey": shortLogKey(candidate.key),
	})
	return true
}

func (s *Server) workspaceIncludeGraphSourceMatches(ctx context.Context, path string, source workspacepkg.SourceMetadata) bool {
	if ctx == nil {
		ctx = context.Background()
	}
	if ctx.Err() != nil {
		return false
	}
	info, exists := s.fsStat(path)
	if !exists || info == nil || !info.File {
		return false
	}
	if source.MtimeMS != 0 && source.MtimeMS != info.MtimeMS || source.Size != 0 && source.Size != info.Size {
		return false
	}
	if source.ContentHash != "" {
		// A complete workspace-index restore already carries the immutable source
		// text that produced this include graph. Reuse it instead of turning every
		// warm start into another full workspace read.
		s.mu.Lock()
		doc := s.workspaceDocumentByURILocked(filePathURI(path))
		s.mu.Unlock()
		if doc != nil {
			return workspacepkg.DiskContentHash(doc.Text) == source.ContentHash
		}
		// The workspace index may have restored text using metadata-only
		// validation. Bypass those document/source snapshots so the persisted
		// hash is checked against the current file contents.
		s.invalidateSourceSnapshot(path)
		content, err := s.readChangedWorkspaceTextFileContext(ctx, path)
		if err != nil || workspacepkg.DiskContentHash(content) != source.ContentHash {
			return false
		}
		return true
	}
	if source.MtimeMS == 0 {
		return false
	}
	return source.MtimeMS == info.MtimeMS && source.Size == info.Size
}

func includeGraphReferences(includes []core.Include) []workspacepkg.IncludeReference {
	references := make([]workspacepkg.IncludeReference, 0, len(includes))
	for _, include := range includes {
		references = append(references, workspacepkg.IncludeReference{Path: include.Path, Mode: include.Mode})
	}
	return references
}

func sameFileIdentityList(first, second []string) bool {
	if len(first) != len(second) {
		return false
	}
	for index := range first {
		if workspacepkg.FileIdentityKeyFromFileName(first[index]) != workspacepkg.FileIdentityKeyFromFileName(second[index]) {
			return false
		}
	}
	return true
}
