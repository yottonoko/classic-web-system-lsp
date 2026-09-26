package lspserver

import (
	"context"
	"fmt"
	"io"
	"sync"
	"testing"

	"github.com/yottonoko/classic-web-system-lsp/internal/lsp"
)

func TestWorkspaceReferenceNameIndexInvalidatesOnlyExactName(t *testing.T) {
	server := New(nil, io.Discard, io.Discard)
	server.mu.Lock()
	server.ensureWorkspaceReferenceNameIndexLocked()
	for index := 0; index < 20_000; index++ {
		name := fmt.Sprintf("unrelated-%05d", index)
		key := workspaceReferenceTargetKey{URI: "file:///workspace/cache.asp", Name: name, NameHash: uint64(index), Generation: 1}
		server.storeWorkspaceReferenceCountLocked(key, index)
	}
	affectedKey := workspaceReferenceTargetKey{URI: "file:///workspace/affected.asp", Name: "shared", NameHash: 42, Generation: 1}
	collisionKey := workspaceReferenceTargetKey{URI: "file:///workspace/collision.asp", Name: "collision", NameHash: 42, Generation: 1}
	inflight := &workspaceReferenceInflight{done: make(chan struct{})}
	server.storeWorkspaceReferenceCountLocked(affectedKey, 1)
	server.storeWorkspaceReferencePartialCountLocked(affectedKey, 2)
	server.storeWorkspaceReferenceResultLocked(affectedKey, []lsp.Location{{URI: affectedKey.URI}})
	server.storeWorkspaceReferenceInflightLocked(affectedKey, inflight)
	server.storeWorkspaceReferenceCountLocked(collisionKey, 3)
	server.storeWorkspaceReferenceResultLocked(collisionKey, []lsp.Location{{URI: collisionKey.URI}})
	affectedContext, affectedCancel := context.WithCancel(context.Background())
	unrelatedContext, unrelatedCancel := context.WithCancel(context.Background())
	defer unrelatedCancel()
	affectedBatchKey := workspaceReferenceBatchKey{DocumentKey: "affected", Generation: 1}
	unrelatedBatchKey := workspaceReferenceBatchKey{DocumentKey: "unrelated", Generation: 1}
	server.storeWorkspaceReferenceBatchLocked(affectedBatchKey, &workspaceReferenceBatchState{
		ctx: affectedContext, cancel: affectedCancel, nameRevisions: map[string]uint64{"shared": 1},
	})
	server.storeWorkspaceReferenceBatchLocked(unrelatedBatchKey, &workspaceReferenceBatchState{
		ctx: unrelatedContext, cancel: unrelatedCancel, nameRevisions: map[string]uint64{"collision": 1},
	})
	planKey := workspaceReferenceImplicitPlanKey{DocumentKey: "root", IndexRevision: 1}
	server.storeWorkspaceReferenceImplicitPlanLocked(planKey, map[string]map[string]struct{}{
		"shared": {"first": {}}, "collision": {"second": {}},
	})
	server.mu.Unlock()

	server.invalidateWorkspaceReferenceNames(nil, nil, []string{"SHARED"})

	server.mu.Lock()
	defer server.mu.Unlock()
	if _, ok := server.referenceCounts[affectedKey]; ok {
		t.Fatal("affected count survived name invalidation")
	}
	if _, ok := server.referencePartialCounts[affectedKey]; ok {
		t.Fatal("affected partial count survived name invalidation")
	}
	if _, ok := server.referenceResults[affectedKey]; ok {
		t.Fatal("affected location result survived name invalidation")
	}
	if _, ok := server.referenceInflight[affectedKey]; ok || !inflight.stale {
		t.Fatal("affected inflight request was not removed and marked stale")
	}
	if count := server.referenceCounts[collisionKey]; count != 3 {
		t.Fatalf("hash-colliding count was invalidated: got %d", count)
	}
	if _, ok := server.referenceResults[collisionKey]; !ok {
		t.Fatal("hash-colliding location result was invalidated")
	}
	if _, ok := server.referenceBatch[affectedBatchKey]; ok || affectedContext.Err() == nil {
		t.Fatal("affected batch survived or was not canceled")
	}
	if _, ok := server.referenceBatch[unrelatedBatchKey]; !ok || unrelatedContext.Err() != nil {
		t.Fatal("unrelated batch was removed or canceled")
	}
	if _, ok := server.referenceImplicitPlans[planKey]["shared"]; ok {
		t.Fatal("affected implicit plan survived")
	}
	if _, ok := server.referenceImplicitPlans[planKey]["collision"]; !ok {
		t.Fatal("unrelated implicit plan was removed")
	}
	lastKey := workspaceReferenceTargetKey{URI: "file:///workspace/cache.asp", Name: "unrelated-19999", NameHash: 19_999, Generation: 1}
	if count := server.referenceCounts[lastKey]; count != 19_999 {
		t.Fatalf("large unrelated cache entry changed: got %d", count)
	}
}

func TestWorkspaceReferenceNameIndexConcurrentStoresAndInvalidations(t *testing.T) {
	server := New(nil, io.Discard, io.Discard)
	server.mu.Lock()
	server.ensureWorkspaceReferenceNameIndexLocked()
	server.mu.Unlock()
	const workers = 8
	const iterations = 250
	var wait sync.WaitGroup
	wait.Add(workers)
	for worker := 0; worker < workers; worker++ {
		go func(worker int) {
			defer wait.Done()
			name := fmt.Sprintf("worker-%d", worker)
			for iteration := 0; iteration < iterations; iteration++ {
				key := workspaceReferenceTargetKey{
					URI: fmt.Sprintf("file:///workspace/%d/%d.asp", worker, iteration), Name: name, NameHash: workspaceReferenceNameHash(name), Generation: 1,
				}
				server.mu.Lock()
				server.storeWorkspaceReferenceCountLocked(key, iteration)
				server.storeWorkspaceReferenceResultLocked(key, []lsp.Location{{URI: key.URI}})
				server.mu.Unlock()
				server.invalidateWorkspaceReferenceNames(nil, nil, []string{name})
			}
		}(worker)
	}
	wait.Wait()
}

func BenchmarkWorkspaceReferenceNameIndexInvalidationLargeCache(b *testing.B) {
	server := New(nil, io.Discard, io.Discard)
	server.mu.Lock()
	server.ensureWorkspaceReferenceNameIndexLocked()
	for index := 0; index < 100_000; index++ {
		name := fmt.Sprintf("unrelated-%06d", index)
		key := workspaceReferenceTargetKey{URI: "file:///workspace/cache.asp", Name: name, NameHash: uint64(index), Generation: 1}
		server.storeWorkspaceReferenceCountLocked(key, index)
	}
	server.mu.Unlock()
	affected := map[string]struct{}{"shared": {}}
	key := workspaceReferenceTargetKey{URI: "file:///workspace/affected.asp", Name: "shared", NameHash: workspaceReferenceNameHash("shared"), Generation: 1}
	b.ReportAllocs()
	b.ResetTimer()
	for iteration := 0; iteration < b.N; iteration++ {
		server.mu.Lock()
		server.storeWorkspaceReferenceCountLocked(key, iteration)
		server.invalidateWorkspaceReferenceTargetsByNameLocked(affected)
		server.mu.Unlock()
	}
}
