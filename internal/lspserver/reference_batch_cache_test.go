package lspserver

import (
	"bufio"
	"context"
	"io"
	"os"
	"path/filepath"
	"strconv"
	"strings"
	"testing"
	"time"

	"github.com/fxamacker/cbor/v2"
	"github.com/yottonoko/classic-web-system-lsp/internal/core"
	"github.com/yottonoko/classic-web-system-lsp/internal/lsp"
	workspacepkg "github.com/yottonoko/classic-web-system-lsp/internal/workspace"
)

func TestResolveCodeLensRefreshesDirectCountWhileBatchIsStillRunning(t *testing.T) {
	outputReader, outputWriter := io.Pipe()
	defer outputReader.Close()
	defer outputWriter.Close()
	server := New(strings.NewReader(""), outputWriter, io.Discard)
	defer server.shutdownRuntimeCaches()
	server.codeLensRefreshSupported = true

	uri := "file:///workspace/direct-count-refresh.asp"
	doc := core.NewTextDocument(uri, "classic-asp", 1, "<%\nDim SharedValue\nResponse.Write SharedValue\n%>")
	server.documents[uri] = doc
	parsed := server.parseTextDocument(doc, "VBScript")
	declarations := server.vbscriptReferenceCodeLensDeclarations(parsed)
	if len(declarations) != 1 {
		t.Fatalf("reference declarations = %#v, want one", declarations)
	}
	// Keep the follow-up document-wide batch unfinished. The direct resolve must
	// still refresh the already calculated declaration instead of waiting for it.
	server.referenceBatch[referenceBatchCacheKey(uri, doc.Version, server.referenceGeneration)] = &workspaceReferenceBatchState{
		generation: server.referenceGeneration,
		total:      len(declarations),
	}

	lenses := server.codeLens(uri)
	if len(lenses) != 1 || lenses[0].Command == nil || !strings.Contains(lenses[0].Command.Title, "Calculating") {
		t.Fatalf("cold CodeLens = %#v", lenses)
	}
	resolved := make(chan lsp.CodeLens, 1)
	go func() { resolved <- server.resolveCodeLens(context.Background(), lenses[0]) }()

	messages := make(chan *rpcMessage, 8)
	go func() {
		reader := bufio.NewReader(outputReader)
		for {
			message, err := readMessage(reader)
			if err != nil {
				return
			}
			messages <- message
		}
	}()
	deadline := time.After(time.Second)
	for {
		select {
		case message := <-messages:
			if message.Method != "workspace/codeLens/refresh" {
				continue
			}
			if message.ID == nil {
				t.Fatalf("CodeLens refresh is not a client request: %#v", message)
			}
			server.deliverClientResponse(rpcMessage{ID: message.ID, Result: mustRaw(nil)})
			select {
			case lens := <-resolved:
				if lens.Command == nil || lens.Command.Title != "1 reference" {
					t.Fatalf("resolved CodeLens = %#v", lens)
				}
			case <-time.After(time.Second):
				t.Fatal("CodeLens resolve did not complete")
			}
			return
		case <-deadline:
			t.Fatal("directly calculated reference count did not refresh CodeLens")
		}
	}
}

func TestWorkspaceReferencePublicationRefreshesVisibleCodeLens(t *testing.T) {
	outputReader, outputWriter := io.Pipe()
	defer outputReader.Close()
	defer outputWriter.Close()
	server := New(strings.NewReader(""), outputWriter, io.Discard)
	defer server.shutdownRuntimeCaches()
	server.codeLensRefreshSupported = true

	const source = "<%\nDim SharedValue\nResponse.Write SharedValue\n%>"
	uri := "file:///workspace/reference-publication-refresh.asp"
	doc := core.NewTextDocument(uri, "classic-asp", 1, source)
	parsed := server.parseTextDocument(doc, "VBScript")
	position := doc.PositionAt(strings.Index(source, "SharedValue"))
	server.referenceWorkspaceIndex.update([]*core.ParsedDocument{parsed})

	locations, stale := server.workspaceVBScriptReferencesOnce(
		context.Background(), parsed, position, false, "reference:variable", false,
		[]*core.ParsedDocument{parsed}, true,
	)
	if stale || len(locations) != 1 {
		t.Fatalf("workspace references = %#v, stale=%t", locations, stale)
	}

	message, err := readMessage(bufio.NewReader(outputReader))
	if err != nil {
		t.Fatal(err)
	}
	if message.Method != "workspace/codeLens/refresh" || message.ID == nil {
		t.Fatalf("reference publication refresh = %#v", message)
	}
	server.deliverClientResponse(rpcMessage{ID: message.ID, Result: mustRaw(nil)})
}

