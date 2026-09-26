package lspserver

import (
	"os"
	"path/filepath"
	"strings"
	"testing"

	"github.com/yottonoko/classic-web-system-lsp/internal/core"
	"github.com/yottonoko/classic-web-system-lsp/internal/lsp"
)

func TestNavigationHTMLStaticFormActionIgnoresBodyExpression(t *testing.T) {
	root := t.TempDir()
	page := filepath.Join(root, "default.asp")
	writeNavigationHTMLTarget(t, root, "fixed.asp")

	source := `<form action="fixed.asp" method="post"><%= userName %><button type="submit">Save</button></form>`
	builder := navigationHTMLTestBuilder(t, root, page, source)

	if len(builder.edges) != 1 {
		t.Fatalf("static form edges = %#v, want one edge", builder.edges)
	}
	edge := builder.edges[0]
	if navigationHTMLTestEdgeTarget(builder, edge) != "fixed.asp" {
		t.Fatalf("static form target = %#v, want fixed.asp", edge)
	}
	if edge["confidence"] != "certain" {
		t.Fatalf("static form confidence = %#v, want certain", edge["confidence"])
	}
	if ranges, _ := edge["ranges"].([]lsp.Range); len(ranges) != 1 {
		t.Fatalf("static form ranges = %#v, want one action range", edge["ranges"])
	}
	if evidence, _ := edge["evidence"].([]map[string]any); len(evidence) != 1 || evidence[0]["extractor"] != "html" {
		t.Fatalf("static form evidence = %#v, want one HTML action evidence", edge["evidence"])
	}
}

func TestNavigationHTMLStaticFormActionIgnoresMultipleUnrelatedMarkers(t *testing.T) {
	root := t.TempDir()
	page := filepath.Join(root, "default.asp")
	writeNavigationHTMLTarget(t, root, "fixed.asp")

	source := `<form action="fixed.asp"><%= userName %><div><%= tenantName %></div><span><%= auditLabel %></span></form>`
	builder := navigationHTMLTestBuilder(t, root, page, source)

	if len(builder.edges) != 1 {
		t.Fatalf("static form with multiple body markers edges = %#v, want one edge", builder.edges)
	}
	edge := builder.edges[0]
	if edge["confidence"] != "certain" {
		t.Fatalf("static form with multiple body markers confidence = %#v, want certain", edge["confidence"])
	}
	if ranges, _ := edge["ranges"].([]lsp.Range); len(ranges) != 1 {
		t.Fatalf("static form with multiple body markers ranges = %#v, want one action range", edge["ranges"])
	}
	if navigationHTMLTestEdgeTarget(builder, edge) != "fixed.asp" {
		t.Fatalf("static form with multiple body markers target = %#v, want fixed.asp", edge)
	}
}

func TestNavigationHTMLDynamicControlKeepsFormActionAttributionSeparate(t *testing.T) {
	root := t.TempDir()
	page := filepath.Join(root, "default.asp")
	writeNavigationHTMLTarget(t, root, "save.asp")
	writeNavigationHTMLTarget(t, root, "delete.asp")

	source := `<% controlTarget = "delete.asp" %><form action="save.asp"><%= userName %><button formaction="<%= controlTarget %>" formmethod="post" name="delete" value="1"></button></form>`
	builder := navigationHTMLTestBuilder(t, root, page, source)

	formEdges := make([]map[string]any, 0, 2)
	for _, edge := range builder.edges {
		if edge["kind"] == "htmlForm" {
			formEdges = append(formEdges, edge)
		}
	}
	if len(formEdges) != 2 {
		t.Fatalf("dynamic control form edges = %#v, want form and control edges", builder.edges)
	}
	var saveEdge, deleteEdge map[string]any
	for _, edge := range formEdges {
		switch navigationHTMLTestEdgeTarget(builder, edge) {
		case "save.asp":
			saveEdge = edge
		case "delete.asp":
			deleteEdge = edge
		}
	}
	if saveEdge == nil || saveEdge["confidence"] != "certain" {
		t.Fatalf("static form edge = %#v, want certain save.asp edge", saveEdge)
	}
	if deleteEdge == nil {
		t.Fatalf("dynamic control edge missing: %#v", builder.edges)
	}
	if ranges, _ := saveEdge["ranges"].([]lsp.Range); len(ranges) != 1 {
		t.Fatalf("static form ranges = %#v, want one action range", saveEdge["ranges"])
	}
	if evidence, _ := saveEdge["evidence"].([]map[string]any); len(evidence) != 1 || evidence[0]["extractor"] != "html" {
		t.Fatalf("static form evidence = %#v, want one HTML action evidence", saveEdge["evidence"])
	}
	if deleteEdge["method"] != "POST" {
		t.Fatalf("dynamic control method = %#v, want POST", deleteEdge["method"])
	}
	if evidence, _ := deleteEdge["evidence"].([]map[string]any); len(evidence) != 1 || !strings.Contains(navigationString(evidence[0]["snippet"]), "controlTarget") {
		t.Fatalf("dynamic control evidence = %#v, want control expression evidence", deleteEdge["evidence"])
	}
}

