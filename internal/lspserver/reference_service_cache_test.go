package lspserver

import (
	"context"
	"fmt"
	"io"
	"strings"
	"sync"
	"sync/atomic"
	"testing"

	"github.com/yottonoko/classic-web-system-lsp/internal/core"
	"github.com/yottonoko/classic-web-system-lsp/internal/lsp"
)

func TestJavaScriptServiceResultCacheSharesInflightAndBoundsEntries(t *testing.T) {
	cache := newJavaScriptServiceResultCache()
	var loads atomic.Int32
	started := make(chan struct{})
	release := make(chan struct{})
	load := func() ([]byte, error) {
		if loads.Add(1) == 1 {
			close(started)
		}
		<-release
		return []byte(`[{"uri":"file:///cached.asp"}]`), nil
	}

	const callers = 8
	results := make(chan []byte, callers)
	var requests sync.WaitGroup
	requests.Add(callers)
	for range callers {
		go func() {
			defer requests.Done()
			raw, err := cache.request(context.Background(), "shared", load)
			if err != nil {
				t.Errorf("cached request: %v", err)
				return
			}
			results <- raw
		}()
	}
	<-started
	close(release)
	requests.Wait()
	close(results)
	if loads.Load() != 1 {
		t.Fatalf("loads = %d, want 1", loads.Load())
	}
	for raw := range results {
		if string(raw) != `[{"uri":"file:///cached.asp"}]` {
			t.Fatalf("result = %s", raw)
		}
	}

	for index := 0; index < javaScriptServiceResultCacheEntries+16; index++ {
		key := fmt.Sprintf("entry-%d", index)
		if _, err := cache.request(context.Background(), key, func() ([]byte, error) { return []byte(key), nil }); err != nil {
			t.Fatal(err)
		}
	}
	cache.mu.Lock()
	entryCount := len(cache.entries)
	byteCount := cache.bytes
	cache.mu.Unlock()
	if entryCount > javaScriptServiceResultCacheEntries {
		t.Fatalf("entries = %d, limit = %d", entryCount, javaScriptServiceResultCacheEntries)
	}
	if byteCount > javaScriptServiceResultCacheBytes {
		t.Fatalf("bytes = %d, limit = %d", byteCount, javaScriptServiceResultCacheBytes)
	}
}

func TestJavaScriptServiceResultCacheKeyIncludesGenerationAndRequest(t *testing.T) {
	request := &javaScriptRequest{
		active: &javaScriptVirtualFile{uri: "file:///page.asp.__asp_client.js"},
	}
	request.state.Generation = 4
	first := javaScriptServiceResultCacheKey(request, "textDocument/references", []byte(`{"position":{"line":1}}`))
	request.state.Generation = 5
	second := javaScriptServiceResultCacheKey(request, "textDocument/references", []byte(`{"position":{"line":1}}`))
	third := javaScriptServiceResultCacheKey(request, "textDocument/references", []byte(`{"position":{"line":2}}`))
	if first == second || second == third {
		t.Fatalf("cache keys did not distinguish generation or params: %q %q %q", first, second, third)
	}
	if key := javaScriptServiceResultCacheKey(request, "textDocument/hover", nil); key != "" {
		t.Fatalf("uncached method key = %q", key)
	}
}

func TestJavaScriptServiceResultCacheReusesRemappedReferencesAndInvalidatesWithDocument(t *testing.T) {
	server := New(strings.NewReader(""), io.Discard, io.Discard)
	server.rootPath = t.TempDir()
	uri := "file:///javascript-reference-cache.asp"
	source := "<script>\nconst cachedValue = 1;\ncachedValue;\n</script>"
	document := core.NewTextDocument(uri, "classic-asp", 1, source)
	server.documents[uri] = document
	position := document.PositionAt(strings.LastIndex(source, "cachedValue") + 2)
	requestReferences := func(position lsp.Position) []lsp.Location {
		var locations []lsp.Location
		if !server.javaScriptLanguageServiceRequest(context.Background(), uri, position, "textDocument/references", map[string]any{
			"context": map[string]any{"includeDeclaration": true},
		}, &locations) {
			t.Fatal("JavaScript references request failed")
		}
		return locations
	}
	firstLocations := requestReferences(position)
	preparation := server.javascriptPreparation
	if preparation == nil || preparation.serviceCache == nil {
		t.Fatal("JavaScript service cache was not attached to the project preparation")
	}
	preparation.serviceCache.mu.Lock()
	firstEntries := len(preparation.serviceCache.entries)
	preparation.serviceCache.mu.Unlock()
	secondLocations := requestReferences(position)
	preparation.serviceCache.mu.Lock()
	secondEntries := len(preparation.serviceCache.entries)
	preparation.serviceCache.mu.Unlock()
	if firstEntries != 1 || secondEntries != firstEntries {
		t.Fatalf("cache entries changed across identical request: first=%d second=%d", firstEntries, secondEntries)
	}
	if fmt.Sprint(firstLocations) != fmt.Sprint(secondLocations) || len(secondLocations) != 2 {
		t.Fatalf("cached remapped locations changed: first=%#v second=%#v", firstLocations, secondLocations)
	}

	updatedSource := "<script>\nconst cachedValue = 1;\ncachedValue;\ncachedValue;\n</script>"
	previousServiceCache := preparation.serviceCache
	server.mu.Lock()
	document.Update(2, updatedSource)
	server.deleteParsedCacheForURILocked(uri)
	server.markJavaScriptDocumentChangedLocked(uri)
	server.mu.Unlock()
	updatedLocations := requestReferences(document.PositionAt(strings.LastIndex(updatedSource, "cachedValue") + 2))
	if server.javascriptPreparation != preparation {
		t.Fatal("changed document replaced reusable JavaScript project preparation")
	}
	if server.javascriptPreparation.serviceCache == previousServiceCache {
		t.Fatal("changed document reused stale JavaScript service result cache")
	}
	if len(updatedLocations) != 3 {
		t.Fatalf("updated references = %#v, want 3 locations", updatedLocations)
	}
}

func TestEmbeddedReferenceRangesAreReusedPerParsedDocument(t *testing.T) {
	uri := "file:///embedded-cache.asp"
	source := `<DIV class="card oldName"><span></span></DIV>
<style>.oldName { color: red; }</style>
<script>document.querySelector(".oldName");</script>`
	parsed := core.ParseDocument(uri, source, core.Settings{DefaultLanguage: "VBScript"})
	first := embeddedReferenceRangesFor(parsed)
	second := embeddedReferenceRangesFor(parsed)
	if first != second {
		t.Fatal("unchanged parsed document rebuilt embedded reference ranges")
	}
	if got := len(first.tagNames("div")); got != 2 {
		t.Fatalf("div tag ranges = %d, want 2", got)
	}
	if got := len(first.classNames("oldName")); got != 3 {
		t.Fatalf("oldName class ranges = %d, want 3", got)
	}

	updated := core.ParseDocument(uri, source+`<div class="oldName"></div>`, core.Settings{DefaultLanguage: "VBScript"})
	updatedRanges := embeddedReferenceRangesFor(updated)
	if updatedRanges == first {
		t.Fatal("changed parsed document reused stale embedded reference ranges")
	}
	if got := len(updatedRanges.classNames("oldName")); got != 4 {
		t.Fatalf("updated oldName class ranges = %d, want 4", got)
	}
}
