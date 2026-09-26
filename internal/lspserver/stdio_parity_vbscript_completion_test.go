package lspserver

import (
	"os"
	"path/filepath"
	"strings"
	"testing"
)

func TestStdioParityReturnsFullVBScriptSyntaxSnippetsAndPrefixSnippets(t *testing.T) {
	client := startStdioTestClient(t)
	defer client.close()

	root := t.TempDir()
	uri := pathToFileURI(filepath.Join(root, "go-vbscript-snippet-completions.asp"))
	source := "<% Option Explicit\nDo\n%>"
	client.request("initialize", map[string]any{
		"processId":    nil,
		"rootUri":      pathToFileURI(root),
		"capabilities": map[string]any{},
	})
	notifyOpenClassicASPDocument(t, client, uri, source)
	waitForDiagnosticsContaining(t, client, "")

	topLevel := completionItems(client.request("textDocument/completion", map[string]any{
		"textDocument": map[string]any{"uri": uri},
		"position":     positionAt(source, strings.Index(source, "\nDo")),
	}).Result)
	for _, label := range []string{"If Then", "If Then Else", "Do Loop", "Do While Loop", "Do Until Loop", "For Next", "For Each Next", "Select Case", "With", "Sub", "Function", "Class", "Property Get", "Property Let", "Property Set"} {
		item, ok := topLevel.find(label)
		if !ok || item.Kind != 15 || item.InsertTextFormat != 2 {
			t.Fatalf("snippet completion %s = %#v ok=%v", label, item, ok)
		}
	}

	prefix := completionItems(client.request("textDocument/completion", map[string]any{
		"textDocument": map[string]any{"uri": uri},
		"position":     positionAt(source, strings.Index(source, "Do")+len("Do")),
	}).Result)
	for _, label := range []string{"Do", "Do Loop", "Do While Loop", "Do Until Loop"} {
		if _, ok := prefix.find(label); !ok {
			t.Fatalf("prefix completions missing %s: %#v", label, prefix)
		}
	}

	notifyConfiguration(t, client, map[string]any{"aspLsp": map[string]any{"vbscript": map[string]any{"syntaxSnippets": false}}})
	disabled := completionLabels(client.request("textDocument/completion", map[string]any{
		"textDocument": map[string]any{"uri": uri},
		"position":     positionAt(source, strings.Index(source, "\nDo")),
	}).Result)
	if disabled.contains("If Then") || !disabled.contains("If") || !disabled.contains("Response") {
		t.Fatalf("snippet-disabled completions mismatch: %#v", disabled)
	}
}

func TestStdioParityCompletesMatchingVBScriptBlockClosersAndContinuations(t *testing.T) {
	client := startStdioTestClient(t)
	defer client.close()

	root := t.TempDir()
	client.request("initialize", map[string]any{
		"processId":    nil,
		"rootUri":      pathToFileURI(root),
		"capabilities": map[string]any{},
	})
	for _, testCase := range []struct {
		name   string
		source string
		needle string
		labels []string
	}{
		{"then", "<%\nIf ready \n%>", "ready ", []string{"Then"}},
		{"if", "<%\nIf ready Then\ne\nEnd If\n%>", "\ne", []string{"ElseIf", "Else", "End", "End If"}},
		{"select", "<%\nSelect Case value\nc\nEnd Select\n%>", "\nc", []string{"Case"}},
		{"loop", "<%\nDo\nlo\n%>", "lo", []string{"Loop"}},
		{"wend", "<%\nWhile ready\nwe\n%>", "we", []string{"Wend"}},
		{"next", "<%\nFor index = 1 To 3\nn\n%>", "\nn", []string{"Next"}},
		{"function", "<%\nFunction Render()\nend f\n%>", "end f", []string{"End Function"}},
		{"sub", "<%\nSub Render()\nend s\n%>", "end s", []string{"End Sub"}},
		{"class", "<%\nClass Widget\nend c\n%>", "end c", []string{"End Class"}},
		{"property", "<%\nClass Widget\nProperty Get Name()\nend p\nEnd Class\n%>", "end p", []string{"End Property"}},
	} {
		uri := pathToFileURI(filepath.Join(root, "go-vbscript-"+testCase.name+"-completion.asp"))
		notifyOpenClassicASPDocument(t, client, uri, testCase.source)
		waitForDiagnosticsContaining(t, client, uri)
		position := positionAt(testCase.source, strings.Index(testCase.source, testCase.needle)+len(testCase.needle))
		labels := completionLabels(client.request("textDocument/completion", map[string]any{
			"textDocument": map[string]any{"uri": uri},
			"position":     position,
		}).Result)
		for _, label := range testCase.labels {
			if !labels.contains(label) {
				t.Fatalf("%s completions missing %s: %#v", testCase.name, label, labels)
			}
		}
	}
}

