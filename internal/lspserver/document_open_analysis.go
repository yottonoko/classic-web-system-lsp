package lspserver

import (
	"context"
	"time"

	"github.com/yottonoko/classic-web-system-lsp/internal/core"
	"github.com/yottonoko/classic-web-system-lsp/internal/lsp"
)

type documentOpenAnalysisJob struct {
	uri                   string
	generation            uint64
	documentVersion       int
	defaultLanguage       string
	sourceFingerprint     workspaceArtifactFingerprint
	publishDiagnostics    bool
	cancel                context.CancelFunc
	active                bool
	pending               *documentOpenAnalysisInput
	generationDone        chan struct{}
	doneClosed            bool
	publicationInFlight   bool
	publicationGeneration uint64
}

type documentOpenAnalysisInput struct {
	ctx                context.Context
	document           *core.TextDocument
	parsed             *core.ParsedDocument
	workspaceDocument  *core.TextDocument
	referencesChanged  bool
	publishDiagnostics bool
	generation         uint64
}

func (s *Server) publishInitialSyntaxDiagnostics(doc *core.TextDocument, parsed *core.ParsedDocument) error {
	if doc == nil || parsed == nil {
		return nil
	}
	s.mu.Lock()
	ifSyntaxDiagnostics := s.settings.VBScriptIfSyntaxDiagnostics
	s.mu.Unlock()
	groups := s.localSyntaxDiagnosticGroups(parsed, ifSyntaxDiagnostics)
	diagnostics := append([]lsp.Diagnostic{}, groups.Parser...)
	diagnostics = append(diagnostics, groups.Declarations...)
	diagnostics = append(diagnostics, groups.Calls...)
	diagnostics = append(diagnostics, groups.VBScript...)
	diagnostics = s.localizeDiagnostics(dedupeDiagnostics(diagnostics))
	if len(diagnostics) == 0 {
		return nil
	}
	written, err := s.writeDiagnosticNotificationForVersion(doc.URI, doc.Version, diagnostics)
	if err != nil || !written {
		return err
	}
	s.logDebugVerbose("[asp-lsp] diagnostics.initialSyntax.published: " + doc.URI)
	return nil
}

func (s *Server) scheduleDocumentOpenAnalysis(doc *core.TextDocument, parsed *core.ParsedDocument, workspaceDoc *core.TextDocument, referencesChanged bool, parents ...context.Context) {
	s.scheduleDocumentRevisionAnalysis(doc, parsed, workspaceDoc, referencesChanged, true, parents...)
}

func (s *Server) scheduleDocumentChangeAnalysis(doc *core.TextDocument, parsed *core.ParsedDocument, parents ...context.Context) {
	s.scheduleDocumentRevisionAnalysis(doc, parsed, nil, false, false, parents...)
}

func (s *Server) scheduleDocumentRevisionAnalysis(doc *core.TextDocument, parsed *core.ParsedDocument, workspaceDoc *core.TextDocument, referencesChanged, publishDiagnostics bool, parents ...context.Context) {
	if doc == nil || parsed == nil {
		return
	}
	document := doc.Clone()
	var workspaceDocument *core.TextDocument
	if workspaceDoc != nil {
		workspaceDocument = workspaceDoc.Clone()
	}
	parent := context.Background()
	if len(parents) > 0 && parents[0] != nil {
		if span, ok := parents[0].Value(runtimeLogSpanKey{}).(runtimeLogSpan); ok {
			parent = context.WithValue(parent, runtimeLogSpanKey{}, span)
		}
	}
	ctx, cancel := context.WithCancel(parent)
	key := diagnosticTimerKey(document.URI)
	var previousCancel context.CancelFunc
	var worker *documentOpenAnalysisJob
	startWorker := false
	s.mu.Lock()
	if s.shutdown {
		s.mu.Unlock()
		cancel()
		return
	}
	s.documentOpenAnalysisSequence++
	generation := s.documentOpenAnalysisSequence
	input := documentOpenAnalysisInput{
		ctx: ctx, document: document, parsed: parsed, workspaceDocument: workspaceDocument,
		referencesChanged: referencesChanged, publishDiagnostics: publishDiagnostics,
		generation: generation,
	}
	if previous := s.documentOpenAnalysisJobs[key]; previous != nil {
		previousCancel = previous.cancel
		// A generation that has already published its manifest must keep the
		// barrier open until its queued side effects and graph invalidation finish.
		// If publication has not started, the old generation can be released
		// immediately while the worker is coalesced onto the new input.
		if !previous.publicationInFlight {
			closeDocumentOpenAnalysisGenerationLocked(previous)
			previous.generationDone = make(chan struct{})
			previous.doneClosed = false
		}
		previous.generation = generation
		previous.documentVersion = document.Version
		previous.defaultLanguage = s.settings.DefaultLanguage
		previous.sourceFingerprint = workspaceFingerprint(document.Text)
		previous.publishDiagnostics = publishDiagnostics
		previous.cancel = cancel
		previous.active = true
		previous.pending = &input
		worker = previous
	} else {
		worker = &documentOpenAnalysisJob{
			uri:                document.URI,
			generation:         generation,
			documentVersion:    document.Version,
			defaultLanguage:    s.settings.DefaultLanguage,
			sourceFingerprint:  workspaceFingerprint(document.Text),
			publishDiagnostics: publishDiagnostics,
			cancel:             cancel,
			active:             true,
			pending:            &input,
			generationDone:     make(chan struct{}),
		}
		s.documentOpenAnalysisJobs[key] = worker
		s.documentOpenAnalysisWorkers.Add(1)
		startWorker = true
	}
	s.mu.Unlock()
	if previousCancel != nil {
		previousCancel()
	}
	if startWorker {
		go s.runDocumentOpenAnalysis(worker)
	}
}

