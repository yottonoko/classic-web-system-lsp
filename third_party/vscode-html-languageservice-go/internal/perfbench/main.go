package main

import (
	"encoding/json"
	"flag"
	"fmt"
	"os"
	"runtime"
	"sort"
	"strings"
	"time"

	htmlservice "github.com/yottonoko/vscode-html-languageservice-go"
)

type identityContext struct{}

func (identityContext) ResolveReference(ref, base string) (string, bool) {
	return ref, true
}

type noopBeautifier struct{}

func (noopBeautifier) BeautifyHTML(source string, options htmlservice.BeautifyHTMLOptions) (string, error) {
	return source, nil
}

type result struct {
	Runtime         string  `json:"runtime"`
	Case            string  `json:"case"`
	SizeBytes       int     `json:"sizeBytes"`
	Operation       string  `json:"operation"`
	Samples         int     `json:"samples"`
	Iterations      int     `json:"iterations"`
	MedianMS        float64 `json:"medianMs"`
	MinMS           float64 `json:"minMs"`
	Checksum        int     `json:"checksum"`
	GoMaxProcs      int     `json:"gomaxprocs,omitempty"`
	ParallelEnabled bool    `json:"parallelEnabled,omitempty"`
}

func main() {
	samples := flag.Int("samples", 7, "sample count per operation")
	gomaxprocs := flag.Int("gomaxprocs", 0, "GOMAXPROCS to use; 0 keeps the runtime default")
	flag.Parse()
	if *gomaxprocs > 0 {
		runtime.GOMAXPROCS(*gomaxprocs)
	}

	counts := []int{100, 300, 1000}
	results := make([]result, 0, len(counts)*10)
	for _, count := range counts {
		name := fmt.Sprintf("sections-%d", count)
		text := makeHTML(count)
		results = append(results, runCase(name, text, *samples)...)
	}
	results = append(results, runWorkspaceCase("workspace-24x100", 24, 100, *samples)...)

	encoder := json.NewEncoder(os.Stdout)
	encoder.SetIndent("", "  ")
	if err := encoder.Encode(results); err != nil {
		panic(err)
	}
}

func runCase(name, text string, samples int) []result {
	ls := htmlservice.GetLanguageService(htmlservice.LanguageServiceOptions{HTMLBeautifier: noopBeautifier{}})
	doc := htmlservice.NewTextDocument("file:///bench.html", "html", 0, text)
	htmlDoc := ls.ParseHTMLDocument(doc)
	completionPos := doc.PositionAt(strings.LastIndex(text, "<div cl") + len("<div cl"))
	hoverPos := doc.PositionAt(strings.Index(text, "<section") + 2)
	renamePositions := sectionRenamePositions(doc, text)
	selectionPositions := []htmlservice.Position{
		doc.PositionAt(len(text) / 4),
		doc.PositionAt(len(text) / 2),
		doc.PositionAt(len(text) * 3 / 4),
	}
	bulkSelectionPositions := selectionPositionsForSections(doc, text)
	formatRange := htmlservice.NewRange(htmlservice.NewPosition(0, 0), doc.PositionAt(len(text)))
	formatOptions := htmlservice.HTMLFormatConfiguration{TabSize: 2, InsertSpaces: true}

	ops := []struct {
		name string
		fn   func() int
	}{
		{"scan", func() int {
			scanner := ls.CreateScanner(text)
			count := 0
			for {
				token := scanner.Scan()
				count += int(token) + scanner.GetTokenOffset()
				if token == htmlservice.TokenTypeEOS {
					break
				}
			}
			return count
		}},
		{"parse", func() int {
			parsed := ls.ParseHTMLDocument(doc)
			return len(parsed.Roots)
		}},
		{"symbols2", func() int {
			return len(ls.FindDocumentSymbols2(doc, htmlDoc))
		}},
		{"folding", func() int {
			return len(ls.GetFoldingRanges(doc))
		}},
		{"links", func() int {
			return len(ls.FindDocumentLinks(doc, identityContext{}))
		}},
		{"hover", func() int {
			hover := ls.DoHover(doc, hoverPos, htmlDoc, nil)
			if hover == nil {
				return 0
			}
			return hoverContentLength(hover.Contents)
		}},
		{"completion", func() int {
			list := ls.DoComplete(doc, completionPos, htmlDoc, nil)
			return len(list.Items)
		}},
		{"selectionRanges", func() int {
			return len(ls.GetSelectionRanges(doc, selectionPositions))
		}},
		{"selectionRangesBulk", func() int {
			return len(ls.GetSelectionRanges(doc, bulkSelectionPositions))
		}},
		{"rename", func() int {
			count := 0
			for _, pos := range renamePositions {
				edit := ls.DoRename(doc, pos, "article", htmlDoc)
				if edit != nil {
					count += len(edit.Changes)
				}
			}
			return count
		}},
		{"format-wrapper", func() int {
			edits := ls.Format(doc, &formatRange, formatOptions)
			return len(edits)
		}},
	}

	results := make([]result, 0, len(ops))
	gomaxprocs := runtime.GOMAXPROCS(0)
	for _, op := range ops {
		iterations := iterationsFor(op.name, len(text))
		median, min, checksum := measure(samples, iterations, op.fn)
		results = append(results, result{
			Runtime:         "go",
			Case:            name,
			SizeBytes:       len(text),
			Operation:       op.name,
			Samples:         samples,
			Iterations:      iterations,
			MedianMS:        median,
			MinMS:           min,
			Checksum:        checksum,
			GoMaxProcs:      gomaxprocs,
			ParallelEnabled: gomaxprocs > 1,
		})
	}
	return results
}

