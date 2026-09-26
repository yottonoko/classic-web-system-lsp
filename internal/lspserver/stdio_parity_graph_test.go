package lspserver

import (
	"os"
	"path/filepath"
	"strings"
	"testing"

	"github.com/yottonoko/classic-web-system-lsp/internal/graph"
)

func TestStdioParityBuildsGraphForActiveDocumentIncludeTreeAndVBIndex(t *testing.T) {
	client := startStdioTestClient(t)
	defer client.close()

	root := t.TempDir()
	page := filepath.Join(root, "default.asp")
	if err := os.WriteFile(filepath.Join(root, "common.inc"), []byte("<%\nConst IncludedValue = 1\n%>"), 0o644); err != nil {
		t.Fatal(err)
	}
	uri := pathToFileURI(page)
	source := `<!-- #include file="common.inc" -->
<!-- #include file="missing.inc" -->
<%
Dim PageValue, PageItems
Function Render(value)
  Dim flags
  ReDim Preserve PageItems(2)
  PageValue = value
  PageValue = &HFF
  PageValue = &077
  PageValue = &O10
  flags = value Xor PageValue
  flags = flags Eqv (PageValue Imp value)
  flags = flags And Not (PageValue Or value)
  flags = flags Mod 2
  Render = value
End Function
Sub Main()
  Render "x"
  MissingName
  MissingValue = PageValue
  Response.Write MissingValue
End Sub
%>`
	if err := os.WriteFile(page, []byte(source), 0o644); err != nil {
		t.Fatal(err)
	}
	client.request("initialize", map[string]any{
		"processId":    nil,
		"rootUri":      pathToFileURI(root),
		"capabilities": map[string]any{},
	})
	openClassicASPDocument(t, client, uri, source)

	response := client.request("workspace/executeCommand", map[string]any{
		"command": "aspLsp.server.buildGraph",
		"arguments": []map[string]any{{
			"scope": "document",
			"uri":   uri,
		}},
	})
	var payload graph.Payload
	mustDecodeResult(t, response.Result, &payload)
	responseJSON := mustJSONText(t, response.Result)

	if payload.Scope != "document" || payload.RootURI != uri {
		t.Fatalf("document graph metadata mismatch: %s", responseJSON)
	}
	if !graphHasNode(payload, func(node graph.Node) bool {
		return node.Kind == "file" && node.Label == "default.asp"
	}) {
		t.Fatalf("document graph missing default.asp file node: %s", responseJSON)
	}
	if !graphHasNode(payload, func(node graph.Node) bool {
		return node.Kind == "file" && node.Label == "default.asp" && node.IsRoot
	}) {
		t.Fatalf("document graph missing root default.asp file node: %s", responseJSON)
	}
	if !graphHasNode(payload, func(node graph.Node) bool {
		return node.Kind == "file" && node.Label == "common.inc"
	}) {
		t.Fatalf("document graph missing common.inc file node: %s", responseJSON)
	}
	if graphHasNode(payload, func(node graph.Node) bool {
		return node.Kind == "file" && node.Label == "common.inc" && node.IsRoot
	}) {
		t.Fatalf("document graph marked common.inc as root: %s", responseJSON)
	}
	if !graphHasNode(payload, func(node graph.Node) bool {
		return node.Kind == "missingInclude" && node.Exists != nil && !*node.Exists
	}) {
		t.Fatalf("document graph missing unresolved include node: %s", responseJSON)
	}

	renderNode := graphNode(payload, func(node graph.Node) bool {
		return node.Kind == "vbDeclaration" && node.Label == "Render"
	})
	missingNameNode := graphNode(payload, func(node graph.Node) bool {
		return node.Kind == "vbDeclaration" && node.Label == "MissingName"
	})
	missingValueNode := graphNode(payload, func(node graph.Node) bool {
		return node.Kind == "vbDeclaration" && node.Label == "MissingValue"
	})
	pageItemsNode := graphNode(payload, func(node graph.Node) bool {
		return node.Kind == "vbDeclaration" && node.Label == "PageItems"
	})
	if renderNode == nil {
		t.Fatalf("document graph missing Render node: %s", responseJSON)
	}
	if missingNameNode == nil || missingNameNode.DeclarationKind != "variable" || missingNameNode.BindingScope != "local" || !missingNameNode.Implicit || missingNameNode.ImplicitGlobal {
		t.Fatalf("document graph read-only procedure implicit node mismatch: %#v in %s", missingNameNode, responseJSON)
	}
	if missingValueNode == nil || missingValueNode.DeclarationKind != "variable" || missingValueNode.BindingScope != "global" || !missingValueNode.Implicit || !missingValueNode.ImplicitGlobal {
		t.Fatalf("document graph assigned implicit global node mismatch: %#v in %s", missingValueNode, responseJSON)
	}
	if strings.Contains(responseJSON, "implicitLocal") || strings.Contains(responseJSON, "unresolvedGlobal") {
		t.Fatalf("document graph leaked legacy implicit fields: %s", responseJSON)
	}
	if graphHasNode(payload, func(node graph.Node) bool {
		return node.Kind == "vbUnresolved" && node.Label == "MissingValue"
	}) {
		t.Fatalf("document graph emitted unresolved MissingValue: %s", responseJSON)
	}
	for _, label := range []string{"HFF", "O10", "Xor", "Eqv", "Imp", "And", "Not", "Or", "Mod"} {
		if graphHasNode(payload, func(node graph.Node) bool {
			return node.Kind == "vbUnresolved" && node.Label == label
		}) {
			t.Fatalf("document graph emitted unresolved VBScript token %s: %s", label, responseJSON)
		}
	}
	for _, kind := range []string{"include", "declares", "references", "assignments", "calls"} {
		if !graphHasEdge(payload, func(edge graph.Edge) bool { return edge.Kind == kind }) {
			t.Fatalf("document graph missing %s edge: %s", kind, responseJSON)
		}
	}
	if !graphHasEdge(payload, func(edge graph.Edge) bool {
		return edge.Kind == "assignments" && edge.Role == "write" && missingValueNode != nil && edge.Target == missingValueNode.ID
	}) {
		t.Fatalf("document graph missing MissingValue write edge: %s", responseJSON)
	}
	if !graphHasEdge(payload, func(edge graph.Edge) bool {
		return edge.Kind == "references" && edge.Role == "read" && missingValueNode != nil && edge.Target == missingValueNode.ID
	}) {
		t.Fatalf("document graph missing MissingValue read edge: %s", responseJSON)
	}
	if pageItemsNode == nil ||
		pageItemsNode.BindingScope != "global" ||
		pageItemsNode.TypeName != "Array" ||
		pageItemsNode.ArrayKind != "dynamic" ||
		pageItemsNode.ArrayDimensions == nil ||
		len(*pageItemsNode.ArrayDimensions) != 1 ||
		(*pageItemsNode.ArrayDimensions)[0] != "2" {
		t.Fatalf("document graph PageItems node mismatch: %#v in %s", pageItemsNode, responseJSON)
	}
	if !graphHasEdge(payload, func(edge graph.Edge) bool {
		return edge.Kind == "assignments" &&
			edge.Role == "write" &&
			renderNode != nil &&
			pageItemsNode != nil &&
			edge.Source == renderNode.ID &&
			edge.Target == pageItemsNode.ID
	}) {
		t.Fatalf("document graph missing Render -> PageItems write edge: %s", responseJSON)
	}
	if graphHasEdge(payload, func(edge graph.Edge) bool {
		return edge.Kind == "declares" &&
			renderNode != nil &&
			pageItemsNode != nil &&
			edge.Source == renderNode.ID &&
			edge.Target == pageItemsNode.ID
	}) {
		t.Fatalf("document graph incorrectly declares PageItems from Render: %s", responseJSON)
	}
	if graphHasEdge(payload, func(edge graph.Edge) bool {
		return edge.Kind == "assignments" &&
			edge.Role == "write" &&
			renderNode != nil &&
			edge.Source == renderNode.ID &&
			edge.Target == renderNode.ID
	}) {
		t.Fatalf("document graph incorrectly writes Render to itself: %s", responseJSON)
	}
	if graphHasEdge(payload, func(edge graph.Edge) bool { return edge.Kind == "unresolvedReference" }) {
		t.Fatalf("document graph emitted unresolvedReference edges: %s", responseJSON)
	}
	if payload.Stats["missingIncludes"] != 1 {
		t.Fatalf("document graph missingIncludes = %d, want 1: %s", payload.Stats["missingIncludes"], responseJSON)
	}
}

