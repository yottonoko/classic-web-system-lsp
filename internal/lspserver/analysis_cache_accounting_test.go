package lspserver

import (
	"context"
	"encoding/json"
	"io"
	"math"
	"strings"
	"testing"
	"time"

	"github.com/yottonoko/classic-web-system-lsp/internal/core"
	"github.com/yottonoko/classic-web-system-lsp/internal/lsp"
	"github.com/yottonoko/classic-web-system-lsp/internal/vbscript"
)

type analysisCacheTestRuntimeOwnerProvider struct {
	identity any
	bytes    int64
}

func (provider analysisCacheTestRuntimeOwnerProvider) RuntimeAnalysisMemoryOwnerSet() []core.RuntimeAnalysisMemoryOwner {
	return []core.RuntimeAnalysisMemoryOwner{{Identity: provider.identity, Bytes: provider.bytes}}
}

type analysisCacheTestExclusiveEstimator struct {
	bytes int64
}

func (estimator analysisCacheTestExclusiveEstimator) EstimateExclusiveRuntimeBytes() int64 {
	return estimator.bytes
}

func TestAnalysisCacheAccountingSaturatesEstimatorAndProviderOwners(t *testing.T) {
	tests := []struct {
		name  string
		value any
	}{
		{
			name:  "provider",
			value: analysisCacheTestRuntimeOwnerProvider{identity: new(int), bytes: math.MaxInt64},
		},
		{
			name:  "exclusive estimator",
			value: analysisCacheTestExclusiveEstimator{bytes: math.MaxInt64},
		},
	}
	for _, test := range tests {
		t.Run(test.name, func(t *testing.T) {
			parsed := core.ParseDocument("file:///site/analysis-cache-saturation.asp", "<% Dim value %>", core.Settings{DefaultLanguage: "VBScript"})
			parsed.StoreRuntimeAnalysis("test.analysis-cache-saturation", test.value)
			cache := newAnalysisCache()
			cache.rememberSnapshot(parsed, &fileAnalysisSnapshot{URI: parsed.URI, runtimeBacked: true})
			cache.rememberWorkspaceSnapshot(parsed, "saturation", &workspaceArtifactSnapshot{URI: parsed.URI})

			if got, entries := cache.memoryEstimate(); got != math.MaxInt64 || entries != 3 {
				t.Fatalf("saturated cache estimate = %d bytes, %d entries; want MaxInt64, 3", got, entries)
			}
			if got := cache.evict(0); got != math.MaxInt64 {
				t.Fatalf("saturated cache eviction freed %d bytes; want MaxInt64", got)
			}
			if got, entries := cache.memoryEstimate(); got != 0 || entries != 0 {
				t.Fatalf("evicted saturated cache estimate = %d bytes, %d entries; want 0, 0", got, entries)
			}
		})
	}
}

func TestAnalysisCacheByteAdditionNormalizesNegativeComponents(t *testing.T) {
	tests := []struct {
		name      string
		total     int64
		component int64
		want      int64
	}{
		{name: "negative total", total: -1, component: 64, want: 64},
		{name: "negative component", total: 64, component: -1, want: 64},
		{name: "both negative", total: -1, component: -1, want: 0},
		{name: "saturating sum", total: math.MaxInt64 - 1, component: 2, want: math.MaxInt64},
	}
	for _, test := range tests {
		t.Run(test.name, func(t *testing.T) {
			if got := addAnalysisCacheBytes(test.total, test.component); got != test.want {
				t.Fatalf("addAnalysisCacheBytes(%d, %d) = %d; want %d", test.total, test.component, got, test.want)
			}
		})
	}
}

func TestAnalysisCacheEstimateExcludesParsedOwnerAcrossLayers(t *testing.T) {
	cache := newAnalysisCache()
	parsed := core.ParseDocument("file:///site/shared.asp", "<% Dim value : value = 1 %>", core.Settings{DefaultLanguage: "VBScript"})
	declarations := []vbUsageDeclaration{{Name: "value", Kind: "variable"}}
	full := &fileAnalysisSnapshot{URI: parsed.URI, GraphDeclarations: declarations, runtimeBacked: true}
	reduced := &workspaceArtifactSnapshot{URI: parsed.URI, VirtualDocuments: map[core.EmbeddedLanguage]core.VirtualDocument{}}
	key := workspaceArtifactSnapshotCacheKey{parsed: parsed, includeResolutionFingerprint: "includes"}
	cache.declarations[parsed] = declarations
	cache.snapshots[parsed] = full
	cache.workspaceSnapshots[key] = reduced

	external := map[*core.ParsedDocument]struct{}{parsed: {}}
	got, entries := cache.memoryEstimateWithExternalParsed(external)
	want := estimateAnalysisDeclarationStorageBytes(nil, declarations) +
		estimateAnalysisCacheFileSnapshotBytes(parsed, full) + estimateAnalysisCacheWorkspaceSnapshotBytes(parsed, reduced)
	if got != want || entries != 3 {
		t.Fatalf("analysis cache estimate = %d, entries=%d; want %d, 3", got, entries, want)
	}
	if owned, _ := cache.memoryEstimate(); owned != want+parsed.EstimateBytes() {
		t.Fatalf("analysis-owned parsed estimate = %d, want %d", owned, want+parsed.EstimateBytes())
	}
}

