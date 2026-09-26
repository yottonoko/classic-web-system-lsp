package lspserver

import (
	"context"
	"errors"
	"path/filepath"
	"sort"
	"sync/atomic"

	"github.com/yottonoko/classic-web-system-lsp/internal/core"
	workspacepkg "github.com/yottonoko/classic-web-system-lsp/internal/workspace"
)

var (
	errGraphCollectionGeneration       = errors.New("graph document collection generation changed")
	errGraphCollectionIncludeSync      = errors.New("graph document collection include graph sync failed")
	errGraphCollectionIncludeExpansion = errors.New("graph document collection include expansion incomplete")
)

type graphDocumentCollectionResult struct {
	documents  []*core.ParsedDocument
	rootURI    string
	generation uint64
	complete   bool
	err        error
}

// incomingIncludeDocumentsResult is the transactional result of traversing
// the reverse include graph. The document slice is only usable when complete
// is true; callers must not publish a partial prefix.
type incomingIncludeDocumentsResult struct {
	documents  []*core.ParsedDocument
	generation uint64
	complete   bool
	err        error
}

func completeIncomingIncludeDocuments(documents []*core.ParsedDocument, generation uint64) incomingIncludeDocumentsResult {
	return incomingIncludeDocumentsResult{documents: documents, generation: generation, complete: true}
}

func incompleteIncomingIncludeDocuments(generation uint64, err error) incomingIncludeDocumentsResult {
	if err == nil {
		err = errGraphCollectionGeneration
	}
	return incomingIncludeDocumentsResult{generation: generation, err: err}
}

func completeGraphDocumentCollection(documents []*core.ParsedDocument, rootURI string, generation uint64) graphDocumentCollectionResult {
	return graphDocumentCollectionResult{
		documents: documents, rootURI: rootURI, generation: generation, complete: true,
	}
}

func incompleteGraphDocumentCollection(rootURI string, generation uint64, err error) graphDocumentCollectionResult {
	if err == nil {
		err = errGraphCollectionGeneration
	}
	return graphDocumentCollectionResult{rootURI: rootURI, generation: generation, err: err}
}

func (s *Server) workspaceGraphDocuments() ([]*core.ParsedDocument, string) {
	return s.workspaceGraphDocumentsContext(context.Background())
}

func (s *Server) workspaceGraphSourceCount() int {
	s.mu.Lock()
	identities := make(map[string]struct{}, len(s.workspace)+len(s.documents))
	for uri := range s.workspace {
		identities[workspacepkg.FileIdentityKeyFromURI(uri)] = struct{}{}
	}
	for uri := range s.documents {
		identities[workspacepkg.FileIdentityKeyFromURI(uri)] = struct{}{}
	}
	s.mu.Unlock()
	return len(identities)
}

func (s *Server) workspaceGraphDocumentsContext(ctx context.Context) ([]*core.ParsedDocument, string) {
	return s.workspaceGraphDocumentsContextWithRestore(ctx, true)
}

func (s *Server) workspaceGraphDocumentsContextWithRestore(ctx context.Context, restoreIncludeGraph bool) ([]*core.ParsedDocument, string) {
	return s.workspaceGraphDocumentsContextWithProgress(ctx, restoreIncludeGraph, nil)
}

func (s *Server) workspaceGraphDocumentsContextWithProgress(ctx context.Context, restoreIncludeGraph bool, report graphProgressReporter) ([]*core.ParsedDocument, string) {
	result := s.workspaceGraphDocumentsContextWithProgressResult(ctx, restoreIncludeGraph, report)
	if !result.complete {
		return nil, result.rootURI
	}
	return result.documents, result.rootURI
}

