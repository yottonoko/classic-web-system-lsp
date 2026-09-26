package lspserver

import (
	"bytes"
	"io"
	"os"
	"path/filepath"
	"strings"
	"testing"

	"github.com/yottonoko/classic-web-system-lsp/internal/core"
)

func TestWatchedIncludeDirectiveChangeRefreshesDependentWithoutPrebuiltGraph(t *testing.T) {
	root := t.TempDir()
	ownerPath := filepath.Join(root, "default.asp")
	includePath := filepath.Join(root, "shared.inc")
	ownerText := `<!-- #include file="shared.inc" -->`
	if err := os.WriteFile(ownerPath, []byte(ownerText), 0o644); err != nil {
		t.Fatal(err)
	}
	if err := os.WriteFile(includePath, []byte(`<% Const SharedValue = 1 %>`), 0o644); err != nil {
		t.Fatal(err)
	}
	var output bytes.Buffer
	server := New(strings.NewReader(""), &output, io.Discard)
	server.rootPath = root
	server.rootURI = filePathURI(root)
	server.workspaceRoots = []workspaceRoot{{Path: root, URI: filePathURI(root)}}
	server.configureFsGateway()
	owner := core.NewTextDocument(filePathURI(ownerPath), "classic-asp", 1, ownerText)
	server.documents[owner.URI] = owner
	if err := server.publishDiagnostics(owner.URI); err != nil {
		t.Fatal(err)
	}
	output.Reset()

	if err := os.WriteFile(includePath, []byte(`<!-- #include file="default.asp" -->`), 0o644); err != nil {
		t.Fatal(err)
	}
	if err := server.didChangeWatchedFiles(didChangeWatchedFilesParams{Changes: []fileEvent{{URI: filePathURI(includePath), Type: fileChangeChanged}}}); err != nil {
		t.Fatal(err)
	}
	if !strings.Contains(output.String(), "include.cycle") {
		t.Fatalf("dependent diagnostics were not refreshed after include directive change: %s", output.String())
	}
}

