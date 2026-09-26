package lspserver

import (
	"context"
	"io"
	"path/filepath"
	"strconv"
	"sync/atomic"
	"testing"

	"github.com/yottonoko/classic-web-system-lsp/internal/core"
	"github.com/yottonoko/classic-web-system-lsp/internal/lsp"
	"github.com/yottonoko/classic-web-system-lsp/internal/vbscript"
	workspacepkg "github.com/yottonoko/classic-web-system-lsp/internal/workspace"
)

func TestWorkspaceReferenceSummaryKeepsGlobalCallsFromNonShadowingClasses(t *testing.T) {
	const source = `<%
Function Utility()
End Function
Class Container
  Public Sub Run()
    Utility()
  End Sub
End Class
%>`
	parsed := core.ParseDocument("file:///global-call-from-class.asp", source, core.Settings{DefaultLanguage: "VBScript"})
	postings := vbscript.BuildReferenceShard(parsed).PostingsFor("Utility")
	counts := summarizeWorkspaceReferencePostings(postings, vbReferenceDeclarationRanges(parsed, "Utility"))
	if counts.UnqualifiedCodeLensCalls != 1 {
		t.Fatalf("unqualified global call count = %d, want 1: %#v", counts.UnqualifiedCodeLensCalls, postings)
	}
}

func TestWorkspaceReferenceIndexUsesProvidedShardForStructuralClone(t *testing.T) {
	const source = "<% Dim shared : shared = 1 : Response.Write shared %>"
	original := core.ParseDocument("file:///provided-shard.asp", source, core.Settings{DefaultLanguage: "VBScript"})
	snapshot := buildWorkspaceArtifactSnapshotReducedContext(context.Background(), original)
	if snapshot == nil || snapshot.ReferenceShard == nil {
		t.Fatal("workspace snapshot did not build a reference shard")
	}
	clone := original.CloneStructural()
	if _, cached := vbscript.CachedReferenceShard(clone); cached {
		t.Fatal("structural clone unexpectedly inherited the reference shard cache")
	}

	index := newWorkspaceReferenceIndex()
	update, prepared := index.prepareContextModeWithShards(
		context.Background(),
		[]*core.ParsedDocument{clone},
		map[*core.ParsedDocument]*vbscript.ReferenceShard{clone: snapshot.ReferenceShard},
		true,
	)
	if prepared == nil || len(prepared) != 1 {
		t.Fatalf("prepared documents = %#v, update = %#v", prepared, update)
	}
	if _, cached := vbscript.CachedReferenceShard(clone); cached {
		t.Fatal("provided shard path rebuilt and cached another shard on the structural clone")
	}
	if applied := index.applyPrepared(prepared); applied.ChangedDocuments != 1 {
		t.Fatalf("applied update = %#v, want one changed document", applied)
	}
	matching := index.documentsForName("shared", []*core.ParsedDocument{clone})
	if len(matching) != 1 || matching[0] != clone {
		t.Fatalf("shared candidates = %#v, want the structural clone", matching)
	}
}

