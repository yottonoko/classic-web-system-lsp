package htmlservice

import (
	"regexp"
	"strings"
)

func Format(document *TextDocument, r *Range, options HTMLFormatConfiguration) []TextEdit {
	return FormatWithBeautifier(document, r, options, defaultHTMLBeautifier)
}

// FormatWithBeautifier formats a document with an optional js-beautify-compatible backend.
func FormatWithBeautifier(document *TextDocument, r *Range, options HTMLFormatConfiguration, beautifier HTMLBeautifier) []TextEdit {
	value := document.GetText()
	includesEnd := true
	initialIndentLevel := 0
	tabSize := options.TabSize
	if tabSize == 0 {
		tabSize = 4
	}
	var targetRange Range
	if r != nil {
		startOffset := document.OffsetAt(r.Start)
		extendedStart := startOffset
		for extendedStart > 0 && isHorizontalWhitespace(value[extendedStart-1]) {
			extendedStart--
		}
		if extendedStart == 0 || isEOL(value[extendedStart-1]) {
			startOffset = extendedStart
		} else if extendedStart < startOffset {
			startOffset = extendedStart + 1
		}
		endOffset := document.OffsetAt(r.End)
		extendedEnd := endOffset
		for extendedEnd < len(value) && isHorizontalWhitespace(value[extendedEnd]) {
			extendedEnd++
		}
		if extendedEnd == len(value) || isEOL(value[extendedEnd]) {
			endOffset = extendedEnd
		}
		targetRange = NewRange(document.PositionAt(startOffset), document.PositionAt(endOffset))
		if regexp.MustCompile(`(?s).*[<][^>]*$`).MatchString(value[:startOffset]) {
			return []TextEdit{NewTextEdit(targetRange, value[startOffset:endOffset])}
		}
		includesEnd = endOffset == len(value)
		value = value[startOffset:endOffset]
		if startOffset != 0 {
			lineStart := document.OffsetAt(NewPosition(targetRange.Start.Line, 0))
			initialIndentLevel = computeIndentLevel(document.GetText(), lineStart, tabSize)
		}
	} else {
		targetRange = NewRange(NewPosition(0, 0), document.PositionAt(len(value)))
	}
	source := trimLeftJSWhitespace(value)
	var result string
	if beautifier != nil {
		var err error
		result, err = beautifier.BeautifyHTML(source, NewBeautifyHTMLOptions(options, tabSize, includesEnd))
		if err != nil {
			panic(err)
		}
	} else {
		result = formatHTML(source, tabSize, options)
	}
	if includesEnd && options.EndWithNewline != nil && *options.EndWithNewline && !strings.HasSuffix(result, "\n") {
		result += "\n"
	}
	if initialIndentLevel > 0 {
		indentChar := " "
		indentLen := tabSize * initialIndentLevel
		if !options.InsertSpaces {
			indentChar = "\t"
			indentLen = initialIndentLevel
		}
		indent := strings.Repeat(indentChar, indentLen)
		result = strings.ReplaceAll(result, "\n", "\n"+indent)
		if targetRange.Start.Character == 0 {
			result = indent + result
		}
	}
	return []TextEdit{NewTextEdit(targetRange, result)}
}

