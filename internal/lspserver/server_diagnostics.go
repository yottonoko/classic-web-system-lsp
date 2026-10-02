package lspserver

import (
	"context"
	"errors"
	"path/filepath"
	"sort"
	"strconv"
	"strings"
	"sync"
	"time"

	"github.com/yottonoko/classic-web-system-lsp/internal/core"
	"github.com/yottonoko/classic-web-system-lsp/internal/lsp"
	workspacepkg "github.com/yottonoko/classic-web-system-lsp/internal/workspace"
)

func (s *Server) publishDiagnostics(uri string) error {
	return s.publishDiagnosticsContext(context.Background(), uri, -1)
}

func (s *Server) publishDiagnosticsContext(ctx context.Context, uri string, expectedVersion int) error {
	if s.shouldPublishStagedDiagnostics(uri) {
		return s.publishStagedDiagnosticsContext(ctx, uri, expectedVersion)
	}
	return s.publishFinalDiagnosticsContext(ctx, uri, true, true, expectedVersion)
}

func (s *Server) publishFastDiagnosticsContext(ctx context.Context, uri string, expectedVersion int) error {
	const total = 1
	taskID, _ := s.beginProgressTask("diagnostics.fast", "analyzing", "diagnostics", "diagnostics.syntax", progressDetailForURI(uri), total, false)
	state := "failed"
	defer func() { s.finishProgressTask(taskID, "diagnostics", state) }()
	s.logDebugTrace("diagnostics.start", "[asp-lsp] diagnostics.start: "+uri)
	doc, parsed := s.parsed(uri)
	if doc == nil || parsed == nil || doc.Version != expectedVersion || !s.diagnosticsContextCurrent(ctx, uri, expectedVersion) {
		state = diagnosticsContextState(ctx)
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
		state = "completed"
		return nil
	}
	if !s.diagnosticsContextCurrent(ctx, uri, expectedVersion) {
		s.logDebugSummary("[asp-lsp] diagnostics.fast.stale: " + uri)
		state = diagnosticsContextState(ctx)
		return nil
	}
	written, err := s.writeDiagnosticNotificationForVersion(uri, expectedVersion, diagnostics)
	if err != nil {
		return err
	}
	if !written {
		state = "stale"
		return nil
	}
	s.updateProgressTask(taskID, "diagnostics", "document.analysis.ready", progressDetailForURI(uri), total, total, nil, "completed")
	state = "completed"
	s.logDebugVerbose("[asp-lsp] diagnostics.fast.published: " + uri)
	return nil
}

func (s *Server) shouldPublishStagedDiagnostics(uri string) bool {
	_, parsed := s.parsed(uri)
	if parsed == nil {
		return false
	}
	return len(core.Diagnostics(parsed)) > 0
}

func (s *Server) publishStagedDiagnosticsContext(ctx context.Context, uri string, expectedVersion int) error {
	const total = 4
	taskID, _ := s.beginProgressTask("diagnostics.staged", "analyzing", "diagnostics", "diagnostics.syntax", progressDetailForURI(uri), total, false)
	state := "failed"
	defer func() { s.finishProgressTask(taskID, "diagnostics", state) }()
	doc, parsed := s.parsed(uri)
	if doc == nil || parsed == nil {
		return nil
	}
	version := doc.Version
	if expectedVersion >= 0 && version != expectedVersion {
		state = "stale"
		return nil
	}
	if !s.diagnosticsContextCurrent(ctx, uri, version) {
		state = diagnosticsContextState(ctx)
		return nil
	}
	parserDiagnostics := s.localizeDiagnostics(core.Diagnostics(parsed))
	written, err := s.writeDiagnosticNotificationForVersion(uri, version, parserDiagnostics)
	if err != nil {
		return err
	}
	if !written {
		state = "stale"
		return nil
	}
	s.logDebugVerbose("[asp-lsp] diagnostics.fast.published: " + uri)
	s.updateProgressTask(taskID, "diagnostics", "diagnostics.syntax", progressDetailForURI(uri), 1, total, []string{progressDetailForURI(uri)}, "running")

	if !s.diagnosticsContextCurrent(ctx, uri, version) {
		state = diagnosticsContextState(ctx)
		return nil
	}
	rawIncludeDiagnostics, includeComplete := s.includeDiagnosticsContext(ctx, parsed)
	if !includeComplete || ctx.Err() != nil {
		state = diagnosticsContextState(ctx)
		return nil
	}
	includeDiagnostics := s.localizeDiagnostics(rawIncludeDiagnostics)
	if ctx.Err() != nil {
		state = diagnosticsContextState(ctx)
		return nil
	}
	if len(includeDiagnostics) > 0 {
		written, err = s.writeDiagnosticNotificationForVersion(uri, version, append(cloneDiagnostics(parserDiagnostics), includeDiagnostics...))
		if err != nil {
			return err
		}
		if !written {
			state = "stale"
			return nil
		}
		s.logDebugVerbose("[asp-lsp] diagnostics.include.published: " + uri)
	}
	s.updateProgressTask(taskID, "diagnostics", "diagnostics.include", progressDetailForURI(uri), 2, total, []string{progressDetailForURI(uri)}, "running")

	if !s.diagnosticsContextCurrent(ctx, uri, version) {
		state = diagnosticsContextState(ctx)
		return nil
	}
	s.htmlMu.Lock()
	syntaxDiagnostics := append([]lsp.Diagnostic{}, s.html.Diagnostics(parsed)...)
	s.htmlMu.Unlock()
	syntaxDiagnostics = append(syntaxDiagnostics, s.cssDiagnostics(parsed)...)
	if len(syntaxDiagnostics) > 0 {
		staged := append(cloneDiagnostics(parserDiagnostics), includeDiagnostics...)
		staged = append(staged, s.localizeDiagnostics(syntaxDiagnostics)...)
		written, err = s.writeDiagnosticNotificationForVersion(uri, version, staged)
		if err != nil {
			return err
		}
		if !written {
			state = "stale"
			return nil
		}
		s.logDebugVerbose("[asp-lsp] diagnostics.syntax.published: " + uri)
	}
	s.updateProgressTask(taskID, "diagnostics", "diagnostics.syntax", progressDetailForURI(uri), 3, total, []string{progressDetailForURI(uri)}, "running")

	if !s.diagnosticsContextCurrent(ctx, uri, version) {
		state = diagnosticsContextState(ctx)
		return nil
	}
	includedDocuments, includesComplete := s.includedDocumentsForDiagnosticsContext(ctx, parsed)
	if !includesComplete || ctx.Err() != nil {
		state = diagnosticsContextState(ctx)
		return nil
	}
	externalGlobals := map[string]struct{}{}
	for _, includedDocument := range includedDocuments {
		if ctx.Err() != nil {
			state = diagnosticsContextState(ctx)
			return nil
		}
		for _, declaration := range serverObjectDeclarations(includedDocument) {
			externalGlobals[strings.ToLower(declaration.Name)] = struct{}{}
		}
	}
	for name := range includedVBVariableInlayGlobalNames(includedDocuments) {
		externalGlobals[name] = struct{}{}
	}
	for name := range s.legacyUndefinedGlobalNames(ctx) {
		externalGlobals[name] = struct{}{}
	}
	s.mu.Lock()
	locale := s.settings.Locale
	implicitGlobalDiagnostics := s.settings.VBScriptImplicitGlobalDiagnostics
	s.mu.Unlock()
	projectDiagnostics := vbscriptUsageDiagnosticsWithGlobals(parsed, locale, externalGlobals)
	if implicitGlobalDiagnostics && !hasVBOptionExplicit(parsed) {
		projectDiagnostics = append(projectDiagnostics, implicitGlobalVBScriptDiagnostics(parsed, locale, externalGlobals)...)
	}
	projectDiagnostics = s.localizeDiagnostics(projectDiagnostics)
	if len(projectDiagnostics) > 0 {
		written, err = s.writeDiagnosticNotificationForVersion(uri, version, projectDiagnostics)
		if err != nil {
			return err
		}
		if !written {
			state = "stale"
			return nil
		}
		s.logDebugVerbose("[asp-lsp] diagnostics.projectFast.published: " + uri)
		written, err = s.writeDiagnosticNotificationForVersion(uri, version, append(cloneDiagnostics(projectDiagnostics), includeDiagnostics...))
		if err != nil {
			return err
		}
		if !written {
			state = "stale"
			return nil
		}
		s.logDebugVerbose("[asp-lsp] diagnostics.project.published: " + uri)
	}
	s.updateProgressTask(taskID, "diagnostics", "diagnostics.projectFast", progressDetailForURI(uri), total, total, nil, "completed")
	state = "completed"
	return s.publishFinalDiagnosticsContext(ctx, uri, true, true, version)
}

