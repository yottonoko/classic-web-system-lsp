package lspserver

import (
	"bytes"
	"encoding/json"
	"io"
	"os"
	"path/filepath"
	"strings"
	"testing"

	"github.com/yottonoko/classic-web-system-lsp/internal/core"
	"github.com/yottonoko/classic-web-system-lsp/internal/lsp"
)

func TestDiskCacheRestartRestoresCompleteAnalysisBundleWithoutParsing(t *testing.T) {
	root := t.TempDir()
	cacheDir := filepath.Join(root, "cache")
	fileName := filepath.Join(root, "default.asp")
	includeName := filepath.Join(root, "shared.inc")
	includeText := `<%
Const IncludedLimit = 3
Function IncludedValue()
  IncludedValue = IncludedLimit
End Function
%>`
	text := `<!-- #include file="shared.inc" -->
<%
Dim first, second
Const GlobalLimit = 10
first = GlobalLimit
second = IncludedValue()

Sub RenderValue(value)
  Response.Write value
End Sub

Function SumValues(left, right)
  SumValues = left + right
End Function

Call RenderValue(first)
Response.Write second
Response.Write SumValues(first, second)
%>`
	if err := os.WriteFile(fileName, []byte(text), 0o644); err != nil {
		t.Fatal(err)
	}
	if err := os.WriteFile(includeName, []byte(includeText), 0o644); err != nil {
		t.Fatal(err)
	}
	doc := core.NewTextDocument(filePathURI(fileName), "classic-asp", 1, text)
	newServer := func(output io.Writer) *Server {
		server := New(strings.NewReader(""), output, io.Discard)
		server.rootPath = root
		server.rootURI = filePathURI(root)
		server.workspaceRoots = []workspaceRoot{{Path: root, URI: filePathURI(root)}}
		server.settings.CacheEnabled = true
		server.settings.CacheDirectory = cacheDir
		server.settings.CacheTTLHours = 24
		server.settings.CacheMaxSizeMB = 16
		server.settings.DebugOutput = "summary"
		server.configureDiskAnalysisCache()
		server.configureFsGateway()
		server.workspace[doc.URI] = doc
		return server
	}

	first := newServer(io.Discard)
	parsed := first.parseTextDocument(doc, "VBScript")
	parsed.Errors = append(parsed.Errors, core.ParseError{Start: 0, End: 1, Message: "persisted parser sentinel"})
	first.writeDiskParsedDocument(doc, "VBScript", parsed, first.cachedFileAnalysisSnapshot(parsed))
	firstDiagnostics := first.diagnostics(doc.URI)
	firstDefinitionFirst := first.definition(doc.URI, cacheBundlePosition(text, strings.LastIndex(text, "first, second")))
	firstDefinitionSecond := first.definition(doc.URI, cacheBundlePosition(text, strings.LastIndex(text, "second)")))
	firstReferencesFirst := first.references(doc.URI, cacheBundlePosition(text, strings.LastIndex(text, "first, second")), true)
	firstReferencesSecond := first.references(doc.URI, cacheBundlePosition(text, strings.LastIndex(text, "second)")), true)
	first.waitForAsyncDiskCacheWrites()
	entry, ok := first.diskCacheForUse().ReadFileBundle(first.parsedDiskLookup(doc, "VBScript"))
	if !ok {
		t.Fatal("complete file analysis snapshot was not persisted")
	}
	var snapshot fileAnalysisSnapshot
	if err := json.Unmarshal(entry.AnalysisSnapshot, &snapshot); err != nil {
		t.Fatal(err)
	}
	if snapshot.SchemaVersion != fileAnalysisSnapshotSchemaVersion || len(snapshot.Assignments) < 3 || len(snapshot.Signatures) != 2 || len(snapshot.VirtualDocuments) == 0 || len(snapshot.VBDocumentSymbols) == 0 {
		t.Fatalf("persisted snapshot is incomplete: %#v", snapshot)
	}
	firstFact := cacheBundleSnapshotSymbolFact(t, snapshot, "first", "")
	secondFact := cacheBundleSnapshotSymbolFact(t, snapshot, "second", "")
	if firstFact.Kind == "" || firstFact.Declaration == (lsp.Range{}) || secondFact.Kind == "" || secondFact.Declaration == (lsp.Range{}) {
		t.Fatalf("multiple Dim declaration facts are incomplete: first=%#v second=%#v", firstFact, secondFact)
	}
	if firstFact.WriteCount != 1 || firstFact.ReferenceCount < 3 || secondFact.WriteCount != 1 || secondFact.ReferenceCount < 3 {
		t.Fatalf("multiple Dim usage facts are incomplete: first=%#v second=%#v", firstFact, secondFact)
	}
	if fact := cacheBundleSnapshotSymbolFact(t, snapshot, "globallimit", ""); fact.ReadCount != 1 {
		t.Fatalf("constant usage count = %#v, want one read", fact)
	}
	if fact := cacheBundleSnapshotSymbolFact(t, snapshot, "rendervalue", ""); fact.CallCount != 1 {
		t.Fatalf("Sub call fact = %#v, want one call", fact)
	}
	if fact := cacheBundleSnapshotSymbolFact(t, snapshot, "sumvalues", ""); fact.CallCount != 1 || fact.WriteCount != 1 {
		t.Fatalf("Function fact = %#v, want one call and one assignment", fact)
	}
	if len(snapshot.Includes) != 1 || !snapshot.Includes[0].Exists || filepath.Clean(snapshot.Includes[0].ResolvedPath) != filepath.Clean(includeName) {
		t.Fatalf("resolved include snapshot = %#v, want %s", snapshot.Includes, includeName)
	}
	first.closeDiskAnalysisCache()

	var secondOutput bytes.Buffer
	second := newServer(&secondOutput)
	defer second.closeDiskAnalysisCache()
	restored := second.parseTextDocument(doc, "VBScript")
	secondDiagnostics := second.diagnostics(doc.URI)
	secondDefinitionFirst := second.definition(doc.URI, cacheBundlePosition(text, strings.LastIndex(text, "first, second")))
	secondDefinitionSecond := second.definition(doc.URI, cacheBundlePosition(text, strings.LastIndex(text, "second)")))
	secondReferencesFirst := second.references(doc.URI, cacheBundlePosition(text, strings.LastIndex(text, "first, second")), true)
	secondReferencesSecond := second.references(doc.URI, cacheBundlePosition(text, strings.LastIndex(text, "second)")), true)
	second.waitForAsyncDiskCacheWrites()
	if len(restored.Analysis) == 0 || second.cachedFileAnalysisSnapshot(restored) == nil {
		t.Fatal("persisted analysis snapshot was not restored")
	}
	if len(restored.Errors) == 0 || restored.Errors[len(restored.Errors)-1].Message != "persisted parser sentinel" {
		t.Fatalf("restart invoked the parser instead of restoring the persisted parse: %#v", restored.Errors)
	}
	if !strings.Contains(secondOutput.String(), "database.fileBundle.hit") || !strings.Contains(secondOutput.String(), `components=[\"parsed\",\"summary\",\"fileAnalysis\",\"includeRefs\"]`) || !strings.Contains(secondOutput.String(), "database.diagnostics.hit") {
		t.Fatalf("restart did not use all persisted bundle components: %s", secondOutput.String())
	}
	if !cacheBundleEqualJSON(firstDiagnostics, secondDiagnostics) || !cacheBundleEqualJSON(firstDefinitionFirst, secondDefinitionFirst) || !cacheBundleEqualJSON(firstDefinitionSecond, secondDefinitionSecond) ||
		!cacheBundleEqualJSON(firstReferencesFirst, secondReferencesFirst) || !cacheBundleEqualJSON(firstReferencesSecond, secondReferencesSecond) {
		t.Fatalf("restart parity mismatch:\ndiagnostics=%#v/%#v\nfirst definition=%#v/%#v\nsecond definition=%#v/%#v\nfirst references=%#v/%#v\nsecond references=%#v/%#v",
			firstDiagnostics, secondDiagnostics, firstDefinitionFirst, secondDefinitionFirst, firstDefinitionSecond, secondDefinitionSecond,
			firstReferencesFirst, secondReferencesFirst, firstReferencesSecond, secondReferencesSecond)
	}
	if len(secondDefinitionFirst) != 1 || len(secondDefinitionSecond) != 1 || secondDefinitionFirst[0].Range.Start == secondDefinitionSecond[0].Range.Start {
		t.Fatalf("both Dim variables did not navigate independently: first=%#v second=%#v", secondDefinitionFirst, secondDefinitionSecond)
	}
}

