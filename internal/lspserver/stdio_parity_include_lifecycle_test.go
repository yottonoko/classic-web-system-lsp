package lspserver

import (
	"os"
	"path/filepath"
	"strings"
	"testing"
	"time"
)

func TestStdioParityReportsSelfIncludeAsCurrentDocument(t *testing.T) {
	client := startStdioTestClient(t)
	defer client.close()

	root := t.TempDir()
	path := filepath.Join(root, "default.asp")
	source := `<!-- #include file="default.asp" -->`
	writeIncludeLifecycleFixture(t, path, source)
	uri := pathToFileURI(path)
	client.request("initialize", map[string]any{
		"processId": nil, "rootUri": pathToFileURI(root), "capabilities": map[string]any{},
	})
	openClassicASPDocument(t, client, uri, source)
	response := client.request("textDocument/diagnostic", map[string]any{"textDocument": map[string]any{"uri": uri}})
	serialized := mustJSONText(t, response.Result)
	if !strings.Contains(serialized, `"code":"include.currentDocument"`) || !strings.Contains(serialized, "Include file references the current document.") {
		t.Fatalf("self include diagnostic mismatch: %s", serialized)
	}
	if strings.Contains(serialized, `"code":"include.cycle"`) {
		t.Fatalf("self include should not be reported as a generic cycle: %s", serialized)
	}
}

func TestStdioParityUpdatesIncludeDirectivesForFileRenameOperations(t *testing.T) {
	client := startStdioTestClient(t)
	defer client.close()

	root := t.TempDir()
	owner := filepath.Join(root, "default.asp")
	include := filepath.Join(root, "common.inc")
	renamed := filepath.Join(root, "renamed.inc")
	writeIncludeLifecycleFixture(t, include, `<% Dim sharedValue %>`)
	source := `<!-- #include file="common.inc" -->
<% Response.Write sharedValue %>`
	writeIncludeLifecycleFixture(t, owner, source)
	uri := pathToFileURI(owner)
	client.request("initialize", map[string]any{
		"processId":    nil,
		"rootUri":      pathToFileURI(root),
		"capabilities": map[string]any{},
	})
	notifyOpenClassicASPDocument(t, client, uri, source)
	waitForDiagnosticsContaining(t, client, "")

	disabledEdit := client.request("workspace/willRenameFiles", map[string]any{
		"files": []map[string]any{{"oldUri": pathToFileURI(include), "newUri": pathToFileURI(renamed)}},
	})
	if disabledEdit.Result != nil {
		t.Fatalf("rename edit should be disabled by default: %s", mustJSONText(t, disabledEdit.Result))
	}

	notifyConfiguration(t, client, map[string]any{"aspLsp": map[string]any{
		"rename": map[string]any{"updateIncludesOnFileRename": true},
	}})
	edit := client.request("workspace/willRenameFiles", map[string]any{
		"files": []map[string]any{{"oldUri": pathToFileURI(include), "newUri": pathToFileURI(renamed)}},
	})
	if !strings.Contains(mustJSONText(t, edit.Result), "renamed.inc") {
		t.Fatalf("rename edit missing renamed.inc: %s", mustJSONText(t, edit.Result))
	}
}

