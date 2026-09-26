package lspserver

import (
	"os"
	"path/filepath"
	"regexp"
	"strings"
	"testing"

	"github.com/yottonoko/classic-web-system-lsp/internal/lsp"
)

func TestStdioParityReturnsVBScriptNamingHintsAndReferenceRenameQuickFixes(t *testing.T) {
	client := startStdioTestClient(t)
	defer client.close()

	root := t.TempDir()
	uri := pathToFileURI(filepath.Join(root, "go-vb-naming.asp"))
	source := `<%
Dim foo
foo = 1
Response.Write FOO
%>`
	client.request("initialize", map[string]any{
		"processId":    nil,
		"rootUri":      pathToFileURI(root),
		"capabilities": map[string]any{},
	})
	notifyConfiguration(t, client, map[string]any{"aspLsp": map[string]any{
		"rename":   map[string]any{"workspaceSymbolRename": true},
		"vbscript": map[string]any{"identifierCase": "PascalCase"},
	}})
	diagnostics := openClassicASPDocumentNamingDiagnostics(t, client, uri, source)
	if serialized := mustJSONText(t, diagnostics); !strings.Contains(serialized, "Foo") {
		t.Fatalf("naming diagnostics missing Foo: %s", serialized)
	}

	actions := requestNamingActions(t, client, uri, lsp.Range{
		Start: lsp.Position{Line: 1, Character: 4},
		End:   lsp.Position{Line: 1, Character: 7},
	}, diagnostics)
	serialized := mustJSONText(t, actions.Result)
	if !strings.Contains(serialized, "Rename foo to Foo") {
		t.Fatalf("naming quick fix missing title: %s", serialized)
	}
	if countJSONNewText(serialized, "Foo") != 3 {
		t.Fatalf("naming quick fix newText count = %d, want 3: %s", countJSONNewText(serialized, "Foo"), serialized)
	}
}

func TestStdioParityReturnsVBScriptNamingQuickFixesForConfiguredCasingStyles(t *testing.T) {
	for _, testCase := range []struct {
		identifierCase string
		sourceName     string
		referenceName  string
		expectedName   string
	}{
		{identifierCase: "UPPERCASE", sourceName: "user_name", referenceName: "USER_NAME", expectedName: "USERNAME"},
		{identifierCase: "camelCase", sourceName: "user_name", referenceName: "USER_NAME", expectedName: "userName"},
		{identifierCase: "lowercase", sourceName: "user_name", referenceName: "USER_NAME", expectedName: "username"},
		{identifierCase: "snake_case", sourceName: "userName", referenceName: "userName", expectedName: "user_name"},
		{identifierCase: "UPPER_SNAKE", sourceName: "user_name", referenceName: "USER_NAME", expectedName: "USER_NAME"},
	} {
		t.Run(testCase.identifierCase, func(t *testing.T) {
			client := startStdioTestClient(t)
			defer client.close()

			root := t.TempDir()
			uri := pathToFileURI(filepath.Join(root, "go-vb-naming-"+testCase.identifierCase+".asp"))
			source := `<%
Dim ` + testCase.sourceName + `
Response.Write ` + testCase.referenceName + `
%>`
			client.request("initialize", map[string]any{
				"processId":    nil,
				"rootUri":      pathToFileURI(root),
				"capabilities": map[string]any{},
			})
			notifyConfiguration(t, client, map[string]any{"aspLsp": map[string]any{
				"vbscript": map[string]any{"identifierCase": testCase.identifierCase},
			}})
			diagnostics := openClassicASPDocumentNamingDiagnostics(t, client, uri, source)
			if serialized := mustJSONText(t, diagnostics); !strings.Contains(serialized, testCase.expectedName) {
				t.Fatalf("naming diagnostics missing %s: %s", testCase.expectedName, serialized)
			}
			actions := requestNamingActions(t, client, uri, lsp.Range{
				Start: lsp.Position{Line: 1, Character: 4},
				End:   lsp.Position{Line: 1, Character: 13},
			}, diagnostics)
			serialized := mustJSONText(t, actions.Result)
			expectedTitle := "Rename " + testCase.sourceName + " to " + testCase.expectedName
			if !strings.Contains(serialized, expectedTitle) {
				t.Fatalf("naming quick fix missing %q: %s", expectedTitle, serialized)
			}
			if countJSONNewText(serialized, testCase.expectedName) != 2 {
				t.Fatalf("naming quick fix %s newText count = %d, want 2: %s", testCase.expectedName, countJSONNewText(serialized, testCase.expectedName), serialized)
			}
		})
	}
}