func TestAnalysisCacheEstimateCountsDistinctParsedRevisions(t *testing.T) {
	cache := newAnalysisCache()
	first := core.ParseDocument("file:///site/revisions.asp", "<% Dim firstValue %>", core.Settings{DefaultLanguage: "VBScript"})
	second := core.ParseDocument("file:///site/revisions.asp", strings.Repeat("<% Dim secondValue %>\n", 64), core.Settings{DefaultLanguage: "VBScript"})
	firstSnapshot := &workspaceArtifactSnapshot{URI: first.URI, VirtualDocuments: map[core.EmbeddedLanguage]core.VirtualDocument{}}
	secondSnapshot := &workspaceArtifactSnapshot{URI: second.URI, VirtualDocuments: map[core.EmbeddedLanguage]core.VirtualDocument{}}
	cache.workspaceSnapshots[workspaceArtifactSnapshotCacheKey{parsed: first, includeResolutionFingerprint: "first"}] = firstSnapshot
	cache.workspaceSnapshots[workspaceArtifactSnapshotCacheKey{parsed: second, includeResolutionFingerprint: "second"}] = secondSnapshot

	got, entries := cache.memoryEstimate()
	want := estimateAnalysisCacheWorkspaceSnapshotBytes(first, firstSnapshot) + first.EstimateBytes() +
		estimateAnalysisCacheWorkspaceSnapshotBytes(second, secondSnapshot) + second.EstimateBytes()
	if got != want || entries != 2 {
		t.Fatalf("revision estimate = %d, entries=%d; want %d, 2", got, entries, want)
	}
	if first.EstimateBytes() >= second.EstimateBytes() {
		t.Fatalf("test revisions did not differ in parsed storage: first=%d second=%d", first.EstimateBytes(), second.EstimateBytes())
	}
}

func TestAnalysisCacheEstimateCountsExclusiveVirtualPayload(t *testing.T) {
	parsed := core.ParseDocument("file:///site/synthetic-virtual.asp", "<% Dim value %>", core.Settings{DefaultLanguage: "VBScript"})
	text := strings.Repeat("virtual-payload", 512)
	segments := make([]core.SourceMapSegment, 8)
	snapshot := &workspaceArtifactSnapshot{
		URI: parsed.URI,
		VirtualDocuments: map[core.EmbeddedLanguage]core.VirtualDocument{
			core.LanguageHTML: {
				URI:        parsed.URI + ".html",
				LanguageID: string(core.LanguageHTML),
				Text:       text,
				Segments:   segments,
			},
		},
	}
	cache := newAnalysisCache()
	cache.rememberWorkspaceSnapshot(parsed, "synthetic", snapshot)

	before, entries := cache.memoryEstimate()
	want := int64(256) + int64(64+128) + int64(len(text))*2 + 16 + int64(cap(segments))*48 + parsed.EstimateBytes()
	if before != want || entries != 1 {
		t.Fatalf("exclusive virtual payload estimate = %d bytes, %d entries; want %d bytes, 1 entry", before, entries, want)
	}
	if before <= 4096 {
		t.Fatalf("exclusive virtual payload was not counted: %d bytes", before)
	}
	if freed := cache.evict(0); freed != before {
		t.Fatalf("exclusive virtual payload eviction freed %d bytes; want %d", freed, before)
	}
}

func TestAnalysisCacheEstimateCountsSharedSnapshotPointersOnce(t *testing.T) {
	first := core.ParseDocument("file:///site/shared-first.asp", "<% Dim firstValue %>", core.Settings{DefaultLanguage: "VBScript"})
	second := core.ParseDocument("file:///site/shared-second.asp", "<% Dim secondValue %>", core.Settings{DefaultLanguage: "VBScript"})
	complete := &fileAnalysisSnapshot{
		runtimeBacked: true,
		SymbolFacts: map[string]symbolAnalysisFact{
			"shared": {Name: "shared", Occurrences: make([]symbolOccurrenceFact, 4)},
		},
	}
	reduced := &workspaceArtifactSnapshot{VirtualDocuments: map[core.EmbeddedLanguage]core.VirtualDocument{}}
	cache := newAnalysisCache()
	cache.rememberSnapshot(first, complete)
	cache.rememberSnapshot(second, complete)
	cache.rememberWorkspaceSnapshot(first, "first", reduced)
	cache.rememberWorkspaceSnapshot(second, "second", reduced)

	got, entries := cache.memoryEstimate()
	want := int64(2)*estimateAnalysisCacheDeclarationsBytes(nil) +
		estimateAnalysisCacheFileSnapshotBytes(first, complete) +
		estimateAnalysisCacheWorkspaceSnapshotBytes(first, reduced) + first.EstimateBytes() + second.EstimateBytes()
	if got != want || entries != 6 {
		t.Fatalf("shared snapshot estimate = %d bytes, %d entries; want %d bytes, 6 entries", got, entries, want)
	}
	if freed := cache.evict(0); freed != got {
		t.Fatalf("shared snapshot eviction freed %d bytes; want estimate %d", freed, got)
	}
}

