package lspserver

import (
	"os"
	"path/filepath"
	"strconv"
	"strings"
	"testing"

	"github.com/yottonoko/classic-web-system-lsp/internal/graph"
	workspacepkg "github.com/yottonoko/classic-web-system-lsp/internal/workspace"
)

func TestStdioParityBuildsWorkspaceGraphFromUnopenedASPFiles(t *testing.T) {
	client := startStdioTestClient(t)
	defer client.close()

	root := t.TempDir()
	writeWorkspaceGraphFixture(t, filepath.Join(root, "default.asp"), `<!-- #include file="common.inc" -->
<%
Sub PageEntry()
End Sub
%>`)
	writeWorkspaceGraphFixture(t, filepath.Join(root, "common.inc"), `<%
Function SharedEntry()
End Function
%>`)
	initializeAndWaitForWorkspaceIndex(t, client, map[string]any{
		"processId":    nil,
		"rootUri":      pathToFileURI(root),
		"capabilities": map[string]any{},
	})

	payload := buildWorkspaceGraph(t, client, nil)
	if payload.Scope != "workspace" {
		t.Fatalf("workspace graph scope = %q: %s", payload.Scope, mustJSONText(t, payload))
	}
	for _, expected := range []struct {
		kind  string
		label string
	}{
		{"file", "default.asp"},
		{"file", "common.inc"},
		{"vbDeclaration", "PageEntry"},
		{"vbDeclaration", "SharedEntry"},
	} {
		if graphNodeMatching(payload, func(node graph.Node) bool {
			return node.Kind == expected.kind && node.Label == expected.label
		}) == nil {
			t.Fatalf("workspace graph missing %s %s: %s", expected.kind, expected.label, mustJSONText(t, payload))
		}
	}
	if !graphHasLinkMatching(payload, func(edge graph.Edge) bool { return edge.Kind == "include" }) ||
		payload.Stats["files"] == 0 {
		t.Fatalf("workspace graph missing include link or stats: %s", mustJSONText(t, payload))
	}
}

func TestStdioParityBuildsFolderGraphFromASPFilesUnderSelectedFolderOnly(t *testing.T) {
	client := startStdioTestClient(t)
	defer client.close()

	root := t.TempDir()
	appDir := filepath.Join(root, "app")
	sharedDir := filepath.Join(root, "shared")
	if err := os.MkdirAll(appDir, 0o755); err != nil {
		t.Fatal(err)
	}
	if err := os.MkdirAll(sharedDir, 0o755); err != nil {
		t.Fatal(err)
	}
	outsideInclude := filepath.Join(sharedDir, "common.inc")
	writeWorkspaceGraphFixture(t, filepath.Join(appDir, "default.asp"), `<!-- #include file="local.inc" -->
<!-- #include file="../shared/common.inc" -->
<%
Sub PageEntry()
End Sub
%>`)
	writeWorkspaceGraphFixture(t, filepath.Join(appDir, "local.inc"), `<%
Function InsideEntry()
End Function
%>`)
	writeWorkspaceGraphFixture(t, outsideInclude, `<%
Function OutsideEntry()
End Function
%>`)
	writeWorkspaceGraphFixture(t, filepath.Join(root, "sibling.asp"), `<%
Sub SiblingEntry()
End Sub
%>`)
	client.request("initialize", map[string]any{
		"processId":    nil,
		"rootUri":      pathToFileURI(root),
		"capabilities": map[string]any{},
	})

	payload := buildFolderGraph(t, client, pathToFileURI(appDir), nil)
	if payload.Scope != "folder" || payload.RootURI != pathToFileURI(appDir) {
		t.Fatalf("folder graph scope/root mismatch: %s", mustJSONText(t, payload))
	}
	for _, expected := range []struct {
		kind  string
		label string
	}{
		{"file", "default.asp"},
		{"file", "local.inc"},
		{"file", "common.inc"},
		{"vbDeclaration", "PageEntry"},
		{"vbDeclaration", "InsideEntry"},
	} {
		if graphNodeMatching(payload, func(node graph.Node) bool {
			return node.Kind == expected.kind && node.Label == expected.label
		}) == nil {
			t.Fatalf("folder graph missing %s %s: %s", expected.kind, expected.label, mustJSONText(t, payload))
		}
	}
	for _, absent := range []string{"sibling.asp", "OutsideEntry", "SiblingEntry"} {
		if strings.Contains(mustJSONText(t, payload.Nodes), absent) {
			t.Fatalf("folder graph unexpectedly included %s: %s", absent, mustJSONText(t, payload))
		}
	}
	if !graphHasLinkMatching(payload, func(edge graph.Edge) bool {
		return edge.Kind == "include" && edge.Include != nil && edge.Include.ResolvedURI == pathToFileURI(outsideInclude)
	}) || payload.Stats["files"] == 0 {
		t.Fatalf("folder graph missing outside include link or stats: %s", mustJSONText(t, payload))
	}
}

