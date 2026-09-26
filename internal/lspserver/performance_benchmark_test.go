package lspserver

import (
	"context"
	"fmt"
	"io"
	"path/filepath"
	"strconv"
	"strings"
	"testing"

	"github.com/yottonoko/classic-web-system-lsp/internal/core"
	"github.com/yottonoko/classic-web-system-lsp/internal/lsp"
)

var lspPerformanceBenchmarkSink any

func BenchmarkClassicASPLSPPublishDiagnostics(b *testing.B) {
	source := benchmarkClassicASPLSPDocument(500)
	uri := "file:///bench/default.asp"
	server := New(nil, io.Discard, nil)
	doc := core.NewTextDocument(uri, "classic-asp", 1, source)
	server.documents[uri] = doc
	b.ReportAllocs()
	for b.Loop() {
		if err := server.publishFinalDiagnostics(uri, false, false); err != nil {
			b.Fatal(err)
		}
	}
}

func BenchmarkClassicASPLSPDidChangeDiagnosticsEquivalent(b *testing.B) {
	source := benchmarkClassicASPLSPDocument(500)
	uri := "file:///bench/default.asp"
	offset := strings.Index(source, "Response.Write")
	if offset < 0 {
		b.Fatal("benchmark source missing Response.Write")
	}
	changeRangeStart := core.NewTextDocument(uri, "classic-asp", 1, source).PositionAt(offset)
	b.ReportAllocs()
	for b.Loop() {
		server := New(nil, io.Discard, nil)
		doc := core.NewTextDocument(uri, "classic-asp", 1, source)
		server.documents[uri] = doc
		changeRange := lsp.Range{Start: changeRangeStart, End: changeRangeStart}
		doc.ApplyChange(&changeRange, "' ", 2)
		server.deleteParsedCacheForURILocked(uri)
		if err := server.publishFinalDiagnostics(uri, false, false); err != nil {
			b.Fatal(err)
		}
	}
}

func BenchmarkClassicASPLSPWorkspaceGraphDocuments(b *testing.B) {
	server := New(nil, io.Discard, nil)
	for i := range 100 {
		uri := "file:///bench/page-" + strconv.Itoa(i) + ".asp"
		server.workspace[uri] = core.NewTextDocument(uri, "classic-asp", 1, benchmarkClassicASPLSPDocument(20))
	}
	if documents, _ := server.workspaceGraphDocuments(); len(documents) != 100 {
		b.Fatalf("workspace graph documents = %d, want 100", len(documents))
	}
	b.ReportAllocs()
	b.ResetTimer()
	for b.Loop() {
		lspPerformanceBenchmarkSink, _ = server.workspaceGraphDocuments()
	}
}

func BenchmarkParsedAnalysisCacheHit(b *testing.B) {
	source := benchmarkClassicASPLSPDocument(500)
	uri := "file:///bench/parsed-cache.asp"

	b.Run("document", func(b *testing.B) {
		server := New(nil, io.Discard, nil)
		document := core.NewTextDocument(uri, "classic-asp", 1, source)
		if parsed := server.parseTextDocument(document, "VBScript"); parsed == nil {
			b.Fatal("initial document parse returned nil")
		}
		b.ReportAllocs()
		b.ResetTimer()
		for b.Loop() {
			lspPerformanceBenchmarkSink = server.parseTextDocument(document, "VBScript")
		}
	})

	b.Run("text", func(b *testing.B) {
		server := New(nil, io.Discard, nil)
		if parsed := server.parseText(uri, source, "VBScript"); parsed == nil {
			b.Fatal("initial text parse returned nil")
		}
		b.ReportAllocs()
		b.ResetTimer()
		for b.Loop() {
			lspPerformanceBenchmarkSink = server.parseText(uri, source, "VBScript")
		}
	})
}

