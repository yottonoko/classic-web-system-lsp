package lspserver

import (
	"path/filepath"
	"strings"
	"testing"

	"github.com/yottonoko/classic-web-system-lsp/internal/lsp"
)

const (
	semanticTokenConstant       = 16
	semanticModifierPublic      = 1 << 0
	semanticModifierReadonly    = 1 << 2
	semanticModifierLibrary     = 1 << 3
	semanticModifierLibraryOnly = semanticModifierLibrary
)

func TestStdioParityReturnsHoverSignatureHelpAndSemanticTokensForVBScriptBuiltInFunctions(t *testing.T) {
	client := startStdioTestClient(t)
	defer client.close()

	root := t.TempDir()
	uri := pathToFileURI(filepath.Join(root, "vbscript-builtins.asp"))
	source := `<%
Dim textValue
Randomize Timer
textValue = CStr(42)
Response.Write UBound(Array("a", "b"))
%>`
	client.request("initialize", map[string]any{
		"processId":    nil,
		"rootUri":      pathToFileURI(root),
		"capabilities": map[string]any{},
	})
	notifyOpenClassicASPDocument(t, client, uri, source)
	waitForDiagnosticsContaining(t, client, "")

	cstrHover := client.request("textDocument/hover", map[string]any{
		"textDocument": map[string]any{"uri": uri},
		"position":     positionAt(source, strings.Index(source, "CStr")+1),
	})
	cstrHoverText := hoverMarkdownValue(t, cstrHover.Result)
	if cstrHoverText != "```vbscript\nFunction CStr(value) As String\n```\n\nConverts a value to String." ||
		strings.Contains(cstrHoverText, "VBScript built-in function.") {
		t.Fatalf("CStr hover mismatch: %s", cstrHoverText)
	}

	randomizeHover := client.request("textDocument/hover", map[string]any{
		"textDocument": map[string]any{"uri": uri},
		"position":     positionAt(source, strings.Index(source, "Randomize")+1),
	})
	randomizeHoverText := hoverMarkdownValue(t, randomizeHover.Result)
	if randomizeHoverText != "```vbscript\nFunction Randomize(number) As Variant\n```\n\nInitializes the random-number generator." {
		t.Fatalf("Randomize hover mismatch: %s", randomizeHoverText)
	}

	arrayHover := client.request("textDocument/hover", map[string]any{
		"textDocument": map[string]any{"uri": uri},
		"position":     positionAt(source, strings.Index(source, `Array("a"`)+1),
	})
	arrayHoverText := hoverMarkdownValue(t, arrayHover.Result)
	if arrayHoverText != "```vbscript\nFunction Array(values) As Array\n```\n\nCreates a Variant array." {
		t.Fatalf("Array hover mismatch: %s", arrayHoverText)
	}

	signature := client.request("textDocument/signatureHelp", map[string]any{
		"textDocument": map[string]any{"uri": uri},
		"position":     positionAt(source, strings.Index(source, `Array("a"`)+len(`Array("`)),
	})
	if !strings.Contains(mustJSONText(t, signature.Result), "Array(values)") {
		t.Fatalf("Array signature help mismatch: %s", mustJSONText(t, signature.Result))
	}

	responseWriteHover := client.request("textDocument/hover", map[string]any{
		"textDocument": map[string]any{"uri": uri},
		"position":     positionAt(source, strings.Index(source, "Response.Write")+len("Response.")),
	})
	if responseWriteHoverText := hoverMarkdownValue(t, responseWriteHover.Result); responseWriteHoverText != "```vbscript\nResponse.Write value\n```\n\nWrites output to the HTTP response." {
		t.Fatalf("Response.Write hover mismatch: %s", responseWriteHoverText)
	}

	semanticTokens := client.request("textDocument/semanticTokens/full", map[string]any{
		"textDocument": map[string]any{"uri": uri},
	})
	decoded := decodeSemanticTokens(t, semanticTokens.Result)
	for _, name := range []string{"CStr", "Randomize", "UBound", "Array"} {
		if !hasTokenMatchingTextAndModifiers(source, decoded, name, semanticTokenFunction, semanticModifierLibraryOnly) {
			t.Fatalf("semantic tokens missing library function %s: %#v", name, decoded)
		}
	}
	if !hasTokenMatchingTextAndModifiers(source, decoded, "Write", semanticTokenMethod, semanticModifierLibraryOnly) {
		t.Fatalf("semantic tokens missing Response.Write library method: %#v", decoded)
	}
}

func signatureDocumentationText(value any) string {
	switch doc := value.(type) {
	case string:
		return doc
	case lsp.MarkupContent:
		return doc.Value
	case map[string]any:
		if value, ok := doc["value"].(string); ok {
			return value
		}
	}
	return ""
}

