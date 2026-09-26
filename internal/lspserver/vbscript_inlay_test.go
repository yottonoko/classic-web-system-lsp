package lspserver

import (
	"context"
	"encoding/json"
	"strings"
	"testing"

	"github.com/yottonoko/classic-web-system-lsp/internal/core"
	"github.com/yottonoko/classic-web-system-lsp/internal/lsp"
)

func TestVBScriptVariableInlaySuppressesIncludeImplicitGlobalDuplicate(t *testing.T) {
	owner := core.ParseDocument("file:///tmp/default.asp", `<!-- #include file="shared.inc" -->
<%
Response.Write a
a = 2
%>`, core.Settings{DefaultLanguage: "VBScript"})
	included := core.ParseDocument("file:///tmp/shared.inc", `<%
a = 1
%>`, core.Settings{DefaultLanguage: "VBScript"})
	hints := vbscriptVariableTypeInlayHints(owner, lsp.Range{
		Start: lsp.Position{Line: 0, Character: 0},
		End:   lsp.Position{Line: 5, Character: 0},
	}, vbscriptVariableTypeInlayOptions{
		VariableTypes:       true,
		ScopeMarkers:        inlayScopeMarkerSettings{Global: true, Local: true, Uncertain: true},
		IncludeAware:        true,
		IncludedGlobalNames: includedVBVariableInlayGlobalNames([]*core.ParsedDocument{included}),
	})
	serialized, err := json.Marshal(hints)
	if err != nil {
		t.Fatal(err)
	}
	if strings.Contains(string(serialized), "(global) As Number") {
		t.Fatalf("expected include duplicate assignment to be suppressed, got %s", serialized)
	}
	if strings.Contains(string(serialized), "(?)") {
		t.Fatalf("expected include-aware hints to avoid uncertain marker, got %s", serialized)
	}
}

func TestVBScriptVariableInlayUsesUncertainMarkersOnlyBeforeIncludeAwareAnalysis(t *testing.T) {
	includeSource := `<%
a = 1
sharedTitle = "include"
Sub Render()
  b = "local"
End Sub
%>`
	include := core.ParseDocument("file:///tmp/shared.inc", includeSource, core.Settings{DefaultLanguage: "VBScript"})
	includeHints := vbscriptVariableTypeInlayHints(include, fullDocumentRange(includeSource), vbscriptVariableTypeInlayOptions{
		VariableTypes: true,
		ScopeMarkers:  inlayScopeMarkerSettings{Global: true, Local: true, Uncertain: true},
		IncludeAware:  true,
	})
	includeText := mustJSONForTest(t, includeHints)
	if countInlayTestOccurrences(includeText, `"label":" (global) As 1"`) != 1 || !strings.Contains(includeText, `"label":" (global) As \"include\""`) || !strings.Contains(includeText, `"label":" (global) As \"local\""`) || strings.Contains(includeText, `"label":" (local)`) {
		t.Fatalf("include implicit global hints mismatch: %s", includeText)
	}
	if strings.Contains(includeText, "(?)") {
		t.Fatalf("include hints should be scope-aware and certain: %s", includeText)
	}

	pageSource := `<!-- #include file="shared.inc" -->
<%
Response.Write sharedTitle
a = 1
Sub Render()
  b = "page local"
End Sub
Response.Write b
%>`
	page := core.ParseDocument("file:///tmp/default.asp", pageSource, core.Settings{DefaultLanguage: "VBScript"})
	pageHints := vbscriptVariableTypeInlayHints(page, fullDocumentRange(pageSource), vbscriptVariableTypeInlayOptions{
		VariableTypes: true,
		ScopeMarkers:  inlayScopeMarkerSettings{Global: true, Local: true, Uncertain: true},
	})
	pageText := mustJSONForTest(t, pageHints)
	if !strings.Contains(pageText, "(?) As 1") || !strings.Contains(pageText, "(?) As Variant") || !strings.Contains(pageText, `(?) As \"page local\"`) {
		t.Fatalf("page hints before include-aware analysis should be uncertain: %s", pageText)
	}
	if strings.Contains(pageText, "(global) As Number") {
		t.Fatalf("page hints before include-aware analysis should avoid global marker: %s", pageText)
	}

	includeAwareHints := vbscriptVariableTypeInlayHints(page, fullDocumentRange(pageSource), vbscriptVariableTypeInlayOptions{
		VariableTypes:       true,
		ScopeMarkers:        inlayScopeMarkerSettings{Global: true, Local: true, Uncertain: true},
		IncludeAware:        true,
		IncludedGlobalNames: includedVBVariableInlayGlobalNames([]*core.ParsedDocument{include}),
	})
	includeAwareText := mustJSONForTest(t, includeAwareHints)
	if len(includeAwareHints) != 0 {
		t.Fatalf("include-aware hints should suppress duplicate include globals: %s", includeAwareText)
	}
}

