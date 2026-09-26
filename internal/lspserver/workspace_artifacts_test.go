package lspserver

import (
	"context"
	"reflect"
	"strings"
	"testing"

	"github.com/yottonoko/classic-web-system-lsp/internal/core"
	"github.com/yottonoko/classic-web-system-lsp/internal/lsp"
	"github.com/yottonoko/classic-web-system-lsp/internal/vbscript"
)

func TestWorkspaceDocumentIDCanonicalizesWindowsAndUNCIdentity(t *testing.T) {
	tests := []struct {
		left  string
		right string
	}{
		{"file:///C:/Workspace/Site/Page.asp", "file:///c:/workspace/site/page.asp"},
		{"file://SERVER/Share/Site/Page.asp", "file://server/share/site/page.asp"},
	}
	for _, test := range tests {
		if left, right := workspaceDocumentIDFromURI(test.left), workspaceDocumentIDFromURI(test.right); left != right {
			t.Fatalf("workspaceDocumentIDFromURI(%q) = %q, workspaceDocumentIDFromURI(%q) = %q", test.left, left, test.right, right)
		}
	}
	if lower, upper := workspaceDocumentIDFromURI("file:///tmp/page.asp"), workspaceDocumentIDFromURI("file:///tmp/Page.asp"); lower == upper {
		t.Fatalf("POSIX case-sensitive identities collapsed to %q", lower)
	}
}

func TestWorkspaceExecutionTapePreservesIncludeAndImplicitSourceOrder(t *testing.T) {
	source := "<% first = 1 %>\n<!-- #include file=\"middle.inc\" -->\n<% second = 2 %>"
	parsed := core.ParseDocument("file:///workspace/root.asp", source, core.Settings{})
	if len(parsed.Includes) != 1 {
		t.Fatalf("includes = %#v, want one", parsed.Includes)
	}
	doc := core.NewTextDocument(parsed.URI, "classic-asp", 0, parsed.Text)
	firstStart := doc.OffsetAt(lsp.Position{Line: 0, Character: 3})
	secondStart := doc.OffsetAt(lsp.Position{Line: 2, Character: 3})
	edges := []workspaceIncludeArtifactEdge{{Path: "middle.inc", Mode: "file", DocumentID: workspaceDocumentID("/workspace/middle.inc"), Exists: true, Range: parsed.Includes[0].Range}}
	tape := workspaceExecutionTape(parsed, []vbUsageDeclaration{
		{Name: "second", Kind: "variable", Implicit: true, Start: secondStart, Range: lsp.Range{Start: lsp.Position{Line: 2, Character: 3}, End: lsp.Position{Line: 2, Character: 9}}},
		{Name: "first", Kind: "variable", Implicit: true, Start: firstStart, Range: lsp.Range{Start: lsp.Position{Line: 0, Character: 3}, End: lsp.Position{Line: 0, Character: 8}}},
	}, edges)
	wantKinds := []workspaceDocumentExecutionEventKind{workspaceExecutionImplicitDeclaration, workspaceExecutionInclude, workspaceExecutionImplicitDeclaration}
	gotKinds := make([]workspaceDocumentExecutionEventKind, len(tape))
	for index := range tape {
		gotKinds[index] = tape[index].Kind
	}
	if !reflect.DeepEqual(gotKinds, wantKinds) {
		t.Fatalf("execution tape kinds = %#v, want %#v", gotKinds, wantKinds)
	}
	if tape[0].Name != "first" || tape[2].Name != "second" {
		t.Fatalf("execution tape = %#v", tape)
	}
}