func (s *Server) workspaceGraphDocumentsContextWithProgressResult(ctx context.Context, restoreIncludeGraph bool, report graphProgressReporter) graphDocumentCollectionResult {
	if ctx == nil {
		ctx = context.Background()
	}
	s.mu.Lock()
	rootURI := s.rootURI
	generation := s.graphGeneration
	documents := make(map[string]graphDocumentSource, len(s.workspace)+len(s.documents))
	for uri, doc := range s.workspace {
		if doc == nil {
			continue
		}
		key := workspacepkg.FileIdentityKeyFromURI(uri)
		documents[key] = graphDocumentSource{URI: canonicalGraphURI(doc.URI), Text: doc.Text}
	}
	for uri, doc := range s.documents {
		if doc == nil {
			continue
		}
		key := workspacepkg.FileIdentityKeyFromURI(uri)
		source := documents[key]
		if source.URI == "" {
			source.URI = canonicalGraphURI(doc.URI)
		}
		source.Text = doc.Text
		documents[key] = source
	}
	defaultLanguage := s.settings.DefaultLanguage
	s.mu.Unlock()

	keys := make([]string, 0, len(documents))
	for key := range documents {
		keys = append(keys, key)
	}
	sort.Strings(keys)
	parsed := make([]*core.ParsedDocument, len(keys))
	var completed atomic.Int64
	parse := func(index int) {
		if ctx.Err() != nil {
			return
		}
		key := keys[index]
		source := documents[key]
		if report != nil {
			defer func() {
				current := int(completed.Add(1))
				report("", progressDetailForURI(source.URI), current, len(keys))
			}()
		}
		if source.URI == "" {
			return
		}
		parsed[index] = s.parseText(source.URI, source.Text, defaultLanguage)
	}
	if shouldParallelParseGraphSources(keys, documents) {
		s.analysisWorkers.parallelForBulk(ctx, len(keys), func(workerCtx context.Context, index int) {
			if workerCtx.Err() != nil {
				return
			}
			parse(index)
		})
	} else {
		for index := range keys {
			parse(index)
		}
	}
	filtered := parsed[:0]
	for _, document := range parsed {
		if document != nil {
			filtered = append(filtered, document)
		}
	}
	if ctx.Err() != nil {
		return incompleteGraphDocumentCollection(rootURI, generation, ctx.Err())
	}
	if !s.graphGenerationCurrent(ctx, generation) {
		return incompleteGraphDocumentCollection(rootURI, generation, errGraphCollectionGeneration)
	}
	includeGraphCurrent := false
	if restoreIncludeGraph {
		settingsKey := s.workspaceDiskSettingsKey()
		s.mu.Lock()
		includeGraphCurrent = s.workspaceIncludeGraph != nil && s.workspaceIncludeGraphComplete &&
			s.workspaceIncludeGraph.SettingsKey() == settingsKey
		s.mu.Unlock()
	}
	if !includeGraphCurrent && (!restoreIncludeGraph || !s.restoreWorkspaceIncludeGraphFromDiskIfCurrent(ctx, generation)) {
		if ctx.Err() != nil {
			return incompleteGraphDocumentCollection(rootURI, generation, ctx.Err())
		}
		if !s.graphGenerationCurrent(ctx, generation) {
			return incompleteGraphDocumentCollection(rootURI, generation, errGraphCollectionGeneration)
		}
		if !s.syncWorkspaceIncludeGraphCacheIfCurrent(ctx, generation, filtered) {
			if ctx.Err() != nil {
				return incompleteGraphDocumentCollection(rootURI, generation, ctx.Err())
			}
			if !s.graphGenerationCurrent(ctx, generation) {
				return incompleteGraphDocumentCollection(rootURI, generation, errGraphCollectionGeneration)
			}
			return incompleteGraphDocumentCollection(rootURI, generation, errGraphCollectionIncludeSync)
		}
	}
	if ctx.Err() != nil {
		return incompleteGraphDocumentCollection(rootURI, generation, ctx.Err())
	}
	if !s.graphGenerationCurrent(ctx, generation) {
		return incompleteGraphDocumentCollection(rootURI, generation, errGraphCollectionGeneration)
	}
	return completeGraphDocumentCollection(filtered, rootURI, generation)
}

func (s *Server) workspaceGraphDocumentsContextWithRestoreResult(ctx context.Context, restoreIncludeGraph bool) graphDocumentCollectionResult {
	return s.workspaceGraphDocumentsContextWithProgressResult(ctx, restoreIncludeGraph, nil)
}

