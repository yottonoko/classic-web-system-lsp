package lspserver

import (
	"context"
	"io"
	"os"
	"strconv"
	"strings"
	"testing"

	"github.com/yottonoko/classic-web-system-lsp/internal/core"
	"github.com/yottonoko/classic-web-system-lsp/internal/embedded"
	"github.com/yottonoko/classic-web-system-lsp/internal/excel"
	"github.com/yottonoko/classic-web-system-lsp/internal/vbscript"
)

type benchmarkScriptSource struct {
	URI  string
	Text string
}

func BenchmarkClassicASPScriptLargeParseAspDocument(b *testing.B) {
	sources := benchmarkLayeredScriptSources("large", benchmarkLargeScriptLines(), benchmarkLargeScriptExtraIncludes())
	benchmarkAcrossScriptSources(b, sources, func(source benchmarkScriptSource) any {
		return core.ParseDocument(source.URI, source.Text, core.Settings{DefaultLanguage: "VBScript"})
	})
}

func BenchmarkClassicASPScriptLargeBuildVirtualDocuments(b *testing.B) {
	sources := benchmarkLayeredScriptSources("large", benchmarkLargeScriptLines(), benchmarkLargeScriptExtraIncludes())
	benchmarkAcrossScriptSources(b, sources, func(source benchmarkScriptSource) any {
		parsed := core.ParseDocument(source.URI, source.Text, core.Settings{DefaultLanguage: "VBScript"})
		return []core.VirtualDocument{
			core.BuildVirtualDocument(parsed, core.LanguageHTML),
			core.BuildVirtualDocument(parsed, core.LanguageCSS),
			core.BuildVirtualDocument(parsed, core.LanguageJavaScript),
			core.BuildVirtualDocument(parsed, core.LanguageVBScript),
		}
	})
}

func BenchmarkClassicASPScriptLargeCollectVbscriptSymbols(b *testing.B) {
	sources := benchmarkLayeredScriptSources("large", benchmarkLargeScriptLines(), benchmarkLargeScriptExtraIncludes())
	benchmarkAcrossScriptSources(b, sources, func(source benchmarkScriptSource) any {
		parsed := core.ParseDocument(source.URI, source.Text, core.Settings{DefaultLanguage: "VBScript"})
		return vbscript.BuildSymbolIndex(parsed)
	})
}

func BenchmarkClassicASPScriptLargeAnalyzeVbscript(b *testing.B) {
	sources := benchmarkLayeredScriptSources("large", benchmarkLargeScriptLines(), benchmarkLargeScriptExtraIncludes())
	benchmarkAcrossScriptSources(b, sources, func(source benchmarkScriptSource) any {
		parsed := core.ParseDocument(source.URI, source.Text, core.Settings{DefaultLanguage: "VBScript"})
		index := vbscript.BuildSymbolIndex(parsed)
		types := graphAnalysisTypes(parsed)
		return []any{index, types}
	})
}

func BenchmarkClassicASPScriptLargeEmbeddedDiagnostics(b *testing.B) {
	sources := benchmarkLayeredScriptSources("large", benchmarkLargeScriptLines(), benchmarkLargeScriptExtraIncludes())
	server := New(nil, io.Discard, nil)
	server.settings.CheckJS = true
	benchmarkAcrossScriptSources(b, sources, func(source benchmarkScriptSource) any {
		doc := core.NewTextDocument(source.URI, "classic-asp", 1, source.Text)
		server.documents[source.URI] = doc
		server.deleteParsedCacheForURILocked(source.URI)
		return server.diagnostics(source.URI)
	})
}

func BenchmarkClassicASPScriptLargeHTMLVirtualDocument(b *testing.B) {
	benchmarkAcrossLargeParsedScriptSources(b, func(parsed *core.ParsedDocument) any {
		return core.BuildVirtualDocument(parsed, core.LanguageHTML)
	})
}

