package lspserver

import (
	"context"
	"io"
	"os"
	"path/filepath"
	"strings"
	"testing"

	"github.com/yottonoko/classic-web-system-lsp/internal/core"
	"github.com/yottonoko/classic-web-system-lsp/internal/lsp"
	workspacepkg "github.com/yottonoko/classic-web-system-lsp/internal/workspace"
)

func TestWorkspaceReferenceRequestKeyUsesFileIdentityAndPreservesExternalURI(t *testing.T) {
	position := lsp.Position{Line: 3, Character: 7}
	tests := []struct {
		name        string
		left, right string
		wantURI     string
	}{
		{
			name: "windows drive spelling",
			left: "file:///C:/Site/Default.asp", right: "file:///c%3A/site/default.asp",
			wantURI: "c:/site/default.asp",
		},
		{
			name: "posix dot segment",
			left: "file:///workspace/./default.asp", right: "file:///workspace/default.asp",
			wantURI: "/workspace/default.asp",
		},
		{
			name: "external URI",
			left: "untitled:Untitled-1", right: "untitled:Untitled-1",
			wantURI: "untitled:Untitled-1",
		},
	}
	for _, test := range tests {
		t.Run(test.name, func(t *testing.T) {
			left := workspaceReferenceRequestKey(test.left, position, false, "variable", 9, "SharedValue")
			right := workspaceReferenceRequestKey(test.right, position, false, "variable", 9, "sharedvalue")
			if left != right {
				t.Fatalf("equivalent request keys differ: left=%#v right=%#v", left, right)
			}
			if left.URI != test.wantURI {
				t.Fatalf("request key URI = %q, want %q", left.URI, test.wantURI)
			}
			if left.URI != workspacepkg.FileIdentityKeyFromURI(test.left) {
				t.Fatalf("request key URI = %q, want file identity %q", left.URI, workspacepkg.FileIdentityKeyFromURI(test.left))
			}
		})
	}
}

func TestWorkspaceReferencesEquivalentURIReusesComputedResultAndCodeLensResolution(t *testing.T) {
	server := New(nil, io.Discard, io.Discard)
	t.Cleanup(server.shutdownRuntimeCaches)
	const source = `<% Dim SharedValue : Response.Write SharedValue %>`
	uri := "file:///C:/workspace/Reference.asp"
	equivalentURI := "file:///c%3A/workspace/reference.asp"
	document := core.NewTextDocument(uri, "classic-asp", 1, source)
	server.documents[uri] = document
	parsed := core.ParseDocument(uri, source, core.Settings{DefaultLanguage: "VBScript"})
	equivalent := core.ParseDocument(equivalentURI, source, core.Settings{DefaultLanguage: "VBScript"})
	declarations := server.vbscriptReferenceCodeLensDeclarations(parsed)
	if len(declarations) != 1 {
		t.Fatalf("reference declarations = %#v, want one", declarations)
	}
	declaration := declarations[0]
	server.referenceWorkspaceIndex.update([]*core.ParsedDocument{parsed})

	first, stale := server.workspaceVBScriptReferencesOnce(
		context.Background(), parsed, declaration.Range.Start, false, declaration.Kind, false,
		[]*core.ParsedDocument{parsed}, true,
	)
	if stale || len(first) != 1 {
		t.Fatalf("initial workspace references = %#v, stale=%t; want one location", first, stale)
	}
	firstKey := workspaceReferenceRequestKey(uri, declaration.Range.Start, false, declaration.Kind, server.referenceGeneration, declaration.Name)

	second, stale := server.workspaceVBScriptReferencesOnce(
		context.Background(), equivalent, declaration.Range.Start, false, declaration.Kind, false,
		[]*core.ParsedDocument{equivalent}, true,
	)
	if stale || len(second) != len(first) {
		t.Fatalf("equivalent workspace references = %#v, stale=%t; want %#v", second, stale, first)
	}
	secondKey := workspaceReferenceRequestKey(equivalentURI, declaration.Range.Start, false, declaration.Kind, server.referenceGeneration, declaration.Name)
	if firstKey != secondKey {
		t.Fatalf("equivalent request keys differ: first=%#v second=%#v", firstKey, secondKey)
	}
	if &first[0] != &second[0] {
		t.Fatal("equivalent URI request recomputed locations instead of reusing the cached slice")
	}
	if len(server.referenceResults) != 1 || len(server.referenceCounts) != 1 {
		t.Fatalf("equivalent URI request grew caches: results=%d counts=%d", len(server.referenceResults), len(server.referenceCounts))
	}

	resolved := server.resolveCodeLens(context.Background(), lsp.CodeLens{Data: map[string]any{
		"uri": equivalentURI, "name": declaration.Name, "line": declaration.Range.Start.Line,
		"character": declaration.Range.Start.Character, "symbolKind": declaration.Kind,
	}})
	if resolved.Command == nil || resolved.Command.Title != "1 reference" {
		t.Fatalf("equivalent URI CodeLens resolution = %#v, want one cached reference", resolved)
	}
	if len(server.referenceResults) != 1 || len(server.referenceCounts) != 1 {
		t.Fatalf("CodeLens resolution recomputed or duplicated caches: results=%d counts=%d", len(server.referenceResults), len(server.referenceCounts))
	}
}

