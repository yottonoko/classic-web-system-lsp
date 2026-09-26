package lspserver

import (
	"context"
	"io"
	"strings"
	"testing"

	"github.com/yottonoko/classic-web-system-lsp/internal/core"
	"github.com/yottonoko/classic-web-system-lsp/internal/graph"
)

func TestConfiguredGraphRejectsIncompatibleParameterTypes(t *testing.T) {
	parsed := core.ParseDocument("file:///site/configured-parameter-types.asp", `<%
value.shared
%>`, core.Settings{DefaultLanguage: "VBScript"})
	server := New(strings.NewReader(""), io.Discard, io.Discard)
	server.settings.VBScriptGlobals = map[string]vbscriptGlobalSetting{"value": {Type: "First | Second"}}
	server.settings.VBScriptComTypes = map[string]vbscriptComTypeSetting{
		"First": {Members: map[string]vbscriptComMemberSetting{
			"shared": {Kind: "method", Parameters: []vbscriptComParameterSetting{{Name: "value", Type: "String"}}},
		}},
		"Second": {Members: map[string]vbscriptComMemberSetting{
			"shared": {Kind: "method", Parameters: []vbscriptComParameterSetting{{Name: "value", Type: "Number"}}},
		}},
	}

	nodes, edges := server.graphMemberReferencesWithProgress(context.Background(), []*core.ParsedDocument{parsed}, nil, "graph.test")
	for _, node := range nodes {
		if node.ExternalKind == "member" && (node.Label == "First.shared" || node.Label == "Second.shared") {
			t.Fatalf("incompatible configured member was linked: %#v", node)
		}
	}
	for _, edge := range edges {
		if strings.Contains(edge.Target, "configuredcomtype") {
			t.Fatalf("incompatible configured member edge was linked: %#v", edge)
		}
	}
}

func TestConfiguredGraphPreservesIdenticalParameterContractsAndMetadata(t *testing.T) {
	parameter := vbscriptComParameterSetting{Name: "input", Type: "String", Mode: "ByRef", Optional: true}
	server := New(strings.NewReader(""), io.Discard, io.Discard)
	server.settings.VBScriptComTypes = map[string]vbscriptComTypeSetting{
		"First": {Members: map[string]vbscriptComMemberSetting{
			"shared": {Kind: "method", ReturnType: "String", Parameters: []vbscriptComParameterSetting{parameter}},
		}},
		"Second": {Members: map[string]vbscriptComMemberSetting{
			"shared": {Kind: "method", ReturnType: "String", Parameters: []vbscriptComParameterSetting{parameter}},
		}},
	}

	nodes := server.configuredComMemberGraphNodes("First | Second", "shared")
	if len(nodes) != 2 {
		t.Fatalf("identical configured member contract nodes = %#v, want two nodes", nodes)
	}
	for _, node := range nodes {
		if len(node.Parameters) != 1 {
			t.Fatalf("configured graph node parameters = %#v, want one parameter: %#v", node.Parameters, node)
		}
		got := node.Parameters[0]
		if got.Name != "input" || got.TypeName != "String" || got.Mode != "byref" || !got.Optional {
			t.Fatalf("configured graph parameter = %#v, want input/byref/String/optional", got)
		}
	}
	common := configuredGraphComMembers(server.settings.VBScriptComTypes["First"])
	second := configuredGraphComMembers(server.settings.VBScriptComTypes["Second"])
	merged, ok := vbscriptCommonTypedMembers([]string{"First", "Second"}, map[string]map[string]vbscriptTypedMember{
		"first":  common,
		"second": second,
	})
	if !ok || len(merged["shared"].Parameters) != 1 || merged["shared"].Parameters[0].TypeName != "String" {
		t.Fatalf("identical configured member contract intersection = %#v, ok=%t", merged, ok)
	}

	var external graph.Node
	for _, candidate := range server.configuredGraphExternalNodes() {
		if candidate.Label == "First.shared" {
			external = candidate
			break
		}
	}
	if len(external.Parameters) != 1 || external.Parameters[0] != nodes[0].Parameters[0] {
		t.Fatalf("configured external graph node parameter metadata = %#v, want %#v", external.Parameters, nodes[0].Parameters)
	}
}

func TestConfiguredGraphRejectsParameterOptionalAndCountDifferences(t *testing.T) {
	tests := []struct {
		name   string
		first  []vbscriptComParameterSetting
		second []vbscriptComParameterSetting
	}{
		{
			name:   "optional difference",
			first:  []vbscriptComParameterSetting{{Name: "input", Type: "String"}},
			second: []vbscriptComParameterSetting{{Name: "input", Type: "String", Optional: true}},
		},
		{
			name:   "count difference",
			first:  []vbscriptComParameterSetting{{Name: "input", Type: "String"}},
			second: []vbscriptComParameterSetting{{Name: "input", Type: "String"}, {Name: "extra", Type: "String"}},
		},
	}
	for _, test := range tests {
		t.Run(test.name, func(t *testing.T) {
			server := New(strings.NewReader(""), io.Discard, io.Discard)
			server.settings.VBScriptComTypes = map[string]vbscriptComTypeSetting{
				"First":  {Members: map[string]vbscriptComMemberSetting{"shared": {Kind: "method", Parameters: test.first}}},
				"Second": {Members: map[string]vbscriptComMemberSetting{"shared": {Kind: "method", Parameters: test.second}}},
			}
			if nodes := server.configuredComMemberGraphNodes("First | Second", "shared"); len(nodes) != 0 {
				t.Fatalf("configured member with %s was exposed: %#v", test.name, nodes)
			}
		})
	}
}

func TestConfiguredGraphKeepsReturnUnionWithIdenticalParameters(t *testing.T) {
	server := New(strings.NewReader(""), io.Discard, io.Discard)
	parameter := vbscriptComParameterSetting{Name: "input", Type: "String"}
	server.settings.VBScriptComTypes = map[string]vbscriptComTypeSetting{
		"First": {Members: map[string]vbscriptComMemberSetting{
			"shared": {Kind: "method", ReturnType: `"ready"`, Parameters: []vbscriptComParameterSetting{parameter}},
		}},
		"Second": {Members: map[string]vbscriptComMemberSetting{
			"shared": {Kind: "method", ReturnType: `"done"`, Parameters: []vbscriptComParameterSetting{parameter}},
		}},
	}
	first := configuredGraphComMembers(server.settings.VBScriptComTypes["First"])
	second := configuredGraphComMembers(server.settings.VBScriptComTypes["Second"])
	common, ok := vbscriptCommonTypedMembers([]string{"First", "Second"}, map[string]map[string]vbscriptTypedMember{
		"first":  first,
		"second": second,
	})
	if !ok || common["shared"].TypeName != `"done" | "ready"` {
		t.Fatalf("configured return union = %#v, ok=%t, want %q", common, ok, `"done" | "ready"`)
	}
	if nodes := server.configuredComMemberGraphNodes("First | Second", "shared"); len(nodes) != 2 {
		t.Fatalf("return-union configured member nodes = %#v, want two nodes", nodes)
	}
}