func TestStdioParityKeepsVBScriptLookupAndCompletionsCaseInsensitive(t *testing.T) {
	client := startStdioTestClient(t)
	defer client.close()

	root := t.TempDir()
	uri := pathToFileURI(filepath.Join(root, "case-insensitive-vbscript.asp"))
	source := `<%
Dim CustomerName
cust
CUST
CuSt
Response.Write customername
%>`
	client.request("initialize", map[string]any{
		"processId":    nil,
		"rootUri":      pathToFileURI(root),
		"capabilities": map[string]any{},
	})
	notifyOpenClassicASPDocument(t, client, uri, source)
	waitForDiagnosticsContaining(t, client, "")

	for _, needle := range []string{"cust", "CUST", "CuSt"} {
		position := positionAt(source, strings.Index(source, needle)+len(needle))
		completions := completionLabels(client.request("textDocument/completion", map[string]any{
			"textDocument": map[string]any{"uri": uri},
			"position":     position,
		}).Result)
		if !completions.contains("CustomerName") {
			t.Fatalf("mixed-case prefix %q completions missing CustomerName: %#v", needle, completions)
		}
	}

	usagePosition := positionAt(source, strings.Index(source, "customername")+len("customer"))
	definition := client.request("textDocument/definition", map[string]any{
		"textDocument": map[string]any{"uri": uri},
		"position":     usagePosition,
	})
	if !strings.Contains(mustJSONText(t, definition.Result), `"line":1`) {
		t.Fatalf("case-insensitive definition mismatch: %s", mustJSONText(t, definition.Result))
	}
	references := client.request("textDocument/references", map[string]any{
		"textDocument": map[string]any{"uri": uri},
		"position":     usagePosition,
		"context":      map[string]any{"includeDeclaration": true},
	})
	if got := resultArrayLength(t, references.Result); got != 2 {
		t.Fatalf("case-insensitive references = %d, want 2: %s", got, mustJSONText(t, references.Result))
	}
}

func TestStdioParityUsesStandaloneVBSCompletionCatalog(t *testing.T) {
	client := startStdioTestClient(t)
	defer client.close()

	root := t.TempDir()
	uri := pathToFileURI(filepath.Join(root, "script.vbs"))
	source := `Option Explicit
Dim message
message = CStr(1)
WScript.Sleep(100)
WScript.
message = Response
Application_OnStart
`
	client.request("initialize", map[string]any{
		"processId":    nil,
		"rootUri":      pathToFileURI(root),
		"capabilities": map[string]any{},
	})
	if err := client.notify("textDocument/didOpen", map[string]any{
		"textDocument": map[string]any{
			"uri":        uri,
			"languageId": "vbscript",
			"version":    1,
			"text":       source,
		},
	}); err != nil {
		t.Fatal(err)
	}
	diagnostics := client.waitForNotification("textDocument/publishDiagnostics", uri)

	topLevel := completionLabels(client.request("textDocument/completion", map[string]any{
		"textDocument": map[string]any{"uri": uri},
		"position":     map[string]any{"line": 0, "character": 0},
	}).Result)
	for _, expected := range []string{"WScript", "Err", "CStr", "adInteger"} {
		if !topLevel.contains(expected) {
			t.Fatalf("standalone VBS top-level completions missing %s: %#v", expected, topLevel)
		}
	}
	if topLevel.contains("Response") || topLevel.contains("Application_OnStart") {
		t.Fatalf("standalone VBS top-level completions mismatch: %#v", topLevel)
	}
	members := completionLabels(client.request("textDocument/completion", map[string]any{
		"textDocument": map[string]any{"uri": uri},
		"position":     positionAt(source, strings.Index(source, "WScript.")+len("WScript.")),
	}).Result)
	for _, expected := range []string{"Echo", "Quit", "Sleep", "ScriptFullName"} {
		if !members.contains(expected) {
			t.Fatalf("standalone VBS member completions missing %s: %#v", expected, members)
		}
	}
	assertRequestContains(t, client, "textDocument/hover", map[string]any{
		"textDocument": map[string]any{"uri": uri},
		"position":     positionAt(source, strings.Index(source, "WScript")),
	}, "Dim WScript As WScript")
	assertRequestContains(t, client, "textDocument/hover", map[string]any{
		"textDocument": map[string]any{"uri": uri},
		"position":     positionAt(source, strings.Index(source, "WScript.Sleep")+len("WScript.")),
	}, "WScript.Sleep(milliseconds)")
	signature := mustJSONText(t, client.request("textDocument/signatureHelp", map[string]any{
		"textDocument": map[string]any{"uri": uri},
		"position":     positionAt(source, strings.Index(source, "WScript.Sleep(")+len("WScript.Sleep(")),
	}).Result)
	if !strings.Contains(signature, "WScript.Sleep(milliseconds)") {
		t.Fatalf("standalone VBS WScript signature mismatch: %s", signature)
	}
	diagnosticText := mustJSONText(t, diagnostics.Params)
	if strings.Contains(diagnosticText, "WScript") || strings.Contains(diagnosticText, "CStr") {
		t.Fatalf("standalone VBS diagnostics should not flag WScript/CStr: %s", diagnosticText)
	}
	if !strings.Contains(diagnosticText, "Response") {
		t.Fatalf("standalone VBS diagnostics should flag Response: %s", diagnosticText)
	}
}

