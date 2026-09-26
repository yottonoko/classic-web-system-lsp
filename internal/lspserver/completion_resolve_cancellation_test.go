package lspserver

import (
	"context"
	"io"
	"strings"
	"sync"
	"testing"
	"time"

	"github.com/yottonoko/classic-web-system-lsp/internal/core"
	"github.com/yottonoko/classic-web-system-lsp/internal/lsp"
)

func TestCompletionResolveCancellationWhileJavaScriptSerializationIsHeld(t *testing.T) {
	server := New(strings.NewReader(""), io.Discard, io.Discard)
	uri := "file:///tmp/completion-resolve-cancel.asp"
	source := "<script>\nconst value = 1;\nvalue.\n</script>"
	server.documents[uri] = core.NewTextDocument(uri, "classic-asp", 1, source)
	item := embeddedJavaScriptCompletionResolveTestItem(uri)

	server.javascriptMu.Lock()
	defer server.javascriptMu.Unlock()
	ctx, cancel := context.WithCancel(context.Background())
	defer cancel()
	started := make(chan struct{})
	server.requestDispatchTestHook = func(_ context.Context, method string) {
		if method == "completionItem/resolve" {
			close(started)
		}
	}
	result := make(chan struct {
		value any
		err   *rpcError
	}, 1)
	go func() {
		value, err := server.handleRequest(ctx, "completionItem/resolve", mustRaw(item))
		result <- struct {
			value any
			err   *rpcError
		}{value: value, err: err}
	}()
	select {
	case <-started:
	case <-time.After(time.Second):
		t.Fatal("completion resolve did not start")
	}
	cancel()
	select {
	case response := <-result:
		if response.err == nil || response.err.Code != requestCancelledError().Code {
			t.Fatalf("cancelled completion resolve error = %#v, want request cancellation", response.err)
		}
		if response.value != nil {
			t.Fatalf("cancelled completion resolve returned partial result: %#v", response.value)
		}
	case <-time.After(time.Second):
		t.Fatal("cancelled completion resolve remained queued behind JavaScript serialization")
	}
}

func TestCompletionResolveNonJavaScriptItemBypassesJavaScriptSerialization(t *testing.T) {
	server := New(strings.NewReader(""), io.Discard, io.Discard)
	item := lsp.CompletionItem{
		Label:  "VBScriptValue",
		Detail: "original detail",
		Data: map[string]any{
			"kind": "vbscript-symbol",
			"uri":  "file:///tmp/completion-resolve.asp",
		},
	}
	server.javascriptMu.Lock()
	defer server.javascriptMu.Unlock()

	result := make(chan lsp.CompletionItem, 1)
	go func() {
		result <- server.resolveCompletionItemContext(context.Background(), item)
	}()
	select {
	case resolved := <-result:
		if resolved.Label != item.Label || resolved.Detail != item.Detail {
			t.Fatalf("non-JavaScript completion resolve = %#v, want %#v", resolved, item)
		}
	case <-time.After(time.Second):
		t.Fatal("non-JavaScript completion resolve waited on JavaScript serialization")
	}
}

func TestCompletionResolveCancellationSerializationRace(t *testing.T) {
	server := New(strings.NewReader(""), io.Discard, io.Discard)
	uri := "file:///tmp/completion-resolve-race.asp"
	source := "<script>\nconst value = 1;\nvalue.\n</script>"
	server.documents[uri] = core.NewTextDocument(uri, "classic-asp", 1, source)
	item := embeddedJavaScriptCompletionResolveTestItem(uri)
	server.javascriptMu.Lock()

	const callers = 24
	var wait sync.WaitGroup
	wait.Add(callers)
	for range callers {
		go func() {
			defer wait.Done()
			ctx, cancel := context.WithTimeout(context.Background(), 10*time.Millisecond)
			defer cancel()
			resolved := server.resolveCompletionItemContext(ctx, item)
			if resolved.Label != "" || resolved.Detail != "" || resolved.Data != nil || resolved.Documentation != nil || resolved.TextEdit != nil || len(resolved.AdditionalTextEdits) != 0 {
				t.Errorf("cancelled completion resolve returned %#v", resolved)
			}
		}()
	}
	wait.Wait()
	server.javascriptMu.Unlock()
}

func embeddedJavaScriptCompletionResolveTestItem(uri string) lsp.CompletionItem {
	return lsp.CompletionItem{
		Label: "value",
		Data: map[string]any{
			"fileName": javaScriptVirtualPath(uri, core.LanguageJavaScript),
			"position": 1,
		},
	}
}
