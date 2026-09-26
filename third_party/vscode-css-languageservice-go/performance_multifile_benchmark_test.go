package cssls

import (
	"fmt"
	"strings"
	"testing"

	"github.com/yottonoko/vscode-css-languageservice-go/lsp"
)

func BenchmarkPublicAPIMultiFile(b *testing.B) {
	cases := []struct {
		name  string
		files int
		rules int
	}{
		{name: "small", files: 8, rules: 80},
		{name: "medium", files: 12, rules: 240},
		{name: "large", files: 16, rules: 640},
	}
	languages := []struct {
		id      string
		service LanguageService
	}{
		{id: "css", service: GetCSSLanguageService()},
		{id: "less", service: GetLESSLanguageService()},
		{id: "scss", service: GetSCSSLanguageService()},
	}

	for _, language := range languages {
		for _, c := range cases {
			documents := benchmarkMultiFileDocuments(language.id, c.files, c.rules)
			b.Run(language.id+"/"+c.name+"/sequential", func(b *testing.B) {
				for i := 0; i < b.N; i++ {
					_ = collectPublicAPIMultiFileSequential(language.service, documents)
				}
			})
			b.Run(language.id+"/"+c.name+"/parallel", func(b *testing.B) {
				for i := 0; i < b.N; i++ {
					_ = collectPublicAPIMultiFileParallel(language.service, documents)
				}
			})
		}
	}
}

func benchmarkMultiFileDocuments(languageID string, files, rules int) []*lsp.TextDocument {
	documents := make([]*lsp.TextDocument, files)
	for i := 0; i < files; i++ {
		documents[i] = lsp.NewTextDocument(
			lsp.DocumentURI(fmt.Sprintf("file:///bench/%s/multi-%02d.%s", languageID, i, languageID)),
			languageID,
			i,
			benchmarkMultiFileCSS(rules, languageID, i),
		)
	}
	return documents
}

func benchmarkMultiFileCSS(rules int, languageID string, fileIndex int) string {
	text := benchmarkCSS(rules, languageID)
	var b strings.Builder
	fmt.Fprintf(&b, `@import "./shared-%d.css";`+"\n", fileIndex)
	if languageID == "scss" {
		fmt.Fprintf(&b, "$file-index: %d;\n", fileIndex)
	} else if languageID == "less" {
		fmt.Fprintf(&b, "@file-index: %d;\n", fileIndex)
	}
	b.WriteString(text)
	return b.String()
}
