package lspserver

import (
	"path/filepath"
	"strings"
	"testing"

	"github.com/yottonoko/classic-web-system-lsp/internal/graph"
)

func TestReferenceCodeLensTitle(t *testing.T) {
	tests := []struct {
		name     string
		count    int
		japanese bool
		want     string
	}{
		{name: "zero", count: 0, want: "0 references"},
		{name: "one", count: 1, want: "1 reference"},
		{name: "many", count: 2, want: "2 references"},
		{name: "Japanese", count: 2, japanese: true, want: "2 件の参照"},
	}
	for _, test := range tests {
		t.Run(test.name, func(t *testing.T) {
			if got := referenceCodeLensTitle(test.count, test.japanese); got != test.want {
				t.Fatalf("referenceCodeLensTitle() = %q, want %q", got, test.want)
			}
		})
	}
}

func TestReferenceCodeLensCalculatingTitleJapanese(t *testing.T) {
	if got := referenceCodeLensCalculatingTitle(nil, nil, true); got != "参照数を計算中" {
		t.Fatalf("referenceCodeLensCalculatingTitle() = %q, want %q", got, "参照数を計算中")
	}
}

func TestStdioParityCountsWorkspaceVBScriptReferencesByDefault(t *testing.T) {
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
Response.Write SharedTitle()
%>`)
	commonURI := pathToFileURI(common)

	client := startStdioTestClient(t)
	defer client.close()
	initializeAndWaitForWorkspaceIndex(t, client, map[string]any{
		"processId":    nil,
		"rootUri":      pathToFileURI(root),
		"capabilities": map[string]any{},
	})
	notifyOpenClassicASPDocument(t, client, commonURI, commonSource)
	waitForDiagnosticsContaining(t, client, "")

	codeLens := client.request("textDocument/codeLens", map[string]any{
		"textDocument": map[string]any{"uri": commonURI},
	})
	referencesCodeLens := referenceCodeLensWithLine(t, codeLens.Result, 1)
	response := make(chan any, 1)
	go func() {
		response <- client.request("codeLens/resolve", referencesCodeLens).Result
	}()
	// Match the resolve task ID; batch labels such as references.countDocuments share the prefix.
	status := client.waitForNotification("aspLsp/status", `"id":"references.count-`)
	statusText := string(status.Params)
	if !strings.Contains(statusText, "references.count") ||
		!strings.Contains(statusText, "SharedTitle") ||
		!strings.Contains(statusText, `"documentUri":"`+commonURI+`"`) ||
		!strings.Contains(statusText, `"documentVersion":1`) {
		t.Fatalf("workspace reference status mismatch: %s", statusText)
	}
	defaultText := mustJSONText(t, <-response)
	if !strings.Contains(defaultText, "1 reference") ||
		strings.Contains(defaultText, "(analyzed only)") {
		t.Fatalf("default workspace CodeLens mismatch: %s", defaultText)
	}
	client.waitForNotification("aspLsp/status", `"idle"`)

	references := client.request("textDocument/references", map[string]any{
		"textDocument": map[string]any{"uri": commonURI},
		"position":     map[string]any{"line": 1, "character": 10},
		"context":      map[string]any{"includeDeclaration": false},
	})
	if !strings.Contains(mustJSONText(t, references.Result), pathToFileURI(page)) {
		t.Fatalf("workspace references missing workspace usage: %s", mustJSONText(t, references.Result))
	}
}

func TestStdioParityPublishesWorkspaceGraphStatusForReferenceGraphBuilds(t *testing.T) {
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

	client := startStdioTestClient(t)
	defer client.close()
	initializeAndWaitForWorkspaceIndex(t, client, map[string]any{
		"processId":    nil,
		"rootUri":      pathToFileURI(root),
		"capabilities": map[string]any{},
	})
	notifyConfiguration(t, client, map[string]any{"aspLsp": map[string]any{
		"diagnostics": map[string]any{"debounceMs": 0},
	}})
	waitForWorkspaceIndexRefresh(t, client)
	notifyOpenClassicASPDocument(t, client, firstURI, firstSource)
	waitForDiagnosticsContaining(t, client, "")

	codeLens := client.request("textDocument/codeLens", map[string]any{
		"textDocument": map[string]any{"uri": firstURI},
	})
	referencesCodeLens := referenceCodeLensWithData(t, codeLens.Result, "a", "variable")
	resolvedCodeLens := client.request("codeLens/resolve", referencesCodeLens)
	resolvedText := mustJSONText(t, resolvedCodeLens.Result)
	if !strings.Contains(resolvedText, "2 references") {
		t.Fatalf("workspace variable CodeLens count mismatch: %s", resolvedText)
	}
	client.waitForNotification("aspLsp/status", `"idle"`)

	graphResponse := make(chan any, 1)
	go func() {
		graphResponse <- client.request("workspace/executeCommand", map[string]any{
			"command":   "aspLsp.server.buildGraph",
			"arguments": []map[string]any{{"scope": "workspace"}},
		}).Result
	}()
	status := client.waitForNotification("aspLsp/status", "graph.workspace")
	if !strings.Contains(string(status.Params), "graph.workspace") {
		t.Fatalf("workspace graph status mismatch: %s", string(status.Params))
	}
	var payload graph.Payload
	mustDecodeResult(t, <-graphResponse, &payload)
	client.waitForNotification("aspLsp/status", `"idle"`)

	aNode := graphNodeByLabelAndURI(payload, "a", firstURI)
	if aNode == nil {
		t.Fatalf("workspace graph missing first a declaration: %s", mustJSONText(t, payload))
	}
	if countGraphReferencesTo(payload, aNode.ID) != 2 {
		t.Fatalf("workspace graph a reference count mismatch: %s", mustJSONText(t, payload))
	}
}

