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

The test suite includes JSON-RPC smoke coverage for HTML, CSS, inline style, JavaScript, and ASP/VBScript completions, completion resolve, pull/workspace diagnostics, hover, definition, references, rename, document highlights, signature help, workspace symbols, semantic tokens, selection ranges, inlay hints, call hierarchy, type hierarchy, monikers, inline values, linked editing, will-save/save hooks, file operations, code actions, CodeLens, formatting, workspace indexing, and virtual include roots.

## VBScript Support

- built-in Classic ASP object hover and member completions
- user-defined variable, constant, function, sub, class, method, field, and property symbols
- scope-aware completions for procedure-local variables and parameters
- `Set value = New ClassName` inference for `value.Member` completions
- `CreateObject("Prog.ID")` and `Server.CreateObject("Prog.ID")` inference for built-in and configured COM type completions and type hierarchy exploration
- `Me.Member` completions inside classes
- definition and references for user-defined VBScript symbols
- include-aware VBScript symbols for completions and definition jumps
- rename, document highlights, signature help, workspace symbols, and semantic tokens for VBScript symbols
- selection ranges, inlay hints, call hierarchy, type hierarchy, type definition, implementation, monikers, inline values, and CodeLens for VBScript symbols
- quick fixes for undeclared variables, missing includes, include suggestions, removable unused VBScript declarations, strict type diagnostics such as missing `Set`, unnecessary `Set`, and type annotations, and extract-variable refactors for selected VBScript expressions
- VB.NET-style `'''` XML documentation comments for VBScript hover, completion resolve, and signature help
- XML documentation tag completion for `summary`, `remarks`, `param`, `returns`, `value`, `exception`, `see`, `seealso`, `example`, `code`, `c`, `list`, and `para`
- conservative support for `ReDim`, `For Each`, `With`, ASP Reference built-ins, FileSystem/Dictionary/MSWC components, and ADO object completions
- TypeScript-backed hover, navigation, references, rename, signature help, call hierarchy, monikers, inline values, and project-model-aware module resolution for JavaScript and server-side JScript regions
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

The VSIX build bundles the standalone language server into `apps/vscode/server/language-server/dist/server.js` before packaging, so the extension does not ship a nested `node_modules` tree.

## Samples

The `samples/classic-asp-dashboard` directory contains a multi-page Classic ASP
sample for manual language-server checks. It mixes `.asp` pages, `.inc` includes,
VBScript server regions, a server-side JScript block, HTML, CSS, and client
JavaScript.

## Settings