func TestStdioParityAcceptsLegacyVBScriptIdentifierCasingSettingAliases(t *testing.T) {
	client := startStdioTestClient(t)
	defer client.close()

	root := t.TempDir()
	uri := pathToFileURI(filepath.Join(root, "go-vb-naming-legacy-alias.asp"))
	source := `<%
Dim user_name
Response.Write USER_NAME
%>`
	client.request("initialize", map[string]any{
		"processId":    nil,
		"rootUri":      pathToFileURI(root),
		"capabilities": map[string]any{},
	})
	notifyConfiguration(t, client, map[string]any{"aspLsp": map[string]any{
		"vbscript": map[string]any{
			"identifierCase":       "camel",
			"identifierCaseByKind": map[string]any{"variable": "upperSnake"},
		},
	}})
	diagnostics := openClassicASPDocumentNamingDiagnostics(t, client, uri, source)
	if serialized := mustJSONText(t, diagnostics); !strings.Contains(serialized, "USER_NAME") {
		t.Fatalf("legacy casing aliases diagnostics missing USER_NAME: %s", serialized)
	}
}

func TestStdioParityUsesVBScriptIdentifierCasingSettingsPerDeclarationKind(t *testing.T) {
	client := startStdioTestClient(t)
	defer client.close()

	root := t.TempDir()
	uri := pathToFileURI(filepath.Join(root, "go-vb-naming-by-kind.asp"))
	source := `<%
Dim selected_customer
Class customer_record
End Class
%>`
	client.request("initialize", map[string]any{
		"processId":    nil,
		"rootUri":      pathToFileURI(root),
		"capabilities": map[string]any{},
	})
	notifyConfiguration(t, client, map[string]any{"aspLsp": map[string]any{
		"vbscript": map[string]any{
			"identifierCaseByKind": map[string]any{"variable": "snake_case", "class": "PascalCase"},
		},
	}})
	diagnostics := openClassicASPDocumentNamingDiagnostics(t, client, uri, source)
	serialized := mustJSONText(t, diagnostics)
	if strings.Contains(serialized, "selected_customer") {
		t.Fatalf("by-kind diagnostics should not flag selected_customer: %s", serialized)
	}
	if !strings.Contains(serialized, "CustomerRecord") {
		t.Fatalf("by-kind diagnostics missing CustomerRecord: %s", serialized)
	}
}

func TestStdioParityDoesNotReportVBScriptNamingHintsWhenIdentifierCasingIsIgnored(t *testing.T) {
	client := startStdioTestClient(t)
	defer client.close()

	root := t.TempDir()
	uri := pathToFileURI(filepath.Join(root, "go-vb-naming-ignore.asp"))
	client.request("initialize", map[string]any{
		"processId":    nil,
		"rootUri":      pathToFileURI(root),
		"capabilities": map[string]any{},
	})
	notifyConfiguration(t, client, map[string]any{"aspLsp": map[string]any{
		"vbscript": map[string]any{"identifierCase": "ignore"},
	}})
	diagnostics := openClassicASPDocumentWithDiagnostics(t, client, uri, "<%\nDim foo\n%>")
	for _, diagnostic := range diagnosticsFromPublishMessage(t, diagnostics) {
		if diagnostic.Source == "asp-lsp-vbscript-naming" {
			t.Fatalf("ignore casing should not report naming diagnostic: %#v", diagnostic)
		}
	}
}

