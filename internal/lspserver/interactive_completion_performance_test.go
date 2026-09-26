package lspserver

import (
	"context"
	"fmt"
	"io"
	"os"
	"path/filepath"
	"strings"
	"sync/atomic"
	"testing"

	"github.com/yottonoko/classic-web-system-lsp/internal/core"
	"github.com/yottonoko/classic-web-system-lsp/internal/lsp"
	workspacepkg "github.com/yottonoko/classic-web-system-lsp/internal/workspace"
)

func TestJavaScriptGlobalCompletionAfterIncrementalEditDoesNotWalkWorkspace(t *testing.T) {
	server, uri, position := newJavaScriptGlobalCompletionTestServer(t, 32)
	if items := server.completion(t.Context(), uri, position, nil).Items; !hasCompletionLabel(items, "helper0000") {
		t.Fatalf("initial auto-import completion missing helper0000: %#v", items)
	}

	var walks atomic.Int64
	server.mu.Lock()
	server.javascriptWorkspaceWalkTestHook = func(string) { walks.Add(1) }
	server.mu.Unlock()
	document := server.documentByURI(uri)
	changeRange := document.Range(strings.Index(document.Text, "h"), strings.Index(document.Text, "h")+1)
	if err := server.handleNotification(t.Context(), "textDocument/didChange", mustRaw(map[string]any{
		"textDocument": map[string]any{"uri": uri, "version": 2},
		"contentChanges": []map[string]any{{
			"range": changeRange,
			"text":  "H",
		}},
	})); err != nil {
		t.Fatal(err)
	}
	position = lsp.Position{Line: position.Line, Character: 1}
	if items := server.completion(t.Context(), uri, position, nil).Items; !hasCompletionLabel(items, "helper0000") {
		t.Fatalf("edited auto-import completion missing helper0000: %#v", items)
	}
	if got := walks.Load(); got != 0 {
		t.Fatalf("warm global completion walked workspace %d times", got)
	}
}

func BenchmarkJavaScriptExplicitGlobalCompletionAfterSmallEdit(b *testing.B) {
	for _, modules := range []int{1, 100, 1000} {
		b.Run(fmt.Sprintf("modules-%d", modules), func(b *testing.B) {
			server, uri, position := newJavaScriptGlobalCompletionBenchmarkServer(b, modules)
			if items := server.completion(context.Background(), uri, position, nil).Items; len(items) == 0 {
				b.Fatal("initial completion returned no items")
			}
			document := server.documentByURI(uri)
			offset := strings.Index(document.Text, "h")
			changeRange := document.Range(offset, offset+1)
			b.ReportAllocs()
			b.ResetTimer()
			for index := 0; b.Loop(); index++ {
				text := "h"
				if index%2 == 0 {
					text = "H"
				}
				if err := server.handleNotification(context.Background(), "textDocument/didChange", mustRaw(map[string]any{
					"textDocument":   map[string]any{"uri": uri, "version": index + 2},
					"contentChanges": []map[string]any{{"range": changeRange, "text": text}},
				})); err != nil {
					b.Fatal(err)
				}
				lspPerformanceBenchmarkSink = server.completion(context.Background(), uri, position, nil)
			}
			b.StopTimer()
		})
	}
}

func BenchmarkVBScriptAutoIncludeCompletionWarm(b *testing.B) {
	for _, documents := range []int{1, 100, 1000} {
		b.Run(fmt.Sprintf("documents-%d", documents), func(b *testing.B) {
			server := New(nil, io.Discard, io.Discard)
			for index := range documents {
				uri := fmt.Sprintf("file:///bench/includes/helper-%04d.inc", index)
				server.workspace[uri] = core.NewTextDocument(uri, "classic-asp", 1,
					fmt.Sprintf("<%% Public Function Helper%04d(): End Function %%>", index))
			}
			if !server.rebuildWorkspaceVBAutoIncludeCatalog(context.Background(), 0) {
				b.Fatal("auto-include catalog did not publish")
			}
			server.workspaceIncludeGraph = workspacepkg.NewWorkspaceIncludeGraph()
			server.workspaceIncludeGraphComplete = true
			owner := core.ParseDocument("file:///bench/default.asp", "<% Hel %>", core.Settings{})
			offset := strings.Index(owner.Text, "Hel") + len("Hel")
			if items, incomplete := server.vbscriptAutoIncludeCompletions(owner, offset); incomplete || len(items) != documents {
				b.Fatalf("initial auto-include completions = %d, incomplete=%v", len(items), incomplete)
			}
			b.ReportAllocs()
			b.ResetTimer()
			for b.Loop() {
				items, incomplete := server.vbscriptAutoIncludeCompletions(owner, offset)
				if incomplete || len(items) != documents {
					b.Fatalf("auto-include completions = %d, incomplete=%v", len(items), incomplete)
				}
				lspPerformanceBenchmarkSink = items
			}
		})
	}
}