func TestStdioParityCanIncludeFilesThatDirectlyIncludeCurrentDocumentInDocumentGraphs(t *testing.T) {
	client := startStdioTestClient(t)
	defer client.close()

	root := t.TempDir()
	shared := filepath.Join(root, "Shared.INC")
	page := filepath.Join(root, "default.asp")
	writeWorkspaceGraphFixture(t, shared, `<%
Function SharedEntry()
End Function
%>`)
	writeWorkspaceGraphFixture(t, page, `<!-- #include file="sHaReD.InC" -->
<%
Sub PageEntry()
End Sub
%>`)
	sharedURI := pathToFileURI(shared)
	pageURI := pathToFileURI(page)
	initializeAndWaitForWorkspaceIndex(t, client, map[string]any{
		"processId":    nil,
		"rootUri":      pathToFileURI(root),
		"capabilities": map[string]any{},
	})

	defaultGraph := buildDocumentGraph(t, client, sharedURI, nil)
	if strings.Contains(mustJSONText(t, defaultGraph.Nodes), "PageEntry") {
		t.Fatalf("default document graph unexpectedly included PageEntry: %s", mustJSONText(t, defaultGraph))
	}
	notifyConfiguration(t, client, map[string]any{"aspLsp": map[string]any{
		"graph": map[string]any{"showIncomingDocumentIncludes": true},
	}})
	waitForWorkspaceIndexRefresh(t, client)
	incomingGraph := buildDocumentGraph(t, client, sharedURI, nil)
	expectGraphSetting(t, incomingGraph, "showIncomingDocumentIncludes", true)
	if graphNodeMatching(incomingGraph, func(node graph.Node) bool {
		return node.Kind == "file" && workspacepkg.SameFileIdentityURI(node.URI, pageURI)
	}) == nil || graphNodeMatching(incomingGraph, func(node graph.Node) bool {
		return node.Kind == "vbDeclaration" && node.Label == "PageEntry"
	}) == nil || !graphHasLinkMatching(incomingGraph, func(edge graph.Edge) bool {
		return edge.Kind == "include" && edge.Include != nil && workspacepkg.SameFileIdentityURI(edge.Include.ResolvedURI, sharedURI)
	}) {
		t.Fatalf("incoming document graph mismatch: %s", mustJSONText(t, incomingGraph))
	}
}

