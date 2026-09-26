package workspace

import (
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

type DocumentStore struct {
	Cache map[string]*CachedDocument
}

func NewDocumentStore() *DocumentStore {
	return &DocumentStore{Cache: map[string]*CachedDocument{}}
}

func (s *DocumentStore) CachedDocumentForURI(uri string) *CachedDocument {
	if cached := s.Cache[uri]; cached != nil {
		s.Touch(cached, 0)
		return cached
	}
	if stringsHasFileScheme(uri) {
		for _, cached := range s.Cache {
			if SameFileIdentityURI(cached.URI, uri) {
				s.Touch(cached, 0)
				return cached
			}
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
	fileKey := FileIdentityKeyFromURI(uri)
	result := []*CachedDocument{}
	for _, cached := range s.Cache {
		if FileIdentityKeyFromURI(cached.URI) == fileKey {
			result = append(result, cached)
		}
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
	for key, cached := range s.Cache {
		if key == uri || stringsHasFileScheme(uri) && SameFileIdentityURI(cached.URI, uri) {
			delete(s.Cache, key)
		}
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
