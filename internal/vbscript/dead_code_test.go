package vbscript

import (
	"strings"
	"testing"

	"github.com/yottonoko/classic-web-system-lsp/internal/core"
	"github.com/yottonoko/classic-web-system-lsp/internal/lsp"
)

func TestDeadCodeDiagnosticsReportUnreachableStatementsAsUnnecessaryHints(t *testing.T) {
	source := `<%
Sub StopSub()
  Exit Sub
  Response.Write "after-sub"
End Sub
Function StopFunction()
  Exit Function
  StopFunction = 1
End Function
Sub StopLoops()
  For i = 0 To 1
    Exit For
    Response.Write "after-for"
  Next
  Do
    Exit Do
    Response.Write "after-do"
  Loop
  Response.Write "after-loops"
End Sub
End
If flag Then
  Response.Write "dead-if-body"
End If
For i = 0 To 1
  Response.Write "dead-for-body"
Next
Response.Write "after-end"
%>`
	parsed := core.ParseDocument("file:///site/default.asp", source, core.Settings{})
	diagnostics := DeadCodeDiagnostics(parsed)
	deadLines := diagnosticLines(source, diagnostics)
	for _, expected := range []string{
		`Response.Write "after-sub"`,
		"End Sub",
		"StopFunction = 1",
		"End Function",
		`Response.Write "after-for"`,
		"Next",
		`Response.Write "after-do"`,
		"Loop",
		`Response.Write "dead-if-body"`,
		"End If",
		`Response.Write "dead-for-body"`,
		`Response.Write "after-end"`,
	} {
		if !containsDeadCodeLine(deadLines, expected) {
			t.Fatalf("dead diagnostics missing %q in %#v", expected, deadLines)
		}
	}
	if containsDeadCodeLine(deadLines, `Response.Write "after-loops"`) {
		t.Fatalf("after-loop statement should be reachable: %#v", deadLines)
	}
	for _, diagnostic := range diagnostics {
		if diagnostic.Severity != lsp.DiagnosticSeverityHint || !diagnosticHasTag(diagnostic, lsp.DiagnosticTagUnnecessary) {
			t.Fatalf("dead diagnostic should be unnecessary hint: %#v", diagnostic)
		}
	}
}

func TestDeadCodeDiagnosticsKeepConditionalExitsAndBlockTerminatorsReachable(t *testing.T) {
	source := `<%
Sub ConditionalExit(flag, values)
  If flag Then Exit Sub
  Response.Write "after-single-line-if"
  If flag Then
    Exit Sub
  Else
    Response.Write "reachable-else"
  End If
  Response.Write "after-if"
  For Each value In values
    If value Then
      Exit For
    End If
    Response.Write "after-conditional-exit-for"
  Next
  Response.Write "after-loop"
End Sub
%>`
	parsed := core.ParseDocument("file:///site/default.asp", source, core.Settings{})
	if diagnostics := DeadCodeDiagnostics(parsed); len(diagnostics) != 0 {
		t.Fatalf("conditional exits should not report dead code: %#v", diagnostics)
	}
}

func diagnosticLines(source string, diagnostics []lsp.Diagnostic) []string {
	lines := strings.Split(source, "\n")
	values := make([]string, 0, len(diagnostics))
	for _, diagnostic := range diagnostics {
		if diagnostic.Range.Start.Line >= 0 && diagnostic.Range.Start.Line < len(lines) {
			values = append(values, strings.TrimSpace(lines[diagnostic.Range.Start.Line]))
		}
	}
	return values
}

func containsDeadCodeLine(values []string, needle string) bool {
	for _, value := range values {
		if value == needle {
			return true
		}
	}
	return false
}

func diagnosticHasTag(diagnostic lsp.Diagnostic, tag lsp.DiagnosticTag) bool {
	for _, candidate := range diagnostic.Tags {
		if candidate == tag {
			return true
		}
	}
	return false
}
