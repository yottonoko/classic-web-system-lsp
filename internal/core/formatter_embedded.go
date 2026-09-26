package core

import (
	"strconv"
	"strings"
)

func formatRegion(text string, region Region, options FormattingOptions) string {
	if region.Language == LanguageCSS {
		return formatEmbeddedRegionWithOptions(text, region, options, options.FormatCSS)
	}
	if region.Language == LanguageJavaScript || region.Language == LanguageJScript {
		return formatEmbeddedScriptRegion(text, region, options, options.FormatJavaScript)
	}
	vbOptions := vbscriptFormattingOptions(options)
	if region.Kind == RegionASPExpression {
		return aspExpressionText(formatVBLine(strings.TrimSpace(text[region.ContentStart:region.ContentEnd]), vbOptions), vbOptions)
	}
	if region.Kind == RegionASPDirective {
		return aspDirectiveText(strings.TrimSpace(text[region.ContentStart:region.ContentEnd]), vbOptions)
	}
	content := text[region.ContentStart:region.ContentEnd]
	if region.Kind == RegionServerScript {
		formatted := formatVBBlock(content, vbOptions)
		return applyRegionLinePrefix(text, region, text[region.Start:region.ContentStart]+"\n"+formatted+"\n"+strings.TrimSpace(text[region.ContentEnd:region.End]))
	}
	if !strings.ContainsAny(content, "\r\n") {
		formatted := formatVBLine(strings.TrimSpace(content), vbOptions)
		if vbOptions.ASPBlockNewline == "alwaysMultiline" {
			return applyRegionLinePrefix(text, region, "<%\n"+vbscriptBlockBaseIndent(vbOptions)+formatted+"\n%>")
		}
		return aspBlockText(formatted, vbOptions)
	}
	if options.RespectDisableRegions && strings.Contains(strings.ToLower(content), "asp-format off") {
		return applyRegionLinePrefix(text, region, "<%\n"+formatVBBlockWithDisableMarkers(content, vbOptions)+"\n%>")
	}
	return applyRegionLinePrefix(text, region, "<%\n"+formatVBBlock(content, vbOptions)+"\n%>")
}

func formatVBBlock(content string, options FormattingOptions) string {
	baseIndent := vbscriptBlockBaseIndent(options)
	lines := strings.Split(strings.Trim(content, "\r\n\t "), "\n")
	indentLevel := 0
	selectIndentStack := []int{}
	previousSignificantLine := ""
	for i, line := range lines {
		trimmed := strings.TrimSpace(strings.TrimRight(line, "\r"))
		if trimmed == "" {
			lines[i] = ""
			continue
		}
		code := codeBeforeVBComment(trimmed)
		continuesPreviousLine := previousSignificantLine != "" && isVBLineContinuation(previousSignificantLine)
		if !continuesPreviousLine {
			switch {
			case isVBEndSelectLine(code):
				if len(selectIndentStack) > 0 {
					indentLevel = selectIndentStack[len(selectIndentStack)-1]
					selectIndentStack = selectIndentStack[:len(selectIndentStack)-1]
				} else if indentLevel > 0 {
					indentLevel--
				}
			case isVBCaseLine(code):
				if len(selectIndentStack) > 0 {
					selectIndent := selectIndentStack[len(selectIndentStack)-1]
					if options.VBScriptSelectCaseIndent == "caseAligned" {
						indentLevel = selectIndent
					} else {
						indentLevel = selectIndent + 1
					}
				}
			case vbDedentsBeforeLine(code):
				if indentLevel > 0 {
					indentLevel--
				}
			}
		}
		formattedLine := formatVBLine(trimmed, options)
		lines[i] = baseIndent + strings.Repeat(indentUnit(options), indentLevel) + vbContinuationIndent(continuesPreviousLine, options) + formattedLine
		switch {
		case !continuesPreviousLine && isVBSelectLine(code):
			selectIndentStack = append(selectIndentStack, indentLevel)
			indentLevel++
		case !continuesPreviousLine && isVBCaseLine(code):
			indentLevel++
		case !continuesPreviousLine && vbIndentsAfterLine(code):
			indentLevel++
		}
		previousSignificantLine = formattedLine
	}
	if options.AlignAssignments {
		lines = alignVBAssignments(lines)
	}
	return strings.Join(lines, "\n")
}

