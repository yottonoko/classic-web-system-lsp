package lspserver

import (
	"io"
	"os"
	"path/filepath"
	"strings"
	"testing"

	"github.com/yottonoko/classic-web-system-lsp/internal/core"
	"github.com/yottonoko/classic-web-system-lsp/internal/vbscript"
)

func TestVBScriptTypeInfoUsesAssignmentsAndAnnotations(t *testing.T) {
	source := `<%
Class Customer
  Public Name
End Class
' @member Customer.Name As String
' @returns Customer
Function MakeCustomer()
  Set MakeCustomer = New Customer
End Function
' @type rs As ADODB.Recordset
Dim rs
Dim c
Set c = MakeCustomer()
Dim d
Set d = c
%>`
	parsed := core.ParseDocument("file:///site/default.asp", source, core.Settings{DefaultLanguage: "VBScript"})
	server := New(strings.NewReader(""), io.Discard, io.Discard)

	info := server.vbscriptTypeInfo(parsed)
	for name, expected := range map[string]string{
		"rs": "ADODB.Recordset",
		"c":  "Customer",
		"d":  "Customer",
	} {
		if got := info.variableTypes[name]; got != expected {
			t.Fatalf("%s type = %q, want %q in %#v", name, got, expected, info.variableTypes)
		}
	}

	member := info.members["customer"]["name"]
	if member.Name != "name" || member.TypeName != "String" || member.Kind != "property" {
		t.Fatalf("Customer.Name member metadata mismatch: %#v", member)
	}

	analysis := graphAnalysisTypes(parsed)
	makeCustomer := requireDeclaration(t, graphVBDeclarations(parsed), "MakeCustomer")
	node := graphNodeForVBDeclaration(parsed, makeCustomer, graphDeclarationNodeID(parsed.URI, makeCustomer.Name, makeCustomer.Range), &analysis)
	if node.TypeName != "Customer" {
		t.Fatalf("MakeCustomer graph type = %q, want Customer: %#v", node.TypeName, node)
	}
}

func TestVBScriptPropertyAccessorsUseDistinctCSTScopes(t *testing.T) {
	source := `<%
Class First
  Property Get Value()
    ' @type local As String
    Dim local
    Const marker = 1
    local = 1
  End Property
  Property Let Value(newValue)
    ' @type local As Number
    Dim local
    Const marker = 1
    local = "bad"
  End Property
  Property Set Value(newObject)
    ' @type local As First
    Dim local
    Const marker = 1
    local = "bad"
  End Property
End Class
Class Second
  Property Get Value()
    ' @type local As Boolean
    Dim local
    Const marker = 1
    local = "bad"
  End Property
End Class
%>`
	parsed := core.ParseDocument("file:///site/property-scopes.asp", source, core.Settings{DefaultLanguage: "VBScript"})
	scopes := vbProcedureScopes(parsed)
	wantScopes := map[string]string{
		"first.value#get":  "get",
		"first.value#let":  "let",
		"first.value#set":  "set",
		"second.value#get": "get",
	}
	for key, accessor := range wantScopes {
		found := false
		for _, scope := range scopes {
			if strings.EqualFold(scope.Key(), key) {
				found = true
				if scope.Accessor != accessor {
					t.Fatalf("scope %s accessor = %q, want %q: %#v", key, scope.Accessor, accessor, scopes)
				}
			}
		}
		if !found {
			t.Fatalf("property scope %s missing: %#v", key, scopes)
		}
	}

	declarations := collectVBUsageDeclarations(parsed).Declarations
	locals := map[string]vbUsageDeclaration{}
	for _, declaration := range declarations {
		if declaration.Local && strings.EqualFold(declaration.Name, "local") {
			locals[strings.ToLower(declaration.Scope)] = declaration
		}
	}
	if len(locals) != len(wantScopes) {
		t.Fatalf("property locals = %#v, want one per accessor scope", locals)
	}
	for key := range wantScopes {
		if declaration, ok := locals[key]; !ok || declaration.Scope == "" {
			t.Fatalf("property local scope %s missing or global: %#v", key, declaration)
		}
	}
	consts := map[string]vbUsageDeclaration{}
	for _, declaration := range declarations {
		if declaration.Local && strings.EqualFold(declaration.Name, "marker") {
			consts[strings.ToLower(declaration.Scope)] = declaration
		}
	}
	if len(consts) != len(wantScopes) {
		t.Fatalf("property constants = %#v, want one per accessor scope", consts)
	}
	for key, declaration := range consts {
		if declaration.Scope == "" || !strings.EqualFold(key, declaration.Scope) {
			t.Fatalf("property constant %q is not scope-bound: %#v", key, declaration)
		}
	}
	parameters := vbParameterDeclarationsFromTokens(parsed)
	for _, name := range []string{"newValue", "newObject"} {
		found := false
		for _, declaration := range parameters {
			if strings.EqualFold(declaration.Name, name) {
				found = true
				if !strings.EqualFold(declaration.Scope, "first.value#let") && !strings.EqualFold(declaration.Scope, "first.value#set") {
					t.Fatalf("property parameter %s scope = %q", name, declaration.Scope)
				}
			}
		}
		if !found {
			t.Fatalf("property parameter %s missing: %#v", name, parameters)
		}
	}

	ranges := graphVBProcedureRanges(parsed)
	accessors := map[string]string{}
	for _, procedure := range ranges {
		accessors[strings.ToLower(procedure.key())] = procedure.accessor
	}
	for key, accessor := range wantScopes {
		if got := accessors[key]; got != accessor {
			t.Fatalf("graph procedure range %s accessor = %q, want %q: %#v", key, got, accessor, ranges)
		}
	}

	server := New(strings.NewReader(""), io.Discard, io.Discard)
	server.settings.VBScriptTypeChecking = "strict"
	analysis := graphAnalysisTypes(parsed)
	for key, declaration := range locals {
		if got := graphTypeAnnotationForDeclaration(declaration, &analysis); got == "" {
			t.Fatalf("property local %s lost annotation: %#v", key, declaration)
		}
	}
	info := server.vbscriptTypeInfo(parsed)
	wantTypes := map[string]string{
		"first.value#get":  "String",
		"first.value#let":  "Number",
		"first.value#set":  "First",
		"second.value#get": "Boolean",
	}
	for key, want := range wantTypes {
		if got := info.scopedExplicitTypeNames[vbscriptTypeScopeKey(parsed, key, "local")]; got != want {
			t.Fatalf("property local %s type = %q, want %s: %#v", key, got, want, info.scopedExplicitTypeNames)
		}
	}
	diagnostics := server.vbscriptTypeDiagnostics(parsed)
	seenExpected := map[string]bool{}
	for _, diagnostic := range diagnostics {
		if diagnostic.Code != "typeMismatch" {
			continue
		}
		if expected, ok := diagnostic.Data.(map[string]any)["expectedType"].(string); ok {
			seenExpected[expected] = true
		}
	}
	for _, expected := range []string{"String", "Number", "First", "Boolean"} {
		if !seenExpected[expected] {
			t.Fatalf("property type mismatch for %s missing: %#v", expected, diagnostics)
		}
	}

	for _, marker := range []string{"local = 1", `local = "bad"`} {
		offset := strings.Index(source, marker)
		if offset < 0 {
			t.Fatalf("property reference marker %q missing", marker)
		}
		position := core.NewTextDocument(parsed.URI, "classic-asp", 0, parsed.Text).PositionAt(offset)
		locations := scopedVBScriptDefinition(parsed, position)
		if len(locations) != 1 {
			t.Fatalf("property reference %q definition count = %d: %#v", marker, len(locations), locations)
		}
	}

	postings := vbscript.BuildReferenceShard(parsed).PostingsFor("local")
	propertyKinds := map[string]bool{}
	for _, posting := range postings {
		if posting.ClassOwner != "" && strings.HasPrefix(strings.ToLower(posting.ScopeKind), "property-") {
			propertyKinds[strings.ToLower(posting.ClassOwner)+":"+strings.ToLower(posting.ScopeKind)] = true
		}
	}
	for _, key := range []string{"first:property-get", "first:property-let", "first:property-set", "second:property-get"} {
		if !propertyKinds[key] {
			t.Fatalf("property reference scope %s missing: %#v", key, postings)
		}
	}
}

