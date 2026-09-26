package lspserver

import (
	"io"
	"os"
	"path/filepath"
	"strings"
	"testing"
)

func TestWorkspaceGlobParitySupportsGlobstarBracesAndCharacterClasses(t *testing.T) {
	tests := []struct {
		pattern  string
		path     string
		expected bool
	}{
		{"**/*.{asp,asa,inc,vbs}", "default.asp", true},
		{"**/*.{asp,asa,inc,vbs}", "scripts/jobs/worker.VBS", true},
		{"**/*.{asp,asa,inc,vbs}", "scripts/jobs/worker.js", false},
		{"src/**/page?.[ai][sn][pc]", "src/admin/page1.asp", true},
		{"*.inc", "nested/common.inc", true},
	}
	for _, test := range tests {
		if actual := matchWorkspaceGlob(test.pattern, test.path); actual != test.expected {
			t.Errorf("matchWorkspaceGlob(%q, %q) = %v, want %v", test.pattern, test.path, actual, test.expected)
		}
	}
}

func TestWorkspaceGitIgnoreParityHonorsNegationAndIgnoredAncestors(t *testing.T) {
	rules := []string{"ignored/**", "!ignored/keep.asp"}
	if workspaceGitIgnoreGlobsIgnorePath("ignored/drop.asp", rules) != true {
		t.Fatal("gitignore did not ignore a file below an ignored directory")
	}
	if workspaceGitIgnoreGlobsIgnorePath("ignored/keep.asp", rules) != false {
		t.Fatal("gitignore negation did not restore an explicitly allowed file")
	}
	if workspaceGitIgnoreGlobsIgnorePath("other/ignored/drop.asp", []string{"ignored"}) != true {
		t.Fatal("gitignore basename rule did not ignore a matching ancestor directory")
	}
}

func TestWorkspaceGraphFileFilterKeepsRootSpecificRules(t *testing.T) {
	filter := newWorkspaceGraphFileFilter(
		[]string{"**/*.{asp,inc}"},
		[]string{"vendor/**"},
		map[string][]string{
			"root-a": {"ignored/**", "!ignored/keep.asp"},
			"root-b": {"private/**"},
		},
	)
	tests := []struct {
		path string
		root string
		want bool
	}{
		{path: "default.asp", root: "root-a", want: true},
		{path: "worker.vbs", root: "root-a", want: false},
		{path: "vendor/shared.inc", root: "root-a", want: false},
		{path: "ignored/drop.asp", root: "root-a", want: false},
		{path: "ignored/keep.asp", root: "root-a", want: true},
		{path: "private/page.asp", root: "root-a", want: true},
		{path: "private/page.asp", root: "root-b", want: false},
	}
	for _, test := range tests {
		if got := filter.allows(test.path, test.root); got != test.want {
			t.Errorf("filter.allows(%q, %q) = %t, want %t", test.path, test.root, got, test.want)
		}
	}
}

func TestNestedGitIgnoreRulesAreRelativeToTheirDirectory(t *testing.T) {
	root := t.TempDir()
	nested := filepath.Join(root, "site", "private")
	if err := os.MkdirAll(nested, 0o755); err != nil {
		t.Fatal(err)
	}
	if err := os.WriteFile(filepath.Join(root, ".gitignore"), []byte("/root-only.asp\n"), 0o644); err != nil {
		t.Fatal(err)
	}
	if err := os.WriteFile(filepath.Join(nested, ".gitignore"), []byte("*.inc\n!keep.inc\n"), 0o644); err != nil {
		t.Fatal(err)
	}
	server := New(nil, io.Discard, io.Discard)
	rules := server.readGitIgnoreGlobs(root)
	for path, ignored := range map[string]bool{
		"root-only.asp": true, "nested/root-only.asp": false,
		"site/private/drop.inc": true, "site/private/deeper/drop.inc": true,
		"site/private/keep.inc": false, "site/public/drop.inc": false,
	} {
		if got := workspaceGitIgnoreGlobsIgnorePath(path, rules); got != ignored {
			t.Errorf("gitignore(%q) = %v, want %v; rules=%#v", path, got, ignored, rules)
		}
	}
}

