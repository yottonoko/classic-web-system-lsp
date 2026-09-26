package lspserver

import (
	"os"
	"path/filepath"
	"strings"
	"testing"

	"github.com/yottonoko/classic-web-system-lsp/internal/lsp"
)

func TestStdioParityPublishesStrictVBScriptTypeDiagnosticsAndCustomCOMCompletions(t *testing.T) {
	client := startStdioTestClient(t)
	defer client.close()

	root := t.TempDir()
	uri := "file:///tmp/go-vb-type.asp"
	source := `<%
Dim widget
Set widget = Server.CreateObject("Custom.Widget")
widget.Title = 1
widget.
widget.Missing
widget.Ping("a", "b")
Repository.
Dim label
' @type label As String
Set label = "x"
%>`
	client.request("initialize", map[string]any{
		"processId":    nil,
		"rootUri":      pathToFileURI(root),
		"capabilities": map[string]any{},
	})
	notifyConfiguration(t, client, map[string]any{"aspLsp": map[string]any{
		"vbscript": map[string]any{
			"typeChecking": "strict",
			"comTypes": map[string]any{
				"Custom.Widget": map[string]any{"members": map[string]any{
					"Child": "Custom.Child",
					"Title": "String",
					"Ping": map[string]any{
						"kind":       "method",
						"returnType": "Boolean",
						"parameters": []map[string]any{{"name": "name", "type": "String"}},
					},
				}},
				"Custom.Child": map[string]any{"members": map[string]any{
					"Name": "String",
				}},
			},
			"globals": map[string]any{
				"Repository": "Custom.Widget",
			},
		},
	}})
	notifyOpenClassicASPDocument(t, client, uri, source)
	diagnostics := waitForDiagnosticsContaining(t, client, "no member")
	diagnosticText := string(diagnostics.Params)
	if !strings.Contains(diagnosticText, "no member") || !strings.Contains(diagnosticText, "Argument count") ||
		!strings.Contains(diagnosticText, "setScalar") {
		t.Fatalf("strict type diagnostics mismatch: %s", diagnosticText)
	}
	if strings.Contains(diagnosticText, "Repository") {
		t.Fatalf("global COM type produced Repository diagnostic: %s", diagnosticText)
	}

	completions := completionLabels(client.request("textDocument/completion", map[string]any{
		"textDocument": map[string]any{"uri": uri},
		"position":     map[string]any{"line": 4, "character": 7},
	}).Result)
	if !completions.contains("Title") || !completions.contains("Ping") {
		t.Fatalf("custom COM completions missing Title or Ping: %#v", completions)
	}
	globalCompletions := completionLabels(client.request("textDocument/completion", map[string]any{
		"textDocument": map[string]any{"uri": uri},
		"position":     map[string]any{"line": 7, "character": 11},
	}).Result)
	if !globalCompletions.contains("Title") {
		t.Fatalf("global COM completions missing Title: %#v", globalCompletions)
	}
	typeHierarchy := client.request("textDocument/prepareTypeHierarchy", map[string]any{
		"textDocument": map[string]any{"uri": uri},
		"position":     positionAt(source, strings.Index(source, "widget.Title")+len("widget")-1),
	})
	if !strings.Contains(mustJSONText(t, typeHierarchy.Result), "Custom.Widget") {
		t.Fatalf("type hierarchy missing Custom.Widget: %s", mustJSONText(t, typeHierarchy.Result))
	}
	var items []any
	mustDecodeResult(t, typeHierarchy.Result, &items)
	if len(items) == 0 {
		t.Fatalf("type hierarchy returned no items: %s", mustJSONText(t, typeHierarchy.Result))
	}
	subtypes := client.request("typeHierarchy/subtypes", map[string]any{"item": items[0]})
	if !strings.Contains(mustJSONText(t, subtypes.Result), "Custom.Child") {
		t.Fatalf("type hierarchy subtypes missing Custom.Child: %s", mustJSONText(t, subtypes.Result))
	}
	supertypes := client.request("typeHierarchy/supertypes", map[string]any{"item": items[0]})
	if got := mustJSONText(t, supertypes.Result); got != "[]" {
		t.Fatalf("configured type hierarchy supertypes = %s, want []", got)
	}
}