func BenchmarkWorkspaceSymbolQueryPruning(b *testing.B) {
	server := New(nil, io.Discard, nil)
	documents := make([]*core.TextDocument, 0, 25)
	for index := range 25 {
		uri := "file:///bench/symbol-page-" + strconv.Itoa(index) + ".asp"
		document := core.NewTextDocument(uri, "classic-asp", 1, benchmarkClassicASPLSPDocument(20))
		server.workspace[uri] = document
		documents = append(documents, document)
	}
	b.Run("pruned", func(b *testing.B) {
		b.ReportAllocs()
		for b.Loop() {
			lspPerformanceBenchmarkSink = server.workspaceSymbols(context.Background(), "definitely-missing-symbol")
		}
	})
	b.Run("full-scan-reference", func(b *testing.B) {
		b.ReportAllocs()
		for b.Loop() {
			symbols := make([]lsp.SymbolInformation, 0)
			for _, document := range documents {
				parsed := server.parseTextDocument(document, server.settings.DefaultLanguage)
				symbols = append(symbols, server.workspaceSymbolsForDocument(context.Background(), document, parsed, "definitely-missing-symbol")...)
			}
			lspPerformanceBenchmarkSink = symbols
		}
	})
}

func BenchmarkJavaScriptProjectPreparationWarm(b *testing.B) {
	server, uri, position := benchmarkJavaScriptLanguageServiceServer(b)
	if _, ok := server.prepareJavaScriptRequest(uri, position); !ok {
		b.Fatal("initial JavaScript project preparation failed")
	}
	b.ReportAllocs()
	b.ResetTimer()
	for b.Loop() {
		request, ok := server.prepareJavaScriptRequest(uri, position)
		if !ok {
			b.Fatal("warm JavaScript project preparation failed")
		}
		lspPerformanceBenchmarkSink = request.state
	}
}

func BenchmarkJavaScriptLanguageServiceSemanticDiagnosticsWarm(b *testing.B) {
	server, uri, position := benchmarkJavaScriptLanguageServiceServer(b)
	server.settings.CheckJS = true
	server.settings.JavaScriptUnusedDiagnostics = false
	var result javaScriptDiagnosticReport
	if !server.javaScriptLanguageServiceRequest(context.Background(), uri, position, "textDocument/semanticDiagnostic", nil, &result) {
		b.Fatal("initial JavaScript semantic diagnostics failed")
	}
	b.ReportAllocs()
	b.ResetTimer()
	for b.Loop() {
		if !server.javaScriptLanguageServiceRequest(context.Background(), uri, position, "textDocument/semanticDiagnostic", nil, &result) {
			b.Fatal("warm JavaScript semantic diagnostics failed")
		}
		lspPerformanceBenchmarkSink = result
	}
}

func BenchmarkJavaScriptLanguageServiceCompletionWarm(b *testing.B) {
	server, uri, position := benchmarkJavaScriptLanguageServiceServer(b)
	if completions := server.completion(context.Background(), uri, position, nil); len(completions.Items) == 0 {
		b.Fatal("initial JavaScript completion failed")
	}
	b.ReportAllocs()
	b.ResetTimer()
	for b.Loop() {
		lspPerformanceBenchmarkSink = server.completion(context.Background(), uri, position, nil)
	}
}

func BenchmarkJavaScriptProjectPreparationAfterDocumentEdit(b *testing.B) {
	server, uri, position := benchmarkJavaScriptLanguageServiceServer(b)
	if _, ok := server.prepareJavaScriptRequest(uri, position); !ok {
		b.Fatal("initial JavaScript project preparation failed")
	}
	document := server.documentByURI(uri)
	if document == nil {
		b.Fatal("benchmark document missing")
	}
	b.ReportAllocs()
	b.ResetTimer()
	for i := 0; i < b.N; i++ {
		document.Text = fmt.Sprintf("<script>\nconst benchmarkValue = { compilerProperty: %d };\nbenchmarkValue.comp\n</script>", i)
		document.Version = i + 2
		server.mu.Lock()
		server.deleteParsedCacheForURILocked(uri)
		server.mu.Unlock()
		if _, ok := server.prepareJavaScriptRequest(uri, position); !ok {
			b.Fatal("JavaScript project preparation after edit failed")
		}
	}
}

