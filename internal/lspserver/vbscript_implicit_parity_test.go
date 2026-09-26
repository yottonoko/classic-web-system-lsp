package lspserver

import (
	"encoding/json"
	"strings"
	"testing"

	"github.com/yottonoko/classic-web-system-lsp/internal/core"
	"github.com/yottonoko/classic-web-system-lsp/internal/lsp"
	"github.com/yottonoko/classic-web-system-lsp/internal/vbscript"
)

func TestImplicitDeclarationsAreOptInAndSkipBuiltInsMembersAndFunctionReturns(t *testing.T) {
	source := `<%
implicitGlobal = "global"
Function BuildValue()
  nestedImplicit = implicitGlobal
  BuildValue = nestedImplicit
End Function
Class Widget
  Public Sub Save()
    methodLocal = "method"
  End Sub
End Class
Response.Write readOnlyGlobal
obj.Member = "member"
Response = "builtin"
CStr = "builtin"
%>`
	parsed := core.ParseDocument("file:///site/implicit.asp", source, core.Settings{DefaultLanguage: "VBScript"})

	defaultDeclarations := collectVBUsageDeclarations(parsed).Declarations
	for _, absent := range []string{"implicitGlobal", "nestedImplicit", "methodLocal", "readOnlyGlobal"} {
		if declarationByName(defaultDeclarations, absent) != nil {
			t.Fatalf("default declarations unexpectedly included %s: %#v", absent, declarationNames(defaultDeclarations))
		}
	}

	graphDeclarations := graphVBDeclarations(parsed)
	for _, expected := range []string{"implicitGlobal", "nestedImplicit", "methodLocal", "readOnlyGlobal"} {
		declaration := declarationByName(graphDeclarations, expected)
		if declaration == nil || !declaration.Implicit {
			t.Fatalf("implicit graph declaration %s mismatch: %#v in %#v", expected, declaration, declarationNames(graphDeclarations))
		}
	}
	for _, absent := range []string{"Member", "Response", "CStr", "BuildValue"} {
		declaration := declarationByName(graphDeclarations, absent)
		if declaration != nil && declaration.Implicit {
			t.Fatalf("implicit declarations should skip %s: %#v", absent, declaration)
		}
	}

	explicit := core.ParseDocument("file:///site/explicit.asp", `<%
Option Explicit
missingValue = 1
%>`, core.Settings{DefaultLanguage: "VBScript"})
	missing := declarationByName(graphVBDeclarations(explicit), "missingValue")
	if missing != nil {
		t.Fatalf("explicit document implicit graph declaration mismatch: %#v", missing)
	}
}

func TestStandaloneVBSSymbolIndexKeepsWScriptBuiltInsOutOfImplicitDeclarations(t *testing.T) {
	source := `value = WScript.ScriptName
Response = value
`
	parsed := core.ParseDocument("file:///site/script.vbs", source, core.Settings{DefaultLanguage: "VBScript"})

	index := vbscript.BuildSymbolIndex(parsed)
	for _, expected := range []string{"wscript", "scriptname", "response", "value"} {
		if occurrences := index.Occurrences[expected]; len(occurrences) == 0 {
			t.Fatalf("standalone VBS index missing reference %s: %#v", expected, index.Occurrences)
		}
	}

	graphDeclarations := graphVBDeclarations(parsed)
	response := declarationByName(graphDeclarations, "Response")
	if response == nil || !response.Implicit {
		t.Fatalf("standalone VBS Response implicit declaration mismatch: %#v in %#v", response, declarationNames(graphDeclarations))
	}
	if wscript := declarationByName(graphDeclarations, "WScript"); wscript != nil && wscript.Implicit {
		t.Fatalf("standalone VBS WScript should stay a built-in, got %#v", wscript)
	}
}

