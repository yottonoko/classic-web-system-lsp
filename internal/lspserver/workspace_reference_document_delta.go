package lspserver

import (
	"strings"
	"time"

	"github.com/yottonoko/classic-web-system-lsp/internal/core"
	"github.com/yottonoko/classic-web-system-lsp/internal/vbscript"
	workspacepkg "github.com/yottonoko/classic-web-system-lsp/internal/workspace"
)

type workspaceReferenceDocumentCountReuse struct {
	current               *workspaceDocumentArtifactManifest
	generation            uint64
	expectedIndexRevision uint64
	currentDeclarations   []vbUsageDeclaration
	previousRequestKeys   []workspaceReferenceTargetKey
	counts                []int
}

type workspaceReferenceDocumentCountSnapshot struct {
	previousSourceFingerprint workspaceArtifactFingerprint
	currentSourceFingerprint  workspaceArtifactFingerprint
	generation                uint64
	previous                  *core.ParsedDocument
	previousDeclarations      []vbUsageDeclaration
	previousCounts            []int
}

func (s *Server) captureWorkspaceReferenceDocumentCountReuse(previous, current *core.ParsedDocument) {
	if previous == nil || current == nil || previous.URI == "" || current.URI == "" ||
		!workspacepkg.SameFileIdentityURI(previous.URI, current.URI) {
		return
	}
	documentID := workspaceDocumentIDFromURI(current.URI)
	currentFingerprint := workspaceFingerprint(current.Text)
	previousPlan := s.workspaceReferenceCodeLensPlan(previous)
	if len(previousPlan.declarations) == 0 {
		return
	}
	for _, declaration := range previousPlan.declarations {
		if declaration.Implicit {
			return
		}
	}

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
		manifest.SourceFingerprint != workspaceFingerprint(previous.Text) || !workspaceReferenceBatchCountsMatch(state, previousPlan.declarations) {
		delete(s.referencePendingDocumentCountReuse, documentID)
		s.mu.Unlock()
		return
	}
	s.referencePendingDocumentCountReuse[documentID] = &workspaceReferenceDocumentCountSnapshot{
		previousSourceFingerprint: manifest.SourceFingerprint,
		currentSourceFingerprint:  currentFingerprint,
		generation:                s.referenceGeneration,
		previous:                  previous,
		previousDeclarations:      append([]vbUsageDeclaration(nil), previousPlan.declarations...),
		previousCounts:            append([]int(nil), state.finalCounts...),
	}
	s.mu.Unlock()
}

func (s *Server) prepareWorkspaceReferenceDocumentCountReuse(previous, current *workspaceDocumentArtifactManifest, delta workspaceDocumentArtifactDelta) *workspaceReferenceDocumentCountReuse {
	if previous == nil || current == nil || previous.CST == nil || current.CST == nil || previous.DocumentID != current.DocumentID {
		return nil
	}
	s.mu.Lock()
	snapshot := s.referencePendingDocumentCountReuse[current.DocumentID]
	delete(s.referencePendingDocumentCountReuse, current.DocumentID)
	generation := s.referenceGeneration
	s.mu.Unlock()
	if delta.ParserSettingsChanged || delta.IncludeEdgesChanged || delta.ExecutionTapeChanged || len(delta.ChangedPublicNames) > 0 ||
		len(delta.ChangedImplicitNames) > 0 || len(delta.ChangedObjectTagNames) > 0 ||
		snapshot == nil || snapshot.previous == nil || snapshot.generation != generation ||
		snapshot.previousSourceFingerprint != previous.SourceFingerprint || snapshot.currentSourceFingerprint != current.SourceFingerprint {
		return nil
	}

	currentPlan := s.workspaceReferenceCodeLensPlan(current.CST)
	previousPlan := workspaceReferenceDeclarationPlan{declarations: snapshot.previousDeclarations}
	if len(previousPlan.declarations) == 0 || len(previousPlan.declarations) != len(currentPlan.declarations) ||
		len(snapshot.previousCounts) != len(previousPlan.declarations) {
		return nil
	}
	previousByIdentity := make(map[workspaceReferencePreviousKey]int, len(previousPlan.declarations))
	for index, declaration := range previousPlan.declarations {
		if declaration.Implicit {
			return nil
		}
		identity := workspaceReferencePreviousCountKey(snapshot.previous, declaration)
		if _, duplicate := previousByIdentity[identity]; duplicate {
			return nil
		}
		previousByIdentity[identity] = index
	}

	currentToPrevious := make([]int, len(currentPlan.declarations))
	for index, declaration := range currentPlan.declarations {
		if declaration.Implicit {
			return nil
		}
		previousIndex, ok := previousByIdentity[workspaceReferencePreviousCountKey(current.CST, declaration)]
		if !ok {
			return nil
		}
		currentToPrevious[index] = previousIndex
	}

	generation = snapshot.generation
	previousCounts := snapshot.previousCounts

	previousPrepared := prepareWorkspaceReferenceCountDocument(
		workspacepkg.FileIdentityKeyFromURI(previous.URI), snapshot.previous, string(previous.SourceFingerprint), vbscript.BuildReferenceShard(snapshot.previous), 0,
	)
	currentPrepared := prepareWorkspaceReferenceCountDocument(
		workspacepkg.FileIdentityKeyFromURI(current.URI), current.CST, string(current.SourceFingerprint), vbscript.BuildReferenceShard(current.CST), 0,
	)
	previousClassLines := vbClassLineSet(snapshot.previous)
	currentClassLines := vbClassLineSet(current.CST)
	counts := make([]int, len(currentPlan.declarations))
	previousRequestKeys := make([]workspaceReferenceTargetKey, len(currentPlan.declarations))
	for currentIndex, currentDeclaration := range currentPlan.declarations {
		previousIndex := currentToPrevious[currentIndex]
		previousDeclaration := previousPlan.declarations[previousIndex]
		previousRequestKeys[currentIndex] = workspaceReferenceRequestKey(previous.URI, previousDeclaration.Range.Start, false, previousDeclaration.Kind, generation, previousDeclaration.Name)
		count := previousCounts[previousIndex]
		count -= workspaceReferenceDocumentCodeLensContribution(previousPrepared.segments[strings.ToLower(previousDeclaration.Name)], previousClassLines, previousDeclaration)
		count += workspaceReferenceDocumentCodeLensContribution(currentPrepared.segments[strings.ToLower(currentDeclaration.Name)], currentClassLines, currentDeclaration)
		counts[currentIndex] = max(0, count)
	}
	return &workspaceReferenceDocumentCountReuse{
		current: current, generation: generation,
		currentDeclarations: append([]vbUsageDeclaration(nil), currentPlan.declarations...),
		previousRequestKeys: previousRequestKeys,
		counts:              counts,
	}
}