func TestImplicitGlobalReferencePlansParseOnlyRelatedIncludeTrees(t *testing.T) {
	server := New(nil, io.Discard, io.Discard)
	root := t.TempDir()
	server.rootPath = root
	server.rootURI = filePathURI(root)
	server.workspaceIncludeGraph.Reset("test")

	targetPath := filepath.Join(root, "target.asp")
	parentPath := filepath.Join(root, "parent.asp")
	siblingPath := filepath.Join(root, "sibling.inc")
	targetSource := "<% SharedValue = 1 %>"
	documents := map[string]string{
		targetPath:  targetSource,
		parentPath:  "<!-- #include file=\"target.asp\" --><!-- #include file=\"sibling.inc\" -->",
		siblingPath: "<% SharedValue = 2 %>",
	}
	for index := 0; index < 64; index++ {
		path := filepath.Join(root, "unrelated-"+strconv.Itoa(index)+".asp")
		documents[path] = "<% UnrelatedValue = 1 %>"
	}
	for path, source := range documents {
		uri := filePathURI(path)
		server.workspace[uri] = core.NewTextDocument(uri, "classic-asp", 0, source)
		server.workspaceIncludeGraph.Upsert(path, workspacepkg.SourceMetadata{FileName: path}, nil, "")
	}
	server.workspaceIncludeGraph.Upsert(parentPath, workspacepkg.SourceMetadata{FileName: parentPath}, []string{targetPath, siblingPath}, "parent")
	server.workspaceIncludeGraphComplete = true

	parsed := core.ParseDocument(filePathURI(targetPath), targetSource, core.Settings{DefaultLanguage: "VBScript"})
	var parses atomic.Int32
	server.documentParseTestHook = func(string) { parses.Add(1) }
	plans, complete := server.implicitGlobalReferencePlans(parsed)
	if !complete || plans == nil {
		t.Fatalf("implicit reference plans = %#v, complete=%t", plans, complete)
	}
	if got := parses.Load(); got > 2 {
		t.Fatalf("implicit reference planning parsed %d documents, want at most the related parent and sibling", got)
	}
}

func TestWorkspaceReferenceInvalidationPreservesCachesForUnrelatedSourceChange(t *testing.T) {
	server := New(nil, io.Discard, io.Discard)
	previous := core.ParseDocument("file:///first.asp", "<% Dim shared : shared = 1 %><p>first</p>", core.Settings{DefaultLanguage: "VBScript"})
	current := core.ParseDocument(previous.URI, "<% Dim shared : shared = 1 %><p>changed</p>", core.Settings{DefaultLanguage: "VBScript"})
	sentinel := workspaceReferenceTargetKey{Name: "sentinel"}
	server.referenceCounts[sentinel] = 12
	server.referenceDeclarationPlans[previous] = workspaceReferenceDeclarationPlan{declarations: []vbUsageDeclaration{{Name: "shared"}}}
	server.referenceCountSummariesRestored[previous] = struct{}{}
	generation := server.referenceGeneration

	server.invalidateWorkspaceReferencesForParsedChange(previous, current)

	if server.referenceGeneration != generation {
		t.Fatalf("generation = %d, want preserved %d", server.referenceGeneration, generation)
	}
	if got := server.referenceCounts[sentinel]; got != 12 {
		t.Fatalf("cached count = %d, want 12", got)
	}
	if _, ok := server.referenceDeclarationPlans[previous]; ok {
		t.Fatal("previous parsed revision survived semantic-unchanged invalidation")
	}
	if _, ok := server.referenceCountSummariesRestored[previous]; ok {
		t.Fatal("previous restored-summary marker survived semantic-unchanged invalidation")
	}
}

func TestWorkspaceReferenceFamilyInvalidationClearsDerivedPlanCaches(t *testing.T) {
	server := New(nil, io.Discard, io.Discard)
	affected := core.ParseDocument("file:///affected.asp", "<% Dim shared %>", core.Settings{DefaultLanguage: "VBScript"})
	unrelated := core.ParseDocument("file:///unrelated.asp", "<% Dim sentinel %>", core.Settings{DefaultLanguage: "VBScript"})
	affectedKey := workspacepkg.FileIdentityKeyFromURI(affected.URI) + "#1"
	unrelatedKey := workspacepkg.FileIdentityKeyFromURI(unrelated.URI) + "#1"
	server.referenceDescriptorFingerprints[affectedKey] = &workspaceReferenceDescriptorFingerprintCache{}
	server.referenceDescriptorFingerprints[unrelatedKey] = &workspaceReferenceDescriptorFingerprintCache{}
	server.referenceDeclarationPlans[affected] = workspaceReferenceDeclarationPlan{}
	server.referenceDeclarationPlans[unrelated] = workspaceReferenceDeclarationPlan{}

	server.mu.Lock()
	server.invalidateWorkspaceReferenceFamilyLocked(map[string]struct{}{
		workspacepkg.FileIdentityKeyFromURI(affected.URI): {},
	})
	server.mu.Unlock()

	if _, ok := server.referenceDescriptorFingerprints[affectedKey]; ok {
		t.Fatal("affected descriptor fingerprint cache survived family invalidation")
	}
	if _, ok := server.referenceDeclarationPlans[affected]; ok {
		t.Fatal("affected declaration plan survived family invalidation")
	}
	if _, ok := server.referenceDescriptorFingerprints[unrelatedKey]; !ok {
		t.Fatal("unrelated descriptor fingerprint cache was invalidated")
	}
	if _, ok := server.referenceDeclarationPlans[unrelated]; !ok {
		t.Fatal("unrelated declaration plan was invalidated")
	}
}

