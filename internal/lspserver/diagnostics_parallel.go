package lspserver

import (
	"context"
	"strconv"
	"strings"
	"sync"
	"sync/atomic"
	"time"

	"github.com/yottonoko/classic-web-system-lsp/internal/core"
	"github.com/yottonoko/classic-web-system-lsp/internal/lsp"
	"github.com/yottonoko/classic-web-system-lsp/internal/vbscript"
	workspacepkg "github.com/yottonoko/classic-web-system-lsp/internal/workspace"
)

type diagnosticsSnapshot struct {
	diagnostics     []lsp.Diagnostic
	targetRevisions map[string]diagnosticTargetRevision
	version         int
	ok              bool
}

const localSyntaxDiagnosticsAnalysisKey = "lspserver.local-syntax-diagnostics.v1"

const (
	htmlDiagnosticsAnalysisKey = "lspserver.html-diagnostics.v1"
	cssDiagnosticsAnalysisKey  = "lspserver.css-diagnostics.v1"
	jsDiagnosticsAnalysisKey   = "lspserver.javascript-diagnostics.v1"
)

type languageDiagnosticsCache struct {
	mu        sync.Mutex
	inflight  chan struct{}
	complete  bool
	items     []lsp.Diagnostic
	memoryGen atomic.Uint64
}

func (c *languageDiagnosticsCache) RuntimeAnalysisMemoryOwnerGeneration() uint64 {
	if c == nil {
		return 0
	}
	return c.memoryGen.Load()
}

func (c *languageDiagnosticsCache) EstimateBytes() int64 {
	if c == nil {
		return 0
	}
	c.mu.Lock()
	defer c.mu.Unlock()
	bytes := int64(128 + cap(c.items)*128)
	for _, diagnostic := range c.items {
		bytes += int64(len(diagnostic.Source)+len(diagnostic.Message))*2 + int64(cap(diagnostic.Tags))*8
	}
	return bytes
}

type localSyntaxDiagnosticGroups struct {
	IfSyntaxDiagnostics string
	Parser              []lsp.Diagnostic
	Declarations        []lsp.Diagnostic
	Calls               []lsp.Diagnostic
	VBScript            []lsp.Diagnostic
}

func (s *Server) localSyntaxDiagnosticGroups(parsed *core.ParsedDocument, ifSyntaxDiagnostics string) localSyntaxDiagnosticGroups {
	if parsed == nil {
		return localSyntaxDiagnosticGroups{}
	}
	if cached, ok := parsed.LoadRuntimeAnalysis(localSyntaxDiagnosticsAnalysisKey); ok {
		if groups, valid := cached.(localSyntaxDiagnosticGroups); valid && groups.IfSyntaxDiagnostics == ifSyntaxDiagnostics {
			return groups
		}
	}
	groups := localSyntaxDiagnosticGroups{
		IfSyntaxDiagnostics: ifSyntaxDiagnostics,
		Parser:              core.Diagnostics(parsed),
		Declarations:        declarationSyntaxDiagnostics(parsed),
		Calls:               callSyntaxDiagnostics(parsed),
		VBScript: vbscript.SyntaxDiagnostics(parsed, vbscript.SyntaxOptions{
			IfSyntaxDiagnostics: ifSyntaxDiagnostics,
		}),
	}
	parsed.StoreRuntimeAnalysis(localSyntaxDiagnosticsAnalysisKey, groups)
	return groups
}

func diagnosticsForEmbeddedLanguage(parsed *core.ParsedDocument, language core.EmbeddedLanguage, key string, compute func() []lsp.Diagnostic) []lsp.Diagnostic {
	return diagnosticsForEmbeddedLanguages(parsed, []core.EmbeddedLanguage{language}, key, compute)
}

func diagnosticsForEmbeddedLanguages(parsed *core.ParsedDocument, languages []core.EmbeddedLanguage, key string, compute func() []lsp.Diagnostic) []lsp.Diagnostic {
	return diagnosticsForEmbeddedLanguagesContext(context.Background(), parsed, languages, key, compute)
}