func TestStdioParityKeepsIncludeBackedVBScriptTypesOutOfProgressiveDiagnostics(t *testing.T) {
	client := startStdioTestClient(t)
	defer client.close()

	root := t.TempDir()
	owner := filepath.Join(root, "default.asp")
	include := filepath.Join(root, "customer.inc")
	writeVBScriptTypeFixture(t, include, `<%
Class IncludedCustomer
  Public Name
End Class
%>`)
	source := `<!-- #include file="customer.inc" -->
<%
' @type customer As IncludedCustomer
Dim customer
customer.Name
%>`
	writeVBScriptTypeFixture(t, owner, source)
	uri := pathToFileURI(owner)
	client.request("initialize", map[string]any{
		"processId":    nil,
		"rootUri":      pathToFileURI(root),
		"capabilities": map[string]any{},
	})
	notifyConfiguration(t, client, map[string]any{"aspLsp": map[string]any{
		"vbscript": map[string]any{"typeChecking": "strict"},
	}})
	notifyOpenClassicASPDocument(t, client, uri, source)
	client.request("textDocument/hover", map[string]any{
		"textDocument": map[string]any{"uri": uri},
		"position":     positionAt(source, strings.Index(source, "customer.Name")),
	})
	diagnostics := client.request("textDocument/diagnostic", map[string]any{
		"textDocument": map[string]any{"uri": uri},
	})
	serialized := mustJSONText(t, diagnostics.Result)
	if strings.Contains(serialized, "no member") || strings.Contains(serialized, "missingMember") {
		t.Fatalf("include-backed type leaked progressive diagnostics: %s", serialized)
	}
}

func TestStdioParityKeepsIncludedVBScriptAnnotationsOutOfOwnerScope(t *testing.T) {
	client := startStdioTestClient(t)
	defer client.close()

	root := t.TempDir()
	owner := filepath.Join(root, "default.asp")
	include := filepath.Join(root, "shared.inc")
	writeVBScriptTypeFixture(t, include, `<%
' @type sharedName As Number
Dim sharedName
sharedName = 1
%>`)
	source := `<!-- #include file="shared.inc" -->
<%
Option Explicit
Dim sharedName
sharedName = "owner"
%>`
	writeVBScriptTypeFixture(t, owner, source)
	uri := pathToFileURI(owner)
	client.request("initialize", map[string]any{
		"processId":    nil,
		"rootUri":      pathToFileURI(root),
		"capabilities": map[string]any{},
	})
	notifyConfiguration(t, client, map[string]any{"aspLsp": map[string]any{
		"vbscript": map[string]any{"typeChecking": "strict"},
	}})
	notifyOpenClassicASPDocument(t, client, uri, source)
	diagnostics := client.request("textDocument/diagnostic", map[string]any{
		"textDocument": map[string]any{"uri": uri},
	})
	serialized := mustJSONText(t, diagnostics.Result)
	if strings.Contains(serialized, "typeMismatch") || strings.Contains(serialized, "sharedName' is Number") {
		t.Fatalf("included annotation constrained owner declaration: %s", serialized)
	}
}

func TestStdioParitySupportsVBScriptClassTypeHierarchyRequests(t *testing.T) {
	client := startStdioTestClient(t)
	defer client.close()

	root := t.TempDir()
	uri := pathToFileURI(filepath.Join(root, "class-hierarchy.asp"))
	source := `<%
Class Widget
End Class
%>`
	writeVBScriptTypeFixture(t, filepath.Join(root, "class-hierarchy.asp"), source)
	client.request("initialize", map[string]any{
		"processId":    nil,
		"rootUri":      pathToFileURI(root),
		"capabilities": map[string]any{},
	})
	notifyOpenClassicASPDocument(t, client, uri, source)
	waitForDiagnosticsContaining(t, client, "")

	hierarchy := client.request("textDocument/prepareTypeHierarchy", map[string]any{
		"textDocument": map[string]any{"uri": uri},
		"position":     positionAt(source, strings.Index(source, "Widget")+2),
	})
	if !strings.Contains(mustJSONText(t, hierarchy.Result), "Widget") {
		t.Fatalf("type hierarchy missing Widget: %s", mustJSONText(t, hierarchy.Result))
	}
	var items []any
	mustDecodeResult(t, hierarchy.Result, &items)
	if len(items) == 0 {
		t.Fatalf("type hierarchy returned no items: %s", mustJSONText(t, hierarchy.Result))
	}
	subtypes := client.request("typeHierarchy/subtypes", map[string]any{"item": items[0]})
	if got := mustJSONText(t, subtypes.Result); got != "[]" {
		t.Fatalf("type hierarchy subtypes = %s, want []", got)
	}
	supertypes := client.request("typeHierarchy/supertypes", map[string]any{"item": items[0]})
	if got := mustJSONText(t, supertypes.Result); got != "[]" {
		t.Fatalf("type hierarchy supertypes = %s, want []", got)
	}
}

