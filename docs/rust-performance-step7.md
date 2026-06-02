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
release benchmark. Later Step 7B sections record the follow-up hardening and
comparison runs that address the initial weak points.

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
- In this initial Step 7A run, `workspace.backgroundAnalysis` did not yet prove
  background warmup: no background start/complete events were observed, and
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

Interpretation: during the initial Step 7A run, include-tree was not usable as a
short proof on this checkout. Step 7B later reduced the harness memory footprint
with bounded source selection before relying on it for regression proof.

## Step 7A Audit Findings

These findings describe the initial Step 7A state. Step 7B closure evidence is
recorded in the sections below.

- There is no Step 7 benchmark report before this file.
- `benchmark:change` is a Rust stdio server benchmark and prints `Server: rust`.
  The script does not expose an in-script TypeScript backend switch.
- Core and embedded benchmarks call `packages/core/dist/index.js` from Node.
  Use them as the Node-side baseline surface, then compare with Rust stdio LSP
  latency using the same sample, cache mode, warmups, and iterations.
- `semanticTokens/full` already has stdio parity coverage, but the large sample
  still shows it as the top Rust LSP latency target.
- At Step 7A time, `workspace.backgroundAnalysis` was exposed in VS Code
  settings and benchmark inputs, but Rust server background processing was not
  observed in source search or benchmark events.
- VS Code has an `aspLsp.workspace.maxIndexFiles` setting. Rust workspace
  indexing now reads that setting, with the same default of 5000 files, so
  cutover indexing capacity matches the extension configuration.

## Step 7B Queue

Step 7B now has evidence for semantic-token hardening, Rust background-analysis
disk-cache warmup, a bounded include-tree harness, and Node/Rust hot/cold
comparison. The remaining performance items are not Step 7B blockers:

1. `semanticTokens/full` remains the largest Rust stdio latency on the huge
   sample.
2. JavaScript semantic diagnostics remain the largest Node-side embedded
   operation on cold large/huge/include-tree samples.

The benchmark harness now measures `semanticTokens/full/delta` separately from
`semanticTokens/full`, and the Rust stdio debug event list no longer includes
worker event names that the Rust server does not emit. Worker latency evidence
is recorded by the Node large benchmark worker pool instead.

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

## Step 7B Background Analysis

Rust `workspace.backgroundAnalysis` now schedules indexed unopened workspace
files for idle analysis and disk-cache warmup when the setting is enabled. The
queue emits `backgroundAnalysis.started` and `backgroundAnalysis.completed`
debug events, and processes at least one file per idle tick. If
`workspace.idleAnalysisConcurrency` is set above zero, that value controls the
per-tick batch size.

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

Workspace cache result:

| Scenario       | Metric                                | elapsed ms | disk hits | disk misses | disk writes | background starts | background completes |
| -------------- | ------------------------------------- | ---------- | --------- | ----------- | ----------- | ----------------- | -------------------- |
| background off | cold workspace diagnostics            | 314.43     | 0         | 1           | 1           | 0                 | 0                    |
| background off | warm workspace diagnostics            | 53.00      | 1         | 0           | 0           | 0                 | 0                    |
| background on  | background warmup                     | 268.19     | 0         | 20          | 20          | 1                 | 1                    |
| background on  | post-background workspace diagnostics | 53.04      | 1         | 0           | 0           | 0                 | 0                    |

Interpretation:

- Background analysis now warms the disk cache before explicit
  `workspace/diagnostic` requests.
- The post-background workspace diagnostics path is back to the warm-cache
  latency band.
  The benchmark stops counting after the first expected cache-hit log, so the
  table proves the hit path is reached rather than enumerating every file hit.

## Step 7B Include Tree Bounded Harness

`benchmark:include-tree` now bounds the selected include-tree sources before
loading file text. The default evidence profile caps the run at 64 files and
4 MiB of source text; both values can be raised with
`ASP_LSP_BENCH_MAX_FILES` and `ASP_LSP_BENCH_MAX_BYTES`. Embedded benchmark
caches are also released between batches, including disposal of TypeScript
language services, so JavaScript diagnostics do not retain services for every
processed include file.

Default bounded command:

```sh
NODE_OPTIONS=--max-old-space-size=4096 \
ASP_LSP_BENCH_ITERATIONS=1 \
ASP_LSP_BENCH_WARMUPS=0 \
ASP_LSP_BENCH_CONCURRENCY=2 \
pnpm run benchmark:include-tree
```

Default bounded result:

| Metric     | Value                               |
| ---------- | ----------------------------------- |
| Files      | 58 of 3906                          |
| Lines      | 116,000                             |
| Bytes      | 4,169,810                           |
| Heap limit | 4096 MiB                            |
| Result     | completed                           |
| Slowest op | 4918.59 ms, JS semantic diagnostics |

