package workspace

import (
	"encoding/json"
	"errors"
	"time"

	"github.com/yottonoko/classic-web-system-lsp/internal/core"
	bolterrors "go.etcd.io/bbolt/errors"
)

func (c *DiskAnalysisCache) ReadFileBundle(lookup DiskAnalysisCacheLookup) (DiskFileBundleCacheEntry, bool) {
	entry, ok := c.readFileEntry(lookup)
	if !ok {
		return DiskFileBundleCacheEntry{}, false
	}
	bundle := DiskFileBundleCacheEntry{
		DiskAnalysisCacheLookup: lookup,
	}
	matched := false
	if entry.ParsedSettingsKey == lookup.SettingsKey {
		matched = true
		bundle.Parsed = entry.Parsed
		bundle.Summary = entry.Summary
		bundle.PublicSignature = entry.PublicSignature
		bundle.AnalysisSnapshot = append(json.RawMessage(nil), entry.FileAnalysisSnapshot...)
	}
	if entry.DiagnosticsSettingsKey == lookup.SettingsKey {
		matched = true
		bundle.Diagnostics = entry.Diagnostics
		bundle.BuilderState = entry.BuilderState
	}
	return bundle, matched
}

func (c *DiskAnalysisCache) ReadWorkspaceIndex(settingsKey string) (DiskWorkspaceIndexCacheEntry, bool) {
	entry, ok := c.readWorkspaceEntry(settingsKey, DiskCacheWorkspaceIndex)
	if !ok {
		return DiskWorkspaceIndexCacheEntry{}, false
	}
	return DiskWorkspaceIndexCacheEntry{SettingsKey: settingsKey, Entries: entry.WorkspaceEntries}, true
}

func (c *DiskAnalysisCache) ReadWorkspaceIncludeGraph(settingsKey string) (DiskWorkspaceIncludeGraphCacheEntry, bool) {
	entry, ok := c.readWorkspaceEntry(settingsKey, DiskCacheWorkspaceIncludeGraph)
	if !ok {
		return DiskWorkspaceIncludeGraphCacheEntry{}, false
	}
	return DiskWorkspaceIncludeGraphCacheEntry{SettingsKey: settingsKey, Entries: entry.WorkspaceIncludeGraphEntries}, true
}

func (c *DiskAnalysisCache) ReadGraphPayload(settingsKey string) (DiskGraphPayloadCacheEntry, bool) {
	entry, ok := c.readWorkspaceEntry(settingsKey, DiskCacheGraphPayload)
	if !ok || len(entry.GraphPayload) == 0 {
		return DiskGraphPayloadCacheEntry{}, false
	}
	return DiskGraphPayloadCacheEntry{SettingsKey: settingsKey, Payload: append(json.RawMessage(nil), entry.GraphPayload...)}, true
}

// ReadWorkspaceReferenceBatch restores an opaque workspace reference-analysis batch.
func (c *DiskAnalysisCache) ReadWorkspaceReferenceBatch(settingsKey string) (DiskWorkspaceReferenceBatchCacheEntry, bool) {
	entry, ok := c.readWorkspaceEntry(settingsKey, DiskCacheWorkspaceReferenceBatch)
	if !ok || len(entry.WorkspaceReferenceBatch) == 0 {
		return DiskWorkspaceReferenceBatchCacheEntry{}, false
	}
	return DiskWorkspaceReferenceBatchCacheEntry{
		SettingsKey: settingsKey,
		Payload:     append(json.RawMessage(nil), entry.WorkspaceReferenceBatch...),
	}, true
}

// ReadWorkspaceLegacyUndefinedGlobals restores an opaque workspace legacy undefined-global catalog.
func (c *DiskAnalysisCache) ReadWorkspaceLegacyUndefinedGlobals(settingsKey string) (DiskWorkspaceLegacyUndefinedGlobalsCacheEntry, bool) {
	entry, ok := c.readWorkspaceEntry(settingsKey, DiskCacheWorkspaceLegacyUndefinedGlobals)
	if !ok || len(entry.WorkspaceLegacyUndefinedGlobals) == 0 {
		return DiskWorkspaceLegacyUndefinedGlobalsCacheEntry{}, false
	}
	return DiskWorkspaceLegacyUndefinedGlobalsCacheEntry{
		SettingsKey: settingsKey,
		Payload:     append(json.RawMessage(nil), entry.WorkspaceLegacyUndefinedGlobals...),
	}, true
}

