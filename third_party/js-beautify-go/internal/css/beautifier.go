// Package css implements the CSS formatter.
package css

import (
	"regexp"
	"strings"

	"github.com/yottonoko/js-beautify-go/internal/core"
)

var cssDirectiveBlockPattern = regexp.MustCompile(`(?s)/\* beautify( \w+:\w+)+ \*/`)
var cssDirectivePattern = regexp.MustCompile(` (\w+):(\w+)`)
var cssIgnoreEndPattern = regexp.MustCompile(`(?s)/\*\sbeautify\signore:end\s\*/`)

// Beautifier formats CSS source using normalized options.
type Beautifier struct {
	sourceText                  string
	options                     *Options
	ch                          string
	input                       *cssScanner
	output                      *core.Output
	indentLevel                 int
	nestedLevel                 int
	nestedAtRule                map[string]bool
	conditionalGroupRule        map[string]bool
	nonSemicolonNewlineProperty []string
}

type cssScanner struct {
	input    string
	position int
}

func newCSSScanner(input string) *cssScanner {
	return &cssScanner{input: input}
}

func (s *cssScanner) hasNext() bool {
	return s.position < len(s.input)
}

func (s *cssScanner) back() {
	if s.position > 0 {
		s.position--
	}
}

func (s *cssScanner) next() string {
	if !s.hasNext() {
		return ""
	}
	start := s.position
	s.position++
	return s.input[start:s.position]
}

func (s *cssScanner) peek(index ...int) string {
	offset := 0
	if len(index) > 0 {
		offset = index[0]
	}
	pos := s.position + offset
	if pos < 0 || pos >= len(s.input) {
		return ""
	}
	return s.input[pos : pos+1]
}

func (s *cssScanner) peekByte(offset int) byte {
	pos := s.position + offset
	if pos < 0 || pos >= len(s.input) {
		return 0
	}
	return s.input[pos]
}

func (s *cssScanner) readWhitespace() bool {
	start := s.position
	for s.position < len(s.input) && isCSSWhitespaceByte(s.input[s.position]) {
		s.position++
	}
	return s.position > start
}

func (s *cssScanner) readBlockComment() string {
	start := s.position
	if !strings.HasPrefix(s.input[start:], "/*") {
		return ""
	}
	if end := strings.Index(s.input[start+2:], "*/"); end >= 0 {
		s.position = start + 2 + end + 2
	} else {
		s.position = len(s.input)
	}
	return s.input[start:s.position]
}

func (s *cssScanner) readLineComment() string {
	start := s.position
	if !strings.HasPrefix(s.input[start:], "//") {
		return ""
	}
	s.position += 2
	for s.position < len(s.input) {
		if s.input[s.position] == '\n' || s.input[s.position] == '\r' {
			break
		}
		s.position++
	}
	return s.input[start:s.position]
}

func (s *cssScanner) readUntilByteAfter(ch byte) string {
	start := s.position
	if index := strings.IndexByte(s.input[s.position:], ch); index >= 0 {
		s.position += index + 1
	} else {
		s.position = len(s.input)
	}
	return s.input[start:s.position]
}

func (s *cssScanner) readUntilRegexpAfter(pattern *regexp.Regexp) string {
	start := s.position
	if loc := pattern.FindStringIndex(s.input[s.position:]); loc != nil {
		s.position += loc[1]
	} else {
		s.position = len(s.input)
	}
	return s.input[start:s.position]
}

func (s *cssScanner) peekUntilBoundaryAfter() string {
	pos := s.position
	for pos < len(s.input) {
		if isCSSVariableBoundaryByte(s.input[pos]) {
			pos++
			break
		}
		pos++
	}
	return s.input[s.position:pos]
}

func (s *cssScanner) lookBack(testVal string) bool {
	start := s.position - 1
	if start < len(testVal) {
		return false
	}
	return equalASCIIFold(s.input[start-len(testVal):start], testVal)
}

