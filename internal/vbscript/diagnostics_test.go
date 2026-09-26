package vbscript

import (
	"reflect"
	"sync"
	"testing"

	"github.com/yottonoko/classic-web-system-lsp/internal/core"
	"github.com/yottonoko/classic-web-system-lsp/internal/lsp"
)

func TestSyntaxDiagnosticsReportIfErrorsByStrictness(t *testing.T) {
	parsed := core.ParseDocument("file:///site/default.asp", `<%
If ready
ElseIf ready
If Then
If (ready Then
If openBlock Then
  Response.Write openBlock
If outer Then
  If inner Then
    Response.Write inner
%>`, core.Settings{DefaultLanguage: "VBScript"})

	basicCodes := diagnosticCodes(SyntaxDiagnostics(parsed, SyntaxOptions{IfSyntaxDiagnostics: "basic"}))
	wantBasic := []string{
		"missingThen",
		"missingThen",
		"missingIfCondition",
		"invalidIfCondition",
		"missingEndIf",
		"missingEndIf",
		"missingEndIf",
	}
	if !reflect.DeepEqual(basicCodes, wantBasic) {
		t.Fatalf("basic If syntax codes = %#v, want %#v", basicCodes, wantBasic)
	}
	for _, diagnostic := range SyntaxDiagnostics(parsed, SyntaxOptions{IfSyntaxDiagnostics: "basic"}) {
		if diagnostic.Severity != lsp.DiagnosticSeverityError {
			t.Fatalf("diagnostic severity = %#v, want error: %#v", diagnostic.Severity, diagnostic)
		}
	}

	strictParsed := core.ParseDocument("file:///site/strict-if.asp", `<%
If value = Then
%>`, core.Settings{DefaultLanguage: "VBScript"})
	if codes := diagnosticCodes(SyntaxDiagnostics(strictParsed, SyntaxOptions{IfSyntaxDiagnostics: "basic"})); containsCode(codes, "invalidIfCondition") {
		t.Fatalf("basic If syntax should not report invalidIfCondition: %#v", codes)
	}
	if codes := diagnosticCodes(SyntaxDiagnostics(strictParsed, SyntaxOptions{IfSyntaxDiagnostics: "strict"})); !reflect.DeepEqual(codes, []string{"invalidIfCondition"}) {
		t.Fatalf("strict If syntax codes = %#v, want [invalidIfCondition]", codes)
	}
	if codes := diagnosticCodes(SyntaxDiagnostics(parsed, SyntaxOptions{IfSyntaxDiagnostics: "off"})); containsAnyCode(codes, "missingThen", "missingIfCondition", "invalidIfCondition", "missingEndIf") {
		t.Fatalf("off If syntax diagnostics reported If codes: %#v", codes)
	}
}

func TestSyntaxDiagnosticsReportMissingBlockTerminators(t *testing.T) {
	parsed := core.ParseDocument("file:///site/missing-block-ends.asp", `<%
Sub MissingSub()
Function MissingFunction()
Class MissingClass
Property Get MissingProperty()
Select Case value
With obj
Do
While ready
For index = 0 To 1
For Each item In items
%>`, core.Settings{DefaultLanguage: "VBScript"})
	codes := diagnosticCodes(SyntaxDiagnostics(parsed, SyntaxOptions{}))
	want := []string{
		"missingEndSub",
		"missingEndFunction",
		"missingEndClass",
		"missingEndProperty",
		"missingEndSelect",
		"missingEndWith",
		"missingLoop",
		"missingWend",
		"missingNext",
		"missingNext",
	}
	if !reflect.DeepEqual(codes, want) {
		t.Fatalf("missing block codes = %#v, want %#v", codes, want)
	}

	valid := core.ParseDocument("file:///site/closed-blocks.asp", `<%
Sub ClosedSub()
End Sub
Function ClosedFunction()
End Function
Class ClosedClass
  Property Get Name()
  End Property
End Class
Select Case value
End Select
With obj
End With
Do
Loop
While ready
Wend
For index = 0 To 1
Next
For Each item In items
Next
%>`, core.Settings{DefaultLanguage: "VBScript"})
	if codes := diagnosticCodes(SyntaxDiagnostics(valid, SyntaxOptions{})); containsAnyCode(codes, "missingEndSub", "missingEndFunction", "missingEndClass", "missingEndProperty", "missingEndSelect", "missingEndWith", "missingLoop", "missingWend", "missingNext") {
		t.Fatalf("valid blocks reported missing terminators: %#v", codes)
	}
}