func cloneDiagnostics(diagnostics []lsp.Diagnostic) []lsp.Diagnostic {
	if len(diagnostics) == 0 {
		return nil
	}
	cloned := make([]lsp.Diagnostic, len(diagnostics))
	copy(cloned, diagnostics)
	return cloned
}

func (s *Server) publishFinalDiagnostics(uri string, logAnalysisStart bool, _ bool) error {
	return s.publishFinalDiagnosticsContext(context.Background(), uri, logAnalysisStart, true, -1)
}

func (s *Server) publishFinalDiagnosticsContext(ctx context.Context, uri string, logAnalysisStart bool, _ bool, expectedVersion int) error {
	const total = 15
	taskID, _ := s.beginProgressTask("diagnostics", "analyzing", "diagnostics", "diagnostics", progressDetailForURI(uri), total, false)
	state := "failed"
	defer func() { s.finishProgressTask(taskID, "diagnostics", state) }()
	started := time.Now()
	if logAnalysisStart {
		s.logDebugSummary("[asp-lsp] LSP analysis started: " + uri)
	}
	s.logDebugTrace("diagnostics.start", "[asp-lsp] diagnostics.start: "+uri)
	checkStarted := time.Now()
	ctx, checkSpan := newRuntimeLogSpan(ctx)
	checkFields := map[string]any{"uri": uri}
	checkSpan.addFields(checkFields)
	s.logDebugSummaryEvent("check.total.started", "[asp-lsp] LSP check started: "+uri+formatLogFields(checkFields), checkFields)
	defer func() {
		if state != "completed" {
			fields := cloneLogMetadata(checkFields)
			fields["durationMs"] = float64(time.Since(checkStarted).Microseconds()) / 1000
			fields["state"] = state
			s.logDebugSummaryEvent("check.total."+state, "[asp-lsp] check.total."+state+formatLogFields(fields), fields)
		}
	}()
	snapshot := s.diagnosticsSnapshotWithProgress(ctx, uri, func(label string, current, _ int) {
		s.updateProgressTask(taskID, "diagnostics", label, progressDetailForURI(uri), current, total, []string{progressDetailForURI(uri)}, "running")
	})
	if !snapshot.ok {
		if ctx != nil && ctx.Err() != nil {
			state = "cancelled"
		}
		return nil
	}
	if expectedVersion >= 0 && snapshot.version != expectedVersion {
		s.logDebugSummary("[asp-lsp] diagnostics.final.stale: " + uri)
		state = "stale"
		return nil
	}
	if !s.diagnosticsContextCurrent(ctx, uri, snapshot.version) {
		s.logDebugSummary("[asp-lsp] diagnostics.final.stale: " + uri)
		state = diagnosticsContextState(ctx)
		return nil
	}
	diagnostics := snapshot.diagnostics
	written, err := s.publishDiagnosticSnapshotWithRevisionsContext(ctx, uri, snapshot.version, diagnostics, snapshot.targetRevisions)
	if err != nil {
		return err
	}
	if !written {
		state = "stale"
		return nil
	}
	if doc := s.documentByURI(uri); doc != nil && doc.Version == snapshot.version {
		s.rememberValidatedDocumentVersion(uri, doc.Version)
	}
	s.logDebugVerbose("[asp-lsp] diagnostics.final.published: " + uri)
	s.updateProgressTask(taskID, "diagnostics", "document.analysis.ready", progressDetailForURI(uri), total, total, nil, "completed")
	state = "completed"
	s.logDebugSummary("[asp-lsp] LSP analysis completed: " + uri + " " + formatElapsedSince(started))
	checkFields["durationMs"] = float64(time.Since(checkStarted).Microseconds()) / 1000
	checkFields["count"] = len(diagnostics)
	s.logDebugSummaryEvent("check.total.completed", "[asp-lsp] LSP check completed: "+uri+" "+formatElapsedSince(checkStarted)+", diagnostics="+strconv.Itoa(len(diagnostics))+formatLogFields(checkFields), checkFields)
	return nil
}

