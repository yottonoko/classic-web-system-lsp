# vscode-html-languageservice-go

This repository was created from a clone of Microsoft's `vscode-html-languageservice` and is its Go-language edition. We are deeply grateful to the VS Code team and all contributors to the original project for the design, behavior, tests, and web platform data that made this port possible.

`vscode-html-languageservice-go` provides the HTML language service as a Go module. The TypeScript and JavaScript implementation has been replaced by Go code while keeping the public language-service concepts, LSP-shaped data types, default HTML data, and baseline test behavior aligned with the original package.

## Install

```sh
go get github.com/yottonoko/vscode-html-languageservice-go
```

## Use

Import the root package as `htmlservice`, create a `TextDocument`, parse it, and call the language-service methods directly.

```go
package main

import htmlservice "github.com/yottonoko/vscode-html-languageservice-go"

func main() {
	ls := htmlservice.GetLanguageService()
	doc := htmlservice.NewTextDocument("file:///index.html", "html", 0, "<div></div>")
	htmlDoc := ls.ParseHTMLDocument(doc)
	_ = ls.FindDocumentSymbols2(doc, htmlDoc)
}
```

## API Surface

The root package exposes the language-service entry points in Go form:

- `GetLanguageService`
- `NewHTMLDataProvider`
- `GetDefaultHTMLDataProvider`
- `LanguageService`
- LSP-shaped types such as `Position`, `Range`, `TextEdit`, `CompletionList`, `Hover`, `DocumentLink`, `FoldingRange`, `SelectionRange`, `WorkspaceEdit`, and `TextDocument`

`LanguageService` provides scanner creation, parsing, completion, hover, formatting, document links, symbols, folding ranges, selection ranges, rename, matching tag lookup, linked editing ranges, document highlights, quote completion, and tag completion.

The Go API keeps idiomatic exported names while preserving the original service shape. For example, `doComplete` is exposed as `DoComplete`, `parseHTMLDocument` as `ParseHTMLDocument`, and `getFoldingRanges` as `GetFoldingRanges`.

## Custom Data

Custom HTML data providers can be passed when constructing a service:

```go
provider := htmlservice.NewHTMLDataProvider("custom", htmlservice.HTMLDataV1{
	Version: 1,
	Tags: []htmlservice.TagData{{
		Name: "x-card",
		Attributes: []htmlservice.AttributeData{{
			Name: "tone",
			Values: []htmlservice.ValueData{{Name: "soft"}},
		}},
	}},
})

ls := htmlservice.GetLanguageService(htmlservice.LanguageServiceOptions{
	CustomDataProviders: []htmlservice.HTMLDataProvider{provider},
})
```

## Path Completion

Path completion uses `DocumentContext` to resolve references and `FileSystemProvider` with an optional `ReadDirectory` method to read directory entries:

```go
type files struct{}

func (files) Stat(uri htmlservice.DocumentUri) (htmlservice.FileStat, error) {
	return htmlservice.FileStat{Type: htmlservice.FileTypeDirectory}, nil
}

func (files) ReadDirectory(uri htmlservice.DocumentUri) ([][2]any, error) {
	return [][2]any{{"index.html", htmlservice.FileTypeFile}}, nil
}
```

Pass the provider through `LanguageServiceOptions` and call `DoComplete2` with a document context when path suggestions are needed.

## Formatter

The default formatter uses [`github.com/yottonoko/js-beautify-go`](https://github.com/yottonoko/js-beautify-go) as the Go implementation of js-beautify's HTML formatter. The language service keeps handling VS Code range expansion, indentation restoration, and LSP text edits around that backend. Embedded CSS is formatted through the backend, while embedded JavaScript is disabled in the adapter to match the fork source's no-op JavaScript formatter behavior.

`LanguageServiceOptions.HTMLBeautifier` can still override the backend for tests or specialized integrations:

```go
type htmlBeautifier struct{}

func (htmlBeautifier) BeautifyHTML(source string, options htmlservice.BeautifyHTMLOptions) (string, error) {
	// Call a custom formatter backend here.
	return source, nil
}

ls := htmlservice.GetLanguageService(htmlservice.LanguageServiceOptions{
	HTMLBeautifier: htmlBeautifier{},
})
```

## Included Data

The default HTML custom data is embedded from `data/webCustomData.json`. The schema and custom data documentation are kept under `docs/`.

## Development

```sh
go test ./...
```

The test suite includes direct Go feature tests, baseline parity tests, public API contract tests, and documentation coverage for exported Go types. Baseline TypeScript test coverage is tracked by `baseline_coverage_manifest_test.go`, which verifies 516 active helper/assertion-call markers from baseline commit `82b56fd`. Completion, path completion, and custom-provider assertion items are tracked by `baseline_assertion_manifest_test.go`, and `baseline_completion_assertions_test.go` executes the original completion expectations against the Go service.

This module is intended to be used as a Go library. It does not require Node.js, npm, or a TypeScript runtime.
