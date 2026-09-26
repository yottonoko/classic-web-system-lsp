package lspserver

import (
	"context"
	"fmt"
	"io"
	"strings"
	"sync"
	"sync/atomic"
	"testing"
	"time"

	"github.com/yottonoko/classic-web-system-lsp/internal/core"
	"github.com/yottonoko/classic-web-system-lsp/internal/lsp"
)

func TestShutdownCancelsReferenceAndSemanticBackgroundContexts(t *testing.T) {
	server := newDocumentOpenCancellationTestServer(t, t.TempDir(), io.Discard)
	ctx, cancel := context.WithCancel(context.Background())
	defer cancel()
	flight := newSemanticTokensInflight()
	defer flight.invalidate()
	server.mu.Lock()
	server.referenceBatch[referenceBatchCacheKey("file:///page.asp", 1, server.referenceGeneration)] = &workspaceReferenceBatchState{ctx: ctx, cancel: cancel, done: make(chan struct{})}
	server.semanticInflight["flight"] = flight
	server.mu.Unlock()
	server.shutdownRuntimeCaches()
	if ctx.Err() == nil {
		t.Error("shutdown left the reference worker context active")
	}
	if flight.context().Err() == nil {
		t.Error("shutdown left the semantic worker context active")
	}
}

func TestShutdownRejectsNewReferenceBatch(t *testing.T) {
	server := newDocumentOpenCancellationTestServer(t, t.TempDir(), io.Discard)
	server.shutdownRuntimeCaches()
	parsed := core.ParseDocument("file:///page.asp", "<% Dim value %>", core.Settings{})
	server.logWorkspaceReferenceBatch(parsed.URI, 1, parsed, "value", []vbUsageDeclaration{{Name: "value", Kind: "variable"}})
	server.mu.Lock()
	count := len(server.referenceBatch)
	for _, state := range server.referenceBatch {
		if state.cancel != nil {
			state.cancel()
		}
	}
	server.mu.Unlock()
	if count != 0 {
		t.Fatalf("shutdown server registered %d reference batches", count)
	}
}

func TestShutdownWaitsForRegisteredBackgroundAnalysis(t *testing.T) {
	server := newDocumentOpenCancellationTestServer(t, t.TempDir(), io.Discard)
	server.backgroundAnalysisWorkers.Add(1)
	done := make(chan struct{})
	go func() { server.shutdownRuntimeCaches(); close(done) }()
	// The shutdown flag is the admission barrier and must precede worker joining.
	deadline := time.Now().Add(time.Second)
	for {
		server.mu.Lock()
		shutdown := server.shutdown
		server.mu.Unlock()
		if shutdown {
			break
		}
		if time.Now().After(deadline) {
			server.backgroundAnalysisWorkers.Done()
			<-done
			t.Fatal("shutdown did not start")
		}
		time.Sleep(time.Millisecond)
	}
	select {
	case <-done:
		server.backgroundAnalysisWorkers.Done()
		t.Fatal("shutdown returned while a registered background worker was active")
	case <-time.After(20 * time.Millisecond):
	}
	server.backgroundAnalysisWorkers.Done()
	select {
	case <-done:
	case <-time.After(time.Second):
		t.Fatal("shutdown did not finish after worker completion")
	}
}

func TestLegacyBackgroundBuildIsCoalescedAndCancelledOnShutdown(t *testing.T) {
	server := newDocumentOpenCancellationTestServer(t, t.TempDir(), io.Discard)
	server.settings.VBScriptAssumeUndefinedGlobals = true
	key := legacyUndefinedGlobalBuildKey{generation: server.graphGeneration, settingsFingerprint: server.legacyUndefinedGlobalSettingsFingerprint(nil)}
	// An existing owner keeps background callers waiting without filesystem I/O.
	server.legacyUndefinedGlobalBuilds = map[legacyUndefinedGlobalBuildKey]*legacyUndefinedGlobalBuild{key: {done: make(chan struct{})}}
	if !server.scheduleLegacyUndefinedGlobals() {
		t.Fatal("background build was not admitted")
	}
	if server.scheduleLegacyUndefinedGlobals() {
		t.Fatal("duplicate background build was admitted")
	}
	done := make(chan struct{})
	go func() { server.shutdownRuntimeCaches(); close(done) }()
	select {
	case <-done:
	case <-time.After(time.Second):
		t.Fatal("shutdown did not cancel the catalog waiter")
	}
	if server.scheduleLegacyUndefinedGlobals() {
		t.Fatal("background build was admitted after shutdown")
	}
}