func TestStdioParityKeepsIncludeBackedVBScriptMemberDiagnosticsCorrectAfterEdits(t *testing.T) {
	client := startStdioTestClient(t)
	defer client.close()

	root := t.TempDir()
	owner := filepath.Join(root, "default.asp")
	include := filepath.Join(root, "customer.inc")
	writeVBScriptTypeFixture(t, include, `<%
Class IncludedCustomer
  Public Name
End Class
%>`)
	source := `<!-- #include file="customer.inc" -->
<%
' @type customer As IncludedCustomer
Dim customer
Response.Write customer.Name
%>`
	writeVBScriptTypeFixture(t, owner, source)
	uri := pathToFileURI(owner)
	client.request("initialize", map[string]any{
		"processId":    nil,
		"rootUri":      pathToFileURI(root),
		"capabilities": map[string]any{},
	})
	notifyConfiguration(t, client, map[string]any{"aspLsp": map[string]any{
		"debug":       map[string]any{"output": "verbose"},
		"diagnostics": map[string]any{"debounceMs": 0},
		"vbscript":    map[string]any{"typeChecking": "strict"},
	}})
	notifyOpenClassicASPDocument(t, client, uri, source)
	initial := client.request("textDocument/diagnostic", map[string]any{
		"textDocument": map[string]any{"uri": uri},
	})
	if serialized := mustJSONText(t, initial.Result); strings.Contains(serialized, "no member") || strings.Contains(serialized, "missingMember") {
		t.Fatalf("initial include-backed type diagnostics mismatch: %s", serialized)
	}
	edited := strings.Replace(source, "Response.Write customer.Name", "Response.Write customer.Name\nResponse.Write customer.Name", 1)
	if err := client.notify("textDocument/didChange", map[string]any{
		"textDocument":   map[string]any{"uri": uri, "version": 2},
		"contentChanges": []map[string]any{{"text": edited}},
	}); err != nil {
		t.Fatal(err)
	}
	changed := client.request("textDocument/diagnostic", map[string]any{
		"textDocument": map[string]any{"uri": uri},
	})
	if serialized := mustJSONText(t, changed.Result); strings.Contains(serialized, "no member") || strings.Contains(serialized, "missingMember") {
		t.Fatalf("changed include-backed type diagnostics mismatch: %s", serialized)
	}
}

func TestStdioParityReturnsVBScriptTypeDiagnosticQuickFixes(t *testing.T) {
	client := startStdioTestClient(t)
	defer client.close()

	root := t.TempDir()
	uri := "file:///tmp/go-vb-type-fixes.asp"
	source := `<%
Dim widget
Dim title
' @type typedValue As Number
Dim typedValue
widget = Server.CreateObject("Custom.Widget")
Set title = "hello"
typedValue = "hello"
%>`
	client.request("initialize", map[string]any{
		"processId":    nil,
		"rootUri":      pathToFileURI(root),
		"capabilities": map[string]any{},
	})
	notifyConfiguration(t, client, map[string]any{"aspLsp": map[string]any{
		"vbscript": map[string]any{
			"typeChecking": "strict",
			"comTypes": map[string]any{
				"Custom.Widget": map[string]any{"members": map[string]any{}},
			},
		},
	}})
	notifyOpenClassicASPDocument(t, client, uri, source)
	diagnosticsMessage := waitForDiagnosticsContaining(t, client, "objectNeedsSet")
	typeDiagnostics := diagnosticsFromSourceLSP(t, diagnosticsMessage, "asp-lsp-vbscript-type")
	diagnosticJSON := mustJSONText(t, typeDiagnostics)
	for _, expected := range []string{"objectNeedsSet", "setScalar", "typeMismatch"} {
		if !strings.Contains(diagnosticJSON, expected) {
			t.Fatalf("type diagnostics missing %q: %s", expected, diagnosticJSON)
		}
	}
	actions := client.request("textDocument/codeAction", map[string]any{
		"textDocument": map[string]any{"uri": uri},
		"range": map[string]any{
			"start": map[string]any{"line": 5, "character": 0},
			"end":   map[string]any{"line": 7, "character": 20},
		},
		"context": map[string]any{"diagnostics": typeDiagnostics},
	})
	actionsJSON := mustJSONText(t, actions.Result)
	for _, expected := range []string{
		"Use Set for object assignment to widget",
		"Remove Set from scalar assignment to title",
		"Annotate typedValue as String",
	} {
		if !strings.Contains(actionsJSON, expected) {
			t.Fatalf("type quick fix missing %q: %s", expected, actionsJSON)
		}
	}
}