func (s *Server) writeDiagnosticNotificationForVersion(uri string, version int, diagnostics []lsp.Diagnostic) (bool, error) {
	return s.writeDiagnosticNotification(uri, &version, diagnostics)
}

func (s *Server) writeDiagnosticNotification(uri string, expectedVersion *int, diagnostics []lsp.Diagnostic) (bool, error) {
	s.mu.Lock()
	doc := s.openDocumentByURILocked(uri)
	if expectedVersion != nil && (doc == nil || doc.Version != *expectedVersion) {
		s.mu.Unlock()
		return false, nil
	}
	version := -1
	if doc != nil {
		version = doc.Version
	}
	message := diagnosticPublishDiagnosticsMessage(uri, version, diagnostics)
	s.mu.Unlock()
	return true, s.writeRPCMessage(message)
}

func diagnosticPublishDiagnosticsMessage(uri string, version int, diagnostics []lsp.Diagnostic) rpcMessage {
	params := map[string]any{
		"uri":         uri,
		"diagnostics": nonNilDiagnostics(dedupeDiagnostics(diagnostics)),
	}
	if version >= 0 {
		params["version"] = version
	}
	return rpcMessage{
		Method: "textDocument/publishDiagnostics",
		Params: mustRaw(params),
	}
}

// publishDiagnosticSnapshotContext publishes each diagnostic under the URI of
// the source document that owns its range. Included-file diagnostics carry
// that URI in Diagnostic.Data. The owner remains the publication gate: an
// owner revision must still be current before any child notification is sent.
func (s *Server) publishDiagnosticSnapshotContext(ctx context.Context, ownerURI string, ownerVersion int, diagnostics []lsp.Diagnostic) (bool, error) {
	return s.publishDiagnosticSnapshotWithRevisionsContext(ctx, ownerURI, ownerVersion, diagnostics, nil)
}

func (s *Server) publishDiagnosticSnapshotWithRevisionsContext(ctx context.Context, ownerURI string, ownerVersion int, diagnostics []lsp.Diagnostic, sourceRevisions map[string]diagnosticTargetRevision) (bool, error) {
	if ctx == nil {
		ctx = context.Background()
	}
	if !s.acquireDiagnosticPublication(ctx) {
		return false, nil
	}
	defer s.releaseDiagnosticPublication()
	if ctx.Err() != nil || !s.documentVersionMatches(ownerURI, ownerVersion) {
		return false, nil
	}
	groups, targetRevisions, complete := s.diagnosticPublicationGroupsContext(ctx, ownerURI, diagnostics, sourceRevisions)
	if !complete || ctx.Err() != nil || !s.documentVersionMatches(ownerURI, ownerVersion) {
		return false, nil
	}
	_, previousTargets, _ := s.publishedDiagnosticContributionSnapshot(ownerURI)
	for _, target := range previousTargets {
		key := workspacepkg.FileIdentityKeyFromURI(target)
		if _, captured := targetRevisions[key]; captured {
			continue
		}
		revision, ok := s.diagnosticTargetRevisionContext(ctx, target)
		if !ok || ctx.Err() != nil {
			return false, nil
		}
		targetRevisions[key] = revision
	}
	batch, ok := s.buildDiagnosticPublicationBatchContext(ctx, ownerURI, ownerVersion, groups, targetRevisions)
	if !ok {
		return false, nil
	}
	if hook := s.diagnosticPublicationBatchTestHook; hook != nil {
		hook()
	}
	return s.writeDiagnosticPublicationBatch(batch)
}

func (s *Server) acquireDiagnosticPublication(ctx context.Context) bool {
	if ctx == nil {
		ctx = context.Background()
	}
	s.diagnosticPublicationGateOnce.Do(func() {
		s.diagnosticPublicationGate = make(chan struct{}, 1)
		s.diagnosticPublicationGate <- struct{}{}
	})
	select {
	case <-ctx.Done():
		return false
	case <-s.diagnosticPublicationGate:
		return true
	}
}

func (s *Server) releaseDiagnosticPublication() {
	s.diagnosticPublicationGate <- struct{}{}
}

func (s *Server) writeDiagnosticPublicationBatch(batch diagnosticPublicationBatch) (bool, error) {
	for index, message := range batch.messages {
		result := s.writeRPCMessageResult(message)
		if result.err != nil {
			if result.outcome == rpcWriteFatal {
				return false, result.err
			}
			escaped := index
			var compensationErr error
			for _, compensation := range batch.compensationMessages[:escaped] {
				compensationResult := s.writeRPCMessageResult(compensation)
				if compensationResult.err != nil {
					compensationErr = errors.Join(compensationErr, compensationResult.err)
					if compensationResult.outcome == rpcWriteFatal {
						break
					}
				}
			}
			if compensationErr != nil {
				return false, errors.Join(result.err, compensationErr)
			}
			return false, result.err
		}
	}
	s.mu.Lock()
	s.applyPublishedDiagnosticContributionStateLocked(batch.contributionState)
	s.mu.Unlock()
	return true, nil
}

type diagnosticTargetRevision struct {
	contentHash string
	version     int
	hasVersion  bool
	// document identifies the immutable in-memory revision that supplied the
	// hash. Publication can compare this pointer under s.mu and avoid hashing
	// the same source again while retaining the version/content fence when a
	// revision has been replaced.
	document     *core.TextDocument
	documentHash string
}