func (s *Server) graphGenerationCurrent(ctx context.Context, generation uint64) bool {
	if ctx != nil && ctx.Err() != nil {
		return false
	}
	s.mu.Lock()
	current := s.graphGeneration == generation
	s.mu.Unlock()
	return current
}

func (s *Server) graphGenerationSnapshot() uint64 {
	s.mu.Lock()
	generation := s.graphGeneration
	s.mu.Unlock()
	return generation
}

// waitForDocumentOpenAnalysisContext lets graph consumers begin from the
// latest published document artifact generation. An open-analysis worker can
// otherwise invalidate the graph after collection has started, turning a
// request over an unchanged document into a spurious stale result.
func (s *Server) waitForDocumentOpenAnalysisContext(ctx context.Context) bool {
	if ctx == nil {
		ctx = context.Background()
	}
	for {
		if ctx.Err() != nil {
			return false
		}
		done := s.activeDocumentOpenAnalysisGenerations()
		if len(done) == 0 {
			return ctx.Err() == nil
		}
		for _, generationDone := range done {
			select {
			case <-ctx.Done():
				return false
			case <-generationDone:
			}
		}
	}
}

func (s *Server) activeDocumentOpenAnalysisGenerations() []<-chan struct{} {
	s.mu.Lock()
	defer s.mu.Unlock()
	done := make([]<-chan struct{}, 0, len(s.documentOpenAnalysisJobs))
	for _, job := range s.documentOpenAnalysisJobs {
		if job != nil && (job.active || job.publicationInFlight) && job.generationDone != nil {
			done = append(done, job.generationDone)
		}
	}
	return done
}

type graphDocumentSource struct {
	URI  string
	Text string
}

func shouldParallelParseGraphSources(keys []string, documents map[string]graphDocumentSource) bool {
	if len(keys) < defaultAnalysisWorkers()*2 {
		return false
	}
	totalText := 0
	for _, key := range keys {
		totalText += len(documents[key].Text)
		if totalText >= 1<<20 {
			return true
		}
	}
	return false
}

// workspaceGraphSourceDocumentsContext collects the in-memory workspace
// source inventory without synchronizing the include graph. It is used when
// the include cache is unavailable (for example during shutdown), or when a
// caller intentionally takes the compatibility path without the reverse
// index. Attempting a graph sync in either case would add an unnecessary
// state mutation to an otherwise usable traversal.
func (s *Server) workspaceGraphSourceDocumentsContext(ctx context.Context) graphDocumentCollectionResult {
	if ctx == nil {
		ctx = context.Background()
	}
	s.mu.Lock()
	generation := s.graphGeneration
	rootURI := s.rootURI
	documents := make(map[string]graphDocumentSource, len(s.workspace)+len(s.documents))
	for uri, doc := range s.workspace {
		if doc == nil {
			continue
		}
		key := workspacepkg.FileIdentityKeyFromURI(uri)
		documents[key] = graphDocumentSource{URI: canonicalGraphURI(doc.URI), Text: doc.Text}
	}
	for uri, doc := range s.documents {
		if doc == nil {
			continue
		}
		key := workspacepkg.FileIdentityKeyFromURI(uri)
		source := documents[key]
		if source.URI == "" {
			source.URI = canonicalGraphURI(doc.URI)
		}
		source.Text = doc.Text
		documents[key] = source
	}
	defaultLanguage := s.settings.DefaultLanguage
	s.mu.Unlock()
	keys := make([]string, 0, len(documents))
	for key := range documents {
		keys = append(keys, key)
	}
	sort.Strings(keys)
	parsed := make([]*core.ParsedDocument, len(keys))
	s.analysisWorkers.parallelForBulk(ctx, len(keys), func(workerCtx context.Context, index int) {
		if workerCtx.Err() != nil {
			return
		}
		key := keys[index]
		source := documents[key]
		if source.URI != "" {
			parsed[index] = s.parseText(source.URI, source.Text, defaultLanguage)
		}
	})
	if ctx.Err() != nil {
		return incompleteGraphDocumentCollection(rootURI, generation, ctx.Err())
	}
	if !s.graphGenerationCurrent(ctx, generation) {
		return incompleteGraphDocumentCollection(rootURI, generation, errGraphCollectionGeneration)
	}
	filtered := parsed[:0]
	for _, document := range parsed {
		if document != nil {
			filtered = append(filtered, document)
		}
	}
	return completeGraphDocumentCollection(filtered, rootURI, generation)
}