Expanded bounded command:

```sh
NODE_OPTIONS=--max-old-space-size=4096 \
ASP_LSP_BENCH_ITERATIONS=1 \
ASP_LSP_BENCH_WARMUPS=0 \
ASP_LSP_BENCH_CONCURRENCY=2 \
ASP_LSP_BENCH_MAX_FILES=256 \
ASP_LSP_BENCH_MAX_BYTES=16777216 \
pnpm run benchmark:include-tree
```

Expanded bounded result:

| Metric     | Value                                |
| ---------- | ------------------------------------ |
| Files      | 229 of 3906                          |
| Lines      | 458,000                              |
| Bytes      | 16,768,185                           |
| Heap limit | 4096 MiB                             |
| Result     | completed                            |
| Slowest op | 18999.05 ms, JS semantic diagnostics |

Interpretation:

- The include-tree benchmark is now usable as a short Step 7 proof instead of
  failing with Node heap exhaustion before results are printed.
- The full generated tree remains available through higher
  `ASP_LSP_BENCH_MAX_*` values when a longer stress run is desired.

## Step 7B Node/Rust Hot-Cold Comparison

These runs use two measured iterations and one warmup:

```sh
ASP_LSP_BENCH_ITERATIONS=2
ASP_LSP_BENCH_WARMUPS=1
```

Node-side commands:

```sh
ASP_LSP_BENCH_CACHE_MODE=hot  ASP_LSP_BENCH_WORKERS=2 pnpm run benchmark:large
ASP_LSP_BENCH_CACHE_MODE=cold ASP_LSP_BENCH_WORKERS=2 pnpm run benchmark:large
ASP_LSP_BENCH_CACHE_MODE=hot  pnpm run benchmark:huge
ASP_LSP_BENCH_CACHE_MODE=cold pnpm run benchmark:huge
ASP_LSP_BENCH_CACHE_MODE=hot  ASP_LSP_BENCH_CONCURRENCY=2 pnpm run benchmark:embedded
ASP_LSP_BENCH_CACHE_MODE=cold ASP_LSP_BENCH_CONCURRENCY=2 pnpm run benchmark:embedded
NODE_OPTIONS=--max-old-space-size=4096 \
  ASP_LSP_BENCH_CACHE_MODE=hot \
  ASP_LSP_BENCH_CONCURRENCY=2 \
  pnpm run benchmark:include-tree
NODE_OPTIONS=--max-old-space-size=4096 \
  ASP_LSP_BENCH_CACHE_MODE=cold \
  ASP_LSP_BENCH_CONCURRENCY=2 \
  pnpm run benchmark:include-tree
```

Rust stdio commands:

```sh
ASP_LSP_BENCH_CHANGE_KIND=replace \
ASP_LSP_BENCH_CHANGE_MODE=default \
ASP_LSP_BENCH_BACKGROUND=off \
ASP_LSP_BENCH_DEBUG_STEPS=1 \
ASP_LSP_BENCH_CACHE_MODE=hot \
pnpm run benchmark:change:large

ASP_LSP_BENCH_CHANGE_KIND=replace \
ASP_LSP_BENCH_CHANGE_MODE=default \
ASP_LSP_BENCH_BACKGROUND=off \
ASP_LSP_BENCH_DEBUG_STEPS=1 \
ASP_LSP_BENCH_CACHE_MODE=cold \
pnpm run benchmark:change:large

ASP_LSP_BENCH_CHANGE_KIND=replace \
ASP_LSP_BENCH_CHANGE_MODE=default \
ASP_LSP_BENCH_BACKGROUND=off \
ASP_LSP_BENCH_DEBUG_STEPS=1 \
ASP_LSP_BENCH_CACHE_MODE=hot \
pnpm run benchmark:change:huge

ASP_LSP_BENCH_CHANGE_KIND=replace \
ASP_LSP_BENCH_CHANGE_MODE=default \
ASP_LSP_BENCH_BACKGROUND=off \
ASP_LSP_BENCH_DEBUG_STEPS=1 \
ASP_LSP_BENCH_CACHE_MODE=cold \
pnpm run benchmark:change:huge
```

Node baseline summary:

| Surface                | Cache | Files | Lines   | Slowest measured operation    | mean ms |
| ---------------------- | ----- | ----- | ------- | ----------------------------- | ------- |
| benchmark:large        | hot   | 20    | 50,150  | buildVirtualDocuments         | 1.91    |
| benchmark:large        | cold  | 20    | 50,150  | javascriptSemanticDiagnostics | 1368.98 |
| benchmark:huge         | hot   | 55    | 100,500 | htmlVirtualDocument           | 0.04    |
| benchmark:huge         | cold  | 55    | 100,500 | javascriptSemanticDiagnostics | 5118.55 |
| benchmark:embedded     | hot   | 20    | 50,150  | htmlVirtualDocument           | 0.03    |
| benchmark:embedded     | cold  | 20    | 50,150  | javascriptSemanticDiagnostics | 2117.33 |
| benchmark:include-tree | hot   | 58    | 116,000 | javascriptSemanticDiagnostics | 4719.22 |
| benchmark:include-tree | cold  | 58    | 116,000 | javascriptSemanticDiagnostics | 4782.59 |

