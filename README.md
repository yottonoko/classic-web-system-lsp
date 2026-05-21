# asp-lsp

Classic ASP language server and VS Code extension.

The implementation treats `.asp`, `.asa`, and `.inc` files as mixed documents:

- Classic ASP server script regions: `<% %>`, `<%= %>`, `<%@ %>`, and `<script runat="server">`
- HTML regions delegated to `vscode-html-languageservice`
- CSS regions and `style=""` attributes delegated to `vscode-css-languageservice`
- client JavaScript regions delegated to the TypeScript language service
- Classic ASP and VBScript boundaries parsed into a lossless CST for editor features and formatting
- VBScript server regions handled by the built-in v1 analyzer

## Commands

```sh
pnpm install
pnpm run typecheck
pnpm run lint
pnpm run format:check
pnpm run test
pnpm run build
pnpm run package:vsix
```

The test suite includes JSON-RPC smoke coverage for HTML, CSS, inline style, JavaScript, and ASP/VBScript completions, diagnostics, hover, definition, references, rename, document highlights, signature help, workspace symbols, semantic tokens, code actions, formatting, and virtual include roots.

## VBScript Support

- built-in Classic ASP object hover and member completions
- user-defined variable, constant, function, sub, class, method, field, and property symbols
- scope-aware completions for procedure-local variables and parameters
- `Set value = New ClassName` inference for `value.Member` completions
- `Me.Member` completions inside classes
- definition and references for user-defined VBScript symbols
- include-aware VBScript symbols for completions and definition jumps
- rename, document highlights, signature help, workspace symbols, and semantic tokens for VBScript symbols
- conservative support for `ReDim`, `For Each`, `With`, and `Server.CreateObject("ADODB.*")` completions

## Standalone Server

```sh
pnpm --filter @asp-lsp/language-server run start -- --stdio
```

After building, the server entrypoint is:

```sh
node packages/language-server/dist/server.js --stdio
```

## VS Code Development

Open this repository in VS Code, run `pnpm install` and `pnpm run build`, then start an Extension Development Host from `apps/vscode`.

The extension registers:

- language id: `classic-asp`
- file extensions: `.asp`, `.asa`, `.inc`
- packaged server path: `node_modules/@asp-lsp/language-server/dist/server.js`
- VSIX server path: `server/language-server/dist/server.js`

To build a local VSIX:

```sh
pnpm run build
pnpm run package:vsix --out classic-asp-lsp.vsix
```

The VSIX build copies the standalone language server and its runtime dependencies into `apps/vscode/server/language-server` before packaging.

## Settings

- `aspLsp.defaultLanguage`: default server-side language, `VBScript` or `JScript`
- `aspLsp.checkJs`: enable semantic checks for client JavaScript regions
- `aspLsp.virtualRoot`: root directory for `<!-- #include virtual="..." -->`
- `aspLsp.virtualRoots`: additional virtual include roots
- `aspLsp.legacyEncoding`: encoding for unopened include files, `utf8`, `shift_jis`, or `cp932`
- `aspLsp.format.indentSize`: Classic ASP formatter indent size; unset uses editor options
- `aspLsp.format.indentStyle`: `space` or `tab`; unset uses editor options
- `aspLsp.format.uppercaseKeywords`: format VBScript keywords as uppercase
- `aspLsp.format.alignAssignments`: align simple consecutive VBScript assignments
- `aspLsp.vbscript.typeChecking`: `basic` or `strict`; strict enables VBScript type diagnostics
- `aspLsp.vbscript.comTypes`: custom COM type catalog keyed by `Server.CreateObject` Prog.ID

## Current v1 Limits

- VBScript analysis is intentionally conservative. It uses an error-tolerant CST and opt-in strict type checks rather than a full VBScript compiler.
- `.inc` files are treated as fragments, so full-document HTML diagnostics are suppressed for them.
- Include resolution supports `file` and `virtual` directives, missing include diagnostics, and bounded cycle detection.
- COM and IIS runtime behavior are not executed. COM type information comes from built-in stubs or `aspLsp.vbscript.comTypes`.
- Full-document formatting is CST based and conservative. HTML-only ranges still use `vscode-html-languageservice`; ASP/VBScript ranges are formatted by the built-in formatter.

## Assistant Instructions

`AGENTS.md` is the source instruction file for coding agents. `CLAUDE.md`, `GEMINI.md`, and `.github/copilot-instructions.md` are symlinks to it.
