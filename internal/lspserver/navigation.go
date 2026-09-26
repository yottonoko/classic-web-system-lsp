package lspserver

import (
	"context"
	"errors"
	"fmt"
	"html"
	"net/url"
	"os"
	"path/filepath"
	"regexp"
	"sort"
	"strconv"
	"strings"
	"time"

	"github.com/yottonoko/classic-web-system-lsp/internal/core"
	"github.com/yottonoko/classic-web-system-lsp/internal/lsp"
	workspacepkg "github.com/yottonoko/classic-web-system-lsp/internal/workspace"
)

type navigationGraphBuildResult struct {
	payload any
	err     error
}

func (s *Server) buildNavigationGraphContext(ctx context.Context, params executeCommandParams) any {
	return s.buildNavigationGraphContextResult(ctx, params).payload
}

func (s *Server) buildNavigationGraphRequest(ctx context.Context, params executeCommandParams) (any, *rpcError) {
	result := s.buildNavigationGraphContextResult(ctx, params)
	if result.err == nil {
		return result.payload, nil
	}
	if errors.Is(result.err, context.Canceled) || errors.Is(result.err, context.DeadlineExceeded) {
		return nil, requestCancelledError()
	}
	if errors.Is(result.err, errGraphCollectionGeneration) {
		return nil, &rpcError{Code: -32803, Message: "navigation graph became stale; retry the request."}
	}
	return nil, &rpcError{Code: -32603, Message: result.err.Error()}
}

func navigationGraphIncompleteError(ctx context.Context, err error) error {
	if ctx != nil && ctx.Err() != nil {
		return ctx.Err()
	}
	if err != nil {
		return err
	}
	return errGraphCollectionGeneration
}

func (s *Server) markNavigationGraphProgressFailure(taskID string) {
	s.mu.Lock()
	if task := s.progressTasks[taskID]; task != nil {
		task.State = "failed"
		task.UpdatedAt = time.Now().UnixMilli()
	}
	s.mu.Unlock()
	done := s.publishProgressTasksImmediate("navigationGraph")
	if done != nil {
		<-done
	}
}

func (s *Server) buildNavigationGraphContextResult(ctx context.Context, params executeCommandParams) navigationGraphBuildResult {
	if ctx == nil {
		ctx = context.Background()
	}
	var arg struct {
		Scope string `json:"scope"`
		URI   string `json:"uri"`
	}
	if len(params.Arguments) > 0 {
		_ = remarshal(params.Arguments[0], &arg)
	}
	if arg.Scope == "" {
		arg.Scope = "document"
	}
	if arg.Scope != "folder" && arg.Scope != "workspace" {
		arg.Scope = "document"
	}
	if arg.Scope == "workspace" {
		arg.URI = ""
	}
	if arg.URI == "" && arg.Scope == "document" {
		s.mu.Lock()
		openURIs := make([]string, 0, len(s.documents))
		for uri := range s.documents {
			openURIs = append(openURIs, uri)
		}
		s.mu.Unlock()
		sort.Strings(openURIs)
		if len(openURIs) > 0 {
			arg.URI = openURIs[0]
		}
	}
	labelPrefix := "navigationGraph." + arg.Scope
	taskID, _ := s.beginProgressTask(labelPrefix, "analyzing", "navigationGraph", labelPrefix+".collectDocuments", progressDetailForURI(arg.URI), 0, true)
	taskContext := s.registerProgressCancellationWithParent(taskID, ctx)
	ctx = taskContext
	defer s.unregisterProgressCancellation(taskID)
	state := "failed"
	defer func() {
		if taskContext.Err() != nil {
			state = "cancelled"
		}
		if state == "failed" {
			s.markNavigationGraphProgressFailure(taskID)
		}
		s.finishProgressTask(taskID, "navigationGraph", state)
	}()
	if !s.waitForDocumentOpenAnalysisContext(ctx) {
		return navigationGraphBuildResult{err: navigationGraphIncompleteError(taskContext, nil)}
	}
	report := func(label, uri string, current, total int) {
		detail := progressDetailForURI(uri)
		activeItems := []string{}
		if detail != "" {
			activeItems = append(activeItems, detail)
		}
		s.updateProgressTask(taskID, "navigationGraph", label, detail, current, total, activeItems, "running")
	}
	collection := s.navigationDocumentsContextWithProgressResult(ctx, arg.Scope, arg.URI, func(_ string, uri string, current, total int) {
		report(labelPrefix+".collectDocuments", uri, current, total)
	})
	if !collection.complete || ctx.Err() != nil {
		return navigationGraphBuildResult{err: navigationGraphIncompleteError(ctx, collection.err)}
	}
	documents := collection.documents
	owners, resolvedIncludes := navigationIncludeRelationsWithProgress(ctx, s, documents, func(_ string, uri string, current, total int) {
		report(labelPrefix+".resolveIncludes", uri, current, total)
	})
	if ctx.Err() != nil || !s.graphGenerationCurrent(ctx, collection.generation) {
		return navigationGraphBuildResult{err: navigationGraphIncompleteError(ctx, nil)}
	}
	includeExpansionIncomplete := s.navigationIncludeExpansionIncompleteContext(ctx, arg.Scope, arg.URI)
	if ctx.Err() != nil || !s.graphGenerationCurrent(ctx, collection.generation) {
		return navigationGraphBuildResult{err: navigationGraphIncompleteError(ctx, nil)}
	}
	s.mu.Lock()
	workspaceRoots := append([]workspaceRoot(nil), s.workspaceRoots...)
	if len(workspaceRoots) == 0 && s.rootPath != "" {
		workspaceRoots = []workspaceRoot{{URI: s.rootURI, Path: s.rootPath}}
	}
	s.mu.Unlock()
	builder := newNavigationGraphBuilder(arg.Scope, arg.URI, workspaceRoots)
	builder.cancelContext = ctx
	builder.javascriptCandidates = typeScriptGoNavigationCandidates{ctx: ctx}
	if includeExpansionIncomplete {
		builder.vbIncludeExpansionTruncated = true
		builder.discardIncompleteIncludeGraph()
	} else {
		if err := builder.prepareVBScriptFunctionsWithIncludesContext(ctx, documents, owners, resolvedIncludes); err != nil {
			builder.navigationError = err
			return navigationGraphBuildResult{err: err}
		}
	}
	if ctx.Err() != nil || !s.graphGenerationCurrent(ctx, collection.generation) {
		builder.navigationError = ctx.Err()
		return navigationGraphBuildResult{err: navigationGraphIncompleteError(ctx, nil)}
	}
	for index, parsed := range documents {
		if ctx.Err() != nil {
			return navigationGraphBuildResult{err: ctx.Err()}
		}
		if parsed == nil {
			report(labelPrefix+".extract", "", index+1, len(documents))
			continue
		}
		documentOwners := []string{parsed.URI}
		if arg.Scope == "document" && workspacepkg.SameFileIdentityURI(parsed.URI, arg.URI) {
			documentOwners = []string{parsed.URI}
		} else if strings.EqualFold(filepath.Ext(fileURIPath(parsed.URI)), ".inc") {
			if includeOwners := owners[workspacepkg.FileIdentityKeyFromURI(parsed.URI)]; len(includeOwners) > 0 {
				documentOwners = includeOwners
			}
		}
		for _, ownerURI := range documentOwners {
			builder.addDocument(parsed, ownerURI)
			if builder.navigationError != nil || ctx.Err() != nil {
				return navigationGraphBuildResult{err: navigationGraphIncompleteError(ctx, builder.navigationError)}
			}
		}
		report(labelPrefix+".extract", parsed.URI, index+1, len(documents))
	}
	if builder.navigationError != nil || ctx.Err() != nil || !s.graphGenerationCurrent(ctx, collection.generation) {
		return navigationGraphBuildResult{err: navigationGraphIncompleteError(ctx, builder.navigationError)}
	}
	if !s.graphGenerationCurrent(ctx, collection.generation) {
		return navigationGraphBuildResult{err: navigationGraphIncompleteError(ctx, nil)}
	}
	var payload map[string]any
	if arg.Scope == "document" {
		payload = builder.documentPayload()
	} else {
		payload = builder.payload(len(documents))
	}
	report(labelPrefix+".buildPayload", arg.URI, 1, 1)
	if ctx.Err() != nil || !s.graphGenerationCurrent(ctx, collection.generation) {
		return navigationGraphBuildResult{err: navigationGraphIncompleteError(ctx, nil)}
	}
	state = "completed"
	return navigationGraphBuildResult{payload: payload}
}

func (s *Server) navigationIncludeExpansionIncompleteContext(ctx context.Context, scope, uri string) bool {
	if scope != "document" || ctx != nil && ctx.Err() != nil {
		return false
	}
	parsed, ok := s.parsedGraphDocumentContext(ctx, uri)
	if !ok || parsed == nil || len(parsed.Includes) == 0 {
		return false
	}
	_, complete := s.vbscriptIncludeExecutionUnitsContext(ctx, parsed)
	return !complete && ctx.Err() == nil
}

func (s *Server) navigationDocumentsContextWithProgressResult(ctx context.Context, scope, uri string, report graphProgressReporter) graphDocumentCollectionResult {
	generation := s.graphGenerationSnapshot()
	switch scope {
	case "workspace":
		return s.navigationWorkspaceDocumentsContextWithProgressResult(ctx, report)
	case "folder":
		return s.navigationFolderDocumentsContextWithProgressResult(ctx, uri, report)
	default:
		parsed, ok := s.parsedGraphDocumentContext(ctx, uri)
		if !ok {
			if ctx != nil && ctx.Err() != nil {
				return incompleteGraphDocumentCollection("", generation, ctx.Err())
			}
			return completeGraphDocumentCollection(nil, uri, generation)
		}
		included, complete := s.includedDocumentsContextResult(ctx, parsed)
		if !complete || ctx.Err() != nil {
			return incompleteGraphDocumentCollection(uri, generation, ctx.Err())
		}
		documents := append([]*core.ParsedDocument{parsed}, included...)
		workspace := s.navigationWorkspaceDocumentsContextWithProgressResult(ctx, report)
		if !workspace.complete || workspace.generation != generation {
			return incompleteGraphDocumentCollection(uri, generation, workspace.err)
		}
		documents = append(workspace.documents, documents...)
		documents = dedupeParsedDocumentsByFileIdentity(documents)
		if !s.graphGenerationCurrent(ctx, generation) {
			return incompleteGraphDocumentCollection(uri, generation, errGraphCollectionGeneration)
		}
		return completeGraphDocumentCollection(documents, uri, generation)
	}
}

func (s *Server) navigationWorkspaceDocumentsContextWithProgressResult(ctx context.Context, report graphProgressReporter) graphDocumentCollectionResult {
	collection := s.workspaceGraphSourceDocumentsContext(ctx)
	if !collection.complete {
		return collection
	}
	roots := s.workspaceRootsSnapshot()
	if len(roots) == 0 {
		return collection
	}
	s.mu.Lock()
	includeGlobs := append([]string(nil), s.settings.WorkspaceIncludeGlobs...)
	excludeGlobs := append([]string(nil), s.settings.WorkspaceExcludeGlobs...)
	respectGitIgnore := s.settings.WorkspaceRespectGitIgnore
	s.mu.Unlock()
	gitIgnoreGlobs := make(map[string][]string, len(roots))
	if respectGitIgnore {
		for _, root := range roots {
			if ctx != nil && ctx.Err() != nil {
				return incompleteGraphDocumentCollection(collection.rootURI, collection.generation, ctx.Err())
			}
			if root.Path != "" {
				gitIgnoreGlobs[root.Path] = s.readGitIgnoreGlobsContext(ctx, root.Path)
			}
			if ctx != nil && ctx.Err() != nil {
				return incompleteGraphDocumentCollection(collection.rootURI, collection.generation, ctx.Err())
			}
		}
	}
	filter := newWorkspaceGraphFileFilter(includeGlobs, excludeGlobs, gitIgnoreGlobs)
	documents := collection.documents[:0]
	for index, document := range collection.documents {
		if ctx != nil && ctx.Err() != nil {
			return incompleteGraphDocumentCollection(collection.rootURI, collection.generation, ctx.Err())
		}
		if document != nil && workspaceGraphURIAllowedInRootsWithFilter(document.URI, roots, filter) {
			documents = append(documents, document)
		}
		if report != nil {
			detail := ""
			if document != nil {
				detail = document.URI
			}
			report("", detail, index+1, len(collection.documents))
		}
	}
	if !s.graphGenerationCurrent(ctx, collection.generation) {
		return incompleteGraphDocumentCollection(collection.rootURI, collection.generation, errGraphCollectionGeneration)
	}
	return completeGraphDocumentCollection(documents, collection.rootURI, collection.generation)
}

func (s *Server) parsedGraphDocumentContext(ctx context.Context, uri string) (*core.ParsedDocument, bool) {
	parsed, err := s.parsedGraphDocumentContextResult(ctx, uri)
	return parsed, err == nil && parsed != nil
}

func (s *Server) parsedGraphDocumentContextResult(ctx context.Context, uri string) (*core.ParsedDocument, error) {
	if ctx == nil {
		ctx = context.Background()
	}
	if err := ctx.Err(); err != nil {
		return nil, err
	}
	if doc := s.documentByURI(uri); doc != nil {
		return s.parseText(canonicalGraphURI(uri), doc.Text, s.settings.DefaultLanguage), nil
	}
	path := fileURIPath(uri)
	if path == "" || !s.graphSourcePathAllowed(path) {
		return nil, nil
	}
	content, err := s.readWorkspaceTextFileWithinBoundaries(ctx, path, filepath.Dir(filepath.Clean(path)))
	if err != nil {
		return nil, err
	}
	if err := ctx.Err(); err != nil {
		return nil, err
	}
	return s.parseText(canonicalGraphURI(uri), content, s.settings.DefaultLanguage), nil
}

func (s *Server) navigationFolderDocumentsContextWithProgress(ctx context.Context, uri string, report graphProgressReporter) []*core.ParsedDocument {
	result := s.navigationFolderDocumentsContextWithProgressResult(ctx, uri, report)
	if !result.complete {
		return nil
	}
	return result.documents
}

func (s *Server) navigationFolderDocumentsContextWithProgressResult(ctx context.Context, uri string, report graphProgressReporter) graphDocumentCollectionResult {
	generation := s.graphGenerationSnapshot()
	incomplete := func(err error) graphDocumentCollectionResult {
		if err == nil {
			err = errGraphCollectionGeneration
		}
		return incompleteGraphDocumentCollection(uri, generation, err)
	}
	if ctx == nil {
		ctx = context.Background()
	}
	if ctx.Err() != nil {
		return incomplete(ctx.Err())
	}
	rootPath := fileURIPath(uri)
	if rootPath == "" {
		return completeGraphDocumentCollection(nil, uri, generation)
	}
	if _, ok := s.trustedFilesystemPathContext(ctx, rootPath); !ok || ctx.Err() != nil {
		if ctx.Err() != nil {
			return incomplete(ctx.Err())
		}
		return completeGraphDocumentCollection(nil, uri, generation)
	}
	if ctx.Err() != nil {
		return incomplete(ctx.Err())
	}
	info, statErr := os.Stat(rootPath)
	if ctx.Err() != nil {
		return incomplete(ctx.Err())
	}
	if statErr == nil && !info.IsDir() {
		rootPath = filepath.Dir(rootPath)
	}
	s.mu.Lock()
	defaultLanguage := s.settings.DefaultLanguage
	workspaceRoots := append([]workspaceRoot(nil), s.workspaceRoots...)
	includeGlobs := append([]string(nil), s.settings.WorkspaceIncludeGlobs...)
	excludeGlobs := append([]string(nil), s.settings.WorkspaceExcludeGlobs...)
	respectGitIgnore := s.settings.WorkspaceRespectGitIgnore
	s.mu.Unlock()
	workspaceRootPath := workspaceRootPathForPath(rootPath, workspaceRoots)
	if ctx.Err() != nil {
		return incomplete(ctx.Err())
	}
	if workspaceRootPath == "" {
		workspaceRootPath = rootPath
	}
	gitIgnoreGlobs := []string{}
	if respectGitIgnore {
		if ctx.Err() != nil {
			return incomplete(ctx.Err())
		}
		gitIgnoreGlobs = s.readGitIgnoreGlobsContext(ctx, workspaceRootPath)
		if ctx.Err() != nil {
			return incomplete(ctx.Err())
		}
	}
	filter := newWorkspaceGraphFileFilter(includeGlobs, excludeGlobs, map[string][]string{workspaceRootPath: gitIgnoreGlobs})
	documents := make([]*core.ParsedDocument, 0)
	scan := s.scanWorkspaceFilesWithContextProgressResult(ctx, rootPath, 0, nil)
	if scan.err != nil {
		if ctx.Err() != nil {
			return incomplete(ctx.Err())
		}
		return incomplete(scan.err)
	}
	files := scan.files
	allowed := make([]bool, len(files))
	for index, file := range files {
		if ctx.Err() != nil {
			return incomplete(ctx.Err())
		}
		relative, err := filepath.Rel(workspaceRootPath, file.Path)
		if err != nil {
			return incomplete(err)
		}
		if !filter.allows(filepath.ToSlash(relative), workspaceRootPath) {
			continue
		}
		allowed[index] = true
	}
	parsedByIndex := make([]*core.ParsedDocument, len(files))
	errorsByIndex := make([]error, len(files))
	s.analysisWorkers.parallelForBulk(ctx, len(files), func(workerCtx context.Context, index int) {
		if !allowed[index] || workerCtx.Err() != nil {
			return
		}
		file := files[index]
		text, err := s.readWorkspaceTextFileWithinBoundaries(workerCtx, file.Path, workspaceRootPath)
		if err != nil {
			errorsByIndex[index] = err
			return
		}
		parsedByIndex[index] = s.parseText(file.URI, text, defaultLanguage)
	})
	for index, file := range files {
		if report != nil {
			report("", file.URI, index+1, len(files))
		}
		if errorsByIndex[index] != nil {
			if ctx.Err() != nil {
				return incomplete(ctx.Err())
			}
			return incomplete(errorsByIndex[index])
		}
		if parsedByIndex[index] != nil {
			documents = append(documents, parsedByIndex[index])
		}
	}
	if ctx.Err() != nil {
		return incomplete(ctx.Err())
	}
	if !s.graphGenerationCurrent(ctx, generation) {
		return incomplete(errGraphCollectionGeneration)
	}
	return completeGraphDocumentCollection(documents, uri, generation)
}

type navigationVBIncludeRelation struct {
	ParentURI string
	ChildURI  string
	ChildKey  string
	Offset    int
	Index     int
}

func navigationIncludeOwnersWithProgress(ctx context.Context, s *Server, documents []*core.ParsedDocument, report graphProgressReporter) map[string][]string {
	owners, _ := navigationIncludeRelationsWithProgress(ctx, s, documents, report)
	return owners
}

func navigationIncludeRelationsWithProgress(ctx context.Context, s *Server, documents []*core.ParsedDocument, report graphProgressReporter) (map[string][]string, map[string][]navigationVBIncludeRelation) {
	owners := map[string][]string{}
	relations := map[string][]navigationVBIncludeRelation{}
	seen := map[string]map[string]struct{}{}
	type includeResolution struct {
		details includeTargetDetails
		ok      bool
	}
	resolutionCache := map[string]includeResolution{}
	for index, document := range documents {
		if ctx.Err() != nil {
			return owners, relations
		}
		if document == nil {
			if report != nil {
				report("", "", index+1, len(documents))
			}
			continue
		}
		documentText := core.NewTextDocument(document.URI, "classic-asp", 0, document.Text)
		parentKey := workspacepkg.FileIdentityKeyFromURI(document.URI)
		for includeIndex, include := range document.Includes {
			if ctx.Err() != nil {
				return owners, relations
			}
			resolutionOwner := document.URI
			if !strings.EqualFold(include.Mode, "virtual") {
				if ownerPath := fileURIPath(document.URI); ownerPath != "" {
					resolutionOwner = workspacepkg.FileIdentityKeyFromFileName(filepath.Dir(ownerPath))
				}
			}
			resolutionKey := strings.ToLower(include.Mode) + "\x00" + resolutionOwner + "\x00" + include.Path
			resolution, cached := resolutionCache[resolutionKey]
			if !cached {
				resolution.details, resolution.ok = s.includeTargetDetailsForModeContext(ctx, document.URI, include.Path, include.Mode)
				resolutionCache[resolutionKey] = resolution
			}
			if ctx.Err() != nil {
				return owners, relations
			}
			if !resolution.ok || resolution.details.Path == "" {
				continue
			}
			details := resolution.details
			childURI := filePathURI(details.Path)
			key := workspacepkg.FileIdentityKeyFromURI(childURI)
			relations[parentKey] = append(relations[parentKey], navigationVBIncludeRelation{
				ParentURI: document.URI, ChildURI: childURI, ChildKey: key, Offset: documentText.OffsetAt(include.Range.Start), Index: includeIndex,
			})
			if seen[key] == nil {
				seen[key] = map[string]struct{}{}
			}
			if _, ok := seen[key][document.URI]; ok {
				continue
			}
			seen[key][document.URI] = struct{}{}
			owners[key] = append(owners[key], document.URI)
		}
		if report != nil {
			report("", document.URI, index+1, len(documents))
		}
	}
	return owners, relations
}

