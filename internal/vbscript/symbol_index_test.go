package vbscript

import (
	"reflect"
	"testing"

	"github.com/yottonoko/classic-web-system-lsp/internal/core"
	"github.com/yottonoko/classic-web-system-lsp/internal/lsp"
)

func TestSymbolIndexSharesReferenceShardPostings(t *testing.T) {
	parsed := core.ParseDocument("file:///tmp/symbol-index-shared.asp", "<% Dim value : value = value + 1 %>", core.Settings{DefaultLanguage: "VBScript"})
	shard := BuildReferenceShard(parsed)
	index := BuildSymbolIndex(parsed)
	if reflect.ValueOf(index.Occurrences).Pointer() != reflect.ValueOf(shard.Postings).Pointer() {
		t.Fatal("symbol index copied the reference posting map")
	}
	postings := shard.PostingsFor("value")
	occurrences := index.Occurrences["value"]
	if len(postings) == 0 || len(occurrences) == 0 || &postings[0] != &occurrences[0] {
		t.Fatal("symbol index copied the reference posting slice")
	}
}

func TestSymbolIndexKeepsQualifiedMembersSeparateFromGlobals(t *testing.T) {
	const source = `<%
Function ABC()
End Function
Dim A
A.ABC()
value = A.ABC
ABC()
%>`
	parsed := core.ParseDocument("file:///tmp/qualified-members.asp", source, core.Settings{DefaultLanguage: "VBScript"})
	memberPosition := positionOf(t, source, "A.ABC()", len("A.A"))
	globalPosition := positionOf(t, source, "ABC()\n%>", 1)

	if definitions := Definition(parsed, memberPosition); len(definitions) != 0 {
		t.Fatalf("qualified member resolved to global definition: %#v", definitions)
	}
	memberReferences := References(parsed, memberPosition, true)
	if len(memberReferences) != 2 || !hasLocationAtLine(memberReferences, 4) || !hasLocationAtLine(memberReferences, 5) {
		t.Fatalf("qualified member references mismatch: %#v", memberReferences)
	}
	globalReferences := References(parsed, globalPosition, true)
	if len(globalReferences) != 2 || !hasLocationAtLine(globalReferences, 1) || !hasLocationAtLine(globalReferences, 6) {
		t.Fatalf("global references included qualified members: %#v", globalReferences)
	}
	if help := SignatureHelp(parsed, positionOf(t, source, "A.ABC()", len("A.ABC("))); help != nil {
		t.Fatalf("qualified member reused global signature help: %#v", help)
	}
}

func TestGlobalReferencesIncludeCallsFromClassesWithoutShadowingMembers(t *testing.T) {
	const source = `<%
Function Utility()
End Function
Class Container
  Public Sub Run()
    Utility()
  End Sub
End Class
%>`
	parsed := core.ParseDocument("file:///tmp/global-call-from-class.asp", source, core.Settings{DefaultLanguage: "VBScript"})
	position := positionOf(t, source, "Function Utility", len("Function U"))

	references := References(parsed, position, true)
	if len(references) != 2 || !hasLocationAtLine(references, 1) || !hasLocationAtLine(references, 5) {
		t.Fatalf("global references from non-shadowing class mismatch: %#v", references)
	}
	callRanges := UnqualifiedCallRanges(parsed, "Utility")
	if len(callRanges) != 1 || callRanges[0].Start.Line != 5 {
		t.Fatalf("global call ranges from non-shadowing class mismatch: %#v", callRanges)
	}
}

