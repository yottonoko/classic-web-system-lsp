package lspserver

import (
	"context"
	"fmt"
	"io"
	"strings"
	"sync/atomic"
	"testing"
	"time"

	"github.com/yottonoko/classic-web-system-lsp/internal/core"
	"github.com/yottonoko/classic-web-system-lsp/internal/lsp"
	workspacepkg "github.com/yottonoko/classic-web-system-lsp/internal/workspace"
)

func TestWorkspaceReferencesUseCurrentDocumentWhileWorkspaceIndexIsCold(t *testing.T) {
	const source = `<%
Function RootFunc(ByVal alpha)
  RootFunc = alpha
End Function
Response.Write RootFunc("x")
%>`
	server := New(nil, io.Discard, io.Discard)
	parsed := server.parseText("file:///private/tmp/reference-root/ref.asp", source, "VBScript")
	document := core.NewTextDocument(parsed.URI, "classic-asp", 1, source)
	position := document.PositionAt(strings.LastIndex(source, `RootFunc("x")`))

	server.mu.Lock()
	server.workspaceIndexEnabled = true
	server.workspaceIndexGeneration = 1
	server.workspaceReferenceIndexReadyGeneration = 0
	server.workspaceIndexDone = make(chan struct{})
	server.mu.Unlock()

	ctx, cancel := context.WithTimeout(t.Context(), 2*time.Second)
	defer cancel()
	started := time.Now()
	locations := server.workspaceVBScriptReferencesWithTestDelay(ctx, parsed, position, true, "reference:function", false)
	if elapsed := time.Since(started); elapsed > 750*time.Millisecond {
		t.Fatalf("cold-index current-document references took %v, want under 750ms", elapsed)
	}
	if len(locations) != 3 {
		t.Fatalf("cold-index current-document references = %#v, want three RootFunc locations", locations)
	}
	if ctx.Err() != nil {
		t.Fatalf("cold-index references exhausted request context: %v", ctx.Err())
	}
	server.mu.Lock()
	cachedResults := len(server.referenceResults)
	cachedInflight := len(server.referenceInflight)
	server.mu.Unlock()
	if cachedResults != 0 || cachedInflight != 0 {
		t.Fatalf("cold-index partial references published caches: results=%d inflight=%d", cachedResults, cachedInflight)
	}
}

