package lspserver

import (
	"os"
	"path/filepath"
	"strings"
	"testing"

	"github.com/yottonoko/classic-web-system-lsp/internal/graph"
)

func TestStdioParityCountsIncludeReachableWorkspaceVariableUsagesInReferenceCodeLens(t *testing.T) {
	root := t.TempDir()
	first := filepath.Join(root, "1.asp")
	second := filepath.Join(root, "2.asp")
	shadow := filepath.Join(root, "3.asp")
	firstSource := `<%
Dim a
a=1
%>`
	writeWorkspaceReferenceFixture(t, first, firstSource)
	writeWorkspaceReferenceFixture(t, second, `<!-- #include file="1.asp" -->
<%
aim b
b = a
%>`)
	writeWorkspaceReferenceFixture(t, shadow, `<%
Dim a
a = 3
%>`)
	firstURI := pathToFileURI(first)

	client := startWorkspaceReferenceClient(t, map[string]any{
		"processId":    nil,
		"rootUri":      pathToFileURI(root),
		"capabilities": map[string]any{},
	})
	defer client.close()
	enableWorkspaceReferenceCodeLens(t, client)
	openClassicASPDocument(t, client, firstURI, firstSource)

	resolvedCodeLens := resolveReferenceCodeLens(t, client, firstURI, "a", "variable")
	resolvedText := mustJSONText(t, resolvedCodeLens)
	if !strings.Contains(resolvedText, "2 references") {
		t.Fatalf("workspace variable CodeLens count mismatch: %s", resolvedText)
	}

	workspaceGraph := buildWorkspaceReferenceGraph(t, client)
	aNode := graphNodeByLabelAndURI(workspaceGraph, "a", firstURI)
	if aNode == nil {
		t.Fatalf("workspace graph missing first a declaration: %s", mustJSONText(t, workspaceGraph))
	}
	if countGraphReferencesTo(workspaceGraph, aNode.ID) != 2 {
		t.Fatalf("workspace graph a reference count mismatch: %s", mustJSONText(t, workspaceGraph))
	}
}

func TestStdioParityUsesOpenWorkspaceReferenceCandidatesWhenFileURISpellingDiffers(t *testing.T) {
	root := t.TempDir()
	common := filepath.Join(root, "common.inc")
	page := filepath.Join(root, "page.asp")
	commonSource := `<%
Function SharedTitle()
End Function
%>`
	openPageSource := `<!-- #include file="common.inc" -->
<%
Response.Write SharedTitle()
%>`
	writeWorkspaceReferenceFixture(t, common, commonSource)
	writeWorkspaceReferenceFixture(t, page, "<%\n%>")
	commonURI := pathToFileURI(common)
	pageURI := pathToFileURI(page)
	encodedPageURI := strings.Replace(pageURI, "page.asp", "p%61ge.asp", 1)

	client := startWorkspaceReferenceClient(t, map[string]any{
		"processId":    nil,
		"rootUri":      pathToFileURI(root),
		"capabilities": map[string]any{},
	})
	defer client.close()
	enableWorkspaceReferenceCodeLens(t, client)
	openClassicASPDocument(t, client, commonURI, commonSource)
	openClassicASPDocument(t, client, encodedPageURI, openPageSource)

	resolvedCodeLens := resolveReferenceCodeLens(t, client, commonURI, "SharedTitle", "function")
	resolvedText := mustJSONText(t, resolvedCodeLens)
	if !strings.Contains(resolvedText, "1 reference") {
		t.Fatalf("encoded URI workspace CodeLens mismatch: %s", resolvedText)
	}
}

func TestStdioParityCountsRootlessSingleFileSiblingVariableUsagesInReferenceCodeLensAndReferences(t *testing.T) {
	root := t.TempDir()
	first := filepath.Join(root, "1.asp")
	second := filepath.Join(root, "2.asp")
	shadow := filepath.Join(root, "3.asp")
	firstSource := `<%
Dim a
a=1
%>`
	writeWorkspaceReferenceFixture(t, first, firstSource)
	writeWorkspaceReferenceFixture(t, second, `<!-- #include file="1.asp" -->
<%
aim b
b = a
%>`)
	writeWorkspaceReferenceFixture(t, shadow, `<%
Dim a
a = 3
%>`)
	firstURI := pathToFileURI(first)
	secondURI := pathToFileURI(second)
	shadowURI := pathToFileURI(shadow)

	client := startWorkspaceReferenceClient(t, map[string]any{
		"processId":        nil,
		"rootUri":          nil,
		"workspaceFolders": nil,
		"capabilities":     map[string]any{},
	})
	defer client.close()
	enableWorkspaceReferenceCodeLens(t, client)
	openClassicASPDocument(t, client, firstURI, firstSource)

	resolvedCodeLens := resolveReferenceCodeLens(t, client, firstURI, "a", "variable")
	resolvedText := mustJSONText(t, resolvedCodeLens)
	if !strings.Contains(resolvedText, "2 references") {
		t.Fatalf("rootless variable CodeLens count mismatch: %s", resolvedText)
	}

	references := client.request("textDocument/references", map[string]any{
		"textDocument": map[string]any{"uri": firstURI},
		"position":     map[string]any{"line": 1, "character": 4},
		"context":      map[string]any{"includeDeclaration": false},
	})
	referencesText := mustJSONText(t, references.Result)
	for _, expected := range []string{firstURI, secondURI} {
		if !strings.Contains(referencesText, expected) {
			t.Fatalf("rootless references missing %q: %s", expected, referencesText)
		}
	}
	if strings.Contains(referencesText, shadowURI) {
		t.Fatalf("rootless references included shadow declaration: %s", referencesText)
	}
}

