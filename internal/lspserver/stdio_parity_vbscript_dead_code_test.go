package lspserver

import (
	"path/filepath"
	"strings"
	"testing"
)

func TestStdioParityReportsAndConfiguresVBScriptDeadCodeDiagnostics(t *testing.T) {
	client := startStdioTestClient(t)
	defer client.close()

	root := t.TempDir()
	uri := pathToFileURI(filepath.Join(root, "go-vbscript-dead-code.asp"))
	source := `<%
Sub StopSub()
  Dim unusedValue
  Exit Sub
  Response.Write "after-sub"
End Sub
%>`
	client.request("initialize", map[string]any{
		"processId":    nil,
		"rootUri":      pathToFileURI(root),
		"capabilities": map[string]any{},
	})
	diagnosticsMessage := openClassicASPDocumentWithDiagnostics(t, client, uri, source)
	serialized := string(diagnosticsMessage.Params)
	if !strings.Contains(serialized, "asp-lsp-vbscript-dead-code") ||
		!strings.Contains(serialized, "unreachableCode") ||
		!strings.Contains(serialized, `"tags":[1]`) {
		t.Fatalf("dead code diagnostic missing unnecessary hint: %s", serialized)
	}

	notifyConfiguration(t, client, map[string]any{"aspLsp": map[string]any{"vbscript": map[string]any{"deadCodeDiagnostics": false}}})
	disabled := client.request("textDocument/diagnostic", map[string]any{
		"textDocument": map[string]any{"uri": uri},
	})
	disabledText := mustJSONText(t, disabled.Result)
	if strings.Contains(disabledText, "asp-lsp-vbscript-dead-code") {
		t.Fatalf("dead code diagnostics should be disabled: %s", disabledText)
	}
	if !strings.Contains(disabledText, "asp-lsp-vbscript-unused") {
		t.Fatalf("unused diagnostics should remain enabled when dead code is disabled: %s", disabledText)
	}
}
