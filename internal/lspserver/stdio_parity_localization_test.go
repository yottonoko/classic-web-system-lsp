package lspserver

import (
	"os"
	"path/filepath"
	"strings"
	"testing"

	"github.com/yottonoko/classic-web-system-lsp/internal/lsp"
)

func TestStdioParityLocalizesASPLSPDiagnosticsCodeActionsAndCodeLens(t *testing.T) {
	client := startStdioTestClient(t)
	defer client.close()

	root := t.TempDir()
	uri := pathToFileURI(filepath.Join(root, "go-localized.asp"))
	marked := markedDocument(`<!-- #include file="missing.inc" -->
<%
Option Explicit
Dim initialized = 1
Function Save()
End Function
Response.Write miss<<<caret>>>ingName
%>`)
	initialize := client.request("initialize", map[string]any{
		"processId":    nil,
		"rootUri":      pathToFileURI(root),
		"locale":       "ja-JP",
		"capabilities": map[string]any{},
	})
	if serialized := mustJSONText(t, initialize.Result); !strings.Contains(serialized, "codeLensProvider") {
		t.Fatalf("initialize result missing codeLensProvider: %s", serialized)
	}
	notifyConfiguration(t, client, map[string]any{"aspLsp": map[string]any{"locale": "auto"}})

	diagnosticsMessage := openClassicASPDocumentWithDiagnostics(t, client, uri, marked.Text)
	if !strings.Contains(string(diagnosticsMessage.Params), "解決できません") {
		diagnosticsMessage = waitForDiagnosticsContaining(t, client, "解決できません")
	}
	serializedDiagnostics := string(diagnosticsMessage.Params)
	for _, expected := range []string{"解決できません", "宣言されていません", "初期値を含められません"} {
		if !strings.Contains(serializedDiagnostics, expected) {
			t.Fatalf("localized diagnostics missing %q: %s", expected, serializedDiagnostics)
		}
	}
	diagnostics := diagnosticsFromPublishMessage(t, diagnosticsMessage)

	actions := client.request("textDocument/codeAction", map[string]any{
		"textDocument": map[string]any{"uri": uri},
		"range":        mapPositionRange(marked.Position, marked.Position),
		"context": map[string]any{
			"diagnostics": diagnostics,
			"only":        []string{"quickfix"},
		},
	})
	serializedActions := mustJSONText(t, actions.Result)
	for _, expected := range []string{"missingName を Dim で宣言", "不足している include missing.inc を作成"} {
		if !strings.Contains(serializedActions, expected) {
			t.Fatalf("localized code actions missing %q: %s", expected, serializedActions)
		}
	}

	splitDimActions := client.request("textDocument/codeAction", map[string]any{
		"textDocument": map[string]any{"uri": uri},
		"range": mapPositionRange(
			positionAt(marked.Text, strings.Index(marked.Text, "initialized")),
			positionAt(marked.Text, strings.Index(marked.Text, "initialized")+len("initialized")),
		),
		"context": map[string]any{"diagnostics": []lsp.Diagnostic{}, "only": []string{"quickfix"}},
	})
	if serialized := mustJSONText(t, splitDimActions.Result); !strings.Contains(serialized, "初期化つき Dim 宣言を分割") {
		t.Fatalf("localized split Dim action missing: %s", serialized)
	}

	codeLens := client.request("textDocument/codeLens", map[string]any{
		"textDocument": map[string]any{"uri": uri},
	})
	var lenses []lsp.CodeLens
	mustDecodeResult(t, codeLens.Result, &lenses)
	var referenceLens *lsp.CodeLens
	for i := range lenses {
		if strings.Contains(mustJSONText(t, lenses[i].Data), `"kind":"vbscript-reference"`) {
			referenceLens = &lenses[i]
			break
		}
	}
	if referenceLens == nil {
		t.Fatalf("reference CodeLens missing: %s", mustJSONText(t, codeLens.Result))
	}
	resolvedCodeLens := client.request("codeLens/resolve", *referenceLens)
	if serialized := mustJSONText(t, resolvedCodeLens.Result); !strings.Contains(serialized, "件の参照") || strings.Contains(serialized, "解析済みのみ") {
		t.Fatalf("localized CodeLens title missing: %s", serialized)
	}

	unknown := client.request("workspace/executeCommand", map[string]any{
		"command": "aspLsp.unknown",
	})
	if serialized := mustJSONText(t, unknown.Result); !strings.Contains(serialized, "不明なコマンド") {
		t.Fatalf("localized unknown command missing: %s", serialized)
	}

	notifyConfiguration(t, client, map[string]any{"aspLsp": map[string]any{"locale": "en"}})
	englishDiagnostics := client.request("textDocument/diagnostic", map[string]any{
		"textDocument": map[string]any{"uri": uri},
	})
	if serialized := mustJSONText(t, englishDiagnostics.Result); !strings.Contains(serialized, "could not be resolved") {
		t.Fatalf("English diagnostics after locale change missing: %s", serialized)
	}
}

