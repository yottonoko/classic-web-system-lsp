package lspserver

import (
	"os"
	"path/filepath"
	"strconv"
	"strings"
	"testing"

	"github.com/yottonoko/classic-web-system-lsp/internal/graph"
)

func TestStdioParityBuildsFlowchartForCurrentASPFileWithIncludeMetadata(t *testing.T) {
	client := startStdioTestClient(t)
	defer client.close()

	root := t.TempDir()
	page := filepath.Join(root, "default.asp")
	common := filepath.Join(root, "common.inc")
	pageURI := pathToFileURI(page)
	commonURI := pathToFileURI(common)
	writeFlowchartFixture(t, common, `<%
Sub Included()
End Sub
%>`)
	source := `<!-- #include file="common.inc" -->
<%
Sub Main()
  On Error Resume Next
  If ready Xor disabled Then
    Call Included()
  Else
    Exit Sub
  End If
End Sub
%>`
	client.request("initialize", map[string]any{
		"processId":    nil,
		"rootUri":      pathToFileURI(root),
		"capabilities": map[string]any{},
	})
	notifyOpenClassicASPDocument(t, client, pageURI, source)

	flowchart := buildFlowchart(t, client, map[string]any{"uri": pageURI})
	if flowchart["uri"] != pageURI ||
		flowchart["fileName"] != "default.asp" ||
		flowchart["labelMode"] != "normal" {
		t.Fatalf("flowchart header mismatch: %s", mustJSONText(t, flowchart))
	}
	if !flowchartListContainsLabel(flowchart["sections"], "Sub Main") ||
		!flowchartNodeMatching(flowchart, func(node map[string]any) bool {
			return node["kind"] == "if" && strings.Contains(asString(node["label"]), "ready Xor disabled")
		}) ||
		!flowchartNodeMatching(flowchart, func(node map[string]any) bool {
			return node["kind"] == "call"
		}) ||
		!flowchartNodeMatching(flowchart, func(node map[string]any) bool {
			return node["kind"] == "exceptionHandling" && node["label"] == "Exception handling: resume next"
		}) ||
		!flowchartNodeMatching(flowchart, func(node map[string]any) bool {
			return node["kind"] == "exit"
		}) ||
		!flowchartEdgeMatching(flowchart, func(edge map[string]any) bool {
			return edge["label"] == "Yes"
		}) {
		t.Fatalf("flowchart control-flow shape mismatch: %s", mustJSONText(t, flowchart))
	}
	if flowchartNodeMatching(flowchart, func(node map[string]any) bool {
		return flowchartNodeHasAnyLinkLabel(node, []string{"Xor", "Eqv", "Imp", "And", "Not", "Or", "Mod"})
	}) {
		t.Fatalf("flowchart linked reserved operator as symbol: %s", mustJSONText(t, flowchart))
	}
	if !flowchartNodeMatching(flowchart, func(node map[string]any) bool {
		return flowchartNodeHasImplicitGlobalLink(node, "ready") ||
			flowchartNodeHasImplicitGlobalLink(node, "disabled")
	}) {
		t.Fatalf("flowchart missing implicit global links: %s", mustJSONText(t, flowchart))
	}
	if !flowchartNodeMatching(flowchart, func(node map[string]any) bool {
		return node["kind"] == "call" && flowchartNodeHasTargetURI(node, "Sub Included", commonURI)
	}) {
		t.Fatalf("flowchart missing included call target: %s", mustJSONText(t, flowchart))
	}
	includes, _ := flowchart["includes"].([]any)
	if len(includes) == 0 || !strings.Contains(mustJSONText(t, includes[0]), `"common.inc"`) ||
		!strings.Contains(mustJSONText(t, includes[0]), commonURI) {
		t.Fatalf("flowchart include metadata mismatch: %s", mustJSONText(t, flowchart["includes"]))
	}
	if !strings.Contains(asString(flowchart["mermaid"]), "flowchart TB") ||
		!strings.Contains(asString(flowchart["mermaid"]), "Sub Main") ||
		!strings.Contains(mustJSONText(t, flowchart["stats"]), `"includes":1`) {
		t.Fatalf("flowchart mermaid/stats mismatch: %s", mustJSONText(t, flowchart))
	}

	graphPayload := buildDocumentGraph(t, client, pageURI, nil)
	serializedGraph := mustJSONText(t, graphPayload)
	if strings.Contains(serializedGraph, "On Error") || strings.Contains(serializedGraph, "exceptionHandling") {
		t.Fatalf("graph payload leaked flowchart-only nodes: %s", serializedGraph)
	}

	rawFlowchart := buildFlowchart(t, client, map[string]any{"uri": pageURI, "labelMode": "raw"})
	if rawFlowchart["labelMode"] != "raw" {
		t.Fatalf("raw flowchart labelMode mismatch: %s", mustJSONText(t, rawFlowchart))
	}
	notifyConfiguration(t, client, map[string]any{"aspLsp": map[string]any{
		"flowchart": map[string]any{"labelMode": "description"},
	}})
	configuredFlowchart := buildFlowchart(t, client, map[string]any{"uri": pageURI})
	if configuredFlowchart["labelMode"] != "description" {
		t.Fatalf("configured flowchart labelMode mismatch: %s", mustJSONText(t, configuredFlowchart))
	}
}

