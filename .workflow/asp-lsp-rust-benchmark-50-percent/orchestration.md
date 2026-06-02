# Orchestration: asp-lsp rust benchmark 50 percent

## Execution Rules

- Keep the original objective intact.
- Ask for approval before risky, expensive, external, or destructive actions.
- Keep immediate blocking work local.
- Delegate only bounded, disjoint, materially useful packets.
- Integrate packet results before final verification.

## Branching Rules

## Packet Prompts

## Completion Audit
# Orchestration

## Sequence
1. Run disjoint discovery packets for parser/virtual documents and VB analysis.
2. Integrate findings into a narrow implementation plan.
3. Edit only the implementation paths needed for the target operations.
4. Verify behavior with focused checks.
5. Rerun the benchmark and compare medians against targets.
6. Record accepted/rejected findings and remaining risk in `final-report.md`.

## Packet Prompts

### P1 parser-virtual
Objective: find safe speedups for `parseAspDocument`, `buildVirtualDocuments`, and language virtual document operations.
Files: `packages/core/src/parser.ts`, `packages/core/src/virtual-document.ts` or equivalent, `crates/asp-ide/src/lib.rs`, benchmark scripts.
Do: identify duplicate parsing, repeated JSON traversal, source-map allocation, and benchmark/product path mismatches.
Do not: edit code or propose behavior-changing scanner shortcuts.
Expected output: concise findings with file and function references.

### P2 vb-analysis
Objective: find safe speedups for `collectVbscriptSymbols` and `analyzeVbscript`.
Files: `crates/asp-analysis/src/core_bridge.rs`, `packages/core/src/vbscript.ts`, `packages/core/src/vbscript-cst.ts`, benchmark scripts.
Do: identify repeated parsing, JSON cloning, cache key misses, and duplicated symbol/diagnostic work.
Do not: change diagnostic semantics or remove checks.
Expected output: concise findings with file and function references.

### P3 verification-design
Objective: design the narrow checks needed to prove behavior and performance.
Files: tests under `packages/core/test`, `crates/asp-lsp-server/tests`, relevant benchmark scripts.
Do: list commands and target assertions.
Do not: run destructive commands or commit.
Expected output: verification checklist.

## Branching Rules
- If the first implementation misses a target by less than 10%, inspect the specific operation and iterate once.
- If targets are missed by more than 10% after a safe implementation, report the blocker and the next risky optimization separately.
- If a benchmark failure is harness-specific, verify with a second relevant benchmark before changing product code.