func TestAnalysisCacheEvictionReleasesParsedOwnerWithFinalLayer(t *testing.T) {
	cache := newAnalysisCache()
	parsed := core.ParseDocument("file:///site/eviction.asp", strings.Repeat("<% Dim value %>\n", 32), core.Settings{DefaultLanguage: "VBScript"})
	declarations := []vbUsageDeclaration{{Name: "value", Kind: "variable"}}
	full := &fileAnalysisSnapshot{URI: parsed.URI, GraphDeclarations: declarations, runtimeBacked: true}
	reduced := &workspaceArtifactSnapshot{URI: parsed.URI, VirtualDocuments: map[core.EmbeddedLanguage]core.VirtualDocument{}}
	key := workspaceArtifactSnapshotCacheKey{parsed: parsed, includeResolutionFingerprint: "includes"}
	cache.declarations[parsed] = declarations
	cache.snapshots[parsed] = full
	cache.workspaceSnapshots[key] = reduced
	before, _ := cache.memoryEstimate()

	firstFreed := cache.evict(1)
	if firstFreed != estimateAnalysisCacheFileSnapshotBytes(parsed, full) {
		t.Fatalf("first eviction freed %d, want full snapshot only (%d)", firstFreed, estimateAnalysisCacheFileSnapshotBytes(parsed, full))
	}
	if cache.snapshots[parsed] != nil || cache.declarations[parsed] == nil || cache.workspaceSnapshots[key] == nil {
		t.Fatalf("first eviction did not preserve remaining parsed owners")
	}
	remaining, _ := cache.memoryEstimate()
	if before-firstFreed != remaining {
		t.Fatalf("remaining estimate = %d, want %d", remaining, before-firstFreed)
	}

	lastFreed := cache.evict(0)
	if firstFreed+lastFreed != before {
		t.Fatalf("total freed = %d, want initial estimate %d", firstFreed+lastFreed, before)
	}
	if bytes, entries := cache.memoryEstimate(); bytes != 0 || entries != 0 {
		t.Fatalf("evicted cache estimate = %d, entries=%d", bytes, entries)
	}
}

func TestAnalysisCacheEvictionKeepsRuntimeValueChargedUntilFinalRevision(t *testing.T) {
	first := core.ParseDocument("file:///site/shared-runtime.asp", "<% Dim value : value = value %>\n<div>a</div>", core.Settings{DefaultLanguage: "VBScript"})
	firstShard := vbscript.BuildReferenceShard(first)
	document := core.NewTextDocument(first.URI, "classic-asp", 1, first.Text)
	start := strings.Index(first.Text, ">a<") + 1
	changeRange := document.Range(start, start+1)
	result := core.UpdateParsedDocument(first, []core.IncrementalChange{{Range: &changeRange, Text: "b"}}, core.Settings{DefaultLanguage: "VBScript"})
	if !result.Incremental || result.Parsed == nil {
		t.Fatalf("test update was not incremental: %#v", result)
	}
	second := result.Parsed
	secondShard := vbscript.BuildReferenceShard(second)
	if secondShard != firstShard {
		t.Fatal("unchanged reference shard was not promoted")
	}
	owners := second.RuntimeAnalysisMemoryOwners()
	if len(owners) != 1 {
		t.Fatalf("successor runtime owners = %#v, want one", owners)
	}
	firstStructuralOwners := analysisOwnerBytesByIdentity(first.StructuralMemoryOwners())
	secondStructuralOwners := analysisOwnerBytesByIdentity(second.StructuralMemoryOwners())
	if len(firstStructuralOwners) == 0 || len(secondStructuralOwners) <= len(firstStructuralOwners) {
		t.Fatalf("incremental structural owners = first %#v second %#v, want successor to retain both revisions", firstStructuralOwners, secondStructuralOwners)
	}

	firstSnapshot := &fileAnalysisSnapshot{URI: first.URI, runtimeBacked: true}
	secondSnapshot := &workspaceArtifactSnapshot{URI: second.URI, VirtualDocuments: map[core.EmbeddedLanguage]core.VirtualDocument{}}
	cache := newAnalysisCache()
	cache.snapshots[first] = firstSnapshot
	cache.workspaceSnapshots[workspaceArtifactSnapshotCacheKey{parsed: second, includeResolutionFingerprint: "second"}] = secondSnapshot

	firstFreed := cache.evict(1)
	wantFirstFreed := estimateAnalysisCacheFileSnapshotBytes(first, firstSnapshot) + first.EstimateStructuralBytesWithoutRevisionText()
	// Runtime values only the predecessor owns, such as its cached source document, are released with it.
	for _, owner := range first.RuntimeAnalysisMemoryOwners() {
		if owner.Identity != owners[0].Identity {
			wantFirstFreed += owner.Bytes
		}
	}
	if firstFreed != wantFirstFreed {
		t.Fatalf("predecessor eviction freed %d bytes, want %d without shared structural or runtime payload", firstFreed, wantFirstFreed)
	}
	remaining, entries := cache.memoryEstimate()
	var secondStructuralBytes int64
	for _, bytes := range secondStructuralOwners {
		secondStructuralBytes += bytes
	}
	wantRemaining := estimateAnalysisCacheWorkspaceSnapshotBytes(second, secondSnapshot) + second.EstimateStructuralBytesWithoutRevisionText() + secondStructuralBytes + owners[0].Bytes
	if remaining != wantRemaining || entries != 1 {
		t.Fatalf("successor retention = %d bytes, %d entries; want %d bytes, 1 entry", remaining, entries, wantRemaining)
	}
	if freed := cache.evict(0); freed != wantRemaining {
		t.Fatalf("final successor eviction freed %d bytes, want %d", freed, wantRemaining)
	}
}

