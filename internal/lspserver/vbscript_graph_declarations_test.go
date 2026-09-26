package lspserver

import (
	"strings"
	"testing"

	"github.com/yottonoko/classic-web-system-lsp/internal/core"
)

func TestGraphDeclarationsTreatProcedureReDimAsVisibleGlobalResize(t *testing.T) {
	source := `<%
Dim globalItems
ReDim pageItems(10)
Sub Resize()
  ReDim Preserve globalItems(2)
  globalItems(0) = "value"
End Sub
%>`
	parsed := core.ParseDocument("file:///site/redim-global-symbol.asp", source, core.Settings{DefaultLanguage: "VBScript"})

	declarations := graphVBDeclarations(parsed)
	matches := declarationsByName(declarations, "globalItems")
	if len(matches) != 1 {
		t.Fatalf("globalItems declarations = %d, want 1: %#v", len(matches), declarationNames(declarations))
	}
	if matches[0].Local || matches[0].Scope != "" {
		t.Fatalf("globalItems should stay global after procedure ReDim: %#v", matches[0])
	}

	node := graphNodeForVBDeclaration(parsed, matches[0], graphDeclarationNodeID(parsed.URI, matches[0].Name, matches[0].Range), nil)
	if node.TypeName != "Array" || node.ArrayKind != "dynamic" || node.ArrayDimensions == nil {
		t.Fatalf("globalItems graph node missing dynamic array metadata: %#v", node)
	}
	if got := *node.ArrayDimensions; len(got) != 1 || got[0] != "2" {
		t.Fatalf("globalItems array dimensions = %#v, want [2]", got)
	}

	pageItems := requireDeclaration(t, declarations, "pageItems")
	if pageItems.Kind != "variable" || pageItems.Local || pageItems.Scope != "" {
		t.Fatalf("pageItems ReDim declaration scope mismatch: %#v", pageItems)
	}
	pageItemsNode := graphNodeForVBDeclaration(parsed, pageItems, graphDeclarationNodeID(parsed.URI, pageItems.Name, pageItems.Range), nil)
	if pageItemsNode.TypeName != "Array" || pageItemsNode.ArrayKind != "dynamic" || pageItemsNode.ArrayDimensions == nil {
		t.Fatalf("pageItems graph node missing dynamic array metadata: %#v", pageItemsNode)
	}
	if got := *pageItemsNode.ArrayDimensions; len(got) != 1 || got[0] != "10" {
		t.Fatalf("pageItems array dimensions = %#v, want [10]", got)
	}
}

func TestGraphDeclarationsCollectVBScriptDeclarationScopes(t *testing.T) {
	source := `<%
Dim topLevel
Class Customer
  Public Name
  Public Sub Save()
    Dim localValue
    For Each item In items
    Next
    For index = 1 To 3
    Next
  End Sub
End Class
%>`
	parsed := core.ParseDocument("file:///site/default.asp", source, core.Settings{DefaultLanguage: "VBScript"})
	declarations := graphVBDeclarations(parsed)

	topLevel := requireDeclaration(t, declarations, "topLevel")
	if topLevel.Kind != "variable" || topLevel.Local || topLevel.MemberOf != "" || topLevel.Scope != "" {
		t.Fatalf("topLevel declaration scope mismatch: %#v", topLevel)
	}
	field := requireDeclaration(t, declarations, "Name")
	if field.Kind != "field" || field.Local || field.MemberOf != "Customer" || field.Scope != "" {
		t.Fatalf("Name declaration scope mismatch: %#v", field)
	}
	local := requireDeclaration(t, declarations, "localValue")
	if local.Kind != "variable" || !local.Local || local.MemberOf != "" || !strings.EqualFold(local.Scope, "Customer.Save") {
		t.Fatalf("localValue declaration scope mismatch: %#v", local)
	}
	loopItem := requireDeclaration(t, declarations, "item")
	if loopItem.Kind != "variable" || !loopItem.Local || loopItem.MemberOf != "" || !strings.EqualFold(loopItem.Scope, "Customer.Save") {
		t.Fatalf("item declaration scope mismatch: %#v", loopItem)
	}
	loopIndex := requireDeclaration(t, declarations, "index")
	if loopIndex.Kind != "variable" || !loopIndex.Local || loopIndex.MemberOf != "" || !strings.EqualFold(loopIndex.Scope, "Customer.Save") {
		t.Fatalf("index declaration scope mismatch: %#v", loopIndex)
	}
}

func declarationsByName(declarations []vbUsageDeclaration, name string) []vbUsageDeclaration {
	matches := make([]vbUsageDeclaration, 0, 1)
	for _, declaration := range declarations {
		if strings.EqualFold(declaration.Name, name) {
			matches = append(matches, declaration)
		}
	}
	return matches
}

func requireDeclaration(t *testing.T, declarations []vbUsageDeclaration, name string) vbUsageDeclaration {
	t.Helper()
	matches := declarationsByName(declarations, name)
	if len(matches) != 1 {
		t.Fatalf("%s declarations = %d, want 1: %#v", name, len(matches), declarationNames(declarations))
	}
	return matches[0]
}
