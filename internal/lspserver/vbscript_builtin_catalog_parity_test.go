package lspserver

import (
	"path/filepath"
	"strings"
	"testing"

	"github.com/yottonoko/classic-web-system-lsp/internal/core"
	"github.com/yottonoko/classic-web-system-lsp/internal/lsp"
)

func TestVBScriptBuiltinGlobalObjectCatalogMatchesTypeScript(t *testing.T) {
	tests := []struct {
		typeName string
		members  []vbBuiltinMember
	}{
		{"Application", []vbBuiltinMember{
			{Name: "Contents", Kind: lsp.CompletionItemKindProperty, TypeName: "Variant"},
			{Name: "StaticObjects", Kind: lsp.CompletionItemKindProperty, TypeName: "Variant"},
			{Name: "Contents.Remove", Kind: lsp.CompletionItemKindMethod, TypeName: "Variant", Signature: "Application.Contents.Remove(name)"},
			{Name: "Contents.RemoveAll", Kind: lsp.CompletionItemKindMethod, TypeName: "Variant", Signature: "Application.Contents.RemoveAll()"},
			{Name: "Lock", Kind: lsp.CompletionItemKindMethod, TypeName: "Variant", Signature: "Application.Lock"},
			{Name: "Unlock", Kind: lsp.CompletionItemKindMethod, TypeName: "Variant", Signature: "Application.Unlock"},
		}},
		{"Session", []vbBuiltinMember{
			{Name: "Contents", Kind: lsp.CompletionItemKindProperty, TypeName: "Variant"},
			{Name: "StaticObjects", Kind: lsp.CompletionItemKindProperty, TypeName: "Variant"},
			{Name: "CodePage", Kind: lsp.CompletionItemKindProperty, TypeName: "Number"},
			{Name: "LCID", Kind: lsp.CompletionItemKindProperty, TypeName: "Number"},
			{Name: "SessionID", Kind: lsp.CompletionItemKindProperty, TypeName: "String"},
			{Name: "Timeout", Kind: lsp.CompletionItemKindProperty, TypeName: "Number"},
			{Name: "Abandon", Kind: lsp.CompletionItemKindMethod, TypeName: "Variant", Signature: "Session.Abandon"},
			{Name: "Contents.Remove", Kind: lsp.CompletionItemKindMethod, TypeName: "Variant", Signature: "Session.Contents.Remove(name)"},
			{Name: "Contents.RemoveAll", Kind: lsp.CompletionItemKindMethod, TypeName: "Variant", Signature: "Session.Contents.RemoveAll()"},
		}},
		{"Response", []vbBuiltinMember{
			{Name: "Cookies", Kind: lsp.CompletionItemKindProperty, TypeName: "Variant"},
			{Name: "Buffer", Kind: lsp.CompletionItemKindProperty, TypeName: "Boolean"},
			{Name: "CacheControl", Kind: lsp.CompletionItemKindProperty, TypeName: "String"},
			{Name: "Charset", Kind: lsp.CompletionItemKindProperty, TypeName: "String"},
			{Name: "ContentType", Kind: lsp.CompletionItemKindProperty, TypeName: "String"},
			{Name: "Expires", Kind: lsp.CompletionItemKindProperty, TypeName: "Number"},
			{Name: "ExpiresAbsolute", Kind: lsp.CompletionItemKindProperty, TypeName: "Date"},
			{Name: "IsClientConnected", Kind: lsp.CompletionItemKindProperty, TypeName: "Boolean"},
			{Name: "Pics", Kind: lsp.CompletionItemKindProperty, TypeName: "String"},
			{Name: "Status", Kind: lsp.CompletionItemKindProperty, TypeName: "String"},
			{Name: "AddHeader", Kind: lsp.CompletionItemKindMethod, TypeName: "Variant", Signature: "Response.AddHeader(name, value)"},
			{Name: "AppendToLog", Kind: lsp.CompletionItemKindMethod, TypeName: "Variant", Signature: "Response.AppendToLog string"},
			{Name: "BinaryWrite", Kind: lsp.CompletionItemKindMethod, TypeName: "Variant", Signature: "Response.BinaryWrite(data)"},
			{Name: "Clear", Kind: lsp.CompletionItemKindMethod, TypeName: "Variant", Signature: "Response.Clear"},
			{Name: "End", Kind: lsp.CompletionItemKindMethod, TypeName: "Variant", Signature: "Response.End"},
			{Name: "Flush", Kind: lsp.CompletionItemKindMethod, TypeName: "Variant", Signature: "Response.Flush"},
			{Name: "Redirect", Kind: lsp.CompletionItemKindMethod, TypeName: "Variant", Signature: "Response.Redirect url"},
			{Name: "Write", Kind: lsp.CompletionItemKindMethod, TypeName: "Variant", Signature: "Response.Write value"},
		}},
		{"Request", []vbBuiltinMember{
			{Name: "QueryString", Kind: lsp.CompletionItemKindProperty, TypeName: "String", Signature: "Request.QueryString(name)"},
			{Name: "Form", Kind: lsp.CompletionItemKindProperty, TypeName: "String", Signature: "Request.Form(name)"},
			{Name: "Cookies", Kind: lsp.CompletionItemKindProperty, TypeName: "Variant", Signature: "Request.Cookies(name)"},
			{Name: "ServerVariables", Kind: lsp.CompletionItemKindProperty, TypeName: "String", Signature: "Request.ServerVariables(name)"},
			{Name: "ClientCertificate", Kind: lsp.CompletionItemKindProperty, TypeName: "Variant"},
			{Name: "TotalBytes", Kind: lsp.CompletionItemKindProperty, TypeName: "Number"},
			{Name: "BinaryRead", Kind: lsp.CompletionItemKindMethod, TypeName: "Array", Signature: "Request.BinaryRead(count)"},
		}},
		{"Server", []vbBuiltinMember{
			{Name: "ScriptTimeout", Kind: lsp.CompletionItemKindProperty, TypeName: "Number"},
			{Name: "CreateObject", Kind: lsp.CompletionItemKindMethod, TypeName: "Object", Signature: "Server.CreateObject(progId)"},
			{Name: "Execute", Kind: lsp.CompletionItemKindMethod, TypeName: "Variant", Signature: "Server.Execute(path)"},
			{Name: "GetLastError", Kind: lsp.CompletionItemKindMethod, TypeName: "ASPError", Signature: "Server.GetLastError()"},
			{Name: "HTMLEncode", Kind: lsp.CompletionItemKindMethod, TypeName: "String", Signature: "Server.HTMLEncode(value)"},
			{Name: "MapPath", Kind: lsp.CompletionItemKindMethod, TypeName: "String", Signature: "Server.MapPath(path)"},
			{Name: "Transfer", Kind: lsp.CompletionItemKindMethod, TypeName: "Variant", Signature: "Server.Transfer(path)"},
			{Name: "URLEncode", Kind: lsp.CompletionItemKindMethod, TypeName: "String", Signature: "Server.URLEncode(value)"},
		}},
		{"WScript", []vbBuiltinMember{
			{Name: "Arguments", Kind: lsp.CompletionItemKindProperty, TypeName: "Variant"},
			{Name: "FullName", Kind: lsp.CompletionItemKindProperty, TypeName: "String"},
			{Name: "Name", Kind: lsp.CompletionItemKindProperty, TypeName: "String"},
			{Name: "Path", Kind: lsp.CompletionItemKindProperty, TypeName: "String"},
			{Name: "ScriptFullName", Kind: lsp.CompletionItemKindProperty, TypeName: "String"},
			{Name: "ScriptName", Kind: lsp.CompletionItemKindProperty, TypeName: "String"},
			{Name: "StdErr", Kind: lsp.CompletionItemKindProperty, TypeName: "Object"},
			{Name: "StdIn", Kind: lsp.CompletionItemKindProperty, TypeName: "Object"},
			{Name: "StdOut", Kind: lsp.CompletionItemKindProperty, TypeName: "Object"},
			{Name: "Version", Kind: lsp.CompletionItemKindProperty, TypeName: "String"},
			{Name: "ConnectObject", Kind: lsp.CompletionItemKindMethod, TypeName: "Variant", Signature: "WScript.ConnectObject(object, prefix)"},
			{Name: "CreateObject", Kind: lsp.CompletionItemKindMethod, TypeName: "Object", Signature: "WScript.CreateObject(progId, prefix)"},
			{Name: "DisconnectObject", Kind: lsp.CompletionItemKindMethod, TypeName: "Variant", Signature: "WScript.DisconnectObject object"},
			{Name: "Echo", Kind: lsp.CompletionItemKindMethod, TypeName: "Variant", Signature: "WScript.Echo value"},
			{Name: "GetObject", Kind: lsp.CompletionItemKindMethod, TypeName: "Object", Signature: "WScript.GetObject(pathname, progId, prefix)"},
			{Name: "Quit", Kind: lsp.CompletionItemKindMethod, TypeName: "Variant", Signature: "WScript.Quit errorCode"},
			{Name: "Sleep", Kind: lsp.CompletionItemKindMethod, TypeName: "Variant", Signature: "WScript.Sleep milliseconds"},
		}},
	}

	for _, test := range tests {
		actual := vbscriptBuiltinTypeMembers(test.typeName)
		if len(actual) != len(test.members) {
			t.Fatalf("%s members = %d, want %d: %#v", test.typeName, len(actual), len(test.members), actual)
		}
		for _, expected := range test.members {
			member, ok := actual[lowerASCII(expected.Name)]
			if !ok || member.Name != expected.Name || member.Kind != expected.Kind || member.TypeName != expected.TypeName || member.Signature != expected.Signature {
				t.Errorf("%s.%s = %#v, want %#v", test.typeName, expected.Name, member, expected)
			}
		}
	}
}

