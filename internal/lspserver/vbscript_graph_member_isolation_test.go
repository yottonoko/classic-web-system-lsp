package lspserver

import (
	"context"
	"io"
	"os"
	"path/filepath"
	"reflect"
	"strings"
	"testing"

	"github.com/yottonoko/classic-web-system-lsp/internal/core"
	"github.com/yottonoko/classic-web-system-lsp/internal/graph"
	"github.com/yottonoko/classic-web-system-lsp/internal/lsp"
)

func TestConfiguredGraphMemberResolutionIsDocumentAndOrderIndependent(t *testing.T) {
	server := graphMemberIsolationServer()
	first := core.ParseDocument("file:///site/a.asp", `<%
' @type service As First
Dim service
service.shared
%>`, core.Settings{DefaultLanguage: "VBScript"})
	second := core.ParseDocument("file:///site/b.asp", `<%
' @type service As Second
Dim service
service.shared
%>`, core.Settings{DefaultLanguage: "VBScript"})

	nodes, edges := server.graphMemberReferencesWithProgress(context.Background(), []*core.ParsedDocument{first, second}, nil, "graph.test")
	reverseNodes, reverseEdges := server.graphMemberReferencesWithProgress(context.Background(), []*core.ParsedDocument{second, first}, nil, "graph.test")
	if !reflect.DeepEqual(nodes, reverseNodes) || !reflect.DeepEqual(edges, reverseEdges) {
		t.Fatalf("reversed graph member output differs:\nfirst nodes=%#v edges=%#v\nreverse nodes=%#v edges=%#v", nodes, edges, reverseNodes, reverseEdges)
	}
	assertConfiguredMemberSource(t, nodes, edges, "First.shared", first.URI)
	assertConfiguredMemberSource(t, nodes, edges, "Second.shared", second.URI)
	if hasConfiguredMemberSource(t, nodes, edges, "First.shared", second.URI) || hasConfiguredMemberSource(t, nodes, edges, "Second.shared", first.URI) {
		t.Fatalf("same-name document member leaked across owners: nodes=%#v edges=%#v", nodes, edges)
	}
}

func TestConfiguredGraphMemberResolutionPrefersProcedureLocalOverGlobal(t *testing.T) {
	server := graphMemberIsolationServer()
	parsed := core.ParseDocument("file:///site/shadows.asp", `<%
' @type service As First
Dim service
Sub Use()
  ' @type service As Second
  Dim service
  service.shared
End Sub
service.shared
%>`, core.Settings{DefaultLanguage: "VBScript"})

	nodes, edges := server.graphMemberReferencesWithProgress(context.Background(), []*core.ParsedDocument{parsed}, nil, "graph.test")
	assertConfiguredMemberSource(t, nodes, edges, "First.shared", parsed.URI)
	assertConfiguredMemberSource(t, nodes, edges, "Second.shared", parsed.URI)
	if count := configuredMemberEdgeCountForSource(nodes, edges, "First.shared", graphDeclarationNodeID(parsed.URI, "Use", lspRangeForName(t, parsed, "Use"))); count != 0 {
		t.Fatalf("procedure-local usage linked to global member: count=%d nodes=%#v edges=%#v", count, nodes, edges)
	}
}

