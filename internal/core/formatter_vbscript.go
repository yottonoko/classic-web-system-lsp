package core

import (
	"regexp"
	"strconv"
	"strings"
)

var vbOperators = regexp.MustCompile(`\s*(=|<>|<=|>=|\+|-|\*|/|&)\s*`)

func formatVBLine(line string, options FormattingOptions) string {
	protected, restore := protectVBLineLiterals(line)
	protected = collapseVBWhitespace(protected)
	if hasVBOperatorSpacingCandidate(protected) {
		protected = vbOperators.ReplaceAllString(protected, " $1 ")
	}
	protected = formatVBKeywords(protected, options)
	return restore(protected)
}

func protectVBLineLiterals(line string) (string, func(string) string) {
	if !strings.ContainsAny(line, `'"`) {
		return line, func(formatted string) string { return formatted }
	}
	var out strings.Builder
	replacements := make([]string, 0)
	for i := 0; i < len(line); {
		if line[i] == '\'' {
			placeholder := vbLiteralPlaceholder(len(replacements))
			replacements = append(replacements, line[i:])
			out.WriteString(placeholder)
			break
		}
		if line[i] != '"' {
			out.WriteByte(line[i])
			i++
			continue
		}
		start := i
		i++
		for i < len(line) {
			if line[i] == '"' {
				if i+1 < len(line) && line[i+1] == '"' {
					i += 2
					continue
				}
				i++
				break
			}
			i++
		}
		placeholder := vbLiteralPlaceholder(len(replacements))
		replacements = append(replacements, line[start:i])
		out.WriteString(placeholder)
	}
	return out.String(), func(formatted string) string {
		return restoreVBLineLiterals(formatted, replacements)
	}
}

// restoreVBLineLiterals substitutes placeholders in one pass so restored
// literal text is never rescanned for placeholder-shaped content.
func restoreVBLineLiterals(formatted string, replacements []string) string {
	const prefix = "__ASP_LSP_VB_LITERAL_"
	if !strings.Contains(formatted, prefix) {
		return formatted
	}
	var out strings.Builder
	out.Grow(len(formatted))
	for {
		start := strings.Index(formatted, prefix)
		if start < 0 {
			out.WriteString(formatted)
			return out.String()
		}
		digits := start + len(prefix)
		end := digits
		for end < len(formatted) && formatted[end] >= '0' && formatted[end] <= '9' {
			end++
		}
		index, err := strconv.Atoi(formatted[digits:end])
		if err != nil || index >= len(replacements) || (end-digits > 1 && formatted[digits] == '0') || !strings.HasPrefix(formatted[end:], "__") {
			out.WriteString(formatted[:digits])
			formatted = formatted[digits:]
			continue
		}
		out.WriteString(formatted[:start])
		out.WriteString(replacements[index])
		formatted = formatted[end+2:]
	}
}

func collapseVBWhitespace(line string) string {
	if !vbWhitespaceNeedsCollapse(line) {
		return line
	}
	return strings.Join(strings.Fields(line), " ")
}

func vbWhitespaceNeedsCollapse(line string) bool {
	previousSpace := true
	for i := 0; i < len(line); i++ {
		switch line[i] {
		case ' ':
			if previousSpace {
				return true
			}
			previousSpace = true
		case '\t', '\n', '\v', '\f', '\r':
			return true
		default:
			previousSpace = false
		}
	}
	return previousSpace && line != ""
}

func hasVBOperatorSpacingCandidate(line string) bool {
	for i := 0; i < len(line); i++ {
		switch line[i] {
		case '=', '+', '-', '*', '/', '&':
			return true
		case '<':
			if i+1 < len(line) && (line[i+1] == '>' || line[i+1] == '=') {
				return true
			}
		case '>':
			if i+1 < len(line) && line[i+1] == '=' {
				return true
			}
		}
	}
	return false
}

func vbLiteralPlaceholder(index int) string {
	return "__ASP_LSP_VB_LITERAL_" + strconv.Itoa(index) + "__"
}