func TestWorkspaceReferenceCodeLensCountIsPendingUntilWorkspaceIndexIsComplete(t *testing.T) {
	server := New(nil, io.Discard, io.Discard)
	uri := "file:///workspace/pending-code-lens.asp"
	doc := core.NewTextDocument(uri, "classic-asp", 7, "<% Dim SharedValue %>")
	server.documents[uri] = doc
	server.settings.CodeLensReferences = true
	parsed := server.parseTextDocument(doc, "VBScript")
	declarations := server.vbscriptReferenceCodeLensDeclarations(parsed)
	if len(declarations) != 1 {
		t.Fatalf("declarations = %d, want 1", len(declarations))
	}
	declaration := declarations[0]
	key := workspaceReferenceRequestKey(uri, declaration.Range.Start, false, declaration.Kind, server.referenceGeneration, declaration.Name)
	server.referenceCounts[key] = 1
	server.workspaceIndexEnabled = true
	server.workspaceIndexGeneration = 2
	server.workspaceReferenceIndexReadyGeneration = 1
	indexDone := make(chan struct{})
	server.workspaceIndexDone = indexDone

	lenses := server.codeLens(uri)
	if len(lenses) != 1 || lenses[0].Command == nil || !strings.Contains(strings.ToLower(lenses[0].Command.Title), "calculating") {
		t.Fatalf("incomplete workspace index did not expose a calculating CodeLens: %#v", lenses)
	}
	batchKey := referenceBatchCacheKey(uri, doc.Version, server.referenceGeneration)
	server.mu.Lock()
	batch := server.referenceBatch[batchKey]
	var progress *serverProgressTask
	if batch != nil {
		progress = server.progressTasks[batch.progressTaskID]
	}
	server.mu.Unlock()
	if batch == nil || progress == nil || progress.DocumentURI != uri || !progress.HasDocumentVersion || progress.DocumentVersion != doc.Version {
		t.Fatalf("pending CodeLens progress metadata is incomplete: batch=%#v progress=%#v", batch, progress)
	}
	server.mu.Lock()
	server.workspaceReferenceIndexReadyGeneration = 2
	server.mu.Unlock()
	close(indexDone)
	select {
	case <-batch.done:
	case <-time.After(time.Second):
		t.Fatal("workspace reference batch did not resume after index completion")
	}
	state := server.snapshotWorkspaceReferenceCodeLensCounts(parsed, declarations)[0]
	if !state.final || state.count != 1 {
		t.Fatalf("complete workspace index did not expose the cached count: %#v", state)
	}
}