func BenchmarkClassicASPScriptLargeCSSVirtualDocument(b *testing.B) {
	benchmarkAcrossLargeParsedScriptSources(b, func(parsed *core.ParsedDocument) any {
		return core.BuildVirtualDocument(parsed, core.LanguageCSS)
	})
}

func BenchmarkClassicASPScriptLargeJavaScriptVirtualDocument(b *testing.B) {
	benchmarkAcrossLargeParsedScriptSources(b, func(parsed *core.ParsedDocument) any {
		return core.BuildVirtualDocument(parsed, core.LanguageJavaScript)
	})
}

func BenchmarkClassicASPScriptLargeCSSDiagnostics(b *testing.B) {
	css := embedded.NewCSS()
	benchmarkAcrossLargeParsedScriptSources(b, func(parsed *core.ParsedDocument) any {
		return css.Diagnostics(parsed)
	})
}

func BenchmarkClassicASPScriptLargeJavaScriptSemanticDiagnostics(b *testing.B) {
	sources := benchmarkLayeredScriptSources("large", benchmarkLargeScriptLines(), benchmarkLargeScriptExtraIncludes())
	benchmarkAcrossJavaScriptLanguageServiceSources(b, sources)
}

func BenchmarkClassicASPScriptHugeParseAspDocument(b *testing.B) {
	sources := benchmarkLayeredScriptSources("huge", benchmarkHugeScriptLines(), benchmarkHugeScriptExtraIncludes())
	benchmarkAcrossScriptSources(b, sources, func(source benchmarkScriptSource) any {
		return core.ParseDocument(source.URI, source.Text, core.Settings{DefaultLanguage: "VBScript"})
	})
}

func BenchmarkClassicASPScriptHugeEmbeddedDiagnostics(b *testing.B) {
	sources := benchmarkLayeredScriptSources("huge", benchmarkHugeScriptLines(), benchmarkHugeScriptExtraIncludes())
	server := New(nil, io.Discard, nil)
	server.settings.CheckJS = true
	benchmarkAcrossScriptSources(b, sources, func(source benchmarkScriptSource) any {
		doc := core.NewTextDocument(source.URI, "classic-asp", 1, source.Text)
		server.documents[source.URI] = doc
		server.deleteParsedCacheForURILocked(source.URI)
		return server.diagnostics(source.URI)
	})
}

func BenchmarkClassicASPScriptIncludeTreeWorkspaceGraph(b *testing.B) {
	sources := benchmarkIncludeTreeScriptSources()
	server := benchmarkServerWithWorkspaceSources(sources)
	b.ReportAllocs()
	b.ResetTimer()
	for b.Loop() {
		lspPerformanceBenchmarkSink = server.buildWorkspaceGraph(false)
	}
}

func BenchmarkClassicASPScriptBulkExportSheets(b *testing.B) {
	sources := benchmarkIncludeTreeScriptSources()
	server := benchmarkServerWithWorkspaceSources(sources)
	b.ReportAllocs()
	b.ResetTimer()
	for b.Loop() {
		payload, _ := server.exportAnalysisPayload(analysisExcelExportArg{Scope: "workspace"})
		lspPerformanceBenchmarkSink = excel.CreateAnalysisSheets(payload, server.analysisExcelLocale(), excel.AnalysisSheetsOptions{})
	}
}

func BenchmarkClassicASPEmbeddedHTMLVirtualDocument(b *testing.B) {
	benchmarkEmbeddedOperation(b, core.LanguageHTML, func(parsed *core.ParsedDocument) any {
		return core.BuildVirtualDocument(parsed, core.LanguageHTML)
	})
}

func BenchmarkClassicASPEmbeddedCSSVirtualDocument(b *testing.B) {
	benchmarkEmbeddedOperation(b, core.LanguageCSS, func(parsed *core.ParsedDocument) any {
		return core.BuildVirtualDocument(parsed, core.LanguageCSS)
	})
}