| Item                                      | Default                  | Description                                                                                                                                      |
| ----------------------------------------- | ------------------------ | ------------------------------------------------------------------------------------------------------------------------------------------------ |
| `aspLsp.locale`                           | `auto`                   | Runtime message locale, `auto`, `en`, or `ja`; `auto` uses Japanese for `ja*` VS Code/LSP client locales and English otherwise.                  |
| `aspLsp.defaultLanguage`                  | `VBScript`               | Default server-side language, `VBScript` or `JScript`.                                                                                           |
| `aspLsp.checkJs`                          | `false`                  | Enable semantic checks for client JavaScript regions.                                                                                            |
| `aspLsp.diagnostics.debounceMs`           | `250`                    | Delay diagnostics after text changes in milliseconds; `0` publishes immediately.                                                                 |
| `aspLsp.cache.enabled`                    | `true`                   | Persist best-effort unopened Classic ASP analysis cache entries using CBOR.                                                                      |
| `aspLsp.cache.directory`                  | `""`                     | Directory for asp-lsp analysis cache files; empty uses the operating system cache directory.                                                     |
| `aspLsp.cache.ttlHours`                   | `168`                    | Hours before unused asp-lsp analysis cache files are eligible for cleanup.                                                                       |
| `aspLsp.cache.maxSizeMb`                  | `1024`                   | Maximum analysis cache size in MiB; older cache files are removed when the limit is exceeded.                                                    |
| `aspLsp.debug.output`                     | `off`                    | Debug timing output, `off`, `summary`, or `verbose`; VS Code highlights elapsed duration as green, yellow, orange, or red.                       |
| `aspLsp.javascript.unusedDiagnostics`     | `true`                   | Report unused JavaScript/JScript locals and parameters as hints.                                                                                 |
| `aspLsp.javascript.autoImports`           | `true`                   | Enable TypeScript-powered JavaScript/JScript auto import completions and quick fixes.                                                            |
| `aspLsp.javascript.ignoreProjectConfig`   | `false`                  | Ignore nearest `tsconfig.json` or `jsconfig.json` for embedded JavaScript/JScript language service projects.                                     |
| `aspLsp.virtualRoot`                      | `""`                     | Root directory for `<!-- #include virtual="..." -->`.                                                                                            |
| `aspLsp.virtualRoots`                     | `[]`                     | Additional virtual include roots.                                                                                                                |
| `aspLsp.windowsPathResolution`            | `true`                   | Resolve includes case-insensitively like Windows and report diagnostics when path casing does not exactly match the file system.                 |
| `aspLsp.legacyEncoding`                   | `auto`                   | Encoding for unopened include files, `auto`, `utf8`, `shift_jis`, or `cp932`.                                                                    |
| `aspLsp.format.indentSize`                | `null`                   | Classic ASP formatter indent size; `null` uses editor options.                                                                                   |
| `aspLsp.format.indentStyle`               | Unset                    | `space` or `tab`; unset uses editor options.                                                                                                     |
| `aspLsp.format.uppercaseKeywords`         | `false`                  | Format VBScript keywords as uppercase.                                                                                                           |
| `aspLsp.format.alignAssignments`          | `false`                  | Align simple consecutive VBScript assignments.                                                                                                   |
| `aspLsp.format.ignoreVbscriptTagIndent`   | `false`                  | Ignore surrounding tag indentation when formatting VBScript regions.                                                                             |
| `aspLsp.format.ignoreCssTagIndent`        | `false`                  | Ignore surrounding tag indentation when formatting CSS regions.                                                                                  |
| `aspLsp.format.ignoreJavaScriptTagIndent` | `false`                  | Ignore surrounding tag indentation when formatting JavaScript/JScript regions.                                                                   |
| `aspLsp.format.onSave`                    | `false`                  | Return full-document formatting edits from `textDocument/willSaveWaitUntil`.                                                                     |
| `aspLsp.vbscript.typeChecking`            | `basic`                  | `basic` or `strict`; strict enables VBScript type diagnostics.                                                                                   |
| `aspLsp.vbscript.identifierCase`          | `ignore`                 | `PascalCase`, `UPPERCASE`, `camelCase`, `lowercase`, `snake_case`, `UPPER_SNAKE`, or `ignore`; reports declaration casing hints and quick fixes. |
| `aspLsp.vbscript.identifierCaseByKind`    | `{}`                     | Per-symbol-kind VBScript identifier casing overrides.                                                                                            |
| `aspLsp.vbscript.comTypes`                | `{}`                     | Custom COM type catalog keyed by `Server.CreateObject` Prog.ID.                                                                                  |
| `aspLsp.vbscript.globals`                 | `{}`                     | Runtime or framework-provided VBScript globals keyed by identifier.                                                                              |
| `aspLsp.vbscript.unusedDiagnostics`       | `true`                   | Report unused VBScript declarations as hints.                                                                                                    |
| `aspLsp.vbscript.includeSuggestions`      | `true`                   | Suggest `<!-- #include ... -->` fixes for undeclared symbols found in workspace files.                                                           |
| `aspLsp.vbscript.syntaxSnippets`          | `true`                   | Enable VBScript syntax snippet completions.                                                                                                      |
| `aspLsp.inlayHints.variableTypes`         | `true`                   | Show inferred VBScript variable types.                                                                                                           |
| `aspLsp.inlayHints.parameterNames`        | `true`                   | Show VBScript procedure parameter names at call sites.                                                                                           |
| `aspLsp.inlayHints.functionReturnTypes`   | `true`                   | Show inferred VBScript function return types.                                                                                                    |
| `aspLsp.inlayHints.implicitByRef`         | `true`                   | Show `ByRef` for VBScript parameters whose passing mode is omitted.                                                                              |
| `aspLsp.codeLens.references`              | `true`                   | Show VBScript reference counts.                                                                                                                  |
| `aspLsp.codeLens.includes`                | `true`                   | Show include resolution lenses.                                                                                                                  |
| `aspLsp.workspace.maxIndexFiles`          | `5000`                   | Maximum unopened Classic ASP files indexed in one workspace scan.                                                                                |
| `aspLsp.workspace.scanChunkSize`          | `200`                    | Filesystem entries processed before yielding during workspace indexing.                                                                          |
| `aspLsp.workspace.backgroundAnalysis`     | `true`                   | Analyze unopened Classic ASP workspace files in the background to warm the disk cache.                                                           |
| `aspLsp.workspace.backgroundConcurrency`  | `2`                      | Maximum unopened Classic ASP files analyzed concurrently in background cache warm-up.                                                            |
| `aspLsp.iis.url`                          | `http://localhost/`      | URL opened by the IIS debug helper command.                                                                                                      |
| `aspLsp.iis.webRoot`                      | `""`                     | Web root used by the IIS debug helper command.                                                                                                   |
| `aspLsp.iis.browser`                      | `pwa-chrome`             | VS Code debug type used by the IIS debug helper command.                                                                                         |
| `aspLsp.iisExpress.url`                   | `http://localhost:8080/` | URL opened by the IIS Express debug helper command.                                                                                              |
| `aspLsp.iisExpress.webRoot`               | `""`                     | Web root used by the IIS Express debug helper command.                                                                                           |
| `aspLsp.iisExpress.browser`               | `pwa-chrome`             | VS Code debug type used by the IIS Express debug helper command.                                                                                 |