func (s *cssScanner) readDefaultRun(first string) string {
	start := s.position - len(first)
	for s.position < len(s.input) && isCSSDefaultRunByte(s.input[s.position]) {
		s.position++
	}
	return s.input[start:s.position]
}

// Beautify formats CSS source with js-beautify-compatible options.
func Beautify(source string, options map[string]any) (string, error) {
	opts, err := NewOptions(options)
	if err != nil {
		return "", err
	}
	return NewBeautifier(source, opts).Beautify()
}

// NewBeautifier creates a CSS beautifier for source and options.
func NewBeautifier(source string, options *Options) *Beautifier {
	return &Beautifier{
		sourceText: source,
		options:    options,
		nestedAtRule: map[string]bool{
			"page": true, "font-face": true, "keyframes": true,
			"media": true, "supports": true, "document": true,
		},
		conditionalGroupRule: map[string]bool{
			"media": true, "supports": true, "document": true,
		},
		nonSemicolonNewlineProperty: []string{"grid-template-areas", "grid-template"},
	}
}

func firstCSSLineBreak(source string) string {
	for i := 0; i < len(source); i++ {
		switch source[i] {
		case '\n':
			return "\n"
		case '\r':
			if i+1 < len(source) && source[i+1] == '\n' {
				return "\r\n"
			}
			return "\r"
		}
	}
	return ""
}

func normalizeCSSLineBreaks(source string) string {
	if !strings.Contains(source, "\r") {
		return source
	}
	var out strings.Builder
	out.Grow(len(source))
	for i := 0; i < len(source); i++ {
		if source[i] != '\r' {
			out.WriteByte(source[i])
			continue
		}
		out.WriteByte('\n')
		if i+1 < len(source) && source[i+1] == '\n' {
			i++
		}
	}
	return out.String()
}

