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

## Step 9A Baseline

Step 9A was run on `codex/revolution` at commit
`26dd36e87a391801abbf691b6308f5e8ad2f9bbe`, then documented in the next
commit. Measurements use one iteration and no warmup, so they are a triage
baseline rather than release-grade statistics.

### Query Surface

`crates/asp-ide` already has the following tracked layers:

| Layer                            | Current role                                                  |
| -------------------------------- | ------------------------------------------------------------- |
| `SourceFile`                     | Per-document URI and text input.                              |
| `WorkspaceSettings`              | Serialized settings input.                                    |
| `parse_asp`                      | Shared ASP skeleton parse root.                               |
| `AspFileIr`                      | Typed diagnostics and include refs derived from `parse_asp`.  |
| `parser_diagnostics`             | Parser diagnostics through `AspFileIr`.                       |
| `include_refs` / `include_edges` | Raw include refs and typed include edges through `AspFileIr`. |
| `vb_symbols` / `vb_diagnostics`  | VBScript symbol and diagnostic queries.                       |
| `document_diagnostics`           | Full document diagnostics query.                              |

The remaining direct `parse_asp` feature callers are the Step 9B-9E migration
targets:

| Feature area                     | Current direct caller shape                                                  | Next owner                                                             |
| -------------------------------- | ---------------------------------------------------------------------------- | ---------------------------------------------------------------------- |
| References and rename            | Workspace document loops parse each document before identifier range search. | Step 9B `DocumentSummary`, then include-aware invalidation in Step 9C. |
| Semantic tokens                  | Open document parses, then rebuilds token data for full/range/delta.         | Step 9E token index and delta cache.                                   |
| Selection ranges and inlay hints | Range features parse the open document and combine with VB symbols.          | Step 9B `DocumentSummary`.                                             |
| Code actions                     | Parses open document to test VBScript offsets and selected ranges.           | Step 9B `DocumentSummary`.                                             |
| Call hierarchy incoming/outgoing | Workspace or item document parses before call-site search.                   | Step 9B `DocumentSummary`, then Step 9C affected roots.                |
| Inline values                    | Parses open document and resolves identifiers against workspace symbols.     | Step 9B `DocumentSummary`.                                             |
| Formatting                       | Parses open document before Classic ASP-safe formatting.                     | Step 9B typed parsed summary, with formatting parity tests.            |
| Embedded virtual documents       | Parses open document before source-map remap and virtual document build.     | Step 9B `VirtualDocuments`.                                            |
| `vb_context`                     | Parses open document for VBScript offset guard and shared context.           | Step 9B `DocumentSummary` or typed VB context query.                   |

Keep the public `Ide::parse_asp` wrapper as a compatibility/test surface even
after feature handlers stop calling the tracked root directly.

### Cache And Server Surface

| Surface              | Current state                                                                                                                                                                                      | Next owner                                                             |
| -------------------- | -------------------------------------------------------------------------------------------------------------------------------------------------------------------------------------------------- | ---------------------------------------------------------------------- |
| Disk snapshots       | `PersistedQuerySnapshotPayload` only has `workspaceDiagnostics`. The envelope already stores format version, tool version, namespace, source metadata, settings key, TTL, and cache size controls. | Step 9D extends the existing envelope with summary and graph payloads. |
| Semantic token cache | Server stores previous encoded token arrays and computes protocol delta after regenerating the full next token array.                                                                              | Step 9E avoids regenerating unchanged token data.                      |
| Sidecar invalidation | Server sends a `u64` project generation and bumps it for settings, roots, watched files, and cache clears.                                                                                         | Step 9F adds a stable project fingerprint and reset reason telemetry.  |
| Sidecar telemetry    | Verbose diagnostics requests can emit count events such as `sidecarCache.readFile.hit` and `sidecarCache.generationReset`. Generic embedded feature requests do not yet expose the same evidence.  | Step 9F extends evidence without changing result payloads.             |
| Background analysis  | Unopened indexed files are queued and warmed with `backgroundAnalysis.started/completed`; queue order is not priority or affected-root based.                                                      | Step 9G adds priority and foreground-safety evidence.                  |

### Benchmark Baseline

Commands:

