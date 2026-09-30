# Classic ASP LSP

Classic ASP language server and VS Code extension.

The Go language-service dependencies are included as source under `third_party/`
and resolved locally. A clone or source archive contains the Go ports without
requiring access to their original repositories. See
[Go port dependencies](third_party/GO_PORTS.md) for the module layout, licenses,
and standalone dependency tests.

The implementation treats `.asp`, `.asa`, and `.inc` files as mixed documents. Standalone `.vbs` files use the `vbscript` language mode:

- Classic ASP server script regions: `<% %>`, `<%= %>`, `<%@ %>`, and `<script runat="server">`
- Classic ASP/IIS compatibility is the highest priority for server script boundary scanning
- HTML regions delegated to the Go port of `vscode-html-languageservice`
- CSS regions and `style=""` attributes delegated to the Go port of `vscode-css-languageservice`
- client JavaScript and server-side JScript regions backed by `typescript-go`
- Classic ASP and VBScript boundaries parsed into a Go CST for editor features and formatting
- VBScript server regions handled by the built-in Go analyzer

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
- screen-transition analysis with finite VBScript string evaluation, form ownership, source ranges for unresolved destinations, and a searchable, collapsible tree with cycle/shared-target references; see [navigation analysis](docs/navigation-analysis.md)
- lazy workspace symbol and diagnostic indexing for unopened `.asp`, `.asa`, `.inc`, and `.vbs` files
- HTML/CSS rename, CSS/JS document symbols, richer folding, CSS colors, and include file-operation updates

## Standalone Server

```sh
pnpm run build:go
bin/asp-lsp-go --stdio
```

## VS Code Development

Open this repository in VS Code, run `pnpm install` and `pnpm run build`, then start an Extension Development Host from `apps/vscode`.

The extension registers:

- language ids: `classic-asp` for `.asp`, `.asa`, `.inc`; `vbscript` for `.vbs`
- development server path: `bin/asp-lsp-go`
- VSIX server path: `server/asp-lsp-go`

To build a local VSIX:

```sh
pnpm run package:vsix
```

The package command builds the extension and Go server, then writes
`apps/vscode/classic-asp-lsp-<version>.vsix`. The VSIX contains the server at
`server/asp-lsp-go`, not a nested `node_modules` tree. It also includes the
project license texts and generated runtime dependency notices; see
[third-party notices](apps/vscode/THIRD_PARTY_NOTICES.md).

## License

This project is available under either the [MIT](LICENSE-MIT) or
[Apache-2.0](LICENSE-APACHE) license. The extension package contains both
license texts. Its bundled Go and JavaScript dependencies are listed in
`third_party_licenses/INDEX.md` inside each VSIX, with their license and notice
files. The list is generated from installed production packages and the Go
server's imported modules during `pnpm run package:vsix`.

## Samples

The `samples/classic-asp-dashboard` directory contains a multi-page Classic ASP
sample for manual language-server checks. It mixes `.asp` pages, `.inc` includes,
VBScript server regions, a server-side JScript block, HTML, CSS, and client
JavaScript.

The generated benchmark samples exercise larger workspaces and editor update
paths. Run Go benchmarks with:

```sh
pnpm run benchmark:go
```

The optional [cross-revision performance gate](docs/performance-regression-gate.md)
requires an available Go baseline revision. The historical default baseline is
from a separate development history and is not present in a fresh public clone.

## Settings