func TestStdioParityDoesNotSuggestIncludesForUndeclaredVBScriptSymbols(t *testing.T) {
	client := startStdioTestClient(t)
	defer client.close()

	root := t.TempDir()
	incDir := filepath.Join(root, "inc")
	if err := os.MkdirAll(incDir, 0o755); err != nil {
		t.Fatal(err)
	}
	if err := os.WriteFile(filepath.Join(incDir, "helpers.inc"), []byte(`<%
Function SharedHelper()
End Function
%>`), 0o644); err != nil {
		t.Fatal(err)
	}
	uri := pathToFileURI(filepath.Join(root, "default.asp"))
	marked := markedDocument(`<%
Option Explicit
Response.Write Shared<<<caret>>>Helper
%>`)
	client.request("initialize", map[string]any{
		"processId":    nil,
		"rootUri":      pathToFileURI(root),
		"capabilities": map[string]any{},
	})
	diagnosticsMessage := openClassicASPDocumentWithDiagnostics(t, client, uri, marked.Text)
	diagnostics := diagnosticsFromPublishMessage(t, diagnosticsMessage)
	if serialized := mustJSONText(t, diagnostics); !strings.Contains(serialized, "SharedHelper") {
		t.Fatalf("SharedHelper diagnostic missing: %s", serialized)
	}

	actions := client.request("textDocument/codeAction", map[string]any{
		"textDocument": map[string]any{"uri": uri},
		"range":        mapPositionRange(marked.Position, marked.Position),
		"context": map[string]any{
			"diagnostics": diagnostics,
			"only":        []string{"quickfix"},
		},
	})
	serialized := mustJSONText(t, actions.Result)
	if !strings.Contains(serialized, "Declare SharedHelper with Dim") {
		t.Fatalf("undeclared quick fix missing Dim declaration: %s", serialized)
	}
	for _, unexpected := range []string{"Include /inc/helpers.inc for SharedHelper", "<!-- #include"} {
		if strings.Contains(serialized, unexpected) {
			t.Fatalf("undeclared quick fix should not suggest include %q: %s", unexpected, serialized)
		}
	}
}

func TestStdioParityLocalizesASPParserDiagnostics(t *testing.T) {
	client := startStdioTestClient(t)
	defer client.close()

	root := t.TempDir()
	uri := pathToFileURI(filepath.Join(root, "go-localized-parser.asp"))
	client.request("initialize", map[string]any{
		"processId":    nil,
		"rootUri":      pathToFileURI(root),
		"locale":       "ja-JP",
		"capabilities": map[string]any{},
	})
	diagnosticsMessage := openClassicASPDocumentWithDiagnostics(t, client, uri, "<html><% Response.Write 1")
	serialized := string(diagnosticsMessage.Params)
	if !strings.Contains(serialized, "Classic ASP ブロックに閉じ区切り") {
		t.Fatalf("localized ASP parser diagnostic missing: %s", serialized)
	}
}