func TestVBScriptTypeAnnotationsIgnoreHTMLComments(t *testing.T) {
	source := `<div>
' @type fake As String
</div>
<%
' @type real As Number
Dim real
%>`
	parsed := core.ParseDocument("file:///site/html-annotation.asp", source, core.Settings{DefaultLanguage: "VBScript"})
	analysis := graphAnalysisTypes(parsed)
	if _, ok := analysis.Types["fake"]; ok {
		t.Fatalf("HTML annotation leaked into global types: %#v", analysis.Types)
	}
	if got := analysis.Types["real"]; got != "Number" {
		t.Fatalf("VBScript annotation type = %q, want Number: %#v", got, analysis.Types)
	}
	if annotations := analysis.TypeAnnotations["real"]; len(annotations) != 1 || annotations[0].Line != 4 {
		t.Fatalf("VBScript annotation line = %#v, want absolute line 4", annotations)
	}
	if len(analysis.TypeAnnotations["fake"]) != 0 {
		t.Fatalf("HTML annotation was collected: %#v", analysis.TypeAnnotations["fake"])
	}
}

func TestVBScriptClassFieldAnnotationsStayMemberScoped(t *testing.T) {
	source := `<%
Class First
  ' @type value As String
  Public value
End Class
Class Second
  Public value
End Class
%>`
	parsed := core.ParseDocument("file:///site/class-field-annotations.asp", source, core.Settings{DefaultLanguage: "VBScript"})
	analysis := graphAnalysisTypes(parsed)
	if _, ok := analysis.Types["value"]; ok {
		t.Fatalf("class field annotation leaked into global types: %#v", analysis.Types)
	}
	annotations := analysis.TypeAnnotations["value"]
	if len(annotations) != 1 || !strings.EqualFold(annotations[0].MemberOf, "First") {
		t.Fatalf("class field annotation owner = %#v, want First", annotations)
	}
	info := (&Server{}).vbscriptTypeInfo(parsed)
	if _, ok := info.variableTypes["value"]; ok {
		t.Fatalf("class field type polluted global variable types: %#v", info.variableTypes)
	}

	declarations := graphVBDeclarations(parsed)
	var first, second vbUsageDeclaration
	for _, declaration := range declarations {
		if declaration.Kind != "field" || !strings.EqualFold(declaration.Name, "value") {
			continue
		}
		switch strings.ToLower(declaration.MemberOf) {
		case "first":
			first = declaration
		case "second":
			second = declaration
		}
	}
	if first.Name == "" || second.Name == "" {
		t.Fatalf("class field declarations missing: %#v", declarations)
	}
	firstNode := graphNodeForVBDeclaration(parsed, first, graphDeclarationNodeID(parsed.URI, first.Name, first.Range), &analysis)
	secondNode := graphNodeForVBDeclaration(parsed, second, graphDeclarationNodeID(parsed.URI, second.Name, second.Range), &analysis)
	if firstNode.TypeName != "String" {
		t.Fatalf("First.value graph type = %q, want String: %#v", firstNode.TypeName, firstNode)
	}
	if strings.EqualFold(secondNode.TypeName, "String") {
		t.Fatalf("Second.value inherited First annotation: %#v", secondNode)
	}

	members := vbClassMemberCompletionsByClass(parsed)
	if got := members["first"]["value"].TypeName; got != "String" {
		t.Fatalf("First.value completion type = %q, want String: %#v", got, members)
	}
	if got := members["second"]["value"].TypeName; strings.EqualFold(got, "String") {
		t.Fatalf("Second.value completion inherited First annotation: %#v", members)
	}
	fieldOffset := strings.Index(source, "Public value") + len("Public ")
	hover, handled := (&Server{}).vbscriptMemberHover(parsed, fieldOffset)
	if !handled || hover == nil || !strings.Contains(mustJSONForTest(t, hover), "First.value As String") {
		t.Fatalf("First.value hover missing annotation: handled=%t hover=%#v", handled, hover)
	}

	summaries := collectVBScriptPublicSummarySymbols(parsed)
	for _, summary := range summaries {
		if summary.Kind != "field" || !strings.EqualFold(summary.Name, "value") {
			continue
		}
		if strings.EqualFold(summary.MemberOf, "First") && (summary.TypeName != "String" || !summary.ExplicitType) {
			t.Fatalf("First.value summary mismatch: %#v", summary)
		}
		if strings.EqualFold(summary.MemberOf, "Second") && (summary.TypeName == "String" || summary.ExplicitType) {
			t.Fatalf("Second.value summary inherited First annotation: %#v", summary)
		}
	}
}

