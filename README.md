# Classic ASP LSP

A lightweight Visual Studio Code extension for **Classic ASP** (`.asp`, `.asa`, `.inc`) and **VBScript / VBA** (`.vbs`, `.vba`, `.bas`, `.cls`, `.frm`), powered by the standalone `classic-asp-lsp` server built from [Mixin](https://github.com/yottonoko/mixin-lang) (`tools/asp_lsp.mxz`).

The full Mixin compiler is not required or bundled—only the standalone ASP LSP server is shipped:

- **Windows native**: `server/classic-asp-lsp.exe` (`x86_64-windows-gnu`)
- **Any OS (WASM / WASI)**: `server/classic-asp-lsp.wasm` (`wasm32-wasi`), run via the built-in zero-dependency `node:wasi` runner (`dist/wasm-runner.js`)

## Features

- **Diagnostics & Linting**: Syntax errors, `Option Explicit` checks, missing `Set`, unused variables, unreachable code, unencoded `<%= Request.* %>` XSS warnings, and SQL concatenation warnings.
- **Completion & Hover**: ASP intrinsic objects (`Request`, `Response`, `Server`, `Session`, `Application`, `Err`, `ASPError`), COM ProgIDs (`ADODB.*`, `Scripting.FileSystemObject`, `MSXML2.*`, `CDO.Message`, `WScript.Shell`, etc.) with inferred member completion, VBScript built-ins, `#include` path completion, and bilingual EN/JA documentation.
- **CodeLens**: Reference counts above procedures, classes, and top-level declarations, plus clickable links above `<!-- #include -->` directives.
- **Inlay Hints**: Inferred variable types with `(local)` / `(global)` scope markers, function/property return types, and parameter names on user procedure calls.
- **Code Actions**: Insert `Option Explicit`, add `Set`, wrap with `Server.HTMLEncode(...)`, remove unused `Dim`, split `Dim x = expr`, remove unsupported VBScript `As` types, fix VBScript `Call` syntax, generate `'''` XML documentation comments, and extract VBScript variables (`refactor.extract`).
- **Navigation & Editing**: Go to definition / declaration (including across `#include` files), references, document highlights, rename, signature help, document symbols, document links, folding ranges, selection ranges, formatting, and on-type auto-indentation.
- **Network-Drive & Antivirus I/O Optimization**: Automatic UNC/network-share detection with event-driven revalidation (`didChangeWatchedFiles` / `didSave`), local `stat()` TTL, negative caching for missing `#include` targets, open-buffer priority, and persistent local DB caching (`globalStorage/yottonoko.classic-asp-lsp/asp-lsp-cache/`).
- **Embedded Web Languages**: Delegates HTML, CSS, and JavaScript regions outside `<% ... %>` to Mixin's built-in Web LSP.

## Settings

| Setting                               | Default  | Description                                                                                            |
| ------------------------------------- | -------- | ------------------------------------------------------------------------------------------------------ |
| `aspLsp.server.path`                  | `""`     | Path to standalone `classic-asp-lsp` binary or `.wasm` module (empty = bundled native/WASM or `PATH`). |
| `aspLsp.server.extraArgs`             | `[]`     | Extra arguments passed to `classic-asp-lsp`.                                                           |
| `aspLsp.codeLens.references`          | `true`   | Show reference-count CodeLens on declarations.                                                         |
| `aspLsp.codeLens.includes`            | `true`   | Show clickable CodeLens on `#include` directives.                                                      |
| `aspLsp.codeLens.crossFileReferences` | `true`   | Include references from other workspace files in CodeLens and reference lists.                         |
| `aspLsp.inlayHints.variableTypes`     | `true`   | Show inferred variable type hints (`As Type`).                                                         |
| `aspLsp.inlayHints.returnTypes`       | `true`   | Show inferred function/property return type hints.                                                     |
| `aspLsp.inlayHints.parameterNames`    | `true`   | Show parameter name hints on user procedure calls.                                                     |
| `aspLsp.inlayHints.scopeMarkers`      | `true`   | Show `(local)` / `(global)` scope markers in variable type hints.                                      |
| `aspLsp.onTypeFormatting`             | `true`   | Auto-indent on Enter and `%>` inside VBScript regions.                                                 |
| `aspLsp.io.networkMode`               | `"auto"` | Network-drive / antivirus I/O mode (`auto` \| `always` \| `never`).                                    |
| `aspLsp.io.statTtlMs`                 | `5000`   | Local `stat()` cache TTL in milliseconds.                                                              |
| `aspLsp.io.diskCache`                 | `true`   | Persist `#include` file cache in a local DB under VS Code's extension global storage.                  |
| `aspLsp.trace.server`                 | `"off"`  | LSP communication trace level.                                                                         |

## Development

```sh
pnpm install
pnpm run typecheck
pnpm run lint
pnpm run format:check
pnpm run test
pnpm run build
pnpm run package:vsix
```

## License

Dual-licensed under `MIT OR Apache-2.0`.