func formatHTML(input string, tabSize int, options HTMLFormatConfiguration) string {
	cfg := newFormatConfig(options, tabSize)
	tokens := htmlFormatTokens(input)
	var lines []string
	indent := 0
	inlineOpen := false
	pendingInlineSpace := false
	var indentStack []bool
	for i := 0; i < len(tokens); i++ {
		token := tokens[i]
		trimmed := strings.TrimSpace(token)
		if trimmed == "" {
			newlines := strings.Count(token, "\n")
			if newlines == 0 && len(token) > 0 {
				pendingInlineSpace = true
			}
			if cfg.preserveNewLines {
				blankCount := newlines - 1
				if cfg.maxPreserveNewLines >= 0 && blankCount > cfg.maxPreserveNewLines {
					blankCount = cfg.maxPreserveNewLines
				}
				for j := 0; j < blankCount; j++ {
					line := ""
					if cfg.indentEmptyLines {
						line = cfg.indent(indent)
					}
					lines = append(lines, line)
				}
			}
			if newlines > 0 {
				inlineOpen = false
				pendingInlineSpace = false
			}
			continue
		}
		if strings.HasPrefix(trimmed, "</") {
			inc := true
			if len(indentStack) > 0 {
				inc = indentStack[len(indentStack)-1]
				indentStack = indentStack[:len(indentStack)-1]
			}
			if inc && indent > 0 {
				indent--
			}
			formatted := normalizeTag(trimmed, cfg, "", 0)
			if inlineOpen && len(lines) > 0 {
				lines[len(lines)-1] += formatted
				inlineOpen = false
			} else {
				lines = append(lines, cfg.indent(indent)+formatted)
			}
			continue
		}
		if strings.HasPrefix(trimmed, "<!--") {
			if pendingInlineSpace && len(lines) > 0 {
				lines[len(lines)-1] += " " + trimmed
				pendingInlineSpace = false
			} else {
				lines = append(lines, cfg.indent(indent)+trimmed)
			}
			continue
		}
		if strings.HasPrefix(trimmed, "<style") {
			open := normalizeTag(trimmed, cfg, cfg.indent(indent), indent)
			lines = appendFormattedLines(lines, cfg.indent(indent), open)
			content := ""
			if i+1 < len(tokens) {
				content = tokens[i+1]
				i++
			}
			for _, cssLine := range formatCSS(content, tabSize, options.CSS) {
				if strings.TrimSpace(cssLine) == "" {
					lines = append(lines, "")
				} else {
					lines = append(lines, cfg.indent(indent+1)+cssLine)
				}
			}
			if i+1 < len(tokens) && strings.HasPrefix(strings.TrimSpace(tokens[i+1]), "</style") {
				i++
				lines = append(lines, cfg.indent(indent)+normalizeTag(strings.TrimSpace(tokens[i]), cfg, "", indent))
			}
			inlineOpen = false
			continue
		}
		if strings.HasPrefix(trimmed, "<script") {
			lines = appendFormattedLines(lines, cfg.indent(indent), normalizeTag(trimmed, cfg, cfg.indent(indent), indent))
			spaceOnlyBody := false
			if i+1 < len(tokens) && !strings.HasPrefix(strings.TrimSpace(tokens[i+1]), "</script") {
				i++
				if body := strings.TrimSpace(tokens[i]); body != "" {
					if cfg.indentScripts == "keep" {
						lines = append(lines, strings.TrimRight(tokens[i], " \t\r\n"))
					} else if cfg.indentScripts == "separate" {
						lines = append(lines, body)
					} else {
						lines = append(lines, cfg.indent(indent+1)+body)
					}
				} else if tokens[i] != "" {
					spaceOnlyBody = true
				}
			}
			if i+1 < len(tokens) && strings.HasPrefix(strings.TrimSpace(tokens[i+1]), "</script") {
				i++
				if spaceOnlyBody {
					lines[len(lines)-1] += " "
				}
				lines[len(lines)-1] += normalizeTag(strings.TrimSpace(tokens[i]), cfg, "", indent)
			}
			continue
		}
		if strings.HasPrefix(trimmed, "<") {
			tagName := normalizedTagName(trimmed)
			if cfg.extraLiners[tagName] && len(lines) > 0 && strings.TrimSpace(lines[len(lines)-1]) != "" {
				lines = append(lines, "")
			}
			tag := normalizeTag(trimmed, cfg, cfg.indent(indent), indent)
			if pendingInlineSpace && len(lines) > 0 {
				lines[len(lines)-1] += " " + tag
				pendingInlineSpace = false
				inlineOpen = false
				continue
			}
			lines = appendFormattedLines(lines, cfg.indent(indent), tag)
			if !isSelfClosingFormatTag(tag) && !strings.HasPrefix(tag, "<!") {
				if cfg.contentUnformatted[tagName] && i+1 < len(tokens) {
					i++
					if content := strings.TrimRight(tokens[i], " \t\r\n"); content != "" {
						lines = append(lines, content)
					}
					if i+1 < len(tokens) && strings.HasPrefix(strings.TrimSpace(tokens[i+1]), "</"+tagName) {
						i++
						lines = append(lines, cfg.indent(indent)+normalizeTag(strings.TrimSpace(tokens[i]), cfg, "", indent))
					}
					inlineOpen = false
					continue
				}
				inc := !isNoIndentContainer(tag, cfg)
				indentStack = append(indentStack, inc)
				if inc {
					indent++
				}
				inlineOpen = true
			} else {
				inlineOpen = false
			}
			continue
		}
		if inlineOpen && len(lines) > 0 {
			lines[len(lines)-1] += strings.TrimSpace(token)
		} else {
			text := strings.TrimSpace(token)
			if cfg.unformattedDelimiter != "" && strings.Contains(token, cfg.unformattedDelimiter) {
				lines = append(lines, cfg.indent(indent)+strings.TrimRight(token, " \t\r\n"))
			} else if cfg.indentHandlebars && strings.Contains(text, "{{") {
				lines = append(lines, cfg.formatHandlebarsText(text, indent)...)
				inlineOpen = false
				continue
			} else {
				lines = append(lines, cfg.indent(indent)+text)
			}
		}
		inlineOpen = true
	}
	return strings.Join(lines, "\n")
}