func TestStdioParityPreservesGraphIncludeModeRangesAndResolvedPathMetadata(t *testing.T) {
	client := startStdioTestClient(t)
	defer client.close()

	root := t.TempDir()
	page := filepath.Join(root, "default.asp")
	include := filepath.Join(root, "Shared.INC")
	writeGraphFixture(t, include, "<% Const IncludedValue = 1 %>")
	source := `<!-- #include virtual="shared.inc" -->
<% Response.Write IncludedValue %>`
	writeGraphFixture(t, page, source)
	uri := pathToFileURI(page)
	client.request("initialize", map[string]any{
		"processId": nil, "rootUri": pathToFileURI(root), "capabilities": map[string]any{},
	})
	openClassicASPDocument(t, client, uri, source)

	response := client.request("workspace/executeCommand", map[string]any{
		"command":   "aspLsp.server.buildGraph",
		"arguments": []map[string]any{{"scope": "document", "uri": uri}},
	})
	var payload graph.Payload
	mustDecodeResult(t, response.Result, &payload)
	edge := graphEdge(payload, func(edge graph.Edge) bool { return edge.Kind == "include" })
	if edge == nil || edge.Include == nil {
		t.Fatalf("graph include edge missing: %s", mustJSONText(t, response.Result))
	}
	if edge.Include.Mode != "virtual" || edge.Include.ResolvedURI != pathToFileURI(include) ||
		edge.Include.ActualPath != "Shared.INC" || edge.Include.PathCaseMatches {
		t.Fatalf("graph include metadata mismatch: %#v", edge.Include)
	}
	if len(edge.Ranges) != 1 || edge.Ranges[0].URI != uri || edge.Ranges[0].Range.Start.Line != 0 {
		t.Fatalf("graph include ranges mismatch: %#v", edge.Ranges)
	}
}