func TestGlobalReferencesIgnoreUnrelatedLocalShadowInAnotherClassProcedure(t *testing.T) {
	const source = `<%
Function Utility()
End Function
Class Container
  Public Sub Run()
    Utility()
  End Sub
  Public Sub Configure()
    Dim Utility
    Utility = 1
  End Sub
End Class
%>`
	parsed := core.ParseDocument("file:///tmp/global-call-with-local-shadow.asp", source, core.Settings{DefaultLanguage: "VBScript"})
	position := positionOf(t, source, "Function Utility", len("Function U"))

	references := References(parsed, position, true)
	if len(references) != 2 || !hasLocationAtLine(references, 1) || !hasLocationAtLine(references, 5) {
		t.Fatalf("global references with unrelated local shadow mismatch: %#v", references)
	}
	callRanges := UnqualifiedCallRanges(parsed, "Utility")
	if len(callRanges) != 1 || callRanges[0].Start.Line != 5 {
		t.Fatalf("global call ranges with unrelated local shadow mismatch: %#v", callRanges)
	}
}

func TestSymbolIndexNavigationAndRename(t *testing.T) {
	const uri = "file:///tmp/symbol-index.asp"
	source := `<%
Function BuildName(ByVal firstName, lastName)
  BuildName = firstName & " " & lastName
  ' BuildName in a comment
  Response.Write "BuildName in a string"
End Function
Response.Write BuildName("Ada", "Lovelace")
%>`
	parsed := core.ParseDocument(uri, source, core.Settings{DefaultLanguage: "VBScript"})
	position := positionOf(t, source, `BuildName("Ada"`, 2)

	definition := Definition(parsed, position)
	if len(definition) != 1 {
		t.Fatalf("expected one definition, got %#v", definition)
	}
	if definition[0].Range.Start.Line != 1 {
		t.Fatalf("expected definition on line 1, got %#v", definition[0].Range)
	}

	references := References(parsed, position, true)
	if len(references) != 3 {
		t.Fatalf("expected declaration, assignment, and call references, got %#v", references)
	}
	usageReferences := References(parsed, position, false)
	if len(usageReferences) != 2 {
		t.Fatalf("expected assignment and call references, got %#v", usageReferences)
	}

	renameRange := RenameRange(parsed, position)
	if renameRange == nil || renameRange.Start.Line != 6 {
		t.Fatalf("expected rename range on call line, got %#v", renameRange)
	}

	edit := RenameEdit(parsed, position, "FormatName")
	changes, ok := edit["changes"].(map[string]any)
	if !ok {
		t.Fatalf("expected workspace edit changes, got %#v", edit)
	}
	edits, ok := changes[uri].([]lsp.TextEdit)
	if !ok {
		t.Fatalf("expected typed text edits for %s, got %#v", uri, changes[uri])
	}
	if len(edits) != 3 {
		t.Fatalf("expected 3 rename edits excluding comments and strings, got %#v", edits)
	}
}

func TestReferencesAndRenameKeepProcedureLocalNamesScoped(t *testing.T) {
	const uri = "file:///tmp/procedure-local-references.asp"
	const source = `<%
Dim value
value = 1

Sub First()
  Dim value
  value = 2
  Response.Write value
End Sub

Sub Second()
  Dim value
  value = 3
  Response.Write value
End Sub

Response.Write value
%>`
	parsed := core.ParseDocument(uri, source, core.Settings{DefaultLanguage: "VBScript"})

	localPosition := positionOf(t, source, "Response.Write value\nEnd Sub", len("Response.Write va"))
	localTarget, ok := BuildReferenceShard(parsed).ReferenceTargetAt("value", localPosition)
	if !ok || !localTarget.ProcedureLocal || localTarget.Scope != "First" || localTarget.ScopeKind != "sub" {
		t.Fatalf("First.value target identity mismatch: %#v, found=%t", localTarget, ok)
	}
	localReferences := References(parsed, localPosition, true)
	if len(localReferences) != 3 || !hasLocationAtLine(localReferences, 5) || !hasLocationAtLine(localReferences, 6) || !hasLocationAtLine(localReferences, 7) {
		t.Fatalf("First.value references escaped their procedure: %#v", localReferences)
	}
	localChanges := RenameEdit(parsed, localPosition, "renamedValue")["changes"].(map[string]any)
	localEdits, ok := localChanges[uri].([]lsp.TextEdit)
	if !ok || len(localEdits) != 3 {
		t.Fatalf("First.value rename escaped its procedure: %#v", localChanges)
	}
	for _, edit := range localEdits {
		if edit.Range.Start.Line < 5 || edit.Range.Start.Line > 7 {
			t.Fatalf("First.value rename included line %d outside its procedure: %#v", edit.Range.Start.Line, localEdits)
		}
	}

	globalPosition := positionOf(t, source, "Response.Write value\n%>", len("Response.Write va"))
	globalTarget, ok := BuildReferenceShard(parsed).ReferenceTargetAt("value", globalPosition)
	if !ok || globalTarget.ProcedureLocal || globalTarget.Scope != "" {
		t.Fatalf("global value target identity mismatch: %#v, found=%t", globalTarget, ok)
	}
	globalReferences := References(parsed, globalPosition, true)
	if len(globalReferences) != 3 || !hasLocationAtLine(globalReferences, 1) || !hasLocationAtLine(globalReferences, 2) || !hasLocationAtLine(globalReferences, 16) {
		t.Fatalf("global value references included procedure locals: %#v", globalReferences)
	}
}