func TestStdioParityCanIncludeFilesThatDirectlyIncludeSelectedFolderFilesInFolderGraphs(t *testing.T) {
	client := startStdioTestClient(t)
	defer client.close()

	root := t.TempDir()
	appDir := filepath.Join(root, "app")
	parentDir := filepath.Join(root, "parent")
	if err := os.MkdirAll(appDir, 0o755); err != nil {
		t.Fatal(err)
	}
	if err := os.MkdirAll(parentDir, 0o755); err != nil {
		t.Fatal(err)
	}
	local := filepath.Join(appDir, "Local.INC")
	parent := filepath.Join(parentDir, "default.asp")
	writeWorkspaceGraphFixture(t, local, `<%
Function InsideEntry()
End Function
%>`)
	writeWorkspaceGraphFixture(t, parent, `<!-- #include file="../APP/lOcAl.InC" -->
<%
Sub ParentEntry()
End Sub
%>`)
	writeWorkspaceGraphFixture(t, filepath.Join(root, "sibling.asp"), `<%
Sub SiblingEntry()
End Sub
%>`)
	parentURI := pathToFileURI(parent)
	localURI := pathToFileURI(local)
	appURI := pathToFileURI(appDir)
	initializeAndWaitForWorkspaceIndex(t, client, map[string]any{
		"processId":    nil,
		"rootUri":      pathToFileURI(root),
		"capabilities": map[string]any{},
	})

	defaultGraph := buildFolderGraph(t, client, appURI, nil)
	if strings.Contains(mustJSONText(t, defaultGraph.Nodes), "ParentEntry") {
		t.Fatalf("default folder graph unexpectedly included ParentEntry: %s", mustJSONText(t, defaultGraph))
	}
	notifyConfiguration(t, client, map[string]any{"aspLsp": map[string]any{
		"graph": map[string]any{"showIncomingFolderIncludes": true},
	}})
	waitForWorkspaceIndexRefresh(t, client)
	incomingGraph := buildFolderGraph(t, client, appURI, nil)
	expectGraphSetting(t, incomingGraph, "showIncomingFolderIncludes", true)
	nodeText := mustJSONText(t, incomingGraph.Nodes)
	if graphNodeMatching(incomingGraph, func(node graph.Node) bool {
		return node.Kind == "file" && workspacepkg.SameFileIdentityURI(node.URI, parentURI)
	}) == nil || !strings.Contains(nodeText, "ParentEntry") || strings.Contains(nodeText, "SiblingEntry") ||
		!graphHasLinkMatching(incomingGraph, func(edge graph.Edge) bool {
			return edge.Kind == "include" && edge.Include != nil && workspacepkg.SameFileIdentityURI(edge.Include.ResolvedURI, localURI)
		}) {
		t.Fatalf("incoming folder graph mismatch: %s", mustJSONText(t, incomingGraph))
	}
}

func TestStdioParityAppliesWorkspacePatternsToFolderGraphCandidateFiles(t *testing.T) {
	client := startStdioTestClient(t)
	defer client.close()

	root := t.TempDir()
	appDir := filepath.Join(root, "app")
	generatedDir := filepath.Join(appDir, "generated")
	ignoredDir := filepath.Join(appDir, "ignored")
	if err := os.MkdirAll(generatedDir, 0o755); err != nil {
		t.Fatal(err)
	}
	if err := os.MkdirAll(ignoredDir, 0o755); err != nil {
		t.Fatal(err)
	}
	writeWorkspaceGraphFixture(t, filepath.Join(root, ".gitignore"), "app/ignored/\n")
	writeWorkspaceGraphFixture(t, filepath.Join(appDir, "default.asp"), `<%
Sub IncludedGraphEntry()
End Sub
%>`)
	writeWorkspaceGraphFixture(t, filepath.Join(appDir, "local.inc"), `<%
Function IncludedGraphHelper()
End Function
%>`)
	writeWorkspaceGraphFixture(t, filepath.Join(generatedDir, "generated.asp"), `<%
Sub GeneratedGraphEntry()
End Sub
%>`)
	writeWorkspaceGraphFixture(t, filepath.Join(ignoredDir, "ignored.asp"), `<%
Sub IgnoredGraphEntry()
End Sub
%>`)
	client.request("initialize", map[string]any{
		"processId":    nil,
		"rootUri":      pathToFileURI(root),
		"capabilities": map[string]any{},
	})
	notifyConfiguration(t, client, map[string]any{"aspLsp": map[string]any{
		"workspace": map[string]any{
			"includes":         []string{"app/**/*.asp", "app/**/*.inc"},
			"excludes":         []string{"app/generated/**"},
			"respectGitIgnore": true,
		},
	}})

	payload := buildFolderGraph(t, client, pathToFileURI(appDir), nil)
	serialized := mustJSONText(t, payload.Nodes)
	for _, expected := range []string{"IncludedGraphEntry", "IncludedGraphHelper"} {
		if !strings.Contains(serialized, expected) {
			t.Fatalf("folder graph missing %s: %s", expected, serialized)
		}
	}
	for _, unexpected := range []string{"GeneratedGraphEntry", "IgnoredGraphEntry"} {
		if strings.Contains(serialized, unexpected) {
			t.Fatalf("folder graph included %s despite workspace patterns: %s", unexpected, serialized)
		}
	}
}

