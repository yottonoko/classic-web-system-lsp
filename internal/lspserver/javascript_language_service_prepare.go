package lspserver

import (
	"context"
	"maps"
	"os"
	"path/filepath"
	"sort"
	"strings"

	"github.com/yottonoko/classic-web-system-lsp/internal/core"
	"github.com/yottonoko/classic-web-system-lsp/internal/lsp"
	"github.com/yottonoko/classic-web-system-lsp/internal/tsgoadapter"
	workspacepkg "github.com/yottonoko/classic-web-system-lsp/internal/workspace"
)

func (s *Server) prepareJavaScriptRequest(sourceURI string, position lsp.Position) (*javaScriptRequest, bool) {
	return s.prepareJavaScriptRequestContext(context.Background(), sourceURI, position)
}

func (s *Server) prepareJavaScriptRequestContext(ctx context.Context, sourceURI string, position lsp.Position) (*javaScriptRequest, bool) {
	if ctx == nil {
		ctx = context.Background()
	}
	if ctx.Err() != nil {
		return nil, false
	}
	s.mu.Lock()
	root := s.rootPath
	defaultLanguage := s.settings.DefaultLanguage
	explicitTypes := append([]string(nil), s.settings.JavaScriptCompilerOptionTypes...)
	explicitTypesSet := s.settings.JavaScriptCompilerOptionTypes != nil
	explicitCompilerOptions := cloneJavaScriptCompilerOptions(s.settings.JavaScriptCompilerOptions)
	if s.settings.JavaScriptUnusedDiagnostics {
		if explicitCompilerOptions == nil {
			explicitCompilerOptions = map[string]any{}
		}
		explicitCompilerOptions["noUnusedLocals"] = true
		explicitCompilerOptions["noUnusedParameters"] = true
	}
	ignoreProjectConfig := s.settings.JavaScriptIgnoreProjectConfig
	project := s.javascriptProject
	created := project == nil
	if project == nil {
		projectRoot := root
		if projectRoot == "" {
			projectRoot = filepath.Dir(fileURIPath(sourceURI))
			if projectRoot == "" {
				projectRoot = string(filepath.Separator)
			}
		}
		project = tsgoadapter.NewProject(javaScriptProjectPath(projectRoot))
		s.javascriptProject = project
	}
	preparation := s.javascriptPreparation
	documentGeneration := s.javascriptDocumentGeneration
	mappingGeneration := s.javascriptMappingGeneration
	if preparation != nil {
		currentDocument := s.openDocumentByURILocked(sourceURI)
		if currentDocument == nil {
			currentDocument = s.workspaceDocumentByURILocked(sourceURI)
		}
		if currentDocument != nil && !matchesJavaScriptDocumentSnapshot(preparation.documents[sourceURI], currentDocument) {
			if _, alreadyDirty := preparation.dirtyOwners[sourceURI]; !alreadyDirty {
				s.markJavaScriptDocumentChangedLocked(sourceURI)
				documentGeneration = s.javascriptDocumentGeneration
				mappingGeneration = s.javascriptMappingGeneration
			}
		}
	}
	s.mu.Unlock()

	discoveryRoots := []string{}
	if root != "" {
		discoveryRoots = append(discoveryRoots, root)
	}
	discoverySettings, discoverySettingsOK := s.javascriptProjectDiscoverySettingsContext(ctx, discoveryRoots)
	if !discoverySettingsOK {
		return nil, false
	}
	preparationRoot := root + "\x00javascript-discovery:" + javascriptProjectIdentitySettingsKeyForSettings(discoveryRoots, discoverySettings)

	s.mu.Lock()
	if preparation != nil && preparation.matchesLocked(s, preparationRoot, defaultLanguage, explicitTypes, explicitTypesSet, explicitCompilerOptions, ignoreProjectConfig) {
		active := preparation.activeMapping(sourceURI, position)
		s.mu.Unlock()
		if active == nil {
			return nil, false
		}
		state := project.State()
		s.logJavaScriptProjectPreparation(sourceURI, false, state)
		return &javaScriptRequest{project: project, active: active, files: preparation.mappings, state: state, cache: preparation.serviceCache}, true
	}
	s.mu.Unlock()
	var workspaceFiles map[string]string
	var autoImportExports map[string][]string
	previousPreparation := preparation
	reusePreparation := preparation != nil && preparation.matchesWorkspace(preparationRoot, defaultLanguage, explicitTypes, explicitTypesSet, explicitCompilerOptions, ignoreProjectConfig)
	if reusePreparation {
		workspaceFiles = preparation.workspaceFiles
		autoImportExports = cloneJavaScriptAutoImportExports(preparation.autoImportExports)
	}

	configuredTypes := explicitTypes
	typesConfigured := explicitTypesSet
	var compilerOptions map[string]any
	var projectConfig javaScriptProjectConfig
	if types, configured := javaScriptCompilerOptionTypes(explicitCompilerOptions); configured {
		configuredTypes, typesConfigured = types, true
	}
	if !ignoreProjectConfig {
		if reusePreparation {
			projectConfig = previousPreparation.projectConfig
		} else {
			projectConfig = s.cachedJavaScriptProjectConfig(root, sourceURI)
		}
		if projectConfig.Options != nil {
			if compilerOptions == nil {
				compilerOptions = map[string]any{}
			}
			for key, value := range projectConfig.Options {
				compilerOptions[key] = value
			}
		}
		if !typesConfigured && projectConfig.Types != nil {
			configuredTypes, typesConfigured = projectConfig.Types, true
		}
	}
	if len(explicitCompilerOptions) > 0 {
		if compilerOptions == nil {
			compilerOptions = map[string]any{}
		}
		for key, value := range explicitCompilerOptions {
			compilerOptions[key] = value
		}
	}
	if compilerOptions == nil {
		compilerOptions = map[string]any{}
	}
	// TypeScript's JavaScript project defaults follow the server-level checkJs
	// setting. Unused diagnostics also need checked-JavaScript mode so the
	// compiler can bind plain JavaScript and include them in diagnostics.
	s.mu.Lock()
	checkJS := s.settings.CheckJS || s.settings.JavaScriptUnusedDiagnostics
	s.mu.Unlock()
	compilerOptions["checkJs"] = checkJS
	embeddedTypes := make([]string, 0, len(configuredTypes))
	for _, typeName := range configuredTypes {
		if !strings.EqualFold(typeName, "node") {
			embeddedTypes = append(embeddedTypes, typeName)
		}
	}
	allowedAmbientTypes := make(map[string]struct{}, len(embeddedTypes))
	for _, typeName := range embeddedTypes {
		allowedAmbientTypes[javaScriptAmbientDirectoryName(typeName)] = struct{}{}
	}
	projectOptions := tsgoadapter.ProjectOptions{Types: embeddedTypes, TypesConfigured: typesConfigured, CompilerOptions: compilerOptions}
	if reusePreparation {
		if request, ok, handled := s.prepareJavaScriptRequestDelta(project, previousPreparation, sourceURI, position, documentGeneration, mappingGeneration, projectOptions); handled {
			if ok {
				s.logJavaScriptProjectPreparation(sourceURI, false, request.state)
			}
			return request, ok
		}
	}

	s.mu.Lock()
	documents := s.javaScriptDocumentsLocked()
	s.mu.Unlock()
	projectFiles := maps.Clone(workspaceFiles)
	if projectFiles == nil {
		projectFiles = map[string]string{}
	}
	if autoImportExports == nil {
		autoImportExports = map[string][]string{}
	}
	mappings := map[string]*javaScriptVirtualFile{}
	mappingsByOwner := map[string][]*javaScriptVirtualFile{}
	addMapping := func(mapping *javaScriptVirtualFile) {
		mappings[mapping.uri] = mapping
		mappingsByOwner[mapping.owner] = append(mappingsByOwner[mapping.owner], mapping)
	}
	var active *javaScriptVirtualFile
	if workspaceFiles == nil && root != "" {
		workspaceFilter := discoverySettings.filterForRoot(root)
		workspacePaths := make([]string, 0, 256)
		walkErr := filepath.WalkDir(root, func(path string, entry os.DirEntry, walkErr error) error {
			if ctx.Err() != nil {
				return ctx.Err()
			}
			s.mu.Lock()
			walkHook := s.javascriptWorkspaceWalkTestHook
			s.mu.Unlock()
			if walkHook != nil {
				walkHook(path)
			}
			if walkErr != nil {
				return nil
			}
			if entry.IsDir() {
				relative, relativeErr := filepath.Rel(root, path)
				if relativeErr != nil {
					return nil
				}
				if relative != "." && workspaceFilter.skipsDirectory(filepath.ToSlash(relative)) {
					return filepath.SkipDir
				}
				name := strings.ToLower(entry.Name())
				if name == ".git" || name == ".codex" || name == "dist" || name == "out" || name == "build" || name == "third_party" || name == "vendor" {
					return filepath.SkipDir
				}
				if name == "node_modules" && len(allowedAmbientTypes) == 0 {
					return filepath.SkipDir
				}
				if path != root && !pathContainsFile(path, fileURIPath(sourceURI)) && directoryHasJavaScriptProjectConfig(path) {
					return filepath.SkipDir
				}
				parent := strings.ToLower(filepath.Base(filepath.Dir(path)))
				if parent == "node_modules" && name != "@types" {
					return filepath.SkipDir
				}
				return nil
			}
			normalized := filepath.ToSlash(path)
			lower := strings.ToLower(normalized)
			if packageName, ambient := javaScriptAmbientPackageName(lower); ambient {
				if typesConfigured {
					if _, allowed := allowedAmbientTypes[packageName]; !allowed {
						return nil
					}
				} else if packageName == "node" {
					return nil
				}
			}
			if !isJavaScriptModuleFile(normalized) && filepath.Base(lower) != "package.json" {
				return nil
			}
			relative, relativeErr := filepath.Rel(root, path)
			if relativeErr != nil || relative == "." || !workspaceFilter.allowsFile(filepath.ToSlash(relative)) {
				return nil
			}
			if strings.Contains(lower, "/node_modules/") && !strings.Contains(lower, "/node_modules/@types/") {
				return nil
			}
			workspacePaths = append(workspacePaths, path)
			return nil
		})
		if walkErr != nil && ctx.Err() != nil {
			return nil, false
		}
		sort.Strings(workspacePaths)
		type workspaceSource struct {
			path    string
			text    string
			exports []string
			ok      bool
		}
		sources := make([]workspaceSource, len(workspacePaths))
		s.analysisWorkers.parallelFor(ctx, len(workspacePaths), func(workerCtx context.Context, index int) {
			path := workspacePaths[index]
			contents, err := s.readSourceFileBytes(workerCtx, path, s.includeReadLimiter)
			if err != nil {
				return
			}
			text := string(contents)
			sources[index] = workspaceSource{path: path, text: text, ok: true}
			if isJavaScriptModuleFile(path) {
				sources[index].exports = exportedJavaScriptNames(text)
			}
		})
		if ctx.Err() != nil {
			return nil, false
		}
		for _, source := range sources {
			if !source.ok {
				continue
			}
			projectPath := javaScriptProjectPath(source.path)
			projectFiles[projectPath] = source.text
			if source.exports != nil {
				autoImportExports[projectPath] = source.exports
			}
		}
	}
	for path := range projectFiles {
		if packageName, ambient := javaScriptAmbientPackageName(strings.ToLower(filepath.ToSlash(path))); ambient {
			if typesConfigured {
				if _, allowed := allowedAmbientTypes[packageName]; !allowed {
					delete(projectFiles, path)
				}
			} else if packageName == "node" {
				delete(projectFiles, path)
			}
		}
	}
	if !reusePreparation {
		workspaceFiles = maps.Clone(projectFiles)
	}
	for _, doc := range documents {
		if ctx.Err() != nil {
			return nil, false
		}
		if isJavaScriptModuleFile(fileURIPath(doc.URI)) {
			path := javaScriptProjectPath(fileURIPath(doc.URI))
			projectFiles[path] = doc.Text
			autoImportExports[path] = exportedJavaScriptNames(doc.Text)
			continue
		}
		if reusePreparation {
			if snapshot, ok := previousPreparation.documents[doc.URI]; ok && matchesJavaScriptDocumentSnapshot(snapshot, doc) {
				for _, mapping := range previousPreparation.mappingsByOwner[doc.URI] {
					addMapping(mapping)
					projectFiles[mapping.path] = mapping.virtual.Text
					if workspacepkg.SameFileIdentityURI(doc.URI, sourceURI) {
						if _, ok := mapping.virtual.ToVirtualPosition(position, doc); ok {
							active = mapping
						}
					}
				}
				continue
			}
		}
		parsed := s.parseTextDocument(doc, defaultLanguage)
		for _, language := range []core.EmbeddedLanguage{core.LanguageJavaScript, core.LanguageJScript} {
			virtual := core.BuildVirtualDocument(parsed, language)
			if virtual.Text == "" {
				continue
			}
			path := javaScriptVirtualPath(doc.URI, language)
			uri := (&javaScriptFileURI{path: path}).String()
			mapping := &javaScriptVirtualFile{
				path:            path,
				uri:             uri,
				owner:           doc.URI,
				virtual:         virtual,
				virtualDocument: core.NewTextDocument(uri, virtual.LanguageID, 0, virtual.Text),
				source:          doc,
			}
			projectFiles[path] = virtual.Text
			addMapping(mapping)
			if workspacepkg.SameFileIdentityURI(doc.URI, sourceURI) {
				if _, ok := virtual.ToVirtualPosition(position, doc); ok {
					active = mapping
				}
			}
		}
	}
	if active == nil {
		return nil, false
	}
	state, err := project.Update(projectFiles, projectOptions)
	if err != nil {
		s.logDebugSummary("[asp-lsp] javascript.languageService.error: " + err.Error())
		return nil, false
	}
	nextPreparation := &javaScriptProjectPreparation{
		root:                preparationRoot,
		defaultLanguage:     defaultLanguage,
		explicitTypes:       explicitTypes,
		explicitTypesSet:    explicitTypesSet,
		compilerOptions:     cloneJavaScriptCompilerOptions(explicitCompilerOptions),
		ignoreProjectConfig: ignoreProjectConfig,
		projectConfig:       cloneJavaScriptProjectConfig(projectConfig),
		documentGeneration:  documentGeneration,
		mappingGeneration:   mappingGeneration,
		documents:           javaScriptDocumentSnapshots(documents),
		mappings:            mappings,
		mappingsByOwner:     mappingsByOwner,
		workspaceFiles:      workspaceFiles,
		autoImportExports:   autoImportExports,
		jqueryCompletion:    javaScriptJQueryCompletionFromProject(typesConfigured, configuredTypes, root),
		serviceCache:        newJavaScriptServiceResultCache(),
		dirtyOwners:         map[string]struct{}{},
	}
	s.mu.Lock()
	if s.javascriptProject == project {
		// The project now holds exactly these files, so the preparation must be
		// replaced even when documents changed during the rebuild. A stale
		// generation keeps the fast path off until the next request resyncs.
		if nextPreparation.matchesLocked(s, preparationRoot, defaultLanguage, explicitTypes, explicitTypesSet, explicitCompilerOptions, ignoreProjectConfig) {
			nextPreparation.documentGeneration = s.javascriptDocumentGeneration
			nextPreparation.mappingGeneration = s.javascriptMappingGeneration
		} else {
			nextPreparation.markChangedDocumentsDirty(uniqueJavaScriptDocuments(s.documents, s.workspace))
		}
		s.javascriptPreparation = nextPreparation
	}
	s.mu.Unlock()
	s.logJavaScriptProjectPreparation(sourceURI, created, state)
	return &javaScriptRequest{project: project, active: active, files: mappings, state: state, cache: nextPreparation.serviceCache}, true
}