func TestReferencesAndRenameKeepProcedureParametersScoped(t *testing.T) {
	const uri = "file:///tmp/procedure-parameter-references.asp"
	const source = `<%
Dim value
Response.Write value

Sub First(ByVal value)
  value = value + 1
End Sub

Sub Second(ByRef value)
  value = value + 2
End Sub
%>`
	parsed := core.ParseDocument(uri, source, core.Settings{DefaultLanguage: "VBScript"})

	firstPosition := positionOf(t, source, "value + 1", 1)
	firstTarget, ok := BuildReferenceShard(parsed).ReferenceTargetAt("value", firstPosition)
	if !ok || !firstTarget.ProcedureLocal || firstTarget.Scope != "First" || firstTarget.ScopeKind != "sub" {
		t.Fatalf("First.value parameter target identity mismatch: %#v, found=%t", firstTarget, ok)
	}
	firstReferences := References(parsed, firstPosition, true)
	if len(firstReferences) != 3 || !hasLocationAtLine(firstReferences, 4) || !hasLocationAtLine(firstReferences, 5) {
		t.Fatalf("First.value parameter references escaped their procedure: %#v", firstReferences)
	}
	firstChanges := RenameEdit(parsed, firstPosition, "renamedValue")["changes"].(map[string]any)
	firstEdits, ok := firstChanges[uri].([]lsp.TextEdit)
	if !ok || len(firstEdits) != 3 {
		t.Fatalf("First.value parameter rename escaped its procedure: %#v", firstChanges)
	}
	for _, edit := range firstEdits {
		if edit.Range.Start.Line < 4 || edit.Range.Start.Line > 5 {
			t.Fatalf("First.value parameter rename included line %d outside its procedure: %#v", edit.Range.Start.Line, firstEdits)
		}
	}

	globalPosition := positionOf(t, source, "Response.Write value", len("Response.Write va"))
	globalReferences := References(parsed, globalPosition, true)
	if len(globalReferences) != 2 || !hasLocationAtLine(globalReferences, 1) || !hasLocationAtLine(globalReferences, 2) {
		t.Fatalf("global value references included procedure parameters: %#v", globalReferences)
	}
}

func TestSymbolIndexNavigationForMultipleDimDeclarations(t *testing.T) {
	const uri = "file:///tmp/multiple-dim.asp"
	source := `<%
Dim first, second, third
Response.Write first
Response.Write second
Response.Write third
%>`
	parsed := core.ParseDocument(uri, source, core.Settings{DefaultLanguage: "VBScript"})

	for _, test := range []struct {
		name                 string
		usageLine            int
		declarationCharacter int
	}{
		{name: "first", usageLine: 2, declarationCharacter: len("Dim ")},
		{name: "second", usageLine: 3, declarationCharacter: len("Dim first, ")},
		{name: "third", usageLine: 4, declarationCharacter: len("Dim first, second, ")},
	} {
		t.Run(test.name, func(t *testing.T) {
			definition := Definition(parsed, lsp.Position{Line: test.usageLine, Character: len("Response.Write ")})
			if len(definition) != 1 {
				t.Fatalf("definition for %s = %#v, want one location", test.name, definition)
			}
			if definition[0].Range.Start.Line != 1 {
				t.Fatalf("definition for %s = %#v, want declaration line 1", test.name, definition[0].Range)
			}
			if definition[0].Range.Start.Character != test.declarationCharacter {
				t.Fatalf("definition for %s starts at character %d, want %d", test.name, definition[0].Range.Start.Character, test.declarationCharacter)
			}
		})
	}
}

