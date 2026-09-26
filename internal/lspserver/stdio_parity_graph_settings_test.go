package lspserver

import (
	"encoding/json"
	"os"
	"path/filepath"
	"sort"
	"strings"
	"testing"

	"github.com/yottonoko/classic-web-system-lsp/internal/graph"
)

func TestStdioParityReusesVBSummaryGraphWhileReportingIncludeCycles(t *testing.T) {
	client := startStdioTestClient(t)
	defer client.close()

	root := t.TempDir()
	owner := filepath.Join(root, "default.asp")
	first := filepath.Join(root, "first.inc")
	second := filepath.Join(root, "second.inc")
	writeGraphSettingsFixture(t, owner, `<!-- #include file="first.inc" -->
<% Response.Write 1 %>`)
	writeGraphSettingsFixture(t, first, `<!-- #include file="second.inc" -->`)
	writeGraphSettingsFixture(t, second, `<!-- #include file="first.inc" -->`)
	ownerURI := pathToFileURI(owner)

	client.request("initialize", map[string]any{
		"processId":    nil,
		"rootUri":      pathToFileURI(root),
		"capabilities": map[string]any{},
	})
	notifyConfiguration(t, client, map[string]any{"aspLsp": map[string]any{
		"debug":       map[string]any{"output": "verbose"},
		"diagnostics": map[string]any{"debounceMs": 0},
	}})
	notifyOpenClassicASPDocument(t, client, ownerURI, mustReadText(t, owner))

	diagnostics, seenDuringDiagnostics := client.waitForNotificationWithSeen("textDocument/publishDiagnostics", "Include cycle detected")
	if !strings.Contains(string(diagnostics.Params), "Include cycle detected") {
		t.Fatalf("include cycle diagnostics mismatch: %s", string(diagnostics.Params))
	}
	logs := append([]*rpcMessage{}, seenDuringDiagnostics...)
	logs = append(logs, client.waitForLogContaining("LSP check completed"))
	logs = append(logs, client.drainNotifications("window/logMessage")...)
	logText := mustJSONText(t, logs)
	for _, expected := range []string{
		"includeDiagnostics.directIncludes",
		"includeDiagnostics.cycleGraph",
		"vbProject.summaryGraph.collect",
		"vbProject.summaryGraph.reuse",
	} {
		if !strings.Contains(logText, expected) {
			t.Fatalf("summary graph log missing %q: %s", expected, logText)
		}
	}
	builtOrTruncated := countOccurrences(logText, "vbProject.summaryGraph.built") +
		countOccurrences(logText, "vbProject.summaryGraph.truncated")
	if builtOrTruncated > 1 {
		t.Fatalf("summary graph built/truncated too many times = %d: %s", builtOrTruncated, logText)
	}
}