func formatVBBlockWithDisableMarkers(content string, options FormattingOptions) string {
	baseIndent := vbscriptBlockBaseIndent(options)
	lines := strings.Split(strings.Trim(content, "\r\n\t "), "\n")
	disabled := false
	indentLevel := 0
	selectIndentStack := []int{}
	previousSignificantLine := ""
	for i, line := range lines {
		lower := strings.ToLower(line)
		if strings.Contains(lower, "asp-format off") {
			disabled = true
			lines[i] = strings.TrimRight(line, "\r")
			continue
		}
		if strings.Contains(lower, "asp-format on") {
			disabled = false
			lines[i] = strings.TrimRight(line, "\r")
			continue
		}
		if disabled {
			lines[i] = strings.TrimRight(line, "\r")
			continue
		}
		trimmed := strings.TrimSpace(strings.TrimRight(line, "\r"))
		if trimmed == "" {
			lines[i] = ""
			continue
		}
		code := codeBeforeVBComment(trimmed)
		continuesPreviousLine := previousSignificantLine != "" && isVBLineContinuation(previousSignificantLine)
		if !continuesPreviousLine {
			switch {
			case isVBEndSelectLine(code):
				if len(selectIndentStack) > 0 {
					indentLevel = selectIndentStack[len(selectIndentStack)-1]
					selectIndentStack = selectIndentStack[:len(selectIndentStack)-1]
				} else if indentLevel > 0 {
					indentLevel--
				}
			case isVBCaseLine(code):
				if len(selectIndentStack) > 0 {
					selectIndent := selectIndentStack[len(selectIndentStack)-1]
					if options.VBScriptSelectCaseIndent == "caseAligned" {
						indentLevel = selectIndent
					} else {
						indentLevel = selectIndent + 1
					}
				}
			case vbDedentsBeforeLine(code):
				if indentLevel > 0 {
					indentLevel--
				}
			}
		}
		formattedLine := formatVBLine(trimmed, options)
		lines[i] = baseIndent + strings.Repeat(indentUnit(options), indentLevel) + vbContinuationIndent(continuesPreviousLine, options) + formattedLine
		switch {
		case !continuesPreviousLine && isVBSelectLine(code):
			selectIndentStack = append(selectIndentStack, indentLevel)
			indentLevel++
		case !continuesPreviousLine && isVBCaseLine(code):
			indentLevel++
		case !continuesPreviousLine && vbIndentsAfterLine(code):
			indentLevel++
		}
		previousSignificantLine = formattedLine
	}
	if options.AlignAssignments {
		lines = alignVBAssignments(lines)
	}
	return strings.Join(lines, "\n")
}

func formatLanguageEnabled(language EmbeddedLanguage, options FormattingOptions) bool {
	if options.EmbeddedLanguageFormatting == "off" && (language == LanguageCSS || language == LanguageJavaScript || language == LanguageJScript) {
		return false
	}
	if len(options.EnabledLanguages) > 0 {
		return formatLanguageExplicitlyEnabled(language, options.EnabledLanguages)
	}
	switch language {
	case LanguageVBScript:
		return true
	case LanguageHTML:
		return options.FormatHTML != nil
	case LanguageCSS:
		return options.FormatCSS != nil
	case LanguageJavaScript, LanguageJScript:
		return options.FormatJavaScript != nil
	default:
		return false
	}
}

func formatLanguageExplicitlyEnabled(language EmbeddedLanguage, enabled []string) bool {
	for _, value := range enabled {
		switch strings.ToLower(strings.TrimSpace(value)) {
		case "vbscript":
			if language == LanguageVBScript {
				return true
			}
		case "css":
			if language == LanguageCSS {
				return true
			}
		case "javascript", "jscript":
			if language == LanguageJavaScript || language == LanguageJScript {
				return true
			}
		case "html":
			if language == LanguageHTML {
				return true
			}
		}
	}
	return false
}

