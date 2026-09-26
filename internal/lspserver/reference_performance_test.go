package lspserver

import (
	"strconv"
	"strings"
	"testing"

	"github.com/yottonoko/classic-web-system-lsp/internal/core"
	"github.com/yottonoko/classic-web-system-lsp/internal/lsp"
	"github.com/yottonoko/classic-web-system-lsp/internal/vbscript"
)

func TestVBReferenceDocumentIndexPersistsDeclarationAndObjectInitializationRanges(t *testing.T) {
	const uri = "file:///reference-index.asp"
	const source = `<%
Dim SharedValue
Set SharedValue = New LegacyClass
Response.Write SharedValue
%>`
	parsed := core.ParseDocument(uri, source, core.Settings{DefaultLanguage: "VBScript"})
	index := vbReferenceDocumentIndexFor(parsed)
	declaration := lsp.Range{Start: lsp.Position{Line: 1, Character: 4}, End: lsp.Position{Line: 1, Character: 15}}
	initialization := lsp.Range{Start: lsp.Position{Line: 2, Character: 4}, End: lsp.Position{Line: 2, Character: 15}}
	if _, ok := index.declarationRanges["sharedvalue"][declaration]; !ok {
		t.Fatalf("declaration range missing: %#v", index.declarationRanges)
	}
	if _, ok := index.objectInitializationRanges["sharedvalue"][initialization]; !ok {
		t.Fatalf("object initialization range missing: %#v", index.objectInitializationRanges)
	}
	if len(parsed.Analysis[vbReferenceDocumentFactsAnalysisKey]) == 0 {
		t.Fatal("reference document facts were not persisted")
	}
	if len(parsed.Analysis["lspserver.vb-assignments.v1"]) != 0 {
		t.Fatal("reference facts rebuilt the assignment analysis instead of using the reference shard")
	}

	restored := core.ParseDocument(uri, source, core.Settings{DefaultLanguage: "VBScript"})
	restored.Analysis = parsed.AnalysisSnapshot()
	restoredIndex := vbReferenceDocumentIndexFor(restored)
	if _, ok := restoredIndex.declarationRanges["sharedvalue"][declaration]; !ok {
		t.Fatalf("restored declaration range missing: %#v", restoredIndex.declarationRanges)
	}
	if _, ok := restoredIndex.objectInitializationRanges["sharedvalue"][initialization]; !ok {
		t.Fatalf("restored object initialization range missing: %#v", restoredIndex.objectInitializationRanges)
	}
}

func TestVBReferenceDeclarationFactsPreserveNamingAndServerObjectOverlays(t *testing.T) {
	const source = `<object id="Repository" runat="server" progid="Example.Repository"></object>
<%
Function Build(ByVal value)
  Build = value
End Function
%>`
	parsed := core.ParseDocument("file:///reference-overlays.asp", source, core.Settings{DefaultLanguage: "VBScript"})
	facts := buildVBReferenceDocumentFacts(parsed)
	for _, name := range []string{"build", "value", "repository"} {
		if len(facts.DeclarationRanges[name]) != 1 {
			t.Fatalf("declaration ranges for %s = %#v", name, facts.DeclarationRanges[name])
		}
	}
}

func BenchmarkVBReferenceLocationsLargeDocument(b *testing.B) {
	var source strings.Builder
	source.WriteString("<%\nDim SharedValue\nSharedValue = 1\n")
	for index := 0; index < 5000; index++ {
		source.WriteString("Response.Write SharedValue ' ")
		source.WriteString(strconv.Itoa(index))
		source.WriteByte('\n')
	}
	source.WriteString("%>")
	parsed := core.ParseDocument("file:///benchmark.asp", source.String(), core.Settings{DefaultLanguage: "VBScript"})
	index := vbscript.BuildSymbolIndex(parsed)

	b.ReportAllocs()
	b.ResetTimer()
	for b.Loop() {
		locations := vbReferenceLocationsInDocumentWithIndex(parsed, index, "SharedValue", "variable", false)
		if len(locations) != 5001 {
			b.Fatalf("reference locations = %d, want 5001", len(locations))
		}
	}
}
