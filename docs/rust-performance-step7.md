# Rust Performance Step 7 Evidence

Step 7A benchmark and hardening notes for `codex/revolution`.

## Scope

This report records current evidence for the Rust stdio server and benchmark
harness. Measurements were run on 2026-06-02 with short evidence settings:

```sh
ASP_LSP_BENCH_ITERATIONS=1
ASP_LSP_BENCH_WARMUPS=0
```

Treat these numbers as a regression triage baseline, not a statistically stable
release benchmark. Step 7B should rerun higher-iteration measurements after the
hot spots below are addressed.

## Harness Change

`scripts/benchmark-document-change.mjs` now treats workspace cache/background
debug-log waits as optional short waits through
`ASP_LSP_BENCH_OPTIONAL_LOG_WAIT_MS` (default: 1000 ms).

Reason: `workspace/diagnostic` can return successfully even when the expected
debug log is absent. Before this change, the benchmark failed after the main LSP
latency table while waiting for `window/logMessage`. Required waits for final
document diagnostics are still strict.

## Results

### `benchmark:change:large`

Command:

```sh
ASP_LSP_BENCH_ITERATIONS=1 \
ASP_LSP_BENCH_WARMUPS=0 \
ASP_LSP_BENCH_CHANGE_KIND=replace \
ASP_LSP_BENCH_CHANGE_MODE=default \
ASP_LSP_BENCH_BACKGROUND=both \
ASP_LSP_BENCH_DEBUG_STEPS=1 \
pnpm run benchmark:change:large
```

Sample:

- Files: 20
- Lines: 50,150
- Bytes: 1,771,616
- Cache mode: hot

Key Rust LSP latency:

| Scenario       | first diagnostics | final diagnostics | semanticTokens/full | semanticTokens/range | didChange final diagnostics |
| -------------- | ----------------- | ----------------- | ------------------- | -------------------- | --------------------------- |
| background off | 26.94 ms          | 1575.98 ms        | 15139.90 ms         | 7817.06 ms           | 1398.17 ms                  |
| background on  | 13.05 ms          | 1570.17 ms        | 15356.38 ms         | 7708.63 ms           | 1386.86 ms                  |

Workspace cache table:

| Scenario       | Metric                                | elapsed ms | diagnostics | disk hits | disk misses | disk writes | background starts | background completes |
| -------------- | ------------------------------------- | ---------- | ----------- | --------- | ----------- | ----------- | ----------------- | -------------------- |
| background off | cold workspace diagnostics            | 320.83     | 0           | 0         | 1           | 1           | 0                 | 0                    |
| background off | warm workspace diagnostics            | 53.10      | 0           | 1         | 0           | 0           | 0                 | 0                    |
| background on  | background warmup                     | 1002.83    | 0           | 0         | 0           | 0           | 0                 | 0                    |
| background on  | post-background workspace diagnostics | 1382.03    | 0           | 0         | 20          | 20          | 0                 | 0                    |

Interpretation:

- Disk cache write/hit is observable for workspace diagnostics.
- `workspace.backgroundAnalysis` is not proving background warmup in the Rust
  server: no background start/complete events were observed, and
  post-background workspace diagnostics still wrote 20 cache entries.
- `semanticTokens/full` is the largest measured open-file latency by a wide
  margin.

### `benchmark:large`

Command:

```sh
ASP_LSP_BENCH_ITERATIONS=1 \
ASP_LSP_BENCH_WARMUPS=0 \
ASP_LSP_BENCH_WORKERS=2 \
pnpm run benchmark:large
```

Key results:

| Operation                     | elapsed ms |
| ----------------------------- | ---------- |
| parseAspDocument              | 187.74     |
| buildVirtualDocuments         | 9.87       |
| collectVbscriptSymbols        | 51.91      |
| analyzeVbscript               | 39.97      |
| htmlDiagnostics               | 10.81      |
| cssDiagnostics                | 68.30      |
| javascriptSemanticDiagnostics | 1479.70    |

### `benchmark:embedded`

Command:

```sh
ASP_LSP_BENCH_ITERATIONS=1 \
ASP_LSP_BENCH_WARMUPS=0 \
ASP_LSP_BENCH_CONCURRENCY=2 \
pnpm run benchmark:embedded
```

Key results:

| Operation                     | elapsed ms |
| ----------------------------- | ---------- |
| htmlVirtualDocument           | 55.22      |
| cssVirtualDocument            | 1.14       |
| javascriptVirtualDocument     | 0.71       |
| htmlDiagnostics               | 15.48      |
| cssDiagnostics                | 65.42      |
| javascriptSemanticDiagnostics | 2119.43    |
| javascriptUnusedDiagnostics   | 126.14     |

