package lspserver

import (
	"sync"
	"sync/atomic"
	"testing"

	"github.com/yottonoko/classic-web-system-lsp/internal/core"
	"github.com/yottonoko/classic-web-system-lsp/internal/lsp"
)

func TestEmbeddedDiagnosticsSkipUnaffectedLanguageAcrossIncrementalRevision(t *testing.T) {
	const uri = "file:///site/language-impact.asp"
	source := "<style>.x { color: red; }</style>\n<div></div>"
	previous := core.ParseDocument(uri, source, core.Settings{DefaultLanguage: "VBScript"})
	oldDocument := core.NewTextDocument(uri, "classic-asp", 1, source)
	htmlOffset := len(source) - len("<div></div>")
	wantRange := oldDocument.Range(htmlOffset, htmlOffset+5)
	htmlComputes := 0
	first := diagnosticsForEmbeddedLanguage(previous, core.LanguageHTML, htmlDiagnosticsAnalysisKey, func() []lsp.Diagnostic {
		htmlComputes++
		return []lsp.Diagnostic{{Range: wantRange, Message: "html"}}
	})
	if len(first) != 1 || htmlComputes != 1 {
		t.Fatalf("initial HTML diagnostics = %#v, computes=%d", first, htmlComputes)
	}

	start := len("<style>.x { color: ")
	changeRange := oldDocument.Range(start, start+len("red"))
	updated := core.UpdateParsedDocument(previous, []core.IncrementalChange{{Range: &changeRange, Text: "green"}}, core.Settings{DefaultLanguage: "VBScript"})
	if !updated.Incremental || updated.Impact.Affects(core.LanguageHTML) || !updated.Impact.Affects(core.LanguageCSS) {
		t.Fatalf("incremental impact = %#v", updated)
	}
	reused := diagnosticsForEmbeddedLanguage(updated.Parsed, core.LanguageHTML, htmlDiagnosticsAnalysisKey, func() []lsp.Diagnostic {
		htmlComputes++
		return nil
	})
	if htmlComputes != 1 {
		t.Fatalf("unaffected HTML diagnostics recomputed %d times", htmlComputes)
	}
	newDocument := core.NewTextDocument(uri, "classic-asp", 2, updated.Parsed.Text)
	wantShifted := newDocument.Range(htmlOffset+2, htmlOffset+7)
	if len(reused) != 1 || reused[0].Range != wantShifted {
		t.Fatalf("shifted HTML diagnostics = %#v, want %#v", reused, wantShifted)
	}
	if _, ok := updated.Parsed.LoadPreviousRuntimeAnalysis(htmlDiagnosticsAnalysisKey); ok {
		t.Fatal("HTML diagnostics retained the predecessor after initialization")
	}

	cssComputes := 0
	_ = diagnosticsForEmbeddedLanguage(updated.Parsed, core.LanguageCSS, cssDiagnosticsAnalysisKey, func() []lsp.Diagnostic {
		cssComputes++
		return nil
	})
	if cssComputes != 1 {
		t.Fatalf("affected CSS diagnostics computes = %d, want 1", cssComputes)
	}
	if _, ok := updated.Parsed.LoadPreviousRuntimeAnalysis(cssDiagnosticsAnalysisKey); ok {
		t.Fatal("CSS diagnostics retained the predecessor after initialization")
	}
}

func TestEmbeddedDiagnosticsSingleflightConcurrentRevisionQueries(t *testing.T) {
	const workers = 64
	parsed := core.ParseDocument("file:///site/diagnostic-singleflight.asp", "<main></main>", core.Settings{})
	var computes atomic.Int64
	var group sync.WaitGroup
	group.Add(workers)
	for range workers {
		go func() {
			defer group.Done()
			items := diagnosticsForEmbeddedLanguage(parsed, core.LanguageHTML, htmlDiagnosticsAnalysisKey, func() []lsp.Diagnostic {
				computes.Add(1)
				return []lsp.Diagnostic{{Message: "html"}}
			})
			if len(items) != 1 || items[0].Message != "html" {
				t.Errorf("diagnostics = %#v", items)
			}
		}()
	}
	group.Wait()
	if got := computes.Load(); got != 1 {
		t.Fatalf("concurrent diagnostic computes = %d, want 1", got)
	}
}