func TestWorkspaceReferenceEquivalentURIReusesPersistedCount(t *testing.T) {
	root := t.TempDir()
	cacheDirectory := filepath.Join(root, "cache")
	path := filepath.Join(root, "default.asp")
	uri := filePathURI(path)
	equivalentURI := strings.Replace(uri, "/default.asp", "/./default.asp", 1)
	const source = `<% Dim SharedValue : Response.Write SharedValue %>`

	first := newWorkspaceReferenceCountPersistenceServer(t, root, cacheDirectory)
	document := core.NewTextDocument(uri, "classic-asp", 1, source)
	first.workspace[uri] = document
	parsed := first.parseTextDocument(document, "VBScript")
	declarations := first.vbscriptReferenceCodeLensDeclarations(parsed)
	if len(declarations) != 1 {
		t.Fatalf("initial reference declarations = %#v, want one", declarations)
	}
	declaration := declarations[0]
	first.referenceWorkspaceIndex.update([]*core.ParsedDocument{parsed})
	locations, stale := first.workspaceVBScriptReferencesOnce(
		context.Background(), parsed, declaration.Range.Start, false, declaration.Kind, false,
		[]*core.ParsedDocument{parsed}, true,
	)
	if stale || len(locations) != 1 {
		t.Fatalf("initial persisted references = %#v, stale=%t; want one location", locations, stale)
	}
	first.persistWorkspaceReferenceBatch(uri, first.referenceGeneration)
	first.waitForAsyncDiskCacheWrites()
	if err := first.diskCacheForUse().Flush(); err != nil {
		t.Fatal(err)
	}
	first.closeDiskAnalysisCache()

	second := newWorkspaceReferenceCountPersistenceServer(t, root, cacheDirectory)
	t.Cleanup(second.closeDiskAnalysisCache)
	equivalentDocument := core.NewTextDocument(equivalentURI, "classic-asp", 1, source)
	second.workspace[equivalentURI] = equivalentDocument
	equivalentParsed := second.parseTextDocument(equivalentDocument, "VBScript")
	if !second.restoreWorkspaceReferenceBatch(equivalentDocument, equivalentParsed) {
		t.Fatal("persisted reference count was not restored for an equivalent URI")
	}
	equivalentDeclarations := second.vbscriptReferenceCodeLensDeclarations(equivalentParsed)
	if len(equivalentDeclarations) != 1 {
		t.Fatalf("restored reference declarations = %#v, want one", equivalentDeclarations)
	}
	key := workspaceReferenceRequestKey(equivalentURI, equivalentDeclarations[0].Range.Start, false, equivalentDeclarations[0].Kind, second.referenceGeneration, equivalentDeclarations[0].Name)
	if got, ok := second.referenceCounts[key]; !ok || got != 1 {
		t.Fatalf("restored reference count = %d, %t; want 1, true", got, ok)
	}
	if len(second.referenceResults) != 0 {
		t.Fatalf("persisted count restore eagerly computed full locations: %#v", second.referenceResults)
	}
	if got, complete := second.workspaceVBScriptReferenceCount(context.Background(), equivalentParsed, equivalentDeclarations[0]); !complete || got != 1 {
		t.Fatalf("resolved persisted reference count = %d, complete=%t; want 1, true", got, complete)
	}
}

func TestWorkspaceReferencePromotionReusesEquivalentURIForUnchangedWorkspace(t *testing.T) {
	root := t.TempDir()
	uri := filePathURI(filepath.Join(root, "default.asp"))
	equivalentURI := strings.Replace(uri, "/default.asp", "/./default.asp", 1)
	const source = `<% Dim SharedValue : Response.Write SharedValue %>`
	server := New(nil, io.Discard, io.Discard)
	t.Cleanup(server.shutdownRuntimeCaches)
	server.rootPath = root
	server.rootURI = filePathURI(root)
	server.workspaceRoots = []workspaceRoot{{Path: root, URI: server.rootURI}}
	server.workspace[uri] = core.NewTextDocument(uri, "classic-asp", 1, source)
	server.workspaceIndexGeneration = 3
	server.workspaceReferenceIndexReadyGeneration = 3
	server.referenceGeneration = 7
	server.workspaceReferenceReadyFingerprint = server.workspaceReferenceUniverseFingerprint()
	key := workspaceReferenceRequestKey(uri, lsp.Position{Line: 0, Character: 6}, false, "variable", server.referenceGeneration, "SharedValue")
	server.referenceCounts[key] = 23

	server.mu.Lock()
	promotion := server.workspaceReferencePromotionCandidateLocked()
	if promotion == nil || len(promotion.counts) != 1 {
		server.mu.Unlock()
		t.Fatalf("unchanged workspace promotion candidate = %#v, want one count", promotion)
	}
	server.clearWorkspaceReferenceCacheLocked()
	nextWorkspaceGeneration := server.workspaceIndexGeneration + 1
	promotion.workspaceGeneration = nextWorkspaceGeneration
	server.workspaceIndexGeneration = nextWorkspaceGeneration
	server.workspaceReferencePendingPromotion = promotion
	server.mu.Unlock()

	server.mu.Lock()
	promoted := server.promoteWorkspaceReferenceCountsLocked(nextWorkspaceGeneration, promotion.fingerprint)
	server.mu.Unlock()
	if promoted != 1 {
		t.Fatalf("unchanged workspace promoted counts = %d, want one", promoted)
	}
	equivalentKey := workspaceReferenceRequestKey(equivalentURI, lsp.Position{Line: 0, Character: 6}, false, "variable", server.referenceGeneration, "SharedValue")
	if got := server.referenceCounts[equivalentKey]; got != 23 {
		t.Fatalf("promoted equivalent URI count = %d, want 23", got)
	}
}