func BenchmarkJavaScriptExplicitGlobalCompletionCold(b *testing.B) {
	for _, modules := range []int{100, 1000, 5000} {
		b.Run(fmt.Sprintf("modules-%d", modules), func(b *testing.B) {
			root := b.TempDir()
			for index := range modules {
				path := filepath.Join(root, fmt.Sprintf("helper-%04d.js", index))
				if err := os.WriteFile(path, []byte(fmt.Sprintf("export const helper%04d = %d;\n", index, index)), 0o600); err != nil {
					b.Fatal(err)
				}
			}
			uri := pathToFileURI(filepath.Join(root, "completion.asp"))
			position := lsp.Position{Line: 1, Character: 1}
			b.ReportAllocs()
			b.ResetTimer()
			for b.Loop() {
				server := New(nil, io.Discard, io.Discard)
				server.rootPath = root
				server.rootURI = pathToFileURI(root)
				server.settings.JavaScriptAutoImports = true
				server.documents[uri] = core.NewTextDocument(uri, "classic-asp", 1, "<script>\nh\n</script>")
				lspPerformanceBenchmarkSink = server.completion(context.Background(), uri, position, nil)
				server.stopDocumentOpenAnalysisWorkers()
			}
		})
	}
}

func BenchmarkInteractiveCompletionAfterSmallEdit(b *testing.B) {
	for _, declarations := range []int{500, 5000} {
		for _, language := range []string{"html", "css", "javascript", "vbscript"} {
			b.Run(fmt.Sprintf("%s/declarations-%d", language, declarations), func(b *testing.B) {
				server, uri, position, changeRange := newInteractiveCompletionBenchmarkServer(b, language, declarations)
				items := server.completion(context.Background(), uri, position, nil).Items
				if len(items) == 0 {
					b.Fatal("initial completion returned no items")
				}
				if language == "javascript" && !hasCompletionLabel(items, "helper0000") {
					b.Fatal("initial JavaScript completion did not use the declaration fixture")
				}
				b.ReportAllocs()
				b.ResetTimer()
				for index := 0; b.Loop(); index++ {
					text := "h"
					if index%2 == 0 {
						text = "H"
					}
					if err := server.handleNotification(context.Background(), "textDocument/didChange", mustRaw(map[string]any{
						"textDocument":   map[string]any{"uri": uri, "version": index + 2},
						"contentChanges": []map[string]any{{"range": changeRange, "text": text}},
					})); err != nil {
						b.Fatal(err)
					}
					lspPerformanceBenchmarkSink = server.completion(context.Background(), uri, position, nil)
				}
				b.StopTimer()
				if language == "javascript" && !hasCompletionLabel(server.completion(context.Background(), uri, position, nil).Items, "helper0000") {
					b.Fatal("edited JavaScript completion did not use the declaration fixture")
				}
			})
		}
	}
}

