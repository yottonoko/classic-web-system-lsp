package core

import "strings"

func indentUnit(options FormattingOptions) string {
	if !options.InsertSpaces {
		return "\t"
	}
	size := options.TabSize
	if size <= 0 {
		size = 2
	}
	return strings.Repeat(" ", size)
}

func finalizeFormattedText(formatted, original string, options FormattingOptions) string {
	formatted = removeWhitespaceFromBlankLines(formatted)
	formatted = applyFormattedEndOfLine(formatted, original, options.EndOfLine)
	if options.InsertFinalNewline && len(formatted) > 0 && !strings.HasSuffix(formatted, "\n") {
		formatted += "\n"
	}
	return formatted
}

func finalizeFormattedRangeText(formatted, original string, options FormattingOptions) string {
	formatted = removeWhitespaceFromBlankLines(formatted)
	return applyFormattedEndOfLine(formatted, original, options.EndOfLine)
}

func removeWhitespaceFromBlankLines(text string) string {
	lines := strings.Split(normalizeLineEndings(text), "\n")
	for index, line := range lines {
		if strings.Trim(line, " \t") == "" {
			lines[index] = ""
		}
	}
	return strings.Join(lines, "\n")
}

func applyFormattedEndOfLine(formatted, original, endOfLine string) string {
	switch endOfLine {
	case "lf":
		return normalizeLineEndings(formatted)
	case "crlf":
		return strings.ReplaceAll(normalizeLineEndings(formatted), "\n", "\r\n")
	default:
		if strings.Contains(original, "\r\n") {
			return strings.ReplaceAll(normalizeLineEndings(formatted), "\n", "\r\n")
		}
		return normalizeLineEndings(formatted)
	}
}

func normalizeLineEndings(text string) string {
	text = strings.ReplaceAll(text, "\r\n", "\n")
	return strings.ReplaceAll(text, "\r", "\n")
}

func min(a, b int) int {
	if a < b {
		return a
	}
	return b
}
