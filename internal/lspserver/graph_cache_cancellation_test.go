package lspserver

import (
	"context"
	"encoding/json"
	"reflect"
	"strings"
	"testing"

	"github.com/yottonoko/classic-web-system-lsp/internal/core"
	"github.com/yottonoko/classic-web-system-lsp/internal/graph"
	"github.com/yottonoko/classic-web-system-lsp/internal/lsp"
)

func TestCancelledGraphBuildDoesNotCreateCacheOrDiskWrite(t *testing.T) {
	server := newRuntimeCacheTestServer(t)
	uri := "file:///workspace/cancelled.asp"
	parsed := core.ParseDocument(uri, `<% Dim Value : Value = 1 %>`, core.Settings{DefaultLanguage: "VBScript"})
	key := server.graphCacheKey(graphCommandArg{Scope: "document", URI: uri})
	ctx, cancel := context.WithCancel(context.Background())
	defer cancel()

	payload, complete := server.buildDocumentSetGraphWithProgress(ctx, "document", "", []*core.ParsedDocument{parsed}, true, false, func(string, string, int, int) {
		cancel()
	})
	if complete || !reflect.DeepEqual(payload, graph.Payload{}) || ctx.Err() == nil {
		t.Fatalf("cancelled graph build = payload=%#v complete=%t ctxErr=%v; want no payload and cancellation", payload, complete, ctx.Err())
	}
	server.waitForAsyncDiskCacheWrites()
	server.mu.Lock()
	_, cached := server.graphCache[key]
	pending := graphCachePendingWriteKeysLocked(server)
	server.mu.Unlock()
	if cached {
		t.Fatalf("cancelled graph build populated memory cache key %q", key)
	}
	if len(pending) != 0 {
		t.Fatalf("cancelled graph build scheduled disk writes: %v", pending)
	}
	if _, ok := server.diskCacheForUse().ReadGraphPayload(server.graphPayloadDiskSettingsKey(key)); ok {
		t.Fatal("cancelled graph build wrote a disk graph payload")
	}
}

func TestCancelledShowIncomingGraphBuildPreservesCache(t *testing.T) {
	server := newRuntimeCacheTestServer(t)
	uri := "file:///workspace/show-incoming.asp"
	source := `<% Dim Value : Value = 1 %>`
	server.documents[uri] = core.NewTextDocument(uri, "classic-asp", 1, source)
	parsed := server.parseText(uri, source, "VBScript")
	server.waitForAsyncDiskCacheWrites()
	server.resumeAsyncDiskCacheWrites()
	arg := graphCommandArg{Scope: "document", URI: uri, ShowIncomingDocumentIncludes: lsp.BoolPtr(true)}
	key := server.graphCacheKey(arg)
	valid := graph.Payload{
		Scope: "document",
		URI:   uri,
		Nodes: []graph.Node{{ID: "valid-cache-node", Label: "valid", Kind: "file"}},
		Edges: []graph.Edge{},
		Stats: map[string]int{"files": 1},
	}
	server.storeGraphPayload(key, valid)
	server.waitForAsyncDiskCacheWrites()
	beforeDisk, ok := server.diskCacheForUse().ReadGraphPayload(server.graphPayloadDiskSettingsKey(key))
	if !ok {
		t.Fatal("failed to seed the valid graph payload on disk")
	}

	ctx, cancel := context.WithCancel(context.Background())
	defer cancel()
	payload, complete := server.buildGraphPayloadContextWithDocuments(ctx, arg, func(label, _ string, current, total int) {
		if strings.HasSuffix(label, ".linkUnresolved") && current == total {
			cancel()
		}
	}, []*core.ParsedDocument{parsed}, "")
	if complete || !reflect.DeepEqual(payload, graph.Payload{}) || ctx.Err() == nil {
		t.Fatalf("cancelled showIncoming graph build = payload=%#v complete=%t ctxErr=%v; want no payload and cancellation", payload, complete, ctx.Err())
	}
	server.waitForAsyncDiskCacheWrites()
	server.mu.Lock()
	got, cached := server.graphCache[key]
	pending := graphCachePendingWriteKeysLocked(server)
	server.mu.Unlock()
	if !cached || !reflect.DeepEqual(got, valid) {
		t.Fatalf("preexisting graph cache = %#v, cached=%t; want %#v preserved", got, cached, valid)
	}
	if len(pending) != 0 {
		t.Fatalf("cancelled showIncoming graph build scheduled disk writes: %v", pending)
	}
	afterDisk, ok := server.diskCacheForUse().ReadGraphPayload(server.graphPayloadDiskSettingsKey(key))
	if !ok || !reflect.DeepEqual(afterDisk, beforeDisk) {
		t.Fatalf("preexisting disk graph cache changed: before=%#v after=%#v ok=%t", beforeDisk, afterDisk, ok)
	}
}