func TestStdioParityResolvesWindowsStyleIncludePathsAndReportsPathCasingMismatches(t *testing.T) {
	client := startStdioTestClient(t)
	defer client.close()

	root := t.TempDir()
	mismatchOwner := filepath.Join(root, "mismatch.asp")
	exactOwner := filepath.Join(root, "exact.asp")
	mixedCaseInclude := filepath.Join(root, "aB.asp")
	upperCaseInclude := filepath.Join(root, "BA.asp")
	writeIncludeLifecycleFixture(t, mixedCaseInclude, `<%
Function SharedFromMixed()
End Function
%>`)
	writeIncludeLifecycleFixture(t, upperCaseInclude, `<%
Function SharedFromUpper()
End Function
%>`)
	mismatch := markedDocument(`<!-- #include file="ab.asp" -->
<%
Response.Write Shared<<<caret>>>FromMixed()
%>`)
	exact := `<!-- #include file="BA.asp" -->
<%
Response.Write SharedFromUpper()
%>`
	writeIncludeLifecycleFixture(t, mismatchOwner, mismatch.Text)
	writeIncludeLifecycleFixture(t, exactOwner, exact)
	mismatchURI := pathToFileURI(mismatchOwner)
	exactURI := pathToFileURI(exactOwner)
	client.request("initialize", map[string]any{
		"processId":    nil,
		"rootUri":      pathToFileURI(root),
		"capabilities": map[string]any{},
	})
	notifyOpenClassicASPDocument(t, client, mismatchURI, mismatch.Text)

	diagnostics := waitForDiagnosticsContaining(t, client, "include.pathCaseMismatch")
	includeDiagnosticsText := string(diagnostics.Params)
	if !strings.Contains(includeDiagnosticsText, "aB.asp") {
		t.Fatalf("path case diagnostics missing resolved casing: %s", includeDiagnosticsText)
	}
	definition := client.request("textDocument/definition", map[string]any{
		"textDocument": map[string]any{"uri": mismatchURI},
		"position":     mismatch.Position,
	})
	if !strings.Contains(mustJSONText(t, definition.Result), pathToFileURI(mixedCaseInclude)) {
		t.Fatalf("definition did not resolve mixed-case include: %s", mustJSONText(t, definition.Result))
	}
	completions := completionLabels(client.request("textDocument/completion", map[string]any{
		"textDocument": map[string]any{"uri": mismatchURI},
		"position":     mismatch.Position,
	}).Result)
	if !completions.contains("SharedFromMixed") {
		t.Fatalf("completion missing SharedFromMixed: %#v", completions)
	}

	notifyOpenClassicASPDocument(t, client, exactURI, exact)
	exactDiagnostics := client.request("textDocument/diagnostic", map[string]any{
		"textDocument": map[string]any{"uri": exactURI},
	})
	exactText := mustJSONText(t, exactDiagnostics.Result)
	if strings.Contains(exactText, "include.pathCaseMismatch") || strings.Contains(exactText, "could not be resolved") {
		t.Fatalf("exact include produced path casing diagnostics: %s", exactText)
	}
}

func TestStdioParityCanDisableWindowsStyleIncludePathCasingDiagnostics(t *testing.T) {
	client := startStdioTestClient(t)
	defer client.close()

	root := t.TempDir()
	owner := filepath.Join(root, "default.asp")
	writeIncludeLifecycleFixture(t, filepath.Join(root, "aB.asp"), `<%
Function SharedFromMixed()
End Function
%>`)
	source := `<!-- #include file="ab.asp" -->
<%
Response.Write SharedFromMixed()
%>`
	writeIncludeLifecycleFixture(t, owner, source)
	uri := pathToFileURI(owner)
	client.request("initialize", map[string]any{
		"processId":    nil,
		"rootUri":      pathToFileURI(root),
		"capabilities": map[string]any{},
	})
	notifyConfiguration(t, client, map[string]any{"aspLsp": map[string]any{"windowsPathResolution": false}})
	notifyOpenClassicASPDocument(t, client, uri, source)

	diagnostics := client.request("textDocument/diagnostic", map[string]any{
		"textDocument": map[string]any{"uri": uri},
	})
	if strings.Contains(mustJSONText(t, diagnostics.Result), "include.pathCaseMismatch") {
		t.Fatalf("disabled path casing diagnostics still reported: %s", mustJSONText(t, diagnostics.Result))
	}
}

func TestStdioParityReportsIncludeCyclesThroughPublishDiagnostics(t *testing.T) {
	client := startStdioTestClient(t)
	defer client.close()

	root := t.TempDir()
	owner := filepath.Join(root, "default.asp")
	include := filepath.Join(root, "loop.inc")
	writeIncludeLifecycleFixture(t, owner, `<!-- #include file="loop.inc" -->
<% Response.Write 1 %>`)
	writeIncludeLifecycleFixture(t, include, `<!-- #include file="default.asp" -->`)
	uri := pathToFileURI(owner)
	client.request("initialize", map[string]any{
		"processId":    nil,
		"rootUri":      pathToFileURI(root),
		"capabilities": map[string]any{},
	})
	notifyOpenClassicASPDocument(t, client, uri, mustReadText(t, owner))

	diagnostics := waitForDiagnosticsContaining(t, client, "Include cycle detected")
	serialized := string(diagnostics.Params)
	if !strings.Contains(serialized, "include.cycle") {
		t.Fatalf("include cycle diagnostics missing code: %s", serialized)
	}
}