func (s *Server) diagnosticPublicationGroupsContext(ctx context.Context, ownerURI string, diagnostics []lsp.Diagnostic, sourceRevisions map[string]diagnosticTargetRevision) (map[string][]lsp.Diagnostic, map[string]diagnosticTargetRevision, bool) {
	if ctx == nil {
		ctx = context.Background()
	}
	if ctx.Err() != nil {
		return nil, nil, false
	}
	groups := map[string][]lsp.Diagnostic{ownerURI: {}}
	allowed := map[string]string{workspacepkg.FileIdentityKeyFromURI(ownerURI): ownerURI}
	sourceDocuments := map[string]*core.ParsedDocument{}
	if _, parsed := s.parsed(ownerURI); parsed != nil {
		sourceDocuments[workspacepkg.FileIdentityKeyFromURI(parsed.URI)] = parsed
		units, complete := s.vbscriptIncludeExecutionUnitsContext(ctx, parsed)
		if !complete {
			return nil, nil, false
		}
		for _, unit := range units {
			if ctx.Err() != nil {
				return nil, nil, false
			}
			if unit.document == nil {
				continue
			}
			key := workspacepkg.FileIdentityKeyFromURI(unit.document.URI)
			if _, exists := allowed[key]; !exists {
				allowed[key] = unit.document.URI
			}
			if _, exists := sourceDocuments[key]; !exists {
				sourceDocuments[key] = unit.document
			}
		}
	}
	for _, diagnostic := range diagnostics {
		if ctx.Err() != nil {
			return nil, nil, false
		}
		target := ownerURI
		if dataURI := diagnosticDataString(diagnostic, "uri"); dataURI != "" {
			canonical, ok := allowed[workspacepkg.FileIdentityKeyFromURI(dataURI)]
			if !ok {
				// Never move a diagnostic with an untrusted/obsolete target URI
				// back onto the owner, where its range could underline unrelated
				// source text. The complete publication is rejected so an
				// incomplete include expansion cannot publish an owner subset.
				return nil, nil, false
			}
			target = canonical
		}
		groups[target] = append(groups[target], diagnostic)
	}
	for target, items := range groups {
		groups[target] = dedupeDiagnostics(items)
	}
	targetRevisions := make(map[string]diagnosticTargetRevision)
	for target, items := range groups {
		if ctx.Err() != nil {
			return nil, nil, false
		}
		if len(items) == 0 {
			continue
		}
		key := workspacepkg.FileIdentityKeyFromURI(target)
		var revision diagnosticTargetRevision
		var ok bool
		if expected, exists := sourceRevisions[key]; exists {
			if !s.diagnosticTargetRevisionMatchesContext(ctx, target, expected) {
				return nil, nil, false
			}
			revision, ok = expected, true
		} else if parsed := sourceDocuments[key]; parsed != nil {
			revision, ok = s.diagnosticTargetRevisionForParsedContext(ctx, parsed)
		} else {
			revision, ok = s.diagnosticTargetRevisionContext(ctx, target)
		}
		if !ok {
			return nil, nil, false
		}
		targetRevisions[key] = revision
	}
	return groups, targetRevisions, true
}

func (s *Server) diagnosticTargetRevisionForParsedContext(ctx context.Context, parsed *core.ParsedDocument) (diagnosticTargetRevision, bool) {
	if parsed == nil {
		return diagnosticTargetRevision{}, false
	}
	if ctx == nil {
		ctx = context.Background()
	}
	if ctx.Err() != nil {
		return diagnosticTargetRevision{}, false
	}
	contentHash := textContentHash(parsed.Text)
	s.mu.Lock()
	if doc := s.openDocumentByURILocked(parsed.URI); doc != nil {
		text, version := doc.Text, doc.Version
		s.mu.Unlock()
		if text != parsed.Text {
			return diagnosticTargetRevision{}, false
		}
		return diagnosticTargetRevision{contentHash: contentHash, version: version, hasVersion: true, document: doc, documentHash: contentHash}, true
	}
	if doc := s.workspaceDocumentByURILocked(parsed.URI); doc != nil {
		text := doc.Text
		s.mu.Unlock()
		if text != parsed.Text {
			return diagnosticTargetRevision{}, false
		}
		return diagnosticTargetRevision{contentHash: contentHash, document: doc, documentHash: contentHash}, true
	}
	s.mu.Unlock()
	return s.diagnosticTargetRevisionFromDiskContext(ctx, parsed.URI, parsed.Text, contentHash)
}

func (s *Server) diagnosticTargetRevisionContext(ctx context.Context, uri string) (diagnosticTargetRevision, bool) {
	if ctx == nil {
		ctx = context.Background()
	}
	if ctx.Err() != nil {
		return diagnosticTargetRevision{}, false
	}
	s.mu.Lock()
	if doc := s.openDocumentByURILocked(uri); doc != nil {
		text, version := doc.Text, doc.Version
		s.mu.Unlock()
		hash := textContentHash(text)
		return diagnosticTargetRevision{
			contentHash:  hash,
			version:      version,
			hasVersion:   true,
			document:     doc,
			documentHash: hash,
		}, true
	}
	if doc := s.workspaceDocumentByURILocked(uri); doc != nil {
		text := doc.Text
		s.mu.Unlock()
		hash := textContentHash(text)
		return diagnosticTargetRevision{contentHash: hash, document: doc, documentHash: hash}, true
	}
	s.mu.Unlock()
	path := fileURIPath(uri)
	if path == "" {
		return diagnosticTargetRevision{}, false
	}
	text, err := s.readWorkspaceTextFileWithinBoundaries(ctx, path, filepath.Dir(filepath.Clean(path)))
	if err != nil || ctx.Err() != nil {
		return diagnosticTargetRevision{}, false
	}
	return diagnosticTargetRevision{contentHash: workspacepkg.DiskContentHash(text)}, true
}

func (s *Server) diagnosticTargetRevisionFromDiskContext(ctx context.Context, uri, expectedText, expectedHash string) (diagnosticTargetRevision, bool) {
	path := fileURIPath(uri)
	if path == "" {
		return diagnosticTargetRevision{}, false
	}
	text, err := s.readWorkspaceTextFileWithinBoundaries(ctx, path, filepath.Dir(filepath.Clean(path)))
	if err != nil || ctx.Err() != nil || text != expectedText {
		return diagnosticTargetRevision{}, false
	}
	return diagnosticTargetRevision{contentHash: expectedHash}, true
}

func (s *Server) diagnosticTargetRevisionMatchesContext(ctx context.Context, uri string, expected diagnosticTargetRevision) bool {
	if ctx != nil && ctx.Err() != nil {
		return false
	}
	if expected.document != nil {
		s.mu.Lock()
		current := s.openDocumentByURILocked(uri)
		if current == nil {
			current = s.workspaceDocumentByURILocked(uri)
		}
		if current == expected.document && expected.documentHash == expected.contentHash && (!expected.hasVersion || current.Version == expected.version) {
			s.mu.Unlock()
			return true
		}
		s.mu.Unlock()
	}
	current, ok := s.diagnosticTargetRevisionContext(ctx, uri)
	return ok && diagnosticTargetRevisionEqual(current, expected)
}

func diagnosticTargetRevisionEqual(left, right diagnosticTargetRevision) bool {
	if left.contentHash != right.contentHash {
		return false
	}
	return !right.hasVersion || left.hasVersion && left.version == right.version
}

