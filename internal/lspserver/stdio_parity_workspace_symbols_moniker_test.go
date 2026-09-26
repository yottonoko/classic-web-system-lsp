package lspserver

import (
	"os"
	"path/filepath"
	"reflect"
	"strings"
	"testing"

	"github.com/yottonoko/classic-web-system-lsp/internal/core"
	"github.com/yottonoko/classic-web-system-lsp/internal/lsp"
)

func TestStdioParityReturnsRichWorkspaceSymbolsAcrossEmbeddedLanguages(t *testing.T) {
	client := startStdioTestClient(t)
	defer client.close()

	root := t.TempDir()
	if err := os.WriteFile(filepath.Join(root, "shared.inc"), []byte("<% Const SharedValue = 1 %>"), 0o644); err != nil {
		t.Fatal(err)
	}
	unopenedURI := pathToFileURI(filepath.Join(root, "unopened.asp"))
	unopenedSource := `<div id="unopenedPanel"></div>
<style>.unopened-card { color: blue; }</style>
<script>function unopenedBoot() {}</script>`
	if err := os.WriteFile(filepath.Join(root, "unopened.asp"), []byte(unopenedSource), 0o644); err != nil {
		t.Fatal(err)
	}
	uri := pathToFileURI(filepath.Join(root, "default.asp"))
	source := `<!-- #include file="shared.inc" -->
<div id="panelId" name="panelName"></div>
<style>.card { color: red; }</style>
<script>
class DashboardWidget {
  render() {}
}
function bootWidget() {}
const formatter = new Intl.DateTimeFormat("en");
let mutableValue = 1;
const fakeMarkup = '<div id="scriptOnly" name="scriptNameOnly"></div>';
</script>`
	initializeAndWaitForWorkspaceIndex(t, client, map[string]any{
		"processId":    nil,
		"rootUri":      pathToFileURI(root),
		"capabilities": map[string]any{},
	})
	openClassicASPDocument(t, client, uri, source)

	response := client.request("workspace/symbol", map[string]any{"query": ""})
	var symbols []lsp.SymbolInformation
	mustDecodeResult(t, response.Result, &symbols)
	for _, expected := range []struct {
		name      string
		kind      int
		container string
		uri       string
		source    string
	}{
		{name: "shared.inc", kind: 1, container: "include", uri: uri, source: source},
		{name: "panelId", kind: 20, container: "html", uri: uri, source: source},
		{name: "panelName", kind: 20, container: "html", uri: uri, source: source},
		{name: ".card", kind: 5, container: "css", uri: uri, source: source},
		{name: "DashboardWidget", kind: 5, container: "javascript", uri: uri, source: source},
		{name: "bootWidget", kind: 12, container: "javascript", uri: uri, source: source},
		{name: "formatter", kind: 14, container: "javascript", uri: uri, source: source},
		{name: "mutableValue", kind: 13, container: "javascript", uri: uri, source: source},
		{name: "unopenedPanel", kind: 20, container: "html", uri: unopenedURI, source: unopenedSource},
		{name: ".unopened-card", kind: 5, container: "css", uri: unopenedURI, source: unopenedSource},
		{name: "unopenedBoot", kind: 12, container: "javascript", uri: unopenedURI, source: unopenedSource},
	} {
		var found *lsp.SymbolInformation
		for _, symbol := range symbols {
			if symbol.Name == expected.name && symbol.ContainerName == expected.container && symbol.Kind == expected.kind && symbol.Location.URI == expected.uri {
				value := symbol
				found = &value
				break
			}
		}
		if found == nil {
			t.Fatalf("workspace symbol missing %#v: %s", expected, mustJSONText(t, response.Result))
		}
		wantRange := tokenRange(expected.source, expected.name)
		if !reflect.DeepEqual(found.Location.Range, wantRange) {
			t.Fatalf("workspace symbol %q range = %#v, want %#v", expected.name, found.Location.Range, wantRange)
		}
	}
	for _, excluded := range []string{"scriptOnly", "scriptNameOnly"} {
		for _, symbol := range symbols {
			if symbol.Name == excluded {
				t.Fatalf("HTML-looking JavaScript string indexed as %q: %s", excluded, mustJSONText(t, response.Result))
			}
		}
	}

	noMatches := client.request("workspace/symbol", map[string]any{"query": "definitely-no-symbol"})
	if got := mustJSONText(t, noMatches.Result); got != "[]" {
		t.Fatalf("empty workspace symbols = %s, want []", got)
	}
	caseInsensitive := client.request("workspace/symbol", map[string]any{"query": "dashboardwidget"})
	if !strings.Contains(mustJSONText(t, caseInsensitive.Result), "DashboardWidget") {
		t.Fatalf("case-insensitive workspace query missed DashboardWidget: %s", mustJSONText(t, caseInsensitive.Result))
	}
}