func TestStdioParityUsesLegacyRootPathForUnopenedIncludeReachableVariableUsages(t *testing.T) {
	root := t.TempDir()
	includesDir := filepath.Join(root, "includes")
	pagesDir := filepath.Join(root, "pages")
	if err := os.Mkdir(includesDir, 0o755); err != nil {
		t.Fatal(err)
	}
	if err := os.Mkdir(pagesDir, 0o755); err != nil {
		t.Fatal(err)
	}
	first := filepath.Join(includesDir, "1.asp")
	second := filepath.Join(pagesDir, "2.asp")
	firstSource := `<%
Dim a
a=1
%>`
	writeWorkspaceReferenceFixture(t, first, firstSource)
	writeWorkspaceReferenceFixture(t, second, `<!-- #include file="../includes/1.asp" -->
<%
aim b
b = a
%>`)
	firstURI := pathToFileURI(first)

	client := startWorkspaceReferenceClient(t, map[string]any{
		"processId":        nil,
		"rootPath":         root,
		"rootUri":          nil,
		"workspaceFolders": nil,
		"capabilities":     map[string]any{},
	})
	defer client.close()
	enableWorkspaceReferenceCodeLens(t, client)
	openClassicASPDocument(t, client, firstURI, firstSource)

	resolvedCodeLens := resolveReferenceCodeLens(t, client, firstURI, "a", "variable")
	resolvedText := mustJSONText(t, resolvedCodeLens)
	if !strings.Contains(resolvedText, "2 references") {
		t.Fatalf("legacy rootPath variable CodeLens count mismatch: %s", resolvedText)
	}
}

func TestStdioParityCountsWorkspaceFolderReferenceCandidates(t *testing.T) {
	root := t.TempDir()
	firstRoot := filepath.Join(root, "root-a")
	secondRoot := filepath.Join(root, "root-b")
	if err := os.MkdirAll(firstRoot, 0o755); err != nil {
		t.Fatal(err)
	}
	if err := os.MkdirAll(secondRoot, 0o755); err != nil {
		t.Fatal(err)
	}
	common := filepath.Join(firstRoot, "common.inc")
	page := filepath.Join(secondRoot, "default.asp")
	unrelated := filepath.Join(secondRoot, "unrelated.asp")
	commonSource := `<%
Dim sharedValue
%>`
	pageSource := `<!-- #include file="../root-a/common.inc" -->
<%
Response.Write sharedValue
%>`
	writeWorkspaceReferenceFixture(t, common, commonSource)
	writeWorkspaceReferenceFixture(t, page, pageSource)
	writeWorkspaceReferenceFixture(t, unrelated, `<%
Response.Write sharedValue
%>`)
	commonURI := pathToFileURI(common)
	pageURI := pathToFileURI(page)
	unrelatedURI := pathToFileURI(unrelated)

	client := startWorkspaceReferenceClient(t, map[string]any{
		"processId": nil,
		"rootUri":   pathToFileURI(firstRoot),
		"workspaceFolders": []map[string]any{
			{"uri": pathToFileURI(firstRoot), "name": "root-a"},
			{"uri": pathToFileURI(secondRoot), "name": "root-b"},
		},
		"capabilities": map[string]any{},
	})
	defer client.close()
	enableWorkspaceReferenceCodeLens(t, client)
	openClassicASPDocument(t, client, commonURI, commonSource)

	resolvedCodeLens := resolveReferenceCodeLens(t, client, commonURI, "sharedValue", "variable")
	resolvedText := mustJSONText(t, resolvedCodeLens)
	if !strings.Contains(resolvedText, "1 reference") {
		t.Fatalf("multi-root workspace variable CodeLens count mismatch: %s", resolvedText)
	}

	references := client.request("textDocument/references", map[string]any{
		"textDocument": map[string]any{"uri": commonURI},
		"position":     map[string]any{"line": 1, "character": 4},
		"context":      map[string]any{"includeDeclaration": false},
	})
	referencesText := mustJSONText(t, references.Result)
	if !strings.Contains(referencesText, pageURI) || strings.Contains(referencesText, unrelatedURI) {
		t.Fatalf("multi-root workspace references mismatch: %s", referencesText)
	}
}