func TestGraphPayloadCommitKeepsCommittedStateAfterCancellation(t *testing.T) {
	server := newRuntimeCacheTestServer(t)
	key := "committed-after-cancel"
	payload := graph.Payload{
		Scope: "document",
		Nodes: []graph.Node{{ID: "committed", Label: "committed", Kind: "file"}},
		Stats: map[string]int{"files": 1},
	}
	ctx, cancel := context.WithCancel(context.Background())
	generation := server.graphGenerationSnapshot()
	if !server.storeGraphPayloadContext(ctx, generation, key, payload) {
		t.Fatal("storeGraphPayloadContext() = false; want committed transaction")
	}
	cancel()
	server.waitForAsyncDiskCacheWrites()

	server.mu.Lock()
	got, cached := server.graphCache[key]
	pending := graphCachePendingWriteKeysLocked(server)
	server.mu.Unlock()
	if !cached || !reflect.DeepEqual(got, payload) {
		t.Fatalf("committed graph cache = %#v, cached=%t; want %#v", got, cached, payload)
	}
	if len(pending) != 0 {
		t.Fatalf("committed graph cache left pending disk writes: %v", pending)
	}
	diskPayload, ok := server.diskCacheForUse().ReadGraphPayload(server.graphPayloadDiskSettingsKey(key))
	if !ok {
		t.Fatal("committed graph payload was not persisted")
	}
	var decoded graph.Payload
	if err := json.Unmarshal(diskPayload.Payload, &decoded); err != nil || !reflect.DeepEqual(decoded, payload) {
		t.Fatalf("committed graph disk cache = %#v, decodeErr=%v; want %#v", diskPayload, err, payload)
	}
}

func TestStaleGraphPayloadCommitPreservesPreviousCache(t *testing.T) {
	server := newRuntimeCacheTestServer(t)
	key := "stale-commit"
	previous := graph.Payload{
		Scope: "document",
		Nodes: []graph.Node{{ID: "previous", Label: "previous", Kind: "file"}},
		Stats: map[string]int{"files": 1},
	}
	generation := server.graphGenerationSnapshot()
	if !server.storeGraphPayloadContext(context.Background(), generation, key, previous) {
		t.Fatal("failed to seed previous graph cache")
	}
	server.waitForAsyncDiskCacheWrites()
	diskBefore, ok := server.diskCacheForUse().ReadGraphPayload(server.graphPayloadDiskSettingsKey(key))
	if !ok {
		t.Fatal("failed to seed previous graph disk cache")
	}

	server.mu.Lock()
	server.graphGeneration++
	server.mu.Unlock()
	stale := graph.Payload{
		Scope: "document",
		Nodes: []graph.Node{{ID: "stale", Label: "stale", Kind: "file"}},
		Stats: map[string]int{"files": 1},
	}
	if server.storeGraphPayloadContext(context.Background(), generation, key, stale) {
		t.Fatal("stale graph payload commit = true; want rejection")
	}
	server.waitForAsyncDiskCacheWrites()
	server.mu.Lock()
	got, cached := server.graphCache[key]
	pending := graphCachePendingWriteKeysLocked(server)
	server.mu.Unlock()
	if !cached || !reflect.DeepEqual(got, previous) {
		t.Fatalf("previous graph cache = %#v, cached=%t; want %#v", got, cached, previous)
	}
	if len(pending) != 0 {
		t.Fatalf("stale graph commit scheduled disk writes: %v", pending)
	}
	diskAfter, ok := server.diskCacheForUse().ReadGraphPayload(server.graphPayloadDiskSettingsKey(key))
	if !ok || !reflect.DeepEqual(diskAfter, diskBefore) {
		t.Fatalf("previous graph disk cache changed: before=%#v after=%#v ok=%t", diskBefore, diskAfter, ok)
	}
}

