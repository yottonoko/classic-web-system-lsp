package lspserver

import (
	"context"
	"os"
	"path/filepath"
	"sort"
	"strings"

	"github.com/yottonoko/classic-web-system-lsp/internal/core"
	"github.com/yottonoko/classic-web-system-lsp/internal/graph"
	"github.com/yottonoko/classic-web-system-lsp/internal/lsp"
	"github.com/yottonoko/classic-web-system-lsp/internal/vbscript"
	workspacepkg "github.com/yottonoko/classic-web-system-lsp/internal/workspace"
)

func (s *Server) documentByURI(uri string) *core.TextDocument {
	s.mu.Lock()
	defer s.mu.Unlock()
	if doc := s.openDocumentByURILocked(uri); doc != nil {
		return doc
	}
	if doc := s.workspace[uri]; doc != nil {
		return doc
	}
	if strings.HasPrefix(strings.ToLower(uri), "file:") {
		for candidateURI, doc := range s.documents {
			if workspacepkg.SameFileIdentityURI(candidateURI, uri) {
				return doc
			}
		}
		for candidateURI, doc := range s.workspace {
			if workspacepkg.SameFileIdentityURI(candidateURI, uri) {
				return doc
			}
		}
	}
	return nil
}

func (s *Server) lineCommentEdits(params lineCommentEditsParams) *lineCommentEditsResult {
	s.mu.Lock()
	doc := s.openDocumentByURILocked(params.TextDocument.URI)
	if doc != nil {
		doc = doc.Clone()
	}
	defaultLanguage := s.settings.DefaultLanguage
	s.mu.Unlock()
	if doc == nil || doc.Version != params.TextDocument.Version || doc.LanguageID != "classic-asp" {
		return nil
	}
	plan := core.ClassicASPLineCommentPlan(doc.URI, doc.Text, params.Selections, core.Settings{
		DefaultLanguage: defaultLanguage,
	})
	edits := plan.Edits
	if edits == nil {
		edits = []lsp.TextEdit{}
	}
	return &lineCommentEditsResult{
		Version:    doc.Version,
		Edits:      edits,
		NoOpReason: plan.NoOpReason,
	}
}

func (s *Server) openDocumentByURILocked(uri string) *core.TextDocument {
	if doc := s.documents[uri]; doc != nil {
		return doc
	}
	if strings.HasPrefix(strings.ToLower(uri), "file:") {
		for _, doc := range s.documents {
			if doc != nil && workspacepkg.SameFileIdentityURI(doc.URI, uri) {
				return doc
			}
		}
	}
	return nil
}

func (s *Server) rememberOpenDocumentLocked(uri string, doc *core.TextDocument) {
	if strings.HasPrefix(strings.ToLower(uri), "file:") {
		for key := range s.documents {
			if workspacepkg.SameFileIdentityURI(key, uri) {
				s.documents[key] = doc
			}
		}
	}
	for _, key := range documentURIKeys(uri) {
		s.documents[key] = doc
	}
}

func (s *Server) deleteOpenDocumentLocked(uri string) {
	doc := s.openDocumentByURILocked(uri)
	workspaceDoc := s.workspaceDocumentByURILocked(uri)
	javascriptChanged := javascriptDocumentRemovalChangesProject(doc, workspaceDoc)
	deleted := false
	for key, candidate := range s.documents {
		if key == uri || candidate == doc || (strings.HasPrefix(strings.ToLower(key), "file:") && workspacepkg.SameFileIdentityURI(key, uri)) {
			delete(s.documents, key)
			deleted = true
		}
	}
	if deleted && javascriptChanged {
		s.markJavaScriptDocumentsChangedLocked()
	}
}

func javascriptDocumentRemovalChangesProject(openDocument, workspaceDocument *core.TextDocument) bool {
	if openDocument == nil {
		return false
	}
	if workspaceDocument == nil {
		return documentHasJavaScript(openDocument)
	}
	if openDocument.Text == workspaceDocument.Text && openDocument.LanguageID == workspaceDocument.LanguageID {
		return false
	}
	return documentHasJavaScript(openDocument) || documentHasJavaScript(workspaceDocument)
}

func documentHasJavaScript(document *core.TextDocument) bool {
	if document == nil {
		return false
	}
	if documentTextHasJavaScript(document.Text) || isJavaScriptProjectFile(fileURIPath(document.URI)) {
		return true
	}
	switch strings.ToLower(document.LanguageID) {
	case "javascript", "javascriptreact", "typescript", "typescriptreact":
		return true
	default:
		return false
	}
}

func (s *Server) deleteSemanticForURILocked(uri string) {
	for key := range s.semantic {
		if key == uri || (strings.HasPrefix(strings.ToLower(key), "file:") && workspacepkg.SameFileIdentityURI(key, uri)) {
			delete(s.semantic, key)
		}
	}
}

func (s *Server) clearWorkspaceReferenceCache() {
	s.mu.Lock()
	s.clearWorkspaceReferenceCacheLocked()
	s.mu.Unlock()
	s.requestCodeLensRefresh("references.invalidated")
}

func (s *Server) markWorkspaceReferenceRevisionDirty() {
	s.mu.Lock()
	s.clearWorkspaceReferenceCacheLocked()
	s.mu.Unlock()
	s.requestCodeLensRefresh("references.revision.changed")
}

func (s *Server) invalidateWorkspaceReferenceRevisionIncremental(previous, current *core.ParsedDocument) bool {
	if previous == nil || current == nil {
		return false
	}
	if _, ok := current.PreviousRevisionText(); !ok {
		return false
	}
	if core.IncrementalChangeAfterLanguage(current, core.LanguageVBScript) {
		return true
	}
	shard, ok := vbscript.CachedReferenceShard(previous)
	if !ok {
		return false
	}
	names := append([]string(nil), shard.NormalizedNames()...)
	impact := current.ChangeImpact
	if impact.OldStart >= 0 && impact.OldEnd >= impact.OldStart && impact.OldEnd <= len(previous.Text) {
		names = append(names, incrementalReferenceIdentifiers(previous.Text[impact.OldStart:impact.OldEnd])...)
	}
	names = append(names, incrementalReferenceIdentifiers(impact.Replacement)...)
	if len(names) > 0 {
		s.captureWorkspaceReferenceDocumentCountReuseBaseline(previous, current)
		s.invalidateWorkspaceReferenceNames(previous, current, names)
	}
	return true
}

