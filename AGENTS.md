# Repository Instructions

This repository contains the **Classic ASP LSP** VS Code extension (`v1.0.0+`), powered by Mixin's built-in Classic ASP / VBScript language server (`mixin lsp --asp-only`).

## Architecture & Ownership Boundary

- **Core Classic ASP / VBScript LSP engine** lives in the Mixin standard library (`yottonoko/mixin-lang`: `std/lang/vbide.mxz`, `std/lang/lsp/vbdoc.mxz`, `std/lang/webide/lsp.mxz`).
- General Classic ASP / VBScript features (diagnostics, completion, hover, signature help, go-to-definition, references, rename, CodeLens, inlay hints, code actions, document links, folding, document highlights, selection ranges, formatting, on-type formatting) belong in **Mixin** (`mixin-lang`), not here.
- **This repository** is a pure VS Code extension that:
  - Registers Classic ASP (`.asp`, `.asa`, `.inc`) and VBScript / VBA (`.vbs`, `.vba`, `.bas`, `.cls`, `.frm`) languages, language configurations, and TextMate grammars.
  - Launches `mixin lsp --asp-only` over stdio and synchronizes `aspLsp.*` settings (mirroring `editors/vscode/src/aspConfig.ts` in `mixin-lang`).
  - Hosts any specialized Classic ASP-only VS Code integration features if needed in the future.

## Project Layout

- `apps/vscode/src`: VS Code extension entrypoint (`extension.ts`), Mixin binary resolver (`serverPath.ts`), and shared ASP LSP settings (`aspConfig.ts`).
- `apps/vscode/syntaxes`: TextMate grammars for Classic ASP and VBScript.
- `apps/vscode/assets`: Extension icons.
- `apps/vscode/test`: Vitest unit tests for the extension manifest, localization, and server path resolution.
- `samples`: Sample Classic ASP / VBScript files for manual testing.

## Language And Style

- Use TypeScript for the VS Code extension.
- Use English identifiers, public API names, source comments, and technical docs unless a user explicitly requests Japanese.
- Keep user-facing manifest strings localized via `package.nls.json` (English) and `package.nls.ja.json` (Japanese).
- Keep comments sparse and focused on non-obvious behavior.

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

## Server Binary Resolution

`apps/vscode/src/serverPath.ts` resolves the `mixin` executable in this order:

1. `aspLsp.server.path` setting
2. VSIX-bundled `server/mixin` (`server/mixin.exe` on Windows)
3. `MIXIN_BIN` environment variable
4. `mixin` on `PATH`

## Git

- Do not stage unrelated files.
- Commit messages should be in English.
- Every commit message must end with exactly one `Co-authored-by` trailer for the agent that creates the commit.

```text
Co-authored-by: <Agent Name> <agent@example.com>
```
