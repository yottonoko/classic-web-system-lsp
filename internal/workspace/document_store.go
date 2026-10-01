package workspace

import (
	"sort"
	"strings"
	"time"
)

type CachedDocument struct {
	URI                  string
	Text                 string
	Version              int
	Parsed               any
	ParseDepth           string
	Virtuals             map[string]any
	VirtualsMaterialized bool
	Analysis             any
	CSSContext           any
	Generation           int
	LastAccess           int64
	DemotedAt            int64
}

// DocumentStore caches documents by URI. Mutate Cache only through Put and
// Delete so file-identity lookups stay indexed. Direct map writes that change
// the entry count or replace an indexed candidate rebuild the index, but a
// direct replacement under a new file identity is not detected.
type DocumentStore struct {
	Cache map[string]*CachedDocument

	identity documentStoreIdentityIndex
}

type documentStoreIdentityIndex struct {
	entries    map[string]documentStoreIdentityEntry
	byIdentity map[string]map[string]struct{}
}

type documentStoreIdentityEntry struct {
	cached   *CachedDocument
	uri      string
	identity string
}

func NewDocumentStore() *DocumentStore {
	return &DocumentStore{Cache: map[string]*CachedDocument{}}
}

// Put stores cached under key and indexes its file identity.
func (s *DocumentStore) Put(key string, cached *CachedDocument) {
	s.Cache[key] = cached
	if s.identity.entries != nil {
		s.identity.remove(key)
		s.identity.add(key, cached)
	}
}

// Delete removes the entry stored under key.
func (s *DocumentStore) Delete(key string) {
	delete(s.Cache, key)
	if s.identity.entries != nil {
		s.identity.remove(key)
	}
}

func (s *DocumentStore) CachedDocumentForURI(uri string) *CachedDocument {
	if cached := s.Cache[uri]; cached != nil {
		s.Touch(cached, 0)
		return cached
	}
	if stringsHasFileScheme(uri) {
		matches := s.identityMatches(uri)
		if len(matches) > 0 {
			cached := s.Cache[matches[0]]
			s.Touch(cached, 0)
			return cached
		}
	}
	return nil
}

func (s *DocumentStore) CachedDocumentsForURI(uri string) []*CachedDocument {
	direct := s.Cache[uri]
	if !stringsHasFileScheme(uri) {
		if direct == nil {
			return nil
		}
		return []*CachedDocument{direct}
	}
	result := []*CachedDocument{}
	for _, key := range s.identityMatches(uri) {
		result = append(result, s.Cache[key])
	}
	if direct != nil && !cachedDocumentSliceContains(result, direct) {
		result = append([]*CachedDocument{direct}, result...)
	}
	for _, cached := range result {
		s.Touch(cached, 0)
	}
	return result
}

func (s *DocumentStore) DeleteCachedDocumentsForURI(uri string) {
	if stringsHasFileScheme(uri) {
		for _, key := range s.identityMatches(uri) {
			s.Delete(key)
		}
	}
	if _, ok := s.Cache[uri]; ok {
		s.Delete(uri)
	}
}

// identityMatches returns the sorted keys whose cached URI shares the file
// identity of uri.
func (s *DocumentStore) identityMatches(uri string) []string {
	identity := FileIdentityKeyFromURI(uri)
	for attempt := 0; ; attempt++ {
		if s.identity.entries == nil || len(s.identity.entries) != len(s.Cache) {
			s.rebuildIdentityIndex()
		}
		keys := make([]string, 0, len(s.identity.byIdentity[identity]))
		stale := false
		for key := range s.identity.byIdentity[identity] {
			entry := s.identity.entries[key]
			if s.Cache[key] != entry.cached || entry.cached == nil || entry.cached.URI != entry.uri {
				stale = true
				break
			}
			keys = append(keys, key)
		}
		if !stale || attempt > 0 {
			sort.Strings(keys)
			return keys
		}
		s.identity.entries = nil
	}
}

func (s *DocumentStore) rebuildIdentityIndex() {
	s.identity = documentStoreIdentityIndex{
		entries:    make(map[string]documentStoreIdentityEntry, len(s.Cache)),
		byIdentity: make(map[string]map[string]struct{}, len(s.Cache)),
	}
	for key, cached := range s.Cache {
		s.identity.add(key, cached)
	}
}

func (index *documentStoreIdentityIndex) add(key string, cached *CachedDocument) {
	entry := documentStoreIdentityEntry{cached: cached}
	if cached != nil {
		entry.uri = cached.URI
		entry.identity = FileIdentityKeyFromURI(cached.URI)
		keys := index.byIdentity[entry.identity]
		if keys == nil {
			keys = map[string]struct{}{}
			index.byIdentity[entry.identity] = keys
		}
		keys[key] = struct{}{}
	}
	index.entries[key] = entry
}

func (index *documentStoreIdentityIndex) remove(key string) {
	entry, ok := index.entries[key]
	if !ok {
		return
	}
	delete(index.entries, key)
	if entry.cached == nil {
		return
	}
	keys := index.byIdentity[entry.identity]
	delete(keys, key)
	if len(keys) == 0 {
		delete(index.byIdentity, entry.identity)
	}
}

func (s *DocumentStore) Touch(cached *CachedDocument, now int64) {
	if now == 0 {
		now = time.Now().UnixMilli()
	}
	cached.LastAccess = now
}

func (s *DocumentStore) Demote(cached *CachedDocument, now int64, parseSkeleton func(uri, text string) any) bool {
	hadEvictable := cached.Analysis != nil || cached.CSSContext != nil || len(cached.Virtuals) > 0 || cached.VirtualsMaterialized || cached.ParseDepth == "full"
	if !hadEvictable {
		return false
	}
	cached.Analysis = nil
	cached.CSSContext = nil
	cached.Virtuals = map[string]any{}
	cached.VirtualsMaterialized = false
	if cached.ParseDepth == "full" {
		cached.Parsed = parseSkeleton(cached.URI, cached.Text)
		cached.ParseDepth = "skeleton"
	}
	if now == 0 {
		now = time.Now().UnixMilli()
	}
	cached.DemotedAt = now
	cached.Generation++
	return true
}

func stringsHasFileScheme(uri string) bool {
	return len(uri) >= len("file://") && strings.EqualFold(uri[:len("file://")], "file://")
}

func cachedDocumentSliceContains(values []*CachedDocument, target *CachedDocument) bool {
	for _, value := range values {
		if value == target {
			return true
		}
	}
	return false
}
