package workspace

import (
	"reflect"
	"testing"
)

func TestMemoryBudgetManagerEvictsByPriority(t *testing.T) {
	evicted := []string{}
	fast := newFakeCache("fast", 10, 80, &evicted)
	later := newFakeCache("later", 20, 80, &evicted)
	manager := NewMemoryBudgetManager(MemoryBudgetManagerOptions{
		HeapStatsProvider:    func() HeapStats { return HeapStats{HeapUsed: 10, HeapSizeLimit: 100} },
		DefaultMaxCacheBytes: 100,
	})
	manager.Register(later)
	manager.Register(fast)

	result := manager.CheckPressure("test", 100)
	if result.Pressure != "budget" || result.RequestedBytes != 60 || result.EvictedBytes != 80 {
		t.Fatalf("unexpected pressure result: %#v", result)
	}
	if !reflect.DeepEqual(evicted, []string{"fast"}) {
		t.Fatalf("evicted = %#v", evicted)
	}
	if result.After.TotalEstimatedBytes != 80 {
		t.Fatalf("after bytes = %d", result.After.TotalEstimatedBytes)
	}
}

func TestMemoryBudgetManagerHeapPressure(t *testing.T) {
	evicted := []string{}
	manager := NewMemoryBudgetManager(MemoryBudgetManagerOptions{
		HeapStatsProvider:    func() HeapStats { return HeapStats{HeapUsed: 80, HeapSizeLimit: 100} },
		DefaultMaxCacheBytes: 200,
	})
	manager.Register(newFakeCache("cache", 10, 180, &evicted))

	result := manager.CheckPressure("heap", 0)
	if result.Pressure != "heap-high" || result.TargetBytes != 100 || result.RequestedBytes != 80 || result.EvictedBytes != 180 {
		t.Fatalf("unexpected heap pressure result: %#v", result)
	}
}

func TestMemoryBudgetManagerTracksAllocationNotes(t *testing.T) {
	manager := NewMemoryBudgetManager(MemoryBudgetManagerOptions{
		HeapStatsProvider: func() HeapStats { return HeapStats{HeapUsed: 1, HeapSizeLimit: 100} },
	})
	manager.Register(newFakeCache("cache", 10, 10, &[]string{}))
	manager.NoteAllocation("cache", 15)
	manager.NoteAllocation("cache", -5)

	snapshot := manager.Snapshot(0)
	if got := snapshot.Caches[0].EstimatedBytes; got != 20 {
		t.Fatalf("estimated bytes = %d", got)
	}
}

func TestRegisteredMapCacheEvictsRegularMaps(t *testing.T) {
	values := map[string]string{
		"a": "aaa",
		"b": "bbb",
	}
	cache := NewRegisteredMapCache("map", values, 1, 0, func(key string, value string) int64 {
		return int64(len(key) + len(value))
	}, nil)

	if got := cache.EstimateBytes(); got != 8 {
		t.Fatalf("estimate = %d, want 8", got)
	}
	if freed := cache.Evict(4); freed != 4 {
		t.Fatalf("freed = %d, want 4", freed)
	}
	if got := len(values); got != 1 {
		t.Fatalf("map length after eviction = %d, want 1", got)
	}
}

func TestSizedLruCacheTracksEntrySizesAndOrder(t *testing.T) {
	disposed := []string{}
	cache := NewSizedLruCache[string, string]("sized", 10, 2, func(_ string, value string) int64 {
		return int64(len(value))
	}, func(key string, _ string) {
		disposed = append(disposed, key)
	})

	cache.Set("a", "aaaa")
	cache.Set("b", "bb")
	if cache.EstimateBytes() != 6 {
		t.Fatalf("estimate = %d", cache.EstimateBytes())
	}
	if value, ok := cache.Get("a"); !ok || value != "aaaa" {
		t.Fatalf("get a = %q, %v", value, ok)
	}
	cache.Set("c", "ccc")
	if !reflect.DeepEqual(cache.Keys(), []string{"a", "c"}) {
		t.Fatalf("keys = %#v", cache.Keys())
	}
	if cache.EstimateBytes() != 7 {
		t.Fatalf("estimate after prune = %d", cache.EstimateBytes())
	}
	if !reflect.DeepEqual(disposed, []string{"b"}) {
		t.Fatalf("disposed = %#v", disposed)
	}

	if freed := cache.Evict(4); freed != 4 {
		t.Fatalf("freed = %d", freed)
	}
	if !reflect.DeepEqual(cache.Keys(), []string{"c"}) || !reflect.DeepEqual(disposed, []string{"b", "a"}) {
		t.Fatalf("keys/disposed = %#v/%#v", cache.Keys(), disposed)
	}
}

type fakeCache struct {
	name    string
	pri     int
	bytes   int64
	evicted *[]string
}

func newFakeCache(name string, priority int, bytes int64, evicted *[]string) *fakeCache {
	return &fakeCache{name: name, pri: priority, bytes: bytes, evicted: evicted}
}

func (c *fakeCache) Name() string         { return c.name }
func (c *fakeCache) Priority() int        { return c.pri }
func (c *fakeCache) EstimateBytes() int64 { return c.bytes }
func (c *fakeCache) EntryCount() int {
	if c.bytes > 0 {
		return 1
	}
	return 0
}
func (c *fakeCache) Evict(_ int64) int64 {
	freed := c.bytes
	c.bytes = 0
	*c.evicted = append(*c.evicted, c.name)
	return freed
}
