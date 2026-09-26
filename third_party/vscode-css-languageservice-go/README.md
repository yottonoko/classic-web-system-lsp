# vscode-css-languageservice-go

This project is a Go-language clone of Microsoft's
[`vscode-css-languageservice`](https://github.com/microsoft/vscode-css-languageservice).
It exists thanks to the excellent work of the VS Code team and all contributors
to the original TypeScript implementation; this repository is maintained with
deep respect and gratitude for that foundation.

The module provides CSS, LESS, and SCSS parser and language-service APIs for
Go programs. It exposes validation, completion, hover, document links,
document symbols, folding, selection ranges, formatting, document colors,
highlights, rename, definitions, references, code actions, and parser
diagnostics.

## Usage

```go
package main

import (
	"context"

	cssls "github.com/yottonoko/vscode-css-languageservice-go"
	"github.com/yottonoko/vscode-css-languageservice-go/lsp"
)

func main() {
	service := cssls.GetCSSLanguageService()
	document := lsp.NewTextDocument("file:///style.css", "css", 0, "body { color: red; }")
	stylesheet := service.ParseStylesheet(document)

	_ = service.DoValidation(document, stylesheet, nil)
	_, _ = service.DoComplete(context.Background(), document, lsp.Position{Line: 0, Character: 8}, stylesheet, nil)
}
```

Use `GetCSSLanguageService`, `GetLESSLanguageService`, or
`GetSCSSLanguageService` for the document language you need. The public API
keeps the same behavior-oriented surface as the upstream language service while
using idiomatic Go names and LSP data structures from this module's `lsp`
package.

Language-service read APIs can be called concurrently for different
`TextDocument` values after configuration is complete. Configure the service and
data providers before starting workers, then fan out per-document requests with
goroutines and merge results in the caller's desired order. If you provide a
custom `FileSystemProvider`, make that provider safe for concurrent calls too.

## Development

Run the Go test suite:

```sh
go test ./...
```

The porting audit in [docs/porting-audit.md](docs/porting-audit.md) records the
TypeScript test and public API migration check that was run before removing the
old TypeScript/JavaScript implementation from this repository. The current
source tree is Go-first: implementation code lives in `service.go`, `parser/`,
`languagefacts/`, `services/`, `lsp/`, and `utils/`.

## License

MIT. See [LICENSE.md](LICENSE.md). Third-party notices are listed in
[THIRD_PARTY_NOTICES.md](THIRD_PARTY_NOTICES.md).