func TestStdioParityReturnsVBScriptNamingQuickFixesForDeclarationKinds(t *testing.T) {
	client := startStdioTestClient(t)
	defer client.close()

	root := t.TempDir()
	uri := pathToFileURI(filepath.Join(root, "go-vb-naming-declaration-kinds.asp"))
	source := `<%
Class customer_record
  Public customer_name
  Public Property Get display_name()
    display_name = 1
  End Property
End Class
Sub save_order(item_name)
  Response.Write item_name
End Sub
Function build_total()
  build_total = 1
End Function
Dim record
Set record = New customer_record
save_order "x"
Response.Write build_total()
Response.Write record.display_name
%>`
	client.request("initialize", map[string]any{
		"processId":    nil,
		"rootUri":      pathToFileURI(root),
		"capabilities": map[string]any{},
	})
	notifyConfiguration(t, client, map[string]any{"aspLsp": map[string]any{
		"rename":   map[string]any{"workspaceSymbolRename": true},
		"vbscript": map[string]any{"identifierCase": "PascalCase"},
	}})
	diagnostics := openClassicASPDocumentNamingDiagnostics(t, client, uri, source)
	for _, testCase := range []struct {
		name         string
		expectedName string
	}{
		{name: "customer_record", expectedName: "CustomerRecord"},
		{name: "customer_name", expectedName: "CustomerName"},
		{name: "display_name", expectedName: "DisplayName"},
		{name: "save_order", expectedName: "SaveOrder"},
		{name: "item_name", expectedName: "ItemName"},
		{name: "build_total", expectedName: "BuildTotal"},
	} {
		diagnostic := namingDiagnosticByName(t, diagnostics, testCase.name)
		if serialized := mustJSONText(t, diagnostic); !strings.Contains(serialized, testCase.expectedName) {
			t.Fatalf("%s diagnostic missing %s: %s", testCase.name, testCase.expectedName, serialized)
		}
		actions := requestNamingActions(t, client, uri, diagnostic.Range, []lsp.Diagnostic{diagnostic})
		if serialized := mustJSONText(t, actions.Result); !strings.Contains(serialized, "Rename "+testCase.name+" to "+testCase.expectedName) {
			t.Fatalf("%s naming quick fix missing title: %s", testCase.name, serialized)
		}
	}
}

func TestStdioParityRenamesIncludedVBScriptReferencesFromNamingQuickFixes(t *testing.T) {
	client := startStdioTestClient(t)
	defer client.close()

	root := t.TempDir()
	owner := filepath.Join(root, "default.asp")
	include := filepath.Join(root, "common.inc")
	if err := os.WriteFile(include, []byte("<%\nResponse.Write FOO\n%>"), 0o644); err != nil {
		t.Fatal(err)
	}
	source := `<!-- #include file="common.inc" -->
<%
Dim foo
%>`
	if err := os.WriteFile(owner, []byte(source), 0o644); err != nil {
		t.Fatal(err)
	}
	uri := pathToFileURI(owner)
	initializeWithConfigurationAndWaitForWorkspaceIndex(t, client, map[string]any{
		"processId":    nil,
		"rootUri":      pathToFileURI(root),
		"capabilities": map[string]any{},
	}, map[string]any{"aspLsp": map[string]any{
		"rename":   map[string]any{"workspaceSymbolRename": true},
		"vbscript": map[string]any{"identifierCase": "PascalCase"},
	}})
	diagnostics := openClassicASPDocumentNamingDiagnostics(t, client, uri, source)
	actions := requestNamingActions(t, client, uri, lsp.Range{
		Start: lsp.Position{Line: 2, Character: 4},
		End:   lsp.Position{Line: 2, Character: 7},
	}, diagnostics)
	serialized := mustJSONText(t, actions.Result)
	for _, expected := range []string{"Rename foo to Foo", "common.inc"} {
		if !strings.Contains(serialized, expected) {
			t.Fatalf("include naming quick fix missing %q: %s", expected, serialized)
		}
	}
	if countJSONNewText(serialized, "Foo") != 2 {
		t.Fatalf("include naming quick fix newText count = %d, want 2: %s", countJSONNewText(serialized, "Foo"), serialized)
	}
}

