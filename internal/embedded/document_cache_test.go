package embedded

import (
	"context"
	"runtime"
	"strings"
	"sync"
	"sync/atomic"
	"testing"

	cssls "github.com/yottonoko/vscode-css-languageservice-go"
	csslsp "github.com/yottonoko/vscode-css-languageservice-go/lsp"
	htmlservice "github.com/yottonoko/vscode-html-languageservice-go"

	"github.com/yottonoko/classic-web-system-lsp/internal/core"
)

type countingHTMLLanguageService struct {
	htmlservice.LanguageService
	parseCount atomic.Int64
}

func (s *countingHTMLLanguageService) ParseHTMLDocument(document *htmlservice.TextDocument) *htmlservice.HTMLDocument {
	s.parseCount.Add(1)
	return s.LanguageService.ParseHTMLDocument(document)
}

type countingCSSLanguageService struct {
	cssls.LanguageService
	parseCount atomic.Int64
}

func (s *countingCSSLanguageService) ParseStylesheet(document *csslsp.TextDocument) *cssls.Stylesheet {
	s.parseCount.Add(1)
	return s.LanguageService.ParseStylesheet(document)
}

func TestEmbeddedDocumentCachesShareOneParsePerParsedDocumentAndLanguage(t *testing.T) {
	const source = `<main><section class="card">value</section></main>
<style>.card { color: coral; }</style>`
	parsed := core.ParseDocument("file:///cache.asp", source, core.Settings{})
	sourceDocument := core.NewTextDocument(parsed.URI, "classic-asp", 1, source)
	htmlService := &countingHTMLLanguageService{LanguageService: htmlservice.GetLanguageService()}
	cssService := &countingCSSLanguageService{LanguageService: cssls.GetCSSLanguageService()}
	html := HTML{service: htmlService}
	css := CSS{service: cssService}

	html.FoldingRanges(parsed)
	html.Diagnostics(parsed)
	css.FoldingRanges(parsed)
	if got := htmlService.parseCount.Load(); got != 0 {
		t.Fatalf("HTML source-only features parsed %d times, want 0", got)
	}
	if got := cssService.parseCount.Load(); got != 0 {
		t.Fatalf("CSS source-only features parsed %d times, want 0", got)
	}

	htmlPosition := sourceDocument.PositionAt(strings.Index(source, "section") + 1)
	html.Complete(parsed, htmlPosition)
	html.Hover(parsed, htmlPosition)
	html.Highlights(parsed, htmlPosition)
	html.DocumentSymbols(parsed)
	if got := htmlService.parseCount.Load(); got != 1 {
		t.Fatalf("HTML parse count = %d, want 1", got)
	}
	if got := cssService.parseCount.Load(); got != 0 {
		t.Fatalf("HTML features initialized CSS cache: parse count = %d", got)
	}

	cssPosition := sourceDocument.PositionAt(strings.Index(source, "color") + 1)
	css.Complete(context.Background(), parsed, cssPosition)
	css.Hover(parsed, cssPosition)
	css.Diagnostics(parsed)
	css.DocumentSymbols(parsed)
	if got := cssService.parseCount.Load(); got != 1 {
		t.Fatalf("CSS parse count = %d, want 1", got)
	}

	nextSource := strings.Replace(source, "coral", "lavender", 1)
	next := core.ParseDocument(parsed.URI, nextSource, core.Settings{})
	html.DocumentSymbols(next)
	css.Diagnostics(next)
	if got := htmlService.parseCount.Load(); got != 2 {
		t.Fatalf("HTML parse count after new ParsedDocument = %d, want 2", got)
	}
	if got := cssService.parseCount.Load(); got != 2 {
		t.Fatalf("CSS parse count after new ParsedDocument = %d, want 2", got)
	}
}