var vbKeywordPattern = regexp.MustCompile(`(?i)\b(Class|Sub|Function|Property|Get|Let|Set|If|Then|Else|ElseIf|End|For|Each|In|Next|Do|Loop|While|Wend|Select|Case|With|Dim|Const|Private|Public|Default|Static|Option|Explicit|Call|ByVal|ByRef|As|New|Exit|On|Error|Resume|Goto|Rem|And|Or|Not|Mod|Is)\b`)

func formatVBKeywords(line string, options FormattingOptions) string {
	mode := strings.ToLower(strings.TrimSpace(options.VBScriptKeywordCase))
	if mode == "" && options.UppercaseKeywords {
		mode = "upper"
	}
	switch mode {
	case "upper":
		return vbKeywordPattern.ReplaceAllStringFunc(line, strings.ToUpper)
	case "lower":
		return vbKeywordPattern.ReplaceAllStringFunc(line, strings.ToLower)
	case "title":
		return vbKeywordPattern.ReplaceAllStringFunc(line, func(keyword string) string {
			if keyword == "" {
				return keyword
			}
			return strings.ToUpper(keyword[:1]) + strings.ToLower(keyword[1:])
		})
	default:
		return line
	}
}

func aspExpressionText(expression string, options FormattingOptions) string {
	if options.ASPDelimiterSpacing == "compact" {
		return "<%=" + expression + "%>"
	}
	return "<%= " + expression + " %>"
}

func aspDirectiveText(directive string, options FormattingOptions) string {
	if options.ASPDelimiterSpacing == "compact" {
		return "<%@" + directive + "%>"
	}
	return "<%@ " + directive + " %>"
}

func aspBlockText(code string, options FormattingOptions) string {
	if options.ASPDelimiterSpacing == "compact" {
		return "<%" + code + "%>"
	}
	return "<% " + code + " %>"
}

func vbscriptBlockBaseIndent(options FormattingOptions) string {
	if options.VBScriptBlockIndent == "alignWithDelimiter" {
		return ""
	}
	return indentUnit(options)
}

func vbContinuationIndent(continued bool, options FormattingOptions) string {
	if !continued {
		return ""
	}
	if options.VBScriptLineContinuationIndentSize > 0 {
		return strings.Repeat(" ", options.VBScriptLineContinuationIndentSize)
	}
	return indentUnit(options)
}

func codeBeforeVBComment(line string) string {
	for i := 0; i < len(line); {
		switch line[i] {
		case '\'':
			return strings.TrimSpace(line[:i])
		case '"':
			i++
			for i < len(line) {
				if line[i] == '"' {
					if i+1 < len(line) && line[i+1] == '"' {
						i += 2
						continue
					}
					i++
					break
				}
				i++
			}
		default:
			i++
		}
	}
	return strings.TrimSpace(line)
}

func isVBLineContinuation(line string) bool {
	return strings.HasSuffix(strings.TrimSpace(codeBeforeVBComment(line)), "_")
}

func isVBEndSelectLine(line string) bool {
	rest, ok := consumeVBWord(line, "End")
	if !ok {
		return false
	}
	rest = strings.TrimLeft(rest, " \t")
	if rest == line || rest == "" {
		return false
	}
	_, ok = consumeVBWord(rest, "Select")
	return ok
}

func isVBSelectLine(line string) bool {
	_, ok := consumeVBWord(line, "Select")
	return ok
}

func isVBCaseLine(line string) bool {
	_, ok := consumeVBWord(line, "Case")
	return ok
}

func vbDedentsBeforeLine(line string) bool {
	for _, keyword := range [...]string{"End", "Else", "ElseIf", "Next", "Loop", "Wend"} {
		if _, ok := consumeVBWord(line, keyword); ok {
			return true
		}
	}
	return false
}

func vbIndentsAfterLine(line string) bool {
	statement := stripVBDeclarationModifiers(line)
	if asciiEqualFold(statement, "Else") {
		return true
	}
	if _, ok := consumeVBWord(statement, "End"); ok {
		return false
	}
	return vbBlockStartsLine(statement) || vbLineEndsWithWord(statement, "Then")
}

