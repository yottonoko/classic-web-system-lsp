package lspserver

import (
	"fmt"
	"os"
	"path/filepath"
	"strings"
	"testing"

	"github.com/yottonoko/classic-web-system-lsp/internal/lsp"
)

func TestStdioParityUnresolvedVBScriptCompletionSetting(t *testing.T) {
	client := startStdioTestClient(t)
	defer client.close()

	root := t.TempDir()
	uri := pathToFileURI(filepath.Join(root, "unresolved-completion.asp"))
	source := `<%
MissingRead
Call MissingProc()
M
%>`
	client.request("initialize", map[string]any{
		"processId":    nil,
		"rootUri":      pathToFileURI(root),
		"capabilities": map[string]any{},
	})
	if err := client.notify("textDocument/didOpen", map[string]any{
		"textDocument": map[string]any{
			"uri":        uri,
			"languageId": "classic-asp",
			"version":    1,
			"text":       source,
		},
	}); err != nil {
		t.Fatal(err)
	}
	client.waitForNotification("textDocument/publishDiagnostics", uri)

	defaultCompletions := completionLabels(client.request("textDocument/completion", map[string]any{
		"textDocument": map[string]any{"uri": uri},
		"position":     map[string]any{"line": 3, "character": 1},
	}).Result)
	if defaultCompletions.contains("MissingRead") || defaultCompletions.contains("MissingProc") {
		t.Fatalf("default completions should hide unresolved symbols: %#v", defaultCompletions)
	}

	if err := client.notify("workspace/didChangeConfiguration", map[string]any{
		"settings": map[string]any{
			"aspLsp": map[string]any{
				"vbscript": map[string]any{"showUnresolvedSymbolsInCompletion": true},
			},
		},
	}); err != nil {
		t.Fatal(err)
	}
	enabled := completionItems(client.request("textDocument/completion", map[string]any{
		"textDocument": map[string]any{"uri": uri},
		"position":     map[string]any{"line": 3, "character": 1},
	}).Result)
	if enabled.hasLabel("M") {
		t.Fatalf("enabled completions should not contain raw prefix item: %#v", enabled)
	}
	if !enabled.hasItem("MissingRead", 6, "Implicit global variable") {
		t.Fatalf("enabled completions missing MissingRead implicit global: %#v", enabled)
	}
	if !enabled.hasItem("MissingProc", 3, "Unresolved Function/Sub") {
		t.Fatalf("enabled completions missing MissingProc unresolved function: %#v", enabled)
	}
}

func TestStdioParityVBScriptSyntaxSnippetSetting(t *testing.T) {
	client := startStdioTestClient(t)
	defer client.close()

	root := t.TempDir()
	uri := pathToFileURI(filepath.Join(root, "vbscript-syntax-snippets.asp"))
	client.request("initialize", map[string]any{
		"processId":    nil,
		"rootUri":      pathToFileURI(root),
		"capabilities": map[string]any{},
	})
	if err := client.notify("textDocument/didOpen", map[string]any{
		"textDocument": map[string]any{
			"uri":        uri,
			"languageId": "classic-asp",
			"version":    1,
			"text":       "<% Option Explicit\n\n%>",
		},
	}); err != nil {
		t.Fatal(err)
	}
	client.waitForNotification("textDocument/publishDiagnostics", uri)

	items := completionItems(client.request("textDocument/completion", map[string]any{
		"textDocument": map[string]any{"uri": uri},
		"position":     map[string]any{"line": 1, "character": 0},
	}).Result)
	ifSnippet, ok := items.find("If Then")
	if !ok || ifSnippet.Kind != 15 || ifSnippet.InsertTextFormat != 2 || !strings.Contains(ifSnippet.InsertText, "End If") {
		t.Fatalf("If Then snippet mismatch: %#v", ifSnippet)
	}

	if err := client.notify("workspace/didChangeConfiguration", map[string]any{
		"settings": map[string]any{"aspLsp": map[string]any{"vbscript": map[string]any{"syntaxSnippets": false}}},
	}); err != nil {
		t.Fatal(err)
	}
	disabled := completionLabels(client.request("textDocument/completion", map[string]any{
		"textDocument": map[string]any{"uri": uri},
		"position":     map[string]any{"line": 1, "character": 0},
	}).Result)
	if disabled.contains("If Then") || !disabled.contains("Response") || !disabled.contains("Dim") {
		t.Fatalf("snippet-disabled completions mismatch: %#v", disabled)
	}
}

func TestStdioParityPartialVBScriptMemberCompletion(t *testing.T) {
	client := startStdioTestClient(t)
	defer client.close()

	root := t.TempDir()
	uri := pathToFileURI(filepath.Join(root, "partial-member.asp"))
	marked := markedDocument("<%\nServer.HTMLEe<<<caret>>>\n%>")
	client.request("initialize", map[string]any{
		"processId":    nil,
		"rootUri":      pathToFileURI(root),
		"capabilities": map[string]any{},
	})
	if err := client.notify("textDocument/didOpen", map[string]any{
		"textDocument": map[string]any{
			"uri":        uri,
			"languageId": "classic-asp",
			"version":    1,
			"text":       marked.Text,
		},
	}); err != nil {
		t.Fatal(err)
	}
	client.waitForNotification("textDocument/publishDiagnostics", uri)

	completions := mustJSONText(t, client.request("textDocument/completion", map[string]any{
		"textDocument": map[string]any{"uri": uri},
		"position":     marked.Position,
	}).Result)
	if !strings.Contains(completions, "HTMLEncode") || strings.Contains(completions, "If Then") {
		t.Fatalf("partial member completions mismatch: %s", completions)
	}
}

