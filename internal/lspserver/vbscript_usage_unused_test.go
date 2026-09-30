package lspserver

import (
	"fmt"
	"strings"
	"testing"

	"github.com/yottonoko/classic-web-system-lsp/internal/core"
)

func TestUnusedVBScriptDiagnosticsWithManySameNamedLocals(t *testing.T) {
	var source strings.Builder
	source.WriteString("<%\n")
	const procedures = 400
	for index := range procedures {
		fmt.Fprintf(&source, "Sub S%d(value, unusedParam)\n  Dim local\n  local = value\nEnd Sub\n", index)
	}
	source.WriteString("%>")
	parsed := core.ParseDocument("file:///unused.asp", source.String(), core.Settings{DefaultLanguage: "VBScript"})
	usage := collectVBUsageDeclarations(parsed)
	unused := map[string]int{}
	for _, diagnostic := range unusedVBScriptDiagnostics(parsed, usage) {
		data, _ := diagnostic.Data.(map[string]any)
		name, _ := data["name"].(string)
		unused[name]++
	}
	if unused["unusedParam"] != procedures || len(unused) != 1 {
		t.Fatalf("unused diagnostics = %v, want only unusedParam x%d", unused, procedures)
	}
}