func TestVBScriptTypeInfoInfersExpressionsAndScopedParameters(t *testing.T) {
	source := `<%
' @type label As String
Dim label
label = "a" & "b"
' @type count As Number
Dim count
count = (1 + 2) * 3
' @type flags As Boolean
Dim flags
flags = count > 1 And True
' @type items As Array
Dim items
items = Array("a", "b")
' @type sharedName As String
Dim sharedName
Sub Save(Optional ByVal firstName, ByRef lastName, defaultName)
  ' @type sharedName As Number
  Dim sharedName
End Sub
%>`
	parsed := core.ParseDocument("file:///site/default.asp", source, core.Settings{DefaultLanguage: "VBScript"})
	server := New(strings.NewReader(""), io.Discard, io.Discard)
	server.settings.VBScriptTypeChecking = "strict"

	if diagnostics := server.vbscriptTypeDiagnostics(parsed); len(diagnostics) != 0 {
		t.Fatalf("expression type diagnostics mismatch: %#v", diagnostics)
	}

	analysis := graphAnalysisTypes(parsed)
	declarations := graphVBDeclarations(parsed)
	globalShared := requireScopedDeclaration(t, declarations, "sharedName", "")
	globalNode := graphNodeForVBDeclaration(parsed, globalShared, graphDeclarationNodeID(parsed.URI, globalShared.Name, globalShared.Range), &analysis)
	if globalNode.TypeName != "String" {
		t.Fatalf("global sharedName type = %q, want String: %#v", globalNode.TypeName, globalNode)
	}
	localShared := requireScopedDeclaration(t, declarations, "sharedName", "save")
	localNode := graphNodeForVBDeclaration(parsed, localShared, graphDeclarationNodeID(parsed.URI, localShared.Name, localShared.Range), &analysis)
	if localNode.TypeName != "Number" {
		t.Fatalf("local sharedName type = %q, want Number: %#v", localNode.TypeName, localNode)
	}

	signature := analysis.Signatures["save"]
	if len(signature.Parameters) != 3 {
		t.Fatalf("Save parameters = %#v", signature.Parameters)
	}
	if signature.Parameters[0].Name != "firstName" || signature.Parameters[0].Mode != "ByVal" || !signature.Parameters[0].Optional {
		t.Fatalf("firstName parameter mismatch: %#v", signature.Parameters[0])
	}
	if signature.Parameters[1].Name != "lastName" || signature.Parameters[1].Mode != "ByRef" || signature.Parameters[1].Optional {
		t.Fatalf("lastName parameter mismatch: %#v", signature.Parameters[1])
	}
	if signature.Parameters[2].Name != "defaultName" || signature.Parameters[2].Mode != "ByRef" || signature.Parameters[2].Optional {
		t.Fatalf("defaultName parameter mismatch: %#v", signature.Parameters[2])
	}
}

func TestVBScriptTypeAnnotationsStayBoundToDeclarationScope(t *testing.T) {
	source := `<%
' @type sharedName As String
Dim sharedName
sharedName = "global"
Sub Save()
  ' @type sharedName As Number
  Dim sharedName
  sharedName = 1
End Sub
%>`
	parsed := core.ParseDocument("file:///site/scoped-annotations.asp", source, core.Settings{DefaultLanguage: "VBScript"})
	server := New(strings.NewReader(""), io.Discard, io.Discard)
	server.settings.VBScriptTypeChecking = "strict"

	if diagnostics := server.vbscriptTypeDiagnostics(parsed); len(diagnostics) != 0 {
		t.Fatalf("scope-local annotations constrained an unrelated same-name declaration: %#v", diagnostics)
	}
	info := server.vbscriptTypeInfo(parsed)
	globalKey := vbscriptTypeScopeKey(parsed, "", "sharedName")
	localKey := vbscriptTypeScopeKey(parsed, "save", "sharedName")
	if got := info.scopedExplicitTypeNames[globalKey]; got != "String" {
		t.Fatalf("global sharedName annotation = %q, want String", got)
	}
	if got := info.scopedExplicitTypeNames[localKey]; got != "Number" {
		t.Fatalf("local sharedName annotation = %q, want Number", got)
	}
}

func TestVBScriptGraphTypeAnnotationsMatchDeclarationScope(t *testing.T) {
	source := `<%
' @type sharedName As Number
Sub Save()
  Dim sharedName
End Sub
' @type sharedName As String
Dim sharedName
%>`
	parsed := core.ParseDocument("file:///site/graph-scoped-annotations.asp", source, core.Settings{DefaultLanguage: "VBScript"})
	analysis := graphAnalysisTypes(parsed)
	declarations := graphVBDeclarations(parsed)
	local := requireScopedDeclaration(t, declarations, "sharedName", "save")
	global := requireScopedDeclaration(t, declarations, "sharedName", "")
	localNode := graphNodeForVBDeclaration(parsed, local, graphDeclarationNodeID(parsed.URI, local.Name, local.Range), &analysis)
	globalNode := graphNodeForVBDeclaration(parsed, global, graphDeclarationNodeID(parsed.URI, global.Name, global.Range), &analysis)
	if localNode.TypeName != "Variant" {
		t.Fatalf("local graph annotation leaked from global scope: %q, want Variant", localNode.TypeName)
	}
	if globalNode.TypeName != "String" {
		t.Fatalf("global graph annotation = %q, want String", globalNode.TypeName)
	}
}

func TestVBScriptGlobalAnnotatedMemberRemainsVisibleInsideProcedure(t *testing.T) {
	source := `<%
Class Customer
  Public Name
End Class
' @member Customer.Name As String
' @type sharedCustomer As Customer
Dim sharedCustomer
Sub Uses()
  sharedCustomer.Name
End Sub
%>`
	parsed := core.ParseDocument("file:///site/global-annotation-in-procedure.asp", source, core.Settings{DefaultLanguage: "VBScript"})
	server := New(strings.NewReader(""), io.Discard, io.Discard)
	server.settings.VBScriptTypeChecking = "strict"

	if vbscriptNameBoundInScope(parsed, "sharedCustomer", "uses") {
		t.Fatal("global declaration incorrectly shadowed the global annotated type inside Uses")
	}
	diagnostics := server.vbscriptTypeDiagnostics(parsed)
	if serialized := mustJSONText(t, diagnostics); strings.Contains(serialized, "missingMember") {
		t.Fatalf("global annotated member was not resolved inside Uses: %s", serialized)
	}
	completionOffset := strings.Index(source, "sharedCustomer.Name") + len("sharedCustomer.")
	items := server.vbscriptTypedMemberCompletions(parsed, "sharedCustomer", completionOffset)
	foundName := false
	for _, item := range items {
		if strings.EqualFold(item.Label, "Name") {
			foundName = true
			break
		}
	}
	if !foundName {
		t.Fatalf("global annotated member completion missing inside Uses: %#v", items)
	}
	if hover, handled := server.vbscriptMemberHover(parsed, completionOffset+1); !handled || hover == nil {
		t.Fatalf("global annotated member hover missing inside Uses: handled=%t hover=%#v", handled, hover)
	}
}

func TestVBScriptIncludedGlobalAnnotationRemainsVisibleInsideProcedure(t *testing.T) {
	root := t.TempDir()
	includePath := filepath.Join(root, "globals.inc")
	ownerPath := filepath.Join(root, "default.asp")
	includeSource := `<%
Class Customer
  Public Name
End Class
' @member Customer.Name As String
' @type sharedCustomer As Customer
Dim sharedCustomer
%>`
	if err := os.WriteFile(includePath, []byte(includeSource), 0o644); err != nil {
		t.Fatal(err)
	}
	ownerSource := `<!-- #include file="globals.inc" -->
<%
Sub Uses()
  sharedCustomer.Name
End Sub
%>`
	parsed := core.ParseDocument(filePathURI(ownerPath), ownerSource, core.Settings{DefaultLanguage: "VBScript"})
	server := New(strings.NewReader(""), io.Discard, io.Discard)
	server.rootPath = root
	server.rootURI = filePathURI(root)
	server.settings.VBScriptTypeChecking = "strict"

	if included := server.includedDocuments(parsed); len(included) != 1 {
		t.Fatalf("included global fixture count = %d, want 1", len(included))
	}
	diagnostics := server.vbscriptTypeDiagnostics(parsed)
	if serialized := mustJSONText(t, diagnostics); strings.Contains(serialized, "missingMember") {
		t.Fatalf("included global annotation was not resolved inside Uses: %s", serialized)
	}
	offset := strings.Index(ownerSource, "sharedCustomer.Name") + len("sharedCustomer.")
	items := server.vbscriptTypedMemberCompletions(parsed, "sharedCustomer", offset)
	foundName := false
	for _, item := range items {
		if strings.EqualFold(item.Label, "Name") {
			foundName = true
			break
		}
	}
	if !foundName {
		t.Fatalf("included global annotation completion missing inside Uses: %#v", items)
	}
}

