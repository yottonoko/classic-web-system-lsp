package lspserver

import (
	"context"
	"errors"
	"io/fs"
	"os"
	"path/filepath"
	"sort"
	"strconv"
	"strings"
	"time"
	"unicode/utf8"

	"github.com/yottonoko/classic-web-system-lsp/internal/core"
	"github.com/yottonoko/classic-web-system-lsp/internal/excel"
	"github.com/yottonoko/classic-web-system-lsp/internal/graph"
	workspacepkg "github.com/yottonoko/classic-web-system-lsp/internal/workspace"
)

var (
	errAnalysisExcelCommitStale       = errors.New("analysis Excel export became stale before commit")
	errAnalysisExcelNoAnalyzableFiles = errors.New("fileUris contains no analyzable files")
	errAnalysisExcelPayloadIncomplete = errors.New("analysis Excel payload construction did not complete")
)

type analysisExcelPayloadResult struct {
	payload    graph.Payload
	generation uint64
	complete   bool
	err        error
}

func incompleteAnalysisExcelPayload(generation uint64, err error) analysisExcelPayloadResult {
	if err == nil {
		err = errAnalysisExcelPayloadIncomplete
	}
	return analysisExcelPayloadResult{generation: generation, err: err}
}

func (s *Server) exportAnalysisExcel(ctx context.Context, params executeCommandParams) any {
	if ctx == nil {
		ctx = context.Background()
	}
	if len(params.Arguments) == 0 {
		return map[string]any{"ok": false, "error": "missing export options"}
	}
	var arg analysisExcelExportArg
	_ = remarshal(params.Arguments[0], &arg)
	if arg.TargetPath == "" {
		return map[string]any{"ok": false, "error": "missing targetPath"}
	}
	taskID, startedAt := s.beginAnalysisExcelProgressTask(arg.TargetPath)
	operationCtx := s.registerProgressCancellationWithParent(taskID, ctx)
	defer s.unregisterProgressCancellation(taskID)
	progress := s.analysisExcelProgressReporter(arg.TargetPath, taskID, startedAt)
	progressState := "failed"
	defer func() {
		s.finishProgressTask(taskID, "excel.export", progressState)
	}()
	if operationCtx.Err() != nil {
		progressState = "cancelled"
		return map[string]any{"ok": false, "targetPath": arg.TargetPath, "error": "cancelled"}
	}
	if !s.waitForDocumentOpenAnalysisContext(operationCtx) {
		progressState = "cancelled"
		return map[string]any{"ok": false, "targetPath": arg.TargetPath, "error": "cancelled"}
	}
	payloadResult := s.exportAnalysisPayloadContextWithProgressResult(operationCtx, arg, analysisExcelGraphProgressReporter(progress))
	if !payloadResult.complete {
		err := payloadResult.err
		if err == nil {
			err = operationCtx.Err()
		}
		if errors.Is(err, context.Canceled) || errors.Is(err, context.DeadlineExceeded) {
			progressState = "cancelled"
			return map[string]any{"ok": false, "targetPath": arg.TargetPath, "error": "cancelled"}
		}
		if errors.Is(err, errAnalysisExcelNoAnalyzableFiles) {
			return map[string]any{"ok": false, "targetPath": arg.TargetPath, "error": errAnalysisExcelNoAnalyzableFiles.Error()}
		}
		if err == nil {
			err = errAnalysisExcelPayloadIncomplete
		}
		return map[string]any{"ok": false, "targetPath": arg.TargetPath, "error": err.Error()}
	}
	payload := payloadResult.payload
	workbookSettings := s.analysisExcelWorkbookSettings(arg)
	if workbookSettings.AnalysisFileCount == 0 && payload.Stats != nil {
		workbookSettings.AnalysisFileCount = payload.Stats["files"]
	}
	sheets := excel.CreateAnalysisSheets(payload, s.analysisExcelLocale(), excel.AnalysisSheetsOptions{
		GeneratedAt:  time.Now(),
		TargetURI:    arg.URI,
		IncludeGlobs: append([]string(nil), arg.IncludeGlobs...),
		ExcludeGlobs: append([]string(nil), arg.ExcludeGlobs...),
		Settings:     workbookSettings,
		Progress:     progress,
		Cancelled:    func() bool { return operationCtx.Err() != nil },
	})
	if operationCtx.Err() != nil || sheets == nil {
		progressState = "cancelled"
		return map[string]any{"ok": false, "targetPath": arg.TargetPath, "error": "cancelled"}
	}
	if err := excel.WriteAnalysisWorkbookFileWithOptions(arg.TargetPath, sheets, excel.AnalysisWorkbookWriteOptions{
		Progress:  progress,
		Cancelled: func() bool { return operationCtx.Err() != nil },
		Commit: func(tempPath string) error {
			return s.commitAnalysisExcelWorkbook(operationCtx, arg.TargetPath, tempPath, payloadResult.generation)
		},
	}); err != nil {
		if operationCtx.Err() != nil {
			progressState = "cancelled"
			return map[string]any{"ok": false, "targetPath": arg.TargetPath, "error": "cancelled"}
		}
		if errors.Is(err, errAnalysisExcelCommitStale) {
			return map[string]any{"ok": false, "targetPath": arg.TargetPath, "error": errAnalysisExcelCommitStale.Error()}
		}
		return map[string]any{"ok": false, "targetPath": arg.TargetPath, "error": err.Error()}
	}
	progressState = "completed"
	return map[string]any{"ok": true, "targetPath": arg.TargetPath}
}

