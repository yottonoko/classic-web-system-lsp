package htmlservice

import (
	"fmt"
	"strings"
	"testing"
)

func BenchmarkLanguageServiceOperations(b *testing.B) {
	text := benchmarkHTML(300)
	ls := GetLanguageService(LanguageServiceOptions{HTMLBeautifier: benchmarkNoopBeautifier{}})
	doc := NewTextDocument("file:///bench.html", "html", 0, text)
	htmlDoc := ls.ParseHTMLDocument(doc)
	completionPos := doc.PositionAt(strings.LastIndex(text, "<div cl") + len("<div cl"))
	hoverPos := doc.PositionAt(strings.Index(text, "<section") + 2)
	selectionPositions := []Position{
		doc.PositionAt(len(text) / 4),
		doc.PositionAt(len(text) / 2),
		doc.PositionAt(len(text) * 3 / 4),
	}

	b.Run("scan", func(b *testing.B) {
		for i := 0; i < b.N; i++ {
			scanner := ls.CreateScanner(text)
			for scanner.Scan() != TokenTypeEOS {
			}
		}
	})
	b.Run("parse", func(b *testing.B) {
		for i := 0; i < b.N; i++ {
			_ = ls.ParseHTMLDocument(doc)
		}
	})
	b.Run("folding", func(b *testing.B) {
		for i := 0; i < b.N; i++ {
			_ = ls.GetFoldingRanges(doc)
		}
	})
	b.Run("links", func(b *testing.B) {
		ctx := benchmarkDocumentContext{}
		for i := 0; i < b.N; i++ {
			_ = ls.FindDocumentLinks(doc, ctx)
		}
	})
	b.Run("completion", func(b *testing.B) {
		for i := 0; i < b.N; i++ {
			_ = ls.DoComplete(doc, completionPos, htmlDoc, nil)
		}
	})
	b.Run("selectionRanges", func(b *testing.B) {
		for i := 0; i < b.N; i++ {
			_ = ls.GetSelectionRanges(doc, selectionPositions)
		}
	})
	b.Run("hover", func(b *testing.B) {
		for i := 0; i < b.N; i++ {
			_ = ls.DoHover(doc, hoverPos, htmlDoc, nil)
		}
	})
}

type benchmarkDocumentContext struct{}

func (benchmarkDocumentContext) ResolveReference(ref, base string) (string, bool) {
	return ref, true
}

type benchmarkNoopBeautifier struct{}

func (benchmarkNoopBeautifier) BeautifyHTML(source string, options BeautifyHTMLOptions) (string, error) {
	return source, nil
}

func benchmarkHTML(count int) string {
	var b strings.Builder
	b.WriteString("<!doctype html>\n<html>\n<head>\n")
	b.WriteString("<title>bench</title><link href=\"/assets/site.css\" rel=\"stylesheet\">\n")
	b.WriteString("<style>.card{display:flex;color:#123}.item{padding:4px}</style>\n")
	b.WriteString("</head>\n<body>\n<main id=\"app\">\n")
	for i := 0; i < count; i++ {
		fmt.Fprintf(&b, "<section id=\"s%d\" class=\"card item\" data-index=\"%d\">\n", i, i)
		fmt.Fprintf(&b, "<h2>Title %d</h2><a href=\"/docs/%d.html\">link</a>\n", i, i)
		b.WriteString("<p class=\"copy\">This is a benchmark paragraph with <strong>nested</strong> inline content.</p>\n")
		b.WriteString("<ul><li>alpha</li><li>beta</li><li>gamma</li></ul>\n")
		b.WriteString("</section>\n")
	}
	b.WriteString("<div cl></div>\n</main>\n<script src=\"/assets/app.js\"></script>\n</body>\n</html>\n")
	return b.String()
}
