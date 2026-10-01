package lspserver

import (
	"context"
	"strings"

	"github.com/yottonoko/classic-web-system-lsp/internal/core"
	workspacepkg "github.com/yottonoko/classic-web-system-lsp/internal/workspace"
)

type parsedDocumentCacheEntry struct {
	Version         int
	Text            string
	DefaultLanguage string
	Parsed          *core.ParsedDocument
}

type parsedAnalysisInflight struct {
	done         chan struct{}
	parsed       *core.ParsedDocument
	publications []*parsedAnalysisPublication
}

type parsedAnalysisPublication struct {
	cacheKey  string
	revision  uint64
	document  *core.TextDocument
	entry     parsedDocumentCacheEntry
	published bool
}

func (s *Server) parseTextDocument(doc *core.TextDocument, defaultLanguage string) *core.ParsedDocument {
	return s.parseTextDocumentWithSnapshotSchedule(doc, defaultLanguage, true)
}

func (s *Server) parseTextDocumentWithSnapshotSchedule(doc *core.TextDocument, defaultLanguage string, scheduleSnapshot bool) *core.ParsedDocument {
	if doc == nil {
		return nil
	}
	key := parsedDocumentCacheKey(doc.URI)
	entry := parsedDocumentCacheEntry{Version: doc.Version, Text: doc.Text, DefaultLanguage: defaultLanguage}
	flight, publication, cached, leader := s.beginParsedAnalysis(key, doc, entry)
	if cached != nil {
		s.touchDocumentStore(doc.URI)
		return cached
	}
	if !leader {
		<-flight.done
		if publication.published && scheduleSnapshot {
			s.scheduleFileAnalysisSnapshot(doc, flight.parsed, defaultLanguage)
		}
		s.touchDocumentStore(doc.URI)
		return flight.parsed
	}

	parsed, snapshot, restored := s.readDiskParsedDocument(doc, defaultLanguage)
	if !restored {
		s.mu.Lock()
		testHook := s.documentParseTestHook
		s.mu.Unlock()
		if testHook != nil {
			testHook(doc.URI)
		}
		parsed = core.ParseDocument(doc.URI, doc.Text, core.Settings{DefaultLanguage: defaultLanguage})
	}
	s.finishParsedAnalysis(doc.URI, doc.Text, defaultLanguage, flight, parsed)
	if publication.published && snapshot == nil && scheduleSnapshot {
		s.scheduleFileAnalysisSnapshot(doc, parsed, defaultLanguage)
	}
	s.scheduleMemoryPressureCheck("parsedDocument.remember")
	return parsed
}

func (s *Server) parseText(uri string, text string, defaultLanguage string) *core.ParsedDocument {
	return s.parseTextContext(context.Background(), uri, text, defaultLanguage)
}

// parseTextContext parses text like parseText. ctx only carries pass-scoped
// state, such as an include resolution memo, for validating a disk-cache hit;
// it does not cancel the parse.
func (s *Server) parseTextContext(ctx context.Context, uri string, text string, defaultLanguage string) *core.ParsedDocument {
	if cached := s.cachedParsedText(uri, text, defaultLanguage); cached != nil {
		s.touchDocumentStore(uri)
		return cached
	}
	key := parsedTextCacheKey(uri)
	doc := core.NewTextDocument(uri, "classic-asp", 0, text)
	entry := parsedDocumentCacheEntry{Text: text, DefaultLanguage: defaultLanguage}
	flight, publication, cached, leader := s.beginParsedAnalysis(key, doc, entry)
	if cached != nil {
		s.touchDocumentStore(uri)
		return cached
	}
	if !leader {
		<-flight.done
		if publication.published {
			s.scheduleFileAnalysisSnapshot(doc, flight.parsed, defaultLanguage)
		}
		s.touchDocumentStore(uri)
		return flight.parsed
	}

	parsed, snapshot, restored := s.readDiskParsedDocumentContext(ctx, doc, defaultLanguage)
	if !restored {
		s.mu.Lock()
		testHook := s.documentParseTestHook
		s.mu.Unlock()
		if testHook != nil {
			testHook(uri)
		}
		parsed = core.ParseDocument(uri, text, core.Settings{DefaultLanguage: defaultLanguage})
	}
	s.finishParsedAnalysis(uri, text, defaultLanguage, flight, parsed)
	if publication.published && snapshot == nil {
		s.scheduleFileAnalysisSnapshot(doc, parsed, defaultLanguage)
	}
	s.scheduleMemoryPressureCheck("parsedText.remember")
	return parsed
}