func TestWorkspaceReferenceInvalidationClearsOnlyAffectedNamesForSemanticChange(t *testing.T) {
	server := New(nil, io.Discard, io.Discard)
	previous := core.ParseDocument("file:///first.asp", "<% Dim shared : shared = 1 %>", core.Settings{DefaultLanguage: "VBScript"})
	current := core.ParseDocument(previous.URI, "<% Dim replacement : replacement = 1 %>", core.Settings{DefaultLanguage: "VBScript"})
	sentinel := workspaceReferenceTargetKey{Name: "sentinel"}
	shared := workspaceReferenceTargetKey{Name: "shared"}
	server.referenceCounts[sentinel] = 12
	server.referenceCounts[shared] = 3
	generation := server.referenceGeneration

	server.invalidateWorkspaceReferencesForParsedChange(previous, current)

	if server.referenceGeneration != generation {
		t.Fatalf("generation = %d, want preserved %d", server.referenceGeneration, generation)
	}
	if got := server.referenceCounts[sentinel]; got != 12 {
		t.Fatalf("unaffected cached count = %d, want 12", got)
	}
	if _, ok := server.referenceCounts[shared]; ok {
		t.Fatalf("affected cached count survived semantic change: %#v", server.referenceCounts)
	}
}

func TestWorkspaceReferenceNameInvalidationPreservesUnrelatedBatchAndImplicitPlan(t *testing.T) {
	server := New(nil, io.Discard, io.Discard)
	previous := core.ParseDocument("file:///first.asp", "<% Dim shared : shared = 1 %>", core.Settings{DefaultLanguage: "VBScript"})
	current := core.ParseDocument(previous.URI, "<% Dim replacement : replacement = 1 %>", core.Settings{DefaultLanguage: "VBScript"})
	affectedContext, affectedCancel := context.WithCancel(context.Background())
	unrelatedContext, unrelatedCancel := context.WithCancel(context.Background())
	defer unrelatedCancel()
	affectedKey := workspaceReferenceBatchKey{DocumentKey: "affected"}
	unrelatedKey := workspaceReferenceBatchKey{DocumentKey: "unrelated"}
	server.referenceBatch[affectedKey] = &workspaceReferenceBatchState{
		ctx: affectedContext, cancel: affectedCancel, nameRevisions: map[string]uint64{"shared": 1},
	}
	server.referenceBatch[unrelatedKey] = &workspaceReferenceBatchState{
		ctx: unrelatedContext, cancel: unrelatedCancel, nameRevisions: map[string]uint64{"sentinel": 1},
	}
	planKey := workspaceReferenceImplicitPlanKey{DocumentKey: "root"}
	server.referenceImplicitPlans[planKey] = map[string]map[string]struct{}{
		"shared":   {"first": {}},
		"sentinel": {"second": {}},
	}

	server.invalidateWorkspaceReferenceNames(previous, current, []string{"shared"})

	if _, ok := server.referenceBatch[affectedKey]; ok || affectedContext.Err() == nil {
		t.Fatal("affected batch survived name invalidation")
	}
	if _, ok := server.referenceBatch[unrelatedKey]; !ok || unrelatedContext.Err() != nil {
		t.Fatal("unrelated batch was canceled by another name")
	}
	plan := server.referenceImplicitPlans[planKey]
	if _, ok := plan["shared"]; ok {
		t.Fatal("affected implicit-global name survived invalidation")
	}
	if _, ok := plan["sentinel"]; !ok {
		t.Fatal("unrelated implicit-global name was invalidated")
	}
}

