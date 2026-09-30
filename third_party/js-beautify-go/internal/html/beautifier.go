// Package html implements the HTML formatter.
package html

import (
	"os"
	"regexp"
	"strings"

	"github.com/yottonoko/js-beautify-go/internal/core"
)

// LangBeautifier formats an embedded language block with inherited options.
type LangBeautifier func(string, map[string]any) (string, error)

var htmlIgnoreStartRE = regexp.MustCompile(`(?is)^<!--\s*beautify\s+ignore:start\s*-->$`)
var htmlIgnoreEndRE = regexp.MustCompile(`(?is)<!--\s*beautify\s+ignore:end\s*-->`)
var scriptBeautifierTypeRE = regexp.MustCompile(`module|((text|application|dojo)/(x-)?(javascript|ecmascript|jscript|livescript|(ld\+)?json|method|aspect))`)
var htmlBeautifierTypeRE = regexp.MustCompile(`(text|application|dojo)/(x-)?(html)`)
var handlebarsSpaceProtector = strings.NewReplacer(" ", "\x00", "\t", "\x01", "\n", "\x02", "\r", "\x03")
var handlebarsSpaceRestorer = strings.NewReplacer("\x00", " ", "\x01", "\t", "\x02", "\n", "\x03", "\r")

// Beautifier formats HTML source using normalized options.
type Beautifier struct {
	sourceText       string
	options          *Options
	js               LangBeautifier
	css              LangBeautifier
	output           *core.Output
	indent           int
	nextIndentBoost  int
	inlineDepth      int
	inlineBlockTags  []string
	optionalTags     []optionalTag
	handlebarsDepth  int
	blankBeforeTag   bool
	closeOnSameLine  bool
	multilineTextTag string
	angularDepth     int
	formatTagCache   map[formatTagCacheKey]string
	closeIndex       *simpleCloseIndex
}

type optionalTag struct {
	name   string
	indent int
}

type formatTagCacheKey struct {
	tag    string
	indent int
}

// Beautify formats HTML source and delegates embedded JavaScript and CSS.
func Beautify(source string, options map[string]any, js LangBeautifier, css LangBeautifier) (string, error) {
	opts, err := NewOptions(options)
	if err != nil {
		return "", err
	}
	return NewBeautifier(source, opts, js, css).Beautify()
}

// NewBeautifier creates an HTML beautifier for source, options, and delegates.
func NewBeautifier(source string, options *Options, js LangBeautifier, css LangBeautifier) *Beautifier {
	return &Beautifier{sourceText: source, options: options, js: js, css: css}
}

// Beautify formats the beautifier source and returns the resulting markup.
func (b *Beautifier) Beautify() (string, error) {
	if b.options.Disabled {
		return b.sourceText, nil
	}
	source := b.sourceText
	eol := b.options.EOL
	if eol == "auto" {
		eol = "\n"
		if lineBreak := firstHTMLLineBreak(source); lineBreak != "" {
			eol = lineBreak
		}
	}
	source = normalizeHTMLLineBreaks(source)
	if startsAngularControlDocument(source) && !hasTemplating(b.options.Templating, "angular") && !hasStandaloneHTMLBlockLine(source, b.options) {
		if b.options.EndWithNewline && !strings.HasSuffix(source, "\n") {
			source += "\n"
		}
		return strings.ReplaceAll(source, "\n", eol), nil
	}
	if startsAngularControlDocument(source) && hasTemplating(b.options.Templating, "angular") {
		source = formatAngularControlDocument(source, b.options)
		if b.options.EndWithNewline && !strings.HasSuffix(source, "\n") {
			source += "\n"
		}
		return strings.ReplaceAll(source, "\n", eol), nil
	}
	b.output = core.NewOutput(core.OutputOptionsFromBase(b.options.BaseOptions), leadingIndent(source))
	b.indent = 0

	i := 0
	for i < len(source) {
		if strings.HasPrefix(source[i:], "{{") {
			token, next := readHandlebars(source, i)
			if token != "" {
				if b.options.IndentHandlebars && b.shouldFormatHandlebars(source, i, next, token) {
					b.printHandlebars(token, source[next:])
				} else if b.options.IndentHandlebars && handlebarsKind(token) != "single" {
					b.printText(normalizeHandlebars(token))
				} else {
					b.printText(token)
				}
				i = next
				continue
			}
		}
		if strings.HasPrefix(source[i:], "<?") {
			token, next := readProcessingInstruction(source, i)
			if token != "" {
				b.printProcessingInstruction(token)
				i = next
				continue
			}
		}
		if strings.HasPrefix(source[i:], "<%") {
			token, next := readERBInstruction(source, i)
			if token != "" {
				b.printProcessingInstruction(token)
				i = next
				continue
			}
		}
		if strings.HasPrefix(source[i:], "{#") || strings.HasPrefix(source[i:], "{%") {
			token, next := readBraceTemplateInstruction(source, i)
			if token != "" {
				b.printProcessingInstruction(token)
				i = next
				continue
			}
		}
		if token, next := b.readSmartyInstruction(source, i); token != "" {
			b.printProcessingInstruction(token)
			i = next
			continue
		}
		if strings.HasPrefix(source[i:], "<!--") {
			end := strings.Index(source[i+4:], "-->")
			commentEnd := len(source)
			if end >= 0 {
				commentEnd = i + 4 + end + 3
			}
			comment := source[i:commentEnd]
			if htmlIgnoreStartRE.MatchString(strings.TrimSpace(comment)) {
				if rawEnd := htmlIgnoreEndRE.FindStringIndex(source[commentEnd:]); rawEnd != nil {
					b.output.AddRawText(source[i : commentEnd+rawEnd[1]])
					i = commentEnd + rawEnd[1]
					continue
				}
			}
			if shouldPrintInlineComment(source, i) {
				b.printInlineComment(comment)
				i = commentEnd
				continue
			}
			b.printBlock(comment)
			i = commentEnd
			continue
		}
		if source[i] == '<' {
			tag, next := readHTMLTag(source, i)
			if tag == "" {
				b.printText(source[i : i+1])
				i++
				continue
			}
			if isDynamicProcessingTag(tag) {
				b.printToken(strings.TrimSpace(tag))
				i = next
				continue
			}
			name := lowerASCII(tagName(tag))
			closeTagToken := isCloseTag(tag)
			selfClosingTag := isSelfClosing(tag)
			voidTag := isVoidTag(name, b.options)
			inlineTag := isInlineName(name, b.options)
			if b.blankBeforeTag {
				if !hasExtraLiner(name, false, b.options) {
					b.output.AddNewLine(true)
				}
				b.blankBeforeTag = false
			}
			if !closeTagToken {
				b.closeOptionalForOpen(name)
			}
			if !closeTagToken && !voidTag && !selfClosingTag && isUnformatted(name, b.options) {
				closeTag := "</" + name + ">"
				if closeIndex := findUnformattedContentClose(source[next:], closeTag); closeIndex >= 0 {
					if !inlineTag && !b.output.JustAddedNewline() {
						b.output.AddNewLine(false)
					}
					contentEnd := next + closeIndex
					raw := normalizeUnformattedRawElement(tag, source[next:contentEnd], source[contentEnd:contentEnd+len(closeTag)])
					if b.output.JustAddedNewline() {
						raw = b.output.GetIndentString(b.indent+b.nextIndentBoost, 0) + raw
						b.nextIndentBoost = 0
					}
					b.output.AddRawText(raw)
					if !inlineTag && hasOpenBlockParent(b.indent) && !startsWithInlineComment(source[contentEnd+len(closeTag):]) {
						b.output.AddNewLine(false)
					}
					i = contentEnd + len(closeTag)
					continue
				}
			}
			if !closeTagToken && !voidTag && !selfClosingTag && name != "script" && name != "style" {
				closeTag := "</" + name + ">"
				if hasPrefixASCIIFold(source[next:], closeTag) {
					if hasExtraLiner(name, false, b.options) {
						b.output.AddNewLine(true)
					} else if !inlineTag && !b.output.JustAddedNewline() {
						b.output.AddNewLine(false)
					}
					b.printToken(b.formatTag(tag) + source[next:next+len(closeTag)])
					if !inlineTag {
						if hasExtraLiner(name, false, b.options) {
							b.output.AddNewLine(true)
						} else if !startsWithInlineComment(source[next+len(closeTag):]) &&
							!startsWithTextBeforeLineBreak(source[next+len(closeTag):]) &&
							!startsWithInlineTagBeforeLineBreak(source[next+len(closeTag):], b.options) {
							b.output.AddNewLine(false)
						}
					}
					i = next + len(closeTag)
					continue
				}
				if hasExtraLiner(name, false, b.options) || hasExtraLiner(name, true, b.options) {
					// Extra-liner tags need separate open/content/close tokens so the
					// configured blank lines can be emitted on both sides.
				} else if isContentUnformatted(name, b.options) {
					if closeIndex := findRawContentClose(source[next:], closeTag); closeIndex >= 0 {
						if !inlineTag && !b.output.JustAddedNewline() {
							b.output.AddNewLine(false)
						}
						contentEnd := next + closeIndex
						b.printToken(b.formatTag(tag) + source[next:contentEnd] + source[contentEnd:contentEnd+len(closeTag)])
						if !inlineTag && hasOpenBlockParent(b.indent) && !startsWithInlineComment(source[contentEnd+len(closeTag):]) {
							b.output.AddNewLine(false)
						}
						i = contentEnd + len(closeTag)
						continue
					}
				} else if shouldTrySimpleHTMLContent(source, next, name, closeTag, b.options) {
					content, closeStart, closeEnd, ok := b.readSimpleHTMLContent(source, next, closeTag, tag)
					if ok {
						if hasExtraLiner(name, false, b.options) {
							b.output.AddNewLine(true)
						} else if !inlineTag && !b.output.JustAddedNewline() {
							b.output.AddNewLine(false)
						}
						openTag := b.formatTag(tag)
						closeText := b.formatTag(source[closeStart:closeEnd])
						lineIndent := b.output.GetIndentString(b.indent, 0)
						initialLineLen := lineTailLen(lineIndent + openTag)
						if !b.output.JustAddedNewline() {
							initialLineLen = b.output.CurrentLine.CharacterCount()
							if b.output.SpaceBeforeToken {
								initialLineLen++
							}
							initialLineLen += visualLen(openTag)
						}
						content = wrapInlineContent(openTag, content, closeText, b.options, lineIndent, initialLineLen)
						b.printToken(openTag + content + closeText)
						if !inlineTag && hasOpenBlockParent(b.indent) && !startsWithInlineComment(source[closeEnd:]) {
							b.output.AddNewLine(false)
						}
						i = closeEnd
						continue
					}
				}
			}
			if closeTagToken {
				b.closeOptionalForClose(name)
				inlineBlock := b.popInlineBlockTag(name)
				inlineName := inlineTag && !inlineBlock
				closeOnSameLine := b.closeOnSameLine
				b.closeOnSameLine = false
				if b.multilineTextTag == name {
					b.multilineTextTag = ""
				}
				if inlineName && b.inlineDepth > 0 {
					b.inlineDepth--
				}
				if !inlineName && b.indent > 0 {
					b.indent--
				}
				attachedClose := closeTagAttachedToContent(name, source, i)
				if hasExtraLiner(name, true, b.options) {
					b.addExtraLinerBefore()
				} else if !inlineName && !closeOnSameLine && !attachedClose && !b.output.JustAddedNewline() {
					b.output.AddNewLine(false)
				}
				if inlineName {
					b.nextIndentBoost = 0
				}
				b.printToken(b.formatTag(tag))
				if !inlineName {
					if !startsWithInlineComment(source[next:]) &&
						!startsWithTextBeforeLineBreak(source[next:]) &&
						!startsWithInlineTagBeforeLineBreak(source[next:], b.options) {
						b.output.AddNewLine(false)
					}
				}
				i = next
				continue
			}
			if name == "script" || name == "style" {
				closeTag := "</" + name + ">"
				closeIndex := findRawContentClose(source[next:], closeTag)
				if !b.output.JustAddedNewline() && !inlineTag {
					b.output.AddNewLine(false)
				}
				if closeIndex >= 0 {
					contentStart := next
					contentEnd := next + closeIndex
					content := source[contentStart:contentEnd]
					closeText := source[contentEnd : contentEnd+len(closeTag)]
					if isContentUnformatted(name, b.options) {
						b.printToken(b.formatTag(tag) + content + closeText)
						if !startsWithInlineComment(source[contentEnd+len(closeTag):]) {
							b.output.AddNewLine(false)
						}
						i = contentEnd + len(closeTag)
						continue
					}
					if isIncompleteWrappedRaw(content) {
						b.printToken(b.formatTag(tag) + content + closeText)
						b.output.AddNewLine(false)
						i = contentEnd + len(closeTag)
						continue
					}
					kind := customBeautifierName(tag, name)
					if kind == "" {
						b.printToken(b.formatTag(tag) + compactUnknownRawContent(content) + closeText)
						if !inlineTag && hasOpenBlockParent(b.indent) {
							b.output.AddNewLine(false)
						}
						i = contentEnd + len(closeTag)
						continue
					}
					disabledRaw := langDisabled(kind, b.options.RawOptions)
					if isSingleLineWhitespace(content) {
						b.printToken(b.formatTag(tag) + content + closeText)
						if !startsWithInlineComment(source[contentEnd+len(closeTag):]) {
							b.output.AddNewLine(false)
						}
						i = contentEnd + len(closeTag)
						continue
					}
					b.printToken(b.formatTag(tag))
					formatted := formatNullRawContent(content)
					rawFormatted := formatted
					preserveRawIndent := true
					disabledRawIndented := false
					if disabledRaw {
						formatted, disabledRawIndented = formatDisabledRawContent(content)
					}
					var err error
					if disabledRaw {
						// Keep the raw script/style body below.
					} else if wrapped, ok := b.formatWrappedScriptStyleContent(content, kind); ok {
						formatted = wrapped
					} else if kind == "javascript" && b.js != nil {
						formatted, err = b.beautifyJavaScriptContent(formatted)
						preserveRawIndent = formatted == rawFormatted && !hasIndentedContinuation(formatted)
					} else if kind == "css" && b.css != nil {
						formatted, err = b.css(formatted, b.options.RawOptions)
						preserveRawIndent = false
					} else if kind == "html" {
						formatted, err = Beautify(formatted, b.options.RawOptions, b.js, b.css)
						preserveRawIndent = false
					} else if kind == "null" {
						formatted = formatNullRawContent(content)
					}
					if err != nil {
						return "", err
					}
					contentIndent := b.rawContentIndent()
					if formatted != "" {
						b.output.AddNewLine(false)
						lines := strings.Split(formatted, "\n")
						for index, line := range lines {
							if line == "" {
								if index > 0 && index < len(lines)-1 {
									b.output.AddNewLine(true)
								}
								continue
							}
							lineIndent := contentIndent
							if disabledRaw && (disabledRawIndented || index > 0) {
								lineIndent = -1
							} else if kind == "javascript" && b.options.IndentScripts == "normal" {
								lineIndent = javascriptRawLineIndent(lines, index, contentIndent, preserveRawIndent)
							}
							b.printTokenAtIndent(line, lineIndent)
							b.output.AddNewLine(false)
						}
					}
					b.printToken(closeText)
					if !startsWithInlineComment(source[contentEnd+len(closeTag):]) {
						b.output.AddNewLine(false)
					}
					i = contentEnd + len(closeTag)
					continue
				}
			}
			if hasExtraLiner(name, false, b.options) {
				b.output.AddNewLine(true)
			} else if !inlineTag && !b.output.JustAddedNewline() {
				b.output.AddNewLine(false)
			}
			formattedTag := b.formatTag(tag)
			b.printToken(formattedTag)
			if !voidTag && !selfClosingTag && !strings.HasPrefix(name, "!") && !strings.HasPrefix(name, "?") {
				if name == "pre" {
					b.multilineTextTag = name
				}
				inlineBlock := inlineTag && shouldTreatInlineAsBlock(name, formattedTag, source[next:], b.options)
				if inlineBlock {
					b.inlineBlockTags = append(b.inlineBlockTags, name)
				}
				if inlineTag && !inlineBlock {
					b.inlineDepth++
				} else {
					if shouldIndentTagContents(name, b.options) {
						b.indent++
						b.output.SetIndent(b.indent, 0)
					}
					if isOptionalEndTag(name) {
						b.optionalTags = append(b.optionalTags, optionalTag{name: name, indent: b.indent})
					}
					if !startsWithTextBeforeLineBreak(source[next:]) {
						b.output.AddNewLine(false)
					}
				}
			} else if !isInlineName(name, b.options) && !isUnformatted(name, b.options) && !startsWithTextBeforeLineBreak(source[next:]) {
				b.output.AddNewLine(false)
			}
			i = next
			continue
		}
		next := nextHTMLSpecialIndex(source[i:], b.options)
		if next == 0 {
			// The marker at i was not consumed by any reader above (for example an
			// unterminated "{#"), so treat it as text to guarantee progress.
			next = nextHTMLSpecialIndex(source[i+1:], b.options)
			if next >= 0 {
				next++
			}
		}
		if next < 0 {
			next = len(source) - i
		}
		b.printText(source[i : i+next])
		i += next
	}
	code := b.output.GetCode("\n")
	if hasTemplating(b.options.Templating, "angular") && containsAngularControlOpenLine(code) {
		code = formatAngularControlDocument(code, b.options)
	}
	if eol != "\n" {
		code = strings.ReplaceAll(code, "\n", eol)
	}
	return code, nil
}