func TestStdioParityCompletesFullClassicASPGlobalObjectCatalog(t *testing.T) {
	client := startStdioTestClient(t)
	defer client.close()

	root := t.TempDir()
	uri := pathToFileURI(filepath.Join(root, "global-object-catalog.asp"))
	source := `<%
Application.
Session.
Response.
Request.
Server.
Dim requestValue, pathValue
requestValue = Request.QueryString("id")
pathValue = Server.MapPath("/")
Response.AppendToLog "catalog"
%>`
	client.request("initialize", map[string]any{
		"processId": nil, "rootUri": pathToFileURI(root), "capabilities": map[string]any{},
	})
	openClassicASPDocument(t, client, uri, source)

	expected := map[string][]string{
		"Application.": {"Contents", "StaticObjects", "Contents.Remove", "Contents.RemoveAll", "Lock", "Unlock"},
		"Session.":     {"Contents", "StaticObjects", "CodePage", "LCID", "SessionID", "Timeout", "Abandon", "Contents.Remove", "Contents.RemoveAll"},
		"Response.":    {"Cookies", "Buffer", "CacheControl", "Charset", "ContentType", "Expires", "ExpiresAbsolute", "IsClientConnected", "Pics", "Status", "AddHeader", "AppendToLog", "BinaryWrite", "Clear", "End", "Flush", "Redirect", "Write"},
		"Request.":     {"QueryString", "Form", "Cookies", "ServerVariables", "ClientCertificate", "TotalBytes", "BinaryRead"},
		"Server.":      {"ScriptTimeout", "CreateObject", "Execute", "GetLastError", "HTMLEncode", "MapPath", "Transfer", "URLEncode"},
	}
	for owner, members := range expected {
		labels := completionLabels(client.request("textDocument/completion", map[string]any{
			"textDocument": map[string]any{"uri": uri},
			"position":     positionAt(source, strings.Index(source, owner)+len(owner)),
		}).Result)
		for _, member := range members {
			if !labels.contains(member) {
				t.Errorf("%s completions missing %s: %#v", owner, member, labels)
			}
		}
	}

	for _, expectedHover := range []string{
		vbscriptHoverCodeBlockJSON("(global) Dim requestValue As String"),
		vbscriptHoverCodeBlockJSON("(global) Dim pathValue As String"),
	} {
		assertRequestContains(t, client, "textDocument/hover", map[string]any{
			"textDocument": map[string]any{"uri": uri},
			"position":     positionAt(source, strings.LastIndex(source, hoverIdentifierFromVBScriptCodeBlockExpectation(expectedHover))),
		}, expectedHover)
	}
	assertRequestContains(t, client, "textDocument/hover", map[string]any{
		"textDocument": map[string]any{"uri": uri},
		"position":     positionAt(source, strings.Index(source, "QueryString")),
	}, "property Request.QueryString(name) As String")
	assertRequestContains(t, client, "textDocument/signatureHelp", map[string]any{
		"textDocument": map[string]any{"uri": uri},
		"position":     positionAt(source, strings.Index(source, `Request.QueryString("id"`)+len("Request.QueryString(")),
	}, "Request.QueryString(name)")
	assertRequestContains(t, client, "textDocument/signatureHelp", map[string]any{
		"textDocument": map[string]any{"uri": uri},
		"position":     positionAt(source, strings.Index(source, `Response.AppendToLog "catalog"`)+len(`Response.AppendToLog "catalog"`)),
	}, "Response.AppendToLog(string)")
}

