package lspserver

import (
	"context"
	"io"
	"reflect"
	"strings"
	"sync"
	"testing"

	"github.com/yottonoko/classic-web-system-lsp/internal/core"
)

func TestReferenceAggregateCacheTracksScopePlansAndEdits(t *testing.T) {
	server := New(nil, io.Discard, io.Discard)
	t.Cleanup(server.shutdownRuntimeCaches)
	parse := func(uri, text string) *core.ParsedDocument {
		return core.ParseDocument(uri, text, core.Settings{DefaultLanguage: "VBScript"})
	}
	origin := parse("file:///origin.asp", "<% Dim value : value = 1 %>")
	use := parse("file:///use.asp", "<% Response.Write value %>")
	shadow := parse("file:///shadow.asp", "<% Dim value : value = 2 %>")
	plans := []workspaceReferenceBatchPlan{{target: workspaceReferenceBatchTarget{name: "value", unqualified: true}}}
	check := func(documents []*core.ParsedDocument, currentOrigin *core.ParsedDocument, currentPlans []workspaceReferenceBatchPlan, progress bool) {
		t.Helper()
		got := server.aggregateWorkspaceReferenceBatch(context.Background(), currentOrigin, documents, currentPlans, progress)
		server.referenceWorkspaceIndex.aggregateCache.clear()
		want := server.aggregateWorkspaceReferenceBatch(context.Background(), currentOrigin, documents, currentPlans, progress)
		if !reflect.DeepEqual(got, want) {
			t.Fatalf("cached result = %#v, fresh = %#v", got, want)
		}
	}
	docs := []*core.ParsedDocument{origin, use, shadow}
	first := server.aggregateWorkspaceReferenceBatch(context.Background(), origin, docs, plans, true)
	if server.referenceWorkspaceIndex.aggregateCache.estimateBytes() == 0 {
		t.Fatal("aggregate was not cached")
	}
	second := server.aggregateWorkspaceReferenceBatch(context.Background(), origin, docs, plans, true)
	if &first.counts[0] != &second.counts[0] {
		t.Fatal("unchanged aggregate was recomputed")
	}
	var workers sync.WaitGroup
	for range 8 {
		workers.Go(func() {
			got := server.aggregateWorkspaceReferenceBatch(context.Background(), origin, docs, plans, true)
			if !reflect.DeepEqual(first, got) {
				t.Error("concurrent aggregate changed cached counts")
			}
		})
	}
	workers.Wait()
	docs[1] = parse(use.URI, "<% ' comment-only edit\nResponse.Write value %>")
	unchanged := server.aggregateWorkspaceReferenceBatch(context.Background(), origin, docs, plans, true)
	if &unchanged.counts[0] != &first.counts[0] {
		t.Fatal("count-preserving edit discarded aggregate cache")
	}
	check(docs, origin, plans, true)
	check([]*core.ParsedDocument{shadow, origin}, origin, plans, true)
	check(docs, shadow, plans, true)
	check(docs, origin, []workspaceReferenceBatchPlan{{target: workspaceReferenceBatchTarget{name: "value", callOnly: true}}}, false)
	docs[1] = parse(use.URI, "<% Response.Write value : Response.Write value %>")
	check(docs, origin, plans, true)
	server.referenceWorkspaceIndex.clear()
	if server.referenceWorkspaceIndex.aggregateCache.estimateBytes() != 0 {
		t.Fatal("cleared index retained aggregate cache")
	}
	check(docs, origin, plans, false)
}

func TestReferenceAggregateCacheBoundAndCancellation(t *testing.T) {
	cache := &workspaceReferenceAggregateCache{}
	result := workspaceReferenceAggregateResult{counts: []int{1}}
	cache.store("small", result)
	cache.store(strings.Repeat("x", workspaceReferenceAggregateCacheMaxBytes), result)
	if _, ok := cache.get("small"); !ok {
		t.Fatal("oversized result displaced bounded cache")
	}
	if cache.estimateBytes() > workspaceReferenceAggregateCacheMaxBytes {
		t.Fatal("cache exceeded memory limit")
	}
	cache.store("cancelled", workspaceReferenceAggregateResult{cancelled: true})
	if _, ok := cache.get("cancelled"); ok {
		t.Fatal("cancelled result was cached")
	}
	server := New(nil, io.Discard, io.Discard)
	t.Cleanup(server.shutdownRuntimeCaches)
	ctx, cancel := context.WithCancel(context.Background())
	cancel()
	parsed := core.ParseDocument("file:///cancel.asp", "<% Dim value %>", core.Settings{})
	result = server.aggregateWorkspaceReferenceBatch(ctx, parsed, []*core.ParsedDocument{parsed}, nil, false)
	if !result.cancelled || server.referenceWorkspaceIndex.aggregateCache.estimateBytes() != 0 {
		t.Fatal("cancelled analysis populated cache")
	}
}