func TestEmbeddedDocumentCachesReuseUnaffectedIncrementalLanguage(t *testing.T) {
	const source = `<main>value</main>
<% count = 1 %>
<style>main { color: coral; }</style>`
	parsed := core.ParseDocument("file:///incremental-cache.asp", source, core.Settings{})
	htmlService := &countingHTMLLanguageService{LanguageService: htmlservice.GetLanguageService()}
	cssService := &countingCSSLanguageService{LanguageService: cssls.GetCSSLanguageService()}
	html := HTML{service: htmlService}
	css := CSS{service: cssService}
	html.DocumentSymbols(parsed)
	css.Diagnostics(parsed)

	parsed = updateEmbeddedCacheTestDocument(t, parsed, "value", "values")
	html.DocumentSymbols(parsed)
	css.Diagnostics(parsed)
	if got := htmlService.parseCount.Load(); got != 2 {
		t.Fatalf("HTML parse count after HTML edit = %d, want 2", got)
	}
	if got := cssService.parseCount.Load(); got != 1 {
		t.Fatalf("CSS parse count after HTML edit = %d, want 1", got)
	}

	parsed = updateEmbeddedCacheTestDocument(t, parsed, "coral", "azure")
	html.DocumentSymbols(parsed)
	css.Diagnostics(parsed)
	if got := htmlService.parseCount.Load(); got != 2 {
		t.Fatalf("HTML parse count after same-width CSS edit = %d, want 2", got)
	}
	if got := cssService.parseCount.Load(); got != 2 {
		t.Fatalf("CSS parse count after CSS edit = %d, want 2", got)
	}

	parsed = updateEmbeddedCacheTestDocument(t, parsed, "count = 1", "count = 22")
	html.DocumentSymbols(parsed)
	css.Diagnostics(parsed)
	if got := htmlService.parseCount.Load(); got != 3 {
		t.Fatalf("HTML parse count after width-changing server edit = %d, want 3", got)
	}
	if got := cssService.parseCount.Load(); got != 2 {
		t.Fatalf("CSS parse count after server edit = %d, want 2", got)
	}
}

func updateEmbeddedCacheTestDocument(t *testing.T, parsed *core.ParsedDocument, oldText, newText string) *core.ParsedDocument {
	t.Helper()
	start := strings.Index(parsed.Text, oldText)
	if start < 0 {
		t.Fatalf("source token %q not found", oldText)
	}
	document := core.NewTextDocument(parsed.URI, "classic-asp", 0, parsed.Text)
	changeRange := document.Range(start, start+len(oldText))
	updated := core.UpdateParsedDocument(parsed, []core.IncrementalChange{{Range: &changeRange, Text: newText}}, core.Settings{})
	if !updated.Incremental {
		t.Fatalf("edit %q -> %q was not incremental: %s", oldText, newText, updated.Reason)
	}
	return updated.Parsed
}

func TestEmbeddedDocumentCachesSingleflightConcurrentInitialization(t *testing.T) {
	const workers = 64
	const source = `<article><h1>title</h1></article>
<style>article { color: lavender; }</style>`
	parsed := core.ParseDocument("file:///concurrent-cache.asp", source, core.Settings{})
	htmlService := &countingHTMLLanguageService{LanguageService: htmlservice.GetLanguageService()}
	cssService := &countingCSSLanguageService{LanguageService: cssls.GetCSSLanguageService()}
	html := HTML{service: htmlService}
	css := CSS{service: cssService}
	htmlCaches := make([]*htmlDocumentCache, workers)
	cssCaches := make([]*cssDocumentCache, workers)

	var ready sync.WaitGroup
	ready.Add(workers)
	start := make(chan struct{})
	var complete sync.WaitGroup
	complete.Add(workers)
	for index := range workers {
		go func() {
			defer complete.Done()
			ready.Done()
			<-start
			htmlCaches[index] = html.cachedHTMLDocument(parsed)
			cssCaches[index] = css.cachedCSSDocument(parsed)
		}()
	}
	ready.Wait()
	close(start)
	complete.Wait()

	if got := htmlService.parseCount.Load(); got != 1 {
		t.Fatalf("concurrent HTML parse count = %d, want 1", got)
	}
	if got := cssService.parseCount.Load(); got != 1 {
		t.Fatalf("concurrent CSS parse count = %d, want 1", got)
	}
	for index := 1; index < workers; index++ {
		if htmlCaches[index] != htmlCaches[0] || htmlCaches[index].service.document != htmlCaches[0].service.document || htmlCaches[index].service.htmlDocument != htmlCaches[0].service.htmlDocument {
			t.Fatalf("worker %d received a different HTML cache", index)
		}
		if cssCaches[index] != cssCaches[0] || cssCaches[index].service.document != cssCaches[0].service.document || cssCaches[index].service.stylesheet != cssCaches[0].service.stylesheet {
			t.Fatalf("worker %d received a different CSS cache", index)
		}
	}
}