func TestWorkspaceDiskSettingsKeyChangesWithGitIgnoreContents(t *testing.T) {
	root := t.TempDir()
	ignorePath := filepath.Join(root, ".gitignore")
	if err := os.WriteFile(ignorePath, []byte("first.asp\n"), 0o644); err != nil {
		t.Fatal(err)
	}
	server := New(nil, io.Discard, io.Discard)
	server.workspaceRoots = []workspaceRoot{{Path: root, URI: filePathURI(root)}}
	server.settings.WorkspaceRespectGitIgnore = true
	first := server.workspaceDiskSettingsKey()
	server.invalidateFsPath(ignorePath)
	server.invalidateSourceSnapshot(ignorePath)
	if err := os.WriteFile(ignorePath, []byte("second.asp\n"), 0o644); err != nil {
		t.Fatal(err)
	}
	server.invalidateFsPath(ignorePath)
	server.invalidateSourceSnapshot(ignorePath)
	second := server.workspaceDiskSettingsKey()
	if first == second {
		t.Fatalf("workspace settings key ignored .gitignore content change: %s", first)
	}
}

func TestStdioParityWatcherSkipsExcludedFilesUnlessExplicitlyOpened(t *testing.T) {
	client := startStdioTestClient(t)
	defer client.close()
	root := t.TempDir()
	initializeAndWaitForWorkspaceIndex(t, client, map[string]any{
		"processId": nil, "rootUri": pathToFileURI(root), "capabilities": map[string]any{},
	})
	notifyConfiguration(t, client, map[string]any{"aspLsp": map[string]any{
		"workspace": map[string]any{"excludes": []string{"ignored/**"}},
	}})
	waitForWorkspaceIndexRefresh(t, client)
	ignored := filepath.Join(root, "ignored", "hidden.asp")
	if err := os.MkdirAll(filepath.Dir(ignored), 0o755); err != nil {
		t.Fatal(err)
	}
	writeIncludeLifecycleFixture(t, ignored, "<%\nSub HiddenWorker()\nEnd Sub\n%>\n")
	if err := client.notify("workspace/didChangeWatchedFiles", map[string]any{"changes": []map[string]any{{"uri": pathToFileURI(ignored), "type": 1}}}); err != nil {
		t.Fatal(err)
	}
	if symbols := mustJSONText(t, client.request("workspace/symbol", map[string]any{"query": "HiddenWorker"}).Result); strings.Contains(symbols, "HiddenWorker") {
		t.Fatalf("excluded watcher file entered workspace index: %s", symbols)
	}
	ignoredURI := pathToFileURI(ignored)
	openClassicASPDocument(t, client, ignoredURI, "<%\nSub HiddenWorker()\nEnd Sub\n%>\n")
	documentSymbols := mustJSONText(t, client.request("textDocument/documentSymbol", map[string]any{"textDocument": map[string]any{"uri": ignoredURI}}).Result)
	if !strings.Contains(documentSymbols, "HiddenWorker") {
		t.Fatalf("explicitly opened excluded file was not analyzed: %s", documentSymbols)
	}
	if err := client.notify("workspace/didChangeWatchedFiles", map[string]any{"changes": []map[string]any{{"uri": ignoredURI, "type": 2}}}); err != nil {
		t.Fatal(err)
	}
	if err := client.notify("textDocument/didClose", map[string]any{"textDocument": map[string]any{"uri": ignoredURI}}); err != nil {
		t.Fatal(err)
	}
	if symbols := mustJSONText(t, client.request("workspace/symbol", map[string]any{"query": "HiddenWorker"}).Result); strings.Contains(symbols, "HiddenWorker") {
		t.Fatalf("closed excluded file remained in workspace index: %s", symbols)
	}
}

func TestStdioParityAdvertisesWorkspaceAndFileOperationCapabilities(t *testing.T) {
	client := startStdioTestClient(t)
	defer client.close()

	result := client.request("initialize", map[string]any{
		"processId": nil, "rootUri": pathToFileURI(t.TempDir()), "capabilities": map[string]any{},
	})
	text := mustJSONText(t, result.Result)
	for _, expected := range []string{
		`"colorProvider":true`, `"workspaceFolders":{"changeNotifications":true,"supported":true}`,
		`"fileOperations"`, `"glob":"**/*.{asp,asa,inc,vbs}"`, `"ignoreCase":true`,
	} {
		if !strings.Contains(text, expected) {
			t.Fatalf("initialize capabilities missing %q: %s", expected, text)
		}
	}
	if strings.Contains(text, "documentColorProvider") {
		t.Fatalf("initialize capabilities contain non-standard documentColorProvider: %s", text)
	}
}

