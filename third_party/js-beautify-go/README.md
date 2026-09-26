This project is a Go-only fork and port of
[beautifier/js-beautify](https://github.com/beautifier/js-beautify). Deep
respect and thanks go to the original authors and contributors for creating,
maintaining, and testing js-beautify; this port builds on their formatter
design, compatibility behavior, and legacy test corpus.

# js-beautify-go

Native Go implementation of js-beautify-compatible JavaScript, CSS, and HTML
formatting.

This repository contains only the Go implementation. The upstream JavaScript,
Python, npm, PyPI, webpack, and browser UI implementations are intentionally
not included in this Go-only fork.

## Install

```bash
go install github.com/yottonoko/js-beautify-go/cmd/js-beautify@latest
go install github.com/yottonoko/js-beautify-go/cmd/css-beautify@latest
go install github.com/yottonoko/js-beautify-go/cmd/html-beautify@latest
```

## Library

```go
package main

import (
	"fmt"

	beautify "github.com/yottonoko/js-beautify-go"
)

func main() {
	out, err := beautify.JS("if(a){b();}", beautify.Options{
		"indent_size": 2,
	})
	if err != nil {
		panic(err)
	}
	fmt.Println(out)
}
```

The public entry points are:

- `beautify.JS(source, options)`
- `beautify.CSS(source, options)`
- `beautify.HTML(source, options)`

Option names may use underscores or dashes. Per-language child maps named
`js`, `css`, and `html` override parent options for that formatter.

### Public API compatibility

The upstream npm library exposes JavaScript, CSS, and HTML entry points plus
default option constructors. This Go module exposes the corresponding formatter
surface as Go functions:

| Upstream library surface | Go library surface |
| --- | --- |
| `require("js-beautify").js(source, options)` | `beautify.JS(source, options)` |
| `require("js-beautify").css(source, options)` | `beautify.CSS(source, options)` |
| `require("js-beautify").html(source, options)` | `beautify.HTML(source, options)` |
| `defaultOptions()` per language | `DefaultJSOptions`, `DefaultCSSOptions`, `DefaultHTMLOptions` |

Browser globals, AMD modules, npm packaging, and Python library APIs remain
outside the scope of this Go-only fork. The supported public Go API is the root
`github.com/yottonoko/js-beautify-go` package; `internal/...` packages are
implementation details.

## CLI

The Go CLI keeps the traditional command names:

```bash
js-beautify file.js
css-beautify file.css
html-beautify file.html
```

Common options:

```text
-f, --file       Input file(s), use - for stdin
-r, --replace    Write output in-place
-o, --outfile    Write output to file
--config         Path to .jsbeautifyrc-compatible JSON
--type           js, css, or html
-q, --quiet      Suppress status output
-h, --help       Show help
-v, --version    Show version
```

Beautifier options use the js-beautify names, for example
`--indent-size 2`, `--brace-style expand`, `--end-with-newline`, and
`--editorconfig`.

## Test

```bash
make test
make bench
```

The imported upstream compatibility corpus lives under `testdata/legacy`. The
full JavaScript, CSS, and HTML parity corpus can be run explicitly:

```bash
make legacy
```

## Status

The formatter is implemented natively in Go and does not execute the upstream
JavaScript or Python implementation. The unpackers are also Go implementations;
upstream eval-based unpacking behavior is implemented as safe decoders.

## License

This project is distributed under the MIT License. The upstream
`beautifier/js-beautify` copyright notice is preserved, and the Go port adds a
copyright notice for yottonoko and js-beautify-go contributors.