func TestEmbeddedDocumentCacheReuseSynchronizesWithPredecessorInitialization(t *testing.T) {
	const source = `<main>` + `value` + `</main>
<style>main { color: coral; }</style>`

	t.Run("HTML", func(t *testing.T) {
		previous := core.ParseDocument("file:///html-reuse-race.asp", strings.Repeat(" ", 256*1024)+source, core.Settings{})
		core.BuildVirtualDocument(previous, core.LanguageHTML)
		candidate := &htmlDocumentCache{}
		previous.LoadOrStoreRuntimeAnalysis(htmlDocumentCacheKey, candidate)
		next := updateEmbeddedCacheTestDocument(t, previous, "coral", "azure")
		virtual := core.BuildVirtualDocument(next, core.LanguageHTML)

		runEmbeddedCacheReuseRace(t, func() {
			HTML{}.cachedHTMLSource(previous)
		}, func() {
			_ = reusableHTMLServiceDocument(next, virtual)
		})
	})

	t.Run("CSS", func(t *testing.T) {
		previous := core.ParseDocument("file:///css-reuse-race.asp", source+strings.Repeat(" ", 256*1024), core.Settings{})
		core.BuildVirtualDocument(previous, core.LanguageCSS)
		candidate := &cssDocumentCache{}
		previous.LoadOrStoreRuntimeAnalysis(cssDocumentCacheKey, candidate)
		next := updateEmbeddedCacheTestDocument(t, previous, "value", "values")
		virtual := core.BuildVirtualDocument(next, core.LanguageCSS)

		runEmbeddedCacheReuseRace(t, func() {
			CSS{}.cachedCSSSource(previous)
		}, func() {
			_ = reusableCSSServiceDocument(next, virtual)
		})
	})
}

func TestEmbeddedDocumentCacheOwnerCacheTracksInitialization(t *testing.T) {
	const source = `<main>value</main><style>main { color: coral; }</style>`

	t.Run("HTML", func(t *testing.T) {
		parsed := core.ParseDocument("file:///html-owner-generation.asp", source, core.Settings{})
		candidate := &htmlDocumentCache{}
		parsed.LoadOrStoreRuntimeAnalysis(htmlDocumentCacheKey, candidate)
		before := embeddedRuntimeOwnerTotal(parsed)

		cache := HTML{}.cachedHTMLSource(parsed)
		after := embeddedRuntimeOwnerTotal(parsed)
		if cache != candidate || after <= before {
			t.Fatalf("HTML owner bytes after initialization = %d, before=%d cache reused=%t", after, before, cache == candidate)
		}
	})

	t.Run("CSS", func(t *testing.T) {
		parsed := core.ParseDocument("file:///css-owner-generation.asp", source, core.Settings{})
		candidate := &cssDocumentCache{}
		parsed.LoadOrStoreRuntimeAnalysis(cssDocumentCacheKey, candidate)
		before := embeddedRuntimeOwnerTotal(parsed)

		cache := CSS{}.cachedCSSSource(parsed)
		after := embeddedRuntimeOwnerTotal(parsed)
		if cache != candidate || after <= before {
			t.Fatalf("CSS owner bytes after initialization = %d, before=%d cache reused=%t", after, before, cache == candidate)
		}
	})
}

func runEmbeddedCacheReuseRace(t *testing.T, initialize func(), reuse func()) {
	t.Helper()
	const (
		workers    = 64
		iterations = 2000
	)
	start := make(chan struct{})
	var complete sync.WaitGroup
	complete.Add(workers + 1)
	for range workers {
		go func() {
			defer complete.Done()
			<-start
			for iteration := range iterations {
				reuse()
				if iteration%32 == 0 {
					runtime.Gosched()
				}
			}
		}()
	}
	go func() {
		defer complete.Done()
		<-start
		initialize()
	}()
	close(start)
	complete.Wait()
}

func TestEmbeddedHTMLDocumentCacheSharedServiceOwnerAccounting(t *testing.T) {
	const source = `<main>value</main>
<style>main { color: coral; }</style>`
	html := HTML{service: htmlservice.GetLanguageService()}
	previous := core.ParseDocument("file:///html-owner-accounting.asp", source, core.Settings{})
	previousCache := html.cachedHTMLSource(previous)
	next := updateEmbeddedCacheTestDocument(t, previous, "coral", "azure")
	nextCache := html.cachedHTMLSource(next)
	if nextCache.service != previousCache.service {
		t.Fatal("unchanged HTML region did not reuse the predecessor service")
	}
	if _, ok := next.LoadPreviousRuntimeAnalysis(htmlDocumentCacheKey); ok {
		t.Fatal("HTML cache retained the predecessor after initialization")
	}

	combined := embeddedRuntimeOwnerBytes(previous, next)
	serviceBytes, ok := combined[previousCache.service]
	if !ok || serviceBytes <= 0 {
		t.Fatalf("combined HTML owners = %#v, want shared service owner", combined)
	}
	remaining := embeddedRuntimeOwnerBytes(next)
	cacheBytes := remaining[nextCache] + remaining[nextCache.source] + remaining[nextCache.service]
	if cacheBytes != nextCache.EstimateBytes()+24 {
		t.Fatalf("HTML component owner bytes = %d, want cache estimate plus one entry overhead %d", cacheBytes, nextCache.EstimateBytes()+24)
	}
	if got := remaining[previousCache.service]; got != serviceBytes {
		t.Fatalf("HTML service bytes after predecessor removal = %d, want %d", got, serviceBytes)
	}

	changed := updateEmbeddedCacheTestDocument(t, previous, "value", "values")
	changedCache := html.cachedHTMLSource(changed)
	if changedCache.service == previousCache.service {
		t.Fatal("changed HTML region reused the predecessor service")
	}
	changedOwners := embeddedRuntimeOwnerBytes(previous, changed)
	if _, ok := changedOwners[previousCache.service]; !ok {
		t.Fatal("combined HTML owners lost the predecessor service")
	}
	if _, ok := changedOwners[changedCache.service]; !ok {
		t.Fatal("combined HTML owners lost the changed-region service")
	}
}