```sh
ASP_LSP_BENCH_ITERATIONS=1 ASP_LSP_BENCH_WARMUPS=0 ASP_LSP_BENCH_WORKERS=2 pnpm run benchmark:large
ASP_LSP_BENCH_ITERATIONS=1 ASP_LSP_BENCH_WARMUPS=0 pnpm run benchmark:huge
ASP_LSP_BENCH_ITERATIONS=1 ASP_LSP_BENCH_WARMUPS=0 ASP_LSP_BENCH_CONCURRENCY=2 pnpm run benchmark:embedded
NODE_OPTIONS=--max-old-space-size=4096 ASP_LSP_BENCH_ITERATIONS=1 ASP_LSP_BENCH_WARMUPS=0 ASP_LSP_BENCH_CONCURRENCY=2 pnpm run benchmark:include-tree
ASP_LSP_BENCH_ITERATIONS=1 ASP_LSP_BENCH_WARMUPS=0 ASP_LSP_BENCH_CHANGE_KIND=replace ASP_LSP_BENCH_CHANGE_MODE=default ASP_LSP_BENCH_BACKGROUND=both ASP_LSP_BENCH_DEBUG_STEPS=1 pnpm run benchmark:change:large
ASP_LSP_BENCH_ITERATIONS=1 ASP_LSP_BENCH_WARMUPS=0 ASP_LSP_BENCH_CHANGE_KIND=replace ASP_LSP_BENCH_CHANGE_MODE=default ASP_LSP_BENCH_BACKGROUND=off ASP_LSP_BENCH_DEBUG_STEPS=1 pnpm run benchmark:change:huge
```

Node/core benchmark summary:

| Surface                  | Files |   Lines | Slowest measured operation      | mean ms |
| ------------------------ | ----: | ------: | ------------------------------- | ------: |
| `benchmark:large`        |    20 |  50,150 | `javascriptSemanticDiagnostics` | 1431.49 |
| `benchmark:huge`         |    55 | 100,500 | `javascriptSemanticDiagnostics` | 4909.06 |
| `benchmark:embedded`     |    20 |  50,150 | `javascriptSemanticDiagnostics` | 2136.05 |
| `benchmark:include-tree` |    58 | 116,000 | `javascriptSemanticDiagnostics` | 4905.89 |

Rust stdio document-change summary:

| Sample | Background | didOpen final diagnostics ms | completion ms | semanticTokens/full ms | semanticTokens/full/delta ms | semanticTokens/range ms | didChange final diagnostics ms |
| ------ | ---------- | ---------------------------: | ------------: | ---------------------: | ---------------------------: | ----------------------: | -----------------------------: |
| large  | off        |                      1564.44 |        955.03 |                9634.86 |                      9846.08 |                 2968.71 |                        1380.99 |
| large  | on         |                      1576.41 |        957.07 |                9796.91 |                      9909.95 |                 2954.46 |                        1378.63 |
| huge   | off        |                      4514.92 |       3786.97 |               37852.17 |                     45659.18 |                11763.68 |                        4403.34 |

Workspace cache summary:

| Sample | Scenario       | Metric                                | elapsed ms | disk hits | disk misses | disk writes | background starts | background completes |
| ------ | -------------- | ------------------------------------- | ---------: | --------: | ----------: | ----------: | ----------------: | -------------------: |
| large  | background off | cold workspace diagnostics            |     317.24 |         0 |           1 |           1 |                 0 |                    0 |
| large  | background off | warm workspace diagnostics            |      53.18 |         1 |           0 |           0 |                 0 |                    0 |
| large  | background on  | background warmup                     |     271.10 |         0 |          20 |          20 |                 1 |                    1 |
| large  | background on  | post-background workspace diagnostics |      53.05 |         1 |           0 |           0 |                 0 |                    0 |
| huge   | background off | cold workspace diagnostics            |     627.82 |         0 |           1 |           1 |                 0 |                    0 |
| huge   | background off | warm workspace diagnostics            |      54.53 |         1 |           0 |           0 |                 0 |                    0 |

### Step 9B Entry Criteria

Start Step 9B with the smallest high-leverage slice:

1. Add tracked `VirtualDocuments` and `DocumentSummary` queries.
2. Move `embedded_virtual_documents` and `vb_context` first.
3. Then move open-document range handlers such as selection ranges, inlay
   hints, code actions, inline values, and formatting.
4. Move workspace loops for references, rename, and call hierarchy only after
   the document summary API preserves open/indexed workspace semantics.
5. Leave semantic-token index/delta behavior for Step 9E, while allowing Step
   9B to consume the same typed summary layer.

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