func TestStdioParityCompletesFullWScriptCatalogWithoutASPLeakage(t *testing.T) {
	client := startStdioTestClient(t)
	defer client.close()

	root := t.TempDir()
	uri := pathToFileURI(filepath.Join(root, "global-object-catalog.vbs"))
	source := `WScript.
Response.
Application.
Dim output
Set output = WScript.StdOut
WScript.DisconnectObject output
`
	client.request("initialize", map[string]any{
		"processId": nil, "rootUri": pathToFileURI(root), "capabilities": map[string]any{},
	})
	if err := client.notify("textDocument/didOpen", map[string]any{
		"textDocument": map[string]any{"uri": uri, "languageId": "vbscript", "version": 1, "text": source},
	}); err != nil {
		t.Fatal(err)
	}
	client.waitForNotification("textDocument/publishDiagnostics", uri)

	members := completionLabels(client.request("textDocument/completion", map[string]any{
		"textDocument": map[string]any{"uri": uri},
		"position":     positionAt(source, len("WScript.")),
	}).Result)
	for _, member := range []string{"Arguments", "FullName", "Name", "Path", "ScriptFullName", "ScriptName", "StdErr", "StdIn", "StdOut", "Version", "ConnectObject", "CreateObject", "DisconnectObject", "Echo", "GetObject", "Quit", "Sleep"} {
		if !members.contains(member) {
			t.Errorf("WScript completions missing %s: %#v", member, members)
		}
	}
	for _, owner := range []string{"Response.", "Application."} {
		labels := completionLabels(client.request("textDocument/completion", map[string]any{
			"textDocument": map[string]any{"uri": uri},
			"position":     positionAt(source, strings.Index(source, owner)+len(owner)),
		}).Result)
		if len(labels) != 0 {
			t.Errorf("standalone %s completions leaked ASP members: %#v", owner, labels)
		}
	}
	assertRequestContains(t, client, "textDocument/hover", map[string]any{
		"textDocument": map[string]any{"uri": uri},
		"position":     positionAt(source, strings.Index(source, "StdOut")),
	}, "property WScript.StdOut As Object")
	assertRequestContains(t, client, "textDocument/signatureHelp", map[string]any{
		"textDocument": map[string]any{"uri": uri},
		"position":     positionAt(source, strings.Index(source, "WScript.DisconnectObject output")+len("WScript.DisconnectObject output")),
	}, "WScript.DisconnectObject(object)")
}