func TestStdioParityReturnsGraphFilterSettingsForWebviewInitialState(t *testing.T) {
	client := startStdioTestClient(t)
	defer client.close()

	root := t.TempDir()
	common := filepath.Join(root, "common.inc")
	owner := filepath.Join(root, "default.asp")
	writeGraphSettingsFixture(t, common, `<%
Dim IncludedGlobal
Const IncludedConst = 5
Sub Shared()
End Sub
%>`)
	source := `<!-- #include file="common.inc" -->
<!-- #include file="missing.inc" -->
<%
Dim GlobalValue
Const GlobalConst = 1
Class Customer
  Public Const Kind = "retail"
  Public Name
  Public Function BuildLabel()
    BuildLabel = Name
  End Function
  Public Sub Save(item)
    Dim localValue
    Const localConst = 2
    Response.Write CStr(item)
    Repository.Find item
    localValue = localConst
  End Sub
End Class
Function Render(value)
  Dim localRender
  Const localRenderConst = 3
  Render = value
End Function
Sub Main(arg)
  Dim localMain
  Const localMainConst = 4
  Dim localItems(1)
  Dim matrixItems(2, 3)
  Dim dynamicItems()
  ReDim Preserve redimItems(2)
  If arg Then
    localMain = 1
  End If
  localMain = arg
  localMain("value") = arg
  bareImplicit = arg
  For loopIndex = 1 To 2
    loopImplicit = loopIndex
  Next
  For Each loopItem In dynamicItems
    eachImplicit = loopItem
  Next
  implicitIndexed(0) = arg
  Response.Write implicitIndexed(0)
  localMain = IncludedGlobal
  localMain = IncludedConst
  localMain = Err.Number
  Call Shared()
  Err.Clear
  Response.Write CStr(GlobalConst)
  Repository.Find arg
  MissingName
  MissingName
  Set missingObject = New MissingClass
  Call MissingProc()
  Call MissingProc()
End Sub
Class WithProperty
  Public Property Get Title()
    Title = "title"
  End Property
End Class
%>
<object runat="server" id="repoObject" progid="RepositoryType"></object>
<script runat="server" language="VBScript">
Sub ObjectMain()
  repoObject.Find "value"
End Sub
</script>`
	writeGraphSettingsFixture(t, owner, source)
	ownerURI := pathToFileURI(owner)

	initializeAndWaitForWorkspaceIndex(t, client, map[string]any{
		"processId":    nil,
		"rootUri":      pathToFileURI(root),
		"capabilities": map[string]any{},
	})
	configureGraphSettingsTest(t, client, nil)
	waitForWorkspaceIndexRefresh(t, client)
	defaultGraph := buildDocumentGraph(t, client, ownerURI, nil)
	expectGraphSetting(t, defaultGraph, "hideSingleNodes", true)
	expectGraphSetting(t, defaultGraph, "hideUnreferencedGlobalSymbols", true)
	expectGraphSetting(t, defaultGraph, "showOutgoingSelectionLinks", true)
	expectGraphSetting(t, defaultGraph, "initialViewMode", "2d")
	for _, category := range []string{"method", "methodFunction", "methodSub", "property", "member", "localVariable", "localConstant", "parameter"} {
		if !graphSettingStrings(defaultGraph, "hiddenNodeCategories").contains(category) {
			t.Fatalf("default hiddenNodeCategories missing %s: %s", category, mustJSONText(t, defaultGraph.Settings))
		}
	}
	if graphSettingStrings(defaultGraph, "hiddenLinkCategories").contains("member") {
		t.Fatalf("member links hidden by default: %s", mustJSONText(t, defaultGraph.Settings))
	}
	expectGraphNodeShape(t, defaultGraph, "default.asp", graph.Node{Kind: "file", IsRoot: true})
	expectGraphNodeShape(t, defaultGraph, "common.inc", graph.Node{Kind: "file"})
	for _, label := range []string{
		"GlobalValue", "GlobalConst", "IncludedGlobal", "IncludedConst", "Customer.Name",
		"Customer.Save", "Customer.Kind", "localValue", "localConst", "localMain", "localItems",
		"matrixItems", "dynamicItems", "redimItems", "bareImplicit", "loopIndex", "loopItem", "loopImplicit",
		"eachImplicit", "implicitIndexed", "arg", "Repository", "RepositoryType.Find", "MissingName",
	} {
		if graphNodeByLabel(defaultGraph, label) == nil {
			t.Fatalf("default graph missing node %s: %s", label, mustJSONText(t, defaultGraph))
		}
	}
	for _, label := range []string{"Preserve", "Response", "Response.Write", "CStr"} {
		if graphNodeByLabel(defaultGraph, label) != nil {
			t.Fatalf("default graph unexpectedly included %s: %s", label, mustJSONText(t, defaultGraph))
		}
	}
	if graphNodeMatching(defaultGraph, func(node graph.Node) bool { return node.Kind == "vbUnresolved" && node.Label == "IncludedGlobal" }) != nil ||
		graphNodeMatching(defaultGraph, func(node graph.Node) bool { return node.Kind == "vbUnresolved" && node.Label == "IncludedConst" }) != nil ||
		graphNodeMatching(defaultGraph, func(node graph.Node) bool { return node.Kind == "vbUnresolved" && node.Label == "Shared" }) != nil ||
		graphNodeMatching(defaultGraph, func(node graph.Node) bool { return node.Kind == "vbUnresolved" && node.Label == "Err" }) != nil {
		t.Fatalf("default graph included resolved symbols as unresolved: %s", mustJSONText(t, defaultGraph))
	}
	if graphNodeMatching(defaultGraph, func(node graph.Node) bool { return node.ExternalKind == "member" }) == nil ||
		!graphHasLinkMatching(defaultGraph, func(edge graph.Edge) bool { return edge.Role == "member" }) {
		t.Fatalf("default graph missing configured member nodes or links: %s", mustJSONText(t, defaultGraph))
	}

	allGraphSettings := allVisibleGraphSettings()
	configureGraphSettingsTest(t, client, allGraphSettings)
	visibleGraph := buildDocumentGraph(t, client, ownerURI, nil)
	expectGraphSetting(t, visibleGraph, "hideSingleNodes", false)
	expectGraphSetting(t, visibleGraph, "hideUnreferencedGlobalSymbols", false)
	expectGraphSetting(t, visibleGraph, "initialViewMode", "3d")
	expectGraphNodeShape(t, visibleGraph, "GlobalValue", graph.Node{DeclarationKind: "variable", BindingScope: "global", Origin: "source"})
	expectGraphNodeShape(t, visibleGraph, "GlobalConst", graph.Node{DeclarationKind: "constant", BindingScope: "global", Origin: "source"})
	expectGraphNodeShape(t, visibleGraph, "localValue", graph.Node{DeclarationKind: "variable", BindingScope: "local", Origin: "source"})
	expectGraphNodeShape(t, visibleGraph, "localConst", graph.Node{DeclarationKind: "constant", BindingScope: "local", Origin: "source"})
	expectGraphNodeShape(t, visibleGraph, "arg", graph.Node{DeclarationKind: "parameter", BindingScope: "local"})
	expectGraphNodeShape(t, visibleGraph, "Customer", graph.Node{DeclarationKind: "class", Origin: "source"})
	expectGraphNodeShape(t, visibleGraph, "Render", graph.Node{DeclarationKind: "function", Origin: "source"})
	expectGraphNodeShape(t, visibleGraph, "Shared", graph.Node{DeclarationKind: "sub", Origin: "source"})
	expectGraphNodeShape(t, visibleGraph, "WithProperty.Title", graph.Node{DeclarationKind: "property", MemberOf: "WithProperty", ProcedureKind: "property-get", Origin: "source"})
	expectGraphNodeShape(t, visibleGraph, "repoObject", graph.Node{DeclarationKind: "variable", BindingScope: "global", TypeName: "RepositoryType", Origin: "source"})
	expectGraphNodeShape(t, visibleGraph, "Customer.Save", graph.Node{DeclarationKind: "method", MemberOf: "Customer", ProcedureKind: "sub", Origin: "source"})
	expectGraphNodeShape(t, visibleGraph, "Customer.BuildLabel", graph.Node{DeclarationKind: "method", MemberOf: "Customer", ProcedureKind: "function", Origin: "source"})
	expectArrayNode(t, visibleGraph, "localItems", "fixed", []string{"1"})
	expectArrayNode(t, visibleGraph, "matrixItems", "fixed", []string{"2", "3"})
	expectArrayNode(t, visibleGraph, "dynamicItems", "dynamic", []string{})
	expectArrayNode(t, visibleGraph, "redimItems", "dynamic", []string{"2"})
	for _, label := range []string{"bareImplicit", "loopImplicit", "eachImplicit", "implicitIndexed", "missingObject"} {
		node := graphNodeByLabel(visibleGraph, label)
		if node == nil || node.DeclarationKind != "variable" || node.BindingScope != "global" || !node.Implicit || !node.ImplicitGlobal {
			t.Fatalf("procedure assigned implicit global %s mismatch: %#v in %s", label, node, mustJSONText(t, visibleGraph))
		}
	}
	for _, label := range []string{"MissingName"} {
		node := graphNodeByLabel(visibleGraph, label)
		if node == nil || node.DeclarationKind != "variable" || node.BindingScope != "local" || !node.Implicit {
			t.Fatalf("procedure read-only implicit declaration %s mismatch: %#v in %s", label, node, mustJSONText(t, visibleGraph))
		}
	}
	for _, link := range [][3]string{
		{"declares", "GlobalValue", "default.asp"},
		{"declares", "GlobalConst", "default.asp"},
		{"declares", "IncludedGlobal", "common.inc"},
		{"declares", "IncludedConst", "common.inc"},
		{"declares", "Customer.Name", "Customer"},
		{"declares", "Customer.Kind", "Customer"},
		{"declares", "Customer.Save", "Customer"},
		{"declares", "localValue", "Customer.Save"},
		{"declares", "localConst", "Customer.Save"},
		{"declares", "arg", "Main"},
		{"declares", "localMain", "Main"},
		{"declares", "localMainConst", "Main"},
		{"declares", "localItems", "Main"},
		{"declares", "matrixItems", "Main"},
		{"declares", "dynamicItems", "Main"},
		{"declares", "redimItems", "Main"},
		{"declares", "bareImplicit", "default.asp"},
		{"declares", "loopIndex", "Main"},
		{"declares", "loopItem", "Main"},
		{"declares", "loopImplicit", "default.asp"},
		{"declares", "eachImplicit", "default.asp"},
		{"declares", "implicitIndexed", "default.asp"},
		{"declares", "missingObject", "default.asp"},
		{"declares", "WithProperty.Title", "WithProperty"},
		{"declares", "repoObject", "default.asp"},
		{"references", "Main", "arg"},
		{"assignments", "Main", "localMain"},
		{"assignments", "Main", "bareImplicit"},
		{"references", "Main", "loopIndex"},
		{"references", "Main", "loopItem"},
		{"references", "Main", "implicitIndexed"},
		{"references", "Main", "MissingName"},
		{"references", "ObjectMain", "repoObject"},
		{"references", "Customer.Save", "localConst"},
		{"references", "Main", "IncludedGlobal"},
		{"references", "Main", "IncludedConst"},
		{"calls", "Main", "Shared"},
		{"calls", "ObjectMain", "RepositoryType.Find"},
	} {
		if !graphHasLinkBetween(visibleGraph, link[0], graphNodeByLabel(visibleGraph, link[1]), graphNodeByLabel(visibleGraph, link[2]), "") {
			t.Fatalf("visible graph missing %s %s -> %s: %s", link[0], link[1], link[2], mustJSONText(t, visibleGraph))
		}
	}
	for _, link := range [][3]string{
		{"references", "Main", "Err"},
		{"calls", "Main", "ErrObject.Clear"},
		{"calls", "Main", "Response.Write"},
		{"calls", "Main", "CStr"},
	} {
		if graphHasLinkBetween(visibleGraph, link[0], graphNodeByLabel(visibleGraph, link[1]), graphNodeByLabel(visibleGraph, link[2]), "") {
			t.Fatalf("visible graph unexpectedly included %s %s -> %s: %s", link[0], link[1], link[2], mustJSONText(t, visibleGraph))
		}
	}
	missingClass := graphNodeMatching(visibleGraph, func(node graph.Node) bool {
		return node.Kind == "vbUnresolved" && node.Label == "MissingClass"
	})
	if missingClass == nil || !graphHasLinkMatching(visibleGraph, func(edge graph.Edge) bool {
		return edge.Kind == "unresolvedReference" && edge.Target == missingClass.ID && edge.Count == 1
	}) {
		t.Fatalf("visible graph missing MissingClass unresolved link: %s", mustJSONText(t, visibleGraph))
	}

	for _, testCase := range []struct {
		setting  string
		value    bool
		category string
		linkKind string
		nodePred func(graph.Node) bool
	}{
		{setting: "showIncludeLinks", value: false, category: "include", linkKind: "include"},
		{setting: "showDeclareLinks", value: false, category: "declares", linkKind: "declares"},
		{setting: "showReferenceLinks", value: false, category: "references", linkKind: "references"},
		{setting: "showAssignmentLinks", value: false, category: "assignments", linkKind: "assignments"},
		{setting: "showCallLinks", value: false, category: "calls", linkKind: "calls"},
		{setting: "showUnresolvedLinks", value: false, category: "unresolvedReference", linkKind: "unresolvedReference"},
		{setting: "showMemberLinks", value: false, category: "member", linkKind: "", nodePred: func(node graph.Node) bool { return node.ExternalKind == "member" }},
	} {
		settings := allVisibleGraphSettings()
		settings[testCase.setting] = testCase.value
		configureGraphSettingsTest(t, client, settings)
		payload := buildDocumentGraph(t, client, ownerURI, nil)
		if testCase.linkKind != "" && !graphHasLinkMatching(payload, func(edge graph.Edge) bool { return edge.Kind == testCase.linkKind }) {
			t.Fatalf("graph with %s=false lost %s link: %s", testCase.setting, testCase.linkKind, mustJSONText(t, payload))
		}
		if testCase.nodePred != nil && graphNodeMatching(payload, testCase.nodePred) == nil {
			t.Fatalf("graph with %s=false lost member node: %s", testCase.setting, mustJSONText(t, payload))
		}
		if !graphSettingStrings(payload, "hiddenLinkCategories").contains(testCase.category) {
			t.Fatalf("graph with %s=false missing hidden link %s: %s", testCase.setting, testCase.category, mustJSONText(t, payload.Settings))
		}
	}
	settings := allVisibleGraphSettings()
	settings["showUnresolvedNodes"] = false
	configureGraphSettingsTest(t, client, settings)
	noUnresolvedNodes := buildDocumentGraph(t, client, ownerURI, nil)
	if graphNodeMatching(noUnresolvedNodes, func(node graph.Node) bool { return node.Kind == "vbUnresolved" }) == nil ||
		!graphSettingStrings(noUnresolvedNodes, "hiddenNodeCategories").contains("unresolved") {
		t.Fatalf("graph with unresolved nodes hidden mismatch: %s", mustJSONText(t, noUnresolvedNodes))
	}
	settings = allVisibleGraphSettings()
	settings["showOutgoingSelectionLinks"] = false
	configureGraphSettingsTest(t, client, settings)
	expectGraphSetting(t, buildDocumentGraph(t, client, ownerURI, nil), "showOutgoingSelectionLinks", false)
	settings = allVisibleGraphSettings()
	settings["showMemberNodes"] = false
	configureGraphSettingsTest(t, client, settings)
	noMemberNodes := buildDocumentGraph(t, client, ownerURI, nil)
	if graphNodeMatching(noMemberNodes, func(node graph.Node) bool { return node.ExternalKind == "member" }) == nil ||
		!graphSettingStrings(noMemberNodes, "hiddenNodeCategories").contains("member") {
		t.Fatalf("graph with member nodes hidden mismatch: %s", mustJSONText(t, noMemberNodes))
	}
	settings = allVisibleGraphSettings()
	settings["showFileNodes"] = false
	configureGraphSettingsTest(t, client, settings)
	noFileNodes := buildDocumentGraph(t, client, ownerURI, nil)
	if graphNodeMatching(noFileNodes, func(node graph.Node) bool { return node.Kind == "file" && !node.IsRoot }) == nil ||
		!graphSettingStrings(noFileNodes, "hiddenNodeCategories").contains("file") ||
		!graphHasLinkMatching(noFileNodes, func(edge graph.Edge) bool {
			return edge.Kind == "include" && edge.Include != nil && !edge.Include.Exists
		}) {
		t.Fatalf("graph with file nodes hidden mismatch: %s", mustJSONText(t, noFileNodes))
	}
}