func TestNavigationHTMLDynamicFormActionPreservesMetadataAndExpressionEvidence(t *testing.T) {
	root := t.TempDir()
	page := filepath.Join(root, "default.asp")
	writeNavigationHTMLTarget(t, root, "dynamic.asp")

	source := `<% target = "dynamic.asp" %><form action="<%= target %>" method="post" target="_blank"><%= userName %></form>`
	builder := navigationHTMLTestBuilder(t, root, page, source)

	if len(builder.edges) != 1 {
		t.Fatalf("dynamic form edges = %#v, want one edge", builder.edges)
	}
	edge := builder.edges[0]
	if navigationHTMLTestEdgeTarget(builder, edge) != "dynamic.asp" {
		t.Fatalf("dynamic form target = %#v, want dynamic.asp", edge)
	}
	if edge["method"] != "POST" || edge["targetFrame"] != "_blank" {
		t.Fatalf("dynamic form metadata = %#v, want POST and _blank", edge)
	}
	if ranges, _ := edge["ranges"].([]lsp.Range); len(ranges) != 1 {
		t.Fatalf("dynamic form ranges = %#v, want one action expression range", edge["ranges"])
	}
	evidence, _ := edge["evidence"].([]map[string]any)
	if len(evidence) != 1 || !strings.Contains(navigationString(evidence[0]["snippet"]), "target") || strings.Contains(navigationString(evidence[0]["snippet"]), "userName") {
		t.Fatalf("dynamic form evidence = %#v, want target expression only", edge["evidence"])
	}
}

func TestNavigationHTMLQuotedGreaterThanAndEncodedAttributesPreserveTargetsAndRanges(t *testing.T) {
	root := t.TempDir()
	page := filepath.Join(root, "default.asp")
	for _, target := range []string{"next.asp", "submit.asp", "control.asp", "dynamic.asp"} {
		writeNavigationHTMLTarget(t, root, target)
	}
	source := "前😀 <a href=\"next.asp?x=1>0&amp;y=2\">next</a>\n" +
		"<form action='submit.asp?x=1>0&amp;y=2'><input type=hidden name=\"token\" value=\"a>b\"></form>\n" +
		"<button formaction=\"control.asp?x=1>0&amp;y=2\" formmethod=\"post\">save</button>\n" +
		"<% dynamicTarget = \"dynamic.asp\" %><a href=\"<%= dynamicTarget %>?x=1&amp;y=2\">dynamic</a>"
	builder := navigationHTMLTestBuilder(t, root, page, source)

	seen := map[string]map[string]any{}
	for _, edge := range builder.edges {
		seen[navigationHTMLTestEdgeTarget(builder, edge)] = edge
	}
	for _, target := range []string{"next.asp", "submit.asp", "control.asp", "dynamic.asp"} {
		if seen[target] == nil {
			t.Fatalf("quoted-attribute target %q missing: %#v", target, builder.edges)
		}
	}

	document := core.NewTextDocument(filePathURI(page), "classic-asp", 0, source)
	encoded := "next.asp?x=1>0&amp;y=2"
	start := strings.Index(source, encoded)
	if start < 0 {
		t.Fatalf("encoded target not found in source")
	}
	wantRange := document.Range(start, start+len(encoded))
	edge := seen["next.asp"]
	ranges, _ := edge["ranges"].([]lsp.Range)
	if len(ranges) != 1 || ranges[0] != wantRange {
		t.Fatalf("encoded target range = %#v, want original source range %#v", ranges, wantRange)
	}
	evidence, _ := edge["evidence"].([]map[string]any)
	if len(evidence) != 1 || !strings.Contains(navigationString(evidence[0]["snippet"]), encoded) {
		t.Fatalf("encoded target evidence = %#v, want encoded source snippet", edge["evidence"])
	}

	dynamicEdge := seen["dynamic.asp"]
	dynamicEvidence, _ := dynamicEdge["evidence"].([]map[string]any)
	if len(dynamicEvidence) != 1 || !strings.Contains(navigationString(dynamicEvidence[0]["snippet"]), "dynamicTarget") {
		t.Fatalf("dynamic target evidence = %#v, want interpolation evidence", dynamicEdge["evidence"])
	}
}