func TestStdioParityRecognizesVBScriptLanguageReferenceBuiltInFunctions(t *testing.T) {
	client := startStdioTestClient(t)
	defer client.close()

	root := t.TempDir()
	uri := pathToFileURI(filepath.Join(root, "reference-builtins.asp"))
	source := `<%
Option Explicit
Dim ascByte, ascUnicode, chrByte, chrUnicode
Dim bytePosition, byteLength, leftBytes, midBytes, rightBytes
Dim textLength, lowercaseText
Dim localeValue, previousLocale, objectValue, refValue, inputValue, pictureValue, messageResult
ascByte = AscB("A")
ascUnicode = AscW("A")
chrByte = ChrB(65)
chrUnicode = ChrW(65)
textLength = Len("abc")
lowercaseText = LCase("ABC")
bytePosition = InStrB(1, "abc", "b")
byteLength = LenB("abc")
leftBytes = LeftB("abc", 1)
midBytes = MidB("abc", 2, 1)
rightBytes = RightB("abc", 1)
localeValue = GetLocale()
previousLocale = SetLocale(localeValue)
Set objectValue = GetObject("", "Scripting.Dictionary")
Set refValue = GetRef("Handler")
inputValue = InputBox("Prompt")
Set pictureValue = LoadPicture("image.bmp")
messageResult = MsgBox("Hello")
%>`
	client.request("initialize", map[string]any{
		"processId":    nil,
		"rootUri":      pathToFileURI(root),
		"capabilities": map[string]any{},
	})
	notifyConfiguration(t, client, map[string]any{"aspLsp": map[string]any{
		"inlayHints": map[string]any{"variableTypes": true},
	}})
	diagnostics := openClassicASPDocumentWithDiagnostics(t, client, uri, source)
	if serialized := mustJSONText(t, diagnosticsFromSource(t, diagnostics, "asp-lsp-vbscript")); serialized != "[]" && serialized != "null" {
		t.Fatalf("language reference built-ins produced undeclared diagnostics: %s", serialized)
	}

	hints := requestInlayHintsText(t, client, uri, 0, 23)
	for _, expected := range []string{"As Number", "As String", "As Object"} {
		if !strings.Contains(hints, expected) {
			t.Fatalf("language reference built-in inlay hints missing %q: %s", expected, hints)
		}
	}

	semanticTokens := client.request("textDocument/semanticTokens/full", map[string]any{
		"textDocument": map[string]any{"uri": uri},
	})
	decoded := decodeSemanticTokens(t, semanticTokens.Result)
	for _, label := range []string{"AscB", "AscW", "ChrB", "ChrW", "Len", "LCase", "InStrB", "LenB", "LeftB", "MidB", "RightB", "GetLocale", "SetLocale", "GetObject", "GetRef", "InputBox", "LoadPicture", "MsgBox"} {
		if !hasTokenMatchingTextAndModifiers(source, decoded, label, semanticTokenFunction, semanticModifierLibraryOnly) {
			t.Fatalf("semantic tokens missing built-in %s: %#v", label, decoded)
		}
	}

	lenHover := client.request("textDocument/hover", map[string]any{
		"textDocument": map[string]any{"uri": uri},
		"position":     positionAt(source, strings.Index(source, "Len(")),
	})
	if lenHoverText := mustJSONText(t, lenHover.Result); !strings.Contains(lenHoverText, "Function Len(value) As Number") ||
		!strings.Contains(lenHoverText, "Returns the number of characters in a string.") {
		t.Fatalf("Len hover mismatch: %s", lenHoverText)
	}

	lenBHover := client.request("textDocument/hover", map[string]any{
		"textDocument": map[string]any{"uri": uri},
		"position":     positionAt(source, strings.Index(source, "LenB")),
	})
	if !strings.Contains(mustJSONText(t, lenBHover.Result), "Function LenB(value) As Number") {
		t.Fatalf("LenB hover mismatch: %s", mustJSONText(t, lenBHover.Result))
	}

	signature := client.request("textDocument/signatureHelp", map[string]any{
		"textDocument": map[string]any{"uri": uri},
		"position":     positionAt(source, strings.Index(source, `MsgBox("Hello"`)+len("MsgBox(")),
	})
	if !strings.Contains(mustJSONText(t, signature.Result), "MsgBox(prompt, buttons, title, helpfile, context)") {
		t.Fatalf("MsgBox signature help mismatch: %s", mustJSONText(t, signature.Result))
	}
}

