package lspserver

import (
	"encoding/json"
	"reflect"
	"strings"
	"testing"

	"github.com/yottonoko/classic-web-system-lsp/internal/core"
	"github.com/yottonoko/classic-web-system-lsp/internal/lsp"
)

func TestFlowchartResponseWriteExposesTypedOutputFragmentsWithExactUTF16Ranges(t *testing.T) {
	source := `😀<%
Response.Write "<div class=""card"">Hello</div>"
Response.Write ".card { color: red; }"
Response.Write "const total = 1;"
Response.Write dynamicValue & "fallback"
Logger.Write "not response output"
%>`
	parsed := core.ParseDocument("file:///output.asp", source, core.Settings{DefaultLanguage: "VBScript"})
	nodes, _ := flowchartVBScriptNodes(parsed, nil, "raw", "en", 80)

	wants := []struct {
		text     string
		language string
	}{
		{text: `<div class="card">Hello</div>`, language: "html"},
		{text: `.card { color: red; }`, language: "css"},
		{text: `const total = 1;`, language: "javascript"},
		{text: `fallback`, language: "text"},
	}
	document := core.NewTextDocument(parsed.URI, "classic-asp", 0, source)
	for _, want := range wants {
		fragment := flowchartOutputFragmentContaining(nodes, want.text)
		if fragment == nil {
			t.Fatalf("missing output fragment %q in %#v", want.text, nodes)
		}
		if fragment["language"] != want.language {
			t.Fatalf("output fragment %q language = %#v, want %q", want.text, fragment["language"], want.language)
		}
		rangeValue, ok := fragment["range"].(lsp.Range)
		if !ok {
			t.Fatalf("output fragment %q range = %#v", want.text, fragment["range"])
		}
		encoded := want.text
		if want.language == "html" {
			encoded = strings.ReplaceAll(encoded, `"`, `""`)
		}
		start := strings.Index(source, encoded)
		if start < 0 {
			t.Fatalf("fixture missing encoded fragment %q", encoded)
		}
		if expected := document.Range(start, start+len(encoded)); rangeValue != expected {
			t.Fatalf("output fragment %q range = %#v, want %#v", want.text, rangeValue, expected)
		}
	}
	if fragment := flowchartOutputFragmentByText(nodes, "not response output"); fragment != nil {
		t.Fatalf("non-Response.Write call exposed output: %#v", fragment)
	}
}

func TestFlowchartGenerationIsDeterministicWithOutputFragments(t *testing.T) {
	source := `<%
Sub Render()
  If enabled Then
    Response.Write "<strong>ready</strong>"
  Else
    Response.Write "const ready = false;"
  End If
End Sub
%>`
	parsed := core.ParseDocument("file:///deterministic.asp", source, core.Settings{DefaultLanguage: "VBScript"})
	firstNodes, firstEdges := flowchartVBScriptNodes(parsed, nil, "raw", "en", 80)
	firstSections := flowchartSections(parsed)
	firstSections, firstNodes, firstEdges = flowchartAttachSectionMembership(firstSections, firstNodes, firstEdges)
	first, err := json.Marshal([]any{firstSections, firstNodes, firstEdges, flowchartMermaid(firstSections, firstNodes, firstEdges, 80)})
	if err != nil {
		t.Fatal(err)
	}
	for iteration := 0; iteration < 20; iteration++ {
		nodes, edges := flowchartVBScriptNodes(parsed, nil, "raw", "en", 80)
		sections := flowchartSections(parsed)
		sections, nodes, edges = flowchartAttachSectionMembership(sections, nodes, edges)
		current, marshalErr := json.Marshal([]any{sections, nodes, edges, flowchartMermaid(sections, nodes, edges, 80)})
		if marshalErr != nil {
			t.Fatal(marshalErr)
		}
		if !reflect.DeepEqual(current, first) {
			t.Fatalf("flowchart changed at iteration %d\nfirst: %s\ncurrent: %s", iteration, first, current)
		}
	}
}

