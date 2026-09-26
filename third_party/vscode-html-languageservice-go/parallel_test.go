package htmlservice

import (
	"reflect"
	"runtime"
	"strings"
	"sync"
	"testing"
)

func TestParallelSymbolsMatchSequential(t *testing.T) {
	text := benchmarkHTML(600)
	ls := GetLanguageService()
	doc := NewTextDocument("file:///parallel.html", "html", 0, text)
	htmlDoc := ls.ParseHTMLDocument(doc)

	var sequential []DocumentSymbol
	withGOMAXPROCS(1, func() {
		sequential = ls.FindDocumentSymbols2(doc, htmlDoc)
	})

	var parallel []DocumentSymbol
	withGOMAXPROCS(4, func() {
		parallel = ls.FindDocumentSymbols2(doc, htmlDoc)
	})

	if !reflect.DeepEqual(parallel, sequential) {
		t.Fatalf("parallel symbols differ from sequential\nparallel: %#v\nsequential: %#v", parallel, sequential)
	}
}

func TestParallelSelectionRangesMatchSequential(t *testing.T) {
	text := benchmarkHTML(1500)
	ls := GetLanguageService()
	doc := NewTextDocument("file:///parallel.html", "html", 0, text)
	positions := parallelSelectionPositions(doc, text)
	if len(positions) < parallelSelectionThreshold {
		t.Fatalf("test fixture produced %d positions, want at least %d", len(positions), parallelSelectionThreshold)
	}

	var sequential []SelectionRange
	withGOMAXPROCS(1, func() {
		sequential = ls.GetSelectionRanges(doc, positions)
	})

	var parallel []SelectionRange
	withGOMAXPROCS(4, func() {
		parallel = ls.GetSelectionRanges(doc, positions)
	})

	if !reflect.DeepEqual(parallel, sequential) {
		t.Fatalf("parallel selection ranges differ from sequential\nparallel: %#v\nsequential: %#v", parallel, sequential)
	}
}

func TestParallelLinksMatchSequential(t *testing.T) {
	text := parallelLinksHTML(4100)
	ls := GetLanguageService()
	doc := NewTextDocument("file:///parallel.html", "html", 0, text)
	context := parallelIdentityContext{}

	var sequential []DocumentLink
	withGOMAXPROCS(1, func() {
		sequential = ls.FindDocumentLinks(doc, context)
	})

	var parallel []DocumentLink
	withGOMAXPROCS(4, func() {
		parallel = ls.FindDocumentLinks(doc, context)
	})

	if !reflect.DeepEqual(parallel, sequential) {
		t.Fatalf("parallel links differ from sequential\nparallel: %#v\nsequential: %#v", parallel, sequential)
	}
}

func parallelLinksHTML(count int) string {
	var builder strings.Builder
	builder.WriteString("<!doctype html><html><body>")
	for i := 0; i < count; i++ {
		builder.WriteString(`<a href="/docs/page.html">link</a>`)
	}
	builder.WriteString("</body></html>")
	return builder.String()
}

func TestHTMLDataManagerVoidElementCacheConcurrent(t *testing.T) {
	ls := GetLanguageService()
	doc := NewTextDocument("file:///parallel.html", "html", 0, benchmarkHTML(20))

	var wg sync.WaitGroup
	for i := 0; i < 8; i++ {
		wg.Add(1)
		go func() {
			defer wg.Done()
			for j := 0; j < 100; j++ {
				_ = ls.GetFoldingRanges(doc)
			}
		}()
	}
	wg.Wait()
}

type parallelIdentityContext struct{}

func (parallelIdentityContext) ResolveReference(ref, base string) (string, bool) {
	return ref, true
}

func withGOMAXPROCS(n int, fn func()) {
	previous := runtime.GOMAXPROCS(n)
	defer runtime.GOMAXPROCS(previous)
	fn()
}

func parallelSelectionPositions(doc *TextDocument, text string) []Position {
	var positions []Position
	for _, marker := range []string{"<h2>Title ", `href="/docs/`, "<strong>nested"} {
		for offset := 0; offset < len(text); {
			index := strings.Index(text[offset:], marker)
			if index < 0 {
				break
			}
			positionOffset := offset + index + len(marker)
			positions = append(positions, doc.PositionAt(positionOffset))
			offset += index + len(marker)
		}
	}
	return positions
}
