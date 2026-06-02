# Rust Cache Architecture

This document tracks the incremental-cache migration toward a
rust-analyzer-style query graph. The goal is not Rust-language feature parity;
it is a cache architecture where document edits invalidate only the affected
inputs and derived queries.

## Step 1 Baseline

The current source of truth for Rust LSP in-process reuse is
`crates/asp-ide`. It owns the Salsa database and keeps open and indexed
documents as `SourceFile` inputs. LSP-facing JSON shapes are still preserved at
the boundary.

Step 1 makes the first query dependency explicit:

- `parse_asp` parses the ASP skeleton for one `SourceFile` and settings.
- `parser_diagnostics` now derives diagnostics from `parse_asp` instead of
  parsing through a separate `asp_analysis` entrypoint.
- `include_refs` now derives include references from `parse_asp` instead of
  parsing through a separate `asp_analysis` entrypoint.

This keeps the existing `.asp/.asa/.inc` guard and result shapes, while giving
parser diagnostics and include traversal a shared parse query.

## Step 2 Typed ASP IR And Include Edges

Step 2 adds the first internal typed IR layer while preserving the public JSON
boundary:

- `AspFileIr` derives from `parse_asp` and owns parser diagnostics plus typed
  `IncludeRef` values.
- `include_edges` derives per-file include edges from `AspFileIr` and resolves
  each target URI once for traversal.
- `include_closure`, document links, and include CodeLens now read direct
  include data through the typed include queries instead of each re-reading
  `parsed["includes"]`.

This is still a per-file query graph, not a full workspace include DAG. Editing
an included `.inc` invalidates that file's include edge query, while unchanged
root parse data can stay reusable. A later Step can add a workspace registry
input and dependent-root counters once the server exposes query telemetry.

## Step 3 Workspace Include Graph

Step 3 widens include traversal from open buffers to the workspace registry:

- `include_closure` now builds an internal include graph from open documents
  plus indexed workspace documents.
- Open documents take precedence over indexed snapshots for the same URI, so
  unsaved editor text remains authoritative.
- The graph still returns the original raw include JSON objects; internal edge
  targets and registry metadata are not exposed through the public LSP-facing
  shape.

This makes transitive `.inc` dependencies visible after workspace indexing, even
when only the root ASP file is open. The next incrementality step is to add an
explicit workspace registry generation/fingerprint and query counters for
dependent-root invalidation.

## Step 4 Embedded Sidecar Generation

Step 4 wires the existing sidecar `projectGeneration` protocol field into the
Rust server:

- Rust no longer sends a fixed `0` generation for embedded sidecar requests.
- Workspace roots, workspace file operations, watched file changes, relevant
  settings changes, and process-cache clears bump the sidecar project
  generation.
- The Node sidecar already clears its TypeScript project/file-system caches
  when `projectGeneration` changes, so external JavaScript/config changes no
  longer need an ASP document reopen to avoid stale project state.

The request wire shape is unchanged: `projectGeneration` stays camelCase and no
new LSP-visible fields are added. A later Step can add explicit project
fingerprints or query counters for sidecar cache hit/miss evidence.

## Step 5 Disk Query Snapshots

Step 5 changes the Rust disk cache from a diagnostics-only persisted entry into
a versioned query snapshot envelope:

- The workspace diagnostics cache now writes a `workspaceDiagnostics` query
  snapshot payload.
- Snapshot validation still checks source metadata, namespace, settings key,
  tool version, TTL, and cache format version before reuse.
- `clearProcessCache` preserves disk snapshot identity; `clearDiskCache` and
  `clearCache` still remove persisted snapshots.
- Watched changes for `.asp`, `.asa`, and `.inc` refresh the workspace index so
  unopened-file snapshots do not outlive changed source text.

This Step still stores the LSP diagnostics payload as JSON values. Later query
snapshot Steps can add include-summary, document-summary, or graph fingerprints
without duplicating the disk cache plumbing.