func TestStdioParityGraphPayloadIgnoresRemovedNodeLimitSetting(t *testing.T) {
	client := startStdioTestClient(t)
	defer client.close()

	root := t.TempDir()
	uri := pathToFileURI(filepath.Join(root, "default.asp"))
	source := `<%
Sub A()
End Sub
Sub B()
End Sub
Sub C()
End Sub
Sub D()
End Sub
Sub E()
End Sub
Sub F()
End Sub
%>`
	client.request("initialize", map[string]any{
		"processId":    nil,
		"rootUri":      pathToFileURI(root),
		"capabilities": map[string]any{},
	})
	notifyConfiguration(t, client, map[string]any{"aspLsp": map[string]any{
		"graph": map[string]any{"maxNodes": 5},
	}})
	notifyOpenClassicASPDocument(t, client, uri, source)
	payload := buildDocumentGraph(t, client, uri, nil)
	if len(payload.Nodes) <= 5 ||
		graphNodeMatching(payload, func(node graph.Node) bool { return node.Kind == "file" && node.IsRoot }) == nil {
		t.Fatalf("complete graph payload mismatch: %s", mustJSONText(t, payload))
	}
}

func TestStdioParityDeduplicatesGraphFilesByNormalizedPathsWhenFileURIEncodingDiffers(t *testing.T) {
	client := startStdioTestClient(t)
	defer client.close()

	root := t.TempDir()
	page := filepath.Join(root, "default page.asp")
	canonicalPageURI := pathToFileURI(page)
	openPageURI := strings.ReplaceAll(canonicalPageURI, "%20", " ")
	if openPageURI == canonicalPageURI {
		t.Fatalf("test setup expected different URI spellings, got %s", openPageURI)
	}
	writeWorkspaceGraphFixture(t, page, `<%
Sub DiskOnly()
End Sub
%>`)
	client.request("initialize", map[string]any{
		"processId":    nil,
		"rootUri":      pathToFileURI(root),
		"capabilities": map[string]any{},
	})
	notifyOpenClassicASPDocument(t, client, openPageURI, `<%
Sub OpenOnly()
End Sub
%>`)

	documentGraph := buildDocumentGraph(t, client, canonicalPageURI, nil)
	if documentGraph.Scope != "document" || !workspacepkg.SameFileIdentityURI(documentGraph.RootURI, canonicalPageURI) ||
		len(graphFileNodesByLabel(documentGraph, "default page.asp")) != 1 ||
		!workspacepkg.SameFileIdentityURI(graphFileNodesByLabel(documentGraph, "default page.asp")[0].URI, canonicalPageURI) ||
		!graphFileNodesByLabel(documentGraph, "default page.asp")[0].IsRoot ||
		graphNodeByLabel(documentGraph, "OpenOnly") == nil ||
		graphNodeByLabel(documentGraph, "DiskOnly") != nil {
		t.Fatalf("document graph URI dedupe mismatch: %s", mustJSONText(t, documentGraph))
	}
	workspaceGraph := buildWorkspaceGraph(t, client, nil)
	if len(graphFileNodesByLabel(workspaceGraph, "default page.asp")) != 1 ||
		!workspacepkg.SameFileIdentityURI(graphFileNodesByLabel(workspaceGraph, "default page.asp")[0].URI, canonicalPageURI) ||
		graphNodeByLabel(workspaceGraph, "OpenOnly") == nil ||
		graphNodeByLabel(workspaceGraph, "DiskOnly") != nil {
		t.Fatalf("workspace graph URI dedupe mismatch: %s", mustJSONText(t, workspaceGraph))
	}
}

