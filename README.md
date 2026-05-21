# asp-lsp

Classic ASP language server and VS Code extension.

The implementation treats `.asp`, `.asa`, and `.inc` files as mixed documents:

- Classic ASP server script regions: `<% %>`, `<%= %>`, `<%@ %>`, and `<script runat="server">`
- HTML regions delegated to `vscode-html-languageservice`
- CSS regions and `style=""` attributes delegated to `vscode-css-languageservice`
- client JavaScript regions delegated to the TypeScript language service
- VBScript server regions handled by the built-in v1 analyzer

## Commands

```sh
pnpm install
pnpm run typecheck
pnpm run lint
pnpm run format:check
pnpm run test
pnpm run build
```

The test suite includes JSON-RPC smoke coverage for HTML, CSS, inline style, JavaScript, and ASP/VBScript completions, diagnostics, hover, definition, and references.

## VBScript Support

- built-in Classic ASP object hover and member completions
- user-defined variable, constant, function, sub, class, method, field, and property symbols
- scope-aware completions for procedure-local variables and parameters
- `Set value = New ClassName` inference for `value.Member` completions
- `Me.Member` completions inside classes
- definition and references for user-defined VBScript symbols
- include-aware VBScript symbols for completions and definition jumps

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

## Current v1 Limits

- VBScript analysis is intentionally conservative. It is regex/scanner based rather than a full VBScript compiler.
- `.inc` files are treated as fragments, so full-document HTML diagnostics are suppressed for them.
- Include resolution supports `file` and `virtual` directives, missing include diagnostics, and bounded cycle detection.
- COM and IIS runtime behavior are not executed or type-checked.
- Full-document formatting is disabled. Range formatting is returned only for HTML-only ranges so ASP server regions are not erased.

## Assistant Instructions

`AGENTS.md` is the source instruction file for coding agents. `CLAUDE.md`, `GEMINI.md`, and `.github/copilot-instructions.md` are symlinks to it.