func TestVBScriptLocalParameterAndClassMembersShadowGlobalAnnotations(t *testing.T) {
	tests := []struct {
		name   string
		source string
	}{
		{
			name: "local",
			source: `<%
Class Customer
  Public Name
End Class
' @member Customer.Name As String
' @type sharedCustomer As Customer
Dim sharedCustomer
Sub Uses()
  Dim sharedCustomer
  sharedCustomer.Name
End Sub
%>`,
		},
		{
			name: "parameter",
			source: `<%
Class Customer
  Public Name
End Class
' @member Customer.Name As String
' @type sharedCustomer As Customer
Dim sharedCustomer
Sub Uses(sharedCustomer)
  sharedCustomer.Name
End Sub
%>`,
		},
		{
			name: "class member",
			source: `<%
Class Customer
  Public Name
End Class
Class Holder
  Public sharedCustomer
  Sub Uses()
    sharedCustomer.Name
  End Sub
End Class
' @member Customer.Name As String
' @type sharedCustomer As Customer
Dim sharedCustomer
%>`,
		},
	}
	for _, test := range tests {
		t.Run(test.name, func(t *testing.T) {
			parsed := core.ParseDocument("file:///site/global-annotation-shadow-"+test.name+".asp", test.source, core.Settings{DefaultLanguage: "VBScript"})
			server := New(strings.NewReader(""), io.Discard, io.Discard)
			server.settings.VBScriptTypeChecking = "strict"
			if !vbscriptNameBoundInScope(parsed, "sharedCustomer", "uses") {
				t.Fatal("scope declaration did not shadow the global annotation")
			}
			offset := strings.Index(test.source, "sharedCustomer.Name") + len("sharedCustomer.")
			items := server.vbscriptTypedMemberCompletions(parsed, "sharedCustomer", offset)
			for _, item := range items {
				if strings.EqualFold(item.Label, "Name") {
					t.Fatalf("shadowed global annotation leaked into completion: %#v", items)
				}
			}
		})
	}
}

func TestVBScriptTypeInfoUsesVBScriptLiteralAndNumericFamilyTypes(t *testing.T) {
	source := `<%
Dim emptyValue, nullValue, objectValue, numericValue, currencyValue
emptyValue = Empty
nullValue = Null
Set objectValue = Nothing
' @type numericValue As Number
numericValue = CCur(1)
' @type currencyValue As Currency
currencyValue = 1
%>`
	parsed := core.ParseDocument("file:///site/default.asp", source, core.Settings{DefaultLanguage: "VBScript"})
	server := New(strings.NewReader(""), io.Discard, io.Discard)
	server.settings.VBScriptTypeChecking = "strict"

	if diagnostics := server.vbscriptTypeDiagnostics(parsed); len(diagnostics) != 0 {
		t.Fatalf("literal type diagnostics mismatch: %#v", diagnostics)
	}

	analysis := graphAnalysisTypes(parsed)
	declarations := graphVBDeclarations(parsed)
	for name, expected := range map[string]string{
		"emptyValue":    "Empty",
		"nullValue":     "Null",
		"objectValue":   "Nothing",
		"numericValue":  "Currency",
		"currencyValue": "1",
	} {
		declaration := requireScopedDeclaration(t, declarations, name, "")
		node := graphNodeForVBDeclaration(parsed, declaration, graphDeclarationNodeID(parsed.URI, declaration.Name, declaration.Range), &analysis)
		if node.TypeName != expected {
			t.Fatalf("%s type = %q, want %q: %#v", name, node.TypeName, expected, node)
		}
	}

	for _, scalar := range []string{"Integer", "Decimal", "Error"} {
		if isVBScriptObjectType(scalar, vbscriptTypeInfo{}) {
			t.Fatalf("%s should not be treated as object type", scalar)
		}
	}
}

func TestVBScriptTypeInfoInfersAndChecksUnionTypes(t *testing.T) {
	source := `<%
Class FirstThing
  Public SharedName
  Public OnlyFirst
End Class
Class SecondThing
  Public SharedName
End Class
x = 1
x = "a"
' @type annotated As Number
Dim annotated
annotated = "oops"
' @type maybeName As String | Number
Dim maybeName
Class Holder
  Public Value
End Class
' @member Holder.Value As String | Number
Function MakeValue(flag)
  If flag Then
    MakeValue = 1
  Else
    MakeValue = "x"
  End If
End Function
Dim both
Set both = New FirstThing
Set both = New SecondThing
both.SharedName
both.OnlyFirst
%>`
	parsed := core.ParseDocument("file:///site/default.asp", source, core.Settings{DefaultLanguage: "VBScript"})
	server := New(strings.NewReader(""), io.Discard, io.Discard)
	server.settings.VBScriptTypeChecking = "strict"

	info := server.vbscriptTypeInfo(parsed)
	for name, expected := range map[string]string{
		"x":         "Number | String",
		"annotated": "Number",
		"maybeName": "String | Number",
		"both":      "FirstThing | SecondThing",
	} {
		if got := info.variableTypes[strings.ToLower(name)]; got != expected {
			t.Fatalf("%s type = %q, want %q in %#v", name, got, expected, info.variableTypes)
		}
	}

	analysis := graphAnalysisTypes(parsed)
	makeValue := requireDeclaration(t, graphVBDeclarations(parsed), "MakeValue")
	makeValueNode := graphNodeForVBDeclaration(parsed, makeValue, graphDeclarationNodeID(parsed.URI, makeValue.Name, makeValue.Range), &analysis)
	if makeValueNode.TypeName != "Number | String" {
		t.Fatalf("MakeValue type = %q, want Number | String: %#v", makeValueNode.TypeName, makeValueNode)
	}
	member := info.members["holder"]["value"]
	if member.TypeName != "String | Number" {
		t.Fatalf("Holder.Value type = %q, want String | Number: %#v", member.TypeName, member)
	}

	diagnostics := server.vbscriptTypeDiagnostics(parsed)
	diagnosticJSON := mustJSONText(t, diagnostics)
	if !strings.Contains(diagnosticJSON, "typeMismatch") || !strings.Contains(diagnosticJSON, "missingMember") {
		t.Fatalf("union diagnostics missing expected issues: %s", diagnosticJSON)
	}
	if strings.Contains(diagnosticJSON, "SharedName") {
		t.Fatalf("union diagnostics flagged shared member: %s", diagnosticJSON)
	}
}

