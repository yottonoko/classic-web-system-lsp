package lspserver

import (
	"io"
	"strings"
	"testing"

	"github.com/yottonoko/classic-web-system-lsp/internal/core"
	"github.com/yottonoko/classic-web-system-lsp/internal/lsp"
	"github.com/yottonoko/classic-web-system-lsp/internal/vbscript"
)

func TestServerObjectSymbolsUseScannerAttributeRanges(t *testing.T) {
	source := `<object data="repoObject" name="fallbackObject" id='repoObject' RUNAT=server progid="Repository.Type"></object>
<object name=legacyObject runat='SERVER' classid='CLSID:123'></object>
<object id="clientObject" runat="client" progid="Ignored.Type"></object>
<object id="invalid-name" runat="server" progid="Ignored.Type"></object>`
	parsed := core.ParseDocument("file:///site/object-symbols.inc", source, core.Settings{DefaultLanguage: "VBScript"})
	symbols := serverObjectSymbols(parsed)
	if len(symbols) != 2 {
		t.Fatalf("server object symbols = %#v, want 2", symbols)
	}
	first := symbols[0].Declaration
	if first.Name != "repoObject" || first.TypeName != "Repository.Type" {
		t.Fatalf("first server object = %#v", first)
	}
	if got := inferVBDeclarationType(parsed, first); got != "Repository.Type" {
		t.Fatalf("server object inferred type = %q", got)
	}
	doc := core.NewTextDocument(parsed.URI, "classic-asp", 0, source)
	if got := source[doc.OffsetAt(first.Range.Start):doc.OffsetAt(first.Range.End)]; got != "repoObject" {
		t.Fatalf("id range text = %q, want repoObject", got)
	}
	idStart := strings.Index(source, "id='repoObject'") + len("id='")
	if first.Start != idStart {
		t.Fatalf("id range start = %d, want %d", first.Start, idStart)
	}
	second := symbols[1].Declaration
	if second.Name != "legacyObject" || second.TypeName != "CLSID:123" {
		t.Fatalf("name fallback server object = %#v", second)
	}
}

func TestImplicitVBDeclarationsAreScopeAware(t *testing.T) {
	source := `<%
Response.Write globalRead
Sub First()
  Response.Write sharedLocal
  Response.Write localRead
End Sub
Sub Second()
  Dim localRead
  Response.Write sharedLocal
  Response.Write localRead
  Call MissingProcedure
  value = MissingFunction()
End Sub
%>`
	parsed := core.ParseDocument("file:///site/implicit-scopes.asp", source, core.Settings{DefaultLanguage: "VBScript"})
	declarations := implicitVBDeclarations(parsed)
	byKey := map[string]vbUsageDeclaration{}
	for _, declaration := range declarations {
		byKey[implicitDeclarationScopeKey(declaration.Local, declaration.Scope, declaration.Name)] = declaration
	}
	if declaration, ok := byKey[implicitDeclarationScopeKey(false, "", "globalRead")]; !ok || declaration.Local {
		t.Fatalf("global read declaration = %#v", declaration)
	}
	for _, scope := range []string{"first", "second"} {
		declaration, ok := byKey[implicitDeclarationScopeKey(true, scope, "sharedLocal")]
		if !ok || !declaration.Local || !strings.EqualFold(declaration.Scope, scope) {
			t.Fatalf("%s sharedLocal declaration = %#v", scope, declaration)
		}
	}
	if _, ok := byKey[implicitDeclarationScopeKey(true, "first", "localRead")]; !ok {
		t.Fatal("First.localRead implicit declaration missing")
	}
	if _, ok := byKey[implicitDeclarationScopeKey(true, "second", "localRead")]; ok {
		t.Fatal("explicit Second.localRead produced an implicit declaration")
	}
	for _, unexpected := range []string{"MissingProcedure", "MissingFunction"} {
		for _, declaration := range declarations {
			if strings.EqualFold(declaration.Name, unexpected) {
				t.Fatalf("call target %s became implicit: %#v", unexpected, declaration)
			}
		}
	}

	explicit := core.ParseDocument("file:///site/implicit-option-explicit.asp", "<% Option Explicit : Response.Write missingValue %>", core.Settings{DefaultLanguage: "VBScript"})
	if declarations := implicitVBDeclarations(explicit); len(declarations) != 0 {
		t.Fatalf("Option Explicit declarations = %#v, want none", declarations)
	}
}

