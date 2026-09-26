package lspserver

import (
	"context"
	"io"
	"path/filepath"
	"strings"
	"testing"

	"github.com/yottonoko/classic-web-system-lsp/internal/core"
	"github.com/yottonoko/classic-web-system-lsp/internal/lsp"
)

func TestWorkspaceVBAutoIncludeCatalogIndexesExplicitTopLevelExportsByCaseInsensitivePrefix(t *testing.T) {
	server := New(strings.NewReader(""), io.Discard, io.Discard)
	first := core.NewTextDocument("file:///workspace/a.inc", "classic-asp", 0, `<%
Dim AlphaValue
Private Sub AlphaPrivate()
End Sub
ImplicitValue = 1
Class AlphaClass
  Public Function Render()
  End Function
End Class
%>`)
	second := core.NewTextDocument("file:///workspace/b.inc", "classic-asp", 0, `<%
Public Function alphaFunction()
End Function
%>`)
	server.workspace[first.URI] = first
	server.workspace[second.URI] = second

	if !server.rebuildWorkspaceVBAutoIncludeCatalog(context.Background(), 0) {
		t.Fatal("catalog generation was not published")
	}
	snapshot := server.workspaceVBAutoIncludeSnapshot()
	if !snapshot.Complete || snapshot.Generation != 0 {
		t.Fatalf("snapshot = %#v, want complete generation zero", snapshot)
	}
	exports := snapshot.ExportsForPrefix("ALPHA")
	if got := workspaceVBAutoIncludeExportNames(exports); strings.Join(got, ",") != "AlphaClass,alphaFunction,AlphaValue" {
		t.Fatalf("prefix exports = %#v", got)
	}
	for _, excluded := range []string{"AlphaPrivate", "ImplicitValue", "Render"} {
		if got := snapshot.Exports(excluded); len(got) != 0 {
			t.Fatalf("%s was indexed as a top-level export: %#v", excluded, got)
		}
	}
	class := snapshot.Exports("alphaclass")
	if len(class) != 1 || len(class[0].Members) != 1 || class[0].Members[0].Name != "Render" {
		t.Fatalf("class export members = %#v", class)
	}
	class[0].Members[0].Name = "mutated"
	if current := snapshot.Exports("AlphaClass"); current[0].Members[0].Name != "Render" {
		t.Fatalf("snapshot storage was mutable through query result: %#v", current)
	}
}

func TestWorkspaceVBAutoIncludeCatalogUpdatesDeletesAndRenamesIncrementally(t *testing.T) {
	server := New(strings.NewReader(""), io.Discard, io.Discard)
	oldURI := "file:///workspace/old.inc"
	newURI := "file:///workspace/new.inc"
	old := core.NewTextDocument(oldURI, "classic-asp", 0, `<% Function OldExport(): End Function %>`)
	server.workspace[oldURI] = old
	if !server.rebuildWorkspaceVBAutoIncludeCatalog(context.Background(), 0) {
		t.Fatal("initial catalog generation was not published")
	}

	changed := core.NewTextDocument(oldURI, "classic-asp", 1, `<% Function NewExport(): End Function %>`)
	server.workspace[oldURI] = changed
	if result := server.applyWorkspaceDocumentRevision(changed, server.parseTextDocument(changed, server.settings.DefaultLanguage)); result.Manifest == nil || result.Stale {
		t.Fatalf("changed revision = %#v", result)
	}
	if got := server.workspaceVBAutoIncludeSnapshot().Exports("OldExport"); len(got) != 0 {
		t.Fatalf("old export survived update: %#v", got)
	}
	if got := server.workspaceVBAutoIncludeSnapshot().Exports("newexport"); len(got) != 1 || got[0].URI != oldURI {
		t.Fatalf("changed export = %#v", got)
	}

	delete(server.workspace, oldURI)
	server.removeWorkspaceDocumentRevision(oldURI)
	if got := server.workspaceVBAutoIncludeSnapshot().Exports("NewExport"); len(got) != 0 {
		t.Fatalf("deleted export survived: %#v", got)
	}

	renamed := core.NewTextDocument(newURI, "classic-asp", 0, changed.Text)
	server.workspace[newURI] = renamed
	if result := server.applyWorkspaceDocumentRevision(renamed, server.parseTextDocument(renamed, server.settings.DefaultLanguage)); result.Manifest == nil || result.Stale {
		t.Fatalf("renamed revision = %#v", result)
	}
	if got := server.workspaceVBAutoIncludeSnapshot().Exports("NewExport"); len(got) != 1 || got[0].URI != newURI {
		t.Fatalf("renamed export = %#v", got)
	}
}

