package lspserver

import (
	"context"
	"reflect"
	"strconv"
	"strings"
	"testing"

	"github.com/yottonoko/classic-web-system-lsp/internal/core"
	"github.com/yottonoko/classic-web-system-lsp/internal/vbscript"
)

func TestWorkspaceArtifactSnapshotCacheHitStaysSeparateFromCompleteAnalysis(t *testing.T) {
	server := New(strings.NewReader(""), nil, nil)
	t.Cleanup(server.shutdownRuntimeCaches)
	parsed := core.ParseDocument("file:///site/reduced-cache.asp", `<%
Dim sharedValue
sharedValue = 1
%>
<style>.card { color: red; }</style>`, core.Settings{DefaultLanguage: "VBScript"})
	complete := &fileAnalysisSnapshot{
		URI:        parsed.URI,
		Signatures: map[string]vbscript.Signature{"complete": {}},
	}
	server.rememberFileAnalysisSnapshot(parsed, complete)
	var builds int
	server.fileAnalysisSnapshotTestHook = func() { builds++ }

	first := server.buildDocumentOpenWorkspaceArtifactSnapshot(parsed)
	second := server.buildDocumentOpenWorkspaceArtifactSnapshot(parsed)
	if first == nil || second == nil {
		t.Fatal("reduced workspace artifact snapshot was not built")
	}
	if builds != 1 {
		t.Fatalf("reduced workspace artifact builds = %d, want one build and one cache hit", builds)
	}
	if got := server.cachedFileAnalysisSnapshot(parsed); got != complete {
		t.Fatalf("complete analysis cache was replaced: got=%p want=%p", got, complete)
	}
	includeFingerprint := server.includeResolutionFingerprint(parsed)
	reduced := server.cachedWorkspaceArtifactSnapshot(parsed, includeFingerprint)
	if reduced == nil {
		t.Fatal("reduced workspace artifact snapshot was not retained")
	}
	if first.Signatures != nil || second.Signatures != nil {
		t.Fatal("document-open reduced snapshot exposed complete analysis fields")
	}
	if reduced.ReferenceShard != first.ReferenceShard || reduced.ReferenceShard != second.ReferenceShard {
		t.Fatal("cache hit did not reuse reduced reference facts")
	}

	if got := server.analysisCache.workspaceSnapshot(parsed, includeFingerprint+"-changed"); got != nil {
		t.Fatal("include-resolution fingerprint mismatch reused a reduced snapshot")
	}
	server.deleteAnalysisCacheForURI(parsed.URI)
	if got := server.cachedWorkspaceArtifactSnapshot(parsed, includeFingerprint); got != nil {
		t.Fatal("URI invalidation retained a reduced workspace artifact snapshot")
	}
	if got := server.cachedFileAnalysisSnapshot(parsed); got != nil {
		t.Fatal("URI invalidation retained a complete analysis snapshot")
	}
}

