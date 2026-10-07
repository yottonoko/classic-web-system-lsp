package workspace

import (
	"crypto/sha256"
	"encoding/hex"
	"sync"
	"unsafe"
)

const (
	contentHashCacheEntries  = 8
	minCachedContentHashText = 4 << 10
	maxCachedContentHashText = 1 << 20
)

// contentHashCache remembers the hashes of the few most recent large texts. A
// document revision is fingerprinted by several independent stages, and each
// would otherwise rehash the same immutable string. Entries hold the string
// itself, so a matching data pointer and length always denote the same bytes.
type contentHashCache struct {
	mu      sync.Mutex
	next    int
	entries [contentHashCacheEntries]contentHashCacheEntry
}

type contentHashCacheEntry struct {
	text string
	hash string
}

var contentHashes contentHashCache

func (c *contentHashCache) hash(text string) string {
	if len(text) < minCachedContentHashText || len(text) > maxCachedContentHashText {
		return sha256Hex(text)
	}
	data := unsafe.StringData(text)
	c.mu.Lock()
	for _, entry := range c.entries {
		if len(entry.text) == len(text) && unsafe.StringData(entry.text) == data {
			c.mu.Unlock()
			return entry.hash
		}
	}
	c.mu.Unlock()
	hash := sha256Hex(text)
	c.mu.Lock()
	c.entries[c.next] = contentHashCacheEntry{text: text, hash: hash}
	c.next = (c.next + 1) % contentHashCacheEntries
	c.mu.Unlock()
	return hash
}

func sha256Hex(text string) string {
	// Sum256 only reads its input, so hash the string bytes without copying.
	sum := sha256.Sum256(unsafe.Slice(unsafe.StringData(text), len(text)))
	return hex.EncodeToString(sum[:])
}