func TestSyntaxDiagnosticsKeepIslandSpanningBlockTerminators(t *testing.T) {
	standaloneEnd := core.ParseDocument("file:///site/island-standalone-end.asp", `<%
Sub Render()
%>
<p>body</p>
<% end %>
<%
Function BuildTitle()
%>
<%= "title" %>
<% End %>`, core.Settings{DefaultLanguage: "VBScript"})
	if codes := diagnosticCodes(SyntaxDiagnostics(standaloneEnd, SyntaxOptions{})); !containsAnyCode(codes, "missingEndSub") || !containsAnyCode(codes, "missingEndFunction") {
		t.Fatalf("standalone End should not close procedure blocks: %#v", codes)
	}

	explicitEnds := core.ParseDocument("file:///site/island-block-ends.asp", `<%
Sub Render()
%>
<p>body</p>
<% End Sub %>
<%
Function BuildTitle()
%>
<%= "title" %>
<% End Function %>
<%
If ready Then
%>
<span>ready</span>
<% Else %>
<span>fallback</span>
<% End If %>`, core.Settings{DefaultLanguage: "VBScript"})
	if codes := diagnosticCodes(SyntaxDiagnostics(explicitEnds, SyntaxOptions{IfSyntaxDiagnostics: "strict"})); containsAnyCode(codes, "missingEndSub", "missingEndFunction", "missingEndIf") {
		t.Fatalf("explicit island terminators reported missing blocks: %#v", codes)
	}
}

func TestSyntaxDiagnosticsMergeAdjacentIslandIfThenHeadersOnlyWithoutTemplateContent(t *testing.T) {
	adjacentThen := core.ParseDocument("file:///site/island-if-then.asp", `<%
If ready
%><% Then %>
<span>ready</span>
<% ElseIf fallback %><% Then %>
<span>fallback</span>
<% End If %>`, core.Settings{DefaultLanguage: "VBScript"})
	if codes := diagnosticCodes(SyntaxDiagnostics(adjacentThen, SyntaxOptions{IfSyntaxDiagnostics: "strict"})); len(codes) != 0 {
		t.Fatalf("adjacent island If/Then should be valid: %#v", codes)
	}

	templateThen := core.ParseDocument("file:///site/island-if-template-then.asp", `<%
If ready
%><span>not gated</span><% Then %>
<span>ready</span>
<% End If %>`, core.Settings{DefaultLanguage: "VBScript"})
	if codes := diagnosticCodes(SyntaxDiagnostics(templateThen, SyntaxOptions{IfSyntaxDiagnostics: "strict"})); !reflect.DeepEqual(codes, []string{"missingThen"}) {
		t.Fatalf("template-separated If/Then codes = %#v, want [missingThen]", codes)
	}
}

func TestSyntaxDiagnosticsKeepIslandSpanningForBlocksValid(t *testing.T) {
	parsed := core.ParseDocument("file:///site/island-for-blocks.asp", `<%
For index = 0 To 1
%>
<span><%= index %></span>
<% Next %>
<%
For Each item In items
%>
<span><%= item %></span>
<% Next %>`, core.Settings{DefaultLanguage: "VBScript"})
	if codes := diagnosticCodes(SyntaxDiagnostics(parsed, SyntaxOptions{})); containsAnyCode(codes, "missingNext") {
		t.Fatalf("island-spanning For blocks reported missing Next: %#v", codes)
	}
}