// workspaceGraphSourceDocumentsContextWithConfiguredRoots extends the
// in-memory source overlay with the current files under every configured
// workspace root. The folder collector owns filesystem filtering and bounded
// reads; this wrapper only merges its complete results without publishing the
// include graph.
func (s *Server) workspaceGraphSourceDocumentsContextWithConfiguredRoots(ctx context.Context, generation uint64) graphDocumentCollectionResult {
	if ctx == nil {
		ctx = context.Background()
	}
	if ctx.Err() != nil {
		return incompleteGraphDocumentCollection("", generation, ctx.Err())
	}
	if !s.graphGenerationCurrent(ctx, generation) {
		return incompleteGraphDocumentCollection("", generation, errGraphCollectionGeneration)
	}

	base := s.workspaceGraphSourceDocumentsContext(ctx)
	if !base.complete {
		return base
	}
	if base.generation != generation || !s.graphGenerationCurrent(ctx, generation) {
		return incompleteGraphDocumentCollection(base.rootURI, generation, errGraphCollectionGeneration)
	}

	s.mu.Lock()
	rootURI := s.rootURI
	roots := append([]workspaceRoot(nil), s.workspaceRoots...)
	if len(roots) == 0 && s.rootPath != "" {
		roots = []workspaceRoot{{URI: s.rootURI, Path: s.rootPath}}
	}
	s.mu.Unlock()
	if rootURI == "" {
		rootURI = base.rootURI
	}

	documentsByIdentity := make(map[string]*core.ParsedDocument, len(base.documents))
	for _, document := range base.documents {
		if document == nil {
			continue
		}
		documentsByIdentity[workspacepkg.FileIdentityKeyFromURI(document.URI)] = document
	}
	for _, root := range roots {
		if ctx.Err() != nil {
			return incompleteGraphDocumentCollection(rootURI, generation, ctx.Err())
		}
		if root.Path == "" {
			continue
		}
		collection := s.navigationFolderDocumentsContextWithProgressResult(ctx, filePathURI(root.Path), nil)
		if !collection.complete {
			return incompleteGraphDocumentCollection(rootURI, generation, collection.err)
		}
		if collection.generation != generation || !s.graphGenerationCurrent(ctx, generation) {
			return incompleteGraphDocumentCollection(rootURI, generation, errGraphCollectionGeneration)
		}
		for _, document := range collection.documents {
			if document == nil {
				continue
			}
			key := workspacepkg.FileIdentityKeyFromURI(document.URI)
			if _, exists := documentsByIdentity[key]; !exists {
				documentsByIdentity[key] = document
			}
		}
	}
	if ctx.Err() != nil {
		return incompleteGraphDocumentCollection(rootURI, generation, ctx.Err())
	}
	if !s.graphGenerationCurrent(ctx, generation) {
		return incompleteGraphDocumentCollection(rootURI, generation, errGraphCollectionGeneration)
	}

	keys := make([]string, 0, len(documentsByIdentity))
	for key := range documentsByIdentity {
		keys = append(keys, key)
	}
	sort.Strings(keys)
	documents := make([]*core.ParsedDocument, 0, len(keys))
	for _, key := range keys {
		documents = append(documents, documentsByIdentity[key])
	}
	return completeGraphDocumentCollection(documents, rootURI, generation)
}

func (s *Server) parsedGraphDocument(uri string) (*core.ParsedDocument, bool) {
	if doc := s.documentByURI(uri); doc != nil {
		return s.parseText(canonicalGraphURI(uri), doc.Text, s.settings.DefaultLanguage), true
	}
	path := fileURIPath(uri)
	if path == "" || !s.graphSourcePathAllowed(path) {
		return nil, false
	}
	content, err := s.readWorkspaceTextFile(path)
	if err != nil {
		return nil, false
	}
	return s.parseText(canonicalGraphURI(uri), content, s.settings.DefaultLanguage), true
}

