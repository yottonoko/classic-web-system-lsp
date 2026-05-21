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

The test suite includes JSON-RPC smoke coverage for HTML, CSS, inline style, JavaScript, and ASP/VBScript completions, completion resolve, pull/workspace diagnostics, hover, definition, references, rename, document highlights, signature help, workspace symbols, semantic tokens, selection ranges, inlay hints, call hierarchy, linked editing, file operations, code actions, CodeLens, formatting, workspace indexing, and virtual include roots.

## VBScript Support

- built-in Classic ASP object hover and member completions
- user-defined variable, constant, function, sub, class, method, field, and property symbols
- scope-aware completions for procedure-local variables and parameters
- `Set value = New ClassName` inference for `value.Member` completions
- `Me.Member` completions inside classes
- definition and references for user-defined VBScript symbols
- include-aware VBScript symbols for completions and definition jumps
- rename, document highlights, signature help, workspace symbols, and semantic tokens for VBScript symbols
- selection ranges, inlay hints, call hierarchy, type definition, implementation, and CodeLens for VBScript symbols
- conservative support for `ReDim`, `For Each`, `With`, and `Server.CreateObject("ADODB.*")` completions
- TypeScript-backed hover, navigation, references, rename, signature help, call hierarchy, and project-model-aware module resolution for JavaScript and server-side JScript regions
- lazy workspace symbol and diagnostic indexing for unopened `.asp`, `.asa`, and `.inc` files
- HTML/CSS rename, CSS/JS document symbols, richer folding, CSS colors, and include file-operation updates

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
- `aspLsp.inlayHints.variableTypes`: show inferred VBScript variable types
- `aspLsp.inlayHints.parameterNames`: show VBScript procedure parameter names at call sites
- `aspLsp.inlayHints.functionReturnTypes`: show inferred VBScript function return types
- `aspLsp.codeLens.references`: show VBScript reference counts
- `aspLsp.codeLens.includes`: show include resolution lenses
- `aspLsp.iis.url`: URL opened by the IIS debug helper command
- `aspLsp.iis.webRoot`: web root used by the IIS debug helper command
- `aspLsp.iis.browser`: VS Code debug type used by the IIS debug helper command
- `aspLsp.iisExpress.url`: URL opened by the IIS Express debug helper command
- `aspLsp.iisExpress.webRoot`: web root used by the IIS Express debug helper command
- `aspLsp.iisExpress.browser`: VS Code debug type used by the IIS Express debug helper command

## Current v1 Limits

- VBScript analysis is intentionally conservative. It uses an error-tolerant CST and opt-in strict type checks rather than a full VBScript compiler.
- `.inc` files are treated as fragments, so full-document HTML diagnostics are suppressed for them.
- Include resolution supports `file` and `virtual` directives, missing include diagnostics, and bounded cycle detection.
- COM and IIS runtime behavior are not executed. COM type information comes from built-in stubs or `aspLsp.vbscript.comTypes`.
- Call hierarchy, CodeLens, type definition, and implementation are static and user-defined-symbol first; runtime COM dispatch is not modeled.
- IIS debug support opens a configured URL in a browser debug session; it does not attach to IIS, COM, or server-side Classic ASP runtime.
- IIS Express support is a browser launch helper; it does not start or configure IIS Express by itself.
- Full-document formatting is CST based and conservative. HTML-only ranges still use `vscode-html-languageservice`; ASP/VBScript ranges are formatted by the built-in formatter.

## Assistant Instructions

`AGENTS.md` is the source instruction file for coding agents. `CLAUDE.md`, `GEMINI.md`, and `.github/copilot-instructions.md` are symlinks to it.