func TestDiskGraphRestoreRejectsInvalidationDuringRead(t *testing.T) {
	server := newRuntimeCacheTestServer(t)
	key := "restore-generation-drift"
	stale := graph.Payload{
		Scope: "document",
		Nodes: []graph.Node{{ID: "stale-disk", Label: "stale-disk", Kind: "file"}},
		Stats: map[string]int{"files": 1},
	}
	server.storeGraphPayload(key, stale)
	server.waitForAsyncDiskCacheWrites()
	diskBefore, ok := server.diskCacheForUse().ReadGraphPayload(server.graphPayloadDiskSettingsKey(key))
	if !ok {
		t.Fatal("failed to seed stale graph disk cache")
	}
	server.mu.Lock()
	delete(server.graphCache, key)
	server.mu.Unlock()

	ctx := context.WithValue(context.Background(), graphPayloadRestoreBeforeCommitContextKey{}, func(server *Server) {
		server.invalidateGraphBackground()
	})
	payload, restored := server.cachedGraphPayloadContext(ctx, key)
	if restored || !reflect.DeepEqual(payload, graph.Payload{}) {
		t.Fatalf("invalidated disk graph restore = payload=%#v restored=%t; want empty/false", payload, restored)
	}
	server.mu.Lock()
	_, cached := server.graphCache[key]
	pending := graphCachePendingWriteKeysLocked(server)
	server.mu.Unlock()
	if cached {
		t.Fatal("invalidated disk graph restore repopulated memory cache")
	}
	if len(pending) != 0 {
		t.Fatalf("invalidated disk graph restore scheduled disk writes: %v", pending)
	}
	diskAfter, ok := server.diskCacheForUse().ReadGraphPayload(server.graphPayloadDiskSettingsKey(key))
	if !ok || !reflect.DeepEqual(diskAfter, diskBefore) {
		t.Fatalf("invalidated disk graph cache changed: before=%#v after=%#v ok=%t", diskBefore, diskAfter, ok)
	}
}

func TestDiskGraphRestorePreservesConcurrentValidCacheAfterInvalidation(t *testing.T) {
	server := newRuntimeCacheTestServer(t)
	key := "restore-concurrent-cache"
	stale := graph.Payload{
		Scope: "document",
		Nodes: []graph.Node{{ID: "stale-disk", Label: "stale-disk", Kind: "file"}},
		Stats: map[string]int{"files": 1},
	}
	previous := graph.Payload{
		Scope: "document",
		Nodes: []graph.Node{{ID: "current-memory", Label: "current-memory", Kind: "file"}},
		Stats: map[string]int{"files": 1},
	}
	server.storeGraphPayload(key, stale)
	server.waitForAsyncDiskCacheWrites()
	diskBefore, ok := server.diskCacheForUse().ReadGraphPayload(server.graphPayloadDiskSettingsKey(key))
	if !ok {
		t.Fatal("failed to seed stale graph disk cache")
	}
	server.mu.Lock()
	delete(server.graphCache, key)
	server.mu.Unlock()

	ctx := context.WithValue(context.Background(), graphPayloadRestoreBeforeCommitContextKey{}, func(server *Server) {
		server.invalidateGraphBackground()
		server.mu.Lock()
		server.graphCache[key] = previous
		server.mu.Unlock()
	})
	payload, restored := server.cachedGraphPayloadContext(ctx, key)
	if restored || !reflect.DeepEqual(payload, graph.Payload{}) {
		t.Fatalf("stale disk restore over concurrent cache = payload=%#v restored=%t; want empty/false", payload, restored)
	}
	server.mu.Lock()
	got, cached := server.graphCache[key]
	pending := graphCachePendingWriteKeysLocked(server)
	server.mu.Unlock()
	if !cached || !reflect.DeepEqual(got, previous) {
		t.Fatalf("concurrent graph cache = %#v cached=%t; want %#v preserved", got, cached, previous)
	}
	if len(pending) != 0 {
		t.Fatalf("stale disk graph restore scheduled disk writes: %v", pending)
	}
	diskAfter, ok := server.diskCacheForUse().ReadGraphPayload(server.graphPayloadDiskSettingsKey(key))
	if !ok || !reflect.DeepEqual(diskAfter, diskBefore) {
		t.Fatalf("stale disk graph cache changed: before=%#v after=%#v ok=%t", diskBefore, diskAfter, ok)
	}
}

