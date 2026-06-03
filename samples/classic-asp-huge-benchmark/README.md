# Huge Classic ASP Benchmark Sample

This directory contains a script-generated Classic ASP sample for checking parser,
include resolution, diagnostics, folding, symbols, and editor latency under very
heavy input.

Only the generator and this README are intended to be tracked. The generated
`.asp` and `.inc` files are ignored by git and should be created only when the
benchmark sample is needed locally.

The include stack has five large files:

```text
default.asp
`-- includes/layer1.inc
    `-- includes/layer2.inc
        `-- includes/layer3.inc
            `-- includes/layer4.inc
```

Each large ASP or include file also directly includes ten generated leaf helper
fragments under `includes/generated/`.

Each large ASP or include file is generated to roughly 20,000 physical lines.
The content is intentionally repetitive and deterministic so benchmark runs
compare against stable input without external services, filesystem access,
databases, or credentials.

Regenerate the sample:

```sh
cd samples/classic-asp-huge-benchmark
node generate.mjs
```

Run the benchmark from the repository root:

```sh
pnpm run benchmark:huge
```

The benchmark rebuilds `@asp-lsp/core`, regenerates this sample, and measures
parsing, virtual document construction, VBScript symbol collection, and VBScript
diagnostics analysis over the generated files. The default run uses one warmup
and five measured iterations. Override those counts when needed:

```sh
ASP_LSP_BENCH_WARMUPS=2 ASP_LSP_BENCH_ITERATIONS=10 pnpm run benchmark:huge
```

Measure end-to-end editor update latency through the LSP server on the huge
sample:

```sh
pnpm run benchmark:change:huge
```

Start with a narrow scenario when comparing interactive changes:

```sh
ASP_LSP_BENCH_ITERATIONS=1 ASP_LSP_BENCH_WARMUPS=1 ASP_LSP_BENCH_CHANGE_KIND=replace ASP_LSP_BENCH_CHANGE_MODE=default pnpm run benchmark:change:huge
```

Useful checks:

```sh
wc -l default.asp includes/*.inc
rg -n "#include" default.asp includes/*.inc includes/generated/*.inc
```

Open this directory in VS Code and inspect `default.asp` to exercise the full
include stack.
