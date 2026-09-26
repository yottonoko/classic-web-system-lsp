package lspserver

import (
	"encoding/binary"
	"strings"
	"sync"
)

const workspaceReferenceAggregateCacheMaxBytes = 1 << 20

// Only the latest aggregate is retained. Keys contain stable document IDs and
// count revisions, never parsed-document pointers or source text. Cached
// results are immutable and may be shared by concurrent readers.
type workspaceReferenceAggregateCache struct {
	mu     sync.Mutex
	key    string
	result workspaceReferenceAggregateResult
	bytes  int64
}

func workspaceReferenceAggregateKey(revision uint64, origin string, ids []uint64, uris []string, plans []workspaceReferenceBatchPlan, progress bool) string {
	size := 8 + 8 + len(origin) + 8 + len(ids)*8 + 1
	for _, uri := range uris {
		size += 8 + len(uri)
	}
	for _, plan := range plans {
		size += 9 + len(plan.target.name)
	}
	if size > workspaceReferenceAggregateCacheMaxBytes {
		return ""
	}
	var buffer strings.Builder
	buffer.Grow(size)
	appendUint := func(value uint64) {
		var encoded [8]byte
		binary.LittleEndian.PutUint64(encoded[:], value)
		buffer.Write(encoded[:])
	}
	appendString := func(value string) { appendUint(uint64(len(value))); buffer.WriteString(value) }
	appendUint(revision)
	appendString(origin)
	appendUint(uint64(len(ids)))
	for _, id := range ids {
		appendUint(id)
	}
	if progress {
		buffer.WriteByte(1)
	} else {
		buffer.WriteByte(0)
	}
	for _, uri := range uris {
		appendString(uri)
	}
	for _, plan := range plans {
		appendString(plan.target.name)
		var flags byte
		if plan.target.callOnly {
			flags |= 1
		}
		if plan.target.unqualified {
			flags |= 2
		}
		buffer.WriteByte(flags)
	}
	return buffer.String()
}

func (cache *workspaceReferenceAggregateCache) get(key string) (workspaceReferenceAggregateResult, bool) {
	cache.mu.Lock()
	defer cache.mu.Unlock()
	return cache.result, cache.key != "" && cache.key == key
}

func (cache *workspaceReferenceAggregateCache) store(key string, result workspaceReferenceAggregateResult) {
	size := int64(len(key) + (len(result.counts)+len(result.shadowed))*8 + len(result.progress)*48)
	for _, progress := range result.progress {
		size += int64(len(progress.counts)*8 + len(progress.uri))
	}
	cache.mu.Lock()
	defer cache.mu.Unlock()
	// Oversized queries must not displace a useful bounded entry.
	if key == "" || size > workspaceReferenceAggregateCacheMaxBytes || result.cancelled {
		return
	}
	cache.key, cache.result, cache.bytes = key, result, size
}

func (cache *workspaceReferenceAggregateCache) clear() {
	cache.mu.Lock()
	defer cache.mu.Unlock()
	cache.key, cache.result, cache.bytes = "", workspaceReferenceAggregateResult{}, 0
}

func (cache *workspaceReferenceAggregateCache) estimateBytes() int64 {
	cache.mu.Lock()
	defer cache.mu.Unlock()
	return cache.bytes
}