func TestStdioParityCoversFullVBScriptBuiltInCatalogSurface(t *testing.T) {
	client := startStdioTestClient(t)
	defer client.close()

	root := t.TempDir()
	uri := pathToFileURI(filepath.Join(root, "full-builtin-catalog.asp"))
	source := `<%
Option Explicit
Dim absValue, filtered, formatted, dateValue, typeNameValue, varTypeValue
Dim weekdayLabel, majorVersion, firstWeek, adoType
absValue = Abs(-42)
filtered = Filter(Array("active", "archived"), "active")
formatted = FormatNumber(absValue, 2)
dateValue = DateSerial(2026, 6, 29)
typeNameValue = TypeName(filtered)
varTypeValue = VarType(dateValue)
weekdayLabel = WeekdayName(Weekday(dateValue), False, vbMonday)
majorVersion = ScriptEngineMajorVersion()
firstWeek = vbFirstFourDays
adoType = adVarWChar
%>`
	client.request("initialize", map[string]any{
		"processId":    nil,
		"rootUri":      pathToFileURI(root),
		"capabilities": map[string]any{},
	})
	diagnostics := openClassicASPDocumentWithDiagnostics(t, client, uri, source)
	if serialized := mustJSONText(t, diagnosticsFromSource(t, diagnostics, "asp-lsp-vbscript")); serialized != "[]" && serialized != "null" {
		t.Fatalf("full built-in catalog produced undeclared diagnostics: %s", serialized)
	}

	absHover := mustJSONText(t, client.request("textDocument/hover", map[string]any{
		"textDocument": map[string]any{"uri": uri},
		"position":     positionAt(source, strings.Index(source, "Abs(")),
	}).Result)
	for _, expected := range []string{"Function Abs(number) As Number", "Returns the absolute value of a number."} {
		if !strings.Contains(absHover, expected) {
			t.Fatalf("Abs hover missing %q: %s", expected, absHover)
		}
	}

	constantHover := mustJSONText(t, client.request("textDocument/hover", map[string]any{
		"textDocument": map[string]any{"uri": uri},
		"position":     positionAt(source, strings.Index(source, "vbFirstFourDays")),
	}).Result)
	for _, expected := range []string{"Const vbFirstFourDays As Number", "VBScript constant available without an explicit Const declaration."} {
		if !strings.Contains(constantHover, expected) {
			t.Fatalf("vbFirstFourDays hover missing %q: %s", expected, constantHover)
		}
	}

	signature := mustJSONText(t, client.request("textDocument/signatureHelp", map[string]any{
		"textDocument": map[string]any{"uri": uri},
		"position":     positionAt(source, strings.Index(source, "FormatNumber(absValue")+len("FormatNumber(")),
	}).Result)
	if !strings.Contains(signature, "FormatNumber(expression, digitsAfterDecimal, includeLeadingDigit, useParensForNegativeNumbers, groupDigits)") {
		t.Fatalf("FormatNumber signature help mismatch: %s", signature)
	}

	semanticTokens := client.request("textDocument/semanticTokens/full", map[string]any{
		"textDocument": map[string]any{"uri": uri},
	})
	decoded := decodeSemanticTokens(t, semanticTokens.Result)
	for _, label := range []string{"Abs", "Filter", "FormatNumber", "DateSerial", "TypeName", "VarType", "WeekdayName", "Weekday", "ScriptEngineMajorVersion"} {
		if !hasTokenMatchingTextAndModifiers(source, decoded, label, semanticTokenFunction, semanticModifierLibraryOnly) {
			t.Fatalf("semantic tokens missing catalog function %s: %#v", label, decoded)
		}
	}
	for _, label := range []string{"vbMonday", "vbFirstFourDays", "adVarWChar"} {
		if !hasTokenMatchingTextAndModifiers(source, decoded, label, semanticTokenConstant, semanticModifierReadonly|semanticModifierLibrary) {
			t.Fatalf("semantic tokens missing catalog constant %s: %#v", label, decoded)
		}
	}

	topLevelItems := completionItems(client.request("textDocument/completion", map[string]any{
		"textDocument": map[string]any{"uri": uri},
		"position":     positionAt(source, strings.Index(source, "Dim absValue")),
	}).Result)
	for _, label := range []string{"FormatNumber", "vbFirstFourDays", "adVarWChar"} {
		if _, ok := topLevelItems.find(label); !ok {
			t.Fatalf("top-level completions missing %s: %#v", label, topLevelItems)
		}
	}

	formatNumberItem, _ := topLevelItems.find("FormatNumber")
	formatNumberResolved := mustJSONText(t, client.request("completionItem/resolve", formatNumberItem).Result)
	for _, expected := range []string{"Function FormatNumber(expression", "Formats an expression as a number."} {
		if !strings.Contains(formatNumberResolved, expected) {
			t.Fatalf("FormatNumber completion docs missing %q: %s", expected, formatNumberResolved)
		}
	}

	adVarWCharItem, _ := topLevelItems.find("adVarWChar")
	adVarWCharResolved := mustJSONText(t, client.request("completionItem/resolve", adVarWCharItem).Result)
	for _, expected := range []string{"Const adVarWChar As Number", "ADO data type constant for a variable-length Unicode string."} {
		if !strings.Contains(adVarWCharResolved, expected) {
			t.Fatalf("adVarWChar completion docs missing %q: %s", expected, adVarWCharResolved)
		}
	}
}