func TestStdioParityResolvesMultiRootVirtualIncludesInFlowchartAndGraphs(t *testing.T) {
	client := startStdioTestClient(t)
	defer client.close()

	root := t.TempDir()
	firstRoot := filepath.Join(root, "root-a")
	secondRoot := filepath.Join(root, "root-b")
	includesDir := filepath.Join(firstRoot, "includes")
	if err := os.MkdirAll(includesDir, 0o755); err != nil {
		t.Fatal(err)
	}
	if err := os.MkdirAll(secondRoot, 0o755); err != nil {
		t.Fatal(err)
	}
	common := filepath.Join(includesDir, "common.inc")
	page := filepath.Join(secondRoot, "default.asp")
	commonURI := pathToFileURI(common)
	pageURI := pathToFileURI(page)
	writeFlowchartFixture(t, common, `<%
Sub SharedIncluded()
End Sub
%>`)
	source := `<!-- #include virtual="/includes/common.inc" -->
<%
Sub Main()
  Call SharedIncluded()
End Sub
%>`
	writeFlowchartFixture(t, page, source)
	client.request("initialize", map[string]any{
		"processId": nil,
		"rootUri":   pathToFileURI(secondRoot),
		"workspaceFolders": []map[string]any{
			{"uri": pathToFileURI(secondRoot), "name": "root-b"},
			{"uri": pathToFileURI(firstRoot), "name": "root-a"},
		},
		"capabilities": map[string]any{},
	})
	notifyOpenClassicASPDocument(t, client, pageURI, source)

	flowchart := buildFlowchart(t, client, map[string]any{"uri": pageURI})
	if !flowchartNodeMatching(flowchart, func(node map[string]any) bool {
		return node["kind"] == "call" && flowchartNodeHasTargetURI(node, "Sub SharedIncluded", commonURI)
	}) {
		t.Fatalf("flowchart missing multi-root included target: %s", mustJSONText(t, flowchart))
	}
	if !strings.Contains(mustJSONText(t, flowchart["includes"]), commonURI) {
		t.Fatalf("flowchart include metadata missing multi-root resolved URI: %s", mustJSONText(t, flowchart["includes"]))
	}

	documentGraph := buildDocumentGraph(t, client, pageURI, nil)
	if graphNodeMatching(documentGraph, func(node graph.Node) bool {
		return node.Kind == "vbDeclaration" && node.Label == "SharedIncluded" && node.URI == commonURI
	}) == nil || !graphHasLinkMatching(documentGraph, func(edge graph.Edge) bool {
		return edge.Kind == "include" && edge.Include != nil && edge.Include.ResolvedURI == commonURI
	}) {
		t.Fatalf("document graph missing multi-root include: %s", mustJSONText(t, documentGraph))
	}
	workspaceGraph := buildWorkspaceGraph(t, client, nil)
	if graphNodeMatching(workspaceGraph, func(node graph.Node) bool {
		return node.Kind == "file" && node.URI == commonURI
	}) == nil || graphNodeMatching(workspaceGraph, func(node graph.Node) bool {
		return node.Kind == "file" && node.URI == pageURI
	}) == nil || !graphHasLinkMatching(workspaceGraph, func(edge graph.Edge) bool {
		return edge.Kind == "include" && edge.Include != nil && edge.Include.ResolvedURI == commonURI
	}) {
		t.Fatalf("workspace graph missing multi-root include: %s", mustJSONText(t, workspaceGraph))
	}
}

func TestStdioParityBuildsMermaidFlowSectionsForVBScriptControlFlow(t *testing.T) {
	client := startStdioTestClient(t)
	defer client.close()

	root := t.TempDir()
	page := filepath.Join(root, "control-flow.asp")
	pageURI := pathToFileURI(page)
	source := `<%
Sub Main()
  If enabled Then branchValue = 2 Else fallbackValue = "fallback"
  If ready Then
    Call Render()
  ElseIf fallback Then
    Exit Sub
  Else
    Response.Write "x"
  End If
  Select Case kind
  Case "a"
    Call A()
  Case Else
    Call B()
  End Select
  For index = 1 To 3
    Call Tick()
  Next
  Do While active
    Call Tick()
  Loop
  While waiting
    Call Tick()
  Wend
End Sub
%>`
	client.request("initialize", map[string]any{
		"processId":    nil,
		"rootUri":      pathToFileURI(root),
		"capabilities": map[string]any{},
	})
	notifyOpenClassicASPDocument(t, client, pageURI, source)

	flowchart := buildFlowchart(t, client, map[string]any{"uri": pageURI})
	if !flowchartListContainsLabel(flowchart["sections"], "Sub Main") {
		t.Fatalf("flowchart missing Sub Main section: %s", mustJSONText(t, flowchart))
	}
	for _, kind := range []string{
		"if",
		"elseif",
		"else",
		"select",
		"case",
		"for",
		"do",
		"while",
		"call",
		"exit",
		"statement",
	} {
		if !flowchartNodeMatching(flowchart, func(node map[string]any) bool { return node["kind"] == kind }) {
			t.Fatalf("flowchart missing node kind %s: %s", kind, mustJSONText(t, flowchart))
		}
	}
	if !flowchartEdgeMatching(flowchart, func(edge map[string]any) bool { return edge["label"] == "Yes" }) ||
		!flowchartEdgeMatching(flowchart, func(edge map[string]any) bool { return edge["label"] == "Repeat" }) {
		t.Fatalf("flowchart missing branch or loop edge labels: %s", mustJSONText(t, flowchart))
	}
	if !strings.Contains(asString(flowchart["mermaid"]), "flowchart TB") ||
		!strings.Contains(asString(flowchart["mermaid"]), "Sub Main") ||
		!strings.Contains(asString(flowchart["mermaid"]), `When &quot;a&quot;`) {
		t.Fatalf("flowchart mermaid mismatch: %s", mustJSONText(t, flowchart))
	}
	stats, _ := flowchart["stats"].(map[string]any)
	nodes, _ := flowchart["nodes"].([]any)
	if stats["nodes"] != float64(len(nodes)) {
		t.Fatalf("flowchart node stats mismatch: %s", mustJSONText(t, flowchart))
	}
	section := flowchartSectionByLabel(flowchart, "Sub Main")
	nodeIDs, _ := section["nodeIds"].([]any)
	if len(nodeIDs) == 0 {
		t.Fatalf("flowchart section missing nodeIds: %s", mustJSONText(t, section))
	}
	if !flowchartNodeMatching(flowchart, func(node map[string]any) bool {
		return node["kind"] == "select" && node["sectionId"] == section["id"]
	}) {
		t.Fatalf("flowchart nodes missing sectionId membership: %s", mustJSONText(t, flowchart))
	}
	if !flowchartEdgeMatching(flowchart, func(edge map[string]any) bool {
		return edge["label"] == "Repeat" && edge["sectionId"] == section["id"]
	}) {
		t.Fatalf("flowchart edges missing sectionId membership: %s", mustJSONText(t, flowchart))
	}
}