func TestStdioParitySharesWorkspaceReferencesBetweenCodeLensAndReferences(t *testing.T) {
	root := t.TempDir()
	common := filepath.Join(root, "common.inc")
	page := filepath.Join(root, "default.asp")
	unrelated := filepath.Join(root, "unrelated.asp")
	commonSource := `<%
Function SharedTitle()
  SharedTitle = "Dashboard"
End Function
%>`
	writeWorkspaceReferenceFixture(t, common, commonSource)
	writeWorkspaceReferenceFixture(t, page, `<!-- #include file="common.inc" -->
<%
Response.Write SharedTitle()
%>`)
	writeWorkspaceReferenceFixture(t, unrelated, "<%\nResponse.Write SharedTitle()\n%>")
	commonURI := pathToFileURI(common)
	pageURI := pathToFileURI(page)
	unrelatedURI := pathToFileURI(unrelated)

	client := startWorkspaceReferenceClient(t, map[string]any{
		"processId":    nil,
		"rootUri":      pathToFileURI(root),
		"capabilities": map[string]any{},
	})
	defer client.close()
	notifyConfiguration(t, client, map[string]any{"aspLsp": map[string]any{
		"debug": map[string]any{"output": "summary"},
	}})
	waitForWorkspaceIndexRefresh(t, client)
	openClassicASPDocument(t, client, commonURI, commonSource)

	resolvedCodeLens := resolveReferenceCodeLensAtLine(t, client, commonURI, 1)
	resolvedText := mustJSONText(t, resolvedCodeLens)
	if !strings.Contains(resolvedText, "1 reference") {
		t.Fatalf("workspace CodeLens shared reference mismatch: %s", resolvedText)
	}

	references := client.request("textDocument/references", map[string]any{
		"textDocument": map[string]any{"uri": commonURI},
		"position":     map[string]any{"line": 1, "character": 10},
		"context":      map[string]any{"includeDeclaration": false},
	})
	referencesText := mustJSONText(t, references.Result)
	if !strings.Contains(referencesText, pageURI) || strings.Contains(referencesText, unrelatedURI) {
		t.Fatalf("workspace references shared cache mismatch: %s", referencesText)
	}
	client.waitForLogContaining("referenceCache.hit")
}

func TestStdioParityStreamsLargeWorkspaceReferencePartialResults(t *testing.T) {
	root := t.TempDir()
	common := filepath.Join(root, "common.inc")
	page := filepath.Join(root, "default.asp")
	commonSource := "<%\nFunction SharedTitle()\n  SharedTitle = \"Dashboard\"\nEnd Function\n%>"
	var pageSource strings.Builder
	pageSource.WriteString("<!-- #include file=\"common.inc\" -->\n<%\n")
	for index := 0; index < 600; index++ {
		pageSource.WriteString("Response.Write SharedTitle()\n")
	}
	pageSource.WriteString("%>")
	writeWorkspaceReferenceFixture(t, common, commonSource)
	writeWorkspaceReferenceFixture(t, page, pageSource.String())
	commonURI := pathToFileURI(common)
	pageURI := pathToFileURI(page)

	client := startWorkspaceReferenceClient(t, map[string]any{
		"processId": nil, "rootUri": pathToFileURI(root), "capabilities": map[string]any{},
	})
	defer client.close()
	notifyConfiguration(t, client, map[string]any{"aspLsp": map[string]any{}})
	openClassicASPDocument(t, client, commonURI, commonSource)

	response := client.request("textDocument/references", map[string]any{
		"textDocument":       map[string]any{"uri": commonURI},
		"position":           map[string]any{"line": 1, "character": 10},
		"context":            map[string]any{"includeDeclaration": false},
		"partialResultToken": "partial-refs",
	})
	if result := mustJSONText(t, response.Result); result != "[]" {
		t.Fatalf("partial reference final result = %s, want []", result)
	}
	progress := client.waitForNotification("$/progress", "partial-refs")
	if !strings.Contains(string(progress.Params), pageURI) {
		t.Fatalf("partial references missing locations: %s", progress.Params)
	}
}

func TestStdioParityBatchResolvesWorkspaceReferenceCodeLensAndSkipsUnreachableCandidates(t *testing.T) {
	root := t.TempDir()
	common := filepath.Join(root, "common.inc")
	page := filepath.Join(root, "default.asp")
	unrelated := filepath.Join(root, "unrelated.asp")
	commonSource := `<%
Function SharedTitle()
  SharedTitle = "Dashboard"
End Function

Function SharedSubtitle()
  SharedSubtitle = "Overview"
End Function
%>`
	writeWorkspaceReferenceFixture(t, common, commonSource)
	writeWorkspaceReferenceFixture(t, page, `<!-- #include file="common.inc" -->
<%
Response.Write SharedTitle()
Response.Write SharedSubtitle()
%>`)
	writeWorkspaceReferenceFixture(t, unrelated, "<%\nResponse.Write SharedTitle()\n%>")
	commonURI := pathToFileURI(common)

	client := startWorkspaceReferenceClient(t, map[string]any{
		"processId":    nil,
		"rootUri":      pathToFileURI(root),
		"capabilities": map[string]any{},
	})
	defer client.close()
	notifyConfiguration(t, client, map[string]any{"aspLsp": map[string]any{
		"debug": map[string]any{"output": "summary"},
	}})
	waitForWorkspaceIndexRefresh(t, client)
	openClassicASPDocument(t, client, commonURI, commonSource)

	resolvedTitleLens := resolveReferenceCodeLensAtLine(t, client, commonURI, 1)
	resolvedTitleText := mustJSONText(t, resolvedTitleLens)
	if !strings.Contains(resolvedTitleText, "1 reference") {
		t.Fatalf("workspace batch title CodeLens mismatch: %s", resolvedTitleText)
	}
	batchLog, _ := client.waitForNotificationWithSeen("window/logMessage", "vb.references.batch.complete")
	if !strings.Contains(mustJSONText(t, batchLog), "symbols=2") {
		t.Fatalf("batch complete log mismatch: %s", mustJSONText(t, batchLog))
	}

	resolvedSubtitleLens := resolveReferenceCodeLensAtLine(t, client, commonURI, 5)
	resolvedSubtitleText := mustJSONText(t, resolvedSubtitleLens)
	if !strings.Contains(resolvedSubtitleText, "1 reference") {
		t.Fatalf("workspace batch subtitle CodeLens mismatch: %s", resolvedSubtitleText)
	}
	client.waitForLogContaining("vb.references.batch.cache.hit")
}

