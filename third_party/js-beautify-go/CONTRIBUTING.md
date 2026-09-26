# Contributing

## Prerequisites

- Go 1.26 or newer
- make

## Build and test

```bash
make test
make bench
```

Use `gofmt` for all Go code:

```bash
gofmt -w .
```

## Project layout

- `beautify.go` exposes the public Go package API.
- `cmd/js-beautify`, `cmd/css-beautify`, and `cmd/html-beautify` contain CLI entry points.
- `internal/core` contains shared option, scanner, token, and output primitives.
- `internal/javascript`, `internal/css`, and `internal/html` contain formatter implementations.
- `internal/unpackers` contains safe Go unpacker implementations.
- `testdata/legacy` contains the imported historical test corpus as JSON.

## Compatibility work

When changing formatter behavior, add or update Go tests. For legacy parity
work, use the imported corpus in `testdata/legacy`; do not reintroduce the old
JavaScript or Python implementations.

## Release

This repository is a Go module:

```bash
go test ./...
go install ./cmd/...
```

There is no npm, PyPI, webpack, or browser release pipeline in this Go-only
tree.