func TestStdioParityExpandsRelatedIncludeTreesBySettingAndForceMode(t *testing.T) {
	client := startStdioTestClient(t)
	defer client.close()

	root := t.TempDir()
	page := filepath.Join(root, "default.asp")
	cleanPage := filepath.Join(root, "clean.asp")
	writeGraphFixture(t, filepath.Join(root, "parent.asp"), `<!-- #include file="default.asp" -->
<!-- #include file="sibling.inc" -->`)
	writeGraphFixture(t, filepath.Join(root, "sibling.inc"), `<%
Sub SiblingDefinition()
End Sub
%>`)
	writeGraphFixture(t, filepath.Join(root, "child.inc"), "<%\nDim ChildValue\n%>")
	writeGraphFixture(t, filepath.Join(root, "child-parent.asp"), `<!-- #include file="child.inc" -->
<!-- #include file="child-sibling.inc" -->`)
	writeGraphFixture(t, filepath.Join(root, "child-sibling.inc"), `<%
Sub ChildSiblingDefinition()
End Sub
%>`)
	writeGraphFixture(t, filepath.Join(root, "clean-parent.asp"), `<!-- #include file="clean.asp" -->
<!-- #include file="clean-sibling.inc" -->`)
	writeGraphFixture(t, filepath.Join(root, "clean-sibling.inc"), `<%
Sub CleanSiblingDefinition()
End Sub
%>`)
	pageSource := `<!-- #include file="child.inc" -->
<%
MissingGlobal = 1
Call MissingProcedure()
%>`
	cleanSource := `<%
Dim CleanValue
CleanValue = 1
Response.Write CleanValue
%>`
	writeGraphFixture(t, page, pageSource)
	writeGraphFixture(t, cleanPage, cleanSource)
	uri := pathToFileURI(page)
	cleanURI := pathToFileURI(cleanPage)
	initializeAndWaitForWorkspaceIndex(t, client, map[string]any{
		"processId":    nil,
		"rootUri":      pathToFileURI(root),
		"capabilities": map[string]any{},
	})
	openClassicASPDocument(t, client, uri, pageSource)

	normalGraph := buildDocumentGraph(t, client, uri, map[string]any{
		"includeRelatedIncludeTreesForUnresolved": false,
	})
	if graphHasFileNode(normalGraph, "parent.asp") || graphHasFileNode(normalGraph, "sibling.inc") {
		t.Fatalf("normal graph included related include tree: %s", mustJSONText(t, normalGraph))
	}
	defaultGraph := buildDocumentGraph(t, client, uri, nil)
	if !graphHasFileNode(defaultGraph, "parent.asp") || !graphHasFileNode(defaultGraph, "sibling.inc") {
		t.Fatalf("default graph missing related include tree: %s", mustJSONText(t, defaultGraph))
	}
	relatedGraph := buildDocumentGraph(t, client, uri, map[string]any{
		"includeRelatedIncludeTreesForUnresolved": true,
	})
	if !graphHasFileNode(relatedGraph, "parent.asp") || !graphHasFileNode(relatedGraph, "sibling.inc") {
		t.Fatalf("related graph missing parent/sibling include tree: %s", mustJSONText(t, relatedGraph))
	}
	if graphHasFileNode(relatedGraph, "child-parent.asp") || graphHasFileNode(relatedGraph, "child-sibling.inc") {
		t.Fatalf("related graph included unrelated child include tree: %s", mustJSONText(t, relatedGraph))
	}
	cleanGraph := buildDocumentGraph(t, client, cleanURI, map[string]any{
		"includeRelatedIncludeTreesForUnresolved": true,
	})
	if graphHasFileNode(cleanGraph, "clean-parent.asp") || graphHasFileNode(cleanGraph, "clean-sibling.inc") {
		t.Fatalf("clean graph included related tree without unresolved symbols: %s", mustJSONText(t, cleanGraph))
	}
	forcedCleanGraph := buildDocumentGraph(t, client, cleanURI, map[string]any{
		"includeRelatedIncludeTreesForUnresolved": true,
		"forceRelatedIncludeTreeAnalysis":         true,
	})
	if !graphHasFileNode(forcedCleanGraph, "clean-parent.asp") || !graphHasFileNode(forcedCleanGraph, "clean-sibling.inc") {
		t.Fatalf("forced clean graph missing related tree: %s", mustJSONText(t, forcedCleanGraph))
	}
}

