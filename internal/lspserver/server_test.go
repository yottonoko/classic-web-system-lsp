package lspserver

import (
	"bufio"
	"bytes"
	"context"
	"encoding/json"
	"fmt"
	"io"
	"os"
	"path/filepath"
	"strconv"
	"strings"
	"testing"
	"time"

	"github.com/yottonoko/classic-web-system-lsp/internal/core"
	"github.com/yottonoko/classic-web-system-lsp/internal/lsp"
	"github.com/yottonoko/classic-web-system-lsp/internal/vbscript"
)

func TestInitializeOverJSONRPC(t *testing.T) {
	var input bytes.Buffer
	var output bytes.Buffer
	writeTestMessage(t, &input, map[string]any{"jsonrpc": "2.0", "id": 1, "method": "initialize", "params": map[string]any{"capabilities": map[string]any{}}})
	writeTestMessage(t, &input, map[string]any{"jsonrpc": "2.0", "method": "exit"})
	server := New(&input, &output, nil)
	ctx, cancel := context.WithTimeout(context.Background(), time.Second)
	defer cancel()
	if err := server.Serve(ctx); err != nil {
		t.Fatalf("Serve: %v", err)
	}
	message, err := readMessage(bufio.NewReader(&output))
	if err != nil {
		t.Fatalf("read response: %v", err)
	}
	if message.ID == nil || message.Result == nil {
		t.Fatalf("response = %#v", message)
	}
}

func TestAbsoluteFilePathURIUsesCanonicalWindowsForms(t *testing.T) {
	tests := []struct {
		name string
		path string
		want string
	}{
		{name: "drive", path: `C:\site\default.asp`, want: "file:///C:/site/default.asp"},
		{name: "UNC", path: `\\server\share\default.asp`, want: "file://server/share/default.asp"},
		{name: "extended drive", path: `\\?\C:\site\default.asp`, want: "file:///C:/site/default.asp"},
		{name: "extended UNC", path: `\\?\UNC\server\share\default.asp`, want: "file://server/share/default.asp"},
	}
	for _, test := range tests {
		t.Run(test.name, func(t *testing.T) {
			if got := absoluteFilePathURI(test.path); got != test.want {
				t.Fatalf("absoluteFilePathURI(%q) = %q, want %q", test.path, got, test.want)
			}
		})
	}
}

func TestFileURIPathAcceptsCanonicalAndLegacyWindowsForms(t *testing.T) {
	tests := []struct {
		name string
		uri  string
		want string
	}{
		{name: "drive", uri: "file:///C:/site/default.asp", want: "C:/site/default.asp"},
		{name: "mixed-case scheme drive", uri: "FiLe:///C:/site/default.asp", want: "C:/site/default.asp"},
		{name: "legacy drive", uri: "file://C:/site/default.asp", want: "C:/site/default.asp"},
		{name: "UNC", uri: "file://server/share/default.asp", want: "//server/share/default.asp"},
		{name: "mixed-case scheme UNC", uri: "FILE://server/share/default.asp", want: "//server/share/default.asp"},
		{name: "legacy UNC", uri: "file:////server/share/default.asp", want: "//server/share/default.asp"},
	}
	for _, test := range tests {
		t.Run(test.name, func(t *testing.T) {
			if got := filepath.ToSlash(fileURIPath(test.uri)); got != test.want {
				t.Fatalf("fileURIPath(%q) = %q, want %q", test.uri, got, test.want)
			}
		})
	}
}

func writeTestMessage(t *testing.T, buffer *bytes.Buffer, value any) {
	t.Helper()
	body, err := json.Marshal(value)
	if err != nil {
		t.Fatal(err)
	}
	buffer.WriteString("Content-Length: ")
	buffer.WriteString(strconv.Itoa(len(body)))
	buffer.WriteString("\r\n\r\n")
	buffer.Write(body)
}

