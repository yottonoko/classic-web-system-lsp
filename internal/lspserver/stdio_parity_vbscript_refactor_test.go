package lspserver

import (
	"path/filepath"
	"strings"
	"testing"

	"github.com/yottonoko/classic-web-system-lsp/internal/lsp"
)

func TestStdioParityDoesNotReportStatementCallSyntaxDiagnosticsForASPExpressionOutput(t *testing.T) {
	client := startStdioTestClient(t)
	defer client.close()

	root := t.TempDir()
	uri := pathToFileURI(filepath.Join(root, "go-asp-expression-call.asp"))
	source := `<%
Function RenderCustomerRows(ByVal customerList, ByVal activeCustomerId)
  RenderCustomerRows = ""
End Function
%>
<%= RenderCustomerRows(filteredCustomers, selectedCustomerId) %>`
	client.request("initialize", map[string]any{
		"processId":    nil,
		"rootUri":      pathToFileURI(root),
		"capabilities": map[string]any{},
	})
	diagnostics := openClassicASPDocumentWithDiagnostics(t, client, uri, source)
	serialized := string(diagnostics.Params)
	for _, unexpected := range []string{"statementCallDisallowsParenthesizedArguments", "VBScript call syntax is invalid"} {
		if strings.Contains(serialized, unexpected) {
			t.Fatalf("ASP expression output reported statement call syntax diagnostic %q: %s", unexpected, serialized)
		}
	}
}

func TestStdioParityReturnsVBScriptCompletionsInsideHTMLAttributeASPIslands(t *testing.T) {
	client := startStdioTestClient(t)
	defer client.close()

	root := t.TempDir()
	uri := pathToFileURI(filepath.Join(root, "go-html-attribute-asp-completion.asp"))
	source := `<input value="<%= Response. %>" <% Response. %>>`
	client.request("initialize", map[string]any{
		"processId":    nil,
		"rootUri":      pathToFileURI(root),
		"capabilities": map[string]any{},
	})
	openClassicASPDocument(t, client, uri, source)

	for _, offset := range []int{
		strings.Index(source, "Response.") + len("Response."),
		strings.LastIndex(source, "Response.") + len("Response."),
	} {
		labels := completionLabels(client.request("textDocument/completion", map[string]any{
			"textDocument": map[string]any{"uri": uri},
			"position":     positionAt(source, offset),
		}).Result)
		if !labels.contains("Write") {
			t.Fatalf("HTML attribute ASP island completions missing Write at offset %d: %#v", offset, labels)
		}
	}
}

func TestStdioParityReturnsVBScriptExtractVariableRefactors(t *testing.T) {
	client := startStdioTestClient(t)
	defer client.close()

	root := t.TempDir()
	uri := pathToFileURI(filepath.Join(root, "go-vb-refactor.asp"))
	source := `<%
Response.Write Request.QueryString("name")
%>`
	client.request("initialize", map[string]any{
		"processId":    nil,
		"rootUri":      pathToFileURI(root),
		"capabilities": map[string]any{},
	})
	openClassicASPDocument(t, client, uri, source)

	actions := requestExtractVariableActions(t, client, uri, source, `Request.QueryString("name")`)
	serialized := mustJSONText(t, actions.Result)
	for _, expected := range []string{
		"Extract VBScript variable",
		"Dim extractedValue",
		`extractedValue = Request.QueryString(\"name\")`,
		`"newText":"extractedValue"`,
	} {
		if !strings.Contains(serialized, expected) {
			t.Fatalf("extract variable action missing %q: %s", expected, serialized)
		}
	}
}

func TestStdioParityAvoidsExistingVBScriptExtractVariableNames(t *testing.T) {
	client := startStdioTestClient(t)
	defer client.close()

	root := t.TempDir()
	uri := pathToFileURI(filepath.Join(root, "go-vb-refactor-collision-focused.asp"))
	source := `<%
Dim extractedValue
Response.Write Request.QueryString("name")
%>`
	client.request("initialize", map[string]any{
		"processId":    nil,
		"rootUri":      pathToFileURI(root),
		"capabilities": map[string]any{},
	})
	openClassicASPDocument(t, client, uri, source)

	actions := requestExtractVariableActions(t, client, uri, source, `Request.QueryString("name")`)
	serialized := mustJSONText(t, actions.Result)
	for _, expected := range []string{
		"Dim extractedValue1",
		`extractedValue1 = Request.QueryString(\"name\")`,
		`"newText":"extractedValue1"`,
	} {
		if !strings.Contains(serialized, expected) {
			t.Fatalf("extract variable collision action missing %q: %s", expected, serialized)
		}
	}
}