func TestImplicitAssignmentsIncludeSingleLineIfBranches(t *testing.T) {
	source := `<%
If enabled Then oneLineValue = 1
If enabled Then branchValue = 2 Else fallbackValue = "fallback"
If enabled Then Let letValue = 3
If enabled Then _
  continuedValue = 4
Sub Render()
  If enabled Then localValue = "local"
End Sub
Class Widget
End Class
If enabled Then Set objectValue = New Widget
%>`
	parsed := core.ParseDocument("file:///site/single-line-if-implicit-index.asp", source, core.Settings{DefaultLanguage: "VBScript"})

	inlayDeclarations := implicitAssignmentInlayDeclarations(parsed, true, nil)
	for _, expected := range []string{
		"oneLineValue",
		"branchValue",
		"fallbackValue",
		"letValue",
		"continuedValue",
		"localValue",
		"objectValue",
	} {
		if declarationByName(inlayDeclarations, expected) == nil {
			t.Fatalf("implicit assignment declarations missing %s: %#v", expected, declarationNames(inlayDeclarations))
		}
	}
	if local := declarationByName(inlayDeclarations, "localValue"); local == nil || local.Local || local.Scope != "" {
		t.Fatalf("localValue implicit global declaration mismatch: %#v", local)
	}

	graphDeclarations := graphVBDeclarations(parsed)
	for _, expected := range []string{
		"oneLineValue",
		"branchValue",
		"fallbackValue",
		"letValue",
		"continuedValue",
		"localValue",
		"objectValue",
	} {
		declaration := declarationByName(graphDeclarations, expected)
		if declaration == nil || !declaration.Implicit || declaration.Local {
			t.Fatalf("graph implicit declaration %s mismatch: %#v in %#v", expected, declaration, declarationNames(graphDeclarations))
		}
	}

	server := &Server{}
	oneLineHover := server.vbscriptVariableHover(parsed, strings.Index(source, "oneLineValue"), "", true, "en")
	if oneLineHover == nil || !strings.Contains(implicitParityHoverValue(t, oneLineHover), "(global) Dim oneLineValue As Number") {
		t.Fatalf("oneLineValue hover mismatch: %#v", oneLineHover)
	}
	fallbackHover := server.vbscriptVariableHover(parsed, strings.Index(source, "fallbackValue"), "", true, "en")
	if fallbackHover == nil || !strings.Contains(implicitParityHoverValue(t, fallbackHover), "(global) Dim fallbackValue As String") {
		t.Fatalf("fallbackValue hover mismatch: %#v", fallbackHover)
	}

	hints := vbscriptVariableTypeInlayHints(parsed, fullImplicitParityDocumentRange(source), vbscriptVariableTypeInlayOptions{
		VariableTypes: true,
		ScopeMarkers:  inlayScopeMarkerSettings{Global: true, Local: true, Uncertain: true},
		IncludeAware:  true,
	})
	hintText := mustImplicitParityJSON(t, hints)
	for _, expected := range []string{
		`"label":" (global) As Number"`,
		`"label":" (global) As String"`,
	} {
		if !strings.Contains(hintText, expected) {
			t.Fatalf("single-line If inlay hints missing %q: %s", expected, hintText)
		}
	}
}

func TestDimlessProcedureAssignmentsShareOneScriptGlobal(t *testing.T) {
	source := `<%
Function A()
  If N = 1 Then
    A = N * 100
  Else
    A = 5
  End If
  N = 1
End Function

R = A()
M = A()

N = ""
%>`
	parsed := core.ParseDocument("file:///site/dimless-global.asp", source, core.Settings{DefaultLanguage: "VBScript"})
	firstProcedureRead := strings.Index(source, "N = 1")
	var matches []vbUsageDeclaration
	for _, declaration := range variableInlayDeclarations(parsed, true, nil) {
		if strings.EqualFold(declaration.Name, "N") {
			matches = append(matches, declaration)
		}
	}
	if len(matches) != 1 {
		t.Fatalf("implicit N declarations = %#v, want one script global", matches)
	}
	globalDeclaration := matches[0]
	if globalDeclaration.Local || globalDeclaration.Scope != "" || globalDeclaration.Start != firstProcedureRead {
		t.Fatalf("global N declaration mismatch: %#v, want start %d", globalDeclaration, firstProcedureRead)
	}
	if got := inferVBDeclarationType(parsed, globalDeclaration); got != "Number | String" {
		t.Fatalf("global N type = %q, want Number | String", got)
	}
}

