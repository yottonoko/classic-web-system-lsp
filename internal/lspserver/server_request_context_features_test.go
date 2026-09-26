package lspserver

import (
	"context"
	"io"
	"strings"
	"testing"
	"time"

	"github.com/yottonoko/classic-web-system-lsp/internal/core"
	"github.com/yottonoko/classic-web-system-lsp/internal/lsp"
)

func TestContextAwareFeatureRequestsKeepNonJavaScriptResults(t *testing.T) {
	server := New(strings.NewReader(""), io.Discard, io.Discard)
	defer server.shutdownRuntimeCaches()
	uri := "file:///workspace/context-features.asp"
	source := `<%
Function BuildName(firstName)
  BuildName = firstName
End Function
Dim sharedValue
sharedValue = BuildName("Dashboard")
%>`
	server.documents[uri] = core.NewTextDocument(uri, "classic-asp", 1, source)
	document := core.NewTextDocument(uri, "classic-asp", 0, source)
	r := document.Range(0, len(source))

	result, rpcErr := server.handleRequest(context.Background(), "textDocument/inlayHint", mustRaw(map[string]any{
		"textDocument": map[string]any{"uri": uri},
		"range":        r,
	}))
	if rpcErr != nil {
		t.Fatalf("inlay request error = %#v", rpcErr)
	}
	hints, ok := result.([]lsp.InlayHint)
	if !ok || len(hints) == 0 {
		t.Fatalf("inlay request = %#v, want VBScript hints", result)
	}

	result, rpcErr = server.handleRequest(context.Background(), "textDocument/semanticTokens/range", mustRaw(map[string]any{
		"textDocument": map[string]any{"uri": uri},
		"range":        r,
	}))
	if rpcErr != nil {
		t.Fatalf("semantic-token range request error = %#v", rpcErr)
	}
	tokens, ok := result.(lsp.SemanticTokens)
	if !ok || len(tokens.Data) == 0 {
		t.Fatalf("semantic-token range request = %#v, want VBScript tokens", result)
	}

	server.settings.CodeLensReferences = false
	server.settings.CodeLensIncludes = true
	includeURI := "file:///workspace/context-features-include.asp"
	includeSource := `<!-- #include file="shared.inc" -->`
	server.documents[includeURI] = core.NewTextDocument(includeURI, "classic-asp", 1, includeSource)
	result, rpcErr = server.handleRequest(context.Background(), "textDocument/codeLens", mustRaw(map[string]any{
		"textDocument": map[string]any{"uri": includeURI},
	}))
	if rpcErr != nil {
		t.Fatalf("CodeLens request error = %#v", rpcErr)
	}
	lenses, ok := result.([]lsp.CodeLens)
	if !ok || len(lenses) != 1 {
		t.Fatalf("CodeLens request = %#v, want one include lens", result)
	}
}

func TestContextAwareIncludeFeatureRequestsDropCancelledResults(t *testing.T) {
	fixture := newRequestCancellationFixture(t)
	document := core.NewTextDocument(fixture.uri, "classic-asp", 0, fixture.source)
	r := document.Range(0, len(fixture.source))
	params := mustRaw(map[string]any{
		"textDocument": map[string]any{"uri": fixture.uri},
		"range":        r,
	})
	methods := []string{"textDocument/inlayHint", "textDocument/semanticTokens/range", "textDocument/codeLens"}
	fixture.server.settings.CodeLensReferences = false
	fixture.server.settings.CodeLensIncludes = true
	for _, method := range methods {
		t.Run(method, func(t *testing.T) {
			ctx := newLiveRequestCancellationContext(t, 12)
			result, rpcErr := fixture.server.handleRequest(ctx, method, params)
			if rpcErr != nil {
				t.Fatalf("cancelled %s request error = %#v", method, rpcErr)
			}
			if ctx.Context.Err() == nil || ctx.checks.Load() < ctx.limit {
				t.Fatalf("%s did not cancel during include traversal: checks=%d limit=%d", method, ctx.checks.Load(), ctx.limit)
			}
			switch value := result.(type) {
			case nil:
			case []lsp.InlayHint:
				if len(value) != 0 {
					t.Fatalf("cancelled %s published inlay hints: %#v", method, value)
				}
			case lsp.SemanticTokens:
				if len(value.Data) != 0 {
					t.Fatalf("cancelled %s published semantic tokens: %#v", method, value)
				}
			case []lsp.CodeLens:
				if len(value) != 0 {
					t.Fatalf("cancelled %s published CodeLens values: %#v", method, value)
				}
			default:
				t.Fatalf("cancelled %s returned partial result of type %T: %#v", method, result, result)
			}
		})
	}
}

func TestContextAwareJavaScriptFeatureRequestsStopWaitingForHeldLock(t *testing.T) {
	server := New(strings.NewReader(""), io.Discard, io.Discard)
	uri := "file:///workspace/held-javascript-lock.asp"
	source := `<script>
const value = 1;
</script>`
	server.documents[uri] = core.NewTextDocument(uri, "classic-asp", 1, source)
	document := core.NewTextDocument(uri, "classic-asp", 0, source)
	paramsFor := func(method string) []byte {
		if method == "textDocument/inlayHint" {
			return mustRaw(map[string]any{
				"textDocument": map[string]any{"uri": uri},
				"range":        document.Range(0, len(source)),
			})
		}
		return mustRaw(map[string]any{
			"textDocument": map[string]any{"uri": uri},
			"range":        document.Range(0, len(source)),
		})
	}
	for _, method := range []string{"textDocument/inlayHint", "textDocument/semanticTokens/range"} {
		t.Run(method, func(t *testing.T) {
			ctx := newLiveRequestCancellationContext(t, 2)
			server.javascriptMu.Lock()
			defer server.javascriptMu.Unlock()
			done := make(chan struct {
				result any
				err    *rpcError
			}, 1)
			go func() {
				result, rpcErr := server.handleRequest(ctx, method, paramsFor(method))
				done <- struct {
					result any
					err    *rpcError
				}{result: result, err: rpcErr}
			}()
			select {
			case response := <-done:
				if response.err != nil {
					t.Fatalf("cancelled %s request error = %#v", method, response.err)
				}
				if ctx.Context.Err() == nil {
					t.Fatalf("%s request did not cancel while JavaScript lock was held", method)
				}
				switch value := response.result.(type) {
				case nil:
				case []lsp.InlayHint:
					if len(value) != 0 {
						t.Fatalf("cancelled %s published inlay hints: %#v", method, value)
					}
				case lsp.SemanticTokens:
					if len(value.Data) != 0 {
						t.Fatalf("cancelled %s published semantic tokens: %#v", method, value)
					}
				default:
					t.Fatalf("cancelled %s returned partial result of type %T: %#v", method, response.result, response.result)
				}
			case <-time.After(time.Second):
				t.Fatal("cancelled request remained queued behind the held JavaScript lock")
			}
		})
	}
}
