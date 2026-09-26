package lspserver

import (
	"os"
	"path/filepath"
	"strings"
	"testing"
)

func TestStdioParityResolvesVirtualIncludesFromConfiguredRoots(t *testing.T) {
	client := startStdioTestClient(t)
	defer client.close()

	root := t.TempDir()
	pageDir := filepath.Join(root, "pages")
	sharedDir := filepath.Join(root, "shared")
	if err := os.MkdirAll(pageDir, 0o755); err != nil {
		t.Fatal(err)
	}
	if err := os.MkdirAll(sharedDir, 0o755); err != nil {
		t.Fatal(err)
	}
	owner := filepath.Join(pageDir, "default.asp")
	include := filepath.Join(sharedDir, "common.inc")
	if err := os.WriteFile(include, []byte("<%\nFunction SharedTitle()\nEnd Function\n%>"), 0o644); err != nil {
		t.Fatal(err)
	}
	marked := markedDocument(`<!-- #include virtual="/shared/common.inc" -->
<%
Response.Write Shared<<<caret>>>Title()
%>`)
	if err := os.WriteFile(owner, []byte(marked.Text), 0o644); err != nil {
		t.Fatal(err)
	}
	uri := pathToFileURI(owner)
	client.request("initialize", map[string]any{
		"processId":    nil,
		"rootUri":      pathToFileURI(pageDir),
		"capabilities": map[string]any{},
	})
	notifyConfiguration(t, client, map[string]any{"aspLsp": map[string]any{"virtualRoots": []string{root}}})
	openClassicASPDocumentWithDiagnostics(t, client, uri, marked.Text)

	definition := mustJSONText(t, client.request("textDocument/definition", map[string]any{
		"textDocument": map[string]any{"uri": uri},
		"position":     marked.Position,
	}).Result)
	if !strings.Contains(definition, "common.inc") {
		t.Fatalf("virtual include definition missing common.inc: %s", definition)
	}
	if !completionLabels(client.request("textDocument/completion", map[string]any{
		"textDocument": map[string]any{"uri": uri},
		"position":     marked.Position,
	}).Result).contains("SharedTitle") {
		t.Fatalf("virtual include completion missing SharedTitle")
	}
}

func TestStdioParityResolvesFileAndVirtualIncludeChainsWithJavaScriptDiagnostics(t *testing.T) {
	client := startStdioTestClient(t)
	defer client.close()

	root := t.TempDir()
	pageDir := filepath.Join(root, "pages")
	includeDir := filepath.Join(root, "includes")
	sharedDir := filepath.Join(root, "shared")
	for _, dir := range []string{pageDir, includeDir, sharedDir} {
		if err := os.MkdirAll(dir, 0o755); err != nil {
			t.Fatal(err)
		}
	}
	writeTextFile(t, filepath.Join(includeDir, "first.inc"), `<!-- #include file="second.inc" -->
<%
Function FromFileChain()
  FromFileChain = "file"
End Function
%>`)
	writeTextFile(t, filepath.Join(includeDir, "second.inc"), `<!-- #include file="third.inc" -->
<%
Const ShadowedThing = "global"
%>`)
	writeTextFile(t, filepath.Join(includeDir, "third.inc"), `<%
Function FromDeepChain()
  FromDeepChain = "deep"
End Function

Function UnusedIncludeUtility()
  Dim unusedIncludeLocal
  UnusedIncludeUtility = "unused"
End Function
%>`)
	writeTextFile(t, filepath.Join(sharedDir, "root.inc"), `<!-- #include file="leaf.inc" -->
<%
Function FromVirtualRoot()
  FromVirtualRoot = "root"
End Function
%>`)
	writeTextFile(t, filepath.Join(sharedDir, "leaf.inc"), `<%
Function FromVirtualLeaf()
  FromVirtualLeaf = "leaf"
End Function
%>`)
	marked := markedDocument(`<!-- #include file="../includes/first.inc" -->
<!-- #include virtual="/shared/root.inc" -->
<%
Dim implicitPageValue
implicitPageValue = FromDeepChain()
Sub LocalOddity()
  Dim ShadowedThing
  ShadowedThing = "local"
End Sub
Response.Write From<<<caret>>>VirtualLeaf()
%>
<script>
const fileValue = <%= FromFileChain() %>;
const virtualValue = <% Response.Write FromVirtualRoot() %>;
missingIncludeJs.toFixed();
</script>`)
	owner := filepath.Join(pageDir, "default.asp")
	writeTextFile(t, owner, marked.Text)
	uri := pathToFileURI(owner)
	client.request("initialize", map[string]any{
		"processId":    nil,
		"rootUri":      pathToFileURI(pageDir),
		"capabilities": map[string]any{},
	})
	notifyConfiguration(t, client, map[string]any{"aspLsp": map[string]any{
		"checkJs":      true,
		"virtualRoots": []string{root},
	}})

	diagnosticsMessage := openClassicASPDocumentWithDiagnostics(t, client, uri, marked.Text)
	diagnosticPayload := mustJSONText(t, diagnosticsMessage.Params)
	for _, want := range []string{"asp-lsp-typescript", "missingIncludeJs"} {
		if !strings.Contains(diagnosticPayload, want) {
			t.Fatalf("diagnostics missing %q: %s", want, diagnosticPayload)
		}
	}
	for _, unexpected := range []string{"FromDeepChain", "FromVirtualLeaf"} {
		if strings.Contains(diagnosticPayload, unexpected) {
			t.Fatalf("diagnostics unexpectedly contained %q: %s", unexpected, diagnosticPayload)
		}
	}
	expectDiagnosticsOutsideAspIslands(t, marked.Text, diagnosticsFromSourceLSP(t, diagnosticsMessage, "asp-lsp-typescript"), []string{
		"<%= FromFileChain",
		"<% Response.Write FromVirtualRoot",
	})
	labels := completionLabels(client.request("textDocument/completion", map[string]any{
		"textDocument": map[string]any{"uri": uri},
		"position":     marked.Position,
	}).Result)
	for _, label := range []string{"FromFileChain", "FromVirtualLeaf", "FromVirtualRoot"} {
		if !labels.contains(label) {
			t.Fatalf("include chain completion missing %s: %v", label, labels)
		}
	}
}

