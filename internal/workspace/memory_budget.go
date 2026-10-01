package workspace

import (
	"encoding/json"
	"math"
	"sort"
)

const DefaultMemoryMaxCacheBytes = 512 * 1024 * 1024

type RegisteredCache interface {
	Name() string
	Priority() int
	EstimateBytes() int64
	Evict(targetBytes int64) int64
	EntryCount() int
}

// MemoryEstimator is an optional RegisteredCache extension that reports bytes
// and entries from one pass, for caches whose estimate walks every entry.
type MemoryEstimator interface {
	MemoryEstimate() (bytes int64, entries int)
}

type MemoryCacheSnapshot struct {
	Name           string `json:"name"`
	Priority       int    `json:"priority"`
	EstimatedBytes int64  `json:"estimatedBytes"`
	Entries        int    `json:"entries,omitempty"`
}

type MemorySnapshot struct {
	TotalEstimatedBytes int64                 `json:"totalEstimatedBytes"`
	HeapUsed            int64                 `json:"heapUsed"`
	HeapSizeLimit       int64                 `json:"heapSizeLimit"`
	HeapUsedRatio       float64               `json:"heapUsedRatio"`
	MaxCacheBytes       int64                 `json:"maxCacheBytes"`
	Caches              []MemoryCacheSnapshot `json:"caches"`
}

type MemoryEvictionRecord struct {
	Name           string `json:"name"`
	Priority       int    `json:"priority"`
	RequestedBytes int64  `json:"requestedBytes"`
	EvictedBytes   int64  `json:"evictedBytes"`
	BeforeBytes    int64  `json:"beforeBytes"`
	AfterBytes     int64  `json:"afterBytes"`
	BeforeEntries  int    `json:"beforeEntries,omitempty"`
	AfterEntries   int    `json:"afterEntries,omitempty"`
}

type MemoryPressureResult struct {
	Reason         string                 `json:"reason"`
	Pressure       string                 `json:"pressure"`
	TargetBytes    int64                  `json:"targetBytes"`
	RequestedBytes int64                  `json:"requestedBytes"`
	EvictedBytes   int64                  `json:"evictedBytes"`
	Before         MemorySnapshot         `json:"before"`
	After          MemorySnapshot         `json:"after"`
	Evictions      []MemoryEvictionRecord `json:"evictions"`
}

type HeapStats struct {
	HeapUsed      int64
	HeapSizeLimit int64
}

type MemoryBudgetManager struct {
	caches               map[string]RegisteredCache
	adjustments          map[string]int64
	heapStats            func() HeapStats
	heapHighRatio        float64
	heapCriticalRatio    float64
	heapHighTargetRatio  float64
	defaultMaxCacheBytes int64
	lastEviction         *MemoryPressureResult
}

type MemoryBudgetManagerOptions struct {
	HeapStatsProvider    func() HeapStats
	HeapHighRatio        float64
	HeapCriticalRatio    float64
	HeapHighTargetRatio  float64
	DefaultMaxCacheBytes int64
}

func NewMemoryBudgetManager(options MemoryBudgetManagerOptions) *MemoryBudgetManager {
	heapStats := options.HeapStatsProvider
	if heapStats == nil {
		heapStats = func() HeapStats { return HeapStats{} }
	}
	m := &MemoryBudgetManager{
		caches:               map[string]RegisteredCache{},
		adjustments:          map[string]int64{},
		heapStats:            heapStats,
		heapHighRatio:        positiveFloat(options.HeapHighRatio, 0.75),
		heapCriticalRatio:    positiveFloat(options.HeapCriticalRatio, 0.9),
		heapHighTargetRatio:  positiveFloat(options.HeapHighTargetRatio, 0.5),
		defaultMaxCacheBytes: positiveInt64(options.DefaultMaxCacheBytes, DefaultMemoryMaxCacheBytes),
	}
	return m
}

func (m *MemoryBudgetManager) Register(cache RegisteredCache) {
	m.caches[cache.Name()] = cache
}

func (m *MemoryBudgetManager) Unregister(name string) {
	delete(m.caches, name)
	delete(m.adjustments, name)
}

