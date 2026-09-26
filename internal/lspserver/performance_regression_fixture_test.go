package lspserver

import (
	"io"
	"strconv"
	"strings"
	"testing"

	"github.com/yottonoko/classic-web-system-lsp/internal/core"
	"github.com/yottonoko/classic-web-system-lsp/internal/vbscript"
)

var crossRevisionPerformanceSink any

type crossRevisionScriptSource struct {
	URI  string
	Text string
}

// BenchmarkCrossRevisionTypedMemberCompletionCold is deliberately kept in a
// standalone fixture so the cross-revision runner can inject the same source
// into both revisions before compiling the package.
func BenchmarkCrossRevisionTypedMemberCompletionCold(b *testing.B) {
	source := crossRevisionTypedMemberSource(160)
	offset := strings.LastIndex(source, "worker159.") + len("worker159.")
	b.ReportAllocs()
	b.ResetTimer()
	for b.Loop() {
		parsed := core.ParseDocument("file:///bench/cross-revision/semantic.asp", source, core.Settings{DefaultLanguage: "VBScript"})
		server := New(nil, io.Discard, nil)
		items := server.vbscriptTypedMemberCompletions(parsed, "worker159", offset)
		if len(items) == 0 {
			b.Fatal("typed member completion returned no items")
		}
		crossRevisionPerformanceSink = items
	}
}

// BenchmarkCrossRevisionLargeAnalyze exercises the large VBScript analysis
// path with fixed inputs; it must not depend on generated files or the host
// filesystem.
func BenchmarkCrossRevisionLargeAnalyze(b *testing.B) {
	sources := crossRevisionLargeAnalysisSources(5, 1_000, 3)
	b.ReportAllocs()
	b.ResetTimer()
	for b.Loop() {
		for _, source := range sources {
			parsed := core.ParseDocument(source.URI, source.Text, core.Settings{DefaultLanguage: "VBScript"})
			index := vbscript.BuildSymbolIndex(parsed)
			types := graphAnalysisTypes(parsed)
			crossRevisionPerformanceSink = []any{index, types}
		}
	}
}

func crossRevisionTypedMemberSource(procedures int) string {
	var source strings.Builder
	source.Grow(procedures * 160)
	source.WriteString("<%\nClass Worker\n  Public Value\n  Public Sub Run()\n  End Sub\nEnd Class\n")
	for index := range procedures {
		value := strconv.Itoa(index)
		source.WriteString("Function result")
		source.WriteString(value)
		source.WriteString("(input)\n  Dim worker")
		source.WriteString(value)
		source.WriteString("\n  Set worker")
		source.WriteString(value)
		source.WriteString(" = New Worker\n  result")
		source.WriteString(value)
		source.WriteString(" = input\n")
		if index+1 == procedures {
			source.WriteString("  worker")
			source.WriteString(value)
			source.WriteString(".\n")
		}
		source.WriteString("End Function\n")
	}
	source.WriteString("%>")
	return source.String()
}

func crossRevisionLargeAnalysisSources(documents, targetLines, includes int) []crossRevisionScriptSource {
	sources := make([]crossRevisionScriptSource, 0, documents)
	for document := range documents {
		lines := make([]string, 0, targetLines)
		for include := 0; include < includes; include++ {
			lines = append(lines, `<!-- #include file="leaf-`+strconv.Itoa(document)+`-`+strconv.Itoa(include)+`.inc" -->`)
		}
		lines = append(lines, "<%")
		for declaration := 0; len(lines)+3 <= targetLines; declaration++ {
			name := "value_" + strconv.Itoa(document) + "_" + strconv.Itoa(declaration)
			lines = append(lines, "Dim "+name, name+" = "+strconv.Itoa(declaration))
		}
		lines = append(lines, "%>")
		for len(lines) < targetLines {
			lines = append(lines, "<!-- fixed filler -->")
		}
		sources = append(sources, crossRevisionScriptSource{
			URI:  "file:///bench/cross-revision/document-" + strconv.Itoa(document) + ".asp",
			Text: strings.Join(lines, "\n") + "\n",
		})
	}
	return sources
}
