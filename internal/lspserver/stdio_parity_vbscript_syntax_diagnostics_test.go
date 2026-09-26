package lspserver

import (
	"path/filepath"
	"strings"
	"testing"
)

func TestStdioParityReportsCoreVBScriptSyntaxDiagnostics(t *testing.T) {
	cases := []struct {
		name     string
		source   string
		expected []string
	}{
		{
			name: "variable-declarations",
			source: `<%
Dim initialized = 1
Dim first, second = 2
Public publicValue = 3
Private privateValue = 4
ReDim resized = 5
Dim typed As Integer
Public publicTyped As String
Private privateTyped As Object
%>`,
			expected: []string{"initializers", "As types"},
		},
		{
			name: "procedure-calls",
			source: `<%
Function Func1(hoge)
  Func1 = hoge
End Function
Sub Func2(hoge, fuga)
End Sub
Call Func1 hoge
Z = Func1 hoge
Func2(hoge, fuga)
Call Func2 hoge, fuga
Z = Func2 hoge, fuga
%>`,
			expected: []string{"call syntax", "Func1", "Func2"},
		},
	}
	for _, testCase := range cases {
		t.Run(testCase.name, func(t *testing.T) {
			client := startStdioTestClient(t)
			defer client.close()

			root := t.TempDir()
			uri := pathToFileURI(filepath.Join(root, testCase.name+".asp"))
			client.request("initialize", map[string]any{
				"processId":    nil,
				"rootUri":      pathToFileURI(root),
				"capabilities": map[string]any{},
			})
			diagnostics := openClassicASPDocumentWithDiagnostics(t, client, uri, testCase.source)
			syntaxText := mustJSONText(t, diagnosticsFromSource(t, diagnostics, "asp-lsp-vbscript-syntax"))
			for _, expected := range testCase.expected {
				if !strings.Contains(syntaxText, expected) {
					t.Fatalf("%s diagnostics missing %q: %s", testCase.name, expected, syntaxText)
				}
			}
		})
	}
}

func TestStdioParityKeepsValidCoreVBScriptSyntaxOutOfDiagnostics(t *testing.T) {
	cases := []struct {
		name       string
		source     string
		unexpected []string
	}{
		{
			name: "variable-declarations",
			source: `<%
Dim value
value = 1
Dim assigned : assigned = 1
Dim fixedItems(10)
Dim dynamicItems()
ReDim fixedItems(20)
ReDim Preserve fixedItems(30)
Const knownValue = 1
%>`,
			unexpected: []string{"initializedDeclaration", "typedDeclaration"},
		},
		{
			name: "parenthesized-expression-output",
			source: `<%
Function RenderCustomerRows(ByVal customerList, ByVal activeCustomerId)
  RenderCustomerRows = ""
End Function
%>
<%= RenderCustomerRows(filteredCustomers, selectedCustomerId) %>`,
			unexpected: []string{"statementCallDisallowsParenthesizedArguments"},
		},
		{
			name: "procedure-calls",
			source: `<%
Function Func1(hoge)
  Func1 = hoge
End Function
Sub Func2(hoge, fuga)
End Sub
Call Func1(hoge)
Func1 hoge
Z = Func1(hoge)
Func2 hoge, fuga
Call Func2(hoge, fuga)
Response.Write("x")
%>`,
			unexpected: []string{
				"callStatementRequiresParentheses",
				"expressionCallRequiresParentheses",
				"statementCallDisallowsParenthesizedArguments",
			},
		},
		{
			name: "if-syntax",
			source: `<%
If ready Then Response.Write "ok"
If ready Then: Response.Write "ok"
If ready Then _
  Response.Write "continued"
If first _
  And second _
  And third Then
  Response.Write "continued condition"
End If
If ready Then
  Response.Write "ready"
ElseIf other Then
  Response.Write "other"
End If
%>`,
			unexpected: []string{"missingThen", "missingIfCondition", "invalidIfCondition", "missingEndIf"},
		},
		{
			name: "function-return-self-reference",
			source: `<%
Option Explicit
Function A(v)
  A = ""
  A = A & v
End Function
%>`,
			unexpected: []string{"not declared", "declared but never used", "missingEndFunction"},
		},
		{
			name: "function-return-recursion-and-exit-keywords",
			source: `<%
Option Explicit
Function Factorial(ByVal n)
  If n <= 1 Then
    Factorial = 1
    Exit Function
  End If
  Factorial = n * Factorial(n - 1)
  Factorial = Factorial + 0
End Function
Sub StopEarly()
  Dim done, index
  Do Until done
    Exit Do
  Loop
  For index = 0 To 1
    Exit For
  Next
  Erase done
  Stop
  Exit Sub
End Sub
Response.Write Factorial(5)
%>`,
			unexpected: []string{
				"'Exit' is not declared",
				"'Until' is not declared",
				"'Erase' is not declared",
				"'Stop' is not declared",
				"'Factorial' is not declared",
				"Parameter 'n' is never used",
				"missingEndFunction",
				"missingEndSub",
			},
		},
		{
			name: "default-property-keyword",
			source: `<%
Option Explicit
Class Customer
  Private displayName
  Public Default Property Get Name()
    Name = displayName
  End Property
End Class
%>`,
			unexpected: []string{
				"'Default' is not declared",
				"'Name' is not declared",
				"missingEndProperty",
				"missingEndClass",
			},
		},
		{
			name: "colon-separated-statements",
			source: `<%
Dim a : a = 1
Const c = 1 : Response.Write c
Sub Main() : Dim x : x = 1 : End Sub
Function Value() : Value = 1 : End Function
If a = 1 Then Response.Write a : Response.Write "ok"
Select Case a : Case 1 : Response.Write a : End Select
For i = 0 To 1 : Response.Write i : Next
With Response : .Write "x" : End With
%>`,
			unexpected: []string{
				"initializedDeclaration",
				"typedDeclaration",
				"missingEndSub",
				"missingEndFunction",
				"missingEndIf",
				"missingEndSelect",
				"missingNext",
				"missingEndWith",
			},
		},
	}
	for _, testCase := range cases {
		t.Run(testCase.name, func(t *testing.T) {
			client := startStdioTestClient(t)
			defer client.close()

			root := t.TempDir()
			uri := pathToFileURI(filepath.Join(root, testCase.name+".asp"))
			client.request("initialize", map[string]any{
				"processId":    nil,
				"rootUri":      pathToFileURI(root),
				"capabilities": map[string]any{},
			})
			diagnostics := openClassicASPDocumentWithDiagnostics(t, client, uri, testCase.source)
			diagnosticsText := string(diagnostics.Params)
			for _, unexpected := range testCase.unexpected {
				if strings.Contains(diagnosticsText, unexpected) {
					t.Fatalf("%s diagnostics unexpectedly included %q: %s", testCase.name, unexpected, diagnosticsText)
				}
			}
		})
	}
}