func TestWatchedIncludeInvalidatesOnlyChangedFileAndTransitiveDependents(t *testing.T) {
	root := t.TempDir()
	paths := map[string]string{
		"owner":     filepath.Join(root, "default.asp"),
		"middle":    filepath.Join(root, "middle.inc"),
		"leaf":      filepath.Join(root, "leaf.inc"),
		"unrelated": filepath.Join(root, "unrelated.asp"),
		"other":     filepath.Join(root, "other.inc"),
	}
	texts := map[string]string{
		"owner":     `<!-- #include file="middle.inc" --><% Response.Write LeafValue %>`,
		"middle":    `<!-- #include file="leaf.inc" -->`,
		"leaf":      `<% Const LeafValue = 1 %>`,
		"unrelated": `<!-- #include file="other.inc" --><% Response.Write OtherValue %>`,
		"other":     `<% Const OtherValue = 1 %>`,
	}
	for name, path := range paths {
		if err := os.WriteFile(path, []byte(texts[name]), 0o644); err != nil {
			t.Fatal(err)
		}
	}

	server := New(strings.NewReader(""), io.Discard, io.Discard)
	server.rootPath = root
	server.rootURI = filePathURI(root)
	server.workspaceRoots = []workspaceRoot{{Path: root, URI: filePathURI(root)}}
	server.settings.DiagnosticsDebounceMS = 0
	server.configureFsGateway()

	documents := make(map[string]*core.TextDocument, len(paths))
	parsedDocuments := make([]*core.ParsedDocument, 0, len(paths))
	for name, path := range paths {
		uri := filePathURI(path)
		doc := core.NewTextDocument(uri, "classic-asp", 0, texts[name])
		documents[name] = doc
		server.workspace[uri] = doc
		parsedDocuments = append(parsedDocuments, server.parseTextDocument(doc, server.settings.DefaultLanguage))
	}
	server.documents[documents["owner"].URI] = documents["owner"]
	server.documents[documents["unrelated"].URI] = documents["unrelated"]
	server.syncWorkspaceIncludeGraphCache(parsedDocuments)

	server.mu.Lock()
	ownerParsed := server.parsedCache[parsedDocumentCacheKey(documents["owner"].URI)].Parsed
	middleParsed := server.parsedCache[parsedDocumentCacheKey(documents["middle"].URI)].Parsed
	unrelatedParsed := server.parsedCache[parsedDocumentCacheKey(documents["unrelated"].URI)].Parsed
	server.mu.Unlock()
	oldLeafEntry, ok := server.workspaceIncludeGraph.Get(paths["leaf"])
	if !ok || oldLeafEntry.Source.ContentHash == "" {
		t.Fatalf("leaf content identity was not cached: %#v, %v", oldLeafEntry, ok)
	}
	affected, graphReady := server.includeInvalidationPaths(map[string]struct{}{paths["leaf"]: {}})
	if !graphReady {
		t.Fatal("include graph was not ready for targeted invalidation")
	}
	for _, name := range []string{"leaf", "middle", "owner"} {
		if _, ok := affected[paths[name]]; !ok {
			t.Fatalf("targeted invalidation omitted %s: %#v", name, affected)
		}
	}
	for _, name := range []string{"unrelated", "other"} {
		if _, ok := affected[paths[name]]; ok {
			t.Fatalf("targeted invalidation included unrelated %s: %#v", name, affected)
		}
	}

	reads := map[string]int{}
	parses := map[string]int{}
	server.mu.Lock()
	server.workspaceFileReadTestHook = func(path string) { reads[path]++ }
	server.documentParseTestHook = func(uri string) { parses[filepath.Clean(fileURIPath(uri))]++ }
	server.mu.Unlock()
	changedLeaf := `<% Const LeafRenamed = 2 %>`
	if err := os.WriteFile(paths["leaf"], []byte(changedLeaf), 0o644); err != nil {
		t.Fatal(err)
	}
	if err := server.didChangeWatchedFiles(didChangeWatchedFilesParams{Changes: []fileEvent{{
		URI:  filePathURI(paths["leaf"]),
		Type: fileChangeChanged,
	}}}); err != nil {
		t.Fatal(err)
	}

	if reads[paths["leaf"]] != 1 || len(reads) != 1 {
		t.Fatalf("workspace source reads = %#v; want changed include only", reads)
	}
	if parses[paths["leaf"]] != 1 || len(parses) != 1 {
		t.Fatalf("source parses = %#v; want changed include only", parses)
	}
	server.mu.Lock()
	gotOwner := server.parsedCache[parsedDocumentCacheKey(documents["owner"].URI)].Parsed
	gotMiddle := server.parsedCache[parsedDocumentCacheKey(documents["middle"].URI)].Parsed
	gotUnrelated := server.parsedCache[parsedDocumentCacheKey(documents["unrelated"].URI)].Parsed
	server.mu.Unlock()
	if gotOwner != ownerParsed || gotMiddle != middleParsed || gotUnrelated != unrelatedParsed {
		t.Fatalf("unchanged syntax cache was rebuilt: owner=%v middle=%v unrelated=%v", gotOwner != ownerParsed, gotMiddle != middleParsed, gotUnrelated != unrelatedParsed)
	}
	newLeafEntry, ok := server.workspaceIncludeGraph.Get(paths["leaf"])
	if !ok || newLeafEntry.Source.ContentHash == oldLeafEntry.Source.ContentHash {
		t.Fatalf("changed include identity was not refreshed: old=%#v new=%#v", oldLeafEntry, newLeafEntry)
	}
	if server.workspaceIncludeGraph.Size() != len(paths) {
		t.Fatalf("include graph size = %d; want %d entries preserved", server.workspaceIncludeGraph.Size(), len(paths))
	}
}