func TestVBScriptBuiltinCatalogInfersGlobalMemberTypes(t *testing.T) {
	tests := map[string]string{
		"Request.QueryString(\"id\")":                    "String",
		"Request.TotalBytes":                             "Number",
		"Request.BinaryRead(10)":                         "Array",
		"Response.ExpiresAbsolute":                       "Date",
		"Response.IsClientConnected":                     "Boolean",
		"Server.MapPath(\"/\")":                          "String",
		"Server.GetLastError()":                          "ASPError",
		"Server.CreateObject(\"ADODB.Recordset\")":       "ADODB.Recordset",
		"WScript.StdOut":                                 "Object",
		"WScript.CreateObject(\"Scripting.Dictionary\")": "Scripting.Dictionary",
	}
	for value, expected := range tests {
		if actual := inferVBValueType(value); actual != expected {
			t.Errorf("inferVBValueType(%q) = %q, want %q", value, actual, expected)
		}
	}
}

func TestVBScriptBuiltinCatalogTypeInferenceRespectsRuntime(t *testing.T) {
	asp := core.ParseDocument("file:///site/default.asp", `<% value = Request.QueryString("id") %>`, core.Settings{DefaultLanguage: "VBScript"})
	standalone := core.ParseDocument("file:///site/script.vbs", `value = WScript.StdOut`, core.Settings{DefaultLanguage: "VBScript"})
	for _, testCase := range []struct {
		name     string
		parsed   *core.ParsedDocument
		value    string
		expected string
	}{
		{name: "ASP accepts Request", parsed: asp, value: `Request.QueryString("id")`, expected: "String"},
		{name: "ASP rejects WScript", parsed: asp, value: "WScript.StdOut"},
		{name: "standalone accepts WScript", parsed: standalone, value: "WScript.StdOut", expected: "Object"},
		{name: "standalone rejects Request", parsed: standalone, value: `Request.QueryString("id")`},
	} {
		t.Run(testCase.name, func(t *testing.T) {
			if actual := inferVBValueTypeForDocument(testCase.parsed, testCase.value); actual != testCase.expected {
				t.Fatalf("inferVBValueTypeForDocument(%q) = %q, want %q", testCase.value, actual, testCase.expected)
			}
		})
	}
}

func lowerASCII(value string) string {
	bytes := []byte(value)
	for index, character := range bytes {
		if character >= 'A' && character <= 'Z' {
			bytes[index] = character + ('a' - 'A')
		}
	}
	return string(bytes)
}