func TestStdioParityStreamsWorkspaceGraphIndexesThroughBulkSpillStorage(t *testing.T) {
	client := startStdioTestClient(t)
	defer client.close()

	root := t.TempDir()
	writeGraphSettingsFixture(t, filepath.Join(root, "default.asp"), `<!-- #include file="common.inc" -->
<%
Dim localValue
localValue = SharedValue
%>`)
	writeGraphSettingsFixture(t, filepath.Join(root, "common.inc"), `<%
Dim SharedValue
%>`)
	initializeWithConfigurationAndWaitForWorkspaceIndex(t, client, map[string]any{
		"processId":    nil,
		"rootUri":      pathToFileURI(root),
		"capabilities": map[string]any{},
	}, map[string]any{"aspLsp": map[string]any{
		"debug":       map[string]any{"output": "verbose"},
		"diagnostics": map[string]any{"debounceMs": 0},
	}})

	payload := buildWorkspaceGraph(t, client, nil)
	if graphNodeByLabel(payload, "SharedValue") == nil ||
		!graphHasLinkMatching(payload, func(edge graph.Edge) bool { return edge.Kind == "include" }) {
		t.Fatalf("workspace bulk graph mismatch: %s", mustJSONText(t, payload))
	}
	client.waitForLogContaining("asp.graph.bulk.spill.write")
	client.waitForLogContaining("asp.graph.bulk.complete")
}