func TestWorkspaceReferenceInvalidationClearsOnlyAffectedIncludeFamily(t *testing.T) {
	server := New(nil, io.Discard, io.Discard)
	server.workspaceIncludeGraph.Reset(server.workspaceDiskSettingsKey())
	server.workspaceIncludeGraphComplete = true
	previous := core.ParseDocument("file:///first.asp", "<!-- #include file=\"one.inc\" --><% shared = 1 %>", core.Settings{DefaultLanguage: "VBScript"})
	current := core.ParseDocument(previous.URI, "<!-- #include file=\"two.inc\" --><% shared = 1 %>", core.Settings{DefaultLanguage: "VBScript"})
	affected := workspaceReferenceTargetKey{URI: previous.URI, Name: "shared"}
	unrelated := workspaceReferenceTargetKey{URI: "file:///unrelated.asp", Name: "sentinel"}
	server.referenceCounts[affected] = 3
	server.referenceCounts[unrelated] = 12
	generation := server.referenceGeneration

	server.invalidateWorkspaceReferencesForParsedChange(previous, current)

	if server.referenceGeneration != generation {
		t.Fatalf("generation = %d, want preserved %d", server.referenceGeneration, generation)
	}
	if _, ok := server.referenceCounts[affected]; ok {
		t.Fatal("affected include-family count survived topology change")
	}
	if got := server.referenceCounts[unrelated]; got != 12 {
		t.Fatalf("unrelated count = %d, want 12", got)
	}
}

func TestWorkspaceReferenceIndexFiltersAndReplacesChangedDocuments(t *testing.T) {
	index := newWorkspaceReferenceIndex()
	first := core.ParseDocument("file:///first.asp", "<% Dim shared : shared = 1 %>", core.Settings{DefaultLanguage: "VBScript"})
	second := core.ParseDocument("file:///second.asp", "<% Dim other : other = 1 %>", core.Settings{DefaultLanguage: "VBScript"})

	matching := index.documentsForName("SHARED", []*core.ParsedDocument{first, second})
	if len(matching) != 1 || matching[0].URI != first.URI {
		t.Fatalf("shared candidates = %#v, want first document", matching)
	}

	changed := core.ParseDocument(first.URI, "<% Dim replacement : replacement = 1 %>", core.Settings{DefaultLanguage: "VBScript"})
	if matching = index.documentsForName("shared", []*core.ParsedDocument{changed, second}); len(matching) != 0 {
		t.Fatalf("stale shared candidates = %#v, want none", matching)
	}
	matching = index.documentsForName("replacement", []*core.ParsedDocument{changed, second})
	if len(matching) != 1 || matching[0].URI != changed.URI {
		t.Fatalf("replacement candidates = %#v, want changed document", matching)
	}
}