func TestStdioParityVBScriptNavigationRenameAndSemanticTokens(t *testing.T) {
	client := startStdioTestClient(t)
	defer client.close()

	root := t.TempDir()
	uri := pathToFileURI(filepath.Join(root, "vbscript-navigation.asp"))
	source := `<%
Function BuildName(ByVal firstName, lastName)
  BuildName = firstName & " " & lastName
  ' BuildName in a comment
  Response.Write "BuildName in a string"
End Function
Response.Write BuildName("Ada", "Lovelace")
%>`
	position := positionAt(source, strings.LastIndex(source, "BuildName")+2)
	initialize := client.request("initialize", map[string]any{
		"processId":    nil,
		"rootUri":      pathToFileURI(root),
		"capabilities": map[string]any{},
	})
	initializeText := mustJSONText(t, initialize.Result)
	for _, expected := range []string{`"parameter"`, `"readonly"`, `"library"`, `"byref"`, `"byval"`, `"string"`, `"operator"`, `"constant"`} {
		if !strings.Contains(initializeText, expected) {
			t.Fatalf("initialize semantic legend missing %q: %s", expected, initializeText)
		}
	}
	if err := client.notify("textDocument/didOpen", map[string]any{
		"textDocument": map[string]any{
			"uri":        uri,
			"languageId": "classic-asp",
			"version":    1,
			"text":       source,
		},
	}); err != nil {
		t.Fatal(err)
	}
	client.waitForNotification("textDocument/publishDiagnostics", uri)

	for _, method := range []string{
		"textDocument/definition",
		"textDocument/declaration",
		"textDocument/typeDefinition",
		"textDocument/implementation",
	} {
		result := client.request(method, map[string]any{
			"textDocument": map[string]any{"uri": uri},
			"position":     position,
		})
		if !strings.Contains(mustJSONText(t, result.Result), `"line":1`) {
			t.Fatalf("%s did not resolve to declaration line: %s", method, mustJSONText(t, result.Result))
		}
	}

	references := client.request("textDocument/references", map[string]any{
		"textDocument": map[string]any{"uri": uri},
		"position":     position,
		"context":      map[string]any{"includeDeclaration": true},
	})
	if got := resultArrayLength(t, references.Result); got != 3 {
		t.Fatalf("references including declaration = %d, want 3: %s", got, mustJSONText(t, references.Result))
	}
	usageReferences := client.request("textDocument/references", map[string]any{
		"textDocument": map[string]any{"uri": uri},
		"position":     position,
		"context":      map[string]any{"includeDeclaration": false},
	})
	if got := resultArrayLength(t, usageReferences.Result); got != 2 {
		t.Fatalf("usage references = %d, want 2: %s", got, mustJSONText(t, usageReferences.Result))
	}

	prepareRename := client.request("textDocument/prepareRename", map[string]any{
		"textDocument": map[string]any{"uri": uri},
		"position":     position,
	})
	if !strings.Contains(mustJSONText(t, prepareRename.Result), `"line":6`) {
		t.Fatalf("prepareRename did not target usage line: %s", mustJSONText(t, prepareRename.Result))
	}

	rename := client.request("textDocument/rename", map[string]any{
		"textDocument": map[string]any{"uri": uri},
		"position":     position,
		"newName":      "FormatName",
	})
	renameText := mustJSONText(t, rename.Result)
	if strings.Count(renameText, `"newText":"FormatName"`) != 3 || strings.Contains(renameText, "comment") || strings.Contains(renameText, "string") {
		t.Fatalf("rename edits mismatch: %s", renameText)
	}

	highlights := client.request("textDocument/documentHighlight", map[string]any{
		"textDocument": map[string]any{"uri": uri},
		"position":     position,
	})
	if got := resultArrayLength(t, highlights.Result); got != 3 {
		t.Fatalf("document highlights = %d, want 3: %s", got, mustJSONText(t, highlights.Result))
	}

	signature := client.request("textDocument/signatureHelp", map[string]any{
		"textDocument": map[string]any{"uri": uri},
		"position":     positionAt(source, strings.Index(source, `"Ada"`)),
	})
	if !strings.Contains(mustJSONText(t, signature.Result), "BuildName(ByVal firstName, ByRef lastName)") {
		t.Fatalf("signature help mismatch: %s", mustJSONText(t, signature.Result))
	}

	workspaceSymbols := client.request("workspace/symbol", map[string]any{"query": "Build"})
	if !strings.Contains(mustJSONText(t, workspaceSymbols.Result), "BuildName") {
		t.Fatalf("workspace symbols missing BuildName: %s", mustJSONText(t, workspaceSymbols.Result))
	}

	semanticTokens := client.request("textDocument/semanticTokens/full", map[string]any{
		"textDocument": map[string]any{"uri": uri},
	})
	tokens := decodeSemanticTokens(t, semanticTokens.Result)
	callPosition := positionAt(source, strings.LastIndex(source, "BuildName"))
	if !hasSemanticToken(tokens, callPosition["line"], callPosition["character"], semanticTokenFunction, 0) {
		t.Fatalf("semantic tokens missing BuildName call function token: %#v", tokens)
	}
	firstNamePosition := positionAt(source, strings.Index(source, "firstName &"))
	if !hasSemanticToken(tokens, firstNamePosition["line"], firstNamePosition["character"], semanticTokenParameter, semanticModifierByVal) {
		t.Fatalf("semantic tokens missing firstName ByVal parameter token: %#v", tokens)
	}
	lastNamePosition := positionAt(source, strings.Index(source, "lastName"))
	if !hasSemanticToken(tokens, lastNamePosition["line"], lastNamePosition["character"], semanticTokenParameter, semanticModifierByRef) {
		t.Fatalf("semantic tokens missing lastName ByRef parameter token: %#v", tokens)
	}
	if !hasSemanticTokenType(tokens, semanticTokenOperator) {
		t.Fatalf("semantic tokens missing operator token: %#v", tokens)
	}

	semanticDelta := client.request("textDocument/semanticTokens/full/delta", map[string]any{
		"textDocument":     map[string]any{"uri": uri},
		"previousResultId": semanticResultID(t, semanticTokens.Result),
	})
	deltaText := mustJSONText(t, semanticDelta.Result)
	if !strings.Contains(deltaText, `"edits"`) ||
		(!strings.Contains(deltaText, `"data"`) && !strings.Contains(deltaText, `"edits":[]`)) {
		t.Fatalf("semantic delta mismatch: %s", deltaText)
	}
}

func TestStdioParityReferencesVBScriptFunctionReturnValueAndRecursiveCall(t *testing.T) {
	client := startStdioTestClient(t)
	defer client.close()

	root := t.TempDir()
	uri := pathToFileURI(filepath.Join(root, "function-return-recursive.asp"))
	source := `<%
Option Explicit
Function Factorial(ByVal n)
  If n <= 1 Then
    Factorial = 1
    Exit Function
  End If
  Factorial = n * Factorial(n - 1)
  Factorial = Factorial + 0
End Function
Response.Write Factorial(5)
%>`
	position := positionAt(source, strings.LastIndex(source, "Factorial(5)")+2)
	client.request("initialize", map[string]any{
		"processId":    nil,
		"rootUri":      pathToFileURI(root),
		"capabilities": map[string]any{},
	})
	diagnostics := openClassicASPDocumentWithDiagnostics(t, client, uri, source)
	diagnosticsText := string(diagnostics.Params)
	for _, unexpected := range []string{
		"'Exit' is not declared",
		"'Factorial' is not declared",
		"Parameter 'n' is never used",
	} {
		if strings.Contains(diagnosticsText, unexpected) {
			t.Fatalf("function return diagnostics unexpectedly included %q: %s", unexpected, diagnosticsText)
		}
	}

	references := client.request("textDocument/references", map[string]any{
		"textDocument": map[string]any{"uri": uri},
		"position":     position,
		"context":      map[string]any{"includeDeclaration": true},
	})
	if got := resultArrayLength(t, references.Result); got != 7 {
		t.Fatalf("function return references = %d, want 7: %s", got, mustJSONText(t, references.Result))
	}
	usageReferences := client.request("textDocument/references", map[string]any{
		"textDocument": map[string]any{"uri": uri},
		"position":     position,
		"context":      map[string]any{"includeDeclaration": false},
	})
	usageText := mustJSONText(t, usageReferences.Result)
	for _, expectedLine := range []string{`"line":4`, `"line":7`, `"line":8`, `"line":10`} {
		if !strings.Contains(usageText, expectedLine) {
			t.Fatalf("function return references missing %s: %s", expectedLine, usageText)
		}
	}
}

func TestStdioParityAllowsReadingCurrentFunctionReturnValueSlot(t *testing.T) {
	client := startStdioTestClient(t)
	defer client.close()

	root := t.TempDir()
	uri := pathToFileURI(filepath.Join(root, "function-return-read.asp"))
	source := `<%
Option Explicit
Function a()
  a = 1
  a = a - 9
End Function
%>`
	client.request("initialize", map[string]any{
		"processId":    nil,
		"rootUri":      pathToFileURI(root),
		"capabilities": map[string]any{},
	})
	diagnostics := diagnosticsFromPublishMessage(t, openClassicASPDocumentWithDiagnostics(t, client, uri, source))
	if len(diagnostics) != 1 || diagnostics[0].Code != "identifierCase" || diagnostics[0].Severity != lsp.DiagnosticSeverityHint {
		t.Fatalf("valid Function return-value read diagnostics = %#v, want only lowercase naming hint", diagnostics)
	}

	readOffset := strings.LastIndex(source, "a - 9")
	position := positionAt(source, readOffset)
	definition := client.request("textDocument/definition", map[string]any{
		"textDocument": map[string]any{"uri": uri},
		"position":     position,
	})
	if got := resultArrayLength(t, definition.Result); got != 1 {
		t.Fatalf("Function return-value definition count = %d, want 1: %s", got, mustJSONText(t, definition.Result))
	}
	references := client.request("textDocument/references", map[string]any{
		"textDocument": map[string]any{"uri": uri},
		"position":     position,
		"context":      map[string]any{"includeDeclaration": true},
	})
	if got := resultArrayLength(t, references.Result); got != 4 {
		t.Fatalf("Function return-value references = %d, want 4: %s", got, mustJSONText(t, references.Result))
	}
}