type navigationGraphBuilder struct {
	scope                      string
	rootURI                    string
	workspaceRoots             []workspaceRoot
	cancelContext              context.Context
	navigationError            error
	nodes                      []map[string]any
	edges                      []map[string]any
	rootNodeID                 string
	nodeByID                   map[string]map[string]any
	fileIdentityByURI          map[string]string
	sourceIDByURI              map[string]string
	materializedSourceURIByID  map[string]string
	trustedWorkspaceRoots      []trustedFilesystemRoot
	trustedWorkspaceRootsReady bool
	pendingSourceURIByID       map[string]string
	edgeByKey                  map[string]map[string]any
	rangeCursor                map[string]int
	current                    *core.ParsedDocument
	document                   *core.TextDocument
	currentOccurrence          string
	context                    *navigationCandidateContext
	javascriptCandidates       navigationJavaScriptCandidateProvider
	// vbFunctions stores definitions for each document. Effective owner scopes
	// are resolved lazily through vbFunctionOwners/vbFunctionChildren so a
	// transitive include chain does not copy every function into every owner.
	vbFunctions                 map[string]map[string]navigationVBFunction
	vbFunctionOwners            map[string][]string
	vbFunctionChildren          map[string][]string
	vbResolvedIncludes          map[string][]navigationVBIncludeRelation
	vbExecutionPrograms         map[string][]navigationVBExecutionUnit
	vbExecutionRootKeys         []string
	vbHTMLProgramsByDocument    map[string][]navigationVBHTMLProgram
	vbExecutionProcessed        map[string]bool
	vbExpressionValues          map[string][]navigationValue
	vbHTMLRendered              map[navigationVBHTMLRenderKey]struct{}
	javascriptRendered          map[string]struct{}
	javascriptDocuments         map[*core.ParsedDocument]*core.TextDocument
	javascriptRegions           map[*core.ParsedDocument][]core.Region
	javascriptContextualExtra   int
	vbIncludeExpansionTruncated bool
}

type navigationVBExecutionUnit struct {
	parsed       *core.ParsedDocument
	start        int
	end          int
	ownerURI     string
	occurrenceID string
	truncated    bool
}

// navigationValue is the language-neutral value shape used by navigation
// extractors. The VBScript evaluator owns the syntax; JavaScript workers can
// provide the same finite values through navigationJavaScriptCandidateProvider
// without coupling this package to a JavaScript parser.
type navigationValueKind int

const (
	navigationValueUnknown navigationValueKind = iota
	navigationValueLiteral
	navigationValueTemplate
)

type navigationPrimitiveKind int

const (
	navigationPrimitiveUnknown navigationPrimitiveKind = iota
	navigationPrimitiveString
	navigationPrimitiveNumber
	navigationPrimitiveBoolean
)

type navigationValue struct {
	Kind         navigationValueKind
	Primitive    navigationPrimitiveKind
	Text         string
	Parameters   []map[string]any
	Alternatives []navigationValue
}

// cloneNavigationMetadata returns an owned copy of the metadata containers
// carried through navigation values and edge extras. Metadata is treated as a
// value: later candidate/edge processing must not be able to mutate an earlier
// candidate through a shared map or slice.
func cloneNavigationMetadata(value any) any {
	switch value := value.(type) {
	case map[string]any:
		cloned := make(map[string]any, len(value))
		for key, nested := range value {
			cloned[key] = cloneNavigationMetadata(nested)
		}
		return cloned
	case []map[string]any:
		if value == nil {
			return []map[string]any(nil)
		}
		cloned := make([]map[string]any, len(value))
		for index, nested := range value {
			cloned[index], _ = cloneNavigationMetadata(nested).(map[string]any)
		}
		return cloned
	case []any:
		if value == nil {
			return []any(nil)
		}
		cloned := make([]any, len(value))
		for index, nested := range value {
			cloned[index] = cloneNavigationMetadata(nested)
		}
		return cloned
	default:
		return value
	}
}

func cloneNavigationParameterMaps(parameters []map[string]any) []map[string]any {
	if parameters == nil {
		return nil
	}
	cloned, _ := cloneNavigationMetadata(parameters).([]map[string]any)
	return cloned
}

func cloneNavigationParameter(parameter map[string]any) map[string]any {
	if parameter == nil {
		return nil
	}
	cloned, _ := cloneNavigationMetadata(parameter).(map[string]any)
	return cloned
}

func cloneNavigationValue(value navigationValue) navigationValue {
	value.Parameters = cloneNavigationParameterMaps(value.Parameters)
	if value.Alternatives != nil {
		value.Alternatives = cloneNavigationValues(value.Alternatives)
	}
	return value
}

func cloneNavigationValues(values []navigationValue) []navigationValue {
	if values == nil {
		return nil
	}
	cloned := make([]navigationValue, len(values))
	for index, value := range values {
		cloned[index] = cloneNavigationValue(value)
	}
	return cloned
}

func (value navigationValue) confidence() string {
	switch value.Kind {
	case navigationValueLiteral:
		return "certain"
	case navigationValueTemplate:
		return "possible"
	default:
		return "unknown"
	}
}

func (value navigationValue) finiteCandidates() []navigationValue {
	if len(value.Alternatives) == 0 {
		return []navigationValue{cloneNavigationValue(value)}
	}
	values := make([]navigationValue, 0, len(value.Alternatives))
	seen := map[string]struct{}{}
	appendValue := func(candidate navigationValue) {
		candidate = cloneNavigationValue(candidate)
		key := strconv.Itoa(int(candidate.Kind)) + "\x00" + strconv.Itoa(int(candidate.Primitive)) + "\x00" + candidate.Text + "\x00" + navigationParameterKey(candidate.Parameters)
		if _, ok := seen[key]; ok {
			return
		}
		seen[key] = struct{}{}
		candidate.Alternatives = nil
		values = append(values, candidate)
	}
	for _, candidate := range value.Alternatives {
		for _, nested := range candidate.finiteCandidates() {
			appendValue(nested)
		}
	}
	if len(values) == 0 {
		return []navigationValue{cloneNavigationValue(value)}
	}
	return values
}

func navigationParameterKey(parameters []map[string]any) string {
	if len(parameters) == 0 {
		return ""
	}
	parts := make([]string, 0, len(parameters))
	for _, parameter := range parameters {
		parts = append(parts, navigationString(parameter["source"])+"\x00"+navigationString(parameter["name"])+"\x00"+navigationString(parameter["value"]))
	}
	sort.Strings(parts)
	return strings.Join(parts, "\x01")
}

type navigationFiniteCandidate struct {
	Kind    string
	Value   navigationValue
	Range   lsp.Range
	Snippet string
	Extra   map[string]any
}

type navigationJavaScriptAnalyzedVariant struct {
	variant    navigationJavaScriptInterpolationVariant
	candidates []navigationFiniteCandidate
}

// navigationJavaScriptCandidateProvider is the deliberately narrow seam for
// finite values produced by the TypeScript-Go JavaScript worker. Navigation
// does not parse JavaScript here; a later adapter can return candidates with
// the same value/range contract as VBScript.
type navigationJavaScriptCandidateProvider interface {
	Candidates(parsed *core.ParsedDocument, region core.Region) ([]navigationFiniteCandidate, error)
}

type navigationContextualJavaScriptCandidateProvider interface {
	CandidatesWithReplacementsSource(parsed *core.ParsedDocument, source *core.TextDocument, region core.Region, replacements []core.VirtualDocumentReplacement, expressionRegions []core.Region) ([]navigationFiniteCandidate, error)
}

const navigationJavaScriptAlternativeEvidenceKey = "__javascriptAlternativeEvidence"
const navigationJavaScriptExpressionIndicesKey = "__javascriptExpressionIndices"
const navigationJavaScriptDependencyMarkersKey = "__javascriptDependencyMarkers"
const navigationJavaScriptFallbackRangeKey = "__javascriptFallbackRange"
const navigationJavaScriptContextualExtraAnalysisLimit = 256

type navigationCandidateContext struct {
	valueRange         *lsp.Range
	rangeValue         lsp.Range
	rangeValues        []lsp.Range
	snippet            string
	snippets           []string
	extractor          string
	confidence         string
	parameters         []map[string]any
	value              navigationValue
	additionalEvidence []map[string]any
}

func newNavigationGraphBuilder(scope, uri string, workspaceRoots []workspaceRoot) *navigationGraphBuilder {
	rootURI := canonicalGraphURI(uri)
	rootNodeID := ""
	fileIdentityByURI := map[string]string{}
	if rootURI != "" {
		rootIdentity := workspacepkg.FileIdentityKeyFromURI(rootURI)
		fileIdentityByURI[rootURI] = rootIdentity
		rootNodeID = "page:" + rootIdentity
	}
	return &navigationGraphBuilder{
		scope: scope, rootURI: rootURI, rootNodeID: rootNodeID, workspaceRoots: workspaceRoots,
		nodes: []map[string]any{}, edges: []map[string]any{}, nodeByID: map[string]map[string]any{}, edgeByKey: map[string]map[string]any{}, rangeCursor: map[string]int{},
		fileIdentityByURI: fileIdentityByURI, sourceIDByURI: map[string]string{}, materializedSourceURIByID: map[string]string{}, pendingSourceURIByID: map[string]string{},
		vbFunctions: map[string]map[string]navigationVBFunction{}, vbFunctionOwners: map[string][]string{}, vbFunctionChildren: map[string][]string{}, vbExecutionPrograms: map[string][]navigationVBExecutionUnit{}, vbHTMLProgramsByDocument: map[string][]navigationVBHTMLProgram{}, vbExecutionProcessed: map[string]bool{}, vbExpressionValues: map[string][]navigationValue{}, vbHTMLRendered: map[navigationVBHTMLRenderKey]struct{}{}, javascriptRendered: map[string]struct{}{}, javascriptDocuments: map[*core.ParsedDocument]*core.TextDocument{}, javascriptRegions: map[*core.ParsedDocument][]core.Region{},
	}
}

func (b *navigationGraphBuilder) prepareVBScriptFunctions(documents []*core.ParsedDocument, owners map[string][]string) {
	_ = b.prepareVBScriptFunctionsContext(context.Background(), documents, owners)
}

func (b *navigationGraphBuilder) prepareVBScriptFunctionsWithIncludes(documents []*core.ParsedDocument, owners map[string][]string, resolvedIncludes map[string][]navigationVBIncludeRelation) {
	_ = b.prepareVBScriptFunctionsWithIncludesContext(context.Background(), documents, owners, resolvedIncludes)
}

func (b *navigationGraphBuilder) prepareVBScriptFunctionsContext(ctx context.Context, documents []*core.ParsedDocument, owners map[string][]string) error {
	return b.prepareVBScriptFunctionsWithIncludesContext(ctx, documents, owners, nil)
}

func (b *navigationGraphBuilder) prepareVBScriptFunctionsWithIncludesContext(ctx context.Context, documents []*core.ParsedDocument, owners map[string][]string, resolvedIncludes map[string][]navigationVBIncludeRelation) error {
	if ctx == nil {
		ctx = context.Background()
	}
	if err := ctx.Err(); err != nil {
		return err
	}
	if resolvedIncludes == nil {
		var err error
		resolvedIncludes, err = navigationVBIncludeRelationsForDocumentsContext(ctx, documents)
		if err != nil {
			return err
		}
	}
	ordered := append([]*core.ParsedDocument(nil), documents...)
	sort.SliceStable(ordered, func(left, right int) bool {
		if ordered[left] == nil {
			return false
		}
		if ordered[right] == nil {
			return true
		}
		return ordered[left].URI < ordered[right].URI
	})
	if err := ctx.Err(); err != nil {
		return err
	}
	// Keep only each document's definitions in the published index. The old
	// owner-closure materialization copied every transitive definition into
	// every ancestor owner, which made a linear include chain quadratic.
	functionsByDocument := map[string]map[string]navigationVBFunction{}
	for _, parsed := range ordered {
		if err := ctx.Err(); err != nil {
			return err
		}
		if parsed == nil {
			continue
		}
		functions := navigationVBFunctionDefinitions(parsed)
		if len(functions) == 0 {
			continue
		}
		functionsByDocument[b.fileIdentityKey(parsed.URI)] = functions
	}
	functionOwners := map[string][]string{}
	appendOwner := func(childKey, parentKey string) {
		if childKey == "" || parentKey == "" {
			return
		}
		for _, existing := range functionOwners[childKey] {
			if existing == parentKey {
				return
			}
		}
		functionOwners[childKey] = append(functionOwners[childKey], parentKey)
	}
	for childURI, parentURIs := range owners {
		if err := ctx.Err(); err != nil {
			return err
		}
		childKey := b.fileIdentityKey(childURI)
		for _, parentURI := range parentURIs {
			if err := ctx.Err(); err != nil {
				return err
			}
			appendOwner(childKey, b.fileIdentityKey(parentURI))
		}
	}
	// Direct relations are also used when callers omit the legacy owners map
	// (for example, direct builder tests). Include resolution is the source of
	// truth for configured paths, repeated occurrences, and cycles.
	for parentKey, includeRelations := range resolvedIncludes {
		if err := ctx.Err(); err != nil {
			return err
		}
		parentKey = b.fileIdentityKey(parentKey)
		for _, relation := range includeRelations {
			if err := ctx.Err(); err != nil {
				return err
			}
			appendOwner(b.fileIdentityKey(relation.ChildKey), parentKey)
		}
	}
	for childKey, parentKeys := range functionOwners {
		if err := ctx.Err(); err != nil {
			return err
		}
		sort.Strings(parentKeys)
		functionOwners[childKey] = parentKeys
	}
	functionChildren := map[string][]string{}
	for childKey, parentKeys := range functionOwners {
		if err := ctx.Err(); err != nil {
			return err
		}
		for _, parentKey := range parentKeys {
			if err := ctx.Err(); err != nil {
				return err
			}
			functionChildren[parentKey] = append(functionChildren[parentKey], childKey)
		}
	}
	for parentKey, childKeys := range functionChildren {
		if err := ctx.Err(); err != nil {
			return err
		}
		sort.Strings(childKeys)
		functionChildren[parentKey] = childKeys
	}
	documentsByIdentity := make(map[string]*core.ParsedDocument, len(ordered))
	for _, parsed := range ordered {
		if err := ctx.Err(); err != nil {
			return err
		}
		if parsed == nil {
			continue
		}
		documentsByIdentity[b.fileIdentityKey(parsed.URI)] = parsed
	}
	programRoots, err := navigationVBExecutionProgramRootKeysContext(ctx, b.scope, b.rootURI, ordered, documentsByIdentity, resolvedIncludes)
	if err != nil {
		return err
	}
	executionPrograms := make(map[string][]navigationVBExecutionUnit, len(programRoots))
	includeExpansionTruncated := false
	for _, key := range programRoots {
		if err := ctx.Err(); err != nil {
			return err
		}
		parsed := documentsByIdentity[key]
		if parsed == nil {
			continue
		}
		program, err := navigationVBExecutionProgramContext(ctx, parsed, parsed.URI, documentsByIdentity, resolvedIncludes)
		if err != nil {
			return err
		}
		executionPrograms[key] = program
		for _, unit := range program {
			if unit.truncated {
				includeExpansionTruncated = true
				break
			}
		}
	}
	var htmlProgramsByDocument map[string][]navigationVBHTMLProgram
	if b.scope != "document" {
		htmlProgramsByDocument, err = navigationVBHTMLProgramsByDocumentContext(ctx, programRoots, executionPrograms)
		if err != nil {
			return err
		}
	}
	if err := ctx.Err(); err != nil {
		return err
	}
	b.vbResolvedIncludes = resolvedIncludes
	b.vbFunctions = functionsByDocument
	b.vbFunctionOwners = functionOwners
	b.vbFunctionChildren = functionChildren
	b.vbExecutionPrograms = executionPrograms
	b.vbExecutionRootKeys = programRoots
	b.vbHTMLProgramsByDocument = htmlProgramsByDocument
	if includeExpansionTruncated {
		b.vbIncludeExpansionTruncated = true
		b.discardIncompleteIncludeGraph()
	}
	return nil
}

func navigationVBExecutionProgramRootKeysContext(ctx context.Context, scope, rootURI string, ordered []*core.ParsedDocument, documents map[string]*core.ParsedDocument, resolvedIncludes map[string][]navigationVBIncludeRelation) ([]string, error) {
	if ctx == nil {
		ctx = context.Background()
	}
	if err := ctx.Err(); err != nil {
		return nil, err
	}
	childKeys := make(map[string]struct{})
	for _, relations := range resolvedIncludes {
		if err := ctx.Err(); err != nil {
			return nil, err
		}
		for _, relation := range relations {
			if err := ctx.Err(); err != nil {
				return nil, err
			}
			if _, exists := documents[relation.ChildKey]; exists {
				childKeys[relation.ChildKey] = struct{}{}
			}
		}
	}
	keys := make([]string, 0, len(documents))
	seenKeys := make(map[string]struct{}, len(documents))
	for _, parsed := range ordered {
		if err := ctx.Err(); err != nil {
			return nil, err
		}
		if parsed == nil {
			continue
		}
		key := workspacepkg.FileIdentityKeyFromURI(parsed.URI)
		if _, child := childKeys[key]; child {
			continue
		}
		if _, seen := seenKeys[key]; seen {
			continue
		}
		seenKeys[key] = struct{}{}
		keys = append(keys, key)
	}
	if len(keys) == 0 {
		for _, parsed := range ordered {
			if err := ctx.Err(); err != nil {
				return nil, err
			}
			if parsed == nil {
				continue
			}
			key := workspacepkg.FileIdentityKeyFromURI(parsed.URI)
			if _, seen := seenKeys[key]; seen {
				continue
			}
			seenKeys[key] = struct{}{}
			keys = append(keys, key)
		}
	}
	if scope == "document" {
		rootKey := workspacepkg.FileIdentityKeyFromURI(rootURI)
		if _, exists := documents[rootKey]; exists {
			if _, seen := seenKeys[rootKey]; !seen {
				keys = append(keys, rootKey)
			}
		}
	}
	sort.Strings(keys)
	return keys, nil
}

func navigationVBExecutionProgramContext(ctx context.Context, root *core.ParsedDocument, ownerURI string, documents map[string]*core.ParsedDocument, resolvedIncludes map[string][]navigationVBIncludeRelation) ([]navigationVBExecutionUnit, error) {
	if root == nil {
		return nil, nil
	}
	if ctx == nil {
		ctx = context.Background()
	}
	if err := ctx.Err(); err != nil {
		return nil, err
	}
	program := make([]navigationVBExecutionUnit, 0)
	stack := map[string]bool{}
	rootOccurrence := navigationVBRootOccurrenceID(ownerURI)
	truncatedMarkerAdded := false
	appendTruncatedMarker := func() {
		if truncatedMarkerAdded {
			return
		}
		program = append(program, navigationVBExecutionUnit{truncated: true, ownerURI: ownerURI, occurrenceID: rootOccurrence})
		truncatedMarkerAdded = true
	}
	appendUnit := func(unit navigationVBExecutionUnit) {
		if len(program) >= includeExpansionUnitBudget {
			appendTruncatedMarker()
			return
		}
		program = append(program, unit)
	}
	var appendDocument func(*core.ParsedDocument, string) error
	appendDocument = func(parsed *core.ParsedDocument, occurrenceID string) error {
		if err := ctx.Err(); err != nil {
			return err
		}
		if parsed == nil {
			return nil
		}
		key := workspacepkg.FileIdentityKeyFromURI(parsed.URI)
		if stack[key] {
			return nil
		}
		stack[key] = true
		textDocument := core.NewTextDocument(parsed.URI, "classic-asp", 0, parsed.Text)
		cursor := 0
		includes := make([]navigationVBIncludeRelation, 0)
		if resolvedIncludes != nil {
			includes = append(includes, resolvedIncludes[key]...)
		} else {
			// Direct builder tests may omit resolver output; the server graph path
			// always supplies relations resolved by includeTargetDetailsForMode.
			for includeIndex, include := range parsed.Includes {
				if err := ctx.Err(); err != nil {
					delete(stack, key)
					return err
				}
				child := navigationVBIncludedDocument(parsed, include, documents)
				if child == nil {
					continue
				}
				includes = append(includes, navigationVBIncludeRelation{
					ParentURI: parsed.URI, ChildURI: child.URI, ChildKey: workspacepkg.FileIdentityKeyFromURI(child.URI), Offset: textDocument.OffsetAt(include.Range.Start), Index: includeIndex,
				})
			}
		}
		sort.SliceStable(includes, func(left, right int) bool {
			leftOffset := includes[left].Offset
			rightOffset := includes[right].Offset
			if leftOffset != rightOffset {
				return leftOffset < rightOffset
			}
			if includes[left].Index != includes[right].Index {
				return includes[left].Index < includes[right].Index
			}
			return includes[left].ChildURI < includes[right].ChildURI
		})
		for includeIndex, include := range includes {
			if err := ctx.Err(); err != nil {
				return err
			}
			offset := include.Offset
			if offset < cursor {
				offset = cursor
			}
			if offset > len(parsed.Text) {
				offset = len(parsed.Text)
			}
			if offset > cursor {
				appendUnit(navigationVBExecutionUnit{parsed: parsed, start: cursor, end: offset, ownerURI: ownerURI, occurrenceID: occurrenceID})
			}
			if len(program) >= includeExpansionUnitBudget {
				appendTruncatedMarker()
				return nil
			}
			if child := documents[include.ChildKey]; child != nil {
				if err := appendDocument(child, navigationVBChildOccurrenceID(occurrenceID, includeIndex)); err != nil {
					delete(stack, key)
					return err
				}
			}
			cursor = offset
		}
		if cursor < len(parsed.Text) {
			appendUnit(navigationVBExecutionUnit{parsed: parsed, start: cursor, end: len(parsed.Text), ownerURI: ownerURI, occurrenceID: occurrenceID})
		}
		delete(stack, key)
		return nil
	}
	if err := appendDocument(root, rootOccurrence); err != nil {
		return nil, err
	}
	return program, nil
}

func navigationVBIncludeRelationsForDocumentsContext(ctx context.Context, documents []*core.ParsedDocument) (map[string][]navigationVBIncludeRelation, error) {
	if ctx == nil {
		ctx = context.Background()
	}
	relations := map[string][]navigationVBIncludeRelation{}
	documentsByIdentity := make(map[string]*core.ParsedDocument, len(documents))
	for _, parsed := range documents {
		if err := ctx.Err(); err != nil {
			return nil, err
		}
		if parsed != nil {
			documentsByIdentity[workspacepkg.FileIdentityKeyFromURI(parsed.URI)] = parsed
		}
	}
	for _, parsed := range documents {
		if err := ctx.Err(); err != nil {
			return nil, err
		}
		if parsed == nil {
			continue
		}
		textDocument := core.NewTextDocument(parsed.URI, "classic-asp", 0, parsed.Text)
		parentKey := workspacepkg.FileIdentityKeyFromURI(parsed.URI)
		for includeIndex, include := range parsed.Includes {
			if err := ctx.Err(); err != nil {
				return nil, err
			}
			child := navigationVBIncludedDocument(parsed, include, documentsByIdentity)
			if child == nil {
				continue
			}
			childURI := child.URI
			relations[parentKey] = append(relations[parentKey], navigationVBIncludeRelation{
				ParentURI: parsed.URI, ChildURI: childURI, ChildKey: workspacepkg.FileIdentityKeyFromURI(childURI), Offset: textDocument.OffsetAt(include.Range.Start), Index: includeIndex,
			})
		}
	}
	return relations, nil
}