func TestWorkspaceArtifactSnapshotCacheInvalidatesChangedLanguageAndPreservesVBFacts(t *testing.T) {
	server := New(strings.NewReader(""), nil, nil)
	t.Cleanup(server.shutdownRuntimeCaches)
	const uri = "file:///site/reduced-cache-incremental.asp"
	source := `<%
Dim sharedValue
sharedValue = 1
%>
<style>.card { color: red; }</style>`
	previous := core.ParseDocument(uri, source, core.Settings{DefaultLanguage: "VBScript"})
	previousSnapshot := server.buildDocumentOpenWorkspaceArtifactSnapshotReducedContext(context.Background(), previous)
	if previousSnapshot == nil {
		t.Fatal("initial reduced workspace artifact snapshot was nil")
	}

	document := core.NewTextDocument(uri, "classic-asp", 1, source)
	start := strings.LastIndex(source, "red")
	changeRange := document.Range(start, start+len("red"))
	updated := core.UpdateParsedDocument(previous, []core.IncrementalChange{{Range: &changeRange, Text: "blue"}}, core.Settings{DefaultLanguage: "VBScript"})
	if !updated.Incremental || !updated.Impact.Affects(core.LanguageCSS) {
		t.Fatalf("CSS update was not classified as a CSS-only incremental edit: %#v", updated)
	}
	currentSnapshot := server.buildDocumentOpenWorkspaceArtifactSnapshotReducedContext(context.Background(), updated.Parsed)
	if currentSnapshot == nil || currentSnapshot == previousSnapshot {
		t.Fatal("changed source revision reused the previous reduced snapshot object")
	}
	if currentSnapshot.VirtualDocuments[core.LanguageCSS].Text == previousSnapshot.VirtualDocuments[core.LanguageCSS].Text {
		t.Fatal("changed CSS virtual document was reused unchanged")
	}
	if !referenceShardsSemanticallyEqual(currentSnapshot.ReferenceShard, previousSnapshot.ReferenceShard) ||
		!reflect.DeepEqual(currentSnapshot.Usage, previousSnapshot.Usage) ||
		!reflect.DeepEqual(currentSnapshot.Summary.VBScript, previousSnapshot.Summary.VBScript) {
		t.Fatal("unaffected VB facts were not preserved across a CSS edit")
	}
	if reflect.DeepEqual(currentSnapshot.Summary, previousSnapshot.Summary) {
		t.Fatal("summary source fingerprint did not follow the new revision")
	}

	fresh := core.ParseDocument(uri, updated.Parsed.Text, core.Settings{DefaultLanguage: "VBScript"})
	freshSnapshot := buildWorkspaceArtifactSnapshotReducedContext(context.Background(), fresh)
	if !referenceShardsSemanticallyEqual(currentSnapshot.ReferenceShard, freshSnapshot.ReferenceShard) ||
		!reflect.DeepEqual(currentSnapshot.Usage, freshSnapshot.Usage) ||
		!reflect.DeepEqual(currentSnapshot.Summary, freshSnapshot.Summary) ||
		!reflect.DeepEqual(currentSnapshot.VirtualDocuments, freshSnapshot.VirtualDocuments) {
		t.Fatal("incremental reduced workspace artifacts differ from a fresh build")
	}

	vbStart := strings.Index(updated.Parsed.Text, "sharedValue")
	vbRange := core.NewTextDocument(uri, "classic-asp", 2, updated.Parsed.Text).Range(vbStart, vbStart+len("sharedValue"))
	vbUpdate := core.UpdateParsedDocument(updated.Parsed, []core.IncrementalChange{{Range: &vbRange, Text: "updatedValue"}}, core.Settings{DefaultLanguage: "VBScript"})
	if !vbUpdate.Incremental || !vbUpdate.Impact.Affects(core.LanguageVBScript) {
		t.Fatalf("VBScript update was not classified as a VBScript edit: %#v", vbUpdate)
	}
	vbSnapshot := server.buildDocumentOpenWorkspaceArtifactSnapshotReducedContext(context.Background(), vbUpdate.Parsed)
	if vbSnapshot == nil || vbSnapshot == currentSnapshot {
		t.Fatal("changed VBScript revision reused the previous reduced snapshot object")
	}
	if referenceShardsSemanticallyEqual(vbSnapshot.ReferenceShard, currentSnapshot.ReferenceShard) {
		t.Fatal("changed VBScript reference facts were not invalidated")
	}
}

func referenceShardsSemanticallyEqual(left, right *vbscript.ReferenceShard) bool {
	if left == nil || right == nil {
		return left == right
	}
	return left.Version == right.Version &&
		reflect.DeepEqual(left.Declarations, right.Declarations) &&
		reflect.DeepEqual(left.Postings, right.Postings) &&
		reflect.DeepEqual(left.Scopes, right.Scopes)
}

var workspaceArtifactSnapshotBenchmarkSink *workspaceArtifactSnapshot