func TestStdioParityResolvesIncludedObjectVariableMemberAndDefaultMemberUsagesInGraph(t *testing.T) {
	client := startStdioTestClient(t)
	defer client.close()

	root := t.TempDir()
	common := filepath.Join(root, "common.inc")
	page := filepath.Join(root, "default.asp")
	commonSource := `<%
Dim db : Set db = CreateObject("Custom.Database")
%>`
	pageSource := `<!-- #include file="common.inc" -->
<%
Function Render()
  Dim localDb
  db.Open()
  Call db.Open()
  localDb.Open()
  Response.Write db("value")
End Function
%>`
	writeGraphFixture(t, common, commonSource)
	writeGraphFixture(t, page, pageSource)
	commonURI := pathToFileURI(common)
	pageURI := pathToFileURI(page)
	client.request("initialize", map[string]any{
		"processId":    nil,
		"rootUri":      pathToFileURI(root),
		"capabilities": map[string]any{},
	})
	openClassicASPDocumentWithDiagnostics(t, client, pageURI, pageSource)
	payload := buildDocumentGraph(t, client, pageURI, nil)
	responseJSON := mustJSONText(t, payload)
	renderNode := graphNodeByLabelAndURI(payload, "Render", pageURI)
	dbNode := graphNodeByLabelAndURI(payload, "db", commonURI)
	dbOpenNode := graphNodeByFullPathAndURI(payload, "db.Open", pageURI)
	localDbOpenNode := graphNodeByFullPathAndURI(payload, "localDb.Open", pageURI)

	if dbNode == nil ||
		dbNode.DeclarationKind != "variable" ||
		dbNode.BindingScope != "global" ||
		dbNode.TypeName != "Custom.Database" {
		t.Fatalf("db graph node mismatch: %#v in %s", dbNode, responseJSON)
	}
	if !graphHasLinkBetween(payload, "references", renderNode, dbNode, "") {
		t.Fatalf("graph missing Render -> db reference: %s", responseJSON)
	}
	expectMemberGraphNode(t, dbOpenNode, "Open", "db", "Open", "db.Open", responseJSON)
	expectMemberGraphNode(t, localDbOpenNode, "Open", "localDb", "Open", "localDb.Open", responseJSON)
	if !graphHasLinkBetween(payload, "calls", renderNode, dbOpenNode, "") {
		t.Fatalf("graph missing Render -> db.Open call: %s", responseJSON)
	}
	if !graphHasLinkBetween(payload, "calls", renderNode, localDbOpenNode, "") {
		t.Fatalf("graph missing Render -> localDb.Open call: %s", responseJSON)
	}
	for _, label := range []string{"db", "localDb", "Open"} {
		if graphHasNode(payload, func(node graph.Node) bool {
			return node.Kind == "vbUnresolved" && node.Label == label
		}) {
			t.Fatalf("graph emitted unresolved member token %s: %s", label, responseJSON)
		}
	}
	if graphHasEdge(payload, func(edge graph.Edge) bool {
		return edge.Kind == "unresolvedReference" && (edge.Label == "member" || edge.Role == "member")
	}) {
		t.Fatalf("graph emitted unresolved member edge: %s", responseJSON)
	}
}

func TestStdioParityConnectsChainedVBScriptMemberGraphNodesBackToTheirReceivers(t *testing.T) {
	client := startStdioTestClient(t)
	defer client.close()

	root := t.TempDir()
	page := filepath.Join(root, "default.asp")
	source := `<%
Function Render()
  Dim a
  a.b.c.d
End Function
%>`
	writeGraphFixture(t, page, source)
	uri := pathToFileURI(page)
	client.request("initialize", map[string]any{
		"processId":    nil,
		"rootUri":      pathToFileURI(root),
		"capabilities": map[string]any{},
	})
	openClassicASPDocument(t, client, uri, source)
	payload := buildDocumentGraph(t, client, uri, nil)
	responseJSON := mustJSONText(t, payload)
	aNode := graphNodeByLabelAndURI(payload, "a", uri)
	abNode := graphNodeByFullPathAndURI(payload, "a.b", uri)
	abcNode := graphNodeByFullPathAndURI(payload, "a.b.c", uri)
	abcdNode := graphNodeByFullPathAndURI(payload, "a.b.c.d", uri)
	if aNode == nil || aNode.DeclarationKind != "variable" || aNode.BindingScope != "local" {
		t.Fatalf("local a graph node mismatch: %#v in %s", aNode, responseJSON)
	}
	if abNode == nil || abNode.Kind != "vbMemberReference" || abNode.Label != "b" {
		t.Fatalf("a.b graph node mismatch: %#v in %s", abNode, responseJSON)
	}
	if abcNode == nil || abcNode.Kind != "vbMemberReference" || abcNode.Label != "c" {
		t.Fatalf("a.b.c graph node mismatch: %#v in %s", abcNode, responseJSON)
	}
	if abcdNode == nil || abcdNode.Kind != "vbMemberReference" || abcdNode.Label != "d" {
		t.Fatalf("a.b.c.d graph node mismatch: %#v in %s", abcdNode, responseJSON)
	}
	if !graphHasLinkBetween(payload, "calls", abcdNode, abcNode, "member") ||
		!graphHasLinkBetween(payload, "calls", abcNode, abNode, "member") ||
		!graphHasLinkBetween(payload, "calls", abNode, aNode, "member") {
		t.Fatalf("member chain graph links mismatch: %s", responseJSON)
	}
}