func TestStdioParitySupportsVBScriptOnErrorCompletionsAndErrBuiltIns(t *testing.T) {
	client := startStdioTestClient(t)
	defer client.close()

	root := t.TempDir()
	uri := pathToFileURI(filepath.Join(root, "vbscript-err-builtins.asp"))
	source := `<%
Option Explicit
On Error Resume Next
Dim captured, errNumber
Set captured = Err
errNumber = Err.Number
Err.Raise(vbObjectError + 513, "App", "Boom")
Err.
%>`
	client.request("initialize", map[string]any{
		"processId":    nil,
		"rootUri":      pathToFileURI(root),
		"capabilities": map[string]any{},
	})
	notifyOpenClassicASPDocument(t, client, uri, source)
	published := waitForDiagnosticsContaining(t, client, "")
	if diagnostics := diagnosticsFromMessage(t, published); len(diagnostics) != 0 {
		t.Fatalf("Err produced diagnostics: %#v", diagnostics)
	}

	memberCompletions := completionLabels(client.request("textDocument/completion", map[string]any{
		"textDocument": map[string]any{"uri": uri},
		"position":     positionAt(source, strings.LastIndex(source, "Err.")+len("Err.")),
	}).Result)
	for _, label := range []string{"Number", "Description", "Clear", "Raise"} {
		if !memberCompletions.contains(label) {
			t.Fatalf("Err member completions missing %s: %#v", label, memberCompletions)
		}
	}

	hover := client.request("textDocument/hover", map[string]any{
		"textDocument": map[string]any{"uri": uri},
		"position":     positionAt(source, strings.Index(source, "Err.Number")+len("Err.")),
	})
	if !strings.Contains(mustJSONText(t, hover.Result), "property ErrObject.Number As Number") {
		t.Fatalf("Err.Number hover mismatch: %s", mustJSONText(t, hover.Result))
	}

	signature := client.request("textDocument/signatureHelp", map[string]any{
		"textDocument": map[string]any{"uri": uri},
		"position":     positionAt(source, strings.Index(source, "Err.Raise(")+len("Err.Raise(")),
	})
	if !strings.Contains(mustJSONText(t, signature.Result), "Err.Raise(number, source, description, helpfile, helpcontext)") {
		t.Fatalf("Err.Raise signature help mismatch: %s", mustJSONText(t, signature.Result))
	}

	semanticTokens := client.request("textDocument/semanticTokens/full", map[string]any{
		"textDocument": map[string]any{"uri": uri},
	})
	decoded := decodeSemanticTokens(t, semanticTokens.Result)
	if !hasTokenAtOffset(source, decoded, strings.Index(source, "Set captured = Err")+len("Set captured = "), semanticTokenConstant, semanticModifierReadonly|semanticModifierLibrary) {
		t.Fatalf("semantic tokens missing Err readonly library constant: %#v", decoded)
	}
	if !hasTokenAtOffset(source, decoded, strings.Index(source, "Err.Number")+len("Err."), semanticTokenProperty, semanticModifierLibraryOnly) {
		t.Fatalf("semantic tokens missing Err.Number library property: %#v", decoded)
	}
	if !hasTokenAtOffset(source, decoded, strings.Index(source, "Err.Raise")+len("Err."), semanticTokenMethod, semanticModifierLibraryOnly) {
		t.Fatalf("semantic tokens missing Err.Raise library method: %#v", decoded)
	}

	completionURI := pathToFileURI(filepath.Join(root, "vbscript-on-error-completion.asp"))
	completionSource := `<%
Error R
%>`
	notifyOpenClassicASPDocument(t, client, completionURI, completionSource)
	waitForDiagnosticsContaining(t, client, completionURI)

	erCompletions := completionLabels(client.request("textDocument/completion", map[string]any{
		"textDocument": map[string]any{"uri": completionURI},
		"position":     positionAt(completionSource, strings.Index(completionSource, "Error R")+len("Er")),
	}).Result)
	for _, label := range []string{"On Error Resume Next", "On Error GoTo 0"} {
		if !erCompletions.contains(label) {
			t.Fatalf("Er completions missing %s: %#v", label, erCompletions)
		}
	}

	onErrorCompletions := completionItems(client.request("textDocument/completion", map[string]any{
		"textDocument": map[string]any{"uri": completionURI},
		"position":     positionAt(completionSource, strings.Index(completionSource, "Error R")+len("Error R")),
	}).Result)
	if len(onErrorCompletions) != 1 ||
		onErrorCompletions[0].Label != "On Error Resume Next" ||
		onErrorCompletions[0].FilterText != "Error Resume Next" {
		t.Fatalf("On Error completion mismatch: %#v", onErrorCompletions)
	}
}

