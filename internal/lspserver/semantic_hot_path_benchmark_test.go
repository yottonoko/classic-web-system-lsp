package lspserver

import (
	"context"
	"io"
	"slices"
	"strconv"
	"strings"
	"testing"

	"github.com/yottonoko/classic-web-system-lsp/internal/core"
	"github.com/yottonoko/classic-web-system-lsp/internal/lsp"
	"github.com/yottonoko/classic-web-system-lsp/internal/vbscript"
)

func BenchmarkVBScriptSemanticHotPaths(b *testing.B) {
	source := benchmarkVBScriptSemanticDocument(160)
	parse := func() *core.ParsedDocument {
		return core.ParseDocument("file:///bench/semantic.asp", source, core.Settings{DefaultLanguage: "VBScript"})
	}

	b.Run("server-objects/cold", func(b *testing.B) {
		b.ReportAllocs()
		for b.Loop() {
			lspPerformanceBenchmarkSink = serverObjectSymbols(parse())
		}
	})
	b.Run("server-objects/warm", func(b *testing.B) {
		parsed := parse()
		serverObjectSymbols(parsed)
		b.ReportAllocs()
		b.ResetTimer()
		for b.Loop() {
			lspPerformanceBenchmarkSink = serverObjectSymbols(parsed)
		}
	})
	b.Run("implicit-declarations/cold", func(b *testing.B) {
		b.ReportAllocs()
		for b.Loop() {
			lspPerformanceBenchmarkSink = implicitVBDeclarations(parse())
		}
	})
	b.Run("implicit-declarations/warm", func(b *testing.B) {
		parsed := parse()
		implicitVBDeclarations(parsed)
		b.ReportAllocs()
		b.ResetTimer()
		for b.Loop() {
			lspPerformanceBenchmarkSink = implicitVBDeclarations(parsed)
		}
	})
	b.Run("return-slot/cold", func(b *testing.B) {
		position := positionAtSuffix(source, "result159 = result159 - 9")
		b.ReportAllocs()
		for b.Loop() {
			lspPerformanceBenchmarkSink = vbscript.IsReturnValueSlot(parse(), position)
		}
	})
	b.Run("return-slot/warm", func(b *testing.B) {
		parsed := parse()
		position := positionAtSuffix(source, "result159 = result159 - 9")
		b.ReportAllocs()
		b.ResetTimer()
		for b.Loop() {
			lspPerformanceBenchmarkSink = vbscript.IsReturnValueSlot(parsed, position)
		}
	})
	b.Run("typed-member-completion/cold", func(b *testing.B) {
		offset := strings.LastIndex(source, "worker159.") + len("worker159.")
		b.ReportAllocs()
		for b.Loop() {
			server := New(nil, io.Discard, nil)
			lspPerformanceBenchmarkSink = server.vbscriptTypedMemberCompletions(parse(), "worker159", offset)
		}
	})
	b.Run("typed-member-completion/warm", func(b *testing.B) {
		server := New(nil, io.Discard, nil)
		parsed := parse()
		offset := strings.LastIndex(source, "worker159.") + len("worker159.")
		if items := server.vbscriptTypedMemberCompletions(parsed, "worker159", offset); len(items) == 0 {
			b.Fatal("initial typed member completion returned no items")
		}
		b.ReportAllocs()
		b.ResetTimer()
		for b.Loop() {
			lspPerformanceBenchmarkSink = server.vbscriptTypedMemberCompletions(parsed, "worker159", offset)
		}
	})
}

