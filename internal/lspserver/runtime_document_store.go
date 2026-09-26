package lspserver

import (
	"strings"

	"github.com/yottonoko/classic-web-system-lsp/internal/core"
	workspacepkg "github.com/yottonoko/classic-web-system-lsp/internal/workspace"
)

func (s *Server) rememberParsedDocument(doc *core.TextDocument, parsed *core.ParsedDocument, defaultLanguage string) {
	if doc == nil {
		return
	}
	s.mu.Lock()
	s.rememberParsedDocumentLocked(doc, parsed, defaultLanguage)
	s.mu.Unlock()
}

func (s *Server) rememberParsedDocumentLocked(doc *core.TextDocument, parsed *core.ParsedDocument, defaultLanguage string) {
	if doc == nil || s.documentStore == nil {
		return
	}
	current := s.openDocumentByURILocked(doc.URI)
	if current == nil {
		current = s.workspaceDocumentByURILocked(doc.URI)
	}
	if current != nil && current.Text != doc.Text {
		return
	}
	cached := s.documentStore.Cache[doc.URI]
	if cached == nil {
		cached = &workspacepkg.CachedDocument{URI: doc.URI}
		s.documentStore.Cache[doc.URI] = cached
	}
	sameSource := cached.Text == doc.Text
	cached.Text = doc.Text
	if !sameSource || cached.Version < doc.Version {
		cached.Version = doc.Version
	}
	cached.Parsed = parsed
	cached.ParseDepth = "full"
	cached.Generation++
	if parsed != nil && parsed.DefaultLanguage != "" {
		defaultLanguage = string(parsed.DefaultLanguage)
	}
	_ = defaultLanguage
	s.documentStore.Touch(cached, 0)
}

func (s *Server) rememberDocumentTextLocked(doc *core.TextDocument) {
	if doc == nil || s.documentStore == nil {
		return
	}
	cached := s.documentStore.Cache[doc.URI]
	if cached == nil {
		cached = &workspacepkg.CachedDocument{URI: doc.URI}
		s.documentStore.Cache[doc.URI] = cached
	}
	cached.Text = doc.Text
	cached.Version = doc.Version
	cached.Parsed = nil
	cached.ParseDepth = "skeleton"
	s.documentStore.Touch(cached, 0)
}

// rememberDocumentRevisionLocked updates the current document metadata without
// dropping parsed or derived state when the source text is unchanged. A didOpen
// notification can carry a newer protocol version for the same immutable text,
// and that revision must not turn a warm document store entry into a skeleton.
// The caller must hold s.mu.
func (s *Server) rememberDocumentRevisionLocked(doc *core.TextDocument) {
	if doc == nil || s.documentStore == nil {
		return
	}
	cached := s.documentStore.CachedDocumentForURI(doc.URI)
	if cached == nil {
		cached = &workspacepkg.CachedDocument{URI: doc.URI}
		s.documentStore.Cache[doc.URI] = cached
	}
	if cached.Text != doc.Text {
		cached.Text = doc.Text
		cached.Version = doc.Version
		cached.Parsed = nil
		cached.ParseDepth = "skeleton"
		cached.Virtuals = nil
		cached.VirtualsMaterialized = false
		cached.Analysis = nil
		cached.CSSContext = nil
		cached.Generation++
	} else if cached.Version < doc.Version {
		cached.Version = doc.Version
	}
	s.documentStore.Touch(cached, 0)
}

func (s *Server) removeDocumentStoreForURI(uri string) {
	s.mu.Lock()
	if s.documentStore != nil {
		s.documentStore.DeleteCachedDocumentsForURI(uri)
	}
	s.mu.Unlock()
}

func (s *Server) touchDocumentStore(uri string) {
	s.mu.Lock()
	if s.documentStore != nil {
		_ = s.documentStore.CachedDocumentForURI(uri)
	}
	s.mu.Unlock()
}

// demoteParsedCacheEntryLocked preserves a cheap CST skeleton while dropping
// analysis-heavy fields.  The caller must hold s.mu.
func (s *Server) demoteParsedCacheEntryLocked(key string, entry parsedDocumentCacheEntry) bool {
	if s.documentStore == nil {
		return false
	}
	identity := strings.TrimPrefix(strings.TrimPrefix(key, "doc:"), "text:")
	uri := ""
	var cached *workspacepkg.CachedDocument
	for candidateURI, candidate := range s.documentStore.Cache {
		if candidate == nil || workspacepkg.FileIdentityKeyFromURI(candidateURI) != identity {
			continue
		}
		uri, cached = candidateURI, candidate
		break
	}
	if uri == "" && entry.Parsed != nil && workspacepkg.FileIdentityKeyFromURI(entry.Parsed.URI) == identity {
		uri = entry.Parsed.URI
	}
	if uri == "" {
		if strings.HasPrefix(identity, "/") || strings.Contains(identity, "\\") {
			uri = filePathURI(identity)
		} else {
			uri = identity
		}
	}
	if cached == nil {
		cached = &workspacepkg.CachedDocument{URI: uri, Text: entry.Text, Version: entry.Version, Parsed: entry.Parsed, ParseDepth: "full"}
		s.documentStore.Cache[uri] = cached
	}
	if cached.Text == "" {
		cached.Text = entry.Text
	}
	if cached.Parsed == nil {
		cached.Parsed = entry.Parsed
	}
	if cached.ParseDepth == "" {
		cached.ParseDepth = "full"
	}
	return s.documentStore.Demote(cached, 0, func(documentURI, text string) any {
		language := "VBScript"
		if parsed, ok := cached.Parsed.(*core.ParsedDocument); ok && parsed.DefaultLanguage != "" {
			language = string(parsed.DefaultLanguage)
		}
		return skeletonParsedDocument(documentURI, text, language)
	})
}

func skeletonParsedDocument(uri, text, defaultLanguage string) *core.ParsedDocument {
	parsed := core.ParseDocument(uri, text, core.Settings{DefaultLanguage: defaultLanguage})
	if parsed == nil {
		return &core.ParsedDocument{URI: uri, Text: text}
	}
	// Keep only the cheap structural data needed for include invalidation.  The
	// full regions, parse errors, and embedded text are rebuilt on next access.
	parsed.Regions = nil
	parsed.Errors = nil
	return parsed
}
