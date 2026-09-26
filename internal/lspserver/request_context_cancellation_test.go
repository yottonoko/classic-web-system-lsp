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
)

type liveRequestCancellationContext struct {
	context.Context
	cancel context.CancelFunc
	checks atomic.Int64
	limit  int64
}

func (c *liveRequestCancellationContext) Err() error {
	checks := c.checks.Add(1)
	if checks >= c.limit {
		c.cancel()
	}
	return c.Context.Err()
}

type requestCancellationFixture struct {
	server *Server
	uri    string
	source string
}

func newRequestCancellationFixture(t *testing.T) requestCancellationFixture {
	t.Helper()
	root := t.TempDir()
	server := New(strings.NewReader(""), io.Discard, io.Discard)
	server.rootPath = root
	server.rootURI = filePathURI(root)
	server.workspaceRoots = []workspaceRoot{{Path: root, URI: server.rootURI}}
	const depth = 18
	for index := depth; index >= 1; index-- {
		body := ""
		if index > 1 {
			body = fmt.Sprintf("<!-- #include file=\"level-%d.inc\" -->\n<!-- #include file=\"level-%d.inc\" -->\n", index-1, index-1)
		} else {
			body = `<% Class Widget
Public Sub Run(ByVal input)
End Sub
End Class %>`
		}
		path := filepath.Join(root, fmt.Sprintf("level-%d.inc", index))
		if err := os.WriteFile(path, []byte(body), 0o600); err != nil {
			t.Fatal(err)
		}
	}
	source := `<!-- #include file="level-18.inc" -->
<!-- #include file="level-18.inc" -->
<%
Dim value
Set value = New Widget
value.Run 1
value.
%>`
	path := filepath.Join(root, "page.asp")
	if err := os.WriteFile(path, []byte(source), 0o600); err != nil {
		t.Fatal(err)
	}
	uri := filePathURI(path)
	server.documents[uri] = core.NewTextDocument(uri, "classic-asp", 1, source)
	return requestCancellationFixture{server: server, uri: uri, source: source}
}

func newLiveRequestCancellationContext(t *testing.T, limit int64) *liveRequestCancellationContext {
	t.Helper()
	base, cancel := context.WithCancel(context.Background())
	t.Cleanup(cancel)
	return &liveRequestCancellationContext{Context: base, cancel: cancel, limit: limit}
}

func requestParams(uri string, position lsp.Position) []byte {
	return mustRaw(map[string]any{
		"textDocument": map[string]any{"uri": uri},
		"position":     position,
	})
}

func TestLiveCancellationDropsVBImplementationCompletionAndSignatureHelp(t *testing.T) {
	fixture := newRequestCancellationFixture(t)
	document := core.NewTextDocument(fixture.uri, "classic-asp", 0, fixture.source)
	positions := map[string]lsp.Position{
		"textDocument/implementation": document.PositionAt(strings.Index(fixture.source, "value.Run") + len("value.Ru")),
		"textDocument/completion":     document.PositionAt(strings.Index(fixture.source, "value.") + len("value.")),
		"textDocument/signatureHelp":  document.PositionAt(strings.Index(fixture.source, "value.Run 1") + len("value.Run 1")),
	}
	for method, position := range positions {
		t.Run(method, func(t *testing.T) {
			ctx := newLiveRequestCancellationContext(t, 12)
			result, rpcErr := fixture.server.handleRequest(ctx, method, requestParams(fixture.uri, position))
			if rpcErr != nil {
				t.Fatalf("%s cancellation error = %#v", method, rpcErr)
			}
			if ctx.Context.Err() == nil || ctx.checks.Load() < ctx.limit {
				t.Fatalf("%s did not cancel during bounded include traversal: checks=%d limit=%d", method, ctx.checks.Load(), ctx.limit)
			}
			switch value := result.(type) {
			case lsp.CompletionList:
				if len(value.Items) != 0 {
					t.Fatalf("cancelled %s published completion items: %#v", method, value)
				}
			case []lsp.Location:
				if len(value) != 0 {
					t.Fatalf("cancelled %s published locations: %#v", method, value)
				}
			case *lsp.SignatureHelp:
				if value != nil && len(value.Signatures) != 0 {
					t.Fatalf("cancelled %s published signature help: %#v", method, value)
				}
			case nil:
			default:
				t.Fatalf("cancelled %s returned partial result of type %T: %#v", method, result, result)
			}
		})
	}
}

func TestLiveCancellationDropsJavaScriptImplementationCompletionAndSignatureHelp(t *testing.T) {
	root := t.TempDir()
	server := New(strings.NewReader(""), io.Discard, io.Discard)
	server.rootPath = root
	server.rootURI = filePathURI(root)
	server.workspaceRoots = []workspaceRoot{{Path: root, URI: server.rootURI}}
	source := `<script>
function render(value) { return value; }
render(1);
</script>`
	path := filepath.Join(root, "page.asp")
	if err := os.WriteFile(path, []byte(source), 0o600); err != nil {
		t.Fatal(err)
	}
	uri := filePathURI(path)
	server.documents[uri] = core.NewTextDocument(uri, "classic-asp", 1, source)
	document := core.NewTextDocument(uri, "classic-asp", 0, source)
	positions := map[string]lsp.Position{
		"textDocument/implementation": document.PositionAt(strings.Index(source, "render(1)") + len("render")),
		"textDocument/completion":     document.PositionAt(strings.Index(source, "render(1)") + len("render")),
		"textDocument/signatureHelp":  document.PositionAt(strings.Index(source, "render(1)") + len("render(1")),
	}
	for method, position := range positions {
		t.Run(method, func(t *testing.T) {
			ctx := newLiveRequestCancellationContext(t, 5)
			result, rpcErr := server.handleRequest(ctx, method, requestParams(uri, position))
			if rpcErr != nil {
				t.Fatalf("%s cancellation error = %#v", method, rpcErr)
			}
			if ctx.Context.Err() == nil || ctx.checks.Load() < ctx.limit {
				t.Fatalf("%s did not cancel during JavaScript request: checks=%d limit=%d", method, ctx.checks.Load(), ctx.limit)
			}
			switch value := result.(type) {
			case lsp.CompletionList:
				if len(value.Items) != 0 {
					t.Fatalf("cancelled %s published completion items: %#v", method, value)
				}
			case []lsp.Location:
				if len(value) != 0 {
					t.Fatalf("cancelled %s published locations: %#v", method, value)
				}
			case *lsp.SignatureHelp:
				if value != nil && len(value.Signatures) != 0 {
					t.Fatalf("cancelled %s published signature help: %#v", method, value)
				}
			case nil:
			default:
				t.Fatalf("cancelled %s returned partial result of type %T: %#v", method, result, result)
			}
		})
	}
}