func (s *Server) runDocumentOpenAnalysis(job *documentOpenAnalysisJob) {
	defer s.documentOpenAnalysisWorkers.Done()
	for {
		input, ctx, ok := s.nextDocumentOpenAnalysisRevision(job)
		if !ok {
			return
		}
		s.runDocumentOpenAnalysisRevision(ctx, input)
	}
}

func (s *Server) nextDocumentOpenAnalysisRevision(job *documentOpenAnalysisJob) (documentOpenAnalysisInput, context.Context, bool) {
	if job == nil {
		return documentOpenAnalysisInput{}, nil, false
	}
	key := diagnosticTimerKey(job.uri)
	s.mu.Lock()
	if s.documentOpenAnalysisJobs[key] != job || job.pending == nil {
		if s.documentOpenAnalysisJobs[key] == job {
			job.active = false
			delete(s.documentOpenAnalysisJobs, key)
			closeDocumentOpenAnalysisGenerationLocked(job)
		}
		s.mu.Unlock()
		return documentOpenAnalysisInput{}, nil, false
	}
	input := *job.pending
	job.pending = nil
	s.mu.Unlock()
	if input.ctx == nil {
		input.ctx = context.Background()
	}
	return input, input.ctx, true
}

func (s *Server) runDocumentOpenAnalysisRevision(ctx context.Context, input documentOpenAnalysisInput) {
	manifestPublished := false
	publicationFinished := false
	finishPublication := func() {
		if publicationFinished {
			return
		}
		publicationFinished = true
		s.finishDocumentOpenAnalysisPublication(input)
	}
	defer finishPublication()
	if !input.publishDiagnostics {
		timer := time.NewTimer(35 * time.Millisecond)
		select {
		case <-ctx.Done():
			if !timer.Stop() {
				<-timer.C
			}
			return
		case <-timer.C:
		}
	}
	if !s.documentOpenAnalysisCurrent(ctx, input) {
		return
	}
	snapshot := s.buildDocumentOpenWorkspaceArtifactSnapshotContext(ctx, input.parsed)
	if snapshot == nil || !s.documentOpenAnalysisCurrent(ctx, input) {
		return
	}
	s.prepareDocumentOpenWorkspaceReferenceCountReuse(ctx, input)
	if !s.documentOpenAnalysisCurrent(ctx, input) {
		return
	}
	revision := s.applyWorkspaceDocumentRevisionWithSnapshotIfCurrentAndPublished(input.document, input.parsed, snapshot, func() bool {
		return s.documentOpenAnalysisCurrentLocked(ctx, input)
	}, func() {
		manifestPublished = true
		s.markDocumentOpenAnalysisPublicationLocked(input)
	})
	if revision.Stale {
		if manifestPublished && workspaceArtifactDeltaChangesGraph(revision.Delta) {
			s.invalidateGraphBackground()
		}
		return
	}
	if input.referencesChanged && revision.Manifest == nil {
		var workspaceParsed *core.ParsedDocument
		if input.workspaceDocument != nil {
			workspaceParsed = s.parseTextDocument(input.workspaceDocument, s.settings.DefaultLanguage)
		}
		s.invalidateWorkspaceReferencesForParsedChange(workspaceParsed, input.parsed)
		s.invalidateWorkspaceDiagnosticsForParsedChange(workspaceParsed, input.parsed)
	}
	if !revision.Duplicate && workspaceArtifactDeltaChangesGraph(revision.Delta) {
		s.invalidateGraphBackground()
	}
	if input.parsed != nil && revision.Manifest == nil && isWorkspaceASPFile(fileURIPath(input.document.URI)) {
		s.refreshWorkspaceIncludeGraphFile(input.parsed)
	}
	finishPublication()
	if !s.documentOpenAnalysisCurrent(ctx, input) {
		return
	}
	if !input.publishDiagnostics {
		s.logDebugVerbose("[asp-lsp] documentChange.artifacts.ready: " + input.document.URI)
		return
	}
	version, current := s.currentDocumentOpenAnalysisVersion(input)
	if !current {
		return
	}
	if err := s.publishDiagnosticsContext(ctx, input.document.URI, version); err != nil {
		s.reportAsyncRPCWriteError(err)
		if ctx.Err() == nil && !isRPCWriteFatal(err) {
			s.logServerWarning("[asp-lsp] diagnostics.documentOpen.failed: " + err.Error())
		}
		return
	}
	if s.documentOpenAnalysisCurrent(ctx, input) {
		s.logDocumentOpenPrewarm(input.document.URI)
	}
}