func TestAnalysisCacheEstimateDeduplicatesStructuralOwnerWithExternalRevision(t *testing.T) {
	first := core.ParseDocument("file:///site/external-structural.asp", "<% Dim value %>\n<div>a</div>", core.Settings{DefaultLanguage: "VBScript"})
	document := core.NewTextDocument(first.URI, "classic-asp", 1, first.Text)
	start := strings.Index(first.Text, ">a<") + 1
	changeRange := document.Range(start, start+1)
	result := core.UpdateParsedDocument(first, []core.IncrementalChange{{Range: &changeRange, Text: "b"}}, core.Settings{DefaultLanguage: "VBScript"})
	if !result.Incremental || result.Parsed == nil {
		t.Fatalf("text edit did not retain incremental predecessor: incremental=%t reason=%s", result.Incremental, result.Reason)
	}
	second := result.Parsed
	firstOwners := analysisOwnerBytesByIdentity(first.StructuralMemoryOwners())
	secondOwners := analysisOwnerBytesByIdentity(second.StructuralMemoryOwners())
	if len(firstOwners) == 0 || len(secondOwners) <= len(firstOwners) {
		t.Fatalf("incremental structural owners = first %#v second %#v, want successor to retain both revisions", firstOwners, secondOwners)
	}

	cache := newAnalysisCache()
	snapshot := &fileAnalysisSnapshot{URI: first.URI, runtimeBacked: true}
	cache.rememberSnapshot(first, snapshot)
	external := map[*core.ParsedDocument]struct{}{second: {}}
	got, entries := cache.memoryEstimateWithExternalParsed(external)
	want := estimateAnalysisCacheDeclarationsBytes(nil) + estimateAnalysisCacheFileSnapshotBytes(first, snapshot) + first.EstimateStructuralBytesWithoutRevisionText()
	if got != want || entries != 2 {
		t.Fatalf("external structural owner estimate = %d bytes, %d entries; want %d bytes, 2 entries", got, entries, want)
	}

	if freed := cache.evictWithExternalParsed(0, external); freed != want {
		t.Fatalf("external structural owner eviction freed %d bytes, want %d", freed, want)
	}
}