func TestWorkspaceReferenceIndexPreservesSemanticRevisionForUnrelatedSourceChange(t *testing.T) {
	index := newWorkspaceReferenceIndex()
	first := core.ParseDocument("file:///first.asp", "<% Dim shared : shared = 1 %><p>first</p>", core.Settings{DefaultLanguage: "VBScript"})
	if update := index.update([]*core.ParsedDocument{first}); update.ChangedDocuments != 1 {
		t.Fatalf("initial update = %#v, want one changed document", update)
	}
	initialRevision := index.revisionNumber()
	initialFingerprint, ok := index.semanticFingerprintForDocuments([]*core.ParsedDocument{first})
	if !ok || initialFingerprint == "" {
		t.Fatalf("initial semantic fingerprint = %q, %t", initialFingerprint, ok)
	}

	changed := core.ParseDocument(first.URI, "<% Dim shared : shared = 1 %><p>changed but unrelated</p>", core.Settings{DefaultLanguage: "VBScript"})
	update := index.update([]*core.ParsedDocument{changed})
	if update.ChangedDocuments != 0 || update.SemanticUnchangedDocuments != 1 || len(update.AffectedNames) != 0 {
		t.Fatalf("unrelated source update = %#v, want semantic reuse", update)
	}
	if got := index.revisionNumber(); got != initialRevision {
		t.Fatalf("revision = %d, want unchanged %d", got, initialRevision)
	}
	gotFingerprint, ok := index.semanticFingerprintForDocuments([]*core.ParsedDocument{changed})
	if !ok || gotFingerprint != initialFingerprint {
		t.Fatalf("semantic fingerprint = %q, %t, want %q, true", gotFingerprint, ok, initialFingerprint)
	}
	segments := index.segmentsForName("shared", []*core.ParsedDocument{changed})
	if len(segments) != 1 || segments[0].parsed != changed {
		t.Fatalf("segments = %#v, want changed parsed document", segments)
	}
}

func TestWorkspaceReferenceIndexSeparatesCountAndLocationChanges(t *testing.T) {
	index := newWorkspaceReferenceIndex()
	first := core.ParseDocument("file:///first.asp", "<% Dim shared : shared = 1 %>", core.Settings{DefaultLanguage: "VBScript"})
	index.update([]*core.ParsedDocument{first})
	countRevision := index.revisionNumber()
	locationRevision := index.locationRevisionNumber()

	moved := core.ParseDocument(first.URI, "<% Dim shared\nshared = 1\n%>", core.Settings{DefaultLanguage: "VBScript"})
	update := index.update([]*core.ParsedDocument{moved})
	if len(update.CountAffectedNames) != 0 {
		t.Fatalf("count-affected names = %#v, want none", update.CountAffectedNames)
	}
	if len(update.LocationAffectedNames) != 1 || update.LocationAffectedNames[0] != "shared" {
		t.Fatalf("location-affected names = %#v, want shared", update.LocationAffectedNames)
	}
	if got := index.revisionNumber(); got != countRevision {
		t.Fatalf("count revision = %d, want unchanged %d", got, countRevision)
	}
	if got := index.locationRevisionNumber(); got != locationRevision+1 {
		t.Fatalf("location revision = %d, want %d", got, locationRevision+1)
	}
}

func TestWorkspaceReferenceSegmentFingerprintIncludesDeclarationOverlay(t *testing.T) {
	counts := workspaceReferenceCountSummary{Declarations: 1, Total: 2, CodeLensReferences: 1}
	first := map[lsp.Range]struct{}{{Start: lsp.Position{Line: 1, Character: 4}, End: lsp.Position{Line: 1, Character: 10}}: {}}
	second := map[lsp.Range]struct{}{{Start: lsp.Position{Line: 2, Character: 4}, End: lsp.Position{Line: 2, Character: 10}}: {}}
	if workspaceReferenceSegmentFingerprint("shard", counts, first) == workspaceReferenceSegmentFingerprint("shard", counts, second) {
		t.Fatal("declaration overlay range did not affect the segment fingerprint")
	}
}