func TestDiagnosticLocalizationMatchesLegacyCatalog(t *testing.T) {
	testCases := []struct {
		name, source, code, message, english, japanese string
		data                                           map[string]any
	}{
		{name: "parser", source: "asp-lsp-go", message: "Expected closing %> before end of file.", english: "Classic ASP block is missing a closing %> delimiter.", japanese: "Classic ASP ブロックに閉じ区切り %> がありません。"},
		{name: "undeclared", source: "asp-lsp-vbscript", data: map[string]any{"name": "missing"}, english: "'missing' is not declared under Option Explicit.", japanese: "'missing' は Option Explicit のもとで宣言されていません。"},
		{name: "unused parameter", source: "asp-lsp-vbscript-unused", data: map[string]any{"name": "value", "kind": "parameter"}, english: "Parameter 'value' is never used.", japanese: "パラメーター 'value' は使われていません。"},
		{name: "unused symbol", source: "asp-lsp-vbscript-unused", data: map[string]any{"name": "value", "kind": "variable"}, english: "'value' is declared but never used.", japanese: "'value' は宣言されていますが使われていません。"},
		{name: "dead code", source: "asp-lsp-vbscript-dead-code", english: "This VBScript code is unreachable.", japanese: "この VBScript code には到達できません。"},
		{name: "initialized declaration", source: "asp-lsp-vbscript-syntax", code: "initializedDeclaration", data: map[string]any{"declarationKind": "private"}, english: "VBScript Private declarations cannot include initializers.", japanese: "VBScript の Private 宣言には初期値を含められません。"},
		{name: "typed declaration", source: "asp-lsp-vbscript-syntax", code: "typedDeclaration", data: map[string]any{"declarationKind": "dim"}, english: "VBScript Dim declarations cannot include As types.", japanese: "VBScript の Dim 宣言には As 型指定を含められません。"},
		{name: "call", source: "asp-lsp-vbscript-syntax", code: "callStatementRequiresParentheses", data: map[string]any{"name": "Render"}, english: "VBScript call syntax is invalid for 'Render'.", japanese: "'Render' の VBScript 呼び出し構文が無効です。"},
		{name: "missing then", source: "asp-lsp-vbscript-syntax", code: "missingThen", english: "VBScript If statements must include Then.", japanese: "VBScript の If statement には Then が必要です。"},
		{name: "missing condition", source: "asp-lsp-vbscript-syntax", code: "missingIfCondition", english: "VBScript If statements must include a condition.", japanese: "VBScript の If statement には条件式が必要です。"},
		{name: "invalid condition", source: "asp-lsp-vbscript-syntax", code: "invalidIfCondition", english: "VBScript If condition syntax is invalid.", japanese: "VBScript の If 条件式の構文が無効です。"},
		{name: "missing end if", source: "asp-lsp-vbscript-syntax", code: "missingEndIf", english: "VBScript multiline If block is missing End If.", japanese: "VBScript の multiline If block に End If がありません。"},
		{name: "missing block terminator", source: "asp-lsp-vbscript-syntax", code: "missingEndFunction", data: map[string]any{"kind": "Function", "terminator": "End Function"}, english: "VBScript Function block is missing End Function.", japanese: "VBScript の Function block に End Function がありません。"},
		{name: "invalid on error", source: "asp-lsp-vbscript-syntax", code: "invalidOnErrorStatement", english: "VBScript On Error statements must be 'On Error Resume Next' or 'On Error GoTo 0'.", japanese: "VBScript の On Error statement は 'On Error Resume Next' または 'On Error GoTo 0' にしてください。"},
		{name: "set scalar", source: "asp-lsp-vbscript-type", code: "setScalar", data: map[string]any{"name": "title", "type": "String"}, english: "Set assigns an object reference, but 'title' receives String.", japanese: "Set はオブジェクト参照を代入しますが、'title' は String を受け取っています。"},
		{name: "object needs set", source: "asp-lsp-vbscript-type", code: "objectNeedsSet", data: map[string]any{"name": "widget"}, english: "Object assignment to 'widget' should use Set.", japanese: "'widget' へのオブジェクト代入には Set が必要です。"},
		{name: "type mismatch", source: "asp-lsp-vbscript-type", code: "typeMismatch", data: map[string]any{"name": "count", "expected": "Number", "actual": "String"}, english: "Type mismatch: 'count' is Number, but assigned String.", japanese: "型が一致しません: 'count' は Number ですが、String が代入されています。"},
		{name: "union type mismatch", source: "asp-lsp-vbscript-type", code: "typeMismatch", data: map[string]any{"name": "value", "expected": `"ready" | 200 | ` + "`page-${String}.asp`", "actual": "Boolean"}, english: "Type mismatch: 'value' is \"ready\" | 200 | `page-${String}.asp`, but assigned Boolean.", japanese: "型が一致しません: 'value' は \"ready\" | 200 | `page-${String}.asp` ですが、Boolean が代入されています。"},
		{name: "malformed type annotation", source: "asp-lsp-vbscript-type", code: "malformedTypeAnnotation", data: map[string]any{"annotation": "@type value As String |"}, english: "Malformed VBScript type annotation: '@type value As String |'.", japanese: "VBScript の型注釈が不正です: '@type value As String |'。"},
		{name: "unknown call", source: "asp-lsp-vbscript-type", code: "unknownCall", data: map[string]any{"name": "missingCall"}, english: "Call target 'missingCall' is not known.", japanese: "呼び出し先 'missingCall' は不明です。"},
		{name: "argument count", source: "asp-lsp-vbscript-type", code: "argumentCountMismatch", data: map[string]any{"name": "Open", "expected": 2, "actual": 1}, english: "Argument count mismatch for 'Open': expected 2, got 1.", japanese: "'Open' の引数の数が一致しません: 期待値 2、実際 1。"},
		{name: "optional argument range", source: "asp-lsp-vbscript-type", code: "argumentCountMismatch", data: map[string]any{"name": "Open", "expected": 2, "expectedMin": 1, "expectedMax": 2, "actual": 0}, english: "Argument count mismatch for 'Open': expected 1-2, got 0.", japanese: "'Open' の引数の数が一致しません: 期待値 1-2、実際 0。"},
		{name: "missing member", source: "asp-lsp-vbscript-type", code: "missingMember", data: map[string]any{"type": "Widget", "member": "Missing"}, english: "Type 'Widget' has no member 'Missing'.", japanese: "型 'Widget' にメンバー 'Missing' はありません。"},
		{name: "identifier case", source: "asp-lsp-vbscript-naming", code: "identifierCase", data: map[string]any{"name": "user_name", "expectedName": "UserName", "style": "PascalCase"}, english: "Identifier 'user_name' should be 'UserName' for PascalCase casing.", japanese: "識別子 'user_name' は PascalCase casing の 'UserName' にしてください。"},
		{name: "missing include", source: "asp-lsp-include", code: "include.missing", data: map[string]any{"path": "missing.inc"}, english: "Include file 'missing.inc' could not be resolved.", japanese: "include file 'missing.inc' を解決できません。"},
		{name: "include case", source: "asp-lsp-include", code: "include.pathCaseMismatch", data: map[string]any{"path": "COMMON.inc", "actualPath": "common.inc"}, english: "Include path 'COMMON.inc' differs from the file system casing 'common.inc'.", japanese: "include path 'COMMON.inc' は file system 上の大文字小文字 'common.inc' と一致していません。"},
		{name: "include current document", source: "asp-lsp-include", code: "include.currentDocument", english: "Include file references the current document.", japanese: "include file が現在のドキュメントを参照しています。"},
		{name: "include cycle", source: "asp-lsp-include", code: "include.cycle", data: map[string]any{"cycle": "a.asp -> b.inc -> a.asp"}, english: "Include cycle detected: a.asp -> b.inc -> a.asp.", japanese: "include の循環を検出しました: a.asp -> b.inc -> a.asp。"},
	}
	for _, locale := range []struct {
		name, value string
		japanese    bool
	}{{name: "english", value: "en"}, {name: "japanese", value: "ja", japanese: true}} {
		t.Run(locale.name, func(t *testing.T) {
			server := &Server{settings: serverSettings{Locale: locale.value}}
			for _, testCase := range testCases {
				diagnostic := lsp.Diagnostic{Source: testCase.source, Code: testCase.code, Message: testCase.message, Data: testCase.data}
				got := server.localizeDiagnostics([]lsp.Diagnostic{diagnostic})[0].Message
				want := testCase.english
				if locale.japanese {
					want = testCase.japanese
				}
				if got != want {
					t.Errorf("%s = %q, want %q", testCase.name, got, want)
				}
			}
		})
	}
}

