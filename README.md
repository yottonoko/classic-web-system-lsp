# asp-lsp

Classic ASP language server and VS Code extension.

The implementation treats `.asp`, `.asa`, and `.inc` files as mixed documents:

- Classic ASP server script regions: `<% %>`, `<%= %>`, `<%@ %>`, and `<script runat="server">`
- Classic ASP/IIS compatibility is the highest priority for server script boundary scanning
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
- quick fixes for undeclared variables, missing includes, removable unused VBScript declarations, strict type diagnostics such as missing `Set`, unnecessary `Set`, and type annotations, and extract-variable refactors for selected VBScript expressions
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

The generated benchmark samples exercise larger workspaces and editor update
paths:

| Command                                  | Sample                                       | Use case                                  |
| ---------------------------------------- | -------------------------------------------- | ----------------------------------------- |
| `pnpm run benchmark:change`              | `samples/classic-asp-large-benchmark`        | Backward-compatible large change latency. |
| `pnpm run benchmark:change:large`        | `samples/classic-asp-large-benchmark`        | Explicit large change latency.            |
| `pnpm run benchmark:change:huge`         | `samples/classic-asp-huge-benchmark`         | Very large file/include stacks.           |
| `pnpm run benchmark:change:include-tree` | `samples/classic-asp-include-tree-benchmark` | Guarded broad include tree run.           |

The include-tree change benchmark is intentionally disabled until this scenario
is at least 10000% faster than the current baseline. After confirming that
speedup, set `ASP_LSP_BENCH_INCLUDE_TREE_SPEEDUP_PERCENT=10000` or higher and
start with a narrow scenario:

```sh
ASP_LSP_BENCH_INCLUDE_TREE_SPEEDUP_PERCENT=10000 ASP_LSP_BENCH_ITERATIONS=1 ASP_LSP_BENCH_CHANGE_KIND=replace ASP_LSP_BENCH_CHANGE_MODE=default ASP_LSP_BENCH_BACKGROUND=off pnpm run benchmark:change:include-tree
```

## Settings