func TestReferenceCodeLensTargetsOnlyOwnerDeclarationsButCountsIncomingIncludeReferences(t *testing.T) {
	root := t.TempDir()
	includedPath := filepath.Join(root, "shared.inc")
	activePath := filepath.Join(root, "default.asp")
	includedSource := "<% Dim SharedValue %>"
	activeSource := "<!-- #include file=\"shared.inc\" -->\n<% Dim ActiveValue : SharedValue = SharedValue + ActiveValue %>"
	if err := os.WriteFile(includedPath, []byte(includedSource), 0o600); err != nil {
		t.Fatal(err)
	}
	if err := os.WriteFile(activePath, []byte(activeSource), 0o600); err != nil {
		t.Fatal(err)
	}
	server := New(nil, io.Discard, io.Discard)
	server.rootPath = root
	server.rootURI = filePathURI(root)
	server.workspaceRoots = []workspaceRoot{{Path: root, URI: server.rootURI}}
	active := server.parseText(filePathURI(activePath), activeSource, "VBScript")
	included := server.parseText(filePathURI(includedPath), includedSource, "VBScript")

	for _, declaration := range server.vbscriptReferenceCodeLensDeclarations(active) {
		if strings.EqualFold(declaration.Name, "SharedValue") {
			t.Fatalf("included declaration usage became an active-document CodeLens target: %#v", declaration)
		}
	}
	includedDeclarations := server.vbscriptReferenceCodeLensDeclarations(included)
	if len(includedDeclarations) != 1 || !strings.EqualFold(includedDeclarations[0].Name, "SharedValue") {
		t.Fatalf("included owner declarations = %#v, want SharedValue", includedDeclarations)
	}
	results := server.workspaceVBScriptReferenceBatch(context.Background(), included, includedDeclarations, []*core.ParsedDocument{included, active}, server.referenceGeneration, nil)
	if len(results) != 1 || results[0].stale || results[0].count != 2 {
		t.Fatalf("incoming include references = %#v, want 2 complete references", results)
	}
}

func TestReferenceCodeLensDoesNotTargetFirstCrossFileUsage(t *testing.T) {
	root := t.TempDir()
	definitionURI := filePathURI(filepath.Join(root, "definitions.asp"))
	usageURI := filePathURI(filepath.Join(root, "page.asp"))
	definitionSource := "<% Dim SharedValue %>"
	usageSource := "<% Response.Write SharedValue %>"
	server := New(nil, io.Discard, io.Discard)
	server.settings.CodeLensReferences = true
	definitionDoc := core.NewTextDocument(definitionURI, "classic-asp", 1, definitionSource)
	usageDoc := core.NewTextDocument(usageURI, "classic-asp", 1, usageSource)
	server.documents[definitionURI] = definitionDoc
	server.documents[usageURI] = usageDoc
	server.parseTextDocument(definitionDoc, "VBScript")
	server.parseTextDocument(usageDoc, "VBScript")

	if codeLensesContainName(server.codeLens(usageURI), "SharedValue") {
		t.Fatalf("first cross-file usage became a CodeLens target: %#v", server.codeLens(usageURI))
	}
	if !codeLensesContainName(server.codeLens(definitionURI), "SharedValue") {
		t.Fatalf("definition CodeLens missing: %#v", server.codeLens(definitionURI))
	}
}

func TestWorkspaceReferenceCodeLensUsesCachedCommandWithoutResolve(t *testing.T) {
	server := New(strings.NewReader(""), io.Discard, io.Discard)
	uri := "file:///workspace/cached-code-lens.asp"
	text := "<%\nDim SharedValue\nResponse.Write SharedValue\n%>"
	doc := core.NewTextDocument(uri, "classic-asp", 1, text)
	server.settings.CodeLensReferences = true
	server.documents[uri] = doc
	parsed := server.parseTextDocument(doc, "VBScript")
	declarations := server.vbscriptReferenceCodeLensDeclarations(parsed)
	if len(declarations) != 1 {
		t.Fatalf("reference declarations = %d, want 1", len(declarations))
	}
	declaration := declarations[0]
	wantLocations := []lsp.Location{{URI: uri, Range: lsp.Range{Start: lsp.Position{Line: 2, Character: 15}, End: lsp.Position{Line: 2, Character: 26}}}}
	server.referenceResults[workspaceReferenceRequestKey(uri, declaration.Range.Start, false, declaration.Kind, server.referenceGeneration, declaration.Name)] = wantLocations
	done := make(chan struct{})
	close(done)
	server.referenceBatch[referenceBatchCacheKey(uri, doc.Version, server.referenceGeneration)] = &workspaceReferenceBatchState{
		generation: server.referenceGeneration, done: done, complete: true, total: 1, warmed: 1,
	}

	lenses := server.codeLens(uri)
	if len(lenses) != 1 || lenses[0].Command == nil {
		t.Fatalf("cached CodeLens was left unresolved: %#v", lenses)
	}
	if lenses[0].Command.Title != "1 reference" {
		t.Fatalf("cached CodeLens title = %q, want %q", lenses[0].Command.Title, "1 reference")
	}
	if len(lenses[0].Command.Arguments) != 2 {
		t.Fatalf("cached CodeLens arguments = %#v", lenses[0].Command.Arguments)
	}
}

