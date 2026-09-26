# VBScript diagnostic allocation follow-up

Baseline: `a6bdc139f3214d45be4933cf415d62c72a7c6bad`.
Measured on macOS arm64 / Apple M4 with Go 1.26.6.

## Changes

- Compact each region's temporary token array in place, then share one document
  token array between regions and statements. Keep synthetic island boundaries
  and comments that invalidate explicit line continuations.
- Allocate separate statement tokens only when joining adjacent `If` / `Then`
  islands. Restrict slice capacities to protect neighboring tokens from append.
- Account for retained array capacity, runtime headers, and separately allocated
  merged tokens. Shared storage has one memory-owner identity.
- Reuse the source line index in parser diagnostics, call-syntax fixes, and
  procedure-scope analysis.

The capacity regression test failed before the fix: it reported 96,040 bytes
for token/region storage retaining at least 192,192 bytes. The corrected test
walks backing addresses, including spare capacity, to avoid charging shared
array views twice.

## Measurements

Each version used a separately compiled test binary. Five pairs ran in
alternating order, in separate processes with `GOMAXPROCS=1` and `-test.cpu=1`.
Values below are medians. Cold LSP diagnostics used `-test.benchtime=10x`;
the remaining benchmarks used `-test.benchtime=300ms`. Profiling and other
verification commands were excluded from these measurement runs.

| Benchmark                                |    Before |     After | Time change | Before B/op | After B/op |
| ---------------------------------------- | --------: | --------: | ----------: | ----------: | ---------: |
| Cold VBScript diagnostics, one island    |  5.119 ms |  3.106 ms |      -39.3% |   4,652,605 |  2,602,192 |
| Cold VBScript diagnostics, 1,000 islands |  8.596 ms |  3.669 ms |      -57.3% |   7,893,269 |  2,780,455 |
| Cold LSP open, complete diagnostics      | 85.753 ms | 80.931 ms |       -5.6% |  39,156,882 | 32,382,561 |
| Cold typed-member completion             |  4.798 ms |  4.970 ms |       +3.6% |   2,820,667 |  2,817,530 |
| Large symbol/type analysis               | 12.365 ms | 12.082 ms |       -2.3% |   4,714,475 |  4,714,482 |

The first syntax publication within cold LSP open improved from 13.399 ms to
8.845 ms (-34.0%). Cold VBScript diagnostic allocation counts changed from
5,050 to 3,047 per operation for one island, and 8,083 to 5,065 for 1,000 islands.

The existing `compareReports` regression policies passed for typed-member
completion and large analysis against this baseline. These comparisons use the
immediately preceding implementation, not the runner's default Preview 20 ref.

## Validation target

The nine changed Go source/test files have aggregate SHA-256
`98ac3308f2b1e9d717d59f60d15836667e7b2230968532fb26042e1f41f1ebf3`.
The digest concatenates each sorted repository-relative path, a NUL byte, its
contents, and another NUL byte. The benchmark source is included.

## Verification

- `pnpm run test`: passed, including all root Go packages, eight benchmark
  runner tests, 116 extension tests, and the parity ledger (2,039 Go tests).
- `pnpm run typecheck`, `pnpm run lint`, and `pnpm run format:check`: passed.
- `go test -race ./internal/core ./internal/vbscript ./internal/lspserver`
  and `go vet ./...`: passed.
- Focused lexical tests cover empty/comment-only islands, invalid continuations,
  unterminated strings, adjacent `If` / `Then`, immutable shared slices, and
  concurrent cache initialization. Existing incremental and UTF-16 tests passed.
- A source-only snapshot built the server with `-trimpath -buildvcs=false`
  and `-ldflags="-s -w -buildid="`, with external module retrieval disabled.
  Public dependencies were already in the Go module cache.
- All three bundled private-origin Go modules passed their complete test suites
  from that snapshot with an empty module cache and external retrieval disabled.
  The bundled TypeScript Go ASP adapter tests also passed. Its first cold build
  exceeded a 180-second process timeout; the retry completed successfully.
- The snapshot-built server passed JSON-RPC initialization, definition lookup,
  a Unicode-containing incremental edit with updated definition coordinates,
  shutdown, and exit.

Review and QA were performed in the current agent. Source and license files for
the three private-origin dependencies were already tracked in this repository;
the accompanying module changes make standalone CSS/HTML tests resolve the
bundled formatter, and document the source-archive workflow.

Verdict: passed, with no unresolved findings in the changed scope.

## Line-index allocation follow-up

The next follow-up reserves exact line-index storage before scanning characters.
LF, CR, and CRLF retain their existing semantics, including a trailing empty
line. UTF-16 mapping and the cancellable construction path are unchanged.
The regression test compares mixed-newline and Unicode indexes with the
cancellable implementation and limits a 5,000-line index to two allocations.

The baseline binary was compiled from `35515c6b83a6533e0cb63325b008794580fb2932`
plus the shared-script navigation and responsive-pane changes, before editing
`internal/core/document.go`. Both binaries used the same sources except for
line-index allocation and its regression test. Five pairs ran in alternating
order, with separate processes, `GOMAXPROCS=1`, `-test.cpu=1`, and 15 cold-open
iterations per sample. No test or build ran during measurement.

| Cold-open measurement | Before median | After median | Change |
| --- | ---: | ---: | ---: |
| Complete operation | 52.395 ms | 50.122 ms | -4.3% |
| First syntax publication | 6.327 ms | 5.249 ms | -17.0% |
| Final diagnostic publication | 51.829 ms | 49.623 ms | -4.3% |
| Allocated bytes per operation | 32,370,723 | 29,854,460 | -7.8% |
| Allocations per operation | 218,142 | 217,565 | -0.3% |

These numbers describe this synthetic cold-open fixture, not all LSP requests.