// captureWorkspaceReferenceDocumentCountReuseBaseline records the completed
// batch that the artifact worker may reuse. It intentionally does not derive a
// declaration plan; that potentially expensive work belongs to the coalesced
// artifact publication path.
func (s *Server) captureWorkspaceReferenceDocumentCountReuseBaseline(previous, current *core.ParsedDocument) {
	if previous == nil || current == nil || previous.URI == "" || current.URI == "" ||
		!workspacepkg.SameFileIdentityURI(previous.URI, current.URI) {
		return
	}
	documentID := workspaceDocumentIDFromURI(current.URI)
	currentFingerprint := workspaceFingerprint(current.Text)
	s.mu.Lock()
	if s.referencePendingDocumentCountReuse == nil {
		s.referencePendingDocumentCountReuse = map[workspaceDocumentID]*workspaceReferenceDocumentCountSnapshot{}
	}
	if pending := s.referencePendingDocumentCountReuse[documentID]; pending != nil && pending.generation == s.referenceGeneration {
		pending.currentSourceFingerprint = currentFingerprint
		s.mu.Unlock()
		return
	}
	manifest := s.workspaceArtifacts[documentID]
	state := s.referenceBatch[referenceBatchCacheKey(previous.URI, 0, s.referenceGeneration)]
	if !s.workspaceReferenceIndexReadyLocked() || manifest == nil || manifest.CST == nil ||
		manifest.SourceFingerprint != workspaceFingerprint(previous.Text) ||
		state == nil || !workspaceReferenceBatchCountsMatch(state, state.declarations) {
		delete(s.referencePendingDocumentCountReuse, documentID)
		s.mu.Unlock()
		return
	}
	previousDeclarations := append([]vbUsageDeclaration(nil), state.declarations...)
	previousCounts := append([]int(nil), state.finalCounts...)
	if len(previousDeclarations) == 0 {
		delete(s.referencePendingDocumentCountReuse, documentID)
		s.mu.Unlock()
		return
	}
	for _, declaration := range previousDeclarations {
		if declaration.Implicit {
			delete(s.referencePendingDocumentCountReuse, documentID)
			s.mu.Unlock()
			return
		}
	}
	s.referencePendingDocumentCountReuse[documentID] = &workspaceReferenceDocumentCountSnapshot{
		previousSourceFingerprint: manifest.SourceFingerprint,
		currentSourceFingerprint:  currentFingerprint,
		generation:                s.referenceGeneration,
		previous:                  previous,
		previousDeclarations:      previousDeclarations,
		previousCounts:            previousCounts,
	}
	s.mu.Unlock()
}

func incrementalReferenceIdentifiers(text string) []string {
	names := make([]string, 0, 8)
	for start := 0; start < len(text); {
		for start < len(text) && !isCompletionIdentifier(text[start]) {
			start++
		}
		end := start
		for end < len(text) && isCompletionIdentifier(text[end]) {
			end++
		}
		if end > start && (text[start] == '_' || text[start] >= 'A' && text[start] <= 'Z' || text[start] >= 'a' && text[start] <= 'z') {
			names = append(names, strings.ToLower(text[start:end]))
		}
		start = max(end, start+1)
	}
	return names
}

func (s *Server) clearWorkspaceReferenceCacheLocked() {
	for _, state := range s.referenceBatch {
		if state.cancel != nil {
			state.cancel()
		}
	}
	s.referenceGeneration++
	s.referenceCounts = map[workspaceReferenceTargetKey]int{}
	s.referencePartialCounts = map[workspaceReferenceTargetKey]int{}
	s.referenceResults = map[workspaceReferenceTargetKey][]lsp.Location{}
	s.referenceBatch = map[workspaceReferenceBatchKey]*workspaceReferenceBatchState{}
	s.referenceInflight = map[workspaceReferenceTargetKey]*workspaceReferenceInflight{}
	s.referenceDocuments = map[string][]*core.ParsedDocument{}
	s.referenceScopes = map[workspaceReferenceScopeCacheKey]workspaceReferenceScopeSnapshot{}
	s.referenceImplicitPlans = map[workspaceReferenceImplicitPlanKey]map[string]map[string]struct{}{}
	s.referenceCountSummariesRestored = map[*core.ParsedDocument]struct{}{}
	s.referenceDeclarationPlans = map[*core.ParsedDocument]workspaceReferenceDeclarationPlan{}
	s.referenceDescriptorFingerprints = map[string]*workspaceReferenceDescriptorFingerprintCache{}
	s.workspaceReferencePendingPromotion = nil
	s.referencePendingDocumentCountReuse = map[workspaceDocumentID]*workspaceReferenceDocumentCountSnapshot{}
	s.resetWorkspaceReferenceNameIndexLocked()
}

func documentURIKeys(uri string) []string {
	keys := []string{uri}
	canonical := canonicalGraphURI(uri)
	if canonical != "" && canonical != uri {
		keys = append(keys, canonical)
	}
	return keys
}

func dedupeCompletionItems(items []lsp.CompletionItem) []lsp.CompletionItem {
	seen := map[string]struct{}{}
	result := make([]lsp.CompletionItem, 0, len(items))
	for _, item := range items {
		key := strings.ToLower(item.Label)
		if _, ok := seen[key]; ok {
			continue
		}
		seen[key] = struct{}{}
		result = append(result, item)
	}
	return result
}

func vbscriptCompletionKind(kind string) lsp.CompletionItemKind {
	switch kind {
	case "function":
		return lsp.CompletionItemKindFunction
	case "sub":
		return lsp.CompletionItemKindMethod
	case "class":
		return lsp.CompletionItemKindClass
	case "const":
		return lsp.CompletionItemKindValue
	default:
		return lsp.CompletionItemKindVariable
	}
}