func TestStdioParityKeepsRegExpBuiltinsOutOfGraphWhilePreservingSourceDeclarationTypes(t *testing.T) {
	client := startStdioTestClient(t)
	defer client.close()

	root := t.TempDir()
	page := filepath.Join(root, "regexp.asp")
	uri := pathToFileURI(page)
	source := `<%
Option Explicit
Dim re, matches
Set re = New RegExp
re.Pattern = "\w+"
Set matches = re.Execute("abc")
%>`
	writeGraphFixture(t, page, source)
	client.request("initialize", map[string]any{
		"processId":    nil,
		"rootUri":      pathToFileURI(root),
		"capabilities": map[string]any{},
	})
	openClassicASPDocument(t, client, uri, source)
	payload := buildDocumentGraph(t, client, uri, nil)
	responseJSON := mustJSONText(t, payload)
	reNode := graphNode(payload, func(node graph.Node) bool {
		return node.Kind == "vbDeclaration" && node.Label == "re"
	})
	matchesNode := graphNode(payload, func(node graph.Node) bool {
		return node.Kind == "vbDeclaration" && node.Label == "matches"
	})
	if reNode == nil || reNode.Origin != "source" {
		t.Fatalf("RegExp source declaration node mismatch: %#v in %s", reNode, responseJSON)
	}
	if matchesNode == nil || matchesNode.Origin != "source" {
		t.Fatalf("matches source declaration node mismatch: %#v in %s", matchesNode, responseJSON)
	}
	if graphHasNode(payload, func(node graph.Node) bool {
		return strings.Contains(node.Label, "RegExp") || strings.Contains(node.FullPath, "RegExp")
	}) {
		t.Fatalf("graph included RegExp builtin node text: %s", responseJSON)
	}
	if graphHasNode(payload, func(node graph.Node) bool {
		return node.Origin == "builtin" && (node.Label == "RegExp" || node.Label == "RegExp.Execute")
	}) {
		t.Fatalf("graph included RegExp builtin nodes: %s", responseJSON)
	}
	if graphHasNode(payload, func(node graph.Node) bool {
		return node.Kind == "vbUnresolved" && (node.Label == "RegExp" || node.Label == "Execute")
	}) {
		t.Fatalf("graph emitted unresolved RegExp nodes: %s", responseJSON)
	}
}

func TestStdioParityIncludesEditorInferredDeclarationTypesForAnalysisGraphRequests(t *testing.T) {
	client := startStdioTestClient(t)
	defer client.close()

	root := t.TempDir()
	page := filepath.Join(root, "types.asp")
	uri := pathToFileURI(page)
	source := `<%
Option Explicit
' @type customerId As String
Dim customerId
' @param BuildName.first As String
' @returns BuildName String
Function BuildName(first)
  BuildName = first
End Function
%>`
	writeGraphFixture(t, page, source)
	client.request("initialize", map[string]any{
		"processId":    nil,
		"rootUri":      pathToFileURI(root),
		"capabilities": map[string]any{},
	})
	openClassicASPDocument(t, client, uri, source)
	payload := buildDocumentGraph(t, client, uri, map[string]any{
		"includeAnalysisTypeDetails": true,
	})
	responseJSON := mustJSONText(t, payload)
	customerNode := graphNode(payload, func(node graph.Node) bool {
		return node.Kind == "vbDeclaration" && node.Label == "customerId"
	})
	if customerNode == nil || customerNode.TypeName != "String" {
		t.Fatalf("customerId graph type mismatch: %#v in %s", customerNode, responseJSON)
	}
	buildNameNode := graphNode(payload, func(node graph.Node) bool {
		return node.Kind == "vbDeclaration" && node.Label == "BuildName"
	})
	if buildNameNode == nil || buildNameNode.TypeName != "String" {
		t.Fatalf("BuildName graph type mismatch: %#v in %s", buildNameNode, responseJSON)
	}
	if len(buildNameNode.Parameters) != 1 ||
		buildNameNode.Parameters[0].Name != "first" ||
		buildNameNode.Parameters[0].Mode != "byref" ||
		buildNameNode.Parameters[0].TypeName != "String" {
		t.Fatalf("BuildName graph parameter type mismatch: %#v in %s", buildNameNode.Parameters, responseJSON)
	}
}