func TestVBScriptUnionMemberRequiresEveryConcreteArm(t *testing.T) {
	source := `<%
Class Customer
  Public Name
End Class
' @type customerOrString As Customer | String
Dim customerOrString
customerOrString.Name
%>`
	parsed := core.ParseDocument("file:///site/unsafe-union-member.asp", source, core.Settings{DefaultLanguage: "VBScript"})
	server := New(strings.NewReader(""), io.Discard, io.Discard)
	server.settings.VBScriptTypeChecking = "strict"

	info := server.vbscriptTypeInfo(parsed)
	member, checked := vbscriptUnionMember("Customer | String", "Name", info)
	if !checked || member != nil {
		t.Fatalf("Customer | String member resolution = (%#v, %t), want (nil, true)", member, checked)
	}
	diagnostics := server.vbscriptTypeDiagnostics(parsed)
	serialized := mustJSONText(t, diagnostics)
	if !strings.Contains(serialized, "missingMember") || !strings.Contains(serialized, "customerOrString") {
		t.Fatalf("unsafe union member diagnostic missing: %s", serialized)
	}
}

func TestVBScriptConfiguredUnionCompletionRequiresEveryConcreteArm(t *testing.T) {
	source := `<%
' @type customerOrString As Customer | String
Dim customerOrString
customerOrString.
%>`
	parsed := core.ParseDocument("file:///site/unsafe-union-completion.asp", source, core.Settings{DefaultLanguage: "VBScript"})
	server := New(strings.NewReader(""), io.Discard, io.Discard)
	server.settings.VBScriptComTypes = map[string]vbscriptComTypeSetting{
		"Customer": {Members: map[string]vbscriptComMemberSetting{"Name": {Type: "String"}}},
	}
	items := server.vbscriptConfiguredMemberCompletions(parsed, strings.Index(source, "customerOrString.")+len("customerOrString."))
	if len(items) != 0 {
		t.Fatalf("Customer | String configured completion exposed unsupported-arm members: %#v", items)
	}
}

func TestVBScriptConfiguredGlobalTypesPreserveExpressionsAndCommonMembers(t *testing.T) {
	source := `<%
value.shared()
value.firstOnly
value.
%>`
	parsed := core.ParseDocument("file:///site/configured-global-types.asp", source, core.Settings{DefaultLanguage: "VBScript"})
	server := New(strings.NewReader(""), io.Discard, io.Discard)
	server.settings.VBScriptGlobals = map[string]vbscriptGlobalSetting{
		"value":         {Type: "First | Second"},
		"templateValue": {Type: "`page-${String}.asp`"},
		"literalValue":  {Type: `"ready" | 200`},
	}
	server.settings.VBScriptComTypes = map[string]vbscriptComTypeSetting{
		"First": {Members: map[string]vbscriptComMemberSetting{
			"shared":    {Kind: "method", ReturnType: "String"},
			"firstOnly": {Type: "String"},
		}},
		"Second": {Members: map[string]vbscriptComMemberSetting{
			"shared": {Kind: "method", ReturnType: "String"},
		}},
	}
	server.settings.VBScriptTypeChecking = "strict"

	info := server.vbscriptTypeInfo(parsed)
	for name, want := range map[string]string{
		"value":         "First | Second",
		"templatevalue": "`page-${String}.asp`",
		"literalvalue":  `"ready" | 200`,
	} {
		if got := info.variableTypes[name]; got != want {
			t.Fatalf("configured global %s type = %q, want %q", name, got, want)
		}
	}

	diagnostics := server.vbscriptTypeDiagnostics(parsed)
	serialized := mustJSONText(t, diagnostics)
	if !strings.Contains(serialized, "missingMember") || !strings.Contains(serialized, "First | Second") {
		t.Fatalf("configured global union diagnostic lost full type/member safety: %s", serialized)
	}
	if strings.Contains(serialized, "unknownCall") {
		t.Fatalf("configured global common method was not resolved: %s", serialized)
	}

	completionOffset := strings.Index(source, "value.") + len("value.")
	items := server.vbscriptConfiguredMemberCompletions(parsed, completionOffset)
	if len(items) != 1 || !strings.EqualFold(items[0].Label, "shared") {
		t.Fatalf("configured global union completion = %#v, want only shared", items)
	}
}