func navigationVBRootOccurrenceID(ownerURI string) string {
	return workspacepkg.FileIdentityKeyFromURI(ownerURI) + "\x00root"
}

func navigationVBChildOccurrenceID(parentOccurrenceID string, index int) string {
	return parentOccurrenceID + "\x00" + strconv.Itoa(index)
}

func navigationVBIncludedDocument(parent *core.ParsedDocument, include core.Include, documents map[string]*core.ParsedDocument) *core.ParsedDocument {
	if parent == nil || include.Path == "" {
		return nil
	}
	parentPath := fileURIPath(parent.URI)
	if parentPath == "" {
		return nil
	}
	path := include.Path
	if !filepath.IsAbs(path) {
		path = filepath.Join(filepath.Dir(parentPath), path)
	}
	key := workspacepkg.FileIdentityKeyFromURI(filePathURI(filepath.Clean(path)))
	return documents[key]
}

func navigationVBFunctionOwnerClosure(uri string, owners map[string][]string) []string {
	closure, _ := navigationVBFunctionOwnerClosureContext(context.Background(), uri, owners)
	return closure
}

func navigationVBFunctionOwnerClosureContext(ctx context.Context, uri string, owners map[string][]string) ([]string, error) {
	if ctx == nil {
		ctx = context.Background()
	}
	if uri == "" {
		return nil, nil
	}
	if err := ctx.Err(); err != nil {
		return nil, err
	}
	result := make([]string, 0)
	queue := []string{uri}
	seen := map[string]struct{}{workspacepkg.FileIdentityKeyFromURI(uri): {}}
	for len(queue) > 0 {
		if err := ctx.Err(); err != nil {
			return nil, err
		}
		current := queue[0]
		queue = queue[1:]
		result = append(result, current)
		parents := append([]string(nil), owners[workspacepkg.FileIdentityKeyFromURI(current)]...)
		sort.SliceStable(parents, func(left, right int) bool {
			leftKey := workspacepkg.FileIdentityKeyFromURI(parents[left])
			rightKey := workspacepkg.FileIdentityKeyFromURI(parents[right])
			if leftKey != rightKey {
				return leftKey < rightKey
			}
			return parents[left] < parents[right]
		})
		for _, parent := range parents {
			if err := ctx.Err(); err != nil {
				return nil, err
			}
			if parent == "" {
				continue
			}
			key := workspacepkg.FileIdentityKeyFromURI(parent)
			if _, exists := seen[key]; exists {
				continue
			}
			seen[key] = struct{}{}
			queue = append(queue, parent)
		}
	}
	return result, nil
}

func (b *navigationGraphBuilder) navigationVBFunctionsForDocument(parsedURI, ownerURI string) map[string]navigationVBFunction {
	functions := make(map[string]navigationVBFunction)
	if b == nil {
		return functions
	}
	if ownerURI == "" {
		ownerURI = parsedURI
	}
	for name, function := range b.navigationVBFunctionsForOwner(ownerURI) {
		functions[name] = function
	}
	// Definitions in the fragment itself take precedence over the effective
	// owner scope, matching VBScript's local include order while keeping
	// unrelated owners isolated.
	for name, function := range b.navigationVBFunctionDefinitionsForURI(parsedURI) {
		functions[name] = function
	}
	return functions
}

func (b *navigationGraphBuilder) navigationVBFunctionDefinitionsForURI(uri string) map[string]navigationVBFunction {
	if b == nil || uri == "" {
		return nil
	}
	if definitions := b.vbFunctions[b.fileIdentityKey(uri)]; definitions != nil {
		return definitions
	}
	// Keep direct builder setup tolerant of non-canonical URI keys.
	return b.vbFunctions[uri]
}

func (b *navigationGraphBuilder) navigationVBFunctionOwnerKeys(ownerURI string) ([]string, error) {
	if b == nil || ownerURI == "" {
		return nil, nil
	}
	ownerKey := b.fileIdentityKey(ownerURI)
	if ownerKey == "" {
		return nil, nil
	}
	keys := make([]string, 0)
	queue := []string{ownerKey}
	seen := map[string]struct{}{ownerKey: {}}
	for len(queue) > 0 {
		if err := b.navigationContextError(); err != nil {
			return nil, err
		}
		current := queue[0]
		queue = queue[1:]
		keys = append(keys, current)
		for _, childKey := range b.vbFunctionChildren[current] {
			if err := b.navigationContextError(); err != nil {
				return nil, err
			}
			if _, exists := seen[childKey]; exists {
				continue
			}
			seen[childKey] = struct{}{}
			queue = append(queue, childKey)
		}
	}
	sort.Strings(keys)
	return keys, nil
}

func (b *navigationGraphBuilder) navigationVBFunctionsForOwner(ownerURI string) map[string]navigationVBFunction {
	functions := make(map[string]navigationVBFunction)
	keys, err := b.navigationVBFunctionOwnerKeys(ownerURI)
	if err != nil {
		return functions
	}
	for _, key := range keys {
		if err := b.navigationContextError(); err != nil {
			return functions
		}
		definitions := b.navigationVBFunctionDefinitionsForURI(key)
		names := make([]string, 0, len(definitions))
		for name := range definitions {
			if err := b.navigationContextError(); err != nil {
				return functions
			}
			names = append(names, name)
		}
		sort.Strings(names)
		for _, name := range names {
			if err := b.navigationContextError(); err != nil {
				return functions
			}
			if _, exists := functions[name]; !exists {
				functions[name] = definitions[name]
			}
		}
	}
	return functions
}

func (b *navigationGraphBuilder) payload(documentCount int) map[string]any {
	connected := make(map[string]bool, len(b.edges)*2)
	for _, edge := range b.edges {
		connected[navigationString(edge["source"])] = true
		connected[navigationString(edge["target"])] = true
	}
	nodes := make([]map[string]any, 0, len(b.nodes))
	for _, node := range b.nodes {
		id := navigationString(node["id"])
		includeOnly := node["kind"] == "fragment" || len(b.vbFunctionOwners[b.fileIdentityKey(navigationString(node["uri"]))]) > 0
		if !connected[id] && includeOnly && id != b.rootNodeID {
			continue
		}
		nodes = append(nodes, node)
	}
	sort.SliceStable(nodes, func(i, j int) bool {
		return navigationString(nodes[i]["label"]) < navigationString(nodes[j]["label"])
	})
	certain, probable, possible, unknown, external := 0, 0, 0, 0, 0
	for _, edge := range b.edges {
		switch edge["confidence"] {
		case "certain":
			certain++
		case "probable":
			probable++
		case "possible":
			possible++
		default:
			unknown++
		}
	}
	for _, node := range nodes {
		if node["kind"] == "external" {
			external++
		}
	}
	payload := map[string]any{
		"scope": b.scope, "uri": b.rootURI, "rootUri": b.rootURI, "nodes": nodes, "edges": b.edges,
		"stats": map[string]int{
			"documents": documentCount,
			"nodes":     len(nodes), "edges": len(b.edges), "certain": certain, "probable": probable,
			"possible": possible, "unknown": unknown, "external": external,
		},
	}
	if b.vbIncludeExpansionTruncated {
		payload["includeExpansionTruncated"] = true
	}
	if b.rootURI == "" {
		delete(payload, "rootUri")
		delete(payload, "uri")
	}
	return payload
}

func (b *navigationGraphBuilder) documentPayload() map[string]any {
	rootID := b.rootNodeID
	keptNodeIDs := map[string]struct{}{rootID: {}}
	keptEdges := make([]map[string]any, 0)
	documentURIs := map[string]struct{}{}
	if root := b.nodeByID[rootID]; root != nil && root["exists"] == true {
		if uri := navigationString(root["uri"]); uri != "" {
			documentURIs[b.fileIdentityKey(uri)] = struct{}{}
		}
	}
	for _, edge := range b.edges {
		sourceID := navigationString(edge["source"])
		targetID := navigationString(edge["target"])
		if sourceID != rootID && targetID != rootID {
			continue
		}
		keptEdges = append(keptEdges, edge)
		keptNodeIDs[sourceID] = struct{}{}
		keptNodeIDs[targetID] = struct{}{}
		for _, nodeID := range []string{sourceID, targetID} {
			node := b.nodeByID[nodeID]
			if node == nil || node["exists"] != true {
				continue
			}
			if uri := navigationString(node["uri"]); uri != "" {
				documentURIs[b.fileIdentityKey(uri)] = struct{}{}
			}
		}
		if uri := navigationString(edge["declaredInUri"]); uri != "" {
			documentURIs[b.fileIdentityKey(uri)] = struct{}{}
		}
		addEvidenceURI := func(evidence map[string]any) {
			if uri := navigationString(evidence["uri"]); uri != "" {
				documentURIs[b.fileIdentityKey(uri)] = struct{}{}
			}
		}
		switch evidence := edge["evidence"].(type) {
		case []map[string]any:
			for _, item := range evidence {
				addEvidenceURI(item)
			}
		case []any:
			for _, item := range evidence {
				if value, ok := item.(map[string]any); ok {
					addEvidenceURI(value)
				}
			}
		}
	}
	keptNodes := make([]map[string]any, 0, len(keptNodeIDs))
	for _, node := range b.nodes {
		if _, ok := keptNodeIDs[navigationString(node["id"])]; ok {
			keptNodes = append(keptNodes, node)
		}
	}
	b.nodes = keptNodes
	b.edges = keptEdges
	return b.payload(len(documentURIs))
}

// discardIncompleteIncludeGraph removes every graph artifact that could have
// come from an arbitrary include-expansion prefix. The requested root URI is
// stable metadata, so it is the only node retained for the conservative
// truncated payload.
func (b *navigationGraphBuilder) discardIncompleteIncludeGraph() {
	if b == nil {
		return
	}
	b.nodes = []map[string]any{}
	b.edges = []map[string]any{}
	b.nodeByID = map[string]map[string]any{}
	b.sourceIDByURI = map[string]string{}
	b.materializedSourceURIByID = map[string]string{}
	b.pendingSourceURIByID = map[string]string{}
	b.edgeByKey = map[string]map[string]any{}
	b.rangeCursor = map[string]int{}
	b.current = nil
	b.document = nil
	b.currentOccurrence = ""
	b.context = nil
	b.vbExecutionProcessed = map[string]bool{}
	b.vbExpressionValues = map[string][]navigationValue{}
	b.vbHTMLRendered = map[navigationVBHTMLRenderKey]struct{}{}
	b.javascriptRendered = map[string]struct{}{}
	b.javascriptDocuments = map[*core.ParsedDocument]*core.TextDocument{}
	b.javascriptRegions = map[*core.ParsedDocument][]core.Region{}
	b.javascriptContextualExtra = 0
	b.vbResolvedIncludes = nil
	b.vbFunctions = map[string]map[string]navigationVBFunction{}
	b.vbFunctionOwners = map[string][]string{}
	b.vbFunctionChildren = map[string][]string{}
	b.vbExecutionPrograms = map[string][]navigationVBExecutionUnit{}
	b.vbExecutionRootKeys = nil
	b.vbHTMLProgramsByDocument = map[string][]navigationVBHTMLProgram{}
	if b.rootURI != "" {
		b.addURINode(b.rootURI)
	}
}

func (b *navigationGraphBuilder) addDocument(parsed *core.ParsedDocument, ownerURI string) {
	if b.vbIncludeExpansionTruncated {
		b.discardIncompleteIncludeGraph()
		return
	}
	if parsed == nil {
		return
	}
	if ownerURI == "" {
		ownerURI = parsed.URI
	}
	if err := b.navigationContextError(); err != nil {
		b.navigationError = err
		return
	}
	if b.current != parsed {
		b.current = parsed
		b.document = core.NewTextDocument(parsed.URI, "classic-asp", 0, parsed.Text)
	}
	sourceID := b.navigationSourceID(ownerURI)
	if sourceID == "" {
		return
	}
	programs := b.navigationVBHTMLProgramsForDocument(parsed, ownerURI)
	if len(programs) > 0 {
		for _, program := range programs {
			if b.vbExecutionProcessed[program.key] {
				continue
			}
			b.vbExecutionProcessed[program.key] = true
			b.executeVBScriptNavigationProgram(program.program)
			if b.navigationError != nil {
				return
			}
			if b.vbIncludeExpansionTruncated {
				b.discardIncompleteIncludeGraph()
				return
			}
		}
		b.addHTMLNavigationForPrograms(parsed, programs)
		if b.navigationError != nil {
			return
		}
		b.addJavaScriptNavigationForPrograms(parsed, programs)
	} else {
		// Execute VBScript before extracting HTML interpolation targets so every
		// expression observes the same source-ordered state as redirects. The
		// execution pass records expression values for the HTML pass below.
		b.addVBScriptNavigationEdges(parsed, sourceID, ownerURI)
		if b.navigationError != nil {
			return
		}
		b.addHTMLNavigationEdges(parsed, sourceID, ownerURI)
		if b.navigationError != nil {
			return
		}
		b.addJavaScriptNavigationEdges(parsed, sourceID, ownerURI)
	}
	if err := b.navigationContextError(); err != nil {
		b.navigationError = err
		return
	}
	if err := b.navigationContextError(); err != nil {
		b.navigationError = err
		return
	}
}

type navigationVBHTMLProgram struct {
	key     string
	program []navigationVBExecutionUnit
}

type navigationVBHTMLRenderKey struct {
	programKey   string
	documentKey  string
	occurrenceID string
}

func navigationVBHTMLProgramsByDocumentContext(ctx context.Context, rootKeys []string, programs map[string][]navigationVBExecutionUnit) (map[string][]navigationVBHTMLProgram, error) {
	if ctx == nil {
		ctx = context.Background()
	}
	indexed := make(map[string][]navigationVBHTMLProgram)
	lastProgramByDocument := make(map[string]string)
	for _, key := range rootKeys {
		if err := ctx.Err(); err != nil {
			return nil, err
		}
		program, exists := programs[key]
		if !exists {
			continue
		}
		selected := navigationVBHTMLProgram{key: key, program: program}
		for _, unit := range program {
			if err := ctx.Err(); err != nil {
				return nil, err
			}
			if unit.parsed == nil {
				continue
			}
			documentKey := workspacepkg.FileIdentityKeyFromURI(unit.parsed.URI)
			if lastProgramByDocument[documentKey] == key {
				continue
			}
			lastProgramByDocument[documentKey] = key
			indexed[documentKey] = append(indexed[documentKey], selected)
		}
	}
	return indexed, nil
}

func (b *navigationGraphBuilder) navigationVBHTMLProgramsForDocument(parsed *core.ParsedDocument, ownerURI string) []navigationVBHTMLProgram {
	if b == nil || parsed == nil || len(b.vbExecutionPrograms) == 0 {
		return nil
	}
	ownerKey := b.fileIdentityKey(ownerURI)
	parsedKey := b.fileIdentityKey(parsed.URI)
	if b.scope != "document" && b.vbHTMLProgramsByDocument != nil {
		if selected := b.vbHTMLProgramsByDocument[parsedKey]; len(selected) > 0 {
			return selected
		}
		if program, exists := b.vbExecutionPrograms[ownerKey]; exists && b.navigationVBExecutionProgramContains(program, parsed) {
			return []navigationVBHTMLProgram{{key: ownerKey, program: program}}
		}
		return nil
	}
	keys := make([]string, 0, 1)
	if b.scope == "document" {
		rootKey := b.fileIdentityKey(b.rootURI)
		switch {
		case ownerKey != parsedKey:
			keys = append(keys, ownerKey)
		case ownerKey == rootKey:
			keys = append(keys, rootKey)
		case b.navigationVBExecutionProgramContains(b.vbExecutionPrograms[rootKey], parsed):
			keys = append(keys, rootKey)
		default:
			keys = append(keys, ownerKey)
		}
	} else {
		keys = b.navigationVBExecutionRootKeys()
	}
	if len(keys) == 0 {
		keys = append(keys, ownerKey)
	}
	selected := make([]navigationVBHTMLProgram, 0, len(keys))
	seen := map[string]struct{}{}
	for _, key := range keys {
		if _, exists := seen[key]; exists {
			continue
		}
		program, exists := b.vbExecutionPrograms[key]
		if !exists || !b.navigationVBExecutionProgramContains(program, parsed) {
			continue
		}
		seen[key] = struct{}{}
		selected = append(selected, navigationVBHTMLProgram{key: key, program: program})
	}
	if len(selected) == 0 {
		if program, exists := b.vbExecutionPrograms[ownerKey]; exists && b.navigationVBExecutionProgramContains(program, parsed) {
			selected = append(selected, navigationVBHTMLProgram{key: ownerKey, program: program})
		}
	}
	return selected
}

func (b *navigationGraphBuilder) navigationVBExecutionRootKeys() []string {
	if b == nil {
		return nil
	}
	if b.vbExecutionRootKeys != nil {
		return b.vbExecutionRootKeys
	}
	keys := make([]string, 0, len(b.vbExecutionPrograms))
	children := map[string]struct{}{}
	for key := range b.vbExecutionPrograms {
		keys = append(keys, key)
	}
	for _, relations := range b.vbResolvedIncludes {
		for _, relation := range relations {
			if _, exists := b.vbExecutionPrograms[relation.ChildKey]; exists {
				children[relation.ChildKey] = struct{}{}
			}
		}
	}
	roots := make([]string, 0, len(keys))
	for _, key := range keys {
		if _, child := children[key]; !child {
			roots = append(roots, key)
		}
	}
	if len(roots) == 0 {
		roots = keys
	}
	sort.Strings(roots)
	return roots
}

func (b *navigationGraphBuilder) addHTMLNavigationForPrograms(parsed *core.ParsedDocument, programs []navigationVBHTMLProgram) {
	if b == nil || parsed == nil {
		return
	}
	parsedKey := b.fileIdentityKey(parsed.URI)
	for _, selected := range programs {
		fullProgram := selected.key == parsedKey
		for _, unit := range selected.program {
			if unit.parsed == nil {
				continue
			}
			unitKey := b.fileIdentityKey(unit.parsed.URI)
			if !fullProgram && unitKey != parsedKey {
				continue
			}
			occurrenceKey := navigationVBHTMLRenderKey{programKey: selected.key, documentKey: unitKey, occurrenceID: unit.occurrenceID}
			if _, exists := b.vbHTMLRendered[occurrenceKey]; exists {
				continue
			}
			if err := b.navigationContextError(); err != nil {
				b.navigationError = err
				return
			}
			b.vbHTMLRendered[occurrenceKey] = struct{}{}
			sourceID := b.navigationSourceID(unit.ownerURI)
			if sourceID == "" {
				continue
			}
			b.addHTMLNavigationEdgesForOccurrence(unit.parsed, sourceID, unit.ownerURI, unit.occurrenceID)
			if b.navigationError != nil {
				return
			}
		}
	}
}

func (b *navigationGraphBuilder) navigationContextError() error {
	if b == nil || b.cancelContext == nil {
		return nil
	}
	return b.cancelContext.Err()
}

func (b *navigationGraphBuilder) addHTMLNavigationEdges(parsed *core.ParsedDocument, sourceID, ownerURI string) {
	b.addHTMLNavigationEdgesForOccurrence(parsed, sourceID, ownerURI, "")
}

func (b *navigationGraphBuilder) addHTMLNavigationEdgesForOccurrence(parsed *core.ParsedDocument, sourceID, ownerURI, occurrenceID string) {
	if parsed == nil {
		return
	}
	if err := b.navigationContextError(); err != nil {
		b.navigationError = err
		return
	}
	previousCurrent, previousDocument, previousOccurrence := b.current, b.document, b.currentOccurrence
	b.current = parsed
	b.document = core.NewTextDocument(parsed.URI, "classic-asp", 0, parsed.Text)
	b.currentOccurrence = occurrenceID
	defer func() {
		b.current, b.document, b.currentOccurrence = previousCurrent, previousDocument, previousOccurrence
	}()
	virtual := core.BuildVirtualDocument(parsed, core.LanguageHTML)
	text := maskHTMLNavigationExpressions(parsed, maskEmbeddedHTMLComments(virtual.Text))
	b.addHTMLNavigationEdgesFromTextFiltered(parsed.URI, sourceID, text, nil)
	if b.navigationError != nil {
		return
	}
	b.addHTMLASPNavigationEdges(parsed, sourceID, ownerURI, occurrenceID)
}

func maskHTMLNavigationExpressions(parsed *core.ParsedDocument, text string) string {
	if parsed == nil || text == "" {
		return text
	}
	masked := []byte(text)
	for _, region := range parsed.Regions {
		if region.Kind != core.RegionASPExpression || !navigationHTMLExpressionVisible(parsed, region) {
			continue
		}
		start, end := region.Start, region.End
		if start < 0 {
			start = 0
		}
		if end > len(masked) {
			end = len(masked)
		}
		if start >= end {
			continue
		}
		for index := start; index < end; index++ {
			masked[index] = 0
		}
	}
	return string(masked)
}

func (b *navigationGraphBuilder) addHTMLNavigationEdgesFromText(ownerURI string, sourceID string, text string) {
	b.addHTMLNavigationEdgesFromTextFiltered(ownerURI, sourceID, text, nil)
}