func (b *Beautifier) closeOptionalForOpen(name string) {
	for len(b.optionalTags) > 0 {
		tag := b.optionalTags[len(b.optionalTags)-1]
		if tag.indent != b.indent || !optionalOpenCloses(name, tag.name) {
			return
		}
		b.optionalTags = b.optionalTags[:len(b.optionalTags)-1]
		if b.indent > 0 {
			b.indent--
			b.output.SetIndent(b.indent, 0)
		}
	}
}

func (b *Beautifier) closeOptionalForClose(name string) {
	for len(b.optionalTags) > 0 {
		tag := b.optionalTags[len(b.optionalTags)-1]
		if tag.name == name {
			b.optionalTags = b.optionalTags[:len(b.optionalTags)-1]
			return
		}
		if !optionalParentCloses(name, tag.name) {
			return
		}
		if tag.indent > b.indent {
			b.optionalTags = b.optionalTags[:len(b.optionalTags)-1]
			continue
		}
		if tag.indent < b.indent {
			return
		}
		b.optionalTags = b.optionalTags[:len(b.optionalTags)-1]
		if b.indent > 0 {
			b.indent--
			b.output.SetIndent(b.indent, 0)
		}
	}
}

func isOptionalEndTag(name string) bool {
	switch name {
	case "li", "dt", "dd", "p", "option", "optgroup", "caption", "colgroup",
		"thead", "tbody", "tfoot", "tr", "td", "th":
		return true
	default:
		return false
	}
}

func optionalOpenCloses(name string, previous string) bool {
	switch previous {
	case "li":
		return name == "li"
	case "dt", "dd":
		return name == "dt" || name == "dd"
	case "p":
		return closesParagraph(name)
	case "option":
		return name == "option" || name == "optgroup"
	case "optgroup":
		return name == "optgroup"
	case "caption":
		return name == "colgroup" || isTableSection(name) || name == "tr"
	case "colgroup":
		return isTableSection(name) || name == "tr"
	case "thead", "tbody", "tfoot":
		return isTableSection(name)
	case "tr":
		return name == "tr" || isTableSection(name)
	case "td", "th":
		return name == "td" || name == "th" || name == "tr" || isTableSection(name)
	default:
		return false
	}
}

func optionalParentCloses(parent string, child string) bool {
	if parent == child {
		return false
	}
	switch child {
	case "li":
		return parent == "ul" || parent == "ol" || parent == "menu"
	case "dt", "dd":
		return parent == "dl"
	case "p":
		return parent == "body" || parent == "html" || parent == "dt" || parent == "dd"
	case "option":
		return parent == "select" || parent == "optgroup"
	case "optgroup":
		return parent == "select"
	case "caption", "colgroup", "thead", "tbody", "tfoot", "tr", "td", "th":
		return parent == "table" || isTableSection(parent) || parent == "tr"
	default:
		return false
	}
}

func isTableSection(name string) bool {
	return name == "thead" || name == "tbody" || name == "tfoot"
}

func closesParagraph(name string) bool {
	switch name {
	case "address", "article", "aside", "blockquote", "details", "div", "dl", "fieldset",
		"figcaption", "figure", "footer", "form", "h1", "h2", "h3", "h4", "h5", "h6",
		"header", "hr", "main", "menu", "nav", "ol", "p", "pre", "section", "table", "ul",
		"caption", "colgroup", "option", "optgroup", "tbody", "td", "tfoot", "th", "thead", "tr":
		return true
	default:
		return false
	}
}

func (b *Beautifier) popInlineBlockTag(name string) bool {
	if len(b.inlineBlockTags) == 0 {
		return false
	}
	index := len(b.inlineBlockTags) - 1
	if b.inlineBlockTags[index] != name {
		return false
	}
	b.inlineBlockTags = b.inlineBlockTags[:index]
	return true
}

func (b *Beautifier) addExtraLinerBefore() {
	if b.output.JustAddedBlankline() {
		return
	}
	if !b.output.JustAddedNewline() {
		b.output.AddNewLine(false)
	}
	b.output.AddNewLine(true)
}

func shouldTreatInlineAsBlock(name string, formattedTag string, rest string, options *Options) bool {
	if name == "button" && strings.Contains(formattedTag, "\n") {
		return true
	}
	closeTag := "</" + name + ">"
	closeIndex := findSimpleCloseIndex(rest, closeTag)
	if closeIndex < 0 {
		return false
	}
	content := rest[:closeIndex]
	if firstNonWhitespaceIsTag(content) && containsLineBreakOutsideTag(content) && !isSingleInlinePreservedChild(content, options) {
		return true
	}
	if !isInlineOnlyHTML(content, options) && containsOnlyTagMarkup(content) {
		return true
	}
	if strings.ContainsAny(content, "\n\r") && containsBlockHandlebars(content) {
		return true
	}
	if name == "select" && strings.ContainsAny(content, "\n\r") && !isInlineOnlyHTML(content, options) {
		return true
	}
	return name == "button" && strings.ContainsAny(content, "\n\r") && !isInlineOnlyHTML(content, options)
}

func shouldIndentTagContents(name string, options *Options) bool {
	switch name {
	case "html":
		return options.IndentInnerHTML
	case "head":
		return options.IndentHeadInnerHTML
	case "body":
		return options.IndentBodyInnerHTML
	default:
		return true
	}
}

func containsLineBreakOutsideTag(content string) bool {
	for i := 0; i < len(content); {
		switch content[i] {
		case '<':
			_, next := readHTMLTag(content, i)
			if next <= i {
				i++
			} else {
				i = next
			}
		case '\n', '\r':
			return true
		default:
			i++
		}
	}
	return false
}

func firstNonWhitespaceIsTag(content string) bool {
	for i := 0; i < len(content); i++ {
		if isASCIIWhitespace(content[i]) {
			continue
		}
		return content[i] == '<'
	}
	return false
}

func isSingleInlinePreservedChild(content string, options *Options) bool {
	trimmed := strings.TrimSpace(content)
	if trimmed == "" || !strings.HasPrefix(trimmed, "<") {
		return false
	}
	tag, next := readHTMLTag(trimmed, 0)
	if tag == "" {
		return false
	}
	name := lowerASCII(tagName(tag))
	if name == "" {
		return false
	}
	rest := strings.TrimSpace(trimmed[next:])
	if isVoidTag(name, options) || isSelfClosing(tag) {
		return rest == ""
	}
	if !isUnformatted(name, options) {
		return false
	}
	closeTag := "</" + name + ">"
	closeIndex, closeEnd, ok := findSimpleCloseSpan(trimmed[next:], closeTag)
	if !ok {
		return false
	}
	return strings.TrimSpace(trimmed[next+closeEnd:]) == "" && closeIndex >= 0
}

func containsOnlyTagMarkup(content string) bool {
	trimmed := strings.TrimSpace(content)
	if trimmed == "" || !strings.HasPrefix(trimmed, "<") {
		return false
	}
	for i := 0; i < len(trimmed); {
		if trimmed[i] != '<' {
			if isASCIIWhitespace(trimmed[i]) {
				i++
				continue
			}
			return false
		}
		tag, next := readHTMLTag(trimmed, i)
		if tag == "" || next <= i {
			return false
		}
		i = next
	}
	return true
}

func containsBlockHandlebars(content string) bool {
	for i := 0; i < len(content); {
		index := strings.Index(content[i:], "{{")
		if index < 0 {
			return false
		}
		index += i
		token, next := readHandlebars(content, index)
		if token == "" {
			return false
		}
		if kind := handlebarsKind(token); kind == "open" || kind == "else" || kind == "close" {
			return true
		}
		i = next
	}
	return false
}

func findRawContentClose(source string, closeTag string) int {
	quote := byte(0)
	for i := 0; i < len(source); i++ {
		ch := source[i]
		if quote != 0 {
			if ch == '\\' && i+1 < len(source) {
				i++
			} else if ch == quote {
				quote = 0
			}
			continue
		}
		if ch == '"' || ch == '\'' {
			quote = ch
			continue
		}
		if hasPrefixASCIIFold(source[i:], closeTag) {
			return i
		}
	}
	return -1
}