func TestStdioParityFiltersVBScriptReferenceCodeLensEntriesBySymbolCategorySettings(t *testing.T) {
	client := startStdioTestClient(t)
	defer client.close()

	root := t.TempDir()
	uri := pathToFileURI(filepath.Join(root, "reference-codelens-categories.asp"))
	source := `<%
Dim globalValue
Const globalConst = 1
Class Customer
  Public Name
  Const Kind = "vip"
  Public Property Get DisplayName()
    DisplayName = Name
  End Property
  Public Sub Save()
  End Sub
End Class
Function BuildName()
  BuildName = globalValue & globalConst
End Function
Sub Render()
  Dim localValue
  Const localConst = 2
  Dim item
  Set item = New Customer
  item.Name = BuildName()
  item.Save
  Response.Write item.DisplayName
End Sub
%>`
	client.request("initialize", map[string]any{
		"processId":    nil,
		"rootUri":      pathToFileURI(root),
		"capabilities": map[string]any{},
	})
	notifyOpenClassicASPDocument(t, client, uri, source)
	waitForDiagnosticsContaining(t, client, "")

	defaultKeys := referenceCodeLensKeys(t, client, uri, nil)
	for _, key := range []string{
		"variable:globalValue",
		"constant:globalConst",
		"class:Customer",
		"field:Name",
		"constant:Kind",
		"property:DisplayName",
		"method:Save",
		"function:BuildName",
		"sub:Render",
	} {
		if !defaultKeys[key] {
			t.Fatalf("default reference CodeLens missing %s: %#v", key, defaultKeys)
		}
	}
	for _, key := range []string{"variable:localValue", "constant:localConst", "variable:item"} {
		if defaultKeys[key] {
			t.Fatalf("default reference CodeLens should exclude local %s: %#v", key, defaultKeys)
		}
	}

	withoutProcedures := referenceCodeLensKeys(t, client, uri, map[string]any{
		"referenceProcedures":   false,
		"referenceGlobals":      true,
		"referenceClasses":      true,
		"referenceClassMembers": true,
	})
	for _, key := range []string{"function:BuildName", "sub:Render", "method:Save", "property:DisplayName"} {
		if withoutProcedures[key] {
			t.Fatalf("procedure-disabled CodeLens included %s: %#v", key, withoutProcedures)
		}
	}
	for _, key := range []string{"variable:globalValue", "class:Customer", "field:Name"} {
		if !withoutProcedures[key] {
			t.Fatalf("procedure-disabled CodeLens missing %s: %#v", key, withoutProcedures)
		}
	}

	withoutGlobals := referenceCodeLensKeys(t, client, uri, map[string]any{
		"referenceProcedures":   true,
		"referenceGlobals":      false,
		"referenceClasses":      true,
		"referenceClassMembers": true,
	})
	for _, key := range []string{"variable:globalValue", "constant:globalConst"} {
		if withoutGlobals[key] {
			t.Fatalf("global-disabled CodeLens included %s: %#v", key, withoutGlobals)
		}
	}
	for _, key := range []string{"constant:Kind", "function:BuildName"} {
		if !withoutGlobals[key] {
			t.Fatalf("global-disabled CodeLens missing %s: %#v", key, withoutGlobals)
		}
	}

	withoutClasses := referenceCodeLensKeys(t, client, uri, map[string]any{
		"referenceProcedures":   true,
		"referenceGlobals":      true,
		"referenceClasses":      false,
		"referenceClassMembers": true,
	})
	if withoutClasses["class:Customer"] {
		t.Fatalf("class-disabled CodeLens included class:Customer: %#v", withoutClasses)
	}
	if !withoutClasses["field:Name"] {
		t.Fatalf("class-disabled CodeLens missing field:Name: %#v", withoutClasses)
	}

	withoutClassMembers := referenceCodeLensKeys(t, client, uri, map[string]any{
		"referenceProcedures":   true,
		"referenceGlobals":      true,
		"referenceClasses":      true,
		"referenceClassMembers": false,
	})
	for _, key := range []string{"field:Name", "constant:Kind"} {
		if withoutClassMembers[key] {
			t.Fatalf("class-member-disabled CodeLens included %s: %#v", key, withoutClassMembers)
		}
	}
	for _, key := range []string{"property:DisplayName", "class:Customer"} {
		if !withoutClassMembers[key] {
			t.Fatalf("class-member-disabled CodeLens missing %s: %#v", key, withoutClassMembers)
		}
	}
}

func referenceCodeLensKeys(t *testing.T, client *stdioTestClient, uri string, codeLensSettings map[string]any) map[string]bool {
	t.Helper()
	if codeLensSettings != nil {
		notifyConfiguration(t, client, map[string]any{"aspLsp": map[string]any{
			"codeLens": codeLensSettings,
		}})
	}
	codeLens := client.request("textDocument/codeLens", map[string]any{
		"textDocument": map[string]any{"uri": uri},
	})
	var lenses []struct {
		Data map[string]any `json:"data"`
	}
	mustDecodeResult(t, codeLens.Result, &lenses)
	keys := map[string]bool{}
	for _, lens := range lenses {
		if lens.Data["kind"] != "vbscript-reference" {
			continue
		}
		name, nameOK := lens.Data["name"].(string)
		symbolKind, symbolKindOK := lens.Data["symbolKind"].(string)
		if nameOK && symbolKindOK {
			keys[symbolKind+":"+name] = true
		}
	}
	return keys
}
