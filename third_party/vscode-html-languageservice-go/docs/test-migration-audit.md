# Baseline Test Migration Audit

この監査は、TypeScript baseline commit `82b56fd` の `src/test/*.test.ts` を対象に、Go 側の parity test と oracle manifest が同じ内容を確認しているかをファイル単位で照合した記録です。

Go 側には次の機械検査があります。

- `baseline_coverage_manifest_test.go`: baseline の active helper/assertion marker 516 件を保持します。
- `baseline_assertion_manifest_test.go`: completion/custom provider/path completion の item expectation 362 件を保持します。
- `baseline_completion_assertions_test.go`: completion baseline の item expectation を実際の Go language service に対して実行します。

## Audit Result

| Baseline TS test file | Named tests | Active checks | Go coverage | Result |
| --- | ---: | ---: | --- | --- |
| `completion.test.ts` | 13 | 289 | `TestBaselineCompletionAssertions`, `TestCompletionBaselineRepresentativeParity`, `TestCompletionQuoteAndTagCompleteBaselineParity`, `TestCompletionDataAriaCaseAndSettingsBaselineParity`, `TestCompletionCloseTagFilterTextBaselineParity`, `TestCompletionEntityBaselineParity` | Exact label/result/filter/option coverage. This audit tightened duplicate-label checks and exact `div` documentation parity. |
| `completionParticipant.test.ts` | 2 | 20 | `TestCompletionParticipantAttributeValueBaselineParity`, `TestCompletionParticipantContentBaselineParity` | All active callback positions, range text, and callback counts covered. |
| `customProviders.test.ts` | 2 | 17 | `TestCustomProviderBaselineParity`, `TestBaselineAssertionManifest` | Completion and hover coverage covered. This audit tightened duplicate-label checks and exact completion documentation for `Bar`, `bar`, `fooAttr`, `xattr`, and repeated value-set contexts. |
| `folding.test.ts` | 11 | 19 | `TestFoldingBaselineParity`, `TestFoldingBaselineLimitParity` | All fold ranges, region/comment kinds, incomplete cases, and range limits covered. |
| `formatter.test.ts` | 25 | 25 | `TestFormatterFullDocumentAndRange`, `TestFormatterBaselineRangeParity`, `TestFormatterBaselineCSSOptionsParity` | Full document, range formatting, range-with-indent variants, bug fixtures, and embedded CSS options covered. |
| `highlighting.test.ts` | 5 | 29 | `TestHighlightingBaselineParity` | All document highlight offsets and tag names covered. |
| `hover.test.ts` | 1 | 22 | `TestHoverBaselineParity` | This audit added the missing opening/closing tag cursor positions, exact markdown content, exact Baseline icon text, and option range checks. |
| `linkedEditing.test.ts` | 1 | 18 | `TestLinkedEditingBaselineParity` | All linked-editing range and no-range cases covered. |
| `links.test.ts` | 3 | 48 | `TestLinkCreationBaselineParity`, `TestLinkDetectionBaselineParity` | Link creation, detection, fragment resolution, and local target behavior covered. |
| `matchingTagPosition.test.ts` | 1 | 12 | `TestMatchingTagPositionBaselineParity` | All matching tag offset cases covered. |
| `parser.test.ts` | 9 | 38 | `TestParserBaselineTreeParity`, `TestParserBaselineRecoveryParity`, `TestParserBaselineFindNodeBeforeParity`, `TestParserBaselineAttributesParity`, `TestNodeAttributeNames` | Tree/recovery/find-node coverage covered. This audit tightened attributes to match TS `deepEqual` shape including exact attribute maps, child lists, and parser-created `attributeNames` insertion order. |
| `pathCompletions.test.ts` | 17 | 82 | `TestPathCompletionBaselineParity`, `TestBaselineAssertionManifest` | Relative/absolute paths, dotfile exclusion, custom elements, middle-of-path completion, and file-type filtering covered. `Unquoted Path` is inactive in TS because the body is commented as unsupported. |
| `rename.test.ts` | 4 | 25 | `TestRenameBaselineParity` | Matched, self-closing, inner, unmatched, and no-rename positions covered. Duplicate TS cases are represented once when they assert identical behavior. |
| `scanner.test.ts` | 45 | 56 | `TestScannerBaselineParity` | All scanner baseline input objects and state carry-over cases covered. |
| `selectionRange.test.ts` | 10 | 29 | `TestSelectionRangeBaselineParity` | All selection range parent chains covered. |
| `symbols.test.ts` | 4 | 10 | `TestSymbolsBaselineParity` | DocumentSymbol and SymbolInformation names, containers, ranges, and selection ranges covered. |

## Notes

- The TypeScript baseline has 153 named `test(...)` blocks across the 16 active `.test.ts` files.
- The only named test without active runtime assertions is `pathCompletions.test.ts` / `Unquoted Path`; the TypeScript body is commented out as unsupported.
- The audit found weak parity checks in completion documentation, custom provider completion documentation, hover cursor/content coverage, and parser attributes. Those checks are now explicit Go tests.
- The Baseline icon constants in Go were restored to the same base64 SVG data used by the TypeScript baseline so hover and completion documentation can be compared exactly.
