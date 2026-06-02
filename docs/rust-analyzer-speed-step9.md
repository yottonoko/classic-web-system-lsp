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

| Target                                                | Current evidence                                                                                                                   | Step 9 direction                                                                                         |
| ----------------------------------------------------- | ---------------------------------------------------------------------------------------------------------------------------------- | -------------------------------------------------------------------------------------------------------- |
| `semanticTokens/full` and `semanticTokens/full/delta` | Large sample full/delta is about 9.7s/9.6s; huge sample full is about 37-38s. Delta is still effectively as expensive as full.     | Add a reusable token index and unchanged-range/hash reuse before returning full or delta results.        |
| Remaining parse-derived feature work                  | Step 9A found direct `parse_asp` feature callers after the existing typed IR work.                                                 | Step 9B moves shared parsed state into tracked `DocumentSummary` and `VirtualDocuments` queries.         |
| Include invalidation                                  | Workspace include traversal exists, but dependent-root invalidation and graph fingerprints are not explicit evidence surfaces yet. | Step 9C adds a workspace registry fingerprint plus include reverse edges and affected-root counts.       |
| Disk query snapshots                                  | Persisted snapshots currently prove workspace diagnostics only.                                                                    | Extend the snapshot envelope with document summaries, include summaries, and graph fingerprints.         |
| Sidecar cache invalidation                            | Sidecar generation counters and telemetry exist, but project fingerprints are not explicit.                                        | Invalidate JS/TS/HTML/CSS project state from a stable project fingerprint, then report hit/miss reasons. |
| Background scheduling                                 | Background analysis warms workspace diagnostics, but priorities and affected-only scheduling are not yet proven.                   | Prioritize open files, affected roots, and include-heavy files without blocking foreground requests.     |

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

Step 9B closes this migration list except for the public wrapper itself.

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

## Step 9B Implementation

Step 9B adds the typed summary layer and moves feature handlers off direct
tracked parse calls without changing LSP payloads.

Implementation changes:

- `DocumentSummary` is a tracked query derived from `parse_asp` and currently
  owns the parsed ASP skeleton.
- `virtual_documents` is a tracked query derived from `DocumentSummary`, source
  URI, and source text.
- `MappedVirtualDocument`, `VirtualDocument`, and `SourceMapSegment` now support
  equality so tracked virtual document results can be compared and reused.
- References, rename, semantic tokens, selection ranges, inlay hints, code
  actions, call hierarchy, inline values, formatting, and `vb_context` now read
  parsed state through `DocumentSummary`.
- Embedded virtual document routing now reads through the tracked
  `virtual_documents` query.
- The public `Ide::parse_asp` wrapper still calls the parse root directly so
  existing parse-boundary tests and compatibility callers remain intact.

Focused evidence:

```sh
cargo fmt --all -- --check
cargo test -p asp-ide
cargo check --workspace
ASP_LSP_BENCH_ITERATIONS=1 ASP_LSP_BENCH_WARMUPS=0 ASP_LSP_BENCH_CHANGE_KIND=replace ASP_LSP_BENCH_CHANGE_MODE=default ASP_LSP_BENCH_BACKGROUND=off ASP_LSP_BENCH_DEBUG_STEPS=1 pnpm run benchmark:change:large
ASP_LSP_BENCH_ITERATIONS=1 ASP_LSP_BENCH_WARMUPS=0 ASP_LSP_BENCH_CONCURRENCY=2 pnpm run benchmark:embedded
```

The new `updates_step_nine_typed_summary_and_virtual_documents_after_edit`
regression proves both the virtual document query and the VB context summary
update after document text changes.

Short Step 9B benchmark result:

| Surface                  | Metric                        | Step 9A ms | Step 9B ms | Notes                                                                           |
| ------------------------ | ----------------------------- | ---------: | ---------: | ------------------------------------------------------------------------------- |
| `benchmark:change:large` | post-open completion          |     955.03 |       5.52 | Completion now benefits from shared summary/context reuse in this short run.    |
| `benchmark:change:large` | semanticTokens/full           |    9634.86 |    9677.08 | Still Step 9E-owned; summary reuse alone does not remove token generation cost. |
| `benchmark:change:large` | semanticTokens/full/delta     |    9846.08 |    9834.38 | Delta is still full-cost and remains Step 9E-owned.                             |
| `benchmark:change:large` | warm workspace diagnostics    |      53.18 |      53.14 | Disk-cache behavior remains stable.                                             |
| `benchmark:embedded`     | javascriptSemanticDiagnostics |    2136.05 |    2129.31 | Sidecar cold semantic diagnostics remains Step 9F-owned.                        |

Step 9B does not implement semantic-token index reuse. It only moves semantic
token generation onto the shared summary input; Step 9E remains responsible for
avoiding full token regeneration and making delta cheaper than full.

## Step 9C Implementation

Step 9C adds explicit workspace-registry and reverse-edge evidence for include
changes.

Implementation changes:

- `WorkspaceRegistry` is a Salsa input that stores a fingerprint of the current
  effective workspace document set.
- The fingerprint is recomputed when open documents, indexed documents, or
  open-document text change. It uses the same open-over-indexed precedence as
  workspace analysis.
- `Ide::include_impact_for_change` builds reverse include edges from current
  direct include queries, walks them transitively, and returns affected
  documents plus affected root ASP files.
- The Rust server logs verbose `includeGraph.affected` telemetry for watched
  `.inc` changes before refreshing the workspace index. The message includes
  changed URI, affected root count, affected document count, and graph
  fingerprint.

Focused evidence:

```sh
cargo fmt --all -- --check
cargo test -p asp-ide reports_step_nine_include_impact_from_reverse_edges
cargo test -p asp-lsp-server include_impact_message_reports_counts_and_fingerprint
cargo check --workspace
NODE_OPTIONS=--max-old-space-size=4096 ASP_LSP_BENCH_ITERATIONS=1 ASP_LSP_BENCH_WARMUPS=0 ASP_LSP_BENCH_CONCURRENCY=2 pnpm run benchmark:include-tree
```

The `asp-ide` regression proves transitive reverse-edge impact:
`default.asp -> shared.inc -> nested.inc` reports `default.asp` as the affected
root when `nested.inc` changes, then reports no affected roots after the open
root switches to a different include. Server unit coverage proves watched-file
URI filtering and the `includeGraph.affected` telemetry count/fingerprint
message shape.

Short include-tree benchmark result:

| Surface                  | Files |   Lines | Slowest measured operation      | Step 9A ms | Step 9C ms |
| ------------------------ | ----: | ------: | ------------------------------- | ---------: | ---------: |
| `benchmark:include-tree` |    58 | 116,000 | `javascriptSemanticDiagnostics` |    4905.89 |    4839.87 |

## Step 9D Implementation

Step 9D extends disk query snapshots without changing LSP response hydration.

Implementation changes:

- The disk snapshot format version is bumped, and persisted envelopes now carry
  `workspaceFingerprint` in addition to source metadata and settings key.
- `PersistedQuerySnapshotPayload` adds `documentSummary`, `includeSummary`, and
  `dependencyGraph` variants alongside `workspaceDiagnostics`.
- `Ide` exposes read-only snapshot helpers for the current document summary,
  direct include summary, and dependency graph fingerprint/reverse edges.
- Indexed diagnostics writes the summary and graph snapshots after diagnostics
  are computed.
- Warm diagnostics reads log verbose `diskCache.documentSummary.hit`,
  `diskCache.includeSummary.hit`, and `diskCache.dependencyGraph.hit` events
  when those snapshots are valid.

Focused evidence:

```sh
cargo fmt --all -- --check
cargo test -p asp-ide exposes_step_nine_summary_snapshots_with_graph_fingerprint
cargo test -p asp-lsp-server disk_snapshot_round_trips_summary_and_graph_payloads
cargo test -p asp-lsp-server disk_snapshot_misses_when_workspace_fingerprint_changes
cargo test -p asp-lsp-server serves_workspace_diagnostics_with_disk_cache
cargo check --workspace
```