func (b *navigationGraphBuilder) addHTMLNavigationEdgesFromTextFiltered(ownerURI, sourceID, text string, include func(attrs, body string) bool) {
	tags := scanNavigationHTMLTags(text)
	byID := make(map[string]navigationHTMLTag)
	for index, tag := range tags {
		if index%64 == 0 {
			if err := b.navigationContextError(); err != nil {
				b.navigationError = err
				return
			}
		}
		if tag.Closing {
			continue
		}
		if id := htmlAttributeValue(tag.attrsText(text), "id"); id != "" {
			if _, exists := byID[id]; !exists {
				byID[id] = tag
			}
		}
	}
	formParameters := make(map[int][]map[string]any)
	parametersForForm := func(tag navigationHTMLTag) []map[string]any {
		if values, ok := formParameters[tag.Start]; ok {
			return values
		}
		values := hiddenInputParameters(tag.bodyText(text))
		formParameters[tag.Start] = values
		return values
	}
	var containingForm *navigationHTMLTag
	for _, tag := range tags {
		if containingForm != nil && tag.Start >= containingForm.BodyEnd {
			containingForm = nil
		}
		if tag.Name == "form" && !tag.Closing && tag.HasClose {
			copy := tag
			containingForm = &copy
		}
		if err := b.navigationContextError(); err != nil {
			b.navigationError = err
			return
		}
		if tag.Closing {
			continue
		}
		attrs := tag.attrsText(text)
		body := tag.bodyText(text)
		addTarget := func(attribute, kind, target string, extra map[string]any) {
			if (target == "" && kind != "htmlForm") || navigationHTMLAttributeContainsMaskedExpression(attrs, attribute) {
				return
			}
			if include != nil && !include(attrs, body) {
				return
			}
			sourceAttribute := tag.attribute(attribute)
			if sourceAttribute.Name == "" && kind == "htmlForm" {
				sourceAttribute.ValueStart, sourceAttribute.ValueEnd = tag.Start, tag.End
			}
			if evidence, ok := navigationHTMLSourceEvidence(text, b.current, sourceAttribute); ok {
				extra = cloneNavigationHTMLExtra(extra)
				extra[navigationHTMLSourceEvidenceKey] = evidence
			}
			b.addTargetEdge(b.cancelContext, ownerURI, sourceID, kind, target, extra)
			if b.navigationError != nil {
				return
			}
		}
		switch tag.Name {
		case "a", "area":
			extra := map[string]any{}
			if frame := htmlAttributeValue(attrs, "target"); frame != "" {
				extra["targetFrame"] = frame
			}
			addTarget("href", "htmlAnchor", htmlAttributeValue(attrs, "href"), extra)
		case "iframe", "frame":
			extra := map[string]any{}
			if frame := htmlAttributeValue(attrs, "name"); frame != "" {
				extra["targetFrame"] = frame
			}
			addTarget("src", "htmlFrame", htmlAttributeValue(attrs, "src"), extra)
		case "meta":
			if !strings.EqualFold(htmlAttributeValue(attrs, "http-equiv"), "refresh") {
				continue
			}
			content := htmlAttributeValue(attrs, "content")
			if target := metaRefreshTarget(content); target != "" {
				// A refresh URL is a sub-value of content, so the complete
				// encoded attribute remains the most reliable evidence span.
				addTarget("content", "metaRefresh", target, nil)
			}
		case "form":
			method := navigationHTMLFormMethod(htmlAttributeValue(attrs, "method"))
			if method == "DIALOG" {
				continue
			}
			extra := map[string]any{"method": method}
			if frame := htmlAttributeValue(attrs, "target"); frame != "" {
				extra["targetFrame"] = frame
			}
			if params := parametersForForm(tag); len(params) > 0 {
				extra["parameters"] = params
			}
			addTarget("action", "htmlForm", htmlAttributeValue(attrs, "action"), extra)
		case "button", "input":
			controlType := strings.ToLower(htmlAttributeValue(attrs, "type"))
			if tag.attribute("disabled").Name != "" || (tag.Name == "button" && (controlType == "button" || controlType == "reset")) ||
				(tag.Name == "input" && controlType != "submit" && controlType != "image") {
				continue
			}
			// A button or input can override its form target with formaction,
			// including when the control is outside a form element.
			control, ok := formActionControlFromAttributes(attrs)
			if !ok && tag.attribute("formaction").Name == "" {
				continue
			}
			if !ok {
				control.Method = strings.ToUpper(htmlAttributeValue(attrs, "formmethod"))
				control.TargetFrame = htmlAttributeValue(attrs, "formtarget")
				if name := htmlAttributeValue(attrs, "name"); name != "" {
					control.Parameter = map[string]any{"name": name, "source": "formControl"}
					if value := htmlAttributeValue(attrs, "value"); value != "" {
						control.Parameter["value"] = value
					}
				}
			}
			owner := containingForm
			if tag.attribute("form").Name != "" {
				owner = nil
				if form, exists := byID[htmlAttributeValue(attrs, "form")]; exists && form.Name == "form" {
					owner = &form
				}
			}
			var parameters []map[string]any
			if owner != nil {
				ownerAttrs := owner.attrsText(text)
				if tag.attribute("formmethod").Name == "" {
					control.Method = strings.ToUpper(htmlAttributeValue(ownerAttrs, "method"))
				}
				if tag.attribute("formtarget").Name == "" {
					control.TargetFrame = htmlAttributeValue(ownerAttrs, "target")
				}
				parameters = parametersForForm(*owner)
			}
			control.Method = navigationHTMLFormMethod(control.Method)
			if control.Method == "DIALOG" {
				continue
			}
			extra := map[string]any{"method": control.Method}
			if control.TargetFrame != "" {
				extra["targetFrame"] = control.TargetFrame
			}
			if control.Parameter != nil {
				parameters = append(parameters, control.Parameter)
			}
			if len(parameters) > 0 {
				extra["parameters"] = parameters
			}
			addTarget("formaction", "htmlForm", control.Action, extra)
		}
		if b.navigationError != nil {
			return
		}
	}
}

type navigationHTMLInterpolation struct {
	marker string
	region core.Region
	values []navigationValue
}

type navigationHTMLAttribute struct {
	Name       string
	RawValue   string
	ValueStart int
	ValueEnd   int
}

type navigationHTMLTag struct {
	Name       string
	Start      int
	End        int
	AttrsStart int
	AttrsEnd   int
	Attributes []navigationHTMLAttribute
	Closing    bool
	HasClose   bool
	BodyStart  int
	BodyEnd    int
}

const navigationHTMLSourceEvidenceKey = "_navigationHTMLSourceEvidence"

type navigationHTMLSourceSpan struct {
	Start int
	End   int
}

func cloneNavigationHTMLExtra(extra map[string]any) map[string]any {
	cloned := cloneNavigationExtra(extra)
	if cloned == nil {
		cloned = make(map[string]any, 1)
	}
	return cloned
}

func navigationHTMLSourceEvidence(text string, current *core.ParsedDocument, attribute navigationHTMLAttribute) (navigationHTMLSourceSpan, bool) {
	if current == nil || len(text) != len(current.Text) || attribute.ValueStart < 0 || attribute.ValueEnd < attribute.ValueStart || attribute.ValueEnd > len(text) {
		return navigationHTMLSourceSpan{}, false
	}
	return navigationHTMLSourceSpan{Start: attribute.ValueStart, End: attribute.ValueEnd}, true
}

func (tag navigationHTMLTag) attrsText(text string) string {
	if tag.AttrsStart < 0 || tag.AttrsEnd < tag.AttrsStart || tag.AttrsEnd > len(text) {
		return ""
	}
	return text[tag.AttrsStart:tag.AttrsEnd]
}

func (tag navigationHTMLTag) bodyText(text string) string {
	if !tag.HasClose || tag.BodyStart < 0 || tag.BodyEnd < tag.BodyStart || tag.BodyEnd > len(text) {
		return ""
	}
	return text[tag.BodyStart:tag.BodyEnd]
}

func (tag navigationHTMLTag) attribute(name string) navigationHTMLAttribute {
	name = strings.ToLower(name)
	for _, attribute := range tag.Attributes {
		if attribute.Name == name {
			return attribute
		}
	}
	return navigationHTMLAttribute{ValueStart: -1, ValueEnd: -1}
}

func scanNavigationHTMLTags(text string) []navigationHTMLTag {
	tags := make([]navigationHTMLTag, 0)
	for index := 0; index < len(text); {
		if text[index] != '<' {
			index++
			continue
		}
		if strings.HasPrefix(text[index:], "<!--") {
			if end := strings.Index(text[index+4:], "-->"); end >= 0 {
				index += end + 7
			} else {
				break
			}
			continue
		}
		if index+1 >= len(text) || text[index+1] == '!' || text[index+1] == '?' {
			if end := navigationHTMLTagEnd(text, index+1); end >= 0 {
				index = end + 1
			} else {
				break
			}
			continue
		}
		closing := text[index+1] == '/'
		nameStart := index + 1
		if closing {
			nameStart++
		}
		for nameStart < len(text) && isNavigationHTMLSpace(text[nameStart]) {
			nameStart++
		}
		nameEnd := nameStart
		for nameEnd < len(text) && isNavigationHTMLNameByte(text[nameEnd]) {
			nameEnd++
		}
		if nameEnd == nameStart {
			index++
			continue
		}
		end := navigationHTMLTagEnd(text, nameEnd)
		if end < 0 {
			break
		}
		tag := navigationHTMLTag{
			Name: strings.ToLower(text[nameStart:nameEnd]), Start: index, End: end + 1,
			AttrsStart: nameEnd, AttrsEnd: end, Closing: closing,
			BodyStart: -1, BodyEnd: -1,
		}
		if !closing {
			tag.Attributes = scanNavigationHTMLAttributes(text, nameEnd, end)
		}
		tags = append(tags, tag)
		index = end + 1
		if !closing && (tag.Name == "script" || tag.Name == "style") {
			if closeStart := findNavigationHTMLClosingTag(text, index, tag.Name); closeStart >= 0 {
				index = closeStart
			} else {
				break
			}
		}
	}
	open := map[string][]int{}
	for index := range tags {
		tag := &tags[index]
		if tag.Closing {
			stack := open[tag.Name]
			if len(stack) == 0 {
				continue
			}
			openIndex := stack[len(stack)-1]
			open[tag.Name] = stack[:len(stack)-1]
			tags[openIndex].HasClose = true
			tags[openIndex].BodyStart = tags[openIndex].End
			tags[openIndex].BodyEnd = tag.Start
			continue
		}
		if tag.Name == "form" || tag.Name == "select" || tag.Name == "textarea" {
			open[tag.Name] = append(open[tag.Name], index)
		}
	}
	return tags
}

func navigationHTMLTagEnd(text string, start int) int {
	var quote byte
	for index := start; index < len(text); index++ {
		character := text[index]
		if quote != 0 {
			if character == quote {
				quote = 0
			}
			continue
		}
		if character == '\'' || character == '"' {
			quote = character
			continue
		}
		if character == '>' {
			return index
		}
	}
	return -1
}

func findNavigationHTMLClosingTag(text string, start int, name string) int {
	lower := strings.ToLower(text[start:])
	needle := "</" + strings.ToLower(name)
	for offset := strings.Index(lower, needle); offset >= 0; {
		index := start + offset
		end := index + len(needle)
		if end >= len(text) || !isNavigationHTMLNameByte(text[end]) {
			return index
		}
		next := strings.Index(lower[offset+len(needle):], needle)
		if next < 0 {
			return -1
		}
		offset += len(needle) + next
	}
	return -1
}

func scanNavigationHTMLAttributes(text string, start, end int) []navigationHTMLAttribute {
	attributes := make([]navigationHTMLAttribute, 0)
	for index := start; index < end; {
		for index < end && (isNavigationHTMLSpace(text[index]) || text[index] == '/') {
			index++
		}
		nameStart := index
		for index < end && !isNavigationHTMLSpace(text[index]) && text[index] != '=' && text[index] != '/' {
			index++
		}
		if index == nameStart {
			index++
			continue
		}
		name := strings.ToLower(text[nameStart:index])
		for index < end && isNavigationHTMLSpace(text[index]) {
			index++
		}
		attribute := navigationHTMLAttribute{Name: name, ValueStart: -1, ValueEnd: -1}
		if index < end && text[index] == '=' {
			index++
			for index < end && isNavigationHTMLSpace(text[index]) {
				index++
			}
			if index < end && (text[index] == '\'' || text[index] == '"') {
				quote := text[index]
				index++
				valueStart := index
				for index < end && text[index] != quote {
					index++
				}
				attribute.ValueStart, attribute.ValueEnd = valueStart, index
				attribute.RawValue = text[valueStart:index]
				if index < end {
					index++
				}
			} else {
				valueStart := index
				for index < end && !isNavigationHTMLSpace(text[index]) && text[index] != '>' {
					index++
				}
				attribute.ValueStart, attribute.ValueEnd = valueStart, index
				attribute.RawValue = text[valueStart:index]
			}
		}
		attributes = append(attributes, attribute)
	}
	return attributes
}

func isNavigationHTMLSpace(character byte) bool {
	return character == ' ' || character == '\t' || character == '\r' || character == '\n' || character == '\f'
}

func isNavigationHTMLNameByte(character byte) bool {
	return character == '-' || character == ':' || character == '_' || character >= 'a' && character <= 'z' || character >= 'A' && character <= 'Z' || character >= '0' && character <= '9'
}

// addHTMLASPNavigationEdges evaluates only ASP expressions that occur in an
// HTML navigation attribute. The HTML virtual document intentionally masks
// those expressions for the HTML language service, so this path keeps the
// source text/ranges intact while supplying finite values to the graph.
func (b *navigationGraphBuilder) addHTMLASPNavigationEdges(parsed *core.ParsedDocument, sourceID, ownerURI string, occurrenceIDs ...string) {
	if parsed == nil {
		return
	}
	if err := b.navigationContextError(); err != nil {
		b.navigationError = err
		return
	}
	occurrenceID := ""
	if len(occurrenceIDs) > 0 {
		occurrenceID = occurrenceIDs[0]
	}
	virtual := core.BuildVirtualDocument(parsed, core.LanguageHTML)
	template := virtual.Text
	interpolations := make([]navigationHTMLInterpolation, 0)
	visibleRegions := make([]core.Region, 0)
	for _, region := range parsed.Regions {
		if err := b.navigationContextError(); err != nil {
			b.navigationError = err
			return
		}
		if region.Kind != core.RegionASPExpression || !navigationHTMLExpressionVisible(parsed, region) {
			continue
		}
		visibleRegions = append(visibleRegions, region)
	}
	var renderedTemplate strings.Builder
	renderedTemplate.Grow(len(template))
	cursor := 0
	for index, region := range visibleRegions {
		if err := b.navigationContextError(); err != nil {
			b.navigationError = err
			return
		}
		if region.Start < cursor || region.End < region.Start || region.End > len(template) {
			continue
		}
		renderedTemplate.WriteString(template[cursor:region.Start])
		marker := navigationHTMLInterpolationMarker(index)
		renderedTemplate.WriteString(marker)
		renderedTemplate.WriteString(template[region.End:region.End])
		cursor = region.End
		values := b.vbExpressionValuesForRegion(parsed, ownerURI, region, occurrenceID)
		if len(values) == 0 {
			values = []navigationValue{{Kind: navigationValueUnknown, Text: "{unknown}"}}
		}
		interpolations = append(interpolations, navigationHTMLInterpolation{marker: marker, region: region, values: values})
	}
	renderedTemplate.WriteString(template[cursor:])
	template = renderedTemplate.String()
	if len(interpolations) == 0 {
		return
	}
	elements, err := navigationHTMLInterpolationElementsContext(b.cancelContext, template, interpolations)
	if err != nil {
		b.navigationError = err
		return
	}
	for _, element := range elements {
		variants, err := navigationHTMLInterpolationVariantsContext(b.cancelContext, element.text, parsed.Text, element.interpolations, element.budgetExceeded)
		if err != nil {
			b.navigationError = err
			return
		}
		for _, variant := range variants {
			if variant.context == nil {
				continue
			}
			b.context = variant.context
			b.addHTMLNavigationEdgesFromTextFiltered(parsed.URI, sourceID, variant.text, nil)
			b.context = nil
			if b.navigationError != nil {
				return
			}
		}
	}
}

func navigationVBExpressionKey(ownerURI, parsedURI string, offset int, occurrenceIDs ...string) string {
	occurrenceID := ""
	if len(occurrenceIDs) > 0 {
		occurrenceID = occurrenceIDs[0]
	}
	return workspacepkg.FileIdentityKeyFromURI(ownerURI) + "\x00" + workspacepkg.FileIdentityKeyFromURI(parsedURI) + "\x00" + occurrenceID + "\x00" + strconv.Itoa(offset)
}

func (b *navigationGraphBuilder) navigationVBExpressionKey(ownerURI, parsedURI string, offset int, occurrenceIDs ...string) string {
	occurrenceID := ""
	if len(occurrenceIDs) > 0 {
		occurrenceID = occurrenceIDs[0]
	}
	return b.fileIdentityKey(ownerURI) + "\x00" + b.fileIdentityKey(parsedURI) + "\x00" + occurrenceID + "\x00" + strconv.Itoa(offset)
}

func (b *navigationGraphBuilder) vbExpressionValuesForRegion(parsed *core.ParsedDocument, ownerURI string, region core.Region, occurrenceIDs ...string) []navigationValue {
	if b == nil || parsed == nil {
		return nil
	}
	values := b.vbExpressionValues[b.navigationVBExpressionKey(ownerURI, parsed.URI, region.Start, occurrenceIDs...)]
	return cloneNavigationValues(values)
}

type navigationHTMLInterpolationElement struct {
	text           string
	interpolations []navigationHTMLInterpolation
	budgetExceeded bool
}

func navigationHTMLInterpolationElements(template string, interpolations []navigationHTMLInterpolation) []navigationHTMLInterpolationElement {
	elements, _ := navigationHTMLInterpolationElementsContext(context.Background(), template, interpolations)
	return elements
}

func navigationHTMLInterpolationElementsContext(ctx context.Context, template string, interpolations []navigationHTMLInterpolation) ([]navigationHTMLInterpolationElement, error) {
	if ctx == nil {
		ctx = context.Background()
	}
	if err := ctx.Err(); err != nil {
		return nil, err
	}
	interpolationByMarker := make(map[string]navigationHTMLInterpolation, len(interpolations))
	for _, interpolation := range interpolations {
		if err := ctx.Err(); err != nil {
			return nil, err
		}
		if interpolation.marker == "" {
			continue
		}
		interpolationByMarker[interpolation.marker] = interpolation
	}
	elements := make([]navigationHTMLInterpolationElement, 0)
	tags := scanNavigationHTMLTags(template)
	if err := ctx.Err(); err != nil {
		return nil, err
	}
	for _, tag := range tags {
		if err := ctx.Err(); err != nil {
			return nil, err
		}
		if tag.Closing {
			continue
		}
		attributeName := ""
		switch tag.Name {
		case "a", "area":
			attributeName = "href"
		case "iframe", "frame":
			attributeName = "src"
		case "meta":
			if !strings.EqualFold(htmlAttributeValue(tag.attrsText(template), "http-equiv"), "refresh") {
				continue
			}
			attributeName = "content"
		case "form":
			if !tag.HasClose {
				continue
			}
			attributeName = "action"
		case "button", "input":
			attributeName = "formaction"
		default:
			continue
		}
		rawTarget := tag.attribute(attributeName).RawValue
		local, localMarkers, budgetExceeded, err := navigationHTMLInterpolationMatchesContext(ctx, rawTarget, interpolationByMarker)
		if err != nil {
			return nil, err
		}
		if len(local) == 0 {
			continue
		}
		end := tag.End
		if tag.HasClose {
			end = tag.BodyEnd
			// Include the closing tag in the rendered candidate, matching the
			// previous form extraction while keeping the opening tag offsets.
			for end < len(template) && template[end] != '<' {
				end++
			}
			if end < len(template) {
				if closeEnd := navigationHTMLTagEnd(template, end+1); closeEnd >= 0 {
					end = closeEnd + 1
				}
			}
		}
		if end < tag.Start || end > len(template) {
			continue
		}
		candidateText := template[tag.Start:end]
		attribute := tag.attribute(attributeName)
		candidateText, err = navigationHTMLInterpolationMaskUnrelatedContext(ctx, candidateText, localMarkers, interpolationByMarker, budgetExceeded, tag.Start, attribute.ValueStart, attribute.ValueEnd)
		if err != nil {
			return nil, err
		}
		elements = append(elements, navigationHTMLInterpolationElement{text: candidateText, interpolations: local, budgetExceeded: budgetExceeded})
	}
	return elements, nil
}

// HTML interpolation expansion is intentionally bounded per navigation
// element. Once any bound is exceeded, the element is rendered through the
// unknown fallback so dynamic targets remain visible without enumerating an
// unbounded Cartesian product.
const (
	navigationHTMLInterpolationElementCountLimit = 64
	navigationHTMLInterpolationElementDepthLimit = 64
	navigationHTMLInterpolationElementWorkLimit  = 1 << 20
	navigationHTMLInterpolationMarkerPrefix      = "\x00ASP_NAV_EXPR_"
)