func TestStdioParitySplitsColonSeparatedFlowchartControlStatements(t *testing.T) {
	client := startStdioTestClient(t)
	defer client.close()

	root := t.TempDir()
	page := filepath.Join(root, "colon-control-flow.asp")
	pageURI := pathToFileURI(page)
	source := `<%
Sub Main() : Dim value : value = 1 : If value = 1 Then Response.Write value : Select Case value : Case 1 : Exit For : Case Else : Exit Do : End Select : End Sub
%>`
	client.request("initialize", map[string]any{
		"processId":    nil,
		"rootUri":      pathToFileURI(root),
		"capabilities": map[string]any{},
	})
	notifyOpenClassicASPDocument(t, client, pageURI, source)

	flowchart := buildFlowchart(t, client, map[string]any{"uri": pageURI})
	if !flowchartListContainsLabel(flowchart["sections"], "Sub Main") {
		t.Fatalf("colon flowchart missing Sub Main section: %s", mustJSONText(t, flowchart))
	}
	for _, kind := range []string{"declaration", "statement", "if", "select", "case", "exit"} {
		if !flowchartNodeMatching(flowchart, func(node map[string]any) bool { return node["kind"] == kind }) {
			t.Fatalf("colon flowchart missing node kind %s: %s", kind, mustJSONText(t, flowchart))
		}
	}
	if labels := flowchartLabelsByKind(flowchart, "case"); !sameStrings(labels, []string{"When 1", "Else"}) {
		t.Fatalf("colon flowchart case labels mismatch: %#v in %s", labels, mustJSONText(t, flowchart))
	}
	if labels := flowchartLabelsByKind(flowchart, "exit"); !sameStrings(labels, []string{"Exit For", "Exit Do"}) {
		t.Fatalf("colon flowchart exit labels mismatch: %#v in %s", labels, mustJSONText(t, flowchart))
	}
}

func TestStdioParityKeepsSplitASPIslandsAndBlockTerminatorRangesInFlowchart(t *testing.T) {
	client := startStdioTestClient(t)
	defer client.close()

	root := t.TempDir()
	splitPage := filepath.Join(root, "split.asp")
	blockPage := filepath.Join(root, "block-ranges.asp")
	splitURI := pathToFileURI(splitPage)
	blockURI := pathToFileURI(blockPage)
	splitSource := `<% If enabled Then %>
<div><%= title %></div>
<% Call Render("😀") %>
<% End If %>`
	blockSource := `<%
Sub Main()
  If ready Then
    Call Render()
  End If
  Select Case kind
  Case "a"
    Call A()
  End Select
  For index = 1 To 3
    Call Tick()
  Next
  For Each item In items
    Call Tick()
  Next
  Do While active
    Call Tick()
  Loop
  While waiting
    Call Tick()
  Wend
End Sub
%>`
	client.request("initialize", map[string]any{
		"processId":    nil,
		"rootUri":      pathToFileURI(root),
		"capabilities": map[string]any{},
	})
	notifyOpenClassicASPDocument(t, client, splitURI, splitSource)
	notifyOpenClassicASPDocument(t, client, blockURI, blockSource)

	splitFlowchart := buildFlowchart(t, client, map[string]any{"uri": splitURI})
	if splitFlowchart["sourceText"] != splitSource {
		t.Fatalf("split flowchart sourceText mismatch: %s", mustJSONText(t, splitFlowchart))
	}
	ifNode := flowchartFirstNodeByKind(splitFlowchart, "if")
	callNode := flowchartFirstNodeByKind(splitFlowchart, "call")
	topLevel := flowchartFirstSectionByKind(splitFlowchart, "topLevel")
	if ifNode["label"] != "Check enabled" ||
		!sameRange(ifNode["range"], 0, 3, 3, len("<% End If")) {
		t.Fatalf("split if node range mismatch: %s", mustJSONText(t, ifNode))
	}
	if callNode["label"] != `Call Render("😀")` ||
		!sameRange(callNode["range"], 2, 3, 2, utf16Len(`<% Call Render("😀")`)) {
		t.Fatalf("split call node range mismatch: %s", mustJSONText(t, callNode))
	}
	if !sameRange(topLevel["range"], 0, 3, 3, len("<% End If")) {
		t.Fatalf("split top-level section range mismatch: %s", mustJSONText(t, topLevel))
	}

	blockFlowchart := buildFlowchart(t, client, map[string]any{"uri": blockURI})
	for _, want := range []struct {
		kind      string
		startLine int
		startChar int
		endLine   int
		endChar   int
	}{
		{"if", 2, 2, 4, len("  End If")},
		{"select", 5, 2, 8, len("  End Select")},
		{"for", 9, 2, 11, len("  Next")},
		{"forEach", 12, 2, 14, len("  Next")},
		{"do", 15, 2, 17, len("  Loop")},
		{"while", 18, 2, 20, len("  Wend")},
	} {
		node := flowchartFirstNodeByKind(blockFlowchart, want.kind)
		if !sameRange(node["range"], want.startLine, want.startChar, want.endLine, want.endChar) {
			t.Fatalf("block %s range mismatch: %s", want.kind, mustJSONText(t, node))
		}
	}
}

func TestStdioParityUsesWholeProcedureRangesAndWrapsLongFlowchartLabelsWithoutSemanticSplitting(t *testing.T) {
	client := startStdioTestClient(t)
	defer client.close()

	root := t.TempDir()
	page := filepath.Join(root, "long-label.asp")
	uri := pathToFileURI(page)
	parts := make([]string, 40)
	for i := range parts {
		parts[i] = "part" + strconv.Itoa(i)
	}
	source := `<%
Sub Main()
  message = "` + strings.Join(parts, "-") + `"
End Sub
%>`
	client.request("initialize", map[string]any{
		"processId":    nil,
		"rootUri":      pathToFileURI(root),
		"capabilities": map[string]any{},
	})
	notifyOpenClassicASPDocument(t, client, uri, source)

	flowchart := buildFlowchart(t, client, map[string]any{"uri": uri})
	section := flowchartSectionByLabel(flowchart, "Sub Main")
	if !sameRange(section["range"], 1, 0, 3, len("End Sub")) {
		t.Fatalf("procedure section range mismatch: %s", mustJSONText(t, section))
	}
	bodyNodes := flowchartNodesExceptKinds(flowchart, map[string]struct{}{
		"start": {},
		"end":   {},
	})
	if len(bodyNodes) != 1 {
		t.Fatalf("long statement was semantically split: %s", mustJSONText(t, flowchart))
	}
	label := asString(bodyNodes[0]["label"])
	if !strings.Contains(label, "part0") || !strings.Contains(label, "part39") || strings.Contains(label, "...") {
		t.Fatalf("flowchart label lost content: %q in %s", label, mustJSONText(t, flowchart))
	}
	if !strings.Contains(asString(flowchart["mermaid"]), "<br/>") {
		t.Fatalf("long Mermaid label was not visually wrapped: %s", mustJSONText(t, flowchart))
	}
}