func TestStdioParityIndexesAndWatchesUnopenedVBSFiles(t *testing.T) {
	client := startStdioTestClient(t)
	defer client.close()

	root := t.TempDir()
	first := filepath.Join(root, "first.vbs")
	writeIncludeLifecycleFixture(t, first, "Function FirstWorker()\nEnd Function\n")
	initializeAndWaitForWorkspaceIndex(t, client, map[string]any{
		"processId": nil, "rootUri": pathToFileURI(root), "capabilities": map[string]any{},
	})
	firstSymbols := mustJSONText(t, client.request("workspace/symbol", map[string]any{"query": "FirstWorker"}).Result)
	if !strings.Contains(firstSymbols, "FirstWorker") || !strings.Contains(firstSymbols, pathToFileURI(first)) {
		t.Fatalf("unopened .vbs was not indexed: %s", firstSymbols)
	}
	writeIncludeLifecycleFixture(t, first, "Function WatchedWorker()\nEnd Function\n")
	if err := client.notify("workspace/didChangeWatchedFiles", map[string]any{"changes": []map[string]any{{"uri": pathToFileURI(first), "type": 2}}}); err != nil {
		t.Fatal(err)
	}
	watchedSymbols := mustJSONText(t, client.request("workspace/symbol", map[string]any{"query": "Worker"}).Result)
	if strings.Contains(watchedSymbols, "FirstWorker") || !strings.Contains(watchedSymbols, "WatchedWorker") {
		t.Fatalf("changed unopened .vbs index mismatch: %s", watchedSymbols)
	}

	second := filepath.Join(root, "second.vbs")
	writeIncludeLifecycleFixture(t, second, "Sub CreatedWorker()\nEnd Sub\n")
	if err := client.notify("workspace/didCreateFiles", map[string]any{"files": []map[string]any{{"uri": pathToFileURI(second)}}}); err != nil {
		t.Fatal(err)
	}
	createdSymbols := mustJSONText(t, client.request("workspace/symbol", map[string]any{"query": "CreatedWorker"}).Result)
	if !strings.Contains(createdSymbols, "CreatedWorker") {
		t.Fatalf("created unopened .vbs was not indexed: %s", createdSymbols)
	}

	if err := os.Remove(second); err != nil {
		t.Fatal(err)
	}
	if err := client.notify("workspace/didDeleteFiles", map[string]any{"files": []map[string]any{{"uri": pathToFileURI(second)}}}); err != nil {
		t.Fatal(err)
	}
	deletedSymbols := mustJSONText(t, client.request("workspace/symbol", map[string]any{"query": "CreatedWorker"}).Result)
	if strings.Contains(deletedSymbols, "CreatedWorker") {
		t.Fatalf("deleted unopened .vbs remained indexed: %s", deletedSymbols)
	}
}