func TestRenameEditRejectsInvalidIdentifier(t *testing.T) {
	const uri = "file:///tmp/invalid-rename.asp"
	source := `<%
Dim customerName
Response.Write customerName
%>`
	parsed := core.ParseDocument(uri, source, core.Settings{DefaultLanguage: "VBScript"})
	for _, newName := range []string{"not valid", "If", "cLaSs", "eNd"} {
		edit := RenameEdit(parsed, positionOf(t, source, "customerName", 1), newName)
		changes := edit["changes"].(map[string]any)
		if len(changes) != 0 {
			t.Fatalf("expected no changes for invalid identifier %q, got %#v", newName, changes)
		}
	}
}

func TestReferencesCanExcludeFunctionReturnAssignments(t *testing.T) {
	const uri = "file:///tmp/function-usage-references.asp"
	source := `<%
Function MakeCustomer()
  Set MakeCustomer = New Customer
End Function

Function Factorial(ByVal n)
  If n <= 1 Then
    Factorial = 1
  Else
    Factorial = n * Factorial(n - 1)
  End If
End Function

Set c = MakeCustomer()
%>`
	parsed := core.ParseDocument(uri, source, core.Settings{DefaultLanguage: "VBScript"})

	makeCustomerPosition := positionOf(t, source, "Set c = Make", len("Set c = Make"))
	allMakeCustomerReferences := References(parsed, makeCustomerPosition, true)
	if len(allMakeCustomerReferences) != 3 {
		t.Fatalf("MakeCustomer references including declaration = %d, want 3: %#v", len(allMakeCustomerReferences), allMakeCustomerReferences)
	}
	makeCustomerUsages := ReferencesWithOptions(parsed, makeCustomerPosition, ReferenceOptions{
		IncludeDeclaration:               false,
		IncludeFunctionReturnAssignments: false,
	})
	if len(makeCustomerUsages) != 1 || makeCustomerUsages[0].Range.Start.Line != 13 {
		t.Fatalf("MakeCustomer usage references mismatch: %#v", makeCustomerUsages)
	}

	factorialPosition := positionOf(t, source, "    Factorial = n * Fact", len("    Factorial = n * Fact"))
	factorialUsages := ReferencesWithOptions(parsed, factorialPosition, ReferenceOptions{
		IncludeDeclaration:               false,
		IncludeFunctionReturnAssignments: false,
	})
	if len(factorialUsages) != 1 {
		t.Fatalf("Factorial usage references = %d, want 1: %#v", len(factorialUsages), factorialUsages)
	}
	if factorialUsages[0].Range.Start.Line != 9 || factorialUsages[0].Range.Start.Character != len("    Factorial = n * ") {
		t.Fatalf("Factorial recursive usage range mismatch: %#v", factorialUsages[0].Range)
	}
}

