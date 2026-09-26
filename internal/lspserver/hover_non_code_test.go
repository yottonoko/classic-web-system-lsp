package lspserver

import (
	"io"
	"strings"
	"testing"

	"github.com/yottonoko/classic-web-system-lsp/internal/core"
	"github.com/yottonoko/classic-web-system-lsp/internal/lsp"
)

func TestHoverIgnoresUserDefinedSymbolsInVBScriptNonCodeTokens(t *testing.T) {
	tests := []struct {
		name string
		uri  string
		text string
	}{
		{
			name: "classic asp",
			uri:  "file:///tmp/non-code-hover.asp",
			text: `<%
Function Fake(ByVal value)
  Fake = value
End Function
Function January()
End Function
' 日本語 Fake in an apostrophe comment
Rem 日本語 Fake in a Rem comment
Response.Write "Fake: doubled ""Fake"""
Response.Write #January 1, 2024#
Call Fake()
%>`,
		},
		{
			name: "standalone vbscript",
			uri:  "file:///tmp/non-code-hover.vbs",
			text: `Function Fake(ByVal value)
  Fake = value
End Function
Function January()
End Function
' 日本語 Fake in an apostrophe comment
Rem 日本語 Fake in a Rem comment
Response.Write "Fake: doubled ""Fake"""
Response.Write #January 1, 2024#
Call Fake()
`,
		},
	}

	for _, test := range tests {
		t.Run(test.name, func(t *testing.T) {
			server := New(strings.NewReader(""), io.Discard, io.Discard)
			defer server.shutdownRuntimeCaches()
			document := core.NewTextDocument(test.uri, "classic-asp", 1, test.text)
			server.mu.Lock()
			server.rememberOpenDocumentLocked(test.uri, document)
			server.mu.Unlock()

			for _, testCase := range []struct {
				name   string
				needle string
				start  int
				want   string
			}{
				{name: "function declaration", needle: "Function Fake", start: len("Function "), want: "Function Fake"},
				{name: "apostrophe comment", needle: "' 日本語 Fake", start: len("' 日本語 ")},
				{name: "rem comment", needle: "Rem 日本語 Fake", start: len("Rem 日本語 ")},
				{name: "string", needle: `"Fake: doubled`, start: 1},
				{name: "escaped string", needle: `""Fake""`, start: 2},
				{name: "date literal", needle: "#January", start: 1},
				{name: "code reference", needle: "Call Fake", start: len("Call "), want: "Function Fake"},
			} {
				offset := strings.Index(test.text, testCase.needle)
				if offset < 0 {
					t.Fatalf("%s test token %q not found", testCase.name, testCase.needle)
				}
				offset += testCase.start
				value, ok := server.hover(test.uri, document.PositionAt(offset)).(*lsp.Hover)
				if testCase.want == "" {
					if ok || value != nil {
						t.Fatalf("%s hover = %#v, want nil", testCase.name, value)
					}
					continue
				}
				if !ok || value == nil {
					t.Fatalf("%s hover = %#v, want user-defined hover", testCase.name, value)
				}
				if serialized := mustJSONForTest(t, value); !strings.Contains(serialized, testCase.want) {
					t.Fatalf("%s hover = %s, want %q", testCase.name, serialized, testCase.want)
				}
			}
		})
	}
}

func TestStdioHoverIgnoresUserDefinedSymbolsInVBScriptNonCodeTokens(t *testing.T) {
	client := startStdioTestClient(t)
	defer client.close()

	client.request("initialize", map[string]any{
		"processId":    nil,
		"rootUri":      "file:///tmp",
		"capabilities": map[string]any{},
	})

	tests := []struct {
		name string
		uri  string
		text string
	}{
		{
			name: "classic asp",
			uri:  "file:///tmp/stdio-non-code-hover.asp",
			text: `<%
Function Fake(ByVal value)
  Fake = value
End Function
Function January()
End Function
' 日本語 Fake in an apostrophe comment
Rem 日本語 Fake in a Rem comment
Response.Write "Fake: doubled ""Fake"""
Response.Write #January 1, 2024#
Call Fake()
%>`,
		},
		{
			name: "standalone vbscript",
			uri:  "file:///tmp/stdio-non-code-hover.vbs",
			text: `Function Fake(ByVal value)
  Fake = value
End Function
Function January()
End Function
' 日本語 Fake in an apostrophe comment
Rem 日本語 Fake in a Rem comment
Response.Write "Fake: doubled ""Fake"""
Response.Write #January 1, 2024#
Call Fake()
`,
		},
	}

	for _, test := range tests {
		t.Run(test.name, func(t *testing.T) {
			openClassicASPDocument(t, client, test.uri, test.text)
			for _, testCase := range []struct {
				name   string
				needle string
				start  int
				want   string
			}{
				{name: "apostrophe comment", needle: "' 日本語 Fake", start: len("' 日本語 ")},
				{name: "rem comment", needle: "Rem 日本語 Fake", start: len("Rem 日本語 ")},
				{name: "string", needle: `"Fake: doubled`, start: 1},
				{name: "escaped string", needle: `""Fake""`, start: 2},
				{name: "date literal", needle: "#January", start: 1},
				{name: "code reference", needle: "Call Fake", start: len("Call "), want: "Function Fake"},
			} {
				offset := strings.Index(test.text, testCase.needle)
				if offset < 0 {
					t.Fatalf("%s test token %q not found", testCase.name, testCase.needle)
				}
				position := positionAt(test.text, offset+testCase.start)
				result := client.request("textDocument/hover", map[string]any{
					"textDocument": map[string]any{"uri": test.uri},
					"position":     position,
				}).Result
				if testCase.want == "" {
					if result != nil {
						t.Fatalf("%s hover = %s, want null", testCase.name, mustJSONText(t, result))
					}
					continue
				}
				if result == nil || !strings.Contains(mustJSONText(t, result), testCase.want) {
					t.Fatalf("%s hover = %s, want %q", testCase.name, mustJSONText(t, result), testCase.want)
				}
			}
		})
	}
}