func TestStdioParityStartsWorkspaceReferenceBatchWhenCodeLensIsCreated(t *testing.T) {
	root := t.TempDir()
	common := filepath.Join(root, "common.inc")
	page := filepath.Join(root, "default.asp")
	commonSource := `<%
Function SharedTitle()
  SharedTitle = "Dashboard"
End Function

Function SharedSubtitle()
  SharedSubtitle = "Overview"
End Function
%>`
	writeWorkspaceReferenceFixture(t, common, commonSource)
	writeWorkspaceReferenceFixture(t, page, `<!-- #include file="common.inc" -->
<%
Response.Write SharedTitle()
Response.Write SharedSubtitle()
%>`)
	commonURI := pathToFileURI(common)

	client := startWorkspaceReferenceClient(t, map[string]any{
		"processId":    nil,
		"rootUri":      pathToFileURI(root),
		"capabilities": map[string]any{},
	})
	defer client.close()
	notifyConfiguration(t, client, map[string]any{"aspLsp": map[string]any{
		"debug": map[string]any{"output": "summary"},
	}})
	openClassicASPDocument(t, client, commonURI, commonSource)

	firstCodeLens := client.request("textDocument/codeLens", map[string]any{
		"textDocument": map[string]any{"uri": commonURI},
	})
	if first := referenceCodeLensWithDataAndLine(t, firstCodeLens.Result, "SharedTitle", 1); !strings.Contains(mustJSONText(t, first), "Calculating references") {
		t.Fatalf("cold CodeLens progress title mismatch: %#v", first)
	}
	if batchLog := client.waitForLogContaining("vb.references.batch.complete"); !strings.Contains(mustJSONText(t, batchLog), "symbols=2") {
		t.Fatalf("CodeLens creation batch log mismatch: %s", mustJSONText(t, batchLog))
	}

	warmCodeLens := client.request("textDocument/codeLens", map[string]any{
		"textDocument": map[string]any{"uri": commonURI},
	})
	warm := referenceCodeLensWithDataAndLine(t, warmCodeLens.Result, "SharedTitle", 1)
	warmText := mustJSONText(t, warm)
	if warm["command"] == nil || !strings.Contains(warmText, "1 reference") {
		t.Fatalf("warm CodeLens was not returned resolved from the shared batch: %s", warmText)
	}
}

func TestStdioParityReusesIndexedParsedDocumentsInWorkerBackedWorkspaceReferences(t *testing.T) {
	root := t.TempDir()
	cacheDir := filepath.Join(root, ".cache")
	common := filepath.Join(root, "common.inc")
	page := filepath.Join(root, "default.asp")
	commonSource := `<%
Function SharedTitle()
  SharedTitle = "Dashboard"
End Function
%>`
	writeWorkspaceReferenceFixture(t, common, commonSource)
	writeWorkspaceReferenceFixture(t, page, `<!-- #include file="common.inc" -->
<%
Function SharedTitle()
  SharedTitle = "Local"
End Function
Response.Write SharedTitle()
%>`)
	commonURI := pathToFileURI(common)
	settings := map[string]any{"aspLsp": map[string]any{
		"debug": map[string]any{"output": "summary"},
		"cache": map[string]any{"enabled": true, "directory": cacheDir},
	}}

	firstClient := startWorkspaceReferenceClient(t, map[string]any{
		"processId":    nil,
		"rootUri":      pathToFileURI(root),
		"capabilities": map[string]any{},
	})
	notifyConfiguration(t, firstClient, settings)
	firstClient.waitForLogContaining("method=workspace/didChangeConfiguration status=ok")
	executeCommand(t, firstClient, "aspLsp.server.reindexWorkspace", nil)
	waitForWorkspaceIndexRefresh(t, firstClient)
	symbols := firstClient.request("workspace/symbol", map[string]any{"query": "SharedTitle"})
	if !strings.Contains(mustJSONText(t, symbols.Result), "SharedTitle") {
		firstClient.close()
		t.Fatalf("workspace symbols missing SharedTitle: %s", mustJSONText(t, symbols.Result))
	}
	firstClient.close()

	secondClient := startWorkspaceReferenceClient(t, map[string]any{
		"processId":    nil,
		"rootUri":      pathToFileURI(root),
		"capabilities": map[string]any{},
	})
	defer secondClient.close()
	notifyConfiguration(t, secondClient, settings)
	secondClient.waitForLogContaining("method=workspace/didChangeConfiguration status=ok")
	executeCommand(t, secondClient, "aspLsp.server.reindexWorkspace", nil)
	waitForWorkspaceIndexRefresh(t, secondClient)
	secondClient.waitForLogContaining("database.workspaceIndex.hit")
	openClassicASPDocument(t, secondClient, commonURI, commonSource)

	resolvedCodeLens := resolveReferenceCodeLensAtLine(t, secondClient, commonURI, 1)
	if !strings.Contains(mustJSONText(t, resolvedCodeLens), "references") {
		t.Fatalf("worker cache CodeLens command mismatch: %s", mustJSONText(t, resolvedCodeLens))
	}
	workerLog := secondClient.waitForLogContaining("vb.references.batch.complete")
	workerLogText := mustJSONText(t, workerLog)
	if !strings.Contains(workerLogText, "symbols=1") {
		t.Fatalf("worker reference batch log mismatch: %s", mustJSONText(t, workerLog))
	}
}