func TestStdioParityReturnsMappedSymbolsForJavaScriptAndJScript(t *testing.T) {
	client := startStdioTestClient(t)
	defer client.close()

	root := t.TempDir()
	uri := pathToFileURI(filepath.Join(root, "mixed-script.asp"))
	source := `<%@ LANGUAGE="JScript" %>
<%
function serverWidget() {}
%>
<script>
class ClientWidget {
  render() {}
}
</script>`
	client.request("initialize", map[string]any{
		"processId":    nil,
		"rootUri":      pathToFileURI(root),
		"capabilities": map[string]any{},
	})
	openClassicASPDocument(t, client, uri, source)

	response := client.request("textDocument/documentSymbol", map[string]any{
		"textDocument": map[string]any{"uri": uri},
	})
	var symbols []lsp.DocumentSymbol
	mustDecodeResult(t, response.Result, &symbols)
	server := findDocumentSymbol(symbols, "serverWidget")
	clientClass := findDocumentSymbol(symbols, "ClientWidget")
	render := findDocumentSymbol(symbols, "render")
	if server == nil || clientClass == nil || render == nil {
		t.Fatalf("mixed JavaScript/JScript symbols missing: %s", mustJSONText(t, response.Result))
	}
	for _, symbol := range []*lsp.DocumentSymbol{server, clientClass, render} {
		if symbol.SelectionRange == (lsp.Range{}) || symbol.SelectionRange != tokenRange(source, symbol.Name) {
			t.Fatalf("symbol %q has unmapped selection range %#v", symbol.Name, symbol.SelectionRange)
		}
	}
	if server.Kind != 12 || clientClass.Kind != 5 || render.Kind != 12 {
		t.Fatalf("mixed symbol kinds = server %d, class %d, method %d", server.Kind, clientClass.Kind, render.Kind)
	}

	workspace := client.request("workspace/symbol", map[string]any{"query": "Widget"})
	text := mustJSONText(t, workspace.Result)
	if !strings.Contains(text, "serverWidget") || !strings.Contains(text, "ClientWidget") {
		t.Fatalf("workspace symbols omit a JavaScript language: %s", text)
	}
}

func TestStdioParityResolvesVBScriptMonikersToProjectDefinitions(t *testing.T) {
	client := startStdioTestClient(t)
	defer client.close()

	root := t.TempDir()
	includePath := filepath.Join(root, "shared.inc")
	includeURI := pathToFileURI(includePath)
	includeSource := `<%
Function BuildName(firstName)
  BuildName = firstName
End Function
%>`
	if err := os.WriteFile(includePath, []byte(includeSource), 0o644); err != nil {
		t.Fatal(err)
	}
	uri := pathToFileURI(filepath.Join(root, "default.asp"))
	source := `<!-- #include file="shared.inc" -->
<%
Response.Write BuildName("Ada")
Response.Write unresolvedName
%>`
	initializeAndWaitForWorkspaceIndex(t, client, map[string]any{
		"processId":    nil,
		"rootUri":      pathToFileURI(root),
		"capabilities": map[string]any{},
	})
	openClassicASPDocument(t, client, uri, source)

	call := client.request("textDocument/moniker", map[string]any{
		"textDocument": map[string]any{"uri": uri},
		"position":     positionAt(source, strings.Index(source, "BuildName")+2),
	})
	var monikers []lsp.Moniker
	mustDecodeResult(t, call.Result, &monikers)
	wantIdentifier := includeURI + "#BuildName#1#9"
	if len(monikers) != 1 || monikers[0].Scheme != "asp-lsp" || monikers[0].Identifier != wantIdentifier || monikers[0].Unique != "project" || monikers[0].Kind != "export" {
		t.Fatalf("resolved moniker = %#v, want identifier %q: %s", monikers, wantIdentifier, mustJSONText(t, call.Result))
	}

	unresolved := client.request("textDocument/moniker", map[string]any{
		"textDocument": map[string]any{"uri": uri},
		"position":     positionAt(source, strings.Index(source, "unresolvedName")+2),
	})
	if got := mustJSONText(t, unresolved.Result); got != "[]" {
		t.Fatalf("unresolved moniker = %s, want []", got)
	}

	parameter := client.request("textDocument/moniker", map[string]any{
		"textDocument": map[string]any{"uri": includeURI},
		"position":     positionAt(includeSource, strings.LastIndex(includeSource, "firstName")+2),
	})
	mustDecodeResult(t, parameter.Result, &monikers)
	wantParameter := includeURI + "#BuildName#firstName#1#19"
	if len(monikers) != 1 || monikers[0].Scheme != "asp-lsp" || monikers[0].Identifier != wantParameter || monikers[0].Unique != "project" || monikers[0].Kind != "local" {
		t.Fatalf("parameter moniker = %#v, want identifier %q: %s", monikers, wantParameter, mustJSONText(t, parameter.Result))
	}
}

func tokenRange(source, token string) lsp.Range {
	start := strings.Index(source, token)
	if start < 0 {
		return lsp.Range{}
	}
	return core.NewTextDocument("file:///test.asp", "classic-asp", 0, source).Range(start, start+len(token))
}

func findDocumentSymbol(symbols []lsp.DocumentSymbol, name string) *lsp.DocumentSymbol {
	for index := range symbols {
		if symbols[index].Name == name {
			return &symbols[index]
		}
		if child := findDocumentSymbol(symbols[index].Children, name); child != nil {
			return child
		}
	}
	return nil
}
