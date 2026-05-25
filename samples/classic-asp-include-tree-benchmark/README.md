# Include Tree Classic ASP Benchmark Sample

This directory contains a script-generated Classic ASP sample for checking parser,
virtual document, symbol, and diagnostics costs over a broad include graph.

Only the generator and this README are intended to be tracked. The generated
`.asp` and `.inc` files are ignored by git and should be created only when the
benchmark sample is needed locally.

The generated include tree uses five branches at each level for five include
levels:

```text
default.asp
`-- includes/level-01/*.inc
    `-- includes/level-02/*.inc
        `-- includes/level-03/*.inc
            `-- includes/level-04/*.inc
                `-- includes/level-05/*.inc
```

This creates 3,905 include files, 3,905 include directives, and 3,906 generated
files including `default.asp`. Each generated file has roughly 2,000 physical
lines, so the full sample is roughly 7.8 million lines.

Regenerate the sample:

```sh
cd samples/classic-asp-include-tree-benchmark
node generate.mjs
```

Run the benchmark from the repository root:

```sh
pnpm run benchmark:include-tree
```

The benchmark rebuilds `@asp-lsp/core`, regenerates this sample, and measures
parsing, virtual document construction, VBScript symbol collection, and VBScript
diagnostics analysis over the generated files. The default run uses one warmup
and five measured iterations. Override those counts when needed:

```sh
ASP_LSP_BENCH_WARMUPS=1 ASP_LSP_BENCH_ITERATIONS=1 pnpm run benchmark:include-tree
```

Useful checks:

```sh
find . -type f \( -name "*.asp" -o -name "*.inc" \) | wc -l
rg -n "#include" default.asp includes | wc -l
wc -l default.asp includes/level-01/node-01.inc includes/level-05/node-01-01-01-01-01.inc
```

Open this directory in VS Code and inspect `default.asp` to exercise the full
include tree.
