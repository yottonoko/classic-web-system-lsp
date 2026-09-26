package lspserver

import (
	"context"
	"fmt"
	"io"
	"os"
	"path/filepath"
	"strings"
	"testing"

	"github.com/yottonoko/classic-web-system-lsp/internal/core"
)

func BenchmarkNavigationGraphWarmSparseWorkspace(b *testing.B) {
	documentCount := max(2, benchmarkEnvInt("ASP_LSP_NAV_BENCH_DOCUMENTS", 128))
	for _, scope := range []string{"document", "folder", "workspace"} {
		b.Run(scope, func(b *testing.B) {
			server, rootURI, targetURI := benchmarkNavigationGraphServer(b, documentCount)
			uri := targetURI
			if scope == "folder" {
				uri = rootURI
			}
			params := executeCommandParams{Arguments: []any{map[string]any{"scope": scope, "uri": uri}}}
			result := server.buildNavigationGraphContextResult(context.Background(), params)
			if result.err != nil || result.payload == nil {
				b.Fatalf("navigation graph warmup failed: payload=%T err=%v", result.payload, result.err)
			}
			b.ReportAllocs()
			b.ResetTimer()
			for b.Loop() {
				result = server.buildNavigationGraphContextResult(context.Background(), params)
				if result.err != nil || result.payload == nil {
					b.Fatalf("navigation graph benchmark failed: payload=%T err=%v", result.payload, result.err)
				}
				lspPerformanceBenchmarkSink = result.payload
			}
			b.ReportMetric(float64(documentCount), "workspace_pages")
		})
	}
}

func benchmarkNavigationGraphServer(b *testing.B, documentCount int) (*Server, string, string) {
	b.Helper()
	root := b.TempDir()
	rootURI := filePathURI(root)
	targetPath := filepath.Join(root, "target.asp")
	targetURI := filePathURI(targetPath)
	server := New(nil, io.Discard, nil)
	server.rootPath = root
	server.rootURI = rootURI
	server.workspaceRoots = []workspaceRoot{{Path: root, URI: rootURI}}

	writeSource := func(path, source string) {
		b.Helper()
		if err := os.WriteFile(path, []byte(source), 0o644); err != nil {
			b.Fatal(err)
		}
		uri := filePathURI(path)
		server.workspace[uri] = core.NewTextDocument(uri, "classic-asp", 1, source)
	}
	writeSource(filepath.Join(root, "shared.inc"), `<% Response.Redirect benchmarkTarget %>`)
	for index := range documentCount {
		path := filepath.Join(root, fmt.Sprintf("page-%04d.asp", index))
		if index == 0 {
			path = targetPath
		}
		nextIndex := (index + 1) % documentCount
		dynamicTarget := fmt.Sprintf("page-%04d.asp", nextIndex)
		if nextIndex == 0 {
			dynamicTarget = "target.asp"
		}
		if index > 0 && index%32 == 0 {
			dynamicTarget = "target.asp?from=dynamic"
		}
		links := make([]string, 0, 4)
		for offset := 1; offset <= 4; offset++ {
			linkTarget := fmt.Sprintf("page-%04d.asp", (index+offset)%documentCount)
			if (index+offset)%documentCount == 0 {
				linkTarget = "target.asp"
			}
			links = append(links, `<a href="`+linkTarget+`?source=benchmark#details">Next</a>`)
		}
		source := strings.Join([]string{
			`<%`,
			`Dim benchmarkTarget`,
			`benchmarkTarget = "` + dynamicTarget + `"`,
			`%>`,
			`<!-- #include file="shared.inc" -->`,
			strings.Join(links, "\n"),
		}, "\n")
		writeSource(path, source)
	}
	return server, rootURI, targetURI
}