func BenchmarkClassicASPEmbeddedJavaScriptVirtualDocument(b *testing.B) {
	benchmarkEmbeddedOperation(b, core.LanguageJavaScript, func(parsed *core.ParsedDocument) any {
		return core.BuildVirtualDocument(parsed, core.LanguageJavaScript)
	})
}

func BenchmarkClassicASPEmbeddedCSSDiagnostics(b *testing.B) {
	css := embedded.NewCSS()
	benchmarkEmbeddedOperation(b, core.LanguageCSS, func(parsed *core.ParsedDocument) any {
		return css.Diagnostics(parsed)
	})
}

func BenchmarkClassicASPEmbeddedJavaScriptDiagnostics(b *testing.B) {
	benchmarkAcrossJavaScriptLanguageServiceSources(b, []benchmarkScriptSource{{
		URI:  "file:///bench/embedded.asp",
		Text: benchmarkLayeredScriptSource("embedded.asp", 0, "entry", 500, 0),
	}})
}

func benchmarkEmbeddedOperation(b *testing.B, language core.EmbeddedLanguage, operation func(*core.ParsedDocument) any) {
	source := benchmarkLayeredScriptSource("embedded", 0, "entry", 500, 0)
	if language == core.LanguageCSS {
		source += "\n<style>.broken { color: }</style>\n"
	}
	parsed := core.ParseDocument("file:///bench/embedded.asp", source, core.Settings{DefaultLanguage: "VBScript"})
	b.ReportAllocs()
	for b.Loop() {
		lspPerformanceBenchmarkSink = operation(parsed)
	}
}

func benchmarkAcrossScriptSources(b *testing.B, sources []benchmarkScriptSource, operation func(benchmarkScriptSource) any) {
	b.ReportAllocs()
	b.ResetTimer()
	for b.Loop() {
		for _, source := range sources {
			lspPerformanceBenchmarkSink = operation(source)
		}
	}
}

func benchmarkAcrossLargeParsedScriptSources(b *testing.B, operation func(*core.ParsedDocument) any) {
	sources := benchmarkLayeredScriptSources("large", benchmarkLargeScriptLines(), benchmarkLargeScriptExtraIncludes())
	benchmarkAcrossScriptSources(b, sources, func(source benchmarkScriptSource) any {
		parsed := core.ParseDocument(source.URI, source.Text, core.Settings{DefaultLanguage: "VBScript"})
		return operation(parsed)
	})
}

func benchmarkAcrossJavaScriptLanguageServiceSources(b *testing.B, sources []benchmarkScriptSource) {
	b.ReportAllocs()
	b.ResetTimer()
	for b.Loop() {
		for _, source := range sources {
			parsed := core.ParseDocument(source.URI, source.Text, core.Settings{DefaultLanguage: "VBScript"})
			document := core.NewTextDocument(source.URI, "classic-asp", 1, source.Text)
			position, ok := firstJavaScriptPosition(document, parsed)
			if !ok {
				continue
			}
			server := New(nil, io.Discard, nil)
			server.settings.CheckJS = true
			server.settings.JavaScriptUnusedDiagnostics = false
			server.documents[source.URI] = document
			var result javaScriptDiagnosticReport
			if !server.javaScriptLanguageServiceRequest(context.Background(), source.URI, position, "textDocument/semanticDiagnostic", nil, &result) {
				b.Fatal("TypeScript-Go semantic diagnostics request failed")
			}
			lspPerformanceBenchmarkSink = result
		}
	}
}

func benchmarkServerWithWorkspaceSources(sources []benchmarkScriptSource) *Server {
	server := New(nil, io.Discard, nil)
	for _, source := range sources {
		server.workspace[source.URI] = core.NewTextDocument(source.URI, "classic-asp", 1, source.Text)
	}
	return server
}

