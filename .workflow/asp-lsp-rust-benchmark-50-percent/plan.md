# asp-lsp rust benchmark 50 percent

## Goal
Speed up the Rust-owned benchmark operations in `asp-lsp` until each selected operation reaches the stricter target: either 50% of the previous current median or under 50ms.

## Success Criteria
- `parseAspDocument`: median <= 23.25ms.
- `buildVirtualDocuments`: median <= 32.04ms.
- `collectVbscriptSymbols`: median < 50ms.
- `analyzeVbscript`: median < 50ms.
- `htmlVirtualDocument`: median <= 28.22ms.
- `cssVirtualDocument`: median <= 23.64ms.
- `javascriptVirtualDocument`: median <= 23.72ms.
- Preserve behavior for parser, VBScript analysis, virtual documents, and embedded source maps.
- Verify with focused tests and `benchmark:huge:cold` using the same sample class as the baseline.

## Current Context
- Branch: `codex/revolution`.
- Baseline medians are from `ASP_LSP_BENCH_WARMUPS=1 ASP_LSP_BENCH_ITERATIONS=5 pnpm run benchmark:huge:cold`.
- `crates/asp-analysis` and `crates/asp-ide` are Rust crates, but several internal operations still call the compatibility bridge in `crates/asp-analysis/src/core_bridge.rs`.
- HTML/CSS/JavaScript diagnostics are sidecar-owned and are not direct success criteria, except virtual document creation.

## Constraints
- Japanese chat/status/final response.
- Use `pnpm`, not `npm`.
- Keep changes surgical and preserve LSP result shape and Classic ASP/IIS behavior.
- Do not stage, commit, or push unless explicitly asked later.
- Do not delete unrelated code or generated artifacts.

## Risks
- Performance wins may accidentally change source maps, UTF-16 offsets, or VBScript symbol semantics.
- Benchmark cache behavior may mask regressions.
- Over-optimizing benchmark scripts instead of product paths would be invalid.
- Broad Rust rewrites could exceed the requested scope.

## Approval Required
- No external writes, destructive actions, secrets, billing, deploys, or push are planned.
- If a broad codemod, destructive cleanup, or expensive multi-agent run becomes necessary, pause and ask.

## Work Packets
- P1 discovery: map benchmark operations to implementation paths and identify hot repeated work.
- P2 parser/virtual documents: inspect parse and virtual document generation for safe reuse or allocation reductions.
- P3 VB analysis: inspect symbol/diagnostic paths for duplicate parsing, JSON cloning, and cache misses.
- P4 implementation and verification: integrate accepted changes, run tests, rerun benchmarks, and compare against targets.

## Integration Policy
- Accept only changes that affect product code or benchmark harness correctness without changing public result shape.
- Reject benchmark-only shortcuts that bypass real operation work.
- Resolve disagreements by reading the implementation and tests directly.

## Verification
- Start with focused unit tests for touched crates/packages.
- Run `cargo check` for touched Rust crates when Rust code changes.
- Run `pnpm --filter @asp-lsp/core run test` if TypeScript core behavior changes.
- Run `ASP_LSP_BENCH_WARMUPS=1 ASP_LSP_BENCH_ITERATIONS=5 pnpm run benchmark:huge:cold`.
- Compare medians against the success criteria table.

## Reusable Artifacts
- Keep packet notes and final report under this workflow directory.
- Save a reusable recipe only if the run creates a general benchmark optimization workflow worth reusing.
