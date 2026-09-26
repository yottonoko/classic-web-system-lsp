package lspserver

import (
	"fmt"
	"io"
	"sync/atomic"
	"testing"

	"github.com/yottonoko/classic-web-system-lsp/internal/core"
)

func TestWorkspaceEmbeddedClassRenameUsesWarmExactCaseIndex(t *testing.T) {
	server := New(nil, io.Discard, nil)
	server.settings.WorkspaceSymbolRename = true
	sources := map[string]string{
		"file:///workspace/owner.asp":  `<div class="OldName"></div>`,
		"file:///workspace/script.asp": `<script>document.querySelector(".OldName")</script>`,
		"file:///workspace/style.asp":  `<style>.OldName {} .oldName {}</style>`,
	}
	documents := make([]*core.ParsedDocument, 0, len(sources))
	for uri, source := range sources {
		doc := core.NewTextDocument(uri, "classic-asp", 1, source)
		server.workspace[uri] = doc
		documents = append(documents, server.parseTextDocument(doc, server.settings.DefaultLanguage))
	}
	server.referenceWorkspaceIndex.update(documents)
	var rebuilt atomic.Int64
	server.referenceWorkspaceIndex.mu.Lock()
	server.referenceWorkspaceIndex.embeddedBuildTestHook = func(string) { rebuilt.Add(1) }
	server.referenceWorkspaceIndex.mu.Unlock()

	owner := server.parseTextDocument(server.workspace["file:///workspace/owner.asp"], server.settings.DefaultLanguage)
	changes := server.embeddedClassRenameChanges(owner, "OldName", "NewName")
	if got := rebuilt.Load(); got != 0 {
		t.Fatalf("warm rename rebuilt %d embedded documents, want 0", got)
	}
	if len(changes) != 3 {
		t.Fatalf("workspace rename changed %d documents, want 3: %#v", len(changes), changes)
	}
	for uri, edits := range changes {
		if len(edits) != 1 {
			t.Fatalf("%s edits = %d, want one exact-case match", uri, len(edits))
		}
	}
	ordered := server.referenceWorkspaceIndex.embeddedClassRanges("OldName", []*core.ParsedDocument{
		server.parseTextDocument(server.workspace["file:///workspace/style.asp"], server.settings.DefaultLanguage),
		owner,
		server.parseTextDocument(server.workspace["file:///workspace/script.asp"], server.settings.DefaultLanguage),
	})
	wantOrder := []string{"file:///workspace/style.asp", "file:///workspace/owner.asp", "file:///workspace/script.asp"}
	for index, item := range ordered {
		if item.URI != wantOrder[index] {
			t.Fatalf("ordered result %d URI = %s, want %s", index, item.URI, wantOrder[index])
		}
	}
}

func TestWorkspaceEmbeddedClassIndexUpdatesOneChangedDocument(t *testing.T) {
	index := newWorkspaceReferenceIndex()
	first := core.ParseDocument("file:///workspace/first.asp", `<div class="OldName"></div>`, core.Settings{DefaultLanguage: "VBScript"})
	second := core.ParseDocument("file:///workspace/second.asp", `<style>.OldName {}</style>`, core.Settings{DefaultLanguage: "VBScript"})
	index.update([]*core.ParsedDocument{first, second})
	var rebuilt atomic.Int64
	index.mu.Lock()
	index.embeddedBuildTestHook = func(string) { rebuilt.Add(1) }
	index.mu.Unlock()

	changed := core.ParseDocument(second.URI, `<style>.OldName {}</style><div class="OldName"></div>`, core.Settings{DefaultLanguage: "VBScript"})
	update := index.update([]*core.ParsedDocument{first, changed})
	if got := rebuilt.Load(); got != 1 {
		t.Fatalf("changed update rebuilt %d embedded documents, want 1", got)
	}
	if update.ChangedDocuments != 1 || len(update.EmbeddedAffectedNames) != 1 || update.EmbeddedAffectedNames[0] != "OldName" {
		t.Fatalf("embedded update = %#v, want one changed OldName segment", update)
	}
	ranges := index.embeddedClassRanges("OldName", []*core.ParsedDocument{first, changed})
	if len(ranges) != 2 || len(ranges[0].Ranges) != 1 || len(ranges[1].Ranges) != 2 {
		t.Fatalf("updated embedded ranges = %#v, want 1 then 2 ranges", ranges)
	}
}

func BenchmarkWorkspaceEmbeddedClassRangesWarm(b *testing.B) {
	const documentCount = 2_000
	documents := make([]*core.ParsedDocument, documentCount)
	for index := range documents {
		documents[index] = core.ParseDocument(
			fmt.Sprintf("file:///bench/embedded-rename/%04d.asp", index),
			`<style>.SharedClass {}</style><div class="SharedClass"></div>`,
			core.Settings{DefaultLanguage: "VBScript"},
		)
	}
	workspaceIndex := newWorkspaceReferenceIndex()
	workspaceIndex.update(documents)
	var rebuilt atomic.Int64
	workspaceIndex.mu.Lock()
	workspaceIndex.embeddedBuildTestHook = func(string) { rebuilt.Add(1) }
	workspaceIndex.mu.Unlock()
	b.ReportAllocs()
	b.ResetTimer()
	for b.Loop() {
		ranges := workspaceIndex.embeddedClassRanges("SharedClass", documents)
		if len(ranges) != documentCount {
			b.Fatalf("matching documents = %d, want %d", len(ranges), documentCount)
		}
	}
	b.StopTimer()
	if got := rebuilt.Load(); got != 0 {
		b.Fatalf("warm queries rebuilt %d embedded documents, want 0", got)
	}
}