func TestWorkspaceReferencesColdFallbackSkipsWorkspaceExpansionAndCaches(t *testing.T) {
	const source = `<!-- #include file="missing.inc" -->
<%
Function RootFunc(ByVal alpha)
  RootFunc = alpha
End Function
Response.Write RootFunc("x")
%>`
	server := New(nil, io.Discard, io.Discard)
	parsed := server.parseText("file:///private/tmp/reference-cold/root.asp", source, "VBScript")
	document := core.NewTextDocument(parsed.URI, "classic-asp", 0, source)
	position := document.PositionAt(strings.LastIndex(source, `RootFunc("x")`))
	var expansions atomic.Int32
	var parses atomic.Int32
	server.includeExpansionTestHook = func() { expansions.Add(1) }
	server.documentParseTestHook = func(string) { parses.Add(1) }
	server.mu.Lock()
	for index := 0; index < 256; index++ {
		uri := fmt.Sprintf("file:///private/tmp/reference-cold/unrelated-%03d.asp", index)
		server.workspace[uri] = core.NewTextDocument(uri, "classic-asp", 0, "<% UnrelatedValue = 1 %>")
	}
	server.workspaceIndexEnabled = true
	server.workspaceIndexGeneration = 1
	server.workspaceReferenceIndexReadyGeneration = 0
	server.workspaceIndexDone = make(chan struct{})
	server.mu.Unlock()

	var chunks [][]lsp.Location
	ctx := context.WithValue(t.Context(), workspaceReferenceLocationSinkContextKey{}, workspaceReferenceLocationSink(func(locations []lsp.Location) {
		chunks = append(chunks, append([]lsp.Location(nil), locations...))
	}))
	started := time.Now()
	locations := server.workspaceVBScriptReferencesWithTestDelay(ctx, parsed, position, true, "reference:function", false)
	if elapsed := time.Since(started); elapsed > 750*time.Millisecond {
		t.Fatalf("cold-index fallback took %v, want under 750ms", elapsed)
	}
	if len(locations) != 3 {
		t.Fatalf("cold-index fallback locations = %#v, want three RootFunc locations", locations)
	}
	for _, location := range locations {
		if location.URI != parsed.URI {
			t.Fatalf("cold-index fallback escaped current document: %#v", locations)
		}
	}
	if expansions.Load() != 0 || parses.Load() != 0 {
		t.Fatalf("cold-index fallback expanded workspace state: includeExpansions=%d parses=%d", expansions.Load(), parses.Load())
	}
	if len(chunks) != 1 || len(chunks[0]) != len(locations) {
		t.Fatalf("cold-index fallback partial result chunks = %#v, want one current-document chunk", chunks)
	}
	server.mu.Lock()
	results := len(server.referenceResults)
	inflight := len(server.referenceInflight)
	documents := len(server.referenceDocuments)
	counts := len(server.referenceCounts)
	partialCounts := len(server.referencePartialCounts)
	server.mu.Unlock()
	if results != 0 || inflight != 0 || documents != 0 || counts != 0 || partialCounts != 0 {
		t.Fatalf("cold-index fallback published workspace caches: results=%d inflight=%d documents=%d counts=%d partialCounts=%d", results, inflight, documents, counts, partialCounts)
	}
	server.referenceWorkspaceIndex.mu.RLock()
	indexedDocuments := len(server.referenceWorkspaceIndex.documents)
	server.referenceWorkspaceIndex.mu.RUnlock()
	if indexedDocuments != 0 {
		t.Fatalf("cold-index fallback mutated workspace reference index: documents=%d", indexedDocuments)
	}
}