func (s *Server) cachedParsedText(uri, text, defaultLanguage string) *core.ParsedDocument {
	s.mu.Lock()
	defer s.mu.Unlock()
	entry := parsedDocumentCacheEntry{Text: text, DefaultLanguage: defaultLanguage}
	cached, ok := s.parsedCacheHitLocked(parsedTextCacheKey(uri), entry)
	if !ok {
		return nil
	}
	return cached
}

func (s *Server) beginParsedAnalysis(cacheKey string, doc *core.TextDocument, entry parsedDocumentCacheEntry) (*parsedAnalysisInflight, *parsedAnalysisPublication, *core.ParsedDocument, bool) {
	s.mu.Lock()
	if cached, ok := s.parsedCacheHitLocked(cacheKey, entry); ok {
		s.mu.Unlock()
		return nil, nil, cached, false
	}
	s.mu.Unlock()

	flightKey := parsedAnalysisFlightKey(doc.URI, doc.Text, entry.DefaultLanguage)
	s.mu.Lock()
	if cached, ok := s.parsedCacheHitLocked(cacheKey, entry); ok {
		s.mu.Unlock()
		return nil, nil, cached, false
	}
	if flight := s.parsedInflight[flightKey]; flight != nil {
		currentRevision := s.parsedCacheRevisions[cacheKey]
		for _, current := range flight.publications {
			if current != nil && current.cacheKey == cacheKey && current.revision == currentRevision && current.entry.Version > entry.Version {
				s.mu.Unlock()
				return flight, &parsedAnalysisPublication{}, nil, false
			}
		}
		revision := s.advanceParsedCacheRevisionLocked(cacheKey)
		publication := &parsedAnalysisPublication{cacheKey: cacheKey, revision: revision, document: doc.Clone(), entry: entry}
		flight.publications = append(flight.publications, publication)
		s.mu.Unlock()
		return flight, publication, nil, false
	}
	revision := s.advanceParsedCacheRevisionLocked(cacheKey)
	publication := &parsedAnalysisPublication{cacheKey: cacheKey, revision: revision, document: doc.Clone(), entry: entry}
	flight := &parsedAnalysisInflight{done: make(chan struct{}), publications: []*parsedAnalysisPublication{publication}}
	s.parsedInflight[flightKey] = flight
	s.mu.Unlock()
	return flight, publication, nil, true
}

func (s *Server) parsedCacheHitLocked(cacheKey string, entry parsedDocumentCacheEntry) (*core.ParsedDocument, bool) {
	cached, ok := s.parsedCache[cacheKey]
	if ok && parsedCacheEntryMatches(cached, entry) {
		if cached.Version < entry.Version {
			cached.Version = entry.Version
			s.parsedCache[cacheKey] = cached
		}
		s.aliasParsedCacheEntryLocked(parsedCacheSiblingKey(cacheKey), cached)
		return cached.Parsed, true
	}

	// The document and text caches are two views of the same immutable parse.
	// Look in the sibling namespace while holding the cache lock, then publish
	// the exact same entry into both views. This also advances a stale target
	// revision so an older in-flight publication cannot overwrite the alias.
	siblingKey := parsedCacheSiblingKey(cacheKey)
	if siblingKey == "" {
		return nil, false
	}
	cached, ok = s.parsedCache[siblingKey]
	if !ok || !parsedCacheEntryMatches(cached, entry) {
		return nil, false
	}
	if current, exists := s.parsedCache[cacheKey]; exists && !parsedCacheEntryMatches(current, entry) {
		s.advanceParsedCacheRevisionLocked(cacheKey)
	}
	if cached.Version < entry.Version {
		cached.Version = entry.Version
		s.parsedCache[siblingKey] = cached
	}
	s.parsedCache[cacheKey] = cached
	return cached.Parsed, true
}

