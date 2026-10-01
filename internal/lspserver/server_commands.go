package lspserver

import (
	"context"
	"sort"
	"strconv"
	"strings"
	"sync"
	"sync/atomic"
	"time"

	"github.com/yottonoko/classic-web-system-lsp/internal/core"
	"github.com/yottonoko/classic-web-system-lsp/internal/graph"
	"github.com/yottonoko/classic-web-system-lsp/internal/lsp"
	workspacepkg "github.com/yottonoko/classic-web-system-lsp/internal/workspace"
)

func (s *Server) workspaceDiagnostics(ctx context.Context) map[string]any {
	s.mu.Lock()
	openURIs := make([]string, 0, len(s.documents))
	open := make(map[string]struct{}, len(s.documents))
	for uri := range s.documents {
		openURIs = append(openURIs, uri)
		open[uri] = struct{}{}
	}
	workspaceURIs := make([]string, 0, len(s.workspace))
	for uri := range s.workspace {
		if _, ok := open[uri]; ok {
			continue
		}
		workspaceURIs = append(workspaceURIs, uri)
	}
	s.mu.Unlock()
	sort.Strings(openURIs)
	sort.Strings(workspaceURIs)
	uris := append(openURIs, workspaceURIs...)
	taskID, _ := s.beginProgressTask("workspace.diagnostics", "analyzing", "workspace.diagnostics", "workspace.diagnostics", "", len(uris), false)
	progressState := "failed"
	defer func() { s.finishProgressTask(taskID, "workspace.diagnostics", progressState) }()
	settingsKey := s.workspaceDiagnosticsSettingsFingerprint()
	closedSettingsKey := settingsKey + "\x00without-editor-hints"
	workspaceDiagnosticsStarted := time.Now()
	s.logWorkspaceDiagnosticsWorkerStarted(uris)
	// Pages usually share include targets; resolve each one once per pass.
	ctx = withIncludeResolutionMemo(ctx)
	itemsByIndex := make([]any, len(uris))
	var cacheHits atomic.Int64
	var cacheMisses atomic.Int64
	var completed atomic.Int64
	s.analysisWorkers.parallelForBulk(ctx, len(uris), func(workerCtx context.Context, index int) {
		if workerCtx.Err() != nil {
			return
		}
		uri := uris[index]
		doc, _ := s.parsed(uri)
		var version any
		_, isOpen := open[uri]
		if isOpen && doc != nil {
			version = doc.Version
		}
		itemSettingsKey := settingsKey
		if !isOpen {
			workerCtx = withoutEditorHints(workerCtx)
			itemSettingsKey = closedSettingsKey
		}
		diagnostics, cached := s.cachedWorkspaceDiagnosticsItem(uri, doc, itemSettingsKey)
		if cached {
			cacheHits.Add(1)
			diagnostics = diagnosticsForDocumentURI(uri, diagnostics)
		} else {
			cacheMisses.Add(1)
			snapshot := s.diagnosticsSnapshot(workerCtx, uri)
			if workerCtx.Err() != nil {
				return
			}
			diagnostics = []lsp.Diagnostic{}
			if snapshot.ok {
				diagnostics = nonNilDiagnostics(diagnosticsForDocumentURI(uri, snapshot.diagnostics))
			}
			s.rememberWorkspaceDiagnosticsItem(uri, doc, itemSettingsKey, diagnostics)
		}
		itemsByIndex[index] = map[string]any{
			"uri":     uri,
			"version": version,
			"kind":    "full",
			"items":   diagnostics,
		}
		label := "workspace.diagnostics.indexed"
		if isOpen {
			label = "workspace.diagnostics.openDocuments"
		}
		current := int(completed.Add(1))
		s.updateProgressTask(taskID, "workspace.diagnostics", label, progressDetailForURI(uri), current, len(uris), []string{progressDetailForURI(uri)}, "running")
	})
	items := make([]any, 0, len(itemsByIndex))
	for _, item := range itemsByIndex {
		if item != nil {
			items = append(items, item)
		}
	}
	if ctx.Err() == nil {
		s.updateProgressTask(taskID, "workspace.diagnostics", "workspace.diagnostics", "", len(uris), len(uris), nil, "completed")
		progressState = "completed"
	} else {
		progressState = "cancelled"
	}
	hits := cacheHits.Load()
	misses := cacheMisses.Load()
	s.logDebugSummaryEvent("workspaceDiagnostics.cache", "[asp-lsp] workspaceDiagnostics.cache"+formatLogFields(map[string]any{
		"hits": hits, "misses": misses, "documents": len(uris),
	}), map[string]any{"hits": hits, "misses": misses, "documents": len(uris)})
	if misses == 0 {
		s.logDebugSummaryEvent("workspaceDiagnostics.cache", "[asp-lsp] workspaceDiagnostics.process.hit", map[string]any{"cache": "process", "documents": len(uris)})
	} else {
		s.logDebugSummaryEvent("workspaceDiagnostics.cache", "[asp-lsp] workspaceDiagnostics.process.miss", map[string]any{"cache": "process", "documents": len(uris), "misses": misses})
	}
	s.mu.Lock()
	s.workspaceDiagnosticsProcessCache = true
	s.mu.Unlock()
	s.logWorkspaceDiagnosticsWorkerTerminated(uris, workspaceDiagnosticsStarted, progressState)
	return map[string]any{"items": items}
}

func (s *Server) executeCommand(params executeCommandParams) any {
	return s.executeCommandContext(context.Background(), params)
}

