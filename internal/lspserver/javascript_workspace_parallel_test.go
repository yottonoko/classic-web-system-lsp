package lspserver

import (
	"context"
	"io"
	"os"
	"path/filepath"
	"strings"
	"testing"
	"time"

	"github.com/yottonoko/classic-web-system-lsp/internal/core"
)

func TestNestedJavaScriptWorkspaceReadsBorrowReleasedAnalysisCapacity(t *testing.T) {
	root := t.TempDir()
	for _, name := range []string{"a.js", "b.js", "c.js"} {
		if err := os.WriteFile(filepath.Join(root, name), []byte("export const value = 1;"), 0o600); err != nil {
			t.Fatal(err)
		}
	}
	server := New(nil, io.Discard, io.Discard)
	t.Cleanup(server.shutdownRuntimeCaches)
	server.rootPath = root
	server.rootURI = filePathURI(root)
	server.workspaceRoots = []workspaceRoot{{Path: root, URI: server.rootURI}}
	server.configureFsGateway()
	server.analysisWorkers.setWorkers(2)
	uri := filePathURI(filepath.Join(root, "default.asp"))
	source := "<script>const localValue = 1;</script>"
	server.documents[uri] = core.NewTextDocument(uri, "classic-asp", 1, source)
	position := positionAtSuffix(source, "localValue")

	releaseSibling := make(chan struct{})
	siblingStarted := make(chan struct{})
	releaseReads := make(chan struct{})
	readsStarted := make(chan struct{}, 3)
	server.workspaceFileReadTestHook = func(path string) {
		if strings.EqualFold(filepath.Ext(path), ".js") {
			readsStarted <- struct{}{}
			<-releaseReads
		}
	}
	done := make(chan bool, 1)
	go func() {
		prepared := false
		server.analysisWorkers.parallelForBulk(context.Background(), 2, func(workerCtx context.Context, index int) {
			if index == 1 {
				close(siblingStarted)
				<-releaseSibling
				return
			}
			<-siblingStarted
			_, prepared = server.prepareJavaScriptRequestContext(workerCtx, uri, position)
		})
		done <- prepared
	}()
	select {
	case <-readsStarted:
	case <-time.After(time.Second):
		close(releaseSibling)
		close(releaseReads)
		t.Fatal("inline JavaScript workspace read did not start")
	}
	close(releaseSibling)
	select {
	case <-readsStarted:
	case <-time.After(time.Second):
		close(releaseReads)
		t.Fatal("JavaScript workspace reads did not borrow the released worker slot")
	}
	close(releaseReads)
	select {
	case prepared := <-done:
		if !prepared {
			t.Fatal("JavaScript request preparation failed")
		}
	case <-time.After(5 * time.Second):
		t.Fatal("JavaScript request preparation did not finish")
	}
}