func TestEmbeddedCSSDocumentCacheSharedServiceOwnerAccounting(t *testing.T) {
	const source = `<main>value</main>
<style>main { color: coral; }</style>`
	css := CSS{service: cssls.GetCSSLanguageService()}
	previous := core.ParseDocument("file:///css-owner-accounting.asp", source, core.Settings{})
	previousCache := css.cachedCSSSource(previous)
	next := updateEmbeddedCacheTestDocument(t, previous, "value", "values")
	nextCache := css.cachedCSSSource(next)
	if nextCache.service != previousCache.service {
		t.Fatal("unchanged CSS region did not reuse the predecessor service")
	}
	if _, ok := next.LoadPreviousRuntimeAnalysis(cssDocumentCacheKey); ok {
		t.Fatal("CSS cache retained the predecessor after initialization")
	}

	combined := embeddedRuntimeOwnerBytes(previous, next)
	serviceBytes, ok := combined[previousCache.service]
	if !ok || serviceBytes <= 0 {
		t.Fatalf("combined CSS owners = %#v, want shared service owner", combined)
	}
	remaining := embeddedRuntimeOwnerBytes(next)
	cacheBytes := remaining[nextCache] + remaining[nextCache.source] + remaining[nextCache.service]
	if cacheBytes != nextCache.EstimateBytes()+24 {
		t.Fatalf("CSS component owner bytes = %d, want cache estimate plus one entry overhead %d", cacheBytes, nextCache.EstimateBytes()+24)
	}
	if got := remaining[previousCache.service]; got != serviceBytes {
		t.Fatalf("CSS service bytes after predecessor removal = %d, want %d", got, serviceBytes)
	}

	changed := updateEmbeddedCacheTestDocument(t, previous, "coral", "azure")
	changedCache := css.cachedCSSSource(changed)
	if changedCache.service == previousCache.service {
		t.Fatal("changed CSS region reused the predecessor service")
	}
	changedOwners := embeddedRuntimeOwnerBytes(previous, changed)
	if _, ok := changedOwners[previousCache.service]; !ok {
		t.Fatal("combined CSS owners lost the predecessor service")
	}
	if _, ok := changedOwners[changedCache.service]; !ok {
		t.Fatal("combined CSS owners lost the changed-region service")
	}
}

func embeddedRuntimeOwnerBytes(parsed ...*core.ParsedDocument) map[any]int64 {
	owners := make(map[any]int64)
	for _, document := range parsed {
		if document == nil {
			continue
		}
		for _, owner := range document.RuntimeAnalysisMemoryOwners() {
			if owner.Bytes > owners[owner.Identity] {
				owners[owner.Identity] = owner.Bytes
			}
		}
	}
	return owners
}

func embeddedRuntimeOwnerTotal(parsed *core.ParsedDocument) int64 {
	var total int64
	for _, owner := range parsed.RuntimeAnalysisMemoryOwners() {
		total += owner.Bytes
	}
	return total
}

var (
	benchmarkHTMLDocument *htmlservice.HTMLDocument
	benchmarkStylesheet   *cssls.Stylesheet
)

func BenchmarkEmbeddedDocumentCacheWarm(b *testing.B) {
	const source = `<main><section class="card">value</section></main>
<style>.card { color: coral; padding: 1rem; }</style>`

	b.Run("HTML", func(b *testing.B) {
		parsed := core.ParseDocument("file:///benchmark-cache.asp", source, core.Settings{})
		html := NewHTML()
		html.cachedHTMLDocument(parsed)
		b.ReportAllocs()
		b.ResetTimer()
		for range b.N {
			benchmarkHTMLDocument = html.cachedHTMLDocument(parsed).service.htmlDocument
		}
	})

	b.Run("CSS", func(b *testing.B) {
		parsed := core.ParseDocument("file:///benchmark-cache.asp", source, core.Settings{})
		css := NewCSS()
		css.cachedCSSDocument(parsed)
		b.ReportAllocs()
		b.ResetTimer()
		for range b.N {
			benchmarkStylesheet = css.cachedCSSDocument(parsed).service.stylesheet
		}
	})
}