func TestStdioParityBuildsDeterministicCFGForLoopExitsAndPostTestDo(t *testing.T) {
	client := startStdioTestClient(t)
	defer client.close()

	root := t.TempDir()
	page := filepath.Join(root, "cfg.asp")
	uri := pathToFileURI(page)
	source := `<%
Sub Main()
  For index = 1 To 3
    If skip Then Exit For
    Call Tick()
  Next
  Do
    Response.Flush
  Loop Until done
End Sub
%>`
	client.request("initialize", map[string]any{
		"processId": nil, "rootUri": pathToFileURI(root), "capabilities": map[string]any{},
	})
	notifyOpenClassicASPDocument(t, client, uri, source)

	flowchart := buildFlowchart(t, client, map[string]any{"uri": uri, "labelMode": "raw"})
	for _, value := range flowchart["edges"].([]any) {
		edge := value.(map[string]any)
		if edge["source"] == edge["target"] {
			t.Fatalf("CFG contains legacy self-loop: %s", mustJSONText(t, edge))
		}
	}
	if !flowchartHasEdgeBetweenLabels(flowchart, "Exit For", "After For", "Exit") {
		t.Fatalf("Exit For does not target the enclosing loop exit: %s", mustJSONText(t, flowchart))
	}
	if !flowchartHasEdgeBetweenLabels(flowchart, "Loop Until done", "Do", "No") ||
		!flowchartHasEdgeBetweenLabels(flowchart, "Loop Until done", "After Do", "Yes") {
		t.Fatalf("Loop Until post-test edges are incorrect: %s", mustJSONText(t, flowchart))
	}
	if !flowchartHasLabel(flowchart, "Response.Flush") ||
		!strings.Contains(asString(flowchart["mermaid"]), `-- "Repeat" -->`) {
		t.Fatalf("CFG lost executable statements or Mermaid edges: %s", mustJSONText(t, flowchart))
	}
}

func TestStdioParityBuildsSeparateTopLevelFunctionAndPropertySections(t *testing.T) {
	client := startStdioTestClient(t)
	defer client.close()

	root := t.TempDir()
	page := filepath.Join(root, "sections.asp")
	uri := pathToFileURI(page)
	source := `<%
topValue = 1
Function Calculate()
  Calculate = 2
  Exit Function
End Function
Class Widget
  Property Get Name()
    Name = "widget"
  End Property
End Class
%>`
	client.request("initialize", map[string]any{
		"processId": nil, "rootUri": pathToFileURI(root), "capabilities": map[string]any{},
	})
	notifyOpenClassicASPDocument(t, client, uri, source)

	flowchart := buildFlowchart(t, client, map[string]any{"uri": uri, "labelMode": "raw"})
	for _, label := range []string{"Top level", "Function Calculate", "Property Name"} {
		if !flowchartListContainsLabel(flowchart["sections"], label) {
			t.Fatalf("flowchart missing %q section: %s", label, mustJSONText(t, flowchart))
		}
	}
	if !flowchartHasLabel(flowchart, "topValue = 1") || !flowchartHasLabel(flowchart, `Name = "widget"`) {
		t.Fatalf("flowchart lost top-level or property statements: %s", mustJSONText(t, flowchart))
	}
}

func TestStdioParityKeepsContinuedStatementsInOneFlowchartNode(t *testing.T) {
	client := startStdioTestClient(t)
	defer client.close()
	root := t.TempDir()
	page := filepath.Join(root, "continued.asp")
	source := `<%
Sub Main()
  total = first + _
    second + _
    third
End Sub
%>`
	uri := pathToFileURI(page)
	client.request("initialize", map[string]any{
		"processId": nil, "rootUri": pathToFileURI(root), "capabilities": map[string]any{},
	})
	notifyOpenClassicASPDocument(t, client, uri, source)
	flowchart := buildFlowchart(t, client, map[string]any{"uri": uri, "labelMode": "raw"})
	labels := flowchartLabelsByKind(flowchart, "statement")
	if !sameStrings(labels, []string{"total = first + second + third"}) {
		t.Fatalf("continued statement labels = %#v, want one logical node: %s", labels, mustJSONText(t, flowchart))
	}
}

func TestStdioParityUsesDedicatedFlowchartKindAndLabelsForOnErrorStatements(t *testing.T) {
	client := startStdioTestClient(t)
	defer client.close()

	root := t.TempDir()
	page := filepath.Join(root, "errors.asp")
	uri := pathToFileURI(page)
	source := `<%
Sub Main()
  On Error Resume Next
  FailingCall()
  On Error GoTo 0
End Sub
%>`
	client.request("initialize", map[string]any{
		"processId":    nil,
		"rootUri":      pathToFileURI(root),
		"capabilities": map[string]any{},
	})
	notifyOpenClassicASPDocument(t, client, uri, source)

	normal := buildFlowchart(t, client, map[string]any{"uri": uri, "labelMode": "normal", "locale": "ja"})
	raw := buildFlowchart(t, client, map[string]any{"uri": uri, "labelMode": "raw", "locale": "ja"})
	description := buildFlowchart(t, client, map[string]any{"uri": uri, "labelMode": "description", "locale": "ja"})

	if labels := flowchartLabelsByKind(normal, "exceptionHandling"); !sameStrings(labels, []string{"例外処理: エラー時は次へ進む", "例外処理を解除"}) {
		t.Fatalf("normal on error labels mismatch: %#v in %s", labels, mustJSONText(t, normal))
	}
	if labels := flowchartLabelsByKind(raw, "exceptionHandling"); !sameStrings(labels, []string{"On Error Resume Next", "On Error GoTo 0"}) {
		t.Fatalf("raw on error labels mismatch: %#v in %s", labels, mustJSONText(t, raw))
	}
	if labels := flowchartLabelsByKind(description, "exceptionHandling"); !sameStrings(labels, []string{
		"エラーが発生しても処理を止めず、次のステートメントから続行",
		"現在の例外処理を解除し、以後のエラーを通常どおり発生",
	}) {
		t.Fatalf("description on error labels mismatch: %#v in %s", labels, mustJSONText(t, description))
	}
}

