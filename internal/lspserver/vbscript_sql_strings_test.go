package lspserver

import (
	"path/filepath"
	"strings"
	"testing"
)

func TestStdioCompletesSQLInsideVBScriptStrings(t *testing.T) {
	client := startStdioTestClient(t)
	defer client.close()

	root := t.TempDir()
	uri := pathToFileURI(filepath.Join(root, "sql-completion.asp"))
	source := `<%
sql = "SELECT user_id, user_name FROM users INNER JOIN orders ON users.user_id = orders.owner_id"
sql2 = "select * from "
sql3 = "SELECT  FROM orders wh"
sql4 = "SELECT * FROM users WHERE user_name = 'sel"
title = "Hello wor"
%>`
	client.request("initialize", map[string]any{
		"processId": nil, "rootUri": pathToFileURI(root), "capabilities": map[string]any{},
	})
	openClassicASPDocument(t, client, uri, source)
	complete := func(marker string, context map[string]any) labelList {
		params := map[string]any{
			"textDocument": map[string]any{"uri": uri},
			"position":     positionAt(source, strings.Index(source, marker)+len(marker)),
		}
		if context != nil {
			params["context"] = context
		}
		return completionLabels(client.request("textDocument/completion", params).Result)
	}

	afterFrom := complete(`"select * from `, map[string]any{"triggerKind": 2, "triggerCharacter": " "})
	for _, table := range []string{"users", "orders"} {
		if !afterFrom.contains(table) {
			t.Errorf("table completions missing %s: %#v", table, afterFrom)
		}
	}
	if afterFrom.contains("user_name") || afterFrom.contains("Dim") {
		t.Errorf("table context offered columns or VBScript keywords: %#v", afterFrom)
	}

	selectList := complete(`sql3 = "SELECT `, nil)
	for _, label := range []string{"user_id", "user_name", "owner_id", "users", "DISTINCT", "COUNT", "ORDER BY"} {
		if !selectList.contains(label) {
			t.Errorf("select list completions missing %s: %#v", label, selectList)
		}
	}
	if selectList.contains("Dim") || selectList.contains("Response") {
		t.Errorf("SQL string offered VBScript completions: %#v", selectList)
	}

	lowerCase := complete(`FROM orders wh`, nil)
	if !lowerCase.contains("where") || lowerCase.contains("WHERE") {
		t.Errorf("keyword case did not follow the typed prefix: %#v", lowerCase)
	}
	if quoted := complete(`user_name = 'sel`, nil); len(quoted) != 0 {
		t.Errorf("SQL literal content offered completions: %#v", quoted)
	}
	if plain := complete(`"Hello wor`, nil); len(plain) != 0 {
		t.Errorf("plain string offered completions: %#v", plain)
	}
	if code := complete("title = ", map[string]any{"triggerKind": 2, "triggerCharacter": " "}); len(code) != 0 {
		t.Errorf("space trigger outside SQL offered completions: %#v", code)
	}
}

func TestStdioReportsSQLInjectionOnlyWhenConfigured(t *testing.T) {
	client := startStdioTestClient(t)
	defer client.close()

	root := t.TempDir()
	uri := pathToFileURI(filepath.Join(root, "sql-injection.asp"))
	source := `<%
Dim sql, conn
sql = "SELECT * FROM users WHERE id = " & Request("id")
sql = sql & " AND age = " & CLng(Request("age"))
conn.Execute sql
%>`
	client.request("initialize", map[string]any{
		"processId": nil, "rootUri": pathToFileURI(root), "capabilities": map[string]any{},
	})
	diagnosticsMessage := openClassicASPDocumentWithDiagnostics(t, client, uri, source)
	if strings.Contains(string(diagnosticsMessage.Params), "sqlInjection") {
		t.Fatalf("SQL injection diagnostics must be off by default: %s", diagnosticsMessage.Params)
	}

	notifyConfiguration(t, client, map[string]any{"aspLsp": map[string]any{"locale": "ja", "vbscript": map[string]any{"sqlInjectionDiagnostics": "warning"}}})
	enabled := mustJSONText(t, client.request("textDocument/diagnostic", map[string]any{
		"textDocument": map[string]any{"uri": uri},
	}).Result)
	if strings.Count(enabled, `"sqlInjection"`) != 1 || !strings.Contains(enabled, `"severity":2`) ||
		!strings.Contains(enabled, `Request の値 'Request(\"id\")' が SQL 文字列に連結されています`) {
		t.Fatalf("unexpected SQL injection diagnostics: %s", enabled)
	}

	notifyConfiguration(t, client, map[string]any{"aspLsp": map[string]any{"vbscript": map[string]any{"sqlInjectionDiagnostics": "unexpected"}}})
	disabled := mustJSONText(t, client.request("textDocument/diagnostic", map[string]any{
		"textDocument": map[string]any{"uri": uri},
	}).Result)
	if strings.Contains(disabled, "sqlInjection") {
		t.Fatalf("an unknown setting value must turn the diagnostic off: %s", disabled)
	}
}