func diagnosticsForEmbeddedLanguagesContext(ctx context.Context, parsed *core.ParsedDocument, languages []core.EmbeddedLanguage, key string, compute func() []lsp.Diagnostic) []lsp.Diagnostic {
	if parsed == nil {
		return nil
	}
	if ctx == nil {
		ctx = context.Background()
	}
	candidate := &languageDiagnosticsCache{}
	actual, _ := parsed.LoadOrStoreRuntimeAnalysis(key, candidate)
	cache := actual.(*languageDiagnosticsCache)
	for {
		cache.mu.Lock()
		if cache.complete {
			items := cloneDiagnostics(cache.items)
			cache.mu.Unlock()
			parsed.ReleasePreviousRuntimeAnalysis(key, cache)
			return items
		}
		if inflight := cache.inflight; inflight != nil {
			cache.mu.Unlock()
			select {
			case <-inflight:
				continue
			case <-ctx.Done():
				return nil
			}
		}
		cache.inflight = make(chan struct{})
		inflight := cache.inflight
		cache.mu.Unlock()

		var items []lsp.Diagnostic
		reused := false
		previousText, hasPrevious := parsed.PreviousRevisionText()
		unaffected := hasPrevious
		for _, language := range languages {
			if parsed.ChangeImpact.Affects(language) {
				unaffected = false
				break
			}
		}
		if unaffected {
			if cached, ok := parsed.LoadPreviousRuntimeAnalysis(key); ok {
				if previous, valid := cached.(*languageDiagnosticsCache); valid {
					previous.mu.Lock()
					previousItems := cloneDiagnostics(previous.items)
					previousComplete := previous.complete
					previous.mu.Unlock()
					if previousComplete {
						if shifted, reusable := shiftDiagnosticsAcrossIncrementalRevision(previousItems, previousText, parsed); reusable {
							items = shifted
							reused = true
						}
					}
				}
			}
		}
		if !reused {
			items = compute()
		}
		cache.mu.Lock()
		if ctx.Err() == nil {
			cache.items = cloneDiagnostics(items)
			cache.complete = true
			cache.memoryGen.Add(1)
		}
		close(inflight)
		cache.inflight = nil
		complete := cache.complete
		cache.mu.Unlock()
		if complete {
			parsed.ReleasePreviousRuntimeAnalysis(key, cache)
			return cloneDiagnostics(items)
		}
		return nil
	}
}

func shiftDiagnosticsAcrossIncrementalRevision(items []lsp.Diagnostic, previousText string, current *core.ParsedDocument) ([]lsp.Diagnostic, bool) {
	if current == nil {
		return nil, false
	}
	impact := current.ChangeImpact
	oldDocument := core.NewTextDocument(current.URI, "classic-asp", 0, previousText)
	newDocument := core.NewTextDocument(current.URI, "classic-asp", 0, current.Text)
	delta := impact.NewEnd - impact.OldEnd
	shifted := cloneDiagnostics(items)
	for index := range shifted {
		start := oldDocument.OffsetAt(shifted[index].Range.Start)
		end := oldDocument.OffsetAt(shifted[index].Range.End)
		switch {
		case end <= impact.OldStart:
			continue
		case start >= impact.OldEnd:
			shifted[index].Range = newDocument.Range(start+delta, end+delta)
		default:
			return nil, false
		}
	}
	return shifted, true
}

func (s *Server) diagnosticsSnapshot(ctx context.Context, uri string) diagnosticsSnapshot {
	return s.diagnosticsSnapshotWithProgress(ctx, uri, nil)
}

type diagnosticsProgressReporter func(label string, current, total int)