func TestStdioParitySupportsVBScriptInlayHintsCallHierarchyMonikersAndCodeLens(t *testing.T) {
	client := startStdioTestClient(t)
	defer client.close()

	root := t.TempDir()
	uri := pathToFileURI(filepath.Join(root, "go-vbscript-advanced-lsp.asp"))
	source := `<%
Function BuildName(ByVal firstName, lastName)
  BuildName = firstName & " " & lastName
End Function
Sub Save()
  Response.Write BuildName("Ada", "Lovelace")
End Sub
%>`
	client.request("initialize", map[string]any{
		"processId":    nil,
		"rootUri":      pathToFileURI(root),
		"capabilities": map[string]any{},
	})
	openClassicASPDocument(t, client, uri, source)

	inlayHints := client.request("textDocument/inlayHint", map[string]any{
		"textDocument": map[string]any{"uri": uri},
		"range": map[string]any{
			"start": map[string]any{"line": 0, "character": 0},
			"end":   map[string]any{"line": 8, "character": 0},
		},
	})
	inlayHintText := mustJSONText(t, inlayHints.Result)
	if !strings.Contains(inlayHintText, "firstName:") || !strings.Contains(inlayHintText, "lastName:") {
		t.Fatalf("inlay hints missing parameter labels: %s", inlayHintText)
	}
	var hints []lsp.InlayHint
	mustDecodeResult(t, inlayHints.Result, &hints)
	if len(hints) == 0 {
		t.Fatalf("inlay hints returned none: %s", inlayHintText)
	}
	resolvedHint := client.request("inlayHint/resolve", hints[0])
	if !strings.Contains(mustJSONText(t, resolvedHint.Result), "firstName:") {
		t.Fatalf("resolved inlay hint mismatch: %s", mustJSONText(t, resolvedHint.Result))
	}

	buildHierarchy := client.request("textDocument/prepareCallHierarchy", map[string]any{
		"textDocument": map[string]any{"uri": uri},
		"position":     positionAt(source, strings.Index(source, "BuildName")+2),
	})
	var buildItems []lsp.CallHierarchyItem
	mustDecodeResult(t, buildHierarchy.Result, &buildItems)
	if len(buildItems) == 0 || !strings.Contains(mustJSONText(t, buildHierarchy.Result), "BuildName") {
		t.Fatalf("BuildName call hierarchy mismatch: %s", mustJSONText(t, buildHierarchy.Result))
	}
	incoming := client.request("callHierarchy/incomingCalls", map[string]any{"item": buildItems[0]})
	if !strings.Contains(mustJSONText(t, incoming.Result), "Save") {
		t.Fatalf("incoming call hierarchy missing Save: %s", mustJSONText(t, incoming.Result))
	}

	saveHierarchy := client.request("textDocument/prepareCallHierarchy", map[string]any{
		"textDocument": map[string]any{"uri": uri},
		"position":     positionAt(source, strings.Index(source, "Save()")+2),
	})
	var saveItems []lsp.CallHierarchyItem
	mustDecodeResult(t, saveHierarchy.Result, &saveItems)
	if len(saveItems) == 0 {
		t.Fatalf("Save call hierarchy returned none: %s", mustJSONText(t, saveHierarchy.Result))
	}
	outgoing := client.request("callHierarchy/outgoingCalls", map[string]any{"item": saveItems[0]})
	if !strings.Contains(mustJSONText(t, outgoing.Result), "BuildName") {
		t.Fatalf("outgoing call hierarchy missing BuildName: %s", mustJSONText(t, outgoing.Result))
	}

	monikers := client.request("textDocument/moniker", map[string]any{
		"textDocument": map[string]any{"uri": uri},
		"position":     positionAt(source, strings.LastIndex(source, "BuildName")+2),
	})
	if !strings.Contains(mustJSONText(t, monikers.Result), "BuildName") {
		t.Fatalf("monikers missing BuildName: %s", mustJSONText(t, monikers.Result))
	}

	typeHierarchy := client.request("textDocument/prepareTypeHierarchy", map[string]any{
		"textDocument": map[string]any{"uri": uri},
		"position":     positionAt(source, strings.Index(source, "BuildName")+2),
	})
	if got := mustJSONText(t, typeHierarchy.Result); got != "[]" {
		t.Fatalf("type hierarchy = %s, want []", got)
	}

	codeLensResponse := client.request("textDocument/codeLens", map[string]any{
		"textDocument": map[string]any{"uri": uri},
	})
	var codeLenses []lsp.CodeLens
	mustDecodeResult(t, codeLensResponse.Result, &codeLenses)
	var buildLens *lsp.CodeLens
	for index := range codeLenses {
		if strings.Contains(mustJSONText(t, codeLenses[index]), "BuildName") {
			buildLens = &codeLenses[index]
			break
		}
	}
	if buildLens == nil {
		t.Fatalf("BuildName CodeLens missing: %s", mustJSONText(t, codeLensResponse.Result))
	}
	resolvedLens := client.request("codeLens/resolve", buildLens)
	var lens lsp.CodeLens
	mustDecodeResult(t, resolvedLens.Result, &lens)
	if lens.Command == nil || lens.Command.Command != "aspLsp.showReferences" || lens.Command.Title != "1 reference" || len(lens.Command.Arguments) != 2 {
		t.Fatalf("resolved CodeLens command mismatch: %s", mustJSONText(t, resolvedLens.Result))
	}
}

func TestStdioParityResolvesVBScriptCompletionWithSourceLocation(t *testing.T) {
	client := startStdioTestClient(t)
	defer client.close()

	root := t.TempDir()
	uri := pathToFileURI(filepath.Join(root, "go-vbscript-completion-resolve.asp"))
	marked := markedDocument(`<%
Function BuildName(firstName)
  BuildName = firstName
End Function
Sub Save()
  Response.Write Bui<<<caret>>>
End Sub
%>`)
	client.request("initialize", map[string]any{
		"processId":    nil,
		"rootUri":      pathToFileURI(root),
		"capabilities": map[string]any{},
	})
	openClassicASPDocument(t, client, uri, marked.Text)

	completions := completionItems(client.request("textDocument/completion", map[string]any{
		"textDocument": map[string]any{"uri": uri},
		"position":     marked.Position,
	}).Result)
	buildCompletion, ok := completions.find("BuildName")
	if !ok {
		t.Fatalf("BuildName completion missing: %#v", completionItemLabels(completions))
	}
	resolved := client.request("completionItem/resolve", buildCompletion)
	resolvedText := mustJSONText(t, resolved.Result)
	for _, expected := range []string{"Defined in [go-vbscript-completion-resolve.asp]", uri} {
		if !strings.Contains(resolvedText, expected) {
			t.Fatalf("resolved BuildName completion missing %q: %s", expected, resolvedText)
		}
	}
}

func TestStdioParityReturnsVBScriptInlineValuesForDeclarations(t *testing.T) {
	client := startStdioTestClient(t)
	defer client.close()

	root := t.TempDir()
	uri := pathToFileURI(filepath.Join(root, "go-vbscript-inline-values.asp"))
	source := `<%
Function BuildName(firstName)
Dim localValue
localValue = firstName
BuildName = localValue
End Function
%>`
	client.request("initialize", map[string]any{
		"processId":    nil,
		"rootUri":      pathToFileURI(root),
		"capabilities": map[string]any{},
	})
	openClassicASPDocument(t, client, uri, source)

	inlineValues := client.request("textDocument/inlineValue", map[string]any{
		"textDocument": map[string]any{"uri": uri},
		"range": map[string]any{
			"start": map[string]any{"line": 0, "character": 0},
			"end":   map[string]any{"line": 6, "character": 0},
		},
		"context": map[string]any{
			"frameId": 1,
			"stoppedLocation": map[string]any{
				"start": positionAt(source, strings.Index(source, "firstName")),
				"end":   positionAt(source, strings.Index(source, "firstName")+len("firstName")),
			},
		},
	})
	inlineText := mustJSONText(t, inlineValues.Result)
	for _, expected := range []string{`"variableName":"firstName"`, `"variableName":"localValue"`} {
		if !strings.Contains(inlineText, expected) {
			t.Fatalf("inline values missing %q: %s", expected, inlineText)
		}
	}
}

func TestStdioParityVBScriptRenameScopeSetting(t *testing.T) {
	root := t.TempDir()
	owner := filepath.Join(root, "default.asp")
	include := filepath.Join(root, "common.inc")
	if err := os.WriteFile(include, []byte("<%\nResponse.Write SharedValue\n%>"), 0o644); err != nil {
		t.Fatal(err)
	}
	source := `<!-- #include file="common.inc" -->
<%
Dim SharedValue
Response.Write SharedValue
%>`
	if err := os.WriteFile(owner, []byte(source), 0o644); err != nil {
		t.Fatal(err)
	}
	ownerURI := pathToFileURI(owner)
	includeURI := pathToFileURI(include)

	localEdit := runRenameScopeCase(t, root, ownerURI, source, false)
	if keys := workspaceEditChangeURIs(t, localEdit); !sameStringSet(keys, []string{ownerURI}) {
		t.Fatalf("local rename changes = %#v, want owner only; edit=%s", keys, mustJSONText(t, localEdit))
	}
	if strings.Contains(mustJSONText(t, localEdit), "common.inc") {
		t.Fatalf("local rename should not touch include: %s", mustJSONText(t, localEdit))
	}

	workspaceEdit := runRenameScopeCase(t, root, ownerURI, source, true)
	if keys := workspaceEditChangeURIs(t, workspaceEdit); !sameStringSet(keys, []string{ownerURI, includeURI}) {
		t.Fatalf("workspace rename changes = %#v, want owner and include; edit=%s", keys, mustJSONText(t, workspaceEdit))
	}
}