type workspaceDocument struct {
	text               string
	doc                *htmlservice.TextDocument
	htmlDoc            *htmlservice.HTMLDocument
	selectionPositions []htmlservice.Position
}

func runWorkspaceCase(name string, fileCount, sectionsPerFile, samples int) []result {
	ls := htmlservice.GetLanguageService(htmlservice.LanguageServiceOptions{HTMLBeautifier: noopBeautifier{}})
	documents := makeWorkspaceDocuments(ls, fileCount, sectionsPerFile)
	totalSize := 0
	for _, document := range documents {
		totalSize += len(document.text)
	}
	ops := []struct {
		name string
		fn   func() int
	}{
		{"workspaceParse", func() int {
			values := parallelMapOrdered(documents, 2, func(_ int, document workspaceDocument) int {
				return len(ls.ParseHTMLDocument(document.doc).Roots)
			})
			return sumInts(values)
		}},
		{"workspaceSymbols2", func() int {
			values := parallelMapOrdered(documents, 2, func(_ int, document workspaceDocument) int {
				return len(ls.FindDocumentSymbols2(document.doc, document.htmlDoc))
			})
			return sumInts(values)
		}},
		{"workspaceFolding", func() int {
			values := parallelMapOrdered(documents, 2, func(_ int, document workspaceDocument) int {
				return len(ls.GetFoldingRanges(document.doc))
			})
			return sumInts(values)
		}},
		{"workspaceLinks", func() int {
			values := parallelMapOrdered(documents, 2, func(_ int, document workspaceDocument) int {
				return len(ls.FindDocumentLinks(document.doc, identityContext{}))
			})
			return sumInts(values)
		}},
		{"workspaceSelectionRanges", func() int {
			values := parallelMapOrdered(documents, 2, func(_ int, document workspaceDocument) int {
				return len(ls.GetSelectionRanges(document.doc, document.selectionPositions))
			})
			return sumInts(values)
		}},
	}

	results := make([]result, 0, len(ops))
	gomaxprocs := runtime.GOMAXPROCS(0)
	for _, op := range ops {
		iterations := iterationsFor(op.name, totalSize)
		median, min, checksum := measure(samples, iterations, op.fn)
		results = append(results, result{
			Runtime:         "go",
			Case:            name,
			SizeBytes:       totalSize,
			Operation:       op.name,
			Samples:         samples,
			Iterations:      iterations,
			MedianMS:        median,
			MinMS:           min,
			Checksum:        checksum,
			GoMaxProcs:      gomaxprocs,
			ParallelEnabled: gomaxprocs > 1,
		})
	}
	return results
}