func (s *Server) graphSourcePathAllowed(path string) bool {
	cleanPath := filepath.Clean(path)
	s.mu.Lock()
	roots := append([]workspaceRoot(nil), s.workspaceRoots...)
	if len(roots) == 0 && s.rootPath != "" {
		roots = []workspaceRoot{{Path: s.rootPath, URI: s.rootURI}}
	}
	includePaths := append([]string(nil), s.settings.IncludePaths...)
	virtualRoots := append([]string(nil), s.settings.VirtualRoots...)
	if s.settings.VirtualRoot != "" {
		virtualRoots = append(virtualRoots, s.settings.VirtualRoot)
	}
	s.mu.Unlock()
	if len(roots) == 0 && len(includePaths) == 0 && len(virtualRoots) == 0 {
		return false
	}
	_, ok := s.trustedFilesystemPath(cleanPath)
	return ok
}

func cleanFileURIPath(uri string) string {
	path := filepath.Clean(fileURIPath(uri))
	if path == "." {
		return ""
	}
	return path
}

func (s *Server) incomingIncludeDocumentsForTargetsContextResult(ctx context.Context, targetPaths map[string]struct{}, excludedURIs map[string]struct{}) incomingIncludeDocumentsResult {
	return s.incomingIncludeDocumentsForTargetsAtGeneration(ctx, s.graphGenerationSnapshot(), targetPaths, excludedURIs)
}