func TestStdioParityWorkspaceRenameKeepsProcedureParametersLocal(t *testing.T) {
	client := startStdioTestClient(t)
	defer client.close()

	root := t.TempDir()
	owner := filepath.Join(root, "default.asp")
	include := filepath.Join(root, "common.inc")
	if err := os.WriteFile(include, []byte("<%\nDim value\nResponse.Write value\n%>"), 0o644); err != nil {
		t.Fatal(err)
	}
	source := `<!-- #include file="common.inc" -->
<%
Sub First(ByVal value)
  value = value + 1
End Sub

Sub Second(ByRef value)
  value = value + 2
End Sub
%>`
	if err := os.WriteFile(owner, []byte(source), 0o644); err != nil {
		t.Fatal(err)
	}
	ownerURI := pathToFileURI(owner)
	initializeWithConfigurationAndWaitForWorkspaceIndex(t, client, map[string]any{
		"processId":    nil,
		"rootUri":      pathToFileURI(root),
		"capabilities": map[string]any{},
	}, map[string]any{"aspLsp": map[string]any{
		"rename": map[string]any{"workspaceSymbolRename": true},
	}})
	openClassicASPDocumentWithDiagnostics(t, client, ownerURI, source)

	rename := client.request("textDocument/rename", map[string]any{
		"textDocument": map[string]any{"uri": ownerURI},
		"position":     positionAt(source, strings.Index(source, "value + 1")+1),
		"newName":      "renamedValue",
	})
	var edit struct {
		Changes map[string][]lsp.TextEdit `json:"changes"`
	}
	mustDecodeResult(t, rename.Result, &edit)
	if len(edit.Changes) != 1 || len(edit.Changes[ownerURI]) != 3 {
		t.Fatalf("procedure parameter workspace rename escaped its owner: %#v", edit.Changes)
	}
	for _, textEdit := range edit.Changes[ownerURI] {
		if textEdit.Range.Start.Line < 2 || textEdit.Range.Start.Line > 3 {
			t.Fatalf("procedure parameter workspace rename escaped First: %#v", edit.Changes[ownerURI])
		}
	}
}

func TestStdioParityVBScriptHoverDefinitionReferencesAndClassMembers(t *testing.T) {
	client := startStdioTestClient(t)
	defer client.close()

	root := t.TempDir()
	uri := pathToFileURI(filepath.Join(root, "vbscript-hover-class-member.asp"))
	marked := markedDocument(`<%
Class Customer
  Public Name
  Public Sub Save()
  End Sub
End Class
Dim customer
Set customer = New Customer
customer.<<<caret>>>
Function BuildName()
End Function
Response.Write BuildName()
%>`)
	source := marked.Text
	callPosition := positionAt(source, strings.Index(source, "BuildName()")+2)
	client.request("initialize", map[string]any{
		"processId":    nil,
		"rootUri":      pathToFileURI(root),
		"capabilities": map[string]any{},
	})
	openClassicASPDocument(t, client, uri, source)

	memberLabels := completionLabels(client.request("textDocument/completion", map[string]any{
		"textDocument": map[string]any{"uri": uri},
		"position":     marked.Position,
	}).Result)
	if !memberLabels.contains("Save") || !memberLabels.contains("Name") {
		t.Fatalf("class member completions mismatch: %#v", memberLabels)
	}

	hover := mustJSONText(t, client.request("textDocument/hover", map[string]any{
		"textDocument": map[string]any{"uri": uri},
		"position":     callPosition,
	}).Result)
	for _, expected := range []string{`"kind":"markdown"`, "```vbscript", "Function BuildName()"} {
		if !strings.Contains(hover, expected) {
			t.Fatalf("hover missing %q: %s", expected, hover)
		}
	}
	if strings.Contains(hover, "VBScript function.") {
		t.Fatalf("hover should not contain generic fallback docs: %s", hover)
	}

	definition := client.request("textDocument/definition", map[string]any{
		"textDocument": map[string]any{"uri": uri},
		"position":     callPosition,
	})
	if !strings.Contains(mustJSONText(t, definition.Result), `"line":9`) {
		t.Fatalf("definition mismatch: %s", mustJSONText(t, definition.Result))
	}
	references := client.request("textDocument/references", map[string]any{
		"textDocument": map[string]any{"uri": uri},
		"position":     callPosition,
		"context":      map[string]any{"includeDeclaration": true},
	})
	if resultArrayLength(t, references.Result) <= 1 {
		t.Fatalf("references should include declaration and usage: %s", mustJSONText(t, references.Result))
	}
	usageReferences := client.request("textDocument/references", map[string]any{
		"textDocument": map[string]any{"uri": uri},
		"position":     callPosition,
		"context":      map[string]any{"includeDeclaration": false},
	})
	if got := resultArrayLength(t, usageReferences.Result); got != 1 {
		t.Fatalf("usage references = %d, want 1: %s", got, mustJSONText(t, usageReferences.Result))
	}
}

func TestStdioParityKeepsQualifiedVBScriptMembersSeparateFromGlobals(t *testing.T) {
	client := startStdioTestClient(t)
	defer client.close()

	root := t.TempDir()
	uri := pathToFileURI(filepath.Join(root, "qualified-vbscript-members.asp"))
	source := `<%
Function ABC()
  ABC = "global"
End Function
Class Container
  Public Function ABC(ByVal input)
    ABC = "member"
    Call ABC(input)
    Call Me.ABC(input)
  End Function
  Public Sub Other(ABC)
    ABC = 1
  End Sub
  Public Property Get Value()
    Value = "member"
  End Property
End Class
Dim A
Set A = New Container
Response.Write A.ABC("value")
Response.Write A . ABC("spaced")
A.ABC "plain"
Response.Write A.Value
Response.Write ABC()
%>`
	client.request("initialize", map[string]any{
		"processId": nil, "rootUri": pathToFileURI(root), "capabilities": map[string]any{},
	})
	openClassicASPDocument(t, client, uri, source)

	memberCall := positionAt(source, strings.Index(source, "A.ABC(")+3)
	memberArgument := positionAt(source, strings.Index(source, `A.ABC("`)+len("A.ABC("))
	memberNoParenArgument := positionAt(source, strings.Index(source, `"plain"`)+len(`"plain"`))
	memberDeclaration := positionAt(source, strings.Index(source, "Public Function ABC")+len("Public Function A"))
	memberValue := positionAt(source, strings.Index(source, "A.Value")+3)
	globalCall := positionAt(source, strings.LastIndex(source, "ABC()")+1)

	memberDefinition := mustJSONText(t, client.request("textDocument/definition", map[string]any{
		"textDocument": map[string]any{"uri": uri}, "position": memberCall,
	}).Result)
	if !strings.Contains(memberDefinition, `"line":5`) || strings.Contains(memberDefinition, `"line":1`) {
		t.Fatalf("qualified method definition mismatch: %s", memberDefinition)
	}
	valueDefinition := mustJSONText(t, client.request("textDocument/definition", map[string]any{
		"textDocument": map[string]any{"uri": uri}, "position": memberValue,
	}).Result)
	if !strings.Contains(valueDefinition, `"line":13`) {
		t.Fatalf("qualified property definition mismatch: %s", valueDefinition)
	}
	globalDefinition := mustJSONText(t, client.request("textDocument/definition", map[string]any{
		"textDocument": map[string]any{"uri": uri}, "position": globalCall,
	}).Result)
	if !strings.Contains(globalDefinition, `"line":1`) || strings.Contains(globalDefinition, `"line":5`) {
		t.Fatalf("global definition mismatch: %s", globalDefinition)
	}

	memberHover := mustJSONText(t, client.request("textDocument/hover", map[string]any{
		"textDocument": map[string]any{"uri": uri}, "position": memberCall,
	}).Result)
	if !strings.Contains(memberHover, "Function ABC(ByVal input)") {
		t.Fatalf("qualified method hover mismatch: %s", memberHover)
	}
	memberSignature := mustJSONText(t, client.request("textDocument/signatureHelp", map[string]any{
		"textDocument": map[string]any{"uri": uri}, "position": memberArgument,
	}).Result)
	if !strings.Contains(memberSignature, "ABC(ByVal input)") {
		t.Fatalf("qualified method signature help mismatch: %s", memberSignature)
	}
	memberNoParenSignature := mustJSONText(t, client.request("textDocument/signatureHelp", map[string]any{
		"textDocument": map[string]any{"uri": uri}, "position": memberNoParenArgument,
	}).Result)
	if !strings.Contains(memberNoParenSignature, "ABC(ByVal input)") {
		t.Fatalf("qualified no-parentheses signature help mismatch: %s", memberNoParenSignature)
	}

	memberReferences := mustJSONText(t, client.request("textDocument/references", map[string]any{
		"textDocument": map[string]any{"uri": uri}, "position": memberCall,
		"context": map[string]any{"includeDeclaration": true},
	}).Result)
	for _, line := range []string{`"line":5`, `"line":6`, `"line":7`, `"line":8`, `"line":19`, `"line":20`, `"line":21`} {
		if !strings.Contains(memberReferences, line) {
			t.Fatalf("qualified method references missing %s: %s", line, memberReferences)
		}
	}
	for _, line := range []string{`"line":1,`, `"line":10,`, `"line":11,`, `"line":23,`} {
		if strings.Contains(memberReferences, line) {
			t.Fatalf("qualified method references included unrelated ABC at %s: %s", line, memberReferences)
		}
	}
	declarationReferences := mustJSONText(t, client.request("textDocument/references", map[string]any{
		"textDocument": map[string]any{"uri": uri}, "position": memberDeclaration,
		"context": map[string]any{"includeDeclaration": true},
	}).Result)
	if declarationReferences != memberReferences {
		t.Fatalf("member declaration references differ from qualified use: declaration=%s use=%s", declarationReferences, memberReferences)
	}

	globalReferences := mustJSONText(t, client.request("textDocument/references", map[string]any{
		"textDocument": map[string]any{"uri": uri}, "position": globalCall,
		"context": map[string]any{"includeDeclaration": true},
	}).Result)
	for _, line := range []string{`"line":5,`, `"line":6,`, `"line":7,`, `"line":8,`, `"line":19,`, `"line":20,`, `"line":21,`} {
		if strings.Contains(globalReferences, line) {
			t.Fatalf("global references included class member at %s: %s", line, globalReferences)
		}
	}

	memberHighlights := mustJSONText(t, client.request("textDocument/documentHighlight", map[string]any{
		"textDocument": map[string]any{"uri": uri}, "position": memberCall,
	}).Result)
	if !strings.Contains(memberHighlights, `"line":5`) || strings.Contains(memberHighlights, `"line":1,`) || strings.Contains(memberHighlights, `"line":23,`) {
		t.Fatalf("qualified method highlights mismatch: %s", memberHighlights)
	}
	memberRename := mustJSONText(t, client.request("textDocument/rename", map[string]any{
		"textDocument": map[string]any{"uri": uri}, "position": memberCall, "newName": "RenamedABC",
	}).Result)
	if !strings.Contains(memberRename, `"newText":"RenamedABC"`) || strings.Contains(memberRename, `"line":1,`) || strings.Contains(memberRename, `"line":23,`) || strings.Contains(memberRename, `"line":10,`) || strings.Contains(memberRename, `"line":11,`) {
		t.Fatalf("qualified method rename mismatch: %s", memberRename)
	}
}