func htmlFormatTokens(input string) []string {
	var tokens []string
	i := 0
	for i < len(input) {
		if input[i] == '<' {
			if strings.HasPrefix(input[i:], "<!--") {
				end := strings.Index(input[i+4:], "-->")
				if end < 0 {
					tokens = append(tokens, input[i:])
					break
				}
				end = i + 4 + end + 3
				tokens = append(tokens, input[i:end])
				i = end
				continue
			}
			end := strings.IndexByte(input[i:], '>')
			if end < 0 {
				tokens = append(tokens, input[i:])
				break
			}
			end = i + end + 1
			tokens = append(tokens, input[i:end])
			i = end
		} else {
			next := strings.IndexByte(input[i:], '<')
			if next < 0 {
				tokens = append(tokens, input[i:])
				break
			}
			tokens = append(tokens, input[i:i+next])
			i += next
		}
	}
	return tokens
}

var attrSpacingRE = regexp.MustCompile(`\s*=\s*`)

type formatConfig struct {
	tabSize              int
	insertSpaces         bool
	indentEmptyLines     bool
	preserveNewLines     bool
	maxPreserveNewLines  int
	indentInnerHTML      bool
	wrapLineLength       int
	wrapAttributes       string
	wrapAttrIndentSize   int
	contentUnformatted   map[string]bool
	extraLiners          map[string]bool
	indentHandlebars     bool
	indentScripts        string
	templating           map[string]bool
	unformattedDelimiter string
}

func newFormatConfig(options HTMLFormatConfiguration, tabSize int) formatConfig {
	cfg := formatConfig{
		tabSize:             tabSize,
		insertSpaces:        options.InsertSpaces,
		preserveNewLines:    true,
		maxPreserveNewLines: 32786,
		wrapLineLength:      120,
		wrapAttributes:      "auto",
		contentUnformatted:  tagSet(options.ContentUnformatted),
		extraLiners:         tagSet(options.ExtraLiners),
		indentScripts:       stringPtrValue(options.IndentScripts),
		templating:          templatingSet(options.Templating),
	}
	if options.IndentEmptyLines != nil {
		cfg.indentEmptyLines = *options.IndentEmptyLines
	}
	if options.PreserveNewLines != nil {
		cfg.preserveNewLines = *options.PreserveNewLines
	}
	if options.MaxPreserveNewLines != nil {
		cfg.maxPreserveNewLines = *options.MaxPreserveNewLines
	}
	if options.IndentInnerHTML != nil {
		cfg.indentInnerHTML = *options.IndentInnerHTML
	}
	if options.WrapLineLength != nil {
		cfg.wrapLineLength = *options.WrapLineLength
	}
	if options.WrapAttributes != nil {
		cfg.wrapAttributes = *options.WrapAttributes
	}
	if options.WrapAttributesIndentSize != nil {
		cfg.wrapAttrIndentSize = *options.WrapAttributesIndentSize
	}
	if options.IndentHandlebars != nil {
		cfg.indentHandlebars = *options.IndentHandlebars
	}
	if options.Unformatted != nil {
		for tag := range tagSet(options.Unformatted) {
			cfg.contentUnformatted[tag] = true
		}
	}
	cfg.unformattedDelimiter = options.UnformattedContentDelimiter
	return cfg
}

