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

## Current Cache Layers

- Salsa in `asp-ide`: open/indexed document inputs plus tracked parse,
  diagnostics, include, and VB queries.
- Process caches in `asp-analysis`: compatibility caches for parsed JSON,
  symbols, diagnostics, and serialized results.
- Server caches in `asp-lsp-server`: semantic-token result cache, disk
  diagnostics cache, and background-analysis warmup.
- Embedded sidecar caches: TypeScript project/file reads and HTML/CSS/JS
  document/service caches.

## Next Steps

1. Move more `asp-ide` derived work onto typed tracked queries:
   `VirtualDocuments`, `DocumentSummary`, `VbSymbols`, and `VbDiagnostics`.
2. Introduce a workspace file registry and include graph query, then invalidate
   only dependent documents on `.inc` changes.
3. Add generation/fingerprint keys to embedded sidecar requests so TypeScript
   project caches cannot become stale.
4. Expand disk cache from diagnostics-only payloads to versioned query
   snapshots.
5. Add query hit/miss benchmark evidence for large, huge, include-tree, and
   embedded workloads.