func TestImplicitGlobalDiagnosticsWarnForAssignmentsAndRespectExternalGlobals(t *testing.T) {
	source := `<%
Function Build()
  assignedValue = 1
  Set objectValue = CreateObject("Scripting.Dictionary")
  Response.Write localReadOnly
End Function
externalValue = 2
%>`
	parsed := core.ParseDocument("file:///site/implicit-diagnostics.asp", source, core.Settings{DefaultLanguage: "VBScript"})
	diagnostics := implicitGlobalVBScriptDiagnostics(parsed, "ja", map[string]struct{}{"externalvalue": {}})
	text := mustJSONForTest(t, diagnostics)
	for _, expected := range []string{"assignedValue", "objectValue", "暗黙の global 変数"} {
		if !strings.Contains(text, expected) {
			t.Fatalf("implicit global diagnostics missing %q: %s", expected, text)
		}
	}
	for _, unexpected := range []string{"localReadOnly", "externalValue"} {
		if strings.Contains(text, unexpected) {
			t.Fatalf("implicit global diagnostics unexpectedly contain %q: %s", unexpected, text)
		}
	}

	explicit := core.ParseDocument("file:///site/implicit-explicit.asp", "<% Option Explicit : assignedValue = 1 %>", core.Settings{DefaultLanguage: "VBScript"})
	if got := implicitGlobalVBScriptDiagnostics(explicit, "en", nil); len(got) != 0 {
		t.Fatalf("Option Explicit implicit global diagnostics = %#v, want none", got)
	}
}

func TestServerObjectsSatisfyOptionExplicitDeclarations(t *testing.T) {
	source := `<object id="repoObject" runat="server" progid="Repository.Type"></object>
<div id="normalID"></div>
<% Option Explicit : Response.Write repoObject : Response.Write normalID : Response.Write includedObject %>`
	parsed := core.ParseDocument("file:///site/object-diagnostics.asp", source, core.Settings{DefaultLanguage: "VBScript"})
	diagnostics := vbscriptUsageDiagnosticsWithGlobals(parsed, "en", map[string]struct{}{"includedobject": {}})
	text := mustJSONForTest(t, diagnostics)
	if strings.Contains(text, `repoObject' is not declared`) || strings.Contains(text, `includedObject' is not declared`) {
		t.Fatalf("server objects were reported undeclared: %s", text)
	}
	if !strings.Contains(text, `normalID' is not declared`) {
		t.Fatalf("normal HTML id incorrectly satisfied Option Explicit: %s", text)
	}
}