func (s *Server) executeCommandContext(ctx context.Context, params executeCommandParams) any {
	if maintenanceExecuteCommand(params.Command) {
		release, acquired := s.acquireMaintenanceCommand(ctx)
		if !acquired {
			return nil
		}
		defer release()
	}
	switch params.Command {
	case "aspLsp.reindexWorkspace", "aspLsp.server.reindexWorkspace":
		if !s.reindexWorkspaceContext(ctx) {
			return nil
		}
		return map[string]any{"ok": true}
	case "aspLsp.clearProcessCache", "aspLsp.server.clearProcessCache":
		if !s.clearRuntimeProcessCachesContext(ctx) {
			return nil
		}
		s.clearWorkspaceDiagnosticsCaches(false)
		if ctx.Err() != nil {
			return nil
		}
		s.reportAsyncRPCWriteError(s.requestVisualRefresh("command.clearProcessCache"))
		for _, uri := range s.openDocumentURIs() {
			if ctx.Err() != nil {
				return nil
			}
			_ = s.publishDiagnostics(uri)
		}
		return map[string]any{"ok": true, "cleared": "process"}
	case "aspLsp.clearDiskCache", "aspLsp.server.clearDiskCache":
		if !s.clearRuntimeDiskCacheContext(ctx) {
			return nil
		}
		s.clearWorkspaceDiagnosticsCaches(true)
		s.reportAsyncRPCWriteError(s.requestVisualRefresh("command.clearDiskCache"))
		return map[string]any{"ok": true, "cleared": "disk"}
	case "aspLsp.clearCache", "aspLsp.server.clearCache":
		if !s.clearRuntimeProcessCachesContext(ctx) || !s.clearRuntimeDiskCacheContext(ctx) {
			return nil
		}
		s.clearWorkspaceDiagnosticsCaches(true)
		s.reportAsyncRPCWriteError(s.requestVisualRefresh("command.clearCache"))
		for _, uri := range s.openDocumentURIs() {
			if ctx.Err() != nil {
				return nil
			}
			_ = s.publishDiagnostics(uri)
		}
		return map[string]any{"ok": true, "cleared": "all"}
	case "aspLsp.server.cancelProgressTask":
		return s.cancelProgressTask(params)
	// Kept as an unadvertised compatibility hook for internal regression tests
	// and non-UI graph consumers. The VS Code graph screen no longer contributes
	// or invokes this command.
	case "aspLsp.server.buildGraph":
		return s.buildGraphContext(ctx, params)
	case "aspLsp.server.buildFlowchart":
		return s.buildFlowchartContext(ctx, params)
	case "aspLsp.server.buildNavigationGraph":
		return s.buildNavigationGraphContext(ctx, params)
	case "aspLsp.server.exportAnalysisExcel":
		return s.exportAnalysisExcel(ctx, params)
	case "aspLsp.server.previewWorkspaceFiles":
		return s.previewWorkspaceFiles(ctx, params)
	default:
		return map[string]any{"ok": false, "message": s.unknownCommandMessage(params.Command)}
	}
}

func maintenanceExecuteCommand(command string) bool {
	switch command {
	case "aspLsp.reindexWorkspace", "aspLsp.server.reindexWorkspace",
		"aspLsp.clearProcessCache", "aspLsp.server.clearProcessCache",
		"aspLsp.clearDiskCache", "aspLsp.server.clearDiskCache",
		"aspLsp.clearCache", "aspLsp.server.clearCache":
		return true
	default:
		return false
	}
}

func (s *Server) acquireMaintenanceCommand(ctx context.Context) (func(), bool) {
	if ctx == nil {
		ctx = context.Background()
	}
	select {
	case s.maintenanceCommandAdmission <- struct{}{}:
		return func() { <-s.maintenanceCommandAdmission }, true
	case <-ctx.Done():
		return func() {}, false
	}
}

func (s *Server) reindexWorkspaceContext(ctx context.Context) bool {
	if ctx == nil {
		ctx = context.Background()
	}
	s.workspaceIndexStateMu.Lock()
	defer s.workspaceIndexStateMu.Unlock()
	s.mu.Lock()
	if ctx.Err() != nil || s.shutdown {
		s.mu.Unlock()
		return false
	}
	s.graphCache = map[string]graph.Payload{}
	s.workspaceIncludeGraph = workspacepkg.NewWorkspaceIncludeGraph()
	s.workspaceIncludeGraphRevision++
	s.workspaceIncludeGraphComplete = false
	s.resetJavaScriptProjectLocked()
	s.mu.Unlock()
	s.invalidateChangedSourceSnapshots()
	s.clearAnalysisCache()
	s.clearWorkspaceReferenceCache()
	s.clearWorkspaceDiagnosticsCaches(true)
	s.clearSemanticTokenCache()
	s.invalidateGraphBackground()
	s.scheduleWorkspaceIndex("command.reindexWorkspace")
	return ctx.Err() == nil
}

func progressTaskIDArgument(arguments []any) string {
	if len(arguments) == 0 {
		return ""
	}
	argument, ok := arguments[0].(map[string]any)
	if !ok {
		return ""
	}
	id, _ := argument["id"].(string)
	return id
}

func (s *Server) clearWorkspaceDiagnosticsCaches(_ bool) {
	s.mu.Lock()
	s.workspaceDiagnosticsProcessCache = false
	s.workspaceDiagnosticsItems = map[string]workspaceDiagnosticsItemCacheEntry{}
	s.workspaceDiagnosticsRevisions = map[string]uint64{}
	s.mu.Unlock()
}

func (s *Server) logWorkspaceDiagnosticsWorkerStarted(uris []string) {
	if len(uris) == 0 {
		return
	}
	payloadBytes := 0
	for _, uri := range uris {
		doc := s.documentByURI(uri)
		if doc == nil {
			continue
		}
		payloadBytes += len(doc.Text)
	}
	s.logDebugSummary("[asp-lsp] vbscript.worker.dispatch: documents=" + strconv.Itoa(len(uris)))
	s.logDebugSummary("[asp-lsp] check.workspace.vbscript.diagnostics.worker: documents=" + strconv.Itoa(len(uris)))
	s.logDebugSummary("[asp-lsp] worker.payload.bytes: payload=" + strconv.Itoa(payloadBytes))
	s.logDebugSummary("[asp-lsp] vbscript.worker.started: documents=" + strconv.Itoa(len(uris)))
}

func (s *Server) logWorkspaceDiagnosticsWorkerTerminated(uris []string, started time.Time, state string) {
	if len(uris) == 0 {
		return
	}
	s.logDebugSummary("[asp-lsp] vbscript.worker." + state + ": documents=" + strconv.Itoa(len(uris)) + " " + formatElapsedSince(started))
}

type graphCommandArg struct {
	Scope                                   string `json:"scope"`
	URI                                     string `json:"uri"`
	IncludeAnalysisTypeDetails              bool   `json:"includeAnalysisTypeDetails"`
	IncludeRelatedIncludeTreesForUnresolved *bool  `json:"includeRelatedIncludeTreesForUnresolved"`
	IncludeIncomingDocumentIncludes         *bool  `json:"includeIncomingDocumentIncludes"`
	ShowIncomingDocumentIncludes            *bool  `json:"showIncomingDocumentIncludes"`
	ForceRelatedIncludeTreeAnalysis         bool   `json:"forceRelatedIncludeTreeAnalysis"`
}