func TestStdioParityCoversASPObjectMembersRuntimeEventsAndMemberHover(t *testing.T) {
	client := startStdioTestClient(t)
	defer client.close()

	root := t.TempDir()
	uri := pathToFileURI(filepath.Join(root, "Global.asa"))
	source := `<script runat="server" language="VBScript">
Sub Application_OnStart()
End Sub
Dim lastError
Set lastError = Server.GetLastError()
Response.
Server.
lastError.
Response.Buffer = True
Response.Write lastError.Description
</script>`
	client.request("initialize", map[string]any{
		"processId":    nil,
		"rootUri":      pathToFileURI(root),
		"capabilities": map[string]any{},
	})
	openClassicASPDocument(t, client, uri, source)

	responseCompletions := completionLabels(client.request("textDocument/completion", map[string]any{
		"textDocument": map[string]any{"uri": uri},
		"position":     positionAt(source, strings.Index(source, "Response.")+len("Response.")),
	}).Result)
	if !responseCompletions.contains("Buffer") {
		t.Fatalf("Response completions missing Buffer: %#v", responseCompletions)
	}
	serverCompletions := completionLabels(client.request("textDocument/completion", map[string]any{
		"textDocument": map[string]any{"uri": uri},
		"position":     positionAt(source, strings.Index(source, "Server.")+len("Server.")),
	}).Result)
	if !serverCompletions.contains("Execute") {
		t.Fatalf("Server completions missing Execute: %#v", serverCompletions)
	}
	errorCompletions := completionLabels(client.request("textDocument/completion", map[string]any{
		"textDocument": map[string]any{"uri": uri},
		"position":     positionAt(source, strings.Index(source, "lastError.")+len("lastError.")),
	}).Result)
	if !errorCompletions.contains("Description") {
		t.Fatalf("ASPError completions missing Description: %#v", errorCompletions)
	}
	topLevelCompletions := completionLabels(client.request("textDocument/completion", map[string]any{
		"textDocument": map[string]any{"uri": uri},
		"position":     positionAt(source, strings.Index(source, "Sub Application_OnStart")),
	}).Result)
	if !topLevelCompletions.contains("Application_OnStart") || topLevelCompletions.contains("WScript") {
		t.Fatalf("top-level ASP completions mismatch: %#v", topLevelCompletions)
	}

	assertRequestContains(t, client, "textDocument/hover", map[string]any{
		"textDocument": map[string]any{"uri": uri},
		"position":     positionAt(source, strings.Index(source, "Buffer")),
	}, "```vbscript\\nproperty Response.Buffer As Boolean\\n```")
	assertRequestContains(t, client, "textDocument/hover", map[string]any{
		"textDocument": map[string]any{"uri": uri},
		"position":     positionAt(source, strings.LastIndex(source, "Description")),
	}, "```vbscript\\nproperty ASPError.Description As String\\n```")
	assertRequestContains(t, client, "textDocument/hover", map[string]any{
		"textDocument": map[string]any{"uri": uri},
		"position":     positionAt(source, strings.Index(source, "Application_OnStart")),
	}, "```vbscript\\nSub Application_OnStart()\\n```")
	assertRequestContains(t, client, "textDocument/hover", map[string]any{
		"textDocument": map[string]any{"uri": uri},
		"position":     positionAt(source, strings.LastIndex(source, "lastError")),
	}, vbscriptHoverCodeBlockJSON("(global) Dim lastError As ASPError"))
}