func TestStdioParityDoesNotReturnVBScriptExtractRefactorsForUnsupportedSelections(t *testing.T) {
	client := startStdioTestClient(t)
	defer client.close()

	root := t.TempDir()
	uri := pathToFileURI(filepath.Join(root, "go-vb-refactor-unsupported-focused.asp"))
	source := `<div>Request.QueryString("name")</div>
<%
Response.Write Request.QueryString("name")
Response.Write Request.Form("name")
%>`
	client.request("initialize", map[string]any{
		"processId":    nil,
		"rootUri":      pathToFileURI(root),
		"capabilities": map[string]any{},
	})
	openClassicASPDocument(t, client, uri, source)

	htmlStart := strings.Index(source, `Request.QueryString("name")`)
	htmlEnd := htmlStart + len(`Request.QueryString("name")`)
	vbStart := strings.LastIndex(source, `Request.QueryString("name")`)
	vbEnd := vbStart + len(`Request.QueryString("name")`)
	multilineEnd := strings.Index(source, `Request.Form("name")`) + len(`Request.Form("name")`)
	for _, testCase := range []struct {
		name  string
		start int
		end   int
	}{
		{name: "html", start: htmlStart, end: htmlEnd},
		{name: "empty", start: vbStart, end: vbStart},
		{name: "whitespace", start: vbStart - 1, end: vbEnd},
		{name: "multiline", start: vbStart, end: multilineEnd},
	} {
		actions := client.request("textDocument/codeAction", map[string]any{
			"textDocument": map[string]any{"uri": uri},
			"range": mapPositionRange(
				positionAt(source, testCase.start),
				positionAt(source, testCase.end),
			),
			"context": map[string]any{"diagnostics": []diagnosticResult{}, "only": []string{"refactor.extract"}},
		})
		if resultArrayLength(t, actions.Result) != 0 {
			t.Fatalf("%s unsupported extract variable selection returned actions: %s", testCase.name, mustJSONText(t, actions.Result))
		}
	}
}

func TestStdioParityReturnsVBScriptQuickFixesForUnusedDeclarations(t *testing.T) {
	client := startStdioTestClient(t)
	defer client.close()

	root := t.TempDir()
	uri := pathToFileURI(filepath.Join(root, "go-vb-unused-code-actions.asp"))
	source := `<%
Const usedValue = 1
Sub Save(usedArg, ByRef unusedArg)
  Dim unusedValue
  Response.Write usedArg
End Sub
Response.Write usedValue
%>`
	client.request("initialize", map[string]any{
		"processId":    nil,
		"rootUri":      pathToFileURI(root),
		"capabilities": map[string]any{},
	})
	diagnosticsMessage := openClassicASPDocumentWithDiagnostics(t, client, uri, source)
	diagnostics := diagnosticsFromPublishMessage(t, diagnosticsMessage)
	diagnosticsText := mustJSONText(t, diagnostics)
	if !strings.Contains(diagnosticsText, "unusedValue") {
		t.Fatalf("unused declaration diagnostics missing unusedValue: %s", diagnosticsText)
	}
	for _, diagnostic := range diagnostics {
		if diagnostic.Source == "asp-lsp-vbscript-unused" && !diagnosticHasTag(diagnostic, lsp.DiagnosticTagUnnecessary) {
			t.Fatalf("unused diagnostic missing unnecessary tag: %#v", diagnostic)
		}
	}

	unusedValueStart := strings.Index(source, "unusedValue")
	valueActions := client.request("textDocument/codeAction", map[string]any{
		"textDocument": map[string]any{"uri": uri},
		"range": mapPositionRange(
			positionAt(source, unusedValueStart),
			positionAt(source, unusedValueStart+len("unusedValue")),
		),
		"context": map[string]any{
			"diagnostics": diagnostics,
			"only":        []string{"quickfix"},
		},
	})
	serialized := mustJSONText(t, valueActions.Result)
	if !strings.Contains(serialized, "Remove unused declaration unusedValue") || !strings.Contains(serialized, `"newText":""`) {
		t.Fatalf("unused value quick fix mismatch: %s", serialized)
	}

	var unusedArgDiagnostic *lsp.Diagnostic
	for i := range diagnostics {
		if strings.Contains(diagnostics[i].Message, "unusedArg") {
			unusedArgDiagnostic = &diagnostics[i]
			break
		}
	}
	if unusedArgDiagnostic == nil {
		t.Fatalf("unusedArg diagnostic missing: %s", diagnosticsText)
	}
	parameterActions := client.request("textDocument/codeAction", map[string]any{
		"textDocument": map[string]any{"uri": uri},
		"range":        unusedArgDiagnostic.Range,
		"context": map[string]any{
			"diagnostics": []lsp.Diagnostic{*unusedArgDiagnostic},
			"only":        []string{"quickfix"},
		},
	})
	var actions []lsp.CodeAction
	mustDecodeResult(t, parameterActions.Result, &actions)
	var parameterEdit *lsp.TextEdit
	for _, action := range actions {
		if strings.Contains(mustJSONText(t, action), "unusedArg") && action.Edit != nil {
			edits := action.Edit.Changes[uri]
			if len(edits) > 0 {
				edit := edits[0]
				parameterEdit = &edit
				break
			}
		}
	}
	if parameterEdit == nil {
		t.Fatalf("unusedArg parameter quick fix edit missing: %s", mustJSONText(t, parameterActions.Result))
	}
	updated := applyTextEditToString(t, source, *parameterEdit)
	for _, expected := range []string{"Sub Save(usedArg)"} {
		if !strings.Contains(updated, expected) {
			t.Fatalf("unusedArg edit missing %q: %s", expected, updated)
		}
	}
	for _, unexpected := range []string{"ByRef unusedArg", "ByRef )"} {
		if strings.Contains(updated, unexpected) {
			t.Fatalf("unusedArg edit left %q: %s", unexpected, updated)
		}
	}
}