type graphProgressReporter func(label, detail string, current, total int)

type graphPayloadBuildResult struct {
	payload    graph.Payload
	generation uint64
	complete   bool
	err        error
}

func incompleteGraphPayloadBuild(generation uint64, err error) graphPayloadBuildResult {
	if err == nil {
		err = errGraphCollectionGeneration
	}
	return graphPayloadBuildResult{generation: generation, err: err}
}

func graphScopeProgress(report graphProgressReporter, label string) graphProgressReporter {
	if report == nil {
		return nil
	}
	return func(_ string, detail string, current, total int) {
		report(label, detail, current, total)
	}
}

func (s *Server) buildGraphContext(ctx context.Context, params executeCommandParams) graph.Payload {
	if ctx == nil {
		ctx = context.Background()
	}
	if !s.waitForDocumentOpenAnalysisContext(ctx) {
		return graph.Payload{}
	}
	var arg graphCommandArg
	if len(params.Arguments) > 0 {
		_ = remarshal(params.Arguments[0], &arg)
	}
	if arg.Scope == "" {
		arg.Scope = "document"
	}
	if ctx.Err() != nil {
		return graph.Payload{}
	}
	if arg.Scope != "workspace" {
		if cached, ok := s.cachedGraphPayloadContext(ctx, s.graphCacheKey(arg)); ok {
			return cached
		}
	}
	if payload, ok := s.maybeBuildGraphInBackgroundContext(ctx, arg); ok {
		return payload
	}
	label := "graph." + arg.Scope
	taskID, _ := s.beginProgressTask(label, "analyzing", "graph", label+".collectDocuments", progressDetailForURI(arg.URI), 0, true)
	taskContext := s.registerProgressCancellationWithParent(taskID, ctx)
	defer s.unregisterProgressCancellation(taskID)
	state := "completed"
	defer func() {
		if taskContext.Err() != nil {
			state = "cancelled"
		}
		s.finishProgressTask(taskID, "graph", state)
	}()
	report := func(progressLabel, detail string, current, total int) {
		activeItems := []string{}
		if detail != "" {
			activeItems = append(activeItems, detail)
		}
		s.updateProgressTask(taskID, "graph", progressLabel, detail, current, total, activeItems, "running")
	}
	result := s.buildGraphPayloadContextResult(taskContext, arg, report)
	if !result.complete || !s.storeGraphPayloadContext(taskContext, result.generation, s.graphCacheKey(arg), result.payload) {
		return graph.Payload{}
	}
	return result.payload
}

func (s *Server) buildGraphPayloadContextResult(ctx context.Context, arg graphCommandArg, report graphProgressReporter) graphPayloadBuildResult {
	return s.buildGraphPayloadContextWithDocumentsResult(ctx, arg, report, nil, "", nil)
}

func (s *Server) buildGraphPayloadContextWithDocuments(ctx context.Context, arg graphCommandArg, report graphProgressReporter, workspaceDocuments []*core.ParsedDocument, workspaceRootURI string) (graph.Payload, bool) {
	result := s.buildGraphPayloadContextWithDocumentsResult(ctx, arg, report, workspaceDocuments, workspaceRootURI, nil)
	return result.payload, result.complete
}