func TestVBScriptMethodAnnotationsAreQualifiedByClassOwner(t *testing.T) {
	source := `<%
Class First
  ' @param Render.first As String
  ' @returns Render String
  Public Function Render(first)
    Render = first
  End Function
End Class
Class Second
  ' @param Render.second As Number
  ' @returns Render Number
  Public Function Render(second)
    Render = second
  End Function
End Class
' @type firstObject As First
Dim firstObject
' @type secondObject As Second
Dim secondObject
firstObject.Render("value")
secondObject.Render(1)
%>`
	parsed := core.ParseDocument("file:///site/qualified-method-annotations.asp", source, core.Settings{DefaultLanguage: "VBScript"})
	server := New(strings.NewReader(""), io.Discard, io.Discard)
	analysis := graphAnalysisTypes(parsed)
	if got := analysis.ScopedReturns[graphSignatureKey("First", "Render")]; got != "String" {
		t.Fatalf("First.Render return = %q, want String: %#v", got, analysis.ScopedReturns)
	}
	if got := analysis.ScopedReturns[graphSignatureKey("Second", "Render")]; got != "Number" {
		t.Fatalf("Second.Render return = %q, want Number: %#v", got, analysis.ScopedReturns)
	}
	if got := analysis.ScopedParams[graphSignatureKey("First", "Render")]["first"]; got != "String" {
		t.Fatalf("First.Render first parameter = %q, want String: %#v", got, analysis.ScopedParams)
	}
	if got := analysis.ScopedParams[graphSignatureKey("Second", "Render")]["second"]; got != "Number" {
		t.Fatalf("Second.Render second parameter = %q, want Number: %#v", got, analysis.ScopedParams)
	}
	if _, ok := analysis.Returns["render"]; ok {
		t.Fatalf("class-scoped return annotation leaked into legacy global map: %#v", analysis.Returns)
	}

	declarations := graphVBDeclarations(parsed)
	for owner, want := range map[string]struct {
		returnType string
		paramType  string
	}{
		"First":  {returnType: "String", paramType: "String"},
		"Second": {returnType: "Number", paramType: "Number"},
	} {
		var declaration vbUsageDeclaration
		for _, candidate := range declarations {
			if candidate.Kind == "method" && strings.EqualFold(candidate.MemberOf, owner) && strings.EqualFold(candidate.Name, "Render") {
				declaration = candidate
				break
			}
		}
		if declaration.Name == "" {
			t.Fatalf("%s.Render declaration missing: %#v", owner, declarations)
		}
		node := graphNodeForVBDeclaration(parsed, declaration, graphDeclarationNodeID(parsed.URI, declaration.Name, declaration.Range), &analysis)
		if node.TypeName != want.returnType || len(node.Parameters) != 1 || node.Parameters[0].TypeName != want.paramType {
			t.Fatalf("%s.Render graph metadata = %#v, want return %s and parameter %s", owner, node, want.returnType, want.paramType)
		}
	}

	info := server.vbscriptTypeInfo(parsed)
	if got := info.members["first"]["render"].TypeName; got != "String" {
		t.Fatalf("First.Render member type = %q, want String", got)
	}
	if got := info.members["second"]["render"].TypeName; got != "Number" {
		t.Fatalf("Second.Render member type = %q, want Number", got)
	}
	inlay := vbscriptFunctionReturnTypeInlayHints(parsed, fullDocumentRange(source), true)
	inlayText := mustJSONForTest(t, inlay)
	if !strings.Contains(inlayText, "As String") || !strings.Contains(inlayText, "As Number") {
		t.Fatalf("class-specific return inlay hints = %s", inlayText)
	}

	firstMethodOffset := strings.Index(source, "Render(first)")
	firstMethodHover, handled := server.vbscriptMemberHover(parsed, firstMethodOffset)
	if !handled || firstMethodHover == nil || !strings.Contains(mustJSONForTest(t, firstMethodHover), "Function Render(ByRef first) As String") {
		t.Fatalf("First.Render declaration hover = handled=%t hover=%#v", handled, firstMethodHover)
	}
	secondCallOffset := strings.Index(source, "secondObject.Render") + len("secondObject.")
	secondCallHover, handled := server.vbscriptMemberHover(parsed, secondCallOffset)
	if !handled || secondCallHover == nil || !strings.Contains(mustJSONForTest(t, secondCallHover), "Function Render(ByRef second) As Number") {
		t.Fatalf("Second.Render member hover = handled=%t hover=%#v", handled, secondCallHover)
	}
	firstParameterOffset := strings.Index(source, "Render(first)") + len("Render(")
	firstParameterHover := vbscriptParameterDeclarationHover(parsed, firstParameterOffset, "en")
	if firstParameterHover == nil || !strings.Contains(mustJSONForTest(t, firstParameterHover), "first As String") {
		t.Fatalf("First.Render parameter hover = %#v", firstParameterHover)
	}
	secondParameterOffset := strings.Index(source, "Render(second)") + len("Render(")
	secondParameterHover := vbscriptParameterDeclarationHover(parsed, secondParameterOffset, "en")
	if secondParameterHover == nil || !strings.Contains(mustJSONForTest(t, secondParameterHover), "second As Number") {
		t.Fatalf("Second.Render parameter hover = %#v", secondParameterHover)
	}

	summaries := collectVBScriptPublicSummarySymbols(parsed)
	for owner, want := range map[string]string{"First": "String", "Second": "Number"} {
		found := false
		for _, summary := range summaries {
			if summary.Kind == "method" && strings.EqualFold(summary.MemberOf, owner) && strings.EqualFold(summary.Name, "Render") {
				found = true
				if summary.TypeName != want {
					t.Fatalf("%s.Render summary type = %q, want %s", owner, summary.TypeName, want)
				}
			}
		}
		if !found {
			t.Fatalf("%s.Render summary missing: %#v", owner, summaries)
		}
	}
}

func TestVBScriptClassProcedureScopesSeparateSameNamedLocalsAndParameters(t *testing.T) {
	source := `<%
Class First
  Public StringMember
  ' @param value As String
  ' @returns Render String
  Public Function Render(value)
    ' @type local As First
    Dim local
    Set local = New First
    local.StringMember
    local.NumberMember
  End Function
End Class
Class Second
  Public NumberMember
  ' @param value As Number
  ' @returns Render Number
  Public Function Render(value)
    ' @type local As Second
    Dim local
    Set local = New Second
    local.NumberMember
    local.StringMember
  End Function
End Class
%>`
	parsed := core.ParseDocument("file:///site/class-procedure-scopes.asp", source, core.Settings{DefaultLanguage: "VBScript"})
	server := New(strings.NewReader(""), io.Discard, io.Discard)
	server.settings.VBScriptTypeChecking = "strict"

	firstLocalOffset := strings.Index(source, "local.StringMember")
	secondLocalOffset := strings.LastIndex(source, "local.NumberMember")
	firstScope := vbscriptScopeAtOffset(parsed, firstLocalOffset)
	secondScope := vbscriptScopeAtOffset(parsed, secondLocalOffset)
	if firstScope == "" || secondScope == "" || strings.EqualFold(firstScope, secondScope) {
		t.Fatalf("class procedure scopes = %q and %q, want distinct owner-qualified keys", firstScope, secondScope)
	}
	if vbProcedureScopeOwner(firstScope) == "" || vbProcedureScopeOwner(secondScope) == "" {
		t.Fatalf("class procedure scopes lost owners: %q, %q", firstScope, secondScope)
	}

	info := server.vbscriptTypeInfo(parsed)
	for scope, want := range map[string]string{firstScope: "First", secondScope: "Second"} {
		if got := info.scopedExplicitTypeNames[vbscriptTypeScopeKey(parsed, scope, "local")]; got != want {
			t.Fatalf("local type in scope %q = %q, want %q", scope, got, want)
		}
	}
	firstItems := server.vbscriptTypedMemberCompletions(parsed, "local", firstLocalOffset+len("local."))
	secondItems := server.vbscriptTypedMemberCompletions(parsed, "local", secondLocalOffset+len("local."))
	if !completionHasLabel(firstItems, "StringMember") || completionHasLabel(firstItems, "NumberMember") {
		t.Fatalf("First.Render local completion = %#v", firstItems)
	}
	if !completionHasLabel(secondItems, "NumberMember") || completionHasLabel(secondItems, "StringMember") {
		t.Fatalf("Second.Render local completion = %#v", secondItems)
	}

	firstHover := server.vbscriptVariableHover(parsed, firstLocalOffset, "", true, "en")
	secondHover := server.vbscriptVariableHover(parsed, secondLocalOffset, "", true, "en")
	if firstHover == nil || !strings.Contains(mustJSONForTest(t, firstHover), "local As First") {
		t.Fatalf("First.Render local hover = %#v", firstHover)
	}
	if secondHover == nil || !strings.Contains(mustJSONForTest(t, secondHover), "local As Second") {
		t.Fatalf("Second.Render local hover = %#v", secondHover)
	}

	firstParamOffset := strings.Index(source, "Render(value)") + len("Render(")
	secondParamOffset := strings.Index(source, "Render(value)") + len("Render(")
	secondParamOffset = strings.Index(source[secondParamOffset+1:], "Render(value)") + secondParamOffset + 1 + len("Render(")
	firstParamHover := vbscriptParameterDeclarationHover(parsed, firstParamOffset, "en")
	secondParamHover := vbscriptParameterDeclarationHover(parsed, secondParamOffset, "en")
	if firstParamHover == nil || !strings.Contains(mustJSONForTest(t, firstParamHover), "value As String") {
		t.Fatalf("First.Render parameter hover = %#v", firstParamHover)
	}
	if secondParamHover == nil || !strings.Contains(mustJSONForTest(t, secondParamHover), "value As Number") {
		t.Fatalf("Second.Render parameter hover = %#v", secondParamHover)
	}

	diagnostics := server.vbscriptTypeDiagnostics(parsed)
	serialized := mustJSONText(t, diagnostics)
	if strings.Count(serialized, "missingMember") < 2 || !strings.Contains(serialized, "StringMember") || !strings.Contains(serialized, "NumberMember") {
		t.Fatalf("same-named class local diagnostics = %s", serialized)
	}
}

