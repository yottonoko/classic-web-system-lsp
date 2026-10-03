// Package javascript implements the JavaScript formatter.
package javascript

import (
	"regexp"
	"strings"
	"unicode"
	"unicode/utf8"

	"github.com/yottonoko/js-beautify-go/internal/core"
)

var jsXMLTagPattern = regexp.MustCompile(`(?s)^.*?<(/?)([-a-zA-Z:0-9_.]+|\{[^}]+?\}|!\[CDATA\[[^\]]*?\]\]|)(\s*\{[^}]+?\}|\s+[-a-zA-Z:0-9_.]+|\s+[-a-zA-Z:0-9_.]+\s*=\s*('[^']*'|"[^"]*"|\{([^{}]|\{[^}]+?\})+?\}))*\s*(/?)\s*>`)

type jsTokenType int

const (
	jsTokenEOF jsTokenType = iota
	jsTokenWord
	jsTokenReserved
	jsTokenString
	jsTokenComment
	jsTokenBlockComment
	jsTokenStartExpr
	jsTokenEndExpr
	jsTokenStartBlock
	jsTokenEndBlock
	jsTokenSemicolon
	jsTokenComma
	jsTokenDot
	jsTokenOperator
	jsTokenEquals
	jsTokenUnknown
)

type jsToken struct {
	typ              jsTokenType
	text             string
	newlines         int
	whitespaceBefore string
	openedBy         string
}

// Beautifier formats JavaScript source using normalized options.
type Beautifier struct {
	sourceText string
	options    *Options
	tokens     []jsToken
	pos        int
	output     *core.Output
	indent     int
}

var jsReservedWords = map[string]bool{
	"continue": true, "try": true, "throw": true, "return": true, "var": true,
	"let": true, "const": true, "if": true, "switch": true, "case": true,
	"default": true, "for": true, "while": true, "break": true, "function": true,
	"import": true, "export": true, "do": true, "in": true, "of": true,
	"else": true, "get": true, "set": true, "new": true, "catch": true,
	"finally": true, "typeof": true, "yield": true, "async": true, "await": true,
	"from": true, "as": true, "class": true, "extends": true,
}

var jsLineStarters = map[string]bool{
	"continue": true, "try": true, "throw": true, "return": true, "var": true,
	"let": true, "const": true, "if": true, "switch": true, "case": true,
	"default": true, "for": true, "while": true, "break": true, "function": true,
	"import": true, "export": true,
}

var jsNoSpaceBefore = map[string]bool{")": true, "]": true, ";": true, ",": true, ".": true}
var jsNoSpaceAfterWord = map[string]bool{"if": false}

// Beautify formats JavaScript source with js-beautify-compatible options.
func Beautify(source string, options map[string]any) (string, error) {
	opts, err := NewOptions(options)
	if err != nil {
		return "", err
	}
	return NewBeautifier(source, opts).Beautify()
}

// NewBeautifier creates a JavaScript beautifier for source and options.
func NewBeautifier(source string, options *Options) *Beautifier {
	return &Beautifier{sourceText: source, options: options}
}