func (s *Server) incomingIncludeDocumentsForTargetsAtGeneration(ctx context.Context, generation uint64, targetPaths map[string]struct{}, excludedURIs map[string]struct{}) incomingIncludeDocumentsResult {
	if ctx == nil {
		ctx = context.Background()
	}
	if ctx.Err() != nil {
		return incompleteIncomingIncludeDocuments(generation, ctx.Err())
	}
	if len(targetPaths) == 0 {
		if !s.graphGenerationCurrent(ctx, generation) {
			return incompleteIncomingIncludeDocuments(generation, errGraphCollectionGeneration)
		}
		return completeIncomingIncludeDocuments(nil, generation)
	}

	s.mu.Lock()
	useReverseIndex := s.settings.GraphUseReverseIncludeIndex
	includeGraph := s.workspaceIncludeGraph
	includeGraphComplete := includeGraph != nil && s.workspaceIncludeGraphComplete
	includeGraphSize := 0
	if includeGraph != nil {
		includeGraphSize = includeGraph.Size()
	}
	diskCacheWritesClosed := s.diskCacheWritesClosed
	includeGraphSyncTestHook := s.workspaceIncludeGraphSyncTestHook
	defaultLanguage := s.settings.DefaultLanguage
	s.mu.Unlock()

	// The compatibility path intentionally scans the live workspace source
	// inventory instead of consulting the reverse index. Keep this collection
	// transactional: a cancelled or stale scan must not publish a partial
	// incoming-owner set or mutate the include graph.
	var fallbackDocuments []*core.ParsedDocument
	if !useReverseIndex {
		collection := s.workspaceGraphSourceDocumentsContextWithConfiguredRoots(ctx, generation)
		if !collection.complete {
			return incompleteIncomingIncludeDocuments(collection.generation, collection.err)
		}
		if collection.generation != generation || !s.graphGenerationCurrent(ctx, generation) {
			return incompleteIncomingIncludeDocuments(generation, errGraphCollectionGeneration)
		}
		fallbackDocuments = collection.documents
	} else if !includeGraphComplete {
		// A reverse graph that merely has entries is not sufficient. Changes can
		// leave those entries as a valid-looking but incomplete prefix, so collect
		// and synchronize the workspace graph through the request context first.
		// If the cache writer is already closed, a graph sync cannot succeed and
		// cannot add any trustworthy reverse-index information. With an empty
		// graph, scan the current source inventory directly so open-document
		// requests can still complete without mutating include-cache state. A
		// nonempty graph remains an incomplete stale prefix and is rejected.
		if diskCacheWritesClosed && includeGraphSize == 0 && includeGraphSyncTestHook == nil {
			collection := s.workspaceGraphSourceDocumentsContextWithConfiguredRoots(ctx, generation)
			if !collection.complete {
				return incompleteIncomingIncludeDocuments(collection.generation, collection.err)
			}
			if collection.generation != generation || !s.graphGenerationCurrent(ctx, generation) {
				return incompleteIncomingIncludeDocuments(generation, errGraphCollectionGeneration)
			}
			fallbackDocuments = collection.documents
			useReverseIndex = false
		} else {
			collection := s.workspaceGraphDocumentsContextWithRestoreResult(ctx, true)
			if !collection.complete {
				return incompleteIncomingIncludeDocuments(collection.generation, collection.err)
			}
			if collection.generation != generation || !s.graphGenerationCurrent(ctx, generation) {
				return incompleteIncomingIncludeDocuments(generation, errGraphCollectionGeneration)
			}
			fallbackDocuments = collection.documents
			s.mu.Lock()
			includeGraph = s.workspaceIncludeGraph
			includeGraphComplete = includeGraph != nil && s.workspaceIncludeGraphComplete
			// The setting may change while the collection is being synchronized.
			useReverseIndex = s.settings.GraphUseReverseIncludeIndex
			s.mu.Unlock()
			if useReverseIndex && !includeGraphComplete {
				return incompleteIncomingIncludeDocuments(generation, errGraphCollectionIncludeSync)
			}
		}
	}
	if ctx.Err() != nil {
		return incompleteIncomingIncludeDocuments(generation, ctx.Err())
	}
	if !s.graphGenerationCurrent(ctx, generation) {
		return incompleteIncomingIncludeDocuments(generation, errGraphCollectionGeneration)
	}

	if useReverseIndex {
		// Copy candidates while holding the server lock. The graph is replaced
		// atomically by include-graph synchronization and is not safe to read
		// concurrently after the lock is released.
		s.mu.Lock()
		includeGraph = s.workspaceIncludeGraph
		if includeGraph == nil || !s.workspaceIncludeGraphComplete {
			s.mu.Unlock()
			return incompleteIncomingIncludeDocuments(generation, errGraphCollectionIncludeSync)
		}
		targets := make([]string, 0, len(targetPaths))
		for targetPath := range targetPaths {
			targets = append(targets, filepath.Clean(targetPath))
		}
		candidates := includeGraph.DependentFileNamesForTargets(targets, false)
		s.mu.Unlock()

		documents := make([]*core.ParsedDocument, 0, len(candidates))
		seen := map[string]struct{}{}
		for _, candidate := range candidates {
			if ctx.Err() != nil {
				return incompleteIncomingIncludeDocuments(generation, ctx.Err())
			}
			if !s.graphGenerationCurrent(ctx, generation) {
				return incompleteIncomingIncludeDocuments(generation, errGraphCollectionGeneration)
			}
			uri := filePathURI(candidate)
			excluded := false
			for excludedURI := range excludedURIs {
				if workspacepkg.SameFileIdentityURI(uri, excludedURI) {
					excluded = true
					break
				}
			}
			if excluded {
				continue
			}
			key := workspacepkg.FileIdentityKeyFromURI(uri)
			if _, alreadySeen := seen[key]; alreadySeen {
				continue
			}
			seen[key] = struct{}{}
			var parsed *core.ParsedDocument
			if doc := s.documentByURI(uri); doc != nil {
				parsed = s.parseTextDocument(doc, defaultLanguage)
			} else {
				content, err := s.readWorkspaceTextFileWithinBoundaries(ctx, candidate, filepath.Dir(filepath.Clean(candidate)))
				if err != nil {
					if ctx.Err() != nil {
						return incompleteIncomingIncludeDocuments(generation, ctx.Err())
					}
					return incompleteIncomingIncludeDocuments(generation, err)
				}
				if ctx.Err() != nil {
					return incompleteIncomingIncludeDocuments(generation, ctx.Err())
				}
				parsed = s.parseText(uri, content, defaultLanguage)
			}
			if parsed != nil {
				documents = append(documents, parsed)
			} else {
				return incompleteIncomingIncludeDocuments(generation, errGraphCollectionIncludeExpansion)
			}
		}
		if ctx.Err() != nil {
			return incompleteIncomingIncludeDocuments(generation, ctx.Err())
		}
		if !s.graphGenerationCurrent(ctx, generation) {
			return incompleteIncomingIncludeDocuments(generation, errGraphCollectionGeneration)
		}
		return completeIncomingIncludeDocuments(documents, generation)
	}

	// Reverse-index use can be disabled, but the workspace collection above is
	// still required to keep this compatibility path context-bound and complete.
	incoming := make([]*core.ParsedDocument, 0)
	seenIncoming := map[string]struct{}{}
	for _, parsed := range fallbackDocuments {
		if ctx.Err() != nil {
			return incompleteIncomingIncludeDocuments(generation, ctx.Err())
		}
		if !s.graphGenerationCurrent(ctx, generation) {
			return incompleteIncomingIncludeDocuments(generation, errGraphCollectionGeneration)
		}
		if parsed == nil {
			continue
		}
		excluded := false
		for excludedURI := range excludedURIs {
			if workspacepkg.SameFileIdentityURI(parsed.URI, excludedURI) {
				excluded = true
				break
			}
		}
		if excluded {
			continue
		}
		parsedKey := workspacepkg.FileIdentityKeyFromURI(parsed.URI)
		if _, alreadySeen := seenIncoming[parsedKey]; alreadySeen {
			continue
		}
		for _, include := range parsed.Includes {
			if ctx.Err() != nil {
				return incompleteIncomingIncludeDocuments(generation, ctx.Err())
			}
			details, ok := s.includeTargetDetailsForModeContext(ctx, parsed.URI, include.Path, include.Mode)
			if !ok {
				if ctx.Err() != nil {
					return incompleteIncomingIncludeDocuments(generation, ctx.Err())
				}
				continue
			}
			if !details.Exists || details.Path == "" {
				continue
			}
			matched := false
			for targetPath := range targetPaths {
				if workspacepkg.SameFileIdentityURI(filePathURI(details.Path), filePathURI(filepath.Clean(targetPath))) {
					matched = true
					break
				}
			}
			if matched {
				incoming = append(incoming, parsed)
				seenIncoming[parsedKey] = struct{}{}
				break
			}
		}
	}
	if ctx.Err() != nil {
		return incompleteIncomingIncludeDocuments(generation, ctx.Err())
	}
	if !s.graphGenerationCurrent(ctx, generation) {
		return incompleteIncomingIncludeDocuments(generation, errGraphCollectionGeneration)
	}
	return completeIncomingIncludeDocuments(incoming, generation)
}