func TestWorkspaceReferenceFingerprintSeparatesCountsFromLocations(t *testing.T) {
	before := workspaceReferenceFingerprints(&vbscript.ReferenceShard{
		Declarations: map[string]vbscript.Symbol{},
		Postings: map[string][]vbscript.ReferencePosting{
			"value": {{Name: "Value", Range: lsp.Range{Start: lsp.Position{Line: 1, Character: 2}, End: lsp.Position{Line: 1, Character: 7}}, Roles: vbscript.ReferenceRoleRead}},
		},
	})
	after := workspaceReferenceFingerprints(&vbscript.ReferenceShard{
		Declarations: map[string]vbscript.Symbol{},
		Postings: map[string][]vbscript.ReferencePosting{
			"value": {{Name: "Value", Range: lsp.Range{Start: lsp.Position{Line: 4, Character: 5}, End: lsp.Position{Line: 4, Character: 10}}, Roles: vbscript.ReferenceRoleRead}},
		},
	})
	if before["value"].CountFingerprint != after["value"].CountFingerprint {
		t.Fatal("moving an occurrence invalidated its count fingerprint")
	}
	if before["value"].LocationFingerprint == after["value"].LocationFingerprint {
		t.Fatal("moving an occurrence did not invalidate its location fingerprint")
	}
	counts, places := workspaceChangedReferences(before, after)
	if len(counts) != 0 || !reflect.DeepEqual(places, []string{"value"}) {
		t.Fatalf("changed references = counts %#v, locations %#v", counts, places)
	}
}

func TestCompareWorkspaceDocumentArtifactsReportsOnlyChangedNamesAndComponents(t *testing.T) {
	before := &workspaceDocumentArtifactManifest{
		SourceFingerprint:         "source-a",
		ParserSettingsFingerprint: "settings",
		IncludeFingerprint:        "includes",
		ExecutionTapeFingerprint:  "tape",
		PublicSymbols:             map[string]workspaceArtifactFingerprint{"alpha": "a", "stable": "same"},
		ExternalUsages:            map[string]workspaceArtifactFingerprint{"consumer": "old"},
		ImplicitGlobalCandidates:  map[string]workspaceArtifactFingerprint{},
		ObjectTagVariables:        map[string]workspaceArtifactFingerprint{},
		References: map[string]workspaceReferenceArtifactSegment{
			"alpha": {CountFingerprint: "count", LocationFingerprint: "old-place"},
		},
		VirtualDocuments: map[core.EmbeddedLanguage]workspaceArtifactFingerprint{core.LanguageJavaScript: "same"},
		LocalDiagnostics: map[string]workspaceArtifactFingerprint{"vb": "old", "html": "same"},
	}
	after := &workspaceDocumentArtifactManifest{
		SourceFingerprint:         "source-b",
		ParserSettingsFingerprint: "settings",
		IncludeFingerprint:        "includes",
		ExecutionTapeFingerprint:  "tape",
		PublicSymbols:             map[string]workspaceArtifactFingerprint{"alpha": "b", "stable": "same"},
		ExternalUsages:            map[string]workspaceArtifactFingerprint{"consumer": "new"},
		ImplicitGlobalCandidates:  map[string]workspaceArtifactFingerprint{},
		ObjectTagVariables:        map[string]workspaceArtifactFingerprint{},
		References: map[string]workspaceReferenceArtifactSegment{
			"alpha": {CountFingerprint: "count", LocationFingerprint: "new-place"},
		},
		VirtualDocuments: map[core.EmbeddedLanguage]workspaceArtifactFingerprint{core.LanguageJavaScript: "same"},
		LocalDiagnostics: map[string]workspaceArtifactFingerprint{"vb": "new", "html": "same"},
	}
	delta := compareWorkspaceDocumentArtifacts(before, after)
	if !delta.SourceChanged || !delta.CSTChanged || delta.ParserSettingsChanged || delta.IncludeEdgesChanged || delta.ExecutionTapeChanged {
		t.Fatalf("component delta = %#v", delta)
	}
	if !reflect.DeepEqual(delta.ChangedPublicNames, []string{"alpha"}) || !reflect.DeepEqual(delta.ChangedUsageNames, []string{"consumer"}) {
		t.Fatalf("name delta = %#v", delta)
	}
	if len(delta.ChangedReferenceCounts) != 0 || !reflect.DeepEqual(delta.ChangedReferenceLocations, []string{"alpha"}) {
		t.Fatalf("reference delta = %#v", delta)
	}
	if len(delta.ChangedVirtualLanguages) != 0 || !reflect.DeepEqual(delta.ChangedDiagnosticLayers, []string{"vb"}) {
		t.Fatalf("feature delta = %#v", delta)
	}
}

