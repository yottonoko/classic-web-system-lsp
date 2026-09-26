package cssls

import (
	"context"
	"fmt"
	"strings"
	"testing"

	"github.com/yottonoko/vscode-css-languageservice-go/lsp"
)

type benchmarkDocumentContext struct{}

func (benchmarkDocumentContext) ResolveReference(ref, baseURL string) (string, bool) {
	if strings.HasPrefix(ref, "http://") || strings.HasPrefix(ref, "https://") || strings.HasPrefix(ref, "file://") {
		return ref, true
	}
	return "file:///bench/" + strings.TrimPrefix(ref, "./"), true
}

func BenchmarkPublicAPI(b *testing.B) {
	cases := []struct {
		name  string
		rules int
	}{
		{name: "small", rules: 200},
		{name: "medium", rules: 1000},
		{name: "large", rules: 3000},
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
			text := benchmarkCSS(c.rules, language.id)
			document := lsp.NewTextDocument(lsp.DocumentURI("file:///bench/style."+language.id), language.id, 0, text)
			position := benchmarkPositionOf(text, ".target { color: r")
			renamePosition := benchmarkPositionOf(text, "color: #")
			color := lsp.Color{Red: 1, Green: 0, Blue: 0, Alpha: 1}
			colorRange := lsp.Range{Start: lsp.Position{Line: 2, Character: 9}, End: lsp.Position{Line: 2, Character: 16}}
			b.Run(language.id+"/"+c.name+"/parse", func(b *testing.B) {
				for i := 0; i < b.N; i++ {
					_ = language.service.ParseStylesheet(document)
				}
			})
			b.Run(language.id+"/"+c.name+"/validation", func(b *testing.B) {
				for i := 0; i < b.N; i++ {
					stylesheet := language.service.ParseStylesheet(document)
					_ = language.service.DoValidation(document, stylesheet, nil)
				}
			})
			b.Run(language.id+"/"+c.name+"/completion", func(b *testing.B) {
				for i := 0; i < b.N; i++ {
					stylesheet := language.service.ParseStylesheet(document)
					_, _ = language.service.DoComplete(context.Background(), document, position, stylesheet, nil)
				}
			})
			b.Run(language.id+"/"+c.name+"/completion2", func(b *testing.B) {
				for i := 0; i < b.N; i++ {
					stylesheet := language.service.ParseStylesheet(document)
					_, _ = language.service.DoComplete2(context.Background(), document, position, stylesheet, benchmarkDocumentContext{}, nil)
				}
			})
			b.Run(language.id+"/"+c.name+"/hover", func(b *testing.B) {
				for i := 0; i < b.N; i++ {
					stylesheet := language.service.ParseStylesheet(document)
					_ = language.service.DoHover(document, renamePosition, stylesheet, nil)
				}
			})
			b.Run(language.id+"/"+c.name+"/navigation", func(b *testing.B) {
				for i := 0; i < b.N; i++ {
					stylesheet := language.service.ParseStylesheet(document)
					_ = language.service.FindDefinition(document, renamePosition, stylesheet)
					_ = language.service.FindReferences(document, renamePosition, stylesheet)
					_ = language.service.FindDocumentHighlights(document, renamePosition, stylesheet)
				}
			})
			b.Run(language.id+"/"+c.name+"/links", func(b *testing.B) {
				for i := 0; i < b.N; i++ {
					stylesheet := language.service.ParseStylesheet(document)
					_, _ = language.service.FindDocumentLinks(context.Background(), document, stylesheet, benchmarkDocumentContext{})
					_, _ = language.service.FindDocumentLinks2(context.Background(), document, stylesheet, benchmarkDocumentContext{})
				}
			})
			b.Run(language.id+"/"+c.name+"/symbols", func(b *testing.B) {
				for i := 0; i < b.N; i++ {
					stylesheet := language.service.ParseStylesheet(document)
					_ = language.service.FindDocumentSymbols(document, stylesheet)
					_ = language.service.FindDocumentSymbols2(document, stylesheet)
				}
			})
			b.Run(language.id+"/"+c.name+"/colors", func(b *testing.B) {
				for i := 0; i < b.N; i++ {
					stylesheet := language.service.ParseStylesheet(document)
					colors := language.service.FindDocumentColors(document, stylesheet)
					if len(colors) > 0 {
						_ = language.service.GetColorPresentations(document, stylesheet, colors[0].Color, colors[0].Range)
					} else {
						_ = language.service.GetColorPresentations(document, stylesheet, color, colorRange)
					}
				}
			})
			b.Run(language.id+"/"+c.name+"/rename", func(b *testing.B) {
				for i := 0; i < b.N; i++ {
					stylesheet := language.service.ParseStylesheet(document)
					_ = language.service.PrepareRename(document, renamePosition, stylesheet)
					_ = language.service.DoRename(document, renamePosition, "visibility", stylesheet)
				}
			})
			b.Run(language.id+"/"+c.name+"/codeActions", func(b *testing.B) {
				for i := 0; i < b.N; i++ {
					stylesheet := language.service.ParseStylesheet(document)
					diagnostics := language.service.DoValidation(document, stylesheet, nil)
					_ = language.service.DoCodeActions(document, lsp.Range{}, lsp.CodeActionContext{Diagnostics: diagnostics}, stylesheet)
					_ = language.service.DoCodeActions2(document, lsp.Range{}, lsp.CodeActionContext{Diagnostics: diagnostics}, stylesheet)
				}
			})
			b.Run(language.id+"/"+c.name+"/foldingSelection", func(b *testing.B) {
				for i := 0; i < b.N; i++ {
					stylesheet := language.service.ParseStylesheet(document)
					_ = language.service.GetFoldingRanges(document, nil)
					_ = language.service.GetSelectionRanges(document, []lsp.Position{renamePosition, position}, stylesheet)
				}
			})
			b.Run(language.id+"/"+c.name+"/format", func(b *testing.B) {
				for i := 0; i < b.N; i++ {
					_ = language.service.Format(document, nil, CSSFormatConfiguration{TabSize: 2, InsertSpaces: true})
				}
			})
		}
	}
}