func TestServerObjectLanguageFeaturesDoNotCaptureNormalHTMLIDs(t *testing.T) {
	server := New(strings.NewReader(""), io.Discard, io.Discard)
	uri := "file:///site/object-features.asp"
	source := `<div id="normalID"></div>
<object id="repoObject" runat="server" progid="Repository.Type"></object>
<% Response.Write repoObject
Sub Shadow()
  Dim repoObject
  Response.Write repoObject
End Sub
%>`
	doc := core.NewTextDocument(uri, "classic-asp", 1, source)
	server.documents[uri] = doc
	parsed := server.parseTextDocument(doc, "VBScript")

	positionAt := func(needle string, delta int) lsp.Position {
		return doc.PositionAt(strings.Index(source, needle) + delta)
	}
	normalPosition := positionAt("normalID", 2)
	if _, _, ok := server.serverObjectTarget(parsed, normalPosition); ok {
		t.Fatal("normal HTML id was treated as a server object")
	}
	objectPosition := positionAt("repoObject", 2)
	definition := server.serverObjectDefinition(parsed, objectPosition)
	if len(definition) != 1 || definition[0].Range.Start != doc.PositionAt(strings.Index(source, "repoObject")) {
		t.Fatalf("object id definition = %#v", definition)
	}
	usagePosition := doc.PositionAt(strings.Index(source, "Response.Write repoObject") + len("Response.Write re"))
	definition = server.serverObjectDefinition(parsed, usagePosition)
	if len(definition) != 1 || definition[0].Range.Start != doc.PositionAt(strings.Index(source, "repoObject")) {
		t.Fatalf("object usage definition = %#v", definition)
	}
	references, ok := server.serverObjectReferences(t.Context(), parsed, objectPosition, true)
	if !ok || len(references) != 2 {
		t.Fatalf("object references = %#v, ok=%v", references, ok)
	}
	shadowPosition := doc.PositionAt(strings.LastIndex(source, "repoObject"))
	if definition := server.serverObjectDefinition(parsed, shadowPosition); len(definition) != 0 {
		t.Fatalf("local shadow routed to server object: %#v", definition)
	}
	hover := server.serverObjectHover(parsed, objectPosition)
	if hover == nil || !strings.Contains(hover.Contents.(lsp.MarkupContent).Value, "Repository.Type") || !strings.Contains(hover.Contents.(lsp.MarkupContent).Value, "References: 1") {
		t.Fatalf("object hover = %#v", hover)
	}
	edit, ok := server.serverObjectRenameEdit(parsed, objectPosition, "renamedObject")
	if !ok {
		t.Fatal("object rename was not handled")
	}
	changes := edit["changes"].(map[string][]lsp.TextEdit)
	if len(changes[uri]) != 2 {
		t.Fatalf("object rename edits = %#v", changes)
	}
	completionOffset := strings.Index(source, "Response.Write repoObject") + len("Response.Write repo")
	completions := server.vbscriptSymbolCompletions(parsed, completionOffset)
	objectCompletion := completionItemByLabelAndDetail(completions, "repoObject", "Server OBJECT · Repository.Type")
	if objectCompletion == nil || objectCompletion.Detail != "Server OBJECT · Repository.Type" {
		t.Fatalf("same-document object completion = %#v", objectCompletion)
	}
	shadowCompletions := server.vbscriptSymbolCompletions(parsed, strings.LastIndex(source, "repoObject"))
	if completion := completionItemByLabelAndDetail(shadowCompletions, "repoObject", "Server OBJECT · Repository.Type"); completion != nil {
		t.Fatalf("shadowed object leaked into local completion: %#v", completion)
	}
	server.settings.CodeLensReferenceGlobals = true
	declarations := server.vbscriptReferenceCodeLensDeclarations(parsed)
	if declarationByName(declarations, "repoObject") == nil {
		t.Fatalf("object CodeLens declaration missing: %#v", declarations)
	}
}

func completionItemByLabelAndDetail(items []lsp.CompletionItem, label, detail string) *lsp.CompletionItem {
	for index := range items {
		if items[index].Label == label && items[index].Detail == detail {
			return &items[index]
		}
	}
	return nil
}

func TestVBScriptCompletionSignatureLookupUsesDeclarationRange(t *testing.T) {
	source := `<%
Class Widget
  Public Function Method(ByVal value)
  End Function
End Class
Function Method(ByVal value)
End Function
%>`
	parsed := core.ParseDocument("file:///site/completion-signature-lookup.asp", source, core.Settings{})
	lookup := vbscriptCompletionSignatureLookup(parsed)
	var methods []vbscript.Signature
	for _, signature := range graphSignatures(parsed) {
		if strings.EqualFold(signature.Name, "Method") {
			methods = append(methods, signature)
		}
	}
	if len(methods) != 2 {
		t.Fatalf("Method signatures = %#v, want both class and root declarations", methods)
	}
	for _, expected := range methods {
		got, ok := vbscriptCompletionSignatureForSymbol(lookup, vbscript.Symbol{Name: expected.Name, Range: expected.NameRange})
		if !ok || got.NameRange != expected.NameRange || got.Label != expected.Label {
			t.Fatalf("signature for %q at %#v = %#v, want %#v", expected.Name, expected.NameRange, got, expected)
		}
	}
}
