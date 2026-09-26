# Porting Audit

This file records the migration audits run while replacing the old
TypeScript/JavaScript implementation with the Go implementation in this
repository.

## Source Test Content Audit

The original TypeScript tests under `src/test` were compared one by one against
the current Go tests and implementation. The audit checked top-level test cases,
nested helper assertions, fixture behavior, expected edits, documentation
strings, completion item metadata, ranges, colors, symbols, highlights, and
parser assertion inputs.

Latest status summary:

- TypeScript test files checked: 29
- Files with behavior-equivalent Go coverage: 29
- Files with partial parity or API-shape differences: 0
- Active non-error parser assertions covered by generated Go source assertions:
  - CSS parser: 358 unique `assertNode` / `assertNoNode` / `assertFunction` rows
  - LESS parser: 231 unique `assertNode` / `assertNoNode` rows
  - SCSS parser: 393 unique active `assertNode` rows
- Active parser error assertions covered by Go parser diagnostics:
  - CSS parser: 77 active `assertError` rows
  - LESS parser: 9 active `assertError` rows
  - SCSS parser: 66 active `assertError` rows

| TypeScript test file | Current status |
| --- | --- |
| `src/test/css/codeActions.test.ts` | Covered, including legacy `Command[]` from `DoCodeActions` and structured `CodeAction[]` from `DoCodeActions2` |
| `src/test/css/completion.test.ts` | Covered with fake filesystem providers; upstream real-fixture path integration is represented by equivalent provider tests |
| `src/test/css/customData.test.ts` | Covered |
| `src/test/css/folding.test.ts` | Covered |
| `src/test/css/formatter.test.ts` | Covered |
| `src/test/css/hover.test.ts` | Covered |
| `src/test/css/languageFacts.test.ts` | Covered, including node-based `IsColorValue` / `GetColorValue` and named color table |
| `src/test/css/lint.test.ts` | Covered, including upstream-style CSS/LESS/SCSS reruns for shared lint cases |
| `src/test/css/navigation.test.ts` | Covered |
| `src/test/css/nodes.test.ts` | Covered |
| `src/test/css/parser.test.ts` | Covered, including 77 active `assertError` ID checks |
| `src/test/css/scanner.test.ts` | Covered |
| `src/test/css/selectionRange.test.ts` | Covered |
| `src/test/css/selectorPrinting.test.ts` | Covered |
| `src/test/less/formatter.test.ts` | Covered |
| `src/test/less/lessCompletion.test.ts` | Covered |
| `src/test/less/lessNavigation.test.ts` | Covered |
| `src/test/less/lint.test.ts` | Covered |
| `src/test/less/nodes.test.ts` | Covered |
| `src/test/less/parser.test.ts` | Covered, including 9 active `assertError` ID checks |
| `src/test/less/scanner.test.ts` | Covered |
| `src/test/scss/formatter.test.ts` | Covered |
| `src/test/scss/languageFacts.test.ts` | Covered |
| `src/test/scss/lint.test.ts` | Covered |
| `src/test/scss/parser.test.ts` | Covered, including 66 active `assertError` ID checks |
| `src/test/scss/scssCompletion.test.ts` | Covered |
| `src/test/scss/scssNavigation.test.ts` | Covered |
| `src/test/scss/selectorPrinting.test.ts` | Covered |
| `src/test/util.test.ts` | Covered, including the upstream single-match `trim` semantics |

## Resolved Prior Differences

The previous audit identified three partial parity areas. They are now covered:

- `DoCodeActions` returns upstream-style legacy LSP `Command[]` using
  `_css.applyCodeAction`, while `DoCodeActions2` keeps the structured
  `[]lsp.CodeAction` surface.
- Shared CSS lint cases run through CSS, LESS, and SCSS document language IDs
  unless a test explicitly targets one language.
- The parser package exposes `ParseErrors` with vscode-css-languageservice rule
  IDs and covers all active upstream `assertError` inputs: 77 CSS, 9 LESS, and
  66 SCSS rows.

## Public API Surface

The Go package exports idiomatic counterparts for the public language service
surface through:

- `service.go`
- `lsp/`
- `languagefacts/`

TypeScript-style names were not kept when Go naming is clearer, for example
`DocumentURI`, `CSSDataProvider`, and `GetCSSLanguageService`. The root package
exposes the main `LanguageService` facade, LSP-compatible data structures,
custom data providers, filesystem/path-completion hooks, completion
participants, formatting settings, and language-service constructors.

The `languagefacts` package exposes the color conversion helpers and the
node-based `IsColorString`, `IsColorValue`, and `GetColorValue` helpers needed
to match the upstream language-facts color contract.

All exported Go types in the public packages were checked with an AST-based
doc-comment audit. The checker currently reports no exported type without a
leading documentation comment.

## Behavior Verification

Command:

```sh
GOCACHE=/private/tmp/vscode-css-languageservice-go-cache go test -count=1 ./...
```

The Go suite covers the migrated behavior for CSS, LESS, and SCSS parsing,
scanning, language facts, validation, completion, hover, formatting, folding,
selection ranges, selector printing, document links, document symbols, colors,
highlights, rename, definitions, and code actions.