func TestConfiguredGraphMemberResolutionKeepsIncludeOwnersIsolated(t *testing.T) {
	root := t.TempDir()
	server := graphMemberIsolationServer()
	server.rootPath = root
	server.rootURI = filePathURI(root)
	server.workspaceRoots = []workspaceRoot{{Path: root, URI: server.rootURI}}
	firstPath := filepath.Join(root, "first.asp")
	secondPath := filepath.Join(root, "second.asp")
	sharedPath := filepath.Join(root, "shared.inc")
	firstSource := `<%
' @type service As First
Dim service
%>
<!-- #include file="shared.inc" -->`
	secondSource := `<%
' @type service As Second
Dim service
%>
<!-- #include file="shared.inc" -->`
	sharedSource := `<% service.shared %>`
	for path, source := range map[string]string{firstPath: firstSource, secondPath: secondSource, sharedPath: sharedSource} {
		if err := os.WriteFile(path, []byte(source), 0o600); err != nil {
			t.Fatal(err)
		}
	}
	first := core.ParseDocument(filePathURI(firstPath), firstSource, core.Settings{DefaultLanguage: "VBScript"})
	second := core.ParseDocument(filePathURI(secondPath), secondSource, core.Settings{DefaultLanguage: "VBScript"})
	shared := core.ParseDocument(filePathURI(sharedPath), sharedSource, core.Settings{DefaultLanguage: "VBScript"})
	server.documents[first.URI] = core.NewTextDocument(first.URI, "classic-asp", 1, firstSource)
	server.documents[second.URI] = core.NewTextDocument(second.URI, "classic-asp", 1, secondSource)
	server.documents[shared.URI] = core.NewTextDocument(shared.URI, "classic-asp", 1, sharedSource)

	nodes, edges := server.graphMemberReferencesWithProgress(context.Background(), []*core.ParsedDocument{shared, first, second}, nil, "graph.test")
	reverseNodes, reverseEdges := server.graphMemberReferencesWithProgress(context.Background(), []*core.ParsedDocument{second, first, shared}, nil, "graph.test")
	if !reflect.DeepEqual(nodes, reverseNodes) || !reflect.DeepEqual(edges, reverseEdges) {
		t.Fatalf("reversed include-owner graph differs:\nfirst nodes=%#v edges=%#v\nreverse nodes=%#v edges=%#v", nodes, edges, reverseNodes, reverseEdges)
	}
	assertConfiguredMemberSource(t, nodes, edges, "First.shared", shared.URI)
	assertConfiguredMemberSource(t, nodes, edges, "Second.shared", shared.URI)
}

func graphMemberIsolationServer() *Server {
	server := New(strings.NewReader(""), io.Discard, io.Discard)
	server.settings.VBScriptGlobals = map[string]vbscriptGlobalSetting{}
	server.settings.VBScriptComTypes = map[string]vbscriptComTypeSetting{
		"First":  {Members: map[string]vbscriptComMemberSetting{"shared": {Kind: "method"}}},
		"Second": {Members: map[string]vbscriptComMemberSetting{"shared": {Kind: "method"}}},
	}
	return server
}

func assertConfiguredMemberSource(t *testing.T, nodes []graph.Node, edges []graph.Edge, label, sourceURI string) {
	t.Helper()
	var target string
	for _, node := range nodes {
		if node.ExternalKind == "member" && node.Label == label {
			target = node.ID
			break
		}
	}
	if target == "" {
		t.Fatalf("configured member %s missing: nodes=%#v edges=%#v", label, nodes, edges)
	}
	for _, edge := range edges {
		if edge.Target == target && (edge.Source == sourceURI || strings.HasPrefix(edge.Source, sourceURI+"#symbol:")) {
			return
		}
	}
	t.Fatalf("configured member %s has no source %s edge: edges=%#v", label, sourceURI, edges)
}

func hasConfiguredMemberSource(t *testing.T, nodes []graph.Node, edges []graph.Edge, label, sourceURI string) bool {
	t.Helper()
	for _, node := range nodes {
		if node.ExternalKind != "member" || node.Label != label {
			continue
		}
		for _, edge := range edges {
			if edge.Target == node.ID && edge.Source == sourceURI {
				return true
			}
		}
	}
	return false
}

func configuredMemberEdgeCountForSource(nodes []graph.Node, edges []graph.Edge, label, source string) int {
	for _, node := range nodes {
		if node.ExternalKind != "member" || node.Label != label {
			continue
		}
		for _, edge := range edges {
			if edge.Target == node.ID && edge.Source == source {
				return edge.Count
			}
		}
	}
	return 0
}

func lspRangeForName(t *testing.T, parsed *core.ParsedDocument, name string) lsp.Range {
	t.Helper()
	index := strings.Index(parsed.Text, name)
	if index < 0 {
		t.Fatalf("name %q missing", name)
	}
	return core.NewTextDocument(parsed.URI, "classic-asp", 0, parsed.Text).Range(index, index+len(name))
}