func (s *Server) buildGraphPayloadContextWithDocumentsResult(ctx context.Context, arg graphCommandArg, report graphProgressReporter, workspaceDocuments []*core.ParsedDocument, workspaceRootURI string, expectedGenerationPtr *uint64) graphPayloadBuildResult {
	if ctx == nil {
		ctx = context.Background()
	}
	expectedGeneration := s.graphGenerationSnapshot()
	if expectedGenerationPtr != nil {
		expectedGeneration = *expectedGenerationPtr
	}
	if ctx.Err() != nil {
		return incompleteGraphPayloadBuild(expectedGeneration, ctx.Err())
	}
	workspaceCacheKey := ""
	if arg.Scope == "workspace" {
		workspaceCacheKey = s.graphCacheKey(arg)
		if cached, ok := s.cachedGraphPayloadContext(ctx, workspaceCacheKey); ok {
			return graphPayloadBuildResult{payload: cached, generation: expectedGeneration, complete: true}
		}
	}
	var payload graph.Payload
	switch arg.Scope {
	case "workspace":
		documents, rootURI := workspaceDocuments, workspaceRootURI
		if documents == nil {
			collection := s.workspaceGraphDocumentsContextWithProgressResult(ctx, true, graphScopeProgress(report, "graph.workspace.collectDocuments"))
			if !collection.complete {
				return incompleteGraphPayloadBuild(collection.generation, collection.err)
			}
			documents, rootURI = collection.documents, collection.rootURI
			expectedGeneration = collection.generation
		}
		if ctx.Err() != nil || !s.graphGenerationCurrent(ctx, expectedGeneration) {
			return incompleteGraphPayloadBuild(expectedGeneration, ctx.Err())
		}
		if s.settings.GraphShowIncomingFolderIncludes {
			incoming := s.appendIncomingFolderGraphDocumentsContextResult(ctx, expectedGeneration, documents)
			if !incoming.complete {
				return incompleteGraphPayloadBuild(incoming.generation, incoming.err)
			}
			documents = incoming.documents
		}
		s.logDebugVerbose("[asp-lsp] asp.graph.bulk.started: documents=" + strconv.Itoa(len(documents)))
		graphBulkStarted := time.Now()
		graphBulkState := "failed"
		defer func() {
			if graphBulkState == "complete" {
				return
			}
			if ctx.Err() != nil {
				graphBulkState = "cancelled"
			} else if !s.graphGenerationCurrent(context.Background(), expectedGeneration) {
				graphBulkState = "stale"
			}
			s.logDebugVerbose("[asp-lsp] asp.graph.bulk." + graphBulkState + ": documents=" + strconv.Itoa(len(documents)) + " " + formatElapsedSince(graphBulkStarted))
		}()
		s.logDebugVerbose("[asp-lsp] asp.graph.bulk.spill.write: documents=" + strconv.Itoa(len(documents)))
		var complete bool
		payload, complete = s.buildDocumentSetGraphWithProgressAtGeneration(ctx, expectedGeneration, "workspace", rootURI, documents, true, arg.IncludeAnalysisTypeDetails, report)
		if !complete {
			return incompleteGraphPayloadBuild(expectedGeneration, ctx.Err())
		}
		s.logDebugVerbose("[asp-lsp] asp.graph.bulk.complete: documents=" + strconv.Itoa(len(documents)))
		graphBulkState = "complete"
	case "folder":
		collection := s.navigationFolderDocumentsContextWithProgressResult(ctx, arg.URI, graphScopeProgress(report, "graph.folder.collectDocuments"))
		if !collection.complete {
			return incompleteGraphPayloadBuild(collection.generation, collection.err)
		}
		documents := collection.documents
		expectedGeneration = collection.generation
		if ctx.Err() != nil || !s.graphGenerationCurrent(ctx, expectedGeneration) {
			return incompleteGraphPayloadBuild(expectedGeneration, ctx.Err())
		}
		if s.settings.GraphShowIncomingFolderIncludes {
			incoming := s.appendIncomingFolderGraphDocumentsContextResult(ctx, expectedGeneration, documents)
			if !incoming.complete {
				return incompleteGraphPayloadBuild(incoming.generation, incoming.err)
			}
			documents = incoming.documents
		}
		var complete bool
		payload, complete = s.buildDocumentSetGraphWithProgressAtGeneration(ctx, expectedGeneration, "folder", arg.URI, documents, true, arg.IncludeAnalysisTypeDetails, report)
		if !complete {
			return incompleteGraphPayloadBuild(expectedGeneration, ctx.Err())
		}
	default:
		if ctx.Err() != nil || !s.graphGenerationCurrent(ctx, expectedGeneration) {
			return incompleteGraphPayloadBuild(expectedGeneration, ctx.Err())
		}
		if arg.URI == "" {
			payload = emptyGraphPayload("document", "", "")
			break
		}
		parsed, ok := s.parsedGraphDocumentContext(ctx, arg.URI)
		if !ok {
			payload = emptyGraphPayload("document", arg.URI, "")
			break
		}
		if ctx.Err() != nil || !s.graphGenerationCurrent(ctx, expectedGeneration) {
			return incompleteGraphPayloadBuild(expectedGeneration, ctx.Err())
		}
		showIncoming := s.settings.GraphShowIncomingDocumentIncludes
		if arg.IncludeIncomingDocumentIncludes != nil {
			showIncoming = *arg.IncludeIncomingDocumentIncludes
		}
		if arg.ShowIncomingDocumentIncludes != nil {
			showIncoming = *arg.ShowIncomingDocumentIncludes
		}
		if showIncoming {
			targetPath := cleanFileURIPath(arg.URI)
			if targetPath == "" {
				return incompleteGraphPayloadBuild(expectedGeneration, errGraphCollectionIncludeExpansion)
			}
			incoming := s.incomingIncludeDocumentsForTargetsAtGeneration(ctx, expectedGeneration,
				map[string]struct{}{targetPath: {}},
				map[string]struct{}{arg.URI: {}},
			)
			if !incoming.complete {
				return incompleteGraphPayloadBuild(incoming.generation, incoming.err)
			}
			documents := append([]*core.ParsedDocument{parsed}, incoming.documents...)
			var complete bool
			payload, complete = s.buildDocumentSetGraphWithProgressAtGeneration(ctx, expectedGeneration, "document", "", documents, true, arg.IncludeAnalysisTypeDetails, report)
			if !complete || ctx.Err() != nil {
				return incompleteGraphPayloadBuild(expectedGeneration, ctx.Err())
			}
			payload.URI = arg.URI
			payload.RootURI = canonicalGraphURI(arg.URI)
			markGraphRoot(&payload, canonicalGraphURI(arg.URI))
			setGraphPayloadSetting(&payload, "showIncomingDocumentIncludes", true)
			return graphPayloadBuildResult{payload: payload, generation: expectedGeneration, complete: true}
		}
		collection := s.documentGraphIncludeTreeContextCollectionResult(ctx, parsed, expectedGeneration)
		if !collection.complete {
			return incompleteGraphPayloadBuild(collection.generation, collection.err)
		}
		documents := collection.documents
		includeRelated := s.settings.GraphIncludeRelatedIncludeTrees
		if arg.IncludeRelatedIncludeTreesForUnresolved != nil {
			includeRelated = *arg.IncludeRelatedIncludeTreesForUnresolved
		}
		if includeRelated && (arg.ForceRelatedIncludeTreeAnalysis || graphDocumentNeedsRelatedIncludes(parsed)) {
			related := s.relatedIncludeTreeDocumentsContextResult(ctx, expectedGeneration, parsed, documents)
			if !related.complete {
				return incompleteGraphPayloadBuild(related.generation, related.err)
			}
			documents = append(documents, related.documents...)
		}
		var complete bool
		payload, complete = s.buildDocumentSetGraphWithProgressAtGeneration(ctx, expectedGeneration, "document", "", documents, true, arg.IncludeAnalysisTypeDetails, report)
		if !complete {
			return incompleteGraphPayloadBuild(expectedGeneration, ctx.Err())
		}
		payload.URI = canonicalGraphURI(arg.URI)
		payload.RootURI = canonicalGraphURI(arg.URI)
		markGraphRoot(&payload, canonicalGraphURI(arg.URI))
	}
	if ctx.Err() != nil || !s.graphGenerationCurrent(ctx, expectedGeneration) {
		return incompleteGraphPayloadBuild(expectedGeneration, ctx.Err())
	}
	return graphPayloadBuildResult{payload: payload, generation: expectedGeneration, complete: true}
}

type graphBackgroundBuild struct {
	ready      chan struct{}
	once       sync.Once
	mu         sync.Mutex
	payload    graph.Payload
	available  bool
	generation uint64
	ctx        context.Context
	cancel     context.CancelFunc
}

func newGraphBackgroundBuild() *graphBackgroundBuild {
	ctx, cancel := context.WithCancel(context.Background())
	return &graphBackgroundBuild{ready: make(chan struct{}), ctx: ctx, cancel: cancel}
}

func (b *graphBackgroundBuild) publish(payload graph.Payload, available bool, generation uint64) bool {
	published := false
	b.once.Do(func() {
		b.mu.Lock()
		b.payload = payload
		b.available = available
		b.generation = generation
		b.mu.Unlock()
		close(b.ready)
		published = true
	})
	return published
}

func (b *graphBackgroundBuild) wait(ctx context.Context) (graph.Payload, bool, uint64) {
	select {
	case <-b.ready:
		b.mu.Lock()
		defer b.mu.Unlock()
		return b.payload, b.available, b.generation
	case <-ctx.Done():
		return graph.Payload{}, false, 0
	}
}