func (b *Beautifier) eatString(endChars string) string {
	start := b.input.position
	b.ch = b.input.next()
	for b.ch != "" {
		if b.ch == `\` {
			b.input.next()
		} else if strings.Contains(endChars, b.ch) || b.ch == "\n" {
			break
		}
		b.ch = b.input.next()
	}
	return b.input.input[start:b.input.position]
}

func (b *Beautifier) eatWhitespace(allowAtLeastOneNewLine ...bool) bool {
	allow := len(allowAtLeastOneNewLine) > 0 && allowAtLeastOneNewLine[0]
	result := isCSSWhitespaceByte(b.input.peekByte(0))
	newlineCount := 0
	for isCSSWhitespaceByte(b.input.peekByte(0)) {
		b.ch = b.input.next()
		if allow && b.ch == "\n" {
			if newlineCount == 0 || newlineCount < b.options.MaxPreserveNewlines {
				newlineCount++
				b.output.AddNewLine(true)
			}
		}
	}
	return result
}

func (b *Beautifier) foundNestedPseudoClass() bool {
	openParen := 0
	for i := 1; ; i++ {
		ch := b.input.peek(i)
		if ch == "" {
			return false
		}
		switch ch {
		case "{":
			return true
		case "(":
			openParen++
		case ")":
			if openParen == 0 {
				return false
			}
			openParen--
		case ";", "}":
			return false
		}
	}
}

func (b *Beautifier) printString(outputString string) {
	b.output.SetIndent(b.indentLevel, 0)
	b.output.NonBreakingSpace = true
	b.output.AddToken(outputString)
}

func (b *Beautifier) preserveSingleSpace(isAfterSpace bool) {
	if isAfterSpace {
		b.output.SpaceBeforeToken = true
	}
}

func (b *Beautifier) indent() {
	b.indentLevel++
}

func (b *Beautifier) outdent() {
	if b.indentLevel > 0 {
		b.indentLevel--
	}
}

// Beautify formats the beautifier source and returns the resulting code.
func (b *Beautifier) Beautify() (string, error) {
	if b.options.Disabled {
		return b.sourceText, nil
	}

	sourceText := b.sourceText
	eol := b.options.EOL
	if eol == "auto" {
		eol = "\n"
		if lineBreak := firstCSSLineBreak(sourceText); lineBreak != "" {
			eol = lineBreak
		}
	}
	sourceText = normalizeCSSLineBreaks(sourceText)
	baseIndent := leadingIndent(sourceText)

	b.output = core.NewOutput(core.OutputOptionsFromBase(b.options.BaseOptions), baseIndent)
	b.input = newCSSScanner(sourceText)
	b.indentLevel = 0
	b.nestedLevel = 0
	b.ch = ""

	parenLevel := 0
	insideRule := false
	insidePropertyValue := false
	enteringConditionalGroup := false
	insideNonNestedAtRule := false
	insideScssMap := false
	topCharacter := b.ch
	insideNonSemicolonValues := false

	for {
		isAfterSpace := b.input.readWhitespace()
		previousCh := topCharacter
		b.ch = b.input.next()
		if b.ch == `\` && b.input.hasNext() {
			b.ch += b.input.next()
		}
		topCharacter = b.ch

		switch {
		case b.ch == "":
			return b.output.GetCode(eol), nil
		case b.ch == "/" && b.input.peek() == "*":
			b.output.AddNewLine(false)
			b.input.back()
			comment := b.input.readBlockComment()
			if directives := cssGetDirectives(comment); directives["ignore"] == "start" {
				comment += b.input.readUntilRegexpAfter(cssIgnoreEndPattern)
			}
			b.printString(comment)
			b.eatWhitespace(true)
			b.output.AddNewLine(false)
		case b.ch == "/" && b.input.peek() == "/":
			b.output.SpaceBeforeToken = true
			b.input.back()
			b.printString(b.input.readLineComment())
			b.eatWhitespace(true)
		case b.ch == "$":
			b.preserveSingleSpace(isAfterSpace)
			b.printString(b.ch)
			variable := b.input.peekUntilBoundaryAfter()
			if strings.HasSuffix(variable, " ") || strings.HasSuffix(variable, ":") {
				variable = strings.TrimRight(b.eatString(": "), " \t\r\n")
				b.printString(variable)
				b.output.SpaceBeforeToken = true
			}
			if parenLevel == 0 && strings.Contains(variable, ":") {
				insidePropertyValue = true
				b.indent()
			}
		case b.ch == "@":
			b.preserveSingleSpace(isAfterSpace)
			if b.input.peek() == "{" {
				b.printString(b.ch + b.input.readUntilByteAfter('}'))
			} else {
				b.printString(b.ch)
				variableOrRule := b.input.peekUntilBoundaryAfter()
				if strings.HasSuffix(variableOrRule, " ") || strings.HasSuffix(variableOrRule, ":") {
					variableOrRule = strings.TrimRight(b.eatString(": "), " \t\r\n")
					b.printString(variableOrRule)
					b.output.SpaceBeforeToken = true
				}
				if parenLevel == 0 && strings.Contains(variableOrRule, ":") {
					insidePropertyValue = true
					b.indent()
				} else if b.nestedAtRule[variableOrRule] {
					b.nestedLevel++
					if b.conditionalGroupRule[variableOrRule] {
						enteringConditionalGroup = true
					}
				} else if parenLevel == 0 && !insidePropertyValue {
					insideNonNestedAtRule = true
				}
			}
		case b.ch == "#" && b.input.peek() == "{":
			b.preserveSingleSpace(isAfterSpace)
			b.printString(b.ch + b.input.readUntilByteAfter('}'))
		case b.ch == "{":
			if insidePropertyValue {
				insidePropertyValue = false
				b.outdent()
			}
			insideNonNestedAtRule = false
			if enteringConditionalGroup {
				enteringConditionalGroup = false
				insideRule = b.indentLevel >= b.nestedLevel
			} else {
				insideRule = b.indentLevel >= b.nestedLevel-1
			}
			if b.options.NewlineBetweenRules && insideRule {
				if b.output.PreviousLine != nil && b.output.PreviousLine.Item(-1) != "{" {
					b.output.EnsureEmptyLineAbove("/", ",")
				}
			}
			b.output.SpaceBeforeToken = true
			if b.options.BraceStyle == "expand" {
				b.output.AddNewLine(false)
				b.printString(b.ch)
				b.indent()
				b.output.SetIndent(b.indentLevel, 0)
			} else {
				if previousCh == "(" {
					b.output.SpaceBeforeToken = false
				} else if previousCh != "," {
					b.indent()
				}
				b.printString(b.ch)
			}
			b.eatWhitespace(true)
			b.output.AddNewLine(false)
		case b.ch == "}":
			b.outdent()
			b.output.AddNewLine(false)
			if previousCh == "{" {
				b.output.Trim(true)
			}
			if insidePropertyValue {
				b.outdent()
				insidePropertyValue = false
			}
			b.printString(b.ch)
			insideRule = false
			if b.nestedLevel > 0 {
				b.nestedLevel--
			}
			b.eatWhitespace(true)
			b.output.AddNewLine(false)
			if b.options.NewlineBetweenRules && !b.output.JustAddedBlankline() {
				if b.input.peek() != "}" {
					b.output.AddNewLine(true)
				}
			}
			if b.input.peek() == ")" {
				b.output.Trim(true)
				if b.options.BraceStyle == "expand" {
					b.output.AddNewLine(true)
				}
			}
		case b.ch == ":":
			for _, property := range b.nonSemicolonNewlineProperty {
				if b.input.lookBack(property) {
					insideNonSemicolonValues = true
					break
				}
			}
			if (insideRule || enteringConditionalGroup) &&
				!(b.input.lookBack("&") || b.foundNestedPseudoClass()) &&
				!b.input.lookBack("(") && !insideNonNestedAtRule && parenLevel == 0 {
				b.printString(":")
				if !insidePropertyValue {
					insidePropertyValue = true
					b.output.SpaceBeforeToken = true
					b.eatWhitespace(true)
					b.indent()
				}
			} else {
				if b.input.lookBack(" ") {
					b.output.SpaceBeforeToken = true
				}
				if b.input.peek() == ":" {
					b.ch = b.input.next()
					b.printString("::")
				} else {
					b.printString(":")
				}
			}
		case b.ch == `"` || b.ch == `'`:
			preserveQuoteSpace := previousCh == `"` || previousCh == `'`
			b.preserveSingleSpace(preserveQuoteSpace || isAfterSpace)
			// eatString moves b.ch, so read the quote before calling it; Go does
			// not order the field read against the call in b.ch + b.eatString().
			quote := b.ch
			b.printString(quote + b.eatString(quote))
			b.eatWhitespace(true)
		case b.ch == ";":
			insideNonSemicolonValues = false
			if parenLevel == 0 {
				if insidePropertyValue {
					b.outdent()
					insidePropertyValue = false
				}
				insideNonNestedAtRule = false
				b.printString(b.ch)
				b.eatWhitespace(true)
				if b.input.peek() != "/" {
					b.output.AddNewLine(false)
				}
			} else {
				b.printString(b.ch)
				b.eatWhitespace(true)
				b.output.SpaceBeforeToken = true
			}
		case b.ch == "(":
			if b.input.lookBack("url") {
				b.printString(b.ch)
				b.eatWhitespace()
				parenLevel++
				b.indent()
				urlValue := b.input.readUntilByteAfter(')')
				if urlValue != "" {
					b.printString(urlValue)
					if parenLevel > 0 {
						parenLevel--
						b.outdent()
					}
				}
			} else {
				spaceNeeded := b.input.lookBack("with")
				b.preserveSingleSpace(isAfterSpace || spaceNeeded)
				b.printString(b.ch)
				if insidePropertyValue && previousCh == "$" && b.options.SelectorSeparatorNewline {
					b.output.AddNewLine(false)
					insideScssMap = true
				} else {
					b.eatWhitespace()
					parenLevel++
					b.indent()
				}
			}
		case b.ch == ")":
			if parenLevel > 0 {
				parenLevel--
				b.outdent()
			}
			if insideScssMap && b.input.peek() == ";" && b.options.SelectorSeparatorNewline {
				insideScssMap = false
				b.outdent()
				b.output.AddNewLine(false)
			}
			b.printString(b.ch)
		case b.ch == ",":
			b.printString(b.ch)
			b.eatWhitespace(true)
			if b.options.SelectorSeparatorNewline && (!insidePropertyValue || insideScssMap) &&
				parenLevel == 0 && !insideNonNestedAtRule {
				b.output.AddNewLine(false)
			} else {
				b.output.SpaceBeforeToken = true
			}
		case (b.ch == ">" || b.ch == "+" || b.ch == "~") && !insidePropertyValue && parenLevel == 0:
			if b.options.SpaceAroundCombinator {
				b.output.SpaceBeforeToken = true
				b.printString(b.ch)
				b.output.SpaceBeforeToken = true
			} else {
				b.printString(b.ch)
				b.eatWhitespace()
				if b.ch != "" && isCSSWhitespaceByte(b.ch[0]) {
					b.ch = ""
				}
			}
		case b.ch == "]":
			b.printString(b.ch)
		case b.ch == "[":
			b.preserveSingleSpace(isAfterSpace)
			b.printString(b.ch)
		case b.ch == "=":
			b.eatWhitespace()
			b.printString("=")
			if b.ch != "" && isCSSWhitespaceByte(b.ch[0]) {
				b.ch = ""
			}
		case b.ch == "!" && !b.input.lookBack(`\`):
			b.output.SpaceBeforeToken = true
			b.printString(b.ch)
		default:
			preserveAfterSpace := previousCh == `"` || previousCh == `'`
			b.preserveSingleSpace(preserveAfterSpace || isAfterSpace)
			b.ch = b.input.readDefaultRun(b.ch)
			b.printString(b.ch)
			topCharacter = b.ch[len(b.ch)-1:]
			if !b.output.JustAddedNewline() && b.input.peek() == "\n" && insideNonSemicolonValues {
				b.output.AddNewLine(false)
			}
		}
	}
}