func TestIncludedDocumentsTraversesTransitiveIncludes(t *testing.T) {
	root := t.TempDir()
	includeDirectives := []string{}
	for index := 0; index < 8; index++ {
		includeName := fmt.Sprintf("shared-%d.inc", index)
		if index == 0 {
			includeDirectives = append(includeDirectives, fmt.Sprintf(`<!-- #include file="%s" -->`, includeName))
		}
		nextInclude := ""
		if index < 7 {
			nextInclude = fmt.Sprintf("<!-- #include file=\"shared-%d.inc\" -->\n", index+1)
		}
		content := fmt.Sprintf("%s<%%\nFunction SharedExport%d()\nEnd Function\n%%>", nextInclude, index)
		if err := os.WriteFile(filepath.Join(root, includeName), []byte(content), 0o644); err != nil {
			t.Fatal(err)
		}
	}
	source := strings.Join(includeDirectives, "\n") + `
<%
Response.Write SharedExport0()
%>`
	owner := filepath.Join(root, "default.asp")
	if err := os.WriteFile(owner, []byte(source), 0o644); err != nil {
		t.Fatal(err)
	}
	server := New(nil, io.Discard, nil)
	parsed := core.ParseDocument(filePathURI(owner), source, core.Settings{DefaultLanguage: server.settings.DefaultLanguage})
	included := server.includedDocuments(parsed)
	if len(included) != 8 {
		t.Fatalf("included document count = %d, want 8", len(included))
	}
}

func TestServerSmokeParsesAndRoutesRepresentativeClassicASPDocument(t *testing.T) {
	source := `<%@ LANGUAGE="VBScript" %>
<html>
<head><style>body { color: red; }</style></head>
<body>
<script>const value = document.title;</script>
<% Option Explicit
Dim userName
Response.Write userName
%>
</body>
</html>`
	parsed := core.ParseDocument("file:///site/default.asp", source, core.Settings{DefaultLanguage: "VBScript"})
	html := core.BuildVirtualDocument(parsed, core.LanguageHTML)
	css := core.BuildVirtualDocument(parsed, core.LanguageCSS)
	javascript := core.BuildVirtualDocument(parsed, core.LanguageJavaScript)
	for _, expectation := range []struct {
		name string
		text string
		want string
	}{
		{name: "html", text: html.Text, want: "<html>"},
		{name: "css", text: css.Text, want: "color"},
		{name: "javascript", text: javascript.Text, want: "document.title"},
	} {
		if !strings.Contains(expectation.text, expectation.want) {
			t.Fatalf("%s virtual document missing %q: %q", expectation.name, expectation.want, expectation.text)
		}
	}
	if diagnostics := core.Diagnostics(parsed); len(diagnostics) != 0 {
		t.Fatalf("parser diagnostics = %#v", diagnostics)
	}
	if !hasCompletionLabel(vbscript.CompletionsFor(source, strings.Index(source, "Response.")+len("Response.")), "Write") {
		t.Fatalf("Classic ASP VBScript completions missing Response.Write")
	}
}

func TestServerSmokeParsesAndRoutesStandaloneVBSDocument(t *testing.T) {
	source := "WScript.\n"
	completions := vbscript.CompletionsForOptions(source, len("WScript."), vbscript.CompletionOptions{Standalone: true})
	if !hasCompletionLabel(completions, "Echo") {
		t.Fatalf("standalone VBS completions missing WScript.Echo: %#v", completions)
	}
	topLevel := vbscript.CompletionsForOptions(source, len(source), vbscript.CompletionOptions{Standalone: true})
	if hasCompletionLabel(topLevel, "Response") {
		t.Fatalf("standalone VBS top-level completions should not include Response: %#v", topLevel)
	}
}

func hasCompletionLabel(items []lsp.CompletionItem, label string) bool {
	for _, item := range items {
		if item.Label == label {
			return true
		}
	}
	return false
}