func diagnosticsForDocumentURI(uri string, diagnostics []lsp.Diagnostic) []lsp.Diagnostic {
	filtered := make([]lsp.Diagnostic, 0, len(diagnostics))
	for _, diagnostic := range diagnostics {
		target := diagnosticDataString(diagnostic, "uri")
		if target != "" && !workspacepkg.SameFileIdentityURI(uri, target) {
			continue
		}
		filtered = append(filtered, diagnostic)
	}
	return dedupeDiagnostics(filtered)
}

func dedupePublishedDiagnostics(diagnostics []lsp.Diagnostic) []lsp.Diagnostic {
	seen := map[string]int{}
	deduped := make([]lsp.Diagnostic, 0, len(diagnostics))
	for _, diagnostic := range diagnostics {
		key := diagnostic.Source + "|" + diagnosticRangeKey(diagnostic.Range) + "|" + diagnostic.Message
		if index, exists := seen[key]; exists {
			if diagnosticDataString(deduped[index], "uri") == "" && diagnosticDataString(diagnostic, "uri") != "" {
				deduped[index] = diagnostic
			}
			continue
		}
		seen[key] = len(deduped)
		deduped = append(deduped, diagnostic)
	}
	return deduped
}

type diagnosticPublicationPlan struct {
	uri         string
	diagnostics []lsp.Diagnostic
	revision    diagnosticTargetRevision
	hasRevision bool
	version     int
	hasVersion  bool
}

type diagnosticPublicationBatch struct {
	messages             []rpcMessage
	compensationMessages []rpcMessage
	contributionState    publishedDiagnosticContributionState
}

type publishedDiagnosticContributionState struct {
	targets   map[string]map[string]string
	items     map[string]map[string][]lsp.Diagnostic
	revisions map[string]map[string]diagnosticTargetRevision
}

func (s *Server) updatePublishedDiagnosticContributions(ownerURI string, groups map[string][]lsp.Diagnostic) []diagnosticPublicationPlan {
	s.acquireDiagnosticPublication(context.Background())
	defer s.releaseDiagnosticPublication()
	return s.updatePublishedDiagnosticContributionsLocked(ownerURI, groups)
}

func (s *Server) publishedDiagnosticContributionSnapshot(ownerURI string) (map[string][]lsp.Diagnostic, map[string]string, map[string]diagnosticTargetRevision) {
	ownerKey := diagnosticTimerKey(ownerURI)
	s.mu.Lock()
	defer s.mu.Unlock()
	items := clonePublishedDiagnosticItems(s.publishedDiagnosticItems[ownerKey])
	targets := clonePublishedDiagnosticTargets(s.publishedDiagnosticTargets[ownerKey])
	revisions := clonePublishedDiagnosticRevisions(s.publishedDiagnosticRevisions[ownerKey])
	return items, targets, revisions
}

func clonePublishedDiagnosticItems(items map[string][]lsp.Diagnostic) map[string][]lsp.Diagnostic {
	if len(items) == 0 {
		return nil
	}
	cloned := make(map[string][]lsp.Diagnostic, len(items))
	for key, diagnostics := range items {
		cloned[key] = cloneDiagnostics(diagnostics)
	}
	return cloned
}

func clonePublishedDiagnosticTargets(targets map[string]string) map[string]string {
	if len(targets) == 0 {
		return nil
	}
	cloned := make(map[string]string, len(targets))
	for key, target := range targets {
		cloned[key] = target
	}
	return cloned
}

func clonePublishedDiagnosticRevisions(revisions map[string]diagnosticTargetRevision) map[string]diagnosticTargetRevision {
	if len(revisions) == 0 {
		return nil
	}
	cloned := make(map[string]diagnosticTargetRevision, len(revisions))
	for key, revision := range revisions {
		cloned[key] = revision
	}
	return cloned
}

func (s *Server) updatePublishedDiagnosticContributionsLocked(ownerURI string, groups map[string][]lsp.Diagnostic) []diagnosticPublicationPlan {
	return s.updatePublishedDiagnosticContributionsLockedWithRevisions(ownerURI, groups, nil)
}

func (s *Server) updatePublishedDiagnosticContributionsLockedWithRevisions(ownerURI string, groups map[string][]lsp.Diagnostic, targetRevisions map[string]diagnosticTargetRevision) []diagnosticPublicationPlan {
	s.mu.Lock()
	defer s.mu.Unlock()
	state := s.clonePublishedDiagnosticContributionStateLocked()
	plans := buildPublishedDiagnosticContributionsForState(ownerURI, groups, targetRevisions, state)
	s.applyPublishedDiagnosticContributionStateLocked(state)
	return plans
}

func (s *Server) clonePublishedDiagnosticContributionStateLocked() publishedDiagnosticContributionState {
	return publishedDiagnosticContributionState{
		targets:   clonePublishedDiagnosticTargetsByOwner(s.publishedDiagnosticTargets),
		items:     clonePublishedDiagnosticItemsByOwner(s.publishedDiagnosticItems),
		revisions: clonePublishedDiagnosticRevisionsByOwner(s.publishedDiagnosticRevisions),
	}
}

func (s *Server) applyPublishedDiagnosticContributionStateLocked(state publishedDiagnosticContributionState) {
	s.publishedDiagnosticTargets = state.targets
	s.publishedDiagnosticItems = state.items
	s.publishedDiagnosticRevisions = state.revisions
}

func clonePublishedDiagnosticTargetsByOwner(targets map[string]map[string]string) map[string]map[string]string {
	if len(targets) == 0 {
		return map[string]map[string]string{}
	}
	cloned := make(map[string]map[string]string, len(targets))
	for owner, values := range targets {
		cloned[owner] = clonePublishedDiagnosticTargets(values)
	}
	return cloned
}

func clonePublishedDiagnosticItemsByOwner(items map[string]map[string][]lsp.Diagnostic) map[string]map[string][]lsp.Diagnostic {
	if len(items) == 0 {
		return map[string]map[string][]lsp.Diagnostic{}
	}
	cloned := make(map[string]map[string][]lsp.Diagnostic, len(items))
	for owner, values := range items {
		cloned[owner] = clonePublishedDiagnosticItems(values)
	}
	return cloned
}

func clonePublishedDiagnosticRevisionsByOwner(revisions map[string]map[string]diagnosticTargetRevision) map[string]map[string]diagnosticTargetRevision {
	if len(revisions) == 0 {
		return map[string]map[string]diagnosticTargetRevision{}
	}
	cloned := make(map[string]map[string]diagnosticTargetRevision, len(revisions))
	for owner, values := range revisions {
		cloned[owner] = clonePublishedDiagnosticRevisions(values)
	}
	return cloned
}

