# Rust Analyzer Style Speed Step 9

Step 9 moves the Rust server closer to a rust-analyzer-style cache
architecture: stable inputs, typed derived queries, explicit dependency
fingerprints, small invalidation sets, and measurable hot paths. Step 9P is the
execution-plan checkpoint. It does not change runtime behavior.

## Step 9P Fixed Direction

Optimize the current `codex/revolution` bottlenecks first. A fresh `main`
comparison can be added as extra evidence, but it is not required before the
next implementation step.

Current evidence points to these primary targets:

| Target                                                | Current evidence                                                                                                                   | Step 9 direction                                                                                                        |
| ----------------------------------------------------- | ---------------------------------------------------------------------------------------------------------------------------------- | ----------------------------------------------------------------------------------------------------------------------- |
| `semanticTokens/full` and `semanticTokens/full/delta` | Large sample full/delta is about 9.7s/9.6s; huge sample full is about 37-38s. Delta is still effectively as expensive as full.     | Add a reusable token index and unchanged-range/hash reuse before returning full or delta results.                       |
| Remaining parse-derived feature work                  | `asp-ide` still has feature handlers that call `parse_asp` directly after the existing typed IR work.                              | Move shared derived state into tracked `VirtualDocuments`, `DocumentSummary`, `VbSymbols`, and `VbDiagnostics` queries. |
| Include invalidation                                  | Workspace include traversal exists, but dependent-root invalidation and graph fingerprints are not explicit evidence surfaces yet. | Add a workspace registry fingerprint plus include reverse edges and affected-root counts.                               |
| Disk query snapshots                                  | Persisted snapshots currently prove workspace diagnostics only.                                                                    | Extend the snapshot envelope with document summaries, include summaries, and graph fingerprints.                        |
| Sidecar cache invalidation                            | Sidecar generation counters and telemetry exist, but project fingerprints are not explicit.                                        | Invalidate JS/TS/HTML/CSS project state from a stable project fingerprint, then report hit/miss reasons.                |
| Background scheduling                                 | Background analysis warms workspace diagnostics, but priorities and affected-only scheduling are not yet proven.                   | Prioritize open files, affected roots, and include-heavy files without blocking foreground requests.                    |

## Step Execution Order

Each Step must close with investigation, implementation or evidence update,
targeted tests or benchmarks, the full gate appropriate for the touched
surface, commit, push, `git fetch origin`, and clean/sync verification.

1. **Step 9A: Baseline and Query Audit**
   - Record the current direct `parse_asp` callers, existing tracked queries,
     disk snapshot payloads, sidecar generation paths, and background-analysis
     telemetry.
   - Re-run short current-branch benchmarks for large, huge, include-tree,
     embedded, and Rust stdio document-change surfaces.
   - Acceptance: the repo has a current Step 9A baseline table and a precise
     list of handlers to move in Step 9B.

2. **Step 9B: Typed `VirtualDocuments` and `DocumentSummary` Queries**
   - Add typed tracked query layers for virtual documents and document
     summaries derived from the parsed ASP input.
   - Move feature handlers that can share this state without changing LSP
     payloads.
   - Acceptance: parity tests for hover, definition, document links,
     formatting, embedded remap, `.inc`, and UTF-16 ranges still pass.

3. **Step 9C: Workspace Registry and Include Dependency Graph**
   - Add a workspace registry generation or fingerprint input and reverse
     include edges.
   - Report affected document counts for `.inc` changes through debug telemetry.
   - Acceptance: an include edit invalidates only affected roots in tests or
     benchmark evidence, while open documents remain authoritative.

4. **Step 9D: Summary Snapshot Payloads**
   - Add persisted query snapshot payloads for document summaries, include
     summaries, and dependency graph fingerprints.
   - Validate snapshots by source metadata, settings key, server version, and
     workspace fingerprint.
   - Acceptance: stale snapshots are ignored, hot snapshots are observed, and
     diagnostics/definition targets do not become stale.

5. **Step 9E: Semantic Token Index and Delta**
   - Cache per-document semantic token indexes and token hashes.
   - Use edit ranges and token hashes to avoid regenerating unchanged token
     data for full/delta requests.
   - Acceptance: huge and large `semanticTokens/full/delta` benchmark results
     improve materially, and semantic-token parity tests still pass.

6. **Step 9F: Sidecar Project Fingerprint**
   - Replace coarse invalidation decisions with a fingerprint of workspace
     roots, relevant config files, and embedded-service file metadata.
   - Keep the sidecar protocol compatible while adding verbose hit/miss reason
     telemetry.
   - Acceptance: cold JavaScript semantic diagnostics and sidecar cache-reset
     behavior are measurable, with no stale JS/TS diagnostics after config or
     file changes.

7. **Step 9G: Background Scheduler Hardening**
   - Prioritize open files, affected roots, and include-heavy documents.
   - Bound background work so foreground LSP requests are not delayed.
   - Acceptance: post-background workspace diagnostics remains in the warm-cache
     band, and the benchmark logs prove the intended priority order.

## Worktree And Subagent Use

- The main thread is the only place that stages, commits, pushes, and fetches.
- Read-only subagents should audit Step 9A query surfaces, benchmark output, and
  Node/Rust parity risks.
- Separate worktrees are appropriate for Step 9B, Step 9E, and Step 9F if they
  can be isolated cleanly. Merge back only after the focused Step gate passes.
- Do not let a subagent change protocol shape, package layout, or VSIX release
  assumptions without the main thread explicitly reviewing the diff.

## Gates

Use focused checks while developing, then run the full gate before closing a
Step that changes runtime behavior:

```sh
cargo fmt --all -- --check
cargo check --workspace
cargo test --workspace
git diff --check
pnpm run format:check
pnpm run typecheck
pnpm run lint
pnpm run test
pnpm run build
pnpm run package:vsix
pnpm run package:vsix:no-native
```

Performance evidence should use the short Step benchmark profile unless a Step
explicitly needs longer stress evidence:

```sh
ASP_LSP_BENCH_ITERATIONS=1 ASP_LSP_BENCH_WARMUPS=0 pnpm run benchmark:change:large
ASP_LSP_BENCH_ITERATIONS=1 ASP_LSP_BENCH_WARMUPS=0 pnpm run benchmark:change:huge
ASP_LSP_BENCH_ITERATIONS=1 ASP_LSP_BENCH_WARMUPS=0 pnpm run benchmark:large
ASP_LSP_BENCH_ITERATIONS=1 ASP_LSP_BENCH_WARMUPS=0 pnpm run benchmark:huge
ASP_LSP_BENCH_ITERATIONS=1 ASP_LSP_BENCH_WARMUPS=0 pnpm run benchmark:embedded
NODE_OPTIONS=--max-old-space-size=4096 ASP_LSP_BENCH_ITERATIONS=1 ASP_LSP_BENCH_WARMUPS=0 pnpm run benchmark:include-tree
```

## Constraints

- Preserve public LSP result shapes and capability behavior.
- Preserve UTF-16 offsets, LSP ranges, source-map remapping, and `.inc`
  fragment behavior.
- Keep Classic ASP/VBScript semantics conservative; do not add regex-only
  symbol extraction as a shortcut.
- Keep the current release assumption: native VSIX packaging is focused on
  Windows 64-bit for now, with no-native remaining a package/test surface.
