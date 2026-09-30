package embedded

import (
	"sync"
	"sync/atomic"

	cssls "github.com/yottonoko/vscode-css-languageservice-go"
	csslsp "github.com/yottonoko/vscode-css-languageservice-go/lsp"
	htmlservice "github.com/yottonoko/vscode-html-languageservice-go"

	"github.com/yottonoko/classic-web-system-lsp/internal/core"
)

const (
	htmlDocumentCacheKey = "embedded.html-document-cache.v1"
	cssDocumentCacheKey  = "embedded.css-document-cache.v1"
)

type htmlDocumentCache struct {
	sourceOnce sync.Once
	memoryMu   sync.RWMutex
	memoryGen  atomic.Uint64

	virtual core.VirtualDocument
	source  *core.TextDocument
	service *htmlServiceDocumentCache
}

type htmlServiceDocumentCache struct {
	parseOnce    sync.Once
	document     *htmlservice.TextDocument
	htmlDocument *htmlservice.HTMLDocument
}

func (h HTML) cachedHTMLSource(parsed *core.ParsedDocument) *htmlDocumentCache {
	if actual, ok := parsed.LoadRuntimeAnalysis(htmlDocumentCacheKey); ok {
		cache := actual.(*htmlDocumentCache)
		cache.initializeSource(parsed)
		parsed.ReleasePreviousRuntimeAnalysis(htmlDocumentCacheKey, cache)
		return cache
	}
	candidate := &htmlDocumentCache{}
	actual, _ := parsed.LoadOrStoreRuntimeAnalysis(htmlDocumentCacheKey, candidate)
	cache := actual.(*htmlDocumentCache)
	cache.initializeSource(parsed)
	parsed.ReleasePreviousRuntimeAnalysis(htmlDocumentCacheKey, cache)
	return cache
}

func (c *htmlDocumentCache) initializeSource(parsed *core.ParsedDocument) {
	c.sourceOnce.Do(func() {
		virtual := core.BuildVirtualDocument(parsed, core.LanguageHTML)
		source := core.SourceDocument(parsed)
		service := reusableHTMLServiceDocument(parsed, virtual)
		if service == nil {
			service = &htmlServiceDocumentCache{
				document: htmlservice.NewTextDocument(htmlservice.DocumentUri(virtual.URI), "html", 0, virtual.Text),
			}
		}
		c.memoryMu.Lock()
		c.virtual = virtual
		c.source = source
		c.service = service
		c.memoryGen.Add(1)
		c.memoryMu.Unlock()
	})
}

func (c *htmlDocumentCache) RuntimeAnalysisMemoryOwnerGeneration() uint64 {
	if c == nil {
		return 0
	}
	return c.memoryGen.Load()
}

func (c *htmlDocumentCache) EstimateBytes() int64 {
	if c == nil {
		return 0
	}
	c.memoryMu.RLock()
	defer c.memoryMu.RUnlock()
	bytes := int64(256)
	if c.source != nil {
		bytes += c.source.EstimateBytes()
	}
	if c.service != nil {
		// The service document and parsed tree scale with the embedded source.
		// Their source text is shared with virtual and is not charged again.
		bytes += 512 + int64(len(c.virtual.Text))*2
	}
	return bytes
}

func (c *htmlDocumentCache) RuntimeAnalysisMemoryOwnerSet() []core.RuntimeAnalysisMemoryOwner {
	if c == nil {
		return nil
	}
	c.memoryMu.RLock()
	defer c.memoryMu.RUnlock()
	owners := []core.RuntimeAnalysisMemoryOwner{{Identity: c, Bytes: 256}}
	if c.source != nil {
		bytes := c.source.EstimateBytes()
		if bytes > 0 {
			owners = append(owners, core.RuntimeAnalysisMemoryOwner{Identity: c.source, Bytes: bytes})
		}
	}
	if c.service != nil {
		bytes := int64(512) + int64(len(c.virtual.Text))*2
		if bytes > 0 {
			owners = append(owners, core.RuntimeAnalysisMemoryOwner{Identity: c.service, Bytes: bytes})
		}
	}
	return owners
}

func reusableHTMLServiceDocument(parsed *core.ParsedDocument, virtual core.VirtualDocument) *htmlServiceDocumentCache {
	if parsed == nil || parsed.ChangeImpact.Affects(core.LanguageHTML) {
		return nil
	}
	actual, ok := parsed.LoadPreviousRuntimeAnalysis(htmlDocumentCacheKey)
	if !ok {
		return nil
	}
	previousCache := actual.(*htmlDocumentCache)
	previousVirtual, previousService := previousCache.sourceSnapshot()
	if previousVirtual.URI != virtual.URI || previousVirtual.LanguageID != virtual.LanguageID || previousVirtual.Text != virtual.Text {
		return nil
	}
	return previousService
}

func (c *htmlDocumentCache) sourceSnapshot() (core.VirtualDocument, *htmlServiceDocumentCache) {
	if c == nil {
		return core.VirtualDocument{}, nil
	}
	c.memoryMu.RLock()
	virtual, service := c.virtual, c.service
	c.memoryMu.RUnlock()
	return virtual, service
}