func BenchmarkJavaScriptProjectPreparationAfterIncrementalDocumentEdit(b *testing.B) {
	server, uri, position := benchmarkJavaScriptLanguageServiceServer(b)
	if _, ok := server.prepareJavaScriptRequest(uri, position); !ok {
		b.Fatal("initial JavaScript project preparation failed")
	}
	document := server.documentByURI(uri)
	if document == nil {
		b.Fatal("benchmark document missing")
	}
	_, parsed := server.parsed(uri)
	if parsed == nil {
		b.Fatal("benchmark parsed document missing")
	}
	prefixOffset := strings.Index(document.Text, "compilerProperty: ")
	digitOffset := prefixOffset + len("compilerProperty: ")
	if prefixOffset < 0 || digitOffset >= len(document.Text) {
		b.Fatal("benchmark value missing")
	}
	b.ReportAllocs()
	b.ResetTimer()
	for i := 0; i < b.N; i++ {
		value := string(rune('0' + i%10))
		changeRange := document.Range(digitOffset, digitOffset+1)
		document.ApplyChange(&changeRange, value, i+2)
		updated := core.UpdateParsedDocument(parsed, []core.IncrementalChange{{Range: &changeRange, Text: value}}, core.Settings{DefaultLanguage: server.settings.DefaultLanguage})
		if !updated.Incremental {
			b.Fatalf("incremental parse failed: %s", updated.Reason)
		}
		parsed = updated.Parsed
		server.mu.Lock()
		server.parsedCache[parsedDocumentCacheKey(uri)] = parsedDocumentCacheEntry{Version: document.Version, Text: document.Text, DefaultLanguage: server.settings.DefaultLanguage, Parsed: parsed}
		server.markJavaScriptDocumentChangedLocked(uri)
		server.mu.Unlock()
		if _, ok := server.prepareJavaScriptRequest(uri, position); !ok {
			b.Fatal("JavaScript project preparation after incremental edit failed")
		}
	}
}

func BenchmarkJavaScriptProjectPreparationAfterMappingOnlyBoundaryEdit(b *testing.B) {
	server, uri, position := benchmarkJavaScriptLanguageServiceServer(b)
	if _, ok := server.prepareJavaScriptRequest(uri, position); !ok {
		b.Fatal("initial JavaScript project preparation failed")
	}
	document := server.documentByURI(uri)
	if document == nil {
		b.Fatal("benchmark document missing")
	}
	sources := [...]string{
		"<script type=\"text/javascript\">\nconst benchmarkValue = { compilerProperty: 1 };\nbenchmarkValue.comp\n</script>",
		"<script>\nconst benchmarkValue = { compilerProperty: 1 };\nbenchmarkValue.comp\n</script>",
	}
	deltaUpserts, deltaDeletes := -1, -1
	server.javascriptProjectDeltaTestHook = func(upserts, deletes int) {
		deltaUpserts, deltaDeletes = upserts, deletes
	}
	b.ReportAllocs()
	b.ResetTimer()
	for i := 0; i < b.N; i++ {
		source := sources[i&1]
		document.Update(i+2, source)
		server.mu.Lock()
		server.deleteParsedCacheForURILocked(uri)
		server.markJavaScriptDocumentChangedLocked(uri)
		server.mu.Unlock()
		position = positionAtSuffix(source, "benchmarkValue.comp")
		if _, ok := server.prepareJavaScriptRequest(uri, position); !ok {
			b.Fatal("JavaScript project preparation after mapping-only edit failed")
		} else if deltaUpserts != 0 || deltaDeletes != 0 {
			b.Fatalf("mapping-only boundary edit updated %d/%d JavaScript project files", deltaUpserts, deltaDeletes)
		}
	}
}