func BenchmarkWorkspaceArtifactSnapshotCache(b *testing.B) {
	const source = `<%
Dim sharedValue
sharedValue = 1
%>
<style>.card { color: red; }</style>
<script>const value = 1;</script>`
	server := New(strings.NewReader(""), nil, nil)
	b.Cleanup(server.shutdownRuntimeCaches)
	b.ReportAllocs()
	b.Run("cold", func(b *testing.B) {
		b.ResetTimer()
		for index := 0; index < b.N; index++ {
			parsed := core.ParseDocument("file:///site/benchmark-"+strconv.Itoa(index)+".asp", source, core.Settings{DefaultLanguage: "VBScript"})
			workspaceArtifactSnapshotBenchmarkSink = buildWorkspaceArtifactSnapshotReducedContext(context.Background(), parsed)
		}
	})
	b.Run("cache-hit", func(b *testing.B) {
		parsed := core.ParseDocument("file:///site/benchmark-hit.asp", source, core.Settings{DefaultLanguage: "VBScript"})
		if workspaceArtifactSnapshotBenchmarkSink = server.buildDocumentOpenWorkspaceArtifactSnapshotReducedContext(context.Background(), parsed); workspaceArtifactSnapshotBenchmarkSink == nil {
			b.Fatal("initial reduced snapshot was nil")
		}
		includeFingerprint := server.includeResolutionFingerprint(parsed)
		b.ResetTimer()
		for index := 0; index < b.N; index++ {
			workspaceArtifactSnapshotBenchmarkSink = server.cachedWorkspaceArtifactSnapshot(parsed, includeFingerprint)
		}
	})
}

func TestIncrementalNonVBEditRemapsCachedVBWorkspaceArtifacts(t *testing.T) {
	const uri = "file:///site/remap-vb-artifacts.asp"
	source := `<style>.card { color: red; }</style>
<div class="card">content</div>
<%
Public Function RenderCard(value)
    RenderCard = value
End Function
Response.Write RenderCard("ok")
%>`
	previous := core.ParseDocument(uri, source, core.Settings{DefaultLanguage: "VBScript"})
	previousShard := vbscript.BuildReferenceShard(previous)
	_ = collectVBUsageDeclarations(previous)
	_ = summarizeVBScriptFileAnalysis(previous)

	document := core.NewTextDocument(uri, "classic-asp", 1, source)
	start := strings.Index(source, "red")
	changeRange := document.Range(start, start+len("red"))
	updated := core.UpdateParsedDocument(previous, []core.IncrementalChange{{Range: &changeRange, Text: "blue\nbackground: white"}}, core.Settings{DefaultLanguage: "VBScript"})
	if !updated.Incremental {
		t.Fatalf("CSS update fell back: %s", updated.Reason)
	}
	incrementalShard := vbscript.BuildReferenceShard(updated.Parsed)
	incrementalUsage := collectVBUsageDeclarations(updated.Parsed)
	incrementalSummary := summarizeVBScriptFileAnalysis(updated.Parsed)

	fresh := core.ParseDocument(uri, updated.Parsed.Text, core.Settings{DefaultLanguage: "VBScript"})
	freshShard := vbscript.BuildReferenceShard(fresh)
	freshUsage := collectVBUsageDeclarations(fresh)
	freshSummary := summarizeVBScriptFileAnalysis(fresh)

	if incrementalShard == previousShard {
		t.Fatal("incremental reference shard reused stale range storage")
	}
	if !reflect.DeepEqual(incrementalShard.Declarations, freshShard.Declarations) ||
		!reflect.DeepEqual(incrementalShard.Postings, freshShard.Postings) ||
		!reflect.DeepEqual(incrementalShard.Scopes, freshShard.Scopes) {
		t.Fatalf("incremental reference shard differs from fresh build:\nincremental=%#v\nfresh=%#v", incrementalShard, freshShard)
	}
	if !reflect.DeepEqual(incrementalUsage, freshUsage) {
		t.Fatalf("incremental VB usage differs from fresh build:\nincremental=%#v\nfresh=%#v", incrementalUsage, freshUsage)
	}
	if !reflect.DeepEqual(incrementalSummary, freshSummary) {
		t.Fatalf("incremental VB summary differs from fresh build:\nincremental=%#v\nfresh=%#v", incrementalSummary, freshSummary)
	}
}
