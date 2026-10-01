package lspserver

import (
	"io"
	"strings"
	"testing"

	"github.com/yottonoko/classic-web-system-lsp/internal/core"
)

func TestWorkspaceURIIndexFindsDifferentlyEncodedURIsAcrossChanges(t *testing.T) {
	server := New(strings.NewReader(""), io.Discard, io.Discard)
	page := core.NewTextDocument("file:///C:/Site/Page.asp", "classic-asp", 0, "page")
	other := core.NewTextDocument("file:///C:/Site/Other.asp", "classic-asp", 0, "other")
	clientPage := "file:///c%3A/site/page.asp"
	lookup := func() *core.TextDocument {
		server.mu.Lock()
		defer server.mu.Unlock()
		return server.workspaceDocumentByURILocked(clientPage)
	}

	server.mu.Lock()
	server.setWorkspaceDocumentLocked(page.URI, page)
	server.mu.Unlock()
	if lookup() != page {
		t.Fatal("escaped lower-case drive URI did not find the indexed page")
	}

	// Replacing one document with another keeps the size, so the helpers must
	// keep the index current.
	server.mu.Lock()
	server.deleteWorkspaceDocumentLocked(page.URI)
	server.setWorkspaceDocumentLocked(other.URI, other)
	server.mu.Unlock()
	if got := lookup(); got != nil {
		t.Fatalf("deleted page still found: %#v", got)
	}

	// Direct map changes and replaced maps are detected without the helpers.
	server.mu.Lock()
	server.workspace[page.URI] = page
	server.mu.Unlock()
	if lookup() != page {
		t.Fatal("page added directly to the map was not found")
	}
	server.mu.Lock()
	server.workspace = map[string]*core.TextDocument{other.URI: other}
	server.mu.Unlock()
	if got := lookup(); got != nil {
		t.Fatalf("replaced workspace map still found the page: %#v", got)
	}

	server.mu.Lock()
	server.setWorkspaceDocumentLocked(page.URI, page)
	server.setWorkspaceDocumentLocked("file:///c:/site/PAGE.asp", page)
	server.deleteWorkspaceDocumentsWithIdentityLocked(clientPage)
	remaining := len(server.workspace)
	server.mu.Unlock()
	if remaining != 1 || lookup() != nil {
		t.Fatalf("identity delete left %d documents", remaining)
	}
}