func (s *Server) configureWorkspace(params initializeParams) {
	roots := workspaceRootsFromInitializeParams(params)
	rootURI := params.RootURI
	if rootURI == "" && len(roots) > 0 {
		rootURI = roots[0].URI
	}
	rootPath := fileURIPath(rootURI)
	if rootPath == "" && params.RootPath != "" {
		rootPath = filepath.Clean(params.RootPath)
		rootURI = filePathURI(rootPath)
	}
	if rootPath == "" && len(roots) > 0 {
		rootPath = roots[0].Path
		rootURI = roots[0].URI
	}
	s.mu.Lock()
	s.rootURI = rootURI
	s.rootPath = rootPath
	s.workspaceRoots = roots
	s.resetTrustedFilesystemRootCacheLocked()
	s.mu.Unlock()
}

func (s *Server) indexSavedWorkspaceFile(path string, doc *core.TextDocument) {
	cleanPath := filepath.Clean(path)
	if !isWorkspaceASPFile(cleanPath) {
		return
	}
	if !s.workspaceFileEligibleForAutomaticIndex(cleanPath) {
		uri := filePathURI(cleanPath)
		s.mu.Lock()
		delete(s.workspace, uri)
		s.deleteParsedCacheForURILocked(uri)
		s.mu.Unlock()
		return
	}
	s.mu.Lock()
	workspace := s.workspace
	s.mu.Unlock()
	uri := filePathURI(cleanPath)
	if doc == nil {
		content, err := s.readWorkspaceTextFile(cleanPath)
		if err != nil {
			return
		}
		doc = core.NewTextDocument(uri, "classic-asp", 0, content)
	} else if doc.URI != uri {
		doc = core.NewTextDocument(uri, doc.LanguageID, doc.Version, doc.Text)
	}
	s.mu.Lock()
	workspace[uri] = doc
	s.rememberDocumentTextLocked(doc)
	s.mu.Unlock()
}

func (s *Server) workspaceFileEligibleForAutomaticIndex(path string) bool {
	return s.workspaceFileEligibleForAutomaticIndexWithOpenOverride(path, true)
}

func (s *Server) workspaceFileEligibleForAutomaticIndexWithOpenOverride(path string, allowOpen bool) bool {
	cleanPath := filepath.Clean(path)
	uri := filePathURI(cleanPath)
	s.mu.Lock()
	roots := append([]workspaceRoot(nil), s.workspaceRoots...)
	if len(roots) == 0 && s.rootPath != "" {
		roots = []workspaceRoot{{URI: s.rootURI, Path: s.rootPath}}
	}
	includes := append([]string(nil), s.settings.WorkspaceIncludeGlobs...)
	excludes := append([]string(nil), s.settings.WorkspaceExcludeGlobs...)
	includePaths := append([]string(nil), s.settings.IncludePaths...)
	virtualRoots := append([]string(nil), s.settings.VirtualRoots...)
	if s.settings.VirtualRoot != "" {
		virtualRoots = append(virtualRoots, s.settings.VirtualRoot)
	}
	respectGitIgnore := s.settings.WorkspaceRespectGitIgnore
	cacheDirectory := strings.TrimSpace(s.settings.CacheDirectory)
	open := s.openDocumentByURILocked(uri) != nil
	directlyReferenced := s.workspaceIncludeGraph != nil && len(s.workspaceIncludeGraph.DependentFileNamesForTargets([]string{cleanPath}, false)) > 0
	s.mu.Unlock()
	if !workspacePathWithinAnyBoundary(&s.trustedPaths, cleanPath, roots, includePaths, virtualRoots) {
		return false
	}
	if workspacePathIsCacheDirectoryDescendant(cleanPath, roots, cacheDirectory) {
		return false
	}
	if (allowOpen && open) || directlyReferenced {
		return true
	}
	rootPath := workspaceRootPathForPath(cleanPath, roots)
	if rootPath == "" {
		return false
	}
	relative, err := filepath.Rel(rootPath, cleanPath)
	if err != nil {
		return false
	}
	if !workspaceGraphFileAllowed(filepath.ToSlash(relative), includes, excludes, nil) {
		return false
	}
	gitIgnoreGlobs := []string{}
	if respectGitIgnore {
		gitIgnoreGlobs = s.readGitIgnoreGlobs(rootPath)
	}
	return workspaceGraphFileAllowed(filepath.ToSlash(relative), includes, excludes, gitIgnoreGlobs)
}

func workspacePathIsCacheDirectoryDescendant(path string, roots []workspaceRoot, configuredCacheDirectory string) bool {
	configuredCacheDirectory = strings.TrimSpace(configuredCacheDirectory)
	if configuredCacheDirectory == "" {
		return false
	}
	cleanPath := filepath.Clean(path)
	for _, workspaceRoot := range roots {
		rootPath := filepath.Clean(workspaceRoot.Path)
		if rootPath == "" || rootPath == "." {
			continue
		}
		cachePath := configuredCacheDirectory
		if !filepath.IsAbs(cachePath) {
			cachePath = filepath.Join(rootPath, cachePath)
		}
		cachePath = filepath.Clean(cachePath)
		if cachePath == rootPath || !pathWithinRoot(rootPath, cachePath) {
			continue
		}
		if pathWithinRoot(cachePath, cleanPath) {
			return true
		}
	}
	return false
}

func (s *Server) readWorkspaceTextFile(path string) (string, error) {
	return s.readWorkspaceTextFileContext(context.Background(), path)
}

func (s *Server) readWorkspaceTextFileContext(ctx context.Context, path string) (string, error) {
	s.mu.Lock()
	limiter := s.includeReadLimiter
	legacyEncoding := s.settings.LegacyEncoding
	s.mu.Unlock()
	return s.readWorkspaceTextFileCached(ctx, path, legacyEncoding, limiter, true)
}