func (s *Server) graphBackgroundBuildCurrent(ctx context.Context, cacheKey string, build *graphBackgroundBuild, generation uint64) bool {
	if ctx != nil && ctx.Err() != nil {
		return false
	}
	s.mu.Lock()
	defer s.mu.Unlock()
	return !s.shutdown && s.graphBackgroundBuilds[cacheKey] == build && s.graphGeneration == generation
}

func (s *Server) maybeBuildGraphInBackgroundContext(ctx context.Context, arg graphCommandArg) (graph.Payload, bool) {
	if ctx == nil {
		ctx = context.Background()
	}
	if ctx.Err() != nil {
		return graph.Payload{}, true
	}
	if arg.Scope != "workspace" {
		return graph.Payload{}, false
	}
	threshold := positiveEnvInt("ASP_LSP_TEST_GRAPH_BACKGROUND_MIN_DOCUMENTS", 1000)
	if threshold <= 0 {
		return graph.Payload{}, false
	}
	cacheKey := s.graphCacheKey(arg)
	if cached, ok := s.cachedGraphPayloadContext(ctx, cacheKey); ok {
		cached.Pending = lsp.BoolPtr(false)
		cached.CorrelationID = ""
		cached.BackgroundTaskID = ""
		return cached, true
	}
	if s.workspaceGraphSourceCount() < threshold {
		return graph.Payload{}, false
	}
	s.mu.Lock()
	if existing := s.graphBackgroundBuilds[cacheKey]; existing != nil {
		s.mu.Unlock()
		if hook := s.graphBackgroundTransitionTestHook; hook != nil {
			hook("waiting")
		}
		payload, available, generation := existing.wait(ctx)
		if available && !s.graphBackgroundBuildCurrent(ctx, cacheKey, existing, generation) {
			return graph.Payload{}, true
		}
		return payload, available || ctx.Err() != nil
	}
	if s.shutdown {
		s.mu.Unlock()
		return graph.Payload{}, true
	}
	build := newGraphBackgroundBuild()
	s.graphBackgroundBuilds[cacheKey] = build
	s.graphBackgroundWorkers.Add(1)
	s.mu.Unlock()
	workerStarted := false
	defer func() {
		if !workerStarted {
			s.graphBackgroundWorkers.Done()
		}
	}()
	if hook := s.graphBackgroundTransitionTestHook; hook != nil {
		hook("registered")
	}
	buildPublished := false
	defer func() {
		if buildPublished {
			return
		}
		build.publish(graph.Payload{}, false, 0)
		s.mu.Lock()
		if s.graphBackgroundBuilds[cacheKey] == build {
			delete(s.graphBackgroundBuilds, cacheKey)
		}
		s.mu.Unlock()
	}()
	correlationID := "go-graph-" + strconv.FormatInt(time.Now().UnixNano(), 10)
	backgroundTaskID, _ := s.beginProgressTask("graph.workspace", "analyzing", "graph", "graph.workspace.collectDocuments", "workspace", 0, true)
	debounce := positiveEnvInt("ASP_LSP_TEST_GRAPH_BACKGROUND_DEBOUNCE_MS", 150)
	backgroundContext := s.registerProgressCancellationWithParent(backgroundTaskID, build.ctx)
	taskTransferred := false
	defer func() {
		if taskTransferred {
			return
		}
		s.mu.Lock()
		delete(s.graphBackgroundTasks, backgroundTaskID)
		s.mu.Unlock()
		s.unregisterProgressCancellation(backgroundTaskID)
	}()
	s.mu.Lock()
	registered := !s.shutdown && s.graphBackgroundBuilds[cacheKey] == build
	if registered {
		s.graphBackgroundTasks[backgroundTaskID] = build.cancel
	}
	s.mu.Unlock()
	if !registered {
		s.finishProgressTask(backgroundTaskID, "graph", "cancelled")
		return graph.Payload{}, true
	}
	report := func(label, detail string, current, total int) {
		activeItems := []string{}
		if detail != "" {
			activeItems = append(activeItems, detail)
		}
		s.updateProgressTask(backgroundTaskID, "graph", label, detail, current, total, activeItems, "running")
	}
	collection := s.workspaceGraphDocumentsContextWithProgressResult(backgroundContext, true, graphScopeProgress(report, "graph.workspace.collectDocuments"))
	if !collection.complete {
		s.finishProgressTask(backgroundTaskID, "graph", "cancelled")
		return graph.Payload{}, true
	}
	documents, rootURI := collection.documents, collection.rootURI
	generation := collection.generation
	partialDocuments := documents
	if len(partialDocuments) > 300 {
		partialDocuments = partialDocuments[:300]
	}
	partial, complete := s.buildDocumentSetGraphWithProgressAtGeneration(backgroundContext, generation, "workspace", rootURI, partialDocuments, true, arg.IncludeAnalysisTypeDetails, report)
	if !complete || backgroundContext.Err() != nil || !s.graphGenerationCurrent(backgroundContext, generation) {
		s.finishProgressTask(backgroundTaskID, "graph", "cancelled")
		return graph.Payload{}, true
	}
	partial.Settings = s.graphPayloadSettings()
	setGraphPayloadSetting(&partial, "partialMaxDocuments", 300)
	partial.Pending = lsp.BoolPtr(true)
	partial.CorrelationID = correlationID
	partial.BackgroundTaskID = backgroundTaskID
	if hook := s.graphBackgroundTransitionTestHook; hook != nil {
		hook("beforePublish")
	}
	if !build.publish(partial, true, generation) || !s.graphBackgroundBuildCurrent(backgroundContext, cacheKey, build, generation) {
		s.finishProgressTask(backgroundTaskID, "graph", "cancelled")
		return graph.Payload{}, true
	}
	buildPublished = true
	taskTransferred = true
	workerStarted = true
	go func() {
		defer s.graphBackgroundWorkers.Done()
		defer func() {
			s.mu.Lock()
			delete(s.graphBackgroundTasks, backgroundTaskID)
			if s.graphBackgroundBuilds[cacheKey] == build {
				delete(s.graphBackgroundBuilds, cacheKey)
			}
			s.mu.Unlock()
			s.unregisterProgressCancellation(backgroundTaskID)
		}()
		if debounce > 0 {
			timer := time.NewTimer(time.Duration(debounce) * time.Millisecond)
			select {
			case <-backgroundContext.Done():
				timer.Stop()
				s.finishProgressTask(backgroundTaskID, "graph", "cancelled")
				s.publishGraphCancellation(correlationID, arg)
				return
			case <-timer.C:
			}
		}
		if backgroundContext.Err() != nil {
			s.finishProgressTask(backgroundTaskID, "graph", "cancelled")
			s.publishGraphCancellation(correlationID, arg)
			return
		}
		result := s.buildGraphPayloadContextWithDocumentsResult(backgroundContext, arg, report, documents, rootURI, &generation)
		s.mu.Lock()
		stale := generation != s.graphGeneration
		s.mu.Unlock()
		if !result.complete || stale || backgroundContext.Err() != nil {
			state := "cancelled"
			if stale {
				state = "stale"
			}
			s.finishProgressTask(backgroundTaskID, "graph", state)
			s.publishGraphCancellation(correlationID, arg)
			return
		}
		payload := result.payload
		payload.Pending = lsp.BoolPtr(false)
		payload.CorrelationID = correlationID
		if !s.storeGraphPayloadContext(backgroundContext, generation, cacheKey, payload) {
			s.finishProgressTask(backgroundTaskID, "graph", "cancelled")
			s.publishGraphCancellation(correlationID, arg)
			return
		}
		s.finishProgressTask(backgroundTaskID, "graph", "completed")
	}()
	return partial, true
}