func TestStdioParityBuildsRichHoverSignaturesForClassProperties(t *testing.T) {
	client := startStdioTestClient(t)
	defer client.close()

	root := t.TempDir()
	uri := pathToFileURI(filepath.Join(root, "vbscript-property-hover.asp"))
	source := `<%
Class DashboardCustomer
  Public Property Get HasItems()
    HasItems = True
  End Property
End Class
%>`
	client.request("initialize", map[string]any{
		"processId":    nil,
		"rootUri":      pathToFileURI(root),
		"capabilities": map[string]any{},
	})
	openClassicASPDocument(t, client, uri, source)

	hover := mustJSONText(t, client.request("textDocument/hover", map[string]any{
		"textDocument": map[string]any{"uri": uri},
		"position":     positionAt(source, strings.Index(source, "HasItems")),
	}).Result)
	if !strings.Contains(hover, "Public Property Get HasItems() As Boolean") {
		t.Fatalf("property hover missing rich signature: %s", hover)
	}
	for _, unexpected := range []string{"VBScript property.", "Member of `DashboardCustomer`."} {
		if strings.Contains(hover, unexpected) {
			t.Fatalf("property hover should not contain %q: %s", unexpected, hover)
		}
	}
}

func TestStdioParityHidesIncludeCodeLensByDefaultAndShowsItWhenEnabled(t *testing.T) {
	client := startStdioTestClient(t)
	defer client.close()

	root := t.TempDir()
	uri := pathToFileURI(filepath.Join(root, "go-include-codelens-default.asp"))
	source := `<!-- #include file="common.inc" -->
<%
Function BuildName()
End Function
Response.Write BuildName()
%>`
	client.request("initialize", map[string]any{
		"processId":    nil,
		"rootUri":      pathToFileURI(root),
		"capabilities": map[string]any{},
	})
	openClassicASPDocument(t, client, uri, source)

	defaultCodeLens := client.request("textDocument/codeLens", map[string]any{
		"textDocument": map[string]any{"uri": uri},
	})
	defaultText := mustJSONText(t, defaultCodeLens.Result)
	if !strings.Contains(defaultText, "reference") || strings.Contains(defaultText, "vscode.open") || strings.Contains(defaultText, "common.inc") {
		t.Fatalf("default CodeLens should include references only: %s", defaultText)
	}

	notifyConfiguration(t, client, map[string]any{"aspLsp": map[string]any{"codeLens": map[string]any{"includes": true}}})
	enabledCodeLens := client.request("textDocument/codeLens", map[string]any{
		"textDocument": map[string]any{"uri": uri},
	})
	enabledText := mustJSONText(t, enabledCodeLens.Result)
	for _, expected := range []string{"reference", "vscode.open", "common.inc"} {
		if !strings.Contains(enabledText, expected) {
			t.Fatalf("enabled CodeLens missing %q: %s", expected, enabledText)
		}
	}

	notifyConfiguration(t, client, map[string]any{"aspLsp": map[string]any{"codeLens": map[string]any{"references": false, "includes": true}}})
	includeOnlyCodeLens := client.request("textDocument/codeLens", map[string]any{
		"textDocument": map[string]any{"uri": uri},
	})
	var includeOnly []lsp.CodeLens
	mustDecodeResult(t, includeOnlyCodeLens.Result, &includeOnly)
	for _, lens := range includeOnly {
		if lens.Data != nil {
			t.Fatalf("include-only CodeLens should not carry reference data: %#v", includeOnly)
		}
	}
	includeOnlyText := mustJSONText(t, includeOnlyCodeLens.Result)
	if !strings.Contains(includeOnlyText, "vscode.open") || !strings.Contains(includeOnlyText, "common.inc") {
		t.Fatalf("include-only CodeLens missing include command: %s", includeOnlyText)
	}
}

func TestStdioParityCurrentFileAndIncludeVBScriptHelp(t *testing.T) {
	client := startStdioTestClient(t)
	defer client.close()

	root := t.TempDir()
	owner := filepath.Join(root, "default.asp")
	include := filepath.Join(root, "common.inc")
	if err := os.WriteFile(include, []byte("<%\nFunction IncludedOnly()\nEnd Function\n%>"), 0o644); err != nil {
		t.Fatal(err)
	}
	marked := markedDocument(`<!-- #include file="common.inc" -->
<%
Function LocalOnly(ByVal value)
LocalOnly = value
End Function
Sub UsesLocal()
LocalOnly(localValue)
End Sub
localValue = 1
Response.Write <<<caret>>>
Response.Write CStr(localValue)
Response.Write IncludedOnly()
%>`)
	if err := os.WriteFile(owner, []byte(marked.Text), 0o644); err != nil {
		t.Fatal(err)
	}
	uri := pathToFileURI(owner)
	client.request("initialize", map[string]any{
		"processId":    nil,
		"rootUri":      pathToFileURI(root),
		"capabilities": map[string]any{},
	})
	openClassicASPDocument(t, client, uri, marked.Text)

	labels := completionLabels(client.request("textDocument/completion", map[string]any{
		"textDocument": map[string]any{"uri": uri},
		"position":     marked.Position,
	}).Result)
	if !labels.contains("LocalOnly") || !labels.contains("IncludedOnly") {
		t.Fatalf("completion labels missing local/include symbols: %#v", labels)
	}

	assertRequestContains(t, client, "textDocument/hover", map[string]any{
		"textDocument": map[string]any{"uri": uri},
		"position":     positionAt(marked.Text, strings.Index(marked.Text, "Write CStr")),
	}, "Response.Write")
	assertRequestContains(t, client, "textDocument/hover", map[string]any{
		"textDocument": map[string]any{"uri": uri},
		"position":     positionAt(marked.Text, strings.Index(marked.Text, "CStr")),
	}, "Function CStr(value) As String")
	assertRequestContains(t, client, "textDocument/hover", map[string]any{
		"textDocument": map[string]any{"uri": uri},
		"position":     positionAt(marked.Text, strings.Index(marked.Text, "LocalOnly")),
	}, "Function LocalOnly(ByVal value)")

	localCallOffset := strings.Index(marked.Text, "LocalOnly(localValue)")
	assertRequestContains(t, client, "textDocument/signatureHelp", map[string]any{
		"textDocument": map[string]any{"uri": uri},
		"position":     positionAt(marked.Text, localCallOffset+len("LocalOnly(")),
	}, "LocalOnly(ByVal value)")
	assertRequestContains(t, client, "textDocument/definition", map[string]any{
		"textDocument": map[string]any{"uri": uri},
		"position":     positionAt(marked.Text, localCallOffset+len("Local")),
	}, `"uri":"`+uri+`"`)
	assertRequestContains(t, client, "textDocument/hover", map[string]any{
		"textDocument": map[string]any{"uri": uri},
		"position":     positionAt(marked.Text, strings.Index(marked.Text, "IncludedOnly()")),
	}, "Function IncludedOnly()")
}