func TestStdioParityExcludesXMLDocumentationCommentsFromCodeLensButReturnsCrefReferences(t *testing.T) {
	root := t.TempDir()
	common := filepath.Join(root, "common.inc")
	page := filepath.Join(root, "default.asp")
	commonSource := `<%
Function SharedTitle()
End Function
%>`
	pageDocument := markedDocument(`<!-- #include file="common.inc" -->
<%
''' <see cref="Shared<<<caret>>>Title" />
Sub Caller()
End Sub
%>`)
	writeWorkspaceReferenceFixture(t, common, commonSource)
	writeWorkspaceReferenceFixture(t, page, pageDocument.Text)
	commonURI := pathToFileURI(common)
	pageURI := pathToFileURI(page)

	client := startWorkspaceReferenceClient(t, map[string]any{
		"processId":    nil,
		"rootUri":      pathToFileURI(root),
		"capabilities": map[string]any{},
	})
	defer client.close()
	notifyConfiguration(t, client, map[string]any{"aspLsp": map[string]any{
		"debug": map[string]any{"output": "summary"},
	}})
	openClassicASPDocument(t, client, pageURI, pageDocument.Text)

	definition := client.request("textDocument/definition", map[string]any{
		"textDocument": map[string]any{"uri": pageURI},
		"position":     pageDocument.Position,
	})
	if !strings.Contains(mustJSONText(t, definition.Result), commonURI) {
		t.Fatalf("XML documentation cref definition mismatch: %s", mustJSONText(t, definition.Result))
	}

	openClassicASPDocument(t, client, commonURI, commonSource)
	resolvedCodeLens := resolveReferenceCodeLensAtLine(t, client, commonURI, 1)
	resolvedText := mustJSONText(t, resolvedCodeLens)
	if !strings.Contains(resolvedText, "0 references") || strings.Contains(resolvedText, pageURI) {
		t.Fatalf("XML documentation cref CodeLens counted as reference: %s", resolvedText)
	}

	references := client.request("textDocument/references", map[string]any{
		"textDocument": map[string]any{"uri": commonURI},
		"position":     map[string]any{"line": 1, "character": 10},
		"context":      map[string]any{"includeDeclaration": false},
	})
	referencesText := mustJSONText(t, references.Result)
	if !strings.Contains(referencesText, pageURI) {
		t.Fatalf("XML documentation cref missing from textDocument/references: %s", referencesText)
	}
	client.waitForLogContaining("referenceCache.hit")
}

func TestStdioParityCountsWorkspaceReferencesFromEveryVBScriptPropertyAccessorDeclaration(t *testing.T) {
	root := t.TempDir()
	common := filepath.Join(root, "common.inc")
	readPage := filepath.Join(root, "read.asp")
	writePage := filepath.Join(root, "write.asp")
	commonSource := `<%
Class Customer
  Public Property Get Name()
  End Property
  Public Property Let Name(value)
  End Property
End Class
%>`
	readSource := `<!-- #include file="common.inc" -->
<%
Dim c
Set c = New Customer
Response.Write c.Name
%>`
	writeSource := `<!-- #include file="common.inc" -->
<%
Dim c
Set c = New Customer
c.Name = "Alice"
%>`
	writeWorkspaceReferenceFixture(t, common, commonSource)
	writeWorkspaceReferenceFixture(t, readPage, readSource)
	writeWorkspaceReferenceFixture(t, writePage, writeSource)
	commonURI := pathToFileURI(common)
	readURI := pathToFileURI(readPage)
	writeURI := pathToFileURI(writePage)

	client := startWorkspaceReferenceClient(t, map[string]any{
		"processId":    nil,
		"rootUri":      pathToFileURI(root),
		"capabilities": map[string]any{},
	})
	defer client.close()
	enableWorkspaceReferenceCodeLens(t, client)
	for _, document := range []struct {
		uri  string
		text string
	}{
		{readURI, readSource},
		{writeURI, writeSource},
		{commonURI, commonSource},
	} {
		openClassicASPDocument(t, client, document.uri, document.text)
	}

	resolvedCodeLens := resolveReferenceCodeLensAtLine(t, client, commonURI, 4)
	resolvedText := mustJSONText(t, resolvedCodeLens)
	if !strings.Contains(resolvedText, "2 references") {
		t.Fatalf("property accessor CodeLens count mismatch: %s", resolvedText)
	}

	references := client.request("textDocument/references", map[string]any{
		"textDocument": map[string]any{"uri": commonURI},
		"position":     map[string]any{"line": 4, "character": 22},
		"context":      map[string]any{"includeDeclaration": false},
	})
	referencesText := mustJSONText(t, references.Result)
	for _, expected := range []string{readURI, writeURI} {
		if !strings.Contains(referencesText, expected) {
			t.Fatalf("property accessor references missing %q: %s", expected, referencesText)
		}
	}
}