func TestVBScriptUnionMemberRequiresCompatibleContracts(t *testing.T) {
	member := func(kind, typeName string, minimum, maximum int) vbscriptTypedMember {
		return vbscriptTypedMember{
			Name:                  "run",
			TypeName:              typeName,
			Kind:                  kind,
			ParameterCount:        maximum,
			ChecksArgumentCount:   true,
			MinimumParameterCount: minimum,
			MaximumParameterCount: maximum,
			ParameterRangeKnown:   true,
		}
	}
	tests := []struct {
		name   string
		second vbscriptTypedMember
		safe   bool
	}{
		{name: "same contract", second: member("method", "String", 1, 2), safe: true},
		{name: "different kind", second: member("property", "String", 1, 2)},
		{name: "disjoint parameter range", second: member("method", "String", 3, 4)},
		{name: "incompatible return", second: member("method", "Number", 1, 2)},
	}
	for _, test := range tests {
		t.Run(test.name, func(t *testing.T) {
			info := vbscriptTypeInfo{members: map[string]map[string]vbscriptTypedMember{
				"first":  {"run": member("method", "String", 1, 2)},
				"second": {"run": test.second},
			}}
			resolved, checked := vbscriptUnionMember("First | Second", "run", info)
			if checked != true || (resolved != nil) != test.safe {
				t.Fatalf("union contract resolution = (%#v, %t), safe=%t", resolved, checked, test.safe)
			}
			common, ok := vbscriptCommonTypedMembers([]string{"First", "Second"}, info.members)
			if !ok {
				t.Fatalf("common member intersection unexpectedly failed")
			}
			_, exposed := common["run"]
			if exposed != test.safe {
				t.Fatalf("union completion contract exposure = %t, safe=%t: %#v", exposed, test.safe, common)
			}
		})
	}
}

func TestVBScriptUnionMemberPreservesIntersectedArgumentRange(t *testing.T) {
	member := func(minimum, maximum int) vbscriptTypedMember {
		return vbscriptTypedMember{
			Name:                  "run",
			TypeName:              "String",
			Kind:                  "method",
			ParameterCount:        maximum,
			ChecksArgumentCount:   true,
			MinimumParameterCount: minimum,
			MaximumParameterCount: maximum,
			ParameterRangeKnown:   true,
		}
	}
	info := vbscriptTypeInfo{members: map[string]map[string]vbscriptTypedMember{
		"first":  {"run": member(0, 2)},
		"second": {"run": member(1, 3)},
	}}
	resolved, checked := vbscriptUnionMember("First | Second", "run", info)
	if !checked || resolved == nil {
		t.Fatalf("overlapping union member range was rejected: member=%#v checked=%t", resolved, checked)
	}
	if resolved.MinimumParameterCount != 1 || resolved.MaximumParameterCount != 2 {
		t.Fatalf("union member range = %d..%d, want 1..2: %#v", resolved.MinimumParameterCount, resolved.MaximumParameterCount, resolved)
	}
	common, ok := vbscriptCommonTypedMembers([]string{"First", "Second"}, info.members)
	if !ok || common["run"].MinimumParameterCount != 1 || common["run"].MaximumParameterCount != 2 {
		t.Fatalf("union completion member range was not intersected: ok=%t common=%#v", ok, common)
	}
}

func TestVBScriptUnionMemberReturnTypesUseDeterministicCommonType(t *testing.T) {
	member := func(returnType string) vbscriptTypedMember {
		return vbscriptTypedMember{Name: "run", TypeName: returnType, Kind: "method"}
	}
	tests := []struct {
		name       string
		typeNames  []string
		members    map[string]vbscriptTypedMember
		wantReturn string
	}{
		{
			name:       "broad string subsumes literal",
			typeNames:  []string{"First", "Second"},
			members:    map[string]vbscriptTypedMember{"first": member("String"), "second": member(`"x"`)},
			wantReturn: "String",
		},
		{
			name:       "literal arms form stable union",
			typeNames:  []string{"Second", "First", "Third"},
			members:    map[string]vbscriptTypedMember{"first": member(`"b"`), "second": member(`"a"`), "third": member(`"c"`)},
			wantReturn: `"a" | "b" | "c"`,
		},
	}
	for _, test := range tests {
		t.Run(test.name, func(t *testing.T) {
			info := vbscriptTypeInfo{members: map[string]map[string]vbscriptTypedMember{}}
			for name, value := range test.members {
				info.members[name] = map[string]vbscriptTypedMember{"run": value}
			}
			resolved, checked := vbscriptUnionMember(strings.Join(test.typeNames, " | "), "run", info)
			if !checked || resolved == nil || resolved.TypeName != test.wantReturn {
				t.Fatalf("union return = (%#v, %t), want %q", resolved, checked, test.wantReturn)
			}
			common, ok := vbscriptCommonTypedMembers(test.typeNames, info.members)
			if !ok || common["run"].TypeName != test.wantReturn {
				t.Fatalf("completion common return = (%#v, %t), want %q", common, ok, test.wantReturn)
			}
		})
	}
	permutations := []string{
		"First | Second | Third",
		"Third | First | Second",
		"Second | Third | First",
	}
	info := vbscriptTypeInfo{members: map[string]map[string]vbscriptTypedMember{
		"first":  {"run": member(`"b"`)},
		"second": {"run": member(`"a"`)},
		"third":  {"run": member(`"c"`)},
	}}
	for _, typeName := range permutations {
		resolved, checked := vbscriptUnionMember(typeName, "run", info)
		if !checked || resolved == nil || resolved.TypeName != `"a" | "b" | "c"` {
			t.Fatalf("permuted union %q = (%#v, %t), want stable literal union", typeName, resolved, checked)
		}
	}
}