func TestStdioParityIncludedVBScriptSymbolsForCompletionDefinitionAndTokens(t *testing.T) {
	client := startStdioTestClient(t)
	defer client.close()

	root := t.TempDir()
	owner := filepath.Join(root, "default.asp")
	include := filepath.Join(root, "common.inc")
	if err := os.WriteFile(include, []byte("<%\nFunction SharedTitle()\nEnd Function\n%>"), 0o644); err != nil {
		t.Fatal(err)
	}
	marked := markedDocument(`<!-- #include file="common.inc" -->
<%
Response.Write Shared<<<caret>>>Title()
%>`)
	if err := os.WriteFile(owner, []byte(marked.Text), 0o644); err != nil {
		t.Fatal(err)
	}
	uri := pathToFileURI(owner)
	client.request("initialize", map[string]any{
		"processId":    nil,
		"rootUri":      pathToFileURI(root),
		"capabilities": map[string]any{},
	})
	openClassicASPDocument(t, client, uri, marked.Text)

	if !completionLabels(client.request("textDocument/completion", map[string]any{
		"textDocument": map[string]any{"uri": uri},
		"position":     marked.Position,
	}).Result).contains("SharedTitle") {
		t.Fatalf("included symbol completion missing SharedTitle")
	}
	assertRequestContains(t, client, "textDocument/definition", map[string]any{
		"textDocument": map[string]any{"uri": uri},
		"position":     marked.Position,
	}, "common.inc")

	semanticTokens := client.request("textDocument/semanticTokens/full", map[string]any{
		"textDocument": map[string]any{"uri": uri},
	})
	tokens := decodeSemanticTokens(t, semanticTokens.Result)
	if !hasSemanticToken(tokens, marked.Position["line"], marked.Position["character"]-len("Shared"), semanticTokenFunction, 0) {
		t.Fatalf("semantic tokens missing included function usage token: %#v", tokens)
	}
}

func TestStdioParityDefaultVBScriptInlayHints(t *testing.T) {
	client := startStdioTestClient(t)
	defer client.close()

	root := t.TempDir()
	uri := pathToFileURI(filepath.Join(root, "default-inlay-hints.asp"))
	source := `<%
Function BuildName(firstName)
  BuildName = firstName
End Function
Dim pageTitle
pageTitle = BuildName("Dashboard")
%>`
	client.request("initialize", map[string]any{
		"processId":    nil,
		"rootUri":      pathToFileURI(root),
		"capabilities": map[string]any{},
	})
	openClassicASPDocument(t, client, uri, source)

	serialized := requestInlayHintsText(t, client, uri, 0, 7)
	for _, unexpected := range []string{" As ", "(global)", "(local)", "ByRef"} {
		if strings.Contains(serialized, unexpected) {
			t.Fatalf("default inlay hints should not contain %q: %s", unexpected, serialized)
		}
	}
	if !strings.Contains(serialized, "firstName:") {
		t.Fatalf("default inlay hints missing parameter name hint: %s", serialized)
	}
}

func TestStdioParityNoCallSiteParameterHintsOnDeclarations(t *testing.T) {
	client := startStdioTestClient(t)
	defer client.close()

	root := t.TempDir()
	uri := pathToFileURI(filepath.Join(root, "formal-parameter-inlay.asp"))
	source := `<%
Function RenderCustomerRows(ByVal customerList, activeCustomerId)
End Function
Response.Write RenderCustomerRows(customers, activeId)
%>`
	client.request("initialize", map[string]any{
		"processId":    nil,
		"rootUri":      pathToFileURI(root),
		"capabilities": map[string]any{},
	})
	notifyConfiguration(t, client, map[string]any{"aspLsp": map[string]any{"inlayHints": map[string]any{"implicitByRef": true}}})
	openClassicASPDocument(t, client, uri, source)

	declarationHints := requestInlayHintsText(t, client, uri, 1, 2)
	if strings.Contains(declarationHints, "customerList:") || strings.Contains(declarationHints, "activeCustomerId:") || !strings.Contains(declarationHints, `"label":"ByRef "`) {
		t.Fatalf("declaration inlay hints mismatch: %s", declarationHints)
	}
	callHints := requestInlayHintsText(t, client, uri, 3, 4)
	if !strings.Contains(callHints, "customerList:") || !strings.Contains(callHints, "activeCustomerId:") {
		t.Fatalf("call inlay hints missing parameter names: %s", callHints)
	}
}

func TestStdioParityImplicitByRefInlayHintSetting(t *testing.T) {
	client := startStdioTestClient(t)
	defer client.close()

	root := t.TempDir()
	uri := pathToFileURI(filepath.Join(root, "implicit-byref-disabled.asp"))
	source := `<%
Function BuildName(firstName)
End Function
Response.Write BuildName("Ada")
%>`
	client.request("initialize", map[string]any{
		"processId":    nil,
		"rootUri":      pathToFileURI(root),
		"capabilities": map[string]any{},
	})
	notifyConfiguration(t, client, map[string]any{"aspLsp": map[string]any{"inlayHints": map[string]any{"implicitByRef": false}}})
	openClassicASPDocument(t, client, uri, source)

	serialized := requestInlayHintsText(t, client, uri, 0, 5)
	if strings.Contains(serialized, "ByRef") || !strings.Contains(serialized, "firstName:") {
		t.Fatalf("implicit ByRef disabled hints mismatch: %s", serialized)
	}
}

func TestStdioParityParameterNameInlayHintSetting(t *testing.T) {
	client := startStdioTestClient(t)
	defer client.close()

	root := t.TempDir()
	uri := pathToFileURI(filepath.Join(root, "parameter-name-inlay-disabled.asp"))
	source := `<%
Function BuildName(firstName)
End Function
Response.Write BuildName("Ada")
%>`
	client.request("initialize", map[string]any{
		"processId":    nil,
		"rootUri":      pathToFileURI(root),
		"capabilities": map[string]any{},
	})
	notifyConfiguration(t, client, map[string]any{"aspLsp": map[string]any{"inlayHints": map[string]any{"implicitByRef": true, "parameterNames": false}}})
	openClassicASPDocument(t, client, uri, source)

	serialized := requestInlayHintsText(t, client, uri, 0, 5)
	if strings.Contains(serialized, "firstName:") || !strings.Contains(serialized, `"label":"ByRef "`) {
		t.Fatalf("parameter name disabled hints mismatch: %s", serialized)
	}
}

func TestStdioParityRemovedGlobalMarkerSettingIsIgnored(t *testing.T) {
	client := startStdioTestClient(t)
	defer client.close()

	root := t.TempDir()
	uri := pathToFileURI(filepath.Join(root, "global-marker-inlay-disabled.asp"))
	source := `<%
Dim pageTitle
pageTitle = "Dashboard"
Sub Render()
  Dim localTitle
End Sub
%>`
	client.request("initialize", map[string]any{
		"processId":    nil,
		"rootUri":      pathToFileURI(root),
		"capabilities": map[string]any{},
	})
	notifyConfiguration(t, client, map[string]any{"aspLsp": map[string]any{"inlayHints": map[string]any{
		"functionReturnTypes":   true,
		"globalVariableMarkers": "global",
		"variableTypes":         true,
	}}})
	openClassicASPDocument(t, client, uri, source)

	serialized := requestInlayHintsText(t, client, uri, 0, 8)
	if strings.Contains(serialized, "(global)") || !strings.Contains(serialized, `As \"Dashboard\"`) || !strings.Contains(serialized, "As Variant") {
		t.Fatalf("removed global marker setting hints mismatch: %s", serialized)
	}
}

func TestStdioParityLocalVBScriptVariableInlayMarkers(t *testing.T) {
	client := startStdioTestClient(t)
	defer client.close()

	root := t.TempDir()
	uri := pathToFileURI(filepath.Join(root, "local-marker-inlay.asp"))
	source := `<%
Dim pageTitle
Sub Render()
  Dim localTitle
End Sub
%>`
	client.request("initialize", map[string]any{
		"processId":    nil,
		"rootUri":      pathToFileURI(root),
		"capabilities": map[string]any{},
	})
	notifyConfiguration(t, client, map[string]any{"aspLsp": map[string]any{"inlayHints": map[string]any{
		"scopeMarkers":  map[string]any{"global": false, "local": true, "uncertain": false},
		"variableTypes": true,
	}}})
	openClassicASPDocument(t, client, uri, source)

	serialized := requestInlayHintsText(t, client, uri, 0, 7)
	if strings.Contains(serialized, "(global) As Variant") || !strings.Contains(serialized, "(local) As Variant") {
		t.Fatalf("local marker hints mismatch: %s", serialized)
	}
}

