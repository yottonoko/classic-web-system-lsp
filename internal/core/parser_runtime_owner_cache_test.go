package core

import (
	"sync"
	"sync/atomic"
	"testing"
)

func TestParsedDocumentRuntimeAnalysisMemoryOwnersReusesStableCache(t *testing.T) {
	parsed := ParseDocument("file:///runtime-owner-cache.asp", "<% Dim value %>", Settings{DefaultLanguage: "VBScript"})
	parsed.StoreRuntimeAnalysis("test.runtime-owner-cache", []byte("runtime"))

	first := parsed.RuntimeAnalysisMemoryOwners()
	if len(first) != 1 {
		t.Fatalf("first runtime owners = %#v, want one owner", first)
	}
	second := parsed.RuntimeAnalysisMemoryOwners()
	if len(second) != 1 || &first[0] != &second[0] {
		t.Fatalf("stable runtime owners were not shared: first=%#v second=%#v", first, second)
	}

	var got []RuntimeAnalysisMemoryOwner
	allocations := testing.AllocsPerRun(100, func() {
		got = parsed.RuntimeAnalysisMemoryOwners()
	})
	if len(got) != 1 {
		t.Fatalf("stable runtime owners after allocation check = %#v, want one owner", got)
	}
	if allocations != 0 {
		t.Fatalf("stable RuntimeAnalysisMemoryOwners allocations = %f, want zero", allocations)
	}
}

func TestParsedDocumentRuntimeAnalysisMemoryOwnersInvalidatesOnStore(t *testing.T) {
	parsed := ParseDocument("file:///runtime-owner-cache-store.asp", "<% Dim value %>", Settings{DefaultLanguage: "VBScript"})
	parsed.StoreRuntimeAnalysis("test.first", []byte("first"))
	first := parsed.RuntimeAnalysisMemoryOwners()

	parsed.StoreRuntimeAnalysis("test.second", []byte("second"))
	second := parsed.RuntimeAnalysisMemoryOwners()
	if len(first) != 1 || len(second) != 2 {
		t.Fatalf("runtime owners after store = first(%#v) second(%#v), want one then two", first, second)
	}
	if &first[0] == &second[0] {
		t.Fatal("runtime owner cache was reused after StoreRuntimeAnalysis")
	}
}

func TestParsedDocumentRuntimeAnalysisMemoryOwnersInvalidatesOnLoadOrStore(t *testing.T) {
	parsed := ParseDocument("file:///runtime-owner-cache-load-or-store.asp", "<% Dim value %>", Settings{DefaultLanguage: "VBScript"})
	first := parsed.RuntimeAnalysisMemoryOwners()
	if len(first) != 0 {
		t.Fatalf("first runtime owners = %#v, want none", first)
	}

	actual, loaded := parsed.LoadOrStoreRuntimeAnalysis("test.runtime", []byte("runtime"))
	if loaded || actual == nil {
		t.Fatalf("LoadOrStore result = (%#v, %t), want a new value", actual, loaded)
	}
	second := parsed.RuntimeAnalysisMemoryOwners()
	if len(second) != 1 {
		t.Fatalf("runtime owners after LoadOrStore = %#v, want one owner", second)
	}
}

func TestParsedDocumentRuntimeAnalysisMemoryOwnersInvalidatesOnRelease(t *testing.T) {
	const key = "test.release"
	previous := ParseDocument("file:///runtime-owner-cache-release.asp", "<% Dim before %>", Settings{DefaultLanguage: "VBScript"})
	previous.StoreRuntimeAnalysis(key, []byte("previous"))
	updated := ParseDocument(previous.URI, "<% Dim after %>", Settings{DefaultLanguage: "VBScript"})
	updated.inheritPreviousRevision(previous)
	current, loaded := updated.LoadOrStoreRuntimeAnalysis(key, []byte("current"))
	if loaded || current == nil {
		t.Fatalf("current runtime value = %#v, loaded=%t; want a new value", current, loaded)
	}

	withPrevious := updated.RuntimeAnalysisMemoryOwners()
	if len(withPrevious) != 2 {
		t.Fatalf("runtime owners before release = %#v, want current and predecessor", withPrevious)
	}
	if !updated.ReleasePreviousRuntimeAnalysis(key, current) {
		t.Fatal("ReleasePreviousRuntimeAnalysis failed")
	}
	withoutPrevious := updated.RuntimeAnalysisMemoryOwners()
	if len(withoutPrevious) != 1 {
		t.Fatalf("runtime owners after release = %#v, want current only", withoutPrevious)
	}
}

func TestParsedDocumentRuntimeAnalysisMemoryOwnersInvalidatesOnInheritance(t *testing.T) {
	previous := ParseDocument("file:///runtime-owner-cache-inherit.asp", "<% Dim before %>", Settings{DefaultLanguage: "VBScript"})
	previous.StoreRuntimeAnalysis("test.previous", []byte("previous"))
	updated := ParseDocument(previous.URI, "<% Dim after %>", Settings{DefaultLanguage: "VBScript"})
	if owners := updated.RuntimeAnalysisMemoryOwners(); len(owners) != 0 {
		t.Fatalf("runtime owners before inheritance = %#v, want none", owners)
	}

	updated.inheritPreviousRevision(previous)
	owners := updated.RuntimeAnalysisMemoryOwners()
	if len(owners) != 1 {
		t.Fatalf("runtime owners after inheritance = %#v, want one predecessor owner", owners)
	}
}