func TestStdioParitySupportsNormalRawAndDescriptionFlowchartLabelModes(t *testing.T) {
	client := startStdioTestClient(t)
	defer client.close()

	root := t.TempDir()
	page := filepath.Join(root, "labels.asp")
	uri := pathToFileURI(page)
	source := `<%
Sub Main()
  a = a + 100
  F(1, 2, 3, 4, 5)
  If a = 100 Then
    Call Done()
  End If
End Sub
%>`
	client.request("initialize", map[string]any{
		"processId":    nil,
		"rootUri":      pathToFileURI(root),
		"capabilities": map[string]any{},
	})
	notifyOpenClassicASPDocument(t, client, uri, source)

	normal := buildFlowchart(t, client, map[string]any{"uri": uri, "labelMode": "normal", "locale": "ja"})
	raw := buildFlowchart(t, client, map[string]any{"uri": uri, "labelMode": "raw", "locale": "ja"})
	description := buildFlowchart(t, client, map[string]any{"uri": uri, "labelMode": "description", "locale": "ja"})

	if normal["labelMode"] != "normal" || raw["labelMode"] != "raw" || description["labelMode"] != "description" {
		t.Fatalf("labelMode header mismatch: normal=%s raw=%s description=%s", mustJSONText(t, normal), mustJSONText(t, raw), mustJSONText(t, description))
	}
	if !flowchartHasLabel(normal, "aにa + 100を代入") {
		t.Fatalf("normal label mode missing assignment label: %s", mustJSONText(t, normal))
	}
	if !flowchartHasLabel(raw, "a = a + 100") ||
		!flowchartLabelMatching(raw, func(label string) bool {
			return strings.Contains(label, "F") && strings.Contains(label, "1") && strings.Contains(label, "5")
		}) {
		t.Fatalf("raw label mode mismatch: %s", mustJSONText(t, raw))
	}
	for _, label := range []string{
		"aに100を加算",
		"Fを引数1、2、3、4、5で呼び出し",
		"aが100と等しいを判定",
	} {
		if !flowchartHasLabel(description, label) {
			t.Fatalf("description label mode missing %q: %s", label, mustJSONText(t, description))
		}
	}
}

func TestStdioParitySplitsComplexArgumentCallsForReadableFlowchartModes(t *testing.T) {
	client := startStdioTestClient(t)
	defer client.close()

	root := t.TempDir()
	page := filepath.Join(root, "complex-args.asp")
	uri := pathToFileURI(page)
	source := `<%
Sub Main()
  Call Render(FormatName(GetUser(id)), Now())
End Sub
%>`
	client.request("initialize", map[string]any{
		"processId":    nil,
		"rootUri":      pathToFileURI(root),
		"capabilities": map[string]any{},
	})
	notifyOpenClassicASPDocument(t, client, uri, source)

	normal := buildFlowchart(t, client, map[string]any{"uri": uri, "labelMode": "normal", "labelLineLength": 140})
	raw := buildFlowchart(t, client, map[string]any{"uri": uri, "labelMode": "raw", "labelLineLength": 140})
	description := buildFlowchart(t, client, map[string]any{"uri": uri, "labelMode": "description", "locale": "ja", "labelLineLength": 140})

	if labels := flowchartLabelsByKind(normal, "call"); !sameStrings(labels, []string{
		"Call GetUser(id)",
		"Call FormatName(GetUser(id))",
		"Call Now",
		"Call Render(FormatName(GetUser(id)), Now())",
	}) {
		t.Fatalf("normal complex call labels mismatch: %#v in %s", labels, mustJSONText(t, normal))
	}
	if labels := flowchartLabelsByKind(raw, "call"); !sameStrings(labels, []string{
		"Call Render(FormatName(GetUser(id)), Now())",
	}) {
		t.Fatalf("raw complex call labels mismatch: %#v in %s", labels, mustJSONText(t, raw))
	}
	if labels := flowchartLabelsByKind(description, "call"); !sameStrings(labels, []string{
		"GetUserを引数idで呼び出し",
		"FormatNameを引数GetUser(id)で呼び出し",
		"Nowを呼び出し",
		"Renderを引数FormatName(GetUser(id))、Now()で呼び出し",
	}) {
		t.Fatalf("description complex call labels mismatch: %#v in %s", labels, mustJSONText(t, description))
	}
}

func TestStdioParityMarksImplicitGlobalVariableLinksWithDistinctFlowchartSymbolKind(t *testing.T) {
	client := startStdioTestClient(t)
	defer client.close()

	root := t.TempDir()
	page := filepath.Join(root, "unresolved-global.asp")
	uri := pathToFileURI(page)
	source := `<%
Sub Main()
  MissingValue = 1
End Sub
%>`
	client.request("initialize", map[string]any{
		"processId":    nil,
		"rootUri":      pathToFileURI(root),
		"capabilities": map[string]any{},
	})
	notifyOpenClassicASPDocument(t, client, uri, source)

	flowchart := buildFlowchart(t, client, map[string]any{"uri": uri})
	if !flowchartNodeMatching(flowchart, func(node map[string]any) bool {
		return strings.Contains(asString(node["label"]), "MissingValue") &&
			flowchartNodeHasImplicitGlobalLink(node, "MissingValue")
	}) {
		t.Fatalf("flowchart missing implicit global variable link kind: %s", mustJSONText(t, flowchart))
	}
}