func TestWorkspaceDocumentArtifactManifestWithDiagnosticsReturnsNewLayerSet(t *testing.T) {
	original := &workspaceDocumentArtifactManifest{LocalDiagnostics: map[string]workspaceArtifactFingerprint{"syntax": "old"}}
	updated := workspaceDocumentArtifactManifestWithDiagnostics(original, map[string][]lsp.Diagnostic{
		"syntax": {{Message: "Expected closing delimiter."}},
		"vb":     nil,
	})
	if original.LocalDiagnostics["syntax"] != "old" || len(original.LocalDiagnostics) != 1 {
		t.Fatalf("original diagnostics were mutated: %#v", original.LocalDiagnostics)
	}
	if updated == original || len(updated.LocalDiagnostics) != 2 || updated.LocalDiagnostics["syntax"] == "old" {
		t.Fatalf("updated diagnostics = %#v", updated.LocalDiagnostics)
	}
}

func TestWorkspaceDocumentArtifactEstimateTracksRetainedStructure(t *testing.T) {
	small := buildWorkspaceDocumentArtifactManifest(core.ParseDocument("file:///workspace/small.asp", "<% value = 1 %>", core.Settings{}), nil, "vbscript")
	large := buildWorkspaceDocumentArtifactManifest(core.ParseDocument("file:///workspace/large.asp", strings.Repeat("<% value = value + 1 %>\n", 200), core.Settings{}), nil, "vbscript")
	smallBytes := estimateWorkspaceDocumentArtifactBytes(small)
	largeBytes := estimateWorkspaceDocumentArtifactBytes(large)
	if smallBytes <= 0 || largeBytes <= smallBytes {
		t.Fatalf("workspace artifact estimates small=%d large=%d", smallBytes, largeBytes)
	}
}

func TestWorkspaceArtifactSnapshotReleasesTransientReferenceCST(t *testing.T) {
	parsed := core.ParseDocument("file:///workspace/transient-cst.asp", "<% Dim value : value = value + 1 %>", core.Settings{DefaultLanguage: "VBScript"})
	snapshot := buildWorkspaceArtifactSnapshotReducedContext(context.Background(), parsed)
	if snapshot == nil || snapshot.ReferenceShard == nil {
		t.Fatal("workspace artifact snapshot did not build")
	}

	withoutCST := &vbscript.ReferenceShard{
		Version:      snapshot.ReferenceShard.Version,
		Declarations: snapshot.ReferenceShard.Declarations,
		Postings:     snapshot.ReferenceShard.Postings,
		Scopes:       snapshot.ReferenceShard.Scopes,
	}
	if got, want := snapshot.ReferenceShard.EstimateBytes(), withoutCST.EstimateBytes(); got != want {
		t.Fatalf("workspace artifact shard estimate = %d, want released no-CST estimate %d", got, want)
	}

	reattached := vbscript.ParseDocumentCST(parsed)
	if reattached == nil {
		t.Fatal("released workspace artifact CST did not reattach")
	}
	if got, want := snapshot.ReferenceShard.EstimateBytes(), withoutCST.EstimateBytes()+reattached.EstimateBytes(); got != want {
		t.Fatalf("reattached workspace artifact shard estimate = %d, want %d", got, want)
	}
}

