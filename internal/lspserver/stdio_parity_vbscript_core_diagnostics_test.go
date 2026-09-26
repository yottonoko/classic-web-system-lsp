package lspserver

import (
	"os"
	"path/filepath"
	"strings"
	"testing"
)

func TestStdioParityDoesNotTreatStringsOrCommentsAsUndeclaredVariables(t *testing.T) {
	client := startStdioTestClient(t)
	defer client.close()

	root := t.TempDir()
	uri := pathToFileURI(filepath.Join(root, "strings-comments-undeclared.asp"))
	source := `<%
Option Explicit
' comment words should be ignored
Rem more comment words should be ignored
Response.Write "hello world"
%>`
	client.request("initialize", map[string]any{
		"processId":    nil,
		"rootUri":      pathToFileURI(root),
		"capabilities": map[string]any{},
	})
	diagnostics := openClassicASPDocumentWithDiagnostics(t, client, uri, source)
	if serialized := mustJSONText(t, diagnosticsFromSource(t, diagnostics, "asp-lsp-vbscript")); serialized != "[]" && serialized != "null" {
		t.Fatalf("strings/comments produced undeclared diagnostics: %s", serialized)
	}
}

func TestStdioParityReportsScopedUnusedDeclarationsWithoutFlaggingGlobals(t *testing.T) {
	client := startStdioTestClient(t)
	defer client.close()

	root := t.TempDir()
	uri := pathToFileURI(filepath.Join(root, "scoped-unused.asp"))
	source := `<%
Dim globalValue
Const GlobalConst = 1
Sub GlobalSave()
End Sub
Class GlobalLonely
End Class
Sub Save(usedArg, unusedArg)
  Dim unusedValue
  Const unusedConst = 1
  Response.Write usedArg
End Sub
%>`
	client.request("initialize", map[string]any{
		"processId":    nil,
		"rootUri":      pathToFileURI(root),
		"capabilities": map[string]any{},
	})
	diagnostics := openClassicASPDocumentWithDiagnostics(t, client, uri, source)
	unused := diagnosticsFromSource(t, diagnostics, "asp-lsp-vbscript-unused")
	unusedText := mustJSONText(t, unused)
	for _, expected := range []string{"unusedValue", "unusedConst", "unusedArg"} {
		if !strings.Contains(unusedText, expected) {
			t.Fatalf("unused diagnostics missing %q: %s", expected, unusedText)
		}
	}
	for _, unexpected := range []string{"globalValue", "GlobalConst", "GlobalSave", "GlobalLonely"} {
		if strings.Contains(unusedText, unexpected) {
			t.Fatalf("unused diagnostics flagged global %q: %s", unexpected, unusedText)
		}
	}
	for _, diagnostic := range diagnosticsFromPublishMessage(t, diagnostics) {
		if diagnostic.Source == "asp-lsp-vbscript-unused" && !diagnosticHasTag(diagnostic, 1) {
			t.Fatalf("unused diagnostic missing unnecessary tag: %#v", diagnostic)
		}
	}
}

func TestStdioParityKeepsIncludeRefsGlobalAsaAndTypedPrivateMembersOutOfUnusedDiagnostics(t *testing.T) {
	client := startStdioTestClient(t)
	defer client.close()

	root := t.TempDir()
	includePath := filepath.Join(root, "shared.inc")
	if err := os.WriteFile(includePath, []byte(`<%
Function SharedName()
End Function
%>`), 0o644); err != nil {
		t.Fatal(err)
	}
	pagePath := filepath.Join(root, "default.asp")
	pageSource := `<!-- #include file="shared.inc" -->
<%
Response.Write SharedName()
%>`
	if err := os.WriteFile(pagePath, []byte(pageSource), 0o644); err != nil {
		t.Fatal(err)
	}
	globalURI := pathToFileURI(filepath.Join(root, "Global.asa"))
	globalSource := `<script runat="server" language="VBScript">
Sub Application_OnStart()
End Sub
</script>`
	memberURI := pathToFileURI(filepath.Join(root, "member-reference.asp"))
	memberSource := `<%
Class Customer
  Private Sub Save()
  End Sub
End Class

Dim customer
Set customer = New Customer
customer.Save
%>`
	client.request("initialize", map[string]any{
		"processId":    nil,
		"rootUri":      pathToFileURI(root),
		"capabilities": map[string]any{},
	})
	openClassicASPDocument(t, client, pathToFileURI(pagePath), pageSource)

	includeDiagnostics := client.request("textDocument/diagnostic", map[string]any{
		"textDocument": map[string]any{"uri": pathToFileURI(includePath)},
	})
	if serialized := mustJSONText(t, includeDiagnostics.Result); strings.Contains(serialized, "SharedName") {
		t.Fatalf("include reference produced unused SharedName diagnostic: %s", serialized)
	}

	globalDiagnostics := openClassicASPDocumentWithDiagnostics(t, client, globalURI, globalSource)
	if serialized := mustJSONText(t, diagnosticsFromSource(t, globalDiagnostics, "asp-lsp-vbscript-unused")); serialized != "[]" && serialized != "null" {
		t.Fatalf("Global.asa event handler produced unused diagnostic: %s", serialized)
	}

	memberDiagnostics := openClassicASPDocumentWithDiagnostics(t, client, memberURI, memberSource)
	if serialized := mustJSONText(t, diagnosticsFromSource(t, memberDiagnostics, "asp-lsp-vbscript-unused")); strings.Contains(serialized, "Save") {
		t.Fatalf("typed private member reference produced unused diagnostic: %s", serialized)
	}
}