func (s *Server) exportAnalysisExcelRequest(ctx context.Context, params executeCommandParams) (any, *rpcError) {
	if len(params.Arguments) == 0 {
		return nil, &rpcError{Code: -32602, Message: "targetPath is required."}
	}
	var arg analysisExcelExportArg
	if err := remarshal(params.Arguments[0], &arg); err != nil {
		return nil, invalidParams(err)
	}
	if strings.TrimSpace(arg.TargetPath) == "" {
		return nil, &rpcError{Code: -32602, Message: "targetPath is required."}
	}
	result := s.exportAnalysisExcel(ctx, params)
	if payload, ok := result.(map[string]any); ok {
		if message, ok := payload["error"].(string); ok {
			switch message {
			case errAnalysisExcelNoAnalyzableFiles.Error():
				return nil, &rpcError{Code: -32602, Message: "fileUris contains no analyzable files."}
			case "cancelled":
				return nil, requestCancelledError()
			case errGraphCollectionGeneration.Error():
				return nil, &rpcError{Code: -32803, Message: "analysis graph became stale; retry the request."}
			case errAnalysisExcelCommitStale.Error():
				return nil, &rpcError{Code: -32803, Message: "analysis graph became stale; retry the request."}
			case errGraphCollectionIncludeSync.Error(), errGraphCollectionIncludeExpansion.Error(), errAnalysisExcelPayloadIncomplete.Error():
				return nil, &rpcError{Code: -32603, Message: message}
			default:
				return nil, &rpcError{Code: -32603, Message: message}
			}
		}
	}
	return result, nil
}

func (s *Server) analysisExcelLocale() excel.Locale {
	locale := s.settings.ExcelLocale
	if locale == "" || locale == "auto" {
		locale = s.settings.Locale
	}
	if locale == "en" {
		return excel.LocaleEnglish
	}
	return excel.LocaleJapanese
}

func (s *Server) exportAnalysisPayload(arg analysisExcelExportArg) (graph.Payload, bool) {
	return s.exportAnalysisPayloadContext(context.Background(), arg)
}

func (s *Server) exportAnalysisPayloadContext(ctx context.Context, arg analysisExcelExportArg) (graph.Payload, bool) {
	return s.exportAnalysisPayloadContextWithProgress(ctx, arg, nil)
}

