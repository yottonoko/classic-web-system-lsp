package lspserver

import (
	"io"
	"strings"
	"testing"

	"github.com/yottonoko/classic-web-system-lsp/internal/core"
)

func TestWorkspaceReferenceDocumentCountReuse(t *testing.T) {
	const uri = "file:///workspace/document-delta.asp"
	oldText := `<%
Function SharedValue()
  SharedValue = 1
End Function
Response.Write SharedValue()
%>`
	newText := `<%
Function SharedValue()
  SharedValue = 1
End Function
Response.Write SharedValue()
Response.Write SharedValue()
%>`
	server := New(nil, io.Discard, io.Discard)
	oldDocument := core.NewTextDocument(uri, "classic-asp", 1, oldText)
	server.workspace[uri] = oldDocument
	oldParsed := server.parseTextDocument(oldDocument, server.settings.DefaultLanguage)
	oldRevision := server.applyWorkspaceDocumentRevision(oldDocument, oldParsed)
	if oldRevision.Manifest == nil || oldRevision.Stale {
		t.Fatalf("initial workspace revision = %#v", oldRevision)
	}
	oldDeclarations := server.workspaceReferenceCodeLensPlan(oldParsed).declarations
	if len(oldDeclarations) != 1 {
		t.Fatalf("initial declarations = %#v", oldDeclarations)
	}
	const previousWorkspaceCount = 7
	done := make(chan struct{})
	close(done)
	server.mu.Lock()
	server.storeWorkspaceReferenceBatchLocked(referenceBatchCacheKey(uri, 0, server.referenceGeneration), &workspaceReferenceBatchState{
		generation: server.referenceGeneration, done: done, complete: true,
		nameRevisions: map[string]uint64{"sharedvalue": server.referenceNameRevisions["sharedvalue"]},
		declarations:  append([]vbUsageDeclaration(nil), oldDeclarations...), finalCounts: []int{previousWorkspaceCount},
	})
	oldKey := workspaceReferenceRequestKey(uri, oldDeclarations[0].Range.Start, false, oldDeclarations[0].Kind, server.referenceGeneration, oldDeclarations[0].Name)
	server.storeWorkspaceReferenceCountLocked(oldKey, previousWorkspaceCount)
	server.mu.Unlock()

	insertOffset := strings.LastIndex(oldText, "%>")
	if insertOffset < 0 {
		t.Fatal("ASP closing delimiter missing")
	}
	changeRange := oldDocument.Range(insertOffset, insertOffset)
	updated := core.UpdateParsedDocument(oldParsed, []core.IncrementalChange{{
		Range: &changeRange, Text: "Response.Write SharedValue()\n",
	}}, core.Settings{DefaultLanguage: server.settings.DefaultLanguage})
	if !updated.Incremental || updated.Parsed.Text != newText {
		t.Fatalf("incremental reference edit = %#v, text=%q", updated, updated.Parsed.Text)
	}
	if !server.invalidateWorkspaceReferenceRevisionIncremental(oldParsed, updated.Parsed) {
		t.Fatal("incremental reference invalidation did not handle the edit")
	}
	server.mu.Lock()
	if stale := server.referenceBatch[referenceBatchCacheKey(uri, 0, server.referenceGeneration)]; stale != nil {
		server.mu.Unlock()
		t.Fatal("synchronous invalidation left the previous batch visible")
	}
	server.mu.Unlock()

	newDocument := core.NewTextDocument(uri, "classic-asp", 2, newText)
	server.workspace[uri] = newDocument
	newParsed := updated.Parsed
	newRevision := server.applyWorkspaceDocumentRevision(newDocument, newParsed)
	if newRevision.Manifest == nil || newRevision.Stale {
		t.Fatalf("changed workspace revision = %#v", newRevision)
	}
	newDeclarations := server.workspaceReferenceCodeLensPlan(newParsed).declarations
	states := server.snapshotWorkspaceReferenceCodeLensCounts(newParsed, newDeclarations)
	if len(states) != 1 || !states[0].final || states[0].count != previousWorkspaceCount+1 {
		t.Fatalf("document-delta count states = %#v, want final count %d", states, previousWorkspaceCount+1)
	}
	server.mu.Lock()
	batch := server.referenceBatch[referenceBatchCacheKey(uri, 0, server.referenceGeneration)]
	server.mu.Unlock()
	if !workspaceReferenceBatchCountsMatch(batch, newDeclarations) {
		t.Fatalf("document-delta batch was not aligned to the current revision: %#v", batch)
	}
	lenses := server.codeLens(uri)
	if len(lenses) != 1 || lenses[0].Command == nil || lenses[0].Command.Title != "8 references" {
		t.Fatalf("edited document CodeLens did not use the delta count immediately: %#v", lenses)
	}
	server.mu.Lock()
	if currentBatch := server.referenceBatch[referenceBatchCacheKey(uri, 0, server.referenceGeneration)]; currentBatch != batch {
		server.mu.Unlock()
		t.Fatal("edited document CodeLens scheduled a workspace batch instead of reusing its document delta")
	}
	server.mu.Unlock()

	renamedText := `<%
Function RenamedValue()
  RenamedValue = 1
End Function
Response.Write RenamedValue()
%>`
	renamedDocument := core.NewTextDocument(uri, "classic-asp", 3, renamedText)
	server.workspace[uri] = renamedDocument
	renamedParsed := server.parseTextDocument(renamedDocument, server.settings.DefaultLanguage)
	server.captureWorkspaceReferenceDocumentCountReuse(newParsed, renamedParsed)
	server.invalidateWorkspaceReferenceNames(newParsed, renamedParsed, []string{"sharedvalue", "renamedvalue"})
	renamedRevision := server.applyWorkspaceDocumentRevision(renamedDocument, renamedParsed)
	if renamedRevision.Manifest == nil || renamedRevision.Stale {
		t.Fatalf("renamed workspace revision = %#v", renamedRevision)
	}
	renamedDeclarations := server.workspaceReferenceCodeLensPlan(renamedParsed).declarations
	renamedStates := server.snapshotWorkspaceReferenceCodeLensCounts(renamedParsed, renamedDeclarations)
	if len(renamedStates) != 1 || renamedStates[0].final {
		t.Fatalf("public declaration change reused a stale count: %#v", renamedStates)
	}
}