func TestStdioParityCountsIncludedObjectVariableMemberAndDefaultMemberUsagesAsWorkspaceReferences(t *testing.T) {
	root := t.TempDir()
	common := filepath.Join(root, "common.inc")
	page := filepath.Join(root, "default.asp")
	commonSource := `<%
Dim db
Set db = Server.CreateObject("ADODB.Connection")
%>`
	pageSource := `<!-- #include file="common.inc" -->
<%
Function Render()
  Dim localDb
  db.Open()
  localDb.Open()
  Response.Write db("value")
End Function
%>`
	writeWorkspaceReferenceFixture(t, common, commonSource)
	writeWorkspaceReferenceFixture(t, page, pageSource)
	commonURI := pathToFileURI(common)
	pageURI := pathToFileURI(page)
	memberPosition := positionAt(pageSource, strings.Index(pageSource, "db.Open")+1)
	defaultMemberPosition := positionAt(pageSource, strings.Index(pageSource, `db("value")`)+1)

	client := startWorkspaceReferenceClient(t, map[string]any{
		"processId":    nil,
		"rootUri":      pathToFileURI(root),
		"capabilities": map[string]any{},
	})
	defer client.close()
	enableWorkspaceReferenceCodeLens(t, client)
	openClassicASPDocument(t, client, pageURI, pageSource)
	for _, position := range []map[string]int{memberPosition, defaultMemberPosition} {
		definition := client.request("textDocument/definition", map[string]any{
			"textDocument": map[string]any{"uri": pageURI},
			"position":     position,
		})
		if !strings.Contains(mustJSONText(t, definition.Result), commonURI) {
			t.Fatalf("object variable definition mismatch: %s", mustJSONText(t, definition.Result))
		}
	}

	openClassicASPDocument(t, client, commonURI, commonSource)
	resolvedCodeLens := resolveReferenceCodeLensAtLine(t, client, commonURI, 1)
	resolvedText := mustJSONText(t, resolvedCodeLens)
	if !strings.Contains(resolvedText, "2 references") {
		t.Fatalf("object variable CodeLens mismatch: %s", resolvedText)
	}

	references := client.request("textDocument/references", map[string]any{
		"textDocument": map[string]any{"uri": commonURI},
		"position":     map[string]any{"line": 1, "character": 4},
		"context":      map[string]any{"includeDeclaration": false},
	})
	referencesText := mustJSONText(t, references.Result)
	if countOccurrences(referencesText, pageURI) != 2 ||
		!strings.Contains(referencesText, `"line":4`) ||
		!strings.Contains(referencesText, `"line":6`) {
		t.Fatalf("object variable references mismatch: %s", referencesText)
	}
}

func TestStdioParityCountsWorkspaceReferenceCodeLensForAllSupportedSymbolCategoriesThroughMixedCaseIncludes(t *testing.T) {
	root := t.TempDir()
	sharedDir := filepath.Join(root, "Shared")
	if err := os.Mkdir(sharedDir, 0o755); err != nil {
		t.Fatal(err)
	}
	common := filepath.Join(sharedDir, "MixedCase.INC")
	page := filepath.Join(root, "default.asp")
	commonSource := `<%
Const SharedConst = 1
Dim SharedValue
Function SharedTitle()
End Function
Sub SharedRun()
End Sub
Class SharedCustomer
  Public Name
  Public Const Kind = "standard"
  Public Sub Save()
  End Sub
  Public Property Get DisplayName()
  End Property
End Class
%>`
	pageSource := `<!-- #include file="sHaReD/mIxEdCaSe.InC" -->
<%
Dim customer
SharedValue = SharedConst
Response.Write SharedTitle()
SharedRun
Set customer = New SharedCustomer
customer.Name = customer.Kind
customer.Save
Response.Write customer.DisplayName
%>`
	writeWorkspaceReferenceFixture(t, common, commonSource)
	writeWorkspaceReferenceFixture(t, page, pageSource)
	commonURI := pathToFileURI(common)

	client := startWorkspaceReferenceClient(t, map[string]any{
		"processId":    nil,
		"rootUri":      pathToFileURI(root),
		"capabilities": map[string]any{},
	})
	defer client.close()
	enableWorkspaceReferenceCodeLens(t, client)
	openClassicASPDocument(t, client, commonURI, commonSource)

	for _, target := range []struct {
		name       string
		symbolKind string
		memberOf   string
	}{
		{"SharedConst", "constant", ""},
		{"SharedValue", "variable", ""},
		{"SharedTitle", "function", ""},
		{"SharedRun", "sub", ""},
		{"SharedCustomer", "class", ""},
		{"Name", "field", "SharedCustomer"},
		{"Kind", "constant", "SharedCustomer"},
		{"Save", "method", "SharedCustomer"},
		{"DisplayName", "property", "SharedCustomer"},
	} {
		resolved := resolveReferenceCodeLensByData(t, client, commonURI, target.name, target.symbolKind, target.memberOf)
		resolvedText := mustJSONText(t, resolved)
		if !strings.Contains(resolvedText, "1 reference") {
			t.Fatalf("%s:%s:%s CodeLens mismatch: %s", target.symbolKind, target.memberOf, target.name, resolvedText)
		}
	}
}