func TestAnalysisCacheRealCompleteAndReducedSnapshotsShareExternalPayloads(t *testing.T) {
	server := New(nil, io.Discard, io.Discard)
	parsed := core.ParseDocument("file:///site/shared-snapshots.asp", `<html>
<style>.card { color: #fff; }</style>
<script>const clientValue = 1;</script>
<%
Class Account
Public Value
Public Function GetValue(ByVal input)
GetValue = input
End Function
End Class
Dim value
value = value
Response.Write value
%>
</html>`, core.Settings{DefaultLanguage: "VBScript"})
	reduced := server.buildDocumentOpenWorkspaceArtifactSnapshotReducedContext(context.Background(), parsed)
	if reduced == nil {
		t.Fatal("reduced workspace snapshot was not built")
	}
	complete := server.buildFileAnalysisSnapshot(parsed)
	if complete == nil {
		t.Fatal("complete file snapshot was not built")
	}
	if complete.ReferenceShard == nil || complete.ReferenceShard != reduced.ReferenceShard {
		t.Fatal("complete and reduced snapshots do not share the parsed reference shard")
	}
	if len(complete.Usage.Declarations) == 0 || len(reduced.Usage.Declarations) == 0 ||
		&complete.Usage.Declarations[0] != &reduced.Usage.Declarations[0] {
		t.Fatal("complete and reduced snapshots do not share parsed usage backing")
	}
	if len(complete.Summary.VBScript.PublicSymbols) == 0 || len(reduced.Summary.VBScript.PublicSymbols) == 0 ||
		&complete.Summary.VBScript.PublicSymbols[0] != &reduced.Summary.VBScript.PublicSymbols[0] {
		t.Fatal("complete and reduced snapshots do not share parsed summary backing")
	}
	completeHTML := complete.VirtualDocuments[core.LanguageHTML]
	reducedHTML := reduced.VirtualDocuments[core.LanguageHTML]
	if len(completeHTML.Segments) == 0 || len(reducedHTML.Segments) == 0 ||
		&completeHTML.Segments[0] != &reducedHTML.Segments[0] {
		t.Fatal("complete and reduced snapshots do not share parsed virtual-document backing")
	}
	sharedVirtualBytes := estimateAnalysisCacheVirtualDocumentsBytes(parsed, complete.VirtualDocuments)
	if want := int64(64 + len(complete.VirtualDocuments)*128); sharedVirtualBytes != want {
		t.Fatalf("runtime-backed virtual documents estimate = %d bytes; want shallow map estimate %d", sharedVirtualBytes, want)
	}

	completeOnly := newAnalysisCache()
	completeOnly.rememberSnapshot(parsed, complete)
	completeBytes, _ := completeOnly.memoryEstimate()
	completeSnapshotBytes := estimateAnalysisCacheFileSnapshotBytes(parsed, complete)
	reducedOnly := newAnalysisCache()
	reducedOnly.rememberWorkspaceSnapshot(parsed, reduced.IncludeResolutionFingerprint, reduced)
	reducedBytes, _ := reducedOnly.memoryEstimate()
	server.rememberFileAnalysisSnapshot(parsed, complete)
	bothBytes, entries := server.analysisCache.memoryEstimate()
	if entries != 3 {
		t.Fatalf("real snapshot cache entries = %d, want 3", entries)
	}
	if bothBytes != completeBytes+reducedBytes-parsed.EstimateBytes() {
		t.Fatalf("shared complete/reduced payloads were double-counted: both=%d complete=%d reduced=%d", bothBytes, completeBytes, reducedBytes)
	}
	naive := parsed.EstimateBytes() + estimateAnalysisDeclarationsBytes(nil, complete.GraphDeclarations) +
		estimateFileAnalysisSnapshotBytes(complete) + estimateWorkspaceArtifactSnapshotBytes(reduced)
	if bothBytes >= naive {
		t.Fatalf("analysis cache estimate=%d still includes external/shared payloads from naive=%d", bothBytes, naive)
	}
	if bothBytes <= 0 || completeBytes <= 0 || reducedBytes <= 0 {
		t.Fatalf("cache-owned snapshot storage was ignored: both=%d complete=%d reduced=%d", bothBytes, completeBytes, reducedBytes)
	}

	freed := server.analysisCache.evict(1)
	if freed != completeSnapshotBytes {
		t.Fatalf("complete eviction freed %d, want cache-owned complete snapshot bytes %d", freed, completeSnapshotBytes)
	}
	remaining, remainingEntries := server.analysisCache.memoryEstimate()
	if remaining != bothBytes-freed || remainingEntries != 2 {
		t.Fatalf("after complete eviction estimate=%d entries=%d, want %d entries=2", remaining, remainingEntries, bothBytes-freed)
	}
	if server.analysisCache.workspaceSnapshot(parsed, reduced.IncludeResolutionFingerprint) != reduced {
		t.Fatal("complete eviction removed the reduced snapshot")
	}
	if totalFreed := freed + server.analysisCache.evict(0); totalFreed != bothBytes {
		t.Fatalf("total real snapshot eviction freed %d, want %d", totalFreed, bothBytes)
	}
}

func TestFileAnalysisSnapshotEstimateIncludesMaterializedReferenceMetadata(t *testing.T) {
	parsed := core.ParseDocument("file:///site/reference-metadata.asp", "<% Dim value : value = value : value() %>", core.Settings{DefaultLanguage: "VBScript"})
	shard := vbscript.BuildReferenceShard(parsed)
	snapshot := &fileAnalysisSnapshot{URI: parsed.URI, ReferenceShard: shard}
	before := estimateFileAnalysisSnapshotBytes(snapshot)
	shard.NormalizedNames()
	after := estimateFileAnalysisSnapshotBytes(snapshot)
	if after <= before {
		t.Fatalf("materialized reference metadata did not increase complete snapshot estimate: before=%d after=%d", before, after)
	}
}

