package lspserver

import (
	"strings"
	"testing"

	"github.com/yottonoko/classic-web-system-lsp/internal/core"
	"github.com/yottonoko/classic-web-system-lsp/internal/vbscript"
)

func TestVBScriptBuiltInADOConstantsDoNotBecomeImplicitGlobals(t *testing.T) {
	source := `<% Option Explicit
Dim fieldType
fieldType = adInteger
Name = "Ada"
%>`
	parsed := core.ParseDocument("file:///site/default.asp", source, core.Settings{DefaultLanguage: "VBScript"})
	diagnostics := undeclaredDiagnosticMessages(parsed)

	if containsDiagnosticFor(diagnostics, "adInteger") {
		t.Fatalf("adInteger should be a built-in ADO constant, got diagnostics %#v", diagnostics)
	}
	if !containsDiagnosticFor(diagnostics, "Name") {
		t.Fatalf("Name should remain undeclared, got diagnostics %#v", diagnostics)
	}
	if !hasCompletionLabel(vbscript.CompletionsForOptions(parsed.Text, strings.Index(parsed.Text, "adInteger"), vbscript.CompletionOptions{}), "adInteger") {
		t.Fatalf("VBScript completions missing adInteger")
	}
}

func TestVBScriptDeclarationFreeConstantsStayBuiltIn(t *testing.T) {
	source := `<% Option Explicit
Dim lineBreak, compareMode, promptStyle, excluded
lineBreak = vbCrLf
compareMode = vbTextCompare
promptStyle = vbOKOnly
excluded = vbOK
%>`
	parsed := core.ParseDocument("file:///site/default.asp", source, core.Settings{DefaultLanguage: "VBScript"})
	diagnostics := undeclaredDiagnosticMessages(parsed)

	for _, name := range []string{"vbCrLf", "vbTextCompare", "vbOKOnly"} {
		if containsDiagnosticFor(diagnostics, name) {
			t.Fatalf("%s should be a built-in VBScript constant, got diagnostics %#v", name, diagnostics)
		}
		if !hasCompletionLabel(vbscript.CompletionsForOptions(parsed.Text, strings.Index(parsed.Text, name), vbscript.CompletionOptions{}), name) {
			t.Fatalf("VBScript completions missing %s", name)
		}
	}
	if !containsDiagnosticFor(diagnostics, "vbOK") {
		t.Fatalf("vbOK should remain undeclared, got diagnostics %#v", diagnostics)
	}
}

func undeclaredDiagnosticMessages(parsed *core.ParsedDocument) []string {
	usage := collectVBUsageDeclarations(parsed)
	diagnostics := undeclaredVBScriptDiagnostics(parsed, usage, "en", nil)
	messages := make([]string, 0, len(diagnostics))
	for _, diagnostic := range diagnostics {
		messages = append(messages, diagnostic.Message)
	}
	return messages
}

func containsDiagnosticFor(messages []string, name string) bool {
	for _, message := range messages {
		if strings.Contains(message, "'"+name+"'") {
			return true
		}
	}
	return false
}