func BenchmarkWorkspaceArtifactRefreshAfterSmallEdit(b *testing.B) {
	for _, declarations := range []int{500, 5000} {
		for _, language := range []string{"html", "css", "vbscript"} {
			b.Run(fmt.Sprintf("%s/declarations-%d", language, declarations), func(b *testing.B) {
				server, uri, _, changeRange := newInteractiveCompletionBenchmarkServer(b, language, declarations)
				document, parsed := server.parsed(uri)
				if document == nil || parsed == nil || server.buildDocumentOpenWorkspaceArtifactSnapshot(parsed) == nil {
					b.Fatal("initial workspace artifact build failed")
				}
				b.ReportAllocs()
				b.ResetTimer()
				for index := 0; b.Loop(); index++ {
					text := "h"
					if index%2 == 0 {
						text = "H"
					}
					if err := server.handleNotification(context.Background(), "textDocument/didChange", mustRaw(map[string]any{
						"textDocument":   map[string]any{"uri": uri, "version": index + 2},
						"contentChanges": []map[string]any{{"range": changeRange, "text": text}},
					})); err != nil {
						b.Fatal(err)
					}
					server.cancelDocumentOpenAnalysis(uri)
					_, parsed = server.parsed(uri)
					lspPerformanceBenchmarkSink = server.buildDocumentOpenWorkspaceArtifactSnapshot(parsed)
				}
			})
		}
	}
}

func newInteractiveCompletionBenchmarkServer(b *testing.B, language string, declarations int) (*Server, string, lsp.Position, lsp.Range) {
	b.Helper()
	var source strings.Builder
	for index := range declarations {
		fmt.Fprintf(&source, "<%% Dim helper%04d : helper%04d = %d %%>\n", index, index, index)
		fmt.Fprintf(&source, "<div class=\"helper%04d\" id=\"node%04d\"></div>\n", index, index)
		fmt.Fprintf(&source, "<style>.helper%04d { color: red; }</style>\n", index)
	}
	marker := ""
	switch language {
	case "html":
		marker = "<h"
	case "css":
		marker = "<style>.h</style>"
	case "javascript":
		source.WriteString("<script>\n")
		for index := range declarations {
			fmt.Fprintf(&source, "const helper%04d = %d;\n", index, index)
		}
		marker = "h\n</script>"
	case "vbscript":
		marker = "<% h %>"
	default:
		b.Fatalf("unknown language %q", language)
	}
	source.WriteString(marker)
	text := source.String()
	offset := strings.LastIndex(text, "h")
	uri := "file:///bench/interactive-" + language + ".asp"
	server := New(nil, io.Discard, io.Discard)
	server.settings.DiagnosticsDebounceMS = 1000
	document := core.NewTextDocument(uri, "classic-asp", 1, text)
	server.documents[uri] = document
	changeRange := document.Range(offset, offset+1)
	position := document.PositionAt(offset + 1)
	b.Cleanup(server.shutdownRuntimeCaches)
	return server, uri, position, changeRange
}

func newJavaScriptGlobalCompletionTestServer(t *testing.T, modules int) (*Server, string, lsp.Position) {
	t.Helper()
	return newJavaScriptGlobalCompletionServer(t, t.TempDir(), modules)
}

func newJavaScriptGlobalCompletionBenchmarkServer(b *testing.B, modules int) (*Server, string, lsp.Position) {
	b.Helper()
	return newJavaScriptGlobalCompletionServer(b, b.TempDir(), modules)
}

type completionTestHelper interface {
	Helper()
	Fatalf(string, ...any)
	Cleanup(func())
}

func newJavaScriptGlobalCompletionServer(t completionTestHelper, root string, modules int) (*Server, string, lsp.Position) {
	t.Helper()
	for index := range modules {
		path := filepath.Join(root, fmt.Sprintf("helper-%04d.js", index))
		if err := os.WriteFile(path, []byte(fmt.Sprintf("export const helper%04d = %d;\n", index, index)), 0o600); err != nil {
			t.Fatalf("write helper module: %v", err)
		}
	}
	server := New(nil, io.Discard, io.Discard)
	server.rootPath = root
	server.rootURI = pathToFileURI(root)
	server.settings.JavaScriptAutoImports = true
	server.settings.DiagnosticsDebounceMS = 1000
	uri := pathToFileURI(filepath.Join(root, "completion.asp"))
	source := "<script>\nh\n</script>"
	server.documents[uri] = core.NewTextDocument(uri, "classic-asp", 1, source)
	t.Cleanup(server.shutdownRuntimeCaches)
	return server, uri, lsp.Position{Line: 1, Character: 1}
}