func (m *MemoryBudgetManager) NoteAllocation(name string, deltaBytes int64) {
	if deltaBytes == 0 {
		return
	}
	next := m.adjustments[name] + deltaBytes
	if next == 0 {
		delete(m.adjustments, name)
	} else {
		m.adjustments[name] = next
	}
}

func (m *MemoryBudgetManager) Snapshot(maxCacheBytes int64) MemorySnapshot {
	maxCacheBytes = positiveInt64(maxCacheBytes, m.defaultMaxCacheBytes)
	heap := m.heapStats()
	caches := make([]MemoryCacheSnapshot, 0, len(m.caches))
	var total int64
	for _, cache := range m.sortedCaches() {
		estimated, entries := m.cacheEstimate(cache)
		total += estimated
		caches = append(caches, MemoryCacheSnapshot{
			Name:           cache.Name(),
			Priority:       cache.Priority(),
			EstimatedBytes: estimated,
			Entries:        entries,
		})
	}
	heapLimit := maxInt64(0, heap.HeapSizeLimit)
	heapUsed := maxInt64(0, heap.HeapUsed)
	ratio := 0.0
	if heapLimit > 0 {
		ratio = float64(heapUsed) / float64(heapLimit)
	}
	return MemorySnapshot{TotalEstimatedBytes: total, HeapUsed: heapUsed, HeapSizeLimit: heapLimit, HeapUsedRatio: ratio, MaxCacheBytes: maxCacheBytes, Caches: caches}
}

func (m *MemoryBudgetManager) CheckPressure(reason string, maxCacheBytes int64) MemoryPressureResult {
	if reason == "" {
		reason = "manual"
	}
	before := m.Snapshot(maxCacheBytes)
	pressure, targetBytes := m.pressureTarget(before)
	requested := maxInt64(0, before.TotalEstimatedBytes-targetBytes)
	remaining := requested
	var evicted int64
	var evictions []MemoryEvictionRecord
	if remaining <= 0 {
		return MemoryPressureResult{Reason: reason, Pressure: pressure, TargetBytes: targetBytes, Before: before, After: before}
	}
	beforeCaches := make(map[string]MemoryCacheSnapshot, len(before.Caches))
	for _, cache := range before.Caches {
		beforeCaches[cache.Name] = cache
	}
	// Evicting one cache can move owners into another, so the before snapshot
	// is reused only until the first Evict call.
	evictCalled := false
	if remaining > 0 {
		for _, cache := range m.sortedCaches() {
			if remaining <= 0 {
				break
			}
			snapshot, ok := beforeCaches[cache.Name()]
			beforeBytes, beforeEntries := snapshot.EstimatedBytes, snapshot.Entries
			if !ok || evictCalled {
				beforeBytes, beforeEntries = m.cacheEstimate(cache)
			}
			if beforeBytes <= 0 && beforeEntries == 0 {
				continue
			}
			evictCalled = true
			freed := maxInt64(0, cache.Evict(remaining))
			if freed <= 0 {
				continue
			}
			afterBytes, afterEntries := m.cacheEstimate(cache)
			evictions = append(evictions, MemoryEvictionRecord{Name: cache.Name(), Priority: cache.Priority(), RequestedBytes: remaining, EvictedBytes: freed, BeforeBytes: beforeBytes, AfterBytes: afterBytes, BeforeEntries: beforeEntries, AfterEntries: afterEntries})
			remaining = maxInt64(0, remaining-freed)
			evicted += freed
		}
	}
	after := m.Snapshot(maxCacheBytes)
	result := MemoryPressureResult{Reason: reason, Pressure: pressure, TargetBytes: targetBytes, RequestedBytes: requested, EvictedBytes: evicted, Before: before, After: after, Evictions: evictions}
	if evicted > 0 {
		m.lastEviction = &result
	}
	return result
}

// HeapBelowBudget reports that the live heap is smaller than the cache budget
// and below the heap-pressure ratio, so no cache can be over budget. It is a
// cheap guard for scheduled checks; an unknown heap size returns false.
func (m *MemoryBudgetManager) HeapBelowBudget(maxCacheBytes int64) bool {
	heap := m.heapStats()
	if heap.HeapUsed <= 0 {
		return false
	}
	if heap.HeapSizeLimit > 0 && float64(heap.HeapUsed)/float64(heap.HeapSizeLimit) >= m.heapHighRatio {
		return false
	}
	return heap.HeapUsed < positiveInt64(maxCacheBytes, m.defaultMaxCacheBytes)
}

