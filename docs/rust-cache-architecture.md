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

## Current Cache Layers

- Salsa in `asp-ide`: open/indexed document inputs plus tracked parse,
  typed ASP file IR, include edge, workspace include graph traversal,
  diagnostics, include, and VB queries.
- Process caches in `asp-analysis`: compatibility caches for parsed JSON,
  symbols, diagnostics, and serialized results.
- Server caches in `asp-lsp-server`: semantic-token result cache, disk
  query snapshot cache, sidecar project generation, and background-analysis
  warmup.
- Embedded sidecar caches: TypeScript project/file reads and HTML/CSS/JS
  document/service caches, invalidated by request `projectGeneration`.

## Next Steps

1. Move more `asp-ide` derived work onto typed tracked queries:
   `VirtualDocuments`, `DocumentSummary`, `VbSymbols`, and `VbDiagnostics`.
2. Add a workspace registry generation/fingerprint input and report dependent
   document counts on `.inc` changes.
3. Add explicit sidecar project fingerprints and cache hit/miss debug counters.
4. Add include-summary/document-summary snapshot payloads and dependency graph
   fingerprints.
5. Add query hit/miss benchmark evidence for large, huge, include-tree, and
   embedded workloads.