func TestStdioParityReusesColdGraphSymbolIndexesWhenAddingAnalysisTypeDetails(t *testing.T) {
	client := startStdioTestClient(t)
	defer client.close()

	root := t.TempDir()
	uri := pathToFileURI(filepath.Join(root, "default.asp"))
	source := `<%
implicitTotal = 1
Function BuildValue()
  BuildValue = implicitTotal
End Function
%>`
	client.request("initialize", map[string]any{
		"processId":    nil,
		"rootUri":      pathToFileURI(root),
		"capabilities": map[string]any{},
	})
	notifyConfiguration(t, client, map[string]any{"aspLsp": map[string]any{
		"debug": map[string]any{"output": "summary"},
	}})
	notifyOpenClassicASPDocument(t, client, uri, source)

	payload := buildDocumentGraph(t, client, uri, map[string]any{
		"includeRelatedIncludeTreesForUnresolved": true,
		"includeAnalysisTypeDetails":              true,
	})
	if graphNodeByLabel(payload, "BuildValue") == nil {
		t.Fatalf("analysis type detail graph missing BuildValue: %s", mustJSONText(t, payload))
	}
	client.waitForLogContaining("graphVbIndex.extendTypeHints")
}

func TestStdioParityRestoresGraphIncludeRefsAndVBSymbolIndexesFromDiskCacheAfterRestart(t *testing.T) {
	root := t.TempDir()
	cacheDir := filepath.Join(root, ".cache")
	writeGraphSettingsFixture(t, filepath.Join(root, "default.asp"), `<!-- #include file="common.inc" -->
<%
Function Render(value)
  Render = value
End Function
Sub PageEntry()
  Render "x"
End Sub
%>`)
	writeGraphSettingsFixture(t, filepath.Join(root, "common.inc"), `<%
Sub SharedEntry()
End Sub
%>`)
	settings := map[string]any{"aspLsp": map[string]any{
		"debug":       map[string]any{"output": "verbose"},
		"cache":       map[string]any{"enabled": true, "directory": cacheDir},
		"diagnostics": map[string]any{"debounceMs": 0},
	}}

	firstClient := startStdioTestClient(t)
	initializeWithConfigurationAndWaitForWorkspaceIndex(t, firstClient, map[string]any{
		"processId":    nil,
		"rootUri":      pathToFileURI(root),
		"capabilities": map[string]any{},
	}, settings)
	firstGraph := buildWorkspaceGraph(t, firstClient, nil)
	if graphNodeMatching(firstGraph, func(node graph.Node) bool {
		return node.Kind == "vbDeclaration" && node.Label == "PageEntry"
	}) == nil || !graphHasLinkMatching(firstGraph, func(edge graph.Edge) bool { return edge.Kind == "include" }) {
		t.Fatalf("first cached workspace graph mismatch: %s", mustJSONText(t, firstGraph))
	}
	firstClient.waitForLogContaining("database.fileBundle.write")
	firstClient.waitForLogContaining("database.graphPayload.write")
	firstClient.close()

	secondClient := startStdioTestClient(t)
	defer secondClient.close()
	initializeWithConfigurationAndWaitForWorkspaceIndex(t, secondClient, map[string]any{
		"processId":    nil,
		"rootUri":      pathToFileURI(root),
		"capabilities": map[string]any{},
	}, settings)
	secondGraph := buildWorkspaceGraph(t, secondClient, nil)
	if mustJSONText(t, normalizeCachedGraph(firstGraph)) != mustJSONText(t, normalizeCachedGraph(secondGraph)) {
		t.Fatalf("restored graph mismatch:\nfirst=%s\nsecond=%s", mustJSONText(t, normalizeCachedGraph(firstGraph)), mustJSONText(t, normalizeCachedGraph(secondGraph)))
	}
	secondClient.waitForLogContaining("database.graphPayload.hit")
}

