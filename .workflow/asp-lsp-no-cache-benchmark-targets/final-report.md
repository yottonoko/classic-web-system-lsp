# Final Report

## Outcome

Passed.

The seven target operations pass on `benchmark:large:cold:no-cache`.
The same seven target operations also pass on `benchmark:huge:cold:no-cache`.

## Accepted

- Reduced no-cache virtual-document benchmark overhead by skipping source text fingerprints for virtual-document-only operations.
- Lazily builds source-ordered source-map lookup data only when `toVirtualOffset` is used.
- Reduced VB from-text symbol collection cost by avoiding type inference in the collect-only path.
- Added a lightweight from-text analysis path for non-strict analysis by precomputing minimal symbols.
- Avoided clearing embedded diagnostic benchmark caches for core-only benchmark operations.
- Kept existing `analyzeVbscript` inference behavior after tests showed public analysis expects inferred built-in types.
- Avoided server-object parsing for non-`object` tags and reduced include/directive allocation work.

## Rejected

- Cache-backed success paths for no-cache benchmarks.
- Attribute bounded ASP scanning; profile and timing showed it was slower than the existing `indexOf` path.
- Removing VBScript CST hydration from `analyzeVbscriptFromTextAsync`; it broke existing type inference tests.
- Adding public `endOffset` to `AspInclude`; incremental parse equality tests exposed result-shape drift.

## Verification

- `pnpm --filter @asp-lsp/core run typecheck`: passed.
- `pnpm --filter @asp-lsp/core run test`: passed.
- `pnpm run typecheck`: passed.
- `pnpm run test`: failed with sandboxed `sccache` permission error while building Rust server.
- `RUSTC_WRAPPER= pnpm run test`: passed.
- `ASP_LSP_BENCH_WARMUPS=1 ASP_LSP_BENCH_ITERATIONS=9 pnpm run benchmark:large:cold:no-cache`: target seven operations passed.
- `ASP_LSP_BENCH_WARMUPS=1 ASP_LSP_BENCH_ITERATIONS=5 pnpm run benchmark:huge:cold:no-cache`: target seven operations passed.

## Remaining Risks

- No-cache benchmark timings have high run-to-run variance on this machine.
- From-text collect/analyze now use lightweight non-strict paths; synchronous `collectVbscriptSymbols` and `analyzeVbscript` keep full inference and diagnostics.