func TestLiveFileAnalysisSnapshotEstimateIncludesSnapshotOnlyCollections(t *testing.T) {
	parsed := core.ParseDocument("file:///site/live-snapshot-owned.asp", "<% Dim value %>", core.Settings{DefaultLanguage: "VBScript"})
	base := estimateAnalysisCacheFileSnapshotBytes(parsed, &fileAnalysisSnapshot{runtimeBacked: true})
	tests := []struct {
		name  string
		apply func(*fileAnalysisSnapshot)
	}{
		{name: "reference facts", apply: func(snapshot *fileAnalysisSnapshot) {
			snapshot.ReferenceFacts = vbReferenceDocumentFacts{DeclarationRanges: map[string][]lsp.Range{"value": []lsp.Range{{}}}}
		}},
		{name: "member occurrences", apply: func(snapshot *fileAnalysisSnapshot) {
			snapshot.Members = []graphMemberOccurrence{{URI: parsed.URI, FullPath: "owner.value", ReceiverName: "owner", MemberName: "value", Parts: []string{"owner", "value"}}}
		}},
		{name: "naming declarations", apply: func(snapshot *fileAnalysisSnapshot) {
			snapshot.NamingDeclarations = []vbUsageDeclaration{{Name: "value", Kind: "variable"}}
		}},
		{name: "document symbols", apply: func(snapshot *fileAnalysisSnapshot) {
			snapshot.VBDocumentSymbols = []lsp.DocumentSymbol{{Name: "value"}}
		}},
		{name: "folding ranges", apply: func(snapshot *fileAnalysisSnapshot) {
			snapshot.VBFoldingRanges = []lsp.FoldingRange{{StartLine: 1, EndLine: 2}}
		}},
		{name: "document colors", apply: func(snapshot *fileAnalysisSnapshot) {
			snapshot.DocumentColors = []lsp.ColorInformation{{Color: lsp.Color{Red: 1}}}
		}},
		{name: "class and procedure lines", apply: func(snapshot *fileAnalysisSnapshot) {
			snapshot.VBClassLines = map[int]struct{}{1: {}}
			snapshot.VBProcedureLines = map[int]struct{}{2: {}}
		}},
	}
	for _, test := range tests {
		t.Run(test.name, func(t *testing.T) {
			snapshot := &fileAnalysisSnapshot{runtimeBacked: true}
			test.apply(snapshot)
			if got := estimateAnalysisCacheFileSnapshotBytes(parsed, snapshot); got <= base {
				t.Fatalf("live snapshot estimate = %d, want greater than empty baseline %d", got, base)
			}
		})
	}
}

func TestAnalysisCacheEstimateTracksLiveSummaryRuntimeGrowth(t *testing.T) {
	small := core.ParseDocument("file:///site/small-summary.asp", "<% Dim value %>", core.Settings{DefaultLanguage: "VBScript"})
	large := core.ParseDocument("file:///site/large-summary.asp", "<% Dim value %>", core.Settings{DefaultLanguage: "VBScript"})
	small.StoreRuntimeAnalysis("lspserver.vb-file-summary.v2", vbFileAnalysisSummary{
		VBScript: vbLocalSummary{PublicSymbols: []vbPublicSummarySymbol{{Name: "value", Kind: "variable"}}},
	})
	large.StoreRuntimeAnalysis("lspserver.vb-file-summary.v2", vbFileAnalysisSummary{
		VBScript: vbLocalSummary{PublicSymbols: make([]vbPublicSummarySymbol, 512)},
	})

	smallBytes := small.EstimateBytes()
	largeBytes := large.EstimateBytes()
	if largeBytes <= smallBytes+32*1024 {
		t.Fatalf("large live summary estimate = %d bytes; want scalable growth beyond small estimate %d", largeBytes, smallBytes)
	}

	cache := newAnalysisCache()
	snapshot := &fileAnalysisSnapshot{URI: large.URI, runtimeBacked: true}
	cache.rememberSnapshot(large, snapshot)
	before, entries := cache.memoryEstimate()
	if entries != 2 || before < largeBytes {
		t.Fatalf("large live cache estimate = %d bytes, %d entries; want parsed runtime owner of at least %d bytes", before, entries, largeBytes)
	}
	if freed := cache.evict(0); freed != before {
		t.Fatalf("large live cache eviction freed %d bytes; want estimate %d", freed, before)
	}
}