func TestDiagnosticLocalizationAddsEmbeddedFixGuidance(t *testing.T) {
	server := &Server{settings: serverSettings{Locale: "ja"}}
	cases := []struct {
		name       string
		diagnostic lsp.Diagnostic
		want       string
	}{
		{
			name:       "html unexpected character",
			diagnostic: lsp.Diagnostic{Source: "asp-lsp-html", Message: "Unexpected character in tag."},
			want:       "修正方法: タグ名・属性名の不要な文字を削除し、VBScript の出力は有効なタグ名か引用符内の属性値にしてください。",
		},
		{
			name:       "html closing bracket",
			diagnostic: lsp.Diagnostic{Source: "asp-lsp-html", Message: "Closing bracket expected."},
			want:       "修正方法: タグ末尾に「>」または自己終了タグなら「/>」を追加し、VBScript の出力後もタグを閉じてください。",
		},
		{
			name:       "css empty ruleset",
			diagnostic: lsp.Diagnostic{Source: "asp-lsp-css", Code: "emptyRules", Message: "Do not use empty rulesets"},
			want:       "修正方法: ルール内に CSS 宣言を1つ以上追加するか、空のルールセットごと削除してください。",
		},
		{
			name:       "css property value",
			diagnostic: lsp.Diagnostic{Source: "asp-lsp-css", Code: "css-propertyvalueexpected", Message: "property value expected"},
			want:       "修正方法: 「:」の後に値を追加するか、値が不要ならプロパティ宣言ごと削除してください。",
		},
	}
	for _, testCase := range cases {
		t.Run(testCase.name, func(t *testing.T) {
			got := server.localizeDiagnostics([]lsp.Diagnostic{testCase.diagnostic})[0].Message
			if !strings.Contains(got, testCase.diagnostic.Message) || !strings.Contains(got, testCase.want) {
				t.Fatalf("message = %q, want original %q and guidance %q", got, testCase.diagnostic.Message, testCase.want)
			}
		})
	}
}