func TestWorkspaceReferenceCodeLensUsesAlignedCompletedBatchCounts(t *testing.T) {
	uri := "file:///workspace/default.asp"
	parsed := core.ParseDocument(uri, `<% Dim First, Second %>`, core.Settings{DefaultLanguage: "VBScript"})
	server := New(nil, io.Discard, nil)
	declarations := server.vbscriptReferenceCodeLensDeclarations(parsed)
	if len(declarations) != 2 {
		t.Fatalf("declarations = %#v, want 2", declarations)
	}
	server.referenceBatch[referenceBatchCacheKey(uri, 0, server.referenceGeneration)] = &workspaceReferenceBatchState{
		generation:   server.referenceGeneration,
		complete:     true,
		declarations: declarations,
		finalCounts:  []int{7, 11},
	}
	states := server.snapshotWorkspaceReferenceCodeLensCounts(parsed, declarations)
	if len(states) != 2 || !states[0].final || states[0].count != 7 || !states[1].final || states[1].count != 11 {
		t.Fatalf("aligned batch states = %#v", states)
	}

	changed := append([]vbUsageDeclaration(nil), declarations...)
	changed[0].Name = "Changed"
	states = server.snapshotWorkspaceReferenceCodeLensCounts(parsed, changed)
	if states[0].final || states[1].final {
		t.Fatalf("mismatched declarations reused aligned batch states: %#v", states)
	}
}

func TestWorkspaceReferenceDescriptorFingerprintsReuseUnchangedNames(t *testing.T) {
	uri := "file:///workspace/default.asp"
	parsed := core.ParseDocument(uri, `<% Dim First, Second : Response.Write First : Response.Write Second %>`, core.Settings{DefaultLanguage: "VBScript"})
	server := New(nil, io.Discard, nil)
	declarations := server.vbscriptReferenceCodeLensDeclarations(parsed)
	server.referenceWorkspaceIndex.updateCountContext(context.Background(), []*core.ParsedDocument{parsed})
	first := server.workspaceReferenceQueryDescriptors(parsed, declarations, []*core.ParsedDocument{parsed})
	if len(first) != 2 {
		t.Fatalf("initial descriptors = %#v", first)
	}
	cacheKey := workspacepkg.FileIdentityKeyFromURI(uri) + "#" + strconv.FormatUint(server.referenceGeneration, 10)
	cached := server.referenceDescriptorFingerprints[cacheKey]
	if cached == nil || cached.scope == "" || len(cached.names) != 2 {
		t.Fatalf("descriptor fingerprint cache = %#v", cached)
	}
	secondBefore := cached.names["second"]
	server.referenceNameRevisions["first"]++
	second := server.workspaceReferenceQueryDescriptors(parsed, declarations, []*core.ParsedDocument{parsed})
	if len(second) != len(first) || second[0].scopeFingerprint != first[0].scopeFingerprint {
		t.Fatalf("recomputed descriptors = %#v, want scope %q", second, first[0].scopeFingerprint)
	}
	cached = server.referenceDescriptorFingerprints[cacheKey]
	if cached.names["first"].revision != 1 || cached.names["second"] != secondBefore {
		t.Fatalf("name fingerprint revisions = %#v", cached.names)
	}
}