func TestStdioParityFlagsWScriptOnlyBuiltInsInClassicASP(t *testing.T) {
	client := startStdioTestClient(t)
	defer client.close()

	root := t.TempDir()
	uri := pathToFileURI(filepath.Join(root, "runtime-separation.asp"))
	source := `<%
Option Explicit
Response.Write "ok"
WScript.Echo "bad"
Response.
%>`
	client.request("initialize", map[string]any{
		"processId":    nil,
		"rootUri":      pathToFileURI(root),
		"capabilities": map[string]any{},
	})
	diagnostics := openClassicASPDocumentWithDiagnostics(t, client, uri, source)
	diagnosticText := mustJSONText(t, diagnostics.Params)
	if !strings.Contains(diagnosticText, "WScript") || strings.Contains(diagnosticText, "Response") {
		t.Fatalf("Classic ASP WScript diagnostics mismatch: %s", diagnosticText)
	}
	topLevel := completionLabels(client.request("textDocument/completion", map[string]any{
		"textDocument": map[string]any{"uri": uri},
		"position":     map[string]any{"line": 1, "character": 0},
	}).Result)
	if !topLevel.contains("Response") || topLevel.contains("WScript") {
		t.Fatalf("Classic ASP top-level completions mismatch: %#v", topLevel)
	}
	responseMembers := completionLabels(client.request("textDocument/completion", map[string]any{
		"textDocument": map[string]any{"uri": uri},
		"position":     positionAt(source, strings.LastIndex(source, "Response.")+len("Response.")),
	}).Result)
	if !responseMembers.contains("Write") {
		t.Fatalf("Classic ASP Response member completions missing Write: %#v", responseMembers)
	}
	hover := mustJSONText(t, client.request("textDocument/hover", map[string]any{
		"textDocument": map[string]any{"uri": uri},
		"position":     positionAt(source, strings.Index(source, "WScript")),
	}).Result)
	if strings.Contains(hover, "Dim WScript As WScript") {
		t.Fatalf("Classic ASP WScript hover should not use standalone catalog: %s", hover)
	}
}

func TestStdioParityTracksClassMembersAndObjectMemberCompletion(t *testing.T) {
	client := startStdioTestClient(t)
	defer client.close()

	root := t.TempDir()
	uri := pathToFileURI(filepath.Join(root, "class-member-completions.asp"))
	source := `<%
Class Customer
  Public Name
  Public Sub Save()
  End Sub
End Class
Dim c
Set c = New Customer
c.
c.Na
With c
  .Sa
End With
%>`
	client.request("initialize", map[string]any{
		"processId":    nil,
		"rootUri":      pathToFileURI(root),
		"capabilities": map[string]any{},
	})
	notifyOpenClassicASPDocument(t, client, uri, source)
	waitForDiagnosticsContaining(t, client, "")

	for _, testCase := range []struct {
		name     string
		needle   string
		expected []string
	}{
		{name: "object", needle: "c.", expected: []string{"Name", "Save"}},
		{name: "partial-object", needle: "c.Na", expected: []string{"Name", "Save"}},
		{name: "with-member", needle: ".Sa", expected: []string{"Save"}},
	} {
		completions := completionLabels(client.request("textDocument/completion", map[string]any{
			"textDocument": map[string]any{"uri": uri},
			"position":     positionAt(source, strings.Index(source, testCase.needle)+len(testCase.needle)),
		}).Result)
		for _, expected := range testCase.expected {
			if !completions.contains(expected) {
				t.Fatalf("%s completions missing %s: %#v", testCase.name, expected, completions)
			}
		}
	}
}