func TestStdioParityRestoresWorkspaceIndexAndParsedFilesInWatchFreshnessMode(t *testing.T) {
	root := t.TempDir()
	cacheDir := filepath.Join(root, ".cache")
	writeGraphSettingsFixture(t, filepath.Join(root, "default.asp"), `<%
Sub WatchIndexed()
End Sub
%>`)
	settings := map[string]any{"aspLsp": map[string]any{
		"debug": map[string]any{"output": "verbose"},
		"cache": map[string]any{"enabled": true, "directory": cacheDir, "freshness": "watch"},
	}}

	firstClient := startStdioTestClient(t)
	initializeWithConfigurationAndWaitForWorkspaceIndex(t, firstClient, map[string]any{
		"processId":    nil,
		"rootUri":      pathToFileURI(root),
		"capabilities": map[string]any{},
	}, settings)
	firstSymbols := firstClient.request("workspace/symbol", map[string]any{"query": "WatchIndexed"})
	if !strings.Contains(mustJSONText(t, firstSymbols.Result), "WatchIndexed") {
		t.Fatalf("first workspace symbols missing WatchIndexed: %s", mustJSONText(t, firstSymbols.Result))
	}
	firstClient.waitForLogContaining("workspaceIndex.write")
	firstClient.waitForLogContaining("database.fileBundle.write")
	firstClient.close()

	secondClient := startStdioTestClient(t)
	defer secondClient.close()
	initializeWithConfigurationAndWaitForWorkspaceIndex(t, secondClient, map[string]any{
		"processId":    nil,
		"rootUri":      pathToFileURI(root),
		"capabilities": map[string]any{},
	}, settings)
	restoredSymbols := secondClient.request("workspace/symbol", map[string]any{"query": "WatchIndexed"})
	if !strings.Contains(mustJSONText(t, restoredSymbols.Result), "WatchIndexed") {
		t.Fatalf("restored workspace symbols missing WatchIndexed: %s", mustJSONText(t, restoredSymbols.Result))
	}
	secondClient.waitForLogContaining("database.workspaceIndex.restore")
	secondClient.waitForLogContaining("database.fileBundle.hit")
}