func navigationHTMLInterpolationMatchesContext(ctx context.Context, rawTarget string, interpolationByMarker map[string]navigationHTMLInterpolation) ([]navigationHTMLInterpolation, map[string]struct{}, bool, error) {
	if ctx == nil {
		ctx = context.Background()
	}
	local := make([]navigationHTMLInterpolation, 0, min(len(interpolationByMarker), navigationHTMLInterpolationElementCountLimit))
	localMarkers := make(map[string]struct{})
	seen := make(map[string]struct{})
	budgetExceeded := false
	if !strings.Contains(rawTarget, navigationHTMLInterpolationMarkerPrefix) {
		markers := make([]string, 0, len(interpolationByMarker))
		for marker := range interpolationByMarker {
			markers = append(markers, marker)
		}
		for offset := 0; offset < len(rawTarget); {
			if err := ctx.Err(); err != nil {
				return nil, nil, false, err
			}
			bestStart := len(rawTarget)
			bestMarker := ""
			for _, marker := range markers {
				startOffset := strings.Index(rawTarget[offset:], marker)
				if startOffset < 0 {
					continue
				}
				start := offset + startOffset
				if start < bestStart || start == bestStart && len(marker) > len(bestMarker) {
					bestStart, bestMarker = start, marker
				}
			}
			if bestMarker == "" {
				break
			}
			if _, duplicate := seen[bestMarker]; !duplicate {
				if len(local) < navigationHTMLInterpolationElementCountLimit {
					seen[bestMarker] = struct{}{}
					local = append(local, interpolationByMarker[bestMarker])
					localMarkers[bestMarker] = struct{}{}
				} else {
					budgetExceeded = true
				}
			}
			offset = bestStart + len(bestMarker)
		}
		return local, localMarkers, budgetExceeded, nil
	}
	for offset := 0; offset < len(rawTarget); {
		if err := ctx.Err(); err != nil {
			return nil, nil, false, err
		}
		relative := strings.Index(rawTarget[offset:], navigationHTMLInterpolationMarkerPrefix)
		if relative < 0 {
			break
		}
		start := offset + relative
		endOffset := strings.IndexByte(rawTarget[start+len(navigationHTMLInterpolationMarkerPrefix):], 0)
		if endOffset < 0 {
			break
		}
		end := start + len(navigationHTMLInterpolationMarkerPrefix) + endOffset + 1
		marker := rawTarget[start:end]
		if interpolation, ok := interpolationByMarker[marker]; ok {
			if _, duplicate := seen[marker]; !duplicate {
				if len(local) < navigationHTMLInterpolationElementCountLimit {
					seen[marker] = struct{}{}
					local = append(local, interpolation)
					localMarkers[marker] = struct{}{}
				} else {
					budgetExceeded = true
				}
			}
		}
		offset = end
	}
	return local, localMarkers, budgetExceeded, nil
}

func navigationHTMLInterpolationMaskUnrelatedContext(ctx context.Context, text string, localMarkers map[string]struct{}, knownMarkers map[string]navigationHTMLInterpolation, budgetExceeded bool, candidateStart, targetStart, targetEnd int) (string, error) {
	if ctx == nil {
		ctx = context.Background()
	}
	if err := ctx.Err(); err != nil {
		return "", err
	}
	if !strings.Contains(text, navigationHTMLInterpolationMarkerPrefix) {
		return navigationHTMLInterpolationMaskKnownMarkersContext(ctx, text, localMarkers, knownMarkers, budgetExceeded, candidateStart, targetStart, targetEnd)
	}
	markerIndex := strings.Index(text, navigationHTMLInterpolationMarkerPrefix)
	if markerIndex < 0 {
		return text, nil
	}
	var masked strings.Builder
	masked.Grow(len(text))
	cursor := 0
	for markerIndex >= 0 {
		if err := ctx.Err(); err != nil {
			return "", err
		}
		markerEndOffset := strings.IndexByte(text[markerIndex+len(navigationHTMLInterpolationMarkerPrefix):], 0)
		if markerEndOffset < 0 {
			break
		}
		markerEnd := markerIndex + len(navigationHTMLInterpolationMarkerPrefix) + markerEndOffset + 1
		masked.WriteString(text[cursor:markerIndex])
		marker := text[markerIndex:markerEnd]
		if _, local := localMarkers[marker]; local {
			masked.WriteString(marker)
		} else if budgetExceeded {
			absoluteMarkerStart := candidateStart + markerIndex
			if _, known := knownMarkers[marker]; !known {
				masked.WriteString(strings.Repeat(" ", len(marker)))
				cursor = markerEnd
				markerOffset := strings.Index(text[cursor:], navigationHTMLInterpolationMarkerPrefix)
				if markerOffset < 0 {
					break
				}
				markerIndex = cursor + markerOffset
				continue
			}
			if absoluteMarkerStart < targetStart || absoluteMarkerStart >= targetEnd {
				masked.WriteString(strings.Repeat(" ", len(marker)))
				cursor = markerEnd
				markerOffset := strings.Index(text[cursor:], navigationHTMLInterpolationMarkerPrefix)
				if markerOffset < 0 {
					break
				}
				markerIndex = cursor + markerOffset
				continue
			}
			// A marker beyond the per-element cap is still part of the dynamic
			// target. Keep it represented by a compact unknown value so the
			// fallback cannot accidentally turn the target into a static one.
			masked.WriteString(navigationHTMLInterpolationStart)
			masked.WriteString("{unknown}")
			masked.WriteString(navigationHTMLInterpolationEnd)
		} else {
			masked.WriteString(strings.Repeat(" ", len(marker)))
		}
		cursor = markerEnd
		markerOffset := strings.Index(text[cursor:], navigationHTMLInterpolationMarkerPrefix)
		if markerOffset < 0 {
			break
		}
		markerIndex = cursor + markerOffset
	}
	masked.WriteString(text[cursor:])
	return masked.String(), nil
}

func navigationHTMLInterpolationMaskKnownMarkersContext(ctx context.Context, text string, localMarkers map[string]struct{}, knownMarkers map[string]navigationHTMLInterpolation, budgetExceeded bool, candidateStart, targetStart, targetEnd int) (string, error) {
	markers := make([]string, 0, len(knownMarkers))
	for marker := range knownMarkers {
		markers = append(markers, marker)
	}
	var masked strings.Builder
	masked.Grow(len(text))
	cursor := 0
	for cursor < len(text) {
		if err := ctx.Err(); err != nil {
			return "", err
		}
		bestStart := len(text)
		bestMarker := ""
		for _, marker := range markers {
			if err := ctx.Err(); err != nil {
				return "", err
			}
			startOffset := strings.Index(text[cursor:], marker)
			if startOffset < 0 {
				continue
			}
			start := cursor + startOffset
			if start < bestStart || start == bestStart && len(marker) > len(bestMarker) {
				bestStart, bestMarker = start, marker
			}
		}
		if bestMarker == "" {
			masked.WriteString(text[cursor:])
			break
		}
		masked.WriteString(text[cursor:bestStart])
		absoluteMarkerStart := candidateStart + bestStart
		if _, local := localMarkers[bestMarker]; local {
			masked.WriteString(bestMarker)
		} else if budgetExceeded && absoluteMarkerStart >= targetStart && absoluteMarkerStart < targetEnd {
			masked.WriteString(navigationHTMLInterpolationStart)
			masked.WriteString("{unknown}")
			masked.WriteString(navigationHTMLInterpolationEnd)
		} else {
			masked.WriteString(strings.Repeat(" ", len(bestMarker)))
		}
		cursor = bestStart + len(bestMarker)
	}
	return masked.String(), nil
}

type navigationHTMLInterpolationVariant struct {
	text    string
	context *navigationCandidateContext
}

func navigationHTMLInterpolationVariants(template, sourceText string, interpolations []navigationHTMLInterpolation) []navigationHTMLInterpolationVariant {
	variants, _ := navigationHTMLInterpolationVariantsContext(context.Background(), template, sourceText, interpolations, false)
	return variants
}

type navigationHTMLInterpolationToken struct {
	literal       string
	interpolation int
}

func navigationHTMLInterpolationVariantsContext(ctx context.Context, template, sourceText string, interpolations []navigationHTMLInterpolation, forceFallback bool) ([]navigationHTMLInterpolationVariant, error) {
	if ctx == nil {
		ctx = context.Background()
	}
	if err := ctx.Err(); err != nil {
		return nil, err
	}
	if len(interpolations) == 0 {
		return nil, nil
	}
	tokens, err := navigationHTMLInterpolationTokensContext(ctx, template, interpolations)
	if err != nil {
		return nil, err
	}
	sourceDocument := core.NewTextDocument("", "classic-asp", 0, sourceText)
	fallback := func() ([]navigationHTMLInterpolationVariant, error) {
		metadataLimit := min(len(interpolations), navigationHTMLInterpolationElementCountLimit)
		fallbackValues := make([]navigationValue, metadataLimit)
		for index := 0; index < metadataLimit; index++ {
			if err := ctx.Err(); err != nil {
				return nil, err
			}
			fallbackValues[index] = navigationHTMLInterpolationFallbackValue(interpolations[index])
		}
		rendered, err := renderNavigationHTMLInterpolationTokens(ctx, tokens, fallbackValues, true)
		if err != nil {
			return nil, err
		}
		candidateContext := navigationHTMLInterpolationFallbackContext(interpolations, fallbackValues, metadataLimit, sourceDocument)
		if err := ctx.Err(); err != nil {
			return nil, err
		}
		if candidateContext == nil {
			return nil, nil
		}
		return []navigationHTMLInterpolationVariant{{text: rendered, context: candidateContext}}, nil
	}
	if forceFallback || len(interpolations) > navigationHTMLInterpolationElementCountLimit || len(interpolations) > navigationHTMLInterpolationElementDepthLimit {
		return fallback()
	}

	candidates := make([][]navigationValue, len(interpolations))
	truncated := false
	hardFallback := false
	for index, interpolation := range interpolations {
		if err := ctx.Err(); err != nil {
			return nil, err
		}
		values := cloneNavigationValues(interpolation.values)
		sort.SliceStable(values, func(left, right int) bool {
			if values[left].Text != values[right].Text {
				return values[left].Text < values[right].Text
			}
			return values[left].confidence() < values[right].confidence()
		})
		if len(values) > navigationHTMLInterpolationVariantLimit {
			values = values[:navigationHTMLInterpolationVariantLimit]
			truncated = true
		}
		seen := make(map[string]struct{}, len(values))
		unique := make([]navigationValue, 0, len(values))
		for _, value := range values {
			if err := ctx.Err(); err != nil {
				return nil, err
			}
			key := strconv.Itoa(int(value.Kind)) + "\x00" + value.Text + "\x00" + navigationParameterKey(value.Parameters)
			if _, ok := seen[key]; ok {
				continue
			}
			seen[key] = struct{}{}
			unique = append(unique, value)
		}
		if len(unique) == 0 {
			hardFallback = true
			break
		}
		candidates[index] = unique
	}
	if hardFallback || !navigationHTMLInterpolationWorkWithinLimit(tokens, candidates) {
		return fallback()
	}

	variants := make([]navigationHTMLInterpolationVariant, 0, navigationHTMLInterpolationVariantLimit)
	selected := make([]navigationValue, len(interpolations))
	type frame struct {
		index  int
		values []navigationValue
		next   int
	}
	stack := []frame{{index: 0, values: candidates[0]}}
	for len(stack) > 0 {
		if err := ctx.Err(); err != nil {
			return nil, err
		}
		last := len(stack) - 1
		current := &stack[last]
		if current.next >= len(current.values) {
			stack = stack[:last]
			continue
		}
		value := current.values[current.next]
		current.next++
		selected[current.index] = value
		nextIndex := current.index + 1
		if nextIndex < len(interpolations) {
			if nextIndex >= navigationHTMLInterpolationElementDepthLimit {
				truncated = true
				break
			}
			stack = append(stack, frame{index: nextIndex, values: candidates[nextIndex]})
			continue
		}
		if len(variants) >= navigationHTMLInterpolationVariantLimit {
			truncated = true
			break
		}
		rendered, err := renderNavigationHTMLInterpolationTokens(ctx, tokens, selected, false)
		if err != nil {
			return nil, err
		}
		candidateContext := navigationHTMLInterpolationContextForPath(ctx, interpolations, selected, sourceDocument)
		if err := ctx.Err(); err != nil {
			return nil, err
		}
		if candidateContext == nil {
			continue
		}
		variants = append(variants, navigationHTMLInterpolationVariant{text: rendered, context: candidateContext})
	}
	if !truncated {
		return variants, nil
	}
	if len(variants) >= navigationHTMLInterpolationVariantLimit {
		variants = variants[:navigationHTMLInterpolationVariantLimit-1]
	}
	fallbackVariants, err := fallback()
	if err != nil {
		return nil, err
	}
	if len(fallbackVariants) == 0 {
		return variants, nil
	}
	return append(variants, fallbackVariants[0]), nil
}

func navigationHTMLInterpolationTokensContext(ctx context.Context, template string, interpolations []navigationHTMLInterpolation) ([]navigationHTMLInterpolationToken, error) {
	if ctx == nil {
		ctx = context.Background()
	}
	indexByMarker := make(map[string]int, len(interpolations))
	markers := make([]string, 0, len(interpolations))
	for index, interpolation := range interpolations {
		if err := ctx.Err(); err != nil {
			return nil, err
		}
		if interpolation.marker != "" {
			indexByMarker[interpolation.marker] = index
			markers = append(markers, interpolation.marker)
		}
	}
	if !strings.Contains(template, navigationHTMLInterpolationMarkerPrefix) {
		return navigationHTMLInterpolationTokensByKnownMarkersContext(ctx, template, markers, indexByMarker)
	}
	tokens := make([]navigationHTMLInterpolationToken, 0, len(interpolations)*2+1)
	cursor := 0
	for cursor < len(template) {
		if err := ctx.Err(); err != nil {
			return nil, err
		}
		relative := strings.Index(template[cursor:], navigationHTMLInterpolationMarkerPrefix)
		if relative < 0 {
			tokens = append(tokens, navigationHTMLInterpolationToken{literal: template[cursor:], interpolation: -1})
			cursor = len(template)
			break
		}
		start := cursor + relative
		endOffset := strings.IndexByte(template[start+len(navigationHTMLInterpolationMarkerPrefix):], 0)
		if endOffset < 0 {
			tokens = append(tokens, navigationHTMLInterpolationToken{literal: template[cursor:], interpolation: -1})
			cursor = len(template)
			break
		}
		end := start + len(navigationHTMLInterpolationMarkerPrefix) + endOffset + 1
		if start > cursor {
			tokens = append(tokens, navigationHTMLInterpolationToken{literal: template[cursor:start], interpolation: -1})
		}
		marker := template[start:end]
		if index, ok := indexByMarker[marker]; ok {
			tokens = append(tokens, navigationHTMLInterpolationToken{interpolation: index})
		} else {
			tokens = append(tokens, navigationHTMLInterpolationToken{literal: marker, interpolation: -1})
		}
		cursor = end
	}
	if cursor == 0 && len(template) == 0 {
		tokens = append(tokens, navigationHTMLInterpolationToken{interpolation: -1})
	}
	return tokens, nil
}

func navigationHTMLInterpolationTokensByKnownMarkersContext(ctx context.Context, template string, markers []string, indexByMarker map[string]int) ([]navigationHTMLInterpolationToken, error) {
	if ctx == nil {
		ctx = context.Background()
	}
	tokens := make([]navigationHTMLInterpolationToken, 0, len(markers)*2+1)
	cursor := 0
	for cursor < len(template) {
		if err := ctx.Err(); err != nil {
			return nil, err
		}
		bestStart := len(template)
		bestMarker := ""
		for _, marker := range markers {
			if err := ctx.Err(); err != nil {
				return nil, err
			}
			startOffset := strings.Index(template[cursor:], marker)
			if startOffset < 0 {
				continue
			}
			start := cursor + startOffset
			if start < bestStart || start == bestStart && len(marker) > len(bestMarker) {
				bestStart, bestMarker = start, marker
			}
		}
		if bestMarker == "" {
			tokens = append(tokens, navigationHTMLInterpolationToken{literal: template[cursor:], interpolation: -1})
			break
		}
		if bestStart > cursor {
			tokens = append(tokens, navigationHTMLInterpolationToken{literal: template[cursor:bestStart], interpolation: -1})
		}
		tokens = append(tokens, navigationHTMLInterpolationToken{interpolation: indexByMarker[bestMarker]})
		cursor = bestStart + len(bestMarker)
	}
	if cursor == 0 && len(template) == 0 {
		tokens = append(tokens, navigationHTMLInterpolationToken{interpolation: -1})
	}
	return tokens, nil
}

func renderNavigationHTMLInterpolationTokens(ctx context.Context, tokens []navigationHTMLInterpolationToken, values []navigationValue, unknownMissing bool) (string, error) {
	var rendered strings.Builder
	for _, token := range tokens {
		if err := ctx.Err(); err != nil {
			return "", err
		}
		if token.interpolation < 0 {
			rendered.WriteString(token.literal)
			continue
		}
		if token.interpolation >= len(values) {
			if unknownMissing {
				rendered.WriteString(navigationHTMLInterpolationStart)
				rendered.WriteString("{unknown}")
				rendered.WriteString(navigationHTMLInterpolationEnd)
			}
			continue
		}
		value := values[token.interpolation]
		rendered.WriteString(navigationHTMLInterpolationStart)
		rendered.WriteString(value.Text)
		rendered.WriteString(navigationHTMLInterpolationEnd)
	}
	return rendered.String(), nil
}

func navigationHTMLInterpolationWorkWithinLimit(tokens []navigationHTMLInterpolationToken, candidates [][]navigationValue) bool {
	staticBytes := 0
	maxVariantBytes := 0
	for _, token := range tokens {
		if token.interpolation < 0 || token.interpolation >= len(candidates) {
			staticBytes += len(token.literal)
			if staticBytes > navigationHTMLInterpolationElementWorkLimit {
				return false
			}
			continue
		}
		maxVariantBytes += len(navigationHTMLInterpolationStart) + len(navigationHTMLInterpolationEnd)
		maxValueBytes := 0
		for _, value := range candidates[token.interpolation] {
			if len(value.Text) > maxValueBytes {
				maxValueBytes = len(value.Text)
			}
		}
		maxVariantBytes += maxValueBytes
	}
	maxVariantBytes += staticBytes
	if maxVariantBytes <= 0 || maxVariantBytes > navigationHTMLInterpolationElementWorkLimit {
		return false
	}
	combinations := 1
	for _, values := range candidates {
		if len(values) == 0 {
			return false
		}
		if combinations > navigationHTMLInterpolationVariantLimit/len(values) {
			combinations = navigationHTMLInterpolationVariantLimit
			break
		}
		combinations *= len(values)
	}
	return combinations <= navigationHTMLInterpolationElementWorkLimit/maxVariantBytes
}

const navigationHTMLInterpolationVariantLimit = 64

func navigationHTMLInterpolationFallbackValue(interpolation navigationHTMLInterpolation) navigationValue {
	var parameters []map[string]any
	for _, value := range interpolation.values {
		parameters = mergeNavigationParameterMaps(parameters, value.Parameters)
	}
	return navigationValue{Kind: navigationValueUnknown, Text: "{unknown}", Parameters: parameters}
}

func navigationHTMLInterpolationContextForPath(ctx context.Context, interpolations []navigationHTMLInterpolation, values []navigationValue, document *core.TextDocument) *navigationCandidateContext {
	var candidateContext *navigationCandidateContext
	for index, interpolation := range interpolations {
		if ctx != nil {
			if err := ctx.Err(); err != nil {
				return nil
			}
		}
		if index >= len(values) {
			break
		}
		candidateContext = appendNavigationHTMLInterpolationContextWithDocument(candidateContext, interpolation, values[index], document)
	}
	return candidateContext
}

func navigationHTMLInterpolationFallbackContext(interpolations []navigationHTMLInterpolation, values []navigationValue, metadataLimit int, document *core.TextDocument) *navigationCandidateContext {
	if len(interpolations) == 0 || metadataLimit <= 0 {
		return nil
	}
	if metadataLimit > len(interpolations) {
		metadataLimit = len(interpolations)
	}
	if metadataLimit > len(values) {
		metadataLimit = len(values)
	}
	if metadataLimit == 0 {
		return nil
	}
	firstRange, firstSnippet := navigationHTMLInterpolationRange(interpolations[0], document)
	candidateContext := &navigationCandidateContext{
		rangeValue:  firstRange,
		rangeValues: make([]lsp.Range, 0, metadataLimit),
		snippet:     firstSnippet,
		snippets:    make([]string, 0, metadataLimit),
		extractor:   "vbscript",
		confidence:  "unknown",
		value:       navigationValue{Kind: navigationValueUnknown, Text: "{unknown}"},
	}
	for index := 0; index < metadataLimit; index++ {
		interpolation := interpolations[index]
		value := values[index]
		rangeValue, snippet := navigationHTMLInterpolationRange(interpolation, document)
		candidateContext.rangeValues = append(candidateContext.rangeValues, rangeValue)
		candidateContext.snippets = append(candidateContext.snippets, snippet)
		candidateContext.confidence = lowerNavigationConfidenceString(candidateContext.confidence, value.confidence())
		candidateContext.parameters = mergeNavigationParameterMaps(candidateContext.parameters, value.Parameters)
	}
	candidateContext.value.Parameters = cloneNavigationParameterMaps(candidateContext.parameters)
	return candidateContext
}

func navigationHTMLInterpolationRange(interpolation navigationHTMLInterpolation, document *core.TextDocument) (lsp.Range, string) {
	if document == nil || interpolation.region.Start < 0 || interpolation.region.End < interpolation.region.Start || interpolation.region.End > len(document.Text) {
		return lsp.Range{}, ""
	}
	rangeValue := document.Range(interpolation.region.Start, interpolation.region.End)
	snippet := strings.TrimSpace(strings.ReplaceAll(document.Text[interpolation.region.Start:interpolation.region.End], "\n", " "))
	return rangeValue, snippet
}

func appendNavigationHTMLInterpolationContextWithDocument(context *navigationCandidateContext, interpolation navigationHTMLInterpolation, value navigationValue, document *core.TextDocument) *navigationCandidateContext {
	rangeValue, snippet := navigationHTMLInterpolationRange(interpolation, document)
	if context == nil {
		return &navigationCandidateContext{
			rangeValue: rangeValue, rangeValues: []lsp.Range{rangeValue}, snippet: snippet, snippets: []string{snippet}, extractor: "vbscript",
			confidence: value.confidence(), parameters: cloneNavigationParameterMaps(value.Parameters), value: cloneNavigationValue(value),
		}
	}
	merged := *context
	merged.rangeValue = context.rangeValue
	merged.rangeValues = append(append([]lsp.Range(nil), context.rangeValues...), rangeValue)
	if len(merged.rangeValues) == 0 {
		merged.rangeValues = []lsp.Range{context.rangeValue, rangeValue}
	}
	merged.snippets = append(append([]string(nil), context.snippets...), snippet)
	if len(merged.snippets) == 0 {
		merged.snippets = []string{context.snippet, snippet}
	}
	merged.confidence = lowerNavigationConfidenceString(context.confidence, value.confidence())
	merged.parameters = mergeNavigationParameterMaps(context.parameters, value.Parameters)
	merged.value = combineNavigationVBValues([]navigationValue{context.value, value})
	return &merged
}