Example `aspLsp.vbscript.comTypes` and `aspLsp.vbscript.globals` entries:

```json
{
  "aspLsp.vbscript.globals": {
    "CustomerRepository": "MyCompany.CustomerRepository"
  },
  "aspLsp.vbscript.comTypes": {
    "MyCompany.CustomerRepository": {
      "members": {
        "ConnectionString": "String",
        "FindById": {
          "kind": "method",
          "returnType": "Customer",
          "parameters": [{ "name": "id", "type": "Number" }]
        }
      }
    }
  }
}
```

## Current v1 Limits

- VBScript analysis is intentionally conservative. It uses an error-tolerant CST and opt-in strict type checks rather than a full VBScript compiler. `Execute`/`Eval`, dynamic includes, COM late binding, and unusual line continuations are modeled only when they can be inferred statically.
- VBScript XML documentation comments must use VB.NET-style triple quotes (`'''`). Single-quote XML comments are treated as ordinary comments.
- XML documentation comments are editor documentation only and hover labels them that way. Existing `' @type`, `' @param ... As ...`, and `' @returns ...` annotations remain the source for explicit type metadata.
- Unused diagnostics are hints. Classic ASP runtime entry points such as `Application_OnStart`, public class members, include-cross references, and names inside strings/comments are excluded from VBScript unused checks.
- JavaScript/JScript auto imports use TypeScript language service results. Import edits are applied only when every edit maps safely back into the same ASP JavaScript/JScript virtual document; cross-file or unmappable edits are skipped instead of partially applying.
- Cross-language rename is conservative. It links HTML `id`/`class`, CSS `#id`/`.class`, and common JavaScript DOM selector strings such as `querySelector`, `querySelectorAll`, `getElementById`, and `classList` across open and indexed Classic ASP workspace files.
- VBScript has no import syntax, so auto import support is exposed as include suggestions for undeclared symbols that exist in indexed `.asp`, `.asa`, or `.inc` workspace files.
- `.inc` files are treated as fragments, so full-document HTML diagnostics are suppressed for them.
- Include resolution supports `file` and `virtual` directives, Windows-style case-insensitive lookup with exact-casing diagnostics, missing include diagnostics, and bounded cycle detection.
- COM and IIS runtime behavior are not executed. COM type information comes from built-in ASP/COM/ADO stubs or `aspLsp.vbscript.comTypes`.
- Call hierarchy, type hierarchy, CodeLens, type definition, implementation, monikers, and inline values are static and user-defined-symbol first; runtime COM dispatch is not modeled.
- Save and will-save hooks refresh diagnostics and caches. `willSaveWaitUntil` is non-mutating by default and returns full-document formatting edits only when `aspLsp.format.onSave` is enabled.
- IIS debug support opens a configured URL in a browser debug session; it does not attach to IIS, COM, or server-side Classic ASP runtime.
- IIS Express support is a browser launch helper; it does not start or configure IIS Express by itself.
- Full-document formatting is CST based and conservative. HTML-only ranges still use `vscode-html-languageservice`; ASP/VBScript ranges are formatted by the built-in formatter.
- Localization applies to asp-lsp generated diagnostics, code actions, CodeLens, and extension messages. TypeScript, HTML, CSS, VS Code, and Node.js upstream messages are left unchanged.
- VS Code manifest text uses `package.nls.json` / `package.nls.ja.json` and follows the VS Code UI locale. Changing `aspLsp.locale` affects runtime messages and LSP output, but it does not immediately relocalize manifest titles or setting descriptions.

## Assistant Instructions

`AGENTS.md` is the source instruction file for coding agents. `CLAUDE.md`, `GEMINI.md`, and `.github/copilot-instructions.md` are symlinks to it.