func TestStdioParityRepairsStaleReverseIncludeCandidatesAfterWatchIndexRestoreRevalidation(t *testing.T) {
	root := t.TempDir()
	cacheDir := filepath.Join(root, ".cache")
	shared := filepath.Join(root, "shared.inc")
	parent := filepath.Join(root, "parent.asp")
	sharedURI := pathToFileURI(shared)
	writeGraphSettingsFixture(t, shared, `<%
Sub SharedEntry()
End Sub
%>`)
	writeGraphSettingsFixture(t, parent, `<%
Sub ParentBefore()
End Sub
%>`)
	settings := map[string]any{"aspLsp": map[string]any{
		"debug":   map[string]any{"output": "verbose"},
		"cache":   map[string]any{"enabled": true, "directory": cacheDir, "freshness": "watch"},
		"graph":   map[string]any{"showIncomingDocumentIncludes": true},
		"network": map[string]any{"profile": "network"},
	}}

	firstClient := startStdioTestClient(t)
	initializeWithConfigurationAndWaitForWorkspaceIndex(t, firstClient, map[string]any{
		"processId":    nil,
		"rootUri":      pathToFileURI(root),
		"capabilities": map[string]any{},
	}, settings)
	firstGraph := buildDocumentGraph(t, firstClient, sharedURI, nil)
	if strings.Contains(mustJSONText(t, firstGraph.Nodes), "ParentAfter") {
		t.Fatalf("initial graph unexpectedly included ParentAfter: %s", mustJSONText(t, firstGraph))
	}
	firstClient.waitForLogContaining("workspaceIndex.write")
	firstClient.waitForLogContaining("workspaceIncludeGraph.write")
	firstClient.close()

	writeGraphSettingsFixture(t, parent, `<!-- #include file="shared.inc" -->
<%
Sub ParentAfter()
End Sub
%>`)
	secondClient := startStdioTestClient(t)
	defer secondClient.close()
	initializeWithConfigurationAndWaitForWorkspaceIndex(t, secondClient, map[string]any{
		"processId":    nil,
		"rootUri":      pathToFileURI(root),
		"capabilities": map[string]any{},
	}, settings)
	_ = buildDocumentGraph(t, secondClient, sharedURI, nil)
	secondClient.waitForLogContaining("database.workspaceIndex.write")
	secondClient.waitForLogContaining("database.workspaceIncludeGraph.write")
	secondClient.waitForLogContaining("workspaceIndex.complete")
	repairedGraph := buildDocumentGraph(t, secondClient, sharedURI, nil)
	if !strings.Contains(mustJSONText(t, repairedGraph.Nodes), "ParentAfter") {
		t.Fatalf("repaired graph missing ParentAfter: %s", mustJSONText(t, repairedGraph))
	}
}

