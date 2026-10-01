package lspserver

import (
	"context"
	"encoding/json"
	"strings"

	"github.com/yottonoko/classic-web-system-lsp/internal/core"
	workspacepkg "github.com/yottonoko/classic-web-system-lsp/internal/workspace"
)

func (s *Server) diskCacheForUse() *workspacepkg.DiskAnalysisCache {
	s.mu.Lock()
	defer s.mu.Unlock()
	return s.diskAnalysisCache
}

func (s *Server) diskCacheUsesDefaultDirectory() bool {
	s.mu.Lock()
	defer s.mu.Unlock()
	return strings.TrimSpace(s.settings.CacheDirectory) == ""
}

func (s *Server) runAsyncDiskCacheWrite(fn func()) bool {
	return s.runAsyncDiskCacheWriteKey("", fn)
}

func (s *Server) runAsyncDiskCacheWriteKey(key string, fn func()) bool {
	if fn == nil {
		return false
	}
	s.mu.Lock()
	if s.diskCacheWritesClosed {
		if key != "" && !s.shutdown {
			if s.diskCacheWriteDeferred == nil {
				s.diskCacheWriteDeferred = map[string]func(){}
			}
			s.diskCacheWriteDeferred[key] = fn
			s.mu.Unlock()
			return true
		}
		s.mu.Unlock()
		return false
	}
	if key != "" {
		if s.diskCacheWritePending == nil {
			s.diskCacheWritePending = map[string]func(){}
		}
		if _, pending := s.diskCacheWritePending[key]; pending {
			s.diskCacheWritePending[key] = fn
			s.mu.Unlock()
			return true
		}
		s.diskCacheWritePending[key] = fn
		s.diskCacheWriteQueue = append(s.diskCacheWriteQueue, diskCacheWriteTask{key: key})
	} else {
		s.diskCacheWriteQueue = append(s.diskCacheWriteQueue, diskCacheWriteTask{fn: fn})
	}
	s.diskCacheWrites.Add(1)
	if s.diskCacheWriteWorkerRunning {
		s.mu.Unlock()
		return true
	}
	s.diskCacheWriteWorkerRunning = true
	s.mu.Unlock()
	go s.runDiskCacheWriteQueue()
	return true
}

func (s *Server) runDiskCacheWriteQueue() {
	for {
		s.mu.Lock()
		if len(s.diskCacheWriteQueue) == 0 {
			s.diskCacheWriteWorkerRunning = false
			s.mu.Unlock()
			return
		}
		task := s.diskCacheWriteQueue[0]
		s.diskCacheWriteQueue[0] = diskCacheWriteTask{}
		s.diskCacheWriteQueue = s.diskCacheWriteQueue[1:]
		fn := task.fn
		if task.key != "" {
			fn = s.diskCacheWritePending[task.key]
			delete(s.diskCacheWritePending, task.key)
		}
		s.mu.Unlock()
		if fn != nil {
			fn()
		}
		s.diskCacheWrites.Done()
	}
}

func (s *Server) pauseAsyncDiskCacheWrites() {
	s.workspaceIndexDiskCommitMu.Lock()
	s.mu.Lock()
	s.diskCacheWritesClosed = true
	s.mu.Unlock()
	s.workspaceIndexDiskCommitMu.Unlock()
	s.diskCacheWrites.Wait()
}

func (s *Server) resumeAsyncDiskCacheWrites() {
	s.workspaceIndexDiskCommitMu.Lock()
	s.mu.Lock()
	deferred := s.diskCacheWriteDeferred
	s.diskCacheWriteDeferred = nil
	if !s.shutdown {
		s.diskCacheWritesClosed = false
	}
	s.mu.Unlock()
	s.workspaceIndexDiskCommitMu.Unlock()
	for key, fn := range deferred {
		s.runAsyncDiskCacheWriteKey(key, fn)
	}
}

func (s *Server) waitForAsyncDiskCacheWrites() {
	s.pauseAsyncDiskCacheWrites()
	if cache := s.diskCacheForUse(); cache != nil {
		if err := cache.Flush(); err != nil {
			s.logServerWarning("[asp-lsp] analysisDatabase.flush.failed: " + err.Error())
		}
	}
}

func (s *Server) diskSourceStillCurrent(doc *core.TextDocument, source workspacepkg.DiskAnalysisSourceMetadata) bool {
	if doc == nil {
		return false
	}
	if currentText, ok := s.currentDocumentText(doc.URI); ok {
		return currentText == doc.Text
	}
	fileName := fileURIPath(doc.URI)
	if fileName == "" || source.ContentHash == "" {
		return true
	}
	content, err := s.readWorkspaceTextFile(fileName)
	if err != nil {
		return false
	}
	return workspacepkg.DiskContentHash(content) == source.ContentHash
}

func (s *Server) currentDocumentText(uri string) (string, bool) {
	s.mu.Lock()
	defer s.mu.Unlock()
	if doc := s.openDocumentByURILocked(uri); doc != nil {
		return doc.Text, true
	}
	if doc := s.workspace[uri]; doc != nil {
		return doc.Text, true
	}
	if strings.HasPrefix(strings.ToLower(uri), "file:") {
		if doc := s.workspaceDocumentWithIdentityLocked(uri); doc != nil {
			return doc.Text, true
		}
	}
	return "", false
}