func (s *Server) readChangedWorkspaceTextFile(path string) (string, error) {
	return s.readChangedWorkspaceTextFileContext(context.Background(), path)
}

func (s *Server) readChangedWorkspaceTextFileContext(ctx context.Context, path string) (string, error) {
	s.mu.Lock()
	limiter := s.includeReadLimiter
	legacyEncoding := s.settings.LegacyEncoding
	s.mu.Unlock()
	return s.readWorkspaceTextFileCached(ctx, path, legacyEncoding, limiter, false)
}

func (s *Server) didChangeWatchedFiles(params didChangeWatchedFilesParams) error {
	type referenceDocumentChange struct {
		path              string
		previous, current *core.TextDocument
		previousParsed    *core.ParsedDocument
	}
	changedPaths := map[string]struct{}{}
	changedWorkspaceDocuments := map[string]*core.TextDocument{}
	changedParsedDocuments := map[string]*core.ParsedDocument{}
	publishedDocumentChanges := map[string]bool{}
	referenceDocumentChanges := make([]referenceDocumentChange, 0, len(params.Changes))
	deletedWorkspacePaths := map[string]struct{}{}
	deletedReferencePaths := map[string]string{}
	workspaceContentChanged := false
	javascriptProjectChanged := false
	javascriptProjectConfigChanged := false
	javascriptProjectChanges := make([]fileEvent, 0, len(params.Changes))
	includeResolutionStructureChanged := false
	gitIgnoreChanged := false
	changedPublicBoundary := map[string]bool{}
	graphArtifactChanged := false
	for _, change := range params.Changes {
		path := fileURIPath(change.URI)
		if path == "" {
			continue
		}
		cleanPath := filepath.Clean(path)
		isASP := isWorkspaceASPFile(cleanPath)
		isJavaScript := isJavaScriptProjectFile(cleanPath)
		directlyReferenced := s.workspacePathDirectlyReferenced(cleanPath)
		automaticIndexEligible := false
		if isASP {
			automaticIndexEligible = s.workspaceFileEligibleForAutomaticIndex(cleanPath)
		}
		gitIgnoreEvent := strings.EqualFold(filepath.Base(cleanPath), ".gitignore") && s.workspaceGitIgnoreEventRelevant(cleanPath)
		includeRelevant := directlyReferenced
		if !isASP && !isJavaScript && !includeRelevant && change.Type != fileChangeChanged && s.workspacePathCanChangeIncludeMembership(cleanPath) {
			includeRelevant = s.workspacePathRelevantForIncludeResolution(cleanPath)
		}
		javascriptRelevant := isJavaScript && s.workspacePathRelevantForJavaScript(cleanPath)
		if !isASP && !isJavaScript && !includeRelevant && !gitIgnoreEvent {
			continue
		}
		if isJavaScript && !javascriptRelevant && !includeRelevant && !gitIgnoreEvent {
			continue
		}
		if isASP && !automaticIndexEligible && !includeRelevant && !gitIgnoreEvent {
			continue
		}
		s.invalidateFsPath(cleanPath)
		s.invalidateSourceSnapshot(cleanPath)
		changedPaths[cleanPath] = struct{}{}
		if gitIgnoreEvent {
			gitIgnoreChanged = true
			invalidateJavaScriptProjectDiscoverySettings(s)
		}
		if includeRelevant && (!isASP || !automaticIndexEligible) {
			includeResolutionStructureChanged = true
		}
		if isJavaScript {
			if javascriptRelevant {
				javascriptProjectChanged = true
				javascriptProjectChanges = append(javascriptProjectChanges, change)
				if isJavaScriptProjectConfigFile(cleanPath) {
					javascriptProjectConfigChanged = true
				}
			}
			continue
		}
		if !isASP {
			continue
		}
		if change.Type != fileChangeDeleted && !automaticIndexEligible {
			uri := filePathURI(cleanPath)
			s.mu.Lock()
			openDoc := s.openDocumentByURILocked(uri)
			if openDoc == nil {
				for candidateURI := range s.workspace {
					if workspacepkg.SameFileIdentityURI(candidateURI, uri) {
						delete(s.workspace, candidateURI)
					}
				}
				s.deleteParsedCacheForURILocked(uri)
			}
			s.mu.Unlock()
			if openDoc == nil {
				continue
			}
		}
		workspaceContentChanged = true
		uri := filePathURI(cleanPath)
		s.mu.Lock()
		openDoc := s.openDocumentByURILocked(uri)
		oldDoc := s.workspace[uri]
		oldText := ""
		oldTextKnown := false
		if oldDoc != nil {
			oldText = oldDoc.Text
			oldTextKnown = true
		}
		s.mu.Unlock()
		var oldParsed *core.ParsedDocument
		if oldDoc != nil {
			oldParsed = s.parseTextDocument(oldDoc, s.settings.DefaultLanguage)
			if !s.documentChangeAnalysisPending(oldDoc.URI, oldDoc.Version) {
				s.applyWorkspaceDocumentRevision(oldDoc, oldParsed)
			}
		}
		if change.Type == fileChangeDeleted {
			changedPublicBoundary[cleanPath] = true
			deletedWorkspacePaths[cleanPath] = struct{}{}
			s.mu.Lock()
			for candidateURI := range s.workspace {
				if workspacepkg.SameFileIdentityURI(candidateURI, uri) {
					delete(s.workspace, candidateURI)
				}
			}
			s.deleteParsedCacheForURILocked(uri)
			s.mu.Unlock()
			if openDoc == nil {
				deletedReferencePaths[cleanPath] = uri
			}
			continue
		}
		if openDoc != nil {
			content, err := s.readChangedWorkspaceTextFile(cleanPath)
			if err == nil {
				changedPublicBoundary[cleanPath] = !oldTextKnown || includePublicBoundaryFingerprint(oldText) != includePublicBoundaryFingerprint(content)
				diskDoc := core.NewTextDocument(uri, "classic-asp", 0, content)
				s.mu.Lock()
				s.workspace[uri] = diskDoc
				s.mu.Unlock()
			} else {
				changedPublicBoundary[cleanPath] = true
				s.mu.Lock()
				for candidateURI := range s.workspace {
					if workspacepkg.SameFileIdentityURI(candidateURI, uri) {
						delete(s.workspace, candidateURI)
					}
				}
				s.mu.Unlock()
			}
			changedWorkspaceDocuments[cleanPath] = openDoc
			continue
		}
		content, err := s.readChangedWorkspaceTextFile(cleanPath)
		if err != nil {
			continue
		}
		changedPublicBoundary[cleanPath] = !oldTextKnown || includePublicBoundaryFingerprint(oldText) != includePublicBoundaryFingerprint(content)
		doc := core.NewTextDocument(uri, "classic-asp", 0, content)
		changedWorkspaceDocuments[cleanPath] = doc
		referenceDocumentChanges = append(referenceDocumentChanges, referenceDocumentChange{path: cleanPath, previous: oldDoc, current: doc, previousParsed: oldParsed})
		s.mu.Lock()
		s.workspace[uri] = doc
		s.deleteParsedCacheForURILocked(uri)
		s.mu.Unlock()
	}
	s.updateJavaScriptProjectIdentityForWatchedFiles(javascriptProjectChanges)
	if len(changedPaths) == 0 {
		return nil
	}
	affectedPaths, includeGraphReady := s.includeInvalidationPaths(changedPaths)
	s.mu.Lock()
	includeGraphComplete := s.workspaceIncludeGraph != nil && s.workspaceIncludeGraphComplete
	s.mu.Unlock()
	preciseReferenceTopology := true
	if len(deletedReferencePaths) > 0 {
		s.mu.Lock()
		preciseReferenceTopology = s.workspaceIncludeGraph != nil && s.workspaceIncludeGraphComplete
		s.mu.Unlock()
	}
	for path := range deletedWorkspacePaths {
		s.mu.Lock()
		if s.workspaceIncludeGraph != nil {
			if _, removeActiveRevision := deletedReferencePaths[path]; removeActiveRevision {
				affected, changed := s.workspaceIncludeGraph.ApplyDeleteDelta(path)
				if changed {
					s.workspaceIncludeGraphRevision++
					if s.workspaceIncludeGraphComplete {
						s.invalidateWorkspaceReferenceFamilyLocked(affected.IdentityMembership())
					} else {
						preciseReferenceTopology = false
					}
				}
			}
		}
		s.mu.Unlock()
	}
	for _, uri := range deletedReferencePaths {
		revision := s.removeWorkspaceDocumentRevision(uri)
		graphArtifactChanged = graphArtifactChanged || revision.Manifest != nil
	}
	if len(deletedWorkspacePaths) > 0 {
		s.scheduleWorkspaceIncludeGraphPersistence(s.workspaceDiskSettingsKey())
	}
	for path, doc := range changedWorkspaceDocuments {
		if _, deleted := deletedWorkspacePaths[path]; deleted || doc == nil {
			continue
		}
		if s.documentChangeAnalysisPending(doc.URI, doc.Version) {
			continue
		}
		parsed := s.parseTextDocument(doc, s.settings.DefaultLanguage)
		if parsed != nil {
			changedParsedDocuments[path] = parsed
			revision := s.applyWorkspaceDocumentRevision(doc, parsed)
			if revision.Manifest != nil && !revision.Duplicate && !revision.Stale {
				publishedDocumentChanges[path] = true
				changedPublicBoundary[path] = revision.Delta.IncludeEdgesChanged || len(revision.Delta.ChangedPublicNames) > 0 || len(revision.Delta.ChangedImplicitNames) > 0 || len(revision.Delta.ChangedObjectTagNames) > 0
				graphArtifactChanged = graphArtifactChanged || workspaceArtifactDeltaChangesGraph(revision.Delta)
			}
		}
	}
	if graphArtifactChanged || len(deletedWorkspacePaths) > 0 || includeResolutionStructureChanged {
		s.invalidateGraphBackground()
	}
	s.mu.Lock()
	if graphArtifactChanged || len(deletedWorkspacePaths) > 0 || includeResolutionStructureChanged {
		s.graphCache = map[string]graph.Payload{}
	}
	if javascriptProjectChanged {
		if javascriptProjectConfigChanged {
			s.resetJavaScriptProjectLocked()
		} else {
			s.javascriptPreparation = nil
			s.markJavaScriptDocumentsChangedLocked()
		}
	}
	s.mu.Unlock()
	if gitIgnoreChanged {
		s.mu.Lock()
		respectGitIgnore := s.settings.WorkspaceRespectGitIgnore
		s.mu.Unlock()
		if respectGitIgnore {
			s.cancelWorkspaceIndexWorker()
			s.workspaceIndexStateMu.Lock()
			s.clearRuntimeDiskCache()
			s.workspaceIndexStateMu.Unlock()
			s.clearWorkspaceDiagnosticsCaches(true)
		}
	}
	if workspaceContentChanged || includeResolutionStructureChanged {
		diagnosticPaths := make(map[string]struct{}, len(changedPaths))
		for path := range changedPaths {
			if isWorkspaceASPFile(path) {
				diagnosticPaths[path] = struct{}{}
			}
		}
		for _, boundaryChanged := range changedPublicBoundary {
			if !boundaryChanged {
				continue
			}
			for dependent := range affectedPaths {
				diagnosticPaths[dependent] = struct{}{}
			}
		}
		if includeResolutionStructureChanged {
			for path := range affectedPaths {
				diagnosticPaths[path] = struct{}{}
			}
			s.mu.Lock()
			for uri := range s.workspace {
				if path := fileURIPath(uri); path != "" {
					diagnosticPaths[filepath.Clean(path)] = struct{}{}
				}
			}
			s.mu.Unlock()
		}
		if !includeGraphComplete {
			for _, uri := range s.openDocumentURIs() {
				if path := fileURIPath(uri); path != "" {
					diagnosticPaths[filepath.Clean(path)] = struct{}{}
				}
			}
			s.mu.Lock()
			for uri := range s.workspace {
				if path := fileURIPath(uri); path != "" {
					diagnosticPaths[filepath.Clean(path)] = struct{}{}
				}
			}
			s.mu.Unlock()
		}
		s.invalidateWorkspaceDiagnosticsPaths(diagnosticPaths)
	}
	if javascriptProjectChanged {
		s.logDebugSummary("[asp-lsp] invalidation.jsProject")
		for _, uri := range s.openDocumentURIs() {
			_, parsed := s.parsed(uri)
			if parsedHasJavaScript(parsed) {
				if err := s.publishDiagnostics(uri); err != nil {
					return err
				}
			}
		}
	}
	if includeResolutionStructureChanged || !preciseReferenceTopology {
		s.clearWorkspaceReferenceCache()
	} else {
		for _, change := range referenceDocumentChanges {
			if publishedDocumentChanges[change.path] {
				continue
			}
			var previous, current *core.ParsedDocument
			previous = change.previousParsed
			current = changedParsedDocuments[change.path]
			if current == nil && change.current != nil {
				current = s.parseTextDocument(change.current, s.settings.DefaultLanguage)
			}
			s.invalidateWorkspaceReferencesForParsedChange(previous, current)
		}
	}
	affected := s.openDocumentURIsForPaths(affectedPaths)
	if !includeGraphReady && workspaceContentChanged {
		affected = s.openDocumentURIs()
	}
	if includeResolutionStructureChanged {
		affected = s.openDocumentURIs()
	}
	sort.Strings(affected)
	publicBoundaryChanged := false
	for _, changed := range changedPublicBoundary {
		if changed {
			publicBoundaryChanged = true
			break
		}
	}
	if workspaceContentChanged && len(affected) > 0 {
		if publicBoundaryChanged {
			s.logDebugSummary("[asp-lsp] invalidation.includePublicBoundary")
		} else {
			s.logDebugSummary("[asp-lsp] include.publicBoundary.reuse")
		}
	}
	for _, uri := range affected {
		if !publicBoundaryChanged && includeGraphReady {
			if _, directlyChanged := changedPaths[filepath.Clean(fileURIPath(uri))]; !directlyChanged {
				continue
			}
		}
		if err := s.publishDiagnostics(uri); err != nil {
			return err
		}
	}
	return nil
}