func TestWorkspaceArtifactEstimateTracksSummaryGrowth(t *testing.T) {
	base := &workspaceArtifactSnapshot{
		URI:                          "file:///workspace/root.asp",
		IncludeResolutionFingerprint: "resolution",
		Usage: vbUsageDeclarations{Declarations: []vbUsageDeclaration{{
			Name: "value", Kind: "variable", Range: lsp.Range{Start: lsp.Position{Line: 1}, End: lsp.Position{Line: 1, Character: 5}},
		}}},
		ReferenceShard: &vbscript.ReferenceShard{
			Declarations: map[string]vbscript.Symbol{"value": {Name: "value", Kind: "variable"}},
			Postings:     map[string][]vbscript.ReferencePosting{"value": {{Name: "value"}}},
		},
		VirtualDocuments: map[core.EmbeddedLanguage]core.VirtualDocument{},
	}
	withoutRanges := *base
	withoutRanges.Summary = vbFileAnalysisSummary{VBScript: vbLocalSummary{
		ExternalRefUsages: []vbExternalRefUsage{{Key: "external", Count: 1}},
	}}
	withRanges := withoutRanges
	withRanges.Summary.VBScript.ExternalRefUsages = []vbExternalRefUsage{{
		Key: "external", Count: 1, Ranges: make([]lsp.Range, 16),
	}}
	if got, want := estimateWorkspaceArtifactSnapshotBytes(&withRanges), estimateWorkspaceArtifactSnapshotBytes(&withoutRanges); got <= want {
		t.Fatalf("summary range growth did not increase estimate: without=%d with=%d", want, got)
	}

	withoutCollections := *base
	withCollections := *base
	withCollections.Summary = vbFileAnalysisSummary{
		Fingerprint:         "summary-fingerprint",
		PublicSignatureHash: "public-signature-hash",
		VBScript: vbLocalSummary{
			Fingerprint: "vb-fingerprint",
			PublicSymbols: []vbPublicSummarySymbol{{
				Name: "PublicValue", Kind: "variable", TypeName: "String", MemberOf: "Thing", Visibility: "public",
			}},
			Exports: []vbExportSummary{{
				Name: "Thing", Kind: "class", Members: []vbExportSummary{{Name: "Value", Kind: "property"}},
			}},
			ExternalRefs:                 []vbExternalRef{{Name: "External", KindHint: "object", MemberName: "Value"}},
			ExternalRefUsages:            []vbExternalRefUsage{{Key: "external.value", Count: 1, Ranges: []lsp.Range{{}}}},
			ImplicitGlobalCandidateNames: []string{"ImplicitValue"},
		},
	}
	if got, want := estimateWorkspaceArtifactSnapshotBytes(&withCollections), estimateWorkspaceArtifactSnapshotBytes(&withoutCollections); got <= want {
		t.Fatalf("summary collection growth did not increase estimate: without=%d with=%d", want, got)
	}
}

func TestWorkspaceArtifactEstimateIncludesRetainedReferenceMetadata(t *testing.T) {
	smallParsed := core.ParseDocument("file:///workspace/reference-small.asp", "<% Dim value : value = value : value() %>", core.Settings{DefaultLanguage: "VBScript"})
	smallShard := vbscript.BuildReferenceShard(smallParsed)
	smallSnapshot := &workspaceArtifactSnapshot{
		URI:              smallParsed.URI,
		ReferenceShard:   smallShard,
		VirtualDocuments: map[core.EmbeddedLanguage]core.VirtualDocument{},
	}
	before := estimateWorkspaceArtifactSnapshotBytes(smallSnapshot)
	workspaceReferenceFingerprints(smallShard)
	after := estimateWorkspaceArtifactSnapshotBytes(smallSnapshot)
	if after <= before {
		t.Fatalf("materialized reference metadata did not increase workspace estimate: before=%d after=%d", before, after)
	}

	largeParsed := core.ParseDocument("file:///workspace/reference-large.asp", strings.Repeat("<% Dim value : value = value : value() %>\n", 64), core.Settings{DefaultLanguage: "VBScript"})
	largeShard := vbscript.BuildReferenceShard(largeParsed)
	workspaceReferenceFingerprints(largeShard)
	largeSnapshot := &workspaceArtifactSnapshot{
		URI:              largeParsed.URI,
		ReferenceShard:   largeShard,
		VirtualDocuments: map[core.EmbeddedLanguage]core.VirtualDocument{},
	}
	if got, want := estimateWorkspaceArtifactSnapshotBytes(largeSnapshot), after; got <= want {
		t.Fatalf("retained reference metadata growth did not increase workspace estimate: small=%d large=%d", want, got)
	}
}

func TestWorkspaceArtifactEstimateIncludesIncludeStringStorage(t *testing.T) {
	base := &workspaceArtifactSnapshot{
		URI:                          "file:///workspace/root.asp",
		IncludeResolutionFingerprint: "resolution",
		VirtualDocuments:             map[core.EmbeddedLanguage]core.VirtualDocument{},
	}
	withoutStrings := *base
	withoutStrings.Includes = []resolvedIncludeSnapshot{{Range: lsp.Range{Start: lsp.Position{Line: 2}, End: lsp.Position{Line: 2, Character: 1}}}}
	withStrings := *base
	withStrings.Includes = []resolvedIncludeSnapshot{{
		Path:         strings.Repeat("includes/shared/", 16),
		Mode:         "virtual",
		ResolvedPath: "/workspace/includes/shared/common.inc",
		URI:          "file:///workspace/includes/shared/common.inc",
		Range:        withoutStrings.Includes[0].Range,
		Exists:       true,
	}}
	if got, want := estimateWorkspaceArtifactSnapshotBytes(&withStrings), estimateWorkspaceArtifactSnapshotBytes(&withoutStrings); got <= want {
		t.Fatalf("include string growth did not increase estimate: without=%d with=%d", want, got)
	}
}