func (s *Server) publishGraphCancellation(correlationID string, arg graphCommandArg) {
	_, _ = correlationID, arg
}

func (s *Server) cancelProgressTask(params executeCommandParams) map[string]any {
	return map[string]any{"ok": s.cancelProgressTaskByID(progressTaskIDArgument(params.Arguments))}
}

func (s *Server) cancelProgressTaskByID(id string) bool {
	if strings.TrimSpace(id) == "" {
		return false
	}
	s.mu.Lock()
	cancel := s.progressCancellations[id]
	if cancel == nil {
		cancel = s.graphBackgroundTasks[id]
	}
	s.mu.Unlock()
	if cancel == nil {
		return false
	}
	cancel()
	return true
}

func (s *Server) invalidateGraphBackground() {
	s.workspaceIndexDiskCommitMu.Lock()
	s.mu.Lock()
	s.graphGeneration++
	s.graphCache = map[string]graph.Payload{}
	cancellations := make([]context.CancelFunc, 0, len(s.graphBackgroundTasks))
	for _, cancel := range s.graphBackgroundTasks {
		cancellations = append(cancellations, cancel)
	}
	builds := make([]*graphBackgroundBuild, 0, len(s.graphBackgroundBuilds))
	for _, build := range s.graphBackgroundBuilds {
		builds = append(builds, build)
	}
	s.graphBackgroundBuilds = map[string]*graphBackgroundBuild{}
	s.mu.Unlock()
	s.workspaceIndexDiskCommitMu.Unlock()
	for _, build := range builds {
		build.cancel()
		build.publish(graph.Payload{}, false, 0)
	}
	for _, cancel := range cancellations {
		cancel()
	}
}

func (s *Server) stopGraphBackgroundTasks() {
	s.mu.Lock()
	cancellations := make([]context.CancelFunc, 0, len(s.graphBackgroundTasks))
	for _, cancel := range s.graphBackgroundTasks {
		cancellations = append(cancellations, cancel)
	}
	builds := make([]*graphBackgroundBuild, 0, len(s.graphBackgroundBuilds))
	for _, build := range s.graphBackgroundBuilds {
		builds = append(builds, build)
	}
	s.graphBackgroundBuilds = map[string]*graphBackgroundBuild{}
	s.mu.Unlock()
	for _, build := range builds {
		build.cancel()
		build.publish(graph.Payload{}, false, 0)
	}
	for _, cancel := range cancellations {
		cancel()
	}
	s.graphBackgroundWorkers.Wait()
}
func (s *Server) graphCacheKey(arg graphCommandArg) string {
	s.mu.Lock()
	includeRelated := s.settings.GraphIncludeRelatedIncludeTrees
	showIncoming := s.settings.GraphShowIncomingDocumentIncludes
	graphSettings := []string{
		strconv.FormatBool(s.settings.GraphUseReverseIncludeIndex),
		strconv.FormatBool(s.settings.GraphWorkerSymbolExtraction),
		strconv.FormatBool(s.settings.GraphIncludeRelatedIncludeTrees),
		strconv.FormatBool(s.settings.GraphShowIncomingDocumentIncludes),
		strconv.FormatBool(s.settings.GraphShowIncomingFolderIncludes),
	}
	s.mu.Unlock()
	if arg.IncludeRelatedIncludeTreesForUnresolved != nil {
		includeRelated = *arg.IncludeRelatedIncludeTreesForUnresolved
	}
	if arg.IncludeIncomingDocumentIncludes != nil {
		showIncoming = *arg.IncludeIncomingDocumentIncludes
	}
	if arg.ShowIncomingDocumentIncludes != nil {
		showIncoming = *arg.ShowIncomingDocumentIncludes
	}
	relatedIncludes := "default"
	if arg.IncludeRelatedIncludeTreesForUnresolved != nil {
		relatedIncludes = strconv.FormatBool(*arg.IncludeRelatedIncludeTreesForUnresolved)
	}
	return strings.Join([]string{
		arg.Scope,
		canonicalGraphURI(arg.URI),
		strconv.FormatBool(arg.IncludeAnalysisTypeDetails),
		relatedIncludes,
		strconv.FormatBool(arg.ForceRelatedIncludeTreeAnalysis),
		strconv.FormatBool(showIncoming),
		strconv.FormatBool(includeRelated),
		strconv.FormatBool(arg.ForceRelatedIncludeTreeAnalysis),
		strings.Join(graphSettings, "\x00"),
	}, "\x00")
}

func (s *Server) cachedGraphPayload(key string) (graph.Payload, bool) {
	return s.cachedGraphPayloadContext(context.Background(), key)
}

type graphPayloadRestoreBeforeCommitContextKey struct{}