The new snapshot tests prove all summary/graph payload variants round-trip, the
workspace fingerprint participates in staleness validation, and the existing
workspace-diagnostics stdio test observes hot summary/graph snapshot hits after
`clearProcessCache`.

Step 9D intentionally keeps persisted summaries as evidence/warm payloads. LSP
requests still use current Salsa inputs and source metadata, so stale persisted
summaries cannot answer definitions, references, diagnostics, or formatting.

## Step 9E Implementation

Step 9E adds an unchanged semantic-token delta fast path without changing the
LSP response shape.

Implementation changes:

- `Ide::semantic_tokens_fingerprint` reports a stable per-document semantic
  token fingerprint from the URI, source text hash, workspace registry
  fingerprint, and serialized settings.
- The server stores that fingerprint with each semantic-token `resultId`.
- `textDocument/semanticTokens/full/delta` first checks whether the requested
  previous result belongs to the same URI and still has the same fingerprint.
  When it does, the server returns a protocol delta from the cached token data
  instead of regenerating the full token array.
- Changed fingerprints fall back to normal semantic-token generation and delta
  edit calculation.

Focused evidence:

```sh
cargo fmt --all -- --check
cargo test -p asp-lsp-server semantic_token_delta_reuses_cached_result_for_unchanged_fingerprint
cargo check --workspace
cargo test -p asp-lsp-server serves_vbscript_read_requests_over_stdio_lsp
RUSTC_WRAPPER= ASP_LSP_BENCH_ITERATIONS=1 ASP_LSP_BENCH_WARMUPS=0 ASP_LSP_BENCH_CHANGE_KIND=replace ASP_LSP_BENCH_CHANGE_MODE=default ASP_LSP_BENCH_BACKGROUND=off ASP_LSP_BENCH_DEBUG_STEPS=1 pnpm run benchmark:change:large
RUSTC_WRAPPER= ASP_LSP_BENCH_ITERATIONS=1 ASP_LSP_BENCH_WARMUPS=0 ASP_LSP_BENCH_CHANGE_KIND=replace ASP_LSP_BENCH_CHANGE_MODE=default ASP_LSP_BENCH_BACKGROUND=off ASP_LSP_BENCH_DEBUG_STEPS=1 pnpm run benchmark:change:huge
```

The new server unit test proves unchanged fingerprints reuse the cached token
data and changed fingerprints do not. The existing stdio semantic-token test
continues to prove full/range/delta wire shape and `resultId` behavior.

Short document-change benchmark result:

| Surface                     | Metric                     | Step 9A ms | Step 9E ms | Notes                                                                                              |
| --------------------------- | -------------------------- | ---------: | ---------: | -------------------------------------------------------------------------------------------------- |
| `benchmark:change:large`    | semanticTokens/full        |    9634.86 |   11163.18 | Full token generation is still expensive and remains a later optimization target.                  |
| `benchmark:change:large`    | semanticTokens/full/delta  |    9846.08 |       0.84 | Unchanged fingerprint delta now returns from the cached previous token result.                     |
| `benchmark:change:large`    | semanticTokens/range       |    2968.71 |    3126.84 | Range requests still generate tokens for the requested range.                                      |
| `benchmark:change:huge`     | semanticTokens/full        |   37852.17 |   42787.90 | Full token generation is intentionally unchanged by this Step.                                     |
| `benchmark:change:huge`     | semanticTokens/full/delta  |   45659.18 |       1.54 | The unchanged delta path is now effectively constant-time for the huge short-run sample.           |
| `benchmark:change:huge`     | semanticTokens/range       |   11763.68 |   12229.07 | Range requests remain outside the unchanged full-delta cache.                                      |
| `workspace cache large/off` | warm workspace diagnostics |      53.18 |     532.33 | This one-iteration run rebuilt with `RUSTC_WRAPPER=` and should not be read as a cache regression. |
| `workspace cache huge/off`  | warm workspace diagnostics |      54.53 |    1059.41 | Same short-run caveat; Step 9E does not modify disk-cache lookup behavior.                         |