func (c formatConfig) indent(level int) string {
	if level <= 0 {
		return ""
	}
	if !c.insertSpaces {
		return strings.Repeat("\t", level)
	}
	return strings.Repeat(" ", level*c.tabSize)
}

func tagSet(value *string) map[string]bool {
	result := map[string]bool{}
	if value == nil {
		return result
	}
	for _, part := range strings.Split(*value, ",") {
		part = strings.ToLower(strings.TrimSpace(part))
		if part != "" {
			result[part] = true
		}
	}
	return result
}

func stringSet(values []string) map[string]bool {
	result := map[string]bool{}
	for _, value := range values {
		value = strings.ToLower(strings.TrimSpace(value))
		if value != "" {
			result[value] = true
		}
	}
	return result
}

func templatingSet(value any) map[string]bool {
	result := map[string]bool{}
	switch v := value.(type) {
	case nil:
		result["none"] = true
	case bool:
		if v {
			result["auto"] = true
		} else {
			result["none"] = true
		}
	case []string:
		result = stringSet(v)
	case string:
		result = stringSet([]string{v})
	}
	if result["auto"] {
		for _, name := range []string{"django", "erb", "handlebars", "php"} {
			result[name] = true
		}
	}
	if len(result) == 0 {
		result["none"] = true
	}
	return result
}

func (c formatConfig) formatHandlebarsText(text string, indentLevel int) []string {
	rawLines := strings.Split(text, "\n")
	level := indentLevel
	var lines []string
	for _, line := range rawLines {
		trimmed := strings.TrimSpace(line)
		if strings.HasPrefix(trimmed, "{{/") && level > indentLevel {
			level--
		}
		lines = append(lines, c.indent(level)+trimmed)
		if strings.HasPrefix(trimmed, "{{#") || strings.HasPrefix(trimmed, "{{else") {
			level++
		}
	}
	return lines
}

func appendFormattedLines(lines []string, indent, formatted string) []string {
	parts := strings.Split(formatted, "\n")
	for i, part := range parts {
		if i == 0 {
			lines = append(lines, indent+part)
		} else {
			lines = append(lines, part)
		}
	}
	return lines
}

func normalizeTag(tag string, cfg formatConfig, baseIndent string, indentLevel int) string {
	if strings.HasPrefix(tag, "<!--") || strings.HasPrefix(strings.ToUpper(tag), "<!DOCTYPE") {
		return tag
	}
	complete := strings.HasSuffix(tag, ">")
	end := len(tag)
	if complete {
		end--
	}
	inside := strings.TrimSpace(tag[1:end])
	selfClose := strings.HasSuffix(inside, "/")
	if selfClose {
		inside = strings.TrimSpace(strings.TrimSuffix(inside, "/"))
	}
	inside = attrSpacingRE.ReplaceAllString(inside, "=")
	parts := splitTagParts(inside)
	if len(parts) == 0 {
		return tag
	}
	inside = strings.Join(parts, " ")
	singleLine := "<" + inside
	if complete {
		if selfClose {
			singleLine += "/>"
		} else {
			singleLine += ">"
		}
	}
	hasExistingAttributeWrap := strings.Contains(tag, "\n")
	firstAttributeWrapped := false
	if len(parts) > 1 {
		if idx := strings.Index(tag, parts[1]); idx >= 0 {
			firstAttributeWrapped = strings.Contains(tag[:idx], "\n")
		}
	}
	if shouldWrapAttributes(cfg, baseIndent, singleLine, len(parts)-1, hasExistingAttributeWrap) {
		return wrapTagAttributes(parts, complete, selfClose, cfg, baseIndent, firstAttributeWrapped)
	}
	if !complete {
		return "<" + inside
	}
	if selfClose {
		return "<" + inside + "/>"
	}
	return "<" + inside + ">"
}

func splitTagParts(inside string) []string {
	var parts []string
	var b strings.Builder
	var quote byte
	for i := 0; i < len(inside); i++ {
		ch := inside[i]
		if quote != 0 {
			b.WriteByte(ch)
			if ch == quote {
				quote = 0
			}
			continue
		}
		if ch == '"' || ch == '\'' {
			quote = ch
			b.WriteByte(ch)
			continue
		}
		if isSpace(ch) {
			if b.Len() > 0 {
				parts = append(parts, b.String())
				b.Reset()
			}
			continue
		}
		b.WriteByte(ch)
	}
	if b.Len() > 0 {
		parts = append(parts, b.String())
	}
	return mergeAttributeParts(parts)
}