func benchmarkCSS(rules int, languageID string) string {
	var b strings.Builder
	if languageID == "less" {
		b.WriteString("@accent: #3366ff;\n")
	} else {
		b.WriteString(":root { --accent: #3366ff; --gap: 12px; }\n")
	}
	b.WriteString(`@import "./base.css";` + "\n")
	if languageID == "scss" {
		b.WriteString("$accent: #3366ff;\n@mixin rounded($radius) { border-radius: $radius; }\n")
	}
	b.WriteString("@media screen and (min-width: 640px) {\n")
	for i := 0; i < rules; i++ {
		fmt.Fprintf(&b, ".card-%d, .panel-%d:hover {\n", i, i)
		fmt.Fprintf(&b, "  color: #%02x%02x%02x;\n", i%256, (i*3)%256, (i*7)%256)
		fmt.Fprintf(&b, "  background: rgba(%d, %d, %d, 0.%d);\n", i%255, (i*5)%255, (i*11)%255, i%10)
		fmt.Fprintf(&b, "  margin: calc(var(--gap) + %dpx);\n", i%17)
		fmt.Fprintf(&b, "  border: %dpx solid currentColor;\n", i%5+1)
		fmt.Fprintf(&b, "  transform: translateX(%dpx) scale(1.%d);\n", i%31, i%9)
		if languageID == "scss" && i%100 == 0 {
			b.WriteString("  @include rounded(4px);\n")
		}
		b.WriteString("}\n")
		if i%50 == 0 {
			fmt.Fprintf(&b, "@keyframes fade-%d { from { opacity: 0; } to { opacity: 1; } }\n", i)
		}
	}
	b.WriteString("}\n")
	b.WriteString(".target { color: r; displai: flex; }\n")
	return b.String()
}

func benchmarkPositionOf(text, marker string) lsp.Position {
	offset := strings.Index(text, marker)
	if offset == -1 {
		return lsp.Position{}
	}
	line := 0
	lineStart := 0
	for i := 0; i < offset; i++ {
		if text[i] == '\n' {
			line++
			lineStart = i + 1
		}
	}
	return lsp.Position{Line: line, Character: offset + len(marker) - lineStart}
}