func buildPublishedDiagnosticContributionsForState(ownerURI string, groups map[string][]lsp.Diagnostic, targetRevisions map[string]diagnosticTargetRevision, state publishedDiagnosticContributionState) []diagnosticPublicationPlan {
	ownerKey := diagnosticTimerKey(ownerURI)
	currentItems := make(map[string][]lsp.Diagnostic)
	currentTargets := make(map[string]string)
	for target, diagnostics := range groups {
		if len(diagnostics) == 0 {
			continue
		}
		key := workspacepkg.FileIdentityKeyFromURI(target)
		currentItems[key] = cloneDiagnostics(dedupePublishedDiagnostics(diagnostics))
		currentTargets[key] = target
	}
	previousItems := state.items[ownerKey]
	previousTargets := state.targets[ownerKey]
	if previousItems == nil {
		previousItems = map[string][]lsp.Diagnostic{}
	}
	if previousTargets == nil {
		previousTargets = map[string]string{}
	}
	if state.targets == nil {
		state.targets = map[string]map[string]string{}
	}
	if state.items == nil {
		state.items = map[string]map[string][]lsp.Diagnostic{}
	}
	if state.revisions == nil {
		state.revisions = map[string]map[string]diagnosticTargetRevision{}
	}
	currentRevisions := make(map[string]diagnosticTargetRevision)
	for key, revision := range targetRevisions {
		if _, exists := currentTargets[key]; exists {
			currentRevisions[key] = revision
		}
	}
	if len(currentTargets) == 0 {
		delete(state.targets, ownerKey)
		delete(state.items, ownerKey)
		delete(state.revisions, ownerKey)
	} else {
		state.targets[ownerKey] = currentTargets
		state.items[ownerKey] = currentItems
		state.revisions[ownerKey] = currentRevisions
	}
	affected := map[string]struct{}{workspacepkg.FileIdentityKeyFromURI(ownerURI): {}}
	for key := range previousItems {
		affected[key] = struct{}{}
	}
	for key := range currentItems {
		affected[key] = struct{}{}
	}
	ownerIdentityKey := workspacepkg.FileIdentityKeyFromURI(ownerURI)
	ownerKeys := make([]string, 0, len(state.targets))
	for otherOwner := range state.targets {
		ownerKeys = append(ownerKeys, otherOwner)
	}
	sort.Strings(ownerKeys)
	plans := make([]diagnosticPublicationPlan, 0, len(affected))
	for key := range affected {
		target := currentTargets[key]
		if target == "" {
			target = previousTargets[key]
		}
		if target == "" && key == ownerIdentityKey {
			target = ownerURI
		}
		if target == "" {
			for _, otherOwner := range ownerKeys {
				if otherOwner == ownerKey {
					continue
				}
				targets := state.targets[otherOwner]
				if candidate := targets[key]; candidate != "" {
					target = candidate
					break
				}
			}
		}
		if target == "" {
			continue
		}
		var aggregate []lsp.Diagnostic
		targetRevision, hasTargetRevision := targetRevisions[key]
		itemOwners := make([]string, 0, len(state.items))
		for otherOwner := range state.items {
			itemOwners = append(itemOwners, otherOwner)
		}
		sort.Strings(itemOwners)
		for _, otherOwner := range itemOwners {
			if hasTargetRevision {
				ownerRevisions := state.revisions[otherOwner]
				contributionRevision, hasContributionRevision := ownerRevisions[key]
				if !hasContributionRevision || !diagnosticTargetRevisionEqual(contributionRevision, targetRevision) {
					continue
				}
			}
			items := state.items[otherOwner]
			aggregate = append(aggregate, items[key]...)
		}
		plans = append(plans, diagnosticPublicationPlan{uri: target, diagnostics: dedupePublishedDiagnostics(aggregate), revision: targetRevision, hasRevision: hasTargetRevision})
	}
	sort.SliceStable(plans, func(left, right int) bool {
		leftOwner := workspacepkg.FileIdentityKeyFromURI(plans[left].uri) == workspacepkg.FileIdentityKeyFromURI(ownerURI)
		rightOwner := workspacepkg.FileIdentityKeyFromURI(plans[right].uri) == workspacepkg.FileIdentityKeyFromURI(ownerURI)
		if leftOwner != rightOwner {
			return leftOwner
		}
		return plans[left].uri < plans[right].uri
	})
	return plans
}

func (s *Server) buildDiagnosticPublicationBatchContext(ctx context.Context, ownerURI string, ownerVersion int, groups map[string][]lsp.Diagnostic, targetRevisions map[string]diagnosticTargetRevision) (diagnosticPublicationBatch, bool) {
	if ctx == nil {
		ctx = context.Background()
	}
	if ctx.Err() != nil {
		return diagnosticPublicationBatch{}, false
	}
	s.mu.Lock()
	defer s.mu.Unlock()
	if ctx.Err() != nil {
		return diagnosticPublicationBatch{}, false
	}
	if ownerVersion >= 0 {
		owner := s.openDocumentByURILocked(ownerURI)
		if owner == nil || owner.Version != ownerVersion {
			return diagnosticPublicationBatch{}, false
		}
	}
	previousState := s.clonePublishedDiagnosticContributionStateLocked()
	state := s.clonePublishedDiagnosticContributionStateLocked()
	plans := buildPublishedDiagnosticContributionsForState(ownerURI, groups, targetRevisions, state)
	messages := make([]rpcMessage, 0, len(plans))
	compensationMessages := make([]rpcMessage, 0, len(plans))
	for index := range plans {
		if ctx.Err() != nil {
			return diagnosticPublicationBatch{}, false
		}
		plan := &plans[index]
		key := workspacepkg.FileIdentityKeyFromURI(plan.uri)
		if expected, exists := targetRevisions[key]; exists {
			if !s.diagnosticTargetRevisionMatchesStateLocked(plan.uri, expected) {
				return diagnosticPublicationBatch{}, false
			}
		}
		version := -1
		doc := s.openDocumentByURILocked(plan.uri)
		if key == workspacepkg.FileIdentityKeyFromURI(ownerURI) && ownerVersion >= 0 {
			version = ownerVersion
			plan.version = ownerVersion
			plan.hasVersion = true
		} else if doc != nil {
			version = doc.Version
			plan.version = version
			plan.hasVersion = true
		}
		if plan.hasRevision && plan.revision.hasVersion {
			if !plan.hasVersion || plan.version != plan.revision.version {
				return diagnosticPublicationBatch{}, false
			}
		}
		messages = append(messages, diagnosticPublishDiagnosticsMessage(plan.uri, version, plan.diagnostics))
		compensationMessages = append(compensationMessages, s.previousDiagnosticPublicationMessageLocked(*plan, previousState))
	}
	if ctx.Err() != nil {
		return diagnosticPublicationBatch{}, false
	}
	return diagnosticPublicationBatch{
		messages:             messages,
		compensationMessages: compensationMessages,
		contributionState:    state,
	}, true
}

