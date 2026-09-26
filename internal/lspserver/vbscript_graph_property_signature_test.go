package lspserver

import (
	"reflect"
	"strings"
	"testing"

	"github.com/yottonoko/classic-web-system-lsp/internal/core"
)

func TestGraphPropertyAccessorSignaturesRemainDistinct(t *testing.T) {
	source := `<%
Class Widget
  ' @returns String
  Public Property Get Value()
    Value = "value"
  End Property
  ' @param Value.scalar As Number
  Public Property Let Value(scalar)
  End Property
  ' @param Value.object As Widget
  Public Property Set Value(object)
  End Property
End Class
%>`
	parsed := core.ParseDocument("file:///site/property-signatures.asp", source, core.Settings{DefaultLanguage: "VBScript"})
	analysis := graphAnalysisTypes(parsed)
	if _, ok := analysis.ScopedSignatures[graphSignatureKey("Widget", "Value")]; ok {
		t.Fatalf("property accessors were collapsed under owner/name: %#v", analysis.ScopedSignatures)
	}
	for _, accessor := range []string{"get", "let", "set"} {
		if _, ok := analysis.ScopedSignatures[graphSignatureKey("Widget", "Value", accessor)]; !ok {
			t.Fatalf("missing Widget.Value#%s signature: %#v", accessor, analysis.ScopedSignatures)
		}
	}
	if got := analysis.ScopedReturns[graphSignatureKey("Widget", "Value", "get")]; got != "String" {
		t.Fatalf("Widget.Value#get return = %q, want String: %#v", got, analysis.ScopedReturns)
	}
	if got := analysis.ScopedParams[graphSignatureKey("Widget", "Value", "let")]["scalar"]; got != "Number" {
		t.Fatalf("Widget.Value#let scalar = %q, want Number", got)
	}
	if got := analysis.ScopedParams[graphSignatureKey("Widget", "Value", "set")]["object"]; got != "Widget" {
		t.Fatalf("Widget.Value#set object = %q, want Widget", got)
	}

	declarations := graphVBDeclarations(parsed)
	want := map[string]struct {
		procedureKind string
		typeName      string
		parameter     string
		parameterType string
	}{
		"property-get": {procedureKind: "property-get", typeName: "String"},
		"property-let": {procedureKind: "property-let", parameter: "scalar", parameterType: "Number"},
		"property-set": {procedureKind: "property-set", parameter: "object", parameterType: "Widget"},
	}
	seen := map[string]bool{}
	for _, declaration := range declarations {
		if declaration.Kind != "property" || !strings.EqualFold(declaration.MemberOf, "Widget") || !strings.EqualFold(declaration.Name, "Value") {
			continue
		}
		node := graphNodeForVBDeclaration(parsed, declaration, graphDeclarationNodeID(parsed.URI, declaration.Name, declaration.Range), &analysis)
		wantNode, ok := want[strings.ToLower(declaration.ProcedureKind)]
		if !ok {
			t.Fatalf("unexpected Widget.Value declaration: %#v", declaration)
		}
		seen[strings.ToLower(declaration.ProcedureKind)] = true
		if node.ProcedureKind != wantNode.procedureKind || node.TypeName != wantNode.typeName {
			t.Fatalf("Widget.Value %s node = %#v, want kind=%s type=%s", declaration.ProcedureKind, node, wantNode.procedureKind, wantNode.typeName)
		}
		if wantNode.parameter == "" {
			if len(node.Parameters) != 0 {
				t.Fatalf("Widget.Value %s parameters contaminated: %#v", declaration.ProcedureKind, node.Parameters)
			}
			continue
		}
		if len(node.Parameters) != 1 || !strings.EqualFold(node.Parameters[0].Name, wantNode.parameter) || node.Parameters[0].TypeName != wantNode.parameterType {
			t.Fatalf("Widget.Value %s parameters = %#v, want %s As %s", declaration.ProcedureKind, node.Parameters, wantNode.parameter, wantNode.parameterType)
		}
	}
	if len(seen) != len(want) {
		t.Fatalf("Widget.Value accessor declarations = %#v, want all accessors", seen)
	}
	if first, second := graphSignatures(parsed), graphSignatures(parsed); !reflect.DeepEqual(first, second) {
		t.Fatalf("property signature output is nondeterministic: first=%#v second=%#v", first, second)
	}
}