func TestStdioParityLocalAndUncertainVBScriptVariableInlayMarkers(t *testing.T) {
	client := startStdioTestClient(t)
	defer client.close()

	root := t.TempDir()
	uri := pathToFileURI(filepath.Join(root, "include-uncertain-inlay.asp"))
	source := `<!-- #include file="shared.inc" -->
<%
a = 1
Sub Render()
  Dim b
  b = "local"
End Sub
%>`
	client.request("initialize", map[string]any{
		"processId":    nil,
		"rootUri":      pathToFileURI(root),
		"capabilities": map[string]any{},
	})
	notifyConfiguration(t, client, map[string]any{"aspLsp": map[string]any{"inlayHints": map[string]any{
		"functionReturnTypes": true,
		"scopeMarkers":        map[string]any{"global": true, "local": true, "uncertain": true},
		"variableTypes":       true,
	}}})
	openClassicASPDocument(t, client, uri, source)

	serialized := requestInlayHintsText(t, client, uri, 0, 8)
	if !strings.Contains(serialized, "(?) As 1") || strings.Contains(serialized, "(global) As 1") || !strings.Contains(serialized, `(local) As \"local\"`) {
		t.Fatalf("local/uncertain marker hints mismatch: %s", serialized)
	}

	notifyConfiguration(t, client, map[string]any{"aspLsp": map[string]any{"inlayHints": map[string]any{
		"scopeMarkers":  map[string]any{"global": false, "local": true, "uncertain": false},
		"variableTypes": true,
	}}})
	localOnlyURI := pathToFileURI(filepath.Join(root, "local-marker-inlay-combined.asp"))
	localOnlySource := `<%
Dim pageTitle
Sub Render()
  Dim localTitle
End Sub
%>`
	openClassicASPDocument(t, client, localOnlyURI, localOnlySource)
	localHints := requestInlayHintsText(t, client, localOnlyURI, 0, 7)
	if strings.Contains(localHints, "(global) As Variant") || !strings.Contains(localHints, "(local) As Variant") {
		t.Fatalf("combined local marker hints mismatch: %s", localHints)
	}
}

func TestStdioParityIncludeAwareInlayMarkersAfterFullReparseEdit(t *testing.T) {
	client := startStdioTestClient(t)
	defer client.close()

	root := t.TempDir()
	include := filepath.Join(root, "shared.inc")
	if err := os.WriteFile(include, []byte("<%\na = 1\n%>"), 0o644); err != nil {
		t.Fatal(err)
	}
	source := `<!-- #include file="shared.inc" -->
<div>top</div>
<%
Response.Write a
a = 2
%>`
	editedSource := strings.Replace(source, "<div>top</div>", `<div data-note="<script>">top</div>`, 1)
	uri := pathToFileURI(filepath.Join(root, "default.asp"))
	client.request("initialize", map[string]any{
		"processId":    nil,
		"rootUri":      pathToFileURI(root),
		"capabilities": map[string]any{},
	})
	notifyConfiguration(t, client, map[string]any{"aspLsp": map[string]any{
		"diagnostics": map[string]any{"debounceMs": 0},
		"inlayHints": map[string]any{
			"functionReturnTypes": true,
			"scopeMarkers":        map[string]any{"global": true, "local": true, "uncertain": true},
			"variableTypes":       true,
		},
	}})
	openClassicASPDocument(t, client, uri, source)
	client.request("textDocument/diagnostic", map[string]any{"textDocument": map[string]any{"uri": uri}})

	warmedHints := mustJSONText(t, client.request("textDocument/inlayHint", map[string]any{
		"textDocument": map[string]any{"uri": uri},
		"range":        map[string]any{"start": map[string]any{"line": 0, "character": 0}, "end": positionAt(source, len(source))},
	}).Result)
	if strings.Contains(warmedHints, "(global) As Number") || strings.Contains(warmedHints, "(?)") {
		t.Fatalf("warmed include-aware hints mismatch: %s", warmedHints)
	}

	if err := client.notify("textDocument/didChange", map[string]any{
		"textDocument":   map[string]any{"uri": uri, "version": 2},
		"contentChanges": []map[string]any{{"text": editedSource}},
	}); err != nil {
		t.Fatal(err)
	}
	client.request("textDocument/diagnostic", map[string]any{"textDocument": map[string]any{"uri": uri}})
	immediateHints := mustJSONText(t, client.request("textDocument/inlayHint", map[string]any{
		"textDocument": map[string]any{"uri": uri},
		"range":        map[string]any{"start": map[string]any{"line": 0, "character": 0}, "end": positionAt(editedSource, len(editedSource))},
	}).Result)
	if strings.Contains(immediateHints, "(global) As Number") || strings.Contains(immediateHints, "(?)") {
		t.Fatalf("immediate include-aware hints mismatch after edit: %s", immediateHints)
	}
}

func TestStdioParityFirstInlayRequestReadsIncludeSummaries(t *testing.T) {
	client := startStdioTestClient(t)
	defer client.close()

	root := t.TempDir()
	include := filepath.Join(root, "shared.inc")
	if err := os.WriteFile(include, []byte("<%\na = 1\nsharedTitle = \"include\"\n%>"), 0o644); err != nil {
		t.Fatal(err)
	}
	source := `<!-- #include file="shared.inc" -->
<%
Response.Write sharedTitle
a = 2
Sub Render()
  Dim b
  b = "local"
End Sub
%>`
	uri := pathToFileURI(filepath.Join(root, "default.asp"))
	client.request("initialize", map[string]any{
		"processId":    nil,
		"rootUri":      pathToFileURI(root),
		"capabilities": map[string]any{},
	})
	notifyConfiguration(t, client, map[string]any{"aspLsp": map[string]any{
		"diagnostics": map[string]any{"debounceMs": 10_000},
		"inlayHints": map[string]any{
			"functionReturnTypes": true,
			"scopeMarkers":        map[string]any{"global": true, "local": true, "uncertain": true},
			"variableTypes":       true,
		},
	}})
	if err := client.notify("textDocument/didOpen", map[string]any{
		"textDocument": map[string]any{
			"uri":        uri,
			"languageId": "classic-asp",
			"version":    1,
			"text":       source,
		},
	}); err != nil {
		t.Fatal(err)
	}

	firstHints := mustJSONText(t, client.request("textDocument/inlayHint", map[string]any{
		"textDocument": map[string]any{"uri": uri},
		"range":        map[string]any{"start": map[string]any{"line": 0, "character": 0}, "end": positionAt(source, len(source))},
	}).Result)
	if strings.Contains(firstHints, "(?)") || strings.Contains(firstHints, "(global) As 1") || !strings.Contains(firstHints, `(local) As \"local\"`) {
		t.Fatalf("first inlay hints did not use include summaries: %s", firstHints)
	}
	assertRequestContains(t, client, "textDocument/hover", map[string]any{
		"textDocument": map[string]any{"uri": uri},
		"position":     positionAt(source, strings.Index(source, "sharedTitle")),
	}, vbscriptHoverCodeBlockJSON(`(global) Dim sharedTitle As \"include\"`))
	assertRequestContains(t, client, "textDocument/hover", map[string]any{
		"textDocument": map[string]any{"uri": uri},
		"position":     positionAt(source, strings.Index(source, "sharedTitle")),
	}, "shared.inc")
	sharedHover := hoverMarkdownValue(t, client.request("textDocument/hover", map[string]any{
		"textDocument": map[string]any{"uri": uri},
		"position":     positionAt(source, strings.Index(source, "sharedTitle")),
	}).Result)
	for _, expected := range []string{"Defined in [shared.inc]", pathToFileURI(include)} {
		if !strings.Contains(sharedHover, expected) {
			t.Fatalf("included variable hover missing %q: %s", expected, sharedHover)
		}
	}
}

func TestStdioParityPrewarmsIncludeBackedVBScriptContextAfterDidOpen(t *testing.T) {
	client := startStdioTestClient(t)
	defer client.close()

	root := t.TempDir()
	if err := os.WriteFile(filepath.Join(root, "shared.inc"), []byte("<%\nsharedOpenTitle = \"include\"\n%>"), 0o644); err != nil {
		t.Fatal(err)
	}
	source := `<!-- #include file="shared.inc" -->
<%
shared
%>`
	uri := pathToFileURI(filepath.Join(root, "default.asp"))
	client.request("initialize", map[string]any{
		"processId":    nil,
		"rootUri":      pathToFileURI(root),
		"capabilities": map[string]any{},
	})
	notifyConfiguration(t, client, map[string]any{"aspLsp": map[string]any{
		"debug":       map[string]any{"output": "verbose"},
		"diagnostics": map[string]any{"debounceMs": 0},
	}})
	if err := client.notify("textDocument/didOpen", map[string]any{
		"textDocument": map[string]any{
			"uri":        uri,
			"languageId": "classic-asp",
			"version":    1,
			"text":       source,
		},
	}); err != nil {
		t.Fatal(err)
	}

	client.waitForNotification("window/logMessage", "reason=document.open.prewarm")
	completions := completionLabels(client.request("textDocument/completion", map[string]any{
		"textDocument": map[string]any{"uri": uri},
		"position":     positionAt(source, strings.LastIndex(source, "shared")+len("shared")),
	}).Result)
	if !completions.contains("sharedOpenTitle") {
		t.Fatalf("prewarmed include completion missing sharedOpenTitle: %#v", completions)
	}
}