func stripVBDeclarationModifiers(line string) string {
	rest := line
	for {
		next, ok := consumeAnyVBWord(rest, "Public", "Private", "Default", "Static")
		if !ok || next == "" || !isVBHorizontalWhitespace(next[0]) {
			return rest
		}
		rest = strings.TrimLeft(next, " \t")
	}
}

func vbBlockStartsLine(line string) bool {
	if _, ok := consumeAnyVBWord(line, "Class", "Sub", "Function", "With", "For", "Do", "While"); ok {
		return true
	}
	rest, ok := consumeVBWord(line, "Property")
	if !ok {
		return false
	}
	return containsVBWord(rest, "Get") || containsVBWord(rest, "Let") || containsVBWord(rest, "Set")
}

func consumeAnyVBWord(line string, words ...string) (string, bool) {
	for _, word := range words {
		if rest, ok := consumeVBWord(line, word); ok {
			return rest, true
		}
	}
	return line, false
}

func consumeVBWord(line string, word string) (string, bool) {
	if len(line) < len(word) || !asciiEqualFold(line[:len(word)], word) {
		return line, false
	}
	if len(line) > len(word) && isVBIdentifierByte(line[len(word)]) {
		return line, false
	}
	return line[len(word):], true
}

func vbLineEndsWithWord(line string, word string) bool {
	if len(line) < len(word) || !asciiEqualFold(line[len(line)-len(word):], word) {
		return false
	}
	return len(line) == len(word) || !isVBIdentifierByte(line[len(line)-len(word)-1])
}

func containsVBWord(line string, word string) bool {
	for i := 0; i+len(word) <= len(line); i++ {
		if i > 0 && isVBIdentifierByte(line[i-1]) {
			continue
		}
		if _, ok := consumeVBWord(line[i:], word); ok {
			return true
		}
	}
	return false
}

func asciiEqualFold(left string, right string) bool {
	if len(left) != len(right) {
		return false
	}
	for i := 0; i < len(left); i++ {
		l, r := left[i], right[i]
		if 'A' <= l && l <= 'Z' {
			l += 'a' - 'A'
		}
		if 'A' <= r && r <= 'Z' {
			r += 'a' - 'A'
		}
		if l != r {
			return false
		}
	}
	return true
}

func isVBIdentifierByte(ch byte) bool {
	return ch == '_' || '0' <= ch && ch <= '9' || 'A' <= ch && ch <= 'Z' || 'a' <= ch && ch <= 'z'
}

func isVBHorizontalWhitespace(ch byte) bool {
	return ch == ' ' || ch == '\t'
}

var vbAssignmentLinePattern = regexp.MustCompile(`^(\s*(?:Set\s+)?[A-Za-z_][A-Za-z0-9_.]*) = (.+)$`)

func alignVBAssignments(lines []string) []string {
	result := append([]string(nil), lines...)
	groupStart := -1
	maxLeft := 0
	flush := func(exclusiveEnd int) {
		if groupStart == -1 || exclusiveEnd-groupStart < 2 {
			groupStart = -1
			maxLeft = 0
			return
		}
		for i := groupStart; i < exclusiveEnd; i++ {
			match := vbAssignmentLinePattern.FindStringSubmatch(result[i])
			if len(match) == 3 {
				result[i] = match[1] + strings.Repeat(" ", maxLeft-len(match[1])) + " = " + match[2]
			}
		}
		groupStart = -1
		maxLeft = 0
	}
	for i := 0; i <= len(result); i++ {
		var match []string
		if i < len(result) {
			match = vbAssignmentLinePattern.FindStringSubmatch(result[i])
		}
		if len(match) != 3 {
			flush(i)
			continue
		}
		if groupStart == -1 {
			groupStart = i
		}
		if len(match[1]) > maxLeft {
			maxLeft = len(match[1])
		}
	}
	return result
}
