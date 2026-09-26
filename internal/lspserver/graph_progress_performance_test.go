package lspserver

import (
	"context"
	"io"
	"strconv"
	"strings"
	"testing"

	"github.com/yottonoko/classic-web-system-lsp/internal/core"
)

func BenchmarkNavigationGraphBuilderLargeDocument(b *testing.B) {
	var source strings.Builder
	source.Grow(256 * 1024)
	source.WriteString("<html><body>\n")
	for index := range 400 {
		target := "target-" + strconv.Itoa(index) + ".asp"
		source.WriteString("<a href=\"")
		source.WriteString(target)
		source.WriteString("\">target</a>\n")
		source.WriteString("<form action=\"")
		source.WriteString(target)
		source.WriteString("\" method=\"post\"><input type=\"hidden\" name=\"id\" value=\"")
		source.WriteString(strconv.Itoa(index))
		source.WriteString("\"></form>\n")
	}
	source.WriteString("<script>\n")
	for index := range 200 {
		name := "form" + strconv.Itoa(index)
		source.WriteString(name + ".action = \"submit-" + strconv.Itoa(index) + ".asp\";\n")
		source.WriteString(name + ".method = \"POST\";\n")
		source.WriteString(name + ".submit();\n")
	}
	source.WriteString("</script>\n<%\n")
	for index := range 200 {
		source.WriteString("Response.Redirect \"redirect-" + strconv.Itoa(index) + ".asp\"\n")
	}
	source.WriteString("%>\n</body></html>")
	server := New(nil, io.Discard, nil)
	parsed := server.parseText("file:///bench/navigation.asp", source.String(), "vbscript")
	b.ReportAllocs()
	b.ResetTimer()
	for b.Loop() {
		builder := newNavigationGraphBuilder("document", parsed.URI, nil)
		builder.addDocument(parsed, parsed.URI)
		lspPerformanceBenchmarkSink = builder.payload(1)
	}
}

func BenchmarkFlowchartLargeDocument(b *testing.B) {
	var source strings.Builder
	source.Grow(256 * 1024)
	source.WriteString("<%\n")
	for procedure := range 40 {
		source.WriteString("Sub Procedure" + strconv.Itoa(procedure) + "()\n")
		for statement := range 40 {
			source.WriteString("If value" + strconv.Itoa(statement) + " = " + strconv.Itoa(statement) + " Then\n")
			source.WriteString("  Procedure" + strconv.Itoa((procedure+1)%40) + "\n")
			source.WriteString("End If\n")
		}
		source.WriteString("End Sub\n")
	}
	source.WriteString("%>")
	server := New(nil, io.Discard, nil)
	parsed := server.parseText("file:///bench/flowchart.asp", source.String(), "vbscript")
	b.ReportAllocs()
	b.ResetTimer()
	for b.Loop() {
		sections := flowchartSections(parsed)
		nodes, edges := flowchartVBScriptNodes(parsed, nil, "normal", "en", 80)
		sections, nodes, edges = flowchartAttachSectionMembership(sections, nodes, edges)
		lspPerformanceBenchmarkSink = []any{sections, nodes, edges, flowchartMermaid(sections, nodes, edges, 80)}
	}
}

func BenchmarkProgressRegistryUpdates(b *testing.B) {
	server := New(nil, io.Discard, nil)
	b.ReportAllocs()
	b.ResetTimer()
	for b.Loop() {
		taskID, _ := server.beginProgressTask("benchmark", "analyzing", "benchmark", "benchmark.files", "", 100, false)
		for current := 1; current <= 100; current++ {
			server.updateProgressTask(taskID, "benchmark", "benchmark.files", "file.asp", current, 100, []string{"file.asp"}, "running")
		}
		server.finishProgressTask(taskID, "benchmark", "completed")
	}
}

func BenchmarkGraphDocumentAnalysesProgress(b *testing.B) {
	server := New(nil, io.Discard, nil)
	documents := make([]*core.ParsedDocument, 100)
	for index := range documents {
		uri := "file:///bench/graph-" + strconv.Itoa(index) + ".asp"
		documents[index] = server.parseText(uri, benchmarkClassicASPLSPDocument(20), "vbscript")
	}
	report := func(string, string, int, int) {}
	b.ReportAllocs()
	b.ResetTimer()
	for b.Loop() {
		analyses := server.graphDocumentAnalysesWithProgress(context.Background(), documents, report, "graph.workspace")
		lspPerformanceBenchmarkSink = analyses
	}
}