func TestStdioParityLabelsResolvedVariablesConstantsAndParametersWithScopeInFlowcharts(t *testing.T) {
	client := startStdioTestClient(t)
	defer client.close()

	root := t.TempDir()
	page := filepath.Join(root, "scoped.asp")
	uri := pathToFileURI(page)
	source := `<%
Dim GlobalValue
Const GlobalLimit = 10
Sub Main(argValue)
  Dim LocalValue
  Const LocalLimit = 1
  LocalValue = GlobalLimit
  Call Render(argValue)
End Sub
Sub Render(value)
End Sub
%>`
	client.request("initialize", map[string]any{
		"processId":    nil,
		"rootUri":      pathToFileURI(root),
		"capabilities": map[string]any{},
	})
	notifyOpenClassicASPDocument(t, client, uri, source)

	normal := buildFlowchart(t, client, map[string]any{"uri": uri, "labelLineLength": 140})
	description := buildFlowchart(t, client, map[string]any{"uri": uri, "labelMode": "description", "labelLineLength": 140})
	for _, label := range []string{
		"Declare global variable GlobalValue",
		"Declare global constant GlobalLimit",
		"Declare local variable LocalValue",
		"Declare local constant LocalLimit",
		"Assign global constant GlobalLimit to local variable LocalValue",
		"Call Sub Render(parameter argValue)",
	} {
		if !flowchartHasLabel(normal, label) {
			t.Fatalf("normal scoped flowchart missing %q: %s", label, mustJSONText(t, normal))
		}
	}
	if !flowchartNodeMatching(normal, func(node map[string]any) bool {
		return node["label"] == "Assign global constant GlobalLimit to local variable LocalValue" &&
			flowchartNodeHasLinkLabel(node, "local variable LocalValue") &&
			flowchartNodeHasLinkLabel(node, "global constant GlobalLimit")
	}) {
		t.Fatalf("scoped assignment links mismatch: %s", mustJSONText(t, normal))
	}
	for _, label := range []string{
		"Assign global constant GlobalLimit to local variable LocalValue",
		"Call Sub Render with parameter argValue",
	} {
		if !flowchartHasLabel(description, label) {
			t.Fatalf("description scoped flowchart missing %q: %s", label, mustJSONText(t, description))
		}
	}
}

func TestStdioParityBuildsNavigationGraphForStaticASPScreenTransitions(t *testing.T) {
	client := startStdioTestClient(t)
	defer client.close()

	root := t.TempDir()
	page := filepath.Join(root, "default.asp")
	writeFlowchartFixture(t, page, `<!-- #include file="common.inc" -->
<a href="products.asp?category=1">Products</a>
<form action="search.asp" method="get"><input type="hidden" name="q" value="classic"></form>
<iframe src="frame.asp"></iframe>
<script>
const next = "client.asp";
location.href = next;
const f = document.forms["jump"];
f.action = "submit.asp";
f.method = "post";
f.submit();
history.pushState({}, "", "history.asp");
</script>
<%
target = "redirect.asp?next=" & Request.QueryString("next")
Response.Redirect target
%>`)
	writeFlowchartFixture(t, filepath.Join(root, "common.inc"), `<a href="included.asp">Included</a>`)
	for _, target := range []string{
		"products.asp",
		"search.asp",
		"frame.asp",
		"client.asp",
		"submit.asp",
		"history.asp",
		"included.asp",
	} {
		writeFlowchartFixture(t, filepath.Join(root, target), "")
	}
	pageURI := pathToFileURI(page)
	client.request("initialize", map[string]any{
		"processId":    nil,
		"rootUri":      pathToFileURI(root),
		"capabilities": map[string]any{},
	})

	payload := buildNavigationGraph(t, client, map[string]any{"scope": "document", "uri": pageURI})
	if payload["scope"] != "document" {
		t.Fatalf("navigation graph scope mismatch: %s", mustJSONText(t, payload))
	}
	for _, kind := range []string{
		"htmlAnchor",
		"htmlForm",
		"htmlFrame",
		"serverRedirect",
		"javascriptLocation",
		"javascriptHistory",
		"javascriptFormSubmit",
	} {
		if !navigationEdgeMatching(payload, func(edge map[string]any) bool { return edge["kind"] == kind }) {
			t.Fatalf("navigation graph missing edge kind %s: %s", kind, mustJSONText(t, payload))
		}
	}
	if !navigationNodeMatching(payload, func(node map[string]any) bool { return node["label"] == "included.asp" }) {
		t.Fatalf("navigation graph missing included.asp node: %s", mustJSONText(t, payload))
	}
	if !navigationEdgeMatching(payload, func(edge map[string]any) bool {
		return edge["kind"] == "htmlForm" &&
			edge["method"] == "GET" &&
			navigationEdgeHasParameter(edge, "q", "hiddenInput")
	}) {
		t.Fatalf("navigation graph missing html form hidden parameter: %s", mustJSONText(t, payload))
	}
	if !navigationEdgeMatching(payload, func(edge map[string]any) bool {
		return edge["kind"] == "serverRedirect" &&
			navigationEdgeHasParameter(edge, "next", "queryString")
	}) {
		t.Fatalf("navigation graph missing server redirect query parameter: %s", mustJSONText(t, payload))
	}
	stats, _ := payload["stats"].(map[string]any)
	if documents, _ := stats["documents"].(float64); documents < 2 {
		t.Fatalf("navigation graph documents stat mismatch: %s", mustJSONText(t, payload))
	}
}

