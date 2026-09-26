package lspserver

import (
	"path/filepath"
	"testing"

	"github.com/yottonoko/classic-web-system-lsp/internal/graph"
)

func TestStdioParityKeepsVBScriptProcedureScopeAfterBlockEndStatementsInGraph(t *testing.T) {
	client := startStdioTestClient(t)
	defer client.close()

	root := t.TempDir()
	uri := pathToFileURI(filepath.Join(root, "block-scope.asp"))
	source := `<%
Function BuildValue(ByVal input)
  Dim values
  If input Then
    values = input
  End If
  values = values & input
  values("total") = values("total") & input
  With values
  End With
  BuildValue = values
End Function
%>`
	client.request("initialize", map[string]any{
		"processId":    nil,
		"rootUri":      pathToFileURI(root),
		"capabilities": map[string]any{},
	})
	configureGraphSettingsTest(t, client, allVisibleGraphSettings())
	notifyOpenClassicASPDocument(t, client, uri, source)

	payload := buildDocumentGraph(t, client, uri, nil)
	responseJSON := mustJSONText(t, payload)
	buildValue := graphNodeByLabel(payload, "BuildValue")
	input := graphNodeByLabel(payload, "input")
	values := graphNodeByLabel(payload, "values")
	if buildValue == nil || input == nil || values == nil {
		t.Fatalf("graph missing scoped symbols: %s", responseJSON)
	}
	expectGraphNodeShape(t, payload, "BuildValue", graph.Node{DeclarationKind: "function", BindingScope: "global"})
	expectGraphNodeShape(t, payload, "input", graph.Node{DeclarationKind: "parameter", BindingScope: "local"})
	expectGraphNodeShape(t, payload, "values", graph.Node{DeclarationKind: "variable", BindingScope: "local"})
	for _, target := range []*graph.Node{input, values} {
		if !graphHasLinkBetween(payload, "declares", target, buildValue, "") {
			t.Fatalf("graph missing local declaration link for %s: %s", target.Label, responseJSON)
		}
		if !graphHasLinkBetween(payload, "references", buildValue, target, "read") {
			t.Fatalf("graph missing scoped read link for %s: %s", target.Label, responseJSON)
		}
	}
	if graphNodeMatching(payload, func(node graph.Node) bool {
		return node.Kind == "vbUnresolved" && (node.Label == "input" || node.Label == "values")
	}) != nil {
		t.Fatalf("graph treated scoped symbols as unresolved: %s", responseJSON)
	}
}

func TestStdioParityKeepsFunctionReturnAssignmentsOutOfGraphReferenceCounts(t *testing.T) {
	client := startStdioTestClient(t)
	defer client.close()

	root := t.TempDir()
	uri := pathToFileURI(filepath.Join(root, "return-assignment.asp"))
	source := `<%
Class Customer
End Class
Function MakeCustomer()
  Set MakeCustomer = New Customer
End Function
Function BuildValue(ByVal n)
  Let BuildValue = n
  BuildValue = BuildValue + n
End Function
result = MakeCustomer()
result = BuildValue(2)
%>`
	client.request("initialize", map[string]any{
		"processId":    nil,
		"rootUri":      pathToFileURI(root),
		"capabilities": map[string]any{},
	})
	configureGraphSettingsTest(t, client, allVisibleGraphSettings())
	notifyOpenClassicASPDocument(t, client, uri, source)

	payload := buildDocumentGraph(t, client, uri, nil)
	responseJSON := mustJSONText(t, payload)
	makeCustomer := graphNodeByLabel(payload, "MakeCustomer")
	buildValue := graphNodeByLabel(payload, "BuildValue")
	if makeCustomer == nil || buildValue == nil {
		t.Fatalf("graph missing functions: %s", responseJSON)
	}
	for _, functionNode := range []*graph.Node{makeCustomer, buildValue} {
		if graphHasLinkBetween(payload, "assignments", functionNode, functionNode, "write") {
			t.Fatalf("graph counted return assignment as write reference for %s: %s", functionNode.Label, responseJSON)
		}
	}
	if !graphHasLinkBetween(payload, "references", buildValue, buildValue, "read") {
		t.Fatalf("graph missing recursive function read reference: %s", responseJSON)
	}
}