func TestStdioParityReturnsQuickFixesForUndeclaredVBScriptVariables(t *testing.T) {
	client := startStdioTestClient(t)
	defer client.close()

	root := t.TempDir()
	uri := pathToFileURI(filepath.Join(root, "go-vb-code-action.asp"))
	marked := markedDocument(`<%
Option Explicit
Response.Write miss<<<caret>>>ingName
%>`)
	client.request("initialize", map[string]any{
		"processId":    nil,
		"rootUri":      pathToFileURI(root),
		"capabilities": map[string]any{},
	})
	diagnosticsMessage := openClassicASPDocumentWithDiagnostics(t, client, uri, marked.Text)
	diagnostics := diagnosticsFromPublishMessage(t, diagnosticsMessage)
	if serialized := mustJSONText(t, diagnostics); !strings.Contains(serialized, "missingName") {
		t.Fatalf("undeclared variable diagnostic missing missingName: %s", serialized)
	}

	actions := client.request("textDocument/codeAction", map[string]any{
		"textDocument": map[string]any{"uri": uri},
		"range": mapPositionRange(
			marked.Position,
			marked.Position,
		),
		"context": map[string]any{
			"diagnostics": diagnostics,
			"only":        []string{"quickfix"},
		},
	})
	if serialized := mustJSONText(t, actions.Result); !strings.Contains(serialized, "Declare missingName with Dim") {
		t.Fatalf("undeclared variable quick fix missing: %s", serialized)
	}
}

func openClassicASPDocumentWithDiagnostics(t *testing.T, client *stdioTestClient, uri string, text string) *rpcMessage {
	t.Helper()
	if err := client.notify("textDocument/didOpen", map[string]any{
		"textDocument": map[string]any{
			"uri":        uri,
			"languageId": "classic-asp",
			"version":    1,
			"text":       text,
		},
	}); err != nil {
		t.Fatal(err)
	}
	return client.waitForNotification("textDocument/publishDiagnostics", uri)
}

func diagnosticsFromPublishMessage(t *testing.T, message *rpcMessage) []lsp.Diagnostic {
	t.Helper()
	var params struct {
		Diagnostics []lsp.Diagnostic `json:"diagnostics"`
	}
	mustDecodeResult(t, message.Params, &params)
	return params.Diagnostics
}

func diagnosticHasTag(diagnostic lsp.Diagnostic, tag lsp.DiagnosticTag) bool {
	for _, candidate := range diagnostic.Tags {
		if candidate == tag {
			return true
		}
	}
	return false
}

func requestExtractVariableActions(t *testing.T, client *stdioTestClient, uri string, source string, selected string) *rpcMessage {
	t.Helper()
	selectionStart := strings.Index(source, selected)
	if selectionStart < 0 {
		t.Fatalf("selection %q not found", selected)
	}
	selectionEnd := selectionStart + len(selected)
	return client.request("textDocument/codeAction", map[string]any{
		"textDocument": map[string]any{"uri": uri},
		"range": mapPositionRange(
			positionAt(source, selectionStart),
			positionAt(source, selectionEnd),
		),
		"context": map[string]any{"diagnostics": []diagnosticResult{}, "only": []string{"refactor.extract"}},
	})
}