func TestDimlessProcedureImplicitUsesFirstProcedureReference(t *testing.T) {
	source := `<%
Function A()
  If N = 1 Then
    A = N * 100
  Else
    A = 5
  End If
  N = 1
End Function

R = A()
M = A()
%>`
	parsed := core.ParseDocument("file:///site/dimless-unresolved-global.asp", source, core.Settings{DefaultLanguage: "VBScript"})
	firstReference := strings.Index(source, "N = 1")

	declaration := declarationByName(variableInlayDeclarations(parsed, true, nil), "N")
	if declaration == nil {
		t.Fatalf("implicit N declaration missing")
	}
	if declaration.Local || declaration.Scope != "" || declaration.Start != firstReference {
		t.Fatalf("implicit N representative mismatch: %#v, want start %d from procedure A", declaration, firstReference)
	}
	if got := inferVBDeclarationType(parsed, *declaration); got != "1" {
		t.Fatalf("implicit N type = %q, want 1", got)
	}
}

func TestProcedureAssignmentAndSetTargetsBecomeScriptGlobals(t *testing.T) {
	source := `<%
Class Widget
End Class
Function Build()
  scalarValue = 1
  Set objectValue = New Widget
End Function
%>`
	parsed := core.ParseDocument("file:///site/procedure-assignments.asp", source, core.Settings{DefaultLanguage: "VBScript"})

	for _, name := range []string{"scalarValue", "objectValue"} {
		declaration := declarationByName(variableInlayDeclarations(parsed, true, nil), name)
		if declaration == nil || !declaration.Implicit || declaration.Local || declaration.Scope != "" {
			t.Fatalf("%s implicit global mismatch: %#v", name, declaration)
		}
	}
}

func TestExplicitProcedureLocalsAndParametersSuppressDimlessImplicitGlobals(t *testing.T) {
	source := `<%
Function ExplicitLocal()
  Dim N
  N = 1
End Function
Function ExplicitParameter(N)
  N = 2
End Function
%>`
	parsed := core.ParseDocument("file:///site/dimless-local-guards.asp", source, core.Settings{DefaultLanguage: "VBScript"})

	if declaration := declarationByName(variableInlayDeclarations(parsed, true, nil), "N"); declaration == nil || !declaration.Local || !strings.EqualFold(declaration.Scope, "ExplicitLocal") || declaration.Implicit {
		t.Fatalf("explicit local declaration mismatch: %#v", declaration)
	}
	for _, declaration := range variableInlayDeclarations(parsed, true, nil) {
		if strings.EqualFold(declaration.Name, "N") && declaration.Implicit {
			t.Fatalf("explicit local/parameter should suppress implicit N: %#v", declaration)
		}
	}
}

func declarationByName(declarations []vbUsageDeclaration, name string) *vbUsageDeclaration {
	for i := range declarations {
		if strings.EqualFold(declarations[i].Name, name) {
			return &declarations[i]
		}
	}
	return nil
}

func declarationNames(declarations []vbUsageDeclaration) []string {
	names := make([]string, 0, len(declarations))
	for _, declaration := range declarations {
		names = append(names, declaration.Name)
	}
	return names
}

func fullImplicitParityDocumentRange(source string) lsp.Range {
	return lsp.Range{
		Start: lsp.Position{Line: 0, Character: 0},
		End:   core.NewTextDocument("file:///site/document.asp", "classic-asp", 0, source).PositionAt(len(source)),
	}
}

func mustImplicitParityJSON(t *testing.T, value any) string {
	t.Helper()
	serialized, err := json.Marshal(value)
	if err != nil {
		t.Fatal(err)
	}
	return string(serialized)
}

func implicitParityHoverValue(t *testing.T, hover *lsp.Hover) string {
	t.Helper()
	content, ok := hover.Contents.(lsp.MarkupContent)
	if !ok {
		t.Fatalf("hover contents type = %T, want lsp.MarkupContent", hover.Contents)
	}
	return content.Value
}