func TestWorkspaceReferenceDocumentCountReusePublishesFromLatestArtifact(t *testing.T) {
	const uri = "file:///workspace/document-delta-async.asp"
	oldText := `<%
Function SharedValue()
  SharedValue = 1
End Function
Response.Write SharedValue()
%>`
	newText := `<%
Function SharedValue()
  SharedValue = 1
End Function
Response.Write SharedValue()
Response.Write SharedValue()
%>`
	server := New(nil, io.Discard, io.Discard)
	t.Cleanup(server.shutdownRuntimeCaches)
	oldDocument := core.NewTextDocument(uri, "classic-asp", 1, oldText)
	oldParsed := server.parseTextDocument(oldDocument, server.settings.DefaultLanguage)
	server.workspace[uri] = oldDocument
	if revision := server.applyWorkspaceDocumentRevision(oldDocument, oldParsed); revision.Manifest == nil || revision.Stale {
		t.Fatalf("initial workspace revision = %#v", revision)
	}
	oldDeclarations := server.workspaceReferenceCodeLensPlan(oldParsed).declarations
	if len(oldDeclarations) != 1 {
		t.Fatalf("initial declarations = %#v", oldDeclarations)
	}
	done := make(chan struct{})
	close(done)
	server.mu.Lock()
	server.storeWorkspaceReferenceBatchLocked(referenceBatchCacheKey(uri, 0, server.referenceGeneration), &workspaceReferenceBatchState{
		generation: server.referenceGeneration, done: done, complete: true,
		nameRevisions: map[string]uint64{"sharedvalue": server.referenceNameRevisions["sharedvalue"]},
		declarations:  append([]vbUsageDeclaration(nil), oldDeclarations...), finalCounts: []int{7},
	})
	server.mu.Unlock()

	insertOffset := strings.LastIndex(oldText, "%>")
	changeRange := oldDocument.Range(insertOffset, insertOffset)
	updated := core.UpdateParsedDocument(oldParsed, []core.IncrementalChange{{
		Range: &changeRange, Text: "Response.Write SharedValue()\n",
	}}, core.Settings{DefaultLanguage: server.settings.DefaultLanguage})
	if !updated.Incremental || updated.Parsed.Text != newText {
		t.Fatalf("incremental reference edit = %#v, text=%q", updated, updated.Parsed.Text)
	}
	if !server.invalidateWorkspaceReferenceRevisionIncremental(oldParsed, updated.Parsed) {
		t.Fatal("incremental reference invalidation did not handle the edit")
	}

	newDocument := core.NewTextDocument(uri, "classic-asp", 2, newText)
	server.mu.Lock()
	server.rememberOpenDocumentLocked(uri, newDocument)
	server.mu.Unlock()
	server.scheduleDocumentChangeAnalysis(newDocument, updated.Parsed)
	waitForDocumentOpenAnalysisWorkers(t, server)

	manifest := workspaceArtifactForURI(server, uri)
	if manifest == nil || manifest.SourceFingerprint != workspaceFingerprint(newText) {
		t.Fatalf("latest workspace artifact = %#v", manifest)
	}
	declarations := server.workspaceReferenceCodeLensPlan(updated.Parsed).declarations
	states := server.snapshotWorkspaceReferenceCodeLensCounts(updated.Parsed, declarations)
	if len(states) != 1 || !states[0].final || states[0].count != 8 {
		t.Fatalf("latest artifact reference count states = %#v, want final count 8", states)
	}
	server.mu.Lock()
	batch := server.referenceBatch[referenceBatchCacheKey(uri, 0, server.referenceGeneration)]
	server.mu.Unlock()
	if !workspaceReferenceBatchCountsMatch(batch, declarations) || batch.finalCounts[0] != 8 {
		t.Fatalf("latest artifact batch = %#v, want aligned final count 8", batch)
	}
}