func TestWorkspaceReferenceIndexRemoveDocumentRejectsStalePreparedWork(t *testing.T) {
	index := newWorkspaceReferenceIndex()
	first := core.ParseDocument("file:///first.asp", "<% Dim shared : shared = 1 %>", core.Settings{DefaultLanguage: "VBScript"})
	index.update([]*core.ParsedDocument{first})
	stale := core.ParseDocument(first.URI, "<% Dim stale : stale = 1 %>", core.Settings{DefaultLanguage: "VBScript"})
	staleTicket := index.sequence.Add(1)
	prepared := prepareWorkspaceReferenceDocument(
		workspacepkg.FileIdentityKeyFromURI(stale.URI), stale, workspacepkg.DiskContentHash(stale.Text), vbscript.BuildReferenceShard(stale), staleTicket,
	)
	countRevision := index.revisionNumber()
	locationRevision := index.locationRevisionNumber()

	update := index.removeDocument(first.URI)
	if update.ChangedDocuments != 1 || len(update.CountAffectedNames) != 2 || update.CountAffectedNames[0] != "dim" || update.CountAffectedNames[1] != "shared" {
		t.Fatalf("remove update = %#v, want dim and shared", update)
	}
	if got := index.revisionNumber(); got != countRevision+1 {
		t.Fatalf("count revision = %d, want %d", got, countRevision+1)
	}
	if got := index.locationRevisionNumber(); got != locationRevision+1 {
		t.Fatalf("location revision = %d, want %d", got, locationRevision+1)
	}
	if update := index.applyPrepared([]workspaceReferencePreparedDocument{prepared}); update.ChangedDocuments != 0 {
		t.Fatalf("stale prepared update after removal = %#v, want ignored", update)
	}
	index.mu.RLock()
	removedSegments := len(index.names["shared"])
	index.mu.RUnlock()
	if removedSegments != 0 {
		t.Fatalf("removed shared segments = %d, want none", removedSegments)
	}
	if segments := index.segmentsForName("stale", []*core.ParsedDocument{stale}); len(segments) != 1 {
		t.Fatalf("fresh stale-name segments = %d, want 1 after a newer publish", len(segments))
	}
}

func TestWorkspaceReferenceIndexReportsAffectedNamesAndCounts(t *testing.T) {
	index := newWorkspaceReferenceIndex()
	first := core.ParseDocument("file:///first.asp", "<% Dim shared : shared = shared + 1 %>", core.Settings{DefaultLanguage: "VBScript"})
	index.update([]*core.ParsedDocument{first})
	segments := index.segmentsForName("shared", []*core.ParsedDocument{first})
	if len(segments) != 1 {
		t.Fatalf("shared segments = %d, want 1", len(segments))
	}
	if got := segments[0].counts; got.Total != 3 || got.Declarations != 1 || got.Reads != 1 || got.Writes != 1 {
		t.Fatalf("shared counts = %#v, want total=3 declaration=1 read=1 write=1", got)
	}

	changed := core.ParseDocument(first.URI, "<% Dim replacement : replacement = 1 %>", core.Settings{DefaultLanguage: "VBScript"})
	update := index.update([]*core.ParsedDocument{changed})
	if update.ChangedDocuments != 1 || len(update.AffectedNames) != 2 || update.AffectedNames[0] != "replacement" || update.AffectedNames[1] != "shared" {
		t.Fatalf("changed update = %#v, want replacement and shared", update)
	}
}

func TestWorkspaceReferenceIndexSegmentsPreserveCandidateOrder(t *testing.T) {
	index := newWorkspaceReferenceIndex()
	first := core.ParseDocument("file:///first.asp", "<% shared = 1 %>", core.Settings{DefaultLanguage: "VBScript"})
	second := core.ParseDocument("file:///second.asp", "<% shared = 2 %>", core.Settings{DefaultLanguage: "VBScript"})
	third := core.ParseDocument("file:///third.asp", "<% shared = 3 %>", core.Settings{DefaultLanguage: "VBScript"})
	candidates := []*core.ParsedDocument{third, nil, first, second, first}

	segments := index.segmentsForName("shared", candidates)
	if len(segments) != 3 {
		t.Fatalf("segments = %d, want 3 unique documents", len(segments))
	}
	want := []*core.ParsedDocument{third, first, second}
	for position, segment := range segments {
		if segment.parsed != want[position] {
			t.Fatalf("segment %d parsed = %s, want %s", position, segment.parsed.URI, want[position].URI)
		}
	}
}