func (h HTML) cachedHTMLDocument(parsed *core.ParsedDocument) *htmlDocumentCache {
	cache := h.cachedHTMLSource(parsed)
	cache.service.parseOnce.Do(func() {
		cache.service.htmlDocument = h.service.ParseHTMLDocument(cache.service.document)
	})
	return cache
}

type cssDocumentCache struct {
	sourceOnce sync.Once
	memoryMu   sync.RWMutex
	memoryGen  atomic.Uint64

	virtual core.VirtualDocument
	source  *core.TextDocument
	service *cssServiceDocumentCache
}

type cssServiceDocumentCache struct {
	parseOnce  sync.Once
	document   *csslsp.TextDocument
	stylesheet *cssls.Stylesheet
}

func (c CSS) cachedCSSSource(parsed *core.ParsedDocument) *cssDocumentCache {
	if actual, ok := parsed.LoadRuntimeAnalysis(cssDocumentCacheKey); ok {
		cache := actual.(*cssDocumentCache)
		cache.initializeSource(parsed)
		parsed.ReleasePreviousRuntimeAnalysis(cssDocumentCacheKey, cache)
		return cache
	}
	candidate := &cssDocumentCache{}
	actual, _ := parsed.LoadOrStoreRuntimeAnalysis(cssDocumentCacheKey, candidate)
	cache := actual.(*cssDocumentCache)
	cache.initializeSource(parsed)
	parsed.ReleasePreviousRuntimeAnalysis(cssDocumentCacheKey, cache)
	return cache
}

func (c *cssDocumentCache) initializeSource(parsed *core.ParsedDocument) {
	c.sourceOnce.Do(func() {
		virtual := core.BuildVirtualDocument(parsed, core.LanguageCSS)
		source := core.SourceDocument(parsed)
		service := reusableCSSServiceDocument(parsed, virtual)
		if service == nil {
			service = &cssServiceDocumentCache{
				document: csslsp.NewTextDocument(csslsp.DocumentURI(virtual.URI), "css", 0, virtual.Text),
			}
		}
		c.memoryMu.Lock()
		c.virtual = virtual
		c.source = source
		c.service = service
		c.memoryGen.Add(1)
		c.memoryMu.Unlock()
	})
}

func (c *cssDocumentCache) RuntimeAnalysisMemoryOwnerGeneration() uint64 {
	if c == nil {
		return 0
	}
	return c.memoryGen.Load()
}

func (c *cssDocumentCache) EstimateBytes() int64 {
	if c == nil {
		return 0
	}
	c.memoryMu.RLock()
	defer c.memoryMu.RUnlock()
	bytes := int64(256)
	if c.source != nil {
		bytes += c.source.EstimateBytes()
	}
	if c.service != nil {
		bytes += 512 + int64(len(c.virtual.Text))*2
	}
	return bytes
}

func (c *cssDocumentCache) RuntimeAnalysisMemoryOwnerSet() []core.RuntimeAnalysisMemoryOwner {
	if c == nil {
		return nil
	}
	c.memoryMu.RLock()
	defer c.memoryMu.RUnlock()
	owners := []core.RuntimeAnalysisMemoryOwner{{Identity: c, Bytes: 256}}
	if c.source != nil {
		bytes := c.source.EstimateBytes()
		if bytes > 0 {
			owners = append(owners, core.RuntimeAnalysisMemoryOwner{Identity: c.source, Bytes: bytes})
		}
	}
	if c.service != nil {
		bytes := int64(512) + int64(len(c.virtual.Text))*2
		if bytes > 0 {
			owners = append(owners, core.RuntimeAnalysisMemoryOwner{Identity: c.service, Bytes: bytes})
		}
	}
	return owners
}

func reusableCSSServiceDocument(parsed *core.ParsedDocument, virtual core.VirtualDocument) *cssServiceDocumentCache {
	if parsed == nil || parsed.ChangeImpact.Affects(core.LanguageCSS) {
		return nil
	}
	actual, ok := parsed.LoadPreviousRuntimeAnalysis(cssDocumentCacheKey)
	if !ok {
		return nil
	}
	previousCache := actual.(*cssDocumentCache)
	previousVirtual, previousService := previousCache.sourceSnapshot()
	if previousVirtual.URI != virtual.URI || previousVirtual.LanguageID != virtual.LanguageID || previousVirtual.Text != virtual.Text {
		return nil
	}
	return previousService
}

func (c *cssDocumentCache) sourceSnapshot() (core.VirtualDocument, *cssServiceDocumentCache) {
	if c == nil {
		return core.VirtualDocument{}, nil
	}
	c.memoryMu.RLock()
	virtual, service := c.virtual, c.service
	c.memoryMu.RUnlock()
	return virtual, service
}

func (c CSS) cachedCSSDocument(parsed *core.ParsedDocument) *cssDocumentCache {
	cache := c.cachedCSSSource(parsed)
	cache.service.parseOnce.Do(func() {
		cache.service.stylesheet = c.service.ParseStylesheet(cache.service.document)
	})
	return cache
}
