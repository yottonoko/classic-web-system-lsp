# Cross-revision performance regression gate

The local performance gate compares the current checkout with the historical
Preview 20 baseline (`ea5cec7aa286e399e280f6e2200e70dc3f0b94e8`). That
commit is available only in the original development history. In a public
clone, first choose a Go-based baseline commit that is present locally and run:

```sh
pnpm run benchmark:regression -- --baseline <go-baseline-ref>
```

In a checkout containing the historical baseline, `pnpm run
benchmark:regression` uses it by default.

By default, `current` is `HEAD` plus tracked working-tree changes. To compare
an exact commit without applying the working-tree diff, pass `--current`:

```sh
pnpm run benchmark:regression -- --current 5a1d11ee7
```

The runner archives each revision into its own temporary directory, injects the
same `internal/lspserver/performance_regression_fixture_test.go`, and compiles
an explicit test binary in that directory. It never writes a benchmark binary
to the repository root and does not copy untracked files. Each sample runs the
baseline and current binaries in alternating order; five samples are collected
by default and the median is compared. Temporary directories and outputs are
removed on completion.

The fixture covers two known regression-sensitive paths:

- `BenchmarkCrossRevisionTypedMemberCompletionCold`: a 160-procedure cold
  typed-member completion request.
- `BenchmarkCrossRevisionLargeAnalyze`: VBScript symbol and type analysis over
  five deterministic 1,000-line documents, each with three fixed include
  directives.

A benchmark fails when the current median exceeds the baseline median by more
than the configured ratio and noise floor. The limits are intentionally
separate for elapsed time and allocations:

| benchmark               |         time |            B/op |   allocs/op |
| ----------------------- | -----------: | --------------: | ----------: |
| typed-member completion | 1.20x + 1 ms | 1.25x + 256 KiB | 1.25x + 500 |
| large analysis          | 1.20x + 1 ms | 1.20x + 256 KiB | 1.20x + 500 |

The sample count, benchmark duration, snapshot/compile timeout, and
per-benchmark timeout can be adjusted for local hardware with `--samples`,
`--benchtime`, `--timeout-ms`, and `--benchmark-timeout-ms`, or with
`ASP_LSP_PERF_SAMPLES`, `ASP_LSP_PERF_BENCHTIME`,
`ASP_LSP_PERF_TIMEOUT_MS`, and `ASP_LSP_PERF_BENCHMARK_TIMEOUT_MS`. Compilation
uses the host's normal parallelism; only benchmark execution is fixed to one
logical processor. Go's content-addressed build cache is shared between the
isolated snapshots so repeated compilation remains practical; benchmark
binaries and source trees remain separate. At least five samples are required
so outliers cannot determine the median. Each benchmark runs in a separate
process capped at 60 seconds. Normal completion, threshold failures, timeouts,
`SIGINT`, and `SIGTERM` all remove the isolated source trees and binaries. This
gate is intentionally local and does not add or modify GitHub Actions.