func BenchmarkWorkspaceReferenceDocumentCountReuse(b *testing.B) {
	const uri = "file:///bench/document-delta.asp"
	texts := [...]string{
		`<% Function SharedValue() : SharedValue = 1 : End Function : Response.Write SharedValue() %>`,
		`<% Function SharedValue() : SharedValue = 1 : End Function : Response.Write SharedValue() : Response.Write SharedValue() %>`,
	}
	server := New(nil, io.Discard, io.Discard)
	var previous *core.ParsedDocument
	apply := func(version int) *core.ParsedDocument {
		document := core.NewTextDocument(uri, "classic-asp", version, texts[version&1])
		server.workspace[uri] = document
		parsed := server.parseTextDocument(document, server.settings.DefaultLanguage)
		if previous != nil {
			server.captureWorkspaceReferenceDocumentCountReuse(previous, parsed)
			server.invalidateWorkspaceReferenceNames(previous, parsed, []string{"sharedvalue"})
		}
		server.applyWorkspaceDocumentRevision(document, parsed)
		previous = parsed
		return parsed
	}
	parsed := apply(0)
	declarations := server.workspaceReferenceCodeLensPlan(parsed).declarations
	done := make(chan struct{})
	close(done)
	server.mu.Lock()
	server.storeWorkspaceReferenceBatchLocked(referenceBatchCacheKey(uri, 0, server.referenceGeneration), &workspaceReferenceBatchState{
		generation: server.referenceGeneration, done: done, complete: true,
		nameRevisions: map[string]uint64{"sharedvalue": server.referenceNameRevisions["sharedvalue"]},
		declarations:  append([]vbUsageDeclaration(nil), declarations...), finalCounts: []int{1},
	})
	server.mu.Unlock()

	b.ReportAllocs()
	b.ResetTimer()
	version := 0
	for b.Loop() {
		version++
		parsed = apply(version)
		states := server.snapshotWorkspaceReferenceCodeLensCounts(parsed, server.workspaceReferenceCodeLensPlan(parsed).declarations)
		if len(states) != 1 || !states[0].final {
			b.Fatalf("document-delta reuse missed at version %d: %#v", version, states)
		}
	}
}