// WriteFileBundle queues one complete, atomic source-analysis record update.
func (c *DiskAnalysisCache) WriteFileBundle(entry DiskFileBundleCacheEntry) error {
	payload := c.baseEntry(DiskCacheFileBundle)
	payload.Source = entry.Source
	components := byte(0)
	updateParsed := entry.UpdateParsed || entry.Parsed != nil || len(entry.AnalysisSnapshot) > 0
	if updateParsed {
		components |= diskCacheParsedComponent
		payload.ParsedSettingsKey = entry.SettingsKey
		payload.Parsed = cloneDiskParsedDocument(entry.Parsed)
		payload.Summary = cloneDiskFileAnalysisSummary(entry.Summary)
		payload.PublicSignature = cloneDiskCacheValue(entry.PublicSignature)
		payload.FileAnalysisSnapshot = append(json.RawMessage(nil), entry.AnalysisSnapshot...)
	}
	updateDiagnostics := entry.UpdateDiagnostics || entry.Diagnostics != nil || entry.BuilderState != nil
	if updateDiagnostics {
		components |= diskCacheDiagnosticsComponent
		payload.DiagnosticsSettingsKey = entry.SettingsKey
		payload.Diagnostics = cloneDiskDiagnostics(entry.Diagnostics)
		payload.BuilderState = cloneDiskBuilderState(entry.BuilderState)
	}
	if components == 0 {
		return errors.New("analysis cache file bundle has no updated component")
	}
	return c.queueFileEntry(entry.DiskAnalysisCacheLookup, payload, components)
}

func cloneDiskParsedDocument(parsed *core.ParsedDocument) *core.ParsedDocument {
	if parsed == nil {
		return nil
	}
	clone := *parsed
	clone.Regions = append([]core.Region(nil), parsed.Regions...)
	clone.Includes = append([]core.Include(nil), parsed.Includes...)
	clone.Errors = append([]core.ParseError(nil), parsed.Errors...)
	clone.ChangeImpact.Languages = append([]core.EmbeddedLanguage(nil), parsed.ChangeImpact.Languages...)
	clone.Analysis = parsed.AnalysisSnapshot()
	return &clone
}

func (c *DiskAnalysisCache) WriteWorkspaceIndex(entry DiskWorkspaceIndexCacheEntry) error {
	payload := c.baseEntry(DiskCacheWorkspaceIndex)
	payload.SettingsKey = entry.SettingsKey
	payload.WorkspaceEntries = append([]DiskWorkspaceIndexedDocument(nil), entry.Entries...)
	return c.queueWorkspaceEntry(entry.SettingsKey, DiskCacheWorkspaceIndex, payload)
}

func (c *DiskAnalysisCache) WriteWorkspaceIncludeGraph(entry DiskWorkspaceIncludeGraphCacheEntry) error {
	payload := c.baseEntry(DiskCacheWorkspaceIncludeGraph)
	payload.SettingsKey = entry.SettingsKey
	payload.WorkspaceIncludeGraphEntries = cloneIncludeGraphEntries(entry.Entries)
	return c.queueWorkspaceEntry(entry.SettingsKey, DiskCacheWorkspaceIncludeGraph, payload)
}

func (c *DiskAnalysisCache) WriteGraphPayload(entry DiskGraphPayloadCacheEntry) error {
	payload := c.baseEntry(DiskCacheGraphPayload)
	payload.SettingsKey = entry.SettingsKey
	payload.GraphPayload = append(json.RawMessage(nil), entry.Payload...)
	return c.queueWorkspaceEntry(entry.SettingsKey, DiskCacheGraphPayload, payload)
}