func TestInferVBValueTypeNewClass(t *testing.T) {
	if got := inferVBValueType("New Customer"); got != "Customer" {
		t.Fatalf("inferVBValueType(New Customer) = %q", got)
	}
}

func TestVBScriptLiteralInferenceDoesNotHideDynamicAssignments(t *testing.T) {
	const source = `<%
Dim route
route = "home"
route = ResolveAtRuntime()
Response.Write route

Dim inverse
inverse = ResolveAtRuntime()
inverse = "known"

' @type annotated As "home" | "login"
Dim annotated
annotated = "home"
annotated = ResolveAtRuntime()

Dim branch
If condition Then
  branch = ResolveAtRuntime()
Else
  branch = "fallback"
End If
%>`
	parsed := core.ParseDocument("file:///tmp/dynamic-literal-inference.asp", source, core.Settings{DefaultLanguage: "VBScript"})
	declarations := variableInlayDeclarations(parsed, true, nil)
	byName := map[string]vbUsageDeclaration{}
	for _, declaration := range declarations {
		byName[strings.ToLower(declaration.Name)] = declaration
	}
	for _, name := range []string{"route", "inverse", "branch"} {
		declaration, ok := byName[name]
		if !ok {
			t.Fatalf("%s declaration missing: %#v", name, declarations)
		}
		if got := inferVBDeclarationType(parsed, declaration); got != "Variant" {
			t.Fatalf("%s inferred type = %q, want Variant", name, got)
		}
	}
	if got := inferVBDeclarationType(parsed, byName["annotated"]); got != `"home" | "login"` {
		t.Fatalf("explicit annotated type = %q, want literal union", got)
	}

	hints := vbscriptVariableTypeInlayHints(parsed, fullDocumentRange(source), vbscriptVariableTypeInlayOptions{VariableTypes: true})
	hintText := mustJSONForTest(t, hints)
	if strings.Contains(hintText, `"label":" As \"home\""`) || strings.Contains(hintText, `"label":" As \"known\""`) || strings.Contains(hintText, `"label":" As \"fallback\""`) {
		t.Fatalf("dynamic literal assignments leaked finite inlay types: %s", hintText)
	}
	if countInlayTestOccurrences(hintText, "As Variant") < 3 {
		t.Fatalf("dynamic literal inlay hints missing Variant fallbacks: %s", hintText)
	}

	server := &Server{}
	declarationOffset := strings.Index(source, "route")
	declarationHover := server.vbscriptVariableHover(parsed, declarationOffset, "", true, "en")
	if declarationHover == nil || !strings.Contains(mustJSONForTest(t, declarationHover), "route As Variant") {
		t.Fatalf("dynamic declaration hover = %#v, want Variant", declarationHover)
	}
	referenceOffset := strings.LastIndex(source, "route")
	referenceHover, handled := server.vbscriptVariableHoverAtOffsetContext(context.Background(), parsed, referenceOffset)
	if !handled || referenceHover == nil || !strings.Contains(mustJSONForTest(t, referenceHover), "route As Variant") {
		t.Fatalf("dynamic reference hover = %#v, handled=%t, want Variant", referenceHover, handled)
	}

	graphDeclaration, ok := byName["route"]
	if !ok {
		t.Fatal("route declaration missing for graph node")
	}
	graphNode := graphNodeForVBDeclaration(parsed, graphDeclaration, graphDeclarationNodeID(parsed.URI, graphDeclaration.Name, graphDeclaration.Range), nil)
	if graphNode.TypeName != "Variant" {
		t.Fatalf("dynamic graph node type = %q, want Variant: %#v", graphNode.TypeName, graphNode)
	}

	objectSource := `<%
Class RouteObject
  Public OnlyKnown
End Class
Dim objectRoute
Set objectRoute = New RouteObject
Set objectRoute = ResolveAtRuntime()
objectRoute.
%>`
	objectParsed := core.ParseDocument("file:///tmp/dynamic-object-inference.asp", objectSource, core.Settings{DefaultLanguage: "VBScript"})
	completionOffset := strings.Index(objectSource, "objectRoute.") + len("objectRoute.")
	if items := server.vbscriptTypedMemberCompletions(objectParsed, "objectRoute", completionOffset); len(items) != 0 {
		t.Fatalf("dynamic object completion retained known members: %#v", items)
	}
}

func fullDocumentRange(source string) lsp.Range {
	return lsp.Range{
		Start: lsp.Position{Line: 0, Character: 0},
		End:   core.NewTextDocument("file:///tmp/document.asp", "classic-asp", 0, source).PositionAt(len(source)),
	}
}

func mustJSONForTest(t *testing.T, value any) string {
	t.Helper()
	serialized, err := json.Marshal(value)
	if err != nil {
		t.Fatal(err)
	}
	return string(serialized)
}

func countInlayTestOccurrences(value string, substr string) int {
	count := 0
	for {
		index := strings.Index(value, substr)
		if index < 0 {
			return count
		}
		count++
		value = value[index+len(substr):]
	}
}
