package cssls

import (
	"context"
	"fmt"
	"reflect"
	"runtime"
	"strings"
	"sync"
	"testing"

	"github.com/yottonoko/vscode-css-languageservice-go/lsp"
)

type publicAPIMultiFileResult struct {
	Diagnostics      []string
	CompletionLabels []string
	HighlightRanges  []lsp.Range
	LinkTargets      []string
	SymbolNames      []string
	DocumentSymbols  []string
	ColorRanges      []lsp.Range
	Errors           []string
}

func TestLanguageServicePublicAPIsSupportConcurrentDocuments(t *testing.T) {
	languages := []struct {
		id      string
		service LanguageService
	}{
		{id: "css", service: GetCSSLanguageService()},
		{id: "less", service: GetLESSLanguageService()},
		{id: "scss", service: GetSCSSLanguageService()},
	}

	for _, language := range languages {
		t.Run(language.id, func(t *testing.T) {
			language.service.Configure(LanguageSettings{
				ImportAliases: AliasSettings{"@assets/": "/assets/"},
				Lint:          LintSettings{"idSelector": "warning"},
			})
			documents := multiFilePublicAPIDocuments(language.id, 16)
			sequential := collectPublicAPIMultiFileSequential(language.service, documents)
			parallel := collectPublicAPIMultiFileParallel(language.service, documents)
			if !reflect.DeepEqual(parallel, sequential) {
				t.Fatalf("parallel public API results differ\nparallel: %#v\nsequential: %#v", parallel, sequential)
			}
		})
	}
}

func collectPublicAPIMultiFileSequential(service LanguageService, documents []*lsp.TextDocument) []publicAPIMultiFileResult {
	results := make([]publicAPIMultiFileResult, len(documents))
	for i, document := range documents {
		results[i] = collectPublicAPIMultiFileResult(service, document)
	}
	return results
}

func collectPublicAPIMultiFileParallel(service LanguageService, documents []*lsp.TextDocument) []publicAPIMultiFileResult {
	if len(documents) == 0 {
		return nil
	}
	workers := runtime.GOMAXPROCS(0)
	if workers > len(documents) {
		workers = len(documents)
	}
	if workers < 1 {
		workers = 1
	}
	results := make([]publicAPIMultiFileResult, len(documents))
	jobs := make(chan int)
	var wg sync.WaitGroup
	for worker := 0; worker < workers; worker++ {
		wg.Add(1)
		go func() {
			defer wg.Done()
			for index := range jobs {
				results[index] = collectPublicAPIMultiFileResult(service, documents[index])
			}
		}()
	}
	for index := range documents {
		jobs <- index
	}
	close(jobs)
	wg.Wait()
	return results
}

func collectPublicAPIMultiFileResult(service LanguageService, document *lsp.TextDocument) publicAPIMultiFileResult {
	stylesheet := service.ParseStylesheet(document)
	text := document.Text()
	completionPosition := document.PositionAt(strings.Index(text, "displai") + len("dis"))
	highlightPosition := document.PositionAt(strings.Index(text, "color"))

	var result publicAPIMultiFileResult
	for _, diagnostic := range service.DoValidation(document, stylesheet, nil) {
		code, _ := diagnostic.Code.(string)
		result.Diagnostics = append(result.Diagnostics, fmt.Sprintf("%s:%d:%d-%d:%d",
			code,
			diagnostic.Range.Start.Line,
			diagnostic.Range.Start.Character,
			diagnostic.Range.End.Line,
			diagnostic.Range.End.Character,
		))
	}
	completion, err := service.DoComplete(context.Background(), document, completionPosition, stylesheet, nil)
	if err != nil {
		result.Errors = append(result.Errors, err.Error())
	} else {
		for _, item := range completion.Items {
			result.CompletionLabels = append(result.CompletionLabels, item.Label)
		}
	}
	for _, highlight := range service.FindDocumentHighlights(document, highlightPosition, stylesheet) {
		result.HighlightRanges = append(result.HighlightRanges, highlight.Range)
	}
	links, err := service.FindDocumentLinks(context.Background(), document, stylesheet, testDocumentContext{})
	if err != nil {
		result.Errors = append(result.Errors, err.Error())
	} else {
		for _, link := range links {
			result.LinkTargets = append(result.LinkTargets, string(link.Target))
		}
	}
	for _, symbol := range service.FindDocumentSymbols(document, stylesheet) {
		result.SymbolNames = append(result.SymbolNames, symbol.Name)
	}
	for _, symbol := range service.FindDocumentSymbols2(document, stylesheet) {
		result.DocumentSymbols = append(result.DocumentSymbols, symbol.Name)
	}
	for _, color := range service.FindDocumentColors(document, stylesheet) {
		result.ColorRanges = append(result.ColorRanges, color.Range)
	}
	return result
}

func multiFilePublicAPIDocuments(languageID string, count int) []*lsp.TextDocument {
	documents := make([]*lsp.TextDocument, count)
	for i := 0; i < count; i++ {
		documents[i] = lsp.NewTextDocument(
			lsp.DocumentURI(fmt.Sprintf("test://test/%s/file-%02d.%s", languageID, i, languageID)),
			languageID,
			i,
			multiFilePublicAPIText(languageID, i),
		)
	}
	return documents
}

func multiFilePublicAPIText(languageID string, index int) string {
	var b strings.Builder
	fmt.Fprintf(&b, `@import "@assets/base-%d.css";`+"\n", index)
	if languageID == "less" {
		fmt.Fprintf(&b, "@accent-%d: #%02x%02x%02x;\n", index, index, index*2, index*3)
	}
	if languageID == "scss" {
		fmt.Fprintf(&b, "$accent-%d: #%02x%02x%02x;\n", index, index, index*2, index*3)
		b.WriteString("@mixin tone($color) { color: $color; }\n")
	}
	fmt.Fprintf(&b, ".item-%d, #id-%d {\n", index, index)
	fmt.Fprintf(&b, "  color: #%02x%02x%02x;\n", index+1, index+2, index+3)
	b.WriteString("  background: rgba(10, 20, 30, 0.5);\n")
	if languageID == "scss" {
		b.WriteString("  @include tone(red);\n")
	}
	b.WriteString("  displai: flex;\n")
	b.WriteString("}\n")
	fmt.Fprintf(&b, ".target-%d { color: red; }\n", index)
	return b.String()
}
