# Repository Instructions

This repository contains a Classic ASP language server and a VS Code extension.

## Project Layout

- `cmd/asp-lsp-go`: Go stdio language server entrypoint.
- `internal/core`: Classic ASP CST/parser, embedded region scanner, virtual documents, source maps, formatter, and shared types.
- `internal/lspserver`: LSP diagnostics, completion, hover, document links, folding, formatting routing, and workspace features.
- `internal/vbscript`, `internal/embedded`, `internal/workspace`, `internal/graph`, `internal/excel`: Go feature packages behind the server.
- `apps/vscode`: VS Code extension client, language registration, grammar, and extension package tests.

## Language And Style

- Use Go for the language server and core implementation. Use TypeScript only for the VS Code extension and webviews.
- Use English identifiers, public API names, source comments, and technical docs unless a user explicitly requests Japanese.
- Keep comments sparse. Add comments only for non-obvious mapping, protocol, parsing, or compatibility behavior.
- Keep Classic ASP handling conservative. Prefer fewer diagnostics over noisy false positives.

## Commands

Use `pnpm`, not `npm`.

```sh
pnpm install
pnpm run typecheck
pnpm run lint
pnpm run format:check
pnpm run test
pnpm run build
pnpm run package:vsix
go test ./...
go build ./cmd/asp-lsp-go
```

The standalone server is built at `bin/asp-lsp-go` and runs with:

```sh
bin/asp-lsp-go --stdio
```

Build release binaries with:

```sh
go build -trimpath -buildvcs=false -ldflags="-s -w -buildid=" -o <output> <package>
```

For cross-compilation, set `CGO_ENABLED=0`, `GOOS`, and `GOARCH` before the command.

## Implementation Rules

- Preserve UTF-16 offsets and LSP ranges when adding parser or source-map behavior.
- Keep Classic ASP and VBScript LSP features based on the CST where practical; do not add new regex-only symbol extraction.
- Route embedded HTML, CSS, and JavaScript through the Go ports of their language services when practical.
- Route new user-facing diagnostics, code action titles, CodeLens titles, completion fallback docs, and extension messages through the localizer/NLS keys. Keep upstream TypeScript/HTML/CSS service messages unchanged.
- Treat `.inc` files as fragments. Do not assume a complete HTML document.
- Do not let formatting edits erase or rewrite Classic ASP server regions.
- Keep `apps/vscode` able to resolve the development server binary via `bin/asp-lsp-go` and the VSIX-bundled server via `server/asp-lsp-go`.

## Git

- Do not stage unrelated files.
- Commit messages in this repository may be English.
- Every commit message must end with exactly one `Co-authored-by` trailer for the agent that creates the commit.

```text
Co-authored-by: <Agent Name> <agent@example.com>
```