func TestStdioParityDocumentGraphsIncludeCompleteDeepIncludeTree(t *testing.T) {
	client := startStdioTestClient(t)
	defer client.close()

	root := t.TempDir()
	for index := 1; index <= 6; index++ {
		nextInclude := ""
		if index < 6 {
			nextInclude = `<!-- #include file="child-` + strconv.Itoa(index+1) + `.inc" -->` + "\n"
		}
		writeWorkspaceGraphFixture(t, filepath.Join(root, "child-"+strconv.Itoa(index)+".inc"), nextInclude+`<%
Function Included`+strconv.Itoa(index)+`()
End Function
%>`)
	}
	page := filepath.Join(root, "default.asp")
	writeWorkspaceGraphFixture(t, page, `<!-- #include file="child-1.inc" -->
<% Dim RootValue %>`)
	uri := pathToFileURI(page)
	client.request("initialize", map[string]any{
		"processId":    nil,
		"rootUri":      pathToFileURI(root),
		"capabilities": map[string]any{},
	})

	payload := buildDocumentGraph(t, client, uri, map[string]any{"includeTreeMaxDocuments": 4})
	if graphNodeByLabel(payload, "Included1") == nil || graphNodeByLabel(payload, "Included6") == nil {
		t.Fatalf("complete include tree mismatch: %s", mustJSONText(t, payload))
	}
}

func TestStdioParityTraversesCompleteDocumentIncludeTrees(t *testing.T) {
	client := startStdioTestClient(t)
	defer client.close()

	root := t.TempDir()
	page := filepath.Join(root, "default.asp")
	writeWorkspaceGraphFixture(t, page, `<!-- #include file="first.inc" -->
<!-- #include file="second.inc" -->
<%
Sub RootEntry()
End Sub
%>`)
	writeWorkspaceGraphFixture(t, filepath.Join(root, "first.inc"), `<!-- #include file="first-child.inc" -->
<%
Sub FirstEntry()
End Sub
%>`)
	writeWorkspaceGraphFixture(t, filepath.Join(root, "first-child.inc"), `<%
Sub FirstChildEntry()
End Sub
%>`)
	writeWorkspaceGraphFixture(t, filepath.Join(root, "second.inc"), `<%
Sub SecondEntry()
End Sub
%>`)
	uri := pathToFileURI(page)
	client.request("initialize", map[string]any{
		"processId":    nil,
		"rootUri":      pathToFileURI(root),
		"capabilities": map[string]any{},
	})

	payload := buildDocumentGraph(t, client, uri, map[string]any{"includeTreeMaxDocuments": 3})
	fileLabels := graphLabelsByKind(payload, "file")
	declarationLabels := graphLabelsByKind(payload, "vbDeclaration")
	if !fileLabels["default.asp"] ||
		!fileLabels["first.inc"] ||
		!fileLabels["first-child.inc"] ||
		!fileLabels["second.inc"] ||
		!declarationLabels["FirstEntry"] ||
		!declarationLabels["FirstChildEntry"] ||
		!declarationLabels["SecondEntry"] {
		t.Fatalf("complete include tree traversal mismatch: %s", mustJSONText(t, payload))
	}
}

func buildFolderGraph(t *testing.T, client *stdioTestClient, uri string, extra map[string]any) graph.Payload {
	t.Helper()
	arg := map[string]any{"scope": "folder", "uri": uri}
	for key, value := range extra {
		arg[key] = value
	}
	response := client.request("workspace/executeCommand", map[string]any{
		"command":   "aspLsp.server.buildGraph",
		"arguments": []map[string]any{arg},
	})
	var payload graph.Payload
	mustDecodeResult(t, response.Result, &payload)
	return payload
}

func graphFileNodesByLabel(payload graph.Payload, label string) []graph.Node {
	nodes := []graph.Node{}
	for _, node := range payload.Nodes {
		if node.Kind == "file" && node.Label == label {
			nodes = append(nodes, node)
		}
	}
	return nodes
}

func graphLabelsByKind(payload graph.Payload, kind string) map[string]bool {
	labels := map[string]bool{}
	for _, node := range payload.Nodes {
		if node.Kind == kind {
			labels[node.Label] = true
		}
	}
	return labels
}

func writeWorkspaceGraphFixture(t *testing.T, path string, text string) {
	t.Helper()
	if err := os.WriteFile(path, []byte(text), 0o644); err != nil {
		t.Fatal(err)
	}
}