### `benchmark:huge`

Command:

```sh
ASP_LSP_BENCH_ITERATIONS=1 \
ASP_LSP_BENCH_WARMUPS=0 \
pnpm run benchmark:huge
```

Key results:

| Operation                     | elapsed ms |
| ----------------------------- | ---------- |
| parseAspDocument              | 96.30      |
| buildVirtualDocuments         | 19.39      |
| collectVbscriptSymbols        | 122.00     |
| analyzeVbscript               | 126.63     |
| htmlDiagnostics               | 24.73      |
| cssDiagnostics                | 105.07     |
| javascriptSemanticDiagnostics | 5027.45    |
| javascriptUnusedDiagnostics   | 274.28     |

### `benchmark:include-tree`

Commands:

```sh
ASP_LSP_BENCH_ITERATIONS=1 \
ASP_LSP_BENCH_WARMUPS=0 \
ASP_LSP_BENCH_CONCURRENCY=2 \
pnpm run benchmark:include-tree

NODE_OPTIONS=--max-old-space-size=8192 \
ASP_LSP_BENCH_ITERATIONS=1 \
ASP_LSP_BENCH_WARMUPS=0 \
ASP_LSP_BENCH_CONCURRENCY=2 \
pnpm run benchmark:include-tree
```

Result:

- Default Node heap: failed with JavaScript heap out of memory near 4 GB.
- 8 GB Node heap: failed with JavaScript heap out of memory near 8 GB.

Interpretation: include-tree benchmark is not currently usable as a short Step 7
proof on this checkout. Step 7B should reduce the harness memory footprint or
split the workload before relying on it for regression proof.

## Audit Findings

- There is no Step 7 benchmark report before this file.
- `benchmark:change` is a Rust stdio server benchmark and prints `Server: rust`.
  The script does not expose an in-script TypeScript backend switch.
- Core and embedded benchmarks call `packages/core/dist/index.js` from Node.
  Use them as the Node-side baseline surface, then compare with Rust stdio LSP
  latency using the same sample, cache mode, warmups, and iterations.
- `semanticTokens/full` already has stdio parity coverage, but the large sample
  still shows it as the top Rust LSP latency target.
- `workspace.backgroundAnalysis` is exposed in VS Code settings and benchmark
  inputs, but Rust server background processing was not observed in source
  search or benchmark events.
- VS Code has an `aspLsp.workspace.maxIndexFiles` setting, while Rust workspace
  indexing currently stops at a fixed 512 files. This may affect include-tree and
  cutover parity.

## Step 7B Queue

1. Implement or explicitly remove/disable Rust `workspace.backgroundAnalysis`
   behavior so benchmark output does not imply a warmup that never occurs.
2. Make include-tree benchmark memory-bounded enough to run under a documented
   heap limit.
3. Add a stronger Node/Rust comparison report after the above fixes, using
   stable iterations and both hot/cold cache modes.

## Step 7B Semantic Tokens

The first Step 7B hardening pass optimized Rust semantic token generation by
building reusable lookup/index structures during one request:

- symbol lookup is indexed by lower-cased name, declaration range, class, and
  member owner/name instead of scanning all workspace symbols for each
  identifier;
- UTF-16 offset to LSP position conversion now uses a per-document line index
  for semantic token ranges instead of rescanning the document for each token.

Command:

```sh
ASP_LSP_BENCH_ITERATIONS=1 \
ASP_LSP_BENCH_WARMUPS=0 \
ASP_LSP_BENCH_CHANGE_KIND=replace \
ASP_LSP_BENCH_CHANGE_MODE=default \
ASP_LSP_BENCH_BACKGROUND=off \
ASP_LSP_BENCH_DEBUG_STEPS=1 \
pnpm run benchmark:change:large
```

Result:

| Metric                         | Step 7A ms | Step 7B ms | Change    |
| ------------------------------ | ---------- | ---------- | --------- |
| post-open semanticTokens/full  | 15139.90   | 9251.38    | -38.9%    |
| post-open semanticTokens/range | 7817.06    | 3048.13    | -61.0%    |
| didChange->finalDiagnostics    | 1398.17    | 1375.70    | unchanged |

The remaining `semanticTokens/full` cost is still high enough to keep future
optimization open, but Step 7B has a concrete performance win without changing
the LSP result shape.