func (s *Server) didChangeWorkspaceFolders(params didChangeWorkspaceFoldersParams) {
	s.mu.Lock()
	removed := make(map[string]struct{}, len(params.Event.Removed))
	for _, folder := range params.Event.Removed {
		removed[workspacepkg.FileIdentityKeyFromURI(folder.URI)] = struct{}{}
	}
	currentRoots := append([]workspaceRoot(nil), s.workspaceRoots...)
	if len(currentRoots) == 0 && s.rootPath != "" {
		currentRoots = []workspaceRoot{{URI: s.rootURI, Path: s.rootPath}}
	}
	roots := make([]workspaceRoot, 0, len(currentRoots)+len(params.Event.Added))
	seen := map[string]struct{}{}
	for _, root := range currentRoots {
		key := workspaceRootIdentityKey(root)
		if _, ok := removed[key]; ok {
			continue
		}
		seen[key] = struct{}{}
		roots = append(roots, root)
	}
	for _, folder := range params.Event.Added {
		path := fileURIPath(folder.URI)
		if path == "" {
			continue
		}
		root := workspaceRoot{URI: filePathURI(path), Path: filepath.Clean(path)}
		key := workspaceRootIdentityKey(root)
		if _, ok := seen[key]; ok {
			continue
		}
		seen[key] = struct{}{}
		roots = append(roots, root)
	}
	if workspaceRootSetsEqual(currentRoots, roots) {
		s.mu.Unlock()
		return
	}
	s.workspaceRoots = roots
	if len(roots) > 0 {
		s.rootURI = roots[0].URI
		s.rootPath = roots[0].Path
	} else {
		s.rootURI = ""
		s.rootPath = ""
	}
	s.resetTrustedFilesystemRootCacheLocked()
	s.graphCache = map[string]graph.Payload{}
	s.workspaceIncludeGraph = workspacepkg.NewWorkspaceIncludeGraph()
	s.workspaceIncludeGraphRevision = 0
	s.workspaceIncludeGraphComplete = false
	s.mu.Unlock()
	s.invalidateGraphBackground()
	s.cancelWorkspaceIndexWorker()
	s.workspaceIndexStateMu.Lock()
	defer s.workspaceIndexStateMu.Unlock()
	s.configureDiskAnalysisCache()
	s.configureFsGateway()
	s.clearAnalysisCache()
	s.clearSemanticTokenCache()
	s.clearWorkspaceDiagnosticsCaches(true)
	s.clearWorkspaceReferenceCache()
	s.mu.Lock()
	s.resetJavaScriptProjectLocked()
	s.mu.Unlock()
	s.scheduleWorkspaceIndex("workspaceFolders.changed")
	s.logDebugSummary("[asp-lsp] workspaceFolders.changed")
}