func TestStdioParityGraphsAndCountsIncludeDefinedImplicitGlobals(t *testing.T) {
	client := startStdioTestClient(t)
	defer client.close()

	root := t.TempDir()
	shared := filepath.Join(root, "shared.inc")
	page := filepath.Join(root, "default.asp")
	child := filepath.Join(root, "child.inc")
	grandchild := filepath.Join(root, "grandchild.inc")
	lateChild := filepath.Join(root, "late-child.inc")
	latePage := filepath.Join(root, "late.asp")
	before := filepath.Join(root, "before.asp")
	unrelated := filepath.Join(root, "unrelated.asp")
	sharedSource := `<%
sharedTitle = "include"
%>`
	pageSource := `<!-- #include file="shared.inc" -->
<!-- #include file="child.inc" -->
<%
Response.Write sharedTitle
sharedTitle = "page"
Response.Write chainTitle
chainTitle = "page"
Function Render()
  sharedTitle = "function"
End Function
%>`
	childSource := `<!-- #include file="grandchild.inc" -->
<%
Response.Write sharedTitle
Response.Write chainTitle
chainTitle = "child"
%>`
	grandchildSource := `<%
chainTitle = "leaf"
%>`
	lateChildSource := `<%
Response.Write sharedTitle
%>`
	latePageSource := `<!-- #include file="late-child.inc" -->
<!-- #include file="shared.inc" -->`
	beforeSource := `<%
sharedTitle = "before"
%>
<!-- #include file="shared.inc" -->`
	writeGraphFixture(t, shared, sharedSource)
	writeGraphFixture(t, page, pageSource)
	writeGraphFixture(t, child, childSource)
	writeGraphFixture(t, grandchild, grandchildSource)
	writeGraphFixture(t, lateChild, lateChildSource)
	writeGraphFixture(t, latePage, latePageSource)
	writeGraphFixture(t, before, beforeSource)
	writeGraphFixture(t, unrelated, `<%
Response.Write sharedTitle
%>`)
	sharedURI := pathToFileURI(shared)
	pageURI := pathToFileURI(page)
	childURI := pathToFileURI(child)
	grandchildURI := pathToFileURI(grandchild)
	lateChildURI := pathToFileURI(lateChild)
	beforeURI := pathToFileURI(before)

	client.request("initialize", map[string]any{
		"processId":    nil,
		"rootUri":      pathToFileURI(root),
		"capabilities": map[string]any{},
	})
	notifyConfiguration(t, client, map[string]any{"aspLsp": map[string]any{
		"diagnostics": map[string]any{"debounceMs": 0},
		"codeLens": map[string]any{
			"references": true,
		},
	}})
	openClassicASPDocumentWithDiagnostics(t, client, sharedURI, sharedSource)
	openClassicASPDocument(t, client, pageURI, pageSource)

	pageGraph := buildDocumentGraph(t, client, pageURI, nil)
	pageGraphJSON := mustJSONText(t, pageGraph)
	sharedFileNode := graphNodeByLabelAndURI(pageGraph, "shared.inc", sharedURI)
	pageFileNode := graphNodeByLabelAndURI(pageGraph, "default.asp", pageURI)
	childFileNode := graphNodeByLabelAndURI(pageGraph, "child.inc", childURI)
	grandchildFileNode := graphNodeByLabelAndURI(pageGraph, "grandchild.inc", grandchildURI)
	renderNode := graphNodeByLabelAndURI(pageGraph, "Render", pageURI)
	includeSharedTitleNode := graphNodeByLabelAndURI(pageGraph, "sharedTitle", sharedURI)
	pageSharedTitleNode := graphNodeByLabelAndURI(pageGraph, "sharedTitle", pageURI)
	childSharedTitleNode := graphNodeByLabelAndURI(pageGraph, "sharedTitle", childURI)
	chainTitleNode := graphNodeByLabelAndURI(pageGraph, "chainTitle", grandchildURI)
	pageChainTitleNode := graphNodeByLabelAndURI(pageGraph, "chainTitle", pageURI)
	childChainTitleNode := graphNodeByLabelAndURI(pageGraph, "chainTitle", childURI)

	expectImplicitGraphDeclaration(t, includeSharedTitleNode, "sharedTitle", pageGraphJSON)
	if !graphHasLinkBetween(pageGraph, "declares", includeSharedTitleNode, sharedFileNode, "") ||
		!graphHasLinkBetween(pageGraph, "references", pageFileNode, includeSharedTitleNode, "") ||
		!graphHasLinkBetween(pageGraph, "assignments", renderNode, includeSharedTitleNode, "") ||
		!graphHasLinkBetween(pageGraph, "references", childFileNode, includeSharedTitleNode, "") {
		t.Fatalf("sharedTitle include-defined links mismatch: %s", pageGraphJSON)
	}
	if graphHasLinkBetween(pageGraph, "references", pageFileNode, pageSharedTitleNode, "") ||
		graphHasLinkBetween(pageGraph, "references", childFileNode, childSharedTitleNode, "") {
		t.Fatalf("sharedTitle linked to non-canonical page/child declarations: %s", pageGraphJSON)
	}
	expectImplicitGraphDeclaration(t, chainTitleNode, "chainTitle", pageGraphJSON)
	if !graphHasLinkBetween(pageGraph, "declares", chainTitleNode, grandchildFileNode, "") ||
		!graphHasLinkBetween(pageGraph, "references", pageFileNode, chainTitleNode, "") ||
		!graphHasLinkBetween(pageGraph, "assignments", pageFileNode, chainTitleNode, "") ||
		!graphHasLinkBetween(pageGraph, "references", childFileNode, chainTitleNode, "") ||
		!graphHasLinkBetween(pageGraph, "assignments", childFileNode, chainTitleNode, "") {
		t.Fatalf("chainTitle include-defined links mismatch: %s", pageGraphJSON)
	}
	if graphHasLinkBetween(pageGraph, "references", pageFileNode, pageChainTitleNode, "") ||
		graphHasLinkBetween(pageGraph, "references", childFileNode, childChainTitleNode, "") {
		t.Fatalf("chainTitle linked to non-canonical page/child declarations: %s", pageGraphJSON)
	}

	flowchart := client.request("workspace/executeCommand", map[string]any{
		"command": "aspLsp.server.buildFlowchart",
		"arguments": []map[string]any{{
			"uri": pageURI,
		}},
	})
	chainTitleTargets := flowchartLinkTargetsContaining(t, flowchart.Result, "chainTitle")
	if !containsString(chainTitleTargets, grandchildURI) {
		t.Fatalf("flowchart missing canonical chainTitle target %s: %s", grandchildURI, mustJSONText(t, flowchart.Result))
	}
	if containsString(chainTitleTargets, childURI) || containsString(chainTitleTargets, pageURI) {
		t.Fatalf("flowchart linked chainTitle to non-canonical target: targets=%v payload=%s", chainTitleTargets, mustJSONText(t, flowchart.Result))
	}

	definition := client.request("textDocument/definition", map[string]any{
		"textDocument": map[string]any{"uri": pageURI},
		"position":     positionAt(pageSource, strings.Index(pageSource, "Response.Write chainTitle")+len("Response.Write ")),
	})
	if !strings.Contains(mustJSONText(t, definition.Result), grandchildURI) {
		t.Fatalf("chainTitle definition did not target grandchild: %s", mustJSONText(t, definition.Result))
	}

	beforeGraph := buildDocumentGraph(t, client, beforeURI, nil)
	beforeJSON := mustJSONText(t, beforeGraph)
	beforeFileNode := graphNodeByLabelAndURI(beforeGraph, "before.asp", beforeURI)
	beforeSharedTitleNode := graphNodeByLabelAndURI(beforeGraph, "sharedTitle", beforeURI)
	beforeIncludeSharedTitleNode := graphNodeByLabelAndURI(beforeGraph, "sharedTitle", sharedURI)
	if !graphHasLinkBetween(beforeGraph, "assignments", beforeFileNode, beforeSharedTitleNode, "") ||
		graphHasLinkBetween(beforeGraph, "references", beforeFileNode, beforeIncludeSharedTitleNode, "") {
		t.Fatalf("before include canonicalization mismatch: %s", beforeJSON)
	}

	codeLens := client.request("textDocument/codeLens", map[string]any{
		"textDocument": map[string]any{"uri": sharedURI},
	})
	referencesCodeLens := codeLensWithData(t, codeLens.Result, "vbscript-reference", "sharedTitle")
	resolvedCodeLens := client.request("codeLens/resolve", referencesCodeLens)
	resolvedText := mustJSONText(t, resolvedCodeLens.Result)
	if !strings.Contains(resolvedText, "4 references") {
		t.Fatalf("include-defined sharedTitle codelens mismatch: %s", resolvedText)
	}
	references := client.request("textDocument/references", map[string]any{
		"textDocument": map[string]any{"uri": sharedURI},
		"position":     map[string]any{"line": 1, "character": 1},
		"context":      map[string]any{"includeDeclaration": false},
	})
	referencesText := mustJSONText(t, references.Result)
	if countOccurrences(referencesText, childURI) != 1 ||
		countOccurrences(referencesText, pageURI) != 3 ||
		strings.Contains(referencesText, lateChildURI) {
		t.Fatalf("include-defined sharedTitle references mismatch: %s", referencesText)
	}
}

