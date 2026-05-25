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

Useful checks:

```sh
wc -l default.asp includes/*.inc
rg -n "#include" default.asp includes/*.inc includes/generated/*.inc
```

Open this directory in VS Code and inspect `default.asp` to exercise the full
include chain.