| Item                                                                                 | Default                      | Description                                                                                                                                        |
| ------------------------------------------------------------------------------------ | ---------------------------- | -------------------------------------------------------------------------------------------------------------------------------------------------- |
| `aspLsp.locale`                                                                      | `auto`                       | Runtime message locale, `auto`, `en`, or `ja`; `auto` uses Japanese for `ja*` VS Code/LSP client locales and English otherwise.                    |
| `aspLsp.defaultLanguage`                                                             | `VBScript`                   | Default server-side language, `VBScript` or `JScript`.                                                                                             |
| `aspLsp.webview.theme`                                                               | `auto`                       | Theme for flowchart, navigation graph, and workspace file webviews; `auto` follows the current VS Code theme, or choose `light`/`dark`.            |
| `aspLsp.checkJs`                                                                     | `false`                      | Enable semantic checks for client JavaScript regions.                                                                                              |
| `aspLsp.diagnostics.debounceMs`                                                      | `250`                        | Delay diagnostics after text changes in milliseconds; `0` publishes immediately.                                                                   |
| `aspLsp.debug.output`                                                                | `off`                        | Debug timing output, `off`, `summary`, or `verbose`; VS Code highlights elapsed duration as green, yellow, orange, or red.                         |
| `aspLsp.javascript.unusedDiagnostics`                                                | `true`                       | Report unused JavaScript/JScript locals and parameters as hints.                                                                                   |
| `aspLsp.javascript.autoImports`                                                      | `true`                       | Enable TypeScript-powered JavaScript/JScript auto import completions and quick fixes.                                                              |
| `aspLsp.javascript.ignoreProjectConfig`                                              | `false`                      | Ignore nearest `tsconfig.json` or `jsconfig.json` for embedded JavaScript/JScript language service projects.                                       |
| `aspLsp.javascript.compilerOptions`                                                  | `{}`                         | Extra TypeScript compiler options for embedded JavaScript/JScript language service projects.                                                       |
| `aspLsp.virtualRoot`                                                                 | `""`                         | Root directory for `<!-- #include virtual="..." -->`.                                                                                              |
| `aspLsp.virtualRoots`                                                                | `[]`                         | Additional virtual include roots.                                                                                                                  |
| `aspLsp.windowsPathResolution`                                                       | `true`                       | Resolve includes case-insensitively like Windows and report diagnostics when path casing does not exactly match the file system.                   |
| `aspLsp.legacyEncoding`                                                              | `auto`                       | Encoding for unopened include files, `auto`, `utf8`, `shift_jis`, or `cp932`.                                                                      |
| `aspLsp.format.indentSize`                                                           | `null`                       | Classic ASP formatter indent size; `null` uses editor options.                                                                                     |
| `aspLsp.format.indentStyle`                                                          | Unset                        | `space` or `tab`; unset uses editor options.                                                                                                       |
| `aspLsp.format.printWidth`                                                           | `null`                       | Preferred wrap width for embedded HTML and CSS; `null` uses each delegate default.                                                                 |
| `aspLsp.format.endOfLine`                                                            | `auto`                       | `lf`, `crlf`, or `auto`; `auto` preserves the document's current line-ending style.                                                                |
| `aspLsp.format.insertFinalNewline`                                                   | `false`                      | Add a final newline when full-document formatting rewrites the file.                                                                               |
| `aspLsp.format.preserveNewLines`                                                     | `true`                       | Preserve existing blank lines in embedded HTML and CSS formatting.                                                                                 |
| `aspLsp.format.maxPreserveNewLines`                                                  | `null`                       | Maximum consecutive blank lines preserved by embedded HTML and CSS formatting.                                                                     |
| `aspLsp.format.indentEmptyLines`                                                     | `false`                      | Indent otherwise empty lines in embedded HTML and CSS blocks.                                                                                      |
| `aspLsp.format.enabledLanguages`                                                     | All formatter languages      | Languages the formatter may rewrite: `html`, `vbscript`, `css`, `javascript`, and `jscript`.                                                       |
| `aspLsp.format.embeddedLanguageFormatting`                                           | `auto`                       | `auto` formats embedded CSS/JavaScript/JScript; `off` leaves those embedded languages unchanged.                                                   |
| `aspLsp.format.respectDisableRegions`                                                | `true`                       | Preserve ranges marked with `asp-format off/on` or `asp-lsp-format off/on`.                                                                        |
| `aspLsp.format.htmlIndentSize`                                                       | `null`                       | HTML formatter indent size; `null` falls back to `aspLsp.format.indentSize` or editor options.                                                     |
| `aspLsp.format.htmlIndentStyle`                                                      | Unset                        | HTML formatter indent style; unset falls back to `aspLsp.format.indentStyle` or editor options.                                                    |
| `aspLsp.format.htmlWrapLineLength`                                                   | `null`                       | HTML wrap length; `null` uses `aspLsp.format.printWidth`, and `0` disables HTML wrapping.                                                          |
| `aspLsp.format.htmlWrapAttributes`                                                   | `auto`                       | HTML attribute wrapping strategy, including preserve and force modes.                                                                              |
| `aspLsp.format.htmlWrapAttributesIndentSize`                                         | `null`                       | Indent size for aligned wrapped HTML attributes.                                                                                                   |
| `aspLsp.format.htmlIndentInnerHtml`                                                  | `false`                      | Indent top-level `html`, `head`, and `body` contents when formatting complete HTML documents.                                                      |
| `aspLsp.format.htmlUnformatted`                                                      | `""`                         | Comma-separated HTML tags whose tags should not be reformatted.                                                                                    |
| `aspLsp.format.htmlContentUnformatted`                                               | `""`                         | Comma-separated HTML tags whose inner content should not be reformatted.                                                                           |
| `aspLsp.format.htmlExtraLiners`                                                      | `""`                         | Comma-separated HTML tags that get an extra blank line before them.                                                                                |
| `aspLsp.format.cssIndentSize`                                                        | `null`                       | CSS formatter indent size; `null` falls back to `aspLsp.format.indentSize` or editor options.                                                      |
| `aspLsp.format.cssIndentStyle`                                                       | Unset                        | CSS formatter indent style; unset falls back to `aspLsp.format.indentStyle` or editor options.                                                     |
| `aspLsp.format.cssWrapLineLength`                                                    | `null`                       | CSS wrap length; `null` uses `aspLsp.format.printWidth`, and `0` disables CSS wrapping.                                                            |
| `aspLsp.format.cssNewlineBetweenRules`                                               | `true`                       | Separate CSS rulesets with a blank line.                                                                                                           |
| `aspLsp.format.cssNewlineBetweenSelectors`                                           | `true`                       | Put selectors in comma-separated selector lists on separate lines.                                                                                 |
| `aspLsp.format.cssSpaceAroundSelectorSeparator`                                      | `false`                      | Add spaces around CSS selector separators such as `>`, `+`, and `~`.                                                                               |
| `aspLsp.format.cssBraceStyle`                                                        | `collapse`                   | CSS brace style: `collapse` keeps `{` on the selector line, `expand` moves it to its own line.                                                     |
| `aspLsp.format.javascriptIndentSize`                                                 | `null`                       | JavaScript formatter indent size; `null` falls back to `aspLsp.format.indentSize` or editor options.                                               |
| `aspLsp.format.javascriptIndentStyle`                                                | Unset                        | JavaScript formatter indent style; unset falls back to `aspLsp.format.indentStyle` or editor options.                                              |
| `aspLsp.format.jscriptIndentSize`                                                    | `null`                       | JScript formatter indent size; `null` falls back to JavaScript indent settings, shared formatter settings, or editor options.                      |
| `aspLsp.format.jscriptIndentStyle`                                                   | Unset                        | JScript formatter indent style; unset falls back to JavaScript indent style, shared formatter settings, or editor options.                         |
| `aspLsp.format.javascriptSemicolons`                                                 | `null`                       | TypeScript formatter semicolon preference for embedded JavaScript/JScript: `ignore`, `insert`, or `remove`.                                        |
| `aspLsp.format.javascriptIndentSwitchCase`                                           | `null`                       | Optional TypeScript formatter override for indenting JavaScript/JScript `case` clauses.                                                            |
| `aspLsp.format.javascriptPlaceOpenBraceOnNewLineForFunctions`                        | `null`                       | Optional TypeScript formatter override for JavaScript/JScript function brace placement.                                                            |
| `aspLsp.format.javascriptPlaceOpenBraceOnNewLineForControlBlocks`                    | `null`                       | Optional TypeScript formatter override for JavaScript/JScript control-block brace placement.                                                       |
| `aspLsp.format.javascriptInsertSpaceAfterCommaDelimiter`                             | `null`                       | Optional TypeScript formatter override for spaces after JavaScript/JScript commas.                                                                 |
| `aspLsp.format.javascriptInsertSpaceAfterSemicolonInForStatements`                   | `null`                       | Optional TypeScript formatter override for spaces after semicolons in JavaScript/JScript `for` statements.                                         |
| `aspLsp.format.javascriptInsertSpaceBeforeAndAfterBinaryOperators`                   | `true`                       | Spaces around JavaScript/JScript binary operators; default preserves existing ASP LSP behavior.                                                    |
| `aspLsp.format.javascriptInsertSpaceAfterKeywordsInControlFlowStatements`            | `null`                       | Optional TypeScript formatter override for spaces after JavaScript/JScript control-flow keywords.                                                  |
| `aspLsp.format.javascriptInsertSpaceAfterFunctionKeywordForAnonymousFunctions`       | `null`                       | Optional TypeScript formatter override for spaces after anonymous JavaScript/JScript `function`.                                                   |
| `aspLsp.format.javascriptInsertSpaceAfterOpeningAndBeforeClosingNonemptyParenthesis` | `null`                       | Optional TypeScript formatter override for spaces inside non-empty JavaScript/JScript parentheses.                                                 |
| `aspLsp.format.javascriptInsertSpaceAfterOpeningAndBeforeClosingNonemptyBrackets`    | `null`                       | Optional TypeScript formatter override for spaces inside non-empty JavaScript/JScript brackets.                                                    |
| `aspLsp.format.javascriptInsertSpaceAfterOpeningAndBeforeClosingNonemptyBraces`      | `null`                       | Optional TypeScript formatter override for spaces inside non-empty JavaScript/JScript braces.                                                      |
| `aspLsp.format.javascriptInsertSpaceAfterOpeningAndBeforeClosingEmptyBraces`         | `null`                       | Optional TypeScript formatter override for spaces inside empty JavaScript/JScript braces.                                                          |
| `aspLsp.format.javascriptInsertSpaceBeforeFunctionParenthesis`                       | `null`                       | Optional TypeScript formatter override for spaces before JavaScript/JScript function parentheses.                                                  |
| `aspLsp.format.vbscriptIndentSize`                                                   | `null`                       | VBScript formatter indent size; `null` falls back to `aspLsp.format.indentSize` or editor options.                                                 |
| `aspLsp.format.vbscriptIndentStyle`                                                  | Unset                        | VBScript formatter indent style; unset falls back to `aspLsp.format.indentStyle` or editor options.                                                |
| `aspLsp.format.vbscriptKeywordCase`                                                  | `null`                       | VBScript keyword casing: `preserve`, `upper`, `lower`, or `title`; `null` keeps legacy `uppercaseKeywords` behavior.                               |
| `aspLsp.format.vbscriptLineContinuationIndentSize`                                   | `null`                       | Extra indent size for VBScript `_` continuation lines; `null` uses one VBScript indent unit.                                                       |
| `aspLsp.format.vbscriptSelectCaseIndent`                                             | `caseIndented`               | `caseIndented` indents `Case` under `Select Case`; `caseAligned` aligns `Case` with `Select Case`.                                                 |
| `aspLsp.format.uppercaseKeywords`                                                    | `false`                      | Format VBScript keywords as uppercase.                                                                                                             |
| `aspLsp.format.alignAssignments`                                                     | `false`                      | Align simple consecutive VBScript assignments.                                                                                                     |
| `aspLsp.format.vbscriptBlockIndent`                                                  | `indentInsideDelimiter`      | Align multiline `<% ... %>` VBScript with delimiters or indent one level inside them.                                                              |
| `aspLsp.format.vbscriptTagIndentMode`                                                | `null`                       | VBScript tag-indent mode: `relativeToTag`, `ignoreTag`, or `preserveExisting`; `null` keeps legacy `ignoreVbscriptTagIndent` behavior.             |
| `aspLsp.format.cssTagIndentMode`                                                     | `null`                       | CSS tag-indent mode: `relativeToTag`, `ignoreTag`, or `preserveExisting`; `null` keeps legacy `ignoreCssTagIndent` behavior.                       |
| `aspLsp.format.javascriptTagIndentMode`                                              | `null`                       | JavaScript/JScript tag-indent mode: `relativeToTag`, `ignoreTag`, or `preserveExisting`; `null` keeps legacy `ignoreJavaScriptTagIndent` behavior. |
| `aspLsp.format.aspDelimiterSpacing`                                                  | `padded`                     | ASP delimiter spacing for one-line blocks, expressions, and directives: `padded` or `compact`.                                                     |
| `aspLsp.format.aspBlockNewline`                                                      | `preserve`                   | Preserve ASP block line shape, always expand one-line blocks, or collapse multiline blocks when possible.                                          |
| `aspLsp.format.nestedAspInCssJs`                                                     | `skipRegion`                 | Handle CSS/JS regions with nested ASP as `skipRegion`, `protectAspOnly`, or `formatAroundAsp`.                                                     |
| `aspLsp.format.fragmentMode`                                                         | `auto`                       | `auto` uses normal HTML formatter behavior; `fragment` formats under a temporary wrapper and removes it afterward; `document` formats directly.    |
| `aspLsp.format.ignoreVbscriptTagIndent`                                              | `false`                      | Ignore surrounding tag indentation when formatting VBScript regions.                                                                               |
| `aspLsp.format.ignoreCssTagIndent`                                                   | `false`                      | Ignore surrounding tag indentation when formatting CSS regions.                                                                                    |
| `aspLsp.format.ignoreJavaScriptTagIndent`                                            | `false`                      | Ignore surrounding tag indentation when formatting JavaScript/JScript regions.                                                                     |
| `aspLsp.format.onSave`                                                               | `false`                      | Return full-document formatting edits from `textDocument/willSaveWaitUntil`.                                                                       |
| `aspLsp.vbscript.typeChecking`                                                       | `basic`                      | `basic` or `strict`; strict enables VBScript type diagnostics.                                                                                     |
| `aspLsp.vbscript.identifierCase`                                                     | `ignore`                     | `PascalCase`, `UPPERCASE`, `camelCase`, `lowercase`, `snake_case`, `UPPER_SNAKE`, or `ignore`; reports declaration casing hints and quick fixes.   |
| `aspLsp.vbscript.identifierCaseByKind`                                               | `{}`                         | Per-symbol-kind VBScript identifier casing overrides.                                                                                              |
| `aspLsp.vbscript.comTypes`                                                           | `{}`                         | Custom COM type catalog keyed by `Server.CreateObject` Prog.ID.                                                                                    |
| `aspLsp.vbscript.globals`                                                            | `{}`                         | Runtime or framework-provided VBScript globals keyed by identifier.                                                                                |
| `aspLsp.vbscript.unusedDiagnostics`                                                  | `true`                       | Report unused VBScript declarations as hints.                                                                                                      |
| `aspLsp.vbscript.implicitGlobalDiagnostics`                                          | `false`                      | Report variables implicitly declared at script-global scope while `Option Explicit` is disabled as warnings.                                       |
| `aspLsp.vbscript.deadCodeDiagnostics`                                                | `true`                       | Report unreachable VBScript code as hints.                                                                                                         |
| `aspLsp.vbscript.syntaxSnippets`                                                     | `true`                       | Enable VBScript syntax snippet completions.                                                                                                        |
| `aspLsp.vbscript.autoIncludes`                                                       | `false`                      | Enable VBScript auto include completions and quick fixes for eligible workspace declarations.                                                      |
| `aspLsp.vbscript.initializedDimQuickFixStyle`                                        | `sameLineColon`              | Quick fix style for initialized `Dim`; `newline` uses separate statements, `sameLineColon` uses `Dim a : a = value`.                               |
| `aspLsp.inlayHints.variableTypes`                                                    | `false`                      | Show inferred VBScript variable types.                                                                                                             |
| `aspLsp.inlayHints.parameterNames`                                                   | `true`                       | Show VBScript procedure parameter names at call sites.                                                                                             |
| `aspLsp.inlayHints.functionReturnTypes`                                              | `false`                      | Show inferred VBScript function return types.                                                                                                      |
| `aspLsp.inlayHints.implicitByRef`                                                    | `false`                      | Show `ByRef` for VBScript parameters whose passing mode is omitted.                                                                                |
| `aspLsp.inlayHints.scopeMarkers.global`                                              | `false`                      | Show `(global)` before global VBScript variable type inlay hints.                                                                                  |
| `aspLsp.inlayHints.scopeMarkers.local`                                               | `false`                      | Show `(local)` before local VBScript variable type inlay hints.                                                                                    |
| `aspLsp.inlayHints.scopeMarkers.uncertain`                                           | `false`                      | Show `(?)` before type inlay hints for implicit globals whose include/global context is uncertain.                                                 |
| `aspLsp.codeLens.references`                                                         | `true`                       | Show VBScript reference counts.                                                                                                                    |
| `aspLsp.codeLens.includes`                                                           | `false`                      | Show include resolution lenses.                                                                                                                    |
| `aspLsp.flowchart.openLocation`                                                      | `active`                     | Controls where flowcharts open: `active` for the current editor group or `beside` for the side editor group.                                       |
| `aspLsp.navigationGraph.openLocation`                                                | `active`                     | Controls where navigation graphs open: `active` for the current editor group or `beside` for the side editor group.                                |
| `aspLsp.workspaceFiles.openLocation`                                                 | `active`                     | Controls where workspace file selection opens: `active` for the current editor group or `beside` for the side editor group.                        |
| `aspLsp.workspace.includes`                                                          | `["**/*.{asp,asa,inc,vbs}"]` | Workspace-relative glob patterns included in workspace analysis, workspace graphs, and folder graphs.                                              |
| `aspLsp.workspace.excludes`                                                          | `[]`                         | Workspace-relative glob patterns excluded from workspace analysis, workspace graphs, and folder graphs.                                            |
| `aspLsp.workspace.respectGitIgnore`                                                  | `false`                      | Ignore files matched by a workspace root `.gitignore` during workspace analysis and graph folder scans.                                            |
| `aspLsp.workspace.scanChunkSize`                                                     | `200`                        | Filesystem entries processed before yielding during workspace indexing.                                                                            |
| `aspLsp.workspace.busyAnalysisConcurrency`                                           | `0`                          | Maximum workspace analysis concurrency; `0` uses the available logical CPU count.                                                                  |