func TestStdioParityUpdatesWorkspaceFoldersAndRenameLifecycle(t *testing.T) {
	client := startStdioTestClient(t)
	defer client.close()

	rootA := t.TempDir()
	rootB := t.TempDir()
	fileA := filepath.Join(rootA, "a.vbs")
	fileB := filepath.Join(rootB, "b.vbs")
	writeIncludeLifecycleFixture(t, fileA, "Sub RootAWorker()\nEnd Sub\n")
	writeIncludeLifecycleFixture(t, fileB, "Sub RootBWorker()\nEnd Sub\n")
	initializeAndWaitForWorkspaceIndex(t, client, map[string]any{
		"processId": nil, "workspaceFolders": []map[string]any{{"uri": pathToFileURI(rootA), "name": "a"}}, "capabilities": map[string]any{},
	})
	if err := client.notify("workspace/didChangeWorkspaceFolders", map[string]any{"event": map[string]any{
		"removed": []map[string]any{{"uri": pathToFileURI(rootA), "name": "a"}},
		"added":   []map[string]any{{"uri": pathToFileURI(rootB), "name": "b"}},
	}}); err != nil {
		t.Fatal(err)
	}
	waitForWorkspaceIndexRefresh(t, client)
	if text := mustJSONText(t, client.request("workspace/symbol", map[string]any{"query": "Worker"}).Result); strings.Contains(text, "RootAWorker") || !strings.Contains(text, "RootBWorker") {
		t.Fatalf("workspace folder replacement was not reflected in the index: %s", text)
	}

	renamed := filepath.Join(rootB, "renamed.vbs")
	if err := os.Rename(fileB, renamed); err != nil {
		t.Fatal(err)
	}
	if err := client.notify("workspace/didRenameFiles", map[string]any{"files": []map[string]any{{"oldUri": pathToFileURI(fileB), "newUri": pathToFileURI(renamed)}}}); err != nil {
		t.Fatal(err)
	}
	if text := mustJSONText(t, client.request("workspace/symbol", map[string]any{"query": "RootBWorker"}).Result); !strings.Contains(text, pathToFileURI(renamed)) || strings.Contains(text, pathToFileURI(fileB)) {
		t.Fatalf("renamed workspace file index mismatch: %s", text)
	}
}

func TestStdioParityRenamesResolvedIncludesInUnopenedFilesForFileAndVirtualModes(t *testing.T) {
	client := startStdioTestClient(t)
	defer client.close()

	root := t.TempDir()
	pages := filepath.Join(root, "pages")
	shared := filepath.Join(root, "shared")
	if err := os.MkdirAll(pages, 0o755); err != nil {
		t.Fatal(err)
	}
	if err := os.MkdirAll(shared, 0o755); err != nil {
		t.Fatal(err)
	}
	target := filepath.Join(shared, "common.inc")
	newTarget := filepath.Join(shared, "nested", "renamed.inc")
	duplicate := filepath.Join(pages, "common.inc")
	owner := filepath.Join(pages, "default.asp")
	virtualOwner := filepath.Join(pages, "virtual.asp")
	writeIncludeLifecycleFixture(t, target, "<% Const SharedValue = 1 %>")
	writeIncludeLifecycleFixture(t, duplicate, "<% Const LocalValue = 1 %>")
	writeIncludeLifecycleFixture(t, owner, `<!-- #include file="../shared/common.inc" -->`)
	writeIncludeLifecycleFixture(t, virtualOwner, `<!-- #include virtual="/common.inc" -->`)
	initializeWithConfigurationAndWaitForWorkspaceIndex(t, client, map[string]any{
		"processId": nil, "rootUri": pathToFileURI(root), "capabilities": map[string]any{},
	}, map[string]any{"aspLsp": map[string]any{
		"virtualRoot": shared,
		"rename":      map[string]any{"updateIncludesOnFileRename": true},
	}})
	edit := client.request("workspace/willRenameFiles", map[string]any{"files": []map[string]any{{
		"oldUri": pathToFileURI(target), "newUri": pathToFileURI(newTarget),
	}}})
	text := mustJSONText(t, edit.Result)
	for _, expected := range []string{pathToFileURI(owner), pathToFileURI(virtualOwner), `../shared/nested/renamed.inc`, `/nested/renamed.inc`} {
		if !strings.Contains(text, expected) {
			t.Fatalf("resolved include rename edit missing %q: %s", expected, text)
		}
	}
	if strings.Contains(text, pathToFileURI(duplicate)) {
		t.Fatalf("duplicate basename was incorrectly edited: %s", text)
	}
}

func TestStdioParityConfiguresUnusedDiagnostics(t *testing.T) {
	client := startStdioTestClient(t)
	defer client.close()

	root := t.TempDir()
	uri := pathToFileURI(filepath.Join(root, "settings.asp"))
	source := `<%
Sub Demo(unusedArg)
  Dim unusedLocal
End Sub
%>
<script>function jsDemo(unusedJs) { const unusedJsLocal = 1; }</script>`
	client.request("initialize", map[string]any{"processId": nil, "rootUri": pathToFileURI(root), "capabilities": map[string]any{}})
	notifyConfiguration(t, client, map[string]any{"aspLsp": map[string]any{
		"vbscript":   map[string]any{"unusedDiagnostics": false},
		"javascript": map[string]any{"unusedDiagnostics": false},
	}})
	notifyOpenClassicASPDocument(t, client, uri, source)
	diagnostics := mustJSONText(t, client.request("textDocument/diagnostic", map[string]any{"textDocument": map[string]any{"uri": uri}}).Result)
	for _, unexpected := range []string{"asp-lsp-vbscript-unused", "asp-lsp-typescript-unused"} {
		if strings.Contains(diagnostics, unexpected) {
			t.Fatalf("disabled unused diagnostics still contain %q: %s", unexpected, diagnostics)
		}
	}
}

