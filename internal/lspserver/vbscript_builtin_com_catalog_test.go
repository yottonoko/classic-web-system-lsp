package lspserver

import (
	"io"
	"path/filepath"
	"strings"
	"testing"

	"github.com/yottonoko/classic-web-system-lsp/internal/core"
	"github.com/yottonoko/classic-web-system-lsp/internal/lsp"
)

func TestVBScriptBuiltinComCatalogCoversCommonProgIDs(t *testing.T) {
	for typeName, members := range map[string][]string{
		"ADODB.Connection":               {"Open", "Execute", "BeginTrans", "CommitTrans", "RollbackTrans", "Errors", "ConnectionString", "State", "Close"},
		"ADODB.Recordset":                {"Open", "EOF", "BOF", "MoveNext", "Fields", "RecordCount", "GetRows", "AddNew", "Update", "PageSize", "Close"},
		"ADODB.Command":                  {"ActiveConnection", "CommandText", "CommandType", "CreateParameter", "Parameters", "Execute"},
		"ADODB.Parameters":               {"Append", "Item", "Count"},
		"ADODB.Field":                    {"Name", "Value", "Type"},
		"ADODB.Error":                    {"Number", "Description", "SQLState"},
		"ADODB.Stream":                   {"Charset", "LoadFromFile", "SaveToFile", "ReadText", "WriteText", "Read", "Write", "Position"},
		"Scripting.FileSystemObject":     {"FileExists", "FolderExists", "GetFolder", "CreateTextFile", "OpenTextFile", "DeleteFile", "BuildPath"},
		"Scripting.Folder":               {"Files", "SubFolders", "Path"},
		"Scripting.Drive":                {"FreeSpace", "IsReady"},
		"Scripting.TextStream":           {"ReadAll", "ReadLine", "WriteLine", "AtEndOfStream", "Close"},
		"Scripting.Dictionary":           {"Add", "Exists", "Item", "Keys", "Items", "Remove", "RemoveAll", "Count", "CompareMode"},
		"MSXML2.ServerXMLHTTP":           {"open", "send", "setRequestHeader", "responseText", "status", "setTimeouts"},
		"MSXML2.ServerXMLHTTP.6.0":       {"open", "waitForResponse"},
		"Msxml2.XMLHTTP.3.0":             {"open", "responseXML"},
		"Microsoft.XMLHTTP":              {"send"},
		"WinHttp.WinHttpRequest.5.1":     {"Open", "Send", "ResponseText"},
		"MSXML2.DOMDocument":             {"load", "loadXML", "selectNodes", "selectSingleNode", "documentElement", "parseError", "async"},
		"MSXML2.DOMDocument.6.0":         {"setProperty"},
		"Microsoft.XMLDOM":               {"createElement"},
		"MSXML2.FreeThreadedDOMDocument": {"save"},
		"MSXML2.IXMLDOMNode":             {"getAttribute", "text", "childNodes"},
		"CDO.Message":                    {"From", "To", "Subject", "TextBody", "HTMLBody", "Configuration", "AddAttachment", "Send"},
		"CDO.Configuration":              {"Fields"},
		"CDONTS.NewMail":                 {"From", "To", "Body", "Send"},
		"WScript.Shell":                  {"Run", "Exec", "RegRead", "ExpandEnvironmentStrings"},
		"WScript.Network":                {"ComputerName", "UserName", "MapNetworkDrive"},
	} {
		catalog := vbscriptBuiltinTypeMembers(typeName)
		for _, member := range members {
			if _, ok := catalog[strings.ToLower(member)]; !ok {
				t.Errorf("%s is missing %s", typeName, member)
			}
		}
	}
	if members := vbscriptBuiltinTypeMembers("Vendor.Unknown.1"); len(members) != 0 {
		t.Fatalf("unknown ProgID resolved to a stub: %#v", members)
	}
}

