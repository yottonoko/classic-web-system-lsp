# Repository Instructions

This repository contains a Classic ASP language server and a VS Code extension.

## Project Layout

- `packages/core`: Classic ASP CST/parser, embedded region scanner, virtual documents, source maps, formatter, and VBScript helpers.
- `packages/language-server`: LSP server entrypoint, diagnostics, completion, hover, document links, folding, and formatting routing.
- `apps/vscode`: VS Code extension client, language registration, grammar, and extension package tests.

## Language And Style

- Use TypeScript for implementation.
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
```

The standalone server is built at `packages/language-server/dist/server.js` and runs with:

```sh
node packages/language-server/dist/server.js --stdio
```

## Implementation Rules

- Preserve UTF-16 offsets and LSP ranges when adding parser or source-map behavior.
- Keep Classic ASP and VBScript LSP features based on the CST where practical; do not add new regex-only symbol extraction.
- Route embedded HTML, CSS, and JavaScript through their language services when practical.
- Route new user-facing diagnostics, code action titles, CodeLens titles, completion fallback docs, and extension messages through the localizer/NLS keys. Keep upstream TypeScript/HTML/CSS service messages unchanged.
- Treat `.inc` files as fragments. Do not assume a complete HTML document.
- Do not let formatting edits erase or rewrite Classic ASP server regions.
- Keep `apps/vscode` able to resolve the development server module via `node_modules/@asp-lsp/language-server/dist/server.js` and the VSIX-bundled server via `server/language-server/dist/server.js`.

## Git

- Do not stage unrelated files.
- Commit messages in this repository may be English.
- Every commit message must end with exactly one `Co-authored-by` trailer for the agent that creates the commit.

```text
Co-authored-by: <Agent Name> <agent@example.com>
```