func TestStdioParityCoversCOMAndADOCatalogCompletionsAndReturnTypes(t *testing.T) {
	client := startStdioTestClient(t)
	defer client.close()

	root := t.TempDir()
	uri := pathToFileURI(filepath.Join(root, "go-vbscript-com-ado.asp"))
	source := `<%
Dim fso, file, dict, stream, rs, command, rows, textRows, parameter
Set fso = Server.CreateObject("Scripting.FileSystemObject")
Set file = fso.GetFile("default.asp")
Set dict = CreateObject("Scripting.Dictionary")
Set stream = Server.CreateObject("ADODB.Stream")
Set rs = Server.CreateObject("ADODB.Recordset")
Set command = Server.CreateObject("ADODB.Command")
rows = rs.GetRows()
textRows = rs.GetString()
Set parameter = command.CreateParameter("id", adInteger)
fso.
file.
dict.
stream.
rs.
command.
%>`
	client.request("initialize", map[string]any{
		"processId":    nil,
		"rootUri":      pathToFileURI(root),
		"capabilities": map[string]any{},
	})
	openClassicASPDocument(t, client, uri, source)

	for _, testCase := range []struct {
		trigger string
		label   string
	}{
		{"fso.", "OpenTextFile"},
		{"file.", "OpenAsTextStream"},
		{"dict.", "Exists"},
		{"stream.", "ReadText"},
		{"rs.", "GetRows"},
		{"command.", "CreateParameter"},
	} {
		labels := completionLabels(client.request("textDocument/completion", map[string]any{
			"textDocument": map[string]any{"uri": uri},
			"position":     positionAt(source, strings.Index(source, testCase.trigger)+len(testCase.trigger)),
		}).Result)
		if !labels.contains(testCase.label) {
			t.Fatalf("%s completions missing %s: %#v", testCase.trigger, testCase.label, labels)
		}
	}
	for _, expected := range []string{
		vbscriptHoverCodeBlockJSON("(global) Dim file As Scripting.File"),
		vbscriptHoverCodeBlockJSON("(global) Dim rows As Array"),
		vbscriptHoverCodeBlockJSON("(global) Dim textRows As String"),
		vbscriptHoverCodeBlockJSON("(global) Dim parameter As ADODB.Parameter"),
	} {
		assertRequestContains(t, client, "textDocument/hover", map[string]any{
			"textDocument": map[string]any{"uri": uri},
			"position":     positionAt(source, strings.LastIndex(source, hoverIdentifierFromVBScriptCodeBlockExpectation(expected))),
		}, expected)
	}
	assertRequestContains(t, client, "textDocument/hover", map[string]any{
		"textDocument": map[string]any{"uri": uri},
		"position":     positionAt(source, strings.Index(source, "adInteger")),
	}, "```vbscript\\nConst adInteger As Number\\n```")
}