func TestStdioParitySummaryBackedVBCompletionAfterEdits(t *testing.T) {
	client := startStdioTestClient(t)
	defer client.close()

	root := t.TempDir()
	includeDirectives := make([]string, 0, 30)
	for index := 0; index < 30; index++ {
		includeName := fmt.Sprintf("shared-%d.inc", index)
		includeDirectives = append(includeDirectives, fmt.Sprintf(`<!-- #include file="%s" -->`, includeName))
		content := fmt.Sprintf("<%%\nFunction SharedExport%d()\nEnd Function\n%%>", index)
		if err := os.WriteFile(filepath.Join(root, includeName), []byte(content), 0o644); err != nil {
			t.Fatal(err)
		}
	}
	source := strings.Join(includeDirectives, "\n") + `
<%
Dim localValue
Response.Write Sha
Response.Write SharedExport29()
%>`
	uri := pathToFileURI(filepath.Join(root, "default.asp"))
	client.request("initialize", map[string]any{
		"processId":    nil,
		"rootUri":      pathToFileURI(root),
		"capabilities": map[string]any{},
	})
	notifyConfiguration(t, client, map[string]any{"aspLsp": map[string]any{
		"debug":       map[string]any{"output": "verbose"},
		"diagnostics": map[string]any{"debounceMs": 0},
	}})
	openClassicASPDocument(t, client, uri, source)
	client.waitForNotification("window/logMessage", "LSP check completed")

	completionPosition := func(text string) map[string]int {
		return positionAt(text, strings.Index(text, "Response.Write Sha")+len("Response.Write Sha"))
	}
	warmedCompletions := completionLabels(client.request("textDocument/completion", map[string]any{
		"textDocument": map[string]any{"uri": uri},
		"position":     completionPosition(source),
	}).Result)
	if !warmedCompletions.contains("SharedExport29") {
		t.Fatalf("warmed completions missing SharedExport29: %#v", warmedCompletions)
	}
	warmedTokens := decodeSemanticTokens(t, client.request("textDocument/semanticTokens/full", map[string]any{
		"textDocument": map[string]any{"uri": uri},
	}).Result)
	if !hasTokenMatchingText(source, warmedTokens, "SharedExport29", semanticTokenFunction) {
		t.Fatalf("warmed semantic tokens missing SharedExport29 function token")
	}

	source = notifyRangedReplacement(t, client, uri, source, 31, "localValue", "localOther")
	immediateCompletions := completionLabels(client.request("textDocument/completion", map[string]any{
		"textDocument": map[string]any{"uri": uri},
		"position":     completionPosition(source),
	}).Result)
	if !immediateCompletions.contains("SharedExport29") {
		t.Fatalf("immediate completions missing SharedExport29: %#v", immediateCompletions)
	}
	immediateTokens := decodeSemanticTokens(t, client.request("textDocument/semanticTokens/full", map[string]any{
		"textDocument": map[string]any{"uri": uri},
	}).Result)
	if !hasTokenMatchingText(source, immediateTokens, "SharedExport29", semanticTokenFunction) {
		t.Fatalf("immediate semantic tokens missing SharedExport29 function token")
	}
	client.waitForNotification("window/logMessage", "LSP check completed")
	refreshedCompletions := completionLabels(client.request("textDocument/completion", map[string]any{
		"textDocument": map[string]any{"uri": uri},
		"position":     completionPosition(source),
	}).Result)
	if !refreshedCompletions.contains("SharedExport29") {
		t.Fatalf("refreshed completions missing SharedExport29: %#v", refreshedCompletions)
	}
}

func TestStdioParityKeepsLargeVBProjectEditedInlayHintsCurrent(t *testing.T) {
	client := startStdioTestClient(t)
	defer client.close()

	root := t.TempDir()
	owner := filepath.Join(root, "default.asp")
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
Dim localValue
localValue = 1
Response.Write localValue
%>`
	if err := os.WriteFile(owner, []byte(source), 0o644); err != nil {
		t.Fatal(err)
	}
	uri := pathToFileURI(owner)
	client.request("initialize", map[string]any{
		"processId":    nil,
		"rootUri":      pathToFileURI(root),
		"capabilities": map[string]any{},
	})
	notifyConfiguration(t, client, map[string]any{"aspLsp": map[string]any{
		"debug":       map[string]any{"output": "summary"},
		"diagnostics": map[string]any{"debounceMs": 0},
		"inlayHints": map[string]any{
			"functionReturnTypes": true,
			"scopeMarkers":        map[string]any{"global": true, "local": true, "uncertain": true},
			"variableTypes":       true,
		},
	}})
	if err := client.notify("textDocument/didOpen", map[string]any{
		"textDocument": map[string]any{
			"uri":        uri,
			"languageId": "classic-asp",
			"version":    1,
			"text":       source,
		},
	}); err != nil {
		t.Fatal(err)
	}
	client.waitForNotification("window/logMessage", "LSP check completed")

	initialHints := mustJSONText(t, client.request("textDocument/inlayHint", map[string]any{
		"textDocument": map[string]any{"uri": uri},
		"range":        map[string]any{"start": map[string]any{"line": 0, "character": 0}, "end": positionAt(source, len(source))},
	}).Result)
	if !strings.Contains(initialHints, "As 1") {
		t.Fatalf("initial inlay hints missing As 1: %s", initialHints)
	}

	source = notifyNeedleReplacement(t, client, uri, source, 2, "localValue = 1", `localValue = "one"`)
	editedHints := mustJSONText(t, client.request("textDocument/inlayHint", map[string]any{
		"textDocument": map[string]any{"uri": uri},
		"range":        map[string]any{"start": map[string]any{"line": 0, "character": 0}, "end": positionAt(source, len(source))},
	}).Result)
	if !strings.Contains(editedHints, `As \"one\"`) || strings.Contains(editedHints, "As 1") {
		t.Fatalf("edited inlay hints mismatch: %s", editedHints)
	}
}

func TestStdioParityInvalidatesVBCompletionCacheAfterWatchedIncludeExportChanges(t *testing.T) {
	client := startStdioTestClient(t)
	defer client.close()

	root := t.TempDir()
	include := filepath.Join(root, "shared.inc")
	if err := os.WriteFile(include, []byte("<%\nFunction SharedOld()\nEnd Function\n%>"), 0o644); err != nil {
		t.Fatal(err)
	}
	source := `<!-- #include file="shared.inc" -->
<%
Response.Write Sha
%>`
	owner := filepath.Join(root, "default.asp")
	if err := os.WriteFile(owner, []byte(source), 0o644); err != nil {
		t.Fatal(err)
	}
	uri := pathToFileURI(owner)
	client.request("initialize", map[string]any{
		"processId":    nil,
		"rootUri":      pathToFileURI(root),
		"capabilities": map[string]any{},
	})
	notifyConfiguration(t, client, map[string]any{"aspLsp": map[string]any{
		"debug":       map[string]any{"output": "verbose"},
		"diagnostics": map[string]any{"debounceMs": 0},
	}})
	openClassicASPDocument(t, client, uri, source)
	client.waitForNotification("window/logMessage", "LSP check completed")

	position := positionAt(source, strings.Index(source, "Response.Write Sha")+len("Response.Write Sha"))
	oldCompletions := completionLabels(client.request("textDocument/completion", map[string]any{
		"textDocument": map[string]any{"uri": uri},
		"position":     position,
	}).Result)
	if !oldCompletions.contains("SharedOld") {
		t.Fatalf("old completions missing SharedOld: %#v", oldCompletions)
	}

	if err := os.WriteFile(include, []byte("<%\nFunction SharedNew()\nEnd Function\n%>"), 0o644); err != nil {
		t.Fatal(err)
	}
	if err := client.notify("workspace/didChangeWatchedFiles", map[string]any{
		"changes": []map[string]any{{"uri": pathToFileURI(include), "type": 2}},
	}); err != nil {
		t.Fatal(err)
	}
	client.waitForNotification("window/logMessage", "LSP check completed")

	newCompletions := completionLabels(client.request("textDocument/completion", map[string]any{
		"textDocument": map[string]any{"uri": uri},
		"position":     position,
	}).Result)
	if !newCompletions.contains("SharedNew") || newCompletions.contains("SharedOld") {
		t.Fatalf("watched include completions mismatch: %#v", newCompletions)
	}
}