func TestStdioParityDoesNotReturnVBScriptNamingQuickFixesWhenExpectedNameCollides(t *testing.T) {
	client := startStdioTestClient(t)
	defer client.close()

	root := t.TempDir()
	uri := pathToFileURI(filepath.Join(root, "go-vb-naming-collision.asp"))
	source := `<%
Dim foo
Dim Foo
Response.Write foo
%>`
	client.request("initialize", map[string]any{
		"processId":    nil,
		"rootUri":      pathToFileURI(root),
		"capabilities": map[string]any{},
	})
	notifyConfiguration(t, client, map[string]any{"aspLsp": map[string]any{
		"vbscript": map[string]any{"identifierCase": "PascalCase"},
	}})
	diagnostics := openClassicASPDocumentNamingDiagnostics(t, client, uri, source)
	if serialized := mustJSONText(t, diagnostics); !strings.Contains(serialized, "Foo") {
		t.Fatalf("collision naming diagnostics missing Foo: %s", serialized)
	}
	actions := requestNamingActions(t, client, uri, lsp.Range{
		Start: lsp.Position{Line: 1, Character: 4},
		End:   lsp.Position{Line: 1, Character: 7},
	}, diagnostics)
	if serialized := mustJSONText(t, actions.Result); strings.Contains(serialized, "Rename foo to Foo") {
		t.Fatalf("collision naming quick fix should not be returned: %s", serialized)
	}
}

func TestStdioParityRenamesVBScriptClassMemberReferencesFromNamingQuickFixes(t *testing.T) {
	client := startStdioTestClient(t)
	defer client.close()

	root := t.TempDir()
	uri := pathToFileURI(filepath.Join(root, "go-vb-naming-member-references.asp"))
	source := `<%
Class customer_record
  Public customer_name
  Public Property Get display_name()
    display_name = customer_name
  End Property
  Public Sub show_name()
    Response.Write Me.display_name
    Response.Write Me.customer_name
  End Sub
End Class
Dim record
Set record = New customer_record
Response.Write record.display_name
Response.Write record.customer_name
%>`
	client.request("initialize", map[string]any{
		"processId":    nil,
		"rootUri":      pathToFileURI(root),
		"capabilities": map[string]any{},
	})
	notifyConfiguration(t, client, map[string]any{"aspLsp": map[string]any{
		"vbscript": map[string]any{"identifierCase": "PascalCase"},
	}})
	diagnostics := openClassicASPDocumentNamingDiagnostics(t, client, uri, source)
	propertyDiagnostic := namingDiagnosticByName(t, diagnostics, "display_name")
	propertyActions := requestNamingActions(t, client, uri, propertyDiagnostic.Range, []lsp.Diagnostic{propertyDiagnostic})
	if serialized := mustJSONText(t, propertyActions.Result); countJSONNewText(serialized, "DisplayName") != 4 {
		t.Fatalf("property naming quick fix DisplayName count mismatch: %s", serialized)
	}

	fieldDiagnostic := namingDiagnosticByName(t, diagnostics, "customer_name")
	fieldActions := requestNamingActions(t, client, uri, fieldDiagnostic.Range, []lsp.Diagnostic{fieldDiagnostic})
	fieldSerialized := mustJSONText(t, fieldActions.Result)
	if !strings.Contains(fieldSerialized, "Rename customer_name to CustomerName") || countJSONNewText(fieldSerialized, "CustomerName") != 3 {
		t.Fatalf("field naming quick fix mismatch: %s", fieldSerialized)
	}
}