// appendIncomingFolderGraphDocumentsContextResult expands a folder/workspace
// graph only after the reverse include traversal has completed. A partial
// incoming-owner set must never reach graph payload construction.
func (s *Server) appendIncomingFolderGraphDocumentsContextResult(ctx context.Context, generation uint64, documents []*core.ParsedDocument) incomingIncludeDocumentsResult {
	if ctx == nil {
		ctx = context.Background()
	}
	if ctx.Err() != nil {
		return incompleteIncomingIncludeDocuments(generation, ctx.Err())
	}
	if !s.graphGenerationCurrent(ctx, generation) {
		return incompleteIncomingIncludeDocuments(generation, errGraphCollectionGeneration)
	}
	targetPaths := map[string]struct{}{}
	excludedURIs := map[string]struct{}{}
	for _, document := range documents {
		if document == nil {
			continue
		}
		if targetPath := cleanFileURIPath(document.URI); targetPath != "" {
			targetPaths[targetPath] = struct{}{}
		}
		excludedURIs[document.URI] = struct{}{}
	}
	incoming := s.incomingIncludeDocumentsForTargetsAtGeneration(ctx, generation, targetPaths, excludedURIs)
	if !incoming.complete {
		return incoming
	}
	result := append(append([]*core.ParsedDocument(nil), documents...), incoming.documents...)
	if ctx.Err() != nil {
		return incompleteIncomingIncludeDocuments(generation, ctx.Err())
	}
	if !s.graphGenerationCurrent(ctx, generation) {
		return incompleteIncomingIncludeDocuments(generation, errGraphCollectionGeneration)
	}
	return completeIncomingIncludeDocuments(result, generation)
}