Rust stdio summary:

| Sample | Cache | Files | Lines   | didOpen final diagnostics mean ms | semanticTokens/full mean ms | semanticTokens/range mean ms | didChange final diagnostics mean ms |
| ------ | ----- | ----- | ------- | --------------------------------- | --------------------------- | ---------------------------- | ----------------------------------- |
| large  | hot   | 20    | 50,150  | 1573.02                           | 9605.02                     | 3176.42                      | 1366.11                             |
| large  | cold  | 20    | 50,150  | 1576.48                           | 9769.56                     | 3105.34                      | 1376.19                             |
| huge   | hot   | 55    | 100,500 | 4552.73                           | 37618.47                    | 12084.80                     | 4337.50                             |
| huge   | cold  | 55    | 100,500 | 4530.94                           | 38039.63                    | 12201.31                     | 4356.72                             |

Workspace cache summary from Rust stdio runs:

| Sample | Cache | cold workspace diagnostics ms | warm workspace diagnostics ms |
| ------ | ----- | ----------------------------- | ----------------------------- |
| large  | hot   | 314.70                        | 52.01                         |
| large  | cold  | 318.85                        | n/a                           |
| huge   | hot   | 634.54                        | 54.34                         |
| huge   | cold  | 634.03                        | n/a                           |

Interpretation:

- Hot/cold cache mode changes Node baseline parse/analyze operations strongly,
  but Rust open-document semantic-token and final-diagnostics latency is mostly
  independent of disk-cache mode.
- Rust workspace diagnostics still proves disk-cache behavior: hot mode returns
  to roughly 52-54 ms after the initial cache write on both large and huge
  samples.
- The next performance optimization target, if Step 8 does not take priority,
  should remain Rust `semanticTokens/full` for large/huge open documents, with
  JavaScript semantic diagnostics as the Node-side embedded baseline bottleneck.

## Step 7B Harness Coverage Follow-up

This follow-up closes two weak evidence points from the Step 7 audit:

- `scripts/benchmark-document-change.mjs` now measures
  `post-open semanticTokens/full/delta` after the initial full semantic-token
  request returns a `resultId`.
- `scripts/benchmark-large.mjs` now prints a worker latency summary when
  `ASP_LSP_BENCH_DEBUG_STEPS=1`, including payload bytes, worker run duration,
  round-trip latency, and overhead.

Rust stdio command:

```sh
ASP_LSP_BENCH_ITERATIONS=1 \
ASP_LSP_BENCH_WARMUPS=0 \
ASP_LSP_BENCH_CHANGE_KIND=replace \
ASP_LSP_BENCH_CHANGE_MODE=default \
ASP_LSP_BENCH_BACKGROUND=off \
ASP_LSP_BENCH_DEBUG_STEPS=1 \
pnpm run benchmark:change:large
```

Rust stdio result:

| Metric                                | elapsed ms |
| ------------------------------------- | ---------- |
| post-open semanticTokens/full         | 9698.44    |
| post-open semanticTokens/full/delta   | 9562.41    |
| post-open semanticTokens/range        | 3045.11    |
| didChange->finalDiagnostics           | 1374.20    |
| warm workspace diagnostics, hot cache | 52.99      |

Interpretation: delta is now visible in the harness and is still effectively as
expensive as full generation for this unchanged-document sample. That is
measurement evidence, not a performance win.

Node worker command:

```sh
ASP_LSP_BENCH_ITERATIONS=1 \
ASP_LSP_BENCH_WARMUPS=0 \
ASP_LSP_BENCH_WORKERS=2 \
ASP_LSP_BENCH_DEBUG_STEPS=1 \
pnpm run benchmark:large
```

Worker latency sample:

| Operation                     | calls | payload mean bytes | run mean ms | round-trip mean ms | overhead mean ms |
| ----------------------------- | ----- | ------------------ | ----------- | ------------------ | ---------------- |
| parseAspDocument              | 20    | 94376              | 3.83        | 155.03             | 151.20           |
| javascriptSemanticDiagnostics | 20    | 94407              | 126.10      | 1169.30            | 1043.19          |
| javascriptUnusedDiagnostics   | 20    | 94405              | 7.22        | 66.38              | 59.16            |

Interpretation: worker latency is now an explicit benchmark surface. The
largest observed worker overhead remains JavaScript semantic diagnostics on the
large sample.
