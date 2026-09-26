package lspserver

import (
	"context"
	"encoding/json"
	"fmt"
	"path/filepath"
	"sort"
	"strings"

	"github.com/yottonoko/classic-web-system-lsp/internal/core"
)

func (s *Server) handleNotification(ctx context.Context, method string, params json.RawMessage) error {
	message := &rpcMessage{Method: method, Params: params}
	return s.handleNotificationMessage(ctx, message)
}

func (s *Server) handleNotificationMessage(ctx context.Context, message *rpcMessage) error {
	if message == nil {
		return malformedNotification(fmt.Errorf("notification message is nil"))
	}
	method := message.Method
	params := message.Params
	s.mu.Lock()
	shuttingDown := s.shutdown
	s.mu.Unlock()
	if shuttingDown {
		return nil
	}
	if revisionAdvancingNotification(method) {
		if !prepareRevisionAdvancingNotification(message) {
			if message.revisionNotificationErr != nil {
				return malformedNotification(message.revisionNotificationErr)
			}
			return malformedNotification(fmt.Errorf("invalid %s notification params", method))
		}
	} else if err := validateNotificationParams(method, params); err != nil {
		return malformedNotification(err)
	}
	switch method {
	case "initialized":
		s.mu.Lock()
		wasWorkspaceIndexEnabled := s.workspaceIndexEnabled
		workspaceIndexGenerationBeforeConfiguration := s.workspaceIndexGeneration
		s.mu.Unlock()
		workspaceIndexConfigurationBefore := workspaceArtifactFingerprint("")
		if wasWorkspaceIndexEnabled {
			workspaceIndexConfigurationBefore = s.workspaceIndexConfigurationFingerprint()
		}
		s.pauseWorkspaceIndexScheduling(true)
		err := s.refreshWorkspaceConfiguration(ctx)
		s.pauseWorkspaceIndexScheduling(false)
		if err != nil {
			s.logServerWarning("[asp-lsp] workspace.configuration.failed: " + err.Error())
		}
		s.mu.Lock()
		workspaceIndexEnabledAfterConfiguration := s.workspaceIndexEnabled
		workspaceIndexGenerationAfterConfiguration := s.workspaceIndexGeneration
		s.mu.Unlock()
		if wasWorkspaceIndexEnabled && workspaceIndexEnabledAfterConfiguration &&
			workspaceIndexGenerationAfterConfiguration == workspaceIndexGenerationBeforeConfiguration &&
			s.workspaceIndexConfigurationFingerprint() == workspaceIndexConfigurationBefore {
			return nil
		}
		s.cancelWorkspaceIndexWorker()
		s.workspaceIndexStateMu.Lock()
		s.configureDiskAnalysisCache()
		// Notification contexts are cancelled as soon as their handler returns.
		// The worker has its own explicit cancellation lifecycle.
		s.activateWorkspaceIndexing(context.Background())
		s.scheduleWorkspaceIndex("initialized")
		s.workspaceIndexStateMu.Unlock()
		return nil
	case "workspace/didChangeWorkspaceFolders":
		var p didChangeWorkspaceFoldersParams
		if err := decodeNotificationParams(params, &p); err != nil {
			return err
		}
		s.didChangeWorkspaceFolders(p)
		return nil
	case "workspace/didRenameFiles":
		var p didRenameFilesParams
		if err := decodeNotificationParams(params, &p); err != nil {
			return err
		}
		return s.didRenameFiles(p)
	case "workspace/didCreateFiles":
		var p didFileOperationParams
		if err := decodeNotificationParams(params, &p); err != nil {
			return err
		}
		return s.didFileOperations(p, fileChangeCreated)
	case "workspace/didDeleteFiles":
		var p didFileOperationParams
		if err := decodeNotificationParams(params, &p); err != nil {
			return err
		}
		return s.didFileOperations(p, fileChangeDeleted)
	case "workspace/didChangeWatchedFiles":
		var p didChangeWatchedFilesParams
		if err := decodeNotificationParams(params, &p); err != nil {
			return err
		}
		return s.didChangeWatchedFiles(p)
	case "workspace/didChangeConfiguration":
		return s.handleDidChangeConfiguration(params)
	case "textDocument/willSave":
		var p willSaveWaitUntilParams
		if err := decodeNotificationParams(params, &p); err != nil {
			return err
		}
		if p.TextDocument.URI == "" {
			return nil
		}
		s.cancelScheduledDiagnostics(p.TextDocument.URI)
		return s.publishDiagnostics(p.TextDocument.URI)
	case "textDocument/didOpen":
		var p didOpenParams
		if err := decodeNotificationParams(params, &p); err != nil {
			return err
		}
		doc := core.NewTextDocument(p.TextDocument.URI, p.TextDocument.LanguageID, p.TextDocument.Version, p.TextDocument.Text)
		defaultLanguage := "VBScript"
		previousOpen := (*core.TextDocument)(nil)
		workspaceDoc := (*core.TextDocument)(nil)
		sameSource := false
		preserveSource := false
		keepAnalysis := false
		s.mu.Lock()
		defaultLanguage = s.settings.DefaultLanguage
		previousOpen = s.openDocumentByURILocked(p.TextDocument.URI)
		workspaceDoc = s.workspaceDocumentByURILocked(p.TextDocument.URI)
		sameSource = previousOpen != nil && previousOpen.Text == doc.Text || workspaceDoc != nil && workspaceDoc.Text == doc.Text
		preserveSource = sameSource && !s.parsedCacheHasIncompatibleLanguageLocked(p.TextDocument.URI, doc.Text, defaultLanguage)
		keepAnalysis = preserveSource && s.documentOpenAnalysisMatchesSourceLocked(p.TextDocument.URI, doc.Text, defaultLanguage)
		s.mu.Unlock()
		if !keepAnalysis {
			s.cancelDocumentOpenAnalysis(p.TextDocument.URI)
		}

		s.mu.Lock()
		// Re-read the source relationships after cancelling a superseded job. The
		// notification queue serializes revisions, but this also keeps direct test
		// callers from publishing an entry based on a stale pointer.
		previousOpen = s.openDocumentByURILocked(p.TextDocument.URI)
		workspaceDoc = s.workspaceDocumentByURILocked(p.TextDocument.URI)
		sameSource = previousOpen != nil && previousOpen.Text == doc.Text || workspaceDoc != nil && workspaceDoc.Text == doc.Text
		preserveSource = sameSource && !s.parsedCacheHasIncompatibleLanguageLocked(p.TextDocument.URI, doc.Text, defaultLanguage)
		referencesChanged := workspaceDoc == nil || workspaceDoc.Text != doc.Text
		if previousOpen != nil && previousOpen.Text == doc.Text {
			referencesChanged = false
		}
		s.rememberOpenDocumentLocked(p.TextDocument.URI, doc)
		if preserveSource {
			s.rememberDocumentRevisionLocked(doc)
		} else {
			s.deleteParsedCacheForURILocked(p.TextDocument.URI)
			s.deleteSemanticForURILocked(p.TextDocument.URI)
			s.rememberDocumentTextLocked(doc)
		}
		s.mu.Unlock()
		openedParsed := s.parseTextDocumentWithSnapshotSchedule(doc, defaultLanguage, false)
		s.clearValidatedDocumentVersion(p.TextDocument.URI)
		s.logDebugTrace("document.open", "[asp-lsp] document.open: "+p.TextDocument.URI)
		if err := s.publishInitialSyntaxDiagnostics(doc, openedParsed); err != nil {
			return err
		}
		s.mu.Lock()
		// An exact source match already has a published artifact or an active
		// coalesced worker. Keep that state and avoid rebuilding the snapshot.
		skipAnalysis := preserveSource && !referencesChanged &&
			(s.documentOpenAnalysisMatchesSourceLocked(p.TextDocument.URI, doc.Text, defaultLanguage) ||
				s.workspaceArtifactMatchesParsedLocked(doc, openedParsed))
		s.mu.Unlock()
		if skipAnalysis {
			return nil
		}
		s.scheduleDocumentOpenAnalysis(doc, openedParsed, workspaceDoc, referencesChanged, ctx)
		return nil
	case "textDocument/didChange":
		var p didChangeParams
		if message.didChangeParams != nil {
			p = *message.didChangeParams
		} else if err := decodeNotificationParams(params, &p); err != nil {
			return err
		}
		s.cancelDocumentOpenAnalysis(p.TextDocument.URI)
		s.mu.Lock()
		current := s.openDocumentByURILocked(p.TextDocument.URI)
		languageID := "classic-asp"
		currentVersion := 0
		currentText := ""
		if current != nil {
			languageID = current.LanguageID
			currentVersion = current.Version
			currentText = current.Text
		}
		previousParsed := (*core.ParsedDocument)(nil)
		defaultLanguage := s.settings.DefaultLanguage
		incrementalMode := s.settings.IncrementalMode
		incrementalAnalysis := s.settings.IncrementalAnalysis
		previousCacheKey := parsedDocumentCacheKey(p.TextDocument.URI)
		if cached, ok := s.parsedCache[previousCacheKey]; ok && cached.Text == currentText && cached.DefaultLanguage == defaultLanguage {
			previousParsed = cached.Parsed
		}
		s.mu.Unlock()

		// Requests keep using the previous immutable document revision while the
		// next revision is prepared.  The pointer stored in s.documents is never
		// mutated in place.
		doc := current.Clone()
		if doc == nil {
			doc = core.NewTextDocument(p.TextDocument.URI, languageID, currentVersion, currentText)
		}
		incremental := false
		fullParseReason := ""
		incrementalChanges := make([]core.IncrementalChange, 0, len(p.ContentChanges))
		for _, change := range p.ContentChanges {
			incrementalChange := core.IncrementalChange{Range: change.Range, Text: change.Text}
			if change.Range != nil {
				incremental = true
				incrementalChange.ByteStart = doc.OffsetAt(change.Range.Start)
				incrementalChange.ByteEnd = doc.OffsetAt(change.Range.End)
				incrementalChange.HasByteRange = true
				if changeTouchesASPBoundary(doc, change) {
					fullParseReason = "incremental resync failed"
				}
			} else {
				fullParseReason = "full document replacement"
			}
			incrementalChanges = append(incrementalChanges, incrementalChange)
			doc.ApplyChange(change.Range, change.Text, p.TextDocument.Version)
		}
		if fullParseReason != "" {
			incremental = false
		}
		if incremental && (incrementalMode == "off" || !incrementalAnalysis) {
			incremental = false
			if fullParseReason == "" {
				fullParseReason = "incremental analysis disabled"
			}
		}
		if incremental && previousParsed == nil {
			incremental = false
			if fullParseReason == "" {
				fullParseReason = "incremental source unavailable"
			}
		}
		javascriptProjectFileChanged := isJavaScriptProjectFile(fileURIPath(p.TextDocument.URI))
		var incrementalParsed *core.ParsedDocument
		if incremental && previousParsed != nil {
			updated := core.UpdateParsedDocument(previousParsed, incrementalChanges, core.Settings{DefaultLanguage: defaultLanguage})
			if updated.Incremental {
				incrementalParsed = updated.Parsed
			} else {
				incremental = false
				if fullParseReason == "" {
					fullParseReason = "incremental resync failed: " + updated.Reason
				}
			}
		}
		javascriptAffected := false
		if incrementalParsed != nil {
			javascriptAffected = incrementalParsed.ChangeImpact.Affects(core.LanguageJavaScript) || incrementalParsed.ChangeImpact.Affects(core.LanguageJScript)
		} else {
			// A full replacement has no trustworthy language-local impact. Include
			// the previous text so removing the final script still deletes its
			// virtual project file synchronously.
			javascriptAffected = documentTextHasJavaScript(currentText) || documentTextHasJavaScript(doc.Text)
		}
		s.mu.Lock()
		s.rememberOpenDocumentLocked(p.TextDocument.URI, doc)
		s.deleteSemanticForURILocked(p.TextDocument.URI)
		if incrementalParsed != nil {
			s.advanceParsedCacheRevisionLocked(previousCacheKey)
			s.parsedCache[previousCacheKey] = parsedDocumentCacheEntry{Version: doc.Version, Text: doc.Text, DefaultLanguage: defaultLanguage, Parsed: incrementalParsed}
		} else {
			s.deleteParsedCacheForURILocked(p.TextDocument.URI)
		}
		s.rememberDocumentTextLocked(doc)
		if javascriptProjectFileChanged && isJavaScriptProjectConfigFile(fileURIPath(p.TextDocument.URI)) {
			s.resetJavaScriptProjectLocked()
		} else if javascriptAffected || javascriptProjectFileChanged {
			s.markJavaScriptDocumentChangedLocked(p.TextDocument.URI)
		} else {
			// Non-JavaScript edits can shift ASP source-map offsets without
			// changing the TypeScript virtual file. Refresh that owner lazily while
			// keeping the compiler project generation intact.
			s.markJavaScriptDocumentMappingChangedLocked(p.TextDocument.URI)
		}
		s.mu.Unlock()
		if incrementalParsed != nil {
			s.rememberParsedDocument(doc, incrementalParsed, defaultLanguage)
		}
		graphParsed := incrementalParsed
		if graphParsed == nil {
			graphParsed = s.parseTextDocument(doc, defaultLanguage)
		}
		// Dependency invalidation is a cheap source-delta operation and must be
		// visible before any request for the new revision. Heavy artifact
		// construction remains in the background job below.
		if previousParsed != nil {
			if workspaceReferenceIncludeFingerprint(previousParsed) != workspaceReferenceIncludeFingerprint(graphParsed) {
				s.refreshWorkspaceIncludeGraphFile(graphParsed)
				if !s.invalidateWorkspaceReferenceTopology(previousParsed, graphParsed) {
					s.markWorkspaceReferenceRevisionDirty()
				}
				s.invalidateWorkspaceDiagnosticsForParsedChange(previousParsed, graphParsed)
			} else {
				// Requests for the new revision must not observe reference results from
				// the previous source, but deriving the changed-name set is a whole-file
				// operation. Advance the revision synchronously and let the coalesced
				// artifact job publish the precise name and dependency deltas.
				if !s.invalidateWorkspaceReferenceRevisionIncremental(previousParsed, graphParsed) {
					s.markWorkspaceReferenceRevisionDirty()
				}
				s.invalidateWorkspaceDiagnosticsURI(p.TextDocument.URI)
			}
		}
		// Workspace artifacts are derived in a coalescing, revision-cancellable
		// job. Completion and hover can use graphParsed immediately instead of
		// waiting for references, graphs, every virtual document, and disk writes.
		s.scheduleDocumentChangeAnalysis(doc, graphParsed, ctx)
		s.clearValidatedDocumentVersion(p.TextDocument.URI)
		if (javascriptAffected || javascriptProjectFileChanged) && !incremental {
			s.logDebugSummary("[asp-lsp] projectUpdate.scheduled: " + p.TextDocument.URI)
		}
		if fullParseReason != "" {
			s.logDebugVerbose("[asp-lsp] analysis.parse.skeleton: " + p.TextDocument.URI)
			s.logDebugVerbose("[asp-lsp] analysis.parse.impact: " + p.TextDocument.URI + " mode=full reason=" + fullParseReason)
			if fullParseReason == "full document replacement" {
				s.logDebugVerbose("[asp-lsp] diagnostics.include.stale: " + p.TextDocument.URI)
			}
		} else if incremental {
			s.logDebugVerbose("[asp-lsp] analysis.parse.incremental: " + p.TextDocument.URI)
			s.logDebugVerbose("[asp-lsp] analysis.vbscript.reuse: " + p.TextDocument.URI)
			s.logDebugVerbose("[asp-lsp] check.javascriptSyntax.reuse: " + p.TextDocument.URI)
			s.logDebugVerbose("[asp-lsp] check.javascriptDiagnostics.reuse: " + p.TextDocument.URI)
			s.logDebugVerbose("[asp-lsp] check.vbscript.diagnostics.reuse: " + p.TextDocument.URI)
			s.logDebugVerbose("[asp-lsp] htmlDiagnostics.reuse: " + p.TextDocument.URI)
			s.logDebugVerbose("[asp-lsp] cssDiagnostics.reuse: " + p.TextDocument.URI)
			s.logDebugVerbose("[asp-lsp] includeDiagnostics.reuse: " + p.TextDocument.URI)
		}
		return s.scheduleDiagnostics(p.TextDocument.URI)
	case "textDocument/didSave":
		var p textDocumentIdentifierParams
		if err := decodeNotificationParams(params, &p); err != nil {
			return err
		}
		if p.TextDocument.URI == "" {
			return nil
		}
		s.logDebugSummary("[asp-lsp] diagnostics.reuse: " + p.TextDocument.URI)
		fileName := fileURIPath(p.TextDocument.URI)
		s.invalidateFsPath(fileName)
		if fileName != "" && strings.EqualFold(filepath.Base(filepath.Clean(fileName)), ".gitignore") && s.workspaceGitIgnoreEventRelevant(filepath.Clean(fileName)) {
			invalidateJavaScriptProjectDiscoverySettings(s)
		}
		if fileName != "" {
			s.invalidateSourceSnapshot(fileName)
		}
		doc := s.documentByURI(p.TextDocument.URI)
		analysisPending := doc != nil && s.documentChangeAnalysisPending(p.TextDocument.URI, doc.Version)
		if analysisPending {
			// Saving is a revision barrier. Flush the coalesced edit artifact from
			// the in-memory document now so a following file watcher event observes
			// the same published revision instead of racing a delayed edit job.
			s.cancelDocumentOpenAnalysis(p.TextDocument.URI)
			analysisPending = false
		}
		if doc == nil {
			s.cancelDocumentOpenAnalysis(p.TextDocument.URI)
		}
		if doc == nil && fileName != "" {
			if content, err := s.readWorkspaceTextFile(fileName); err == nil {
				doc = core.NewTextDocument(p.TextDocument.URI, "classic-asp", 0, content)
			}
		}
		needsValidation := doc == nil
		if doc != nil {
			needsValidation = !s.documentValidationIsCurrent(p.TextDocument.URI, doc.Version)
		}
		javascriptChanged := false
		javascriptArtifactChanged := false
		var parsed *core.ParsedDocument
		if doc != nil {
			javascriptChanged = documentTextHasJavaScript(doc.Text) || isJavaScriptProjectFile(fileName)
			if fileName != "" {
				s.indexSavedWorkspaceFile(fileName, doc)
			}
			if needsValidation {
				s.mu.Lock()
				s.deleteSemanticForURILocked(p.TextDocument.URI)
				if javascriptChanged && isJavaScriptProjectConfigFile(fileName) {
					s.resetJavaScriptProjectLocked()
				}
				s.mu.Unlock()
			}
			parsed = s.parseTextDocument(doc, s.settings.DefaultLanguage)
			if !analysisPending {
				revision := s.applyWorkspaceDocumentRevision(doc, parsed)
				javascriptArtifactChanged = workspaceVirtualDeltaHasJavaScript(revision.Delta.ChangedVirtualLanguages)
			}
		}
		s.logDebugTrace("document.save", "[asp-lsp] document.save: "+p.TextDocument.URI)
		if javascriptChanged && (javascriptArtifactChanged || isJavaScriptProjectConfigFile(fileName)) {
			s.logDebugSummary("[asp-lsp] projectUpdate.scheduled: " + p.TextDocument.URI)
		}
		if doc != nil && needsValidation {
			if err := s.scheduleDiagnostics(p.TextDocument.URI); err != nil {
				return err
			}
		} else if doc != nil {
			s.logDebugSummary("[asp-lsp] diagnostics.reuse: " + p.TextDocument.URI)
		}
		return nil
	case "textDocument/didClose":
		var p textDocumentIdentifierParams
		if err := decodeNotificationParams(params, &p); err != nil {
			return err
		}
		s.cancelDocumentOpenAnalysis(p.TextDocument.URI)
		path := cleanFileURIPath(p.TextDocument.URI)
		eligible := path == "" || s.workspaceFileEligibleForAutomaticIndexWithOpenOverride(path, false)
		s.mu.Lock()
		openDoc := s.openDocumentByURILocked(p.TextDocument.URI)
		workspaceDoc := s.workspaceDocumentByURILocked(p.TextDocument.URI)
		preserveIndexedState := eligible && openDoc != nil && workspaceDoc != nil && openDoc.Text == workspaceDoc.Text
		var openParsed *core.ParsedDocument
		if openDoc != nil {
			openParsed, _ = s.parsedCacheHitLocked(parsedDocumentCacheKey(p.TextDocument.URI), parsedDocumentCacheEntry{
				Version: openDoc.Version, Text: openDoc.Text, DefaultLanguage: s.settings.DefaultLanguage,
			})
		}
		referencesChanged := openDoc != nil && (!eligible || workspaceDoc == nil || workspaceDoc.Text != openDoc.Text)
		s.deleteOpenDocumentLocked(p.TextDocument.URI)
		if !preserveIndexedState {
			s.deleteParsedCacheForURILocked(p.TextDocument.URI)
			s.deleteSemanticForURILocked(p.TextDocument.URI)
		}
		s.mu.Unlock()
		if path != "" && !eligible {
			s.mu.Lock()
			delete(s.workspace, p.TextDocument.URI)
			workspaceDoc = nil
			s.mu.Unlock()
		}
		if referencesChanged {
			var workspaceParsed *core.ParsedDocument
			if openParsed == nil && openDoc != nil {
				// The closing overlay is only needed as an immutable delta input. Do
				// not publish it back into URI caches after didClose invalidated them.
				openParsed = core.ParseDocument(openDoc.URI, openDoc.Text, core.Settings{DefaultLanguage: s.settings.DefaultLanguage})
			}
			if workspaceDoc != nil {
				workspaceParsed = s.parseTextDocument(workspaceDoc, s.settings.DefaultLanguage)
			}
			published := false
			if workspaceDoc != nil && workspaceParsed != nil {
				revision := s.applyWorkspaceDocumentRevision(workspaceDoc, workspaceParsed)
				published = revision.Manifest != nil && !revision.Stale
			}
			if !published {
				s.invalidateWorkspaceReferencesForParsedChange(openParsed, workspaceParsed)
			}
			if workspaceParsed == nil {
				s.invalidateWorkspaceDiagnosticsURI(p.TextDocument.URI)
			} else if !published {
				s.invalidateWorkspaceDiagnosticsForParsedChange(openParsed, workspaceParsed)
			}
			if workspaceParsed == nil {
				s.removeWorkspaceDocumentRevision(p.TextDocument.URI)
				if path := cleanFileURIPath(p.TextDocument.URI); path != "" {
					s.mu.Lock()
					if s.workspaceIncludeGraph != nil {
						affected, changed := s.workspaceIncludeGraph.ApplyDeleteDelta(path)
						if changed {
							s.workspaceIncludeGraphRevision++
							s.invalidateWorkspaceReferenceFamilyLocked(affected.IdentityMembership())
						}
					}
					s.mu.Unlock()
				}
			}
		}
		s.clearValidatedDocumentVersion(p.TextDocument.URI)
		if !preserveIndexedState {
			s.removeDocumentStoreForURI(p.TextDocument.URI)
		}
		s.cancelScheduledDiagnostics(p.TextDocument.URI)
		if err := s.clearPublishedDiagnosticTargetsForOwner(p.TextDocument.URI); err != nil {
			return err
		}
		return nil
	default:
		return nil
	}
}