| Item                                                                                 | Default                  | Description                                                                                                                                        |
| ------------------------------------------------------------------------------------ | ------------------------ | -------------------------------------------------------------------------------------------------------------------------------------------------- |
| `aspLsp.locale`                                                                      | `auto`                   | Runtime message locale, `auto`, `en`, or `ja`; `auto` uses Japanese for `ja*` VS Code/LSP client locales and English otherwise.                    |
| `aspLsp.defaultLanguage`                                                             | `VBScript`               | Default server-side language, `VBScript` or `JScript`.                                                                                             |
| `aspLsp.checkJs`                                                                     | `false`                  | Enable semantic checks for client JavaScript regions.                                                                                              |
| `aspLsp.diagnostics.debounceMs`                                                      | `250`                    | Delay diagnostics after text changes in milliseconds; `0` publishes immediately.                                                                   |
| `aspLsp.debug.output`                                                                | `off`                    | Debug timing output, `off`, `summary`, or `verbose`; VS Code highlights elapsed duration as green, yellow, orange, or red.                         |
| `aspLsp.javascript.unusedDiagnostics`                                                | `true`                   | Report unused JavaScript/JScript locals and parameters as hints.                                                                                   |
| `aspLsp.javascript.autoImports`                                                      | `true`                   | Enable TypeScript-powered JavaScript/JScript auto import completions and quick fixes.                                                              |
| `aspLsp.javascript.ignoreProjectConfig`                                              | `false`                  | Ignore nearest `tsconfig.json` or `jsconfig.json` for embedded JavaScript/JScript language service projects.                                       |
| `aspLsp.javascript.compilerOptions`                                                  | `{}`                     | Extra TypeScript compiler options for embedded JavaScript/JScript language service projects.                                                       |
| `aspLsp.virtualRoot`                                                                 | `""`                     | Root directory for `<!-- #include virtual="..." -->`.                                                                                              |
| `aspLsp.virtualRoots`                                                                | `[]`                     | Additional virtual include roots.                                                                                                                  |
| `aspLsp.windowsPathResolution`                                                       | `true`                   | Resolve includes case-insensitively like Windows and report diagnostics when path casing does not exactly match the file system.                   |
| `aspLsp.legacyEncoding`                                                              | `auto`                   | Encoding for unopened include files, `auto`, `utf8`, `shift_jis`, or `cp932`.                                                                      |
| `aspLsp.format.indentSize`                                                           | `null`                   | Classic ASP formatter indent size; `null` uses editor options.                                                                                     |
| `aspLsp.format.indentStyle`                                                          | Unset                    | `space` or `tab`; unset uses editor options.                                                                                                       |
| `aspLsp.format.printWidth`                                                           | `null`                   | Preferred wrap width for embedded HTML and CSS; `null` uses each delegate default.                                                                 |
| `aspLsp.format.endOfLine`                                                            | `auto`                   | `lf`, `crlf`, or `auto`; `auto` preserves the document's current line-ending style.                                                                |
| `aspLsp.format.insertFinalNewline`                                                   | `false`                  | Add a final newline when full-document formatting rewrites the file.                                                                               |
| `aspLsp.format.preserveNewLines`                                                     | `true`                   | Preserve existing blank lines in embedded HTML and CSS formatting.                                                                                 |
| `aspLsp.format.maxPreserveNewLines`                                                  | `null`                   | Maximum consecutive blank lines preserved by embedded HTML and CSS formatting.                                                                     |
| `aspLsp.format.indentEmptyLines`                                                     | `false`                  | Indent otherwise empty lines in embedded HTML and CSS blocks.                                                                                      |
| `aspLsp.format.enabledLanguages`                                                     | All formatter languages  | Languages the formatter may rewrite: `html`, `vbscript`, `css`, `javascript`, and `jscript`.                                                       |
| `aspLsp.format.embeddedLanguageFormatting`                                           | `auto`                   | `auto` formats embedded CSS/JavaScript/JScript; `off` leaves those embedded languages unchanged.                                                   |
| `aspLsp.format.respectDisableRegions`                                                | `true`                   | Preserve ranges marked with `asp-format off/on` or `asp-lsp-format off/on`.                                                                        |
| `aspLsp.format.htmlIndentSize`                                                       | `null`                   | HTML formatter indent size; `null` falls back to `aspLsp.format.indentSize` or editor options.                                                     |
| `aspLsp.format.htmlIndentStyle`                                                      | Unset                    | HTML formatter indent style; unset falls back to `aspLsp.format.indentStyle` or editor options.                                                    |
| `aspLsp.format.htmlWrapLineLength`                                                   | `null`                   | HTML wrap length; `null` uses `aspLsp.format.printWidth`, and `0` disables HTML wrapping.                                                          |
| `aspLsp.format.htmlWrapAttributes`                                                   | `auto`                   | HTML attribute wrapping strategy, including preserve and force modes.                                                                              |
| `aspLsp.format.htmlWrapAttributesIndentSize`                                         | `null`                   | Indent size for aligned wrapped HTML attributes.                                                                                                   |
| `aspLsp.format.htmlIndentInnerHtml`                                                  | `false`                  | Indent top-level `html`, `head`, and `body` contents when formatting complete HTML documents.                                                      |
| `aspLsp.format.htmlUnformatted`                                                      | `""`                     | Comma-separated HTML tags whose tags should not be reformatted.                                                                                    |
| `aspLsp.format.htmlContentUnformatted`                                               | `""`                     | Comma-separated HTML tags whose inner content should not be reformatted.                                                                           |
| `aspLsp.format.htmlExtraLiners`                                                      | `""`                     | Comma-separated HTML tags that get an extra blank line before them.                                                                                |
| `aspLsp.format.cssIndentSize`                                                        | `null`                   | CSS formatter indent size; `null` falls back to `aspLsp.format.indentSize` or editor options.                                                      |
| `aspLsp.format.cssIndentStyle`                                                       | Unset                    | CSS formatter indent style; unset falls back to `aspLsp.format.indentStyle` or editor options.                                                     |
| `aspLsp.format.cssWrapLineLength`                                                    | `null`                   | CSS wrap length; `null` uses `aspLsp.format.printWidth`, and `0` disables CSS wrapping.                                                            |
| `aspLsp.format.cssNewlineBetweenRules`                                               | `true`                   | Separate CSS rulesets with a blank line.                                                                                                           |
| `aspLsp.format.cssNewlineBetweenSelectors`                                           | `true`                   | Put selectors in comma-separated selector lists on separate lines.                                                                                 |
| `aspLsp.format.cssSpaceAroundSelectorSeparator`                                      | `false`                  | Add spaces around CSS selector separators such as `>`, `+`, and `~`.                                                                               |
| `aspLsp.format.cssBraceStyle`                                                        | `collapse`               | CSS brace style: `collapse` keeps `{` on the selector line, `expand` moves it to its own line.                                                     |
| `aspLsp.format.javascriptIndentSize`                                                 | `null`                   | JavaScript formatter indent size; `null` falls back to `aspLsp.format.indentSize` or editor options.                                               |
| `aspLsp.format.javascriptIndentStyle`                                                | Unset                    | JavaScript formatter indent style; unset falls back to `aspLsp.format.indentStyle` or editor options.                                              |
| `aspLsp.format.jscriptIndentSize`                                                    | `null`                   | JScript formatter indent size; `null` falls back to JavaScript indent settings, shared formatter settings, or editor options.                      |
| `aspLsp.format.jscriptIndentStyle`                                                   | Unset                    | JScript formatter indent style; unset falls back to JavaScript indent style, shared formatter settings, or editor options.                         |
| `aspLsp.format.javascriptSemicolons`                                                 | `null`                   | TypeScript formatter semicolon preference for embedded JavaScript/JScript: `ignore`, `insert`, or `remove`.                                        |
| `aspLsp.format.javascriptIndentSwitchCase`                                           | `null`                   | Optional TypeScript formatter override for indenting JavaScript/JScript `case` clauses.                                                            |
| `aspLsp.format.javascriptPlaceOpenBraceOnNewLineForFunctions`                        | `null`                   | Optional TypeScript formatter override for JavaScript/JScript function brace placement.                                                            |
| `aspLsp.format.javascriptPlaceOpenBraceOnNewLineForControlBlocks`                    | `null`                   | Optional TypeScript formatter override for JavaScript/JScript control-block brace placement.                                                       |
| `aspLsp.format.javascriptInsertSpaceAfterCommaDelimiter`                             | `null`                   | Optional TypeScript formatter override for spaces after JavaScript/JScript commas.                                                                 |
| `aspLsp.format.javascriptInsertSpaceAfterSemicolonInForStatements`                   | `null`                   | Optional TypeScript formatter override for spaces after semicolons in JavaScript/JScript `for` statements.                                         |
| `aspLsp.format.javascriptInsertSpaceBeforeAndAfterBinaryOperators`                   | `true`                   | Spaces around JavaScript/JScript binary operators; default preserves existing ASP LSP behavior.                                                    |
| `aspLsp.format.javascriptInsertSpaceAfterKeywordsInControlFlowStatements`            | `null`                   | Optional TypeScript formatter override for spaces after JavaScript/JScript control-flow keywords.                                                  |
| `aspLsp.format.javascriptInsertSpaceAfterFunctionKeywordForAnonymousFunctions`       | `null`                   | Optional TypeScript formatter override for spaces after anonymous JavaScript/JScript `function`.                                                   |
| `aspLsp.format.javascriptInsertSpaceAfterOpeningAndBeforeClosingNonemptyParenthesis` | `null`                   | Optional TypeScript formatter override for spaces inside non-empty JavaScript/JScript parentheses.                                                 |
| `aspLsp.format.javascriptInsertSpaceAfterOpeningAndBeforeClosingNonemptyBrackets`    | `null`                   | Optional TypeScript formatter override for spaces inside non-empty JavaScript/JScript brackets.                                                    |
| `aspLsp.format.javascriptInsertSpaceAfterOpeningAndBeforeClosingNonemptyBraces`      | `null`                   | Optional TypeScript formatter override for spaces inside non-empty JavaScript/JScript braces.                                                      |
| `aspLsp.format.javascriptInsertSpaceAfterOpeningAndBeforeClosingEmptyBraces`         | `null`                   | Optional TypeScript formatter override for spaces inside empty JavaScript/JScript braces.                                                          |
| `aspLsp.format.javascriptInsertSpaceBeforeFunctionParenthesis`                       | `null`                   | Optional TypeScript formatter override for spaces before JavaScript/JScript function parentheses.                                                  |
| `aspLsp.format.vbscriptIndentSize`                                                   | `null`                   | VBScript formatter indent size; `null` falls back to `aspLsp.format.indentSize` or editor options.                                                 |
| `aspLsp.format.vbscriptIndentStyle`                                                  | Unset                    | VBScript formatter indent style; unset falls back to `aspLsp.format.indentStyle` or editor options.                                                |
| `aspLsp.format.vbscriptKeywordCase`                                                  | `null`                   | VBScript keyword casing: `preserve`, `upper`, `lower`, or `title`; `null` keeps legacy `uppercaseKeywords` behavior.                               |
| `aspLsp.format.vbscriptLineContinuationIndentSize`                                   | `null`                   | Extra indent size for VBScript `_` continuation lines; `null` uses one VBScript indent unit.                                                       |
| `aspLsp.format.vbscriptSelectCaseIndent`                                             | `caseIndented`           | `caseIndented` indents `Case` under `Select Case`; `caseAligned` aligns `Case` with `Select Case`.                                                 |
| `aspLsp.format.uppercaseKeywords`                                                    | `false`                  | Format VBScript keywords as uppercase.                                                                                                             |
| `aspLsp.format.alignAssignments`                                                     | `false`                  | Align simple consecutive VBScript assignments.                                                                                                     |
| `aspLsp.format.vbscriptBlockIndent`                                                  | `indentInsideDelimiter`  | Align multiline `<% ... %>` VBScript with delimiters or indent one level inside them.                                                              |
| `aspLsp.format.vbscriptTagIndentMode`                                                | `null`                   | VBScript tag-indent mode: `relativeToTag`, `ignoreTag`, or `preserveExisting`; `null` keeps legacy `ignoreVbscriptTagIndent` behavior.             |
| `aspLsp.format.cssTagIndentMode`                                                     | `null`                   | CSS tag-indent mode: `relativeToTag`, `ignoreTag`, or `preserveExisting`; `null` keeps legacy `ignoreCssTagIndent` behavior.                       |
| `aspLsp.format.javascriptTagIndentMode`                                              | `null`                   | JavaScript/JScript tag-indent mode: `relativeToTag`, `ignoreTag`, or `preserveExisting`; `null` keeps legacy `ignoreJavaScriptTagIndent` behavior. |
| `aspLsp.format.aspDelimiterSpacing`                                                  | `padded`                 | ASP delimiter spacing for one-line blocks, expressions, and directives: `padded` or `compact`.                                                     |
| `aspLsp.format.aspBlockNewline`                                                      | `preserve`               | Preserve ASP block line shape, always expand one-line blocks, or collapse multiline blocks when possible.                                          |
| `aspLsp.format.nestedAspInCssJs`                                                     | `skipRegion`             | Handle CSS/JS regions with nested ASP as `skipRegion`, `protectAspOnly`, or `formatAroundAsp`.                                                     |
| `aspLsp.format.fragmentMode`                                                         | `auto`                   | `auto` uses normal HTML formatter behavior; `fragment` formats under a temporary wrapper and removes it afterward; `document` formats directly.    |
| `aspLsp.format.ignoreVbscriptTagIndent`                                              | `false`                  | Ignore surrounding tag indentation when formatting VBScript regions.                                                                               |
| `aspLsp.format.ignoreCssTagIndent`                                                   | `false`                  | Ignore surrounding tag indentation when formatting CSS regions.                                                                                    |
| `aspLsp.format.ignoreJavaScriptTagIndent`                                            | `false`                  | Ignore surrounding tag indentation when formatting JavaScript/JScript regions.                                                                     |
| `aspLsp.format.onSave`                                                               | `false`                  | Return full-document formatting edits from `textDocument/willSaveWaitUntil`.                                                                       |
| `aspLsp.vbscript.typeChecking`                                                       | `basic`                  | `basic` or `strict`; strict enables VBScript type diagnostics.                                                                                     |
| `aspLsp.vbscript.identifierCase`                                                     | `ignore`                 | `PascalCase`, `UPPERCASE`, `camelCase`, `lowercase`, `snake_case`, `UPPER_SNAKE`, or `ignore`; reports declaration casing hints and quick fixes.   |
| `aspLsp.vbscript.identifierCaseByKind`                                               | `{}`                     | Per-symbol-kind VBScript identifier casing overrides.                                                                                              |
| `aspLsp.vbscript.comTypes`                                                           | `{}`                     | Custom COM type catalog keyed by `Server.CreateObject` Prog.ID.                                                                                    |
| `aspLsp.vbscript.globals`                                                            | `{}`                     | Runtime or framework-provided VBScript globals keyed by identifier.                                                                                |
| `aspLsp.vbscript.unusedDiagnostics`                                                  | `true`                   | Report unused VBScript declarations as hints.                                                                                                      |
| `aspLsp.vbscript.deadCodeDiagnostics`                                                | `true`                   | Report unreachable VBScript code as hints.                                                                                                         |
| `aspLsp.vbscript.syntaxSnippets`                                                     | `true`                   | Enable VBScript syntax snippet completions.                                                                                                        |
| `aspLsp.vbscript.initializedDimQuickFixStyle`                                        | `newline`                | Quick fix style for initialized `Dim`; `newline` uses separate statements, `sameLineColon` uses `Dim a : a = value`.                               |
| `aspLsp.inlayHints.variableTypes`                                                    | `true`                   | Show inferred VBScript variable types.                                                                                                             |
| `aspLsp.inlayHints.parameterNames`                                                   | `true`                   | Show VBScript procedure parameter names at call sites.                                                                                             |
| `aspLsp.inlayHints.functionReturnTypes`                                              | `true`                   | Show inferred VBScript function return types.                                                                                                      |
| `aspLsp.inlayHints.implicitByRef`                                                    | `true`                   | Show `ByRef` for VBScript parameters whose passing mode is omitted.                                                                                |
| `aspLsp.inlayHints.globalVariableMarkers`                                            | `global`                 | Show VBScript variable scope markers, `global`, `local`, `all`, or `off`; in-progress include analysis may use `(?)`.                              |
| `aspLsp.codeLens.references`                                                         | `true`                   | Show VBScript reference counts.                                                                                                                    |
| `aspLsp.codeLens.referenceScope`                                                     | `analyzed`               | Reference count scope, `analyzed` for currently analyzed files or `workspace` for full workspace reference counts.                                 |
| `aspLsp.codeLens.includes`                                                           | `true`                   | Show include resolution lenses.                                                                                                                    |
| `aspLsp.graph.showRootNodes`                                                         | `true`                   | Show root file nodes in graph views.                                                                                                               |
| `aspLsp.graph.showFileNodes`                                                         | `true`                   | Show non-root file nodes in graph views.                                                                                                           |
| `aspLsp.graph.showFunctionNodes`                                                     | `true`                   | Show function nodes in graph views.                                                                                                                |
| `aspLsp.graph.showSubNodes`                                                          | `true`                   | Show subroutine nodes in graph views.                                                                                                              |
| `aspLsp.graph.showClassNodes`                                                        | `true`                   | Show class nodes in graph views.                                                                                                                   |
| `aspLsp.graph.showMethodNodes`                                                       | `false`                  | Show class method nodes with unknown procedure kind in graph views.                                                                                |
| `aspLsp.graph.showMethodFunctionNodes`                                               | `false`                  | Show class function method nodes in graph views.                                                                                                   |
| `aspLsp.graph.showMethodSubNodes`                                                    | `false`                  | Show class sub method nodes in graph views.                                                                                                        |
| `aspLsp.graph.showPropertyNodes`                                                     | `false`                  | Show class property nodes in graph views.                                                                                                          |
| `aspLsp.graph.showMemberNodes`                                                       | `false`                  | Show built-in, configured, and unresolved object member nodes in graph views.                                                                      |
| `aspLsp.graph.showGlobalVariableNodes`                                               | `true`                   | Show global VBScript variable and object nodes in graph views.                                                                                     |
| `aspLsp.graph.showGlobalConstantNodes`                                               | `true`                   | Show global VBScript constant nodes in graph views.                                                                                                |
| `aspLsp.graph.showLocalVariableNodes`                                                | `false`                  | Show procedure-local VBScript variable and class field nodes in graph views.                                                                       |
| `aspLsp.graph.showLocalConstantNodes`                                                | `false`                  | Show procedure-local and class constant nodes in graph views.                                                                                      |
| `aspLsp.graph.showParameterNodes`                                                    | `false`                  | Show function, sub, method, and property parameter nodes in graph views.                                                                           |
| `aspLsp.graph.showUnresolvedNodes`                                                   | `true`                   | Show unresolved reference nodes in graph views.                                                                                                    |
| `aspLsp.graph.showIncludeLinks`                                                      | `true`                   | Show include links in graph views.                                                                                                                 |
| `aspLsp.graph.showDeclareLinks`                                                      | `true`                   | Show declaration-to-source links in graph views.                                                                                                   |
| `aspLsp.graph.showReferenceLinks`                                                    | `true`                   | Show reference links in graph views.                                                                                                               |
| `aspLsp.graph.showCallLinks`                                                         | `true`                   | Show call links in graph views.                                                                                                                    |
| `aspLsp.graph.showUnresolvedLinks`                                                   | `true`                   | Show unresolved reference links in graph views.                                                                                                    |
| `aspLsp.graph.showMemberLinks`                                                       | `false`                  | Show object member reference and call links in graph views.                                                                                        |
| `aspLsp.graph.openLocation`                                                          | `active`                 | Controls where graph views open: `active` for the current editor group or `beside` for the side editor group.                                      |
| `aspLsp.workspace.includes`                                                          | `["**/*.{asp,asa,inc}"]` | Workspace-relative glob patterns included in workspace analysis, workspace graphs, and folder graphs.                                              |
| `aspLsp.workspace.excludes`                                                          | `[]`                     | Workspace-relative glob patterns excluded from workspace analysis, workspace graphs, and folder graphs.                                            |
| `aspLsp.workspace.respectGitIgnore`                                                  | `false`                  | Ignore files matched by a workspace root `.gitignore` during workspace analysis and graph folder scans.                                            |
| `aspLsp.workspace.maxIndexFiles`                                                     | `5000`                   | Maximum unopened Classic ASP files indexed in one workspace scan.                                                                                  |
| `aspLsp.workspace.scanChunkSize`                                                     | `200`                    | Filesystem entries processed before yielding during workspace indexing.                                                                            |
| `aspLsp.workspace.busyAnalysisConcurrency`                                           | `0`                      | Maximum workspace analysis concurrency; `0` uses half the available CPU count.                                                                     |

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
- ASP region scanning follows Classic ASP/IIS script block boundaries before embedded language syntax. In `<% ... %>` regions, raw `%>` closes the ASP block even when it appears in VBScript/JScript strings or comments. Use Classic ASP escaping such as `%\>` when literal output needs that character sequence. `<script runat="server">` regions are bounded by the first matching script end tag.
- VBScript XML documentation comments must use VB.NET-style triple quotes (`'''`). Single-quote XML comments are treated as ordinary comments.
- XML documentation comments are editor documentation only and hover labels them that way. Existing `' @type`, `' @param ... As ...`, and `' @returns ...` annotations remain the source for explicit type metadata.
- Unused diagnostics are hints. Classic ASP runtime entry points such as `Application_OnStart`, public class members, include-cross references, and names inside strings/comments are excluded from VBScript unused checks.
- JavaScript/JScript auto imports use TypeScript language service results. Import edits are applied only when every edit maps safely back into the same ASP JavaScript/JScript virtual document; cross-file or unmappable edits are skipped instead of partially applying.
- Cross-language rename is conservative. It links HTML `id`/`class`, CSS `#id`/`.class`, and common JavaScript DOM selector strings such as `querySelector`, `querySelectorAll`, `getElementById`, and `classList` across open and indexed Classic ASP workspace files.
- `.inc` files are treated as fragments, so full-document HTML diagnostics are suppressed for them.
- Include resolution supports `file` and `virtual` directives, Windows-style case-insensitive lookup with exact-casing diagnostics, missing include diagnostics, and bounded cycle detection.
- COM and IIS runtime behavior are not executed. COM type information comes from built-in ASP/COM/ADO stubs or `aspLsp.vbscript.comTypes`.
- Call hierarchy, type hierarchy, CodeLens, type definition, implementation, monikers, and inline values are static and user-defined-symbol first; runtime COM dispatch is not modeled.
- Save and will-save hooks refresh diagnostics and caches. `willSaveWaitUntil` is non-mutating by default and returns full-document formatting edits only when `aspLsp.format.onSave` is enabled.
- Full-document formatting is CST based and conservative. HTML-only ranges still use `vscode-html-languageservice`; ASP/VBScript ranges are formatted by the built-in formatter.
- Localization applies to asp-lsp generated diagnostics, code actions, CodeLens, and extension messages. TypeScript, HTML, CSS, VS Code, and Node.js upstream messages are left unchanged.
- Native VS Code manifest text uses `package.nls.json` / `package.nls.ja.json` and follows the VS Code UI locale. Changing `aspLsp.locale` affects runtime messages, LSP output, and the Classic ASP settings webview after save or reload, but VS Code's built-in Settings UI does not immediately relocalize manifest titles or setting descriptions.

## Assistant Instructions

`AGENTS.md` is the source instruction file for coding agents. `CLAUDE.md`, `GEMINI.md`, and `.github/copilot-instructions.md` are symlinks to it.