// markChangedDocumentsDirty records owners whose current document no longer
// matches the snapshot this preparation was built from.
func (p *javaScriptProjectPreparation) markChangedDocumentsDirty(documents []*core.TextDocument) {
	current := make(map[string]struct{}, len(documents))
	for _, document := range documents {
		current[document.URI] = struct{}{}
		if !matchesJavaScriptDocumentSnapshot(p.documents[document.URI], document) {
			p.dirtyOwners[document.URI] = struct{}{}
		}
	}
	for uri := range p.documents {
		if _, ok := current[uri]; !ok {
			p.dirtyOwners[uri] = struct{}{}
		}
	}
}

func (s *Server) prepareJavaScriptRequestDelta(project *tsgoadapter.Project, preparation *javaScriptProjectPreparation, sourceURI string, position lsp.Position, documentGeneration, mappingGeneration uint64, options tsgoadapter.ProjectOptions) (*javaScriptRequest, bool, bool) {
	s.mu.Lock()
	if s.javascriptPreparation != preparation || preparation == nil || len(preparation.dirtyOwners) == 0 {
		s.mu.Unlock()
		return nil, false, false
	}
	type ownerDelta struct {
		uri              string
		snapshot         javaScriptDocumentSnapshot
		document         *core.TextDocument
		previousMappings []*javaScriptVirtualFile
		nextMappings     []*javaScriptVirtualFile
	}
	owners := make([]string, 0, len(preparation.dirtyOwners))
	if _, sourceDirty := preparation.dirtyOwners[sourceURI]; sourceDirty {
		owners = append(owners, sourceURI)
	} else {
		// Embedded scripts share a global TypeScript environment. When the
		// requester itself is clean, every changed owner can affect its types.
		for owner := range preparation.dirtyOwners {
			owners = append(owners, owner)
		}
		sort.Strings(owners)
	}
	deltas := make([]ownerDelta, 0, len(owners))
	for _, owner := range owners {
		currentDocument := s.openDocumentByURILocked(owner)
		if currentDocument == nil {
			currentDocument = s.workspaceDocumentByURILocked(owner)
		}
		delta := ownerDelta{uri: owner, previousMappings: preparation.mappingsByOwner[owner]}
		if currentDocument != nil {
			delta.snapshot = javaScriptDocumentSnapshot{document: currentDocument, version: currentDocument.Version, text: currentDocument.Text}
			delta.document = currentDocument.Clone()
		}
		deltas = append(deltas, delta)
	}
	defaultLanguage := preparation.defaultLanguage
	s.mu.Unlock()

	upserts := map[string]string{}
	deleteSet := map[string]struct{}{}
	autoImportExports := preparation.autoImportExports
	autoImportExportsChanged := false
	for index := range deltas {
		delta := &deltas[index]
		previousTextByPath := make(map[string]string, len(delta.previousMappings))
		for _, mapping := range delta.previousMappings {
			deleteSet[mapping.path] = struct{}{}
			previousTextByPath[mapping.path] = mapping.virtual.Text
		}
		document := delta.document
		if document != nil {
			if isJavaScriptModuleFile(fileURIPath(document.URI)) {
				path := javaScriptProjectPath(fileURIPath(document.URI))
				previousText := preparation.documents[delta.uri].text
				if previousText != document.Text {
					upserts[path] = document.Text
				}
				if !autoImportExportsChanged {
					autoImportExports = cloneJavaScriptAutoImportExports(autoImportExports)
					autoImportExportsChanged = true
				}
				autoImportExports[path] = exportedJavaScriptNames(document.Text)
				delete(deleteSet, path)
			} else {
				parsed := s.parseTextDocument(document, defaultLanguage)
				for _, language := range []core.EmbeddedLanguage{core.LanguageJavaScript, core.LanguageJScript} {
					virtual := core.BuildVirtualDocument(parsed, language)
					if virtual.Text == "" {
						continue
					}
					path := javaScriptVirtualPath(document.URI, language)
					uri := (&javaScriptFileURI{path: path}).String()
					mapping := &javaScriptVirtualFile{
						path: path, uri: uri, owner: document.URI, virtual: virtual,
						virtualDocument: core.NewTextDocument(uri, virtual.LanguageID, 0, virtual.Text), source: document,
					}
					delta.nextMappings = append(delta.nextMappings, mapping)
					if previousTextByPath[path] != virtual.Text {
						upserts[path] = virtual.Text
					}
					delete(deleteSet, path)
				}
			}
		}
	}
	deletes := make([]string, 0, len(deleteSet))
	for path := range deleteSet {
		deletes = append(deletes, path)
		if _, exists := autoImportExports[path]; exists {
			if !autoImportExportsChanged {
				autoImportExports = cloneJavaScriptAutoImportExports(autoImportExports)
				autoImportExportsChanged = true
			}
			delete(autoImportExports, path)
		}
	}
	s.mu.Lock()
	deltaHook := s.javascriptProjectDeltaTestHook
	s.mu.Unlock()
	if deltaHook != nil {
		deltaHook(len(upserts), len(deletes))
	}
	state := project.State()
	if len(upserts) > 0 || len(deletes) > 0 {
		var err error
		state, err = project.UpdateDelta(upserts, deletes, options)
		if err != nil {
			s.logDebugSummary("[asp-lsp] javascript.languageService.error: " + err.Error())
			return nil, false, true
		}
	}
	s.mu.Lock()
	samePreparation := s.javascriptPreparation == preparation
	current := samePreparation
	for _, delta := range deltas {
		latestDocument := s.openDocumentByURILocked(delta.uri)
		if latestDocument == nil {
			latestDocument = s.workspaceDocumentByURILocked(delta.uri)
		}
		if latestDocument != delta.snapshot.document || latestDocument != nil &&
			(latestDocument.Version != delta.snapshot.version || latestDocument.Text != delta.snapshot.text) {
			current = false
			break
		}
	}
	if samePreparation {
		// UpdateDelta has already changed the project, so the mappings must
		// follow it even when a document moved on meanwhile. Otherwise the next
		// delta diffs against files the project no longer contains. Owners that
		// changed again stay dirty and are refreshed by the next request.
		for _, delta := range deltas {
			for _, mapping := range delta.previousMappings {
				delete(preparation.mappings, mapping.uri)
			}
			delete(preparation.mappingsByOwner, delta.uri)
			delete(preparation.documents, delta.uri)
			if delta.document != nil {
				preparation.documents[delta.uri] = delta.snapshot
			}
			for _, mapping := range delta.nextMappings {
				preparation.mappings[mapping.uri] = mapping
				preparation.mappingsByOwner[delta.uri] = append(preparation.mappingsByOwner[delta.uri], mapping)
			}
			if current {
				delete(preparation.dirtyOwners, delta.uri)
			}
		}
		if current && len(preparation.dirtyOwners) == 0 && s.javascriptDocumentGeneration == documentGeneration && s.javascriptMappingGeneration == mappingGeneration {
			preparation.documentGeneration = documentGeneration
			preparation.mappingGeneration = mappingGeneration
		}
		preparation.autoImportExports = autoImportExports
		preparation.serviceCache = newJavaScriptServiceResultCache()
	}
	s.mu.Unlock()
	if !current {
		return nil, false, false
	}
	active := preparation.activeMapping(sourceURI, position)
	if active == nil {
		return nil, false, true
	}
	return &javaScriptRequest{project: project, active: active, files: preparation.mappings, state: state, cache: preparation.serviceCache}, true, true
}