## Step 6 Sidecar Cache Telemetry

Step 6 adds explicit cache evidence for the embedded Node sidecar without
changing LSP result payloads:

- The sidecar response can include optional `cacheStats` counters for
  generation resets plus file/directory read hit and miss counts.
- Rust accepts the optional wire field and emits verbose `window/logMessage`
  events such as `sidecarCache.readFile.hit` and
  `sidecarCache.generationReset` only when `debug.output` is `verbose`.
- The document-change benchmark collects those sidecar events alongside the
  existing disk-cache, builder, and background-analysis debug counters.

This keeps the sidecar as the HTML/CSS/JavaScript service boundary while making
its process-local cache behavior measurable during large and embedded
benchmarks.

## Step 7 Performance Hardening And Benchmark Evidence

Step 7 adds performance proof and targeted hardening around the highest-cost
cache surfaces:

- Rust semantic-token generation now builds per-request lookup/index structures
  for symbols and UTF-16 line positions instead of repeatedly scanning the same
  document and workspace symbol lists.
- Background analysis can warm indexed unopened files into the disk snapshot
  cache, and emits `backgroundAnalysis.started` /
  `backgroundAnalysis.completed` debug events.
- Workspace indexing now reads `workspace.maxIndexFiles`, matching the VS Code
  `aspLsp.workspace.maxIndexFiles` setting and defaulting to 5000 indexed files.
- The include-tree benchmark is bounded by source count and source bytes before
  reading file contents, so short evidence runs complete without Node heap
  exhaustion while still allowing larger stress runs through
  `ASP_LSP_BENCH_MAX_FILES` and `ASP_LSP_BENCH_MAX_BYTES`.
- Benchmark evidence covers large, huge, embedded, include-tree, hot/cold cache
  modes, Rust stdio workspace cache behavior, semantic-token full/delta/range,
  and Node worker latency.

The detailed evidence is recorded in `docs/rust-performance-step7.md`. The
remaining high-cost areas, especially huge-sample `semanticTokens/full` and
cold JavaScript semantic diagnostics, are future optimization targets rather
than Step 7 blockers.

## Step 8 Cutover And Package Audit

Step 8 audits the VS Code cutover from the old Node language-server runtime to
the Rust stdio server plus embedded sidecar:

- The extension resolves `aspLsp.server.path`, then a bundled
  `server/bin/<platform>-<arch>/asp-lsp-server`, then the development
  `target/release/asp-lsp-server`.
- Backend status handling accepts the Rust backend shape only; the old
  TypeScript fallback status labels and language-server runtime dependency are
  absent.
- Native, no-native, and `win32-x64` VSIX layouts are covered by package tests.
  The no-native VSIX includes the sidecar and requires `aspLsp.server.path` for
  an external Rust server.
- The Rust server honors `workspace.maxIndexFiles`, so the extension's
  workspace indexing setting is not left as a TypeScript-era no-op.
- The release workflow is intentionally limited to the `win32-x64` native VSIX
  asset for now.

The detailed evidence is recorded in `docs/rust-cutover-step8.md`.

## Step 9P rust-analyzer-style Speed Plan

Step 9P fixes the execution plan for the next cache-architecture work before
changing runtime behavior. The detailed plan is recorded in
`docs/rust-analyzer-speed-step9.md`.

- The primary optimization target is the current `codex/revolution` bottleneck
  set, not a fresh `main` comparison: huge-document semantic tokens, remaining
  direct parse-derived feature work, include dependency invalidation, expanded
  disk query snapshots, and sidecar project fingerprints.
- The first implementation step after this plan is Step 9A, which must lock the
  typed-query and benchmark baseline before Step 9B starts moving feature
  handlers onto new query layers.
- Public LSP result shapes, UTF-16 ranges, embedded source-map remapping, `.inc`
  fragment behavior, and VSIX cutover semantics remain compatibility
  constraints for every Step 9 change.

## Step 9A Baseline And Query Audit