### Workspace analysis reuse

- Within one unchanged workspace source generation, each file is physically read at most once. The immutable source snapshot is shared by workspace indexing, include graph construction, VBScript auto-include discovery, and later workspace consumers.
- On the cold workspace-index path, syntax/CST construction is counted separately from the physical file read and runs once for each canonical file identity, content hash, and default language. Syntax diagnostics, full analysis, include graph construction, and VBScript auto-include discovery reuse that parsed document. This is one source read plus one syntax parse, not a second syntax-only filesystem read.
- A repeated configuration notification with values identical to the active settings is a no-op: it does not cancel or restart workspace indexing, clear analysis caches, or invalidate graph results. Formatting, diagnostics presentation, cache tuning, and other settings that do not alter workspace discovery or source interpretation also preserve the active workspace index; workspace scan scheduling controls may start a replacement generation when their values actually change.
- Formatting, debug, memory, filesystem, and disk-cache tuning changes preserve graph and in-memory analysis results. Disk-cache changes reconfigure persistence without cancelling or restarting active workspace indexing. Graph settings invalidate only graph results, while a network profile change also refreshes include/reference state when it changes the effective case-resolution mode.
- A new source read or syntax parse is expected only after the file content or source identity changes, the relevant source snapshot is explicitly invalidated, or a source-interpretation setting such as the legacy encoding or default language changes. Workspace membership and include-resolution changes may schedule a new index generation, while unchanged source snapshots and parsed documents remain reusable when their cache keys are still valid.
- A warm startup keeps full parsed-document hydration lazy when persisted workspace artifacts are sufficient. If an older or partial cache lacks a required summary, the server may build that summary with a transient syntax parse rather than retaining a full parsed document solely for the cache upgrade.
- A complete warm workspace-index restore reuses the persisted immutable text when validating the matching include graph, so unchanged files are not physically reread. Validation falls back to a context-cancellable source read only when restored workspace text is unavailable; a cancelled generation stops before continuing through the remaining graph entries.
- Warm workspace restoration prepares the persisted include-graph candidate while cached workspace files undergo bounded parallel metadata validation. Bulk validation shares the workspace worker limit and preserves the editor-facing worker slot, so cache recovery does not monopolize hover, completion, or other interactive requests.
- Restored workspace and include-graph candidates remain private until freshness checks finish. Publication rechecks the active workspace generation, graph revision, disk-cache instance and configuration, effective freshness mode, and cancellation state; changed files are excluded from the candidate and only those missing or stale files fall back to workspace discovery and source analysis. Cache reconfiguration rejects the old candidate and schedules a fresh workspace generation. Every speculative include-graph read is joined before the workspace build returns, including cancellation and scan/read failures.
- State-changing document and workspace notifications cancel older requests before publication, allowing later editor interactions to observe the new state without waiting for obsolete workspace work. Concurrent requests for the same background workspace graph share one build and one progress task.
- Progress counts are clamped to their declared totals before display. An aggregate remains indeterminate while any active task has an unknown total, and restarting the language server discards stale extension-side progress claims.