func TestStdioParityLocalizesASPAndADOMemberDocumentationAndParameterHelp(t *testing.T) {
	client := startStdioTestClient(t)
	defer client.close()

	root := t.TempDir()
	uri := pathToFileURI(filepath.Join(root, "go-vbscript-member-docs.asp"))
	source := `<%
Dim rs, rows, fieldType
Set rs = Server.CreateObject("ADODB.Recordset")
Response.Buffer = True
Server.Execute("next.asp")
rows = rs.GetRows(10, 0)
fieldType = adInteger
Response.
Server.
rs.
%>`
	client.request("initialize", map[string]any{
		"processId":    nil,
		"rootUri":      pathToFileURI(root),
		"capabilities": map[string]any{},
	})
	notifyConfiguration(t, client, map[string]any{"aspLsp": map[string]any{"locale": "ja"}})
	openClassicASPDocument(t, client, uri, source)

	bufferHover := mustJSONText(t, client.request("textDocument/hover", map[string]any{
		"textDocument": map[string]any{"uri": uri},
		"position":     positionAt(source, strings.Index(source, "Buffer")),
	}).Result)
	expectedBufferHover := "```vbscript\nproperty Response.Buffer As Boolean\n```\n\nASP の page output を buffer してから client へ送るかを制御します。\n\n**値**\n\nhtml tag より前、または response output より前に設定します。"
	if hoverMarkdownValue(t, client.request("textDocument/hover", map[string]any{
		"textDocument": map[string]any{"uri": uri},
		"position":     positionAt(source, strings.Index(source, "Buffer")),
	}).Result) != expectedBufferHover {
		t.Fatalf("localized Response.Buffer hover mismatch: %s", bufferHover)
	}

	responseItems := completionItems(client.request("textDocument/completion", map[string]any{
		"textDocument": map[string]any{"uri": uri},
		"position":     positionAt(source, strings.Index(source, "Response.")+len("Response.")),
	}).Result)
	bufferCompletion, ok := responseItems.find("Buffer")
	if !ok {
		t.Fatalf("Response completions missing Buffer: %#v", responseItems)
	}
	notifyConfiguration(t, client, map[string]any{"aspLsp": map[string]any{"locale": "en"}})
	bufferResolved := mustJSONText(t, client.request("completionItem/resolve", bufferCompletion).Result)
	if !strings.Contains(bufferResolved, "Controls whether ASP buffers page output") {
		t.Fatalf("English Response.Buffer completion docs missing: %s", bufferResolved)
	}

	serverItems := completionItems(client.request("textDocument/completion", map[string]any{
		"textDocument": map[string]any{"uri": uri},
		"position":     positionAt(source, strings.Index(source, "Server.")+len("Server.")),
	}).Result)
	executeCompletion, ok := serverItems.find("Execute")
	if !ok {
		t.Fatalf("Server completions missing Execute: %#v", serverItems)
	}
	notifyConfiguration(t, client, map[string]any{"aspLsp": map[string]any{"locale": "ja"}})
	executeResolved := mustJSONText(t, client.request("completionItem/resolve", executeCompletion).Result)
	if !strings.Contains(executeResolved, "別の ASP page を実行") {
		t.Fatalf("Japanese Server.Execute completion docs missing: %s", executeResolved)
	}

	notifyConfiguration(t, client, map[string]any{"aspLsp": map[string]any{"locale": "en"}})
	var executeSignature lsp.SignatureHelp
	mustDecodeResult(t, client.request("textDocument/signatureHelp", map[string]any{
		"textDocument": map[string]any{"uri": uri},
		"position":     positionAt(source, strings.Index(source, `Server.Execute("next.asp"`)+len("Server.Execute(")),
	}).Result, &executeSignature)
	if len(executeSignature.Signatures) == 0 ||
		executeSignature.Signatures[0].Documentation != "Runs another ASP page and returns to the current page after it finishes." ||
		len(executeSignature.Signatures[0].Parameters) == 0 ||
		executeSignature.Signatures[0].Parameters[0].Documentation != "Relative or absolute path of the ASP page to execute." {
		t.Fatalf("Server.Execute signature help mismatch: %#v", executeSignature)
	}

	notifyConfiguration(t, client, map[string]any{"aspLsp": map[string]any{"locale": "ja"}})
	var getRowsSignature lsp.SignatureHelp
	mustDecodeResult(t, client.request("textDocument/signatureHelp", map[string]any{
		"textDocument": map[string]any{"uri": uri},
		"position":     positionAt(source, strings.Index(source, "rs.GetRows(10,")+len("rs.GetRows(10,")),
	}).Result, &getRowsSignature)
	if len(getRowsSignature.Signatures) == 0 ||
		!strings.Contains(signatureDocumentationText(getRowsSignature.Signatures[0].Documentation), "2 次元 array") {
		t.Fatalf("Recordset.GetRows signature help docs mismatch: %#v", getRowsSignature)
	}
	expectedGetRowsParameterDocs := []string{
		"取得する records 数です。省略すると Recordset の残りを取得します。",
		"copy を開始する record number または bookmark です。",
		"含める field name/number、または field names/numbers の array です。",
	}
	for index, expected := range expectedGetRowsParameterDocs {
		if index >= len(getRowsSignature.Signatures[0].Parameters) || getRowsSignature.Signatures[0].Parameters[index].Documentation != expected {
			t.Fatalf("Recordset.GetRows parameter %d docs mismatch: %#v", index, getRowsSignature.Signatures[0].Parameters)
		}
	}

	adIntegerHover := hoverMarkdownValue(t, client.request("textDocument/hover", map[string]any{
		"textDocument": map[string]any{"uri": uri},
		"position":     positionAt(source, strings.Index(source, "adInteger")),
	}).Result)
	expectedAdIntegerHover := "```vbscript\nConst adInteger As Number\n```\n\nADO data type constant for a 32-bit signed integer value.\n\n**値**\n\n32-bit signed integer value。"
	if adIntegerHover != expectedAdIntegerHover {
		t.Fatalf("localized adInteger hover mismatch:\nwant: %s\n got: %s", expectedAdIntegerHover, adIntegerHover)
	}
	notifyConfiguration(t, client, map[string]any{"aspLsp": map[string]any{"locale": "en"}})
	topLevelItems := completionItems(client.request("textDocument/completion", map[string]any{
		"textDocument": map[string]any{"uri": uri},
		"position":     positionAt(source, strings.Index(source, "Dim rs")),
	}).Result)
	adIntegerCompletion, ok := topLevelItems.find("adInteger")
	if !ok {
		t.Fatalf("top-level completions missing adInteger: %#v", topLevelItems)
	}
	adIntegerResolved := mustJSONText(t, client.request("completionItem/resolve", adIntegerCompletion).Result)
	if !strings.Contains(adIntegerResolved, "ADO data type constant for a 32-bit signed integer value.") {
		t.Fatalf("English adInteger completion docs missing: %s", adIntegerResolved)
	}
}