func findUnformattedContentClose(source string, closeTag string) int {
	return indexASCIIFold(source, closeTag)
}

func (b *Beautifier) formatWrappedScriptStyleContent(content string, kind string) (string, bool) {
	trimmed := strings.TrimSpace(content)
	open, close := "", ""
	switch {
	case strings.HasPrefix(trimmed, "<!--"):
		open, close = "<!--", "-->"
	case strings.HasPrefix(trimmed, "<![CDATA["):
		open, close = "<![CDATA[", "]]>"
	default:
		return "", false
	}
	end := strings.LastIndex(trimmed, close)
	if end < 0 {
		return "", false
	}
	rawInner := trimmed[len(open):end]
	inner := formatNullRawContent(rawInner)
	if firstBreak := strings.IndexAny(rawInner, "\r\n"); firstBreak > 0 && strings.HasPrefix(open, "<!--") {
		firstLine := strings.TrimSpace(rawInner[:firstBreak])
		if firstLine != "" {
			inner = formatNullRawContent(rawInner[firstBreak:])
		}
	}
	var formatted string
	var err error
	switch kind {
	case "javascript":
		if b.js != nil {
			formatted, err = b.beautifyJavaScriptContent(inner)
		} else {
			formatted = inner
		}
	case "css":
		if b.css != nil {
			formatted, err = b.css(inner, b.options.RawOptions)
		} else {
			formatted = inner
		}
	case "html":
		formatted, err = Beautify(inner, b.options.RawOptions, b.js, b.css)
	case "null":
		formatted = formatNullRawContent(rawInner)
	default:
		formatted = inner
	}
	if err != nil {
		return "", false
	}
	lines := []string{open}
	if firstBreak := strings.IndexAny(rawInner, "\r\n"); firstBreak > 0 {
		firstLine := strings.TrimSpace(rawInner[:firstBreak])
		if firstLine != "" && strings.HasPrefix(open, "<!--") {
			lines[0] = open + " " + firstLine
		}
	}
	for _, line := range strings.Split(formatted, "\n") {
		line = strings.TrimRight(line, " \t\r")
		if strings.TrimSpace(line) != "" {
			lines = append(lines, line)
		}
	}
	lines = append(lines, close)
	return strings.Join(lines, "\n"), true
}

func (b *Beautifier) printToken(text string) {
	if text == "" {
		return
	}
	indent := b.indent + b.nextIndentBoost
	if hasTemplating(b.options.Templating, "angular") {
		indent += b.angularDepth
	}
	b.nextIndentBoost = 0
	b.output.SetIndent(indent, 0)
	b.output.AddToken(text)
}

func javascriptRawLineIndent(lines []string, index int, contentIndent int, preserveRaw bool) int {
	if !preserveRaw {
		return contentIndent
	}
	if index == 0 {
		return contentIndent
	}
	if len(lines) == 0 {
		return contentIndent
	}
	first := strings.TrimSpace(lines[0])
	wrapped := strings.HasPrefix(first, "<!--") || strings.HasPrefix(first, "<![CDATA[")
	if !wrapped {
		return -1
	}
	if index == 1 || index == len(lines)-1 {
		return contentIndent
	}
	return -1
}

func hasIndentedContinuation(source string) bool {
	lines := strings.Split(source, "\n")
	for _, line := range lines[1:] {
		if line == "" {
			continue
		}
		return hasLeadingASCIIWhitespace(line)
	}
	return false
}

func (b *Beautifier) printTokenAtIndent(text string, indent int) {
	if text == "" {
		return
	}
	b.output.SetIndent(indent, 0)
	b.output.AddToken(text)
}

func (b *Beautifier) rawContentIndent() int {
	switch b.options.IndentScripts {
	case "keep":
		return b.indent
	case "separate":
		return -1
	default:
		return b.indent + 1
	}
}

func (b *Beautifier) beautifyJavaScriptContent(source string) (string, error) {
	protected, replacements := protectPHPBlocks(source)
	if hasTemplating(b.options.Templating, "smarty") {
		protected, replacements = protectSmartyScriptBlocks(protected, replacements)
	}
	formatted, err := b.js(protected, b.options.RawOptions)
	if err != nil {
		return "", err
	}
	return restoreTemplateBlocks(formatted, replacements), nil
}

func (b *Beautifier) printBlock(text string) {
	if !b.output.JustAddedNewline() {
		b.output.AddNewLine(false)
	}
	for _, line := range strings.Split(text, "\n") {
		b.printToken(strings.TrimRight(line, " \t"))
		b.output.AddNewLine(false)
	}
}

func (b *Beautifier) printInlineComment(text string) {
	if b.output.JustAddedNewline() {
		b.printBlock(text)
		return
	}
	b.output.SpaceBeforeToken = true
	b.printToken(strings.TrimSpace(text))
}

func (b *Beautifier) printRawIgnoredText(text string) {
	if text == "" {
		return
	}
	if b.output.JustAddedNewline() {
		text = strings.TrimPrefix(text, "\n")
	}
	b.output.AddRawText(text)
}

func startsWithInlineComment(source string) bool {
	source = strings.TrimLeft(source, " \t")
	if !strings.HasPrefix(source, "<!--") {
		return false
	}
	end := strings.Index(source, "-->")
	return end >= 0 && !strings.Contains(source[:end], "\n")
}

func shouldPrintInlineComment(source string, index int) bool {
	for i := index - 1; i >= 0; i-- {
		switch source[i] {
		case ' ', '\t':
			continue
		case '\n':
			return false
		default:
			return true
		}
	}
	return false
}

func (b *Beautifier) printProcessingInstruction(text string) {
	text = strings.TrimSpace(text)
	if !strings.Contains(text, "\n") {
		b.printToken(text)
		return
	}
	if !b.output.JustAddedNewline() {
		b.output.AddNewLine(false)
	}
	b.output.AddRawText(text)
}

func (b *Beautifier) printText(text string) {
	hadLineBreak := strings.Contains(text, "\n")
	leadingNewlines := leadingLineBreakCount(text)
	previousToken := ""
	if b.output.PreviousLine != nil {
		previousToken = b.output.PreviousLine.Last()
	}
	if leadingNewlines > 0 && b.output.JustAddedNewline() && !preserveLeadingBlanklineAfter(previousToken) {
		if leadingNewlines > 1 && strings.EqualFold(strings.TrimSpace(previousToken), "<html>") {
			b.blankBeforeTag = true
		}
		leadingNewlines--
	}
	if leadingNewlines > 0 {
		b.printPreservedNewlines(leadingNewlines)
		text = strings.TrimLeft(text, " \t\r\n")
	}
	trailingNewlines := trailingLineBreakCount(text)
	leadingWhitespace := len(text) > 0 && (text[0] == ' ' || text[0] == '\t')
	trailingWhitespace := len(text) > 0 && (text[len(text)-1] == ' ' || text[len(text)-1] == '\t')
	trimmed := strings.TrimSpace(text)
	if trimmed == "" {
		if !strings.Contains(text, "\n") && len(text) > 0 && !b.output.JustAddedNewline() {
			b.output.SpaceBeforeToken = true
		}
		b.printPreservedNewlines(lineBreakCount(text))
		if hadLineBreak && b.inlineDepth > 0 {
			b.nextIndentBoost = 1
		}
		return
	}
	if leadingWhitespace && !b.output.JustAddedNewline() {
		b.output.SpaceBeforeToken = true
	}
	angularTemplating := hasTemplating(b.options.Templating, "angular")
	if strings.Contains(trimmed, "\n") && (containsAngularControlOpenLine(trimmed) || (angularTemplating && b.angularDepth > 0)) {
		if angularTemplating {
			b.printAngularText(trimmed)
		} else {
			b.printMultilineText(trimmed)
		}
	} else if b.multilineTextTag != "" && strings.Contains(trimmed, "\n") {
		b.printMultilineText(trimmed)
		if trailingNewlines == 0 {
			b.closeOnSameLine = true
		}
	} else if b.shouldWrapMultilinePlainText(trimmed) {
		b.printWrappedMultilinePlainText(trimmed)
	} else if lines, ok := b.wrapPlainText(trimmed); ok {
		for index, line := range lines {
			if index > 0 {
				b.output.AddNewLine(false)
				b.printTokenAtIndent(line, b.indent)
				continue
			}
			b.printToken(line)
		}
	} else {
		b.printToken(trimmed)
	}
	if trailingWhitespace && trailingNewlines == 0 {
		b.output.SpaceBeforeToken = true
	}
	if trailingNewlines > 0 {
		b.printPreservedNewlines(trailingNewlines)
		if b.indent == 0 && b.inlineDepth > 0 {
			b.nextIndentBoost = 1
		}
	}
}

func (b *Beautifier) printMultilineText(text string) {
	for index, line := range strings.Split(text, "\n") {
		if index > 0 {
			b.output.AddNewLine(false)
		}
		line = strings.TrimSpace(line)
		if line == "" {
			continue
		}
		b.printToken(line)
	}
}

func (b *Beautifier) printAngularText(text string) {
	for index, line := range strings.Split(text, "\n") {
		if index > 0 {
			b.output.AddNewLine(false)
		}
		line = strings.TrimSpace(line)
		if line == "" {
			continue
		}
		lineDepth := b.angularDepth
		if isAngularControlCloseLine(line) && lineDepth > 0 {
			lineDepth--
		}
		b.printTokenAtIndent(line, b.indent+lineDepth)
		if isAngularControlCloseLine(line) && b.angularDepth > 0 {
			b.angularDepth--
		}
		if isAngularControlOpenLine(line) {
			b.angularDepth++
		}
	}
}

func (b *Beautifier) wrapPlainText(text string) ([]string, bool) {
	if b.options.WrapLineLength <= 0 || text == "" || strings.ContainsAny(text, "\n\r<") {
		return nil, false
	}
	words := splitInlineWords(text, b.options.UnformattedContentDelimiter)
	if len(words) < 2 {
		return nil, false
	}
	lineLen := lineTailLen(b.output.CurrentLine.String())
	if b.output.JustAddedNewline() {
		lineLen = b.output.GetIndentSize(b.indent, 0)
	}
	if b.output.SpaceBeforeToken && !b.output.JustAddedNewline() {
		lineLen++
	}
	if lineLen+visualLen(text) <= b.options.WrapLineLength {
		return nil, false
	}
	var lines []string
	var current strings.Builder
	for _, word := range words {
		wordLen := visualLen(word)
		space := 0
		if current.Len() > 0 {
			space = 1
		}
		if current.Len() > 0 && lineLen+space+wordLen > b.options.WrapLineLength {
			lines = append(lines, current.String())
			current.Reset()
			lineLen = b.output.GetIndentSize(b.indent, 0)
			space = 0
		}
		if space > 0 {
			current.WriteByte(' ')
			lineLen++
		}
		current.WriteString(word)
		lineLen += wordLen
	}
	if current.Len() > 0 {
		lines = append(lines, current.String())
	}
	return lines, len(lines) > 1
}

func (b *Beautifier) shouldWrapMultilinePlainText(text string) bool {
	if b.options.WrapLineLength <= 0 || !strings.Contains(text, "\n") || strings.Contains(text, "<") {
		return false
	}
	trimmed := strings.TrimSpace(text)
	return !strings.HasPrefix(trimmed, "{{") &&
		!strings.HasPrefix(trimmed, "{#") &&
		!strings.HasPrefix(trimmed, "{%") &&
		!strings.HasPrefix(trimmed, "<?") &&
		!strings.HasPrefix(trimmed, "<%")
}

func (b *Beautifier) printWrappedMultilinePlainText(text string) {
	for lineIndex, line := range strings.Split(text, "\n") {
		if lineIndex > 0 {
			b.output.AddNewLine(false)
		}
		line = strings.TrimSpace(line)
		if line == "" {
			continue
		}
		if lines, ok := b.wrapPlainText(line); ok {
			for index, wrapped := range lines {
				if index > 0 {
					b.output.AddNewLine(false)
					b.printTokenAtIndent(wrapped, b.indent)
					continue
				}
				b.printToken(wrapped)
			}
			continue
		}
		b.printToken(line)
	}
}

func preserveLeadingBlanklineAfter(token string) bool {
	trimmed := lowerASCII(strings.TrimSpace(token))
	switch trimmed {
	case "<html>", "</body>", "</html>":
		return false
	case "":
		return false
	}
	if strings.Contains(trimmed, "</body>") {
		return false
	}
	return strings.HasPrefix(trimmed, "<") &&
		!strings.HasPrefix(trimmed, "<!") &&
		!strings.HasPrefix(trimmed, "<?") &&
		strings.HasSuffix(trimmed, ">")
}