func TestStdioParityResolvesStaleReferenceCodeLensDataWhenUniqueSymbolMoves(t *testing.T) {
	client := startStdioTestClient(t)
	defer client.close()

	root := t.TempDir()
	uri := pathToFileURI(filepath.Join(root, "go-vb-stale-codelens.asp"))
	source := `<%
Function SharedTitle()
End Function
Response.Write SharedTitle()
%>`
	updated := `<%
' moved
Function SharedTitle()
End Function
Response.Write SharedTitle()
%>`
	client.request("initialize", map[string]any{
		"processId":    nil,
		"rootUri":      pathToFileURI(root),
		"capabilities": map[string]any{},
	})
	notifyConfiguration(t, client, map[string]any{"aspLsp": map[string]any{
		"diagnostics": map[string]any{"debounceMs": 0},
	}})
	openClassicASPDocument(t, client, uri, source)

	codeLens := client.request("textDocument/codeLens", map[string]any{
		"textDocument": map[string]any{"uri": uri},
	})
	referencesCodeLens := referenceCodeLensWithDataAndLine(t, codeLens.Result, "SharedTitle", 1)
	if err := client.notify("textDocument/didChange", map[string]any{
		"textDocument": map[string]any{"uri": uri, "version": 2},
		"contentChanges": []map[string]any{{
			"text": updated,
		}},
	}); err != nil {
		t.Fatal(err)
	}
	client.waitForNotification("textDocument/publishDiagnostics", uri)

	resolvedCodeLens := client.request("codeLens/resolve", referencesCodeLens)
	resolvedText := mustJSONText(t, resolvedCodeLens.Result)
	for _, expected := range []string{"aspLsp.showReferences", "1 reference", `"line":2`, `"character":9`} {
		if !strings.Contains(resolvedText, expected) {
			t.Fatalf("stale CodeLens resolve missing %q: %s", expected, resolvedText)
		}
	}
}

func TestStdioParityFallsBackFromSummaryWorkspaceReferencesWhenCandidateHasSameNameDeclaration(t *testing.T) {
	root := t.TempDir()
	common := filepath.Join(root, "common.inc")
	page := filepath.Join(root, "default.asp")
	commonSource := `<%
Function SharedTitle()
  SharedTitle = "Dashboard"
End Function
%>`
	writeWorkspaceReferenceFixture(t, common, commonSource)
	writeWorkspaceReferenceFixture(t, page, `<!-- #include file="common.inc" -->
<%
Function SharedTitle()
  SharedTitle = "Local"
End Function
Response.Write SharedTitle()
%>`)
	commonURI := pathToFileURI(common)
	pageURI := pathToFileURI(page)

	client := startWorkspaceReferenceClient(t, map[string]any{
		"processId":    nil,
		"rootUri":      pathToFileURI(root),
		"capabilities": map[string]any{},
	})
	defer client.close()
	notifyConfiguration(t, client, map[string]any{"aspLsp": map[string]any{
		"debug": map[string]any{"output": "summary"},
	}})
	openClassicASPDocument(t, client, commonURI, commonSource)

	resolvedCodeLens := resolveReferenceCodeLensAtLine(t, client, commonURI, 1)
	resolvedText := mustJSONText(t, resolvedCodeLens)
	if strings.Contains(resolvedText, pageURI) {
		t.Fatalf("same-name declaration candidate leaked into CodeLens: %s", resolvedText)
	}
	client.waitForLogContaining("vb.references.batch.shadowed")
}

func TestStdioParityCountsRelatedIncludeFamilyUsagesInReferenceCodeLensWhenUnresolvedAnalysisNeedsThem(t *testing.T) {
	root := t.TempDir()
	page := filepath.Join(root, "default.asp")
	parent := filepath.Join(root, "parent.asp")
	sibling := filepath.Join(root, "sibling.inc")
	pageSource := `<%
Dim SharedValue
MissingGlobal = 1
%>`
	writeWorkspaceReferenceFixture(t, page, pageSource)
	writeWorkspaceReferenceFixture(t, parent, `<!-- #include file="default.asp" -->
<!-- #include file="sibling.inc" -->`)
	writeWorkspaceReferenceFixture(t, sibling, `<%
SharedValue = 2
%>`)
	pageURI := pathToFileURI(page)

	client := startWorkspaceReferenceClient(t, map[string]any{
		"processId":    nil,
		"rootUri":      pathToFileURI(root),
		"capabilities": map[string]any{},
	})
	defer client.close()
	openClassicASPDocument(t, client, pageURI, pageSource)

	codeLens := client.request("textDocument/codeLens", map[string]any{
		"textDocument": map[string]any{"uri": pageURI},
	})
	referencesCodeLens := referenceCodeLensWithData(t, codeLens.Result, "SharedValue", "variable")
	resolvedCodeLens := client.request("codeLens/resolve", referencesCodeLens)
	resolvedText := mustJSONText(t, resolvedCodeLens.Result)
	if !strings.Contains(resolvedText, "1 reference") || strings.Contains(resolvedText, "(analyzed only)") {
		t.Fatalf("default related include CodeLens mismatch: %s", resolvedText)
	}

	notifyConfiguration(t, client, map[string]any{"aspLsp": map[string]any{
		"codeLens": map[string]any{
			"includeRelatedIncludeTreesForUnresolved": false,
		},
		"diagnostics": map[string]any{"debounceMs": 0},
	}})
	waitForWorkspaceIndexRefresh(t, client)
	disabledResolved := client.request("codeLens/resolve", referencesCodeLens)
	disabledText := mustJSONText(t, disabledResolved.Result)
	if !strings.Contains(disabledText, "0 references") ||
		strings.Contains(disabledText, "(analyzed only)") {
		t.Fatalf("disabled related include CodeLens mismatch: %s", disabledText)
	}
}