func TestFlowchartStaticHTMLCSSAndJavaScriptOutputsStayInsideCFGBranches(t *testing.T) {
	source := `<%
If enabled Then
%><div><style>.card { color: red; }</style><script>const ready = true;</script></div><%
Else
%><p>disabled</p><%
End If
%>`
	parsed := core.ParseDocument("file:///static-output.asp", source, core.Settings{DefaultLanguage: "VBScript"})
	nodes, edges := flowchartVBScriptNodes(parsed, nil, "raw", "en", 80)
	thenOutput := flowchartOutputNodeContaining(nodes, ".card { color: red; }")
	elseOutput := flowchartOutputNodeContaining(nodes, "<p>disabled</p>")
	if thenOutput == nil || elseOutput == nil {
		t.Fatalf("static output nodes missing: %#v", nodes)
	}
	if thenOutput["kind"] != "output" || elseOutput["kind"] != "output" {
		t.Fatalf("static output kinds = %#v, %#v", thenOutput["kind"], elseOutput["kind"])
	}
	for _, want := range []struct {
		text, language string
	}{
		{text: `<div>`, language: "html"},
		{text: `.card { color: red; }`, language: "css"},
		{text: `const ready = true;`, language: "javascript"},
		{text: `</div>`, language: "html"},
	} {
		fragment := flowchartOutputFragmentContaining(nodes, want.text)
		if fragment == nil || fragment["language"] != want.language {
			t.Fatalf("static fragment %q = %#v, want language %q", want.text, fragment, want.language)
		}
	}
	if !flowchartHasIncomingEdgeLabel(edges, thenOutput["id"], "Yes") {
		t.Fatalf("then output is not connected to the Yes branch: nodes=%#v edges=%#v", nodes, edges)
	}
	if !flowchartHasIncomingEdgeFromKind(nodes, edges, elseOutput["id"], "else") {
		t.Fatalf("else output is not connected after Else: nodes=%#v edges=%#v", nodes, edges)
	}
}

func TestFlowchartStaticOnlyDocumentHasConnectedOutputFlow(t *testing.T) {
	source := `<main>Hello</main><style>main { color: red; }</style><script>console.log("ready")</script>`
	parsed := core.ParseDocument("file:///static-only.asp", source, core.Settings{DefaultLanguage: "VBScript"})
	nodes, edges := flowchartVBScriptNodes(parsed, nil, "raw", "en", 80)
	output := flowchartOutputNodeContaining(nodes, "Hello")
	if output == nil {
		t.Fatalf("static-only output node missing: %#v", nodes)
	}
	if !flowchartHasIncomingEdgeFromKind(nodes, edges, output["id"], "start") {
		t.Fatalf("static-only output has no start edge: nodes=%#v edges=%#v", nodes, edges)
	}
	if !flowchartHasOutgoingEdgeToKind(nodes, edges, output["id"], "end") {
		t.Fatalf("static-only output has no end edge: nodes=%#v edges=%#v", nodes, edges)
	}
}

func TestFlowchartASPExpressionIsImplicitResponseOutput(t *testing.T) {
	source := `<% If enabled Then %><h1><%= title %></h1><% End If %>`
	parsed := core.ParseDocument("file:///expression.asp", source, core.Settings{DefaultLanguage: "VBScript"})
	nodes, edges := flowchartVBScriptNodes(parsed, nil, "raw", "en", 80)
	expression := flowchartOutputNodeContaining(nodes, "title")
	if expression == nil || expression["label"] != "Render expression" {
		t.Fatalf("ASP expression output missing: %#v", nodes)
	}
	fragment := flowchartOutputFragmentContaining(nodes, "title")
	if fragment == nil || fragment["language"] != "text" || fragment["text"] != "title" {
		t.Fatalf("ASP expression fragment = %#v", fragment)
	}
	if !flowchartHasIncomingEdgeFromKind(nodes, edges, expression["id"], "output") {
		t.Fatalf("ASP expression did not stay after surrounding HTML output: nodes=%#v edges=%#v", nodes, edges)
	}
}

