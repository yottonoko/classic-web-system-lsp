package lspserver

import (
	"context"
	"encoding/json"
	"io"
	"path/filepath"
	"strings"
	"testing"

	"github.com/yottonoko/classic-web-system-lsp/internal/core"
	"github.com/yottonoko/classic-web-system-lsp/internal/lsp"
)

func TestJavaScriptPreparationFollowsProjectWhenDocumentChangesDuringDelta(t *testing.T) {
	server := New(strings.NewReader(""), io.Discard, io.Discard)
	server.rootPath = t.TempDir()
	uri := pathToFileURI(filepath.Join(server.rootPath, "page.asp"))
	otherURI := pathToFileURI(filepath.Join(server.rootPath, "other.asp"))
	scriptSource := "<script>\nconst pageValue = { pageMember: 1 };\npageValue.page\n</script>"
	document := core.NewTextDocument(uri, "classic-asp", 1, scriptSource)
	server.documents[uri] = document
	server.documents[otherURI] = core.NewTextDocument(otherURI, "classic-asp", 1, "<script>\nconst otherValue = 1;\n</script>")
	position := positionAtSuffix(scriptSource, "pageValue.page")
	if _, ok := server.prepareJavaScriptRequest(uri, position); !ok {
		t.Fatal("initial JavaScript preparation failed")
	}

	update := func(version int, source string) {
		server.mu.Lock()
		document.Update(version, source)
		server.deleteParsedCacheForURILocked(uri)
		server.markJavaScriptDocumentChangedLocked(uri)
		server.mu.Unlock()
	}
	// The delta removes the script from the project while the editor restores
	// it, so the delta result is stale by the time it is recorded.
	update(2, "<p>no script</p>")
	restored := false
	server.javascriptProjectDeltaTestHook = func(int, int) {
		if !restored {
			restored = true
			update(3, scriptSource)
		}
	}
	server.prepareJavaScriptRequest(uri, lsp.Position{})
	if !restored {
		t.Fatal("delta preparation did not run")
	}

	var completion struct {
		Items []struct {
			Label string `json:"label"`
		} `json:"items"`
	}
	if !server.javaScriptLanguageServiceRequest(context.Background(), uri, position, "textDocument/completion", nil, &completion) {
		t.Fatal("completion failed after a document changed during delta preparation")
	}
	encoded, _ := json.Marshal(completion)
	if !strings.Contains(string(encoded), "pageMember") {
		t.Fatalf("completion did not see the restored script: %s", encoded)
	}
}