func (s *Server) workspaceIndexConfigurationFingerprint() workspaceArtifactFingerprint {
	s.mu.Lock()
	roots := make([]string, 0, len(s.workspaceRoots)+1)
	for _, root := range s.workspaceRoots {
		if root.Path != "" {
			roots = append(roots, filepath.Clean(root.Path))
		}
	}
	if len(roots) == 0 && s.rootPath != "" {
		roots = append(roots, filepath.Clean(s.rootPath))
	}
	settings := struct {
		Roots             []string
		Includes          []string
		Excludes          []string
		RespectGitIgnore  bool
		DefaultLanguage   string
		LegacyEncoding    string
		IncludePaths      []string
		VirtualRoots      []string
		VirtualRoot       string
		WindowsResolution bool
		CaseResolution    string
		ScanChunkSize     int
		CacheDirectory    string
	}{
		Roots:             roots,
		Includes:          append([]string(nil), s.settings.WorkspaceIncludeGlobs...),
		Excludes:          append([]string(nil), s.settings.WorkspaceExcludeGlobs...),
		RespectGitIgnore:  s.settings.WorkspaceRespectGitIgnore,
		DefaultLanguage:   s.settings.DefaultLanguage,
		LegacyEncoding:    s.settings.LegacyEncoding,
		IncludePaths:      append([]string(nil), s.settings.IncludePaths...),
		VirtualRoots:      append([]string(nil), s.settings.VirtualRoots...),
		VirtualRoot:       s.settings.VirtualRoot,
		WindowsResolution: s.settings.WindowsPathResolution,
		CaseResolution:    s.settings.NetworkCaseResolution,
		ScanChunkSize:     s.settings.WorkspaceScanChunkSize,
		CacheDirectory:    s.settings.CacheDirectory,
	}
	s.mu.Unlock()
	sort.Strings(settings.Roots)
	return workspaceFingerprint(settings)
}