func (s *Server) cachedGraphPayloadContext(ctx context.Context, key string) (graph.Payload, bool) {
	if ctx == nil {
		ctx = context.Background()
	}
	if ctx.Err() != nil {
		return graph.Payload{}, false
	}
	s.mu.Lock()
	expectedGeneration := s.graphGeneration
	payload, ok := s.graphCache[key]
	s.mu.Unlock()
	if ok {
		if ctx.Err() != nil {
			return graph.Payload{}, false
		}
		return payload, true
	}
	if payload, restored := s.readDiskGraphPayload(key); restored {
		if ctx.Err() != nil {
			return graph.Payload{}, false
		}
		if hook, ok := ctx.Value(graphPayloadRestoreBeforeCommitContextKey{}).(func(*Server)); ok && hook != nil {
			hook(s)
		}
		s.workspaceIndexDiskCommitMu.Lock()
		s.mu.Lock()
		if ctx.Err() != nil || s.graphGeneration != expectedGeneration {
			s.mu.Unlock()
			s.workspaceIndexDiskCommitMu.Unlock()
			return graph.Payload{}, false
		}
		if cached, ok := s.graphCache[key]; ok {
			s.mu.Unlock()
			s.workspaceIndexDiskCommitMu.Unlock()
			return cached, true
		}
		s.graphCache[key] = payload
		s.scheduleMemoryPressureCheckLocked("graphPayload.restore")
		s.mu.Unlock()
		s.workspaceIndexDiskCommitMu.Unlock()
		return payload, true
	}
	return graph.Payload{}, false
}

func (s *Server) storeGraphPayload(key string, payload graph.Payload) {
	_ = s.storeGraphPayloadContext(context.Background(), s.graphGenerationSnapshot(), key, payload)
}

func (s *Server) storeGraphPayloadContext(ctx context.Context, expectedGeneration uint64, key string, payload graph.Payload) bool {
	if ctx == nil {
		ctx = context.Background()
	}
	s.workspaceIndexDiskCommitMu.Lock()
	s.mu.Lock()
	if ctx.Err() != nil || s.graphGeneration != expectedGeneration {
		s.mu.Unlock()
		s.workspaceIndexDiskCommitMu.Unlock()
		return false
	}
	s.graphCache[key] = payload
	s.scheduleMemoryPressureCheckLocked("graphPayload.store")
	s.mu.Unlock()
	keyName := "graph-payload\x00" + workspacepkg.DiskContentHash(key)
	s.runAsyncDiskCacheWriteKey(keyName, func() {
		s.writeDiskGraphPayload(key, payload, expectedGeneration)
	})
	s.workspaceIndexDiskCommitMu.Unlock()
	return true
}

func emptyGraphPayload(scope, uri, rootURI string) graph.Payload {
	return graph.Payload{
		Scope:   scope,
		URI:     uri,
		RootURI: rootURI,
		Nodes:   []graph.Node{},
		Edges:   []graph.Edge{},
		Links:   []graph.Edge{},
		Stats:   map[string]int{"files": 0, "declarations": 0, "includes": 0, "links": 0},
	}
}

func setGraphPayloadSetting(payload *graph.Payload, key string, value any) {
	if payload.Settings == nil {
		payload.Settings = map[string]any{}
	}
	payload.Settings[key] = value
}

func (s *Server) graphPayloadSettings() map[string]any {
	s.mu.Lock()
	settings := s.settings
	s.mu.Unlock()
	hiddenNodeCategories := []string{}
	for _, category := range graphNodeCategoryOrder {
		if !graphNodeCategoryVisible(category, settings) {
			hiddenNodeCategories = append(hiddenNodeCategories, category)
		}
	}
	hiddenLinkCategories := []string{}
	for _, category := range graphLinkCategoryOrder {
		if !graphLinkCategoryVisible(category, settings) {
			hiddenLinkCategories = append(hiddenLinkCategories, category)
		}
	}
	return map[string]any{
		"initialViewMode":                         settings.GraphInitialViewMode,
		"hideSingleNodes":                         settings.GraphHideSingleNodes,
		"hideUnreferencedGlobalSymbols":           settings.GraphHideUnreferencedGlobalSymbols,
		"showOutgoingSelectionLinks":              settings.GraphShowOutgoingSelectionLinks,
		"showIncomingDocumentIncludes":            settings.GraphShowIncomingDocumentIncludes,
		"showIncomingFolderIncludes":              settings.GraphShowIncomingFolderIncludes,
		"includeRelatedIncludeTreesForUnresolved": settings.GraphIncludeRelatedIncludeTrees,
		"hiddenNodeCategories":                    hiddenNodeCategories,
		"hiddenLinkCategories":                    hiddenLinkCategories,
		"useReverseIncludeIndex":                  settings.GraphUseReverseIncludeIndex,
		"workerSymbolExtraction":                  settings.GraphWorkerSymbolExtraction,
	}
}

var graphNodeCategoryOrder = []string{
	"root",
	"file",
	"missingInclude",
	"function",
	"sub",
	"class",
	"method",
	"methodFunction",
	"methodSub",
	"property",
	"member",
	"globalVariable",
	"implicitGlobalVariable",
	"globalConstant",
	"localVariable",
	"localConstant",
	"parameter",
	"unresolvedFunction",
	"unresolved",
}

var graphLinkCategoryOrder = []string{
	"include",
	"declares",
	"references",
	"assignments",
	"calls",
	"unresolvedReference",
	"member",
}

func graphNodeCategoryVisible(category string, settings serverSettings) bool {
	switch category {
	case "root":
		return settings.GraphShowRootNodes
	case "file", "missingInclude":
		return settings.GraphShowFileNodes
	case "function":
		return settings.GraphShowFunctionNodes
	case "sub":
		return settings.GraphShowSubNodes
	case "class":
		return settings.GraphShowClassNodes
	case "method":
		return settings.GraphShowMethodNodes
	case "methodFunction":
		return settings.GraphShowMethodFunctionNodes
	case "methodSub":
		return settings.GraphShowMethodSubNodes
	case "property":
		return settings.GraphShowPropertyNodes
	case "member":
		return settings.GraphShowMemberNodes
	case "globalVariable", "implicitGlobalVariable":
		return settings.GraphShowGlobalVariableNodes
	case "globalConstant":
		return settings.GraphShowGlobalConstantNodes
	case "localVariable":
		return settings.GraphShowLocalVariableNodes
	case "localConstant":
		return settings.GraphShowLocalConstantNodes
	case "parameter":
		return settings.GraphShowParameterNodes
	case "unresolvedFunction", "unresolved":
		return settings.GraphShowUnresolvedNodes
	default:
		return true
	}
}