func TestVBScriptBuiltinComCatalogMembersAreDocumentedAndConsistent(t *testing.T) {
	catalog := vbscriptBuiltinComCatalog()
	for _, stub := range vbscriptBuiltinComStubTypes() {
		seen := map[string]struct{}{}
		for _, member := range stub.members {
			key := strings.ToLower(member.Name)
			if _, duplicate := seen[key]; duplicate {
				t.Errorf("%s declares %s twice", stub.name, member.Name)
			}
			seen[key] = struct{}{}
			if member.Docs.EN == "" || member.Docs.JA == "" {
				t.Errorf("%s.%s is missing localized documentation", stub.name, member.Name)
			}
			if member.TypeName == "" {
				t.Errorf("%s.%s is missing a type", stub.name, member.Name)
			}
			if strings.Contains(member.TypeName, ".") {
				if _, ok := catalog[strings.ToLower(member.TypeName)]; !ok {
					t.Errorf("%s.%s returns %s, which has no stub", stub.name, member.Name, member.TypeName)
				}
			}
			if member.Kind == lsp.CompletionItemKindMethod && member.Signature == "" {
				t.Errorf("%s.%s is a method without a signature", stub.name, member.Name)
			}
			optionalSeen := false
			for _, parameter := range vbscriptBuiltinMemberParameters(catalog[strings.ToLower(stub.name)][key]) {
				if strings.ContainsAny(parameter.Name, "[] ") {
					t.Errorf("%s.%s has a malformed parameter %q", stub.name, member.Name, parameter.Name)
				}
				if optionalSeen && !parameter.Optional {
					t.Errorf("%s.%s has required parameter %q after an optional one", stub.name, member.Name, parameter.Name)
				}
				optionalSeen = optionalSeen || parameter.Optional
			}
		}
	}
}

func TestVBScriptBuiltinMemberOptionalParametersRelaxArgumentRange(t *testing.T) {
	open := vbscriptBuiltinTypeMembers("ADODB.Recordset")["open"]
	typed := vbscriptTypedMemberFromBuiltin(open)
	if typed.MinimumParameterCount != 0 || typed.MaximumParameterCount != 5 || !typed.ParameterRangeKnown {
		t.Fatalf("Recordset.Open range = %d-%d known=%v", typed.MinimumParameterCount, typed.MaximumParameterCount, typed.ParameterRangeKnown)
	}
	execute := vbscriptTypedMemberFromBuiltin(vbscriptBuiltinTypeMembers("ADODB.Connection")["execute"])
	if execute.MinimumParameterCount != 1 || execute.MaximumParameterCount != 3 {
		t.Fatalf("Connection.Execute range = %d-%d", execute.MinimumParameterCount, execute.MaximumParameterCount)
	}
	mapPath := vbscriptTypedMemberFromBuiltin(vbscriptBuiltinTypeMembers("Server")["mappath"])
	if mapPath.MinimumParameterCount != 1 || mapPath.MaximumParameterCount != 1 {
		t.Fatalf("Server.MapPath range = %d-%d", mapPath.MinimumParameterCount, mapPath.MaximumParameterCount)
	}
	if label := (&Server{}).vbscriptBuiltinMemberSignatureInformation("ADODB.Connection", vbscriptBuiltinTypeMembers("ADODB.Connection")["execute"]).Label; label != "ADODB.Connection.Execute(commandText, [recordsAffected], [options])" {
		t.Fatalf("signature label = %q", label)
	}
}