func mergeAttributeParts(parts []string) []string {
	if len(parts) < 3 {
		return parts
	}
	var merged []string
	for i := 0; i < len(parts); i++ {
		if i+2 < len(parts) && parts[i+1] == "=" {
			merged = append(merged, parts[i]+"="+parts[i+2])
			i += 2
			continue
		}
		if strings.HasSuffix(parts[i], "=") && i+1 < len(parts) {
			merged = append(merged, parts[i]+parts[i+1])
			i++
			continue
		}
		if i+1 < len(parts) && strings.HasPrefix(parts[i+1], "=") {
			merged = append(merged, parts[i]+parts[i+1])
			i++
			continue
		}
		merged = append(merged, parts[i])
	}
	return merged
}

func shouldWrapAttributes(cfg formatConfig, baseIndent, singleLine string, attrCount int, hasExistingWrap bool) bool {
	if attrCount <= 0 {
		return false
	}
	switch cfg.wrapAttributes {
	case "force", "force-aligned", "force-expand-multiline", "aligned-multiple":
		if cfg.wrapAttributes == "aligned-multiple" {
			return cfg.wrapLineLength > 0 && len(baseIndent)+len(singleLine) > cfg.wrapLineLength
		}
		return attrCount >= 2
	case "preserve", "preserve-aligned":
		return hasExistingWrap
	case "auto", "":
		return cfg.wrapLineLength > 0 && len(baseIndent)+len(singleLine) > cfg.wrapLineLength
	default:
		return false
	}
}

func wrapTagAttributes(parts []string, complete, selfClose bool, cfg formatConfig, baseIndent string, firstAttributeWrapped bool) string {
	if len(parts) <= 1 {
		return "<" + strings.Join(parts, " ")
	}
	first := "<" + parts[0]
	indentSize := cfg.wrapAttrIndentSize
	if indentSize == 0 {
		indentSize = cfg.tabSize
	}
	attrIndent := baseIndent + strings.Repeat(" ", indentSize)
	if cfg.wrapAttributes == "force-aligned" || cfg.wrapAttributes == "aligned-multiple" || cfg.wrapAttributes == "preserve-aligned" {
		attrIndent = strings.Repeat(" ", len(baseIndent)+len(first)+1)
	}
	var b strings.Builder
	b.WriteString(first)
	startAttr := 1
	if cfg.wrapAttributes != "force-expand-multiline" && !(firstAttributeWrapped && strings.HasPrefix(cfg.wrapAttributes, "preserve")) && len(parts) > 1 {
		b.WriteByte(' ')
		b.WriteString(parts[1])
		startAttr = 2
	}
	for i, attr := range parts[startAttr:] {
		b.WriteByte('\n')
		b.WriteString(attrIndent)
		b.WriteString(attr)
		if complete && cfg.wrapAttributes != "force-expand-multiline" && i == len(parts[startAttr:])-1 {
			writeTagClose(&b, selfClose)
		}
	}
	if complete {
		if cfg.wrapAttributes == "force-expand-multiline" {
			b.WriteByte('\n')
			b.WriteString(baseIndent)
			writeTagClose(&b, selfClose)
		} else if startAttr >= len(parts) {
			writeTagClose(&b, selfClose)
		}
	}
	return b.String()
}

func writeTagClose(b *strings.Builder, selfClose bool) {
	if selfClose {
		b.WriteString("/>")
	} else {
		b.WriteString(">")
	}
}

func normalizedTagName(tag string) string {
	tag = strings.TrimSpace(tag)
	if !strings.HasPrefix(tag, "<") || strings.HasPrefix(tag, "</") || strings.HasPrefix(tag, "<!") {
		return ""
	}
	i := 1
	for i < len(tag) && isSpace(tag[i]) {
		i++
	}
	start := i
	for i < len(tag) && isNameChar(tag[i]) {
		i++
	}
	return strings.ToLower(tag[start:i])
}