func (s *Server) prepareDocumentOpenWorkspaceReferenceCountReuse(ctx context.Context, input documentOpenAnalysisInput) {
	if ctx == nil || ctx.Err() != nil || input.parsed == nil {
		return
	}
	documentID := workspaceDocumentIDFromURI(input.parsed.URI)
	s.mu.Lock()
	previous := s.workspaceArtifacts[documentID]
	pending := s.referencePendingDocumentCountReuse[documentID]
	if previous == nil || previous.CST == nil || previous.SourceFingerprint == workspaceFingerprint(input.parsed.Text) {
		s.mu.Unlock()
		return
	}
	if pending != nil && pending.generation == s.referenceGeneration {
		pending.currentSourceFingerprint = workspaceFingerprint(input.parsed.Text)
		s.mu.Unlock()
		return
	}
	previousParsed := previous.CST
	s.mu.Unlock()
	if ctx.Err() != nil {
		return
	}
	// Count-reuse preparation is deliberately outside the notification path. It
	// may derive a full declaration plan and reference shard for the prior
	// revision; the artifact worker can discard it if this revision is superseded.
	s.captureWorkspaceReferenceDocumentCountReuse(previousParsed, input.parsed)
}

func (s *Server) documentOpenAnalysisCurrent(ctx context.Context, input documentOpenAnalysisInput) bool {
	if ctx == nil || ctx.Err() != nil || input.document == nil {
		return false
	}
	s.mu.Lock()
	current := s.documentOpenAnalysisCurrentLocked(ctx, input)
	s.mu.Unlock()
	return current
}

func (s *Server) documentOpenAnalysisCurrentLocked(ctx context.Context, input documentOpenAnalysisInput) bool {
	if ctx == nil || ctx.Err() != nil || input.document == nil {
		return false
	}
	job := s.documentOpenAnalysisJobs[diagnosticTimerKey(input.document.URI)]
	doc := s.openDocumentByURILocked(input.document.URI)
	return job != nil && job.active && job.generation == input.generation && doc != nil &&
		doc.Text == input.document.Text
}

func (s *Server) documentOpenAnalysisMatchesSourceLocked(uri, text, defaultLanguage string) bool {
	job := s.documentOpenAnalysisJobs[diagnosticTimerKey(uri)]
	return job != nil && job.active && job.sourceFingerprint == workspaceFingerprint(text) &&
		job.defaultLanguage == defaultLanguage
}

func (s *Server) workspaceArtifactMatchesParsedLocked(doc *core.TextDocument, parsed *core.ParsedDocument) bool {
	if doc == nil || parsed == nil || doc.Text != parsed.Text {
		return false
	}
	manifest := s.workspaceArtifacts[workspaceDocumentIDFromURI(doc.URI)]
	if manifest == nil {
		return false
	}
	parserSettingsFingerprint := workspaceFingerprint(struct {
		Settings        string
		DefaultLanguage core.EmbeddedLanguage
	}{s.settings.DefaultLanguage, parsed.DefaultLanguage})
	return manifest.SourceFingerprint == workspaceFingerprint(doc.Text) &&
		manifest.ParserSettingsFingerprint == parserSettingsFingerprint
}