func (s *Server) diskSourceMetadata(doc *core.TextDocument) workspacepkg.DiskAnalysisSourceMetadata {
	fileName := fileURIPath(doc.URI)
	if fileName == "" {
		fileName = doc.URI
	}
	metadata := workspacepkg.DiskAnalysisSourceMetadata{
		FileName:    fileName,
		Size:        int64(len(doc.Text)),
		ContentHash: workspacepkg.DiskContentHash(doc.Text),
	}
	if info, ok := s.fsStat(fileName); ok {
		metadata.MtimeMS = info.MtimeMS
		metadata.Size = info.Size
	}
	return metadata
}

func (s *Server) parsedDiskLookup(doc *core.TextDocument, defaultLanguage string) workspacepkg.DiskAnalysisCacheLookup {
	settingsKey := workspacepkg.DiskContentHash("parsed\x00" + defaultLanguage)
	return workspacepkg.DiskAnalysisCacheLookup{Source: s.diskSourceMetadata(doc), SettingsKey: settingsKey}
}

func (s *Server) readDiskParsedDocument(doc *core.TextDocument, defaultLanguage string) (*core.ParsedDocument, *fileAnalysisSnapshot, bool) {
	return s.readDiskParsedDocumentContext(context.Background(), doc, defaultLanguage)
}

func (s *Server) readDiskParsedDocumentContext(ctx context.Context, doc *core.TextDocument, defaultLanguage string) (*core.ParsedDocument, *fileAnalysisSnapshot, bool) {
	cache := s.diskCacheForUse()
	if cache == nil || !cache.Enabled() {
		return nil, nil, false
	}
	entry, ok := cache.ReadFileBundle(s.parsedDiskLookup(doc, defaultLanguage))
	if !ok || entry.Parsed == nil || entry.Parsed.Text != doc.Text || entry.Parsed.URI != doc.URI {
		return nil, nil, false
	}
	var snapshot *fileAnalysisSnapshot
	components := []string{"parsed", "summary"}
	if len(entry.AnalysisSnapshot) > 0 {
		var restored fileAnalysisSnapshot
		if json.Unmarshal(entry.AnalysisSnapshot, &restored) == nil &&
			restored.SchemaVersion == fileAnalysisSnapshotSchemaVersion &&
			restored.URI == doc.URI &&
			restored.IncludeResolutionFingerprint == s.includeResolutionFingerprintContext(ctx, entry.Parsed) {
			snapshot = &restored
			components = append(components, "fileAnalysis")
			seedParsedAnalysis(entry.Parsed, snapshot)
			s.rememberFileAnalysisSnapshot(entry.Parsed, snapshot)
		}
	}
	if len(entry.Parsed.Includes) > 0 {
		components = append(components, "includeRefs")
	}
	s.logAnalysisDatabaseEvent("fileBundle", "hit", map[string]any{
		"components": components, "includes": len(entry.Parsed.Includes), "settingsKey": shortLogKey(entry.SettingsKey), "sourceBytes": len(doc.Text), "uri": doc.URI,
	})
	return entry.Parsed, snapshot, true
}

func (s *Server) writeDiskParsedDocument(doc *core.TextDocument, defaultLanguage string, parsed *core.ParsedDocument, analysisSnapshot *fileAnalysisSnapshot) {
	cache := s.diskCacheForUse()
	if cache == nil || !cache.Enabled() || doc == nil || parsed == nil || analysisSnapshot == nil {
		return
	}
	snapshot := core.NewTextDocument(doc.URI, doc.LanguageID, doc.Version, doc.Text)
	lookup := s.parsedDiskLookup(snapshot, defaultLanguage)
	validateSource := s.diskCacheUsesDefaultDirectory()
	write := func() {
		s.writeDiskParsedDocumentToCache(cache, snapshot, defaultLanguage, parsed, analysisSnapshot, lookup, validateSource)
	}
	if validateSource {
		s.runAsyncDiskCacheWrite(write)
		return
	}
	write()
}