func startWorkspaceReferenceClient(t *testing.T, initializeParams map[string]any) *stdioTestClient {
	t.Helper()
	client := startStdioTestClient(t)
	initializeAndWaitForWorkspaceIndex(t, client, initializeParams)
	return client
}

func enableWorkspaceReferenceCodeLens(t *testing.T, client *stdioTestClient) {
	t.Helper()
	notifyConfiguration(t, client, map[string]any{"aspLsp": map[string]any{
		"diagnostics": map[string]any{"debounceMs": 0},
	}})
	waitForWorkspaceIndexRefresh(t, client)
}

func resolveReferenceCodeLens(t *testing.T, client *stdioTestClient, uri string, name string, symbolKind string) any {
	t.Helper()
	codeLens := client.request("textDocument/codeLens", map[string]any{
		"textDocument": map[string]any{"uri": uri},
	})
	referencesCodeLens := referenceCodeLensWithData(t, codeLens.Result, name, symbolKind)
	return client.request("codeLens/resolve", referencesCodeLens).Result
}

func resolveReferenceCodeLensByData(t *testing.T, client *stdioTestClient, uri string, name string, symbolKind string, memberOf string) any {
	t.Helper()
	codeLens := client.request("textDocument/codeLens", map[string]any{
		"textDocument": map[string]any{"uri": uri},
	})
	referencesCodeLens := referenceCodeLensWithDataAndMember(t, codeLens.Result, name, symbolKind, memberOf)
	return client.request("codeLens/resolve", referencesCodeLens).Result
}

func resolveReferenceCodeLensAtLine(t *testing.T, client *stdioTestClient, uri string, line float64) any {
	t.Helper()
	codeLens := client.request("textDocument/codeLens", map[string]any{
		"textDocument": map[string]any{"uri": uri},
	})
	referencesCodeLens := referenceCodeLensWithLine(t, codeLens.Result, line)
	return client.request("codeLens/resolve", referencesCodeLens).Result
}

func referenceCodeLensWithData(t *testing.T, value any, name string, symbolKind string) map[string]any {
	t.Helper()
	var lenses []map[string]any
	mustDecodeResult(t, value, &lenses)
	for _, lens := range lenses {
		data, ok := lens["data"].(map[string]any)
		if !ok {
			continue
		}
		if data["kind"] == "vbscript-reference" && data["name"] == name && data["symbolKind"] == symbolKind {
			return lens
		}
	}
	t.Fatalf("reference CodeLens data name=%s symbolKind=%s missing: %s", name, symbolKind, mustJSONText(t, value))
	return nil
}

func referenceCodeLensWithDataAndMember(t *testing.T, value any, name string, symbolKind string, memberOf string) map[string]any {
	t.Helper()
	var lenses []map[string]any
	mustDecodeResult(t, value, &lenses)
	for _, lens := range lenses {
		data, ok := lens["data"].(map[string]any)
		if !ok {
			continue
		}
		dataMemberOf, _ := data["memberOf"].(string)
		if data["kind"] == "vbscript-reference" &&
			data["name"] == name &&
			data["symbolKind"] == symbolKind &&
			dataMemberOf == memberOf {
			return lens
		}
	}
	t.Fatalf("reference CodeLens data name=%s symbolKind=%s memberOf=%s missing: %s", name, symbolKind, memberOf, mustJSONText(t, value))
	return nil
}

func referenceCodeLensWithDataAndLine(t *testing.T, value any, name string, line float64) map[string]any {
	t.Helper()
	var lenses []map[string]any
	mustDecodeResult(t, value, &lenses)
	for _, lens := range lenses {
		data, ok := lens["data"].(map[string]any)
		if !ok {
			continue
		}
		if data["kind"] == "vbscript-reference" && data["name"] == name && data["line"] == line {
			return lens
		}
	}
	t.Fatalf("reference CodeLens data name=%s line=%v missing: %s", name, line, mustJSONText(t, value))
	return nil
}

func referenceCodeLensWithLine(t *testing.T, value any, line float64) map[string]any {
	t.Helper()
	var lenses []map[string]any
	mustDecodeResult(t, value, &lenses)
	for _, lens := range lenses {
		data, ok := lens["data"].(map[string]any)
		if !ok {
			continue
		}
		if data["kind"] == "vbscript-reference" && data["line"] == line {
			return lens
		}
	}
	t.Fatalf("reference CodeLens data line=%v missing: %s", line, mustJSONText(t, value))
	return nil
}

func buildWorkspaceReferenceGraph(t *testing.T, client *stdioTestClient) graph.Payload {
	t.Helper()
	response := client.request("workspace/executeCommand", map[string]any{
		"command":   "aspLsp.server.buildGraph",
		"arguments": []map[string]any{{"scope": "workspace"}},
	})
	var payload graph.Payload
	mustDecodeResult(t, response.Result, &payload)
	return payload
}

func countGraphReferencesTo(payload graph.Payload, targetID string) int {
	count := 0
	for _, edge := range graphEdges(payload) {
		if edge.Target == targetID && (edge.Kind == "references" || edge.Kind == "assignments") {
			count += edge.Count
		}
	}
	return count
}

func writeWorkspaceReferenceFixture(t *testing.T, path string, text string) {
	t.Helper()
	if err := os.WriteFile(path, []byte(text), 0o644); err != nil {
		t.Fatal(err)
	}
}