func TestWorkspaceReferenceCodeLensPlanReusesExactParsedRevision(t *testing.T) {
	parsed := core.ParseDocument("file:///workspace/default.asp", `<% Dim First, Second %>`, core.Settings{DefaultLanguage: "VBScript"})
	server := New(nil, io.Discard, nil)
	first := server.workspaceReferenceCodeLensPlan(parsed)
	second := server.workspaceReferenceCodeLensPlan(parsed)
	if len(first.declarations) != 2 || len(second.declarations) != 2 || &first.declarations[0] != &second.declarations[0] {
		t.Fatalf("declaration plan was not reused: first=%#v second=%#v", first, second)
	}
	server.clearWorkspaceReferenceCache()
	third := server.workspaceReferenceCodeLensPlan(parsed)
	if len(third.declarations) != 2 || &third.declarations[0] == &first.declarations[0] {
		t.Fatalf("invalidated declaration plan was reused: first=%#v third=%#v", first, third)
	}
}

func TestWorkspaceReferenceBatchRestoresFromAnalysisDatabase(t *testing.T) {
	root := t.TempDir()
	cacheDirectory := filepath.Join(root, "cache")
	path := filepath.Join(root, "default.asp")
	uri := filePathURI(path)
	text := `<%
Function FirstValue()
  FirstValue = 1
End Function
Function SecondValue()
  SecondValue = FirstValue()
End Function
Response.Write SecondValue()
%>`
	doc := core.NewTextDocument(uri, "classic-asp", 1, text)
	newServer := func() *Server {
		server := New(strings.NewReader(""), io.Discard, io.Discard)
		server.rootPath = root
		server.rootURI = filePathURI(root)
		server.workspaceRoots = []workspaceRoot{{Path: root, URI: filePathURI(root)}}
		server.settings.CacheEnabled = true
		server.settings.CacheDirectory = cacheDirectory
		server.settings.CacheTTLHours = 24
		server.settings.CacheMaxSizeMB = 16
		server.configureDiskAnalysisCache()
		server.configureFsGateway()
		server.workspace[uri] = doc
		return server
	}

	first := newServer()
	parsed := first.parseTextDocument(doc, "VBScript")
	first.applyWorkspaceDocumentRevision(doc, parsed)
	declarations := first.vbscriptReferenceCodeLensDeclarations(parsed)
	if len(declarations) < 2 {
		t.Fatalf("reference declarations = %d, want at least 2", len(declarations))
	}
	for _, declaration := range declarations {
		first.workspaceVBScriptReferencesWithTestDelay(context.Background(), parsed, declaration.Range.Start, false, declaration.Kind, false)
	}
	first.persistWorkspaceReferenceBatch(uri, first.referenceGeneration)
	first.waitForAsyncDiskCacheWrites()
	if err := first.diskCacheForUse().Flush(); err != nil {
		t.Fatal(err)
	}
	documentKey := workspacepkg.FileIdentityKeyFromURI(uri)
	manifestPayload := first.diskCacheForUse().ReadReferenceValuesAligned(workspacepkg.DiskReferenceDocuments, [][]byte{workspaceReferenceDocumentCacheKey(documentKey)})[0]
	var manifest persistedWorkspaceReferenceDocument
	if cbor.Unmarshal(manifestPayload, &manifest) != nil || len(manifest.PostingKeys) == 0 {
		t.Fatalf("persisted reference manifest = %#v", manifest)
	}
	postingKey := []byte(workspaceReferencePostingCacheKey(documentKey, manifest.PostingKeys[0]))
	if payload := first.diskCacheForUse().ReadReferenceValuesAligned(workspacepkg.DiskReferencePostings, [][]byte{postingKey})[0]; len(payload) == 0 {
		t.Fatal("persisted reference posting is missing")
	}
	first.closeDiskAnalysisCache()

	second := newServer()
	t.Cleanup(second.closeDiskAnalysisCache)
	secondParsed := second.parseTextDocument(doc, "VBScript")
	if !second.restoreWorkspaceReferenceBatch(doc, secondParsed) {
		t.Fatal("workspace reference batch was not restored")
	}
	for _, declaration := range second.vbscriptReferenceCodeLensDeclarations(secondParsed) {
		key := workspaceReferenceRequestKey(uri, declaration.Range.Start, false, declaration.Kind, second.referenceGeneration, declaration.Name)
		if _, ok := second.referenceCounts[key]; !ok {
			t.Fatalf("restored reference count missing for %s at %#v", declaration.Name, declaration.Range.Start)
		}
	}
}