func (s *Server) exportAnalysisPayloadContextWithProgress(ctx context.Context, arg analysisExcelExportArg, report graphProgressReporter) (graph.Payload, bool) {
	result := s.exportAnalysisPayloadContextWithProgressResult(ctx, arg, report)
	return result.payload, result.complete
}

func (s *Server) exportAnalysisPayloadContextWithProgressResult(ctx context.Context, arg analysisExcelExportArg, report graphProgressReporter) analysisExcelPayloadResult {
	if ctx == nil {
		ctx = context.Background()
	}
	generation := s.graphGenerationSnapshot()
	settings := s.analysisExcelWorkbookSettings(arg)
	scope := arg.Scope
	if scope != "folder" && scope != "workspace" {
		scope = "document"
	}
	rootURI := canonicalGraphURI(arg.URI)
	documents := []*core.ParsedDocument{}
	includeExternalFiles := true

	if len(arg.FileURIs) > 0 {
		scope = "workspace"
		rootURI = ""
		workspaceRoots := s.workspaceRootsSnapshot()
		for index, uri := range arg.FileURIs {
			if ctx.Err() != nil {
				return incompleteAnalysisExcelPayload(generation, ctx.Err())
			}
			path := fileURIPath(uri)
			if path == "" || !isWorkspaceASPFile(path) || workspaceRootPathForPath(path, workspaceRoots) == "" {
				continue
			}
			parsed, err := s.parsedGraphDocumentContextResult(ctx, uri)
			if err != nil {
				if errors.Is(err, fs.ErrNotExist) {
					parsed = nil
				} else {
					return incompleteAnalysisExcelPayload(generation, err)
				}
			}
			if parsed != nil {
				documents = append(documents, parsed)
			}
			if report != nil {
				report("graph.workspace.collectDocuments", progressDetailForURI(uri), index+1, len(arg.FileURIs))
			}
			if ctx.Err() != nil || !s.graphGenerationCurrent(ctx, generation) {
				if err := ctx.Err(); err != nil {
					return incompleteAnalysisExcelPayload(generation, err)
				}
				return incompleteAnalysisExcelPayload(generation, errGraphCollectionGeneration)
			}
		}
		if len(documents) == 0 {
			return analysisExcelPayloadResult{
				payload: emptyGraphPayload(scope, "", ""), generation: generation, err: errAnalysisExcelNoAnalyzableFiles,
			}
		}
	} else {
		switch scope {
		case "workspace":
			collection := s.workspaceGraphDocumentsContextWithProgressResult(ctx, true, graphScopeProgress(report, "graph.workspace.collectDocuments"))
			if !collection.complete {
				return incompleteAnalysisExcelPayload(collection.generation, collection.err)
			}
			documents, rootURI = collection.documents, collection.rootURI
			generation = collection.generation
		case "folder":
			collection := s.navigationFolderDocumentsContextWithProgressResult(ctx, arg.URI, graphScopeProgress(report, "graph.folder.collectDocuments"))
			if !collection.complete {
				return incompleteAnalysisExcelPayload(collection.generation, collection.err)
			}
			documents = collection.documents
			generation = collection.generation
		case "document":
			if strings.TrimSpace(arg.URI) == "" {
				return analysisExcelPayloadResult{payload: emptyGraphPayload(scope, "", ""), generation: generation, err: errAnalysisExcelNoAnalyzableFiles}
			}
			if settings.IncludeRelatedIncludeTreesForUnresolved && !s.waitForWorkspaceIndex(ctx) {
				return incompleteAnalysisExcelPayload(generation, ctx.Err())
			}
			parsed, err := s.parsedGraphDocumentContextResult(ctx, arg.URI)
			if err != nil {
				if errors.Is(err, fs.ErrNotExist) {
					return analysisExcelPayloadResult{payload: emptyGraphPayload(scope, rootURI, rootURI), generation: generation, err: errAnalysisExcelNoAnalyzableFiles}
				}
				return incompleteAnalysisExcelPayload(generation, err)
			}
			if parsed == nil {
				if err := ctx.Err(); err != nil {
					return incompleteAnalysisExcelPayload(generation, err)
				}
				return analysisExcelPayloadResult{payload: emptyGraphPayload(scope, rootURI, rootURI), generation: generation, err: errAnalysisExcelNoAnalyzableFiles}
			}
			collection := s.documentGraphIncludeTreeContextCollectionResult(ctx, parsed, generation)
			if !collection.complete {
				return incompleteAnalysisExcelPayload(collection.generation, collection.err)
			}
			documents = collection.documents
			if settings.IncludeRelatedIncludeTreesForUnresolved {
				related := s.relatedIncludeTreeDocumentsContextResult(ctx, generation, parsed, documents)
				if !related.complete {
					return incompleteAnalysisExcelPayload(related.generation, related.err)
				}
				documents = append(documents, related.documents...)
			}
			if report != nil {
				report("graph.document.collectDocuments", progressDetailForURI(arg.URI), len(documents), len(documents))
			}
			if ctx.Err() != nil || !s.graphGenerationCurrent(ctx, generation) {
				if err := ctx.Err(); err != nil {
					return incompleteAnalysisExcelPayload(generation, err)
				}
				return incompleteAnalysisExcelPayload(generation, errGraphCollectionGeneration)
			}
		}
	}

	if len(arg.FileURIs) == 0 {
		includeGlobs, excludeGlobs, respectGitIgnore := s.analysisExcelWorkspaceFilters(arg)
		documents = s.filterExportGraphDocuments(documents, arg.URI, includeGlobs, excludeGlobs, respectGitIgnore)
		includeExternalFiles = true
	}
	if report != nil {
		report("graph."+scope+".filterDocuments", progressDetailForURI(arg.URI), len(documents), len(documents))
	}
	if ctx.Err() != nil || !s.graphGenerationCurrent(ctx, generation) {
		if err := ctx.Err(); err != nil {
			return incompleteAnalysisExcelPayload(generation, err)
		}
		return incompleteAnalysisExcelPayload(generation, errGraphCollectionGeneration)
	}
	documents = dedupeParsedDocumentsByFileIdentity(documents)
	if ctx.Err() != nil || !s.graphGenerationCurrent(ctx, generation) {
		if err := ctx.Err(); err != nil {
			return incompleteAnalysisExcelPayload(generation, err)
		}
		return incompleteAnalysisExcelPayload(generation, errGraphCollectionGeneration)
	}
	if len(documents) == 0 {
		return analysisExcelPayloadResult{
			payload: emptyGraphPayload(scope, rootURI, rootURI), generation: generation, err: errAnalysisExcelNoAnalyzableFiles,
		}
	}
	payload, complete := s.buildDocumentSetGraphWithProgressAtGeneration(ctx, generation, scope, rootURI, documents, includeExternalFiles, settings.IncludeAnalysisTypeDetails, report)
	if !complete {
		if err := ctx.Err(); err != nil {
			return incompleteAnalysisExcelPayload(generation, err)
		}
		return incompleteAnalysisExcelPayload(generation, errGraphCollectionGeneration)
	}
	if ctx.Err() != nil || !s.graphGenerationCurrent(ctx, generation) {
		if err := ctx.Err(); err != nil {
			return incompleteAnalysisExcelPayload(generation, err)
		}
		return incompleteAnalysisExcelPayload(generation, errGraphCollectionGeneration)
	}
	setGraphPayloadSetting(&payload, "includeRelatedIncludeTreesForUnresolved", settings.IncludeRelatedIncludeTreesForUnresolved)
	setGraphPayloadSetting(&payload, "includeAnalysisTypeDetails", settings.IncludeAnalysisTypeDetails)
	if scope == "document" {
		payload.URI = rootURI
		payload.RootURI = rootURI
		markGraphRoot(&payload, rootURI)
	}
	return analysisExcelPayloadResult{payload: payload, generation: generation, complete: true}
}