func workspaceRootSetsEqual(left, right []workspaceRoot) bool {
	if len(left) != len(right) {
		return false
	}
	leftKeys := make(map[string]struct{}, len(left))
	rightKeys := make(map[string]struct{}, len(right))
	for _, root := range left {
		leftKeys[workspaceRootIdentityKey(root)] = struct{}{}
	}
	for _, root := range right {
		rightKeys[workspaceRootIdentityKey(root)] = struct{}{}
	}
	if len(leftKeys) != len(rightKeys) {
		return false
	}
	for key := range leftKeys {
		if _, ok := rightKeys[key]; !ok {
			return false
		}
	}
	return true
}

func workspaceRootIdentityKey(root workspaceRoot) string {
	key := workspacepkg.FileIdentityKeyFromURI(root.URI)
	if key == "" && root.Path != "" {
		key = workspacepkg.FileIdentityKeyFromURI(filePathURI(filepath.Clean(root.Path)))
	}
	return key
}

func (s *Server) didRenameFiles(params didRenameFilesParams) error {
	changes := didChangeWatchedFilesParams{Changes: make([]fileEvent, 0, len(params.Files)*2)}
	paths := make([]string, 0, len(params.Files)*2)
	for _, file := range params.Files {
		paths = append(paths, file.OldURI, file.NewURI)
		changes.Changes = append(changes.Changes,
			fileEvent{URI: file.OldURI, Type: fileChangeDeleted},
			fileEvent{URI: file.NewURI, Type: fileChangeCreated},
		)
	}
	if err := s.didChangeWatchedFiles(changes); err != nil {
		return err
	}
	if !s.workspaceGitIgnoreChangeWillReindex(paths) && s.workspacePathsNeedFullIndex(paths) {
		s.scheduleWorkspaceIndex("workspace.files.renamed")
	}
	return nil
}