func benchmarkLayeredScriptSources(profile string, targetLines int, extraIncludes int) []benchmarkScriptSource {
	specs := []struct {
		file         string
		layer        int
		role         string
		chainInclude string
		extraPrefix  string
	}{
		{"default.asp", 0, "entry", "includes/layer1.inc", "default"},
		{"includes/layer1.inc", 1, "include", "layer2.inc", "layer1"},
		{"includes/layer2.inc", 2, "include", "layer3.inc", "layer2"},
		{"includes/layer3.inc", 3, "include", "layer4.inc", "layer3"},
		{"includes/layer4.inc", 4, "leaf", "", "layer4"},
	}
	var sources []benchmarkScriptSource
	for _, spec := range specs {
		sources = append(sources, benchmarkScriptSource{
			URI:  "file:///bench/" + profile + "/" + spec.file,
			Text: benchmarkLayeredScriptSource(spec.file, spec.layer, spec.role, targetLines, extraIncludes),
		})
		for i := 1; i <= extraIncludes; i++ {
			name := spec.extraPrefix + "-extra-" + benchmarkPad(i, 2) + ".inc"
			sources = append(sources, benchmarkScriptSource{
				URI:  "file:///bench/" + profile + "/includes/generated/" + name,
				Text: benchmarkExtraIncludeSource(spec.file, spec.extraPrefix, i),
			})
		}
	}
	return sources
}

func benchmarkLayeredScriptSource(file string, layer int, role string, targetLines int, extraIncludes int) string {
	lines := []string{
		`<%@ Language="VBScript" %>`,
		`<!-- Generated Go benchmark fixture: ` + file + ` -->`,
	}
	if layer < 4 {
		next := "layer" + strconv.Itoa(layer+1) + ".inc"
		if layer == 0 {
			next = "includes/" + next
		}
		lines = append(lines, `<!-- #include file="`+next+`" -->`)
	}
	for i := 1; i <= extraIncludes; i++ {
		lines = append(lines, `<!-- #include file="includes/generated/extra-`+benchmarkPad(i, 2)+`.inc" -->`)
	}
	lines = append(lines,
		"<%",
		"Dim layer"+strconv.Itoa(layer)+"BenchmarkTitle",
		"layer"+strconv.Itoa(layer)+"BenchmarkTitle = \"Layer "+strconv.Itoa(layer)+" benchmark\"",
		"%>",
		`<div class="layer`+strconv.Itoa(layer)+`-benchmark" data-role="`+role+`">`,
	)
	for block := 1; len(lines)+38 < targetLines; block++ {
		benchmarkPushLayeredBlock(&lines, layer, block, role)
	}
	lines = append(lines, "</div>")
	for len(lines) < targetLines {
		lines = append(lines, "<!-- filler line "+benchmarkPad(len(lines)+1, 5)+" -->")
	}
	return strings.Join(lines, "\n") + "\n"
}