func (s *Server) commitAnalysisExcelWorkbook(ctx context.Context, targetPath, tempPath string, generation uint64) error {
	s.workspaceIndexDiskCommitMu.Lock()
	defer s.workspaceIndexDiskCommitMu.Unlock()
	if ctx != nil && ctx.Err() != nil {
		return context.Canceled
	}
	s.mu.Lock()
	current := s.graphGeneration == generation
	s.mu.Unlock()
	if !current {
		return errAnalysisExcelCommitStale
	}
	if ctx != nil && ctx.Err() != nil {
		return context.Canceled
	}
	return os.Rename(tempPath, targetPath)
}

func (s *Server) analysisExcelWorkspaceFilters(arg analysisExcelExportArg) ([]string, []string, bool) {
	s.mu.Lock()
	defaultIncludes := append([]string(nil), s.settings.WorkspaceIncludeGlobs...)
	defaultExcludes := append([]string(nil), s.settings.WorkspaceExcludeGlobs...)
	defaultRespectGitIgnore := s.settings.WorkspaceRespectGitIgnore
	s.mu.Unlock()
	includeGlobs := append([]string(nil), arg.IncludeGlobs...)
	if arg.IncludeGlobs == nil {
		includeGlobs = defaultIncludes
	}
	excludeGlobs := append([]string(nil), arg.ExcludeGlobs...)
	if arg.ExcludeGlobs == nil {
		excludeGlobs = defaultExcludes
	}
	respectGitIgnore := defaultRespectGitIgnore
	if arg.RespectGitIgnore != nil {
		respectGitIgnore = *arg.RespectGitIgnore
	}
	return includeGlobs, excludeGlobs, respectGitIgnore
}

