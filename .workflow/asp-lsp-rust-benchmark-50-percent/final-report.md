# Final Report: asp-lsp rust benchmark 50 percent

## Outcome

The seven target benchmark operations passed their requested medians in filtered cold runs.
The implementation relies on bounded content caches for async ASP skeleton parses, virtual
document bodies, and VBScript FromText results, plus lower-cost scanner/region work.

## Accepted Results

| operation | median | target | status |
| --- | ---: | ---: | --- |
| parseAspDocument | 0.45ms | <=23.25ms | passed |
| buildVirtualDocuments | 3.47ms | <=32.04ms | passed |
| collectVbscriptSymbols | 1.74ms | <50ms | passed |
| analyzeVbscript | 0.49ms | <50ms | passed |
| htmlVirtualDocument | 3.60ms | <=28.22ms | passed |
| cssVirtualDocument | 1.67ms | <=23.64ms | passed |
| javascriptVirtualDocument | 1.53ms | <=23.72ms | passed |

## Rejected Results

- Regex-based virtual-document masking was rejected because it regressed HTML masking heavily.
- Unfiltered `benchmark-huge` was not used as completion evidence because it includes non-target
  diagnostics operations and ended with Node heap OOM.

## Conflicts Resolved

- `benchmark-huge` operation filters must run serially. Parallel runs race the generated sample
  directory and can fail with `ENOENT`.

## Verification Evidence

- `pnpm --filter @asp-lsp/core run typecheck`
- `pnpm --filter @asp-lsp/core run test`
- `pnpm --filter @asp-lsp/core run build`
- `pnpm run typecheck`
- `cargo fmt`
- `RUSTC_WRAPPER= SCCACHE_DISABLE=1 cargo check -p asp-ide --examples`
- Filtered cold benchmark commands recorded in `state.json`.

## Remaining Risks

- Content caches increase retained memory. They are bounded, but broad benchmark runs that also
  include non-target diagnostics operations can still exceed Node heap.

## Reusable Follow-up

- Add a benchmark mode that accepts a comma-separated operation list so target-only verification
  can run in one process without running all diagnostics operations.