func TestStdioParityAllowsVBScriptNamingQuickFixesWhenSameCaseNamesAreInDifferentScopes(t *testing.T) {
	client := startStdioTestClient(t)
	defer client.close()

	root := t.TempDir()
	uri := pathToFileURI(filepath.Join(root, "go-vb-naming-scope-collision.asp"))
	source := `<%
Sub First()
  Dim foo
  Response.Write foo
End Sub
Sub Second()
  Dim Foo
  Response.Write Foo
End Sub
%>`
	client.request("initialize", map[string]any{
		"processId":    nil,
		"rootUri":      pathToFileURI(root),
		"capabilities": map[string]any{},
	})
	notifyConfiguration(t, client, map[string]any{"aspLsp": map[string]any{
		"vbscript": map[string]any{"identifierCase": "PascalCase"},
	}})
	diagnostics := openClassicASPDocumentNamingDiagnostics(t, client, uri, source)
	diagnostic := namingDiagnosticByName(t, diagnostics, "foo")
	actions := requestNamingActions(t, client, uri, diagnostic.Range, []lsp.Diagnostic{diagnostic})
	serialized := mustJSONText(t, actions.Result)
	if !strings.Contains(serialized, "Rename foo to Foo") || countJSONNewText(serialized, "Foo") != 2 {
		t.Fatalf("scoped naming quick fix mismatch: %s", serialized)
	}
}

func TestStdioParityDoesNotRenameVBScriptIdentifierTextInStringsOrComments(t *testing.T) {
	client := startStdioTestClient(t)
	defer client.close()

	root := t.TempDir()
	uri := pathToFileURI(filepath.Join(root, "go-vb-naming-string-comment.asp"))
	source := `<%
Dim foo
' foo should stay in this comment
Response.Write "foo should stay in this string"
Response.Write foo
%>`
	client.request("initialize", map[string]any{
		"processId":    nil,
		"rootUri":      pathToFileURI(root),
		"capabilities": map[string]any{},
	})
	notifyConfiguration(t, client, map[string]any{"aspLsp": map[string]any{
		"vbscript": map[string]any{"identifierCase": "PascalCase"},
	}})
	diagnostics := openClassicASPDocumentNamingDiagnostics(t, client, uri, source)
	diagnostic := namingDiagnosticByName(t, diagnostics, "foo")
	actions := requestNamingActions(t, client, uri, diagnostic.Range, []lsp.Diagnostic{diagnostic})
	serialized := mustJSONText(t, actions.Result)
	if !strings.Contains(serialized, "Rename foo to Foo") || countJSONNewText(serialized, "Foo") != 2 {
		t.Fatalf("string/comment naming quick fix mismatch: %s", serialized)
	}
}

func openClassicASPDocumentNamingDiagnostics(t *testing.T, client *stdioTestClient, uri string, source string) []lsp.Diagnostic {
	t.Helper()
	message := openClassicASPDocumentWithDiagnostics(t, client, uri, source)
	var result []lsp.Diagnostic
	for _, diagnostic := range diagnosticsFromPublishMessage(t, message) {
		if diagnostic.Source == "asp-lsp-vbscript-naming" {
			result = append(result, diagnostic)
		}
	}
	return result
}

func namingDiagnosticByName(t *testing.T, diagnostics []lsp.Diagnostic, name string) lsp.Diagnostic {
	t.Helper()
	needle := `"name":"` + name + `"`
	for _, diagnostic := range diagnostics {
		if strings.Contains(mustJSONText(t, diagnostic.Data), needle) {
			return diagnostic
		}
	}
	t.Fatalf("naming diagnostic for %s missing: %s", name, mustJSONText(t, diagnostics))
	return lsp.Diagnostic{}
}

func requestNamingActions(t *testing.T, client *stdioTestClient, uri string, actionRange lsp.Range, diagnostics []lsp.Diagnostic) *rpcMessage {
	t.Helper()
	return client.request("textDocument/codeAction", map[string]any{
		"textDocument": map[string]any{"uri": uri},
		"range":        actionRange,
		"context": map[string]any{
			"diagnostics": diagnostics,
			"only":        []string{"quickfix"},
		},
	})
}

func countJSONNewText(serialized string, value string) int {
	return len(regexp.MustCompile(`"newText":"`+regexp.QuoteMeta(value)+`"`).FindAllStringIndex(serialized, -1))
}