func (s *Server) previousDiagnosticPublicationMessageLocked(candidate diagnosticPublicationPlan, state publishedDiagnosticContributionState) rpcMessage {
	key := workspacepkg.FileIdentityKeyFromURI(candidate.uri)
	itemOwners := make([]string, 0, len(state.items))
	for owner := range state.items {
		itemOwners = append(itemOwners, owner)
	}
	sort.Strings(itemOwners)
	var aggregate []lsp.Diagnostic
	version := -1
	for _, owner := range itemOwners {
		aggregate = append(aggregate, state.items[owner][key]...)
		if version < 0 {
			if revision, ok := state.revisions[owner][key]; ok && revision.hasVersion {
				version = revision.version
			}
		}
	}
	if version < 0 {
		version = candidate.version
	}
	if version < 0 {
		if doc := s.openDocumentByURILocked(candidate.uri); doc != nil {
			version = doc.Version
		}
	}
	return diagnosticPublishDiagnosticsMessage(candidate.uri, version, aggregate)
}

func (s *Server) diagnosticTargetRevisionMatchesStateLocked(uri string, expected diagnosticTargetRevision) bool {
	if expected.document != nil {
		current := s.openDocumentByURILocked(uri)
		if current == nil {
			current = s.workspaceDocumentByURILocked(uri)
		}
		if current == expected.document && expected.documentHash == expected.contentHash && (!expected.hasVersion || current.Version == expected.version) {
			return true
		}
	}
	if doc := s.openDocumentByURILocked(uri); doc != nil {
		current := diagnosticTargetRevision{
			contentHash: workspacepkg.DiskContentHash(doc.Text),
			version:     doc.Version,
			hasVersion:  true,
		}
		return diagnosticTargetRevisionEqual(current, expected)
	}
	if doc := s.workspaceDocumentByURILocked(uri); doc != nil {
		current := diagnosticTargetRevision{contentHash: workspacepkg.DiskContentHash(doc.Text)}
		return diagnosticTargetRevisionEqual(current, expected)
	}
	// Closed-file revisions are read and validated before entering the state
	// lock. There is no mutable in-memory version to recheck here.
	return !expected.hasVersion
}

func (s *Server) clearPublishedDiagnosticTargetsForOwner(ownerURI string) error {
	s.acquireDiagnosticPublication(context.Background())
	defer s.releaseDiagnosticPublication()
	_, previousTargets, _ := s.publishedDiagnosticContributionSnapshot(ownerURI)
	targetRevisions := make(map[string]diagnosticTargetRevision)
	for _, target := range previousTargets {
		key := workspacepkg.FileIdentityKeyFromURI(target)
		if revision, ok := s.diagnosticTargetRevisionContext(context.Background(), target); ok {
			targetRevisions[key] = revision
		}
	}
	batch, ok := s.buildDiagnosticPublicationBatchContext(context.Background(), ownerURI, -1, nil, targetRevisions)
	if !ok {
		return nil
	}
	_, err := s.writeDiagnosticPublicationBatch(batch)
	return err
}

func (s *Server) diagnosticsContextCurrent(ctx context.Context, uri string, version int) bool {
	return (ctx == nil || ctx.Err() == nil) && s.documentVersionMatches(uri, version)
}

func diagnosticsContextState(ctx context.Context) string {
	if ctx != nil && ctx.Err() != nil {
		return "cancelled"
	}
	return "stale"
}

func nonNilDiagnostics(diagnostics []lsp.Diagnostic) []lsp.Diagnostic {
	if diagnostics == nil {
		return []lsp.Diagnostic{}
	}
	return diagnostics
}