func benchmarkPushLayeredBlock(lines *[]string, layer int, block int, role string) {
	padded := benchmarkPad(block, 4)
	prefix := "layer" + strconv.Itoa(layer)
	*lines = append(*lines,
		`<!-- `+prefix+` benchmark block `+padded+` -->`,
		`<section class="`+prefix+`-block" data-layer="`+strconv.Itoa(layer)+`" data-block="`+padded+`">`,
		`  <h2>`+role+` block `+padded+`</h2>`,
		`  <p>Static benchmark markup for `+prefix+` block `+padded+`.</p>`,
		`  <style>`,
		`    .`+prefix+`-block[data-block="`+padded+`"] .benchmark-meter {`,
		`      --benchmark-layer: `+strconv.Itoa(layer)+`;`,
		`      color: hsl(`+strconv.Itoa((layer*47+block)%360)+` 64% 32%);`,
		`      border-left: `+strconv.Itoa(1+block%4)+`px solid hsl(`+strconv.Itoa((layer*31+block)%360)+` 56% 52%);`,
		`    }`,
		`    .`+prefix+`-block[data-block="`+padded+`"] .benchmark-meter::before { content: "`+prefix+`-`+padded+`"; }`,
		`  </style>`,
		`  <script>`,
		`    (function () {`,
		`      const key = "`+prefix+`-`+padded+`";`,
		`      const store = (window.aspLspBenchmark = window.aspLspBenchmark || {});`,
		`      store[key] = { layer: `+strconv.Itoa(layer)+`, block: `+strconv.Itoa(block)+`, role: "`+role+`", even: `+strconv.FormatBool(block%2 == 0)+` };`,
		`      document.documentElement.dataset.aspLspLastBenchmark = key;`,
		`    })();`,
		`  </script>`,
		`<%`,
		`Dim `+prefix+`Index`+padded,
		prefix+`Index`+padded+` = `+strconv.Itoa(block),
		`If (`+prefix+`Index`+padded+` Mod 2) = 0 Then`,
		`    Response.Write "<span class=""even"">" & Server.HTMLEncode("`+prefix+`-even-`+padded+`") & "</span>"`,
		`Else`,
		`    Response.Write "<span class=""odd"">" & Server.HTMLEncode("`+prefix+`-odd-`+padded+`") & "</span>"`,
		`End If`,
		`%>`,
		`  <ul>`,
		`    <li><%= Server.HTMLEncode("`+prefix+`-item-`+padded+`-a") %></li>`,
		`    <li><%= Server.HTMLEncode("`+prefix+`-item-`+padded+`-b") %></li>`,
		`  </ul>`,
		`  <div class="benchmark-meter" data-score="<%= `+prefix+`Index`+padded+` %>"></div>`,
		`</section>`,
		``,
	)
}

func benchmarkExtraIncludeSource(owner string, prefix string, index int) string {
	suffix := benchmarkPad(index, 2)
	variable := prefix + "Extra" + suffix
	return strings.Join([]string{
		`<%@ Language="VBScript" %>`,
		`<!-- Leaf helper include for ` + owner + `. -->`,
		`<aside class="` + prefix + `-extra-` + suffix + `">`,
		`<%`,
		`Dim ` + variable + `Label`,
		variable + `Label = "` + prefix + `-extra-` + suffix + `"`,
		`Response.Write "<span class=""helper"">" & Server.HTMLEncode(` + variable + `Label) & "</span>"`,
		`%>`,
		`</aside>`,
	}, "\n") + "\n"
}

func benchmarkIncludeTreeScriptSources() []benchmarkScriptSource {
	depth := benchmarkEnvInt("ASP_LSP_GO_BENCH_INCLUDE_DEPTH", 3)
	branchFactor := benchmarkEnvInt("ASP_LSP_GO_BENCH_INCLUDE_BRANCH_FACTOR", 3)
	targetLines := benchmarkEnvInt("ASP_LSP_GO_BENCH_INCLUDE_LINES", 250)
	var sources []benchmarkScriptSource
	var writeNode func(int, []int)
	writeNode = func(currentDepth int, path []int) {
		relative := "default.asp"
		if currentDepth > 0 {
			parts := make([]string, 0, len(path))
			for _, part := range path {
				parts = append(parts, benchmarkPad(part, 2))
			}
			relative = "includes/level-" + benchmarkPad(currentDepth, 2) + "/node-" + strings.Join(parts, "-") + ".inc"
		}
		sources = append(sources, benchmarkScriptSource{
			URI:  "file:///bench/include-tree/" + relative,
			Text: benchmarkIncludeTreeNodeSource(relative, currentDepth, path, depth, branchFactor, targetLines),
		})
		if currentDepth >= depth {
			return
		}
		for branch := 1; branch <= branchFactor; branch++ {
			next := append(append([]int(nil), path...), branch)
			writeNode(currentDepth+1, next)
		}
	}
	writeNode(0, nil)
	return sources
}

