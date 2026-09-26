package lspserver

import (
	"context"
	"io"
	"os"
	"path/filepath"
	"sync/atomic"
	"testing"
	"time"

	"github.com/yottonoko/classic-web-system-lsp/internal/core"
)

func TestIncludeDiagnosticsResolvesIndependentTargetsInParallel(t *testing.T) {
	root := t.TempDir()
	ownerPath := filepath.Join(root, "default.asp")
	ownerText := ""
	for _, name := range []string{"a.inc", "b.inc", "c.inc"} {
		ownerText += `<!-- #include file="` + name + `" -->` + "\n"
		if err := os.WriteFile(filepath.Join(root, name), []byte(`<% Dim Value %>`), 0o600); err != nil {
			t.Fatal(err)
		}
	}
	server := New(nil, io.Discard, io.Discard)
	t.Cleanup(server.shutdownRuntimeCaches)
	server.rootPath = root
	server.rootURI = filePathURI(root)
	server.workspaceRoots = []workspaceRoot{{Path: root, URI: server.rootURI}}
	server.configureFsGateway()
	server.analysisWorkers.setWorkers(3)

	release := make(chan struct{})
	started := make(chan struct{}, 3)
	var active atomic.Int64
	var maximum atomic.Int64
	server.workspaceFileReadTestHook = func(string) {
		current := active.Add(1)
		for {
			previous := maximum.Load()
			if current <= previous || maximum.CompareAndSwap(previous, current) {
				break
			}
		}
		started <- struct{}{}
		<-release
		active.Add(-1)
	}
	parsed := core.ParseDocument(filePathURI(ownerPath), ownerText, core.Settings{DefaultLanguage: "VBScript"})
	type result struct {
		diagnostics int
		complete    bool
	}
	done := make(chan result, 1)
	go func() {
		diagnostics, complete := server.includeDiagnosticsContext(context.Background(), parsed)
		done <- result{diagnostics: len(diagnostics), complete: complete}
	}()
	for range 2 {
		select {
		case <-started:
		case <-time.After(time.Second):
			close(release)
			t.Fatal("independent include targets were not resolved concurrently")
		}
	}
	close(release)
	got := <-done
	if !got.complete || got.diagnostics != 0 {
		t.Fatalf("parallel include diagnostics = %#v", got)
	}
	if maximum.Load() < 2 {
		t.Fatalf("maximum concurrent include reads = %d, want at least 2", maximum.Load())
	}
}