func (s *Server) scheduleDiagnostics(uri string) error {
	cancelPrevious := func(job *diagnosticRevisionJob) {
		if job != nil && job.supersede != nil {
			// Publication admission is context-aware, so callbacks waiting behind a
			// slow writer leave promptly. A callback already inside the transport
			// remains owned as a retired job until it completes.
			job.supersede()
		}
	}
	s.mu.Lock()
	if s.shutdown {
		s.mu.Unlock()
		return nil
	}
	debounceMS := s.settings.DiagnosticsDebounceMS
	key := diagnosticTimerKey(uri)
	previous := s.diagnosticJobs[key]
	previousTimer := s.diagnosticTimers[key]
	if previous != nil {
		delete(s.diagnosticJobs, key)
		s.diagnosticRetiredJobs[previous] = struct{}{}
	}
	delete(s.diagnosticTimers, key)
	doc := s.openDocumentByURILocked(uri)
	if doc == nil {
		s.mu.Unlock()
		if previous != nil {
			cancelPrevious(previous)
		} else if previousTimer != nil {
			previousTimer.Stop()
		}
		return nil
	}
	version := doc.Version
	ctx, cancelContext := context.WithCancel(context.Background())
	s.diagnosticJobSequence++
	job := &diagnosticRevisionJob{generation: s.diagnosticJobSequence, key: key, version: version, done: make(chan struct{})}
	var callbacks sync.WaitGroup
	var fastDone sync.Once
	var finalDone sync.Once
	callbacks.Add(1)
	if debounceMS > 0 {
		callbacks.Add(1)
	}
	markFastDone := func() { fastDone.Do(callbacks.Done) }
	markFinalDone := func() { finalDone.Do(callbacks.Done) }
	var cancelStartOnce sync.Once
	startCancel := func() {
		cancelStartOnce.Do(func() {
			cancelContext()
			if job.fastTimer != nil && job.fastTimer.Stop() {
				markFastDone()
			}
			if job.finalTimer != nil && job.finalTimer.Stop() {
				markFinalDone()
			}
		})
	}
	job.supersede = startCancel
	job.cancel = func() {
		startCancel()
		<-job.done
	}
	s.diagnosticJobs[key] = job
	go func() {
		callbacks.Wait()
		cancelContext()
		s.mu.Lock()
		if s.diagnosticJobs[key] == job {
			delete(s.diagnosticJobs, key)
			delete(s.diagnosticTimers, key)
		}
		delete(s.diagnosticRetiredJobs, job)
		s.mu.Unlock()
		close(job.done)
	}()
	if debounceMS <= 0 {
		s.mu.Unlock()
		if previous != nil {
			cancelPrevious(previous)
		} else if previousTimer != nil {
			previousTimer.Stop()
		}
		s.logDebugVerbose("[asp-lsp] documentChange.scheduleDiagnostics: " + uri)
		s.logDebugVerbose("[asp-lsp] documentChange.scheduleDiagnostics.postScheduleProjectUpdate: " + uri)
		defer markFinalDone()
		return s.publishFinalDiagnosticsContext(ctx, uri, true, false, version)
	}
	job.finalTimer = time.AfterFunc(time.Duration(debounceMS)*time.Millisecond, func() {
		defer markFinalDone()
		s.reportAsyncRPCWriteError(s.publishFinalDiagnosticsContext(ctx, uri, false, false, version))
	})
	fastDelay := time.Duration(debounceMS/2) * time.Millisecond
	if fastDelay > 40*time.Millisecond {
		fastDelay = 40 * time.Millisecond
	}
	if fastDelay <= 0 {
		fastDelay = time.Millisecond
	}
	job.fastTimer = time.AfterFunc(fastDelay, func() {
		defer markFastDone()
		s.reportAsyncRPCWriteError(s.publishFastDiagnosticsContext(ctx, uri, version))
	})
	// Keep the legacy timer registry populated for cache/reset code and tests.
	s.diagnosticTimers[key] = job.finalTimer
	s.mu.Unlock()
	if previous != nil {
		cancelPrevious(previous)
	} else if previousTimer != nil {
		previousTimer.Stop()
	}
	s.logDebugSummary("[asp-lsp] LSP analysis started: " + uri)
	s.logDebugVerbose("[asp-lsp] documentChange.scheduleDiagnostics: " + uri)
	s.logDebugVerbose("[asp-lsp] documentChange.scheduleDiagnostics.postScheduleProjectUpdate: " + uri)
	return nil
}

func (s *Server) cancelScheduledDiagnostics(uri string) {
	s.mu.Lock()
	key := diagnosticTimerKey(uri)
	job := s.diagnosticJobs[key]
	timer := s.diagnosticTimers[key]
	delete(s.diagnosticJobs, key)
	delete(s.diagnosticTimers, key)
	jobs := map[*diagnosticRevisionJob]struct{}{}
	if job != nil {
		jobs[job] = struct{}{}
		s.diagnosticRetiredJobs[job] = struct{}{}
	}
	for retired := range s.diagnosticRetiredJobs {
		if retired != nil && retired.key == key {
			jobs[retired] = struct{}{}
		}
	}
	s.mu.Unlock()
	for current := range jobs {
		if current.supersede != nil {
			current.supersede()
		}
	}
	if len(jobs) == 0 && timer != nil {
		timer.Stop()
	}
	for current := range jobs {
		if current.cancel != nil {
			current.cancel()
		}
	}
}

func (s *Server) stopScheduledDiagnostics() {
	s.mu.Lock()
	jobSet := make(map[*diagnosticRevisionJob]struct{}, len(s.diagnosticJobs)+len(s.diagnosticRetiredJobs))
	ownedTimers := make(map[*time.Timer]struct{}, len(s.diagnosticJobs)*2)
	for key, job := range s.diagnosticJobs {
		delete(s.diagnosticJobs, key)
		if job == nil {
			continue
		}
		jobSet[job] = struct{}{}
		if job.fastTimer != nil {
			ownedTimers[job.fastTimer] = struct{}{}
		}
		if job.finalTimer != nil {
			ownedTimers[job.finalTimer] = struct{}{}
		}
	}
	for job := range s.diagnosticRetiredJobs {
		jobSet[job] = struct{}{}
		delete(s.diagnosticRetiredJobs, job)
	}
	strayTimers := make([]*time.Timer, 0, len(s.diagnosticTimers))
	for key, timer := range s.diagnosticTimers {
		delete(s.diagnosticTimers, key)
		if timer != nil {
			if _, owned := ownedTimers[timer]; !owned {
				strayTimers = append(strayTimers, timer)
			}
		}
	}
	s.mu.Unlock()
	for _, timer := range strayTimers {
		timer.Stop()
	}
	for job := range jobSet {
		if job != nil && job.supersede != nil {
			job.supersede()
		}
	}
	for job := range jobSet {
		if job != nil && job.cancel != nil {
			job.cancel()
		}
	}
}

func (s *Server) clearValidatedDocumentVersion(uri string) {
	s.mu.Lock()
	delete(s.validatedDocumentVersions, diagnosticTimerKey(uri))
	s.mu.Unlock()
}

func (s *Server) documentValidationIsCurrent(uri string, version int) bool {
	s.mu.Lock()
	validated, ok := s.validatedDocumentVersions[diagnosticTimerKey(uri)]
	s.mu.Unlock()
	return ok && validated == version
}

func (s *Server) rememberValidatedDocumentVersion(uri string, version int) {
	s.mu.Lock()
	s.validatedDocumentVersions[diagnosticTimerKey(uri)] = version
	s.mu.Unlock()
}

func diagnosticTimerKey(uri string) string {
	if strings.HasPrefix(strings.ToLower(uri), "file:") {
		return workspacepkg.FileIdentityKeyFromURI(uri)
	}
	return uri
}

func changeTouchesASPBoundary(doc *core.TextDocument, change struct {
	Range *lsp.Range `json:"range,omitempty"`
	Text  string     `json:"text"`
}) bool {
	if strings.Contains(change.Text, "<%") || strings.Contains(change.Text, "%>") {
		return true
	}
	if change.Range == nil {
		return true
	}
	if doc == nil {
		return true
	}
	text := doc.Text
	start := doc.OffsetAt(change.Range.Start)
	end := doc.OffsetAt(change.Range.End)
	if start < 0 {
		start = 0
	}
	if end < start {
		start, end = end, start
	}
	if end > len(text) {
		end = len(text)
	}
	replaced := text[start:end]
	return strings.Contains(replaced, "<%") || strings.Contains(replaced, "%>")
}