func cacheBundlePosition(text string, offset int) lsp.Position {
	doc := core.NewTextDocument("", "classic-asp", 0, text)
	return doc.PositionAt(offset)
}

func cacheBundleSnapshotSymbolFact(t *testing.T, snapshot fileAnalysisSnapshot, name, scope string) symbolAnalysisFact {
	t.Helper()
	var matches []symbolAnalysisFact
	for _, fact := range snapshot.SymbolFacts {
		if fact.NormalizedName == name && strings.EqualFold(fact.Scope, scope) {
			matches = append(matches, fact)
		}
	}
	if len(matches) != 1 {
		t.Fatalf("symbol facts for %s scope %s = %#v, want exactly one", name, scope, matches)
	}
	return matches[0]
}

func cacheBundleEqualJSON(left, right any) bool {
	leftJSON, leftErr := json.Marshal(left)
	rightJSON, rightErr := json.Marshal(right)
	return leftErr == nil && rightErr == nil && bytes.Equal(leftJSON, rightJSON)
}

func BenchmarkRuntimeDiskCacheColdStartup(b *testing.B) {
	root := b.TempDir()
	cacheDir := filepath.Join(root, "cold-cache")
	fileName := filepath.Join(root, "cold.asp")
	text := `<% Dim first, second : first = 1 : second = first %>`
	if err := os.WriteFile(fileName, []byte(text), 0o644); err != nil {
		b.Fatal(err)
	}
	doc := core.NewTextDocument(filePathURI(fileName), "classic-asp", 1, text)
	b.ReportAllocs()
	b.ResetTimer()
	for b.Loop() {
		server := benchmarkRuntimeCacheServer(root, cacheDir, doc)
		if parsed := server.parseTextDocument(doc, "VBScript"); parsed == nil {
			b.Fatal("cold startup parse returned nil")
		}
		server.waitForAsyncDiskCacheWrites()
		server.closeDiskAnalysisCache()
		if err := os.RemoveAll(cacheDir); err != nil {
			b.Fatal(err)
		}
	}
}

