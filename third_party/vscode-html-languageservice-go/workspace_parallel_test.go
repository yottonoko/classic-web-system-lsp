package htmlservice

import (
	"reflect"
	"strings"
	"sync"
	"testing"
)

type workspaceParallelDocument struct {
	doc       *TextDocument
	htmlDoc   *HTMLDocument
	positions []Position
}

type workspaceParallelChecksum struct {
	parseRoots      int
	symbols         int
	folding         int
	links           int
	selectionRanges int
}

func TestLanguageServiceMultiFileConcurrentMatchesSequential(t *testing.T) {
	ls := GetLanguageService()
	documents := makeWorkspaceParallelDocuments(ls, 24, 40)

	sequential := workspaceParallelChecksums(ls, documents, false)
	parallel := workspaceParallelChecksums(ls, documents, true)

	if !reflect.DeepEqual(parallel, sequential) {
		t.Fatalf("parallel workspace checksums differ\nparallel: %#v\nsequential: %#v", parallel, sequential)
	}
}

func TestLanguageServiceConcurrentConfigurationUpdates(t *testing.T) {
	ls := GetLanguageService()
	text := benchmarkHTML(20)
	doc := NewTextDocument("file:///workspace/config.html", "html", 0, text)
	htmlDoc := ls.ParseHTMLDocument(doc)
	position := doc.PositionAt(len("<"))
	completionPosition := doc.PositionAt(strings.LastIndex(text, "<div cl") + len("<div cl"))

	var wg sync.WaitGroup
	for i := 0; i < 4; i++ {
		wg.Add(1)
		go func() {
			defer wg.Done()
			for j := 0; j < 100; j++ {
				ls.SetDataProviders(true, nil)
				ls.SetCompletionParticipants(nil)
			}
		}()
	}
	for i := 0; i < 8; i++ {
		wg.Add(1)
		go func() {
			defer wg.Done()
			for j := 0; j < 100; j++ {
				_ = ls.ParseHTMLDocument(doc)
				_ = ls.DoHover(doc, position, htmlDoc, nil)
				_ = ls.DoComplete(doc, completionPosition, htmlDoc, nil)
				_ = ls.GetFoldingRanges(doc)
				_ = ls.FindDocumentLinks(doc, parallelIdentityContext{})
			}
		}()
	}
	wg.Wait()
}

func makeWorkspaceParallelDocuments(ls LanguageService, fileCount, sectionsPerFile int) []workspaceParallelDocument {
	documents := make([]workspaceParallelDocument, 0, fileCount)
	for i := 0; i < fileCount; i++ {
		text := benchmarkHTML(sectionsPerFile + i%3)
		doc := NewTextDocument(DocumentUri("file:///workspace/parallel.html"), "html", 0, text)
		documents = append(documents, workspaceParallelDocument{
			doc:       doc,
			htmlDoc:   ls.ParseHTMLDocument(doc),
			positions: parallelSelectionPositions(doc, text),
		})
	}
	return documents
}

func workspaceParallelChecksums(ls LanguageService, documents []workspaceParallelDocument, concurrent bool) []workspaceParallelChecksum {
	if concurrent {
		return parallelMapOrdered(documents, 2, func(_ int, document workspaceParallelDocument) workspaceParallelChecksum {
			return workspaceParallelChecksumForDocument(ls, document)
		})
	}
	result := make([]workspaceParallelChecksum, 0, len(documents))
	for _, document := range documents {
		result = append(result, workspaceParallelChecksumForDocument(ls, document))
	}
	return result
}

func workspaceParallelChecksumForDocument(ls LanguageService, document workspaceParallelDocument) workspaceParallelChecksum {
	return workspaceParallelChecksum{
		parseRoots:      len(ls.ParseHTMLDocument(document.doc).Roots),
		symbols:         len(ls.FindDocumentSymbols2(document.doc, document.htmlDoc)),
		folding:         len(ls.GetFoldingRanges(document.doc)),
		links:           len(ls.FindDocumentLinks(document.doc, parallelIdentityContext{})),
		selectionRanges: len(ls.GetSelectionRanges(document.doc, document.positions)),
	}
}