func navigationHTMLInterpolationMarker(index int) string {
	return navigationHTMLInterpolationMarkerPrefix + strconv.Itoa(index) + "\x00"
}

const (
	navigationHTMLInterpolationStart = "\x00ASP_NAV_VALUE\x00"
	navigationHTMLInterpolationEnd   = "\x00/ASP_NAV_VALUE\x00"
)

func navigationHTMLAttributeContainsMaskedExpression(attrs, name string) bool {
	raw := navigationStripHTMLInterpolationMarkers(htmlAttributeRawValue(attrs, name))
	return strings.Contains(raw, "\x00")
}

func navigationHTMLExpressionVisible(parsed *core.ParsedDocument, expression core.Region) bool {
	for _, region := range parsed.Regions {
		if region == expression || expression.Start < region.ContentStart || expression.End > region.ContentEnd {
			continue
		}
		if region.Kind == core.RegionStyle || region.Kind == core.RegionStyleAttribute || region.Kind == core.RegionClientScript || region.Kind == core.RegionServerScript {
			return false
		}
	}
	return true
}

func navigationStripHTMLInterpolationMarkers(value string) string {
	value = strings.ReplaceAll(value, navigationHTMLInterpolationStart, "")
	return strings.ReplaceAll(value, navigationHTMLInterpolationEnd, "")
}

// addJavaScriptNavigationForPrograms extracts each embedded JavaScript region
// once for each textual include occurrence in the VB execution program. The
// same include document can therefore contribute multiple pieces of evidence
// while retaining the owner/source semantics of the occurrence that rendered
// it.
func (b *navigationGraphBuilder) addJavaScriptNavigationForPrograms(parsed *core.ParsedDocument, programs []navigationVBHTMLProgram) {
	if b == nil || parsed == nil {
		return
	}
	parsedKey := b.fileIdentityKey(parsed.URI)
	for _, selected := range programs {
		if b.addJavaScriptNavigationProgram(selected) {
			if b.navigationError != nil {
				return
			}
			continue
		}
		fullProgram := selected.key == parsedKey
		for _, unit := range selected.program {
			if err := b.navigationContextError(); err != nil {
				b.navigationError = err
				return
			}
			if unit.parsed == nil || unit.truncated {
				continue
			}
			unitKey := b.fileIdentityKey(unit.parsed.URI)
			if !fullProgram && unitKey != parsedKey {
				continue
			}
			if len(b.javascriptRegionsForRange(unit.parsed, unit.start, unit.end)) == 0 {
				if b.navigationError != nil {
					return
				}
				continue
			}
			key := strings.Join([]string{
				selected.key,
				unitKey,
				unit.occurrenceID,
				strconv.Itoa(unit.start),
				strconv.Itoa(unit.end),
			}, "\x00")
			if _, exists := b.javascriptRendered[key]; exists {
				continue
			}
			b.javascriptRendered[key] = struct{}{}
			sourceID := b.navigationSourceID(unit.ownerURI)
			if sourceID == "" {
				continue
			}
			b.addJavaScriptNavigationEdgesForOccurrence(unit.parsed, sourceID, unit.ownerURI, unit.occurrenceID, unit.start, unit.end)
			if b.navigationError != nil {
				return
			}
		}
	}
}

func (b *navigationGraphBuilder) addJavaScriptNavigationEdgesForOccurrence(parsed *core.ParsedDocument, sourceID, ownerURI, occurrenceID string, start, end int) {
	if b == nil || parsed == nil {
		return
	}
	if start < 0 {
		start = 0
	}
	if end > len(parsed.Text) {
		end = len(parsed.Text)
	}
	if end <= start {
		return
	}
	regions := b.javascriptRegionsForRange(parsed, start, end)
	if b.navigationError != nil || len(regions) == 0 {
		return
	}
	ctx := b.cancelContext
	if ctx == nil {
		ctx = context.Background()
	}
	document := b.javascriptDocuments[parsed]
	if document == nil {
		var err error
		document, err = core.NewTextDocumentContext(ctx, parsed.URI, "classic-asp", 0, parsed.Text)
		if err != nil {
			b.navigationError = err
			return
		}
		if b.javascriptDocuments == nil {
			b.javascriptDocuments = map[*core.ParsedDocument]*core.TextDocument{}
		}
		b.javascriptDocuments[parsed] = document
	}
	previousCurrent, previousDocument, previousOccurrence := b.current, b.document, b.currentOccurrence
	b.current = parsed
	b.document = document
	b.currentOccurrence = occurrenceID
	defer func() {
		b.current, b.document, b.currentOccurrence = previousCurrent, previousDocument, previousOccurrence
	}()
	for _, region := range regions {
		if err := b.navigationContextError(); err != nil {
			b.navigationError = err
			return
		}
		if b.javascriptCandidates != nil {
			if contextual, ok := b.javascriptCandidates.(navigationContextualJavaScriptCandidateProvider); ok {
				valuesByOffset := make(map[int][]navigationValue)
				for _, expression := range navigationJavaScriptNestedExpressions(parsed, region) {
					valuesByOffset[expression.Start] = b.vbExpressionValuesForRegion(parsed, ownerURI, expression, occurrenceID)
				}
				variants, err := navigationJavaScriptInterpolationVariantsContext(b.cancelContext, parsed, region, valuesByOffset)
				if err != nil {
					b.navigationError = err
					return
				}
				if len(variants) > 0 {
					variants, err = b.limitJavaScriptInterpolationVariants(parsed, region, valuesByOffset, variants)
					if err != nil {
						b.navigationError = err
						return
					}
					if len(variants) > 0 {
						if len(variants) == 1 && variants[0].budgetFallback {
							b.addJavaScriptBudgetFallbackEdges(parsed, sourceID, region, variants[0])
							continue
						}
						dependencyVariant, dependencyMarkerPrefix, dependencyMarkerCount, err := navigationJavaScriptInterpolationDependencyVariantContext(b.cancelContext, parsed, region)
						if err != nil {
							b.navigationError = err
							return
						}
						dependencyCandidates, err := contextual.CandidatesWithReplacementsSource(parsed, b.document, region, dependencyVariant.replacements, dependencyVariant.regions)
						if err != nil {
							b.navigationError = err
							return
						}
						dependencies := navigationJavaScriptCandidateDependencies(dependencyCandidates, dependencyMarkerPrefix, dependencyMarkerCount)
						analyzed := make([]navigationJavaScriptAnalyzedVariant, 0, len(variants))
						for _, variant := range variants {
							candidates, err := contextual.CandidatesWithReplacementsSource(parsed, b.document, region, variant.replacements, variant.regions)
							if err != nil {
								b.navigationError = err
								return
							}
							analyzed = append(analyzed, navigationJavaScriptAnalyzedVariant{variant: variant, candidates: candidates})
						}
						if err := navigationJavaScriptInferVariantDependencies(b.cancelContext, analyzed, dependencyCandidates, dependencies); err != nil {
							b.navigationError = err
							return
						}
						emitted := make(map[string]struct{})
						for index := range analyzed {
							analysis := &analyzed[index]
							navigationJavaScriptAttachCandidateDependencies(analysis.candidates, dependencies)
							b.addJavaScriptFiniteCandidates(parsed, sourceID, analysis.candidates, &analysis.variant, emitted)
							if b.navigationError != nil {
								return
							}
						}
						continue
					}
				}
			}
			candidates, err := b.javascriptCandidates.Candidates(parsed, region)
			if err != nil {
				b.navigationError = err
				return
			}
			b.addJavaScriptFiniteCandidates(parsed, sourceID, candidates, nil, nil)
			if b.navigationError != nil {
				return
			}
			continue
		}
		virtual := core.BuildEmbeddedRegionVirtualDocument(parsed, region)
		script := maskEmbeddedJavaScriptComments(virtual.Text)
		values := javascriptStringAssignments(script)
		for _, match := range navigationLocationPattern.FindAllStringSubmatch(script, -1) {
			target, dynamic := javascriptNavigationValue(match[1], values)
			b.addTargetEdge(b.cancelContext, parsed.URI, sourceID, "javascriptLocation", target, navigationJavascriptExtra(dynamic))
		}
		for _, match := range navigationLocationCallPattern.FindAllStringSubmatch(script, -1) {
			target, dynamic := javascriptNavigationValue(match[1], values)
			b.addTargetEdge(b.cancelContext, parsed.URI, sourceID, "javascriptLocation", target, navigationJavascriptExtra(dynamic))
		}
		for _, match := range navigationWindowOpenPattern.FindAllStringSubmatch(script, -1) {
			target, dynamic := javascriptNavigationValue(match[1], values)
			extra := navigationJavascriptExtra(dynamic)
			if len(match) > 2 && match[2] != "" {
				extra["targetFrame"] = match[2]
			}
			b.addTargetEdge(b.cancelContext, parsed.URI, sourceID, "javascriptLocation", target, extra)
		}
		for _, match := range navigationHistoryPattern.FindAllStringSubmatch(script, -1) {
			b.addTargetEdge(b.cancelContext, parsed.URI, sourceID, "javascriptHistory", match[1], map[string]any{"confidence": "probable"})
		}
		formActions := navigationFormActionPattern.FindAllStringSubmatch(script, -1)
		if len(formActions) == 0 {
			continue
		}
		formMethods := javascriptFormMethods(script)
		submittedForms := javascriptSubmittedForms(script)
		for _, match := range formActions {
			varName := match[1]
			target := match[2]
			method := "GET"
			if configured := formMethods[strings.ToLower(varName)]; configured != "" {
				method = configured
			}
			if _, submitted := submittedForms[strings.ToLower(varName)]; submitted {
				b.addTargetEdge(b.cancelContext, parsed.URI, sourceID, "javascriptFormSubmit", target, map[string]any{"method": method, "confidence": "probable"})
			}
		}
	}
}

func (b *navigationGraphBuilder) javascriptRegionsForRange(parsed *core.ParsedDocument, start, end int) []core.Region {
	if b == nil || parsed == nil {
		return nil
	}
	if b.javascriptRegions == nil {
		b.javascriptRegions = map[*core.ParsedDocument][]core.Region{}
	}
	regions, ok := b.javascriptRegions[parsed]
	if !ok {
		regions = make([]core.Region, 0)
		for index, region := range parsed.Regions {
			if index&255 == 0 {
				if err := b.navigationContextError(); err != nil {
					b.navigationError = err
					return nil
				}
			}
			if region.Language == core.LanguageJavaScript || region.Language == core.LanguageJScript {
				regions = append(regions, region)
			}
		}
		sort.SliceStable(regions, func(left, right int) bool { return regions[left].Start < regions[right].Start })
		b.javascriptRegions[parsed] = regions
	}
	if start < 0 {
		start = 0
	}
	if end > len(parsed.Text) {
		end = len(parsed.Text)
	}
	if end <= start {
		return nil
	}
	first := sort.Search(len(regions), func(index int) bool { return regions[index].Start >= start })
	last := sort.Search(len(regions), func(index int) bool { return regions[index].Start >= end })
	return regions[first:last]
}

func navigationJavaScriptCandidateDependencies(candidates []navigationFiniteCandidate, markerPrefix string, markerCount int) map[string][]int {
	dependencies := make(map[string][]int, len(candidates))
	for _, candidate := range candidates {
		seen := make(map[int]struct{})
		if markers, ok := candidate.Extra[navigationJavaScriptDependencyMarkersKey].([]string); ok {
			for _, marker := range markers {
				for _, index := range navigationJavaScriptDependencyMarkerIndices(marker, markerPrefix, markerCount) {
					seen[index] = struct{}{}
				}
			}
		}
		for _, value := range candidate.Value.finiteCandidates() {
			for _, index := range navigationJavaScriptDependencyMarkerIndices(value.Text, markerPrefix, markerCount) {
				seen[index] = struct{}{}
			}
		}
		indices := make([]int, 0, len(seen))
		for index := range seen {
			indices = append(indices, index)
		}
		sort.Ints(indices)
		dependencies[navigationJavaScriptCandidateSinkKey(candidate)] = indices
	}
	return dependencies
}

func navigationJavaScriptDependencyMarkerIndices(text, markerPrefix string, markerCount int) []int {
	indices := make([]int, 0)
	for cursor := 0; cursor < len(text); {
		relative := strings.Index(text[cursor:], markerPrefix)
		if relative < 0 {
			break
		}
		start := cursor + relative + len(markerPrefix)
		endOffset := strings.IndexByte(text[start:], 0)
		if endOffset < 0 {
			break
		}
		end := start + endOffset
		index, err := strconv.Atoi(text[start:end])
		if err == nil && index >= 0 && index < markerCount {
			indices = append(indices, index)
		}
		cursor = end + 1
	}
	return indices
}

func navigationJavaScriptAttachCandidateDependencies(candidates []navigationFiniteCandidate, dependencies map[string][]int) {
	for index := range candidates {
		candidate := &candidates[index]
		inferred := dependencies[navigationJavaScriptCandidateSinkKey(*candidate)]
		if len(inferred) == 0 {
			if fallback, ok := candidate.Extra[navigationJavaScriptFallbackRangeKey].(lsp.Range); ok {
				candidate.Range = fallback
			}
		}
		delete(candidate.Extra, "javascriptExpressionRanges")
		delete(candidate.Extra, "javascriptExpressionSnippets")
		candidate.Extra[navigationJavaScriptExpressionIndicesKey] = append([]int(nil), inferred...)
	}
}

func navigationJavaScriptInferVariantDependencies(ctx context.Context, analyzed []navigationJavaScriptAnalyzedVariant, dependencyCandidates []navigationFiniteCandidate, dependencies map[string][]int) error {
	if ctx == nil {
		ctx = context.Background()
	}
	byVariant := make([]map[string]string, len(analyzed))
	for index, analysis := range analyzed {
		if err := ctx.Err(); err != nil {
			return err
		}
		byVariant[index] = navigationJavaScriptCandidateSignatures(analysis.candidates)
	}
	for left := 0; left < len(analyzed); left++ {
		for right := left + 1; right < len(analyzed); right++ {
			if err := ctx.Err(); err != nil {
				return err
			}
			changedIndex := -1
			for index := range analyzed[left].variant.values {
				if index >= len(analyzed[right].variant.values) || navigationJavaScriptInterpolationValueKey(analyzed[left].variant.values[index]) != navigationJavaScriptInterpolationValueKey(analyzed[right].variant.values[index]) {
					if changedIndex >= 0 {
						changedIndex = -2
						break
					}
					changedIndex = index
				}
			}
			if changedIndex < 0 {
				continue
			}
			keys := make(map[string]struct{}, len(byVariant[left])+len(byVariant[right]))
			for key := range byVariant[left] {
				keys[key] = struct{}{}
			}
			for key := range byVariant[right] {
				keys[key] = struct{}{}
			}
			for key := range keys {
				if byVariant[left][key] != byVariant[right][key] {
					dependencies[key] = navigationJavaScriptMergeDependencyIndices(dependencies[key], []int{changedIndex})
				}
			}
		}
	}
	if len(analyzed) == 1 {
		dependencySignatures := navigationJavaScriptCandidateSignatures(dependencyCandidates)
		actualSignatures := byVariant[0]
		all := make([]int, len(analyzed[0].variant.values))
		for index := range all {
			all[index] = index
		}
		for key, signature := range actualSignatures {
			if signature != dependencySignatures[key] && len(dependencies[key]) == 0 {
				dependencies[key] = append([]int(nil), all...)
			}
		}
	}
	return nil
}

func navigationJavaScriptCandidateSignatures(candidates []navigationFiniteCandidate) map[string]string {
	signatures := make(map[string]string, len(candidates))
	for _, candidate := range candidates {
		values := candidate.Value.finiteCandidates()
		parts := make([]string, 0, len(values))
		for _, value := range values {
			parts = append(parts, navigationJavaScriptInterpolationValueKey(value))
		}
		sort.Strings(parts)
		signatures[navigationJavaScriptCandidateSinkKey(candidate)] = strings.Join(parts, "\x01")
	}
	return signatures
}

func navigationJavaScriptInterpolationValueKey(value navigationValue) string {
	return strconv.Itoa(int(value.Kind)) + "\x00" + strconv.Itoa(int(value.Primitive)) + "\x00" + value.Text + "\x00" + navigationParameterKey(value.Parameters)
}

func navigationJavaScriptMergeDependencyIndices(left, right []int) []int {
	seen := make(map[int]struct{}, len(left)+len(right))
	merged := make([]int, 0, len(left)+len(right))
	for _, index := range append(append([]int(nil), left...), right...) {
		if _, ok := seen[index]; ok {
			continue
		}
		seen[index] = struct{}{}
		merged = append(merged, index)
	}
	sort.Ints(merged)
	return merged
}

func navigationJavaScriptCandidateSinkKey(candidate navigationFiniteCandidate) string {
	parts := []string{
		candidate.Kind,
		strconv.Itoa(candidate.Range.Start.Line),
		strconv.Itoa(candidate.Range.Start.Character),
		strconv.Itoa(candidate.Range.End.Line),
		strconv.Itoa(candidate.Range.End.Character),
		navigationString(candidate.Extra["method"]),
		navigationString(candidate.Extra["targetFrame"]),
	}
	return strings.Join(parts, "\x00")
}

func (b *navigationGraphBuilder) limitJavaScriptInterpolationVariants(parsed *core.ParsedDocument, region core.Region, valuesByOffset map[int][]navigationValue, variants []navigationJavaScriptInterpolationVariant) ([]navigationJavaScriptInterpolationVariant, error) {
	remaining := navigationJavaScriptContextualExtraAnalysisLimit - b.javascriptContextualExtra
	if remaining <= 1 {
		fallback, ok, err := navigationJavaScriptUnknownInterpolationVariantForRegion(b.cancelContext, parsed, region, valuesByOffset)
		if err != nil || !ok {
			return nil, err
		}
		fallback.budgetFallback = true
		b.javascriptContextualExtra = navigationJavaScriptContextualExtraAnalysisLimit
		return []navigationJavaScriptInterpolationVariant{fallback}, nil
	}
	if len(variants)+1 <= remaining {
		b.javascriptContextualExtra += len(variants) + 1
		return variants, nil
	}
	fallback, ok, err := navigationJavaScriptUnknownInterpolationVariantForRegion(b.cancelContext, parsed, region, valuesByOffset)
	if err != nil {
		return nil, err
	}
	if !ok {
		return variants[:1], nil
	}
	variantSlots := remaining - 1
	limited := make([]navigationJavaScriptInterpolationVariant, 0, variantSlots)
	if variantSlots > 1 {
		limited = append(limited, variants[:variantSlots-1]...)
	}
	limited = append(limited, fallback)
	b.javascriptContextualExtra += len(limited) + 1
	return limited, nil
}

func (b *navigationGraphBuilder) addJavaScriptFiniteCandidates(parsed *core.ParsedDocument, sourceID string, candidates []navigationFiniteCandidate, variant *navigationJavaScriptInterpolationVariant, emitted map[string]struct{}) {
	for _, candidate := range candidates {
		if err := b.navigationContextError(); err != nil {
			b.navigationError = err
			return
		}
		interpolated := []navigationValue(nil)
		dependencyIndices := []int(nil)
		multipleValues := false
		if variant != nil {
			if indices, ok := candidate.Extra[navigationJavaScriptExpressionIndicesKey].([]int); ok {
				dependencyIndices = indices
				interpolated = make([]navigationValue, 0, len(indices))
				for _, index := range indices {
					if index < 0 || index >= len(variant.values) {
						continue
					}
					interpolated = append(interpolated, variant.values[index])
					if index < len(variant.ambiguous) && variant.ambiguous[index] {
						multipleValues = true
					}
				}
			}
		}
		values := candidate.Value.finiteCandidates()
		for _, value := range values {
			duplicateAlternative := false
			if emitted != nil {
				key := navigationJavaScriptCandidateEmissionKey(candidate, value)
				if _, exists := emitted[key]; exists {
					duplicateAlternative = true
				} else {
					emitted[key] = struct{}{}
				}
			}
			contextValue := cloneNavigationValue(value)
			parameters := cloneNavigationParameterMaps(value.Parameters)
			confidence := value.confidence()
			interpolationUnknown := false
			for _, interpolationValue := range interpolated {
				parameters = mergeNavigationParameterMaps(parameters, interpolationValue.Parameters)
				confidence = lowerNavigationConfidenceString(confidence, interpolationValue.confidence())
				if interpolationValue.Kind == navigationValueUnknown || interpolationValue.Kind == navigationValueTemplate {
					interpolationUnknown = true
				}
			}
			if multipleValues {
				confidence = lowerNavigationConfidenceString(confidence, "possible")
			}
			context := &navigationCandidateContext{
				rangeValue: candidate.Range, snippet: candidate.Snippet, extractor: "javascript",
				confidence: confidence, parameters: parameters, value: contextValue,
			}
			extra := cloneNavigationExtra(candidate.Extra)
			if extra == nil {
				extra = map[string]any{}
			}
			delete(extra, navigationJavaScriptExpressionIndicesKey)
			delete(extra, navigationJavaScriptDependencyMarkersKey)
			delete(extra, navigationJavaScriptFallbackRangeKey)
			if ranges, ok := extra["javascriptExpressionRanges"].([]lsp.Range); ok && len(ranges) > 0 {
				context.rangeValues = append([]lsp.Range(nil), ranges...)
				delete(extra, "javascriptExpressionRanges")
			}
			if snippets, ok := extra["javascriptExpressionSnippets"].([]string); ok && len(snippets) > 0 {
				context.snippets = append([]string(nil), snippets...)
				delete(extra, "javascriptExpressionSnippets")
			}
			if variant != nil && len(dependencyIndices) > 0 {
				if variant.sourceEvidence != nil {
					for _, index := range dependencyIndices {
						if index >= 0 && index < len(variant.sourceEvidence) {
							evidence := variant.sourceEvidence[index]
							if evidence.uri == "" {
								continue
							}
							context.additionalEvidence = append(context.additionalEvidence, map[string]any{"uri": evidence.uri, "range": evidence.rangeValue, "snippet": evidence.snippet, "label": candidate.Kind, "extractor": "javascript"})
						}
					}
				} else if err := navigationJavaScriptAppendDependencyEvidence(b.cancelContext, context, parsed, b.document, variant.regions, dependencyIndices); err != nil {
					b.navigationError = err
					return
				}
			}
			b.context = context
			extra["dynamic"] = len(interpolated) > 0 || value.Kind != navigationValueLiteral
			extra["pathKnown"] = !interpolationUnknown && navigationValuePathKnown(value)
			if duplicateAlternative {
				extra[navigationJavaScriptAlternativeEvidenceKey] = true
			}
			b.addTargetEdge(b.cancelContext, parsed.URI, sourceID, candidate.Kind, value.Text, extra)
			b.context = nil
		}
	}
}