type runtimeOwnerCacheVersionedEstimator struct {
	bytes      atomic.Int64
	generation atomic.Uint64
}

func (estimator *runtimeOwnerCacheVersionedEstimator) EstimateBytes() int64 {
	return estimator.bytes.Load()
}

func (estimator *runtimeOwnerCacheVersionedEstimator) RuntimeAnalysisMemoryOwnerGeneration() uint64 {
	return estimator.generation.Load()
}

func (estimator *runtimeOwnerCacheVersionedEstimator) setBytes(bytes int64) {
	estimator.bytes.Store(bytes)
	estimator.generation.Add(1)
}

func TestParsedDocumentRuntimeAnalysisMemoryOwnersInvalidatesOnProviderGeneration(t *testing.T) {
	parsed := ParseDocument("file:///runtime-owner-cache-provider-generation.asp", "<% Dim value %>", Settings{DefaultLanguage: "VBScript"})
	estimator := &runtimeOwnerCacheVersionedEstimator{}
	estimator.setBytes(64)
	parsed.StoreRuntimeAnalysis("test.versioned-estimator", estimator)

	first := parsed.RuntimeAnalysisMemoryOwners()
	if len(first) != 1 || first[0].Bytes != 88 {
		t.Fatalf("initial versioned owner = %#v, want one 88-byte owner", first)
	}
	estimator.setBytes(256)
	second := parsed.RuntimeAnalysisMemoryOwners()
	if len(second) != 1 || second[0].Bytes != 280 {
		t.Fatalf("updated versioned owner = %#v, want one 280-byte owner", second)
	}
	if &first[0] == &second[0] {
		t.Fatal("runtime owner cache was reused after provider generation changed")
	}

	var got []RuntimeAnalysisMemoryOwner
	allocations := testing.AllocsPerRun(100, func() {
		got = parsed.RuntimeAnalysisMemoryOwners()
	})
	if len(got) != 1 || got[0].Bytes != 280 {
		t.Fatalf("stable versioned owner = %#v, want one 280-byte owner", got)
	}
	if allocations != 0 {
		t.Fatalf("stable versioned RuntimeAnalysisMemoryOwners allocations = %f, want zero", allocations)
	}
}

type runtimeOwnerCacheMutatingProvider struct {
	document *ParsedDocument
	once     *sync.Once
	shared   *int
}

func (provider runtimeOwnerCacheMutatingProvider) RuntimeAnalysisMemoryOwnerSet() []RuntimeAnalysisMemoryOwner {
	provider.once.Do(func() {
		provider.document.StoreRuntimeAnalysis("test.mutated-during-owner-walk", []byte("new"))
	})
	return []RuntimeAnalysisMemoryOwner{{Identity: provider.shared, Bytes: 64}}
}

func TestParsedDocumentRuntimeAnalysisMemoryOwnersDoesNotPublishStaleWalk(t *testing.T) {
	parsed := ParseDocument("file:///runtime-owner-cache-publish.asp", "<% Dim value %>", Settings{DefaultLanguage: "VBScript"})
	parsed.StoreRuntimeAnalysis("test.provider", runtimeOwnerCacheMutatingProvider{
		document: parsed,
		once:     new(sync.Once),
		shared:   new(int),
	})

	first := parsed.RuntimeAnalysisMemoryOwners()
	if len(first) != 2 {
		t.Fatalf("first runtime owners = %#v, want provider and newly stored owner", first)
	}
	second := parsed.RuntimeAnalysisMemoryOwners()
	if len(second) != 2 {
		t.Fatalf("runtime owners after mutation = %#v, want provider and newly stored owner", second)
	}
	if &first[0] != &second[0] {
		t.Fatal("stable runtime owners were not cached after the mutation retry")
	}
}

func TestParsedDocumentRuntimeAnalysisMemoryOwnersConcurrentMutation(t *testing.T) {
	parsed := ParseDocument("file:///runtime-owner-cache-concurrent.asp", "<% Dim value %>", Settings{DefaultLanguage: "VBScript"})
	parsed.StoreRuntimeAnalysis("test.seed", []byte("seed"))

	const (
		readers    = 8
		iterations = 200
	)
	start := make(chan struct{})
	var complete sync.WaitGroup
	complete.Add(readers + 2)
	for range readers {
		go func() {
			defer complete.Done()
			<-start
			for range iterations {
				owners := parsed.RuntimeAnalysisMemoryOwners()
				for _, owner := range owners {
					if owner.Identity == nil || owner.Bytes <= 0 {
						t.Errorf("invalid runtime owner = %#v", owner)
					}
				}
			}
		}()
	}
	for writer := range 2 {
		go func(writer int) {
			defer complete.Done()
			<-start
			for range iterations {
				parsed.StoreRuntimeAnalysis("test.writer-"+string(rune('a'+writer)), []byte("runtime"))
			}
		}(writer)
	}
	close(start)
	complete.Wait()
}