func (s *Server) diagnosticsSnapshotWithProgress(ctx context.Context, uri string, progress diagnosticsProgressReporter) diagnosticsSnapshot {
	const totalSteps = 15
	if ctx == nil {
		ctx = context.Background()
	}
	if ctx.Err() != nil {
		return diagnosticsSnapshot{}
	}
	_, parseSpan := newRuntimeLogSpan(ctx)
	s.logMeasuredStepStarted(uri, "analysis.parse", parseSpan)
	parseStarted := time.Now()
	doc, parsed := s.parsed(uri)
	if doc == nil || parsed == nil {
		s.logMeasuredStep(uri, "analysis.parse", parseStarted, 0, parseSpan)
		return diagnosticsSnapshot{}
	}
	s.logMeasuredStep(uri, "analysis.parse", parseStarted, len(parsed.Regions), parseSpan)
	if progress != nil {
		progress("diagnostics.syntax", 1, totalSteps)
	}
	if ctx.Err() != nil {
		return diagnosticsSnapshot{}
	}
	_, databaseSpan := newRuntimeLogSpan(ctx)
	s.logMeasuredStepStarted(uri, "analysisDatabase.diagnostics", databaseSpan)
	databaseStarted := time.Now()
	databaseFinished := false
	defer func() {
		if !databaseFinished {
			s.logMeasuredStepTerminated(uri, "analysisDatabase.diagnostics", diagnosticsTerminationState(ctx), databaseStarted, 0, databaseSpan)
		}
	}()
	if diagnostics, ok := s.readDiskDiagnosticsContext(ctx, doc, parsed); ok {
		if ctx.Err() != nil {
			return diagnosticsSnapshot{}
		}
		targetRevisions, complete := s.diagnosticTargetRevisionsContext(ctx, parsed)
		if !complete || ctx.Err() != nil {
			return diagnosticsSnapshot{}
		}
		s.logMeasuredStep(uri, "analysisDatabase.diagnostics.hit", databaseStarted, len(diagnostics), databaseSpan)
		databaseFinished = true
		if progress != nil {
			progress("document.analysis.cache", totalSteps-1, totalSteps)
		}
		if ctx.Err() != nil {
			return diagnosticsSnapshot{}
		}
		return diagnosticsSnapshot{diagnostics: dedupeDiagnostics(diagnostics), targetRevisions: targetRevisions, version: doc.Version, ok: true}
	}
	s.logMeasuredStep(uri, "analysisDatabase.diagnostics.miss", databaseStarted, 0, databaseSpan)
	databaseFinished = true
	if progress != nil {
		progress("document.analysis.cache", 2, totalSteps)
	}
	ctx, diagnosticsSpan := newRuntimeLogSpan(ctx)
	s.logMeasuredStepStarted(uri, "check.diagnostics", diagnosticsSpan)
	diagnosticsStarted := time.Now()
	diagnosticsFinished := false
	defer func() {
		if !diagnosticsFinished {
			s.logMeasuredStepTerminated(uri, "check.diagnostics", diagnosticsTerminationState(ctx), diagnosticsStarted, 0, diagnosticsSpan)
		}
	}()
	rawDiagnostics, targetRevisions, complete := s.diagnosticsForParsedWithProgressResult(ctx, parsed, 2, totalSteps, progress)
	if !complete || ctx.Err() != nil {
		return diagnosticsSnapshot{}
	}
	diagnostics := dedupeDiagnostics(s.localizeDiagnostics(rawDiagnostics))
	if ctx.Err() != nil {
		return diagnosticsSnapshot{}
	}
	s.logMeasuredStep(uri, "check.diagnostics", diagnosticsStarted, len(diagnostics), diagnosticsSpan)
	diagnosticsFinished = true
	if ctx.Err() != nil {
		return diagnosticsSnapshot{}
	}
	s.writeDiskDiagnosticsContext(ctx, doc, parsed, diagnostics)
	if ctx.Err() != nil {
		return diagnosticsSnapshot{}
	}
	s.scheduleMemoryPressureCheck("diagnostics.completed")
	return diagnosticsSnapshot{
		diagnostics:     diagnostics,
		targetRevisions: targetRevisions,
		version:         doc.Version,
		ok:              true,
	}
}