func writeGraphFixture(t *testing.T, path string, text string) {
	t.Helper()
	if err := os.WriteFile(path, []byte(text), 0o644); err != nil {
		t.Fatal(err)
	}
}

func buildDocumentGraph(t *testing.T, client *stdioTestClient, uri string, options map[string]any) graph.Payload {
	t.Helper()
	argument := map[string]any{
		"scope": "document",
		"uri":   uri,
	}
	for key, value := range options {
		argument[key] = value
	}
	response := client.request("workspace/executeCommand", map[string]any{
		"command":   "aspLsp.server.buildGraph",
		"arguments": []map[string]any{argument},
	})
	var payload graph.Payload
	mustDecodeResult(t, response.Result, &payload)
	return payload
}

func graphNode(payload graph.Payload, match func(graph.Node) bool) *graph.Node {
	for i := range payload.Nodes {
		if match(payload.Nodes[i]) {
			return &payload.Nodes[i]
		}
	}
	return nil
}

func graphHasNode(payload graph.Payload, match func(graph.Node) bool) bool {
	return graphNode(payload, match) != nil
}

func graphHasFileNode(payload graph.Payload, label string) bool {
	return graphHasNode(payload, func(node graph.Node) bool {
		return node.Kind == "file" && node.Label == label
	})
}