func TestStdioParityKeepsMetadataFreshnessCurrentForUnchangedWorkspaceIndexes(t *testing.T) {
	root := t.TempDir()
	cacheDir := filepath.Join(root, ".cache")
	fileName := filepath.Join(root, "default.asp")
	writeGraphSettingsFixture(t, fileName, `<%
Sub OldCachedName()
End Sub
%>`)
	client := startStdioTestClient(t)
	defer client.close()
	initializeWithConfigurationAndWaitForWorkspaceIndex(t, client, map[string]any{
		"processId":    nil,
		"rootUri":      pathToFileURI(root),
		"capabilities": map[string]any{},
	}, map[string]any{"aspLsp": map[string]any{
		"debug": map[string]any{"output": "verbose"},
		"cache": map[string]any{"enabled": true, "directory": cacheDir, "freshness": "metadata"},
	}})
	oldSymbols := client.request("workspace/symbol", map[string]any{"query": "OldCachedName"})
	if !strings.Contains(mustJSONText(t, oldSymbols.Result), "OldCachedName") {
		t.Fatalf("old workspace symbols missing OldCachedName: %s", mustJSONText(t, oldSymbols.Result))
	}
	client.waitForLogContaining("database.fileBundle.write")
	client.drainNotifications("window/logMessage")

	writeGraphSettingsFixture(t, fileName, `<%
Sub NewCachedNameLonger()
End Sub
%>`)
	newSymbols := client.request("workspace/symbol", map[string]any{"query": "NewCachedNameLonger"})
	if !strings.Contains(mustJSONText(t, newSymbols.Result), "NewCachedNameLonger") {
		t.Fatalf("metadata-refreshed workspace symbols missing NewCachedNameLonger: %s", mustJSONText(t, newSymbols.Result))
	}
	client.waitForLogContaining("database.workspaceIndex.stale")
}

func configureGraphSettingsTest(t *testing.T, client *stdioTestClient, graphSettings map[string]any) {
	t.Helper()
	settings := map[string]any{"aspLsp": map[string]any{
		"vbscript": map[string]any{
			"globals": map[string]any{"Repository": "RepositoryType"},
			"comTypes": map[string]any{
				"RepositoryType": map[string]any{"members": map[string]any{
					"Find": map[string]any{
						"kind":       "method",
						"returnType": "Variant",
						"parameters": []map[string]any{{"name": "id", "type": "String"}},
					},
				}},
			},
		},
		"diagnostics": map[string]any{"debounceMs": 0},
	}}
	if graphSettings != nil {
		settings["aspLsp"].(map[string]any)["graph"] = graphSettings
	}
	notifyConfiguration(t, client, settings)
}

func allVisibleGraphSettings() map[string]any {
	return map[string]any{
		"initialViewMode":               "3d",
		"showRootNodes":                 true,
		"showFileNodes":                 true,
		"showFunctionNodes":             true,
		"showSubNodes":                  true,
		"showClassNodes":                true,
		"showMethodNodes":               true,
		"showMethodFunctionNodes":       true,
		"showMethodSubNodes":            true,
		"showPropertyNodes":             true,
		"showMemberNodes":               true,
		"showGlobalVariableNodes":       true,
		"showGlobalConstantNodes":       true,
		"showLocalVariableNodes":        true,
		"showLocalConstantNodes":        true,
		"showParameterNodes":            true,
		"showUnresolvedNodes":           true,
		"hideSingleNodes":               false,
		"hideUnreferencedGlobalSymbols": false,
		"showOutgoingSelectionLinks":    true,
		"showIncludeLinks":              true,
		"showDeclareLinks":              true,
		"showReferenceLinks":            true,
		"showAssignmentLinks":           true,
		"showCallLinks":                 true,
		"showUnresolvedLinks":           true,
		"showMemberLinks":               true,
	}
}

func graphNodeByLabel(payload graph.Payload, label string) *graph.Node {
	return graphNodeMatching(payload, func(node graph.Node) bool {
		return node.Label == label
	})
}