func TestStdioParityExtractsHTMLNavigationTagsAndGeneratedHTML(t *testing.T) {
	client := startStdioTestClient(t)
	defer client.close()

	root := t.TempDir()
	page := filepath.Join(root, "navigation-tags.asp")
	pageURI := pathToFileURI(page)
	writeFlowchartFixture(t, page, `<a href="next.asp?id=1">Next</a>
<area href="map.asp">
<iframe name="detail" src="detail.asp"></iframe>
<frame src="legacy.asp">
<form action="save.asp" method="post"><input type="hidden" name="token" value="abc"><button formaction="delete.asp" formmethod="post" name="delete" value="1"></button></form>
<meta http-equiv="refresh" content="0; url=refresh.asp">
<% Response.Write "<a href=""generated.asp"">Generated</a><form action=""posted.asp""><input type=""hidden"" name=""mode"" value=""x""></form>" %>`)
	for _, target := range []string{
		"next.asp",
		"map.asp",
		"detail.asp",
		"legacy.asp",
		"save.asp",
		"delete.asp",
		"refresh.asp",
		"generated.asp",
		"posted.asp",
	} {
		writeFlowchartFixture(t, filepath.Join(root, target), "")
	}
	client.request("initialize", map[string]any{
		"processId":    nil,
		"rootUri":      pathToFileURI(root),
		"capabilities": map[string]any{},
	})

	payload := buildNavigationGraph(t, client, map[string]any{"scope": "document", "uri": pageURI})
	for _, want := range []struct {
		kind  string
		label string
	}{
		{"htmlAnchor", "next.asp"},
		{"htmlAnchor", "map.asp"},
		{"htmlFrame", "detail.asp"},
		{"htmlFrame", "legacy.asp"},
		{"htmlForm", "save.asp"},
		{"htmlForm", "delete.asp"},
		{"metaRefresh", "refresh.asp"},
		{"htmlAnchor", "generated.asp"},
		{"htmlForm", "posted.asp"},
	} {
		if !navigationEdgeMatching(payload, func(edge map[string]any) bool {
			target := navigationNodeByID(payload, asString(edge["target"]))
			return edge["kind"] == want.kind && target["label"] == want.label
		}) {
			t.Fatalf("navigation graph missing %s edge to %s: %s", want.kind, want.label, mustJSONText(t, payload))
		}
	}
	if !navigationEdgeMatching(payload, func(edge map[string]any) bool {
		target := navigationNodeByID(payload, asString(edge["target"]))
		return edge["kind"] == "htmlForm" &&
			edge["method"] == "POST" &&
			target["label"] == "save.asp" &&
			navigationEdgeHasParameterValue(edge, "token", "hiddenInput", "abc")
	}) {
		t.Fatalf("navigation graph missing form hidden parameter: %s", mustJSONText(t, payload))
	}
	if !navigationEdgeMatching(payload, func(edge map[string]any) bool {
		target := navigationNodeByID(payload, asString(edge["target"]))
		return edge["kind"] == "htmlForm" &&
			edge["method"] == "POST" &&
			target["label"] == "delete.asp" &&
			navigationEdgeHasParameterValue(edge, "delete", "formControl", "1")
	}) {
		t.Fatalf("navigation graph missing formaction form control parameter: %s", mustJSONText(t, payload))
	}
	if !navigationEdgeMatching(payload, func(edge map[string]any) bool {
		target := navigationNodeByID(payload, asString(edge["target"]))
		return edge["kind"] == "htmlForm" &&
			target["label"] == "posted.asp" &&
			navigationEdgeHasParameterValue(edge, "mode", "hiddenInput", "x")
	}) {
		t.Fatalf("navigation graph missing generated html form parameter: %s", mustJSONText(t, payload))
	}
}

func TestStdioParityExtractsFormControlsOutsideFormsAndDecodesHTMLNavigationEntities(t *testing.T) {
	client := startStdioTestClient(t)
	defer client.close()

	root := t.TempDir()
	page := filepath.Join(root, "navigation-controls.asp")
	pageURI := pathToFileURI(page)
	writeFlowchartFixture(t, page, `<a href="next.asp?a=1&amp;b=two">Next</a>
<form action="save.asp">
  <select name="choice"><option value="one">One</option></select>
  <textarea name="memo"></textarea>
</form>
<button formaction="outside.asp" formmethod="post" name="outside" value="1"></button>`)
	for _, target := range []string{"next.asp", "save.asp", "outside.asp"} {
		writeFlowchartFixture(t, filepath.Join(root, target), "")
	}
	client.request("initialize", map[string]any{
		"processId": nil, "rootUri": pathToFileURI(root), "capabilities": map[string]any{},
	})

	payload := buildNavigationGraph(t, client, map[string]any{"scope": "document", "uri": pageURI})
	if !navigationEdgeMatching(payload, func(edge map[string]any) bool {
		target := navigationNodeByID(payload, asString(edge["target"]))
		return edge["kind"] == "htmlForm" && target["label"] == "save.asp" &&
			navigationEdgeHasParameter(edge, "choice", "formControl") &&
			navigationEdgeHasParameter(edge, "memo", "formControl")
	}) {
		t.Fatalf("navigation graph missing select/textarea form parameters: %s", mustJSONText(t, payload))
	}
	if !navigationEdgeMatching(payload, func(edge map[string]any) bool {
		target := navigationNodeByID(payload, asString(edge["target"]))
		return edge["kind"] == "htmlForm" && edge["method"] == "POST" && target["label"] == "outside.asp" &&
			navigationEdgeHasParameterValue(edge, "outside", "formControl", "1")
	}) {
		t.Fatalf("navigation graph missing formaction outside form: %s", mustJSONText(t, payload))
	}
	if !navigationEdgeMatching(payload, func(edge map[string]any) bool {
		if edge["kind"] != "htmlAnchor" {
			return false
		}
		return navigationEdgeHasParameterValue(edge, "a", "queryString", "1") &&
			navigationEdgeHasParameterValue(edge, "b", "queryString", "two")
	}) {
		t.Fatalf("navigation graph did not decode HTML entities in href: %s", mustJSONText(t, payload))
	}
}

func buildFlowchart(t *testing.T, client *stdioTestClient, arg map[string]any) map[string]any {
	t.Helper()
	response := client.request("workspace/executeCommand", map[string]any{
		"command":   "aspLsp.server.buildFlowchart",
		"arguments": []map[string]any{arg},
	})
	var payload map[string]any
	mustDecodeResult(t, response.Result, &payload)
	return payload
}

func buildNavigationGraph(t *testing.T, client *stdioTestClient, arg map[string]any) map[string]any {
	t.Helper()
	response := client.request("workspace/executeCommand", map[string]any{
		"command":   "aspLsp.server.buildNavigationGraph",
		"arguments": []map[string]any{arg},
	})
	var payload map[string]any
	mustDecodeResult(t, response.Result, &payload)
	return payload
}

func flowchartListContainsLabel(value any, label string) bool {
	items, _ := value.([]any)
	for _, item := range items {
		object, _ := item.(map[string]any)
		if object["label"] == label {
			return true
		}
	}
	return false
}

func flowchartNodeMatching(payload map[string]any, predicate func(map[string]any) bool) bool {
	nodes, _ := payload["nodes"].([]any)
	for _, item := range nodes {
		node, _ := item.(map[string]any)
		if node != nil && predicate(node) {
			return true
		}
	}
	return false
}

func flowchartEdgeMatching(payload map[string]any, predicate func(map[string]any) bool) bool {
	edges, _ := payload["edges"].([]any)
	for _, item := range edges {
		edge, _ := item.(map[string]any)
		if edge != nil && predicate(edge) {
			return true
		}
	}
	return false
}

func flowchartHasEdgeBetweenLabels(payload map[string]any, sourceLabel, targetLabel, edgeLabel string) bool {
	labelsByID := map[any]string{}
	for _, value := range payload["nodes"].([]any) {
		node := value.(map[string]any)
		labelsByID[node["id"]] = asString(node["label"])
	}
	return flowchartEdgeMatching(payload, func(edge map[string]any) bool {
		return labelsByID[edge["source"]] == sourceLabel &&
			labelsByID[edge["target"]] == targetLabel &&
			asString(edge["label"]) == edgeLabel
	})
}