func TestPersistWorkspaceReferenceBatchWritesQueriesOnly(t *testing.T) {
	root := t.TempDir()
	cacheDirectory := filepath.Join(root, "cache")
	path := filepath.Join(root, "default.asp")
	uri := filePathURI(path)
	doc := core.NewTextDocument(uri, "classic-asp", 1, `<% Dim SharedValue : Response.Write SharedValue %>`)
	server := New(strings.NewReader(""), io.Discard, io.Discard)
	defer server.closeDiskAnalysisCache()
	server.rootPath, server.rootURI = root, filePathURI(root)
	server.workspaceRoots = []workspaceRoot{{Path: root, URI: server.rootURI}}
	server.settings.CacheEnabled, server.settings.CacheDirectory = true, cacheDirectory
	server.settings.CacheTTLHours, server.settings.CacheMaxSizeMB = 24, 16
	server.configureDiskAnalysisCache()
	server.workspace[uri] = doc
	parsed := server.parseTextDocument(doc, server.settings.DefaultLanguage)
	server.applyWorkspaceDocumentRevision(doc, parsed)
	server.waitForAsyncDiskCacheWrites()
	server.removeWorkspaceDocumentRevision(uri)
	if err := server.diskCacheForUse().Flush(); err != nil {
		t.Fatal(err)
	}
	declarations := server.vbscriptReferenceCodeLensDeclarations(parsed)
	if len(declarations) != 1 {
		t.Fatalf("declarations = %d, want 1", len(declarations))
	}
	declaration := declarations[0]
	requestKey := workspaceReferenceRequestKey(uri, declaration.Range.Start, false, declaration.Kind, server.referenceGeneration, declaration.Name)
	server.referenceCounts[requestKey] = 1
	server.persistWorkspaceReferenceBatch(uri, server.referenceGeneration)
	if err := server.diskCacheForUse().Flush(); err != nil {
		t.Fatal(err)
	}
	documentKey := workspacepkg.FileIdentityKeyFromURI(uri)
	if payload := server.diskCacheForUse().ReadReferenceValuesAligned(workspacepkg.DiskReferenceDocuments, [][]byte{workspaceReferenceDocumentCacheKey(documentKey)})[0]; payload != nil {
		t.Fatal("query persistence rewrote the deleted document shard")
	}
	descriptors := server.workspaceReferenceQueryDescriptors(parsed, declarations, server.workspaceReferenceDocuments(parsed))
	if payload := server.diskCacheForUse().ReadReferenceValuesAligned(workspacepkg.DiskReferenceQueries, [][]byte{descriptors[0].key})[0]; payload == nil {
		t.Fatal("reference query count was not persisted")
	}
}