func vbscriptFormattingOptions(options FormattingOptions) FormattingOptions {
	if options.VBScriptTabSize > 0 {
		options.TabSize = options.VBScriptTabSize
	}
	if options.VBScriptInsertSpaces != nil {
		options.InsertSpaces = *options.VBScriptInsertSpaces
	}
	return options
}

func formatEmbeddedRegionWithOptions(text string, region Region, options FormattingOptions, formatter func(string, FormattingOptions) (string, error)) string {
	return formatEmbeddedRegionWithOptionsMode(text, region, options, formatter, false)
}

func formatEmbeddedRegionWithOptionsMode(text string, region Region, options FormattingOptions, formatter func(string, FormattingOptions) (string, error), forceMultiline bool) string {
	if formatter == nil || !formatLanguageEnabled(region.Language, options) {
		return text[region.Start:region.End]
	}
	content := text[region.ContentStart:region.ContentEnd]
	trimmed := trimEmbeddedContentForFormatting(content, forceMultiline)
	if hasNestedASPForFormatting(trimmed) && options.NestedASPInCSSJS == "skipRegion" {
		return text[region.Start:region.End]
	}
	input, restore := embeddedFormattingInput(trimmed, options.NestedASPInCSSJS)
	formatted, err := formatter(input, options)
	if err != nil || formatted == "" {
		return text[region.Start:region.End]
	}
	formatted = restore(formatted)
	return formattedEmbeddedRegion(text, region, formatted, options, forceMultiline)
}

func formatEmbeddedScriptRegion(text string, region Region, options FormattingOptions, formatter func(string, FormattingOptions, EmbeddedLanguage) (string, error)) string {
	return formatEmbeddedScriptRegionMode(text, region, options, formatter, false)
}

func formatEmbeddedScriptRegionMode(text string, region Region, options FormattingOptions, formatter func(string, FormattingOptions, EmbeddedLanguage) (string, error), forceMultiline bool) string {
	if formatter == nil || !formatLanguageEnabled(region.Language, options) {
		return text[region.Start:region.End]
	}
	content := text[region.ContentStart:region.ContentEnd]
	trimmed := trimEmbeddedContentForFormatting(content, forceMultiline)
	if hasNestedASPForFormatting(trimmed) && options.NestedASPInCSSJS == "skipRegion" {
		return text[region.Start:region.End]
	}
	input, restore := embeddedFormattingInput(trimmed, options.NestedASPInCSSJS)
	formatted, err := formatter(input, options, region.Language)
	if err != nil || formatted == "" {
		return text[region.Start:region.End]
	}
	formatted = restore(formatted)
	return formattedEmbeddedRegion(text, region, formatted, options, forceMultiline)
}

func embeddedFormattingInput(content, mode string) (string, func(string) string) {
	if mode == "formatAroundAsp" {
		return content, func(formatted string) string { return formatted }
	}
	return protectEmbeddedASPForFormatting(content)
}

func hasNestedASPForFormatting(content string) bool {
	open := indexASPOpen(content, 0)
	return open >= 0 && aspCloseDelimiter(content, open) >= 0
}

func protectEmbeddedASPForFormatting(content string) (string, func(string) string) {
	replacements := map[string]string{}
	var out strings.Builder
	cursor := 0
	index := 0
	for {
		open := indexASPOpen(content, cursor)
		if open < 0 {
			break
		}
		close := aspCloseDelimiter(content, open)
		if close < 0 {
			break
		}
		close += 2
		token := embeddedASPPlaceholderToken(content, index)
		index++
		out.WriteString(content[cursor:open])
		out.WriteString(token)
		replacements[token] = content[open:close]
		cursor = close
	}
	out.WriteString(content[cursor:])
	if len(replacements) == 0 {
		return content, func(formatted string) string { return formatted }
	}
	return out.String(), func(formatted string) string {
		for token, original := range replacements {
			formatted = strings.ReplaceAll(formatted, token, original)
		}
		return formatted
	}
}

func embeddedASPPlaceholderToken(content string, index int) string {
	for {
		token := "__ASP_LSP_EMBEDDED_HOLE_" + strconv.Itoa(index) + "__"
		if !strings.Contains(content, token) {
			return token
		}
		index++
	}
}