func TestSymbolIndexSkipsVBScriptHexAndOctalLiteralFragments(t *testing.T) {
	source := `<%
hexValue = &HFF
lowerHex = &ha
octValue = &077
explicitOctal = &O10
zeroOctal = &0
%>`
	parsed := core.ParseDocument("file:///tmp/numeric-literals.asp", source, core.Settings{DefaultLanguage: "VBScript"})
	index := BuildSymbolIndex(parsed)

	for _, unexpected := range []string{"hff", "ha", "o10"} {
		if occurrences := index.Occurrences[unexpected]; len(occurrences) > 0 {
			t.Fatalf("numeric literal fragment %s should not be indexed: %#v", unexpected, occurrences)
		}
	}
	for _, expected := range []string{"hexvalue", "lowerhex", "octvalue", "explicitoctal", "zerooctal"} {
		if occurrences := index.Occurrences[expected]; len(occurrences) == 0 {
			t.Fatalf("expected assignment target %s to stay indexed: %#v", expected, index.Occurrences)
		}
	}
}

func TestSymbolIndexIncludesXMLDocumentationCrefReferences(t *testing.T) {
	const uri = "file:///tmp/xml-doc-cref.asp"
	source := `<%
Function BuildName(first)
End Function
''' <see cref="BuildName" />
Class Customer
  Public Name
End Class
''' <see cref="Customer.Name" />
Sub Save()
End Sub
%>`
	parsed := core.ParseDocument(uri, source, core.Settings{DefaultLanguage: "VBScript"})
	position := positionOf(t, source, `BuildName"`, len("Build"))

	definition := Definition(parsed, position)
	if len(definition) != 1 || definition[0].Range.Start.Line != 1 {
		t.Fatalf("expected cref definition on function line, got %#v", definition)
	}

	references := References(parsed, position, true)
	if len(references) != 2 {
		t.Fatalf("expected declaration and cref references, got %#v", references)
	}
	if !hasLocationAtLine(references, 1) || !hasLocationAtLine(references, 3) {
		t.Fatalf("expected declaration and cref reference lines, got %#v", references)
	}

	memberPosition := positionOf(t, source, `Customer.Name"`, len("Customer.Na"))
	memberReferences := References(parsed, memberPosition, true)
	if !hasLocationAtLine(memberReferences, 5) || !hasLocationAtLine(memberReferences, 7) {
		t.Fatalf("expected member declaration and dotted cref reference lines, got %#v", memberReferences)
	}
}

func TestSymbolIndexIncludesClassMemberDeclarations(t *testing.T) {
	const uri = "file:///tmp/class-members.asp"
	source := `<%
Class Customer
  Public Name
  Public Const Kind = "standard"
  Public Sub Save()
  End Sub
  Public Property Get DisplayName()
    DisplayName = Name
  End Property
End Class
%>`
	parsed := core.ParseDocument(uri, source, core.Settings{DefaultLanguage: "VBScript"})
	index := BuildSymbolIndex(parsed)
	expected := map[string]string{
		"customer":    "class",
		"name":        "variable",
		"kind":        "const",
		"save":        "sub",
		"displayname": "property",
	}
	for name, kind := range expected {
		symbol, ok := index.Declarations[name]
		if !ok {
			t.Fatalf("expected declaration %q, got %#v", name, index.Declarations)
		}
		if symbol.Kind != kind {
			t.Fatalf("expected %s kind %q, got %q", name, kind, symbol.Kind)
		}
	}
	for _, name := range []string{"public", "property", "get"} {
		if _, ok := index.Declarations[name]; ok {
			t.Fatalf("did not expect modifier token %q as declaration: %#v", name, index.Declarations[name])
		}
	}
}

func TestSymbolIndexIgnoresDeclarationsInsideStringsCommentsAndClientContent(t *testing.T) {
	const uri = "file:///tmp/ignored-content.asp"
	source := `<script>
const ignored = "Function ClientFake()";
// Dim ClientComment
</script>
<style>
.fake { content: "Class StyleFake"; }
</style>
<%
Dim realValue
message = "Sub StringFake()"
' Function CommentFake()
Rem Class RemFake
Reminder = 2
%>`
	parsed := core.ParseDocument(uri, source, core.Settings{DefaultLanguage: "VBScript"})
	index := BuildSymbolIndex(parsed)
	for _, absent := range []string{"clientfake", "clientcomment", "stylefake", "stringfake", "commentfake", "remfake"} {
		if declaration, ok := index.Declarations[absent]; ok {
			t.Fatalf("unexpected declaration %s: %#v", absent, declaration)
		}
	}
	if declaration, ok := index.Declarations["realvalue"]; !ok || declaration.Kind != "variable" {
		t.Fatalf("realValue declaration = %#v ok=%v", declaration, ok)
	}
	if _, ok := index.Declarations["reminder"]; ok {
		t.Fatalf("Reminder assignment should not be a declaration: %#v", index.Declarations["reminder"])
	}
	if occurrences := index.Occurrences["reminder"]; len(occurrences) != 1 {
		t.Fatalf("Reminder occurrence = %#v", occurrences)
	}
}