func diagnosticsTerminationState(ctx context.Context) string {
	if ctx != nil && ctx.Err() != nil {
		return "cancelled"
	}
	return "stale"
}

func (s *Server) diagnosticsForParsed(ctx context.Context, parsed *core.ParsedDocument) []lsp.Diagnostic {
	return s.diagnosticsForParsedWithProgress(ctx, parsed, 0, 12, nil)
}

func (s *Server) diagnosticsForParsedWithProgress(ctx context.Context, parsed *core.ParsedDocument, completedBase, total int, progress diagnosticsProgressReporter) []lsp.Diagnostic {
	diagnostics, _, complete := s.diagnosticsForParsedWithProgressResult(ctx, parsed, completedBase, total, progress)
	if !complete {
		return nil
	}
	return diagnostics
}

func (s *Server) diagnosticsForParsedWithProgressResult(ctx context.Context, parsed *core.ParsedDocument, completedBase, total int, progress diagnosticsProgressReporter) ([]lsp.Diagnostic, map[string]diagnosticTargetRevision, bool) {
	if ctx == nil {
		ctx = context.Background()
	}
	if ctx.Err() != nil {
		return nil, nil, false
	}
	if parsed == nil {
		return nil, nil, true
	}
	s.mu.Lock()
	ifSyntaxDiagnostics := s.settings.VBScriptIfSyntaxDiagnostics
	deadCodeDiagnostics := s.settings.VBScriptDeadCodeDiagnostics
	unusedVBScriptDiagnostics := s.settings.VBScriptUnusedDiagnostics
	implicitGlobalDiagnostics := s.settings.VBScriptImplicitGlobalDiagnostics
	unusedJavaScriptDiagnostics := s.settings.JavaScriptUnusedDiagnostics
	locale := s.settings.Locale
	checkJS := s.settings.CheckJS
	s.mu.Unlock()
	_, preparationSpan := newRuntimeLogSpan(ctx)
	preparationStarted := time.Now()
	preparationFinished := false
	s.logMeasuredStepStarted(parsed.URI, "check.prepare", preparationSpan)
	defer func() {
		if !preparationFinished {
			s.logMeasuredStepTerminated(parsed.URI, "check.prepare", diagnosticsTerminationState(ctx), preparationStarted, 0, preparationSpan)
		}
	}()
	localSyntax := s.localSyntaxDiagnosticGroups(parsed, ifSyntaxDiagnostics)
	includedDocuments, includesComplete := s.includedDocumentsForDiagnosticsContext(ctx, parsed)
	if !includesComplete || ctx.Err() != nil {
		return nil, nil, false
	}
	includedServerObjects := map[string]struct{}{}
	for _, document := range includedDocuments {
		for _, declaration := range serverObjectDeclarations(document) {
			includedServerObjects[strings.ToLower(declaration.Name)] = struct{}{}
		}
	}
	if ctx.Err() != nil {
		return nil, nil, false
	}
	for name := range includedVBVariableInlayGlobalNames(includedDocuments) {
		includedServerObjects[name] = struct{}{}
	}
	for name := range s.legacyUndefinedGlobalNames(ctx) {
		includedServerObjects[name] = struct{}{}
	}

	if ctx.Err() != nil {
		return nil, nil, false
	}
	s.logMeasuredStep(parsed.URI, "check.prepare", preparationStarted, len(includedDocuments), preparationSpan)
	preparationFinished = true

	const taskCount = 12
	taskNames := [...]string{
		"parser", "declarations", "calls", "vbscript.syntax", "vbscript.deadCode", "vbscript.unused",
		"vbscript.naming", "includes", "vbscript.types", "html", "css", "javascript",
	}
	groups := make([][]lsp.Diagnostic, taskCount)
	var completed atomic.Int64
	var completedTasks atomic.Uint32
	var completedGroups atomic.Uint32
	groupComplete := make([]bool, taskCount)
	for index := range groupComplete {
		groupComplete[index] = true
	}
	s.analysisWorkers.parallelForBulk(ctx, taskCount, func(workerCtx context.Context, index int) {
		workerCtx, taskSpan := newRuntimeLogSpan(workerCtx)
		s.logMeasuredStepStarted(parsed.URI, "check."+taskNames[index], taskSpan)
		started := time.Now()
		defer func() {
			if workerCtx.Err() == nil && groupComplete[index] {
				s.logMeasuredStep(parsed.URI, "check."+taskNames[index], started, len(groups[index]), taskSpan)
				completedGroups.Or(1 << index)
			} else {
				s.logMeasuredStepTerminated(parsed.URI, "check."+taskNames[index], diagnosticsTerminationState(workerCtx), started, len(groups[index]), taskSpan)
			}
			if progress != nil {
				completedTasks.Or(1 << index)
				current := completedBase + int(completed.Add(1))
				progress(diagnosticsPendingProgressLabel(taskNames[:], completedTasks.Load()), current, total)
			}
		}()
		switch index {
		case 0:
			groups[index] = localSyntax.Parser
		case 1:
			groups[index] = localSyntax.Declarations
		case 2:
			groups[index] = localSyntax.Calls
		case 3:
			groups[index] = localSyntax.VBScript
		case 4:
			if deadCodeDiagnostics {
				groups[index] = vbscript.DeadCodeDiagnostics(parsed)
			}
		case 5:
			if unusedVBScriptDiagnostics {
				groups[index] = vbscriptUsageDiagnosticsWithGlobals(parsed, locale, includedServerObjects)
			}
			if implicitGlobalDiagnostics && !hasVBOptionExplicit(parsed) {
				groups[index] = append(groups[index], implicitGlobalVBScriptDiagnostics(parsed, locale, includedServerObjects)...)
			}
		case 6:
			groups[index] = s.vbscriptNamingDiagnostics(parsed)
		case 7:
			groups[index], groupComplete[index] = s.includeDiagnosticsContext(workerCtx, parsed)
		case 8:
			groups[index], groupComplete[index] = s.vbscriptTypeDiagnosticsContextResult(workerCtx, parsed)
		case 9:
			groups[index] = diagnosticsForEmbeddedLanguagesContext(workerCtx, parsed, []core.EmbeddedLanguage{core.LanguageHTML}, htmlDiagnosticsAnalysisKey, func() []lsp.Diagnostic {
				return s.html.Diagnostics(parsed)
			})
		case 10:
			groups[index] = diagnosticsForEmbeddedLanguagesContext(workerCtx, parsed, []core.EmbeddedLanguage{core.LanguageCSS}, cssDiagnosticsAnalysisKey, func() []lsp.Diagnostic {
				return s.cssDiagnostics(parsed)
			})
		case 11:
			cacheKey := jsDiagnosticsAnalysisKey
			s.mu.Lock()
			javascriptGeneration := s.javascriptDocumentGeneration
			s.mu.Unlock()
			cacheKey += ".generation-" + strconv.FormatUint(javascriptGeneration, 10)
			if checkJS {
				cacheKey += ".check"
			}
			if unusedJavaScriptDiagnostics {
				cacheKey += ".unused"
			}
			groups[index] = diagnosticsForEmbeddedLanguagesContext(workerCtx, parsed, []core.EmbeddedLanguage{core.LanguageJavaScript, core.LanguageJScript}, cacheKey, func() []lsp.Diagnostic {
				return s.javaScriptDiagnosticsForParsed(workerCtx, parsed, checkJS, unusedJavaScriptDiagnostics)
			})
		}
	})
	if ctx.Err() != nil || completedGroups.Load() != (uint32(1)<<taskCount)-1 {
		return nil, nil, false
	}
	targetRevisions, revisionsComplete := s.diagnosticTargetRevisionsForDocumentsContext(ctx, parsed, includedDocuments)
	if !revisionsComplete || ctx.Err() != nil {
		return nil, nil, false
	}

	diagnosticCount := 0
	for _, group := range groups {
		diagnosticCount += len(group)
	}
	if diagnosticCount == 0 {
		return nil, targetRevisions, true
	}
	diagnostics := make([]lsp.Diagnostic, 0, diagnosticCount)
	for _, group := range groups {
		diagnostics = append(diagnostics, group...)
	}
	return dedupeDiagnostics(diagnostics), targetRevisions, true
}