func leadingIndent(source string) string {
	i := 0
	for i < len(source) && (source[i] == ' ' || source[i] == '\t') {
		i++
	}
	return source[:i]
}

func isCSSWhitespaceByte(ch byte) bool {
	switch ch {
	case ' ', '\t', '\n', '\r', '\f':
		return true
	default:
		return false
	}
}

func isCSSVariableBoundaryByte(ch byte) bool {
	switch ch {
	case ':', ' ', ',', ';', '{', '}', '(', ')', '[', ']', '/', '=', '\'', '"':
		return true
	default:
		return isCSSWhitespaceByte(ch)
	}
}

func isCSSDefaultRunByte(ch byte) bool {
	if isCSSWhitespaceByte(ch) {
		return false
	}
	switch ch {
	case '\\', '/', '$', '@', '#', '{', '}', ':', '\'', '"', ';', '(', ')', ',', '>', '+', '~', '[', ']', '=', '!':
		return false
	default:
		return true
	}
}

func equalASCIIFold(left string, right string) bool {
	if len(left) != len(right) {
		return false
	}
	for i := 0; i < len(left); i++ {
		if toLowerASCII(left[i]) != toLowerASCII(right[i]) {
			return false
		}
	}
	return true
}

func toLowerASCII(ch byte) byte {
	if ch >= 'A' && ch <= 'Z' {
		return ch + ('a' - 'A')
	}
	return ch
}

func cssGetDirectives(text string) map[string]string {
	if !cssDirectiveBlockPattern.MatchString(text) {
		return nil
	}
	out := map[string]string{}
	for _, match := range cssDirectivePattern.FindAllStringSubmatch(text, -1) {
		out[match[1]] = match[2]
	}
	return out
}
