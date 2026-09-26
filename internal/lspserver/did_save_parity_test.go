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

func TestDidSaveRefreshesWorkspaceIndexIncludeGraphAndRuntimeCaches(t *testing.T) {
	root := t.TempDir()
	cacheDir := filepath.Join(root, ".cache")
	ownerPath := filepath.Join(root, "default.asp")
	includePath := filepath.Join(root, "shared.inc")
	if err := os.WriteFile(includePath, []byte("<% Const SharedValue = 1 %>"), 0o644); err != nil {
		t.Fatal(err)
	}
	if err := os.WriteFile(ownerPath, []byte("old"), 0o644); err != nil {
		t.Fatal(err)
	}
	ownerURI := filePathURI(ownerPath)
	ownerText := `<!-- #include file="shared.inc" -->
<% Response.Write SharedValue %>`
	if err := os.WriteFile(ownerPath, []byte(ownerText), 0o644); err != nil {
		t.Fatal(err)
	}

	server := New(strings.NewReader(""), &bytes.Buffer{}, io.Discard)
	server.rootPath = root
	server.rootURI = filePathURI(root)
	server.workspaceRoots = []workspaceRoot{{URI: server.rootURI, Path: root}}
	server.settings.CacheEnabled = true
	server.settings.CacheDirectory = cacheDir
	server.configureDiskAnalysisCache()
	server.configureFsGateway()
	server.documents[ownerURI] = core.NewTextDocument(ownerURI, "classic-asp", 2, ownerText)
	if err := server.handleNotification(t.Context(), "textDocument/didSave", mustRaw(map[string]any{
		"textDocument": map[string]any{"uri": ownerURI},
	})); err != nil {
		t.Fatal(err)
	}

	server.mu.Lock()
	workspaceDoc := server.workspace[ownerURI]
	graph := server.workspaceIncludeGraph
	javascriptProject := server.javascriptProject
	server.mu.Unlock()
	if workspaceDoc == nil || workspaceDoc.Text != ownerText {
		t.Fatalf("saved workspace document = %#v, want saved text", workspaceDoc)
	}
	if graph == nil || graph.Size() == 0 {
		t.Fatalf("saved include graph was not refreshed: %#v", graph)
	}
	entry, ok := graph.Get(ownerPath)
	if !ok || len(entry.TargetFileNames) != 1 || filepath.Clean(entry.TargetFileNames[0]) != filepath.Clean(includePath) {
		t.Fatalf("saved include graph entry = %#v, %v; want %q", entry, ok, includePath)
	}
	if javascriptProject != nil {
		t.Fatal("Classic ASP save unexpectedly created a JavaScript project")
	}
	server.waitForAsyncDiskCacheWrites()
	server.mu.Lock()
	cached := server.documentStore.CachedDocumentForURI(ownerURI)
	server.mu.Unlock()
	if cached == nil || cached.Parsed == nil {
		t.Fatalf("saved document was not connected to DocumentStore: %#v", cached)
	}
	cache := server.diskCacheForUse()
	settingsKey := server.workspaceDiskSettingsKey()
	if err := cache.Flush(); err != nil {
		t.Fatal(err)
	}
	heads := cache.ReadDocumentHeadsAligned([]string{string(workspaceDocumentIDFromURI(ownerURI))})
	if len(heads) != 1 || heads[0] == nil {
		t.Fatal("didSave did not write document head cache")
	}
	if _, ok := cache.ReadWorkspaceIncludeGraph(settingsKey); !ok {
		t.Fatal("didSave did not write workspace include graph cache")
	}
}

func TestDidSaveReusesUnchangedParsedAndAnalysisCaches(t *testing.T) {
	root := t.TempDir()
	fileName := filepath.Join(root, "default.asp")
	text := `<% Dim Value : Value = 1 : Response.Write Value %>`
	if err := os.WriteFile(fileName, []byte(text), 0o644); err != nil {
		t.Fatal(err)
	}
	uri := filePathURI(fileName)
	doc := core.NewTextDocument(uri, "classic-asp", 3, text)
	server := New(strings.NewReader(""), io.Discard, io.Discard)
	server.rootPath = root
	server.rootURI = filePathURI(root)
	server.workspaceRoots = []workspaceRoot{{URI: server.rootURI, Path: root}}
	server.settings.CacheEnabled = true
	server.settings.CacheDirectory = filepath.Join(root, ".cache")
	server.configureDiskAnalysisCache()
	server.configureFsGateway()
	server.documents[uri] = doc
	parsed := server.parseTextDocument(doc, server.settings.DefaultLanguage)
	server.waitForAsyncDiskCacheWrites()
	server.resumeAsyncDiskCacheWrites()
	snapshot := server.cachedFileAnalysisSnapshot(parsed)
	server.rememberValidatedDocumentVersion(uri, doc.Version)
	if err := server.handleNotification(t.Context(), "textDocument/didSave", mustRaw(map[string]any{
		"textDocument": map[string]any{"uri": uri},
	})); err != nil {
		t.Fatal(err)
	}
	reused := server.parseTextDocument(doc, server.settings.DefaultLanguage)
	if reused != parsed {
		t.Fatal("unchanged didSave replaced the parsed document")
	}
	if server.cachedFileAnalysisSnapshot(reused) != snapshot {
		t.Fatal("unchanged didSave replaced the analysis snapshot")
	}
}