func (b *Beautifier) printPreservedNewlines(count int) {
	if count <= 0 {
		return
	}
	if !b.options.PreserveNewlines {
		count = 1
	} else if count > b.options.MaxPreserveNewlines+1 {
		count = b.options.MaxPreserveNewlines + 1
	}
	for i := 0; i < count; i++ {
		b.output.AddNewLine(i > 0)
	}
}

func readHTMLTag(source string, i int) (string, int) {
	quote := byte(0)
	for j := i; j < len(source); j++ {
		ch := source[j]
		if quote != 0 {
			if ch == '\\' && j+1 < len(source) {
				j++
			} else if ch == quote {
				quote = 0
			}
			continue
		}
		if ch == '"' || ch == '\'' {
			quote = ch
			continue
		}
		if j > i {
			switch ch {
			case '<':
				if j+1 < len(source) && (source[j+1] == '?' || source[j+1] == '%') {
					if _, next := readTemplateInstruction(source, j); next > j {
						j = next - 1
						continue
					}
				}
			case '{':
				if j+1 < len(source) && (source[j+1] == '#' || source[j+1] == '%') {
					if _, next := readTemplateInstruction(source, j); next > j {
						j = next - 1
						continue
					}
				}
			}
		}
		if ch == '>' {
			return source[i : j+1], j + 1
		}
	}
	return source[i:], len(source)
}