func TestStdioParityPublishesIncludedVBScriptTypeDiagnosticsUnderChildURI(t *testing.T) {
	client := startStdioTestClient(t)
	defer client.close()

	root := t.TempDir()
	owner := filepath.Join(root, "default.asp")
	include := filepath.Join(root, "shared.inc")
	ownerSource := `<%
' @type typedValue As Number
Dim typedValue
%>
<!-- #include file="shared.inc" -->`
	includeSource := `<%
Dim widget
Dim title
' @type typedValue As Number
Dim typedValue
widget = Server.CreateObject("Custom.Widget")
Set title = "hello"
typedValue = "hello"
%>`
	writeVBScriptTypeFixture(t, owner, ownerSource)
	writeVBScriptTypeFixture(t, include, includeSource)
	ownerURI := pathToFileURI(owner)
	includeURI := pathToFileURI(include)

	client.request("initialize", map[string]any{
		"processId":    nil,
		"rootUri":      pathToFileURI(root),
		"capabilities": map[string]any{},
	})
	notifyConfiguration(t, client, map[string]any{"aspLsp": map[string]any{
		"diagnostics": map[string]any{"debounceMs": 0},
		"vbscript": map[string]any{
			"typeChecking": "strict",
			"comTypes": map[string]any{
				"Custom.Widget": map[string]any{"members": map[string]any{}},
			},
		},
	}})
	notifyOpenClassicASPDocument(t, client, includeURI, includeSource)
	client.waitForNotification("textDocument/publishDiagnostics", includeURI)
	notifyOpenClassicASPDocument(t, client, ownerURI, ownerSource)
	childDiagnostics, seen := client.waitForNotificationWithSeen("textDocument/publishDiagnostics", "objectNeedsSet")
	var childParams struct {
		URI         string           `json:"uri"`
		Diagnostics []lsp.Diagnostic `json:"diagnostics"`
	}
	mustDecodeResult(t, childDiagnostics.Params, &childParams)
	if childParams.URI != includeURI {
		t.Fatalf("included type diagnostics published under %q, want %q: %s", childParams.URI, includeURI, childDiagnostics.Params)
	}
	if len(childParams.Diagnostics) != 3 {
		t.Fatalf("included type diagnostics = %#v, want objectNeedsSet/setScalar/typeMismatch", childParams.Diagnostics)
	}
	for _, code := range []string{"objectNeedsSet", "setScalar", "typeMismatch"} {
		found := false
		for _, diagnostic := range childParams.Diagnostics {
			if diagnosticCodeString(diagnostic) == code {
				found = true
				if diagnosticDataString(diagnostic, "uri") != includeURI {
					t.Fatalf("%s diagnostic URI data = %q, want %q", code, diagnosticDataString(diagnostic, "uri"), includeURI)
				}
			}
		}
		if !found {
			t.Fatalf("included diagnostics missing %s: %#v", code, childParams.Diagnostics)
		}
	}
	for _, message := range seen {
		if message.Method != "textDocument/publishDiagnostics" || !strings.Contains(string(message.Params), "objectNeedsSet") {
			continue
		}
		var params struct {
			URI string `json:"uri"`
		}
		mustDecodeResult(t, message.Params, &params)
		if params.URI == ownerURI {
			t.Fatalf("owner publication carried included type diagnostic: %s", message.Params)
		}
	}
}

