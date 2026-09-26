package lspserver

import (
	"testing"

	"github.com/yottonoko/classic-web-system-lsp/internal/core"
)

func TestVBScriptUndeclaredDiagnosticsSkipHexAndOctalLiteralFragments(t *testing.T) {
	source := `<%
Option Explicit
Dim hexValue, lowerHex, octValue, explicitOctal, zeroOctal
hexValue = &HFF
lowerHex = &ha
octValue = &077
explicitOctal = &O10
zeroOctal = &0
%>`
	parsed := core.ParseDocument("file:///site/numeric-literals.asp", source, core.Settings{DefaultLanguage: "VBScript"})
	diagnostics := undeclaredDiagnosticMessages(parsed)

	for _, unexpected := range []string{"HFF", "ha", "O10"} {
		if containsDiagnosticFor(diagnostics, unexpected) {
			t.Fatalf("numeric literal fragment %s should not produce diagnostics: %#v", unexpected, diagnostics)
		}
	}
}