func TestSymbolIndexTreatsRemAsCommentOnlyAtStatementStart(t *testing.T) {
	const uri = "file:///tmp/rem-comments.asp"
	source := `<%
Rem leading comment
value = 1 : Rem trailing comment
rEm mixed case comment
Reminder = 2
value = Rem
%>`
	parsed := core.ParseDocument(uri, source, core.Settings{DefaultLanguage: "VBScript"})
	index := BuildSymbolIndex(parsed)

	for _, absent := range []string{"leading", "trailing", "comment", "mixed", "case"} {
		if occurrences := index.Occurrences[absent]; len(occurrences) != 0 {
			t.Fatalf("Rem comment text %q should not be indexed: %#v", absent, occurrences)
		}
	}
	if occurrences := index.Occurrences["reminder"]; len(occurrences) != 1 {
		t.Fatalf("Reminder should remain an identifier, got %#v", occurrences)
	}
	if occurrences := index.Occurrences["rem"]; len(occurrences) != 1 {
		t.Fatalf("expression Rem should remain an identifier occurrence, got %#v", occurrences)
	}
}

func TestSignatureHelpWorkspaceSymbolsAndSemanticTokens(t *testing.T) {
	const uri = "file:///tmp/signature-semantic.asp"
	source := `<%
Function BuildName(ByVal firstName, lastName)
  BuildName = firstName & " " & lastName
End Function
Response.Write BuildName("Ada", "Lovelace")
%>`
	parsed := core.ParseDocument(uri, source, core.Settings{DefaultLanguage: "VBScript"})

	signature := SignatureHelp(parsed, positionOf(t, source, `"Ada"`, 0))
	if signature == nil || len(signature.Signatures) != 1 {
		t.Fatalf("expected signature help, got %#v", signature)
	}
	if signature.Signatures[0].Label != "BuildName(ByVal firstName, ByRef lastName)" {
		t.Fatalf("unexpected signature label: %#v", signature.Signatures[0].Label)
	}

	symbols := WorkspaceSymbols(parsed, "Build")
	if len(symbols) != 1 || symbols[0].Name != "BuildName" {
		t.Fatalf("expected BuildName workspace symbol, got %#v", symbols)
	}

	tokens := decodeSemantic(SemanticTokens(parsed).Data)
	callPosition := positionOf(t, source, `BuildName("Ada"`, 0)
	if !hasSemanticToken(tokens, callPosition.Line, callPosition.Character, semanticFunction, 0) {
		t.Fatalf("expected function token at call position, got %#v", tokens)
	}
	firstName := positionOf(t, source, `firstName &`, 0)
	if !hasSemanticToken(tokens, firstName.Line, firstName.Character, semanticParameter, semanticByval) {
		t.Fatalf("expected ByVal parameter token at firstName usage, got %#v", tokens)
	}
	lastName := positionOf(t, source, `lastName`, 0)
	if !hasSemanticToken(tokens, lastName.Line, lastName.Character, semanticParameter, semanticByref) {
		t.Fatalf("expected ByRef parameter token at lastName usage, got %#v", tokens)
	}
}