func navigationJavaScriptAppendDependencyEvidence(ctx context.Context, candidateContext *navigationCandidateContext, parsed *core.ParsedDocument, document *core.TextDocument, regions []core.Region, indices []int) error {
	if candidateContext == nil || parsed == nil {
		return nil
	}
	if document == nil || document.URI != parsed.URI || document.Text != parsed.Text {
		return nil
	}
	seen := make(map[lsp.Range]struct{}, len(candidateContext.rangeValues)+len(indices))
	for _, rangeValue := range candidateContext.rangeValues {
		seen[rangeValue] = struct{}{}
	}
	for _, index := range indices {
		if ctx != nil {
			if err := ctx.Err(); err != nil {
				return err
			}
		}
		if index < 0 || index >= len(regions) {
			continue
		}
		region := regions[index]
		rangeValue := document.Range(region.Start, region.End)
		if _, ok := seen[rangeValue]; ok {
			continue
		}
		seen[rangeValue] = struct{}{}
		candidateContext.rangeValues = append(candidateContext.rangeValues, rangeValue)
		candidateContext.snippets = append(candidateContext.snippets, navigationJavaScriptEvidenceSnippet(parsed.Text, region.Start, region.End))
	}
	return nil
}

func (b *navigationGraphBuilder) addJavaScriptBudgetFallbackEdges(parsed *core.ParsedDocument, sourceID string, region core.Region, variant navigationJavaScriptInterpolationVariant) {
	if b == nil || parsed == nil || len(variant.regions) == 0 {
		return
	}
	virtual, err := core.BuildEmbeddedRegionVirtualDocumentWithReplacementsContext(b.cancelContext, parsed, region, nil)
	if err != nil {
		b.navigationError = err
		return
	}
	ranges := make([]lsp.Range, 0, len(variant.regions))
	snippets := make([]string, 0, len(variant.regions))
	for _, expression := range variant.regions {
		if err := b.navigationContextError(); err != nil {
			b.navigationError = err
			return
		}
		ranges = append(ranges, b.document.Range(expression.Start, expression.End))
		snippets = append(snippets, navigationJavaScriptEvidenceSnippet(parsed.Text, expression.Start, expression.End))
	}
	parameters := []map[string]any(nil)
	for _, value := range variant.values {
		parameters = mergeNavigationParameterMaps(parameters, value.Parameters)
	}
	b.context = &navigationCandidateContext{
		rangeValue: ranges[0], rangeValues: ranges, snippet: snippets[0], snippets: snippets,
		extractor: "javascript", confidence: "possible", parameters: parameters,
		value: navigationValue{Kind: navigationValueUnknown, Text: "{unknown}", Parameters: parameters},
	}
	defer func() { b.context = nil }()
	script := maskEmbeddedJavaScriptComments(virtual.Text)
	extra := map[string]any{"dynamic": true, "pathKnown": false, "confidence": "possible"}
	addMatches := func(kind string, count int, matchExtra map[string]any) {
		for range count {
			combined := cloneNavigationExtra(extra)
			for key, value := range matchExtra {
				combined[key] = value
			}
			b.addTargetEdge(b.cancelContext, parsed.URI, sourceID, kind, "{unknown}", combined)
			if b.navigationError != nil {
				return
			}
		}
	}
	addMatches("javascriptLocation", len(navigationLocationPattern.FindAllStringSubmatch(script, -1)), nil)
	addMatches("javascriptLocation", len(navigationLocationCallPattern.FindAllStringSubmatch(script, -1)), nil)
	addMatches("javascriptLocation", len(navigationWindowOpenPattern.FindAllStringSubmatch(script, -1)), nil)
	addMatches("javascriptHistory", len(navigationHistoryCallPattern.FindAllStringSubmatch(script, -1)), nil)
	addMatches("javascriptFormSubmit", len(navigationFormSubmitPattern.FindAllStringSubmatch(script, -1)), map[string]any{"method": "{unknown}"})
}

func navigationJavaScriptCandidateEmissionKey(candidate navigationFiniteCandidate, value navigationValue) string {
	parts := []string{
		candidate.Kind,
		strconv.Itoa(int(value.Kind)),
		strconv.Itoa(int(value.Primitive)),
		value.Text,
		strconv.Itoa(candidate.Range.Start.Line),
		strconv.Itoa(candidate.Range.Start.Character),
		strconv.Itoa(candidate.Range.End.Line),
		strconv.Itoa(candidate.Range.End.Character),
		navigationString(candidate.Extra["method"]),
		navigationString(candidate.Extra["targetFrame"]),
	}
	return strings.Join(parts, "\x00")
}

func (b *navigationGraphBuilder) addJavaScriptNavigationEdges(parsed *core.ParsedDocument, sourceID string, ownerURIs ...string) {
	if parsed == nil {
		return
	}
	ownerURI := parsed.URI
	if len(ownerURIs) > 0 && ownerURIs[0] != "" {
		ownerURI = ownerURIs[0]
	}
	b.addJavaScriptNavigationEdgesForOccurrence(parsed, sourceID, ownerURI, "", 0, len(parsed.Text))
}

func (b *navigationGraphBuilder) addVBScriptNavigationEdges(parsed *core.ParsedDocument, sourceID, ownerURI string) {
	programKey := b.fileIdentityKey(ownerURI)
	if program, ok := b.vbExecutionPrograms[programKey]; ok {
		if !b.navigationVBExecutionProgramContains(program, parsed) {
			state := newNavigationVBState()
			for name, function := range b.navigationVBFunctionsForDocument(parsed.URI, ownerURI) {
				state.functions[name] = function
			}
			b.addVBScriptNavigationRegions(parsed, sourceID, ownerURI, state, 0, len(parsed.Text))
			return
		}
		if b.vbExecutionProcessed[programKey] {
			return
		}
		b.vbExecutionProcessed[programKey] = true
		b.executeVBScriptNavigationProgram(program)
		return
	}
	state := newNavigationVBState()
	for name, function := range b.navigationVBFunctionsForDocument(parsed.URI, ownerURI) {
		state.functions[name] = function
	}
	b.addVBScriptNavigationRegions(parsed, sourceID, ownerURI, state, 0, len(parsed.Text))
}

func (b *navigationGraphBuilder) navigationVBExecutionProgramContains(program []navigationVBExecutionUnit, parsed *core.ParsedDocument) bool {
	if b == nil || parsed == nil {
		return false
	}
	key := b.fileIdentityKey(parsed.URI)
	for _, unit := range program {
		if unit.parsed != nil && b.fileIdentityKey(unit.parsed.URI) == key {
			return true
		}
	}
	return false
}

func (b *navigationGraphBuilder) executeVBScriptNavigationProgram(program []navigationVBExecutionUnit) {
	if len(program) == 0 {
		return
	}
	state := newNavigationVBState()
	// Seed the execution state once with the root owner's effective function
	// scope. Each subsequent unit contributes only its own definitions; this
	// preserves source-order shadowing without rebuilding the transitive scope
	// for every include unit.
	if err := b.navigationVBAddOwnerFunctions(state, program[0].ownerURI); err != nil {
		b.navigationError = err
		return
	}
	for _, unit := range program {
		if unit.truncated {
			b.vbIncludeExpansionTruncated = true
			b.discardIncompleteIncludeGraph()
			return
		}
		if unit.parsed == nil {
			continue
		}
		if err := b.navigationContextError(); err != nil {
			b.navigationError = err
			return
		}
		if err := b.navigationVBAddDocumentFunctions(state, unit.parsed.URI); err != nil {
			b.navigationError = err
			return
		}
		sourceID := b.navigationSourceID(unit.ownerURI)
		previousCurrent, previousDocument := b.current, b.document
		previousOccurrence := b.currentOccurrence
		b.current = unit.parsed
		b.document = core.NewTextDocument(unit.parsed.URI, "classic-asp", 0, unit.parsed.Text)
		b.currentOccurrence = unit.occurrenceID
		b.addVBScriptNavigationRegions(unit.parsed, sourceID, unit.ownerURI, state, unit.start, unit.end, unit.occurrenceID)
		b.current, b.document, b.currentOccurrence = previousCurrent, previousDocument, previousOccurrence
		if b.navigationError != nil {
			return
		}
	}
}

func (b *navigationGraphBuilder) navigationVBAddOwnerFunctions(state *navigationVBState, ownerURI string) error {
	if b == nil || state == nil {
		return nil
	}
	keys, err := b.navigationVBFunctionOwnerKeys(ownerURI)
	if err != nil {
		return err
	}
	for _, key := range keys {
		if err := b.navigationContextError(); err != nil {
			return err
		}
		definitions := b.navigationVBFunctionDefinitionsForURI(key)
		names := make([]string, 0, len(definitions))
		for name := range definitions {
			if err := b.navigationContextError(); err != nil {
				return err
			}
			names = append(names, name)
		}
		sort.Strings(names)
		for _, name := range names {
			if err := b.navigationContextError(); err != nil {
				return err
			}
			if _, exists := state.functions[name]; !exists {
				state.functions[name] = definitions[name]
			}
		}
	}
	return nil
}

func (b *navigationGraphBuilder) navigationVBAddDocumentFunctions(state *navigationVBState, parsedURI string) error {
	if b == nil || state == nil {
		return nil
	}
	definitions := b.navigationVBFunctionDefinitionsForURI(parsedURI)
	names := make([]string, 0, len(definitions))
	for name := range definitions {
		if err := b.navigationContextError(); err != nil {
			return err
		}
		names = append(names, name)
	}
	sort.Strings(names)
	for _, name := range names {
		if err := b.navigationContextError(); err != nil {
			return err
		}
		state.functions[name] = definitions[name]
	}
	return nil
}

func (b *navigationGraphBuilder) addVBScriptNavigationRegions(parsed *core.ParsedDocument, sourceID, ownerURI string, state *navigationVBState, start, end int, occurrenceIDs ...string) {
	if parsed == nil || state == nil {
		return
	}
	state.cancelContext = b.cancelContext
	if start < 0 {
		start = 0
	}
	if end > len(parsed.Text) {
		end = len(parsed.Text)
	}
	occurrenceID := ""
	if len(occurrenceIDs) > 0 {
		occurrenceID = occurrenceIDs[0]
	}
	for _, region := range parsed.Regions {
		if err := b.navigationContextError(); err != nil || navigationVBCancelled(state) {
			if err == nil && state.cancelContext != nil {
				err = state.cancelContext.Err()
			}
			if err == nil {
				err = context.Canceled
			}
			b.navigationError = err
			return
		}
		if region.Language != core.LanguageVBScript || region.Start < start || region.Start >= end {
			continue
		}
		content := parsed.Text[region.ContentStart:region.ContentEnd]
		var candidates []navigationVBCandidate
		var expressionValues []navigationVBValue
		if region.Kind == core.RegionASPExpression {
			candidates, expressionValues = extractVBScriptNavigationExpressionWithState(content, region.ContentStart, parsed.Text, state)
			if len(expressionValues) > 0 {
				values := make([]navigationValue, 0, len(expressionValues))
				for _, value := range expressionValues {
					values = append(values, value.finiteCandidates()...)
				}
				b.vbExpressionValues[b.navigationVBExpressionKey(ownerURI, parsed.URI, region.Start, occurrenceID)] = values
			}
		} else {
			candidates = extractVBScriptNavigationCandidatesWithState(content, region.ContentStart, parsed.Text, state)
		}
		for _, candidate := range candidates {
			values := cloneNavigationValues(candidate.Values)
			if len(values) == 0 {
				values = candidate.Value.finiteCandidates()
			}
			for _, value := range values {
				if err := b.navigationContextError(); err != nil {
					b.navigationError = err
					return
				}
				b.context = &navigationCandidateContext{
					rangeValue: candidate.Range, valueRange: candidate.ValueRange, snippet: candidate.Snippet, extractor: "vbscript",
					confidence: value.confidence(), parameters: cloneNavigationParameterMaps(value.Parameters), value: cloneNavigationValue(value),
				}
				if candidate.Kind == "write" {
					if navigationHTMLPattern.MatchString(value.Text) {
						b.addHTMLNavigationEdgesFromText(parsed.URI, sourceID, value.Text)
					}
				} else {
					extra := map[string]any{"confidence": value.confidence(), "dynamic": value.Kind != navigationValueLiteral, "pathKnown": navigationValuePathKnown(value)}
					if len(value.Parameters) > 0 {
						extra["parameters"] = cloneNavigationParameterMaps(value.Parameters)
					}
					b.addTargetEdge(b.cancelContext, parsed.URI, sourceID, "serverRedirect", value.Text, extra)
				}
				b.context = nil
			}
		}
		if navigationVBCancelled(state) {
			b.navigationError = b.navigationContextError()
			if b.navigationError == nil {
				b.navigationError = context.Canceled
			}
			return
		}
		if b.navigationError != nil {
			return
		}
	}
}

func (b *navigationGraphBuilder) addTargetEdge(ctx context.Context, ownerURI string, sourceID string, kind string, rawTarget string, extra map[string]any) {
	if ctx == nil {
		ctx = context.Background()
	}
	if err := ctx.Err(); err != nil {
		b.navigationError = err
		return
	}
	// Includes execute in the requesting page. Keep source evidence in the
	// declaring file, but resolve relative destinations against that page.
	if source := b.nodeByID[sourceID]; source != nil && navigationString(source["uri"]) != "" {
		ownerURI = navigationString(source["uri"])
	} else if uri := b.pendingSourceURIByID[sourceID]; uri != "" {
		ownerURI = uri
	}
	dynamic, _ := extra["dynamic"].(bool)
	pathKnown, hasPathKnown := extra["pathKnown"].(bool)
	if !hasPathKnown {
		pathKnown = false
	}
	if b.context != nil && strings.Contains(rawTarget, navigationHTMLInterpolationStart) {
		rawTarget = navigationStripHTMLInterpolationMarkers(rawTarget)
	}
	if b.context != nil && !hasPathKnown && b.context.value.Kind != navigationValueLiteral {
		dynamic = strings.ContainsAny(rawTarget, "{}")
		pathKnown = dynamic && navigationRawTargetPathKnown(rawTarget)
	}
	if b.scope == "document" && sourceID != b.rootNodeID && !navigationDocumentTargetMatchesRoot(ownerURI, rawTarget, kind, b.workspaceRoots, dynamic, pathKnown, b.rootURI) {
		return
	}
	target, ok := b.navigationTarget(ctx, ownerURI, rawTarget, kind, dynamic, pathKnown)
	if err := ctx.Err(); err != nil {
		b.navigationError = err
		return
	}
	if !ok {
		return
	}
	method, targetFrame := "", ""
	confidence := "certain"
	if extra != nil {
		method, _ = extra["method"].(string)
		targetFrame, _ = extra["targetFrame"].(string)
		if configured, ok := extra["confidence"].(string); ok {
			confidence = configured
		}
	}
	rangeValue, snippet := b.currentRange(rawTarget)
	if sourceSpan, ok := extra[navigationHTMLSourceEvidenceKey].(navigationHTMLSourceSpan); ok {
		rangeValue, snippet = b.currentRangeAt(sourceSpan.Start, sourceSpan.End)
	}
	extractor := "html"
	if strings.HasPrefix(kind, "javascript") {
		extractor = "javascript"
	}
	if kind == "serverRedirect" {
		extractor = "vbscript"
	}
	if b.context != nil {
		rangeValue, snippet, extractor = b.context.rangeValue, b.context.snippet, b.context.extractor
		confidence = lowerNavigationConfidenceString(confidence, b.context.confidence)
		if _, configured := extra["dynamic"]; !configured {
			dynamic = b.context.value.Kind != navigationValueLiteral
		}
		if _, configured := extra["pathKnown"]; !configured {
			if b.context.value.Kind != navigationValueLiteral {
				pathKnown = navigationRawTargetPathKnown(rawTarget)
			} else {
				pathKnown = navigationValuePathKnown(b.context.value)
			}
		}
	}
	if target["kind"] == "unknown" {
		// Unresolved expressions at different source sites are different targets.
		// Keep the raw label for compatibility; the UI uses edge evidence to
		// show the originating statement and its exact UTF-16 location.
		target["id"] = fmt.Sprintf("%s@%s:%s:%s:%d:%d:%d:%d", target["id"], sourceID, b.current.URI, kind, rangeValue.Start.Line, rangeValue.Start.Character, rangeValue.End.Line, rangeValue.End.Character)
	}
	b.materializeNavigationSourceNode(sourceID)
	targetID := b.addNode(target)
	if targetID == "" {
		return
	}
	targetKey := targetID
	if query := navigationTargetQuery(rawTarget); query != "" {
		targetKey += "?" + query
	}
	key := strings.Join([]string{sourceID, targetKey, kind, method, targetFrame}, "|")
	rangeValues := []lsp.Range{rangeValue}
	snippets := []string{snippet}
	if b.context != nil {
		if len(b.context.rangeValues) > 0 {
			rangeValues = append([]lsp.Range(nil), b.context.rangeValues...)
		}
		if len(b.context.snippets) > 0 {
			snippets = append([]string(nil), b.context.snippets...)
		}
	}
	evidence := make([]map[string]any, 0, len(rangeValues))
	for index, evidenceRange := range rangeValues {
		evidenceSnippet := snippet
		if index < len(snippets) {
			evidenceSnippet = snippets[index]
		}
		item := map[string]any{"uri": b.current.URI, "range": evidenceRange, "label": kind, "snippet": evidenceSnippet, "extractor": extractor}
		if b.context != nil && b.context.valueRange != nil && navigationRangeContains(evidenceRange, *b.context.valueRange) {
			item["valueRange"] = *b.context.valueRange
		}
		evidence = append(evidence, item)
	}
	if b.context != nil {
		evidence = append(evidence, b.context.additionalEvidence...)
	}
	parameters := []map[string]any(nil)
	if !dynamic || pathKnown {
		parameters = navigationQueryParameters(rawTarget)
	}
	if b.context != nil {
		parameters = mergeNavigationParameterMaps(parameters, b.context.parameters)
	}
	if extraParameters, ok := extra["parameters"].([]map[string]any); ok {
		parameters = mergeNavigationParameterMaps(parameters, extraParameters)
	}
	if existing := b.edgeByKey[key]; existing != nil {
		alternativeEvidence, _ := extra[navigationJavaScriptAlternativeEvidenceKey].(bool)
		if !alternativeEvidence {
			existing["count"] = existing["count"].(int) + 1
			existing["ranges"] = append(existing["ranges"].([]lsp.Range), rangeValues...)
			existing["evidence"] = append(existing["evidence"].([]map[string]any), evidence...)
		}
		existing["confidence"] = lowerNavigationConfidenceString(navigationString(existing["confidence"]), confidence)
		if existingParameters, ok := existing["parameters"].([]map[string]any); ok || len(parameters) > 0 {
			existing["parameters"] = mergeNavigationParameterMaps(existingParameters, parameters)
		}
		if err := ctx.Err(); err != nil {
			b.navigationError = err
		}
		return
	}
	edge := map[string]any{
		"id": "edge:" + strconv.Itoa(len(b.edges)+1), "source": sourceID, "target": targetID, "kind": kind,
		"label": kind, "confidence": confidence, "ranges": rangeValues, "declaredInUri": b.current.URI,
		"evidence": evidence, "count": 1,
	}
	for key, value := range extra {
		if key == "dynamic" || key == "confidence" || key == "pathKnown" || key == navigationHTMLSourceEvidenceKey || key == navigationJavaScriptAlternativeEvidenceKey {
			continue
		}
		edge[key] = cloneNavigationMetadata(value)
	}
	if len(parameters) > 0 {
		edge["parameters"] = parameters
	}
	b.edgeByKey[key] = edge
	b.edges = append(b.edges, edge)
	if err := ctx.Err(); err != nil {
		b.navigationError = err
	}
}

func (b *navigationGraphBuilder) navigationTarget(ctx context.Context, ownerURI, rawTarget, edgeKind string, dynamic, pathKnown bool) (map[string]any, bool) {
	if b == nil {
		return navigationTarget(ctx, ownerURI, rawTarget, edgeKind, nil, dynamic, pathKnown)
	}
	if !b.trustedWorkspaceRootsReady {
		paths := make([]string, 0, len(b.workspaceRoots))
		for _, root := range b.workspaceRoots {
			if root.Path != "" {
				paths = append(paths, root.Path)
			}
		}
		roots, complete := trustedPathRootsContext(ctx, paths)
		if !complete {
			return nil, false
		}
		b.trustedWorkspaceRoots = roots
		b.trustedWorkspaceRootsReady = true
	}
	return navigationTargetWithPreparedRoots(ctx, ownerURI, rawTarget, edgeKind, b.workspaceRoots, b.trustedWorkspaceRoots, true, dynamic, pathKnown)
}

func lowerNavigationConfidenceString(left, right string) string {
	if navigationConfidenceRank(right) > navigationConfidenceRank(left) {
		return right
	}
	return left
}

func navigationConfidenceRank(value string) int {
	switch value {
	case "probable":
		return 1
	case "possible":
		return 2
	case "unknown":
		return 3
	default:
		return 0
	}
}