func TestStdioParityReportsTransitiveIncludeCyclesThatDoNotReturnToTheOwner(t *testing.T) {
	client := startStdioTestClient(t)
	defer client.close()

	root := t.TempDir()
	owner := filepath.Join(root, "default.asp")
	first := filepath.Join(root, "first.inc")
	second := filepath.Join(root, "second.inc")
	writeIncludeLifecycleFixture(t, owner, `<!-- #include file="first.inc" -->`)
	writeIncludeLifecycleFixture(t, first, `<!-- #include file="second.inc" -->`)
	writeIncludeLifecycleFixture(t, second, `<!-- #include file="first.inc" -->`)
	uri := pathToFileURI(owner)
	client.request("initialize", map[string]any{
		"processId":    nil,
		"rootUri":      pathToFileURI(root),
		"capabilities": map[string]any{},
	})
	notifyOpenClassicASPDocument(t, client, uri, mustReadText(t, owner))

	diagnostics := waitForDiagnosticsContaining(t, client, "Include cycle detected")
	serialized := string(diagnostics.Params)
	for _, expected := range []string{"first.inc", "second.inc"} {
		if !strings.Contains(serialized, expected) {
			t.Fatalf("transitive include cycle diagnostics missing %s: %s", expected, serialized)
		}
	}
}

func TestStdioParityReportsIncludeCyclesWithoutSwallowingJavaScriptDiagnosticsAroundASPIslands(t *testing.T) {
	client := startStdioTestClient(t)
	defer client.close()

	root := t.TempDir()
	owner := filepath.Join(root, "default.asp")
	first := filepath.Join(root, "first.inc")
	second := filepath.Join(root, "second.inc")
	third := filepath.Join(root, "third.inc")
	source := `<!-- #include file="first.inc" -->
<script>
const cycleValue = <%= CycleValue %>;
const shifted = <% Response.Write ShiftedValue %>;
missingCycleJs.toFixed();
</script>`
	writeIncludeLifecycleFixture(t, owner, source)
	writeIncludeLifecycleFixture(t, first, `<!-- #include file="second.inc" -->`)
	writeIncludeLifecycleFixture(t, second, `<!-- #include file="third.inc" -->`)
	writeIncludeLifecycleFixture(t, third, `<!-- #include file="first.inc" -->`)
	uri := pathToFileURI(owner)
	client.request("initialize", map[string]any{
		"processId":    nil,
		"rootUri":      pathToFileURI(root),
		"capabilities": map[string]any{},
	})
	notifyConfiguration(t, client, map[string]any{"aspLsp": map[string]any{"checkJs": true}})
	notifyOpenClassicASPDocument(t, client, uri, source)

	diagnostics := waitForDiagnosticsContaining(t, client, "missingCycleJs")
	serialized := string(diagnostics.Params)
	for _, expected := range []string{"Include cycle detected", "include.cycle", "asp-lsp-typescript", "missingCycleJs"} {
		if !strings.Contains(serialized, expected) {
			t.Fatalf("cycle plus JavaScript diagnostics missing %s: %s", expected, serialized)
		}
	}
}

func TestStdioParityRefreshesDependentDiagnosticsAfterIncludeDirectiveChanges(t *testing.T) {
	client := startStdioTestClient(t)
	defer client.close()

	root := t.TempDir()
	owner := filepath.Join(root, "default.asp")
	include := filepath.Join(root, "shared.inc")
	writeIncludeLifecycleFixture(t, owner, `<!-- #include file="shared.inc" -->`)
	writeIncludeLifecycleFixture(t, include, `<% Const SharedValue = "ok" %>`)
	uri := pathToFileURI(owner)
	client.request("initialize", map[string]any{
		"processId":    nil,
		"rootUri":      pathToFileURI(root),
		"capabilities": map[string]any{},
	})
	notifyConfiguration(t, client, map[string]any{"aspLsp": map[string]any{"debug": map[string]any{"output": "verbose"}}})
	notifyOpenClassicASPDocument(t, client, uri, mustReadText(t, owner))
	initialDiagnostics := waitForDiagnosticsContaining(t, client, "")
	if strings.Contains(string(initialDiagnostics.Params), "Include cycle detected") {
		t.Fatalf("initial diagnostics unexpectedly reported include cycle: %s", string(initialDiagnostics.Params))
	}

	writeIncludeLifecycleFixture(t, include, `<!-- #include file="default.asp" -->`)
	if err := client.notify("workspace/didChangeWatchedFiles", map[string]any{
		"changes": []map[string]any{{"uri": pathToFileURI(include), "type": 2}},
	}); err != nil {
		t.Fatal(err)
	}
	refreshedDiagnostics := waitForDiagnosticsContaining(t, client, "Include cycle detected")
	if !strings.Contains(string(refreshedDiagnostics.Params), "include.cycle") {
		t.Fatalf("refreshed diagnostics missing include.cycle: %s", string(refreshedDiagnostics.Params))
	}
}