func TestNavigationHTMLEntityEvidenceUsesEachRepeatedAttributeOccurrence(t *testing.T) {
	root := t.TempDir()
	page := filepath.Join(root, "default.asp")
	writeNavigationHTMLTarget(t, root, "same.asp")
	source := `<a href="same.asp?x=1&amp;y=2">one</a><a href="same.asp?x=1&amp;y=2">two</a>`
	builder := navigationHTMLTestBuilder(t, root, page, source)
	if len(builder.edges) != 1 {
		t.Fatalf("repeated encoded links produced edges = %#v, want one merged edge", builder.edges)
	}
	edge := builder.edges[0]
	if edge["count"] != 2 {
		t.Fatalf("repeated encoded link count = %#v, want two", edge["count"])
	}
	ranges, _ := edge["ranges"].([]lsp.Range)
	if len(ranges) != 2 {
		t.Fatalf("repeated encoded link ranges = %#v, want two", ranges)
	}
	document := core.NewTextDocument(filePathURI(page), "classic-asp", 0, source)
	encoded := `same.asp?x=1&amp;y=2`
	first := strings.Index(source, encoded)
	second := strings.LastIndex(source, encoded)
	want := []lsp.Range{document.Range(first, first+len(encoded)), document.Range(second, second+len(encoded))}
	for index := range want {
		if ranges[index] != want[index] {
			t.Fatalf("repeated encoded range[%d] = %#v, want %#v", index, ranges[index], want[index])
		}
	}
}

func TestNavigationHTMLEqualRenderedPrimitiveAlternativesCountOnce(t *testing.T) {
	root := t.TempDir()
	page := filepath.Join(root, "default.asp")
	writeNavigationHTMLTarget(t, root, "page10.asp")
	source := `<% value = IIf(enabled, "10", 10) %><a href="page<%= value %>.asp">next</a>`
	builder := navigationHTMLTestBuilder(t, root, page, source)
	if len(builder.edges) != 1 {
		t.Fatalf("equal rendered primitive alternatives edges = %d, want 1: %#v", len(builder.edges), builder.edges)
	}
	if builder.edges[0]["count"] != 1 {
		t.Fatalf("equal rendered primitive alternatives count = %#v, want 1", builder.edges[0]["count"])
	}
}

func navigationHTMLTestBuilder(t *testing.T, root, page, source string) *navigationGraphBuilder {
	t.Helper()
	parsed := core.ParseDocument(filePathURI(page), source, core.Settings{})
	builder := newNavigationGraphBuilder("document", parsed.URI, []workspaceRoot{{Path: root, URI: filePathURI(root)}})
	builder.addDocument(parsed, parsed.URI)
	if builder.navigationError != nil {
		t.Fatalf("navigation builder error = %v", builder.navigationError)
	}
	return builder
}

func navigationHTMLTestEdgeTarget(builder *navigationGraphBuilder, edge map[string]any) string {
	targetID, _ := edge["target"].(string)
	target := builder.nodeByID[targetID]
	return navigationString(target["label"])
}

func writeNavigationHTMLTarget(t *testing.T, root, name string) {
	t.Helper()
	if err := os.WriteFile(filepath.Join(root, name), nil, 0o644); err != nil {
		t.Fatal(err)
	}
}