Step 9A records the current query graph and benchmark baseline for the next
implementation Steps. No runtime behavior changes are included in this Step.

- `asp-ide` already has tracked inputs for `SourceFile` and
  `WorkspaceSettings`, plus tracked parse, typed ASP IR, include edge,
  diagnostics, and VB symbol/diagnostic queries.
- Several high-level feature handlers still consume `parse_asp` directly. The
  first Step 9B implementation slice should move shared parsed derivatives into
  typed `VirtualDocuments` and `DocumentSummary` queries while preserving public
  wrappers and result shapes.
- The server disk cache still persists only the `workspaceDiagnostics` query
  payload. Step 9D must extend the same envelope for summary and graph
  snapshots instead of adding a separate cache format.
- The sidecar still invalidates by generation counter. Step 9F must add a
  stable project fingerprint and verbose hit/miss reason evidence without
  changing the LSP-visible payloads.
- Current short benchmarks continue to identify Rust semantic tokens and cold
  JavaScript semantic diagnostics as the largest latency surfaces.

The detailed baseline is recorded in `docs/rust-analyzer-speed-step9.md`.

## Step 9B Typed Virtual Documents And Document Summary

Step 9B adds the first typed shared summary layer for the remaining
parse-derived feature handlers:

- `DocumentSummary` is a tracked query derived from `parse_asp`.
- `virtual_documents` is a tracked query derived from `DocumentSummary`, source
  text, and source URI.
- Feature handlers that previously parsed directly now consume
  `DocumentSummary`; embedded virtual document routing consumes
  `virtual_documents`.
- The public `Ide::parse_asp` wrapper remains as a compatibility/test surface,
  but LSP feature handlers no longer call the tracked parse root directly.
- A focused regression test proves virtual documents and VB context update after
  document edits.

The detailed implementation evidence is recorded in
`docs/rust-analyzer-speed-step9.md`.

## Step 9C Workspace Registry And Include Impact

Step 9C adds explicit workspace registry and include-impact evidence:

- `WorkspaceRegistry` is a Salsa input carrying a fingerprint of the effective
  workspace document set. Open documents remain authoritative over indexed
  snapshots when the fingerprint is computed.
- `Ide::include_impact_for_change` builds reverse include edges and reports the
  transitive documents plus root ASP files affected by a changed include.
- `workspace/didChangeWatchedFiles` reports verbose
  `includeGraph.affected` telemetry for `.inc` changes before refreshing the
  workspace index, so the previous graph can identify dependent roots.
- Unit tests cover transitive reverse-edge affected roots, workspace URI
  filtering, and telemetry message shape.

The detailed implementation evidence is recorded in
`docs/rust-analyzer-speed-step9.md`.

## Current Cache Layers

- Salsa in `asp-ide`: open/indexed document inputs plus tracked parse,
  typed ASP file IR, document summary, virtual document, workspace registry
  fingerprint, include edge, reverse include impact, workspace include graph
  traversal, diagnostics, include, and VB queries.
- Process caches in `asp-analysis`: compatibility caches for parsed JSON,
  symbols, diagnostics, and serialized results.
- Server caches in `asp-lsp-server`: semantic-token result cache, disk
  query snapshot cache, sidecar project generation, and background-analysis
  warmup.
- Embedded sidecar caches: TypeScript project/file reads and HTML/CSS/JS
  document/service caches, invalidated by request `projectGeneration`.

## Next Steps

1. Execute Step 9D from `docs/rust-analyzer-speed-step9.md` by extending disk
   snapshots with document/include summaries and graph fingerprints.
2. Move more summary payloads onto typed tracked queries as Step 9D needs
   them.
3. Add explicit sidecar project fingerprints beyond generation counters.
4. Add include-summary/document-summary snapshot payloads and dependency graph
   fingerprints.
5. Continue optimizing huge-sample `semanticTokens/full` and cold JavaScript
   semantic diagnostics if more performance work is prioritized after cutover.