func makeWorkspaceDocuments(ls htmlservice.LanguageService, fileCount, sectionsPerFile int) []workspaceDocument {
	documents := make([]workspaceDocument, 0, fileCount)
	for i := 0; i < fileCount; i++ {
		text := makeHTML(sectionsPerFile + i%3)
		doc := htmlservice.NewTextDocument(htmlservice.DocumentUri(fmt.Sprintf("file:///workspace/file-%02d.html", i)), "html", 0, text)
		documents = append(documents, workspaceDocument{
			text:               text,
			doc:                doc,
			htmlDoc:            ls.ParseHTMLDocument(doc),
			selectionPositions: selectionPositionsForSections(doc, text),
		})
	}
	return documents
}

func sectionRenamePositions(doc *htmlservice.TextDocument, text string) []htmlservice.Position {
	var positions []htmlservice.Position
	for offset := 0; offset < len(text); {
		index := strings.Index(text[offset:], "<section")
		if index < 0 {
			break
		}
		tagOffset := offset + index + len("<")
		positions = append(positions, doc.PositionAt(tagOffset))
		offset += index + len("<section")
	}
	return positions
}

func selectionPositionsForSections(doc *htmlservice.TextDocument, text string) []htmlservice.Position {
	var positions []htmlservice.Position
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

func hoverContentLength(contents any) int {
	switch value := contents.(type) {
	case string:
		return len(value)
	case htmlservice.MarkupContent:
		return len(value.Kind) + len(value.Value)
	default:
		return len(fmt.Sprint(value))
	}
}

func measure(samples, iterations int, fn func() int) (float64, float64, int) {
	if samples <= 0 {
		samples = 1
	}
	warmupIterations := iterations
	if warmupIterations < 10 {
		warmupIterations = 10
	}
	for i := 0; i < warmupIterations; i++ {
		_ = fn()
	}
	values := make([]float64, 0, samples)
	checksum := 0
	for sample := 0; sample < samples; sample++ {
		start := time.Now()
		local := 0
		for i := 0; i < iterations; i++ {
			local += fn()
		}
		elapsed := float64(time.Since(start).Nanoseconds()) / 1e6
		values = append(values, elapsed/float64(iterations))
		checksum += local
	}
	sort.Float64s(values)
	return values[len(values)/2], values[0], checksum
}

func iterationsFor(operation string, size int) int {
	switch operation {
	case "workspaceParse", "workspaceSymbols2", "workspaceFolding", "workspaceLinks", "workspaceSelectionRanges":
		return 3
	case "hover":
		return 10000
	case "rename":
		return 20
	case "format-wrapper":
		return 1
	case "parse", "selectionRanges":
		if size > 150000 {
			return 20
		}
		return 50
	case "selectionRangesBulk":
		if size > 150000 {
			return 3
		}
		if size > 50000 {
			return 5
		}
		return 10
	case "completion":
		return 200
	default:
		if size > 500000 {
			return 3
		}
		if size > 150000 {
			return 5
		}
		return 10
	}
}

func parallelMapOrdered[T any, R any](items []T, minItems int, fn func(int, T) R) []R {
	results := make([]R, len(items))
	if len(items) == 0 {
		return results
	}
	if len(items) < minItems || runtime.GOMAXPROCS(0) < 2 {
		for i, item := range items {
			results[i] = fn(i, item)
		}
		return results
	}
	workers := runtime.GOMAXPROCS(0)
	if workers > len(items) {
		workers = len(items)
	}
	jobs := make(chan int, workers)
	done := make(chan struct{}, workers)
	for worker := 0; worker < workers; worker++ {
		go func() {
			for i := range jobs {
				results[i] = fn(i, items[i])
			}
			done <- struct{}{}
		}()
	}
	for i := range items {
		jobs <- i
	}
	close(jobs)
	for worker := 0; worker < workers; worker++ {
		<-done
	}
	return results
}

func sumInts(values []int) int {
	total := 0
	for _, value := range values {
		total += value
	}
	return total
}

func makeHTML(count int) string {
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
