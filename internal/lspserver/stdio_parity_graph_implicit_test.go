package lspserver

import (
	"os"
	"path/filepath"
	"testing"
)

func TestStdioParityCanonicalizesImplicitGlobalsAcrossSiblingDiamondAndCyclicIncludes(t *testing.T) {
	client := startStdioTestClient(t)
	defer client.close()

	root := t.TempDir()
	write := func(relativePath string, source string) (string, string) {
		t.Helper()
		fileName := filepath.Join(root, relativePath)
		if err := os.MkdirAll(filepath.Dir(fileName), 0o755); err != nil {
			t.Fatal(err)
		}
		writeGraphFixture(t, fileName, source)
		return fileName, pathToFileURI(fileName)
	}
	_, siblingRootURI := write("sibling/root.asp", `<!-- #include file="a.inc" -->
<!-- #include file="b.inc" -->
<%
Response.Write siblingTitle
%>`)
	_, siblingAURI := write("sibling/a.inc", `<%
siblingTitle = "a"
%>`)
	_, siblingBURI := write("sibling/b.inc", `<%
siblingTitle = "b"
%>`)
	_, diamondRootURI := write("diamond/root.asp", `<!-- #include file="left.inc" -->
<!-- #include file="right.inc" -->`)
	_, diamondLeftURI := write("diamond/left.inc", `<!-- #include file="shared.inc" -->
<%
Response.Write diamondTitle
%>`)
	_, diamondRightURI := write("diamond/right.inc", `<!-- #include file="shared.inc" -->
<%
diamondTitle = "right"
%>`)
	_, diamondSharedURI := write("diamond/shared.inc", `<%
diamondTitle = "shared"
%>`)
	_, cycleAURI := write("cycle/a.asp", `<!-- #include file="b.inc" -->
<%
cycleTitle = "a"
%>`)
	_, cycleBURI := write("cycle/b.inc", `<!-- #include file="c.inc" -->
<%
Response.Write cycleTitle
%>`)
	_, cycleCURI := write("cycle/c.inc", `<!-- #include file="a.asp" -->
<%
cycleTitle = "c"
%>`)

	client.request("initialize", map[string]any{
		"processId":    nil,
		"rootUri":      pathToFileURI(root),
		"capabilities": map[string]any{},
	})
	siblingGraph := buildDocumentGraph(t, client, siblingRootURI, nil)
	siblingRootNode := graphNodeByLabelAndURI(siblingGraph, "root.asp", siblingRootURI)
	siblingATitle := graphNodeByLabelAndURI(siblingGraph, "siblingTitle", siblingAURI)
	siblingBTitle := graphNodeByLabelAndURI(siblingGraph, "siblingTitle", siblingBURI)
	if !graphHasLinkBetween(siblingGraph, "references", siblingRootNode, siblingATitle, "") ||
		graphHasLinkBetween(siblingGraph, "references", siblingRootNode, siblingBTitle, "") {
		t.Fatalf("sibling implicit global canonicalization mismatch: %s", mustJSONText(t, siblingGraph))
	}

	diamondGraph := buildDocumentGraph(t, client, diamondRootURI, nil)
	diamondSharedTitle := graphNodeByLabelAndURI(diamondGraph, "diamondTitle", diamondSharedURI)
	diamondLeftNode := graphNodeByLabelAndURI(diamondGraph, "left.inc", diamondLeftURI)
	diamondRightNode := graphNodeByLabelAndURI(diamondGraph, "right.inc", diamondRightURI)
	if diamondSharedTitle == nil || !diamondSharedTitle.Implicit {
		t.Fatalf("diamond implicit global node mismatch: %#v in %s", diamondSharedTitle, mustJSONText(t, diamondGraph))
	}
	if !graphHasLinkBetween(diamondGraph, "references", diamondLeftNode, diamondSharedTitle, "") ||
		!graphHasLinkBetween(diamondGraph, "assignments", diamondRightNode, diamondSharedTitle, "") {
		t.Fatalf("diamond implicit global canonicalization mismatch: %s", mustJSONText(t, diamondGraph))
	}

	cycleGraph := buildDocumentGraph(t, client, cycleAURI, nil)
	if len(cycleGraph.Nodes) >= 50 {
		t.Fatalf("cycle graph node count = %d, want < 50: %s", len(cycleGraph.Nodes), mustJSONText(t, cycleGraph))
	}
	hasCycleTitle := false
	for _, uri := range []string{cycleAURI, cycleBURI, cycleCURI} {
		hasCycleTitle = hasCycleTitle || graphNodeByLabelAndURI(cycleGraph, "cycleTitle", uri) != nil
	}
	if !hasCycleTitle {
		t.Fatalf("cycle graph missing cycleTitle declaration: %s", mustJSONText(t, cycleGraph))
	}
}

func TestStdioParityBuildsDocumentGraphsWithWorkerSymbolExtractionEnabled(t *testing.T) {
	client := startStdioTestClient(t)
	defer client.close()

	root := t.TempDir()
	common := filepath.Join(root, "common.inc")
	page := filepath.Join(root, "default.asp")
	writeGraphFixture(t, common, `<%
Dim sharedValue
sharedValue = "ok"
%>`)
	writeGraphFixture(t, page, `<!-- #include file="common.inc" -->
<%
Response.Write sharedValue
%>`)
	commonURI := pathToFileURI(common)
	pageURI := pathToFileURI(page)
	initializeAndWaitForWorkspaceIndex(t, client, map[string]any{
		"processId":    nil,
		"rootUri":      pathToFileURI(root),
		"capabilities": map[string]any{},
	})
	notifyConfiguration(t, client, map[string]any{"aspLsp": map[string]any{
		"graph": map[string]any{"workerSymbolExtraction": true},
	}})
	waitForWorkspaceIndexRefresh(t, client)
	payload := buildDocumentGraph(t, client, pageURI, nil)
	pageNode := graphNodeByLabelAndURI(payload, "default.asp", pageURI)
	sharedNode := graphNodeByLabelAndURI(payload, "sharedValue", commonURI)
	if sharedNode == nil || sharedNode.Label != "sharedValue" {
		t.Fatalf("worker graph missing sharedValue node: %s", mustJSONText(t, payload))
	}
	if !graphHasLinkBetween(payload, "references", pageNode, sharedNode, "") {
		t.Fatalf("worker graph missing page -> sharedValue reference: %s", mustJSONText(t, payload))
	}
}