// WriteWorkspaceReferenceBatch queues an opaque workspace reference-analysis batch.
func (c *DiskAnalysisCache) WriteWorkspaceReferenceBatch(entry DiskWorkspaceReferenceBatchCacheEntry) error {
	payload := c.baseEntry(DiskCacheWorkspaceReferenceBatch)
	payload.SettingsKey = entry.SettingsKey
	payload.WorkspaceReferenceBatch = append(json.RawMessage(nil), entry.Payload...)
	return c.queueWorkspaceEntry(entry.SettingsKey, DiskCacheWorkspaceReferenceBatch, payload)
}

// WriteWorkspaceLegacyUndefinedGlobals queues an opaque workspace legacy undefined-global catalog.
func (c *DiskAnalysisCache) WriteWorkspaceLegacyUndefinedGlobals(entry DiskWorkspaceLegacyUndefinedGlobalsCacheEntry) error {
	payload := c.baseEntry(DiskCacheWorkspaceLegacyUndefinedGlobals)
	payload.SettingsKey = entry.SettingsKey
	payload.WorkspaceLegacyUndefinedGlobals = append(json.RawMessage(nil), entry.Payload...)
	return c.queueWorkspaceEntry(entry.SettingsKey, DiskCacheWorkspaceLegacyUndefinedGlobals, payload)
}

func (c *DiskAnalysisCache) baseEntry(kind DiskCacheEntryKind) persistedDiskEntry {
	return persistedDiskEntry{
		Kind:          kind,
		FormatVersion: diskAnalysisFormatVersion,
		ToolVersion:   c.toolVersion,
		Namespace:     c.namespace,
		WrittenAt:     time.Now().UnixMilli(),
	}
}

func (c *DiskAnalysisCache) queueFileEntry(lookup DiskAnalysisCacheLookup, payload persistedDiskEntry, components byte) error {
	key := c.keyForLookup(lookup)
	return c.queueEntry(diskCacheFilesBucket, key, payload, components)
}

func (c *DiskAnalysisCache) queueWorkspaceEntry(settingsKey string, kind DiskCacheEntryKind, payload persistedDiskEntry) error {
	return c.queueEntry(diskCacheWorkspaceBucket, c.keyForWorkspace(settingsKey, kind), payload, 0)
}

func (c *DiskAnalysisCache) queueEntry(bucket, key []byte, payload persistedDiskEntry, components byte) error {
	if !c.enabled {
		return nil
	}
	payload = clonePersistedDiskEntryValue(payload)
	c.lifecycleMu.RLock()
	defer c.lifecycleMu.RUnlock()
	if c.closed {
		return bolterrors.ErrDatabaseNotOpen
	}
	mapKey := diskCachePendingKey(bucket, key)
	c.pendingMu.Lock()
	pending, found := c.pending[mapKey]
	if !found {
		pending, found = c.inFlight[mapKey]
	}
	if !found || components == 0 {
		pending = pendingDiskWrite{bucket: append([]byte(nil), bucket...), key: append([]byte(nil), key...), entry: payload}
	} else {
		pending.entry = mergeFileBundleEntry(pending.entry, payload, components)
	}
	pending.components |= components
	c.pending[mapKey] = pending
	pendingCount := c.pendingWriteCountLocked()
	c.pendingMu.Unlock()
	c.notifyPendingWrites(pendingCount)
	return nil
}

func mergeFileBundleEntry(current, update persistedDiskEntry, components byte) persistedDiskEntry {
	if current.Kind != DiskCacheFileBundle || !sourceMetadataMatches(current.Source, update.Source) {
		current = persistedDiskEntry{Kind: DiskCacheFileBundle, Source: update.Source}
	}
	current.FormatVersion = update.FormatVersion
	current.ToolVersion = update.ToolVersion
	current.Namespace = update.Namespace
	current.WrittenAt = update.WrittenAt
	current.Source = update.Source
	if components&diskCacheParsedComponent != 0 {
		current.ParsedSettingsKey = update.ParsedSettingsKey
		current.Parsed = update.Parsed
		current.Summary = update.Summary
		current.PublicSignature = update.PublicSignature
		current.FileAnalysisSnapshot = update.FileAnalysisSnapshot
	}
	if components&diskCacheDiagnosticsComponent != 0 {
		current.DiagnosticsSettingsKey = update.DiagnosticsSettingsKey
		current.Diagnostics = update.Diagnostics
		current.BuilderState = update.BuilderState
	}
	return current
}