Step 9E optimizes unchanged full-delta requests only. It does not yet cache a
range-aware token index for `semanticTokens/full` or `semanticTokens/range`.
Those remain follow-up work after sidecar fingerprinting and background
scheduler hardening unless semantic-token full generation is prioritized again.

## Step 9F Implementation

Step 9F adds a stable sidecar project fingerprint while preserving the existing
`projectGeneration` request field.

Implementation changes:

- `EmbeddedRequest` now carries optional `projectFingerprint` and
  `projectResetReason` fields. Older sidecars can still ignore them, and the
  Node sidecar still falls back to `projectGeneration` when no fingerprint is
  present.
- The Rust server computes the fingerprint from workspace roots, sidecar-owned
  settings, indexed ASP source metadata, JS/TS project config metadata, and
  JS/TS source metadata under workspace roots.
- Explicit `clearCache` and `clearProcessCache` commands add a forced reset
  nonce to the fingerprint so process-cache clears still invalidate sidecar
  state even when project inputs are stable.
- `workspace/didChangeWatchedFiles` now refreshes the fingerprint for JS/TS and
  config changes even when the ASP workspace index is not affected.
- Verbose sidecar cache reset logs include `reason=<reason>` and
  `fingerprint=<fingerprint>`, and generic embedded requests now emit the same
  cache hit/miss telemetry as embedded diagnostics.
- `benchmark:change` prints a `Debug event details` table for sidecar reset
  messages, preserving reset reason/fingerprint evidence when a scenario
  triggers a reset.

Focused evidence:

```sh
cargo fmt --all -- --check
cargo test -p asp-sidecar-protocol
cargo test -p asp-lsp-server sidecar_project_fingerprint_tracks_project_inputs_and_forced_resets
cargo test -p asp-lsp-server sidecar_cache_reset_message_includes_reason_and_fingerprint
pnpm --filter @asp-lsp/embedded-sidecar run build
cargo test -p asp-lsp-server invalidates_embedded_sidecar_project_cache_after_watched_file_change
pnpm run typecheck
RUSTC_WRAPPER= ASP_LSP_BENCH_ITERATIONS=1 ASP_LSP_BENCH_WARMUPS=0 ASP_LSP_BENCH_CHANGE_KIND=replace ASP_LSP_BENCH_CHANGE_MODE=default ASP_LSP_BENCH_BACKGROUND=off ASP_LSP_BENCH_DEBUG_STEPS=1 ASP_LSP_BENCH_CHECK_JS=1 pnpm run benchmark:change:large
RUSTC_WRAPPER= ASP_LSP_BENCH_ITERATIONS=1 ASP_LSP_BENCH_WARMUPS=0 ASP_LSP_BENCH_CONCURRENCY=2 pnpm run benchmark:embedded
```

The stdio watched-file test proves updated `shared.js` diagnostics are observed
after a watched change and the verbose sidecar reset log reports
`reason=watchedFiles` plus a project fingerprint. The document-change benchmark
with `checkJs=on` reports sidecar file-system cache hits without a reset during
the unchanged project-input scenario:

| Surface                  | Metric / event                     | Step 9F result |
| ------------------------ | ---------------------------------- | -------------: |
| `benchmark:change:large` | sidecarCache.fileExists.hit        |              1 |
| `benchmark:change:large` | sidecarCache.readFile.hit          |              1 |
| `benchmark:change:large` | sidecarCache.directoryExists.hit   |              1 |
| `benchmark:change:large` | sidecarCache.readDirectory.hit     |              1 |
| `benchmark:change:large` | didOpen final diagnostics          |     2667.67 ms |
| `benchmark:change:large` | didChange final diagnostics        |     2113.87 ms |
| `benchmark:embedded`     | javascriptSemanticDiagnostics mean |     3143.30 ms |

`benchmark:embedded` still bypasses the Rust server and sidecar protocol, so it
is retained as a cold embedded-service performance reference rather than
fingerprint evidence.

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
