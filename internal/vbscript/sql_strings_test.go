package vbscript

import (
	"strings"
	"testing"

	"github.com/yottonoko/classic-web-system-lsp/internal/core"
	"github.com/yottonoko/classic-web-system-lsp/internal/lsp"
)

func parseSQLTestDocument(source string) *core.ParsedDocument {
	return core.ParseDocument("file:///site/sql.asp", source, core.Settings{DefaultLanguage: "VBScript"})
}

func sqlStringTexts(parsed *core.ParsedDocument) []string {
	var texts []string
	for _, fragment := range SQLStrings(parsed) {
		texts = append(texts, parsed.Text[fragment.Start:fragment.End])
	}
	return texts
}

func TestSQLStringsDetectsStatementsAndFragmentsConservatively(t *testing.T) {
	parsed := parseSQLTestDocument(`<%
sql = "SELECT id, name FROM users"
strSQL = strSQL & " AND deleted = 0"
strSQL = strSQL & "'"
cmd.CommandText = "exec sp_list " & id
conn.Execute "DELETE FROM logs WHERE id = " & id
rs.Open "select top 1 " & col & _
  " from items", conn
value = "INSERT INTO t (a) VALUES (1)"
title = "Select an item"
message = "Update your profile"
Response.Write "Select a category from the list"
conn.Open "Provider=SQLOLEDB;Data Source=x"
Server.Execute "from.asp"
queryString = "on=1"
sqlType = "mssql"
label = "Select * now" ' SELECT a FROM b
If mode = "select" Then note = "from here"
%>`)
	got := sqlStringTexts(parsed)
	want := []string{
		"SELECT id, name FROM users",
		" AND deleted = 0",
		"'",
		"exec sp_list ",
		"DELETE FROM logs WHERE id = ",
		"select top 1 ",
		" from items",
		"INSERT INTO t (a) VALUES (1)",
	}
	if strings.Join(got, "|") != strings.Join(want, "|") {
		t.Fatalf("SQL strings = %q, want %q", got, want)
	}
}

func TestSQLWordsSkipQuotedLiteralsAcrossFragmentsAndComments(t *testing.T) {
	parsed := parseSQLTestDocument(`<%
sql = "SELECT COUNT(*), [from], left FROM t WHERE name = 'select " & name & " from' AND x = @from -- where"
%>`)
	var keywords, functions, identifiers []string
	for _, fragment := range SQLStrings(parsed) {
		for _, word := range SQLWords(parsed.Text, fragment) {
			text := parsed.Text[word.Start:word.End]
			switch word.Kind {
			case SQLWordKeyword:
				keywords = append(keywords, text)
			case SQLWordFunction:
				functions = append(functions, text)
			default:
				identifiers = append(identifiers, text)
			}
		}
	}
	if strings.Join(keywords, ",") != "SELECT,left,FROM,WHERE,AND" {
		t.Fatalf("keywords = %q", keywords)
	}
	if strings.Join(functions, ",") != "COUNT" {
		t.Fatalf("functions = %q", functions)
	}
	if strings.Join(identifiers, ",") != "from,t,name,x" {
		t.Fatalf("identifiers = %q", identifiers)
	}
}

func TestSQLStringAtCoversUnterminatedLiteralOnItsLineOnly(t *testing.T) {
	source := "<%\nsql = \"SELECT * FROM users WHERE \nnext = 1\n%>"
	parsed := parseSQLTestDocument(source)
	fragment, ok := SQLStringAt(parsed, strings.Index(source, "WHERE ")+len("WHERE "))
	if !ok || parsed.Text[fragment.Start:fragment.End] != "SELECT * FROM users WHERE " {
		t.Fatalf("fragment = %q ok=%v", parsed.Text[fragment.Start:fragment.End], ok)
	}
	if _, ok := SQLStringAt(parsed, strings.Index(source, "next")); ok {
		t.Fatal("unterminated SQL string leaked onto the next line")
	}
}

func TestSemanticTokensHighlightSQLKeywordsInsideStrings(t *testing.T) {
	source := "<%\nsql = \"SELECT COUNT(id) FROM users\"\ntitle = \"Select an item\"\n%>"
	parsed := parseSQLTestDocument(source)
	data := SemanticTokens(parsed).Data
	type token struct{ line, character, length, tokenType int }
	var tokens []token
	line, character := 0, 0
	for index := 0; index+4 < len(data); index += 5 {
		if data[index] != 0 {
			character = 0
		}
		line += data[index]
		character += data[index+1]
		tokens = append(tokens, token{line, character, data[index+2], data[index+3]})
	}
	has := func(expected token) bool {
		for _, candidate := range tokens {
			if candidate == expected {
				return true
			}
		}
		return false
	}
	for _, expected := range []token{
		{1, 7, 6, semanticKeyword},
		{1, 14, 5, semanticFunction},
		{1, 24, 4, semanticKeyword},
	} {
		if !has(expected) {
			t.Errorf("missing SQL token %+v in %+v", expected, tokens)
		}
	}
	for _, candidate := range tokens {
		if candidate.line == 2 && candidate.character > 8 {
			t.Errorf("non-SQL string produced token %+v", candidate)
		}
	}
}

func TestSQLInjectionDiagnosticsReportUnsanitizedRequestValues(t *testing.T) {
	source := `<%
id = Request("id")
name = Trim(Request.Form("name"))
safeId = CLng(Request("id"))
checked = Request("checked")
If Not IsNumeric(checked) Then Response.End
mixed = Request("mixed")
mixed = "0"
sql = "SELECT * FROM users WHERE id = " & id
sql = sql & " AND name = '" & name & "'"
sql = sql & " AND age = " & Request.QueryString("age")
sql = sql & " AND city = '" & LCase(Trim(Request("city"))) & "'"
conn.Execute("DELETE FROM t WHERE id = " & Request("del"))
sql = sql & " AND a = " & safeId
sql = sql & " AND b = " & CLng(Request("b"))
sql = sql & " AND c = '" & Replace(Request("c"), "'", "''") & "'"
sql = sql & " AND d = " & Escape(Request("d"))
sql = sql & " AND e = " & checked & " AND f = " & mixed
If Request("g") <> "" Then sql = sql & " AND g = 1"
Response.Write "SELECT " & Request("h") & " FROM menu"
label = "Hello " & Request("i")
%>`
	parsed := parseSQLTestDocument(source)
	var names []string
	for _, diagnostic := range SQLInjectionDiagnostics(parsed, lsp.DiagnosticSeverityInformation) {
		if diagnostic.Code != "sqlInjection" || diagnostic.Source != "asp-lsp-vbscript-sql" || diagnostic.Severity != lsp.DiagnosticSeverityInformation {
			t.Fatalf("unexpected diagnostic %+v", diagnostic)
		}
		data, _ := diagnostic.Data.(map[string]any)
		names = append(names, data["name"].(string))
	}
	want := []string{`id`, `name`, `Request.QueryString("age")`, `Request("city")`, `Request("del")`}
	if strings.Join(names, "|") != strings.Join(want, "|") {
		t.Fatalf("reported = %q, want %q", names, want)
	}
}
