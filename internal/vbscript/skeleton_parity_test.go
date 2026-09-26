package vbscript

import (
	"testing"

	"github.com/yottonoko/classic-web-system-lsp/internal/core"
)

func TestSkeletonParityUsesEagerVBScriptSymbolsWithoutHydration(t *testing.T) {
	source := `<%
Dim Greeting
Greeting = "hello"

Function BuildTitle(name)
  BuildTitle = "Hello " & name
End Function
%>`
	parsed := core.ParseDocument("file:///site/skeleton.asp", source, core.Settings{DefaultLanguage: "VBScript"})
	index := BuildSymbolIndex(parsed)

	if symbol, ok := index.Declarations["greeting"]; !ok || symbol.Kind != "variable" {
		t.Fatalf("Greeting symbol = %#v, ok=%v", symbol, ok)
	}
	if symbol, ok := index.Declarations["buildtitle"]; !ok || symbol.Kind != "function" {
		t.Fatalf("BuildTitle symbol = %#v, ok=%v", symbol, ok)
	}
	if symbols := WorkspaceSymbols(parsed, "BuildTitle"); len(symbols) != 1 || symbols[0].Name != "BuildTitle" {
		t.Fatalf("workspace symbols = %#v", symbols)
	}
}