func (s *Server) diagnosticTargetRevisionsContext(ctx context.Context, parsed *core.ParsedDocument) (map[string]diagnosticTargetRevision, bool) {
	if parsed == nil {
		return nil, true
	}
	if ctx == nil {
		ctx = context.Background()
	}
	includedDocuments, complete := s.includedDocumentsForDiagnosticsContext(ctx, parsed)
	if !complete || ctx.Err() != nil {
		return nil, false
	}
	return s.diagnosticTargetRevisionsForDocumentsContext(ctx, parsed, includedDocuments)
}

func (s *Server) diagnosticTargetRevisionsForDocumentsContext(ctx context.Context, parsed *core.ParsedDocument, includedDocuments []*core.ParsedDocument) (map[string]diagnosticTargetRevision, bool) {
	if ctx == nil {
		ctx = context.Background()
	}
	revisions := make(map[string]diagnosticTargetRevision)
	documents := append([]*core.ParsedDocument{parsed}, includedDocuments...)
	for _, document := range documents {
		if ctx.Err() != nil {
			return nil, false
		}
		if document == nil {
			continue
		}
		key := workspacepkg.FileIdentityKeyFromURI(document.URI)
		if _, exists := revisions[key]; exists {
			continue
		}
		revision, ok := s.diagnosticTargetRevisionForParsedContext(ctx, document)
		if !ok {
			return nil, false
		}
		revisions[key] = revision
	}
	return revisions, true
}