These invariants are covered by `TestColdWorkspaceIndexReadsAndParsesEachFileOnce`, `TestDidOpenInitialSyntaxAndFullAnalysisShareOneParse`, `TestWarmWorkspaceIndexRestoresIncludeGraphWithoutHydratingFileBundles`, `TestWarmWorkspaceIndexOverlapsIncludeGraphReadWithMetadataValidation`, `TestWarmWorkspaceIndexRejectsIncludeGraphCandidateAfterGraphGenerationChange`, `TestWarmWorkspaceIndexValidatesCachedMetadataInParallel`, `TestWarmWorkspaceIndexDoesNotPublishCandidateBeforeFreshnessValidation`, `TestWarmWorkspaceIndexRejectsCacheCandidateAfterCacheReconfiguration`, `TestWarmWorkspaceIndexRejectsGraphOnlyCandidateAfterCacheReconfiguration`, `TestWarmWorkspaceIndexJoinsSpeculativeGraphReadOnScanFailure`, `TestWarmWorkspaceIndexReusesFreshnessReadDuringStaleFallback`, `TestWorkspaceIncludeGraphRestoreStopsReadingAfterCancellation`, the `TestSourceSnapshot*` single-flight tests, and the `TestDidChangeConfiguration*Preserves*` configuration tests. `BenchmarkWarmWorkspaceIndexMetadataValidation` compares the bounded parallel warm path with a one-worker baseline.

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
- COM and IIS runtime behavior are not executed. COM type information comes from built-in ASP/COM/ADO stubs or `aspLsp.vbscript.comTypes`. Built-in stubs cover ADODB (`Connection`, `Recordset`, `Command`, `Stream`, `Record`, and their collections), `Scripting.FileSystemObject`/`Dictionary` and related objects, MSXML (`ServerXMLHTTP`, `XMLHTTP`, `DOMDocument`, including version-dependent ProgIDs such as `.6.0`), `WinHttp.WinHttpRequest`, `CDO.Message`/`CDO.Configuration`, `CDONTS.NewMail`, `WScript.Shell`, and `WScript.Network`. Member chains such as `rs.Fields("name").Value` resolve through these stubs.
- Call hierarchy, type hierarchy, CodeLens, type definition, implementation, monikers, and inline values are static and user-defined-symbol first; runtime COM dispatch is not modeled.
- Save and will-save hooks refresh diagnostics and caches. `willSaveWaitUntil` is non-mutating by default and returns full-document formatting edits only when `aspLsp.format.onSave` is enabled.
- Full-document formatting is CST based and conservative. HTML-only ranges still use `vscode-html-languageservice`; ASP/VBScript ranges are formatted by the built-in formatter.
- Localization applies to asp-lsp generated diagnostics, code actions, CodeLens, and extension messages. TypeScript, HTML, CSS, VS Code, and Node.js upstream messages are left unchanged.
- Native VS Code manifest text uses `package.nls.json` / `package.nls.ja.json` and follows the VS Code UI locale. Changing `aspLsp.locale` affects runtime messages and LSP output, but VS Code's built-in Settings UI does not immediately relocalize manifest titles or setting descriptions.

