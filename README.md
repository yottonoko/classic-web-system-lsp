# Classic ASP LSP

A lightweight Visual Studio Code extension for **Classic ASP** (`.asp`, `.asa`, `.inc`) and **VBScript / VBA** (`.vbs`, `.vba`, `.bas`, `.cls`, `.frm`), powered by Mixin's built-in standalone ASP language server (`mixin lsp --asp-only`).

## Features

All core Classic ASP / VBScript language intelligence is provided by Mixin (`std::lang::vbide` + `mixin lsp --asp-only`):

- **Diagnostics & Linting**: Syntax errors, `Option Explicit` checks, missing `Set`, unused variables, unreachable code, unencoded `<%= Request.* %>` XSS warnings, and SQL concatenation warnings.
- **Completion & Hover**: ASP intrinsic objects (`Request`, `Response`, `Server`, `Session`, `Application`, `Err`, `ASPError`), COM ProgIDs (`ADODB.*`, `Scripting.FileSystemObject`, `MSXML2.*`, `CDO.Message`, `WScript.Shell`, etc.) with inferred member completion, VBScript built-ins, `#include` path completion, and bilingual EN/JA documentation.
- **CodeLens**: Reference counts above procedures, classes, and top-level declarations, plus clickable links above `<!-- #include -->` directives.
- **Inlay Hints**: Inferred variable types with `(local)` / `(global)` scope markers, function/property return types, and parameter names on user procedure calls.
- **Code Actions**: Insert `Option Explicit`, add `Set`, wrap with `Server.HTMLEncode(...)`, remove unused `Dim`, split `Dim x = expr`, remove unsupported VBScript `As` types, fix VBScript `Call` syntax, generate `'''` XML documentation comments, and extract VBScript variables (`refactor.extract`).
- **Navigation & Editing**: Go to definition / declaration (including across `#include` files), references, document highlights, rename, signature help, document symbols, document links, folding ranges, selection ranges, formatting, and on-type auto-indentation.
- **Embedded Web Languages**: Delegates HTML, CSS, and JavaScript regions outside `<% ... %>` to Mixin's built-in Web LSP.

## Requirements

- [Mixin](https://github.com/yottonoko/mixin-lang) (`mixin` on `PATH`, `MIXIN_BIN`, `aspLsp.server.path`, or bundled under `server/mixin` in the VSIX).

## Settings

| Setting                            | Default | Description                                                                           |
| ---------------------------------- | ------- | ------------------------------------------------------------------------------------- |
| `aspLsp.server.path`               | `""`    | Path to the `mixin` executable (empty = bundled `server/mixin` or `mixin` on `PATH`). |
| `aspLsp.server.extraArgs`          | `[]`    | Extra arguments passed to `mixin lsp --asp-only`.                                     |
| `aspLsp.codeLens.references`       | `true`  | Show reference-count CodeLens on declarations.                                        |
| `aspLsp.codeLens.includes`         | `true`  | Show clickable CodeLens on `#include` directives.                                     |
| `aspLsp.inlayHints.variableTypes`  | `true`  | Show inferred variable type hints (`As Type`).                                        |
| `aspLsp.inlayHints.returnTypes`    | `true`  | Show inferred function/property return type hints.                                    |
| `aspLsp.inlayHints.parameterNames` | `true`  | Show parameter name hints on user procedure calls.                                    |
| `aspLsp.inlayHints.scopeMarkers`   | `true`  | Show `(local)` / `(global)` scope markers in variable type hints.                     |
| `aspLsp.onTypeFormatting`          | `true`  | Auto-indent on Enter and `%>` inside VBScript regions.                                |
| `aspLsp.trace.server`              | `"off"` | LSP communication trace level.                                                        |

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
