package lspserver

import (
	"path/filepath"
	"reflect"
	"strings"
	"testing"

	"github.com/yottonoko/classic-web-system-lsp/internal/lsp"
)

func TestStdioParityMatchesLegacyVBScriptOnTypeIndentation(t *testing.T) {
	client := startStdioTestClient(t)
	defer client.close()

	root := t.TempDir()
	client.request("initialize", map[string]any{
		"processId":    nil,
		"rootUri":      pathToFileURI(root),
		"capabilities": map[string]any{},
	})
	tests := []struct {
		name     string
		source   string
		line     int
		expected []lsp.TextEdit
	}{
		{
			name:   "indent-after-block-opener",
			source: "<%\n  If ready Then\n\n%>",
			line:   2,
			expected: []lsp.TextEdit{{
				Range:   lsp.Range{Start: lsp.Position{Line: 2}, End: lsp.Position{Line: 2}},
				NewText: "    ",
			}},
		},
		{
			name:   "outdent-block-closer",
			source: "<%\n    Response.Write ready\n      End If\n%>",
			line:   2,
			expected: []lsp.TextEdit{{
				Range:   lsp.Range{Start: lsp.Position{Line: 2}, End: lsp.Position{Line: 2, Character: 6}},
				NewText: "  ",
			}},
		},
		{
			name:     "skip-blank-previous-line",
			source:   "<%\n  If ready Then\n\n    \n%>",
			line:     3,
			expected: []lsp.TextEdit{},
		},
		{
			name:   "inherit-ordinary-line-indent",
			source: "<%\n    Response.Write ready\n  value = 1\n%>",
			line:   2,
			expected: []lsp.TextEdit{{
				Range:   lsp.Range{Start: lsp.Position{Line: 2}, End: lsp.Position{Line: 2, Character: 2}},
				NewText: "    ",
			}},
		},
	}
	for _, test := range tests {
		t.Run(test.name, func(t *testing.T) {
			uri := pathToFileURI(filepath.Join(root, test.name+".asp"))
			edits := requestOnTypeFormatting(t, client, uri, test.source, map[string]int{"line": test.line, "character": 0}, "\n")
			if !reflect.DeepEqual(edits, test.expected) {
				t.Fatalf("on-type edits = %#v, want %#v", edits, test.expected)
			}
		})
	}
}

func TestStdioParityMatchesLegacyASPCloseOnTypeAlignment(t *testing.T) {
	client := startStdioTestClient(t)
	defer client.close()

	root := t.TempDir()
	client.request("initialize", map[string]any{
		"processId":    nil,
		"rootUri":      pathToFileURI(root),
		"capabilities": map[string]any{},
	})
	tests := []struct {
		name    string
		source  string
		newText string
		end     int
	}{
		{name: "outdent-after-end", source: "<%\n    End If\n      %>", newText: "  ", end: 6},
		{name: "inherit-previous-indent", source: "<%\n    Response.Write ready\n  %>", newText: "    ", end: 2},
	}
	for _, test := range tests {
		t.Run(test.name, func(t *testing.T) {
			uri := pathToFileURI(filepath.Join(root, test.name+".asp"))
			lineStart := strings.LastIndex(test.source, "\n") + 1
			edits := requestOnTypeFormatting(t, client, uri, test.source, positionAt(test.source, len(test.source)), ">")
			expected := []lsp.TextEdit{{
				Range: lsp.Range{
					Start: positionAtLSP(test.source, lineStart),
					End:   lsp.Position{Line: 2, Character: test.end},
				},
				NewText: test.newText,
			}}
			if !reflect.DeepEqual(edits, expected) {
				t.Fatalf("ASP-close edits = %#v, want %#v", edits, expected)
			}
		})
	}
}

func positionAtLSP(text string, offset int) lsp.Position {
	position := positionAt(text, offset)
	return lsp.Position{Line: position["line"], Character: position["character"]}
}