func TestStdioParityAppliesIncludedVBScriptTypeQuickFixesOnlyToChild(t *testing.T) {
	client := startStdioTestClient(t)
	defer client.close()

	root := t.TempDir()
	owner := filepath.Join(root, "default.asp")
	include := filepath.Join(root, "shared.inc")
	ownerSource := `<%
' @type typedValue As Number
Dim typedValue
%>
<!-- #include file="shared.inc" -->`
	includeSource := `<%
Dim widget
Dim title
' @type typedValue As Number
Dim typedValue
widget = Server.CreateObject("Custom.Widget")
Set title = "hello"
typedValue = "hello"
%>`
	writeVBScriptTypeFixture(t, owner, ownerSource)
	writeVBScriptTypeFixture(t, include, includeSource)
	ownerURI := pathToFileURI(owner)
	includeURI := pathToFileURI(include)

	client.request("initialize", map[string]any{
		"processId":    nil,
		"rootUri":      pathToFileURI(root),
		"capabilities": map[string]any{},
	})
	notifyConfiguration(t, client, map[string]any{"aspLsp": map[string]any{
		"diagnostics": map[string]any{"debounceMs": 0},
		"vbscript": map[string]any{
			"typeChecking": "strict",
			"comTypes": map[string]any{
				"Custom.Widget": map[string]any{"members": map[string]any{}},
			},
		},
	}})
	notifyOpenClassicASPDocument(t, client, includeURI, includeSource)
	client.waitForNotification("textDocument/publishDiagnostics", includeURI)
	notifyOpenClassicASPDocument(t, client, ownerURI, ownerSource)
	childDiagnosticsMessage := client.waitForNotification("textDocument/publishDiagnostics", "objectNeedsSet")
	childDiagnostics := diagnosticsFromPublishMessage(t, childDiagnosticsMessage)
	if len(childDiagnostics) != 3 {
		t.Fatalf("included diagnostics = %#v, want three type diagnostics", childDiagnostics)
	}
	ownerActions := client.request("textDocument/codeAction", map[string]any{
		"textDocument": map[string]any{"uri": ownerURI},
		"range":        map[string]any{"start": map[string]any{"line": 0, "character": 0}, "end": map[string]any{"line": 10, "character": 0}},
		"context":      map[string]any{"diagnostics": childDiagnostics, "only": []string{"quickfix"}},
	})
	var ownerActionItems []lsp.CodeAction
	mustDecodeResult(t, ownerActions.Result, &ownerActionItems)
	if len(ownerActionItems) != 0 {
		t.Fatalf("owner code actions mis-targeted included diagnostics: %#v", ownerActionItems)
	}
	childActions := client.request("textDocument/codeAction", map[string]any{
		"textDocument": map[string]any{"uri": includeURI},
		"range":        map[string]any{"start": map[string]any{"line": 0, "character": 0}, "end": map[string]any{"line": 10, "character": 0}},
		"context":      map[string]any{"diagnostics": childDiagnostics, "only": []string{"quickfix"}},
	})
	var actions []lsp.CodeAction
	mustDecodeResult(t, childActions.Result, &actions)
	for _, expected := range []string{"Use Set for object assignment", "Remove Set from scalar assignment", "Annotate typedValue as String"} {
		if codeActionByTitle(actions, expected) == nil {
			t.Fatalf("included child quick fix missing %q: %s", expected, mustJSONText(t, childActions.Result))
		}
	}
	for _, testCase := range []struct {
		title   string
		want    string
		invalid string
	}{
		{title: "Use Set for object assignment", want: `Set widget = Server.CreateObject("Custom.Widget")`},
		{title: "Remove Set from scalar assignment", want: `title = "hello"`},
		{title: "Annotate typedValue as String", want: "' @type typedValue As String"},
	} {
		action := codeActionByTitle(actions, testCase.title)
		if action == nil {
			t.Fatalf("included child action missing %q", testCase.title)
		}
		updated := applyWorkspaceEditForURI(t, includeSource, includeURI, action.Edit)
		if !strings.Contains(updated, testCase.want) {
			t.Fatalf("included child %s edit produced %q, want substring %q", testCase.title, updated, testCase.want)
		}
		if strings.Contains(updated, ownerSource) {
			t.Fatalf("included child %s edit unexpectedly contained owner source", testCase.title)
		}
	}
}

func writeVBScriptTypeFixture(t *testing.T, path string, text string) {
	t.Helper()
	if err := os.WriteFile(path, []byte(text), 0o644); err != nil {
		t.Fatal(err)
	}
}