func TestVBScriptTypedMemberCompletionCachesDirectResult(t *testing.T) {
	source := benchmarkVBScriptSemanticDocument(24)
	parsed := core.ParseDocument("file:///test/semantic.asp", source, core.Settings{DefaultLanguage: "VBScript"})
	server := New(nil, io.Discard, nil)
	offset := strings.LastIndex(source, "worker23.") + len("worker23.")
	if items := server.vbscriptTypedMemberCompletions(parsed, "worker23", offset); len(items) == 0 {
		t.Fatal("typed member completion returned no items")
	}

	value, _ := parsed.LoadRuntimeAnalysis(vbTypedMemberCompletionCacheAnalysisKey)
	cache, ok := value.(*vbTypedMemberCompletionCache)
	if !ok || cache == nil || !cache.matches("worker23", offset, server.settings) {
		t.Fatalf("typed completion did not publish direct-result cache: %#v", value)
	}
	if len(cache.items) == 0 {
		t.Fatal("typed completion cached an empty direct result")
	}
	if _, ok := parsed.LoadRuntimeAnalysis(vbscriptTypeInfoCacheAnalysisKey); ok {
		t.Fatal("direct typed completion unexpectedly built full source-wide type state")
	}
	if items := server.vbscriptTypedMemberCompletions(parsed, "worker23", offset); len(items) != len(cache.items) {
		t.Fatalf("cached typed completion item count = %d, want %d", len(items), len(cache.items))
	}
}

func TestVBScriptDirectLocalMemberCompletionMatchesSourceOrderedTypes(t *testing.T) {
	source := `<%
Class Worker
  Public Value
  Public Sub Run()
  End Sub
End Class
Function Build()
  Dim ignored(10), worker
  Set worker = New Worker
  worker.
End Function
%>`
	offset := strings.Index(source, "worker.") + len("worker.")
	server := New(nil, io.Discard, nil)
	parsed := core.ParseDocument("file:///test/direct-typed-completion.asp", source, core.Settings{DefaultLanguage: "VBScript"})
	fast, handled := server.vbscriptDirectLocalMemberCompletionsContext(context.Background(), parsed, "worker", offset)
	if !handled {
		t.Fatal("direct typed completion did not handle a local New assignment")
	}

	fullParsed := core.ParseDocument("file:///test/direct-typed-completion.asp", source, core.Settings{DefaultLanguage: "VBScript"})
	info, complete := server.vbscriptTypeInfoAtOffsetContext(context.Background(), fullParsed, offset)
	if !complete {
		t.Fatal("source-ordered type evaluation was incomplete")
	}
	full := server.vbscriptTypedMemberCompletionsFromTypesContext(context.Background(), []string{"Worker"}, info.members)
	labels := func(items []lsp.CompletionItem) []string {
		result := make([]string, 0, len(items))
		for _, item := range items {
			result = append(result, item.Label+":"+strconv.Itoa(int(item.Kind)))
		}
		slices.Sort(result)
		return result
	}
	if got, want := labels(fast), labels(full); !slices.Equal(got, want) {
		t.Fatalf("direct typed completion = %#v, want source-ordered %#v", got, want)
	}

	parameterSource := `<%
Class Worker
  Public Value
End Class
Function Build(worker)
  Set worker = New Worker
  worker.
End Function
%>`
	parameterOffset := strings.Index(parameterSource, "worker.") + len("worker.")
	parameterParsed := core.ParseDocument("file:///test/direct-typed-parameter.asp", parameterSource, core.Settings{DefaultLanguage: "VBScript"})
	if _, handled := server.vbscriptDirectLocalMemberCompletionsContext(context.Background(), parameterParsed, "worker", parameterOffset); !handled {
		t.Fatal("direct typed completion did not handle a locally bound parameter")
	}
}