func isSelfClosingFormatTag(tag string) bool {
	lower := strings.ToLower(tag)
	if strings.HasSuffix(lower, "/>") {
		return true
	}
	for _, name := range []string{"br", "hr", "img", "input", "meta", "link", "source"} {
		if strings.HasPrefix(lower, "<"+name) && (len(lower) == len(name)+2 || strings.Contains(" />", string(lower[len(name)+1]))) {
			return true
		}
	}
	return false
}

func isNoIndentContainer(tag string, cfg formatConfig) bool {
	lower := strings.ToLower(tag)
	return strings.HasPrefix(lower, "<html") && !cfg.indentInnerHTML
}

func formatCSS(content string, tabSize int, options *EmbeddedCSSFormatConfiguration) []string {
	css := strings.TrimSpace(content)
	if css == "" {
		return nil
	}
	spaceAroundSelectorSeparator := true
	braceStyle := "collapse"
	preserveNewLines := true
	maxPreserveNewLines := 32786
	if options != nil {
		if options.SpaceAroundSelectorSeparator != nil {
			spaceAroundSelectorSeparator = *options.SpaceAroundSelectorSeparator
		}
		if options.BraceStyle != nil {
			braceStyle = *options.BraceStyle
		}
		if options.PreserveNewLines != nil {
			preserveNewLines = *options.PreserveNewLines
		}
		if options.MaxPreserveNewLines != nil {
			maxPreserveNewLines = *options.MaxPreserveNewLines
		}
	}
	if !preserveNewLines {
		css = regexp.MustCompile(`\n\s*\n+`).ReplaceAllString(css, "\n")
	} else if maxPreserveNewLines >= 0 {
		css = limitBlankLines(css, maxPreserveNewLines)
	}
	if spaceAroundSelectorSeparator {
		css = regexp.MustCompile(`\s*>\s*`).ReplaceAllString(css, " > ")
	}
	css = regexp.MustCompile(`\s*,\s*`).ReplaceAllStringFunc(css, func(s string) string {
		if options != nil && options.NewlineBetweenSelectors != nil && !*options.NewlineBetweenSelectors {
			return ", "
		}
		return ",\n"
	})
	if braceStyle == "expand" {
		css = regexp.MustCompile(`\s*\{\s*`).ReplaceAllString(css, "\n{\n")
	} else {
		css = regexp.MustCompile(`\s*\{\s*`).ReplaceAllString(css, " {\n")
	}
	css = regexp.MustCompile(`;\s*`).ReplaceAllString(css, ";\n")
	css = regexp.MustCompile(`\s*\}\s*`).ReplaceAllString(css, "\n}\n")
	rawLines := strings.Split(css, "\n")
	var lines []string
	level := 0
	for _, line := range rawLines {
		line = strings.TrimSpace(line)
		if line == "" {
			continue
		}
		if strings.HasPrefix(line, "}") && level > 0 {
			level--
		}
		lines = append(lines, strings.Repeat(" ", level*tabSize)+line)
		if strings.HasSuffix(line, "{") {
			level++
		}
	}
	if options != nil && options.NewlineBetweenRules != nil && !*options.NewlineBetweenRules {
		return lines
	}
	var withBlank []string
	for i, line := range lines {
		withBlank = append(withBlank, line)
		if strings.TrimSpace(line) == "}" && i+1 < len(lines) {
			withBlank = append(withBlank, "")
		}
	}
	return withBlank
}

func limitBlankLines(text string, maxBlankLines int) string {
	lines := strings.Split(text, "\n")
	var out []string
	blankCount := 0
	for _, line := range lines {
		if strings.TrimSpace(line) == "" {
			if blankCount < maxBlankLines {
				out = append(out, line)
			}
			blankCount++
			continue
		}
		blankCount = 0
		out = append(out, line)
	}
	return strings.Join(out, "\n")
}

func computeIndentLevel(content string, offset, tabSize int) int {
	i := offset
	nChars := 0
	for i < len(content) {
		switch content[i] {
		case ' ':
			nChars++
		case '\t':
			nChars += tabSize
		default:
			return nChars / tabSize
		}
		i++
	}
	return nChars / tabSize
}

func isHorizontalWhitespace(ch byte) bool {
	return ch == ' ' || ch == '\t'
}

func isEOL(ch byte) bool {
	return ch == '\r' || ch == '\n'
}
