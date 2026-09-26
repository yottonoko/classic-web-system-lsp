# Go Port Dependencies

This directory contains the Go ports used by the Classic ASP Go language server.
Their source, tests, and license files are tracked as regular files in this
repository. Cloning or downloading a source archive includes these dependencies;
access to their original repositories and Git submodule initialization are not
required.

| Module identity | Local source | Imported revision |
| --- | --- | --- |
| `github.com/yottonoko/js-beautify-go` | [`js-beautify-go/`](js-beautify-go/) | `2828794ee1b56f54aa38d5424733ecd61113e3fc` |
| `github.com/yottonoko/vscode-css-languageservice-go` | [`vscode-css-languageservice-go/`](vscode-css-languageservice-go/) | `d101fdcdd3af1f05307a345b6c6c773810150f37` |
| `github.com/yottonoko/vscode-html-languageservice-go` | [`vscode-html-languageservice-go/`](vscode-html-languageservice-go/) | `38e583430f641b812be0fbf7e21ecabaae651303` |

The revisions above record the original imports. Changes made after import are
maintained directly in this repository. Module names remain unchanged for import
compatibility. The root `go.mod` replaces all three modules with these local
directories, and the CSS and HTML modules replace their formatter dependency
with `../js-beautify-go` so they can also be tested independently.

The public TypeScript Go implementation is included as regular files in
`typescript-go/`, with its local ASP adapter, `LICENSE`, and `NOTICE.txt`. The
root module also selects this copy through a local `replace` directive.

## Build and test

From the repository root, build and test the server with:

```sh
go test ./...
go build -trimpath -buildvcs=false -ldflags="-s -w -buildid=" -o bin/asp-lsp-go ./cmd/asp-lsp-go
```

Go's `./...` pattern does not enter nested modules. Run their suites explicitly:

```sh
go -C third_party/js-beautify-go test ./...
go -C third_party/vscode-css-languageservice-go test ./...
go -C third_party/vscode-html-languageservice-go test ./...
go -C third_party/typescript-go test ./aspadapter/...
```

The server still downloads its public Go module dependencies on a fresh machine.
No credentials or `GOPRIVATE` configuration are needed for the bundled ports.
Preserve each directory's license and notice files when redistributing source
or binaries. VSIX packaging copies the imported modules' license and notice
files into `third_party_licenses/`.