func parsedCacheEntryMatches(cached, entry parsedDocumentCacheEntry) bool {
	return cached.Parsed != nil && cached.Text == entry.Text && cached.DefaultLanguage == entry.DefaultLanguage
}

func parsedCacheSiblingKey(cacheKey string) string {
	switch {
	case strings.HasPrefix(cacheKey, "doc:"):
		return "text:" + strings.TrimPrefix(cacheKey, "doc:")
	case strings.HasPrefix(cacheKey, "text:"):
		return "doc:" + strings.TrimPrefix(cacheKey, "text:")
	default:
		return ""
	}
}

func (s *Server) aliasParsedCacheEntryLocked(cacheKey string, entry parsedDocumentCacheEntry) {
	if cacheKey == "" || entry.Parsed == nil {
		return
	}
	if current, ok := s.parsedCache[cacheKey]; ok && current.Parsed != entry.Parsed {
		s.advanceParsedCacheRevisionLocked(cacheKey)
	}
	s.parsedCache[cacheKey] = entry
}

func (s *Server) advanceParsedCacheRevisionLocked(key string) uint64 {
	s.parsedCacheRevisions[key]++
	return s.parsedCacheRevisions[key]
}

func (s *Server) finishParsedAnalysis(uri, text, defaultLanguage string, flight *parsedAnalysisInflight, parsed *core.ParsedDocument) {
	flightKey := parsedAnalysisFlightKey(uri, text, defaultLanguage)
	s.mu.Lock()
	flight.parsed = parsed
	for _, publication := range flight.publications {
		if publication == nil || publication.document == nil || s.parsedCacheRevisions[publication.cacheKey] != publication.revision {
			continue
		}
		publication.entry.Parsed = parsed
		s.parsedCache[publication.cacheKey] = publication.entry
		s.rememberParsedDocumentLocked(publication.document, parsed, publication.entry.DefaultLanguage)
		publication.published = true
	}
	if s.parsedInflight[flightKey] == flight {
		delete(s.parsedInflight, flightKey)
	}
	close(flight.done)
	s.mu.Unlock()
}

func parsedAnalysisFlightKey(uri, text, defaultLanguage string) string {
	return workspacepkg.FileIdentityKeyFromURI(uri) + "\x00" + workspacepkg.DiskContentHash(text) + "\x00" + defaultLanguage
}

func (s *Server) deleteParsedCacheForURILocked(uri string) {
	for key := range s.parsedCache {
		if keyMatchesParsedCacheURI(key, uri) {
			delete(s.parsedCache, key)
		}
	}
	s.advanceParsedCacheRevisionLocked(parsedDocumentCacheKey(uri))
	s.advanceParsedCacheRevisionLocked(parsedTextCacheKey(uri))
	s.deleteAnalysisCacheForURI(uri)
}

func parsedDocumentCacheKey(uri string) string {
	return "doc:" + workspacepkg.FileIdentityKeyFromURI(uri)
}

func parsedTextCacheKey(uri string) string {
	return "text:" + workspacepkg.FileIdentityKeyFromURI(uri)
}

func keyMatchesParsedCacheURI(key string, uri string) bool {
	identity := workspacepkg.FileIdentityKeyFromURI(uri)
	return key == "doc:"+identity || key == "text:"+identity
}