## Assistant Instructions

`AGENTS.md` is the source instruction file for coding agents. `CLAUDE.md`, `GEMINI.md`, and `.github/copilot-instructions.md` are symlinks to it.

### Debug log analysis

Run **Classic ASP: Analyze Debug Log** (`aspLsp.analyzeDebugLog`) from the Command Palette and paste server logs. The command opens a local-only page with Japanese/English explanations, a chronological event DAG, timing comparisons, and a heatmap. Each event has a Hint button explaining its operation and recorded fields. Unknown events remain visible and are explicitly identified.

The DAG places time downward and concurrent operations in separate columns. Solid blue connections pair recorded starts and finishes; dashed connections indicate estimated starts or inferred parent relationships. Purple connections show parent relationships. Vertical spacing preserves order rather than elapsed-time scale. Untimed Output records follow their original log order between neighboring records; recorded starts and finishes establish lifecycle intervals even without timestamps. Ordering does not invent elapsed times or prove causality. Repeated or backward timestamps preserve log order. The diagram displays up to 100 operations; search narrows the input. Heatmap values group elapsed durations by completion time, not CPU usage or a critical path, and overlapping durations must not be summed as wall time.

Hover, graph exports, and Excel declaration types use the same detail as inlay hints. Source-position semantics used for analysis and hierarchy remain separate from declaration-wide type presentation.

The log graph has an expanded graph-only view. Circular question-mark help buttons support hover, focus, and click; row help sits beside the event type. Open node details with the Details button or a double-click. Selected rows share the same structured explanation, timing/status cards, field descriptions, and original log. Dialog headers remain visible while their content scrolls.
