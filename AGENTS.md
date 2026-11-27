# Repository Instructions

This repository contains the **Classic ASP LSP** VS Code extension (`v0.12.0+`), powered by the standalone Mixin-built Classic ASP / VBScript language server CLI (`classic-asp-lsp`, built from `yottonoko/mixin-lang`: `tools/asp_lsp.mxz`).

## Architecture & Ownership Boundary

- **Core Classic ASP / VBScript LSP engine** lives in the Mixin standard library (`yottonoko/mixin-lang`: `std/lang/vbide.mxz`, `std/lang/lsp/auxcache.mxz`, `std/lang/lsp/vbdoc.mxz`, `std/lang/webide/lsp.mxz`, and standalone CLI `tools/asp_lsp.mxz`).
- The full `mixin` compiler is **not** bundled here. Instead, this extension ships/launches only the standalone `classic-asp-lsp` CLI in two distribution forms:
  - **Windows native binary**: `server/classic-asp-lsp.exe` (`x86_64-windows-gnu`)
  - **WASM (WASI, Any OS)**: `server/classic-asp-lsp.wasm` (`wasm32-wasi`), executed via the bundled zero-dependency Node.js WASI runner (`dist/wasm-runner.js`)
- General Classic ASP / VBScript features (diagnostics, completion, hover, signature help, go-to-definition, references, rename, CodeLens, inlay hints, code actions, document links, folding, document highlights, selection ranges, formatting, on-type formatting, network-drive / antivirus I/O reduction, persistent `std::db` cache) belong in **Mixin** (`mixin-lang`), not here.
- **This repository** is a pure VS Code extension that:
  - Registers Classic ASP (`.asp`, `.asa`, `.inc`) and VBScript / VBA (`.vbs`, `.vba`, `.bas`, `.cls`, `.frm`) languages, language configurations, and TextMate grammars.
  - Launches the standalone `classic-asp-lsp` server (native binary or `.wasm` via `dist/wasm-runner.js`) over stdio, watches ASP/VB files for event-driven cache invalidation, and synchronizes `aspLsp.*` settings (mirroring `editors/vscode/src/aspConfig.ts` in `mixin-lang`).
  - Hosts any specialized Classic ASP-only VS Code integration features if needed in the future.

## Cache Storage Policy

- **Never** write Classic ASP LSP caches to the user's `~/.mixinlang` (Mixin home) directory.
- All persistent caches (such as `asp-lsp-cache/` `.mdb` files and `ws_idx_*.dat` workspace index caches) **must** be stored under VS Code's managed extension storage (`context.globalStorageUri.fsPath`).
- `apps/vscode/src/extension.ts` passes `MIXIN_HOME: context.globalStorageUri.fsPath` to both native and WASM server processes, and `apps/vscode/src/wasmRunner.ts` preopens `MIXIN_HOME` for WASI so all cache I/O stays inside VS Code's extension storage.

## Project Layout

- `apps/vscode/src`: VS Code extension entrypoint (`extension.ts`), standalone server resolver (`serverPath.ts`), Node.js WASI runner (`wasmRunner.ts`), and shared ASP LSP settings (`aspConfig.ts`).
- `apps/vscode/syntaxes`: TextMate grammars for Classic ASP and VBScript.
- `apps/vscode/assets`: Extension icons.
- `apps/vscode/test`: Vitest unit tests for the extension manifest, localization, and native/WASM server resolution.
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

## Server Resolution

`apps/vscode/src/serverPath.ts` resolves the standalone `classic-asp-lsp` server in this order:

1. `aspLsp.server.path` setting (native binary or `.wasm` file)
2. VSIX-bundled native binary: `server/classic-asp-lsp.exe` (Windows) / `server/classic-asp-lsp` (POSIX)
3. VSIX-bundled WASM (Any OS): `server/classic-asp-lsp.wasm` (launched via `dist/wasm-runner.js`)
4. `CLASSIC_ASP_LSP_BIN` environment variable (native binary or `.wasm` file)
5. `classic-asp-lsp` on `PATH`

## Git

- Do not stage unrelated files.
- Commit messages should be in English.
- Every commit message must end with exactly one `Co-authored-by` trailer for the agent that creates the commit.

```text
Co-authored-by: <Agent Name> <agent@example.com>
```