func (s *Server) didFileOperations(params didFileOperationParams, changeType int) error {
	changes := didChangeWatchedFilesParams{Changes: make([]fileEvent, 0, len(params.Files))}
	paths := make([]string, 0, len(params.Files))
	for _, file := range params.Files {
		paths = append(paths, file.URI)
		changes.Changes = append(changes.Changes, fileEvent{URI: file.URI, Type: changeType})
	}
	if err := s.didChangeWatchedFiles(changes); err != nil {
		return err
	}
	if !s.workspaceGitIgnoreChangeWillReindex(paths) && s.workspacePathsNeedFullIndex(paths) {
		s.scheduleWorkspaceIndex("workspace.files.changed")
	}
	return nil
}

func (s *Server) workspacePathsNeedFullIndex(uris []string) bool {
	for _, uri := range uris {
		path := fileURIPath(uri)
		if path == "" {
			continue
		}
		cleanPath := filepath.Clean(path)
		if isWorkspaceASPFile(cleanPath) || isJavaScriptProjectFile(cleanPath) || strings.EqualFold(filepath.Base(cleanPath), ".gitignore") {
			continue
		}
		if !workspaceOperationPathMayBeDirectory(cleanPath) || !s.workspacePathRelevantForWorkspaceIndex(cleanPath) {
			continue
		}
		return true
	}
	return false
}

func workspaceOperationPathMayBeDirectory(path string) bool {
	if info, err := os.Stat(path); err == nil {
		return info.IsDir()
	}
	return filepath.Ext(filepath.Base(path)) == ""
}

func (s *Server) workspaceGitIgnoreChangeWillReindex(uris []string) bool {
	s.mu.Lock()
	respectGitIgnore := s.settings.WorkspaceRespectGitIgnore
	s.mu.Unlock()
	if !respectGitIgnore {
		return false
	}
	for _, uri := range uris {
		path := fileURIPath(uri)
		if path != "" && strings.EqualFold(filepath.Base(filepath.Clean(path)), ".gitignore") && s.workspaceGitIgnoreEventRelevant(filepath.Clean(path)) {
			return true
		}
	}
	return false
}

func (s *Server) workspacePathRelevantForWorkspaceIndex(path string) bool {
	return s.workspacePathAllowedForEvent(path, false, true)
}

func (s *Server) workspacePathRelevantForJavaScript(path string) bool {
	return s.workspacePathAllowedForEvent(path, false, true)
}

func (s *Server) workspacePathRelevantForIncludeResolution(path string) bool {
	if s.workspacePathAllowedForEvent(path, true, true) {
		return true
	}
	return s.workspacePathDirectlyReferenced(path)
}

func (s *Server) workspacePathCanChangeIncludeMembership(path string) bool {
	return workspaceOperationPathMayBeDirectory(filepath.Clean(path)) || s.workspacePathDirectlyReferenced(path)
}

func (s *Server) workspacePathDirectlyReferenced(path string) bool {
	cleanPath := filepath.Clean(path)
	s.mu.Lock()
	includeGraph := s.workspaceIncludeGraph
	s.mu.Unlock()
	if includeGraph == nil {
		return false
	}
	return len(includeGraph.DependentFileNamesForTargets([]string{cleanPath}, false)) > 0
}

func (s *Server) workspaceGitIgnoreEventRelevant(path string) bool {
	cleanPath := filepath.Clean(path)
	root, _, ok := s.workspaceEventPathInfo(cleanPath, false)
	if !ok || workspaceWalkPathIsIgnored(root, cleanPath) {
		return false
	}
	s.mu.Lock()
	respectGitIgnore := s.settings.WorkspaceRespectGitIgnore
	cacheDirectory := strings.TrimSpace(s.settings.CacheDirectory)
	s.mu.Unlock()
	return respectGitIgnore && !workspacePathIsCacheDirectoryDescendant(cleanPath, []workspaceRoot{{Path: root}}, cacheDirectory)
}

func (s *Server) workspacePathAllowedForEvent(path string, includeAuxiliaryBoundaries, applyGitIgnore bool) bool {
	cleanPath := filepath.Clean(path)
	root, relative, ok := s.workspaceEventPathInfo(cleanPath, includeAuxiliaryBoundaries)
	if !ok || workspaceWalkPathIsIgnored(root, cleanPath) {
		return false
	}
	s.mu.Lock()
	excludes := append([]string(nil), s.settings.WorkspaceExcludeGlobs...)
	respectGitIgnore := s.settings.WorkspaceRespectGitIgnore
	cacheDirectory := strings.TrimSpace(s.settings.CacheDirectory)
	s.mu.Unlock()
	if workspacePathIsCacheDirectoryDescendant(cleanPath, []workspaceRoot{{Path: root}}, cacheDirectory) {
		return false
	}
	if !workspaceGraphFileAllowed(relative, nil, excludes, nil) {
		return false
	}
	gitIgnoreGlobs := []string(nil)
	if applyGitIgnore && respectGitIgnore {
		gitIgnoreGlobs = s.readGitIgnoreGlobs(root)
	}
	return workspaceGraphFileAllowed(relative, nil, excludes, gitIgnoreGlobs)
}