func TestStdioParityLocalizesVBScriptCallSyntaxDiagnostics(t *testing.T) {
	client := startStdioTestClient(t)
	defer client.close()

	root := t.TempDir()
	uri := pathToFileURI(filepath.Join(root, "go-localized-call-syntax.asp"))
	source := `<%
Function Func1(hoge)
  Func1 = hoge
End Function
Call Func1 hoge
%>`
	client.request("initialize", map[string]any{
		"processId":    nil,
		"rootUri":      pathToFileURI(root),
		"locale":       "ja-JP",
		"capabilities": map[string]any{},
	})
	diagnosticsMessage := openClassicASPDocumentWithDiagnostics(t, client, uri, source)
	serialized := string(diagnosticsMessage.Params)
	if !strings.Contains(serialized, "呼び出し構文") {
		t.Fatalf("localized call syntax diagnostic missing: %s", serialized)
	}
}

func TestStdioParityLocalizesVBScriptXMLDocsAndCompletionDetails(t *testing.T) {
	client := startStdioTestClient(t)
	defer client.close()

	root := t.TempDir()
	uri := pathToFileURI(filepath.Join(root, "go-localized-vbscript-docs.asp"))
	source := `<% Option Explicit
''' <summary>名前を作ります。</summary>
''' <param name="first">名。</param>
''' <returns>表示名。</returns>
Function BuildName(first)
  BuildName = missingName
End Function
%>`
	client.request("initialize", map[string]any{
		"processId":    nil,
		"rootUri":      pathToFileURI(root),
		"locale":       "ja-JP",
		"capabilities": map[string]any{},
	})
	diagnosticsMessage := openClassicASPDocumentWithDiagnostics(t, client, uri, source)
	diagnosticsText := string(diagnosticsMessage.Params)
	for _, expected := range []string{"宣言されていません", "使われていません"} {
		if !strings.Contains(diagnosticsText, expected) {
			t.Fatalf("localized VBScript diagnostics missing %q: %s", expected, diagnosticsText)
		}
	}
	hover := client.request("textDocument/hover", map[string]any{
		"textDocument": map[string]any{"uri": uri},
		"position":     positionAt(source, strings.Index(source, "BuildName")),
	})
	hoverText := hoverMarkdownValue(t, hover.Result)
	expectedXMLHover := "```vbscript\nFunction BuildName(ByRef first)\n```\n\n名前を作ります。\n\n**パラメーター**\n- `first`: 名。\n\n**戻り値**\n\n表示名。\n\n_XML ドキュメントコメントは説明用です。VBScript の型メタデータには `' @type`、`' @param ... As ...`、`' @returns ...` 注釈を使ってください。_"
	if hoverText != expectedXMLHover {
		t.Fatalf("localized XML documentation hover mismatch:\nwant: %s\n got: %s", expectedXMLHover, hoverText)
	}
	completions := completionItems(client.request("textDocument/completion", map[string]any{
		"textDocument": map[string]any{"uri": uri},
		"position":     positionAt(source, strings.Index(source, "BuildName = ")+len("BuildName = ")),
	}).Result)
	responseItem, ok := completions.find("Response")
	if !ok || !strings.Contains(responseItem.Detail, "VBScript") {
		t.Fatalf("localized completion details missing VBScript Response item: %#v", completionItemLabels(completions))
	}
}