func TestWorkspaceReferencesColdFallbackKeepsImplicitGlobalAndLocalBindingsDocumentLocal(t *testing.T) {
	const source = `<!-- #include file="shared.inc" -->
<%
implicitGlobal = "root"
Function RootFunction(ByVal value)
  RootFunction = value
  implicitGlobal = value
End Function
Sub Shadow(ByVal implicitGlobal)
  Response.Write implicitGlobal
End Sub
Response.Write implicitGlobal
Response.Write RootFunction("x")
%>`
	server := New(nil, io.Discard, io.Discard)
	root := t.TempDir()
	rootPath := root + "/root.asp"
	sharedPath := root + "/shared.inc"
	parsed := server.parseText(filePathURI(rootPath), source, "VBScript")
	document := core.NewTextDocument(parsed.URI, "classic-asp", 0, source)
	implicitPosition := document.PositionAt(strings.Index(source, "implicitGlobal ="))
	var parses atomic.Int32
	server.documentParseTestHook = func(string) { parses.Add(1) }
	server.rootPath = root
	server.rootURI = filePathURI(root)
	server.workspace[filePathURI(sharedPath)] = core.NewTextDocument(filePathURI(sharedPath), "classic-asp", 0, "<% implicitGlobal = 2 %>")
	server.workspaceIncludeGraph.Reset("cold-reference-test")
	server.workspaceIncludeGraph.Upsert(rootPath, workspacepkg.SourceMetadata{FileName: rootPath}, []string{sharedPath}, "root")
	server.workspaceIncludeGraph.Upsert(sharedPath, workspacepkg.SourceMetadata{FileName: sharedPath}, nil, "shared")
	for index := 0; index < 1024; index++ {
		parentPath := fmt.Sprintf("%s/parent-%04d.asp", root, index)
		server.workspace[filePathURI(parentPath)] = core.NewTextDocument(filePathURI(parentPath), "classic-asp", 0, "<% unrelated = 1 %>")
		server.workspaceIncludeGraph.Upsert(parentPath, workspacepkg.SourceMetadata{FileName: parentPath}, []string{rootPath}, "parent")
	}
	server.workspaceIncludeGraphComplete = true
	server.workspaceIndexEnabled = true
	server.workspaceIndexGeneration = 1
	server.workspaceReferenceIndexReadyGeneration = 0
	server.workspaceIndexDone = make(chan struct{})

	locations := server.workspaceVBScriptReferencesWithTestDelay(t.Context(), parsed, implicitPosition, false, "reference:variable", false)
	if len(locations) != 2 {
		t.Fatalf("cold-index implicit-global locations = %#v, want two non-declaration current-document locations", locations)
	}
	wantLines := map[int]struct{}{5: {}, 10: {}}
	for _, location := range locations {
		if location.URI != parsed.URI {
			t.Fatalf("cold-index implicit-global fallback escaped current document: %#v", locations)
		}
		if _, ok := wantLines[location.Range.Start.Line]; !ok {
			t.Fatalf("cold-index implicit-global fallback returned unrelated local/parameter reference: %#v", locations)
		}
	}
	if parses.Load() != 0 {
		t.Fatalf("cold-index implicit-global fallback traversed workspace documents: parses=%d", parses.Load())
	}
	shadowStart := strings.Index(source, "Sub Shadow")
	shadowReference := strings.Index(source[shadowStart:], "Response.Write implicitGlobal") + shadowStart + len("Response.Write ")
	parameterLocations := server.workspaceVBScriptReferencesWithTestDelay(t.Context(), parsed, document.PositionAt(shadowReference), false, "reference:variable", false)
	if len(parameterLocations) != 1 || parameterLocations[0].Range.Start.Line != 8 {
		t.Fatalf("cold-index parameter-local references = %#v, want only Shadow body reference", parameterLocations)
	}
	server.mu.Lock()
	if got := len(server.referenceResults) + len(server.referenceInflight) + len(server.referenceDocuments) + len(server.referenceCounts) + len(server.referencePartialCounts); got != 0 {
		t.Fatalf("cold-index implicit-global fallback published workspace caches: entries=%d", got)
	}
	server.mu.Unlock()
}

func TestWorkspaceReferencesColdFallbackHonorsCancellationBeforePartialPublication(t *testing.T) {
	const source = `<%
Function RootFunc(ByVal value)
  RootFunc = value
End Function
Response.Write RootFunc("x")
%>`
	server := New(nil, io.Discard, io.Discard)
	parsed := server.parseText("file:///private/tmp/reference-cold/cancel.asp", source, "VBScript")
	document := core.NewTextDocument(parsed.URI, "classic-asp", 0, source)
	position := document.PositionAt(strings.LastIndex(source, `RootFunc("x")`))
	server.mu.Lock()
	server.workspaceIndexEnabled = true
	server.workspaceIndexGeneration = 1
	server.workspaceReferenceIndexReadyGeneration = 0
	server.workspaceIndexDone = make(chan struct{})
	server.mu.Unlock()
	ctx, cancel := context.WithCancel(t.Context())
	cancel()
	locations := server.workspaceVBScriptReferencesWithTestDelay(ctx, parsed, position, true, "reference:function", false)
	if locations != nil {
		t.Fatalf("cancelled cold-index fallback returned locations: %#v", locations)
	}
	server.mu.Lock()
	results := len(server.referenceResults)
	inflight := len(server.referenceInflight)
	documents := len(server.referenceDocuments)
	counts := len(server.referenceCounts)
	partialCounts := len(server.referencePartialCounts)
	server.mu.Unlock()
	if results != 0 || inflight != 0 || documents != 0 || counts != 0 || partialCounts != 0 {
		t.Fatalf("cancelled cold-index fallback published workspace caches: results=%d inflight=%d documents=%d counts=%d partialCounts=%d", results, inflight, documents, counts, partialCounts)
	}
}