func (s *Server) parsedCacheHasIncompatibleLanguageLocked(uri, text, defaultLanguage string) bool {
	for _, key := range []string{parsedDocumentCacheKey(uri), parsedTextCacheKey(uri)} {
		entry, ok := s.parsedCache[key]
		if ok && entry.Parsed != nil && entry.Text == text && entry.DefaultLanguage != defaultLanguage {
			return true
		}
	}
	return false
}

func (s *Server) currentDocumentOpenAnalysisVersion(input documentOpenAnalysisInput) (int, bool) {
	if input.document == nil {
		return 0, false
	}
	s.mu.Lock()
	doc := s.openDocumentByURILocked(input.document.URI)
	if doc == nil || doc.Text != input.document.Text {
		s.mu.Unlock()
		return 0, false
	}
	version := doc.Version
	s.mu.Unlock()
	return version, true
}

func (s *Server) documentChangeAnalysisPending(uri string, version int) bool {
	key := diagnosticTimerKey(uri)
	s.mu.Lock()
	job := s.documentOpenAnalysisJobs[key]
	pending := job != nil && job.active && !job.publishDiagnostics && job.documentVersion == version
	s.mu.Unlock()
	return pending
}

func (s *Server) cancelDocumentOpenAnalysis(uri string) {
	key := diagnosticTimerKey(uri)
	s.mu.Lock()
	job := s.documentOpenAnalysisJobs[key]
	var cancel context.CancelFunc
	if job != nil {
		job.active = false
		job.pending = nil
		if !job.publicationInFlight {
			closeDocumentOpenAnalysisGenerationLocked(job)
		}
		cancel = job.cancel
	}
	s.mu.Unlock()
	if cancel != nil {
		cancel()
	}
}

func (s *Server) stopDocumentOpenAnalysisWorkers() {
	s.mu.Lock()
	jobs := make([]*documentOpenAnalysisJob, 0, len(s.documentOpenAnalysisJobs))
	for _, job := range s.documentOpenAnalysisJobs {
		jobs = append(jobs, job)
	}
	s.documentOpenAnalysisJobs = map[string]*documentOpenAnalysisJob{}
	cancels := make([]context.CancelFunc, 0, len(jobs))
	for _, job := range jobs {
		if job != nil {
			job.active = false
			job.pending = nil
			if !job.publicationInFlight {
				closeDocumentOpenAnalysisGenerationLocked(job)
			}
		}
		if job != nil && job.cancel != nil {
			cancels = append(cancels, job.cancel)
		}
	}
	s.mu.Unlock()
	for _, cancel := range cancels {
		if cancel != nil {
			cancel()
		}
	}
	s.documentOpenAnalysisWorkers.Wait()
	s.mu.Lock()
	for _, job := range jobs {
		closeDocumentOpenAnalysisGenerationLocked(job)
	}
	s.mu.Unlock()
}

// markDocumentOpenAnalysisPublicationLocked records that the current worker
// swapped in its manifest. The caller holds s.mu; the marker remains set while
// queueing and graph invalidation complete.
func (s *Server) markDocumentOpenAnalysisPublicationLocked(input documentOpenAnalysisInput) {
	if input.document == nil {
		return
	}
	job := s.documentOpenAnalysisJobs[diagnosticTimerKey(input.document.URI)]
	if job == nil || !job.active || job.generation != input.generation {
		return
	}
	job.publicationInFlight = true
	job.publicationGeneration = input.generation
}

func (s *Server) finishDocumentOpenAnalysisPublication(input documentOpenAnalysisInput) {
	if input.document == nil {
		return
	}
	s.mu.Lock()
	job := s.documentOpenAnalysisJobs[diagnosticTimerKey(input.document.URI)]
	if job == nil || !job.publicationInFlight || job.publicationGeneration != input.generation {
		s.mu.Unlock()
		return
	}
	job.publicationInFlight = false
	job.publicationGeneration = 0
	if !job.active && job.pending == nil {
		closeDocumentOpenAnalysisGenerationLocked(job)
	}
	s.mu.Unlock()
}

func closeDocumentOpenAnalysisGenerationLocked(job *documentOpenAnalysisJob) {
	if job == nil || job.generationDone == nil || job.doneClosed {
		return
	}
	close(job.generationDone)
	job.doneClosed = true
}
