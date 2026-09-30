package core

import "strings"

const (
	maxFormatIndentSize       = 32
	maxFormatLineLength       = 10000
	maxFormatPreservedNewLine = 100
)

// SanitizeFormattingOptions bounds client- and settings-supplied values before
// they reach the embedded formatters, which reject unknown enum values by
// failing and build indentation strings proportional to the indent size.
func SanitizeFormattingOptions(options FormattingOptions) FormattingOptions {
	for _, size := range []*int{
		&options.TabSize,
		&options.HTMLTabSize,
		&options.CSSTabSize,
		&options.JavaScriptTabSize,
		&options.JScriptTabSize,
		&options.VBScriptTabSize,
		&options.HTMLWrapAttributesIndentSize,
		&options.VBScriptLineContinuationIndentSize,
	} {
		*size = clampFormatInt(*size, maxFormatIndentSize)
	}
	for _, length := range []*int{&options.PrintWidth, &options.HTMLWrapLineLength, &options.CSSWrapLineLength} {
		*length = clampFormatInt(*length, maxFormatLineLength)
	}
	if options.MaxPreserveNewLines != nil {
		value := clampFormatInt(*options.MaxPreserveNewLines, maxFormatPreservedNewLine)
		options.MaxPreserveNewLines = &value
	}
	options.HTMLWrapAttributes = knownFormatOption(options.HTMLWrapAttributes,
		"auto", "force", "force-aligned", "force-expand-multiline", "aligned-multiple", "preserve", "preserve-aligned")
	options.CSSBraceStyle = knownFormatOption(options.CSSBraceStyle, "collapse", "expand")
	return options
}

func clampFormatInt(value int, limit int) int {
	return min(max(value, 0), limit)
}

func knownFormatOption(value string, known ...string) string {
	value = strings.ToLower(strings.TrimSpace(value))
	for _, candidate := range known {
		if value == candidate {
			return value
		}
	}
	return ""
}
