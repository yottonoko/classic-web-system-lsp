# Go js-beautify Integration

This repository keeps the VS Code CSS formatter facade in `services.Format`.
That function owns LSP range expansion, range-to-edit conversion, and the
existing public `FormatOptions` shape.

The CSS beautifier itself is behind `services.CSSBeautifier` in
`services/formatter_backend.go`. `defaultCSSBeautifier` now uses
`github.com/yottonoko/js-beautify-go`; only the VS Code range wrapper remains in
this repository.

```go
type CSSBeautifier interface {
	BeautifyCSS(input string, options CSSBeautifierOptions) string
}
```

`CSSBeautifierOptions` intentionally mirrors js-beautify CSS options in Go
field names:

- `IndentSize` -> `indent_size`
- `IndentChar` -> `indent_char`
- `EndWithNewline` -> `end_with_newline`
- `SelectorSeparatorNewline` -> `selector_separator_newline`
- `NewlineBetweenRules` -> `newline_between_rules`
- `SpaceAroundSelectorSeparator` -> `space_around_selector_separator`
- `BraceStyle` -> `brace_style`
- `PreserveNewlines` -> `preserve_newlines`
- `MaxPreserveNewlines` -> `max_preserve_newlines`
- `WrapLineLength` -> `wrap_line_length`
- `IndentEmptyLines` -> `indent_empty_lines`
- `EOL` -> `eol`
- `BaseIndentLevel` -> `indent_level`

The adapter converts these fields to a fresh `beautify.Options` map for every
format call. That keeps formatter calls independent and safe to use from
multiple goroutines as long as callers do not mutate their own input document
while formatting.

Keep parity tests focused on byte-for-byte output against
`github.com/yottonoko/js-beautify-go`, which carries the upstream js-beautify CSS
corpus. Do not add local CSS formatting rules here; fix the Go js-beautify port
when beautifier behavior diverges.