func TestVBScriptDirectLocalMemberCompletionFallsBackForDynamicAndAnnotatedState(t *testing.T) {
	for _, test := range []struct {
		name   string
		source string
	}{
		{
			name: "dynamic",
			source: `<%
Class Worker
  Public Value
End Class
Function Build()
  Dim worker
  Set worker = New Worker
  Set worker = ResolveAtRuntime()
  worker.
End Function
%>`,
		},
		{
			name: "annotated",
			source: `<%
Class Worker
  Public Value
End Class
Function Build()
  ' @type worker As Worker
  Dim worker
  Set worker = New Worker
  worker.
End Function
%>`,
		},
		{
			name: "conditional",
			source: `<%
Class Worker
  Public Value
End Class
Function Build()
  Dim worker
  Set worker = New Worker
  If condition Then Set worker = ResolveAtRuntime()
  worker.
End Function
%>`,
		},
		{
			name: "explicit as type",
			source: `<%
Class Worker
  Public Value
End Class
Function Build()
  Dim worker As Other
  Set worker = New Worker
  worker.
End Function
%>`,
		},
		{
			name: "implicit global across procedures",
			source: `<%
Class First
  Public Shared
  Public FirstOnly
End Class
Class Second
  Public Shared
  Public SecondOnly
End Class
Sub AssignFirst()
  Set worker = New First
End Sub
Sub AssignSecond()
  Set worker = New Second
  worker.
End Sub
%>`,
		},
		{
			name: "class procedure",
			source: `<%
Class Worker
  Public Value
  Public Sub Build()
    Dim worker
    Set worker = New Worker
    worker.
  End Sub
End Class
%>`,
		},
	} {
		t.Run(test.name, func(t *testing.T) {
			offset := strings.Index(test.source, "worker.") + len("worker.")
			parsed := core.ParseDocument("file:///test/direct-fallback-"+test.name+".asp", test.source, core.Settings{DefaultLanguage: "VBScript"})
			server := New(nil, io.Discard, nil)
			if _, handled := server.vbscriptDirectLocalMemberCompletionsContext(context.Background(), parsed, "worker", offset); handled {
				t.Fatal("direct typed completion handled state requiring the source-ordered evaluator")
			}
		})
	}
}

func TestVBScriptDirectLocalMemberCompletionFallsBackForCatalogCollisions(t *testing.T) {
	t.Run("configured normalized type", func(t *testing.T) {
		source := `<%
Class Worker
  Public SourceOnly
End Class
Function Build()
  Dim worker
  Set worker = New Worker
  worker.
End Function
%>`
		offset := strings.Index(source, "worker.") + len("worker.")
		parsed := core.ParseDocument("file:///test/direct-configured-collision.asp", source, core.Settings{DefaultLanguage: "VBScript"})
		server := New(nil, io.Discard, nil)
		server.settings.VBScriptComTypes = map[string]vbscriptComTypeSetting{
			" Worker ": {Members: map[string]vbscriptComMemberSetting{"ConfiguredOnly": {Type: "String"}}},
		}
		if _, handled := server.vbscriptDirectLocalMemberCompletionsContext(context.Background(), parsed, "worker", offset); handled {
			t.Fatal("direct typed completion handled a configured/source type collision")
		}
		items := server.vbscriptTypedMemberCompletions(parsed, "worker", offset)
		if !completionHasLabel(items, "ConfiguredOnly") || !completionHasLabel(items, "SourceOnly") {
			t.Fatalf("source-ordered configured/source completion = %#v", items)
		}
	})

	t.Run("builtin type", func(t *testing.T) {
		source := `<%
Class RegExp
  Public SourceOnly
End Class
Function Build()
  Dim value
  Set value = New RegExp
  value.
End Function
%>`
		offset := strings.Index(source, "value.") + len("value.")
		parsed := core.ParseDocument("file:///test/direct-builtin-collision.asp", source, core.Settings{DefaultLanguage: "VBScript"})
		server := New(nil, io.Discard, nil)
		if _, handled := server.vbscriptDirectLocalMemberCompletionsContext(context.Background(), parsed, "value", offset); handled {
			t.Fatal("direct typed completion handled a builtin/source type collision")
		}
		items := server.vbscriptTypedMemberCompletions(parsed, "value", offset)
		if !completionHasLabel(items, "Execute") || !completionHasLabel(items, "SourceOnly") {
			t.Fatalf("source-ordered builtin/source completion = %#v", items)
		}
	})

	t.Run("configured global", func(t *testing.T) {
		source := `<%
Class SourceType
  Public SourceOnly
End Class
Function Build()
  Set worker = New SourceType
  worker.
End Function
%>`
		offset := strings.Index(source, "worker.") + len("worker.")
		parsed := core.ParseDocument("file:///test/direct-configured-global.asp", source, core.Settings{DefaultLanguage: "VBScript"})
		server := New(nil, io.Discard, nil)
		server.settings.VBScriptGlobals = map[string]vbscriptGlobalSetting{"worker": {Type: "ConfiguredType"}}
		server.settings.VBScriptComTypes = map[string]vbscriptComTypeSetting{
			"ConfiguredType": {Members: map[string]vbscriptComMemberSetting{"ConfiguredOnly": {Type: "String"}}},
		}
		if _, handled := server.vbscriptDirectLocalMemberCompletionsContext(context.Background(), parsed, "worker", offset); handled {
			t.Fatal("direct typed completion handled an undeclared configured global")
		}
		items := server.vbscriptTypedMemberCompletions(parsed, "worker", offset)
		if !completionHasLabel(items, "ConfiguredOnly") {
			t.Fatalf("configured global completion lost configured contract: %#v", items)
		}
	})
}