func TestSyntaxDiagnosticsReportInvalidOnErrorStatements(t *testing.T) {
	parsed := core.ParseDocument("file:///site/on-error-syntax.asp", `<%
On Error Resume Next
On Error GoTo 0
On Error Resume
On Error GoTo
On Error GoTo label
On Error GoTo -1
On Error Next
%>`, core.Settings{DefaultLanguage: "VBScript"})
	codes := diagnosticCodes(SyntaxDiagnostics(parsed, SyntaxOptions{}))
	want := []string{
		"invalidOnErrorStatement",
		"invalidOnErrorStatement",
		"invalidOnErrorStatement",
		"invalidOnErrorStatement",
		"invalidOnErrorStatement",
	}
	if !reflect.DeepEqual(codes, want) {
		t.Fatalf("On Error syntax codes = %#v, want %#v", codes, want)
	}
	for _, diagnostic := range SyntaxDiagnostics(parsed, SyntaxOptions{}) {
		if diagnostic.Severity != lsp.DiagnosticSeverityError {
			t.Fatalf("diagnostic severity = %#v, want error: %#v", diagnostic.Severity, diagnostic)
		}
	}
}

func diagnosticCodes(diagnostics []lsp.Diagnostic) []string {
	codes := make([]string, 0, len(diagnostics))
	for _, diagnostic := range diagnostics {
		if diagnostic.Source == "asp-lsp-vbscript-syntax" {
			code, ok := diagnostic.Code.(string)
			if !ok {
				continue
			}
			codes = append(codes, code)
		}
	}
	return codes
}

func containsCode(codes []string, expected string) bool {
	for _, code := range codes {
		if code == expected {
			return true
		}
	}
	return false
}

func TestVBStatementsShareRevisionSplit(t *testing.T) {
	const source = "<% Dim firstValue %>\nfirstValue = 1\n<% If firstValue Then %><% Then %>\n<% End If %>\n"
	parsed := core.ParseDocument("file:///shared-statements.asp", source, core.Settings{DefaultLanguage: "VBScript"})
	first := vbStatements(parsed)
	if len(first) == 0 {
		t.Fatal("expected statements")
	}
	if _, ok := parsed.LoadRuntimeAnalysis(vbStatementsAnalysisKey); !ok {
		t.Fatal("statement split was not cached for the revision")
	}
	second := vbStatements(parsed)
	if !reflect.DeepEqual(first, second) {
		t.Fatal("cached statement split diverged")
	}
	if &first[0] != &second[0] {
		t.Fatal("cached statement split did not reuse its backing slice")
	}
	fresh := core.ParseDocument(parsed.URI, source, core.Settings{DefaultLanguage: "VBScript"})
	if got := vbStatements(fresh); !reflect.DeepEqual(first, got) {
		t.Fatalf("shared statement split differs from fresh revision: got %#v want %#v", first, got)
	}
	coldParsed := core.ParseDocument("file:///cold-shared-statements.asp", source, core.Settings{DefaultLanguage: "VBScript"})
	var workers sync.WaitGroup
	results := make([][]vbStatement, 8)
	workers.Add(8)
	for index := range results {
		go func() {
			defer workers.Done()
			results[index] = vbStatements(coldParsed)
		}()
	}
	workers.Wait()
	coldCanonical := results[0]
	if len(coldCanonical) == 0 {
		t.Fatal("cold concurrent statement split was empty")
	}
	for _, got := range results[1:] {
		if len(got) == 0 || &got[0] != &coldCanonical[0] {
			t.Fatalf("cold concurrent statement split diverged")
		}
	}
}

func containsAnyCode(codes []string, expected ...string) bool {
	for _, code := range codes {
		for _, candidate := range expected {
			if code == candidate {
				return true
			}
		}
	}
	return false
}