func TestVBScriptBuiltinMemberChainType(t *testing.T) {
	variables := map[string]string{
		"conn": "ADODB.Connection",
		"rs":   "ADODB.Recordset",
		"fso":  "Scripting.FileSystemObject",
		"xml":  "MSXML2.DOMDocument.6.0",
		"re":   "RegExp",
		"any":  "ADODB.Recordset | ADODB.Stream",
	}
	variableType := func(name string) string { return variables[strings.ToLower(name)] }
	for _, test := range []struct {
		value    string
		typeName string
		resolved bool
	}{
		{`conn.Execute("select "")"" from t")`, "ADODB.Recordset", true},
		{`conn.Execute(sql).Fields`, "ADODB.Fields", true},
		{`rs.Fields("name")`, "ADODB.Field", true},
		{`rs.Fields("name").Value`, "Variant", true},
		{`rs.Fields.Item(0).Name`, "String", true},
		{`conn.Errors(0).Description`, "String", true},
		{`fso.GetFolder(path).Files`, "Scripting.Files", true},
		{`fso . OpenTextFile (path, 1) . ReadAll ()`, "String", true},
		{`xml.selectSingleNode("//a").getAttribute("id")`, "Variant", true},
		{`xml.documentElement.childNodes.item(0)`, "MSXML2.IXMLDOMNode", true},
		{`rs.EOF And rs.BOF`, "", false},
		{`rs.Missing`, "", false},
		{`rs("name")`, "", false},
		{`re.Execute(text)`, "Matches", true},
		{`re.Execute(text).Item(0).SubMatches`, "SubMatches", true},
		{`any.Open()`, "", false},
		{`unknown.Execute(sql)`, "", false},
		{`conn.Execute(sql`, "", false},
	} {
		typeName, resolved := vbscriptBuiltinMemberChainType(test.value, variableType)
		if typeName != test.typeName || resolved != test.resolved {
			t.Errorf("%s = (%q, %v), want (%q, %v)", test.value, typeName, resolved, test.typeName, test.resolved)
		}
	}
}

func TestVBScriptStrictDiagnosticsAcceptBuiltinComMemberCalls(t *testing.T) {
	source := `<%
Dim conn, rs, fso, dict, http, vendor, other
Set conn = Server.CreateObject("ADODB.Connection")
conn.open(dsn)
Set rs = conn.execute("select 1", , adCmdText)
Set fso = Server.CreateObject("Scripting.FileSystemObject")
Set dict = Server.CreateObject("Scripting.Dictionary")
Set http = Server.CreateObject("MSXML2.ServerXMLHTTP.6.0")
Set vendor = Server.CreateObject("Vendor.Component")
found = fso.fileExists("a")
keys = dict.keys()
value = rs.fields("a")
rows = rs.getRows()
rs.open()
http.open("GET", url, False)
future = rs.futureMember(1)
result = vendor.run(1)
Set xml = Server.CreateObject("MSXML2.DOMDocument")
Set nodes = xml.documentElement.selectNodes("//item")
chunk = rs.fields("a").getChunk(10)
first = GetConnection().execute("select 1")
With conn
  .open(dsn)
  Set other = .execute("select 1")
End With
tooMany = dict.exists("a", "b")
tooFew = conn.execute()
chainTooFew = rs.fields.item()
%>`
	parsed := core.ParseDocument("file:///site/com-calls.asp", source, core.Settings{DefaultLanguage: "VBScript"})
	server := New(strings.NewReader(""), io.Discard, io.Discard)
	server.settings.VBScriptTypeChecking = "strict"
	var unknown, mismatched []string
	for _, diagnostic := range server.vbscriptTypeDiagnostics(parsed) {
		data, _ := diagnostic.Data.(map[string]any)
		name, _ := data["name"].(string)
		switch diagnosticCodeString(diagnostic) {
		case "unknownCall":
			unknown = append(unknown, name)
		case "argumentCountMismatch":
			mismatched = append(mismatched, name)
		}
	}
	if len(unknown) != 0 {
		t.Fatalf("built-in COM member calls reported as unknown: %v", unknown)
	}
	if strings.Join(mismatched, ",") != "dict.exists,conn.execute,rs.fields.item" {
		t.Fatalf("argument count diagnostics = %v", mismatched)
	}
}