func TestAnalysisCacheRestoredSnapshotCountsDecodedPayloadsOnce(t *testing.T) {
	server := New(nil, io.Discard, io.Discard)
	parsed := core.ParseDocument("file:///site/restored-snapshot.asp", `<% Dim value : value = value %>`, core.Settings{DefaultLanguage: "VBScript"})
	live := server.buildFileAnalysisSnapshot(parsed)
	if live == nil || !live.runtimeBacked {
		t.Fatal("live file analysis snapshot did not record runtime ownership")
	}
	encoded, err := json.Marshal(live)
	if err != nil {
		t.Fatal(err)
	}
	var restored fileAnalysisSnapshot
	if err := json.Unmarshal(encoded, &restored); err != nil {
		t.Fatal(err)
	}
	if restored.runtimeBacked {
		t.Fatal("restored file analysis snapshot unexpectedly retained live ownership")
	}
	restoredParsed := core.ParseDocument(parsed.URI, parsed.Text, core.Settings{DefaultLanguage: "VBScript"})
	seedParsedAnalysis(restoredParsed, &restored)
	if restored.ReferenceShard == nil || restored.Symbols.Declarations == nil {
		t.Fatal("restored snapshot did not seed reference and symbol runtime values")
	}
	cache := newAnalysisCache()
	cache.rememberSnapshot(restoredParsed, &restored)
	got, entries := cache.memoryEstimate()
	wantDeclarations := estimateAnalysisDeclarationStorageBytes(nil, restored.GraphDeclarations)
	wantSnapshot := estimateRestoredFileAnalysisSnapshotBytes(&restored)
	want := wantDeclarations + wantSnapshot + restoredParsed.EstimateBytes()
	if got != want || entries != 2 {
		t.Fatalf("restored snapshot estimate = %d bytes, %d entries; want %d bytes, 2 entries", got, entries, want)
	}
	if freed := cache.evict(1); freed != wantSnapshot {
		t.Fatalf("restored snapshot eviction freed %d bytes; want snapshot-owned %d", freed, wantSnapshot)
	}
	wantRemaining := wantDeclarations + restoredParsed.EstimateBytes()
	if remaining, remainingEntries := cache.memoryEstimate(); remaining != wantRemaining || remainingEntries != 1 {
		t.Fatalf("restored declaration retention = %d bytes, %d entries; want %d bytes, 1 entry", remaining, remainingEntries, wantRemaining)
	}
	if freed := cache.evict(0); freed != wantRemaining {
		t.Fatalf("restored declarations eviction freed %d bytes; want %d", freed, wantRemaining)
	}
	withoutDecoded := restored
	withoutDecoded.ReferenceFacts = vbReferenceDocumentFacts{}
	withoutDecoded.Signatures = nil
	withoutDecoded.SignatureList = nil
	withoutDecoded.Usage = vbUsageDeclarations{}
	withoutDecoded.GraphDeclarations = nil
	withoutDecoded.Assignments = nil
	withoutDecoded.AnalysisTypes = vbGraphAnalysisTypes{}
	withoutDecoded.Members = nil
	withoutDecoded.Summary = vbFileAnalysisSummary{}
	withoutDecoded.SymbolFacts = nil
	withoutDecoded.Includes = nil
	withoutDecoded.VirtualDocuments = nil
	withoutDecoded.VBDocumentSymbols = nil
	withoutDecoded.VBFoldingRanges = nil
	withoutDecoded.DocumentColors = nil
	withoutDecoded.NamingDeclarations = nil
	withoutDecoded.VBClassLines = nil
	withoutDecoded.VBProcedureLines = nil
	if estimateRestoredFileAnalysisSnapshotBytes(&restored) <= estimateRestoredFileAnalysisSnapshotBytes(&withoutDecoded) {
		t.Fatal("decoded restored payloads did not contribute to snapshot estimate")
	}
}

func TestAnalysisCacheDuplicateSnapshotUsesDeterministicMaximumSize(t *testing.T) {
	first := core.ParseDocument("file:///site/duplicate-first.asp", "<% value %>", core.Settings{DefaultLanguage: "VBScript"})
	second := core.ParseDocument("file:///site/duplicate-second.asp", "<% value %>", core.Settings{DefaultLanguage: "VBScript"})
	virtual := core.BuildVirtualDocument(first, core.LanguageHTML)
	virtual.Text = strings.Repeat("synthetic", 512)
	virtual.Segments = make([]core.SourceMapSegment, 32)
	snapshot := &fileAnalysisSnapshot{
		URI:              first.URI,
		VirtualDocuments: map[core.EmbeddedLanguage]core.VirtualDocument{core.LanguageHTML: virtual},
		runtimeBacked:    true,
	}
	cache := newAnalysisCache()
	cache.rememberSnapshot(first, snapshot)
	cache.rememberSnapshot(second, snapshot)
	before, _ := cache.memoryEstimate()
	firstSize := estimateAnalysisCacheFileSnapshotBytes(first, snapshot)
	secondSize := estimateAnalysisCacheFileSnapshotBytes(second, snapshot)
	wantSnapshot := firstSize
	if secondSize > wantSnapshot {
		wantSnapshot = secondSize
	}
	wantRemaining := 2*estimateAnalysisCacheDeclarationsBytes(nil) + estimateAnalysisParsedBytes(first, second)
	if before != wantRemaining+wantSnapshot {
		t.Fatalf("duplicate snapshot estimate = %d; want declarations, parsed owners, and max snapshot %d", before, wantRemaining+wantSnapshot)
	}
	if freed := cache.evict(wantSnapshot / 2); freed != wantSnapshot {
		t.Fatalf("partial duplicate snapshot eviction freed %d; want deterministic max %d", freed, wantSnapshot)
	}
	if total := cache.evict(0); total != wantRemaining {
		t.Fatalf("remaining declaration eviction freed %d; want %d", total, wantRemaining)
	}
}