func TestStdioParityKeepsVBScriptMemberCompletionsReceiverRelevant(t *testing.T) {
	client := startStdioTestClient(t)
	defer client.close()

	root := t.TempDir()
	uri := pathToFileURI(filepath.Join(root, "member-relevance.asp"))
	source := `<%
Class Wanted
  Public WantedMember
End Class
Class Unrelated
  Public UnrelatedMember
End Class
Dim globalValue
Function GlobalFunction()
End Function
Sub First()
  Dim value
  Set value = New Wanted
  value.
  value .
  value.Wa
End Sub
Sub Second()
  Dim value
  Set value = New Unrelated
End Sub
unknownReceiver.
unknownReceiver .
unknownReceiver.Na
%>`
	client.request("initialize", map[string]any{
		"processId":    nil,
		"rootUri":      pathToFileURI(root),
		"capabilities": map[string]any{},
	})
	notifyOpenClassicASPDocument(t, client, uri, source)
	waitForDiagnosticsContaining(t, client, "")

	for _, needle := range []string{"value.\n", "value .\n", "value.Wa"} {
		positionOffset := strings.Index(source, needle) + len(strings.TrimSuffix(needle, "\n"))
		completions := completionLabels(client.request("textDocument/completion", map[string]any{
			"textDocument": map[string]any{"uri": uri},
			"position":     positionAt(source, positionOffset),
		}).Result)
		if !completions.contains("WantedMember") {
			t.Fatalf("%q member completions missing WantedMember: %#v", needle, completions)
		}
		for _, forbidden := range []string{"UnrelatedMember", "globalValue", "GlobalFunction"} {
			if completions.contains(forbidden) {
				t.Fatalf("%q member completions leaked %s: %#v", needle, forbidden, completions)
			}
		}
	}

	for _, needle := range []string{"unknownReceiver.\n", "unknownReceiver .\n", "unknownReceiver.Na"} {
		positionOffset := strings.Index(source, needle) + len(strings.TrimSuffix(needle, "\n"))
		completions := completionLabels(client.request("textDocument/completion", map[string]any{
			"textDocument": map[string]any{"uri": uri},
			"position":     positionAt(source, positionOffset),
		}).Result)
		if len(completions) != 0 {
			t.Fatalf("%q unknown receiver completions = %#v, want none", needle, completions)
		}
	}
}

func TestStdioParityResolvesWithAndIncludeClassMemberCompletions(t *testing.T) {
	client := startStdioTestClient(t)
	defer client.close()

	root := t.TempDir()
	if err := os.WriteFile(filepath.Join(root, "included-customer.inc"), []byte(`<%
Class IncludedCustomer
  Public IncludedName
End Class
%>`), 0o644); err != nil {
		t.Fatal(err)
	}
	uri := pathToFileURI(filepath.Join(root, "with-member-relevance.asp"))
	source := `<!-- #include file="included-customer.inc" -->
<%
Class OuterThing
  Public OuterName
End Class
Class InnerThing
  Public InnerName
End Class
Dim unrelatedGlobal
Sub Render()
  Dim outerValue, innerValue, includedValue
  Set outerValue = New OuterThing
  Set innerValue = New InnerThing
  Set includedValue = New IncludedCustomer
  With outerValue
    .Outer
    With innerValue
      .Inner
    End With
    .OuterName
  End With
  includedValue.Inc
  With includedValue
    .Included
  End With
  With unknownValue
    .Missing
  End With
End Sub
%>`
	client.request("initialize", map[string]any{
		"processId":    nil,
		"rootUri":      pathToFileURI(root),
		"capabilities": map[string]any{},
	})
	notifyOpenClassicASPDocument(t, client, uri, source)
	waitForDiagnosticsContaining(t, client, "")

	testCases := []struct {
		needle    string
		expected  string
		forbidden []string
	}{
		{needle: ".Outer\n", expected: "OuterName", forbidden: []string{"InnerName", "IncludedName", "unrelatedGlobal"}},
		{needle: ".Inner\n", expected: "InnerName", forbidden: []string{"OuterName", "IncludedName", "unrelatedGlobal"}},
		{needle: ".OuterName", expected: "OuterName", forbidden: []string{"InnerName", "IncludedName", "unrelatedGlobal"}},
		{needle: "includedValue.Inc", expected: "IncludedName", forbidden: []string{"OuterName", "InnerName", "unrelatedGlobal"}},
		{needle: ".Included\n", expected: "IncludedName", forbidden: []string{"OuterName", "InnerName", "unrelatedGlobal"}},
	}
	for _, testCase := range testCases {
		positionOffset := strings.Index(source, testCase.needle) + len(strings.TrimSuffix(testCase.needle, "\n"))
		completions := completionLabels(client.request("textDocument/completion", map[string]any{
			"textDocument": map[string]any{"uri": uri},
			"position":     positionAt(source, positionOffset),
		}).Result)
		if !completions.contains(testCase.expected) {
			t.Fatalf("%q completions missing %s: %#v", testCase.needle, testCase.expected, completions)
		}
		for _, forbidden := range testCase.forbidden {
			if completions.contains(forbidden) {
				t.Fatalf("%q completions leaked %s: %#v", testCase.needle, forbidden, completions)
			}
		}
	}

	unknownOffset := strings.Index(source, ".Missing") + len(".Missing")
	unknown := completionLabels(client.request("textDocument/completion", map[string]any{
		"textDocument": map[string]any{"uri": uri},
		"position":     positionAt(source, unknownOffset),
	}).Result)
	if len(unknown) != 0 {
		t.Fatalf("unknown With receiver completions = %#v, want none", unknown)
	}
}