func TestWorkspaceReferenceIndexSparseSegmentsPreserveCandidateOrder(t *testing.T) {
	index := newWorkspaceReferenceIndex()
	documents := make([]*core.ParsedDocument, 64)
	for position := range documents {
		source := "<% unrelated = 1 %>"
		if position == 3 || position == 29 || position == 57 {
			source = "<% sparse = 1 %>"
		}
		documents[position] = core.ParseDocument("file:///sparse-"+strconv.Itoa(position)+".asp", source, core.Settings{DefaultLanguage: "VBScript"})
	}
	candidates := append([]*core.ParsedDocument(nil), documents...)
	candidates[3], candidates[57] = candidates[57], candidates[3]

	segments := index.segmentsForNamesContext(context.Background(), []string{"SPARSE"}, candidates)["sparse"]
	if len(segments) != 3 {
		t.Fatalf("sparse segments = %d, want 3", len(segments))
	}
	want := []*core.ParsedDocument{documents[57], documents[29], documents[3]}
	for position, segment := range segments {
		if segment.parsed != want[position] {
			t.Fatalf("sparse segment %d parsed = %s, want %s", position, segment.parsed.URI, want[position].URI)
		}
	}
}

func TestWorkspaceReferenceIndexCandidateScratchIsBoundedAfterDocumentIDChurn(t *testing.T) {
	index := newWorkspaceReferenceIndex()
	first := core.ParseDocument("file:///first.asp", "<% sparse = 1 %>", core.Settings{DefaultLanguage: "VBScript"})
	second := core.ParseDocument("file:///second.asp", "<% sparse = 2 %>", core.Settings{DefaultLanguage: "VBScript"})
	index.updateCountContext(context.Background(), []*core.ParsedDocument{first, second})
	index.mu.Lock()
	index.nextDocumentID = 1 << 40
	index.mu.Unlock()

	segments := index.segmentsForNamesContext(context.Background(), []string{"sparse"}, []*core.ParsedDocument{second, first})["sparse"]
	if len(segments) != 2 || segments[0].parsed != second || segments[1].parsed != first {
		t.Fatalf("segments after document ID churn = %#v, want second then first", segments)
	}
}

func TestWorkspaceReferenceCountPreparationSkipsPostingsAndEmbeddedClassesUntilFullUpgrade(t *testing.T) {
	index := newWorkspaceReferenceIndex()
	parsed := core.ParseDocument("file:///count-only.asp", `<style>.shared { color: red; }</style><div class="shared"></div><% SharedValue = 1 %>`, core.Settings{DefaultLanguage: "VBScript"})
	var embeddedBuilds atomic.Int32
	index.embeddedBuildTestHook = func(string) { embeddedBuilds.Add(1) }

	segments := index.segmentsForNamesContext(context.Background(), []string{"SharedValue"}, []*core.ParsedDocument{parsed})["sharedvalue"]
	if len(segments) != 1 || segments[0].postingsLoaded || segments[0].postings != nil {
		t.Fatalf("count-only segments = %#v, want one summary without postings", segments)
	}
	if len(segments[0].implicitAdjustments) != 1 {
		t.Fatalf("count-only implicit adjustments = %#v, want one self-reference adjustment", segments[0].implicitAdjustments)
	}
	if got := embeddedBuilds.Load(); got != 0 {
		t.Fatalf("embedded builds during count preparation = %d, want 0", got)
	}
	index.mu.RLock()
	entry := index.documents[workspacepkg.FileIdentityKeyFromURI(parsed.URI)]
	index.mu.RUnlock()
	if !entry.countSummaryOnly || len(entry.embeddedSegments) != 0 {
		t.Fatalf("count-only entry = %#v, want no embedded segments", entry)
	}

	full := index.segmentsForNameContext(context.Background(), "SharedValue", []*core.ParsedDocument{parsed})
	if len(full) != 1 || !full[0].postingsLoaded || len(full[0].postings) == 0 {
		t.Fatalf("full-upgrade segments = %#v, want loaded postings", full)
	}
	if got := embeddedBuilds.Load(); got != 1 {
		t.Fatalf("embedded builds after full upgrade = %d, want 1", got)
	}
	if ranges := index.embeddedClassRanges("shared", []*core.ParsedDocument{parsed}); len(ranges) != 1 || len(ranges[0].Ranges) != 2 {
		t.Fatalf("embedded ranges after full upgrade = %#v, want CSS and HTML ranges", ranges)
	}
}