func graphLinkCategoryVisible(category string, settings serverSettings) bool {
	switch category {
	case "include":
		return settings.GraphShowIncludeLinks
	case "declares":
		return settings.GraphShowDeclareLinks
	case "references":
		return settings.GraphShowReferenceLinks
	case "assignments":
		return settings.GraphShowAssignmentLinks
	case "calls":
		return settings.GraphShowCallLinks
	case "unresolvedReference":
		return settings.GraphShowUnresolvedLinks
	case "member":
		return settings.GraphShowMemberLinks
	default:
		return true
	}
}

func (s *Server) documentGraphIncludeTreeContextCollectionResult(ctx context.Context, parsed *core.ParsedDocument, generation uint64) graphDocumentCollectionResult {
	if ctx == nil {
		ctx = context.Background()
	}
	if ctx.Err() != nil {
		return incompleteGraphDocumentCollection("", generation, ctx.Err())
	}
	if !s.graphGenerationCurrent(ctx, generation) {
		return incompleteGraphDocumentCollection("", generation, errGraphCollectionGeneration)
	}
	if parsed == nil {
		return completeGraphDocumentCollection(nil, "", generation)
	}
	included, complete := s.includedDocumentsContextResult(ctx, parsed)
	if ctx.Err() != nil {
		return incompleteGraphDocumentCollection(parsed.URI, generation, ctx.Err())
	}
	if !complete {
		return incompleteGraphDocumentCollection(parsed.URI, generation, errGraphCollectionIncludeExpansion)
	}
	if !s.graphGenerationCurrent(ctx, generation) {
		return incompleteGraphDocumentCollection(parsed.URI, generation, errGraphCollectionGeneration)
	}
	documents := append([]*core.ParsedDocument{parsed}, included...)
	return completeGraphDocumentCollection(documents, parsed.URI, generation)
}

func (s *Server) documentGraphIncludeTreeContextResult(ctx context.Context, parsed *core.ParsedDocument) ([]*core.ParsedDocument, bool) {
	result := s.documentGraphIncludeTreeContextCollectionResult(ctx, parsed, s.graphGenerationSnapshot())
	if !result.complete {
		return nil, false
	}
	return result.documents, true
}

func graphDocumentNeedsRelatedIncludes(parsed *core.ParsedDocument) bool {
	if parsed == nil {
		return false
	}
	declaredNames := map[string]struct{}{}
	for _, declaration := range graphVBDeclarations(parsed) {
		if declaration.Implicit {
			continue
		}
		declaredNames[strings.ToLower(declaration.Name)] = struct{}{}
	}
	return len(graphImplicitGlobalDeclarations(parsed, declaredNames)) > 0
}

func (s *Server) relatedIncludeTreeDocumentsContextResult(ctx context.Context, generation uint64, parsed *core.ParsedDocument, current []*core.ParsedDocument) graphDocumentCollectionResult {
	if ctx == nil {
		ctx = context.Background()
	}
	if parsed == nil {
		return completeGraphDocumentCollection(nil, "", generation)
	}
	if ctx.Err() != nil {
		return incompleteGraphDocumentCollection(parsed.URI, generation, ctx.Err())
	}
	if !s.graphGenerationCurrent(ctx, generation) {
		return incompleteGraphDocumentCollection(parsed.URI, generation, errGraphCollectionGeneration)
	}
	seen := map[string]struct{}{}
	for _, document := range current {
		if document != nil {
			seen[workspacepkg.FileIdentityKeyFromURI(document.URI)] = struct{}{}
		}
	}
	targetPath := cleanFileURIPath(parsed.URI)
	if targetPath == "" {
		return completeGraphDocumentCollection(nil, parsed.URI, generation)
	}
	incoming := s.incomingIncludeDocumentsForTargetsAtGeneration(ctx, generation,
		map[string]struct{}{targetPath: {}}, seen)
	if !incoming.complete {
		return incompleteGraphDocumentCollection(parsed.URI, incoming.generation, incoming.err)
	}
	related := make([]*core.ParsedDocument, 0, len(incoming.documents))
	for _, parent := range incoming.documents {
		if ctx.Err() != nil {
			return incompleteGraphDocumentCollection(parsed.URI, generation, ctx.Err())
		}
		if !s.graphGenerationCurrent(ctx, generation) {
			return incompleteGraphDocumentCollection(parsed.URI, generation, errGraphCollectionGeneration)
		}
		if parent == nil {
			continue
		}
		parentKey := workspacepkg.FileIdentityKeyFromURI(parent.URI)
		if _, ok := seen[parentKey]; ok {
			continue
		}
		seen[parentKey] = struct{}{}
		related = append(related, parent)
		for _, include := range parent.Includes {
			if ctx.Err() != nil {
				return incompleteGraphDocumentCollection(parsed.URI, generation, ctx.Err())
			}
			details, ok := s.includeTargetDetailsForModeContext(ctx, parent.URI, include.Path, include.Mode)
			if ctx.Err() != nil {
				return incompleteGraphDocumentCollection(parsed.URI, generation, ctx.Err())
			}
			if !ok || !details.Exists || details.Path == "" {
				continue
			}
			uri := filePathURI(details.Path)
			childKey := workspacepkg.FileIdentityKeyFromURI(uri)
			if _, ok := seen[childKey]; ok {
				continue
			}
			child, ok := s.parsedGraphDocumentContext(ctx, uri)
			if !ok {
				if ctx.Err() != nil {
					return incompleteGraphDocumentCollection(parsed.URI, generation, ctx.Err())
				}
				return incompleteGraphDocumentCollection(parsed.URI, generation, errGraphCollectionIncludeExpansion)
			}
			if !s.graphGenerationCurrent(ctx, generation) {
				return incompleteGraphDocumentCollection(parsed.URI, generation, errGraphCollectionGeneration)
			}
			seen[workspacepkg.FileIdentityKeyFromURI(child.URI)] = struct{}{}
			related = append(related, child)
		}
	}
	if ctx.Err() != nil {
		return incompleteGraphDocumentCollection(parsed.URI, generation, ctx.Err())
	}
	if !s.graphGenerationCurrent(ctx, generation) {
		return incompleteGraphDocumentCollection(parsed.URI, generation, errGraphCollectionGeneration)
	}
	return completeGraphDocumentCollection(related, parsed.URI, generation)
}

func markGraphRoot(payload *graph.Payload, rootURI string) {
	if payload == nil || rootURI == "" {
		return
	}
	for index := range payload.Nodes {
		if payload.Nodes[index].Kind == "file" && workspacepkg.SameFileIdentityURI(payload.Nodes[index].URI, rootURI) {
			payload.Nodes[index].IsRoot = true
		}
	}
}