func TestVBScriptUnionMemberReturnTypeCompletionUsesCommonContract(t *testing.T) {
	const source = `<%
' @type value As First | Second
Dim value
value.
%>`
	parsed := core.ParseDocument("file:///site/union-return-completion.asp", source, core.Settings{DefaultLanguage: "VBScript"})
	server := New(strings.NewReader(""), io.Discard, io.Discard)
	server.settings.VBScriptComTypes = map[string]vbscriptComTypeSetting{
		"First": {Members: map[string]vbscriptComMemberSetting{
			"run": {Kind: "method", ReturnType: "String"},
		}},
		"Second": {Members: map[string]vbscriptComMemberSetting{
			"run": {Kind: "method", ReturnType: `"x"`},
		}},
	}
	items := server.vbscriptConfiguredMemberCompletions(parsed, strings.Index(source, "value.")+len("value."))
	if !completionHasLabel(items, "run") {
		t.Fatalf("common union member completion omitted run: %#v", items)
	}
}

func TestVBScriptStrictMemberArgumentDiagnosticsHonorOptionalRanges(t *testing.T) {
	source := `<%
Class Worker
  Public Sub Run(Optional first, ByVal second)
  End Sub
End Class
' @type worker As Worker
Dim worker
worker.Run()
worker.Run(1)
worker.Run(1, 2)
worker.Run(1, 2, 3)
%>`
	parsed := core.ParseDocument("file:///site/optional-member-range.asp", source, core.Settings{DefaultLanguage: "VBScript"})
	server := New(strings.NewReader(""), io.Discard, io.Discard)
	server.settings.VBScriptTypeChecking = "strict"
	info := server.vbscriptTypeInfo(parsed)
	member := info.members["worker"]["run"]
	if !member.ParameterRangeKnown || member.MinimumParameterCount != 1 || member.MaximumParameterCount != 2 {
		t.Fatalf("optional method range = %#v, want 1..2", member)
	}
	diagnostics := server.vbscriptTypeDiagnostics(parsed)
	var mismatchLines []int
	for _, diagnostic := range diagnostics {
		if diagnostic.Code == "argumentCountMismatch" {
			mismatchLines = append(mismatchLines, diagnostic.Range.Start.Line)
		}
	}
	if len(mismatchLines) != 2 || mismatchLines[0] != 7 || mismatchLines[1] != 10 {
		t.Fatalf("optional member mismatch lines = %#v, diagnostics=%#v", mismatchLines, diagnostics)
	}
}

func TestVBScriptUnsafeUnionCallAndCompletionHideFirstArmContract(t *testing.T) {
	memberSetting := func(returnType string) vbscriptComMemberSetting {
		return vbscriptComMemberSetting{
			Kind:       "method",
			ReturnType: returnType,
			Parameters: []vbscriptComParameterSetting{{Name: "value", Type: "String"}},
		}
	}
	settings := map[string]vbscriptComTypeSetting{
		"First":  {Members: map[string]vbscriptComMemberSetting{"run": memberSetting("String")}},
		"Second": {Members: map[string]vbscriptComMemberSetting{"run": memberSetting("Number")}},
	}
	source := `<%
' @type value As First | Second
Dim value
value.run("x")
value.
%>`
	parsed := core.ParseDocument("file:///site/unsafe-union-contract.asp", source, core.Settings{DefaultLanguage: "VBScript"})
	server := New(strings.NewReader(""), io.Discard, io.Discard)
	server.settings.VBScriptComTypes = settings
	server.settings.VBScriptTypeChecking = "strict"
	diagnostics := server.vbscriptTypeDiagnostics(parsed)
	serialized := mustJSONText(t, diagnostics)
	if !strings.Contains(serialized, "unknownCall") || strings.Contains(serialized, "argumentCountMismatch") {
		t.Fatalf("unsafe union call exposed a first-arm contract: %s", serialized)
	}
	items := server.vbscriptConfiguredMemberCompletions(parsed, strings.LastIndex(source, "value.")+len("value."))
	for _, item := range items {
		if strings.EqualFold(item.Label, "run") {
			t.Fatalf("unsafe union completion exposed incompatible run contract: %#v", items)
		}
	}
}

func TestVBScriptTypeInfoInfersConstDeclarationTypesFromExpressions(t *testing.T) {
	source := `<%
Function MakeTitle()
  MakeTitle = "ok"
End Function
Const TextValue = "hello"
Const NumericValue = 10
Const DateValue = #2026-06-01#
Const BooleanValue = True
Const BuiltinValue = adInteger
Const StringBuiltinValue = vbCrLf
Const NumericBuiltinValue = vbOKOnly
Const FunctionValue = MakeTitle()
Const UnknownValue = MissingValue
%>`
	parsed := core.ParseDocument("file:///site/default.asp", source, core.Settings{DefaultLanguage: "VBScript"})
	analysis := graphAnalysisTypes(parsed)
	declarations := graphVBDeclarations(parsed)

	for name, expected := range map[string]string{
		"TextValue":           "String",
		"NumericValue":        "Number",
		"DateValue":           "Date",
		"BooleanValue":        "Boolean",
		"BuiltinValue":        "Number",
		"StringBuiltinValue":  "String",
		"NumericBuiltinValue": "Number",
		"FunctionValue":       "String",
		"UnknownValue":        "Variant",
	} {
		declaration := requireDeclaration(t, declarations, name)
		node := graphNodeForVBDeclaration(parsed, declaration, graphDeclarationNodeID(parsed.URI, declaration.Name, declaration.Range), &analysis)
		if node.TypeName != expected {
			t.Fatalf("%s type = %q, want %q: %#v", name, node.TypeName, expected, node)
		}
	}
}

func TestVBScriptStrictTypeDiagnosticsReportUnknownCallsAndLegacyArgumentCode(t *testing.T) {
	parsed := core.ParseDocument("file:///site/default.asp", `<%
Function knownCall(first, second)
End Function
knownCall(1)
missingCall()
DynamicCall()
%>`, core.Settings{DefaultLanguage: "VBScript"})
	server := &Server{settings: serverSettings{VBScriptTypeChecking: "strict"}}
	diagnostics := server.vbscriptTypeDiagnostics(parsed)
	foundArgumentCount := false
	foundUnknown := false
	for _, diagnostic := range diagnostics {
		switch diagnostic.Code {
		case "argumentCountMismatch":
			foundArgumentCount = strings.Contains(diagnostic.Message, "knownCall")
		case "unknownCall":
			foundUnknown = strings.Contains(diagnostic.Message, "missingCall")
			if strings.Contains(diagnostic.Message, "DynamicCall") {
				t.Fatalf("likely dynamic call should not be diagnosed: %#v", diagnostic)
			}
		}
	}
	if !foundArgumentCount || !foundUnknown {
		t.Fatalf("strict call diagnostics missing argumentCount=%t unknown=%t: %#v", foundArgumentCount, foundUnknown, diagnostics)
	}
}

func requireScopedDeclaration(t *testing.T, declarations []vbUsageDeclaration, name string, scope string) vbUsageDeclaration {
	t.Helper()
	for _, declaration := range declarations {
		if strings.EqualFold(declaration.Name, name) && strings.EqualFold(declaration.Scope, scope) {
			return declaration
		}
	}
	t.Fatalf("%s in scope %q missing from %#v", name, scope, declarationNames(declarations))
	return vbUsageDeclaration{}
}
