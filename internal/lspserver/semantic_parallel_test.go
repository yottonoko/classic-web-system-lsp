package lspserver

import (
	"context"
	"io"
	"sync"
	"testing"
	"time"

	"github.com/yottonoko/classic-web-system-lsp/internal/core"
	"github.com/yottonoko/classic-web-system-lsp/internal/lsp"
)

func TestJavaScriptSemanticTokensHonorsCanceledContext(t *testing.T) {
	server := New(nil, io.Discard, io.Discard)
	ctx, cancel := context.WithCancel(context.Background())
	cancel()

	if tokens := server.javaScriptSemanticTokensContext(ctx, "file:///workspace/canceled.asp"); tokens != nil {
		t.Fatalf("canceled JavaScript semantic tokens = %#v, want nil", tokens)
	}
}

func TestSemanticTokensInflightSynchronizesPartialAndFinalResults(t *testing.T) {
	inflight := newSemanticTokensInflight()
	partial := lsp.SemanticTokens{ResultID: "file:///test.asp#1:partial", Data: []int{1, 2, 3, 4, 5}}
	final := lsp.SemanticTokens{ResultID: "file:///test.asp#1", Data: []int{6, 7, 8, 9, 10}}
	inflight.setTokens(partial)

	const readers = 16
	var readersWG sync.WaitGroup
	readersWG.Add(readers)
	for index := 0; index < readers; index++ {
		go func() {
			defer readersWG.Done()
			for attempt := 0; attempt < 100; attempt++ {
				_ = inflight.snapshot()
			}
		}()
	}
	go func() {
		for attempt := 0; attempt < 100; attempt++ {
			inflight.setTokens(partial)
		}
		inflight.complete(final)
	}()
	if !inflight.wait(context.Background()) {
		t.Fatal("in-flight result did not complete")
	}
	readersWG.Wait()

	if got := inflight.snapshot(); got.ResultID != final.ResultID || len(got.Data) != len(final.Data) {
		t.Fatalf("final in-flight result = %#v, want %#v", got, final)
	}
	if inflight.active() {
		t.Fatal("completed in-flight result remained active")
	}
	if inflight.setTokens(partial) {
		t.Fatal("completed in-flight result accepted a late partial result")
	}
}

func TestClearSemanticTokenCacheInvalidatesSemanticTokenFlights(t *testing.T) {
	server := New(nil, io.Discard, io.Discard)
	uri := "file:///workspace/stale.asp"
	doc := core.NewTextDocument(uri, "classic-asp", 1, "<% Dim value %>")
	inflight := newSemanticTokensInflight()
	inflight.uri = uri
	inflight.document = doc
	inflight.version = doc.Version
	inflight.text = doc.Text
	key := semanticTokensResultID(uri, doc.Version)

	server.mu.Lock()
	server.documents[uri] = doc
	inflight.generation = server.semanticTokensGenerationLocked(uri)
	server.semanticInflight[key] = inflight
	server.semantic[uri] = semanticTokenCache{Version: doc.Version, Tokens: lsp.SemanticTokens{ResultID: key}}
	server.mu.Unlock()

	server.clearSemanticTokenCache()
	if !inflight.wait(context.Background()) {
		t.Fatal("invalidated in-flight result did not complete")
	}

	server.mu.Lock()
	_, flightStillRegistered := server.semanticInflight[key]
	_, cacheStillPresent := server.semantic[uri]
	server.mu.Unlock()
	if flightStillRegistered || cacheStillPresent {
		t.Fatalf("semantic cache clear left stale state: flight=%v cache=%v", flightStillRegistered, cacheStillPresent)
	}
	if inflight.active() {
		t.Fatal("invalidated semantic token flight remained active")
	}
}

func TestFinishDeferredSemanticTokensRejectsSameVersionDocumentReplacement(t *testing.T) {
	server := New(nil, io.Discard, io.Discard)
	uri := "file:///workspace/replaced.asp"
	oldDoc := core.NewTextDocument(uri, "classic-asp", 1, "<% Dim oldValue %>")
	newDoc := core.NewTextDocument(uri, "classic-asp", 1, "<% Dim newValue %>")
	parsed := core.ParseDocument(uri, oldDoc.Text, core.Settings{DefaultLanguage: "VBScript"})
	inflight := newSemanticTokensInflight()
	inflight.uri = uri
	inflight.document = oldDoc
	inflight.version = oldDoc.Version
	inflight.text = oldDoc.Text
	key := semanticTokensResultID(uri, oldDoc.Version)

	server.mu.Lock()
	server.documents[uri] = oldDoc
	inflight.generation = server.semanticTokensGenerationLocked(uri)
	server.semanticInflight[key] = inflight
	server.documents[uri] = newDoc
	server.mu.Unlock()

	server.finishDeferredSemanticTokens(uri, oldDoc.Version, parsed, nil, key, key, inflight)
	if !inflight.wait(context.Background()) {
		t.Fatal("invalidated deferred result did not complete")
	}

	server.mu.Lock()
	_, cachePresent := server.semantic[uri]
	_, flightStillRegistered := server.semanticInflight[key]
	server.mu.Unlock()
	if cachePresent || flightStillRegistered {
		t.Fatalf("stale deferred result was published: cache=%v flight=%v", cachePresent, flightStillRegistered)
	}
}

func TestInvalidatedSemanticTokenFlightUnblocksWaiters(t *testing.T) {
	inflight := newSemanticTokensInflight()
	done := make(chan struct{})
	go func() {
		if !inflight.wait(context.Background()) {
			t.Errorf("in-flight waiter did not complete")
		}
		close(done)
	}()

	inflight.invalidate()
	select {
	case <-done:
	case <-time.After(time.Second):
		t.Fatal("invalidated semantic token flight did not unblock its waiter")
	}
}

func TestSemanticTokenWaiterHonorsCanceledContext(t *testing.T) {
	inflight := newSemanticTokensInflight()
	ctx, cancel := context.WithCancel(context.Background())
	cancel()

	if inflight.wait(ctx) {
		t.Fatal("canceled semantic token waiter reported completion")
	}
}