func TestWorkspaceReferenceBatchRestoresValidPartialAnalysisDatabasePayload(t *testing.T) {
	root := t.TempDir()
	cacheDirectory := filepath.Join(root, "cache")
	path := filepath.Join(root, "default.asp")
	uri := filePathURI(path)
	text := "<%\nFunction FirstValue()\nEnd Function\nFunction SecondValue()\nEnd Function\n%>"
	doc := core.NewTextDocument(uri, "classic-asp", 1, text)
	server := New(strings.NewReader(""), io.Discard, io.Discard)
	defer server.closeDiskAnalysisCache()
	server.rootPath = root
	server.rootURI = filePathURI(root)
	server.workspaceRoots = []workspaceRoot{{Path: root, URI: filePathURI(root)}}
	server.settings.CacheEnabled = true
	server.settings.CacheDirectory = cacheDirectory
	server.settings.CacheTTLHours = 24
	server.settings.CacheMaxSizeMB = 16
	server.configureDiskAnalysisCache()
	server.workspace[uri] = doc
	parsed := server.parseTextDocument(doc, "VBScript")
	declarations := server.vbscriptReferenceCodeLensDeclarations(parsed)
	if len(declarations) < 2 {
		t.Fatalf("reference declarations = %d, want at least 2", len(declarations))
	}
	documents := server.workspaceReferenceDocuments(parsed)
	descriptors := server.workspaceReferenceQueryDescriptors(parsed, declarations, documents)
	partial := persistedWorkspaceReferenceQuery{
		SchemaVersion: workspaceReferenceQuerySchemaVersion, DeclarationFingerprint: descriptors[0].declarationFingerprint,
		NameFingerprint: descriptors[0].nameFingerprint, ScopeFingerprint: descriptors[0].scopeFingerprint, Count: 7,
	}
	payload, err := cbor.Marshal(partial)
	if err != nil {
		t.Fatal(err)
	}
	if err := server.diskCacheForUse().WriteReferenceQuery(descriptors[0].key, payload); err != nil {
		t.Fatal(err)
	}
	if err := server.diskCacheForUse().WriteReferenceQuery(descriptors[1].key, []byte("corrupt-cbor")); err != nil {
		t.Fatal(err)
	}
	if !server.restoreWorkspaceReferenceBatch(doc, parsed) {
		t.Fatal("valid partial workspace reference query was not restored")
	}
	firstKey := workspaceReferenceRequestKey(uri, declarations[0].Range.Start, false, declarations[0].Kind, server.referenceGeneration, declarations[0].Name)
	secondKey := workspaceReferenceRequestKey(uri, declarations[1].Range.Start, false, declarations[1].Kind, server.referenceGeneration, declarations[1].Name)
	if count, ok := server.referenceCounts[firstKey]; !ok || count != 7 {
		t.Fatalf("first restored count = %d, %t; want 7, true", count, ok)
	}
	if _, ok := server.referenceCounts[secondKey]; ok {
		t.Fatal("corrupt query was unexpectedly restored")
	}
	if err := server.diskCacheForUse().Flush(); err != nil {
		t.Fatal(err)
	}
	if value := server.diskCacheForUse().ReadReferenceValuesAligned(workspacepkg.DiskReferenceQueries, [][]byte{descriptors[1].key})[0]; value != nil {
		t.Fatalf("corrupt query was not deleted: %x", value)
	}
}

func TestWorkspaceReferenceRetryRejectsChangedSourceDocument(t *testing.T) {
	server := New(nil, io.Discard, io.Discard)
	uri := "file:///workspace/reference-source-change.asp"
	oldDoc := core.NewTextDocument(uri, "classic-asp", 1, "<% Dim OldValue : Response.Write OldValue %>")
	server.mu.Lock()
	server.rememberOpenDocumentLocked(uri, oldDoc)
	server.mu.Unlock()
	previous := server.parseTextDocument(oldDoc, "VBScript")
	newDoc := core.NewTextDocument(uri, "classic-asp", 2, "<% Dim NewValue : Response.Write NewValue %>")
	server.mu.Lock()
	server.rememberOpenDocumentLocked(uri, newDoc)
	server.deleteParsedCacheForURILocked(uri)
	server.rememberDocumentTextLocked(newDoc)
	server.clearWorkspaceReferenceCacheLocked()
	server.mu.Unlock()
	if current, ok := server.currentWorkspaceReferenceParsed(previous); ok || current == nil || current.Text != newDoc.Text {
		t.Fatalf("changed source accepted for retry: current=%#v ok=%t", current, ok)
	}
}