func flowchartFirstNodeByKind(payload map[string]any, kind string) map[string]any {
	nodes, _ := payload["nodes"].([]any)
	for _, item := range nodes {
		node, _ := item.(map[string]any)
		if node != nil && node["kind"] == kind {
			return node
		}
	}
	return map[string]any{}
}

func flowchartFirstSectionByKind(payload map[string]any, kind string) map[string]any {
	sections, _ := payload["sections"].([]any)
	for _, item := range sections {
		section, _ := item.(map[string]any)
		if section != nil && section["kind"] == kind {
			return section
		}
	}
	return map[string]any{}
}

func flowchartSectionByLabel(payload map[string]any, label string) map[string]any {
	sections, _ := payload["sections"].([]any)
	for _, item := range sections {
		section, _ := item.(map[string]any)
		if section != nil && section["label"] == label {
			return section
		}
	}
	return map[string]any{}
}

func flowchartNodesExceptKinds(payload map[string]any, excluded map[string]struct{}) []map[string]any {
	nodes, _ := payload["nodes"].([]any)
	result := []map[string]any{}
	for _, item := range nodes {
		node, _ := item.(map[string]any)
		if node == nil {
			continue
		}
		if _, ok := excluded[asString(node["kind"])]; ok {
			continue
		}
		result = append(result, node)
	}
	return result
}

func flowchartLabelsByKind(payload map[string]any, kind string) []string {
	nodes, _ := payload["nodes"].([]any)
	labels := []string{}
	for _, item := range nodes {
		node, _ := item.(map[string]any)
		if node != nil && node["kind"] == kind {
			labels = append(labels, asString(node["label"]))
		}
	}
	return labels
}

func flowchartHasLabel(payload map[string]any, label string) bool {
	return flowchartLabelMatching(payload, func(candidate string) bool {
		return candidate == label
	})
}

func flowchartLabelMatching(payload map[string]any, predicate func(string) bool) bool {
	nodes, _ := payload["nodes"].([]any)
	for _, item := range nodes {
		node, _ := item.(map[string]any)
		if node != nil && predicate(asString(node["label"])) {
			return true
		}
	}
	return false
}

func sameStrings(actual []string, expected []string) bool {
	if len(actual) != len(expected) {
		return false
	}
	for index := range actual {
		if actual[index] != expected[index] {
			return false
		}
	}
	return true
}

func sameRange(value any, startLine int, startChar int, endLine int, endChar int) bool {
	rng, _ := value.(map[string]any)
	start, _ := rng["start"].(map[string]any)
	end, _ := rng["end"].(map[string]any)
	return intFromJSONNumber(start["line"]) == startLine &&
		intFromJSONNumber(start["character"]) == startChar &&
		intFromJSONNumber(end["line"]) == endLine &&
		intFromJSONNumber(end["character"]) == endChar
}

func intFromJSONNumber(value any) int {
	switch typed := value.(type) {
	case int:
		return typed
	case float64:
		return int(typed)
	default:
		return -1
	}
}

func utf16Len(value string) int {
	units := 0
	for _, r := range value {
		if r >= 0x10000 {
			units += 2
		} else {
			units++
		}
	}
	return units
}

func flowchartNodeHasAnyLinkLabel(node map[string]any, labels []string) bool {
	links, _ := node["links"].([]any)
	for _, item := range links {
		link, _ := item.(map[string]any)
		for _, label := range labels {
			if link["label"] == label {
				return true
			}
		}
	}
	return false
}

func flowchartNodeHasLinkLabel(node map[string]any, label string) bool {
	links, _ := node["links"].([]any)
	for _, item := range links {
		link, _ := item.(map[string]any)
		if link["label"] == label {
			return true
		}
	}
	return false
}

func flowchartNodeHasImplicitGlobalLink(node map[string]any, name string) bool {
	links, _ := node["links"].([]any)
	for _, item := range links {
		link, _ := item.(map[string]any)
		label := asString(link["label"])
		if link["symbolKind"] == "implicitGlobalVariable" &&
			(label == "implicit global variable "+name || label == name) {
			return true
		}
	}
	return false
}

func flowchartNodeHasTargetURI(node map[string]any, label string, uri string) bool {
	links, _ := node["links"].([]any)
	for _, item := range links {
		link, _ := item.(map[string]any)
		target, _ := link["target"].(map[string]any)
		if link["label"] == label && target["uri"] == uri {
			return true
		}
	}
	return false
}

func navigationNodeMatching(payload map[string]any, predicate func(map[string]any) bool) bool {
	nodes, _ := payload["nodes"].([]any)
	for _, item := range nodes {
		node, _ := item.(map[string]any)
		if node != nil && predicate(node) {
			return true
		}
	}
	return false
}

func navigationEdgeMatching(payload map[string]any, predicate func(map[string]any) bool) bool {
	edges, _ := payload["edges"].([]any)
	for _, item := range edges {
		edge, _ := item.(map[string]any)
		if edge != nil && predicate(edge) {
			return true
		}
	}
	return false
}

func navigationEdgeHasParameter(edge map[string]any, name string, source string) bool {
	parameters, _ := edge["parameters"].([]any)
	for _, item := range parameters {
		parameter, _ := item.(map[string]any)
		if parameter["name"] == name && parameter["source"] == source {
			return true
		}
	}
	return false
}

func navigationEdgeHasParameterValue(edge map[string]any, name string, source string, value string) bool {
	parameters, _ := edge["parameters"].([]any)
	for _, item := range parameters {
		parameter, _ := item.(map[string]any)
		if parameter["name"] == name && parameter["source"] == source && parameter["value"] == value {
			return true
		}
	}
	return false
}

func navigationNodeByID(payload map[string]any, id string) map[string]any {
	nodes, _ := payload["nodes"].([]any)
	for _, item := range nodes {
		node, _ := item.(map[string]any)
		if node != nil && node["id"] == id {
			return node
		}
	}
	return map[string]any{}
}

func asString(value any) string {
	text, _ := value.(string)
	return text
}

func writeFlowchartFixture(t *testing.T, path string, text string) {
	t.Helper()
	if err := os.WriteFile(path, []byte(text), 0o644); err != nil {
		t.Fatal(err)
	}
}
