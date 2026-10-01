package lspserver

import (
	"fmt"
	"io"
	"strings"
	"testing"
	"time"

	"github.com/yottonoko/classic-web-system-lsp/internal/core"
	workspacepkg "github.com/yottonoko/classic-web-system-lsp/internal/workspace"
)

func registeredParsedAndStoreBytes(server *Server) int64 {
	parsed := parsedDocumentCacheOwnershipForEntries(server.parsedCache)
	store := documentStoreCacheOwnershipForStore(server.documentStore, parsed)
	return addRuntimeCacheBytes(parsed.bytes(), store.bytes())
}

func seedRuntimeCacheOwnerLedgerServer(t *testing.T, documents int) *Server {
	t.Helper()
	server := New(nil, io.Discard, io.Discard)
	t.Cleanup(server.shutdownRuntimeCaches)
	server.mu.Lock()
	defer server.mu.Unlock()
	for index := range documents {
		uri := fmt.Sprintf("file:///site/ledger-%02d.asp", index)
		parsed := core.ParseDocument(uri, strings.Repeat("<% Dim value %>\n", 16+index), core.Settings{DefaultLanguage: "VBScript"})
		parsed.StoreRuntimeAnalysis("test.runtime", strings.Repeat("runtime", 8+index))
		entry := parsedDocumentCacheEntry{Version: 1, Text: parsed.Text, DefaultLanguage: "VBScript", Parsed: parsed}
		server.parsedCache[parsedDocumentCacheKey(uri)] = entry
		switch index % 4 {
		case 0:
			// The text key shares the parsed revision with the document key.
			server.parsedCache[parsedTextCacheKey(uri)] = entry
		case 1:
			server.documentStore.Cache[uri] = &workspacepkg.CachedDocument{URI: uri, Text: parsed.Text, Parsed: parsed, ParseDepth: "full"}
		case 2:
			server.documentStore.Cache[uri] = &workspacepkg.CachedDocument{URI: uri, Text: strings.Clone(parsed.Text), Parsed: map[string]any{"kind": "legacy"}}
		}
	}
	server.parsedCache["text:unparsed"] = parsedDocumentCacheEntry{Version: 1, Text: "<% Dim pending %>", DefaultLanguage: "VBScript"}
	server.documentStore.Cache["file:///site/missing.asp"] = nil
	return server
}

func TestRuntimeCacheOwnerLedgerTracksRegisteredEstimatesAcrossRemovals(t *testing.T) {
	server := seedRuntimeCacheOwnerLedgerServer(t, 12)
	server.mu.Lock()
	defer server.mu.Unlock()
	ledger := server.runtimeCacheOwnerLedgerLocked()
	if got, want := ledger.total(), registeredParsedAndStoreBytes(server); got != want {
		t.Fatalf("ledger total = %d, want registered estimate %d", got, want)
	}
	for _, key := range sortedMapKeys(server.parsedCache) {
		delete(server.parsedCache, key)
		ledger.removeParsedEntry(key)
		if got, want := ledger.total(), registeredParsedAndStoreBytes(server); got != want {
			t.Fatalf("after removing parsed %s ledger total = %d, want %d", key, got, want)
		}
	}
	for _, key := range sortedMapKeys(server.documentStore.Cache) {
		delete(server.documentStore.Cache, key)
		ledger.removeStoreEntry(key)
		if got, want := ledger.total(), registeredParsedAndStoreBytes(server); got != want {
			t.Fatalf("after removing store %s ledger total = %d, want %d", key, got, want)
		}
	}
	if ledger.total() != 0 || len(ledger.parsed) != 0 || len(ledger.source) != 0 || len(ledger.runtime) != 0 {
		t.Fatalf("empty caches left ledger total=%d parsed=%d source=%d runtime=%d", ledger.total(), len(ledger.parsed), len(ledger.source), len(ledger.runtime))
	}
}

func TestRegisteredParsedCachePartialEvictionReportsGlobalDelta(t *testing.T) {
	server := seedRuntimeCacheOwnerLedgerServer(t, 24)
	cache := server.registeredParsedCache()
	server.mu.Lock()
	before := registeredParsedAndStoreBytes(server)
	entriesBefore := len(server.parsedCache)
	server.mu.Unlock()

	freed := cache.Evict(before / 3)

	server.mu.Lock()
	after := registeredParsedAndStoreBytes(server)
	entriesAfter := len(server.parsedCache)
	server.mu.Unlock()
	if entriesAfter == 0 || entriesAfter >= entriesBefore {
		t.Fatalf("partial eviction kept %d of %d parsed entries, want a partial eviction", entriesAfter, entriesBefore)
	}
	if want := subtractRuntimeCacheBytes(before, after); freed != want || freed < before/3 {
		t.Fatalf("partial eviction freed %d, want global delta %d reaching target %d", freed, want, before/3)
	}
}

func TestMemoryPressureCheckDelayScalesWithPreviousCost(t *testing.T) {
	cases := []struct {
		cost time.Duration
		want time.Duration
	}{
		{cost: 0, want: runtimeMemoryPressureDebounce},
		{cost: time.Millisecond, want: runtimeMemoryPressureDebounce},
		{cost: 200 * time.Millisecond, want: 2 * time.Second},
		{cost: time.Minute, want: memoryPressureCheckMaxDelay},
	}
	for _, tc := range cases {
		if got := memoryPressureCheckDelay(tc.cost); got != tc.want {
			t.Fatalf("memoryPressureCheckDelay(%v) = %v, want %v", tc.cost, got, tc.want)
		}
	}
}