func TestStdioParityTracksCommonVBScriptStatementsAndConservativeExternalCOMMembers(t *testing.T) {
	client := startStdioTestClient(t)
	defer client.close()

	root := t.TempDir()
	uri := pathToFileURI(filepath.Join(root, "common-vbscript-statements.asp"))
	source := `<%
Class Customer
  Public Name
End Class
Dim c
Set c = New Customer
With c
  .
End With
ReDim items(10)
For Each item In items
Next
Dim rs
Set rs = Server.CreateObject("ADODB.Recordset")
rs.
Function BuildName(firstName, lastName)
End Function
Response.Write BuildName("Ada", "Lovelace")
%>`
	client.request("initialize", map[string]any{
		"processId":    nil,
		"rootUri":      pathToFileURI(root),
		"capabilities": map[string]any{},
	})
	notifyOpenClassicASPDocument(t, client, uri, source)
	waitForDiagnosticsContaining(t, client, "")

	withCompletions := completionLabels(client.request("textDocument/completion", map[string]any{
		"textDocument": map[string]any{"uri": uri},
		"position":     positionAt(source, strings.Index(source, ".\nEnd With")+1),
	}).Result)
	if !withCompletions.contains("Name") {
		t.Fatalf("With member completions missing Name: %#v", withCompletions)
	}

	adoCompletions := completionLabels(client.request("textDocument/completion", map[string]any{
		"textDocument": map[string]any{"uri": uri},
		"position":     positionAt(source, strings.Index(source, "rs.")+len("rs.")),
	}).Result)
	if !adoCompletions.contains("MoveNext") {
		t.Fatalf("ADODB Recordset completions missing MoveNext: %#v", adoCompletions)
	}

	signature := mustJSONText(t, client.request("textDocument/signatureHelp", map[string]any{
		"textDocument": map[string]any{"uri": uri},
		"position":     positionAt(source, strings.Index(source, `"Lovelace"`)+len(`"Lov`)),
	}).Result)
	if !strings.Contains(signature, `"activeParameter":1`) {
		t.Fatalf("BuildName signature active parameter mismatch: %s", signature)
	}
}

func TestStdioParityKeepsLocalVBScriptVariablesScopedToProcedure(t *testing.T) {
	client := startStdioTestClient(t)
	defer client.close()

	root := t.TempDir()
	uri := pathToFileURI(filepath.Join(root, "local-scope-completions.asp"))
	source := `<%
Sub First()
  Dim firstOnly
  fir
End Sub
Sub Second()
  fir
End Sub
%>`
	client.request("initialize", map[string]any{
		"processId":    nil,
		"rootUri":      pathToFileURI(root),
		"capabilities": map[string]any{},
	})
	notifyOpenClassicASPDocument(t, client, uri, source)
	waitForDiagnosticsContaining(t, client, "")

	firstCompletions := completionLabels(client.request("textDocument/completion", map[string]any{
		"textDocument": map[string]any{"uri": uri},
		"position":     positionAt(source, strings.Index(source, "\n  fir")+len("\n  fir")),
	}).Result)
	if !firstCompletions.contains("firstOnly") {
		t.Fatalf("local variable missing inside declaring procedure: %#v", firstCompletions)
	}
	secondOffset := strings.LastIndex(source, "fir") + len("fir")
	secondCompletions := completionLabels(client.request("textDocument/completion", map[string]any{
		"textDocument": map[string]any{"uri": uri},
		"position":     positionAt(source, secondOffset),
	}).Result)
	if secondCompletions.contains("firstOnly") {
		t.Fatalf("local variable from sibling procedure leaked into completions: %#v", secondCompletions)
	}
}