func TestShutdownJoinsDeferredSemanticAnalysisWaitingForJavaScript(t *testing.T) {
	t.Setenv("ASP_LSP_TEST_SEMANTIC_TOKENS_LARGE_SOURCE_THRESHOLD", "1")
	server := newDocumentOpenCancellationTestServer(t, t.TempDir(), io.Discard)
	uri := "file:///deferred.asp"
	server.documents[uri] = core.NewTextDocument(uri, "classic-asp", 1, "<script>var value = 1;</script>")
	server.semanticTokensRefreshSupported = true
	server.javascriptMu.Lock()
	var unlock sync.Once
	release := func() { unlock.Do(server.javascriptMu.Unlock) }
	defer release()
	tokens := server.semanticTokensContext(context.Background(), uri)
	if !strings.HasSuffix(tokens.ResultID, ":partial") {
		t.Fatalf("expected deferred tokens, got %q", tokens.ResultID)
	}
	done := make(chan struct{})
	go func() { server.shutdownRuntimeCaches(); close(done) }()
	select {
	case <-done:
	case <-time.After(time.Second):
		release()
		<-done
		t.Fatal("shutdown did not cancel and join deferred JavaScript analysis")
	}
	server.mu.Lock()
	flights := len(server.semanticInflight)
	server.mu.Unlock()
	if flights != 0 {
		t.Fatalf("shutdown left %d semantic flights", flights)
	}
}

func TestCancelledReferenceDescriptorsDoNotPopulateCaches(t *testing.T) {
	server := newDocumentOpenCancellationTestServer(t, t.TempDir(), io.Discard)
	parsed := core.ParseDocument("file:///cancelled.asp", "<% Dim value %>", core.Settings{})
	ctx, cancel := context.WithCancel(context.Background())
	cancel()
	descriptors := server.workspaceReferenceQueryDescriptorsContext(ctx, parsed, []vbUsageDeclaration{{Name: "value", Kind: "variable"}}, []*core.ParsedDocument{parsed})
	if len(descriptors) != 0 || len(server.referenceDescriptorFingerprints) != 0 {
		t.Fatal("cancelled descriptor preparation published cache state")
	}
}

func TestReferenceFingerprintsPreserveNestedWorkerContext(t *testing.T) {
	pool := &analysisWorkerPool{}
	pool.setWorkers(2)
	index := newWorkspaceReferenceIndex()
	index.workers = pool
	parsed := core.ParseDocument("file:///nested.asp", "<% Dim value : value = 1 %>", core.Settings{})
	ctx, cancel := context.WithTimeout(context.Background(), time.Second)
	defer cancel()
	ready := make(chan struct{})
	var arrivals atomic.Int32
	results := make([]map[string]string, 2)
	done := make(chan struct{})
	go func() {
		defer close(done)
		pool.parallelForBulk(ctx, 2, func(workerCtx context.Context, position int) {
			if arrivals.Add(1) == 2 {
				close(ready)
			}
			select {
			case <-ready:
			case <-ctx.Done():
				return
			}
			results[position] = index.semanticFingerprintsForNamesContext(workerCtx, []string{"value"}, []*core.ParsedDocument{parsed})
		})
	}()
	select {
	case <-done:
	case <-time.After(2 * time.Second):
		t.Fatal("nested fingerprint analysis did not return")
	}
	for _, result := range results {
		if result["value"] == "" {
			t.Fatal("nested fingerprint analysis exhausted its worker pool")
		}
	}
}

func TestAbortingOldSemanticFlightPreservesReplacementResults(t *testing.T) {
	for _, replacementActive := range []bool{false, true} {
		t.Run(fmt.Sprintf("replacementActive=%v", replacementActive), func(t *testing.T) {
			server := newDocumentOpenCancellationTestServer(t, t.TempDir(), io.Discard)
			uri := "file:///replacement.asp"
			oldDoc := core.NewTextDocument(uri, "classic-asp", 1, "<% Dim oldValue %>")
			server.documents[uri] = oldDoc
			old := newSemanticTokensInflight()
			defer old.invalidate()
			old.uri, old.document, old.version, old.text = uri, oldDoc, 1, oldDoc.Text
			old.generation = server.semanticTokensGenerationLocked(uri)
			key := semanticTokensResultID(uri, 1)
			if replacementActive {
				replacement := newSemanticTokensInflight()
				defer replacement.invalidate()
				server.semanticInflight[key] = replacement
			} else {
				server.documents[uri] = core.NewTextDocument(uri, "classic-asp", 1, "<% Dim newValue %>")
			}
			server.semantic[uri] = semanticTokenCache{Version: 1, Tokens: lsp.SemanticTokens{ResultID: key, Data: []int{0, 0, 3, 1, 0}}}
			server.semanticHistory[key] = []int{0, 0, 3, 1, 0}
			server.abortSemanticTokensFlight(key, old)
			if _, ok := server.semantic[uri]; !ok {
				t.Fatal("aborting an old flight erased the replacement token cache")
			}
			if _, ok := server.semanticHistory[key]; !ok {
				t.Fatal("aborting an old flight erased replacement delta history")
			}
		})
	}
}
