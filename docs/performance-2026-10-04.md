# Cold-open analysis follow-up

Baseline: `f20be7c4` (the parent of this change).
Measured on linux/amd64, Intel Xeon @ 2.80GHz (no SHA extensions), Go 1.26.6.

## Changes

- The workspace reference index derives segments for the manifest's structural
  clone from the revision that built the manifest. The clone had no runtime
  analysis, so it decoded the persisted JSON facts and recomputed usage,
  implicit declarations, and embedded class ranges. Segments are still owned by
  the clone.
- `vbProcedureScopesFromCST` skips the document CST when no VBScript region
  contains `Sub`, `Function`, or `Property`; such a CST has no procedure nodes.
- `didOpen` params are decoded once and reused by the handler. Object-params
  validation uses `json.Valid` instead of decoding into a map.
- A hand-written scanner replaces the member-chain regular expression. A fuzz
  test compares it with the original expression.
- The include-aware implicit assignment declarations are cached per parsed
  revision; two analyses requested them separately.
- `publicExportBoundaries` encodes each boundary once instead of in every sort
  comparison.
- Count and location segment fingerprints share one encoding of counts and
  declaration ranges. Reference name fingerprints build one payload per hash
  instead of making many small escaping writes. Hash inputs are unchanged.
- Hashes of the eight most recent large texts (4 KiB to 1 MiB) are reused.
  Several stages hash the same immutable document text.
- Identifier spans of many regions reuse one token buffer. Document tokens are
  staged and compacted in one buffer. Significant tokens are exactly sized, and
  keyword lower-casing returns shared strings.

## Measurements

Each version used a separately compiled test binary. Samples alternated
between binaries in separate processes with `GOMAXPROCS=1` and `-test.cpu=1`.
Values are medians.

| Benchmark                                         | Samples |   Before |    After | After / before |
| ------------------------------------------------- | ------: | -------: | -------: | -------------: |
| Cold LSP open, complete diagnostics (15x)         |      10 | 92.34 ms | 58.18 ms |          63.0% |
| Cold LSP open with 100 procedures (15x)           |      10 | 106.1 ms | 75.85 ms |          71.5% |
| Cold LSP open, 100 workspace files (300ms)        |       5 | 100.9 ms | 58.63 ms |          58.1% |
| didChange diagnostics equivalent (300ms)          |       5 | 44.12 ms | 31.38 ms |          71.1% |
| Large symbol/type analysis (300ms)                |       5 | 11.37 ms | 10.97 ms |          96.5% |
| Cold typed-member completion (300ms)              |       5 |  5.09 ms |  5.05 ms |          99.3% |

The procedure benchmark was a temporary variant of the cold-open fixture with
400 blocks plus 100 small functions; it is not part of the repository. With
the default `GOMAXPROCS` (4), the cold-open benchmark changed from about 82 ms
to 54 ms over six alternating samples. Allocation per cold open fell from
30.6 MB to 19.6 MB, and allocation count from 172,458 to 127,924.

SHA-256 is about 7% of the remaining cold-open CPU time on this machine. CPUs
with SHA extensions spend less time there, so their ratios can differ.

## Verification

- `go test ./...` and `go vet ./...`: passed.
- `pnpm run verify:parity` and `pnpm run test:benchmark-regression`: passed.
- Old and new reference and segment fingerprints were compared on generated
  inputs and were identical.
- `FuzzGraphMemberChainMatchesRegexpOracle` ran for 30 seconds without a
  mismatch.