func (s *Server) workspaceEventPathInfo(path string, includeAuxiliaryBoundaries bool) (root, relative string, ok bool) {
	cleanPath := filepath.Clean(path)
	s.mu.Lock()
	roots := append([]workspaceRoot(nil), s.workspaceRoots...)
	if len(roots) == 0 && s.rootPath != "" {
		roots = []workspaceRoot{{URI: s.rootURI, Path: s.rootPath}}
	}
	if includeAuxiliaryBoundaries {
		for _, includePath := range s.settings.IncludePaths {
			if includePath != "" {
				roots = append(roots, workspaceRoot{Path: includePath})
			}
		}
		for _, virtualRoot := range s.settings.VirtualRoots {
			if virtualRoot != "" {
				roots = append(roots, workspaceRoot{Path: virtualRoot})
			}
		}
		if s.settings.VirtualRoot != "" {
			roots = append(roots, workspaceRoot{Path: s.settings.VirtualRoot})
		}
	}
	s.mu.Unlock()
	root = workspaceRootPathForPath(cleanPath, roots)
	if root == "" || !workspacePathWithinBoundary(&s.trustedPaths, cleanPath, root) {
		return "", "", false
	}
	relativePath, err := filepath.Rel(root, cleanPath)
	if err != nil {
		return "", "", false
	}
	return root, filepath.ToSlash(relativePath), true
}

func (s *Server) openDocumentURIs() []string {
	s.mu.Lock()
	defer s.mu.Unlock()
	uris := make([]string, 0, len(s.documents))
	for uri := range s.documents {
		uris = append(uris, uri)
	}
	sort.Strings(uris)
	return uris
}

func (s *Server) includeInvalidationPaths(changedPaths map[string]struct{}) (map[string]struct{}, bool) {
	affected := make(map[string]struct{}, len(changedPaths))
	targets := make([]string, 0, len(changedPaths))
	for path := range changedPaths {
		cleaned := filepath.Clean(path)
		affected[cleaned] = struct{}{}
		targets = append(targets, cleaned)
	}
	sort.Strings(targets)
	s.mu.Lock()
	graph := s.workspaceIncludeGraph
	graphReady := graph != nil && graph.Size() > 0
	if graphReady {
		for _, dependent := range graph.DependentFileNamesForTargets(targets, true) {
			affected[filepath.Clean(dependent)] = struct{}{}
		}
	}
	s.mu.Unlock()
	return affected, graphReady
}

func (s *Server) openDocumentURIsForPaths(paths map[string]struct{}) []string {
	s.mu.Lock()
	defer s.mu.Unlock()
	result := make([]string, 0, len(paths))
	for _, doc := range s.documents {
		if doc == nil {
			continue
		}
		if _, ok := paths[filepath.Clean(fileURIPath(doc.URI))]; ok {
			result = append(result, doc.URI)
		}
	}
	sort.Strings(result)
	return result
}

type workspaceFile struct {
	Path     string
	URI      string
	Relative string
}

func scanWorkspaceFiles(rootPath string) []workspaceFile {
	return scanWorkspaceFilesWithChunk(rootPath, 0)
}

func scanWorkspaceFilesWithChunk(rootPath string, chunkSize int) []workspaceFile {
	return scanWorkspaceFilesWithContextProgress(context.Background(), rootPath, chunkSize, nil)
}

func (s *Server) workspaceSourcePathAllowed(path string) bool {
	s.mu.Lock()
	roots := append([]workspaceRoot(nil), s.workspaceRoots...)
	if len(roots) == 0 && s.rootPath != "" {
		roots = []workspaceRoot{{URI: s.rootURI, Path: s.rootPath}}
	}
	for _, root := range s.openDocumentBoundaryRootsLocked() {
		roots = append(roots, workspaceRoot{Path: root})
	}
	includePaths := append([]string(nil), s.settings.IncludePaths...)
	virtualRoots := append([]string(nil), s.settings.VirtualRoots...)
	if s.settings.VirtualRoot != "" {
		virtualRoots = append(virtualRoots, s.settings.VirtualRoot)
	}
	if len(roots) == 0 && len(includePaths) == 0 && len(virtualRoots) == 0 {
		for uri, document := range s.documents {
			if document == nil {
				continue
			}
			if documentPath := fileURIPath(uri); documentPath != "" {
				roots = append(roots, workspaceRoot{Path: filepath.Dir(filepath.Clean(documentPath))})
			}
		}
		for uri, document := range s.workspace {
			if document == nil {
				continue
			}
			if documentPath := fileURIPath(uri); documentPath != "" {
				roots = append(roots, workspaceRoot{Path: filepath.Dir(filepath.Clean(documentPath))})
			}
		}
	}
	s.mu.Unlock()
	if len(roots) == 0 && len(includePaths) == 0 && len(virtualRoots) == 0 {
		return false
	}
	return workspacePathWithinAnyBoundary(&s.trustedPaths, path, roots, includePaths, virtualRoots)
}

func workspacePathWithinAnyBoundary(cache *trustedPathCache, path string, roots []workspaceRoot, includePaths, virtualRoots []string) bool {
	cleanPath := filepath.Clean(path)
	for _, root := range roots {
		if workspacePathWithinBoundary(cache, cleanPath, root.Path) {
			return true
		}
	}
	for _, root := range append(append([]string(nil), includePaths...), virtualRoots...) {
		if workspacePathWithinBoundary(cache, cleanPath, root) {
			return true
		}
	}
	return false
}

func workspacePathWithinBoundary(cache *trustedPathCache, path, root string) bool {
	if root == "" || !pathWithinRoot(root, path) {
		return false
	}
	return !cache.pathContainsSymlinkWithinRoot(path, root)
}

func (s *Server) openDocumentBoundaryRootsLocked() []string {
	roots := make([]string, 0, len(s.documents))
	for _, document := range s.documents {
		if document == nil {
			continue
		}
		path := fileURIPath(document.URI)
		if path == "" {
			continue
		}
		roots = append(roots, filepath.Dir(filepath.Clean(path)))
	}
	return roots
}