func (s *Server) includedDocumentsForDiagnosticsContext(ctx context.Context, parsed *core.ParsedDocument) ([]*core.ParsedDocument, bool) {
	if parsed == nil {
		return nil, true
	}
	if ctx == nil {
		ctx = context.Background()
	}
	if ctx.Err() != nil {
		return nil, false
	}
	units, complete := s.vbscriptIncludeExecutionUnitsContext(ctx, parsed)
	if !complete || ctx.Err() != nil {
		return nil, false
	}
	documents := make([]*core.ParsedDocument, 0, len(units))
	seen := map[string]struct{}{workspacepkg.FileIdentityKeyFromURI(parsed.URI): {}}
	for _, unit := range units {
		if ctx.Err() != nil {
			return nil, false
		}
		if unit.document == nil {
			continue
		}
		key := workspacepkg.FileIdentityKeyFromURI(unit.document.URI)
		if _, exists := seen[key]; exists {
			continue
		}
		seen[key] = struct{}{}
		documents = append(documents, unit.document)
	}
	return documents, true
}

func (s *Server) javaScriptDiagnosticsForParsed(workerCtx context.Context, parsed *core.ParsedDocument, checkJS, unusedJavaScriptDiagnostics bool) []lsp.Diagnostic {
	var positions []lsp.Position
	seenLanguages := map[core.EmbeddedLanguage]struct{}{}
	document := core.SourceDocument(parsed)
	for _, region := range parsed.Regions {
		if region.Language != core.LanguageJavaScript && region.Language != core.LanguageJScript {
			continue
		}
		if _, seen := seenLanguages[region.Language]; seen {
			continue
		}
		seenLanguages[region.Language] = struct{}{}
		positions = append(positions, document.PositionAt(region.ContentStart))
	}
	if len(positions) == 0 {
		return nil
	}
	if checkJS {
		s.logJavaScriptDiagnosticsWorker(parsed)
	}
	methods := []string{"textDocument/syntacticDiagnostic"}
	if checkJS {
		methods = []string{"textDocument/diagnostic"}
	} else if unusedJavaScriptDiagnostics {
		methods = append(methods, "textDocument/semanticDiagnostic")
	}
	var diagnostics []lsp.Diagnostic
	for _, position := range positions {
		for _, method := range methods {
			var report javaScriptDiagnosticReport
			if !s.javaScriptLanguageServiceRequestWithLockContext(workerCtx, workerCtx, parsed.URI, position, method, nil, &report) {
				continue
			}
			for _, diagnostic := range report.Items {
				unused := isJavaScriptUnusedDiagnostic(diagnostic)
				if method == "textDocument/semanticDiagnostic" && !checkJS && !unused {
					continue
				}
				if unused {
					diagnostic.Severity = lsp.DiagnosticSeverityHint
					diagnostic.Tags = []lsp.DiagnosticTag{lsp.DiagnosticTagUnnecessary}
					diagnostic.Source = "asp-lsp-typescript-unused"
				} else {
					diagnostic.Source = "asp-lsp-typescript"
				}
				diagnostics = append(diagnostics, diagnostic)
			}
		}
	}
	if !unusedJavaScriptDiagnostics {
		diagnostics = filterDiagnosticsBySource(diagnostics, "asp-lsp-typescript-unused")
	}
	return dedupeDiagnostics(diagnostics)
}