func TestStdioParityAutoDetectsShiftJISIncludeDirectivesInUnopenedIncludeFiles(t *testing.T) {
	client := startStdioTestClient(t)
	defer client.close()

	root := t.TempDir()
	owner := filepath.Join(root, "default.asp")
	first := filepath.Join(root, "first.inc")
	next := filepath.Join(root, "次.inc")
	marked := markedDocument(`<!-- #include file="first.inc" -->
<%
Response.Write Shared<<<caret>>>Thing()
%>`)
	writeTextFile(t, owner, marked.Text)
	writeShiftJISNextInclude(t, first)
	writeTextFile(t, next, "<%\nFunction SharedThing()\nEnd Function\n%>")
	uri := pathToFileURI(owner)
	client.request("initialize", map[string]any{
		"processId":    nil,
		"rootUri":      pathToFileURI(root),
		"capabilities": map[string]any{},
	})
	notifyConfiguration(t, client, map[string]any{"aspLsp": map[string]any{"legacyEncoding": "auto"}})
	openClassicASPDocumentWithDiagnostics(t, client, uri, marked.Text)

	if !completionLabels(client.request("textDocument/completion", map[string]any{
		"textDocument": map[string]any{"uri": uri},
		"position":     marked.Position,
	}).Result).contains("SharedThing") {
		t.Fatalf("auto Shift_JIS include completion missing SharedThing")
	}
}

func TestStdioParityKeepsExplicitUTF8IncludeDecodingDeterministic(t *testing.T) {
	client := startStdioTestClient(t)
	defer client.close()

	root := t.TempDir()
	owner := filepath.Join(root, "default.asp")
	first := filepath.Join(root, "first.inc")
	next := filepath.Join(root, "次.inc")
	marked := markedDocument(`<!-- #include file="first.inc" -->
<%
Response.Write Shared<<<caret>>>Thing()
%>`)
	writeTextFile(t, owner, marked.Text)
	writeShiftJISNextInclude(t, first)
	writeTextFile(t, next, "<%\nFunction SharedThing()\nEnd Function\n%>")
	uri := pathToFileURI(owner)
	client.request("initialize", map[string]any{
		"processId":    nil,
		"rootUri":      pathToFileURI(root),
		"capabilities": map[string]any{},
	})
	notifyConfiguration(t, client, map[string]any{"aspLsp": map[string]any{"legacyEncoding": "utf8"}})
	openClassicASPDocumentWithDiagnostics(t, client, uri, marked.Text)

	if completionLabels(client.request("textDocument/completion", map[string]any{
		"textDocument": map[string]any{"uri": uri},
		"position":     marked.Position,
	}).Result).contains("SharedThing") {
		t.Fatalf("utf8 include decoding unexpectedly resolved Shift_JIS include")
	}
}

func TestStdioParityAutoDetectsShiftJISUnopenedWorkspaceFilesForWorkspaceSymbols(t *testing.T) {
	client := startStdioTestClient(t)
	defer client.close()

	root := t.TempDir()
	if err := os.WriteFile(filepath.Join(root, "unopened.asp"), []byte{
		'<', 'd', 'i', 'v', ' ', 'i', 'd', '=', '"',
		0x93, 0xfa, 0x96, 0x7b, 0x8c, 0xea,
		'"', '>', '<', '/', 'd', 'i', 'v', '>',
	}, 0o644); err != nil {
		t.Fatal(err)
	}
	initializeWithConfigurationAndWaitForWorkspaceIndex(t, client, map[string]any{
		"processId":    nil,
		"rootUri":      pathToFileURI(root),
		"capabilities": map[string]any{},
	}, map[string]any{"aspLsp": map[string]any{"legacyEncoding": "auto"}})

	symbols := mustJSONText(t, client.request("workspace/symbol", map[string]any{"query": "日本語"}).Result)
	if !strings.Contains(symbols, "日本語") || !strings.Contains(symbols, "unopened.asp") {
		t.Fatalf("Shift_JIS workspace symbols missing 日本語 id: %s", symbols)
	}
}

func writeTextFile(t *testing.T, path string, content string) {
	t.Helper()
	if err := os.WriteFile(path, []byte(content), 0o644); err != nil {
		t.Fatal(err)
	}
}

func writeShiftJISNextInclude(t *testing.T, path string) {
	t.Helper()
	content := []byte(`<!-- #include file="`)
	content = append(content, 0x8e, 0x9f)
	content = append(content, []byte(`.inc" -->`)...)
	if err := os.WriteFile(path, content, 0o644); err != nil {
		t.Fatal(err)
	}
}