func TestCancelledDiskGraphRestoreDoesNotRepopulateMemory(t *testing.T) {
	server := newRuntimeCacheTestServer(t)
	key := "restore-cancelled"
	payload := graph.Payload{
		Scope: "document",
		Nodes: []graph.Node{{ID: "cancelled-disk", Label: "cancelled-disk", Kind: "file"}},
		Stats: map[string]int{"files": 1},
	}
	server.storeGraphPayload(key, payload)
	server.waitForAsyncDiskCacheWrites()
	diskBefore, ok := server.diskCacheForUse().ReadGraphPayload(server.graphPayloadDiskSettingsKey(key))
	if !ok {
		t.Fatal("failed to seed graph disk cache")
	}
	server.mu.Lock()
	delete(server.graphCache, key)
	server.mu.Unlock()

	ctx, cancel := context.WithCancel(context.Background())
	defer cancel()
	ctx = context.WithValue(ctx, graphPayloadRestoreBeforeCommitContextKey{}, func(*Server) {
		cancel()
	})
	got, restored := server.cachedGraphPayloadContext(ctx, key)
	if restored || !reflect.DeepEqual(got, graph.Payload{}) {
		t.Fatalf("cancelled disk graph restore = payload=%#v restored=%t; want empty/false", got, restored)
	}
	server.mu.Lock()
	_, cached := server.graphCache[key]
	pending := graphCachePendingWriteKeysLocked(server)
	server.mu.Unlock()
	if cached {
		t.Fatal("cancelled disk graph restore repopulated memory cache")
	}
	if len(pending) != 0 {
		t.Fatalf("cancelled disk graph restore scheduled disk writes: %v", pending)
	}
	diskAfter, ok := server.diskCacheForUse().ReadGraphPayload(server.graphPayloadDiskSettingsKey(key))
	if !ok || !reflect.DeepEqual(diskAfter, diskBefore) {
		t.Fatalf("cancelled disk graph cache changed: before=%#v after=%#v ok=%t", diskBefore, diskAfter, ok)
	}
}

func TestGraphCommandBuildRejectsProgressGenerationDrift(t *testing.T) {
	server := New(nil, nil, nil)
	uri := "file:///workspace/drift.asp"
	server.documents[uri] = core.NewTextDocument(uri, "classic-asp", 1, `<% Dim Value : Value = 1 %>`)
	arg := graphCommandArg{Scope: "document", URI: uri}
	advanced := false
	result := server.buildGraphPayloadContextResult(context.Background(), arg, func(string, string, int, int) {
		if advanced {
			return
		}
		advanced = true
		server.mu.Lock()
		server.graphGeneration++
		server.mu.Unlock()
	})
	if result.complete || !reflect.DeepEqual(result.payload, graph.Payload{}) || result.err != errGraphCollectionGeneration {
		t.Fatalf("generation-drift graph build = payload=%#v complete=%t err=%v; want empty/incomplete/generation error", result.payload, result.complete, result.err)
	}
	cacheKey := server.graphCacheKey(arg)
	server.mu.Lock()
	_, cached := server.graphCache[cacheKey]
	server.mu.Unlock()
	if cached {
		t.Fatal("generation-drift graph build populated memory cache")
	}
}

func TestNavigationCollectionRejectsProgressGenerationDrift(t *testing.T) {
	server := New(nil, nil, nil)
	uri := "file:///workspace/navigation-drift.asp"
	server.documents[uri] = core.NewTextDocument(uri, "classic-asp", 1, `<% Dim Value : Value = 1 %>`)
	advanced := false
	result := server.navigationDocumentsContextWithProgressResult(context.Background(), "document", uri, func(string, string, int, int) {
		if advanced {
			return
		}
		advanced = true
		server.mu.Lock()
		server.graphGeneration++
		server.mu.Unlock()
	})
	if result.complete || len(result.documents) != 0 || result.err != errGraphCollectionGeneration {
		t.Fatalf("generation-drift navigation collection = documents=%d complete=%t err=%v; want empty/incomplete/generation error", len(result.documents), result.complete, result.err)
	}
}

func TestExcelExportRejectsProgressGenerationDrift(t *testing.T) {
	server := New(nil, nil, nil)
	uri := "file:///workspace/export-drift.asp"
	server.workspaceRoots = []workspaceRoot{{URI: "file:///workspace", Path: "/workspace"}}
	server.documents[uri] = core.NewTextDocument(uri, "classic-asp", 1, `<% Dim Value : Value = 1 %>`)
	advanced := false
	payload, ok := server.exportAnalysisPayloadContextWithProgress(context.Background(), analysisExcelExportArg{FileURIs: []string{uri}}, func(string, string, int, int) {
		if advanced {
			return
		}
		advanced = true
		server.mu.Lock()
		server.graphGeneration++
		server.mu.Unlock()
	})
	if ok || !reflect.DeepEqual(payload, graph.Payload{}) {
		t.Fatalf("generation-drift Excel export = payload=%#v ok=%t; want empty/false", payload, ok)
	}
}

func graphCachePendingWriteKeysLocked(server *Server) []string {
	keys := make([]string, 0, len(server.diskCacheWritePending)+len(server.diskCacheWriteDeferred))
	for key := range server.diskCacheWritePending {
		if strings.HasPrefix(key, "graph-payload\x00") {
			keys = append(keys, key)
		}
	}
	for key := range server.diskCacheWriteDeferred {
		if strings.HasPrefix(key, "graph-payload\x00") {
			keys = append(keys, key)
		}
	}
	return keys
}
