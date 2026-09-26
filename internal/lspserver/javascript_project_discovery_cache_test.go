package lspserver

import (
	"context"
	"io"
	"os"
	"path/filepath"
	"strings"
	"sync/atomic"
	"testing"

	"github.com/yottonoko/classic-web-system-lsp/internal/core"
)

func TestJavaScriptProjectDiscoveryCacheIsReleasedAtShutdown(t *testing.T) {
	server := New(strings.NewReader(""), io.Discard, io.Discard)
	javascriptProjectDiscoverySettingsMemory.Store(server, javascriptProjectDiscoverySettingsCacheEntry{key: "cached"})
	server.shutdownRuntimeCaches()
	if _, ok := javascriptProjectDiscoverySettingsMemory.Load(server); ok {
		t.Fatal("JavaScript project discovery cache retained a shut down server")
	}
}

func TestJavaScriptProjectPreparationWarmCallDoesNotRewalkGitIgnore(t *testing.T) {
	root := t.TempDir()
	pagePath := filepath.Join(root, "page.asp")
	ignorePath := filepath.Join(root, ".gitignore")
	if err := os.WriteFile(pagePath, []byte("<script>\nconst warmValue = 1;\nwarmValue;\n</script>"), 0o600); err != nil {
		t.Fatal(err)
	}
	if err := os.WriteFile(ignorePath, []byte("ignored/\n"), 0o600); err != nil {
		t.Fatal(err)
	}
	server := New(strings.NewReader(""), io.Discard, io.Discard)
	server.rootPath = root
	server.rootURI = filePathURI(root)
	server.workspaceRoots = []workspaceRoot{{URI: server.rootURI, Path: root}}
	server.settings.WorkspaceRespectGitIgnore = true
	server.configureFsGateway()
	uri := filePathURI(pagePath)
	doc := core.NewTextDocument(uri, "classic-asp", 1, "<script>\nconst warmValue = 1;\nwarmValue;\n</script>")
	server.documents[uri] = doc
	position := doc.PositionAt(strings.Index(doc.Text, "warmValue;") + len("warmValue"))
	if request, ok := server.prepareJavaScriptRequestContext(context.Background(), uri, position); !ok || request == nil {
		t.Fatal("initial JavaScript preparation failed")
	}
	server.clearSourceSnapshots()
	var gitIgnoreReads atomic.Int32
	server.workspaceFileReadTestHook = func(path string) {
		if filepath.Base(path) == ".gitignore" {
			gitIgnoreReads.Add(1)
		}
	}
	if request, ok := server.prepareJavaScriptRequestContext(context.Background(), uri, position); !ok || request == nil {
		t.Fatal("warm JavaScript preparation failed")
	}
	if got := gitIgnoreReads.Load(); got != 0 {
		t.Fatalf("warm JavaScript preparation reread .gitignore %d times", got)
	}

	if err := server.didChangeWatchedFiles(didChangeWatchedFilesParams{Changes: []fileEvent{{URI: filePathURI(ignorePath), Type: fileChangeChanged}}}); err != nil {
		t.Fatal(err)
	}
	server.clearSourceSnapshots()
	gitIgnoreReads.Store(0)
	if request, ok := server.prepareJavaScriptRequestContext(context.Background(), uri, position); !ok || request == nil {
		t.Fatal("JavaScript preparation after .gitignore invalidation failed")
	}
	if got := gitIgnoreReads.Load(); got == 0 {
		t.Fatal(".gitignore invalidation did not refresh discovery rules")
	}
}