func TestStdioParityInvalidatesMissingIncludeResolutionWhenNonASPTargetFileIsCreated(t *testing.T) {
	client := startStdioTestClient(t)
	defer client.close()

	root := t.TempDir()
	page := filepath.Join(root, "default.asp")
	include := filepath.Join(root, "fragment.txt")
	source := `<!-- #include file="fragment.txt" -->
<% Response.Write "ok" %>`
	writeIncludeLifecycleFixture(t, page, source)
	uri := pathToFileURI(page)
	client.request("initialize", map[string]any{
		"processId":    nil,
		"rootUri":      pathToFileURI(root),
		"capabilities": map[string]any{},
	})
	notifyConfiguration(t, client, map[string]any{"aspLsp": map[string]any{
		"debug":       map[string]any{"output": "verbose"},
		"diagnostics": map[string]any{"debounceMs": 0},
	}})
	notifyOpenClassicASPDocument(t, client, uri, source)
	waitForDiagnosticsContaining(t, client, "fragment.txt")

	writeIncludeLifecycleFixture(t, include, `<% Const FragmentValue = 1 %>`)
	if err := client.notify("workspace/didChangeWatchedFiles", map[string]any{
		"changes": []map[string]any{{"uri": pathToFileURI(include), "type": 1}},
	}); err != nil {
		t.Fatal(err)
	}
	time.Sleep(50 * time.Millisecond)
	diagnostics := client.request("textDocument/diagnostic", map[string]any{
		"textDocument": map[string]any{"uri": uri},
	})
	if strings.Contains(mustJSONText(t, diagnostics.Result), "fragment.txt") {
		t.Fatalf("created non-ASP include still reported missing: %s", mustJSONText(t, diagnostics.Result))
	}
}

func TestStdioParityInvalidatesExistingIncludeResolutionWhenNonASPTargetFileIsDeleted(t *testing.T) {
	client := startStdioTestClient(t)
	defer client.close()

	root := t.TempDir()
	page := filepath.Join(root, "default.asp")
	include := filepath.Join(root, "fragment.txt")
	source := `<!-- #include file="fragment.txt" -->
<% Response.Write "ok" %>`
	writeIncludeLifecycleFixture(t, page, source)
	writeIncludeLifecycleFixture(t, include, `<% Const FragmentValue = 1 %>`)
	uri := pathToFileURI(page)
	client.request("initialize", map[string]any{
		"processId":    nil,
		"rootUri":      pathToFileURI(root),
		"capabilities": map[string]any{},
	})
	notifyConfiguration(t, client, map[string]any{"aspLsp": map[string]any{
		"debug":       map[string]any{"output": "verbose"},
		"diagnostics": map[string]any{"debounceMs": 0},
	}})
	notifyOpenClassicASPDocument(t, client, uri, source)
	client.request("textDocument/diagnostic", map[string]any{
		"textDocument": map[string]any{"uri": uri},
	})

	if err := os.Remove(include); err != nil {
		t.Fatal(err)
	}
	if err := client.notify("workspace/didChangeWatchedFiles", map[string]any{
		"changes": []map[string]any{{"uri": pathToFileURI(include), "type": 3}},
	}); err != nil {
		t.Fatal(err)
	}
	time.Sleep(50 * time.Millisecond)
	diagnostics := client.request("textDocument/diagnostic", map[string]any{
		"textDocument": map[string]any{"uri": uri},
	})
	if !strings.Contains(mustJSONText(t, diagnostics.Result), "fragment.txt") {
		t.Fatalf("deleted non-ASP include did not report missing include: %s", mustJSONText(t, diagnostics.Result))
	}
}

func writeIncludeLifecycleFixture(t *testing.T, path string, text string) {
	t.Helper()
	if err := os.WriteFile(path, []byte(text), 0o644); err != nil {
		t.Fatal(err)
	}
}
