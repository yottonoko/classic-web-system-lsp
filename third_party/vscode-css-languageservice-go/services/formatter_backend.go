package services

import beautify "github.com/yottonoko/js-beautify-go"

// CSSBeautifier is the narrow adapter point for js-beautify's CSS beautifier.
// Format keeps VS Code range handling; implementations should match
// js-beautify CSS output for the provided options.
type CSSBeautifier interface {
	BeautifyCSS(input string, options CSSBeautifierOptions) string
}

// CSSBeautifierOptions mirrors the js-beautify CSS option names in Go form.
// Keep this type small and stable so the external Go js-beautify package only
// needs one adapter.
type CSSBeautifierOptions struct {
	IndentSize                   int
	IndentChar                   string
	EndWithNewline               bool
	SelectorSeparatorNewline     bool
	NewlineBetweenRules          bool
	SpaceAroundSelectorSeparator bool
	BraceStyle                   string
	PreserveNewlines             bool
	MaxPreserveNewlines          int
	WrapLineLength               int
	IndentEmptyLines             bool
	EOL                          string
	BaseIndentLevel              int
}

var defaultCSSBeautifier CSSBeautifier = jsBeautifyCSSBeautifier{}

// CSSBeautifierOptionsFromFormatOptions converts the public formatter options
// to the js-beautify-shaped backend contract.
func CSSBeautifierOptionsFromFormatOptions(options FormatOptions, baseIndent int) CSSBeautifierOptions {
	options = normalizeFormatOptions(options)
	return CSSBeautifierOptions{
		IndentSize:                   options.TabSize,
		IndentChar:                   indentChar(options),
		EndWithNewline:               options.InsertFinalNewline,
		SelectorSeparatorNewline:     options.NewlineBetweenSelectors,
		NewlineBetweenRules:          options.NewlineBetweenRules,
		SpaceAroundSelectorSeparator: options.SpaceAroundSelectorSeparator,
		BraceStyle:                   options.BraceStyle,
		PreserveNewlines:             options.PreserveNewLines,
		MaxPreserveNewlines:          options.MaxPreserveNewLines,
		WrapLineLength:               options.WrapLineLength,
		IndentEmptyLines:             options.IndentEmptyLines,
		EOL:                          "\n",
		BaseIndentLevel:              baseIndent,
	}
}

func indentChar(options FormatOptions) string {
	if options.InsertSpaces {
		return " "
	}
	return "\t"
}

// BeautifyCSS preserves the existing standalone entry point while routing
// through the backend adapter.
func BeautifyCSS(input string, options FormatOptions, baseIndent int) string {
	return defaultCSSBeautifier.BeautifyCSS(input, CSSBeautifierOptionsFromFormatOptions(options, baseIndent))
}

type jsBeautifyCSSBeautifier struct{}

func (jsBeautifyCSSBeautifier) BeautifyCSS(input string, options CSSBeautifierOptions) string {
	result, err := beautify.CSS(input, cssBeautifierOptionsMap(options))
	if err != nil {
		return input
	}
	return result
}

func cssBeautifierOptionsMap(options CSSBeautifierOptions) beautify.Options {
	mapped := beautify.Options{
		"indent_size":                     options.IndentSize,
		"indent_char":                     options.IndentChar,
		"end_with_newline":                options.EndWithNewline,
		"selector_separator_newline":      options.SelectorSeparatorNewline,
		"newline_between_rules":           options.NewlineBetweenRules,
		"space_around_selector_separator": options.SpaceAroundSelectorSeparator,
		"space_around_combinator":         options.SpaceAroundSelectorSeparator,
		"brace_style":                     options.BraceStyle,
		"preserve_newlines":               options.PreserveNewlines,
		"indent_empty_lines":              options.IndentEmptyLines,
	}
	if options.IndentChar == "\t" {
		mapped["indent_with_tabs"] = true
	}
	if options.MaxPreserveNewlines > 0 {
		mapped["max_preserve_newlines"] = options.MaxPreserveNewlines
	}
	if options.WrapLineLength > 0 {
		mapped["wrap_line_length"] = options.WrapLineLength
	}
	if options.EOL != "" {
		mapped["eol"] = options.EOL
	}
	if options.BaseIndentLevel > 0 {
		mapped["indent_level"] = options.BaseIndentLevel
	}
	return mapped
}