func TestStdioCompletesAndDocumentsBuiltinComMembersThroughChains(t *testing.T) {
	client := startStdioTestClient(t)
	defer client.close()

	root := t.TempDir()
	uri := pathToFileURI(filepath.Join(root, "com-chain.asp"))
	source := `<%
Dim conn, rs, fields, field, http, xml, node, mail, shell, folder
Set conn = Server.CreateObject("ADODB.Connection")
Set rs = conn.Execute("select 1")
Set fields = rs.Fields
Set field = rs.Fields("name")
Set http = Server.CreateObject("MSXML2.ServerXMLHTTP.6.0")
Set xml = Server.CreateObject("Msxml2.DOMDocument.6.0")
Set node = xml.selectSingleNode("//item")
Set mail = Server.CreateObject("CDO.Message")
Set shell = Server.CreateObject("WScript.Shell")
Set folder = Server.CreateObject("Scripting.FileSystemObject").GetFolder("c:\")
conn.Open dsn
rs.Open sql, conn, adOpenStatic
total = conn.Errors.Count
rs.Fields.Append("name", adVarChar, 50)
rs.Fields("name").
conn.Errors.
xml.documentElement.childNodes.
conn.
rs.
fields.
field.
http.
xml.
node.
mail.
shell.
%>`
	client.request("initialize", map[string]any{
		"processId": nil, "rootUri": pathToFileURI(root), "capabilities": map[string]any{},
	})
	notifyConfiguration(t, client, map[string]any{"aspLsp": map[string]any{"locale": "ja"}})
	openClassicASPDocument(t, client, uri, source)

	for _, testCase := range []struct {
		trigger string
		labels  []string
	}{
		{"\nconn.\n", []string{"BeginTrans", "Execute", "Errors"}},
		{"\nrs.\n", []string{"MoveNext", "EOF", "RecordCount"}},
		{"\nfields.\n", []string{"Count", "Item"}},
		{"\nfield.\n", []string{"Value", "Name"}},
		{"\nhttp.\n", []string{"setRequestHeader", "responseText", "setTimeouts"}},
		{"\nxml.\n", []string{"loadXML", "selectNodes", "parseError"}},
		{"\nnode.\n", []string{"getAttribute", "text"}},
		{"\nmail.\n", []string{"Subject", "HTMLBody", "Send"}},
		{"\nshell.\n", []string{"Run", "ExpandEnvironmentStrings"}},
		{"\nrs.Fields(\"name\").\n", []string{"Value", "DefinedSize"}},
		{"\nconn.Errors.\n", []string{"Count", "Clear"}},
		{"\nxml.documentElement.childNodes.\n", []string{"length", "item"}},
	} {
		labels := completionLabels(client.request("textDocument/completion", map[string]any{
			"textDocument": map[string]any{"uri": uri},
			"position":     positionAt(source, strings.Index(source, testCase.trigger)+len(testCase.trigger)-1),
		}).Result)
		for _, label := range testCase.labels {
			if !labels.contains(label) {
				t.Errorf("%q completions missing %s: %#v", testCase.trigger, label, labels)
			}
		}
	}
	assertRequestContains(t, client, "textDocument/hover", map[string]any{
		"textDocument": map[string]any{"uri": uri},
		"position":     positionAt(source, strings.Index(source, "Execute(")),
	}, "クエリ、SQL 文、またはストアドプロシージャを実行し")
	assertRequestContains(t, client, "textDocument/hover", map[string]any{
		"textDocument": map[string]any{"uri": uri},
		"position":     positionAt(source, strings.Index(source, "Execute(")),
	}, "Function Connection.Execute(commandText, [recordsAffected], [options]) As ADODB.Recordset")
	assertRequestContains(t, client, "textDocument/signatureHelp", map[string]any{
		"textDocument": map[string]any{"uri": uri},
		"position":     positionAt(source, strings.Index(source, "rs.Open sql, conn, ")+len("rs.Open sql, conn, ")),
	}, "ADODB.Recordset.Open([source], [activeConnection], [cursorType], [lockType], [options])")
	assertRequestContains(t, client, "textDocument/hover", map[string]any{
		"textDocument": map[string]any{"uri": uri},
		"position":     positionAt(source, strings.Index(source, "Count\n")),
	}, "property ADODB.Errors.Count As Number")
	assertRequestContains(t, client, "textDocument/signatureHelp", map[string]any{
		"textDocument": map[string]any{"uri": uri},
		"position":     positionAt(source, strings.Index(source, `Append("name", `)+len(`Append("name", `)),
	}, "ADODB.Fields.Append(name, type, [definedSize], [attrib], [fieldValue])")
}