func TestAnalysisCacheDuplicateRestoredSnapshotCountsDeclarationsOnce(t *testing.T) {
	first := core.ParseDocument("file:///site/restored-first.asp", "<% value %>", core.Settings{DefaultLanguage: "VBScript"})
	second := core.ParseDocument("file:///site/restored-second.asp", "<% value %>", core.Settings{DefaultLanguage: "VBScript"})
	declarations := accountingTestDeclarations(32)
	snapshot := &fileAnalysisSnapshot{
		URI:               first.URI,
		GraphDeclarations: declarations,
		Usage:             vbUsageDeclarations{Declarations: accountingTestDeclarations(16)},
	}
	cache := newAnalysisCache()
	cache.rememberSnapshot(first, snapshot)
	cache.rememberSnapshot(second, snapshot)

	snapshotBytes := estimateRestoredFileAnalysisSnapshotBytes(snapshot)
	declarationBytes := estimateAnalysisDeclarationStorageBytes(nil, declarations) + estimateAnalysisCacheDeclarationsBytes(declarations)
	before, entries := cache.memoryEstimate()
	parsedBytes := estimateAnalysisParsedBytes(first, second)
	if want := snapshotBytes + declarationBytes + parsedBytes; before != want || entries != 4 {
		t.Fatalf("duplicate restored estimate = %d bytes, %d entries; want %d bytes, 4 entries", before, entries, want)
	}
	if freed := cache.evict(1); freed != snapshotBytes {
		t.Fatalf("duplicate restored snapshot eviction freed %d bytes; want %d", freed, snapshotBytes)
	}
	if remaining, remainingEntries := cache.memoryEstimate(); remaining != declarationBytes+parsedBytes || remainingEntries != 2 {
		t.Fatalf("duplicate restored declarations = %d bytes, %d entries; want %d bytes, 2 entries", remaining, remainingEntries, declarationBytes+parsedBytes)
	}
	if freed := cache.evict(0); freed != declarationBytes+parsedBytes {
		t.Fatalf("duplicate restored declarations eviction freed %d bytes; want %d", freed, declarationBytes+parsedBytes)
	}
}

func TestAnalysisCacheAccountingTransactionSerializesParsedOwnership(t *testing.T) {
	server := New(nil, io.Discard, io.Discard)
	parsed := core.ParseDocument("file:///site/accounting-transaction.asp", strings.Repeat("<% Dim value %>\n", 32), core.Settings{DefaultLanguage: "VBScript"})
	server.analysisCache.rememberSnapshot(parsed, &fileAnalysisSnapshot{URI: parsed.URI, runtimeBacked: true})
	key := parsedDocumentCacheKey(parsed.URI)
	server.mu.Lock()
	server.parsedCache[key] = parsedDocumentCacheEntry{Text: parsed.Text, DefaultLanguage: "VBScript", Parsed: parsed}
	server.mu.Unlock()

	ownershipSampled := make(chan bool, 1)
	releaseAccounting := make(chan struct{})
	accountingDone := make(chan int64, 1)
	go func() {
		bytes, _ := server.withParsedCacheOwnership(func(external map[*core.ParsedDocument]struct{}) (int64, int) {
			_, retained := external[parsed]
			ownershipSampled <- retained
			<-releaseAccounting
			estimated, _ := server.analysisCache.memoryEstimateWithExternalParsed(external)
			return estimated, 0
		})
		accountingDone <- bytes
	}()

	select {
	case retained := <-ownershipSampled:
		if !retained {
			t.Fatal("transaction did not sample the parsed cache owner")
		}
	case <-time.After(time.Second):
		t.Fatal("analysis accounting transaction did not start")
	}

	mutationDone := make(chan struct{})
	go func() {
		server.mu.Lock()
		delete(server.parsedCache, key)
		server.mu.Unlock()
		close(mutationDone)
	}()
	select {
	case <-mutationDone:
		t.Fatal("parsed-cache ownership changed while accounting transaction was active")
	case <-time.After(50 * time.Millisecond):
	}

	close(releaseAccounting)
	var got int64
	select {
	case got = <-accountingDone:
	case <-time.After(time.Second):
		t.Fatal("analysis accounting transaction did not finish")
	}
	select {
	case <-mutationDone:
	case <-time.After(time.Second):
		t.Fatal("parsed-cache ownership mutation did not finish")
	}

	want, _ := server.analysisCache.memoryEstimateWithExternalParsed(map[*core.ParsedDocument]struct{}{parsed: {}})
	if got != want {
		t.Fatalf("transaction estimate = %d, want owner-sampled estimate %d", got, want)
	}
}

func analysisOwnerBytesByIdentity(owners []core.RuntimeAnalysisMemoryOwner) map[any]int64 {
	result := make(map[any]int64, len(owners))
	for _, owner := range owners {
		result[owner.Identity] = owner.Bytes
	}
	return result
}

func estimateAnalysisParsedBytes(parsedDocuments ...*core.ParsedDocument) int64 {
	structuralOwners := map[any]struct{}{}
	runtimeOwners := map[any]int64{}
	var bytes int64
	for _, parsed := range parsedDocuments {
		if parsed == nil {
			continue
		}
		bytes += parsed.EstimateStructuralBytesWithoutRevisionText()
		for _, owner := range parsed.StructuralMemoryOwners() {
			if _, exists := structuralOwners[owner.Identity]; exists {
				continue
			}
			structuralOwners[owner.Identity] = struct{}{}
			bytes += owner.Bytes
		}
		for _, owner := range parsed.RuntimeAnalysisMemoryOwners() {
			if current := runtimeOwners[owner.Identity]; owner.Bytes > current {
				runtimeOwners[owner.Identity] = owner.Bytes
			}
		}
	}
	for _, ownerBytes := range runtimeOwners {
		bytes += ownerBytes
	}
	return bytes
}