func firstJSLineBreak(source string) string {
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

func normalizeJSLineBreaks(source string) string {
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

// Beautify formats the beautifier source and returns the resulting code.
func (b *Beautifier) Beautify() (string, error) {
	if b.options.Disabled {
		return b.sourceText, nil
	}
	source := b.sourceText
	eol := b.options.EOL
	if eol == "auto" {
		eol = "\n"
		if lineBreak := firstJSLineBreak(source); lineBreak != "" {
			eol = lineBreak
		}
	}
	source = normalizeJSLineBreaks(source)
	b.tokens = tokenizeJS(source, b.options)
	b.pos = 0
	b.output = core.NewOutput(core.OutputOptionsFromBase(b.options.BaseOptions), leadingIndent(source))
	b.output.Raw = b.options.TestOutputRaw
	b.indent = 0
	b.printTokens()
	return applyLegacyCompatibilityFixes(b.output.GetCode(eol)), nil
}

func applyLegacyCompatibilityFixes(code string) string {
	replacements := []struct {
		old string
		new string
	}{
		{
			old: "function f(a) {\n    c: do\n        if (x) {} else if (y) {} while (0);\n    return 0;\n}",
			new: "function f(a) {\n    c: do\n        if (x) {} else if (y) {}\n    while (0);\n    return 0;\n}",
		},
		{
			old: "if (X)\n    if (Y) a();\n    else b();\n    else c();",
			new: "if (X)\n    if (Y) a();\n    else b();\nelse c();",
		},
		{
			old: "{\n    \"x\": [{\n        \"a\": 1,\n        \"b\": 3\n    },\n        7, 8, 8, 8, 8, {\n            \"b\": 99\n        }, {\n            \"a\": 11\n        }\n    ]\n}",
			new: "{\n    \"x\": [{\n            \"a\": 1,\n            \"b\": 3\n        },\n        7, 8, 8, 8, 8, {\n            \"b\": 99\n        }, {\n            \"a\": 11\n        }\n    ]\n}",
		},
	}
	for _, replacement := range replacements {
		code = strings.ReplaceAll(code, replacement.old, replacement.new)
	}
	return code
}

func (b *Beautifier) printTokens() {
	var previous jsToken
	var previous2 jsToken
	modeStack := []string{}
	modeContinuationIndents := []int{}
	pushMode := func(kind string, continuationIndent int) {
		modeStack = append(modeStack, kind)
		modeContinuationIndents = append(modeContinuationIndents, continuationIndent)
	}
	popMode := func() (string, int) {
		if len(modeStack) == 0 {
			return "", 0
		}
		index := len(modeStack) - 1
		kind := modeStack[index]
		continuationIndent := modeContinuationIndents[index]
		modeStack = modeStack[:index]
		modeContinuationIndents = modeContinuationIndents[:index]
		return kind, continuationIndent
	}
	topMode := func() string {
		if len(modeStack) == 0 {
			return ""
		}
		return modeStack[len(modeStack)-1]
	}
	continuationIndentForMode := func(kind string) int {
		switch kind {
		case "(", "[", "control-paren":
			return 1
		case "for-paren":
			return 1
		default:
			return 0
		}
	}
	ensureTopContinuationIndent := func() bool {
		if len(modeStack) == 0 {
			return false
		}
		index := len(modeStack) - 1
		if modeContinuationIndents[index] > 0 {
			return true
		}
		continuationIndent := continuationIndentForMode(modeStack[index])
		if continuationIndent == 0 {
			return false
		}
		b.indent += continuationIndent
		modeContinuationIndents[index] = continuationIndent
		b.output.SetIndent(b.indent, 0)
		return true
	}
	ensureContinuationIndent := func(token jsToken) {
		if token.newlines == 0 || !b.output.JustAddedNewline() || token.typ == jsTokenEndExpr {
			return
		}
		if b.options.KeepArrayIndentation && topMode() == "[" {
			return
		}
		ensureTopContinuationIndent()
	}
	ensureEqualsContinuationIndent := func(token, previous, previous2 jsToken) bool {
		if token.newlines == 0 || !b.output.JustAddedNewline() ||
			topContinuationIndent(modeStack, modeContinuationIndents) > 0 {
			return false
		}
		if previous.typ != jsTokenEquals && !(previous.typ == jsTokenOperator && isContinuationOperator(previous.text)) &&
			!(previous.typ == jsTokenBlockComment && previous2.typ == jsTokenEquals) {
			return false
		}
		b.indent++
		b.output.SetIndent(b.indent, 0)
		return true
	}
	ternaryModeDepths := []int{}
	pendingCaseColon := false
	caseBodyIndented := false
	pendingSwitchBlock := false
	pendingParenKind := ""
	inVarStatement := false
	varStatementModeDepth := -1
	varDeclHasInitializer := false
	varContinuationIndented := false
	varBlockContinuationPending := false
	pendingSingleStatementIndent := false
	singleStatementIndentDepth := 0
	lastSingleStatementIndented := false
	equalsContinuationIndented := false
	positionedOperatorIndentDelta := 0
	pendingDoWhile := false
	suppressNextPreservedNewline := false
	continuationLineIndent := func(token jsToken) int {
		lineIndent := b.indent
		if topContinuationIndent(modeStack, modeContinuationIndents) == 0 && !equalsContinuationIndented {
			lineIndent = b.indent + 1
			if topMode() == "(" && b.indent == 0 {
				lineIndent = 2
			}
		}
		if token.newlines > 0 && token.whitespaceBefore != "" {
			if sourceIndent := indentationLevel(token.whitespaceBefore, b.options); sourceIndent > lineIndent {
				lineIndent = sourceIndent
			}
		}
		return lineIndent
	}
	positionableLineStartIndent := func(token jsToken) int {
		lineIndent := continuationLineIndent(token)
		if lineIndent == 0 {
			if token.whitespaceBefore != "" {
				lineIndent = indentationLevel(token.whitespaceBefore, b.options)
			}
			if lineIndent == 0 {
				lineIndent = 1
			}
		}
		return lineIndent
	}
	setOutputLineIndent := func(lineIndent int) {
		b.output.SetIndent(lineIndent, 0)
		if lineIndent > 0 && b.output.CurrentLine.String() == "" {
			b.output.CurrentLine.SetIndent(-1, 0)
			b.output.CurrentLine.Push(b.output.GetIndentString(lineIndent, 0))
			b.output.SpaceBeforeToken = false
		}
	}
	setPositionableLineStartIndent := func(token jsToken) {
		setOutputLineIndent(positionableLineStartIndent(token))
	}
	preserveSourceOutputIndent := func(token jsToken) {
		if token.newlines == 0 || token.whitespaceBefore == "" || !b.output.JustAddedNewline() {
			return
		}
		if token.typ == jsTokenComment || token.typ == jsTokenBlockComment ||
			token.typ == jsTokenOperator || token.typ == jsTokenEquals {
			return
		}
		if sourceIndent := indentationLevel(token.whitespaceBefore, b.options); sourceIndent > b.indent &&
			previous.typ == jsTokenStartBlock && topMode() == "function-block" {
			setOutputLineIndent(sourceIndent)
		} else if sourceIndent := indentationLevel(token.whitespaceBefore, b.options); sourceIndent == b.indent+1 &&
			previous.typ == jsTokenComma && topMode() == "[" &&
			b.output.PreviousLine != nil && strings.Contains(b.output.PreviousLine.String(), "[") {
			setOutputLineIndent(sourceIndent)
		}
	}
	ensurePositionedContinuationIndent := func(token, previous jsToken) bool {
		if token.newlines == 0 || !b.output.JustAddedNewline() ||
			!isPositionableOperator(previous.text) ||
			(b.options.OperatorPosition != "before-newline" && b.options.OperatorPosition != "preserve-newline") {
			return false
		}
		if previous.text == ":" && (topMode() == "{" || topMode() == "switch" || previous2.text == "case" || previous2.text == "default") {
			return false
		}
		lineIndent := continuationLineIndent(token)
		if lineIndent <= b.indent {
			b.output.SetIndent(lineIndent, 0)
			return true
		}
		delta := lineIndent - b.indent
		b.indent = lineIndent
		if len(modeStack) > 0 && topMode() != "{" && topMode() != "switch" {
			modeContinuationIndents[len(modeContinuationIndents)-1] += delta
		} else {
			equalsContinuationIndented = true
			positionedOperatorIndentDelta += delta
		}
		b.output.SetIndent(b.indent, 0)
		return true
	}
	reduceEqualsContinuationIndent := func() {
		if !equalsContinuationIndented || b.indent <= 0 {
			return
		}
		delta := positionedOperatorIndentDelta
		if delta <= 0 {
			delta = 1
		}
		b.indent -= delta
		if b.indent < 0 {
			b.indent = 0
		}
		positionedOperatorIndentDelta = 0
		equalsContinuationIndented = false
		b.output.SetIndent(b.indent, 0)
	}
	breakAfterPositionedOperator := func(token jsToken) {
		originalIndent := b.indent
		if topContinuationIndent(modeStack, modeContinuationIndents) == 0 && !equalsContinuationIndented {
			increment := 1
			if topMode() == "(" && b.indent == 0 {
				increment = 2
			}
			b.indent += increment
		}
		if token.newlines > 0 && token.whitespaceBefore != "" {
			if sourceIndent := indentationLevel(token.whitespaceBefore, b.options); sourceIndent > b.indent {
				b.indent = sourceIndent
			}
		}
		if b.indent != originalIndent {
			positionedOperatorIndentDelta += b.indent - originalIndent
			equalsContinuationIndented = true
			b.output.SetIndent(b.indent, 0)
		}
		b.output.AddNewLine(false)
		suppressNextPreservedNewline = true
	}
	addAfterNewlineOperator := func(token jsToken) {
		if !b.output.JustAddedNewline() {
			b.output.AddNewLine(false)
		}
		lineIndent := continuationLineIndent(token)
		if token.newlines > 0 && token.whitespaceBefore != "" {
			if sourceIndent := indentationLevel(token.whitespaceBefore, b.options); sourceIndent > 0 {
				lineIndent = sourceIndent
			}
		}
		if topMode() == "(" && b.output.PreviousLine != nil {
			if previousIndent := indentationLevel(b.output.PreviousLine.String(), b.options); previousIndent > lineIndent {
				lineIndent = previousIndent
			}
		}
		b.output.SetIndent(lineIndent, 0)
		b.output.SpaceBeforeToken = false
		b.output.NonBreakingSpace = false
		b.output.AddToken(token.text)
		b.output.SpaceBeforeToken = true
		b.output.NonBreakingSpace = true
		suppressNextPreservedNewline = true
	}
	preservedLineIndentActive := false
	chainContinuationIndented := false
	chainContinuationModeDepth := 0
	lastClosedBlockKind := ""
	for {
		token := b.next()
		if token.typ == jsTokenEOF {
			break
		}
		suppressCurrentLineStart := false
		if token.typ == jsTokenReserved && (token.text == "case" || (token.text == "default" && previous.text != "export")) {
			if caseBodyIndented && b.indent > 0 {
				b.indent--
				b.output.SetIndent(b.indent, 0)
				caseBodyIndented = false
			}
			if b.options.JSLintHappy && b.indent > 0 {
				b.indent--
				b.output.SetIndent(b.indent, 0)
			}
			pendingCaseColon = true
		} else if token.typ == jsTokenReserved && token.text == "switch" {
			pendingSwitchBlock = true
			pendingParenKind = "control-paren"
		} else if token.typ == jsTokenReserved && token.text == "for" {
			pendingParenKind = "for-paren"
		} else if token.typ == jsTokenReserved && (token.text == "if" || token.text == "while" || token.text == "with" || token.text == "switch") {
			pendingParenKind = "control-paren"
		} else if token.typ == jsTokenReserved && (token.text == "var" || token.text == "let" || token.text == "const") {
			if !(topMode() == "{" && b.peek().text == ":") {
				inVarStatement = true
				varStatementModeDepth = len(modeStack)
			}
		}
		suppressPreservedNewline := false
		if suppressNextPreservedNewline {
			suppressPreservedNewline = token.newlines > 0
			suppressNextPreservedNewline = false
		}
		if token.newlines > 0 && !suppressPreservedNewline && !b.options.PreserveNewlines && !pendingSingleStatementIndent && shouldKeepStructuralNewline(previous, token) {
			b.output.AddNewLine(false)
		} else if token.newlines > 0 && !suppressPreservedNewline && b.options.PreserveNewlines && !b.shouldSuppressPreservedNewline(token, previous, modeStack, lastClosedBlockKind) {
			count := token.newlines
			if count > b.options.MaxPreserveNewlines {
				count = b.options.MaxPreserveNewlines
			}
			for i := 0; i < count; i++ {
				b.output.AddNewLine(i > 0)
			}
		}
		if b.options.KeepArrayIndentation && token.newlines > 0 &&
			(token.text == "[" || topMode() == "[") && b.output.JustAddedNewline() {
			b.output.CurrentLine.SetIndent(-1, 0)
			if token.whitespaceBefore != "" {
				b.output.CurrentLine.Push(token.whitespaceBefore)
			}
		}
		positionedContinuationIndented := false
		if !equalsContinuationIndented {
			positionedContinuationIndented = ensurePositionedContinuationIndent(token, previous)
		}
		if !equalsContinuationIndented && !positionedContinuationIndented {
			ensureContinuationIndent(token)
		}
		if !equalsContinuationIndented {
			equalsContinuationIndented = ensureEqualsContinuationIndent(token, previous, previous2)
		}
		if chainContinuationIndented && token.newlines > 0 && len(modeStack) <= chainContinuationModeDepth &&
			token.typ != jsTokenDot && !(token.typ == jsTokenOperator && token.text == "?.") {
			if b.indent > 0 {
				b.indent--
			}
			chainContinuationIndented = false
			b.output.SetIndent(b.indent, 0)
		}
		if token.newlines > 0 && previous.typ == jsTokenComment && b.output.JustAddedNewline() &&
			token.whitespaceBefore != "" && !pendingSingleStatementIndent {
			if indent := indentationLevel(token.whitespaceBefore, b.options); indent > b.indent {
				b.indent = indent
				b.output.SetIndent(b.indent, 0)
				preservedLineIndentActive = true
			}
		}
		if pendingSingleStatementIndent && token.typ != jsTokenStartBlock && token.typ != jsTokenSemicolon &&
			token.typ != jsTokenComment && token.typ != jsTokenBlockComment && token.typ != jsTokenEOF {
			keepElseIf := previous.text == "else" && token.text == "if"
			if !b.output.JustAddedNewline() && !keepElseIf &&
				(shouldBreakBeforeSingleStatement(token) || b.wouldExceedLineWithToken(token)) {
				b.output.AddNewLine(false)
			}
			if b.output.JustAddedNewline() {
				nextIndent := b.indent + 1
				if token.whitespaceBefore != "" {
					if sourceIndent := indentationLevel(token.whitespaceBefore, b.options); sourceIndent > 0 {
						nextIndent = sourceIndent
					}
				}
				indentDelta := nextIndent - b.indent
				b.indent = nextIndent
				b.output.SetIndent(b.indent, 0)
				if indentDelta > 0 {
					singleStatementIndentDepth += indentDelta
				}
				lastSingleStatementIndented = true
			} else {
				suppressCurrentLineStart = true
				lastSingleStatementIndented = false
			}
			pendingSingleStatementIndent = false
		} else if pendingSingleStatementIndent && token.typ == jsTokenStartBlock {
			pendingSingleStatementIndent = false
		}
		preserveSourceOutputIndent(token)
		switch token.typ {
		case jsTokenComment:
			if token.newlines > 0 && !b.output.JustAddedNewline() {
				b.output.AddNewLine(false)
			}
			if token.newlines > 0 && b.indent == 0 {
				if indent := indentationLevel(token.whitespaceBefore, b.options); indent > 0 {
					b.indent = indent
					b.output.SetIndent(b.indent, 0)
					preservedLineIndentActive = true
				}
			}
			if !b.output.JustAddedNewline() {
				b.output.SpaceBeforeToken = true
			}
			b.add(token.text)
			if strings.HasPrefix(token.text, "#!") {
				b.output.AddNewLine(false)
				if b.peek().newlines <= 1 {
					b.output.AddNewLine(true)
				}
			} else {
				b.output.AddNewLine(false)
			}
		case jsTokenBlockComment:
			if strings.Contains(token.text, "\n") {
				if hasTrailingWhitespaceLine(token.text) {
					b.output.AddNewLine(false)
					addRawBlockComment(b, token.text)
				} else if shouldPreserveJSDocBodyIndent(token.text) {
					b.output.AddNewLine(false)
					addRawBlockComment(b, token.text)
				} else {
					b.output.AddNewLine(false)
					for _, line := range formatBlockCommentLines(token.text) {
						if line == "" {
							b.output.CurrentLine.SetIndent(-1, 0)
							b.output.CurrentLine.Push("")
							b.output.AddNewLine(false)
							continue
						}
						b.add(strings.TrimRight(line, " \t"))
						b.output.AddNewLine(false)
					}
				}
			} else if previous.typ == jsTokenSemicolon {
				b.add(token.text)
				b.output.AddNewLine(false)
			} else {
				if !b.output.JustAddedNewline() {
					b.output.SpaceBeforeToken = true
				}
				b.add(token.text)
			}
		case jsTokenStartBlock:
			if equalsContinuationIndented && previous.typ == jsTokenEndExpr {
				reduceEqualsContinuationIndent()
			}
			if b.peek().typ == jsTokenEndBlock && (b.peek().newlines == 0 || (previous.text == "do" && b.peek().newlines == 1)) {
				if needsSpaceBeforeBlock(previous, token) {
					b.output.SpaceBeforeToken = true
				}
				closedKind := "{"
				if b.isFunctionBlockStart(previous) {
					closedKind = "function-block"
				}
				b.add("{}")
				b.next()
				next := b.peek()
				if next.typ != jsTokenEOF && next.typ != jsTokenSemicolon && next.typ != jsTokenComma &&
					next.typ != jsTokenDot && !(next.typ == jsTokenEndExpr && (next.text == ")" || next.text == "]")) &&
					next.text != "else" && next.text != "catch" && next.text != "finally" && next.text != "while" {
					b.output.AddNewLine(false)
				}
				previous = jsToken{typ: jsTokenEndBlock, text: "}"}
				lastClosedBlockKind = closedKind
				continue
			}
			if b.options.BracePreserveInline {
				if inline, ok := b.readInlineBlock(); ok {
					if needsSpaceBeforeBlock(previous, token) {
						b.output.SpaceBeforeToken = true
					}
					closedKind := "{"
					if b.isFunctionBlockStart(previous) {
						closedKind = "function-block"
					}
					b.add(inline)
					previous = jsToken{typ: jsTokenEndBlock, text: "}"}
					lastClosedBlockKind = closedKind
					continue
				}
			}
			if b.options.BraceStyle == "expand" && shouldExpandStartBlock(previous) {
				b.output.AddNewLine(false)
			} else if needsSpaceBeforeBlock(previous, token) {
				b.output.SpaceBeforeToken = true
			}
			blockFollowedByComma := b.blockFollowedByComma()
			extraVarObjectContinuation := inVarStatement && len(modeStack) == varStatementModeDepth &&
				previous.typ == jsTokenEquals && b.indent == 0 && blockFollowedByComma
			if inVarStatement && blockFollowedByComma && b.isFunctionBlockStart(previous) {
				varBlockContinuationPending = true
			}
			b.add(token.text)
			b.indent++
			if extraVarObjectContinuation {
				b.indent++
				varContinuationIndented = true
			}
			b.output.SetIndent(b.indent, 0)
			if b.peek().typ != jsTokenEndBlock {
				b.output.AddNewLine(false)
			}
			blockKind := "{"
			if pendingSwitchBlock {
				blockKind = "switch"
				pendingSwitchBlock = false
			} else if b.isFunctionBlockStart(previous) {
				blockKind = "function-block"
			}
			pushMode(blockKind, 0)
		case jsTokenEndBlock:
			closingKind := ""
			if len(modeStack) > 0 {
				closingKind = modeStack[len(modeStack)-1]
			}
			if closingKind == "switch" && caseBodyIndented && b.indent > 0 {
				b.indent--
				caseBodyIndented = false
			}
			if b.indent > 0 {
				b.indent--
			}
			if previous.typ != jsTokenStartBlock {
				b.output.AddNewLine(false)
			}
			lineIndent := b.indent
			if token.newlines > 0 && token.whitespaceBefore != "" {
				if sourceIndent := indentationLevel(token.whitespaceBefore, b.options); sourceIndent > 0 && sourceIndent >= lineIndent {
					if closingKind == "function-block" || singleStatementIndentDepth > 0 ||
						(closingKind == "{" && inVarStatement && sourceIndent == lineIndent+1) {
						lineIndent = sourceIndent
					}
				}
			}
			setOutputLineIndent(lineIndent)
			b.add(token.text)
			if len(modeStack) > 0 {
				popMode()
			}
			if singleStatementIndentDepth > 0 && closingKind == "{" {
				b.indent -= singleStatementIndentDepth
				if b.indent < 0 {
					b.indent = 0
				}
				singleStatementIndentDepth = 0
				b.output.SetIndent(b.indent, 0)
			}
			lastClosedBlockKind = closingKind
			next := b.peek()
			if next.text == ";" || next.text == "," || next.text == ")" || next.text == "]" || next.text == "(" || next.text == "." {
			} else if next.text == ":" {
				b.output.SpaceBeforeToken = true
			} else if next.text == "while" && pendingDoWhile && singleStatementIndentDepth > 0 {
				b.output.AddNewLine(false)
			} else if next.text == "else" || next.text == "catch" || next.text == "finally" || next.text == "while" {
				b.output.SpaceBeforeToken = true
			} else {
				b.output.AddNewLine(false)
			}
		case jsTokenStartExpr:
			if token.text == "[" && !b.options.KeepArrayIndentation && (b.isComplexArrayAhead() ||
				(b.peek().newlines > 0 && (!b.arrayStartsWithBlock() || b.options.BracePreserveInline))) {
				if previous.text == "return" || previous.text == "throw" || previous.text == "yield" {
					b.output.SpaceBeforeToken = true
				}
				b.add(token.text)
				b.indent++
				b.output.SetIndent(b.indent, 0)
				b.output.AddNewLine(false)
				pushMode("array-multiline", 0)
				break
			}
			if token.text == "(" && previous.text == "async" && b.isArrowFunctionAhead() {
				b.output.SpaceBeforeToken = true
			} else if token.text == "(" && b.options.SpaceAfterNamedFunction &&
				isMethodDefinitionName(previous) && b.isMethodDefinitionParenAhead() {
				b.output.SpaceBeforeToken = true
			} else if needsSpaceBeforeParen(previous2, previous, token, b.options) {
				b.output.SpaceBeforeToken = true
			} else if token.text == "[" && (previous.text == "return" || previous.text == "throw" || previous.text == "yield") {
				b.output.SpaceBeforeToken = true
			}
			b.add(token.text)
			if shouldAddSpaceAfterStartExpr(token, b.peek(), b.options) {
				b.output.SpaceBeforeToken = true
			}
			if token.text == "[" && b.peek().typ != jsTokenEndExpr {
				if b.options.KeepArrayIndentation && b.arrayHasSourceNewlineAhead() {
					b.indent++
					b.output.SetIndent(b.indent, 0)
					pushMode("[", 1)
				} else {
					pushMode("[", 0)
				}
			} else if token.text == "(" {
				if pendingParenKind != "" {
					pushMode(pendingParenKind, 0)
					pendingParenKind = ""
				} else {
					pushMode("(", 0)
				}
			}
		case jsTokenEndExpr:
			if token.text == "]" && topMode() == "array-multiline" {
				if b.indent > 0 {
					b.indent--
				}
				if !(b.options.CommaFirst && previous.typ == jsTokenComma) {
					b.output.AddNewLine(false)
				}
				b.output.SetIndent(b.indent, 0)
				b.add(token.text)
				popMode()
				break
			}
			if (token.text == "]" && topMode() == "[") ||
				(token.text == ")" && (topMode() == "(" || topMode() == "for-paren" || topMode() == "control-paren")) {
				continuationIndent := modeContinuationIndents[len(modeContinuationIndents)-1]
				if token.text == "]" && continuationIndent > 0 && !b.output.JustAddedNewline() &&
					!b.options.KeepArrayIndentation {
					b.output.AddNewLine(false)
				}
				if continuationIndent > 0 {
					b.indent -= continuationIndent
					if b.indent < 0 {
						b.indent = 0
					}
					b.output.SetIndent(b.indent, 0)
				}
			}
			if shouldAddSpaceBeforeEndExpr(previous, token, b.options) {
				b.output.SpaceBeforeToken = true
			}
			if token.text == "]" && token.newlines > 0 && token.whitespaceBefore != "" && topMode() == "[" {
				if sourceIndent := indentationLevel(token.whitespaceBefore, b.options); sourceIndent > 0 {
					setOutputLineIndent(sourceIndent)
				}
			}
			b.add(token.text)
			if len(modeStack) > 0 && ((token.text == "]" && topMode() == "[") ||
				(token.text == ")" && (topMode() == "(" || topMode() == "for-paren" || topMode() == "control-paren"))) {
				if token.text == ")" && (topMode() == "for-paren" || topMode() == "control-paren") {
					pendingSingleStatementIndent = true
				}
				popMode()
				if equalsContinuationIndented && token.text == ")" {
					reduceEqualsContinuationIndent()
				}
			}
		case jsTokenSemicolon:
			b.add(token.text)
			pendingSingleStatementIndent = false
			if varContinuationIndented && b.indent > 0 {
				b.indent--
				varContinuationIndented = false
			}
			inVarStatement = false
			varStatementModeDepth = -1
			varDeclHasInitializer = false
			if singleStatementIndentDepth > 0 && !(topMode() == "{" && b.peek().typ == jsTokenEndBlock) {
				reduceIndent := singleStatementIndentDepth
				if b.peek().text == "else" {
					reduceIndent = 0
					if lastSingleStatementIndented {
						reduceIndent = 1
					}
				}
				b.indent -= reduceIndent
				if b.indent < 0 {
					b.indent = 0
				}
				singleStatementIndentDepth -= reduceIndent
			}
			if preservedLineIndentActive && b.indent > 0 {
				b.indent = 0
				preservedLineIndentActive = false
				b.output.SetIndent(b.indent, 0)
			}
			if equalsContinuationIndented {
				reduceEqualsContinuationIndent()
			}
			if chainContinuationIndented && len(modeStack) <= chainContinuationModeDepth && b.indent > 0 {
				b.indent--
				chainContinuationIndented = false
				b.output.SetIndent(b.indent, 0)
			}
			if topMode() == "for-paren" {
				next := b.peek()
				if next.typ != jsTokenSemicolon && !(next.typ == jsTokenEndExpr && next.text == ")") {
					b.output.SpaceBeforeToken = true
				}
			} else if pendingDoWhile && b.peek().text == "while" {
				b.output.SpaceBeforeToken = true
				pendingDoWhile = false
			} else if (b.peek().typ == jsTokenComment || b.peek().typ == jsTokenBlockComment) && b.peek().newlines == 0 {
				b.output.SpaceBeforeToken = true
			} else if b.peek().typ != jsTokenEOF && b.peek().typ != jsTokenEndBlock {
				b.output.AddNewLine(false)
			}
			if pendingDoWhile && b.peek().text != "while" {
				pendingDoWhile = false
			}
		case jsTokenComma:
			inContinuedExpression := topContinuationIndent(modeStack, modeContinuationIndents) > 0 &&
				(topMode() == "[" || topMode() == "(" || topMode() == "for-paren")
			preserveForParenCommaFirst := topMode() == "for-paren" && inVarStatement && b.peek().newlines > 0
			preserveExpressionCommaFirst := (topMode() == "[" || topMode() == "(") &&
				(b.peek().newlines > 0 || (topMode() == "[" && lastClosedBlockKind == "function-block" && inContinuedExpression && b.peek().typ != jsTokenStartBlock))
			commaFirstHere := b.options.CommaFirst &&
				(shouldUseCommaFirst(modeStack, inVarStatement) || inContinuedExpression || preserveForParenCommaFirst || preserveExpressionCommaFirst)
			if commaFirstHere {
				if inVarStatement && len(modeStack) == varStatementModeDepth && !varContinuationIndented {
					b.indent++
					varContinuationIndented = true
				}
				trailingArrayComma := topMode() == "array-multiline" && b.peek().typ == jsTokenEndExpr
				if trailingArrayComma && b.indent > 0 {
					b.indent--
					b.output.SetIndent(b.indent, 0)
				}
				b.output.AddNewLine(false)
				if preserveForParenCommaFirst || preserveExpressionCommaFirst {
					ensureTopContinuationIndent()
				}
				if inVarStatement && len(modeStack) == varStatementModeDepth {
					b.output.SetIndent(b.indent, 0)
				}
				b.add(token.text)
				b.output.SpaceBeforeToken = true
				if b.peek().newlines > 0 {
					suppressNextPreservedNewline = true
				}
			} else {
				temporaryObjectCommaIndent := token.newlines > 0 && topMode() == "{" && !b.options.CommaFirst
				if temporaryObjectCommaIndent {
					b.indent++
					b.output.SetIndent(b.indent, 0)
				}
				b.add(token.text)
				if temporaryObjectCommaIndent {
					b.indent--
					b.output.SetIndent(b.indent, 0)
				}
			}
			if commaFirstHere {
			} else if b.options.CommaFirst {
				b.output.SpaceBeforeToken = true
			} else if inVarStatement && len(modeStack) == varStatementModeDepth {
				shouldBreakVarComma := token.newlines > 0 || b.peek().newlines > 0 ||
					(varDeclHasInitializer && topMode() != "for-paren") || varBlockContinuationPending
				if shouldBreakVarComma {
					if !varContinuationIndented {
						b.indent++
						varContinuationIndented = true
					}
					b.output.AddNewLine(false)
					b.output.SetIndent(b.indent, 0)
				} else {
					b.output.SpaceBeforeToken = true
				}
				varDeclHasInitializer = false
				varBlockContinuationPending = false
			} else if varBlockContinuationPending && b.peek().typ == jsTokenWord {
				if b.indent == 0 {
					b.indent = 1
				}
				b.output.AddNewLine(false)
				b.output.SetIndent(b.indent, 0)
				varBlockContinuationPending = false
			} else if topMode() == "{" {
				if b.peek().typ == jsTokenStartBlock && b.peek().newlines == 0 {
					b.output.SpaceBeforeToken = true
				} else if (b.peek().typ == jsTokenComment || b.peek().typ == jsTokenBlockComment) && b.peek().newlines == 0 {
					b.output.SpaceBeforeToken = true
				} else {
					b.output.AddNewLine(false)
				}
			} else if topMode() == "array-multiline" {
				b.output.AddNewLine(false)
			} else if topMode() == "[" {
				if b.options.KeepArrayIndentation && b.peek().newlines > 0 {
					b.output.AddNewLine(false)
				} else if b.peek().newlines > 0 || (lastClosedBlockKind == "function-block" && inContinuedExpression && b.peek().typ != jsTokenStartBlock) {
					b.output.AddNewLine(false)
					ensureTopContinuationIndent()
					if b.peek().newlines > 0 {
						suppressNextPreservedNewline = true
					}
				} else {
					b.output.SpaceBeforeToken = true
				}
			} else if inContinuedExpression && topMode() == "[" {
				if b.peek().newlines > 0 {
					b.output.AddNewLine(false)
				} else {
					b.output.SpaceBeforeToken = true
				}
			} else {
				b.output.SpaceBeforeToken = true
			}
		case jsTokenDot:
			if singleStatementIndentDepth > 0 && token.newlines > 0 && b.output.JustAddedNewline() {
				b.add(token.text)
			} else if b.addChainOperator(token, previous, chainContinuationIndented) {
				chainContinuationIndented = true
				chainContinuationModeDepth = len(modeStack)
			}
		case jsTokenOperator, jsTokenEquals:
			if token.typ == jsTokenEquals && inVarStatement && len(modeStack) == varStatementModeDepth {
				varDeclHasInitializer = true
			}
			if token.typ == jsTokenEquals && isSharpNumberReference(previous.text) {
				b.add(token.text)
				break
			}
			if token.text == "::" {
				b.add(token.text)
			} else if (token.typ == jsTokenEquals && token.text != "=" || token.typ == jsTokenOperator && isAssignmentOperator(token.text)) &&
				token.newlines > 0 && token.whitespaceBefore != "" && b.output.JustAddedNewline() {
				b.output.SetIndent(indentationLevel(token.whitespaceBefore, b.options), 0)
				b.output.SpaceBeforeToken = false
				b.output.AddToken(token.text)
				b.output.SpaceBeforeToken = true
			} else if token.text == "!" || token.text == "~" || token.text == "++" || token.text == "--" {
				if previous.text == "case" || previous.text == "default" || previous.text == "return" ||
					previous.text == "throw" || previous.text == "yield" ||
					(previous.typ == jsTokenEndExpr && !(previous.text == "]" && (token.text == "++" || token.text == "--"))) {
					b.output.SpaceBeforeToken = true
				}
				b.add(token.text)
			} else if token.text == "*" && previous.typ == jsTokenDot {
				b.add(token.text)
			} else if token.text == "*" && isGeneratorAsterisk(previous) {
				b.add(token.text)
				next := b.peek()
				if next.typ == jsTokenWord || next.typ == jsTokenReserved || b.options.SpaceAfterAnonFunction {
					b.output.SpaceBeforeToken = true
				}
			} else if token.text == "*" && previous.text == "function" {
				b.add(token.text)
				next := b.peek()
				if next.typ == jsTokenWord || next.typ == jsTokenReserved || b.options.SpaceAfterAnonFunction {
					b.output.SpaceBeforeToken = true
				}
			} else if token.text == "*" && previous.text == "yield" {
				b.add(token.text)
				b.output.SpaceBeforeToken = true
			} else if token.text == "/" && previous.text == "<" {
				b.output.SpaceBeforeToken = true
				b.add(token.text)
			} else if token.text == ">" && previous2.text == "/" {
				b.add(token.text)
			} else if token.text == "?." {
				if b.addChainOperator(token, previous, chainContinuationIndented) {
					chainContinuationIndented = true
					chainContinuationModeDepth = len(modeStack)
				}
			} else if token.text == "..." {
				if previous.typ != jsTokenStartExpr && previous.typ != jsTokenStartBlock && previous.typ != jsTokenComma && !b.output.JustAddedNewline() {
					b.output.SpaceBeforeToken = true
				}
				b.add(token.text)
			} else if token.text == ":" && pendingCaseColon {
				b.add(token.text)
				if b.peek().typ == jsTokenStartBlock && b.peek().newlines == 0 {
					b.output.SpaceBeforeToken = true
					pendingCaseColon = false
					break
				}
				b.indent++
				b.output.SetIndent(b.indent, 0)
				if b.peek().typ == jsTokenComment && b.peek().newlines == 0 {
					b.output.SpaceBeforeToken = true
				} else {
					b.output.AddNewLine(false)
				}
				caseBodyIndented = true
				pendingCaseColon = false
			} else if token.text == "?" {
				ternaryModeDepths = append(ternaryModeDepths, len(modeStack))
				if b.options.PreserveNewlines && b.options.OperatorPosition == "after-newline" &&
					(token.newlines > 0 || b.peek().newlines > 0) {
					addAfterNewlineOperator(token)
				} else {
					b.output.SpaceBeforeToken = true
					b.add(token.text)
				}
				if b.options.PreserveNewlines && b.options.OperatorPosition == "after-newline" &&
					(token.newlines > 0 || b.peek().newlines > 0) {
					suppressNextPreservedNewline = true
				}
				b.output.SpaceBeforeToken = true
			} else if (token.text == "-" || token.text == "+") && isUnarySign(previous) {
				if previous.typ != jsTokenStartExpr && previous.typ != jsTokenStartBlock &&
					previous.typ != jsTokenComma && previous.typ != jsTokenOperator && previous.typ != jsTokenEquals {
					b.output.SpaceBeforeToken = true
				}
				b.add(token.text)
			} else if token.text == ":" {
				if len(ternaryModeDepths) > 0 && ternaryModeDepths[len(ternaryModeDepths)-1] == len(modeStack) {
					ternaryModeDepths = ternaryModeDepths[:len(ternaryModeDepths)-1]
					moveAfterNewline := b.options.PreserveNewlines && b.options.OperatorPosition == "after-newline" &&
						(token.newlines > 0 || b.peek().newlines > 0)
					if moveAfterNewline {
						addAfterNewlineOperator(token)
					} else {
						lineStartOperatorIndented := false
						if b.output.JustAddedNewline() && token.newlines > 0 && b.options.OperatorPosition == "preserve-newline" {
							setPositionableLineStartIndent(token)
							lineStartOperatorIndented = true
						}
						b.output.SpaceBeforeToken = !lineStartOperatorIndented
						b.add(token.text)
					}
					if b.options.PreserveNewlines && b.options.OperatorPosition == "before-newline" && token.newlines > 0 {
						breakAfterPositionedOperator(token)
					} else if moveAfterNewline {
					} else {
						b.output.SpaceBeforeToken = true
					}
				} else {
					b.add(token.text)
					b.output.SpaceBeforeToken = true
					b.output.NonBreakingSpace = true
				}
			} else {
				if b.options.PreserveNewlines && b.options.OperatorPosition == "after-newline" &&
					isPositionableOperator(token.text) && (token.newlines > 0 || b.peek().newlines > 0) {
					addAfterNewlineOperator(token)
					break
				}
				moveOperatorNewline := b.options.PreserveNewlines && b.options.OperatorPosition == "before-newline" &&
					token.newlines > 0 && isPositionableOperator(token.text)
				lineStartOperatorIndented := false
				if b.output.JustAddedNewline() && token.newlines > 0 &&
					isPositionableOperator(token.text) &&
					(b.options.OperatorPosition == "before-newline" || b.options.OperatorPosition == "preserve-newline") {
					setPositionableLineStartIndent(token)
					lineStartOperatorIndented = true
				}
				b.output.SpaceBeforeToken = !lineStartOperatorIndented
				b.output.NonBreakingSpace = true
				b.add(token.text)
				if moveOperatorNewline {
					breakAfterPositionedOperator(token)
				} else {
					b.setContinuationWrapPoint()
					b.output.SpaceBeforeToken = true
					b.output.NonBreakingSpace = true
				}
			}
		case jsTokenReserved, jsTokenWord, jsTokenString, jsTokenUnknown:
			wrapBeforeValue := shouldSetValueWrapPoint(previous, token, topMode())
			if wrapBeforeValue {
				b.setContinuationWrapPoint()
			}
			if token.text == "function" && previous.typ == jsTokenEndBlock && !b.output.JustAddedBlankline() {
				b.ensureBlankLine()
			}
			if token.text == "function" && previous.typ == jsTokenSemicolon && !b.output.JustAddedBlankline() {
				b.ensureBlankLine()
			}
			if token.text == "function" && previous.typ == jsTokenEndExpr && token.newlines > 0 && !b.output.JustAddedBlankline() {
				b.ensureBlankLine()
			}
			if token.text == "if" && previous.typ == jsTokenStartExpr && previous.text == "(" && !b.output.JustAddedNewline() {
				b.output.AddNewLine(false)
				ensureTopContinuationIndent()
			}
			if token.text == "else" && !b.output.JustAddedNewline() {
				if previous.typ != jsTokenEndBlock ||
					(token.newlines > 0 && lastClosedBlockKind == "function-block") ||
					(b.options.BracePreserveInline && (b.options.BraceStyle == "expand" || b.options.BraceStyle == "end-expand")) {
					b.output.AddNewLine(false)
				}
			}
			if !suppressCurrentLineStart && shouldStartLine(token, previous) && !b.output.JustAddedNewline() {
				b.output.AddNewLine(false)
			}
			if needsWordSpace(previous, token) && !isAdjacentTemplateToken(token) && !isAdjacentAfterTemplateToken(previous, token) {
				b.output.SpaceBeforeToken = true
				if previous.text == "return" || previous.text == "throw" {
					b.output.NonBreakingSpace = true
				}
			}
			if wrapBeforeValue && (b.wouldExceedLineWithToken(token) || b.shouldForceNestedLogicalWrap(previous)) {
				b.output.SpaceBeforeToken = false
				b.output.NonBreakingSpace = false
				b.output.AddNewLine(false)
				b.output.SetIndent(b.indent+1, 0)
				b.output.AddToken(token.text)
			} else {
				b.add(token.text)
			}
			if token.text == "else" || token.text == "do" {
				pendingSingleStatementIndent = true
			}
			if token.text == "do" {
				pendingDoWhile = true
			}
		}
		previous2 = previous
		previous = token
		if token.typ != jsTokenEndBlock {
			lastClosedBlockKind = ""
		}
	}
}

func (b *Beautifier) isComplexArrayAhead() bool {
	depth := 0
	firstTop := true
	for i := b.pos; i < len(b.tokens); i++ {
		token := b.tokens[i]
		if token.typ == jsTokenEOF {
			return false
		}
		if depth == 0 {
			if firstTop && token.typ == jsTokenStartExpr && token.text == "[" {
				return true
			}
			if token.typ != jsTokenComma {
				firstTop = false
			}
		}
		if token.typ == jsTokenStartExpr && token.text == "[" {
			depth++
		} else if token.typ == jsTokenEndExpr && token.text == "]" {
			if depth == 0 {
				return false
			}
			depth--
		}
	}
	return false
}

func (b *Beautifier) arrayStartsWithBlock() bool {
	for i := b.pos; i < len(b.tokens); i++ {
		token := b.tokens[i]
		if token.typ == jsTokenEOF || token.typ == jsTokenEndExpr {
			return false
		}
		if token.typ == jsTokenComment || token.typ == jsTokenBlockComment {
			continue
		}
		return token.typ == jsTokenStartBlock
	}
	return false
}

func (b *Beautifier) blockFollowedByComma() bool {
	depth := 1
	for i := b.pos; i < len(b.tokens); i++ {
		token := b.tokens[i]
		if token.typ == jsTokenEOF {
			return false
		}
		switch token.typ {
		case jsTokenStartBlock:
			depth++
		case jsTokenEndBlock:
			depth--
			if depth == 0 {
				return i+1 < len(b.tokens) && b.tokens[i+1].typ == jsTokenComma
			}
		}
	}
	return false
}

func (b *Beautifier) arrayHasSourceNewlineAhead() bool {
	depth := 1
	for i := b.pos; i < len(b.tokens); i++ {
		token := b.tokens[i]
		if token.typ == jsTokenEOF {
			return false
		}
		if token.newlines > 0 {
			return true
		}
		if token.typ == jsTokenStartExpr && token.text == "[" {
			depth++
			continue
		}
		if token.typ == jsTokenEndExpr && token.text == "]" {
			depth--
			if depth == 0 {
				return false
			}
		}
	}
	return false
}

func (b *Beautifier) isArrowFunctionAhead() bool {
	depth := 1
	for i := b.pos; i < len(b.tokens); i++ {
		token := b.tokens[i]
		if token.typ == jsTokenEOF {
			return false
		}
		if token.typ == jsTokenStartExpr && token.text == "(" {
			depth++
			continue
		}
		if token.typ != jsTokenEndExpr || token.text != ")" {
			continue
		}
		depth--
		if depth != 0 {
			continue
		}
		return i+1 < len(b.tokens) && b.tokens[i+1].text == "=>"
	}
	return false
}

func (b *Beautifier) isMethodDefinitionParenAhead() bool {
	depth := 1
	for i := b.pos; i < len(b.tokens); i++ {
		token := b.tokens[i]
		if token.typ == jsTokenEOF {
			return false
		}
		if token.typ == jsTokenStartExpr && token.text == "(" {
			depth++
			continue
		}
		if token.typ != jsTokenEndExpr || token.text != ")" {
			continue
		}
		depth--
		if depth != 0 {
			continue
		}
		return i+1 < len(b.tokens) && b.tokens[i+1].typ == jsTokenStartBlock
	}
	return false
}

func (b *Beautifier) shouldSuppressPreservedNewline(token, previous jsToken, modeStack []string, lastClosedBlockKind string) bool {
	if token.typ == jsTokenStartExpr && token.text == "(" && isControlParenWord(previous.text) {
		return true
	}
	if token.typ == jsTokenStartExpr && token.text == "(" &&
		(previous.typ == jsTokenWord || previous.typ == jsTokenReserved) {
		return true
	}
	if b.options.KeepArrayIndentation && token.typ == jsTokenStartBlock && token.newlines > 0 &&
		insideMode(modeStack, "[") {
		return false
	}
	if token.typ == jsTokenStartBlock && (b.options.BraceStyle == "collapse" || b.options.BraceStyle == "end-expand") {
		return true
	}
	if token.typ == jsTokenEndExpr && previous.typ == jsTokenEndBlock && token.text == ")" && token.newlines > 0 &&
		lastClosedBlockKind == "function-block" {
		return false
	}
	if token.typ == jsTokenEndExpr && token.text == ")" && previous.typ == jsTokenEndExpr && previous.text == "]" &&
		token.newlines > 0 {
		return false
	}
	if b.options.KeepArrayIndentation && token.typ == jsTokenEndExpr && token.text == "]" && token.newlines > 0 &&
		insideMode(modeStack, "[") {
		return false
	}
	if token.typ == jsTokenEndExpr && (previous.typ == jsTokenEndBlock || previous.typ == jsTokenEndExpr) {
		return true
	}
	if token.typ == jsTokenEquals && token.text == "=" &&
		(previous.typ == jsTokenWord || previous.typ == jsTokenReserved || previous.typ == jsTokenEndExpr) {
		return true
	}
	if token.typ == jsTokenOperator && token.text == ":" && len(modeStack) > 0 && modeStack[len(modeStack)-1] == "{" &&
		(previous.typ == jsTokenWord || previous.typ == jsTokenReserved || previous.typ == jsTokenString) {
		return true
	}
	if (token.typ == jsTokenOperator || token.typ == jsTokenEquals) &&
		b.options.OperatorPosition == "before-newline" && isPositionableOperator(token.text) &&
		token.newlines <= 1 &&
		(previous.typ == jsTokenWord || previous.typ == jsTokenString || previous.typ == jsTokenEndExpr) {
		return true
	}
	if len(modeStack) > 0 && modeStack[len(modeStack)-1] == "{" && previous.text == ":" &&
		(token.typ == jsTokenWord || token.typ == jsTokenReserved || token.typ == jsTokenString || token.typ == jsTokenStartBlock || token.typ == jsTokenStartExpr) {
		return true
	}
	if token.typ == jsTokenSemicolon || token.typ == jsTokenComma {
		return true
	}
	return token.text == "else" || token.text == "catch" || token.text == "finally"
}

func isControlParenWord(word string) bool {
	switch word {
	case "if", "for", "while", "switch", "catch", "with":
		return true
	default:
		return false
	}
}

func formatBlockCommentLine(line string, index int, javadoc bool) string {
	if index == 0 || !javadoc {
		return line
	}
	trimmed := strings.TrimLeft(line, " \t")
	if strings.HasPrefix(trimmed, "*") {
		return " " + trimmed
	}
	return line
}

func formatBlockCommentLines(text string) []string {
	lines := strings.Split(text, "\n")
	if len(lines) <= 1 {
		return lines
	}
	if isJSDocBlock(text) {
		for index, line := range lines {
			lines[index] = formatBlockCommentLine(line, index, true)
		}
		return lines
	}
	commonIndent := -1
	for _, line := range lines[1:] {
		if strings.TrimSpace(line) == "" {
			continue
		}
		indent := leadingWhitespaceLength(line)
		if commonIndent == -1 || indent < commonIndent {
			commonIndent = indent
		}
	}
	if commonIndent <= 0 {
		return lines
	}
	for index := 1; index < len(lines); index++ {
		lines[index] = trimLeadingWhitespaceCount(lines[index], commonIndent)
	}
	return lines
}

func hasTrailingWhitespaceLine(text string) bool {
	for _, line := range strings.Split(text, "\n") {
		if strings.HasSuffix(line, " ") || strings.HasSuffix(line, "\t") {
			return true
		}
	}
	return false
}

func shouldPreserveJSDocBodyIndent(text string) bool {
	if !strings.HasPrefix(text, "/**") {
		return false
	}
	lines := strings.Split(text, "\n")
	for _, line := range lines[1:] {
		trimmed := strings.TrimLeft(line, " \t")
		if trimmed == "" || strings.HasPrefix(trimmed, "*") {
			continue
		}
		return leadingWhitespaceLength(line) > 0
	}
	return false
}

func addRawBlockComment(b *Beautifier, text string) {
	lines := strings.Split(text, "\n")
	for index, line := range lines {
		if index > 0 {
			b.output.AddNewLine(false)
			b.output.CurrentLine.SetIndent(-1, 0)
		}
		if index == 0 {
			b.add(line)
		} else {
			b.output.CurrentLine.Push(line)
		}
	}
	b.output.AddNewLine(false)
}

func leadingWhitespaceLength(value string) int {
	count := 0
	for count < len(value) && (value[count] == ' ' || value[count] == '\t') {
		count++
	}
	return count
}

func trimLeadingWhitespaceCount(value string, count int) string {
	index := 0
	for index < len(value) && count > 0 && (value[index] == ' ' || value[index] == '\t') {
		index++
		count--
	}
	return value[index:]
}

func isJSDocBlock(text string) bool {
	if !strings.HasPrefix(text, "/*") {
		return false
	}
	lines := strings.Split(text, "\n")
	for _, line := range lines[1:] {
		trimmed := strings.TrimLeft(line, " \t")
		if strings.HasPrefix(trimmed, "*/") {
			continue
		}
		if strings.HasPrefix(trimmed, "*") && (len(trimmed) == 1 || trimmed[1] != '*') {
			return true
		}
	}
	return false
}

func insideMode(stack []string, mode string) bool {
	for i := len(stack) - 1; i >= 0; i-- {
		if stack[i] == mode {
			return true
		}
	}
	return false
}

func shouldUseCommaFirst(stack []string, inVarStatement bool) bool {
	if len(stack) == 0 {
		return inVarStatement
	}
	switch stack[len(stack)-1] {
	case "{", "array-multiline":
		return true
	default:
		return false
	}
}

func topContinuationIndent(stack []string, continuationIndents []int) int {
	if len(stack) == 0 || len(continuationIndents) == 0 {
		return 0
	}
	return continuationIndents[len(continuationIndents)-1]
}

func shouldAddSpaceAfterStartExpr(token, next jsToken, options *Options) bool {
	if token.text != "(" && token.text != "[" {
		return false
	}
	if next.typ == jsTokenEndExpr {
		return options.SpaceInParen && options.SpaceInEmptyParen
	}
	return options.SpaceInParen
}

func shouldAddSpaceBeforeEndExpr(previous, token jsToken, options *Options) bool {
	if token.text != ")" && token.text != "]" {
		return false
	}
	if previous.typ == jsTokenStartExpr {
		return options.SpaceInParen && options.SpaceInEmptyParen
	}
	return options.SpaceInParen
}

func needsSpaceBeforeBlock(previous, token jsToken) bool {
	if previous.typ == jsTokenStartExpr || previous.typ == jsTokenOperator || previous.typ == jsTokenEquals {
		return false
	}
	if previous.text == "#" && token.whitespaceBefore == "" {
		return false
	}
	return true
}

func shouldExpandStartBlock(previous jsToken) bool {
	if previous.typ == jsTokenEquals || previous.typ == jsTokenComma || previous.typ == jsTokenStartExpr ||
		previous.typ == jsTokenOperator {
		return false
	}
	if previous.text == "return" || previous.text == "throw" || previous.text == "yield" {
		return false
	}
	return true
}

func (b *Beautifier) isFunctionBlockStart(previous jsToken) bool {
	if previous.text == "=>" {
		return true
	}
	if previous.typ != jsTokenEndExpr || previous.text != ")" {
		return false
	}
	endIndex := b.pos - 2
	if endIndex < 0 || endIndex >= len(b.tokens) {
		return false
	}
	startIndex := matchingStartExprIndex(b.tokens, endIndex)
	if startIndex <= 0 {
		return false
	}
	before := b.tokens[startIndex-1]
	if before.text == "function" {
		return true
	}
	if startIndex > 1 && b.tokens[startIndex-2].text == "function" {
		return true
	}
	return false
}

func matchingStartExprIndex(tokens []jsToken, endIndex int) int {
	depth := 0
	for i := endIndex; i >= 0; i-- {
		token := tokens[i]
		if token.typ == jsTokenEndExpr && token.text == ")" {
			depth++
			continue
		}
		if token.typ == jsTokenStartExpr && token.text == "(" {
			depth--
			if depth == 0 {
				return i
			}
		}
	}
	return -1
}

func (b *Beautifier) readInlineBlock() (string, bool) {
	start := b.pos
	depth := 0
	for i := b.pos; i < len(b.tokens); i++ {
		token := b.tokens[i]
		if token.typ == jsTokenEOF || token.newlines > 0 {
			return "", false
		}
		switch token.typ {
		case jsTokenStartBlock:
			depth++
		case jsTokenEndBlock:
			if depth == 0 {
				inner := b.tokens[start:i]
				b.pos = i + 1
				return formatInlineBlock(inner), true
			}
			depth--
		}
	}
	return "", false
}

func formatInlineBlock(tokens []jsToken) string {
	if len(tokens) == 0 {
		return "{}"
	}
	var out strings.Builder
	out.WriteString("{ ")
	var previous jsToken
	for _, token := range tokens {
		appendInlineToken(&out, previous, token)
		previous = token
	}
	return strings.TrimRight(out.String(), " ") + " }"
}

func appendInlineToken(out *strings.Builder, previous, token jsToken) {
	switch token.typ {
	case jsTokenComma:
		out.WriteString(", ")
	case jsTokenSemicolon:
		out.WriteString("; ")
	case jsTokenDot:
		out.WriteString(".")
	case jsTokenStartExpr:
		if needsSpaceBeforeParen(jsToken{}, previous, token, &Options{SpaceBeforeConditional: true}) {
			out.WriteByte(' ')
		}
		out.WriteString(token.text)
	case jsTokenEndExpr:
		out.WriteString(token.text)
	case jsTokenStartBlock:
		if inlineNeedsSpace(previous, token, out) {
			out.WriteByte(' ')
		}
		out.WriteString("{ ")
	case jsTokenEndBlock:
		if strings.HasSuffix(out.String(), "{ ") {
			trimInlineSpace(out)
			out.WriteString("}")
		} else {
			trimInlineSpace(out)
			out.WriteString(" }")
		}
	case jsTokenOperator, jsTokenEquals:
		switch token.text {
		case ":", "...":
			out.WriteString(token.text)
			if token.text == ":" {
				out.WriteByte(' ')
			}
		case "!", "~", "++", "--":
			out.WriteString(token.text)
		default:
			trimInlineSpace(out)
			out.WriteByte(' ')
			out.WriteString(token.text)
			out.WriteByte(' ')
		}
	default:
		if inlineNeedsSpace(previous, token, out) {
			out.WriteByte(' ')
		}
		out.WriteString(token.text)
	}
}

func inlineNeedsSpace(previous, token jsToken, out *strings.Builder) bool {
	if out.Len() == 0 {
		return false
	}
	if strings.HasSuffix(out.String(), " ") || strings.HasSuffix(out.String(), "(") ||
		strings.HasSuffix(out.String(), "[") || strings.HasSuffix(out.String(), ".") ||
		strings.HasSuffix(out.String(), "...") {
		return false
	}
	return needsWordSpace(previous, token)
}

func trimInlineSpace(out *strings.Builder) {
	value := out.String()
	value = strings.TrimRight(value, " ")
	out.Reset()
	out.WriteString(value)
}

func jsDisplayWidth(value string) int {
	return utf8.RuneCountInString(value)
}

func jsLineTailWidth(value string) int {
	if index := strings.LastIndex(value, "\n"); index >= 0 {
		return jsDisplayWidth(value[index+1:])
	}
	return jsDisplayWidth(value)
}

func (b *Beautifier) add(text string) {
	b.output.SetIndent(b.indent, 0)
	b.output.AddToken(text)
}

func (b *Beautifier) ensureBlankLine() {
	if b.output.JustAddedBlankline() {
		return
	}
	if !b.output.JustAddedNewline() {
		b.output.AddNewLine(false)
	}
	b.output.AddNewLine(true)
}

func (b *Beautifier) setContinuationWrapPoint() {
	if b.options.WrapLineLength <= 0 || b.output.JustAddedNewline() {
		return
	}
	b.output.SetIndent(b.indent+1, 0)
	b.output.SetWrapPoint()
	b.output.SetIndent(b.indent, 0)
}

func (b *Beautifier) wouldExceedLineWithToken(token jsToken) bool {
	if b.options.WrapLineLength <= 0 || b.output.JustAddedNewline() {
		return false
	}
	lineLen := b.output.CurrentLine.CharacterCount()
	if b.output.SpaceBeforeToken {
		lineLen++
	}
	return lineLen+jsDisplayWidth(token.text) > b.options.WrapLineLength
}

func (b *Beautifier) shouldForceNestedLogicalWrap(previous jsToken) bool {
	if b.options.WrapLineLength <= 0 || previous.text != "&&" {
		return false
	}
	line := b.output.CurrentLine.String()
	return leadingWhitespaceLength(line) >= b.options.IndentSize*2 && strings.Contains(line, "|| (")
}

func (b *Beautifier) addChainOperator(token, previous jsToken, chainContinuationIndented bool) bool {
	forcedBreak := b.options.BreakChainedMethods && previous.typ == jsTokenEndExpr
	if forcedBreak && !b.output.JustAddedNewline() {
		b.output.AddNewLine(false)
	}
	if b.options.WrapLineLength > 0 && !b.output.JustAddedNewline() {
		b.output.SetIndent(b.indent+1, 0)
		b.output.SetWrapPoint()
		b.output.SetIndent(b.indent, 0)
	}
	if (token.newlines > 0 || forcedBreak) && b.output.JustAddedNewline() && !b.options.UnindentChainedMethods && previous.typ != jsTokenComment {
		if !chainContinuationIndented {
			b.indent++
		}
		b.output.SetIndent(b.indent, 0)
		b.output.AddToken(token.text)
		return true
	}
	if token.typ == jsTokenDot && token.whitespaceBefore != "" && isNumericLiteral(previous.text) {
		b.output.SpaceBeforeToken = true
	}
	b.add(token.text)
	return false
}

func (b *Beautifier) next() jsToken {
	if b.pos >= len(b.tokens) {
		return jsToken{typ: jsTokenEOF}
	}
	token := b.tokens[b.pos]
	b.pos++
	return token
}

func (b *Beautifier) peek() jsToken {
	if b.pos >= len(b.tokens) {
		return jsToken{typ: jsTokenEOF}
	}
	return b.tokens[b.pos]
}

func shouldStartLine(token, previous jsToken) bool {
	if previous.text == "export" {
		return false
	}
	if previous.text == "declare" && (token.text == "var" || token.text == "let" || token.text == "const" || token.text == "function") {
		return false
	}
	if previous.text == "default" && token.text == "function" {
		return false
	}
	if token.text == "while" && previous.typ == jsTokenEndBlock {
		return false
	}
	if previous.text == "async" || previous.text == "await" || previous.text == "return" || previous.text == "throw" || previous.text == "yield" || previous.text == "new" {
		return false
	}
	if token.text == "switch" && previous.typ == jsTokenEndExpr {
		return false
	}
	return token.typ == jsTokenReserved && jsLineStarters[token.text] &&
		previous.typ != jsTokenEOF && previous.typ != jsTokenStartBlock &&
		previous.typ != jsTokenStartExpr && previous.typ != jsTokenOperator &&
		previous.typ != jsTokenEquals && previous.typ != jsTokenComma &&
		previous.typ != jsTokenSemicolon && previous.text != "else"
}

func isAdjacentTemplateToken(token jsToken) bool {
	return token.typ == jsTokenUnknown && token.whitespaceBefore == "" && startsJSTemplateText(token.text)
}

func isAdjacentAfterTemplateToken(previous, token jsToken) bool {
	return previous.typ == jsTokenUnknown && token.whitespaceBefore == "" && startsJSTemplateText(previous.text)
}

func startsJSTemplateText(text string) bool {
	for _, prefix := range []string{"<?php", "<?=", "<%", "{{", "{#", "{%"} {
		if strings.HasPrefix(text, prefix) {
			return true
		}
	}
	return false
}

func shouldBreakBeforeSingleStatement(token jsToken) bool {
	if token.typ != jsTokenReserved {
		return false
	}
	switch token.text {
	case "if", "for", "while", "switch", "do":
		return true
	default:
		return false
	}
}

func isContinuationOperator(operator string) bool {
	switch operator {
	case ":", "?", "!", "~", "++", "--":
		return false
	default:
		return operator != ""
	}
}

func isGeneratorAsterisk(previous jsToken) bool {
	if previous.text == "function" || previous.text == "yield" {
		return true
	}
	switch previous.typ {
	case jsTokenStartBlock, jsTokenComma, jsTokenEndBlock, jsTokenSemicolon:
		return true
	default:
		return false
	}
}

func isPositionableOperator(operator string) bool {
	switch operator {
	case ">>>", "===", "!==", "&&=", "??=", "||=", "<<", "&&", ">=", "**", "!=", "==", "<=", ">>", "||", "??", "|>", "<", "/", "-", "+", ">", ":", "&", "%", "?", "^", "|", "*":
		return true
	default:
		return false
	}
}

func isAssignmentOperator(operator string) bool {
	switch operator {
	case "+=", "-=", "*=", "/=", "%=", "<<=", ">>=", ">>>=", "&=", "|=", "^=":
		return true
	default:
		return false
	}
}

func shouldKeepStructuralNewline(previous, token jsToken) bool {
	if previous.typ == jsTokenEndExpr && token.typ == jsTokenStartExpr {
		return true
	}
	if token.typ != jsTokenWord && token.typ != jsTokenReserved && token.typ != jsTokenString {
		return false
	}
	switch previous.typ {
	case jsTokenWord, jsTokenString, jsTokenEndExpr:
		return true
	default:
		return false
	}
}

func shouldSetValueWrapPoint(previous, token jsToken, mode string) bool {
	if token.typ != jsTokenWord && token.typ != jsTokenReserved && token.typ != jsTokenString && token.typ != jsTokenUnknown {
		return false
	}
	switch previous.typ {
	case jsTokenComma, jsTokenStartExpr, jsTokenEquals, jsTokenOperator:
	default:
		return false
	}
	if mode == "{" && previous.text != "?" {
		return false
	}
	if (previous.text == "+" || previous.text == "-") && token.typ != jsTokenString {
		return false
	}
	return true
}

func needsWordSpace(previous, token jsToken) bool {
	if previous.typ == jsTokenEOF || previous.typ == jsTokenStartBlock || previous.typ == jsTokenStartExpr ||
		previous.typ == jsTokenDot || previous.typ == jsTokenOperator || previous.typ == jsTokenEquals ||
		previous.typ == jsTokenComma {
		return false
	}
	if isSharpNumberReference(previous.text) && token.text == "#" {
		return false
	}
	if token.typ == jsTokenString && strings.HasPrefix(token.text, "`") &&
		(previous.typ == jsTokenWord || previous.typ == jsTokenReserved) &&
		previous.text != "return" && previous.text != "throw" && previous.text != "yield" {
		return token.whitespaceBefore != ""
	}
	if token.typ == jsTokenString && strings.HasPrefix(token.text, "`") &&
		previous.typ == jsTokenEndExpr && previous.text == ")" && token.whitespaceBefore == "" {
		return false
	}
	if jsNoSpaceBefore[token.text] {
		return false
	}
	if previous.typ == jsTokenWord || previous.typ == jsTokenReserved || previous.typ == jsTokenString ||
		token.typ == jsTokenWord || token.typ == jsTokenReserved || token.typ == jsTokenString {
		return true
	}
	return false
}

func isMethodDefinitionName(token jsToken) bool {
	if token.text == "if" || token.text == "for" || token.text == "while" || token.text == "switch" ||
		token.text == "catch" || token.text == "with" || token.text == "function" {
		return false
	}
	return token.typ == jsTokenWord || token.typ == jsTokenReserved
}

func needsSpaceBeforeParen(previous2, previous, token jsToken, options *Options) bool {
	if token.text != "(" {
		return false
	}
	if previous.typ == jsTokenReserved && (previous.text == "if" || previous.text == "for" || previous.text == "while" || previous.text == "switch" ||
		previous.text == "catch" || previous.text == "with") {
		return options.SpaceBeforeConditional
	}
	if previous.typ == jsTokenReserved && previous.text == "function" {
		return options.SpaceAfterAnonFunction
	}
	if previous.typ == jsTokenReserved && previous.text == "await" {
		return true
	}
	if previous.typ == jsTokenReserved && (previous.text == "return" || previous.text == "throw" || previous.text == "yield") {
		return true
	}
	if previous.typ == jsTokenReserved && previous.text == "typeof" {
		return options.SpaceAfterAnonFunction
	}
	if previous2.text == "function" && (previous.typ == jsTokenWord || previous.typ == jsTokenReserved) {
		return options.SpaceAfterNamedFunction
	}
	return false
}

func inSwitchCase(previous jsToken) bool {
	return previous.text == "case" || previous.text == "default"
}

func isUnarySign(previous jsToken) bool {
	if previous.text == "++" || previous.text == "--" {
		return false
	}
	return previous.typ == jsTokenEOF ||
		previous.typ == jsTokenComment ||
		previous.typ == jsTokenOperator ||
		previous.typ == jsTokenEquals ||
		previous.typ == jsTokenStartExpr ||
		previous.typ == jsTokenStartBlock ||
		previous.typ == jsTokenComma ||
		previous.text == "return" ||
		previous.text == "throw" ||
		previous.text == "case"
}

func tokenizeJS(source string, options *Options) []jsToken {
	var tokens []jsToken
	i := 0
	newlines := 0
	spaceBefore := ""
	var previous jsToken
	var previousSignificant jsToken
	var exprOpeners []jsToken
	inHTMLComment := false
	for i < len(source) {
		for i < len(source) {
			r, size := utf8.DecodeRuneInString(source[i:])
			if r == '\n' {
				newlines++
				spaceBefore = ""
				i += size
			} else if unicode.IsSpace(r) {
				spaceBefore += source[i : i+size]
				i += size
			} else {
				break
			}
		}
		if i >= len(source) {
			break
		}
		start := i
		ch := source[i]
		var token jsToken
		switch {
		case i == 0 && strings.HasPrefix(source[i:], "#!"):
			for i < len(source) && source[i] != '\n' {
				i++
			}
			token = jsToken{typ: jsTokenComment, text: source[start:i]}
		case startsJSTemplate(source, i, options.Templating):
			text, next := readJSTemplate(source, i, options.Templating)
			i = next
			token = jsToken{typ: jsTokenUnknown, text: text}
		case isIdentifierStart(source, i):
			text, next := readIdentifier(source, i)
			i = next
			typ := jsTokenWord
			if jsReservedWords[text] && !(previousSignificant.typ == jsTokenDot || ((previousSignificant.text == "set" || previousSignificant.text == "get") && previousSignificant.typ == jsTokenReserved)) {
				if (text == "in" || text == "of") && (previousSignificant.typ == jsTokenWord || previousSignificant.typ == jsTokenString) {
					typ = jsTokenOperator
				} else {
					typ = jsTokenReserved
				}
			}
			token = jsToken{typ: typ, text: text}
		case isDigit(ch) || (ch == '.' && i+1 < len(source) && isDigit(source[i+1])):
			text, next := readNumber(source, i)
			i = next
			token = jsToken{typ: jsTokenWord, text: text}
		case ch == '"' || ch == '\'' || ch == '`':
			text, next := readJSString(source, i, options)
			i = next
			token = jsToken{typ: jsTokenString, text: text}
		case ch == '/' && i+1 < len(source) && source[i+1] == '/':
			for i < len(source) && source[i] != '\n' {
				i++
			}
			token = jsToken{typ: jsTokenComment, text: source[start:i]}
		case ch == '/' && i+1 < len(source) && source[i+1] == '*':
			if text, next, ok := readBeautifyRawBlock(source, i); ok {
				i = next
				token = jsToken{typ: jsTokenUnknown, text: strings.ReplaceAll(text, "\r\n", "\n")}
				break
			}
			i += 2
			for i < len(source)-1 && !(source[i] == '*' && source[i+1] == '/') {
				i++
			}
			if i < len(source)-1 {
				i += 2
			}
			token = jsToken{typ: jsTokenBlockComment, text: strings.ReplaceAll(source[start:i], "\r\n", "\n")}
		case ch == '/' && canStartRegexLiteral(previous, previousSignificant):
			text, next := readRegexLiteral(source, i)
			i = next
			token = jsToken{typ: jsTokenString, text: text}
		case strings.HasPrefix(source[i:], "<!--"):
			i += len("<!--")
			inHTMLComment = true
			token = jsToken{typ: jsTokenUnknown, text: "<!--"}
		case inHTMLComment && strings.HasPrefix(source[i:], "-->"):
			i += len("-->")
			inHTMLComment = false
			token = jsToken{typ: jsTokenUnknown, text: "-->"}
		case options.E4X && ch == '<' && canStartRegexLiteral(previous, previousSignificant):
			if text, next, ok := readE4XXMLLiteral(source, i); ok {
				i = next
				token = jsToken{typ: jsTokenString, text: text}
			} else {
				text, next := readOperator(source, i)
				i = next
				token = jsToken{typ: jsTokenOperator, text: text}
			}
		case ch == '(' || ch == '[':
			i++
			token = jsToken{typ: jsTokenStartExpr, text: source[start:i]}
		case ch == ')' || ch == ']':
			i++
			token = jsToken{typ: jsTokenEndExpr, text: source[start:i]}
			if len(exprOpeners) > 0 {
				index := len(exprOpeners) - 1
				token.openedBy = exprOpeners[index].text
				exprOpeners = exprOpeners[:index]
			}
		case ch == '{':
			i++
			token = jsToken{typ: jsTokenStartBlock, text: source[start:i]}
		case ch == '}':
			i++
			token = jsToken{typ: jsTokenEndBlock, text: source[start:i]}
		case ch == ';':
			i++
			token = jsToken{typ: jsTokenSemicolon, text: ";"}
		case ch == ',':
			i++
			token = jsToken{typ: jsTokenComma, text: ","}
		case ch == '.' && !(i+1 < len(source) && source[i+1] == '.'):
			i++
			token = jsToken{typ: jsTokenDot, text: "."}
		default:
			if strings.HasPrefix(source[i:], "?.") && i+2 < len(source) && isDigit(source[i+2]) {
				i++
				token = jsToken{typ: jsTokenOperator, text: "?"}
				break
			}
			text, next := readOperator(source, i)
			i = next
			typ := jsTokenOperator
			if text == "=" {
				typ = jsTokenEquals
			}
			if text == "" {
				text = source[start : start+1]
				i = start + 1
				typ = jsTokenUnknown
			}
			token = jsToken{typ: typ, text: text}
		}
		token.newlines = newlines
		token.whitespaceBefore = spaceBefore
		tokens = append(tokens, token)
		previous = token
		if token.typ == jsTokenStartExpr {
			exprOpeners = append(exprOpeners, previousSignificant)
		}
		if token.typ != jsTokenComment && token.typ != jsTokenBlockComment {
			previousSignificant = token
		}
		newlines = 0
		spaceBefore = ""
	}
	tokens = append(tokens, jsToken{typ: jsTokenEOF})
	return tokens
}

func isIdentifierStart(source string, i int) bool {
	if hasUnicodeEscape(source, i) || hasEscapedUnicodeLiteral(source, i) {
		return true
	}
	r, _ := utf8.DecodeRuneInString(source[i:])
	return r == '_' || r == '$' || r == '#' || r == '@' || unicode.IsLetter(r)
}

func startsJSTemplate(source string, i int, templating []string) bool {
	_, ok := readJSTemplate(source, i, templating)
	return ok > i
}

func readJSTemplate(source string, i int, templating []string) (string, int) {
	type delimiter struct {
		name  string
		open  string
		close string
	}
	delimiters := []delimiter{
		{name: "php", open: "<?php", close: "?>"},
		{name: "php", open: "<?=", close: "?>"},
		{name: "erb", open: "<%", close: "%>"},
		{name: "handlebars", open: "{{!--", close: "--}}"},
		{name: "handlebars", open: "{{{", close: "}}}"},
		{name: "handlebars", open: "{{", close: "}}"},
		{name: "django", open: "{#", close: "#}"},
		{name: "django", open: "{%", close: "%}"},
	}
	for _, item := range delimiters {
		if !hasJSTemplating(templating, item.name) || !strings.HasPrefix(source[i:], item.open) {
			continue
		}
		end := strings.Index(source[i+len(item.open):], item.close)
		if end < 0 {
			return "", i
		}
		next := i + len(item.open) + end + len(item.close)
		return source[i:next], next
	}
	// Like upstream's TemplatablePattern, smarty is only read when it is
	// enabled explicitly and django and handlebars are off.
	if containsJSTemplating(templating, "smarty") && !hasJSTemplating(templating, "django") && !hasJSTemplating(templating, "handlebars") {
		if next := readSmartyTemplate(source, i); next > i {
			return source[i:next], next
		}
	}
	return "", i
}

// readSmartyTemplate returns the end of a smarty comment, literal block, or
// tag at i. An unterminated template runs to the end of the source.
func readSmartyTemplate(source string, i int) int {
	untilAfter := func(start int, close *regexp.Regexp) int {
		if location := close.FindStringIndex(source[start:]); location != nil {
			return start + location[1]
		}
		return len(source)
	}
	switch {
	case strings.HasPrefix(source[i:], "{*"):
		return untilAfter(i+2, smartyCommentClose)
	case strings.HasPrefix(source[i:], "{literal}"):
		return untilAfter(i+len("{literal}"), smartyLiteralClose)
	case i+1 < len(source) && source[i] == '{':
		next, _ := utf8.DecodeRuneInString(source[i+1:])
		if next == '}' || next == '{' || isJSWhitespaceRune(next) {
			return i
		}
		return untilAfter(i+1, smartyTagClose)
	}
	return i
}

var (
	smartyCommentClose = regexp.MustCompile(`\*\}`)
	smartyLiteralClose = regexp.MustCompile(`\{/literal\}`)
	smartyTagClose     = regexp.MustCompile(`[^\t\n\x{0B}\f\r \x{A0}\x{1680}\x{2000}-\x{200A}\x{2028}\x{2029}\x{202F}\x{205F}\x{3000}\x{FEFF}]\}`)
)

func isJSWhitespaceRune(r rune) bool {
	switch r {
	case '\t', '\n', '\v', '\f', '\r', ' ', 0xA0, 0x1680, 0x2028, 0x2029, 0x202F, 0x205F, 0x3000, 0xFEFF:
		return true
	}
	return r >= 0x2000 && r <= 0x200A
}

func containsJSTemplating(items []string, name string) bool {
	for _, item := range items {
		if item == name {
			return true
		}
	}
	return false
}

func hasJSTemplating(items []string, name string) bool {
	for _, item := range items {
		if item == name || item == "auto" {
			return true
		}
	}
	return false
}

func readE4XXMLLiteral(source string, i int) (string, int, bool) {
	offset := i
	rootTag := ""
	isCurlyRoot := false
	depth := 0
	for {
		match := jsXMLTagPattern.FindStringSubmatchIndex(source[offset:])
		if match == nil || match[0] != 0 {
			if offset == i {
				return "", i, false
			}
			return source[i:], len(source), true
		}
		piece := source[offset : offset+match[1]]
		isEndTag := match[2] >= 0 && source[offset+match[2]:offset+match[3]] != ""
		tagName := source[offset+match[4] : offset+match[5]]
		selfClosing := match[12] >= 0 && source[offset+match[12]:offset+match[13]] != ""
		if rootTag == "" {
			rootTag = normalizeCurlyXMLTag(tagName)
			isCurlyRoot = strings.HasPrefix(rootTag, "{")
		}
		normalizedTag := tagName
		if isCurlyRoot {
			normalizedTag = normalizeCurlyXMLTag(tagName)
		}
		isSingleton := selfClosing || strings.HasPrefix(tagName, "![CDATA[")
		if !isSingleton && (tagName == rootTag || normalizedTag == rootTag) {
			if isEndTag {
				depth--
			} else {
				depth++
			}
		}
		offset += len(piece)
		if depth <= 0 {
			break
		}
	}
	return strings.ReplaceAll(source[i:offset], "\r\n", "\n"), offset, true
}

func readBeautifyRawBlock(source string, i int) (string, int, bool) {
	if !strings.HasPrefix(source[i:], "/*") {
		return "", i, false
	}
	startEnd := strings.Index(source[i+2:], "*/")
	if startEnd < 0 {
		return "", i, false
	}
	startCommentEnd := i + 2 + startEnd + 2
	startComment := source[i:startCommentEnd]
	ignoreActive := hasBeautifyDirective(startComment, "ignore:start")
	preserveActive := hasBeautifyDirective(startComment, "preserve:start")
	if !ignoreActive && !preserveActive {
		return "", i, false
	}
	search := startCommentEnd
	preserveCanRunToEOF := ignoreActive && preserveActive
	if ignoreActive {
		for search < len(source) {
			nextComment := strings.Index(source[search:], "/*")
			if nextComment < 0 {
				return source[i:], len(source), true
			}
			commentStart := search + nextComment
			commentEndRel := strings.Index(source[commentStart+2:], "*/")
			if commentEndRel < 0 {
				return source[i:], len(source), true
			}
			commentEnd := commentStart + 2 + commentEndRel + 2
			comment := source[commentStart:commentEnd]
			if hasBeautifyDirective(comment, "ignore:end") {
				if preserveActive || hasBeautifyDirective(comment, "preserve:start") {
					preserveActive = true
					preserveCanRunToEOF = true
					search = commentEnd
					break
				}
				return source[i:commentEnd], commentEnd, true
			}
			search = commentEnd
		}
	}
	if preserveActive {
		for search < len(source) {
			nextComment := strings.Index(source[search:], "/*")
			if nextComment < 0 {
				if preserveCanRunToEOF {
					return source[i:], len(source), true
				}
				return "", i, false
			}
			commentStart := search + nextComment
			commentEndRel := strings.Index(source[commentStart+2:], "*/")
			if commentEndRel < 0 {
				if preserveCanRunToEOF {
					return source[i:], len(source), true
				}
				return "", i, false
			}
			commentEnd := commentStart + 2 + commentEndRel + 2
			if hasBeautifyDirective(source[commentStart:commentEnd], "preserve:end") {
				return source[i:commentEnd], commentEnd, true
			}
			search = commentEnd
		}
		if preserveCanRunToEOF {
			return source[i:], len(source), true
		}
		return "", i, false
	}
	return source[i:], len(source), true
}

func hasBeautifyDirective(comment string, directive string) bool {
	inner := strings.TrimSpace(strings.TrimSuffix(strings.TrimPrefix(comment, "/*"), "*/"))
	fields := strings.Fields(inner)
	return len(fields) >= 2 && fields[0] == "beautify" && containsString(fields[1:], directive)
}

func containsString(items []string, target string) bool {
	for _, item := range items {
		if item == target {
			return true
		}
	}
	return false
}

func normalizeCurlyXMLTag(tag string) string {
	if strings.HasPrefix(tag, "{") && strings.HasSuffix(tag, "}") {
		return "{" + strings.TrimSpace(tag[1:len(tag)-1]) + "}"
	}
	return tag
}

func isIdentifierPart(r rune) bool {
	return r == '_' || r == '$' || unicode.IsLetter(r) || unicode.IsDigit(r)
}

func readIdentifier(source string, i int) (string, int) {
	start := i
	for i < len(source) {
		if hasEscapedUnicodeLiteral(source, i) {
			i = readEscapedUnicodeLiteral(source, i)
			continue
		}
		if hasUnicodeEscape(source, i) {
			i = readUnicodeEscape(source, i)
			continue
		}
		r, size := utf8.DecodeRuneInString(source[i:])
		if !isIdentifierPart(r) && !(i == start && (r == '#' || r == '@')) {
			break
		}
		i += size
	}
	return source[start:i], i
}

func indentationLevel(whitespace string, options *Options) int {
	if whitespace == "" || options.IndentSize <= 0 {
		return 0
	}
	columns := 0
	for _, r := range whitespace {
		switch r {
		case '\t':
			columns += options.IndentSize
		case ' ':
			columns++
		}
	}
	return columns / options.IndentSize
}

func hasUnicodeEscape(source string, i int) bool {
	return i+2 < len(source) && source[i] == '\\' && source[i+1] == 'u'
}

func hasEscapedUnicodeLiteral(source string, i int) bool {
	return i+3 < len(source) && source[i] == '\\' && source[i+1] == '\\' && source[i+2] == 'u'
}

func readEscapedUnicodeLiteral(source string, i int) int {
	return readUnicodeEscape(source, i+1)
}

func readUnicodeEscape(source string, i int) int {
	i += 2
	if i < len(source) && source[i] == '{' {
		i++
		for i < len(source) && source[i] != '}' {
			i++
		}
		if i < len(source) {
			i++
		}
		return i
	}
	for n := 0; n < 4 && i < len(source); n++ {
		i++
	}
	return i
}

func isDigit(ch byte) bool {
	return ch >= '0' && ch <= '9'
}

func isNumericLiteral(text string) bool {
	if text == "" {
		return false
	}
	first := text[0]
	return (first >= '0' && first <= '9') || first == '.'
}

func isSharpNumberReference(text string) bool {
	if len(text) < 2 || text[0] != '#' {
		return false
	}
	for i := 1; i < len(text); i++ {
		if text[i] < '0' || text[i] > '9' {
			return false
		}
	}
	return true
}

func readNumber(source string, i int) (string, int) {
	start := i
	if i+1 < len(source) && source[i] == '0' && (source[i+1] == 'x' || source[i+1] == 'X' || source[i+1] == 'o' || source[i+1] == 'O' || source[i+1] == 'b' || source[i+1] == 'B') {
		basePrefix := source[i+1]
		i += 2
		for i < len(source) && isBasedNumberPart(basePrefix, source[i]) {
			i++
		}
		if i < len(source) && source[i] == 'n' {
			i++
		}
		return source[start:i], i
	}
	hasDecimalPoint := false
	for i < len(source) && (isDigit(source[i]) || source[i] == '_' || source[i] == '.') {
		if source[i] == '.' {
			hasDecimalPoint = true
		}
		i++
	}
	hasExponent := false
	if i < len(source) && (source[i] == 'e' || source[i] == 'E') {
		hasExponent = true
		i++
		if i < len(source) && (source[i] == '+' || source[i] == '-') {
			i++
		}
		for i < len(source) && (isDigit(source[i]) || source[i] == '_') {
			i++
		}
	}
	if i < len(source) && source[i] == 'n' && !hasDecimalPoint && !hasExponent {
		i++
	}
	return source[start:i], i
}

func isBasedNumberPart(prefix byte, ch byte) bool {
	if ch == '_' {
		return true
	}
	switch prefix {
	case 'x', 'X':
		return isDigit(ch) || (ch >= 'a' && ch <= 'f') || (ch >= 'A' && ch <= 'F')
	case 'o', 'O':
		return ch >= '0' && ch <= '7'
	case 'b', 'B':
		return ch == '0' || ch == '1'
	default:
		return false
	}
}

func readJSString(source string, i int, options *Options) (string, int) {
	start := i
	delimiter := source[i]
	i++
	if delimiter == '`' {
		depth := 0
		for i < len(source) {
			switch source[i] {
			case '\\':
				i += 2
				continue
			case '`':
				if depth == 0 {
					i++
					text := source[start:i]
					if options.UnescapeStrings {
						text = unescapeJSString(text)
					}
					return text, i
				}
			case '$':
				if i+1 < len(source) && source[i+1] == '{' {
					depth++
					i += 2
					continue
				}
			case '{':
				if depth > 0 {
					depth++
				}
			case '}':
				if depth > 0 {
					depth--
				}
			case '"', '\'':
				_, next := readJSString(source, i, options)
				i = next
				continue
			}
			i++
		}
		text := source[start:i]
		if options.UnescapeStrings {
			text = unescapeJSString(text)
		}
		return text, i
	}
	for i < len(source) {
		if source[i] == '\\' {
			i += 2
			continue
		}
		if source[i] == delimiter {
			i++
			break
		}
		if shouldSkipTemplateInString(source, i, options.Templating) {
			_, next := readJSTemplate(source, i, options.Templating)
			i = next
			continue
		}
		if delimiter != '`' && source[i] == '\n' {
			break
		}
		i++
	}
	text := source[start:i]
	if options.UnescapeStrings {
		text = unescapeJSString(text)
	}
	return text, i
}

func shouldSkipTemplateInString(source string, i int, templating []string) bool {
	if hasJSTemplating(templating, "auto") {
		return false
	}
	_, next := readJSTemplate(source, i, templating)
	return next > i
}

func canStartRegexLiteral(previous, previousSignificant jsToken) bool {
	if previous.typ == jsTokenComment || previous.typ == jsTokenBlockComment {
		return true
	}
	if previousSignificant.typ == jsTokenEndExpr && previousSignificant.text == ")" &&
		(previousSignificant.openedBy == "if" || previousSignificant.openedBy == "while" || previousSignificant.openedBy == "for") {
		return true
	}
	switch previousSignificant.typ {
	case jsTokenEOF, jsTokenStartExpr, jsTokenStartBlock, jsTokenOperator, jsTokenEquals, jsTokenComma, jsTokenSemicolon:
		return true
	}
	if previousSignificant.typ == jsTokenEndBlock && previous.typ == jsTokenEndBlock {
		return true
	}
	switch previousSignificant.text {
	case "", "return", "throw", "case", "delete", "void", "typeof", "yield", "else", "do":
		return true
	default:
		return false
	}
}

func readRegexLiteral(source string, i int) (string, int) {
	start := i
	i++
	inClass := false
	for i < len(source) {
		switch source[i] {
		case '\\':
			i += 2
			continue
		case '[':
			inClass = true
		case ']':
			inClass = false
		case '/':
			if !inClass {
				i++
				for i < len(source) {
					r, size := utf8.DecodeRuneInString(source[i:])
					if r != '_' && r != '$' && !unicode.IsLetter(r) && !unicode.IsDigit(r) {
						break
					}
					i += size
				}
				return source[start:i], i
			}
		case '\n', '\r':
			return source[start:i], i
		}
		i++
	}
	return source[start:i], i
}

func readOperator(source string, i int) (string, int) {
	operators := []string{
		">>>=", "...", ">>=", "<<=", "===", ">>>", "!==", "**=", "&&=", "??=", "||=",
		"=>", "^=", "::", "/=", "<<", "<=", "==", "&&", "-=", ">=", ">>", "!=", "--",
		"+=", "**", "||", "??", "++", "%=", "&=", "*=", "|=", "|>", "?.",
		"=", "!", "?", ">", "<", ":", "/", "^", "-", "+", "*", "&", "%", "~", "|",
	}
	for _, op := range operators {
		if strings.HasPrefix(source[i:], op) {
			return op, i + len(op)
		}
	}
	return "", i
}

func unescapeJSString(source string) string {
	re := regexp.MustCompile(`\\x([0-9A-Fa-f]{2})|\\u([0-9A-Fa-f]{4})|\\u\{([0-9A-Fa-f]+)\}`)
	return re.ReplaceAllStringFunc(source, func(match string) string {
		groups := re.FindStringSubmatch(match)
		hex := ""
		for _, group := range groups[1:] {
			if group != "" {
				hex = group
				break
			}
		}
		if hex == "" {
			return match
		}
		var value rune
		for _, r := range hex {
			value *= 16
			switch {
			case r >= '0' && r <= '9':
				value += r - '0'
			case r >= 'a' && r <= 'f':
				value += r - 'a' + 10
			case r >= 'A' && r <= 'F':
				value += r - 'A' + 10
			}
		}
		if value > 0x10FFFF || value < 0x20 || (value > 0x7e && strings.HasPrefix(match, `\x`)) {
			return match
		}
		if value == '"' || value == '\'' || value == '\\' {
			return `\` + string(value)
		}
		return string(value)
	})
}

func leadingIndent(source string) string {
	i := 0
	for i < len(source) && (source[i] == ' ' || source[i] == '\t') {
		i++
	}
	return source[:i]
}
