package lspserver

import (
	"context"
	"io"
	"reflect"
	"strings"
	"testing"

	"github.com/yottonoko/classic-web-system-lsp/internal/core"
	"github.com/yottonoko/classic-web-system-lsp/internal/graph"
)

func TestConfiguredGraphPreservesStructuredReturnUnionMetadata(t *testing.T) {
	server := New(strings.NewReader(""), io.Discard, io.Discard)
	server.settings.VBScriptGlobals = map[string]vbscriptGlobalSetting{
		"value": {Type: "First | Second"},
	}
	server.settings.VBScriptComTypes = map[string]vbscriptComTypeSetting{
		"First": {Members: map[string]vbscriptComMemberSetting{
			"Run": {Kind: "method", ReturnType: `"ready" | 200`},
		}},
	}

	var globalNode, memberNode *graph.Node
	for _, node := range server.configuredGraphExternalNodes() {
		copy := node
		switch node.Label {
		case "value":
			globalNode = &copy
		case "First.Run":
			memberNode = &copy
		}
	}
	if globalNode == nil || globalNode.TypeName != "First | Second" {
		t.Fatalf("configured global union metadata = %#v, want First | Second", globalNode)
	}
	if memberNode == nil || memberNode.TypeName != `"ready" | 200` {
		t.Fatalf("configured member return union metadata = %#v, want %q", memberNode, `"ready" | 200`)
	}

	parsed := core.ParseDocument("file:///site/return-union.asp", `<%
' @returns "ready" | 200
Function Build()
End Function
%>`, core.Settings{DefaultLanguage: "VBScript"})
	analysis := graphAnalysisTypes(parsed)
	var declaration vbUsageDeclaration
	for _, candidate := range graphVBDeclarations(parsed) {
		if strings.EqualFold(candidate.Name, "Build") {
			declaration = candidate
			break
		}
	}
	if declaration.Name == "" {
		t.Fatal("Build declaration missing")
	}
	node := graphNodeForVBDeclaration(parsed, declaration, graphDeclarationNodeID(parsed.URI, declaration.Name, declaration.Range), &analysis)
	if node.TypeName != `"ready" | 200` {
		t.Fatalf("source return union graph metadata = %q, want %q", node.TypeName, `"ready" | 200`)
	}
}

func TestConfiguredGraphLinksOnlyCommonCompatibleUnionMembers(t *testing.T) {
	parsed := core.ParseDocument("file:///site/configured-union-graph.asp", `<%
value.shared
value.shared
value.firstOnly
value.secondOnly
value.incompatible
literal.run
template.run
%>`, core.Settings{DefaultLanguage: "VBScript"})
	server := New(strings.NewReader(""), io.Discard, io.Discard)
	server.settings.VBScriptGlobals = map[string]vbscriptGlobalSetting{
		"value":    {Type: "First | Second"},
		"literal":  {Type: `"ready" | 200`},
		"template": {Type: "`page-${String}.asp`"},
	}
	server.settings.VBScriptComTypes = map[string]vbscriptComTypeSetting{
		"First": {Members: map[string]vbscriptComMemberSetting{
			"shared":       {Kind: "method", ReturnType: "String"},
			"firstOnly":    {Type: "String"},
			"incompatible": {Kind: "method", ReturnType: "String"},
			"run":          {Kind: "method", ReturnType: "String"},
		}},
		"Second": {Members: map[string]vbscriptComMemberSetting{
			"shared":       {Kind: "method", ReturnType: `"ready"`},
			"secondOnly":   {Type: "String"},
			"incompatible": {Kind: "method", ReturnType: "Number"},
			"run":          {Kind: "method", ReturnType: "String"},
		}},
	}

	nodes, edges := server.graphMemberReferencesWithProgress(context.Background(), []*core.ParsedDocument{parsed}, nil, "graph.test")
	if len(nodes) == 0 || len(edges) == 0 {
		t.Fatalf("configured union graph is empty: nodes=%#v edges=%#v", nodes, edges)
	}
	for _, label := range []string{"First.shared", "Second.shared"} {
		if count := configuredComUnionGraphNodeCount(nodes, label); count != 1 {
			t.Fatalf("configured union graph node %s count = %d, want 1: %#v", label, count, nodes)
		}
	}
	for _, label := range []string{"First.firstOnly", "Second.secondOnly", "First.incompatible", "Second.incompatible", "First.run", "Second.run"} {
		if count := configuredComUnionGraphNodeCount(nodes, label); count != 0 {
			t.Fatalf("unsafe configured union graph node %s count = %d, want 0: %#v", label, count, nodes)
		}
	}
	for _, label := range []string{"First.shared", "Second.shared"} {
		targets := configuredComUnionGraphNodeIDs(nodes, label)
		if len(targets) != 1 || configuredComUnionGraphEdgeCount(edges, targets[0]) != 2 {
			t.Fatalf("configured union graph edge target %s mismatch: targets=%v edges=%#v", label, targets, edges)
		}
	}

	secondNodes, secondEdges := server.graphMemberReferencesWithProgress(context.Background(), []*core.ParsedDocument{parsed}, nil, "graph.test")
	if !reflect.DeepEqual(nodes, secondNodes) || !reflect.DeepEqual(edges, secondEdges) {
		t.Fatalf("configured union graph output is not deterministic:\nfirst nodes=%#v edges=%#v\nsecond nodes=%#v edges=%#v", nodes, edges, secondNodes, secondEdges)
	}
}

func configuredComUnionGraphNodeCount(nodes []graph.Node, label string) int {
	count := 0
	for _, node := range nodes {
		if node.ExternalKind == "member" && node.Label == label {
			count++
		}
	}
	return count
}

func configuredComUnionGraphNodeIDs(nodes []graph.Node, label string) []string {
	ids := []string{}
	for _, node := range nodes {
		if node.ExternalKind == "member" && node.Label == label {
			ids = append(ids, node.ID)
		}
	}
	return ids
}

func configuredComUnionGraphEdgeCount(edges []graph.Edge, target string) int {
	count := 0
	for _, edge := range edges {
		if edge.Target == target && edge.Role == "member" {
			count += edge.Count
		}
	}
	return count
}