func firstHTMLLineBreak(source string) string {
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

func normalizeHTMLLineBreaks(source string) string {
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

func isDynamicProcessingTag(tag string) bool {
	trimmed := strings.TrimSpace(tag)
	return strings.HasPrefix(trimmed, "<<?") || strings.HasPrefix(trimmed, "</<?") ||
		strings.HasPrefix(trimmed, "<<%") || strings.HasPrefix(trimmed, "</<%") ||
		strings.HasPrefix(trimmed, "<{#") || strings.HasPrefix(trimmed, "</{#") ||
		strings.HasPrefix(trimmed, "<{%") || strings.HasPrefix(trimmed, "</{%") ||
		strings.HasPrefix(trimmed, "<{a") || strings.HasPrefix(trimmed, "</{a") ||
		strings.HasPrefix(trimmed, "<{*") || strings.HasPrefix(trimmed, "</{*") ||
		strings.HasPrefix(trimmed, "<{literal}") || strings.HasPrefix(trimmed, "</{literal}")
}

func readProcessingInstruction(source string, i int) (string, int) {
	return readDelimitedInstruction(source, i, "?>")
}

func readERBInstruction(source string, i int) (string, int) {
	return readDelimitedInstruction(source, i, "%>")
}

func readBraceTemplateInstruction(source string, i int) (string, int) {
	switch {
	case strings.HasPrefix(source[i:], "{#"):
		return readDelimitedInstruction(source, i, "#}")
	case strings.HasPrefix(source[i:], "{%"):
		return readDelimitedInstruction(source, i, "%}")
	default:
		return "", i
	}
}

func (b *Beautifier) readSmartyInstruction(source string, i int) (string, int) {
	if !hasTemplating(b.options.Templating, "smarty") {
		return "", i
	}
	switch {
	case strings.HasPrefix(source[i:], "{literal}"):
		end := strings.Index(source[i+len("{literal}"):], "{/literal}")
		if end < 0 {
			return "", i
		}
		next := i + len("{literal}") + end + len("{/literal}")
		return source[i:next], next
	case strings.HasPrefix(source[i:], "{a"):
		return readDelimitedInstruction(source, i, "a}")
	case strings.HasPrefix(source[i:], "{*"):
		return readDelimitedInstruction(source, i, "*}")
	default:
		return "", i
	}
}

func readTemplateInstruction(source string, i int) (string, int) {
	switch {
	case strings.HasPrefix(source[i:], "<?"):
		return readProcessingInstruction(source, i)
	case strings.HasPrefix(source[i:], "<%"):
		return readERBInstruction(source, i)
	case strings.HasPrefix(source[i:], "{#"), strings.HasPrefix(source[i:], "{%"):
		return readBraceTemplateInstruction(source, i)
	default:
		return "", i
	}
}

func hasTemplating(items []string, name string) bool {
	for _, item := range items {
		if item == name {
			return true
		}
	}
	return false
}

func startsAngularControlDocument(source string) bool {
	trimmed := strings.TrimLeft(source, " \t\r\n")
	for _, prefix := range []string{"@if", "@for", "@switch"} {
		if strings.HasPrefix(trimmed, prefix) {
			return true
		}
	}
	return false
}

func isInlineAngularControlContent(content string) bool {
	trimmed := strings.TrimSpace(content)
	for _, prefix := range []string{"@if", "@for", "@switch"} {
		if strings.HasPrefix(trimmed, prefix) {
			return true
		}
	}
	return false
}

func containsAngularControlLine(content string) bool {
	for _, line := range strings.Split(content, "\n") {
		trimmed := strings.TrimSpace(line)
		if isAngularControlOpenLine(trimmed) || isAngularControlCloseLine(trimmed) {
			return true
		}
	}
	return false
}

func containsAngularControlOpenLine(content string) bool {
	for _, line := range strings.Split(content, "\n") {
		if isAngularControlOpenLine(strings.TrimSpace(line)) {
			return true
		}
	}
	return false
}

func isAngularControlOpenLine(line string) bool {
	if !strings.Contains(line, "{") {
		return false
	}
	for _, prefix := range []string{"@if", "@else", "@for", "@empty", "@switch", "@case", "@default", "@defer"} {
		if strings.HasPrefix(line, prefix) || strings.Contains(line, "} "+prefix) {
			return true
		}
	}
	if strings.HasSuffix(strings.TrimSpace(line), "{") {
		for _, marker := range []string{" @if", " @else", " @for", " @empty", " @switch", " @case", " @default", " @defer"} {
			if strings.Contains(line, marker) {
				return true
			}
		}
	}
	return false
}

func isAngularControlCloseLine(line string) bool {
	return line == "}" || strings.HasPrefix(line, "} ")
}

func isAngularControlInlineCloseLine(line string) bool {
	if strings.HasPrefix(line, "}") || strings.HasPrefix(line, "{{") {
		return false
	}
	for index := 0; index < len(line); index++ {
		if line[index] != '}' {
			continue
		}
		if index > 0 && line[index-1] == '}' {
			continue
		}
		if index+1 < len(line) && line[index+1] == '}' {
			continue
		}
		if index > 0 && line[index-1] == '{' {
			continue
		}
		return true
	}
	return false
}

func preserveInlineOuterSpace(original string, compacted string) string {
	if compacted == "" {
		return compacted
	}
	if hasLeadingASCIIWhitespace(original) && !strings.HasPrefix(compacted, " ") {
		compacted = " " + compacted
	}
	if hasTrailingASCIIWhitespace(original) && !strings.HasSuffix(compacted, " ") {
		compacted += " "
	}
	return compacted
}

func formatAngularControlDocument(source string, options *Options) string {
	lines := strings.Split(source, "\n")
	indent := 0
	var out []string
	for _, line := range lines {
		trimmed := strings.TrimSpace(line)
		if trimmed == "" {
			out = append(out, "")
			continue
		}
		if (strings.HasPrefix(trimmed, "}") || isAngularLineCloseTag(trimmed)) && indent > 0 {
			indent--
		}
		out = append(out, strings.Repeat(options.IndentChar, options.IndentSize*indent)+trimmed)
		opensAngularControl := isAngularControlOpenLine(trimmed)
		if strings.HasSuffix(trimmed, "{") || isAngularLineOpenTag(trimmed, options) {
			indent++
		}
		if !opensAngularControl && isAngularControlInlineCloseLine(trimmed) && indent > 0 {
			indent--
		}
	}
	return strings.Join(out, "\n")
}

func isAngularLineOpenTag(line string, options *Options) bool {
	if !strings.HasPrefix(line, "<") || strings.HasPrefix(line, "</") {
		return false
	}
	tag, next := readHTMLTag(line, 0)
	if tag == "" || next != len(line) {
		return false
	}
	name := lowerASCII(tagName(tag))
	return name != "" && !isVoidTag(name, options) && !isSelfClosing(tag)
}

func isAngularLineCloseTag(line string) bool {
	return strings.HasPrefix(line, "</")
}

func hasStandaloneHTMLBlockLine(source string, options *Options) bool {
	for _, line := range strings.Split(source, "\n") {
		trimmed := strings.TrimSpace(line)
		if !strings.HasPrefix(trimmed, "<") {
			continue
		}
		tag, next := readHTMLTag(trimmed, 0)
		if tag == "" || next != len(trimmed) {
			continue
		}
		name := lowerASCII(tagName(tag))
		if name == "" || strings.HasPrefix(name, "!") || strings.HasPrefix(name, "?") {
			continue
		}
		if !isInlineName(name, options) {
			return true
		}
	}
	return false
}

func readDelimitedInstruction(source string, i int, close string) (string, int) {
	end := strings.Index(source[i+2:], close)
	if end < 0 {
		return "", i
	}
	next := i + 2 + end + 2
	return source[i:next], next
}

func readHandlebars(source string, i int) (string, int) {
	if strings.HasPrefix(source[i:], "{{!--") {
		end := strings.Index(source[i+5:], "--}}")
		if end < 0 {
			return "", i
		}
		next := i + 5 + end + 4
		return source[i:next], next
	}
	end := strings.Index(source[i+2:], "}}")
	if end < 0 {
		return "", i
	}
	next := i + 2 + end + 2
	if next < len(source) && source[i:i+3] == "{{{" && source[next] == '}' {
		next++
	}
	return source[i:next], next
}

func (b *Beautifier) shouldFormatHandlebars(source string, start int, next int, token string) bool {
	kind := handlebarsKind(token)
	if kind == "single" {
		return false
	}
	if kind != "open" && b.handlebarsDepth == 0 {
		return false
	}
	if kind == "open" {
		after := strings.TrimLeft(source[next:], " \t")
		if strings.HasPrefix(after, "{{/") {
			return false
		}
		if innerToken, innerNext := readHandlebars(source, next); innerToken != "" && handlebarsKind(innerToken) == "single" {
			afterInner := strings.TrimLeft(source[innerNext:], " \t")
			if strings.HasPrefix(afterInner, "{{/") {
				return false
			}
		}
		if lineEnd := strings.IndexAny(source[next:], "\r\n"); lineEnd < 0 {
			sameLine := source[next:]
			if strings.Contains(sameLine, "{{/") && !containsBlockHandlebarsInlineContent(sameLine) {
				return false
			}
		} else if sameLine := source[next : next+lineEnd]; strings.Contains(sameLine, "{{/") && !containsBlockHandlebarsInlineContent(sameLine) {
			return false
		}
		if strings.HasPrefix(after, "<") {
			tag, _ := readHTMLTag(after, 0)
			name := lowerASCII(tagName(tag))
			return name != "" && !isInlineName(name, b.options)
		}
		return startsLine(source, start) || strings.HasPrefix(source[next:], "\n")
	}
	return true
}

func (b *Beautifier) printHandlebars(token string, following string) {
	token = normalizeHandlebars(token)
	kind := handlebarsKind(token)
	switch kind {
	case "close":
		if b.handlebarsDepth > 0 {
			b.handlebarsDepth--
		}
		if b.indent > 0 {
			b.indent--
		}
		if !b.output.JustAddedNewline() {
			b.output.AddNewLine(false)
		}
		b.printToken(token)
		if !strings.HasPrefix(strings.TrimLeft(following, " \t"), "</") {
			b.output.AddNewLine(false)
		} else {
			b.closeOnSameLine = true
		}
	case "else":
		if b.indent > 0 {
			b.indent--
		}
		if !b.output.JustAddedNewline() {
			b.output.AddNewLine(false)
		}
		b.printToken(token)
		b.output.AddNewLine(false)
		b.indent++
	case "open":
		b.printToken(token)
		b.indent++
		b.handlebarsDepth++
		b.output.AddNewLine(false)
	}
}

func handlebarsKind(token string) string {
	inner := strings.TrimSpace(strings.TrimSuffix(strings.TrimPrefix(token, "{{"), "}}"))
	inner = strings.TrimPrefix(inner, "~")
	switch {
	case strings.HasPrefix(inner, "#"), strings.HasPrefix(inner, "^"):
		return "open"
	case strings.HasPrefix(inner, "/"):
		return "close"
	case strings.HasPrefix(inner, "else"):
		return "else"
	default:
		return "single"
	}
}

func containsBlockHandlebarsInlineContent(content string) bool {
	return strings.Contains(content, "<") || strings.Contains(content, "{{else")
}

func normalizeHandlebars(token string) string {
	if strings.HasPrefix(token, "{{!") || strings.HasPrefix(token, "{{{") {
		return token
	}
	inner := strings.TrimSuffix(strings.TrimPrefix(token, "{{"), "}}")
	collapsed := collapseHandlebarsWhitespace(inner)
	trimmedInner := strings.TrimLeft(inner, " \t")
	trimmedInner = strings.TrimPrefix(trimmedInner, "~")
	if strings.HasPrefix(trimmedInner, "#>") && hasTrailingASCIIWhitespace(inner) {
		collapsed += " "
	}
	return "{{" + collapsed + "}}"
}

func collapseHandlebarsWhitespace(value string) string {
	var out strings.Builder
	quote := byte(0)
	inSpace := false
	for i := 0; i < len(value); i++ {
		ch := value[i]
		if quote != 0 {
			out.WriteByte(ch)
			if ch == '\\' && i+1 < len(value) {
				i++
				out.WriteByte(value[i])
			} else if ch == quote {
				quote = 0
			}
			continue
		}
		if ch == '"' || ch == '\'' || ch == '`' {
			quote = ch
			out.WriteByte(ch)
			inSpace = false
			continue
		}
		switch ch {
		case ' ', '\t', '\n', '\r':
			if !inSpace && out.Len() > 0 {
				out.WriteByte(' ')
			}
			inSpace = true
		default:
			out.WriteByte(ch)
			inSpace = false
		}
	}
	return strings.TrimSpace(out.String())
}

func startsLine(source string, index int) bool {
	for i := index - 1; i >= 0; i-- {
		switch source[i] {
		case ' ', '\t':
			continue
		case '\n':
			return true
		default:
			return false
		}
	}
	return true
}

func nextHTMLSpecialIndex(source string, options *Options) int {
	_ = options
	next := strings.IndexByte(source, '<')
	search := source
	if next >= 0 {
		search = source[:next]
	}
	offset := 0
	for {
		index := strings.Index(search[offset:], "{{")
		if index < 0 {
			break
		}
		index += offset
		token, _ := readHandlebars(source, index)
		if token == "" {
			break
		}
		if next < 0 || index < next {
			next = index
		}
		break
	}
	for _, marker := range []string{"{#", "{%"} {
		if index := strings.Index(search, marker); index >= 0 && (next < 0 || index < next) {
			next = index
		}
	}
	if hasTemplating(options.Templating, "smarty") {
		for _, marker := range []string{"{a", "{*", "{literal}"} {
			if index := strings.Index(search, marker); index >= 0 && (next < 0 || index < next) {
				next = index
			}
		}
	}
	return next
}

func tagName(tag string) string {
	if len(tag) == 0 || tag[0] != '<' {
		return ""
	}
	i := 1
	if i < len(tag) && tag[i] == '/' {
		i++
	}
	for i < len(tag) && isASCIIWhitespace(tag[i]) {
		i++
	}
	start := i
	if i < len(tag) && (tag[i] == '!' || tag[i] == '?') {
		i++
	}
	if strings.HasPrefix(tag[i:], "{{") {
		if end := strings.Index(tag[i+2:], "}}"); end >= 0 {
			return tag[start : i+2+end+2]
		}
		return tag[start:i]
	}
	for i < len(tag) && isHTMLNameByte(tag[i]) {
		i++
	}
	if i == start && start < len(tag) && (tag[start] == '!' || tag[start] == '?') {
		return tag[start : start+1]
	}
	if i == start {
		return ""
	}
	return tag[start:i]
}

func isHTMLNameByte(ch byte) bool {
	return (ch >= 'A' && ch <= 'Z') ||
		(ch >= 'a' && ch <= 'z') ||
		(ch >= '0' && ch <= '9') ||
		ch == ':' || ch == '_' || ch == '-'
}

func isCloseTag(tag string) bool {
	return strings.HasPrefix(strings.TrimSpace(tag), "</")
}

func isSelfClosing(tag string) bool {
	return strings.HasSuffix(strings.TrimSpace(tag), "/>")
}

func isVoidTag(name string, options *Options) bool {
	_, ok := options.voidElementSet[name]
	return ok
}

func isContentUnformatted(name string, options *Options) bool {
	_, ok := options.contentUnformattedSet[name]
	return ok
}

func isUnformatted(name string, options *Options) bool {
	_, ok := options.unformattedSet[name]
	return ok
}

func normalizeUnformattedRawElement(openTag, content, closeTag string) string {
	openTag = strings.Replace(openTag, " \n", "\n", 1)
	if index := strings.IndexByte(content, '\n'); index >= 0 {
		prefix := content[:index]
		if strings.Trim(prefix, " \t\r") == "" {
			content = content[index:]
		}
	}
	return openTag + content + closeTag
}

func isInlineName(name string, options *Options) bool {
	if strings.HasPrefix(name, "{{") {
		return true
	}
	if name == "svg" {
		return false
	}
	if _, ok := options.inlineSet[name]; ok {
		return true
	}
	return options.InlineCustomElements && (strings.Contains(name, "-") || strings.Contains(name, "_"))
}

func hasExtraLiner(name string, close bool, options *Options) bool {
	target := name
	if close {
		target = "/" + name
	}
	_, ok := options.extraLinerSet[target]
	return ok
}

func (b *Beautifier) formatTag(tag string) string {
	lineIndent := b.output.GetIndentString(b.indent, 0)
	if len(tag) <= 256 {
		key := formatTagCacheKey{tag: tag, indent: b.indent}
		if b.formatTagCache == nil {
			b.formatTagCache = map[formatTagCacheKey]string{}
		} else if formatted, ok := b.formatTagCache[key]; ok {
			return formatted
		}
		formatted := formatTag(tag, b.options, lineIndent)
		if len(b.formatTagCache) < 512 {
			b.formatTagCache[key] = formatted
		}
		return formatted
	}
	return formatTag(tag, b.options, lineIndent)
}

func formatTag(tag string, options *Options, lineIndent string) string {
	if strings.HasPrefix(strings.TrimSpace(tag), "<?") {
		return strings.TrimSpace(tag)
	}
	if options.WrapAttributes == "auto" && options.WrapLineLength > 0 && !strings.Contains(tag, "{{") {
		if formatted, ok := formatAutoWrappedRawTag(tag, options, lineIndent, options.WrapAttributesIndentSize, false); ok {
			return formatted
		}
	}
	normalized := normalizeTag(tag)
	if strings.HasPrefix(strings.TrimSpace(normalized), "<?") {
		return normalized
	}
	if options.WrapAttributes == "preserve" || options.WrapAttributes == "preserve-aligned" {
		return formatPreservedWrappedTag(tag, normalized, options, lineIndent)
	}
	if options.WrapAttributes == "auto" && options.WrapLineLength > 0 {
		return formatAutoWrappedTag(normalized, options, lineIndent, options.WrapAttributesIndentSize, false)
	}
	if options.WrapAttributes == "aligned-multiple" && options.WrapLineLength > 0 {
		name, _, _, _, ok := splitTag(normalized)
		if ok {
			return formatAutoWrappedTag(normalized, options, lineIndent, len(name)+2, true)
		}
	}
	if options.WrapAttributes != "force" && options.WrapAttributes != "force-aligned" && options.WrapAttributes != "force-expand-multiline" {
		return normalized
	}
	name, attrs, closing, selfClosing, ok := splitTag(normalized)
	if !ok || closing || len(attrs) == 0 {
		return normalized
	}
	if len(attrs) < options.WrapAttributesMinAttrs {
		return normalized
	}
	indentSize := options.WrapAttributesIndentSize
	if options.WrapAttributes == "force-aligned" {
		indentSize = len(name) + 2
	}
	if indentSize < 0 {
		indentSize = 0
	}
	var out strings.Builder
	out.WriteByte('<')
	out.WriteString(name)
	if options.WrapAttributes == "force-expand-multiline" {
		out.WriteByte('\n')
		out.WriteString(lineIndent)
		out.WriteString(indentColumns(options, indentSize))
	} else if options.WrapAttributes == "force" &&
		options.WrapLineLength > 0 &&
		visualLen(lineIndent)+visualLen(name)+2+visualLen(attrs[0]) > options.WrapLineLength {
		out.WriteByte('\n')
		out.WriteString(lineIndent)
		out.WriteString(indentColumns(options, indentSize))
	} else {
		out.WriteByte(' ')
	}
	out.WriteString(attrs[0])
	indent := "\n" + lineIndent + indentColumns(options, indentSize)
	for _, attr := range attrs[1:] {
		out.WriteString(indent)
		out.WriteString(attr)
	}
	if selfClosing {
		if options.WrapAttributes == "force-expand-multiline" {
			out.WriteByte('\n')
			out.WriteString(lineIndent)
		} else {
			out.WriteByte(' ')
		}
		out.WriteString("/>")
	} else if options.WrapAttributes == "force-expand-multiline" {
		out.WriteByte('\n')
		out.WriteString(lineIndent)
		out.WriteByte('>')
	} else {
		out.WriteByte('>')
	}
	return out.String()
}

func formatAutoWrappedRawTag(tag string, options *Options, lineIndent string, wrapIndentSize int, keepFirstAttributeInline bool) (string, bool) {
	if normalized, ok := normalizeIncompleteTag(tag); ok {
		return formatAutoWrappedTag(normalized, options, lineIndent, wrapIndentSize, keepFirstAttributeInline), true
	}
	name, attrs, closing, selfClosing, ok := splitTag(tag)
	if !ok {
		return strings.Join(strings.Fields(tag), " "), true
	}
	if closing {
		inner := strings.TrimSpace(strings.TrimSuffix(strings.TrimPrefix(strings.TrimSpace(tag), "</"), ">"))
		if strings.HasPrefix(inner, "{{") {
			return strings.TrimSpace(tag), true
		}
		return "</" + name + ">", true
	}
	if len(attrs) == 0 {
		if selfClosing {
			return "<" + name + " />", true
		}
		return "<" + name + ">", true
	}
	return formatAutoWrappedTagParts(name, attrs, selfClosing, options, lineIndent, wrapIndentSize, keepFirstAttributeInline), true
}

func formatAutoWrappedTag(tag string, options *Options, lineIndent string, wrapIndentSize int, keepFirstAttributeInline bool) string {
	name, attrs, closing, selfClosing, ok := splitTag(tag)
	if !ok || closing || len(attrs) == 0 {
		return tag
	}
	return formatAutoWrappedTagParts(name, attrs, selfClosing, options, lineIndent, wrapIndentSize, keepFirstAttributeInline)
}

func formatAutoWrappedTagParts(name string, attrs []string, selfClosing bool, options *Options, lineIndent string, wrapIndentSize int, keepFirstAttributeInline bool) string {
	currentLen := len(lineIndent) + 1 + len(name)
	indent := lineIndent + indentColumns(options, wrapIndentSize)
	indentLen := len(indent)
	var out strings.Builder
	grow := len(name) + len(attrs) + 3
	for _, attr := range attrs {
		grow += len(attr)
	}
	if len(attrs) > 1 {
		grow += len(indent) * (len(attrs) - 1)
	}
	out.Grow(grow)
	out.WriteByte('<')
	out.WriteString(name)
	wrapped := false
	for index, attr := range attrs {
		attrLen := len(attr)
		if index == 0 && keepFirstAttributeInline {
			out.WriteByte(' ')
			out.WriteString(attr)
			currentLen += 1 + attrLen
		} else if currentLen+1+attrLen > options.WrapLineLength {
			out.WriteByte('\n')
			out.WriteString(indent)
			out.WriteString(attr)
			currentLen = indentLen + attrLen
			wrapped = true
		} else {
			out.WriteByte(' ')
			out.WriteString(attr)
			currentLen += 1 + attrLen
		}
	}
	end := ">"
	if selfClosing {
		end = " />"
	}
	if wrapped && currentLen+len(end) > options.WrapLineLength {
		out.WriteByte('\n')
		out.WriteString(lineIndent)
		out.WriteString(strings.TrimSpace(end))
	} else {
		out.WriteString(end)
	}
	return spaceHandlebarsControlBoundaries(out.String())
}

func formatPreservedWrappedTag(tag string, normalized string, options *Options, lineIndent string) string {
	if !strings.ContainsAny(tag, "\n\r") {
		return normalized
	}
	trimmed := strings.TrimSpace(normalizeHTMLLineBreaks(tag))
	if len(trimmed) < 3 || trimmed[0] != '<' || strings.HasPrefix(trimmed, "</") || strings.HasPrefix(trimmed, "<!") || !strings.HasSuffix(trimmed, ">") {
		return normalized
	}
	inner := strings.TrimSpace(trimmed[1 : len(trimmed)-1])
	selfClosing := false
	if strings.HasSuffix(inner, "/") {
		selfClosing = true
		inner = strings.TrimSpace(strings.TrimSuffix(inner, "/"))
	}
	lines := strings.Split(inner, "\n")
	if len(lines) == 0 {
		return normalized
	}
	firstFields := splitTagFields(strings.TrimSpace(lines[0]))
	if len(firstFields) == 0 {
		return normalized
	}
	name := firstFields[0]
	indentSize := options.WrapAttributesIndentSize
	if options.WrapAttributes == "preserve-aligned" {
		indentSize = len(name) + 2
	}
	continuation := lineIndent + indentColumns(options, indentSize)
	var out strings.Builder
	out.WriteByte('<')
	out.WriteString(name)
	if len(firstFields) > 1 {
		out.WriteByte(' ')
		out.WriteString(strings.Join(firstFields[1:], " "))
	}
	wroteContinuation := false
	for _, line := range lines[1:] {
		fields := splitTagFields(strings.TrimSpace(line))
		if len(fields) == 0 {
			continue
		}
		out.WriteByte('\n')
		out.WriteString(continuation)
		out.WriteString(strings.Join(fields, " "))
		wroteContinuation = true
	}
	if selfClosing {
		if wroteContinuation || len(firstFields) > 1 {
			out.WriteByte(' ')
		}
		out.WriteString("/>")
	} else {
		out.WriteByte('>')
	}
	return out.String()
}

func normalizeTag(tag string) string {
	if normalized, ok := normalizeIncompleteTag(tag); ok {
		return normalized
	}
	name, attrs, closing, selfClosing, ok := splitTag(tag)
	if !ok {
		return strings.Join(strings.Fields(tag), " ")
	}
	if closing {
		inner := strings.TrimSpace(strings.TrimSuffix(strings.TrimPrefix(strings.TrimSpace(tag), "</"), ">"))
		if strings.HasPrefix(inner, "{{") {
			return strings.TrimSpace(tag)
		}
		return "</" + name + ">"
	}
	if len(attrs) == 0 {
		if selfClosing {
			return "<" + name + " />"
		}
		return "<" + name + ">"
	}
	end := ">"
	if selfClosing {
		end = " />"
	}
	return spaceHandlebarsControlBoundaries("<" + name + " " + strings.Join(attrs, " ") + end)
}

func normalizeIncompleteTag(tag string) (string, bool) {
	trimmed := strings.TrimSpace(tag)
	if len(trimmed) < 2 || trimmed[0] != '<' || strings.HasPrefix(trimmed, "</") ||
		strings.HasPrefix(trimmed, "<!") || strings.HasSuffix(trimmed, ">") {
		return "", false
	}
	fields := splitTagFields(strings.TrimSpace(trimmed[1:]))
	if len(fields) == 0 {
		return "", false
	}
	if len(fields) == 1 {
		return "<" + fields[0], true
	}
	return spaceHandlebarsControlBoundaries("<" + fields[0] + " " + strings.Join(fields[1:], " ")), true
}

func splitTag(tag string) (name string, attrs []string, closing bool, selfClosing bool, ok bool) {
	trimmed := strings.TrimSpace(tag)
	if len(trimmed) < 3 || trimmed[0] != '<' || strings.HasPrefix(trimmed, "<!") {
		return "", nil, false, false, false
	}
	if strings.HasPrefix(trimmed, "</") {
		inner := strings.TrimSpace(strings.TrimSuffix(strings.TrimPrefix(trimmed, "</"), ">"))
		fields := strings.Fields(inner)
		if len(fields) == 0 {
			return "", nil, false, false, false
		}
		return fields[0], nil, true, false, true
	}
	if !strings.HasSuffix(trimmed, ">") {
		return "", nil, false, false, false
	}
	inner := strings.TrimSpace(trimmed[1 : len(trimmed)-1])
	if strings.HasSuffix(inner, "/") {
		selfClosing = true
		inner = strings.TrimSpace(strings.TrimSuffix(inner, "/"))
	}
	fields := splitTagFields(inner)
	if len(fields) == 0 {
		return "", nil, false, false, false
	}
	return fields[0], fields[1:], false, selfClosing, true
}

func splitTagFields(inner string) []string {
	inner = protectHandlebarsSpaces(inner)
	var fields []string
	start := -1
	quote := byte(0)
	for i := 0; i < len(inner); i++ {
		ch := inner[i]
		if quote != 0 {
			if ch == quote {
				quote = 0
			}
			continue
		}
		switch ch {
		case '\'', '"':
			if start < 0 {
				start = i
			}
			quote = ch
		case ' ', '\t', '\n', '\r':
			if start >= 0 {
				fields = append(fields, restoreProtectedSpaces(inner[start:i]))
				start = -1
			}
		default:
			if start < 0 {
				start = i
			}
		}
	}
	if start >= 0 {
		fields = append(fields, restoreProtectedSpaces(inner[start:]))
	}
	return normalizeTagFields(fields)
}

func normalizeTagFields(fields []string) []string {
	if len(fields) == 0 {
		return nil
	}
	out := []string{fields[0]}
	for i := 1; i < len(fields); i++ {
		field := fields[i]
		switch {
		case i+2 < len(fields) && fields[i+1] == "=":
			out = append(out, normalizeAttr(field+"="+fields[i+2]))
			i += 2
		case strings.HasSuffix(field, "=") && i+1 < len(fields):
			out = append(out, normalizeAttr(field+fields[i+1]))
			i++
		case i+1 < len(fields) && strings.HasPrefix(fields[i+1], "="):
			out = append(out, normalizeAttr(field+fields[i+1]))
			i++
		default:
			out = append(out, normalizeAttr(field))
		}
	}
	return out
}

func protectHandlebarsSpaces(inner string) string {
	if !strings.Contains(inner, "{{") {
		return inner
	}
	var out strings.Builder
	for i := 0; i < len(inner); {
		if strings.HasPrefix(inner[i:], "{{") {
			end := strings.Index(inner[i+2:], "}}")
			if end < 0 {
				out.WriteString(inner[i:])
				break
			}
			block := inner[i : i+2+end+2]
			block = handlebarsSpaceProtector.Replace(block)
			out.WriteString(block)
			i += 2 + end + 2
			continue
		}
		out.WriteByte(inner[i])
		i++
	}
	return out.String()
}

func restoreProtectedSpaces(value string) string {
	if !strings.ContainsAny(value, "\x00\x01\x02\x03") {
		return value
	}
	return handlebarsSpaceRestorer.Replace(value)
}

func normalizeAttr(attr string) string {
	trimmed := strings.TrimSpace(attr)
	if !strings.Contains(trimmed, "=") {
		return trimmed
	}
	left, right, _ := strings.Cut(trimmed, "=")
	normalizedLeft := strings.TrimSpace(left)
	normalizedRight := strings.TrimSpace(right)
	if trimmed == attr && normalizedLeft == left && normalizedRight == right {
		return attr
	}
	return normalizedLeft + "=" + normalizedRight
}

func spaceHandlebarsControlBoundaries(value string) string {
	if !strings.Contains(value, "{{/") && !strings.Contains(value, "{{else") {
		return value
	}
	var out strings.Builder
	quote := byte(0)
	last := byte(0)
	for i := 0; i < len(value); i++ {
		ch := value[i]
		if quote != 0 {
			out.WriteByte(ch)
			last = ch
			if ch == '\\' && i+1 < len(value) {
				i++
				out.WriteByte(value[i])
				last = value[i]
			} else if ch == quote {
				quote = 0
			}
			continue
		}
		if ch == '"' || ch == '\'' {
			quote = ch
			out.WriteByte(ch)
			last = ch
			continue
		}
		if strings.HasPrefix(value[i:], "{{/") || strings.HasPrefix(value[i:], "{{else") {
			if needsSpaceBeforeHandlebarsControlByte(last) {
				out.WriteByte(' ')
				last = ' '
			}
		}
		out.WriteByte(ch)
		last = ch
	}
	return out.String()
}

func needsSpaceBeforeHandlebarsControlByte(last byte) bool {
	return last != 0 && last != ' ' && last != '\t' && last != '\n' && last != '<' && last != '{'
}

func customBeautifierName(tag string, tagName string) string {
	typeAttr := typeAttribute(tag)
	if typeAttr == "" {
		if tagName == "style" {
			return "css"
		}
		return "javascript"
	}
	switch {
	case strings.Contains(typeAttr, "test/null"):
		return "null"
	case strings.Contains(typeAttr, "text/css"):
		return "css"
	case htmlBeautifierTypeRE.MatchString(typeAttr):
		return "html"
	case scriptBeautifierTypeRE.MatchString(typeAttr):
		return "javascript"
	case typeAttr == "importmap" && os.Getenv("JS_BEAUTIFY_GO_LEGACY") != "":
		return "javascript"
	default:
		return ""
	}
}

func langDisabled(kind string, options map[string]any) bool {
	field := ""
	switch kind {
	case "javascript":
		field = "js"
	case "css":
		field = "css"
	default:
		return false
	}
	child, ok := options[field].(map[string]any)
	if !ok {
		return false
	}
	value, ok := child["disabled"]
	if !ok {
		return false
	}
	disabled, ok := value.(bool)
	return ok && disabled
}

func typeAttribute(tag string) string {
	_, attrs, _, _, ok := splitTag(tag)
	if !ok {
		return ""
	}
	for _, attr := range attrs {
		name, value, ok := strings.Cut(attr, "=")
		if !ok || !equalASCIIFold(strings.TrimSpace(name), "type") {
			continue
		}
		value = strings.TrimSpace(value)
		if len(value) >= 2 {
			if (value[0] == '"' && value[len(value)-1] == '"') || (value[0] == '\'' && value[len(value)-1] == '\'') {
				value = value[1 : len(value)-1]
			}
		}
		return value
	}
	return ""
}

func compactUnknownRawContent(content string) string {
	if !strings.ContainsAny(content, "\n\r\t ") {
		return content
	}
	if strings.HasPrefix(content, "\n") || strings.HasPrefix(content, "\r") {
		return content
	}
	leftTrimmed := strings.TrimLeft(content, " \t\r\n")
	if leftTrimmed == content {
		return content
	}
	return " " + leftTrimmed
}

func isIncompleteWrappedRaw(content string) bool {
	trimmed := strings.TrimSpace(content)
	return (strings.HasPrefix(trimmed, "<!--") && !strings.Contains(trimmed, "-->")) ||
		(strings.HasPrefix(trimmed, "<![CDATA[") && !strings.Contains(trimmed, "]]>"))
}

func protectPHPBlocks(source string) (string, []string) {
	var replacements []string
	var out strings.Builder
	for i := 0; i < len(source); {
		if strings.HasPrefix(source[i:], "<?") {
			end := strings.Index(source[i+2:], "?>")
			if end >= 0 {
				raw := source[i : i+2+end+2]
				placeholder := "__JSB_PHP_BLOCK_" + string(rune('A'+len(replacements))) + "__"
				replacements = append(replacements, raw)
				out.WriteString(placeholder)
				i += len(raw)
				continue
			}
		}
		out.WriteByte(source[i])
		i++
	}
	return out.String(), replacements
}

func restorePHPBlocks(source string, replacements []string) string {
	return restoreTemplateBlocks(source, replacements)
}

func protectSmartyScriptBlocks(source string, replacements []string) (string, []string) {
	var out strings.Builder
	for i := 0; i < len(source); {
		if strings.HasPrefix(source[i:], "{$") {
			end := strings.IndexByte(source[i:], '}')
			if end >= 0 {
				raw := source[i : i+end+1]
				placeholder := "__JSB_PHP_BLOCK_" + string(rune('A'+len(replacements))) + "__"
				replacements = append(replacements, raw)
				out.WriteString(placeholder)
				i += len(raw)
				continue
			}
		}
		out.WriteByte(source[i])
		i++
	}
	return out.String(), replacements
}

func restoreTemplateBlocks(source string, replacements []string) string {
	for i, raw := range replacements {
		placeholder := "__JSB_PHP_BLOCK_" + string(rune('A'+i)) + "__"
		source = strings.ReplaceAll(source, placeholder, raw)
	}
	return source
}

func formatNullRawContent(content string) string {
	content = strings.TrimRight(content, "\n\r\t ")
	lines := strings.Split(strings.Trim(content, "\n\r"), "\n")
	for len(lines) > 0 && strings.TrimSpace(lines[0]) == "" {
		lines = lines[1:]
	}
	for len(lines) > 0 && strings.TrimSpace(lines[len(lines)-1]) == "" {
		lines = lines[:len(lines)-1]
	}
	if len(lines) == 0 {
		return ""
	}
	trimPrefix := firstLineIndent(lines)
	for i, line := range lines {
		line = strings.TrimRight(line, " \t\r")
		if trimPrefix != "" {
			line = strings.TrimPrefix(line, trimPrefix)
		}
		lines[i] = line
	}
	return strings.Join(lines, "\n")
}

func formatDisabledRawContent(content string) (string, bool) {
	content = strings.Trim(content, "\r\n")
	lines := strings.Split(content, "\n")
	for len(lines) > 0 && strings.TrimSpace(lines[0]) == "" {
		lines = lines[1:]
	}
	for len(lines) > 0 && strings.TrimSpace(lines[len(lines)-1]) == "" {
		lines = lines[:len(lines)-1]
	}
	if len(lines) == 0 {
		return "", false
	}
	firstIndented := hasLeadingASCIIWhitespace(lines[0])
	for i, line := range lines {
		lines[i] = strings.TrimRight(line, " \t\r")
	}
	return strings.Join(lines, "\n"), firstIndented
}

func isSingleLineWhitespace(content string) bool {
	return content != "" && !strings.ContainsAny(content, "\r\n") && strings.Trim(content, " \t") == ""
}

func firstLineIndent(lines []string) string {
	for _, line := range lines {
		if strings.TrimSpace(line) == "" {
			continue
		}
		return leadingIndent(line)
	}
	return ""
}

func (b *Beautifier) readSimpleHTMLContent(source string, start int, closeTag string, openTag string) (string, int, int, bool) {
	options := b.options
	closeIndex, closeEnd, ok := b.findSimpleCloseSpan(source, start, closeTag)
	if !ok {
		return "", 0, 0, false
	}
	content := source[start : start+closeIndex]
	if containsHTMLIgnoreDirectiveSpan(content) {
		return content, start + closeIndex, start + closeEnd, true
	}
	if strings.ContainsAny(content, "\n\r") && containsLineBreakOutsideTag(content) {
		if !options.PreserveNewlines && !strings.Contains(content, "<") {
			compacted := collapseInlineText(content, options.UnformattedContentDelimiter)
			return preserveInlineOuterSpace(content, compacted), start + closeIndex, start + closeEnd, true
		}
		trimmed := strings.TrimSpace(content)
		if strings.HasPrefix(trimmed, "{{") {
			if token, next := readHandlebars(trimmed, 0); token != "" && next == len(trimmed) && handlebarsKind(token) == "single" {
				return token, start + closeIndex, start + closeEnd, true
			}
		}
		return "", 0, 0, false
	}
	if options.UnformattedContentDelimiter != "" && strings.Contains(content, options.UnformattedContentDelimiter) {
		return content, start + closeIndex, start + closeEnd, true
	}
	if !isInlineOnlyHTML(content, options) {
		return "", 0, 0, false
	}
	compacted := compactInlineHTML(content, options)
	if isInlineAngularControlContent(content) {
		compacted = preserveInlineOuterSpace(content, compacted)
	}
	if compacted == "" && strings.TrimSpace(content) == "" && content != "" && strings.Contains(openTag, "{{") {
		compacted = " "
	}
	return compacted, start + closeIndex, start + closeEnd, true
}

func shouldTrySimpleHTMLContent(source string, start int, name string, closeTag string, options *Options) bool {
	if isInlineName(name, options) || isUnformatted(name, options) {
		return true
	}
	for i := start; i < len(source); i++ {
		if isASCIIWhitespace(source[i]) {
			continue
		}
		if source[i] != '<' {
			return true
		}
		if hasPrefixASCIIFold(source[i:], closeTag) {
			return true
		}
		nearClose := indexASCIIFoldLimit(source[i:], closeTag, 512) >= 0
		if strings.HasPrefix(source[i:], "<!--") {
			return true
		}
		if strings.HasPrefix(source[i:], "<?") || strings.HasPrefix(source[i:], "<%") {
			return nearClose
		}
		tag, _ := readHTMLTag(source, i)
		if tag == "" {
			return false
		}
		childName := lowerASCII(tagName(tag))
		if strings.HasPrefix(childName, "?") {
			return nearClose
		}
		return nearClose && childName != "" &&
			(isInlineName(childName, options) ||
				isUnformatted(childName, options) ||
				isContentUnformatted(childName, options) ||
				isVoidTag(childName, options) ||
				isSelfClosing(tag))
	}
	return false
}

func containsHTMLIgnoreDirectiveSpan(content string) bool {
	return containsASCIIFold(content, "beautify ignore:start") && containsASCIIFold(content, "beautify ignore:end")
}

func findSimpleCloseIndex(source string, closeTag string) int {
	start, _, ok := findSimpleCloseSpan(source, closeTag)
	if !ok {
		return -1
	}
	return start
}

func findSimpleCloseSpan(source string, closeTag string) (int, int, bool) {
	name := lowerASCII(tagName(closeTag))
	if strings.HasPrefix(name, "{{") {
		return -1, -1, false
	}
	if name == "" {
		index := indexASCIIFold(source, closeTag)
		if index < 0 {
			return -1, -1, false
		}
		return index, index + len(closeTag), true
	}
	if index := indexASCIIFold(source, closeTag); index >= 0 &&
		!hasCloseTagNameBefore(source[:index], name) &&
		!hasOpenTagNameBefore(source[:index], name) {
		return index, index + len(closeTag), true
	}
	depth := 0
	for offset := 0; offset < len(source); {
		nextTag := strings.IndexByte(source[offset:], '<')
		if nextTag < 0 {
			return -1, -1, false
		}
		index := offset + nextTag
		tag, next := readHTMLTag(source, index)
		if tag == "" || next <= index {
			offset = index + 1
			continue
		}
		tagName := tagName(tag)
		if equalASCIIFold(tagName, name) && isCloseTag(tag) {
			if depth == 0 {
				return index, next, true
			}
			depth--
			offset = next
			continue
		}
		if equalASCIIFold(tagName, name) && !isSelfClosing(tag) {
			depth++
		}
		offset = next
	}
	return -1, -1, false
}

func hasOpenTagNameBefore(source string, name string) bool {
	for offset := 0; offset < len(source); {
		nextTag := strings.IndexByte(source[offset:], '<')
		if nextTag < 0 {
			return false
		}
		i := offset + nextTag + 1
		if i >= len(source) {
			return false
		}
		if source[i] == '/' || source[i] == '!' || source[i] == '?' {
			offset = i + 1
			continue
		}
		for i < len(source) && isASCIIWhitespace(source[i]) {
			i++
		}
		start := i
		for i < len(source) && isHTMLNameByte(source[i]) {
			i++
		}
		if i > start && equalASCIIFold(source[start:i], name) {
			if i >= len(source) || isASCIIWhitespace(source[i]) || source[i] == '>' || source[i] == '/' {
				return true
			}
		}
		offset = i
	}
	return false
}

func hasCloseTagNameBefore(source string, name string) bool {
	for offset := 0; offset < len(source); {
		nextTag := strings.Index(source[offset:], "</")
		if nextTag < 0 {
			return false
		}
		i := offset + nextTag + 2
		for i < len(source) && isASCIIWhitespace(source[i]) {
			i++
		}
		start := i
		for i < len(source) && isHTMLNameByte(source[i]) {
			i++
		}
		if i > start && equalASCIIFold(source[start:i], name) {
			if i >= len(source) || isASCIIWhitespace(source[i]) || source[i] == '>' {
				return true
			}
		}
		offset = i
	}
	return false
}

func containsASCIIFold(source string, needle string) bool {
	return indexASCIIFold(source, needle) >= 0
}

func indexASCIIFold(source string, needle string) int {
	return indexASCIIFoldLimit(source, needle, len(source))
}

func indexASCIIFoldLimit(source string, needle string, limit int) int {
	if needle == "" {
		return 0
	}
	if len(needle) > len(source) {
		return -1
	}
	if limit > len(source) {
		limit = len(source)
	}
	if limit < len(needle) {
		return -1
	}
	first := toLowerASCII(needle[0])
	last := limit - len(needle)
	for i := 0; i <= last; i++ {
		if toLowerASCII(source[i]) == first && hasPrefixASCIIFold(source[i:], needle) {
			return i
		}
	}
	return -1
}

func hasPrefixASCIIFold(source string, prefix string) bool {
	if len(prefix) > len(source) {
		return false
	}
	return equalASCIIFold(source[:len(prefix)], prefix)
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

func lowerASCII(value string) string {
	for i := 0; i < len(value); i++ {
		ch := value[i]
		if ch >= 'A' && ch <= 'Z' {
			var out strings.Builder
			out.Grow(len(value))
			out.WriteString(value[:i])
			out.WriteByte(ch + ('a' - 'A'))
			for j := i + 1; j < len(value); j++ {
				next := value[j]
				if next >= 'A' && next <= 'Z' {
					next += 'a' - 'A'
				}
				out.WriteByte(next)
			}
			return out.String()
		}
	}
	return value
}

func isInlineOnlyHTML(content string, options *Options) bool {
	for i := 0; i < len(content); {
		if content[i] != '<' {
			i++
			continue
		}
		if strings.HasPrefix(content[i:], "<?") || strings.HasPrefix(content[i:], "<%") ||
			strings.HasPrefix(content[i:], "{#") || strings.HasPrefix(content[i:], "{%") {
			token, next := readTemplateInstruction(content, i)
			if token == "" {
				return false
			}
			i = next
			continue
		}
		tag, next := readHTMLTag(content, i)
		if tag == "" {
			return false
		}
		name := lowerASCII(tagName(tag))
		if name == "" {
			return false
		}
		if strings.HasPrefix(name, "!") {
			return false
		}
		if strings.HasPrefix(name, "?") {
			i = next
			continue
		}
		if name != "" && !isInlineName(name, options) && !isUnformatted(name, options) {
			return false
		}
		if !isCloseTag(tag) && !isSelfClosing(tag) && (isInlineName(name, options) || isUnformatted(name, options)) {
			closeTag := "</" + name + ">"
			if _, closeEnd, ok := findSimpleCloseSpan(content[next:], closeTag); ok {
				i = next + closeEnd
				continue
			}
		}
		i = next
	}
	return true
}

func compactInlineHTML(content string, options *Options) string {
	var out strings.Builder
	for i := 0; i < len(content); {
		if content[i] != '<' {
			next := strings.IndexByte(content[i:], '<')
			if next < 0 {
				next = len(content) - i
			}
			segment := content[i : i+next]
			collapsed := collapseInlineText(segment, options.UnformattedContentDelimiter)
			if collapsed != "" {
				if hasLeadingASCIIWhitespace(segment) && out.Len() > 0 {
					out.WriteByte(' ')
				}
				out.WriteString(collapsed)
				if hasTrailingASCIIWhitespace(segment) && i+next < len(content) && content[i+next] == '<' {
					out.WriteByte(' ')
				}
			}
			i += next
			continue
		}
		if strings.HasPrefix(content[i:], "<?") || strings.HasPrefix(content[i:], "<%") ||
			strings.HasPrefix(content[i:], "{#") || strings.HasPrefix(content[i:], "{%") {
			token, next := readTemplateInstruction(content, i)
			if token == "" {
				out.WriteByte(content[i])
				i++
				continue
			}
			out.WriteString(strings.TrimSpace(token))
			i = next
			continue
		}
		tag, next := readHTMLTag(content, i)
		if tag == "" {
			out.WriteByte(content[i])
			i++
			continue
		}
		name := lowerASCII(tagName(tag))
		if name != "" && !isCloseTag(tag) && !isVoidTag(name, options) && !isSelfClosing(tag) && isContentUnformatted(name, options) {
			closeTag := "</" + name + ">"
			if closeIndex, closeEnd, ok := findSimpleCloseSpan(content[next:], closeTag); ok {
				out.WriteString(formatTag(tag, options, ""))
				out.WriteString(content[next : next+closeIndex])
				out.WriteString(content[next+closeIndex : next+closeEnd])
				i = next + closeEnd
				continue
			}
		}
		out.WriteString(formatTag(tag, options, ""))
		i = next
	}
	return out.String()
}

func wrapInlineContent(openTag, content, closeTag string, options *Options, lineIndent string, initialLineLen int) string {
	if options.WrapLineLength <= 0 || content == "" {
		return content
	}
	if initialLineLen == 0 {
		initialLineLen = lineTailLen(lineIndent + openTag)
	}
	if initialLineLen+visualLen(content)+visualLen(closeTag) <= options.WrapLineLength {
		return content
	}
	if strings.Contains(content, "{{") {
		continuation := lineIndent + indentColumns(options, options.IndentSize)
		if initialLineLen+visualLen(content) <= options.WrapLineLength {
			return content + "\n" + lineIndent
		}
		return "\n" + continuation + content + "\n" + lineIndent
	}
	if strings.Contains(content, "<") {
		return wrapInlineHTMLContent(openTag, content, closeTag, options, lineIndent, initialLineLen)
	}
	words := splitInlineWords(content, options.UnformattedContentDelimiter)
	if len(words) < 2 {
		if isInlineName(lowerASCII(tagName(openTag)), options) {
			return content
		}
		if strings.Contains(content, "<") && initialLineLen+visualLen(content) > options.WrapLineLength {
			continuation := lineIndent + indentColumns(options, options.IndentSize)
			return "\n" + continuation + content + "\n" + lineIndent
		}
		if initialLineLen+visualLen(content)+visualLen(closeTag) > options.WrapLineLength {
			return content + "\n" + lineIndent
		}
		return content
	}
	continuation := lineIndent + indentColumns(options, options.IndentSize)
	var out strings.Builder
	lineLen := initialLineLen
	for _, word := range words {
		wordLen := visualLen(word)
		space := 0
		if out.Len() > 0 && !strings.HasSuffix(out.String(), "\n") {
			space = 1
		}
		if out.Len() > 0 && lineLen+space+wordLen > options.WrapLineLength {
			out.WriteByte('\n')
			out.WriteString(continuation)
			lineLen = visualLen(continuation)
			space = 0
		}
		if space > 0 {
			out.WriteByte(' ')
			lineLen++
		}
		out.WriteString(word)
		lineLen += wordLen
	}
	result := out.String()
	finalLineLen := lineTailLen(result)
	if !strings.Contains(result, "\n") {
		finalLineLen = initialLineLen + visualLen(result)
	}
	if finalLineLen+visualLen(closeTag) > options.WrapLineLength {
		result += "\n" + lineIndent
	}
	return result
}

type inlineWrapToken struct {
	text        string
	spaceBefore bool
	tag         bool
	name        string
	attrs       []string
	closing     bool
	selfClosing bool
}

func wrapInlineHTMLContent(openTag, content, closeTag string, options *Options, lineIndent string, initialLineLen int) string {
	tokens := splitInlineTokens(content, options.UnformattedContentDelimiter, options)
	if len(tokens) == 0 {
		return content
	}
	if len(tokens) == 1 && !tokens[0].tag && isInlineName(lowerASCII(tagName(openTag)), options) {
		return content
	}
	continuation := lineIndent + indentColumns(options, options.IndentSize)
	attrContinuation := continuation + indentColumns(options, options.WrapAttributesIndentSize)
	var out strings.Builder
	lineLen := initialLineLen
	wrote := false
	for _, token := range tokens {
		space := token.spaceBefore && wrote && !strings.HasSuffix(out.String(), "\n")
		spaceLen := 0
		if space {
			spaceLen = 1
		}
		tokenLen := visualLen(token.text)
		if token.tag && len(token.attrs) > 0 && !token.closing && lineLen+spaceLen+tokenLen > options.WrapLineLength {
			nameLen := 1 + visualLen(token.name)
			if wrote && lineLen+spaceLen+nameLen > options.WrapLineLength {
				out.WriteByte('\n')
				out.WriteString(continuation)
				lineLen = visualLen(continuation)
				space = false
				spaceLen = 0
			}
			if lineLen+spaceLen+nameLen <= options.WrapLineLength {
				if space {
					out.WriteByte(' ')
					lineLen++
				}
				out.WriteByte('<')
				out.WriteString(token.name)
				lineLen += nameLen
				attrText := strings.Join(token.attrs, " ")
				end := ">"
				if token.selfClosing {
					end = " />"
				}
				out.WriteByte('\n')
				out.WriteString(attrContinuation)
				out.WriteString(attrText)
				out.WriteString(end)
				lineLen = visualLen(attrContinuation) + visualLen(attrText) + visualLen(end)
				wrote = true
				continue
			}
		}
		if wrote && space && lineLen+spaceLen+tokenLen > options.WrapLineLength {
			out.WriteByte('\n')
			out.WriteString(continuation)
			lineLen = visualLen(continuation)
			space = false
		}
		if !wrote && lineLen+tokenLen > options.WrapLineLength {
			out.WriteByte('\n')
			out.WriteString(continuation)
			lineLen = visualLen(continuation)
		}
		if space {
			out.WriteByte(' ')
			lineLen++
		}
		out.WriteString(token.text)
		lineLen += tokenLen
		wrote = true
	}
	result := out.String()
	finalLineLen := lineTailLen(result)
	if !strings.Contains(result, "\n") {
		finalLineLen = initialLineLen + visualLen(result)
	}
	if finalLineLen+visualLen(closeTag) > options.WrapLineLength {
		result += "\n" + lineIndent
	}
	return result
}

func splitInlineTokens(content string, delimiter string, options *Options) []inlineWrapToken {
	var tokens []inlineWrapToken
	var current strings.Builder
	currentSpaceBefore := false
	pendingSpace := false
	first := true
	flushCurrent := func() {
		if current.Len() == 0 {
			return
		}
		tokens = append(tokens, inlineWrapToken{text: current.String(), spaceBefore: currentSpaceBefore})
		current.Reset()
		currentSpaceBefore = false
		first = false
	}
	startCurrent := func() {
		if current.Len() == 0 {
			currentSpaceBefore = pendingSpace && !first
			pendingSpace = false
		}
	}
	appendToken := func(text string, tag bool) {
		if text == "" {
			return
		}
		flushCurrent()
		token := inlineWrapToken{text: text, spaceBefore: pendingSpace && !first, tag: tag}
		if tag {
			if name, attrs, closing, selfClosing, ok := splitTag(text); ok {
				token.name = name
				token.attrs = attrs
				token.closing = closing
				token.selfClosing = selfClosing
			}
		}
		tokens = append(tokens, token)
		pendingSpace = false
		first = false
	}
	for i := 0; i < len(content); {
		if delimiter != "" && startsDelimiterWord(content, i, delimiter) {
			end := strings.Index(content[i+len(delimiter):], delimiter)
			if end < 0 {
				startCurrent()
				current.WriteString(strings.TrimSpace(content[i:]))
				break
			}
			endIndex := i + len(delimiter) + end + len(delimiter)
			for endIndex < len(content) && !isASCIIWhitespace(content[endIndex]) && content[endIndex] != '<' {
				endIndex++
			}
			startCurrent()
			current.WriteString(strings.TrimSpace(content[i:endIndex]))
			i = endIndex
			continue
		}
		if isASCIIWhitespace(content[i]) {
			flushCurrent()
			pendingSpace = !first
			i++
			continue
		}
		if content[i] == '<' {
			tag, next := readHTMLTag(content, i)
			if tag != "" && next > i {
				formatted := formatTag(tag, options, "")
				_, attrs, closing, selfClosing, ok := splitTag(formatted)
				if ok && len(attrs) > 0 && !closing && !selfClosing {
					appendToken(formatted, true)
				} else {
					startCurrent()
					current.WriteString(formatted)
				}
				i = next
				continue
			}
		}
		next := i + 1
		for next < len(content) && !isASCIIWhitespace(content[next]) && content[next] != '<' {
			next++
		}
		startCurrent()
		current.WriteString(content[i:next])
		i = next
	}
	flushCurrent()
	return tokens
}

func splitInlineWords(content string, delimiter string) []string {
	var words []string
	var current strings.Builder
	for i := 0; i < len(content); {
		if delimiter != "" && startsDelimiterWord(content, i, delimiter) {
			if current.Len() > 0 {
				words = append(words, current.String())
				current.Reset()
			}
			end := strings.Index(content[i+len(delimiter):], delimiter)
			if end < 0 {
				words = append(words, strings.TrimSpace(content[i:]))
				break
			}
			endIndex := i + len(delimiter) + end + len(delimiter)
			for endIndex < len(content) && !isASCIIWhitespace(content[endIndex]) {
				endIndex++
			}
			words = append(words, strings.TrimSpace(content[i:endIndex]))
			i = endIndex
			continue
		}
		if content[i] == '<' {
			tag, next := readHTMLTag(content, i)
			if tag != "" && next > i {
				current.WriteString(tag)
				i = next
				continue
			}
		}
		switch content[i] {
		case ' ', '\t', '\n', '\r':
			if current.Len() > 0 {
				words = append(words, current.String())
				current.Reset()
			}
			i++
		default:
			current.WriteByte(content[i])
			i++
		}
	}
	if current.Len() > 0 {
		words = append(words, current.String())
	}
	return words
}

func startsDelimiterWord(content string, index int, delimiter string) bool {
	if !strings.HasPrefix(content[index:], delimiter) {
		return false
	}
	return index == 0 || isASCIIWhitespace(content[index-1])
}

func isASCIIWhitespace(ch byte) bool {
	return ch == ' ' || ch == '\t' || ch == '\n' || ch == '\r'
}

func visualLen(value string) int {
	for i := 0; i < len(value); i++ {
		if value[i] == '\t' || value[i] >= 0x80 {
			width := i
			for _, r := range value[i:] {
				if r == '\t' {
					width += 4
				} else {
					width++
				}
			}
			return width
		}
	}
	return len(value)
}

func lineTailLen(value string) int {
	if index := strings.LastIndex(value, "\n"); index >= 0 {
		return visualLen(value[index+1:])
	}
	return visualLen(value)
}

func startsWithTextBeforeLineBreak(source string) bool {
	for i := 0; i < len(source); i++ {
		switch source[i] {
		case ' ', '\t':
			continue
		case '\n', '\r', '<':
			return false
		default:
			return true
		}
	}
	return false
}

func startsWithInlineTagBeforeLineBreak(source string, options *Options) bool {
	for i := 0; i < len(source); i++ {
		switch source[i] {
		case ' ', '\t':
			continue
		case '\n', '\r':
			return false
		case '<':
			tag, next := readHTMLTag(source, i)
			if tag == "" || next == i || isCloseTag(tag) {
				return false
			}
			name := lowerASCII(tagName(tag))
			return name != "" && isInlineName(name, options)
		default:
			return false
		}
	}
	return false
}

func closeTagAttachedToContent(name string, source string, index int) bool {
	if name != "p" {
		return false
	}
	if index <= 0 || index > len(source) {
		return false
	}
	return !isASCIIWhitespace(source[index-1]) && source[index-1] != '>'
}

func indentColumns(options *Options, columns int) string {
	if columns <= 0 {
		return ""
	}
	if options.IndentWithTabs {
		tabs := columns / options.IndentSize
		spaces := columns % options.IndentSize
		return strings.Repeat("\t", tabs) + strings.Repeat(" ", spaces)
	}
	return strings.Repeat(" ", columns)
}

func lineBreakCount(text string) int {
	return strings.Count(text, "\n")
}

func leadingLineBreakCount(text string) int {
	count := 0
	for i := 0; i < len(text); i++ {
		switch text[i] {
		case ' ', '\t':
			continue
		case '\n':
			count++
		default:
			return count
		}
	}
	return count
}

func trailingLineBreakCount(text string) int {
	count := 0
	for i := len(text) - 1; i >= 0; i-- {
		switch text[i] {
		case ' ', '\t':
			continue
		case '\n':
			count++
		default:
			return count
		}
	}
	return count
}

func collapseASCIIWhitespace(text string) string {
	var out strings.Builder
	inWhitespace := false
	for i := 0; i < len(text); i++ {
		switch text[i] {
		case ' ', '\t', '\n', '\r':
			if !inWhitespace && out.Len() > 0 {
				out.WriteByte(' ')
			}
			inWhitespace = true
		default:
			out.WriteByte(text[i])
			inWhitespace = false
		}
	}
	return strings.Trim(out.String(), " ")
}

func collapseInlineText(text string, delimiter string) string {
	if delimiter == "" || !strings.Contains(text, delimiter) {
		return collapseASCIIWhitespace(text)
	}
	var out strings.Builder
	inWhitespace := false
	for i := 0; i < len(text); {
		if startsDelimiterWord(text, i, delimiter) {
			if out.Len() > 0 && !strings.HasSuffix(out.String(), " ") {
				out.WriteByte(' ')
			}
			end := strings.Index(text[i+len(delimiter):], delimiter)
			if end < 0 {
				out.WriteString(strings.TrimSpace(text[i:]))
				break
			}
			endIndex := i + len(delimiter) + end + len(delimiter)
			for endIndex < len(text) && !isASCIIWhitespace(text[endIndex]) {
				endIndex++
			}
			out.WriteString(strings.TrimSpace(text[i:endIndex]))
			i = endIndex
			inWhitespace = false
			continue
		}
		switch text[i] {
		case ' ', '\t', '\n', '\r':
			if !inWhitespace && out.Len() > 0 {
				out.WriteByte(' ')
			}
			inWhitespace = true
			i++
		default:
			out.WriteByte(text[i])
			inWhitespace = false
			i++
		}
	}
	return strings.Trim(out.String(), " ")
}

func hasLeadingASCIIWhitespace(text string) bool {
	if text == "" {
		return false
	}
	switch text[0] {
	case ' ', '\t', '\n', '\r':
		return true
	default:
		return false
	}
}

func hasTrailingASCIIWhitespace(text string) bool {
	for i := len(text) - 1; i >= 0; i-- {
		switch text[i] {
		case ' ', '\t', '\n', '\r':
			return true
		default:
			return false
		}
	}
	return false
}

func hasOpenBlockParent(indent int) bool {
	return indent > 0
}

func leadingIndent(source string) string {
	i := 0
	for i < len(source) && (source[i] == ' ' || source[i] == '\t') {
		i++
	}
	return source[:i]
}