func workspaceReferenceDocumentCodeLensContribution(segment *workspaceReferenceDocumentSegment, classLines map[int]struct{}, declaration vbUsageDeclaration) int {
	if segment == nil {
		return 0
	}
	count := segment.counts.CodeLensReferences
	callOnly := vbReferenceUsesCallRanges(declaration.Kind)
	if callOnly {
		count = segment.counts.CodeLensCalls
	}
	_, inClass := classLines[declaration.Line]
	if declaration.MemberOf == "" && !inClass {
		count = segment.counts.UnqualifiedCodeLensReferences
		if callOnly {
			count = segment.counts.UnqualifiedCodeLensCalls
		}
	}
	return count
}

func (s *Server) publishWorkspaceReferenceDocumentCountReuse(reuse *workspaceReferenceDocumentCountReuse) bool {
	if reuse == nil || reuse.current == nil || len(reuse.currentDeclarations) != len(reuse.counts) {
		return false
	}
	s.mu.Lock()
	if s.referenceGeneration != reuse.generation || s.workspaceArtifacts[reuse.current.DocumentID] != reuse.current ||
		s.referenceWorkspaceIndex.revisionNumber() != reuse.expectedIndexRevision || !s.workspaceReferenceIndexReadyLocked() {
		s.mu.Unlock()
		return false
	}
	batchKey := referenceBatchCacheKey(reuse.current.URI, 0, reuse.generation)
	if previous := s.referenceBatch[batchKey]; previous != nil && previous.cancel != nil {
		previous.cancel()
	}
	nameRevisions := make(map[string]uint64, len(reuse.currentDeclarations))
	for index, declaration := range reuse.currentDeclarations {
		if index < len(reuse.previousRequestKeys) {
			s.deleteWorkspaceReferenceTargetKindLocked(reuse.previousRequestKeys[index], workspaceReferenceCountCache|workspaceReferencePartialCountCache)
		}
		name := strings.ToLower(declaration.Name)
		nameRevisions[name] = s.referenceNameRevisions[name]
		requestKey := workspaceReferenceRequestKey(reuse.current.URI, declaration.Range.Start, false, declaration.Kind, reuse.generation, declaration.Name)
		s.storeWorkspaceReferenceCountLocked(requestKey, reuse.counts[index])
		previousKey := workspaceReferencePreviousCountKey(reuse.current.CST, declaration)
		if previousKey.DocumentKey != "" {
			s.referencePreviousCounts[previousKey] = reuse.counts[index]
		}
	}
	done := make(chan struct{})
	close(done)
	state := &workspaceReferenceBatchState{
		generation: reuse.generation, done: done, nameRevisions: nameRevisions,
		total: len(reuse.currentDeclarations), warmed: len(reuse.currentDeclarations), started: time.Now(),
		declarations: append([]vbUsageDeclaration(nil), reuse.currentDeclarations...), finalCounts: append([]int(nil), reuse.counts...), complete: true,
	}
	s.storeWorkspaceReferenceBatchLocked(batchKey, state)
	s.mu.Unlock()
	s.logDebugSummaryEvent("referenceCache.documentDelta", "[asp-lsp] vb.references.batch.documentDelta.reuse: "+reuse.current.URI, map[string]any{
		"generation": reuse.generation, "symbolsTotal": len(reuse.currentDeclarations), "uri": reuse.current.URI,
	})
	s.requestCodeLensRefresh("references.documentDelta.reused")
	return true
}