func TestStdioParityConfiguresImplicitGlobalDiagnostics(t *testing.T) {
	client := startStdioTestClient(t)
	defer client.close()

	root := t.TempDir()
	uri := pathToFileURI(filepath.Join(root, "implicit-global.asp"))
	if err := os.WriteFile(filepath.Join(root, "shared.inc"), []byte("<% Dim includedValue %>"), 0o644); err != nil {
		t.Fatal(err)
	}
	source := `<!-- #include file="shared.inc" -->
<%
Function Build()
  assignedValue = 1
  Set objectValue = CreateObject("Scripting.Dictionary")
  includedValue = 2
  Response.Write localReadOnly
End Function
%>`
	client.request("initialize", map[string]any{
		"processId": nil, "rootUri": pathToFileURI(root), "locale": "ja-JP", "capabilities": map[string]any{},
	})
	notifyOpenClassicASPDocument(t, client, uri, source)
	disabled := mustJSONText(t, client.request("textDocument/diagnostic", map[string]any{
		"textDocument": map[string]any{"uri": uri},
	}).Result)
	if strings.Contains(disabled, "asp-lsp-vbscript-implicit-global") {
		t.Fatalf("disabled implicit global diagnostics were reported: %s", disabled)
	}

	notifyConfiguration(t, client, map[string]any{"aspLsp": map[string]any{
		"locale": "auto", "vbscript": map[string]any{"implicitGlobalDiagnostics": true},
	}})
	enabled := mustJSONText(t, client.request("textDocument/diagnostic", map[string]any{
		"textDocument": map[string]any{"uri": uri},
	}).Result)
	for _, expected := range []string{"asp-lsp-vbscript-implicit-global", "assignedValue", "objectValue", "暗黙の global 変数"} {
		if !strings.Contains(enabled, expected) {
			t.Fatalf("enabled implicit global diagnostics missing %q: %s", expected, enabled)
		}
	}
	for _, unexpected := range []string{"localReadOnly", "includedValue"} {
		if strings.Contains(enabled, unexpected) {
			t.Fatalf("non-global implicit %q was reported: %s", unexpected, enabled)
		}
	}
}

func TestStdioParityConfiguresVBScriptSyntaxKeywords(t *testing.T) {
	client := startStdioTestClient(t)
	defer client.close()

	root := t.TempDir()
	uri := pathToFileURI(filepath.Join(root, "keywords.asp"))
	source := "<%\nIf value Then\n  Response.Write value\nEn\n%>"
	position := positionAt(source, strings.Index(source, "En\n")+2)
	client.request("initialize", map[string]any{"processId": nil, "rootUri": pathToFileURI(root), "capabilities": map[string]any{}})
	notifyOpenClassicASPDocument(t, client, uri, source)
	enabled := mustJSONText(t, client.request("textDocument/completion", map[string]any{
		"textDocument": map[string]any{"uri": uri}, "position": position,
	}).Result)
	if !strings.Contains(enabled, `"label":"End If"`) || !strings.Contains(enabled, "VBScript syntax") {
		t.Fatalf("context-aware syntax keyword completion missing: %s", enabled)
	}
	notifyConfiguration(t, client, map[string]any{"aspLsp": map[string]any{"vbscript": map[string]any{"syntaxKeywords": false}}})
	disabled := mustJSONText(t, client.request("textDocument/completion", map[string]any{
		"textDocument": map[string]any{"uri": uri}, "position": position,
	}).Result)
	if strings.Contains(disabled, "VBScript syntax") {
		t.Fatalf("disabled syntax keyword completion still returned contextual items: %s", disabled)
	}
}