func trimEmbeddedContentForFormatting(content string, dedent bool) string {
	if !dedent {
		return strings.Trim(content, "\r\n\t ")
	}
	trimmedLines := strings.Trim(content, "\r\n")
	return strings.Trim(dedentCommonIndent(trimmedLines), "\r\n\t ")
}

func dedentCommonIndent(text string) string {
	lines := strings.Split(text, "\n")
	common := -1
	for _, line := range lines {
		trimmed := strings.TrimSpace(strings.TrimRight(line, "\r"))
		if trimmed == "" {
			continue
		}
		indent := leadingHorizontalWhitespace(line)
		if common == -1 || indent < common {
			common = indent
		}
	}
	if common <= 0 {
		return text
	}
	for i, line := range lines {
		if strings.TrimSpace(strings.TrimRight(line, "\r")) == "" {
			continue
		}
		if len(line) >= common {
			lines[i] = line[common:]
		}
	}
	return strings.Join(lines, "\n")
}

func leadingHorizontalWhitespace(text string) int {
	for i := 0; i < len(text); i++ {
		if text[i] != ' ' && text[i] != '\t' {
			return i
		}
	}
	return len(text)
}

func formattedEmbeddedRegion(text string, region Region, formatted string, options FormattingOptions, forceMultiline bool) string {
	if region.Kind == RegionASPExpression {
		return "<%= " + strings.TrimSpace(formatted) + " %>"
	}
	if region.Kind == RegionASPBlock {
		return applyRegionLinePrefix(text, region, "<%\n"+strings.TrimRight(formatted, "\r\n")+"\n%>")
	}
	if forceMultiline || embeddedTagContentHasLineBreaks(text, region) {
		bodyIndent := indentUnit(options)
		if tagIndentIgnored(region, options) {
			bodyIndent = ""
		}
		body := strings.TrimRight(formatted, "\r\n")
		if body != "" {
			body = bodyIndent + strings.ReplaceAll(body, "\n", "\n"+bodyIndent)
		}
		replacement := text[region.Start:region.ContentStart] + "\n" + body + "\n" + strings.TrimSpace(text[region.ContentEnd:region.End])
		if forceMultiline {
			return replacement
		}
		return applyRegionLinePrefix(text, region, replacement)
	}
	return text[region.Start:region.ContentStart] + formatted + text[region.ContentEnd:region.End]
}

func applyRegionLinePrefix(text string, region Region, replacement string) string {
	if !strings.Contains(replacement, "\n") {
		return replacement
	}
	prefix, ok := leadingWhitespaceBeforeOffset(text, region.Start)
	if !ok || prefix == "" {
		return replacement
	}
	return strings.ReplaceAll(replacement, "\n", "\n"+prefix)
}

func embeddedTagContentHasLineBreaks(text string, region Region) bool {
	if region.Kind != RegionStyle && region.Kind != RegionClientScript && region.Kind != RegionServerScript {
		return false
	}
	return strings.ContainsAny(text[region.ContentStart:region.ContentEnd], "\r\n")
}

func tagIndentIgnored(region Region, options FormattingOptions) bool {
	return tagIndentMode(region, options) == "ignoreTag"
}

func tagIndentMode(region Region, options FormattingOptions) string {
	switch {
	case region.Language == LanguageCSS:
		if options.CSSTagIndentMode != "" {
			return options.CSSTagIndentMode
		}
		if options.IgnoreCSSTagIndent {
			return "ignoreTag"
		}
	case region.Language == LanguageJavaScript || region.Language == LanguageJScript:
		if options.JavaScriptTagIndentMode != "" {
			return options.JavaScriptTagIndentMode
		}
		if options.IgnoreJavaScriptTagIndent {
			return "ignoreTag"
		}
	case region.Language == LanguageVBScript || isASPHole(region):
		if options.VBScriptTagIndentMode != "" {
			return options.VBScriptTagIndentMode
		}
		if options.IgnoreVBScriptTagIndent {
			return "ignoreTag"
		}
	}
	return "relativeToTag"
}