func (m *MemoryBudgetManager) LastEvictionResult() *MemoryPressureResult {
	return m.lastEviction
}

func (m *MemoryBudgetManager) pressureTarget(snapshot MemorySnapshot) (string, int64) {
	if snapshot.HeapUsedRatio >= m.heapCriticalRatio {
		return "heap-critical", 0
	}
	if snapshot.HeapUsedRatio >= m.heapHighRatio {
		return "heap-high", int64(math.Floor(float64(snapshot.MaxCacheBytes) * m.heapHighTargetRatio))
	}
	if snapshot.TotalEstimatedBytes > snapshot.MaxCacheBytes {
		return "budget", snapshot.MaxCacheBytes
	}
	return "none", snapshot.MaxCacheBytes
}

func (m *MemoryBudgetManager) sortedCaches() []RegisteredCache {
	caches := make([]RegisteredCache, 0, len(m.caches))
	for _, cache := range m.caches {
		caches = append(caches, cache)
	}
	sort.Slice(caches, func(i, j int) bool {
		if caches[i].Priority() == caches[j].Priority() {
			return caches[i].Name() < caches[j].Name()
		}
		return caches[i].Priority() < caches[j].Priority()
	})
	return caches
}

func (m *MemoryBudgetManager) cacheEstimate(cache RegisteredCache) (int64, int) {
	if estimator, ok := cache.(MemoryEstimator); ok {
		bytes, entries := estimator.MemoryEstimate()
		return maxInt64(0, bytes+m.adjustments[cache.Name()]), entries
	}
	return maxInt64(0, cache.EstimateBytes()+m.adjustments[cache.Name()]), cache.EntryCount()
}

type SizedLruCache[K comparable, V any] struct {
	name       string
	priority   int
	maxEntries int
	estimate   func(K, V) int64
	dispose    func(K, V)
	items      map[K]sizedLruEntry[V]
	order      []K
	bytes      int64
}

type sizedLruEntry[V any] struct {
	value V
	bytes int64
}

func NewSizedLruCache[K comparable, V any](name string, priority int, maxEntries int, estimate func(K, V) int64, dispose func(K, V)) *SizedLruCache[K, V] {
	if estimate == nil {
		estimate = func(key K, value V) int64 { return EstimateJSONBytes(key, 128) + EstimateJSONBytes(value, 512) + 64 }
	}
	return &SizedLruCache[K, V]{name: name, priority: priority, maxEntries: maxEntries, estimate: estimate, dispose: dispose, items: map[K]sizedLruEntry[V]{}}
}

func (c *SizedLruCache[K, V]) Name() string         { return c.name }
func (c *SizedLruCache[K, V]) Priority() int        { return c.priority }
func (c *SizedLruCache[K, V]) EstimateBytes() int64 { return c.bytes }
func (c *SizedLruCache[K, V]) EntryCount() int      { return len(c.items) }
func (c *SizedLruCache[K, V]) Size() int            { return len(c.items) }

func (c *SizedLruCache[K, V]) Get(key K) (V, bool) {
	entry, ok := c.items[key]
	if !ok {
		var zero V
		return zero, false
	}
	c.touch(key)
	return entry.value, true
}

func (c *SizedLruCache[K, V]) Has(key K) bool {
	_, ok := c.items[key]
	return ok
}

func (c *SizedLruCache[K, V]) Set(key K, value V) {
	c.Delete(key)
	bytes := maxInt64(0, c.estimate(key, value))
	c.items[key] = sizedLruEntry[V]{value: value, bytes: bytes}
	c.order = append(c.order, key)
	c.bytes += bytes
	c.PruneToMaxEntries(c.maxEntries)
}

func (c *SizedLruCache[K, V]) Delete(key K) bool {
	entry, ok := c.items[key]
	if !ok {
		return false
	}
	delete(c.items, key)
	c.removeOrder(key)
	c.bytes = maxInt64(0, c.bytes-entry.bytes)
	if c.dispose != nil {
		c.dispose(key, entry.value)
	}
	return true
}