func TestStdioParityLocalizesBuiltInFunctionDocumentationAndParameterHelp(t *testing.T) {
	client := startStdioTestClient(t)
	defer client.close()

	root := t.TempDir()
	uri := pathToFileURI(filepath.Join(root, "go-localized-builtins.asp"))
	source := `<%
Dim datePartValue
datePartValue = DatePart("yyyy", Date())
%>`
	client.request("initialize", map[string]any{
		"processId":    nil,
		"rootUri":      pathToFileURI(root),
		"locale":       "ja-JP",
		"capabilities": map[string]any{},
	})
	notifyOpenClassicASPDocument(t, client, uri, source)

	hover := client.request("textDocument/hover", map[string]any{
		"textDocument": map[string]any{"uri": uri},
		"position":     positionAt(source, strings.Index(source, "DatePart")),
	})
	hoverText := hoverMarkdownValue(t, hover.Result)
	expectedHover := "```vbscript\nFunction DatePart(interval, date, firstDayOfWeek, firstWeekOfYear) As Number\n```\n\ndate expression から指定した interval part を返します。\n\n**パラメーター**\n\n- `interval`: 返す interval code。\n- `date`: 評価する date expression。\n- `firstDayOfWeek`: 週の最初の曜日。\n- `firstWeekOfYear`: 年の最初の週。\n\n**戻り値**\n\ndate の指定部分。"
	if hoverText != expectedHover {
		t.Fatalf("localized DatePart hover mismatch:\nwant: %s\n got: %s", expectedHover, hoverText)
	}

	signature := client.request("textDocument/signatureHelp", map[string]any{
		"textDocument": map[string]any{"uri": uri},
		"position":     positionAt(source, strings.Index(source, `DatePart("yyyy"`)+len(`DatePart("yyyy"`)),
	})
	var signatureHelp lsp.SignatureHelp
	mustDecodeResult(t, signature.Result, &signatureHelp)
	if len(signatureHelp.Signatures) != 1 {
		t.Fatalf("localized DatePart signature count mismatch: %#v", signatureHelp)
	}
	if signatureHelp.Signatures[0].Documentation != "date expression から指定した interval part を返します。\n\n**パラメーター**\n\n- `interval`: 返す interval code。\n- `date`: 評価する date expression。\n- `firstDayOfWeek`: 週の最初の曜日。\n- `firstWeekOfYear`: 年の最初の週。\n\n**戻り値**\n\ndate の指定部分。" {
		t.Fatalf("localized DatePart signature docs mismatch: %#v", signatureHelp.Signatures[0].Documentation)
	}
	expectedParameterDocs := []string{"返す interval code。", "評価する date expression。", "週の最初の曜日。", "年の最初の週。"}
	for index, expected := range expectedParameterDocs {
		if index >= len(signatureHelp.Signatures[0].Parameters) || signatureHelp.Signatures[0].Parameters[index].Documentation != expected {
			t.Fatalf("localized DatePart parameter %d docs mismatch: %#v", index, signatureHelp.Signatures[0].Parameters)
		}
	}

	completions := completionItems(client.request("textDocument/completion", map[string]any{
		"textDocument": map[string]any{"uri": uri},
		"position":     positionAt(source, strings.Index(source, "Dim datePartValue")),
	}).Result)
	datePartItem, ok := completions.find("DatePart")
	if !ok {
		t.Fatalf("DatePart completion missing: %#v", completionItemLabels(completions))
	}
	resolved := client.request("completionItem/resolve", datePartItem)
	var resolvedItem lsp.CompletionItem
	mustDecodeResult(t, resolved.Result, &resolvedItem)
	expectedCompletionDocs := expectedHover[len("```vbscript\nFunction DatePart(interval, date, firstDayOfWeek, firstWeekOfYear) As Number\n```\n\n"):]
	if signatureDocumentationText(resolvedItem.Documentation) != expectedCompletionDocs {
		t.Fatalf("localized DatePart completion resolve docs mismatch:\nwant: %s\n got: %s", expectedCompletionDocs, signatureDocumentationText(resolvedItem.Documentation))
	}
}