func TestWorkspaceVBAutoIncludeCatalogRestoresArtifactWithoutParsing(t *testing.T) {
	root := t.TempDir()
	cacheDirectory := filepath.Join(root, "cache")
	uri := filePathURI(filepath.Join(root, "shared.inc"))
	document := core.NewTextDocument(uri, "classic-asp", 0, `<% Public Function SharedValue(): End Function %>`)
	newServer := func() *Server {
		server := New(strings.NewReader(""), io.Discard, io.Discard)
		server.rootPath = root
		server.rootURI = filePathURI(root)
		server.workspaceRoots = []workspaceRoot{{URI: server.rootURI, Path: root}}
		server.settings.CacheEnabled = true
		server.settings.CacheDirectory = cacheDirectory
		server.settings.CacheTTLHours = 24
		server.settings.CacheMaxSizeMB = 16
		server.configureDiskAnalysisCache()
		server.workspace[uri] = document
		return server
	}

	first := newServer()
	if !first.rebuildWorkspaceVBAutoIncludeCatalog(context.Background(), 0) {
		t.Fatal("initial catalog generation was not published")
	}
	if err := first.diskCacheForUse().Flush(); err != nil {
		t.Fatal(err)
	}
	first.closeDiskAnalysisCache()

	second := newServer()
	if len(second.parsedCache) != 0 {
		t.Fatalf("unexpected parsed cache before restore: %#v", second.parsedCache)
	}
	if !second.rebuildWorkspaceVBAutoIncludeCatalog(context.Background(), 0) {
		t.Fatal("restored catalog generation was not published")
	}
	if len(second.parsedCache) != 0 {
		t.Fatalf("catalog restore parsed workspace files: %#v", second.parsedCache)
	}
	if got := second.workspaceVBAutoIncludeSnapshot().Exports("sharedvalue"); len(got) != 1 || got[0].URI != uri {
		t.Fatalf("restored export = %#v", got)
	}
	second.closeDiskAnalysisCache()

	third := newServer()
	defer third.closeDiskAnalysisCache()
	third.settings.DefaultLanguage = "JScript"
	if !third.rebuildWorkspaceVBAutoIncludeCatalog(context.Background(), 0) {
		t.Fatal("catalog generation with changed parser settings was not published")
	}
	if got := third.workspaceVBAutoIncludeSnapshot().Exports("SharedValue"); len(got) != 0 {
		t.Fatalf("stale VBScript export survived default-language change: %#v", got)
	}
	if len(third.parsedCache) != 0 {
		t.Fatalf("catalog fallback hydrated parsed cache: %#v", third.parsedCache)
	}
}

func TestWorkspaceVBAutoIncludeCatalogHidesIncompleteGeneration(t *testing.T) {
	server := New(strings.NewReader(""), io.Discard, io.Discard)
	document := core.NewTextDocument("file:///workspace/shared.inc", "classic-asp", 0, `<% Const SharedValue = 1 %>`)
	server.workspace[document.URI] = document
	if !server.rebuildWorkspaceVBAutoIncludeCatalog(context.Background(), 0) {
		t.Fatal("initial catalog generation was not published")
	}

	server.mu.Lock()
	server.workspaceIndexGeneration = 1
	server.markWorkspaceVBAutoIncludeCatalogIncompleteLocked(1)
	server.mu.Unlock()
	snapshot := server.workspaceVBAutoIncludeSnapshot()
	if snapshot.Complete || snapshot.Generation != 1 || len(snapshot.ExportsForPrefix("Shared")) != 0 {
		t.Fatalf("incomplete generation remained queryable: %#v", snapshot)
	}
}

func TestVBScriptAutoIncludeCompletionMarksIncompleteCatalog(t *testing.T) {
	server := New(strings.NewReader(""), io.Discard, io.Discard)
	server.settings.VBScriptAutoIncludes = true
	uri := "file:///workspace/default.asp"
	document := core.NewTextDocument(uri, "classic-asp", 1, "<% Shared %>")
	server.documents[uri] = document
	result := server.completion(context.Background(), uri, lsp.Position{Line: 0, Character: 9}, nil)
	if !result.IsIncomplete {
		t.Fatalf("completion = %#v, want incomplete while catalog generation is unavailable", result)
	}
}

func workspaceVBAutoIncludeExportNames(exports []WorkspaceVBAutoIncludeExport) []string {
	names := make([]string, len(exports))
	for index, export := range exports {
		names[index] = export.Name
	}
	return names
}