func (s *Server) writeDiskParsedDocumentToCache(cache *workspacepkg.DiskAnalysisCache, doc *core.TextDocument, defaultLanguage string, parsed *core.ParsedDocument, analysisSnapshot *fileAnalysisSnapshot, lookup workspacepkg.DiskAnalysisCacheLookup, validateSource bool) {
	if cache == nil || !cache.Enabled() || doc == nil || parsed == nil || analysisSnapshot == nil || s.diskCacheForUse() != cache {
		return
	}
	if validateSource && !s.diskSourceStillCurrent(doc, lookup.Source) {
		return
	}
	parsedSnapshot := parsed.CloneStructural()
	if parsedSnapshot == nil {
		return
	}
	// The typed snapshot is the authoritative persisted analysis. Restoring it
	// seeds Parsed.Analysis without storing the same facts twice in one bundle.
	parsedSnapshot.Analysis = nil
	encodedSnapshot, err := json.Marshal(analysisSnapshot)
	if err != nil {
		s.logServerWarning("[asp-lsp] analysisDatabase.fileBundle.encode.failed: " + err.Error())
		return
	}
	entry := workspacepkg.DiskFileBundleCacheEntry{
		DiskAnalysisCacheLookup: lookup,
		UpdateParsed:            true,
		Parsed:                  parsedSnapshot,
		Summary: workspacepkg.DiskFileAnalysisSummary{
			URI:                 parsed.URI,
			Fingerprint:         workspacepkg.DiskContentHash(parsed.Text),
			PublicSignatureHash: workspacepkg.DiskContentHash(parsed.URI + "\x00" + parsed.Text),
			DefaultLanguage:     parsed.DefaultLanguage,
			LanguageRegions:     append([]core.Region(nil), parsed.Regions...),
			IncludeRefs:         diskIncludeRefs(parsed),
		},
		AnalysisSnapshot: encodedSnapshot,
	}
	if err := cache.WriteFileBundle(entry); err != nil {
		s.logServerWarning("[asp-lsp] analysisDatabase.fileBundle.write.failed: " + err.Error())
		return
	}
	components := []string{"parsed", "summary", "fileAnalysis"}
	if len(entry.Summary.IncludeRefs) > 0 {
		components = append(components, "includeRefs")
	}
	s.logAnalysisDatabaseEvent("fileBundle", "write", map[string]any{
		"components": components, "includes": len(entry.Summary.IncludeRefs), "settingsKey": shortLogKey(lookup.SettingsKey), "sourceBytes": len(doc.Text), "uri": doc.URI,
	})
}

func diskIncludeRefs(parsed *core.ParsedDocument) []workspacepkg.DiskIncludeRef {
	if parsed == nil || len(parsed.Includes) == 0 {
		return nil
	}
	document := core.SourceDocument(parsed)
	references := make([]workspacepkg.DiskIncludeRef, 0, len(parsed.Includes))
	for _, include := range parsed.Includes {
		references = append(references, workspacepkg.DiskIncludeRef{
			Offset:         document.OffsetAt(include.Range.Start),
			Range:          include.Range,
			DirectiveRange: include.Range,
			Mode:           include.Mode,
			ModeRange:      include.Range,
			Path:           include.Path,
			PathRange:      include.Range,
		})
	}
	return references
}

func (s *Server) diagnosticsDiskLookup(doc *core.TextDocument, parsed *core.ParsedDocument) workspacepkg.DiskAnalysisCacheLookup {
	return s.diagnosticsDiskLookupContext(context.Background(), doc, parsed)
}

func (s *Server) diagnosticsDiskLookupContext(ctx context.Context, doc *core.TextDocument, parsed *core.ParsedDocument) workspacepkg.DiskAnalysisCacheLookup {
	s.mu.Lock()
	settings := s.settings
	s.mu.Unlock()
	settingsValue := map[string]any{
		"checkJs":                           settings.CheckJS,
		"defaultLanguage":                   settings.DefaultLanguage,
		"locale":                            settings.Locale,
		"legacyEncoding":                    settings.LegacyEncoding,
		"includePaths":                      settings.IncludePaths,
		"virtualRoots":                      settings.VirtualRoots,
		"windowsPathResolution":             settings.WindowsPathResolution,
		"vbscriptDeadCodeDiagnostics":       settings.VBScriptDeadCodeDiagnostics,
		"vbscriptUnusedDiagnostics":         settings.VBScriptUnusedDiagnostics,
		"vbscriptImplicitGlobalDiagnostics": settings.VBScriptImplicitGlobalDiagnostics,
		"javascriptUnusedDiagnostics":       javaScriptUnusedDiagnosticsForContext(ctx, settings),
		"vbscriptIfSyntaxDiagnostics":       settings.VBScriptIfSyntaxDiagnostics,
		"vbscriptSqlInjectionDiagnostics":   settings.VBScriptSQLInjectionDiagnostics,
		"vbscriptTypeChecking":              settings.VBScriptTypeChecking,
		"vbscriptGlobals":                   settings.VBScriptGlobals,
		"vbscriptComTypes":                  settings.VBScriptComTypes,
		"includeFingerprint":                s.diskIncludeFingerprintContext(ctx, parsed),
		"javascriptAutoImports":             settings.JavaScriptAutoImports,
		"javascriptIgnoreProjectConfig":     settings.JavaScriptIgnoreProjectConfig,
		"javascriptCompilerOptions":         settings.JavaScriptCompilerOptions,
		"javascriptCompilerOptionTypes":     settings.JavaScriptCompilerOptionTypes,
	}
	if parsedHasJavaScript(parsed) {
		settingsValue["javascriptProjectFingerprint"] = s.javascriptProjectFingerprintContext(ctx)
	}
	payload, _ := json.Marshal(settingsValue)
	return workspacepkg.DiskAnalysisCacheLookup{
		Source:      s.diskSourceMetadata(doc),
		SettingsKey: workspacepkg.DiskContentHash(string(payload)),
	}
}
