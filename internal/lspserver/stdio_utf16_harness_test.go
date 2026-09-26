package lspserver

import (
	"path/filepath"
	"strings"
	"testing"
)

func TestStdioHarnessUsesUTF16PositionsForNonBMPPrefixes(t *testing.T) {
	client := startStdioTestClient(t)
	defer client.close()

	root := t.TempDir()
	client.request("initialize", map[string]any{
		"processId": nil,
		"rootUri":   pathToFileURI(root),
		"capabilities": map[string]any{
			"workspace": map[string]any{},
		},
	})

	uri := pathToFileURI(filepath.Join(root, "utf16.asp"))
	source := "😀<% Response. %>"
	responseOffset := strings.Index(source, "Response.") + len("Response.")
	position := positionAt(source, responseOffset)
	if position["character"] != 14 {
		t.Fatalf("UTF-16 position character = %d, want 14", position["character"])
	}
	if got := offsetAtPosition(source, position["line"], position["character"]); got != responseOffset {
		t.Fatalf("UTF-16 position round trip = %d, want %d", got, responseOffset)
	}

	openClassicASPDocument(t, client, uri, source)
	completion := client.request("textDocument/completion", map[string]any{
		"textDocument": map[string]any{"uri": uri},
		"position":     position,
	})
	if !completionLabels(completion.Result).contains("Write") {
		t.Fatalf("completion after non-BMP prefix missing Write: %s", mustJSONText(t, completion.Result))
	}

	source = notifyNeedleReplacement(t, client, uri, source, 2, "Response.", "Response.Wri")
	completion = client.request("textDocument/completion", map[string]any{
		"textDocument": map[string]any{"uri": uri},
		"position":     positionAt(source, strings.Index(source, "Response.Wri")+len("Response.Wri")),
	})
	if !completionLabels(completion.Result).contains("Write") {
		t.Fatalf("completion after UTF-16 ranged change missing Write: %s", mustJSONText(t, completion.Result))
	}
}
