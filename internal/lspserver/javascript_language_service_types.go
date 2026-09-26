package lspserver

import (
	"context"
	"strconv"
	"sync"

	"github.com/yottonoko/classic-web-system-lsp/internal/core"
	"github.com/yottonoko/classic-web-system-lsp/internal/lsp"
	"github.com/yottonoko/classic-web-system-lsp/internal/tsgoadapter"
)

type javaScriptVirtualFile struct {
	path            string
	uri             string
	owner           string
	virtual         core.VirtualDocument
	virtualDocument *core.TextDocument
	source          *core.TextDocument
}

type javaScriptDiagnosticReport struct {
	Items []lsp.Diagnostic `json:"items"`
}

type javaScriptRequest struct {
	project *tsgoadapter.Project
	active  *javaScriptVirtualFile
	files   map[string]*javaScriptVirtualFile
	state   tsgoadapter.ProjectState
	cache   *javaScriptServiceResultCache
}

const (
	javaScriptServiceResultCacheEntries = 128
	javaScriptServiceResultCacheBytes   = 16 << 20
)

type javaScriptServiceResultCache struct {
	mu       sync.Mutex
	entries  map[string][]byte
	order    []string
	bytes    int
	inflight map[string]*javaScriptServiceResultInflight
}

type javaScriptServiceResultInflight struct {
	done chan struct{}
	raw  []byte
	err  error
}

func newJavaScriptServiceResultCache() *javaScriptServiceResultCache {
	return &javaScriptServiceResultCache{
		entries:  map[string][]byte{},
		inflight: map[string]*javaScriptServiceResultInflight{},
	}
}

func (c *javaScriptServiceResultCache) request(ctx context.Context, key string, load func() ([]byte, error)) ([]byte, error) {
	if c == nil || key == "" {
		return load()
	}
	c.mu.Lock()
	if raw, ok := c.entries[key]; ok {
		c.touchLocked(key)
		result := append([]byte(nil), raw...)
		c.mu.Unlock()
		return result, nil
	}
	if flight := c.inflight[key]; flight != nil {
		c.mu.Unlock()
		select {
		case <-ctx.Done():
			return nil, ctx.Err()
		case <-flight.done:
			return append([]byte(nil), flight.raw...), flight.err
		}
	}
	flight := &javaScriptServiceResultInflight{done: make(chan struct{})}
	c.inflight[key] = flight
	c.mu.Unlock()

	raw, err := load()
	if err == nil && ctx.Err() != nil {
		err = ctx.Err()
		raw = nil
	}
	result := append([]byte(nil), raw...)
	c.mu.Lock()
	delete(c.inflight, key)
	flight.raw = result
	flight.err = err
	if err == nil && len(result) <= javaScriptServiceResultCacheBytes {
		c.insertLocked(key, result)
	}
	close(flight.done)
	c.mu.Unlock()
	return append([]byte(nil), result...), err
}

func (c *javaScriptServiceResultCache) touchLocked(key string) {
	for index, current := range c.order {
		if current == key {
			copy(c.order[index:], c.order[index+1:])
			c.order[len(c.order)-1] = key
			return
		}
	}
}

func (c *javaScriptServiceResultCache) insertLocked(key string, raw []byte) {
	if previous, ok := c.entries[key]; ok {
		c.bytes -= len(previous)
		c.touchLocked(key)
	} else {
		c.order = append(c.order, key)
	}
	copyRaw := append([]byte(nil), raw...)
	c.entries[key] = copyRaw
	c.bytes += len(copyRaw)
	for len(c.order) > javaScriptServiceResultCacheEntries || c.bytes > javaScriptServiceResultCacheBytes {
		oldest := c.order[0]
		c.order = c.order[1:]
		c.bytes -= len(c.entries[oldest])
		delete(c.entries, oldest)
	}
}

func javaScriptServiceMethodCacheable(method string) bool {
	switch method {
	case "textDocument/references", "textDocument/documentHighlight", "textDocument/prepareRename", "textDocument/rename",
		"textDocument/prepareCallHierarchy", "callHierarchy/incomingCalls", "callHierarchy/outgoingCalls":
		return true
	default:
		return false
	}
}

func javaScriptServiceResultCacheKey(request *javaScriptRequest, method string, body []byte) string {
	if request == nil || request.active == nil || !javaScriptServiceMethodCacheable(method) {
		return ""
	}
	return strconv.FormatUint(request.state.Generation, 10) + "\x00" + request.active.uri + "\x00" + method + "\x00" + string(body)
}

func (request *javaScriptRequest) serviceRequest(ctx context.Context, method string, body []byte) ([]byte, error) {
	key := javaScriptServiceResultCacheKey(request, method, body)
	return request.cache.request(ctx, key, func() ([]byte, error) {
		return request.project.Request(ctx, method, body)
	})
}

type javaScriptDocumentSnapshot struct {
	document *core.TextDocument
	version  int
	text     string
}

type javaScriptProjectPreparation struct {
	root                string
	defaultLanguage     string
	explicitTypes       []string
	explicitTypesSet    bool
	compilerOptions     map[string]any
	ignoreProjectConfig bool
	projectConfig       javaScriptProjectConfig
	documentGeneration  uint64
	mappingGeneration   uint64
	documents           map[string]javaScriptDocumentSnapshot
	mappings            map[string]*javaScriptVirtualFile
	mappingsByOwner     map[string][]*javaScriptVirtualFile
	workspaceFiles      map[string]string
	autoImportExports   map[string][]string
	jqueryCompletion    bool
	serviceCache        *javaScriptServiceResultCache
	dirtyOwners         map[string]struct{}
}