func TestGraphAnalysisWithoutTypeAnnotationsSkipsScopeMetadata(t *testing.T) {
	source := `<div data-note="@type ignored As String"></div>
<script>const marker = "@returns ignored As String";</script>
<%
Class Worker
  Public Property Get Value()
    Value = 1
  End Property
End Class
Dim first, second
first = 1
If first > 0 Then
  second = first
End If
%>`
	parsed := core.ParseDocument("file:///test/flat-graph-analysis.asp", source, core.Settings{DefaultLanguage: "VBScript"})
	index := vbscript.BuildSymbolIndex(parsed)
	if _, ok := index.Declarations["first"]; !ok {
		t.Fatalf("flat symbol index lost first declaration: %#v", index.Declarations)
	}
	details := graphAnalysisTypes(parsed)
	if len(details.Types) != 0 || len(details.TypeAnnotations) != 0 || len(details.Signatures) != 0 {
		t.Fatalf("flat graph analysis produced unexpected type metadata: %#v", details)
	}
	if _, ok := parsed.LoadRuntimeAnalysis(vbProcedureScopesAnalysisKey); ok {
		t.Fatal("annotation-free flat graph analysis built procedure scope metadata")
	}
	if _, ok := details.ScopedSignatures["worker\x00value#get"]; !ok {
		t.Fatalf("annotation-free property accessor signature missing: %#v", details.ScopedSignatures)
	}
}

func benchmarkVBScriptSemanticDocument(procedures int) string {
	var source strings.Builder
	source.Grow(procedures * 300)
	source.WriteString("<object id=\"SharedObject\" runat=\"server\" progid=\"Example.Component\"></object>\n<%\n")
	source.WriteString("Class Worker\n  Public Property Get Value\n    Value = 1\n  End Property\n  Public Sub Run()\n  End Sub\nEnd Class\n")
	for index := range procedures {
		value := strconv.Itoa(index)
		source.WriteString("Function result")
		source.WriteString(value)
		source.WriteString("(input)\n  Dim worker")
		source.WriteString(value)
		source.WriteString("\n  Set worker")
		source.WriteString(value)
		source.WriteString(" = New Worker\n  implicit")
		source.WriteString(value)
		source.WriteString(" = input + SharedObject.Value\n  result")
		source.WriteString(value)
		source.WriteString(" = implicit")
		source.WriteString(value)
		source.WriteString("\n  result")
		source.WriteString(value)
		source.WriteString(" = result")
		source.WriteString(value)
		source.WriteString(" - 9\n")
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