func navigationDynamicExtra(dynamic bool) map[string]any {
	if !dynamic {
		return nil
	}
	return map[string]any{"dynamic": true, "confidence": "unknown"}
}

func navigationJavascriptExtra(dynamic bool) map[string]any {
	if dynamic {
		return navigationDynamicExtra(true)
	}
	return map[string]any{"confidence": "probable"}
}

func mergeNavigationParameterMaps(groups ...[]map[string]any) []map[string]any {
	merged := make([]map[string]any, 0)
	seen := map[string]struct{}{}
	for _, group := range groups {
		for _, parameter := range group {
			if parameter == nil {
				continue
			}
			key := navigationString(parameter["source"]) + "\x00" + navigationString(parameter["name"]) + "\x00" + navigationString(parameter["value"])
			if _, ok := seen[key]; ok {
				continue
			}
			seen[key] = struct{}{}
			merged = append(merged, cloneNavigationParameter(parameter))
		}
	}
	return merged
}

func (b *navigationGraphBuilder) currentRange(rawTarget string) (lsp.Range, string) {
	if b.current == nil {
		return lsp.Range{}, ""
	}
	key := b.current.URI + "\x00" + b.currentOccurrence + "\x00" + rawTarget
	searchStart := b.rangeCursor[key]
	offset := strings.Index(b.current.Text[searchStart:], rawTarget)
	if offset >= 0 {
		offset += searchStart
		b.rangeCursor[key] = offset + len(rawTarget)
	}
	if offset < 0 {
		offset = 0
	}
	end := offset + len(rawTarget)
	return b.currentRangeAt(offset, end)
}

func (b *navigationGraphBuilder) currentRangeAt(offset, end int) (lsp.Range, string) {
	if b == nil || b.current == nil {
		return lsp.Range{}, ""
	}
	if offset < 0 {
		offset = 0
	}
	if offset > len(b.current.Text) {
		offset = len(b.current.Text)
	}
	if end < offset {
		end = offset
	}
	if end > len(b.current.Text) {
		end = len(b.current.Text)
	}
	doc := b.document
	if doc == nil {
		doc = core.NewTextDocument(b.current.URI, "classic-asp", 0, b.current.Text)
		b.document = doc
	}
	snippetEnd := end
	for snippetEnd < len(b.current.Text) && b.current.Text[snippetEnd] != '\n' && b.current.Text[snippetEnd] != '\r' {
		snippetEnd++
	}
	snippetStart := offset
	for snippetStart > 0 && b.current.Text[snippetStart-1] != '\n' && b.current.Text[snippetStart-1] != '\r' {
		snippetStart--
	}
	return doc.Range(offset, end), strings.TrimSpace(b.current.Text[snippetStart:snippetEnd])
}

func (b *navigationGraphBuilder) fileIdentityKey(uri string) string {
	if b == nil {
		return workspacepkg.FileIdentityKeyFromURI(uri)
	}
	if key, ok := b.fileIdentityByURI[uri]; ok {
		return key
	}
	key := workspacepkg.FileIdentityKeyFromURI(uri)
	if b.fileIdentityByURI == nil {
		b.fileIdentityByURI = map[string]string{}
	}
	b.fileIdentityByURI[uri] = key
	return key
}

func (b *navigationGraphBuilder) addURINode(uri string) string {
	id := b.sourceIDByURI[uri]
	if id == "" {
		id = "page:" + b.fileIdentityKey(uri)
		b.sourceIDByURI[uri] = id
	}
	if b.materializedSourceURIByID[id] == uri {
		return id
	}
	path := fileURIPath(uri)
	kind := "page"
	if strings.EqualFold(filepath.Ext(path), ".inc") {
		kind = "fragment"
	}
	exists := true
	node := map[string]any{
		"id": id, "label": filepath.Base(path), "kind": kind,
		"uri": canonicalGraphURI(uri), "fileName": filepath.Base(path), "exists": exists,
		"isRoot": id == b.rootNodeID,
	}
	if existing := b.nodeByID[id]; existing != nil {
		for key, value := range node {
			existing[key] = value
		}
		b.materializedSourceURIByID[id] = uri
		return id
	}
	b.materializedSourceURIByID[id] = uri
	return b.addNode(node)
}

func (b *navigationGraphBuilder) navigationSourceID(uri string) string {
	if b == nil || uri == "" {
		return ""
	}
	id := b.sourceIDByURI[uri]
	if id == "" {
		id = "page:" + b.fileIdentityKey(uri)
		b.sourceIDByURI[uri] = id
	}
	if b.scope != "document" || id == b.rootNodeID {
		return b.addURINode(uri)
	}
	if _, tracked := b.pendingSourceURIByID[id]; !tracked {
		b.pendingSourceURIByID[id] = canonicalGraphURI(uri)
	}
	return id
}

func (b *navigationGraphBuilder) materializeNavigationSourceNode(sourceID string) {
	if b == nil || sourceID == "" {
		return
	}
	uri := b.pendingSourceURIByID[sourceID]
	if uri == "" {
		return
	}
	b.pendingSourceURIByID[sourceID] = ""
	b.addURINode(uri)
}

func (b *navigationGraphBuilder) addNode(node map[string]any) string {
	id := navigationString(node["id"])
	if existing := b.nodeByID[id]; existing != nil {
		return id
	}
	b.nodeByID[id] = node
	b.nodes = append(b.nodes, node)
	return id
}

func navigationString(value any) string {
	text, _ := value.(string)
	return text
}

var (
	navigationLocationPattern         = regexp.MustCompile(`(?is)\b(?:(?:window|document)\s*\.\s*)?location(?:\s*\.\s*href)?\s*=\s*([^;\n]+)`)
	navigationLocationCallPattern     = regexp.MustCompile(`(?is)\b(?:window\s*\.\s*)?location\s*\.\s*(?:assign|replace)\s*\(\s*([^,)]+)`)
	navigationWindowOpenPattern       = regexp.MustCompile(`(?is)\bwindow\s*\.\s*open\s*\(\s*([^,\)]+)(?:,\s*["']([^"']+)["'])?`)
	navigationHistoryPattern          = regexp.MustCompile(`(?is)\bhistory\.(?:pushState|replaceState)\s*\([^)]*["']([^"']+\.asp[^"']*)["']`)
	navigationHistoryCallPattern      = regexp.MustCompile(`(?is)\bhistory\s*\.\s*(?:pushState|replaceState)\s*\(`)
	navigationFormActionPattern       = regexp.MustCompile(`(?is)\b([A-Za-z_$][A-Za-z0-9_$]*)\s*\.\s*action\s*=\s*["']([^"']+)["']`)
	navigationFormMethodPattern       = regexp.MustCompile(`(?is)\b([A-Za-z_$][A-Za-z0-9_$]*)\s*\.\s*method\s*=\s*["']([^"']+)["']`)
	navigationFormSubmitPattern       = regexp.MustCompile(`(?is)\b([A-Za-z_$][A-Za-z0-9_$]*)\s*\.\s*submit\s*\(`)
	navigationStringAssignmentPattern = regexp.MustCompile(`(?is)\b(?:const|let|var)\s+([A-Za-z_$][A-Za-z0-9_$]*)\s*=\s*["']([^"']+)["']`)
	navigationHTMLPattern             = regexp.MustCompile(`(?is)<\s*(?:a|area|iframe|frame|form|input|button|meta)\b`)
	navigationStableIDPattern         = regexp.MustCompile(`[^a-z0-9._-]+`)
)

func htmlAttributeValue(attrs string, name string) string {
	return navigationStripHTMLInterpolationMarkers(decodeHTMLAttributeValue(htmlAttributeRawValue(attrs, name)))
}

func htmlAttributeRawValue(attrs string, name string) string {
	for _, attribute := range scanNavigationHTMLAttributes(attrs, 0, len(attrs)) {
		if attribute.Name == strings.ToLower(name) {
			return attribute.RawValue
		}
	}
	return ""
}

func hiddenInputParameters(html string) []map[string]any {
	return formControlParameters(html)
}

type formActionControl struct {
	Action      string
	Method      string
	TargetFrame string
	Parameter   map[string]any
}

func formActionControlFromAttributes(attrs string) (formActionControl, bool) {
	action := htmlAttributeValue(attrs, "formaction")
	if action == "" {
		return formActionControl{}, false
	}
	control := formActionControl{
		Action:      action,
		Method:      strings.ToUpper(htmlAttributeValue(attrs, "formmethod")),
		TargetFrame: htmlAttributeValue(attrs, "formtarget"),
	}
	if control.Method == "" {
		control.Method = "GET"
	}
	if name := htmlAttributeValue(attrs, "name"); name != "" {
		parameter := map[string]any{"name": name, "source": "formControl"}
		if value := htmlAttributeValue(attrs, "value"); value != "" {
			parameter["value"] = value
		}
		control.Parameter = parameter
	}
	return control, true
}

func formControlParameters(html string) []map[string]any {
	parameters := []map[string]any{}
	for _, tag := range scanNavigationHTMLTags(html) {
		if tag.Closing || tag.Name != "button" && tag.Name != "input" {
			continue
		}
		attrs := tag.attrsText(html)
		name := htmlAttributeValue(attrs, "name")
		if name == "" {
			continue
		}
		source := "formControl"
		if strings.EqualFold(htmlAttributeValue(attrs, "type"), "hidden") {
			source = "hiddenInput"
		}
		parameter := map[string]any{"name": name, "source": source}
		if value := htmlAttributeValue(attrs, "value"); value != "" {
			parameter["value"] = value
		}
		parameters = append(parameters, parameter)
	}
	for _, tag := range scanNavigationHTMLTags(html) {
		if tag.Closing || tag.Name != "select" && tag.Name != "textarea" {
			continue
		}
		name := htmlAttributeValue(tag.attrsText(html), "name")
		if name != "" {
			parameters = append(parameters, map[string]any{"name": name, "source": "formControl"})
		}
	}
	return parameters
}

func decodeHTMLAttributeValue(value string) string {
	// Unescape exactly once so named and numeric HTML references are handled
	// uniformly without changing the raw source span used for evidence.
	return html.UnescapeString(value)
}

func metaRefreshTarget(content string) string {
	for _, part := range strings.Split(content, ";") {
		part = strings.TrimSpace(part)
		if key, value, ok := strings.Cut(part, "="); ok && strings.EqualFold(strings.TrimSpace(key), "url") {
			return strings.Trim(strings.TrimSpace(value), `"'`)
		}
	}
	return ""
}

func javascriptStringAssignments(script string) map[string]string {
	values := map[string]string{}
	for _, match := range navigationStringAssignmentPattern.FindAllStringSubmatch(script, -1) {
		values[match[1]] = match[2]
	}
	return values
}

func javascriptFormMethods(script string) map[string]string {
	methods := map[string]string{}
	for _, match := range navigationFormMethodPattern.FindAllStringSubmatch(script, -1) {
		key := strings.ToLower(match[1])
		if _, exists := methods[key]; !exists {
			methods[key] = strings.ToUpper(match[2])
		}
	}
	return methods
}

func javascriptSubmittedForms(script string) map[string]struct{} {
	forms := map[string]struct{}{}
	for _, match := range navigationFormSubmitPattern.FindAllStringSubmatch(script, -1) {
		forms[strings.ToLower(match[1])] = struct{}{}
	}
	return forms
}

func javascriptNavigationValue(value string, assignments map[string]string) (string, bool) {
	trimmed := strings.TrimSpace(value)
	if len(trimmed) >= 2 && ((trimmed[0] == '"' && trimmed[len(trimmed)-1] == '"') || (trimmed[0] == '\'' && trimmed[len(trimmed)-1] == '\'')) {
		return strings.Trim(trimmed, `"'`), false
	}
	if assigned, ok := assignments[trimmed]; ok {
		return assigned, false
	}
	return strings.Trim(trimmed, `"'`), true
}

func navigationDocumentTargetMatchesRoot(ownerURI, rawTarget, edgeKind string, workspaceRoots []workspaceRoot, dynamic, pathKnown bool, rootURI string) bool {
	if rootURI == "" || dynamic && !pathKnown {
		return false
	}
	target := strings.TrimSpace(rawTarget)
	if target == "" {
		return edgeKind == "htmlForm" && workspacepkg.SameFileIdentityURI(ownerURI, rootURI)
	}
	if target == "#" || strings.HasPrefix(strings.ToLower(target), "javascript:") {
		return false
	}
	pathPart := target
	if index := strings.IndexAny(pathPart, "?#"); index >= 0 {
		pathPart = pathPart[:index]
	}
	if pathPart == "" {
		return workspacepkg.SameFileIdentityURI(ownerURI, rootURI)
	}
	if navigationTargetIsExternal(pathPart) {
		return false
	}
	ownerPath := fileURIPath(ownerURI)
	if ownerPath == "" {
		return false
	}
	resolved := pathPart
	if strings.HasPrefix(resolved, "/") {
		workspaceRoot := workspaceRootPathForPath(ownerPath, workspaceRoots)
		if workspaceRoot == "" {
			workspaceRoot = filepath.Dir(ownerPath)
		}
		resolved = filepath.Join(workspaceRoot, filepath.FromSlash(strings.TrimLeft(resolved, "/")))
	} else if !filepath.IsAbs(resolved) {
		resolved = filepath.Join(filepath.Dir(ownerPath), filepath.FromSlash(pathPart))
	}
	return workspacepkg.SameFileIdentityURI(filePathURI(filepath.Clean(resolved)), rootURI)
}

func navigationTarget(ctx context.Context, ownerURI, rawTarget, edgeKind string, workspaceRoots []workspaceRoot, dynamic bool, pathKnown ...bool) (map[string]any, bool) {
	return navigationTargetWithPreparedRoots(ctx, ownerURI, rawTarget, edgeKind, workspaceRoots, nil, false, dynamic, pathKnown...)
}

func navigationTargetIsExternal(pathPart string) bool {
	if !strings.Contains(pathPart, ":") && !strings.HasPrefix(pathPart, "//") {
		return false
	}
	parsed, err := url.Parse(pathPart)
	return err == nil && (parsed.IsAbs() || strings.HasPrefix(pathPart, "//"))
}

func navigationTargetWithPreparedRoots(ctx context.Context, ownerURI, rawTarget, edgeKind string, workspaceRoots []workspaceRoot, trustedRoots []trustedFilesystemRoot, trustedRootsPrepared, dynamic bool, pathKnown ...bool) (map[string]any, bool) {
	if ctx == nil {
		ctx = context.Background()
	}
	if ctx.Err() != nil {
		return nil, false
	}
	target := strings.TrimSpace(rawTarget)
	knownPath := false
	if len(pathKnown) > 0 {
		knownPath = pathKnown[0]
	}
	if dynamic && !knownPath {
		if ctx.Err() != nil {
			return nil, false
		}
		label := target
		if label == "" {
			label = "dynamic target"
		}
		return map[string]any{"id": "unknown:" + stableNavigationID(label), "label": label, "kind": "unknown", "exists": false}, true
	}
	if target == "" && edgeKind == "htmlForm" {
		if ctx.Err() != nil {
			return nil, false
		}
		path := fileURIPath(ownerURI)
		return map[string]any{
			"id": "page:" + workspacepkg.FileIdentityKeyFromURI(ownerURI), "label": filepath.Base(path), "kind": navigationFileKind(path),
			"uri": canonicalGraphURI(ownerURI), "fileName": filepath.Base(path), "exists": true,
		}, true
	}
	if target == "" || target == "#" {
		return nil, false
	}
	if strings.HasPrefix(strings.ToLower(target), "javascript:") {
		return nil, false
	}
	pathPart := target
	if index := strings.IndexAny(pathPart, "?#"); index >= 0 {
		pathPart = pathPart[:index]
	}
	if pathPart == "" {
		ownerPath := fileURIPath(ownerURI)
		if ownerPath == "" {
			return nil, false
		}
		return map[string]any{
			"id": "page:" + workspacepkg.FileIdentityKeyFromURI(ownerURI), "label": filepath.Base(ownerPath), "kind": navigationFileKind(ownerPath),
			"uri": canonicalGraphURI(ownerURI), "fileName": filepath.Base(ownerPath), "exists": true,
		}, true
	}
	label := filepath.Base(pathPart)
	if navigationTargetIsExternal(pathPart) {
		if ctx.Err() != nil {
			return nil, false
		}
		id := "external:" + stableNavigationID(target)
		return map[string]any{"id": id, "label": target, "kind": "external", "externalUrl": target, "exists": true}, true
	}
	ownerPath := fileURIPath(ownerURI)
	if ownerPath == "" {
		if ctx.Err() != nil {
			return nil, false
		}
		return map[string]any{"id": "unknown:" + stableNavigationID(target), "label": target, "kind": "unknown", "exists": false}, true
	}
	resolved := pathPart
	if strings.HasPrefix(resolved, "/") {
		if ctx.Err() != nil {
			return nil, false
		}
		workspaceRoot := workspaceRootPathForPath(ownerPath, workspaceRoots)
		if ctx.Err() != nil {
			return nil, false
		}
		if workspaceRoot == "" {
			workspaceRoot = filepath.Dir(ownerPath)
		}
		resolved = filepath.Join(workspaceRoot, filepath.FromSlash(strings.TrimLeft(resolved, "/")))
	} else if !filepath.IsAbs(resolved) {
		resolved = filepath.Join(filepath.Dir(ownerPath), filepath.FromSlash(pathPart))
	}
	resolved = filepath.Clean(resolved)
	if len(workspaceRoots) > 0 {
		hasRootPath := false
		var rootPaths []string
		if !trustedRootsPrepared {
			rootPaths = make([]string, 0, len(workspaceRoots))
		}
		for _, root := range workspaceRoots {
			if ctx.Err() != nil {
				return nil, false
			}
			if root.Path != "" {
				hasRootPath = true
				if !trustedRootsPrepared {
					rootPaths = append(rootPaths, root.Path)
				}
			}
		}
		if hasRootPath {
			var ok bool
			if trustedRootsPrepared {
				_, ok = trustedPathForPreparedRootsContext(ctx, resolved, trustedRoots)
			} else {
				_, ok = trustedPathForRootsContext(ctx, resolved, rootPaths)
			}
			if !ok {
				return nil, false
			}
		}
	}
	if ctx.Err() != nil {
		return nil, false
	}
	uri := filePathURI(resolved)
	_, err := os.Stat(resolved)
	if ctx.Err() != nil {
		return nil, false
	}
	return map[string]any{
		"id": "page:" + workspacepkg.FileIdentityKeyFromURI(uri), "label": label, "kind": navigationFileKind(resolved),
		"uri": uri, "fileName": label, "exists": err == nil,
	}, true
}

func navigationTargetQuery(rawTarget string) string {
	queryStart := strings.Index(rawTarget, "?")
	if queryStart < 0 {
		return ""
	}
	query := rawTarget[queryStart+1:]
	if fragment := strings.Index(query, "#"); fragment >= 0 {
		query = query[:fragment]
	}
	return query
}

func navigationValuePathKnown(value navigationValue) bool {
	if value.Kind == navigationValueLiteral {
		return true
	}
	target := strings.TrimSpace(value.Text)
	pathPart := target
	if index := strings.IndexAny(pathPart, "?#"); index >= 0 {
		pathPart = pathPart[:index]
	}
	if strings.ContainsAny(pathPart, "{}") {
		return false
	}
	if pathPart == "" {
		return strings.HasPrefix(target, "?") || strings.HasPrefix(target, "#")
	}
	return value.Kind == navigationValueTemplate
}

func navigationRawTargetPathKnown(target string) bool {
	target = strings.TrimSpace(target)
	pathPart := target
	if index := strings.IndexAny(pathPart, "?#"); index >= 0 {
		pathPart = pathPart[:index]
	}
	if strings.ContainsAny(pathPart, "{}") {
		return false
	}
	return pathPart != "" || strings.HasPrefix(target, "?") || strings.HasPrefix(target, "#")
}

func cloneNavigationExtra(extra map[string]any) map[string]any {
	if len(extra) == 0 {
		return nil
	}
	cloned := make(map[string]any, len(extra))
	for key, value := range extra {
		cloned[key] = cloneNavigationMetadata(value)
	}
	return cloned
}

func navigationQueryParameters(rawTarget string) []map[string]any {
	queryStart := strings.Index(rawTarget, "?")
	if queryStart < 0 {
		return nil
	}
	query := rawTarget[queryStart+1:]
	if fragment := strings.Index(query, "#"); fragment >= 0 {
		query = query[:fragment]
	}
	parameters := make([]map[string]any, 0)
	for _, part := range strings.Split(query, "&") {
		if part == "" {
			continue
		}
		name, value, hasValue := strings.Cut(part, "=")
		if decoded, err := url.QueryUnescape(name); err == nil {
			name = decoded
		}
		parameter := map[string]any{"name": name, "source": "queryString", "confidence": "certain"}
		if hasValue {
			if decoded, err := url.QueryUnescape(value); err == nil {
				value = decoded
			}
			parameter["value"] = value
		}
		parameters = append(parameters, parameter)
	}
	return parameters
}

func navigationFileKind(path string) string {
	if strings.EqualFold(filepath.Ext(path), ".inc") {
		return "fragment"
	}
	return "page"
}

func stableNavigationID(value string) string {
	value = strings.ToLower(value)
	value = navigationStableIDPattern.ReplaceAllString(value, "-")
	value = strings.Trim(value, "-")
	if len(value) > 96 {
		value = value[:96]
	}
	if value == "" {
		return "unknown"
	}
	return value
}

func navigationRangeContains(outer, inner lsp.Range) bool {
	before := func(a, b lsp.Position) bool {
		return a.Line < b.Line || (a.Line == b.Line && a.Character <= b.Character)
	}
	return before(outer.Start, inner.Start) && before(inner.Start, inner.End) && before(inner.End, outer.End)
}

func navigationHTMLFormMethod(value string) string {
	switch strings.ToUpper(value) {
	case "POST":
		return "POST"
	case "DIALOG":
		return "DIALOG"
	default:
		return "GET"
	}
}