func TestInlayHintsWithOptions(t *testing.T) {
	const uri = "file:///tmp/inlay-options.asp"
	source := `<%
Function BuildName(ByVal firstName, lastName)
End Function
Response.Write BuildName("Ada", "Lovelace")
%>`
	parsed := core.ParseDocument(uri, source, core.Settings{DefaultLanguage: "VBScript"})
	fullRange := lsp.Range{Start: lsp.Position{}, End: positionOf(t, source, "%>", 0)}

	hints := InlayHintsWithOptions(parsed, fullRange, InlayHintOptions{ImplicitByRef: true, ParameterNames: true})
	labels := inlayLabels(hints)
	if !containsString(labels, "ByRef ") {
		t.Fatalf("expected implicit ByRef hint, got %#v", hints)
	}
	if !containsString(labels, "firstName:") || !containsString(labels, "lastName:") {
		t.Fatalf("expected parameter name hints, got %#v", hints)
	}

	noNames := InlayHintsWithOptions(parsed, fullRange, InlayHintOptions{ParameterNames: false})
	if len(noNames) != 0 {
		t.Fatalf("expected parameter names disabled, got %#v", noNames)
	}
}

func TestImplicitByRefInlayHintsUseParameterPositionsWhenNamesOverlap(t *testing.T) {
	const uri = "file:///tmp/inlay-parameter-position.asp"
	source := `<%
Function RandomInt(mInt, maxInt)
End Function
Function ExplicitRef(ByRef ref)
End Function
%>`
	parsed := core.ParseDocument(uri, source, core.Settings{DefaultLanguage: "VBScript"})
	fullRange := lsp.Range{Start: lsp.Position{}, End: positionOf(t, source, "%>", 0)}

	hints := InlayHintsWithOptions(parsed, fullRange, InlayHintOptions{ImplicitByRef: true})
	if len(hints) != 2 {
		t.Fatalf("expected two implicit ByRef hints, got %#v", hints)
	}
	wantPositions := []lsp.Position{
		positionOf(t, source, "(mInt", 1),
		positionOf(t, source, "maxInt", 0),
	}
	for i, want := range wantPositions {
		if hints[i].Position != want {
			t.Fatalf("hint %d position = %#v, want %#v: %#v", i, hints[i].Position, want, hints)
		}
	}
}

func positionOf(t *testing.T, source, needle string, delta int) lsp.Position {
	t.Helper()
	offset := -1
	for i := 0; i+len(needle) <= len(source); i++ {
		if source[i:i+len(needle)] == needle {
			offset = i + delta
			break
		}
	}
	if offset < 0 {
		t.Fatalf("needle %q not found", needle)
	}
	doc := core.NewTextDocument("file:///tmp/test.asp", "classic-asp", 0, source)
	return doc.PositionAt(offset)
}

func inlayLabels(hints []lsp.InlayHint) []string {
	labels := make([]string, 0, len(hints))
	for _, hint := range hints {
		if label, ok := hint.Label.(string); ok {
			labels = append(labels, label)
		}
	}
	return labels
}

func containsString(values []string, target string) bool {
	for _, value := range values {
		if value == target {
			return true
		}
	}
	return false
}

func hasLocationAtLine(locations []lsp.Location, line int) bool {
	for _, location := range locations {
		if location.Range.Start.Line == line {
			return true
		}
	}
	return false
}

func decodeSemantic(data []int) []semanticToken {
	var tokens []semanticToken
	line := 0
	character := 0
	for i := 0; i+4 < len(data); i += 5 {
		line += data[i]
		if data[i] == 0 {
			character += data[i+1]
		} else {
			character = data[i+1]
		}
		tokens = append(tokens, semanticToken{
			Line:      line,
			Character: character,
			Length:    data[i+2],
			Type:      data[i+3],
			Modifiers: data[i+4],
		})
	}
	return tokens
}

func hasSemanticToken(tokens []semanticToken, line int, character int, tokenType int, modifiers int) bool {
	for _, token := range tokens {
		if token.Line == line && token.Character == character && token.Type == tokenType && token.Modifiers == modifiers {
			return true
		}
	}
	return false
}