func (s *Server) filterExportGraphDocuments(documents []*core.ParsedDocument, rootURI string, includeGlobs, excludeGlobs []string, respectGitIgnore bool) []*core.ParsedDocument {
	roots := s.workspaceRootsSnapshot()
	sort.Slice(roots, func(i, j int) bool { return filepath.ToSlash(roots[i].Path) < filepath.ToSlash(roots[j].Path) })
	gitIgnoreGlobs := s.gitIgnoreGlobsByWorkspaceRoot(roots, respectGitIgnore)
	rootURI = canonicalGraphURI(rootURI)
	allowedURIs := map[string]struct{}{rootURI: {}}
	for _, document := range documents {
		if document == nil {
			continue
		}
		documentURI := canonicalGraphURI(document.URI)
		if workspaceGraphURIAllowedInRoots(documentURI, roots, includeGlobs, excludeGlobs, gitIgnoreGlobs) {
			allowedURIs[documentURI] = struct{}{}
		}
	}
	directReferenceOwners := make(map[string]struct{}, len(allowedURIs))
	for uri := range allowedURIs {
		directReferenceOwners[uri] = struct{}{}
	}
	for _, document := range documents {
		if document == nil {
			continue
		}
		if _, ownerAllowed := directReferenceOwners[canonicalGraphURI(document.URI)]; !ownerAllowed {
			continue
		}
		for _, include := range document.Includes {
			details, ok := s.includeTargetDetailsForMode(document.URI, include.Path, include.Mode)
			if ok && details.Path != "" {
				allowedURIs[canonicalGraphURI(filePathURI(details.Path))] = struct{}{}
			}
		}
	}
	result := make([]*core.ParsedDocument, 0, len(documents))
	for _, document := range documents {
		if document == nil {
			continue
		}
		documentURI := canonicalGraphURI(document.URI)
		if _, allowed := allowedURIs[documentURI]; !allowed {
			continue
		}
		result = append(result, s.copyGraphDocumentWithAllowedIncludes(document, allowedURIs))
	}
	return result
}