func BenchmarkRuntimeDiskCacheWarmStartup(b *testing.B) {
	root := b.TempDir()
	cacheDir := filepath.Join(root, "warm-cache")
	fileName := filepath.Join(root, "warm.asp")
	text := `<% Dim first, second : first = 1 : second = first %>`
	if err := os.WriteFile(fileName, []byte(text), 0o644); err != nil {
		b.Fatal(err)
	}
	doc := core.NewTextDocument(filePathURI(fileName), "classic-asp", 1, text)
	initial := benchmarkRuntimeCacheServer(root, cacheDir, doc)
	initial.parseTextDocument(doc, "VBScript")
	initial.waitForAsyncDiskCacheWrites()
	initial.closeDiskAnalysisCache()
	b.ReportAllocs()
	b.ResetTimer()
	for b.Loop() {
		server := benchmarkRuntimeCacheServer(root, cacheDir, doc)
		if parsed := server.parseTextDocument(doc, "VBScript"); parsed == nil {
			b.Fatal("warm startup restore returned nil")
		}
		server.closeDiskAnalysisCache()
	}
}

func benchmarkRuntimeCacheServer(root, cacheDir string, doc *core.TextDocument) *Server {
	server := New(strings.NewReader(""), io.Discard, io.Discard)
	server.rootPath = root
	server.rootURI = filePathURI(root)
	server.workspaceRoots = []workspaceRoot{{Path: root, URI: filePathURI(root)}}
	server.settings.CacheEnabled = true
	server.settings.CacheDirectory = cacheDir
	server.settings.CacheTTLHours = 24
	server.settings.CacheMaxSizeMB = 16
	server.configureDiskAnalysisCache()
	server.configureFsGateway()
	server.workspace[doc.URI] = doc
	return server
}