func diagnosticsProgressLabel(task string) string {
	switch task {
	case "includes":
		return "diagnostics.include"
	case "vbscript.types", "javascript":
		return "diagnostics.project"
	case "vbscript.deadCode", "vbscript.unused":
		return "diagnostics.projectFast"
	default:
		return "diagnostics.syntax"
	}
}

func diagnosticsPendingProgressLabel(tasks []string, completed uint32) string {
	for _, label := range []string{"diagnostics.project", "diagnostics.include", "diagnostics.projectFast", "diagnostics.syntax"} {
		for index, task := range tasks {
			if completed&(1<<index) == 0 && diagnosticsProgressLabel(task) == label {
				return label
			}
		}
	}
	return "diagnostics.project"
}

func filterDiagnosticsBySource(diagnostics []lsp.Diagnostic, excludedSource string) []lsp.Diagnostic {
	filtered := diagnostics[:0]
	for _, diagnostic := range diagnostics {
		if diagnostic.Source != excludedSource {
			filtered = append(filtered, diagnostic)
		}
	}
	return filtered
}

func isJavaScriptUnusedDiagnostic(diagnostic lsp.Diagnostic) bool {
	for _, tag := range diagnostic.Tags {
		if tag == lsp.DiagnosticTagUnnecessary {
			return true
		}
	}
	switch code := diagnostic.Code.(type) {
	case float64:
		return isJavaScriptUnusedDiagnosticCode(int(code))
	case int:
		return isJavaScriptUnusedDiagnosticCode(code)
	case int32:
		return isJavaScriptUnusedDiagnosticCode(int(code))
	case int64:
		return isJavaScriptUnusedDiagnosticCode(int(code))
	default:
		return false
	}
}

func isJavaScriptUnusedDiagnosticCode(code int) bool {
	switch code {
	case 6133, 6138, 6192, 6196, 6198:
		return true
	default:
		return false
	}
}

func (s *Server) cssDiagnostics(parsed *core.ParsedDocument) []lsp.Diagnostic {
	return s.css.Diagnostics(parsed)
}

func (s *Server) documentVersionMatches(uri string, version int) bool {
	s.mu.Lock()
	defer s.mu.Unlock()
	doc := s.documents[uri]
	return doc != nil && doc.Version == version
}