func graphNodeByLabelAndURI(payload graph.Payload, label string, uri string) *graph.Node {
	return graphNode(payload, func(node graph.Node) bool {
		return node.Label == label && node.URI == uri
	})
}

func graphNodeByFullPathAndURI(payload graph.Payload, fullPath string, uri string) *graph.Node {
	return graphNode(payload, func(node graph.Node) bool {
		return node.FullPath == fullPath && node.URI == uri
	})
}

func graphHasEdge(payload graph.Payload, match func(graph.Edge) bool) bool {
	for _, edge := range graphEdges(payload) {
		if match(edge) {
			return true
		}
	}
	return false
}

func graphEdge(payload graph.Payload, match func(graph.Edge) bool) *graph.Edge {
	for _, edge := range graphEdges(payload) {
		if match(edge) {
			copy := edge
			return &copy
		}
	}
	return nil
}

func graphEdges(payload graph.Payload) []graph.Edge {
	if len(payload.Links) > 0 {
		return payload.Links
	}
	return payload.Edges
}

func graphHasLinkBetween(payload graph.Payload, kind string, source *graph.Node, target *graph.Node, role string) bool {
	if source == nil || target == nil {
		return false
	}
	return graphHasEdge(payload, func(edge graph.Edge) bool {
		return edge.Kind == kind &&
			edge.Source == source.ID &&
			edge.Target == target.ID &&
			(role == "" || edge.Role == role)
	})
}

func expectImplicitGraphDeclaration(t *testing.T, node *graph.Node, label string, responseJSON string) {
	t.Helper()
	if node == nil ||
		node.Label != label ||
		node.DeclarationKind != "variable" ||
		node.BindingScope != "global" ||
		!node.Implicit {
		t.Fatalf("implicit graph declaration %s mismatch: %#v in %s", label, node, responseJSON)
	}
}

func codeLensWithData(t *testing.T, value any, kind string, name string) map[string]any {
	t.Helper()
	var lenses []map[string]any
	mustDecodeResult(t, value, &lenses)
	for _, lens := range lenses {
		data, ok := lens["data"].(map[string]any)
		if !ok {
			continue
		}
		if data["kind"] == kind && data["name"] == name {
			return lens
		}
	}
	t.Fatalf("codeLens data kind=%s name=%s missing: %s", kind, name, mustJSONText(t, value))
	return nil
}

func flowchartLinkTargetsContaining(t *testing.T, value any, labelPart string) []string {
	t.Helper()
	var payload struct {
		Nodes []struct {
			Links []struct {
				Label  string `json:"label"`
				Target struct {
					URI string `json:"uri"`
				} `json:"target"`
			} `json:"links"`
		} `json:"nodes"`
	}
	mustDecodeResult(t, value, &payload)
	targets := []string{}
	for _, node := range payload.Nodes {
		for _, link := range node.Links {
			if strings.Contains(link.Label, labelPart) && link.Target.URI != "" {
				targets = append(targets, link.Target.URI)
			}
		}
	}
	return targets
}

func containsString(values []string, expected string) bool {
	for _, value := range values {
		if value == expected {
			return true
		}
	}
	return false
}

func expectMemberGraphNode(t *testing.T, node *graph.Node, label string, receiverName string, memberName string, fullPath string, responseJSON string) {
	t.Helper()
	if node == nil ||
		node.Kind != "vbMemberReference" ||
		node.Label != label ||
		node.Role != "member" ||
		node.ReceiverName != receiverName ||
		node.MemberName != memberName ||
		node.FullPath != fullPath {
		t.Fatalf("member graph node %s mismatch: %#v in %s", fullPath, node, responseJSON)
	}
}