func (s *Server) copyGraphDocumentWithAllowedIncludes(document *core.ParsedDocument, allowedURIs map[string]struct{}) *core.ParsedDocument {
	filteredIncludes := make([]core.Include, 0, len(document.Includes))
	for _, include := range document.Includes {
		details, ok := s.includeTargetDetailsForMode(document.URI, include.Path, include.Mode)
		if !ok || details.Path == "" {
			continue
		}
		targetURI := canonicalGraphURI(filePathURI(details.Path))
		if _, allowed := allowedURIs[targetURI]; allowed {
			filteredIncludes = append(filteredIncludes, include)
		}
	}
	copied := *document
	copied.Includes = filteredIncludes
	return &copied
}

func workspaceGraphPathAllowed(path, rootPath string, includeGlobs, excludeGlobs, gitIgnoreGlobs []string) bool {
	if rootPath == "" {
		return false
	}
	relative, err := filepath.Rel(filepath.Clean(rootPath), filepath.Clean(path))
	if err != nil {
		return false
	}
	if relative == "." {
		return true
	}
	if relative == ".." || filepath.IsAbs(relative) || strings.HasPrefix(relative, ".."+string(filepath.Separator)) {
		return false
	}
	return workspaceGraphFileAllowed(filepath.ToSlash(relative), includeGlobs, excludeGlobs, gitIgnoreGlobs)
}

func workspaceGraphPathAllowedWithFilter(path, rootPath string, filter workspaceGraphFileFilter) bool {
	if rootPath == "" {
		return false
	}
	relative, err := filepath.Rel(filepath.Clean(rootPath), filepath.Clean(path))
	if err != nil {
		return false
	}
	if relative == "." {
		return true
	}
	if relative == ".." || filepath.IsAbs(relative) || strings.HasPrefix(relative, ".."+string(filepath.Separator)) {
		return false
	}
	return filter.allows(filepath.ToSlash(relative), rootPath)
}

func dedupeParsedDocumentsByFileIdentity(documents []*core.ParsedDocument) []*core.ParsedDocument {
	seen := map[string]struct{}{}
	result := make([]*core.ParsedDocument, 0, len(documents))
	for _, document := range documents {
		if document == nil {
			continue
		}
		key := workspacepkg.FileIdentityKeyFromURI(document.URI)
		if _, ok := seen[key]; ok {
			continue
		}
		seen[key] = struct{}{}
		result = append(result, document)
	}
	return result
}

const (
	defaultWorkspacePreviewMaxFiles = 5000
	maxWorkspacePreviewFiles        = 10000
	maxWorkspacePreviewGlobs        = 128
	maxWorkspacePreviewGlobLength   = 1024
)

func boundedWorkspacePreviewMaxFiles(value *int) int {
	if value == nil || *value <= 0 {
		return defaultWorkspacePreviewMaxFiles
	}
	return min(*value, maxWorkspacePreviewFiles)
}

func boundedWorkspacePreviewGlobs(globs []string) []string {
	if len(globs) > maxWorkspacePreviewGlobs {
		globs = globs[:maxWorkspacePreviewGlobs]
	}
	bounded := make([]string, 0, len(globs))
	for _, glob := range globs {
		if len(glob) > maxWorkspacePreviewGlobLength {
			glob = glob[:maxWorkspacePreviewGlobLength]
			for !utf8.ValidString(glob) {
				glob = glob[:len(glob)-1]
			}
		}
		bounded = append(bounded, glob)
	}
	return bounded
}