func TestFlowchartProcedureSectionsUseProtocolKind(t *testing.T) {
	parsed := core.ParseDocument("file:///sections.asp", `<% Sub Render(): End Sub : Function Value(): End Function %>`, core.Settings{DefaultLanguage: "VBScript"})
	sections := flowchartSections(parsed)
	if len(sections) != 2 {
		t.Fatalf("sections = %#v", sections)
	}
	for _, section := range sections {
		if section["kind"] != "procedure" {
			t.Fatalf("section kind = %#v, want procedure", section["kind"])
		}
	}
}

func TestFlowchartResolvedLinksCarryStableIDsAndExactTargetRanges(t *testing.T) {
	targetRange := lsp.Range{Start: lsp.Position{Line: 7, Character: 2}, End: lsp.Position{Line: 9, Character: 8}}
	nameRange := lsp.Range{Start: lsp.Position{Line: 7, Character: 6}, End: lsp.Position{Line: 7, Character: 12}}
	links := flowchartExpressionLinks("RenderCard", map[string]flowchartSymbolTarget{
		"rendercard": {Label: "Sub RenderCard", URI: "file:///include.inc", Range: targetRange, NameRange: nameRange},
	}, &flowchartSymbolTable{byName: map[string][]vbUsageDeclaration{}, byLine: map[int][]vbUsageDeclaration{}}, "")
	if len(links) != 1 || links[0]["id"] != "link-rendercard-read" {
		t.Fatalf("links = %#v", links)
	}
	target, _ := links[0]["target"].(map[string]any)
	if target["uri"] != "file:///include.inc" || target["range"] != targetRange || target["nameRange"] != nameRange {
		t.Fatalf("target = %#v", target)
	}
}

func flowchartOutputFragmentByText(nodes []map[string]any, text string) map[string]any {
	for _, node := range nodes {
		fragments, _ := node["outputFragments"].([]map[string]any)
		for _, fragment := range fragments {
			if fragment["text"] == text {
				return fragment
			}
		}
	}
	return nil
}

func flowchartOutputNodeContaining(nodes []map[string]any, text string) map[string]any {
	for _, node := range nodes {
		fragments, _ := node["outputFragments"].([]map[string]any)
		for _, fragment := range fragments {
			value, _ := fragment["text"].(string)
			if strings.Contains(value, text) {
				return node
			}
		}
	}
	return nil
}

func flowchartOutputFragmentContaining(nodes []map[string]any, text string) map[string]any {
	for _, node := range nodes {
		fragments, _ := node["outputFragments"].([]map[string]any)
		for _, fragment := range fragments {
			value, _ := fragment["text"].(string)
			if strings.Contains(value, text) {
				return fragment
			}
		}
	}
	return nil
}

func flowchartHasIncomingEdgeLabel(edges []map[string]any, target any, label string) bool {
	for _, edge := range edges {
		if edge["target"] == target && edge["label"] == label {
			return true
		}
	}
	return false
}

func flowchartHasIncomingEdgeFromKind(nodes []map[string]any, edges []map[string]any, target any, sourceKind string) bool {
	for _, edge := range edges {
		if edge["target"] != target {
			continue
		}
		source := flowchartNodeByID(nodes, edge["source"].(string))
		if source != nil && source["kind"] == sourceKind {
			return true
		}
	}
	return false
}

func flowchartHasOutgoingEdgeToKind(nodes []map[string]any, edges []map[string]any, source any, targetKind string) bool {
	for _, edge := range edges {
		if edge["source"] != source {
			continue
		}
		target, _ := edge["target"].(string)
		node := flowchartNodeByID(nodes, target)
		if node != nil && node["kind"] == targetKind {
			return true
		}
	}
	return false
}