func TestWorkspaceReferenceIndexSemanticFingerprintDoesNotReadMissingDocuments(t *testing.T) {
	index := newWorkspaceReferenceIndex()
	parsed := core.ParseDocument("file:///missing.asp", "<% value = 1 %>", core.Settings{DefaultLanguage: "VBScript"})
	if fingerprint, ok := index.semanticFingerprintForDocuments([]*core.ParsedDocument{parsed}); ok || fingerprint != "" {
		t.Fatalf("missing fingerprint = %q, %t, want empty false", fingerprint, ok)
	}
}

func TestWorkspaceReferenceNameFingerprintIgnoresOutOfScopeDocuments(t *testing.T) {
	index := newWorkspaceReferenceIndex()
	inScope := core.ParseDocument("file:///scope.asp", "<% Dim shared : shared = 1 %>", core.Settings{DefaultLanguage: "VBScript"})
	outOfScope := core.ParseDocument("file:///other.asp", "<% Dim shared : shared = 2 %>", core.Settings{DefaultLanguage: "VBScript"})
	index.update([]*core.ParsedDocument{inScope, outOfScope})
	before := index.semanticFingerprintsForNames([]string{"shared"}, []*core.ParsedDocument{inScope})["shared"]

	changed := core.ParseDocument(outOfScope.URI, "<% Dim shared : shared = shared + 3 %>", core.Settings{DefaultLanguage: "VBScript"})
	index.update([]*core.ParsedDocument{changed})
	after := index.semanticFingerprintsForNames([]string{"shared"}, []*core.ParsedDocument{inScope})["shared"]
	if after != before {
		t.Fatalf("out-of-scope change altered fingerprint: before=%q after=%q", before, after)
	}
}

func TestWorkspaceReferenceIndexClearRejectsPreparedWork(t *testing.T) {
	index := newWorkspaceReferenceIndex()
	parsed := core.ParseDocument("file:///stale.asp", "<% Dim stale %>", core.Settings{DefaultLanguage: "VBScript"})
	prepared := prepareWorkspaceReferenceDocument(
		"file:///stale.asp",
		parsed,
		"source",
		vbscript.BuildReferenceShard(parsed),
		1,
	)
	index.clear()
	if update := index.applyPrepared([]workspaceReferencePreparedDocument{prepared}); update.ChangedDocuments != 0 {
		t.Fatalf("stale update after clear = %#v, want ignored", update)
	}
	if matches := index.documentsForName("stale", nil); len(matches) != 0 {
		t.Fatalf("stale matches after clear = %#v, want none", matches)
	}
}

func TestWorkspaceReferenceIndexMemoryEstimateIncludesHydration(t *testing.T) {
	index := newWorkspaceReferenceIndex()
	parsed := core.ParseDocument("file:///hydrated.asp", `<% Dim SharedValue : SharedValue = 1 %>`, core.Settings{DefaultLanguage: "VBScript"})
	segments := index.segmentsForNamesContext(context.Background(), []string{"SharedValue"}, []*core.ParsedDocument{parsed})["sharedvalue"]
	if len(segments) != 1 || segments[0].hydration == nil {
		t.Fatalf("count-only segments = %#v, want one hydratable segment", segments)
	}
	before, _ := index.estimateMemory()
	shard := vbscript.BuildReferenceShard(parsed)
	hydration := segments[0].hydration
	hydration.mu.Lock()
	hydration.ready = true
	hydration.postings = shard.PostingsFor("SharedValue")
	hydration.globalResolutions = shard.GlobalResolutionsFor("SharedValue")
	hydration.declarationRanges = vbReferenceDeclarationRanges(parsed, "SharedValue")
	hydration.mu.Unlock()
	after, _ := index.estimateMemory()
	if after <= before {
		t.Fatalf("memory estimate after hydration = %d, want greater than %d", after, before)
	}
}