func TestStdioParityTreatsRegExpAsTypedBuiltInObject(t *testing.T) {
	client := startStdioTestClient(t)
	defer client.close()

	root := t.TempDir()
	uri := pathToFileURI(filepath.Join(root, "go-vbscript-regexp.asp"))
	source := `<%
Option Explicit
Dim re, created, matches, firstMatch, subItems, subText, replaced, found
Set re = New RegExp
Set created = CreateObject("VBScript.RegExp")
re.Pattern = "(\w+)-(\d+)"
re.Global = True
re.IgnoreCase = True
re.MultiLine = True
Set matches = re.Execute("abc-123")
Set firstMatch = matches.Item(0)
Set subItems = firstMatch.SubMatches
subText = subItems.Item(0)
replaced = re.Replace("abc-123", "$1")
found = created.Test("abc-123")
re.
created.
matches.
firstMatch.
%>`
	client.request("initialize", map[string]any{
		"processId":    nil,
		"rootUri":      pathToFileURI(root),
		"capabilities": map[string]any{},
	})
	diagnostics := openClassicASPDocumentWithDiagnostics(t, client, uri, source)
	if serialized := mustJSONText(t, diagnosticsFromSource(t, diagnostics, "asp-lsp-vbscript")); serialized != "[]" && serialized != "null" {
		t.Fatalf("RegExp built-ins produced undeclared diagnostics: %s", serialized)
	}

	for _, testCase := range []struct {
		trigger string
		labels  []string
	}{
		{"re.", []string{"Pattern", "Global", "IgnoreCase", "MultiLine", "Execute"}},
		{"created.", []string{"Test"}},
		{"matches.", []string{"Count", "Item"}},
		{"firstMatch.", []string{"FirstIndex", "Length", "Value", "SubMatches"}},
	} {
		labels := completionLabels(client.request("textDocument/completion", map[string]any{
			"textDocument": map[string]any{"uri": uri},
			"position":     positionAt(source, strings.Index(source, testCase.trigger)+len(testCase.trigger)),
		}).Result)
		for _, label := range testCase.labels {
			if !labels.contains(label) {
				t.Fatalf("%s completions missing %s: %#v", testCase.trigger, label, labels)
			}
		}
	}
	for _, expected := range []string{
		vbscriptHoverCodeBlockJSON("(global) Dim re As RegExp"),
		vbscriptHoverCodeBlockJSON("(global) Dim created As RegExp"),
		vbscriptHoverCodeBlockJSON("(global) Dim matches As Matches"),
		vbscriptHoverCodeBlockJSON("(global) Dim firstMatch As Match"),
		vbscriptHoverCodeBlockJSON("(global) Dim subItems As SubMatches"),
		vbscriptHoverCodeBlockJSON("(global) Dim replaced As String"),
		vbscriptHoverCodeBlockJSON("(global) Dim found As Boolean"),
	} {
		identifier := hoverIdentifierFromVBScriptCodeBlockExpectation(expected)
		offset := strings.LastIndex(source, identifier+".")
		if offset < 0 {
			offset = strings.LastIndex(source, identifier+" =")
		}
		if offset < 0 {
			offset = strings.LastIndex(source, identifier)
		}
		assertRequestContains(t, client, "textDocument/hover", map[string]any{
			"textDocument": map[string]any{"uri": uri},
			"position":     positionAt(source, offset),
		}, expected)
	}

	topLevel := completionItems(client.request("textDocument/completion", map[string]any{
		"textDocument": map[string]any{"uri": uri},
		"position":     positionAt(source, strings.Index(source, "Dim re")),
	}).Result)
	regexpItem, ok := topLevel.find("RegExp")
	if !ok || regexpItem.Kind != int(lsp.CompletionItemKindClass) {
		t.Fatalf("RegExp top-level completion mismatch: %#v", regexpItem)
	}
	notifyConfiguration(t, client, map[string]any{"aspLsp": map[string]any{"locale": "ja"}})
	resolved := client.request("completionItem/resolve", regexpItem)
	if serialized := mustJSONText(t, resolved.Result); !strings.Contains(serialized, "Pattern を使って") {
		t.Fatalf("localized RegExp completion documentation missing: %s", serialized)
	}
}

func hasTokenMatchingTextAndModifiers(source string, tokens []decodedSemanticToken, text string, tokenType int, modifiers int) bool {
	for _, token := range tokens {
		if token.TokenType != tokenType || token.TokenModifiers != modifiers {
			continue
		}
		offset := offsetAtPosition(source, token.Line, token.Character)
		if offset < 0 || offset+token.Length > len(source) {
			continue
		}
		if source[offset:offset+token.Length] == text {
			return true
		}
	}
	return false
}

func hasTokenAtOffset(source string, tokens []decodedSemanticToken, offset int, tokenType int, modifiers int) bool {
	position := positionAt(source, offset)
	return hasSemanticToken(tokens, position["line"], position["character"], tokenType, modifiers)
}