func (c *SizedLruCache[K, V]) Clear() {
	for key, entry := range c.items {
		if c.dispose != nil {
			c.dispose(key, entry.value)
		}
	}
	c.items = map[K]sizedLruEntry[V]{}
	c.order = nil
	c.bytes = 0
}

func (c *SizedLruCache[K, V]) Keys() []K {
	return append([]K(nil), c.order...)
}

func (c *SizedLruCache[K, V]) Evict(targetBytes int64) int64 {
	var freed int64
	for len(c.order) > 0 && freed < targetBytes {
		key := c.order[0]
		entry := c.items[key]
		if c.Delete(key) {
			freed += entry.bytes
		}
	}
	return freed
}

func (c *SizedLruCache[K, V]) PruneToMaxEntries(maxEntries int) int64 {
	if maxEntries <= 0 {
		return 0
	}
	var freed int64
	for len(c.items) > maxEntries {
		key := c.order[0]
		entry := c.items[key]
		if c.Delete(key) {
			freed += entry.bytes
		}
	}
	return freed
}

func (c *SizedLruCache[K, V]) touch(key K) {
	c.removeOrder(key)
	c.order = append(c.order, key)
}

func (c *SizedLruCache[K, V]) removeOrder(key K) {
	for i, value := range c.order {
		if value == key {
			c.order = append(c.order[:i], c.order[i+1:]...)
			return
		}
	}
}

type RegisteredMapCache[K comparable, V any] struct {
	name          string
	priority      int
	values        map[K]V
	bytesPerEntry int64
	estimate      func(K, V) int64
	dispose       func(K, V)
}

func NewRegisteredMapCache[K comparable, V any](name string, values map[K]V, priority int, bytesPerEntry int64, estimate func(K, V) int64, dispose func(K, V)) *RegisteredMapCache[K, V] {
	if bytesPerEntry <= 0 {
		bytesPerEntry = 1024
	}
	return &RegisteredMapCache[K, V]{name: name, priority: priority, values: values, bytesPerEntry: bytesPerEntry, estimate: estimate, dispose: dispose}
}

func (c *RegisteredMapCache[K, V]) Name() string    { return c.name }
func (c *RegisteredMapCache[K, V]) Priority() int   { return c.priority }
func (c *RegisteredMapCache[K, V]) EntryCount() int { return len(c.values) }

func (c *RegisteredMapCache[K, V]) EstimateBytes() int64 {
	if c.estimate == nil {
		return int64(len(c.values)) * c.bytesPerEntry
	}
	var total int64
	for key, value := range c.values {
		total += maxInt64(0, c.estimate(key, value))
	}
	return total
}

func (c *RegisteredMapCache[K, V]) Evict(targetBytes int64) int64 {
	var freed int64
	for key, value := range c.values {
		if freed >= targetBytes {
			break
		}
		bytes := c.bytesPerEntry
		if c.estimate != nil {
			bytes = maxInt64(0, c.estimate(key, value))
		}
		delete(c.values, key)
		if c.dispose != nil {
			c.dispose(key, value)
		}
		freed += bytes
	}
	return freed
}

func EstimateStringBytes(value string) int64 {
	return int64(len(value))*2 + 40
}

func EstimateStringArrayBytes(values []string) int64 {
	var total int64 = 32
	for _, value := range values {
		total += EstimateStringBytes(value)
	}
	return total
}

func EstimateJSONBytes(value any, fallbackBytes int64) int64 {
	data, err := json.Marshal(value)
	if err != nil || len(data) == 0 {
		return fallbackBytes
	}
	return EstimateStringBytes(string(data))
}

func positiveFloat(value, fallback float64) float64 {
	if value > 0 && !math.IsNaN(value) && !math.IsInf(value, 0) {
		return value
	}
	return fallback
}

func positiveInt64(value, fallback int64) int64 {
	if value > 0 {
		return value
	}
	return fallback
}

func maxInt64(left, right int64) int64 {
	if left > right {
		return left
	}
	return right
}