func TestWorkspaceArtifactManifestOwnsImmutableCSTAndDirectIncludes(t *testing.T) {
	parsed := core.ParseDocument("file:///workspace/root.asp", "<!-- #include file=\"leaf.inc\" -->\n<% Response.Write value %>", core.Settings{})
	snapshot := &fileAnalysisSnapshot{
		URI:            parsed.URI,
		ReferenceShard: &vbscript.ReferenceShard{Declarations: map[string]vbscript.Symbol{}, Postings: map[string][]vbscript.ReferencePosting{}},
		Includes: []resolvedIncludeSnapshot{{
			Path: "leaf.inc", Mode: "file", Range: parsed.Includes[0].Range,
			ResolvedPath: "/workspace/leaf.inc", URI: "file:///workspace/leaf.inc", Exists: true,
		}},
		VirtualDocuments: map[core.EmbeddedLanguage]core.VirtualDocument{},
	}
	manifest := buildWorkspaceDocumentArtifactManifest(parsed, snapshot, "vbscript")
	if len(manifest.IncludeEdges) != 1 || manifest.IncludeEdges[0].DocumentID != workspaceDocumentID("/workspace/leaf.inc") {
		t.Fatalf("include edges = %#v", manifest.IncludeEdges)
	}
	parsed.Includes[0].Path = "mutated.inc"
	parsed.Regions[0].Start = 42
	if manifest.CST.Includes[0].Path != "leaf.inc" || manifest.CST.Regions[0].Start == 42 {
		t.Fatalf("manifest CST changed through parsed document: %#v", manifest.CST)
	}
	if manifest.IncludeEdges[0].Path != "leaf.inc" {
		t.Fatalf("manifest edge changed through parsed document: %#v", manifest.IncludeEdges)
	}
}

func TestWorkspaceArtifactCSTCloneOmitsRuntimeBacking(t *testing.T) {
	parsed := core.ParseDocument("file:///workspace/clone-runtime.asp", "<% value %>", core.Settings{DefaultLanguage: "VBScript"})
	parsed.StoreAnalysis("workspace.test", []byte("persisted"))
	parsed.StoreRuntimeAnalysis("workspace.runtime", []byte("runtime"))
	previous := core.ParseDocument(parsed.URI, "<% previous %>", core.Settings{DefaultLanguage: "VBScript"})
	// The predecessor is intentionally installed through the core incremental
	// path so the structural clone is tested against both runtime maps.
	updated := core.UpdateParsedDocument(previous, []core.IncrementalChange{{
		Range: &lsp.Range{Start: lsp.Position{Line: 0, Character: 3}, End: lsp.Position{Line: 0, Character: 11}},
		Text:  "value",
	}}, core.Settings{DefaultLanguage: "VBScript"})
	if updated.Parsed == nil {
		t.Fatal("incremental predecessor setup returned nil parsed document")
	}
	updated.Parsed.StoreAnalysis("workspace.test", []byte("persisted"))
	updated.Parsed.StoreRuntimeAnalysis("workspace.runtime", []byte("runtime"))
	clone := cloneWorkspaceArtifactCST(updated.Parsed)
	if clone == nil || clone == updated.Parsed {
		t.Fatalf("clone = %#v, want distinct structural clone", clone)
	}
	if _, ok := clone.LoadRuntimeAnalysis("workspace.runtime"); ok {
		t.Fatal("workspace CST clone retained runtime analysis")
	}
	if _, ok := clone.PreviousRevisionText(); ok {
		t.Fatal("workspace CST clone retained incremental predecessor")
	}
	var persisted []byte
	if !clone.LoadAnalysis("workspace.test", &persisted) || string(persisted) != "persisted" {
		t.Fatalf("workspace CST clone lost persisted analysis: %q", persisted)
	}
	clone.Analysis["workspace.test"][0] = 'x'
	var original []byte
	if !updated.Parsed.LoadAnalysis("workspace.test", &original) || string(original) != "persisted" {
		t.Fatalf("workspace CST clone shares Analysis backing: %q", original)
	}
}
