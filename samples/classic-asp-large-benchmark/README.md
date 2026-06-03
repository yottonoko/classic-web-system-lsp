# Large Classic ASP Benchmark Sample

This directory contains a script-generated Classic ASP sample for checking parser,
include resolution, diagnostics, folding, symbols, and editor latency under
heavier input.

Only the generator and this README are intended to be tracked. The generated
`.asp` and `.inc` files are ignored by git and should be created only when the
benchmark sample is needed locally.

The include chain is four layers deep:

```text
default.asp
`-- includes/layer1.inc
    `-- includes/layer2.inc
        `-- includes/layer3.inc
            `-- includes/layer4.inc
```

Each large ASP or include file also directly includes three generated leaf helper
fragments under `includes/generated/`.

Each ASP or include file is generated to roughly 10,000 physical lines. The
content is intentionally repetitive and deterministic so benchmark runs compare
against stable input without external services, filesystem access, databases, or
credentials.

Regenerate the sample:

```sh
cd samples/classic-asp-large-benchmark
node generate.mjs
```

Run the benchmark from the repository root:

```sh
pnpm run benchmark:large
```

The benchmark rebuilds `@asp-lsp/core`, regenerates this sample, and measures
parsing, virtual document construction, VBScript symbol collection, and VBScript
diagnostics analysis over the generated files. The default run uses one warmup
and five measured iterations. Override those counts when needed:

```sh
ASP_LSP_BENCH_WARMUPS=2 ASP_LSP_BENCH_ITERATIONS=10 pnpm run benchmark:large
```

Measure end-to-end editor update latency through the LSP server after one
character insert, delete, and replace edits:

```sh
pnpm run benchmark:change
pnpm run benchmark:change:large
```

Useful filters:

```sh
ASP_LSP_BENCH_CHANGE_KIND=replace pnpm run benchmark:change:large
ASP_LSP_BENCH_DEBUG_STEPS=1 pnpm run benchmark:change:large
```

Useful checks:

```sh
wc -l default.asp includes/*.inc
rg -n "#include" default.asp includes/*.inc includes/generated/*.inc
```

Open this directory in VS Code and inspect `default.asp` to exercise the full
include chain.