func benchmarkIncludeTreeNodeSource(relative string, depth int, path []int, maxDepth int, branchFactor int, targetLines int) string {
	nodeID := "root"
	if len(path) > 0 {
		parts := make([]string, 0, len(path))
		for _, part := range path {
			parts = append(parts, strconv.Itoa(part))
		}
		nodeID = strings.Join(parts, "_")
	}
	lines := []string{`<%@ Language="VBScript" %>`, `<!-- Include tree Go benchmark fixture: ` + relative + ` -->`}
	if depth < maxDepth {
		for branch := 1; branch <= branchFactor; branch++ {
			lines = append(lines, `<!-- #include file="`+benchmarkIncludeTreeChildPath(depth, append(append([]int(nil), path...), branch))+`" -->`)
		}
	}
	lines = append(lines,
		"<%",
		"Dim includeTreeDepth",
		"includeTreeDepth = "+strconv.Itoa(depth),
		"%>",
		`<div class="include-tree-benchmark" data-depth="`+strconv.Itoa(depth)+`" data-node="`+nodeID+`">`,
		`<style>.tree-node-`+strings.ReplaceAll(nodeID, "_", "-")+` { color: hsl(`+strconv.Itoa((depth*61+len(path))%360)+` 60% 30%); }</style>`,
		`<script>window.aspLspIncludeTreeBenchmark = window.aspLspIncludeTreeBenchmark || {}; window.aspLspIncludeTreeBenchmark["`+nodeID+`"] = `+strconv.Itoa(depth)+`;</script>`,
	)
	for block := 1; len(lines)+15 < targetLines; block++ {
		padded := benchmarkPad(block, 4)
		lines = append(lines,
			`<section class="include-tree-node" data-depth="`+strconv.Itoa(depth)+`" data-node="`+nodeID+`">`,
			`<%`,
			`Dim includeTreeValue`+padded,
			`includeTreeValue`+padded+` = "`+nodeID+`-`+padded+`"`,
			`If Len(includeTreeValue`+padded+`) > 0 Then`,
			`    Response.Write "<span class=""tree-value"">" & Server.HTMLEncode(includeTreeValue`+padded+`) & "</span>"`,
			`End If`,
			`%>`,
			`<p>Static include tree benchmark markup for `+nodeID+`.</p>`,
			`</section>`,
			``,
		)
	}
	lines = append(lines, "</div>")
	for len(lines) < targetLines {
		lines = append(lines, "<!-- include tree filler "+benchmarkPad(len(lines)+1, 5)+" -->")
	}
	return strings.Join(lines, "\n") + "\n"
}

func benchmarkIncludeTreeChildPath(depth int, path []int) string {
	parts := make([]string, 0, len(path))
	for _, part := range path {
		parts = append(parts, benchmarkPad(part, 2))
	}
	file := "node-" + strings.Join(parts, "-") + ".inc"
	if depth == 0 {
		return "includes/level-01/" + file
	}
	return "../level-" + benchmarkPad(depth+1, 2) + "/" + file
}

func benchmarkPad(value int, width int) string {
	text := strconv.Itoa(value)
	if len(text) >= width {
		return text
	}
	return strings.Repeat("0", width-len(text)) + text
}

func benchmarkEnvInt(name string, fallback int) int {
	raw := os.Getenv(name)
	if raw == "" {
		return fallback
	}
	value, err := strconv.Atoi(raw)
	if err != nil || value <= 0 {
		return fallback
	}
	return value
}

func benchmarkLargeScriptLines() int {
	return benchmarkEnvInt("ASP_LSP_GO_BENCH_LARGE_LINES", 1_000)
}

func benchmarkLargeScriptExtraIncludes() int {
	return benchmarkEnvInt("ASP_LSP_GO_BENCH_LARGE_EXTRA_INCLUDES", 3)
}

func benchmarkHugeScriptLines() int {
	return benchmarkEnvInt("ASP_LSP_GO_BENCH_HUGE_LINES", 2_000)
}

func benchmarkHugeScriptExtraIncludes() int {
	return benchmarkEnvInt("ASP_LSP_GO_BENCH_HUGE_EXTRA_INCLUDES", 5)
}
