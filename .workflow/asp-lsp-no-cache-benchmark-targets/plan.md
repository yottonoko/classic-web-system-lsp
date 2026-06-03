# asp-lsp no-cache benchmark targets

## Goal

Speed up the seven target huge benchmark operations with benchmark caches disabled.

## Success Criteria

- `parseAspDocument` median <= 23.25ms.
- `buildVirtualDocuments` median <= 32.04ms.
- `collectVbscriptSymbols` median < 50ms.
- `analyzeVbscript` median < 50ms.
- `htmlVirtualDocument` median <= 28.22ms.
- `cssVirtualDocument` median <= 23.64ms.
- `javascriptVirtualDocument` median <= 23.72ms.
- Evidence must come from `benchmark:huge:cold:no-cache` or equivalent env.

## Current Context

- Cached benchmark targets were previously achieved by content caches.
- No-cache benchmark variants now exist and clear core/embedded caches between sources.
- Worktree starts clean on `codex/revolution`.

## Constraints

- Preserve Classic ASP/IIS behavior, UTF-16 offsets, LSP ranges, and public result shapes.
- Use `pnpm`, not `npm`.
- Avoid relying on cache hits for success.
- Do not run huge benchmark operations in parallel.

## Risks

- Full unfiltered huge benchmark includes non-target diagnostics and can OOM.
- Parser and VBScript changes can affect many language server features.
- Benchmark timing has run-to-run variance; use operation-filtered runs.

## Approval Required

None for local code changes and non-destructive benchmarks.

## Work Packets

- P1 parser-scanner: no-cache parse and region construction profile.
- P2 virtual-documents: no-cache build/html/css/javascript profile.
- P3 vb-analysis: no-cache collect/analyze VBScript profile.
- P4 verification: test and benchmark gate design.

## Integration Policy

- Accept only changes that reduce target operation medians without changing API/result shapes.
- Reject cache-only changes for this goal.
- Verify with narrow operation filters before broader checks.

## Verification

- `pnpm --filter @asp-lsp/core run typecheck`
- `pnpm --filter @asp-lsp/core run test`
- `pnpm run typecheck`
- operation-filtered no-cache huge benchmark for all seven target operations

## Reusable Artifacts

Record final results in `final-report.md` and status in `state.json`.