func (s *Server) previewWorkspaceFiles(ctx context.Context, params executeCommandParams) any {
	if ctx == nil {
		ctx = context.Background()
	}
	taskID, startedAt := s.beginWorkspacePreviewProgressTask()
	operationCtx := s.registerProgressCancellationWithParent(taskID, ctx)
	defer s.unregisterProgressCancellation(taskID)
	defer s.publishWorkspacePreviewIdleStatus(taskID)
	var arg struct {
		IncludeGlobs     []string `json:"includeGlobs"`
		ExcludeGlobs     []string `json:"excludeGlobs"`
		ShowUnmatched    *bool    `json:"showUnmatched"`
		RespectGitIgnore *bool    `json:"respectGitIgnore"`
		MaxFiles         *int     `json:"maxFiles"`
	}
	if len(params.Arguments) > 0 {
		_ = remarshal(params.Arguments[0], &arg)
	}
	roots := s.workspaceRootsSnapshot()
	sort.Slice(roots, func(i, j int) bool { return filepath.ToSlash(roots[i].Path) < filepath.ToSlash(roots[j].Path) })
	s.mu.Lock()
	defaultIncludes := append([]string(nil), s.settings.WorkspaceIncludeGlobs...)
	defaultExcludes := append([]string(nil), s.settings.WorkspaceExcludeGlobs...)
	defaultRespectGitIgnore := s.settings.WorkspaceRespectGitIgnore
	s.mu.Unlock()
	if defaultIncludes == nil {
		defaultIncludes = []string{"**/*.{asp,asa,inc,vbs}"}
	}
	includeGlobs := append([]string(nil), arg.IncludeGlobs...)
	if arg.IncludeGlobs == nil {
		includeGlobs = defaultIncludes
	}
	excludeGlobs := append([]string(nil), arg.ExcludeGlobs...)
	if arg.ExcludeGlobs == nil {
		excludeGlobs = defaultExcludes
	}
	includeGlobs = boundedWorkspacePreviewGlobs(includeGlobs)
	excludeGlobs = boundedWorkspacePreviewGlobs(excludeGlobs)
	if arg.ShowUnmatched == nil {
		value := true
		arg.ShowUnmatched = &value
	}
	if arg.RespectGitIgnore == nil {
		value := defaultRespectGitIgnore
		arg.RespectGitIgnore = &value
	}
	maxFiles := boundedWorkspacePreviewMaxFiles(arg.MaxFiles)
	includeCounts := make([]map[string]any, len(includeGlobs))
	for i, pattern := range includeGlobs {
		includeCounts[i] = map[string]any{"glob": pattern, "files": 0}
	}
	excludeCounts := make([]map[string]any, len(excludeGlobs))
	for i, pattern := range excludeGlobs {
		excludeCounts[i] = map[string]any{"glob": pattern, "files": 0}
	}
	globStats := map[string]any{"include": includeCounts, "exclude": excludeCounts}
	previewRoots := make([]map[string]any, 0, len(roots))
	matchedCount := 0
	totalBytes := int64(0)
	visibleCount := 0
	truncated := false
	processedCount := 0
	totalFiles := 0
	for _, root := range roots {
		if operationCtx.Err() != nil {
			break
		}
		gitIgnoreGlobs := []string{}
		if *arg.RespectGitIgnore {
			gitIgnoreGlobs = s.readGitIgnoreGlobsContext(withSourceReadBoundaries(operationCtx, root.Path), root.Path)
		}
		if operationCtx.Err() != nil {
			break
		}
		rootFiles := make([]map[string]any, 0)
		completed := walkWorkspaceFilesWithContextProgress(operationCtx, root.Path, func(file workspaceFile) {
			if operationCtx.Err() != nil {
				return
			}
			totalFiles++
			processedCount++
			if !workspaceGraphFileAllowed(file.Relative, nil, nil, gitIgnoreGlobs) {
				s.publishWorkspacePreviewProgress(taskID, startedAt, processedCount, totalFiles, "running", file.Relative)
				return
			}
			includeMatch := len(includeGlobs) == 0
			for i, pattern := range includeGlobs {
				if matchWorkspaceGlob(pattern, file.Relative) {
					includeMatch = true
					includeCounts[i]["files"] = includeCounts[i]["files"].(int) + 1
				}
			}
			excluded := false
			if includeMatch {
				for i, pattern := range excludeGlobs {
					if matchWorkspaceGlob(pattern, file.Relative) || workspaceGlobMatchesAncestor(pattern, file.Relative) {
						excluded = true
						excludeCounts[i]["files"] = excludeCounts[i]["files"].(int) + 1
					}
				}
			}
			matchesFilter := includeMatch && !excluded
			if matchesFilter {
				matchedCount++
			}
			if !matchesFilter && !*arg.ShowUnmatched {
				s.publishWorkspacePreviewProgress(taskID, startedAt, processedCount, totalFiles, "running", file.Relative)
				return
			}
			info, err := os.Lstat(file.Path)
			if err != nil || !info.Mode().IsRegular() {
				s.publishWorkspacePreviewProgress(taskID, startedAt, processedCount, totalFiles, "running", file.Relative)
				return
			}
			if matchesFilter {
				totalBytes += info.Size()
			}
			if visibleCount >= maxFiles {
				truncated = true
				s.publishWorkspacePreviewProgress(taskID, startedAt, processedCount, totalFiles, "running", file.Relative)
				return
			}
			rootFiles = append(rootFiles, map[string]any{
				"uri":           file.URI,
				"fileName":      filepath.Clean(file.Path),
				"relativePath":  file.Relative,
				"matchesFilter": matchesFilter,
				"size":          info.Size(),
				"mtimeMs":       info.ModTime().UnixMilli(),
			})
			visibleCount++
			s.publishWorkspacePreviewProgress(taskID, startedAt, processedCount, totalFiles, "running", file.Relative)
		})
		if !completed || operationCtx.Err() != nil {
			break
		}
		sort.SliceStable(rootFiles, func(i, j int) bool {
			return rootFiles[i]["relativePath"].(string) < rootFiles[j]["relativePath"].(string)
		})
		rootPath := filepath.Clean(root.Path)
		previewRoots = append(previewRoots, map[string]any{
			"uri":      filePathURI(rootPath),
			"fileName": rootPath,
			"name":     filepath.Base(rootPath),
			"files":    rootFiles,
		})
	}
	if operationCtx.Err() != nil {
		s.publishWorkspacePreviewProgress(taskID, startedAt, processedCount, totalFiles, "cancelled", "")
		return map[string]any{"ok": false, "error": "cancelled"}
	}
	s.publishWorkspacePreviewProgress(taskID, startedAt, totalFiles, totalFiles, "completed", "")
	result := map[string]any{
		"includeGlobs":     includeGlobs,
		"excludeGlobs":     excludeGlobs,
		"globStats":        globStats,
		"respectGitIgnore": *arg.RespectGitIgnore,
		"roots":            previewRoots,
		"showUnmatched":    *arg.ShowUnmatched,
		"stats":            map[string]any{"files": matchedCount, "totalBytes": totalBytes},
	}
	if truncated {
		result["truncated"] = map[string]any{"reason": "files>" + strconv.Itoa(maxFiles)}
	}
	return result
}

func (s *Server) beginWorkspacePreviewProgressTask() (string, int64) {
	return s.beginProgressTask("workspace.previewFiles", "loading", "workspace.previewFiles", "workspace.previewFiles", "", 0, true)
}

func (s *Server) publishWorkspacePreviewProgress(taskID string, startedAt int64, current, total int, state, detail string) {
	_ = startedAt
	activeItems := []string(nil)
	if detail != "" {
		activeItems = []string{detail}
	}
	s.updateProgressTask(taskID, "workspace.previewFiles", "workspace.previewFiles", detail, current, total, activeItems, state)
}

func (s *Server) publishWorkspacePreviewIdleStatus(taskID string) {
	s.finishProgressTask(taskID, "workspace.previewFiles", "")
}

func (s *Server) parsed(uri string) (*core.TextDocument, *core.ParsedDocument) {
	doc := s.documentByURI(uri)
	if doc == nil {
		return nil, nil
	}
	return doc, s.parseTextDocument(doc, s.settings.DefaultLanguage)
}