func BenchmarkJavaScriptProjectPreparationAfterMultiFileEdit(b *testing.B) {
	server, uri, position := benchmarkJavaScriptLanguageServiceServer(b)
	for index := 0; index < 20; index++ {
		workspaceURI := pathToFileURI(filepath.Join(server.rootPath, fmt.Sprintf("workspace-%02d.asp", index)))
		server.mu.Lock()
		server.rememberOpenDocumentLocked(workspaceURI, core.NewTextDocument(workspaceURI, "classic-asp", 1, fmt.Sprintf("<script>\nconst workspaceValue%02d = %d;\n</script>", index, index)))
		server.mu.Unlock()
	}
	if _, ok := server.prepareJavaScriptRequest(uri, position); !ok {
		b.Fatal("initial JavaScript project preparation failed")
	}
	document := server.documentByURI(uri)
	if document == nil {
		b.Fatal("benchmark document missing")
	}
	_, parsed := server.parsed(uri)
	if parsed == nil {
		b.Fatal("benchmark parsed document missing")
	}
	prefixOffset := strings.Index(document.Text, "compilerProperty: ")
	digitOffset := prefixOffset + len("compilerProperty: ")
	if prefixOffset < 0 || digitOffset >= len(document.Text) {
		b.Fatal("benchmark value missing")
	}
	b.ReportAllocs()
	b.ResetTimer()
	for i := 0; i < b.N; i++ {
		value := string(rune('0' + i%10))
		changeRange := document.Range(digitOffset, digitOffset+1)
		document.ApplyChange(&changeRange, value, i+2)
		updated := core.UpdateParsedDocument(parsed, []core.IncrementalChange{{Range: &changeRange, Text: value}}, core.Settings{DefaultLanguage: server.settings.DefaultLanguage})
		if !updated.Incremental {
			b.Fatalf("incremental parse failed: %s", updated.Reason)
		}
		parsed = updated.Parsed
		server.mu.Lock()
		server.parsedCache[parsedDocumentCacheKey(uri)] = parsedDocumentCacheEntry{Version: document.Version, Text: document.Text, DefaultLanguage: server.settings.DefaultLanguage, Parsed: parsed}
		server.markJavaScriptDocumentChangedLocked(uri)
		server.mu.Unlock()
		if _, ok := server.prepareJavaScriptRequest(uri, position); !ok {
			b.Fatal("JavaScript project preparation after multi-file edit failed")
		}
	}
}

func benchmarkJavaScriptLanguageServiceServer(b *testing.B) (*Server, string, lsp.Position) {
	b.Helper()
	server := New(nil, io.Discard, nil)
	server.rootPath = b.TempDir()
	uri := pathToFileURI(filepath.Join(server.rootPath, "completion.asp"))
	source := "<script>\nconst benchmarkValue = { compilerProperty: 1 };\nbenchmarkValue.comp\n</script>"
	server.documents[uri] = core.NewTextDocument(uri, "classic-asp", 1, source)
	return server, uri, positionAtSuffix(source, "benchmarkValue.comp")
}

func benchmarkClassicASPLSPDocument(blocks int) string {
	var b strings.Builder
	for i := range blocks {
		value := strconv.Itoa(i)
		b.WriteString(`<section data-index="`)
		b.WriteString(value)
		b.WriteString(`">`)
		b.WriteByte('\n')
		b.WriteString("<%\n")
		b.WriteString("Dim value")
		b.WriteString(value)
		b.WriteByte('\n')
		b.WriteString("value")
		b.WriteString(value)
		b.WriteString(" = ")
		b.WriteString(value)
		b.WriteByte('\n')
		b.WriteString("If value")
		b.WriteString(value)
		b.WriteString(" > 0 Then\n")
		b.WriteString("  Response.Write value")
		b.WriteString(value)
		b.WriteByte('\n')
		b.WriteString("End If\n")
		b.WriteString("%>\n")
		b.WriteString("</section>")
		if i+1 < blocks {
			b.WriteByte('\n')
		}
	}
	return b.String()
}