func TestWorkspaceReferenceUnchangedWorkspaceReindexPromotesEquivalentURICount(t *testing.T) {
	root := t.TempDir()
	path := filepath.Join(root, "default.asp")
	const source = `<% Dim SharedValue : Response.Write SharedValue %>`
	if err := os.WriteFile(path, []byte(source), 0o600); err != nil {
		t.Fatal(err)
	}
	uri := filePathURI(path)
	equivalentURI := strings.Replace(uri, "/default.asp", "/./default.asp", 1)
	server := New(nil, io.Discard, io.Discard)
	t.Cleanup(func() {
		server.stopWorkspaceIndexWorkers()
		server.shutdownRuntimeCaches()
	})
	server.rootPath = root
	server.rootURI = filePathURI(root)
	server.workspaceRoots = []workspaceRoot{{Path: root, URI: server.rootURI}}
	server.settings.CacheEnabled = false
	server.analysisWorkers.setWorkers(2)
	server.activateWorkspaceIndexing(context.Background())
	server.scheduleWorkspaceIndex("test.initial")
	waitForWorkspaceIndexCompletion(t, server)

	equivalentParsed := core.ParseDocument(equivalentURI, source, core.Settings{DefaultLanguage: "VBScript"})
	declarations := server.vbscriptReferenceCodeLensDeclarations(equivalentParsed)
	if len(declarations) != 1 {
		t.Fatalf("reference declarations = %#v, want one", declarations)
	}
	locations, stale := server.workspaceVBScriptReferencesOnce(
		context.Background(), equivalentParsed, declarations[0].Range.Start, false, declarations[0].Kind, false,
		[]*core.ParsedDocument{equivalentParsed}, true,
	)
	if stale || len(locations) != 1 {
		t.Fatalf("initial equivalent URI references = %#v, stale=%t; want one location", locations, stale)
	}
	initialKey := workspaceReferenceRequestKey(equivalentURI, declarations[0].Range.Start, false, declarations[0].Kind, server.referenceGeneration, declarations[0].Name)
	if got := server.referenceCounts[initialKey]; got != 1 {
		t.Fatalf("initial equivalent URI count = %d, want 1", got)
	}

	server.scheduleWorkspaceIndex("test.unchangedReopen")
	waitForWorkspaceIndexCompletion(t, server)
	server.mu.Lock()
	currentGeneration := server.referenceGeneration
	server.mu.Unlock()
	promotedKey := workspaceReferenceRequestKey(equivalentURI, declarations[0].Range.Start, false, declarations[0].Kind, currentGeneration, declarations[0].Name)
	if got := server.referenceCounts[promotedKey]; got != 1 {
		t.Fatalf("unchanged workspace reindex promoted count = %d, want 1", got)
	}
}

func TestWorkspaceReferenceOpenOnlyCloseStillInvalidatesCanonicalCache(t *testing.T) {
	uri := "file:///C:/workspace/OpenOnly.asp"
	equivalentURI := "file:///c%3A/workspace/openonly.asp"
	const source = `<% Dim SharedValue : Response.Write SharedValue %>`
	server := New(nil, io.Discard, io.Discard)
	t.Cleanup(server.shutdownRuntimeCaches)
	document := core.NewTextDocument(uri, "classic-asp", 1, source)
	server.documents[uri] = document
	parsed := server.parseTextDocument(document, "VBScript")
	declarations := server.vbscriptReferenceCodeLensDeclarations(parsed)
	if len(declarations) != 1 {
		t.Fatalf("reference declarations = %#v, want one", declarations)
	}
	key := workspaceReferenceRequestKey(uri, declarations[0].Range.Start, false, declarations[0].Kind, server.referenceGeneration, declarations[0].Name)
	server.referenceCounts[key] = 1
	server.referenceResults[key] = []lsp.Location{{URI: uri, Range: declarations[0].Range}}

	if err := server.handleNotification(context.Background(), "textDocument/didClose", mustRaw(map[string]any{
		"textDocument": map[string]any{"uri": equivalentURI},
	})); err != nil {
		t.Fatal(err)
	}
	if len(server.referenceCounts) != 0 || len(server.referenceResults) != 0 {
		t.Fatalf("open-only close retained reference cache: counts=%#v results=%#v", server.referenceCounts, server.referenceResults)
	}
}