func buildWorkspaceGraph(t *testing.T, client *stdioTestClient, extra map[string]any) graph.Payload {
	t.Helper()
	arg := map[string]any{"scope": "workspace"}
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

func normalizeCachedGraph(payload graph.Payload) map[string]any {
	nodes := make([]map[string]any, 0, len(payload.Nodes))
	for _, node := range payload.Nodes {
		nodes = append(nodes, map[string]any{
			"id":     node.ID,
			"kind":   node.Kind,
			"label":  node.Label,
			"exists": node.Exists,
		})
	}
	sort.Slice(nodes, func(left, right int) bool {
		return mustJSONTextNoTest(nodes[left]) < mustJSONTextNoTest(nodes[right])
	})
	links := make([]map[string]any, 0, len(payload.Links))
	for _, edge := range payload.Links {
		links = append(links, map[string]any{
			"source": edge.Source,
			"target": edge.Target,
			"kind":   edge.Kind,
			"label":  edge.Label,
			"role":   edge.Role,
			"count":  edge.Count,
		})
	}
	sort.Slice(links, func(left, right int) bool {
		return mustJSONTextNoTest(links[left]) < mustJSONTextNoTest(links[right])
	})
	return map[string]any{
		"nodes": nodes,
		"links": links,
		"stats": payload.Stats,
	}
}

func mustJSONTextNoTest(value any) string {
	body, err := json.Marshal(value)
	if err != nil {
		panic(err)
	}
	return string(body)
}

func graphNodeMatching(payload graph.Payload, predicate func(graph.Node) bool) *graph.Node {
	for i := range payload.Nodes {
		if predicate(payload.Nodes[i]) {
			return &payload.Nodes[i]
		}
	}
	return nil
}

func graphHasLinkMatching(payload graph.Payload, predicate func(graph.Edge) bool) bool {
	for _, edge := range payload.Links {
		if predicate(edge) {
			return true
		}
	}
	return false
}

func expectGraphSetting(t *testing.T, payload graph.Payload, key string, expected any) {
	t.Helper()
	if payload.Settings[key] != expected {
		t.Fatalf("graph setting %s = %#v, want %#v in %s", key, payload.Settings[key], expected, mustJSONText(t, payload.Settings))
	}
}

func expectGraphNodeShape(t *testing.T, payload graph.Payload, label string, expected graph.Node) {
	t.Helper()
	node := graphNodeByLabel(payload, label)
	if node == nil {
		t.Fatalf("graph missing node %s: %s", label, mustJSONText(t, payload))
	}
	if expected.Kind != "" && node.Kind != expected.Kind {
		t.Fatalf("node %s kind = %q, want %q: %#v", label, node.Kind, expected.Kind, node)
	}
	if expected.DeclarationKind != "" && node.DeclarationKind != expected.DeclarationKind {
		t.Fatalf("node %s declarationKind = %q, want %q: %#v", label, node.DeclarationKind, expected.DeclarationKind, node)
	}
	if expected.BindingScope != "" && node.BindingScope != expected.BindingScope {
		t.Fatalf("node %s bindingScope = %q, want %q: %#v", label, node.BindingScope, expected.BindingScope, node)
	}
	if expected.Origin != "" && node.Origin != expected.Origin {
		t.Fatalf("node %s origin = %q, want %q: %#v", label, node.Origin, expected.Origin, node)
	}
	if expected.MemberOf != "" && node.MemberOf != expected.MemberOf {
		t.Fatalf("node %s memberOf = %q, want %q: %#v", label, node.MemberOf, expected.MemberOf, node)
	}
	if expected.ProcedureKind != "" && node.ProcedureKind != expected.ProcedureKind {
		t.Fatalf("node %s procedureKind = %q, want %q: %#v", label, node.ProcedureKind, expected.ProcedureKind, node)
	}
	if expected.TypeName != "" && node.TypeName != expected.TypeName {
		t.Fatalf("node %s typeName = %q, want %q: %#v", label, node.TypeName, expected.TypeName, node)
	}
	if expected.IsRoot && !node.IsRoot {
		t.Fatalf("node %s isRoot = false, want true: %#v", label, node)
	}
}

func expectArrayNode(t *testing.T, payload graph.Payload, label string, arrayKind string, dimensions []string) {
	t.Helper()
	node := graphNodeByLabel(payload, label)
	if node == nil || node.TypeName != "Array" || node.ArrayKind != arrayKind || node.ArrayDimensions == nil ||
		!stringSlicesEqual(*node.ArrayDimensions, dimensions) {
		t.Fatalf("array node %s mismatch: %#v", label, node)
	}
}

type graphSettingStringSlice []string

func (slice graphSettingStringSlice) contains(expected string) bool {
	for _, value := range slice {
		if value == expected {
			return true
		}
	}
	return false
}

func graphSettingStrings(payload graph.Payload, key string) graphSettingStringSlice {
	values, ok := payload.Settings[key].([]any)
	if !ok {
		return nil
	}
	result := make([]string, 0, len(values))
	for _, value := range values {
		if text, ok := value.(string); ok {
			result = append(result, text)
		}
	}
	return result
}

func writeGraphSettingsFixture(t *testing.T, path string, text string) {
	t.Helper()
	if err := os.WriteFile(path, []byte(text), 0o644); err != nil {
		t.Fatal(err)
	}
}
